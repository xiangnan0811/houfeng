package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestComparisonResultKindRejectsLiveCaptureSurfaces(t *testing.T) {
	t.Parallel()

	kind := mustComparisonResultKind(t)
	actor := testActor(t)
	selection := Selection{Key: ComparisonResultV1Key(), SourceType: "target", SourceID: "tg_0123456789abcdef", RequestedWindow: testWindow()}
	if err := kind.ValidateSelection(context.Background(), actor, selection); !errors.Is(err, ErrInvalidCanonicalPayload) {
		t.Fatalf("ValidateSelection() error = %v, want ErrInvalidCanonicalPayload", err)
	}
	if _, err := kind.PreviewCapture(context.Background(), actor, selection); !errors.Is(err, ErrInvalidCanonicalPayload) {
		t.Fatalf("PreviewCapture() error = %v, want ErrInvalidCanonicalPayload", err)
	}
	if _, err := kind.Authorize(context.Background(), actor, selection); !errors.Is(err, ErrInvalidCanonicalPayload) {
		t.Fatalf("Authorize() error = %v, want ErrInvalidCanonicalPayload", err)
	}
	if _, err := kind.Capture(context.Background(), actor, Intent{Key: ComparisonResultV1Key(), ID: "evi_0123456789abcdef01234567"}); !errors.Is(err, ErrInvalidCanonicalPayload) {
		t.Fatalf("Capture() error = %v, want ErrInvalidCanonicalPayload", err)
	}
}

func TestComparisonResultSummarizeCompareAndExportOmitHumanConclusion(t *testing.T) {
	t.Parallel()

	kind := mustComparisonResultKind(t)
	snapshot := mustComparisonResultSnapshot(t, kind, comparisonResultTestPayload("evs_leftcopy", "evs_rightcopy"))

	summary := kind.Summarize(snapshot)
	if summary.Key != ComparisonResultV1Key() || summary.RendererVersion != kind.Descriptor().Conformance.RendererVersion {
		t.Fatalf("Summarize() identity = %#v", summary)
	}
	readModel, ok := summary.ReadModel["version"].(string)
	if !ok || readModel != "comparison_result_read_model/v1" {
		t.Fatalf("Summarize() read model version = %#v", summary.ReadModel)
	}
	assertComparisonResultHasNoHumanConclusion(t, summary.ReadModel)
	baseline, _ := summary.ReadModel["baseline_index"].(float64)
	if baseline != 0 || summary.ReadModel["alignment"] != string(CoverageActual) {
		t.Fatalf("Summarize() dropped conditions: %#v", summary.ReadModel)
	}
	items, _ := summary.ReadModel["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("Summarize() items = %#v", summary.ReadModel["items"])
	}
	left, _ := items[0].(map[string]any)
	if left["original_snapshot_id"] != "evs_left" || left["copied_snapshot_id"] != "evs_leftcopy" ||
		left["record_type"] != "incident" || left["business_status"] != "open" ||
		left["status_group"] != "active" || left["impact_level"] != "high" ||
		left["occurred_at"] != "2026-08-01T00:00:00Z" {
		t.Fatalf("Summarize() item metadata = %#v", left)
	}
	if _, ok := summary.ReadModel["warnings"]; !ok {
		t.Fatal("Summarize() dropped warnings")
	}
	if _, ok := summary.ReadModel["system_differences"]; !ok {
		t.Fatal("Summarize() dropped system differences")
	}
	if strings.Contains(strings.ToLower(summary.Title+" "+summary.SearchText), "conclusion") {
		t.Fatalf("Summarize() leaked conclusion text: title=%q search=%q", summary.Title, summary.SearchText)
	}

	self := kind.Compare(snapshot, snapshot, Alignment{Mode: AlignmentExact})
	if !self.Compatible || self.Key != ComparisonResultV1Key() {
		t.Fatalf("Compare(self, exact) = %#v", self)
	}
	equal, _ := self.Values["equal"].(bool)
	if !equal {
		t.Fatalf("Compare(self, exact) values = %#v, want equal", self.Values)
	}

	other := mustComparisonResultSnapshot(t, kind, comparisonResultTestPayload("evs_otherleft", "evs_otherright"))
	diff := kind.Compare(snapshot, other, Alignment{Mode: AlignmentExact})
	if !diff.Compatible {
		t.Fatalf("Compare(distinct, exact) = %#v, want compatible schema", diff)
	}
	if equal, _ = diff.Values["equal"].(bool); equal {
		t.Fatalf("Compare(distinct, exact) values = %#v, want unequal", diff.Values)
	}

	if got := kind.Compare(snapshot, snapshot, Alignment{Mode: AlignmentMode("window")}); got.Compatible {
		t.Fatalf("Compare(non-exact) = %#v, want incompatible", got)
	}

	exported := kind.Export(snapshot, ExportModeSafe)
	if exported.Key != ComparisonResultV1Key() || exported.MediaType != "application/json" || len(exported.Bytes) == 0 {
		t.Fatalf("Export() = %#v", exported)
	}
	if !bytes.Equal(exported.Bytes, snapshot.Bytes()) {
		t.Fatal("Export() bytes must equal the canonical snapshot")
	}
	assertComparisonResultHasNoHumanConclusion(t, exported.Bytes)
}

func TestComparisonResultPayloadRejectsConclusionAndForbiddenCorpus(t *testing.T) {
	t.Parallel()

	kind := mustComparisonResultKind(t)
	descriptor := kind.Descriptor()
	envelope := testEnvelope(t, ComparisonResultV1Key())
	envelope.CalculationVersion = ComparisonCalculationVersion
	envelope.ProducerVersion = comparisonResultProducerVersion
	envelope.Units = UnitsSemantics{Status: UnitsNotApplicable, Reason: "comparison result metadata"}
	envelope.ActualPrecision = DurationSemantics{Applicable: false, Reason: "comparison result metadata"}
	envelope.BucketWidth = DurationSemantics{Applicable: false, Reason: "comparison result metadata"}

	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr error
	}{
		{name: "conclusion", mutate: func(payload map[string]any) { payload["conclusion"] = "人工结论" }, wantErr: ErrForbiddenField},
		{name: "markdown", mutate: func(payload map[string]any) { payload["markdown"] = "# 结论" }, wantErr: ErrForbiddenField},
		{name: "body markdown", mutate: func(payload map[string]any) { payload["body_markdown"] = "x" }, wantErr: ErrForbiddenField},
		{name: "token", mutate: func(payload map[string]any) { payload["token"] = "cmp1.secret" }, wantErr: ErrForbiddenField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := comparisonResultTestPayload("evs_leftcopy", "evs_rightcopy")
			tt.mutate(payload)
			if _, _, err := NewCanonicalSnapshot(descriptor, envelope, payload, RedactionNormalOnly); !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewCanonicalSnapshot(%s) error = %v, want %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestComparisonResultKindIsRegisteredKnownKey(t *testing.T) {
	t.Parallel()

	found := false
	for _, key := range KnownKindKeys() {
		if key == ComparisonResultV1Key() {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("KnownKindKeys() missing comparison.result/v1")
	}
}

func mustComparisonResultKind(t *testing.T) *ComparisonResultKind {
	t.Helper()
	kind, err := NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	return kind
}

func mustComparisonResultSnapshot(t *testing.T, kind *ComparisonResultKind, payload map[string]any) CanonicalSnapshot {
	t.Helper()
	envelope := testEnvelope(t, ComparisonResultV1Key())
	envelope.CalculationVersion = ComparisonCalculationVersion
	envelope.ProducerVersion = comparisonResultProducerVersion
	envelope.Units = UnitsSemantics{Status: UnitsNotApplicable, Reason: "comparison result metadata"}
	envelope.ActualPrecision = DurationSemantics{Applicable: false, Reason: "comparison result metadata"}
	envelope.BucketWidth = DurationSemantics{Applicable: false, Reason: "comparison result metadata"}
	snapshot, _, err := NewCanonicalSnapshot(kind.Descriptor(), envelope, payload, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	return snapshot
}

func comparisonResultTestPayload(leftCopy, rightCopy string) map[string]any {
	return map[string]any{
		"version":             "comparison_result/v1",
		"baseline_index":      0,
		"alignment":           string(CoverageActual),
		"requested_from":      "2026-08-10T11:00:00Z",
		"requested_to":        "2026-08-10T12:00:00Z",
		"tolerance_seconds":   60,
		"digest":              strings.Repeat("ab", 32),
		"registry_version":    "evidence-kinds/v1",
		"calculation_version": ComparisonCalculationVersion,
		"items": []any{
			map[string]any{
				"original_snapshot_id": "evs_left",
				"copied_snapshot_id":   leftCopy,
				"hash":                 strings.Repeat("11", 32),
				"kind":                 CommandAuditV1Key().String(),
				"revision_context":     string(RevisionContextBound),
				"record_type":          "incident",
				"business_status":      "open",
				"status_group":         "active",
				"impact_level":         "high",
				"occurred_at":          "2026-08-01T00:00:00Z",
			},
			map[string]any{
				"original_snapshot_id": "evs_right",
				"copied_snapshot_id":   rightCopy,
				"hash":                 strings.Repeat("22", 32),
				"kind":                 CommandAuditV1Key().String(),
				"revision_context":     string(RevisionContextNotApplicable),
			},
		},
		"warnings": []any{
			map[string]any{"item_index": 0, "kind": CommandAuditV1Key().String(), "reason": string(ReasonCoveragePartial)},
		},
		"system_differences": []any{
			map[string]any{
				"item_index": 1,
				"kind":       CommandAuditV1Key().String(),
				"compatible": true,
				"reason":     "exact compatible evidence semantics",
				"left_hash":  strings.Repeat("11", 32),
				"right_hash": strings.Repeat("22", 32),
				"equal":      false,
			},
		},
		"available_kinds": []any{CommandAuditV1Key().String()},
	}
}

func assertComparisonResultHasNoHumanConclusion(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal conclusion probe: %v", err)
	}
	lower := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"conclusion", "body_markdown", "markdown", "cmp1.", "人工结论"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("comparison result leaked %q: %s", forbidden, encoded)
		}
	}
}
