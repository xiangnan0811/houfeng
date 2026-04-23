package store

import (
	"os"
	"strings"
	"testing"

	"houfeng/internal/center/nodes"
)

func TestResolveEnrollmentBindingTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		currentStatus      string
		currentFingerprint string
		newFingerprint     string
		wantStatus         string
		wantFingerprint    string
	}{
		{
			name:            "binds unbound node",
			currentStatus:   nodes.BindingUnbound,
			newFingerprint:  "fp-new",
			wantStatus:      nodes.BindingBound,
			wantFingerprint: "fp-new",
		},
		{
			name:               "keeps bound node with same fingerprint",
			currentStatus:      nodes.BindingBound,
			currentFingerprint: "fp-same",
			newFingerprint:     "fp-same",
			wantStatus:         nodes.BindingBound,
			wantFingerprint:    "fp-same",
		},
		{
			name:               "marks pending confirmation when fingerprint changes",
			currentStatus:      nodes.BindingBound,
			currentFingerprint: "fp-old",
			newFingerprint:     "fp-new",
			wantStatus:         nodes.BindingPendingConfirmation,
			wantFingerprint:    "fp-old",
		},
		{
			name:               "keeps other statuses unchanged",
			currentStatus:      nodes.BindingPendingConfirmation,
			currentFingerprint: "fp-existing",
			newFingerprint:     "fp-new",
			wantStatus:         nodes.BindingPendingConfirmation,
			wantFingerprint:    "fp-existing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, fingerprint := resolveEnrollmentBindingTransition(tt.currentStatus, tt.currentFingerprint, tt.newFingerprint)
			if status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", status, tt.wantStatus)
			}
			if fingerprint != tt.wantFingerprint {
				t.Fatalf("fingerprint = %q, want %q", fingerprint, tt.wantFingerprint)
			}
		})
	}
}

func TestApplyEnrollmentDoesNotAdvanceLastSyncAt(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("nodes.go")
	if err != nil {
		t.Fatalf("ReadFile(nodes.go) error = %v", err)
	}

	applyEnrollment := sourceBetween(t, string(source), "func (r *PostgresNodeRepository) ApplyEnrollment", "func (r *PostgresNodeRepository) RecordAcceptedHeartbeats")
	if strings.Contains(applyEnrollment, "last_sync_at") {
		t.Fatalf("ApplyEnrollment() unexpectedly updates last_sync_at:\n%s", applyEnrollment)
	}
	if !strings.Contains(applyEnrollment, "binding_status = $2") {
		t.Fatal("ApplyEnrollment() source no longer contains the enrollment binding update")
	}

	heartbeatPath := sourceBetween(t, string(source), "func (r *PostgresNodeRepository) RecordAcceptedHeartbeats", "")
	if !strings.Contains(heartbeatPath, "last_sync_at = greatest(coalesce(last_sync_at, $3), $3)") {
		t.Fatal("RecordAcceptedHeartbeats() should remain the path that advances last_sync_at")
	}
}

func TestStoreSourceIncludesSyncTokenValidationForHeartbeatWrites(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("nodes.go")
	if err != nil {
		t.Fatalf("ReadFile(nodes.go) error = %v", err)
	}

	heartbeatPath := sourceBetween(t, string(source), "func (r *PostgresNodeRepository) RecordAcceptedHeartbeats", "")
	if !strings.Contains(heartbeatPath, "coalesce(sync_token_hash, '')") {
		t.Fatal("RecordAcceptedHeartbeats() should load sync_token_hash inside the heartbeat transaction")
	}
	if !strings.Contains(heartbeatPath, "storedSyncTokenHash != hashSyncToken(syncToken)") {
		t.Fatal("RecordAcceptedHeartbeats() should reject mismatched sync token hashes")
	}
	if !strings.Contains(heartbeatPath, "write.Fingerprint != bindingFingerprint") {
		t.Fatal("RecordAcceptedHeartbeats() should continue validating fingerprint within the transaction")
	}
}

func sourceBetween(t *testing.T, source, start, end string) string {
	t.Helper()

	startIndex := strings.Index(source, start)
	if startIndex == -1 {
		t.Fatalf("source missing start marker %q", start)
	}
	section := source[startIndex:]
	if end == "" {
		return section
	}
	endIndex := strings.Index(section, end)
	if endIndex == -1 {
		t.Fatalf("source missing end marker %q", end)
	}
	return section[:endIndex]
}
