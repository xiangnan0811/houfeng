package deploy_test

import (
	"maps"
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

func TestDockerEntrypointDoesNotRequireCenterSecretsForProcessor(t *testing.T) {
	result := runDockerEntrypoint(t, map[string]string{
		"HOUFENG_DATABASE_USER":     "houfeng",
		"HOUFENG_DATABASE_NAME":     "houfeng",
		"HOUFENG_DATABASE_PASSWORD": "processor-database-password",
		"HOUFENG_INITIAL_PASSWORD":  "",
		"HOUFENG_SESSION_HMAC_KEY":  "",
	}, "", 0)
	if result.err != nil {
		t.Fatalf("processor entrypoint failed on center-only secrets: %v\n%s", result.err, result.output)
	}
	if !result.childRan {
		t.Fatal("processor entrypoint did not execute its child")
	}
}

func TestDockerEntrypointRequiresHTTPSOnlyForProductionCompose(t *testing.T) {
	base := map[string]string{
		"HOUFENG_DATABASE_USER":     "houfeng_runtime",
		"HOUFENG_DATABASE_NAME":     "houfeng",
		"HOUFENG_DATABASE_PASSWORD": "runtime-secret",
	}

	productionHTTP := maps.Clone(base)
	productionHTTP["HOUFENG_REQUIRE_HTTPS_PUBLIC_BASE_URL"] = "true"
	productionHTTP["HOUFENG_PUBLIC_BASE_URL"] = "http://center.example.com"
	result := runDockerEntrypoint(t, productionHTTP, "", 0)
	if result.err == nil || result.childRan {
		t.Fatalf("production Compose HTTP origin unexpectedly ran child; output:\n%s", result.output)
	}
	if !strings.Contains(result.output, "production Compose requires an https:// HOUFENG_PUBLIC_BASE_URL") {
		t.Fatalf("production Compose HTTP rejection = %q", result.output)
	}

	productionHTTPS := maps.Clone(base)
	productionHTTPS["HOUFENG_REQUIRE_HTTPS_PUBLIC_BASE_URL"] = "true"
	productionHTTPS["HOUFENG_PUBLIC_BASE_URL"] = "https://center.example.com"
	result = runDockerEntrypoint(t, productionHTTPS, "", 0)
	if result.err != nil || !result.childRan {
		t.Fatalf("production Compose HTTPS origin did not run child: %v\n%s", result.err, result.output)
	}

	localHTTP := maps.Clone(base)
	localHTTP["HOUFENG_PUBLIC_BASE_URL"] = "http://127.0.0.1:8080"
	result = runDockerEntrypoint(t, localHTTP, "", 0)
	if result.err != nil || !result.childRan {
		t.Fatalf("direct local HTTP path was affected by Compose-only preflight: %v\n%s", result.err, result.output)
	}
}

func TestProductionComposeIncludesBoundedOneShotInitializers(t *testing.T) {
	t.Parallel()

	compose := readText(t, filepath.Join(repoRoot(t), "compose.yaml"))
	storageInit := composeServiceBlock(t, compose, "houfeng-storage-init")
	databaseInit := composeServiceBlock(t, compose, "houfeng-db-init")
	houfeng := composeServiceBlock(t, compose, "houfeng")
	processor := composeServiceBlock(t, compose, "houfeng-content-processor")

	for service, block := range map[string]string{
		"houfeng-storage-init": storageInit,
		"houfeng-db-init":      databaseInit,
	} {
		if !strings.Contains(block, `restart: "no"`) {
			t.Fatalf("%s must be a bounded one-shot service with restart disabled:\n%s", service, block)
		}
	}

	for service, block := range map[string]string{
		"houfeng":                   houfeng,
		"houfeng-content-processor": processor,
	} {
		if !strings.Contains(block, "houfeng-storage-init:\n        condition: service_completed_successfully") {
			t.Fatalf("%s must wait for successful storage initialization:\n%s", service, block)
		}
		if !strings.Contains(block, "houfeng-db-init:\n        condition: service_completed_successfully") {
			t.Fatalf("%s must wait for successful database initialization:\n%s", service, block)
		}
	}
}

func TestAttachmentRuntimeDeploymentIsPersistentAndIsolated(t *testing.T) {
	root := repoRoot(t)
	dockerfile := readText(t, filepath.Join(root, "Dockerfile"))
	compose := readText(t, filepath.Join(root, "compose.yaml"))
	processorUnit := readText(t, filepath.Join(root, "docs", "deploy", "systemd", "houfeng-content-processor.service"))

	for _, required := range []string{
		"-o /out/houfeng-content-processor ./cmd/houfeng-content-processor",
		"poppler-utils",
		"/out/houfeng-content-processor /usr/local/bin/houfeng-content-processor",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Fatalf("Dockerfile must contain attachment processor runtime contract %q", required)
		}
	}

	center := composeServiceBlock(t, compose, "houfeng")
	processor := composeServiceBlock(t, compose, "houfeng-content-processor")
	clamav := composeServiceBlock(t, compose, "clamav")
	for _, block := range []string{center, processor} {
		for _, required := range []string{
			"./data/attachments:/var/lib/houfeng/attachments",
			"HOUFENG_ATTACHMENT_BLOB_BACKEND",
			"HOUFENG_ATTACHMENT_BLOB_ROOT",
		} {
			if !strings.Contains(block, required) {
				t.Fatalf("center/processor must share persistent attachment config %q:\n%s", required, block)
			}
		}
	}
	for _, required := range []string{
		`command: ["/usr/local/bin/houfeng-content-processor"]`,
		`user: "houfeng:houfeng"`,
		"read_only: true",
		"cap_drop:",
		"- ALL",
		"no-new-privileges:true",
		"tmpfs:",
		"HOUFENG_CONTENT_PROCESSOR_WORKSPACE_ROOT",
		"HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_ITEMS",
		"HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_RUNTIME",
		"HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS",
		"HOUFENG_CONTENT_PROCESSOR_JOB_TTL",
		"core:",
		"soft: 0",
		"hard: 0",
	} {
		if !strings.Contains(processor, required) {
			t.Fatalf("processor Compose service must contain isolation/bound %q:\n%s", required, processor)
		}
	}
	for _, required := range []string{"image: clamav/clamav:", "healthcheck:", "clamdcheck.sh"} {
		if !strings.Contains(clamav, required) {
			t.Fatalf("ClamAV Compose service must contain pinned readiness contract %q:\n%s", required, clamav)
		}
	}
	if strings.Contains(clamav, "clamav/clamav:latest") {
		t.Fatal("ClamAV image must use a pinned version, not latest")
	}
	for _, required := range []string{
		"User=houfeng",
		"Group=houfeng",
		"ExecStart=/usr/local/bin/houfeng-content-processor",
		"NoNewPrivileges=true",
		"PrivateTmp=true",
		"ProtectSystem=strict",
		"LimitCORE=0",
		"RuntimeDirectory=houfeng-content-processor",
		"RuntimeDirectoryMode=0700",
		"ReadWritePaths=/var/lib/houfeng/attachments /run/houfeng-content-processor",
	} {
		if !strings.Contains(processorUnit, required) {
			t.Fatalf("processor systemd unit must contain %q", required)
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

func TestAppACLR2PreR1ProvisioningRevokesPGControlSystemFromPublic(t *testing.T) {
	root := repoRoot(t)
	provisioning := readText(t, filepath.Join(root, "docs", "deploy", "app-acl-r2-pre-r1-provisioning.sql"))
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
	if strings.Contains(strings.ToUpper(runtimeReader), "REVOKE EXECUTE ON FUNCTION PG_CATALOG.PG_CONTROL_SYSTEM") {
		t.Fatal("APP ACL R2 runtime catalog reader must not repair pg_control_system() privileges")
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

func composeServiceBlock(t *testing.T, body, service string) string {
	t.Helper()
	marker := "\n  " + service + ":\n"
	start := strings.Index("\n"+body, marker)
	if start < 0 {
		t.Fatalf("compose.yaml must define service %q", service)
	}
	remaining := ("\n" + body)[start+len(marker):]
	lines := strings.Split(remaining, "\n")
	end := len(lines)
	for index, line := range lines {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			end = index
			break
		}
	}
	return marker + strings.Join(lines[:end], "\n")
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
