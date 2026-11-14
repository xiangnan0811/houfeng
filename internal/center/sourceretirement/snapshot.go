package sourceretirement

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Snapshot authenticates the configured physical W2 domain and direct principal.
// It grants no source admission: initial enrollment and recovery may inspect a
// domain before its APP activation. Source readers must use Observe instead.
func (authority *Authority) Snapshot(ctx context.Context) (Observation, error) {
	if err := authority.checkContext(ctx); err != nil {
		return Observation{}, err
	}
	tx, err := authority.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Observation{}, authorityFailure("begin authority snapshot", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := authority.snapshot(ctx, tx)
	if err != nil {
		return Observation{}, authorityFailure("read authority snapshot", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Observation{}, authorityFailure("commit authority snapshot", err)
	}
	return state, nil
}
