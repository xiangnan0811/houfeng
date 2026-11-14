package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/records"
)

type v2CoreHook func(context.Context, recordPlatformPostgresFixture, *pgxpool.Pool, *pgxpool.Pool, records.RevisionCommitCommand, recorddeletion.PurgeTarget)

type v2CoreHookAdapter struct {
	recorddeletion.DeletionPurgeAdapter
	hook func(recorddeletion.PurgeTarget)
}

func (a v2CoreHookAdapter) PurgeDeletion(ctx context.Context, target recorddeletion.PurgeTarget) (recorddeletion.AdapterPurgeReceipt, error) {
	a.hook(target)
	return a.DeletionPurgeAdapter.PurgeDeletion(ctx, target)
}

func testV2CoreCreatorFence(t *testing.T, factory func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error)) {
	called := false
	setupV2FinishFixtureWithCoreHook(t, factory, func(ctx context.Context, fixture recordPlatformPostgresFixture, runtime, worker *pgxpool.Pool, command records.RevisionCommitCommand, target recorddeletion.PurgeTarget) {
		called = true
		purge, err := worker.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = purge.Rollback(context.Background()) }()
		body, err := deletionAdapterEnvelope(ctx, purge, target, recorddeletion.RecordCoreSurfaceDigest())
		if err != nil {
			t.Fatal(err)
		}
		var surface, receipt []byte
		var count int64
		var verified any
		if err := purge.QueryRow(ctx, `select * from public.record_core_purge_v2($1)`, body).Scan(&surface, &receipt, &count, &verified); err != nil {
			t.Fatalf("core purge: %v", err)
		}
		insert := `insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch) values('default','record',$1,0) on conflict(project_id,object_kind,object_id) do nothing`
		creator, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = creator.Exec(ctx, insert, target.Operation.Object.ObjectID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
			_ = creator.Rollback(ctx)
			t.Fatalf("epoch creator while core holds lock: %v, want 40001", err)
		}
		if err := creator.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		birthHeld, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = consumeRecordBirthOnTx(ctx, birthHeld, command.BirthRequest, command.BirthPermit)
		pgErr = nil
		if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
			_ = birthHeld.Rollback(ctx)
			t.Fatalf("birth replay while core holds lock: %v, want 40001", err)
		}
		if err := birthHeld.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if err := purge.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		creator, err = runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = creator.Exec(ctx, insert, target.Operation.Object.ObjectID)
		pgErr = nil
		if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
			_ = creator.Rollback(ctx)
			t.Fatalf("epoch creator after core commit: %v, want 55000", err)
		}
		if err := creator.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		birth, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = consumeRecordBirthOnTx(ctx, birth, command.BirthRequest, command.BirthPermit)
		pgErr = nil
		if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
			_ = birth.Rollback(ctx)
			t.Fatalf("birth replay after core: %v, want 55000", err)
		}
		if err := birth.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var root, epoch bool
		if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.records where record_id=$1),exists(select 1 from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1)`, target.Operation.Object.ObjectID).Scan(&root, &epoch); err != nil || root || epoch {
			t.Fatalf("creator restored root=%t epoch=%t: %v", root, epoch, err)
		}
	})
	if !called {
		t.Fatal("core transaction hook was not reached")
	}
}
