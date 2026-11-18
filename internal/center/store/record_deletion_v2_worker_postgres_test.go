package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
	storemigrate "houfeng/internal/center/store/migrate"
)

// openV2LaunchBoundPool uses the exact binding staged by the real processor
// launch fixture. The fixture's center binding names the measured processor
// test executable; this exercises database admission, not center bootstrap.
func openV2LaunchBoundPool(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, role, application string, measured [32]byte) *pgxpool.Pool {
	t.Helper()
	var raw []byte
	var opened bool
	var runtimePrincipal, workerPrincipal string
	if err := fixture.db.QueryRow(ctx, `select center_binding,opened,runtime_principal::text,worker_principal::text
		from record_platform_internal.record_birth_launch_state where singleton`).Scan(&raw, &opened, &runtimePrincipal, &workerPrincipal); err != nil {
		t.Fatalf("read staged runtime launch: %v", err)
	}
	launch, err := recordbirth.DecodeLaunchBinding(raw)
	if err != nil || !opened || runtimePrincipal != fixture.runtime || workerPrincipal != fixture.deletionWorker ||
		(role != runtimePrincipal && role != workerPrincipal) || launch.ProcessKind != "center" || launch.BinaryDigest != measured ||
		launch.BirthInstalled || launch.SourceGeneration != 0 {
		t.Fatalf("invalid staged runtime launch binding: %v", err)
	}
	config := fixture.directRoleConfig(t, role, application)
	config.MaxConns = 4
	digest := sha256.Sum256(raw)
	config.ConnConfig.RuntimeParams["houfeng.birth_launch_digest"] = hex.EncodeToString(digest[:])
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open launch-bound direct role: %v", err)
	}
	t.Cleanup(pool.Close)
	var sessionUser, currentUser string
	var generation int64
	if err := pool.QueryRow(ctx, `select session_user,current_user,public.source_application_admit()`).Scan(&sessionUser, &currentUser, &generation); err != nil || sessionUser != role || currentUser != role || generation != 0 {
		t.Fatalf("launch-bound direct admission user=%q/%q generation=%d: %v", sessionUser, currentUser, generation, err)
	}
	if role == fixture.runtime {
		if err := recordbirth.VerifyRuntimeLaunch(ctx, pool, nil, launch); err != nil {
			t.Fatalf("verify runtime birth continuity: %v", err)
		}
	}
	return pool
}

// seedV2ProcessedWorkspace runs the real processor in the existing isolated
// process fixture before publishing the draft as a record.
func seedV2ProcessedWorkspace(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) (*localPublicationFixture, string, string, int64, []byte, []byte, [32]byte, records.RevisionCommitCommand) {
	t.Helper()
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	writer, err := attachments.NewManagedBlobWriter(local.blob, attachments.BackendKindLocal, local.local)
	if err != nil {
		t.Fatal(err)
	}
	drafts := NewPostgresRecordDraftRepository(local.pool, fixture.admissionGate)
	draft := createRecordsPostgresDraft(t, ctx, drafts, "rdf_v2workspace", recordsPostgresDraftPayload(t, "V2 workspace"))
	content := []byte("V2 deletion processor workspace\n")
	reserve := draftReleaseReservation(draft.DraftID, 0)
	reserve.DeclaredSizeBytes = int64(len(content))
	if _, err := local.repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatal(err)
	}
	written := putManagedAttachmentUpload(t, ctx, local.repository, writer, reserve, content)
	enqueueManagedAttachmentUpload(t, ctx, local.repository, reserve, written, "apj_v2workspace")
	workspaceRoot := filepath.Join(t.TempDir(), "processor-root")
	processorBinary := buildContentProcessorCrashHelper(t)
	measured, err := recordbirth.DigestExecutable(processorBinary)
	if err != nil {
		t.Fatalf("measure processor fixture: %v", err)
	}
	container, deploymentID := newContentProcessorLaunchFixture(t, ctx, fixture, processorBinary, local.root, workspaceRoot)
	// The real processor requires its own admitted worker identity. Seed the
	// worker row for the active signed contract; issuer acceptance is tested
	// separately.
	if _, err := fixture.db.Exec(ctx, `insert into public.deployment_membership
		(instance_id,deployment_id,project_id,instance_kind,capability,deployment_epoch,fence_contract_version,load_balancer_admitted,queue_admitted,heartbeat_expires_at)
		select 'compose-processor',deployment_id,project_id,'worker','records.runtime',active_domain_identity_epoch,minimum_fence_contract_version,false,true,clock_timestamp()+interval '1 hour'
		from public.deployment_contract_state where project_id='default'`); err != nil {
		t.Fatal(err)
	}
	databaseURL := contentProcessorCrashDatabaseURL(fixture.directRoleConfig(t, fixture.runtime, "v2-finish-processor"))
	environment := contentProcessorCrashEnvironment(databaseURL, local.root, workspaceRoot)
	environment = append(environment,
		"HOUFENG_RECORD_BIRTH_LAUNCH_FILE=/run/houfeng/records-authority/birth-launch-processor.json",
		"HOUFENG_RECORD_DEPLOYMENT_ID="+deploymentID,
		"HOUFENG_RECORD_INSTANCE_ID=compose-processor", "HOUFENG_RECORD_INSTANCE_KIND=worker",
		"HOUFENG_RECORD_INSTANCE_CAPABILITY=records.runtime")
	if output, err := runContentProcessorCrashHelper(container, environment, "recover", ""); err != nil {
		t.Fatalf("run real processor: %v: %s", err, output)
	}
	// The processor command has exited. The prior pools predate launch staging,
	// so reopen the same namespace through a newly admitted direct runtime pool.
	originalNamespace := local.blob.Namespace()
	if err := local.blob.Close(); err != nil {
		t.Fatalf("close previous local descriptors: %v", err)
	}
	local.pool.Close()
	local.pool = openV2LaunchBoundPool(t, ctx, fixture, fixture.runtime, "v2-finish-runtime-bound", measured)
	local.local = NewPostgresLocalBlobRepository(local.pool, nil, nil)
	reopened, err := attachments.NewLocalBlobStore(ctx, local.root, local.local)
	if err != nil {
		t.Fatalf("reopen admitted local namespace: %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("close reopened local namespace: %v", err)
		}
	})
	currentNamespace := reopened.Namespace()
	if currentNamespace.NamespaceID != originalNamespace.NamespaceID || currentNamespace.IncarnationID != originalNamespace.IncarnationID ||
		currentNamespace.Device != originalNamespace.Device || currentNamespace.RootInode != originalNamespace.RootInode ||
		currentNamespace.DigestInode != originalNamespace.DigestInode || currentNamespace.LocksInode != originalNamespace.LocksInode ||
		currentNamespace.MarkerInode != originalNamespace.MarkerInode || currentNamespace.MarkerDigest != originalNamespace.MarkerDigest ||
		currentNamespace.MarkerNonce != originalNamespace.MarkerNonce {
		t.Fatal("local namespace identity changed across processor launch")
	}
	local.blob = reopened
	local.repository = NewPostgresAttachmentRepository(local.pool)
	if err := local.repository.ConfigureRecordMutationAuthorization(NewPostgresCollaborationMembershipReader(), newRecordsPostgresAuthorization(t, local.pool, fixture.admissionGate)); err != nil {
		t.Fatal(err)
	}
	var workspaceID string
	var attempt int64
	var receipt, pathDigest []byte
	if err := fixture.db.QueryRow(ctx, `select w.workspace_id,w.attempt,w.workspace_path_digest,r.receipt_digest
		from public.content_processor_workspaces w join public.content_workspace_purge_receipts r on r.workspace_id=w.workspace_id
		where w.processor_job_id=$1 and w.workspace_state='purged'`, "apj_v2workspace").Scan(&workspaceID, &attempt, &pathDigest, &receipt); err != nil || attempt <= 0 || len(pathDigest) != sha256.Size || len(receipt) != sha256.Size {
		t.Fatalf("real workspace purge receipt: id=%q attempt=%d error=%v", workspaceID, attempt, err)
	}
	records := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: local.pool, Gate: fixture.admissionGate}, NewRecordAttachmentRevisionParticipant())
	command := revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: local.pool, Gate: fixture.admissionGate},
		recordsPostgresCompleteRevisionInput(t, "V2 workspace", reserve.AttachmentID), "v2-workspace-record")
	command.DraftID, command.DraftETag = draft.DraftID, draft.ETag
	created, err := records.CommitRevision(ctx, command)
	if err != nil {
		t.Fatalf("publish processed record: %v", err)
	}
	return local, created.RecordID, workspaceID, attempt, pathDigest, receipt, measured, command
}

func seedV2SharedArchive(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, local *localPublicationFixture, firstRecord string) (string, attachments.WrittenBlob, []byte) {
	t.Helper()
	birthFixture := ImportAttachmentGCFixture{Runtime: local.pool, Gate: fixture.admissionGate}
	recordRepository := newRevisionMutationAdmittedRepository(t, birthFixture)
	survivor, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birthFixture,
		recordsPostgresCompleteRevisionInput(t, "V2 shared archive survivor"), "v2-shared-survivor"))
	if err != nil {
		t.Fatalf("create shared archive survivor: %v", err)
	}
	content := []byte("shared archive must remain readable after record deletion")
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	if _, err := fixture.db.Exec(ctx, `insert into public.record_import_jobs(import_job_id,actor_id,idempotency_key,job_state,
		identity_classification,archive_digest,destination_subject_kind,destination_subject_source_id,expires_at)
		values('rij_v2shared','usr_aaaaaaaaaaaaaaaaaaaaaaaa','v2shared','uploaded','unknown',$1,'vps',$2,clock_timestamp()+interval '1 hour')`, digest[:], testStoreRecordVPSID); err != nil {
		t.Fatal(err)
	}
	writer, err := attachments.NewManagedBlobWriter(local.blob, attachments.BackendKindLocal, NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil))
	if err != nil {
		t.Fatal(err)
	}
	written, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: "default", Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))},
		Origin: attachments.WriteOrigin{Kind: "import_archive", ID: "rij_v2shared", Generation: 1, ArchiveDigest: &digestText}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	var expiry time.Time
	if err := fixture.db.QueryRow(ctx, `select expires_at from public.record_import_jobs where import_job_id='rij_v2shared'`).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	publication, err := json.Marshal(map[string]any{"artifact_id": "ria_v2shared", "job_id": "rij_v2shared", "kind": "archive",
		"backend_kind": "local", "namespace_id": local.blob.Namespace().NamespaceID, "key": written.Version.Key, "version": written.Version.VersionID,
		"sha256": digestText, "size": len(content), "expires_at": expiry, "obligation_id": written.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.pool.Exec(ctx, `select public.record_import_artifact_publish($1)`, publication); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_import_plans(import_plan_id,import_job_id,plan_digest,object_count,remap_count,expires_at)
		values('rip_v2shared','rij_v2shared',$1,2,0,clock_timestamp()+interval '1 hour')`, digest[:]); err != nil {
		t.Fatal(err)
	}
	for _, recordID := range []string{firstRecord, survivor.RecordID} {
		identity := sha256.Sum256([]byte(recordID))
		if _, err := fixture.db.Exec(ctx, `insert into public.record_import_entity_mappings(import_plan_id,entity_kind,source_id,source_identity_digest,target_id)
			values('rip_v2shared','record',$1,$2,$1)`, recordID, identity[:]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id='rij_v2shared'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind,namespace_id)
		values($1,$2,$3,$4,'local',$5)`, written.Version.Key, digest[:], written.Version.VersionID, len(content), local.blob.Namespace().NamespaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.attachment_quota_accounts set physical_bytes=physical_bytes+$1 where project_id='default'`, len(content)); err != nil {
		t.Fatal(err)
	}
	return survivor.RecordID, written, content
}

func seedV2ExportArtifact(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, local *localPublicationFixture, recordID string) attachments.WrittenBlob {
	t.Helper()
	content := []byte("exclusive export artifact for V2 deletion")
	digest := sha256.Sum256(content)
	var revision string
	if err := fixture.db.QueryRow(ctx, `select current_revision_id from public.records where record_id=$1`, recordID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	var expiry time.Time
	if err := fixture.db.QueryRow(ctx, `insert into public.record_export_jobs
		(export_job_id,actor_id,idempotency_key,export_kind,export_mode,job_state,request_fingerprint,inventory_digest,
		authorization_epoch,record_id,revision_id,owner_id,owner_generation,lease_expires_at,expires_at)
		values('rej_v2exclusive','usr_aaaaaaaaaaaaaaaaaaaaaaaa','v2exclusive','markdown','safe','staging',$1,$1,1,$2,$3,'local_export_worker',1,
		clock_timestamp()+interval '30 minutes',clock_timestamp()+interval '1 hour') returning expires_at`, digest[:], recordID, revision).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	writer, err := attachments.NewManagedBlobWriter(local.blob, attachments.BackendKindLocal, NewPostgresLocalBlobRepository(local.pool, localBlobFixtureWitness(t, local.pool), nil))
	if err != nil {
		t.Fatal(err)
	}
	written, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: "default", Origin: attachments.WriteOrigin{Kind: "export_artifact", ID: "rej_v2exclusive", Generation: 1},
		Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"artifact_id": "rxa_v2exclusive", "job_id": "rej_v2exclusive", "kind": "markdown", "content_type": "text/markdown",
		"backend_kind": "local", "namespace_id": local.blob.Namespace().NamespaceID, "key": written.Version.Key, "version": written.Version.VersionID,
		"sha256": hex.EncodeToString(digest[:]), "size": len(content), "expires_at": expiry, "obligation_id": written.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.pool.Exec(ctx, `select public.record_export_artifact_publish($1)`, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.record_export_jobs set job_state='published',owner_id='',owner_generation=0,lease_expires_at=null where export_job_id='rej_v2exclusive'`); err != nil {
		t.Fatal(err)
	}
	return written
}

// waitV2DeadlineInTransaction keeps the transaction open across an actual
// blocked PostgreSQL lock request, then checks the server clock after timeout.
func waitV2DeadlineInTransaction(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, tx pgx.Tx, expiry time.Time) {
	t.Helper()
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	if _, err := holder.Exec(ctx, `select pg_advisory_xact_lock(1909861244,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `savepoint v2_expiry_wait`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select set_config('lock_timeout', (greatest(1,ceil(extract(epoch from ($1::timestamptz-clock_timestamp()))*1000)::bigint+1000)::text || 'ms'),true)`, expiry); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `select pg_advisory_xact_lock(1909861244,1)`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("deadline lock wait = %v, want lock timeout", err)
	}
	if _, err := tx.Exec(ctx, `rollback to savepoint v2_expiry_wait`); err != nil {
		t.Fatal(err)
	}
	var expired bool
	if err := tx.QueryRow(ctx, `select clock_timestamp() >= $1::timestamptz`, expiry).Scan(&expired); err != nil || !expired {
		t.Fatalf("database owner deadline not reached: %t, %v", expired, err)
	}
}

// An imported target may exist before its attachment write is handed off.
// A V2 core purge must discover that real outstanding target and refuse it.
func testV2UnassignedImportAttachment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	actor := storeEvidenceParticipantActor(t)
	imports := NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, localBlobFixtureWitness(t, fixture.Runtime), nil)
	archiveDigest := sha256.Sum256([]byte("v2 pending import archive"))
	destination := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
		Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true}
	expires := time.Now().UTC().Add(time.Hour)
	job, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID, IdempotencyKey: "v2-unassigned-import",
		ArchiveDigest: archiveDigest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_v2pendingimport")
	planDigest := sha256.Sum256([]byte("v2 pending import plan"))
	const attachmentID = "att_v2pendingimport"
	_, err = imports.SaveImportPlan(ctx, SaveRecordImportPlanInput{ImportJobID: job.ImportJobID, Actor: actor,
		ArchiveDigest: archiveDigest, PlanDigest: planDigest, ObjectCount: 2, RemapCount: 2, Allocations: []recordbirth.Allocation{allocation},
		Remaps: []ImportRemap{{EntityKind: "record", SourceID: "rec_v2pendingimport", TargetID: allocation.RecordID, BirthAllocationID: allocation.AllocationID},
			{EntityKind: "attachment", SourceID: "att_v2pendingimport", TargetID: attachmentID, OwnerTargetRecordID: allocation.RecordID}},
		Documents:          []ImportDocumentPlan{{SourceID: "rec_v2pendingimport", TargetID: allocation.RecordID, Title: "Pending import", Body: "Pending import"}},
		DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	if err := imports.AdvanceImportJob(ctx, AdvanceRecordImportJobInput{ImportJobID: job.ImportJobID, LockVersion: job.LockVersion, JobState: RecordImportJobStatePlanned}); err != nil {
		t.Fatal(err)
	}
	job, err = imports.LoadImportJob(ctx, job.ImportJobID)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("import attachment with pending target handoff")
	digest := sha256.Sum256(content)
	archiveHex, planHex := hex.EncodeToString(archiveDigest[:]), hex.EncodeToString(planDigest[:])
	recordID := allocation.RecordID
	written, err := fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
		Origin: attachments.WriteOrigin{Kind: "import_attachment", ID: job.ImportJobID, Generation: int64(job.LockVersion), ArchiveDigest: &archiveHex,
			PlanDigest: &planHex, TargetRecordID: &recordID, TargetAttachmentID: new(attachmentID)},
		Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	fixture.RefreshHeartbeat()
	if err := fixture.CommitImported(ctx, "CommitRevision", [][]attachments.ImportedAvailableAttachment{{}}, []recordbirth.Allocation{allocation}); err != nil {
		t.Fatal(err)
	}
	expiry := issueV2WorkerFixture(t, ctx, fixture.platform, fixture.Runtime, recordID, "pendingimport")
	waitServingTestFenceExpired(t, ctx, fixture.platform, expiry)
	worker := fixture.platform.openDirectRolePool(t, ctx, fixture.platform.deletionWorker, "v2-pending-import-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, worker); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRecordDeletionRepository(worker, allowRecordPlatformAdmissionGate)
	claim, err := repository.ClaimV2DeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_pending_import", OwnerLeaseDuration: time.Minute})
	if err != nil || claim == nil {
		t.Fatalf("claim pending import: %v, %v", claim, err)
	}
	target := recorddeletion.PurgeTarget{Operation: claim.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: claim.Operation.OperationID, Owner: claim.Owner}}
	_, err = repository.PurgeRecordCore(ctx, recorddeletion.CorePurgeCommand{PurgeTarget: target, SurfaceDigest: recorddeletion.RecordCoreSurfaceDigest()})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" || pgErr.Message != "record purge responsibility remains outstanding" {
		t.Fatalf("core pending import rejection = %v", err)
	}
	var pending, rootExists bool
	if err := fixture.platform.db.QueryRow(ctx, `select exists(select 1 from public.record_write_obligations o join public.record_write_obligation_targets t using(obligation_id)
		where o.obligation_id=$1 and t.record_id=$2 and t.attachment_id=$3 and o.cleanup_completed_at is null and t.handed_off_at is null),
		exists(select 1 from public.records where record_id=$2)`, written.ObligationID, recordID, attachmentID).Scan(&pending, &rootExists); err != nil || !pending || !rootExists {
		t.Fatalf("pending import/root after rejected purge = %t/%t, %v", pending, rootExists, err)
	}
	if _, err := fixture.Blobs.Stat(ctx, written.Version); err != nil {
		t.Fatalf("pending import bytes disappeared: %v", err)
	}
}

// issueV2WorkerFixture uses the existing restricted preview and issue commands.
// Its fixed preview evidence exercises SQL authority, not production issuer readiness.
func issueV2WorkerFixture(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, runtime *pgxpool.Pool, recordID, suffix string) time.Time {
	t.Helper()
	var lifecycle, revision string
	var lockVersion, authorizationEpoch, contentEpoch int64
	if err := fixture.db.QueryRow(ctx, `select r.lifecycle,r.current_revision_id,r.lock_version,r.authorization_epoch,e.delivery_epoch
		from public.records r join public.content_delivery_epochs e on e.project_id=r.project_id and e.object_kind='record' and e.object_id=r.record_id
		where r.record_id=$1`, recordID).Scan(&lifecycle, &revision, &lockVersion, &authorizationEpoch, &contentEpoch); err != nil {
		t.Fatal(err)
	}
	tokenDigest := sha256.Sum256([]byte("v2 fixture token " + suffix))
	digest, token, actor := strings.Repeat("12", 32), hex.EncodeToString(tokenDigest[:]), "usr_aaaaaaaaaaaaaaaaaaaaaaaa"
	preview, err := json.Marshal(map[string]any{
		"deployment_id": recordsPostgresActiveDeletionDeployment(t, ctx, fixture), "record_id": recordID,
		"reservation_id": "drs_v2worker" + suffix,
		"actor_id":       actor, "actor_scope_digest": digest, "token_commitment": token,
		"request_fingerprint": digest, "binding_digest": digest, "authorization_evidence": "12",
		"lifecycle": lifecycle, "revision_id": revision, "lock_version": lockVersion,
		"authorization_epoch": authorizationEpoch, "content_epoch": contentEpoch,
		"dependency_digest": digest, "processor_digest": digest, "obligation_digest": digest,
		"adapter_readiness_digest": digest, "adapter_preview_digest": digest, "backup_inventory_digest": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	var previewRecord string
	if err := runtime.QueryRow(ctx, `select (public.record_deletion_preview_create($1::bytea)).record_id`, preview).Scan(&previewRecord); err != nil || previewRecord != recordID {
		t.Fatalf("create deletion preview: record=%q error=%v", previewRecord, err)
	}
	secondToken := sha256.Sum256([]byte("v2 fixture second token " + suffix))
	secondPreview, err := json.Marshal(map[string]any{
		"deployment_id": recordsPostgresActiveDeletionDeployment(t, ctx, fixture), "record_id": recordID,
		"reservation_id": "drs_v2worker" + suffix + "2",
		"actor_id":       actor, "actor_scope_digest": digest, "token_commitment": hex.EncodeToString(secondToken[:]),
		"request_fingerprint": digest, "binding_digest": digest, "authorization_evidence": "12",
		"lifecycle": lifecycle, "revision_id": revision, "lock_version": lockVersion,
		"authorization_epoch": authorizationEpoch, "content_epoch": contentEpoch,
		"dependency_digest": digest, "processor_digest": digest, "obligation_digest": digest,
		"adapter_readiness_digest": digest, "adapter_preview_digest": digest, "backup_inventory_digest": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.QueryRow(ctx, `select (public.record_deletion_preview_create($1::bytea)).record_id`, secondPreview).Scan(&previewRecord); err != nil || previewRecord != recordID {
		t.Fatalf("create second deletion preview: record=%q error=%v", previewRecord, err)
	}
	lookup, err := json.Marshal(map[string]any{"reservation_id": "drs_v2worker" + suffix, "actor_id": actor, "record_id": recordID})
	if err != nil {
		t.Fatal(err)
	}
	var storedCommitment []byte
	if err := runtime.QueryRow(ctx, `select deletion_token_commitment from public.record_deletion_preview_lock($1::bytea)`, lookup).Scan(&storedCommitment); err != nil || !bytes.Equal(storedCommitment, tokenDigest[:]) {
		t.Fatalf("lock deletion preview: commitment=%x error=%v", storedCommitment, err)
	}
	wrongActor, err := json.Marshal(map[string]any{"reservation_id": "drs_v2worker" + suffix, "actor_id": "usr_wrong", "record_id": recordID})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.QueryRow(ctx, `select deletion_token_commitment from public.record_deletion_preview_lock($1::bytea)`, wrongActor).Scan(&storedCommitment); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("preview lock with different actor: %v", err)
	}
	issue, err := json.Marshal(map[string]any{"token_commitment": token, "actor_id": actor, "actor_scope_digest": digest,
		"record_id": recordID, "binding_digest": digest, "authorization_evidence": "12", "request_fingerprint": digest,
		"reservation_id": "drs_v2worker" + suffix, "operation_id": "rpo_v2worker" + suffix, "owner_id": "v2_issuer", "confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	var wrongIssue map[string]any
	if err := json.Unmarshal(issue, &wrongIssue); err != nil {
		t.Fatal(err)
	}
	wrongIssue["reservation_id"] = "drs_v2worker" + suffix + "2"
	wrongIssueBody, err := json.Marshal(wrongIssue)
	if err != nil {
		t.Fatal(err)
	}
	var wrongOperation string
	err = runtime.QueryRow(ctx, `select (public.record_deletion_issue($1::bytea)).operation_id`, wrongIssueBody).Scan(&wrongOperation)
	var identityError *pgconn.PgError
	if !errors.As(err, &identityError) || identityError.Code != "55000" || identityError.Message != "deletion preview identity conflict" {
		t.Fatalf("issue with different reservation: %v", err)
	}
	var operationID string
	if err := runtime.QueryRow(ctx, `select (public.record_deletion_issue($1::bytea)).operation_id`, issue).Scan(&operationID); err != nil || operationID != "rpo_v2worker"+suffix {
		t.Fatalf("issue deletion: operation=%q error=%v", operationID, err)
	}
	var expiry time.Time
	if err := fixture.db.QueryRow(ctx, `select owner_expires_at from public.record_purge_operations where operation_id=$1`, operationID).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	return expiry
}

func TestPostgresIntegrationRecordDeletionV2WorkerLifecycle(t *testing.T) {
	for _, immediate := range []bool{false, true} {
		name := "ordinary"
		if immediate {
			name = "constraints_immediate"
		}
		t.Run(name, func(t *testing.T) { runV2WorkerLifecycle(t, immediate) })
	}
}

func runV2WorkerLifecycle(t *testing.T, immediate bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "v2-worker-runtime", 2)
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v2-worker-direct", 3)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workerPool); err != nil {
		t.Fatal(err)
	}
	created := seedAdmittedOutboxRecord(t, ctx, runtime, fixture.admissionGate, "v2-worker-parent")
	if _, err := fixture.db.Exec(ctx, `insert into public.record_activity_projection_heads(project_id,projection_generation,head_state) values('default',1,'active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_activity_projection(activity_id,project_id,projection_generation,ingest_sequence,event_kind,event_at,recorded_at,source_kind,source_event_id,source_version,record_id,revision_id,presentation_version,presentation_json,auth_scope_digest,canonical_hash) values('act_v2lifecycle','default',1,1,'record_created',transaction_timestamp(),transaction_timestamp(),'record_domain','event_v2lifecycle',1,$1,$2,1,'{}'::jsonb,decode(repeat('11',32),'hex'),decode(repeat('11',32),'hex'))`, created.RecordID, created.RevisionID); err != nil {
		t.Fatal(err)
	}
	expires := issueV2WorkerFixture(t, ctx, fixture, runtime, created.RecordID, "claim")
	repository := NewPostgresRecordDeletionRepository(workerPool, allowRecordPlatformAdmissionGate)
	input := recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_worker", OwnerLeaseDuration: time.Second}
	if claim, err := repository.ClaimV2DeletionWork(ctx, input); err != nil || claim != nil {
		t.Fatalf("claim before issuer expiry = %v, %v", claim, err)
	}
	if _, err := NewPostgresRecordDeletionRepository(runtime, fixture.admissionGate).ClaimV2DeletionWork(ctx, input); err == nil {
		t.Fatal("runtime claimed deletion work")
	}
	waitServingTestFenceExpired(t, ctx, fixture, expires)
	type result struct {
		claim *recorddeletion.ClaimedDeletionWork
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			claim, err := repository.ClaimV2DeletionWork(ctx, input)
			results <- result{claim, err}
		}()
	}
	close(start)
	var first *recorddeletion.ClaimedDeletionWork
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatalf("competing claim: %v", got.err)
		}
		if got.claim != nil {
			if first != nil {
				t.Fatal("two workers claimed one live lease")
			}
			first = got.claim
		}
	}
	if first == nil {
		t.Fatal("no worker claimed expired operation")
	}
	wrongRole := NewPostgresRecordDeletionRepository(runtime, fixture.admissionGate)
	if _, err := wrongRole.RenewV2DeletionWork(ctx, *first, time.Minute); err == nil {
		t.Fatal("runtime renewed deletion ownership")
	}
	if err := wrongRole.CompleteV2OnlinePurge(ctx, *first, recorddeletion.OnlinePurgeReceipt{OperationID: first.Operation.OperationID, ReceiptDigest: sha256.Sum256([]byte("wrong role"))}); err == nil {
		t.Fatal("runtime completed deletion")
	}
	waitServingTestFenceExpired(t, ctx, fixture, first.Owner.ExpiresAt)
	second, err := repository.ClaimV2DeletionWork(ctx, input)
	if err != nil || second == nil || second.Owner.Generation != first.Owner.Generation+1 {
		t.Fatalf("same-owner takeover: %v, %v", second, err)
	}
	if _, err := repository.RenewV2DeletionWork(ctx, *first, time.Minute); err == nil {
		t.Fatal("expired owner renewed after takeover")
	}
	renewed, err := repository.RenewV2DeletionWork(ctx, *second, time.Minute)
	if err != nil || renewed.Owner.Generation != second.Owner.Generation || !renewed.Owner.ExpiresAt.After(second.Owner.ExpiresAt) {
		t.Fatalf("renew: %v, %v", renewed, err)
	}
	if _, err := repository.RenewV2DeletionWork(ctx, *second, time.Minute); err == nil {
		t.Fatal("old expiry renewed after renewal")
	}
	staleTarget := recorddeletion.PurgeTarget{Operation: second.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: second.Operation.OperationID, Owner: second.Owner}}
	if _, err := repository.PurgeRecordActivity(ctx, activity.DeletionCommand{PurgeTarget: staleTarget, SurfaceDigest: recorddeletion.RecordActivitySurfaceDigest()}); err == nil {
		t.Fatal("activity purge accepted pre-renewal expiry")
	}
	short, err := repository.RenewV2DeletionWork(ctx, renewed, 5*time.Second)
	if err != nil {
		t.Fatalf("renew returned tuple: %v", err)
	}
	tx, err := workerPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	target := recorddeletion.PurgeTarget{Operation: short.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: short.Operation.OperationID, Owner: short.Owner}}
	body, err := deletionAdapterEnvelope(ctx, tx, target, recorddeletion.RecordActivitySurfaceDigest())
	if err != nil {
		t.Fatal(err)
	}
	var surface, digest []byte
	var removed int64
	var verified time.Time
	if err := tx.QueryRow(ctx, `select removed_surface_digest,receipt_digest,removed_row_count,verified_absent_at from public.record_activity_purge_v2($1)`, body).Scan(&surface, &digest, &removed, &verified); err != nil || removed != 1 {
		t.Fatalf("activity purge removed=%d before expiry: %v", removed, err)
	}
	if immediate {
		if _, err := tx.Exec(ctx, `set constraints all immediate`); err != nil {
			t.Fatalf("set constraints immediate: %v", err)
		}
	}
	waitV2DeadlineInTransaction(t, ctx, fixture, tx, short.Owner.ExpiresAt)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit after authorized activity and expiry: %v", err)
	}
	var persisted []byte
	var rowExists bool
	if err := fixture.db.QueryRow(ctx, `select receipt_digest,exists(select 1 from public.record_activity_projection where record_id=$2) from public.record_activity_purge_receipts where operation_id=$1`, short.Operation.OperationID, created.RecordID).Scan(&persisted, &rowExists); err != nil || !bytes.Equal(persisted, digest) || rowExists {
		t.Fatalf("committed activity receipt matches=%t row_exists=%t: %v", bytes.Equal(persisted, digest), rowExists, err)
	}
}

// RunV2FinishResponsibilities exercises the real adapters against a direct worker role.
func RunV2FinishResponsibilities(t *testing.T, newPortabilityAdapter func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error)) {
	t.Run("unassigned import attachment", testV2UnassignedImportAttachment)
	t.Run("terminal_update_wait_crosses_expiry", func(t *testing.T) { testV2TerminalUpdateWait(t, newPortabilityAdapter) })
	t.Run("terminal_takeover_commit", func(t *testing.T) { testV2TerminalTakeover(t, newPortabilityAdapter, true) })
	t.Run("terminal_takeover_rollback", func(t *testing.T) { testV2TerminalTakeover(t, newPortabilityAdapter, false) })
	t.Run("core_creator_fence", func(t *testing.T) { testV2CoreCreatorFence(t, newPortabilityAdapter) })
	for _, immediate := range []bool{false, true} {
		name := "ordinary"
		if immediate {
			name = "constraints_immediate"
		}
		t.Run(name, func(t *testing.T) { runV2FinishResponsibilities(t, newPortabilityAdapter, immediate) })
	}
}

type v2FinishFixture struct {
	ctx                 context.Context
	fixture             recordPlatformPostgresFixture
	runtime, workerPool *pgxpool.Pool
	repository          *PostgresRecordDeletionRepository
	claim               *recorddeletion.ClaimedDeletionWork
	receipt, bogus      recorddeletion.OnlinePurgeReceipt
	purger              *recorddeletion.RegistryPurger
	physicalAfter       int64
	measured            [32]byte
	command             records.RevisionCommitCommand
	local               *localPublicationFixture
	export              attachments.WrittenBlob
}

func setupV2FinishFixture(t *testing.T, newPortabilityAdapter func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error)) v2FinishFixture {
	return setupV2FinishFixtureWithCoreHook(t, newPortabilityAdapter, nil)
}

func setupV2FinishFixtureWithCoreHook(t *testing.T, newPortabilityAdapter func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error), hook v2CoreHook) v2FinishFixture {
	return setupV2FinishFixtureWithHooks(t, newPortabilityAdapter, hook, nil, nil)
}

func setupV2FinishFixtureWithHooks(t *testing.T, newPortabilityAdapter func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error), hook v2CoreHook, beforeIssue func(context.Context, recordPlatformPostgresFixture, string), beforePurge func(context.Context, recordPlatformPostgresFixture, *localPublicationFixture, *pgxpool.Pool, *recorddeletion.ClaimedDeletionWork, attachments.WrittenBlob)) v2FinishFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	t.Cleanup(cancel)
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	local, recordID, workspaceID, workspaceAttempt, workspacePath, workspaceReceipt, measured, command := seedV2ProcessedWorkspace(t, ctx, fixture)
	runtime := local.pool
	workerPool := openV2LaunchBoundPool(t, ctx, fixture, fixture.deletionWorker, "v2-finish-worker-bound", measured)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workerPool); err != nil {
		t.Fatal(err)
	}
	survivorID, archive, archiveContent := seedV2SharedArchive(t, ctx, fixture, local, recordID)
	export := seedV2ExportArtifact(t, ctx, fixture, local, recordID)
	if beforeIssue != nil {
		beforeIssue(ctx, fixture, recordID)
	}
	expires := issueV2WorkerFixture(t, ctx, fixture, runtime, recordID, "finish")
	waitServingTestFenceExpired(t, ctx, fixture, expires)
	repository := NewPostgresRecordDeletionRepository(workerPool, allowRecordPlatformAdmissionGate)
	claim, err := repository.ClaimV2DeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_finish_worker", OwnerLeaseDuration: 2 * time.Minute})
	if err != nil || claim == nil {
		t.Fatalf("claim: %v, %v", claim, err)
	}
	if beforePurge != nil {
		beforePurge(ctx, fixture, local, workerPool, claim, export)
	}
	bogus := recorddeletion.OnlinePurgeReceipt{OperationID: claim.Operation.OperationID, ReceiptDigest: sha256.Sum256([]byte("not an aggregate"))}
	if err := repository.CompleteV2OnlinePurge(ctx, *claim, bogus); err == nil {
		t.Fatal("finished without nine real receipts")
	}
	var state string
	if err := fixture.db.QueryRow(ctx, `select operation_state from public.record_purge_operations where operation_id=$1`, claim.Operation.OperationID).Scan(&state); err != nil || state != "online_purging" {
		t.Fatalf("missing receipts changed operation state: %q, %v", state, err)
	}
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(workerPool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	attachmentRepository := NewPostgresAttachmentRepository(workerPool)
	if err := attachmentRepository.ConfigureAttachmentDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	evidenceRepository := NewPostgresEvidenceRepository(workerPool, allowRecordPlatformAdmissionGate, nil)
	runtimeEvidenceRepository := NewPostgresEvidenceRepository(runtime, allowRecordPlatformAdmissionGate, nil)
	comparisonStore, err := NewPostgresComparisonDeletionStore(runtimeEvidenceRepository, evidenceRepository)
	if err != nil {
		t.Fatal(err)
	}
	for _, repositories := range [][2]*PostgresEvidenceRepository{
		{nil, evidenceRepository}, {runtimeEvidenceRepository, nil},
		{NewPostgresEvidenceRepository(nil, allowRecordPlatformAdmissionGate, nil), evidenceRepository},
		{runtimeEvidenceRepository, NewPostgresEvidenceRepository(nil, allowRecordPlatformAdmissionGate, nil)},
	} {
		if _, err := NewPostgresComparisonDeletionStore(repositories[0], repositories[1]); !errors.Is(err, recorddeletion.ErrInvalidComparisonAdapter) {
			t.Fatalf("comparison store accepted missing dependency: %v", err)
		}
	}
	var roleErr *pgconn.PgError
	if _, err := evidenceRepository.RecordComparisonHealth(ctx); !errors.As(err, &roleErr) || roleErr.Code != "42501" {
		t.Fatalf("worker comparison health = %v, want evidence read denied", err)
	}
	health, err := comparisonStore.RecordComparisonHealth(ctx)
	if err != nil || !health.Healthy() {
		t.Fatalf("runtime comparison health = %v, %v", health, err)
	}
	var revision string
	var lockVersion, authorizationEpoch, contentEpoch int64
	if err := fixture.db.QueryRow(ctx, `select r.current_revision_id,r.lock_version,r.authorization_epoch,e.delivery_epoch
		from public.records r join public.content_delivery_epochs e on e.project_id=r.project_id and e.object_kind='record' and e.object_id=r.record_id
		where r.record_id=$1`, survivorID).Scan(&revision, &lockVersion, &authorizationEpoch, &contentEpoch); err != nil {
		t.Fatal(err)
	}
	previewTarget := recorddeletion.PreviewTarget{
		Object: claim.Operation.Object, CurrentRevisionID: revision, LockVersion: uint64(lockVersion),
		AuthorizationEpoch: uint64(authorizationEpoch), ContentDeliveryEpoch: recordplatform.ContentEpoch(contentEpoch),
		DependencyGraphDigest: sha256.Sum256([]byte("v2 comparison preview fixture")),
	}
	previewTarget.Object.ObjectID = survivorID
	roleErr = nil
	if _, err := evidenceRepository.PreviewComparisonDeletion(ctx, previewTarget); !errors.As(err, &roleErr) || roleErr.Code != "42501" {
		t.Fatalf("worker comparison preview = %v, want evidence read denied", err)
	}
	preview, err := comparisonStore.PreviewComparisonDeletion(ctx, previewTarget)
	if err != nil || preview.Validate() != nil {
		t.Fatalf("runtime comparison preview = %v, %v", preview, err)
	}
	var adapters []recorddeletion.Adapter
	add := func(adapter recorddeletion.Adapter, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		adapters = append(adapters, adapter)
	}
	core, err := recorddeletion.NewCoreAdapter(repository)
	if err != nil {
		t.Fatal(err)
	}
	if hook == nil {
		add(core, nil)
	} else {
		add(v2CoreHookAdapter{DeletionPurgeAdapter: core, hook: func(target recorddeletion.PurgeTarget) { hook(ctx, fixture, runtime, workerPool, command, target) }}, nil)
	}
	add(attachments.NewDeletionAdapter(attachmentRepository))
	add(evidence.NewDeletionAdapter(evidenceRepository))
	add(recorddeletion.NewMarkdownClientAdapter(repository))
	add(recordsearch.NewDeletionAdapter(repository))
	add(activity.NewDeletionAdapter(repository))
	add(recorddeletion.NewComparisonAdapter(comparisonStore))
	add(recordcollaboration.NewDeletionAdapter(repository))
	add(newPortabilityAdapter(repository))
	registry, err := recorddeletion.NewRegistry(adapters)
	if err != nil {
		t.Fatal(err)
	}
	purger := recorddeletion.NewRegistryPurger(registry)
	target := recorddeletion.PurgeTarget{Operation: claim.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: claim.Operation.OperationID, Owner: claim.Owner}}
	_, err = runtimeEvidenceRepository.PurgeRecordComparison(ctx, recorddeletion.ComparisonPurgeCommand{
		Operation: target.Operation, OwnerClaim: target.OwnerClaim, SurfaceDigest: recorddeletion.RecordComparisonSurfaceDigest(),
	})
	roleErr = nil
	if !errors.As(err, &roleErr) || roleErr.Code != "42501" {
		t.Fatalf("runtime comparison purge = %v, want worker authority required", err)
	}
	fixtureConn, err := fixture.db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer fixtureConn.Release()
	if _, err := fixtureConn.Exec(ctx, `set session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = fixtureConn.Exec(context.Background(), `set session_replication_role = origin`) }()
	if !t.Run("workspace receipt required before preparation", func(t *testing.T) {
		statement := `update public.content_workspace_purge_receipts set receipt_digest=set_byte(receipt_digest,0,get_byte(receipt_digest,0) # 255) where workspace_id=$1`
		result, err := fixtureConn.Exec(ctx, statement, workspaceID)
		if err != nil || result.RowsAffected() != 1 {
			t.Fatalf("alter workspace receipt fixture: %v", err)
		}
		defer func() {
			if _, err := fixtureConn.Exec(context.Background(), statement, workspaceID); err != nil {
				t.Errorf("restore workspace receipt: %v", err)
			}
		}()
		if _, err := attachmentRepository.PurgeRecordAttachments(ctx, attachments.AttachmentDeletionCommand{PurgeTarget: target, SurfaceDigest: recorddeletion.RecordAttachmentsSurfaceDigest()}); err == nil {
			t.Fatal("attachment preparation accepted invalid workspace receipt")
		}
		var present bool
		if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.content_processor_workspaces where workspace_id=$1)`, workspaceID).Scan(&present); err != nil || !present {
			t.Fatalf("failed preparation removed workspace: %t, %v", present, err)
		}
	}) {
		t.Fatal("workspace receipt rejection failed")
	}
	receipt, err := purger.PurgeOnline(ctx, target)
	if err != nil {
		t.Fatalf("purge nine real adapters: %v", err)
	}
	var rootExists bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.records where record_id=$1)`, claim.Operation.Object.ObjectID).Scan(&rootExists); err != nil || rootExists {
		t.Fatalf("core purge left record root: %t, %v", rootExists, err)
	}
	var registryExists bool
	var retainedReceipt []byte
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.content_processor_workspaces where workspace_id=$1),
		(select receipt_digest from public.content_workspace_purge_receipts where workspace_id=$1)`, workspaceID).Scan(&registryExists, &retainedReceipt); err != nil || registryExists || hex.EncodeToString(retainedReceipt) != hex.EncodeToString(workspaceReceipt) {
		t.Fatalf("workspace %s attempt %d: registry=%t receipt changed=%t error=%v", workspaceID, workspaceAttempt, registryExists, hex.EncodeToString(retainedReceipt) != hex.EncodeToString(workspaceReceipt), err)
	}
	var manifestJob string
	var manifestAttempt int64
	var manifestPath, manifestReceipt []byte
	if err := fixture.db.QueryRow(ctx, `select processor_job_id,attempt,workspace_path_digest,receipt_digest
		from public.attachment_purge_workspace_targets where operation_id=$1 and workspace_id=$2`, claim.Operation.OperationID, workspaceID).
		Scan(&manifestJob, &manifestAttempt, &manifestPath, &manifestReceipt); err != nil || manifestJob != "apj_v2workspace" || manifestAttempt != workspaceAttempt ||
		hex.EncodeToString(manifestPath) != hex.EncodeToString(workspacePath) || hex.EncodeToString(manifestReceipt) != hex.EncodeToString(workspaceReceipt) {
		t.Fatalf("workspace manifest job=%q attempt=%d error=%v", manifestJob, manifestAttempt, err)
	}
	var sharedIntact bool
	var physicalAfter int64
	if err := fixture.db.QueryRow(ctx, `select
		exists(select 1 from public.record_import_artifacts where import_job_id='rij_v2shared')
		and exists(select 1 from public.record_import_entity_mappings where target_id=$1)
		and exists(select 1 from public.record_write_obligations where obligation_id=$2 and operation_id is null and cleanup_completed_at is null)
		and exists(select 1 from public.blob_gc_deletions where operation_id=$3 and physical_delete_result='retained_referenced'),
		(select physical_bytes from public.attachment_quota_accounts where project_id='default')`, survivorID, archive.ObligationID, claim.Operation.OperationID).Scan(&sharedIntact, &physicalAfter); err != nil || !sharedIntact || physicalAfter != int64(len(archiveContent)) {
		t.Fatalf("shared archive handoff intact=%t physical=%d error=%v", sharedIntact, physicalAfter, err)
	}
	reader, err := local.blob.Open(ctx, archive.Version, attachments.FullByteRange())
	if err != nil {
		t.Fatal(err)
	}
	observed, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(observed, archiveContent) {
		t.Fatalf("survivor archive read: %v, %v", readErr, closeErr)
	}
	if _, err := local.blob.Stat(ctx, export.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("exclusive export remains: %v", err)
	}
	var exportSettled bool
	if err := fixture.db.QueryRow(ctx, `select not exists(select 1 from public.record_export_artifacts where export_artifact_id='rxa_v2exclusive')
		and exists(select 1 from public.record_write_obligations where obligation_id=$1 and operation_id=$2 and cleanup_completed_at is not null)`, export.ObligationID, claim.Operation.OperationID).Scan(&exportSettled); err != nil || !exportSettled {
		t.Fatalf("export responsibility not settled: %t, %v", exportSettled, err)
	}
	replayed, err := purger.PurgeOnline(ctx, target)
	if err != nil || replayed != receipt {
		t.Fatalf("root-absent purge replay: %v, %v", replayed, err)
	}

	// Corrupt one real receipt at a time as the isolated fixture owner. The
	// finish call itself always uses the direct worker login and the original aggregate.
	for _, table := range []string{
		"record_core_purge_receipts", "attachment_purge_receipts", "evidence_purge_receipts",
		"record_markdown_client_purge_receipts", "record_search_purge_receipts", "record_activity_purge_receipts",
		"record_comparison_purge_receipts", "record_collaboration_purge_receipts", "record_portability_purge_receipts",
	} {
		t.Run("receipt corruption/"+table, func(t *testing.T) {
			statement := `update public.` + table + ` set receipt_digest=set_byte(receipt_digest,0,get_byte(receipt_digest,0) # 255) where operation_id=$1`
			result, err := fixtureConn.Exec(ctx, statement, claim.Operation.OperationID)
			if err != nil || result.RowsAffected() == 0 {
				t.Fatalf("corrupt receipt fixture: %v", err)
			}
			defer func() {
				if _, err := fixtureConn.Exec(context.Background(), statement, claim.Operation.OperationID); err != nil {
					t.Errorf("restore receipt fixture: %v", err)
				}
			}()
			if err := repository.CompleteV2OnlinePurge(ctx, *claim, receipt); err == nil {
				t.Fatal("finish accepted altered receipt")
			}
		})
	}
	t.Run("workspace receipt corruption after registry removal", func(t *testing.T) {
		statement := `update public.content_workspace_purge_receipts set receipt_digest=set_byte(receipt_digest,0,get_byte(receipt_digest,0) # 255) where workspace_id=$1`
		result, err := fixtureConn.Exec(ctx, statement, workspaceID)
		if err != nil || result.RowsAffected() != 1 {
			t.Fatalf("corrupt workspace receipt fixture: %v", err)
		}
		defer func() {
			if _, err := fixtureConn.Exec(context.Background(), statement, workspaceID); err != nil {
				t.Errorf("restore workspace receipt: %v", err)
			}
		}()
		if err := repository.CompleteV2OnlinePurge(ctx, *claim, receipt); err == nil {
			t.Fatal("finish accepted altered workspace receipt")
		}
	})
	t.Run("shared archive requires target handoff and live owner", func(t *testing.T) {
		var handedOff *time.Time
		if err := fixtureConn.QueryRow(ctx, `select handed_off_at from public.record_write_obligation_targets where obligation_id=$1 and record_id=$2`, archive.ObligationID, recordID).Scan(&handedOff); err != nil || handedOff == nil {
			t.Fatalf("read handed-off target: %v", err)
		}
		if _, err := fixtureConn.Exec(ctx, `update public.record_write_obligation_targets set handed_off_at=null where obligation_id=$1 and record_id=$2`, archive.ObligationID, recordID); err != nil {
			t.Fatal(err)
		}
		if err := repository.CompleteV2OnlinePurge(ctx, *claim, receipt); err == nil {
			t.Error("finish accepted missing target handoff")
		}
		if _, err := fixtureConn.Exec(ctx, `update public.record_write_obligation_targets set handed_off_at=$3 where obligation_id=$1 and record_id=$2`, archive.ObligationID, recordID, handedOff); err != nil {
			t.Fatal(err)
		}
		if _, err := fixtureConn.Exec(ctx, `update public.record_write_obligation_targets set handed_off_at=clock_timestamp() where obligation_id=$1 and record_id=$2`, archive.ObligationID, survivorID); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := fixtureConn.Exec(context.Background(), `update public.record_write_obligation_targets set handed_off_at=null where obligation_id=$1 and record_id=$2`, archive.ObligationID, survivorID); err != nil {
				t.Errorf("restore live archive owner: %v", err)
			}
		}()
		if err := repository.CompleteV2OnlinePurge(ctx, *claim, receipt); err == nil {
			t.Error("finish accepted archive without a live owner")
		}
	})
	return v2FinishFixture{ctx: ctx, fixture: fixture, runtime: runtime, workerPool: workerPool, repository: repository,
		claim: claim, receipt: receipt, bogus: bogus, purger: purger, physicalAfter: physicalAfter, measured: measured, command: command, local: local, export: export}
}

func runV2FinishResponsibilities(t *testing.T, newPortabilityAdapter func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error), immediate bool) {
	f := setupV2FinishFixture(t, newPortabilityAdapter)
	ctx, fixture, runtime, workerPool, repository := f.ctx, f.fixture, f.runtime, f.workerPool, f.repository
	claim, receipt, bogus, purger, physicalAfter := f.claim, f.receipt, f.bogus, f.purger, f.physicalAfter
	var state string

	// Finish inside a real transaction, then let the original tuple expire before COMMIT.
	short, err := repository.RenewV2DeletionWork(ctx, *claim, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := deletionV2Owner(short)
	if err != nil {
		t.Fatal(err)
	}
	body, err := encodeRecordPurgeCommand(struct {
		deletionV2OwnerEnvelope
		ReceiptDigest string `json:"receipt_digest"`
	}{owner, hex.EncodeToString(receipt.ReceiptDigest[:])})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := workerPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var finished bool
	if err := tx.QueryRow(ctx, `select public.record_deletion_finish_v2($1)`, body).Scan(&finished); err != nil || !finished {
		t.Fatalf("finish before commit: %t, %v", finished, err)
	}
	if immediate {
		if _, err := tx.Exec(ctx, `set constraints all immediate`); err != nil {
			t.Fatalf("set constraints immediate: %v", err)
		}
	}
	waitV2DeadlineInTransaction(t, ctx, fixture, tx, short.Owner.ExpiresAt)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit after authorized finish and expiry: %v", err)
	}
	var terminalValid bool
	if err := fixture.db.QueryRow(ctx, `select o.operation_state='online_purged' and o.completed_at is not null
		and o.owner_id='' and o.owner_generation=0 and o.owner_expires_at is null
		and r.state='committed' and r.owner_id='' and r.owner_generation=0 and r.owner_expires_at is null
		and exists(select 1 from public.record_deletion_gates g where g.operation_id=o.operation_id and g.project_id=o.project_id and g.record_id=r.object_id and g.fence_epoch=r.fence_epoch)
		and not exists(select 1 from public.deletion_fence_leases f where f.project_id=o.project_id and f.object_kind='record' and f.object_id=r.object_id)
		from public.record_purge_operations o join public.deletion_reservations r on r.reservation_id=o.reservation_id where o.operation_id=$1`, short.Operation.OperationID).Scan(&terminalValid); err != nil || !terminalValid {
		t.Fatalf("terminal invariants after late commit: %t, %v", terminalValid, err)
	}
	if err := repository.CompleteV2OnlinePurge(ctx, short, receipt); err != nil {
		t.Fatalf("exact finish replay: %v", err)
	}
	var physicalReplay int64
	if err := fixture.db.QueryRow(ctx, `select physical_bytes from public.attachment_quota_accounts where project_id='default'`).Scan(&physicalReplay); err != nil || physicalReplay != physicalAfter {
		t.Fatalf("quota changed on terminal replay: before=%d after=%d error=%v", physicalAfter, physicalReplay, err)
	}
	if err := repository.CompleteV2OnlinePurge(ctx, short, bogus); err == nil {
		t.Fatal("different terminal receipt accepted")
	}
	secondRecord := seedAdmittedOutboxRecord(t, ctx, runtime, fixture.admissionGate, "v2-worker-complete")
	secondExpiry := issueV2WorkerFixture(t, ctx, fixture, runtime, secondRecord.RecordID, "runonce")
	waitServingTestFenceExpired(t, ctx, fixture, secondExpiry)
	worker := recorddeletion.NewV2DeletionWorker(repository, purger, recorddeletion.DeletionWorkerOptions{OwnerID: "v2_runonce", OwnerLeaseDuration: time.Minute})
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("V2 worker RunOnce: %v", err)
	}
	if err := fixture.db.QueryRow(ctx, `select operation_state from public.record_purge_operations where operation_id='rpo_v2workerrunonce'`).Scan(&state); err != nil || state != "online_purged" {
		t.Fatalf("V2 worker terminal state = %q, %v", state, err)
	}
}

func testV2TerminalUpdateWait(t *testing.T, factory func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error)) {
	f := setupV2FinishFixture(t, factory)
	ctx := f.ctx
	short, err := f.repository.RenewV2DeletionWork(ctx, *f.claim, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := deletionV2Owner(short)
	if err != nil {
		t.Fatal(err)
	}
	body, err := encodeRecordPurgeCommand(struct {
		deletionV2OwnerEnvelope
		ReceiptDigest string `json:"receipt_digest"`
	}{owner, hex.EncodeToString(f.receipt.ReceiptDigest[:])})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.fixture.db.Exec(ctx, `create function public.v2_test_terminal_wait() returns trigger language plpgsql as $$ begin if new.operation_state='online_purged' then perform pg_catalog.pg_advisory_xact_lock(1909861244,3); end if; return new; end $$;
		create trigger v2_test_terminal_wait before update on public.record_purge_operations for each row execute function public.v2_test_terminal_wait()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.fixture.db.Exec(context.Background(), `drop trigger if exists v2_test_terminal_wait on public.record_purge_operations; drop function if exists public.v2_test_terminal_wait()`); err != nil {
			t.Errorf("remove terminal trigger: %v", err)
		}
	})
	holder, err := f.fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	var holderPID int
	if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `select pg_advisory_xact_lock(1909861244,3)`); err != nil {
		t.Fatal(err)
	}
	tx, err := f.workerPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var workerPID int
	if err := tx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		var finished bool
		err := tx.QueryRow(ctx, `select public.record_deletion_finish_v2($1)`, body).Scan(&finished)
		if err == nil && !finished {
			err = errors.New("finish returned false")
		}
		done <- err
	}()
	waitForPostgresBlockingPID(t, ctx, f.fixture.db, workerPID, holderPID)
	var live bool
	if err := f.fixture.db.QueryRow(ctx, `select clock_timestamp() < $1::timestamptz`, short.Owner.ExpiresAt).Scan(&live); err != nil || !live {
		t.Fatalf("owner expired before terminal update wait: %t, %v", live, err)
	}
	waitServingTestFenceExpired(t, ctx, f.fixture, short.Owner.ExpiresAt)
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("terminal update wait = %v, want 42501", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var unchanged bool
	if err := f.fixture.db.QueryRow(ctx, `select o.operation_state='online_purging' and o.completed_at is null and o.owner_id=$2 and o.owner_generation=$3 and o.owner_expires_at=$4
		and r.state='fenced' and r.owner_id=$2 and r.owner_generation=$3 and r.owner_expires_at=$4
		and exists(select 1 from public.deletion_fence_leases l where l.project_id=o.project_id and l.object_kind='record' and l.object_id=r.object_id and l.owner_id=$2 and l.owner_generation=$3 and l.expires_at=$4)
		from public.record_purge_operations o join public.deletion_reservations r on r.reservation_id=o.reservation_id where o.operation_id=$1`, short.Operation.OperationID, short.Owner.OwnerID, short.Owner.Generation, short.Owner.ExpiresAt).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("terminal effects survived failed command: %t, %v", unchanged, err)
	}
}
