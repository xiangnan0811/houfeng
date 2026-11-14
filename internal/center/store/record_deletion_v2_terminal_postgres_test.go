package store

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
)

func testV2TerminalTakeover(t *testing.T, factory func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error), commit bool) {
	const snapshotID = "evs_v2terminal"
	var legacyCompletion []byte
	var legacyInventory string
	f := setupV2FinishFixtureWithHooks(t, factory, nil, func(ctx context.Context, fixture recordPlatformPostgresFixture, recordID string) {
		snapshot := storeEvidenceSnapshotFixture(t, "terminal evidence payload")
		seedEvidencePayloadAt(t, ctx, fixture, snapshot, time.Now())
		envelope := snapshot.Envelope()
		encoded := make([][]byte, 10)
		for i, value := range []any{envelope.Subject, envelope.Source, envelope.Authorization, envelope.ActualPrecision, envelope.BucketWidth, envelope.Units, envelope.Quality, envelope.QuotaOutcome, envelope.Retention, envelope.Redaction} {
			var err error
			encoded[i], err = marshalEvidenceParticipantJSON(value)
			if err != nil {
				t.Fatal(err)
			}
		}
		digest := snapshot.Hash()
		if _, err := fixture.db.Exec(ctx, `insert into public.evidence_snapshots (
			snapshot_id,record_id,kind,schema_version,source_kind,source_id,subject_identity_snapshot,source_identity_snapshot,capture_authorization,capture_authorization_digest,
			requested_started_at,requested_ended_at,actual_started_at,actual_ended_at,observed_at,captured_at,referenced_at,source_revision,source_watermark,source_digest,producer_version,calculation_version,
			actual_precision,bucket_width,unit_semantics,quality,quota_outcome,retention,sensitivity_level,redaction,canonical_hash,logical_size_bytes,payload_digest
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$31)`,
			snapshotID, recordID, string(envelope.Key.Kind), int64(envelope.Key.SchemaVersion), envelope.Source.Type, envelope.Source.ID,
			encoded[0], encoded[1], encoded[2], envelope.Authorization.Digest[:], envelope.RequestedWindow.Start, envelope.RequestedWindow.End,
			envelope.ActualWindow.Start, envelope.ActualWindow.End, envelope.ObservedAt, envelope.CapturedAt, envelope.ReferencedAt,
			envelope.SourceRevision, envelope.SourceWatermark, envelope.SourceDigest[:], envelope.ProducerVersion, envelope.CalculationVersion,
			encoded[3], encoded[4], encoded[5], encoded[6], encoded[7], encoded[8], string(envelope.Sensitivity), encoded[9], digest[:], int64(snapshot.Size())); err != nil {
			t.Fatalf("seed terminal evidence snapshot: %v", err)
		}
	}, func(ctx context.Context, fixture recordPlatformPostgresFixture, local *localPublicationFixture, workers *pgxpool.Pool, claim *recorddeletion.ClaimedDeletionWork, export attachments.WrittenBlob) {
		var anchorInode int64
		var anchorNonce string
		ns := local.blob.Namespace()
		if err := fixture.db.QueryRow(ctx, `select lock_inode,lock_nonce::text from public.local_blob_key_anchors where namespace_id=$1 and incarnation_id=$2 and canonical_key=$3`, ns.NamespaceID, ns.IncarnationID, export.Version.Key).Scan(&anchorInode, &anchorNonce); err != nil {
			t.Fatal(err)
		}
		dispatch := localBlobFixtureUUID(t)
		body, err := json.Marshal(map[string]any{"obligation_id": export.ObligationID, "namespace_id": ns.NamespaceID, "incarnation_id": ns.IncarnationID, "anchor_inode": anchorInode, "anchor_nonce": anchorNonce, "cleanup_owner_id": claim.Owner.OwnerID, "cleanup_generation": 0, "cleanup_expires_at": nil, "cleanup_dispatch_id": dispatch, "operation_id": claim.Operation.OperationID, "operation_owner_id": claim.Owner.OwnerID, "operation_generation": claim.Owner.Generation, "operation_expires_at": claim.Owner.ExpiresAt})
		if err != nil {
			t.Fatal(err)
		}
		if err := local.blob.WithKeyGuard(ctx, export.Version.Key, func(ctx context.Context, _ *attachments.LocalKeyGuard) error {
			type inventoryTarget struct{ ID, Temporary string }
			rows, err := fixture.db.Query(ctx, `select obligation_id,temporary_key from public.record_write_obligations where namespace_id=$1 and incarnation_id=$2 and canonical_key=$3 order by obligation_id`, ns.NamespaceID, ns.IncarnationID, export.Version.Key)
			if err != nil {
				return err
			}
			var targets []inventoryTarget
			for rows.Next() {
				var target inventoryTarget
				if err := rows.Scan(&target.ID, &target.Temporary); err != nil {
					rows.Close()
					return err
				}
				targets = append(targets, target)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if len(targets) != 1 || targets[0].ID != export.ObligationID || targets[0].Temporary != "sha256/.blob-tmp-"+export.ObligationID {
				return errors.New("unexpected export cleanup inventory")
			}
			inventory, err := json.Marshal(struct {
				Namespace, Incarnation, Key string
				Targets                     []inventoryTarget
			}{ns.NamespaceID, ns.IncarnationID, export.Version.Key, targets})
			if err != nil {
				return err
			}
			digest := sha256.Sum256(inventory)
			legacyInventory = hex.EncodeToString(digest[:])
			var generation int64
			var expiry time.Time
			if err := workers.QueryRow(ctx, `select cleanup_generation,cleanup_expires_at from public.local_blob_purge_claim($1) where obligation_id=$2`, body, export.ObligationID).Scan(&generation, &expiry); err != nil {
				return err
			}
			legacyCompletion, err = json.Marshal(map[string]any{"obligation_id": export.ObligationID, "cleanup_owner_id": claim.Owner.OwnerID, "cleanup_generation": generation, "cleanup_expires_at": expiry, "cleanup_dispatch_id": dispatch, "outcome": "retained_referenced", "inventory_digest": legacyInventory, "operation_id": claim.Operation.OperationID, "operation_owner_id": claim.Owner.OwnerID, "operation_generation": claim.Owner.Generation, "operation_expires_at": claim.Owner.ExpiresAt})
			if err != nil {
				return err
			}
			var completed int64
			if err := workers.QueryRow(ctx, `select public.local_blob_purge_complete($1)`, legacyCompletion).Scan(&completed); err != nil {
				return err
			} else if completed != 1 {
				return errors.New("legacy cleanup did not settle referenced obligation")
			}
			if err := workers.QueryRow(ctx, `select public.local_blob_purge_complete($1)`, legacyCompletion).Scan(&completed); err != nil {
				return err
			} else if completed != 0 {
				return errors.New("legacy cleanup exact replay changed state")
			}
			return nil
		}); err != nil {
			t.Fatalf("claim genuine per-obligation cleanup: %v", err)
		}
	})
	ctx := f.ctx
	short := *f.claim
	// Snapshot adapter effects committed before the finish transaction.
	durableSnapshot := func() string {
		t.Helper()
		var result string
		if err := f.fixture.db.QueryRow(ctx, `select jsonb_build_array(
			(select to_jsonb(q) from public.attachment_quota_accounts q where project_id='default'),
			(select to_jsonb(r) from public.record_core_purge_receipts r where operation_id=$1),
			(select jsonb_agg(to_jsonb(r) order by r.surface_kind, r.object_version_digest) from public.attachment_purge_receipts r where operation_id=$1),
			(select to_jsonb(r) from public.evidence_purge_receipts r where operation_id=$1),
			(select to_jsonb(r) from public.record_markdown_client_purge_receipts r where operation_id=$1),
			(select to_jsonb(r) from public.record_search_purge_receipts r where operation_id=$1),
			(select to_jsonb(r) from public.record_activity_purge_receipts r where operation_id=$1),
			(select to_jsonb(r) from public.record_comparison_purge_receipts r where operation_id=$1),
			(select to_jsonb(r) from public.record_collaboration_purge_receipts r where operation_id=$1),
			(select to_jsonb(r) from public.record_portability_purge_receipts r where operation_id=$1),
			(select to_jsonb(w) from public.record_write_obligations w where obligation_id=$2),
			(select jsonb_agg(to_jsonb(g) order by g.deletion_id) from public.blob_gc_deletions g where operation_id=$1),
			exists(select 1 from public.records where record_id=$3),
			exists(select 1 from public.content_delivery_epochs where object_kind='record' and object_id=$3))::text`, short.Operation.OperationID, f.export.ObligationID, short.Operation.Object.ObjectID).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	beforeFinish := durableSnapshot()
	owner, err := deletionV2Owner(short)
	if err != nil {
		t.Fatal(err)
	}
	finishBody, err := encodeRecordPurgeCommand(struct {
		deletionV2OwnerEnvelope
		ReceiptDigest string `json:"receipt_digest"`
	}{owner, hex.EncodeToString(f.receipt.ReceiptDigest[:])})
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.workerPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Rollback(ctx) }()
	var aPID int
	if err := a.QueryRow(ctx, `select pg_backend_pid()`).Scan(&aPID); err != nil {
		t.Fatal(err)
	}
	var finished bool
	if err := a.QueryRow(ctx, `select public.record_deletion_finish_v2($1)`, finishBody).Scan(&finished); err != nil || !finished {
		t.Fatalf("finish: %t, %v", finished, err)
	}
	// The completed operation may be read back even in its terminal transaction.
	if err := a.QueryRow(ctx, `select public.record_deletion_finish_v2($1)`, finishBody).Scan(&finished); err != nil || !finished {
		t.Fatalf("terminal readback: %t, %v", finished, err)
	}
	target := recorddeletion.PurgeTarget{Operation: short.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: short.Operation.OperationID, Owner: short.Owner}}
	expectSavepointCode := func(sql string, arg []byte, want string) {
		if _, err := a.Exec(ctx, `savepoint after_finish`); err != nil {
			t.Fatal(err)
		}
		_, err = a.Exec(ctx, sql, arg)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != want {
			t.Fatalf("post-finish %s = %v, want %s", sql, err, want)
		}
		if _, err := a.Exec(ctx, `rollback to savepoint after_finish`); err != nil {
			t.Fatal(err)
		}
	}
	bogusFinish, err := encodeRecordPurgeCommand(struct {
		deletionV2OwnerEnvelope
		ReceiptDigest string `json:"receipt_digest"`
	}{owner, hex.EncodeToString(f.bogus.ReceiptDigest[:])})
	if err != nil {
		t.Fatal(err)
	}
	expectSavepointCode(`select public.record_deletion_finish_v2($1)`, bogusFinish, "42501")
	renewBody, err := encodeRecordPurgeCommand(struct {
		deletionV2OwnerEnvelope
		OwnerLeaseMicroseconds int64 `json:"owner_lease_microseconds"`
	}{owner, int64(time.Minute / time.Microsecond)})
	if err != nil {
		t.Fatal(err)
	}
	expectSavepointCode(`select * from public.record_deletion_renew_v2($1)`, renewBody, "42501")
	claimReq, err := encodeRecordPurgeCommand(struct {
		OwnerID                string `json:"owner_id"`
		OwnerLeaseMicroseconds int64  `json:"owner_lease_microseconds"`
	}{"v2_same_tx", int64(time.Minute / time.Microsecond)})
	if err != nil {
		t.Fatal(err)
	}
	if sameClaim, err := scanV2DeletionWork(a.QueryRow(ctx, `select * from public.record_deletion_claim_v2($1)`, claimReq), "v2_same_tx"); err != nil || sameClaim != nil {
		t.Fatalf("post-finish claim = %v, %v", sameClaim, err)
	}
	secondToken := sha256.Sum256([]byte("v2 fixture second token finish"))
	digest := strings.Repeat("12", 32)
	issueBody, err := json.Marshal(map[string]any{"token_commitment": hex.EncodeToString(secondToken[:]), "actor_id": "usr_aaaaaaaaaaaaaaaaaaaaaaaa", "actor_scope_digest": digest, "record_id": short.Operation.Object.ObjectID, "binding_digest": digest, "authorization_evidence": "12", "request_fingerprint": digest, "reservation_id": "drs_v2workerfinish2", "operation_id": "rpo_v2workerfinish2", "owner_id": "v2_issuer", "confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	expectSavepointCode(`select public.record_deletion_issue($1::bytea)`, issueBody, "42501")
	for _, adapter := range []struct {
		name    string
		surface [32]byte
	}{
		{"record_core_purge_v2", recorddeletion.RecordCoreSurfaceDigest()}, {"record_core_purge_verify_v2", recorddeletion.RecordCoreSurfaceDigest()},
		{"record_evidence_purge_v2", recorddeletion.RecordEvidenceSurfaceDigest()}, {"record_evidence_purge_verify_v2", recorddeletion.RecordEvidenceSurfaceDigest()},
		{"record_markdown_client_purge", recorddeletion.RecordMarkdownClientSurfaceDigest()}, {"record_markdown_client_purge_verify", recorddeletion.RecordMarkdownClientSurfaceDigest()},
		{"record_search_purge_v2", recorddeletion.RecordSearchSurfaceDigest()}, {"record_search_purge_verify_v2", recorddeletion.RecordSearchSurfaceDigest()},
		{"record_activity_purge_v2", recorddeletion.RecordActivitySurfaceDigest()}, {"record_activity_purge_verify_v2", recorddeletion.RecordActivitySurfaceDigest()},
		{"record_comparison_purge", recorddeletion.RecordComparisonSurfaceDigest()}, {"record_comparison_purge_verify", recorddeletion.RecordComparisonSurfaceDigest()},
		{"record_collaboration_purge_v2", recorddeletion.RecordCollaborationSurfaceDigest()}, {"record_collaboration_purge_verify_v2", recorddeletion.RecordCollaborationSurfaceDigest()},
	} {
		body, err := deletionAdapterEnvelope(ctx, a, target, adapter.surface)
		if err != nil {
			t.Fatal(err)
		}
		expectSavepointCode(`select * from public.`+adapter.name+`($1)`, body, "42501")
		if adapter.name == "record_evidence_purge_v2" {
			expectSavepointCode(`select * from public.record_evidence_purge_prepare_v2($1)`, body, "42501")
			payloadBody, err := json.Marshal(map[string]any{"authority": base64.StdEncoding.EncodeToString(body), "snapshot_id": snapshotID})
			if err != nil {
				t.Fatal(err)
			}
			expectSavepointCode(`select public.record_evidence_purge_payload_v2($1::bytea)`, payloadBody, "42501")
		}
	}
	ns := f.local.blob.Namespace()
	for _, localAdapter := range []struct {
		name    string
		surface [32]byte
	}{
		{"attachment", recorddeletion.RecordAttachmentsSurfaceDigest()}, {"portability", recorddeletion.RecordPortabilitySurfaceDigest()},
	} {
		body, err := json.Marshal(map[string]any{"operation_id": short.Operation.OperationID, "operation_owner_id": short.Owner.OwnerID, "operation_generation": short.Owner.Generation, "operation_expires_at": short.Owner.ExpiresAt, "namespace_id": ns.NamespaceID, "incarnation_id": ns.IncarnationID, "record_id": short.Operation.Object.ObjectID, "reservation_id": short.Operation.ReservationID, "fence_epoch": short.Operation.FenceEpoch, "surface_digest": hex.EncodeToString(localAdapter.surface[:])})
		if err != nil {
			t.Fatal(err)
		}
		for _, phase := range []string{"prepare", "complete", "verify"} {
			expectSavepointCode(`select public.local_`+localAdapter.name+`_purge_`+phase+`($1::bytea)`, body, "42501")
		}
	}
	var anchorInode, cleanupGen int64
	var anchorNonce, deletionID, cleanupOwner, cleanupDispatch, gcOwner, gcDispatch string
	var cleanupExp, gcExp time.Time
	var gcGen int64
	var gcInv, gcCompletion []byte
	var gcVersion *string
	var gcDevice, gcInode *int64
	var gcOutcome string
	if err := f.fixture.db.QueryRow(ctx, `select a.lock_inode,a.lock_nonce::text,w.cleanup_owner_id,w.cleanup_generation,w.cleanup_expires_at,w.cleanup_dispatch_id::text,g.deletion_id,g.owner_id,g.owner_generation,g.lease_expires_at,g.cleanup_dispatch_id::text,g.inventory_digest,g.object_version,g.final_device,g.final_inode,g.completion_command,g.physical_delete_result from public.local_blob_key_anchors a join public.record_write_obligations w on w.namespace_id=a.namespace_id and w.incarnation_id=a.incarnation_id and w.canonical_key=a.canonical_key join public.local_blob_gc_obligations link on link.obligation_id=w.obligation_id join public.blob_gc_deletions g on g.deletion_id=link.deletion_id where w.obligation_id=$1 and not link.seal_obligation and g.deletion_state='completed' and g.final_identity_bound`, f.export.ObligationID).Scan(&anchorInode, &anchorNonce, &cleanupOwner, &cleanupGen, &cleanupExp, &cleanupDispatch, &deletionID, &gcOwner, &gcGen, &gcExp, &gcDispatch, &gcInv, &gcVersion, &gcDevice, &gcInode, &gcCompletion, &gcOutcome); err != nil {
		t.Fatal(err)
	}
	if legacyInventory != hex.EncodeToString(gcInv) || gcOutcome != "deleted" {
		t.Fatal("persisted GC inventory or outcome differs from actual legacy cleanup target")
	}
	opID, opOwner, opGen, opExp := short.Operation.OperationID, short.Owner.OwnerID, int64(short.Owner.Generation), short.Owner.ExpiresAt
	op := attachments.LocalCleanupOperation{OperationID: &opID, OwnerID: &opOwner, Generation: &opGen, ExpiresAt: &opExp}
	purgeClaim, err := json.Marshal(map[string]any{"obligation_id": f.export.ObligationID, "namespace_id": ns.NamespaceID, "incarnation_id": ns.IncarnationID, "anchor_inode": anchorInode, "anchor_nonce": anchorNonce, "cleanup_owner_id": cleanupOwner, "cleanup_generation": cleanupGen, "cleanup_expires_at": cleanupExp, "cleanup_dispatch_id": cleanupDispatch, "operation_id": opID, "operation_owner_id": opOwner, "operation_generation": opGen, "operation_expires_at": opExp})
	if err != nil {
		t.Fatal(err)
	}
	expectSavepointCode(`select count(*) from public.local_blob_purge_claim($1)`, purgeClaim, "42501")
	if len(legacyCompletion) == 0 {
		t.Fatal("missing successful legacy completion command")
	}
	expectSavepointCode(`select public.local_blob_purge_complete($1)`, legacyCompletion, "42501")
	gcIdentity := attachments.LocalCleanupIdentity{LocalCleanupOperation: op, DeletionID: deletionID, NamespaceID: ns.NamespaceID, IncarnationID: ns.IncarnationID, OwnerID: gcOwner, DispatchID: gcDispatch}
	gcCurrent := attachments.LocalCleanupCurrent{LocalCleanupIdentity: gcIdentity, Generation: gcGen, ExpiresAt: &gcExp}
	var finalDevice, finalInode uint64
	if gcVersion != nil {
		if gcDevice == nil || gcInode == nil || *gcDevice < 0 || *gcInode <= 0 {
			t.Fatal("persisted final identity is incomplete")
		}
		finalDevice, finalInode = uint64(*gcDevice), uint64(*gcInode)
	} else if gcDevice != nil || gcInode != nil {
		t.Fatal("absent final has persisted physical identity")
	}
	for _, gcCall := range []struct {
		sql string
		req any
	}{
		{`select (public.local_blob_purge_prepare($1)).deletion_id`, attachments.LocalCleanupPreparation{LocalCleanupOperation: op, DeletionID: deletionID, NamespaceID: ns.NamespaceID, IncarnationID: ns.IncarnationID, Key: f.export.Version.Key}},
		{`select public.local_blob_purge_task_claim($1)`, attachments.LocalCleanupClaim{LocalCleanupIdentity: gcIdentity, AnchorInode: uint64(anchorInode), AnchorNonce: anchorNonce, Generation: gcGen, ExpiresAt: &gcExp}},
		{`select public.local_blob_purge_task_resolve($1)`, gcIdentity},
		{`select public.local_blob_purge_bind_final($1)`, attachments.LocalCleanupFinalBinding{LocalCleanupCurrent: gcCurrent, Version: gcVersion, Device: finalDevice, Inode: finalInode, InventoryDigest: hex.EncodeToString(gcInv)}},
	} {
		raw, err := json.Marshal(gcCall.req)
		if err != nil {
			t.Fatal(err)
		}
		expectSavepointCode(gcCall.sql, raw, "42501")
	}
	if len(gcCompletion) == 0 {
		t.Fatal("completed GC task has no persisted completion command")
	}
	expectSavepointCode(`select public.local_blob_purge_task_complete($1)`, gcCompletion, "42501")
	outboxBody, err := deletionAdapterEnvelope(ctx, a, target, recorddeletion.RecordCollaborationSurfaceDigest())
	if err != nil {
		t.Fatal(err)
	}
	expectSavepointCode(`select * from public.record_outbox_drain_v2($1::bytea)`, outboxBody, "42501")
	// The exact production terminal readback observes A's own uncommitted effects.
	if err := a.QueryRow(ctx, `select public.record_deletion_finish_v2($1)`, finishBody).Scan(&finished); err != nil || !finished {
		t.Fatalf("same-transaction terminal readback after rejections: %t, %v", finished, err)
	}
	var committedOwner bool
	if err := f.fixture.db.QueryRow(ctx, `select o.operation_state='online_purging' and o.owner_id=$2 and o.owner_generation=$3 and o.owner_expires_at=$4 and r.state='fenced' and exists(select 1 from public.deletion_fence_leases l where l.project_id=o.project_id and l.object_kind='record' and l.object_id=r.object_id) from public.record_purge_operations o join public.deletion_reservations r on r.reservation_id=o.reservation_id where o.operation_id=$1`, opID, opOwner, opGen, opExp).Scan(&committedOwner); err != nil || !committedOwner {
		t.Fatalf("committed owner while finish is uncommitted: %t, %v", committedOwner, err)
	}
	var ownerLive bool
	if err := f.fixture.db.QueryRow(ctx, `select clock_timestamp() < $1::timestamptz and clock_timestamp() < $2::timestamptz and clock_timestamp() < $3::timestamptz`, opExp, cleanupExp, gcExp).Scan(&ownerLive); err != nil || !ownerLive {
		t.Fatalf("operation or cleanup authority expired during terminal-state rejection assertions: %t, %v", ownerLive, err)
	}
	for _, pool := range []*pgxpool.Pool{f.runtime, f.workerPool} {
		_, err := pool.Exec(ctx, `insert into public.deletion_fence_leases(project_id,object_kind,object_id,owner_id,owner_generation,created_at,expires_at) values('default','record',$1,'direct_dml',1,clock_timestamp()-interval '1 minute',clock_timestamp()+interval '1 minute') on conflict(project_id,object_kind,object_id) do update set owner_id=excluded.owner_id`, short.Operation.Object.ObjectID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("direct fence upsert = %v, want 42501", err)
		}
	}
	v1Body, err := json.Marshal(map[string]any{"operation_id": opID, "reservation_id": short.Operation.ReservationID, "project_id": "default", "record_id": short.Operation.Object.ObjectID, "fence_epoch": short.Operation.FenceEpoch, "owner_id": opOwner, "owner_generation": opGen, "owner_expires_at": opExp.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	waitV2DeadlineInTransaction(t, ctx, f.fixture, a, short.Owner.ExpiresAt)
	b, err := f.workerPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Rollback(ctx) }()
	var bPID int
	if err := b.QueryRow(ctx, `select pg_backend_pid()`).Scan(&bPID); err != nil {
		t.Fatal(err)
	}
	request, err := encodeRecordPurgeCommand(struct {
		OwnerID                string `json:"owner_id"`
		OwnerLeaseMicroseconds int64  `json:"owner_lease_microseconds"`
	}{"v2_terminal_b", int64(time.Minute / time.Microsecond)})
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		claim *recorddeletion.ClaimedDeletionWork
		err   error
	}
	done := make(chan result, 1)
	go func() {
		claim, err := scanV2DeletionWork(b.QueryRow(ctx, `select * from public.record_deletion_claim_v2($1)`, request), "v2_terminal_b")
		if err == nil {
			err = b.Commit(ctx)
		} else {
			_ = b.Rollback(ctx)
		}
		done <- result{claim, err}
	}()
	waitForPostgresBlockingPID(t, ctx, f.fixture.db, bPID, aPID)
	if commit {
		var previewValid bool
		if err := f.fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_deletion_previews where deletion_token_commitment=$1 and record_id=$2 and actor_id='usr_aaaaaaaaaaaaaaaaaaaaaaaa' and operation_id is null and expires_at>clock_timestamp())`, secondToken[:], short.Operation.Object.ObjectID).Scan(&previewValid); err != nil || !previewValid {
			t.Fatalf("independent runtime issuer preview unavailable: %t, %v", previewValid, err)
		}
		var rawIssueErr *pgconn.PgError
		if _, err := f.runtime.Exec(ctx, `select public.record_deletion_issue($1::bytea)`, issueBody); !errors.As(err, &rawIssueErr) || rawIssueErr.Code != "55P03" {
			t.Fatalf("unprefixed runtime issue while finish holds reservation lock = %v, want 55P03", err)
		}
		issueTx, err := f.runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = issueTx.Rollback(ctx) }()
		renewTx, err := f.workerPool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = renewTx.Rollback(ctx) }()
		v1Tx, err := f.workerPool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = v1Tx.Rollback(ctx) }()
		var issuePID, renewPID, v1PID int
		if err := issueTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&issuePID); err != nil {
			t.Fatal(err)
		}
		if err := renewTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&renewPID); err != nil {
			t.Fatal(err)
		}
		if err := v1Tx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&v1PID); err != nil {
			t.Fatal(err)
		}
		issueDone, renewDone, v1Done := make(chan error, 1), make(chan error, 1), make(chan error, 1)
		go func() {
			if err := lockComparisonDecisionForWriter(ctx, issueTx); err != nil {
				issueDone <- err
				return
			}
			_, err := issueTx.Exec(ctx, `select public.record_deletion_issue($1::bytea)`, issueBody)
			issueDone <- err
		}()
		go func() {
			_, err := renewTx.Exec(ctx, `select * from public.record_deletion_renew_v2($1)`, renewBody)
			renewDone <- err
		}()
		go func() {
			_, err := v1Tx.Exec(ctx, `select public.record_deletion_mark_delete_commit_unknown_v1($1::bytea)`, v1Body)
			v1Done <- err
		}()
		waitForPostgresBlockingPID(t, ctx, f.fixture.db, issuePID, aPID)
		waitForPostgresBlockingPID(t, ctx, f.fixture.db, renewPID, aPID)
		waitForPostgresBlockingPID(t, ctx, f.fixture.db, v1PID, aPID)
		if err := a.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		for _, ch := range []struct {
			ch   chan error
			code string
		}{{issueDone, "55000"}, {renewDone, "42501"}, {v1Done, "42501"}} {
			select {
			case err = <-ch.ch:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != ch.code {
				t.Fatalf("competitor after finish = %v, want %s", err, ch.code)
			}
		}
		_ = issueTx.Rollback(ctx)
		_ = renewTx.Rollback(ctx)
		_ = v1Tx.Rollback(ctx)
	} else if err := a.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var got result
	select {
	case got = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if got.err != nil || (commit && got.claim != nil) || (!commit && (got.claim == nil || got.claim.Owner.Generation != short.Owner.Generation+1)) {
		t.Fatalf("terminal takeover after commit=%t: %v, %v", commit, got.claim, got.err)
	}
	if commit {
		if err := f.repository.CompleteV2OnlinePurge(ctx, short, f.receipt); err != nil {
			t.Fatalf("late terminal readback: %v", err)
		}
	}
	var valid bool
	if err := f.fixture.db.QueryRow(ctx, `select (case when $2 then
		o.operation_state='online_purged' and o.completed_at is not null and o.receipt_digest=$3
		and (o.owner_id,o.owner_generation,o.owner_expires_at) is not distinct from ('',0::bigint,null::timestamptz)
		and r.state='committed' and (r.owner_id,r.owner_generation,r.owner_expires_at) is not distinct from ('',0::bigint,null::timestamptz)
		and not exists(select 1 from public.deletion_fence_leases l where l.project_id=o.project_id and l.object_kind='record' and l.object_id=r.object_id)
	else o.operation_state='online_purging' and o.completed_at is null and o.receipt_digest is null and r.state='fenced'
		and o.owner_id='v2_terminal_b' and o.owner_generation=$4 and o.owner_expires_at is not null
		and (r.owner_id,r.owner_generation,r.owner_expires_at) is not distinct from (o.owner_id,o.owner_generation,o.owner_expires_at)
		and exists(select 1 from public.deletion_fence_leases l where l.project_id=o.project_id and l.object_kind='record' and l.object_id=r.object_id and (l.owner_id,l.owner_generation,l.expires_at) is not distinct from (o.owner_id,o.owner_generation,o.owner_expires_at)) end)
		and o.deletion_contract_version=2 and r.deletion_contract_version=2 and r.reservation_id=$5 and r.object_id=$6 and r.fence_epoch=$7
		and exists(select 1 from public.record_deletion_gates g where (g.operation_id,g.project_id,g.record_id,g.fence_epoch,g.binding_digest,g.request_fingerprint,g.actor_scope_digest,g.actor_id) is not distinct from (o.operation_id,r.project_id,r.object_id,r.fence_epoch,r.preview_binding_digest,r.request_fingerprint,r.actor_scope_digest,o.actor_id))
		from public.record_purge_operations o join public.deletion_reservations r on r.reservation_id=o.reservation_id where o.operation_id=$1`, short.Operation.OperationID, commit, f.receipt.ReceiptDigest[:], short.Owner.Generation+1, short.Operation.ReservationID, short.Operation.Object.ObjectID, short.Operation.FenceEpoch).Scan(&valid); err != nil || !valid {
		t.Fatalf("terminal outcome commit=%t valid=%t: %v", commit, valid, err)
	}
	if after := durableSnapshot(); after != beforeFinish {
		t.Fatalf("finish commit=%t changed previously committed adapter effects, receipts, cleanup or quota", commit)
	}
}
