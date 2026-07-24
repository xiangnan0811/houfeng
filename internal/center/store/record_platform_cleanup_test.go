package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordplatform"
)

func TestPostgresRecordPlatformCleanupExpiresOnlyOutboxAndRetainsReusableGenerations(t *testing.T) {
	tx := &fakeRecordPlatformTx{}
	tx.exec = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
		switch {
		case strings.Contains(sql, "public.record_outbox"):
			return pgconn.NewCommandTag("DELETE 2"), nil
		default:
			return pgconn.CommandTag{}, errors.New("unexpected cleanup relation")
		}
	}
	repository := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
	}

	result, err := repository.CleanupExpiredRecordPlatformPrimitives(context.Background())
	if err != nil {
		t.Fatalf("CleanupExpiredRecordPlatformPrimitives() error = %v", err)
	}
	want := recordplatform.ExpiredPrimitiveCleanupResultV1{
		OutboxEvents: 2,
	}
	if result != want {
		t.Fatalf("CleanupExpiredRecordPlatformPrimitives() = %#v, want %#v", result, want)
	}
	if len(tx.execSQL) != 1 || !strings.Contains(tx.execSQL[0], "public.record_outbox") {
		t.Fatalf("cleanup SQL = %#v, want only non-reusable outbox delete", tx.execSQL)
	}
	for _, sql := range tx.execSQL {
		for _, relation := range []string{
			"public.record_idempotency_keys",
			"public.identity_mutation_guards",
			"public.deletion_fence_leases",
			"public.object_content_leases",
			"public.client_content_leases",
			"public.deletion_reservations",
			"public.record_purge_operations",
			"ledger",
		} {
			if strings.Contains(sql, relation) {
				t.Fatalf("cleanup must retain reusable generation or durable evidence %q:\n%s", relation, sql)
			}
		}
		if !strings.Contains(sql, "expires_at <= transaction_timestamp()") {
			t.Fatalf("cleanup SQL missing expired predicate:\n%s", sql)
		}
	}
	for _, fragment := range []string{"owner_expires_at is null", "owner_expires_at <= transaction_timestamp()"} {
		if !strings.Contains(tx.execSQL[0], fragment) {
			t.Fatalf("outbox cleanup SQL missing %q:\n%s", fragment, tx.execSQL[0])
		}
	}
}
