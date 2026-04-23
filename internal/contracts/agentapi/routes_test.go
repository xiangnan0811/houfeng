package agentapi_test

import (
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func TestAgentRoutesStayStable(t *testing.T) {
	if agentapi.EnrollPath != "/api/agent/enroll" {
		t.Fatalf("EnrollPath = %q, want %q", agentapi.EnrollPath, "/api/agent/enroll")
	}

	if agentapi.SyncPath != "/api/agent/sync" {
		t.Fatalf("SyncPath = %q, want %q", agentapi.SyncPath, "/api/agent/sync")
	}
}
