package deletionledger

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// SourceCommitObservation describes the transaction backend at a source ledger
// commit boundary. Committed is true only after COMMIT succeeds.
type SourceCommitObservation struct {
	Boundary       string
	BackendPID     int32
	BackendStarted time.Time
	BackendRole    string
	Committed      bool
}

type sourceCommitObserverKey struct{}

type sourceCommitObserverFunc func(context.Context, SourceCommitObservation) error

// WithSourceCommitObserver installs optional maintenance instrumentation for
// new W1 source and W2 witness commits in ctx. For source retirement, the
// observer runs synchronously under the existing source guard. A nil observer
// disables an observer in a parent context. Exact W1 replays and already-copied
// W2 prefixes produce no observations.
func WithSourceCommitObserver(ctx context.Context, observer func(context.Context, SourceCommitObservation) error) context.Context {
	return context.WithValue(ctx, sourceCommitObserverKey{}, sourceCommitObserverFunc(observer))
}

func sourceCommitObserverFromContext(ctx context.Context) sourceCommitObserverFunc {
	observer, _ := ctx.Value(sourceCommitObserverKey{}).(sourceCommitObserverFunc)
	return observer
}

// commitObservedSource runs only for a new write with an explicit observer.
// The identity is read from the same transaction whose COMMIT follows the first
// callback; no connection or transaction handle is exposed to the observer.
func commitObservedSource(ctx context.Context, tx pgx.Tx, boundary, commitOperation string, observer sourceCommitObserverFunc) error {
	observation := SourceCommitObservation{Boundary: boundary}
	if err := tx.QueryRow(ctx, `select pg_catalog.pg_backend_pid(), activity.backend_start, current_user::text
 from pg_catalog.pg_stat_activity as activity
 where activity.pid = pg_catalog.pg_backend_pid()`).Scan(&observation.BackendPID, &observation.BackendStarted, &observation.BackendRole); err != nil {
		return failure("read source transaction backend", err)
	}
	if err := observer(ctx, observation); err != nil {
		return failure("observe source transaction before commit", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return failure(commitOperation, err)
	}
	observation.Committed = true
	if err := observer(ctx, observation); err != nil {
		return fmt.Errorf("source transaction committed; outcome requires resolution: %w", failure("post-commit source observer", err))
	}
	return nil
}
