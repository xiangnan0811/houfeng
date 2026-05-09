package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type AssetLedgerDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func NewPostgresAssetLedgerRepositories(db AssetLedgerDB) (*PostgresProviderRepository, *PostgresVPSAssetRepository, *PostgresSubscriptionRepository) {
	return &PostgresProviderRepository{db: db},
		&PostgresVPSAssetRepository{db: db},
		&PostgresSubscriptionRepository{db: db}
}
