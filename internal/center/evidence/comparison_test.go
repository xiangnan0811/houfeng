package evidence

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestEvaluateComparisonRejectsOutOfRangeItemCountAndBaseline(t *testing.T) {
	registry := comparisonTestRegistry(t)
	window := testWindow()
	item := comparisonTestItem(t, "evs_a", CommandAuditV1Key(), RevisionContextNotApplicable, nil)
	tests := []struct {
		name  string
		input ComparisonEvaluateInput
		want  error
	}{
		{
			name:  "one item",
			input: ComparisonEvaluateInput{Items: []ComparisonItemInput{item}, BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: window},
			want:  ErrInvalidComparisonSelection,
		},
		{
			name: "seven items",
			input: ComparisonEvaluateInput{
				Items:           []ComparisonItemInput{item, item, item, item, item, item, item},
				BaselineIndex:   0,
				Alignment:       CoverageActual,
				RequestedWindow: window,
			},
			want: ErrInvalidComparisonSelection,
		},
		{
			name: "baseline out of range",
			input: ComparisonEvaluateInput{
				Items:           []ComparisonItemInput{item, comparisonTestItem(t, "evs_b", CommandAuditV1Key(), RevisionContextNotApplicable, nil)},
				BaselineIndex:   2,
				Alignment:       CoverageActual,
				RequestedWindow: window,
			},
			want: ErrInvalidComparisonSelection,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := EvaluateComparison(registry, tt.input); !errors.Is(err, tt.want) {
				t.Fatalf("EvaluateComparison() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestEvaluateComparisonKeepsImmutableRevisionMetadataAndSnapshotOnlyNull(t *testing.T) {
	occurred := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	revision := &RevisionMetadataSnapshot{
		RecordType:     "incident",
		BusinessStatus: "open",
		StatusGroup:    "active",
		ImpactLevel:    "high",
		OccurredAt:     occurred,
		HasOccurredAt:  true,
	}
	result, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items: []ComparisonItemInput{
			comparisonTestItem(t, "evs_rev", CommandAuditV1Key(), RevisionContextBound, revision),
			comparisonTestItem(t, "evs_snap", CommandAuditV1Key(), RevisionContextNotApplicable, nil),
		},
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: testWindow(),
	})
	if err != nil {
		t.Fatalf("EvaluateComparison() error = %v", err)
	}
	if result.Items[0].RevisionContext != RevisionContextBound || result.Items[0].Revision == nil {
		t.Fatalf("revision-bound item = %#v", result.Items[0])
	}
	if result.Items[0].Revision.RecordType != "incident" ||
		result.Items[0].Revision.BusinessStatus != "open" ||
		result.Items[0].Revision.StatusGroup != "active" ||
		result.Items[0].Revision.ImpactLevel != "high" ||
		!result.Items[0].Revision.HasOccurredAt ||
		!result.Items[0].Revision.OccurredAt.Equal(occurred) {
		t.Fatalf("revision metadata = %#v", result.Items[0].Revision)
	}
	if result.Items[1].RevisionContext != RevisionContextNotApplicable || result.Items[1].Revision != nil {
		t.Fatalf("snapshot-only item leaked revision metadata: %#v", result.Items[1])
	}
}

func TestEvaluateComparisonCommonOverlapIsUnsupportedForCurrentKinds(t *testing.T) {
	result, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items: []ComparisonItemInput{
			comparisonTestItem(t, "evs_a", MonitoringHostV1Key(), RevisionContextNotApplicable, nil),
			comparisonTestItem(t, "evs_b", MonitoringHostV1Key(), RevisionContextNotApplicable, nil),
		},
		BaselineIndex:   0,
		Alignment:       CoverageCommonOverlap,
		RequestedWindow: testWindow(),
		Detail:          &ComparisonDetail{Kind: MonitoringHostV1Key(), Metric: "cpu_pct"},
	})
	if err != nil {
		t.Fatalf("EvaluateComparison() error = %v", err)
	}
	if len(result.Series) != 0 {
		t.Fatalf("Series = %#v, want empty under common_overlap", result.Series)
	}
	if !hasComparisonReason(result.Review, ReasonCommonOverlapUnsupported) {
		t.Fatalf("Review = %#v, want %q", result.Review, ReasonCommonOverlapUnsupported)
	}
}

func TestEvaluateComparisonWrapsPairwiseKindCompareAndRejectsCrossKindScore(t *testing.T) {
	left := comparisonTestItem(t, "evs_left", CommandAuditV1Key(), RevisionContextNotApplicable, nil)
	right := comparisonTestItem(t, "evs_right", CommandAuditV1Key(), RevisionContextNotApplicable, nil)
	result, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items:           []ComparisonItemInput{left, right},
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: testWindow(),
		Detail:          &ComparisonDetail{Kind: CommandAuditV1Key()},
	})
	if err != nil {
		t.Fatalf("EvaluateComparison() error = %v", err)
	}
	if len(result.Pairwise) != 1 || !result.Pairwise[0].Compatible || result.Pairwise[0].Values["equal"] != true {
		t.Fatalf("Pairwise = %#v, want wrapped Kind.Compare DTO", result.Pairwise)
	}
	if _, ok := result.Pairwise[0].Values["best_vps_score"]; ok {
		t.Fatalf("Pairwise invented cross-kind score: %#v", result.Pairwise[0].Values)
	}
	if len(result.Series) != 0 {
		t.Fatalf("Series = %#v, want none for non-timeseries kind", result.Series)
	}
}

func TestEvaluateComparisonDigestIgnoresMapIterationAndChangesWithItemOrder(t *testing.T) {
	window := testWindow()
	first := comparisonTestItem(t, "evs_a", CommandAuditV1Key(), RevisionContextNotApplicable, nil)
	second := comparisonTestItem(t, "evs_b", CommandAuditV1Key(), RevisionContextNotApplicable, nil)
	left, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items: []ComparisonItemInput{first, second}, BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: window,
	})
	if err != nil {
		t.Fatalf("EvaluateComparison(left) error = %v", err)
	}
	right, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items: []ComparisonItemInput{first, second}, BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: window,
	})
	if err != nil {
		t.Fatalf("EvaluateComparison(right) error = %v", err)
	}
	if left.Digest != right.Digest {
		t.Fatalf("digest unstable: %x vs %x", left.Digest, right.Digest)
	}
	swapped, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items: []ComparisonItemInput{second, first}, BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: window,
	})
	if err != nil {
		t.Fatalf("EvaluateComparison(swapped) error = %v", err)
	}
	if swapped.Digest == left.Digest {
		t.Fatal("ordered item permutation did not change digest")
	}
}

func TestEvaluateComparisonDigestChangesWithBaselineAlignmentAndRevisionMetadata(t *testing.T) {
	window := testWindow()
	occurred := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	revision := &RevisionMetadataSnapshot{
		RecordType: "incident", BusinessStatus: "open", StatusGroup: "active",
		ImpactLevel: "high", OccurredAt: occurred, HasOccurredAt: true,
	}
	first := comparisonTestItem(t, "evs_a", CommandAuditV1Key(), RevisionContextBound, revision)
	second := comparisonTestItem(t, "evs_b", CommandAuditV1Key(), RevisionContextNotApplicable, nil)
	base, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items: []ComparisonItemInput{first, second}, BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: window,
	})
	if err != nil {
		t.Fatalf("EvaluateComparison(base) error = %v", err)
	}
	shifted, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items: []ComparisonItemInput{first, second}, BaselineIndex: 1, Alignment: CoverageActual, RequestedWindow: window,
	})
	if err != nil {
		t.Fatalf("EvaluateComparison(baseline) error = %v", err)
	}
	if shifted.Digest == base.Digest {
		t.Fatal("baseline change did not change digest")
	}
	aligned, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items: []ComparisonItemInput{first, second}, BaselineIndex: 0, Alignment: CoverageCommonOverlap, RequestedWindow: window,
	})
	if err != nil {
		t.Fatalf("EvaluateComparison(alignment) error = %v", err)
	}
	if aligned.Digest == base.Digest {
		t.Fatal("alignment change did not change digest")
	}
	changed := *revision
	changed.ImpactLevel = "critical"
	mutatedFirst := comparisonTestItem(t, "evs_a", CommandAuditV1Key(), RevisionContextBound, &changed)
	mutated, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items: []ComparisonItemInput{mutatedFirst, second}, BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: window,
	})
	if err != nil {
		t.Fatalf("EvaluateComparison(metadata) error = %v", err)
	}
	if mutated.Digest == base.Digest {
		t.Fatal("immutable revision metadata change did not change digest")
	}
}

func TestEvaluateComparisonMetadataOnlyHasNoNumericSeries(t *testing.T) {
	result, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items: []ComparisonItemInput{
			{
				SnapshotID:      "evs_meta",
				Kind:            CommandAuditV1Key(),
				RevisionContext: RevisionContextBound,
				Revision:        &RevisionMetadataSnapshot{RecordType: "note", ImpactLevel: "low"},
				Reasons:         []ComparisonReason{ReasonMetadataOnly},
			},
			comparisonTestItem(t, "evs_other", CommandAuditV1Key(), RevisionContextNotApplicable, nil),
		},
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: testWindow(),
	})
	if err != nil {
		t.Fatalf("EvaluateComparison() error = %v", err)
	}
	if !hasComparisonReason(result.Review, ReasonMetadataOnly) {
		t.Fatalf("Review = %#v, want metadata_only", result.Review)
	}
	if len(result.Series) != 0 {
		t.Fatalf("Series = %#v, want none for metadata_only", result.Series)
	}
}

func comparisonTestRegistry(t *testing.T) Registry {
	t.Helper()
	keys := []KindKey{CommandAuditV1Key(), MonitoringHostV1Key(), MonitoringProbeV2Key()}
	kinds := make([]Kind, 0, len(keys))
	for _, key := range keys {
		kinds = append(kinds, &kindStub{
			descriptor: testDescriptor(t, key),
			comparison: &Comparison{Key: key, Compatible: true, Reason: "compatible_test", Values: map[string]any{"equal": true}},
		})
	}
	registry, err := NewRegistry(kinds)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

func comparisonTestItem(
	t *testing.T,
	snapshotID string,
	key KindKey,
	context RevisionContext,
	revision *RevisionMetadataSnapshot,
) ComparisonItemInput {
	t.Helper()
	snapshot, _, err := NewCanonicalSnapshot(
		testDescriptor(t, key),
		testEnvelope(t, key),
		map[string]any{"metric_name": "latency_ms", "status": "ok"},
		RedactionNormalOnly,
	)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	return ComparisonItemInput{
		SnapshotID:      snapshotID,
		Hash:            snapshot.Hash(),
		Kind:            key,
		RevisionContext: context,
		Revision:        cloneRevisionMetadata(revision),
		Snapshot:        snapshot,
	}
}

func hasComparisonReason(findings []ComparabilityFinding, reason ComparisonReason) bool {
	for _, finding := range findings {
		if finding.Reason == reason {
			return true
		}
	}
	return false
}

func cloneRevisionMetadata(value *RevisionMetadataSnapshot) *RevisionMetadataSnapshot {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func TestComparisonDigestCanonicalJSONIsStable(t *testing.T) {
	payload := map[string]any{"b": 2, "a": 1}
	first, err := comparisonDigest(payload)
	if err != nil {
		t.Fatalf("comparisonDigest() error = %v", err)
	}
	second, err := comparisonDigest(payload)
	if err != nil {
		t.Fatalf("comparisonDigest() second error = %v", err)
	}
	if first != second {
		t.Fatalf("digest unstable: %x vs %x", first, second)
	}
	var encoded json.RawMessage
	_ = encoded
	if first == sha256.Sum256(nil) {
		t.Fatal("digest is empty")
	}
	if !reflect.DeepEqual(payload, map[string]any{"b": 2, "a": 1}) {
		t.Fatalf("digest mutated input: %#v", payload)
	}
}
