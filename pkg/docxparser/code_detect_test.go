package docxparser

import (
	"strings"
	"testing"
)

func TestParseIDEPastedGoCodeAndFragmentedBold(t *testing.T) {
	stylesXML := `<?xml version="1.0" encoding="UTF-8"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:style w:type="paragraph" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
</w:styles>`

	// Reproduces 12a0435c: lead-in, fragmented multi-color monospace Go runs with NBSP & blank paragraph,
	// fragmented bold title (**202****5**...), and a normal 6-col table.
	docXML := `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>代码如下：</w:t></w:r></w:p>
    <w:p>
      <w:r><w:rPr><w:rFonts w:ascii="Consolas"/><w:color w:val="0000FF"/></w:rPr><w:t>func</w:t></w:r>
      <w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t> GenerateToken(user models.User) (</w:t></w:r>
      <w:r><w:rPr><w:rFonts w:ascii="Consolas"/><w:color w:val="2B91AF"/></w:rPr><w:t>string</w:t></w:r>
      <w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t>, </w:t></w:r>
      <w:r><w:rPr><w:rFonts w:ascii="Consolas"/><w:color w:val="2B91AF"/></w:rPr><w:t>error</w:t></w:r>
      <w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t>) {</w:t></w:r>
    </w:p>
    <w:p><w:pPr><w:ind w:left="420"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t>secret := getJWTSecret()</w:t></w:r></w:p>
    <w:p><w:pPr><w:ind w:left="420"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t>claims := &amp;Claims{</w:t></w:r></w:p>
    <w:p><w:pPr><w:ind w:left="840"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t>UserID:` + "\u00a0\u00a0\u00a0\u00a0" + `user.ID,</w:t></w:r></w:p>
    <w:p><w:r><w:t>// 校验并签发</w:t></w:r></w:p>
    <w:p><w:pPr><w:ind w:left="420"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t>}</w:t></w:r></w:p>
    <w:p><w:pPr><w:ind w:left="420"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t>return token.SignedString(secret)</w:t></w:r></w:p>
    <w:p><w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t>}</w:t></w:r></w:p>
    <w:p/>
    <w:p>
      <w:r><w:rPr><w:rFonts w:ascii="Consolas"/><w:color w:val="0000FF"/></w:rPr><w:t>func</w:t></w:r>
      <w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t> ParseToken(tokenString string) (*Claims, error) {</w:t></w:r>
    </w:p>
    <w:p><w:pPr><w:ind w:left="420"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t>return commonAuth.ParseToken(tokenString)</w:t></w:r></w:p>
    <w:p><w:r><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr><w:t>}</w:t></w:r></w:p>
    <w:p>
      <w:r><w:rPr><w:b/></w:rPr><w:t>202</w:t></w:r>
      <w:r><w:rPr><w:b/></w:rPr><w:t>5</w:t></w:r>
      <w:r><w:rPr><w:b/></w:rPr><w:t>-2026学年清单</w:t></w:r>
    </w:p>
    <w:tbl>
      <w:tr><w:tc><w:p><w:r><w:t>序号</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>学科</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>语文</w:t></w:r></w:p></w:tc></w:tr>
    </w:tbl>
  </w:body>
</w:document>`

	data := buildTestDocx(t, map[string]string{
		"word/styles.xml":   stylesXML,
		"word/document.xml": docXML,
	}, nil)

	ir, err := ParseBytes(data, ParseOptions{})
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	var codeBlocks []Block
	var paras []string
	var tables int
	for _, b := range ir.Blocks {
		switch b.Kind {
		case BlockCode:
			codeBlocks = append(codeBlocks, b)
		case BlockParagraph:
			paras = append(paras, b.Text)
		case BlockTable:
			tables++
		}
	}

	if len(codeBlocks) != 1 {
		t.Fatalf("expected 1 merged code block, got %d (%+v)", len(codeBlocks), ir.Blocks)
	}
	cb := codeBlocks[0]
	if cb.Language != "go" {
		t.Fatalf("expected language go, got %q", cb.Language)
	}
	if strings.Contains(cb.Text, "`func`") || strings.Contains(cb.Text, "\u00a0") {
		t.Fatalf("code block should not contain inline backticks or NBSP: %q", cb.Text)
	}
	if !strings.Contains(cb.Text, "  secret := getJWTSecret()") ||
		!strings.Contains(cb.Text, "    UserID:    user.ID,") ||
		!strings.Contains(cb.Text, "// 校验并签发") ||
		!strings.Contains(cb.Text, "func ParseToken") {
		t.Fatalf("unexpected code block content:\n%s", cb.Text)
	}
	foundMergedBold := false
	for _, p := range paras {
		if p == "**2025-2026学年清单**" {
			foundMergedBold = true
		}
	}
	if !foundMergedBold {
		t.Fatalf("expected merged bold paragraph, got %+v", paras)
	}
	if tables != 1 {
		t.Fatalf("expected 1 normal table, got %d", tables)
	}
}

func TestCodeStylesSoftBreaksTablesAndHeuristics(t *testing.T) {
	stylesXML := `<?xml version="1.0" encoding="UTF-8"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:style w:type="paragraph" w:styleId="CodeBlock">
    <w:name w:val="Source Code"/>
    <w:pPr><w:shd w:val="clear" w:fill="F5F5F5"/></w:pPr>
    <w:rPr><w:rFonts w:ascii="JetBrains Mono"/></w:rPr>
  </w:style>
</w:styles>`

	docXML := `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:pPr><w:pStyle w:val="CodeBlock"/></w:pPr>
      <w:r><w:t>def greet(name):</w:t><w:br/><w:t>    print("hi", name)</w:t></w:r>
    </w:p>
    <w:tbl>
      <w:tr><w:tc>
        <w:tcPr><w:shd w:val="clear" w:fill="F2F2F2"/></w:tcPr>
        <w:p><w:r><w:t>SELECT id, name</w:t></w:r></w:p>
        <w:p><w:r><w:t>FROM users</w:t></w:r></w:p>
        <w:p><w:r><w:t>WHERE active = 1;</w:t></w:r></w:p>
      </w:tc></w:tr>
    </w:tbl>
    <w:tbl>
      <w:tr><w:tc>
        <w:tcPr><w:shd w:val="clear" w:fill="FFF8E1"/></w:tcPr>
        <w:p><w:r><w:t>注意：请勿在生产环境开启调试开关，否则会造成安全风险。</w:t></w:r></w:p>
      </w:tc></w:tr>
    </w:tbl>
    <w:tbl>
      <w:tr>
        <w:tc><w:p><w:r><w:t>1</w:t></w:r><w:br/><w:r><w:t>2</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>curl -s https://api.example.com/v1/health</w:t></w:r><w:br/><w:r><w:t>docker ps -a</w:t></w:r></w:p></w:tc>
      </w:tr>
    </w:tbl>
    <w:p><w:r><w:t>{</w:t></w:r></w:p>
    <w:p><w:r><w:t>  "service": "auth",</w:t></w:r></w:p>
    <w:p><w:r><w:t>  "port": 8080</w:t></w:r></w:p>
    <w:p><w:r><w:t>}</w:t></w:r></w:p>
    <w:p><w:r><w:t>This is a normal English sentence describing the auth service.</w:t></w:r></w:p>
  </w:body>
</w:document>`

	data := buildTestDocx(t, map[string]string{
		"word/styles.xml":   stylesXML,
		"word/document.xml": docXML,
	}, nil)

	ir, err := ParseBytes(data, ParseOptions{})
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	var codes []Block
	var tables []Block
	var paras []string
	for _, b := range ir.Blocks {
		switch b.Kind {
		case BlockCode:
			codes = append(codes, b)
		case BlockTable:
			tables = append(tables, b)
		case BlockParagraph:
			paras = append(paras, b.Text)
		}
	}

	if len(codes) != 4 {
		t.Fatalf("expected 4 code blocks (python, sql, bash, json), got %d: %+v", len(codes), codes)
	}
	if codes[0].Language != "python" || !strings.Contains(codes[0].Text, "    print(\"hi\", name)") {
		t.Fatalf("unexpected python code block: %+v", codes[0])
	}
	if codes[1].Language != "sql" {
		t.Fatalf("expected sql code block, got %+v", codes[1])
	}
	if codes[2].Language != "bash" || strings.Contains(codes[2].Text, "\n1\n") {
		t.Fatalf("expected bash code block with line numbers stripped, got %+v", codes[2])
	}
	if codes[3].Language != "json" {
		t.Fatalf("expected json heuristic code block, got %+v", codes[3])
	}
	if len(tables) != 1 {
		t.Fatalf("expected Chinese callout 1x1 table to remain table, got %d tables", len(tables))
	}
	if len(paras) != 1 || !strings.Contains(paras[0], "normal English sentence") {
		t.Fatalf("expected English prose to remain paragraph, got %+v", paras)
	}
}

func TestDetectCodeLanguageCoverage(t *testing.T) {
	cases := []struct {
		name string
		code string
		hint string
		want string
	}{
		{"go", "package main\nfunc main() {\n\tx := 1\n}", "", "go"},
		{"json", `{"ok": true, "items": [1, 2]}`, "", "json"},
		{"sql", "SELECT id FROM orders WHERE status = 'PAID';", "", "sql"},
		{"python", "def handler(event):\n    if event is None:\n        return None", "", "python"},
		{"yaml", "server:\n  port: 8080\n  mode: prod", "", "yaml"},
		{"java", "public class OrderService {\n  private final Repo repo;\n}", "", "java"},
		{"hint", "foo = bar", "以下为 TypeScript 示例：", "typescript"},
		{"unknown", "alpha beta\ngamma delta", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectCodeLanguage(tc.code, tc.hint); got != tc.want {
				t.Fatalf("DetectCodeLanguage() = %q, want %q", got, tc.want)
			}
		})
	}
}
