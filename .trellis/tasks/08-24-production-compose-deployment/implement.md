# Production Docker Compose deployment implementation plan

> Follow TDD. Keep all implementation on `codex/production-compose-deployment` in the dedicated worktree. Do not stage, commit, push, or change another task/worktree.

## Phase 0: Start gate and baseline

- [x] Validate task context and start the Trellis task after approved design is recorded.
- [x] Confirm hooks, branch, Node 22 (if Web tooling is touched), Docker/Compose, and current dirty scope.
- [x] Run focused deployment/admin tests and current Compose validation as baseline.

## Phase 1: RED — public deployment contract

**Files:** `internal/center/deploy/*_test.go`, new focused deployment tests, release-workflow tests

- [x] Replace old launcher/named-volume expectations with failing tests for prebuilt-only images, full service graph, one-shot dependencies, NPM external network, no public port, `./data` bindings, secret scope, and Records default true.
- [x] Add failing tests for the three env-template categories, required placeholders, no manual SQL/source-build quick start, and exact download/edit/config/up ordering.
- [x] Add failing release tests for version-matched Compose/env assets.

## Phase 2: RED/GREEN — deploy initializer

**Files:** `cmd/houfeng-record-platform-admin`, `internal/center/platformmigrate`, `internal/center/store/migrate`, Dockerfile

- [x] Add deterministic unit REDs for deploy-init argument/config validation, safe secret loading, role set validation, pre-R1 provisioning, failure redaction, and no forbidden input lookup.
- [x] Add strict PostgreSQL REDs for fresh role provisioning/current convergence/runtime admission, exact repeat, password rotation, membership drift, unsupported PostgreSQL/bootstrap drift, and partial-failure rollback.
- [x] Implement the minimal idempotent Compose provisioning route by reusing `ConvergeAppACLCurrent` and `AdmitAppACLCurrentRuntime`; do not duplicate the APP migration engine.
- [x] Build `houfeng-record-platform-admin` into the release image and verify the non-root runtime still owns normal application processes.

## Phase 3: GREEN — Compose, env, storage, and NPM

**Files:** `compose.yaml`, `docs/deploy/compose.env.example`, deployment tests

- [x] Add service-scoped secrets sourced from `.env` and explicit per-service ordinary environment mappings.
- [x] Add storage-init and database-init one-shot services with `service_completed_successfully` dependencies.
- [x] Bind PostgreSQL, attachments, logs, and ClamAV cache below `./data`; keep processor read-only/capless with bounded tmpfs.
- [x] Join only Center to the configured external NPM network at stable alias `houfeng`; remove the default public host port.
- [x] Default Records true and keep Center/processor scanner/blob settings aligned.
- [x] Make `docker compose config` fail on unchanged mandatory placeholders and pass with a test-owned valid env.

## Phase 4: RED/GREEN — single-host Records authority proof

**Files:** `internal/center/recordplatform`, new deployment-authority package/CLI, focused tests

- [x] Add deterministic REDs for atomic first-run state, stable deployment ID/key/credential, canonical signed bounded ledger, complete verification, hostile/corrupt/truncated state, exact repeat, and safe diagnostics.
- [x] Implement a closed v1 Compose activation bundle whose verifier derives the existing `ContractActivationProjectionCommandV1`; reuse the existing projector codec and never accept caller-supplied witness digests.
- [x] Add Center deployment-ID file loading with explicit precedence and REDs for missing, malformed, oversized, and control-character content.

## Phase 5: RED/GREEN — authority role and heartbeat

**Files:** new `0060_*` migration, matching current APP ACL fragment/compiler contract, authority runtime, PostgreSQL tests

- [x] Add migration/ACL REDs for the constrained direct authority role and its single closed membership-heartbeat function.
- [x] Prove runtime/admin/PUBLIC cannot execute it; authority cannot direct-DML Records tables, execute activation projectors, or inherit bootstrap/migrator/platform-admin/runtime privileges.
- [x] Add strict PostgreSQL REDs for exact active deployment membership, bounded TTL, stable epoch/fence, stale/foreign/mismatched rejection, restart renewal, and zero mutation on rejection.
- [x] Implement the long-running authority with fixed bounded refresh/TTL, health only after verified active contract + fresh membership, safe shutdown, and no destructive membership delete.

## Phase 6: GREEN — activation, Compose wiring, and recovery

**Files:** deploy initializer/admin CLI, Dockerfile/entrypoint, `compose.yaml`, env/deployment tests

- [x] Extend db-init to generate state only for a truly inactive fresh database, activate from valid state through the existing projector, verify/persist CAS receipt, and fail closed for active DB + absent/corrupt/mismatched state.
- [x] Add the authority service, public/private mounts, storage-init ownership, Center fixed internal identity, dependency on authority health, and least-privilege secret scopes.
- [x] Keep authority internals out of the three operator env sections; no new required user variable.
- [x] Add restart/upgrade/restore tests proving PostgreSQL and authority state are one coordinated unit.

## Phase 7: GREEN — release assets and documentation

**Files:** `.github/workflows/publish-images.yml`, `README.md`, `docs/deploy/local-and-systemd.md`, docs index/operations docs as needed

- [x] Generate/upload release-tag-pinned `compose.yaml` and `.env.example` assets and verify image/tag agreement.
- [x] Rewrite README quick start around download → edit → validate → up, then exact NPM setup.
- [x] Rewrite the canonical Compose guide for full production topology, categorized variables, automatic DB initialization, upgrade/backup/restore/host migration, logs/health, and rollback limits.
- [x] Remove stale claims that Compose is only a development/conformance topology or requires `scripts/compose-up.sh`/manual SQL/password files.
- [x] Preserve advanced direct/systemd instructions and clearly separate them from the ordinary Docker path.

## Phase 8: Contract/spec sync

**Files:** `.trellis/spec/backend/directory-structure.md` and any directly affected deployment contract

- [x] Replace the old wrapper-script/named-volume contract with the approved release-asset, one-shot initialization, single-host authority, external NPM network, service-scoped secret, and portable-data contract.
- [x] Record the signed local ledger, projector derivation, narrow heartbeat role, exact membership, coordinated restore, and fail-closed recovery contracts in database/Records specs.
- [x] Keep mandatory seven-section Trellis scenario structure and executable test requirements.

## Phase 9: Verification

- [x] Focused deployment/admin tests GREEN under Go 1.26.2.
- [x] Strict PostgreSQL 16 initialization/activation/heartbeat/privilege tests GREEN with zero skips.
- [x] `docker compose config` passes with test config and fails for each required placeholder.
- [x] Build/inspect the project image; confirm all three binaries, Web UI, Poppler, UID/GID 10001, and entrypoints.
- [x] Real isolated Compose smoke: fresh start, authority health, actual Records write, attachment process/ClamAV, restart heartbeat/exact repeat, no manual SQL, corrupt-state fail-closed, and clean teardown.
- [x] Portable copy smoke or equivalent evidence proves the visible deployment directory is the migration unit.
- [x] `make verify-go`, applicable workflow/static checks, `actionlint` if installed, shell syntax, and `git diff --check` pass.
- [x] Independent `trellis-check` reports no unresolved Critical/Important/Minor findings.

## Implementation evidence (2026-08-24)

- Focused Go packages pass under Go 1.26.2: `go test ./cmd/houfeng-record-platform-admin ./internal/center/deploy ./internal/center/platformmigrate ./internal/center/recordauthority ./internal/center/config ./internal/center/recordsearch ./internal/center/store/migrate -count=1`.
- Strict isolated PostgreSQL 16 passes with zero skips for `TestPostgresIntegrationComposeInitialize`, `TestPostgresIntegrationComposeBootstrapRollback`, and sparse committed Record search projection.
- Docker Compose 5.5.0 accepts the valid release configuration, rejects each of the ten required blank values, and renders no secret value. Environment-backed secret content does not rebuild running application containers, so the documented controlled rotation explicitly reruns both initializers and force-recreates Center/processor.
- The built task image contains Center, processor, admin, Web assets, and Poppler, with UID/GID 10001 and the expected Center entrypoint. The local release-asset render resolves every project service to one matching version-pinned image; `actionlint` was not installed.
- A unique isolated real stack completed fresh automatic provisioning, signed activation, authority health, actual admission-gated Record publish, upload quarantine, ClamAV processing, available attachment download, exact repeat, authority restart, SCRAM password rotation, and safe stage-visible/no-secret diagnostics.
- A stopped full-directory copy started under a second unique project/network and returned the existing Record and byte-identical attachment. Removing its authority ledger made db-init exit 1 while authority, Center, and processor remained unstarted. The original task stack was restored healthy, then all task containers/networks, the disposable smoke `./data`, portable copy, task image, and task-only temporary state were removed.
- `GOFLAGS='-p=2' make verify-go`, release/static checks, shell syntax, and final diff hygiene pass. Low parallelism avoids the host's transient test-artifact quota without changing test coverage.

## Stop conditions

- Implementation would require Center/processor to receive bootstrap or migrator credentials.
- The authority implementation would bypass the existing projector, derive trust from PostgreSQL alone, or relax the existing admission gate.
- Active database state would be overwritten or silently paired with newly generated authority state.
- Compose would silently start application services after init failure.
- Achieving portability would require destructive mutation of existing user data or broad host cleanup.
- A real gate would collide with non-task Docker resources; use a unique project/network/data root or stop.
