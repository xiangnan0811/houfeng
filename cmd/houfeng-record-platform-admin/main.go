package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/store"
	"houfeng/internal/center/store/migrate"
)

var (
	errInvalidAppMigrationInvocation = errors.New("only migrate --scope app is supported")
	errOpenAppMigratorPool           = errors.New("open app migrator PostgreSQL connection failed")
	errConvergeAppMigration          = errors.New("converge app migration failed")
)

type appMigrationDependencies struct {
	lookupEnv    func(string) (string, bool)
	openPostgres func(context.Context, string) (*pgxpool.Pool, error)
	closePool    func(*pgxpool.Pool)
	converge     func(context.Context, *pgxpool.Pool, string, string) error
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("record platform app migration failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runWithDeps(context.Background(), args, defaultAppMigrationDependencies())
}

func defaultAppMigrationDependencies() appMigrationDependencies {
	return appMigrationDependencies{
		lookupEnv:    os.LookupEnv,
		openPostgres: store.OpenPostgres,
		closePool: func(pool *pgxpool.Pool) {
			pool.Close()
		},
		converge: func(ctx context.Context, pool *pgxpool.Pool, runtimeRole, adminRole string) error {
			_, err := migrate.ConvergeAppACLR1(ctx, pool, runtimeRole, adminRole)
			return err
		},
	}
}

func runWithDeps(ctx context.Context, args []string, deps appMigrationDependencies) error {
	if err := parseAppMigrationArgs(args); err != nil {
		return err
	}
	config, err := platformmigrate.LoadAppScopeConfig(deps.lookupEnv)
	if err != nil {
		return err
	}
	if deps.openPostgres == nil || deps.closePool == nil || deps.converge == nil {
		return errConvergeAppMigration
	}
	pool, err := deps.openPostgres(ctx, config.MigratorDatabaseURL)
	if pool != nil {
		defer deps.closePool(pool)
	}
	if err != nil || pool == nil {
		return errOpenAppMigratorPool
	}
	if err := deps.converge(ctx, pool, config.RuntimeRole, config.AdminRole); err != nil {
		return errConvergeAppMigration
	}
	return nil
}

func parseAppMigrationArgs(args []string) error {
	if len(args) == 0 || args[0] != "migrate" {
		return errInvalidAppMigrationInvocation
	}

	flags := flag.NewFlagSet("houfeng-record-platform-admin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scope := flags.String("scope", "", "")
	if err := flags.Parse(args[1:]); err != nil {
		return errInvalidAppMigrationInvocation
	}
	if len(args) != 3 || args[1] != "--scope" || args[2] != "app" || *scope != "app" || flags.NArg() != 0 {
		return errInvalidAppMigrationInvocation
	}
	return nil
}
