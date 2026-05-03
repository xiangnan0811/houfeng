# 重设计节点列表与详情页

## Goal

把节点列表（NodesPage）和节点详情（NodeDetailPage）从"低密度数字阵列"重做成"engineering tool 级的高密度趋势化视图"。

**核心矛盾**：后端已有 24h × 5min 步长的全量主机时序数据（25 个指标），前端却只用 3 个画了 sparkline，其余仍是单点数字 → 用户无法一眼掌握节点趋势，达不到"管理节点"的目的。

## Background

- 三个页面源码已审计，详见 [`research/codebase-frontend.md`](research/codebase-frontend.md)
- 后端时序就绪：`/api/nodes/{id}/runtime-facts` 返回 `recent_host_samples`（24h × 5min ≈ 288 个 HostSample，25 字段全有）
- 共享原子已就绪：`Sparkline` / `StatusGlyph` / `TrendArrow` / `DataTable` / `MonoDigits` / `Hostname` / `Timestamp`，**但节点页面基本没用**
- v2 设计规范（`docs/design/v2-houfeng/component-spec.md` §5）**已经规约了目标形态**，本任务本质是"v2 spec 实施 + 漂移收口"
- 设计语言硬约束（§12）：**不引入图表库**，所有图必须用纯 SVG

## Decisions (resolved Open Questions)

- **Q1 列表页趋势可视化** — D：列表页**不画**趋势，只在详情页画。理由：单用户场景节点数 ≤ 几十，列表主价值是密度提升与 DataTable 改造。
- **Q2 详情页"近期趋势"区块** — A：删掉整个区块（与"当前主机指标"卡数据冗余）。元信息（24h 样本数 / 最早最新观测 / 是否含 backfill）合并到"当前主机指标" section 的 aside 头部一行 mono 文案。
- **Q3 接入工作台** — B：拆 follow-up 任务（4-phase stepper / token 倒计时 / 安装步骤模板替换），完成本任务时同步登记到 `docs/release/v1-gap-checklist.md`。
- **Q4 Mono 字体包装** — A：本次在节点列表 + 详情页全面补齐 `MonoDigits` / `Hostname` / `Timestamp`。其他页面的漂移作为独立 gap 跟踪。
- **Q-CHART 图表库约束** — 保留纯 SVG。实现边界：「label · 当前值 · 中型 sparkline 240×60 · hover 显示该时刻数值与时间」，不做完整 X/Y 轴标尺、不做十字线、不做时间窗口选择 UI（架构层面预留 series 长度可变，便于未来加切换器）。

## Requirements

### NodesPage（节点列表）

1. 改用 `<DataTable density="compact">`（36px 行高），列定义：
   - `[StatusGlyph 健康态, 节点(Hostname + display_name 两行), 位置(region/city/provider), 标签, 当前主问题(数+摘要), 心跳(Timestamp relative + hover absolute), 操作]`
2. 操作列 ghost 按钮**仅 hover 才显**（"快速编辑标签 / 进入维护 / 暂停监控"），不再常驻
3. 行点击 → 导航到节点详情页
4. 视图切换 `[全部节点 N | 绑定异常 M]` 改用 segmented control（不再是占整个 hero 的大卡）
5. 「新建节点」按钮位于 section heading 右侧，不再是占整行宽度的 ghost button
6. 创建节点表单收到可折叠 page-panel
7. 筛选栏保留所有现有筛选项（地区/城市/供应商/生命周期/运行状态/健康状态/标签/仅看异常）
8. 所有时间戳用 `<Timestamp>`、所有节点 ID 用 `<Hostname>`、所有数字用 `<MonoDigits>`
9. 行内编辑标签 / 备注的入口保留（点"快速编辑标签"按钮触发现有 inline editor）

### NodeDetailPage（节点详情）

1. **删除"近期趋势"整个 section**（NodeTrendCards.tsx 移除使用，但组件文件保留以防回归测试）
2. **重构"当前主机指标" section**（NodeHostMetrics.tsx）：4 张 metric-card 仍按 CPU/Load、内存/Swap、磁盘/Inode、网络/吞吐 分组，每张卡内部布局：
   ```
   ┌─────────────────────────────────────┐
   │ 卡标题 (sans 13px)                   │
   ├─────────────────────────────────────┤
   │ 主指标 label (eyebrow) · 当前值 (MonoDigits 大字)│
   │ ┌─────────────────────────────┐     │
   │ │   中型 sparkline 240×60      │     │
   │ └─────────────────────────────┘     │
   │ 次指标 1 label · MonoDigits          │
   │ 次指标 2 label · MonoDigits          │
   │ ...                                 │
   └─────────────────────────────────────┘
   ```
   - 每张卡的"主指标"画 sparkline（CPU 卡画 cpu_usage_pct、内存卡画 mem_used_pct、磁盘卡画 disk_used_pct、网络卡画 net_in_bytes_per_sec + net_out_bytes_per_sec 双线）
   - 次指标只显数字，不画图
3. section 头部 aside 显示采样元信息（mono）：`24h 样本 N · 最早 YYYY/MM/DD HH:mm · 最新 YYYY/MM/DD HH:mm` + 含 backfill 时追加 `· backfill M`
4. 其他 section 保持顺序：Hero / 绑定冲突 / 标签备注 / 运行控制 / 生命周期 / **当前主机指标(增强)** / 当前异常 / 事件
5. 节点 ID、所有时间戳、所有数字度量全部用 mono 包装

### Sparkline 增强（共享原子）

1. 新增 `interactive?: boolean` prop（默认 false 保持向后兼容）
2. interactive 模式：mouse hover sparkline → 出现垂直 hairline + tooltip 显示 `<时间戳> · <值>`，移出消失
3. 新增 `samples?: { value: number, observedAt: string }[]` prop（可选）替代 `values: number[]`：当传 samples 时 hover tooltip 能显示具体时间戳；不传则只显示值
4. **边界态明确化**：
   - `values.length === 0` → 渲染 "暂无数据" placeholder（现有空态升级，加文案）
   - `values.length === 1` → 不画线，只画末点 + "样本不足" 灰色微提示
   - 中间数据缺口 → 不做断线处理，直接连接（缺口本身是采样问题不是指标问题）
   - backfill 数据 → 不做视觉区分（backfill 也是真实数据）
   - 维护状态下整张 metric-card 加 maintenance ribbon，sparkline 本身保持原色

### Mono 字体落地

- 节点列表 + 详情页内：
  - 数字 / 百分比 / 字节单位 / 毫秒 / uptime → `<MonoDigits>` (`tabular-nums`)
  - 节点 ID (`nd_xxx`) / hostname / IP / fingerprint / agent_version → `<Hostname>`
  - 所有绝对时间戳 → `<Timestamp value={iso} mode="both">` (relative + hover absolute)
- 已有 formatter (`formatPercent` 等) 不变，只在调用点包 wrapper

## Acceptance Criteria

- [ ] NodesPage 完成 DataTable 改造，行高 36px，操作按钮 hover 才显
- [ ] NodesPage 视图切换是 segmented control，不再是大 hero 卡
- [ ] NodesPage 所有数字 / 时间戳 / 节点 ID 用 mono 包装
- [ ] NodeDetailPage 删除"近期趋势" section
- [ ] NodeDetailPage 主机指标 4 卡每张含主指标 sparkline + hover tooltip
- [ ] NodeDetailPage 主机指标 section 头部 aside 显示采样元信息
- [ ] NodeDetailPage 所有数字 / 时间戳 / 节点 ID 用 mono 包装
- [ ] `<Sparkline>` 支持 `interactive` prop + 边界态（无样本 / 单样本 / 维护态）
- [ ] 现有功能零回归：行内编辑标签/备注、维护/暂停/退役/恢复操作、绑定冲突处置、节点创建表单
- [ ] 单元测试覆盖关键交互：DataTable 行 hover 显操作按钮、Sparkline interactive hover 显 tooltip、边界态文案
- [ ] `make verify-go`（应无影响）+ `cd web && npm run lint && npm run test && npm run build` 全绿

## Definition of Done

- TypeScript 严格模式无错误
- ESLint 无 warning
- 现有 `*.test.tsx` 全部通过；新增交互的 vitest 用例覆盖
- `docs/design/v2-houfeng/component-spec.md` §五的 NodeDetailPage 段落如发生偏离，同步更新（"近期趋势" 4 卡改为说明已合并到主机指标）
- `docs/release/v1-gap-checklist.md` 登记 follow-up：（a）接入工作台改造（b）TargetsPage 镜像改造（c）Dashboard 节点概览块对齐（d）其他页面 mono 字体落地

## Out of Scope (explicit)

- 接入工作台（NodeOnboardingPage）改造 — 拆 follow-up 任务
- TargetsPage / TargetDetailPage 镜像改造 — 拆 follow-up
- Dashboard 节点概览块同步 — 视觉差异不大，暂不动
- 后端 API 形状变更（不动 `/api/nodes`、不动 `/runtime-facts`、不动 retention）
- 引入图表库（recharts/echarts/visx）
- 时间窗口切换 UI（24h ↔ 7d）— 仅在组件 prop 层预留可变 series 长度
- 事件标记叠加在趋势线上 — 不预留接口
- 移动端响应式
- 国际化

## Technical Approach

**实现路径（小 PR 拆分）**：

- **PR1：Sparkline 增强 + 节点详情页主机指标改造**
  - 升级 `web/src/components/atoms/Sparkline.tsx`：加 `interactive` / `samples` prop + 边界态
  - 重构 `web/src/components/node-detail/NodeHostMetrics.tsx`：4 卡 + 每卡主指标 sparkline + section aside 元信息
  - 删除 `NodeDetailPage` 对 `NodeTrendCards` 的引用（组件文件暂留）
  - 节点详情页内 Mono 包装落地
  - 单元测试：sparkline 边界 + tooltip + host metrics 渲染

- **PR2：NodesPage DataTable 改造**
  - NodesPage 表格部分迁移到 `<DataTable>`
  - 视图切换改 segmented control
  - 操作列 hover 才显
  - 节点列表内 Mono 包装落地
  - 单元测试：行 hover 操作按钮、segmented control 切换、列渲染

- **PR3：清理与文档同步**
  - 删除 `NodeTrendCards.tsx` 与对应测试（确认无引用）
  - `component-spec.md` 同步
  - `v1-gap-checklist.md` 登记 follow-up
  - 全量 `npm run build` 验证

> 注：实际是否拆 3 个 PR 取决于后续实施时的验证粒度，最终可能合 1 个 PR 提交。

**关键技术点**：

- Sparkline 240×60 内部坐标系：viewBox 设计要让 hover 命中区域宽 = 整张 sparkline 宽，鼠标 X 坐标除以 step 得 index → 显示 samples[index]
- Tooltip 定位用 absolute + transform: translate(-50%, -100%)，避免溢出 metric-card 边界（必要时改 portal 但优先尝试 inline）
- DataTable 操作列 hover：用 CSS `:hover` + opacity 而非 React state（避免每行 re-render）
- Mono 包装是渐进式：先包关键列，发现遗漏 grep `formatPercent\|formatBytes\|formatDateTime` 调用补

## Technical Notes

**关键文件**：
- `web/src/pages/NodesPage.tsx` — 列表页主体
- `web/src/pages/NodeDetailPage.tsx` — 详情页主体（删 NodeTrendCards 引用）
- `web/src/components/node-detail/NodeHostMetrics.tsx` — 主机指标卡（核心改造）
- `web/src/components/node-detail/NodeTrendCards.tsx` — 待删除
- `web/src/components/atoms/Sparkline.tsx` — 增强 interactive
- `web/src/components/atoms/Mono.tsx` — 已存在，开始消费
- `web/src/components/atoms/DataTable.tsx` — 已存在，NodesPage 开始消费

**测试文件**：
- `web/src/pages/NodesPage.test.tsx`
- `web/src/pages/NodeDetailPage.test.tsx`
- `web/src/components/node-detail/NodeHostMetrics.test.tsx`
- `web/src/components/atoms/Sparkline.test.tsx`（如不存在则新增）

**设计权威**：
- `docs/design/v2-houfeng/design-language.md`（§3.2 字体强制 / §4 密度 / §6 状态 / §12 不做的事）
- `docs/design/v2-houfeng/component-spec.md`（§五的 NodesPage / NodeDetailPage 模板）

**业务约束**：
- `docs/design/v1-baseline/architecture-data-model.md`（节点 = 服务器、健康状态派生）
- `docs/design/v1-baseline/rules-and-interaction.md`（生命周期 / 健康态 / 维护语义）

**后端契约（不变）**：
- `internal/center/http/handlers/runtime_facts.go` — `/runtime-facts` handler
- `internal/center/store/runtime_facts.go` — SQL `limit 288` (24h)

## Research References

- [`research/codebase-frontend.md`](research/codebase-frontend.md) — 现状审计、数据契约、v2 漂移点清单（含 8 项 P0/P1 漂移）

## Decision (ADR-lite)

**Context**：候风 V1 实现期，节点页面被用户判定"信息密度不够、看不到趋势、布局松散"。审计后发现这是"v2 设计规范实施漂移"问题，而不是设计本身的问题：所有需要的原子（Sparkline / DataTable / Mono）和数据（24h × 5min × 25 指标）都已就绪，只是没用。

**Decision**：本任务定位为"v2 spec 漂移收口 + sparkline 交互增强"，不另起设计；范围聚焦节点列表与详情两个高频页面，接入工作台拆 follow-up；保留"不引入图表库"约束（先看效果再决定是否松动）；列表页不画趋势（节点数小、详情页消费足矣）。

**Consequences**：
- 收益：实现路径清晰、设计争议极小、用户能在 1-2 个 PR 内看到效果
- 取舍：sparkline 没有完整坐标轴 / 时间窗口切换，用户读不出"3 小时前那个尖峰是多少"（hover 可读但不能精确选时刻）
- 风险：mono 字体大规模落地可能在某些 narrow flex 容器下挤压视觉（须 lint 阶段 grep 排查）；Sparkline 的 hover hit-area 在 240×60 范围内可能误触（须 vitest 验证）
- 未来：本次完成后再评审是否引入 visx；TargetsPage 镜像改造与接入工作台改造已登记到 v1-gap-checklist
