package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type TelegramNotifier struct {
	name   string
	token  string
	chatID string
}

func NewTelegramNotifier(name, token, chatID string) *TelegramNotifier {
	return &TelegramNotifier{name: name, token: token, chatID: chatID}
}

func (n *TelegramNotifier) Name() string { return n.name }

func (n *TelegramNotifier) Send(ctx context.Context, notif Notification) error {
	text, err := Render("default", notif)
	if err != nil {
		return fmt.Errorf("render telegram template: %w", err)
	}

	payload := map[string]string{
		"chat_id": n.chatID,
		"text":    text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.token)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("create telegram request: %w", err)
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
		return fmt.Errorf("do telegram request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram returned status: %d", resp.StatusCode)
	}
	return nil
}
