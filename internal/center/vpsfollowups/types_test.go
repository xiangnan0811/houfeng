package vpsfollowups

import (
	"encoding/json"
	"testing"
)

func TestFollowupRequiresReasonAndStructuredEvidence(t *testing.T) {
	for _, input := range []ResolveInput{{Status: "pending", Reason: "reopen"}, {Status: "resolved"}, {Status: "ignored", Reason: " "}} {
		if ValidateResolve(input) == nil {
			t.Errorf("accepted %+v", input)
		}
	}
	if ValidateResolve(ResolveInput{Status: "ignored", Reason: "provider confirmed"}) != nil {
		t.Fatal("valid closure rejected")
	}
	for _, details := range []json.RawMessage{json.RawMessage(`[]`), json.RawMessage(`null`), json.RawMessage(`bad`)} {
		if ValidateCreate(CreateInput{Kind: "migration", Summary: "move", Details: details}) == nil {
			t.Errorf("accepted %s", details)
		}
	}
	if ValidateCreate(CreateInput{Kind: "migration", Summary: "move", Details: json.RawMessage(`{"source":"a","target":"b"}`)}) != nil {
		t.Fatal("valid migration evidence rejected")
	}
}
