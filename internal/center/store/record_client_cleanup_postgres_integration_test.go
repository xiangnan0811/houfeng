package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordclientcleanup"
	"houfeng/internal/center/records"
)

// This exercises the restricted APP allocation lookup and direct runtime role.
// It intentionally does not manufacture an issuer gate or cleanup event.
func TestPostgresIntegrationCleanupAllocationReferences(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	if _, err := runtime.Exec(ctx, `insert into public.record_access_groups(group_id,display_name)
		values('rag_cleanup','Cleanup group')`); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Exec(ctx, `insert into public.record_access_group_members(group_id,user_id)
		values('rag_cleanup','usr_aaaaaaaaaaaaaaaaaaaaaaaa')`); err != nil {
		t.Fatal(err)
	}
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: recordauth.ProjectIDDefault,
		Role: recordauth.RoleProjectAdmin, GroupIDs: []string{"rag_cleanup"},
	})
	if err != nil {
		t.Fatal(err)
	}
	birthRepository := NewPostgresRecordBirthRepository(runtime, gate, nil)
	birth, err := recordbirth.NewCoordinator(birthRepository, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reserve := func(key string) recordbirth.Allocation {
		t.Helper()
		allocation, err := birth.Reserve(ctx, recordbirth.Request{
			ProjectID: string(actor.ProjectID), ActorID: actor.UserID, ActorScopeDigest: actor.CanonicalHash(),
			Purpose: recordbirth.PurposeCreate, IdempotencyKey: key, RequestFingerprint: sha256.Sum256([]byte(key)),
		})
		if err != nil {
			t.Fatal(err)
		}
		return allocation
	}
	requestFor := func(allocation recordbirth.Allocation) recordclientcleanup.ReconcileRequest {
		return recordclientcleanup.ReconcileRequest{BrowserID: "browser_cleanup", AllocationReferences: []recordclientcleanup.AllocationReference{{
			RecordID: allocation.RecordID, AllocationID: allocation.AllocationID,
		}}}
	}
	current := newRecordsPostgresAuthorization(t, runtime, gate)
	cleanup := NewPostgresRecordClientCleanupRepository(runtime, gate, current)
	allocation := reserve("cleanup-pending")
	request := requestFor(allocation)
	assertResult := func(got recordclientcleanup.ReconcileResult, err error, allowed, pending, invalidate []string) {
		t.Helper()
		if err != nil || !reflect.DeepEqual(got.Allowed, allowed) || !reflect.DeepEqual(got.Pending, pending) || !reflect.DeepEqual(got.Invalidate, invalidate) {
			t.Fatalf("reconciliation = %#v, %v; want allowed=%v pending=%v invalidate=%v", got, err, allowed, pending, invalidate)
		}
	}
	empty := []string{}
	got, err := cleanup.Reconcile(ctx, actor, request)
	assertResult(got, err, empty, []string{allocation.RecordID}, empty)
	var target bool
	if err := runtime.QueryRow(ctx, `select exists(select 1 from public.browser_record_cleanup_targets
		where user_id=$1 and browser_id=$2 and project_id=$3 and record_id=$4)`, actor.UserID,
		request.BrowserID, actor.ProjectID, allocation.RecordID).Scan(&target); err != nil || !target {
		t.Fatalf("pending cleanup target = %t, %v", target, err)
	}
	if event, err := cleanup.Next(ctx, actor, request.BrowserID); err != nil || event != nil {
		t.Fatalf("unissued cleanup event = %#v, %v", event, err)
	}

	other := actor
	other.UserID = "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
	if got, err := cleanup.Reconcile(ctx, other, request); !errors.Is(err, ErrRecordSubjectUnavailable) || len(got.Invalidate) != 0 {
		t.Fatalf("other actor allocation = %#v, %v", got, err)
	}
	wrong := requestFor(allocation)
	last := byte('0')
	if allocation.RecordID[len(allocation.RecordID)-1] == last {
		last = '1'
	}
	wrong.AllocationReferences[0].RecordID = allocation.RecordID[:len(allocation.RecordID)-1] + string(last)
	if got, err := cleanup.Reconcile(ctx, actor, wrong); !errors.Is(err, recordauth.ErrDenied) || len(got.Invalidate) != 0 {
		t.Fatalf("mismatched allocation = %#v, %v", got, err)
	}
	missing := requestFor(allocation)
	missing.AllocationReferences[0].AllocationID = "rqa_ffffffffffffffffffffffffffffffff"
	if got, err := cleanup.Reconcile(ctx, actor, missing); !errors.Is(err, ErrRecordSubjectUnavailable) || len(got.Invalidate) != 0 {
		t.Fatalf("missing allocation = %#v, %v", got, err)
	}
	closed := NewPostgresRecordClientCleanupRepository(runtime, AdmissionGateFunc(func(context.Context, pgx.Tx) error {
		return ErrRecordPlatformAdmissionUnavailable
	}), current)
	if got, err := closed.Reconcile(ctx, actor, request); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) || len(got.Invalidate) != 0 {
		t.Fatalf("unknown admission = %#v, %v", got, err)
	}

	prepared := reserve("cleanup-prepared")
	if _, err := birth.PrepareCreate(ctx, prepared.Request, birthTestCreation(prepared, "rrv_cleanupprepared")); err != nil {
		t.Fatal(err)
	}
	got, err = cleanup.Reconcile(ctx, actor, requestFor(prepared))
	assertResult(got, err, empty, []string{prepared.RecordID}, empty)

	// Commit the original allocation through the production revision service.
	// Its restricted visibility makes group removal an actual read revocation.
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		NewVPSRecordSubjectAdapter(NewPostgresVPSAssetRepository(runtime)),
		NewMonitoringInstanceRecordSubjectAdapter(NewPostgresMonitoringInstanceRepository(runtime)),
		NewTargetRecordSubjectAdapter(NewPostgresTargetRepository(runtime)),
	})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewPostgresRecordRepository(runtime, gate, nil, current, nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := records.NewRevisionService(subjects, current, repository, birth)
	if err != nil {
		t.Fatal(err)
	}
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindRestricted,
		ProjectID: actor.ProjectID, AllowedGroupIDs: []string{"rag_cleanup"},
		PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	preparation, err := evidence.NewRevisionPreparation(allocation.RecordID, evidence.RevisionPreparationValues{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = revisions.SaveRevision(ctx, records.RevisionSaveRequest{
		Actor: actor, RecordID: allocation.RecordID, Allocation: &allocation,
		Values: records.CompleteRevisionValues{
			Title: "Cleanup", BodyMarkdown: "# Cleanup\n", MarkdownDialectVersion: records.MarkdownDialectVersionV1,
			RecordType: records.RecordTypeNote, ImpactLevel: "informational", VisibilityScope: visibility,
		},
		SubjectReferences: []records.SubjectReference{{
			RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
			Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true,
		}},
		EvidencePreparation: preparation, ActivityKind: records.DomainActivityRecordCreated,
		IdempotencyKey: "cleanup-create", IdempotencyOwnerID: "cleanup-fixture",
		OwnerLeaseDuration: time.Minute, IdempotencyTTL: time.Hour, OutboxTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("create allocated record: %v", err)
	}
	got, err = cleanup.Reconcile(ctx, actor, request)
	assertResult(got, err, []string{allocation.RecordID}, empty, empty)
	ambiguous := NewPostgresRecordClientCleanupRepository(runtime, gate, cleanupAuthorizationFunc(func(context.Context, recordauth.ActorScope, string) (records.CurrentRecordAuthorization, error) {
		return records.CurrentRecordAuthorization{}, records.ErrRecordDeletionReserved
	}))
	if got, err := ambiguous.Reconcile(ctx, actor, request); !errors.Is(err, ErrRecordSubjectUnavailable) || len(got.Invalidate) != 0 {
		t.Fatalf("unsubstantiated read fence = %#v, %v", got, err)
	}
	mixed := requestFor(prepared)
	mixed.RecordIDs = []string{allocation.RecordID, "rec_cleanupmissing", "rec_cleanupmissing"}
	mixed.AllocationReferences = append(mixed.AllocationReferences, mixed.AllocationReferences[0])
	got, err = cleanup.Reconcile(ctx, actor, mixed)
	assertResult(got, err, []string{allocation.RecordID}, []string{prepared.RecordID}, []string{"rec_cleanupmissing"})

	if _, err := runtime.Exec(ctx, `delete from public.record_access_group_members
		where group_id='rag_cleanup' and user_id=$1`, actor.UserID); err != nil {
		t.Fatal(err)
	}
	// The passed actor still has the old group; the repository must use current membership.
	got, err = cleanup.Reconcile(ctx, actor, request)
	assertResult(got, err, empty, empty, []string{allocation.RecordID})
	got, err = cleanup.Reconcile(ctx, actor, requestFor(prepared))
	assertResult(got, err, empty, []string{prepared.RecordID}, empty)
	if _, err := fixture.db.Exec(ctx, `update public.users set disabled_at=now() where user_id=$1`, actor.UserID); err != nil {
		t.Fatal(err)
	}
	if got, err := cleanup.Reconcile(ctx, actor, requestFor(prepared)); !errors.Is(err, recordauth.ErrDenied) || len(got.Pending) != 0 {
		t.Fatalf("disabled member retained allocation = %#v, %v", got, err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.users set disabled_at=null where user_id=$1`, actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Exec(ctx, `insert into public.record_access_group_members(group_id,user_id)
		values('rag_cleanup',$1)`, actor.UserID); err != nil {
		t.Fatal(err)
	}
	got, err = cleanup.Reconcile(ctx, actor, request)
	assertResult(got, err, []string{allocation.RecordID}, empty, empty)

	// A real root deletion invokes the birth tombstone trigger. This is not an
	// issued deletion gate and is not presented as cleanup-event acceptance.
	tombstone := reserve("cleanup-tombstone")
	creation := birthTestCreation(tombstone, "rrv_cleanuptombstone")
	permit, err := birth.PrepareCreate(ctx, tombstone.Request, creation)
	if err != nil {
		t.Fatal(err)
	}
	tx := birthWriterTx(t, ctx, runtime, gate, tombstone.RecordID, nil)
	if err := consumeRecordBirthOnTx(ctx, tx, tombstone.Request, permit); err != nil {
		t.Fatal(err)
	}
	birthInsertFirstRevision(t, ctx, tx, creation)
	if err := finishRecordBirthOnTx(ctx, tx, permit); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// Remove the root after the restricted lookup returned finished. The
	// in-flight observation must remain unavailable; a later lookup sees the
	// actual tombstone and may invalidate it.
	racing := NewPostgresRecordClientCleanupRepository(runtime, gate, cleanupAuthorizationFunc(func(context.Context, recordauth.ActorScope, string) (records.CurrentRecordAuthorization, error) {
		birthFixtureRemoveRecord(t, ctx, fixture.db, creation)
		return records.CurrentRecordAuthorization{}, records.ErrRecordNotFound
	}))
	if got, err := racing.Reconcile(ctx, actor, requestFor(tombstone)); !errors.Is(err, ErrRecordSubjectUnavailable) || len(got.Invalidate) != 0 {
		t.Fatalf("finished allocation with disappearing root = %#v, %v", got, err)
	}
	got, err = cleanup.Reconcile(ctx, actor, requestFor(tombstone))
	assertResult(got, err, empty, empty, []string{tombstone.RecordID})
}

type cleanupAuthorizationFunc func(context.Context, recordauth.ActorScope, string) (records.CurrentRecordAuthorization, error)

func (authorize cleanupAuthorizationFunc) ResolveCurrentRecordAuthorization(ctx context.Context, actor recordauth.ActorScope, recordID string) (records.CurrentRecordAuthorization, error) {
	return authorize(ctx, actor, recordID)
}
