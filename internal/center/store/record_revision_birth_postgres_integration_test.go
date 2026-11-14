package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/records"
)

// The first real W2 confirmation commits, then its response is lost to APP.
type revisionBirthConfirmUnavailable struct {
	recordbirth.Authority
	calls int
}

func (authority *revisionBirthConfirmUnavailable) Confirm(ctx context.Context, binding recordbirth.Binding, reservationID string, commitment [32]byte) ([]byte, error) {
	authority.calls++
	if authority.calls == 1 {
		if _, err := authority.Authority.Confirm(ctx, binding, reservationID, commitment); err != nil {
			return nil, err
		}
		return nil, recordbirth.ErrUnavailable
	}
	return authority.Authority.Confirm(ctx, binding, reservationID, commitment)
}

func TestPostgresIntegrationRevisionBirthConsumptionAndReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	fixture := newSourceRetirementPGFixture(t, ctx)
	fixture.refresh()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_000000000000000000000001", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	vps, _ := fixture.newVPS(t, ctx, "revision birth source")
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		NewVPSRecordSubjectAdapter(NewPostgresVPSAssetRepository(fixture.runtime)),
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewRecordSubjectReadResolver(subjects, fixture.tombstoneReader(t))
	current := NewPostgresCurrentRecordAuthorizationSource(fixture.runtime, resolver, fixture.gate)
	repository, err := NewPostgresRecordRepository(fixture.runtime, fixture.gate, nil, current, nil)
	if err != nil {
		t.Fatal(err)
	}
	app := NewPostgresRecordBirthRepository(fixture.runtime, fixture.gate, nil)
	ordinary, err := recordbirth.NewCoordinator(app, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := records.NewRevisionService(subjects, current, repository, ordinary)
	if err != nil {
		t.Fatal(err)
	}
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	allocate := func(coordinator *recordbirth.Coordinator, key string) (recordbirth.Allocation, records.RevisionSaveRequest) {
		t.Helper()
		request := recordbirth.Request{
			ProjectID: string(actor.ProjectID), ActorID: actor.UserID, ActorScopeDigest: actor.CanonicalHash(),
			Purpose: recordbirth.PurposeCreate, IdempotencyKey: key,
			RequestFingerprint: sha256.Sum256([]byte("revision-birth-allocation:" + key)),
		}
		allocation, err := coordinator.Reserve(ctx, request)
		if err != nil {
			t.Fatalf("reserve %s: %v", key, err)
		}
		save := records.RevisionSaveRequest{
			Actor: actor, RecordID: allocation.RecordID, Allocation: &allocation,
			Values: records.CompleteRevisionValues{
				Title: key, BodyMarkdown: "# " + key + "\n", MarkdownDialectVersion: records.MarkdownDialectVersionV1,
				RecordType: records.RecordTypeNote, ImpactLevel: "informational", VisibilityScope: visibility,
			},
			SubjectReferences: []records.SubjectReference{{
				RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
				Role: records.RelationRoleAffected, SourceID: vps.VPSID, Primary: true,
			}},
			ActivityKind: records.DomainActivityRecordCreated, IdempotencyKey: key,
			IdempotencyOwnerID: "revision-birth-test", OwnerLeaseDuration: time.Minute,
			IdempotencyTTL: 24 * time.Hour, OutboxTTL: 24 * time.Hour,
		}
		return allocation, save
	}

	firstAllocation, firstRequest := allocate(ordinary, "revision-birth-ordinary")
	if firstAllocation.Domain != recordidentity.Ordinary {
		t.Fatalf("ordinary reservation has domain %v", firstAllocation.Domain)
	}
	first, err := service.SaveRevision(ctx, firstRequest)
	if err != nil || first.RecordID != firstAllocation.RecordID || !first.Created || first.Replayed || first.CertificationPending {
		t.Fatalf("ordinary create = %+v, %v", first, err)
	}
	replay, err := service.SaveRevision(ctx, firstRequest)
	if err != nil || !replay.Replayed || replay.RecordID != first.RecordID || replay.RevisionID != first.RevisionID || replay.CertificationPending {
		t.Fatalf("ordinary replay = %+v, %v", replay, err)
	}
	committed, err := app.CommittedCreation(ctx, first.RecordID)
	if err != nil || committed.State != recordbirth.StateFinished || committed.Creation.RevisionID != first.RevisionID {
		t.Fatalf("ordinary committed birth = %+v, %v", committed, err)
	}
	changed := firstRequest
	changed.Values.Title = "different body"
	if _, err := service.SaveRevision(ctx, changed); err == nil {
		t.Fatal("same creation identity accepted a different canonical body")
	}

	unreserved := firstRequest
	unreserved.RecordID, unreserved.Allocation, unreserved.IdempotencyKey = "rec_legacyunreserved", nil, "revision-birth-legacy"
	if _, err := service.SaveRevision(ctx, unreserved); !errors.Is(err, records.ErrInvalidRevisionCommand) {
		t.Fatalf("new legacy record without reservation: %v", err)
	}
	var legacyExists bool
	if err := fixture.app.db.QueryRow(ctx, `select exists(select 1 from public.records where record_id=$1)`, unreserved.RecordID).Scan(&legacyExists); err != nil || legacyExists {
		t.Fatalf("unreserved legacy root exists=%v, error=%v", legacyExists, err)
	}

	allocationA, requestA := allocate(ordinary, "revision-birth-batch-a")
	allocationB, requestB := allocate(ordinary, "revision-birth-batch-b")
	wrong := requestA
	wrong.Allocation = &allocationB
	if _, err := service.SaveRevision(ctx, wrong); !errors.Is(err, records.ErrInvalidRevisionServiceRequest) {
		t.Fatalf("different allocation accepted: %v", err)
	}
	missing := requestA
	missing.Allocation = nil
	if _, err := service.SaveRevision(ctx, missing); !errors.Is(err, records.ErrInvalidRevisionServiceRequest) {
		t.Fatalf("reserved target accepted without its allocation: %v", err)
	}
	withoutBirth, err := records.NewRevisionService(subjects, current, repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withoutBirth.SaveRevision(ctx, requestA); !errors.Is(err, records.ErrInvalidRevisionServiceRequest) {
		t.Fatalf("new record accepted without birth coordinator: %v", err)
	}
	if requestA.RecordID < requestB.RecordID {
		requestA, requestB = requestB, requestA
	}
	batch, err := service.SaveRevisions(ctx, []records.RevisionSaveRequest{requestA, requestA, requestB})
	if err != nil || len(batch) != 3 || !batch[0].Created || batch[0].Replayed || !batch[1].Replayed ||
		batch[0].RecordID != requestA.RecordID || batch[1].RevisionID != batch[0].RevisionID ||
		batch[2].RecordID != requestB.RecordID || !batch[2].Created {
		t.Fatalf("ordered batch = %+v, %v", batch, err)
	}
	if allocationA.RecordID == allocationB.RecordID {
		t.Fatal("distinct durable allocations reused a record identity")
	}

	realAuthority, reader, _ := birthTestAuthority(t, ctx, fixture)
	// Installing birth reopens the runtime with the current launch binding.
	// Rebuild services that held the pre-installation pool.
	subjects, err = records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		NewVPSRecordSubjectAdapter(NewPostgresVPSAssetRepository(fixture.runtime)),
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver = NewRecordSubjectReadResolver(subjects, fixture.tombstoneReader(t))
	current = NewPostgresCurrentRecordAuthorizationSource(fixture.runtime, resolver, fixture.gate)
	app = NewPostgresRecordBirthRepository(fixture.runtime, fixture.gate, nil)
	unavailable := &revisionBirthConfirmUnavailable{Authority: realAuthority}
	certified, err := recordbirth.NewCoordinator(app, reader, unavailable)
	if err != nil {
		t.Fatal(err)
	}
	certifiedRepository, err := NewPostgresRecordRepository(fixture.runtime, fixture.gate, nil, current, certified)
	if err != nil {
		t.Fatal(err)
	}
	certifiedService, err := records.NewRevisionService(subjects, current, certifiedRepository, certified)
	if err != nil {
		t.Fatal(err)
	}
	certAllocation, certRequest := allocate(certified, "revision-birth-certifiable")
	if certAllocation.Domain != recordidentity.Certifiable {
		t.Fatalf("certifiable reservation has domain %v", certAllocation.Domain)
	}
	certFirst, err := certifiedService.SaveRevision(ctx, certRequest)
	if err != nil || !certFirst.Created || certFirst.Replayed || !certFirst.CertificationPending || certFirst.RecordID != certAllocation.RecordID {
		t.Fatalf("committed create with unavailable confirmation = %+v, %v", certFirst, err)
	}
	pending, err := app.CommittedCreation(ctx, certFirst.RecordID)
	if err != nil || pending.State != recordbirth.StateFinished || pending.Creation.RevisionID != certFirst.RevisionID {
		t.Fatalf("pending committed birth = %+v, %v", pending, err)
	}
	if _, err := certified.ReadCertification(ctx, certFirst.RecordID); err == nil {
		t.Fatalf("pending birth unexpectedly certified: %v", err)
	}
	var createdEvents int
	if err := fixture.witnessDB.QueryRow(ctx, `select count(*) from public.record_birth_events where reservation_id=$1 and kind='created'`, certAllocation.ReservationID).Scan(&createdEvents); err != nil || createdEvents != 1 {
		t.Fatalf("lost W2 response did not preserve one created event: count=%d, error=%v", createdEvents, err)
	}
	certReplay, err := certifiedService.SaveRevision(ctx, certRequest)
	if err != nil || !certReplay.Replayed || certReplay.CertificationPending || certReplay.RecordID != certFirst.RecordID || certReplay.RevisionID != certFirst.RevisionID {
		t.Fatalf("confirmation retry = %+v, %v", certReplay, err)
	}
	confirmed, err := app.CommittedCreation(ctx, certFirst.RecordID)
	if err != nil || confirmed.State != recordbirth.StateConfirmed || unavailable.calls != 2 {
		t.Fatalf("confirmed birth = %+v, calls=%d, %v", confirmed, unavailable.calls, err)
	}
	if _, err := certified.ReadCertification(ctx, certFirst.RecordID); err != nil {
		t.Fatalf("confirmed birth did not verify: %v", err)
	}
	if err := fixture.witnessDB.QueryRow(ctx, `select count(*) from public.record_birth_events where reservation_id=$1 and kind='created'`, certAllocation.ReservationID).Scan(&createdEvents); err != nil || createdEvents != 1 {
		t.Fatalf("confirmation replay duplicated W2 creation: count=%d, error=%v", createdEvents, err)
	}
	var roots, revisions int
	if err := fixture.app.db.QueryRow(ctx, `select (select count(*) from public.records where record_id=$1),
		(select count(*) from public.record_revisions where record_id=$1)`, certFirst.RecordID).Scan(&roots, &revisions); err != nil || roots != 1 || revisions != 1 {
		t.Fatalf("confirmation retry created extra rows: roots=%d revisions=%d error=%v", roots, revisions, err)
	}
}
