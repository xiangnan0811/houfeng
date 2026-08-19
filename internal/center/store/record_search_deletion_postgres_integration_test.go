package store

import (
	"context"
	"crypto/sha256"
	"testing"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
)

func TestPostgresIntegrationRecordSearchDeletionPurgesEveryGenerationAndReplays(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	purged := seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_searchdelete", "Purged parent")
	survivor := seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_searchkeep", "Survivor parent")

	digest := testStoreRecordPlatformDigest(0x71)
	indexed := []struct {
		recordID   string
		revisionID string
		title      string
		sourceID   string
	}{
		{recordID: purged.RecordID, revisionID: purged.RevisionID, title: "purged incident", sourceID: "vps_searchdelete"},
		{recordID: survivor.RecordID, revisionID: survivor.RevisionID, title: "unrelated incident", sourceID: "vps_searchkeep"},
	}
	// A published generation and a shadow generation being built. A purge that
	// only cleaned the published one would let the next publish restore the text.
	for _, generation := range []struct {
		id    int64
		state string
	}{{id: 1, state: "published"}, {id: 2, state: "building"}} {
		if _, err := fixture.db.Exec(ctx, `
			insert into public.record_search_generations (generation, generation_state, published_at)
			values ($1, $2, case when $2 = 'published' then transaction_timestamp() end)`,
			generation.id, generation.state,
		); err != nil {
			t.Fatalf("seed search generation %d: %v", generation.id, err)
		}
		for _, record := range indexed {
			if _, err := fixture.db.Exec(ctx, `
				insert into public.record_search_documents (
					generation, record_id, current_revision_id, record_lock_version,
					authorization_epoch, record_fence_epoch, lifecycle, record_type,
					impact_level, title, plain_text, visibility_kind, visibility_digest,
					record_created_at, record_updated_at, document_digest
				) values ($1, $2, $3, 1, 1, 0, 'active', 'troubleshooting',
					'medium', $4, 'private body text', 'project', $5,
					transaction_timestamp(), transaction_timestamp(), $5)`,
				generation.id, record.recordID, record.revisionID, record.title, digest[:],
			); err != nil {
				t.Fatalf("seed search document %s/%d: %v", record.recordID, generation.id, err)
			}
			if _, err := fixture.db.Exec(ctx, `
				insert into public.record_search_subjects (
					generation, record_id, subject_kind, relation_role, source_id, is_primary
				) values ($1, $2, 'vps', 'affected', $3, true)`,
				generation.id, record.recordID, record.sourceID,
			); err != nil {
				t.Fatalf("seed search subject %s/%d: %v", record.recordID, generation.id, err)
			}
		}
	}

	operation := recorddeletion.DeletionOperation{
		OperationID: "rpo_searchdelete", ReservationID: "drs_searchdelete",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: purged.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 7, LedgerSequence: 11, LedgerEntryHash: sha256.Sum256([]byte("search deletion ledger")),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, purged.RevisionID)
	repository := NewPostgresRecordDeletionRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-search-deletion", 2),
		allowRecordPlatformAdmissionGate,
	)
	adapter, err := recordsearch.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	health, err := adapter.HealthSnapshot(ctx)
	if err != nil || !health.Healthy() {
		t.Fatalf("HealthSnapshot() = %#v, %v", health, err)
	}
	preview, err := adapter.PreviewDeletion(ctx, recorddeletion.PreviewTarget{
		Object: operation.Object, CurrentRevisionID: purged.RevisionID,
		LockVersion: purged.LockVersion, AuthorizationEpoch: purged.AuthorizationEpoch,
		ContentDeliveryEpoch: 0, DependencyGraphDigest: sha256.Sum256([]byte("search graph")),
	})
	if err != nil {
		t.Fatalf("PreviewDeletion() error = %v", err)
	}
	// The index is derived state, so nothing here outlives the record.
	if len(preview.SurvivingCopies) != 0 {
		t.Fatalf("PreviewDeletion().SurvivingCopies = %#v, want none", preview.SurvivingCopies)
	}

	target := recorddeletion.PurgeTarget{Operation: operation}
	receipt, err := adapter.PurgeDeletion(ctx, target)
	if err != nil {
		t.Fatalf("PurgeDeletion() error = %v", err)
	}
	if receipt.RemovedRowCount != 4 {
		t.Fatalf("PurgeDeletion().RemovedRowCount = %d, want 4 (2 generations x document+subject)", receipt.RemovedRowCount)
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		t.Fatalf("VerifyDeletion() error = %v", err)
	}
	replay, err := adapter.PurgeDeletion(ctx, target)
	if err != nil || replay != receipt {
		t.Fatalf("PurgeDeletion(replay) = %#v, %v, want %#v", replay, err, receipt)
	}

	var remaining, surviving, receipts, generations int64
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select count(*) from public.record_search_documents where record_id = $1) +
		  (select count(*) from public.record_search_subjects where record_id = $1),
		  (select count(*) from public.record_search_documents where record_id = $2) +
		  (select count(*) from public.record_search_subjects where record_id = $2),
		  (select count(*) from public.record_search_purge_receipts where operation_id = $3),
		  (select count(*) from public.record_search_generations)`,
		purged.RecordID, survivor.RecordID, operation.OperationID,
	).Scan(&remaining, &surviving, &receipts, &generations); err != nil {
		t.Fatalf("read search deletion result: %v", err)
	}
	if remaining != 0 || receipts != 1 {
		t.Fatalf("remaining/receipts = %d/%d, want 0/1", remaining, receipts)
	}
	// One record's purge must not disturb other records or the generations every
	// other record is being served from.
	if surviving != 4 || generations != 2 {
		t.Fatalf("surviving rows/generations = %d/%d, want 4/2", surviving, generations)
	}
}

func TestPostgresIntegrationRecordSearchDeletionDoesNotCommitReceiptBeforeExactAbsence(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	parent := seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_searchblocked", "Blocked parent")
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_search_generations (generation, generation_state, published_at)
		values (1, 'published', transaction_timestamp())`); err != nil {
		t.Fatalf("seed blocked search generation: %v", err)
	}
	blockedDigest := testStoreRecordPlatformDigest(0x72)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_search_documents (
			generation, record_id, current_revision_id, record_lock_version,
			authorization_epoch, record_fence_epoch, lifecycle, record_type,
			impact_level, title, plain_text, visibility_kind, visibility_digest,
			record_created_at, record_updated_at, document_digest
		) values (1, $1, $2, 1, 1, 0, 'active', 'troubleshooting',
			'medium', 'blocked incident', 'private body text', 'project', $3,
			transaction_timestamp(), transaction_timestamp(), $3)`,
		parent.RecordID, parent.RevisionID, blockedDigest[:],
	); err != nil {
		t.Fatalf("seed blocked search document: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		create or replace function record_platform_internal.suppress_search_document_delete()
		returns trigger language plpgsql security invoker set search_path = pg_catalog
		as $$ begin return null; end $$;
		create trigger suppress_search_document_delete
		before delete on public.record_search_documents
		for each row execute function record_platform_internal.suppress_search_document_delete()`); err != nil {
		t.Fatalf("install blocked search delete trigger: %v", err)
	}
	operation := recorddeletion.DeletionOperation{
		OperationID: "rpo_searchblocked", ReservationID: "drs_searchblocked",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: parent.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 7, LedgerSequence: 12, LedgerEntryHash: sha256.Sum256([]byte("blocked search deletion ledger")),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, parent.RevisionID)
	repository := NewPostgresRecordDeletionRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-search-blocked-deletion", 2),
		allowRecordPlatformAdmissionGate,
	)
	adapter, err := recordsearch.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	if _, err := adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation}); err == nil {
		t.Fatal("PurgeDeletion(suppressed owned row) error = nil, want fail closed")
	}
	var blockedRows, blockedReceipts int
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select count(*)::int from public.record_search_documents where record_id = $1),
		  (select count(*)::int from public.record_search_purge_receipts where operation_id = $2)`,
		parent.RecordID, operation.OperationID,
	).Scan(&blockedRows, &blockedReceipts); err != nil {
		t.Fatalf("read blocked search purge state: %v", err)
	}
	if blockedRows != 1 || blockedReceipts != 0 {
		t.Fatalf("blocked search rows/receipts = %d/%d, want 1/0 rollback", blockedRows, blockedReceipts)
	}
}

// seedRecordWithoutSearchProjection commits with no search participant, which is
// what a record written before the index looks like. Revision identity is derived
// from the payload, so seeded records need distinct titles or their revision rows
// collide.
func seedRecordWithoutSearchProjection(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	recordID string,
	title string,
) records.RevisionCommitResult {
	t.Helper()
	repository := newRecordsPostgresRepository(t, fixture.openDirectRuntimePool(t, ctx, recordID+"-seed", 1))
	result, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, recordID, "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, title), recordID+"-seed",
	))
	if err != nil {
		t.Fatalf("CommitRevision(%s) error = %v", recordID, err)
	}
	return result
}
