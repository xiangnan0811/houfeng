package store

import (
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
