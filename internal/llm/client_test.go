package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestComplete(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected path /chat/completions, got %s", r.URL.Path)
		}
		
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{"content": "hello world"},
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 5,
			},
			"model": "gpt-4o-mini",
		})
	}))
	defer ts.Close()

	client := New(Config{
		BaseURL: ts.URL,
		Model:   "gpt-4o-mini",
		Timeout: time.Second,
	})

	resp, err := client.Complete(context.Background(), Request{
		System: "You are a helpful assistant.",
		User:   "Say hello.",
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if resp.Content != "hello world" {
		t.Errorf("expected hello world, got %s", resp.Content)
	}
	if resp.Model != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %s", resp.Model)
	}
}
