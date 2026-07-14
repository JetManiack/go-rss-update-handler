package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
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

func TestCollector_Fetch_RetriesOn500(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("retried"))
	}))
	defer server.Close()

	col := NewCollector(Config{
		Timeout:     time.Second,
		RatePerHost: 100,
		Retries:     1,
		BackoffBase: time.Millisecond,
		UserAgent:   "test",
	})

	res, err := col.Fetch(context.Background(), FeedRef{URL: server.URL})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if string(res.Body) != "retried" {
		t.Errorf("Expected retried, got %s", res.Body)
	}
	if attempts.Load() != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts.Load())
	}
}

func TestCollector_Fetch_RespectsRetryAfterOn429(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", strconv.Itoa(1))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	col := NewCollector(Config{
		Timeout:     time.Second,
		RatePerHost: 100,
		Retries:     1,
		BackoffBase: time.Millisecond, // tiny default backoff, Retry-After should dominate
		UserAgent:   "test",
	})

	start := time.Now()
	res, err := col.Fetch(context.Background(), FeedRef{URL: server.URL})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if string(res.Body) != "ok" {
		t.Errorf("Expected ok, got %s", res.Body)
	}
	if elapsed < time.Second {
		t.Errorf("expected Fetch to wait for Retry-After (>=1s), got %s", elapsed)
	}
}

func TestCollector_Fetch_DoesNotRetry404(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	col := NewCollector(Config{
		Timeout:     time.Second,
		RatePerHost: 100,
		Retries:     3,
		BackoffBase: time.Millisecond,
		UserAgent:   "test",
	})
	if _, err := col.Fetch(context.Background(), FeedRef{URL: server.URL}); err == nil {
		t.Fatal("expected an error for 404")
	}
	if attempts.Load() != 1 {
		t.Errorf("404 must not be retried: got %d attempts, want 1", attempts.Load())
	}
}

func TestCollector_Fetch_RejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 2048))
	}))
	defer server.Close()

	col := NewCollector(Config{
		Timeout:     time.Second,
		RatePerHost: 100,
		Retries:     2,
		BackoffBase: time.Millisecond,
		UserAgent:   "test",
		MaxBodySize: 1024,
	})
	if _, err := col.Fetch(context.Background(), FeedRef{URL: server.URL}); err == nil {
		t.Fatal("expected an error for an oversized body")
	}
}
