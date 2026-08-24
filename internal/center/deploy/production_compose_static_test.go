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
	for _, service := range []string{
		"houfeng-storage-init",
		"houfeng-db-init",
		"houfeng-record-authority",
		"houfeng",
		"houfeng-content-processor",
		"clamav",
		"db",
	} {
		composeServiceBlock(t, compose, service)
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

func TestProductionComposeUsesPortableDataAndNPMNetwork(t *testing.T) {
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
			"aliases:",
			"- houfeng",
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
				t.Fatalf("%s must contain portable/NPM contract %q:\n%s", service, required, block)
			}
		}
	}
	if strings.Contains(compose, "\n    ports:") {
		t.Fatal("production compose must not publish a host port by default")
	}
	for _, service := range []string{"houfeng-storage-init", "houfeng-db-init", "houfeng-record-authority", "houfeng-content-processor", "clamav", "db"} {
		if strings.Contains(composeServiceBlock(t, compose, service), "houfeng-proxy") {
			t.Fatalf("only Center may join the external NPM network; %s also joins it", service)
		}
	}
	for _, required := range []string{
		"\nnetworks:\n",
		"  houfeng-proxy:\n",
		"    external: true",
		`    name: "${HOUFENG_PROXY_NETWORK:?set HOUFENG_PROXY_NETWORK in .env}"`,
	} {
		if !strings.Contains(compose, required) {
			t.Fatalf("production compose must define the external NPM network through %q", required)
		}
	}
	if strings.Contains(compose, "\nvolumes:\n") {
		t.Fatal("production business data must use visible ./data bind mounts, not top-level named volumes")
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
	for _, required := range []string{
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
		if strings.Contains(document, "github.com/linnea7171/houfeng/") {
			t.Fatalf("%s must download release assets from the canonical xiangnan0811/houfeng repository", name)
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

func TestPublishWorkflowUploadsVersionMatchedDeploymentAssets(t *testing.T) {
	t.Parallel()

	workflow := readText(t, filepath.Join(repoRoot(t), ".github", "workflows", "publish-images.yml"))
	for _, required := range []string{
		"\n  deployment-assets:\n",
		"compose.yaml",
		"compose.env.example",
		"docker.io/linnea7171/houfeng:v${{ needs.resolve.outputs.version }}",
		"docker compose --env-file dist/compose.env.example -f dist/compose.yaml config --images",
		"gh release upload",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("publish workflow must contain deployment asset contract %q", required)
		}
	}
}

func TestPublishWorkflowUsesStablePublicComposeEnvironmentAssetName(t *testing.T) {
	t.Parallel()

	workflow := readText(t, filepath.Join(repoRoot(t), ".github", "workflows", "publish-images.yml"))
	for _, required := range []string{
		"cp docs/deploy/compose.env.example dist/compose.env.example",
		"docker compose --env-file dist/compose.env.example -f dist/compose.yaml config --images",
		"dist/compose.env.example \\",
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
		"required_deployment_assets=(compose.yaml compose.env.example)",
		`if [[ "$release_asset" == "$asset" ]]; then`,
		`if [[ "$matches" -ne 1 ]]; then`,
		"for forbidden_asset in .env.example default.env.example; do",
		`gh release download "$VERSION" --pattern 'compose.yaml' --dir "$verify_dir"`,
		`gh release download "$VERSION" --pattern 'compose.env.example' --dir "$verify_dir"`,
		`test -f "$verify_dir/compose.yaml"`,
		`test -f "$verify_dir/compose.env.example"`,
		`cmp -s dist/compose.yaml "$verify_dir/compose.yaml"`,
		`cmp -s dist/compose.env.example "$verify_dir/compose.env.example"`,
	} {
		if !strings.Contains(verificationStep, required) {
			t.Fatalf("publish workflow must verify public deployment release assets through %q", required)
		}
	}
	for _, forbidden := range []string{
		`"${#release_asset_names[@]}" -ne 2`,
		`"${#release_asset_names[@]}" != 2`,
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
	for _, required := range []string{
		"release-asset production Compose",
		"houfeng-secrets-init",
		"houfeng-record-authority",
		"HOUFENG_PROXY_NETWORK",
		"./data/records-authority",
		"no public host port",
		"post-upload public readback",
	} {
		if !strings.Contains(deploymentSpec, required) {
			t.Fatalf("deployment Trellis spec must contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"scripts/compose-up.sh",
		"Compose is a development/conformance topology",
		"houfeng_blobs:/var/lib/houfeng/attachments",
		`127.0.0.1:${HOUFENG_HOST_PORT:-16001}:16001`,
	} {
		if strings.Contains(deploymentSpec, forbidden) {
			t.Fatalf("deployment Trellis spec retains obsolete production contract %q", forbidden)
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
