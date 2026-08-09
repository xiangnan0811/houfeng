package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresIntegrationAttachmentDeletionPreservesSharedAndPurgesExclusiveBlob(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	targetRecord, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_attdeleteone", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Attachment deletion target"), "attachment-deletion-target",
	))
	if err != nil {
		t.Fatalf("CommitRevision(target) error = %v", err)
	}
	otherRecord, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_attdeletetwo", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Attachment deletion survivor"), "attachment-deletion-survivor",
	))
	if err != nil {
		t.Fatalf("CommitRevision(other) error = %v", err)
	}

	blobStore, err := attachments.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	sharedContent := []byte("shared attachment deletion content\n")
	exclusiveContent := []byte("exclusive attachment deletion content\n")
	sharedDigest := sha256.Sum256(sharedContent)
	exclusiveDigest := sha256.Sum256(exclusiveContent)
	shared, err := blobStore.Put(ctx, attachments.PutRequest{
		ExpectedSHA256: sharedDigest, ExpectedSizeBytes: int64(len(sharedContent)),
	}, bytes.NewReader(sharedContent))
	if err != nil {
		t.Fatalf("Put(shared) error = %v", err)
	}
	exclusive, err := blobStore.Put(ctx, attachments.PutRequest{
		ExpectedSHA256: exclusiveDigest, ExpectedSizeBytes: int64(len(exclusiveContent)),
	}, bytes.NewReader(exclusiveContent))
	if err != nil {
		t.Fatalf("Put(exclusive) error = %v", err)
	}
	for _, object := range []attachments.ObjectVersion{shared, exclusive} {
		if _, err := fixture.db.Exec(ctx, `
			insert into public.blob_objects (
				blob_key, sha256_digest, object_version, size_bytes, backend_kind
			) values ($1, $2, $3, $4, 'local')`,
			object.Key, object.SHA256[:], object.VersionID, object.SizeBytes,
		); err != nil {
			t.Fatalf("insert Blob metadata: %v", err)
		}
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
			upload_id, part_number, size_bytes, sha256_digest, object_version
		) values ('aup_deleteexclusive', 1, $1, $2, $3)`,
		exclusive.SizeBytes, exclusive.SHA256[:], exclusive.VersionID); err != nil {
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
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_quota_accounts (
			project_id, logical_bytes, reserved_bytes, physical_bytes
		) values ('default', $1, 0, $2)`, logicalBytes, physicalBytes); err != nil {
		t.Fatalf("insert attachment quota: %v", err)
	}

	operation := recorddeletion.DeletionOperation{
		OperationID: "rpo_attdeleteone", ReservationID: "drs_attdeleteone",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: targetRecord.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7,
		LedgerSequence: 11, LedgerEntryHash: testStoreRecordPlatformDigest(0x92),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, targetRecord.RevisionID)

	repository := NewPostgresAttachmentRepository(runtimePool)
	deleteCalls := 0
	deletionBlobStore := &attachmentDeletionObservingBlobStore{
		BlobStore: blobStore,
		beforeDelete: func(version attachments.ObjectVersion) error {
			deleteCalls++
			if version != exclusive {
				return errors.New("attachment purge attempted a non-exclusive or inexact Blob delete")
			}
			var claimCount, targetAttachmentCount int
			if err := fixture.db.QueryRow(ctx, `
				select (select count(*)::int from public.blob_gc_deletions
				         where owner_id = $1 and purge_mode = 'permanent'
				           and blob_key = $2 and object_version = $3
				           and deletion_state = 'claimed'),
				       (select count(*)::int from public.record_attachments where record_id = $4)`,
				operation.OperationID, version.Key, version.VersionID, targetRecord.RecordID,
			).Scan(&claimCount, &targetAttachmentCount); err != nil {
				return err
			}
			if claimCount != 1 || targetAttachmentCount != 0 {
				return errors.New("attachment Blob delete ran before the durable SQL claim committed")
			}
			return nil
		},
	}
	if err := repository.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindLocal, deletionBlobStore); err != nil {
		t.Fatalf("ConfigureAttachmentDeletionBlobStore() error = %v", err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	target := recorddeletion.PurgeTarget{Operation: operation}
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
	if deleteCalls != 1 {
		t.Fatalf("exact external Blob delete calls = %d, want 1", deleteCalls)
	}
	if _, err := fixture.db.Exec(ctx, `
		delete from public.blob_gc_deletions
		where project_id = 'default' and purge_mode = 'permanent' and owner_id = $1`,
		operation.OperationID,
	); err != nil {
		t.Fatalf("delete completed attachment GC proof fixture: %v", err)
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("VerifyDeletion(missing completed GC proof) error = %v, want ErrDeletionSafetyUnavailable", err)
	}
}

func TestPostgresIntegrationAttachmentDeletionPurgesExclusiveS3BlobExactlyOnce(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion-s3", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	record, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_attdeletes3", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Attachment deletion S3 target"), "attachment-deletion-s3",
	))
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

	operation := recorddeletion.DeletionOperation{
		OperationID: "rpo_attdeletes3", ReservationID: "drs_attdeletes3",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 15,
		LedgerSequence: 16, LedgerEntryHash: testStoreRecordPlatformDigest(0xa3),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, record.RevisionID)

	repository := NewPostgresAttachmentRepository(runtimePool)
	deleteCalls := 0
	deletionBlobStore := &attachmentDeletionObservingBlobStore{
		BlobStore: blobStore,
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
				  and owner_id = $1 and blob_key = $2 and object_version = $3`,
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
	if err := repository.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindS3, deletionBlobStore); err != nil {
		t.Fatalf("ConfigureAttachmentDeletionBlobStore() error = %v", err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	target := recorddeletion.PurgeTarget{Operation: operation}
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
		  and owner_id = $1 and blob_key = $2 and object_version = $3`,
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

func TestPostgresIntegrationAttachmentDeletionRejectsOperationDriftAndActivePinWithoutMutation(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion-pin", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	record, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_attdeletepin", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Attachment deletion pin"), "attachment-deletion-pin",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	blobStore, err := attachments.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	content := []byte("pinned attachment deletion content\n")
	digest := sha256.Sum256(content)
	object, err := blobStore.Put(ctx, attachments.PutRequest{
		ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values ($1, $2, $3, $4, 'local')`,
		object.Key, object.SHA256[:], object.VersionID, object.SizeBytes); err != nil {
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
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_quota_accounts (
			project_id, logical_bytes, reserved_bytes, physical_bytes
		) values ('default', $1, 0, $1)`, object.SizeBytes); err != nil {
		t.Fatalf("insert pinned attachment quota: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_gc_pins (
			pin_id, pin_owner_kind, pin_owner_id, blob_key,
			blob_object_version, expires_at
		) values ('bgp_deletepinned', 'backup_manifest', 'manifest_deletepinned',
		  $1, $2, transaction_timestamp() + interval '1 hour')`, object.Key, object.VersionID); err != nil {
		t.Fatalf("insert active Blob pin: %v", err)
	}
	operation := recorddeletion.DeletionOperation{
		OperationID: "rpo_attdeletepin", ReservationID: "drs_attdeletepin",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 9,
		LedgerSequence: 13, LedgerEntryHash: testStoreRecordPlatformDigest(0xa1),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, record.RevisionID)
	repository := NewPostgresAttachmentRepository(runtimePool)
	if err := repository.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindLocal, blobStore); err != nil {
		t.Fatalf("ConfigureAttachmentDeletionBlobStore() error = %v", err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	drifted := operation
	drifted.LedgerSequence++
	if _, err := adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: drifted}); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("PurgeDeletion(drifted operation) error = %v, want ErrDeletionSafetyUnavailable", err)
	}
	if _, err := adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation}); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("PurgeDeletion(active pin) error = %v, want ErrDeletionSafetyUnavailable", err)
	}
	var attachmentCount, refCount, blobCount, pinCount, gcCount, receiptCount int
	var logicalBytes, physicalBytes int64
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_attachments where attachment_id = 'att_deletepinned'),
		       (select count(*)::int from public.record_revision_attachments where attachment_id = 'att_deletepinned'),
		       (select count(*)::int from public.blob_objects where blob_key = $1 and object_version = $2),
		       (select count(*)::int from public.blob_gc_pins where pin_id = 'bgp_deletepinned'),
		       (select count(*)::int from public.blob_gc_deletions where owner_id = $3),
		       (select count(*)::int from public.attachment_purge_receipts where operation_id = $3),
		       (select logical_bytes from public.attachment_quota_accounts where project_id = 'default'),
		       (select physical_bytes from public.attachment_quota_accounts where project_id = 'default')`,
		object.Key, object.VersionID, operation.OperationID,
	).Scan(&attachmentCount, &refCount, &blobCount, &pinCount, &gcCount, &receiptCount, &logicalBytes, &physicalBytes); err != nil {
		t.Fatalf("read pinned attachment rollback result: %v", err)
	}
	if attachmentCount != 1 || refCount != 1 || blobCount != 1 || pinCount != 1 || gcCount != 0 || receiptCount != 0 ||
		logicalBytes != object.SizeBytes || physicalBytes != object.SizeBytes {
		t.Fatalf("pinned rollback attachment/ref/blob/pin/gc/receipt logical/physical = %d/%d/%d/%d/%d/%d %d/%d",
			attachmentCount, refCount, blobCount, pinCount, gcCount, receiptCount, logicalBytes, physicalBytes)
	}
	if _, err := blobStore.Stat(ctx, object); err != nil {
		t.Fatalf("pinned Blob Stat() error = %v", err)
	}
}

func TestPostgresIntegrationAttachmentDeletionFailsClosedForActiveUploadPartial(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion-partial", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	record, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_attdeletepartial", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Attachment deletion active partial"), "attachment-deletion-partial",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	blobStore, err := attachments.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	content := []byte("active partial attachment deletion content\n")
	digest := sha256.Sum256(content)
	object, err := blobStore.Put(ctx, attachments.PutRequest{
		ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)),
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values ($1, $2, $3, $4, 'local')`,
		object.Key, object.SHA256[:], object.VersionID, object.SizeBytes); err != nil {
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
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_revision_attachments (
			record_id, revision_id, ordinal, attachment_id
		) values ($1, $2, 0, 'att_deletepartial')`, record.RecordID, record.RevisionID); err != nil {
		t.Fatalf("insert active-partial attachment ref: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_quota_accounts (
			project_id, logical_bytes, reserved_bytes, physical_bytes
		) values ('default', $1, $1, $1)`, object.SizeBytes); err != nil {
		t.Fatalf("insert active-partial attachment quota: %v", err)
	}
	operation := recorddeletion.DeletionOperation{
		OperationID: "rpo_attdeletepartial", ReservationID: "drs_attdeletepartial",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 10,
		LedgerSequence: 14, LedgerEntryHash: testStoreRecordPlatformDigest(0xa2),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, record.RevisionID)
	repository := NewPostgresAttachmentRepository(runtimePool)
	if err := repository.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindLocal, blobStore); err != nil {
		t.Fatalf("ConfigureAttachmentDeletionBlobStore() error = %v", err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	if _, err := adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation}); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("PurgeDeletion(active partial) error = %v, want ErrDeletionSafetyUnavailable", err)
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

func TestPostgresIntegrationAttachmentDeletionFailsClosedForActivePublicationIntent(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion-publication", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	record, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_attdeletepublication", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Attachment deletion publication"), "attachment-deletion-publication",
	))
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
		  $1, $2, $3, 'local', 'prepared', transaction_timestamp() + interval '1 hour')`,
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
	operation := recorddeletion.DeletionOperation{
		OperationID: "rpo_attdeletepublication", ReservationID: "drs_attdeletepublication",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 11,
		LedgerSequence: 15, LedgerEntryHash: testStoreRecordPlatformDigest(0xa5),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, record.RevisionID)
	repository := NewPostgresAttachmentRepository(runtimePool)
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	if _, err := adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation}); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("PurgeDeletion(active publication) error = %v, want ErrDeletionSafetyUnavailable", err)
	}

	var attachmentCount, uploadCount, publicationCount, receiptCount int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_attachments where attachment_id = 'att_deletepublication'),
		       (select count(*)::int from public.attachment_uploads where upload_id = 'aup_deletepublication'),
		       (select count(*)::int from public.blob_publication_intents where publication_id = 'bpi_deletepublication'),
		       (select count(*)::int from public.attachment_purge_receipts where operation_id = $1)`,
		operation.OperationID,
	).Scan(&attachmentCount, &uploadCount, &publicationCount, &receiptCount); err != nil {
		t.Fatalf("read active-publication rollback result: %v", err)
	}
	if attachmentCount != 1 || uploadCount != 1 || publicationCount != 1 || receiptCount != 0 {
		t.Fatalf("active-publication rollback attachment/upload/publication/receipt = %d/%d/%d/%d, want 1/1/1/0",
			attachmentCount, uploadCount, publicationCount, receiptCount)
	}
}

func TestPostgresMinIOIntegrationAttachmentDeletionPurgesExclusiveExactVersion(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "attachment-deletion-minio", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	record, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_attdeleteminio", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Attachment deletion MinIO"), "attachment-deletion-minio",
	))
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

	operation := recorddeletion.DeletionOperation{
		OperationID: "rpo_attdeleteminio", ReservationID: "drs_attdeleteminio",
		Object: recordplatform.ObjectRef{
			ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID,
		},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12,
		LedgerSequence: 16, LedgerEntryHash: testStoreRecordPlatformDigest(0xa4),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, record.RevisionID)
	repository := NewPostgresAttachmentRepository(runtimePool)
	if err := repository.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindS3, blobStore); err != nil {
		t.Fatalf("ConfigureAttachmentDeletionBlobStore(S3) error = %v", err)
	}
	adapter, err := attachments.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	target := recorddeletion.PurgeTarget{Operation: operation}
	receipt, err := adapter.PurgeDeletion(ctx, target)
	if err != nil {
		t.Fatalf("PurgeDeletion(MinIO) error = %v", err)
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		t.Fatalf("VerifyDeletion(MinIO) error = %v", err)
	}
	if replay, err := adapter.PurgeDeletion(ctx, target); err != nil || replay != receipt {
		t.Fatalf("PurgeDeletion(MinIO replay) = %#v, %v, want %#v", replay, err, receipt)
	}
	if _, err := blobStore.Stat(ctx, object); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("S3BlobStore.Stat(after purge) error = %v, want ErrBlobNotFound", err)
	}

	var logicalBytes, physicalBytes int64
	var attachmentCount, refCount, completedGC, receiptCount int
	var physicalResult string
	if err := fixture.db.QueryRow(ctx, `
		select (select logical_bytes from public.attachment_quota_accounts where project_id = 'default'),
		       (select physical_bytes from public.attachment_quota_accounts where project_id = 'default'),
		       (select count(*)::int from public.record_attachments where attachment_id = 'att_deleteminio'),
		       (select count(*)::int from public.record_revision_attachments where attachment_id = 'att_deleteminio'),
		       (select count(*)::int from public.blob_gc_deletions
		         where owner_id = $1 and backend_kind = 's3' and deletion_state = 'completed'),
		       (select physical_delete_result from public.blob_gc_deletions
		         where owner_id = $1 and backend_kind = 's3' and deletion_state = 'completed'),
		       (select count(*)::int from public.attachment_purge_receipts where operation_id = $1)`,
		operation.OperationID,
	).Scan(&logicalBytes, &physicalBytes, &attachmentCount, &refCount, &completedGC, &physicalResult, &receiptCount); err != nil {
		t.Fatalf("read MinIO attachment deletion result: %v", err)
	}
	if logicalBytes != 0 || physicalBytes != 0 || attachmentCount != 0 || refCount != 0 ||
		completedGC != 1 || physicalResult != "deleted" || receiptCount != 3 {
		t.Fatalf("MinIO deletion logical/physical attachment/ref gc/result/receipt = %d/%d %d/%d %d/%q/%d",
			logicalBytes, physicalBytes, attachmentCount, refCount, completedGC, physicalResult, receiptCount)
	}
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
	attachments.BlobStore
	beforeDelete func(attachments.ObjectVersion) error
}

func (store *attachmentDeletionObservingBlobStore) Delete(
	ctx context.Context,
	version attachments.ObjectVersion,
) (attachments.DeletionReceipt, error) {
	if store.beforeDelete != nil {
		if err := store.beforeDelete(version); err != nil {
			return attachments.DeletionReceipt{}, err
		}
	}
	return store.BlobStore.Delete(ctx, version)
}
