package notify

import (
	"bytes"
	"context"
	"encoding/json"
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
		return NewSendFailure(SendFailurePermanent)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+"/bot"+n.botToken+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return NewSendFailure(SendFailurePermanent)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := n.httpClient.Do(request)
	if err != nil {
		return NewSendFailure(SendFailureUnknown)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
		return NewSendFailure(SendFailureTemporary)
	}
	if response.StatusCode >= http.StatusMultipleChoices {
		return NewSendFailure(SendFailurePermanent)
	}
	return nil
}
