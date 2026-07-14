package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type SlackNotifier struct {
	name      string
	webhookURL string
}

func NewSlackNotifier(name, webhookURL string) *SlackNotifier {
	return &SlackNotifier{name: name, webhookURL: webhookURL}
}

func (n *SlackNotifier) Name() string { return n.name }

func (n *SlackNotifier) Send(ctx context.Context, notif Notification) error {
	text, err := Render("default", notif)
	if err != nil {
		return fmt.Errorf("render slack template: %w", err)
	}

	payload := map[string]string{
		"text": text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", n.webhookURL, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("create slack request: %w", err)
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
		return fmt.Errorf("do slack request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack returned status: %d", resp.StatusCode)
	}
	return nil
}
