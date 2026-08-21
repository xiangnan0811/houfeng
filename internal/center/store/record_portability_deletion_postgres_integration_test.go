package store

import (
	"context"
	"crypto/sha256"
	"testing"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresIntegrationRecordPortabilityDeletionPurgesOwnedRowsKeepsTombstonesAndReplays(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	purged := seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_portdelete", "Purged portability parent")
	survivor := seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_portkeep", "Survivor portability parent")
	digest := testStoreRecordPlatformDigest(0x58)

	seedPortabilityExport(t, ctx, fixture, "rej_portdelete", "rxa_portdelete", purged.RecordID, purged.RevisionID, digest, "published")
	seedPortabilityExport(t, ctx, fixture, "rej_portkeep", "rxa_portkeep", survivor.RecordID, survivor.RevisionID, digest, "published")
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_origins (
			origin_id, origin_kind, origin_digest, source_record_id
		) values ('ror_portdelete', 'import', $1, $2)`, digest[:], purged.RecordID); err != nil {
		t.Fatalf("seed purged origin: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_origin_tombstones (origin_digest, ledger_sequence)
		values ($1, 11)`, digest[:]); err != nil {
		t.Fatalf("seed origin tombstone: %v", err)
	}
	seedPortabilityImportTree(t, ctx, fixture, "rij_portexcl", "rip_portexcl", "ria_portexcl", "excl-key", digest, purged.RecordID)
	seedPortabilityImportTree(t, ctx, fixture, "rij_portshare", "rip_portshare", "ria_portshare", "share-key", digest, purged.RecordID)
	sharedSurvivorDigest := testStoreRecordPlatformDigest(0x59)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_import_entity_mappings (
			import_plan_id, entity_kind, source_id, source_identity_digest, target_id
		) values ('rip_portshare', 'record', $2, $1, $2)`, sharedSurvivorDigest[:], survivor.RecordID); err != nil {
		t.Fatalf("seed shared survivor mapping: %v", err)
	}

	operation := recorddeletion.DeletionOperation{
		OperationID: "rpo_portdelete", ReservationID: "drs_portdelete",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: purged.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 7, LedgerSequence: 11, LedgerEntryHash: sha256.Sum256([]byte("portability deletion ledger")),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, purged.RevisionID)
	repository := NewPostgresRecordDeletionRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-portability-deletion", 2),
		allowRecordPlatformAdmissionGate,
	)
	health, err := repository.PortabilityDeletionHealth(ctx)
	if err != nil || !health.Healthy() {
		t.Fatalf("PortabilityDeletionHealth() = %#v, %v", health, err)
	}
	preview, err := repository.PreviewPortabilityDeletion(ctx, recorddeletion.PreviewTarget{
		Object: operation.Object, CurrentRevisionID: purged.RevisionID,
		LockVersion: purged.LockVersion, AuthorizationEpoch: purged.AuthorizationEpoch,
		ContentDeliveryEpoch: 0, DependencyGraphDigest: sha256.Sum256([]byte("portability graph")),
	})
	if err != nil {
		t.Fatalf("PreviewPortabilityDeletion() error = %v", err)
	}
	if len(preview.SurvivingCopies) != 1 ||
		preview.SurvivingCopies[0].Kind != recorddeletion.SurvivingCopyKindDeliveredExport ||
		preview.SurvivingCopies[0].CopyCount != 1 {
		t.Fatalf("PreviewPortabilityDeletion().SurvivingCopies = %#v", preview.SurvivingCopies)
	}

	surfaceDigest := recorddeletion.RecordPortabilitySurfaceDigest()
	receipt, err := repository.PurgeRecordPortability(ctx, operation, surfaceDigest)
	if err != nil {
		t.Fatalf("PurgeRecordPortability() error = %v", err)
	}
	if receipt.RemovedRowCount == 0 {
		t.Fatal("PurgeRecordPortability() removed no owned rows")
	}
	if err := repository.VerifyRecordPortabilityPurge(ctx, operation, surfaceDigest, receipt); err != nil {
		t.Fatalf("VerifyRecordPortabilityPurge() error = %v", err)
	}
	replay, err := repository.PurgeRecordPortability(ctx, operation, surfaceDigest)
	if err != nil || replay != receipt {
		t.Fatalf("PurgeRecordPortability(replay) = %#v, %v, want %#v", replay, err, receipt)
	}

	var purgedJobs, survivingJobs, origins, tombstones, exclusiveJobs, sharedJobs, sharedMappings int64
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select count(*) from public.record_export_jobs where record_id = $1),
		  (select count(*) from public.record_export_jobs where record_id = $2),
		  (select count(*) from public.record_origins where source_record_id = $1),
		  (select count(*) from public.record_origin_tombstones),
		  (select count(*) from public.record_import_jobs where import_job_id = 'rij_portexcl'),
		  (select count(*) from public.record_import_jobs where import_job_id = 'rij_portshare'),
		  (select count(*) from public.record_import_entity_mappings
		    where import_plan_id = 'rip_portshare' and target_id = $2)`,
		purged.RecordID, survivor.RecordID,
	).Scan(&purgedJobs, &survivingJobs, &origins, &tombstones, &exclusiveJobs, &sharedJobs, &sharedMappings); err != nil {
		t.Fatalf("count remaining portability rows: %v", err)
	}
	if purgedJobs != 0 || survivingJobs != 1 || origins != 0 || tombstones != 1 ||
		exclusiveJobs != 0 || sharedJobs != 1 || sharedMappings != 1 {
		t.Fatalf("remaining rows jobs=%d/%d origins=%d tombstones=%d exclusive=%d shared=%d mappings=%d",
			purgedJobs, survivingJobs, origins, tombstones, exclusiveJobs, sharedJobs, sharedMappings)
	}
}

func seedPortabilityExport(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	jobID string,
	artifactID string,
	recordID string,
	revisionID string,
	digest [32]byte,
	state string,
) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_export_jobs (
			export_job_id, actor_id, idempotency_key, export_kind, export_mode,
			job_state, lock_version, request_fingerprint, inventory_digest,
			authorization_epoch, record_id, revision_id, expires_at
		) values (
			$1, 'usr_records1', $1, 'markdown', 'safe', $2, 1, $3, $3, 1, $4, $5,
			transaction_timestamp() + interval '1 hour'
		)`, jobID, state, digest[:], recordID, revisionID); err != nil {
		t.Fatalf("seed export job %s: %v", jobID, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_export_artifacts (
			export_artifact_id, export_job_id, artifact_kind, content_type,
			backend_kind, blob_key, sha256, byte_size, expires_at
		) values (
			$1, $2, 'markdown', 'text/markdown', 'local', $3, $4, 32,
			transaction_timestamp() + interval '1 hour'
		)`, artifactID, jobID, "sha256/"+jobID, digest[:]); err != nil {
		t.Fatalf("seed export artifact %s: %v", artifactID, err)
	}
}

func seedPortabilityImportTree(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	jobID string,
	planID string,
	artifactID string,
	idempotencyKey string,
	digest [32]byte,
	recordID string,
) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_import_jobs (
			import_job_id, actor_id, idempotency_key, job_state,
			identity_classification, archive_digest, expires_at
		) values (
			$1, 'usr_records1', $2, 'applied', 'complete', $3,
			transaction_timestamp() + interval '1 hour'
		)`, jobID, idempotencyKey, digest[:]); err != nil {
		t.Fatalf("seed import job %s: %v", jobID, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_import_plans (
			import_plan_id, import_job_id, plan_digest, object_count, remap_count, expires_at
		) values (
			$1, $2, $3, 1, 0, transaction_timestamp() + interval '1 hour'
		)`, planID, jobID, digest[:]); err != nil {
		t.Fatalf("seed import plan %s: %v", planID, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_import_artifacts (
			import_artifact_id, import_job_id, artifact_role, backend_kind,
			blob_key, object_version_id, sha256, byte_size, expires_at
		) values (
			$1, $2, 'archive', 'local', $3, $5, $4, 32,
			transaction_timestamp() + interval '1 hour'
		)`, artifactID, jobID, "sha256/"+jobID, digest[:], "local-v1-"+jobID); err != nil {
		t.Fatalf("seed import artifact %s: %v", artifactID, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_import_entity_mappings (
			import_plan_id, entity_kind, source_id, source_identity_digest, target_id
		) values ($1, 'record', $3, $2, $3)`, planID, digest[:], recordID); err != nil {
		t.Fatalf("seed import mapping %s: %v", planID, err)
	}
}
