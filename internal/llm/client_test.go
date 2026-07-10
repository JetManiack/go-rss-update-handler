package llm

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
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
