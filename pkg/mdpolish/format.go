package mdpolish

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"code-markdocx/pkg/docxparser"
)

// FormatStats records deterministic formatting improvements applied to DocumentIR.
type FormatStats struct {
	PromotedHeadings int `json:"promoted_headings"`
	NormalizedLists  int `json:"normalized_lists"`
	ConvertedAlerts  int `json:"converted_alerts"`
	CleanedInline    int `json:"cleaned_inline"`
	TrimmedTables    int `json:"trimmed_tables"`
}

func (s FormatStats) Total() int {
	return s.PromotedHeadings + s.NormalizedLists + s.ConvertedAlerts + s.CleanedInline + s.TrimmedTables
}

var (
	reAdjacentBold   = regexp.MustCompile(`\*\*([^*\n]+?)\*\*\*\*([^*\n]+?)\*\*`)
	reEmptyBold      = regexp.MustCompile(`\*\*[ \t]*\*\*`)
	reBoldInnerSpace = regexp.MustCompile(`\*\*([ \t]*)([^*\n]+?)([ \t]*)\*\*`)
	reMultiSpaces    = regexp.MustCompile(`[ \t]{2,}`)

	reCaptionText    = regexp.MustCompile(`^(?i)(图|表|Figure|Fig\.?|Diagram)\s*\d*[\s:：.\-]*`)
	reZhChapter      = regexp.MustCompile(`^第[一二三四五六七八九十百千0-9]+[章篇]\s*.+$`)
	reZhSection      = regexp.MustCompile(`^第[一二三四五六七八九十百千0-9]+[节部分]\s*.+$`)
	reZhEnumHeading  = regexp.MustCompile(`^[一二三四五六七八九十]+、\s*.+$`)
	reZhParenHeading = regexp.MustCompile(`^[（(][一二三四五六七八九十]+[)）]\s*.+$`)

	reBulletLine  = regexp.MustCompile(`^[•●○▪▫■□◆◇➢➤➔⁃]\s*(.+)$`)
	reOrderedLine = regexp.MustCompile(`^(?:[①②③④⑤⑥⑦⑧⑨⑩]|[（(]\d{1,2}[)）])[.、\s]*(.+)$`)
)

// FormatDocument applies deterministic, zero-hallucination formatting normalization
// directly on DocumentIR while leaving code blocks and images untouched.
func FormatDocument(doc *docxparser.DocumentIR) FormatStats {
	var stats FormatStats
	if doc == nil || len(doc.Blocks) == 0 {
		return stats
	}

	// Pass 1: Clean inline markdown artifacts and heading bold wrappers.
	for i := range doc.Blocks {
		blk := &doc.Blocks[i]
		switch blk.Kind {
		case docxparser.BlockHeading:
			cleaned := cleanInlineText(blk.Text, false)
			cleaned = stripWholeBoldMarkdown(cleaned)
			if cleaned != blk.Text {
				blk.Text = cleaned
				stats.CleanedInline++
			}
		case docxparser.BlockParagraph, docxparser.BlockListItem:
			cleaned := cleanInlineText(blk.Text, false)
			if cleaned != blk.Text {
				blk.Text = cleaned
				stats.CleanedInline++
			}
		case docxparser.BlockQuote:
			cleaned := cleanInlineText(blk.Text, true)
			if cleaned != blk.Text {
				blk.Text = cleaned
				stats.CleanedInline++
			}
		}
	}

	// Pass 2: Promote unambiguous pseudo-headings (Chinese numbering & short bold lines).
	hasH1 := false
	for _, blk := range doc.Blocks {
		if blk.Kind == docxparser.BlockHeading && blk.Level == 1 {
			hasH1 = true
			break
		}
	}
	lastHeadingLvl := 0
	var prevKind docxparser.BlockKind
	for i := range doc.Blocks {
		blk := &doc.Blocks[i]
		if blk.Kind == docxparser.BlockHeading {
			if blk.Level == 1 {
				hasH1 = true
			}
			lastHeadingLvl = blk.Level
			prevKind = blk.Kind
			continue
		}
		if blk.Kind == docxparser.BlockParagraph {
			if lvl, cleanTitle, ok := detectPseudoHeading(blk.Text, prevKind, hasH1, lastHeadingLvl); ok {
				blk.Kind = docxparser.BlockHeading
				blk.Level = lvl
				blk.Text = cleanTitle
				if lvl == 1 {
					hasH1 = true
				}
				lastHeadingLvl = lvl
				stats.PromotedHeadings++
				prevKind = blk.Kind
				continue
			}
		}
		prevKind = blk.Kind
	}

	// Pass 3: Normalize bullet and consecutive ordered pseudo-list paragraphs.
	for i := 0; i < len(doc.Blocks); i++ {
		blk := &doc.Blocks[i]
		if blk.Kind != docxparser.BlockParagraph {
			continue
		}
		if m := reBulletLine.FindStringSubmatch(strings.TrimSpace(blk.Text)); len(m) == 2 {
			body := strings.TrimSpace(m[1])
			if body != "" {
				blk.Kind = docxparser.BlockListItem
				blk.Ordered = false
				blk.Indent = 0
				blk.Text = body
				stats.NormalizedLists++
				continue
			}
		}
	}

	for i := 0; i < len(doc.Blocks); {
		if doc.Blocks[i].Kind != docxparser.BlockParagraph {
			i++
			continue
		}
		j := i
		for j < len(doc.Blocks) && doc.Blocks[j].Kind == docxparser.BlockParagraph {
			if m := reOrderedLine.FindStringSubmatch(strings.TrimSpace(doc.Blocks[j].Text)); len(m) == 2 && strings.TrimSpace(m[1]) != "" {
				j++
				continue
			}
			break
		}
		if j-i >= 2 {
			for k := i; k < j; k++ {
				m := reOrderedLine.FindStringSubmatch(strings.TrimSpace(doc.Blocks[k].Text))
				doc.Blocks[k].Kind = docxparser.BlockListItem
				doc.Blocks[k].Ordered = true
				doc.Blocks[k].Indent = 0
				doc.Blocks[k].Text = strings.TrimSpace(m[1])
				stats.NormalizedLists++
			}
			i = j
			continue
		}
		i++
	}

	// Pass 4: Convert callout prefixes (注意/提示/重要/警告/危险) into GitHub Alerts.
	for i := range doc.Blocks {
		blk := &doc.Blocks[i]
		if blk.Kind != docxparser.BlockParagraph {
			continue
		}
		if alertType, body, ok := detectAlertParagraph(blk.Text); ok {
			blk.Kind = docxparser.BlockQuote
			blk.AlertType = alertType
			blk.Text = body
			stats.ConvertedAlerts++
		}
	}

	// Pass 5: Trim empty rows/columns and escape raw pipes in tables.
	for i := range doc.Blocks {
		blk := &doc.Blocks[i]
		if blk.Kind != docxparser.BlockTable {
			continue
		}
		cleanedRows, changed := normalizeTableRows(blk.TableRows)
		if changed {
			blk.TableRows = cleanedRows
			stats.TrimmedTables++
		}
	}

	return stats
}

func cleanInlineText(raw string, preserveNewlines bool) string {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if !preserveNewlines {
		s = strings.ReplaceAll(s, "\n", " ")
	}

	// Split by inline code spans so backtick contents are never modified.
	parts := splitInlineCodeSegments(s)
	for idx := range parts {
		if parts[idx].isCode {
			continue
		}
		seg := parts[idx].text
		seg = reEmptyBold.ReplaceAllString(seg, "")
		for {
			next := reAdjacentBold.ReplaceAllString(seg, "**$1$2**")
			if next == seg {
				break
			}
			seg = next
		}
		seg = reBoldInnerSpace.ReplaceAllStringFunc(seg, func(match string) string {
			sub := reBoldInnerSpace.FindStringSubmatch(match)
			if len(sub) != 4 {
				return match
			}
			inner := strings.TrimSpace(sub[2])
			if inner == "" {
				return ""
			}
			return sub[1] + "**" + inner + "**" + sub[3]
		})
		if preserveNewlines {
			lines := strings.Split(seg, "\n")
			for li := range lines {
				lines[li] = reMultiSpaces.ReplaceAllString(lines[li], " ")
			}
			seg = strings.Join(lines, "\n")
		} else {
			seg = reMultiSpaces.ReplaceAllString(seg, " ")
		}
		parts[idx].text = seg
	}

	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.text)
	}
	return strings.TrimSpace(b.String())
}

type inlineSegment struct {
	text   string
	isCode bool
}

func splitInlineCodeSegments(s string) []inlineSegment {
	var out []inlineSegment
	i := 0
	lastTextStart := 0
	for i < len(s) {
		if s[i] != '`' {
			i++
			continue
		}
		ticks := 0
		for i+ticks < len(s) && s[i+ticks] == '`' {
			ticks++
		}
		marker := s[i : i+ticks]
		closeIdx := -1
		searchFrom := i + ticks
		for searchFrom < len(s) {
			rel := strings.Index(s[searchFrom:], marker)
			if rel < 0 {
				break
			}
			cand := searchFrom + rel
			beforeOK := cand == 0 || s[cand-1] != '`'
			afterOK := cand+ticks >= len(s) || s[cand+ticks] != '`'
			if beforeOK && afterOK {
				closeIdx = cand
				break
			}
			searchFrom = cand + 1
		}
		if closeIdx < 0 {
			i += ticks
			continue
		}
		if i > lastTextStart {
			out = append(out, inlineSegment{text: s[lastTextStart:i], isCode: false})
		}
		out = append(out, inlineSegment{text: s[i : closeIdx+ticks], isCode: true})
		i = closeIdx + ticks
		lastTextStart = i
	}
	if lastTextStart < len(s) {
		out = append(out, inlineSegment{text: s[lastTextStart:], isCode: false})
	}
	return out
}

func isWholeBoldMarkdown(raw string) bool {
	t := strings.TrimSpace(raw)
	if !strings.HasPrefix(t, "**") || !strings.HasSuffix(t, "**") || len(t) <= 4 {
		return false
	}
	inner := t[2 : len(t)-2]
	return !strings.Contains(inner, "**")
}

func stripWholeBoldMarkdown(raw string) string {
	t := strings.TrimSpace(raw)
	if isWholeBoldMarkdown(t) {
		return strings.TrimSpace(t[2 : len(t)-2])
	}
	return t
}

func isCaptionText(plain string) bool {
	return reCaptionText.MatchString(strings.TrimSpace(plain))
}

func endsWithSentencePunct(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(t)
	switch r {
	case '。', '！', '？', '；', '，', '：', '.', '!', '?', ';', ',', ':':
		return true
	default:
		return false
	}
}

func detectPseudoHeading(
	rawText string,
	prevKind docxparser.BlockKind,
	hasH1 bool,
	lastHeadingLvl int,
) (int, string, bool) {
	trimmed := strings.TrimSpace(rawText)
	if trimmed == "" || strings.Contains(trimmed, "\n") {
		return 0, "", false
	}
	wholeBold := isWholeBoldMarkdown(trimmed)
	plain := stripWholeBoldMarkdown(trimmed)
	if plain == "" || isCaptionText(plain) {
		return 0, "", false
	}
	runeLen := utf8.RuneCountInString(plain)
	if runeLen < 2 || runeLen > 40 || endsWithSentencePunct(plain) {
		return 0, "", false
	}
	if strings.Contains(plain, "：") || strings.Contains(plain, ": ") {
		return 0, "", false
	}

	switch {
	case reZhChapter.MatchString(plain):
		return 1, plain, true
	case reZhSection.MatchString(plain):
		return 2, plain, true
	case reZhEnumHeading.MatchString(plain):
		if hasH1 {
			return 2, plain, true
		}
		return 1, plain, true
	case reZhParenHeading.MatchString(plain):
		if hasH1 {
			return 3, plain, true
		}
		return 2, plain, true
	case wholeBold && prevKind != docxparser.BlockImage && runeLen <= 32:
		if strings.HasPrefix(plain, "-") || strings.HasPrefix(plain, "•") {
			return 0, "", false
		}
		lvl := 2
		if lastHeadingLvl >= 1 {
			lvl = min(4, max(2, lastHeadingLvl+1))
		}
		return lvl, plain, true
	default:
		return 0, "", false
	}
}

func detectAlertParagraph(rawText string) (string, string, bool) {
	trimmed := strings.TrimSpace(rawText)
	if trimmed == "" {
		return "", "", false
	}

	// Normalize leading bold wrapping e.g. "**注意：** 内容" or "**注意**：内容"
	candidate := trimmed
	if strings.HasPrefix(candidate, "**") {
		if end := strings.Index(candidate[2:], "**"); end >= 0 {
			boldPart := strings.TrimSpace(candidate[2 : 2+end])
			restPart := strings.TrimSpace(candidate[4+end:])
			candidate = boldPart + restPart
		}
	}

	type prefixRule struct {
		prefix    string
		alertType string
	}
	rules := []prefixRule{
		{"提示说明", "NOTE"},
		{"注意", "NOTE"},
		{"说明", "NOTE"},
		{"备注", "NOTE"},
		{"note", "NOTE"},
		{"小贴士", "TIP"},
		{"提示", "TIP"},
		{"建议", "TIP"},
		{"tip", "TIP"},
		{"重要", "IMPORTANT"},
		{"关键", "IMPORTANT"},
		{"important", "IMPORTANT"},
		{"警告", "WARNING"},
		{"风险", "WARNING"},
		{"warning", "WARNING"},
		{"危险", "CAUTION"},
		{"严禁", "CAUTION"},
		{"caution", "CAUTION"},
	}

	lower := strings.ToLower(candidate)
	for _, r := range rules {
		if !strings.HasPrefix(lower, r.prefix) {
			continue
		}
		after := candidate[len(r.prefix):]
		afterTrim := strings.TrimLeft(after, " \t")
		if strings.HasPrefix(afterTrim, "：") {
			afterTrim = strings.TrimPrefix(afterTrim, "：")
		} else if strings.HasPrefix(afterTrim, ":") {
			afterTrim = strings.TrimPrefix(afterTrim, ":")
		} else {
			continue
		}
		body := strings.TrimSpace(afterTrim)
		if utf8.RuneCountInString(body) < 6 {
			return "", "", false
		}
		// Ensure English keywords are not mid-word matches (already checked colon immediately after).
		return r.alertType, body, true
	}
	return "", "", false
}

func escapeTableCellPipe(cell string) string {
	trimmed := strings.TrimSpace(cell)
	if !strings.Contains(trimmed, "|") {
		return trimmed
	}
	var b strings.Builder
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == '|' && (i == 0 || trimmed[i-1] != '\\') {
			b.WriteByte('\\')
		}
		b.WriteByte(trimmed[i])
	}
	return b.String()
}

func normalizeTableRows(rows [][]string) ([][]string, bool) {
	if len(rows) == 0 {
		return rows, false
	}

	maxCols := 0
	for _, r := range rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	if maxCols == 0 {
		return nil, true
	}

	changed := false
	cleaned := make([][]string, 0, len(rows))
	for rIdx, r := range rows {
		normRow := make([]string, maxCols)
		nonEmpty := false
		for c := 0; c < maxCols; c++ {
			orig := ""
			if c < len(r) {
				orig = r[c]
			}
			val := escapeTableCellPipe(orig)
			if c >= len(r) || val != orig {
				changed = true
			}
			normRow[c] = val
			if val != "" {
				nonEmpty = true
			}
		}
		if rIdx == 0 || nonEmpty {
			cleaned = append(cleaned, normRow)
		} else {
			changed = true
		}
	}

	if len(cleaned) == 0 {
		return nil, true
	}

	// Keep columns that have at least one non-empty cell across remaining rows.
	keepCols := make([]int, 0, maxCols)
	for c := 0; c < maxCols; c++ {
		hasVal := false
		for _, r := range cleaned {
			if r[c] != "" {
				hasVal = true
				break
			}
		}
		if hasVal {
			keepCols = append(keepCols, c)
		}
	}
	if len(keepCols) == 0 {
		return nil, true
	}
	if len(keepCols) != maxCols {
		changed = true
		projected := make([][]string, len(cleaned))
		for rIdx, r := range cleaned {
			row := make([]string, len(keepCols))
			for i, col := range keepCols {
				row[i] = r[col]
			}
			projected[rIdx] = row
		}
		cleaned = projected
	}

	if !changed {
		return rows, false
	}
	return cleaned, true
}

func isCJKOrPunct(r rune) bool {
	if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) {
		return true
	}
	switch r {
	case '，', '。', '、', '；', '：', '？', '！', '（', '）', '【', '】', '《', '》', '“', '”', '‘', '’':
		return true
	default:
		return false
	}
}
