package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/http/handlers"
	centersettings "houfeng/internal/center/settings"
)

type fakeSettingsRepository struct {
	getSettingsResult centersettings.CenterSettings
	getSettingsErr    error
	putSettingsResult centersettings.CenterSettings
	putSettingsErr    error
	putSettingsInput  centersettings.CenterSettings
}

func (f *fakeSettingsRepository) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	if f.getSettingsErr != nil {
		return centersettings.CenterSettings{}, f.getSettingsErr
	}
	return f.getSettingsResult, nil
}

func (f *fakeSettingsRepository) PutSettings(_ context.Context, input centersettings.CenterSettings) (centersettings.CenterSettings, error) {
	f.putSettingsInput = input
	if f.putSettingsErr != nil {
		return centersettings.CenterSettings{}, f.putSettingsErr
	}
	return f.putSettingsResult, nil
}

func TestSettingsHandlerReturnsCurrentSettings(t *testing.T) {
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.Default()}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body centersettings.CenterSettings
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if body.HostSampleFrequencyTier != centersettings.Default().HostSampleFrequencyTier {
		t.Fatalf("expected host sample frequency tier %q, got %q", centersettings.Default().HostSampleFrequencyTier, body.HostSampleFrequencyTier)
	}
}

func TestSettingsHandlerUpdatesSettingsOnPut(t *testing.T) {
	updated := centersettings.Default()
	updated.Telegram.BotToken = "bot-token"
	updated.Telegram.ChatID = "chat-id"
	updated.HostSampleFrequencyTier = "1m"
	updated.ProbeFrequencyDefaults.HTTP = "1m"
	repo := &fakeSettingsRepository{putSettingsResult: updated}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"bot_token":"bot-token","chat_id":"chat-id"},"host_sample_frequency_tier":"1m","probe_frequency_defaults":{"tcp":"5m","http":"1m","tls":"5m"},"incident_defaults":{"heartbeat_interval_seconds":30,"stale_threshold_intervals":3,"sweep_interval_seconds":60,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"node_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":7,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if repo.putSettingsInput.HostSampleFrequencyTier != "1m" {
		t.Fatalf("expected persisted host sample frequency tier %q, got %q", "1m", repo.putSettingsInput.HostSampleFrequencyTier)
	}

	var body centersettings.CenterSettings
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Telegram.BotToken != "bot-token" {
		t.Fatalf("expected telegram bot token %q, got %q", "bot-token", body.Telegram.BotToken)
	}
}

func TestSettingsHandlerRejectsUnknownFieldsOnPut(t *testing.T) {
	repo := &fakeSettingsRepository{}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"bot_token":"bot-token","chat_id":"chat-id","unexpected":true},"host_sample_frequency_tier":"5m","probe_frequency_defaults":{"tcp":"5m","http":"5m","tls":"5m"},"incident_defaults":{"heartbeat_interval_seconds":30,"stale_threshold_intervals":3,"sweep_interval_seconds":60,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"node_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":7,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestSettingsHandlerMapsValidationFailureToBadRequest(t *testing.T) {
	repo := &fakeSettingsRepository{putSettingsErr: errors.Join(centersettings.ErrInvalidSettings, errors.New("bad settings"))}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"bot_token":"","chat_id":""},"host_sample_frequency_tier":"5m","probe_frequency_defaults":{"tcp":"5m","http":"5m","tls":"5m"},"incident_defaults":{"heartbeat_interval_seconds":30,"stale_threshold_intervals":3,"sweep_interval_seconds":60,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"node_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":7,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestSettingsHandlerMapsRepositoryFailureToInternalServerError(t *testing.T) {
	repo := &fakeSettingsRepository{getSettingsErr: errors.New("boom")}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}
}
