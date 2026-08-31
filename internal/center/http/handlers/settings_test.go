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
	Feishu                  feishuSettingsResponse                `json:"feishu"`
	HostSampleFrequencyTier string                                `json:"host_sample_frequency_tier"`
	ProbeFrequencyDefaults  centersettings.ProbeFrequencyDefaults `json:"probe_frequency_defaults"`
	IncidentDefaults        centersettings.IncidentDefaults       `json:"incident_defaults"`
	OverrideRules           centersettings.OverrideRules          `json:"override_rules"`
	RetentionPolicy         centersettings.RetentionPolicy        `json:"retention_policy"`
	IPQualitySettings       centersettings.IPQualitySettings      `json:"ip_quality_settings"`
}

type feishuSettingsResponse struct {
	Enabled                 bool   `json:"enabled"`
	WebhookURL              string `json:"webhook_url"`
	WebhookURLPresent       bool   `json:"webhook_url_present"`
	WebhookURLMaskedSummary string `json:"webhook_url_masked_summary"`
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
	record.FeishuEnabled = true
	record.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/full-secret-value"
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
	if body.Feishu.WebhookURL != "" {
		t.Fatalf("expected Feishu webhook URL to be redacted from response, got %q", body.Feishu.WebhookURL)
	}
	if !body.Feishu.WebhookURLPresent {
		t.Fatal("expected feishu.webhook_url_present to be true")
	}
	if body.Feishu.WebhookURLMaskedSummary == "" || strings.Contains(body.Feishu.WebhookURLMaskedSummary, "full-secret-value") {
		t.Fatalf("expected masked Feishu webhook summary, got %q", body.Feishu.WebhookURLMaskedSummary)
	}
	if body.HostSampleFrequencyTier != centersettings.Default().HostSampleFrequencyTier {
		t.Fatalf("expected host sample frequency tier %q, got %q", centersettings.Default().HostSampleFrequencyTier, body.HostSampleFrequencyTier)
	}
	if body.IPQualitySettings.FrequencySeconds != centersettings.Default().IPQuality.FrequencySeconds {
		t.Fatalf("expected ip quality frequency %d, got %d", centersettings.Default().IPQuality.FrequencySeconds, body.IPQualitySettings.FrequencySeconds)
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
	updated.IPQuality.Enabled = false
	updated.IPQuality.FrequencySeconds = 259200
	updated.IPQuality.StaleAfterSeconds = 864000
	updated.IPQuality.TimeoutSeconds = 20
	updated.IPQuality.RawRetentionDays = 45
	updated.IPQuality.HistoryRetentionDays = 180
	updated.IPQuality.Services = []string{"netflix", "chatgpt"}
	repo := &fakeSettingsRepository{getSettingsResult: current, putSettingsResult: updated}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"bot_token":"bot-token","chat_id":"chat-id","runtime_managed":true},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":12,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"monitoring_instance_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":30,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180},"ip_quality_settings":{"enabled":false,"frequency_seconds":259200,"stale_after_seconds":864000,"timeout_seconds":20,"raw_retention_days":45,"history_retention_days":180,"services":["netflix","chatgpt"]}}`))
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
	if repo.putSettingsInput.IPQuality.FrequencySeconds != 259200 {
		t.Fatalf("expected repository input ip quality frequency 259200, got %d", repo.putSettingsInput.IPQuality.FrequencySeconds)
	}
	if repo.putSettingsInput.IPQuality.StaleAfterSeconds != 864000 {
		t.Fatalf("expected repository input ip quality stale after 864000, got %d", repo.putSettingsInput.IPQuality.StaleAfterSeconds)
	}
	if len(repo.putSettingsInput.IPQuality.Services) != 2 || repo.putSettingsInput.IPQuality.Services[1] != "chatgpt" {
		t.Fatalf("expected repository input ip quality services to preserve request, got %#v", repo.putSettingsInput.IPQuality.Services)
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
	if body.IPQualitySettings.Enabled {
		t.Fatal("expected ip_quality_settings.enabled false in response")
	}
	if body.IPQualitySettings.TimeoutSeconds != 20 {
		t.Fatalf("expected ip_quality_settings.timeout_seconds 20, got %d", body.IPQualitySettings.TimeoutSeconds)
	}
	if body.IPQualitySettings.StaleAfterSeconds != 864000 {
		t.Fatalf("expected ip_quality_settings.stale_after_seconds 864000, got %d", body.IPQualitySettings.StaleAfterSeconds)
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
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"chat_id":"chat-id"},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":12,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"monitoring_instance_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":30,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
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
	current.OverrideRules.MonitoringInstanceLabels = []centersettings.MonitoringInstanceLabelOverrideRule{{
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
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"chat_id":"","runtime_managed":false},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":12,"sweep_interval_seconds":90,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"monitoring_instance_labels":[{"label":"核心","overrides":{"host_sample_frequency_tier":"5s"}}],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":30,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if repo.putSettingsInput.IncidentDefaults.SweepIntervalSeconds != 90 {
		t.Fatalf("SweepIntervalSeconds = %d, want %d", repo.putSettingsInput.IncidentDefaults.SweepIntervalSeconds, 90)
	}
	if repo.putSettingsInput.IncidentDefaults.StaleThresholdIntervals != 12 {
		t.Fatalf("StaleThresholdIntervals = %d, want fresh-install default 12", repo.putSettingsInput.IncidentDefaults.StaleThresholdIntervals)
	}
	if len(repo.putSettingsInput.OverrideRules.MonitoringInstanceLabels) != 1 {
		t.Fatalf("len(MonitoringInstanceLabelOverrides) = %d, want 1", len(repo.putSettingsInput.OverrideRules.MonitoringInstanceLabels))
	}
	if repo.putSettingsInput.OverrideRules.MonitoringInstanceLabels[0].Label != "核心" {
		t.Fatalf("MonitoringInstanceLabelOverrides[0].Label = %q, want %q", repo.putSettingsInput.OverrideRules.MonitoringInstanceLabels[0].Label, "核心")
	}
	if repo.putSettingsInput.OverrideRules.MonitoringInstanceLabels[0].Overrides.HostSampleFrequencyTier == nil {
		t.Fatal("HostSampleFrequencyTier override = nil, want preserved legacy core override")
	}
	if *repo.putSettingsInput.OverrideRules.MonitoringInstanceLabels[0].Overrides.HostSampleFrequencyTier != "5s" {
		t.Fatalf(
			"HostSampleFrequencyTier override = %q, want %q",
			*repo.putSettingsInput.OverrideRules.MonitoringInstanceLabels[0].Overrides.HostSampleFrequencyTier,
			"5s",
		)
	}
}

func TestSettingsHandlerRejectsUnknownFieldsOnPut(t *testing.T) {
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.Default()}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"bot_token":"bot-token","chat_id":"chat-id","unexpected":true},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":12,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"monitoring_instance_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":30,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestSettingsHandlerRejectsOversizedPutBody(t *testing.T) {
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.Default()}

	handler := handlers.Settings(repo)
	body := `{"telegram":{"chat_id":"` + strings.Repeat("x", handlers.DefaultJSONBodyLimit) + `"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
	if repo.putSettingsCalls != 0 {
		t.Fatalf("putSettingsCalls = %d, want 0 because oversized body should not be persisted", repo.putSettingsCalls)
	}
}

func TestSettingsHandlerMapsValidationFailureToBadRequest(t *testing.T) {
	repo := &fakeSettingsRepository{
		getSettingsResult: centersettings.Default(),
		putSettingsErr:    errors.Join(centersettings.ErrInvalidSettings, errors.New("bad settings")),
	}

	handler := handlers.Settings(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"bot_token":"","chat_id":""},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":12,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"monitoring_instance_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":30,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
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
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"telegram":{"bot_token":"replacement-token","chat_id":""},"host_sample_frequency_tier":"5s","probe_frequency_defaults":{"tcp":"5s","http":"5s","tls":"6h"},"incident_defaults":{"heartbeat_interval_seconds":5,"stale_threshold_intervals":12,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true},"override_rules":{"monitoring_instance_labels":[],"target_types":[],"target_labels":[]},"retention_policy":{"raw_layer_days":30,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}}`))
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
