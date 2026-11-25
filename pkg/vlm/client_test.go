package vlm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestConvertImageWithFencedJSONAndRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"content": "```json\n{\"category\":\"uml_diagram\",\"diagram_type\":\"sequence\",\"mermaid_code\":\"```mermaid\\nsequenceDiagram\\n  Client->>Auth: Login\\n```\",\"summary\":\"客户端向认证服务发起登录请求。\",\"confidence\":\"high\",\"recommended_skip\":false}\n```",
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{
		BaseURL:    srv.URL,
		APIKey:     "sk-test",
		Model:      "gpt-4o",
		Timeout:    5 * time.Second,
		MaxRetries: 2,
	})

	res, err := client.ConvertImage(context.Background(), ConvertImageInput{
		ImageID:     "img-001",
		ImageBytes:  []byte{0x89, 0x50, 0x4E, 0x47},
		ContentType: "image/png",
		SectionPath: "1. 登录",
		Caption:     "图 1 登录时序",
	})
	if err != nil {
		t.Fatalf("ConvertImage failed: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls due to 429 retry, got %d", calls.Load())
	}
	if res.Category != CategoryUMLDiagram || res.DiagramType != DiagramSequence {
		t.Fatalf("unexpected category/type: %s/%s", res.Category, res.DiagramType)
	}
	if res.MermaidCode != "sequenceDiagram\n  Client->>Auth: Login" {
		t.Fatalf("expected stripped mermaid code, got %q", res.MermaidCode)
	}
}

func TestRepairImageIncludesErrorAndInstruction(t *testing.T) {
	var seenUserText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) == 2 {
			seenUserText = string(req.Messages[1].Content)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": `{"category":"uml_diagram","diagram_type":"flowchart","mermaid_code":"flowchart TD\n  A[\"网关 (Gateway)\"] --> B[Auth]","summary":"修复后流程图","confidence":"high"}`}},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Model: "gpt-4o"})
	res, err := client.RepairImage(context.Background(), RepairImageInput{
		ConvertImageInput: ConvertImageInput{
			ImageID:    "img-002",
			ImageBytes: []byte{1, 2, 3},
		},
		PreviousResult: &VLMResult{MermaidCode: "flowchart TD\n  A[网关(Gateway)] --> B"},
		MermaidError:   "Parse error on line 2: unexpected token",
		Instruction:    "请用双引号包裹中文括号标签",
	})
	if err != nil {
		t.Fatalf("RepairImage failed: %v", err)
	}
	if res.DiagramType != DiagramFlowchart {
		t.Fatalf("expected flowchart, got %s", res.DiagramType)
	}
	if seenUserText == "" {
		t.Fatalf("expected repair user prompt payload")
	}
}

func TestParseCodeScreenshotAndPolishCodeWithPreservationGuard(t *testing.T) {
	rawVLM := `{
  "category": "code_screenshot",
  "code_language": "Golang",
  "code_content": "` + "```go" + `\nfunc main() {\n    println(\"hi\")\n}\n` + "```" + `",
  "summary": "打印 hi 的示例主函数。",
  "confidence": "high",
  "recommended_skip": false
}`
	res, err := ParseVLMJSON(rawVLM)
	if err != nil {
		t.Fatalf("ParseVLMJSON failed: %v", err)
	}
	if res.Category != CategoryCodeShot || res.CodeLanguage != "go" {
		t.Fatalf("unexpected category/lang: %s/%s", res.Category, res.CodeLanguage)
	}
	if res.CodeContent != "func main() {\n    println(\"hi\")\n}" {
		t.Fatalf("expected preserved indent code content, got %q", res.CodeContent)
	}

	orig := "func main() {\nx := 1\n}"
	good := "func main() {\n\tx := 1\n}"
	bad := "func main() {\n\ty := 1\n}"
	if !VerifyCodePreservation(orig, good) {
		t.Fatalf("expected whitespace-only reindent to pass preservation guard")
	}
	if VerifyCodePreservation(orig, bad) {
		t.Fatalf("expected modified identifier to fail preservation guard")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{
					"content": `{"is_code":true,"language":"go","start_line":1,"end_line":3,"formatted_code":"func main() {\n  x := 1\n}"}`,
				}},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Model: "gpt-4o"})
	polished, err := client.PolishCode(context.Background(), CodePolishInput{
		ID:    "code-001",
		Mode:  CodePolishFormat,
		Lines: []string{"func main() {", "x := 1", "}"},
	})
	if err != nil {
		t.Fatalf("PolishCode failed: %v", err)
	}
	if !polished.IsCode || polished.Language != "go" || !VerifyCodePreservation(orig, polished.FormattedCode) {
		t.Fatalf("unexpected polished result: %+v", polished)
	}
}

func TestParseLayoutPolishJSONAndClient(t *testing.T) {
	raw := "```json\n{\"ops\":[{\"op\":\"set_heading\",\"block\":1,\"level\":2},{\"op\":\"set_alert\",\"block\":3,\"alert_type\":\"warning\"},{\"op\":\"unknown_op\",\"block\":4},{\"op\":\"set_alert\",\"block\":5,\"alert_type\":\"INVALID\"}],\"summary\":\"提升 1 处标题并标注警告提示\"}\n```"
	parsed, err := ParseLayoutPolishJSON(raw)
	if err != nil {
		t.Fatalf("ParseLayoutPolishJSON failed: %v", err)
	}
	if len(parsed.Ops) != 2 {
		t.Fatalf("expected 2 valid ops after filtering, got %+v", parsed.Ops)
	}
	if parsed.Ops[1].AlertType != "WARNING" {
		t.Fatalf("expected normalized uppercase WARNING, got %q", parsed.Ops[1].AlertType)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{
					"content": `{"ops":[{"op":"set_heading","block":1,"level":2}],"summary":"已识别小节标题"}`,
				}},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Model: "gpt-4o"})
	res, err := client.PolishLayout(context.Background(), LayoutPolishInput{
		ChunkID:  "layout-01",
		DocTitle: "测试文档",
		Blocks: []LayoutBlockSummary{
			{Index: 0, Kind: "heading", Level: 1, TextPreview: "总览", CharCount: 2},
			{Index: 1, Kind: "paragraph", TextPreview: "架构边界", CharCount: 4},
		},
	})
	if err != nil {
		t.Fatalf("PolishLayout failed: %v", err)
	}
	if len(res.Ops) != 1 || res.Ops[0].Op != LayoutOpSetHeading || res.Ops[0].Block != 1 {
		t.Fatalf("unexpected PolishLayout result: %+v", res)
	}
}
