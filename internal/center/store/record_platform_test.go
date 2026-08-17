package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordplatform"
)

func TestPostgresRecordPlatformClaimIdempotencyRunsGateInsideTransactionBeforePrimitiveWrite(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		if strings.Contains(sql, "from public.record_idempotency_keys") {
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}
		if strings.Contains(sql, "insert into public.record_idempotency_keys") {
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "worker_01"
				*(dest[1].(*int64)) = 1
				*(dest[2].(*time.Time)) = now.Add(time.Minute)
				return nil
			}}
		}
		return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
	}

	var admittedTx pgx.Tx
	repo := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			tx.calls = append(tx.calls, "begin")
			return tx, nil
		},
		gate: AdmissionGateFunc(func(_ context.Context, got pgx.Tx) error {
			admittedTx = got
			tx.calls = append(tx.calls, "gate")
			return nil
		}),
	}

	claim, err := repo.ClaimIdempotency(ctx, recordplatform.IdempotencyClaimInputV1{
		Key:                recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "client-key.1"},
		RequestFingerprint: testStoreCanonicalRequestFingerprint(t, 0x11),
		OwnerID:            "worker_01",
		OwnerLeaseDuration: time.Minute,
		RecordTTL:          2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimIdempotency() error = %v", err)
	}
	if admittedTx != tx {
		t.Fatal("AdmissionGate did not receive the same transaction")
	}
	if claim.Owner == nil || claim.Owner.OwnerID != "worker_01" || claim.Owner.Generation != 1 || !claim.Owner.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("ClaimIdempotency() = %#v, want acquired generation-one owner", claim)
	}
	if len(tx.calls) < 4 || tx.calls[0] != "begin" || tx.calls[1] != "gate" || tx.calls[2] != "query" || tx.calls[3] != "query" {
		t.Fatalf("transaction call order = %#v, want begin gate select insert", tx.calls)
	}
	if !tx.committed {
		t.Fatal("ClaimIdempotency() did not commit successful transaction")
	}
}

func TestPostgresRecordPlatformClaimIdempotencyNilOrFailingGateMakesZeroPrimitiveWrites(t *testing.T) {
	input := recordplatform.IdempotencyClaimInputV1{
		Key:                recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "client-key.1"},
		RequestFingerprint: testStoreCanonicalRequestFingerprint(t, 0x11),
		OwnerID:            "worker_01",
		OwnerLeaseDuration: time.Minute,
		RecordTTL:          2 * time.Minute,
	}
	for _, test := range []struct {
		name string
		gate AdmissionGate
	}{
		{name: "nil gate"},
		{name: "failing gate", gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error { return errors.New("membership unavailable") })},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeRecordPlatformTx{}
			repo := &PostgresRecordPlatformRepository{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
				gate:    test.gate,
			}
			if _, err := repo.ClaimIdempotency(context.Background(), input); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
				t.Fatalf("ClaimIdempotency() error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
			}
			if tx.queryCount != 0 || tx.execCount != 0 || tx.committed {
				t.Fatalf("gate failure primitive state = queries %d execs %d committed %v, want zero writes", tx.queryCount, tx.execCount, tx.committed)
			}
		})
	}
}

func TestPostgresRecordPlatformTypedNilAdmissionGatePreventsPrimitiveWritesAndWorkerSend(t *testing.T) {
	var typedNil *recordPlatformTypedNilAdmissionGate
	admissionCalls := 0
	ctx := context.WithValue(context.Background(), recordPlatformTypedNilAdmissionGateCallKey{}, &admissionCalls)
	tx := &fakeRecordPlatformTx{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected primitive query") }}
		},
	}
	repo := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    typedNil,
	}

	if _, err := repo.ClaimIdempotency(ctx, recordplatform.IdempotencyClaimInputV1{
		Key:                recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "client-key.1"},
		RequestFingerprint: testStoreCanonicalRequestFingerprint(t, 0x11),
		OwnerID:            "worker_01",
		OwnerLeaseDuration: time.Minute,
		RecordTTL:          2 * time.Minute,
	}); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Errorf("ClaimIdempotency() error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
	}
	if admissionCalls != 0 || tx.queryCount != 0 || tx.execCount != 0 || tx.committed {
		t.Fatalf("typed-nil gate primitive state = gate calls %d queries %d execs %d committed %v, want zero", admissionCalls, tx.queryCount, tx.execCount, tx.committed)
	}

	sendCalls := 0
	worker := recordplatform.NewOutboxWorker(
		repo,
		recordPlatformTestFreshOutboxAuthorizer(func(context.Context, recordplatform.OutboxEvent) (recordplatform.RenderedDelivery, recordplatform.FreshAuthDecision, error) {
			t.Fatal("worker authorized after admission failure")
			return nil, recordplatform.FreshAuthDecision{}, nil
		}),
		recordPlatformTestOutboxSender(func(context.Context, recordplatform.RenderedDelivery) error {
			sendCalls++
			return nil
		}),
		recordplatform.OutboxWorkerOptions{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute, RetryDelay: time.Second},
	)
	if err := worker.RunOnce(ctx); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Errorf("OutboxWorker.RunOnce() error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
	}
	if admissionCalls != 0 || sendCalls != 0 || tx.queryCount != 0 || tx.execCount != 0 || tx.committed {
		t.Fatalf("typed-nil gate worker state = gate calls %d sends %d queries %d execs %d committed %v, want zero", admissionCalls, sendCalls, tx.queryCount, tx.execCount, tx.committed)
	}
}

func TestPostgresRecordPlatformCompleteIdempotencyUsesOneOwnerFencedWriteAndMapsZeroRowsToLostLease(t *testing.T) {
	tx := &fakeRecordPlatformTx{}
	repo := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { tx.calls = append(tx.calls, "gate"); return nil }),
	}
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "client-key.1"}
	owner := recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: time.Date(2026, time.July, 24, 12, 2, 0, 0, time.UTC)}
	result := testStoreCanonicalRequestFingerprint(t, 0x13)

	err := repo.CompleteIdempotency(context.Background(), key, owner, result)
	if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("CompleteIdempotency() error = %v, want ErrLostOwnerLease", err)
	}
	if tx.execCount != 1 {
		t.Fatalf("CompleteIdempotency() write count = %d, want exactly one fenced update", tx.execCount)
	}
	if len(tx.execSQL) != 1 {
		t.Fatalf("CompleteIdempotency() SQL = %#v, want one statement", tx.execSQL)
	}
	sql := tx.execSQL[0]
	for _, fragment := range []string{
		"owner_id = ''",
		"owner_expires_at = null",
		"owner_id = $",
		"owner_generation = $",
		"owner_expires_at > transaction_timestamp()",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("completion SQL missing %q:\n%s", fragment, sql)
		}
	}
	if len(tx.calls) < 2 || tx.calls[0] != "gate" || tx.calls[1] != "exec" {
		t.Fatalf("completion call order = %#v, want gate before exec", tx.calls)
	}
}

func TestPostgresRecordPlatformRenewAndReleaseIdempotencyUseLiveOwnerTripleFences(t *testing.T) {
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "client-key.1"}
	owner := recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: time.Date(2026, time.July, 24, 12, 2, 0, 0, time.UTC)}

	t.Run("renew stale owner", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{}
		repo := &PostgresRecordPlatformRepository{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
			gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { tx.calls = append(tx.calls, "gate"); return nil }),
		}
		_, err := repo.RenewIdempotency(context.Background(), recordplatform.IdempotencyRenewInputV1{
			Key:                key,
			Owner:              owner,
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          2 * time.Minute,
		})
		if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("RenewIdempotency() error = %v, want ErrLostOwnerLease", err)
		}
		if len(tx.querySQL) != 1 {
			t.Fatalf("RenewIdempotency() query count = %d, want one fenced update", len(tx.querySQL))
		}
		for _, fragment := range []string{
			"owner_id = $",
			"owner_generation = $",
			"owner_expires_at > transaction_timestamp()",
			"expires_at > transaction_timestamp() + ($",
			"set owner_expires_at = transaction_timestamp() + ($",
			"expires_at = transaction_timestamp() + ($",
		} {
			if !strings.Contains(tx.querySQL[0], fragment) {
				t.Fatalf("renew SQL missing %q:\n%s", fragment, tx.querySQL[0])
			}
		}
		if len(tx.calls) < 2 || tx.calls[0] != "gate" || tx.calls[1] != "query" {
			t.Fatalf("renew call order = %#v, want gate before fenced update", tx.calls)
		}
	})

	t.Run("release stale owner", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{}
		repo := &PostgresRecordPlatformRepository{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
			gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { tx.calls = append(tx.calls, "gate"); return nil }),
		}
		if err := repo.ReleaseIdempotency(context.Background(), key, owner); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("ReleaseIdempotency() error = %v, want ErrLostOwnerLease", err)
		}
		if len(tx.execSQL) != 1 {
			t.Fatalf("ReleaseIdempotency() write count = %d, want one fenced update", len(tx.execSQL))
		}
		for _, fragment := range []string{
			"set owner_expires_at = transaction_timestamp()",
			"owner_id = $",
			"owner_generation = $",
			"owner_expires_at > transaction_timestamp()",
		} {
			if !strings.Contains(tx.execSQL[0], fragment) {
				t.Fatalf("release SQL missing %q:\n%s", fragment, tx.execSQL[0])
			}
		}
		if len(tx.calls) < 2 || tx.calls[0] != "gate" || tx.calls[1] != "exec" {
			t.Fatalf("release call order = %#v, want gate before fenced update", tx.calls)
		}
	})
}

func TestRecordPlatformTransactionRenewsAndReleasesIdempotencyWithoutNestedTransaction(t *testing.T) {
	ctx := context.Background()
	originalOwner := recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: time.Date(2026, time.July, 24, 12, 2, 0, 0, time.UTC)}
	renewedOwner := originalOwner
	renewedOwner.ExpiresAt = originalOwner.ExpiresAt.Add(time.Minute)
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "client-key.1"}
	tx := &fakeRecordPlatformTx{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if !strings.Contains(sql, "update public.record_idempotency_keys") {
				return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
			}
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = renewedOwner.OwnerID
				*(dest[1].(*int64)) = int64(renewedOwner.Generation)
				*(dest[2].(*time.Time)) = renewedOwner.ExpiresAt
				return nil
			}}
		},
		exec: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			if !strings.Contains(sql, "update public.record_idempotency_keys") {
				return pgconn.CommandTag{}, errors.New("unexpected exec")
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	repository := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			tx.calls = append(tx.calls, "begin")
			return tx, nil
		},
		gate: AdmissionGateFunc(func(_ context.Context, admitted pgx.Tx) error {
			if admitted != tx {
				t.Fatal("AdmissionGate did not receive the callback transaction")
			}
			tx.calls = append(tx.calls, "gate")
			return nil
		}),
	}

	err := repository.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		renewed, err := transaction.RenewIdempotency(ctx, recordplatform.IdempotencyRenewInputV1{
			Key:                key,
			Owner:              originalOwner,
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          2 * time.Minute,
		})
		if err != nil {
			return err
		}
		if renewed != renewedOwner {
			return errors.New("renew did not return the transaction-scoped owner")
		}
		return transaction.ReleaseIdempotency(ctx, key, renewed)
	})
	if err != nil {
		t.Fatalf("RunRecordPlatformTransaction() error = %v", err)
	}
	if !tx.committed {
		t.Fatal("RunRecordPlatformTransaction() did not commit the outer transaction")
	}
	if got, want := tx.calls, []string{"begin", "gate", "gate", "query", "gate", "exec"}; !equalRecordPlatformStrings(got, want) {
		t.Fatalf("transaction calls = %#v, want %#v", got, want)
	}
}

func TestPostgresRecordPlatformTransactionAtomicallyCompletesIdempotencyAndEnqueuesOutbox(t *testing.T) {
	ctx := context.Background()
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from public.record_idempotency_keys"):
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "insert into public.record_idempotency_keys"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "worker_01"
				*(dest[1].(*int64)) = 1
				*(dest[2].(*time.Time)) = time.Date(2026, time.July, 24, 12, 1, 0, 0, time.UTC)
				return nil
			}}
		case strings.Contains(sql, "insert into public.record_outbox"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*int64)) = 42
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
	}
	tx.exec = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "update public.record_idempotency_keys") {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	repo := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { tx.calls = append(tx.calls, "gate"); return nil }),
	}
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "client-key.1"}

	err := repo.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		claim, err := transaction.ClaimIdempotency(ctx, recordplatform.IdempotencyClaimInputV1{
			Key:                key,
			RequestFingerprint: testStoreCanonicalRequestFingerprint(t, 0x11),
			OwnerID:            "worker_01",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          2 * time.Minute,
		})
		if err != nil {
			return err
		}
		if claim.Owner == nil {
			return errors.New("claim did not return an owner")
		}
		if _, err := recordPlatformTestExec(ctx, transaction, "insert into future_business_facts (fact_id) values ($1)", "fact_01"); err != nil {
			return err
		}
		if err := transaction.CompleteIdempotency(ctx, key, *claim.Owner, testStoreCanonicalRequestFingerprint(t, 0x12)); err != nil {
			return err
		}
		_, err = transaction.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{
			Event: recordplatform.OutboxEvent{
				ProjectID:          string(recordplatform.ProjectIDDefault),
				EventKind:          recordplatform.OutboxEventKindRecordCreated,
				SubjectKind:        recordplatform.OutboxSubjectKindRecord,
				SubjectID:          "rec_01",
				AuthorizationEpoch: 0,
			},
			ExpiresAfter: time.Hour,
		})
		return err
	})
	if err != nil {
		t.Fatalf("RunRecordPlatformTransaction() error = %v", err)
	}
	if !tx.committed || tx.rollbackCount == 0 {
		t.Fatalf("transaction state committed=%v rollbacks=%d, want one committed transaction", tx.committed, tx.rollbackCount)
	}
	if len(tx.execSQL) != 2 || !strings.Contains(tx.execSQL[1], "update public.record_idempotency_keys") {
		t.Fatalf("transaction writes = %#v, want business fact and fenced idempotency completion", tx.execSQL)
	}
	if len(tx.querySQL) != 3 || !strings.Contains(tx.querySQL[2], "insert into public.record_outbox") {
		t.Fatalf("transaction queries = %#v, want idempotency select/insert then outbox enqueue", tx.querySQL)
	}
	if gates := countRecordPlatformTestCalls(tx.calls, "gate"); gates != 3 {
		t.Fatalf("admission gate calls = %d, want transaction, claim, and completion gates", gates)
	}
}

func TestPostgresRecordPlatformTransactionRollsBackOnStaleIdempotencyCompletion(t *testing.T) {
	ctx := context.Background()
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from public.record_idempotency_keys"):
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "insert into public.record_idempotency_keys"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "worker_01"
				*(dest[1].(*int64)) = 1
				*(dest[2].(*time.Time)) = time.Date(2026, time.July, 24, 12, 1, 0, 0, time.UTC)
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
	}
	repo := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
	}
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "client-key.1"}

	err := repo.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		claim, err := transaction.ClaimIdempotency(ctx, recordplatform.IdempotencyClaimInputV1{
			Key:                key,
			RequestFingerprint: testStoreCanonicalRequestFingerprint(t, 0x11),
			OwnerID:            "worker_01",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          2 * time.Minute,
		})
		if err != nil {
			return err
		}
		if claim.Owner == nil {
			return errors.New("claim did not return an owner")
		}
		if _, err := recordPlatformTestExec(ctx, transaction, "insert into future_business_facts (fact_id) values ($1)", "fact_01"); err != nil {
			return err
		}
		return transaction.CompleteIdempotency(ctx, key, *claim.Owner, testStoreCanonicalRequestFingerprint(t, 0x12))
	})
	if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("RunRecordPlatformTransaction() error = %v, want ErrLostOwnerLease", err)
	}
	if tx.committed || tx.rollbackCount == 0 {
		t.Fatalf("stale completion transaction committed=%v rollbacks=%d, want rollback only", tx.committed, tx.rollbackCount)
	}
	if len(tx.querySQL) != 2 || len(tx.execSQL) != 2 {
		t.Fatalf("stale completion transaction state queries=%#v execs=%#v, want claim plus business/completion only", tx.querySQL, tx.execSQL)
	}
}

func TestRecordPlatformTransactionDoesNotExposeGenericExec(t *testing.T) {
	if _, exists := reflect.TypeOf(&RecordPlatformTransaction{}).MethodByName("Exec"); exists {
		t.Fatal("RecordPlatformTransaction must not expose a public generic Exec method")
	}
}

func TestPostgresRecordPlatformTransactionAtomicallyRunsBusinessIdempotencyAndOutbox(t *testing.T) {
	ctx := context.Background()
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from public.record_idempotency_keys"):
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "insert into public.record_idempotency_keys"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "worker_01"
				*(dest[1].(*int64)) = 1
				*(dest[2].(*time.Time)) = time.Date(2026, time.July, 24, 12, 1, 0, 0, time.UTC)
				return nil
			}}
		case strings.Contains(sql, "insert into public.record_outbox"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*int64)) = 42
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
	}
	repo := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			tx.calls = append(tx.calls, "begin")
			return tx, nil
		},
		gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error {
			tx.calls = append(tx.calls, "gate")
			return nil
		}),
	}

	err := repo.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		tx.calls = append(tx.calls, "business")
		if _, err := recordPlatformTestExec(ctx, transaction, "insert into future_business_facts (fact_id) values ($1)", "fact_01"); err != nil {
			return err
		}
		if _, err := transaction.ClaimIdempotency(ctx, recordplatform.IdempotencyClaimInputV1{
			Key:                recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "client-key.1"},
			RequestFingerprint: testStoreCanonicalRequestFingerprint(t, 0x11),
			OwnerID:            "worker_01",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          2 * time.Minute,
		}); err != nil {
			return err
		}
		event, err := transaction.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{
			Event: recordplatform.OutboxEvent{
				ProjectID:          string(recordplatform.ProjectIDDefault),
				EventKind:          "record_created",
				SubjectKind:        "record",
				SubjectID:          "rec_01",
				AuthorizationEpoch: 0,
			},
			ExpiresAfter: time.Hour,
		})
		if err != nil {
			return err
		}
		if event.Event.RowID != 42 {
			return errors.New("outbox row identity was not returned")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunRecordPlatformTransaction() error = %v", err)
	}
	if !tx.committed || tx.rollbackCount == 0 {
		t.Fatalf("transaction state committed=%v rollbacks=%d, want committed with deferred rollback", tx.committed, tx.rollbackCount)
	}
	wantPrefix := []string{"begin", "gate", "business", "exec", "gate", "query", "query", "query"}
	if len(tx.calls) < len(wantPrefix) {
		t.Fatalf("transaction calls = %#v, want %v", tx.calls, wantPrefix)
	}
	for index, want := range wantPrefix {
		if tx.calls[index] != want {
			t.Fatalf("transaction calls = %#v, call %d = %q, want %q", tx.calls, index, tx.calls[index], want)
		}
	}
	if len(tx.querySQL) != 3 {
		t.Fatalf("transaction query SQL = %#v, want idempotency select/insert and outbox insert", tx.querySQL)
	}
	if strings.Contains(strings.ToLower(tx.querySQL[2]), "body") || strings.Contains(strings.ToLower(tx.querySQL[2]), "recipient") || strings.Contains(strings.ToLower(tx.querySQL[2]), "render") {
		t.Fatalf("outbox enqueue SQL persists forbidden delivery content:\n%s", tx.querySQL[2])
	}
}

func TestPostgresRecordPlatformTransactionRollsBackCallbackAndCommitFailures(t *testing.T) {
	for _, test := range []struct {
		name      string
		callback  RecordPlatformTransactionCallback
		commitErr error
		wantErr   string
	}{
		{
			name:     "callback failure",
			callback: func(context.Context, *RecordPlatformTransaction) error { return errors.New("business fact failed") },
			wantErr:  "business fact failed",
		},
		{
			name:      "commit failure",
			callback:  func(context.Context, *RecordPlatformTransaction) error { return nil },
			commitErr: errors.New("commit failed"),
			wantErr:   "commit record platform transaction",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeRecordPlatformTx{commitErr: test.commitErr}
			repo := &PostgresRecordPlatformRepository{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
				gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
			}
			err := repo.RunRecordPlatformTransaction(context.Background(), test.callback)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("RunRecordPlatformTransaction() error = %v, want %q", err, test.wantErr)
			}
			if tx.committed || tx.rollbackCount != 1 {
				t.Fatalf("transaction state committed=%v rollbacks=%d, want no commit and one rollback", tx.committed, tx.rollbackCount)
			}
		})
	}
}

func TestPostgresRecordPlatformClaimOutboxCommitsDatabaseTimeFencedClaim(t *testing.T) {
	ownerExpiry := time.Date(2026, time.July, 24, 13, 1, 0, 0, time.UTC)
	eventExpiry := time.Date(2026, time.July, 24, 14, 0, 0, 0, time.UTC)
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		if !strings.Contains(sql, "update public.record_outbox") {
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*(dest[0].(*int64)) = 42
			*(dest[1].(*string)) = string(recordplatform.ProjectIDDefault)
			*(dest[2].(*string)) = string(recordplatform.OutboxEventKindRecordCreated)
			*(dest[3].(*string)) = string(recordplatform.OutboxSubjectKindRecord)
			*(dest[4].(*string)) = "rec_01"
			*(dest[5].(*int64)) = 0
			*(dest[6].(*int64)) = 3
			*(dest[7].(*int64)) = 0
			*(dest[8].(*string)) = "worker_01"
			*(dest[9].(*int64)) = 2
			*(dest[10].(*time.Time)) = ownerExpiry
			*(dest[11].(*time.Time)) = eventExpiry
			return nil
		}}
	}
	repo := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			tx.calls = append(tx.calls, "begin")
			return tx, nil
		},
		gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error { tx.calls = append(tx.calls, "gate"); return nil }),
	}

	claim, err := repo.ClaimOutbox(context.Background(), recordplatform.OutboxClaimInputV1{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("ClaimOutbox() error = %v", err)
	}
	if claim == nil || claim.Event.RowID != 42 || claim.Owner.Generation != 2 || claim.Event.AuthorizationEpoch != 3 || !claim.ExpiresAt.Equal(eventExpiry) {
		t.Fatalf("ClaimOutbox() = %#v, want fenced claimed event", claim)
	}
	if !tx.committed {
		t.Fatal("ClaimOutbox() returned before committing its claim transaction")
	}
	if got, want := tx.calls, []string{"begin", "gate", "query"}; len(got) < len(want) {
		t.Fatalf("claim call order = %#v, want prefix %#v", got, want)
	} else {
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("claim call order = %#v, want prefix %#v", got, want)
			}
		}
	}
	if len(tx.querySQL) != 1 {
		t.Fatalf("claim SQL = %#v, want one claim statement", tx.querySQL)
	}
	for _, fragment := range []string{
		"for update skip locked",
		"status = 'pending'",
		"status = 'processing'",
		"owner_expires_at <= transaction_timestamp()",
		"owner_generation = owner_generation + 1",
		"expires_at > transaction_timestamp() + ($2 * interval '1 microsecond')",
	} {
		if !strings.Contains(tx.querySQL[0], fragment) {
			t.Fatalf("claim SQL missing %q:\n%s", fragment, tx.querySQL[0])
		}
	}
}

func TestObservedOutboxClaimPreservesTypedSourceVersion(t *testing.T) {
	row := observedOutboxClaimRow{
		rowID: 42, projectID: "default", eventKind: recordplatform.OutboxEventKindRecordActionAssigned,
		subjectKind: "action", subjectID: "ract_version", sourceVersion: 7, authorizationEpoch: 3, recordFenceEpoch: 5,
		ownerID: "worker_01", ownerGeneration: 2,
		ownerExpiresAt: time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
		expiresAt:      time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC),
	}
	claim, err := row.claim()
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if claim.Event.SubjectID != "ract_version" || claim.Event.SourceVersion != 7 || claim.Event.RecordFenceEpoch != 5 {
		t.Fatalf("claim event = %#v, want raw subject, source version 7, and fence 5", claim.Event)
	}
}

func TestRecordPlatformTransactionAssertOutboxClaimFencesProjectionWithExactLiveOwner(t *testing.T) {
	claim := recordplatform.ClaimedOutboxEventV1{
		Event:     recordplatform.OutboxEvent{RowID: 42, ProjectID: "default", EventKind: recordplatform.OutboxEventKindRecordActionAssigned, SubjectKind: "action", SubjectID: "ract_projection", SourceVersion: 7, AuthorizationEpoch: 3},
		Owner:     recordplatform.OwnerLease{OwnerID: "notification_worker", Generation: 2, ExpiresAt: time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)},
		ExpiresAt: time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC),
	}
	for _, tt := range []struct {
		name    string
		rowErr  error
		wantErr error
	}{
		{name: "exact live owner"},
		{name: "expired or taken over", rowErr: pgx.ErrNoRows, wantErr: recordplatform.ErrLostOwnerLease},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeRecordPlatformTx{}
			tx.queryRow = func(_ context.Context, sql string, args ...any) pgx.Row {
				for _, want := range []string{"status = 'processing'", "owner_id = $2", "owner_generation = $3", "owner_expires_at = $4", "owner_expires_at > transaction_timestamp()", "for update"} {
					if !strings.Contains(sql, want) {
						t.Fatalf("assert SQL missing %q: %s", want, sql)
					}
				}
				if !reflect.DeepEqual(args, []any{claim.Event.RowID, claim.Owner.OwnerID, claim.Owner.Generation, claim.Owner.ExpiresAt}) {
					t.Fatalf("assert args = %#v", args)
				}
				return fakeRecordPlatformRow{scan: func(dest ...any) error {
					if tt.rowErr != nil {
						return tt.rowErr
					}
					*(dest[0].(*int)) = 1
					return nil
				}}
			}
			transaction := &RecordPlatformTransaction{repository: &PostgresRecordPlatformRepository{gate: allowRecordPlatformAdmissionGate}, tx: tx}
			if err := transaction.AssertOutboxClaim(context.Background(), claim); !errors.Is(err, tt.wantErr) {
				t.Fatalf("AssertOutboxClaim() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestPostgresRecordPlatformOutboxFinalizersUseOneLiveOwnerFencedWrite(t *testing.T) {
	claim := recordplatform.ClaimedOutboxEventV1{
		Event: recordplatform.OutboxEvent{
			RowID:              42,
			ProjectID:          string(recordplatform.ProjectIDDefault),
			EventKind:          string(recordplatform.OutboxEventKindRecordCreated),
			SubjectKind:        string(recordplatform.OutboxSubjectKindRecord),
			SubjectID:          "rec_01",
			AuthorizationEpoch: 3,
		},
		Owner:     recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: time.Date(2026, time.July, 24, 13, 1, 0, 0, time.UTC)},
		ExpiresAt: time.Date(2026, time.July, 24, 14, 0, 0, 0, time.UTC),
	}
	for _, test := range []struct {
		name      string
		finalize  func(*PostgresRecordPlatformRepository) error
		fragments []string
	}{
		{
			name: "cancel",
			finalize: func(repo *PostgresRecordPlatformRepository) error {
				return repo.CancelOutbox(context.Background(), claim)
			},
			fragments: []string{"status = 'cancelled'", "owner_id = ''", "owner_expires_at = null"},
		},
		{
			name: "retry",
			finalize: func(repo *PostgresRecordPlatformRepository) error {
				return repo.RetryOutbox(context.Background(), claim, time.Minute)
			},
			fragments: []string{"status = 'pending'", "next_attempt_at = transaction_timestamp() + ($4 * interval '1 microsecond')", "owner_id = ''"},
		},
		{
			name: "sent",
			finalize: func(repo *PostgresRecordPlatformRepository) error {
				return repo.MarkOutboxSent(context.Background(), claim)
			},
			fragments: []string{"status = 'sent'", "sent_at = transaction_timestamp()", "owner_id = ''"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeRecordPlatformTx{}
			repo := &PostgresRecordPlatformRepository{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
				gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { tx.calls = append(tx.calls, "gate"); return nil }),
			}
			if err := test.finalize(repo); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
				t.Fatalf("%s finalizer error = %v, want ErrLostOwnerLease", test.name, err)
			}
			if tx.execCount != 1 || len(tx.execSQL) != 1 {
				t.Fatalf("%s writes = %d SQL %#v, want one fenced write", test.name, tx.execCount, tx.execSQL)
			}
			for _, fragment := range append([]string{
				"outbox_row_id = $1",
				"status = 'processing'",
				"owner_id = $2",
				"owner_generation = $3",
				"owner_expires_at > transaction_timestamp()",
			}, test.fragments...) {
				if !strings.Contains(tx.execSQL[0], fragment) {
					t.Fatalf("%s SQL missing %q:\n%s", test.name, fragment, tx.execSQL[0])
				}
			}
			if len(tx.calls) < 2 || tx.calls[0] != "gate" || tx.calls[1] != "exec" {
				t.Fatalf("%s call order = %#v, want gate then exec", test.name, tx.calls)
			}
		})
	}
}

func TestPostgresRecordPlatformRenewedIdempotencyFencesPreRenewTokenByObservedExpiry(t *testing.T) {
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "client-key.1"}
	original := recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: time.Date(2026, time.July, 24, 13, 0, 0, 0, time.UTC)}
	currentExpiry := original.ExpiresAt
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, args ...any) pgx.Row {
		if !strings.Contains(sql, "and owner_expires_at = $") {
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("idempotency renewal did not fence the observed owner expiry") }}
		}
		observedExpiry, ok := recordPlatformOwnerExpiryArgument(args)
		if !ok {
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("idempotency renewal did not bind the observed owner expiry") }}
		}
		if !observedExpiry.Equal(currentExpiry) {
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}
		currentExpiry = currentExpiry.Add(time.Minute)
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*(dest[0].(*string)) = original.OwnerID
			*(dest[1].(*int64)) = int64(original.Generation)
			*(dest[2].(*time.Time)) = currentExpiry
			return nil
		}}
	}
	repository := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
	}
	renew := func(owner recordplatform.OwnerLease) (recordplatform.OwnerLease, error) {
		return repository.RenewIdempotency(context.Background(), recordplatform.IdempotencyRenewInputV1{
			Key:                key,
			Owner:              owner,
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          2 * time.Minute,
		})
	}

	renewed, err := renew(original)
	if err != nil {
		t.Fatalf("first RenewIdempotency() error = %v", err)
	}
	if !renewed.ExpiresAt.After(original.ExpiresAt) {
		t.Fatalf("first RenewIdempotency() owner = %#v, want a newer observed expiry", renewed)
	}
	if _, err := renew(original); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("RenewIdempotency() with pre-renew token error = %v, want ErrLostOwnerLease", err)
	}
	if _, err := renew(renewed); err != nil {
		t.Fatalf("RenewIdempotency() with renewed token error = %v", err)
	}
}

func TestPostgresRecordPlatformOutboxFinalizersBindObservedOwnerExpiry(t *testing.T) {
	claim := recordplatform.ClaimedOutboxEventV1{
		Event: recordplatform.OutboxEvent{
			RowID:              42,
			ProjectID:          string(recordplatform.ProjectIDDefault),
			EventKind:          string(recordplatform.OutboxEventKindRecordCreated),
			SubjectKind:        string(recordplatform.OutboxSubjectKindRecord),
			SubjectID:          "rec_01",
			AuthorizationEpoch: 3,
		},
		Owner:     recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: time.Date(2026, time.July, 24, 13, 1, 0, 0, time.UTC)},
		ExpiresAt: time.Date(2026, time.July, 24, 14, 0, 0, 0, time.UTC),
	}
	for _, test := range []struct {
		name     string
		finalize func(*PostgresRecordPlatformRepository) error
	}{
		{name: "cancel", finalize: func(repository *PostgresRecordPlatformRepository) error {
			return repository.CancelOutbox(context.Background(), claim)
		}},
		{name: "retry", finalize: func(repository *PostgresRecordPlatformRepository) error {
			return repository.RetryOutbox(context.Background(), claim, time.Minute)
		}},
		{name: "sent", finalize: func(repository *PostgresRecordPlatformRepository) error {
			return repository.MarkOutboxSent(context.Background(), claim)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeRecordPlatformTx{}
			repository := &PostgresRecordPlatformRepository{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
				gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
			}
			if err := test.finalize(repository); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
				t.Fatalf("finalizer error = %v, want ErrLostOwnerLease", err)
			}
			if len(tx.execSQL) != 1 || !strings.Contains(tx.execSQL[0], "and owner_expires_at = $") {
				t.Fatalf("finalizer SQL = %#v, want exact observed owner-expiry predicate", tx.execSQL)
			}
			assertRecordPlatformOwnerExpiryArgument(t, tx.execArgs[0], claim.Owner.ExpiresAt)
		})
	}
}

type fakeRecordPlatformTx struct {
	queryRow      func(context.Context, string, ...any) pgx.Row
	exec          func(context.Context, string, ...any) (pgconn.CommandTag, error)
	calls         []string
	execSQL       []string
	execArgs      [][]any
	querySQL      []string
	queryArgs     [][]any
	queryCount    int
	execCount     int
	committed     bool
	commitErr     error
	rollbackCount int
}

func (tx *fakeRecordPlatformTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *fakeRecordPlatformTx) Commit(context.Context) error {
	if tx.commitErr != nil {
		return tx.commitErr
	}
	tx.committed = true
	return nil
}
func (tx *fakeRecordPlatformTx) Rollback(context.Context) error {
	tx.rollbackCount++
	return nil
}
func (tx *fakeRecordPlatformTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *fakeRecordPlatformTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (tx *fakeRecordPlatformTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (tx *fakeRecordPlatformTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (tx *fakeRecordPlatformTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.calls = append(tx.calls, "exec")
	tx.execCount++
	tx.execSQL = append(tx.execSQL, sql)
	tx.execArgs = append(tx.execArgs, append([]any(nil), args...))
	if tx.exec != nil {
		return tx.exec(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 0"), nil
}
func (tx *fakeRecordPlatformTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (tx *fakeRecordPlatformTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.calls = append(tx.calls, "query")
	tx.queryCount++
	tx.querySQL = append(tx.querySQL, sql)
	tx.queryArgs = append(tx.queryArgs, append([]any(nil), args...))
	if tx.queryRow != nil {
		return tx.queryRow(ctx, sql, args...)
	}
	return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
}
func (tx *fakeRecordPlatformTx) Conn() *pgx.Conn { return nil }

func TestRecordPlatformFingerprintUsesOnlyTrustedExactPersistedReadback(t *testing.T) {
	original := testStoreCanonicalRequestFingerprint(t, 0x51)
	persisted, err := original.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	restored, err := recordPlatformFingerprint(persisted[:])
	if err != nil {
		t.Fatalf("recordPlatformFingerprint() error = %v", err)
	}
	if err := restored.Validate(); err != nil {
		t.Fatalf("recordPlatformFingerprint() returned invalid value: %v", err)
	}
	if reflect.TypeOf(restored) == reflect.TypeOf(original) {
		t.Fatal("recordPlatformFingerprint() returned the canonical write-capable fingerprint type")
	}
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "client-key.1"}
	owner := recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 1, ExpiresAt: now.Add(time.Minute)}
	_, err = recordplatform.ResolveIdempotencyV1(recordplatform.IdempotencyRecordV1{
		Key:                key,
		RequestFingerprint: restored,
		Status:             recordplatform.IdempotencyStatusInProgress,
		Owner:              &owner,
		ExpiresAt:          now.Add(2 * time.Minute),
	}, original, now)
	if !errors.Is(err, recordplatform.ErrIdempotencyInProgress) {
		t.Fatalf("ResolveIdempotencyV1() with persisted readback error = %v, want ErrIdempotencyInProgress", err)
	}

	for _, raw := range [][]byte{nil, make([]byte, 31), make([]byte, 33)} {
		if _, err := recordPlatformFingerprint(raw); !errors.Is(err, recordplatform.ErrInvalidIdempotencyRecord) {
			t.Fatalf("recordPlatformFingerprint(%d bytes) error = %v, want ErrInvalidIdempotencyRecord", len(raw), err)
		}
	}
}

type fakeRecordPlatformRow struct {
	scan func(...any) error
}

// recordPlatformTestExec models a future same-package named store method in
// atomicity tests without exposing arbitrary SQL to transaction callbacks.
func recordPlatformTestExec(ctx context.Context, transaction *RecordPlatformTransaction, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return transaction.tx.Exec(ctx, sql, arguments...)
}

func (row fakeRecordPlatformRow) Scan(dest ...any) error {
	return row.scan(dest...)
}

type recordPlatformTestFreshOutboxAuthorizer func(context.Context, recordplatform.OutboxEvent) (recordplatform.RenderedDelivery, recordplatform.FreshAuthDecision, error)

func (authorizer recordPlatformTestFreshOutboxAuthorizer) AuthorizeAndRender(ctx context.Context, event recordplatform.OutboxEvent) (recordplatform.RenderedDelivery, recordplatform.FreshAuthDecision, error) {
	return authorizer(ctx, event)
}

type recordPlatformTestOutboxSender func(context.Context, recordplatform.RenderedDelivery) error

func (sender recordPlatformTestOutboxSender) SendOutbox(ctx context.Context, delivery recordplatform.RenderedDelivery) error {
	return sender(ctx, delivery)
}

type recordPlatformTypedNilAdmissionGate struct{}

type recordPlatformTypedNilAdmissionGateCallKey struct{}

func (*recordPlatformTypedNilAdmissionGate) Admit(ctx context.Context, _ pgx.Tx) error {
	if calls, ok := ctx.Value(recordPlatformTypedNilAdmissionGateCallKey{}).(*int); ok {
		(*calls)++
	}
	return nil
}

func testStoreRecordPlatformDigest(value byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = value
	}
	return digest
}

func testStoreCanonicalRequestFingerprint(t *testing.T, payloadDigest byte) recordplatform.RequestFingerprintV1 {
	t.Helper()
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordCreate,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   testStoreRecordPlatformDigest(0xa1),
		RequestScopeDigest: testStoreRecordPlatformDigest(0xb2),
		PayloadDigest:      testStoreRecordPlatformDigest(payloadDigest),
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	return fingerprint
}

func countRecordPlatformTestCalls(calls []string, want string) int {
	count := 0
	for _, call := range calls {
		if call == want {
			count++
		}
	}
	return count
}

func recordPlatformOwnerExpiryArgument(arguments []any) (time.Time, bool) {
	for _, argument := range arguments {
		if value, ok := argument.(time.Time); ok {
			return value, true
		}
	}
	return time.Time{}, false
}

func assertRecordPlatformOwnerExpiryArgument(t *testing.T, arguments []any, want time.Time) {
	t.Helper()
	got, ok := recordPlatformOwnerExpiryArgument(arguments)
	if !ok || !got.Equal(want) {
		t.Fatalf("owner-expiry arguments = %#v, want %s", arguments, want)
	}
}
