package vlm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"golang.org/x/sync/errgroup"
)

func (c *Client) PolishCode(ctx context.Context, in CodePolishInput) (*CodePolishResult, error) {
	if strings.TrimSpace(c.cfg.BaseURL) == "" || strings.TrimSpace(c.cfg.Model) == "" {
		return nil, fmt.Errorf("VLM 未配置 base_url 或 model")
	}
	if len(in.Lines) == 0 {
		return nil, fmt.Errorf("待处理代码片段为空 (%s)", in.ID)
	}

	messages := []map[string]any{
		{
			"role":    "system",
			"content": BuildCodePolishSystemPrompt(c.cfg.ExtraSystemPrompt),
		},
		{
			"role":    "user",
			"content": BuildCodePolishUserPrompt(in),
		},
	}

	var parsed *CodePolishResult
	err := c.callChatWithRetry(ctx, messages, func(rawContent string) error {
		res, parseErr := ParseCodePolishJSON(rawContent)
		if parseErr != nil {
			return parseErr
		}
		parsed = res
		return nil
	})
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

func ParseCodePolishJSON(raw string) (*CodePolishResult, error) {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		if len(lines) >= 2 {
			lines = lines[1:]
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
				lines = lines[:len(lines)-1]
			}
			cleaned = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}

	start := strings.IndexByte(cleaned, '{')
	end := strings.LastIndexByte(cleaned, '}')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("LLM code polish output does not contain a JSON object")
	}
	jsonSlice := cleaned[start : end+1]

	var res CodePolishResult
	if err := json.Unmarshal([]byte(jsonSlice), &res); err != nil {
		return nil, fmt.Errorf("parse LLM code polish JSON: %w", err)
	}

	res.Language = normalizeCodeLanguage(res.Language)
	res.FormattedCode = stripCodeFencePreserveIndent(res.FormattedCode)
	res.Notes = strings.TrimSpace(res.Notes)
	return &res, nil
}

// VerifyCodePreservation ensures LLM formatting only altered whitespace/indentation
// and never added, removed, or modified any non-whitespace character.
func VerifyCodePreservation(orig, polished string) bool {
	if strings.TrimSpace(orig) == "" || strings.TrimSpace(polished) == "" {
		return false
	}
	stripWS := func(s string) string {
		var b strings.Builder
		b.Grow(len(s))
		for _, r := range s {
			if !unicode.IsSpace(r) {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	return stripWS(orig) == stripWS(polished)
}

func ProcessCodePolishBatch(
	ctx context.Context,
	client *Client,
	inputs []CodePolishInput,
	concurrency int,
	onProgress func(CodePolishProgress),
) map[string]*CodePolishResult {
	if concurrency <= 0 {
		concurrency = 3
	}
	total := len(inputs)
	results := make(map[string]*CodePolishResult, total)
	var mu sync.Mutex
	var doneCount atomic.Int64

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, item := range inputs {
		item := item
		g.Go(func() error {
			res, err := client.PolishCode(gCtx, item)
			if err == nil && res != nil {
				mu.Lock()
				results[item.ID] = res
				mu.Unlock()
			}
			curDone := int(doneCount.Add(1))
			if onProgress != nil {
				onProgress(CodePolishProgress{
					ID:     item.ID,
					Done:   curDone,
					Total:  total,
					Result: res,
					Err:    err,
				})
			}
			return nil
		})
	}
	_ = g.Wait()
	return results
}
