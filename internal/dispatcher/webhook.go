package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type WebhookNotifier struct {
	name string
	url  string
}

func NewWebhookNotifier(name, url string) *WebhookNotifier {
	return &WebhookNotifier{name: name, url: url}
}

func (n *WebhookNotifier) Name() string { return n.name }

func (n *WebhookNotifier) Send(ctx context.Context, notif Notification) error {
	content, err := Render("default", notif)
	if err != nil {
		return fmt.Errorf("render webhook template: %w", err)
	}

	payload := map[string]any{
		"content": content,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", n.url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	var resp *http.Response
	for i := 0; i < 3; i++ {
		resp, err = http.DefaultClient.Do(req)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	if err != nil {
		return fmt.Errorf("do webhook request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status: %d", resp.StatusCode)
	}

	return nil
}
