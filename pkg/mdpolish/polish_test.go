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

func TestCodeBlockRenderingDynamicFenceAndCandidates(t *testing.T) {
	doc := &docxparser.DocumentIR{
		Blocks: []docxparser.Block{
			{Kind: docxparser.BlockHeading, Level: 1, Text: "代码规范"},
			{Kind: docxparser.BlockParagraph, Text: "示例："},
			{Kind: docxparser.BlockCode, Language: "", Text: "func demo() {\nreturn\n}"},
			{Kind: docxparser.BlockParagraph, Text: "使用反引号示例："},
			{Kind: docxparser.BlockCode, Language: "markdown", Text: "```go\nfmt.Println(1)\n```"},
			{Kind: docxparser.BlockParagraph, Text: "代码如下："},
			{Kind: docxparser.BlockParagraph, Text: "a := 1"},
			{Kind: docxparser.BlockParagraph, Text: "b := a + 2"},
			{Kind: docxparser.BlockImage, ImageID: "img-code"},
		},
	}

	tasks := ExtractCodePolishTasks(doc, "", 10)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 code polish tasks (format + detect), got %d: %+v", len(tasks), tasks)
	}
	if tasks[0].Input.Mode != vlm.CodePolishFormat || tasks[1].Input.Mode != vlm.CodePolishDetect {
		t.Fatalf("unexpected task modes: %+v", tasks)
	}

	results := map[string]*vlm.CodePolishResult{
		tasks[0].Input.ID: {
			IsCode:        true,
			Language:      "go",
			FormattedCode: "func demo() {\n\treturn\n}",
		},
		tasks[1].Input.ID: {
			IsCode:        true,
			Language:      "go",
			StartLine:     1,
			EndLine:       2,
			FormattedCode: "a := 1\nb := a + 999", // Hallucinated modification must be blocked by guard!
		},
	}
	applied := ApplyCodePolishResults(doc, tasks, results)
	if applied != 2 {
		t.Fatalf("expected 2 applied tasks, got %d", applied)
	}
	if doc.Blocks[2].Text != "func demo() {\n\treturn\n}" || doc.Blocks[2].Language != "go" {
		t.Fatalf("expected formatted code block, got %+v", doc.Blocks[2])
	}
	// Block 6+7 merged to Block 6, preserving original "b := a + 2" despite hallucination in FormattedCode
	if doc.Blocks[6].Kind != docxparser.BlockCode || !strings.Contains(doc.Blocks[6].Text, "b := a + 2") || strings.Contains(doc.Blocks[6].Text, "999") {
		t.Fatalf("expected hallucination guard to keep original text, got %+v", doc.Blocks[6])
	}

	states := map[string]*ImageState{
		"img-code": {
			ImageID:      "img-code",
			Status:       StatusDone,
			Category:     vlm.CategoryCodeShot,
			CodeLanguage: "python",
			CodeContent:  "print('ok')",
			Summary:      "Python 打印脚本",
		},
	}
	md := Assemble(doc, states)
	if !strings.Contains(md, "````markdown\n```go\nfmt.Println(1)\n```\n````") {
		t.Fatalf("expected 4-backtick dynamic fence for nested backticks:\n%s", md)
	}
	if !strings.Contains(md, "```python\nprint('ok')\n```") {
		t.Fatalf("expected code_screenshot rendered as fenced block:\n%s", md)
	}
	rep := Analyze(md, []*ImageState{states["img-code"]}, 524288)
	if rep.CodeBlockCount != 4 {
		t.Fatalf("expected 4 code blocks in preflight, got %d\n%s", rep.CodeBlockCount, md)
	}
}
