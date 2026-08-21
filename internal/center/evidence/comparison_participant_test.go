package evidence

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestComparisonParticipantRejectsForgedExpiredAndStaleIntent(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	source := comparisonSaveTestSource(t)
	tests := []struct {
		name    string
		signer  ComparisonIntentSigner
		token   string
		wantErr error
	}{
		{
			name:    "forged",
			signer:  comparisonIntentVerifyStub{err: ErrComparisonIntentInvalid},
			token:   "cmp1.forged.payload.mac",
			wantErr: ErrComparisonIntentInvalid,
		},
		{
			name:    "expired without authentic claims",
			signer:  comparisonIntentVerifyStub{err: ErrComparisonIntentExpired},
			token:   "cmp1.expired.payload.mac",
			wantErr: ErrComparisonIntentExpired,
		},
		{
			name:    "stale digest",
			signer:  comparisonIntentVerifyStub{claims: staleComparisonClaims(t, actor, now, "00")},
			token:   "cmp1.stale.payload.mac",
			wantErr: ErrComparisonIntentStale,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PrepareComparisonSave(context.Background(), comparisonTestRegistry(t), source, tt.signer, nil, ComparisonSaveRequest{
				Actor: actor, RecordID: "rec_comparisonsave", Token: tt.token, Now: now,
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("PrepareComparisonSave() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestComparisonParticipantBuildsLogicalCopiesAndResultWithoutConclusion(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	now := time.Date(2026, 8, 20, 9, 15, 0, 0, time.UTC)
	source := comparisonSaveTestSource(t)
	signer := mustComparisonSaveSigner(t)
	resolved, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, signer, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr("evs_fixeda")},
			{SnapshotID: comparisonStringPtr("evs_fixedb")},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(), Now: now,
	})
	if err != nil || resolved.Intent == nil {
		t.Fatalf("ResolveFixedComparison() = %#v, %v", resolved, err)
	}

	sink := &comparisonSavePayloadSink{}
	plan, err := PrepareComparisonSave(context.Background(), comparisonTestRegistry(t), source, signer, sink, ComparisonSaveRequest{
		Actor: actor, RecordID: "rec_comparisonsave", Token: resolved.Intent.Token, Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareComparisonSave() error = %v", err)
	}
	if plan.Token != resolved.Intent.Token || len(plan.Copies) != 2 {
		t.Fatalf("plan = copies:%d token:%q", len(plan.Copies), plan.Token)
	}
	sourceIDs := map[string]struct{}{"evs_fixeda": {}, "evs_fixedb": {}}
	copied := make(map[string]string, 2)
	for _, copy := range plan.Copies {
		if err := copy.Validate(); err != nil {
			t.Fatalf("copy.Validate() error = %v", err)
		}
		if copy.RecordID() != "rec_comparisonsave" || copy.SnapshotID() == copy.CopiedFromSnapshotID() {
			t.Fatalf("copy identities = %#v", copy)
		}
		if _, exists := sourceIDs[copy.SnapshotID()]; exists {
			t.Fatalf("copy reused source snapshot identity %q", copy.SnapshotID())
		}
		if copy.Snapshot().Hash() != source.snapshots[copy.CopiedFromSnapshotID()].Hash {
			t.Fatalf("copy payload digest drifted from source %q", copy.CopiedFromSnapshotID())
		}
		copied[copy.CopiedFromSnapshotID()] = copy.SnapshotID()
	}
	if len(copied) != 2 || copied["evs_fixeda"] == "" || copied["evs_fixedb"] == "" {
		t.Fatalf("original→copied = %#v", copied)
	}
	if plan.Result.RecordID() != "rec_comparisonsave" || plan.Result.SnapshotID() == "" ||
		plan.Result.Snapshot().Envelope().Key != ComparisonResultV1Key() {
		t.Fatalf("result = record:%q snapshot:%q key:%q", plan.Result.RecordID(), plan.Result.SnapshotID(), plan.Result.Snapshot().Envelope().Key)
	}
	if plan.Result.Snapshot().Envelope().Subject.Type != "comparison" {
		t.Fatalf("result envelope copied a machine subject: %#v", plan.Result.Snapshot().Envelope().Subject)
	}
	if _, exists := sourceIDs[plan.Result.SnapshotID()]; exists {
		t.Fatal("result snapshot reused a source identity")
	}
	assertComparisonResultHasNoHumanConclusion(t, plan.Result.Snapshot().Bytes())
	if sink.calls != 1 {
		t.Fatalf("result payload persist calls = %d, want 1", sink.calls)
	}
	again, err := PrepareComparisonSave(context.Background(), comparisonTestRegistry(t), source, signer, sink, ComparisonSaveRequest{
		Actor: actor, RecordID: "rec_comparisonsave", Token: resolved.Intent.Token, Now: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareComparisonSave(retry) error = %v", err)
	}
	if again.Copies[0].SnapshotID() != plan.Copies[0].SnapshotID() || again.Result.SnapshotID() != plan.Result.SnapshotID() {
		t.Fatalf("retry allocated new identities: first=%q/%q retry=%q/%q",
			plan.Copies[0].SnapshotID(), plan.Result.SnapshotID(), again.Copies[0].SnapshotID(), again.Result.SnapshotID())
	}
}

func TestComparisonParticipantPersistsRevisionMetadataConditionsAndDetail(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	now := time.Date(2026, 8, 20, 9, 15, 0, 0, time.UTC)
	occurred := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	left := loadedComparisonSnapshot(t, "evs_fixeda", CommandAuditV1Key(), false)
	right := loadedComparisonSnapshot(t, "evs_fixedb", CommandAuditV1Key(), false)
	source := &comparisonSelectionSourceStub{
		snapshots: map[string]ComparisonLoadedSnapshot{
			left.SnapshotID: left, right.SnapshotID: right,
		},
		revisions: map[ComparisonRevisionKey]ComparisonLoadedRevision{
			{RecordID: "rec_fixeda", RevisionID: "rrv_fixeda"}: {
				RecordID: "rec_fixeda", RevisionID: "rrv_fixeda",
				Metadata: RevisionMetadataSnapshot{
					RecordType: "incident", BusinessStatus: "open", StatusGroup: "active",
					ImpactLevel: "high", OccurredAt: occurred, HasOccurredAt: true,
				},
				RecordScope: left.RecordScope,
				SnapshotIDs: []string{left.SnapshotID},
			},
		},
	}
	signer := mustComparisonSaveSigner(t)
	resolved, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, signer, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{Revision: &ComparisonFixedRevision{RecordID: "rec_fixeda", RevisionID: "rrv_fixeda"}},
			{SnapshotID: comparisonStringPtr(right.SnapshotID)},
		},
		BaselineIndex: 1, Alignment: CoverageActual, RequestedWindow: testWindow(),
		Detail: &ComparisonDetail{Kind: CommandAuditV1Key()}, Now: now,
	})
	if err != nil || resolved.Intent == nil {
		t.Fatalf("ResolveFixedComparison() = %#v, %v", resolved, err)
	}
	if resolved.Items[0].Revision == nil || resolved.Items[0].RecordID != "rec_fixeda" {
		t.Fatalf("resolved revision item = %#v", resolved.Items[0])
	}

	plan, err := PrepareComparisonSave(context.Background(), comparisonTestRegistry(t), source, signer, &comparisonSavePayloadSink{}, ComparisonSaveRequest{
		Actor: actor, RecordID: "rec_comparisonsave", Token: resolved.Intent.Token, Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareComparisonSave() error = %v", err)
	}
	payload, err := decodeSnapshotPayload(plan.Result.Snapshot().Bytes())
	if err != nil {
		t.Fatalf("decode comparison result: %v", err)
	}
	baseline, _ := payload["baseline_index"].(float64)
	if baseline != 1 || payload["alignment"] != string(CoverageActual) {
		t.Fatalf("saved conditions = %#v", payload)
	}
	if payload["digest"] != hex.EncodeToString(resolved.Digest[:]) {
		t.Fatalf("saved digest = %v, want %s", payload["digest"], hex.EncodeToString(resolved.Digest[:]))
	}
	items, _ := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("saved items = %#v", payload["items"])
	}
	leftItem, _ := items[0].(map[string]any)
	rightItem, _ := items[1].(map[string]any)
	if leftItem["original_snapshot_id"] != left.SnapshotID ||
		leftItem["copied_snapshot_id"] == "" ||
		leftItem["copied_snapshot_id"] == left.SnapshotID ||
		leftItem["record_type"] != "incident" ||
		leftItem["business_status"] != "open" ||
		leftItem["status_group"] != "active" ||
		leftItem["impact_level"] != "high" ||
		leftItem["occurred_at"] != occurred.UTC().Format(time.RFC3339Nano) ||
		leftItem["revision_context"] != string(RevisionContextBound) {
		t.Fatalf("saved revision item = %#v", leftItem)
	}
	if rightItem["original_snapshot_id"] != right.SnapshotID ||
		rightItem["revision_context"] != string(RevisionContextNotApplicable) ||
		rightItem["record_type"] != nil ||
		rightItem["impact_level"] != nil {
		t.Fatalf("saved snapshot-only item leaked revision metadata: %#v", rightItem)
	}
	if _, ok := payload["warnings"]; !ok {
		t.Fatal("saved result missing warnings")
	}
	differences, _ := payload["system_differences"].([]any)
	if len(differences) == 0 {
		t.Fatalf("saved result missing system differences: %#v", payload["system_differences"])
	}
	firstDiff, _ := differences[0].(map[string]any)
	if firstDiff["item_index"] != float64(0) && firstDiff["item_index"] != 0 {
		t.Fatalf("system_differences item_index = %#v, want compared item 0 when baseline is 1", firstDiff["item_index"])
	}
	if leftItem["record_id"] != "rec_fixeda" || leftItem["revision_id"] != "rrv_fixeda" {
		t.Fatalf("saved revision identities = %#v", leftItem)
	}
	assertComparisonResultHasNoHumanConclusion(t, payload)

	kind := mustComparisonResultKind(t)
	summary := kind.Summarize(plan.Result.Snapshot())
	readItems, _ := summary.ReadModel["items"].([]any)
	summarizedBaseline, _ := summary.ReadModel["baseline_index"].(float64)
	if summarizedBaseline != 1 || len(readItems) != 2 {
		t.Fatalf("Summarize() dropped saved fields: %#v", summary.ReadModel)
	}
	readLeft, _ := readItems[0].(map[string]any)
	if readLeft["record_type"] != "incident" || readLeft["impact_level"] != "high" {
		t.Fatalf("Summarize() dropped revision metadata: %#v", readLeft)
	}
	exported := kind.Export(plan.Result.Snapshot(), ExportModeSafe)
	if !bytes.Equal(exported.Bytes, plan.Result.Snapshot().Bytes()) {
		t.Fatal("Export() bytes must equal the canonical snapshot")
	}
	assertComparisonResultHasNoHumanConclusion(t, exported.Bytes)
}

func TestComparisonParticipantRejectsExpiredAuthenticIntentWithoutReplay(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	issued := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	source := comparisonSaveTestSource(t)
	signer := mustComparisonSaveSigner(t)
	resolved, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, signer, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr("evs_fixeda")},
			{SnapshotID: comparisonStringPtr("evs_fixedb")},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(), Now: issued,
	})
	if err != nil || resolved.Intent == nil {
		t.Fatalf("ResolveFixedComparison() = %#v, %v", resolved, err)
	}
	if _, err := PrepareComparisonSave(context.Background(), comparisonTestRegistry(t), source, signer, nil, ComparisonSaveRequest{
		Actor: actor, RecordID: "rec_comparisonsave", Token: resolved.Intent.Token, Now: issued.Add(time.Minute),
	}); err != nil {
		t.Fatalf("PrepareComparisonSave(live) error = %v", err)
	}
	_, err = PrepareComparisonSave(context.Background(), comparisonTestRegistry(t), source, signer, nil, ComparisonSaveRequest{
		Actor: actor, RecordID: "rec_comparisonsave", Token: resolved.Intent.Token, Now: issued.Add(ComparisonIntentTTL + time.Minute),
	})
	if !errors.Is(err, ErrComparisonIntentExpired) {
		t.Fatalf("PrepareComparisonSave(expired authentic) error = %v, want %v", err, ErrComparisonIntentExpired)
	}
}

func TestComparisonParticipantReplaysExpiredAuthenticIntentWhenFlagged(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	issued := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	source := comparisonSaveTestSource(t)
	signer := mustComparisonSaveSigner(t)
	resolved, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, signer, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr("evs_fixeda")},
			{SnapshotID: comparisonStringPtr("evs_fixedb")},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(), Now: issued,
	})
	if err != nil || resolved.Intent == nil {
		t.Fatalf("ResolveFixedComparison() = %#v, %v", resolved, err)
	}
	first, err := PrepareComparisonSave(context.Background(), comparisonTestRegistry(t), source, signer, nil, ComparisonSaveRequest{
		Actor: actor, RecordID: "rec_comparisonsave", Token: resolved.Intent.Token, Now: issued.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareComparisonSave(live) error = %v", err)
	}
	replay, err := PrepareComparisonSave(context.Background(), comparisonTestRegistry(t), source, signer, nil, ComparisonSaveRequest{
		Actor: actor, RecordID: "rec_comparisonsave", Token: resolved.Intent.Token,
		Now: issued.Add(ComparisonIntentTTL + time.Minute), AllowExpiredReplay: true,
	})
	if err != nil {
		t.Fatalf("PrepareComparisonSave(expired replay) error = %v", err)
	}
	if replay.Result.SnapshotID() != first.Result.SnapshotID() || len(replay.Copies) != len(first.Copies) {
		t.Fatalf("expired replay allocated new identities: live=%q/%d expired=%q/%d",
			first.Result.SnapshotID(), len(first.Copies), replay.Result.SnapshotID(), len(replay.Copies))
	}
}

func TestComparisonParticipantPersistsHostSeriesDeltas(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	now := time.Date(2026, 8, 20, 9, 15, 0, 0, time.UTC)
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	left := loadedFromItem(t, comparisonHostItem(t, "evs_hosta", start, 10))
	right := loadedFromItem(t, comparisonHostItem(t, "evs_hostb", start.Add(time.Second), 12))
	source := &comparisonSelectionSourceStub{
		snapshots: map[string]ComparisonLoadedSnapshot{left.SnapshotID: left, right.SnapshotID: right},
	}
	signer := mustComparisonSaveSigner(t)
	resolved, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, signer, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{SnapshotID: comparisonStringPtr(left.SnapshotID)},
			{SnapshotID: comparisonStringPtr(right.SnapshotID)},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
		Tolerance: time.Minute, Detail: &ComparisonDetail{Kind: MonitoringHostV1Key(), Metric: "cpu_pct"},
		Now: now,
	})
	if err != nil || resolved.Intent == nil {
		t.Fatalf("ResolveFixedComparison() = %#v, %v", resolved, err)
	}
	plan, err := PrepareComparisonSave(context.Background(), comparisonTestRegistry(t), source, signer, &comparisonSavePayloadSink{}, ComparisonSaveRequest{
		Actor: actor, RecordID: "rec_comparisonsave", Token: resolved.Intent.Token, Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareComparisonSave(host) error = %v", err)
	}
	payload, err := decodeSnapshotPayload(plan.Result.Snapshot().Bytes())
	if err != nil {
		t.Fatalf("decode host result: %v", err)
	}
	differences, _ := payload["system_differences"].([]any)
	if len(differences) != 1 {
		t.Fatalf("system_differences = %#v, want one host pair", payload["system_differences"])
	}
	first, _ := differences[0].(map[string]any)
	if first["left_hash"] != nil || first["right_hash"] != nil {
		t.Fatalf("host difference leaked hash fields: %#v", first)
	}
	if first["matched"] != int64(1) && first["matched"] != float64(1) {
		t.Fatalf("host matched = %#v", first["matched"])
	}
	deltas, _ := first["deltas"].([]any)
	if len(deltas) != 1 {
		t.Fatalf("host deltas = %#v, want one bucket", first["deltas"])
	}
	delta, _ := deltas[0].(map[string]any)
	if delta["delta"] != float64(2) && delta["delta"] != 2 {
		t.Fatalf("host bucket delta = %#v, want 2", delta)
	}
	summary := mustComparisonResultKind(t).Summarize(plan.Result.Snapshot())
	readDiffs, _ := summary.ReadModel["system_differences"].([]any)
	readFirst, _ := readDiffs[0].(map[string]any)
	if _, ok := readFirst["deltas"]; !ok {
		t.Fatalf("Summarize() dropped host deltas: %#v", summary.ReadModel["system_differences"])
	}
}

func TestComparisonParticipantSavesMetadataOnlyWarningsWithoutCopies(t *testing.T) {
	t.Parallel()

	actor := testActor(t)
	now := time.Date(2026, 8, 20, 9, 15, 0, 0, time.UTC)
	scope := comparisonProjectRecordScope(t, ComparisonSubjectRef{Kind: "target", ID: "tg_0123456789abcdef"})
	source := &comparisonSelectionSourceStub{
		revisions: map[ComparisonRevisionKey]ComparisonLoadedRevision{
			{RecordID: "rec_metaa", RevisionID: "rrv_metaa"}: {
				RecordID: "rec_metaa", RevisionID: "rrv_metaa",
				Metadata:    RevisionMetadataSnapshot{RecordType: "note", ImpactLevel: "low"},
				RecordScope: scope,
			},
			{RecordID: "rec_metab", RevisionID: "rrv_metab"}: {
				RecordID: "rec_metab", RevisionID: "rrv_metab",
				Metadata:    RevisionMetadataSnapshot{RecordType: "note", ImpactLevel: "low"},
				RecordScope: scope,
			},
		},
	}
	signer := mustComparisonSaveSigner(t)
	resolved, err := ResolveFixedComparison(context.Background(), comparisonTestRegistry(t), source, signer, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{Revision: &ComparisonFixedRevision{RecordID: "rec_metaa", RevisionID: "rrv_metaa"}},
			{Revision: &ComparisonFixedRevision{RecordID: "rec_metab", RevisionID: "rrv_metab"}},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(), Now: now,
	})
	if err != nil || resolved.Intent == nil {
		t.Fatalf("ResolveFixedComparison() = %#v, %v", resolved, err)
	}
	if !resolved.SaveEligibility.Eligible {
		t.Fatalf("metadata-only eligibility = %#v, want eligible", resolved.SaveEligibility)
	}
	plan, err := PrepareComparisonSave(context.Background(), comparisonTestRegistry(t), source, signer, &comparisonSavePayloadSink{}, ComparisonSaveRequest{
		Actor: actor, RecordID: "rec_comparisonsave", Token: resolved.Intent.Token, Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PrepareComparisonSave(metadata-only) error = %v", err)
	}
	if len(plan.Copies) != 0 || plan.Result.Empty() || plan.Result.Snapshot().Envelope().Key != ComparisonResultV1Key() {
		t.Fatalf("metadata-only plan = copies:%d result:%q", len(plan.Copies), plan.Result.Snapshot().Envelope().Key)
	}
	payload, err := decodeSnapshotPayload(plan.Result.Snapshot().Bytes())
	if err != nil {
		t.Fatalf("decode metadata-only result: %v", err)
	}
	warnings, _ := payload["warnings"].([]any)
	if len(warnings) == 0 {
		t.Fatal("metadata-only result missing warnings")
	}
}

func comparisonSaveTestSource(t *testing.T) *comparisonSelectionSourceStub {
	t.Helper()
	return &comparisonSelectionSourceStub{snapshots: map[string]ComparisonLoadedSnapshot{
		"evs_fixeda": loadedComparisonSnapshot(t, "evs_fixeda", CommandAuditV1Key(), false),
		"evs_fixedb": loadedComparisonSnapshot(t, "evs_fixedb", CommandAuditV1Key(), false),
	}}
}

func mustComparisonSaveSigner(t *testing.T) *FileComparisonIntentSigner {
	t.Helper()
	dir := t.TempDir()
	writeComparisonKey(t, filepath.Join(dir, "cmp_save"), 0400)
	signer, err := OpenComparisonIntentKeyring(dir, "cmp_save", nil)
	if err != nil {
		t.Fatalf("OpenComparisonIntentKeyring() error = %v", err)
	}
	return signer
}

func staleComparisonClaims(t *testing.T, actor ActorScope, now time.Time, digestByte string) ComparisonIntentClaims {
	t.Helper()
	left := loadedComparisonSnapshot(t, "evs_fixeda", CommandAuditV1Key(), false)
	right := loadedComparisonSnapshot(t, "evs_fixedb", CommandAuditV1Key(), false)
	digest, err := hex.DecodeString(digestByte + "00000000000000000000000000000000000000000000000000000000000000")
	if err != nil || len(digest) < 32 {
		t.Fatalf("stale digest: %v", err)
	}
	var hashed [32]byte
	copy(hashed[:], digest[:32])
	return BuildComparisonIntentClaims(ComparisonIntentClaimsInput{
		Actor: actor,
		Items: []ResolvedComparisonItem{
			{SnapshotID: left.SnapshotID, Hash: left.Hash, Kind: left.Kind, RevisionContext: RevisionContextNotApplicable},
			{SnapshotID: right.SnapshotID, Hash: right.Hash, Kind: right.Kind, RevisionContext: RevisionContextNotApplicable},
		},
		BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(),
		Digest: hashed, Now: now, KeyID: "cmp_stale",
	})
}

type comparisonIntentVerifyStub struct {
	claims ComparisonIntentClaims
	err    error
}

func (stub comparisonIntentVerifyStub) Sign(ComparisonIntentClaims) (ComparisonIntent, error) {
	return ComparisonIntent{}, ErrComparisonIntentInvalid
}

func (stub comparisonIntentVerifyStub) Verify(string, time.Time) (ComparisonIntentClaims, error) {
	if stub.err != nil {
		return ComparisonIntentClaims{}, stub.err
	}
	return stub.claims, nil
}

type comparisonSavePayloadSink struct {
	calls int
}

func (sink *comparisonSavePayloadSink) PersistCapturePayload(context.Context, CanonicalSnapshot) error {
	if sink != nil {
		sink.calls++
	}
	return nil
}
