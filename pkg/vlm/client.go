package vlm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ClientConfig struct {
	BaseURL           string
	APIKey            string
	Model             string
	Timeout           time.Duration
	MaxRetries        int
	Temperature       float64
	MaxTokens         int
	UserAgent         string
	ExtraSystemPrompt string
}

type Client struct {
	cfg        ClientConfig
	httpClient *http.Client
}

func NewClient(cfg ClientConfig) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4096
	}
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) ConvertImage(ctx context.Context, in ConvertImageInput) (*VLMResult, error) {
	return c.callVision(ctx, in, BuildImageUserPrompt(in))
}

func (c *Client) RepairImage(ctx context.Context, in RepairImageInput) (*VLMResult, error) {
	return c.callVision(ctx, in.ConvertImageInput, BuildRepairUserPrompt(in))
}

func (c *Client) callVision(ctx context.Context, in ConvertImageInput, userPrompt string) (*VLMResult, error) {
	if strings.TrimSpace(c.cfg.BaseURL) == "" || strings.TrimSpace(c.cfg.Model) == "" {
		return nil, fmt.Errorf("VLM 未配置 base_url 或 model")
	}
	if len(in.ImageBytes) == 0 {
		return nil, fmt.Errorf("图片内容为空 (%s)", in.ImageID)
	}
	ct := strings.TrimSpace(in.ContentType)
	if ct == "" {
		ct = "image/png"
	}
	dataURI := fmt.Sprintf("data:%s;base64,%s", ct, base64.StdEncoding.EncodeToString(in.ImageBytes))

	payload := map[string]any{
		"model":       c.cfg.Model,
		"temperature": c.cfg.Temperature,
		"max_tokens":  c.cfg.MaxTokens,
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": BuildSystemPrompt(c.cfg.ExtraSystemPrompt),
			},
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": userPrompt},
					{"type": "image_url", "image_url": map[string]string{"url": dataURI}},
				},
			},
		},
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal VLM payload: %w", err)
	}

	endpoint := strings.TrimRight(strings.TrimSpace(c.cfg.BaseURL), "/") + "/chat/completions"

	var lastErr error
	attempts := c.cfg.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		rawContent, retryable, err := c.doHTTP(ctx, endpoint, reqBody)
		if err != nil {
			lastErr = err
			if !retryable || ctx.Err() != nil {
				break
			}
			continue
		}

		parsed, parseErr := ParseVLMJSON(rawContent)
		if parseErr != nil {
			lastErr = parseErr
			// Retry once on malformed JSON if retries remain
			continue
		}
		return parsed, nil
	}

	return nil, lastErr
}

type chatCompletionResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) doHTTP(ctx context.Context, endpoint string, reqBody []byte) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	if c.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", false, err
		}
		return "", true, fmt.Errorf("VLM request error: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", true, fmt.Errorf("read VLM response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "", true, fmt.Errorf("VLM HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false, fmt.Errorf("VLM HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed chatCompletionResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false, fmt.Errorf("decode VLM HTTP response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", false, fmt.Errorf("VLM API error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", false, fmt.Errorf("VLM returned empty choices")
	}
	return parsed.Choices[0].Message.Content, false, nil
}

func ParseVLMJSON(raw string) (*VLMResult, error) {
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
		return nil, fmt.Errorf("VLM output does not contain a JSON object")
	}
	jsonSlice := cleaned[start : end+1]

	var res VLMResult
	if err := json.Unmarshal([]byte(jsonSlice), &res); err != nil {
		return nil, fmt.Errorf("parse VLM JSON: %w", err)
	}

	res.MermaidCode = stripCodeFence(res.MermaidCode)
	res.MarkdownTable = strings.TrimSpace(res.MarkdownTable)
	res.Summary = strings.TrimSpace(res.Summary)
	res.Notes = strings.TrimSpace(res.Notes)

	switch res.Category {
	case CategoryUMLDiagram, CategoryComplexArch, CategoryTableShot, CategoryUIOrDecorative:
	default:
		if res.MermaidCode != "" {
			res.Category = CategoryUMLDiagram
		} else if res.MarkdownTable != "" {
			res.Category = CategoryTableShot
		} else {
			res.Category = CategoryUIOrDecorative
		}
	}

	switch res.DiagramType {
	case DiagramSequence, DiagramClass, DiagramState, DiagramFlowchart, DiagramER, DiagramOther:
	default:
		res.DiagramType = DiagramOther
	}

	switch res.Confidence {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
	default:
		res.Confidence = ConfidenceMedium
	}

	return &res, nil
}

func stripCodeFence(code string) string {
	s := strings.TrimSpace(code)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) >= 2 {
		lines = lines[1:]
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			lines = lines[:len(lines)-1]
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return s
}
