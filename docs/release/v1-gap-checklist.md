# Houfeng V1 Gap Checklist

> **⚠️ V1 收口未完成 / Closed 状态待重审 (2026-05-02 标注)**
>
> 下方 ~30 行 Status = "Closed" 的判定截至 2026-04-30，与 2026-05-02 用户重新判定（实现连 V0.1 都不到）严重 mismatch。本次（T3）批量为所有 Closed 行追加 `(⚠️ need-reassess)` 标记，**不做逐行现场验证**——逐行验证由 T2 起草的 next-phase plan 列为独立 Stage 1 工作项。
>
> 本表此刻仍可作为"V1 设计意图清单"参考，但**不能作为"V1 已完成度"权威**。
>
> 末尾新增"V1 收口期发现的 gap 项 (新增 2026-05-02)"段记录 12 条 sub-agent 实证发现的代码-文档差异。

## Scope

This checklist compares the implementation repository against the frozen V1 baseline. It does not revise the baseline.

Status values:

- **Closed:** implemented and covered by automated or documented evidence.
- **Partial:** implemented or documented, but final live evidence is still required.
- **Deferred outside V1:** intentionally not part of frozen V1 delivery.

## Product and architecture baseline

| Area | Status | Evidence |
| --- | --- | --- |
| Product naming is `候风 / Houfeng Fleet Control Plane` | Closed (verified 2026-05-02) | `README.md`, binary names, design handoff. **Reassessed 2026-05-02**: README.md L1+L3+L28、CLAUDE.md L7+L15、Makefile L4-L5（`CENTER_BIN := ./bin/houfeng-center`、`AGENT_BIN := ./bin/houfeng-agent`）三处命名完全一致；`cmd/houfeng-center/` 与 `cmd/houfeng-agent/` 目录均存在。 |
| Go center + Go agent + React/Vite + PostgreSQL | Closed (verified 2026-05-02) | `go.mod`, `cmd/houfeng-center`, `cmd/houfeng-agent`, `web/package.json`, `db/migrations`. **Reassessed 2026-05-02**: `go.mod` 声明 `go 1.26.2` + `pgx/v5`；`cmd/houfeng-{center,agent}/main.go` 双入口存在；`web/package.json` 显示 React 19 + Vite 8 + Vitest 4；`db/migrations/` 含 11 个 SQL 文件 + `embed.go`。技术栈与基线 `tech-selection.md` 全部对齐。 |
| Single center process owns API/UI/background workers/notifications | Closed (verified 2026-05-02) | `cmd/houfeng-center/bootstrap.go`. **Reassessed 2026-05-02**: `bootstrap.go:115-146` 在同一进程内 wire HTTP router（含 SPA via `WebDistDir`）+ 3 个 `app.Worker`（`incidentSvc` / `retentionWorker` / `sessionCleanup`，line 146）+ Telegram notifier（`newIncidentNotifier`，line 169-183）。`internal/center/app/app.go` 显示 worker 与 HTTP server 同进程并发执行。与设计 §3 "异常 / 健康状态 / 通知由单体内部后台处理" 一致。 |
| systemd agent direction documented | Closed (verified 2026-05-02) | `docs/deploy/systemd/houfeng-agent.service`. **Reassessed 2026-05-02**: 单元文件包含 `[Unit]/[Service]/[Install]` 完整三段，含 `ExecStart=/usr/local/bin/houfeng-agent`、`EnvironmentFile`、`StateDirectory`、`Restart=always`、`NoNewPrivileges`/`ProtectHome`/`ProtectSystem` 等加固选项；center 侧 `houfeng-center.service` 同目录存在。 |
| Docker-first deployment | Deferred outside V1 | Frozen tech selection excludes Docker as required runtime |

## Core object model

| Area | Status | Evidence |
| --- | --- | --- |
| Node persistence and UI | Partial (was Closed) | `internal/center/store/nodes.go`, `web/src/pages/NodesPage.tsx`. **Reassessed 2026-05-02**: 后端齐全——`store/nodes.go` 1285 行，覆盖 CRUD + 5 档生命周期状态机（待接入/在用/观察中/不续费/已退役 — `RetireNode` / `RestoreRetiredNodeToObserving` 等）+ 3 档监控状态（启用/维护中/暂停 — `SetNodeMonitoringMaintenance` / `PauseNodeMonitoring` / `ResumeNodeMonitoring`）+ 3 档绑定状态（`ConfirmNodeRebind` / `RejectPendingFingerprint` / `ResetNodeBinding`），与设计 §5.1-5.3 完全对齐。前端 `NodesPage.tsx` 671 行有筛选、创建表单、跳转 onboarding。**已知偏差**：第 138 段 gap #10 已记录 `NodesPage.tsx:60` `createNode` 直接 `fetch('/api/nodes')` 绕过 `lib/api.ts`（反模式偿还点）；本行降级 Partial 反映该已记录技术债，不影响功能。 |
| Target persistence and UI | Closed (verified 2026-05-02) | `internal/center/store/targets.go`, `web/src/pages/TargetsPage.tsx`. **Reassessed 2026-05-02**: `store/targets.go` 700 行，覆盖 Target CRUD + 4 档运行状态机（启用/维护中/暂停/已归档 — `SetTargetMaintenance` / `PauseTargetRun` / `ResumeTargetRun` / `ArchiveTarget` / `RestoreArchivedTargetToPaused`），与设计 §5.4 完全对齐。`TargetsPage.tsx` 740 行含创建表单、ProbeItem 视角列表、暂停/归档 confirm UI；`TargetDetailPage.tsx` 1731 行处理详情页。 |
| ProbeItem persistence and UI | Closed (verified 2026-05-02) | `internal/center/store/targets.go`, `web/src/pages/TargetDetailPage.tsx`. **Reassessed 2026-05-02**: `store/targets.go:566-685` 含 `ListProbeItems` / `CreateProbeItem` / `UpdateProbeItem` / `DeleteProbeItem` / `GetProbeMetadata`。`TargetDetailPage.tsx:12-25` import CRUD client 函数；表单支持 tcp/http/tls 三 kind（line 60-62 `PROBE_KIND_OPTIONS`），与 `agentapi.ProbeKind*` (`internal/contracts/agentapi/types.go:31-33`) 一致；含编辑 (`openProbeEditForm`)、删除、配置字段 schema 校验（`hasUnsupportedProbeConfigFields`）。 |
| HostSample and ProbeObservation ingestion | Closed (verified 2026-05-02) | `internal/center/observations`, `internal/center/syncing`, `agent/hostsample`, `agent/probe`. **Reassessed 2026-05-02**: 端到端链路串通——`agent/hostsample/provider.go` (`Collect`) + `agent/probe/provider.go` (`CollectDue`，支持 tcp/http/tls 三 kind) → `agent/runtime/runtime.go:177-197` 构造 `SyncRequest` 并通过 `syncqueue` 缓冲后调 `/api/agent/sync` → `internal/center/http/handlers/agent.go:81` 调 `syncing.Service.SyncBatch` → `syncing/service.go:54` 写仓库并触发 `PostSyncProcessor`（即 `incidentSvc`）。`observations/service.go` 提供 `Ingest` + 探针元数据校验。 |
| Incident and Event model | Closed (verified 2026-05-02) | `internal/center/incidents`, `internal/center/store/dashboard.go`, `web/src/pages/EventsPage.tsx`. **Reassessed 2026-05-02**: `incidents/evaluator.go` 实现 7 类判定（heartbeat-missing / disk-pressure / inode-pressure / resource-pressure / probe-failure / TLS-expiry / node-trend-degradation / target-latency-trend-degradation）；`incidents/service.go` 装配 debounce + notifier；`store/incidents.go:122-261` `ApplyIncidentMutation` 在同一事务内更新 incidents、写 state-change events、保存 notification 记录、刷新 object summary。`store/dashboard.go` 提供 24h 趋势、异常 node/target 列表、events 查询。`EventsPage.tsx` 290 行筛选丰富（object_type / severity / event_type / label / 时间范围 / notification_only / recovery_only / maintenance_only）。 |

## Runtime behavior

| Area | Status | Evidence |
| --- | --- | --- |
| Node enrollment and binding state | Closed (⚠️ need-reassess) | `internal/center/enrollment`, `web/src/pages/NodeOnboardingPage.tsx` |
| Agent durable sync buffer | Closed (⚠️ need-reassess) | `agent/syncqueue`, `agent/runtime/runtime.go` |
| Node pause/maintenance/retire sync semantics | Closed (⚠️ need-reassess) | `internal/center/store/agent_plan.go`, runtime control tests |
| Target pause/maintenance/archive semantics | Closed (⚠️ need-reassess) | `internal/center/http/handlers/runtime_controls.go`, target page tests |
| Retention and daily aggregation execution | Closed (⚠️ need-reassess) | `internal/center/retention`, `internal/center/store/retention.go` |
| Trend degradation incident families | Closed (⚠️ need-reassess) | `internal/center/incidents/evaluator.go` |

## UI and interaction surfaces

| Area | Status | Evidence |
| --- | --- | --- |
| Frozen app shell and primary navigation | Closed (⚠️ need-reassess) | Implementation-level shell hierarchy and routes are aligned in `web/src/app/layout/AppShell.tsx`, `web/src/app/router.tsx`, and `web/src/index.css`; screenshot evidence remains tracked separately |
| Dashboard abnormal summaries and event stream | Closed (⚠️ need-reassess) | `web/src/pages/DashboardPage.tsx` |
| Nodes list filters and onboarding entry | Closed (⚠️ need-reassess) | `web/src/pages/NodesPage.tsx`, `web/src/pages/NodeOnboardingPage.tsx` |
| Node detail operational summary and trends | Closed (⚠️ need-reassess) | `web/src/pages/NodeDetailPage.tsx` |
| Target list/detail and ProbeItem management | Closed (⚠️ need-reassess) | `web/src/pages/TargetsPage.tsx`, `web/src/pages/TargetDetailPage.tsx` |
| Events advanced filters | Closed (⚠️ need-reassess) | `web/src/pages/EventsPage.tsx` |
| Settings runtime truthfulness | Closed (⚠️ need-reassess) | `web/src/pages/SettingsPage.tsx`, `internal/center/settings` |
| Chinese-first UI copy and dense baseline hierarchy | Closed (⚠️ need-reassess) | Alignment pass recorded in `docs/operations/v1-visual-verification.md`; frontend evidence in `web/src/app/layout/AppShell.tsx`, `web/src/components/ActionConfirmationCard.tsx`, `web/src/pages/DashboardPage.tsx`, `web/src/pages/NodesPage.tsx`, `web/src/pages/NodeDetailPage.tsx`, `web/src/pages/NodeOnboardingPage.tsx`, `web/src/pages/TargetsPage.tsx`, `web/src/pages/TargetDetailPage.tsx`, `web/src/pages/EventsPage.tsx`, and `web/src/pages/SettingsPage.tsx` |
| Visual screenshot comparison against baseline PNGs | Partial | Live route screenshots were captured on 2026-04-29 under `docs/operations/visual-evidence/` and indexed by `docs/operations/visual-evidence/manifest.json`; strict visual-fidelity acceptance remains pending because the captures have not been accepted as high-fidelity matches to the frozen references |

## Notifications

| Area | Status | Evidence |
| --- | --- | --- |
| Telegram notifier implementation | Closed (⚠️ need-reassess) | `internal/center/notify/telegram.go` |
| Settings-aware notification policy | Closed (⚠️ need-reassess) | `internal/center/incidents/service.go`, settings tests |
| Live Telegram delivery evidence | Partial | Requires operator credentials; smoke guide records evidence path |

## Delivery and operations

| Area | Status | Evidence |
| --- | --- | --- |
| Local build/test verification path | Closed (⚠️ need-reassess) | `Makefile`, `scripts/verify.sh` |
| systemd examples for center and agent | Closed (⚠️ need-reassess) | `docs/deploy/systemd/*.service` |
| Deployment guide | Closed (⚠️ need-reassess) | `docs/deploy/local-and-systemd.md` |
| Fresh-install smoke procedure | Closed (⚠️ need-reassess) | `docs/operations/v1-smoke-run.md` documents the reproducible Node → agent enrollment → Target → ProbeItem → observation → incident/event/notification path |
| Fresh-install smoke executed on live PostgreSQL | Closed (⚠️ need-reassess) | `docs/operations/v1-smoke-run.md` records the 2026-04-29 live run against PostgreSQL `192.168.100.192:5432/user_82Xkx5`: center health, Node, agent enrollment/sync, Target, ProbeItem, observation, incident start/recovery, and notification-backed event query passed. Telegram delivery and browser screenshots remain separate evidence rows. |

## Authentication (V1.x scope add)

| Area | Status | Evidence |
| --- | --- | --- |
| Username + password login (方案 2) | Closed (⚠️ need-reassess) | `internal/center/auth/`, `internal/center/store/users.go`, `internal/center/store/sessions.go`, migration `db/migrations/0010_add_users_and_sessions.sql` |
| All non-agent / non-health API protected by session cookie | Closed (⚠️ need-reassess) | `internal/center/http/middleware.go`, `internal/center/http/router.go`, `internal/center/http/auth_e2e_test.go` |
| Initial user seed from env on first startup | Closed (⚠️ need-reassess) | `internal/center/auth/seed.go`, `cmd/houfeng-center/bootstrap.go` |
| Session cleanup worker | Closed (⚠️ need-reassess) | `internal/center/auth/cleanup.go`, wired in `cmd/houfeng-center/bootstrap.go` |

## V1.x visual baseline (replaces frozen V1 visual portion)

The V1 visual baseline (Stitch Unified / Baseline screens, `docs/design/v1-baseline/ui-ux-spec.md`,
`visual-review-round2.md`, `baseline-screens.md`) was officially **unfrozen 2026-04-29** and replaced
by `docs/design/v1.x-frontend-redesign/`. The structural sections of the V1 baseline
(`architecture-data-model.md`, `rules-and-interaction.md`, `tech-selection.md`) remain frozen and
authoritative.

| Area | Status | Evidence |
| --- | --- | --- |
| 4-theme token system (候风原色 / 经典 × 深 / 浅) | Closed (⚠️ need-reassess) | `web/src/styles/tokens.css`, `web/src/lib/theme.ts`, `web/src/lib/theme-context.tsx` |
| FOUC-free sync theme bootstrap | Closed (⚠️ need-reassess) | inline script in `web/index.html` |
| 6 component atoms with tests | Closed (⚠️ need-reassess) | `web/src/components/atoms/*` |
| Sidebar shell with user chip + sync status | Closed (⚠️ need-reassess) | `web/src/app/layout/Sidebar.tsx`, `UserChip.tsx`, `SyncStatus.tsx` |
| Login page with backend auth (方案 2) | Closed (⚠️ need-reassess) | `web/src/pages/LoginPage.tsx`, Plan 1 backend |
| Route guard + 401 redirect | Closed (⚠️ need-reassess) | `web/src/app/RequireAuth.tsx`, `web/src/lib/auth-context.tsx` |
| Token-driven page chrome (8 pages) | Closed (⚠️ need-reassess) | `web/src/styles/pages.css` re-skins every page class through V1.x tokens |
| Theme tab inside Settings | Closed (⚠️ need-reassess) | `ThemeSettingsSection` block in `web/src/pages/SettingsPage.tsx` |
| Page-level redesign per spec §10 (身份卡, 5 Tab, 危险区, 趋势条) | Deferred — follow-up | Tracked as V1.x.1; current pages keep their pre-V1.x layouts under the new shell + tokens |
| Visual evidence (4 themes × representative pages) | Deferred — follow-up | Operations work; legacy V1 captures preserved under `docs/operations/visual-evidence/` |
| WCAG AA contrast verified per theme | Deferred — follow-up | Manual smoke pending |

## Final V1 release gate

Before tagging or declaring V1 fully release-ready, collect:

- passing `go test ./...`;
- passing `./scripts/verify.sh`;
- passing `cd web && npm run build`;
- completed live PostgreSQL smoke table in `docs/operations/v1-smoke-run.md` (collected 2026-04-29);
- visual screenshot comparison artifacts are captured; strict visual-fidelity acceptance or an explicit accepted waiver remains pending;
- Telegram delivery proof or an explicit note that Telegram is disabled for the deployment.

---

## V2 设计语言取代记录 (2026-05-01)

- v1 视觉基线（`docs/design/v1-baseline/ui-ux-spec.md`）和 v1.x 前端重设计（`docs/design/v1.x-frontend-redesign/`）的视觉部分均已被 **v2-houfeng** 取代
- 新视觉权威：`docs/design/v2-houfeng/{design-language,component-spec}.md`
- v2 不动后端 / 数据形状 / API / 路由 / 主题切换逻辑
- 实施完工证据：259 web tests 全绿、`npm run build` 通过、CSS 41KB（v1=28KB，+47%）
- 已知遗留：`agent/runtime` 的 `TestRuntimeQueuesFailedSyncAndRetriesAsBackfilled` / `TestRuntimeFlushesPersistedQueueAfterRestart` 在 main HEAD baseline 上同样失败，与 v2 重塑无关，须单独 issue 跟踪

---

## V1 收口期发现的 gap 项 (新增 2026-05-02)

下列 gap 项由 2026-05-02 的 .trellis/spec/ bootstrap 任务（00-bootstrap-guidelines）的 sub-agent 在实证编写 spec 时累积发现。状态为新发现项的"待处理"，不计入上面 V1 release gate。

### Backend (7 条)

| # | 现象 | 证据 |
|---|---|---|
| 1 | CLAUDE.md handler 清单缺 `auth.go` 与 `metadata.go`，但代码均存在 | `internal/center/http/handlers/{auth.go, metadata.go}` + `router.go:35-69` 注册 `/api/auth/*` |
| 2 | CLAUDE.md 子包清单未提 `internal/center/auth/` | 实际包含用户/会话/cookie/cleanup worker，配 0010 migration |
| 3 | `db/migrations/` 0004 序号撞车（**已确认无法 rename**） | `0004_add_node_onboarding_binding_state.sql` + `0004_add_observation_provenance.sql` 两份。`internal/center/store/migrate/migrate.go:16-19` 显示 `schema_migrations` 用文件名作主键，rename 会让已部署环境 re-apply 失败导致中心启动崩溃。**约定下次 migration 序号从 0011 起**（已落入 `.trellis/spec/backend/database-guidelines.md`）。字典序兼容现状不动 |
| 4 | `0010_add_users_and_sessions.sql` 索引命名不遵循 `idx_<table>_<purpose>` 规则 | `sessions_user_idx` / `sessions_expires_idx`，与其他迁移不一致 |
| 5 | bootstrap 实际 wire 了 3 个 worker（含 `sessionCleanup`），CLAUDE.md 只列 2 个 | `cmd/houfeng-center/bootstrap.go:146` + `bootstrap_test.go:152` 已断言 `len(workers)==3` |
| 6 | `agentapi.ProbeKind*` 只有 `tcp/http/tls` 三常量，CLAUDE.md 列了 4 种 | `internal/contracts/agentapi/types.go:30-34`；`https` 走 http+配置区分 |
| 7 | `cmd/houfeng-center/main.go` 仍用 stdlib `"log"`，与全仓 `slog` 不一致 | 历史遗留 |

### Web (5 条)

| # | 现象 | 证据 |
|---|---|---|
| 8 | `web/src/components/atoms/` 子目录 CLAUDE.md 未提（事实上的设计系统原子落点） | `web/src/components/atoms/{Button, Input, Badge, Card, Tabs, Toggle, Sparkline, StatusGlyph}.tsx` 等 |
| 9 | `web/src/lib/` 并存 `fetcher.ts`（auth）+ `api.ts`（业务）双 fetch 包装 + 双 401 钩子 | 历史遗留，可考虑合并 |
| 10 | `pages/NodesPage.tsx:60` `createNode` 直接 `fetch('/api/nodes')` 绕 `lib/api.ts` | 已识别反模式偿还点 |
| 11 | 多 page > 1000 行：`TargetDetailPage` 1731 / `NodeDetailPage` 1138 / `SettingsPage` 873 / `TargetsPage` 740 / `NodesPage` 671 | 技术债 |
| 12 | `make verify-web` 不跑 `npm run lint`，CI 抓不到 lint 失败 | `Makefile:67`；潜在风险 |
