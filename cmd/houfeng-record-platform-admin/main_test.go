package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
)

func TestLegacyR2TokenMatcherDoesNotTreatVersionedR2CodecsAsLegacy(t *testing.T) {
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

func TestLegacyR2TokenSearchDistinguishesMatchesNoMatchesAndErrors(t *testing.T) {
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
	args := append([]string{"-n", "-e", legacyR2TokenPattern(token).String()}, searchPaths...)
	output, err := exec.Command("rg", args...).CombinedOutput()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("rg search for obsolete APP root token %q failed: %w: %s", token, err, output)
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
	if strings.Contains(err.Error(), migratorDSN) {
		t.Fatalf("runWithDeps() error leaked APP migrator DSN: %q", err)
	}
	if closed != 1 {
		t.Fatalf("closePool() calls = %d, want one after convergence failure", closed)
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

func environmentWithoutHoufengVariables() []string {
	entries := os.Environ()
	filtered := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry, "HOUFENG_") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
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
