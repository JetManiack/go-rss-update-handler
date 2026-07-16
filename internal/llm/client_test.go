package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/JetManiack/go-rss-update-handler/internal/metrics"
)

func TestComplete_RetryWithBody(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Second attempt, check body
		w.Header().Set("Content-Type", "application/json")

		// Read body to verify it's not empty
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		if buf.Len() == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1},"model":"test"}`))
	}))
	defer server.Close()

	client := New(Config{
		BaseURL:    server.URL,
		APIKey:     "test",
		Model:      "test",
		MaxRetries: 1,
	})

	_, err := client.Complete(context.Background(), Request{System: "sys", User: "user"})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}

func TestComplete_ErrorsOnTruncatedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}],"model":"test"}`))
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIKey: "x", Model: "test", MaxRetries: 0})
	if _, err := client.Complete(context.Background(), Request{}); err == nil {
		t.Fatal("expected an error for finish_reason=length")
	}
}

func TestComplete_ExhaustedRetriesReturnsStatusError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIKey: "x", Model: "test", MaxRetries: 1})
	_, err := client.Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected an error after retries are exhausted")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("expected the last HTTP status in the error, got %v", err)
	}
	if attempts.Load() != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts.Load())
	}
}

func TestComplete_RecordsMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":5},"model":"test"}`))
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIKey: "x", Model: "test"})

	before := testutil.ToFloat64(metrics.LLMRequests.WithLabelValues("ok"))
	beforeTok := testutil.ToFloat64(metrics.LLMTokens.WithLabelValues("prompt"))
	if _, err := client.Complete(context.Background(), Request{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if after := testutil.ToFloat64(metrics.LLMRequests.WithLabelValues("ok")); after <= before {
		t.Errorf("LLMRequests{ok} did not increment: before=%v after=%v", before, after)
	}
	if after := testutil.ToFloat64(metrics.LLMTokens.WithLabelValues("prompt")); after <= beforeTok {
		t.Errorf("LLMTokens{prompt} did not increment: before=%v after=%v", beforeTok, after)
	}
}

func TestComplete_InsecureTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"model":"test"}`))
	}))
	defer server.Close()

	// A default (verifying) client must reject the server's self-signed cert.
	secure := New(Config{BaseURL: server.URL, Model: "test", MaxRetries: 0})
	if _, err := secure.Complete(context.Background(), Request{}); err == nil {
		t.Fatal("expected a TLS verification failure without llm.tls.insecure")
	}

	// With insecure verification disabled, the request succeeds.
	insecure := New(Config{BaseURL: server.URL, Model: "test", MaxRetries: 0, TLS: TLSConfig{Insecure: true}})
	if _, err := insecure.Complete(context.Background(), Request{}); err != nil {
		t.Fatalf("insecure client should connect to the self-signed server: %v", err)
	}
}

// When a Request carries a Schema, the client must send an OpenAI-compatible
// json_schema response_format (strict) wrapping that exact schema.
func TestComplete_StructuredOutputSendsJSONSchema(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		body = buf.Bytes()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}],"model":"test"}`))
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIKey: "x", Model: "test", MaxRetries: 0})
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"foo":{"type":"string"}},"required":["foo"]}`)
	_, err := client.Complete(context.Background(), Request{
		System: "sys", User: "usr",
		Schema: schema, SchemaName: "my_schema",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var got struct {
		ResponseFormat struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Name   string          `json:"name"`
				Schema json.RawMessage `json:"schema"`
				Strict bool            `json:"strict"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode request body: %v\nbody=%s", err, body)
	}
	if got.ResponseFormat.Type != "json_schema" {
		t.Errorf("response_format.type = %q, want %q", got.ResponseFormat.Type, "json_schema")
	}
	if got.ResponseFormat.JSONSchema.Name != "my_schema" {
		t.Errorf("json_schema.name = %q, want %q", got.ResponseFormat.JSONSchema.Name, "my_schema")
	}
	if !got.ResponseFormat.JSONSchema.Strict {
		t.Error("json_schema.strict must be true")
	}
	var wantSchema, gotSchema map[string]any
	_ = json.Unmarshal(schema, &wantSchema)
	if err := json.Unmarshal(got.ResponseFormat.JSONSchema.Schema, &gotSchema); err != nil {
		t.Fatalf("json_schema.schema is not valid JSON: %v", err)
	}
	if gotSchema["type"] != "object" || gotSchema["additionalProperties"] != false {
		t.Errorf("schema not passed through verbatim: %v", gotSchema)
	}
}

// Without a Schema, the client must not send any response_format (the legacy
// json_object mode is gone).
func TestComplete_NoSchemaOmitsResponseFormat(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		body = buf.Bytes()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"model":"test"}`))
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIKey: "x", Model: "test", MaxRetries: 0})
	if _, err := client.Complete(context.Background(), Request{System: "s", User: "u"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if _, ok := got["response_format"]; ok {
		t.Errorf("response_format must be absent when no schema is set; body=%s", body)
	}
}

// A 400 that complains about response_format (provider does not support
// structured output) must surface a clear, actionable error.
func TestComplete_UnsupportedStructuredOutputError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"response_format json_schema is not supported by this model"}}`))
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIKey: "x", Model: "test", MaxRetries: 0})
	schema := json.RawMessage(`{"type":"object"}`)
	_, err := client.Complete(context.Background(), Request{Schema: schema, SchemaName: "s"})
	if err == nil {
		t.Fatal("expected an error when the provider rejects json_schema")
	}
	if !strings.Contains(err.Error(), "structured output") {
		t.Errorf("error should mention structured output, got: %v", err)
	}
}

func TestComplete_DoesNotRetry4xx(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIKey: "x", Model: "test", MaxRetries: 3})
	if _, err := client.Complete(context.Background(), Request{}); err == nil {
		t.Fatal("expected an error for a 4xx status")
	}
	if attempts.Load() != 1 {
		t.Errorf("4xx must not be retried: got %d attempts, want 1", attempts.Load())
	}
}
