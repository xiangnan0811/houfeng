//go:build linux && !cgo

package main

import (
	"bytes"
	"context"
	"math"
	"net"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/store/migrate"
)

type backendIdentity struct {
	pid     int32
	started time.Time
	role    string
}

func openMigrator(ctx context.Context, input privateConfig) (*pgxpool.Pool, backendIdentity, error) {
	var none backendIdentity
	config, err := platformmigrate.ParseSourceAuthorityPostgresConfig(input.migratorURL)
	if err != nil || config.ConnConfig.Database != input.application.databaseName ||
		net.ParseIP(config.ConnConfig.Host) == nil || len(config.ConnConfig.Fallbacks) != 0 {
		return nil, none, denied
	}
	role := config.ConnConfig.User
	roles := platformmigrate.AppRoleSetV2{
		CenterRuntime: input.runtimeRole, PlatformAdmin: input.adminRole,
		DeletionWorker: input.workerRole, Migrator: role,
	}
	if roles.Validate() != nil {
		return nil, none, denied
	}
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline || ctx.Err() != nil {
		return nil, none, denied
	}
	connectTimeout := time.Until(deadline)
	if connectTimeout <= 0 {
		return nil, none, denied
	}
	if connectTimeout > 10*time.Second {
		connectTimeout = 10 * time.Second
	}
	config.ConnConfig.ConnectTimeout = connectTimeout
	config.ConnConfig.RuntimeParams = map[string]string{"application_name": "g2_c81_migrator"}
	config.MinConns = 0
	config.MinIdleConns = 0
	config.MaxConns = 1
	// A zero duration in pgx means immediate expiry, not disabled recycling.
	// These durations exceed the hard deadline of this one-shot process.
	config.MaxConnLifetime = time.Duration(math.MaxInt64)
	config.MaxConnLifetimeJitter = 0
	config.MaxConnIdleTime = time.Duration(math.MaxInt64)
	config.HealthCheckPeriod = time.Duration(math.MaxInt64)
	config.ShouldPing = func(context.Context, pgxpool.ShouldPingParams) bool { return false }
	var attempted atomic.Bool
	var original atomic.Pointer[pgx.Conn]
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
		if !attempted.CompareAndSwap(false, true) {
			return denied
		}
		return nil
	}
	config.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		if !original.CompareAndSwap(nil, conn) {
			return denied
		}
		return nil
	}
	config.PrepareConn = func(_ context.Context, conn *pgx.Conn) (bool, error) {
		if conn != original.Load() || conn.IsClosed() {
			return false, denied
		}
		return true, nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, none, denied
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		return nil, none, denied
	}
	identity, err := observeBackend(ctx, conn.Conn(), input, role)
	conn.Release()
	if err != nil {
		pool.Close()
		return nil, none, denied
	}
	return pool, identity, nil
}

func observeBackend(ctx context.Context, conn *pgx.Conn, input privateConfig, expectedRole string) (backendIdentity, error) {
	var actual backendIdentity
	var session, current, database, activityRole string
	var oid uint32
	err := conn.QueryRow(ctx, `
		select pg_catalog.pg_backend_pid(), a.backend_start, session_user::text,
		       current_user::text, pg_catalog.current_database()::text, d.oid, a.usename::text
		from pg_catalog.pg_stat_activity a
		join pg_catalog.pg_database d on d.datname = pg_catalog.current_database()
		where a.pid = pg_catalog.pg_backend_pid()`).Scan(
		&actual.pid, &actual.started, &session, &current, &database, &oid, &activityRole)
	if err != nil || actual.pid <= 0 || uint32(actual.pid) != conn.PgConn().PID() || actual.started.IsZero() ||
		actual.started.Nanosecond()%int(time.Microsecond) != 0 || session != expectedRole || current != session ||
		activityRole != session || database != input.application.databaseName || oid != input.application.databaseOID {
		return backendIdentity{}, denied
	}
	actual.started = actual.started.UTC()
	actual.role = session
	return actual, nil
}

func checkBackend(ctx context.Context, conn *pgx.Conn, input privateConfig, expected backendIdentity) error {
	actual, err := observeBackend(ctx, conn, input, expected.role)
	if err != nil || actual.pid != expected.pid || actual.role != expected.role || !actual.started.Equal(expected.started) {
		return denied
	}
	return nil
}

func converge(ctx context.Context, pool *pgxpool.Pool, input privateConfig) (migrate.AppACLManifestPersistedV1, error) {
	manifest, err := migrate.ConvergeAppACLCurrent(ctx, pool, input.runtimeRole, input.adminRole, input.workerRole)
	if err != nil || manifest.Validate() != nil || manifest.ManifestDigest == ([32]byte{}) {
		return migrate.AppACLManifestPersistedV1{}, denied
	}
	return manifest, nil
}

func readManifest(ctx context.Context, conn *pgx.Conn, manifest migrate.AppACLManifestPersistedV1) error {
	var revision int64
	var digest []byte
	if conn.QueryRow(ctx, `select manifest_revision, manifest_digest
		from public.app_acl_manifest_head where singleton`).Scan(&revision, &digest) != nil ||
		revision <= 0 || uint64(revision) != manifest.ManifestRevision || !bytes.Equal(digest, manifest.ManifestDigest[:]) {
		return denied
	}
	return nil
}
