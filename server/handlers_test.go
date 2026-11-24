package server_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"code-markdocx/config"
	"code-markdocx/pkg/vlm"
	"code-markdocx/server"

	"github.com/gin-gonic/gin"
)

func buildIntegrationDocx(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 20, G: 80, B: 180, A: 255})
		}
	}
	var imgBuf bytes.Buffer
	if err := png.Encode(&imgBuf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeStr := func(name, body string) {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	writeStr("word/styles.xml", `<?xml version="1.0" encoding="UTF-8"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/></w:style><w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/></w:style></w:styles>`)
	writeStr("word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/diag.png"/></Relationships>`)
	writeStr("word/document.xml", `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><w:body><w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>1. 系统设计</w:t></w:r></w:p><w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>1.1 登录流程</w:t></w:r></w:p><w:p><w:r><w:drawing><a:blip r:embed="rId1"/></w:drawing></w:r></w:p><w:p><w:r><w:t>图 1 登录时序图</w:t></w:r></w:p></w:body></w:document>`)
	iw, _ := zw.Create("word/media/diag.png")
	_, _ = iw.Write(imgBuf.Bytes())
	_ = zw.Close()
	return buf.Bytes()
}

func TestEndToEndJobFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	vlmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{
					"content": `{"category":"uml_diagram","diagram_type":"sequence","mermaid_code":"sequenceDiagram\n  User->>Gateway: Login","summary":"用户经网关登录","confidence":"high"}`,
				}},
			},
		})
	}))
	defer vlmSrv.Close()

	cfg := config.DefaultConfig()
	cfg.Storage.TempDir = t.TempDir()
	cfg.VLM.BaseURL = vlmSrv.URL
	cfg.VLM.APIKey = "sk-mock"
	cfg.VLM.Model = "mock-vl"

	store, err := server.NewJobStore(cfg.Storage.TempDir, 60, 10, 524288)
	if err != nil {
		t.Fatalf("NewJobStore: %v", err)
	}
	defer store.Close()

	vlmClient := vlm.NewClient(vlm.ClientConfig{
		BaseURL: cfg.VLM.BaseURL,
		APIKey:  cfg.VLM.APIKey,
		Model:   cfg.VLM.Model,
		Timeout: 5 * time.Second,
	})

	r := gin.New()
	server.RegisterRoutes(r, server.NewHandlers(cfg, store, vlmClient))

	// 1. Upload .docx
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "arch.docx")
	_, _ = fw.Write(buildIntegrationDocx(t))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/jobs", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created server.JobDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create resp: %v", err)
	}

	// 2. Poll until done
	var detail server.JobDetail
	for i := 0; i < 30; i++ {
		getReq := httptest.NewRequest(http.MethodGet, "/api/jobs/"+created.ID, nil)
		getRec := httptest.NewRecorder()
		r.ServeHTTP(getRec, getReq)
		_ = json.Unmarshal(getRec.Body.Bytes(), &detail)
		if detail.Status == server.JobDone {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if detail.Status != server.JobDone {
		t.Fatalf("expected job done, got %s", detail.Status)
	}
	if !strings.Contains(detail.Markdown, "sequenceDiagram") {
		t.Fatalf("expected mermaid in markdown: %s", detail.Markdown)
	}

	// 3. Get image
	imgReq := httptest.NewRequest(http.MethodGet, "/api/jobs/"+created.ID+"/images/img-001", nil)
	imgRec := httptest.NewRecorder()
	r.ServeHTTP(imgRec, imgReq)
	if imgRec.Code != http.StatusOK || imgRec.Body.Len() == 0 {
		t.Fatalf("expected image 200, got %d", imgRec.Code)
	}

	// 4. Patch image
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/jobs/"+created.ID+"/images/img-001", strings.NewReader(`{"mermaid_code":"sequenceDiagram\n  User->>Auth: Token"}`))
	patchReq.Header.Set("Content-Type", "application/json")
	patchRec := httptest.NewRecorder()
	r.ServeHTTP(patchRec, patchReq)
	var patchResp struct {
		Markdown string `json:"markdown"`
	}
	_ = json.Unmarshal(patchRec.Body.Bytes(), &patchResp)
	if patchRec.Code != http.StatusOK || !strings.Contains(patchResp.Markdown, "User->>Auth: Token") {
		t.Fatalf("patch failed: %d %s", patchRec.Code, patchRec.Body.String())
	}

	// 5. Retry image with mermaid_error
	retryReq := httptest.NewRequest(http.MethodPost, "/api/jobs/"+created.ID+"/images/img-001/retry", strings.NewReader(`{"mermaid_error":"syntax err","instruction":"fix"}`))
	retryReq.Header.Set("Content-Type", "application/json")
	retryRec := httptest.NewRecorder()
	r.ServeHTTP(retryRec, retryReq)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("retry failed: %d %s", retryRec.Code, retryRec.Body.String())
	}

	// 6. Export .md and split .zip
	expReq := httptest.NewRequest(http.MethodGet, "/api/jobs/"+created.ID+"/export", nil)
	expRec := httptest.NewRecorder()
	r.ServeHTTP(expRec, expReq)
	if expRec.Code != http.StatusOK || !strings.Contains(expRec.Body.String(), "```mermaid") {
		t.Fatalf("export md failed: %d", expRec.Code)
	}

	zipReq := httptest.NewRequest(http.MethodGet, "/api/jobs/"+created.ID+"/export?split=true", nil)
	zipRec := httptest.NewRecorder()
	r.ServeHTTP(zipRec, zipReq)
	if zipRec.Code != http.StatusOK || zipRec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export zip failed: %d %s", zipRec.Code, zipRec.Header().Get("Content-Type"))
	}

	// 7. Delete job
	delReq := httptest.NewRequest(http.MethodDelete, "/api/jobs/"+created.ID, nil)
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete failed: %d", delRec.Code)
	}
}

func TestKeepOriginalAndBundleExport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig()
	cfg.Storage.TempDir = t.TempDir()
	// Leave VLM unconfigured so img-001 initially fails

	store, err := server.NewJobStore(cfg.Storage.TempDir, 60, 10, 524288)
	if err != nil {
		t.Fatalf("NewJobStore: %v", err)
	}
	defer store.Close()

	r := gin.New()
	server.RegisterRoutes(r, server.NewHandlers(cfg, store, nil))

	upload := func(extraFields map[string]string) server.JobDetail {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", "arch.docx")
		_, _ = fw.Write(buildIntegrationDocx(t))
		for k, v := range extraFields {
			_ = mw.WriteField(k, v)
		}
		_ = mw.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/jobs", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
		}
		var detail server.JobDetail
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		return detail
	}

	// 1. Upload without VLM -> img-001 fails, then batch keep-originals converts it
	job1 := upload(nil)
	if job1.Preflight.ImageStats.Failed != 1 {
		t.Fatalf("expected 1 failed image without VLM, got %+v", job1.Preflight.ImageStats)
	}

	batchReq := httptest.NewRequest(http.MethodPost, "/api/jobs/"+job1.ID+"/images/keep-originals", strings.NewReader(`{"keep_original":true}`))
	batchReq.Header.Set("Content-Type", "application/json")
	batchRec := httptest.NewRecorder()
	r.ServeHTTP(batchRec, batchReq)
	if batchRec.Code != http.StatusOK {
		t.Fatalf("batch keep-originals failed: %d %s", batchRec.Code, batchRec.Body.String())
	}
	var batchResp struct {
		Markdown  string `json:"markdown"`
		Updated   int    `json:"updated_count"`
		Preflight struct {
			ImageStats struct {
				Failed       int `json:"failed"`
				KeptOriginal int `json:"kept_original"`
			} `json:"image_stats"`
		} `json:"preflight"`
	}
	if err := json.Unmarshal(batchRec.Body.Bytes(), &batchResp); err != nil {
		t.Fatalf("decode batch resp: %v", err)
	}
	if batchResp.Updated != 1 || batchResp.Preflight.ImageStats.Failed != 0 || batchResp.Preflight.ImageStats.KeptOriginal != 1 {
		t.Fatalf("unexpected batch resp: %+v", batchResp)
	}
	if !strings.Contains(batchResp.Markdown, "(assets/img-001.png)") {
		t.Fatalf("expected asset link in markdown:\n%s", batchResp.Markdown)
	}

	// 2. Patch toggle off and back on
	patchOff := httptest.NewRequest(http.MethodPatch, "/api/jobs/"+job1.ID+"/images/img-001", strings.NewReader(`{"keep_original":false}`))
	patchOff.Header.Set("Content-Type", "application/json")
	patchOffRec := httptest.NewRecorder()
	r.ServeHTTP(patchOffRec, patchOff)
	if patchOffRec.Code != http.StatusOK {
		t.Fatalf("patch off failed: %d", patchOffRec.Code)
	}
	patchOn := httptest.NewRequest(http.MethodPatch, "/api/jobs/"+job1.ID+"/images/img-001", strings.NewReader(`{"keep_original":true}`))
	patchOn.Header.Set("Content-Type", "application/json")
	patchOnRec := httptest.NewRecorder()
	r.ServeHTTP(patchOnRec, patchOn)
	if patchOnRec.Code != http.StatusOK {
		t.Fatalf("patch on failed: %d", patchOnRec.Code)
	}

	// 3. Export bundle ZIP (single md + assets/img-001.png)
	checkZip := func(url string, expectedMD bool) {
		zReq := httptest.NewRequest(http.MethodGet, url, nil)
		zRec := httptest.NewRecorder()
		r.ServeHTTP(zRec, zReq)
		if zRec.Code != http.StatusOK || zRec.Header().Get("Content-Type") != "application/zip" {
			t.Fatalf("bundle export %s failed: %d %s", url, zRec.Code, zRec.Header().Get("Content-Type"))
		}
		zr, err := zip.NewReader(bytes.NewReader(zRec.Body.Bytes()), int64(zRec.Body.Len()))
		if err != nil {
			t.Fatalf("open zip %s: %v", url, err)
		}
		entries := map[string][]byte{}
		for _, f := range zr.File {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			entries[f.Name] = b
		}
		if len(entries["assets/img-001.png"]) == 0 {
			t.Fatalf("expected non-empty assets/img-001.png in %s, entries=%v", url, keysOf(entries))
		}
		if expectedMD {
			if !strings.Contains(string(entries["arch.md"]), "(assets/img-001.png)") {
				t.Fatalf("expected arch.md with asset ref in %s", url)
			}
		} else {
			hasMD := false
			for name, body := range entries {
				if strings.HasSuffix(name, ".md") && strings.Contains(string(body), "(assets/img-001.png)") {
					hasMD = true
				}
			}
			if !hasMD {
				t.Fatalf("expected split .md with asset ref in %s, entries=%v", url, keysOf(entries))
			}
		}
	}
	checkZip("/api/jobs/"+job1.ID+"/export?bundle=true", true)
	checkZip("/api/jobs/"+job1.ID+"/export?bundle=true&split=true", false)

	// 4. Upload with skip_vlm=true & keep_unconverted_originals=true
	job2 := upload(map[string]string{
		"skip_vlm":                   "true",
		"keep_unconverted_originals": "true",
	})
	if job2.Preflight.ImageStats.Failed != 0 || job2.Preflight.ImageStats.KeptOriginal != 1 {
		t.Fatalf("expected kept_original=1 and failed=0, got %+v", job2.Preflight.ImageStats)
	}
	if !strings.Contains(job2.Markdown, "(assets/img-001.png)") {
		t.Fatalf("expected auto kept original link in markdown:\n%s", job2.Markdown)
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
