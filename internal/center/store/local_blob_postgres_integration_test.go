package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
)

func TestPostgresIntegrationLocalBlobObligationLifecycle(t *testing.T) {
	t.Run("unpublished_orphan_after_origin_deletion_and_reopen", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		fixture := newRecordsPostgresFixture(t, ctx)
		root := filepath.Join(t.TempDir(), "blob-root")
		blob, err := newPostgresLocalBlobStore(t, ctx, fixture, root)
		if err != nil {
			t.Fatal(err)
		}
		written := seedPostgresManagedLocalBlob(t, ctx, fixture, blob, []byte("unpublished durable local orphan"))
		var metadata, origins, tasks int
		if err := fixture.db.QueryRow(ctx, `select
			(select count(*) from public.blob_objects where blob_key=$1),
			(select count(*) from public.record_import_jobs j join public.record_write_obligations o on o.origin_id=j.import_job_id where o.obligation_id=$2),
			(select count(*) from public.blob_gc_deletions where blob_key=$1)`, written.Version.Key, written.ObligationID).Scan(&metadata, &origins, &tasks); err != nil {
			t.Fatal(err)
		}
		if metadata != 0 || origins != 0 || tasks != 0 {
			t.Fatalf("orphan setup still has metadata/origin/task: %d/%d/%d", metadata, origins, tasks)
		}
		if _, err := blob.Stat(ctx, written.Version); err != nil {
			t.Fatalf("materialized orphan missing: %v", err)
		}
		if err := blob.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := newPostgresLocalBlobStore(t, ctx, fixture, root)
		if err != nil {
			t.Fatal(err)
		}
		runtime := fixture.openDirectRuntimePool(t, ctx, "orphan-recovery", 2)
		coordinator, err := attachments.NewLocalCleanupCoordinator(reopened, NewPostgresLocalBlobRepository(runtime, nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		worker, err := attachments.NewBlobGCWorker(NewPostgresAttachmentRepository(runtime), reopened,
			attachments.BlobGCWorkerOptions{BackendKind: attachments.BackendKindLocal, LocalCleanup: coordinator,
				Now: func() time.Time { return time.Now().Add(48 * time.Hour) }})
		if err != nil {
			t.Fatal(err)
		}
		if worked, err := worker.RunOnce(ctx); err != nil || !worked {
			t.Fatalf("recover unpublished orphan: worked=%t error=%v", worked, err)
		}
		if _, err := reopened.Stat(ctx, written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
			t.Fatalf("recovered orphan remains: %v", err)
		}
		var outcome string
		var receipt []byte
		var settled bool
		if err := fixture.db.QueryRow(ctx, `select cleanup_outcome,cleanup_receipt,cleanup_completed_at is not null and quota_settled_at is not null
			from public.record_write_obligations where obligation_id=$1`, written.ObligationID).Scan(&outcome, &receipt, &settled); err != nil {
			t.Fatal(err)
		}
		if outcome != "deleted" || len(receipt) != 32 || !settled {
			t.Fatalf("orphan completion: outcome=%q receipt=%x settled=%t", outcome, receipt, settled)
		}
		if worked, err := worker.RunOnce(ctx); err != nil || worked {
			t.Fatalf("completed orphan selected again: worked=%t error=%v", worked, err)
		}
		var completed int
		if err := fixture.db.QueryRow(ctx, `select count(*) from public.blob_gc_deletions where blob_key=$1 and deletion_state='completed' and receipt_digest=$2`, written.Version.Key, receipt).Scan(&completed); err != nil {
			t.Fatal(err)
		}
		if completed != 1 {
			t.Fatalf("durable orphan completion count=%d, want exactly one", completed)
		}
	})
}
