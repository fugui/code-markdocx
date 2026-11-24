package mdpolish

import (
	"fmt"
	"path/filepath"
	"strings"
)

type SplitChunk struct {
	Filename string `json:"filename"`
	Title    string `json:"title"`
	Content  string `json:"content"`
}

// SplitByH2 splits Markdown cleanly along top-level `## ` headings (respecting fenced code blocks),
// packing consecutive sections up to maxBytes when possible.
func SplitByH2(markdown string, baseName string, maxBytes int64) []SplitChunk {
	base := strings.TrimSuffix(filepath.Base(baseName), filepath.Ext(baseName))
	if base == "" {
		base = "document"
	}
	if maxBytes <= 0 {
		maxBytes = 524288
	}

	type section struct {
		title   string
		content string
	}

	lines := strings.Split(markdown, "\n")
	var sections []section
	var curTitle string
	var buf []string
	inFence := false

	flush := func() {
		body := strings.TrimSpace(strings.Join(buf, "\n"))
		if body != "" {
			if curTitle == "" {
				curTitle = "概览"
			}
			sections = append(sections, section{title: curTitle, content: body + "\n"})
		}
		buf = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			flush()
			curTitle = strings.TrimSpace(strings.TrimPrefix(line, "## "))
		}
		buf = append(buf, line)
	}
	flush()

	if len(sections) == 0 {
		return []SplitChunk{{Filename: base + ".md", Title: base, Content: markdown}}
	}

	var chunks []SplitChunk
	var curBuf strings.Builder
	var curChunkTitle string
	chunkIdx := 0

	emitChunk := func() {
		if curBuf.Len() == 0 {
			return
		}
		chunkIdx++
		slug := sanitizeFilenamePart(curChunkTitle)
		if slug == "" {
			slug = fmt.Sprintf("part-%02d", chunkIdx)
		}
		chunks = append(chunks, SplitChunk{
			Filename: fmt.Sprintf("%s-%02d-%s.md", base, chunkIdx, slug),
			Title:    curChunkTitle,
			Content:  curBuf.String(),
		})
		curBuf.Reset()
		curChunkTitle = ""
	}

	for _, sec := range sections {
		if curBuf.Len() > 0 && int64(curBuf.Len()+2+len(sec.content)) > maxBytes {
			emitChunk()
		}
		if curBuf.Len() > 0 {
			curBuf.WriteString("\n")
		} else {
			curChunkTitle = sec.title
		}
		curBuf.WriteString(sec.content)
	}
	emitChunk()

	return chunks
}

func sanitizeFilenamePart(s string) string {
	var b strings.Builder
	count := 0
	for _, r := range strings.TrimSpace(s) {
		if count >= 24 {
			break
		}
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteByte('-')
		case r == ' ' || r == '\t':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
		count++
	}
	return strings.Trim(b.String(), "-.")
}
