package cli

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code-markdocx/config"
	"code-markdocx/pkg/docxparser"
	"code-markdocx/pkg/imgprep"
	"code-markdocx/pkg/mdpolish"
	"code-markdocx/pkg/vlm"
)

func RunConvert(args []string) int {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	cfgPath := fs.String("config", "config.yaml", "配置文件路径")
	outPath := fs.String("o", "", "输出 Markdown 文件路径（默认同目录同名 .md）")
	glossary := fs.String("glossary", "", "可选项目术语提示")
	noVLM := fs.Bool("no-vlm", false, "跳过 VLM 识图（仅提取文本结构与图片占位）")
	reviewHTML := fs.String("review-html", "", "可选输出离线图文对照审阅 HTML 文件路径")
	splitDir := fs.String("split-dir", "", "当超出单篇体积上限时，自动按二级标题拆分输出到目录")
	stripTOC := fs.Bool("strip-toc", true, "剔除 Word 自动目录")
	stripCover := fs.Bool("strip-cover", false, "剔除首个标题前的封面内容")
	stripRev := fs.Bool("strip-revision-table", true, "剔除文首修订记录表")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "用法: code-markdocx-server convert [选项] <input.docx>")
		fs.PrintDefaults()
		return 2
	}

	inputPath := fs.Arg(0)
	cfg, err := config.LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		return 1
	}

	parseOpts := docxparser.ParseOptions{
		StripTOC:           *stripTOC,
		StripCover:         *stripCover,
		StripRevisionTable: *stripRev,
	}

	fmt.Fprintf(os.Stderr, "[1/4] 解析 .docx: %s\n", inputPath)
	doc, err := docxparser.ParseFile(inputPath, parseOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 .docx 失败: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "[2/4] 预处理 %d 张内嵌图片...\n", len(doc.Images))
	prepped, err := imgprep.PrepareImages(doc.Images, imgprep.PrepOptions{
		MinImagePx:          cfg.Convert.MinImagePx,
		MaxImageDim:         cfg.Convert.MaxImageDim,
		DedupeLogoThreshold: cfg.Convert.DedupeLogoThreshold,
		SofficePath:         cfg.Convert.SofficePath,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "图片预处理失败: %v\n", err)
		return 1
	}

	states := make(map[string]*mdpolish.ImageState, len(prepped))
	orderedStates := make([]*mdpolish.ImageState, 0, len(prepped))
	prepByID := make(map[string]*imgprep.PreparedImage, len(prepped))
	var vlmInputs []vlm.ConvertImageInput

	for _, p := range prepped {
		a := p.Asset
		prepByID[a.ID] = p
		assetFilename := ""
		if p.PreparedPath != "" {
			assetFilename = filepath.Base(p.PreparedPath)
		}
		st := &mdpolish.ImageState{
			ImageID:           a.ID,
			OrigFilename:      a.OrigFilename,
			SectionPath:       a.SectionPath,
			Caption:           a.Caption,
			ContextBefore:     a.ContextBefore,
			ContextAfter:      a.ContextAfter,
			Width:             a.Width,
			Height:            a.Height,
			Status:            mdpolish.StatusPending,
			Skipped:           a.PreSkipped,
			SkipReason:        a.SkipReason,
			AssetFilename:     assetFilename,
			HasImageFile:      len(p.PreparedData) > 0,
			UnsupportedVector: a.UnsupportedVector,
			VectorWarning:     a.VectorWarning,
		}
		if a.PreSkipped {
			st.Status = mdpolish.StatusSkipped
		} else if a.UnsupportedVector {
			st.Status = mdpolish.StatusUnsupportedVector
		} else if !*noVLM && cfg.VLM.IsConfigured() {
			vlmInputs = append(vlmInputs, vlm.ConvertImageInput{
				ImageID:       a.ID,
				ImageBytes:    p.PreparedData,
				ContentType:   p.PreparedCT,
				SectionPath:   a.SectionPath,
				Caption:       a.Caption,
				ContextBefore: a.ContextBefore,
				ContextAfter:  a.ContextAfter,
				GlossaryHints: *glossary,
			})
		}
		states[a.ID] = st
		orderedStates = append(orderedStates, st)
	}

	var codeTasks []mdpolish.CodePolishTask
	if !*noVLM && cfg.VLM.IsConfigured() {
		codeTasks = mdpolish.ExtractCodePolishTasks(doc, *glossary, 12)
	}

	if len(vlmInputs) > 0 || len(codeTasks) > 0 {
		client := vlm.NewClient(vlm.ClientConfig{
			BaseURL:           cfg.VLM.BaseURL,
			APIKey:            cfg.VLM.APIKey,
			Model:             cfg.VLM.Model,
			Timeout:           time.Duration(cfg.VLM.TimeoutSeconds) * time.Second,
			MaxRetries:        cfg.VLM.MaxRetries,
			Temperature:       cfg.VLM.Temperature,
			MaxTokens:         cfg.VLM.MaxTokens,
			UserAgent:         cfg.VLM.EffectiveUserAgent(),
			ExtraSystemPrompt: cfg.VLM.ExtraSystemPrompt,
		})
		if len(vlmInputs) > 0 {
			fmt.Fprintf(os.Stderr, "[3/4] 调用 VLM (%s) 并发转译 %d 张图表...\n", cfg.VLM.Model, len(vlmInputs))
			vlm.ProcessBatch(context.Background(), client, vlmInputs, cfg.VLM.MaxConcurrency, func(prog vlm.BatchProgress) {
				st := states[prog.ImageID]
				if prog.Err != nil {
					st.Status = mdpolish.StatusFailed
					st.Error = prog.Err.Error()
					fmt.Fprintf(os.Stderr, "  - [%d/%d] %s 失败: %v\n", prog.Done, prog.Total, prog.ImageID, prog.Err)
					return
				}
				st.Status = mdpolish.StatusDone
				st.Category = prog.Result.Category
				st.DiagramType = prog.Result.DiagramType
				st.MermaidCode = prog.Result.MermaidCode
				st.MarkdownTable = prog.Result.MarkdownTable
				st.CodeLanguage = prog.Result.CodeLanguage
				st.CodeContent = prog.Result.CodeContent
				st.Summary = prog.Result.Summary
				st.Confidence = prog.Result.Confidence
				st.Notes = prog.Result.Notes
				if prog.Result.RecommendedSkip {
					st.Skipped = true
					st.Status = mdpolish.StatusSkipped
					st.SkipReason = "VLM 判定为非架构装饰/界面图"
				}
				fmt.Fprintf(os.Stderr, "  - [%d/%d] %s 完成 (%s / %s)\n", prog.Done, prog.Total, prog.ImageID, st.Category, st.Confidence)
			})
		}
		if len(codeTasks) > 0 {
			fmt.Fprintf(os.Stderr, "[3/4] 调用 LLM (%s) 精修 %d 处代码块...\n", cfg.VLM.Model, len(codeTasks))
			codeInputs := make([]vlm.CodePolishInput, 0, len(codeTasks))
			for _, t := range codeTasks {
				codeInputs = append(codeInputs, t.Input)
			}
			codeResults := vlm.ProcessCodePolishBatch(context.Background(), client, codeInputs, cfg.VLM.MaxConcurrency, nil)
			applied := mdpolish.ApplyCodePolishResults(doc, codeTasks, codeResults)
			fmt.Fprintf(os.Stderr, "  - 已应用 %d 处代码块精修\n", applied)
		}
	} else {
		fmt.Fprintln(os.Stderr, "[3/4] 跳过 VLM 识图阶段（--no-vlm 或未配置 VLM）")
	}

	fmt.Fprintln(os.Stderr, "[4/4] 组装 Markdown 与合规预检...")
	md := mdpolish.Assemble(doc, states)
	report := mdpolish.Analyze(md, orderedStates, cfg.Convert.RefDocMaxBytes)

	targetMD := *outPath
	if targetMD == "" {
		ext := filepath.Ext(inputPath)
		targetMD = strings.TrimSuffix(inputPath, ext) + ".md"
	}
	if err := os.WriteFile(targetMD, []byte(md), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "写入 %s 失败: %v\n", targetMD, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "已生成: %s (%d bytes, ~%d tokens, %d mermaid blocks)\n",
		targetMD, report.ByteSize, report.EstimatedTokens, report.MermaidBlockCount)
	if report.ExceedsSizeLimit {
		fmt.Fprintf(os.Stderr, "提示: 体积超过 %d bytes，建议按二级标题拆分上传至 code-archdoc。\n", report.MaxBytes)
	}
	if len(report.SecretWarnings) > 0 {
		fmt.Fprintf(os.Stderr, "警告: 检测到 %d 处疑似凭据文本，上传 code-archdoc 前请核对。\n", len(report.SecretWarnings))
	}

	if *splitDir != "" {
		if err := os.MkdirAll(*splitDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "创建拆分目录失败: %v\n", err)
			return 1
		}
		chunks := mdpolish.SplitByH2(md, filepath.Base(targetMD), cfg.Convert.RefDocMaxBytes)
		for _, ch := range chunks {
			p := filepath.Join(*splitDir, ch.Filename)
			if err := os.WriteFile(p, []byte(ch.Content), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "写入拆分文件 %s 失败: %v\n", p, err)
				return 1
			}
		}
		fmt.Fprintf(os.Stderr, "已拆分输出 %d 个文件至: %s\n", len(chunks), *splitDir)
	}

	if *reviewHTML != "" {
		htmlContent := buildOfflineReviewHTML(filepath.Base(inputPath), orderedStates, prepByID)
		if err := os.WriteFile(*reviewHTML, []byte(htmlContent), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入对照报告失败: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "已生成离线图文对照页面: %s\n", *reviewHTML)
	}
	return 0
}

func buildOfflineReviewHTML(title string, states []*mdpolish.ImageState, prepByID map[string]*imgprep.PreparedImage) string {
	var cards strings.Builder
	for _, st := range states {
		p := prepByID[st.ImageID]
		imgTag := "<em>无预览图</em>"
		if p != nil && len(p.PreparedData) > 0 {
			ct := p.PreparedCT
			if ct == "" {
				ct = "image/png"
			}
			imgTag = fmt.Sprintf(`<img src="data:%s;base64,%s" alt="%s" style="max-width:100%%;border:1px solid #cbd5e1;border-radius:6px"/>`,
				html.EscapeString(ct),
				base64.StdEncoding.EncodeToString(p.PreparedData),
				html.EscapeString(st.ImageID),
			)
		}
		right := ""
		if st.MermaidCode != "" {
			right = fmt.Sprintf(`<pre class="mermaid">%s</pre><details><summary>Mermaid 源码</summary><pre><code>%s</code></pre></details>`,
				html.EscapeString(st.MermaidCode),
				html.EscapeString(st.MermaidCode),
			)
		} else if st.CodeContent != "" {
			right = fmt.Sprintf(`<pre><code>%s</code></pre>`, html.EscapeString(st.CodeContent))
		} else if st.MarkdownTable != "" {
			right = fmt.Sprintf(`<pre><code>%s</code></pre>`, html.EscapeString(st.MarkdownTable))
		} else {
			right = fmt.Sprintf(`<p>状态：%s %s</p>`, html.EscapeString(string(st.Status)), html.EscapeString(st.SkipReason+st.VectorWarning+st.Error))
		}
		cards.WriteString(fmt.Sprintf(`
<div class="card">
  <div class="meta"><strong>%s</strong> · %s · %s · %s</div>
  <div class="grid">
    <div>%s</div>
    <div>%s<p class="sum">%s</p></div>
  </div>
</div>`,
			html.EscapeString(st.ImageID),
			html.EscapeString(st.SectionPath),
			html.EscapeString(st.Caption),
			html.EscapeString(string(st.Status)),
			imgTag,
			right,
			html.EscapeString(st.Summary),
		))
	}
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>MarkDocx 图文对照 - %s</title>
<style>body{font-family:sans-serif;background:#f8fafc;color:#0f172a;margin:24px} .card{background:#fff;border:1px solid #e2e8f0;border-radius:8px;padding:16px;margin-bottom:16px} .meta{font-size:13px;color:#475569;margin-bottom:12px} .grid{display:grid;grid-template-columns:1fr 1fr;gap:16px} .sum{font-size:13px;color:#334155;background:#f1f5f9;padding:8px;border-radius:6px} pre{overflow:auto;background:#f8fafc;padding:8px;border-radius:6px}</style>
</head><body><h1>%s 图文转换对照</h1>%s</body></html>`,
		html.EscapeString(title), html.EscapeString(title), cards.String())
}
