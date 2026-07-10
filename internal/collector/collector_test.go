package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCollector_Fetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == "etag1" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", "etag2")
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2025 07:28:00 GMT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()
	
	col := NewCollector(time.Second)
	
	// Test normal fetch
	body, etag, _, err := col.Fetch(context.Background(), server.URL, "", "")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("Expected hello, got %s", body)
	}
	if etag != "etag2" {
		t.Errorf("Expected etag2, got %s", etag)
	}
	
	// Test NotModified
	body, etag, _, err = col.Fetch(context.Background(), server.URL, "etag1", "")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if body != nil {
		t.Errorf("Expected nil body for 304, got %s", body)
	}
}
