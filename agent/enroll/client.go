package enroll

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"houfeng/internal/contracts/agentapi"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type RemoteError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *RemoteError) Error() string {
	parts := []string{fmt.Sprintf("remote error status %d", e.StatusCode)}
	if e.Code != "" {
		parts = append(parts, fmt.Sprintf("code=%s", e.Code))
	}
	if e.Message != "" {
		parts = append(parts, fmt.Sprintf("message=%s", e.Message))
	}
	return strings.Join(parts, " ")
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) Enroll(ctx context.Context, request agentapi.EnrollmentRequest) (*agentapi.EnrollmentResponse, error) {
	response := &agentapi.EnrollmentResponse{}
	if err := c.postJSON(ctx, agentapi.EnrollPath, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

func (c *Client) Sync(ctx context.Context, request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
	response := &agentapi.SyncResponse{}
	headers := map[string]string{}
	if strings.TrimSpace(request.SyncToken) != "" {
		headers["Authorization"] = "Bearer " + strings.TrimSpace(request.SyncToken)
	}
	if err := c.postJSON(ctx, agentapi.SyncPath, syncRequestPayloadFrom(request), response, headers); err != nil {
		return nil, err
	}
	return response, nil
}

type syncRequestPayload struct {
	MonitoringInstanceID string                                 `json:"monitoring_instance_id"`
	Heartbeats           []agentapi.MonitoringInstanceHeartbeat `json:"heartbeats,omitempty"`
	HostSamples          []agentapi.HostSamplePayload           `json:"host_samples,omitempty"`
	ProbeObservations    []agentapi.ProbeObservationPayload     `json:"probe_observations,omitempty"`
	IPQualityReports     []agentapi.IPQualityReportPayload      `json:"ip_quality_reports,omitempty"`
	CommandResults       []agentapi.CommandResult               `json:"command_results,omitempty"`
}

func syncRequestPayloadFrom(request agentapi.SyncRequest) syncRequestPayload {
	return syncRequestPayload{
		MonitoringInstanceID: request.MonitoringInstanceID,
		Heartbeats:           request.Heartbeats,
		HostSamples:          request.HostSamples,
		ProbeObservations:    request.ProbeObservations,
		IPQualityReports:     request.IPQualityReports,
		CommandResults:       request.CommandResults,
	}
}

func (c *Client) postJSON(ctx context.Context, path string, requestBody any, responseBody any, extraHeaders ...map[string]string) error {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, headers := range extraHeaders {
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("request failed with status %d; read body: %w", resp.StatusCode, readErr)
		}

		return decodeRemoteError(resp.StatusCode, body)
	}

	if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func decodeRemoteError(statusCode int, body []byte) error {
	var response agentapi.ErrorResponse
	if err := json.Unmarshal(body, &response); err == nil && (response.Code != "" || response.Message != "") {
		return &RemoteError{
			StatusCode: statusCode,
			Code:       response.Code,
			Message:    response.Message,
		}
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(statusCode)
	}

	return &RemoteError{
		StatusCode: statusCode,
		Message:    message,
	}
}
