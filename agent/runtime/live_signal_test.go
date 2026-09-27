package runtime

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func TestLiveSignalGeneratedAtTransmissionAndNeverBuffered(t *testing.T) {
	request := agentapi.SyncRequest{Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{Fingerprint: "fp"}}}
	if err := attachLiveSignal(&request); err != nil {
		t.Fatal(err)
	}
	first := request.LiveSignal.ID
	if err := attachLiveSignal(&request); err != nil {
		t.Fatal(err)
	}
	if first == request.LiveSignal.ID || request.LiveSignal.Fingerprint != "fp" {
		t.Fatal("live signal must be fresh and identity-bound")
	}
}

func TestStopCollectionClearsPlanAndPendingWorkButBuildsHeartbeat(t *testing.T) {
	r := &Runtime{currentPlan: &agentapi.SyncPlan{HostSampleFrequencyTier: "5s"}, pendingIPReports: []agentapi.IPQualityReportPayload{{IPAddress: "192.0.2.1"}}}
	r.applySyncPlan(context.Background(), &agentapi.SyncResponse{StopCollection: true, Plan: &agentapi.SyncPlan{PendingAction: &agentapi.PendingAction{CommandID: "uptime"}}})
	if r.currentPlan != nil || len(r.pendingIPReports) != 0 || len(r.pendingResults) != 0 {
		t.Fatal("stop response retained pending work")
	}
	req := r.buildSyncRequest(context.Background(), "mi_1", "mas_1.secret", time.Now(), "fp", "batch")
	if len(req.Heartbeats) != 1 || len(req.HostSamples)+len(req.IPQualityReports)+len(req.CommandResults) != 0 || req.SessionID != "mas_1" || req.LiveSignal != nil {
		t.Fatalf("stopped request=%#v", req)
	}
}
