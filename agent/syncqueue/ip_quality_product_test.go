package syncqueue_test

import (
	"context"
	"path/filepath"
	"testing"

	"houfeng/agent/syncqueue"
	"houfeng/internal/contracts/agentapi"
)

func TestFileStoreProductSnapshotCannotBeMutatedByCaller(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "queue.json")
	store := openStore(t, path, syncqueue.Options{})
	value := 0.0183
	request := syncRequest("product-snapshot", false)
	report := &request.IPQualityReports[0]
	report.Product = &agentapi.IPQualityAgentProduct{Revision: 1, PTRNames: []string{"old.example"}, Mail: []agentapi.IPQualityMailResult{{Status: "reachable"}}}
	report.ProviderResults = []agentapi.IPQualityProviderResultPayload{{Provider: "ipapi.is", Details: &agentapi.IPQualityProviderDetails{FieldStates: map[string]string{"is_vpn": "unknown"}, Scores: []agentapi.IPQualityScore{{Value: &value}}}}}
	report.ServiceUnlocks = []agentapi.IPQualityServiceUnlockPayload{{Service: "chatgpt", Checks: []agentapi.IPQualityServiceCheck{{ID: "web", Status: "available"}}}}
	if _, err := store.Enqueue(ctx, request); err != nil {
		t.Fatal(err)
	}
	report.Product.PTRNames[0] = "changed.example"
	report.Product.Mail[0].Status = "timeout"
	report.ProviderResults[0].Details.FieldStates["is_vpn"] = "known"
	*report.ProviderResults[0].Details.Scores[0].Value = 99
	report.ServiceUnlocks[0].Checks[0].Status = "restricted"
	entries, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := entries[0].Request.IPQualityReports[0]
	if got.Product.PTRNames[0] != "old.example" || got.Product.Mail[0].Status != "reachable" || got.ProviderResults[0].Details.FieldStates["is_vpn"] != "unknown" || *got.ProviderResults[0].Details.Scores[0].Value != 0.0183 || got.ServiceUnlocks[0].Checks[0].Status != "available" {
		t.Fatalf("caller mutated queued snapshot: %+v", got)
	}
	got.Product.PTRNames[0] = "list-mutated.example"
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(t, path, syncqueue.Options{}).List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reopened[0].Request.IPQualityReports[0].Product.PTRNames[0] != "old.example" {
		t.Fatal("durable snapshot changed")
	}
}
