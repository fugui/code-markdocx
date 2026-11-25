package vlm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

func (c *Client) PolishLayout(ctx context.Context, in LayoutPolishInput) (*LayoutPolishResult, error) {
	if strings.TrimSpace(c.cfg.BaseURL) == "" || strings.TrimSpace(c.cfg.Model) == "" {
		return nil, fmt.Errorf("VLM 未配置 base_url 或 model")
	}
	if len(in.Blocks) == 0 {
		return &LayoutPolishResult{Summary: "文档块为空，无需调整"}, nil
	}

	messages := []map[string]any{
		{
			"role":    "system",
			"content": BuildLayoutPolishSystemPrompt(c.cfg.ExtraSystemPrompt),
		},
		{
			"role":    "user",
			"content": BuildLayoutPolishUserPrompt(in),
		},
	}

	var parsed *LayoutPolishResult
	err := c.callChatWithRetry(ctx, messages, func(rawContent string) error {
		res, parseErr := ParseLayoutPolishJSON(rawContent)
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

func ParseLayoutPolishJSON(raw string) (*LayoutPolishResult, error) {
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
		return nil, fmt.Errorf("LLM layout polish output does not contain a JSON object")
	}
	jsonSlice := cleaned[start : end+1]

	var res LayoutPolishResult
	if err := json.Unmarshal([]byte(jsonSlice), &res); err != nil {
		return nil, fmt.Errorf("parse LLM layout polish JSON: %w", err)
	}

	filtered := make([]LayoutPolishOp, 0, len(res.Ops))
	for _, op := range res.Ops {
		normOp := LayoutOpType(strings.ToLower(strings.TrimSpace(string(op.Op))))
		switch normOp {
		case LayoutOpSetHeading, LayoutOpSetParagraph, LayoutOpSetListItem, LayoutOpSetQuote, LayoutOpSetAlert, LayoutOpMergeParagraphs:
			op.Op = normOp
			if op.Op == LayoutOpSetAlert {
				op.AlertType = normalizeLayoutAlertType(op.AlertType)
				if op.AlertType == "" {
					continue
				}
			} else {
				op.AlertType = ""
			}
			filtered = append(filtered, op)
		}
	}
	res.Ops = filtered
	res.Summary = strings.TrimSpace(res.Summary)
	return &res, nil
}

func normalizeLayoutAlertType(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION":
		return strings.ToUpper(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func ProcessLayoutPolishBatch(
	ctx context.Context,
	client *Client,
	inputs []LayoutPolishInput,
	concurrency int,
	onProgress func(LayoutPolishProgress),
) (map[string]*LayoutPolishResult, error) {
	if concurrency <= 0 {
		concurrency = 3
	}
	total := len(inputs)
	results := make(map[string]*LayoutPolishResult, total)
	if total == 0 {
		return results, nil
	}

	var mu sync.Mutex
	var doneCount atomic.Int64
	var firstErr error
	var errOnce sync.Once

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, item := range inputs {
		item := item
		g.Go(func() error {
			res, err := client.PolishLayout(gCtx, item)
			if err != nil {
				errOnce.Do(func() {
					firstErr = err
				})
			} else if res != nil {
				mu.Lock()
				results[item.ChunkID] = res
				mu.Unlock()
			}
			curDone := int(doneCount.Add(1))
			if onProgress != nil {
				onProgress(LayoutPolishProgress{
					ChunkID: item.ChunkID,
					Done:    curDone,
					Total:   total,
					Result:  res,
					Err:     err,
				})
			}
			return nil
		})
	}
	_ = g.Wait()
	if len(results) == 0 && firstErr != nil {
		return results, firstErr
	}
	return results, nil
}
