# Dashboard 重设计 — DataTable 迁移 + Hostname 补齐

## Goal

把 Dashboard 首页的 `AbnormalNodeList` / `AbnormalTargetList` 从自渲染的 `probe-card` 行卡迁到 `<DataTable density="compact">`，与新版节点列表风格严格统一；同时补齐 AbnormalNodeList 缺失的 node_id `<Hostname>` 显示。一次性关闭 v1-gap-checklist 中 #19 和 #20-Dashboard 部分。

**注**：研究表明 Dashboard 的 Mono 字体落地已 9/10 完成（StatTile / EventList / 两个 list 的数字时间戳全部包装齐全），唯一未包的是 AbnormalNodeList 节点身份位置的 node_id —— 本任务一并修复。

## Background

- 详见 [`research/codebase-dashboard.md`](research/codebase-dashboard.md)：现状 / 数据契约 / Mono 漂移点 grep / v2 spec 摘录
- 前置任务 `05-03-redesign-node-pages` 已建立的 v2 模式（DataTable 紧凑表 + StatusGlyph 列 + Hostname/Timestamp/MonoDigits 包装）可直接复用
- Dashboard API 数据已含所有字段（含 node_id），无后端改动需求
- v2 spec 权威：`docs/design/v2-houfeng/component-spec.md` §五 DashboardPage 段（"紧凑节点行：StatusGlyph + Hostname + 位置 + 当前问题 + Timestamp"）

## Decisions

- **Q-SCOPE 改造范围** — **2（DataTable 迁移 + Hostname 补齐）**
  - AbnormalNodeList → DataTable（紧凑 36px 行，列：StatusGlyph / 节点(Hostname+display_name) / 位置 / 当前主问题 / 心跳 / 操作）
  - AbnormalTargetList → DataTable（紧凑 36px 行，列：StatusGlyph / 目标(Hostname host:port + name) / 类型 / 当前主问题 / 最近成功 / 操作）
  - 行点击 → 导航到详情页（与 NodesPage 一致）
  - 操作列保留"查看节点 / 查看目标" link（hover 才显或常驻 ghost link，跟 NodesPage 操作列一致 hover 显）

## Requirements

### AbnormalNodeList 改造

1. 替换 `<div className="probe-list probe-list--rows">{...probe-card...}</div>` 为 `<DataTable<DashboardNodeSummary> density="compact" columns={...} rows={sorted} onRowClick={(node) => navigate(...)} />`
2. 列定义（6 列）：
   - 列 1：`<StatusGlyph state={statusGlyph(node.current_health_status)} size="md" />`（紧凑列 ~32px）
   - 列 2：节点（两行：第一行 `<Hostname truncate maxChars={14}>{node.node_id}</Hostname>` mono；第二行 display_name sans 文本）
   - 列 3：位置（`region · city · provider` sans 小字 muted）
   - 列 4：当前主问题（`<MonoDigits>{count}</MonoDigits>` + 摘要单行截断）
   - 列 5：心跳（`<Timestamp value={last_heartbeat_at} mode="relative" />` hover 显 absolute）
   - 列 6：操作（hover 才显 ghost link "查看节点"；点击 stopPropagation 防双触发）
3. 行点击 → `useNavigate()` 跳到 `/nodes/${node_id}`
4. 空态保持现有"当前没有异常节点"empty-state 文案

### AbnormalTargetList 改造

1. 同样替换为 `<DataTable<DashboardTargetSummary>>`
2. 列定义（6 列）：
   - 列 1：`<StatusGlyph state={statusGlyph(target.current_health_status)} size="md" />`
   - 列 2：目标（两行：第一行 `<Hostname>{hostPortSummary(target)}</Hostname>` mono；第二行 target.name sans）
   - 列 3：类型（`target_type` sans 小字）
   - 列 4：当前主问题（`<MonoDigits>{count}</MonoDigits>` + 摘要截断）
   - 列 5：最近成功（`<Timestamp value={last_success_at} mode="relative" />`）
   - 列 6：操作（hover ghost link "查看目标"；点击 stopPropagation）
3. 行点击 → `/targets/${target_id}`
4. 空态保持现有文案

### Mono 包装兜底

- 已有 Mono 包装全部保留
- 唯一新增：AbnormalNodeList 列 2 第一行 `<Hostname>` 包 node_id（关闭 #20-Dashboard 漂移）

### 不动

- StatTile / 5 KPI Stat strip（已 v2 对齐，零修改）
- EventList（已 v2 对齐）
- Hero panel
- DashboardPage 主容器结构 / DetailSection 编排

## Acceptance Criteria

- [ ] AbnormalNodeList 渲染为 `<DataTable density="compact">`，6 列定义符合 PRD
- [ ] AbnormalTargetList 渲染为 `<DataTable density="compact">`，6 列定义符合 PRD
- [ ] AbnormalNodeList 节点身份列显示 `<Hostname>{node.node_id}</Hostname>`
- [ ] 行点击导航到详情页（`/nodes/${id}` / `/targets/${id}`）
- [ ] 操作列 ghost link 点击 stopPropagation 不触发行导航
- [ ] 操作列默认 opacity 0，行 hover/focus-within 时显（复用 NodesPage 同款 CSS 模式）
- [ ] 空态保留（无异常节点 / 无异常目标 各自的 empty-state）
- [ ] 现有功能零回归：StatTile sparkline / EventList timeline / 加载态 / fresh-install 空态 / 错误态
- [ ] DashboardPage.test.tsx 既有断言全过；新增 ≥3 用例：AbnormalNodeList Hostname 出现 / 行点击 navigate 触发 / 操作 link stopPropagation 不导航
- [ ] `cd web && npm run lint && npm run test && npm run build` 全绿（基线 351 tests）

## Definition of Done

- TypeScript strict 0 error
- ESLint 0 warning
- vitest 全 pass，新增 ≥3 用例
- 关闭 `docs/release/v1-gap-checklist.md` gap #19 和 #20-Dashboard 部分（#20 整体仍 open，分项标注 Dashboard 已闭，Targets/Events/Settings 待后续任务）
- `docs/design/v2-houfeng/component-spec.md` §五 DashboardPage 段落如有调整同步（应该不需要，v2 spec 跟新实现已一致）
- 不需要 trellis-update-spec（这次工作是按既有 v2 模式执行，无新规范沉淀）

## Out of Scope

- 后端任何改动
- 节点 / 目标 / 事件 / 设置等其他页面（其他 follow-up 任务）
- StatTile / EventList 改动（已 v2 对齐）
- Hero panel 视觉调整
- 引入图表库 / Tailwind / styled-components
- 移动端响应式

## Technical Approach

**单 PR 实施**（任务 scope 太小不需要拆）：

1. 改 `web/src/pages/DashboardPage.tsx` 的 `AbnormalNodeList` 与 `AbnormalTargetList` 函数（保持函数名以兼容测试 selector）
2. import `DataTable` + `useNavigate`，定义 columns 配置
3. 行点击与操作列 stopPropagation：参考 NodesPage 同款写法
4. 修改 / 新增对应测试用例
5. 复用既有 CSS（`.data-table` 已落地；操作列 hover-only 模式参考 NodesPage 的 `.nodes-table__actions` 写法，新增对应 `.dashboard-table__actions` 类或复用通用类）

**关键技术点**：
- DataTable API 与 NodesPage 中用法完全一致（直接拷贝 columns config 模式）
- `statusGlyph(...)` mapper 已存在于 DashboardPage 内（私有 helper），不抽到 lib
- `hostPortSummary(target)` 同上保留私有
- Hostname truncate maxChars=14 与 NodesPage 一致
- 行点击与按钮 stopPropagation 模式：每个 cell 内部的 link 都需要 `e.stopPropagation()`

**样式**：

`web/src/styles/pages.css` 末尾追加 `.dashboard-table__actions` 段（参考 `.nodes-table__actions` 写法）：
```css
.dashboard-table__actions { opacity: 0; transition: opacity var(--dur-micro) var(--ease-calm); }
.data-table__row:hover .dashboard-table__actions,
.data-table__row:focus-within .dashboard-table__actions { opacity: 1; }
```

## Decision (ADR-lite)

**Context**：Dashboard 首页的异常节点/目标摘要当前用自渲染 `probe-card` 行卡，与 v2 节点列表风格不一致；同时 AbnormalNodeList 缺少 node_id 的 `<Hostname>` 显示，是 v1-gap-checklist #20-Dashboard 唯一显著漂移。

**Decision**：
1. AbnormalNodeList / AbnormalTargetList 迁到 `<DataTable density="compact">`，列定义与 NodesPage 一致风格但简化（少 1-2 列符合 Dashboard 摘要视图定位）
2. AbnormalNodeList 节点身份列采用 NodesPage 同款 "Hostname + display_name 两行" 模式
3. 行点击 + 操作列 hover-only 模式直接复用 NodesPage 已建立的交互范式
4. 不抽 statusGlyph / hostPortSummary 到 lib（YAGNI，私有 helper 仍合适）

**Consequences**：
- 收益：Dashboard 与节点列表风格严格统一；node_id 信息可见；hover 操作模式跨页面一致
- 取舍：DataTable 行密度比 probe-card dl 形态高（4 行 dl 信息变成 6 列单行），单行信息减少 — 但 Dashboard 是"摘要"视图，只看关键 5-6 个字段足矣
- 风险：极小 — 数据契约不变，纯 UI 重排

## Technical Notes

**关键文件**：
- 改造：`web/src/pages/DashboardPage.tsx`（382 行，主要改 AbnormalNodeList + AbnormalTargetList 两个函数）
- 改造：`web/src/pages/DashboardPage.test.tsx`（190 行，更新 selector + 新增用例）
- 调整：`web/src/styles/pages.css`（末尾追加 `.dashboard-table__actions` 段）
- 同步：`docs/release/v1-gap-checklist.md`（gap #19 closed；#20 标记 Dashboard 部分已闭）

**复用**：
- `web/src/components/atoms/DataTable.tsx`（已实装，NodesPage 已消费）
- `web/src/components/atoms/Mono.tsx`（Hostname / Timestamp / MonoDigits）
- `web/src/components/atoms/StatusGlyph.tsx`
- DashboardPage 内私有 helpers `statusGlyph()` / `hostPortSummary()` / `severityWeight()` / `statusTone()`

**设计权威**：
- `docs/design/v2-houfeng/design-language.md`（§3.2 字体强制 / §6 状态优先级 / §12 不引图表库）
- `docs/design/v2-houfeng/component-spec.md`（§五 DashboardPage 段、§二 DataTable atom）

## Research References

- [`research/codebase-dashboard.md`](research/codebase-dashboard.md) — 现状审计 / Mono 漂移点 grep / v2 缺口对照（370 行结构化报告）
