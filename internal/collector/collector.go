package collector

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type FeedRef struct {
	FeedID       string
	URL          string
	ETag         string
	LastModified string
}

type FetchResult struct {
	NotModified  bool
	Body         []byte
	ETag         string
	LastModified string
	FetchedAt    time.Time
}

// defaultMaxBodySize caps a feed response body when the config leaves it unset.
const defaultMaxBodySize int64 = 5 << 20 // 5 MiB

type Config struct {
	Timeout     time.Duration `koanf:"timeout"`
	RatePerHost float64       `koanf:"rate_per_host"`
	Retries     int           `koanf:"retries"`
	BackoffBase time.Duration `koanf:"backoff_base"`
	UserAgent   string        `koanf:"user_agent"`
	MaxBodySize int64         `koanf:"max_body_size"` // 0 = default
}

func (c Config) Validate() error {
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be > 0")
	}
	if c.RatePerHost <= 0 {
		return fmt.Errorf("rate per host must be > 0")
	}
	if c.Retries < 0 {
		return fmt.Errorf("retries must be >= 0")
	}
	if c.BackoffBase <= 0 {
		return fmt.Errorf("backoff base must be > 0")
	}
	return nil
}

type Collector struct {
	cfg         Config
	client      *http.Client
	limiters    map[string]*rate.Limiter
	maxBodySize int64
	mu          sync.Mutex
}

func NewCollector(cfg Config) *Collector {
	maxBody := cfg.MaxBodySize
	if maxBody <= 0 {
		maxBody = defaultMaxBodySize
	}
	return &Collector{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		limiters:    make(map[string]*rate.Limiter),
		maxBodySize: maxBody,
	}
}

func (c *Collector) getLimiter(host string) *rate.Limiter {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.limiters[host]; !ok {
		c.limiters[host] = rate.NewLimiter(rate.Limit(c.cfg.RatePerHost), 1)
	}
	return c.limiters[host]
}

func (c *Collector) Fetch(ctx context.Context, ref FeedRef) (FetchResult, error) {
	u, err := url.Parse(ref.URL)
	if err != nil {
		return FetchResult{}, err
	}
	limiter := c.getLimiter(u.Host)

	var lastErr error
	for i := 0; i <= c.cfg.Retries; i++ {
		if err := limiter.Wait(ctx); err != nil {
			return FetchResult{}, err
		}

		res, retryAfter, retryable, err := c.doFetch(ctx, ref)
		if err == nil {
			return res, nil
		}
		lastErr = err

		// Permanent failures (4xx, oversized body, malformed request) are not retried.
		if !retryable {
			return FetchResult{}, err
		}
		if i == c.cfg.Retries {
			break
		}

		// Backoff: honor Retry-After on 429, otherwise exponential backoff.
		backoff := time.Duration(float64(c.cfg.BackoffBase) * math.Pow(2, float64(i)))
		if retryAfter > 0 {
			backoff = retryAfter
		}
		select {
		case <-ctx.Done():
			return FetchResult{}, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return FetchResult{}, lastErr
}

// doFetch performs a single fetch attempt. The returned bool reports whether a
// non-nil error is retryable (network errors, 5xx and 429 are; 4xx and an
// oversized body are not). On a 429 response it also returns the Retry-After
// duration so the caller can honor it instead of the default backoff.
func (c *Collector) doFetch(ctx context.Context, ref FeedRef) (FetchResult, time.Duration, bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", ref.URL, nil)
	if err != nil {
		return FetchResult{}, 0, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)

	if ref.ETag != "" {
		req.Header.Set("If-None-Match", ref.ETag)
	}
	if ref.LastModified != "" {
		req.Header.Set("If-Modified-Since", ref.LastModified)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return FetchResult{}, 0, true, err // network/transport error: retryable
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return FetchResult{NotModified: true, ETag: ref.ETag, LastModified: ref.LastModified, FetchedAt: time.Now()}, 0, false, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return FetchResult{}, parseRetryAfter(resp.Header.Get("Retry-After")), true, fmt.Errorf("too many requests")
	case resp.StatusCode >= 500:
		return FetchResult{}, 0, true, fmt.Errorf("server error: %d", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		// 4xx (e.g. 404/410/400): permanent — do not retry.
		return FetchResult{}, 0, false, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Cap the body: read one extra byte to detect overflow.
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBodySize+1))
	if err != nil {
		return FetchResult{}, 0, true, err // read error: retryable
	}
	if int64(len(body)) > c.maxBodySize {
		return FetchResult{}, 0, false, fmt.Errorf("response body exceeds %d bytes", c.maxBodySize)
	}

	return FetchResult{
		Body:         body,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		FetchedAt:    time.Now(),
	}, 0, false, nil
}

// parseRetryAfter parses the Retry-After header value, which may be either
// a number of seconds or an HTTP-date. Returns 0 if the header is absent or
// cannot be parsed.
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
