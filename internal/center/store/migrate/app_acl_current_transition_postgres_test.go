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
func TestAppACLCurrentTransitionDeploymentOverrideSuffixClassification(t *testing.T) {
	t.Run("P74 suffix", func(t *testing.T) {
		transition := appACLCurrentTransition{
			profile: appACLCurrentProfileP74,
			successor: migrationSourceSnapshot{names: []string{
				"0075_add_settings_deployment_overrides.sql",
			}},
		}
		if got, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(transition); err != nil || got {
			t.Fatalf("P74 heartbeat classification = %t, error = %v, want false/nil", got, err)
		}
	})
	t.Run("P74 suffix is required", func(t *testing.T) {
		transition := appACLCurrentTransition{
			profile: appACLCurrentProfileP74,
		}
		if _, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(transition); err == nil {
			t.Fatal("classifier accepted P74 without terminal 0075 successor")
		}
	})
	t.Run("duplicate terminal 0075 suffix is rejected", func(t *testing.T) {
		transition := appACLCurrentTransition{
			profile: appACLCurrentProfileP74,
			successor: migrationSourceSnapshot{names: []string{
				"0075_add_settings_deployment_overrides.sql",
				"0075_add_settings_deployment_overrides.sql",
			}},
		}
		if _, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(transition); err == nil {
			t.Fatal("classifier accepted duplicate terminal 0075 successor")
		}
	})
	t.Run("P73 suffix strips terminal 0075", func(t *testing.T) {
		transition := appACLCurrentTransition{
			profile: appACLCurrentProfileP73,
			successor: migrationSourceSnapshot{names: []string{
				"0074_add_subscription_exchange_rate_month_end.sql",
				"0075_add_settings_deployment_overrides.sql",
			}},
		}
		if got, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(transition); err != nil || got {
			t.Fatalf("P73 heartbeat classification = %t, error = %v, want false/nil", got, err)
		}
	})
	t.Run("interior 0075 suffix is rejected", func(t *testing.T) {
		transition := appACLCurrentTransition{
			profile: appACLCurrentProfileP74,
			successor: migrationSourceSnapshot{names: []string{
				"0075_add_settings_deployment_overrides.sql",
				"0076_future.sql",
			}},
		}
		if _, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(transition); err == nil {
			t.Fatal("classifier accepted nonterminal 0075 successor")
		}
	})
}

func TestAppACLCurrentTransitionDeploymentOverrideSettingsTransformation(t *testing.T) {
	beforeRaw := []byte(`{"settings_id":"center","incident_defaults":{"stale_threshold_intervals":20},"telegram_bot_token":"before","updated_at":"2025-01-02T03:04:05Z"}`)
	beforeExcept := []byte(`{"settings_id":"center","telegram_bot_token":"before"}`)
	afterRaw := []byte(`{"updated_at":"2025-01-02T03:04:05Z","telegram_bot_token":"before","incident_defaults":{"stale_threshold_intervals":20},"incident_sweep_runtime_managed":true,"settings_id":"center"}`)
	afterExcept := []byte(`{"incident_sweep_runtime_managed":true,"settings_id":"center","telegram_bot_token":"before"}`)
	before := appACLCurrentTransitionPreflight{
		settingsDeploymentOverridesMigrationPending: true,
		settingsRowPresent:                          true,
		settingsSnapshot:                            beforeRaw,
		settingsExceptTransition:                    beforeExcept,
		incidentDefaults:                            []byte(`{"stale_threshold_intervals":20}`),
		updatedAt:                                   time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	if err := verifyAppliedAppACLCurrentTransitionSettings(
		before,
		true,
		before.incidentDefaults,
		afterRaw,
		afterExcept,
		before.updatedAt,
	); err != nil {
		t.Fatalf("0075 existing-row settings transformation was rejected: %v", err)
	}
	if err := verifyAppliedAppACLCurrentTransitionSettings(
		before,
		true,
		before.incidentDefaults,
		[]byte(`{"settings_id":"center","incident_defaults":{"stale_threshold_intervals":20},"telegram_bot_token":"before","updated_at":"2025-01-02T03:04:05Z","incident_sweep_runtime_managed":false}`),
		afterExcept,
		before.updatedAt,
	); err == nil || !strings.Contains(err.Error(), "changed settings without a heartbeat policy migration") {
		t.Fatalf("0075 false ownership transformation error = %v, want exact settings rejection", err)
	}
	missing := before
	missing.settingsRowPresent = false
	if err := verifyAppliedAppACLCurrentTransitionSettings(missing, false, nil, nil, nil, time.Time{}); err != nil {
		t.Fatalf("0075 missing-row settings transformation was rejected: %v", err)
	}
	t.Run("existing-row corruption remains fail closed", func(t *testing.T) {
		for name, corrupted := range map[string][]byte{
			"unrelated field":   []byte(`{"settings_id":"center","incident_defaults":{"stale_threshold_intervals":20},"telegram_bot_token":"tampered","updated_at":"2025-01-02T03:04:05Z","incident_sweep_runtime_managed":true}`),
			"incident defaults": []byte(`{"settings_id":"center","incident_defaults":{"stale_threshold_intervals":21},"telegram_bot_token":"before","updated_at":"2025-01-02T03:04:05Z","incident_sweep_runtime_managed":true}`),
			"updated_at":        []byte(`{"settings_id":"center","incident_defaults":{"stale_threshold_intervals":20},"telegram_bot_token":"before","updated_at":"2025-01-02T03:04:06Z","incident_sweep_runtime_managed":true}`),
		} {
			t.Run(name, func(t *testing.T) {
				if err := verifyAppliedAppACLCurrentTransitionSettings(
					before,
					true,
					before.incidentDefaults,
					corrupted,
					afterExcept,
					before.updatedAt,
				); err == nil || !strings.Contains(err.Error(), "changed settings without a heartbeat policy migration") {
					t.Fatalf("0075 %s corruption error = %v, want exact settings rejection", name, err)
				}
			})
		}
	})
	t.Run("explicit false repeat is preserved", func(t *testing.T) {
		repeat := appACLCurrentTransitionPreflight{
			settingsRowPresent:       true,
			settingsSnapshot:         []byte(`{"settings_id":"center","incident_sweep_runtime_managed":false}`),
			settingsExceptTransition: []byte(`{"settings_id":"center","incident_sweep_runtime_managed":false}`),
		}
		if err := verifyAppliedAppACLCurrentTransitionSettings(
			repeat,
			true,
			nil,
			repeat.settingsSnapshot,
			repeat.settingsExceptTransition,
			time.Time{},
		); err != nil {
			t.Fatalf("0075 repeat changed explicit false ownership: %v", err)
		}
	})
}

func TestAppACLCurrentTransitionAddressFamiliesSuffixClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		profile   appACLCurrentProfileID
		names     []string
		wantError bool
	}{
		{name: "P75 exact", profile: appACLCurrentProfileP75, names: []string{"0076_ip_quality_address_families.sql"}},
		{name: "P74 exact", profile: appACLCurrentProfileP74, names: []string{"0075_add_settings_deployment_overrides.sql", "0076_ip_quality_address_families.sql"}},
		{name: "P75 missing", profile: appACLCurrentProfileP75, wantError: true},
		{name: "P75 duplicate", profile: appACLCurrentProfileP75, names: []string{"0076_ip_quality_address_families.sql", "0076_ip_quality_address_families.sql"}, wantError: true},
		{name: "P74 reordered", profile: appACLCurrentProfileP74, names: []string{"0076_ip_quality_address_families.sql", "0075_add_settings_deployment_overrides.sql"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(appACLCurrentTransition{
				profile:   tc.profile,
				successor: migrationSourceSnapshot{names: tc.names},
			})
			if (err != nil) != tc.wantError || got {
				t.Fatalf("heartbeat policy classification = %t/%v, want false/error=%t", got, err, tc.wantError)
			}
		})
	}
}

func TestAppACLCurrentTransitionProductSuffixClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		profile   appACLCurrentProfileID
		names     []string
		wantError bool
	}{
		{name: "P76 exact", profile: appACLCurrentProfileP76, names: []string{"0077_ip_quality_product.sql"}},
		{name: "P75 exact", profile: appACLCurrentProfileP75, names: []string{"0076_ip_quality_address_families.sql", "0077_ip_quality_product.sql"}},
		{name: "P76 missing", profile: appACLCurrentProfileP76, wantError: true},
		{name: "P76 duplicate", profile: appACLCurrentProfileP76, names: []string{"0077_ip_quality_product.sql", "0077_ip_quality_product.sql"}, wantError: true},
		{name: "P75 reordered", profile: appACLCurrentProfileP75, names: []string{"0077_ip_quality_product.sql", "0076_ip_quality_address_families.sql"}, wantError: true},
		{name: "P75 missing family state", profile: appACLCurrentProfileP75, names: []string{"0077_ip_quality_product.sql"}, wantError: true},
		{name: "P76 unknown tail", profile: appACLCurrentProfileP76, names: []string{"0077_ip_quality_product.sql", "0078_future.sql"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(appACLCurrentTransition{
				profile: tc.profile, successor: migrationSourceSnapshot{names: tc.names},
			})
			if (err != nil) != tc.wantError || got {
				t.Fatalf("heartbeat policy classification = %t/%v, want false/error=%t", got, err, tc.wantError)
			}
		})
	}
}

func TestAppACLCurrentTransitionCollectionScopeSuffixClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		names     []string
		wantError bool
	}{
		{name: "exact", names: []string{"0078_ip_quality_collection_scope.sql", "0079_ip_quality_cache_expiry.sql"}},
		{name: "missing", wantError: true},
		{name: "missing index", names: []string{"0078_ip_quality_collection_scope.sql"}, wantError: true},
		{name: "duplicate", names: []string{"0078_ip_quality_collection_scope.sql", "0078_ip_quality_collection_scope.sql"}, wantError: true},
		{name: "nonterminal", names: []string{"0078_ip_quality_collection_scope.sql", "0079_future.sql"}, wantError: true},
		{name: "unknown", names: []string{"0079_future.sql"}, wantError: true},
		{name: "already applied product", names: []string{"0077_ip_quality_product.sql", "0078_ip_quality_collection_scope.sql"}, wantError: true},
		{name: "already applied family view", names: []string{"0076_ip_quality_address_families.sql", "0078_ip_quality_collection_scope.sql"}, wantError: true},
		{name: "reordered", names: []string{"0078_ip_quality_collection_scope.sql", "0077_ip_quality_product.sql"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(appACLCurrentTransition{
				profile: appACLCurrentProfileP77, successor: migrationSourceSnapshot{names: tc.names},
			})
			if got || (err != nil) != tc.wantError {
				t.Fatalf("collection scope heartbeat classification = %t/%v, want false/error=%t", got, err, tc.wantError)
			}
		})
	}
}

func TestAppACLCurrentTransitionCacheExpirySuffixClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		names     []string
		wantError bool
	}{
		{name: "exact", names: []string{"0079_ip_quality_cache_expiry.sql"}},
		{name: "missing", wantError: true},
		{name: "duplicate", names: []string{"0079_ip_quality_cache_expiry.sql", "0079_ip_quality_cache_expiry.sql"}, wantError: true},
		{name: "nonterminal", names: []string{"0079_ip_quality_cache_expiry.sql", "0080_future.sql"}, wantError: true},
		{name: "unknown", names: []string{"0080_future.sql"}, wantError: true},
		{name: "already applied scope", names: []string{"0078_ip_quality_collection_scope.sql", "0079_ip_quality_cache_expiry.sql"}, wantError: true},
		{name: "reordered", names: []string{"0079_ip_quality_cache_expiry.sql", "0078_ip_quality_collection_scope.sql"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(appACLCurrentTransition{
				profile: appACLCurrentProfileP78, successor: migrationSourceSnapshot{names: tc.names},
			})
			if got || (err != nil) != tc.wantError {
				t.Fatalf("cache expiry heartbeat classification = %t/%v, want false/error=%t", got, err, tc.wantError)
			}
		})
	}
}

func TestAppACLCurrentRegisteredSuffixesPreservePolicyClassification(t *testing.T) {
	for _, definition := range appACLCurrentTransitionDefinitions {
		got, err := appACLCurrentTransitionAppliesHeartbeatPolicyMigration(appACLCurrentTransition{
			profile: definition.profile, successor: migrationSourceSnapshot{names: definition.successorMigrations},
		})
		if err != nil || got != (definition.profile == appACLCurrentProfileP62) {
			t.Fatalf("registered profile %d heartbeat classification = %t/%v", definition.profile, got, err)
		}
	}
}
