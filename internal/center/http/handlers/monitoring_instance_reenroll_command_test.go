package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/monitoringinstances"
)

func TestInstallCommandAlwaysReplacesHostCredentialsForExplicitEnrollment(t *testing.T) {
	for _, instanceID := range []string{"mi_reenroll", "mi_new_after_retired_predecessor"} {
		repo := &fakeMonitoringInstanceOnboardingRepository{issueEnrollmentTokenResult: monitoringinstances.EnrollmentTokenIssue{Token: "enroll_fixture"}}
		handler := handlers.MonitoringInstanceInstallCommand(repo, handlers.InstallCommandOptions{PublicBaseURL: "https://center.example.com", AgentVersion: "v1.2.3"})
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/"+instanceID+"/install-command", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d", recorder.Code)
		}
		var response monitoringinstances.InstallCommandIssue
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(response.Command, " --reenroll") {
			t.Fatal("explicit enrollment command would preserve another instance's stale host credentials")
		}
	}
}
