package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
	storemigrate "houfeng/internal/center/store/migrate"
)

func TestIssuerComparisonPreviewUsesObservedState(t *testing.T) {
	target := recorddeletion.PreviewTarget{
		Object:            recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_previewone"},
		CurrentRevisionID: "rrv_previewone", LockVersion: 3, AuthorizationEpoch: 4,
		ContentDeliveryEpoch: 5, DependencyGraphDigest: [32]byte{1},
	}
	observed := comparisonObservedState{
		IndexDigest: [32]byte{2}, material: []byte(`{"results": []}`), surviving: 1,
		identity: comparisonObservedIdentity{ProjectID: "default", RecordID: "rec_previewone", RevisionID: "rrv_previewone", LockVersion: 3, AuthorizationEpoch: 4, ContentEpoch: 5},
	}
	first, err := deriveComparisonPreviewFromObserved(observed, target)
	if err != nil || first.Validate() != nil || len(first.SurvivingCopies) != 1 {
		t.Fatalf("derive preview: %#v, %v", first, err)
	}
	target.DependencyGraphDigest = [32]byte{3}
	second, err := deriveComparisonPreviewFromObserved(observed, target)
	if err != nil || second.DependencyDigest == first.DependencyDigest || second.ImpactDigest != first.ImpactDigest {
		t.Fatalf("derive preview with changed graph: %#v, %v", second, err)
	}
	target.LockVersion++
	if _, err := deriveComparisonPreviewFromObserved(observed, target); !errors.Is(err, recorddeletion.ErrDeletionPreviewStale) {
		t.Fatalf("derive stale preview: %v", err)
	}
}

// RunV2Issuer is called by the external package so the real portability
// adapter can be constructed without introducing a store import cycle.
func RunV2Issuer(t *testing.T, newPortabilityAdapter func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error)) {
	t.Helper()
	t.Run("certified_birth", runV2IssuerCertifiedBirth)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	local, recordID, _, _, _, _, measured, creation := seedV2ProcessedWorkspace(t, ctx, fixture)
	runtime := local.pool
	workerPool := openV2LaunchBoundPool(t, ctx, fixture, fixture.deletionWorker, "v2-issuer-worker", measured)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workerPool); err != nil {
		t.Fatal(err)
	}
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(runtime, fixture.admissionGate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewPostgresRecordDeletionIssuer(NewPostgresRecordDeletionRepository(runtime, fixture.admissionGate),
		recordsPostgresActiveDeletionDeployment(t, ctx, fixture), birth, recorddeletion.UnavailableDeletionBackupInventorySource{},
		recorddeletion.ServiceOptions{OwnerID: "v2_issuer", OwnerLeaseDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	actor := creation.Actor
	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: recordID}
	previewRequest := recorddeletion.PreviewRequest{Actor: actor, RecordID: recordID}
	stale, err := issuer.Preview(ctx, previewRequest)
	if err != nil {
		t.Fatalf("first preview: %v", err)
	}
	staleToken, err := recordplatform.ParseDeletionRequestTokenTransportV1(stale.Token.Transport())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Exec(ctx, `insert into public.browser_record_cleanup_targets(user_id,browser_id,record_id,project_id) values($1,'issuer_drift',$2,'default')`, actor.UserID, recordID); err != nil {
		t.Fatal(err)
	}
	_, err = issuer.Execute(ctx, recorddeletion.ExecuteRequest{Actor: actor, RecordID: recordID, ReservationID: stale.ReservationID, Token: staleToken, ReasonCode: recorddeletion.DeletionReasonUserConfirmed})
	if !errors.Is(err, recorddeletion.ErrDeletionPreviewStale) {
		t.Fatalf("changed preview: %v", err)
	}
	preview, err := issuer.Preview(ctx, previewRequest)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if err := preview.Summary.Validate(); err != nil || preview.Summary.BackupInventory.Availability != recorddeletion.BackupInventoryUnavailable {
		t.Fatalf("preview disclosure: %#v, %v", preview.Summary, err)
	}
	token, err := recordplatform.ParseDeletionRequestTokenTransportV1(preview.Token.Transport())
	if err != nil {
		t.Fatal(err)
	}
	request := recorddeletion.ExecuteRequest{Actor: actor, RecordID: recordID, ReservationID: preview.ReservationID, Token: token, ReasonCode: recorddeletion.DeletionReasonUserConfirmed}
	wrongIssued, err := recordplatform.NewIssuedDeletionRequestTokenV1()
	if err != nil {
		t.Fatal(err)
	}
	wrongToken, err := recordplatform.ParseDeletionRequestTokenTransportV1(wrongIssued.Transport())
	if err != nil {
		t.Fatal(err)
	}
	wrong := request
	wrong.Token = wrongToken
	if _, err := issuer.Execute(ctx, wrong); !errors.Is(err, recorddeletion.ErrDeletionRequestTokenReused) {
		t.Fatalf("wrong token: %v", err)
	}
	wrong = request
	wrong.Actor.UserID = "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := issuer.Execute(ctx, wrong); err == nil {
		t.Fatal("wrong actor accepted")
	}
	wrong = request
	wrong.ReservationID = stale.ReservationID
	if _, err := issuer.Execute(ctx, wrong); !errors.Is(err, recorddeletion.ErrDeletionRequestTokenReused) {
		t.Fatalf("wrong reservation/token binding: %v", err)
	}
	servingRepository := NewPostgresRecordPlatformRepository(runtime, fixture.admissionGate)
	leaseInput := recordplatform.LeaseClaimInputV1{OwnerID: "issuer_serving", LeaseDuration: time.Minute}
	serving, err := servingRepository.AcquireServingLease(ctx, object, leaseInput)
	if err != nil {
		t.Fatal(err)
	}
	var oldEpoch int64
	if err := fixture.db.QueryRow(ctx, `select delivery_epoch from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1`, recordID).Scan(&oldEpoch); err != nil {
		t.Fatal(err)
	}
	if _, err := issuer.Execute(ctx, request); !errors.Is(err, recordplatform.ErrObjectContentLeaseLive) {
		t.Fatalf("live serving lease: %v", err)
	}
	var unchangedEpoch int64
	var gates int
	if err := fixture.db.QueryRow(ctx, `select e.delivery_epoch,(select count(*) from public.record_deletion_gates g where g.record_id=$1) from public.content_delivery_epochs e where e.project_id='default' and e.object_kind='record' and e.object_id=$1`, recordID).Scan(&unchangedEpoch, &gates); err != nil || unchangedEpoch != oldEpoch || gates != 0 {
		t.Fatalf("live lease changed epoch or gate: %d %d %v", unchangedEpoch, gates, err)
	}
	if err := servingRepository.ReleaseObjectContentLease(ctx, object, serving.Owner); err != nil {
		t.Fatal(err)
	}
	begin := issuer.repository.platform.beginTx
	dropped := false
	issuer.repository.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		tx, err := begin(ctx, options)
		if err != nil {
			return nil, err
		}
		if options.AccessMode != pgx.ReadOnly && !dropped {
			return &issuerCommitLossTx{Tx: tx, dropped: &dropped}, nil
		}
		return tx, nil
	}
	operation, err := issuer.Execute(ctx, request)
	issuer.repository.platform.beginTx = begin
	if !dropped {
		t.Fatal("issuer commit response was not lost")
	}
	if err != nil || operation.Validate() != nil || operation.State != recorddeletion.DeletionStateOnlinePurging {
		t.Fatalf("execute: %#v %v", operation, err)
	}
	if _, err := servingRepository.RenewServingLease(ctx, serving, time.Minute); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("old serving renewal: %v", err)
	}
	if err := servingRepository.AssertServingLease(ctx, serving); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("old serving assertion: %v", err)
	}
	if _, err := servingRepository.AcquireServingLease(ctx, object, leaseInput); !errors.Is(err, recordplatform.ErrDeletionFenceLeaseLive) {
		t.Fatalf("new serving lease behind issuer fence: %v", err)
	}
	replayed, err := issuer.Execute(ctx, request)
	if err != nil || replayed != operation {
		t.Fatalf("exact replay before purge: %#v %v", replayed, err)
	}
	var expiry time.Time
	if err := fixture.db.QueryRow(ctx, `select owner_expires_at from public.record_purge_operations where operation_id=$1`, operation.OperationID).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	waitServingTestFenceExpired(t, ctx, fixture, expiry)
	workerRepository := NewPostgresRecordDeletionRepository(workerPool, allowRecordPlatformAdmissionGate)
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(workerPool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := workerRepository.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	attachmentRepository := NewPostgresAttachmentRepository(workerPool)
	if err := attachmentRepository.ConfigureAttachmentDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	workerEvidence := NewPostgresEvidenceRepository(workerPool, allowRecordPlatformAdmissionGate, nil)
	runtimeEvidence := NewPostgresEvidenceRepository(runtime, allowRecordPlatformAdmissionGate, nil)
	comparison, err := NewPostgresComparisonDeletionStore(runtimeEvidence, workerEvidence)
	if err != nil {
		t.Fatal(err)
	}
	var adapters []recorddeletion.Adapter
	add := func(adapter recorddeletion.Adapter, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		adapters = append(adapters, adapter)
	}
	add(recorddeletion.NewCoreAdapter(workerRepository))
	add(attachments.NewDeletionAdapter(attachmentRepository))
	add(evidence.NewDeletionAdapter(workerEvidence))
	add(recorddeletion.NewMarkdownClientAdapter(workerRepository))
	add(recordsearch.NewDeletionAdapter(workerRepository))
	add(activity.NewDeletionAdapter(workerRepository))
	add(recorddeletion.NewComparisonAdapter(comparison))
	add(recordcollaboration.NewDeletionAdapter(workerRepository))
	add(newPortabilityAdapter(workerRepository))
	registry, err := recorddeletion.NewRegistry(adapters)
	if err != nil {
		t.Fatal(err)
	}
	worker := recorddeletion.NewV2DeletionWorker(workerRepository, recorddeletion.NewRegistryPurger(registry), recorddeletion.DeletionWorkerOptions{OwnerID: "issuer_worker", OwnerLeaseDuration: 2 * time.Minute})
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("V2 worker RunOnce: %v", err)
	}
	var state string
	var rootExists bool
	if err := fixture.db.QueryRow(ctx, `select operation_state,exists(select 1 from public.records where record_id=$2) from public.record_purge_operations where operation_id=$1`, operation.OperationID, recordID).Scan(&state, &rootExists); err != nil || state != "online_purged" || rootExists {
		t.Fatalf("worker completion: %q root=%t err=%v", state, rootExists, err)
	}
	if _, err := servingRepository.AcquireServingLease(ctx, object, leaseInput); !errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) {
		t.Fatalf("serving after core purge: %v", err)
	}
	replayed, err = issuer.Execute(ctx, request)
	if err != nil || replayed.OperationID != operation.OperationID || replayed.State != recorddeletion.DeletionStateOnlinePurged || replayed.ReceiptDigest == ([32]byte{}) {
		t.Fatalf("exact replay after purge: %#v %v", replayed, err)
	}
	wrong = request
	wrong.Token = wrongToken
	if _, err := issuer.Execute(ctx, wrong); !errors.Is(err, recorddeletion.ErrDeletionRequestTokenReused) {
		t.Fatalf("wrong token after purge: %v", err)
	}
	wrong = request
	wrong.Actor.UserID = "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := issuer.Execute(ctx, wrong); err == nil {
		t.Fatal("wrong actor accepted after purge")
	}
}

// This uses independent APP/W2 fixtures and direct runtime and witness roles.
// The first W2 confirmation commits but its response is lost, leaving a real
// record whose APP birth is still unconfirmed.
func runV2IssuerCertifiedBirth(t *testing.T) {
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
	vps, _ := fixture.newVPS(t, ctx, "issuer certified birth source")
	producer, reader, binding := birthTestAuthority(t, ctx, fixture)
	app := NewPostgresRecordBirthRepository(fixture.runtime, fixture.gate, nil)
	confirmLoss := &revisionBirthConfirmUnavailable{Authority: producer}
	birth, err := recordbirth.NewCoordinator(app, reader, confirmLoss)
	if err != nil {
		t.Fatal(err)
	}
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		NewVPSRecordSubjectAdapter(NewPostgresVPSAssetRepository(fixture.runtime)),
	})
	if err != nil {
		t.Fatal(err)
	}
	current := NewPostgresCurrentRecordAuthorizationSource(fixture.runtime, NewRecordSubjectReadResolver(subjects, fixture.tombstoneReader(t)), fixture.gate)
	repository, err := NewPostgresRecordRepository(fixture.runtime, fixture.gate, nil, current, birth)
	if err != nil {
		t.Fatal(err)
	}
	service, err := records.NewRevisionService(subjects, current, repository, birth)
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
	const key = "issuer-certified-birth"
	allocation, err := birth.Reserve(ctx, recordbirth.Request{
		ProjectID: string(actor.ProjectID), ActorID: actor.UserID, ActorScopeDigest: actor.CanonicalHash(),
		Purpose: recordbirth.PurposeCreate, IdempotencyKey: key,
		RequestFingerprint: sha256.Sum256([]byte("issuer-certified-birth-allocation")),
	})
	if err != nil || allocation.Domain != recordidentity.Certifiable {
		t.Fatalf("reserve certifiable birth: domain=%v error=%v", allocation.Domain, err)
	}
	created, err := service.SaveRevision(ctx, records.RevisionSaveRequest{
		Actor: actor, RecordID: allocation.RecordID, Allocation: &allocation,
		Values: records.CompleteRevisionValues{
			Title: "issuer certified birth", BodyMarkdown: "# issuer certified birth\n",
			MarkdownDialectVersion: records.MarkdownDialectVersionV1, RecordType: records.RecordTypeNote,
			ImpactLevel: "informational", VisibilityScope: visibility,
		},
		SubjectReferences: []records.SubjectReference{{
			RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
			Role: records.RelationRoleAffected, SourceID: vps.VPSID, Primary: true,
		}},
		ActivityKind: records.DomainActivityRecordCreated, IdempotencyKey: key,
		IdempotencyOwnerID: "issuer_birth_test", OwnerLeaseDuration: time.Minute,
		IdempotencyTTL: 24 * time.Hour, OutboxTTL: 24 * time.Hour,
	})
	if err != nil || !created.Created || !created.CertificationPending || created.RecordID != allocation.RecordID || confirmLoss.calls != 1 {
		t.Fatalf("create pending certifiable record: %+v confirmations=%d error=%v", created, confirmLoss.calls, err)
	}
	issuer, err := NewPostgresRecordDeletionIssuer(NewPostgresRecordDeletionRepository(fixture.runtime, fixture.gate),
		recordplatform.DeploymentID(fixture.deployment), birth, recorddeletion.UnavailableDeletionBackupInventorySource{},
		recorddeletion.ServiceOptions{OwnerID: "issuer_birth", OwnerLeaseDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := issuer.ConfigureWitness(fixture.reader); err != nil {
		t.Fatal(err)
	}
	previewRequest := recorddeletion.PreviewRequest{Actor: actor, RecordID: created.RecordID}
	assertUncertified := func(label string, err error) {
		t.Helper()
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "55000" || postgresError.Message != "birth is not confirmed" {
			t.Fatalf("%s: expected APP birth certification rejection, got %v", label, err)
		}
	}
	if _, err := issuer.Preview(ctx, previewRequest); err == nil {
		t.Fatal("preview accepted a record before APP birth confirmation")
	} else {
		assertUncertified("pending birth preview", err)
	}
	if err := birth.ConfirmCreate(ctx, created.RecordID); err != nil {
		t.Fatalf("confirm certifiable birth: %v", err)
	}
	if confirmLoss.calls != 2 {
		t.Fatalf("confirmation calls = %d, want 2", confirmLoss.calls)
	}
	observation, err := birth.ReadCertificationObservation(ctx, created.RecordID)
	if err != nil || observation.Binding != binding || len(observation.Created) == 0 {
		t.Fatalf("read real W2 birth observation: binding=%+v error=%v", observation.Binding, err)
	}
	preview, err := issuer.Preview(ctx, previewRequest)
	if err != nil {
		t.Fatalf("preview confirmed birth: %v", err)
	}
	token, err := recordplatform.ParseDeletionRequestTokenTransportV1(preview.Token.Transport())
	if err != nil {
		t.Fatal(err)
	}
	request := recorddeletion.ExecuteRequest{Actor: actor, RecordID: created.RecordID,
		ReservationID: preview.ReservationID, Token: token, ReasonCode: recorddeletion.DeletionReasonUserConfirmed}
	driftBirth, err := recordbirth.NewCoordinator(app, issuerBirthDriftReader{PostgresAuthority: reader}, producer)
	if err != nil {
		t.Fatal(err)
	}
	driftIssuer, err := NewPostgresRecordDeletionIssuer(NewPostgresRecordDeletionRepository(fixture.runtime, fixture.gate),
		recordplatform.DeploymentID(fixture.deployment), driftBirth, recorddeletion.UnavailableDeletionBackupInventorySource{},
		recorddeletion.ServiceOptions{OwnerID: "issuer_birth", OwnerLeaseDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := driftIssuer.ConfigureWitness(fixture.reader); err != nil {
		t.Fatal(err)
	}
	var beforeEpoch, afterEpoch int64
	if err := fixture.app.db.QueryRow(ctx, `select delivery_epoch from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1`, created.RecordID).Scan(&beforeEpoch); err != nil {
		t.Fatal(err)
	}
	if _, err := driftIssuer.Execute(ctx, request); err == nil {
		t.Fatal("execute accepted a mismatched W2 created receipt")
	} else {
		assertUncertified("drifted birth execute", err)
	}
	var gates int
	if err := fixture.app.db.QueryRow(ctx, `select e.delivery_epoch,(select count(*) from public.record_deletion_gates g where g.record_id=$1) from public.content_delivery_epochs e where e.project_id='default' and e.object_kind='record' and e.object_id=$1`, created.RecordID).Scan(&afterEpoch, &gates); err != nil || afterEpoch != beforeEpoch || gates != 0 {
		t.Fatalf("rejected birth observation changed epoch or gate: epoch=%d gates=%d error=%v", afterEpoch, gates, err)
	}
	operation, err := issuer.Execute(ctx, request)
	if err != nil || operation.Validate() != nil || operation.Object.ObjectID != created.RecordID || operation.State != recorddeletion.DeletionStateOnlinePurging {
		t.Fatalf("execute confirmed birth: operation=%+v error=%v", operation, err)
	}
}

// The real reader supplies continuity and observation. Only the received
// created receipt changes, exercising APP's independent comparison.
type issuerBirthDriftReader struct{ *recordbirth.PostgresAuthority }

func (reader issuerBirthDriftReader) Observe(ctx context.Context, binding recordbirth.Binding, recordID string) (recordbirth.Observation, error) {
	observation, err := reader.PostgresAuthority.Observe(ctx, binding, recordID)
	if err != nil || len(observation.Created) == 0 {
		return observation, err
	}
	observation.Created = append([]byte(nil), observation.Created...)
	observation.Created[len(observation.Created)-1] ^= 1
	return observation, nil
}

type issuerCommitLossTx struct {
	pgx.Tx
	dropped *bool
}

func (tx *issuerCommitLossTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	*tx.dropped = true
	return errors.New("test: committed issuer response lost")
}
