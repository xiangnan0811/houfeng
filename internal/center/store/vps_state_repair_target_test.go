package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/targets"
)

func TestVPSStateRepairCreateTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresTargetRepository(pool)
	port := 8443
	input := targets.CreateTargetInput{
		Name:                              "State repair target",
		TargetType:                        targets.TargetTypeService,
		Host:                              "repair.example.test",
		BasePort:                          &port,
		ExecutionMonitoringInstanceLabels: []string{"edge", "production"},
		RunStatus:                         targets.RunStatusEnabled,
		Group:                             "state-repair",
		Labels:                            []string{"https", "critical"},
		Note:                              "created by the production repository",
	}

	created, err := repo.CreateTarget(ctx, input)
	if err != nil {
		t.Fatalf("CreateTarget: %v", err)
	}

	got, err := repo.GetTarget(ctx, created.TargetID)
	if err != nil {
		t.Fatalf("GetTarget(%q): %v", created.TargetID, err)
	}

	if got.TargetID != created.TargetID {
		t.Errorf("readback target ID = %q, CreateTarget returned %q", got.TargetID, created.TargetID)
	}
	if got.Name != input.Name {
		t.Errorf("readback name = %q, want %q", got.Name, input.Name)
	}
	if got.TargetType != input.TargetType {
		t.Errorf("readback target type = %q, want %q", got.TargetType, input.TargetType)
	}
	if got.Host != input.Host {
		t.Errorf("readback host = %q, want %q", got.Host, input.Host)
	}
	if !reflect.DeepEqual(got.BasePort, input.BasePort) {
		t.Errorf("readback base port = %v, want %v", got.BasePort, input.BasePort)
	}
	if !reflect.DeepEqual(got.ExecutionMonitoringInstanceLabels, input.ExecutionMonitoringInstanceLabels) {
		t.Errorf("readback execution monitoring instance labels = %v, want %v", got.ExecutionMonitoringInstanceLabels, input.ExecutionMonitoringInstanceLabels)
	}
	if got.RunStatus != input.RunStatus {
		t.Errorf("readback run status = %q, want %q", got.RunStatus, input.RunStatus)
	}
	if got.Group != input.Group {
		t.Errorf("readback group = %q, want %q", got.Group, input.Group)
	}
	if !reflect.DeepEqual(got.Labels, input.Labels) {
		t.Errorf("readback labels = %v, want %v", got.Labels, input.Labels)
	}
	if got.Note != input.Note {
		t.Errorf("readback note = %q, want %q", got.Note, input.Note)
	}
	if got.CurrentHealthStatus != targets.HealthNormal {
		t.Errorf("readback health status = %q, want %q", got.CurrentHealthStatus, targets.HealthNormal)
	}
	if got.CurrentActiveIncidentCount != 0 {
		t.Errorf("readback active incident count = %d, want 0", got.CurrentActiveIncidentCount)
	}
	if got.CurrentPrimaryIssueSummary != "" {
		t.Errorf("readback primary issue summary = %q, want empty", got.CurrentPrimaryIssueSummary)
	}
}
