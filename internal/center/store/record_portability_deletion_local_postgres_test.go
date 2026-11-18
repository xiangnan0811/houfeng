//go:build linux

package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	storemigrate "houfeng/internal/center/store/migrate"
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

// v1LocalCrashFixture starts with a real processor workspace and a separately
// published managed export. The V1 operation is witnessed by the independent
// recovery ledger and all cleanup commands use the admitted worker connection.
type v1LocalCrashFixture struct {
	platform    recordPlatformPostgresFixture
	pool        *pgxpool.Pool
	local       *localPublicationFixture
	worker      *PostgresRecordDeletionRepository
	target      recorddeletion.PurgeTarget
	written     attachments.WrittenBlob
	preparation portabilityV1Preparation
	request     attachments.LocalCleanupPreparation
}

func v1LocalCrashSetup(t *testing.T, ctx context.Context) *v1LocalCrashFixture {
	t.Helper()
	platform := newAdmittedCoreRecordFixture(t, ctx)
	local, recordID, _, _, _, _, measured, _ := seedV2ProcessedWorkspace(t, ctx, platform)
	written := seedV2ExportArtifact(t, ctx, platform, local, recordID)
	record := records.RevisionCommitResult{RecordID: recordID}
	var lock, epoch int64
	if err := platform.db.QueryRow(ctx, `select current_revision_id,lock_version,authorization_epoch from public.records where record_id=$1`, recordID).Scan(&record.RevisionID, &lock, &epoch); err != nil {
		t.Fatal(err)
	}
	record.LockVersion, record.AuthorizationEpoch = uint64(lock), uint64(epoch)
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1localcrash", ReservationID: "drs_v1localcrash",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: recordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, platform, record, op)
	pool := openV2LaunchBoundPool(t, ctx, platform, platform.deletionWorker, "v1-local-crash-worker", measured)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	prep, err := worker.preparePortabilityV1(ctx, target, surface)
	if err != nil || prep.UnresolvedCount != 0 || len(prep.LocalTargets) != 1 {
		t.Fatalf("V1 physical preparation: %#v: %v", prep, err)
	}
	item := prep.LocalTargets[0]
	if item.Key != written.Version.Key || item.ExpectedVersion != written.Version.VersionID {
		t.Fatalf("prepared wrong source: %#v %#v", item, written)
	}
	assertV1SavedInventoryDigest(t, ctx, platform, target.Operation.OperationID, prep.InventoryDigest)
	var linked bool
	if err := platform.db.QueryRow(ctx, `select exists(select 1 from public.record_portability_v1_obligations c
		join public.record_portability_v1_targets t using(operation_id,target_id)
		where c.operation_id=$1 and c.obligation_id=$2 and t.local_deletion_id=$3
		and c.snapshot->>'object_version'=$4 and t.object_version_id=$4)`, target.Operation.OperationID,
		written.ObligationID, item.DeletionID, written.Version.VersionID).Scan(&linked); err != nil || !linked {
		t.Fatalf("original source link: %v: %v", linked, err)
	}
	if _, err := local.blob.Stat(ctx, written.Version); err != nil {
		t.Fatalf("prepared source bytes: %v", err)
	}
	owner := target.OwnerClaim.Owner
	generation := int64(owner.Generation)
	version := item.ExpectedVersion
	request := attachments.LocalCleanupPreparation{LocalCleanupOperation: attachments.LocalCleanupOperation{
		OperationID: &target.Operation.OperationID, OwnerID: &owner.OwnerID, Generation: &generation, ExpiresAt: &owner.ExpiresAt},
		DeletionID: item.DeletionID, NamespaceID: item.NamespaceID, IncarnationID: item.IncarnationID, Key: item.Key, ExpectedVersion: &version}
	return &v1LocalCrashFixture{platform: platform, pool: pool, local: local, worker: worker, target: target, written: written, preparation: prep, request: request}
}

var v1LocalCrashLostResponse = errors.New("test: committed V1 local response lost")
var v1LocalCrashAfterPhysical = errors.New("test: interrupted after actual local deletion")

// The injected error is returned only after the real selected transaction has
// committed. QueryRow identifies the command in that same transaction.
type v1LocalCrashCommitTx struct {
	pgx.Tx
	command string
	matched bool
	fired   *bool
}

func (tx *v1LocalCrashCommitTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, tx.command) {
		tx.matched = true
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func (tx *v1LocalCrashCommitTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	if tx.matched && !*tx.fired {
		*tx.fired = true
		return v1LocalCrashLostResponse
	}
	return nil
}

// Delay only the transaction which has already inserted the candidate receipt;
// its deferred owner check must reject COMMIT after the actual database deadline.
type v1LocalCrashExpiryTx struct {
	pgx.Tx
	t        *testing.T
	fixture  recordPlatformPostgresFixture
	expires  time.Time
	matched  bool
	observed bool
	fired    *bool
}

type v1LocalCrashObservedRow struct {
	pgx.Row
	observed *bool
}

func (row v1LocalCrashObservedRow) Scan(dest ...any) error {
	err := row.Row.Scan(dest...)
	if err == nil {
		*row.observed = true
	}
	return err
}

func (tx *v1LocalCrashExpiryTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	row := tx.Tx.QueryRow(ctx, sql, args...)
	if strings.Contains(sql, "public.record_portability_purge_v1(") {
		tx.matched = true
		return v1LocalCrashObservedRow{Row: row, observed: &tx.observed}
	}
	return row
}

func (tx *v1LocalCrashExpiryTx) Commit(ctx context.Context) error {
	if tx.matched {
		*tx.fired = true
		if !tx.observed {
			tx.t.Fatal("receipt command did not return its candidate before expiry cut")
		}
		waitV2DeadlineInTransaction(tx.t, ctx, tx.fixture, tx.Tx, tx.expires)
	}
	return tx.Tx.Commit(ctx)
}

type v1LocalCrashRepository struct {
	*portabilityV1LocalCleanupRepository
	t                 *testing.T
	fixture           *v1LocalCrashFixture
	resolveCount      int
	stopAfterPhysical bool
	repeatResolve     bool
	identity          attachments.LocalCleanupIdentity
}

func (r *v1LocalCrashRepository) ResolveLocalCleanup(ctx context.Context, identity attachments.LocalCleanupIdentity) (attachments.LocalCleanupSnapshot, error) {
	r.t.Helper()
	var before, after string
	if err := r.fixture.platform.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, identity.DeletionID).Scan(&before); err != nil {
		r.t.Fatal(err)
	}
	if !r.stopAfterPhysical {
		if _, err := r.fixture.local.blob.Stat(ctx, r.fixture.written.Version); err != nil {
			r.t.Fatalf("physical I/O preceded readonly resolution: %v", err)
		}
	}
	snapshot, err := r.portabilityV1LocalCleanupRepository.ResolveLocalCleanup(ctx, identity)
	if err != nil {
		return snapshot, err
	}
	if err := r.fixture.platform.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, identity.DeletionID).Scan(&after); err != nil {
		r.t.Fatal(err)
	}
	if before != after || snapshot.Task.DispatchID == nil || *snapshot.Task.DispatchID != identity.DispatchID || snapshot.Task.OwnerID == nil || *snapshot.Task.OwnerID != identity.OwnerID || snapshot.Task.ExpiresAt == nil || snapshot.Task.Generation == nil {
		r.t.Fatalf("resolve changed task or returned another dispatch: before=%s after=%s task=%#v", before, after, snapshot.Task)
	}
	if r.repeatResolve {
		again, err := r.portabilityV1LocalCleanupRepository.ResolveLocalCleanup(ctx, identity)
		if err != nil {
			return snapshot, err
		}
		firstJSON, err1 := json.Marshal(snapshot)
		secondJSON, err2 := json.Marshal(again)
		if err1 != nil || err2 != nil || !bytes.Equal(firstJSON, secondJSON) {
			r.t.Fatalf("repeated readonly resolve changed snapshot: %s %s", firstJSON, secondJSON)
		}
		var repeated string
		if err := r.fixture.platform.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, identity.DeletionID).Scan(&repeated); err != nil || repeated != before {
			r.t.Fatalf("repeated resolve changed dispatch/lease: %v", err)
		}
	}
	r.resolveCount++
	r.identity = identity
	return snapshot, nil
}

func (r *v1LocalCrashRepository) CompleteLocalCleanup(ctx context.Context, request attachments.LocalCleanupCompletion) (attachments.LocalCleanupSnapshot, error) {
	if r.stopAfterPhysical {
		if _, err := r.fixture.local.blob.Stat(ctx, r.fixture.written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
			r.t.Fatalf("physical cut without actual deletion: %v", err)
		}
		return attachments.LocalCleanupSnapshot{}, v1LocalCrashAfterPhysical
	}
	return r.portabilityV1LocalCleanupRepository.CompleteLocalCleanup(ctx, request)
}

func v1LocalCrashCoordinator(t *testing.T, f *v1LocalCrashFixture, stop bool) (*attachments.LocalCleanupCoordinator, *v1LocalCrashRepository) {
	t.Helper()
	r := &v1LocalCrashRepository{portabilityV1LocalCleanupRepository: &portabilityV1LocalCleanupRepository{
		repository: f.worker, target: f.target, surface: recorddeletion.RecordPortabilitySurfaceDigest()}, t: t, fixture: f, stopAfterPhysical: stop}
	coordinator, err := f.worker.portabilityCleanup.Load().WithRepository(r)
	if err != nil {
		t.Fatal(err)
	}
	return coordinator, r
}

func v1LocalCrashAssertFinished(t *testing.T, ctx context.Context, f *v1LocalCrashFixture) {
	t.Helper()
	if _, err := f.local.blob.Stat(ctx, f.written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("exact local bytes remain: %v", err)
	}
	var proof bool
	err := f.platform.db.QueryRow(ctx, `select g.deletion_state='completed' and g.final_identity_bound and g.final_was_present
		and g.object_version=$3 and g.final_device is not null and g.final_inode is not null and octet_length(g.inventory_digest)=32
		and record_platform_internal.record_portability_v1_local_task_proof(g) and l.seal_obligation
		and o.cleanup_completed_at is not null and o.quota_settled_at is not null and o.cleanup_receipt=g.receipt_digest
		from public.record_portability_v1_obligations c join public.record_portability_v1_targets t using(operation_id,target_id)
		join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
		join public.local_blob_gc_obligations l on l.deletion_id=g.deletion_id and l.obligation_id=c.obligation_id
		join public.record_write_obligations o on o.obligation_id=c.obligation_id
		where c.operation_id=$1 and c.obligation_id=$2`, f.target.Operation.OperationID, f.written.ObligationID, f.written.Version.VersionID).Scan(&proof)
	if err != nil || !proof {
		t.Fatalf("physical source proof=%v err=%v", proof, err)
	}
	assertV1SavedInventoryDigest(t, ctx, f.platform, f.target.Operation.OperationID, f.preparation.InventoryDigest)
	quota := v2DispatchQuota(t, ctx, f.platform)
	receipt, err := f.worker.portabilityV1Finish(ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest(), false)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.worker.PurgeRecordPortability(ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest())
	if err != nil || replay != receipt {
		t.Fatalf("receipt replay=%#v err=%v", replay, err)
	}
	if after := v2DispatchQuota(t, ctx, f.platform); after != quota {
		t.Fatalf("receipt replay settled quota twice: before=%#v after=%#v", quota, after)
	}
	if err := f.worker.VerifyRecordPortabilityPurge(ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest(), receipt); err != nil {
		t.Fatal(err)
	}
}

func runV1PortabilityPhysicalCrashCuts(t *testing.T) {
	for _, tc := range []struct{ name, command string }{
		{"claim_commit_unknown", "record_portability_local_claim_v1"},
		{"bind_commit_unknown", "record_portability_local_bind_final_v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := v1LocalCrashSetup(t, ctx)
			begin := f.worker.platform.beginTx
			fired := false
			f.worker.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
				tx, err := begin(ctx, options)
				if err != nil || fired {
					return tx, err
				}
				return &v1LocalCrashCommitTx{Tx: tx, command: tc.command, fired: &fired}, nil
			}
			coordinator, observer := v1LocalCrashCoordinator(t, f, false)
			_, err := coordinator.Cleanup(ctx, f.request, f.target.OwnerClaim.Owner.OwnerID)
			f.worker.platform.beginTx = begin
			if err != nil || !fired || observer.resolveCount != 1 {
				t.Fatalf("lost COMMIT recovery fired=%v resolves=%d err=%v", fired, observer.resolveCount, err)
			}
			v1LocalCrashAssertFinished(t, ctx, f)
		})
	}
	t.Run("physical_return_loss_restart", func(t *testing.T) {
		ctx := context.Background()
		f := v1LocalCrashSetup(t, ctx)
		quotaBefore := v2DispatchQuota(t, ctx, f.platform)
		coordinator, observer := v1LocalCrashCoordinator(t, f, true)
		_, err := coordinator.Cleanup(ctx, f.request, f.target.OwnerClaim.Owner.OwnerID)
		if !errors.Is(err, attachments.ErrWriteOutcomeUnknown) || observer.resolveCount != 1 {
			t.Fatalf("physical cut did not preserve unknown outcome: %v", err)
		}
		if after := v2DispatchQuota(t, ctx, f.platform); after != quotaBefore {
			t.Fatalf("physical cut prematurely settled quota: before=%#v after=%#v", quotaBefore, after)
		}
		var expiry time.Time
		var pending bool
		if err := f.platform.db.QueryRow(ctx, `select lease_expires_at,deletion_state='claimed' and final_identity_bound and final_was_present and receipt_digest is null from public.blob_gc_deletions where deletion_id=$1`, f.request.DeletionID).Scan(&expiry, &pending); err != nil || !pending {
			t.Fatalf("physical cut state=%v err=%v", pending, err)
		}
		v2DispatchWaitDeadline(t, ctx, f.platform, expiry)
		// Retire the original operation owner after the committed physical cut;
		// a new worker must authenticate its own tuple before resuming.
		old := f.target
		result, err := f.platform.db.Exec(ctx, `update public.record_purge_operations set owner_expires_at=clock_timestamp()-interval '1 second'
			where operation_id=$1 and owner_id=$2 and owner_generation=$3 and owner_expires_at=$4`, old.Operation.OperationID,
			old.OwnerClaim.Owner.OwnerID, old.OwnerClaim.Owner.Generation, old.OwnerClaim.Owner.ExpiresAt)
		if err != nil || result.RowsAffected() != 1 {
			t.Fatalf("expire original operation owner: %v", err)
		}
		work, err := f.worker.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v1_local_restart_successor", OwnerLeaseDuration: 2 * time.Minute})
		if err != nil || work == nil || work.Operation.OperationID != old.Operation.OperationID || work.Owner.Generation != old.OwnerClaim.Owner.Generation+1 {
			t.Fatalf("restart takeover=%#v err=%v", work, err)
		}
		if _, err := observer.portabilityV1LocalCleanupRepository.ResolveLocalCleanup(ctx, observer.identity); err == nil {
			t.Fatal("stale owner resolved after takeover")
		}
		f.target = recorddeletion.PurgeTarget{Operation: work.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: work.Operation.OperationID, Owner: work.Owner}}
		// Reopen the repository against the same admitted worker pool and local namespace.
		fresh := NewPostgresRecordDeletionRepository(f.pool, allowRecordPlatformAdmissionGate)
		base, err := attachments.NewLocalCleanupCoordinator(f.local.blob, NewPostgresLocalBlobRepository(f.pool, nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		if err := fresh.ConfigurePortabilityDeletionLocalCleanup(base); err != nil {
			t.Fatal(err)
		}
		f.worker = fresh
		if _, err := f.worker.PurgeRecordPortability(ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest()); err != nil {
			t.Fatalf("restart exact absent completion: %v", err)
		}
		v1LocalCrashAssertFinished(t, ctx, f)
	})
	t.Run("owner_expiry_receipt_rollback", func(t *testing.T) {
		ctx := context.Background()
		f := v1LocalCrashSetup(t, ctx)
		coordinator, _ := v1LocalCrashCoordinator(t, f, false)
		if _, err := coordinator.Cleanup(ctx, f.request, f.target.OwnerClaim.Owner.OwnerID); err != nil {
			t.Fatal(err)
		}
		var before string
		if err := f.platform.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, f.request.DeletionID).Scan(&before); err != nil {
			t.Fatal(err)
		}
		var expiry time.Time
		if err := f.platform.db.QueryRow(ctx, `update public.record_purge_operations set owner_expires_at=clock_timestamp()+interval '5 seconds'
			where operation_id=$1 and owner_id=$2 and owner_generation=$3 and owner_expires_at=$4 returning owner_expires_at`,
			f.target.Operation.OperationID, f.target.OwnerClaim.Owner.OwnerID, f.target.OwnerClaim.Owner.Generation, f.target.OwnerClaim.Owner.ExpiresAt).Scan(&expiry); err != nil {
			t.Fatal(err)
		}
		f.target.OwnerClaim.Owner.ExpiresAt = expiry
		begin := f.worker.platform.beginTx
		fired := false
		f.worker.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			tx, err := begin(ctx, options)
			if err != nil || fired {
				return tx, err
			}
			return &v1LocalCrashExpiryTx{Tx: tx, t: t, fixture: f.platform, expires: expiry, fired: &fired}, nil
		}
		_, err := f.worker.portabilityV1Finish(ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest(), false)
		f.worker.platform.beginTx = begin
		if !fired || err == nil {
			t.Fatalf("expired owner committed receipt: fired=%v err=%v", fired, err)
		}
		var after string
		var receipts int
		if err := f.platform.db.QueryRow(ctx, `select to_jsonb(g)::text,(select count(*) from public.record_portability_purge_receipts where operation_id=$2)
			from public.blob_gc_deletions g where deletion_id=$1`, f.request.DeletionID, f.target.Operation.OperationID).Scan(&after, &receipts); err != nil || before != after || receipts != 0 {
			t.Fatalf("expiry rollback task unchanged=%v receipts=%d err=%v", before == after, receipts, err)
		}
		old := f.target
		work, err := f.worker.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v1_local_crash_successor", OwnerLeaseDuration: 2 * time.Minute})
		if err != nil || work == nil || work.Operation.OperationID != old.Operation.OperationID || work.Owner.Generation != old.OwnerClaim.Owner.Generation+1 {
			t.Fatalf("takeover=%#v err=%v", work, err)
		}
		f.target = recorddeletion.PurgeTarget{Operation: work.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: work.Operation.OperationID, Owner: work.Owner}}
		if _, err := f.worker.portabilityV1Finish(ctx, old, recorddeletion.RecordPortabilitySurfaceDigest(), false); err == nil {
			t.Fatal("stale owner wrote receipt after takeover")
		}
		v1LocalCrashAssertFinished(t, ctx, f)
		if err := f.platform.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, f.request.DeletionID).Scan(&after); err != nil || after != before {
			t.Fatalf("takeover rewrote historic physical proof: %v", err)
		}
	})
	t.Run("resolve_no_lease_extension", func(t *testing.T) {
		ctx := context.Background()
		f := v1LocalCrashSetup(t, ctx)
		begin := f.worker.platform.beginTx
		fired := false
		f.worker.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			tx, err := begin(ctx, options)
			if err != nil || fired {
				return tx, err
			}
			return &v1LocalCrashCommitTx{Tx: tx, command: "record_portability_local_bind_final_v1", fired: &fired}, nil
		}
		coordinator, observer := v1LocalCrashCoordinator(t, f, false)
		observer.repeatResolve = true
		_, err := coordinator.Cleanup(ctx, f.request, f.target.OwnerClaim.Owner.OwnerID)
		f.worker.platform.beginTx = begin
		if err != nil || !fired || observer.resolveCount != 1 {
			t.Fatalf("repeated resolve fired=%v count=%d err=%v", fired, observer.resolveCount, err)
		}
		v1LocalCrashAssertFinished(t, ctx, f)
	})
	t.Run("closed_no_final_sources", func(t *testing.T) {
		t.Run("registered_absent", TestPostgresIntegrationRecordDeletionV1ObligationOnlyAbsent)
		t.Run("failed_absent", func(t *testing.T) { v1LocalCrashNoFinal(t, "failed") })
		t.Run("local_excluded_absent", func(t *testing.T) { v1LocalCrashNoFinal(t, "local_excluded") })
		// Labelled consumer-recovery fault state: authentic failed/NULL return
		// plus an uncertain final-link effect onto the same registered inode.
		t.Run("failed_present_fault_state", func(t *testing.T) { v1LocalCrashNoFinal(t, "failed_present") })
	})
}

func v1LocalCrashNoFinal(t *testing.T, mode string) {
	excluded, present := mode == "local_excluded", mode == "failed_present"
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	actor := storeEvidenceParticipantActor(t)
	witness := localBlobFixtureWitness(t, fixture.Runtime)
	imports := NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, witness, nil)
	content := []byte("real managed writer failed before final publication")
	digest := sha256.Sum256(content)
	hexDigest := hex.EncodeToString(digest[:])
	destination := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
		Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true}
	expires := time.Now().UTC().Add(time.Hour)
	job, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID, IdempotencyKey: "v1-crash-failed-absent",
		ArchiveDigest: digest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	var obligation string
	var original bool
	if excluded {
		namespace, incarnation := fixture.LocalCleanup.NamespaceID(), fixture.LocalCleanup.IncarnationID()
		obligation = "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", "")
		registration := attachments.WriteRegistration{WriteOrigin: attachments.WriteOrigin{Kind: "import_archive", ID: job.ImportJobID,
			Generation: int64(job.LockVersion), ArchiveDigest: &hexDigest}, ObligationID: obligation,
			WriterInstance: localBlobFixtureUUID(t), RequestSeq: 1, ProjectID: "default", NamespaceID: &namespace, IncarnationID: &incarnation,
			BackendKind: attachments.BackendKindLocal, Key: "sha256/" + hexDigest, SHA256: hexDigest, Size: int64(len(content))}
		if _, err := NewPostgresLocalBlobRepository(fixture.Runtime, witness, nil).RegisterWrite(ctx, registration); err != nil {
			t.Fatal(err)
		}
		if err := fixture.platform.db.QueryRow(ctx, `select outcome='registered' and dispatched_at is null and object_version is null
			from public.record_write_obligations where obligation_id=$1`, obligation).Scan(&original); err != nil || !original {
			t.Fatalf("registered original=%v err=%v", original, err)
		}
	} else if present {
		v1LocalCrashFailedPresentSource(t, ctx, fixture, job.ImportJobID, int64(job.LockVersion), content)
		if err := fixture.platform.db.QueryRow(ctx, `select obligation_id,outcome='failed' and dispatched_at is not null and returned_at is not null
			and object_version is null and temporary_inode is not null from public.record_write_obligations where origin_kind='import_archive' and origin_id=$1`, job.ImportJobID).Scan(&obligation, &original); err != nil || !original {
			t.Fatalf("failed source return=%v err=%v", original, err)
		}
	} else {
		_, err = fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
			Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: job.ImportJobID, Generation: int64(job.LockVersion), ArchiveDigest: &hexDigest},
			Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content[:len(content)-1]))
		if !errors.Is(err, attachments.ErrBlobSizeMismatch) {
			t.Fatalf("managed writer did not fail before final: %v", err)
		}
		if err := fixture.platform.db.QueryRow(ctx, `select obligation_id,outcome='failed' and dispatched_at is not null and returned_at is not null
			and object_version is null and temporary_inode is not null from public.record_write_obligations where origin_kind='import_archive' and origin_id=$1`, job.ImportJobID).Scan(&obligation, &original); err != nil || !original {
			t.Fatalf("failed source return=%v err=%v", original, err)
		}
	}
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_v1failedabsent")
	planDigest := sha256.Sum256([]byte("V1 failed archive mapping"))
	_, err = imports.SaveImportPlan(ctx, SaveRecordImportPlanInput{ImportJobID: job.ImportJobID, Actor: actor, ArchiveDigest: digest,
		PlanDigest: planDigest, ObjectCount: 1, RemapCount: 1, Allocations: []recordbirth.Allocation{allocation},
		Remaps:             []ImportRemap{{EntityKind: "record", SourceID: "rec_v1failedabsent", TargetID: allocation.RecordID, BirthAllocationID: allocation.AllocationID}},
		Documents:          []ImportDocumentPlan{{SourceID: "rec_v1failedabsent", TargetID: allocation.RecordID, Title: "Failed source", Body: "Body"}},
		DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	fixture.RefreshHeartbeat()
	if err := fixture.CommitImported(ctx, "CommitRevision", [][]attachments.ImportedAvailableAttachment{{}}, []recordbirth.Allocation{allocation}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id=$1`, job.ImportJobID); err != nil {
		t.Fatal(err)
	}
	record := records.RevisionCommitResult{RecordID: allocation.RecordID}
	var lock, epoch int64
	if err := fixture.platform.db.QueryRow(ctx, `select current_revision_id,lock_version,authorization_epoch from public.records where record_id=$1`, record.RecordID).Scan(&record.RevisionID, &lock, &epoch); err != nil {
		t.Fatal(err)
	}
	record.LockVersion, record.AuthorizationEpoch = uint64(lock), uint64(epoch)
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_v1failedabsent", ReservationID: "drs_v1failedabsent",
		Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID}, ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture.platform, record, op)
	pool := fixture.platform.openDirectRolePool(t, ctx, fixture.platform.deletionWorker, "v1-failed-absent-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(fixture.LocalCleanup); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	prep, err := worker.preparePortabilityV1(ctx, target, surface)
	if err != nil || prep.UnresolvedCount != 0 || len(prep.LocalTargets) != 1 || prep.LocalTargets[0].ExpectedVersion != "local-v2-"+obligation {
		t.Fatalf("failed source preparation=%#v err=%v", prep, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture.platform, target.Operation.OperationID, prep.InventoryDigest)
	var source bool
	wantCaptured, wantCurrent := "failed", "failed"
	if excluded {
		wantCaptured, wantCurrent = "registered", "local_excluded"
	}
	if err := fixture.platform.db.QueryRow(ctx, `select c.snapshot->>'outcome'=$3 and o.outcome=$4 and c.snapshot->>'object_version' is null
		and t.object_version_id is null and t.obligation_id=c.obligation_id and not exists(select 1 from public.record_portability_v1_artifacts a where a.operation_id=c.operation_id)
		from public.record_portability_v1_obligations c join public.record_portability_v1_targets t using(operation_id,target_id)
		join public.record_write_obligations o on o.obligation_id=c.obligation_id
		where c.operation_id=$1 and c.obligation_id=$2`, target.Operation.OperationID, obligation, wantCaptured, wantCurrent).Scan(&source); err != nil || !source {
		t.Fatalf("saved source and exclusion=%v err=%v", source, err)
	}
	receipt, err := worker.PurgeRecordPortability(ctx, target, surface)
	if err != nil {
		t.Fatal(err)
	}
	// Present and absent bindings come only from the coordinator's own
	// inspection; the saved target/snapshot version remains the stored NULL.
	var proof bool
	if err := fixture.platform.db.QueryRow(ctx, `select g.deletion_state='completed' and g.final_identity_bound and g.final_was_present=$3
		and g.object_version is not distinct from (case when $3 then 'local-v2-'||c.obligation_id end)
		and g.physical_delete_result=(case when $3 then 'deleted' else 'already_absent' end)
		and (g.final_inode is not null)=$3 and l.seal_obligation and t.object_version_id is null and c.snapshot->>'object_version' is null
		and o.object_version is null and o.cleanup_completed_at is not null and o.cleanup_receipt=g.receipt_digest
		and record_platform_internal.record_portability_v1_local_task_proof(g)
		from public.record_portability_v1_obligations c join public.record_portability_v1_targets t using(operation_id,target_id)
		join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
		join public.local_blob_gc_obligations l on l.deletion_id=g.deletion_id and l.obligation_id=c.obligation_id
		join public.record_write_obligations o on o.obligation_id=c.obligation_id
		where c.operation_id=$1 and c.obligation_id=$2`, target.Operation.OperationID, obligation, present).Scan(&proof); err != nil || !proof {
		t.Fatalf("no-final physical proof present=%v proof=%v err=%v", present, proof, err)
	}
	if present {
		root := fixture.Blobs.(*attachments.LocalBlobStore).Namespace().RootPath
		for _, name := range []string{"sha256/" + hexDigest, "sha256/.blob-tmp-" + obligation} {
			if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("fault-state name %s remains: %v", name, err)
			}
		}
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err != nil {
		t.Fatal(err)
	}
	if replay, err := worker.PurgeRecordPortability(ctx, target, surface); err != nil || replay != receipt {
		t.Fatalf("no-final receipt replay=%#v err=%v", replay, err)
	}
}

// v1LocalCrashFailedPresentSource drives the real managed writer in a child
// process to a fully copied, synced and registered temporary inode, then fails
// the before-publish boundary by closing the control pipe. The writer itself
// records its authentic failed/NULL return. The test then models an uncertain
// link effect by hard-linking the final name to that exact registered inode.
// This is a consumer-recovery fault state, not a current writer outcome.
func v1LocalCrashFailedPresentSource(t *testing.T, ctx context.Context, fixture ImportAttachmentGCFixture, job string, generation int64, content []byte) {
	t.Helper()
	if generation != 1 {
		t.Fatalf("process writer uses origin generation 1, job lock version=%d", generation)
	}
	blob, ok := fixture.Blobs.(*attachments.LocalBlobStore)
	if !ok {
		t.Fatalf("local fixture store has type %T", fixture.Blobs)
	}
	namespace := blob.Namespace()
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "attachments-process.test")
	build := exec.Command("go", "test", "-c", "-o", binary, "./internal/center/attachments")
	build.Dir = repo
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build process fixture: %v\n%s", err, output)
	}
	process := startLocalProtocolProcess(t, binary, []string{
		"HOUFENG_LOCAL_PROCESS_ROOT=" + namespace.RootPath,
		"HOUFENG_LOCAL_PROCESS_DATABASE_URL=" + contentProcessorCrashDatabaseURL(fixture.platform.directRoleConfig(t, fixture.platform.runtime, "v1-crash-failed-present")),
		"HOUFENG_LOCAL_PROCESS_MODE=put", "HOUFENG_LOCAL_PROCESS_JOB=" + job,
		"HOUFENG_LOCAL_PROCESS_CONTENT=" + string(content), "HOUFENG_LOCAL_PROCESS_CUTPOINT=before-publish",
	})
	process.await(t, 'B')
	facts := readLocalProcessWriteFacts(t, ctx, fixture.platform, job)
	if !facts.dispatched || !facts.created || facts.returned || facts.inode <= 0 {
		t.Fatalf("before-publish facts: %#v", facts)
	}
	// EOF on the private control pipe makes the real cutpoint return an error.
	if err := process.control.Close(); err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	if err := process.wait(false); !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("writer did not fail at before-publish: %v\n%s", err, process.output.String())
	}
	facts = readLocalProcessWriteFacts(t, ctx, fixture.platform, job)
	if !facts.returned || facts.outcome != "failed" || facts.version != "" {
		t.Fatalf("authentic failed return: %#v", facts)
	}
	digest := sha256.Sum256(content)
	finalPath := filepath.Join(namespace.RootPath, "sha256", hex.EncodeToString(digest[:]))
	tempPath := filepath.Join(namespace.RootPath, facts.temporary)
	if _, err := os.Lstat(finalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("writer published final: %v", err)
	}
	var temp unix.Stat_t
	if err := unix.Lstat(tempPath, &temp); err != nil || int64(temp.Dev) != facts.device || int64(temp.Ino) != facts.inode {
		t.Fatalf("registered temporary inode: %#v %v", temp, err)
	}
	if body, err := os.ReadFile(tempPath); err != nil || !bytes.Equal(body, content) {
		t.Fatalf("registered temporary bytes: %v", err)
	}
	attribute := make([]byte, 128)
	size, err := unix.Getxattr(tempPath, "user.houfeng.blob-materialization", attribute)
	if err != nil || string(attribute[:size]) != "local-v2-"+facts.obligationID {
		t.Fatalf("temporary source xattr=%q err=%v", attribute[:max(size, 0)], err)
	}
	if err := os.Link(tempPath, finalPath); err != nil {
		t.Fatal(err)
	}
	var final unix.Stat_t
	if err := unix.Lstat(finalPath, &final); err != nil || final.Ino != temp.Ino || final.Dev != temp.Dev {
		t.Fatalf("fault-state link inode: %#v %v", final, err)
	}
}
