package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigMissingReturnsDefaults(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != ":8105" {
		t.Fatalf("expected default port :8105, got %s", cfg.Server.Port)
	}
	if cfg.Convert.RefDocMaxBytes != 524288 {
		t.Fatalf("expected default 524288, got %d", cfg.Convert.RefDocMaxBytes)
	}
	if cfg.VLM.IsConfigured() {
		t.Fatalf("empty VLM should not be configured")
	}
}

func TestLoadConfigParsesValuesAndNormalizesPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  port: "9105"
  max_upload_mb: 25
vlm:
  base_url: "http://127.0.0.1:8088/v1"
  api_key: "sk-test"
  model: "qwen2.5-vl"
  max_concurrency: 5
convert:
  strip_cover: true
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Server.Port != ":9105" {
		t.Fatalf("expected :9105, got %s", cfg.Server.Port)
	}
	if cfg.Server.MaxUploadMB != 25 {
		t.Fatalf("expected max_upload_mb 25, got %d", cfg.Server.MaxUploadMB)
	}
	if !cfg.VLM.IsConfigured() {
		t.Fatalf("expected VLM to be configured")
	}
	if cfg.VLM.MaxConcurrency != 5 {
		t.Fatalf("expected max_concurrency 5, got %d", cfg.VLM.MaxConcurrency)
	}
	if !cfg.Convert.StripCover {
		t.Fatalf("expected strip_cover true")
	}
	if !cfg.Convert.StripTOC {
		t.Fatalf("expected strip_toc default true")
	}
}

func TestVLMPlaceholderKeyNotConfigured(t *testing.T) {
	cfg := DefaultConfig()
	cfg.VLM.BaseURL = "https://api.openai.com/v1"
	cfg.VLM.Model = "gpt-4o"
	cfg.VLM.APIKey = "YOUR_VLM_API_KEY"
	if cfg.VLM.IsConfigured() {
		t.Fatalf("placeholder API key must report IsConfigured=false")
	}
}
