package mdpolish

import (
	"fmt"
	"strings"

	"code-markdocx/pkg/docxparser"
	"code-markdocx/pkg/vlm"
)

type ImageStatus string

const (
	StatusPending           ImageStatus = "pending"
	StatusProcessing        ImageStatus = "processing"
	StatusDone              ImageStatus = "done"
	StatusFailed            ImageStatus = "failed"
	StatusSkipped           ImageStatus = "skipped"
	StatusUnsupportedVector ImageStatus = "unsupported_vector"
)

type ImageState struct {
	ImageID           string              `json:"image_id"`
	OrigFilename      string              `json:"orig_filename"`
	SectionPath       string              `json:"section_path"`
	Caption           string              `json:"caption"`
	ContextBefore     string              `json:"context_before"`
	ContextAfter      string              `json:"context_after"`
	Width             int                 `json:"width"`
	Height            int                 `json:"height"`
	Status            ImageStatus         `json:"status"`
	Skipped           bool                `json:"skipped"`
	SkipReason        string              `json:"skip_reason,omitempty"`
	UnsupportedVector bool                `json:"unsupported_vector"`
	VectorWarning     string              `json:"vector_warning,omitempty"`
	Error             string              `json:"error,omitempty"`
	Category          vlm.DiagramCategory `json:"category,omitempty"`
	DiagramType       vlm.DiagramType     `json:"diagram_type,omitempty"`
	MermaidCode       string              `json:"mermaid_code,omitempty"`
	MarkdownTable     string              `json:"markdown_table,omitempty"`
	Summary           string              `json:"summary,omitempty"`
	Confidence        vlm.ConfidenceLevel `json:"confidence,omitempty"`
	Notes             string              `json:"notes,omitempty"`
}

func Assemble(doc *docxparser.DocumentIR, states map[string]*ImageState) string {
	if doc == nil {
		return ""
	}
	var out []string
	var prevKind docxparser.BlockKind
	var lastRenderedCaption string

	for _, blk := range doc.Blocks {
		switch blk.Kind {
		case docxparser.BlockHeading:
			lvl := blk.Level
			if lvl < 1 {
				lvl = 1
			}
			if lvl > 6 {
				lvl = 6
			}
			out = append(out, strings.Repeat("#", lvl)+" "+strings.TrimSpace(blk.Text))
			prevKind = blk.Kind
			lastRenderedCaption = ""

		case docxparser.BlockParagraph:
			txt := strings.TrimSpace(blk.Text)
			if txt != "" {
				if prevKind == docxparser.BlockImage && lastRenderedCaption != "" && txt == lastRenderedCaption {
					lastRenderedCaption = ""
					continue
				}
				out = append(out, txt)
				prevKind = blk.Kind
				lastRenderedCaption = ""
			}

		case docxparser.BlockListItem:
			indent := strings.Repeat("  ", max(0, blk.Indent))
			marker := "- "
			if blk.Ordered {
				marker = "1. "
			}
			line := indent + marker + strings.TrimSpace(blk.Text)
			if prevKind == docxparser.BlockListItem && len(out) > 0 {
				out[len(out)-1] = out[len(out)-1] + "\n" + line
			} else {
				out = append(out, line)
			}
			prevKind = blk.Kind
			lastRenderedCaption = ""

		case docxparser.BlockTable:
			tbl := renderGFMTable(blk.TableRows)
			if tbl != "" {
				out = append(out, tbl)
				prevKind = blk.Kind
				lastRenderedCaption = ""
			}

		case docxparser.BlockImage:
			st := states[blk.ImageID]
			rendered := renderImageBlock(st)
			if rendered != "" {
				out = append(out, rendered)
				prevKind = blk.Kind
				if st != nil {
					lastRenderedCaption = strings.TrimSpace(st.Caption)
				}
			}
		}
	}

	result := strings.Join(out, "\n\n")
	if result != "" && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

func renderGFMTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	cols := len(rows[0])
	if cols == 0 {
		return ""
	}
	var b strings.Builder
	writeRow := func(r []string) {
		b.WriteByte('|')
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(r) {
				cell = strings.TrimSpace(r[i])
			}
			b.WriteByte(' ')
			b.WriteString(cell)
			b.WriteString(" |")
		}
		b.WriteByte('\n')
	}

	writeRow(rows[0])
	b.WriteByte('|')
	for i := 0; i < cols; i++ {
		b.WriteString(" :--- |")
	}
	b.WriteByte('\n')
	for _, r := range rows[1:] {
		writeRow(r)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderImageBlock(st *ImageState) string {
	if st == nil || st.Skipped || st.Status == StatusSkipped {
		return ""
	}
	if st.UnsupportedVector || st.Status == StatusUnsupportedVector {
		label := st.Caption
		if label == "" {
			label = st.OrigFilename
		}
		return fmt.Sprintf("> [未转换矢量图: %s] %s", label, st.VectorWarning)
	}
	if st.Status == StatusFailed {
		label := st.Caption
		if label == "" {
			label = st.OrigFilename
		}
		return fmt.Sprintf("> [图片转译待补充: %s] 转译失败（%s），请人工补充文字或 Mermaid 说明。", label, st.Error)
	}
	if st.Status == StatusPending || st.Status == StatusProcessing {
		label := st.Caption
		if label == "" {
			label = st.OrigFilename
		}
		return fmt.Sprintf("> [图片转译处理中: %s (%s)]", label, st.ImageID)
	}

	var parts []string
	if st.Caption != "" {
		parts = append(parts, "**"+st.Caption+"**")
	}

	switch st.Category {
	case vlm.CategoryTableShot:
		if tbl := strings.TrimSpace(st.MarkdownTable); tbl != "" {
			parts = append(parts, tbl)
		}
		if sum := strings.TrimSpace(st.Summary); sum != "" {
			parts = append(parts, "> 图表说明："+sum)
		}
	case vlm.CategoryUIOrDecorative:
		if sum := strings.TrimSpace(st.Summary); sum != "" {
			parts = append(parts, "> [界面/辅助图示] "+sum)
		}
	default:
		if code := strings.TrimSpace(st.MermaidCode); code != "" {
			parts = append(parts, "```mermaid\n"+code+"\n```")
		}
		if sum := strings.TrimSpace(st.Summary); sum != "" {
			parts = append(parts, "> 图表说明："+sum)
		}
	}

	return strings.Join(parts, "\n\n")
}
