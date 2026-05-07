# PR2: Shell 计数与状态可信度

## Goal

把 AppShell / Sidebar 从“静态装饰壳”改成可信的全站状态入口：侧边栏节点/目标异常计数必须来自真实 dashboard 数据；底部状态组件不能继续伪装 center 正常或已同步，而应明确表达 Shell 摘要是否已加载、是否降级、是否无法读取。PR2 不扩后端 contract，优先复用现有 `/api/dashboard` 事实，避免夹带 PR3。

## What I Already Know

* PR1 已完成：Dashboard 首页已改成 Fleet State、全局 KPI、统一处理队列、系统入口、首次接入工作台和最近事件。
* PR1 PRD 已明确把 Shell 可信度拆成 PR2：`AppShell 不再硬编码 Sidebar anomaly counts 为 0`，`SyncStatus 必须给真实来源或降级成非健康断言`。
* 当前 `web/src/app/layout/AppShell.tsx` 写死：
  * `sync.state = 'ok'`
  * `sync.version = 'v1.0'`
  * `sync.lastSync = new Date().toISOString()`
  * `anomalyCounts = { nodes: 0, targets: 0 }`
* 当前 `Sidebar.tsx` 已有 count badge 设计，且 v2 spec 要求节点/目标 count Badge 保留但 `tone='neutral'`，因此 PR2 不需要重新设计导航结构。
* 当前 `SyncStatus.tsx` 展示三档：`ok / degraded / down`，文案是 `中心运行正常 / 中心运行降级 / 中心不可达`，还展示 `v1.0 · sync HH:mm:ss`。这些文案在没有真实 health/sync API 时容易误导。
* 当前已有 `getDashboard()` / `/api/dashboard`，返回 `total_node_count`、`total_target_count`、异常/严重/维护计数、24h 趋势、异常摘要和最近事件。
* 当前 `/api/dashboard` 没有真实 snapshot 时间、center health、shell summary、notification status 或全量 group summary。PR2 不能伪造这些事实。

## Current Problems

* 首页可以显示真实异常计数，但 Sidebar 仍显示 0，产生全站信息矛盾。
* `SyncStatus` 使用当前浏览器时间作为 `lastSync`，看起来像真实同步时间，实际只是渲染时间。
* `SyncStatus` 写死 `中心运行正常`，但没有 center health endpoint 或 heartbeat source 支撑。
* 如果 `/api/dashboard` 加载失败，AppShell 目前没有降级表达，用户只会看到静态正常状态。
* 如果用户离开 Dashboard，Shell 本身仍不能作为全局可信提示。

## Product Direction

Shell 的职责不是复制 Dashboard，也不是抢占告警语义；它应该提供轻量、可信、全局一致的状态提示：

1. 当前节点/目标是否有异常计数。
2. Shell 摘要是否来自真实 API。
3. 如果摘要不可用，明确显示“摘要不可用/降级”，不要假装系统正常。
4. 导航 count 仅用于提示，不染成告警色；具体处置仍由 Dashboard / Nodes / Targets / Events 承担。

## Proposed MVP

### 1. AppShell 拉取 Dashboard 摘要

* `AppShell` 在 authenticated user 存在时调用 `getDashboard()`。
* 使用本地 state 维护：
  * `loading`
  * `error`
  * `overview`
  * `loadedAt`
* `useEffect` 使用 `cancelled` 旗标，遵守 web state/data spec。
* 不引入 React Query / Zustand / 新 Context。
* 不新增后端 endpoint。

### 2. Sidebar anomaly counts 接真实数据

* `anomalyCounts.nodes = overview.abnormal_node_count`
* `anomalyCounts.targets = overview.abnormal_target_count`
* 初始加载或失败时可以显示 0，但 SyncStatus 必须说明摘要加载中或不可用，避免把 0 误读成无异常。
* Badge 保持 `tone="neutral"`，符合 v2 Sidebar 契约。

### 3. SyncStatus 改成 Shell 摘要状态

在没有真实 health/sync contract 前，`SyncStatus` 不得再写死 `中心运行正常`。

建议状态映射：

* loading：`state="degraded"`，label `正在读取系统摘要`，meta `v1.0 · dashboard loading`
* success 且有严重异常：`state="degraded"`，label `摘要已加载 · 存在严重异常`
* success 且无严重但有异常：`state="degraded"`，label `摘要已加载 · 存在活跃异常`
* success 且无异常：`state="ok"`，label `摘要已加载`
* error：`state="down"`，label `摘要不可用`

Meta 行不再使用假的 `sync HH:mm:ss` 文案，改为能被事实支撑的 `dashboard HH:mm:ss` 或 `dashboard unavailable`。

### 4. Tests

* `AppShell.test.tsx`
  * authenticated 时请求 `/api/dashboard`。
  * 成功后 Sidebar 显示节点/目标异常 count。
  * 加载或失败时不显示假的“中心运行正常”。
  * 失败时显示 `摘要不可用`。
* `SyncStatus.test.tsx`
  * 覆盖自定义 label/meta。
  * 保留三档 state 的基础样式契约。
* `Sidebar.test.tsx`
  * 继续证明 count badge 只出现在节点/目标，并且 count 为 0 时不显示。

## Acceptance Criteria

* [x] AppShell 不再硬编码 `anomalyCounts={{ nodes: 0, targets: 0 }}`。
* [x] Sidebar 节点/目标 count 来自真实 `/api/dashboard` 的异常计数。
* [x] AppShell 不再写死 `sync.state='ok'` 和 `lastSync=new Date().toISOString()` 来暗示 center 健康/同步正常。
* [x] `/api/dashboard` 加载失败时，Shell 明确显示摘要不可用或降级状态。
* [x] 初始加载时，Shell 明确显示正在读取摘要，避免把 0 count 误读为无异常。
* [x] 不新增后端 endpoint，不扩展 `DashboardOverview` contract，不展示 PR3 才能支撑的事实。
* [x] 不引入 React Query / Zustand / Redux 或新的 CSS 体系。
* [x] AppShell / Sidebar / SyncStatus tests 覆盖成功、加载、失败和 count 行为。

## PR2 Completion Evidence

Implemented in:
* `web/src/app/layout/AppShell.tsx`
* `web/src/app/layout/SyncStatus.tsx`
* `web/src/app/layout/AppShell.test.tsx`
* `web/src/app/layout/Sidebar.test.tsx`
* `web/src/app/layout/SyncStatus.test.tsx`
* `docs/design/v2-houfeng/component-spec.md`
* `.trellis/spec/web/state-and-data.md`

Verification:
* `cd web && TMPDIR=/tmp npm run test -- --run src/app/layout/AppShell.test.tsx src/app/layout/Sidebar.test.tsx src/app/layout/SyncStatus.test.tsx` passed: 16 tests.
* `cd web && TMPDIR=/tmp npm run lint` passed.
* `cd web && TMPDIR=/tmp npm run build` passed; Vite reported the existing large chunk warning only.
* `git diff --check` passed before PRD/spec completion updates; rerun required before commit.

## Definition of Done

* PRD 完整，implement/check jsonl 注入相关 web spec 与 PR1 归档 PRD。
* 代码实现后运行：
  * `cd web && TMPDIR=/tmp npm run test -- --run src/app/layout/AppShell.test.tsx src/app/layout/Sidebar.test.tsx src/app/layout/SyncStatus.test.tsx`
  * `cd web && TMPDIR=/tmp npm run lint`
  * `cd web && TMPDIR=/tmp npm run build`
* 如发现 v2 component spec 对 SyncStatus 文案仍诱导假健康，更新 `docs/design/v2-houfeng/component-spec.md`。
* 如沉淀新规则，更新 `.trellis/spec/web/state-and-data.md` 或 `component-conventions.md`。

## Out of Scope

* 新增 shell summary endpoint。
* 扩展 `/api/dashboard` 字段。
* 真实 center heartbeat / uptime / sync batch health。
* 通知配置状态、真实 snapshot 时间、全量 group summary。
* Sidebar 告警色、红点或复杂通知中心。
* URL-state 深链筛选（留给 PR4）。

## Technical Notes

* 主要文件：
  * `web/src/app/layout/AppShell.tsx`
  * `web/src/app/layout/Sidebar.tsx`
  * `web/src/app/layout/SyncStatus.tsx`
  * `web/src/app/layout/AppShell.test.tsx`
  * `web/src/app/layout/Sidebar.test.tsx`
  * `web/src/app/layout/SyncStatus.test.tsx`
* 数据来源：
  * `web/src/lib/api.ts` 的 `getDashboard()`
  * `web/src/lib/types.ts` 的 `DashboardOverview`
* 设计依据：
  * `docs/design/v2-houfeng/component-spec.md` 的 Sidebar / SyncStatus / AppShell 契约。
  * PR1 归档 PRD 的 PR2 计划。
  * `.trellis/spec/web/state-and-data.md` 的 Dashboard 数据可信度规则。
