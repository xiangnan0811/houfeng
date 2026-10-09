package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/observations"
	"houfeng/internal/contracts/agentapi"
)

func TestProjectProbeLiveObservationsBatchesCappedTimes(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	exec := &captureObservationExec{}
	writes := []observations.ProbeObservationWrite{
		{
			TargetID:    "tg_b",
			ProbeItemID: "pb_2",
			ObservedAt:  base.Add(5 * time.Minute),
			ReceivedAt:  base.Add(6 * time.Minute),
			ResultKind:  agentapi.ProbeResultSuccess,
		},
		{
			TargetID:    "tg_b",
			ProbeItemID: "pb_2",
			ObservedAt:  base.Add(20 * time.Minute),
			ReceivedAt:  base.Add(15 * time.Minute),
			ResultKind:  agentapi.ProbeResultFailure,
		},
		{
			TargetID:    "tg_b",
			ProbeItemID: "pb_2",
			ObservedAt:  base.Add(14 * time.Minute),
			ReceivedAt:  base.Add(14 * time.Minute),
			ResultKind:  agentapi.ProbeResultSuccess,
		},
		{
			TargetID:    "tg_a",
			ProbeItemID: "pb_9",
			ObservedAt:  base.Add(3 * time.Minute),
			ReceivedAt:  base.Add(2 * time.Minute),
			ResultKind:  agentapi.ProbeResultSuccess,
		},
		{
			TargetID:           "tg_a",
			ProbeItemID:        "pb_ignored_maintenance",
			ObservedAt:         base.Add(30 * time.Minute),
			ReceivedAt:         base.Add(30 * time.Minute),
			ResultKind:         agentapi.ProbeResultSuccess,
			MaintenanceContext: true,
		},
		{
			TargetID:     "tg_a",
			ProbeItemID:  "pb_ignored_backfill",
			ObservedAt:   base.Add(31 * time.Minute),
			ReceivedAt:   base.Add(31 * time.Minute),
			ResultKind:   agentapi.ProbeResultFailure,
			IsBackfilled: true,
		},
		{
			TargetID:    "tg_a",
			ProbeItemID: "pb_ignored_result",
			ObservedAt:  base.Add(32 * time.Minute),
			ReceivedAt:  base.Add(32 * time.Minute),
			ResultKind:  "timeout",
		},
	}

	if err := projectProbeLiveObservations(context.Background(), exec, writes); err != nil {
		t.Fatalf("projectProbeLiveObservations() error = %v", err)
	}
	if len(exec.calls) != 2 {
		t.Fatalf("len(exec.calls) = %d, want 2", len(exec.calls))
	}

	want := []struct {
		targetID    string
		probeItemID string
		effectiveAt time.Time
	}{
		{targetID: "tg_a", probeItemID: "pb_9", effectiveAt: base.Add(2 * time.Minute)},
		{targetID: "tg_b", probeItemID: "pb_2", effectiveAt: base.Add(15 * time.Minute)},
	}
	for index, expected := range want {
		call := exec.calls[index]
		if call.args[0] != expected.targetID || call.args[1] != expected.probeItemID {
			t.Fatalf("call %d scope = %#v, want target=%q probe=%q", index, call.args[:2], expected.targetID, expected.probeItemID)
		}
		got, ok := call.args[2].(time.Time)
		if !ok || !got.Equal(expected.effectiveAt) {
			t.Fatalf("call %d effective timestamp = %#v, want %v", index, call.args[2], expected.effectiveAt)
		}
	}
}
