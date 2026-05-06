package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		return fmt.Errorf("feishu: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("feishu: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("feishu: post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("feishu: unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}
