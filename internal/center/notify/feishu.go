package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type FeishuNotifier struct {
	webhookURL string
	client     *http.Client
}

type feishuProviderResponse struct {
	Code *int `json:"code"`
}

func NewFeishuNotifier(webhookURL string) *FeishuNotifier {
	return NewFeishuNotifierWithClient(webhookURL, &http.Client{Timeout: 10 * time.Second})
}

func NewFeishuNotifierWithClient(webhookURL string, client *http.Client) *FeishuNotifier {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &FeishuNotifier{
		webhookURL: webhookURL,
		client:     client,
	}
}

func (n *FeishuNotifier) Send(ctx context.Context, summary string) error {
	body := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]interface{}{
			"text": summary,
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return NewSendFailure(SendFailurePermanent)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(data))
	if err != nil {
		return NewSendFailure(SendFailurePermanent)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return NewSendFailure(SendFailureUnknown)
	}
	defer resp.Body.Close()

	if failureClass, failed := classifyProviderHTTPStatus(resp.StatusCode); failed {
		return NewSendFailure(failureClass)
	}
	var providerResponse feishuProviderResponse
	if !decodeBoundedProviderResponse(resp.Body, &providerResponse) || providerResponse.Code == nil {
		return NewSendFailure(SendFailureUnknown)
	}
	if *providerResponse.Code == 0 {
		return nil
	}
	return NewSendFailure(SendFailureUnknown)
}
