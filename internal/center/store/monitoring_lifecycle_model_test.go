package store

import (
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/targets"
	"testing"
	"time"
)

func TestMonitoringLifecycleControlsNeverMasqueradeAsHealthy(t *testing.T) {
	now := time.Now()
	base := monitoringinstances.Record{VPSLifecycleStatus: "active", LifecycleStatus: monitoringinstances.LifecycleEnrolled, MonitoringStatus: monitoringinstances.MonitoringEnabled, BindingStatus: monitoringinstances.BindingBound, LastTrustedOnlineAt: &now, CurrentHealthStatus: monitoringinstances.HealthNormal}
	tests := []struct {
		name   string
		change func(*monitoringinstances.Record)
		want   string
	}{
		{"pending", func(r *monitoringinstances.Record) {
			r.LifecycleStatus = monitoringinstances.LifecyclePendingEnrollment
		}, "未接入"},
		{"retired", func(r *monitoringinstances.Record) { r.LifecycleStatus = monitoringinstances.LifecycleRetired }, "已退役"},
		{"archived owner", func(r *monitoringinstances.Record) { r.VPSLifecycleStatus = "archived" }, "已归档"},
		{"maintenance", func(r *monitoringinstances.Record) { r.MonitoringStatus = monitoringinstances.MonitoringMaintenance }, "维护中"},
		{"paused", func(r *monitoringinstances.Record) { r.MonitoringStatus = monitoringinstances.MonitoringPaused }, "暂停"},
		{"fingerprint pending with stored normal", func(r *monitoringinstances.Record) { r.BindingStatus = monitoringinstances.BindingPendingConfirmation }, "绑定待确认"},
		{"no performance observation", func(r *monitoringinstances.Record) { r.CurrentHealthStatus = "unknown" }, "数据不可用"},
		{"unavailable", func(r *monitoringinstances.Record) { r.LastTrustedOnlineAt = nil }, "数据不可用"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := base
			tt.change(&r)
			projectMonitoringHealth(&r)
			if r.CurrentHealthStatus != tt.want {
				t.Fatalf("health=%q want %q", r.CurrentHealthStatus, tt.want)
			}
		})
	}
}

func TestRetiredMonitoringHasNoRetireEligibility(t *testing.T) {
	result := deriveMonitoringInstanceManagementFindings(monitoringinstances.ManagementReview{Record: monitoringinstances.Record{LifecycleStatus: monitoringinstances.LifecycleRetired}})
	if result[monitoringinstances.ManagementActionRetire].Allowed {
		t.Fatal("retired instance permits a new retirement")
	}
	if len(result) != 1 {
		t.Fatalf("obsolete actions exposed: %+v", result)
	}
}

func TestTargetLifecycleDoesNotOverwriteRuntimeControl(t *testing.T) {
	current := targets.TargetRecord{LifecycleStatus: targets.LifecycleActive, RunStatus: targets.RunStatusMaintenance}
	retired, err := targetTransitionForRecord(current, "archive")
	if err != nil || retired.lifecycleStatus != targets.LifecycleRetired || retired.runStatus != targets.RunStatusPaused {
		t.Fatalf("retire=%+v err=%v", retired, err)
	}
	current.LifecycleStatus = targets.LifecycleRetired
	current.RunStatus = targets.RunStatusPaused
	if _, err := targetTransitionForRecord(current, "resume"); err == nil {
		t.Fatal("retired target resumed through runtime control")
	}
	if _, err := targetTransitionForRecord(current, "archive"); err == nil {
		t.Fatal("duplicate target retirement accepted")
	}
	restored, err := targetTransitionForRecord(current, "restore_to_paused")
	if err != nil || restored.lifecycleStatus != targets.LifecycleActive || restored.runStatus != targets.RunStatusPaused {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
}
