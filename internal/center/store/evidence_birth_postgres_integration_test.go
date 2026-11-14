package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

// The kind supplies deterministic source observations; all allocation,
// admission, source reauthorization, payload persistence and birth operations
// still use the real direct-login repositories.
type evidenceBirthTestKind struct {
	descriptor evidence.Descriptor
	capture    evidence.PreparedCapture
}

func (kind evidenceBirthTestKind) Descriptor() evidence.Descriptor { return kind.descriptor }
func (evidenceBirthTestKind) ValidateSelection(context.Context, evidence.ActorScope, evidence.Selection) error {
	return nil
}
func (evidenceBirthTestKind) Summarize(evidence.CanonicalSnapshot) evidence.Summary {
	return evidence.Summary{}
}
func (evidenceBirthTestKind) Compare(evidence.CanonicalSnapshot, evidence.CanonicalSnapshot, evidence.Alignment) evidence.Comparison {
	return evidence.Comparison{}
}
func (evidenceBirthTestKind) Export(evidence.CanonicalSnapshot, evidence.ExportMode) evidence.ExportMaterial {
	return evidence.ExportMaterial{}
}

func (kind evidenceBirthTestKind) PreviewCapture(_ context.Context, _ evidence.ActorScope, selection evidence.Selection) (evidence.Preview, error) {
	if selection.SourceID != kind.capture.Intent().Selection.SourceID {
		return evidence.Preview{}, evidence.ErrSourceNotFound
	}
	return kind.capture.Preview(), nil
}

func (kind evidenceBirthTestKind) Authorize(context.Context, evidence.ActorScope, evidence.Selection) (evidence.AuthorizationScope, error) {
	return kind.capture.Authorization(), nil
}

func (kind evidenceBirthTestKind) Capture(_ context.Context, _ evidence.ActorScope, intent evidence.Intent) (evidence.CanonicalSnapshot, error) {
	if intent.ID != kind.capture.Intent().ID {
		return evidence.CanonicalSnapshot{}, evidence.ErrCaptureIntentUnavailable
	}
	return kind.capture.Snapshot(), nil
}

func evidenceBirthTestApplications(t *testing.T, pool *pgxpool.Pool, gate AdmissionGate, witness *WitnessedRecordSubjectTombstoneReader, capture evidence.PreparedCapture, continuity recordbirth.ContinuitySource) (*evidence.Service, *evidence.RevisionPreparer, *PostgresRecordRepository, *PostgresEvidenceRepository) {
	t.Helper()
	live := transactionRecordSubjectSource{db: pool}
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		newVPSRecordSubjectAdapter(live), newMonitoringInstanceRecordSubjectAdapter(live), newTargetRecordSubjectAdapter(live),
	})
	if err != nil {
		t.Fatal(err)
	}
	current := NewPostgresCurrentRecordAuthorizationSource(pool, NewRecordSubjectReadResolver(subjects, witness), gate)
	repository := NewPostgresEvidenceRepository(pool, gate, continuity)
	repository.current, repository.subjects = current, current.resolver
	kind := evidenceBirthTestKind{descriptor: storeEvidenceParticipantDescriptor(), capture: capture}
	registry, err := evidence.NewRegistry([]evidence.Kind{kind})
	if err != nil {
		t.Fatal(err)
	}
	repository.registry = registry
	capacity, err := evidence.NewCapacityEnforcer(evidence.DefaultCapacityPolicy(), repository)
	if err != nil {
		t.Fatal(err)
	}
	service, err := evidence.NewService(registry, repository, repository, capacity)
	if err != nil {
		t.Fatal(err)
	}
	preparer, err := evidence.NewRevisionPreparer(registry, repository, repository, repository, capacity)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := NewPostgresRecordRepository(pool, gate, []records.RevisionParticipant{NewRecordEvidenceRevisionParticipant()}, current, continuity)
	if err != nil {
		t.Fatal(err)
	}
	return service, preparer, revisions, repository
}

func evidenceBirthTestSource(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, `insert into public.targets(target_id,name,target_type,host,run_status)
		values ('tg_0123456789abcdef','Evidence target','hostname','evidence.invalid','启用') on conflict(target_id) do nothing`); err != nil {
		t.Fatal(err)
	}
}

func evidenceBirthTestRequest(actor recordauth.ActorScope, key string, purpose recordbirth.Purpose) recordbirth.Request {
	return recordbirth.Request{
		ProjectID: string(actor.ProjectID), ActorID: actor.UserID, ActorScopeDigest: actor.CanonicalHash(),
		Purpose: purpose, IdempotencyKey: key, RequestFingerprint: sha256.Sum256([]byte("evidence-birth:" + key)),
	}
}

func evidenceBirthTestCommit(t *testing.T, ctx context.Context, coordinator *recordbirth.Coordinator, revisions *PostgresRecordRepository, allocation recordbirth.Allocation, preparation evidence.RevisionPreparation) records.RevisionCommitResult {
	t.Helper()
	command := recordEvidenceParticipantCommand(t, recordplatform.OperationKindRecordCreate, allocation.RecordID, "", 0, 0,
		"Reserved evidence birth", allocation.Request.IdempotencyKey, preparation)
	revisionID, err := command.RevisionID()
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := command.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatal(err)
	}
	creation := recordbirth.InitialCreation{ReservationID: allocation.ReservationID, RecordID: allocation.RecordID,
		RevisionID: revisionID, AuthorID: command.Actor.UserID, CanonicalHash: command.Input.CanonicalHash(), RequestFingerprint: fingerprint}
	permit, err := coordinator.PrepareCreate(ctx, allocation.Request, creation)
	if err != nil {
		t.Fatal(err)
	}
	command.BirthRequest, command.BirthPermit = allocation.Request, permit
	result, err := revisions.CommitRevision(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.ConfirmCreate(ctx, allocation.RecordID); err != nil {
		t.Fatal(err)
	}
	return result
}

func exerciseEvidenceBirth(t *testing.T, ctx context.Context, admin, runtime *pgxpool.Pool, gate AdmissionGate, witness *WitnessedRecordSubjectTombstoneReader, coordinator *recordbirth.Coordinator, domain recordidentity.Domain) {
	t.Helper()
	actor := storeEvidenceParticipantActor(t)
	allocation, err := coordinator.Reserve(ctx, evidenceBirthTestRequest(actor, "evidence-birth-main", recordbirth.PurposeEvidence))
	if err != nil || allocation.Domain != domain {
		t.Fatalf("reserve evidence target = %+v, %v", allocation, err)
	}
	var now time.Time
	if err := admin.QueryRow(ctx, `select transaction_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	capture := storePreparedEvidenceCapture(t, allocation.RecordID, "evs_birthcapture", "evi_121212121212121212121212", now.UTC())
	service, preparer, revisions, _ := evidenceBirthTestApplications(t, runtime, gate, witness, capture, coordinator)
	request := evidence.CapturePreviewRequest{Actor: actor, AllocationID: allocation.AllocationID, RecordID: allocation.RecordID,
		SnapshotID: capture.SnapshotID(), Selection: capture.Intent().Selection}

	wrongPurpose, err := coordinator.Reserve(ctx, evidenceBirthTestRequest(actor, "evidence-birth-wrong-purpose", recordbirth.PurposeComparison))
	if err != nil {
		t.Fatal(err)
	}
	other, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault})
	if err != nil {
		t.Fatal(err)
	}
	changedScope, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: actor.UserID, Role: actor.Role, ProjectID: actor.ProjectID, GroupIDs: []string{"rag_scopechanged"}})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*evidence.CapturePreviewRequest){
		"missing reservation": func(r *evidence.CapturePreviewRequest) { r.AllocationID = "" },
		"unknown reservation": func(r *evidence.CapturePreviewRequest) { r.AllocationID = "rqa_ffffffffffffffffffffffffffffffff" },
		"wrong record":        func(r *evidence.CapturePreviewRequest) { r.RecordID = wrongPurpose.RecordID },
		"wrong actor":         func(r *evidence.CapturePreviewRequest) { r.Actor = other },
		"changed actor scope": func(r *evidence.CapturePreviewRequest) { r.Actor = changedScope },
		"wrong purpose": func(r *evidence.CapturePreviewRequest) {
			r.AllocationID, r.RecordID = wrongPurpose.AllocationID, wrongPurpose.RecordID
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			invalid := request
			mutate(&invalid)
			if _, err := service.CapturePreview(ctx, invalid); err == nil {
				t.Fatalf("%s accepted", name)
			}
			var exists bool
			if err := admin.QueryRow(ctx, `select exists(select 1 from public.evidence_capture_intents where intent_id=$1)`, capture.Intent().ID).Scan(&exists); err != nil || exists {
				t.Fatalf("%s wrote capture intent: exists=%t err=%v", name, exists, err)
			}
		})
	}
	if _, err := service.CapturePreview(ctx, request); err != nil {
		t.Fatalf("preview reserved capture: %v", err)
	}
	var linked string
	if err := admin.QueryRow(ctx, `select coalesce(birth_allocation_id,'') from public.evidence_capture_intents where intent_id=$1`, capture.Intent().ID).Scan(&linked); err != nil || linked != allocation.AllocationID {
		t.Fatalf("stored allocation = %q, %v", linked, err)
	}
	if _, err := preparer.Prepare(ctx, actor, evidence.RevisionPreparationRequest{AllocationID: wrongPurpose.AllocationID,
		RecordID: allocation.RecordID, Items: []evidence.RevisionPreparationItem{{CaptureIntentID: capture.Intent().ID}}}); !errors.Is(err, evidence.ErrInvalidCaptureIntentBinding) {
		t.Fatalf("capture accepted a different allocation: %v", err)
	}
	if _, err := runtime.Exec(ctx, `insert into public.evidence_payloads(payload_digest,canonical_size_bytes,compressed_size_bytes,compressed_payload)
		values(decode(repeat('11',32),'hex'),1,1,decode('00','hex'))`); err == nil {
		t.Fatal("runtime directly inserted an evidence payload")
	}
	route := evidenceCaptureFixtureRoute(capture, allocation.AllocationID)
	for name, mutate := range map[string]func(*evidencePayloadCommandRoute){
		"wrong allocation":   func(r *evidencePayloadCommandRoute) { r.AllocationID = wrongPurpose.AllocationID },
		"missing allocation": func(r *evidencePayloadCommandRoute) { r.AllocationID = "" },
		"wrong target":       func(r *evidencePayloadCommandRoute) { r.RecordID = wrongPurpose.RecordID },
		"wrong snapshot":     func(r *evidencePayloadCommandRoute) { r.SnapshotID = "evs_unrelated" },
		"wrong intent":       func(r *evidencePayloadCommandRoute) { r.IntentID = "evi_ffffffffffffffffffffffff" },
		"wrong actor": func(r *evidencePayloadCommandRoute) {
			r.ActorID = other.UserID
			r.ActorScopeDigest = fmt.Sprintf("%x", other.CanonicalHash())
		},
	} {
		t.Run("restricted payload "+name, func(t *testing.T) {
			attempt := route
			mutate(&attempt)
			tx, err := runtime.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err := persistEvidencePayloadOnTx(ctx, tx, attempt, capture.Snapshot()); err == nil {
				t.Fatalf("restricted payload accepted %s", name)
			}
		})
	}
	// SQL hashes the canonical input but deliberately does not decode gzip.
	// A malicious/malformed gzip therefore must fail the shared proof reader.
	t.Run("mismatched gzip cannot produce evidence proof", func(t *testing.T) {
		snapshot := capture.Snapshot()
		envelope := snapshot.Envelope()
		attempt := route
		attempt.SourceKind, attempt.SourceID = envelope.Source.Type, envelope.Source.ID
		attempt.SourceDigest = fmt.Sprintf("%x", envelope.SourceDigest)
		attempt.Kind, attempt.SchemaVersion = string(envelope.Key.Kind), fmt.Sprintf("%d", envelope.Key.SchemaVersion)
		attempt.PayloadDigest = fmt.Sprintf("%x", snapshot.Hash())
		metadata, err := json.Marshal(attempt)
		if err != nil {
			t.Fatal(err)
		}
		canonical := snapshot.Bytes()
		wrongCanonical := bytes.Replace(canonical, []byte(capture.Intent().ID), []byte("evi_ffffffffffffffffffffffff"), 1)
		if bytes.Equal(wrongCanonical, canonical) || len(wrongCanonical) != len(canonical) || !json.Valid(wrongCanonical) {
			t.Fatal("could not build equal-size valid mismatched canonical payload")
		}
		wrongGzip, err := deterministicEvidenceGzip(wrongCanonical)
		if err != nil {
			t.Fatal(err)
		}
		command := binary.BigEndian.AppendUint32(nil, uint32(len(metadata)))
		command = append(command, metadata...)
		command = binary.BigEndian.AppendUint32(command, uint32(len(canonical)))
		command = append(command, canonical...)
		command = append(command, wrongGzip...)
		tx, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `select public.record_evidence_payload_insert($1)`, command); err != nil {
			t.Fatalf("insert test gzip through restricted command: %v", err)
		}
		var stored []byte
		var canonicalSize, compressedSize int64
		digest := snapshot.Hash()
		if err := tx.QueryRow(ctx, `select compressed_payload,canonical_size_bytes,compressed_size_bytes from public.evidence_payloads where payload_digest=$1`, digest[:]).Scan(&stored, &canonicalSize, &compressedSize); err != nil {
			t.Fatal(err)
		}
		if _, err := decodeEvidencePayloadBytes(stored, canonicalSize, compressedSize, digest); !errors.Is(err, ErrEvidencePersistenceConflict) {
			t.Fatalf("mismatched gzip accepted by proof reader: %v", err)
		}
	})

	platform := NewPostgresRecordPlatformRepository(runtime, gate)
	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: allocation.RecordID}
	fence, err := platform.AcquireDeletionFenceLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "evidence_birth_fence", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	preparationRequest := evidence.RevisionPreparationRequest{AllocationID: allocation.AllocationID, RecordID: allocation.RecordID,
		Items: []evidence.RevisionPreparationItem{{CaptureIntentID: capture.Intent().ID}}}
	if _, err := preparer.Prepare(ctx, actor, preparationRequest); err == nil {
		t.Fatal("capture payload crossed an active deletion fence")
	}
	var exists bool
	digest := capture.Snapshot().Hash()
	if err := admin.QueryRow(ctx, `select exists(select 1 from public.evidence_payloads where payload_digest=$1)`, digest[:]).Scan(&exists); err != nil || exists {
		t.Fatalf("fenced payload exists=%t, err=%v", exists, err)
	}
	if err := platform.ReleaseDeletionFenceLease(ctx, object, fence.Owner); err != nil {
		t.Fatal(err)
	}
	preparation, err := preparer.Prepare(ctx, actor, preparationRequest)
	if err != nil {
		t.Fatalf("prepare reserved capture: %v", err)
	}
	tx, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := persistEvidencePayloadOnTx(ctx, tx, route, capture.Snapshot())
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("replay routed payload: %v", err)
	}
	second, err := persistEvidencePayloadOnTx(ctx, tx, route, capture.Snapshot())
	if err != nil || second != first {
		_ = tx.Rollback(ctx)
		t.Fatalf("second routed replay = (%+v, %v), want %+v", second, err, first)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := evidenceBirthTestCommit(t, ctx, coordinator, revisions, allocation, preparation)
	if result.RecordID != allocation.RecordID || result.RevisionID == "" {
		t.Fatalf("created root = %+v", result)
	}
	if err := admin.QueryRow(ctx, `select exists(select 1 from public.evidence_snapshots where record_id=$1 and snapshot_id=$2)
		and not exists(select 1 from public.evidence_capture_intents where intent_id=$3)`, result.RecordID, capture.SnapshotID(), capture.Intent().ID).Scan(&exists); err != nil || !exists {
		t.Fatalf("committed capture linkage = %t, %v", exists, err)
	}
	if _, err := service.CapturePreview(ctx, request); err == nil {
		t.Fatal("consumed allocation authorized another pre-root preview")
	}
	existing := storePreparedEvidenceCapture(t, result.RecordID, "evs_birthexisting", "evi_565656565656565656565656", now.Add(time.Second).UTC())
	existingService, existingPreparer, _, _ := evidenceBirthTestApplications(t, runtime, gate, witness, existing, coordinator)
	if _, err := existingService.CapturePreview(ctx, evidence.CapturePreviewRequest{Actor: actor, RecordID: result.RecordID,
		SnapshotID: existing.SnapshotID(), Selection: existing.Intent().Selection}); err != nil {
		t.Fatalf("preview existing root: %v", err)
	}
	if _, err := existingPreparer.Prepare(ctx, actor, evidence.RevisionPreparationRequest{RecordID: result.RecordID,
		Items: []evidence.RevisionPreparationItem{{CaptureIntentID: existing.Intent().ID}}}); err != nil {
		t.Fatalf("prepare existing-root capture: %v", err)
	}
}

func TestPostgresIntegrationEvidenceOrdinaryBirthPreviewPayloadRoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	evidenceBirthTestSource(t, ctx, fixture.platform.db)
	witness, err := NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, fixture.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	exerciseEvidenceBirth(t, ctx, fixture.platform.db, fixture.Runtime, fixture.Gate, witness, coordinator, recordidentity.Ordinary)
}

func TestPostgresIntegrationEvidenceCertifiedBirthPreviewPayloadRoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	fixture := newSourceRetirementPGFixture(t, ctx)
	fixture.refresh()
	for index, userID := range []string{"usr_aaaaaaaaaaaaaaaaaaaaaaaa", "usr_bbbbbbbbbbbbbbbbbbbbbbbb"} {
		if _, err := fixture.app.db.Exec(ctx, `insert into public.users(user_id,username,password_hash,display_name,role) values($1,$2,'test-hash',$2,'admin')`, userID, fmt.Sprintf("evidence-birth-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.app.db.Exec(ctx, `insert into public.vps_assets(vps_id,display_name,lifecycle_status,usage_status)
		values($1,'Evidence VPS','active','in_use') on conflict(vps_id) do nothing`, testStoreRecordVPSID); err != nil {
		t.Fatal(err)
	}
	evidenceBirthTestSource(t, ctx, fixture.app.db)
	authority, reader, _ := birthTestAuthority(t, ctx, fixture)
	coordinator, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.runtime, fixture.gate, nil), reader, authority)
	if err != nil {
		t.Fatal(err)
	}
	exerciseEvidenceBirth(t, ctx, fixture.app.db, fixture.runtime, fixture.gate, fixture.tombstoneReader(t), coordinator, recordidentity.Certifiable)

	actor := storeEvidenceParticipantActor(t)
	allocation, err := coordinator.Reserve(ctx, evidenceBirthTestRequest(actor, "evidence-birth-stale-binding", recordbirth.PurposeEvidence))
	if err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := fixture.app.db.QueryRow(ctx, `select transaction_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	capture := storePreparedEvidenceCapture(t, allocation.RecordID, "evs_birthstale", "evi_343434343434343434343434", now.UTC())
	service, _, _, _ := evidenceBirthTestApplications(t, fixture.runtime, fixture.gate, fixture.tombstoneReader(t), capture, coordinator)
	if _, err := fixture.app.db.Exec(ctx, `delete from record_platform_internal.record_birth_enabled where singleton`); err != nil {
		t.Fatal(err)
	}
	_, err = service.CapturePreview(ctx, evidence.CapturePreviewRequest{Actor: actor, AllocationID: allocation.AllocationID,
		RecordID: allocation.RecordID, SnapshotID: capture.SnapshotID(), Selection: capture.Intent().Selection})
	if err == nil {
		t.Fatal("stale certified allocation admitted pre-root preview")
	}
	var exists bool
	if err := fixture.app.db.QueryRow(ctx, `select exists(select 1 from public.evidence_capture_intents where intent_id=$1)`, capture.Intent().ID).Scan(&exists); err != nil || exists {
		t.Fatalf("stale binding wrote an intent: exists=%t, err=%v", exists, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stale binding check timed out: %v", err)
	}
}

type evidenceBirthCommitBarrierTx struct {
	pgx.Tx
	intentID string
	entered  chan struct{}
	release  <-chan struct{}
}

func (tx *evidenceBirthCommitBarrierTx) Commit(ctx context.Context) error {
	var intentWritten bool
	if err := tx.Tx.QueryRow(ctx, `select exists(select 1 from public.evidence_capture_intents where intent_id=$1)`, tx.intentID).Scan(&intentWritten); err != nil {
		return err
	}
	if intentWritten {
		close(tx.entered)
		select {
		case <-tx.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return tx.Tx.Commit(ctx)
}

func TestPostgresIntegrationEvidenceBirthWriterFirstAndRouteDrift(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	evidenceBirthTestSource(t, ctx, fixture.platform.db)
	writer := fixture.platform.openDirectRuntimePool(t, ctx, "evidence-birth-race-writer", 1)
	fencer := fixture.platform.openDirectRuntimePool(t, ctx, "evidence-birth-race-fence", 1)
	var writerPID, fencePID int
	if err := writer.QueryRow(ctx, `select pg_backend_pid()`).Scan(&writerPID); err != nil {
		t.Fatal(err)
	}
	if err := fencer.QueryRow(ctx, `select pg_backend_pid()`).Scan(&fencePID); err != nil {
		t.Fatal(err)
	}
	witness, err := NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, writer)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	actor := storeEvidenceParticipantActor(t)
	allocation, err := coordinator.Reserve(ctx, evidenceBirthTestRequest(actor, "evidence-birth-writer-first", recordbirth.PurposeEvidence))
	if err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := fixture.platform.db.QueryRow(ctx, `select transaction_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	first := storePreparedEvidenceCapture(t, allocation.RecordID, "evs_birthracefirst", "evi_787878787878787878787878", now.UTC())
	firstService, firstPreparer, _, _ := evidenceBirthTestApplications(t, writer, fixture.Gate, witness, first, coordinator)
	if _, err := firstService.CapturePreview(ctx, evidence.CapturePreviewRequest{Actor: actor, AllocationID: allocation.AllocationID,
		RecordID: allocation.RecordID, SnapshotID: first.SnapshotID(), Selection: first.Intent().Selection}); err != nil {
		t.Fatal(err)
	}
	second := storePreparedEvidenceCapture(t, allocation.RecordID, "evs_birthracesecond", "evi_909090909090909090909090", now.Add(time.Second).UTC())
	secondService, _, _, repository := evidenceBirthTestApplications(t, writer, fixture.Gate, witness, second, coordinator)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	begin := repository.platform.beginTx
	repository.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		tx, err := begin(ctx, options)
		if err != nil {
			return nil, err
		}
		return &evidenceBirthCommitBarrierTx{Tx: tx, intentID: second.Intent().ID, entered: entered, release: release}, nil
	}
	written := make(chan error, 1)
	go func() {
		_, err := secondService.CapturePreview(ctx, evidence.CapturePreviewRequest{Actor: actor, AllocationID: allocation.AllocationID,
			RecordID: allocation.RecordID, SnapshotID: second.SnapshotID(), Selection: second.Intent().Selection})
		written <- err
	}()
	select {
	case <-entered:
	case err := <-written:
		t.Fatalf("evidence writer returned before reaching the intent commit barrier: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: allocation.RecordID}
	platform := NewPostgresRecordPlatformRepository(fencer, fixture.Gate)
	type fenceOutcome struct {
		lease recordplatform.DeletionFenceLeaseV1
		err   error
	}
	fenced := make(chan fenceOutcome, 1)
	go func() {
		lease, err := platform.AcquireDeletionFenceLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "evidence_writer_first", LeaseDuration: time.Minute})
		fenced <- fenceOutcome{lease: lease, err: err}
	}()
	waitForPostgresBlockingPID(t, ctx, fixture.platform.db, fencePID, writerPID)
	unblock()
	if err := waitForRecordPlatformResult(t, written, "evidence writer before fence"); err != nil {
		t.Fatal(err)
	}
	result := waitForRecordPlatformResult(t, fenced, "fence after evidence writer")
	if result.err != nil {
		t.Fatal(result.err)
	}
	if err := platform.ReleaseDeletionFenceLease(ctx, object, result.lease.Owner); err != nil {
		t.Fatal(err)
	}

	holder, err := fixture.platform.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	var holderPID int
	if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `select 1 from public.evidence_capture_intents where intent_id=$1 for update`, first.Intent().ID); err != nil {
		t.Fatal(err)
	}
	prepared := make(chan error, 1)
	go func() {
		_, err := firstPreparer.Prepare(ctx, actor, evidence.RevisionPreparationRequest{AllocationID: allocation.AllocationID,
			RecordID: allocation.RecordID, Items: []evidence.RevisionPreparationItem{{CaptureIntentID: first.Intent().ID}}})
		prepared <- err
	}()
	waitForPostgresBlockingPID(t, ctx, fixture.platform.db, writerPID, holderPID)
	if _, err := holder.Exec(ctx, `update public.evidence_capture_intents set source_digest=decode(repeat('ff',32),'hex') where intent_id=$1`, first.Intent().ID); err != nil {
		t.Fatal(err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	err = waitForRecordPlatformResult(t, prepared, "drifted capture route")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
		t.Fatalf("changed capture route = %v, want serialization failure", err)
	}
	digest := first.Snapshot().Hash()
	var exists bool
	if err := fixture.platform.db.QueryRow(ctx, `select exists(select 1 from public.evidence_payloads where payload_digest=$1)`, digest[:]).Scan(&exists); err != nil || exists {
		t.Fatalf("drifted route persisted payload: exists=%t, err=%v", exists, err)
	}
}
