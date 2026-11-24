package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ModuleKey   = "markdocx"
	ServiceName = "Code-Markdocx"
)

var AppVersion = "dev"

type ServerConfig struct {
	Port              string        `yaml:"port"`
	GinLog            bool          `yaml:"gin_log"`
	ReadTimeout       time.Duration `yaml:"read_timeout"`
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout"`
	WriteTimeout      time.Duration `yaml:"write_timeout"`
	IdleTimeout       time.Duration `yaml:"idle_timeout"`
	MaxHeaderBytes    int           `yaml:"max_header_bytes"`
	MaxUploadMB       int64         `yaml:"max_upload_mb"`
}

type VLMConfig struct {
	BaseURL           string  `yaml:"base_url"`
	APIKey            string  `yaml:"api_key"`
	Model             string  `yaml:"model"`
	TimeoutSeconds    int     `yaml:"timeout_seconds"`
	MaxConcurrency    int     `yaml:"max_concurrency"`
	MaxRetries        int     `yaml:"max_retries"`
	Temperature       float64 `yaml:"temperature"`
	MaxTokens         int     `yaml:"max_tokens"`
	UserAgent         string  `yaml:"user_agent"`
	ExtraSystemPrompt string  `yaml:"extra_system_prompt"`
}

// IsConfigured reports whether static VLM settings are usable for API calls.
func (v VLMConfig) IsConfigured() bool {
	base := strings.TrimSpace(v.BaseURL)
	model := strings.TrimSpace(v.Model)
	key := strings.TrimSpace(v.APIKey)
	if base == "" || model == "" {
		return false
	}
	if strings.EqualFold(key, "YOUR_VLM_API_KEY") {
		return false
	}
	return true
}

func (v VLMConfig) EffectiveUserAgent() string {
	if ua := strings.TrimSpace(v.UserAgent); ua != "" {
		return ua
	}
	ver := strings.TrimSpace(AppVersion)
	if ver == "" {
		ver = "dev"
	}
	return "Code-Markdocx/" + ver
}

type ConvertConfig struct {
	StripTOC            bool   `yaml:"strip_toc"`
	StripCover          bool   `yaml:"strip_cover"`
	StripRevisionTable  bool   `yaml:"strip_revision_table"`
	MinImagePx          int    `yaml:"min_image_px"`
	MaxImageDim         int    `yaml:"max_image_dim"`
	DedupeLogoThreshold int    `yaml:"dedupe_logo_threshold"`
	SofficePath         string `yaml:"soffice_path"`
	RefDocMaxBytes      int64  `yaml:"ref_doc_max_bytes"`
}

type StorageConfig struct {
	TempDir       string `yaml:"temp_dir"`
	JobTTLMinutes int    `yaml:"job_ttl_minutes"`
	MaxActiveJobs int    `yaml:"max_active_jobs"`
}

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	VLM     VLMConfig     `yaml:"vlm"`
	Convert ConvertConfig `yaml:"convert"`
	Storage StorageConfig `yaml:"storage"`
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:              ":8105",
			GinLog:            true,
			ReadTimeout:       120 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      180 * time.Second,
			IdleTimeout:       180 * time.Second,
			MaxHeaderBytes:    1048576,
			MaxUploadMB:       50,
		},
		VLM: VLMConfig{
			TimeoutSeconds: 90,
			MaxConcurrency: 3,
			MaxRetries:     2,
			Temperature:    0.1,
			MaxTokens:      4096,
		},
		Convert: ConvertConfig{
			StripTOC:            true,
			StripCover:          false,
			StripRevisionTable:  true,
			MinImagePx:          48,
			MaxImageDim:         2048,
			DedupeLogoThreshold: 3,
			RefDocMaxBytes:      524288,
		},
		Storage: StorageConfig{
			TempDir:       "./tmp_jobs",
			JobTTLMinutes: 120,
			MaxActiveJobs: 20,
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	applyDefaults(cfg)
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	def := DefaultConfig()
	if strings.TrimSpace(cfg.Server.Port) == "" {
		cfg.Server.Port = def.Server.Port
	}
	if !strings.HasPrefix(cfg.Server.Port, ":") {
		cfg.Server.Port = ":" + cfg.Server.Port
	}
	if cfg.Server.ReadTimeout <= 0 {
		cfg.Server.ReadTimeout = def.Server.ReadTimeout
	}
	if cfg.Server.ReadHeaderTimeout <= 0 {
		cfg.Server.ReadHeaderTimeout = def.Server.ReadHeaderTimeout
	}
	if cfg.Server.WriteTimeout <= 0 {
		cfg.Server.WriteTimeout = def.Server.WriteTimeout
	}
	if cfg.Server.IdleTimeout <= 0 {
		cfg.Server.IdleTimeout = def.Server.IdleTimeout
	}
	if cfg.Server.MaxHeaderBytes <= 0 {
		cfg.Server.MaxHeaderBytes = def.Server.MaxHeaderBytes
	}
	if cfg.Server.MaxUploadMB <= 0 {
		cfg.Server.MaxUploadMB = def.Server.MaxUploadMB
	}

	if cfg.VLM.TimeoutSeconds <= 0 {
		cfg.VLM.TimeoutSeconds = def.VLM.TimeoutSeconds
	}
	if cfg.VLM.MaxConcurrency <= 0 {
		cfg.VLM.MaxConcurrency = def.VLM.MaxConcurrency
	}
	if cfg.VLM.MaxRetries < 0 {
		cfg.VLM.MaxRetries = def.VLM.MaxRetries
	}
	if cfg.VLM.MaxTokens <= 0 {
		cfg.VLM.MaxTokens = def.VLM.MaxTokens
	}

	if cfg.Convert.MinImagePx <= 0 {
		cfg.Convert.MinImagePx = def.Convert.MinImagePx
	}
	if cfg.Convert.MaxImageDim <= 0 {
		cfg.Convert.MaxImageDim = def.Convert.MaxImageDim
	}
	if cfg.Convert.DedupeLogoThreshold <= 0 {
		cfg.Convert.DedupeLogoThreshold = def.Convert.DedupeLogoThreshold
	}
	if cfg.Convert.RefDocMaxBytes <= 0 {
		cfg.Convert.RefDocMaxBytes = def.Convert.RefDocMaxBytes
	}

	if strings.TrimSpace(cfg.Storage.TempDir) == "" {
		cfg.Storage.TempDir = def.Storage.TempDir
	}
	if cfg.Storage.JobTTLMinutes <= 0 {
		cfg.Storage.JobTTLMinutes = def.Storage.JobTTLMinutes
	}
	if cfg.Storage.MaxActiveJobs <= 0 {
		cfg.Storage.MaxActiveJobs = def.Storage.MaxActiveJobs
	}
}
