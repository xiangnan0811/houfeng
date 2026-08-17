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

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return NewSendFailure(SendFailureTemporary)
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		return NewSendFailure(SendFailurePermanent)
	}
	return nil
}
