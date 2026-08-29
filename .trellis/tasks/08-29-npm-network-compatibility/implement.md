# NPM Network Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add explicit shared-network and host-proxy production Compose modes so Houfeng adapts to an existing NPM deployment without weakening the default no-host-port boundary.

**Architecture:** Keep the complete eight-service topology in `compose.yaml`, then merge exactly one thin proxy-mode file selected by `COMPOSE_FILE` in `.env`. The shared-network file alone owns the required external NPM network; the host-proxy file alone owns a Docker Engine 28+ IPv4-loopback port mapping.

**Tech Stack:** Docker Engine 28+, Docker Compose, YAML, Go static contract tests, GitHub Actions, Markdown operator documentation, Trellis specs.

---

## File map

**Create**

- `compose.proxy-network.yaml` — shared external-network attachment and stable `houfeng` alias.
- `compose.proxy-host.yaml` — host-mode NPM loopback-only Center port publication.

**Modify**

- `compose.yaml` — common topology only; Center retains explicit default network.
- `docs/deploy/compose.env.example` — default mode selection and conditional NPM network guidance.
- `README.md` — four-asset quick start and concise two-mode routing.
- `docs/deploy/local-and-systemd.md` — canonical mode selection, validation, upgrade, recovery, and troubleshooting.
- `internal/center/deploy/production_compose_static_test.go` — static topology/docs/release contracts.
- `.github/workflows/publish-images.yml` — stage, validate, upload, publicly read back, and report four deployment assets and two rendered modes.
- `.trellis/spec/backend/directory-structure.md` — executable deployment contract.

**Task artifacts**

- `.trellis/tasks/08-29-npm-network-compatibility/{prd.md,design.md,implement.md,implement.jsonl,check.jsonl,research/*}`.

## Task 1: RED — freeze the two-mode Compose contract

**Files**

- Modify: `internal/center/deploy/production_compose_static_test.go`
- Test: `internal/center/deploy/production_compose_static_test.go`

- [ ] **Step 1: Replace the single-network topology assertion with base + mode assertions**

Add a focused test shaped as follows, reusing `readText`, `repoRoot`, and `composeServiceBlock`:

```go
func TestProductionComposeDefinesExplicitProxyModes(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	base := readText(t, filepath.Join(root, "compose.yaml"))
	networkMode := readText(t, filepath.Join(root, "compose.proxy-network.yaml"))
	hostMode := readText(t, filepath.Join(root, "compose.proxy-host.yaml"))

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
```

Keep the existing portable bind-mount assertions, but move proxy assertions out of `TestProductionComposeUsesPortableDataAndNPMNetwork` and rename it to describe portable data only.

- [ ] **Step 2: Run the focused RED**

```bash
go test ./internal/center/deploy -run 'TestProductionComposeDefinesExplicitProxyModes|TestProductionComposeUsesPortableData' -count=1 -v
```

Expected: FAIL because both mode files are absent and the base still owns `HOUFENG_PROXY_NETWORK`.

## Task 2: GREEN — split the Compose topology

**Files**

- Modify: `compose.yaml`
- Create: `compose.proxy-network.yaml`
- Create: `compose.proxy-host.yaml`
- Test: `internal/center/deploy/production_compose_static_test.go`

- [ ] **Step 1: Make the base Center private-network-only**

Replace the Center network block with:

```yaml
    networks:
      default:
```

Delete the base top-level `houfeng-proxy` network declaration. Do not change another service, dependency, environment value, secret, mount, healthcheck, or default network.

- [ ] **Step 2: Add the exact shared-network mode file**

```yaml
services:
  houfeng:
    networks:
      houfeng-proxy:
        aliases:
          - houfeng

networks:
  houfeng-proxy:
    external: true
    name: "${HOUFENG_PROXY_NETWORK:?set HOUFENG_PROXY_NETWORK for shared-network mode}"
```

- [ ] **Step 3: Add the exact host-proxy mode file**

```yaml
services:
  houfeng:
    ports:
      - name: npm-host-proxy
        target: 16001
        published: "16001"
        host_ip: 127.0.0.1
        protocol: tcp
```

- [ ] **Step 4: Run the focused GREEN**

```bash
go test ./internal/center/deploy -run 'TestProductionComposeDefinesExplicitProxyModes|TestProductionComposeUsesPortableData' -count=1 -v
```

Expected: PASS.

- [ ] **Step 5: Render both modes with complete non-secret test values**

Shared-network render:

```bash
env \
  HOUFENG_IMAGE=docker.io/linnea7171/houfeng:v0.0.0-test \
  HOUFENG_PROXY_NETWORK=houfeng-test-proxy \
  HOUFENG_PUBLIC_BASE_URL=https://center.example.invalid \
  HOUFENG_INITIAL_USERNAME=compose-test \
  POSTGRES_BOOTSTRAP_PASSWORD=compose-test-bootstrap \
  HOUFENG_RUNTIME_PASSWORD=compose-test-runtime \
  HOUFENG_MIGRATOR_PASSWORD=compose-test-migrator \
  HOUFENG_PLATFORM_ADMIN_PASSWORD=compose-test-admin \
  HOUFENG_INITIAL_PASSWORD=compose-test-initial \
  HOUFENG_SESSION_HMAC_KEY=compose-test-session-hmac-key-32-bytes \
  docker compose --env-file docs/deploy/compose.env.example \
    -f compose.yaml -f compose.proxy-network.yaml config
```

Expected: exit 0; Center contains default + `houfeng-proxy`, no published port.

Host-proxy render:

```bash
env -u HOUFENG_PROXY_NETWORK \
  HOUFENG_IMAGE=docker.io/linnea7171/houfeng:v0.0.0-test \
  HOUFENG_PUBLIC_BASE_URL=https://center.example.invalid \
  HOUFENG_INITIAL_USERNAME=compose-test \
  POSTGRES_BOOTSTRAP_PASSWORD=compose-test-bootstrap \
  HOUFENG_RUNTIME_PASSWORD=compose-test-runtime \
  HOUFENG_MIGRATOR_PASSWORD=compose-test-migrator \
  HOUFENG_PLATFORM_ADMIN_PASSWORD=compose-test-admin \
  HOUFENG_INITIAL_PASSWORD=compose-test-initial \
  HOUFENG_SESSION_HMAC_KEY=compose-test-session-hmac-key-32-bytes \
  docker compose --env-file docs/deploy/compose.env.example \
    -f compose.yaml -f compose.proxy-host.yaml config
```

Expected: exit 0; Center retains default network and renders only `127.0.0.1:16001:16001/tcp`; no external network exists.

## Task 3: RED/GREEN — env template and operator documentation

**Files**

- Modify: `internal/center/deploy/production_compose_static_test.go`
- Modify: `docs/deploy/compose.env.example`
- Modify: `README.md`
- Modify: `docs/deploy/local-and-systemd.md`

- [ ] **Step 1: Add failing env/docs contract assertions**

Update `TestProductionEnvironmentTemplateHasThreeOperatorSections` and `TestProductionQuickStartUsesReleaseAssetsAndAutomaticInitialization` to require:

```go
required := []string{
	"COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml",
	"HOUFENG_PROXY_NETWORK=",
	"compose.proxy-network.yaml",
	"compose.proxy-host.yaml",
	"docker compose config",
	"docker compose up -d",
	"houfeng:16001",
	"127.0.0.1:16001",
	"Docker Engine 28.0.0",
}
```

Add negative checks so docs do not recommend `HOUFENG_PROXY_NETWORK=host`, a placeholder proxy network for host mode, or changing NPM from host to bridge merely for Houfeng.

- [ ] **Step 2: Run the docs RED**

```bash
go test ./internal/center/deploy -run 'TestProductionEnvironmentTemplateHasThreeOperatorSections|TestProductionQuickStartUsesReleaseAssetsAndAutomaticInitialization' -count=1 -v
```

Expected: FAIL on missing mode assets, `COMPOSE_FILE`, host upstream, and Engine requirement.

- [ ] **Step 3: Update the env template**

Keep exactly Must change / Recommended / Optional headings. Add the default mode selection before mode-specific variables:

```dotenv
# Select exactly one reviewed proxy mode. The default joins an existing
# bridge-network NPM. For network_mode: host NPM, change only this line to:
# COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml
COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml

# Required only by the default shared-network mode. Set the exact existing
# Docker network already joined by NPM. Leave blank in host-proxy mode.
HOUFENG_PROXY_NETWORK=
```

Do not add a configurable host bind address or public port variable.

- [ ] **Step 4: Rewrite the quick start around four assets**

README and the canonical guide must download, in order:

```bash
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.yaml -o compose.yaml
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.proxy-network.yaml -o compose.proxy-network.yaml
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.proxy-host.yaml -o compose.proxy-host.yaml
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.env.example -o .env
```

Document shared-network discovery without editing NPM:

```bash
docker inspect "$(docker compose ps -q app)" \
  --format '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{"\n"}}{{end}}'
```

Document host-mode preflight and upstream:

```bash
docker version --format '{{.Server.Version}}'
```

The guide must say Engine 28.0.0+ is required, set `COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml`, leave `HOUFENG_PROXY_NETWORK` blank, and configure NPM to `127.0.0.1:16001`.

Update backup/restore and upgrade text so the deployment unit includes the common and mode assets. Preserve public HTTPS, NPM security toggles, secret rotation, Records authority, and coordinated recovery text.

- [ ] **Step 5: Run docs GREEN**

```bash
go test ./internal/center/deploy -run 'TestProductionEnvironmentTemplateHasThreeOperatorSections|TestProductionQuickStartUsesReleaseAssetsAndAutomaticInitialization|TestProductionGuideDocumentsAuthorityAndPortableRecoveryUnit' -count=1 -v
```

Expected: PASS.


## Task 4: RED/GREEN — release all four assets and validate both modes

**Files**

- Modify: `internal/center/deploy/production_compose_static_test.go`
- Modify: `.github/workflows/publish-images.yml`

- [ ] **Step 1: Extend release static tests first**

Change required asset assertions to the exact ordered set:

```go
deploymentAssets := []string{
	"compose.yaml",
	"compose.proxy-network.yaml",
	"compose.proxy-host.yaml",
	"compose.env.example",
}
```

Require workflow evidence for:

- copying both mode files into `dist/`;
- rendering `dist/compose.yaml + dist/compose.proxy-network.yaml` with a nonblank `HOUFENG_PROXY_NETWORK`;
- rendering `dist/compose.yaml + dist/compose.proxy-host.yaml` with `HOUFENG_PROXY_NETWORK` unset;
- uploading all four exact names;
- requiring exactly one of each name in public Release assets;
- downloading and byte-comparing all four names;
- preserving unrelated agent release assets.

- [ ] **Step 2: Run the release RED**

```bash
go test ./internal/center/deploy -run 'TestPublishWorkflowUploadsVersionMatchedDeploymentAssets|TestPublishWorkflowUsesStablePublicComposeEnvironmentAssetName|TestPublishWorkflowVerifiesPublicDeploymentAssetsAfterUpload' -count=1 -v
```

Expected: FAIL because the workflow still stages, validates, uploads, and reads back only two deployment assets.

- [ ] **Step 3: Implement dual-mode pre-upload validation**

In the deployment-assets job:

1. copy both mode files into `dist/`;
2. pin `HOUFENG_IMAGE` in the env asset;
3. render network and host modes with explicit `-f` pairs;
4. collect project images from both render results and require the single exact release image;
5. inspect the release image manifest only after both renders pass.

Do not depend on `COMPOSE_FILE` for CI rendering; explicit `-f` pairs make the two validation targets auditable.

- [ ] **Step 4: Extend upload and public readback**

Upload all four `dist/` files. Use one `required_deployment_assets` array for exact-name cardinality, downloads, existence checks, and `cmp -s` comparisons. Keep the existing checks rejecting `.env.example` and `default.env.example`, and do not require the Release's total asset count to equal four.

- [ ] **Step 5: Run release GREEN and syntax checks**

```bash
go test ./internal/center/deploy -run 'TestPublishWorkflowUploadsVersionMatchedDeploymentAssets|TestPublishWorkflowUsesStablePublicComposeEnvironmentAssetName|TestPublishWorkflowVerifiesPublicDeploymentAssetsAfterUpload' -count=1 -v
ruby -e 'require "yaml"; YAML.load_file(".github/workflows/publish-images.yml")'
```

Extract any changed multiline shell bodies with the existing test/helper approach and run `bash -n` on them. Expected: all pass.

## Task 5: Synchronize the executable Trellis deployment spec

**Files**

- Modify: `.trellis/spec/backend/directory-structure.md`
- Modify: `internal/center/deploy/production_compose_static_test.go`

- [ ] **Step 1: Update the release-asset production Compose scenario**

Change the scenario's scope, signatures, contracts, error matrix, good/base/bad cases, required tests, and correct YAML examples to encode:

- four deployment assets;
- common base + exactly one selected mode;
- shared-network mode as default;
- Houfeng joining the existing NPM network;
- host-proxy loopback mapping with Engine 28+;
- no `HOUFENG_PROXY_NETWORK=host`, placeholder network, all-interface port, or duplicated full Compose;
- both mode renders and public byte-identity verification.

Remove the obsolete blanket assertion that every loopback host port is forbidden; replace it with the conditional rule that the base and shared-network mode have no published port while host mode has exactly one loopback mapping.

- [ ] **Step 2: Update the spec sync test and run it**

```bash
go test ./internal/center/deploy -run 'TestProductionComposeTrellisSpecsMatchReleaseAndAuthorityContract' -count=1 -v
```

Expected: PASS with the new four-asset/two-mode/Engine-28 contract.

## Task 6: Full verification and independent review

**Files**

- Check all task-owned files.

- [ ] **Step 1: Run focused and package tests**

```bash
go test ./internal/center/deploy -count=1
go test ./cmd/houfeng-record-platform-admin -count=1
```

Expected: PASS with zero skips attributable to this task.

- [ ] **Step 2: Re-run both final Compose renders**

Run the exact shared-network and host-proxy commands from Task 2 against the final env template. Save no rendered secrets or configs in tracked files. Expected: both exit 0 with the mode-specific topology.

- [ ] **Step 3: Run repository quality gates**

```bash
make verify-go
git diff --check
git status --short
```

Run `actionlint` if installed; otherwise record that it is unavailable after YAML parse and shell syntax checks. No Web gate is required unless implementation unexpectedly changes `web/`.

- [ ] **Step 4: Dispatch Trellis check**

Provide the active task path first and require findings-first review of:

- mode isolation and Compose merge semantics;
- Engine 28 security boundary;
- existing-NPM responsibility direction;
- release asset cardinality/readback;
- docs/upgrade/recovery consistency;
- no regression to service, secret, authority, or data topology.

Fix all Critical/Important findings, rerun affected RED/GREEN and full gates, and obtain Critical 0 / Important 0.

## Task 7: Commit, PR, CI, merge, and release evidence

**Files**

- Stage only task-owned source, tests, docs, workflow, spec, and Trellis task artifacts.

- [ ] **Step 1: Review exact diff and commit on the feature branch**

```bash
git status --short
git diff --stat
git diff --check
git add compose.yaml compose.proxy-network.yaml compose.proxy-host.yaml \
  docs/deploy/compose.env.example README.md docs/deploy/local-and-systemd.md \
  internal/center/deploy/production_compose_static_test.go \
  .github/workflows/publish-images.yml \
  .trellis/spec/backend/directory-structure.md \
  .trellis/tasks/08-29-npm-network-compatibility
git diff --cached --check
git commit -m "feat: support existing NPM proxy modes"
```

Expected: commit succeeds on `codex/npm-network-compatibility`; no unrelated or user-owned files are staged.

- [ ] **Step 2: Push and open a PR**

```bash
git push -u origin codex/npm-network-compatibility
gh pr create --base main --head codex/npm-network-compatibility \
  --title "feat: support existing NPM proxy modes" \
  --body-file .trellis/tasks/08-29-npm-network-compatibility/pr-body.md
```

The PR body must summarize the two modes, Engine 28 boundary, RED/GREEN commands, Compose renders, and rollback.

- [ ] **Step 3: Monitor required CI and merge only when green**

```bash
gh pr checks --watch
```

Resolve failures on the same branch, rerun proportional local gates, push fixes, and re-watch. Merge through the protected PR path only after required checks pass.

- [ ] **Step 4: Verify post-merge and release assets**

Monitor main CI, Release Please, release publishing, and deployment-assets jobs. For the resulting release, download all four deployment assets into a fresh temporary directory, compare them with the tagged sources, render both modes with the matching release image, and verify the public asset list contains exactly one of each required deployment name while preserving unrelated agent assets.

- [ ] **Step 5: Finish Trellis task**

Record commit/PR/CI/merge/release evidence in task artifacts and the developer journal, run the final task validation, then archive only after all acceptance criteria and release evidence are satisfied.
