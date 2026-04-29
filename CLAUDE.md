# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project identity

**候风 / Houfeng Fleet Control Plane** — a single-user, monitoring-and-probe-first fleet control plane. This repo is the V1 **implementation** repo. V1 product, interaction, and visual design are **frozen** in `docs/design/v1-baseline/`; do not redesign V1 first-class capabilities while implementing. If implementation discovers a mismatch, record the gap against the frozen baseline before changing behavior (see `docs/release/v1-gap-checklist.md`).

Naming is fixed: product is `候风 / Houfeng Fleet Control Plane`; binaries are `houfeng-center` and `houfeng-agent`.

## Common commands

All workflow targets live in `Makefile` and operate on `./agent/...`, `./cmd/...`, `./db/...`, `./internal/...`.

```bash
# Go: format, vet, unit tests
make fmt-go
make vet-go
make test-go
make verify-go            # = fmt-go + vet-go + test-go

# Build single binaries (skipped silently if cmd entrypoint absent)
make build-center         # -> ./bin/houfeng-center
make build-agent          # -> ./bin/houfeng-agent

# Web (in web/, Node 22)
cd web && npm ci
cd web && npm run dev     # vite dev server
cd web && npm run build   # tsc -b + vite build -> web/dist
cd web && npm run test    # vitest (jsdom)
cd web && npm run lint    # eslint

# Full repo verification (Go + web build/test)
./scripts/verify.sh       # equivalent to: make verify-go && make verify-web

# Single Go test
go test ./internal/center/store -run TestPostgresNodeRepository
go test ./internal/center/http/handlers -run TestNodeOnboarding -v

# Single web test
cd web && npx vitest run src/pages/NodesPage.test.tsx
```

CI (`.github/workflows/ci.yml`) runs `make verify-go` and `make verify-web` on push/PR to `main`.

## Runtime layout

V1 topology is exactly: **1 Go center + 1 PostgreSQL + N systemd Go agents**. Agents always initiate; the center never connects back to an agent. Do not add MQs, TSDBs, or microservices.

Center entry: `cmd/houfeng-center/main.go` → `bootstrapCenter` (`cmd/houfeng-center/bootstrap.go`) → `internal/center/app.New`. Bootstrap wires the pgx pool, applies embedded migrations, constructs Postgres-backed repositories, the incident notifier (Telegram or no-op based on settings + env), the retention worker, and the HTTP router, then runs the app with workers (`incidentSvc`, `retentionWorker`).

Agent entry: `cmd/houfeng-agent/main.go` → `agent/runtime.New(...).Run(ctx)`. Agent reads token from a file, fingerprints the host, samples, runs probes, and posts batches via the agent contract.

Agent ↔ center contract: `internal/contracts/agentapi/` — only two routes today, `POST /api/agent/enroll` and `POST /api/agent/sync`. Both sides depend on this package; keep request/response types here, not in handler/runtime packages.

The center also serves the built React SPA from `HOUFENG_WEB_DIST_DIR` (handler in `internal/center/http/handlers/spa.go`). Production deploys ship `web/dist/` next to the Go binary.

## Configuration (env)

See `.env.example` and `docs/deploy/local-and-systemd.md`. Required at minimum:

- Center: `HOUFENG_HTTP_ADDR`, `HOUFENG_DATABASE_URL`, `HOUFENG_WEB_DIST_DIR`, `HOUFENG_INCIDENT_SWEEP_INTERVAL`. Telegram is disabled unless **both** `HOUFENG_TELEGRAM_BOT_TOKEN` and `HOUFENG_TELEGRAM_CHAT_ID` are set; runtime overrides via `center_settings` table can switch to runtime-managed Telegram (`telegram_runtime_managed`).
- Agent: `HOUFENG_AGENT_SERVER_URL`, `HOUFENG_AGENT_TOKEN_FILE`, `HOUFENG_AGENT_BUFFER_FILE`, `HOUFENG_AGENT_BUFFER_MAX_ENTRIES`, `HOUFENG_AGENT_BUFFER_MAX_AGE`. Token file must contain an enrollment token issued from the Node onboarding flow.

## Backend architecture (`internal/center/`)

- `app/` — process lifecycle: HTTP server + workers (`Worker.Run(ctx)`).
- `config/` — env loading; `CenterConfig`.
- `http/` — `router.go` builds the chi-style routing tree from `RouterOptions`; bootstrap fills every handler explicitly so wiring is grep-able. Subpackage `http/handlers/` has one file per resource (`nodes`, `targets`, `incidents`, `events`, `dashboard`, `settings`, `agent`, `runtime_facts`, `runtime_controls`, `node_onboarding`, `health`, `spa`). JSON helpers in `handlers/json.go`.
- `store/` — Postgres repositories (one file per aggregate); migrations in `db/migrations/*.sql` are **embedded** via `db/migrations/embed.go` and applied at startup by `store/migrate.Apply`. Migrations are the single source of truth — write raw SQL, do not introduce an ORM.
- `enrollment/` — agent token issuing, fingerprint binding, binding-state transitions (未绑定 / 已绑定 / 指纹变更待确认).
- `incidents/` — incident judgment, debounce, snapshotting; `NewSettingsBackedService` wires the runtime configurable thresholds and sweep interval.
- `notify/` — Telegram delivery, wrapped by `incidents.NewSettingsAwareNotifier` so persisted settings can override env defaults at runtime.
- `syncing/` — agent batch ingest pipeline (raw observations first, then incident eval).
- `retention/` — per-table retention worker (driven by `center_settings`).
- `settings/` — `CenterSettings` model + repository; first-install fallbacks are layered in `bootstrap.go` (`settingsPresentationRepository`).
- `targets/`, `nodes/`, `agentplan/`, `runtimefacts/`, `observations/`, `ids/` — domain helpers.

Key model invariants (from `docs/design/v1-baseline/architecture-data-model.md`):
- `Node = a specific server`. Same machine reinstall stays the same Node; new hardware needs a new Node.
- `Target = an observable entrypoint`. `ProbeItem` only describes how to observe it; address belongs to `Target`.
- Probe kinds in V1 are exactly `tcp`, `http`/`https`, `tls`. Do not add new kinds without baseline approval.
- Health state is **derived** (正常/关注/告警/严重); lifecycle is managed (待接入/在用/观察中/不续费/已退役); maintenance is a runtime control, not a health state.
- Raw observations land first; incident/event/notification objects are produced by in-process workers, not in the request path.
- "Backfilled" observations (`is_backfilled=true`) are stored but must not retroactively trigger Telegram.

When adding a new endpoint: add the handler in `http/handlers/`, register it in `RouterOptions` and `router.New`, wire it in `cmd/houfeng-center/bootstrap.go`, and add table-driven tests next to the handler. New persisted state needs both a new migration in `db/migrations/` and updates to the relevant `store/` repository.

## Agent architecture (`agent/`)

- `config/` — env loading.
- `token/` — file-backed token source.
- `fingerprint/` — host identity.
- `enroll/` — first-run enrollment against `/api/agent/enroll`.
- `hostsample/`, `probe/` — collection and probe execution (TCP/HTTP/TLS).
- `syncqueue/` — durable on-disk buffer (single JSON file at `HOUFENG_AGENT_BUFFER_FILE`, bounded by `MAX_ENTRIES` and `MAX_AGE`). Stays pure-Go; no embedded DB.
- `runtime/` — main loop: collect → buffer → sync to `/api/agent/sync` → apply returned plan.

Agents must remain "thin": observe, buffer, sync, fetch plan. They do not run arbitrary scripts, run Docker, or evaluate rules locally — all interpretation happens in the center.

## Frontend (`web/`)

React 19 + TypeScript + Vite SPA, Vitest + jsdom for tests, ESLint flat config. Routing in `src/app/router.tsx`, layout shell in `src/app/layout/`, page components in `src/pages/` (one `*.tsx` + colocated `*.test.tsx` per page), shared atoms in `src/components/`, API client + types + formatters in `src/lib/`.

Visual authority: only the **Unified / Baseline Stitch** screens documented in `docs/design/v1-baseline/baseline-screens.md` and `ui-ux-spec.md`. Earlier concept screens are historical — do not regress to them. Dark-first, Chinese as the primary UI language, high-density engineering-tool feel.

## V1 verification artifacts

These docs track real-environment evidence beyond `make verify`:

- `docs/operations/v1-smoke-run.md` — fresh-install smoke against a real Postgres.
- `docs/operations/v1-visual-verification.md` and `docs/operations/visual-evidence/` — screenshot comparison vs. baseline.
- `docs/release/v1-gap-checklist.md` — implementation-vs-design gap log.
- `docs/deploy/local-and-systemd.md` and `docs/deploy/systemd/*.service` — canonical deployment recipe.

When changing user-visible behavior, copy/adjust evidence into `docs/operations/visual-evidence/` and update the gap checklist rather than editing the frozen baseline docs.
