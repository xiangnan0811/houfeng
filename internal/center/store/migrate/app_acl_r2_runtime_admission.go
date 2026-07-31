package migrate

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	appACLR2RuntimeAdmissionSessionSharedTransitionLockSQL   = "select pg_catalog.pg_advisory_lock_shared(pg_catalog.hashtextextended($1, 0))"
	appACLR2RuntimeAdmissionSessionSharedTransitionUnlockSQL = "select pg_catalog.pg_advisory_unlock_shared(pg_catalog.hashtextextended($1, 0))"
)

type appACLR2RuntimeAdmissionBeginTx func(context.Context, pgx.TxOptions) (pgx.Tx, error)

type appACLR2RuntimeAdmissionDependencies struct {
	classify             func(context.Context, pgx.Tx) (AppACLR2State, error)
	verifyFrozen         func(context.Context, pgx.Tx) (FrozenAppACLR1StateV1, error)
	requireDirectRuntime func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) error
}

func defaultAppACLR2RuntimeAdmissionDependencies() appACLR2RuntimeAdmissionDependencies {
	return appACLR2RuntimeAdmissionDependencies{
		classify:             ClassifyAppACLR2State,
		verifyFrozen:         VerifyFrozenAppACLR1StateInTx,
		requireDirectRuntime: RequireDirectFrozenAppACLR1RuntimeInTx,
	}
}

func (dependencies appACLR2RuntimeAdmissionDependencies) validate() error {
	if dependencies.classify == nil || dependencies.verifyFrozen == nil || dependencies.requireDirectRuntime == nil {
		return fmt.Errorf("APP ACL R2 runtime admission dependencies are incomplete")
	}
	return nil
}

// AdmitAppACLR1OnlyRuntime admits only an exact frozen R1 state through the
// new transition-safe route. It leaves the frozen R1 runtime entry point
// unchanged.
func AdmitAppACLR1OnlyRuntime(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return fmt.Errorf("APP ACL R1-only runtime admission has no PostgreSQL pool")
	}
	_, err := admitAppACLR1OnlyRuntimeWithDependencies(
		ctx,
		newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(db)),
		defaultAppACLR2RuntimeAdmissionDependencies(),
	)
	return err
}

// AdmitAppACLR2Runtime admits exact R1 before the transition and exact
// FINALIZED R2 after the shared constrained state proof succeeds.
func AdmitAppACLR2Runtime(ctx context.Context, db *pgxpool.Pool) (AppACLR2State, error) {
	if db == nil {
		return AppACLR2StateCorrupt, fmt.Errorf("APP ACL R2 runtime admission has no PostgreSQL pool")
	}
	return admitAppACLR2RuntimeWithDependencies(
		ctx,
		newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(db)),
		defaultAppACLR2RuntimeAdmissionDependencies(),
	)
}

// StartAppACLR2Runtime is the opt-in startup route for transition-aware
// runtime admission. Existing R1 startup routes remain unchanged.
func StartAppACLR2Runtime(ctx context.Context, db *pgxpool.Pool) (AppACLR2State, error) {
	return AdmitAppACLR2Runtime(ctx, db)
}

func appACLR2RuntimeAdmissionPoolAcquire(db *pgxpool.Pool) appACLR2BootstrapAcquireConn {
	return func(ctx context.Context) (appACLR2BootstrapReservedConn, error) {
		conn, err := db.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		return &appACLR2BootstrapPoolConn{Conn: conn}, nil
	}
}

func admitAppACLR1OnlyRuntimeWithDependencies(
	ctx context.Context,
	begin appACLR2RuntimeAdmissionBeginTx,
	dependencies appACLR2RuntimeAdmissionDependencies,
) (AppACLR2State, error) {
	return admitAppACLR2RuntimeWithMode(ctx, begin, dependencies, false)
}

func admitAppACLR2RuntimeWithDependencies(
	ctx context.Context,
	begin appACLR2RuntimeAdmissionBeginTx,
	dependencies appACLR2RuntimeAdmissionDependencies,
) (AppACLR2State, error) {
	return admitAppACLR2RuntimeWithMode(ctx, begin, dependencies, true)
}

func startAppACLR2RuntimeWithDependencies(
	ctx context.Context,
	begin appACLR2RuntimeAdmissionBeginTx,
	dependencies appACLR2RuntimeAdmissionDependencies,
) (AppACLR2State, error) {
	return admitAppACLR2RuntimeWithDependencies(ctx, begin, dependencies)
}

func admitAppACLR2RuntimeWithMode(
	ctx context.Context,
	begin appACLR2RuntimeAdmissionBeginTx,
	dependencies appACLR2RuntimeAdmissionDependencies,
	allowFinalized bool,
) (AppACLR2State, error) {
	if begin == nil {
		return AppACLR2StateCorrupt, fmt.Errorf("APP ACL R2 runtime admission transaction opener is nil")
	}
	if err := dependencies.validate(); err != nil {
		return AppACLR2StateCorrupt, err
	}
	tx, err := begin(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return AppACLR2StateCorrupt, fmt.Errorf("begin APP ACL R2 runtime admission transaction: %w", err)
	}
	if tx == nil {
		return AppACLR2StateCorrupt, fmt.Errorf("begin APP ACL R2 runtime admission transaction returned nil")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), appACLR2BootstrapDiscardTimeout)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	}()

	state, err := dependencies.classify(ctx, tx)
	if err != nil {
		return AppACLR2StateCorrupt, fmt.Errorf("classify APP ACL R2 runtime state: %w", err)
	}
	switch state {
	case AppACLR2StateR1:
		frozen, err := dependencies.verifyFrozen(ctx, tx)
		if err != nil {
			return AppACLR2StateCorrupt, fmt.Errorf("verify frozen APP ACL R1 runtime state: %w", err)
		}
		if err := dependencies.requireDirectRuntime(ctx, tx, frozen); err != nil {
			return AppACLR2StateCorrupt, fmt.Errorf("require direct frozen APP ACL R1 runtime: %w", err)
		}
	case AppACLR2StateFinalized:
		if !allowFinalized {
			return AppACLR2StateCorrupt, fmt.Errorf("APP ACL R1-only runtime admission requires exact R1 state")
		}
	default:
		if allowFinalized {
			return AppACLR2StateCorrupt, fmt.Errorf("APP ACL R2 runtime admission requires exact R1 or FINALIZED state")
		}
		return AppACLR2StateCorrupt, fmt.Errorf("APP ACL R1-only runtime admission requires exact R1 state")
	}
	if err := tx.Commit(ctx); err != nil {
		return AppACLR2StateCorrupt, fmt.Errorf("commit APP ACL R2 runtime admission transaction: %w", err)
	}
	return state, nil
}

func newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(
	acquire appACLR2BootstrapAcquireConn,
) appACLR2RuntimeAdmissionBeginTx {
	if acquire == nil {
		return nil
	}
	return func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		conn, err := acquire(ctx)
		if err != nil {
			return nil, fmt.Errorf("reserve APP ACL R2 runtime PostgreSQL connection: %w", err)
		}
		if conn == nil {
			return nil, fmt.Errorf("reserve APP ACL R2 runtime PostgreSQL connection returned nil")
		}
		if err := acquireAppACLR2RuntimeAdmissionSharedTransitionLock(ctx, conn); err != nil {
			discardAppACLR2RuntimeAdmissionConnection(conn)
			return nil, err
		}
		tx, err := conn.BeginTx(ctx, options)
		if err != nil {
			return nil, finishAppACLR2RuntimeAdmissionBeginFailure(ctx, conn, fmt.Errorf("begin transition-locked APP ACL R2 runtime transaction: %w", err))
		}
		if tx == nil {
			return nil, finishAppACLR2RuntimeAdmissionBeginFailure(ctx, conn, fmt.Errorf("begin transition-locked APP ACL R2 runtime transaction returned nil"))
		}
		return &appACLR2RuntimeAdmissionReservedTx{Tx: tx, conn: conn}, nil
	}
}

func acquireAppACLR2RuntimeAdmissionSharedTransitionLock(ctx context.Context, conn appACLR2BootstrapReservedConn) error {
	if _, err := conn.Exec(ctx, appACLR2RuntimeAdmissionSessionSharedTransitionLockSQL, appACLR2TransitionAdvisoryLockName); err != nil {
		return fmt.Errorf("lock APP ACL R2 runtime shared transition before transaction: %w", err)
	}
	return nil
}

func releaseAppACLR2RuntimeAdmissionSharedTransitionLock(ctx context.Context, conn appACLR2BootstrapReservedConn) error {
	var unlocked bool
	if err := conn.QueryRow(ctx, appACLR2RuntimeAdmissionSessionSharedTransitionUnlockSQL, appACLR2TransitionAdvisoryLockName).Scan(&unlocked); err != nil {
		return fmt.Errorf("unlock APP ACL R2 runtime shared transition lock: %w", err)
	}
	if !unlocked {
		return fmt.Errorf("APP ACL R2 runtime shared transition lock was not held")
	}
	return nil
}

func finishAppACLR2RuntimeAdmissionBeginFailure(
	ctx context.Context,
	conn appACLR2BootstrapReservedConn,
	beginErr error,
) error {
	if unlockErr := releaseAppACLR2RuntimeAdmissionSharedTransitionLock(ctx, conn); unlockErr != nil {
		discardAppACLR2RuntimeAdmissionConnection(conn)
		return errors.Join(beginErr, unlockErr)
	}
	conn.Release()
	return beginErr
}

func discardAppACLR2RuntimeAdmissionConnection(conn appACLR2BootstrapReservedConn) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), appACLR2BootstrapDiscardTimeout)
	defer cancel()
	_ = conn.Discard(cleanupCtx)
}

type appACLR2RuntimeAdmissionReservedTx struct {
	pgx.Tx
	conn     appACLR2BootstrapReservedConn
	finishMu sync.Mutex
	finished bool
}

func (tx *appACLR2RuntimeAdmissionReservedTx) Commit(ctx context.Context) error {
	return tx.finish(ctx, tx.Tx.Commit)
}

func (tx *appACLR2RuntimeAdmissionReservedTx) Rollback(ctx context.Context) error {
	return tx.finish(ctx, tx.Tx.Rollback)
}

func (tx *appACLR2RuntimeAdmissionReservedTx) finish(ctx context.Context, finish func(context.Context) error) error {
	if !tx.claimFinish() {
		return pgx.ErrTxClosed
	}
	if err := finish(ctx); err != nil {
		discardAppACLR2RuntimeAdmissionConnection(tx.conn)
		return err
	}
	if err := releaseAppACLR2RuntimeAdmissionSharedTransitionLock(ctx, tx.conn); err != nil {
		discardAppACLR2RuntimeAdmissionConnection(tx.conn)
		return err
	}
	tx.conn.Release()
	return nil
}

func (tx *appACLR2RuntimeAdmissionReservedTx) claimFinish() bool {
	tx.finishMu.Lock()
	defer tx.finishMu.Unlock()
	if tx.finished {
		return false
	}
	tx.finished = true
	return true
}
