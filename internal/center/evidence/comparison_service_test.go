package evidence

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFixedComparisonRejectsSnapshotRevisionXORAndRange(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	snapshotA := "evs_fixeda"
	snapshotB := "evs_fixedb"
	source := &comparisonSelectionSourceStub{snapshots: map[string]ComparisonLoadedSnapshot{
		snapshotA: loadedComparisonSnapshot(t, snapshotA, CommandAuditV1Key(), false),
		snapshotB: loadedComparisonSnapshot(t, snapshotB, CommandAuditV1Key(), false),
	}}
	tests := []struct {
		name    string
		request ComparisonEvaluateRequest
	}{
		{
			name: "both snapshot and revision",
			request: ComparisonEvaluateRequest{
				Actor: actor,
				Items: []ComparisonFixedItem{
					{SnapshotID: &snapshotA, Revision: &ComparisonFixedRevision{RecordID: "rec_fixeda", RevisionID: "rrv_fixeda"}},
					{SnapshotID: &snapshotB},
				},
				BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
			},
		},
		{
			name: "neither snapshot nor revision",
			request: ComparisonEvaluateRequest{
				Actor: actor,
				Items: []ComparisonFixedItem{
					{},
					{SnapshotID: &snapshotB},
				},
				BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
			},
		},
		{
			name: "one item",
			request: ComparisonEvaluateRequest{
				Actor: actor, Items: []ComparisonFixedItem{{SnapshotID: &snapshotA}},
				BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
			},
		},
		{
			name: "baseline out of range",
			request: ComparisonEvaluateRequest{
				Actor:         actor,
				Items:         []ComparisonFixedItem{{SnapshotID: &snapshotA}, {SnapshotID: &snapshotB}},
				BaselineIndex: 2, Alignment: CoverageActual, RequestedWindow: testWindow(),
			},
		},
		{
			name: "invalid snapshot grammar",
			request: ComparisonEvaluateRequest{
				Actor: actor,
				Items: []ComparisonFixedItem{
					{SnapshotID: comparisonStringPtr("evs_near_a")},
					{SnapshotID: &snapshotB},
				},
				BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, nil, tt.request)
			if !errors.Is(err, ErrInvalidComparisonSelection) {
				t.Fatalf("ResolveFixedComparison() error = %v, want %v", err, ErrInvalidComparisonSelection)
			}
			if source.snapshotCalls != 0 || source.revisionCalls != 0 {
				t.Fatalf("selection loaded after invalid XOR/range: snapshots=%d revisions=%d", source.snapshotCalls, source.revisionCalls)
			}
		})
	}
}

func TestFixedComparisonKeepsImpactLevelAndSnapshotOnlyNotApplicable(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	occurred := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	snapshotA := loadedComparisonSnapshot(t, "evs_fixeda", CommandAuditV1Key(), false)
	snapshotB := loadedComparisonSnapshot(t, "evs_fixedb", CommandAuditV1Key(), false)
	source := &comparisonSelectionSourceStub{
		snapshots: map[string]ComparisonLoadedSnapshot{
			snapshotA.SnapshotID: snapshotA,
			snapshotB.SnapshotID: snapshotB,
		},
		revisions: map[ComparisonRevisionKey]ComparisonLoadedRevision{
			{RecordID: "rec_fixeda", RevisionID: "rrv_fixeda"}: {
				RecordID: "rec_fixeda", RevisionID: "rrv_fixeda",
				Metadata: RevisionMetadataSnapshot{
					RecordType: "incident", BusinessStatus: "open", StatusGroup: "active",
					ImpactLevel: "high", OccurredAt: occurred, HasOccurredAt: true,
				},
				RecordScope: snapshotA.RecordScope,
				SnapshotIDs: []string{snapshotA.SnapshotID},
			},
		},
	}
	result, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, nil, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{Revision: &ComparisonFixedRevision{RecordID: "rec_fixeda", RevisionID: "rrv_fixeda"}},
			{SnapshotID: comparisonStringPtr(snapshotB.SnapshotID)},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
	})
	if err != nil {
		t.Fatalf("ResolveFixedComparison() error = %v", err)
	}
	if result.Items[0].RevisionContext != RevisionContextBound || result.Items[0].Revision == nil {
		t.Fatalf("revision-bound item = %#v", result.Items[0])
	}
	if result.Items[0].RecordID != "rec_fixeda" || result.Items[0].RevisionID != "rrv_fixeda" {
		t.Fatalf("revision identities = %#v", result.Items[0])
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

func TestFixedComparisonIncompleteWhenSameKindUnchosen(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	left := loadedComparisonSnapshot(t, "evs_fixeda", CommandAuditV1Key(), false)
	right := loadedComparisonSnapshot(t, "evs_fixedb", CommandAuditV1Key(), false)
	other := loadedComparisonSnapshot(t, "evs_fixedc", CommandAuditV1Key(), false)
	source := &comparisonSelectionSourceStub{
		snapshots: map[string]ComparisonLoadedSnapshot{
			left.SnapshotID: left, right.SnapshotID: right, other.SnapshotID: other,
		},
		revisions: map[ComparisonRevisionKey]ComparisonLoadedRevision{
			{RecordID: "rec_fixeda", RevisionID: "rrv_fixeda"}: {
				RecordID: "rec_fixeda", RevisionID: "rrv_fixeda",
				Metadata:    RevisionMetadataSnapshot{RecordType: "note", ImpactLevel: "low"},
				RecordScope: left.RecordScope,
				SnapshotIDs: []string{left.SnapshotID, right.SnapshotID},
			},
		},
	}
	_, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, nil, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{Revision: &ComparisonFixedRevision{RecordID: "rec_fixeda", RevisionID: "rrv_fixeda"}},
			{SnapshotID: comparisonStringPtr(other.SnapshotID)},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
	})
	if !errors.Is(err, ErrComparisonSelectionIncomplete) {
		t.Fatalf("ResolveFixedComparison() error = %v, want %v", err, ErrComparisonSelectionIncomplete)
	}

	result, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, nil, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{Revision: &ComparisonFixedRevision{
				RecordID: "rec_fixeda", RevisionID: "rrv_fixeda", ChosenSnapshotIDs: []string{left.SnapshotID},
			}},
			{SnapshotID: comparisonStringPtr(other.SnapshotID)},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
	})
	if err != nil {
		t.Fatalf("ResolveFixedComparison(chosen) error = %v", err)
	}
	if result.Items[0].SnapshotID != left.SnapshotID {
		t.Fatalf("chosen snapshot = %q, want %q", result.Items[0].SnapshotID, left.SnapshotID)
	}
}

func TestFixedComparisonPinnedRevisionIgnoresUnreferencedSnapshot(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	pinned := loadedComparisonSnapshot(t, "evs_fixeda", CommandAuditV1Key(), false)
	newer := loadedComparisonSnapshot(t, "evs_fixedc", CommandAuditV1Key(), false)
	other := loadedComparisonSnapshot(t, "evs_fixedb", CommandAuditV1Key(), false)
	source := &comparisonSelectionSourceStub{
		snapshots: map[string]ComparisonLoadedSnapshot{
			pinned.SnapshotID: pinned, newer.SnapshotID: newer, other.SnapshotID: other,
		},
		revisions: map[ComparisonRevisionKey]ComparisonLoadedRevision{
			{RecordID: "rec_fixeda", RevisionID: "rrv_fixeda"}: {
				RecordID: "rec_fixeda", RevisionID: "rrv_fixeda",
				Metadata:    RevisionMetadataSnapshot{RecordType: "note", ImpactLevel: "low"},
				RecordScope: pinned.RecordScope,
				SnapshotIDs: []string{pinned.SnapshotID},
			},
		},
	}
	first, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, nil, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{Revision: &ComparisonFixedRevision{RecordID: "rec_fixeda", RevisionID: "rrv_fixeda"}},
			{SnapshotID: comparisonStringPtr(other.SnapshotID)},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
	})
	if err != nil {
		t.Fatalf("ResolveFixedComparison() error = %v", err)
	}
	source.revisions[ComparisonRevisionKey{RecordID: "rec_fixeda", RevisionID: "rrv_fixeda"}] = ComparisonLoadedRevision{
		RecordID: "rec_fixeda", RevisionID: "rrv_fixeda",
		Metadata:    RevisionMetadataSnapshot{RecordType: "note", ImpactLevel: "low"},
		RecordScope: pinned.RecordScope,
		SnapshotIDs: []string{pinned.SnapshotID},
	}
	second, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, nil, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{Revision: &ComparisonFixedRevision{RecordID: "rec_fixeda", RevisionID: "rrv_fixeda"}},
			{SnapshotID: comparisonStringPtr(other.SnapshotID)},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
	})
	if err != nil {
		t.Fatalf("ResolveFixedComparison(again) error = %v", err)
	}
	if first.Items[0].SnapshotID != pinned.SnapshotID || second.Items[0].SnapshotID != pinned.SnapshotID {
		t.Fatalf("pinned snapshot drifted: first=%q second=%q", first.Items[0].SnapshotID, second.Items[0].SnapshotID)
	}
	if first.Digest != second.Digest {
		t.Fatalf("pinned revision digest drifted: %x vs %x", first.Digest, second.Digest)
	}
	if first.Items[0].Hash != pinned.Hash || second.Items[0].Hash != pinned.Hash {
		t.Fatal("pinned revision hash drifted after an unreferenced snapshot appeared")
	}
}

func TestFixedComparisonHidesMissingOrDeniedSelection(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	visible := loadedComparisonSnapshot(t, "evs_fixeda", CommandAuditV1Key(), false)
	source := &comparisonSelectionSourceStub{snapshots: map[string]ComparisonLoadedSnapshot{visible.SnapshotID: visible}}
	_, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, nil, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr(visible.SnapshotID)},
			{SnapshotID: comparisonStringPtr("evs_hidden")},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
	})
	if !errors.Is(err, ErrComparisonSelectionNotFound) {
		t.Fatalf("ResolveFixedComparison() error = %v, want opaque not found", err)
	}
	if strings.Contains(err.Error(), "evs_hidden") || strings.Contains(err.Error(), "evs_fixeda") {
		t.Fatalf("not-found error leaked selection identity: %v", err)
	}
}

func TestComparisonSummaryOmitsDetailAndListsAvailableKinds(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	left := loadedComparisonSnapshot(t, "evs_fixeda", CommandAuditV1Key(), false)
	right := loadedComparisonSnapshot(t, "evs_fixedb", MonitoringHostV1Key(), false)
	source := &comparisonSelectionSourceStub{snapshots: map[string]ComparisonLoadedSnapshot{
		left.SnapshotID: left, right.SnapshotID: right,
	}}
	result, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, nil, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr(left.SnapshotID)},
			{SnapshotID: comparisonStringPtr(right.SnapshotID)},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
	})
	if err != nil {
		t.Fatalf("ResolveFixedComparison() error = %v", err)
	}
	if len(result.Series) != 0 || len(result.Pairwise) != 0 {
		t.Fatalf("summary leaked detail: series=%#v pairwise=%#v", result.Series, result.Pairwise)
	}
	if !containsKind(result.AvailableKinds, CommandAuditV1Key()) || !containsKind(result.AvailableKinds, MonitoringHostV1Key()) {
		t.Fatalf("available kinds = %#v", result.AvailableKinds)
	}
}

func TestComparisonDetailBindsHostProbeSeriesAndBlocksCommonOverlap(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	leftItem := comparisonHostItem(t, "evs_hosta", start, 10)
	rightItem := comparisonHostItem(t, "evs_hostb", start.Add(time.Second), 12)
	left := loadedFromItem(t, leftItem)
	right := loadedFromItem(t, rightItem)
	source := &comparisonSelectionSourceStub{snapshots: map[string]ComparisonLoadedSnapshot{
		left.SnapshotID: left, right.SnapshotID: right,
	}}
	series, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, nil, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr(left.SnapshotID)},
			{SnapshotID: comparisonStringPtr(right.SnapshotID)},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
		Tolerance: 5 * time.Second,
		Detail:    &ComparisonDetail{Kind: MonitoringHostV1Key(), Metric: "cpu_pct"},
	})
	if err != nil {
		t.Fatalf("ResolveFixedComparison(host detail) error = %v", err)
	}
	if len(series.Series) != 2 || pointCount(series.Series[0]) == 0 || pointCount(series.Series[1]) == 0 {
		t.Fatalf("host series missing: %#v", series.Series)
	}

	blocked, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, nil, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr(left.SnapshotID)},
			{SnapshotID: comparisonStringPtr(right.SnapshotID)},
		},
		BaselineIndex: 0, Alignment: CoverageCommonOverlap, RequestedWindow: testWindow(),
		Detail: &ComparisonDetail{Kind: MonitoringHostV1Key(), Metric: "cpu_pct"},
	})
	if err != nil {
		t.Fatalf("ResolveFixedComparison(common_overlap) error = %v", err)
	}
	if len(blocked.Series) != 0 || len(blocked.Pairwise) != 0 {
		t.Fatalf("common_overlap leaked numeric detail: %#v %#v", blocked.Series, blocked.Pairwise)
	}
	if !hasComparisonReason(blocked.Review, ReasonCommonOverlapUnsupported) {
		t.Fatalf("Review = %#v, want %q", blocked.Review, ReasonCommonOverlapUnsupported)
	}
}

func TestFixedComparisonSignFailureDoesNotLookEligible(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	left := loadedComparisonSnapshot(t, "evs_fixeda", CommandAuditV1Key(), false)
	right := loadedComparisonSnapshot(t, "evs_fixedb", CommandAuditV1Key(), false)
	source := &comparisonSelectionSourceStub{snapshots: map[string]ComparisonLoadedSnapshot{
		left.SnapshotID: left, right.SnapshotID: right,
	}}
	_, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, comparisonIntentFailingSigner{}, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr(left.SnapshotID)},
			{SnapshotID: comparisonStringPtr(right.SnapshotID)},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
		Now: time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrComparisonIntentUnavailable) {
		t.Fatalf("ResolveFixedComparison() error = %v, want %v", err, ErrComparisonIntentUnavailable)
	}
}

func TestFixedComparisonUnreadableDoesNotSignIntent(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	readable := loadedComparisonSnapshot(t, "evs_fixeda", CommandAuditV1Key(), false)
	unreadable := loadedComparisonSnapshot(t, "evs_fixedb", CommandAuditV1Key(), true)
	source := &comparisonSelectionSourceStub{snapshots: map[string]ComparisonLoadedSnapshot{
		readable.SnapshotID: readable, unreadable.SnapshotID: unreadable,
	}}
	signer := &comparisonIntentSignerStub{keyID: "cmp_test"}
	result, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, signer, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr(readable.SnapshotID)},
			{SnapshotID: comparisonStringPtr(unreadable.SnapshotID)},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
		Now: time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ResolveFixedComparison() error = %v", err)
	}
	if result.SaveEligibility.Eligible || result.Intent != nil || signer.signCalls != 0 {
		t.Fatalf("unreadable signed intent: eligibility=%#v intent=%#v calls=%d", result.SaveEligibility, result.Intent, signer.signCalls)
	}
	if !hasComparisonReason(result.Review, ReasonSnapshotUnreadable) {
		t.Fatalf("Review = %#v, want %q", result.Review, ReasonSnapshotUnreadable)
	}

	source.snapshots["evs_fixedc"] = loadedComparisonSnapshot(t, "evs_fixedc", CommandAuditV1Key(), false)
	ok, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, signer, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr(readable.SnapshotID)},
			{SnapshotID: comparisonStringPtr("evs_fixedc")},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
		Now: time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ResolveFixedComparison(readable retry) error = %v", err)
	}
	if !ok.SaveEligibility.Eligible || ok.Intent == nil || ok.Intent.KeyID != "cmp_test" {
		t.Fatalf("readable eligibility/intent = %#v %#v", ok.SaveEligibility, ok.Intent)
	}
	if ok.Intent.ExpiresAt.Sub(ok.Intent.IssuedAt) != ComparisonIntentTTL {
		t.Fatalf("intent TTL = %s, want %s", ok.Intent.ExpiresAt.Sub(ok.Intent.IssuedAt), ComparisonIntentTTL)
	}
}

type comparisonSelectionSourceStub struct {
	snapshots     map[string]ComparisonLoadedSnapshot
	revisions     map[ComparisonRevisionKey]ComparisonLoadedRevision
	snapshotCalls int
	revisionCalls int
}

func (stub *comparisonSelectionSourceStub) LoadComparisonSnapshots(
	_ context.Context,
	_ ActorScope,
	ids []string,
) (map[string]ComparisonLoadedSnapshot, error) {
	stub.snapshotCalls++
	out := make(map[string]ComparisonLoadedSnapshot, len(ids))
	for _, id := range ids {
		if loaded, ok := stub.snapshots[id]; ok {
			out[id] = loaded
		}
	}
	return out, nil
}

func (stub *comparisonSelectionSourceStub) LoadComparisonRevisions(
	_ context.Context,
	_ ActorScope,
	keys []ComparisonRevisionKey,
) (map[ComparisonRevisionKey]ComparisonLoadedRevision, error) {
	stub.revisionCalls++
	out := make(map[ComparisonRevisionKey]ComparisonLoadedRevision, len(keys))
	for _, key := range keys {
		if loaded, ok := stub.revisions[key]; ok {
			out[key] = loaded
		}
	}
	return out, nil
}

type comparisonIntentSignerStub struct {
	keyID     string
	signCalls int
}

func (stub *comparisonIntentSignerStub) Sign(claims ComparisonIntentClaims) (ComparisonIntent, error) {
	stub.signCalls++
	if claims.Purpose != ComparisonIntentPurpose {
		return ComparisonIntent{}, ErrComparisonIntentInvalid
	}
	return ComparisonIntent{
		Token:     "cmp-test-token",
		KeyID:     stub.keyID,
		IssuedAt:  claims.IssuedAt,
		ExpiresAt: claims.ExpiresAt,
	}, nil
}

func (*comparisonIntentSignerStub) Verify(string, time.Time) (ComparisonIntentClaims, error) {
	return ComparisonIntentClaims{}, ErrComparisonIntentInvalid
}

type comparisonIntentFailingSigner struct{}

func (comparisonIntentFailingSigner) Sign(ComparisonIntentClaims) (ComparisonIntent, error) {
	return ComparisonIntent{}, ErrComparisonIntentUnavailable
}

func (comparisonIntentFailingSigner) Verify(string, time.Time) (ComparisonIntentClaims, error) {
	return ComparisonIntentClaims{}, ErrComparisonIntentInvalid
}

func loadedComparisonSnapshot(t *testing.T, snapshotID string, key KindKey, unreadable bool) ComparisonLoadedSnapshot {
	t.Helper()
	item := comparisonTestItem(t, snapshotID, key, RevisionContextNotApplicable, nil)
	scope := comparisonProjectRecordScope(t, ComparisonSubjectRef{Kind: "target", ID: "tg_0123456789abcdef"})
	return ComparisonLoadedSnapshot{
		SnapshotID: snapshotID, RecordID: "rec_" + strings.TrimPrefix(snapshotID, "evs_"),
		Kind: key, Hash: item.Hash, Snapshot: item.Snapshot,
		RecordScope: scope, SourceAuthorization: scope.Sources[0],
		SourceAvailable: true, Unreadable: unreadable,
	}
}

func loadedFromItem(t *testing.T, item ComparisonItemInput) ComparisonLoadedSnapshot {
	t.Helper()
	scope := comparisonProjectRecordScope(t, ComparisonSubjectRef{Kind: "target", ID: "tg_0123456789abcdef"})
	return ComparisonLoadedSnapshot{
		SnapshotID: item.SnapshotID, RecordID: "rec_" + strings.TrimPrefix(item.SnapshotID, "evs_"),
		Kind: item.Kind, Hash: item.Hash, Snapshot: item.Snapshot,
		RecordScope: scope, SourceAuthorization: scope.Sources[0], SourceAvailable: true,
	}
}

func comparisonStringPtr(value string) *string {
	return &value
}

func containsKind(kinds []KindKey, want KindKey) bool {
	for _, key := range kinds {
		if key == want {
			return true
		}
	}
	return false
}
