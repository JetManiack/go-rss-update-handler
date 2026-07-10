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
	
	col := NewCollector(Config{
		Timeout:     time.Second,
		RatePerHost: 10,
		Retries:     1,
		BackoffBase: time.Millisecond,
		UserAgent:   "test",
	})
	
	// Test normal fetch
	res, err := col.Fetch(context.Background(), FeedRef{URL: server.URL})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if string(res.Body) != "hello" {
		t.Errorf("Expected hello, got %s", res.Body)
	}
	if res.ETag != "etag2" {
		t.Errorf("Expected etag2, got %s", res.ETag)
	}
	
	// Test NotModified
	res, err = col.Fetch(context.Background(), FeedRef{URL: server.URL, ETag: "etag1"})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if res.Body != nil {
		t.Errorf("Expected nil body for 304, got %s", res.Body)
	}
}
