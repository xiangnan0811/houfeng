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
| Node enrollment and binding state | Closed (verified 2026-05-02) | `internal/center/enrollment`, `web/src/pages/NodeOnboardingPage.tsx`. **Reassessed 2026-05-02**: enrollment 三档 binding 完整——`nodes/types.go:19-21` `BindingUnbound/BindingBound/BindingPendingConfirmation` + `store/nodes.go:1043-1078` 同 fingerprint→Bound、新 fingerprint 在已绑定→PendingConfirmation；`enrollment/service.go:78-89` 在 SyncToken 校验前先 reject `BindingPendingConfirmation` 或 fingerprint mismatch。`OnboardingRepository`（types.go:126-132）含 `IssueNodeEnrollmentToken / GetNodeOnboarding / ConfirmNodeRebind / RejectPendingFingerprint / ResetNodeBinding` 5 操作；`bootstrap.go:127-131` 全部 wire；`NodeOnboardingPage.tsx:62-88` 4 phase 含 `绑定冲突待处理` 卡片调三种 conflict action。与 rules-and-interaction.md §7.1 接入卡设计一致。 |
| Agent durable sync buffer | Closed (verified 2026-05-02) | `agent/syncqueue`, `agent/runtime/runtime.go`. **Reassessed 2026-05-02**: `syncqueue/store.go` 392 行——单文件 JSON + atomic temp+rename (L255-285) + 可选 fsync + 0o600 perm + per-path mutex + `pruneEntries` 同时按 `MaxAge` cutoff 与 `MaxEntries` 限长。`runtime.go:229-273` `enqueueAndFlush` 失败 → `MarkAttempt` + 下一轮 `syncRequest` 检测 `entry.Attempts > 0 || syncBatchID != currentBatchID` 时调 `syncqueue.WithBackfilledFacts(req, true)`（store.go:189-201 把 Heartbeats/HostSamples/ProbeObservations 全部 `IsBackfilled=true`）。后端 evaluator 对 `IsBackfilled` 一律 `skip`/`suppressNotification` 兑现"补传不触发追溯通知"（rules-and-interaction.md §5.3.5）。已知 `TestRuntime{Queues,Flushes}…` 在 macOS APFS 上偶发失败（CLAUDE.md L134 + store.go L24-31 注释），属测试环境耗时问题非实现缺陷。 |
| Node pause/maintenance/retire sync semantics | Closed (verified 2026-05-02) | `internal/center/store/agent_plan.go`, runtime control tests. **Reassessed 2026-05-02**: `agent_plan.go:99-106` 明确 3 档语义——`MonitoringMaintenance` → `HostSampleMaintenanceContext=true` 但仍下发 plan（标记维护上下文，evaluator 自然 suppress notify）；`MonitoringPaused || LifecycleRetired` → 清空 `HostSampleFrequencyTier` 并直接返回空 plan（agent 不再采）。L140 同时把 `MonitoringMaintenance` 透传到每个 ProbeAssignment 的 `MaintenanceContext`。`agent_plan_test.go:384` `TestBuildSyncPlanSuppressesPausedAndRetiredNodes` + `TestBuildSyncPlanMarksNodeMaintenanceContext` 覆盖三档转换；`runtime_controls.go:50-60` 暴露 `enter-maintenance/exit-maintenance/pause/resume` 4 action；`store/nodes.go` 含 `RetireNode/RestoreRetiredNodeToObserving` 生命周期入口。 |
| Target pause/maintenance/archive semantics | Closed (verified 2026-05-02) | `internal/center/http/handlers/runtime_controls.go`, target page tests. **Reassessed 2026-05-02**: `targets/types.go:14-17` 4 档常量 `启用/维护中/暂停/已归档` + allowedRunStatuses 白名单；`runtime_controls.go:138-150` `TargetRuntimeControls` 暴露 5 action (`enter-maintenance/exit-maintenance/pause/resume/archive/restore-to-paused`)；`agent_plan.go:111` SQL `t.run_status = any($1)` + Go 端只传 `[启用, 维护中]`，`暂停/已归档` 自然不下发 plan，agent 端无需感知。Target 维护中仍下发但 evaluator 走 maintenance suppress 路径（incidents/evaluator.go:30-36）。与 rules-and-interaction.md §6.4/§7.4 一致。 |
| Retention and daily aggregation execution | Closed (verified 2026-05-02) | `internal/center/retention`, `internal/center/store/retention.go`. **Reassessed 2026-05-02**: `worker.go:37-63` 周期 `runOnce` + 上下文取消处理；`store/retention.go:35-82` 在单 RepeatableRead transaction 内执行 9 步——upsert `node_host_sample_daily_aggregates` + upsert `target_probe_daily_aggregates`（按 UTC 日 group + p95 用 `percentile_cont(0.95) within group`，统计 backfilled/maintenance count），然后按 4 层 cutoff 删 heartbeats/host_samples/probe_observations/aggregates/events/notifications；`bootstrap.go:78` `retentionWorker := retention.NewWorker(retentionRepo, settingsRepo, ...)` + L146 `app.New(..., retentionWorker, ...)` 真在 worker pool 中跑。`worker_test.go` 6 个单测 + `store/retention_test.go` 4 个 SQL 单测覆盖 transaction/cutoff/rollback。 |
| Trend degradation incident families | Closed (verified 2026-05-02) | `internal/center/incidents/evaluator.go`. **Reassessed 2026-05-02**: `evaluator.go:142-242` 实现两类 trend incident——`EvaluateNodeTrendDegradation` (load5/iowait/steal 三指标按 7d 加权 baseline + 1.8x guard + 绝对 floor + 绝对 delta，需 ≥3 sample 且 maintenance/backfilled 直接 skip，单指标 Notice / 多指标 Alert，**无 Critical 路径** 与 §9.6 "默认不直接推严重" 一致)；`EvaluateTargetLatencyTrendDegradationAcrossSeries` 按 ProbeItem 分系列对比 baseline + 多 ProbeItem 或多节点退化才升 Alert。`incidents/types.go:166+169` 已注册 `IncidentNodeTrendDegradation` / `IncidentTargetLatencyTrendDegradation` 类；`service.go:288/320` 真接入 evaluation 链；`evaluator_test.go:396-606` 4 单测覆盖 starts/escalates/maintenance suppress/conservative recovery。设计 §1.3/§9.6 完全对齐。 |

## UI and interaction surfaces

| Area | Status | Evidence |
| --- | --- | --- |
| Frozen app shell and primary navigation | Closed (verified 2026-05-02) | Implementation-level shell hierarchy and routes are aligned in `web/src/app/layout/AppShell.tsx`, `web/src/app/router.tsx`, and `web/src/index.css`; screenshot evidence remains tracked separately. **Reassessed 2026-05-02**: `app/metadata.ts:12-18` `PRIMARY_NAV_ITEMS` 含 5 项（首页/节点/目标/事件/设置），与 rules-and-interaction.md §6.1 完全一致；`AppShell.tsx` 装配 `Sidebar`（含 `UserChip` + `SyncStatus`）+ `TopBar`（含 `Breadcrumb` + `GlobalSearch`）+ `ChangePasswordModal` + `Outlet`；`router.tsx` 注册全 9 路由（含 `/nodes/:id/onboarding` 与 `/targets/:id`）。无 children list 缺失。 |
| Dashboard abnormal summaries and event stream | Closed (verified 2026-05-02) | `web/src/pages/DashboardPage.tsx`. **Reassessed 2026-05-02**: 382 行实现 rules-and-interaction.md §6.2 三块——5 KPI strip（风险对象 / 严重对象 / 维护对象 / 新增异常 / 恢复事件，后两者带 24h Sparkline，line 337-353）+ `AbnormalNodeList`（line 92-168，按健康度排序，渲染地区/供应商/生命周期/活跃异常/最近心跳）+ `AbnormalTargetList`（line 170-247，含 host:port/类型/活跃异常/最近成功失败时间）+ `EventList` 最近事件流。`getDashboard()` 真实接 backend；fresh-install 空态 + loading 态 + error 态齐全。 |
| Nodes list filters and onboarding entry | Partial (was Closed) | `web/src/pages/NodesPage.tsx`, `web/src/pages/NodeOnboardingPage.tsx`. **Reassessed 2026-05-02**: onboarding 端到端可走通——`NodeOnboardingPage.tsx` 445 行覆盖 4 phase 描述 + 3 binding conflict action（confirm/reject/reset）+ Token 重新生成 + 安装步骤；`NodesPage.tsx` 含创建表单 + 监控状态切换（enter/exit-maintenance, pause/resume）+ 快速编辑标签 + 暂停 confirmation。**但筛选条严重缺失**：rules-and-interaction.md §6.3 列了 7 项（地区/城市/供应商/生命周期/监控状态/健康状态/标签 + 仅看异常），实际只有 "全部 / 绑定异常" 2 个 toggle（`NodesPage.tsx:486-501`）；亦缺 §7.4 "编辑基本信息 / 修改生命周期状态 / 重置绑定" 列表行内入口（仅详情页有）。同时 gap #10 `createNode` 绕 `lib/api.ts` (`NodesPage.tsx:59-89`) 仍在。判 Partial 反映"筛选只是壳子" + 已记技术债。 |
| Node detail operational summary and trends | Closed (verified 2026-05-02) | `web/src/pages/NodeDetailPage.tsx`. **Reassessed 2026-05-02**: 1138 行覆盖 rules-and-interaction.md §6.3 节点详情建议全部块——hero（display_name + region/city/provider + 4 状态 badge）+ 摘要 grid（健康/活跃异常/当前主问题）+ 绑定冲突处置（含 3 action）+ 标签与备注编辑 + 运行控制（4 action + pause confirmation card）+ 生命周期（退役 confirmation + 退役后只能"恢复到观察中"copy）+ 当前主机指标 4 metric-card（CPU/Load、内存/Swap、磁盘/Inode、网络/吞吐）+ 近期趋势 4 卡（含 Load5/iowait/steal Sparkline，line 1064/1079/1094）+ 当前异常 IncidentList + 事件 EventList。所有 CRUD/状态机入口齐全，长 page 文件（gap #11）但功能完整。注：未实现 v1.x.1 5-Tab 布局（line 111 已记 deferred），rules-and-interaction.md 本身未要求 Tab 化。 |
| Target list/detail and ProbeItem management | Partial (was Closed) | `web/src/pages/TargetsPage.tsx`, `web/src/pages/TargetDetailPage.tsx`. **Reassessed 2026-05-02**: detail 端 ProbeItem CRUD 完整——`TargetDetailPage.tsx` 1731 行 import `createProbeItem` / `updateProbeItem` / `deleteProbeItem` (line 12-25)；`FREQUENCY_TIER_OPTIONS` 4 档（1m/5m/15m/6h，line 65-71）与 §8.2 对齐；3 kind (tcp/http/tls) 表单 + toggle 启停 (`handleToggleProbeItem` line 923) + 删除 confirmation (line 1612)。Target 列表含 5 状态运行控制 (`targetRuntimeActions` line 117-148：启用/暂停/维护/归档/恢复) + 暂停 + 归档 confirmation。**但 TargetsPage.tsx 完全没有筛选条**：rules-and-interaction.md §6.4 列 6 项（类型/运行状态/健康状态/标签/执行节点标签/异常）全部缺失（`grep -n "filter\|筛选" TargetsPage.tsx` 仅返回数组方法）。判 Partial 反映"列表 = 只读表" 的缺口。 |
| Events advanced filters | Partial (was Closed) | `web/src/pages/EventsPage.tsx`. **Reassessed 2026-05-02**: 290 行实现 §6.5 多数筛选——object_type / severity / event_type / 数量 / created_from-to / label / 3 boolean (notification_only / recovery_only / maintenance_only) (`DEFAULT_FILTERS` line 31-42)。**但缺关键三项**：(1) 第 4 boolean "含 backfill" 未实现（observations/incidents 已支持 `is_backfilled` 但 UI 未透出对应开关）；(2) 时间 segmented（rules-and-interaction.md §6.5 隐含的"时间范围"应为 segmented 而非纯 ISO 字符串输入，当前用 placeholder `2026-04-25T00:00:00Z` 文本框，对单用户不友好）；(3) 时间分组渲染 + 加载更早分页（当前是单次 listEvents + 全量列表，无分组无翻页）。基础筛选已可走通，分组与 backfill 开关属"半成品"。 |
| Settings runtime truthfulness | Closed (verified 2026-05-02) | `web/src/pages/SettingsPage.tsx`, `internal/center/settings`. **Reassessed 2026-05-02**: 873 行覆盖 §6.6 全部 5 块——主题（`ThemeSettingsSection` Pill Tab，候风原色/经典 × 深/浅/系统）+ Telegram 通知（`telegramRuntimeManaged` 真实读 `settings.telegram.token_present` / `runtime_managed`，明确区分"持久化但未接管"vs"接管中") + 默认频率档位 + 全局默认规则 + 覆盖规则（`TargetTypeSummary` aside）+ 数据保留策略。Save 通过 form submit 触发 `handleSubmit` (line 497) 真调 `updateSettings` API；persist + runtime override 路径与 `internal/center/settings/` repo 一致（已在 Notifications 段第 71 行验证）。注：(a) 5 块用 `DetailSection` 平铺而非 Pill Tab——rules-and-interaction.md §6.6 未硬要求 Tab 布局；(b) 仅单一 "保存设置" 按钮 + `disabled.saving`，无 dirty 检测，但 `saveError/saveSuccess` 反馈完整。runtime 真实性是核心，已验证。 |
| Chinese-first UI copy and dense baseline hierarchy | Closed (verified 2026-05-02) | Alignment pass recorded in `docs/operations/v1-visual-verification.md`; frontend evidence in `web/src/app/layout/AppShell.tsx`, `web/src/components/ActionConfirmationCard.tsx`, `web/src/pages/DashboardPage.tsx`, `web/src/pages/NodesPage.tsx`, `web/src/pages/NodeDetailPage.tsx`, `web/src/pages/NodeOnboardingPage.tsx`, `web/src/pages/TargetsPage.tsx`, `web/src/pages/TargetDetailPage.tsx`, `web/src/pages/EventsPage.tsx`, and `web/src/pages/SettingsPage.tsx`. **Reassessed 2026-05-02**: 8 个 page + AppShell 抽查均以中文为主（"节点列表" / "新建节点" / "暂停监控" / "确认归档" / "进入维护" / "近期趋势" 等），仅二级 mono 数字与代码标识用英文。dense 信息呈现通过 `summary-grid` / `metric-grid` / `badge-row badge-row--wrap` / `resource-table` 实现（NodeDetailPage 8 个 DetailSection 平铺、TargetDetailPage 1731 行单页堆叠 ProbeItem 列表 + 趋势）；ActionConfirmationCard 4 维 (current/result/impact/unchanged) 文本一致中文。无空旷 SaaS 后台风格。 |
| Visual screenshot comparison against baseline PNGs | Partial | Live route screenshots were captured on 2026-04-29 under `docs/operations/visual-evidence/` and indexed by `docs/operations/visual-evidence/manifest.json`; strict visual-fidelity acceptance remains pending because the captures have not been accepted as high-fidelity matches to the frozen references |

## Notifications

| Area | Status | Evidence |
| --- | --- | --- |
| Telegram notifier implementation | Closed (verified 2026-05-02) | `internal/center/notify/telegram.go`. **Reassessed 2026-05-02**: `telegram.go:21-63` 真实 Telegram Bot API 调用——`POST {baseURL}/bot{token}/sendMessage` JSON `{chat_id, text}` + 10s `http.Client` 超时 + `Status >= 400` 时拼 body 报错；`NewTelegramNotifierWithBaseURL` 注入点便于测试。`telegram_test.go` 含 `TestTelegramNotifierPostsMessage` 验真实 HTTP path 与 chat_id/text payload + `TestTelegramNotifierReturnsErrorForBadStatus` 覆盖错误路径。**注**：实现确实是真实 HTTP 调用而非 stub，但**无重试**（错误返回给上层 `appendNotificationRecords`，被记为 `DeliveryStatusFailed`）。设计 §5.3 强调"只通知值得打断人的变化 + 不主动追溯补传"，对失败重试无要求；活跃 incident 无 retry 风险被下一轮 evaluation cycle 自然覆盖。 |
| Settings-aware notification policy | Closed (verified 2026-05-02) | `internal/center/incidents/service.go`, settings tests. **Reassessed 2026-05-02**: 三层 settings 真接入运行时——(1) `NewSettingsAwareNotifier` (service.go:72-123) 在每次 `Send` 前查 `GetPersistedTelegramSettings`：未 `RuntimeManaged` 走 fallback (env)、`RuntimeManaged && !Enabled()` 返回 `errNotificationSuppressed`、`RuntimeManaged && Enabled()` 用持久化 token+chat 临时构造 `NewTelegramNotifier`；(2) `notificationPolicyFor` (L411-427) 按 `IncidentDefaults.NotifyOnStarted/Escalated/Recovered` 在 `appendNotificationRecords` (L468-505) 决定 `shouldSend`；(3) `incidentTimingFor` (L379-409) 按 `HeartbeatIntervalSeconds/SweepIntervalSeconds` 实时影响 stale-node sweep 与心跳判定。`service_test.go` 5 个 settings 测：`TestSettingsAwareNotifierSuppresses…` / `…UsesFallbackWhen…` / `…UsesPersistedTelegramConfig` / `TestSettingsBackedHeartbeatInterval…` / `TestSettingsBackedSweepInterval…`。`bootstrap.go:88-99` 真把 `notifierSettingsRepo` 注入 `NewSettingsBackedService`，运行时设置生效路径完整。 |
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
