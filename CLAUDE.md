# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project identity

**候风 / Houfeng Fleet Control Plane** — an early-stage, single-operator, monitoring-and-probe-first fleet control plane with a lightweight VPS Asset Ledger. The repository should stay truthful to current code and deployment reality: do not present it as production-ready packaging, a Docker/Kubernetes platform, a multi-user SaaS, or a completed real-inventory validation.

V1 business structure (data model / rules / technical choices / operation flow) is retained in `docs/design/v1-baseline/` as the frozen historical baseline. Current active product semantics use `MonitoringInstance` for the former Node domain, plus Target, ProbeItem, agent, incident, and notification semantics. Visual authority is `docs/design/v2-houfeng/`.

Keep constraints proportional to current code and accepted architecture. Preserve hard boundaries backed by implementation and deployment reality, especially the single center + PostgreSQL + outbound systemd agents topology, center-owned one-command install contract, thin-agent security model, raw-observation-first incident processing, and token secrecy. Do not revive completed roadmap/process documents as active requirements.

Naming stays: product `候风 / Houfeng Fleet Control Plane`; binaries `houfeng-center` and `houfeng-agent`.

## Common commands

All workflow targets live in `Makefile` and operate on `./agent/...`, `./cmd/...`, `./db/...`, `./internal/...`.

```bash
# Go: format, vet, unit tests
make fmt-go
make vet-go
make test-go
make verify-go            # = fmt-go + vet-go + test-go

# Build single binaries
make build-center         # -> ./bin/houfeng-center
make build-agent          # -> ./bin/houfeng-agent
make build-agent-release VERSION=v1.2.3

# Web (in web/, Node 22)
cd web && npm ci
cd web && npm run dev     # vite dev server
cd web && npm run build   # tsc -b + vite build -> web/dist
cd web && npm run test    # vitest (jsdom)
cd web && npm run lint    # eslint

# Full repo verification (Go + web build/test)
./scripts/verify.sh       # equivalent to: make verify-go && make verify-web

# Single Go test
go test ./internal/center/store -run TestPostgresMonitoringInstanceRepository
go test ./internal/center/http/handlers -run TestMonitoringInstanceOnboarding -v

# Single web test
cd web && npx vitest run src/pages/MonitoringPage.test.tsx
```

CI (`.github/workflows/ci.yml`) runs `make verify-go` and `make verify-web` on push/PR to `main`.

## Runtime layout

Current topology is exactly: **1 Go center + 1 PostgreSQL + N systemd Go agents**. Agents always initiate; the center never connects back to an agent. Do not add MQs, TSDBs, microservices, Docker/Kubernetes deployment, or package-manager publishing without a new product/architecture decision.

Center entry: `cmd/houfeng-center/main.go` → `bootstrapCenter` (`cmd/houfeng-center/bootstrap.go`) → `internal/center/app.New`. Bootstrap wires the pgx pool, applies embedded migrations, constructs Postgres-backed repositories, the incident notifier (Telegram/Feishu or no-op based on settings + env), the retention worker, the session cleanup worker, and the HTTP router, then runs the app with workers.

Agent entry: `cmd/houfeng-agent/main.go` → `agent/runtime.New(...).Run(ctx)`. Agent reads token state from a file, fingerprints the host, samples, runs probes, buffers sync data, and posts batches via the agent contract.

Agent ↔ center contract: `internal/contracts/agentapi/` — `POST /api/agent/enroll`, `POST /api/agent/sync`, and public installer script path `GET /api/agent/install.sh`. Both sides depend on this package; keep request/response types here, not in handler/runtime packages.

The center also serves the built React SPA from `HOUFENG_WEB_DIST_DIR` (handler in `internal/center/http/handlers/spa.go`). Production-like deploys ship `web/dist/` next to the Go binary or point `HOUFENG_WEB_DIST_DIR` at the installed SPA directory.

## Configuration (env)

See `.env.example` and `docs/deploy/local-and-systemd.md`. Required at minimum:

- Center: `HOUFENG_HTTP_ADDR`, `HOUFENG_DATABASE_URL`, `HOUFENG_WEB_DIST_DIR`, `HOUFENG_INCIDENT_SWEEP_INTERVAL`, `HOUFENG_INITIAL_USERNAME`, `HOUFENG_INITIAL_PASSWORD`. `HOUFENG_PUBLIC_BASE_URL` is optional for center startup but required for generated one-command install commands; it must be an externally reachable absolute `http(s)` URL without query or fragment. `HOUFENG_LOG_FILE` is optional; when set, center tees structured `slog` output to stdout and the configured file. Telegram is disabled unless both `HOUFENG_TELEGRAM_BOT_TOKEN` and `HOUFENG_TELEGRAM_CHAT_ID` are set; runtime settings can also manage Telegram/Feishu notification behavior. Initial username/password seed the first admin user when the users table is empty, but the env vars are still required by config load.
- Agent: `HOUFENG_AGENT_SERVER_URL`, `HOUFENG_AGENT_TOKEN_FILE`, `HOUFENG_AGENT_BUFFER_FILE`, `HOUFENG_AGENT_BUFFER_MAX_ENTRIES`, `HOUFENG_AGENT_BUFFER_MAX_AGE`. The token file initially contains an enrollment token issued from the MonitoringInstance onboarding flow or center-generated install command; after enrollment it stores post-enrollment sync credentials.

## Backend architecture (`internal/center/`)

- `app/` — process lifecycle: HTTP server + workers (`Worker.Run(ctx)`).
- `config/` — env loading; `CenterConfig`.
- `http/` — `router.go` builds the routing tree from `RouterOptions`; bootstrap fills every handler explicitly so wiring is grep-able. `http/middleware.go` carries `RequireSession` (cookie-backed auth gate) used by every non-agent / non-health route. Subpackage `http/handlers/` has one file per resource (`monitoring_instances`, `monitoring_instance_onboarding`, `monitoring_instance_actions`, `monitoring_instance_batch`, `monitoring_instance_sparklines`, `targets`, `incidents`, `events`, `dashboard`, `settings`, `agent`, `runtime_facts`, `runtime_controls`, `health`, `spa`, `auth`, `metadata`, Asset Ledger handlers). JSON helpers in `handlers/json.go`.
- `store/` — Postgres repositories (one file per aggregate); migrations in `db/migrations/*.sql` are embedded via `db/migrations/embed.go` and applied at startup by `store/migrate.Apply`. Migrations are the single source of truth — write raw SQL, do not introduce an ORM.
- `auth/` — username/password login, session lifecycle, password hashing, initial-user seeding, session cleanup worker.
- `enrollment/` — agent token issuing, fingerprint binding, binding-state transitions (`未绑定` / `已绑定` / `指纹变更待确认`).
- `incidents/` — incident judgment, debounce, snapshotting; settings-backed thresholds and sweep interval.
- `notify/` — Telegram/Feishu delivery clients, wrapped by incidents/settings logic so persisted settings can override defaults at runtime.
- `syncing/` — agent batch ingest pipeline (raw observations first, then incident eval).
- `retention/` — per-table retention worker driven by center settings.
- `settings/` — `CenterSettings` model + repository; first-install fallbacks are layered in bootstrap.
- `providers/`, `vpsassets/`, `subscriptions/`, `assetlinks/`, `renewals/`, `assetservices/`, `assetdomains/`, `importing/` — VPS Asset Ledger domains, JSON dry-run/import, and lightweight VPS-scoped service/domain records.
- `targets/`, `monitoringinstances/`, `agentplan/`, `runtimefacts/`, `observations/`, `ids/` — core observability domain helpers.

Key model invariants:

- `VPS = a server asset ledger object`. It owns provider, cost, lifecycle, service/domain context, and operator decisions.
- `MonitoringInstance = an agent-attached runtime observation object`. Same machine reinstall can stay the same MonitoringInstance; hardware replacement or a distinct agent identity should become a new MonitoringInstance unless the operator explicitly confirms otherwise.
- VPS and MonitoringInstance are connected only through explicit links. Link/unlink never implicitly rewrites VPS lifecycle, subscription state, Target state, agent plan, or MonitoringInstance runtime state.
- `Target = an observable entrypoint`. `ProbeItem` only describes how to observe it; address belongs to `Target`.
- `agentapi.ProbeKind` constants are exactly three: `tcp` / `http` / `tls` (see `internal/contracts/agentapi/types.go`). `https` is not a separate kind — it runs as `http` with TLS configuration on the Target.
- Health state is derived (`正常` / `关注` / `告警` / `严重`); lifecycle is managed (`待接入` / `在用` / `观察中` / `不续费` / `已退役`); maintenance is a runtime control, not a health state.
- Raw observations land first; incident/event/notification objects are produced by in-process workers, not in the request path.
- Backfilled observations (`is_backfilled=true`) are stored but must not retroactively trigger outbound notifications.

When adding a new endpoint: add the handler in `http/handlers/`, register it in `RouterOptions` and `router.New`, wire it in `cmd/houfeng-center/bootstrap.go`, and add table-driven tests next to the handler. New persisted state needs both a migration in `db/migrations/` and updates to the relevant `store/` repository.

## Agent architecture (`agent/`)

- `config/` — env loading.
- `token/` — file-backed token state.
- `fingerprint/` — host identity.
- `enroll/` — first-run enrollment against `/api/agent/enroll`.
- `hostsample/`, `probe/`, `containersample/` — host collection, probe execution (TCP/HTTP/TLS), and opportunistic Docker container facts when the local `docker` CLI is available.
- `exec/` — compiled-in whitelist for monitoring instance actions; the center sends `command_id`, and the agent maps it to fixed binary/args without accepting user-supplied shell text.
- `syncqueue/` — durable on-disk buffer (single JSON file at `HOUFENG_AGENT_BUFFER_FILE`, bounded by `MAX_ENTRIES` and `MAX_AGE`). Stays pure-Go; no embedded DB.
- `runtime/` — main loop: collect → buffer → sync to `/api/agent/sync` → apply returned plan.

Agents must remain thin: observe, buffer, sync, fetch plan, and apply center-issued plans. They must not accept arbitrary scripts, user-supplied command args, local rule evaluation, Docker control/orchestration, or shell snippets. Current bounded exceptions are compiled-in diagnostic command IDs (`agent/exec`) and best-effort Docker CLI facts (`agent/containersample`).

## Agent one-command install boundary

- Generated install commands must come from the center via `POST /api/monitoring-instances/{monitoring_instance_id}/install-command` and use `HOUFENG_PUBLIC_BASE_URL` as the URL authority.
- The browser must not synthesize production install commands from `window.location.origin`.
- `GET /api/agent/install.sh` is public and contains no deployment-specific secret until a generated command passes `--enrollment-token`.
- The generated token is short-lived and one-time. Regeneration invalidates the previous active token for that MonitoringInstance.
- Agent release assets are `houfeng-agent_<version>_linux_amd64`, `houfeng-agent_<version>_linux_arm64`, and `sha256sums.txt`; the installer verifies checksums before replacing the binary or starting systemd.
- MVP installer support is Linux + systemd + `amd64`/`arm64` only. Auto-upgrade, uninstall UX, non-systemd hosts, package repos, Docker/Kubernetes installs, and center-hosted binary mirrors are out of scope.
- Installer output, logs, docs, and UI copy must not expose full enrollment tokens except in the deliberate authenticated copy/reveal surface.

## Frontend (`web/`)

React 19 + TypeScript + Vite SPA, Vitest + jsdom for tests, ESLint flat config. Routing in `src/app/router.tsx`, layout shell in `src/app/layout/`, page components in `src/pages/` (one `*.tsx` + colocated `*.test.tsx` per page), shared atoms in `src/components/atoms/`, composite components in `src/components/`, API client + types + formatters in `src/lib/`.

Atoms (`web/src/components/atoms/`): `Badge` / `Button` / `Card` / `Input` / `Toggle` / `Tabs` / `Sparkline` / `TrendArrow` / `StatusGlyph` / `Mono` (`MonoDigits` / `Hostname` / `Timestamp`) / `DataTable` / `MetricChart` / `Drawer` / `Stepper`. All pure CSS + BEM + design tokens; no Tailwind / CSS-in-JS / chart library introduced.

Visual authority: `docs/design/v2-houfeng/design-language.md` + `docs/design/v2-houfeng/component-spec.md`. Earlier v1 visual screenshots, Stitch materials, and v1.x frontend redesign process docs have been removed from tracked docs; do not use them as current guidance. Dark-first, Chinese as the primary UI language, high-density engineering-tool feel.

## Public docs and evidence

Current tracked docs are intentionally lean:

- `README.md` — public project overview and quick start.
- `docs/README.md` — maintained docs index.
- `docs/deploy/local-and-systemd.md` and `docs/deploy/systemd/*.service` — canonical deployment recipe.
- `docs/operations/v1-smoke-run.md` — current fresh-install smoke workflow, with one-command onboarding primary and manual token fallback secondary.
- `docs/operations/v2-visual-evidence.md` — active UI preview/browser-sanity workflow; raster screenshots stay untracked unless explicitly approved as public README/docs assets.
- `docs/operations/asset-ledger-real-data-validation-readiness.md` and `docs/operations/asset-ledger-local-sample.json` — non-sensitive Asset Ledger sample validation and real-data privacy boundaries.
- `docs/design/v1-baseline/` — retained business/data/rule/tech/operation-flow references.
- `docs/design/v2-houfeng/` — current visual language and component reference.

Completed roadmap, release-gate, audit, archive, and one-off evidence documents are intentionally not kept as tracked in-repository archive copies. If a future task needs historical context, use git history rather than restoring archived copies into the public docs tree.

When changing user-visible behavior, keep docs truthful to current code and update only active guidance/reference docs that remain useful to public operators or maintainers. Do not edit frozen baseline docs to justify new behavior; record new product decisions in the relevant active task/spec instead.
