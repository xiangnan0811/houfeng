package enroll

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	RetryAfter time.Duration
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

// CheckSyncProtocol must succeed before opening or rewriting the durable queue.
func (c *Client) CheckSyncProtocol(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/healthz", nil)
	if err != nil {
		return fmt.Errorf("build sync protocol check: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("check sync protocol: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("check sync protocol: health status %d", resp.StatusCode)
	}
	var health struct {
		Name                      string `json:"name"`
		Status                    string `json:"status"`
		AgentSyncProtocolRevision int    `json:"agent_sync_protocol_revision"`
		AgentSyncReplayWindow     int64  `json:"agent_sync_replay_window"`
	}
	const maxHealthBytes = 64 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHealthBytes+1))
	if err != nil {
		return fmt.Errorf("read sync protocol health: %w", err)
	}
	if len(body) > maxHealthBytes {
		return fmt.Errorf("check sync protocol: health response too large")
	}
	if err := json.Unmarshal(body, &health); err != nil {
		return fmt.Errorf("decode sync protocol health: %w", err)
	}
	if health.Name != "houfeng-center" || health.Status != "ok" ||
		health.AgentSyncProtocolRevision != agentapi.SyncProtocolRevision ||
		health.AgentSyncReplayWindow != agentapi.SyncReplayWindow {
		return fmt.Errorf("check sync protocol: incompatible Center health capabilities")
	}
	return nil
}

func (c *Client) Sync(ctx context.Context, request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
	response := &agentapi.SyncResponse{}
	headers := map[string]string{
		agentapi.SyncProtocolHeader: strconv.Itoa(agentapi.SyncProtocolRevision),
	}
	if strings.TrimSpace(request.SyncToken) != "" {
		headers["Authorization"] = "Bearer " + strings.TrimSpace(request.SyncToken)
	}
	if request.LiveSignal != nil {
		headers[agentapi.IPQualityCollectionRevisionHeader] = "5"
	}
	if err := c.postJSON(ctx, agentapi.SyncPath, syncRequestPayloadFrom(request), response, headers); err != nil {
		return nil, err
	}
	return response, nil
}

type syncRequestPayload struct {
	ProtocolRevision     int                                    `json:"protocol_revision"`
	ProducerID           string                                 `json:"producer_id"`
	BatchSequence        int64                                  `json:"batch_sequence"`
	AllocatedThrough     int64                                  `json:"allocated_through"`
	RetiredThrough       int64                                  `json:"retired_through"`
	SessionID            string                                 `json:"session_id"`
	LiveSignal           *agentapi.LiveSignal                   `json:"live_signal,omitempty"`
	MonitoringInstanceID string                                 `json:"monitoring_instance_id"`
	Heartbeats           []agentapi.MonitoringInstanceHeartbeat `json:"heartbeats,omitempty"`
	HostSamples          []agentapi.HostSamplePayload           `json:"host_samples,omitempty"`
	ProbeObservations    []agentapi.ProbeObservationPayload     `json:"probe_observations,omitempty"`
	IPQualityReports     []agentapi.IPQualityReportPayload      `json:"ip_quality_reports,omitempty"`
	CommandResults       []agentapi.CommandResult               `json:"command_results,omitempty"`
}

func syncRequestPayloadFrom(request agentapi.SyncRequest) syncRequestPayload {
	return syncRequestPayload{
		ProtocolRevision:     request.ProtocolRevision,
		ProducerID:           request.ProducerID,
		BatchSequence:        request.BatchSequence,
		AllocatedThrough:     request.AllocatedThrough,
		RetiredThrough:       request.RetiredThrough,
		SessionID:            request.SessionID,
		LiveSignal:           request.LiveSignal,
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

		return decodeRemoteError(resp.StatusCode, body, resp.Header.Get("Retry-After"), time.Now())
	}

	if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func decodeRemoteError(statusCode int, body []byte, retryAfterHeader string, now time.Time) error {
	var retryAfter time.Duration
	if statusCode == http.StatusTooManyRequests || statusCode == http.StatusServiceUnavailable {
		retryAfter = parseRetryAfter(retryAfterHeader, now)
	}
	var response agentapi.ErrorResponse
	if err := json.Unmarshal(body, &response); err == nil && (response.Code != "" || response.Message != "") {
		return &RemoteError{
			StatusCode: statusCode,
			Code:       response.Code,
			Message:    response.Message,
			RetryAfter: retryAfter,
		}
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(statusCode)
	}

	return &RemoteError{
		StatusCode: statusCode,
		Message:    message,
		RetryAfter: retryAfter,
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	const fallback = 5 * time.Second
	if value == "" {
		return fallback
	}
	digitsOnly := true
	for _, char := range value {
		if char < '0' || char > '9' {
			digitsOnly = false
			break
		}
	}
	if digitsOnly {
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil || seconds == 0 {
			return fallback
		}
		if seconds > 60 {
			seconds = 60
		}
		return time.Duration(seconds) * time.Second
	}
	date, err := http.ParseTime(value)
	if err != nil {
		return fallback
	}
	wait := date.Sub(now)
	if wait < time.Second {
		return time.Second
	}
	if wait > time.Minute {
		return time.Minute
	}
	return wait
}
