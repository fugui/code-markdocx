package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"code-markdocx/pkg/docxparser"
	"code-markdocx/pkg/mdpolish"
)

var (
	ErrJobNotFound   = errors.New("job not found")
	ErrImageNotFound = errors.New("image not found")
	ErrTooManyJobs   = errors.New("too many active jobs running, please wait")
)

type JobStatus string

const (
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

type JobOptions struct {
	StripTOC                 bool `json:"strip_toc"`
	StripCover               bool `json:"strip_cover"`
	StripRevisionTable       bool `json:"strip_revision_table"`
	SkipVLM                  bool `json:"skip_vlm"`
	KeepUnconvertedOriginals bool `json:"keep_unconverted_originals"`
}

type BundleAsset struct {
	ArchivePath string
	DiskPath    string
}

type ImageFileMeta struct {
	Path        string
	ContentType string
}

type Job struct {
	ID            string
	Filename      string
	GlossaryHints string
	Options       JobOptions
	Status        JobStatus
	Error         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ExpiresAt     time.Time
	WorkDir       string
	Doc           *docxparser.DocumentIR
	ImageOrder    []string
	Images        map[string]*mdpolish.ImageState
	ImageFiles    map[string]ImageFileMeta
	Markdown      string
	Preflight     mdpolish.PreflightReport
	ProgressDone  int
	ProgressTotal int
	Cancel        context.CancelFunc
}

type JobSummary struct {
	ID            string                `json:"id"`
	Filename      string                `json:"filename"`
	Status        JobStatus             `json:"status"`
	CreatedAt     time.Time             `json:"created_at"`
	ExpiresAt     time.Time             `json:"expires_at"`
	ProgressDone  int                   `json:"progress_done"`
	ProgressTotal int                   `json:"progress_total"`
	ImageCount    int                   `json:"image_count"`
	ByteSize      int                   `json:"byte_size"`
	NoiseStats    docxparser.NoiseStats `json:"noise_stats"`
}

type JobDetail struct {
	ID            string                   `json:"id"`
	Filename      string                   `json:"filename"`
	GlossaryHints string                   `json:"glossary_hints"`
	Options       JobOptions               `json:"options"`
	Status        JobStatus                `json:"status"`
	Error         string                   `json:"error,omitempty"`
	CreatedAt     time.Time                `json:"created_at"`
	UpdatedAt     time.Time                `json:"updated_at"`
	ExpiresAt     time.Time                `json:"expires_at"`
	ProgressDone  int                      `json:"progress_done"`
	ProgressTotal int                      `json:"progress_total"`
	NoiseStats    docxparser.NoiseStats    `json:"noise_stats"`
	Images        []*mdpolish.ImageState   `json:"images"`
	Markdown      string                   `json:"markdown"`
	Preflight     mdpolish.PreflightReport `json:"preflight"`
}

type JobStore struct {
	mu             sync.RWMutex
	jobs           map[string]*Job
	tempDir        string
	ttl            time.Duration
	maxJobs        int
	refDocMaxBytes int64
	stopCh         chan struct{}
	stopOnce       sync.Once
}

func NewJobStore(tempDir string, ttlMinutes, maxJobs int, refDocMaxBytes int64) (*JobStore, error) {
	if tempDir == "" {
		tempDir = "./tmp_jobs"
	}
	if ttlMinutes <= 0 {
		ttlMinutes = 120
	}
	if maxJobs <= 0 {
		maxJobs = 20
	}
	if refDocMaxBytes <= 0 {
		refDocMaxBytes = 524288
	}
	if err := os.MkdirAll(tempDir, 0750); err != nil {
		return nil, fmt.Errorf("create temp dir %s: %w", tempDir, err)
	}
	js := &JobStore{
		jobs:           make(map[string]*Job),
		tempDir:        tempDir,
		ttl:            time.Duration(ttlMinutes) * time.Minute,
		maxJobs:        maxJobs,
		refDocMaxBytes: refDocMaxBytes,
		stopCh:         make(chan struct{}),
	}
	go js.reapLoop()
	return js, nil
}

func (s *JobStore) CreateJobDir(jobID string) (string, error) {
	dir := filepath.Join(s.tempDir, jobID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *JobStore) AddJob(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.evictExpiredLocked(now)

	if len(s.jobs) >= s.maxJobs {
		if !s.evictOldestCompletedLocked() {
			return ErrTooManyJobs
		}
	}

	job.CreatedAt = now
	job.UpdatedAt = now
	job.ExpiresAt = now.Add(s.ttl)
	s.reassembleLocked(job)
	s.jobs[job.ID] = job
	return nil
}

func (s *JobStore) GetDetail(id string) (*JobDetail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	return s.toDetailLocked(job), nil
}

func (s *JobStore) ListSummaries() []JobSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]JobSummary, 0, len(s.jobs))
	for _, j := range s.jobs {
		var ns docxparser.NoiseStats
		if j.Doc != nil {
			ns = j.Doc.NoiseStats
		}
		list = append(list, JobSummary{
			ID:            j.ID,
			Filename:      j.Filename,
			Status:        j.Status,
			CreatedAt:     j.CreatedAt,
			ExpiresAt:     j.ExpiresAt,
			ProgressDone:  j.ProgressDone,
			ProgressTotal: j.ProgressTotal,
			ImageCount:    len(j.ImageOrder),
			ByteSize:      len(j.Markdown),
			NoiseStats:    ns,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list
}

func (s *JobStore) DeleteJob(id string) error {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return ErrJobNotFound
	}
	delete(s.jobs, id)
	s.mu.Unlock()

	if job.Cancel != nil {
		job.Cancel()
	}
	if job.WorkDir != "" {
		_ = os.RemoveAll(job.WorkDir)
	}
	return nil
}

func (s *JobStore) GetImageFile(jobID, imgID string) (ImageFileMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return ImageFileMeta{}, ErrJobNotFound
	}
	meta, ok := job.ImageFiles[imgID]
	if !ok || meta.Path == "" {
		return ImageFileMeta{}, ErrImageNotFound
	}
	return meta, nil
}

func (s *JobStore) UpdateImage(jobID, imgID string, fn func(st *mdpolish.ImageState)) (*mdpolish.ImageState, string, mdpolish.PreflightReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return nil, "", mdpolish.PreflightReport{}, ErrJobNotFound
	}
	st, ok := job.Images[imgID]
	if !ok {
		return nil, "", mdpolish.PreflightReport{}, ErrImageNotFound
	}
	fn(st)
	job.UpdatedAt = time.Now()
	s.reassembleLocked(job)
	copied := *st
	return &copied, job.Markdown, job.Preflight, nil
}

func (s *JobStore) UpdateImagesBatch(jobID string, fn func(job *Job) int) ([]*mdpolish.ImageState, string, mdpolish.PreflightReport, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return nil, "", mdpolish.PreflightReport{}, 0, ErrJobNotFound
	}
	updated := fn(job)
	job.UpdatedAt = time.Now()
	s.reassembleLocked(job)
	imgs := make([]*mdpolish.ImageState, 0, len(job.ImageOrder))
	for _, id := range job.ImageOrder {
		if st := job.Images[id]; st != nil {
			cp := *st
			imgs = append(imgs, &cp)
		}
	}
	return imgs, job.Markdown, job.Preflight, updated, nil
}

func (s *JobStore) MutateJob(jobID string, fn func(job *Job)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return ErrJobNotFound
	}
	fn(job)
	job.UpdatedAt = time.Now()
	s.reassembleLocked(job)
	return nil
}

func (s *JobStore) SnapshotImageForRetry(jobID, imgID string) (ImageFileMeta, mdpolish.ImageState, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return ImageFileMeta{}, mdpolish.ImageState{}, "", ErrJobNotFound
	}
	st, ok := job.Images[imgID]
	if !ok {
		return ImageFileMeta{}, mdpolish.ImageState{}, "", ErrImageNotFound
	}
	meta := job.ImageFiles[imgID]
	return meta, *st, job.GlossaryHints, nil
}

func (s *JobStore) SnapshotExport(jobID string) (string, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return "", "", ErrJobNotFound
	}
	return job.Filename, job.Markdown, nil
}

func (s *JobStore) SnapshotBundleExport(jobID string) (string, string, []BundleAsset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return "", "", nil, ErrJobNotFound
	}
	var assets []BundleAsset
	seen := make(map[string]struct{})
	for _, id := range job.ImageOrder {
		st := job.Images[id]
		if st == nil || st.Skipped || st.Status == mdpolish.StatusSkipped || !st.KeepOriginal || st.AssetFilename == "" {
			continue
		}
		meta := job.ImageFiles[id]
		if meta.Path == "" {
			continue
		}
		archivePath := "assets/" + st.AssetFilename
		if _, exists := seen[archivePath]; exists {
			continue
		}
		seen[archivePath] = struct{}{}
		assets = append(assets, BundleAsset{
			ArchivePath: archivePath,
			DiskPath:    meta.Path,
		})
	}
	return job.Filename, job.Markdown, assets, nil
}

func (s *JobStore) reassembleLocked(job *Job) {
	if job.Doc == nil {
		return
	}
	ordered := make([]*mdpolish.ImageState, 0, len(job.ImageOrder))
	for _, id := range job.ImageOrder {
		if st := job.Images[id]; st != nil {
			ordered = append(ordered, st)
		}
	}
	job.Markdown = mdpolish.Assemble(job.Doc, job.Images)
	job.Preflight = mdpolish.Analyze(job.Markdown, ordered, s.refDocMaxBytes)
}

func (s *JobStore) toDetailLocked(job *Job) *JobDetail {
	imgs := make([]*mdpolish.ImageState, 0, len(job.ImageOrder))
	for _, id := range job.ImageOrder {
		if st := job.Images[id]; st != nil {
			cp := *st
			imgs = append(imgs, &cp)
		}
	}
	var ns docxparser.NoiseStats
	if job.Doc != nil {
		ns = job.Doc.NoiseStats
	}
	return &JobDetail{
		ID:            job.ID,
		Filename:      job.Filename,
		GlossaryHints: job.GlossaryHints,
		Options:       job.Options,
		Status:        job.Status,
		Error:         job.Error,
		CreatedAt:     job.CreatedAt,
		UpdatedAt:     job.UpdatedAt,
		ExpiresAt:     job.ExpiresAt,
		ProgressDone:  job.ProgressDone,
		ProgressTotal: job.ProgressTotal,
		NoiseStats:    ns,
		Images:        imgs,
		Markdown:      job.Markdown,
		Preflight:     job.Preflight,
	}
}

func (s *JobStore) evictExpiredLocked(now time.Time) []string {
	var workDirs []string
	for id, j := range s.jobs {
		if now.After(j.ExpiresAt) && j.Status != JobRunning {
			delete(s.jobs, id)
			if j.WorkDir != "" {
				workDirs = append(workDirs, j.WorkDir)
			}
		}
	}
	return workDirs
}

func (s *JobStore) evictOldestCompletedLocked() bool {
	var oldestID string
	var oldestTime time.Time
	for id, j := range s.jobs {
		if j.Status == JobRunning {
			continue
		}
		if oldestID == "" || j.CreatedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = j.CreatedAt
		}
	}
	if oldestID == "" {
		return false
	}
	j := s.jobs[oldestID]
	delete(s.jobs, oldestID)
	if j.WorkDir != "" {
		go os.RemoveAll(j.WorkDir)
	}
	return true
}

func (s *JobStore) reapLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			dirs := s.evictExpiredLocked(now)
			s.mu.Unlock()
			for _, d := range dirs {
				_ = os.RemoveAll(d)
			}
		}
	}
}

func (s *JobStore) Close() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.mu.Lock()
	jobs := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	s.jobs = make(map[string]*Job)
	s.mu.Unlock()

	for _, j := range jobs {
		if j.Cancel != nil {
			j.Cancel()
		}
		if j.WorkDir != "" {
			_ = os.RemoveAll(j.WorkDir)
		}
	}
}
