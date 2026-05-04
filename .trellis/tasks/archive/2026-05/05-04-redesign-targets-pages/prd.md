# 重设计目标列表与详情页（镜像 v2 节点页面）

## Goal

把 TargetsPage（985 行）+ TargetDetailPage（1127 行）从"自渲 .resource-table + 卡片栈 + 数字阵 + 0 Mono"改造为 "DataTable 紧凑 36px + Mono 全栈 + ProbeItem 卡内嵌 observation DataTable + LatencyTrends 含 Sparkline"，对齐 v2 节点页面已实施形态。一次性关闭 v1-gap-checklist #18 与 #20-Targets 部分。

## Background

- 详见 [`research/codebase-targets.md`](research/codebase-targets.md)：现状审计、12+ Mono 漂移点、ProbeItem 业务模型、改造体量预估
- 前置任务直接复用：
  - `05-03-redesign-node-pages`：NodesPage DataTable + NodeHostMetrics Sparkline interactive + Mono 全栈
  - `05-03-redesign-node-onboarding`：ActionConfirmationCard 两步式
  - `05-04-redesign-dashboard`：Dashboard DataTable 迁移（hover 操作模式可直接 copy）
- 后端契约不变：`GET /api/targets` / `GET /api/targets/{id}` / `GET /api/targets/{id}/runtime-facts` / `GET /api/targets/{id}/probes`
- v2 设计权威（仅简短）：`docs/design/v2-houfeng/component-spec.md` §五 — "TargetsPage / TargetDetailPage 镜像 NodesPage / NodeDetailPage 的 DataTable + 详情结构；列略不同：[StatusGlyph, 名字, 类型, host, 标签, 状态, 操作]"
- 业务约束：`docs/design/v1-baseline/architecture-data-model.md`（Target / ProbeItem 模型；TCP/HTTP/TLS 三种 ProbeKind；frequency_tier 4 档），`rules-and-interaction.md` §6.4 / §7.2 / §7.3 / §8 / §9

## Decisions (resolved Open Questions)

- **Q-PROBE-FORM ProbeItem 列表形态** — **3（混合）**：ProbeItem 仍卡片栈（适合 TCP/HTTP/TLS 多样配置）；每卡内嵌 observation-list 改为 `<DataTable density="compact">` 36px。
- **Q-SPARKLINE Latency Sparkline** — **2**：每 probe_item 一张 metric-card 含 Sparkline interactive 240×60（hover tooltip 显时间+延迟），与 NodeHostMetrics 风格一致。
- **Q-PR-SPLIT** — **1（3-PR 拆）**：PR1 = TargetsPage / PR2 = TargetDetailPage Mono+ProbeList / PR3 = LatencyTrends + 文档同步 + 关 gap。

## Requirements

### PR1：TargetsPage DataTable 迁移 + Mono 全栈

1. 替换 `<div className="resource-table">` 自渲为 `<DataTable<TargetRecord> density="compact">`
2. 列定义（7 列，按 v2 spec §五"[StatusGlyph, 名字, 类型, host, 标签, 状态, 操作]"）：
   - 列 1：`<StatusGlyph state={targetGlyphState(target)} size="md" />`（mapper 同 nodeGlyphState 风格，maintenance/暂停/已归档 outrank 健康）
   - 列 2：目标（两行：第一行 `<Hostname>{target.target_id}</Hostname>`；第二行 target.name + target_type 小字）
   - 列 3：类型（`target.target_type`）
   - 列 4：Host（`target.host` + base_port，用 `<Hostname>` 包）
   - 列 5：标签（labels 截断 3 + overflow `+N`，复用 NodesPage 同款；执行节点标签也展示）
   - 列 6：状态（run_status + health StatusBadge 组）
   - 列 7：操作（hover 才显 ghost button："快速编辑标签 / 进入维护 / 暂停监控 / 归档" 等条件性显示）
3. 行点击 → `useNavigate()` 跳 `/targets/${target_id}`
4. 操作列内 button / link / input 全部 `e.stopPropagation()` 防误导航
5. 操作列 hover-only：CSS opacity 模式（`.targets-table__actions` 复用 `.nodes-table__actions` / `.dashboard-table__actions` 样式族）
6. 视图切换：保留无切换（target 无"绑定异常"等子视图概念）
7. 创建表单：可折叠 page-panel 保留，只把现有 inline 按钮换成 section heading 右侧 primary "新建目标"按钮
8. 筛选栏：6 项保留全部
9. Mono 全栈包装：所有 `formatDateTime` → `<Timestamp>`，`formatNumber/formatLatency` → `<MonoDigits>`，`target_id / host` → `<Hostname>`

### PR2：TargetDetailPage Mono + ProbeList 内嵌 observation DataTable

1. **TargetHero**：4 个 hero-meta-card 时间戳改用 `<Timestamp mode="both">`；标签 / 执行节点标签视情况包 `<MonoDigits>` 或保留普通展示
2. **TargetStatusSummary**：3 KPI 数字 `<MonoDigits>` 包装
3. **TargetLabelsAndNote**：保持现有 inline 编辑流程；只把显示态时间戳 Mono 包装
4. **TargetRuntimeControls**：保持，只 Mono 化
5. **TargetProbeList 改造（核心）**：
   - 每 probe-card 保留 header 结构（`<h3>{probe_kind.toUpperCase()}</h3>` + config 摘要 + Badge 行 + 操作 button 行）
   - 卡内 meta dl 时间戳 `<Timestamp>` 包装
   - **observation-list 改造**：替换 `<div className="observation-list">{...<div className="observation-row">...}</div>` 为 `<DataTable density="compact">`
     - 列：`[StatusGlyph result_kind | node (Hostname) | 时间 (Timestamp relative) | 延迟 (MonoDigits + 'ms') | HTTP/TLS (MonoDigits) | 错误摘要 (mono 截断)]`
     - 6 列，紧凑 36px 行高
   - 0 ProbeItem 时空态："尚未添加 ProbeItem" + ghost button "添加第一个 Probe" 跳到 ProbeItem 创建 form
   - 0 observations 时（probe 刚启用）：DataTable empty 空态显"尚未收到观测"
6. **ProbeItem 编辑 form 中的 Mono 包装**（如 form 含时间戳 / 数字显示）：grep 现有 form 实现，发现时间戳 / 数字裸渲处补 Mono；如 form 是纯配置无时间戳，则零工作量

### PR3：TargetLatencyTrends Sparkline 重构 + 文档同步 + 关 gap

1. **TargetLatencyTrends 改造**：
   - 替换原"6-7 个数字阵"为 metric-card grid，每 probe_item 一张卡：
   ```
   ┌───────────────────────────────────────┐
   │ <kind> · <config 摘要>                │
   │ 当前延迟: <MonoDigits>11 ms</MonoDigits>│
   │ ┌─────────────────────────────┐       │
   │ │   Sparkline 240×60 interactive │     │
   │ └─────────────────────────────┘       │
   │ 平均: 12.4 ms · 最大: 89 ms · 样本数: 288 │
   │ 覆盖 3 节点 · 24h 窗口                 │
   └───────────────────────────────────────┘
   ```
   - Sparkline samples 派生：`recent_probe_observations.filter(o => o.probe_item_id === ... && o.latency_ms != null).map(o => ({ value: o.latency_ms, observedAt: o.observed_at }))`
   - 稀疏采样下 Sparkline 已有 single-sample 边界态
   - section aside 显示采样元信息：`24h N 样本 · 最早 ... · 最新 ...`（参照 NodeHostMetrics aside meta）
   - 维护态 target → 整张 section 加 `ribbon="maintenance"`
2. **文档同步**：
   - `docs/design/v2-houfeng/component-spec.md` §五 TargetsPage / TargetDetailPage 段落细化（spec 当前太简短，本次实施细则补完）
   - 关闭 `docs/release/v1-gap-checklist.md` gap #18，附实施摘要；#20 追注 Targets 部分已闭

### Mono 包装兜底（贯穿 PR1-PR3）

- 节点 / 目标 ID（`nd_xxx` `tg_xxx`）、host、fingerprint → `<Hostname truncate maxChars=14>`
- 所有时间戳（`*_at` 字段）→ `<Timestamp value={iso} mode="relative|absolute|both">`
- 所有数字（latency_ms、http_status、tls_expiry_days、count、attempt_count、incident count）→ `<MonoDigits>`

## Acceptance Criteria

### 通用
- [ ] 现有功能零回归：创建表单、行内编辑标签、运行控制（启用/暂停/维护/归档/恢复）、ProbeItem 编辑/启用/删除、绑定状态机、IncidentList、EventList
- [ ] `cd web && npm run lint && npm run test && npm run build` 全绿（基线 354 tests）
- [ ] `make verify-go` 全绿（前端改动应不影响后端）

### PR1 TargetsPage
- [ ] 渲染为 `<DataTable density="compact">` 36px 行高，7 列符合 PRD
- [ ] 行首列 `<StatusGlyph>` 按 `targetGlyphState()` mapper
- [ ] 节点 ID / host / 时间戳 / 数字全部 mono 包装
- [ ] 行点击导航到 `/targets/${id}`，操作按钮 stopPropagation
- [ ] 操作按钮默认 opacity 0，行 hover/focus-within 时显
- [ ] 创建表单可折叠，"新建目标" primary button 在 section heading 右侧
- [ ] 筛选栏所有 6 项保留功能
- [ ] 新增 ≥3 测试用例：行点击导航 / stopPropagation / 筛选切换

### PR2 TargetDetailPage Mono + ProbeList
- [ ] TargetHero / StatusSummary / LabelsAndNote / RuntimeControls Mono 全栈
- [ ] TargetProbeList 每卡内嵌 observation DataTable（紧凑 36px，6 列）
- [ ] 0 ProbeItem / 0 observations 空态文案到位
- [ ] ProbeItem 编辑 form Mono 包装兜底
- [ ] 新增 ≥3 测试用例：observation DataTable 渲染、空 ProbeItem 空态、空 observations 空态

### PR3 LatencyTrends + 文档
- [ ] 每 probe_item 一张 metric-card 含 Sparkline 240×60 interactive
- [ ] Sparkline hover tooltip 显时间 + latency_ms（formatLatency）
- [ ] section aside 显采样元信息
- [ ] 维护态 ribbon
- [ ] component-spec.md §五 TargetsPage / TargetDetailPage 段细化
- [ ] v1-gap-checklist.md gap #18 关闭；#20 追注 Targets 部分已闭
- [ ] 新增 ≥3 测试用例：sparkline 渲染、hover tooltip、空数据 placeholder

## Definition of Done

- TypeScript strict 0 error
- ESLint 0 warning
- vitest 全 pass
- spec / 文档同步完成
- 不需要 trellis-update-spec（按既有 v2 模式执行）

## Out of Scope

- 后端任何改动（不改 API 形态、不改 retention）
- 引入图表库（仅纯 SVG Sparkline）
- ProbeItem 创建表单视觉重做（仅 Mono 包装）
- TargetCreate 引导用户挂 Probe（单独任务）
- 未来扩展：Probe 成功率 / 连续失败 聚合指标 / 时间窗口切换 / 多节点延迟对比
- 移动端响应式 / i18n
- 节点页面 / Dashboard / Onboarding 改动（已完成）
- Events / Settings 页 Mono 字体（剩余 #20 部分由后续单独任务）

## Technical Approach

3-PR 拆分按 Decisions Q-PR-SPLIT。每 PR 独立可验证、独立 commit。

**关键技术点**：
- DataTable API 与 columns 配置已在 NodesPage / DashboardPage 多处消费，直接复用模式
- targetGlyphState() mapper 仿 nodeGlyphState：维护中 / 暂停 / 已归档 outrank 健康
- recent_probe_observations 按 probe_item_id group → SparklineSample[]：纯前端逻辑，参考 NodeHostMetrics 的 toAscending + toSeries 函数
- ProbeItem 卡内嵌 DataTable：DataTable 不依赖任何 page-level 状态，可任意嵌套
- 操作列 hover-only：CSS-only（`.targets-table__actions { opacity: 0 } / .data-table__row:hover ... { opacity: 1 }`）
- ProbeItem 编辑 form：grep "<input" / "formatDateTime" 看是否有 mono 漂移点

## Decision (ADR-lite)

**Context**：TargetsPage / TargetDetailPage 是 v1-gap-checklist 中体量最大的 UI 改造遗留项（共 2112 行）。前置任务（节点页面 / 接入工作台 / Dashboard）已建立完整 v2 模式，可直接复用 80%+ 模式。

**Decision**：
1. 列表完全镜像 NodesPage（DataTable 紧凑 36px + StatusGlyph + Mono 全栈 + hover 操作）
2. ProbeList **不**整体表格化（保留卡片，因为 TCP/HTTP/TLS 配置多样）；observation-list 在卡内**用 DataTable 表达**（同质多节点观测数据天然适合表格）
3. 引入 Sparkline 替代 LatencyTrends 数字阵（每 probe_item 一张卡，跟 NodeHostMetrics 风格严格一致）
4. 3-PR 拆分（与节点页面任务节奏一致）

**Consequences**：
- 收益：v2 一致性大幅提升；用户在 target 详情页能看到延迟趋势（之前只看数字）；observation-list 表格化让多节点观测对齐查看更顺
- 取舍：稀疏采样（6h frequency 24h 仅 4 点）下 sparkline 信息量低，但 placeholder 已就绪；ProbeItem 卡片 + 内嵌表的两层视觉层级稍复杂（但用户已熟悉 NodeHostMetrics 类似模式）
- 风险：体量较大（3 PR），但有节点页面成熟模式做参考，风险面小

## Technical Notes

**关键文件**：
- 改造：`web/src/pages/TargetsPage.tsx`（985 行）/ `TargetDetailPage.tsx`（1127 行）
- 改造：`web/src/components/target-detail/*.tsx` 8 个子组件
- 复用：`web/src/components/atoms/{DataTable,Sparkline,Mono,StatusGlyph}.tsx`
- 同步：`docs/design/v2-houfeng/component-spec.md` §五 + `docs/release/v1-gap-checklist.md` #18 / #20

**测试**：
- 现有：TargetsPage / TargetDetailPage / 8 子组件可能有零或少测试 — 本任务每 PR 至少新增 ≥3 用例
- 复用：parametric vitest pattern 参考 NodesPage.test.tsx / NodeDetailPage.test.tsx

**设计权威**：
- `docs/design/v2-houfeng/design-language.md`（§3.2 字体强制 / §6 状态优先级 / §7 三态 / §12 不引图表库）
- `docs/design/v2-houfeng/component-spec.md`（§五，本任务做局部细化）

**业务约束**：
- `docs/design/v1-baseline/architecture-data-model.md`
- `docs/design/v1-baseline/rules-and-interaction.md`（§6.4 目标列表字段建议 / §7.2 Target 创建 / §7.3 ProbeItem 创建）

## Research References

- [`research/codebase-targets.md`](research/codebase-targets.md) — 现状 / 数据契约 / 缺口对照 / 改造体量预估
