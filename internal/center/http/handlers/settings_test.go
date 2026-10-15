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
	next, err = centersettings.Validate(next)
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

func TestSettingsHandlerIPQualityResponsesNeverExposeCredentials(t *testing.T) {
	current := centersettings.FreshInstallDefaults()
	current.IPQuality.Sources = centersettings.IPQualitySources{
		IPAPIISKey:   "ipapi-private-value-1234",
		AbuseIPDBKey: "abuse-private-value-5678",
	}
	repo := &fakeSettingsRepository{getSettingsResult: current}
	handler := handlers.Settings(repo, nil, centersettings.DeploymentDefaults{})

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	put := serveSettingsPut(handler, fullSettingsBody(t, map[string]any{
		"ip_quality_settings": map[string]any{
			"sources": map[string]any{"ipapi_is_key": "replacement-private-value-9012"},
		},
	}))
	for name, recorder := range map[string]*httptest.ResponseRecorder{"get": get, "put": put} {
		t.Run(name, func(t *testing.T) {
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			body := recorder.Body.String()
			for _, forbidden := range []string{
				`"ipapi_is_key"`, `"abuseipdb_key"`,
				current.IPQuality.Sources.IPAPIISKey, current.IPQuality.Sources.AbuseIPDBKey,
				"replacement-private-value-9012",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatal("settings response exposed a credential or internal credential field")
				}
			}
			var response struct {
				IPQuality struct {
					MailEnabled bool `json:"mail_enabled"`
					Sources     map[string]struct {
						Configured    bool   `json:"configured"`
						MaskedSummary string `json:"masked_summary"`
					} `json:"sources"`
				} `json:"ip_quality_settings"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			for _, source := range []string{"ipapi_is", "abuseipdb"} {
				summary := response.IPQuality.Sources[source]
				if !summary.Configured || !strings.Contains(summary.MaskedSummary, "*") {
					t.Fatalf("%s must be configured with a masked summary", source)
				}
			}
		})
	}
}

func TestSettingsHandlerIPQualityUpdatesMergeLatestRawSettings(t *testing.T) {
	for name, update := range map[string]map[string]any{
		"block omitted": nil,
		"old client collection block": {
			"enabled": false, "frequency_seconds": 3600, "stale_after_seconds": 7200,
			"timeout_seconds": 15, "services": []string{"chatgpt"},
		},
		"empty sources": {"sources": map[string]any{}},
		"mail disabled": {"mail_enabled": false},
		"key replaced":  {"sources": map[string]any{"ipapi_is_key": "new-private-key"}},
		"key cleared":   {"sources": map[string]any{"abuseipdb_key": ""}},
	} {
		t.Run(name, func(t *testing.T) {
			current := centersettings.FreshInstallDefaults()
			current.IPQuality.TimeoutSeconds = 15
			current.IPQuality.Sources = centersettings.IPQualitySources{
				IPAPIISKey: "latest-ipapi-secret", AbuseIPDBKey: "latest-abuse-secret",
			}
			repo := &fakeSettingsRepository{
				getSettingsResult: current,
				getSettingsErr:    errors.New("updates must use lock-held raw settings, not a separate read"),
			}
			handler := handlers.Settings(repo, nil, centersettings.DeploymentDefaults{})
			extras := map[string]any{}
			if update != nil {
				extras["ip_quality_settings"] = update
			}
			recorder := serveSettingsPut(handler, fullSettingsBody(t, extras))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			got := repo.getSettingsResult.IPQuality
			wantIPAPI, wantAbuse := "latest-ipapi-secret", "latest-abuse-secret"
			if name == "key replaced" {
				wantIPAPI = "new-private-key"
			}
			if name == "key cleared" {
				wantAbuse = ""
			}
			if got.Sources.IPAPIISKey != wantIPAPI || got.Sources.AbuseIPDBKey != wantAbuse {
				t.Fatal("update lost or overwrote an omitted credential")
			}
			if got.MailEnabled != (name != "mail disabled") || got.TimeoutSeconds != 15 {
				t.Fatal("update lost mail state or changed an explicit persisted timeout")
			}
			if name == "old client collection block" && (got.Enabled || got.FrequencySeconds != 3600 || len(got.Services) != 1 || got.Services[0] != "chatgpt") {
				t.Fatal("old client's collection changes were not applied")
			}
			if name == "key cleared" {
				var response map[string]json.RawMessage
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				var ipQuality struct {
					Sources map[string]struct {
						Configured    bool   `json:"configured"`
						MaskedSummary string `json:"masked_summary"`
					} `json:"sources"`
				}
				if err := json.Unmarshal(response["ip_quality_settings"], &ipQuality); err != nil {
					t.Fatal(err)
				}
				if ipQuality.Sources["abuseipdb"].Configured || ipQuality.Sources["abuseipdb"].MaskedSummary != "" {
					t.Fatal("cleared credential must have an unconfigured public response")
				}
			}
		})
	}
}

func TestSettingsHandlerRejectsInvalidIPQualityUpdatesAtomically(t *testing.T) {
	for name, raw := range map[string]string{
		"null block":      `null`,
		"null sources":    `{"sources":null}`,
		"null ipapi key":  `{"sources":{"ipapi_is_key":null}}`,
		"null abuse key":  `{"sources":{"abuseipdb_key":null}}`,
		"nonstring key":   `{"sources":{"ipapi_is_key":123}}`,
		"null mail":       `{"mail_enabled":null}`,
		"unknown source":  `{"sources":{"other_key":"secret"}}`,
		"response echoed": `{"sources":{"ipapi_is":{"configured":true,"masked_summary":"****"}}}`,
		"overlong ipapi":  `{"sources":{"ipapi_is_key":"` + strings.Repeat("x", 257) + `"}}`,
		"overlong abuse":  `{"sources":{"abuseipdb_key":"` + strings.Repeat("x", 257) + `"}}`,
		"empty services":  `{"services":[]}`,
		"invalid timeout": `{"timeout_seconds":0}`,
	} {
		t.Run(name, func(t *testing.T) {
			current := centersettings.FreshInstallDefaults()
			current.IPQuality.Sources.IPAPIISKey = "keep-private-secret"
			repo := &fakeSettingsRepository{getSettingsResult: current}
			refresh := &refreshRecorder{}
			handler := handlers.Settings(repo, refresh, centersettings.DeploymentDefaults{})
			recorder := serveSettingsPut(handler, fullSettingsBody(t, map[string]any{
				"ip_quality_settings": json.RawMessage(raw),
			}))
			if recorder.Code != http.StatusBadRequest || refresh.calls != 0 {
				t.Fatalf("status/refresh = %d/%d, want 400/0", recorder.Code, refresh.calls)
			}
			if repo.getSettingsResult.IPQuality.Sources.IPAPIISKey != "keep-private-secret" {
				t.Fatal("rejected update changed persisted credentials")
			}
			if strings.Contains(recorder.Body.String(), "keep-private-secret") || strings.Contains(recorder.Body.String(), strings.Repeat("x", 257)) {
				t.Fatal("error response exposed a credential")
			}
		})
	}
}

func TestSettingsHandlerIPQualityShortCredentialsRemainFullyMasked(t *testing.T) {
	current := centersettings.FreshInstallDefaults()
	current.IPQuality.MailEnabled = false
	repo := &fakeSettingsRepository{getSettingsResult: current}
	handler := handlers.Settings(repo, nil, centersettings.DeploymentDefaults{})
	recorder := serveSettingsPut(handler, fullSettingsBody(t, map[string]any{
		"ip_quality_settings": map[string]any{
			"sources": map[string]any{"ipapi_is_key": "abc", "abuseipdb_key": strings.Repeat("z", 256)},
		},
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), `"abc"`) || strings.Contains(recorder.Body.String(), strings.Repeat("z", 256)) {
		t.Fatal("response exposed short or maximum-length credentials")
	}
	if repo.getSettingsResult.IPQuality.Sources.IPAPIISKey != "abc" || len(repo.getSettingsResult.IPQuality.Sources.AbuseIPDBKey) != 256 || repo.getSettingsResult.IPQuality.MailEnabled {
		t.Fatal("credential update changed omitted disabled mail or truncated a valid key")
	}
}
