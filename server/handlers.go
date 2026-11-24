package server

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"code-markdocx/config"
	"code-markdocx/pkg/docxparser"
	"code-markdocx/pkg/imgprep"
	"code-markdocx/pkg/mdpolish"
	"code-markdocx/pkg/vlm"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handlers struct {
	cfg       *config.Config
	store     *JobStore
	vlmClient *vlm.Client
}

func NewHandlers(cfg *config.Config, store *JobStore, vlmClient *vlm.Client) *Handlers {
	return &Handlers{
		cfg:       cfg,
		store:     store,
		vlmClient: vlmClient,
	}
}

func RegisterRoutes(r *gin.Engine, h *Handlers) {
	api := r.Group("/api")
	{
		api.GET("/health", h.Health)
		api.GET("/jobs", h.ListJobs)
		api.POST("/jobs", h.CreateJob)
		api.GET("/jobs/:id", h.GetJob)
		api.DELETE("/jobs/:id", h.DeleteJob)
		api.GET("/jobs/:id/images/:img_id", h.GetImage)
		api.PATCH("/jobs/:id/images/:img_id", h.PatchImage)
		api.POST("/jobs/:id/images/:img_id/retry", h.RetryImage)
		api.GET("/jobs/:id/export", h.ExportJob)
	}
}

func (h *Handlers) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":            "ok",
		"version":           config.AppVersion,
		"vlm_configured":    h.cfg.VLM.IsConfigured(),
		"vlm_model":         h.cfg.VLM.Model,
		"soffice_available": imgprep.DetectSoffice(h.cfg.Convert.SofficePath) != "",
		"max_upload_mb":     h.cfg.Server.MaxUploadMB,
		"ref_doc_max_bytes": h.cfg.Convert.RefDocMaxBytes,
	})
}

func (h *Handlers) ListJobs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"jobs": h.store.ListSummaries(),
	})
}

func (h *Handlers) CreateJob(c *gin.Context) {
	maxBytes := h.cfg.Server.MaxUploadMB * 1024 * 1024
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+(1<<20))

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传 .docx 文件: " + err.Error()})
		return
	}
	filename := filepath.Base(fileHeader.Filename)
	if !strings.EqualFold(filepath.Ext(filename), ".docx") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 .docx 格式文档（老版 .doc 请先用 Word/WPS 另存为 .docx）"})
		return
	}
	if fileHeader.Size > maxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("文件超过限制 (%d MB)", h.cfg.Server.MaxUploadMB)})
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法读取上传文件"})
		return
	}
	defer f.Close()

	docxBytes, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取上传文件内容失败"})
		return
	}
	if int64(len(docxBytes)) > maxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("文件超过限制 (%d MB)", h.cfg.Server.MaxUploadMB)})
		return
	}

	opts := JobOptions{
		StripTOC:           parseBoolForm(c.PostForm("strip_toc"), h.cfg.Convert.StripTOC),
		StripCover:         parseBoolForm(c.PostForm("strip_cover"), h.cfg.Convert.StripCover),
		StripRevisionTable: parseBoolForm(c.PostForm("strip_revision_table"), h.cfg.Convert.StripRevisionTable),
		SkipVLM:            parseBoolForm(c.PostForm("skip_vlm"), false),
	}
	glossary := strings.TrimSpace(c.PostForm("glossary"))

	doc, err := docxparser.ParseBytes(docxBytes, docxparser.ParseOptions{
		StripTOC:           opts.StripTOC,
		StripCover:         opts.StripCover,
		StripRevisionTable: opts.StripRevisionTable,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "解析 .docx 失败: " + err.Error()})
		return
	}

	jobID := uuid.NewString()
	workDir, err := h.store.CreateJobDir(jobID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建临时工作目录失败"})
		return
	}

	imgDir := filepath.Join(workDir, "images")
	prepped, err := imgprep.PrepareImages(doc.Images, imgprep.PrepOptions{
		MinImagePx:          h.cfg.Convert.MinImagePx,
		MaxImageDim:         h.cfg.Convert.MaxImageDim,
		DedupeLogoThreshold: h.cfg.Convert.DedupeLogoThreshold,
		SofficePath:         h.cfg.Convert.SofficePath,
		OutputDir:           imgDir,
	})
	if err != nil {
		_ = os.RemoveAll(workDir)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "图片预处理失败: " + err.Error()})
		return
	}

	states := make(map[string]*mdpolish.ImageState, len(prepped))
	imageOrder := make([]string, 0, len(prepped))
	imageFiles := make(map[string]ImageFileMeta, len(prepped))
	var vlmInputs []vlm.ConvertImageInput

	vlmConfigured := h.cfg.VLM.IsConfigured() && h.vlmClient != nil

	for _, p := range prepped {
		a := p.Asset
		imageOrder = append(imageOrder, a.ID)
		if p.PreparedPath != "" {
			imageFiles[a.ID] = ImageFileMeta{Path: p.PreparedPath, ContentType: p.PreparedCT}
		}
		st := &mdpolish.ImageState{
			ImageID:           a.ID,
			OrigFilename:      a.OrigFilename,
			SectionPath:       a.SectionPath,
			Caption:           a.Caption,
			ContextBefore:     a.ContextBefore,
			ContextAfter:      a.ContextAfter,
			Width:             a.Width,
			Height:            a.Height,
			Status:            mdpolish.StatusPending,
			Skipped:           a.PreSkipped,
			SkipReason:        a.SkipReason,
			UnsupportedVector: a.UnsupportedVector,
			VectorWarning:     a.VectorWarning,
		}
		switch {
		case a.PreSkipped:
			st.Status = mdpolish.StatusSkipped
		case a.UnsupportedVector:
			st.Status = mdpolish.StatusUnsupportedVector
		case opts.SkipVLM:
			st.Status = mdpolish.StatusSkipped
			st.Skipped = true
			st.SkipReason = "已选择跳过 VLM 识图"
		case !vlmConfigured:
			st.Status = mdpolish.StatusFailed
			st.Error = "服务端 config.yaml 未配置有效 VLM (base_url / api_key / model)"
		default:
			st.Status = mdpolish.StatusProcessing
			vlmInputs = append(vlmInputs, vlm.ConvertImageInput{
				ImageID:       a.ID,
				ImageBytes:    p.PreparedData,
				ContentType:   p.PreparedCT,
				SectionPath:   a.SectionPath,
				Caption:       a.Caption,
				ContextBefore: a.ContextBefore,
				ContextAfter:  a.ContextAfter,
				GlossaryHints: glossary,
			})
		}
		states[a.ID] = st
	}

	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		ID:            jobID,
		Filename:      filename,
		GlossaryHints: glossary,
		Options:       opts,
		Status:        JobDone,
		WorkDir:       workDir,
		Doc:           doc,
		ImageOrder:    imageOrder,
		Images:        states,
		ImageFiles:    imageFiles,
		ProgressTotal: len(vlmInputs),
		Cancel:        cancel,
	}
	if len(vlmInputs) > 0 {
		job.Status = JobRunning
	}

	if err := h.store.AddJob(job); err != nil {
		cancel()
		_ = os.RemoveAll(workDir)
		if errors.Is(err, ErrTooManyJobs) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(vlmInputs) > 0 {
		go h.runBatchVLM(ctx, jobID, vlmInputs)
	}

	detail, err := h.store.GetDetail(jobID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, detail)
}

func (h *Handlers) runBatchVLM(ctx context.Context, jobID string, inputs []vlm.ConvertImageInput) {
	vlm.ProcessBatch(ctx, h.vlmClient, inputs, h.cfg.VLM.MaxConcurrency, func(prog vlm.BatchProgress) {
		_ = h.store.MutateJob(jobID, func(job *Job) {
			job.ProgressDone = prog.Done
			st := job.Images[prog.ImageID]
			if st == nil {
				return
			}
			if prog.Err != nil {
				st.Status = mdpolish.StatusFailed
				st.Error = prog.Err.Error()
				return
			}
			applyVLMResult(st, prog.Result)
		})
	})

	_ = h.store.MutateJob(jobID, func(job *Job) {
		if ctx.Err() != nil {
			job.Status = JobFailed
			job.Error = "任务已取消"
		} else {
			job.Status = JobDone
		}
	})
}

func applyVLMResult(st *mdpolish.ImageState, res *vlm.VLMResult) {
	if res == nil {
		return
	}
	st.Status = mdpolish.StatusDone
	st.Error = ""
	st.Category = res.Category
	st.DiagramType = res.DiagramType
	st.MermaidCode = res.MermaidCode
	st.MarkdownTable = res.MarkdownTable
	st.Summary = res.Summary
	st.Confidence = res.Confidence
	st.Notes = res.Notes
	if res.RecommendedSkip {
		st.Skipped = true
		st.Status = mdpolish.StatusSkipped
		st.SkipReason = "VLM 判定为非架构装饰/界面图"
	} else {
		st.Skipped = false
		st.SkipReason = ""
	}
}

func (h *Handlers) GetJob(c *gin.Context) {
	detail, err := h.store.GetDetail(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在或已过期"})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (h *Handlers) DeleteJob(c *gin.Context) {
	if err := h.store.DeleteJob(c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在或已清理"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func (h *Handlers) GetImage(c *gin.Context) {
	meta, err := h.store.GetImageFile(c.Param("id"), c.Param("img_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "图片不存在或为无法预览的矢量图"})
		return
	}
	if meta.ContentType != "" {
		c.Header("Content-Type", meta.ContentType)
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.File(meta.Path)
}

type patchImageReq struct {
	Category      *vlm.DiagramCategory `json:"category"`
	DiagramType   *vlm.DiagramType     `json:"diagram_type"`
	MermaidCode   *string              `json:"mermaid_code"`
	MarkdownTable *string              `json:"markdown_table"`
	Summary       *string              `json:"summary"`
	Skipped       *bool                `json:"skipped"`
}

func (h *Handlers) PatchImage(c *gin.Context) {
	var req patchImageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效: " + err.Error()})
		return
	}

	st, md, preflight, err := h.store.UpdateImage(c.Param("id"), c.Param("img_id"), func(st *mdpolish.ImageState) {
		if req.Category != nil {
			st.Category = *req.Category
		}
		if req.DiagramType != nil {
			st.DiagramType = *req.DiagramType
		}
		if req.MermaidCode != nil {
			st.MermaidCode = *req.MermaidCode
		}
		if req.MarkdownTable != nil {
			st.MarkdownTable = *req.MarkdownTable
		}
		if req.Summary != nil {
			st.Summary = *req.Summary
		}
		if req.Skipped != nil {
			st.Skipped = *req.Skipped
			if *req.Skipped {
				st.Status = mdpolish.StatusSkipped
				if st.SkipReason == "" {
					st.SkipReason = "人工跳过"
				}
			} else if !st.UnsupportedVector {
				st.Status = mdpolish.StatusDone
				st.SkipReason = ""
			}
		} else if !st.Skipped && !st.UnsupportedVector {
			st.Status = mdpolish.StatusDone
		}
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务或图片不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"image":     st,
		"markdown":  md,
		"preflight": preflight,
	})
}

type retryImageReq struct {
	MermaidError string `json:"mermaid_error"`
	Instruction  string `json:"instruction"`
}

func (h *Handlers) RetryImage(c *gin.Context) {
	if !h.cfg.VLM.IsConfigured() || h.vlmClient == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "服务端 config.yaml 未配置有效 VLM 模型"})
		return
	}
	var req retryImageReq
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效: " + err.Error()})
			return
		}
	}

	jobID := c.Param("id")
	imgID := c.Param("img_id")
	meta, prevState, glossary, err := h.store.SnapshotImageForRetry(jobID, imgID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务或图片不存在"})
		return
	}
	if meta.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该图片无可用位图文件（可能是未转换矢量图）"})
		return
	}
	imgBytes, err := os.ReadFile(meta.Path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取图片文件失败"})
		return
	}

	convInput := vlm.ConvertImageInput{
		ImageID:       imgID,
		ImageBytes:    imgBytes,
		ContentType:   meta.ContentType,
		SectionPath:   prevState.SectionPath,
		Caption:       prevState.Caption,
		ContextBefore: prevState.ContextBefore,
		ContextAfter:  prevState.ContextAfter,
		GlossaryHints: glossary,
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(h.cfg.VLM.TimeoutSeconds)*time.Second)
	defer cancel()

	var res *vlm.VLMResult
	if strings.TrimSpace(req.MermaidError) != "" || strings.TrimSpace(req.Instruction) != "" || prevState.MermaidCode != "" {
		res, err = h.vlmClient.RepairImage(ctx, vlm.RepairImageInput{
			ConvertImageInput: convInput,
			PreviousResult: &vlm.VLMResult{
				Category:      prevState.Category,
				DiagramType:   prevState.DiagramType,
				MermaidCode:   prevState.MermaidCode,
				MarkdownTable: prevState.MarkdownTable,
				Summary:       prevState.Summary,
			},
			MermaidError: req.MermaidError,
			Instruction:  req.Instruction,
		})
	} else {
		res, err = h.vlmClient.ConvertImage(ctx, convInput)
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "VLM 调用失败: " + err.Error()})
		return
	}

	st, md, preflight, err := h.store.UpdateImage(jobID, imgID, func(st *mdpolish.ImageState) {
		applyVLMResult(st, res)
		// User explicitly requested retry, so don't auto-skip even if VLM suggested skip.
		if st.Skipped {
			st.Skipped = false
			st.Status = mdpolish.StatusDone
			st.SkipReason = ""
		}
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"image":     st,
		"markdown":  md,
		"preflight": preflight,
	})
}

func (h *Handlers) ExportJob(c *gin.Context) {
	filename, md, err := h.store.SnapshotExport(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在或已过期"})
		return
	}
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	if base == "" {
		base = "converted"
	}

	if c.Query("split") == "true" {
		chunks := mdpolish.SplitByH2(md, filename, h.cfg.Convert.RefDocMaxBytes)
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, ch := range chunks {
			w, wErr := zw.Create(ch.Filename)
			if wErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "生成 ZIP 失败"})
				return
			}
			if _, wErr = w.Write([]byte(ch.Content)); wErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "写入 ZIP 失败"})
				return
			}
		}
		if err := zw.Close(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "关闭 ZIP 失败"})
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-split.zip"`, sanitizeHeaderFilename(base)))
		c.Data(http.StatusOK, "application/zip", buf.Bytes())
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.md"`, sanitizeHeaderFilename(base)))
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(md))
}

func parseBoolForm(raw string, def bool) bool {
	if strings.TrimSpace(raw) == "" {
		return def
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return v
}

func sanitizeHeaderFilename(s string) string {
	s = strings.ReplaceAll(s, `"`, "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
