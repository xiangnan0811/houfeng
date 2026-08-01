package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordplatform"
)

func TestPostgresRecordPlatformAcquireIdentityMutationGuardsAdmitsAndSortsKeys(t *testing.T) {
	expiresAt := time.Date(2026, time.July, 24, 16, 1, 0, 0, time.UTC)
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		if !strings.Contains(sql, "insert into public.identity_mutation_guards") {
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*(dest[0].(*string)) = "worker_01"
			*(dest[1].(*int64)) = 1
			*(dest[2].(*time.Time)) = expiresAt
			return nil
		}}
	}
	repository := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			tx.calls = append(tx.calls, "begin")
			return tx, nil
		},
		gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error { tx.calls = append(tx.calls, "gate"); return nil }),
	}
	first := recordplatform.IdentityMutationGuardKeyV1{Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}, MutationKind: "rename"}
	second := recordplatform.IdentityMutationGuardKeyV1{Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_02"}, MutationKind: "rename"}

	guards, err := repository.AcquireIdentityMutationGuards(context.Background(), []recordplatform.IdentityMutationGuardKeyV1{second, first}, recordplatform.LeaseClaimInputV1{OwnerID: "worker_01", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("AcquireIdentityMutationGuards() error = %v", err)
	}
	if len(guards) != 2 || guards[0].Key != first || guards[1].Key != second || guards[0].Owner.Generation != 1 || !guards[0].Owner.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("AcquireIdentityMutationGuards() = %#v, want sorted fenced guards", guards)
	}
	if got, want := tx.calls, []string{"begin", "gate", "query", "query"}; !equalRecordPlatformStrings(got, want) {
		t.Fatalf("transaction calls = %#v, want %#v", got, want)
	}
	if len(tx.queryArgs) != 2 || tx.queryArgs[0][2] != "rec_01" || tx.queryArgs[1][2] != "rec_02" {
		t.Fatalf("guard SQL arguments = %#v, want canonical object order", tx.queryArgs)
	}
	for _, sql := range tx.querySQL {
		for _, fragment := range []string{"on conflict", "owner_generation = identity_mutation_guards.owner_generation + 1", "expires_at <= transaction_timestamp()", "returning owner_id, owner_generation, expires_at"} {
			if !strings.Contains(sql, fragment) {
				t.Fatalf("guard acquire SQL missing %q:\n%s", fragment, sql)
			}
		}
	}
}

func TestPostgresRecordPlatformGuardNilOrFailingGateMakesZeroPrimitiveWrites(t *testing.T) {
	key := recordplatform.IdentityMutationGuardKeyV1{Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}, MutationKind: "rename"}
	for _, test := range []struct {
		name string
		gate AdmissionGate
	}{
		{name: "nil gate"},
		{name: "failing gate", gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error { return errors.New("membership unavailable") })},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeRecordPlatformTx{}
			repository := &PostgresRecordPlatformRepository{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }, gate: test.gate}
			if _, err := repository.AcquireIdentityMutationGuards(context.Background(), []recordplatform.IdentityMutationGuardKeyV1{key}, recordplatform.LeaseClaimInputV1{OwnerID: "worker_01", LeaseDuration: time.Minute}); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
				t.Fatalf("AcquireIdentityMutationGuards() error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
			}
			if tx.queryCount != 0 || tx.execCount != 0 || tx.committed {
				t.Fatalf("gate failure primitive state queries=%d execs=%d committed=%v, want zero writes", tx.queryCount, tx.execCount, tx.committed)
			}
		})
	}
}

func TestPostgresRecordPlatformObjectContentLeaseUsesLiveOwnerFenceForAcquireRenewRelease(t *testing.T) {
	expiresAt := time.Date(2026, time.July, 24, 16, 1, 0, 0, time.UTC)
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from public.content_delivery_epochs"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error { *(dest[0].(*int64)) = 0; return nil }}
		case strings.Contains(sql, "from public.deletion_fence_leases"):
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "public.object_content_leases"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "worker_01"
				*(dest[1].(*int64)) = 2
				*(dest[2].(*time.Time)) = expiresAt
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
	}
	tx.exec = func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("DELETE 1"), nil
	}
	repository := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { tx.calls = append(tx.calls, "gate"); return nil }),
	}
	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}

	lease, err := repository.AcquireObjectContentLease(context.Background(), object, recordplatform.LeaseClaimInputV1{OwnerID: "worker_01", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("AcquireObjectContentLease() error = %v", err)
	}
	if lease.Object != object || lease.Owner.Generation != 2 || !lease.Owner.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("AcquireObjectContentLease() = %#v, want object owner fence", lease)
	}
	renewed, err := repository.RenewObjectContentLease(context.Background(), object, lease.Owner, time.Minute)
	if err != nil {
		t.Fatalf("RenewObjectContentLease() error = %v", err)
	}
	if renewed.Owner != lease.Owner {
		t.Fatalf("RenewObjectContentLease() = %#v, want same returned owner from fixture", renewed)
	}
	if err := repository.ReleaseObjectContentLease(context.Background(), object, renewed.Owner); err != nil {
		t.Fatalf("ReleaseObjectContentLease() error = %v", err)
	}
	if len(tx.querySQL) != 4 || len(tx.execSQL) != 1 {
		t.Fatalf("lease SQL queries=%#v execs=%#v, want epoch/fence/object acquire, renew, release", tx.querySQL, tx.execSQL)
	}
	for index, relation := range []string{
		"public.content_delivery_epochs",
		"public.deletion_fence_leases",
	} {
		if !strings.Contains(tx.querySQL[index], relation) || !strings.Contains(tx.querySQL[index], "for update") {
			t.Fatalf("object-content claim lock %d = %q, want FOR UPDATE on %s", index, tx.querySQL[index], relation)
		}
	}
	if !strings.Contains(tx.querySQL[2], "public.object_content_leases") || !strings.Contains(tx.querySQL[2], "on conflict") {
		t.Fatalf("object-content claim SQL = %q, want fenced upsert after epoch and deletion-fence locks", tx.querySQL[2])
	}
	for index, sql := range append(append([]string(nil), tx.querySQL[2:]...), tx.execSQL...) {
		fragments := []string{"owner_id", "owner_generation"}
		if index == 0 {
			fragments = append(fragments, "expires_at <= transaction_timestamp()")
		} else {
			fragments = append(fragments, "expires_at > transaction_timestamp()")
		}
		for _, fragment := range fragments {
			if !strings.Contains(sql, fragment) {
				t.Fatalf("lease SQL missing %q:\n%s", fragment, sql)
			}
		}
	}
}

func TestPostgresRecordPlatformObjectContentLeaseFailsClosedForMissingEpochAndLiveDeletionFence(t *testing.T) {
	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}
	input := recordplatform.LeaseClaimInputV1{OwnerID: "worker_01", LeaseDuration: time.Minute}
	for _, test := range []struct {
		name    string
		query   func(string) pgx.Row
		wantErr error
	}{
		{
			name: "missing epoch",
			query: func(string) pgx.Row {
				return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
			},
			wantErr: recordplatform.ErrContentDeliveryEpochMissing,
		},
		{
			name: "live deletion fence",
			query: func(sql string) pgx.Row {
				if strings.Contains(sql, "from public.content_delivery_epochs") {
					return fakeRecordPlatformRow{scan: func(dest ...any) error { *(dest[0].(*int64)) = 0; return nil }}
				}
				if strings.Contains(sql, "from public.deletion_fence_leases") {
					return fakeRecordPlatformRow{scan: func(dest ...any) error {
						*(dest[0].(*string)) = "fence_worker"
						*(dest[1].(*int64)) = 1
						*(dest[2].(*time.Time)) = time.Date(2026, time.July, 24, 16, 1, 0, 0, time.UTC)
						*(dest[3].(*bool)) = true
						return nil
					}}
				}
				return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected object-content claim") }}
			},
			wantErr: recordplatform.ErrDeletionFenceLeaseLive,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row { return test.query(sql) }}
			repository := &PostgresRecordPlatformRepository{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
				gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
			}
			if _, err := repository.AcquireObjectContentLease(context.Background(), object, input); !errors.Is(err, test.wantErr) {
				t.Fatalf("AcquireObjectContentLease() error = %v, want %v", err, test.wantErr)
			}
			if tx.committed {
				t.Fatal("AcquireObjectContentLease() committed after a fail-closed precondition")
			}
		})
	}
}

func TestPostgresRecordPlatformServingLeaseIsCapturedAndAssertedFromOneAdmittedDatabaseTransaction(t *testing.T) {
	ctx := context.Background()
	expiresAt := time.Date(2026, time.July, 24, 16, 1, 0, 0, time.UTC)
	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from public.content_delivery_epochs"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error { *(dest[0].(*int64)) = 4; return nil }}
		case strings.Contains(sql, "from public.deletion_fence_leases") && strings.Contains(sql, "for update"):
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "insert into public.object_content_leases"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "worker_01"
				*(dest[1].(*int64)) = 2
				*(dest[2].(*time.Time)) = expiresAt
				return nil
			}}
		case strings.Contains(sql, "from public.object_content_leases as object_lease"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error { *(dest[0].(*int)) = 1; return nil }}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected serving lease query") }}
		}
	}
	repository := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { tx.calls = append(tx.calls, "gate"); return nil }),
	}

	serving, err := repository.AcquireServingLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "worker_01", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("AcquireServingLease() error = %v", err)
	}
	if serving.Object != object || serving.Owner.Generation != 2 || serving.CapturedEpoch != 4 {
		t.Fatalf("AcquireServingLease() = %#v, want database-captured owner and epoch", serving)
	}
	if !tx.committed {
		t.Fatal("AcquireServingLease() did not commit its admitted transaction")
	}
	if len(tx.querySQL) != 4 {
		t.Fatalf("serving lease query count = %d, want epoch/fence/object-lease/assert", len(tx.querySQL))
	}
	assertSQL := tx.querySQL[3]
	for _, fragment := range []string{
		"from public.object_content_leases as object_lease",
		"object_lease.owner_id = $",
		"object_lease.owner_generation = $",
		"object_lease.expires_at = $",
		"object_lease.expires_at > transaction_timestamp()",
		"epoch.delivery_epoch = $",
		"public.deletion_fence_leases",
		"expires_at > transaction_timestamp()",
	} {
		if !strings.Contains(assertSQL, fragment) {
			t.Fatalf("serving assertion SQL missing %q:\n%s", fragment, assertSQL)
		}
	}

	fabricated := serving
	fabricated.CapturedEpoch++
	staleTx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
		return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
	}}
	staleRepository := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return staleTx, nil },
		gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
	}
	if err := staleRepository.AssertServingLease(ctx, fabricated); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("AssertServingLease() fabricated epoch error = %v, want ErrLostOwnerLease", err)
	}
	if staleTx.queryCount != 1 || staleTx.committed {
		t.Fatalf("fabricated serving assertion state queries=%d committed=%v, want one rejected query and no commit", staleTx.queryCount, staleTx.committed)
	}
}

func TestPostgresRecordPlatformDeletionFenceAndClientContentLeaseUseTheirOwnRelations(t *testing.T) {
	expiresAt := time.Date(2026, time.July, 24, 16, 1, 0, 0, time.UTC)
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from public.content_delivery_epochs"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*int64)) = 0
				return nil
			}}
		case strings.Contains(sql, "public.deletion_fence_leases"), strings.Contains(sql, "public.client_content_leases"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "worker_01"
				*(dest[1].(*int64)) = 1
				*(dest[2].(*time.Time)) = expiresAt
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected lease query") }}
		}
	}
	repository := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
	}
	input := recordplatform.LeaseClaimInputV1{OwnerID: "worker_01", LeaseDuration: time.Minute}
	if _, err := repository.AcquireDeletionFenceLease(context.Background(), recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}, input); err != nil {
		t.Fatalf("AcquireDeletionFenceLease() error = %v", err)
	}
	if _, err := repository.AcquireClientContentLease(context.Background(), recordplatform.ClientContentLeaseKeyV1{ProjectID: "default", ClientID: "client_01"}, input); err != nil {
		t.Fatalf("AcquireClientContentLease() error = %v", err)
	}
	if len(tx.querySQL) != 3 ||
		!strings.Contains(tx.querySQL[0], "public.content_delivery_epochs") ||
		!strings.Contains(tx.querySQL[0], "for update") ||
		!strings.Contains(tx.querySQL[1], "public.deletion_fence_leases") ||
		!strings.Contains(tx.querySQL[2], "public.client_content_leases") {
		t.Fatalf("lease relation SQL = %#v, want epoch then deletion fence then client content tables", tx.querySQL)
	}
}

func TestPostgresRecordPlatformDeletionFenceLeaseFailsClosedForMissingEpochBeforeClaim(t *testing.T) {
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		if !strings.Contains(sql, "from public.content_delivery_epochs") {
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("deletion-fence claim ran before epoch lock") }}
		}
		return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
	}
	repository := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			tx.calls = append(tx.calls, "begin")
			return tx, nil
		},
		gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error {
			tx.calls = append(tx.calls, "gate")
			return nil
		}),
	}

	_, err := repository.AcquireDeletionFenceLease(context.Background(), recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}, recordplatform.LeaseClaimInputV1{OwnerID: "worker_01", LeaseDuration: time.Minute})
	if !errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) {
		t.Fatalf("AcquireDeletionFenceLease() error = %v, want ErrContentDeliveryEpochMissing", err)
	}
	if tx.committed || tx.rollbackCount == 0 {
		t.Fatalf("missing epoch transaction committed=%v rollbacks=%d, want rollback only", tx.committed, tx.rollbackCount)
	}
	if len(tx.querySQL) != 1 || !strings.Contains(tx.querySQL[0], "from public.content_delivery_epochs") || !strings.Contains(tx.querySQL[0], "for update") {
		t.Fatalf("missing epoch query sequence = %#v, want only content_delivery_epochs FOR UPDATE", tx.querySQL)
	}
	if got, want := tx.calls, []string{"begin", "gate", "query"}; !equalRecordPlatformStrings(got, want) {
		t.Fatalf("missing epoch transaction calls = %#v, want %#v", got, want)
	}
}

func TestPostgresRecordPlatformLeaseRenewalsUseDurationAfterOwnerGenerationAndMapStaleRows(t *testing.T) {
	for _, test := range []struct {
		name      string
		sql       string
		wantParam string
		renew     func(*PostgresRecordPlatformRepository) error
	}{
		{
			name:      "identity guard",
			sql:       renewIdentityMutationGuardSQL,
			wantParam: "($7 * interval '1 microsecond')",
			renew: func(repository *PostgresRecordPlatformRepository) error {
				_, err := repository.RenewIdentityMutationGuard(context.Background(), recordplatform.IdentityMutationGuardKeyV1{Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}, MutationKind: "rename"}, recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: time.Date(2026, time.July, 24, 16, 1, 0, 0, time.UTC)}, time.Minute)
				return err
			},
		},
		{
			name:      "deletion fence",
			sql:       renewDeletionFenceLeaseSQL,
			wantParam: "($6 * interval '1 microsecond')",
			renew: func(repository *PostgresRecordPlatformRepository) error {
				_, err := repository.RenewDeletionFenceLease(context.Background(), recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}, recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: time.Date(2026, time.July, 24, 16, 1, 0, 0, time.UTC)}, time.Minute)
				return err
			},
		},
		{
			name:      "object content",
			sql:       renewObjectContentLeaseSQL,
			wantParam: "($6 * interval '1 microsecond')",
			renew: func(repository *PostgresRecordPlatformRepository) error {
				_, err := repository.RenewObjectContentLease(context.Background(), recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}, recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: time.Date(2026, time.July, 24, 16, 1, 0, 0, time.UTC)}, time.Minute)
				return err
			},
		},
		{
			name:      "client content",
			sql:       renewClientContentLeaseSQL,
			wantParam: "($5 * interval '1 microsecond')",
			renew: func(repository *PostgresRecordPlatformRepository) error {
				_, err := repository.RenewClientContentLease(context.Background(), recordplatform.ClientContentLeaseKeyV1{ProjectID: "default", ClientID: "client_01"}, recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: time.Date(2026, time.July, 24, 16, 1, 0, 0, time.UTC)}, time.Minute)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !strings.Contains(test.sql, test.wantParam) {
				t.Fatalf("renew SQL = %s, want duration placeholder %q", test.sql, test.wantParam)
			}
			tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
				return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
			}}
			repository := &PostgresRecordPlatformRepository{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
				gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
			}
			if err := test.renew(repository); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
				t.Fatalf("stale renewal error = %v, want ErrLostOwnerLease", err)
			}
		})
	}
}

func TestPostgresRecordPlatformLeaseReleasesRetainGenerationTombstones(t *testing.T) {
	for _, test := range []struct {
		name string
		sql  string
	}{
		{name: "identity mutation guard", sql: releaseIdentityMutationGuardSQL},
		{name: "deletion fence", sql: releaseDeletionFenceLeaseSQL},
		{name: "object content", sql: releaseObjectContentLeaseSQL},
		{name: "client content", sql: releaseClientContentLeaseSQL},
		{name: "reservation deletion fence", sql: releaseDeletionFenceLeaseForReservationSQL},
	} {
		t.Run(test.name, func(t *testing.T) {
			if strings.Contains(strings.ToLower(test.sql), "delete from") {
				t.Fatalf("release SQL deletes its generation row and permits ABA reuse:\n%s", test.sql)
			}
			for _, fragment := range []string{
				"set expires_at = transaction_timestamp()",
				"owner_id = $",
				"owner_generation = $",
				"expires_at > transaction_timestamp()",
			} {
				if !strings.Contains(test.sql, fragment) {
					t.Fatalf("release SQL missing %q:\n%s", fragment, test.sql)
				}
			}
		})
	}
}

func TestPostgresRecordPlatformLeaseReleasesAllowImmediateOwnerRelease(t *testing.T) {
	for _, test := range []struct {
		name string
		sql  string
	}{
		{name: "identity mutation guard", sql: releaseIdentityMutationGuardSQL},
		{name: "deletion fence", sql: releaseDeletionFenceLeaseSQL},
		{name: "object content", sql: releaseObjectContentLeaseSQL},
		{name: "client content", sql: releaseClientContentLeaseSQL},
		{name: "reservation deletion fence", sql: releaseDeletionFenceLeaseForReservationSQL},
	} {
		t.Run(test.name, func(t *testing.T) {
			if strings.Contains(test.sql, "created_at < transaction_timestamp()") {
				t.Fatalf("release SQL rejects a valid immediate release:\n%s", test.sql)
			}
		})
	}
}

func TestPostgresRecordPlatformLeaseLifecycleBindsObservedOwnerExpiry(t *testing.T) {
	owner := recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: time.Date(2026, time.July, 24, 16, 1, 0, 0, time.UTC)}
	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}
	guardKey := recordplatform.IdentityMutationGuardKeyV1{Object: object, MutationKind: "rename"}
	clientKey := recordplatform.ClientContentLeaseKeyV1{ProjectID: "default", ClientID: "client_01"}
	for _, test := range []struct {
		name      string
		query     bool
		lifecycle func(*PostgresRecordPlatformRepository) error
	}{
		{name: "renew identity guard", query: true, lifecycle: func(repository *PostgresRecordPlatformRepository) error {
			_, err := repository.RenewIdentityMutationGuard(context.Background(), guardKey, owner, time.Minute)
			return err
		}},
		{name: "release identity guard", lifecycle: func(repository *PostgresRecordPlatformRepository) error {
			return repository.ReleaseIdentityMutationGuard(context.Background(), guardKey, owner)
		}},
		{name: "renew deletion fence", query: true, lifecycle: func(repository *PostgresRecordPlatformRepository) error {
			_, err := repository.RenewDeletionFenceLease(context.Background(), object, owner, time.Minute)
			return err
		}},
		{name: "release deletion fence", lifecycle: func(repository *PostgresRecordPlatformRepository) error {
			return repository.ReleaseDeletionFenceLease(context.Background(), object, owner)
		}},
		{name: "renew object content", query: true, lifecycle: func(repository *PostgresRecordPlatformRepository) error {
			_, err := repository.RenewObjectContentLease(context.Background(), object, owner, time.Minute)
			return err
		}},
		{name: "release object content", lifecycle: func(repository *PostgresRecordPlatformRepository) error {
			return repository.ReleaseObjectContentLease(context.Background(), object, owner)
		}},
		{name: "renew client content", query: true, lifecycle: func(repository *PostgresRecordPlatformRepository) error {
			_, err := repository.RenewClientContentLease(context.Background(), clientKey, owner, time.Minute)
			return err
		}},
		{name: "release client content", lifecycle: func(repository *PostgresRecordPlatformRepository) error {
			return repository.ReleaseClientContentLease(context.Background(), clientKey, owner)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeRecordPlatformTx{}
			repository := &PostgresRecordPlatformRepository{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
				gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
			}
			if err := test.lifecycle(repository); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
				t.Fatalf("lifecycle error = %v, want ErrLostOwnerLease", err)
			}
			if test.query {
				if len(tx.querySQL) != 1 || !strings.Contains(tx.querySQL[0], "and expires_at = $") {
					t.Fatalf("renew SQL = %#v, want exact observed owner-expiry predicate", tx.querySQL)
				}
				assertRecordPlatformOwnerExpiryArgument(t, tx.queryArgs[0], owner.ExpiresAt)
				return
			}
			if len(tx.execSQL) != 1 || !strings.Contains(tx.execSQL[0], "and expires_at = $") {
				t.Fatalf("release SQL = %#v, want exact observed owner-expiry predicate", tx.execSQL)
			}
			assertRecordPlatformOwnerExpiryArgument(t, tx.execArgs[0], owner.ExpiresAt)
		})
	}
}

func equalRecordPlatformStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
