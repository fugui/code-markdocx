package imgprep

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"code-markdocx/pkg/docxparser"
)

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 50, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestPrepareImagesFiltersTinyDedupesLogoDownscalesAndFlagsEMF(t *testing.T) {
	tiny := makePNG(t, 24, 24)
	logo := makePNG(t, 120, 60)
	big := makePNG(t, 3000, 1500)

	assets := []*docxparser.ImageAsset{
		{ID: "img-001", Ext: ".png", ContentType: "image/png", RawBytes: tiny, SHA256: "h-tiny"},
		{ID: "img-002", Ext: ".png", ContentType: "image/png", RawBytes: logo, SHA256: "h-logo"},
		{ID: "img-003", Ext: ".png", ContentType: "image/png", RawBytes: logo, SHA256: "h-logo"},
		{ID: "img-004", Ext: ".png", ContentType: "image/png", RawBytes: logo, SHA256: "h-logo"},
		{ID: "img-005", Ext: ".png", ContentType: "image/png", RawBytes: big, SHA256: "h-big"},
		{ID: "img-006", Ext: ".emf", ContentType: "image/x-emf", RawBytes: []byte{1, 2, 3, 4}, SHA256: "h-emf"},
	}

	outDir := t.TempDir()
	res, err := PrepareImages(assets, PrepOptions{
		MinImagePx:          48,
		MaxImageDim:         2048,
		DedupeLogoThreshold: 3,
		SofficePath:         "/nonexistent/soffice",
		OutputDir:           outDir,
	})
	if err != nil {
		t.Fatalf("PrepareImages failed: %v", err)
	}
	if len(res) != 6 {
		t.Fatalf("expected 6 results, got %d", len(res))
	}
	if !res[0].Asset.PreSkipped {
		t.Fatalf("expected tiny icon to be pre-skipped")
	}
	if !res[1].Asset.PreSkipped || !res[2].Asset.PreSkipped || !res[3].Asset.PreSkipped {
		t.Fatalf("expected repeated logo to be pre-skipped")
	}
	if res[4].Asset.Width != 2048 || res[4].Asset.Height != 1024 {
		t.Fatalf("expected downscaled 2048x1024, got %dx%d", res[4].Asset.Width, res[4].Asset.Height)
	}
	if res[4].PreparedPath == "" {
		t.Fatalf("expected PreparedPath to be written")
	}
	// If soffice happens to be installed on PATH, DetectSoffice falls back to PATH; otherwise UnsupportedVector=true.
	if DetectSoffice("") == "" && !res[5].Asset.UnsupportedVector {
		t.Fatalf("expected UnsupportedVector=true when soffice is missing")
	}
}
