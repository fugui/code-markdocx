package docxparser

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func buildTestDocx(t *testing.T, entries map[string]string, binaryEntries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	for name, body := range binaryEntries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create binary zip entry %s: %v", name, err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("write binary zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestParseDocxHeadingsListsTablesAndImages(t *testing.T) {
	stylesXML := `<?xml version="1.0" encoding="UTF-8"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>
  <w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="标题 2"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style>
  <w:style w:type="paragraph" w:styleId="TOC1"><w:name w:val="toc 1"/></w:style>
</w:styles>`

	numberingXML := `<?xml version="1.0" encoding="UTF-8"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/></w:lvl></w:abstractNum>
  <w:num w:numId="10"><w:abstractNumId w:val="1"/></w:num>
</w:numbering>`

	relsXML := `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdImg1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/seq.png"/>
</Relationships>`

	docXML := `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
            xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <w:body>
    <w:tbl>
      <w:tr><w:tc><w:p><w:r><w:t>版本</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>修订人</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>日期</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>v1.0</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>张三</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>2026-01-01</w:t></w:r></w:p></w:tc></w:tr>
    </w:tbl>
    <w:p><w:pPr><w:pStyle w:val="TOC1"/></w:pPr><w:r><w:t>1. 系统架构 ..... 3</w:t></w:r></w:p>
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>1. 系统架构</w:t></w:r></w:p>
    <w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>1.1 登录时序</w:t></w:r></w:p>
    <w:p><w:r><w:t>客户端发起 </w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:t>OAuth2</w:t></w:r><w:r><w:t> 授权请求。</w:t></w:r></w:p>
    <w:p><w:r><w:drawing><a:blip r:embed="rIdImg1"/></w:drawing></w:r></w:p>
    <w:p><w:r><w:t>图 1-1 用户登录时序图</w:t></w:r></w:p>
    <w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="10"/></w:numPr></w:pPr><w:r><w:t>校验 JWT 签名</w:t></w:r></w:p>
    <w:tbl>
      <w:tr><w:tc><w:p><w:r><w:t>组件</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>职责</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>Gateway</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>路由转发</w:t></w:r></w:p></w:tc></w:tr>
    </w:tbl>
  </w:body>
</w:document>`

	data := buildTestDocx(t, map[string]string{
		"word/styles.xml":              stylesXML,
		"word/numbering.xml":           numberingXML,
		"word/_rels/document.xml.rels": relsXML,
		"word/document.xml":            docXML,
	}, map[string][]byte{
		"word/media/seq.png": []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
	})

	ir, err := ParseBytes(data, ParseOptions{StripTOC: true, StripRevisionTable: true})
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if ir.NoiseStats.StrippedRevisionTables != 1 {
		t.Fatalf("expected 1 stripped revision table, got %d", ir.NoiseStats.StrippedRevisionTables)
	}
	if ir.NoiseStats.StrippedTOCBlocks != 1 {
		t.Fatalf("expected 1 stripped TOC block, got %d", ir.NoiseStats.StrippedTOCBlocks)
	}
	if len(ir.Images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(ir.Images))
	}
	img := ir.Images[0]
	if img.SectionPath != "1. 系统架构 > 1.1 登录时序" {
		t.Fatalf("unexpected section path: %q", img.SectionPath)
	}
	if img.Caption != "图 1-1 用户登录时序图" {
		t.Fatalf("unexpected caption: %q", img.Caption)
	}
	if !strings.Contains(img.ContextBefore, "**OAuth2**") {
		t.Fatalf("expected bold context before image, got %q", img.ContextBefore)
	}
	var foundOrderedList, foundTable bool
	for _, b := range ir.Blocks {
		if b.Kind == BlockListItem && b.Ordered {
			foundOrderedList = true
		}
		if b.Kind == BlockTable && len(b.TableRows) == 2 {
			foundTable = true
		}
	}
	if !foundOrderedList || !foundTable {
		t.Fatalf("expected ordered list=%v and table=%v", foundOrderedList, foundTable)
	}
}

func TestOpenSafeZipRejectsTraversal(t *testing.T) {
	data := buildTestDocx(t, map[string]string{
		"word/document.xml": "<w:document/>",
		"../evil.txt":       "bad",
	}, nil)
	if _, err := OpenSafeZip(data); err == nil {
		t.Fatalf("expected path traversal error")
	}
}
