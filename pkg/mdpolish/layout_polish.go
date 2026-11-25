package mdpolish

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"code-markdocx/pkg/docxparser"
	"code-markdocx/pkg/vlm"
)

const (
	layoutChunkMaxBlocks    = 45
	layoutChunkMaxRunes     = 5000
	layoutChunkSplitBlocks  = 15
	layoutChunkSplitRunes   = 1800
	layoutPreviewMaxRunes   = 220
	layoutHeadingMaxRunes   = 60
	layoutMergeMaxBlockSpan = 6
)

// ExtractLayoutPolishChunks splits DocumentIR blocks into bounded read-only structural
// summaries with global block indices so the LLM can emit safe IR layout operations.
func ExtractLayoutPolishChunks(doc *docxparser.DocumentIR, glossary string, maxChunks int) []vlm.LayoutPolishInput {
	if doc == nil || len(doc.Blocks) == 0 {
		return nil
	}
	if maxChunks <= 0 {
		maxChunks = 8
	}

	summaries := make([]vlm.LayoutBlockSummary, len(doc.Blocks))
	for i, blk := range doc.Blocks {
		summaries[i] = summarizeBlockForLayout(i, blk)
	}

	var chunks []vlm.LayoutPolishInput
	start := 0
	for start < len(summaries) && len(chunks) < maxChunks {
		curRunes := 0
		hasMutable := false
		end := start
		for end < len(summaries) {
			s := summaries[end]
			if end > start && s.Kind == string(docxparser.BlockHeading) &&
				(end-start >= layoutChunkSplitBlocks || curRunes >= layoutChunkSplitRunes) {
				break
			}
			previewRunes := utf8.RuneCountInString(s.TextPreview)
			if end > start && (end-start >= layoutChunkMaxBlocks || curRunes+previewRunes > layoutChunkMaxRunes) {
				break
			}
			curRunes += previewRunes
			if !s.Immutable {
				hasMutable = true
			}
			end++
		}
		if hasMutable {
			chunks = append(chunks, vlm.LayoutPolishInput{
				ChunkID:       fmt.Sprintf("layout-%02d", len(chunks)+1),
				DocTitle:      doc.Title,
				GlossaryHints: glossary,
				Blocks:        summaries[start:end],
			})
		}
		start = end
	}

	return chunks
}

func summarizeBlockForLayout(index int, blk docxparser.Block) vlm.LayoutBlockSummary {
	switch blk.Kind {
	case docxparser.BlockImage:
		return vlm.LayoutBlockSummary{
			Index:       index,
			Kind:        string(blk.Kind),
			TextPreview: fmt.Sprintf("[IMAGE %s]", blk.ImageID),
			Immutable:   true,
		}
	case docxparser.BlockCode:
		lang := strings.TrimSpace(blk.Language)
		if lang == "" {
			lang = "plain"
		}
		return vlm.LayoutBlockSummary{
			Index:       index,
			Kind:        string(blk.Kind),
			TextPreview: fmt.Sprintf("[CODE %s, %d chars]", lang, utf8.RuneCountInString(blk.Text)),
			Immutable:   true,
		}
	case docxparser.BlockTable:
		rows := len(blk.TableRows)
		cols := 0
		if rows > 0 {
			cols = len(blk.TableRows[0])
		}
		return vlm.LayoutBlockSummary{
			Index:       index,
			Kind:        string(blk.Kind),
			TextPreview: fmt.Sprintf("[TABLE %dx%d]", rows, cols),
			Immutable:   true,
		}
	default:
		clean := strings.ReplaceAll(blk.Text, "\r\n", "\n")
		clean = strings.ReplaceAll(clean, "\n", " / ")
		clean = strings.TrimSpace(clean)
		charCount := utf8.RuneCountInString(clean)
		return vlm.LayoutBlockSummary{
			Index:       index,
			Kind:        string(blk.Kind),
			Level:       blk.Level,
			Ordered:     blk.Ordered,
			AlertType:   blk.AlertType,
			TextPreview: truncateRunes(clean, layoutPreviewMaxRunes),
			CharCount:   charCount,
			Immutable:   false,
		}
	}
}

func truncateRunes(s string, maxLen int) string {
	if maxLen <= 0 || utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "..."
}

func isMutableLayoutKind(kind docxparser.BlockKind) bool {
	switch kind {
	case docxparser.BlockHeading, docxparser.BlockParagraph, docxparser.BlockListItem, docxparser.BlockQuote:
		return true
	default:
		return false
	}
}

type validatedMergeRange struct {
	start int
	end   int
}

// ApplyLayoutPolishOps validates and applies LLM structural layout instructions to DocumentIR
// under strict guardrails: immutable blocks (images/code/tables) are never altered and no text
// characters are ever deleted or rewritten.
func ApplyLayoutPolishOps(doc *docxparser.DocumentIR, ops []vlm.LayoutPolishOp) int {
	if doc == nil || len(doc.Blocks) == 0 || len(ops) == 0 {
		return 0
	}

	n := len(doc.Blocks)
	used := make([]bool, n)
	mutations := make(map[int]docxparser.Block)
	mergeByStart := make(map[int]validatedMergeRange)
	applied := 0

	for _, op := range ops {
		if op.Op == vlm.LayoutOpMergeParagraphs {
			if op.StartBlock < 0 || op.EndBlock >= n || op.StartBlock >= op.EndBlock {
				continue
			}
			if op.EndBlock-op.StartBlock+1 > layoutMergeMaxBlockSpan {
				continue
			}
			valid := true
			for idx := op.StartBlock; idx <= op.EndBlock; idx++ {
				if used[idx] || doc.Blocks[idx].Kind != docxparser.BlockParagraph || strings.TrimSpace(doc.Blocks[idx].Text) == "" {
					valid = false
					break
				}
			}
			if !valid {
				continue
			}
			for idx := op.StartBlock; idx <= op.EndBlock; idx++ {
				used[idx] = true
			}
			mergeByStart[op.StartBlock] = validatedMergeRange{start: op.StartBlock, end: op.EndBlock}
			applied++
			continue
		}

		idx := op.Block
		if idx < 0 || idx >= n || used[idx] {
			continue
		}
		orig := doc.Blocks[idx]
		if !isMutableLayoutKind(orig.Kind) {
			continue
		}

		next := orig
		switch op.Op {
		case vlm.LayoutOpSetHeading:
			if op.Level < 1 || op.Level > 6 {
				continue
			}
			txt := stripWholeBoldMarkdown(strings.TrimSpace(orig.Text))
			if txt == "" || strings.Contains(txt, "\n") || utf8.RuneCountInString(txt) > layoutHeadingMaxRunes || isCaptionText(txt) {
				continue
			}
			next.Kind = docxparser.BlockHeading
			next.Level = op.Level
			next.Text = txt
			next.Ordered = false
			next.Indent = 0
			next.AlertType = ""

		case vlm.LayoutOpSetParagraph:
			next.Kind = docxparser.BlockParagraph
			next.Level = 0
			next.Ordered = false
			next.Indent = 0
			next.AlertType = ""

		case vlm.LayoutOpSetListItem:
			indent := op.Indent
			if indent < 0 {
				indent = 0
			}
			if indent > 3 {
				indent = 3
			}
			next.Kind = docxparser.BlockListItem
			next.Level = 0
			next.Ordered = op.Ordered
			next.Indent = indent
			next.AlertType = ""

		case vlm.LayoutOpSetQuote:
			next.Kind = docxparser.BlockQuote
			next.Level = 0
			next.Ordered = false
			next.Indent = 0
			next.AlertType = ""

		case vlm.LayoutOpSetAlert:
			alert := normalizeAlertType(op.AlertType)
			if alert == "" {
				continue
			}
			next.Kind = docxparser.BlockQuote
			next.Level = 0
			next.Ordered = false
			next.Indent = 0
			next.AlertType = alert

		default:
			continue
		}

		if blocksEqualLayout(orig, next) {
			continue
		}
		used[idx] = true
		mutations[idx] = next
		applied++
	}

	if applied == 0 {
		return 0
	}

	newBlocks := make([]docxparser.Block, 0, n)
	for i := 0; i < n; {
		if mr, ok := mergeByStart[i]; ok {
			parts := make([]string, 0, mr.end-mr.start+1)
			for k := mr.start; k <= mr.end; k++ {
				parts = append(parts, strings.TrimSpace(doc.Blocks[k].Text))
			}
			newBlocks = append(newBlocks, docxparser.Block{
				Kind: docxparser.BlockParagraph,
				Text: joinParagraphTexts(parts),
			})
			i = mr.end + 1
			continue
		}
		if mut, ok := mutations[i]; ok {
			newBlocks = append(newBlocks, mut)
		} else {
			newBlocks = append(newBlocks, doc.Blocks[i])
		}
		i++
	}
	doc.Blocks = newBlocks
	return applied
}

func blocksEqualLayout(a, b docxparser.Block) bool {
	return a.Kind == b.Kind &&
		a.Level == b.Level &&
		a.Text == b.Text &&
		a.Ordered == b.Ordered &&
		a.Indent == b.Indent &&
		a.AlertType == b.AlertType
}

func joinParagraphTexts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString(p)
			continue
		}
		cur := b.String()
		lastR, _ := utf8.DecodeLastRuneInString(cur)
		firstR, _ := utf8.DecodeRuneInString(p)
		if isCJKOrPunct(lastR) || isCJKOrPunct(firstR) {
			b.WriteString(p)
		} else {
			b.WriteByte(' ')
			b.WriteString(p)
		}
	}
	return b.String()
}

// CombineLayoutSummaries merges per-chunk summaries into a concise Chinese status message.
func CombineLayoutSummaries(
	results map[string]*vlm.LayoutPolishResult,
	orderedChunkIDs []string,
	applied int,
) string {
	if applied == 0 {
		return "文档结构已较为清晰，未发现需要调整的排版项"
	}
	seen := make(map[string]struct{})
	var summaries []string
	for _, id := range orderedChunkIDs {
		res := results[id]
		if res == nil || len(res.Ops) == 0 {
			continue
		}
		s := strings.TrimSpace(res.Summary)
		if s == "" || strings.Contains(s, "无需调整") {
			continue
		}
		if _, exists := seen[s]; exists {
			continue
		}
		seen[s] = struct{}{}
		summaries = append(summaries, s)
		if len(summaries) >= 3 {
			break
		}
	}
	if len(summaries) == 0 {
		return fmt.Sprintf("已完成 %d 处结构化排版优化", applied)
	}
	return strings.Join(summaries, "；")
}
