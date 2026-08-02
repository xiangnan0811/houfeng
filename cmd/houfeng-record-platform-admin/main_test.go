package main

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/store/migrate"
)

type fakeAppACLR2FinalizePoolHandle struct {
	pool       *pgxpool.Pool
	ping       func(context.Context) error
	closeCalls int
}

func (pool *fakeAppACLR2FinalizePoolHandle) Ping(ctx context.Context) error {
	if pool.ping == nil {
		return nil
	}
	return pool.ping(ctx)
}

func (pool *fakeAppACLR2FinalizePoolHandle) Close() {
	pool.closeCalls++
}

func (pool *fakeAppACLR2FinalizePoolHandle) postgresPool() *pgxpool.Pool {
	return pool.pool
}

func TestNoLegacyR2TokenMatcherDoesNotTreatVersionedR2CodecsAsLegacy(t *testing.T) {
	camel := func(parts ...string) string { return strings.Join(parts, "") }
	tests := []struct {
		token    string
		contents string
		want     bool
	}{
		{
			token:    camel("App", "ACL", "Privilege", "Set", "R2"),
			contents: camel("App", "ACL", "Privilege", "Set", "R2"),
			want:     true,
		},
		{
			token:    camel("App", "ACL", "Privilege", "Set", "R2"),
			contents: camel("App", "ACL", "Privilege", "Set", "R2", "V1"),
			want:     false,
		},
		{
			token:    camel("Compile", "App", "ACL", "Privilege", "Set", "R2"),
			contents: camel("Compile", "App", "ACL", "Privilege", "Set", "R2"),
			want:     true,
		},
		{
			token:    camel("Compile", "App", "ACL", "Privilege", "Set", "R2"),
			contents: camel("Compile", "App", "ACL", "Privilege", "Set", "R2", "V1"),
			want:     false,
		},
	}

	for _, tt := range tests {
		if got := legacyR2TokenPattern(tt.token).MatchString(tt.contents); got != tt.want {
			t.Errorf("legacy R2 token match for %q in %q = %t, want %t", tt.token, tt.contents, got, tt.want)
		}
	}
}

func TestNoLegacyR2TokenSearchDistinguishesMatchesNoMatchesAndErrors(t *testing.T) {
	camel := func(parts ...string) string { return strings.Join(parts, "") }
	tests := []struct {
		token    string
		isolated string
	}{
		{
			token:    camel("App", "ACL", "Privilege", "Set", "R2"),
			isolated: camel("App", "ACL", "Privilege", "Set", "R2", "V1"),
		},
		{
			token:    camel("Compile", "App", "ACL", "Privilege", "Set", "R2"),
			isolated: camel("Compile", "App", "ACL", "Privilege", "Set", "R2", "V1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "r2.go")
			if err := os.WriteFile(source, []byte(tt.isolated), 0o600); err != nil {
				t.Fatalf("write isolated R2 source: %v", err)
			}

			matched, err := legacyR2TokenSearch(tt.token, source)
			if err != nil {
				t.Fatalf("search isolated R2 source: %v", err)
			}
			if matched {
				t.Fatalf("search isolated R2 source matched obsolete token %q", tt.token)
			}

			if err := os.WriteFile(source, []byte(tt.token), 0o600); err != nil {
				t.Fatalf("write obsolete R2 source: %v", err)
			}
			matched, err = legacyR2TokenSearch(tt.token, source)
			if err != nil {
				t.Fatalf("search obsolete R2 source: %v", err)
			}
			if !matched {
				t.Fatalf("search obsolete R2 source did not match token %q", tt.token)
			}
		})
	}

	if _, err := legacyR2TokenSearch(tests[0].token, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("search missing source path error = nil, want rg failure")
	}
}

func TestNoLegacyR2TokenSearchIgnoresHostRipgrepConfig(t *testing.T) {
	token := strings.Join([]string{"App", "ACL", "Privilege", "Set", "R2"}, "")
	root := t.TempDir()
	source := filepath.Join(root, "r2.go")
	if err := os.WriteFile(source, []byte(token), 0o600); err != nil {
		t.Fatalf("write obsolete R2 source: %v", err)
	}
	config := filepath.Join(root, "ripgreprc")
	if err := os.WriteFile(config, []byte("--fixed-strings\n"), 0o600); err != nil {
		t.Fatalf("write hostile ripgrep configuration: %v", err)
	}
	t.Setenv("RIPGREP_CONFIG_PATH", config)

	matched, err := legacyR2TokenSearch(token, source)
	if err != nil {
		t.Fatalf("search obsolete R2 source with hostile ripgrep configuration: %v", err)
	}
	if !matched {
		t.Fatalf("search obsolete R2 source did not match token %q with hostile ripgrep configuration", token)
	}
}

func TestNoLegacyR2TokenSearchIncludesIgnoredFiles(t *testing.T) {
	token := strings.Join([]string{"App", "ACL", "Privilege", "Set", "R2"}, "")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ignored.go"), []byte(token), 0o600); err != nil {
		t.Fatalf("write ignored obsolete R2 source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ignore"), []byte("ignored.go\n"), 0o600); err != nil {
		t.Fatalf("write ripgrep ignore file: %v", err)
	}

	matched, err := legacyR2TokenSearch(token, root)
	if err != nil {
		t.Fatalf("search ignored obsolete R2 source: %v", err)
	}
	if !matched {
		t.Fatalf("search ignored obsolete R2 source did not match token %q", token)
	}
}

func TestNoLegacyR2TokenSearchIncludesHiddenFiles(t *testing.T) {
	token := strings.Join([]string{"App", "ACL", "Privilege", "Set", "R2"}, "")
	root := t.TempDir()
	source := filepath.Join(root, ".obsolete", "r2.go")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatalf("create hidden source directory: %v", err)
	}
	if err := os.WriteFile(source, []byte(token), 0o600); err != nil {
		t.Fatalf("write hidden obsolete R2 source: %v", err)
	}

	matched, err := legacyR2TokenSearch(token, root)
	if err != nil {
		t.Fatalf("search hidden obsolete R2 source: %v", err)
	}
	if !matched {
		t.Fatalf("search hidden obsolete R2 source did not match token %q", token)
	}
}

func TestNoLegacyR2TokenSearchDoesNotRequireRipgrep(t *testing.T) {
	token := strings.Join([]string{"App", "ACL", "Privilege", "Set", "R2"}, "")
	source := filepath.Join(t.TempDir(), "r2.go")
	if err := os.WriteFile(source, []byte(token), 0o600); err != nil {
		t.Fatalf("write obsolete R2 source: %v", err)
	}
	t.Setenv("PATH", t.TempDir())

	matched, err := legacyR2TokenSearch(token, source)
	if err != nil {
		t.Fatalf("search obsolete R2 source without ripgrep: %v", err)
	}
	if !matched {
		t.Fatalf("search obsolete R2 source without ripgrep did not match token %q", token)
	}
}

func TestNoLegacyR2RoutesAndEnvironmentLookups(t *testing.T) {
	root := filepath.Join("..", "..")
	underscore := func(parts ...string) string { return strings.Join(parts, "_") }
	camel := func(parts ...string) string { return strings.Join(parts, "") }
	forbidden := []string{
		underscore("0052", "add", "app", "extension", "hardening", "receipt"),
		underscore("app", "extension", "hardening", "receipt"),
		camel("App", "Extension", "Hardening"),
		camel("App", "Extension", "Hardener"),
		camel("app", "Extension", "Hardening"),
		camel("app", "Extension", "Hardener"),
		camel("app", "ACL", "R2", "Migration", "Source", "Contract"),
		camel("App", "ACL", "R2", "Frozen", "Source", "Snapshot"),
		camel("validate", "App", "ACL", "R2", "Frozen", "Source", "Snapshot"),
		camel("App", "ACL", "Privilege", "Set", "R2"),
		camel("app", "ACL", "Privileges", "R2"),
		camel("Compile", "App", "ACL", "Privilege", "Set", "R2"),
		underscore("HOUFENG", "RECORD", "PLATFORM", "APP", "EXTENSION", "HARDENER", "DATABASE", "URL"),
		underscore("HOUFENG", "RECORD", "PLATFORM", "APP", "EXTENSION", "HARDENER", "ROLE"),
		underscore("APP", "EXTENSION", "HARDENING"),
		underscore("APP", "EXTENSION", "HARDENER"),
		strings.Join([]string{"app", "extension", "hardening"}, "-"),
	}
	if _, err := os.Stat(filepath.Join(root, "db", "migrations", forbidden[0]+".sql")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("obsolete root migration presence check error = %v, want not exist", err)
	}
	searchPaths := []string{
		filepath.Join(root, "db"),
		filepath.Join(root, "internal"),
		filepath.Join(root, "cmd"),
	}
	for _, token := range forbidden {
		matched, err := legacyR2TokenSearch(token, searchPaths...)
		if err != nil {
			t.Fatalf("search obsolete APP root token %q: %v", token, err)
		}
		if matched {
			t.Errorf("obsolete APP root token %q remains", token)
		}
	}
}

func legacyR2TokenPattern(token string) *regexp.Regexp {
	return regexp.MustCompile(`(?:^|[^[:alnum:]_])` + regexp.QuoteMeta(token) + `(?:$|[^[:alnum:]_])`)
}

func legacyR2TokenSearch(token string, searchPaths ...string) (bool, error) {
	pattern := legacyR2TokenPattern(token)
	matched := false
	for _, searchPath := range searchPaths {
		err := filepath.WalkDir(searchPath, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !entry.Type().IsRegular() {
				return nil
			}

			// The gate intentionally scans dot-prefixed paths as well as visible files.
			contents, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %q: %w", path, err)
			}
			if pattern.Match(contents) {
				matched = true
			}
			return nil
		})
		if err != nil {
			return false, fmt.Errorf("search obsolete APP root token %q at %q failed: %w", token, searchPath, err)
		}
	}
	return matched, nil
}

func TestMigrateAppRejectsMissingAllowedConfigurationWithoutLeakingOtherDSN(t *testing.T) {
	const forbiddenDSN = "postgres://owner:outside-secret@example.invalid/houfeng"

	command := exec.Command("go", "run", ".", "migrate", "--scope", "app")
	command.Env = append(environmentWithoutHoufengVariables(), "HOUFENG_DATABASE_URL="+forbiddenDSN)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("migrate --scope app unexpectedly accepted missing APP-only configuration")
	}
	if !strings.Contains(string(output), "app migration configuration is invalid") {
		t.Fatalf("migrate --scope app output = %q, want redacted configuration rejection", output)
	}
	if strings.Contains(string(output), forbiddenDSN) {
		t.Fatalf("migrate --scope app output leaked forbidden DSN: %q", output)
	}
}

func TestMigrateAppMasksMigratorPoolOpenFailure(t *testing.T) {
	const migratorDSN = "postgres://migrator:app-only-secret@%zz/houfeng"
	t.Setenv("HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL", migratorDSN)
	t.Setenv("HOUFENG_RECORD_PLATFORM_APP_RUNTIME_ROLE", "houfeng_center_runtime")
	t.Setenv("HOUFENG_RECORD_PLATFORM_APP_ADMIN_ROLE", "houfeng_platform_admin")

	err := run([]string{"migrate", "--scope", "app"})
	if err == nil {
		t.Fatal("run() unexpectedly accepted a malformed APP migrator DSN")
	}
	if got, want := err.Error(), "open app migrator PostgreSQL connection failed"; got != want {
		t.Fatalf("run() error = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), migratorDSN) {
		t.Fatalf("run() error leaked APP migrator DSN: %q", err)
	}
}

func TestDefaultAppMigrationDependenciesUseCurrentConvergence(t *testing.T) {
	deps := defaultAppMigrationDependencies()

	err := deps.converge(t.Context(), nil, "houfeng_center_runtime", "houfeng_platform_admin")
	if err == nil {
		t.Fatal("default converge() error = nil, want nil-pool rejection")
	}
	if got, want := err.Error(), "current app ACL convergence has no PostgreSQL pool"; got != want {
		t.Fatalf("default converge() error = %q, want %q", got, want)
	}
}

func TestMigrateAppRejectsForbiddenArgumentsBeforeLoadingEnvironment(t *testing.T) {
	for _, args := range [][]string{
		{"status", "--scope", "app"},
		{"migrate", "--scope", "witness"},
		{"migrate", "--scope", "app", "positional-secret"},
		{"migrate", "--scope", "app", "--ledger-database-url", "postgres://ledger:secret@example.invalid/ledger"},
		{"migrate", "--witness", "--scope", "app"},
		{"migrate", "--scope", "app", "--unknown"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			lookupCalls := 0
			err := runWithDeps(t.Context(), args, appMigrationDependencies{
				lookupEnv: func(string) (string, bool) {
					lookupCalls++
					return "", false
				},
				openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
					t.Fatal("forbidden invocation opened a PostgreSQL pool")
					return nil, nil
				},
				closePool: func(*pgxpool.Pool) {
					t.Fatal("forbidden invocation closed an unopened PostgreSQL pool")
				},
				converge: func(context.Context, *pgxpool.Pool, string, string) error {
					t.Fatal("forbidden invocation attempted APP convergence")
					return nil
				},
			})
			if !errors.Is(err, errInvalidAppMigrationInvocation) {
				t.Fatalf("runWithDeps(%q) error = %v, want invalid APP invocation", args, err)
			}
			if lookupCalls != 0 {
				t.Fatalf("runWithDeps(%q) read %d environment values before rejection", args, lookupCalls)
			}
		})
	}
}

func TestMigrateAppClosesPoolAfterSuccessfulConvergence(t *testing.T) {
	const migratorDSN = "postgres://migrator:app-only-secret@example.invalid/houfeng"
	pool := &pgxpool.Pool{}
	closed := 0
	converged := 0

	err := runWithDeps(t.Context(), []string{"migrate", "--scope", "app"}, appMigrationDependencies{
		lookupEnv: appScopeLookup(t, migratorDSN, "houfeng_center_runtime", "houfeng_platform_admin"),
		openPostgres: func(_ context.Context, gotDSN string) (*pgxpool.Pool, error) {
			if gotDSN != migratorDSN {
				t.Fatalf("openPostgres() DSN = %q, want APP migrator DSN", gotDSN)
			}
			return pool, nil
		},
		closePool: func(gotPool *pgxpool.Pool) {
			if gotPool != pool {
				t.Fatalf("closePool() pool = %p, want opened pool %p", gotPool, pool)
			}
			closed++
		},
		converge: func(_ context.Context, gotPool *pgxpool.Pool, runtimeRole, adminRole string) error {
			if gotPool != pool || runtimeRole != "houfeng_center_runtime" || adminRole != "houfeng_platform_admin" {
				t.Fatalf("converge() arguments = (%p, %q, %q), want opened pool and APP roles", gotPool, runtimeRole, adminRole)
			}
			converged++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runWithDeps() error = %v, want nil", err)
	}
	if converged != 1 || closed != 1 {
		t.Fatalf("converged=%d closed=%d, want one convergence and one close", converged, closed)
	}
}

func TestMigrateAppClosesPoolAndMasksConvergenceFailure(t *testing.T) {
	const migratorDSN = "postgres://migrator:app-only-secret@example.invalid/houfeng"
	pool := &pgxpool.Pool{}
	closed := 0
	convergenceErr := errors.New("database error for " + migratorDSN)

	err := runWithDeps(t.Context(), []string{"migrate", "--scope", "app"}, appMigrationDependencies{
		lookupEnv: appScopeLookup(t, migratorDSN, "houfeng_center_runtime", "houfeng_platform_admin"),
		openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
			return pool, nil
		},
		closePool: func(*pgxpool.Pool) {
			closed++
		},
		converge: func(context.Context, *pgxpool.Pool, string, string) error {
			return convergenceErr
		},
	})
	if !errors.Is(err, errConvergeAppMigration) {
		t.Fatalf("runWithDeps() error = %v, want redacted convergence error", err)
	}
	if errors.Is(err, convergenceErr) {
		t.Fatalf("runWithDeps() error retained arbitrary database failure: %v", err)
	}
	if strings.Contains(err.Error(), migratorDSN) {
		t.Fatalf("runWithDeps() error leaked APP migrator DSN: %q", err)
	}
	if closed != 1 {
		t.Fatalf("closePool() calls = %d, want one after convergence failure", closed)
	}
}

func TestMigrateAppPreservesOnlyRebuildRequiredConvergenceCause(t *testing.T) {
	const migratorDSN = "postgres://migrator:app-only-secret@example.invalid/houfeng"
	pool := &pgxpool.Pool{}
	closed := 0
	convergenceErr := fmt.Errorf(
		"inspect prior baseline for %s: %w",
		migratorDSN,
		migrate.ErrDevelopmentDatabaseRebuildRequired,
	)

	err := runWithDeps(t.Context(), []string{"migrate", "--scope", "app"}, appMigrationDependencies{
		lookupEnv: appScopeLookup(t, migratorDSN, "houfeng_center_runtime", "houfeng_platform_admin"),
		openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
			return pool, nil
		},
		closePool: func(*pgxpool.Pool) {
			closed++
		},
		converge: func(context.Context, *pgxpool.Pool, string, string) error {
			return convergenceErr
		},
	})
	if !errors.Is(err, errConvergeAppMigration) {
		t.Fatalf("runWithDeps() error = %v, want command convergence error", err)
	}
	if !errors.Is(err, migrate.ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("runWithDeps() error = %v, want rebuild-required cause", err)
	}
	if got, want := err.Error(), errConvergeAppMigration.Error()+": "+migrate.ErrDevelopmentDatabaseRebuildRequired.Error(); got != want {
		t.Fatalf("runWithDeps() error = %q, want safe actionable error %q", got, want)
	}
	if strings.Contains(err.Error(), migratorDSN) {
		t.Fatalf("runWithDeps() error leaked APP migrator DSN: %q", err)
	}
	if closed != 1 {
		t.Fatalf("closePool() calls = %d, want one after rebuild-required failure", closed)
	}
}

func TestMigrateAppClosesReturnedPoolWhenOpeningFails(t *testing.T) {
	const migratorDSN = "postgres://migrator:app-only-secret@example.invalid/houfeng"
	pool := &pgxpool.Pool{}
	closed := 0

	err := runWithDeps(t.Context(), []string{"migrate", "--scope", "app"}, appMigrationDependencies{
		lookupEnv: appScopeLookup(t, migratorDSN, "houfeng_center_runtime", "houfeng_platform_admin"),
		openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
			return pool, errors.New("open failure for " + migratorDSN)
		},
		closePool: func(*pgxpool.Pool) {
			closed++
		},
		converge: func(context.Context, *pgxpool.Pool, string, string) error {
			t.Fatal("pool-open failure attempted APP convergence")
			return nil
		},
	})
	if !errors.Is(err, errOpenAppMigratorPool) {
		t.Fatalf("runWithDeps() error = %v, want redacted pool-open error", err)
	}
	if strings.Contains(err.Error(), migratorDSN) {
		t.Fatalf("runWithDeps() error leaked APP migrator DSN: %q", err)
	}
	if closed != 1 {
		t.Fatalf("closePool() calls = %d, want one after opener failure", closed)
	}
}

func TestBootstrapAppACLR2RouteAcceptsExactRouteAndUsesOnlyBootstrapDSN(t *testing.T) {
	const bootstrapDSN = "postgres://bootstrap:bootstrap-secret@example.invalid/houfeng"
	pool := &pgxpool.Pool{}
	opened := 0
	closed := 0
	bootstrapped := 0

	err := runAdminWithDeps(t.Context(), []string{"bootstrap", "--scope", "app-acl-r2"}, appAdminDependencies{
		migration: appMigrationDependencies{
			lookupEnv: func(string) (string, bool) {
				t.Fatal("bootstrap route attempted app migration environment lookup")
				return "", false
			},
			openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
				t.Fatal("bootstrap route attempted app migration pool open")
				return nil, nil
			},
			closePool: func(*pgxpool.Pool) {
				t.Fatal("bootstrap route attempted app migration pool close")
			},
			converge: func(context.Context, *pgxpool.Pool, string, string) error {
				t.Fatal("bootstrap route attempted frozen R1 convergence")
				return nil
			},
		},
		bootstrap: appACLR2BootstrapDependencies{
			lookupEnv: func(key string) (string, bool) {
				if key != platformmigrate.AppBootstrapDatabaseURLEnv {
					t.Fatalf("bootstrap route looked up forbidden environment key %q", key)
				}
				return bootstrapDSN, true
			},
			openPostgres: func(_ context.Context, gotDSN string) (*pgxpool.Pool, error) {
				if gotDSN != bootstrapDSN {
					t.Fatalf("openPostgres() DSN = %q, want bootstrap DSN", gotDSN)
				}
				opened++
				return pool, nil
			},
			closePool: func(gotPool *pgxpool.Pool) {
				if gotPool != pool {
					t.Fatalf("closePool() pool = %p, want opened pool %p", gotPool, pool)
				}
				closed++
			},
			bootstrap: func(_ context.Context, gotPool *pgxpool.Pool) error {
				if gotPool != pool {
					t.Fatalf("bootstrap() pool = %p, want opened pool %p", gotPool, pool)
				}
				bootstrapped++
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("runAdminWithDeps() error = %v", err)
	}
	if opened != 1 || bootstrapped != 1 || closed != 1 {
		t.Fatalf("opened=%d bootstrapped=%d closed=%d, want one of each", opened, bootstrapped, closed)
	}
}

func TestBootstrapAppACLR2FailureLogNamesAdminCommand(t *testing.T) {
	const bootstrapDSN = "postgres://bootstrap:bootstrap-secret@%zz/houfeng"
	command := exec.Command("go", "run", ".", "bootstrap", "--scope", "app-acl-r2")
	command.Env = append(environmentWithoutHoufengVariables(), platformmigrate.AppBootstrapDatabaseURLEnv+"="+bootstrapDSN)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("bootstrap --scope app-acl-r2 unexpectedly accepted a malformed bootstrap DSN")
	}
	if !strings.Contains(string(output), "record platform admin command failed") {
		t.Fatalf("bootstrap failure output = %q, want generic admin-command log message", output)
	}
	if strings.Contains(string(output), "record platform app migration failed") {
		t.Fatalf("bootstrap failure output mislabeled the command as an APP migration: %q", output)
	}
	if strings.Contains(string(output), bootstrapDSN) {
		t.Fatalf("bootstrap failure output leaked bootstrap DSN: %q", output)
	}
}

func TestBootstrapAppACLR2RouteRejectsForbiddenArgumentsBeforeLookupOrPoolOpen(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		wantErr error
	}{
		{args: []string{"bootstrap"}, wantErr: errInvalidAppACLR2BootstrapInvocation},
		{args: []string{"bootstrap", "--scope", "app"}, wantErr: errInvalidAppACLR2BootstrapInvocation},
		{args: []string{"bootstrap", "--scope", "app-acl-r2", "positional-secret"}, wantErr: errInvalidAppACLR2BootstrapInvocation},
		{args: []string{"bootstrap", "--scope", "app-acl-r2", "--role", "bootstrap"}, wantErr: errInvalidAppACLR2BootstrapInvocation},
		{args: []string{"bootstrap", "--scope", "app-acl-r2", "--bootstrap-database-url", "postgres://bootstrap:secret@example.invalid/houfeng"}, wantErr: errInvalidAppACLR2BootstrapInvocation},
		{args: []string{"bootstrap", "--scope", "app-acl-r2", "--bootstrap-database-url-file", "/tmp/secret"}, wantErr: errInvalidAppACLR2BootstrapInvocation},
		{args: []string{"bootstrap", "--scope", "app-acl-r2", "--unknown"}, wantErr: errInvalidAppACLR2BootstrapInvocation},
		{args: []string{"finalize", "--scope", "app-acl-r2"}, wantErr: errInvalidAppACLR2FinalizeInvocation},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			lookupCalls := 0
			err := runAdminWithDeps(t.Context(), tc.args, appAdminDependencies{
				bootstrap: appACLR2BootstrapDependencies{
					lookupEnv: func(string) (string, bool) {
						lookupCalls++
						return "", false
					},
					openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
						t.Fatal("invalid bootstrap invocation opened a PostgreSQL pool")
						return nil, nil
					},
					closePool: func(*pgxpool.Pool) {
						t.Fatal("invalid bootstrap invocation closed an unopened PostgreSQL pool")
					},
					bootstrap: func(context.Context, *pgxpool.Pool) error {
						t.Fatal("invalid bootstrap invocation performed bootstrap")
						return nil
					},
				},
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("runAdminWithDeps(%q) error = %v, want %v", tc.args, err, tc.wantErr)
			}
			if lookupCalls != 0 {
				t.Fatalf("runAdminWithDeps(%q) performed %d environment lookups before rejection", tc.args, lookupCalls)
			}
		})
	}
}

func TestBootstrapAppACLR2RouteClosesPoolAndRedactsOperationalErrors(t *testing.T) {
	const bootstrapDSN = "postgres://bootstrap:bootstrap-secret@example.invalid/houfeng"

	for _, tc := range []struct {
		name     string
		openErr  error
		bootErr  error
		wantErr  error
		wantBoot int
	}{
		{
			name:    "pool_open_failure",
			openErr: errors.New("open failed for " + bootstrapDSN),
			wantErr: errOpenAppACLR2BootstrapPool,
		},
		{
			name:     "bootstrap_failure",
			bootErr:  errors.New("bootstrap failed for " + bootstrapDSN),
			wantErr:  errBootstrapAppACLR2,
			wantBoot: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := &pgxpool.Pool{}
			closed := 0
			bootstrapped := 0
			err := runAppACLR2BootstrapWithDeps(t.Context(), []string{"bootstrap", "--scope", "app-acl-r2"}, appACLR2BootstrapDependencies{
				lookupEnv: func(key string) (string, bool) {
					if key != platformmigrate.AppBootstrapDatabaseURLEnv {
						t.Fatalf("bootstrap route looked up forbidden environment key %q", key)
					}
					return bootstrapDSN, true
				},
				openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
					return pool, tc.openErr
				},
				closePool: func(gotPool *pgxpool.Pool) {
					if gotPool != pool {
						t.Fatalf("closePool() pool = %p, want opened pool %p", gotPool, pool)
					}
					closed++
				},
				bootstrap: func(context.Context, *pgxpool.Pool) error {
					bootstrapped++
					return tc.bootErr
				},
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("runAppACLR2BootstrapWithDeps() error = %v, want %v", err, tc.wantErr)
			}
			if strings.Contains(err.Error(), bootstrapDSN) {
				t.Fatalf("runAppACLR2BootstrapWithDeps() error leaked bootstrap DSN: %q", err)
			}
			if closed != 1 {
				t.Fatalf("closePool() calls = %d, want one", closed)
			}
			if bootstrapped != tc.wantBoot {
				t.Fatalf("bootstrap() calls = %d, want %d", bootstrapped, tc.wantBoot)
			}
		})
	}
}

func TestAppACLR2FinalizeRouteAcceptsExactRouteAndUsesOnlyMigratorDSN(t *testing.T) {
	const migratorDSN = "postgres://direct_migrator:finalize-secret@example.invalid:5432/houfeng?sslmode=disable"
	clearAppACLR2FinalizeAmbientPostgresVariables(t)
	canonicalMigratorDSN, err := canonicalAppACLR2FinalizeDatabaseURL(migratorDSN)
	if err != nil {
		t.Fatalf("canonicalAppACLR2FinalizeDatabaseURL() error = %v", err)
	}
	pool := &pgxpool.Pool{}
	lookups := 0
	opened := 0
	closed := 0
	finalized := 0

	err = runAdminWithDeps(t.Context(), []string{"finalize", "--scope", "app-acl-r2"}, appAdminDependencies{
		migration: appMigrationDependencies{
			lookupEnv: func(string) (string, bool) {
				t.Fatal("finalize route attempted frozen R1 migration environment lookup")
				return "", false
			},
			openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
				t.Fatal("finalize route attempted frozen R1 migration pool open")
				return nil, nil
			},
			closePool: func(*pgxpool.Pool) {
				t.Fatal("finalize route attempted frozen R1 migration pool close")
			},
			converge: func(context.Context, *pgxpool.Pool, string, string) error {
				t.Fatal("finalize route attempted frozen R1 convergence")
				return nil
			},
		},
		bootstrap: appACLR2BootstrapDependencies{
			lookupEnv: func(string) (string, bool) {
				t.Fatal("finalize route attempted bootstrap environment lookup")
				return "", false
			},
			openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
				t.Fatal("finalize route attempted bootstrap pool open")
				return nil, nil
			},
			closePool: func(*pgxpool.Pool) {
				t.Fatal("finalize route attempted bootstrap pool close")
			},
			bootstrap: func(context.Context, *pgxpool.Pool) error {
				t.Fatal("finalize route attempted bootstrap")
				return nil
			},
		},
		finalize: appACLR2FinalizeDependencies{
			lookupEnv: func(key string) (string, bool) {
				lookups++
				if key != platformmigrate.AppMigratorDatabaseURLEnv {
					t.Fatalf("finalize route looked up forbidden environment key %q", key)
				}
				return migratorDSN, true
			},
			openPostgres: func(_ context.Context, gotDSN appACLR2FinalizeDatabaseURL) (*pgxpool.Pool, error) {
				if gotDSN != canonicalMigratorDSN {
					t.Fatalf("openPostgres() DSN = %q, want canonical migrator DSN %q", gotDSN, canonicalMigratorDSN)
				}
				opened++
				return pool, nil
			},
			closePool: func(gotPool *pgxpool.Pool) {
				if gotPool != pool {
					t.Fatalf("closePool() pool = %p, want opened pool %p", gotPool, pool)
				}
				closed++
			},
			finalize: func(_ context.Context, gotPool *pgxpool.Pool) error {
				if gotPool != pool {
					t.Fatalf("finalize() pool = %p, want opened pool %p", gotPool, pool)
				}
				finalized++
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("runAdminWithDeps() error = %v", err)
	}
	if lookups != 1 || opened != 1 || finalized != 1 || closed != 1 {
		t.Fatalf("lookups=%d opened=%d finalized=%d closed=%d, want one of each", lookups, opened, finalized, closed)
	}
}

func TestAppACLR2FinalizeUsesFinalizerSpecificPostgresOpener(t *testing.T) {
	dependencies := defaultAppACLR2FinalizeDependencies()
	if dependencies.openPostgres == nil {
		t.Fatal("default APP ACL R2 finalizer opener is nil")
	}

	want := reflect.ValueOf(productionAppACLR2FinalizePostgresOpener().open).Pointer()
	bypass := func(context.Context, appACLR2FinalizeDatabaseURL) (*pgxpool.Pool, error) {
		return nil, errors.New("generic PostgreSQL opener bypass")
	}
	if got := reflect.ValueOf(bypass).Pointer(); got == want {
		t.Fatal("concrete generic-opener bypass unexpectedly matches the finalizer-specific opener")
	}
	if got := reflect.ValueOf(dependencies.openPostgres).Pointer(); got != want {
		t.Fatalf("default APP ACL R2 finalizer opener pointer = %#x, want production opener method %#x", got, want)
	}
}

func TestAppACLR2FinalizeProductionPostgresOpenerPassesStrictConfigToControlledConstructor(t *testing.T) {
	const constructorFailure = "controlled constructor failure"
	clearAppACLR2FinalizeAmbientPostgresVariables(t)

	for _, sslMode := range []string{"verify-ca", "verify-full"} {
		t.Run(sslMode, func(t *testing.T) {
			canonical, err := canonicalAppACLR2FinalizeDatabaseURL(
				"postgres://direct_migrator:finalizer-secret@db.example:5444/houfeng?sslmode=" + sslMode,
			)
			if err != nil {
				t.Fatalf("canonicalAppACLR2FinalizeDatabaseURL() error = %v", err)
			}

			constructorErr := errors.New(constructorFailure)
			constructorCalls := 0
			opener := appACLR2FinalizePostgresOpener{
				newPool: func(ctx context.Context, config *pgxpool.Config) (appACLR2FinalizePoolHandle, error) {
					constructorCalls++
					if !errors.Is(ctx.Err(), context.Canceled) {
						t.Fatalf("controlled constructor context error = %v, want canceled", ctx.Err())
					}
					if config == nil || config.ConnConfig == nil {
						t.Fatal("controlled constructor received nil PostgreSQL config")
					}
					if got := config.ConnConfig.Host; got != "db.example" {
						t.Fatalf("PostgreSQL host = %q, want db.example", got)
					}
					if got := config.ConnConfig.Port; got != 5444 {
						t.Fatalf("PostgreSQL port = %d, want 5444", got)
					}
					if got := config.ConnConfig.User; got != "direct_migrator" {
						t.Fatalf("PostgreSQL user = %q, want direct_migrator", got)
					}
					if got := config.ConnConfig.Password; got != "finalizer-secret" {
						t.Fatalf("PostgreSQL password = %q, want explicit finalizer password", got)
					}
					if got := config.ConnConfig.Database; got != "houfeng" {
						t.Fatalf("PostgreSQL database = %q, want houfeng", got)
					}
					if got := config.ConnString(); got != string(canonical) {
						t.Fatalf("PostgreSQL config connection string = %q, want canonical finalizer URL %q", got, canonical)
					}
					parsedConfigURL, err := url.Parse(config.ConnString())
					if err != nil {
						t.Fatalf("parse captured PostgreSQL config URL: %v", err)
					}
					wantConfigQuery := url.Values{
						"passfile":    {""},
						"servicefile": {""},
						"sslcert":     {""},
						"sslkey":      {""},
						"sslmode":     {sslMode},
						"sslpassword": {""},
						"sslrootcert": {""},
					}
					if got := parsedConfigURL.Query(); !reflect.DeepEqual(got, wantConfigQuery) {
						t.Fatalf("captured PostgreSQL config query = %#v, want explicit file-source contract %#v", got, wantConfigQuery)
					}
					if config.ConnConfig.TLSConfig == nil {
						t.Fatal("controlled constructor received nil verified-TLS config")
					}
					if got := len(config.ConnConfig.TLSConfig.Certificates); got != 0 {
						t.Fatalf("verified-TLS client certificates = %d, want zero", got)
					}
					if config.ConnConfig.TLSConfig.RootCAs == nil {
						t.Fatal("controlled constructor received implicit verified-TLS root CAs")
					}
					if !config.ConnConfig.TLSConfig.RootCAs.Equal(x509.NewCertPool()) {
						t.Fatal("controlled constructor received ambient or system TLS roots")
					}
					return nil, constructorErr
				},
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			pool, err := opener.open(ctx, canonical)
			if pool != nil {
				t.Fatalf("controlled constructor failure returned pool %p, want nil", pool)
			}
			if !errors.Is(err, constructorErr) {
				t.Fatalf("production opener error = %v, want controlled constructor failure", err)
			}
			if constructorCalls != 1 {
				t.Fatalf("controlled constructor calls = %d, want one", constructorCalls)
			}
		})
	}
}

func TestAppACLR2FinalizeProductionPostgresOpenerOwnsPoolUntilSuccessfulReturn(t *testing.T) {
	clearAppACLR2FinalizeAmbientPostgresVariables(t)
	canonical, err := canonicalAppACLR2FinalizeDatabaseURL(
		"postgres://direct_migrator:finalizer-secret@db.example:5444/houfeng?sslmode=disable",
	)
	if err != nil {
		t.Fatalf("canonicalAppACLR2FinalizeDatabaseURL() error = %v", err)
	}

	constructorErr := errors.New("constructor failed")
	pingErr := errors.New("ping failed")
	for _, tt := range []struct {
		name           string
		constructorErr error
		handle         *fakeAppACLR2FinalizePoolHandle
		wantErr        error
		wantClose      int
		wantPing       int
	}{
		{
			name:           "constructor error closes returned handle",
			constructorErr: constructorErr,
			handle:         &fakeAppACLR2FinalizePoolHandle{pool: &pgxpool.Pool{}},
			wantErr:        constructorErr,
			wantClose:      1,
		},
		{
			name:    "nil handle fails closed",
			wantErr: errors.New("nil pool handle"),
		},
		{
			name:      "missing underlying pool closes handle",
			handle:    &fakeAppACLR2FinalizePoolHandle{},
			wantErr:   errors.New("nil PostgreSQL pool"),
			wantClose: 1,
		},
		{
			name:      "ping failure closes handle",
			handle:    &fakeAppACLR2FinalizePoolHandle{pool: &pgxpool.Pool{}},
			wantErr:   pingErr,
			wantClose: 1,
			wantPing:  1,
		},
		{
			name:     "success transfers pool ownership to caller",
			handle:   &fakeAppACLR2FinalizePoolHandle{pool: &pgxpool.Pool{}},
			wantPing: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var wantPool *pgxpool.Pool
			if tt.wantErr == nil {
				wantPool = tt.handle.pool
			}
			pingCalls := 0
			if tt.handle != nil {
				tt.handle.ping = func(ctx context.Context) error {
					pingCalls++
					deadline, ok := ctx.Deadline()
					if !ok {
						t.Fatal("pool ping context has no deadline")
					}
					remaining := time.Until(deadline)
					if remaining <= 0 || remaining > 5*time.Second {
						t.Fatalf("pool ping deadline remaining = %s, want within (0s, 5s]", remaining)
					}
					if tt.wantErr == pingErr {
						return pingErr
					}
					return nil
				}
			}

			constructorCalls := 0
			opener := appACLR2FinalizePostgresOpener{
				newPool: func(context.Context, *pgxpool.Config) (appACLR2FinalizePoolHandle, error) {
					constructorCalls++
					if tt.handle == nil {
						return nil, tt.constructorErr
					}
					return tt.handle, tt.constructorErr
				},
			}
			pool, err := opener.open(context.Background(), canonical)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("production opener error = %v, want nil", err)
				}
			} else if tt.wantErr == constructorErr || tt.wantErr == pingErr {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("production opener error = %v, want wrapped %v", err, tt.wantErr)
				}
			} else if err == nil {
				t.Fatalf("production opener error = nil, want %v", tt.wantErr)
			}
			if pool != wantPool {
				t.Fatalf("production opener pool = %p, want %p", pool, wantPool)
			}
			if constructorCalls != 1 {
				t.Fatalf("controlled constructor calls = %d, want one", constructorCalls)
			}
			if got := pingCalls; got != tt.wantPing {
				t.Fatalf("pool Ping() calls = %d, want %d", got, tt.wantPing)
			}
			if tt.handle != nil && tt.handle.closeCalls != tt.wantClose {
				t.Fatalf("pool Close() calls = %d, want %d", tt.handle.closeCalls, tt.wantClose)
			}
		})
	}
}

func TestAppACLR2FinalizeRejectsNonExplicitPostgresURLsBeforePoolOpen(t *testing.T) {
	const secret = "finalizer-secret"
	clearAppACLR2FinalizeAmbientPostgresVariables(t)
	for _, tt := range []struct {
		name        string
		databaseURL string
	}{
		{name: "keyword value DSN", databaseURL: "host=db.example user=direct_migrator password=" + secret + " dbname=houfeng sslmode=disable"},
		{name: "missing password", databaseURL: "postgres://direct_migrator@db.example:5432/houfeng?sslmode=disable"},
		{name: "missing explicit port", databaseURL: "postgres://direct_migrator:" + secret + "@db.example/houfeng?sslmode=disable"},
		{name: "missing database", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432?sslmode=disable"},
		{name: "missing sslmode", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng"},
		{name: "service", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=disable&service=outside"},
		{name: "service file", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=disable&servicefile=/tmp/service"},
		{name: "empty service file", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=disable&servicefile="},
		{name: "pass file", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=disable&passfile=/tmp/pass"},
		{name: "empty pass file", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=disable&passfile="},
		{name: "TLS certificate file", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=verify-full&sslcert=/tmp/cert"},
		{name: "empty TLS certificate file", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=verify-full&sslcert="},
		{name: "TLS key file", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=verify-full&sslkey=/tmp/key"},
		{name: "empty TLS key file", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=verify-full&sslkey="},
		{name: "TLS root file", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=verify-full&sslrootcert=/tmp/root"},
		{name: "empty TLS root file", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=verify-full&sslrootcert="},
		{name: "connection timeout injection", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=disable&connect_timeout=5"},
		{name: "pool option injection", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=disable&pool_max_conns=2"},
		{name: "target session injection", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=disable&target_session_attrs=read-write"},
		{name: "runtime options injection", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=disable&options=-c%20search_path%3Dpublic"},
		{name: "timezone injection", databaseURL: "postgres://direct_migrator:" + secret + "@db.example:5432/houfeng?sslmode=disable&timezone=UTC"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opened := 0
			err := runAppACLR2FinalizeWithDeps(t.Context(), []string{"finalize", "--scope", "app-acl-r2"}, appACLR2FinalizeDependencies{
				lookupEnv: func(string) (string, bool) { return tt.databaseURL, true },
				openPostgres: func(context.Context, appACLR2FinalizeDatabaseURL) (*pgxpool.Pool, error) {
					opened++
					return &pgxpool.Pool{}, nil
				},
				closePool: func(*pgxpool.Pool) {},
				finalize: func(context.Context, *pgxpool.Pool) error {
					t.Fatal("non-explicit finalizer URL reached finalization")
					return nil
				},
			})
			if !errors.Is(err, errInvalidAppACLR2FinalizeConfiguration) {
				t.Fatalf("runAppACLR2FinalizeWithDeps() error = %v, want invalid explicit-URL configuration", err)
			}
			if opened != 0 {
				t.Fatalf("non-explicit finalizer URL opened %d pools, want zero", opened)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), tt.databaseURL) {
				t.Fatalf("configuration error leaked finalizer URL: %q", err)
			}
		})
	}
}

func TestAppACLR2FinalizeRejectsAmbientPostgresConfigurationBeforePoolOpen(t *testing.T) {
	const databaseURL = "postgres://direct_migrator:finalizer-secret@db.example:5432/houfeng?sslmode=disable"
	clearAppACLR2FinalizeAmbientPostgresVariables(t)
	for _, variable := range []string{
		"PGSERVICE", "PGSERVICEFILE", "PGPASSFILE", "PGSSLCERT", "PGSSLKEY", "PGSSLROOTCERT",
		"PGHOSTADDR", "PGGSSENCMODE", "PGOPTIONS", "PGTZ",
		"SSL_CERT_FILE", "SSL_CERT_DIR",
	} {
		t.Run(variable, func(t *testing.T) {
			t.Setenv(variable, "/tmp/ambient-finalizer-config")
			opened := 0
			err := runAppACLR2FinalizeWithDeps(t.Context(), []string{"finalize", "--scope", "app-acl-r2"}, appACLR2FinalizeDependencies{
				lookupEnv: func(string) (string, bool) { return databaseURL, true },
				openPostgres: func(context.Context, appACLR2FinalizeDatabaseURL) (*pgxpool.Pool, error) {
					opened++
					return &pgxpool.Pool{}, nil
				},
				closePool: func(*pgxpool.Pool) {},
				finalize:  func(context.Context, *pgxpool.Pool) error { return nil },
			})
			if !errors.Is(err, errInvalidAppACLR2FinalizeConfiguration) || opened != 0 {
				t.Fatalf("ambient %s result = error %v/opened %d, want redacted configuration rejection before open", variable, err, opened)
			}
		})
	}
}

func TestAppACLR2FinalizeCanonicalURLDisablesDefaultFileSources(t *testing.T) {
	clearAppACLR2FinalizeAmbientPostgresVariables(t)
	canonical, err := canonicalAppACLR2FinalizeDatabaseURL("postgres://direct_migrator:finalizer-secret@db.example:5432/houfeng?sslmode=verify-full")
	if err != nil {
		t.Fatalf("canonicalAppACLR2FinalizeDatabaseURL() error = %v", err)
	}
	parsed, err := url.Parse(string(canonical))
	if err != nil {
		t.Fatalf("parse canonical finalizer URL: %v", err)
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		t.Fatalf("parse canonical finalizer URL query: %v", err)
	}
	want := url.Values{
		"passfile":    {""},
		"servicefile": {""},
		"sslcert":     {""},
		"sslkey":      {""},
		"sslmode":     {"verify-full"},
		"sslpassword": {""},
		"sslrootcert": {""},
	}
	if !reflect.DeepEqual(query, want) {
		t.Fatalf("canonical finalizer URL query = %#v, want %#v", query, want)
	}
	for _, tt := range []struct {
		parameter string
		value     string
	}{
		{parameter: "sslrootcert", value: "system"},
		{parameter: "passfile", value: "/tmp/pass"},
		{parameter: "sslcert", value: "/tmp/cert"},
	} {
		regressed := make(url.Values, len(query))
		for parameter, values := range query {
			regressed[parameter] = append([]string(nil), values...)
		}
		regressed.Set(tt.parameter, tt.value)
		if reflect.DeepEqual(regressed, want) {
			t.Fatalf("canonical finalizer URL query assertion accepted %s=%q", tt.parameter, tt.value)
		}
	}
}

func TestAppACLR2FinalizeStrictPostgresConfigPinsEmptyRootPoolForVerifiedTLS(t *testing.T) {
	clearAppACLR2FinalizeAmbientPostgresVariables(t)
	for _, sslMode := range []string{"verify-ca", "verify-full"} {
		t.Run(sslMode, func(t *testing.T) {
			canonical, err := canonicalAppACLR2FinalizeDatabaseURL("postgres://direct_migrator:finalizer-secret@db.example:5432/houfeng?sslmode=" + sslMode)
			if err != nil {
				t.Fatalf("canonicalAppACLR2FinalizeDatabaseURL() error = %v", err)
			}

			config, err := parseAppACLR2FinalizePostgresConfig(canonical)
			if err != nil {
				t.Fatalf("parseAppACLR2FinalizePostgresConfig() error = %v", err)
			}
			if config.ConnConfig.TLSConfig == nil || config.ConnConfig.TLSConfig.RootCAs == nil {
				t.Fatal("strict finalizer PostgreSQL config left verified TLS root CAs implicit")
			}
			if !config.ConnConfig.TLSConfig.RootCAs.Equal(x509.NewCertPool()) {
				t.Fatal("strict finalizer PostgreSQL config permits ambient or system TLS roots")
			}
		})
	}
}

func TestAppACLR2FinalizeRouteNormalizesWhitespaceWrappedMigratorDSNOnce(t *testing.T) {
	const migratorDSN = "postgres://direct_migrator:finalize-secret@example.invalid:5432/houfeng?sslmode=disable"
	const wrappedMigratorDSN = " \tpostgres://direct_migrator:finalize-secret@example.invalid:5432/houfeng?sslmode=disable\n "
	clearAppACLR2FinalizeAmbientPostgresVariables(t)
	canonicalMigratorDSN, err := canonicalAppACLR2FinalizeDatabaseURL(migratorDSN)
	if err != nil {
		t.Fatalf("canonicalAppACLR2FinalizeDatabaseURL() error = %v", err)
	}
	pool := &pgxpool.Pool{}
	lookups := 0
	opened := 0
	closed := 0
	finalized := 0

	err = runAppACLR2FinalizeWithDeps(t.Context(), []string{"finalize", "--scope", "app-acl-r2"}, appACLR2FinalizeDependencies{
		lookupEnv: func(key string) (string, bool) {
			lookups++
			if key != platformmigrate.AppMigratorDatabaseURLEnv {
				t.Fatalf("finalize route looked up forbidden environment key %q", key)
			}
			return wrappedMigratorDSN, true
		},
		openPostgres: func(_ context.Context, gotDSN appACLR2FinalizeDatabaseURL) (*pgxpool.Pool, error) {
			opened++
			if gotDSN != canonicalMigratorDSN {
				t.Fatalf("openPostgres() DSN = %q, want canonical migrator DSN %q", gotDSN, canonicalMigratorDSN)
			}
			return pool, nil
		},
		closePool: func(gotPool *pgxpool.Pool) {
			if gotPool != pool {
				t.Fatalf("closePool() pool = %p, want opened pool %p", gotPool, pool)
			}
			closed++
		},
		finalize: func(_ context.Context, gotPool *pgxpool.Pool) error {
			if gotPool != pool {
				t.Fatalf("finalize() pool = %p, want opened pool %p", gotPool, pool)
			}
			finalized++
			return errors.New("finalize failed for " + migratorDSN)
		},
	})
	if !errors.Is(err, errFinalizeAppACLR2) {
		t.Fatalf("runAppACLR2FinalizeWithDeps() error = %v, want finalizer error", err)
	}
	if strings.Contains(err.Error(), migratorDSN) || strings.Contains(err.Error(), wrappedMigratorDSN) {
		t.Fatalf("runAppACLR2FinalizeWithDeps() error leaked migrator DSN: %q", err)
	}
	if lookups != 1 || opened != 1 || finalized != 1 || closed != 1 {
		t.Fatalf("lookups=%d opened=%d finalized=%d closed=%d, want one of each", lookups, opened, finalized, closed)
	}
}

func TestAppACLR2FinalizeRouteUsesCanonicalMigratorDatabaseURLEnv(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if strings.Contains(string(source), "appACLR2FinalizeDatabaseURLEnv") {
		t.Fatal("main.go duplicates the canonical APP migrator database URL environment constant")
	}
	if !strings.Contains(string(source), "platformmigrate.AppMigratorDatabaseURLEnv") {
		t.Fatal("finalizer route does not use the canonical APP migrator database URL environment constant")
	}
}

func TestAppACLR2FinalizeRouteRejectsForbiddenArgumentsBeforeLookupOrPoolOpen(t *testing.T) {
	for _, args := range [][]string{
		{"finalize"},
		{"finalize", "--scope", "app"},
		{"finalize", "--scope", "app-acl-r2", "positional-secret"},
		{"finalize", "--scope", "app-acl-r2", "--role", "direct_migrator"},
		{"finalize", "--scope", "app-acl-r2", "--migrator-database-url", "postgres://migrator:secret@example.invalid/houfeng"},
		{"finalize", "--scope", "app-acl-r2", "--migrator-database-url-file", "/tmp/secret"},
		{"finalize", "--scope", "app-acl-r2", "--unknown"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			lookupCalls := 0
			err := runAdminWithDeps(t.Context(), args, appAdminDependencies{
				finalize: appACLR2FinalizeDependencies{
					lookupEnv: func(string) (string, bool) {
						lookupCalls++
						return "", false
					},
					openPostgres: func(context.Context, appACLR2FinalizeDatabaseURL) (*pgxpool.Pool, error) {
						t.Fatal("invalid finalizer invocation opened a PostgreSQL pool")
						return nil, nil
					},
					closePool: func(*pgxpool.Pool) {
						t.Fatal("invalid finalizer invocation closed an unopened PostgreSQL pool")
					},
					finalize: func(context.Context, *pgxpool.Pool) error {
						t.Fatal("invalid finalizer invocation attempted finalization")
						return nil
					},
				},
			})
			if !errors.Is(err, errInvalidAppACLR2FinalizeInvocation) {
				t.Fatalf("runAdminWithDeps(%q) error = %v, want invalid finalizer invocation", args, err)
			}
			if lookupCalls != 0 {
				t.Fatalf("runAdminWithDeps(%q) performed %d environment lookups before rejection", args, lookupCalls)
			}
		})
	}
}

func TestAppACLR2FinalizeRouteClosesPoolAndRedactsOperationalErrors(t *testing.T) {
	const migratorDSN = "postgres://direct_migrator:finalize-secret@example.invalid:5432/houfeng?sslmode=disable"
	clearAppACLR2FinalizeAmbientPostgresVariables(t)
	for _, tt := range []struct {
		name         string
		lookupValue  string
		lookupOK     bool
		openErr      error
		finalizeErr  error
		wantErr      error
		wantFinalize int
	}{
		{
			name:    "missing_migrator_dsn",
			wantErr: errInvalidAppACLR2FinalizeConfiguration,
		},
		{
			name:        "pool_open_failure",
			lookupValue: migratorDSN,
			lookupOK:    true,
			openErr:     errors.New("open failed for " + migratorDSN),
			wantErr:     errOpenAppACLR2FinalizePool,
		},
		{
			name:         "finalize_failure",
			lookupValue:  migratorDSN,
			lookupOK:     true,
			finalizeErr:  errors.New("finalize failed for " + migratorDSN),
			wantErr:      errFinalizeAppACLR2,
			wantFinalize: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pool := &pgxpool.Pool{}
			closed := 0
			finalized := 0
			err := runAppACLR2FinalizeWithDeps(t.Context(), []string{"finalize", "--scope", "app-acl-r2"}, appACLR2FinalizeDependencies{
				lookupEnv: func(key string) (string, bool) {
					if key != platformmigrate.AppMigratorDatabaseURLEnv {
						t.Fatalf("finalize route looked up forbidden environment key %q", key)
					}
					return tt.lookupValue, tt.lookupOK
				},
				openPostgres: func(context.Context, appACLR2FinalizeDatabaseURL) (*pgxpool.Pool, error) {
					return pool, tt.openErr
				},
				closePool: func(gotPool *pgxpool.Pool) {
					if gotPool != pool {
						t.Fatalf("closePool() pool = %p, want opened pool %p", gotPool, pool)
					}
					closed++
				},
				finalize: func(context.Context, *pgxpool.Pool) error {
					finalized++
					return tt.finalizeErr
				},
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("runAppACLR2FinalizeWithDeps() error = %v, want %v", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), migratorDSN) {
				t.Fatalf("runAppACLR2FinalizeWithDeps() error leaked migrator DSN: %q", err)
			}
			if tt.lookupOK && closed != 1 {
				t.Fatalf("closePool() calls = %d, want one", closed)
			}
			if !tt.lookupOK && closed != 0 {
				t.Fatalf("closePool() calls = %d, want zero before configuration rejection", closed)
			}
			if finalized != tt.wantFinalize {
				t.Fatalf("finalize() calls = %d, want %d", finalized, tt.wantFinalize)
			}
		})
	}
}

func TestAppACLR2FinalizeFailureLogNamesAdminCommand(t *testing.T) {
	const migratorDSN = "postgres://direct_migrator:finalize-secret@%zz/houfeng"
	command := exec.Command("go", "run", ".", "finalize", "--scope", "app-acl-r2")
	command.Env = append(environmentWithoutHoufengVariables(), "HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL="+migratorDSN)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("finalize --scope app-acl-r2 unexpectedly accepted a malformed migrator DSN")
	}
	if !strings.Contains(string(output), "record platform admin command failed") {
		t.Fatalf("finalize failure output = %q, want generic admin-command log message", output)
	}
	if strings.Contains(string(output), "record platform app migration failed") {
		t.Fatalf("finalize failure output mislabeled the command as an APP migration: %q", output)
	}
	if strings.Contains(string(output), migratorDSN) {
		t.Fatalf("finalize failure output leaked migrator DSN: %q", output)
	}
}

func environmentWithoutHoufengVariables() []string {
	entries := os.Environ()
	filtered := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry, "HOUFENG_") || strings.HasPrefix(entry, "PG") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func clearAppACLR2FinalizeAmbientPostgresVariables(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && (strings.HasPrefix(name, "PG") || name == "SSL_CERT_FILE" || name == "SSL_CERT_DIR") {
			t.Setenv(name, "")
		}
	}
}

func appScopeLookup(t *testing.T, databaseURL, runtimeRole, adminRole string) func(string) (string, bool) {
	t.Helper()
	values := map[string]string{
		platformmigrate.AppMigratorDatabaseURLEnv: databaseURL,
		platformmigrate.AppRuntimeRoleEnv:         runtimeRole,
		platformmigrate.AppAdminRoleEnv:           adminRole,
	}
	return func(key string) (string, bool) {
		value, ok := values[key]
		if !ok {
			t.Fatalf("unexpected environment lookup for %q", key)
		}
		return value, true
	}
}
