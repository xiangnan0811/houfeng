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

	classifiedR1 := make(chan struct{})
	allowFrozenVerification := make(chan struct{})
	bootstrapExclusiveAttempted := make(chan struct{}, 1)
	bootstrapExclusiveAcquired := make(chan struct{}, 1)
	bootstrapPreparedCommitted := make(chan struct{}, 1)
	locks := newAppACLR2RuntimeAdmissionRaceLockManager()

	admissionTx := &fakeAppACLR2RuntimeAdmissionTx{}
	admissionConn := &fakeAppACLR2RuntimeAdmissionRaceAdmissionConn{
		tx:    admissionTx,
		locks: locks,
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
	case err := <-admissionDone:
		t.Fatalf("R1 admission failed before classifying under the shared transition lock: %v", err)
	case <-ctx.Done():
		t.Fatalf("R1 classification did not reach the pause: %v", ctx.Err())
	}

	bootstrapLocks := &appACLR2RuntimeAdmissionRaceBootstrapLockState{locks: locks}
	bootstrapTx := &fakeAppACLR2RuntimeAdmissionRaceBootstrapTx{
		locks:             bootstrapLocks,
		preparedCommitted: bootstrapPreparedCommitted,
	}
	bootstrapConn := &fakeAppACLR2RuntimeAdmissionRaceBootstrapConn{
		tx:                 bootstrapTx,
		locks:              bootstrapLocks,
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
	if got := bootstrapTx.CommitCalls(); got != 0 {
		t.Fatalf("bootstrap commit calls = %d while shared lock is held, want 0", got)
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

func TestAppACLR2RuntimeAdmissionLockBeginFaultsReleaseConnection(t *testing.T) {
	beginErr := errors.New("begin transaction failed")
	tests := []struct {
		name     string
		beginErr error
		wantIs   error
		wantText string
	}{
		{name: "BeginTx error", beginErr: beginErr, wantIs: beginErr},
		{name: "nil transaction", wantText: "returned nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &fakeAppACLR2RuntimeAdmissionFaultConn{
				beginErr:     tt.beginErr,
				unlockResult: true,
			}
			begin := newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
				return conn, nil
			})

			gotTx, err := begin(context.Background(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if gotTx != nil {
				t.Fatalf("locked begin transaction = %v, want nil", gotTx)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Fatalf("locked begin error = %v, want wrapped %v", err, tt.wantIs)
			}
			if tt.wantText != "" && !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("locked begin error = %v, want text %q", err, tt.wantText)
			}
			assertAppACLR2RuntimeAdmissionConnectionReleased(t, conn)
			if conn.lockCalls != 1 || conn.beginCalls != 1 || conn.unlockCalls != 1 {
				t.Fatalf("locked begin calls = lock %d begin %d unlock %d, want 1/1/1", conn.lockCalls, conn.beginCalls, conn.unlockCalls)
			}
		})
	}
}

func TestAppACLR2RuntimeAdmissionCommitFaultDiscardsConnection(t *testing.T) {
	commitErr := errors.New("commit failed")
	tx := &fakeAppACLR2RuntimeAdmissionFaultTx{commitErr: commitErr}
	conn := &fakeAppACLR2RuntimeAdmissionFaultConn{tx: tx, unlockResult: true}
	trace := make([]string, 0, 1)

	_, err := admitAppACLR2RuntimeWithDependencies(
		context.Background(),
		newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) { return conn, nil }),
		newAppACLR2RuntimeAdmissionTraceDependencies(&trace, AppACLR2StateFinalized),
	)
	if !errors.Is(err, commitErr) {
		t.Fatalf("runtime admission commit error = %v, want wrapped %v", err, commitErr)
	}
	assertAppACLR2RuntimeAdmissionConnectionDiscarded(t, conn)
	if tx.commitCalls != 1 || tx.rollbackCalls != 0 || conn.unlockCalls != 0 {
		t.Fatalf("commit-fault cleanup = commit %d rollback %d unlock %d, want 1/0/0", tx.commitCalls, tx.rollbackCalls, conn.unlockCalls)
	}
}

func TestAppACLR2RuntimeAdmissionRollbackFaultDiscardsConnection(t *testing.T) {
	classifyErr := errors.New("classification failed")
	rollbackErr := errors.New("rollback failed")
	tx := &fakeAppACLR2RuntimeAdmissionFaultTx{rollbackErr: rollbackErr}
	conn := &fakeAppACLR2RuntimeAdmissionFaultConn{tx: tx, unlockResult: true}
	trace := make([]string, 0, 1)
	dependencies := newAppACLR2RuntimeAdmissionTraceDependencies(&trace, AppACLR2StateFinalized)
	dependencies.classify = func(context.Context, pgx.Tx) (AppACLR2State, error) {
		return AppACLR2StateCorrupt, classifyErr
	}

	_, err := admitAppACLR2RuntimeWithDependencies(
		context.Background(),
		newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) { return conn, nil }),
		dependencies,
	)
	if !errors.Is(err, classifyErr) {
		t.Fatalf("runtime admission classification error = %v, want wrapped %v", err, classifyErr)
	}
	assertAppACLR2RuntimeAdmissionConnectionDiscarded(t, conn)
	if tx.commitCalls != 0 || tx.rollbackCalls != 1 || conn.unlockCalls != 0 {
		t.Fatalf("rollback-fault cleanup = commit %d rollback %d unlock %d, want 0/1/0", tx.commitCalls, tx.rollbackCalls, conn.unlockCalls)
	}
}

func TestAppACLR2RuntimeAdmissionUnlockFaultsDiscardConnection(t *testing.T) {
	unlockErr := errors.New("unlock query failed")
	tests := []struct {
		name         string
		unlockErr    error
		unlockResult bool
		wantIs       error
		wantText     string
	}{
		{name: "unlock query error", unlockErr: unlockErr, wantIs: unlockErr},
		{name: "unlock false", unlockResult: false, wantText: "was not held"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeAppACLR2RuntimeAdmissionFaultTx{}
			conn := &fakeAppACLR2RuntimeAdmissionFaultConn{
				tx:           tx,
				unlockErr:    tt.unlockErr,
				unlockResult: tt.unlockResult,
			}
			trace := make([]string, 0, 1)
			_, err := admitAppACLR2RuntimeWithDependencies(
				context.Background(),
				newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) { return conn, nil }),
				newAppACLR2RuntimeAdmissionTraceDependencies(&trace, AppACLR2StateFinalized),
			)
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Fatalf("runtime admission unlock error = %v, want wrapped %v", err, tt.wantIs)
			}
			if tt.wantText != "" && !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("runtime admission unlock error = %v, want text %q", err, tt.wantText)
			}
			assertAppACLR2RuntimeAdmissionConnectionDiscarded(t, conn)
			if tx.commitCalls != 1 || tx.rollbackCalls != 0 || conn.unlockCalls != 1 {
				t.Fatalf("unlock-fault cleanup = commit %d rollback %d unlock %d, want 1/0/1", tx.commitCalls, tx.rollbackCalls, conn.unlockCalls)
			}
		})
	}
}

func TestAppACLR2RuntimeAdmissionCancelDiscardsWithFreshBoundedContext(t *testing.T) {
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx := &fakeAppACLR2RuntimeAdmissionFaultTx{commitReturnsContextError: true}
	conn := &fakeAppACLR2RuntimeAdmissionFaultConn{tx: tx, unlockResult: true}
	trace := make([]string, 0, 1)
	dependencies := newAppACLR2RuntimeAdmissionTraceDependencies(&trace, AppACLR2StateFinalized)
	dependencies.classify = func(context.Context, pgx.Tx) (AppACLR2State, error) {
		cancel()
		return AppACLR2StateFinalized, nil
	}

	_, err := admitAppACLR2RuntimeWithDependencies(
		requestCtx,
		newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) { return conn, nil }),
		dependencies,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runtime admission cancellation error = %v, want context cancellation", err)
	}
	assertAppACLR2RuntimeAdmissionConnectionDiscarded(t, conn)
	if conn.discardContext == requestCtx {
		t.Fatal("discard received the canceled request context, want a fresh cleanup context")
	}
	if tx.commitCalls != 1 || tx.rollbackCalls != 0 || !errors.Is(tx.commitContextErr, context.Canceled) {
		t.Fatalf("cancellation cleanup = commit %d rollback %d commit context error %v, want 1/0/canceled", tx.commitCalls, tx.rollbackCalls, tx.commitContextErr)
	}
}

func TestAppACLR2RuntimeAdmissionFinishSuccessReleasesConnection(t *testing.T) {
	tests := []struct {
		name              string
		finish            func(pgx.Tx, context.Context) error
		wantCommitCalls   int
		wantRollbackCalls int
	}{
		{name: "commit", finish: func(tx pgx.Tx, ctx context.Context) error { return tx.Commit(ctx) }, wantCommitCalls: 1},
		{name: "rollback", finish: func(tx pgx.Tx, ctx context.Context) error { return tx.Rollback(ctx) }, wantRollbackCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeAppACLR2RuntimeAdmissionFaultTx{}
			conn := &fakeAppACLR2RuntimeAdmissionFaultConn{tx: tx, unlockResult: true}
			begin := newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) { return conn, nil })
			gotTx, err := begin(context.Background(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatalf("locked begin error = %v", err)
			}
			if err := tt.finish(gotTx, context.Background()); err != nil {
				t.Fatalf("successful %s error = %v", tt.name, err)
			}
			assertAppACLR2RuntimeAdmissionConnectionReleased(t, conn)
			if tx.commitCalls != tt.wantCommitCalls || tx.rollbackCalls != tt.wantRollbackCalls || conn.unlockCalls != 1 {
				t.Fatalf("successful %s cleanup = commit %d rollback %d unlock %d, want %d/%d/1", tt.name, tx.commitCalls, tx.rollbackCalls, conn.unlockCalls, tt.wantCommitCalls, tt.wantRollbackCalls)
			}
		})
	}
}

func TestAppACLR2RuntimeAdmissionFinishDoubleFinishReturnsTxClosedWithoutDuplicateCleanup(t *testing.T) {
	tx := &fakeAppACLR2RuntimeAdmissionFaultTx{}
	conn := &fakeAppACLR2RuntimeAdmissionFaultConn{tx: tx, unlockResult: true}
	begin := newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) { return conn, nil })
	gotTx, err := begin(context.Background(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("locked begin error = %v", err)
	}
	if err := gotTx.Commit(context.Background()); err != nil {
		t.Fatalf("first finish error = %v", err)
	}
	if err := gotTx.Rollback(context.Background()); !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("second finish error = %v, want pgx.ErrTxClosed", err)
	}
	assertAppACLR2RuntimeAdmissionConnectionReleased(t, conn)
	if tx.commitCalls != 1 || tx.rollbackCalls != 0 || conn.unlockCalls != 1 {
		t.Fatalf("double-finish cleanup = commit %d rollback %d unlock %d, want 1/0/1", tx.commitCalls, tx.rollbackCalls, conn.unlockCalls)
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

type appACLR2RuntimeAdmissionRaceLockMode uint8

const (
	appACLR2RuntimeAdmissionRaceLockModeInvalid appACLR2RuntimeAdmissionRaceLockMode = iota
	appACLR2RuntimeAdmissionRaceLockModeShared
	appACLR2RuntimeAdmissionRaceLockModeExclusive
)

const (
	appACLR2RuntimeAdmissionRaceRuntimeOwner   = "runtime-admission"
	appACLR2RuntimeAdmissionRaceBootstrapOwner = "bootstrap"
)

type appACLR2RuntimeAdmissionRaceLockHold struct {
	owner string
	mode  appACLR2RuntimeAdmissionRaceLockMode
}

type appACLR2RuntimeAdmissionRaceLockManager struct {
	mu      sync.Mutex
	held    map[string][]appACLR2RuntimeAdmissionRaceLockHold
	changed chan struct{}
}

func newAppACLR2RuntimeAdmissionRaceLockManager() *appACLR2RuntimeAdmissionRaceLockManager {
	return &appACLR2RuntimeAdmissionRaceLockManager{
		held:    make(map[string][]appACLR2RuntimeAdmissionRaceLockHold),
		changed: make(chan struct{}),
	}
}

func (manager *appACLR2RuntimeAdmissionRaceLockManager) acquire(ctx context.Context, owner, key string, mode appACLR2RuntimeAdmissionRaceLockMode) error {
	if owner == "" || key == "" || mode == appACLR2RuntimeAdmissionRaceLockModeInvalid {
		return fmt.Errorf("invalid deterministic advisory lock request owner=%q key=%q mode=%d", owner, key, mode)
	}
	for {
		manager.mu.Lock()
		if manager.canAcquireLocked(owner, key, mode) {
			manager.held[key] = append(manager.held[key], appACLR2RuntimeAdmissionRaceLockHold{owner: owner, mode: mode})
			manager.mu.Unlock()
			return nil
		}
		changed := manager.changed
		manager.mu.Unlock()

		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (manager *appACLR2RuntimeAdmissionRaceLockManager) canAcquireLocked(owner, key string, mode appACLR2RuntimeAdmissionRaceLockMode) bool {
	for _, hold := range manager.held[key] {
		if hold.owner == owner {
			continue
		}
		if mode == appACLR2RuntimeAdmissionRaceLockModeShared && hold.mode == appACLR2RuntimeAdmissionRaceLockModeShared {
			continue
		}
		return false
	}
	return true
}

func (manager *appACLR2RuntimeAdmissionRaceLockManager) release(owner, key string, mode appACLR2RuntimeAdmissionRaceLockMode) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	holds := manager.held[key]
	for index, hold := range holds {
		if hold.owner != owner || hold.mode != mode {
			continue
		}
		holds = append(holds[:index], holds[index+1:]...)
		if len(holds) == 0 {
			delete(manager.held, key)
		} else {
			manager.held[key] = holds
		}
		close(manager.changed)
		manager.changed = make(chan struct{})
		return nil
	}
	return fmt.Errorf("deterministic advisory lock %q mode %d is not held by %q", key, mode, owner)
}

func appACLR2RuntimeAdmissionRaceLockKey(args []any) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("advisory lock argument count = %d, want 1", len(args))
	}
	key, ok := args[0].(string)
	if !ok || key == "" {
		return "", fmt.Errorf("advisory lock key = %#v, want non-empty string", args[0])
	}
	return key, nil
}

func appACLR2RuntimeAdmissionRaceLockModeFromSQL(sql string) (appACLR2RuntimeAdmissionRaceLockMode, error) {
	switch {
	case strings.Contains(sql, "pg_advisory_lock_shared("):
		return appACLR2RuntimeAdmissionRaceLockModeShared, nil
	case strings.Contains(sql, "pg_advisory_lock("), strings.Contains(sql, "pg_advisory_xact_lock("):
		return appACLR2RuntimeAdmissionRaceLockModeExclusive, nil
	default:
		return appACLR2RuntimeAdmissionRaceLockModeInvalid, fmt.Errorf("unrecognized advisory lock SQL %q", sql)
	}
}

func appACLR2RuntimeAdmissionRaceRuntimeSharedLockRequest(sql string, args []any) (string, error) {
	key, err := appACLR2RuntimeAdmissionRaceLockKey(args)
	if err != nil {
		return "", err
	}
	mode, err := appACLR2RuntimeAdmissionRaceLockModeFromSQL(sql)
	if err != nil {
		return "", err
	}
	if mode != appACLR2RuntimeAdmissionRaceLockModeShared {
		return "", fmt.Errorf("runtime admission advisory lock mode = %d, want shared", mode)
	}
	return key, nil
}

func appACLR2RuntimeAdmissionRaceBootstrapExclusiveLockRequest(sql string, args []any) (string, error) {
	key, err := appACLR2RuntimeAdmissionRaceLockKey(args)
	if err != nil {
		return "", err
	}
	mode, err := appACLR2RuntimeAdmissionRaceLockModeFromSQL(sql)
	if err != nil {
		return "", err
	}
	if mode != appACLR2RuntimeAdmissionRaceLockModeExclusive {
		return "", fmt.Errorf("bootstrap advisory lock mode = %d, want exclusive", mode)
	}
	return key, nil
}

func appACLR2RuntimeAdmissionRaceSharedUnlockKey(sql string, args []any) (string, error) {
	if !strings.Contains(sql, "pg_advisory_unlock_shared(") {
		return "", fmt.Errorf("unexpected runtime shared advisory unlock SQL %q", sql)
	}
	return appACLR2RuntimeAdmissionRaceLockKey(args)
}

func appACLR2RuntimeAdmissionRaceExclusiveUnlockKey(sql string, args []any) (string, error) {
	if !strings.Contains(sql, "pg_advisory_unlock(") {
		return "", fmt.Errorf("unexpected bootstrap exclusive advisory unlock SQL %q", sql)
	}
	return appACLR2RuntimeAdmissionRaceLockKey(args)
}

type fakeAppACLR2RuntimeAdmissionRaceAdmissionConn struct {
	tx    pgx.Tx
	locks *appACLR2RuntimeAdmissionRaceLockManager
	key   string
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	key, err := appACLR2RuntimeAdmissionRaceRuntimeSharedLockRequest(sql, args)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	if conn.key != "" {
		return pgconn.CommandTag{}, fmt.Errorf("runtime admission deterministic advisory lock already held for %q", conn.key)
	}
	if err := conn.locks.acquire(ctx, appACLR2RuntimeAdmissionRaceRuntimeOwner, key, appACLR2RuntimeAdmissionRaceLockModeShared); err != nil {
		return pgconn.CommandTag{}, err
	}
	conn.key = key
	return pgconn.CommandTag{}, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return conn.tx, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	key, err := appACLR2RuntimeAdmissionRaceSharedUnlockKey(sql, args)
	if err != nil {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: err}
	}
	if key != conn.key {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: fmt.Errorf("runtime shared advisory unlock key = %q, want %q", key, conn.key)}
	}
	if err := conn.locks.release(appACLR2RuntimeAdmissionRaceRuntimeOwner, key, appACLR2RuntimeAdmissionRaceLockModeShared); err != nil {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: err}
	}
	conn.key = ""
	return fakeAppACLR2RuntimeAdmissionBoolRow(true)
}

func (*fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) Release() {}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) Discard(context.Context) error {
	if conn.key == "" {
		return nil
	}
	err := conn.locks.release(appACLR2RuntimeAdmissionRaceRuntimeOwner, conn.key, appACLR2RuntimeAdmissionRaceLockModeShared)
	conn.key = ""
	return err
}

type appACLR2RuntimeAdmissionRaceBootstrapLockState struct {
	locks          *appACLR2RuntimeAdmissionRaceLockManager
	sessionKey     string
	transactionKey string
}

type fakeAppACLR2RuntimeAdmissionRaceBootstrapConn struct {
	tx                 pgx.Tx
	locks              *appACLR2RuntimeAdmissionRaceBootstrapLockState
	exclusiveAttempted chan<- struct{}
	exclusiveAcquired  chan<- struct{}
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	key, err := appACLR2RuntimeAdmissionRaceBootstrapExclusiveLockRequest(sql, args)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	select {
	case conn.exclusiveAttempted <- struct{}{}:
	default:
	}
	if err := conn.locks.locks.acquire(ctx, appACLR2RuntimeAdmissionRaceBootstrapOwner, key, appACLR2RuntimeAdmissionRaceLockModeExclusive); err != nil {
		return pgconn.CommandTag{}, err
	}
	conn.locks.sessionKey = key
	select {
	case conn.exclusiveAcquired <- struct{}{}:
	default:
	}
	return pgconn.CommandTag{}, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return conn.tx, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	key, err := appACLR2RuntimeAdmissionRaceExclusiveUnlockKey(sql, args)
	if err != nil {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: err}
	}
	if key != conn.locks.sessionKey {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: fmt.Errorf("bootstrap session advisory unlock key = %q, want %q", key, conn.locks.sessionKey)}
	}
	if err := conn.locks.locks.release(appACLR2RuntimeAdmissionRaceBootstrapOwner, key, appACLR2RuntimeAdmissionRaceLockModeExclusive); err != nil {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: err}
	}
	conn.locks.sessionKey = ""
	return fakeAppACLR2RuntimeAdmissionBoolRow(true)
}

func (*fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) Release() {}

func (conn *fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) Discard(context.Context) error {
	var err error
	if conn.locks.transactionKey != "" {
		err = conn.locks.locks.release(appACLR2RuntimeAdmissionRaceBootstrapOwner, conn.locks.transactionKey, appACLR2RuntimeAdmissionRaceLockModeExclusive)
		conn.locks.transactionKey = ""
	}
	if conn.locks.sessionKey != "" {
		unlockErr := conn.locks.locks.release(appACLR2RuntimeAdmissionRaceBootstrapOwner, conn.locks.sessionKey, appACLR2RuntimeAdmissionRaceLockModeExclusive)
		if err == nil {
			err = unlockErr
		}
		conn.locks.sessionKey = ""
	}
	return err
}

type fakeAppACLR2RuntimeAdmissionRaceBootstrapTx struct {
	pgx.Tx
	locks             *appACLR2RuntimeAdmissionRaceBootstrapLockState
	preparedCommitted chan<- struct{}
	mu                sync.Mutex
	commitCalls       int
}

func (tx *fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	key, err := appACLR2RuntimeAdmissionRaceBootstrapExclusiveLockRequest(sql, args)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	if err := tx.locks.locks.acquire(ctx, appACLR2RuntimeAdmissionRaceBootstrapOwner, key, appACLR2RuntimeAdmissionRaceLockModeExclusive); err != nil {
		return pgconn.CommandTag{}, err
	}
	tx.locks.transactionKey = key
	return pgconn.CommandTag{}, nil
}

func (tx *fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	key, err := appACLR2RuntimeAdmissionRaceExclusiveUnlockKey(sql, args)
	if err != nil {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: err}
	}
	if key != tx.locks.sessionKey {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: fmt.Errorf("bootstrap transaction session-unlock key = %q, want %q", key, tx.locks.sessionKey)}
	}
	if err := tx.locks.locks.release(appACLR2RuntimeAdmissionRaceBootstrapOwner, key, appACLR2RuntimeAdmissionRaceLockModeExclusive); err != nil {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: err}
	}
	tx.locks.sessionKey = ""
	return fakeAppACLR2RuntimeAdmissionBoolRow(true)
}

func (tx *fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) Commit(context.Context) error {
	tx.mu.Lock()
	tx.commitCalls++
	tx.mu.Unlock()
	if err := tx.releaseTransactionLock(); err != nil {
		return err
	}
	tx.preparedCommitted <- struct{}{}
	return nil
}

func (tx *fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) Rollback(context.Context) error {
	return tx.releaseTransactionLock()
}

func (tx *fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) CommitCalls() int {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	return tx.commitCalls
}

func (tx *fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) releaseTransactionLock() error {
	if tx.locks.transactionKey == "" {
		return nil
	}
	err := tx.locks.locks.release(appACLR2RuntimeAdmissionRaceBootstrapOwner, tx.locks.transactionKey, appACLR2RuntimeAdmissionRaceLockModeExclusive)
	tx.locks.transactionKey = ""
	return err
}

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

type fakeAppACLR2RuntimeAdmissionFaultTx struct {
	pgx.Tx
	commitErr                 error
	rollbackErr               error
	commitReturnsContextError bool
	commitCalls               int
	rollbackCalls             int
	commitContextErr          error
	rollbackContextErr        error
}

func (tx *fakeAppACLR2RuntimeAdmissionFaultTx) Commit(ctx context.Context) error {
	tx.commitCalls++
	tx.commitContextErr = ctx.Err()
	if tx.commitReturnsContextError {
		return ctx.Err()
	}
	return tx.commitErr
}

func (tx *fakeAppACLR2RuntimeAdmissionFaultTx) Rollback(ctx context.Context) error {
	tx.rollbackCalls++
	tx.rollbackContextErr = ctx.Err()
	return tx.rollbackErr
}

type fakeAppACLR2RuntimeAdmissionFaultConn struct {
	tx                 pgx.Tx
	beginErr           error
	unlockErr          error
	unlockResult       bool
	lockCalls          int
	beginCalls         int
	unlockCalls        int
	releases           int
	discards           int
	discardContext     context.Context
	discardCtxErr      error
	discardDeadline    time.Time
	discardHasDeadline bool
}

func (conn *fakeAppACLR2RuntimeAdmissionFaultConn) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	conn.lockCalls++
	if sql != appACLR2RuntimeAdmissionSessionSharedTransitionLockSQL {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected runtime admission lock SQL %q", sql)
	}
	if len(args) != 1 || args[0] != appACLR2TransitionAdvisoryLockName {
		return pgconn.CommandTag{}, fmt.Errorf("runtime admission lock arguments = %#v, want transition key", args)
	}
	return pgconn.CommandTag{}, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionFaultConn) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	conn.beginCalls++
	return conn.tx, conn.beginErr
}

func (conn *fakeAppACLR2RuntimeAdmissionFaultConn) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	conn.unlockCalls++
	if sql != appACLR2RuntimeAdmissionSessionSharedTransitionUnlockSQL {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: fmt.Errorf("unexpected runtime admission unlock SQL %q", sql)}
	}
	if len(args) != 1 || args[0] != appACLR2TransitionAdvisoryLockName {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: fmt.Errorf("runtime admission unlock arguments = %#v, want transition key", args)}
	}
	if conn.unlockErr != nil {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: conn.unlockErr}
	}
	return fakeAppACLR2RuntimeAdmissionBoolRow(conn.unlockResult)
}

func (conn *fakeAppACLR2RuntimeAdmissionFaultConn) Release() {
	conn.releases++
}

func (conn *fakeAppACLR2RuntimeAdmissionFaultConn) Discard(ctx context.Context) error {
	conn.discards++
	conn.discardContext = ctx
	conn.discardCtxErr = ctx.Err()
	conn.discardDeadline, conn.discardHasDeadline = ctx.Deadline()
	return nil
}

func assertAppACLR2RuntimeAdmissionConnectionReleased(t *testing.T, conn *fakeAppACLR2RuntimeAdmissionFaultConn) {
	t.Helper()
	if conn.releases != 1 || conn.discards != 0 {
		t.Fatalf("connection cleanup = release %d discard %d, want 1/0", conn.releases, conn.discards)
	}
}

func assertAppACLR2RuntimeAdmissionConnectionDiscarded(t *testing.T, conn *fakeAppACLR2RuntimeAdmissionFaultConn) {
	t.Helper()
	if conn.releases != 0 || conn.discards != 1 {
		t.Fatalf("connection cleanup = release %d discard %d, want 0/1", conn.releases, conn.discards)
	}
	if conn.discardContext == nil || conn.discardCtxErr != nil || !conn.discardHasDeadline {
		t.Fatalf("discard context = %v, err = %v, has deadline = %t; want fresh non-canceled bounded context", conn.discardContext, conn.discardCtxErr, conn.discardHasDeadline)
	}
	remaining := time.Until(conn.discardDeadline)
	if remaining <= 0 || remaining > appACLR2BootstrapDiscardTimeout {
		t.Fatalf("discard context remaining duration = %s, want bounded positive duration no greater than %s", remaining, appACLR2BootstrapDiscardTimeout)
	}
}
