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
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
		}},
	}
	p64Transition := appACLCurrentTransition{
		successor: migrationSourceSnapshot{names: []string{
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
		}},
	}

	p69Transition := appACLCurrentTransition{
		profile:   appACLCurrentProfileP69,
		successor: migrationSourceSnapshot{names: []string{"0070_add_record_import_destination_subject.sql", "0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}},
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
		{name: "P66 lifecycle-only suffix preserves heartbeat policy", transition: appACLCurrentTransition{
			successor: migrationSourceSnapshot{names: []string{"0067_refactor_vps_monitoring_lifecycle.sql", "0068_normalize_ip_quality_host_address_identity.sql", "0069_add_cpu_rates_valid.sql", "0070_add_record_import_destination_subject.sql", "0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}},
		}, want: false},
		{name: "P67 identity-only suffix preserves heartbeat and lifecycle policy", transition: appACLCurrentTransition{
			successor: migrationSourceSnapshot{names: []string{"0068_normalize_ip_quality_host_address_identity.sql", "0069_add_cpu_rates_valid.sql", "0070_add_record_import_destination_subject.sql", "0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}},
		}, want: false},
		{name: "P68 CPU-validity-only suffix preserves settings", transition: appACLCurrentTransition{
			successor: migrationSourceSnapshot{names: []string{"0069_add_cpu_rates_valid.sql", "0070_add_record_import_destination_subject.sql", "0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}},
		}, want: false},
		{name: "P69 destination-subject and access-management suffix preserves settings", transition: p69Transition, want: false},
		{name: "P70 access-management-only suffix preserves settings", transition: appACLCurrentTransition{
			profile:   appACLCurrentProfileP70,
			successor: migrationSourceSnapshot{names: []string{"0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}},
		}, want: false},
		{name: "P71 freshness-only suffix preserves settings", transition: appACLCurrentTransition{
			profile:   appACLCurrentProfileP71,
			successor: migrationSourceSnapshot{names: []string{"0072_add_target_observation_freshness.sql"}},
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
		{
			name: "duplicate C69 suffix is rejected",
			transition: appACLCurrentTransition{
				successor: migrationSourceSnapshot{names: []string{
					"0069_add_cpu_rates_valid.sql",
					"0069_add_cpu_rates_valid.sql",
				}},
			},
			wantError: true,
		},
		{
			name: "duplicate C68 suffix is rejected",
			transition: appACLCurrentTransition{
				successor: migrationSourceSnapshot{names: []string{
					"0068_normalize_ip_quality_host_address_identity.sql",
					"0068_normalize_ip_quality_host_address_identity.sql",
					"0069_add_cpu_rates_valid.sql",
				}},
			},
			wantError: true,
		},
		{
			name: "unknown suffix is rejected",
			transition: appACLCurrentTransition{
				successor: migrationSourceSnapshot{names: []string{"0071_future.sql"}},
			},
			wantError: true,
		},
		{
			name: "nonterminal access-management suffix is rejected",
			transition: appACLCurrentTransition{
				successor: migrationSourceSnapshot{names: []string{
					"0071_add_access_management.sql",
					"0070_add_record_import_destination_subject.sql",
				}},
			},
			wantError: true,
		},
		{
			name: "C69 before C68 is rejected",
			transition: appACLCurrentTransition{
				successor: migrationSourceSnapshot{names: []string{
					"0069_add_cpu_rates_valid.sql",
					"0068_normalize_ip_quality_host_address_identity.sql",
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
		settingsRowPresent: true,
		settingsSnapshot:   []byte(`{"settings_id":"center","incident_defaults":{"stale_threshold_intervals":12},"telegram_bot_token":"before","updated_at":"2025-01-02T03:04:05Z"}`),
	}
	unchanged := []byte(`{"updated_at":"2025-01-02T03:04:05Z","telegram_bot_token":"before","incident_defaults":{"stale_threshold_intervals":12},"settings_id":"center"}`)

	if err := verifyAppliedAppACLCurrentTransitionSettings(before, true, nil, unchanged, nil, time.Time{}); err != nil {
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
			err := verifyAppliedAppACLCurrentTransitionSettings(before, true, nil, tc.after, nil, time.Time{})
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("verifyAppliedAppACLCurrentTransitionSettings() error = %v, want %q", err, tc.wantError)
			}
		})
	}
}

func TestAppACLCurrentTransitionSettingsRowPresenceAcrossRegisteredSuffixes(t *testing.T) {
	suffixes := []struct {
		name       string
		transition appACLCurrentTransition
	}{
		{name: "P62", transition: appACLCurrentTransition{successor: migrationSourceSnapshot{names: []string{
			"0063_tune_heartbeat_incident_policy.sql",
			"0064_add_network_rates_valid.sql",
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
		}}}},
		{name: "P63", transition: appACLCurrentTransition{successor: migrationSourceSnapshot{names: []string{
			"0064_add_network_rates_valid.sql",
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
		}}}},
		{name: "P64", transition: appACLCurrentTransition{successor: migrationSourceSnapshot{names: []string{
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
		}}}},
		{name: "P66", transition: appACLCurrentTransition{successor: migrationSourceSnapshot{names: []string{
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
		}}}},
		{name: "P67", transition: appACLCurrentTransition{successor: migrationSourceSnapshot{names: []string{
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
		}}}},
		{name: "P68", transition: appACLCurrentTransition{successor: migrationSourceSnapshot{names: []string{
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
		}}}},
		{name: "P69", transition: appACLCurrentTransition{
			profile:   appACLCurrentProfileP69,
			successor: migrationSourceSnapshot{names: []string{"0070_add_record_import_destination_subject.sql", "0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}},
		}},
		{name: "P70", transition: appACLCurrentTransition{
			profile:   appACLCurrentProfileP70,
			successor: migrationSourceSnapshot{names: []string{"0071_add_access_management.sql", "0072_add_target_observation_freshness.sql"}},
		}},
		{name: "P71", transition: appACLCurrentTransition{
			profile:   appACLCurrentProfileP71,
			successor: migrationSourceSnapshot{names: []string{"0072_add_target_observation_freshness.sql"}},
		}},
	}
	for _, suffix := range suffixes {
		t.Run(suffix.name, func(t *testing.T) {
			if _, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(suffix.transition); err != nil {
				t.Fatalf("registered %s suffix rejected: %v", suffix.name, err)
			}
			absent := appACLCurrentTransitionPreflight{
				lifecycleMigrationPending: true,
				settingsSnapshot:          []byte(`not-json`),
				settingsExceptTransition:  []byte(`not-json`),
			}
			if err := verifyAppliedAppACLCurrentTransitionSettings(absent, false, nil, nil, nil, time.Time{}); err != nil {
				t.Fatalf("absent to absent settings snapshot was rejected: %v", err)
			}
			if err := verifyAppliedAppACLCurrentTransitionSettings(absent, true, nil, []byte(`{}`), nil, time.Time{}); err == nil || !strings.Contains(err.Error(), "changed settings row presence") {
				t.Fatalf("absent to present settings snapshot error = %v, want row-presence rejection", err)
			}

			present := appACLCurrentTransitionPreflight{settingsRowPresent: true}
			if err := verifyAppliedAppACLCurrentTransitionSettings(present, false, nil, nil, nil, time.Time{}); err == nil || !strings.Contains(err.Error(), "changed settings row presence") {
				t.Fatalf("present to absent settings snapshot error = %v, want row-presence rejection", err)
			}
		})
	}
}
func TestAppACLCurrentTransitionP68PreservesSettings(t *testing.T) {
	transition := appACLCurrentTransition{
		successor: migrationSourceSnapshot{names: []string{"0069_add_cpu_rates_valid.sql", "0070_add_record_import_destination_subject.sql", "0071_add_access_management.sql"}},
	}
	if got, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(transition); err != nil || got {
		t.Fatalf("P68 heartbeat classification = %t, error = %v, want false/nil", got, err)
	}
	before := appACLCurrentTransitionPreflight{
		settingsRowPresent: true,
		settingsSnapshot:   []byte(`{"settings_id":"center","updated_at":"2025-01-02T03:04:05Z","telegram_bot_token":"before"}`),
	}
	unchanged := []byte(`{"telegram_bot_token":"before","settings_id":"center","updated_at":"2025-01-02T03:04:05Z"}`)
	if err := verifyAppliedAppACLCurrentTransitionSettings(before, true, nil, unchanged, nil, time.Time{}); err != nil {
		t.Fatalf("P68 unchanged settings snapshot was rejected: %v", err)
	}
	changed := []byte(`{"telegram_bot_token":"after","settings_id":"center","updated_at":"2025-01-02T03:04:05Z"}`)
	if err := verifyAppliedAppACLCurrentTransitionSettings(before, true, nil, changed, nil, time.Time{}); err == nil ||
		!strings.Contains(err.Error(), "changed settings without a heartbeat policy migration") {
		t.Fatalf("P68 changed settings snapshot error = %v, want preservation rejection", err)
	}
}

func TestAppACLCurrentTransitionHeartbeatSettingsTransformations(t *testing.T) {
	beforeDefaults := []byte(`{"heartbeat_interval_seconds":5,"stale_threshold_intervals":3,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true}`)
	afterDefaults := []byte(`{"notify_on_recovered":true,"stale_threshold_intervals":12,"notify_on_started":true,"heartbeat_interval_seconds":5,"notify_on_escalated":true,"sweep_interval_seconds":5}`)
	if err := verifyAppliedAppACLCurrentTransitionSettings(
		appACLCurrentTransitionPreflight{
			settingsRowPresent:              true,
			heartbeatPolicyMigrationPending: true,
			incidentDefaults:                beforeDefaults,
			settingsExceptTransition:        []byte(`{"settings_id":"center","telegram_bot_token":"before"}`),
			updatedAt:                       time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
			staleThreshold:                  3,
		},
		true,
		afterDefaults,
		[]byte(`{"settings_id":"center","incident_defaults":{"stale_threshold_intervals":12},"telegram_bot_token":"before","updated_at":"2025-01-02T03:04:06Z"}`),
		[]byte(`{"settings_id":"center","telegram_bot_token":"before"}`),
		time.Date(2025, 1, 2, 3, 4, 6, 0, time.UTC),
	); err != nil {
		t.Fatalf("default heartbeat settings transformation was rejected: %v", err)
	}

	custom := appACLCurrentTransitionPreflight{
		settingsRowPresent:              true,
		heartbeatPolicyMigrationPending: true,
		incidentDefaults:                []byte(`{"heartbeat_interval_seconds":5,"stale_threshold_intervals":20}`),
		settingsExceptTransition:        []byte(`{"settings_id":"center","telegram_bot_token":"before"}`),
		updatedAt:                       time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		staleThreshold:                  20,
	}
	if err := verifyAppliedAppACLCurrentTransitionSettings(
		custom,
		true,
		custom.incidentDefaults,
		[]byte(`{"settings_id":"center","telegram_bot_token":"after"}`),
		custom.settingsExceptTransition,
		custom.updatedAt,
	); err != nil {
		t.Fatalf("custom heartbeat settings transformation was rejected: %v", err)
	}
}
