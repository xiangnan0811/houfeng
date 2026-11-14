package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordplatform"
)

// Exercise the current constrained runtime role against the actual lock command.
func TestPostgresIntegrationServingDeletionFenceLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	created := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "serving-fence-lock-parent")
	repository := NewPostgresRecordPlatformRepository(runtime, gate)
	record := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: created.RecordID}
	nonRecord := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "export", ObjectID: "export_serving_lock"}
	seedServingTestEpoch(t, ctx, fixture, nonRecord)

	for _, object := range []recordplatform.ObjectRef{record, nonRecord} {
		t.Run(object.ObjectKind, func(t *testing.T) {
			input := recordplatform.LeaseClaimInputV1{OwnerID: "serving_lock_owner", LeaseDuration: time.Minute}
			serving, err := repository.AcquireServingLease(ctx, object, input)
			if err != nil {
				t.Fatalf("acquire without fence: %v", err)
			}
			renewed, err := repository.RenewServingLease(ctx, serving, 2*time.Minute)
			if err != nil || renewed.Owner.Generation != serving.Owner.Generation || !renewed.Owner.ExpiresAt.After(serving.Owner.ExpiresAt) {
				t.Fatalf("renew without fence = (%#v, %v), previous %#v", renewed, err, serving)
			}
			if _, err := repository.RenewServingLease(ctx, serving, time.Minute); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
				t.Fatalf("renew with stale expiry = %v, want lost owner", err)
			}
			wrongEpoch := renewed
			wrongEpoch.CapturedEpoch++
			if _, err := repository.RenewServingLease(ctx, wrongEpoch, time.Minute); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
				t.Fatalf("renew with wrong epoch = %v, want lost owner", err)
			}

			seedServingTestFence(t, ctx, fixture, object, "clock_timestamp() + interval '5 minutes'")
			if _, err := repository.RenewServingLease(ctx, renewed, time.Minute); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
				t.Fatalf("renew behind live fence = %v, want lost owner", err)
			}
			if _, err := repository.AcquireServingLease(ctx, object, input); !errors.Is(err, recordplatform.ErrDeletionFenceLeaseLive) {
				t.Fatalf("acquire behind live fence = %v, want live fence", err)
			}
			if _, err := fixture.db.Exec(ctx, `update public.deletion_fence_leases set created_at=clock_timestamp()-interval '2 minutes', expires_at=clock_timestamp()-interval '1 minute'
				where project_id=$1 and object_kind=$2 and object_id=$3`, object.ProjectID, object.ObjectKind, object.ObjectID); err != nil {
				t.Fatalf("expire test fence: %v", err)
			}
			afterExpiry, err := repository.RenewServingLease(ctx, renewed, 3*time.Minute)
			if err != nil {
				t.Fatalf("renew behind expired fence: %v", err)
			}
			if err := repository.ReleaseObjectContentLease(ctx, object, afterExpiry.Owner); err != nil {
				t.Fatalf("release serving lease: %v", err)
			}
			reacquired, err := repository.AcquireServingLease(ctx, object, input)
			if err != nil || reacquired.Owner.Generation != afterExpiry.Owner.Generation+1 {
				t.Fatalf("acquire behind expired fence = (%#v, %v), want next content owner generation", reacquired, err)
			}
		})
	}

	missing := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "export", ObjectID: "export_missing_epoch"}
	if _, err := repository.AcquireServingLease(ctx, missing, recordplatform.LeaseClaimInputV1{OwnerID: "serving_missing", LeaseDuration: time.Minute}); !errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) {
		t.Fatalf("acquire without epoch = %v, want missing epoch", err)
	}
	if _, err := repository.AcquireObjectContentLease(ctx, missing, recordplatform.LeaseClaimInputV1{OwnerID: "serving_missing", LeaseDuration: time.Minute}); !errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) {
		t.Fatalf("content acquire without epoch = %v, want missing epoch", err)
	}
	tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	_, err = lockDeletionFenceLeaseForFence(ctx, tx, missing)
	if !errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) {
		t.Fatalf("lock command without epoch = %v, want missing epoch", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	lostEpoch := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "export", ObjectID: "export_lost_epoch"}
	seedServingTestEpoch(t, ctx, fixture, lostEpoch)
	prior, err := repository.AcquireServingLease(ctx, lostEpoch, recordplatform.LeaseClaimInputV1{OwnerID: "serving_lost_epoch", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("acquire before epoch removal: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `delete from public.content_delivery_epochs where project_id=$1 and object_kind=$2 and object_id=$3`, lostEpoch.ProjectID, lostEpoch.ObjectKind, lostEpoch.ObjectID); err != nil {
		t.Fatalf("remove test epoch: %v", err)
	}
	if _, err := repository.RenewServingLease(ctx, prior, time.Minute); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("renew with missing epoch = %v, want lost owner", err)
	}

	t.Run("fresh clock after fence wait", func(t *testing.T) {
		object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "export", ObjectID: "export_fence_wait"}
		seedServingTestEpoch(t, ctx, fixture, object)
		seedServingTestFence(t, ctx, fixture, object, "clock_timestamp() + interval '5 minutes'")
		holder, err := fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = holder.Rollback(context.Background()) }()
		var holderPID int
		if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
			t.Fatal(err)
		}
		var one int
		if err := holder.QueryRow(ctx, `select 1 from public.deletion_fence_leases where project_id=$1 and object_kind=$2 and object_id=$3 for update`, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&one); err != nil {
			t.Fatal(err)
		}
		workerPool := fixture.openDirectRuntimePool(t, ctx, "serving-fence-clock-wait", 1)
		var workerPID int
		if err := workerPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
			t.Fatal(err)
		}
		worker := NewPostgresRecordPlatformRepository(workerPool, gate)
		type result struct {
			lease recordplatform.ServingLeaseV1
			err   error
		}
		results := make(chan result, 1)
		go func() {
			lease, err := worker.AcquireServingLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "serving_wait_owner", LeaseDuration: time.Minute})
			results <- result{lease, err}
		}()
		waitForMutationTestBlocker(t, ctx, fixture.db, workerPID, holderPID, results)
		var expiresAt time.Time
		if err := holder.QueryRow(ctx, `update public.deletion_fence_leases set expires_at=clock_timestamp()+interval '1 second' where project_id=$1 and object_kind=$2 and object_id=$3 returning expires_at`, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&expiresAt); err != nil {
			t.Fatal(err)
		}
		waitServingTestFenceExpired(t, ctx, fixture, expiresAt)
		if err := holder.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		got := waitForRecordPlatformResult(t, results, "serving acquisition after fence expiry")
		if got.err != nil {
			t.Fatalf("acquire after blocked fence expired = %v", got.err)
		}
		if err := worker.AssertServingLease(ctx, got.lease); err != nil {
			t.Fatalf("assert acquired serving lease: %v", err)
		}
	})

	t.Run("cancel while waiting for epoch", func(t *testing.T) {
		object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "export", ObjectID: "export_epoch_cancel"}
		seedServingTestEpoch(t, ctx, fixture, object)
		holder, err := fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = holder.Rollback(context.Background()) }()
		var holderPID, one int
		if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
			t.Fatal(err)
		}
		if err := holder.QueryRow(ctx, `select 1 from public.content_delivery_epochs where project_id=$1 and object_kind=$2 and object_id=$3 for update`, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&one); err != nil {
			t.Fatal(err)
		}
		workerPool := fixture.openDirectRuntimePool(t, ctx, "serving-epoch-cancel", 1)
		var workerPID int
		if err := workerPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
			t.Fatal(err)
		}
		worker := NewPostgresRecordPlatformRepository(workerPool, gate)
		workerCtx, stop := context.WithCancel(ctx)
		defer stop()
		results := make(chan error, 1)
		go func() {
			_, err := worker.AcquireServingLease(workerCtx, object, recordplatform.LeaseClaimInputV1{OwnerID: "serving_cancel_owner", LeaseDuration: time.Minute})
			results <- err
		}()
		waitForMutationTestBlocker(t, ctx, fixture.db, workerPID, holderPID, results)
		stop()
		if err := waitForRecordPlatformResult(t, results, "cancelled serving acquisition"); err == nil {
			t.Fatal("cancelled blocked acquisition succeeded")
		}
		if err := holder.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := worker.AcquireServingLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "serving_after_cancel", LeaseDuration: time.Minute}); err != nil {
			t.Fatalf("acquire after cancellation and rollback: %v", err)
		}
	})

	t.Run("cancel while waiting for fence releases epoch", func(t *testing.T) {
		object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "export", ObjectID: "export_fence_cancel"}
		seedServingTestEpoch(t, ctx, fixture, object)
		seedServingTestFence(t, ctx, fixture, object, "clock_timestamp() - interval '1 microsecond'")
		holder, err := fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = holder.Rollback(context.Background()) }()
		var holderPID, one int
		if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
			t.Fatal(err)
		}
		if err := holder.QueryRow(ctx, `select 1 from public.deletion_fence_leases where project_id=$1 and object_kind=$2 and object_id=$3 for update`, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&one); err != nil {
			t.Fatal(err)
		}
		workerPool := fixture.openDirectRuntimePool(t, ctx, "serving-fence-cancel", 1)
		var workerPID int
		if err := workerPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
			t.Fatal(err)
		}
		worker := NewPostgresRecordPlatformRepository(workerPool, gate)
		workerCtx, stop := context.WithCancel(ctx)
		defer stop()
		results := make(chan error, 1)
		go func() {
			_, err := worker.AcquireServingLease(workerCtx, object, recordplatform.LeaseClaimInputV1{OwnerID: "serving_fence_cancel", LeaseDuration: time.Minute})
			results <- err
		}()
		waitForMutationTestBlocker(t, ctx, fixture.db, workerPID, holderPID, results)
		stop()
		if err := waitForRecordPlatformResult(t, results, "cancelled fence acquisition"); err == nil {
			t.Fatal("cancelled fence acquisition succeeded")
		}
		probe, err := fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := probe.QueryRow(ctx, `select 1 from public.content_delivery_epochs where project_id=$1 and object_kind=$2 and object_id=$3 for update nowait`, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&one); err != nil {
			_ = probe.Rollback(ctx)
			t.Fatalf("cancelled transaction retained epoch lock: %v", err)
		}
		if err := probe.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if err := holder.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := worker.AcquireServingLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "serving_fence_after_cancel", LeaseDuration: time.Minute}); err != nil {
			t.Fatalf("acquire after fence cancellation: %v", err)
		}
	})

	t.Run("restricted issuer waits for content drain", func(t *testing.T) {
		created := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "serving-issuer-parent")
		object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: created.RecordID}
		seedServingTestFence(t, ctx, fixture, object, "clock_timestamp() - interval '1 microsecond'")
		serving, err := repository.AcquireServingLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "serving_issuer_owner", LeaseDuration: time.Minute})
		if err != nil {
			t.Fatalf("acquire serving before issuance: %v", err)
		}
		var lifecycle, revision string
		var lockVersion, authorizationEpoch, contentEpoch int64
		if err := fixture.db.QueryRow(ctx, `select r.lifecycle,r.current_revision_id,r.lock_version,r.authorization_epoch,e.delivery_epoch
			from public.records r join public.content_delivery_epochs e on e.project_id=r.project_id and e.object_kind='record' and e.object_id=r.record_id
			where r.record_id=$1`, object.ObjectID).Scan(&lifecycle, &revision, &lockVersion, &authorizationEpoch, &contentEpoch); err != nil {
			t.Fatal(err)
		}
		// These fixed evidence bytes exercise the database command's identity and
		// lease contract only; they are not a nine-adapter or issuer attestation.
		digest := strings.Repeat("12", 32)
		token := strings.Repeat("34", 32)
		actor := "usr_aaaaaaaaaaaaaaaaaaaaaaaa"
		preview := map[string]any{
			"deployment_id": recordsPostgresActiveDeletionDeployment(t, ctx, fixture), "record_id": object.ObjectID,
			"reservation_id": "drs_servingissuer",
			"actor_id":       actor, "actor_scope_digest": digest, "token_commitment": token,
			"request_fingerprint": digest, "binding_digest": digest, "authorization_evidence": "12",
			"lifecycle": lifecycle, "revision_id": revision, "lock_version": lockVersion,
			"authorization_epoch": authorizationEpoch, "content_epoch": contentEpoch,
			"dependency_digest": digest, "processor_digest": digest, "obligation_digest": digest,
			"adapter_readiness_digest": digest, "adapter_preview_digest": digest, "backup_inventory_digest": nil,
		}
		previewBody, err := json.Marshal(preview)
		if err != nil {
			t.Fatal(err)
		}
		var previewRecord string
		if err := runtime.QueryRow(ctx, `select (public.record_deletion_preview_create($1::bytea)).record_id`, previewBody).Scan(&previewRecord); err != nil || previewRecord != object.ObjectID {
			t.Fatalf("create restricted deletion preview = %q, %v", previewRecord, err)
		}
		issue := map[string]any{"token_commitment": token, "actor_id": actor, "actor_scope_digest": digest,
			"record_id": object.ObjectID, "binding_digest": digest, "authorization_evidence": "12",
			"request_fingerprint": digest, "reservation_id": "drs_servingissuer", "operation_id": "rpo_servingissuer",
			"owner_id": "serving_issuer", "confirmed": true}
		issueBody, err := json.Marshal(issue)
		if err != nil {
			t.Fatal(err)
		}
		var operationID string
		err = runtime.QueryRow(ctx, `select (public.record_deletion_issue($1::bytea)).operation_id`, issueBody).Scan(&operationID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55000" || pgErr.Message != "record deletion has a live prior lease" {
			t.Fatalf("issue while serving is live = %v, want live-prior-lease refusal", err)
		}
		if err := repository.ReleaseObjectContentLease(ctx, object, serving.Owner); err != nil {
			t.Fatalf("drain serving lease: %v", err)
		}
		if err := runtime.QueryRow(ctx, `select (public.record_deletion_issue($1::bytea)).operation_id`, issueBody).Scan(&operationID); err != nil || operationID != "rpo_servingissuer" {
			t.Fatalf("issue after drain = %q, %v", operationID, err)
		}
		var generation, epoch int64
		if err := fixture.db.QueryRow(ctx, `select f.owner_generation,e.delivery_epoch from public.deletion_fence_leases f
			join public.content_delivery_epochs e using(project_id,object_kind,object_id)
			where f.project_id=$1 and f.object_kind=$2 and f.object_id=$3`, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&generation, &epoch); err != nil {
			t.Fatal(err)
		}
		if generation != 4 || epoch != contentEpoch+1 {
			t.Fatalf("issued fence generation=%d epoch=%d, want 4/%d", generation, epoch, contentEpoch+1)
		}
		if _, err := repository.RenewServingLease(ctx, serving, time.Minute); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("renew stale serving token after issuance = %v, want lost owner", err)
		}
	})

	t.Run("command validation and role", func(t *testing.T) {
		for _, body := range []string{
			`{"project_id":"other","object_kind":"record","object_id":"rec_01"}`,
			`{"project_id":"default","object_kind":"Bad Kind","object_id":"rec_01"}`,
			`{"project_id":"default","object_kind":"record","object_id":""}`,
			`{"project_id":"default","object_kind":"record","object_id":"drt1_` + strings.Repeat("c", 43) + `"}`,
			`{"project_id":"default","object_kind":"record","object_id":"rec_01","extra":1}`,
			`{"project_id":"default","project_id":"default","object_kind":"record","object_id":"rec_01"}`,
		} {
			var owner string
			err := runtime.QueryRow(ctx, `select owner_id from public.record_serving_deletion_fence_lock($1::bytea)`, []byte(body)).Scan(&owner)
			if err == nil || errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("invalid command %s accepted: %v", body, err)
			}
		}
		body, err := json.Marshal(struct {
			ProjectID  string `json:"project_id"`
			ObjectKind string `json:"object_kind"`
			ObjectID   string `json:"object_id"`
		}{record.ProjectID, record.ObjectKind, record.ObjectID})
		if err != nil {
			t.Fatal(err)
		}
		worker := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "serving-lock-wrong-role", 1)
		var owner string
		err = worker.QueryRow(ctx, `select owner_id from public.record_serving_deletion_fence_lock($1::bytea)`, body).Scan(&owner)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("worker command error = %v, want insufficient privilege", err)
		}
		err = runtime.QueryRow(ctx, `select 1 from public.deletion_fence_leases where project_id=$1 and object_kind=$2 and object_id=$3 for update`, record.ProjectID, record.ObjectKind, record.ObjectID).Scan(&owner)
		pgErr = nil
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("runtime raw fence lock error = %v, want insufficient privilege", err)
		}
		tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		err = tx.QueryRow(ctx, `select owner_id from public.record_serving_deletion_fence_lock($1::bytea)`, body).Scan(&owner)
		if err == nil || errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("repeatable read command accepted: %v", err)
		}
	})
}

func seedServingTestEpoch(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, object recordplatform.ObjectRef) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch) values ($1,$2,$3,0)`, object.ProjectID, object.ObjectKind, object.ObjectID); err != nil {
		t.Fatalf("seed epoch: %v", err)
	}
}

func seedServingTestFence(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, object recordplatform.ObjectRef, expirySQL string) {
	t.Helper()
	// expirySQL is selected exclusively by this test; it is never caller input.
	query := fmt.Sprintf(`insert into public.deletion_fence_leases(project_id,object_kind,object_id,owner_id,owner_generation,created_at,expires_at)
		values ($1,$2,$3,'serving_test_fence',3,clock_timestamp()-interval '1 second',%s)`, expirySQL)
	if _, err := fixture.db.Exec(ctx, query, object.ProjectID, object.ObjectKind, object.ObjectID); err != nil {
		t.Fatalf("seed fence: %v", err)
	}
}

func waitServingTestFenceExpired(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, expiresAt time.Time) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var expired bool
		if err := fixture.db.QueryRow(ctx, `select $1::timestamptz <= clock_timestamp()`, expiresAt).Scan(&expired); err != nil {
			t.Fatal(err)
		}
		if expired {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("fence did not expire: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}
