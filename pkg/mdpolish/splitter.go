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
	var activeFence fenceMarker

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
		if !activeFence.active {
			if fm, _, ok := parseFenceOpen(trimmed); ok {
				activeFence = fm
			}
		} else if isFenceClose(trimmed, activeFence) {
			activeFence = fenceMarker{}
		}
		if !activeFence.active && strings.HasPrefix(line, "## ") {
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

type fenceMarker struct {
	active bool
	char   byte
	length int
}

func parseFenceOpen(trimmed string) (fenceMarker, string, bool) {
	if len(trimmed) < 3 {
		return fenceMarker{}, "", false
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return fenceMarker{}, "", false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return fenceMarker{}, "", false
	}
	info := strings.TrimSpace(trimmed[n:])
	if ch == '`' && strings.Contains(info, "`") {
		return fenceMarker{}, "", false
	}
	firstWord := info
	if sp := strings.IndexAny(info, " \t"); sp >= 0 {
		firstWord = info[:sp]
	}
	return fenceMarker{active: true, char: ch, length: n}, firstWord, true
}

func isFenceClose(trimmed string, fm fenceMarker) bool {
	if !fm.active || len(trimmed) < fm.length {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != fm.char {
			return false
		}
	}
	return true
}
