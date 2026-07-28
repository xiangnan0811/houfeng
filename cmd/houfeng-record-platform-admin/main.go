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
	errInvalidAppMigrationInvocation      = errors.New("only migrate --scope app is supported")
	errOpenAppMigratorPool                = errors.New("open app migrator PostgreSQL connection failed")
	errConvergeAppMigration               = errors.New("converge app migration failed")
	errInvalidAppACLR2BootstrapInvocation = errors.New("only bootstrap --scope app-acl-r2 is supported")
	errOpenAppACLR2BootstrapPool          = errors.New("open app ACL R2 bootstrap PostgreSQL connection failed")
	errBootstrapAppACLR2                  = errors.New("bootstrap app ACL R2 failed")
)

type appMigrationDependencies struct {
	lookupEnv    func(string) (string, bool)
	openPostgres func(context.Context, string) (*pgxpool.Pool, error)
	closePool    func(*pgxpool.Pool)
	converge     func(context.Context, *pgxpool.Pool, string, string) error
}

type appACLR2BootstrapDependencies struct {
	lookupEnv    func(string) (string, bool)
	openPostgres func(context.Context, string) (*pgxpool.Pool, error)
	closePool    func(*pgxpool.Pool)
	bootstrap    func(context.Context, *pgxpool.Pool) error
}

type appAdminDependencies struct {
	migration appMigrationDependencies
	bootstrap appACLR2BootstrapDependencies
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("record platform admin command failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runAdminWithDeps(context.Background(), args, defaultAppAdminDependencies())
}

func defaultAppAdminDependencies() appAdminDependencies {
	return appAdminDependencies{
		migration: defaultAppMigrationDependencies(),
		bootstrap: defaultAppACLR2BootstrapDependencies(),
	}
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

func defaultAppACLR2BootstrapDependencies() appACLR2BootstrapDependencies {
	return appACLR2BootstrapDependencies{
		lookupEnv:    os.LookupEnv,
		openPostgres: store.OpenPostgres,
		closePool: func(pool *pgxpool.Pool) {
			pool.Close()
		},
		bootstrap: migrate.BootstrapAppACLR2,
	}
}

func runAdminWithDeps(ctx context.Context, args []string, deps appAdminDependencies) error {
	if len(args) > 0 && args[0] == "bootstrap" {
		return runAppACLR2BootstrapWithDeps(ctx, args, deps.bootstrap)
	}
	return runWithDeps(ctx, args, deps.migration)
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

func runAppACLR2BootstrapWithDeps(ctx context.Context, args []string, deps appACLR2BootstrapDependencies) error {
	if err := parseAppACLR2BootstrapArgs(args); err != nil {
		return err
	}
	config, err := platformmigrate.LoadAppACLR2BootstrapConfig(deps.lookupEnv)
	if err != nil {
		return err
	}
	if deps.openPostgres == nil || deps.closePool == nil || deps.bootstrap == nil {
		return errBootstrapAppACLR2
	}
	pool, err := deps.openPostgres(ctx, config.BootstrapDatabaseURL)
	if pool != nil {
		defer deps.closePool(pool)
	}
	if err != nil || pool == nil {
		return errOpenAppACLR2BootstrapPool
	}
	if err := deps.bootstrap(ctx, pool); err != nil {
		return errBootstrapAppACLR2
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

func parseAppACLR2BootstrapArgs(args []string) error {
	if len(args) == 0 || args[0] != "bootstrap" {
		return errInvalidAppACLR2BootstrapInvocation
	}

	flags := flag.NewFlagSet("houfeng-record-platform-admin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scope := flags.String("scope", "", "")
	if err := flags.Parse(args[1:]); err != nil {
		return errInvalidAppACLR2BootstrapInvocation
	}
	if len(args) != 3 || args[1] != "--scope" || args[2] != "app-acl-r2" || *scope != "app-acl-r2" || flags.NArg() != 0 {
		return errInvalidAppACLR2BootstrapInvocation
	}
	return nil
}
