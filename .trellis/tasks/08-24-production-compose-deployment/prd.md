# Production Docker Compose deployment bundle

## Goal

Deliver Houfeng as a downloadable, prebuilt-image-only production Docker Compose bundle for a single Docker host behind Nginx Proxy Manager. An operator downloads `compose.yaml` and an environment template, edits the documented configuration, validates it, and starts the complete stack with `docker compose up -d`; no source checkout, local image build, manual SQL, or migration launcher is part of the operator path.

## Requirements

### Complete runtime

- The default stack includes Center with baked Web UI, the Records attachment content processor, ClamAV, PostgreSQL, bounded one-shot initialization services, and a long-running single-host Records authority needed to make Records writes genuinely available.
- `HOUFENG_RECORDS_ENABLED=true` is the production-template default approved by the user. Center and processor share the same attachment store and scanner contract.
- The authority automatically activates the local deployment contract and continuously renews the exact known runtime membership. An ordinary operator does not run SQL, projection commands, or heartbeat commands.
- Agents remain host-installed Linux/systemd workloads and are not added to Compose.

### Distribution and startup

- Every Houfeng service uses a published `linnea7171/houfeng:vX.Y.Z` image; public Compose contains no `build:` path.
- GitHub Releases publish version-matched `compose.yaml` and environment-template assets so users can download them with stable commands.
- A fresh deployment requires no repository clone, helper launcher, `psql`, SQL file, Go/Node toolchain, or manual database initialization.
- `docker compose up -d` starts PostgreSQL, performs idempotent privileged pre-provisioning, creates or tightens the distinct runtime/admin/migrator roles, converges the current Records APP schema/ACL through the existing authoritative migrator, and starts Center/processor only after initialization succeeds.
- Automatic initialization creates or validates a persistent signed local authority state, derives the existing contract activation projection from that state, verifies the projector receipt, and starts the authority heartbeat before Center becomes healthy.
- A repeat `docker compose up -d` is safe. A new pinned Houfeng image reruns the one-shot initializer before the new application containers become ready.

### Nginx Proxy Manager

- Compose does not run Caddy or another edge proxy and does not publish Houfeng publicly by default.
- Center joins a user-selected existing external Docker network shared with Nginx Proxy Manager. Documentation gives the exact NPM forward hostname/port and TLS settings.
- `HOUFENG_PUBLIC_BASE_URL` is a required external HTTPS URL. Trusted proxy CIDRs remain an explicit operator choice.

### Configuration

- The downloaded environment template is organized in exactly three operator-facing sections:
  1. **Must change** — values without which the production stack must not start or must not be considered configured.
  2. **Recommended** — values needed for the normal majority-feature deployment and long-term operation.
  3. **Optional** — integrations, tuning, alternate storage, and additional hardening.
- Required secret values are entered in the untracked `.env` file but are exposed to containers only through service-scoped Compose secrets whenever supported by the consuming binary/image.
- Necessary configuration is explicit; it is not silently invented from the domain or administrator account.
- Authority identity, key material, and its constrained database credential are generated and stored by the stack under `./data/records-authority`; they are internal deployment state, not additional operator variables.
- Optional configuration only names variables actually supported by current code.

### Portability and long-term operation

- PostgreSQL, local attachments, Center file logs, Records authority state, and other persistent local state live under the deployment directory's `./data/` tree rather than anonymous or project-named business-data volumes.
- One-shot storage initialization makes non-root Houfeng paths writable without an operator `chown` step.
- Documentation includes backup/restore, upgrade, rollback, migration-to-another-host, log/health inspection, image pinning, and secret-rotation boundaries.
- PostgreSQL, local attachments, and Records authority state are documented as one coordinated recovery point. ClamAV signatures are reproducible cache data but remain inside the portable layout.

### Documentation and compatibility

- Update `README.md`, the canonical deployment guide, Compose/env artifacts, Docker image contents, release workflow, tests, and the Trellis deployment contract together.
- Keep direct local/systemd deployment documented as an advanced path without making it the Docker quick start.
- Preserve current Center/processor non-root and processor sandboxing guarantees.

## Acceptance Criteria

- [ ] README production quick start uses download commands for `compose.yaml` and the stable public asset `compose.env.example` (saved locally as `.env`), then tells the user to edit `.env`, run `docker compose config`, and only afterward run `docker compose up -d`.
- [ ] The downloaded template has visible Must change / Recommended / Optional sections and defaults `HOUFENG_RECORDS_ENABLED=true`.
- [ ] `compose.yaml` uses only published images, contains the complete Center/Web + processor + ClamAV + PostgreSQL topology, and contains no Caddy or agent service.
- [ ] Center is reachable to Nginx Proxy Manager over the required external network at the documented service name and port, with no public host port by default.
- [ ] Fresh `docker compose up -d` on an empty deployment directory performs storage and database initialization automatically and reaches healthy Center plus running processor/ClamAV/PostgreSQL without manual SQL or a wrapper script.
- [ ] Database initialization uses distinct constrained runtime, platform-admin, and direct-migrator PostgreSQL roles and invokes `ConvergeAppACLCurrent` as the only Records APP schema/ACL writer; Center subsequently passes current runtime admission as the runtime role.
- [ ] A dedicated constrained authority role can execute only the narrow membership-heartbeat interface: it has no direct Records table DML, projector, bootstrap, migrator, platform-admin, or application-runtime privileges.
- [ ] Fresh initialization generates a durable Ed25519-backed local authority bundle outside PostgreSQL, verifies its complete bounded ledger, derives the existing activation projection, verifies the CAS receipt, and reaches an active contract without manual commands.
- [ ] The long-running authority renews only the exact Compose runtime membership; Center waits for authority health, and stale/missing/mismatched authority state causes Records writes and startup/health to fail closed rather than bypass admission.
- [ ] Initialization failure prevents Center and processor startup and returns visible failure through Compose service state/logs; exact repeat is non-mutating except approved credential rotation.
- [ ] All durable local paths are under `./data/`; copying `.env`, `compose.yaml`, and `data/` is the documented host-migration unit, including coordinated PostgreSQL + Records authority restore.
- [ ] Center and processor do not receive the bootstrap secret; processor does not receive administrator/session/migrator/platform-admin secrets.
- [ ] Release automation uploads version-matched `compose.yaml` and `compose.env.example` assets, never relies on a hidden release-asset filename, verifies their pinned project image version before upload, then publicly reads back exactly those deployment names and byte-identical downloads before reporting success.
- [ ] Static/TDD tests cover topology, config grouping, no-build/no-manual-SQL docs, secret scoping, release assets, NPM network, role provisioning, failure ordering, and idempotence.
- [ ] A real isolated Docker smoke test proves fresh start, an actual admitted Records write, attachment upload/process/ClamAV flow, authority heartbeat and restart idempotence, zero manual DB step, fail-closed corrupt/missing authority state, and no task-owned residue after cleanup.
- [ ] Focused Go tests, `make verify-go`, Compose validation, `actionlint` when available, `git diff --check`, and independent `trellis-check` pass on the final snapshot.

## Non-goals

- Kubernetes, Swarm, HA PostgreSQL, multi-host object-storage orchestration, or automatic application upgrades.
- Containerizing the Houfeng agent.
- Replacing Nginx Proxy Manager or managing its certificate/account lifecycle.
- Claiming that a single-host Compose deployment removes the need for tested off-host backups.

## Approved decisions

- Use existing published Docker images; do not build source on the deployment host.
- Use Nginx Proxy Manager as the external proxy.
- Include Records, attachment processing, and ClamAV.
- Make all database provisioning automatic for ordinary users.
- Default `HOUFENG_RECORDS_ENABLED=true`.
- Use the approved single-host Compose Records authority profile: persistent signed state outside PostgreSQL, existing contract projector for activation, and a narrow least-privilege membership heartbeat role. Do not fabricate authority rows in db-init and do not weaken the admission gate.
