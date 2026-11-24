package mdpolish

import (
	"regexp"
	"strings"
	"unicode"

	"code-markdocx/pkg/vlm"
)

type SecretWarning struct {
	Line    int    `json:"line"`
	Rule    string `json:"rule"`
	Snippet string `json:"snippet"`
}

type ImageStats struct {
	Total             int `json:"total"`
	Done              int `json:"done"`
	Pending           int `json:"pending"`
	Failed            int `json:"failed"`
	Skipped           int `json:"skipped"`
	LowConfidence     int `json:"low_confidence"`
	UnsupportedVector int `json:"unsupported_vector"`
}

type SplitSuggestion struct {
	Filename        string `json:"filename"`
	Title           string `json:"title"`
	ByteSize        int    `json:"byte_size"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

type PreflightReport struct {
	ByteSize          int               `json:"byte_size"`
	MaxBytes          int64             `json:"max_bytes"`
	ExceedsSizeLimit  bool              `json:"exceeds_size_limit"`
	EstimatedTokens   int               `json:"estimated_tokens"`
	LineCount         int               `json:"line_count"`
	HeadingCount      int               `json:"heading_count"`
	MermaidBlockCount int               `json:"mermaid_block_count"`
	HasInlineBase64   bool              `json:"has_inline_base64"`
	ImageStats        ImageStats        `json:"image_stats"`
	SecretWarnings    []SecretWarning   `json:"secret_warnings"`
	SplitSuggestions  []SplitSuggestion `json:"split_suggestions,omitempty"`
}

var (
	base64DataImgRe = regexp.MustCompile(`data:image/[a-zA-Z0-9+.-]+;base64,[A-Za-z0-9+/=]{100,}`)
	pemRe           = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
	tokenPrefixRe   = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{16,}|ghp_[A-Za-z0-9]{20,}|glpat-[A-Za-z0-9_-]{16,}|AKIA[0-9A-Z]{16})\b`)
	urlCredRe       = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:[^\s/@]+@[^\s]+`)
	pwdAssignRe     = regexp.MustCompile(`(?i)\b(password|passwd|secret|api_key|access_token)\s*[:=]\s*["']?[^\s"']{6,}`)
)

func EstimateTextTokens(s string) int {
	if s == "" {
		return 0
	}
	var cjk, asciiWords int
	inWord := false
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || (r >= 0x3040 && r <= 0x30FF) {
			cjk++
			inWord = false
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if !inWord {
				asciiWords++
				inWord = true
			}
		} else {
			inWord = false
		}
	}
	return int(float64(cjk)*1.5 + float64(asciiWords)*1.3 + float64(strings.Count(s, "\n"))*0.5)
}

func Analyze(markdown string, states []*ImageState, maxBytes int64) PreflightReport {
	if maxBytes <= 0 {
		maxBytes = 524288
	}
	rep := PreflightReport{
		ByteSize:         len(markdown),
		MaxBytes:         maxBytes,
		ExceedsSizeLimit: int64(len(markdown)) > maxBytes,
		EstimatedTokens:  EstimateTextTokens(markdown),
		HasInlineBase64:  base64DataImgRe.MatchString(markdown),
		SecretWarnings:   []SecretWarning{},
	}

	lines := strings.Split(markdown, "\n")
	if markdown == "" {
		lines = nil
	}
	rep.LineCount = len(lines)

	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if !inFence && strings.HasPrefix(trimmed, "```mermaid") {
				rep.MermaidBlockCount++
			}
			inFence = !inFence
		} else if !inFence && strings.HasPrefix(trimmed, "#") {
			rep.HeadingCount++
		}

		lineNo := i + 1
		if pemRe.MatchString(line) {
			rep.SecretWarnings = append(rep.SecretWarnings, SecretWarning{
				Line: lineNo, Rule: "pem_private_key", Snippet: "-----BEGIN ... PRIVATE KEY-----",
			})
		} else if m := tokenPrefixRe.FindString(line); m != "" {
			rep.SecretWarnings = append(rep.SecretWarnings, SecretWarning{
				Line: lineNo, Rule: "token_prefix", Snippet: maskSecret(m),
			})
		} else if m := urlCredRe.FindString(line); m != "" {
			rep.SecretWarnings = append(rep.SecretWarnings, SecretWarning{
				Line: lineNo, Rule: "url_credential", Snippet: maskSecret(m),
			})
		} else if m := pwdAssignRe.FindString(line); m != "" {
			rep.SecretWarnings = append(rep.SecretWarnings, SecretWarning{
				Line: lineNo, Rule: "key_assignment", Snippet: maskSecret(m),
			})
		}
	}

	rep.ImageStats.Total = len(states)
	for _, st := range states {
		if st == nil {
			continue
		}
		if st.Skipped || st.Status == StatusSkipped {
			rep.ImageStats.Skipped++
			continue
		}
		if st.UnsupportedVector || st.Status == StatusUnsupportedVector {
			rep.ImageStats.UnsupportedVector++
			continue
		}
		switch st.Status {
		case StatusDone:
			rep.ImageStats.Done++
			if st.Confidence == vlm.ConfidenceLow {
				rep.ImageStats.LowConfidence++
			}
		case StatusFailed:
			rep.ImageStats.Failed++
		default:
			rep.ImageStats.Pending++
		}
	}

	if rep.ExceedsSizeLimit {
		chunks := SplitByH2(markdown, "doc", maxBytes)
		for _, ch := range chunks {
			rep.SplitSuggestions = append(rep.SplitSuggestions, SplitSuggestion{
				Filename:        ch.Filename,
				Title:           ch.Title,
				ByteSize:        len(ch.Content),
				EstimatedTokens: EstimateTextTokens(ch.Content),
			})
		}
	}

	return rep
}

func maskSecret(s string) string {
	r := []rune(s)
	if len(r) <= 8 {
		return "****"
	}
	return string(r[:4]) + "****" + string(r[len(r)-4:])
}
