package deploy_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDockerRuntimeRunsAsHoufengUser(t *testing.T) {
	root := repoRoot(t)
	dockerfile := readText(t, filepath.Join(root, "Dockerfile"))

	if !strings.Contains(dockerfile, "\nUSER houfeng:houfeng\n") {
		t.Fatal("Dockerfile runtime stage must set USER houfeng:houfeng")
	}
	if strings.Contains(dockerfile, "gosu") {
		t.Fatal("Dockerfile runtime stage must not depend on gosu privilege dropping")
	}
}

func TestDockerEntrypointDoesNotDropPrivilegesAtRuntime(t *testing.T) {
	root := repoRoot(t)
	entrypoint := readText(t, filepath.Join(root, "scripts", "docker-entrypoint.sh"))

	for _, forbidden := range []string{"gosu", "id -u", "install -d -o"} {
		if strings.Contains(entrypoint, forbidden) {
			t.Fatalf("docker entrypoint must not contain %q", forbidden)
		}
	}
}

func TestDockerEntrypointAcceptsSecretFileInputs(t *testing.T) {
	root := repoRoot(t)
	entrypoint := readText(t, filepath.Join(root, "scripts", "docker-entrypoint.sh"))

	for _, required := range []string{
		"${HOUFENG_INITIAL_PASSWORD_FILE:-}",
		"${HOUFENG_SESSION_HMAC_KEY_FILE:-}",
		"HOUFENG_INITIAL_PASSWORD or HOUFENG_INITIAL_PASSWORD_FILE",
		"HOUFENG_SESSION_HMAC_KEY or HOUFENG_SESSION_HMAC_KEY_FILE",
	} {
		if !strings.Contains(entrypoint, required) {
			t.Fatalf("docker entrypoint must contain %q", required)
		}
	}
}

func TestDockerEntrypointExecutesActualScriptWithValidatedDatabaseInputs(t *testing.T) {
	tests := []struct {
		name               string
		environment        map[string]string
		passwordFile       string
		passwordFileMode   os.FileMode
		wantChild          bool
		wantDatabaseURL    string
		wantDatabaseName   string
		wantOutputContains string
	}{
		{
			name: "missing database user",
			environment: map[string]string{
				"HOUFENG_DATABASE_NAME":     "houfeng",
				"HOUFENG_DATABASE_PASSWORD": "secret",
			},
			wantOutputContains: "HOUFENG_DATABASE_USER is required",
		},
		{
			name: "missing database name",
			environment: map[string]string{
				"HOUFENG_DATABASE_USER":     "houfeng",
				"HOUFENG_DATABASE_PASSWORD": "secret",
			},
			wantOutputContains: "HOUFENG_DATABASE_NAME is required",
		},
		{
			name: "missing database password",
			environment: map[string]string{
				"HOUFENG_DATABASE_USER": "houfeng",
				"HOUFENG_DATABASE_NAME": "houfeng",
			},
			wantOutputContains: "HOUFENG_DATABASE_PASSWORD or HOUFENG_DATABASE_PASSWORD_FILE is required",
		},
		{
			name: "password file does not exist",
			environment: map[string]string{
				"HOUFENG_DATABASE_USER": "houfeng",
				"HOUFENG_DATABASE_NAME": "houfeng",
			},
			passwordFile:       "missing",
			wantOutputContains: "HOUFENG_DATABASE_PASSWORD_FILE is not readable",
		},
		{
			name: "password file is unreadable",
			environment: map[string]string{
				"HOUFENG_DATABASE_USER": "houfeng",
				"HOUFENG_DATABASE_NAME": "houfeng",
			},
			passwordFile:       "file-secret",
			passwordFileMode:   0,
			wantOutputContains: "HOUFENG_DATABASE_PASSWORD_FILE is not readable",
		},
		{
			name: "password file is empty",
			environment: map[string]string{
				"HOUFENG_DATABASE_USER": "houfeng",
				"HOUFENG_DATABASE_NAME": "houfeng",
			},
			passwordFile:       "",
			passwordFileMode:   0o600,
			wantOutputContains: "HOUFENG_DATABASE_PASSWORD or HOUFENG_DATABASE_PASSWORD_FILE is required",
		},
		{
			name: "password file takes precedence over environment",
			environment: map[string]string{
				"HOUFENG_DATABASE_USER":     "houfeng",
				"HOUFENG_DATABASE_NAME":     "houfeng",
				"HOUFENG_DATABASE_PASSWORD": "environment-secret",
			},
			passwordFile:     "file:/? secret",
			passwordFileMode: 0o600,
			wantChild:        true,
			wantDatabaseURL:  "postgres://houfeng:file%3A%2F%3F%20secret@db:5432/?dbname=houfeng&sslmode=disable",
			wantDatabaseName: "houfeng",
		},
		{
			name: "explicit database URL bypasses fallback inputs",
			environment: map[string]string{
				"HOUFENG_DATABASE_URL": "postgres://explicit.invalid/database?sslmode=require",
			},
			wantChild:        true,
			wantDatabaseURL:  "postgres://explicit.invalid/database?sslmode=require",
			wantDatabaseName: "database",
		},
		{
			name: "reserved URI characters are percent encoded",
			environment: map[string]string{
				"HOUFENG_DATABASE_USER":     "app:user",
				"HOUFENG_DATABASE_NAME":     "db/name?blue",
				"HOUFENG_DATABASE_PASSWORD": "p@ss:/?#[]%&=+ space",
			},
			wantChild:        true,
			wantDatabaseURL:  "postgres://app%3Auser:p%40ss%3A%2F%3F%23%5B%5D%25%26%3D%2B%20space@db:5432/?dbname=db%2Fname%3Fblue&sslmode=disable",
			wantDatabaseName: "db/name?blue",
		},
		{
			name: "leading slash in database name survives pgx parsing",
			environment: map[string]string{
				"HOUFENG_DATABASE_USER":     "houfeng",
				"HOUFENG_DATABASE_NAME":     "/tenant",
				"HOUFENG_DATABASE_PASSWORD": "secret",
			},
			wantChild:        true,
			wantDatabaseURL:  "postgres://houfeng:secret@db:5432/?dbname=%2Ftenant&sslmode=disable",
			wantDatabaseName: "/tenant",
		},
		{
			name: "UTF-8 database name survives pgx parsing",
			environment: map[string]string{
				"HOUFENG_DATABASE_USER":     "houfeng",
				"HOUFENG_DATABASE_NAME":     "数据库/生产",
				"HOUFENG_DATABASE_PASSWORD": "secret",
			},
			wantChild:        true,
			wantDatabaseURL:  "postgres://houfeng:secret@db:5432/?dbname=%E6%95%B0%E6%8D%AE%E5%BA%93%2F%E7%94%9F%E4%BA%A7&sslmode=disable",
			wantDatabaseName: "数据库/生产",
		},
		{
			name: "ASCII control byte is rejected",
			environment: map[string]string{
				"HOUFENG_DATABASE_USER":     "houfeng",
				"HOUFENG_DATABASE_NAME":     "bad\nname",
				"HOUFENG_DATABASE_PASSWORD": "secret",
			},
			wantOutputContains: "database connection component contains an ASCII control byte",
		},
		{
			name: "NUL byte in password file is rejected",
			environment: map[string]string{
				"HOUFENG_DATABASE_USER": "houfeng",
				"HOUFENG_DATABASE_NAME": "houfeng",
			},
			passwordFile:       "before\x00after",
			passwordFileMode:   0o600,
			wantOutputContains: "database connection component contains an ASCII control byte",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runDockerEntrypoint(t, tt.environment, tt.passwordFile, tt.passwordFileMode)
			if tt.wantChild {
				if result.err != nil {
					t.Fatalf("docker entrypoint failed: %v\n%s", result.err, result.output)
				}
				if !result.childRan {
					t.Fatal("docker entrypoint did not execute its child")
				}
				config, err := pgxpool.ParseConfig(result.databaseURL)
				if err != nil {
					t.Fatalf("pgxpool.ParseConfig(%q) error = %v", result.databaseURL, err)
				}
				if config.ConnConfig.Database != tt.wantDatabaseName {
					t.Fatalf("parsed database name = %q, want %q", config.ConnConfig.Database, tt.wantDatabaseName)
				}
				if result.databaseURL != tt.wantDatabaseURL {
					t.Fatalf("child HOUFENG_DATABASE_URL = %q, want %q", result.databaseURL, tt.wantDatabaseURL)
				}
				return
			}

			if result.err == nil {
				t.Fatalf("docker entrypoint unexpectedly succeeded; output:\n%s", result.output)
			}
			if result.childRan {
				t.Fatal("docker entrypoint executed its child after validation failure")
			}
			if !strings.Contains(result.output, tt.wantOutputContains) {
				t.Fatalf("docker entrypoint output = %q, want substring %q", result.output, tt.wantOutputContains)
			}
		})
	}
}

func TestComposeApplicationRoleRejectsExistingMembershipBeforeMutation(t *testing.T) {
	root := repoRoot(t)
	applicationRoleSQL := readText(t, filepath.Join(root, "docs", "deploy", "compose-application-role.sql"))

	membershipGuard := strings.Index(applicationRoleSQL, "pg_catalog.pg_auth_members")
	if membershipGuard < 0 {
		t.Fatal("Compose application-role provisioning must inspect pg_auth_members before mutating an existing role")
	}
	for _, required := range []string{
		`\set ON_ERROR_STOP on`,
		"RAISE EXCEPTION 'Houfeng application role must not have direct or recursive role membership'",
	} {
		if !strings.Contains(applicationRoleSQL, required) {
			t.Fatalf("Compose application-role membership drift must make psql fail through %q", required)
		}
	}
	if strings.Contains(applicationRoleSQL, `\quit`) {
		t.Fatal("Compose application-role membership drift must not rely on psql \\quit accepting an exit-status argument")
	}
	for _, mutation := range []string{"'ALTER ROLE %I", "'ALTER DATABASE %I OWNER TO %I"} {
		mutationOffset := strings.Index(applicationRoleSQL, mutation)
		if mutationOffset < 0 {
			t.Fatalf("Compose application-role provisioning must contain %q", mutation)
		}
		if membershipGuard > mutationOffset {
			t.Fatalf("Compose application-role membership guard must precede %q", mutation)
		}
	}
}

func TestAppACLR2PreR1ProvisioningRevokesPGControlSystemFromPublic(t *testing.T) {
	root := repoRoot(t)
	provisioning := readText(t, filepath.Join(root, "docs", "deploy", "app-acl-r2-pre-r1-provisioning.sql"))
	guide := readText(t, filepath.Join(root, "docs", "deploy", "local-and-systemd.md"))
	runtimeReader := readText(t, filepath.Join(root, "internal", "center", "store", "migrate", "app_acl_r2_receipt_postgres.go"))

	for _, required := range []string{
		"REVOKE EXECUTE ON FUNCTION pg_catalog.pg_control_system() FROM PUBLIC;",
		"procedure.pronargs = 0",
		"OUT pg_control_version integer, OUT catalog_version_no integer, OUT system_identifier bigint, OUT pg_control_last_modified timestamp with time zone",
		"procedure.proowner = 10",
	} {
		if !strings.Contains(provisioning, required) {
			t.Fatalf("APP ACL R2 pre-R1 provisioning SQL must contain %q", required)
		}
	}
	if strings.Contains(provisioning, "pg_catalog.pg_get_function_identity_arguments(procedure.oid) = ''") {
		t.Fatal("APP ACL R2 pre-R1 provisioning must select pg_control_system() by zero input arguments, not empty formatted arguments")
	}
	for _, forbidden := range []string{"0051_create_record_platform_foundation", "0052_app_acl_r2_privileged_transition"} {
		if strings.Contains(provisioning, forbidden) {
			t.Fatalf("APP ACL R2 pre-R1 provisioning SQL must not depend on migration %q", forbidden)
		}
	}
	for _, required := range []string{
		"scripts/compose-up.sh docs/deploy/compose.env",
		"docs/deploy/app-acl-r2-pre-r1-provisioning.sql",
	} {
		if !strings.Contains(guide, required) {
			t.Fatalf("deployment guide must contain pre-R1 provisioning step %q", required)
		}
	}
	if strings.Contains(guide, "psql -X -v ON_ERROR_STOP=1 -U houfeng -d houfeng") {
		t.Fatal("deployment guide must not provision with the Houfeng application principal")
	}
	if strings.Contains(strings.ToUpper(runtimeReader), "REVOKE EXECUTE ON FUNCTION PG_CATALOG.PG_CONTROL_SYSTEM") {
		t.Fatal("APP ACL R2 runtime catalog reader must not repair pg_control_system() privileges")
	}
}

func TestComposeSeparatesBootstrapAndApplicationDatabasePrincipals(t *testing.T) {
	root := repoRoot(t)
	compose := readText(t, filepath.Join(root, "compose.yaml"))
	envExample := readText(t, filepath.Join(root, "docs", "deploy", "compose.env.example"))
	entrypoint := readText(t, filepath.Join(root, "scripts", "docker-entrypoint.sh"))
	applicationRoleSQL := readText(t, filepath.Join(root, "docs", "deploy", "compose-application-role.sql"))

	bootstrapUser := dotenvValue(t, envExample, "POSTGRES_BOOTSTRAP_USER")
	applicationUser := dotenvValue(t, envExample, "HOUFENG_DATABASE_USER")
	if bootstrapUser == applicationUser {
		t.Fatalf("Compose bootstrap principal %q must differ from application principal", bootstrapUser)
	}

	for _, required := range []string{
		"image: postgres:16.12",
		`POSTGRES_USER: "${POSTGRES_BOOTSTRAP_USER`,
		`HOUFENG_DATABASE_USER: "${HOUFENG_DATABASE_USER`,
		"POSTGRES_PASSWORD_FILE: /run/secrets/postgres_bootstrap_password",
		"HOUFENG_DATABASE_PASSWORD_FILE: /run/secrets/houfeng_database_password",
	} {
		if !strings.Contains(compose, required) {
			t.Fatalf("compose.yaml must wire distinct database identities through %q", required)
		}
	}
	if strings.Contains(compose, "POSTGRES_USER: houfeng") {
		t.Fatal("compose.yaml must not initialize the application principal as the PostgreSQL bootstrap superuser")
	}
	if strings.Contains(compose, "image: postgres:16-alpine") {
		t.Fatal("compose.yaml must pin PostgreSQL to an APP ACL R2 allowlisted image")
	}
	dbServiceOffset := strings.Index(compose, "\n  db:\n")
	if dbServiceOffset < 0 {
		t.Fatal("compose.yaml must define the db service after the Houfeng service")
	}
	houfengService := compose[:dbServiceOffset]
	if !strings.Contains(houfengService, "\n      - houfeng_database_password\n") {
		t.Fatal("Houfeng service must mount the application database password secret")
	}
	if strings.Contains(houfengService, "postgres_bootstrap_password") {
		t.Fatal("Houfeng service must not receive the PostgreSQL bootstrap password secret")
	}
	for _, required := range []string{
		"${HOUFENG_DATABASE_USER:-}",
		"${HOUFENG_DATABASE_NAME:-}",
		"${HOUFENG_DATABASE_PASSWORD_FILE:-}",
	} {
		if !strings.Contains(entrypoint, required) {
			t.Fatalf("docker entrypoint must assemble the application DSN from %q", required)
		}
	}
	if strings.Contains(entrypoint, "$POSTGRES_PASSWORD") {
		t.Fatal("Houfeng entrypoint must not reuse the PostgreSQL bootstrap password")
	}
	for _, required := range []string{
		"CREATE ROLE %I LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD %L",
		"ALTER ROLE %I WITH LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD %L",
		"ALTER DATABASE %I OWNER TO %I",
	} {
		if !strings.Contains(applicationRoleSQL, required) {
			t.Fatalf("Compose application-role provisioning must contain %q", required)
		}
	}
}

func TestComposeQuickStartWaitsForReadinessBeforeProvisioning(t *testing.T) {
	result := runComposeBootstrap(t, "")
	if result.err != nil {
		t.Fatalf("Compose quick-start failed: %v\n%s", result.err, result.output)
	}
	const wantEvents = "up-db\nready\nready-query\nidentity\nprovision\napp-role\nup-houfeng\n"
	if result.events != wantEvents {
		t.Fatalf("Compose quick-start events = %q, want readiness and provisioning before application startup %q", result.events, wantEvents)
	}
}

func TestComposeQuickStartStopsBeforeHoufengOnReadinessOrProvisioningFailure(t *testing.T) {
	tests := []struct {
		failure    string
		wantEvents string
	}{
		{failure: "readiness", wantEvents: "up-db\nready\n"},
		{failure: "readiness-query", wantEvents: "up-db\nready\nready-query\n"},
		{failure: "identity", wantEvents: "up-db\nready\nready-query\nidentity\n"},
		{failure: "provision", wantEvents: "up-db\nready\nready-query\nidentity\nprovision\n"},
		{failure: "app-role", wantEvents: "up-db\nready\nready-query\nidentity\nprovision\napp-role\n"},
	}
	for _, tt := range tests {
		t.Run(tt.failure, func(t *testing.T) {
			result := runComposeBootstrap(t, tt.failure)
			if result.err == nil {
				t.Fatalf("Compose quick-start unexpectedly succeeded after %s failure", tt.failure)
			}
			if result.events != tt.wantEvents {
				t.Fatalf("Compose quick-start events after %s failure = %q, want %q", tt.failure, result.events, tt.wantEvents)
			}
			if strings.Contains(result.events, "up-houfeng\n") {
				t.Fatalf("Compose quick-start started Houfeng after %s failure; events: %q", tt.failure, result.events)
			}
		})
	}
}

func TestComposeUsesNamedLogVolumeForNonRootContainer(t *testing.T) {
	root := repoRoot(t)
	compose := readText(t, filepath.Join(root, "compose.yaml"))

	if strings.Contains(compose, "./data/logs:/var/log/houfeng") {
		t.Fatal("compose.yaml must not bind-mount ./data/logs over the non-root image log directory")
	}
	if !strings.Contains(compose, "houfeng_logs:/var/log/houfeng") {
		t.Fatal("compose.yaml must mount the named houfeng_logs volume at /var/log/houfeng")
	}
	if !strings.Contains(compose, "\nvolumes:\n  houfeng_logs:\n") {
		t.Fatal("compose.yaml must declare the houfeng_logs named volume")
	}
}

func TestCIDockerImageJobBuildsImageInGitHubActions(t *testing.T) {
	root := repoRoot(t)
	workflow := readText(t, filepath.Join(root, ".github", "workflows", "ci.yml"))

	for _, required := range []string{
		"\n  docker-image:\n",
		"docker/setup-buildx-action@v4",
		"docker/build-push-action@v7",
		"context: .",
		"file: Dockerfile",
		"push: false",
		"VERSION=dev",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("ci docker-image job must contain %q", required)
		}
	}
}

func TestPublishImagesWorkflowBuildsAndInspectsDockerImage(t *testing.T) {
	root := repoRoot(t)
	workflow := readText(t, filepath.Join(root, ".github", "workflows", "publish-images.yml"))

	for _, required := range []string{
		"\n  build:\n",
		"\n  publish:\n",
		"docker/build-push-action@v7",
		"file: Dockerfile",
		"outputs: type=image,name=${{ env.REGISTRY_IMAGE }},push-by-digest=true,name-canonical=true,push=true",
		"docker buildx imagetools create",
		"docker buildx imagetools inspect",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("publish-images workflow must contain %q", required)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func readText(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func dotenvValue(t *testing.T, body, key string) string {
	t.Helper()
	prefix := key + "="
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, prefix) {
			value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if value == "" {
				t.Fatalf("%s must not be empty in compose.env.example", key)
			}
			return value
		}
	}
	t.Fatalf("compose.env.example must define %s", key)
	return ""
}

type composeBootstrapResult struct {
	events string
	output string
	err    error
}

type dockerEntrypointResult struct {
	childRan    bool
	databaseURL string
	output      string
	err         error
}

func runDockerEntrypoint(t *testing.T, values map[string]string, passwordFile string, passwordFileMode os.FileMode) dockerEntrypointResult {
	t.Helper()
	root := repoRoot(t)
	tempDir := t.TempDir()
	childMarker := filepath.Join(tempDir, "child-ran")
	databaseURLPath := filepath.Join(tempDir, "database-url")
	fakeChild := filepath.Join(tempDir, "child")
	if err := os.WriteFile(fakeChild, []byte(`#!/bin/sh
set -eu
printf '%s' child-ran >"$HOUFENG_ENTRYPOINT_CHILD_MARKER"
printf '%s' "$HOUFENG_DATABASE_URL" >"$HOUFENG_ENTRYPOINT_CHILD_DATABASE_URL"
`), 0o755); err != nil {
		t.Fatalf("write fake entrypoint child: %v", err)
	}

	environment := map[string]string{
		"PATH":                                  os.Getenv("PATH"),
		"HOUFENG_INITIAL_PASSWORD":              "admin-secret",
		"HOUFENG_SESSION_HMAC_KEY":              "0123456789abcdef0123456789abcdef",
		"HOUFENG_ENTRYPOINT_CHILD_MARKER":       childMarker,
		"HOUFENG_ENTRYPOINT_CHILD_DATABASE_URL": databaseURLPath,
	}
	for key, value := range values {
		environment[key] = value
	}
	if passwordFile != "" || passwordFileMode != 0 {
		passwordPath := filepath.Join(tempDir, "database-password")
		if passwordFile == "missing" {
			passwordPath = filepath.Join(tempDir, "missing-database-password")
		} else if err := os.WriteFile(passwordPath, []byte(passwordFile), 0o600); err != nil {
			t.Fatalf("write database password file: %v", err)
		} else if err := os.Chmod(passwordPath, passwordFileMode); err != nil {
			t.Fatalf("chmod database password file: %v", err)
		}
		environment["HOUFENG_DATABASE_PASSWORD_FILE"] = passwordPath
	}

	command := exec.Command("/bin/sh", filepath.Join(root, "scripts", "docker-entrypoint.sh"), fakeChild)
	command.Dir = root
	command.Env = make([]string, 0, len(environment))
	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	marker, markerErr := os.ReadFile(childMarker)
	if markerErr != nil && !os.IsNotExist(markerErr) {
		t.Fatalf("read entrypoint child marker: %v", markerErr)
	}
	databaseURL, databaseURLErr := os.ReadFile(databaseURLPath)
	if databaseURLErr != nil && !os.IsNotExist(databaseURLErr) {
		t.Fatalf("read child database URL: %v", databaseURLErr)
	}
	return dockerEntrypointResult{
		childRan:    string(marker) == "child-ran",
		databaseURL: string(databaseURL),
		output:      string(output),
		err:         err,
	}
}

func runComposeBootstrap(t *testing.T, failure string) composeBootstrapResult {
	t.Helper()
	root := repoRoot(t)
	tempDir := t.TempDir()
	eventsPath := filepath.Join(tempDir, "events")
	fakeDocker := filepath.Join(tempDir, "docker")
	const fakeDockerBody = `#!/bin/sh
set -eu

args=$*
case "$args" in
  *"up -d db"*)
    printf '%s\n' up-db >>"$FAKE_DOCKER_EVENTS"
    ;;
  *pg_isready*)
    printf '%s\n' ready >>"$FAKE_DOCKER_EVENTS"
    if [ "${FAKE_DOCKER_FAILURE:-}" = readiness ]; then
      exit 1
    fi
    ;;
  *psql*"select 1"*)
    printf '%s\n' ready-query >>"$FAKE_DOCKER_EVENTS"
    if [ "${FAKE_DOCKER_FAILURE:-}" = readiness-query ]; then
      exit 1
    fi
    ;;
  *"principals must differ"*)
    printf '%s\n' identity >>"$FAKE_DOCKER_EVENTS"
    if [ "${FAKE_DOCKER_FAILURE:-}" = identity ]; then
      exit 1
    fi
    ;;
  *psql*)
    input=$(cat)
    case "$input" in
      *"REVOKE EXECUTE ON FUNCTION pg_catalog.pg_control_system() FROM PUBLIC;"*)
        printf '%s\n' provision >>"$FAKE_DOCKER_EVENTS"
        if [ "${FAKE_DOCKER_FAILURE:-}" = provision ]; then
          exit 1
        fi
        ;;
      *"CREATE ROLE"*)
        printf '%s\n' app-role >>"$FAKE_DOCKER_EVENTS"
        if [ "${FAKE_DOCKER_FAILURE:-}" = app-role ]; then
          exit 1
        fi
        ;;
      *)
        printf '%s\n' unknown-psql >>"$FAKE_DOCKER_EVENTS"
        exit 1
        ;;
    esac
    ;;
  *"up -d houfeng"*)
    printf '%s\n' up-houfeng >>"$FAKE_DOCKER_EVENTS"
    ;;
esac
`
	if err := os.WriteFile(fakeDocker, []byte(fakeDockerBody), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}

	command := exec.Command(
		"/bin/sh",
		filepath.Join(root, "scripts", "compose-up.sh"),
		filepath.Join(root, "docs", "deploy", "compose.env.example"),
	)
	command.Dir = root
	command.Env = append(os.Environ(),
		"PATH="+tempDir+":"+os.Getenv("PATH"),
		"FAKE_DOCKER_EVENTS="+eventsPath,
		"FAKE_DOCKER_FAILURE="+failure,
		"HOUFENG_COMPOSE_DB_READY_MAX_ATTEMPTS=1",
		"HOUFENG_COMPOSE_DB_READY_INTERVAL_SECONDS=0",
	)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	events, readErr := os.ReadFile(eventsPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read fake docker events: %v", readErr)
	}
	return composeBootstrapResult{events: string(events), output: output.String(), err: err}
}
