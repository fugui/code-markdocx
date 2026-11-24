package mdpolish

import (
	"strings"
	"testing"

	"code-markdocx/pkg/docxparser"
	"code-markdocx/pkg/vlm"
)

func TestAssembleAndPreflight(t *testing.T) {
	doc := &docxparser.DocumentIR{
		Title: "系统架构",
		Blocks: []docxparser.Block{
			{Kind: docxparser.BlockHeading, Level: 1, Text: "1. 系统架构"},
			{Kind: docxparser.BlockParagraph, Text: "本系统采用网关与认证中心分离架构。"},
			{Kind: docxparser.BlockImage, ImageID: "img-001"},
			{Kind: docxparser.BlockImage, ImageID: "img-002"},
			{Kind: docxparser.BlockImage, ImageID: "img-003"},
		},
	}

	states := map[string]*ImageState{
		"img-001": {
			ImageID:     "img-001",
			Caption:     "图 1-1 登录时序",
			Status:      StatusDone,
			Category:    vlm.CategoryUMLDiagram,
			DiagramType: vlm.DiagramSequence,
			MermaidCode: "sequenceDiagram\n  Client->>Auth: Login",
			Summary:     "客户端向认证中心请求登录。",
			Confidence:  vlm.ConfidenceHigh,
		},
		"img-002": {
			ImageID: "img-002",
			Status:  StatusSkipped,
			Skipped: true,
		},
		"img-003": {
			ImageID:           "img-003",
			OrigFilename:      "arch.emf",
			Status:            StatusUnsupportedVector,
			UnsupportedVector: true,
			VectorWarning:     "需 LibreOffice",
		},
	}

	md := Assemble(doc, states)
	if !strings.Contains(md, "```mermaid\nsequenceDiagram") {
		t.Fatalf("expected mermaid block in assembled markdown:\n%s", md)
	}
	if strings.Contains(md, "img-002") {
		t.Fatalf("skipped image should not appear in markdown")
	}
	if !strings.Contains(md, "未转换矢量图") {
		t.Fatalf("expected unsupported vector callout")
	}

	rep := Analyze(md+"\napi_key = \"sk-1234567890abcdef123456\"", []*ImageState{states["img-001"], states["img-002"], states["img-003"]}, 524288)
	if rep.MermaidBlockCount != 1 {
		t.Fatalf("expected 1 mermaid block, got %d", rep.MermaidBlockCount)
	}
	if rep.HasInlineBase64 {
		t.Fatalf("did not expect base64")
	}
	if len(rep.SecretWarnings) == 0 {
		t.Fatalf("expected secret warning for sk- token")
	}
	if rep.ImageStats.Done != 1 || rep.ImageStats.Skipped != 1 || rep.ImageStats.UnsupportedVector != 1 {
		t.Fatalf("unexpected image stats: %+v", rep.ImageStats)
	}
}

func TestSplitByH2RespectsFences(t *testing.T) {
	md := "# Title\n\n## 第一节\n内容A\n```markdown\n## 围栏内的假标题\n```\n\n## 第二节\n" + strings.Repeat("文字内容测试", 40) + "\n"
	chunks := SplitByH2(md, "arch.docx", 180)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Content, "## 围栏内的假标题") {
		t.Fatalf("fenced heading must stay inside first chunk: %q", chunks[0].Content)
	}
}

func TestKeepOriginalRenderingAndHelpers(t *testing.T) {
	doc := &docxparser.DocumentIR{
		Blocks: []docxparser.Block{
			{Kind: docxparser.BlockHeading, Level: 1, Text: "系统部署"},
			{Kind: docxparser.BlockImage, ImageID: "img-001"},
			{Kind: docxparser.BlockImage, ImageID: "img-002"},
		},
	}
	states := map[string]*ImageState{
		"img-001": {
			ImageID:       "img-001",
			OrigFilename:  "deploy[v2].png",
			Caption:       "图 2-1 部署[拓扑]",
			Summary:       "双活部署拓扑原图。",
			Status:        StatusDone,
			KeepOriginal:  true,
			HasImageFile:  true,
			AssetFilename: "img-001.png",
			Confidence:    vlm.ConfidenceLow, // Kept original must not be flagged as low_confidence
		},
		"img-002": {
			ImageID:       "img-002",
			OrigFilename:  "ui.png",
			Status:        StatusSkipped,
			Skipped:       true,
			HasImageFile:  true,
			AssetFilename: "img-002.png",
		},
	}

	if IsUnconvertedToStructured(states["img-001"]) {
		t.Fatalf("img-001 already keeps original, should not be treated as unconverted")
	}
	if !IsUnconvertedToStructured(states["img-002"]) {
		t.Fatalf("skipped raster img-002 should be eligible for keeping original")
	}

	md := Assemble(doc, states)
	if !strings.Contains(md, `![图 2-1 部署\[拓扑\]](assets/img-001.png)`) {
		t.Fatalf("expected escaped alt and assets path in markdown:\n%s", md)
	}
	if !strings.Contains(md, "> 图表说明：双活部署拓扑原图。") {
		t.Fatalf("expected summary quote in markdown:\n%s", md)
	}

	rep := Analyze(md, []*ImageState{states["img-001"], states["img-002"]}, 524288)
	if rep.ImageStats.KeptOriginal != 1 {
		t.Fatalf("expected kept_original=1, got %d", rep.ImageStats.KeptOriginal)
	}
	if rep.ImageStats.LowConfidence != 0 {
		t.Fatalf("kept_original image should not count as low_confidence, got %d", rep.ImageStats.LowConfidence)
	}
}
