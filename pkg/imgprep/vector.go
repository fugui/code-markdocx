package imgprep

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var sofficeMu sync.Mutex

func IsVectorFormat(ext string, raw []byte) bool {
	e := strings.ToLower(strings.TrimSpace(ext))
	if e == ".emf" || e == ".wmf" {
		return true
	}
	// WMF Aldus Placeable Header: D7 CD C6 9A
	if len(raw) >= 4 && raw[0] == 0xD7 && raw[1] == 0xCD && raw[2] == 0xC6 && raw[3] == 0x9A {
		return true
	}
	return false
}

func DetectSoffice(configuredPath string) string {
	if p := strings.TrimSpace(configuredPath); p != "" {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("soffice"); err == nil {
		return p
	}
	return ""
}

func ConvertVectorToPNG(sofficePath, ext string, raw []byte) ([]byte, error) {
	if sofficePath == "" {
		return nil, fmt.Errorf("soffice not available")
	}
	if ext == "" {
		ext = ".emf"
	}

	sofficeMu.Lock()
	defer sofficeMu.Unlock()

	tmpDir, err := os.MkdirTemp("", "markdocx-vec-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	profileDir := filepath.Join(tmpDir, "lo_profile")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		return nil, err
	}
	inPath := filepath.Join(tmpDir, "diagram"+ext)
	if err := os.WriteFile(inPath, raw, 0600); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(
		ctx,
		sofficePath,
		"--headless",
		"-env:UserInstallation=file://"+profileDir,
		"--convert-to", "png",
		"--outdir", tmpDir,
		inPath,
	)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}

	outPath := filepath.Join(tmpDir, "diagram.png")
	pngBytes, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("read converted png: %w", err)
	}
	return pngBytes, nil
}
