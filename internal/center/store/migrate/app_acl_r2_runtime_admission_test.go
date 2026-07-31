package migrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
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
	bootstrapExclusiveAcquired := make(chan struct{}, 1)
	bootstrapPreparedCommitted := make(chan struct{}, 1)
	locks := newAppACLR2RuntimeAdmissionRaceLockManager()

	admissionTx := &fakeAppACLR2RuntimeAdmissionTx{}
	admissionConn := &fakeAppACLR2RuntimeAdmissionRaceAdmissionConn{
		tx:    admissionTx,
		locks: locks,
		owner: appACLR2RuntimeAdmissionRaceRuntimeOwner,
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

	bootstrapLocks := &appACLR2RuntimeAdmissionRaceBootstrapLockState{
		locks: locks,
		owner: appACLR2RuntimeAdmissionRaceBootstrapOwner,
	}
	bootstrapBlocked := make(chan error, 1)
	go func() {
		bootstrapBlocked <- locks.waitForBlocked(ctx, appACLR2RuntimeAdmissionRaceBootstrapOwner)
	}()
	bootstrapTx := &fakeAppACLR2RuntimeAdmissionRaceBootstrapTx{
		locks:             bootstrapLocks,
		exclusiveAcquired: bootstrapExclusiveAcquired,
		preparedCommitted: bootstrapPreparedCommitted,
	}
	bootstrapConn := &fakeAppACLR2RuntimeAdmissionRaceBootstrapConn{
		tx:    bootstrapTx,
		locks: bootstrapLocks,
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
	case <-bootstrapExclusiveAcquired:
		t.Fatal("bootstrap acquired the exclusive transition lock while R1 admission held the shared lock")
	case err := <-bootstrapBlocked:
		if err != nil {
			t.Fatalf("bootstrap did not block on the physical transition lock: %v", err)
		}
	case err := <-bootstrapDone:
		t.Fatalf("bootstrap transition failed before blocking on the physical transition lock: %v", err)
	case <-ctx.Done():
		t.Fatalf("bootstrap did not block on the physical transition lock: %v", ctx.Err())
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
	locks.assertReleased(t)
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

type appACLR2RuntimeAdmissionRaceLockScope uint8

const (
	appACLR2RuntimeAdmissionRaceLockScopeInvalid appACLR2RuntimeAdmissionRaceLockScope = iota
	appACLR2RuntimeAdmissionRaceLockScopeSession
	appACLR2RuntimeAdmissionRaceLockScopeTransaction
)

type appACLR2RuntimeAdmissionRaceAdvisoryFunction uint8

const (
	appACLR2RuntimeAdmissionRaceAdvisoryFunctionInvalid appACLR2RuntimeAdmissionRaceAdvisoryFunction = iota
	appACLR2RuntimeAdmissionRaceAdvisoryFunctionLock
	appACLR2RuntimeAdmissionRaceAdvisoryFunctionUnlock
)

const (
	appACLR2RuntimeAdmissionRaceRuntimeOwner       = "runtime-admission-connection"
	appACLR2RuntimeAdmissionRaceBootstrapOwner     = "bootstrap-connection"
	appACLR2RuntimeAdmissionRaceTransitionLockSeed = int64(0)
	appACLR2RuntimeAdmissionRaceTransitionLockKey  = "houfeng.app-acl-r2-privileged-transition.v1"
)

type appACLR2RuntimeAdmissionRaceLockIdentity struct {
	seed int64
	key  string
}

type appACLR2RuntimeAdmissionRaceLockRequest struct {
	function appACLR2RuntimeAdmissionRaceAdvisoryFunction
	mode     appACLR2RuntimeAdmissionRaceLockMode
	scope    appACLR2RuntimeAdmissionRaceLockScope
	identity appACLR2RuntimeAdmissionRaceLockIdentity
}

type appACLR2RuntimeAdmissionRaceLockHold struct {
	owner string
	mode  appACLR2RuntimeAdmissionRaceLockMode
	scope appACLR2RuntimeAdmissionRaceLockScope
}

type appACLR2RuntimeAdmissionRaceLockManager struct {
	mu      sync.Mutex
	held    map[appACLR2RuntimeAdmissionRaceLockIdentity][]appACLR2RuntimeAdmissionRaceLockHold
	waiting map[string]appACLR2RuntimeAdmissionRaceLockRequest
	changed chan struct{}
}

func newAppACLR2RuntimeAdmissionRaceLockManager() *appACLR2RuntimeAdmissionRaceLockManager {
	return &appACLR2RuntimeAdmissionRaceLockManager{
		held:    make(map[appACLR2RuntimeAdmissionRaceLockIdentity][]appACLR2RuntimeAdmissionRaceLockHold),
		waiting: make(map[string]appACLR2RuntimeAdmissionRaceLockRequest),
		changed: make(chan struct{}),
	}
}

func (manager *appACLR2RuntimeAdmissionRaceLockManager) acquire(ctx context.Context, owner string, request appACLR2RuntimeAdmissionRaceLockRequest) error {
	if owner == "" || request.function != appACLR2RuntimeAdmissionRaceAdvisoryFunctionLock || request.mode == appACLR2RuntimeAdmissionRaceLockModeInvalid || request.scope == appACLR2RuntimeAdmissionRaceLockScopeInvalid || request.identity.key == "" {
		return fmt.Errorf("invalid physical advisory lock request owner=%q request=%#v", owner, request)
	}
	waiting := false
	for {
		manager.mu.Lock()
		if manager.canAcquireLocked(owner, request.identity, request.mode) {
			if waiting {
				delete(manager.waiting, owner)
				manager.notifyLocked()
			}
			manager.held[request.identity] = append(manager.held[request.identity], appACLR2RuntimeAdmissionRaceLockHold{owner: owner, mode: request.mode, scope: request.scope})
			manager.mu.Unlock()
			return nil
		}
		if !waiting {
			manager.waiting[owner] = request
			waiting = true
			manager.notifyLocked()
		}
		changed := manager.changed
		manager.mu.Unlock()

		select {
		case <-changed:
		case <-ctx.Done():
			if waiting {
				manager.mu.Lock()
				delete(manager.waiting, owner)
				manager.notifyLocked()
				manager.mu.Unlock()
			}
			return ctx.Err()
		}
	}
}

func (manager *appACLR2RuntimeAdmissionRaceLockManager) canAcquireLocked(owner string, identity appACLR2RuntimeAdmissionRaceLockIdentity, mode appACLR2RuntimeAdmissionRaceLockMode) bool {
	for _, hold := range manager.held[identity] {
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

func (manager *appACLR2RuntimeAdmissionRaceLockManager) release(owner string, identity appACLR2RuntimeAdmissionRaceLockIdentity, mode appACLR2RuntimeAdmissionRaceLockMode, scope appACLR2RuntimeAdmissionRaceLockScope) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	holds := manager.held[identity]
	for index, hold := range holds {
		if hold.owner != owner || hold.mode != mode || hold.scope != scope {
			continue
		}
		holds = append(holds[:index], holds[index+1:]...)
		if len(holds) == 0 {
			delete(manager.held, identity)
		} else {
			manager.held[identity] = holds
		}
		manager.notifyLocked()
		return nil
	}
	return fmt.Errorf("physical advisory lock %#v mode %d scope %d is not held by %q", identity, mode, scope, owner)
}

func (manager *appACLR2RuntimeAdmissionRaceLockManager) waitForBlocked(ctx context.Context, owner string) error {
	for {
		manager.mu.Lock()
		if _, ok := manager.waiting[owner]; ok {
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

func (manager *appACLR2RuntimeAdmissionRaceLockManager) assertReleased(t *testing.T) {
	t.Helper()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.held) != 0 || len(manager.waiting) != 0 {
		t.Fatalf("physical advisory lock lifecycle = held %#v waiting %#v, want none", manager.held, manager.waiting)
	}
}

func (manager *appACLR2RuntimeAdmissionRaceLockManager) notifyLocked() {
	close(manager.changed)
	manager.changed = make(chan struct{})
}

var appACLR2RuntimeAdmissionRaceAdvisorySQLPattern = regexp.MustCompile(`(?i)^\s*select\s+pg_catalog\.(pg_advisory_(?:xact_)?lock(?:_shared)?|pg_advisory_unlock(?:_shared)?)\s*\(\s*pg_catalog\.hashtextextended\(\s*\$1\s*,\s*(-?[0-9]+)\s*\)\s*\)\s*$`)

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

func appACLR2RuntimeAdmissionRaceAdvisoryRequest(sql string, args []any) (appACLR2RuntimeAdmissionRaceLockRequest, error) {
	matches := appACLR2RuntimeAdmissionRaceAdvisorySQLPattern.FindStringSubmatch(sql)
	if matches == nil {
		return appACLR2RuntimeAdmissionRaceLockRequest{}, fmt.Errorf("unrecognized physical advisory SQL %q", sql)
	}
	key, err := appACLR2RuntimeAdmissionRaceLockKey(args)
	if err != nil {
		return appACLR2RuntimeAdmissionRaceLockRequest{}, err
	}
	seed, err := strconv.ParseInt(matches[2], 10, 64)
	if err != nil {
		return appACLR2RuntimeAdmissionRaceLockRequest{}, fmt.Errorf("parse advisory lock hashtextextended seed %q: %w", matches[2], err)
	}
	request := appACLR2RuntimeAdmissionRaceLockRequest{
		identity: appACLR2RuntimeAdmissionRaceLockIdentity{seed: seed, key: key},
	}
	switch strings.ToLower(matches[1]) {
	case "pg_advisory_lock_shared":
		request.function = appACLR2RuntimeAdmissionRaceAdvisoryFunctionLock
		request.mode = appACLR2RuntimeAdmissionRaceLockModeShared
		request.scope = appACLR2RuntimeAdmissionRaceLockScopeSession
	case "pg_advisory_lock":
		request.function = appACLR2RuntimeAdmissionRaceAdvisoryFunctionLock
		request.mode = appACLR2RuntimeAdmissionRaceLockModeExclusive
		request.scope = appACLR2RuntimeAdmissionRaceLockScopeSession
	case "pg_advisory_xact_lock_shared":
		request.function = appACLR2RuntimeAdmissionRaceAdvisoryFunctionLock
		request.mode = appACLR2RuntimeAdmissionRaceLockModeShared
		request.scope = appACLR2RuntimeAdmissionRaceLockScopeTransaction
	case "pg_advisory_xact_lock":
		request.function = appACLR2RuntimeAdmissionRaceAdvisoryFunctionLock
		request.mode = appACLR2RuntimeAdmissionRaceLockModeExclusive
		request.scope = appACLR2RuntimeAdmissionRaceLockScopeTransaction
	case "pg_advisory_unlock_shared":
		request.function = appACLR2RuntimeAdmissionRaceAdvisoryFunctionUnlock
		request.mode = appACLR2RuntimeAdmissionRaceLockModeShared
		request.scope = appACLR2RuntimeAdmissionRaceLockScopeSession
	case "pg_advisory_unlock":
		request.function = appACLR2RuntimeAdmissionRaceAdvisoryFunctionUnlock
		request.mode = appACLR2RuntimeAdmissionRaceLockModeExclusive
		request.scope = appACLR2RuntimeAdmissionRaceLockScopeSession
	default:
		return appACLR2RuntimeAdmissionRaceLockRequest{}, fmt.Errorf("unrecognized physical advisory function %q", matches[1])
	}
	return request, nil
}

func appACLR2RuntimeAdmissionRaceExpectedRequest(
	sql string,
	args []any,
	function appACLR2RuntimeAdmissionRaceAdvisoryFunction,
	mode appACLR2RuntimeAdmissionRaceLockMode,
	scope appACLR2RuntimeAdmissionRaceLockScope,
	actor string,
) (appACLR2RuntimeAdmissionRaceLockRequest, error) {
	request, err := appACLR2RuntimeAdmissionRaceAdvisoryRequest(sql, args)
	if err != nil {
		return appACLR2RuntimeAdmissionRaceLockRequest{}, err
	}
	wantIdentity := appACLR2RuntimeAdmissionRaceLockIdentity{
		seed: appACLR2RuntimeAdmissionRaceTransitionLockSeed,
		key:  appACLR2RuntimeAdmissionRaceTransitionLockKey,
	}
	if request.function != function || request.mode != mode || request.scope != scope || request.identity != wantIdentity {
		return appACLR2RuntimeAdmissionRaceLockRequest{}, fmt.Errorf("%s physical advisory request = %#v, want function %d mode %d scope %d identity %#v", actor, request, function, mode, scope, wantIdentity)
	}
	return request, nil
}

func appACLR2RuntimeAdmissionRaceRuntimeSessionSharedLockRequest(sql string, args []any) (appACLR2RuntimeAdmissionRaceLockRequest, error) {
	return appACLR2RuntimeAdmissionRaceExpectedRequest(
		sql,
		args,
		appACLR2RuntimeAdmissionRaceAdvisoryFunctionLock,
		appACLR2RuntimeAdmissionRaceLockModeShared,
		appACLR2RuntimeAdmissionRaceLockScopeSession,
		"runtime admission",
	)
}

func appACLR2RuntimeAdmissionRaceRuntimeSessionSharedUnlockRequest(sql string, args []any) (appACLR2RuntimeAdmissionRaceLockRequest, error) {
	return appACLR2RuntimeAdmissionRaceExpectedRequest(
		sql,
		args,
		appACLR2RuntimeAdmissionRaceAdvisoryFunctionUnlock,
		appACLR2RuntimeAdmissionRaceLockModeShared,
		appACLR2RuntimeAdmissionRaceLockScopeSession,
		"runtime admission",
	)
}

func appACLR2RuntimeAdmissionRaceBootstrapSessionExclusiveLockRequest(sql string, args []any) (appACLR2RuntimeAdmissionRaceLockRequest, error) {
	return appACLR2RuntimeAdmissionRaceExpectedRequest(
		sql,
		args,
		appACLR2RuntimeAdmissionRaceAdvisoryFunctionLock,
		appACLR2RuntimeAdmissionRaceLockModeExclusive,
		appACLR2RuntimeAdmissionRaceLockScopeSession,
		"bootstrap session",
	)
}

func appACLR2RuntimeAdmissionRaceBootstrapTransactionExclusiveLockRequest(sql string, args []any) (appACLR2RuntimeAdmissionRaceLockRequest, error) {
	return appACLR2RuntimeAdmissionRaceExpectedRequest(
		sql,
		args,
		appACLR2RuntimeAdmissionRaceAdvisoryFunctionLock,
		appACLR2RuntimeAdmissionRaceLockModeExclusive,
		appACLR2RuntimeAdmissionRaceLockScopeTransaction,
		"bootstrap transaction",
	)
}

func appACLR2RuntimeAdmissionRaceBootstrapSessionExclusiveUnlockRequest(sql string, args []any) (appACLR2RuntimeAdmissionRaceLockRequest, error) {
	return appACLR2RuntimeAdmissionRaceExpectedRequest(
		sql,
		args,
		appACLR2RuntimeAdmissionRaceAdvisoryFunctionUnlock,
		appACLR2RuntimeAdmissionRaceLockModeExclusive,
		appACLR2RuntimeAdmissionRaceLockScopeSession,
		"bootstrap session",
	)
}

type fakeAppACLR2RuntimeAdmissionRaceAdmissionConn struct {
	tx          pgx.Tx
	locks       *appACLR2RuntimeAdmissionRaceLockManager
	owner       string
	sessionLock *appACLR2RuntimeAdmissionRaceLockRequest
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	request, err := appACLR2RuntimeAdmissionRaceRuntimeSessionSharedLockRequest(sql, args)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	if conn.sessionLock != nil {
		return pgconn.CommandTag{}, fmt.Errorf("runtime admission session shared lock already held for %#v", conn.sessionLock.identity)
	}
	if err := conn.locks.acquire(ctx, conn.owner, request); err != nil {
		return pgconn.CommandTag{}, err
	}
	conn.sessionLock = &request
	return pgconn.CommandTag{}, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return conn.tx, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	request, err := appACLR2RuntimeAdmissionRaceRuntimeSessionSharedUnlockRequest(sql, args)
	if err != nil {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: err}
	}
	if conn.sessionLock == nil || request.identity != conn.sessionLock.identity {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: fmt.Errorf("runtime shared advisory unlock identity = %#v, want held session identity %#v", request.identity, conn.sessionLock)}
	}
	if err := conn.locks.release(conn.owner, conn.sessionLock.identity, conn.sessionLock.mode, conn.sessionLock.scope); err != nil {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: err}
	}
	conn.sessionLock = nil
	return fakeAppACLR2RuntimeAdmissionBoolRow(true)
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) Release() {
	if conn.sessionLock != nil {
		panic(fmt.Sprintf("runtime admission released a connection with a session lock still held: %#v", conn.sessionLock))
	}
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceAdmissionConn) Discard(context.Context) error {
	if conn.sessionLock == nil {
		return nil
	}
	err := conn.locks.release(conn.owner, conn.sessionLock.identity, conn.sessionLock.mode, conn.sessionLock.scope)
	conn.sessionLock = nil
	return err
}

type appACLR2RuntimeAdmissionRaceBootstrapLockState struct {
	locks           *appACLR2RuntimeAdmissionRaceLockManager
	owner           string
	sessionLock     *appACLR2RuntimeAdmissionRaceLockRequest
	transactionLock *appACLR2RuntimeAdmissionRaceLockRequest
}

func (state *appACLR2RuntimeAdmissionRaceBootstrapLockState) acquireSession(ctx context.Context, sql string, args []any) error {
	request, err := appACLR2RuntimeAdmissionRaceBootstrapSessionExclusiveLockRequest(sql, args)
	if err != nil {
		return err
	}
	if state.sessionLock != nil {
		return fmt.Errorf("bootstrap session exclusive lock already held for %#v", state.sessionLock.identity)
	}
	if err := state.locks.acquire(ctx, state.owner, request); err != nil {
		return err
	}
	state.sessionLock = &request
	return nil
}

func (state *appACLR2RuntimeAdmissionRaceBootstrapLockState) acquireTransaction(ctx context.Context, sql string, args []any) error {
	request, err := appACLR2RuntimeAdmissionRaceBootstrapTransactionExclusiveLockRequest(sql, args)
	if err != nil {
		return err
	}
	if state.sessionLock == nil {
		return fmt.Errorf("bootstrap transaction exclusive lock acquired without a session handoff lock")
	}
	if state.transactionLock != nil {
		return fmt.Errorf("bootstrap transaction exclusive lock already held for %#v", state.transactionLock.identity)
	}
	if err := state.locks.acquire(ctx, state.owner, request); err != nil {
		return err
	}
	state.transactionLock = &request
	return nil
}

func (state *appACLR2RuntimeAdmissionRaceBootstrapLockState) releaseSession(sql string, args []any) error {
	request, err := appACLR2RuntimeAdmissionRaceBootstrapSessionExclusiveUnlockRequest(sql, args)
	if err != nil {
		return err
	}
	if state.sessionLock == nil || request.identity != state.sessionLock.identity {
		return fmt.Errorf("bootstrap session advisory unlock identity = %#v, want held session identity %#v", request.identity, state.sessionLock)
	}
	if err := state.locks.release(state.owner, state.sessionLock.identity, state.sessionLock.mode, state.sessionLock.scope); err != nil {
		return err
	}
	state.sessionLock = nil
	return nil
}

func (state *appACLR2RuntimeAdmissionRaceBootstrapLockState) releaseTransaction() error {
	if state.transactionLock == nil {
		return nil
	}
	err := state.locks.release(state.owner, state.transactionLock.identity, state.transactionLock.mode, state.transactionLock.scope)
	state.transactionLock = nil
	return err
}

func (state *appACLR2RuntimeAdmissionRaceBootstrapLockState) discard() error {
	err := state.releaseTransaction()
	if state.sessionLock != nil {
		unlockErr := state.locks.release(state.owner, state.sessionLock.identity, state.sessionLock.mode, state.sessionLock.scope)
		state.sessionLock = nil
		if err == nil {
			err = unlockErr
		}
	}
	return err
}

func (state *appACLR2RuntimeAdmissionRaceBootstrapLockState) assertReleased() {
	if state.sessionLock != nil || state.transactionLock != nil {
		panic(fmt.Sprintf("bootstrap released a connection with locks still held: session %#v transaction %#v", state.sessionLock, state.transactionLock))
	}
}

type fakeAppACLR2RuntimeAdmissionRaceBootstrapConn struct {
	tx    pgx.Tx
	locks *appACLR2RuntimeAdmissionRaceBootstrapLockState
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := conn.locks.acquireSession(ctx, sql, args); err != nil {
		return pgconn.CommandTag{}, err
	}
	return pgconn.CommandTag{}, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return conn.tx, nil
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if err := conn.locks.releaseSession(sql, args); err != nil {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: err}
	}
	return fakeAppACLR2RuntimeAdmissionBoolRow(true)
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) Release() {
	conn.locks.assertReleased()
}

func (conn *fakeAppACLR2RuntimeAdmissionRaceBootstrapConn) Discard(context.Context) error {
	return conn.locks.discard()
}

type fakeAppACLR2RuntimeAdmissionRaceBootstrapTx struct {
	pgx.Tx
	locks             *appACLR2RuntimeAdmissionRaceBootstrapLockState
	exclusiveAcquired chan<- struct{}
	preparedCommitted chan<- struct{}
	mu                sync.Mutex
	commitCalls       int
}

func (tx *fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := tx.locks.acquireTransaction(ctx, sql, args); err != nil {
		return pgconn.CommandTag{}, err
	}
	select {
	case tx.exclusiveAcquired <- struct{}{}:
	default:
	}
	return pgconn.CommandTag{}, nil
}

func (tx *fakeAppACLR2RuntimeAdmissionRaceBootstrapTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if err := tx.locks.releaseSession(sql, args); err != nil {
		return fakeAppACLR2RuntimeAdmissionErrorRow{err: err}
	}
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
	return tx.locks.releaseTransaction()
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
