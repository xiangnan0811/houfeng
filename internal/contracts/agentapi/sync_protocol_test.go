package agentapi

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func digestTestRequest() SyncRequest {
	return SyncRequest{
		ProtocolRevision: SyncProtocolRevision, SessionID: "session", ProducerID: "f597ed04-aa58-4aa4-9c18-c41f712ecfad", BatchSequence: 1, AllocatedThrough: 1,
		Heartbeats:        []MonitoringInstanceHeartbeat{{ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Fingerprint: "fp", SyncBatchID: "f597ed04-aa58-4aa4-9c18-c41f712ecfad:1"}},
		HostSamples:       []HostSamplePayload{{CPUUsagePct: 12}},
		ProbeObservations: []ProbeObservationPayload{{ResultKind: "success"}},
		IPQualityReports:  []IPQualityReportPayload{{RawJSON: json.RawMessage(`{"z":9007199254740993,"a":{"y":2,"x":1}}`), Product: &IPQualityAgentProduct{}, ProviderResults: []IPQualityProviderResultPayload{{Details: &IPQualityProviderDetails{}}}}},
		CommandResults:    []CommandResult{{ActionID: "action", Stdout: "original"}},
	}
}

func TestCanonicalSyncBatchDigestIgnoresTransportWithoutMutation(t *testing.T) {
	request := digestTestRequest()
	before, _ := json.Marshal(request)
	first, err := CanonicalSyncBatchDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(request)
	if !bytes.Equal(before, after) {
		t.Fatal("digest mutated queue payload")
	}
	request.SyncToken = "rotated-secret"
	request.LiveSignal = &LiveSignal{Sequence: 100, Fingerprint: "fp"}
	request.AllocatedThrough = 200
	request.RetiredThrough = 99
	request.Heartbeats[0].IsBackfilled = true
	request.HostSamples[0].IsBackfilled = true
	request.ProbeObservations[0].IsBackfilled = true
	request.IPQualityReports[0].IsBackfilled = true
	request.IPQualityReports[0].RawJSON = json.RawMessage(` { "a": {"x":1,"y":2}, "z":9007199254740993 } `)
	second, err := CanonicalSyncBatchDigest(request)
	if err != nil || first != second {
		t.Fatalf("transport/key order changed digest: %v", err)
	}
}

func TestCanonicalSyncBatchDigestBindsFactsAndAuthority(t *testing.T) {
	for name, mutate := range map[string]func(*SyncRequest){
		"session":   func(r *SyncRequest) { r.SessionID += "-other" },
		"producer":  func(r *SyncRequest) { r.ProducerID = "b2ff21fe-4902-4602-8d43-d45fd7255a59" },
		"sequence":  func(r *SyncRequest) { r.BatchSequence++ },
		"heartbeat": func(r *SyncRequest) { r.Heartbeats[0].Fingerprint = "other" },
		"sample":    func(r *SyncRequest) { r.HostSamples[0].CPUUsagePct++ },
		"probe":     func(r *SyncRequest) { r.ProbeObservations[0].ResultKind = "failure" },
		"command":   func(r *SyncRequest) { r.CommandResults[0].Stdout = "changed" },
		"large raw integer": func(r *SyncRequest) {
			r.IPQualityReports[0].RawJSON = json.RawMessage(`{"z":9007199254740992,"a":{"x":1,"y":2}}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := digestTestRequest()
			before, err := CanonicalSyncBatchDigest(r)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&r)
			after, err := CanonicalSyncBatchDigest(r)
			if err != nil || before == after {
				t.Fatalf("changed immutable payload not distinguished: %v", err)
			}
		})
	}
}

func TestCanonicalSyncBatchDigestNormalizesEmptyCollections(t *testing.T) {
	r := digestTestRequest()
	r.Heartbeats, r.HostSamples, r.ProbeObservations, r.CommandResults = nil, nil, nil, nil
	first, err := CanonicalSyncBatchDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Heartbeats = []MonitoringInstanceHeartbeat{}
	r.HostSamples = []HostSamplePayload{}
	r.ProbeObservations = []ProbeObservationPayload{}
	r.CommandResults = []CommandResult{}
	r.IPQualityReports[0].Product.PTRNames = []string{}
	r.IPQualityReports[0].Product.Mail = []IPQualityMailResult{}
	r.IPQualityReports[0].ProviderResults[0].Details.Scores = []IPQualityScore{}
	r.IPQualityReports[0].ProviderResults[0].Details.FieldStates = map[string]string{}
	second, err := CanonicalSyncBatchDigest(r)
	if err != nil || first != second {
		t.Fatalf("empty collections changed digest: %v", err)
	}
	r.IPQualityReports[0].RawJSON = json.RawMessage(`{broken`)
	if _, err := CanonicalSyncBatchDigest(r); err == nil {
		t.Fatal("invalid raw JSON accepted")
	}
}
