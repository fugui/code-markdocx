package mdpolish

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"code-markdocx/pkg/docxparser"
	"code-markdocx/pkg/vlm"
)

type CodePolishTask struct {
	Input            vlm.CodePolishInput
	StartBlock       int
	EndBlock         int
	LineBlockOffsets []int
}

// ExtractCodePolishTasks finds code blocks that benefit from LLM language/indentation
// polishing and ambiguous prose regions that may contain unformatted code snippets.
func ExtractCodePolishTasks(doc *docxparser.DocumentIR, glossary string, maxTasks int) []CodePolishTask {
	if doc == nil || len(doc.Blocks) == 0 {
		return nil
	}
	if maxTasks <= 0 {
		maxTasks = 12
	}

	var tasks []CodePolishTask
	var headingStack [6]string
	seq := 0

	currentSection := func() string {
		var parts []string
		for _, h := range headingStack {
			if h != "" {
				parts = append(parts, h)
			}
		}
		return strings.Join(parts, " > ")
	}

	i := 0
	for i < len(doc.Blocks) && len(tasks) < maxTasks {
		blk := doc.Blocks[i]
		if blk.Kind == docxparser.BlockHeading {
			lvl := blk.Level
			if lvl >= 1 && lvl <= 6 {
				headingStack[lvl-1] = strings.TrimSpace(blk.Text)
				for k := lvl; k < 6; k++ {
					headingStack[k] = ""
				}
			}
			i++
			continue
		}

		// Mode A: Existing BlockCode missing language or missing structural indentation
		if blk.Kind == docxparser.BlockCode {
			if needsCodeBlockPolish(blk) {
				lines := strings.Split(strings.ReplaceAll(blk.Text, "\r\n", "\n"), "\n")
				if len(lines) > 0 && len(lines) <= 120 {
					seq++
					ctxBefore := ""
					if i > 0 && doc.Blocks[i-1].Kind == docxparser.BlockParagraph {
						ctxBefore = strings.TrimSpace(doc.Blocks[i-1].Text)
					}
					tasks = append(tasks, CodePolishTask{
						Input: vlm.CodePolishInput{
							ID:            fmt.Sprintf("code-%03d", seq),
							Mode:          vlm.CodePolishFormat,
							SectionPath:   currentSection(),
							ContextBefore: ctxBefore,
							Lines:         lines,
							DetectedLang:  blk.Language,
							GlossaryHints: glossary,
						},
						StartBlock: i,
						EndBlock:   i,
					})
				}
			}
			i++
			continue
		}

		// Mode B: Contiguous plain paragraphs with ambiguous/unformatted code cues
		if blk.Kind == docxparser.BlockParagraph {
			end := i
			for end+1 < len(doc.Blocks) && doc.Blocks[end+1].Kind == docxparser.BlockParagraph {
				end++
			}
			// Scan sub-windows within [i..end] for ambiguous code cues
			if end-i >= 1 || isLeadInPrompt(blk.Text) {
				if subTask, nextIdx, ok := extractAmbiguousParaTask(doc.Blocks, i, end, currentSection(), glossary, seq+1); ok {
					seq++
					tasks = append(tasks, subTask)
					i = nextIdx
					continue
				}
			}
			i = end + 1
			continue
		}

		i++
	}

	return tasks
}

func needsCodeBlockPolish(blk docxparser.Block) bool {
	lines := strings.Split(strings.ReplaceAll(blk.Text, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return false
	}
	if strings.TrimSpace(blk.Language) == "" && len(lines) >= 2 {
		return true
	}
	if len(lines) < 3 {
		return false
	}
	hasLeadingIndent := false
	hasBlockStructure := false
	for _, l := range lines {
		if strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "\t") {
			hasLeadingIndent = true
		}
		if strings.Contains(l, "{") || strings.Contains(l, "}") || strings.HasSuffix(strings.TrimSpace(l), ":") {
			hasBlockStructure = true
		}
	}
	return hasBlockStructure && !hasLeadingIndent
}

func isLeadInPrompt(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" || len([]rune(t)) > 48 {
		return false
	}
	if !strings.HasSuffix(t, "：") && !strings.HasSuffix(t, ":") {
		return false
	}
	lower := strings.ToLower(t)
	for _, kw := range []string{"代码", "源码", "示例", "配置", "脚本", "命令", "sql", "json", "yaml", "xml", "请求", "响应"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func extractAmbiguousParaTask(
	blocks []docxparser.Block,
	start, end int,
	sectionPath, glossary string,
	seq int,
) (CodePolishTask, int, bool) {
	hasCue := func(txt string) bool {
		t := strings.TrimSpace(txt)
		if t == "" {
			return false
		}
		for _, tok := range []string{":=", "=>", "->", "::", "&&", "||", "();", "{", "}", "SELECT ", "FROM ", "INSERT ", "UPDATE ", "CREATE TABLE", "import ", "package ", "func ", "def ", "class ", "curl ", "docker ", "kubectl ", "public ", "private "} {
			if strings.Contains(t, tok) {
				return true
			}
		}
		if strings.HasPrefix(t, "$ ") || strings.HasPrefix(t, "// ") {
			return true
		}
		return false
	}

	firstTrigger := -1
	for k := start; k <= end; k++ {
		if isLeadInPrompt(blocks[k].Text) && k+1 <= end {
			firstTrigger = k + 1
			break
		}
		if hasCue(blocks[k].Text) {
			firstTrigger = k
			break
		}
	}
	if firstTrigger < 0 {
		return CodePolishTask{}, end + 1, false
	}

	winStart := firstTrigger
	ctxBefore := ""
	if winStart > start {
		ctxBefore = strings.TrimSpace(blocks[winStart-1].Text)
	}

	var lines []string
	var offsets []int
	winEnd := winStart
	cueLines := 0
	for k := winStart; k <= end && len(lines) < 60; k++ {
		txt := blocks[k].Text
		if hasCue(txt) {
			cueLines++
		}
		split := strings.Split(strings.ReplaceAll(txt, "\r\n", "\n"), "\n")
		if len(lines)+len(split) > 60 && len(lines) > 0 {
			break
		}
		for _, l := range split {
			lines = append(lines, l)
			offsets = append(offsets, k)
		}
		winEnd = k
		// Stop window if we hit two consecutive purely Chinese prose lines after seeing code cues
		if cueLines > 0 && isChineseProseLine(txt) && k+1 <= end && isChineseProseLine(blocks[k+1].Text) {
			break
		}
	}

	if len(lines) < 2 || (cueLines == 0 && !isLeadInPrompt(ctxBefore)) {
		return CodePolishTask{}, winEnd + 1, false
	}

	return CodePolishTask{
		Input: vlm.CodePolishInput{
			ID:            fmt.Sprintf("code-%03d", seq),
			Mode:          vlm.CodePolishDetect,
			SectionPath:   sectionPath,
			ContextBefore: ctxBefore,
			Lines:         lines,
			GlossaryHints: glossary,
		},
		StartBlock:       winStart,
		EndBlock:         winEnd,
		LineBlockOffsets: offsets,
	}, winEnd + 1, true
}

func isChineseProseLine(s string) bool {
	han := 0
	total := 0
	hasPunct := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if unicode.Is(unicode.Han, r) {
			han++
		}
		if r == '。' || r == '，' || r == '、' {
			hasPunct = true
		}
	}
	if total == 0 {
		return false
	}
	return float64(han)/float64(total) > 0.5 && hasPunct
}

// ApplyCodePolishResults safely applies LLM formatting/detection results to doc.Blocks.
// Every code replacement is guarded by vlm.VerifyCodePreservation so non-whitespace characters
// can never be altered or hallucinated.
func ApplyCodePolishResults(
	doc *docxparser.DocumentIR,
	tasks []CodePolishTask,
	results map[string]*vlm.CodePolishResult,
) int {
	if doc == nil || len(tasks) == 0 || len(results) == 0 {
		return 0
	}

	sorted := make([]CodePolishTask, len(tasks))
	copy(sorted, tasks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartBlock > sorted[j].StartBlock
	})

	applied := 0
	for _, task := range sorted {
		res := results[task.Input.ID]
		if res == nil || !res.IsCode {
			continue
		}

		if task.Input.Mode == vlm.CodePolishFormat {
			if task.StartBlock < 0 || task.StartBlock >= len(doc.Blocks) {
				continue
			}
			blk := &doc.Blocks[task.StartBlock]
			if blk.Kind != docxparser.BlockCode {
				continue
			}
			changed := false
			if res.Language != "" && blk.Language == "" {
				blk.Language = res.Language
				changed = true
			}
			if res.FormattedCode != "" && vlm.VerifyCodePreservation(blk.Text, res.FormattedCode) {
				if res.FormattedCode != blk.Text {
					blk.Text = res.FormattedCode
					changed = true
				}
			}
			if changed {
				applied++
			}
			continue
		}

		// CodePolishDetect
		if len(task.LineBlockOffsets) == 0 {
			continue
		}
		startLine := res.StartLine
		endLine := res.EndLine
		if startLine < 1 || endLine < startLine || endLine > len(task.LineBlockOffsets) {
			continue
		}
		startBlk := task.LineBlockOffsets[startLine-1]
		endBlk := task.LineBlockOffsets[endLine-1]
		if startBlk < 0 || endBlk < startBlk || endBlk >= len(doc.Blocks) {
			continue
		}

		var origParts []string
		validRange := true
		for b := startBlk; b <= endBlk; b++ {
			if doc.Blocks[b].Kind != docxparser.BlockParagraph {
				validRange = false
				break
			}
			origParts = append(origParts, doc.Blocks[b].Text)
		}
		if !validRange {
			continue
		}

		origJoined := strings.Join(origParts, "\n")
		finalCode := origJoined
		if res.FormattedCode != "" && vlm.VerifyCodePreservation(origJoined, res.FormattedCode) {
			finalCode = res.FormattedCode
		}
		if strings.TrimSpace(finalCode) == "" {
			continue
		}

		codeBlock := docxparser.Block{
			Kind:     docxparser.BlockCode,
			Language: res.Language,
			Text:     finalCode,
		}
		nextBlocks := make([]docxparser.Block, 0, len(doc.Blocks)-(endBlk-startBlk))
		nextBlocks = append(nextBlocks, doc.Blocks[:startBlk]...)
		nextBlocks = append(nextBlocks, codeBlock)
		nextBlocks = append(nextBlocks, doc.Blocks[endBlk+1:]...)
		doc.Blocks = nextBlocks
		applied++
	}

	return applied
}
