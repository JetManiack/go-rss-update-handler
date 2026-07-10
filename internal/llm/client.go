package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Request defines the input for the LLM.
type Request struct {
	System      string
	User        string
	JSONMode    bool // require application/json response
	MaxTokens   int
	Temperature float64
}

// Response defines the output from the LLM.
type Response struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
	Model            string
}

// Client defines the interface for LLM operations.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

type client struct {
	cfg        Config
	httpClient *http.Client
	tracer     trace.Tracer
}

// New creates a new LLM client.
func New(cfg Config) Client {
	return &client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		tracer: otel.Tracer("llm"),
	}
}

// Complete sends a request to the LLM API.
func (c *client) Complete(ctx context.Context, req Request) (Response, error) {
	ctx, span := c.tracer.Start(ctx, "Complete")
	defer span.End()

	url := fmt.Sprintf("%s/chat/completions", c.cfg.BaseURL)

	payload := map[string]any{
		"model":       c.cfg.Model,
		"temperature": req.Temperature,
		"messages": []map[string]string{
			{"role": "system", "content": req.System},
			{"role": "user", "content": req.User},
		},
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.JSONMode {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("llm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return Response{}, fmt.Errorf("llm: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	// TODO: implement retries with backoff here.
	var resp *http.Response
	for i := 0; i <= c.cfg.MaxRetries; i++ {
		resp, err = c.httpClient.Do(httpReq)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				break
			}
			// Only retry on 429 and 5xx
			if resp.StatusCode != http.StatusTooManyRequests && (resp.StatusCode < 500 || resp.StatusCode > 599) {
				defer resp.Body.Close()
				return Response{}, fmt.Errorf("llm: fatal status: %d", resp.StatusCode)
			}
			resp.Body.Close()
		}

		if i < c.cfg.MaxRetries {
			backoff := time.Duration(i+1) * time.Second // Simple linear backoff
			select {
			case <-ctx.Done():
				return Response{}, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	if err != nil {
		return Response{}, fmt.Errorf("llm: execute request: %w", err)
	}
	defer resp.Body.Close()

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return Response{}, fmt.Errorf("llm: decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return Response{}, fmt.Errorf("llm: no choices in response")
	}

	return Response{
		Content:          apiResp.Choices[0].Message.Content,
		PromptTokens:     apiResp.Usage.PromptTokens,
		CompletionTokens: apiResp.Usage.CompletionTokens,
		Model:            apiResp.Model,
	}, nil
}
