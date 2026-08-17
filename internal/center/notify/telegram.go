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

type telegramProviderResponse struct {
	OK        *bool `json:"ok"`
	ErrorCode *int  `json:"error_code"`
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

	if failureClass, failed := classifyProviderHTTPStatus(response.StatusCode); failed {
		return NewSendFailure(failureClass)
	}
	var providerResponse telegramProviderResponse
	if !decodeBoundedProviderResponse(response.Body, &providerResponse) || providerResponse.OK == nil {
		return NewSendFailure(SendFailureUnknown)
	}
	if *providerResponse.OK {
		return nil
	}
	if providerResponse.ErrorCode != nil {
		if failureClass, closed := classifyClosedProviderCode(*providerResponse.ErrorCode); closed {
			return NewSendFailure(failureClass)
		}
	}
	return NewSendFailure(SendFailureUnknown)
}
