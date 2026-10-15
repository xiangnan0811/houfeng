package agentapi

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestParseCollectionMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, raw     string
		paired, valid bool
		request       string
	}{
		{"legacy", `{"collect_request_id":" request_1 "}`, false, false, "request_1"},
		{"legacy bad id", `{"collect_request_id":"bad/id"}`, false, false, ""},
		{"legacy long id", `{"collect_request_id":"` + strings.Repeat("a", 65) + `"}`, false, false, ""},
		{"invalid json", `{`, false, false, ""},
		{"array", `[]`, false, false, ""},
		{"null", `null`, false, false, ""},
		{"id only", `{"collection_id":"one","collect_request_id":"request"}`, true, false, "request"},
		{"expected only", `{"expected_ip_versions":null}`, true, false, ""},
		{"revision only", `{"collection_revision":4}`, true, false, ""},
		{"future revision", `{"collection_revision":6}`, true, false, ""},
		{"fractional future revision", `{"collection_revision":4.5}`, true, false, ""},
		{"valid", `{"collection_revision":4,"collection_id":"round-1","expected_ip_versions":[4,6],"ip_address_family":4,"family_availability":"available","collect_request_id":"request"}`, true, true, "request"},
		{"unknown", `{"collection_revision":4,"collection_id":"round-1","expected_ip_versions":[4,6],"ip_address_family":6,"family_availability":"unknown"}`, true, true, ""},
		{"unavailable", `{"collection_revision":4,"collection_id":"round-1","expected_ip_versions":[4,6],"ip_address_family":6,"family_availability":"unavailable"}`, true, true, ""},
		{"extra family", `{"collection_revision":4,"collection_id":"round-1","expected_ip_versions":[4,6,4],"ip_address_family":4,"family_availability":"available"}`, true, false, ""},
		{"reversed families", `{"collection_revision":4,"collection_id":"round-1","expected_ip_versions":[6,4],"ip_address_family":4,"family_availability":"available"}`, true, false, ""},
		{"invalid family", `{"collection_revision":4,"collection_id":"round-1","expected_ip_versions":[4,6],"ip_address_family":5,"family_availability":"available"}`, true, false, ""},
		{"invalid availability", `{"collection_revision":4,"collection_id":"round-1","expected_ip_versions":[4,6],"ip_address_family":4,"family_availability":"absent"}`, true, false, ""},
		{"whitespace collection id", `{"collection_revision":4,"collection_id":" round-1 ","expected_ip_versions":[4,6],"ip_address_family":4,"family_availability":"available"}`, true, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCollectionMetadata(json.RawMessage(tc.raw))
			if got.Paired != tc.paired || got.Valid != tc.valid || got.CollectRequestID != tc.request {
				t.Fatalf("metadata = %+v", got)
			}
			if got.Valid && (!slices.Equal(got.ExpectedIPVersions, []int{4, 6}) || got.CollectionID != "round-1") {
				t.Fatalf("paired identity = %+v", got)
			}
		})
	}
}
