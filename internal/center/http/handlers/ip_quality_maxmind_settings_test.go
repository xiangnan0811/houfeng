package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/http/handlers"
	centersettings "houfeng/internal/center/settings"
)

func TestSettingsMaxMindPairMergeClearAndSafeDTO(t *testing.T) {
	for _, test := range []struct {
		name    string
		sources map[string]any
		id, key string
	}{
		{"omitted", map[string]any{}, "12345678", "original-private-key"},
		{"replace account", map[string]any{"maxmind_account_id": "87654321"}, "87654321", "original-private-key"},
		{"replace key", map[string]any{"maxmind_license_key": "replacement-private-key"}, "12345678", "replacement-private-key"},
		{"clear pair", map[string]any{"maxmind_account_id": "", "maxmind_license_key": ""}, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := centersettings.FreshInstallDefaults()
			current.IPQuality.Sources.MaxMindAccountID = "12345678"
			current.IPQuality.Sources.MaxMindLicenseKey = "original-private-key"
			repo := &fakeSettingsRepository{getSettingsResult: current}
			handler := handlers.Settings(repo, nil, centersettings.DeploymentDefaults{})
			response := serveSettingsPut(handler, fullSettingsBody(t, map[string]any{"ip_quality_settings": map[string]any{"sources": test.sources}}))
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d", response.Code)
			}
			got := repo.getSettingsResult.IPQuality.Sources
			if got.MaxMindAccountID != test.id || got.MaxMindLicenseKey != test.key {
				t.Fatal("pair update lost omitted member")
			}
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				body := response.Body.String()
				if method == http.MethodGet {
					recorder := httptest.NewRecorder()
					handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
					body = recorder.Body.String()
				}
				for _, secret := range []string{"12345678", "87654321", "original-private-key", "replacement-private-key", "maxmind_account_id", "maxmind_license_key"} {
					if strings.Contains(body, secret) {
						t.Fatal("public settings returned GeoLite secret")
					}
				}
				var parsed struct {
					IPQuality struct {
						Sources map[string]struct {
							Configured bool   `json:"configured"`
							Masked     string `json:"masked_summary"`
						} `json:"sources"`
					} `json:"ip_quality_settings"`
				}
				if err := json.Unmarshal([]byte(body), &parsed); err != nil {
					t.Fatal(err)
				}
				maxmind, exists := parsed.IPQuality.Sources["maxmind"]
				if !exists || maxmind.Configured != (test.id != "") || (maxmind.Masked != "") != maxmind.Configured {
					t.Fatal("safe descriptor does not reflect pair configuration")
				}
			}
		})
	}
}

func TestSettingsRejectsInvalidMaxMindPairAtomically(t *testing.T) {
	for _, initial := range []bool{false, true} {
		for name, raw := range map[string]string{
			"null account": `{"maxmind_account_id":null}`, "null key": `{"maxmind_license_key":null}`,
			"wrong type": `{"maxmind_account_id":123}`, "single clear account": `{"maxmind_account_id":""}`,
			"single clear key": `{"maxmind_license_key":""}`, "half clear": `{"maxmind_account_id":"","maxmind_license_key":"private-key"}`,
			"invalid id":        `{"maxmind_account_id":"account:bad","maxmind_license_key":"private-key"}`,
			"overlong":          `{"maxmind_account_id":"12345678","maxmind_license_key":"` + strings.Repeat("x", 257) + `"}`,
			"newline":           `{"maxmind_account_id":"12345678","maxmind_license_key":"private\nkey"}`,
			"public DTO echoed": `{"maxmind":{"configured":true,"masked_summary":"****"}}`,
		} {
			t.Run(name, func(t *testing.T) {
				current := centersettings.FreshInstallDefaults()
				if initial {
					current.IPQuality.Sources.MaxMindAccountID = "12345678"
					current.IPQuality.Sources.MaxMindLicenseKey = "keep-private-key"
				}
				repo := &fakeSettingsRepository{getSettingsResult: current}
				response := serveSettingsPut(handlers.Settings(repo, nil, centersettings.DeploymentDefaults{}), fullSettingsBody(t, map[string]any{
					"ip_quality_settings": json.RawMessage(`{"sources":` + raw + `}`),
				}))
				if response.Code != http.StatusBadRequest || repo.getSettingsResult.IPQuality.Sources != current.IPQuality.Sources {
					t.Fatal("invalid credentials changed persisted pair")
				}
				if strings.Contains(response.Body.String(), "keep-private-key") {
					t.Fatal("invalid update leaked secret")
				}
			})
		}
	}
	for _, field := range []string{"maxmind_account_id", "maxmind_license_key"} {
		repo := &fakeSettingsRepository{getSettingsResult: centersettings.FreshInstallDefaults()}
		value := "private-key"
		if field == "maxmind_account_id" {
			value = "12345678"
		}
		response := serveSettingsPut(handlers.Settings(repo, nil, centersettings.DeploymentDefaults{}), fullSettingsBody(t, map[string]any{
			"ip_quality_settings": map[string]any{"sources": map[string]any{field: value}},
		}))
		if response.Code != http.StatusBadRequest {
			t.Fatal("first configuration accepted an incomplete pair")
		}
	}
}
