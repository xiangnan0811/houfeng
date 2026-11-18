package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationAttachmentDeletionPreservesSharedAndPurgesExclusiveBlob(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion", 2)
	recordRepository := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate})
	targetRecord, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, recordsPostgresCompleteRevisionInput(t, "Attachment deletion target"), "attachment-deletion-target"))
	if err != nil {
		t.Fatalf("CommitRevision(target) error = %v", err)
	}
	otherRecord, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, recordsPostgresCompleteRevisionInput(t, "Attachment deletion survivor"), "attachment-deletion-survivor"))
	if err != nil {
		t.Fatalf("CommitRevision(other) error = %v", err)
	}

	blobStore, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "blob-root"))
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	sharedWrite := seedPostgresManagedLocalBlob(t, ctx, fixture, blobStore, []byte("shared attachment deletion content\n"))
	exclusiveWrite := seedPostgresManagedLocalBlob(t, ctx, fixture, blobStore, []byte("exclusive attachment deletion content\n"))
	shared, exclusive := sharedWrite.Version, exclusiveWrite.Version
	for _, object := range []attachments.ObjectVersion{shared, exclusive} {
		if _, err := fixture.db.Exec(ctx, `
			insert into public.blob_objects (
				blob_key, sha256_digest, object_version, size_bytes, backend_kind, namespace_id
			) values ($1, $2, $3, $4, 'local', $5)`,
			object.Key, object.SHA256[:], object.VersionID, object.SizeBytes, blobStore.Namespace().NamespaceID,
		); err != nil {
			t.Fatalf("insert Blob metadata: %v", err)
		}
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.blob_gc_pins(pin_id,pin_owner_kind,pin_owner_id,
		blob_key,blob_object_version,created_at,expires_at) values('bgp_expiredpurge','revision_transaction','fixture',
		$1,$2,clock_timestamp()-interval '2 hours',clock_timestamp()-interval '1 hour')`, exclusive.Key, exclusive.VersionID); err != nil {
		t.Fatal(err)
	}
	attachmentRows := []struct {
		attachmentID string
		recordID     string
		originDraft  any
		object       attachments.ObjectVersion
	}{
		{attachmentID: "att_deleteshared", recordID: targetRecord.RecordID, object: shared},
		{attachmentID: "att_deleteexclusive", recordID: targetRecord.RecordID, originDraft: "rdf_deleteexclusive", object: exclusive},
		{attachmentID: "att_deletecopy", recordID: otherRecord.RecordID, object: shared},
	}
	for _, row := range attachmentRows {
		if _, err := fixture.db.Exec(ctx, `
				insert into public.record_attachments (
					attachment_id, project_id, record_id, origin_draft_id, attachment_state,
					display_name, media_type, logical_size_bytes,
					blob_key, blob_object_version, created_by
				) values ($1, 'default', $2, $3, 'available', $4, 'text/plain', $5, $6, $7, 'usr_records1')`,
			row.attachmentID, row.recordID, row.originDraft, row.attachmentID+".txt", row.object.SizeBytes,
			row.object.Key, row.object.VersionID,
		); err != nil {
			t.Fatalf("insert logical attachment %s: %v", row.attachmentID, err)
		}
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, project_id, record_id, attachment_state,
			display_name, media_type, logical_size_bytes, created_by
		) values ('att_deleterejected', 'default', $1, 'rejected',
		  'rejected.txt', 'text/plain', 17, 'usr_records1')`, targetRecord.RecordID); err != nil {
		t.Fatalf("insert released rejected attachment: %v", err)
	}
	for ordinal, attachmentID := range []string{"att_deleteshared", "att_deleteexclusive"} {
		if _, err := fixture.db.Exec(ctx, `
			insert into public.record_revision_attachments (
				record_id, revision_id, ordinal, attachment_id
			) values ($1, $2, $3, $4)`,
			targetRecord.RecordID, targetRecord.RevisionID, ordinal, attachmentID,
		); err != nil {
			t.Fatalf("insert target revision attachment: %v", err)
		}
	}
	if _, err := fixture.db.Exec(ctx, `
			insert into public.record_revision_attachments (
				record_id, revision_id, ordinal, attachment_id
			) values ($1, $2, 0, 'att_deletecopy')`, otherRecord.RecordID, otherRecord.RevisionID); err != nil {
		t.Fatalf("insert surviving revision attachment: %v", err)
	}
	resultDigest := testStoreRecordPlatformDigest(0x94)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_uploads (
			upload_id, project_id, attachment_id, origin_draft_id, author_id,
			upload_state, transport_kind, declared_size_bytes, reserved_size_bytes,
			actual_size_bytes, actual_sha256_digest, completion_fingerprint,
			completed_at, expires_at
		) values ('aup_deleteexclusive', 'default', 'att_deleteexclusive',
		  'rdf_deleteexclusive', 'usr_records1', 'available', 'local', $1, $1,
		  $1, $2, $3, transaction_timestamp(), transaction_timestamp() + interval '1 hour')`,
		exclusive.SizeBytes, exclusive.SHA256[:], resultDigest[:]); err != nil {
		t.Fatalf("insert terminal attachment upload: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_upload_parts (
			upload_id, part_number, size_bytes, sha256_digest, object_version, namespace_id, obligation_id
		) values ('aup_deleteexclusive', 1, $1, $2, $3, $4, $5)`,
		exclusive.SizeBytes, exclusive.SHA256[:], exclusive.VersionID, blobStore.Namespace().NamespaceID, exclusiveWrite.ObligationID); err != nil {
		t.Fatalf("insert terminal attachment upload part: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_processor_jobs (
			processor_job_id, upload_id, attachment_id, processor_state,
			processor_profile, attempt, max_attempts, result_code, result_digest,
			result_owner_id, result_lease_expires_at, expires_at
		) values ('apj_deleteexclusive', 'aup_deleteexclusive', 'att_deleteexclusive',
		  'succeeded', 'text', 1, 3, 'clean', $1,
		  'processor_deleteexclusive', transaction_timestamp() + interval '1 hour',
		  transaction_timestamp() + interval '1 hour')`, resultDigest[:]); err != nil {
		t.Fatalf("insert terminal attachment processor job: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.content_processor_workspaces (
			workspace_id, processor_job_id, attempt, workspace_state,
			workspace_path_digest, expires_at, purged_at
		) values ('cpw_deleteexclusive', 'apj_deleteexclusive', 1, 'purged',
		  $1, transaction_timestamp() + interval '1 hour', transaction_timestamp())`, resultDigest[:]); err != nil {
		t.Fatalf("insert purged attachment processor workspace: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_publication_intents (
			publication_id, project_id, owner_kind, owner_id, owner_generation,
			blob_key, sha256_digest, size_bytes, backend_kind, object_version,
			publication_state, publish_expires_at, completion_outcome,
			receipt_digest, completed_at
		) values ('bpi_deleteexclusive', 'default', 'upload', 'aup_deleteexclusive', 1,
		  $1, $2, $3, 'local', $4, 'completed',
		  transaction_timestamp() + interval '1 hour', 'consumed', $5,
		  transaction_timestamp())`,
		exclusive.Key, exclusive.SHA256[:], exclusive.SizeBytes, exclusive.VersionID, resultDigest[:],
	); err != nil {
		t.Fatalf("insert completed attachment publication intent: %v", err)
	}
	logicalBytes := shared.SizeBytes*2 + exclusive.SizeBytes
	physicalBytes := shared.SizeBytes + exclusive.SizeBytes
	if result, err := fixture.db.Exec(ctx, `
		update public.attachment_quota_accounts
		set logical_bytes = $1, physical_bytes = $2
		where project_id = 'default' and logical_bytes = 0
		  and reserved_bytes = 0 and physical_bytes = 0`, logicalBytes, physicalBytes); err != nil {
		t.Fatalf("initialize attachment quota: %v", err)
	} else if result.RowsAffected() != 1 {
		t.Fatal("expected one empty attachment quota account")
	}

	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: "rpo_attdeleteone", ReservationID: "drs_attdeleteone",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: targetRecord.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7,
	}
	claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, operation, targetRecord.RevisionID)
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "local-attachment-purge", 2)
	coordinator, err := attachments.NewLocalCleanupCoordinator(blobStore, NewPostgresLocalBlobRepository(workerPool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresAttachmentRepository(workerPool)
	if err := repository.ConfigureAttachmentDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatal(err)
	}
	target := recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}
	wrongOwner := target
	wrongOwner.OwnerClaim.Owner.Generation++
	if _, err := adapter.PurgeDeletion(ctx, wrongOwner); err == nil {
		t.Fatal("unclaimed operation generation removed attachments")
	}
	var unchanged int
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_attachments where record_id=$1`, targetRecord.RecordID).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged != 3 {
		t.Fatalf("invalid owner changed attachment set: %d", unchanged)
	}
	command := attachments.AttachmentDeletionCommand{PurgeTarget: target, SurfaceDigest: recorddeletion.RecordAttachmentsSurfaceDigest()}
	_, preparationRequest, body, err := repository.localAttachmentPurgeBinding(command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimePool.Exec(ctx, `select public.local_attachment_purge_prepare($1)`, body); err == nil {
		t.Fatal("runtime acquired destructive preparation authority")
	}
	// Another adapter may have prepared this shared key for the same operation.
	// It has no cleanup owner or frozen inventory yet.
	if _, err := NewPostgresLocalBlobRepository(workerPool, nil, nil).PrepareLocalCleanup(ctx, attachments.LocalCleanupPreparation{
		LocalCleanupOperation: preparationRequest.LocalCleanupOperation,
		DeletionID:            "bgd_sharedpreparation", NamespaceID: preparationRequest.NamespaceID,
		IncarnationID: preparationRequest.IncarnationID, Key: shared.Key,
	}); err != nil {
		t.Fatal(err)
	}
	var pending map[string]any
	if err := repository.localAttachmentPurgeCommand(ctx, `select public.local_attachment_purge_prepare($1)`, body, &pending); err != nil {
		t.Fatal(err)
	}
	var pendingTargets, remainingOwners, earlyReceipts int
	if err := fixture.db.QueryRow(ctx, `select
		(select count(*) from public.blob_gc_deletions where operation_id=$1 and deletion_state='pending' and owner_id is null),
		(select count(*) from public.record_attachments where record_id=$2),
		(select count(*) from public.attachment_purge_receipts where operation_id=$1)`, operation.OperationID, targetRecord.RecordID,
	).Scan(&pendingTargets, &remainingOwners, &earlyReceipts); err != nil {
		t.Fatal(err)
	}
	if pendingTargets != 2 || remainingOwners != 0 || earlyReceipts != 0 {
		t.Fatalf("handoff before IO: pending=%d owners=%d receipts=%d", pendingTargets, remainingOwners, earlyReceipts)
	}
	if _, err := blobStore.Stat(ctx, exclusive); err != nil {
		t.Fatalf("prepare mutated filesystem: %v", err)
	}
	root := blobStore.Namespace().RootPath
	if err := blobStore.Close(); err != nil {
		t.Fatal(err)
	}
	blobStore, err = newPostgresLocalBlobStore(t, ctx, fixture, root)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err = attachments.NewLocalCleanupCoordinator(blobStore, NewPostgresLocalBlobRepository(workerPool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	repository = NewPostgresAttachmentRepository(workerPool)
	if err := repository.ConfigureAttachmentDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	adapter, err = attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := adapter.PurgeDeletion(ctx, target)
	if err != nil {
		t.Fatalf("PurgeDeletion() error = %v", err)
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		t.Fatalf("VerifyDeletion() error = %v", err)
	}
	replay, err := adapter.PurgeDeletion(ctx, target)
	if err != nil || replay != receipt {
		t.Fatalf("PurgeDeletion(replay) = %#v, %v, want %#v", replay, err, receipt)
	}

	var targetAttachmentCount, targetRefCount, survivorCount, sharedBlobCount, exclusiveBlobCount int
	var uploadCount, partCount, jobCount, workspaceCount, completedPublicationCount int
	var logicalAfter, physicalAfter int64
	var gcCompleted, purgeReceiptCount int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_attachments where record_id = $1),
		       (select count(*)::int from public.record_revision_attachments where record_id = $1),
		       (select count(*)::int from public.record_attachments where attachment_id = 'att_deletecopy'),
		       (select count(*)::int from public.blob_objects where blob_key = $2 and object_version = $3),
		       (select count(*)::int from public.blob_objects where blob_key = $4 and object_version = $5),
		       (select logical_bytes from public.attachment_quota_accounts where project_id = 'default'),
		       (select physical_bytes from public.attachment_quota_accounts where project_id = 'default'),
		       (select count(*)::int from public.blob_gc_deletions
		         where purge_mode = 'permanent' and blob_key = $4 and deletion_state = 'completed'),
			       (select count(*)::int from public.attachment_purge_receipts where operation_id = $6)`,
		targetRecord.RecordID,
		shared.Key, shared.VersionID,
		exclusive.Key, exclusive.VersionID,
		operation.OperationID,
	).Scan(
		&targetAttachmentCount, &targetRefCount, &survivorCount,
		&sharedBlobCount, &exclusiveBlobCount, &logicalAfter, &physicalAfter,
		&gcCompleted, &purgeReceiptCount,
	); err != nil {
		t.Fatalf("read attachment deletion result: %v", err)
	}
	if targetAttachmentCount != 0 || targetRefCount != 0 || survivorCount != 1 ||
		sharedBlobCount != 1 || exclusiveBlobCount != 0 || logicalAfter != shared.SizeBytes ||
		physicalAfter != shared.SizeBytes || gcCompleted != 1 || purgeReceiptCount != 3 {
		t.Fatalf("deletion result target/ref/survivor blobs logical/physical gc/receipt = %d/%d/%d %d/%d %d/%d %d/%d",
			targetAttachmentCount, targetRefCount, survivorCount, sharedBlobCount, exclusiveBlobCount,
			logicalAfter, physicalAfter, gcCompleted, purgeReceiptCount)
	}
	if _, err := blobStore.Stat(ctx, exclusive); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("exclusive Blob Stat() error = %v, want ErrBlobNotFound", err)
	}
	if _, err := blobStore.Stat(ctx, shared); err != nil {
		t.Fatalf("shared Blob Stat() error = %v", err)
	}
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.attachment_uploads where upload_id = 'aup_deleteexclusive'),
		       (select count(*)::int from public.attachment_upload_parts where upload_id = 'aup_deleteexclusive'),
		       (select count(*)::int from public.attachment_processor_jobs where processor_job_id = 'apj_deleteexclusive'),
		       (select count(*)::int from public.content_processor_workspaces where workspace_id = 'cpw_deleteexclusive'),
		       (select count(*)::int from public.blob_publication_intents
		         where publication_id = 'bpi_deleteexclusive' and publication_state = 'completed')`,
	).Scan(&uploadCount, &partCount, &jobCount, &workspaceCount, &completedPublicationCount); err != nil {
		t.Fatalf("read terminal attachment dependency cleanup: %v", err)
	}
	if uploadCount != 0 || partCount != 0 || jobCount != 0 || workspaceCount != 0 || completedPublicationCount != 1 {
		t.Fatalf("terminal upload/part/job/workspace and completed publication counts = %d/%d/%d/%d/%d, want 0/0/0/0/1",
			uploadCount, partCount, jobCount, workspaceCount, completedPublicationCount)
	}
	var retained, absent int
	if err := fixture.db.QueryRow(ctx, `select
		count(*) filter(where physical_delete_result='retained_referenced'),
		count(*) filter(where physical_delete_result in ('deleted','already_absent'))
		from public.blob_gc_deletions where operation_id=$1`, operation.OperationID).Scan(&retained, &absent); err != nil {
		t.Fatal(err)
	}
	if retained != 1 || absent != 1 {
		t.Fatalf("shared/exclusive physical outcomes: retained=%d absent=%d", retained, absent)
	}
	var unadoptedArchives int
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_write_obligations
		where obligation_id=any($1::text[]) and operation_id is null and cleanup_completed_at is null`,
		[]string{sharedWrite.ObligationID, exclusiveWrite.ObligationID}).Scan(&unadoptedArchives); err != nil {
		t.Fatal(err)
	}
	if unadoptedArchives != 2 {
		t.Fatalf("attachment purge sealed unrelated archive responsibilities: preserved=%d", unadoptedArchives)
	}
	if _, err := fixture.db.Exec(ctx, `update public.record_purge_operations set owner_expires_at=clock_timestamp()-interval '1 second'
		where operation_id=$1`, operation.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.PurgeDeletion(ctx, target); err == nil {
		t.Fatal("stale operation owner replayed completed purge")
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err == nil {
		t.Fatal("stale operation owner verified completed purge")
	}
}

func TestPostgresIntegrationAttachmentDeletionPurgesExclusiveS3BlobExactlyOnce(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion-s3", 2)
	recordRepository := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate})
	record, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, recordsPostgresCompleteRevisionInput(t, "Attachment deletion S3 target"), "attachment-deletion-s3"))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	client, bucket := newAttachmentUploadWorkflowMinIO(t)
	blobStore, err := attachments.NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("exclusive S3 attachment deletion content\n")
	digest := sha256.Sum256(content)
	object, err := blobStore.Put(ctx, attachments.PutRequest{
		ExpectedSHA256:    digest,
		ExpectedSizeBytes: int64(len(content)),
		TemporaryKey:      "temporary/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("S3BlobStore.Put() error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values ($1, $2, $3, $4, 's3')`,
		object.Key, object.SHA256[:], object.VersionID, object.SizeBytes,
	); err != nil {
		t.Fatalf("insert S3 Blob metadata: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, project_id, record_id, attachment_state,
			display_name, media_type, logical_size_bytes,
			blob_key, blob_object_version, created_by
		) values ('att_deletes3exclusive', 'default', $1, 'available',
		  'exclusive-s3.txt', 'text/plain', $2, $3, $4, 'usr_records1')`,
		record.RecordID, object.SizeBytes, object.Key, object.VersionID,
	); err != nil {
		t.Fatalf("insert exclusive S3 attachment: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_revision_attachments (
			record_id, revision_id, ordinal, attachment_id
		) values ($1, $2, 0, 'att_deletes3exclusive')`,
		record.RecordID, record.RevisionID,
	); err != nil {
		t.Fatalf("insert exclusive S3 attachment ref: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_quota_accounts (
			project_id, logical_bytes, reserved_bytes, physical_bytes, quota_version
		) values ('default', $1, 0, $1, 0)`, object.SizeBytes); err != nil {
		t.Fatalf("insert exclusive S3 attachment quota: %v", err)
	}

	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_attdeletes3", ReservationID: "drs_attdeletes3",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 15,
		LedgerSequence: 16, LedgerEntryHash: testStoreRecordPlatformDigest(0xa3)}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, operation)
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "attachment-deletion-s3-worker", 2)
	repository := NewPostgresAttachmentRepository(workerPool)
	deleteCalls := 0
	deletionBlobStore := &attachmentDeletionObservingBlobStore{
		BlobGCDeleter: blobStore,
		beforeDelete: func(version attachments.ObjectVersion) error {
			deleteCalls++
			if version != object {
				return errors.New("attachment purge attempted an inexact S3 Blob delete")
			}
			var backendKind, deletionState string
			if err := fixture.db.QueryRow(ctx, `
				select backend_kind, deletion_state
				from public.blob_gc_deletions
				where project_id = 'default' and purge_mode = 'permanent'
				  and operation_id = $1 and blob_key = $2 and object_version = $3`,
				operation.OperationID, version.Key, version.VersionID,
			).Scan(&backendKind, &deletionState); err != nil {
				return err
			}
			if backendKind != string(attachments.BackendKindS3) || deletionState != "claimed" {
				return errors.New("S3 Blob deletion ran without its durable permanent GC claim")
			}
			return nil
		},
	}
	unconfigured, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unconfigured.PurgeDeletion(ctx, target); err == nil {
		t.Fatal("S3 purge committed with no configured version deleter")
	}
	var unconfiguredAttachments, unconfiguredClaims, unconfiguredReceipts int
	if err := fixture.db.QueryRow(ctx, `select
		(select count(*) from public.record_attachments where attachment_id='att_deletes3exclusive'),
		(select count(*) from public.blob_gc_deletions where operation_id=$1),
		(select count(*) from public.attachment_purge_receipts where operation_id=$1)`, operation.OperationID).
		Scan(&unconfiguredAttachments, &unconfiguredClaims, &unconfiguredReceipts); err != nil {
		t.Fatal(err)
	}
	if unconfiguredAttachments != 1 || unconfiguredClaims != 0 || unconfiguredReceipts != 0 {
		t.Fatalf("missing backend committed attachment/claim/receipt changes: %d/%d/%d", unconfiguredAttachments, unconfiguredClaims, unconfiguredReceipts)
	}
	if err := repository.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindS3, deletionBlobStore); err != nil {
		t.Fatalf("ConfigureAttachmentDeletionBlobStore() error = %v", err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	wrongOwner := target
	wrongOwner.OwnerClaim.Owner.Generation++
	if _, err := adapter.PurgeDeletion(ctx, wrongOwner); err == nil {
		t.Fatal("stale operation owner authorized S3 purge")
	}
	wrongExpiry := target
	wrongExpiry.OwnerClaim.Owner.ExpiresAt = wrongExpiry.OwnerClaim.Owner.ExpiresAt.Add(time.Microsecond)
	if _, err := adapter.PurgeDeletion(ctx, wrongExpiry); err == nil {
		t.Fatal("different observed expiry authorized S3 purge")
	}
	wrongLedger := target
	wrongLedger.Operation.LedgerEntryHash[0] ^= 1
	if _, err := adapter.PurgeDeletion(ctx, wrongLedger); err == nil {
		t.Fatal("uninstalled ledger entry authorized S3 purge")
	}
	identityTx, err := workerPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := legacyAdapterPurgeEnvelope(ctx, identityTx, target, recorddeletion.RecordAttachmentsSurfaceDigest())
	if err != nil {
		_ = identityTx.Rollback(ctx)
		t.Fatalf("read installed witness binding: %v", err)
	}
	if err := identityTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ledger_domain", "witness_proof_digest", "surface_digest"} {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(authority, &envelope); err != nil {
			t.Fatal(err)
		}
		var value string
		if err := json.Unmarshal(envelope[field], &value); err != nil || len(value) != 64 {
			t.Fatalf("invalid installed %s: %q %v", field, value, err)
		}
		if value[0] == '0' {
			value = "1" + value[1:]
		} else {
			value = "0" + value[1:]
		}
		envelope[field], err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := workerPool.Exec(ctx, `select public.record_attachment_purge_prepare_v1($1)`, body); err == nil {
			t.Fatalf("tampered %s authorized S3 purge", field)
		}
	}
	runtimeRepository := NewPostgresAttachmentRepository(runtimePool)
	if err := runtimeRepository.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindS3, deletionBlobStore); err != nil {
		t.Fatal(err)
	}
	runtimeAdapter, err := attachments.NewDeletionAdapter(runtimeRepository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeAdapter.PurgeDeletion(ctx, target); err == nil {
		t.Fatal("runtime role authorized S3 purge")
	}
	for _, denial := range []struct {
		name string
		run  func() error
	}{
		{"worker claim delete", func() error {
			_, err := workerPool.Exec(ctx, `delete from public.blob_gc_deletions where operation_id=$1`, operation.OperationID)
			return err
		}},
		{"runtime purge receipt delete", func() error {
			_, err := runtimePool.Exec(ctx, `delete from public.attachment_purge_receipts where operation_id=$1`, operation.OperationID)
			return err
		}},
	} {
		var sqlError interface{ SQLState() string }
		if err := denial.run(); !errors.As(err, &sqlError) || sqlError.SQLState() != "42501" {
			t.Fatalf("%s = %v, want SQLSTATE 42501", denial.name, err)
		}
	}
	var protectedAttachments, protectedClaims int
	if err := fixture.db.QueryRow(ctx, `select
		(select count(*) from public.record_attachments where attachment_id='att_deletes3exclusive'),
		(select count(*) from public.blob_gc_deletions where operation_id=$1)`, operation.OperationID).
		Scan(&protectedAttachments, &protectedClaims); err != nil {
		t.Fatal(err)
	}
	if protectedAttachments != 1 || protectedClaims != 0 || deleteCalls != 0 {
		t.Fatalf("refused authority changed attachment/claims/delete calls: %d/%d/%d", protectedAttachments, protectedClaims, deleteCalls)
	}
	receipt, err := adapter.PurgeDeletion(ctx, target)
	if err != nil {
		t.Fatalf("PurgeDeletion() error = %v", err)
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		t.Fatalf("VerifyDeletion() error = %v", err)
	}
	if _, err := blobStore.Stat(ctx, object); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("S3BlobStore.Stat(exact deleted version) error = %v, want ErrBlobNotFound", err)
	}

	var gcCount int64
	var backendKind, deletionState, physicalResult string
	var storedDigest, gcReceiptDigest []byte
	var storedSize, logicalAfterPurge, physicalAfterPurge, quotaVersionAfterPurge int64
	var retryAtAbsent bool
	var completedAt *time.Time
	if err := fixture.db.QueryRow(ctx, `
		select count(*) over (), backend_kind, deletion_state, physical_delete_result,
		       sha256_digest, size_bytes, receipt_digest, retry_at is null, completed_at
		from public.blob_gc_deletions
		where project_id = 'default' and purge_mode = 'permanent'
		  and operation_id = $1 and blob_key = $2 and object_version = $3`,
		operation.OperationID, object.Key, object.VersionID,
	).Scan(
		&gcCount, &backendKind, &deletionState, &physicalResult,
		&storedDigest, &storedSize, &gcReceiptDigest, &retryAtAbsent, &completedAt,
	); err != nil {
		t.Fatalf("read completed S3 attachment GC claim: %v", err)
	}
	if gcCount != 1 || backendKind != string(attachments.BackendKindS3) ||
		deletionState != "completed" || physicalResult != "deleted" ||
		!bytes.Equal(storedDigest, object.SHA256[:]) || storedSize != object.SizeBytes ||
		len(gcReceiptDigest) != sha256.Size || !retryAtAbsent || completedAt == nil || completedAt.IsZero() {
		t.Fatalf("completed S3 GC claim = count %d backend/state/result %q/%q/%q digest %x size %d receipt %x retry_absent %t completed %#v",
			gcCount, backendKind, deletionState, physicalResult, storedDigest, storedSize,
			gcReceiptDigest, retryAtAbsent, completedAt)
	}
	if err := fixture.db.QueryRow(ctx, `
		select logical_bytes, physical_bytes, quota_version
		from public.attachment_quota_accounts where project_id = 'default'`,
	).Scan(&logicalAfterPurge, &physicalAfterPurge, &quotaVersionAfterPurge); err != nil {
		t.Fatalf("read S3 attachment quota after purge: %v", err)
	}
	if logicalAfterPurge != 0 || physicalAfterPurge != 0 || quotaVersionAfterPurge != 2 {
		t.Fatalf("S3 attachment quota after purge = %d/%d@%d, want 0/0@2",
			logicalAfterPurge, physicalAfterPurge, quotaVersionAfterPurge)
	}

	replay, err := adapter.PurgeDeletion(ctx, target)
	if err != nil || replay != receipt {
		t.Fatalf("PurgeDeletion(replay) = %#v, %v, want %#v", replay, err, receipt)
	}
	var logicalAfterReplay, physicalAfterReplay, quotaVersionAfterReplay int64
	if err := fixture.db.QueryRow(ctx, `
		select logical_bytes, physical_bytes, quota_version
		from public.attachment_quota_accounts where project_id = 'default'`,
	).Scan(&logicalAfterReplay, &physicalAfterReplay, &quotaVersionAfterReplay); err != nil {
		t.Fatalf("read S3 attachment quota after replay: %v", err)
	}
	if logicalAfterReplay != logicalAfterPurge || physicalAfterReplay != physicalAfterPurge ||
		quotaVersionAfterReplay != quotaVersionAfterPurge {
		t.Fatalf("S3 attachment quota after replay = %d/%d@%d, want unchanged %d/%d@%d",
			logicalAfterReplay, physicalAfterReplay, quotaVersionAfterReplay,
			logicalAfterPurge, physicalAfterPurge, quotaVersionAfterPurge)
	}
	if deleteCalls != 1 {
		t.Fatalf("exact external S3 Blob delete calls = %d, want 1", deleteCalls)
	}
}

func TestPostgresIntegrationAttachmentDeletionRejectsOperationDriftAndRetainsActivePin(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion-pin", 2)
	recordRepository := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate})
	record, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, recordsPostgresCompleteRevisionInput(t, "Attachment deletion pin"), "attachment-deletion-pin"))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	blobStore, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "blob-root"))
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	content := []byte("pinned attachment deletion content\n")
	object := seedPostgresManagedLocalBlob(t, ctx, fixture, blobStore, content).Version
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind, namespace_id
		) values ($1, $2, $3, $4, 'local', $5)`,
		object.Key, object.SHA256[:], object.VersionID, object.SizeBytes, blobStore.Namespace().NamespaceID); err != nil {
		t.Fatalf("insert Blob metadata: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, project_id, record_id, attachment_state,
			display_name, media_type, logical_size_bytes,
			blob_key, blob_object_version, created_by
		) values ('att_deletepinned', 'default', $1, 'available',
		  'pinned.txt', 'text/plain', $2, $3, $4, 'usr_records1')`,
		record.RecordID, object.SizeBytes, object.Key, object.VersionID); err != nil {
		t.Fatalf("insert pinned attachment: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_revision_attachments (
			record_id, revision_id, ordinal, attachment_id
		) values ($1, $2, 0, 'att_deletepinned')`, record.RecordID, record.RevisionID); err != nil {
		t.Fatalf("insert pinned attachment ref: %v", err)
	}
	if result, err := fixture.db.Exec(ctx, `
		update public.attachment_quota_accounts
		set logical_bytes = $1, physical_bytes = $1
		where project_id = 'default' and logical_bytes = 0
		  and reserved_bytes = 0 and physical_bytes = 0`, object.SizeBytes); err != nil {
		t.Fatalf("initialize pinned attachment quota: %v", err)
	} else if result.RowsAffected() != 1 {
		t.Fatal("expected one empty attachment quota account")
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_gc_pins (
			pin_id, pin_owner_kind, pin_owner_id, blob_key,
			blob_object_version, expires_at
		) values ('bgp_deletepinned', 'backup_manifest', 'manifest_deletepinned',
		  $1, $2, transaction_timestamp() + interval '1 hour')`, object.Key, object.VersionID); err != nil {
		t.Fatalf("insert active Blob pin: %v", err)
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: "rpo_attdeletepin", ReservationID: "drs_attdeletepin",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 9,
	}
	claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, operation, record.RevisionID)
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "local-pin-purge", 2)
	coordinator, err := attachments.NewLocalCleanupCoordinator(blobStore, NewPostgresLocalBlobRepository(workerPool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresAttachmentRepository(workerPool)
	if err := repository.ConfigureAttachmentDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	target := recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}
	drifted := target
	drifted.OwnerClaim.Owner.Generation++
	if _, err := adapter.PurgeDeletion(ctx, drifted); err == nil {
		t.Fatal("drifted operation removed owner references")
	}
	var preserved int
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_attachments where attachment_id='att_deletepinned'`).Scan(&preserved); err != nil || preserved != 1 {
		t.Fatalf("invalid owner changed attachment: %d %v", preserved, err)
	}
	receipt, err := adapter.PurgeDeletion(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		t.Fatal(err)
	}
	var attachmentCount, refCount, blobCount, pinCount, gcCount, receiptCount int
	var logicalBytes, physicalBytes int64
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_attachments where attachment_id = 'att_deletepinned'),
		       (select count(*)::int from public.record_revision_attachments where attachment_id = 'att_deletepinned'),
		       (select count(*)::int from public.blob_objects where blob_key = $1 and object_version = $2),
		       (select count(*)::int from public.blob_gc_pins where pin_id = 'bgp_deletepinned'),
		       (select count(*)::int from public.blob_gc_deletions where operation_id = $3 and physical_delete_result='retained_referenced'),
		       (select count(*)::int from public.attachment_purge_receipts where operation_id = $3 and surface_kind='blob_object' and removed_row_count=0),
		       (select logical_bytes from public.attachment_quota_accounts where project_id = 'default'),
		       (select physical_bytes from public.attachment_quota_accounts where project_id = 'default')`,
		object.Key, object.VersionID, operation.OperationID,
	).Scan(&attachmentCount, &refCount, &blobCount, &pinCount, &gcCount, &receiptCount, &logicalBytes, &physicalBytes); err != nil {
		t.Fatalf("read pinned attachment rollback result: %v", err)
	}
	if attachmentCount != 0 || refCount != 0 || blobCount != 1 || pinCount != 1 || gcCount != 1 || receiptCount != 1 ||
		logicalBytes != 0 || physicalBytes != object.SizeBytes {
		t.Fatalf("pinned retention attachment/ref/blob/pin/gc/receipt logical/physical = %d/%d/%d/%d/%d/%d %d/%d",
			attachmentCount, refCount, blobCount, pinCount, gcCount, receiptCount, logicalBytes, physicalBytes)
	}
	if _, err := blobStore.Stat(ctx, object); err != nil {
		t.Fatalf("pinned Blob Stat() error = %v", err)
	}
}

func TestPostgresIntegrationAttachmentDeletionFailsClosedForLegacyUploadPartial(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion-partial", 2)
	recordRepository := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate})
	record, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, recordsPostgresCompleteRevisionInput(t, "Attachment deletion active partial"), "attachment-deletion-partial"))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	blobStore, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "blob-root"))
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	content := []byte("active partial attachment deletion content\n")
	object := seedPostgresManagedLocalBlob(t, ctx, fixture, blobStore, content).Version
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind, namespace_id
		) values ($1, $2, $3, $4, 'local', $5)`,
		object.Key, object.SHA256[:], object.VersionID, object.SizeBytes, blobStore.Namespace().NamespaceID); err != nil {
		t.Fatalf("insert Blob metadata: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, project_id, record_id, origin_draft_id, attachment_state,
			display_name, media_type, logical_size_bytes,
			blob_key, blob_object_version, created_by
		) values ('att_deletepartial', 'default', $1, 'rdf_deletepartial', 'available',
		  'partial.txt', 'text/plain', $2, $3, $4, 'usr_records1')`,
		record.RecordID, object.SizeBytes, object.Key, object.VersionID); err != nil {
		t.Fatalf("insert active-partial attachment: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_uploads (
			upload_id, project_id, attachment_id, origin_draft_id, author_id,
			upload_state, transport_kind, declared_size_bytes, reserved_size_bytes,
			expires_at
		) values ('aup_deletepartial', 'default', 'att_deletepartial', 'rdf_deletepartial',
		  'usr_records1', 'uploading', 'local', $1, $1,
		  transaction_timestamp() + interval '1 hour')`, object.SizeBytes); err != nil {
		t.Fatalf("insert active upload partial: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.attachment_upload_parts(upload_id,part_number,size_bytes,sha256_digest,object_version)
		values('aup_deletepartial',1,$1,$2,$3)`, object.SizeBytes, object.SHA256[:], object.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_revision_attachments (
			record_id, revision_id, ordinal, attachment_id
		) values ($1, $2, 0, 'att_deletepartial')`, record.RecordID, record.RevisionID); err != nil {
		t.Fatalf("insert active-partial attachment ref: %v", err)
	}
	if result, err := fixture.db.Exec(ctx, `
		update public.attachment_quota_accounts
		set logical_bytes = $1, reserved_bytes = $1, physical_bytes = $1
		where project_id = 'default' and logical_bytes = 0
		  and reserved_bytes = 0 and physical_bytes = 0`, object.SizeBytes); err != nil {
		t.Fatalf("initialize active-partial attachment quota: %v", err)
	} else if result.RowsAffected() != 1 {
		t.Fatal("expected one empty attachment quota account")
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: "rpo_attdeletepartial", ReservationID: "drs_attdeletepartial",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 10,
	}
	claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, operation, record.RevisionID)
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "local-legacy-purge", 2)
	coordinator, err := attachments.NewLocalCleanupCoordinator(blobStore, NewPostgresLocalBlobRepository(workerPool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresAttachmentRepository(workerPool)
	if err := repository.ConfigureAttachmentDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	_, err = adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim})
	var databaseError interface{ SQLState() string }
	if !errors.As(err, &databaseError) || databaseError.SQLState() != "55000" {
		t.Fatalf("legacy partial was not rejected by safety precondition: %v", err)
	}
	var attachmentCount, uploadCount, refCount, blobCount, gcCount, receiptCount int
	var logicalBytes, reservedBytes, physicalBytes int64
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_attachments where attachment_id = 'att_deletepartial'),
		       (select count(*)::int from public.attachment_uploads where upload_id = 'aup_deletepartial'),
		       (select count(*)::int from public.record_revision_attachments where attachment_id = 'att_deletepartial'),
		       (select count(*)::int from public.blob_objects where blob_key = $1 and object_version = $2),
		       (select count(*)::int from public.blob_gc_deletions where owner_id = $3),
		       (select count(*)::int from public.attachment_purge_receipts where operation_id = $3),
		       (select logical_bytes from public.attachment_quota_accounts where project_id = 'default'),
		       (select reserved_bytes from public.attachment_quota_accounts where project_id = 'default'),
		       (select physical_bytes from public.attachment_quota_accounts where project_id = 'default')`,
		object.Key, object.VersionID, operation.OperationID,
	).Scan(&attachmentCount, &uploadCount, &refCount, &blobCount, &gcCount, &receiptCount,
		&logicalBytes, &reservedBytes, &physicalBytes); err != nil {
		t.Fatalf("read active-partial rollback result: %v", err)
	}
	if attachmentCount != 1 || uploadCount != 1 || refCount != 1 || blobCount != 1 || gcCount != 0 || receiptCount != 0 ||
		logicalBytes != object.SizeBytes || reservedBytes != object.SizeBytes || physicalBytes != object.SizeBytes {
		t.Fatalf("active-partial rollback attachment/upload/ref/blob/gc/receipt logical/reserved/physical = %d/%d/%d/%d/%d/%d %d/%d/%d",
			attachmentCount, uploadCount, refCount, blobCount, gcCount, receiptCount,
			logicalBytes, reservedBytes, physicalBytes)
	}
	if _, err := blobStore.Stat(ctx, object); err != nil {
		t.Fatalf("active-partial Blob Stat() error = %v", err)
	}
}

func TestPostgresIntegrationAttachmentDeletionLocksQuotaBeforeBlobTables(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion-lock-order", 2)
	recordRepository := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate})
	record, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, recordsPostgresCompleteRevisionInput(t, "Attachment deletion lock order"), "attachment-deletion-order"))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	blobStore, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "blob-root"))
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	content := []byte("attachment deletion lock order content\n")
	object := seedPostgresManagedLocalBlob(t, ctx, fixture, blobStore, content).Version
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind, namespace_id
		) values ($1, $2, $3, $4, 'local', $5)`,
		object.Key, object.SHA256[:], object.VersionID, object.SizeBytes, blobStore.Namespace().NamespaceID); err != nil {
		t.Fatalf("insert Blob metadata: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, project_id, record_id, attachment_state,
			display_name, media_type, logical_size_bytes,
			blob_key, blob_object_version, created_by
		) values ('att_deleteorder', 'default', $1, 'available',
		  'order.txt', 'text/plain', $2, $3, $4, 'usr_records1')`,
		record.RecordID, object.SizeBytes, object.Key, object.VersionID); err != nil {
		t.Fatalf("insert attachment: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_revision_attachments (record_id, revision_id, ordinal, attachment_id)
		values ($1, $2, 0, 'att_deleteorder')`, record.RecordID, record.RevisionID); err != nil {
		t.Fatalf("insert attachment ref: %v", err)
	}
	if result, err := fixture.db.Exec(ctx, `
		update public.attachment_quota_accounts
		set logical_bytes = $1, physical_bytes = $1
		where project_id = 'default' and logical_bytes = 0
		  and reserved_bytes = 0 and physical_bytes = 0`, object.SizeBytes); err != nil {
		t.Fatalf("initialize attachment quota: %v", err)
	} else if result.RowsAffected() != 1 {
		t.Fatal("expected one empty attachment quota account")
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: "rpo_attdeleteorder", ReservationID: "drs_attdeleteorder",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 10,
	}
	claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, operation, record.RevisionID)
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "local-order-purge", 2)
	coordinator, err := attachments.NewLocalCleanupCoordinator(blobStore, NewPostgresLocalBlobRepository(workerPool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresAttachmentRepository(workerPool)
	if err := repository.ConfigureAttachmentDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}

	// 模拟草稿附件释放：先持有配额行锁，之后才会去取 blob_objects 表锁。
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin quota holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `
		select 1 from public.attachment_quota_accounts where project_id = 'default' for update`); err != nil {
		t.Fatalf("hold quota row: %v", err)
	}
	outcome := make(chan error, 1)
	go func() {
		_, purgeErr := adapter.PurgeDeletion(context.Background(), recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim})
		outcome <- purgeErr
	}()
	waitForRecordsPostgresLockWaiter(t, ctx, fixture.db, "%local_attachment_purge_prepare%")
	// 永久删除此刻在等配额行，还没拿 Blob 表锁：释放一侧能立即取得 blob_objects 锁，不会成环。
	if _, err := holder.Exec(ctx, `lock table public.blob_objects in row exclusive mode nowait`); err != nil {
		t.Fatalf("Blob table locked before quota row: %v", err)
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("release quota holder: %v", err)
	}
	if err := <-outcome; err != nil {
		t.Fatalf("PurgeDeletion() after quota holder released error = %v", err)
	}
	var remaining int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int from public.record_attachments where attachment_id = 'att_deleteorder'`).Scan(&remaining); err != nil {
		t.Fatalf("count purged attachment: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("purged attachment rows = %d, want 0", remaining)
	}
}

func TestPostgresIntegrationAttachmentDeletionFailsClosedForActivePublicationIntent(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion-publication", 2)
	recordRepository := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate})
	record, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, recordsPostgresCompleteRevisionInput(t, "Attachment deletion publication"), "attachment-deletion-publication"))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	const logicalSize = int64(23)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, project_id, record_id, origin_draft_id, attachment_state,
			display_name, media_type, logical_size_bytes, created_by
		) values ('att_deletepublication', 'default', $1, 'rdf_deletepublication', 'expired',
		  'publication.txt', 'text/plain', $2, 'usr_records1')`,
		record.RecordID, logicalSize,
	); err != nil {
		t.Fatalf("insert expired publication attachment: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_uploads (
			upload_id, project_id, attachment_id, origin_draft_id, author_id,
			upload_state, transport_kind, declared_size_bytes, reserved_size_bytes,
			expires_at
		) values ('aup_deletepublication', 'default', 'att_deletepublication',
		  'rdf_deletepublication', 'usr_records1', 'expired', 'local', $1, $1,
		  transaction_timestamp() + interval '1 hour')`, logicalSize,
	); err != nil {
		t.Fatalf("insert expired publication upload: %v", err)
	}
	digest := sha256.Sum256([]byte("active publication attachment deletion content"))
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_publication_intents (
			publication_id, project_id, owner_kind, owner_id, owner_generation,
			blob_key, sha256_digest, size_bytes, backend_kind,
			publication_state, publish_expires_at
		) values ('bpi_deletepublication', 'default', 'upload', 'aup_deletepublication', 1,
		  $1, $2, $3, 's3', 'prepared', transaction_timestamp() + interval '1 hour')`,
		"sha256/"+fmt.Sprintf("%x", digest), digest[:], logicalSize,
	); err != nil {
		t.Fatalf("insert active publication intent: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_quota_accounts (
			project_id, logical_bytes, reserved_bytes, physical_bytes
		) values ('default', 0, 0, 0)`,
	); err != nil {
		t.Fatalf("insert publication attachment quota: %v", err)
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_attdeletepublication", ReservationID: "drs_attdeletepublication",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 11,
		LedgerSequence: 15, LedgerEntryHash: testStoreRecordPlatformDigest(0xa5)}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, operation)
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "attachment-publication-s3-worker", 2)
	repository := NewPostgresAttachmentRepository(workerPool)
	client, bucket := newAttachmentUploadWorkflowMinIO(t)
	blobStore, err := attachments.NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindS3, blobStore); err != nil {
		t.Fatal(err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	_, err = adapter.PurgeDeletion(ctx, target)
	var databaseError interface{ SQLState() string }
	if !errors.As(err, &databaseError) || databaseError.SQLState() != "55000" {
		t.Fatalf("PurgeDeletion(active publication) error = %v, want SQLSTATE 55000", err)
	}

	var attachmentCount, uploadCount, publicationCount, receiptCount, claimCount int
	var logical, reserved, physical, quotaVersion int64
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_attachments where attachment_id = 'att_deletepublication'),
		       (select count(*)::int from public.attachment_uploads where upload_id = 'aup_deletepublication'),
		       (select count(*)::int from public.blob_publication_intents where publication_id = 'bpi_deletepublication'),
		       (select count(*)::int from public.attachment_purge_receipts where operation_id = $1),
		       (select count(*)::int from public.blob_gc_deletions where operation_id = $1),
		       logical_bytes, reserved_bytes, physical_bytes, quota_version
		from public.attachment_quota_accounts where project_id='default'`, operation.OperationID).
		Scan(&attachmentCount, &uploadCount, &publicationCount, &receiptCount, &claimCount,
			&logical, &reserved, &physical, &quotaVersion); err != nil {
		t.Fatalf("read active-publication rollback result: %v", err)
	}
	if attachmentCount != 1 || uploadCount != 1 || publicationCount != 1 || receiptCount != 0 || claimCount != 0 ||
		logical != 0 || reserved != 0 || physical != 0 || quotaVersion != 0 {
		t.Fatalf("active-publication rollback attachment/upload/publication/receipt/claim = %d/%d/%d/%d/%d, quota = %d/%d/%d@%d",
			attachmentCount, uploadCount, publicationCount, receiptCount, claimCount, logical, reserved, physical, quotaVersion)
	}
}

func TestPostgresMinIOIntegrationAttachmentDeletionPurgesExclusiveExactVersion(t *testing.T) {
	runAttachmentDeletionExactVersionWorkflow(t, false, "normal")
}

func TestPostgresMinIOIntegrationAttachmentDeletionPurgesHistoricalExactVersion(t *testing.T) {
	runAttachmentDeletionExactVersionWorkflow(t, true, "normal")
}

func TestPostgresMinIOIntegrationAttachmentDeletionCompletionUnknown(t *testing.T) {
	runAttachmentDeletionExactVersionWorkflow(t, false, "completion_unknown")
}

func TestPostgresMinIOIntegrationAttachmentDeletionOwnerTakeover(t *testing.T) {
	runAttachmentDeletionExactVersionWorkflow(t, true, "takeover")
}

func runAttachmentDeletionExactVersionWorkflow(t *testing.T, historical bool, scenario string) {
	t.Helper()
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion-minio", 2)
	recordRepository := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate})
	record, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, recordsPostgresCompleteRevisionInput(t, "Attachment deletion MinIO"), "attachment-deletion-minio"))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	client, bucket := newAttachmentUploadWorkflowMinIO(t)
	blobStore, err := attachments.NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("exclusive MinIO attachment deletion content\n")
	digest := sha256.Sum256(content)
	temporaryDigest := sha256.Sum256([]byte(t.Name()))
	object, err := blobStore.Put(ctx, attachments.PutRequest{
		ExpectedSHA256:    digest,
		ExpectedSizeBytes: int64(len(content)),
		TemporaryKey:      fmt.Sprintf("temporary/%x", temporaryDigest),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("S3BlobStore.Put() error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values ($1, $2, $3, $4, 's3')`,
		object.Key, object.SHA256[:], object.VersionID, object.SizeBytes); err != nil {
		t.Fatalf("insert MinIO Blob metadata: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, project_id, record_id, attachment_state,
			display_name, media_type, logical_size_bytes,
			blob_key, blob_object_version, created_by
		) values ('att_deleteminio', 'default', $1, 'available',
		  'minio.txt', 'text/plain', $2, $3, $4, 'usr_records1')`,
		record.RecordID, object.SizeBytes, object.Key, object.VersionID); err != nil {
		t.Fatalf("insert MinIO logical attachment: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_revision_attachments (
			record_id, revision_id, ordinal, attachment_id
		) values ($1, $2, 0, 'att_deleteminio')`, record.RecordID, record.RevisionID); err != nil {
		t.Fatalf("insert MinIO attachment ref: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_quota_accounts (
			project_id, logical_bytes, reserved_bytes, physical_bytes
		) values ('default', $1, 0, $1)`, object.SizeBytes); err != nil {
		t.Fatalf("insert MinIO attachment quota: %v", err)
	}

	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_attdeleteminio", ReservationID: "drs_attdeleteminio",
		Object: recordplatform.ObjectRef{
			ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID,
		},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12,
		LedgerSequence: 16, LedgerEntryHash: testStoreRecordPlatformDigest(0xa4)}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, operation)
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "attachment-deletion-minio-worker", 2)
	repository := NewPostgresAttachmentRepository(workerPool)
	var completionUnknownInjected bool
	if scenario == "completion_unknown" {
		repository.beginTx = func(ctx context.Context, options pgx.TxOptions) (attachmentTx, error) {
			tx, err := workerPool.BeginTx(ctx, options)
			if err != nil {
				return nil, err
			}
			return &attachmentV1UnknownCompletionTx{Tx: tx, injected: &completionUnknownInjected}, nil
		}
	}
	var successor recorddeletion.PurgeTarget
	var oldCleanupGeneration, oldCleanupAttempt int64
	deleteCalls := 0
	var v2 string
	deletionBlobStore := &attachmentDeletionObservingBlobStore{
		BlobGCDeleter: blobStore,
		beforeDelete: func(version attachments.ObjectVersion) error {
			deleteCalls++
			if version != object || (deleteCalls != 1 && (scenario != "takeover" || deleteCalls != 2)) {
				return errors.New("attachment purge attempted a repeated or inexact version delete")
			}
			var committed int
			if err := fixture.db.QueryRow(ctx, `
				select count(*)::int from public.blob_gc_deletions
				where project_id = 'default' and purge_mode = 'permanent'
				  and operation_id = $1 and blob_key = $2 and object_version = $3
				  and sha256_digest = $4 and size_bytes = $5
				  and backend_kind = 's3' and deletion_state = 'claimed'`,
				operation.OperationID, object.Key, object.VersionID, object.SHA256[:], object.SizeBytes,
			).Scan(&committed); err != nil {
				return err
			}
			if committed != 1 {
				return errors.New("physical delete ran without its committed exact GC claim")
			}
			if historical && deleteCalls == 1 {
				upload, err := client.PutObject(ctx, bucket, object.Key, bytes.NewReader(content),
					int64(len(content)), minio.PutObjectOptions{DisableMultipart: true})
				if err != nil {
					return err
				}
				if upload.VersionID == "" || upload.VersionID == object.VersionID {
					return errors.New("raw overwrite did not produce a distinct V2")
				}
				v2 = upload.VersionID
			}
			if scenario == "takeover" && deleteCalls == 1 {
				if err := fixture.db.QueryRow(ctx, `update public.blob_gc_deletions
					set lease_expires_at=clock_timestamp()-interval '1 second'
					where operation_id=$1 and blob_key=$2 and object_version=$3 and deletion_state='claimed'
					returning owner_generation,attempt`, operation.OperationID, object.Key, object.VersionID).
					Scan(&oldCleanupGeneration, &oldCleanupAttempt); err != nil {
					return fmt.Errorf("expire exact committed cleanup claim: %w", err)
				}
				if _, err := fixture.db.Exec(ctx, `update public.record_purge_operations
					set owner_expires_at=clock_timestamp()-interval '1 second'
					where operation_id=$1 and owner_id=$2 and owner_generation=$3 and owner_expires_at=$4`,
					operation.OperationID, target.OwnerClaim.Owner.OwnerID, target.OwnerClaim.Owner.Generation,
					target.OwnerClaim.Owner.ExpiresAt); err != nil {
					return fmt.Errorf("expire exact operation owner: %w", err)
				}
				work, err := NewPostgresRecordDeletionRepository(workerPool, allowRecordPlatformAdmissionGate).ClaimDeletionWork(ctx,
					recorddeletion.DeletionWorkClaimInput{OwnerID: "attachment_successor", OwnerLeaseDuration: 2 * time.Minute})
				if err != nil || work == nil {
					return fmt.Errorf("claim successor operation: %v", err)
				}
				successor = recorddeletion.PurgeTarget{Operation: work.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{
					OperationID: work.Operation.OperationID, Owner: work.Owner,
				}}
				if successor.OwnerClaim.Owner.Generation != target.OwnerClaim.Owner.Generation+1 {
					return errors.New("operation takeover did not advance owner generation")
				}
			}
			return nil
		},
	}
	if err := repository.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindS3, deletionBlobStore); err != nil {
		t.Fatalf("ConfigureAttachmentDeletionBlobStore(S3) error = %v", err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	receipt, err := adapter.PurgeDeletion(ctx, target)
	if scenario == "takeover" {
		if err == nil {
			t.Fatal("expired operation owner completed an already-dispatched S3 deletion")
		}
		assertAttachmentHistoricalGCObjects(t, ctx, client, bucket, object, v2, content)
		var state string
		var physical int64
		if err := fixture.db.QueryRow(ctx, `select g.deletion_state,q.physical_bytes from public.blob_gc_deletions g
			join public.attachment_quota_accounts q on q.project_id=g.project_id where g.operation_id=$1`, operation.OperationID).
			Scan(&state, &physical); err != nil || state != "claimed" || physical != object.SizeBytes {
			t.Fatalf("stale completion settled claim or quota: state=%q physical=%d error=%v", state, physical, err)
		}
		target = successor
		receipt, err = adapter.PurgeDeletion(ctx, target)
	}
	if err != nil {
		t.Fatalf("PurgeDeletion(MinIO %s) error = %v", scenario, err)
	}
	if scenario == "completion_unknown" && !completionUnknownInjected {
		t.Fatal("completion COMMIT loss was not injected after the real database commit")
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		t.Fatalf("VerifyDeletion(MinIO) error = %v", err)
	}
	if replay, err := adapter.PurgeDeletion(ctx, target); err != nil || replay != receipt {
		t.Fatalf("PurgeDeletion(MinIO replay) = %#v, %v, want %#v", replay, err, receipt)
	}
	wantCalls := 1
	if scenario == "takeover" {
		wantCalls = 2
		var generation, attempt int64
		if err := fixture.db.QueryRow(ctx, `select owner_generation,attempt from public.blob_gc_deletions
			where operation_id=$1 and blob_key=$2 and object_version=$3`, operation.OperationID, object.Key, object.VersionID).
			Scan(&generation, &attempt); err != nil || generation != oldCleanupGeneration+1 || attempt != oldCleanupAttempt+1 {
			t.Fatalf("cleanup takeover generation/attempt = %d/%d from %d/%d: %v", generation, attempt, oldCleanupGeneration, oldCleanupAttempt, err)
		}
	}
	if deleteCalls != wantCalls {
		t.Fatalf("physical calls after purge/verify/replay = %d, want %d", deleteCalls, wantCalls)
	}
	if historical {
		assertAttachmentHistoricalGCObjects(t, ctx, client, bucket, object, v2, content)
	} else if _, err := blobStore.Stat(ctx, object); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("S3BlobStore.Stat(after purge) error = %v, want ErrBlobNotFound", err)
	}

	var logicalBytes, physicalBytes, quotaVersion int64
	var attachmentCount, refCount, completedGC, receiptCount int
	var physicalResult string
	if err := fixture.db.QueryRow(ctx, `
		select (select logical_bytes from public.attachment_quota_accounts where project_id = 'default'),
		       (select physical_bytes from public.attachment_quota_accounts where project_id = 'default'),
		       (select quota_version from public.attachment_quota_accounts where project_id = 'default'),
		       (select count(*)::int from public.record_attachments where attachment_id = 'att_deleteminio'),
		       (select count(*)::int from public.record_revision_attachments where attachment_id = 'att_deleteminio'),
		       (select count(*)::int from public.blob_gc_deletions
		         where operation_id = $1 and backend_kind = 's3' and deletion_state = 'completed'),
		       (select physical_delete_result from public.blob_gc_deletions
		         where operation_id = $1 and backend_kind = 's3' and deletion_state = 'completed'),
		       (select count(*)::int from public.attachment_purge_receipts where operation_id = $1)`,
		operation.OperationID,
	).Scan(&logicalBytes, &physicalBytes, &quotaVersion, &attachmentCount, &refCount, &completedGC, &physicalResult, &receiptCount); err != nil {
		t.Fatalf("read MinIO attachment deletion result: %v", err)
	}
	wantPhysicalResult := "deleted"
	if scenario == "takeover" {
		wantPhysicalResult = "already_absent"
	}
	if logicalBytes != 0 || physicalBytes != 0 || quotaVersion != 2 || attachmentCount != 0 || refCount != 0 ||
		completedGC != 1 || physicalResult != wantPhysicalResult || receiptCount != 3 {
		t.Fatalf("MinIO deletion quota logical/physical/version=%d/%d/%d attachment/ref=%d/%d gc/result/receipt=%d/%q/%d",
			logicalBytes, physicalBytes, quotaVersion, attachmentCount, refCount, completedGC, physicalResult, receiptCount)
	}
}

// The transaction really commits; only its acknowledgment is lost. The
// coordinator must resolve the original completion without a second deletion.
type attachmentV1UnknownCompletionTx struct {
	pgx.Tx
	injected   *bool
	completion bool
}

func (tx *attachmentV1UnknownCompletionTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if sql == `select public.record_attachment_gc_complete_v1($1)` && !*tx.injected {
		tx.completion = true
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func (tx *attachmentV1UnknownCompletionTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	if tx.completion {
		*tx.injected = true
		return errors.New("injected lost S3 completion COMMIT acknowledgment")
	}
	return nil
}

// seedVerifiedS3AttachmentDeletionOperation installs a real independently
// verified and witnessed V1 entry before creating the matching APP projection.
// The caller receives the exact owner tuple returned by the operation INSERT.
func seedVerifiedS3AttachmentDeletionOperation(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture,
	record records.RevisionCommitResult, operation recorddeletion.DeletionOperation) recorddeletion.PurgeTarget {
	t.Helper()
	ledger := newRecordsPostgresRecoveryLedger(t, ctx, ImportAttachmentGCFixture{platform: fixture})
	deployment := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	request := recordsPostgresRecoveryRequest(t, record, operation.OperationID, deployment, recorddeletion.LedgerEntryDeleteCommit, 0)
	entry, proof := ledger.record(t, ctx, request)
	fingerprint, err := request.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatal(err)
	}
	operation.LedgerSequence, operation.LedgerEntryHash = entry.Sequence, entry.EntryHash
	digest := testStoreRecordPlatformDigest(0x93)
	if _, err := fixture.db.Exec(ctx, `insert into public.deletion_reservations (
		reservation_id, project_id, object_kind, object_id, deletion_token_commitment, request_fingerprint,
		state, fence_epoch, expires_at, completed_at, actor_scope_digest, preview_binding_digest,
		preview_current_revision_id, preview_lock_version, preview_authorization_epoch, preview_content_delivery_epoch,
		preview_dependency_graph_digest, preview_backup_inventory_digest, preview_processor_inventory_digest,
		adapter_readiness_digest, adapter_preview_digest, preview_witness_sequence, preview_witness_entry_hash
	) values ($1,'default','record',$2,$3,$4,'committed',$5,clock_timestamp()+interval '1 hour',clock_timestamp(),
		$6,$6,$7,1,1,0,$6,$6,$6,$6,$6,1,$6)`, operation.ReservationID, record.RecordID,
		request.TokenCommitment[:], fingerprint[:], int64(operation.FenceEpoch), digest[:], record.RevisionID); err != nil {
		t.Fatalf("insert verified attachment deletion reservation: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=$2
		where project_id='default' and object_kind='record' and object_id=$1`, record.RecordID, int64(operation.FenceEpoch)); err != nil {
		t.Fatalf("set verified attachment deletion epoch: %v", err)
	}
	claim := recorddeletion.OperationOwnerClaim{OperationID: operation.OperationID}
	if err := fixture.db.QueryRow(ctx, `insert into public.record_purge_operations (
		operation_id,reservation_id,project_id,operation_state,ledger_sequence,ledger_entry_hash,deployment_id,actor_id,
		reason_code,deletion_contract_version,ledger_entry_type,witness_proof_digest,owner_id,owner_generation,owner_expires_at
	) values ($1,$2,'default','online_purging',$3,$4,$5,$6,'user_confirmed',1,'delete_commit',$7,
		'attachment_deletion_worker',1,clock_timestamp()+interval '1 hour')
	returning owner_id,owner_generation,owner_expires_at`, operation.OperationID, operation.ReservationID,
		int64(entry.Sequence), entry.EntryHash[:], string(deployment), request.ActorID, proof[:]).
		Scan(&claim.Owner.OwnerID, &claim.Owner.Generation, &claim.Owner.ExpiresAt); err != nil {
		t.Fatalf("insert verified attachment purge operation: %v", err)
	}
	return recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}
}

func seedAttachmentDeletionOperation(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	operation recorddeletion.DeletionOperation,
	currentRevisionID string,
) {
	t.Helper()
	digest := testStoreRecordPlatformDigest(0x93)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.deletion_reservations (
			reservation_id, project_id, object_kind, object_id,
			deletion_token_commitment, request_fingerprint, state, fence_epoch,
			expires_at, completed_at, actor_scope_digest, preview_binding_digest,
			preview_current_revision_id, preview_lock_version,
			preview_authorization_epoch, preview_content_delivery_epoch,
			preview_dependency_graph_digest, preview_backup_inventory_digest,
			preview_processor_inventory_digest, adapter_readiness_digest,
			adapter_preview_digest, preview_witness_sequence,
			preview_witness_entry_hash
		) values (
			$1, 'default', 'record', $2, $3, $3, 'committed', $4,
			transaction_timestamp() + interval '1 hour', transaction_timestamp(),
			$3, $3, $5, 1, 1, 0, $3, $3, $3, $3, $3, 1, $3
		)`,
		operation.ReservationID, operation.Object.ObjectID, digest[:], int64(operation.FenceEpoch), currentRevisionID,
	); err != nil {
		t.Fatalf("insert attachment deletion reservation: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_purge_operations (
			operation_id, reservation_id, project_id, operation_state,
			ledger_sequence, ledger_entry_hash, deployment_id, actor_id,
			reason_code, deletion_contract_version, ledger_entry_type,
			witness_proof_digest, owner_id, owner_generation, owner_expires_at
		) values (
			$1, $2, 'default', 'online_purging', $3, $4,
			'dp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
			'usr_records1', 'user_confirmed', 1, 'delete_commit', $5,
			'attachment_deletion_worker', 1, transaction_timestamp() + interval '1 hour'
		)`,
		operation.OperationID, operation.ReservationID, int64(operation.LedgerSequence),
		operation.LedgerEntryHash[:], digest[:],
	); err != nil {
		t.Fatalf("insert attachment purge operation: %v", err)
	}
}

var _ = time.Second

type attachmentDeletionObservingBlobStore struct {
	attachments.BlobGCDeleter
	beforeDelete func(attachments.ObjectVersion) error
}

func (store *attachmentDeletionObservingBlobStore) DeleteGCVersion(
	ctx context.Context,
	version attachments.ObjectVersion,
) (attachments.DeletionReceipt, error) {
	if store.beforeDelete != nil {
		if err := store.beforeDelete(version); err != nil {
			return attachments.DeletionReceipt{}, err
		}
	}
	return store.BlobGCDeleter.DeleteGCVersion(ctx, version)
}

func seedLocalAttachmentDeletionOperation(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture,
	operation recorddeletion.DeletionOperation, revisionID string) recorddeletion.OperationOwnerClaim {
	t.Helper()
	digest := sha256.Sum256([]byte(operation.OperationID))
	var expiresAt time.Time
	if err := fixture.db.QueryRow(ctx, `select clock_timestamp() + interval '1 hour'`).Scan(&expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.deletion_reservations(
		reservation_id,project_id,object_kind,object_id,deletion_token_commitment,request_fingerprint,state,fence_epoch,
		owner_id,owner_generation,owner_expires_at,
		expires_at,actor_scope_digest,preview_binding_digest,preview_current_revision_id,preview_lock_version,
		preview_authorization_epoch,preview_content_delivery_epoch,adapter_readiness_digest,adapter_preview_digest,deletion_contract_version)
		values($1,'default','record',$2,$3,$3,'fenced',$4,'local_attachment_worker',1,$5,
		$5,$3,$3,$6,1,1,0,$3,$3,2)`, operation.ReservationID, operation.Object.ObjectID, digest[:], int64(operation.FenceEpoch), expiresAt, revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=$2,updated_at=clock_timestamp()
		where project_id='default' and object_kind='record' and object_id=$1`, operation.Object.ObjectID, int64(operation.FenceEpoch)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.deletion_fence_leases(
		project_id,object_kind,object_id,owner_id,owner_generation,expires_at)
		values('default','record',$1,'local_attachment_worker',1,$2)
		on conflict (project_id,object_kind,object_id) do update
		set owner_id=excluded.owner_id,owner_generation=excluded.owner_generation,expires_at=excluded.expires_at`,
		operation.Object.ObjectID, expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.content_workspace_purge_receipts (
		workspace_id, removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
	)
	select
		w.workspace_id,
		s.surface,
		sha256(int4send(octet_length(convert_to('houfeng.attachments.workspace-purge-receipt.v1','UTF8')))
			|| convert_to('houfeng.attachments.workspace-purge-receipt.v1','UTF8')
			|| int4send(octet_length(convert_to(w.workspace_id,'UTF8')))
			|| convert_to(w.workspace_id,'UTF8')
			|| s.surface
			|| int8send(1::bigint)
			|| int8send((extract(epoch from w.purged_at)*1000000)::bigint)),
		1::bigint,
		w.purged_at
	from public.content_processor_workspaces w
	join public.attachment_processor_jobs j on j.processor_job_id = w.processor_job_id
	join public.record_attachments a on a.attachment_id = j.attachment_id
	cross join lateral (
		select sha256(int4send(octet_length(convert_to('houfeng.attachments.workspace-purge-surface.v1','UTF8')))
			|| convert_to('houfeng.attachments.workspace-purge-surface.v1','UTF8')
			|| int4send(octet_length(convert_to(w.workspace_id,'UTF8')))
			|| convert_to(w.workspace_id,'UTF8')
			|| int8send(1::bigint)
			|| int4send(15)
			|| convert_to('verified_absent','UTF8')) as surface
	) s
	where a.record_id = $1 and w.workspace_state = 'purged' and w.purged_at is not null
	on conflict (workspace_id) do nothing`, operation.Object.ObjectID); err != nil {
		t.Fatal(err)
	}
	claim := recorddeletion.OperationOwnerClaim{OperationID: operation.OperationID}
	if err := fixture.db.QueryRow(ctx, `insert into public.record_purge_operations(
		operation_id,reservation_id,project_id,operation_state,deployment_id,actor_id,reason_code,deletion_contract_version,
		owner_id,owner_generation,owner_expires_at) values($1,$2,'default','online_purging',
		'dp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','usr_records1','user_confirmed',2,
		'local_attachment_worker',1,$3) returning owner_id,owner_generation,owner_expires_at`,
		operation.OperationID, operation.ReservationID, expiresAt).Scan(&claim.Owner.OwnerID, &claim.Owner.Generation, &claim.Owner.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_deletion_gates(
		operation_id,record_id,expected_lifecycle,expected_revision_id,expected_lock_version,expected_authorization_epoch,
		fence_epoch,actor_id,actor_scope_digest,request_fingerprint,binding_digest,authorization_evidence)
		values($1,$2,'active',$3,1,1,$4,'usr_records1',$5,$5,$5,$6)`, operation.OperationID, operation.Object.ObjectID,
		revisionID, int64(operation.FenceEpoch), digest[:], []byte("isolated fixture authorization evidence")); err != nil {
		t.Fatal(err)
	}
	return claim
}
