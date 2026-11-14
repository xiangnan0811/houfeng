package evidence

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestVerifyComparisonSaveReplayAuthenticatesExpiredIntentWithoutRecapture(t *testing.T) {
	ctx := context.Background()
	actor := testActor(t)
	issued := time.Now().UTC().Add(-time.Hour)
	left := loadedComparisonSnapshot(t, "evs_fixeda", CommandAuditV1Key(), false)
	right := loadedComparisonSnapshot(t, "evs_fixedb", CommandAuditV1Key(), false)
	source := &comparisonSelectionSourceStub{
		snapshots: map[string]ComparisonLoadedSnapshot{left.SnapshotID: left, right.SnapshotID: right},
		revisions: map[ComparisonRevisionKey]ComparisonLoadedRevision{
			{RecordID: left.RecordID, RevisionID: "rrv_fixeda"}: {
				RecordID: left.RecordID, RevisionID: "rrv_fixeda", RecordScope: left.RecordScope,
				SnapshotIDs: []string{left.SnapshotID}, Metadata: RevisionMetadataSnapshot{
					RecordType: "note", ImpactLevel: "low", OccurredAt: issued, HasOccurredAt: true,
				},
			},
		},
	}
	signer := mustComparisonSaveSigner(t)
	resolved, err := ResolveFixedComparison(ctx, comparisonTestRegistry(t), source, signer, ComparisonEvaluateRequest{
		Actor: actor,
		Items: []ComparisonFixedItem{
			{Revision: &ComparisonFixedRevision{RecordID: left.RecordID, RevisionID: "rrv_fixeda"}},
			{SnapshotID: comparisonStringPtr(right.SnapshotID)},
		},
		BaselineIndex: 1, Alignment: CoverageActual, RequestedWindow: testWindow(),
		Tolerance: 30 * time.Second, Detail: &ComparisonDetail{Kind: CommandAuditV1Key()}, Now: issued,
	})
	if err != nil || resolved.Intent == nil || !resolved.SaveEligibility.Eligible {
		t.Fatalf("resolve comparison = %#v, %v", resolved, err)
	}
	sink := &comparisonSavePayloadSink{}
	const recordID = "rec_comparisonsave"
	plan, err := PrepareComparisonSave(ctx, comparisonTestRegistry(t), source, signer, sink, ComparisonSaveRequest{
		Actor: actor, RecordID: recordID, Token: resolved.Intent.Token, Now: issued.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("prepare original comparison = %v", err)
	}
	want := make([]string, 0, len(plan.Copies)+1)
	for _, copy := range plan.Copies {
		want = append(want, copy.SnapshotID())
	}
	want = append(want, plan.Result.SnapshotID())
	if len(want) != 3 || sink.calls != 1 {
		t.Fatalf("original comparison identities = %v, persisted payloads = %d", want, sink.calls)
	}

	service := &Service{comparisonSelection: source, comparisonSigner: signer}
	beforeSnapshots, beforeRevisions := source.snapshotCalls, source.revisionCalls
	// A real historical replay must work even after both source types vanish.
	source.snapshots, source.revisions = nil, nil
	got, err := service.VerifyComparisonSaveReplay(ctx, actor, recordID, resolved.Intent.Token)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("expired comparison replay = %v, %v; want %v", got, err, want)
	}
	if source.snapshotCalls != beforeSnapshots || source.revisionCalls != beforeRevisions || sink.calls != 1 {
		t.Fatal("replay resolved sources or persisted another payload")
	}

	claims, err := signer.Verify(resolved.Intent.Token, time.Now().UTC())
	if !errors.Is(err, ErrComparisonIntentExpired) {
		t.Fatalf("fixture token was not expired: %v", err)
	}
	// The released predecessor signed claims before source_record_id existed.
	// The field is omitempty, so this is the predecessor's actual token shape.
	legacy := claims
	legacy.Items = append([]ComparisonIntentItem(nil), claims.Items...)
	for index := range legacy.Items {
		legacy.Items[index].SourceRecordID = ""
	}
	legacyIntent, err := signer.Sign(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyIDs, err := service.VerifyComparisonSaveReplay(ctx, actor, recordID, legacyIntent.Token)
	if err != nil || !reflect.DeepEqual(legacyIDs, want) {
		t.Fatalf("expired predecessor token replay = %v, %v; want %v", legacyIDs, err, want)
	}
	if source.snapshotCalls != beforeSnapshots || source.revisionCalls != beforeRevisions || sink.calls != 1 {
		t.Fatal("predecessor replay resolved sources or persisted another payload")
	}

	changed := claims
	changed.Items = append([]ComparisonIntentItem(nil), claims.Items...)
	changed.Items[1].SnapshotID = "evs_changed"
	changedIntent, err := signer.Sign(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyComparisonSaveReplay(ctx, actor, recordID, changedIntent.Token); !errors.Is(err, ErrComparisonIntentInvalid) {
		t.Fatalf("signed claims with inconsistent semantic digest = %v", err)
	}
	wrongOwner := claims
	wrongOwner.Items = append([]ComparisonIntentItem(nil), claims.Items...)
	wrongOwner.Items[0].SourceRecordID = "rec_wrongowner"
	wrongOwnerIntent, err := signer.Sign(wrongOwner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyComparisonSaveReplay(ctx, actor, recordID, wrongOwnerIntent.Token); !errors.Is(err, ErrComparisonIntentInvalid) {
		t.Fatalf("contradictory signed source = %v", err)
	}
	wrongProject := claims
	wrongProject.ProjectID = "other"
	wrongProjectIntent, err := signer.Sign(wrongProject)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyComparisonSaveReplay(ctx, actor, recordID, wrongProjectIntent.Token); !errors.Is(err, ErrComparisonIntentInvalid) {
		t.Fatalf("different signed project = %v", err)
	}
	if _, err := service.VerifyComparisonSaveReplay(ctx, actor, recordID, resolved.Intent.Token+"x"); !errors.Is(err, ErrComparisonIntentInvalid) {
		t.Fatalf("tampered expired token = %v", err)
	}
	otherActor := actor.Clone()
	otherActor.Role = recordauth.RoleViewer
	if _, err := service.VerifyComparisonSaveReplay(ctx, otherActor, recordID, resolved.Intent.Token); !errors.Is(err, ErrComparisonIntentInvalid) {
		t.Fatalf("different actor scope = %v", err)
	}

	dir := t.TempDir()
	writeComparisonKey(t, filepath.Join(dir, "other_key"), 0400)
	otherSigner, err := OpenComparisonIntentKeyring(dir, "other_key", nil)
	if err != nil {
		t.Fatal(err)
	}
	otherService := &Service{comparisonSigner: otherSigner}
	if _, err := otherService.VerifyComparisonSaveReplay(ctx, actor, recordID, resolved.Intent.Token); !errors.Is(err, ErrComparisonIntentInvalid) {
		t.Fatalf("unknown signing key = %v", err)
	}
}

func TestVerifyComparisonSaveReplayMetadataOnly(t *testing.T) {
	ctx := context.Background()
	actor := testActor(t)
	issued := time.Now().UTC().Add(-time.Hour)
	scope := comparisonProjectRecordScope(t, ComparisonSubjectRef{Kind: "target", ID: "tg_0123456789abcdef"})
	source := &comparisonSelectionSourceStub{revisions: map[ComparisonRevisionKey]ComparisonLoadedRevision{
		{RecordID: "rec_metaa", RevisionID: "rrv_metaa"}: {
			RecordID: "rec_metaa", RevisionID: "rrv_metaa", RecordScope: scope,
			Metadata: RevisionMetadataSnapshot{RecordType: "note", ImpactLevel: "low"},
		},
		{RecordID: "rec_metab", RevisionID: "rrv_metab"}: {
			RecordID: "rec_metab", RevisionID: "rrv_metab", RecordScope: scope,
			Metadata: RevisionMetadataSnapshot{RecordType: "note", ImpactLevel: "low"},
		},
	}}
	signer := mustComparisonSaveSigner(t)
	resolved, err := ResolveFixedComparison(ctx, comparisonTestRegistry(t), source, signer, ComparisonEvaluateRequest{
		Actor: actor, Items: []ComparisonFixedItem{
			{Revision: &ComparisonFixedRevision{RecordID: "rec_metaa", RevisionID: "rrv_metaa"}},
			{Revision: &ComparisonFixedRevision{RecordID: "rec_metab", RevisionID: "rrv_metab"}},
		}, BaselineIndex: 0, Alignment: CoverageActual, RequestedWindow: testWindow(), Now: issued,
	})
	if err != nil || resolved.Intent == nil {
		t.Fatalf("resolve metadata comparison = %#v, %v", resolved, err)
	}
	plan, err := PrepareComparisonSave(ctx, comparisonTestRegistry(t), source, signer, nil, ComparisonSaveRequest{
		Actor: actor, RecordID: "rec_result", Token: resolved.Intent.Token, Now: issued.Add(time.Minute),
	})
	if err != nil || len(plan.Copies) != 0 {
		t.Fatalf("prepare metadata comparison = %#v, %v", plan, err)
	}
	before := source.revisionCalls
	source.revisions = nil
	service := &Service{comparisonSelection: source, comparisonSigner: signer}
	got, err := service.VerifyComparisonSaveReplay(ctx, actor, "rec_result", resolved.Intent.Token)
	if err != nil || !reflect.DeepEqual(got, []string{plan.Result.SnapshotID()}) || source.revisionCalls != before {
		t.Fatalf("metadata-only replay = %v, %v", got, err)
	}
}
