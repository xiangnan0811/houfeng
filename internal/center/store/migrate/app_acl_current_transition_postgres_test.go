package migrate

import (
	"strings"
	"testing"
	"time"
)

func TestAppACLCurrentTransitionHeartbeatPreflightPendingSuffix(t *testing.T) {
	p62Transition := appACLCurrentTransition{
		successor: migrationSourceSnapshot{names: []string{
			"0063_tune_heartbeat_incident_policy.sql",
			"0064_add_network_rates_valid.sql",
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
		}},
	}
	p64Transition := appACLCurrentTransition{
		successor: migrationSourceSnapshot{names: []string{
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
		}},
	}

	for _, tc := range []struct {
		name       string
		transition appACLCurrentTransition
		want       bool
		wantError  bool
	}{
		{name: "P62 suffix includes heartbeat policy migration", transition: p62Transition, want: true},
		{name: "P64 suffix starts after heartbeat policy migration", transition: p64Transition, want: false},
		{name: "P63 suffix preserves heartbeat policy", transition: appACLCurrentTransition{
			successor: migrationSourceSnapshot{names: p62Transition.successor.names[1:]},
		}, want: false},
		{
			name: "incomplete successor suffix is rejected",
			transition: appACLCurrentTransition{
				successor: migrationSourceSnapshot{names: p62Transition.successor.names[:3]},
			},
			wantError: true,
		},
		{
			name: "misordered successor suffix is rejected",
			transition: appACLCurrentTransition{
				successor: migrationSourceSnapshot{names: []string{
					"0064_add_network_rates_valid.sql",
					"0063_tune_heartbeat_incident_policy.sql",
					"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
					"0066_constrain_monitoring_and_target_state_values.sql",
				}},
			},
			wantError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(tc.transition)
			if tc.wantError {
				if err == nil {
					t.Fatal("appACLCurrentTransitionAppliesHeartbeatPolicyMigration() error = nil, want unsupported transition")
				}
				return
			}
			if err != nil {
				t.Fatalf("appACLCurrentTransitionAppliesHeartbeatPolicyMigration() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("appACLCurrentTransitionAppliesHeartbeatPolicyMigration() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestAppACLCurrentTransitionWithoutHeartbeatMigrationRequiresExactSettingsSnapshot(t *testing.T) {
	before := appACLCurrentTransitionPreflight{
		settingsSnapshot: []byte(`{"settings_id":"center","incident_defaults":{"stale_threshold_intervals":12},"telegram_bot_token":"before","updated_at":"2025-01-02T03:04:05Z"}`),
	}
	unchanged := []byte(`{"updated_at":"2025-01-02T03:04:05Z","telegram_bot_token":"before","incident_defaults":{"stale_threshold_intervals":12},"settings_id":"center"}`)

	if err := verifyAppliedAppACLCurrentTransitionSettings(before, nil, unchanged, nil, time.Time{}); err != nil {
		t.Fatalf("unchanged complete settings snapshot was rejected: %v", err)
	}

	for _, tc := range []struct {
		name      string
		after     []byte
		wantError string
	}{
		{
			name:      "unrelated settings field changed",
			after:     []byte(`{"settings_id":"center","incident_defaults":{"stale_threshold_intervals":12},"telegram_bot_token":"after","updated_at":"2025-01-02T03:04:05Z"}`),
			wantError: "changed settings without a heartbeat policy migration",
		},
		{
			name:      "updated_at changed",
			after:     []byte(`{"settings_id":"center","incident_defaults":{"stale_threshold_intervals":12},"telegram_bot_token":"before","updated_at":"2025-01-02T03:04:06Z"}`),
			wantError: "changed settings without a heartbeat policy migration",
		},
		{
			name:      "incident defaults changed",
			after:     []byte(`{"settings_id":"center","incident_defaults":{"stale_threshold_intervals":3},"telegram_bot_token":"before","updated_at":"2025-01-02T03:04:05Z"}`),
			wantError: "changed settings without a heartbeat policy migration",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyAppliedAppACLCurrentTransitionSettings(before, nil, tc.after, nil, time.Time{})
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("verifyAppliedAppACLCurrentTransitionSettings() error = %v, want %q", err, tc.wantError)
			}
		})
	}
}
