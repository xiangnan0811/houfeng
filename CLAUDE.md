# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project identity

**候风 / Houfeng Fleet Control Plane** — a single-user, monitoring-and-probe-first fleet control plane. This repo started as the V1 implementation repo and now has passed the V1 release-gate judgment recorded in `docs/release/next-phase-plan.md`.

**重要**：V1 ≠ MVP。V1 frozen 业务结构仍是基础边界，但 post-V1 第一条扩展计划（VPS Asset Ledger + Fleet Observability）已闭合到当前计划边界。下一步不要自动继续旧计划；先查 `docs/release/current-state-and-next-stage-plan.md`。用户已确认当前下一阶段入口是核心页面产品/UX 重新规划，父级规划见 `docs/release/core-pages-product-ux-replan.md`。

V1 业务结构（数据模型 / 规则 / 技术选型 / 交互原型）frozen 在 `docs/design/v1-baseline/` 的 4 份子集（`architecture-data-model.md` + `rules-and-interaction.md` + `tech-selection.md` + `interactive-prototype-and-operation-flow.md`）。视觉部分已 unfrozen，权威指向 `docs/design/v2-houfeng/`。

实现阶段不重新设计 V1 一级能力。如发现实现与 frozen 业务子集 mismatch，先记录到 `docs/release/v1-gap-checklist.md`，再参考 `docs/release/next-phase-plan.md` 与 `docs/release/current-state-and-next-stage-plan.md` 决定优先级。

Naming 不变：product `候风 / Houfeng Fleet Control Plane`；binaries `houfeng-center` 和 `houfeng-agent`。

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

Center entry: `cmd/houfeng-center/main.go` → `bootstrapCenter` (`cmd/houfeng-center/bootstrap.go`) → `internal/center/app.New`. Bootstrap wires the pgx pool, applies embedded migrations, constructs Postgres-backed repositories, the incident notifier (Telegram or no-op based on settings + env), the retention worker, the session cleanup worker, and the HTTP router, then runs the app with workers (`incidentSvc`, `retentionWorker`, `sessionCleanup`).

Agent entry: `cmd/houfeng-agent/main.go` → `agent/runtime.New(...).Run(ctx)`. Agent reads token from a file, fingerprints the host, samples, runs probes, and posts batches via the agent contract.

Agent ↔ center contract: `internal/contracts/agentapi/` — only two routes today, `POST /api/agent/enroll` and `POST /api/agent/sync`. Both sides depend on this package; keep request/response types here, not in handler/runtime packages.

The center also serves the built React SPA from `HOUFENG_WEB_DIST_DIR` (handler in `internal/center/http/handlers/spa.go`). Production deploys ship `web/dist/` next to the Go binary.

## Configuration (env)

See `.env.example` and `docs/deploy/local-and-systemd.md`. Required at minimum:

- Center: `HOUFENG_HTTP_ADDR`, `HOUFENG_DATABASE_URL`, `HOUFENG_WEB_DIST_DIR`, `HOUFENG_INCIDENT_SWEEP_INTERVAL`, `HOUFENG_INITIAL_USERNAME`, `HOUFENG_INITIAL_PASSWORD`. Telegram is disabled unless **both** `HOUFENG_TELEGRAM_BOT_TOKEN` and `HOUFENG_TELEGRAM_CHAT_ID` are set; runtime overrides via `center_settings` table can switch to runtime-managed Telegram (`telegram_runtime_managed`). Initial username/password seed the first admin user when the users table is empty, but the env vars are still required by config load.
- Agent: `HOUFENG_AGENT_SERVER_URL`, `HOUFENG_AGENT_TOKEN_FILE`, `HOUFENG_AGENT_BUFFER_FILE`, `HOUFENG_AGENT_BUFFER_MAX_ENTRIES`, `HOUFENG_AGENT_BUFFER_MAX_AGE`. Token file must contain an enrollment token issued from the Node onboarding flow.

## Backend architecture (`internal/center/`)

- `app/` — process lifecycle: HTTP server + workers (`Worker.Run(ctx)`).
- `config/` — env loading; `CenterConfig`.
- `http/` — `router.go` builds the chi-style routing tree from `RouterOptions`; bootstrap fills every handler explicitly so wiring is grep-able. `http/middleware.go` carries `RequireSession` (cookie-backed auth gate) used by every non-agent / non-health route. Subpackage `http/handlers/` has one file per resource (`nodes`, `targets`, `incidents`, `events`, `dashboard`, `settings`, `agent`, `runtime_facts`, `runtime_controls`, `node_onboarding`, `health`, `spa`, `auth`, `metadata`). JSON helpers in `handlers/json.go`.
- `store/` — Postgres repositories (one file per aggregate); migrations in `db/migrations/*.sql` are **embedded** via `db/migrations/embed.go` and applied at startup by `store/migrate.Apply`. Migrations are the single source of truth — write raw SQL, do not introduce an ORM.
- `auth/` — username/password login, session lifecycle, password hashing, initial-user seeding, session cleanup worker (consumed by `RequireSession` middleware and the `/api/auth/*` handlers).
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
- `agentapi.ProbeKind` constants are exactly three: `tcp` / `http` / `tls` (see `internal/contracts/agentapi/types.go`). `https` is not a separate kind — it runs as `http` with TLS configuration on the Target. Do not add new kinds without baseline approval (V1 business definition lives in `docs/design/v1-baseline/rules-and-interaction.md`).
- Health state is **derived** (正常/关注/告警/严重); lifecycle is managed (待接入/在用/观察中/不续费/已退役); maintenance is a runtime control, not a health state.
- Raw observations land first; incident/event/notification objects are produced by in-process workers, not in the request path.
- "Backfilled" observations (`is_backfilled=true`) are stored but must not retroactively trigger Telegram.

When adding a new endpoint: add the handler in `http/handlers/`, register it in `RouterOptions` and `router.New`, wire it in `cmd/houfeng-center/bootstrap.go`, and add table-driven tests next to the handler. New persisted state needs both a new migration in `db/migrations/` and updates to the relevant `store/` repository.

## Agent architecture (`agent/`)

- `config/` — env loading.
- `token/` — file-backed token source.
- `fingerprint/` — host identity.
- `enroll/` — first-run enrollment against `/api/agent/enroll`.
- `hostsample/`, `probe/`, `containersample/` — host collection, probe execution (TCP/HTTP/TLS), and opportunistic Docker container facts when the local `docker` CLI is available.
- `exec/` — compiled-in whitelist for node actions; the center sends `command_id`, and the agent maps it to fixed binary/args without accepting user-supplied shell text.
- `syncqueue/` — durable on-disk buffer (single JSON file at `HOUFENG_AGENT_BUFFER_FILE`, bounded by `MAX_ENTRIES` and `MAX_AGE`). Stays pure-Go; no embedded DB.
- `runtime/` — main loop: collect → buffer → sync to `/api/agent/sync` → apply returned plan.

Agents must remain "thin": observe, buffer, sync, fetch plan, and apply center-issued plans. They must not accept arbitrary scripts, user-supplied command args, or local rule evaluation. Current exceptions are intentionally bounded: whitelisted node actions (`agent/exec`) and best-effort Docker CLI sampling (`agent/containersample`). Command-result durability and the Agent command / Docker product boundary are closed to the current scope; do not expand into arbitrary scripts, user-defined command args, Docker control, or orchestration without a new product plan.

## Frontend (`web/`)

React 19 + TypeScript + Vite SPA, Vitest + jsdom for tests, ESLint flat config. Routing in `src/app/router.tsx`, layout shell in `src/app/layout/`, page components in `src/pages/` (one `*.tsx` + colocated `*.test.tsx` per page), shared atoms in `src/components/atoms/`, composite components in `src/components/`, API client + types + formatters in `src/lib/`.

Atoms (`web/src/components/atoms/`): `Badge` / `Button` / `Card` / `Input` / `Toggle` / `Tabs` / `Sparkline` (SVG 64×16 mini chart) / `TrendArrow` / `StatusGlyph` (6-state shape indicator) / `Mono` (`MonoDigits` / `Hostname` / `Timestamp`) / `DataTable` (compact 36px / standard 44px) / `MetricChart` (SVG 360×140 full chart — X/Y axes, thresholds, maintenance windows, crosshair tooltip) / `Drawer` (right/left slide-in panel with portal, overlay, ESC, focus containment, and focus restore) / `Stepper` (horizontal 4-step progress bar). All pure CSS + BEM + design tokens; no Tailwind / CSS-in-JS / chart library introduced (per `design-language.md` §12).

Visual authority: `docs/design/v2-houfeng/design-language.md` + `docs/design/v2-houfeng/component-spec.md`. v2-houfeng has superseded the earlier v1-baseline visual sections (`ui-ux-spec`, `baseline-screens`, `visual-review-round2`, `stitch/*`) and the entire `v1.x-frontend-redesign/` package. Those historical materials have been moved to `docs/_archive/design/` and are kept for traceability only — do not regress to them. Dark-first, Chinese as the primary UI language, high-density engineering-tool feel.

## Planning and verification artifacts

当前规划、运维与发布证据：

- `docs/operations/v1-smoke-run.md` — fresh-install smoke against a real Postgres (V1 release gate 的核心证据).
- `docs/operations/` — v2 visual evidence screenshots (Dashboard / 节点列表 / 节点详情 / 目标列表 / 目标详情，2026-05-06)。
- `docs/release/v1-gap-checklist.md` — implementation-vs-design gap 清单（含 V1 release gate 与 12 条 2026-05-02 新增 gap）.
- `docs/release/docs-audit.md` — docs 审计与 archive 决策（T1 落地，决定哪些 docs 是 keep / archive）.
- `docs/release/next-phase-plan.md` — 下一阶段开发计划（Stage 1 V1 收口 / Stage 2 post-V1 → MVP / Stage 3+ 远期）.
- `docs/release/asset-ledger-roadmap-completion.md` — VPS Asset Ledger 计划完成度审计。
- `docs/release/current-state-and-next-stage-plan.md` — 当前项目剩余工作审计与下一阶段入口；明确旧计划无立即任务、真实数据条件性延期、前端机械拆分暂停。
- `docs/release/core-pages-product-ux-replan.md` — 用户已确认的核心页面产品/UX 重新规划；后续前端实现应从 UX-1 App shell / 导航 / 视觉基线重置开始拆任务。
- `docs/deploy/local-and-systemd.md` 与 `docs/deploy/systemd/*.service` — canonical 部署 recipe.

注：早期的 `docs/operations/v1-visual-verification.md` 与 `docs/operations/visual-evidence/` 与 v1-baseline/stitch 视觉强绑定，stitch 已 archive 后这两份也已迁至 `docs/_archive/operations/`，仅作历史记录。当前已有一次性 v2 截图证据直接存放在 `docs/operations/*.jpg`；正式、可重复的 v2 视觉证据收集流程仍待后续建立。

When changing user-visible behavior, first record the gap in `docs/release/v1-gap-checklist.md` and consult `docs/release/next-phase-plan.md`, `docs/release/current-state-and-next-stage-plan.md`, and `docs/release/core-pages-product-ux-replan.md` for prioritization, rather than editing the frozen baseline docs.
