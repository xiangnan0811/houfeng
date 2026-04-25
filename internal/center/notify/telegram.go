package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type TelegramNotifier struct {
	baseURL    string
	botToken   string
	chatID     string
	httpClient *http.Client
}

func NewTelegramNotifier(botToken, chatID string) *TelegramNotifier {
	return NewTelegramNotifierWithBaseURL(botToken, chatID, "https://api.telegram.org", &http.Client{Timeout: 10 * time.Second})
}

func NewTelegramNotifierWithBaseURL(botToken, chatID, baseURL string, httpClient *http.Client) *TelegramNotifier {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &TelegramNotifier{
		baseURL:    strings.TrimRight(baseURL, "/"),
		botToken:   botToken,
		chatID:     chatID,
		httpClient: httpClient,
	}
}

func (n *TelegramNotifier) Send(ctx context.Context, summary string) error {
	payload, err := json.Marshal(map[string]string{
		"chat_id": n.chatID,
		"text":    summary,
	})
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+"/bot"+n.botToken+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build telegram request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := n.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send telegram request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("telegram send failed status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
