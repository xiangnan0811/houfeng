package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/monitoringinstances"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/vpsassets"
	"houfeng/internal/center/vpsoverview"
)

func TestVPSOverviewMonitoringFreshnessUsesEffectiveHeartbeatPolicy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	defaultSettings := centersettings.Default()
	freshHeartbeat := now.Add(-10 * time.Second)
	staleHeartbeat := now.Add(-60 * time.Second)
	configuredHeartbeat := now.Add(-40 * time.Second)

	cases := []struct {
		name         string
		heartbeat    *time.Time
		monitoring   string
		lifecycle    string
		binding      string
		settings     centersettings.CenterSettings
		wantState    string
		wantReason   string
		wantStatus   string
		wantHealth   string
		wantDetail   string
		wantObserved bool
	}{
		{
			name:         "fresh heartbeat",
			heartbeat:    &freshHeartbeat,
			monitoring:   monitoringinstances.MonitoringEnabled,
			lifecycle:    monitoringinstances.LifecycleInUse,
			binding:      monitoringinstances.BindingBound,
			settings:     defaultSettings,
			wantState:    vpsoverview.SectionReady,
			wantObserved: true,
		},
		{
			name:         "old heartbeat",
			heartbeat:    &staleHeartbeat,
			monitoring:   monitoringinstances.MonitoringEnabled,
			lifecycle:    monitoringinstances.LifecycleInUse,
			binding:      monitoringinstances.BindingBound,
			settings:     defaultSettings,
			wantState:    vpsoverview.SectionStale,
			wantReason:   "monitoring_heartbeat_stale",
			wantObserved: true,
		},
		{
			name:       "pending first heartbeat",
			monitoring: monitoringinstances.MonitoringEnabled,
			lifecycle:  monitoringinstances.LifecyclePendingEnrollment,
			binding:    monitoringinstances.BindingUnbound,
			settings:   defaultSettings,
			wantState:  vpsoverview.SectionReady,
			wantReason: "monitoring_first_heartbeat_missing",
			wantHealth: "unknown",
			wantDetail: "等待首次心跳",
		},
		{
			name:       "paused without heartbeat remains visible",
			monitoring: monitoringinstances.MonitoringPaused,
			lifecycle:  monitoringinstances.LifecycleInUse,
			binding:    monitoringinstances.BindingBound,
			settings:   defaultSettings,
			wantState:  vpsoverview.SectionReady,
			wantReason: "monitoring_first_heartbeat_missing",
			wantStatus: monitoringinstances.MonitoringPaused,
			wantHealth: "unknown",
			wantDetail: "等待首次心跳",
		},
		{
			name:       "maintenance without heartbeat remains visible",
			monitoring: monitoringinstances.MonitoringMaintenance,
			lifecycle:  monitoringinstances.LifecycleInUse,
			binding:    monitoringinstances.BindingBound,
			settings:   defaultSettings,
			wantState:  vpsoverview.SectionReady,
			wantReason: "monitoring_first_heartbeat_missing",
			wantStatus: monitoringinstances.MonitoringMaintenance,
			wantHealth: "unknown",
			wantDetail: "等待首次心跳",
		},
		{
			name:         "paused old heartbeat is not fresh",
			heartbeat:    &staleHeartbeat,
			monitoring:   monitoringinstances.MonitoringPaused,
			lifecycle:    monitoringinstances.LifecycleInUse,
			binding:      monitoringinstances.BindingBound,
			settings:     defaultSettings,
			wantState:    vpsoverview.SectionStale,
			wantReason:   "monitoring_heartbeat_stale",
			wantStatus:   monitoringinstances.MonitoringPaused,
			wantObserved: true,
		},
		{
			name:       "configured global threshold",
			heartbeat:  &configuredHeartbeat,
			monitoring: monitoringinstances.MonitoringEnabled,
			lifecycle:  monitoringinstances.LifecycleInUse,
			binding:    monitoringinstances.BindingBound,
			settings: func() centersettings.CenterSettings {
				settings := defaultSettings
				settings.IncidentDefaults.HeartbeatIntervalSeconds = 10
				settings.IncidentDefaults.StaleThresholdIntervals = 3
				return settings
			}(),
			wantState:    vpsoverview.SectionStale,
			wantReason:   "monitoring_heartbeat_stale",
			wantObserved: true,
		},
	}

	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repo, err := NewVPSOverviewRepository(
				&fakeVPSRepo{record: vpsassets.Record{VPSID: "vps_1"}},
				&fakeMonitoringLinks{links: []assetlinks.MonitoringInstanceSummary{{
					MonitoringInstanceID: "mi_1",
					LifecycleStatus:      test.lifecycle,
					MonitoringStatus:     test.monitoring,
					BindingStatus:        test.binding,
					CurrentHealthStatus:  monitoringinstances.HealthNormal,
					LastHeartbeatAt:      test.heartbeat,
				}}},
				&fakeIPQuality{},
				&fakeIPQualityAvailability{settings: test.settings},
				&fakeSubscriptions{},
				&fakeServices{},
				&fakeDomains{},
			)
			if err != nil {
				t.Fatalf("NewVPSOverviewRepository: %v", err)
			}
			repo.now = func() time.Time { return now }

			got, err := repo.LoadMonitoring(context.Background(), "vps_1")
			if err != nil {
				t.Fatalf("LoadMonitoring: %v", err)
			}
			if got.Section.State != test.wantState {
				t.Fatalf("section state = %q, want %q (%#v)", got.Section.State, test.wantState, got.Section)
			}
			if got.Section.ReasonCode != test.wantReason {
				t.Fatalf("reason = %q, want %q", got.Section.ReasonCode, test.wantReason)
			}
			if test.wantStatus != "" && got.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, test.wantStatus)
			}
			if test.wantHealth != "" && got.Health != test.wantHealth {
				t.Fatalf("health = %q, want %q", got.Health, test.wantHealth)
			}
			if test.wantDetail != "" && got.Detail != test.wantDetail {
				t.Fatalf("detail = %q, want %q", got.Detail, test.wantDetail)
			}
			if (got.Section.ObservedAt != nil) != test.wantObserved {
				t.Fatalf("observed_at = %#v, want present=%t", got.Section.ObservedAt, test.wantObserved)
			}
		})
	}
}

func TestVPSOverviewMonitoringUnlinkedDoesNotRequireSettings(t *testing.T) {
	t.Parallel()

	availability := &fakeIPQualityAvailability{settings: centersettings.Default(), settingsErr: errors.New("settings read failed")}
	repo, err := NewVPSOverviewRepository(
		&fakeVPSRepo{},
		&fakeMonitoringLinks{},
		&fakeIPQuality{},
		availability,
		&fakeSubscriptions{},
		&fakeServices{},
		&fakeDomains{},
	)
	if err != nil {
		t.Fatalf("NewVPSOverviewRepository: %v", err)
	}

	got, err := repo.LoadMonitoring(context.Background(), "vps_1")
	if err != nil {
		t.Fatalf("LoadMonitoring: %v", err)
	}
	if got.Status != "unlinked" || got.Section.State != vpsoverview.SectionReady {
		t.Fatalf("monitoring = %#v, want unlinked ready", got)
	}
}
