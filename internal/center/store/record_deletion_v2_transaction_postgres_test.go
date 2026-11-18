package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"houfeng/internal/center/activity"

	"houfeng/internal/center/recorddeletion"
	storemigrate "houfeng/internal/center/store/migrate"
)

// TestPostgresIntegrationRecordDeletionV2TransactionLocks uses direct worker
// sessions and observes PostgreSQL's actual blocker relationship.
func TestPostgresIntegrationRecordDeletionV2TransactionLocks(t *testing.T) {
	t.Run("client_receipt_wait_crosses_expiry", testV2ClientReceiptWait)
	t.Run("evidence_payload_final_materialization", testV2EvidencePayloadFinalMaterialization)
	t.Run("local_adapter_isolation", testV2LocalAdapterIsolation)
	for _, scenario := range []struct {
		name      string
		savepoint string
		commit    bool
		waits     bool
	}{
		{"commit", "", true, true},
		{"rollback", "", false, true},
		{"savepoint_rollback", "rollback", false, false},
		{"savepoint_release_commit", "release", true, true},
		{"savepoint_release_rollback", "release", false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			runtime := fixture.openDirectRuntimePool(t, ctx, "v2-lock-runtime", 2)
			workers := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v2-lock-worker", 4)
			if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workers); err != nil {
				t.Fatal(err)
			}
			record := seedAdmittedOutboxRecord(t, ctx, runtime, fixture.admissionGate, "v2-lock-record")
			if _, err := fixture.db.Exec(ctx, `insert into public.record_activity_projection_heads(project_id,projection_generation,head_state) values('default',1,'active')`); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.db.Exec(ctx, `insert into public.record_activity_projection(activity_id,project_id,projection_generation,ingest_sequence,event_kind,event_at,recorded_at,source_kind,source_event_id,source_version,record_id,revision_id,presentation_version,presentation_json,auth_scope_digest,canonical_hash) values('act_v2locks','default',1,1,'record_created',transaction_timestamp(),transaction_timestamp(),'record_domain','event_v2locks',1,$1,$2,1,'{}'::jsonb,decode(repeat('11',32),'hex'),decode(repeat('11',32),'hex'))`, record.RecordID, record.RevisionID); err != nil {
				t.Fatal(err)
			}
			expiry := issueV2WorkerFixture(t, ctx, fixture, runtime, record.RecordID, "locks")
			waitServingTestFenceExpired(t, ctx, fixture, expiry)
			repository := NewPostgresRecordDeletionRepository(workers, allowRecordPlatformAdmissionGate)
			claim, err := repository.ClaimV2DeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_lock_a", OwnerLeaseDuration: 5 * time.Second})
			if err != nil || claim == nil {
				t.Fatalf("initial claim: %v, %v", claim, err)
			}
			var originalActivity, originalQuota string
			if err := fixture.db.QueryRow(ctx, `select coalesce((select to_jsonb(p) from public.record_activity_projection p where p.record_id=$1),'null'::jsonb)::text,coalesce((select to_jsonb(q) from public.attachment_quota_accounts q where q.project_id='default'),'null'::jsonb)::text`, record.RecordID).Scan(&originalActivity, &originalQuota); err != nil || originalActivity == "null" {
				t.Fatalf("read original activity/quota: %v", err)
			}
			a, err := workers.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = a.Rollback(context.Background()) }()
			var aPID int
			if err := a.QueryRow(ctx, `select pg_backend_pid()`).Scan(&aPID); err != nil {
				t.Fatal(err)
			}
			target := recorddeletion.PurgeTarget{Operation: claim.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: claim.Operation.OperationID, Owner: claim.Owner}}
			body, err := deletionAdapterEnvelope(ctx, a, target, recorddeletion.RecordActivitySurfaceDigest())
			if err != nil {
				t.Fatal(err)
			}
			if scenario.savepoint != "" {
				if _, err := a.Exec(ctx, `savepoint before_activity`); err != nil {
					t.Fatal(err)
				}
			}
			var surface, receipt []byte
			var count int64
			var verified time.Time
			if err := a.QueryRow(ctx, `select removed_surface_digest,receipt_digest,removed_row_count,verified_absent_at from public.record_activity_purge_v2($1)`, body).Scan(&surface, &receipt, &count, &verified); err != nil || count != 1 {
				t.Fatalf("activity count=%d: %v", count, err)
			}
			switch scenario.savepoint {
			case "rollback":
				if _, err := a.Exec(ctx, `rollback to savepoint before_activity`); err != nil {
					t.Fatal(err)
				}
			case "release":
				if _, err := a.Exec(ctx, `release savepoint before_activity`); err != nil {
					t.Fatal(err)
				}
			}
			waitV2DeadlineInTransaction(t, ctx, fixture, a, claim.Owner.ExpiresAt)
			if scenario.waits {
				owner, err := deletionV2Owner(*claim)
				if err != nil {
					t.Fatal(err)
				}
				renewBody, err := encodeRecordPurgeCommand(struct {
					deletionV2OwnerEnvelope
					OwnerLeaseMicroseconds int64 `json:"owner_lease_microseconds"`
				}{owner, int64(time.Minute / time.Microsecond)})
				if err != nil {
					t.Fatal(err)
				}
				for _, query := range []struct {
					sql string
					arg []byte
				}{
					{`select * from public.record_activity_purge_v2($1)`, body},
					{`select * from public.record_activity_purge_verify_v2($1)`, body},
					{`select * from public.record_deletion_renew_v2($1)`, renewBody},
				} {
					if _, err := a.Exec(ctx, `savepoint after_activity`); err != nil {
						t.Fatal(err)
					}
					_, err = a.Exec(ctx, query.sql, query.arg)
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
						t.Fatalf("expired %s: %v", query.sql, err)
					}
					if _, err := a.Exec(ctx, `rollback to savepoint after_activity`); err != nil {
						t.Fatal(err)
					}
				}
				// The bootstrap connection sees the last committed state, not A's
				// uncommitted deletion. The blocker check below proves A still holds its locks.
				var currentExpiry time.Time
				var exists, rowExists bool
				if err := fixture.db.QueryRow(ctx, `select owner_expires_at,exists(select 1 from public.record_activity_purge_receipts where operation_id=$1),exists(select 1 from public.record_activity_projection where record_id=$2) from public.record_purge_operations where operation_id=$1`, claim.Operation.OperationID, record.RecordID).Scan(&currentExpiry, &exists, &rowExists); err != nil || !currentExpiry.Equal(claim.Owner.ExpiresAt) || exists || !rowExists {
					t.Fatalf("committed state while A holds locks expires=%v receipt=%t row=%t: %v", currentExpiry, exists, rowExists, err)
				}
			}
			b, err := workers.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = b.Rollback(context.Background()) }()
			var bPID int
			if err := b.QueryRow(ctx, `select pg_backend_pid()`).Scan(&bPID); err != nil {
				t.Fatal(err)
			}
			request, err := encodeRecordPurgeCommand(struct {
				OwnerID                string `json:"owner_id"`
				OwnerLeaseMicroseconds int64  `json:"owner_lease_microseconds"`
			}{"v2_lock_b", int64(time.Minute / time.Microsecond)})
			if err != nil {
				t.Fatal(err)
			}
			type claimResult struct {
				claim *recorddeletion.ClaimedDeletionWork
				err   error
			}
			done := make(chan claimResult, 1)
			go func() {
				claimed, err := scanV2DeletionWork(b.QueryRow(ctx, `select * from public.record_deletion_claim_v2($1)`, request), "v2_lock_b")
				done <- claimResult{claimed, err}
			}()
			if scenario.waits {
				waitForPostgresBlockingPID(t, ctx, fixture.db, bPID, aPID)
				if scenario.commit {
					err = a.Commit(ctx)
				} else {
					err = a.Rollback(ctx)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			var result claimResult
			select {
			case result = <-done:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if result.err != nil || result.claim == nil || result.claim.Owner.Generation != claim.Owner.Generation+1 {
				t.Fatalf("takeover: %v, %v", result.claim, result.err)
			}
			if err := b.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if !scenario.waits {
				if err := a.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var exists, rowExists bool
			if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_activity_purge_receipts where operation_id=$1),exists(select 1 from public.record_activity_projection where record_id=$2)`, claim.Operation.OperationID, record.RecordID).Scan(&exists, &rowExists); err != nil || exists != scenario.commit || rowExists == scenario.commit {
				t.Fatalf("receipt=%t row=%t, expected commit=%t: %v", exists, rowExists, scenario.commit, err)
			}
			var activityAfter, quotaAfter string
			var storedReceipt []byte
			if err := fixture.db.QueryRow(ctx, `select coalesce((select to_jsonb(p) from public.record_activity_projection p where p.record_id=$2),'null'::jsonb)::text,coalesce((select to_jsonb(q) from public.attachment_quota_accounts q where q.project_id='default'),'null'::jsonb)::text,(select receipt_digest from public.record_activity_purge_receipts where operation_id=$1)`, claim.Operation.OperationID, record.RecordID).Scan(&activityAfter, &quotaAfter, &storedReceipt); err != nil {
				t.Fatal(err)
			}
			if quotaAfter != originalQuota || (scenario.commit && (activityAfter != "null" || string(storedReceipt) != string(receipt))) || (!scenario.commit && (activityAfter != originalActivity || storedReceipt != nil)) {
				t.Fatalf("activity/receipt/quota mismatch after commit=%t", scenario.commit)
			}
			var ownerConsistent bool
			if err := fixture.db.QueryRow(ctx, `select o.owner_id=$2 and o.owner_generation=$3 and o.owner_expires_at=$4 and r.owner_id=o.owner_id and r.owner_generation=o.owner_generation and r.owner_expires_at=o.owner_expires_at and l.owner_id=o.owner_id and l.owner_generation=o.owner_generation and l.expires_at=o.owner_expires_at and r.state='fenced' and o.operation_state='online_purging' and exists(select 1 from public.record_deletion_gates g where g.operation_id=o.operation_id) from public.record_purge_operations o join public.deletion_reservations r on r.reservation_id=o.reservation_id join public.deletion_fence_leases l on l.project_id=o.project_id and l.object_kind='record' and l.object_id=r.object_id where o.operation_id=$1`, claim.Operation.OperationID, result.claim.Owner.OwnerID, result.claim.Owner.Generation, result.claim.Owner.ExpiresAt).Scan(&ownerConsistent); err != nil || !ownerConsistent {
				t.Fatalf("takeover owner/fence state valid=%t: %v", ownerConsistent, err)
			}
			if scenario.commit {
				newTarget := recorddeletion.PurgeTarget{Operation: result.claim.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: result.claim.Operation.OperationID, Owner: result.claim.Owner}}
				replay, err := repository.PurgeRecordActivity(ctx, activity.DeletionCommand{PurgeTarget: newTarget, SurfaceDigest: recorddeletion.RecordActivitySurfaceDigest()})
				if err != nil || string(replay.ReceiptDigest[:]) != string(receipt) {
					t.Fatalf("activity replay: %v, %v", replay, err)
				}
			}
		})
	}
}

func testV2ClientReceiptWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "v2-receipt-runtime", 2)
	workers := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v2-receipt-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workers); err != nil {
		t.Fatal(err)
	}
	record := seedAdmittedOutboxRecord(t, ctx, runtime, fixture.admissionGate, "v2-receipt-record")
	if _, err := runtime.Exec(ctx, `insert into public.markdown_client_draft_buffers(user_id,browser_id,record_id,project_id,session_generation,cleanup_generation) values('usr_records1','v2-receipt-browser',$1,'default',1,0)`, record.RecordID); err != nil {
		t.Fatalf("register markdown draft buffer: %v", err)
	}
	var originalMetadata string
	var originalCount int
	if err := fixture.db.QueryRow(ctx, `select jsonb_build_array(coalesce((select jsonb_agg(to_jsonb(b) order by user_id,browser_id) from public.markdown_client_draft_buffers b where project_id='default' and record_id=$1),'[]'::jsonb),coalesce((select jsonb_agg(to_jsonb(w) order by user_id,browser_id) from public.markdown_client_workspaces w where project_id='default' and record_id=$1),'[]'::jsonb))::text,(select count(*) from public.markdown_client_draft_buffers where project_id='default' and record_id=$1)`, record.RecordID).Scan(&originalMetadata, &originalCount); err != nil || originalCount != 1 {
		t.Fatalf("read original markdown metadata count=%d: %v", originalCount, err)
	}
	expiry := issueV2WorkerFixture(t, ctx, fixture, runtime, record.RecordID, "receipt")
	waitServingTestFenceExpired(t, ctx, fixture, expiry)
	repository := NewPostgresRecordDeletionRepository(workers, allowRecordPlatformAdmissionGate)
	claim, err := repository.ClaimV2DeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_receipt", OwnerLeaseDuration: 10 * time.Second})
	if err != nil || claim == nil {
		t.Fatalf("claim: %v, %v", claim, err)
	}
	_, err = fixture.db.Exec(ctx, `create function public.v2_test_receipt_wait() returns trigger language plpgsql as $$ begin if new.removed_row_count <> 1 then raise exception 'expected actual markdown deletion before receipt'; end if; perform pg_catalog.pg_advisory_xact_lock(1909861244,2); return new; end $$;
		create trigger v2_test_receipt_wait before insert on public.record_markdown_client_purge_receipts for each row execute function public.v2_test_receipt_wait()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := fixture.db.Exec(context.Background(), `drop trigger if exists v2_test_receipt_wait on public.record_markdown_client_purge_receipts; drop function if exists public.v2_test_receipt_wait()`); err != nil {
			t.Errorf("remove receipt trigger: %v", err)
		}
	})
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	var holderPID int
	if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `select pg_advisory_xact_lock(1909861244,2)`); err != nil {
		t.Fatal(err)
	}
	tx, err := workers.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var workerPID int
	if err := tx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
		t.Fatal(err)
	}
	target := recorddeletion.PurgeTarget{Operation: claim.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: claim.Operation.OperationID, Owner: claim.Owner}}
	body, err := deletionAdapterEnvelope(ctx, tx, target, recorddeletion.RecordMarkdownClientSurfaceDigest())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := tx.Exec(ctx, `select * from public.record_markdown_client_purge($1)`, body)
		done <- err
	}()
	waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
	var live bool
	if err := fixture.db.QueryRow(ctx, `select clock_timestamp() < $1::timestamptz`, claim.Owner.ExpiresAt).Scan(&live); err != nil || !live {
		t.Fatalf("owner expired before receipt wait: %t, %v", live, err)
	}
	waitServingTestFenceExpired(t, ctx, fixture, claim.Owner.ExpiresAt)
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
		t.Fatalf("receipt insert wait = %v, want 42501", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var exists bool
	var restoredMetadata string
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_markdown_client_purge_receipts where operation_id=$1),jsonb_build_array(coalesce((select jsonb_agg(to_jsonb(b) order by user_id,browser_id) from public.markdown_client_draft_buffers b where project_id='default' and record_id=$2),'[]'::jsonb),coalesce((select jsonb_agg(to_jsonb(w) order by user_id,browser_id) from public.markdown_client_workspaces w where project_id='default' and record_id=$2),'[]'::jsonb))::text`, claim.Operation.OperationID, record.RecordID).Scan(&exists, &restoredMetadata); err != nil || exists || restoredMetadata != originalMetadata {
		t.Fatalf("failed command receipt=%t restored=%t: %v", exists, restoredMetadata == originalMetadata, err)
	}
}
