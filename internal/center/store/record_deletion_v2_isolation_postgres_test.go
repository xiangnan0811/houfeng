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
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
	storemigrate "houfeng/internal/center/store/migrate"
)

func testV2LocalAdapterIsolation(t *testing.T) {
	for _, adapter := range []struct {
		name    string
		surface [32]byte
	}{
		{"attachment", recorddeletion.RecordAttachmentsSurfaceDigest()},
		{"portability", recorddeletion.RecordPortabilitySurfaceDigest()},
	} {
		for _, phase := range []string{"prepare", "complete"} {
			for _, isolation := range []pgx.TxIsoLevel{pgx.RepeatableRead, pgx.Serializable} {
				t.Run(adapter.name+"/"+phase+"/"+string(isolation), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
					defer cancel()
					fixture := newAdmittedCoreRecordFixture(t, ctx)
					runtime := fixture.openDirectRuntimePool(t, ctx, "v2-isolation-runtime", 2)
					workers := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v2-isolation-worker", 2)
					if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workers); err != nil {
						t.Fatal(err)
					}
					blob, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "blob-root"))
					if err != nil {
						t.Fatal(err)
					}
					record := seedAdmittedOutboxRecord(t, ctx, runtime, fixture.admissionGate, "v2-isolation-record")
					if _, err := fixture.db.Exec(ctx, `insert into public.attachment_quota_accounts(project_id) values ('default') on conflict (project_id) do nothing`); err != nil {
						t.Fatal(err)
					}
					expires := issueV2WorkerFixture(t, ctx, fixture, runtime, record.RecordID, "isolation")
					waitServingTestFenceExpired(t, ctx, fixture, expires)
					repository := NewPostgresRecordDeletionRepository(workers, allowRecordPlatformAdmissionGate)
					claim, err := repository.ClaimV2DeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_isolation", OwnerLeaseDuration: time.Minute})
					if err != nil || claim == nil {
						t.Fatalf("claim: %v, %v", claim, err)
					}
					body, err := json.Marshal(map[string]any{"operation_id": claim.Operation.OperationID, "operation_owner_id": claim.Owner.OwnerID, "operation_generation": claim.Owner.Generation, "operation_expires_at": claim.Owner.ExpiresAt, "namespace_id": blob.Namespace().NamespaceID, "incarnation_id": blob.Namespace().IncarnationID, "record_id": record.RecordID, "reservation_id": claim.Operation.ReservationID, "fence_epoch": claim.Operation.FenceEpoch, "surface_digest": hex.EncodeToString(adapter.surface[:])})
					if err != nil {
						t.Fatal(err)
					}
					prepare := `select public.local_` + adapter.name + `_purge_prepare($1::bytea)`
					command := `select public.local_` + adapter.name + `_purge_` + phase + `($1::bytea)`
					if phase == "complete" {
						if _, err := workers.Exec(ctx, prepare, body); err != nil {
							t.Fatalf("valid preparation: %v", err)
						}
					}
					snapshot := func() string {
						var value string
						if err := fixture.db.QueryRow(ctx, `select jsonb_build_array((select count(*) from public.attachment_purge_preparations),(select count(*) from public.record_portability_cleanup_preparations),(select count(*) from public.blob_gc_deletions),(select count(*) from public.attachment_purge_receipts),(select count(*) from public.record_portability_purge_receipts),(select row_to_json(q) from public.attachment_quota_accounts q where project_id='default'))::text`).Scan(&value); err != nil {
							t.Fatal(err)
						}
						return value
					}
					before := snapshot()
					tx, err := workers.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation})
					if err != nil {
						t.Fatal(err)
					}
					_, err = tx.Exec(ctx, command, body)
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "25000" {
						_ = tx.Rollback(ctx)
						t.Fatalf("%s under %s: %v, want 25000", command, isolation, err)
					}
					if err := tx.Rollback(ctx); err != nil {
						t.Fatal(err)
					}
					if after := snapshot(); after != before {
						t.Fatalf("rejected command changed state: before=%s after=%s", before, after)
					}
					if _, err := workers.Exec(ctx, command, body); err != nil {
						t.Fatalf("read committed control with same request: %v", err)
					}
				})
			}
		}
	}
	for _, command := range []string{"purge_claim", "purge_complete", "gc_prepare", "gc_claim", "gc_resolve", "gc_bind_final", "gc_complete"} {
		for _, isolation := range []pgx.TxIsoLevel{pgx.RepeatableRead, pgx.Serializable} {
			t.Run(command+"/"+string(isolation), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
				defer cancel()
				fixture := newAdmittedCoreRecordFixture(t, ctx)
				local, recordID, _, _, _, _, measured, _ := seedV2ProcessedWorkspace(t, ctx, fixture)
				written := seedV2ExportArtifact(t, ctx, fixture, local, recordID)
				workers := openV2LaunchBoundPool(t, ctx, fixture, fixture.deletionWorker, "v2-gc-isolation-worker", measured)
				if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workers); err != nil {
					t.Fatal(err)
				}
				var obligationRecord string
				var obligationEpoch, actualEpoch int64
				if err := fixture.db.QueryRow(ctx, `select o.record_id,o.record_epoch,e.delivery_epoch
					from public.record_write_obligations o join public.content_delivery_epochs e
					on e.project_id=o.project_id and e.object_kind='record' and e.object_id=o.record_id
					where o.obligation_id=$1 and o.origin_kind='export_artifact' and o.origin_id='rej_v2exclusive'`, written.ObligationID).Scan(&obligationRecord, &obligationEpoch, &actualEpoch); err != nil || obligationRecord != recordID || obligationEpoch != actualEpoch {
					t.Fatalf("export obligation record/epoch: record=%q epoch=%d actual=%d: %v", obligationRecord, obligationEpoch, actualEpoch, err)
				}
				expires := issueV2WorkerFixture(t, ctx, fixture, local.pool, recordID, "gcisolation")
				waitServingTestFenceExpired(t, ctx, fixture, expires)
				repository := NewPostgresRecordDeletionRepository(workers, allowRecordPlatformAdmissionGate)
				claim, err := repository.ClaimV2DeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_gc_isolation", OwnerLeaseDuration: 3 * time.Minute})
				if err != nil || claim == nil {
					t.Fatalf("claim: %v, %v", claim, err)
				}
				opID, opOwner, opGen, opExp := claim.Operation.OperationID, claim.Owner.OwnerID, int64(claim.Owner.Generation), claim.Owner.ExpiresAt
				op := attachments.LocalCleanupOperation{OperationID: &opID, OwnerID: &opOwner, Generation: &opGen, ExpiresAt: &opExp}
				observer := &v2IsolationCleanupRepository{
					PostgresLocalBlobRepository: NewPostgresLocalBlobRepository(workers, nil, nil),
					t:                           t, db: fixture.db, isolation: isolation, command: command, obligationID: written.ObligationID, version: written.Version.VersionID,
				}
				coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, observer)
				if err != nil {
					t.Fatal(err)
				}
				ns := local.blob.Namespace()
				prep := attachments.LocalCleanupPreparation{LocalCleanupOperation: op, DeletionID: "bgd_v2isolation", NamespaceID: ns.NamespaceID, IncarnationID: ns.IncarnationID, Key: written.Version.Key}
				result, err := coordinator.Cleanup(ctx, prep, "v2_gc_cleanup")
				if err != nil {
					t.Fatalf("guarded cleanup: %v", err)
				}
				if !observer.checked || result.State != "completed" || result.Outcome == nil || *result.Outcome != "retained_referenced" {
					t.Fatalf("isolation case not exercised or export not retained: checked=%t task=%+v", observer.checked, result)
				}
			})
		}
	}
}

// The coordinator supplies every request while holding its real key guard.
// The rejected transaction and the subsequent read-committed call use the
// same serialized request, at the same point in the physical cleanup sequence.
type v2IsolationCleanupRepository struct {
	*PostgresLocalBlobRepository
	t            *testing.T
	db           *pgxpool.Pool
	isolation    pgx.TxIsoLevel
	command      string
	obligationID string
	version      string
	claim        attachments.LocalCleanupClaim
	checked      bool
}

func (r *v2IsolationCleanupRepository) reject(ctx context.Context, statement string, body []byte) {
	r.t.Helper()
	snapshot := func() string {
		var state string
		err := r.db.QueryRow(ctx, `select jsonb_build_array(
			(select coalesce(jsonb_agg(to_jsonb(g) order by deletion_id),'[]'::jsonb) from public.blob_gc_deletions g),
			(select coalesce(jsonb_agg(to_jsonb(l) order by deletion_id,obligation_id),'[]'::jsonb) from public.local_blob_gc_obligations l),
			(select coalesce(jsonb_agg(to_jsonb(w) order by obligation_id),'[]'::jsonb) from public.record_write_obligations w),
			(select coalesce(jsonb_agg(to_jsonb(b) order by blob_key,object_version),'[]'::jsonb) from public.blob_objects b),
			(select coalesce(jsonb_agg(to_jsonb(a) order by export_artifact_id),'[]'::jsonb) from public.record_export_artifacts a),
			(select coalesce(jsonb_agg(to_jsonb(o) order by operation_id),'[]'::jsonb) from public.record_purge_operations o),
			(select coalesce(jsonb_agg(to_jsonb(f) order by project_id,object_kind,object_id),'[]'::jsonb) from public.deletion_fence_leases f),
			(select row_to_json(q) from public.attachment_quota_accounts q where project_id='default'))::text`).Scan(&state)
		if err != nil {
			r.t.Fatal(err)
		}
		return state
	}
	before := snapshot()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: r.isolation})
	if err != nil {
		r.t.Fatal(err)
	}
	_, err = tx.Exec(ctx, statement, body)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25000" {
		_ = tx.Rollback(ctx)
		r.t.Fatalf("%s under %s: %v, want 25000", r.command, r.isolation, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		r.t.Fatal(err)
	}
	if after := snapshot(); before != after {
		r.t.Fatalf("rejected %s changed durable state: before=%s after=%s", r.command, before, after)
	}
	r.checked = true
}

func (r *v2IsolationCleanupRepository) rejectRequest(ctx context.Context, name, statement string, request any) {
	if r.command != name {
		return
	}
	body, err := json.Marshal(request)
	if err != nil {
		r.t.Fatal(err)
	}
	r.reject(ctx, statement, body)
}

func (r *v2IsolationCleanupRepository) PrepareLocalCleanup(ctx context.Context, request attachments.LocalCleanupPreparation) (attachments.LocalCleanupTask, error) {
	r.rejectRequest(ctx, "gc_prepare", `select (public.local_blob_purge_prepare($1)).deletion_id`, request)
	result, err := r.PostgresLocalBlobRepository.PrepareLocalCleanup(ctx, request)
	if err != nil && r.command == "gc_prepare" {
		r.t.Fatalf("read committed gc_prepare control: %v", err)
	}
	return result, err
}

func (r *v2IsolationCleanupRepository) ClaimLocalCleanup(ctx context.Context, request attachments.LocalCleanupClaim) (attachments.LocalCleanupSnapshot, error) {
	r.claim = request
	r.rejectRequest(ctx, "gc_claim", `select public.local_blob_purge_task_claim($1)`, request)
	result, err := r.PostgresLocalBlobRepository.ClaimLocalCleanup(ctx, request)
	if err != nil && r.command == "gc_claim" {
		r.t.Fatalf("read committed gc_claim control: %v", err)
	}
	return result, err
}

func (r *v2IsolationCleanupRepository) BindLocalCleanupFinal(ctx context.Context, request attachments.LocalCleanupFinalBinding) (attachments.LocalCleanupSnapshot, error) {
	if request.Version == nil || *request.Version != r.version || request.Inode == 0 {
		r.t.Fatalf("coordinator final identity does not match the written object: %+v", request)
	}
	r.rejectRequest(ctx, "gc_bind_final", `select public.local_blob_purge_bind_final($1)`, request)
	result, err := r.PostgresLocalBlobRepository.BindLocalCleanupFinal(ctx, request)
	if err != nil && r.command == "gc_bind_final" {
		r.t.Fatalf("read committed gc_bind_final control: %v", err)
	}
	if err == nil && r.command == "gc_resolve" {
		if !result.Task.FinalIdentityBound || result.Task.FinalWasPresent == nil || !*result.Task.FinalWasPresent || result.Task.Version == nil || *result.Task.Version != r.version || result.Task.FinalDevice == nil || *result.Task.FinalDevice != request.Device || result.Task.FinalInode == nil || *result.Task.FinalInode != request.Inode || result.Task.InventoryDigest == nil || *result.Task.InventoryDigest != request.InventoryDigest {
			r.t.Fatalf("gc_resolve missing bound final identity: %+v", result.Task)
		}
		identity := request.LocalCleanupIdentity
		r.rejectRequest(ctx, "gc_resolve", `select public.local_blob_purge_task_resolve($1)`, identity)
		resolved, resolveErr := r.PostgresLocalBlobRepository.ResolveLocalCleanup(ctx, identity)
		if resolveErr != nil || !resolved.Task.FinalIdentityBound || resolved.Task.FinalInode == nil || *resolved.Task.FinalInode != request.Inode || resolved.Task.InventoryDigest == nil || *resolved.Task.InventoryDigest != request.InventoryDigest {
			r.t.Fatalf("read committed bound gc_resolve control: %+v, %v", resolved.Task, resolveErr)
		}
	}
	if err == nil && (r.command == "purge_claim" || r.command == "purge_complete") {
		r.testObligationCleanup(ctx, request, result)
	}
	return result, err
}

func (r *v2IsolationCleanupRepository) CompleteLocalCleanup(ctx context.Context, request attachments.LocalCleanupCompletion) (attachments.LocalCleanupSnapshot, error) {
	r.rejectRequest(ctx, "gc_complete", `select public.local_blob_purge_task_complete($1)`, request)
	result, err := r.PostgresLocalBlobRepository.CompleteLocalCleanup(ctx, request)
	if err != nil && r.command == "gc_complete" {
		r.t.Fatalf("read committed gc_complete control: %v", err)
	}
	return result, err
}

func (r *v2IsolationCleanupRepository) testObligationCleanup(ctx context.Context, binding attachments.LocalCleanupFinalBinding, snapshot attachments.LocalCleanupSnapshot) {
	r.t.Helper()
	if !snapshot.Referenced || binding.Version == nil || binding.Inode == 0 || snapshot.Task.InventoryDigest == nil || *snapshot.Task.InventoryDigest != binding.InventoryDigest {
		r.t.Fatalf("coordinator did not bind the real referenced final and inventory: binding=%+v task=%+v", binding, snapshot.Task)
	}
	var generation int64
	var expiry *time.Time
	if err := r.db.QueryRow(ctx, `select cleanup_generation,cleanup_expires_at from public.record_write_obligations where obligation_id=$1`, r.obligationID).Scan(&generation, &expiry); err != nil {
		r.t.Fatal(err)
	}
	const obligationOwner = "v2_obligation_cleanup"
	const obligationDispatch = "d0000000-0000-4000-8000-000000000010"
	op := binding.LocalCleanupOperation
	claim := map[string]any{"obligation_id": r.obligationID, "namespace_id": r.claim.NamespaceID, "incarnation_id": r.claim.IncarnationID,
		"anchor_inode": r.claim.AnchorInode, "anchor_nonce": r.claim.AnchorNonce, "cleanup_owner_id": obligationOwner,
		"cleanup_generation": generation, "cleanup_expires_at": expiry, "cleanup_dispatch_id": obligationDispatch,
		"operation_id": op.OperationID, "operation_owner_id": op.OwnerID, "operation_generation": op.Generation, "operation_expires_at": op.ExpiresAt}
	body, err := json.Marshal(claim)
	if err != nil {
		r.t.Fatal(err)
	}
	if r.command == "purge_claim" {
		r.reject(ctx, `select count(*) from public.local_blob_purge_claim($1)`, body)
	}
	if err := r.pool.QueryRow(ctx, `select cleanup_generation,cleanup_expires_at from public.local_blob_purge_claim($1) where obligation_id=$2`, body, r.obligationID).Scan(&generation, &expiry); err != nil {
		r.t.Fatalf("read committed obligation claim: %v", err)
	}
	if r.command != "purge_complete" {
		return
	}
	completion, err := json.Marshal(map[string]any{"obligation_id": r.obligationID, "cleanup_owner_id": obligationOwner,
		"cleanup_generation": generation, "cleanup_expires_at": expiry, "cleanup_dispatch_id": obligationDispatch,
		"outcome": "retained_referenced", "inventory_digest": binding.InventoryDigest,
		"operation_id": op.OperationID, "operation_owner_id": op.OwnerID, "operation_generation": op.Generation, "operation_expires_at": op.ExpiresAt})
	if err != nil {
		r.t.Fatal(err)
	}
	r.reject(ctx, `select public.local_blob_purge_complete($1)`, completion)
	var completed int64
	if err := r.pool.QueryRow(ctx, `select public.local_blob_purge_complete($1)`, completion).Scan(&completed); err != nil || completed < 1 {
		r.t.Fatalf("read committed obligation completion: count=%d error=%v", completed, err)
	}
}
