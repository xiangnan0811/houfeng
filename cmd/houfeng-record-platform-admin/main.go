package main

import (
	"context"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/store"
	"houfeng/internal/center/store/migrate"
)

var (
	errInvalidAppMigrationInvocation        = errors.New("only migrate --scope app is supported")
	errOpenAppMigratorPool                  = errors.New("open app migrator PostgreSQL connection failed")
	errConvergeAppMigration                 = errors.New("converge app migration failed")
	errInvalidAppACLR2BootstrapInvocation   = errors.New("only bootstrap --scope app-acl-r2 is supported")
	errOpenAppACLR2BootstrapPool            = errors.New("open app ACL R2 bootstrap PostgreSQL connection failed")
	errBootstrapAppACLR2                    = errors.New("bootstrap app ACL R2 failed")
	errInvalidAppACLR2FinalizeInvocation    = errors.New("only finalize --scope app-acl-r2 is supported")
	errInvalidAppACLR2FinalizeConfiguration = errors.New("app ACL R2 finalizer configuration is invalid")
	errOpenAppACLR2FinalizePool             = errors.New("open app ACL R2 finalizer PostgreSQL connection failed")
	errFinalizeAppACLR2                     = errors.New("finalize app ACL R2 failed")
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

// appACLR2FinalizeDatabaseURL is constructed only after validating the raw
// direct-migrator environment value and stripping every file-backed default.
type appACLR2FinalizeDatabaseURL string

type appACLR2FinalizePoolHandle interface {
	Ping(context.Context) error
	Close()
	postgresPool() *pgxpool.Pool
}

type appACLR2FinalizePoolConstructor func(context.Context, *pgxpool.Config) (appACLR2FinalizePoolHandle, error)

type appACLR2FinalizePostgresOpener struct {
	newPool appACLR2FinalizePoolConstructor
}

type appACLR2FinalizePGXPoolHandle struct {
	pool *pgxpool.Pool
}

type appACLR2FinalizeDependencies struct {
	lookupEnv    func(string) (string, bool)
	openPostgres func(context.Context, appACLR2FinalizeDatabaseURL) (*pgxpool.Pool, error)
	closePool    func(*pgxpool.Pool)
	finalize     func(context.Context, *pgxpool.Pool) error
}

type appAdminDependencies struct {
	migration appMigrationDependencies
	bootstrap appACLR2BootstrapDependencies
	finalize  appACLR2FinalizeDependencies
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
		finalize:  defaultAppACLR2FinalizeDependencies(),
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
			_, err := migrate.ConvergeAppACLCurrent(ctx, pool, runtimeRole, adminRole)
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

func defaultAppACLR2FinalizeDependencies() appACLR2FinalizeDependencies {
	return appACLR2FinalizeDependencies{
		lookupEnv:    os.LookupEnv,
		openPostgres: productionAppACLR2FinalizePostgresOpener().open,
		closePool: func(pool *pgxpool.Pool) {
			pool.Close()
		},
		finalize: migrate.FinalizeAppACLR2,
	}
}

func runAdminWithDeps(ctx context.Context, args []string, deps appAdminDependencies) error {
	if len(args) > 0 {
		switch args[0] {
		case "bootstrap":
			return runAppACLR2BootstrapWithDeps(ctx, args, deps.bootstrap)
		case "finalize":
			return runAppACLR2FinalizeWithDeps(ctx, args, deps.finalize)
		}
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
		if errors.Is(err, migrate.ErrDevelopmentDatabaseRebuildRequired) {
			return fmt.Errorf("%w: %w", errConvergeAppMigration, migrate.ErrDevelopmentDatabaseRebuildRequired)
		}
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

func runAppACLR2FinalizeWithDeps(ctx context.Context, args []string, deps appACLR2FinalizeDependencies) error {
	if err := parseAppACLR2FinalizeArgs(args); err != nil {
		return err
	}
	if deps.lookupEnv == nil {
		return errInvalidAppACLR2FinalizeInvocation
	}
	databaseURL, ok := deps.lookupEnv(platformmigrate.AppMigratorDatabaseURLEnv)
	databaseURL = strings.TrimSpace(databaseURL)
	if !ok || databaseURL == "" {
		return errInvalidAppACLR2FinalizeConfiguration
	}
	canonicalDatabaseURL, err := canonicalAppACLR2FinalizeDatabaseURL(databaseURL)
	if err != nil {
		return errInvalidAppACLR2FinalizeConfiguration
	}
	if deps.openPostgres == nil || deps.closePool == nil || deps.finalize == nil {
		return errFinalizeAppACLR2
	}
	pool, err := deps.openPostgres(ctx, canonicalDatabaseURL)
	if pool != nil {
		defer deps.closePool(pool)
	}
	if err != nil || pool == nil {
		return errOpenAppACLR2FinalizePool
	}
	if err := deps.finalize(ctx, pool); err != nil {
		return errFinalizeAppACLR2
	}
	return nil
}

func productionAppACLR2FinalizePostgresOpener() appACLR2FinalizePostgresOpener {
	return appACLR2FinalizePostgresOpener{newPool: newAppACLR2FinalizePGXPool}
}

func newAppACLR2FinalizePGXPool(ctx context.Context, config *pgxpool.Config) (appACLR2FinalizePoolHandle, error) {
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	return &appACLR2FinalizePGXPoolHandle{pool: pool}, nil
}

func (pool *appACLR2FinalizePGXPoolHandle) Ping(ctx context.Context) error {
	return pool.pool.Ping(ctx)
}

func (pool *appACLR2FinalizePGXPoolHandle) Close() {
	pool.pool.Close()
}

func (pool *appACLR2FinalizePGXPoolHandle) postgresPool() *pgxpool.Pool {
	return pool.pool
}

func (opener appACLR2FinalizePostgresOpener) open(ctx context.Context, databaseURL appACLR2FinalizeDatabaseURL) (*pgxpool.Pool, error) {
	config, err := parseAppACLR2FinalizePostgresConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if opener.newPool == nil {
		return nil, errors.New("APP ACL R2 finalizer postgres pool constructor is unavailable")
	}

	handle, err := opener.newPool(ctx, config)
	if err != nil {
		if handle != nil {
			handle.Close()
		}
		return nil, fmt.Errorf("open APP ACL R2 finalizer postgres pool: %w", err)
	}
	if handle == nil {
		return nil, errors.New("open APP ACL R2 finalizer postgres pool returned nil pool handle")
	}
	pool := handle.postgresPool()
	if pool == nil {
		handle.Close()
		return nil, errors.New("open APP ACL R2 finalizer postgres pool returned nil PostgreSQL pool")
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := handle.Ping(pingCtx); err != nil {
		handle.Close()
		return nil, fmt.Errorf("ping APP ACL R2 finalizer postgres: %w", err)
	}
	return pool, nil
}

func parseAppACLR2FinalizePostgresConfig(databaseURL appACLR2FinalizeDatabaseURL) (*pgxpool.Config, error) {
	config, err := pgxpool.ParseConfig(string(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse APP ACL R2 finalizer postgres config: %w", err)
	}
	parsed, err := url.Parse(string(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse APP ACL R2 finalizer postgres URL: %w", err)
	}
	switch parsed.Query().Get("sslmode") {
	case "verify-ca", "verify-full":
		if config.ConnConfig == nil || config.ConnConfig.TLSConfig == nil {
			return nil, errors.New("APP ACL R2 finalizer verified TLS has no configuration")
		}
		// pgx leaves RootCAs nil when sslrootcert is empty, which delegates
		// verification to Go's ambient system roots. This route has no approved
		// root source, so an explicit empty pool makes verified TLS fail closed.
		config.ConnConfig.TLSConfig.RootCAs = x509.NewCertPool()
	}
	return config, nil
}

func canonicalAppACLR2FinalizeDatabaseURL(databaseURL string) (appACLR2FinalizeDatabaseURL, error) {
	for _, entry := range os.Environ() {
		name, value, found := strings.Cut(entry, "=")
		if found && value != "" && (strings.HasPrefix(name, "PG") || name == "SSL_CERT_FILE" || name == "SSL_CERT_DIR") {
			return "", errInvalidAppACLR2FinalizeConfiguration
		}
	}

	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed == nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Opaque != "" || parsed.Fragment != "" {
		return "", errInvalidAppACLR2FinalizeConfiguration
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return "", errInvalidAppACLR2FinalizeConfiguration
	}
	password, passwordPresent := parsed.User.Password()
	if !passwordPresent || password == "" {
		return "", errInvalidAppACLR2FinalizeConfiguration
	}
	if parsed.Host == "" || strings.Contains(parsed.Host, ",") || parsed.Hostname() == "" || parsed.Port() == "" {
		return "", errInvalidAppACLR2FinalizeConfiguration
	}
	port, err := strconv.ParseUint(parsed.Port(), 10, 16)
	if err != nil || port == 0 {
		return "", errInvalidAppACLR2FinalizeConfiguration
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if parsed.Path == "" || databaseName == "" || strings.Contains(databaseName, "/") {
		return "", errInvalidAppACLR2FinalizeConfiguration
	}

	parameters, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", errInvalidAppACLR2FinalizeConfiguration
	}
	for name, values := range parameters {
		if len(values) != 1 {
			return "", errInvalidAppACLR2FinalizeConfiguration
		}
		switch name {
		case "sslmode":
			if values[0] == "" {
				return "", errInvalidAppACLR2FinalizeConfiguration
			}
		default:
			return "", errInvalidAppACLR2FinalizeConfiguration
		}
	}
	sslModes := parameters["sslmode"]
	if len(sslModes) != 1 {
		return "", errInvalidAppACLR2FinalizeConfiguration
	}
	switch sslModes[0] {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return "", errInvalidAppACLR2FinalizeConfiguration
	}

	// ParseConfig merges OS and home-directory defaults before a URL's values.
	// Carry explicit empty file paths in the canonical URL so it cannot read a
	// service file, passfile, or TLS material from the caller's environment or
	// home directory. The route passes only this canonical value to its opener.
	canonicalParameters := url.Values{
		"passfile":    {""},
		"servicefile": {""},
		"sslcert":     {""},
		"sslkey":      {""},
		"sslmode":     {sslModes[0]},
		"sslpassword": {""},
		"sslrootcert": {""},
	}
	parsed.RawQuery = canonicalParameters.Encode()
	return appACLR2FinalizeDatabaseURL(parsed.String()), nil
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

func parseAppACLR2FinalizeArgs(args []string) error {
	if len(args) == 0 || args[0] != "finalize" {
		return errInvalidAppACLR2FinalizeInvocation
	}

	flags := flag.NewFlagSet("houfeng-record-platform-admin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scope := flags.String("scope", "", "")
	if err := flags.Parse(args[1:]); err != nil {
		return errInvalidAppACLR2FinalizeInvocation
	}
	if len(args) != 3 || args[1] != "--scope" || args[2] != "app-acl-r2" || *scope != "app-acl-r2" || flags.NArg() != 0 {
		return errInvalidAppACLR2FinalizeInvocation
	}
	return nil
}
