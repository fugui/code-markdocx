package docxparser

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

type textSpan struct {
	text   string
	bold   bool
	italic bool
	code   bool
	isMono bool
	rawMD  bool
	color  string
}

type paraMeta struct {
	styleID      string
	isCodeStyle  bool
	hasShading   bool
	shadingFill  string
	hasBorder    bool
	indentLeft   int
	spans        []textSpan
	plainText    string
	inlineMD     string
	monoRatio    float64
	distinctCols int
	hasSoftBreak bool
	isEmpty      bool
}

type rawBlockItem struct {
	block           Block
	meta            *paraMeta
	blankLinesAfter int
}

type codeSignalLevel int

const (
	signalNone codeSignalLevel = iota
	signalWeak
	signalHeuristic
	signalStrong
)

var (
	leadInRe = regexp.MustCompile(`(?i)(代码|源码|示例|实现|配置|脚本|命令|报文|sql|json|yaml|xml|curl|请求|响应).{0,12}[：:]\s*$`)
	yamlKVRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+:\s+\S+`)
	jsonKVRe = regexp.MustCompile(`^"[^"\n]+"\s*:`)
	xmlTagRe = regexp.MustCompile(`^<\/?[a-zA-Z][a-zA-Z0-9:_-]*(\s[^>]*)?\/?>$`)
	httpRe   = regexp.MustCompile(`^(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+/\S*\s+HTTP/\d`)
	funcGoRe = regexp.MustCompile(`^func\s+(\([^)]+\)\s*)?[A-Za-z_][A-Za-z0-9_]*\s*\(`)
	funcPyRe = regexp.MustCompile(`^(async\s+)?def\s+[A-Za-z_][A-Za-z0-9_]*\s*\(`)
)

func isLeadInLine(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" || len([]rune(t)) > 48 {
		return false
	}
	if t == "代码如下：" || t == "代码如下:" || t == "示例：" || t == "示例:" || t == "示例代码：" || t == "示例代码:" {
		return true
	}
	return leadInRe.MatchString(t)
}

func textStats(s string) (hanCount, nonSpaceCount int, hasCJKPunct bool) {
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		nonSpaceCount++
		if unicode.Is(unicode.Han, r) {
			hanCount++
		}
		if r == '。' || r == '，' || r == '、' || r == '；' || r == '！' || r == '？' {
			hasCJKPunct = true
		}
	}
	return hanCount, nonSpaceCount, hasCJKPunct
}

func hanRatio(s string) float64 {
	han, total, _ := textStats(s)
	if total == 0 {
		return 0
	}
	return float64(han) / float64(total)
}

func hasStrongCodeSyntax(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	for _, op := range []string{":=", "==", "!=", "->", "=>", "::", "&&", "||", "<?xml", "#!/", "```"} {
		if strings.Contains(t, op) {
			return true
		}
	}
	if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "SELECT ") ||
		strings.HasPrefix(t, "CREATE TABLE") || strings.HasPrefix(t, "INSERT INTO") {
		return true
	}
	if jsonKVRe.MatchString(t) || httpRe.MatchString(t) || funcGoRe.MatchString(t) || funcPyRe.MatchString(t) {
		return true
	}
	if strings.HasSuffix(t, "{") || strings.HasSuffix(t, ";") || strings.HasSuffix(t, ");") || strings.HasSuffix(t, "},") {
		return true
	}
	return false
}

func isNaturalLanguageCallout(text string) bool {
	han, total, hasPunct := textStats(text)
	if total == 0 {
		return false
	}
	ratio := float64(han) / float64(total)
	if ratio > 0.45 && hasPunct && !hasStrongCodeSyntax(text) {
		return true
	}
	return false
}

func scoreCodeLine(line string) int {
	t := strings.TrimSpace(line)
	if t == "" {
		return 0
	}
	score := 0

	if hasStrongCodeSyntax(t) {
		score += 3
	}
	kwPrefixes := []string{
		"package ", "import ", "func ", "type ", "struct ", "interface ",
		"def ", "class ", "async ", "return ", "public ", "private ", "protected ",
		"const ", "let ", "var ", "export ", "from ", "where ", "select ", "insert ",
		"update ", "delete ", "create ", "alter ", "drop ", "curl ", "docker ",
		"kubectl ", "git ", "npm ", "go ", "python ", "pip ", "sudo ", "chmod ",
	}
	lower := strings.ToLower(t)
	for _, kw := range kwPrefixes {
		if strings.HasPrefix(lower, kw) {
			score += 2
			break
		}
	}
	if strings.HasPrefix(t, "$ ") || strings.HasPrefix(t, "#!/") {
		score += 2
	}
	if yamlKVRe.MatchString(t) || xmlTagRe.MatchString(t) {
		score += 2
	}
	if strings.Contains(t, "(") && strings.Contains(t, ")") && !strings.Contains(t, "（") {
		score++
	}
	if strings.Contains(t, "{") || strings.Contains(t, "}") {
		score++
	}
	if strings.Contains(t, " = ") || strings.Contains(t, " := ") {
		score++
	}

	han, total, hasPunct := textStats(t)
	if total > 0 && float64(han)/float64(total) > 0.45 && hasPunct && !hasStrongCodeSyntax(t) {
		score -= 3
	}
	return score
}

func isClosingDelimiterLine(line string) bool {
	switch strings.TrimSpace(line) {
	case "}", "};", "},", ");", "]", "],", "*/":
		return true
	}
	return false
}

func isWeakContextCodeLine(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	if isClosingDelimiterLine(t) {
		return true
	}
	switch t {
	case "{", "else", "else {", "} else {", "return", "break", "continue", "pass":
		return true
	}
	if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "* ") ||
		strings.HasPrefix(t, "# ") || strings.HasPrefix(t, "-- ") {
		return true
	}
	return false
}

func isMultiLineCodeText(text string) bool {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	nonEmpty := 0
	totalScore := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		nonEmpty++
		totalScore += scoreCodeLine(l)
	}
	if nonEmpty < 2 {
		return false
	}
	if json.Valid([]byte(strings.TrimSpace(text))) && strings.HasPrefix(strings.TrimSpace(text), "{") {
		return true
	}
	return totalScore >= 3
}

func classifyCodePara(m *paraMeta, afterLeadIn bool) codeSignalLevel {
	if m == nil || m.isEmpty {
		return signalNone
	}
	t := strings.TrimSpace(m.plainText)
	if t == "" {
		return signalNone
	}

	han, total, hasPunct := textStats(t)
	ratio := 0.0
	if total > 0 {
		ratio = float64(han) / float64(total)
	}

	if m.isCodeStyle {
		return signalStrong
	}

	// Strong exclusion for plain prose when no code style or monospace dominance
	if m.monoRatio < 0.5 && ratio > 0.45 && hasPunct && !hasStrongCodeSyntax(t) {
		return signalNone
	}

	if m.monoRatio >= 0.80 {
		return signalStrong
	}
	if (m.hasShading || m.hasBorder) && (ratio < 0.45 || hasStrongCodeSyntax(t)) {
		return signalStrong
	}
	if m.distinctCols >= 2 && ratio < 0.45 && scoreCodeLine(t) >= 1 {
		return signalStrong
	}
	if m.hasSoftBreak && isMultiLineCodeText(m.plainText) {
		return signalStrong
	}

	sc := scoreCodeLine(t)
	if sc >= 3 || (afterLeadIn && sc >= 2) {
		return signalHeuristic
	}
	if isWeakContextCodeLine(t) {
		return signalWeak
	}
	if afterLeadIn && sc >= 1 {
		return signalWeak
	}
	return signalNone
}

func isStandaloneStrongCodeWorthFence(m *paraMeta) bool {
	if m == nil {
		return false
	}
	t := strings.TrimSpace(m.plainText)
	if m.isCodeStyle || m.hasShading || m.hasBorder {
		return !isNaturalLanguageCallout(t)
	}
	if m.hasSoftBreak && isMultiLineCodeText(m.plainText) {
		return true
	}
	if scoreCodeLine(t) >= 2 {
		return true
	}
	// Single isolated short monospace token (e.g. "UserService" or "OAuth2") stays inline code in paragraph.
	return false
}

func applyRelativeIndent(metas []*paraMeta, texts []string) []string {
	minLeft := -1
	hasVariedIndent := false
	for _, m := range metas {
		if m == nil || m.isEmpty {
			continue
		}
		if minLeft < 0 || m.indentLeft < minLeft {
			minLeft = m.indentLeft
		}
		if m.indentLeft != metas[0].indentLeft {
			hasVariedIndent = true
		}
	}
	if !hasVariedIndent || minLeft < 0 {
		return texts
	}

	out := make([]string, len(texts))
	for i, txt := range texts {
		m := metas[i]
		if m == nil || m.isEmpty || strings.TrimSpace(txt) == "" {
			out[i] = txt
			continue
		}
		// Only synthesize indent from w:ind if line doesn't already start with whitespace
		if len(txt) > 0 && (txt[0] == ' ' || txt[0] == '\t') {
			out[i] = txt
			continue
		}
		diff := m.indentLeft - minLeft
		if diff > 100 {
			levels := (diff + 180) / 360
			if levels < 1 {
				levels = 1
			}
			if levels > 12 {
				levels = 12
			}
			out[i] = strings.Repeat("  ", levels) + txt
		} else {
			out[i] = txt
		}
	}
	return out
}

func dedentCode(raw string) string {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.ReplaceAll(normalized, "\u00a0", " ")
	normalized = strings.ReplaceAll(normalized, "\t", "    ")

	lines := strings.Split(normalized, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}

	minIndent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		indent := 0
		for _, ch := range l {
			if ch == ' ' {
				indent++
			} else {
				break
			}
		}
		if minIndent < 0 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent > 0 {
		for i, l := range lines {
			if len(l) >= minIndent && strings.TrimSpace(l) != "" {
				lines[i] = l[minIndent:]
			} else if strings.TrimSpace(l) == "" {
				lines[i] = ""
			}
		}
	}
	return strings.Join(lines, "\n")
}

func postProcessCodeBlocks(items []rawBlockItem) []Block {
	if len(items) == 0 {
		return nil
	}

	signals := make([]codeSignalLevel, len(items))
	leadIns := make([]string, len(items))
	var curLeadIn string
	for i := range items {
		it := items[i]
		if it.block.Kind == BlockParagraph && it.meta != nil {
			afterLead := curLeadIn != ""
			signals[i] = classifyCodePara(it.meta, afterLead)
			leadIns[i] = curLeadIn
			if isLeadInLine(it.meta.plainText) {
				curLeadIn = strings.TrimSpace(it.meta.plainText)
			} else if signals[i] == signalNone {
				curLeadIn = ""
			}
		} else {
			curLeadIn = ""
		}
	}

	var out []Block
	i := 0
	for i < len(items) {
		it := items[i]
		if it.block.Kind != BlockParagraph || it.meta == nil || (signals[i] != signalStrong && signals[i] != signalHeuristic) {
			out = append(out, it.block)
			i++
			continue
		}

		// Collect contiguous code candidate span, bridging <=2 blank lines and absorbing weak intermediate lines
		end := i
		strongCount := 0
		heurCount := 0
		for j := i; j < len(items); j++ {
			if items[j].block.Kind != BlockParagraph || items[j].meta == nil {
				break
			}
			sig := signals[j]
			if sig == signalStrong {
				strongCount++
				end = j
			} else if sig == signalHeuristic {
				heurCount++
				end = j
			} else if sig == signalWeak && j > i {
				// Absorb weak line if followed by code OR if it's a valid closing delimiter ending the block
				hasNext := false
				if items[j].blankLinesAfter <= 2 && j+1 < len(items) && items[j+1].block.Kind == BlockParagraph && items[j+1].meta != nil {
					ns := signals[j+1]
					if ns == signalStrong || ns == signalHeuristic || ns == signalWeak {
						hasNext = true
					}
				}
				if hasNext {
					end = j
				} else if isClosingDelimiterLine(items[j].meta.plainText) {
					end = j
					break
				} else {
					break
				}
			} else {
				break
			}

			if items[j].blankLinesAfter > 2 {
				break
			}
		}

		// Trim trailing weak lines if we ended on weak lines without strong backing?
		// Actually trailing `}` or `*/` IS valid code end! Only verify group validity.
		spanLen := end - i + 1
		accept := false
		if strongCount >= 2 {
			accept = true
		} else if strongCount == 1 && spanLen >= 2 {
			accept = true
		} else if strongCount == 1 && spanLen == 1 {
			accept = isStandaloneStrongCodeWorthFence(items[i].meta)
		} else if heurCount >= 2 {
			accept = true
		} else if heurCount == 1 && spanLen == 1 && items[i].meta.hasSoftBreak && isMultiLineCodeText(items[i].meta.plainText) {
			accept = true
		}

		if !accept {
			out = append(out, items[i].block)
			i++
			continue
		}

		var segMetas []*paraMeta
		var segTexts []string
		var rawLines []string
		for k := i; k <= end; k++ {
			segMetas = append(segMetas, items[k].meta)
			segTexts = append(segTexts, items[k].meta.plainText)
		}
		indented := applyRelativeIndent(segMetas, segTexts)
		for idx, txt := range indented {
			rawLines = append(rawLines, txt)
			if i+idx < end && items[i+idx].blankLinesAfter > 0 {
				rawLines = append(rawLines, "")
			}
		}

		codeText := dedentCode(strings.Join(rawLines, "\n"))
		if codeText == "" {
			out = append(out, items[i].block)
			i++
			continue
		}

		// For pure heuristic multi-line blocks without strong formatting, double check overall block code score
		if strongCount == 0 && !isHighConfidenceCodeBlock(codeText) {
			for k := i; k <= end; k++ {
				out = append(out, items[k].block)
			}
			i = end + 1
			continue
		}

		lang := DetectCodeLanguage(codeText, leadIns[i])
		out = append(out, Block{
			Kind:     BlockCode,
			Text:     codeText,
			Language: lang,
		})
		i = end + 1
	}

	return out
}

func isHighConfidenceCodeBlock(code string) bool {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return false
	}
	if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && json.Valid([]byte(trimmed)) {
		return true
	}
	lines := strings.Split(trimmed, "\n")
	nonEmpty := 0
	totalScore := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		nonEmpty++
		totalScore += scoreCodeLine(l)
	}
	if nonEmpty == 0 {
		return false
	}
	if nonEmpty == 1 {
		return totalScore >= 3 && !isNaturalLanguageCallout(trimmed)
	}
	return totalScore >= nonEmpty+1 && !isNaturalLanguageCallout(trimmed)
}

func DetectCodeLanguage(code, leadIn string) string {
	if lang := detectLangFromHint(leadIn); lang != "" {
		return lang
	}
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return ""
	}
	if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && json.Valid([]byte(trimmed)) {
		return "json"
	}

	lines := strings.Split(trimmed, "\n")
	firstLine := strings.TrimSpace(lines[0])
	if httpRe.MatchString(firstLine) {
		return "http"
	}
	if strings.HasPrefix(firstLine, "#!/bin/bash") || strings.HasPrefix(firstLine, "#!/bin/sh") || strings.HasPrefix(firstLine, "#!/usr/bin/env bash") {
		return "bash"
	}
	if strings.HasPrefix(firstLine, "<?xml") {
		return "xml"
	}

	scores := map[string]int{}
	lower := strings.ToLower(trimmed)

	// Go
	if strings.Contains(trimmed, "package ") {
		scores["go"] += 3
	}
	if funcGoRe.MatchString(firstLine) || strings.Contains(trimmed, "\nfunc ") || strings.HasPrefix(trimmed, "func ") {
		scores["go"] += 3
	}
	if strings.Contains(trimmed, ":=") {
		scores["go"] += 3
	}
	if strings.Contains(trimmed, "error)") || strings.Contains(trimmed, "fmt.") || strings.Contains(trimmed, "struct {") || strings.Contains(trimmed, "interface {") {
		scores["go"] += 2
	}

	// Python
	if funcPyRe.MatchString(firstLine) || strings.Contains(trimmed, "\ndef ") {
		scores["python"] += 3
	}
	if strings.Contains(trimmed, "if __name__ ==") || strings.Contains(trimmed, "elif ") {
		scores["python"] += 3
	}
	if regexp.MustCompile(`(?m)^from\s+\S+\s+import\s+`).MatchString(trimmed) {
		scores["python"] += 2
	}
	if strings.Contains(trimmed, "None") || strings.Contains(trimmed, "self.") {
		scores["python"]++
	}

	// Java
	if strings.Contains(trimmed, "public class ") || strings.Contains(trimmed, "private final ") || strings.Contains(trimmed, "@Override") {
		scores["java"] += 3
	}
	if strings.Contains(trimmed, "System.out.") || strings.Contains(trimmed, "implements ") || strings.Contains(trimmed, "extends ") {
		scores["java"] += 2
	}

	// TypeScript / JavaScript
	if strings.Contains(trimmed, "interface ") && strings.Contains(trimmed, ": ") && strings.Contains(trimmed, "{") {
		scores["typescript"] += 3
	}
	if regexp.MustCompile(`(?m)\bexport\s+(interface|type)\s+`).MatchString(trimmed) {
		scores["typescript"] += 3
	}
	if strings.Contains(trimmed, ": string") || strings.Contains(trimmed, ": number") || strings.Contains(trimmed, ": boolean") {
		scores["typescript"] += 2
	}
	if strings.Contains(trimmed, "console.log") || strings.Contains(trimmed, "=>") || regexp.MustCompile(`(?m)\b(const|let)\s+\w+\s*=`).MatchString(trimmed) {
		scores["javascript"] += 2
		scores["typescript"]++
	}

	// SQL
	if regexp.MustCompile(`(?i)\bSELECT\b[\s\S]+\bFROM\b`).MatchString(trimmed) ||
		regexp.MustCompile(`(?i)\bCREATE\s+TABLE\b`).MatchString(trimmed) ||
		regexp.MustCompile(`(?i)\bINSERT\s+INTO\b`).MatchString(trimmed) ||
		regexp.MustCompile(`(?i)\bUPDATE\b[\s\S]+\bSET\b`).MatchString(trimmed) ||
		regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`).MatchString(trimmed) {
		scores["sql"] += 4
	}

	// Dockerfile
	if regexp.MustCompile(`(?m)^FROM\s+\S+`).MatchString(trimmed) &&
		regexp.MustCompile(`(?m)^(RUN|COPY|ADD|WORKDIR|ENV|EXPOSE|CMD|ENTRYPOINT)\s+`).MatchString(trimmed) {
		scores["dockerfile"] += 4
	}

	// Bash / Shell
	bashHits := 0
	for _, l := range lines {
		lt := strings.TrimSpace(l)
		if strings.HasPrefix(lt, "$ ") || strings.HasPrefix(lt, "curl ") || strings.HasPrefix(lt, "docker ") ||
			strings.HasPrefix(lt, "kubectl ") || strings.HasPrefix(lt, "export ") || strings.HasPrefix(lt, "sudo ") ||
			strings.HasPrefix(lt, "chmod ") || strings.HasPrefix(lt, "echo ") {
			bashHits++
		}
	}
	if bashHits >= 1 && !strings.Contains(trimmed, "{") {
		scores["bash"] += bashHits * 2
	}

	// XML / HTML
	if strings.HasPrefix(firstLine, "<") && strings.Contains(trimmed, ">") && strings.Contains(trimmed, "</") {
		if strings.Contains(lower, "<html") || strings.Contains(lower, "<div") || strings.Contains(lower, "<script") {
			scores["html"] += 3
		} else {
			scores["xml"] += 3
		}
	}

	// YAML
	yamlHits := 0
	for _, l := range lines {
		lt := strings.TrimSpace(l)
		if yamlKVRe.MatchString(lt) {
			yamlHits++
		}
	}
	if yamlHits >= 2 && !strings.Contains(trimmed, "{") && !strings.Contains(trimmed, ";") {
		scores["yaml"] += yamlHits + 1
	}

	bestLang := ""
	bestScore := 0
	for lang, sc := range scores {
		if sc > bestScore {
			bestScore = sc
			bestLang = lang
		}
	}
	if bestScore >= 3 {
		return bestLang
	}
	return ""
}

func detectLangFromHint(hint string) string {
	l := strings.ToLower(strings.TrimSpace(hint))
	if l == "" {
		return ""
	}
	checks := []struct {
		lang string
		kws  []string
	}{
		{"go", []string{"golang", "go代码", "go 语言", "go语言"}},
		{"python", []string{"python", "py代码"}},
		{"typescript", []string{"typescript", "ts代码"}},
		{"javascript", []string{"javascript", "js代码"}},
		{"java", []string{"java代码", "java语言", "java "}},
		{"sql", []string{"sql", "建表语句", "查询语句"}},
		{"json", []string{"json"}},
		{"yaml", []string{"yaml", "yml"}},
		{"xml", []string{"xml"}},
		{"html", []string{"html"}},
		{"bash", []string{"bash", "shell", "命令行", "终端命令"}},
		{"dockerfile", []string{"dockerfile"}},
		{"nginx", []string{"nginx"}},
		{"protobuf", []string{"protobuf", "proto"}},
	}
	for _, c := range checks {
		for _, kw := range c.kws {
			if strings.Contains(l, kw) {
				return c.lang
			}
		}
	}
	return ""
}
