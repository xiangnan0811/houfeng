package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// AppACLR2State is the complete R2 transition state derived only from catalog,
// source, ledger, and ACL evidence.
type AppACLR2State uint8

const (
	AppACLR2StateCorrupt AppACLR2State = iota
	AppACLR2StateR1
	AppACLR2StatePrepared
	AppACLR2StateFinalized
)

// AppACLR2StateReader classifies state through a caller-owned transaction.
type AppACLR2StateReader interface {
	ClassifyAppACLR2State(context.Context, pgx.Tx) (AppACLR2State, error)
}

// PostgresAppACLR2StateReader is the PostgreSQL implementation of the R2
// state reader. It owns no pool because callers provide the locked snapshot.
type PostgresAppACLR2StateReader struct{}

// NewPostgresAppACLR2StateReader creates a state reader for caller-owned
// PostgreSQL transactions.
func NewPostgresAppACLR2StateReader() *PostgresAppACLR2StateReader {
	return &PostgresAppACLR2StateReader{}
}

// ClassifyAppACLR2State classifies through the supplied transaction without
// reading the session identity or opening another transaction.
func (reader *PostgresAppACLR2StateReader) ClassifyAppACLR2State(ctx context.Context, tx pgx.Tx) (AppACLR2State, error) {
	if reader == nil {
		return AppACLR2StateCorrupt, fmt.Errorf("APP ACL R2 state reader is nil")
	}
	return ClassifyAppACLR2State(ctx, tx)
}

type appACLR2StateDependencies struct {
	readPredicates func(context.Context, pgx.Tx) (AppACLR2CatalogPredicates, error)
}

// ClassifyAppACLR2State is the sole R2 classifier. After every evidence read
// succeeds, its four values are exhaustive and any non-exact shape is CORRUPT.
// The API is error-first: when a catalog read fails, the accompanying zero-value
// CORRUPT result is not an evidence verdict.
func ClassifyAppACLR2State(ctx context.Context, tx pgx.Tx) (AppACLR2State, error) {
	return classifyAppACLR2StateWithDependencies(ctx, tx, appACLR2StateDependencies{
		readPredicates: ReadAppACLR2CatalogPredicatesInTx,
	})
}

func classifyAppACLR2StateWithDependencies(
	ctx context.Context,
	tx pgx.Tx,
	dependencies appACLR2StateDependencies,
) (AppACLR2State, error) {
	if tx == nil {
		return AppACLR2StateCorrupt, fmt.Errorf("APP ACL R2 state classifier has no transaction")
	}
	if dependencies.readPredicates == nil {
		return AppACLR2StateCorrupt, fmt.Errorf("APP ACL R2 state classifier dependencies are incomplete")
	}
	predicates, err := dependencies.readPredicates(ctx, tx)
	if err != nil {
		return AppACLR2StateCorrupt, fmt.Errorf("read APP ACL R2 catalog predicates: %w", err)
	}
	if !appACLR2CatalogPredicatesAreConsistent(predicates) {
		return AppACLR2StateCorrupt, nil
	}
	switch {
	case predicates.ExactR1():
		return AppACLR2StateR1, nil
	case predicates.ExactPrepared():
		return AppACLR2StatePrepared, nil
	case predicates.ExactFinalized():
		return AppACLR2StateFinalized, nil
	default:
		return AppACLR2StateCorrupt, nil
	}
}

func appACLR2CatalogPredicatesAreConsistent(predicates AppACLR2CatalogPredicates) bool {
	if predicates.ExactL2 && (!predicates.ExactL1M1 || predicates.L2Absent) {
		return false
	}
	if predicates.ExactM2 && (!predicates.ExactL2 || predicates.M2Absent) {
		return false
	}
	if predicates.ExactL1M1 && predicates.L2Absent && predicates.M2Absent && (predicates.ExactL2 || predicates.ExactM2) {
		return false
	}
	return true
}
