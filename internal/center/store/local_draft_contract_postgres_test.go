package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/records"

	"github.com/jackc/pgx/v5"
)

func TestPostgresIntegrationLocalDraftContract(t *testing.T) {
	t.Run("direct_published_delete", func(t *testing.T) {
		ctx := context.Background()
		fixture := newRecordsPostgresFixture(t, ctx)
		pool := fixture.openDirectRuntimePool(t, ctx, "direct-published-draft-delete", 2)
		drafts := newRecordsPostgresDraftRepository(pool)
		attachmentsRepo := NewPostgresAttachmentRepository(pool)
		writer := newDraftReleaseWriter(t, ctx, fixture)
		draft := createRecordsPostgresDraft(t, ctx, drafts, "rdf_contractdirect", recordsPostgresDraftPayload(t, "before"))
		attachmentID := seedDraftReleaseUpload(t, ctx, attachmentsRepo, writer, draft.DraftID, 0, draftReleaseAvailable)
		patched, err := drafts.PatchDraft(ctx, records.DraftPatchCommand{DraftID: draft.DraftID, AuthorID: draft.AuthorID, IfMatch: draft.ETag, Payload: recordsPostgresDraftPayload(t, "after"), Policy: records.DefaultDraftRetentionPolicy()})
		if err != nil {
			t.Fatal(err)
		}
		if err := drafts.DeleteDraft(ctx, records.DraftDeleteCommand{DraftID: patched.DraftID, AuthorID: patched.AuthorID, Reason: records.DraftDeletePublished, IfMatch: draft.ETag}); !errors.Is(err, records.ErrDraftConflict) {
			t.Fatalf("stale published delete: %v", err)
		}
		if err := drafts.DeleteDraft(ctx, records.DraftDeleteCommand{DraftID: patched.DraftID, AuthorID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", Reason: records.DraftDeletePublished, IfMatch: patched.ETag}); !errors.Is(err, records.ErrDraftNotFound) {
			t.Fatalf("foreign published delete: %v", err)
		}
		var draftCount, checkpoints, owned int
		if err := fixture.db.QueryRow(ctx, `select (select count(*) from public.record_drafts where draft_id=$1), (select count(*) from public.record_draft_checkpoints where draft_id=$1), (select count(*) from public.record_attachments where attachment_id=$2)`, draft.DraftID, attachmentID).Scan(&draftCount, &checkpoints, &owned); err != nil || draftCount != 1 || checkpoints != 1 || owned != 1 {
			t.Fatalf("rejected deletion changed owned data: draft=%d checkpoints=%d attachment=%d: %v", draftCount, checkpoints, owned, err)
		}
		if err := drafts.DeleteDraft(ctx, records.DraftDeleteCommand{DraftID: patched.DraftID, AuthorID: patched.AuthorID, Reason: records.DraftDeletePublished, IfMatch: patched.ETag}); err != nil {
			t.Fatal(err)
		}
		assertDraftReleaseRowsGone(t, ctx, fixture, draft.DraftID)
		assertDraftReleaseQuota(t, ctx, fixture, 0, 0, draftReleaseSize)
		assertRecordsPostgresDraftFormalSideEffects(t, ctx, fixture.db, 0, 0)
	})

	for _, cutpoint := range []string{"attachment_release", "checkpoint_delete"} {
		t.Run("publish_rollback_"+cutpoint, func(t *testing.T) {
			ctx := context.Background()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			pool := fixture.openDirectRuntimePool(t, ctx, "draft-contract-rollback", 2)
			drafts := NewPostgresRecordDraftRepository(pool, fixture.admissionGate)
			attachmentRepo := NewPostgresAttachmentRepository(pool)
			writer := newDraftReleaseWriter(t, ctx, fixture)
			draft := createRecordsPostgresDraft(t, ctx, drafts, "rdf_contractrollback", recordsPostgresDraftPayload(t, "before"))
			seedDraftReleaseUpload(t, ctx, attachmentRepo, writer, draft.DraftID, 0, draftReleaseAvailable)
			draft, err := drafts.PatchDraft(ctx, records.DraftPatchCommand{DraftID: draft.DraftID, AuthorID: draft.AuthorID, IfMatch: draft.ETag, Payload: recordsPostgresDraftPayload(t, "checkpoint"), Policy: records.DefaultDraftRetentionPolicy()})
			if err != nil {
				t.Fatal(err)
			}
			birthFixture := ImportAttachmentGCFixture{Runtime: pool, Gate: fixture.admissionGate}
			repository := newRevisionMutationAdmittedRepository(t, birthFixture)
			command := revisionBirthBatchCommand(t, ctx, birthFixture, recordsPostgresCompleteRevisionInput(t, "publish after rollback"), "draft-contract-rollback")
			command.DraftID, command.DraftETag = draft.DraftID, draft.ETag
			snapshot := func() string {
				t.Helper()
				var got string
				if err := fixture.db.QueryRow(ctx, `select jsonb_build_object(
					'draft',(select to_jsonb(d) from public.record_drafts d where draft_id=$1),
					'checkpoints',(select coalesce(jsonb_agg(to_jsonb(c) order by checkpoint_id),'[]'::jsonb) from public.record_draft_checkpoints c where draft_id=$1),
					'attachments',(select coalesce(jsonb_agg(to_jsonb(a) order by attachment_id),'[]'::jsonb) from public.record_attachments a where draft_id=$1),
					'uploads',(select coalesce(jsonb_agg(to_jsonb(u) order by upload_id),'[]'::jsonb) from public.attachment_uploads u where origin_draft_id=$1),
					'parts',(select coalesce(jsonb_agg(to_jsonb(p) order by upload_id,part_number),'[]'::jsonb) from public.attachment_upload_parts p join public.attachment_uploads u using(upload_id) where u.origin_draft_id=$1),
					'jobs',(select coalesce(jsonb_agg(to_jsonb(j) order by processor_job_id),'[]'::jsonb) from public.attachment_processor_jobs j join public.attachment_uploads u using(upload_id) where u.origin_draft_id=$1),
					'quota',(select to_jsonb(q) from public.attachment_quota_accounts q where project_id='default'),
					'blobs',(select coalesce(jsonb_agg(to_jsonb(b) order by blob_key,object_version),'[]'::jsonb) from public.blob_objects b),
					'obligations',(select coalesce(jsonb_agg(to_jsonb(o) order by obligation_id),'[]'::jsonb) from public.record_write_obligations o)
				)::text`, draft.DraftID).Scan(&got); err != nil {
					t.Fatal(err)
				}
				return got
			}
			before := snapshot()
			table := "record_attachments"
			if cutpoint == "checkpoint_delete" {
				table = "record_draft_checkpoints"
			}
			if _, err := fixture.db.Exec(ctx, fmt.Sprintf(`create function public.reject_draft_contract_cleanup() returns trigger language plpgsql as $$ begin raise exception 'draft contract cleanup failure'; end $$; create trigger reject_draft_contract_cleanup before delete on public.%s for each row execute function public.reject_draft_contract_cleanup()`, table)); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.CommitRevision(ctx, command); err == nil || !strings.Contains(err.Error(), "draft contract cleanup failure") {
				t.Fatalf("cleanup failure was not reached: %v", err)
			}
			if after := snapshot(); after != before {
				t.Fatalf("failed publication changed draft, attachment, quota, or obligations:\nbefore %s\nafter  %s", before, after)
			}
			var roots, revisions int
			if err := fixture.db.QueryRow(ctx, `select (select count(*) from public.records where record_id=$1),(select count(*) from public.record_revisions where record_id=$1)`, command.RecordID).Scan(&roots, &revisions); err != nil || roots != 0 || revisions != 0 {
				t.Fatalf("failed publication left root/revision: %d/%d %v", roots, revisions, err)
			}
			assertRecordsPostgresDraftFormalSideEffects(t, ctx, fixture.db, 0, 0)
			if _, err := fixture.db.Exec(ctx, fmt.Sprintf(`drop trigger reject_draft_contract_cleanup on public.%s; drop function public.reject_draft_contract_cleanup()`, table)); err != nil {
				t.Fatal(err)
			}
			result, err := repository.CommitRevision(ctx, command)
			if err != nil || !result.Created || result.Replayed {
				t.Fatalf("retry after rollback: %#v %v", result, err)
			}
			assertDraftReleaseRowsGone(t, ctx, fixture, draft.DraftID)
		})
	}
}

// The transaction and its results remain PostgreSQL's; the barrier stops the
// consumer after its candidate query has begun, before it acquires ownership.
type draftDiscoveryBarrierTx struct {
	pgx.Tx
	reached chan struct{}
	resume  <-chan struct{}
	blocked bool
}

func (tx *draftDiscoveryBarrierTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := tx.Tx.Query(ctx, sql, args...)
	if err != nil || tx.blocked {
		return rows, err
	}
	tx.blocked = true
	close(tx.reached)
	select {
	case <-tx.resume:
		return rows, nil
	case <-ctx.Done():
		rows.Close()
		return nil, ctx.Err()
	}
}

func TestPostgresIntegrationLocalDraftCleanupContract(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "draft-cleanup-contract", 2)
	drafts := NewPostgresRecordDraftRepository(pool, fixture.admissionGate)
	authorization := newRecordsPostgresAuthorization(t, pool, fixture.admissionGate)
	if err := drafts.ConfigureRecordMutationAuthorization(NewPostgresCollaborationMembershipReader(), authorization); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []uint64{0, 101} {
		if _, err := drafts.ClaimExpiredDrafts(ctx, limit); !errors.Is(err, records.ErrInvalidDraftCommand) {
			t.Fatalf("cleanup limit %d = %v", limit, err)
		}
	}
	empty, err := drafts.ClaimExpiredDrafts(ctx, 10)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty cleanup: %#v %v", empty, err)
	}
	for _, id := range []string{"rdf_contractlocked", "rdf_contractunlocked"} {
		createRecordsPostgresDraft(t, ctx, drafts, id, recordsPostgresDraftPayload(t, id))
		expireRecordsPostgresDraft(t, ctx, fixture, id, "1 day")
	}
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `select draft_id from public.record_drafts where draft_id='rdf_contractlocked' for update`); err != nil {
		t.Fatal(err)
	}
	claimed, err := drafts.ClaimExpiredDrafts(ctx, 2)
	if err != nil || len(claimed) != 1 || claimed[0] != "rdf_contractunlocked" {
		t.Fatalf("locked cleanup: %#v %v", claimed, err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	claimed, err = drafts.ClaimExpiredDrafts(ctx, 1)
	if err != nil || len(claimed) != 1 || claimed[0] != "rdf_contractlocked" {
		t.Fatalf("bounded cleanup after unlock: %#v %v", claimed, err)
	}

	birthFixture := ImportAttachmentGCFixture{Runtime: pool, Gate: fixture.admissionGate}
	recordsRepo := newRevisionMutationAdmittedRepository(t, birthFixture)
	created, err := recordsRepo.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birthFixture, recordsPostgresCompleteRevisionInput(t, "reservation race"), "draft-contract-reservation"))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := drafts.CreateDraft(ctx, records.DraftCreateCommand{DraftID: "rdf_contractreservation", ProjectID: "default", RecordID: created.RecordID, BaseRevisionID: created.RevisionID, AuthorID: recordsPostgresDraftAuthorID, Payload: recordsPostgresDraftPayload(t, "reservation race"), Policy: records.DefaultDraftRetentionPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	expireRecordsPostgresDraft(t, ctx, fixture, draft.DraftID, "1 day")
	reached, resume := make(chan struct{}), make(chan struct{})
	race := NewPostgresRecordDraftRepository(pool, fixture.admissionGate)
	if err := race.ConfigureRecordMutationAuthorization(NewPostgresCollaborationMembershipReader(), authorization); err != nil {
		t.Fatal(err)
	}
	race.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		tx, err := pool.BeginTx(ctx, options)
		if err != nil {
			return nil, err
		}
		return &draftDiscoveryBarrierTx{Tx: tx, reached: reached, resume: resume}, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := race.ClaimExpiredDrafts(runCtx, 1); result <- err }()
	select {
	case <-reached:
	case <-runCtx.Done():
		t.Fatal("candidate discovery barrier was not reached")
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.deletion_reservations (
		reservation_id,project_id,object_kind,object_id,deletion_token_commitment,request_fingerprint,
		actor_scope_digest,preview_binding_digest,preview_current_revision_id,preview_lock_version,
		preview_authorization_epoch,preview_content_delivery_epoch,preview_dependency_graph_digest,
		preview_backup_inventory_digest,preview_processor_inventory_digest,adapter_readiness_digest,
		adapter_preview_digest,preview_witness_sequence,preview_witness_entry_hash,state,expires_at,completed_at
	) values ('drs_contractrace','default','record',$1,decode(repeat('41',32),'hex'),decode(repeat('42',32),'hex'),
		decode(repeat('43',32),'hex'),decode(repeat('44',32),'hex'),$2,$3,$4,0,
		decode(repeat('45',32),'hex'),decode(repeat('46',32),'hex'),decode(repeat('47',32),'hex'),
		decode(repeat('48',32),'hex'),decode(repeat('49',32),'hex'),1,decode(repeat('4a',32),'hex'),
		'committed',transaction_timestamp()+interval '5 minutes',transaction_timestamp())`, created.RecordID, created.RevisionID, created.LockVersion, created.AuthorizationEpoch); err != nil {
		close(resume)
		t.Fatal(err)
	}
	close(resume)
	select {
	case err := <-result:
		if !errors.Is(err, records.ErrRecordDeletionReserved) {
			t.Fatalf("racing reservation cleanup = %v", err)
		}
	case <-runCtx.Done():
		t.Fatal("cleanup did not finish after reservation")
	}
	var count int
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_drafts where draft_id=$1`, draft.DraftID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("racing reservation lost draft: %d %v", count, err)
	}
}
