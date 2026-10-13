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
	mutateSettingsErr error
	mutateCalls       int
	mutateInput       centersettings.CenterSettings
	deployment        centersettings.DeploymentDefaults
}

func (f *fakeSettingsRepository) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	if f.getSettingsErr != nil {
		return centersettings.CenterSettings{}, f.getSettingsErr
	}
	return centersettings.ResolveEffectiveSettings(f.getSettingsResult, f.deployment), nil
}

func (f *fakeSettingsRepository) MutateSettings(_ context.Context, mutate centersettings.MutateSettingsFunc) (centersettings.CenterSettings, error) {
	f.mutateCalls++
	next, err := mutate(f.getSettingsResult)
	if err != nil {
		return centersettings.CenterSettings{}, err
	}
	f.mutateInput = next
	if f.mutateSettingsErr != nil {
		return centersettings.CenterSettings{}, f.mutateSettingsErr
	}
	next.Effective.Persisted = true
	f.getSettingsResult = next
	return centersettings.ResolveEffectiveSettings(next, f.deployment), nil
}

type settingsHandlerResponse struct {
	Telegram         telegramSettingsResponse        `json:"telegram"`
	IncidentDefaults centersettings.IncidentDefaults `json:"incident_defaults"`
	IncidentSweep    incidentSweepResponse           `json:"incident_sweep"`
	SettingsSource   string                          `json:"settings_source"`
}

type telegramSettingsResponse struct {
	Mode               string                     `json:"mode"`
	Source             string                     `json:"source"`
	Active             bool                       `json:"active"`
	ChatID             string                     `json:"chat_id"`
	TokenPresent       bool                       `json:"token_present"`
	TokenMaskedSummary string                     `json:"token_masked_summary"`
	Deployment         telegramDeploymentResponse `json:"deployment"`
}

type telegramDeploymentResponse struct {
	Configured         bool   `json:"configured"`
	ChatID             string `json:"chat_id"`
	TokenPresent       bool   `json:"token_present"`
	TokenMaskedSummary string `json:"token_masked_summary"`
}

type incidentSweepResponse struct {
	Mode              string `json:"mode"`
	Source            string `json:"source"`
	DeploymentSeconds int    `json:"deployment_seconds"`
}

type refreshRecorder struct{ calls int }

func (r *refreshRecorder) RequestRefresh(bool) { r.calls++ }

func testDeployment() centersettings.DeploymentDefaults {
	return centersettings.DeploymentDefaults{
		Telegram: centersettings.TelegramSettings{
			BotToken: "123456:deployment-secret-token",
			ChatID:   "deployment-chat",
		},
		IncidentSweepIntervalSeconds: 60,
		IncidentSweepSource:          centersettings.ConfigSourceDeployment,
	}
}

func serveSettingsPut(handler http.Handler, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	return recorder
}

func fullSettingsBody(t *testing.T, extras map[string]any) string {
	t.Helper()
	defaults := centersettings.FreshInstallDefaults()
	body := map[string]any{
		"feishu": map[string]any{
			"enabled":     defaults.FeishuEnabled,
			"webhook_url": defaults.FeishuWebhookURL,
		},
		"host_sample_frequency_tier": defaults.HostSampleFrequencyTier,
		"probe_frequency_defaults":   defaults.ProbeFrequencyDefaults,
		"incident_defaults":          defaults.IncidentDefaults,
		"override_rules":             defaults.OverrideRules,
		"retention_policy":           defaults.RetentionPolicy,
	}
	for key, value := range extras {
		body[key] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal full settings body: %v", err)
	}
	return string(encoded)
}

func decodeSettingsResponse(t *testing.T, recorder *httptest.ResponseRecorder) settingsHandlerResponse {
	t.Helper()
	var response settingsHandlerResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode settings response: %v; body=%s", err, recorder.Body.String())
	}
	return response
}

func TestSettingsHandlerReturnsEffectiveDeploymentSnapshotWithoutSecrets(t *testing.T) {
	deployment := testDeployment()
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.FreshInstallDefaults(), deployment: deployment}
	handler := handlers.Settings(repo, nil, deployment)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/settings", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", got)
	}
	response := decodeSettingsResponse(t, recorder)
	if response.Telegram.Mode != "inherit" || response.Telegram.Source != string(centersettings.ConfigSourceDeployment) {
		t.Fatalf("telegram mode/source = %q/%q, want inherit/deployment", response.Telegram.Mode, response.Telegram.Source)
	}
	if !response.Telegram.Active || response.Telegram.ChatID != deployment.Telegram.ChatID || !response.Telegram.TokenPresent {
		t.Fatalf("telegram effective response = %#v, want active deployment channel", response.Telegram)
	}
	if response.Telegram.Deployment.ChatID != deployment.Telegram.ChatID || !response.Telegram.Deployment.Configured || !response.Telegram.Deployment.TokenPresent {
		t.Fatalf("telegram deployment summary = %#v, want configured safe summary", response.Telegram.Deployment)
	}
	if strings.Contains(recorder.Body.String(), deployment.Telegram.BotToken) {
		t.Fatalf("response leaked deployment token: %s", recorder.Body.String())
	}
	if response.IncidentSweep.Mode != "inherit" || response.IncidentSweep.Source != string(centersettings.ConfigSourceDeployment) || response.IncidentSweep.DeploymentSeconds != 60 {
		t.Fatalf("incident sweep response = %#v, want deployment inheritance", response.IncidentSweep)
	}
	if response.SettingsSource != string(centersettings.ConfigSourceDefault) {
		t.Fatalf("settings_source = %q, want default before persistence", response.SettingsSource)
	}
	for _, removed := range []string{"runtime_managed", "runtime_apply_active", "bot_token"} {
		if strings.Contains(recorder.Body.String(), `"`+removed+`"`) {
			t.Fatalf("response contains removed/raw field %q: %s", removed, recorder.Body.String())
		}
	}
}

func TestSettingsHandlerTelegramOverrideUsesCurrentEffectiveValuesAndModes(t *testing.T) {
	deployment := testDeployment()
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.FreshInstallDefaults(), deployment: deployment}
	refresh := &refreshRecorder{}
	handler := handlers.Settings(repo, refresh, deployment)

	recorder := serveSettingsPut(handler, fullSettingsBody(t, map[string]any{
		"telegram": map[string]any{"mode": "override", "chat_id": "custom-chat"},
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("chat-only override status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("PUT Cache-Control = %q, want private, no-store", recorder.Header().Get("Cache-Control"))
	}
	if repo.mutateInput.Telegram.BotToken != deployment.Telegram.BotToken || repo.mutateInput.Telegram.ChatID != "custom-chat" || !repo.mutateInput.Telegram.RuntimeManaged {
		t.Fatalf("chat-only override persisted = %#v, want deployment token + custom chat managed", repo.mutateInput.Telegram)
	}
	response := decodeSettingsResponse(t, recorder)
	if response.Telegram.Mode != "override" || response.Telegram.Source != string(centersettings.ConfigSourceDatabase) || !response.Telegram.Active || response.Telegram.ChatID != "custom-chat" {
		t.Fatalf("override response telegram = %#v", response.Telegram)
	}
	if strings.Contains(recorder.Body.String(), deployment.Telegram.BotToken) {
		t.Fatalf("override response leaked raw token: %s", recorder.Body.String())
	}

	recorder = serveSettingsPut(handler, fullSettingsBody(t, map[string]any{
		"telegram": map[string]any{"mode": "disabled"},
	}))
	if recorder.Code != http.StatusOK || repo.mutateInput.Telegram.BotToken != "" || repo.mutateInput.Telegram.ChatID != "" || !repo.mutateInput.Telegram.RuntimeManaged {
		t.Fatalf("disabled mutation status/input = %d/%#v", recorder.Code, repo.mutateInput.Telegram)
	}
	response = decodeSettingsResponse(t, recorder)
	if response.Telegram.Mode != "disabled" || response.Telegram.Active || response.Telegram.Source != string(centersettings.ConfigSourceDatabase) {
		t.Fatalf("disabled response telegram = %#v", response.Telegram)
	}

	recorder = serveSettingsPut(handler, fullSettingsBody(t, map[string]any{
		"telegram": map[string]any{"mode": "inherit"},
	}))
	if recorder.Code != http.StatusOK || repo.mutateInput.Telegram.RuntimeManaged || repo.mutateInput.Telegram.BotToken != "" || repo.mutateInput.Telegram.ChatID != "" {
		t.Fatalf("inherit mutation status/input = %d/%#v", recorder.Code, repo.mutateInput.Telegram)
	}
	response = decodeSettingsResponse(t, recorder)
	if response.Telegram.Mode != "inherit" || response.Telegram.Source != string(centersettings.ConfigSourceDeployment) || !response.Telegram.Active || response.Telegram.ChatID != deployment.Telegram.ChatID {
		t.Fatalf("inherit response telegram = %#v", response.Telegram)
	}
	if refresh.calls != 3 {
		t.Fatalf("refresh calls = %d, want 3 successful mutations", refresh.calls)
	}
}

func TestSettingsHandlerRejectsInvalidTelegramModesAndCredentialsAtomically(t *testing.T) {
	deployment := testDeployment()
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.FreshInstallDefaults(), deployment: deployment}
	handler := handlers.Settings(repo, nil, deployment)
	cases := []string{
		fullSettingsBody(t, map[string]any{"telegram": map[string]any{"mode": "inherit", "chat_id": "not-allowed"}}),
		fullSettingsBody(t, map[string]any{"telegram": map[string]any{"mode": "inherit", "bot_token": nil}}),
		fullSettingsBody(t, map[string]any{"telegram": map[string]any{"mode": "disabled", "bot_token": "not-allowed"}}),
		fullSettingsBody(t, map[string]any{"telegram": map[string]any{"mode": "disabled", "chat_id": nil}}),
		fullSettingsBody(t, map[string]any{"telegram": map[string]any{"mode": "override", "bot_token": ""}}),
		fullSettingsBody(t, map[string]any{"telegram": map[string]any{"mode": "override", "chat_id": ""}}),
		fullSettingsBody(t, map[string]any{"telegram": map[string]any{"mode": ""}}),
		fullSettingsBody(t, map[string]any{"telegram": map[string]any{"mode": "unknown"}}),
		fullSettingsBody(t, map[string]any{"telegram": map[string]any{"mode": "override", "bot_token": "********oken"}}),
		fullSettingsBody(t, map[string]any{"telegram": map[string]any{"mode": "override", "bot_token": nil}}),
		fullSettingsBody(t, map[string]any{"telegram": nil}),
		`null`,
		fullSettingsBody(t, map[string]any{"incident_sweep_mode": ""}),
		fullSettingsBody(t, map[string]any{"telegram": map[string]any{"mode": "override", "unexpected": true}}),
		fullSettingsBody(t, map[string]any{"runtime_managed": true}),
	}
	for _, body := range cases {
		recorder := serveSettingsPut(handler, body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want 400", body, recorder.Code)
		}
	}
	if repo.mutateCalls != 0 {
		t.Fatalf("mutate calls = %d, want zero for rejected requests", repo.mutateCalls)
	}
}

func TestSettingsHandlerRejectsOversizedPutBody(t *testing.T) {
	deployment := testDeployment()
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.FreshInstallDefaults(), deployment: deployment}
	handler := handlers.Settings(repo, nil, deployment)
	body := fullSettingsBody(t, map[string]any{
		"telegram": map[string]any{"mode": "override", "chat_id": strings.Repeat("x", handlers.DefaultJSONBodyLimit)},
	})
	recorder := serveSettingsPut(handler, body)
	if recorder.Code != http.StatusBadRequest || repo.mutateCalls != 0 {
		t.Fatalf("oversized PUT status/calls = %d/%d, want 400/0", recorder.Code, repo.mutateCalls)
	}
}

func TestSettingsHandlerSweepModesPreserveRawOwnership(t *testing.T) {
	deployment := testDeployment()
	current := centersettings.FreshInstallDefaults()
	current.IncidentSweepRuntimeManaged = true
	current.IncidentDefaults.SweepIntervalSeconds = 17
	repo := &fakeSettingsRepository{getSettingsResult: current, deployment: deployment}
	handler := handlers.Settings(repo, nil, deployment)

	requested := centersettings.FreshInstallDefaults().IncidentDefaults
	requested.SweepIntervalSeconds = 23
	recorder := serveSettingsPut(handler, fullSettingsBody(t, map[string]any{
		"incident_defaults":   requested,
		"incident_sweep_mode": "override",
	}))
	if recorder.Code != http.StatusOK || !repo.mutateInput.IncidentSweepRuntimeManaged || repo.mutateInput.IncidentDefaults.SweepIntervalSeconds != 23 {
		t.Fatalf("override sweep status/input = %d/%t/%d", recorder.Code, repo.mutateInput.IncidentSweepRuntimeManaged, repo.mutateInput.IncidentDefaults.SweepIntervalSeconds)
	}

	requested.SweepIntervalSeconds = 90
	recorder = serveSettingsPut(handler, fullSettingsBody(t, map[string]any{
		"incident_defaults": requested,
	}))
	if recorder.Code != http.StatusOK || !repo.mutateInput.IncidentSweepRuntimeManaged || repo.mutateInput.IncidentDefaults.SweepIntervalSeconds != 23 {
		t.Fatalf("omitted sweep mode changed ownership/value: status=%d managed=%t sweep=%d", recorder.Code, repo.mutateInput.IncidentSweepRuntimeManaged, repo.mutateInput.IncidentDefaults.SweepIntervalSeconds)
	}

	recorder = serveSettingsPut(handler, fullSettingsBody(t, map[string]any{
		"incident_sweep_mode": "inherit",
	}))
	if recorder.Code != http.StatusOK || repo.mutateInput.IncidentSweepRuntimeManaged || repo.mutateInput.IncidentDefaults.SweepIntervalSeconds != centersettings.Default().IncidentDefaults.SweepIntervalSeconds {
		t.Fatalf("inherit sweep status/input = %d/%t/%d", recorder.Code, repo.mutateInput.IncidentSweepRuntimeManaged, repo.mutateInput.IncidentDefaults.SweepIntervalSeconds)
	}
	response := decodeSettingsResponse(t, recorder)
	if response.IncidentSweep.Mode != "inherit" ||
		response.IncidentSweep.Source != string(centersettings.ConfigSourceDeployment) ||
		response.IncidentSweep.DeploymentSeconds != 60 ||
		response.IncidentDefaults.SweepIntervalSeconds != 60 {
		t.Fatalf("inherit sweep response = %#v defaults=%#v", response.IncidentSweep, response.IncidentDefaults)
	}
}

func TestSettingsHandlerRejectsInvalidSweepModesAndKeepsAtomicity(t *testing.T) {
	deployment := testDeployment()
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.FreshInstallDefaults(), deployment: deployment}
	handler := handlers.Settings(repo, nil, deployment)
	invalidDefaults := centersettings.FreshInstallDefaults().IncidentDefaults
	invalidDefaults.SweepIntervalSeconds = 0
	for _, body := range []string{
		fullSettingsBody(t, map[string]any{"incident_sweep_mode": "override", "incident_defaults": invalidDefaults}),
		fullSettingsBody(t, map[string]any{"incident_sweep_mode": "inherit", "incident_defaults": invalidDefaults}),
		fullSettingsBody(t, map[string]any{"incident_sweep_mode": "unknown"}),
		fullSettingsBody(t, map[string]any{"incident_sweep_mode": nil}),
	} {
		recorder := serveSettingsPut(handler, body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want 400", body, recorder.Code)
		}
	}
	if repo.mutateCalls != 0 {
		t.Fatalf("mutate calls = %d, want zero for invalid sweep requests", repo.mutateCalls)
	}
}

func TestSettingsHandlerPreservesCurrentSettingsOnUnrelatedSave(t *testing.T) {
	deployment := testDeployment()
	current := centersettings.FreshInstallDefaults()
	current.Telegram = centersettings.TelegramSettings{BotToken: "stored-token", ChatID: "stored-chat", RuntimeManaged: true}
	current.IncidentSweepRuntimeManaged = true
	current.IncidentDefaults.SweepIntervalSeconds = 17
	current.SubscriptionCost.FixerAPIKey = "existing-secret"
	repo := &fakeSettingsRepository{getSettingsResult: current, deployment: deployment}
	handler := handlers.Settings(repo, nil, deployment)

	recorder := serveSettingsPut(handler, fullSettingsBody(t, map[string]any{
		"subscription_cost_settings": map[string]any{"base_currency": "USD"},
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unrelated save status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if repo.mutateInput.Telegram != current.Telegram || repo.mutateInput.IncidentSweepRuntimeManaged != current.IncidentSweepRuntimeManaged || repo.mutateInput.IncidentDefaults.SweepIntervalSeconds != 17 || repo.mutateInput.SubscriptionCost.FixerAPIKey != "existing-secret" {
		t.Fatalf("unrelated save lost raw settings: %#v", repo.mutateInput)
	}
}

func TestSettingsHandlerMapsRepositoryAndValidationFailures(t *testing.T) {
	deployment := testDeployment()
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.FreshInstallDefaults(), mutateSettingsErr: errors.New("database unavailable"), deployment: deployment}
	refresh := &refreshRecorder{}
	handler := handlers.Settings(repo, refresh, deployment)

	recorder := serveSettingsPut(handler, fullSettingsBody(t, map[string]any{
		"subscription_cost_settings": map[string]any{"base_currency": "USD"},
	}))
	if recorder.Code != http.StatusInternalServerError || refresh.calls != 0 {
		t.Fatalf("repository failure status/refresh = %d/%d, want 500/0", recorder.Code, refresh.calls)
	}

	repo.getSettingsErr = errors.New("database unavailable")
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if getRecorder.Code != http.StatusInternalServerError || getRecorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("GET repository failure status/cache = %d/%q", getRecorder.Code, getRecorder.Header().Get("Cache-Control"))
	}
}

func TestSettingsHandlerRejectsRemovedRetentionControlsAndMethods(t *testing.T) {
	deployment := testDeployment()
	repo := &fakeSettingsRepository{getSettingsResult: centersettings.FreshInstallDefaults(), deployment: deployment}
	handler := handlers.Settings(repo, nil, deployment)
	for _, body := range []string{
		fullSettingsBody(t, map[string]any{"retention_policy": map[string]any{"raw_layer_days": 30, "aggregate_layer_days": 365, "event_layer_days": 1}}),
		fullSettingsBody(t, map[string]any{"retention_policy": map[string]any{"raw_layer_days": 30, "aggregate_layer_days": 365, "notification_layer_days": 1}}),
		fullSettingsBody(t, map[string]any{"ip_quality_settings": map[string]any{"raw_retention_days": 1}}),
		fullSettingsBody(t, map[string]any{"ip_quality_settings": map[string]any{"history_retention_days": 1}}),
	} {
		recorder := serveSettingsPut(handler, body)
		if recorder.Code != http.StatusBadRequest || repo.mutateCalls != 0 {
			t.Fatalf("removed field body %s status/calls = %d/%d", body, recorder.Code, repo.mutateCalls)
		}
	}
	methodRecorder := httptest.NewRecorder()
	handler.ServeHTTP(methodRecorder, httptest.NewRequest(http.MethodPost, "/api/settings", nil))
	if methodRecorder.Code != http.StatusMethodNotAllowed || methodRecorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("POST status/cache = %d/%q", methodRecorder.Code, methodRecorder.Header().Get("Cache-Control"))
	}
}
