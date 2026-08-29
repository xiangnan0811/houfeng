package deploy_test

import (
	"path/filepath"
	"strings"
	"testing"
)

const requiredProjectImage = `image: "${HOUFENG_IMAGE:?set HOUFENG_IMAGE in .env}"`

func TestProductionImageIncludesDatabaseInitializer(t *testing.T) {
	t.Parallel()

	dockerfile := readText(t, filepath.Join(repoRoot(t), "Dockerfile"))
	for _, required := range []string{
		"-o /out/houfeng-record-platform-admin ./cmd/houfeng-record-platform-admin",
		"/out/houfeng-record-platform-admin /usr/local/bin/houfeng-record-platform-admin",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Fatalf("production image must include database initializer binary through %q", required)
		}
	}
}

func TestProductionComposeUsesPublishedFullStack(t *testing.T) {
	t.Parallel()

	compose := readText(t, filepath.Join(repoRoot(t), "compose.yaml"))
	expectedServices := []string{
		"houfeng-storage-init",
		"houfeng-secrets-init",
		"db",
		"houfeng-db-init",
		"houfeng-record-authority",
		"clamav",
		"houfeng",
		"houfeng-content-processor",
	}
	for _, service := range expectedServices {
		composeServiceBlock(t, compose, service)
	}
	if services := composeTopLevelServiceNames(compose); len(services) != len(expectedServices) {
		t.Fatalf("production compose must define exactly the reviewed eight-service graph; got %v", services)
	}
	for _, service := range []string{
		"houfeng-storage-init",
		"houfeng-db-init",
		"houfeng-record-authority",
		"houfeng",
		"houfeng-content-processor",
	} {
		block := composeServiceBlock(t, compose, service)
		if !strings.Contains(block, requiredProjectImage) {
			t.Fatalf("%s must use the operator-pinned published project image:\n%s", service, block)
		}
	}
	if strings.Contains(compose, "\n    build:") {
		t.Fatal("production compose must never build project services from source")
	}
	for _, forbidden := range []string{"\n  caddy:", "\n  houfeng-agent:", "\n  agent:"} {
		if strings.Contains(compose, forbidden) {
			t.Fatalf("production compose must not define %q", forbidden)
		}
	}
	center := composeServiceBlock(t, compose, "houfeng")
	for _, invariant := range []string{
		`HOUFENG_RECORDS_ENABLED: "true"`,
		`HOUFENG_RECORD_PERMANENT_DELETE_ENABLED: "false"`,
	} {
		if !strings.Contains(center, invariant) {
			t.Fatalf("center must pin production mode invariant %q:\n%s", invariant, center)
		}
	}
	for _, forbidden := range []string{
		"${HOUFENG_RECORDS_ENABLED",
		"${HOUFENG_RECORD_PERMANENT_DELETE_ENABLED",
	} {
		if strings.Contains(center, forbidden) {
			t.Fatalf("production ACL/deletion invariants must not be operator-selectable through %q:\n%s", forbidden, center)
		}
	}
	if !strings.Contains(center, `HOUFENG_REQUIRE_HTTPS_PUBLIC_BASE_URL: "true"`) {
		t.Fatalf("production Center must activate the Compose-only HTTPS origin preflight:\n%s", center)
	}
}

func composeTopLevelServiceNames(body string) []string {
	var services []string
	inServices := false
	for _, line := range strings.Split(body, "\n") {
		if line == "services:" {
			inServices = true
			continue
		}
		if !inServices {
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") {
			break
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(line, ":") {
			services = append(services, strings.TrimSuffix(strings.TrimSpace(line), ":"))
		}
	}
	return services
}

func TestProductionComposeWiresFailClosedRecordsAuthority(t *testing.T) {
	t.Parallel()

	compose := readText(t, filepath.Join(repoRoot(t), "compose.yaml"))
	storageInit := composeServiceBlock(t, compose, "houfeng-storage-init")
	databaseInit := composeServiceBlock(t, compose, "houfeng-db-init")
	authority := composeServiceBlock(t, compose, "houfeng-record-authority")
	center := composeServiceBlock(t, compose, "houfeng")

	for _, required := range []string{
		"./data:/state",
		"/state/attachments",
		"/state/logs",
		"/state/center-config",
		"10002",
	} {
		if !strings.Contains(storageInit, required) {
			t.Fatalf("storage initializer must prepare authority/public topology through %q:\n%s", required, storageInit)
		}
	}
	for _, required := range []string{
		`user: "10002:10002"`,
		"./data:/var/lib/houfeng",
		`command: ["deploy-init", "--scope", "compose"]`,
		"houfeng-storage-init:\n        condition: service_completed_successfully",
	} {
		if !strings.Contains(databaseInit, required) {
			t.Fatalf("database initializer must own private authority initialization through %q:\n%s", required, databaseInit)
		}
	}
	for _, required := range []string{
		requiredProjectImage,
		`user: "10002:10002"`,
		`entrypoint: ["/usr/local/bin/houfeng-record-platform-admin"]`,
		`command: ["record-authority", "--scope", "compose"]`,
		"./data:/var/lib/houfeng:ro",
		"houfeng-db-init:\n        condition: service_completed_successfully",
		"healthcheck:",
		"http://127.0.0.1:16002/healthz",
		`restart: unless-stopped`,
	} {
		if !strings.Contains(authority, required) {
			t.Fatalf("Records authority service must contain %q:\n%s", required, authority)
		}
	}
	for _, forbidden := range []string{
		"secrets:",
		"postgres_bootstrap_password",
		"houfeng_runtime_password",
		"houfeng_migrator_password",
		"houfeng_platform_admin_password",
		"houfeng-proxy",
	} {
		if strings.Contains(authority, forbidden) {
			t.Fatalf("Records authority must not receive operator/database-admin scope %q:\n%s", forbidden, authority)
		}
	}
	for _, required := range []string{
		`HOUFENG_RECORD_DEPLOYMENT_ID_FILE: /run/houfeng/records-authority/deployment-id`,
		`HOUFENG_RECORD_INSTANCE_ID: compose-center`,
		`HOUFENG_RECORD_INSTANCE_KIND: api`,
		`HOUFENG_RECORD_INSTANCE_CAPABILITY: records.runtime`,
		"./data/center-config:/run/houfeng/records-authority:ro",
		"houfeng-record-authority:\n        condition: service_healthy",
	} {
		if !strings.Contains(center, required) {
			t.Fatalf("Center must receive only public fixed authority identity through %q:\n%s", required, center)
		}
	}
	for _, forbidden := range []string{"authority-key", "database-secret", "activation-ledger", "houfeng_records_authority"} {
		if strings.Contains(center, forbidden) {
			t.Fatalf("Center must not receive private authority material %q:\n%s", forbidden, center)
		}
	}
}

func TestProductionRecordsAuthorityMasksStagedOperatorSecrets(t *testing.T) {
	t.Parallel()

	compose := readText(t, filepath.Join(repoRoot(t), "compose.yaml"))
	authority := composeServiceBlock(t, compose, "houfeng-record-authority")
	for _, required := range []string{
		"- type: tmpfs",
		"target: /var/lib/houfeng/secrets",
		"size: 4096",
		"mode: 0",
	} {
		if !strings.Contains(authority, required) {
			t.Fatalf("Records authority must mask staged operator secrets through %q inside its required atomic-state parent mount:\n%s", required, authority)
		}
	}
}

func TestProductionComposeStagesEnvironmentSecretsForReadOnlyServices(t *testing.T) {
	t.Parallel()

	compose := readText(t, filepath.Join(repoRoot(t), "compose.yaml"))
	stager := composeServiceBlock(t, compose, "houfeng-secrets-init")
	for _, required := range []string{
		requiredProjectImage,
		`user: "10002:10002"`,
		"group_add:\n      - \"10001\"",
		`network_mode: none`,
		`./data/secrets:/state/secrets`,
		`/state/secrets/db-init`,
		`/state/secrets/processor`,
		"postgres_bootstrap_password",
		"houfeng_runtime_password",
		"houfeng_migrator_password",
		"houfeng_platform_admin_password",
	} {
		if !strings.Contains(stager, required) {
			t.Fatalf("secret stager must materialize only database-role files through %q:\n%s", required, stager)
		}
	}
	for _, forbidden := range []string{"houfeng_initial_password", "houfeng_session_hmac_key", "houfeng-proxy"} {
		if strings.Contains(stager, forbidden) {
			t.Fatalf("secret stager must not receive Center-only material %q:\n%s", forbidden, stager)
		}
	}

	tests := []struct {
		service string
		mount   string
	}{
		{service: "houfeng-db-init", mount: "./data/secrets/db-init:/run/secrets:ro"},
		{service: "houfeng-content-processor", mount: "./data/secrets/processor:/run/secrets:ro"},
	}
	for _, tt := range tests {
		block := composeServiceBlock(t, compose, tt.service)
		if !strings.Contains(block, `read_only: true`) || !strings.Contains(block, tt.mount) {
			t.Fatalf("%s must remain read-only and consume staged files via %q:\n%s", tt.service, tt.mount, block)
		}
		if strings.Contains(block, "\n    secrets:") {
			t.Fatalf("%s cannot use environment-backed Compose secret injection with a read-only root:\n%s", tt.service, block)
		}
		if !strings.Contains(block, "houfeng-secrets-init:\n        condition: service_completed_successfully") {
			t.Fatalf("%s must wait for staged secrets:\n%s", tt.service, block)
		}
	}
}

func TestProductionComposeUsesPortableData(t *testing.T) {
	t.Parallel()

	compose := readText(t, filepath.Join(repoRoot(t), "compose.yaml"))
	serviceRequirements := map[string][]string{
		"houfeng-storage-init": {
			"./data:/state",
			"/state/attachments",
			"/state/logs",
			"/state/center-config",
		},
		"houfeng": {
			"./data/attachments:/var/lib/houfeng/attachments",
			"./data/logs:/var/log/houfeng",
		},
		"houfeng-content-processor": {
			"./data/attachments:/var/lib/houfeng/attachments",
		},
		"clamav": {
			"./data/clamav:/var/lib/clamav",
		},
		"db": {
			"./data/postgres:/var/lib/postgresql/data",
		},
	}
	for service, requirements := range serviceRequirements {
		block := composeServiceBlock(t, compose, service)
		for _, required := range requirements {
			if !strings.Contains(block, required) {
				t.Fatalf("%s must contain portable data contract %q:\n%s", service, required, block)
			}
		}
	}
	if strings.Contains(compose, "\nvolumes:\n") {
		t.Fatal("production business data must use visible ./data bind mounts, not top-level named volumes")
	}
}

func TestProductionComposeDefinesExplicitProxyModes(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	base := readText(t, filepath.Join(root, "compose.yaml"))
	networkMode := readText(t, filepath.Join(root, "compose.proxy-network.yaml"))
	hostMode := readText(t, filepath.Join(root, "compose.proxy-host.yaml"))

	const expectedNetworkMode = `services:
  houfeng:
    networks:
      houfeng-proxy:
        aliases:
          - houfeng

networks:
  houfeng-proxy:
    external: true
    name: "${HOUFENG_PROXY_NETWORK:?set HOUFENG_PROXY_NETWORK for shared-network mode}"`
	const expectedHostMode = `services:
  houfeng:
    ports:
      - name: npm-host-proxy
        target: 16001
        published: "16001"
        host_ip: 127.0.0.1
        protocol: tcp`
	for _, mode := range []struct {
		name string
		got  string
		want string
	}{
		{name: "shared-network", got: networkMode, want: expectedNetworkMode},
		{name: "host-proxy", got: hostMode, want: expectedHostMode},
	} {
		if got, want := strings.TrimSpace(mode.got), strings.TrimSpace(mode.want); got != want {
			t.Fatalf("%s mode file must remain the exact thin reviewed overlay:\nwant:\n%s\n\ngot:\n%s", mode.name, want, got)
		}
	}

	center := composeServiceBlock(t, base, "houfeng")
	if !strings.Contains(center, "networks:\n      default:") {
		t.Fatalf("base Center must retain its private default network:\n%s", center)
	}
	for _, forbidden := range []string{"HOUFENG_PROXY_NETWORK", "houfeng-proxy", "\n    ports:"} {
		if strings.Contains(base, forbidden) {
			t.Fatalf("base Compose must not own proxy mode value %q", forbidden)
		}
	}

	networkCenter := composeServiceBlock(t, networkMode, "houfeng")
	for _, required := range []string{
		"houfeng-proxy:",
		"aliases:",
		"- houfeng",
		"name: \"${HOUFENG_PROXY_NETWORK:?set HOUFENG_PROXY_NETWORK for shared-network mode}\"",
		"external: true",
	} {
		if !strings.Contains(networkMode, required) && !strings.Contains(networkCenter, required) {
			t.Fatalf("shared-network mode must contain %q", required)
		}
	}
	if strings.Contains(networkMode, "ports:") {
		t.Fatal("shared-network mode must not publish Center")
	}

	hostCenter := composeServiceBlock(t, hostMode, "houfeng")
	for _, required := range []string{
		"ports:",
		"target: 16001",
		"published: \"16001\"",
		"host_ip: 127.0.0.1",
		"protocol: tcp",
	} {
		if !strings.Contains(hostCenter, required) {
			t.Fatalf("host-proxy mode must contain %q", required)
		}
	}
	for _, forbidden := range []string{"HOUFENG_PROXY_NETWORK", "external:", "houfeng-proxy", "0.0.0.0", "host_ip: \"::\""} {
		if strings.Contains(hostMode, forbidden) {
			t.Fatalf("host-proxy mode contains forbidden value %q", forbidden)
		}
	}
}

func TestProductionComposeScopesSecretsByService(t *testing.T) {
	t.Parallel()

	compose := readText(t, filepath.Join(repoRoot(t), "compose.yaml"))
	tests := []struct {
		service string
		want    []string
		forbid  []string
	}{
		{
			service: "houfeng-storage-init",
			forbid:  []string{"_password", "session_hmac", "secrets:"},
		},
		{
			service: "db",
			want:    []string{"postgres_bootstrap_password"},
			forbid:  []string{"houfeng_runtime_password", "houfeng_migrator_password", "houfeng_platform_admin_password", "houfeng_initial_password", "houfeng_session_hmac_key"},
		},
		{
			service: "houfeng-secrets-init",
			want:    []string{"./data/secrets:/state/secrets", "postgres_bootstrap_password", "houfeng_runtime_password", "houfeng_migrator_password", "houfeng_platform_admin_password"},
			forbid:  []string{"./data:/state", "houfeng_initial_password", "houfeng_session_hmac_key", "records-authority", "attachments", "center-config"},
		},
		{
			service: "houfeng-db-init",
			want:    []string{"./data/secrets/db-init:/run/secrets:ro"},
			forbid:  []string{"\n    secrets:", "houfeng_initial_password", "houfeng_session_hmac_key"},
		},
		{
			service: "houfeng-record-authority",
			forbid:  []string{"secrets:", "postgres_bootstrap_password", "houfeng_runtime_password", "houfeng_migrator_password", "houfeng_platform_admin_password", "houfeng_initial_password", "houfeng_session_hmac_key"},
		},
		{
			service: "houfeng",
			want:    []string{"houfeng_runtime_password", "houfeng_initial_password", "houfeng_session_hmac_key"},
			forbid:  []string{"postgres_bootstrap_password", "houfeng_migrator_password", "houfeng_platform_admin_password"},
		},
		{
			service: "houfeng-content-processor",
			want:    []string{"houfeng_runtime_password", "./data/secrets/processor:/run/secrets:ro"},
			forbid:  []string{"\n    secrets:", "postgres_bootstrap_password", "houfeng_migrator_password", "houfeng_platform_admin_password", "houfeng_initial_password", "houfeng_session_hmac_key"},
		},
	}
	for _, tt := range tests {
		block := composeServiceBlock(t, compose, tt.service)
		for _, want := range tt.want {
			if !strings.Contains(block, want) {
				t.Fatalf("%s must receive %q:\n%s", tt.service, want, block)
			}
		}
		for _, forbidden := range tt.forbid {
			if strings.Contains(block, forbidden) {
				t.Fatalf("%s must not receive %q:\n%s", tt.service, forbidden, block)
			}
		}
	}

	for _, secret := range []string{
		"postgres_bootstrap_password",
		"houfeng_runtime_password",
		"houfeng_migrator_password",
		"houfeng_platform_admin_password",
		"houfeng_initial_password",
		"houfeng_session_hmac_key",
	} {
		definition := secret + ":\n    environment:"
		if !strings.Contains(compose, definition) {
			t.Fatalf("Compose secret %q must be sourced from the root .env environment, not a host file path", secret)
		}
	}
}

func TestProductionComposeKeepsComparisonKeysOutOfProcessor(t *testing.T) {
	t.Parallel()

	compose := readText(t, filepath.Join(repoRoot(t), "compose.yaml"))
	center := composeServiceBlock(t, compose, "houfeng")
	processor := composeServiceBlock(t, compose, "houfeng-content-processor")
	comparisonMount := "./optional-secrets/comparison-keyring:/run/houfeng/optional-secrets/comparison-keyring:ro"
	s3Mount := "./optional-secrets/s3:/run/houfeng/optional-secrets/s3:ro"
	for _, mount := range []string{comparisonMount, s3Mount} {
		if !strings.Contains(center, mount) {
			t.Fatalf("Center must receive scoped optional-secret mount %q:\n%s", mount, center)
		}
	}
	if !strings.Contains(processor, s3Mount) {
		t.Fatalf("processor must receive only the shared S3 credential directory:\n%s", processor)
	}
	for _, forbidden := range []string{comparisonMount, "./optional-secrets:/run/houfeng/optional-secrets:ro"} {
		if strings.Contains(processor, forbidden) {
			t.Fatalf("processor received Center-only or wholesale optional secrets through %q:\n%s", forbidden, processor)
		}
	}
}

func TestProductionComposeDocumentsContainerReadableOptionalSecretOwnership(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		filepath.Join(repoRoot(t), "README.md"),
		filepath.Join(repoRoot(t), "docs", "deploy", "local-and-systemd.md"),
	} {
		document := readText(t, path)
		if !strings.Contains(document, "sudo install -d -o 10001 -g 10001 -m 0700 optional-secrets optional-secrets/comparison-keyring optional-secrets/s3") {
			t.Fatalf("%s must create private optional-secret bind mounts for the non-root container UID/GID", path)
		}
	}
}

func TestProductionComposeRejectsBlankSecretEnvironmentValues(t *testing.T) {
	t.Parallel()

	compose := readText(t, filepath.Join(repoRoot(t), "compose.yaml"))
	for _, variable := range []string{
		"POSTGRES_BOOTSTRAP_PASSWORD",
		"HOUFENG_RUNTIME_PASSWORD",
		"HOUFENG_PLATFORM_ADMIN_PASSWORD",
		"HOUFENG_MIGRATOR_PASSWORD",
		"HOUFENG_INITIAL_PASSWORD",
		"HOUFENG_SESSION_HMAC_KEY",
	} {
		guard := `environment: "${` + variable + `:+` + variable + `}"`
		if !strings.Contains(compose, guard) {
			t.Fatalf("Compose secret %s must reject a blank source without rendering its value; want %q", variable, guard)
		}
	}
}

func TestProductionEnvironmentTemplateHasThreeOperatorSections(t *testing.T) {
	t.Parallel()

	envExample := readText(t, filepath.Join(repoRoot(t), "docs", "deploy", "compose.env.example"))
	mustChange := strings.Index(envExample, "# Must change")
	recommended := strings.Index(envExample, "# Recommended")
	optional := strings.Index(envExample, "# Optional")
	if mustChange < 0 || recommended < 0 || optional < 0 || !(mustChange < recommended && recommended < optional) {
		t.Fatalf("environment template must contain ordered Must change, Recommended, and Optional sections")
	}
	image := strings.Index(envExample, "HOUFENG_IMAGE=")
	if image <= recommended || image >= optional {
		t.Fatal("release-pinned HOUFENG_IMAGE must be Recommended, not a user-required Must change value")
	}
	for _, heading := range []string{"# Must change", "# Recommended", "# Optional"} {
		if strings.Count(envExample, heading) != 1 {
			t.Fatalf("environment template must contain exactly one %q heading", heading)
		}
	}
	const defaultComposeFile = "COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml"
	var activeComposeFileAssignments []string
	for _, line := range strings.Split(envExample, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "COMPOSE_FILE=") {
			activeComposeFileAssignments = append(activeComposeFileAssignments, line)
		}
	}
	if len(activeComposeFileAssignments) != 1 {
		t.Fatalf("environment template must contain exactly one active COMPOSE_FILE assignment, got %v", activeComposeFileAssignments)
	}
	if activeComposeFileAssignments[0] != defaultComposeFile {
		t.Fatalf("environment template must select only the shared-network overlay by default: got %q, want %q", activeComposeFileAssignments[0], defaultComposeFile)
	}
	for _, required := range []string{
		"COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml",
		"compose.proxy-host.yaml",
		"HOUFENG_IMAGE=",
		"HOUFENG_PROXY_NETWORK=",
		"HOUFENG_PUBLIC_BASE_URL=",
		"POSTGRES_BOOTSTRAP_PASSWORD=",
		"HOUFENG_RUNTIME_PASSWORD=",
		"HOUFENG_MIGRATOR_PASSWORD=",
		"HOUFENG_PLATFORM_ADMIN_PASSWORD=",
		"HOUFENG_INITIAL_USERNAME=",
		"HOUFENG_INITIAL_PASSWORD=",
		"HOUFENG_SESSION_HMAC_KEY=",
		"COMPOSE_PROJECT_NAME=houfeng",
		"HOUFENG_RECORDS_ENABLED=true",
		"openssl rand -hex 32",
		"Generate a fresh value for every secret",
		"Never reuse one value",
		"exact observed proxy-source",
		"for the selected mode",
		"Do not guess 127.0.0.0/8",
		"0.0.0.0/0",
		"::/0",
	} {
		if !strings.Contains(envExample, required) {
			t.Fatalf("environment template must expose %q", required)
		}
	}
	for _, forbidden := range []string{
		"linnea7171/houfeng:latest",
		"POSTGRES_BOOTSTRAP_PASSWORD_FILE=",
		"HOUFENG_DATABASE_PASSWORD_FILE=",
		"HOUFENG_RECORD_PERMANENT_DELETE_ENABLED=",
		"HOUFENG_CLAMAV_DIAL_TIMEOUT=",
		"HOUFENG_CLAMAV_OPERATION_TIMEOUT=",
		"HOUFENG_CLAMAV_CHUNK_SIZE=",
		"HOUFENG_CLAMAV_RESPONSE_LIMIT=",
		"HOUFENG_CONTENT_PROCESSOR_LEASE_DURATION=",
		"HOUFENG_CONTENT_PROCESSOR_CLEANUP_TIMEOUT=",
		"HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_ITEMS=",
		"HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_RUNTIME=",
		"HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_RETRY_DELAY=",
		"HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS=",
		"HOUFENG_CONTENT_PROCESSOR_JOB_TTL=",
		"HOUFENG_PROXY_NETWORK=host",
		"HOUFENG_HOST_PORT=",
	} {
		if strings.Contains(envExample, forbidden) {
			t.Fatalf("environment template must not contain legacy/mutable setting %q", forbidden)
		}
	}
}

func TestProductionSecretRotationExplicitlyRerunsInitializer(t *testing.T) {
	t.Parallel()

	guide := readText(t, filepath.Join(repoRoot(t), "docs", "deploy", "local-and-systemd.md"))
	composeGuide := markdownSection(t, guide, "## Docker Compose deployment", "## Agent environment")
	steps := []string{
		"docker compose stop houfeng houfeng-content-processor",
		"docker compose run --rm houfeng-secrets-init",
		"docker compose run --rm houfeng-db-init",
		"docker compose up -d --force-recreate houfeng houfeng-content-processor",
	}
	last := -1
	for _, step := range steps {
		offset := strings.Index(composeGuide, step)
		if offset < 0 {
			t.Fatalf("production password rotation must include %q", step)
		}
		if offset <= last {
			t.Fatalf("production password rotation steps are out of order at %q", step)
		}
		last = offset
	}
	if strings.Contains(composeGuide, "replace only that value in `.env` and rerun `docker compose up -d`") {
		t.Fatal("ordinary compose up does not hash environment-sourced secret values and must not be documented as a sufficient rotation trigger")
	}
}

func TestProductionGuideDocumentsAuthorityAndPortableRecoveryUnit(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	readme := readText(t, filepath.Join(root, "README.md"))
	guide := readText(t, filepath.Join(root, "docs", "deploy", "local-and-systemd.md"))
	composeGuide := markdownSection(t, guide, "## Docker Compose deployment", "## Agent environment")
	for _, required := range []string{
		"houfeng-secrets-init",
		"houfeng-record-authority",
		"data/records-authority",
		"data/center-config",
		"data/secrets",
		"PostgreSQL, local attachments, and Records authority state",
		"restore PostgreSQL and Records authority state together",
	} {
		if !strings.Contains(composeGuide, required) {
			t.Fatalf("production Compose guide must document authority/recovery contract %q", required)
		}
	}
	for _, required := range []string{"Records authority", "./data/records-authority"} {
		if !strings.Contains(readme, required) {
			t.Fatalf("README production overview must mention %q", required)
		}
	}
	const recoveryUnit = "`compose.yaml`, `compose.proxy-network.yaml`, `compose.proxy-host.yaml`, `.env`, `optional-secrets/`, and the entire `data/` tree"
	for name, document := range map[string]string{"README": readme, "deployment guide": composeGuide} {
		normalizedDocument := strings.Join(strings.Fields(document), " ")
		if !strings.Contains(normalizedDocument, recoveryUnit) {
			t.Fatalf("%s must preserve all proxy-mode assets in the portable recovery unit through %q", name, recoveryUnit)
		}
	}
}

func TestProductionQuickStartUsesReleaseAssetsAndAutomaticInitialization(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	readme := readText(t, filepath.Join(root, "README.md"))
	guide := readText(t, filepath.Join(root, "docs", "deploy", "local-and-systemd.md"))
	composeGuide := markdownSection(t, guide, "## Docker Compose deployment", "## Agent environment")
	for name, document := range map[string]string{"README": readme, "deployment guide": composeGuide} {
		steps := []string{
			"https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.yaml",
			"https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.proxy-network.yaml",
			"https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.proxy-host.yaml",
			"https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.env.example",
			"docker compose config",
			"docker compose up -d",
		}
		last := -1
		for _, step := range steps {
			offset := strings.Index(document, step)
			if offset < 0 {
				t.Fatalf("%s must contain quick-start step %q", name, step)
			}
			if offset <= last {
				t.Fatalf("%s quick start must order download, edit/validate, then up; %q is out of order", name, step)
			}
			last = offset
		}
		postStartup := document[last+len("docker compose up -d"):]
		hasPublicHealthProbe := false
		for _, line := range strings.Split(postStartup, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "curl -fsS ") &&
				strings.Contains(line, "/api/healthz") &&
				!strings.Contains(line, "http://") &&
				!strings.Contains(line, "127.0.0.1") {
				hasPublicHealthProbe = true
				break
			}
		}
		if !hasPublicHealthProbe {
			t.Fatalf("%s must verify the public HTTPS /api/healthz route after docker compose up -d", name)
		}
		if strings.Contains(document, "github.com/linnea7171/houfeng/") {
			t.Fatalf("%s must download release assets from the canonical xiangnan0811/houfeng repository", name)
		}
		for _, required := range []string{
			"COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml",
			"COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml",
			`docker inspect "$(docker compose ps -q app)"`,
			".NetworkSettings.Networks",
			"houfeng:16001",
			"127.0.0.1:16001",
			"Docker Engine 28.0.0",
			`docker version --format '{{.Server.Version}}'`,
		} {
			if !strings.Contains(document, required) {
				t.Fatalf("%s must document the two reviewed NPM proxy modes through %q", name, required)
			}
		}
		lowerDocument := strings.ToLower(document)
		for _, forbidden := range []string{
			"houfeng_proxy_network=host",
			"houfeng_proxy_network=bridge",
			"houfeng_proxy_network=npm-network",
			"houfeng_proxy_network=placeholder",
			"houfeng_proxy_network=example",
			"houfeng_proxy_network=your-network",
			"create a placeholder proxy network",
			"change npm from host to bridge",
			"move npm from host to bridge",
			"reconfigure npm from host to bridge",
			"switch npm from host to bridge",
		} {
			if strings.Contains(lowerDocument, forbidden) {
				t.Fatalf("%s must not recommend unsupported NPM compatibility instruction %q", name, forbidden)
			}
		}
	}
	if !strings.Contains(readme, `curl -fsS "${HOUFENG_PUBLIC_BASE_URL%/}/api/healthz"`) {
		t.Fatal("README public health check must reuse HOUFENG_PUBLIC_BASE_URL without exposing deployment secrets")
	}
	for _, required := range []string{
		`awk -F= '$1 == "HOUFENG_PUBLIC_BASE_URL" {`,
		`value = substr($0, index($0, "=") + 1)`,
		`quote = substr(value, 1, 1)`,
		`quote == "\"" || quote == sprintf("%c", 39)`,
		`substr(value, length(value), 1) == quote`,
		`value = substr(value, 2, length(value) - 2)`,
	} {
		if !strings.Contains(readme, required) {
			t.Fatalf("README public health extraction must handle unquoted, single-quoted, and double-quoted HOUFENG_PUBLIC_BASE_URL through %q", required)
		}
	}
	for _, forbidden := range []string{
		"scripts/compose-up.sh",
		"docs/deploy/app-acl-r2-pre-r1-provisioning.sql",
		"docs/deploy/compose-application-role.sql",
	} {
		if strings.Contains(readme, forbidden) || strings.Contains(composeGuide, forbidden) {
			t.Fatalf("ordinary production quick start must not require %q", forbidden)
		}
	}
}

func markdownSection(t *testing.T, document, startHeading, endHeading string) string {
	t.Helper()
	start := strings.Index(document, startHeading)
	if start < 0 {
		t.Fatalf("document is missing section heading %q", startHeading)
	}
	end := strings.Index(document[start+len(startHeading):], endHeading)
	if end < 0 {
		t.Fatalf("document is missing section boundary %q", endHeading)
	}
	return document[start : start+len(startHeading)+end]
}

func TestPublishWorkflowSerializesDeploymentAssetsByResolvedVersion(t *testing.T) {
	t.Parallel()

	workflow := readText(t, filepath.Join(repoRoot(t), ".github", "workflows", "publish-images.yml"))
	deploymentJobOffset := strings.Index(workflow, "\n  deployment-assets:\n")
	if deploymentJobOffset < 0 {
		t.Fatal("publish workflow must define the deployment-assets job")
	}
	deploymentJob := workflow[deploymentJobOffset:]
	const requiredConcurrency = `    concurrency:
      group: deployment-assets-${{ needs.resolve.outputs.version }}
      cancel-in-progress: false`
	if !strings.Contains(deploymentJob, requiredConcurrency) {
		t.Fatalf("deployment-assets must serialize release and workflow_dispatch runs for the same resolved version through:\n%s", requiredConcurrency)
	}
}

func TestPublishWorkflowUploadsVersionMatchedDeploymentAssets(t *testing.T) {
	t.Parallel()

	workflow := readText(t, filepath.Join(repoRoot(t), ".github", "workflows", "publish-images.yml"))
	deploymentJobOffset := strings.Index(workflow, "\n  deployment-assets:\n")
	if deploymentJobOffset < 0 {
		t.Fatal("publish workflow must define the deployment-assets job")
	}
	deploymentJob := workflow[deploymentJobOffset:]
	deploymentAssets := []string{
		"compose.yaml",
		"compose.proxy-network.yaml",
		"compose.proxy-host.yaml",
		"compose.env.example",
	}
	staging := []struct {
		source string
		asset  string
	}{
		{source: "compose.yaml", asset: deploymentAssets[0]},
		{source: "compose.proxy-network.yaml", asset: deploymentAssets[1]},
		{source: "compose.proxy-host.yaml", asset: deploymentAssets[2]},
		{source: "docs/deploy/compose.env.example", asset: deploymentAssets[3]},
	}
	lastStagingOffset := -1
	for _, staged := range staging {
		copyCommand := "cp " + staged.source + " dist/" + staged.asset
		offset := strings.Index(deploymentJob, copyCommand)
		if offset < 0 {
			t.Fatalf("publish workflow must stage deployment asset through %q", copyCommand)
		}
		if offset <= lastStagingOffset {
			t.Fatalf("publish workflow must stage deployment assets in reviewed order; %q is out of order", staged.asset)
		}
		lastStagingOffset = offset
	}

	sharedRender := `shared_network_images="$(docker compose --env-file dist/compose.env.example -f dist/compose.yaml -f dist/compose.proxy-network.yaml config --images)"`
	hostRender := `host_proxy_images="$(env -u HOUFENG_PROXY_NETWORK docker compose --env-file dist/compose.env.example -f dist/compose.yaml -f dist/compose.proxy-host.yaml config --images)"`
	for _, required := range []string{
		"docker.io/linnea7171/houfeng:v${{ needs.resolve.outputs.version }}",
		"HOUFENG_PROXY_NETWORK: houfeng-release-validation",
		`sed -i "s|^HOUFENG_IMAGE=.*$|HOUFENG_IMAGE=$HOUFENG_IMAGE|" dist/compose.env.example`,
		`grep -Fx "HOUFENG_IMAGE=$HOUFENG_IMAGE" dist/compose.env.example`,
		sharedRender,
		hostRender,
		`mapfile -t project_images < <(printf '%s\n%s\n' "$shared_network_images" "$host_proxy_images" | grep -F 'docker.io/linnea7171/houfeng:' | sort -u)`,
		`if [[ "${#project_images[@]}" -ne 1 || "${project_images[0]}" != "$HOUFENG_IMAGE" ]]; then`,
	} {
		if !strings.Contains(deploymentJob, required) {
			t.Fatalf("publish workflow must contain deployment asset contract %q", required)
		}
	}
	sharedRenderOffset := strings.Index(deploymentJob, sharedRender)
	hostRenderOffset := strings.Index(deploymentJob, hostRender)
	exactImageOffset := strings.Index(deploymentJob, `if [[ "${#project_images[@]}" -ne 1 || "${project_images[0]}" != "$HOUFENG_IMAGE" ]]; then`)
	manifestOffset := strings.Index(deploymentJob, `docker buildx imagetools inspect "$HOUFENG_IMAGE"`)
	if !(lastStagingOffset < sharedRenderOffset && sharedRenderOffset < hostRenderOffset && hostRenderOffset < exactImageOffset && exactImageOffset < manifestOffset) {
		t.Fatal("publish workflow must stage assets, render shared-network and host-proxy modes, require their one exact release image, then inspect its manifest")
	}

	uploadStepOffset := strings.Index(deploymentJob, "- name: Upload deployment release assets")
	verificationStepOffset := strings.Index(deploymentJob, "- name: Verify public deployment release assets")
	if uploadStepOffset < 0 || verificationStepOffset <= uploadStepOffset {
		t.Fatal("publish workflow must upload deployment assets before public verification")
	}
	uploadStep := deploymentJob[uploadStepOffset:verificationStepOffset]
	expectedAssetList := "required_deployment_assets=(" + strings.Join(deploymentAssets, " ") + ")"
	if !strings.Contains(uploadStep, expectedAssetList) {
		t.Fatalf("publish workflow must use the exact ordered deployment asset list before upload: %q", expectedAssetList)
	}
}

func TestPublishWorkflowUsesStablePublicComposeEnvironmentAssetName(t *testing.T) {
	t.Parallel()

	workflow := readText(t, filepath.Join(repoRoot(t), ".github", "workflows", "publish-images.yml"))
	for _, required := range []string{
		"cp docs/deploy/compose.env.example dist/compose.env.example",
		"docker compose --env-file dist/compose.env.example -f dist/compose.yaml -f dist/compose.proxy-network.yaml config --images",
		"env -u HOUFENG_PROXY_NETWORK docker compose --env-file dist/compose.env.example -f dist/compose.yaml -f dist/compose.proxy-host.yaml config --images",
		"required_deployment_assets=(compose.yaml compose.proxy-network.yaml compose.proxy-host.yaml compose.env.example)",
		"echo \"- \\`compose.env.example\\`\"",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("publish workflow must stage, validate, upload, and report the stable public deployment asset through %q", required)
		}
	}
	for _, forbidden := range []string{
		"dist/.env.example",
		"dist/default.env.example",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("publish workflow must not use normalized or hidden deployment asset path %q", forbidden)
		}
	}
}

func TestPublishWorkflowSafelyRetriesDeploymentAssetUpload(t *testing.T) {
	t.Parallel()

	workflow := readText(t, filepath.Join(repoRoot(t), ".github", "workflows", "publish-images.yml"))
	deploymentJobOffset := strings.Index(workflow, "\n  deployment-assets:\n")
	if deploymentJobOffset < 0 {
		t.Fatal("publish workflow must define the deployment-assets job")
	}
	deploymentJob := workflow[deploymentJobOffset:]
	uploadStepOffset := strings.Index(deploymentJob, "- name: Upload deployment release assets")
	verificationStepOffset := strings.Index(deploymentJob, "- name: Verify public deployment release assets")
	if uploadStepOffset < 0 || verificationStepOffset <= uploadStepOffset {
		t.Fatal("publish workflow must define upload and post-upload verification steps in order")
	}
	uploadStep := deploymentJob[uploadStepOffset:verificationStepOffset]
	for _, required := range []string{
		"set -euo pipefail",
		`upload_check_dir="$(mktemp -d)"`,
		`trap 'rm -rf "$upload_check_dir"' EXIT`,
		`gh release view "$VERSION" --json assets --jq '.assets[].name'`,
		"required_deployment_assets=(compose.yaml compose.proxy-network.yaml compose.proxy-host.yaml compose.env.example)",
		"missing_deployment_assets=()",
		`for asset in "${required_deployment_assets[@]}"; do`,
		`if [[ "$release_asset" == "$asset" ]]; then`,
		`case "$matches" in`,
		`missing_deployment_assets+=("dist/$asset")`,
		`gh release download "$VERSION" --pattern "$asset" --dir "$upload_check_dir"`,
		`test -f "$upload_check_dir/$asset"`,
		`if cmp -s "dist/$asset" "$upload_check_dir/$asset"; then`,
		`Existing deployment asset $asset is byte-identical; retaining it.`,
		`Existing deployment asset $asset differs from staged bytes; refusing to overwrite.`,
		`Release contains duplicate deployment assets named $asset; refusing to mutate it.`,
		`if [[ "${#missing_deployment_assets[@]}" -gt 0 ]]; then`,
		`gh release upload "$VERSION" "${missing_deployment_assets[@]}"`,
	} {
		if !strings.Contains(uploadStep, required) {
			t.Fatalf("deployment upload retry contract must contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"--clobber",
		"gh release delete-asset",
		"--method DELETE",
		"-X DELETE",
	} {
		if strings.Contains(uploadStep, forbidden) {
			t.Fatalf("deployment upload retries must never destructively replace existing assets through %q", forbidden)
		}
	}
	cardinalityOffset := strings.Index(uploadStep, `case "$matches" in`)
	comparisonOffset := strings.Index(uploadStep, `if cmp -s "dist/$asset" "$upload_check_dir/$asset"; then`)
	uploadOffset := strings.Index(uploadStep, `gh release upload "$VERSION" "${missing_deployment_assets[@]}"`)
	if !(cardinalityOffset >= 0 && cardinalityOffset < comparisonOffset && comparisonOffset < uploadOffset) {
		t.Fatal("deployment upload must inspect cardinality and byte-compare every existing asset before uploading any missing asset")
	}
}

func TestPublishWorkflowVerifiesPublicDeploymentAssetsAfterUpload(t *testing.T) {
	t.Parallel()

	workflow := readText(t, filepath.Join(repoRoot(t), ".github", "workflows", "publish-images.yml"))
	deploymentJobOffset := strings.Index(workflow, "\n  deployment-assets:\n")
	if deploymentJobOffset < 0 {
		t.Fatal("publish workflow must define the deployment-assets job")
	}
	deploymentJob := workflow[deploymentJobOffset:]
	uploadOffset := strings.Index(deploymentJob, "gh release upload")
	verificationOffset := strings.Index(deploymentJob, "- name: Verify public deployment release assets")
	summaryOffset := strings.Index(deploymentJob, "- name: Publish summary")
	if uploadOffset < 0 || verificationOffset <= uploadOffset || summaryOffset <= verificationOffset {
		t.Fatal("publish workflow must upload, publicly read back/download, then report deployment assets")
	}
	verificationStep := deploymentJob[verificationOffset:summaryOffset]
	for _, required := range []string{
		"- name: Verify public deployment release assets",
		`verify_dir="$(mktemp -d)"`,
		`trap 'rm -rf "$verify_dir"' EXIT`,
		`gh release view "$VERSION" --json assets --jq '.assets[].name'`,
		"required_deployment_assets=(compose.yaml compose.proxy-network.yaml compose.proxy-host.yaml compose.env.example)",
		"for forbidden_asset in .env.example default.env.example; do",
	} {
		if !strings.Contains(verificationStep, required) {
			t.Fatalf("publish workflow must verify public deployment release assets through %q", required)
		}
	}
	if count := strings.Count(verificationStep, "required_deployment_assets=("); count != 1 {
		t.Fatalf("public deployment verification must use one required_deployment_assets array, got %d", count)
	}
	assetLoopOffset := strings.Index(verificationStep, `for asset in "${required_deployment_assets[@]}"; do`)
	forbiddenLoopOffset := strings.Index(verificationStep, "for forbidden_asset in .env.example default.env.example; do")
	if assetLoopOffset < 0 || forbiddenLoopOffset <= assetLoopOffset {
		t.Fatal("public deployment verification must use the required_deployment_assets array before checking legacy names")
	}
	assetLoop := verificationStep[assetLoopOffset:forbiddenLoopOffset]
	assetLoopRequirements := []string{
		`if [[ "$release_asset" == "$asset" ]]; then`,
		`if [[ "$matches" -ne 1 ]]; then`,
		`gh release download "$VERSION" --pattern "$asset" --dir "$verify_dir"`,
		`test -f "$verify_dir/$asset"`,
		`cmp -s "dist/$asset" "$verify_dir/$asset"`,
	}
	lastAssetLoopOffset := -1
	for _, required := range assetLoopRequirements {
		offset := strings.Index(assetLoop, required)
		if offset < 0 {
			t.Fatalf("required_deployment_assets loop must verify every public asset through %q", required)
		}
		if offset <= lastAssetLoopOffset {
			t.Fatalf("required_deployment_assets loop must check cardinality before download/existence/byte identity; %q is out of order", required)
		}
		lastAssetLoopOffset = offset
	}
	for _, forbidden := range []string{
		`"${#release_asset_names[@]}" -ne 2`,
		`"${#release_asset_names[@]}" != 2`,
		`"${#release_asset_names[@]}" -ne 4`,
		`"${#release_asset_names[@]}" != 4`,
	} {
		if strings.Contains(verificationStep, forbidden) {
			t.Fatalf("public deployment verification must preserve unrelated release assets; found total-asset assumption %q", forbidden)
		}
	}
}

func TestProductionComposeTrellisSpecsMatchReleaseAndAuthorityContract(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	deploymentSpec := readText(t, filepath.Join(root, ".trellis", "spec", "backend", "directory-structure.md"))
	databaseSpec := readText(t, filepath.Join(root, ".trellis", "spec", "backend", "database-guidelines.md"))
	composeScenario := markdownSection(
		t,
		deploymentSpec,
		"#### Scenario: release-asset production Compose and image contract",
		"### `internal/contracts/agentapi/`",
	)
	for _, required := range []string{
		"1. **Scope / Trigger**",
		"2. **Signatures**",
		"3. **Contracts**",
		"4. **Validation & Error Matrix**",
		"5. **Good / Base / Bad Cases**",
		"6. **Tests Required**",
		"7. **Wrong vs Correct**",
		"`compose.yaml` → `compose.proxy-network.yaml` → `compose.proxy-host.yaml` → `compose.env.example`",
		"COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml",
		"COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml",
		"Mode selection is common base + exactly one selected overlay.",
		"loading both overlays is unsupported",
		"existing NPM user-defined network",
		"NPM remains unchanged",
		"upstream `houfeng:16001`",
		"base and shared-network mode publish no host port",
		"`target: 16001`",
		"`published: \"16001\"`",
		"`host_ip: 127.0.0.1`",
		"`protocol: tcp`",
		"The effective behavior is `127.0.0.1:16001 -> 16001/tcp`",
		"renderer-specific short form",
		"exactly one fixed IPv4-loopback mapping",
		"Docker Engine `28.0.0+`",
		"Do not set `HOUFENG_PROXY_NETWORK=host` or invent a placeholder network.",
		"all-interface/LAN/IPv6 mapping",
		"must not duplicate the eight-service graph",
		"renders both modes before upload",
		"`concurrency.group: deployment-assets-${{ needs.resolve.outputs.version }}`",
		"`cancel-in-progress: false`",
		"serializes deployment-asset mutation for one resolved version",
		"does not cancel the running job",
		"queued pending runs may be replaced",
		"non-destructive fail-closed idempotent upload",
		"exact-name cardinality",
		"byte identity",
		"unrelated agent assets",
		"do not assert total asset count equals four",
		"`docker compose up -d` owns ordinary initialization.",
		"Center waits for healthy authority",
		"processor waits for db-init and ClamAV",
		"houfeng-secrets-init",
		"houfeng-record-authority",
		"secret stager bind-mounts only `./data/secrets`",
		"Center/processor never receive bootstrap, migrator, platform-admin, or authority credentials",
		"Processor stays non-root, read-only, `cap_drop: ALL`, `no-new-privileges`, core=0",
		"./data/records-authority",
		"PostgreSQL, local attachments, and Records authority state are one coordinated restore unit",
	} {
		if !strings.Contains(composeScenario, required) {
			t.Fatalf("deployment Trellis Compose scenario must contain %q", required)
		}
	}
	rotationSteps := []string{
		"`docker compose stop houfeng houfeng-content-processor`",
		"`docker compose run --rm houfeng-secrets-init`",
		"`docker compose run --rm houfeng-db-init`",
		"`docker compose up -d --force-recreate houfeng houfeng-content-processor`",
	}
	lastRotationStep := -1
	for _, step := range rotationSteps {
		offset := strings.Index(composeScenario, step)
		if offset < 0 {
			t.Fatalf("deployment Trellis Compose scenario must preserve password rotation step %q", step)
		}
		if offset <= lastRotationStep {
			t.Fatalf("deployment Trellis Compose scenario password rotation steps are out of order at %q", step)
		}
		lastRotationStep = offset
	}
	for _, forbidden := range []string{
		"scripts/compose-up.sh",
		"Compose is a development/conformance topology",
		"houfeng_blobs:/var/lib/houfeng/attachments",
		`127.0.0.1:${HOUFENG_HOST_PORT:-16001}:16001`,
		"`127.0.0.1:16001:16001/tcp`",
		"one deployment-assets job waits",
		"every later job waits",
		"all later jobs wait",
		"cancellation is globally disabled",
		"cancellation and cross-run mutation for that version are disabled",
		"`compose.yaml` 与 `compose.env.example`（后者保存为本地 `.env`）",
		"uploads `compose.yaml` plus `compose.env.example`",
		"never add a public fallback port",
		`name: "${HOUFENG_PROXY_NETWORK:?set HOUFENG_PROXY_NETWORK in .env}"`,
	} {
		if strings.Contains(composeScenario, forbidden) {
			t.Fatalf("deployment Trellis Compose scenario retains obsolete single-mode contract %q", forbidden)
		}
	}
	for _, required := range []string{
		"single-host Compose Records authority",
		"LoadComposeState",
		"ContractActivationProjectionCommandV1",
		"record_platform_compose_membership_heartbeat",
		"restore PostgreSQL and authority state together",
	} {
		if !strings.Contains(databaseSpec, required) {
			t.Fatalf("database Trellis spec must contain authority contract %q", required)
		}
	}
}
