package collector

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Collector struct {
	client *http.Client
}

func NewCollector(timeout time.Duration) *Collector {
	return &Collector{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Collector) Fetch(ctx context.Context, url string, etag, lastModified string) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", "", err
	}
	
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, lastModified, nil
	}
	
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", err
	}
	
	newETag := resp.Header.Get("ETag")
	newLastModified := resp.Header.Get("Last-Modified")
	
	return body, newETag, newLastModified, nil
}
