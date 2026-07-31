package migrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAdmitAppACLR1OnlyRuntimeRejectsEveryNonR1State(t *testing.T) {
	tests := []struct {
		name  string
		state AppACLR2State
	}{
		{name: "prepared", state: AppACLR2StatePrepared},
		{name: "finalized", state: AppACLR2StateFinalized},
		{name: "corrupt", state: AppACLR2StateCorrupt},
		{name: "unknown", state: AppACLR2State(255)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trace := make([]string, 0, 4)
			tx := &fakeAppACLR2RuntimeAdmissionTx{}
			_, err := admitAppACLR1OnlyRuntimeWithDependencies(context.Background(), oneAppACLR2RuntimeAdmissionTx(tx), newAppACLR2RuntimeAdmissionTraceDependencies(&trace, tt.state))
			if err == nil {
				t.Fatalf("admitAppACLR1OnlyRuntimeWithDependencies() error = nil for %v, want rejection", tt.state)
			}
			assertAppACLR2RuntimeAdmissionTrace(t, trace, "classify")
			if tx.commitCalls != 0 || tx.rollbackCalls != 1 {
				t.Fatalf("transaction lifecycle = commit %d rollback %d, want 0/1", tx.commitCalls, tx.rollbackCalls)
			}
		})
	}
}

func TestAdmitAppACLR2RuntimeRoutesOnlyExactStates(t *testing.T) {
	tests := []struct {
		name      string
		state     AppACLR2State
		wantError bool
		wantTrace []string
	}{
		{name: "exact R1", state: AppACLR2StateR1, wantTrace: []string{"classify", "verify-frozen", "require-direct-runtime"}},
		{name: "prepared", state: AppACLR2StatePrepared, wantError: true, wantTrace: []string{"classify"}},
		{name: "finalized", state: AppACLR2StateFinalized, wantTrace: []string{"classify"}},
		{name: "corrupt", state: AppACLR2StateCorrupt, wantError: true, wantTrace: []string{"classify"}},
		{name: "unknown", state: AppACLR2State(255), wantError: true, wantTrace: []string{"classify"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trace := make([]string, 0, 4)
			tx := &fakeAppACLR2RuntimeAdmissionTx{}
			got, err := admitAppACLR2RuntimeWithDependencies(context.Background(), oneAppACLR2RuntimeAdmissionTx(tx), newAppACLR2RuntimeAdmissionTraceDependencies(&trace, tt.state))
			if tt.wantError {
				if err == nil {
					t.Fatalf("admitAppACLR2RuntimeWithDependencies() error = nil for %v, want rejection", tt.state)
				}
				if tx.commitCalls != 0 || tx.rollbackCalls != 1 {
					t.Fatalf("transaction lifecycle = commit %d rollback %d, want 0/1", tx.commitCalls, tx.rollbackCalls)
				}
			} else {
				if err != nil {
					t.Fatalf("admitAppACLR2RuntimeWithDependencies() error = %v", err)
				}
				if got != tt.state {
					t.Fatalf("admitted state = %v, want %v", got, tt.state)
				}
				if tx.commitCalls != 1 || tx.rollbackCalls != 1 {
					t.Fatalf("transaction lifecycle = commit %d rollback %d, want 1/1", tx.commitCalls, tx.rollbackCalls)
				}
			}
			assertAppACLR2RuntimeAdmissionTrace(t, trace, tt.wantTrace...)
		})
	}
}

func TestStartAppACLR2RuntimeUsesTheR2AdmissionRoute(t *testing.T) {
	tests := []struct {
		name      string
		state     AppACLR2State
		wantError bool
		wantTrace []string
	}{
		{name: "R1", state: AppACLR2StateR1, wantTrace: []string{"classify", "verify-frozen", "require-direct-runtime"}},
		{name: "prepared", state: AppACLR2StatePrepared, wantError: true, wantTrace: []string{"classify"}},
		{name: "finalized", state: AppACLR2StateFinalized, wantTrace: []string{"classify"}},
		{name: "corrupt", state: AppACLR2StateCorrupt, wantError: true, wantTrace: []string{"classify"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trace := make([]string, 0, 4)
			tx := &fakeAppACLR2RuntimeAdmissionTx{}
			got, err := startAppACLR2RuntimeWithDependencies(context.Background(), oneAppACLR2RuntimeAdmissionTx(tx), newAppACLR2RuntimeAdmissionTraceDependencies(&trace, tt.state))
			if tt.wantError {
				if err == nil {
					t.Fatalf("startAppACLR2RuntimeWithDependencies() error = nil for %v, want rejection", tt.state)
				}
			} else {
				if err != nil {
					t.Fatalf("startAppACLR2RuntimeWithDependencies() error = %v", err)
				}
				if got != tt.state {
					t.Fatalf("started state = %v, want %v", got, tt.state)
				}
			}
			assertAppACLR2RuntimeAdmissionTrace(t, trace, tt.wantTrace...)
		})
	}
}

func TestAdmitAppACLR2RuntimeRejectsR1RuntimeIdentityMismatch(t *testing.T) {
	trace := make([]string, 0, 3)
	tx := &fakeAppACLR2RuntimeAdmissionIdentityTx{
		sessionUser: "member_login",
		currentUser: "center_runtime",
	}
	dependencies := newAppACLR2RuntimeAdmissionTraceDependencies(&trace, AppACLR2StateR1)
	dependencies.requireDirectRuntime = func(ctx context.Context, gotTx pgx.Tx, state FrozenAppACLR1StateV1) error {
		trace = append(trace, "require-direct-runtime")
		return RequireDirectFrozenAppACLR1RuntimeInTx(ctx, gotTx, state)
	}

	_, err := admitAppACLR2RuntimeWithDependencies(context.Background(), oneAppACLR2RuntimeAdmissionTx(tx), dependencies)
	if err == nil || !strings.Contains(err.Error(), "requires direct role") {
		t.Fatalf("R1 runtime identity mismatch error = %v, want direct-role rejection", err)
	}
	assertAppACLR2RuntimeAdmissionTrace(t, trace, "classify", "verify-frozen", "require-direct-runtime")
	if tx.commitCalls != 0 || tx.rollbackCalls != 1 {
		t.Fatalf("transaction lifecycle = commit %d rollback %d, want 0/1", tx.commitCalls, tx.rollbackCalls)
	}
}

func TestAppACLR2RuntimeAdmissionUsesOneSharedLockedRepeatableReadOnlySnapshot(t *testing.T) {
	tests := []struct {
		name      string
		state     AppACLR2State
		wantTrace []string
	}{
		{
			name:      "exact R1",
			state:     AppACLR2StateR1,
			wantTrace: []string{"shared-lock", "begin", "classify", "verify-frozen", "require-direct-runtime", "commit", "shared-unlock", "release"},
		},
		{
			name:      "finalized R2",
			state:     AppACLR2StateFinalized,
			wantTrace: []string{"shared-lock", "begin", "classify", "commit", "shared-unlock", "release"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trace := make([]string, 0, len(tt.wantTrace))
			tx := &fakeAppACLR2RuntimeAdmissionTx{trace: &trace}
			conn := &fakeAppACLR2RuntimeAdmissionReservedConn{trace: &trace, tx: tx}
			begin := newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
				return conn, nil
			})
			var classifiedTx, verifiedTx, requiredTx pgx.Tx
			dependencies := newAppACLR2RuntimeAdmissionTraceDependencies(&trace, tt.state)
			dependencies.classify = func(_ context.Context, gotTx pgx.Tx) (AppACLR2State, error) {
				classifiedTx = gotTx
				trace = append(trace, "classify")
				return tt.state, nil
			}
			dependencies.verifyFrozen = func(_ context.Context, gotTx pgx.Tx) (FrozenAppACLR1StateV1, error) {
				verifiedTx = gotTx
				trace = append(trace, "verify-frozen")
				return FrozenAppACLR1StateV1{CenterRuntimeRole: "center_runtime"}, nil
			}
			dependencies.requireDirectRuntime = func(_ context.Context, gotTx pgx.Tx, _ FrozenAppACLR1StateV1) error {
				requiredTx = gotTx
				trace = append(trace, "require-direct-runtime")
				return nil
			}
			got, err := admitAppACLR2RuntimeWithDependencies(context.Background(), begin, dependencies)
			if err != nil {
				t.Fatalf("admitAppACLR2RuntimeWithDependencies() error = %v", err)
			}
			if got != tt.state {
				t.Fatalf("admitted state = %v, want %v", got, tt.state)
			}
			if len(conn.beginOptions) != 1 {
				t.Fatalf("transaction begins = %d, want 1", len(conn.beginOptions))
			}
			if got := conn.beginOptions[0]; got.IsoLevel != pgx.RepeatableRead || got.AccessMode != pgx.ReadOnly {
				t.Fatalf("transaction options = %#v, want REPEATABLE READ READ ONLY", got)
			}
			if classifiedTx == nil {
				t.Fatal("classifier did not receive the locked admission transaction")
			}
			if tt.state == AppACLR2StateR1 && (verifiedTx != classifiedTx || requiredTx != classifiedTx) {
				t.Fatalf("R1 classifier/verifier/predicate transactions = %p/%p/%p, want the one locked transaction", classifiedTx, verifiedTx, requiredTx)
			}
			if tt.state == AppACLR2StateFinalized && (verifiedTx != nil || requiredTx != nil) {
				t.Fatalf("FINALIZED verifier/predicate transactions = %p/%p, want none", verifiedTx, requiredTx)
			}
			assertAppACLR2RuntimeAdmissionTrace(t, trace, tt.wantTrace...)
		})
	}
}

func TestAdmitAppACLR2RuntimeRejectsR1ToPreparedRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sharedReleased := make(chan struct{})
	classifiedR1 := make(chan struct{})
	allowFrozenVerification := make(chan struct{})
	bootstrapExclusiveAttempted := make(chan struct{})
	bootstrapExclusiveAcquired := make(chan struct{})
	bootstrapPreparedCommitted := make(chan struct{}, 1)

	admissionTx := &fakeAppACLR2RuntimeAdmissionTx{}
	admissionConn := &fakeAppACLR2RuntimeAdmissionRaceAdmissionConn{
		tx:             admissionTx,
		sharedReleased: sharedReleased,
	}
	admissionBegin := newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
		return admissionConn, nil
	})
	admissionTrace := make([]string, 0, 3)
	admissionDependencies := newAppACLR2RuntimeAdmissionTraceDependencies(&admissionTrace, AppACLR2StateR1)
	admissionDependencies.classify = func(context.Context, pgx.Tx) (AppACLR2State, error) {
		admissionTrace = append(admissionTrace, "classify")
		close(classifiedR1)
		return AppACLR2StateR1, nil
	}
	admissionDependencies.verifyFrozen = func(context.Context, pgx.Tx) (FrozenAppACLR1StateV1, error) {
		admissionTrace = append(admissionTrace, "verify-frozen")
		select {
		case <-allowFrozenVerification:
			return FrozenAppACLR1StateV1{CenterRuntimeRole: "center_runtime"}, nil
		case <-ctx.Done():
			return FrozenAppACLR1StateV1{}, ctx.Err()
		}
	}

	admissionDone := make(chan error, 1)
	go func() {
		_, err := admitAppACLR2RuntimeWithDependencies(ctx, admissionBegin, admissionDependencies)
		admissionDone <- err
	}()

	select {
	case <-classifiedR1:
	case <-ctx.Done():
		t.Fatalf("R1 classification did not reach the pause: %v", ctx.Err())
	}

	bootstrapTx := &fakeAppACLR2RuntimeAdmissionRaceBootstrapTx{preparedCommitted: bootstrapPreparedCommitted}
	bootstrapConn := &fakeAppACLR2RuntimeAdmissionRaceBootstrapConn{
		tx:                 bootstrapTx,
		sharedReleased:     sharedReleased,
		exclusiveAttempted: bootstrapExclusiveAttempted,
		exclusiveAcquired:  bootstrapExclusiveAcquired,
	}
	bootstrapBegin := newAppACLR2BootstrapTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
		return bootstrapConn, nil
	})
	bootstrapDone := make(chan error, 1)
	go func() {
		tx, err := bootstrapBegin(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err == nil {
			err = tx.Commit(ctx)
		}
		bootstrapDone <- err
	}()

	select {
	case <-bootstrapExclusiveAttempted:
	case <-ctx.Done():
		t.Fatalf("bootstrap did not attempt its exclusive transition lock: %v", ctx.Err())
	}
	select {
	case <-bootstrapExclusiveAcquired:
		t.Fatal("bootstrap acquired the exclusive transition lock while R1 admission held the shared lock")
	default:
	}
	if bootstrapTx.commitCalls != 0 {
		t.Fatalf("bootstrap commit calls = %d while shared lock is held, want 0", bootstrapTx.commitCalls)
	}

	close(allowFrozenVerification)
	select {
	case err := <-admissionDone:
		if err != nil {
			t.Fatalf("R1 admission error = %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("R1 admission did not complete: %v", ctx.Err())
	}
	select {
	case <-bootstrapExclusiveAcquired:
	case <-ctx.Done():
		t.Fatalf("bootstrap did not acquire the exclusive transition lock after admission completed: %v", ctx.Err())
	}
	select {
	case err := <-bootstrapDone:
		if err != nil {
			t.Fatalf("bootstrap transition error = %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("bootstrap did not commit PREPARED after admission completed: %v", ctx.Err())
	}
	select {
	case <-bootstrapPreparedCommitted:
	case <-ctx.Done():
		t.Fatalf("bootstrap did not report its PREPARED commit: %v", ctx.Err())
	}
	assertAppACLR2RuntimeAdmissionTrace(t, admissionTrace, "classify", "verify-frozen", "require-direct-runtime")

	preparedTrace := make([]string, 0, 1)
	preparedTx := &fakeAppACLR2RuntimeAdmissionTx{}
	_, err := admitAppACLR2RuntimeWithDependencies(context.Background(), oneAppACLR2RuntimeAdmissionTx(preparedTx), newAppACLR2RuntimeAdmissionTraceDependencies(&preparedTrace, AppACLR2StatePrepared))
	if err == nil {
		t.Fatal("fresh admission after PREPARED commit error = nil, want rejection")
	}
	assertAppACLR2RuntimeAdmissionTrace(t, preparedTrace, "classify")
}

func TestAdmitAppACLR2RuntimePreparedClassificationDoesNotInvokeFrozenOrR2Admission(t *testing.T) {
	trace := make([]string, 0, 1)
	tx := &fakeAppACLR2RuntimeAdmissionTx{}
	_, err := admitAppACLR2RuntimeWithDependencies(context.Background(), oneAppACLR2RuntimeAdmissionTx(tx), newAppACLR2RuntimeAdmissionTraceDependencies(&trace, AppACLR2StatePrepared))
	if err == nil {
		t.Fatal("prepared admission error = nil, want rejection")
	}
	assertAppACLR2RuntimeAdmissionTrace(t, trace, "classify")

	source, readErr := os.ReadFile("app_acl_r2_runtime_admission.go")
	if readErr != nil {
		t.Fatalf("read runtime admission source: %v", readErr)
	}
	for _, forbidden := range []string{
		"AdmitAppACLRuntime(",
		"ParseCanonicalAppACLPrivilegeSetR2BodyV1(",
		"CanonicalAppACLR2BootstrapReceiptBodyV1(",
		"CanonicalAppACLManifestR2BodyV1(",
		"ReadAppACLR2ReceiptCatalogSnapshotInTx(",
		"readAppACLR2Manifest",
	} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("runtime admission source contains forbidden PREPARED path dependency %q", forbidden)
		}
	}
}

func TestAppACLR2RuntimeAdmissionLeavesFrozenR1AndGenericMigrationClosed(t *testing.T) {
	for _, name := range []string{
		"app_acl_runtime_admission.go",
		"app_acl_convergence.go",
		"migrate.go",
	} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read frozen source %q: %v", name, err)
		}
		for _, forbidden := range []string{"AppACLR2", "app_acl_r2"} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("frozen source %q contains R2 dependency %q", name, forbidden)
			}
		}
	}
	if _, err := os.Stat("app_acl_r2_dispatch.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("R2 dispatch file stat error = %v, want no dispatch file", err)
	}
}

func TestAppACLR2RuntimeAdmissionPublicEntriesRejectNilPool(t *testing.T) {
	if err := AdmitAppACLR1OnlyRuntime(context.Background(), nil); err == nil {
		t.Fatal("AdmitAppACLR1OnlyRuntime(nil) error = nil, want rejection")
	}
	if _, err := AdmitAppACLR2Runtime(context.Background(), nil); err == nil {
		t.Fatal("AdmitAppACLR2Runtime(nil) error = nil, want rejection")
	}
	if _, err := StartAppACLR2Runtime(context.Background(), nil); err == nil {
		t.Fatal("StartAppACLR2Runtime(nil) error = nil, want rejection")
	}
}

func newAppACLR2RuntimeAdmissionTraceDependencies(trace *[]string, state AppACLR2State) appACLR2RuntimeAdmissionDependencies {
	return appACLR2RuntimeAdmissionDependencies{
		classify: func(_ context.Context, _ pgx.Tx) (AppACLR2State, error) {
			*trace = append(*trace, "classify")
			return state, nil
		},
		verifyFrozen: func(_ context.Context, _ pgx.Tx) (FrozenAppACLR1StateV1, error) {
			*trace = append(*trace, "verify-frozen")
			return FrozenAppACLR1StateV1{CenterRuntimeRole: "center_runtime"}, nil
		},
		requireDirectRuntime: func(_ context.Context, _ pgx.Tx, _ FrozenAppACLR1StateV1) error {
			*trace = append(*trace, "require-direct-runtime")
			return nil
		},
	}
}

func oneAppACLR2RuntimeAdmissionTx(tx pgx.Tx) appACLR2RuntimeAdmissionBeginTx {
	return func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		return tx, nil
	}
}

func assertAppACLR2RuntimeAdmissionTrace(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("runtime admission trace = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("runtime admission trace = %#v, want %#v", got, want)
		}
	}
}

type fakeAppACLR2RuntimeAdmissionTx struct {
	pgx.Tx
	trace         *[]string
	commitCalls   int
	rollbackCalls int
}

func (tx *fakeAppACLR2RuntimeAdmissionTx) Commit(context.Context) error {
	tx.commitCalls++
	if tx.trace != nil {
		*tx.trace = append(*tx.trace, "commit")
	}
	return nil
}

func (tx *fakeAppACLR2RuntimeAdmissionTx) Rollback(context.Context) error {
	tx.rollbackCalls++
	if tx.trace != nil {
		*tx.trace = append(*tx.trace, "rollback")
	}
	return nil
}

type fakeAppACLR2RuntimeAdmissionIdentityTx struct {
	fakeAppACLR2RuntimeAdmissionTx
	sessionUser string
	currentUser string
}

func (tx *fakeAppACLR2RuntimeAdmissionIdentityTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return fakeAppACLR2RuntimeAdmissionIdentityRow{
		sessionUser: tx.sessionUser,
		currentUser: tx.currentUser,
	}
}

type fakeAppACLR2RuntimeAdmissionIdentityRow struct {
	sessionUser string
	currentUser string
}

func (row fakeAppACLR2RuntimeAdmissionIdentityRow) Scan(destinations ...any) error {
	if len(destinations) != 2 {
		return fmt.Errorf("identity row destination count = %d", len(destinations))
	}
	for index, value := range []string{row.sessionUser, row.currentUser} {
		destination, ok := destinations[index].(*string)
		if !ok {
			return fmt.Errorf("identity row destination %d has type %T", index, destinations[index])
		}
		*destination = value
	}
	return nil
}

type fakeAppACLR2RuntimeAdmissionReservedConn struct {
	trace        *[]string
	tx           pgx.Tx
	beginOptions []pgx.TxOptions
	releases     int
	discards     int
}

func (conn *fakeAppACLR2RuntimeAdmissionReservedConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if sql != appACLR2RuntimeAdmissionSessionSharedTransitionLockSQL {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected runtime reserved-connection SQL %q", sql)
	}
	if conn.trace != nil {
		*conn.trace = append(*conn.trace, "shared-lock")
	}
	return pgconn.CommandTag{}, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionReservedConn) BeginTx(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	conn.beginOptions = append(conn.beginOptions, options)
	if conn.trace != nil {
		*conn.trace = append(*conn.trace, "begin")
	}
	return conn.tx, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionReservedConn) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if sql != appACLR2RuntimeAdmissionSessionSharedTransitionUnlockSQL {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: fmt.Errorf("unexpected runtime reserved-connection query %q", sql)}
	}
	if conn.trace != nil {
		*conn.trace = append(*conn.trace, "shared-unlock")
	}
	return fakeAppACLR2RuntimeAdmissionBoolRow(true)
}

func (conn *fakeAppACLR2RuntimeAdmissionReservedConn) Release() {
	conn.releases++
	if conn.trace != nil {
		*conn.trace = append(*conn.trace, "release")
	}
}

func (conn *fakeAppACLR2RuntimeAdmissionReservedConn) Discard(context.Context) error {
	conn.discards++
	return nil
}

type fakeAppACLR2RuntimeAdmissionRaceAdmissionConn struct {
	tx             pgx.Tx
	sharedReleased chan struct{}
	sharedOnce     sync.Once
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if sql != appACLR2RuntimeAdmissionSessionSharedTransitionLockSQL {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected runtime race admission SQL %q", sql)
	}
	return pgconn.CommandTag{}, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return conn.tx, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if sql != appACLR2RuntimeAdmissionSessionSharedTransitionUnlockSQL {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: fmt.Errorf("unexpected runtime race admission unlock query %q", sql)}
	}
	conn.sharedOnce.Do(func() { close(conn.sharedReleased) })
	return fakeAppACLR2RuntimeAdmissionBoolRow(true)
}

func (*fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) Release() {}

func (*fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) Discard(context.Context) error { return nil }

type fakeAppACLR2RuntimeAdmissionRaceBootstrapConn struct {
	tx                 pgx.Tx
	sharedReleased     <-chan struct{}
	exclusiveAttempted chan<- struct{}
	exclusiveAcquired  chan<- struct{}
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) Exec(ctx context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if sql != appACLR2BootstrapSessionTransitionLockSQL {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected bootstrap race connection SQL %q", sql)
	}
	conn.exclusiveAttempted <- struct{}{}
	select {
	case <-conn.sharedReleased:
		conn.exclusiveAcquired <- struct{}{}
		return pgconn.CommandTag{}, nil
	case <-ctx.Done():
		return pgconn.CommandTag{}, ctx.Err()
	}
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return conn.tx, nil
}

func (*fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeAppACLR2RuntimeAdmissionBoolRow(true)
}

func (*fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) Release() {}

func (*fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) Discard(context.Context) error { return nil }

type fakeAppACLR2RuntimeAdmissionRaceBootstrapTx struct {
	pgx.Tx
	preparedCommitted chan<- struct{}
	commitCalls       int
}

func (tx *fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if sql != appACLR2BootstrapTransactionTransitionLockSQL {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected bootstrap race transaction SQL %q", sql)
	}
	return pgconn.CommandTag{}, nil
}

func (tx *fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if sql != appACLR2BootstrapSessionTransitionUnlockSQL {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: fmt.Errorf("unexpected bootstrap race transaction query %q", sql)}
	}
	return fakeAppACLR2RuntimeAdmissionBoolRow(true)
}

func (tx *fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) Commit(context.Context) error {
	tx.commitCalls++
	tx.preparedCommitted <- struct{}{}
	return nil
}

func (*fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) Rollback(context.Context) error { return nil }

type fakeAppACLR2RuntimeAdmissionErrorRow struct{ err error }

func (row fakeAppACLR2RuntimeAdmissionErrorRow) Scan(...any) error { return row.err }

type fakeAppACLR2RuntimeAdmissionBoolRow bool

func (row fakeAppACLR2RuntimeAdmissionBoolRow) Scan(destinations ...any) error {
	if len(destinations) != 1 {
		return fmt.Errorf("boolean row destination count = %d", len(destinations))
	}
	destination, ok := destinations[0].(*bool)
	if !ok {
		return fmt.Errorf("boolean row destination has type %T", destinations[0])
	}
	*destination = bool(row)
	return nil
}
