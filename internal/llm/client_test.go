package llm

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
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
