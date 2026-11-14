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

func TestRecordPlatformTransactionDoesNotExposeGenericExec(t *testing.T) {
	if _, exists := reflect.TypeOf(&RecordPlatformTransaction{}).MethodByName("Exec"); exists {
		t.Fatal("RecordPlatformTransaction must not expose a public generic Exec method")
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
