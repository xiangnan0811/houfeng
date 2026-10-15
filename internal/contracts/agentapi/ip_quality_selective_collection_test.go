package agentapi

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

func TestSelectiveCollectionMetadataTruthTable(t *testing.T) {
	for _, revision := range []int{3, 4, 5, 6} {
		for _, versions := range []string{`null`, `[]`, `[4]`, `[6]`, `[4,6]`, `[6,4]`, `[4,4]`, `[5]`, `[4,6,4]`} {
			for _, family := range []int{4, 6} {
				t.Run(fmt.Sprintf("rev%d_%s_family%d", revision, versions, family), func(t *testing.T) {
					raw := fmt.Sprintf(`{"collection_revision":%d,"collection_id":"round","expected_ip_versions":%s,"ip_address_family":%d,"family_availability":"available"}`, revision, versions, family)
					got := ParseCollectionMetadata(json.RawMessage(raw))
					want := revision == 4 && versions == `[4,6]` || revision == 5 && (versions == `[4,6]` || family == 4 && versions == `[4]` || family == 6 && versions == `[6]`)
					if !got.Paired || got.Valid != want {
						t.Fatalf("metadata=%#v want valid=%v; malformed paired metadata must not become legacy", got, want)
					}
				})
			}
		}
	}
}

func TestCollectionIPVersionsRequiresNegotiatedManualScopeAndOwnsSlice(t *testing.T) {
	for _, tc := range []struct {
		name string
		plan *IPQualityPlan
		want []int
	}{
		{"nil", nil, []int{4, 6}},
		{"periodic", &IPQualityPlan{CollectionRevision: 5, CollectIPVersions: []int{4}}, []int{4, 6}},
		{"legacy", &IPQualityPlan{CollectRequestID: "id", CollectIPVersions: []int{4}}, []int{4, 6}},
		{"future", &IPQualityPlan{CollectionRevision: 6, CollectRequestID: "id", CollectIPVersions: []int{4}}, []int{4, 6}},
		{"invalid", &IPQualityPlan{CollectionRevision: 5, CollectRequestID: "id", CollectIPVersions: []int{6, 4}}, []int{4, 6}},
		{"four", &IPQualityPlan{CollectionRevision: 5, CollectRequestID: "id", CollectIPVersions: []int{4}}, []int{4}},
		{"six", &IPQualityPlan{CollectionRevision: 5, CollectRequestID: "id", CollectIPVersions: []int{6}}, []int{6}},
		{"dual", &IPQualityPlan{CollectionRevision: 5, CollectRequestID: "id", CollectIPVersions: []int{4, 6}}, []int{4, 6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CollectionIPVersions(tc.plan)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("versions=%v want=%v", got, tc.want)
			}
			got[0] = 99
			if again := CollectionIPVersions(tc.plan); !slices.Equal(again, tc.want) {
				t.Fatalf("selection shares storage: %v", again)
			}
		})
	}
}
