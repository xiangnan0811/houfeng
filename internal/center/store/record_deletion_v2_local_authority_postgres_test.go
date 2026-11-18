package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recorddeletion"
	storemigrate "houfeng/internal/center/store/migrate"
)

// The worker's actual SQL command must have reached its quota or root lock
// while the original owner tuple was still live. The lock holder then crosses
// the database deadline before releasing the resource.
func TestPostgresIntegrationRecordDeletionV2LocalAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, command, prepare, lock string
		attachment                   bool
	}{
		{"attachment prepare", "local_attachment_purge_prepare", "", "quota", true},
		{"attachment finish", "local_attachment_purge_complete", "local_attachment_purge_prepare", "quota", true},
		{"portability prepare", "local_portability_purge_prepare", "", "quota", false},
		{"portability finish", "local_portability_purge_complete", "local_portability_purge_prepare", "quota", false},
		{"root wait", "local_attachment_purge_prepare", "", "root", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			runtime := fixture.openDirectRuntimePool(t, ctx, "v2-local-authority-runtime", 2)
			worker := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v2-local-authority-worker", 2)
			if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, worker); err != nil {
				t.Fatal(err)
			}
			blob, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "blob-root"))
			if err != nil {
				t.Fatal(err)
			}
			record := seedAdmittedOutboxRecord(t, ctx, runtime, fixture.admissionGate, "v2-local-authority")
			if _, err := fixture.db.Exec(ctx, `insert into public.attachment_quota_accounts(project_id) values ('default') on conflict (project_id) do nothing`); err != nil {
				t.Fatal(err)
			}
			var logicalBytes, reservedBytes, physicalBytes int64
			if err := fixture.db.QueryRow(ctx, `select logical_bytes,reserved_bytes,physical_bytes from public.attachment_quota_accounts where project_id='default'`).Scan(&logicalBytes, &reservedBytes, &physicalBytes); err != nil || logicalBytes != 0 || reservedBytes != 0 || physicalBytes != 0 {
				t.Fatalf("initial quota = %d/%d/%d, %v; want 0/0/0", logicalBytes, reservedBytes, physicalBytes, err)
			}
			if tc.attachment {
				if _, err := fixture.db.Exec(ctx, `insert into public.record_attachments
					(attachment_id,project_id,record_id,origin_draft_id,attachment_state,display_name,media_type,logical_size_bytes,created_by)
					values ('att_v2authority','default',$1,'rdf_v2authority','uploading','pending.txt','text/plain',17,'usr_records1')`, record.RecordID); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.db.Exec(ctx, `insert into public.attachment_uploads
					(upload_id,project_id,attachment_id,origin_draft_id,author_id,upload_state,transport_kind,
					 declared_size_bytes,reserved_size_bytes,expires_at)
					values ('aup_v2authority','default','att_v2authority','rdf_v2authority','usr_records1',
					 'uploading','local',17,17,clock_timestamp()+interval '1 hour')`); err != nil {
					t.Fatal(err)
				}
				if err := fixture.db.QueryRow(ctx, `update public.attachment_quota_accounts set reserved_bytes=reserved_bytes+17 where project_id='default' returning logical_bytes,reserved_bytes,physical_bytes`).Scan(&logicalBytes, &reservedBytes, &physicalBytes); err != nil || logicalBytes != 0 || reservedBytes != 17 || physicalBytes != 0 {
					t.Fatalf("reserved upload quota = %d/%d/%d, %v; want 0/17/0", logicalBytes, reservedBytes, physicalBytes, err)
				}
			} else {
				if _, err := fixture.db.Exec(ctx, `insert into public.record_export_jobs
					(export_job_id,actor_id,idempotency_key,export_kind,export_mode,job_state,request_fingerprint,inventory_digest,
					 authorization_epoch,record_id,revision_id,owner_id,owner_generation,lease_expires_at,expires_at)
					values ('rej_v2authority','usr_aaaaaaaaaaaaaaaaaaaaaaaa','v2authority','markdown','safe','staging',
					 decode(repeat('12',32),'hex'),decode(repeat('12',32),'hex'),1,$1,$2,'export_worker',1,
					 clock_timestamp()+interval '30 minutes',clock_timestamp()+interval '1 hour')`, record.RecordID, record.RevisionID); err != nil {
					t.Fatal(err)
				}
			}
			issuerExpiry := issueV2WorkerFixture(t, ctx, fixture, runtime, record.RecordID, "localauthority")
			issuerWait, err := fixture.db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			waitV2DeadlineInTransaction(t, ctx, fixture, issuerWait, issuerExpiry)
			if err := issuerWait.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			repo := NewPostgresRecordDeletionRepository(worker, allowRecordPlatformAdmissionGate)
			claim, err := repo.ClaimV2DeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_local_worker", OwnerLeaseDuration: time.Minute})
			if err != nil || claim == nil {
				t.Fatalf("claim original owner: %v, %v", claim, err)
			}
			surface := recorddeletion.RecordPortabilitySurfaceDigest()
			if tc.attachment {
				surface = recorddeletion.RecordAttachmentsSurfaceDigest()
			}
			makeBody := func() []byte {
				body, err := json.Marshal(map[string]any{
					"operation_id": claim.Operation.OperationID, "operation_owner_id": claim.Owner.OwnerID,
					"operation_generation": claim.Owner.Generation, "operation_expires_at": claim.Owner.ExpiresAt,
					"namespace_id": blob.Namespace().NamespaceID, "incarnation_id": blob.Namespace().IncarnationID,
					"record_id": record.RecordID, "reservation_id": claim.Operation.ReservationID,
					"fence_epoch": claim.Operation.FenceEpoch, "surface_digest": hex.EncodeToString(surface[:]),
				})
				if err != nil {
					t.Fatal(err)
				}
				return body
			}
			if tc.prepare != "" {
				if _, err := worker.Exec(ctx, `select public.`+tc.prepare+`($1::bytea)`, makeBody()); err != nil {
					t.Fatalf("prepare metadata before finish: %v", err)
				}
			}
			short, err := repo.RenewV2DeletionWork(ctx, *claim, 8*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			claim = &short
			body := makeBody()
			snapshot := func() string {
				t.Helper()
				var s string
				err := fixture.db.QueryRow(ctx, `select jsonb_build_object(
					'attachment',coalesce((select jsonb_agg(to_jsonb(a) order by attachment_id) from public.record_attachments a where record_id=$1),'[]'::jsonb),
					'upload',coalesce((select jsonb_agg(to_jsonb(u) order by upload_id) from public.attachment_uploads u where attachment_id='att_v2authority'),'[]'::jsonb),
					'export',coalesce((select jsonb_agg(to_jsonb(j) order by export_job_id) from public.record_export_jobs j where record_id=$1),'[]'::jsonb),
					'quota',(select to_jsonb(q) from public.attachment_quota_accounts q where project_id='default'),
					'attachment_preparation',(select to_jsonb(p) from public.attachment_purge_preparations p where operation_id=$2),
					'portability_preparation',(select to_jsonb(p) from public.record_portability_cleanup_preparations p where operation_id=$2),
					'attachment_targets',coalesce((select jsonb_agg(to_jsonb(x) order by blob_key) from public.attachment_purge_targets x where operation_id=$2),'[]'::jsonb),
					'portability_targets',coalesce((select jsonb_agg(to_jsonb(x) order by blob_key) from public.record_portability_cleanup_targets x where operation_id=$2),'[]'::jsonb),
					'obligations',coalesce((select jsonb_agg(to_jsonb(o) order by obligation_id) from public.record_write_obligations o where operation_id=$2 or record_id=$1),'[]'::jsonb),
					'attachment_receipts',coalesce((select jsonb_agg(to_jsonb(r) order by surface_kind) from public.attachment_purge_receipts r where operation_id=$2),'[]'::jsonb),
					'portability_receipts',coalesce((select jsonb_agg(to_jsonb(r)) from public.record_portability_purge_receipts r where operation_id=$2),'[]'::jsonb),
					'gc',coalesce((select jsonb_agg(to_jsonb(g) order by deletion_id) from public.blob_gc_deletions g where operation_id=$2),'[]'::jsonb))::text`, record.RecordID, claim.Operation.OperationID).Scan(&s)
				if err != nil {
					t.Fatal(err)
				}
				return s
			}
			before := snapshot()
			holder, err := fixture.db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = holder.Rollback(context.Background()) }()
			if tc.lock == "quota" {
				// A real accounting update holds the exact quota row until rollback.
				err = holder.QueryRow(ctx, `update public.attachment_quota_accounts set quota_version=quota_version+1 where project_id='default' returning logical_bytes,reserved_bytes,physical_bytes`).Scan(&logicalBytes, &reservedBytes, &physicalBytes)
				wantReserved := int64(0)
				if tc.attachment && tc.prepare == "" {
					wantReserved = 17
				}
				if err != nil || logicalBytes != 0 || reservedBytes != wantReserved || physicalBytes != 0 {
					t.Fatalf("held quota = %d/%d/%d, %v; want 0/%d/0", logicalBytes, reservedBytes, physicalBytes, err, wantReserved)
				}
			} else {
				var lockedRecord string
				err = holder.QueryRow(ctx, `select record_id from public.records where record_id=$1 for update`, record.RecordID).Scan(&lockedRecord)
				if err == nil && lockedRecord != record.RecordID {
					t.Fatalf("locked record = %q, want %q", lockedRecord, record.RecordID)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			tx, err := worker.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			var pid int
			if err := tx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				var output []byte
				result <- tx.QueryRow(ctx, `select public.`+tc.command+`($1::bytea)`, body).Scan(&output)
			}()
			received := false
			defer func() {
				cancel()
				_ = holder.Rollback(context.Background())
				if !received {
					<-result
				}
				_ = tx.Rollback(context.Background())
			}()
			// A real unrelated lock request supplies a bounded scheduling barrier;
			// the following single observation requires both a live deadline and
			// the worker actually waiting on the holder's PostgreSQL lock.
			var barrier time.Time
			if err := fixture.db.QueryRow(ctx, `select clock_timestamp()+interval '500 milliseconds'`).Scan(&barrier); err != nil {
				t.Fatal(err)
			}
			waitV2DeadlineInTransaction(t, ctx, fixture, holder, barrier)
			var blockedLive bool
			if err := fixture.db.QueryRow(ctx, `select clock_timestamp()<$2::timestamptz and exists
				(select 1 from pg_stat_activity a where a.pid=$1 and a.wait_event_type='Lock' and $3=any(pg_blocking_pids(a.pid)))`,
				pid, claim.Owner.ExpiresAt, int(holder.Conn().PgConn().PID())).Scan(&blockedLive); err != nil || !blockedLive {
				t.Fatalf("worker did not reach the held lock while owner was live: %t, %v", blockedLive, err)
			}
			waitV2DeadlineInTransaction(t, ctx, fixture, holder, claim.Owner.ExpiresAt)
			if err := holder.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			err = <-result
			received = true
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("expired %s = %v, want 42501", tc.command, err)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if after := snapshot(); before != after {
				t.Fatalf("expired command changed metadata, receipts, GC or quota\nbefore: %s\nafter:  %s", before, after)
			}
		})
	}
}
