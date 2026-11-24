package docxparser

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const (
	MaxZipFiles            = 2000
	MaxSingleEntryBytes    = 50 * 1024 * 1024  // 50 MB
	MaxTotalUncompressByte = 200 * 1024 * 1024 // 200 MB
)

type SafeZip struct {
	Reader *zip.Reader
	files  map[string]*zip.File
}

func OpenSafeZipFile(filePath string) (*SafeZip, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read docx file: %w", err)
	}
	return OpenSafeZip(data)
}

func OpenSafeZip(data []byte) (*SafeZip, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty docx payload")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid docx zip archive: %w", err)
	}
	if len(zr.File) > MaxZipFiles {
		return nil, fmt.Errorf("docx zip exceeds max entry count (%d > %d)", len(zr.File), MaxZipFiles)
	}

	files := make(map[string]*zip.File, len(zr.File))
	var totalDeclared uint64
	for _, f := range zr.File {
		clean, err := sanitizeZipPath(f.Name)
		if err != nil {
			return nil, err
		}
		if f.UncompressedSize64 > MaxSingleEntryBytes {
			return nil, fmt.Errorf("zip entry %s exceeds single entry limit", clean)
		}
		totalDeclared += f.UncompressedSize64
		if totalDeclared > MaxTotalUncompressByte {
			return nil, fmt.Errorf("docx zip exceeds total uncompressed size limit")
		}
		files[clean] = f
	}
	if _, ok := files["word/document.xml"]; !ok {
		return nil, fmt.Errorf("not a valid .docx file: missing word/document.xml")
	}
	return &SafeZip{Reader: zr, files: files}, nil
}

func sanitizeZipPath(name string) (string, error) {
	if strings.Contains(name, "\\") {
		name = strings.ReplaceAll(name, "\\", "/")
	}
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe absolute zip entry path: %s", name)
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("unsafe zip traversal path: %s", name)
	}
	return clean, nil
}

func (s *SafeZip) Has(entryPath string) bool {
	_, ok := s.files[path.Clean(entryPath)]
	return ok
}

func (s *SafeZip) ReadEntry(entryPath string) ([]byte, error) {
	f, ok := s.files[path.Clean(entryPath)]
	if !ok {
		return nil, fmt.Errorf("zip entry not found: %s", entryPath)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("open zip entry %s: %w", entryPath, err)
	}
	defer rc.Close()

	limited := io.LimitReader(rc, MaxSingleEntryBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read zip entry %s: %w", entryPath, err)
	}
	if int64(len(data)) > MaxSingleEntryBytes {
		return nil, fmt.Errorf("zip entry %s exceeded uncompressed byte limit", entryPath)
	}
	return data, nil
}
