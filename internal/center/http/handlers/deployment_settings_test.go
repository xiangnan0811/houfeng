package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"houfeng/internal/center/accessadmin"
	centerconfig "houfeng/internal/center/config"
	"houfeng/internal/center/http/sessionctx"
)

func TestDeploymentSettingsEnforcesMethodAndSupervisorBoundaries(t *testing.T) {
	snapshot := centerconfig.DeploymentSettingsSnapshot{Items: []centerconfig.DeploymentSettingItem{{
		Key: "HOUFENG_HTTP_ADDR", Group: centerconfig.DeploymentSettingGroupProcess,
		Source: centerconfig.DeploymentSettingSourceDefault, Value: new(":8080"),
		Application: centerconfig.DeploymentSettingApplicationStartup,
	}}}

	tests := []struct {
		name       string
		method     string
		actorID    string
		supervisor error
		wantStatus int
	}{
		{name: "non get", method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed},
		{name: "unauthenticated", method: http.MethodGet, wantStatus: http.StatusUnauthorized},
		{name: "ordinary admin", method: http.MethodGet, actorID: "usr_operator", supervisor: accessadmin.ErrManagementForbidden, wantStatus: http.StatusForbidden},
		{name: "authorization failure", method: http.MethodGet, actorID: "usr_operator", supervisor: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &accessAdminHandlerRepository{supervisorErr: tt.supervisor}
			service := accessadmin.New(repo, accessadmin.Options{PasswordBcryptCost: bcrypt.MinCost})
			req := httptest.NewRequest(tt.method, "/api/settings/deployment", nil)
			if tt.actorID != "" {
				req = req.WithContext(sessionctx.WithUserID(req.Context(), tt.actorID))
			}
			response := httptest.NewRecorder()

			DeploymentSettings(service, snapshot).ServeHTTP(response, req)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Fatalf("Cache-Control = %q, want private, no-store", got)
			}
		})
	}
}

func TestDeploymentSettingsReturnsImmutableSnapshot(t *testing.T) {
	value := "before"
	snapshot := centerconfig.DeploymentSettingsSnapshot{Items: []centerconfig.DeploymentSettingItem{{
		Key: "HOUFENG_PUBLIC_BASE_URL", Group: centerconfig.DeploymentSettingGroupProcess,
		Source: centerconfig.DeploymentSettingSourceEnvironment, Value: &value,
		Configured: true, Application: centerconfig.DeploymentSettingApplicationStartup,
	}}}
	repo := &accessAdminHandlerRepository{}
	service := accessadmin.New(repo, accessadmin.Options{})
	handler := DeploymentSettings(service, snapshot)
	value = "mutated before request"
	snapshot.Items[0].Value = new("mutated snapshot")

	req := httptest.NewRequest(http.MethodGet, "/api/settings/deployment", nil)
	req = req.WithContext(sessionctx.WithUserID(req.Context(), "usr_supervisor"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"value":"before"`) {
		t.Fatalf("response = %s, want the original immutable snapshot", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "mutated") {
		t.Fatalf("response = %s, contains post-construction mutation", response.Body.String())
	}
}

func TestDeploymentSettingsDoesNotReturnSensitiveValues(t *testing.T) {
	const sentinel = "secret-sentinel-must-not-escape"
	snapshot := centerconfig.DeploymentSettingsSnapshot{Items: []centerconfig.DeploymentSettingItem{{
		Key: "HOUFENG_SESSION_HMAC_KEY", Group: centerconfig.DeploymentSettingGroupAuthentication,
		Source: centerconfig.DeploymentSettingSourceFile, Value: new(sentinel), Configured: true, Sensitive: true,
		Application: centerconfig.DeploymentSettingApplicationStartup,
	}, {
		Key: "HOUFENG_DATABASE_URL", Group: centerconfig.DeploymentSettingGroupDatabase,
		Source: centerconfig.DeploymentSettingSourceEnvironment, Value: new(sentinel), Configured: true, Sensitive: true,
		Application: centerconfig.DeploymentSettingApplicationStartup,
	}}}
	service := accessadmin.New(&accessAdminHandlerRepository{}, accessadmin.Options{})
	req := httptest.NewRequest(http.MethodGet, "/api/settings/deployment", nil)
	req = req.WithContext(sessionctx.WithUserID(req.Context(), "usr_supervisor"))
	response := httptest.NewRecorder()

	DeploymentSettings(service, snapshot).ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if strings.Contains(response.Body.String(), sentinel) {
		t.Fatalf("response contains sensitive sentinel: %s", response.Body.String())
	}
}
