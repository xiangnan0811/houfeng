package migrate

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/recoverycontrol/migrations"
	"houfeng/internal/center/platformmigrate"
)

func Names() ([]string, error) {
	return platformmigrate.Names(migrations.FS)
}

func Apply(ctx context.Context, db *pgxpool.Pool) error {
	return platformmigrate.Apply(ctx, db, migrations.FS)
}
