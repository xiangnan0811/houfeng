package recordclientcleanup

import (
	"errors"
	"strings"
	"testing"
)

func TestCleanupRecordIdentityDomains(t *testing.T) {
	ids := []string{"rec_legacy", "rec2_70000000000000000000000000000000", "rec2_80000000000000000000000000000000"}
	for _, recordID := range ids {
		t.Run(recordID, func(t *testing.T) {
			event := Event{Version: 1, Cursor: "1", OperationID: "rpo_cleanup", RecordID: recordID, CleanupEpoch: "2", Scopes: []string{"markdown_workspace"}}
			if err := event.Validate(); err != nil {
				t.Fatalf("Event.Validate() error = %v", err)
			}
			if err := event.Acknowledgement().Validate(); err != nil {
				t.Fatalf("Acknowledgement.Validate() error = %v", err)
			}
			request := ReconcileRequest{BrowserID: "browser_a", RecordIDs: []string{recordID}}
			if err := request.Validate(); err != nil {
				t.Fatalf("ReconcileRequest.Validate() error = %v", err)
			}
		})
	}
	if err := (ReconcileRequest{BrowserID: "browser_a", RecordIDs: ids}).Validate(); err != nil {
		t.Fatalf("mixed ReconcileRequest.Validate() error = %v", err)
	}

	valid := Event{Version: 1, Cursor: "1", OperationID: "rpo_cleanup", RecordID: ids[1], CleanupEpoch: "2", Scopes: []string{"markdown_workspace"}}
	for _, mutate := range []func(*Event){
		func(event *Event) { event.RecordID = "rec2_7000000000000000000000000000000g" },
		func(event *Event) { event.RecordID = "rrv_revision" },
		func(event *Event) { event.OperationID = ids[1] },
	} {
		candidate := valid
		mutate(&candidate)
		if err := candidate.Validate(); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid Event.Validate() error = %v", err)
		}
	}
	if err := (ReconcileRequest{BrowserID: "browser_a", RecordIDs: []string{ids[0], "rrv_revision"}}).Validate(); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid ReconcileRequest.Validate() error = %v", err)
	}
}

func TestReconcileAllocationReferences(t *testing.T) {
	const recordID = "rec2_0123456789abcdef0123456789abcdef"
	const otherRecord = "rec2_8123456789abcdef0123456789abcdef"
	const allocationID = "rqa_0123456789abcdef0123456789abcdef"
	reference := AllocationReference{RecordID: recordID, AllocationID: allocationID}
	tests := []struct {
		name    string
		request ReconcileRequest
		valid   bool
	}{
		{"one pending reference", ReconcileRequest{BrowserID: "browser_a", AllocationReferences: []AllocationReference{reference}}, true},
		{"identical reference replay", ReconcileRequest{BrowserID: "browser_a", AllocationReferences: []AllocationReference{reference, reference}}, true},
		{"duplicate ordinary id", ReconcileRequest{BrowserID: "browser_a", RecordIDs: []string{"rec_old", "rec_old"}}, true},
		{"both lists contain record", ReconcileRequest{BrowserID: "browser_a", RecordIDs: []string{recordID}, AllocationReferences: []AllocationReference{reference}}, false},
		{"same record different allocation", ReconcileRequest{BrowserID: "browser_a", AllocationReferences: []AllocationReference{reference, {RecordID: recordID, AllocationID: "rqa_1123456789abcdef0123456789abcdef"}}}, false},
		{"same allocation different record", ReconcileRequest{BrowserID: "browser_a", AllocationReferences: []AllocationReference{reference, {RecordID: otherRecord, AllocationID: allocationID}}}, false},
		{"legacy record has no allocation", ReconcileRequest{BrowserID: "browser_a", AllocationReferences: []AllocationReference{{RecordID: "rec_old", AllocationID: allocationID}}}, false},
		{"invalid record", ReconcileRequest{BrowserID: "browser_a", AllocationReferences: []AllocationReference{{RecordID: "rec2_0123456789abcdef0123456789abcdeG", AllocationID: allocationID}}}, false},
		{"invalid allocation", ReconcileRequest{BrowserID: "browser_a", AllocationReferences: []AllocationReference{{RecordID: recordID, AllocationID: "rqa_" + strings.Repeat("A", 32)}}}, false},
		{"truncated allocation", ReconcileRequest{BrowserID: "browser_a", AllocationReferences: []AllocationReference{{RecordID: recordID, AllocationID: "rqa_0123456789abcdef"}}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.request.Validate()
			if test.valid && err != nil {
				t.Fatalf("Validate() = %v, want success", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Validate() = %v, want invalid request", err)
			}
		})
	}

	atLimit := ReconcileRequest{BrowserID: "browser_a", RecordIDs: make([]string, MaxReconcileRecords-1), AllocationReferences: []AllocationReference{reference}}
	for index := range atLimit.RecordIDs {
		atLimit.RecordIDs[index] = "rec_old"
	}
	if err := atLimit.Validate(); err != nil {
		t.Fatalf("combined limit rejected: %v", err)
	}
	atLimit.RecordIDs = append(atLimit.RecordIDs, "rec_old")
	if err := atLimit.Validate(); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("combined overflow = %v, want invalid request", err)
	}
}
