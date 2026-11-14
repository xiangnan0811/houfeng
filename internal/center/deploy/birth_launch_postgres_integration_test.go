//go:build linux

package deploy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordbirth"
)

// This fixture exercises the real initializer, restricted runtime admission and
// file publisher on an isolated APP database. It covers the never-installed
// path only; installed W2 continuity and a complete Compose lifecycle require
// their own real UID/source/reader fixture.
func TestPostgresIntegrationComposeBirthLaunchPublication(t *testing.T) {
	fixtureRoot := os.Getenv("HOUFENG_BIRTH_LAUNCH_FIXTURE_ROOT")
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" || fixtureRoot == "" {
		t.Skip("requires strict PostgreSQL runner and protected UID launch fixture")
	}
	if os.Getuid() != composeLaunchPublisherUID || os.Geteuid() != composeLaunchPublisherUID || !filepath.IsAbs(fixtureRoot) || filepath.Clean(fixtureRoot) != fixtureRoot ||
		os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" {
		t.Fatal("launch fixture requires UID10002, isolated protected path and ownership-checked PostgreSQL")
	}
	ctx := context.Background()
	baseConfig, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DATABASE_URL"))
	if err != nil || baseConfig.ConnConfig.User != "postgres" {
		t.Fatal("launch fixture requires the strict direct postgres connection")
	}
	bootstrap, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bootstrap.Close)
	assertComposeIntegrationFixtureHasNoProductionState(t, ctx, bootstrap)
	config := uniqueComposeIntegrationConfig(t, baseConfig.ConnConfig.Password)
	directory, err := os.MkdirTemp(fixtureRoot, "birth-launch-pg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	centerDirectory := filepath.Join(directory, "center-config")
	if err := os.Mkdir(centerDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	config.AuthorityStateRoot = filepath.Join(directory, "authority")
	config.CenterDeploymentIDPath = filepath.Join(centerDirectory, "deployment-id")
	if _, err := bootstrap.Exec(ctx, formatComposeIntegrationDDL(t, ctx, bootstrap, `CREATE DATABASE %I`, config.DatabaseName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupComposeIntegrationDatabase(t, context.Background(), bootstrap, config) })
	open := func(ctx context.Context, endpoint composePostgresEndpoint) (*pgxpool.Pool, error) {
		connection, err := composeIntegrationEndpointConfig(baseConfig, endpoint)
		if err != nil {
			return nil, err
		}
		pool, err := pgxpool.NewWithConfig(ctx, connection)
		if err != nil {
			return nil, err
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, err
		}
		return pool, nil
	}
	deps := defaultComposeInitDependencies(open)
	controlPath := filepath.Join(directory, "source-authority.json")
	deps.withMaintenance = func(ctx context.Context, app *pgxpool.Pool, fn func(*SourceMaintenanceConfig) error) error {
		return WithSourceMaintenance(ctx, app, controlPath, false, fn)
	}
	if err := initializeComposeWithDependencies(ctx, config, deps); err != nil {
		t.Fatalf("first protected launch initialization: %v", err)
	}
	centerPath := filepath.Join(centerDirectory, composeCenterBirthLaunchFile)
	before, err := os.Stat(centerPath)
	if err != nil {
		t.Fatal(err)
	}
	center, digest, err := recordbirth.ReadPublishedLaunchBinding(centerPath, "center")
	if err != nil || center.BirthInstalled || center.SourceGeneration != 0 {
		t.Fatalf("strict ordinary center binding: %v", err)
	}
	if _, _, err := recordbirth.ReadPublishedLaunchBinding(filepath.Join(centerDirectory, composeProcessorBirthLaunchFile), "content-processor"); err != nil {
		t.Fatalf("strict processor binding: %v", err)
	}
	if err := initializeComposeWithDependencies(ctx, config, deps); err != nil {
		t.Fatalf("exact protected launch replay: %v", err)
	}
	after, err := os.Stat(centerPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("exact initialization replay replaced the launch file")
	}
	owner, err := open(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.Roles.Migrator, Password: config.Passwords.Migrator})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var revision int64
	if err := owner.QueryRow(ctx, `select revision from record_platform_internal.record_birth_launch_state where singleton`).Scan(&revision); err != nil || revision <= 0 {
		t.Fatalf("read launch revision: %v", err)
	}
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, revision); err != nil {
		t.Fatal(err)
	}
	if err := initializeComposeWithDependencies(ctx, config, deps); err != nil {
		t.Fatalf("resume exact closed ordinary publication: %v", err)
	}
	var currentRevision int64
	var opened bool
	if err := owner.QueryRow(ctx, `select revision,opened from record_platform_internal.record_birth_launch_state where singleton`).Scan(&currentRevision, &opened); err != nil || currentRevision != revision || !opened {
		t.Fatalf("ordinary replay changed revision or failed to open: %v", err)
	}
	// Owner-level corruption here is a negative fixture, never a provisioning path.
	if _, err := owner.Exec(ctx, `update record_platform_internal.record_birth_launch_state set runtime_principal=$1::name where singleton`, config.Roles.PlatformAdmin); err != nil {
		t.Fatal(err)
	}
	if err := initializeComposeWithDependencies(ctx, config, deps); err == nil {
		t.Fatal("initializer accepted a changed registered runtime principal")
	}
	var principal string
	if err := owner.QueryRow(ctx, `select runtime_principal::text from record_platform_internal.record_birth_launch_state where singleton`).Scan(&principal); err != nil || principal != config.Roles.PlatformAdmin {
		t.Fatal("rejected initialization rewrote the changed registration")
	}
	if _, err := owner.Exec(ctx, `update record_platform_internal.record_birth_launch_state set runtime_principal=$1::name where singleton`, config.Roles.CenterRuntime); err != nil {
		t.Fatal(err)
	}
	state, err := recordauthority.LoadComposeState(config.AuthorityStateRoot)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := open(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.AuthorityRole, Password: state.DatabasePassword()})
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	if _, err := inspectPublishedComposeBirthLaunch(ctx, authority, state, nil, centerDirectory, true); err != nil {
		t.Fatalf("constrained authority launch readback: %v", err)
	}
	wrong := digest
	wrong[0] ^= 0xff
	runtime, err := open(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.Roles.CenterRuntime, Password: config.Passwords.Runtime, LaunchDigest: wrong})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	var generation int64
	if err := runtime.QueryRow(ctx, `select public.source_application_admit()`).Scan(&generation); err == nil {
		t.Fatal("runtime admission accepted an unregistered launch digest")
	}
	if err := os.Chmod(centerPath, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := initializeComposeWithDependencies(ctx, config, deps); err == nil {
		t.Fatal("initializer accepted unsafe existing launch permissions")
	}
}
