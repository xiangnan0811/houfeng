package runtime

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func TestLiveSignalGeneratedAtTransmissionAndNeverBuffered(t *testing.T) {
	ctx := context.Background()
	queue := openReplayTestQueue(t)
	r := &Runtime{syncQueue: queue}
	request := cooldownTestRequest(time.Now())
	if err := r.attachLiveSignal(ctx, &request, "fp-current"); err != nil {
		t.Fatal(err)
	}
	first := request.LiveSignal.Sequence
	if err := r.attachLiveSignal(ctx, &request, "fp-current"); err != nil {
		t.Fatal(err)
	}
	if request.LiveSignal.Sequence != first+1 || request.LiveSignal.Fingerprint != "fp-current" {
		t.Fatal("live signal must be fresh and identity-bound")
	}
	if _, err := queue.Enqueue(ctx, request); err != nil {
		t.Fatal(err)
	}
	entries, err := queue.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Request.LiveSignal != nil {
		t.Fatal("live evidence persisted in history queue")
	}
	// A new runtime never resumes an old in-memory cursor, even when using the
	// same file after a clean shutdown rather than a simulated crash.
	restarted := &Runtime{syncQueue: queue}
	if err := restarted.attachLiveSignal(ctx, &request, "fp-current"); err != nil {
		t.Fatal(err)
	}
	if request.LiveSignal.Sequence != agentapi.LiveSequenceReservation+1 {
		t.Fatalf("restart sequence=%d", request.LiveSignal.Sequence)
	}
}

func TestStopCollectionClearsPlanAndPendingWorkAndBuildsMinimalControl(t *testing.T) {
	r := &Runtime{currentPlan: &agentapi.SyncPlan{HostSampleFrequencyTier: "5s"}, pendingIPReports: []agentapi.IPQualityReportPayload{{IPAddress: "192.0.2.1"}}}
	r.applySyncPlan(context.Background(), &agentapi.SyncResponse{StopCollection: true, Plan: &agentapi.SyncPlan{PendingAction: &agentapi.PendingAction{CommandID: "uptime"}}})
	if r.currentPlan != nil || len(r.pendingIPReports) != 0 || len(r.pendingResults) != 0 {
		t.Fatal("stop response retained pending work")
	}
	req := r.buildSyncRequest(context.Background(), "mi_1", "mas_1.secret", time.Now(), "fp", "batch")
	if req.BatchSequence != 0 || len(req.Heartbeats)+len(req.HostSamples)+len(req.IPQualityReports)+len(req.CommandResults) != 0 || req.SessionID != "mas_1" || req.LiveSignal != nil {
		t.Fatalf("stopped request=%#v", req)
	}
}
