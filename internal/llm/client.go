package llm

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/JetManiack/go-rss-update-handler/internal/metrics"
)

// Request defines the input for the LLM.
type Request struct {
	System      string
	User        string
	MaxTokens   int
	Temperature float64

	// Schema, when non-nil, requests structured output: the response is
	// constrained to this JSON Schema via an OpenAI-compatible json_schema
	// response_format (strict). SchemaName names that wrapper.
	Schema     json.RawMessage
	SchemaName string
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
	sem        chan struct{}
}

// New creates a new LLM client.
func New(cfg Config) Client {
	concurrency := cfg.MaxConcurrent
	if concurrency <= 0 {
		concurrency = 1
	}
	httpClient := &http.Client{Timeout: cfg.Timeout}
	if cfg.TLS.Insecure {
		// Opt-in only: skip certificate verification for self-signed/local endpoints.
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- gated behind llm.tls.insecure
		}
	}
	return &client{
		cfg:        cfg,
		httpClient: httpClient,
		tracer:     otel.Tracer("llm"),
		sem:        make(chan struct{}, concurrency),
	}
}

// statusError is a non-2xx HTTP response from the LLM API.
type statusError struct {
	code       int
	retryable  bool
	retryAfter time.Duration
	structured bool // 400 rejecting the json_schema response_format
}

func (e *statusError) Error() string {
	if e.structured {
		return fmt.Sprintf("llm: status %d: request used structured output (json_schema) — the served model may not support it", e.code)
	}
	return fmt.Sprintf("llm: status %d", e.code)
}

// Complete sends a request to the LLM API, honoring the concurrency limit,
// retrying network errors / 429 / 5xx (respecting Retry-After), and failing
// fast on other 4xx.
func (c *client) Complete(ctx context.Context, req Request) (Response, error) {
	// Concurrency gate: protects the provider's rate limits and our budget.
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return Response{}, ctx.Err()
	}

	start := time.Now()
	out, err := c.doComplete(ctx, req)
	metrics.LLMDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.LLMRequests.WithLabelValues("error").Inc()
	} else {
		metrics.LLMRequests.WithLabelValues("ok").Inc()
		metrics.LLMTokens.WithLabelValues("prompt").Add(float64(out.PromptTokens))
		metrics.LLMTokens.WithLabelValues("completion").Add(float64(out.CompletionTokens))
	}
	return out, err
}

func (c *client) doComplete(ctx context.Context, req Request) (Response, error) {
	ctx, span := c.tracer.Start(ctx, "llm.Complete")
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
	if len(req.Schema) > 0 {
		payload["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   req.SchemaName,
				"schema": req.Schema, // json.RawMessage: embedded verbatim
				"strict": true,
			},
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("llm: marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		resp, reqErr := c.doRequest(ctx, url, body)
		if reqErr == nil {
			return parseResponse(resp)
		}
		lastErr = reqErr

		// Other 4xx are permanent — do not retry.
		var se *statusError
		if errors.As(reqErr, &se) && !se.retryable {
			return Response{}, reqErr
		}
		if attempt == c.cfg.MaxRetries {
			break
		}

		retryAfter := time.Duration(0)
		if errors.As(reqErr, &se) {
			retryAfter = se.retryAfter
		}
		if err := sleepBackoff(ctx, attempt, retryAfter); err != nil {
			return Response{}, err
		}
	}
	return Response{}, fmt.Errorf("llm: request failed after %d attempt(s): %w", c.cfg.MaxRetries+1, lastErr)
}

// doRequest performs one attempt. On HTTP 200 it returns the live response for
// the caller to read/close. On a non-2xx status it closes the body and returns
// a *statusError. A transport error is returned wrapped (and is retryable).
func (c *client) doRequest(ctx context.Context, url string, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: execute request: %w", err) // transport error: retryable
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}

	se := &statusError{code: resp.StatusCode}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		se.retryable = true
		se.retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
	case resp.StatusCode >= 500 && resp.StatusCode <= 599:
		se.retryable = true
	case resp.StatusCode == http.StatusBadRequest && bytes.Contains(body, []byte(`"json_schema"`)):
		// We asked for structured output and the provider rejected the request;
		// the served model most likely can't honor json_schema. Surface an
		// actionable hint instead of a bare "status 400".
		se.structured = true
	}
	_ = resp.Body.Close()
	return nil, se
}

func parseResponse(resp *http.Response) (Response, error) {
	defer func() { _ = resp.Body.Close() }()

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
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
	if apiResp.Choices[0].FinishReason == "length" {
		return Response{}, fmt.Errorf("llm: response truncated (finish_reason=length)")
	}

	return Response{
		Content:          apiResp.Choices[0].Message.Content,
		PromptTokens:     apiResp.Usage.PromptTokens,
		CompletionTokens: apiResp.Usage.CompletionTokens,
		Model:            apiResp.Model,
	}, nil
}

// sleepBackoff waits before the next attempt: Retry-After when provided,
// otherwise a simple linear backoff. Returns ctx.Err() if cancelled.
func sleepBackoff(ctx context.Context, attempt int, retryAfter time.Duration) error {
	backoff := time.Duration(attempt+1) * time.Second
	if retryAfter > 0 {
		backoff = retryAfter
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(backoff):
		return nil
	}
}

// parseRetryAfter parses a Retry-After header value (delta-seconds or HTTP-date).
// Returns 0 when absent or unparseable.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
