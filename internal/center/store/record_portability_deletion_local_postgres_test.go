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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresIntegrationLocalPortabilitySharedArchiveHandoff(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	first := seedRecordWithoutSearchProjection(t, ctx, fixture, "First archive owner")
	last := seedRecordWithoutSearchProjection(t, ctx, fixture, "Last archive owner")
	survivor := seedRecordWithoutSearchProjection(t, ctx, fixture, "Unrelated export owner")
	runtime := fixture.openDirectRuntimePool(t, ctx, "portability-local-runtime", 2)
	root := filepath.Join(t.TempDir(), "blob-root")
	blob, err := newPostgresLocalBlobStore(t, ctx, fixture, root)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := attachments.NewManagedBlobWriter(blob, attachments.BackendKindLocal, NewPostgresLocalBlobRepository(runtime, localBlobFixtureWitness(t, runtime), nil))
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("archive shared by two independently purged records")
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	if _, err := fixture.db.Exec(ctx, `insert into public.record_import_jobs(import_job_id,actor_id,idempotency_key,job_state,
		identity_classification,archive_digest,destination_subject_kind,destination_subject_source_id,expires_at)
		values('rij_localshared','usr_aaaaaaaaaaaaaaaaaaaaaaaa','localshared','uploaded','unknown',$1,'vps',$2,clock_timestamp()+interval '1 hour')`, digest[:], testStoreRecordVPSID); err != nil {
		t.Fatal(err)
	}
	written, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
		Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))},
		Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: "rij_localshared", Generation: 1, ArchiveDigest: &digestText}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	var archiveExpiry time.Time
	if err := fixture.db.QueryRow(ctx, `select expires_at from public.record_import_jobs where import_job_id='rij_localshared'`).Scan(&archiveExpiry); err != nil {
		t.Fatal(err)
	}
	publication, err := json.Marshal(map[string]any{"artifact_id": "ria_localshared", "job_id": "rij_localshared", "kind": "archive",
		"backend_kind": "local", "namespace_id": blob.Namespace().NamespaceID, "key": written.Version.Key, "version": written.Version.VersionID,
		"sha256": digestText, "size": len(content), "expires_at": archiveExpiry, "obligation_id": written.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Exec(ctx, `select public.record_import_artifact_publish($1)`, publication); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_import_plans(import_plan_id,import_job_id,plan_digest,object_count,remap_count,expires_at)
		values('rip_localshared','rij_localshared',$1,2,0,clock_timestamp()+interval '1 hour')`, digest[:]); err != nil {
		t.Fatal(err)
	}
	for _, recordID := range []string{first.RecordID, last.RecordID} {
		sourceDigest := sha256.Sum256([]byte(recordID))
		if _, err := fixture.db.Exec(ctx, `insert into public.record_import_entity_mappings(import_plan_id,entity_kind,source_id,source_identity_digest,target_id)
			values('rip_localshared','record',$1,$2,$1)`, recordID, sourceDigest[:]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id='rij_localshared'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind,namespace_id)
		values($1,$2,$3,$4,'local',$5)`, written.Version.Key, digest[:], written.Version.VersionID, len(content), blob.Namespace().NamespaceID); err != nil {
		t.Fatal(err)
	}
	if result, err := fixture.db.Exec(ctx, `update public.attachment_quota_accounts set physical_bytes=$1
		where project_id='default' and logical_bytes=0 and reserved_bytes=0 and physical_bytes=0`, len(content)); err != nil {
		t.Fatal(err)
	} else if result.RowsAffected() != 1 {
		t.Fatal("expected one empty attachment quota account")
	}
	stageExport := func(recordID, revisionID, suffix string) attachments.ObjectVersion {
		t.Helper()
		jobID, artifactID := "rej_localexport"+suffix, "rxa_localexport"+suffix
		body := []byte("export " + suffix + " bytes")
		hash := sha256.Sum256(body)
		var expiry time.Time
		if err := fixture.db.QueryRow(ctx, `insert into public.record_export_jobs
			(export_job_id,actor_id,idempotency_key,export_kind,export_mode,job_state,request_fingerprint,inventory_digest,
			authorization_epoch,record_id,revision_id,owner_id,owner_generation,lease_expires_at,expires_at)
			values($1,'usr_aaaaaaaaaaaaaaaaaaaaaaaa',$1,'markdown','safe','staging',$2,$2,1,$3,$4,'local_export_worker',1,
			clock_timestamp()+interval '30 minutes',clock_timestamp()+interval '1 hour') returning expires_at`,
			jobID, hash[:], recordID, revisionID).Scan(&expiry); err != nil {
			t.Fatal(err)
		}
		result, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
			Origin:  attachments.WriteOrigin{Kind: "export_artifact", ID: jobID, Generation: 1},
			Content: attachments.PutRequest{ExpectedSHA256: hash, ExpectedSizeBytes: int64(len(body))}}, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		command, err := json.Marshal(map[string]any{"artifact_id": artifactID, "job_id": jobID, "kind": "markdown", "content_type": "text/markdown",
			"backend_kind": "local", "namespace_id": blob.Namespace().NamespaceID, "key": result.Version.Key, "version": result.Version.VersionID,
			"sha256": hex.EncodeToString(hash[:]), "size": len(body), "expires_at": expiry, "obligation_id": result.ObligationID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Exec(ctx, `select public.record_export_artifact_publish($1)`, command); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.db.Exec(ctx, `update public.record_export_jobs set job_state='published',owner_id='',owner_generation=0,lease_expires_at=null where export_job_id=$1`, jobID); err != nil {
			t.Fatal(err)
		}
		return result.Version
	}
	targetExport := stageExport(first.RecordID, first.RevisionID, "target")
	survivorExport := stageExport(survivor.RecordID, survivor.RevisionID, "survivor")
	assertExports := func() {
		t.Helper()
		var targetJobs, targetArtifacts, survivorJobs, survivorArtifacts int
		if err := fixture.db.QueryRow(ctx, `select
			(select count(*)::int from public.record_export_jobs where export_job_id='rej_localexporttarget'),
			(select count(*)::int from public.record_export_artifacts where export_artifact_id='rxa_localexporttarget'),
			(select count(*)::int from public.record_export_jobs where export_job_id='rej_localexportsurvivor'),
			(select count(*)::int from public.record_export_artifacts where export_artifact_id='rxa_localexportsurvivor')`).Scan(
			&targetJobs, &targetArtifacts, &survivorJobs, &survivorArtifacts); err != nil {
			t.Fatal(err)
		}
		if targetJobs != 0 || targetArtifacts != 0 || survivorJobs != 1 || survivorArtifacts != 1 {
			t.Fatalf("export cleanup target=%d/%d survivor=%d/%d", targetJobs, targetArtifacts, survivorJobs, survivorArtifacts)
		}
		if _, err := blob.Stat(ctx, targetExport); !errors.Is(err, attachments.ErrBlobNotFound) {
			t.Fatalf("purged export bytes remain: %v", err)
		}
		reader, err := blob.Open(ctx, survivorExport, attachments.FullByteRange())
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || !bytes.Equal(body, []byte("export survivor bytes")) {
			t.Fatalf("surviving export bytes=%q read=%v close=%v", body, readErr, closeErr)
		}
	}
	firstOrigin := sha256.Sum256([]byte("first local archive origin"))
	lastOrigin := sha256.Sum256([]byte("last local archive origin"))
	for index, recordID := range []string{first.RecordID, last.RecordID} {
		originDigest := [][32]byte{firstOrigin, lastOrigin}[index]
		if _, err := fixture.db.Exec(ctx, `insert into public.record_origins(origin_id,origin_kind,origin_digest,source_record_id)
			values ($1,'import',$2,$3)`, []string{"ror_localsharedfirst", "ror_localsharedlast"}[index], originDigest[:], recordID); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.db.Exec(ctx, `insert into public.record_origin_tombstones(origin_digest,ledger_sequence) values($1,11)`, originDigest[:]); err != nil {
			t.Fatal(err)
		}
	}
	assertOrigins := func(want int) {
		t.Helper()
		var origins, tombstones int
		if err := fixture.db.QueryRow(ctx, `select
			(select count(*)::int from public.record_origins where source_record_id in ($1,$2)),
			(select count(*)::int from public.record_origin_tombstones where origin_digest in ($3,$4))`,
			first.RecordID, last.RecordID, firstOrigin[:], lastOrigin[:]).Scan(&origins, &tombstones); err != nil {
			t.Fatal(err)
		}
		if origins != want || tombstones != 2 {
			t.Fatalf("origin/tombstone retention = %d/%d, want %d/2", origins, tombstones, want)
		}
	}
	makeTarget := func(recordID, revisionID, suffix string) recorddeletion.PurgeTarget {
		operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: "rpo_" + suffix, ReservationID: "drs_" + suffix,
			Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: recordID}, ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
			State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
		return recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: seedLocalAttachmentDeletionOperation(t, ctx, fixture, operation, revisionID)}
	}
	firstTarget := makeTarget(first.RecordID, first.RevisionID, "portlocalone")
	lastTarget := makeTarget(last.RecordID, last.RevisionID, "portlocaltwo")
	workerConfig := fixture.directRoleConfig(t, fixture.deletionWorker, "portability-local-worker")
	workerConfig.MaxConns = 2
	workerConfig.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	worker, err := pgxpool.NewWithConfig(ctx, workerConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	coordinator, err := attachments.NewLocalCleanupCoordinator(blob, NewPostgresLocalBlobRepository(worker, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRecordDeletionRepository(worker, allowRecordPlatformAdmissionGate)
	if err := repository.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	_, _, body, err := localAdapterPurgeBinding(coordinator, firstTarget, surface)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Exec(ctx, `select public.local_portability_purge_prepare($1)`, body); err == nil {
		t.Fatal("runtime acquired portability purge authority")
	}
	wrong := firstTarget
	wrong.OwnerClaim.Owner.Generation++
	if _, err := repository.PurgeRecordPortability(ctx, wrong, surface); err == nil {
		t.Fatal("wrong operation owner purged archive")
	}
	firstReceipt, err := repository.PurgeRecordPortability(ctx, firstTarget, surface)
	if err != nil {
		t.Fatal(err)
	}
	var intact bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_import_artifacts where import_job_id='rij_localshared')
		and exists(select 1 from public.record_import_entity_mappings where target_id=$1)
		and exists(select 1 from public.record_write_obligations where obligation_id=$2 and operation_id is null and cleanup_completed_at is null)
		and exists(select 1 from public.blob_gc_deletions where operation_id=$3 and physical_delete_result='retained_referenced')
		and (select physical_bytes from public.attachment_quota_accounts where project_id='default')=$4`, last.RecordID, written.ObligationID, firstTarget.Operation.OperationID, len(content)).Scan(&intact); err != nil || !intact {
		t.Fatalf("first purge stole shared archive: intact=%v err=%v", intact, err)
	}
	assertOrigins(1)
	assertExports()
	reader, err := blob.Open(ctx, written.Version, attachments.FullByteRange())
	if err != nil {
		t.Fatal(err)
	}
	observed, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(observed, content) {
		t.Fatalf("surviving archive bytes=%q read=%v close=%v", observed, readErr, closeErr)
	}
	// Commit durable preparation, lose the process-local objects, and resume from
	// the same pending manifest after the final owner and its artifacts are gone.
	_, _, body, err = localAdapterPurgeBinding(coordinator, lastTarget, surface)
	if err != nil {
		t.Fatal(err)
	}
	var preparation map[string]any
	if err := repository.localPortabilityPurgeCommand(ctx, `select public.local_portability_purge_prepare($1)`, body, &preparation); err != nil {
		t.Fatal(err)
	}
	if err := blob.Close(); err != nil {
		t.Fatal(err)
	}
	blob, err = newPostgresLocalBlobStore(t, ctx, fixture, root)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err = attachments.NewLocalCleanupCoordinator(blob, NewPostgresLocalBlobRepository(worker, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	repository = NewPostgresRecordDeletionRepository(worker, allowRecordPlatformAdmissionGate)
	if err := repository.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	lastReceipt, err := repository.PurgeRecordPortability(ctx, lastTarget, surface)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blob.Stat(ctx, written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("last-owner bytes remain: %v", err)
	}
	var settled bool
	if err := fixture.db.QueryRow(ctx, `select not exists(select 1 from public.record_import_jobs where import_job_id='rij_localshared')
		and exists(select 1 from public.record_write_obligations where obligation_id=$1 and operation_id=$2 and cleanup_completed_at is not null)
		and (select physical_bytes from public.attachment_quota_accounts where project_id='default')=0
		and (select count(*) from public.blob_gc_deletions where blob_key=$3 and deletion_state='completed')=2`, written.ObligationID, lastTarget.Operation.OperationID, written.Version.Key).Scan(&settled); err != nil || !settled {
		t.Fatalf("final archive settlement=%v err=%v", settled, err)
	}
	for _, replay := range []struct {
		target  recorddeletion.PurgeTarget
		receipt recorddeletion.AdapterPurgeReceipt
	}{{firstTarget, firstReceipt}, {lastTarget, lastReceipt}} {
		actual, err := repository.PurgeRecordPortability(ctx, replay.target, surface)
		if err != nil || actual != replay.receipt {
			t.Fatalf("immutable receipt replay=%#v err=%v", actual, err)
		}
		if err := repository.VerifyRecordPortabilityPurge(ctx, replay.target, surface, replay.receipt); err != nil {
			t.Fatal(err)
		}
	}
	assertOrigins(0)
	assertExports()
}
