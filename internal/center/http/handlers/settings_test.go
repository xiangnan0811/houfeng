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
	putSettingsCalls  int
}

type settingsHandlerResponse struct {
	Telegram                telegramSettingsResponse              `json:"telegram"`
	HostSampleFrequencyTier string                                `json:"host_sample_frequency_tier"`
	ProbeFrequencyDefaults  centersettings.ProbeFrequencyDefaults `json:"probe_frequency_defaults"`
	IncidentDefaults        centersettings.IncidentDefaults       `json:"incident_defaults"`
	OverrideRules           centersettings.OverrideRules          `json:"override_rules"`
	RetentionPolicy         centersettings.RetentionPolicy        `json:"retention_policy"`
}

type telegramSettingsResponse struct {
	BotToken           string `json:"bot_token"`
	ChatID             string `json:"chat_id"`
	TokenPresent       bool   `json:"token_present"`
	TokenMaskedSummary string `json:"token_masked_summary"`
	RuntimeManaged     bool   `json:"runtime_managed"`
	RuntimeApplyActive bool   `json:"runtime_apply_active"`
}

func (f *fakeSettingsRepository) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	if f.getSettingsErr != nil {
		return centersettings.CenterSettings{}, f.getSettingsErr
	}
	return f.getSettingsResult, nil
}

func (f *fakeSettingsRepository) PutSettings(_ context.Context, input centersettings.CenterSettings) (centersettings.CenterSettings, error) {
	f.putSettingsCalls++
	f.putSettingsInput = input
	if f.putSettingsErr != nil {
		return centersettings.CenterSettings{}, f.putSettingsErr
	}
	return f.putSettingsResult, nil
}

func TestSettingsHandlerReturnsCurrentSettingsWithoutTelegramBotToken(t *testing.T) {
	record := centersettings.Default()
	record.Telegram.BotToken = "123456:ABCDEF-secret-token"
	record.Telegram.ChatID = "chat-id"
	record.Telegram.RuntimeManaged = false
	repo := &fakeSettingsRepository{getSettingsResult: record}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body settingsHandlerResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if body.Telegram.BotToken != "" {
		t.Fatalf("expected bot token to be redacted from response, got %q", body.Telegram.BotToken)
	}
	if !body.Telegram.TokenPresent {
		t.Fatal("expected token_present to be true")
	}
	if body.Telegram.TokenMaskedSummary == "" {
		t.Fatal("expected token_masked_summary to be set")
	}
	if strings.Contains(body.Telegram.TokenMaskedSummary, "secret-token") {
		t.Fatalf("expected token_masked_summary to hide raw token, got %q", body.Telegram.TokenMaskedSummary)
	}
	if body.Telegram.RuntimeApplyActive {
		t.Fatal("expected runtime_apply_active to stay false until persisted Telegram settings explicitly manage runtime")
	}
	if body.Telegram.RuntimeManaged {
		t.Fatal("expected runtime_managed to stay false until persisted Telegram settings explicitly manage runtime")
	}
	if body.HostSampleFrequencyTier != centersettings.Default().HostSampleFrequencyTier {
		t.Fatalf("expected host sample frequency tier %q, got %q", centersettings.Default().HostSampleFrequencyTier, body.HostSampleFrequencyTier)
	}
}

func TestSettingsHandlerReturnsManagedTelegramDisableStateTruthfully(t *testing.T) {
	record := centersettings.Default()
	record.Telegram.RuntimeManaged = true
	repo := &fakeSettingsRepository{getSettingsResult: record}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body settingsHandlerResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if !body.Telegram.RuntimeManaged {
		t.Fatal("expected runtime_managed to stay true when persisted Telegram settings explicitly manage runtime")
	}
	if body.Telegram.RuntimeApplyActive {
		t.Fatal("expected runtime_apply_active to stay false when persisted Telegram settings explicitly disable delivery")
	}
}

func TestSettingsHandlerUpdatesSettingsOnPutWithoutEchoingTelegramBotToken(t *testing.T) {
	current := centersettings.Default()
	current.Telegram.BotToken = "current-token"
	current.Telegram.ChatID = "chat-id"
	current.Telegram.RuntimeManaged = false

	updated := centersettings.Default()
	updated.Telegram.BotToken = "bot-token"
	updated.Telegram.ChatID = "chat-id"
	updated.Telegram.RuntimeManaged = true
	updated.HostSampleFrequencyTier = "5s"
	updated.ProbeFrequencyDefaults.HTTP = "5s"
	repo := &fakeSettingsRepository{getSettingsResult: current, putSettingsResult: updated}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"bot_token":"bot-token","chat_id":"chat-id","runtime_managed":true},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":3,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"node_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":7,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if repo.putSettingsInput.HostSampleFrequencyTier != "5s" {
		t.Fatalf("expected persisted host sample frequency tier %q, got %q", "5s", repo.putSettingsInput.HostSampleFrequencyTier)
	}
	if repo.putSettingsInput.Telegram.BotToken != "bot-token" {
		t.Fatalf("expected repository input bot token %q, got %q", "bot-token", repo.putSettingsInput.Telegram.BotToken)
	}
	if !repo.putSettingsInput.Telegram.RuntimeManaged {
		t.Fatal("expected repository input runtime_managed to be true")
	}

	var body settingsHandlerResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Telegram.BotToken != "" {
		t.Fatalf("expected bot token to be redacted from response, got %q", body.Telegram.BotToken)
	}
	if body.Telegram.ChatID != "chat-id" {
		t.Fatalf("expected telegram chat id %q, got %q", "chat-id", body.Telegram.ChatID)
	}
	if !body.Telegram.TokenPresent {
		t.Fatal("expected token_present to be true")
	}
	if !body.Telegram.RuntimeManaged {
		t.Fatal("expected runtime_managed to be true once persisted Telegram settings explicitly manage runtime")
	}
	if !body.Telegram.RuntimeApplyActive {
		t.Fatal("expected runtime_apply_active to be true once persisted Telegram settings drive the live notifier path")
	}
}

func TestSettingsHandlerPreservesExistingTelegramTokenWhenBotTokenIsOmitted(t *testing.T) {
	current := centersettings.Default()
	current.Telegram.BotToken = "current-token"
	current.Telegram.ChatID = "chat-id"
	current.Telegram.RuntimeManaged = false

	updated := centersettings.Default()
	updated.Telegram.BotToken = "current-token"
	updated.Telegram.ChatID = "chat-id"
	updated.Telegram.RuntimeManaged = false
	updated.HostSampleFrequencyTier = "5s"
	repo := &fakeSettingsRepository{getSettingsResult: current, putSettingsResult: updated}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"chat_id":"chat-id"},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":3,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"node_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":14,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if repo.putSettingsInput.Telegram.BotToken != "current-token" {
		t.Fatalf("expected existing bot token to be preserved, got %q", repo.putSettingsInput.Telegram.BotToken)
	}
	if repo.putSettingsInput.Telegram.ChatID != "chat-id" {
		t.Fatalf("expected telegram chat id %q, got %q", "chat-id", repo.putSettingsInput.Telegram.ChatID)
	}
	if repo.putSettingsInput.Telegram.RuntimeManaged {
		t.Fatal("expected omitted runtime_managed to preserve false for unrelated saves")
	}
}

func TestSettingsHandlerPreservesEffectiveFreshInstallSettingsOnUnrelatedSave(t *testing.T) {
	coreTier := "5s"
	current := centersettings.Default()
	current.IncidentDefaults.SweepIntervalSeconds = 90
	current.OverrideRules.NodeLabels = []centersettings.NodeLabelOverrideRule{{
		Label: "核心",
		Overrides: centersettings.SettingsOverrideFields{
			HostSampleFrequencyTier: &coreTier,
		},
	}}

	repo := &fakeSettingsRepository{
		getSettingsResult: current,
		putSettingsResult: current,
	}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"chat_id":"","runtime_managed":false},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":3,"sweep_interval_seconds":90,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"node_labels":[{"label":"核心","overrides":{"host_sample_frequency_tier":"5s"}}],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":14,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if repo.putSettingsInput.IncidentDefaults.SweepIntervalSeconds != 90 {
		t.Fatalf("SweepIntervalSeconds = %d, want %d", repo.putSettingsInput.IncidentDefaults.SweepIntervalSeconds, 90)
	}
	if len(repo.putSettingsInput.OverrideRules.NodeLabels) != 1 {
		t.Fatalf("len(NodeLabelOverrides) = %d, want 1", len(repo.putSettingsInput.OverrideRules.NodeLabels))
	}
	if repo.putSettingsInput.OverrideRules.NodeLabels[0].Label != "核心" {
		t.Fatalf("NodeLabelOverrides[0].Label = %q, want %q", repo.putSettingsInput.OverrideRules.NodeLabels[0].Label, "核心")
	}
	if repo.putSettingsInput.OverrideRules.NodeLabels[0].Overrides.HostSampleFrequencyTier == nil {
		t.Fatal("HostSampleFrequencyTier override = nil, want preserved legacy core override")
	}
	if *repo.putSettingsInput.OverrideRules.NodeLabels[0].Overrides.HostSampleFrequencyTier != "5s" {
		t.Fatalf(
			"HostSampleFrequencyTier override = %q, want %q",
			*repo.putSettingsInput.OverrideRules.NodeLabels[0].Overrides.HostSampleFrequencyTier,
			"5s",
		)
	}
}

func TestSettingsHandlerRejectsUnknownFieldsOnPut(t *testing.T) {
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.Default()}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"bot_token":"bot-token","chat_id":"chat-id","unexpected":true},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":3,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"node_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":7,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestSettingsHandlerMapsValidationFailureToBadRequest(t *testing.T) {
	repo := &fakeSettingsRepository{
		getSettingsResult: centersettings.Default(),
		putSettingsErr:    errors.Join(centersettings.ErrInvalidSettings, errors.New("bad settings")),
	}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"bot_token":"","chat_id":""},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":3,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"node_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":7,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestSettingsHandlerRejectsTelegramTokenWithoutChatID(t *testing.T) {
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.Default()}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"bot_token":"replacement-token","chat_id":""},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":3,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"node_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":7,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
	if repo.putSettingsCalls != 0 {
		t.Fatalf("putSettingsCalls = %d, want 0 because repository should not be called", repo.putSettingsCalls)
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
