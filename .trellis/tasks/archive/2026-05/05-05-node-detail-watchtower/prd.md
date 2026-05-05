# 重设计节点详情页 watchtower 视图

## Goal

把 NodeDetailPage 从"8 个 DetailSection 纵向堆叠 / 装饰多 / 趋势是配角"重做成 ops-first 的 watchtower 视图：**身份条紧凑 + 异常前置 + 8 张时序大图为主体 + 次要折叠 + 历史抽屉**。让用户进页面就能"整体掌握一台服务器现在到底怎么样"。

**战略转向**：从"候风美学优先"调整为"监控工具优先"。设计语言原料保留（mono / StatusGlyph / DataTable / 暗色 / 状态色），但信息架构与视觉层级重排，砍装饰。

## Background

- **战略反馈**（用户 2026-05-05）："本系统首先是个服务器管理监控系统……我自己观测服务器状态都非常困难，很难从整体上掌握一个服务器的综合情况……这是本系统目前最大的缺陷。"
- 用户与 main 对齐 5 卡点诊断（A 缺中央视图 / B 异常被埋 / C 跨服务器对比难 / D 趋势是装饰 / E 视觉装饰偷密度）
- 实施顺序（用户确认）：1️⃣ **节点详情 watchtower（本任务）** → 2️⃣ 节点列表对齐新模板 → 3️⃣ Dashboard 节点概览块再调
- 后端契约基本不变（仅 `/api/incidents` 加 `include_resolved` 参数）：`/api/nodes/{id}` + `/api/nodes/{id}/runtime-facts`（24h × 5min ≈ 288 个 HostSample）+ `/api/events?object_type=node&object_id=X` + `/api/incidents?object_type=node&object_id=X[&include_resolved=true]`

## Decisions (resolved Open Questions)

### 用例优先级（已对齐）
1. "这台机器现在出问题了吗？严重吗？" — 现状 / 当前异常
2. "问题什么时候开始的？还在恶化还是稳住了？" — 趋势 / 时间线
3. "过去 24h / 7d 这台机器整体稳不稳？" — 长期健康度
4. "我要维护 / 暂停 / 退役，按钮在哪？" — 操作

### 整体轮廓（已对齐 5 块）

```
┌─────────────────────────────────────────────────────────────┐
│ ① 身份条（升级为 2 行）                                       │
│   行 1：display_name (大字) · 4 状态 badge · 数据新鲜度       │
│        (last_heartbeat_at relative + uptime)                 │
│   行 2：mono 元数据条 — node_id · 位置 · 供应商 · 标签 ·     │
│        agent_version (小字 muted)                            │
│   右上 sticky："…" 操作菜单 + "查看历史" 按钮                │
├─────────────────────────────────────────────────────────────┤
│ ② 危险区前置（条件性 — 仅 active_incident_count > 0 显示）   │
│   绛红 ribbon + 大字摘要 + 持续时长 + 趋势箭头              │
│   下方一行：受影响指标缩略 sparkline + "查看时间线"链接      │
├─────────────────────────────────────────────────────────────┤
│ ③ 主视图：8 张时序大图（4 列 × 2 行栅格）                    │
│   CPU usage% / Load5 / Mem used% / Disk used% /             │
│   Inode used% / Net In B/s / Net Out B/s / IOWait%          │
│   每图：MetricChart 360×140（X 轴时间 / Y 轴值标尺 /         │
│   阈值线 / 维护窗口阴影 / 十字线 hover tooltip）             │
│   每图次指标 dl（如 CPU 卡含 steal%；Mem 卡含 Swap used%）   │
├─────────────────────────────────────────────────────────────┤
│ ④ 次要信息（默认 collapsed，<details> 折叠）                 │
│   ▸ 标签与备注（编辑入口）                                  │
│   ▸ 生命周期（退役 / 恢复观察 等真危险动作 + ConfirmationCard）│
│   ▸ 接入凭证状态（链接到接入工作台）                         │
├─────────────────────────────────────────────────────────────┤
│ 页面底部 mono 小字：数据快照时间 YYYY/MM/DD HH:mm，          │
│ 刷新页面获取最新                                             │
└─────────────────────────────────────────────────────────────┘
                          ↘ 右侧抽屉触发 ↙
                          ┌──────────────────────┐
                          │ 历史抽屉（B 决策）    │
                          │ Tabs:                │
                          │  [事件时间线]         │
                          │  [历史异常]           │
                          │ 宽度 min(440px,40vw) │
                          └──────────────────────┘
```

## Decisions

| ID | 决策 |
|---|---|
| Q-CHART | **B**（新建 `<MetricChart>` 原子，纯 SVG）。完整化：X 轴时间格式化 + Y 轴值标尺含单位 + 阈值线（绛红虚线）+ 维护窗口背景阴影（烟蓝半透明）+ 十字线 hover tooltip + 缺口处理。Sparkline 原子保留并继续维护，不被 MetricChart 取代。**风险**：如未来发现读不准 / 缺缩放刷选 / 多线 overlay 等真实需求，再单独发起 task 评估升级到 visx |
| Q-METRICS | **B**（标准 8 张）。CPU usage% / Load5 / Mem used% / Disk used% / Inode used% / Net In B/s / Net Out B/s / IOWait%；4×2 栅格；每张 ~360×140；阈值线（CPU/Mem/Disk/Inode 各 80%/95% 双线；IOWait 20%/50%；Load5 4.0/8.0；Net In/Out 不画阈值）；次指标 dl（CPU 卡含 steal%；Memory 卡含 Swap used%；Disk 卡含 disk_busy% / read+write） |
| Q-DRAWER | **B**（事件 + 历史 incident）。抽屉内 tabs 切换；事件 tab 复用 `<EventList>`；异常 tab 复用 `<IncidentList>`；**后端改**：`/api/incidents` handler 加 `include_resolved` query 参数 + repo 加 SQL where 条件，~50 行 Go + ≥1 个新单测 |
| Q-PR-SPLIT | **B**（3 PR 拆分）。PR1 MetricChart 原子 / PR2 NodeDetailPage 主体重构（①+②+③+④）/ PR3 抽屉 + 后端 + 文档同步 |

## Requirements

### MetricChart 原子（PR1）

文件：`web/src/components/atoms/MetricChart.tsx` + 测试 + atoms.css 段

API：
```ts
export type MetricChartTone = 'normal' | 'notice' | 'alert' | 'critical' | 'maintenance' | 'accent' | 'accent-2'

export type MetricChartSample = {
  value: number
  observedAt: string
}

export type MetricChartThreshold = {
  value: number
  tone: 'notice' | 'alert' | 'critical'
  label?: string
}

export type MetricChartMaintenanceWindow = {
  startedAt: string
  endedAt: string  // 可选 'now' 类似的开放区间，未来再说
}

export interface MetricChartProps {
  samples: MetricChartSample[]
  width?: number
  height?: number
  tone?: MetricChartTone
  thresholds?: MetricChartThreshold[]
  maintenanceWindows?: MetricChartMaintenanceWindow[]
  ariaLabel?: string
  formatValue?: (value: number) => string  // Y 轴 + tooltip 格式化
  yMin?: number  // 显式锁定 Y 范围（如百分比 0-100）；不设则自动
  yMax?: number
  className?: string
}
```

视觉约定：
- 边距：左侧 32px Y 轴标尺区 / 底部 24px X 轴标尺区 / 右侧 8px / 顶部 8px
- 网格：水平 3-5 条淡虚线（值标尺刻度对齐）
- 折线：1.5px stroke + 末点圆 + tone 配色
- 阈值线：水平虚线 + tone 颜色 + 右侧小标签（"80%"）
- 维护窗口：竖向半透明带（fill 烟蓝 8% opacity）+ 顶部小三角 marker
- 十字线 tooltip：hover 显垂直 hairline + 横标尺 marker + 浮窗（时间 + 值）
- 边界态：0 sample → "暂无观测数据" placeholder（与 Sparkline 类似）；1 sample → 仅末点 + "样本不足" hint
- 维护态（外层 isMaintenance prop）→ 整图 opacity 0.6 + ribbon 由父级负责

测试 ≥ 5 用例：
- 默认渲染（含 thresholds + samples）
- 0 samples 占位
- 1 sample 退化
- maintenance windows 渲染（验证带状态 path）
- hover 显示十字线 + tooltip

### NodeDetailPage 主体重构（PR2）

#### ① 身份条（2 行 sticky 头部）

行 1（关键状态）：
- 左：`<Hostname truncate maxChars={28}>` (display_name) + 4 个 StatusBadge（lifecycle / monitoring / binding / current_health）
- 右：mono "数据新鲜度" — `心跳 5 分钟前 · 运行 11 天 6 小时`（`<Timestamp mode="relative">` + `formatUptime`）

行 2（mono 元数据条）：
- 一行 sans 11px muted：`<Hostname>{node_id}</Hostname>` · 位置（region · city · provider）· 标签（`labels.join(' · ')`）· `<MonoDigits>{agent_version}</MonoDigits>`

右上 sticky：
- `<Button variant="ghost">查看历史</Button>` 触发抽屉
- "…" 操作菜单（点击展开 popover）：进入维护 / 退出维护 / 暂停监控 / 恢复监控（按 monitoring_status 条件性显示，复用现有 `nodeRuntimeActions` 逻辑）

#### ② 危险区前置（条件性 — `current_active_incident_count > 0` 才渲染）

```
┌─ 绛红 ribbon top critical ──────────────────────────────┐
│ <h2 大字>{当前主问题摘要}</h2>                           │
│ 持续 <MonoDigits>{duration}</MonoDigits> · 状态从       │
│ <StatusBadge>{previous}</StatusBadge> 升级为             │
│ <StatusBadge>{current_health_status}</StatusBadge>      │
│ ─────────────────────────────────────────               │
│ 受影响：{affected_metric_label} <Sparkline tone="critical"> + │
│ <Link>查看完整时间线 →</Link>（触发抽屉跳到事件 tab）    │
└──────────────────────────────────────────────────────────┘
```

实现要点：
- 用 `<Card cardRole="warning" ribbonPlacement="top">` 包
- 主问题摘要直接取 `current_primary_issue_summary`（如"最近 514 个心跳周期未收到心跳"）
- 持续时长：`Date.now() - new Date(active_incident.opened_at)` 派生（或后端给）
- 受影响指标：根据 incident 类型映射（heartbeat → 心跳；metric → 对应指标）—— 首期可只显示 incident 摘要，"受影响指标"指引未来再做

#### ③ 主视图栅格

8 张 `<MetricChart>` 4×2 栅格：

```tsx
<div className="watchtower-metrics">
  <article className="watchtower-metric-card">
    <header className="watchtower-metric-card__head">
      <h3>CPU 使用率</h3>
      <span className="watchtower-metric-card__current">
        <MonoDigits>{formatPercent(latest.cpu_usage_pct)}</MonoDigits>
      </span>
    </header>
    <MetricChart
      samples={cpuSamples}
      tone={isMaintenance ? 'maintenance' : 'accent'}
      thresholds={[{value: 80, tone: 'notice'}, {value: 95, tone: 'critical'}]}
      yMin={0}
      yMax={100}
      formatValue={(v) => formatPercent(v)}
      maintenanceWindows={maintenanceWindows}
      ariaLabel="CPU 使用率近 24h 趋势"
    />
    <dl className="watchtower-metric-card__sub">
      <div><dt>steal</dt><dd><MonoDigits>{formatPercent(latest.cpu_steal_pct)}</MonoDigits></dd></div>
    </dl>
  </article>
  {/* ... 7 more cards ... */}
</div>
```

CSS：
```css
.watchtower-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-3);
}
@media (max-width: 1280px) {
  .watchtower-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
.watchtower-metric-card {
  background: var(--surface-elevated);
  border: 1px solid var(--border);
  border-radius: var(--radius-2);
  padding: var(--space-3);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}
.watchtower-metric-card__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}
.watchtower-metric-card__head h3 {
  font-family: var(--font-sans);
  font-size: 12px;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--text-muted);
  margin: 0;
}
.watchtower-metric-card__current {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
}
.watchtower-metric-card__sub dl {
  display: flex;
  gap: var(--space-3);
  font-size: 11px;
  color: var(--text-muted);
}
.watchtower-metric-card__sub dl dt { display: inline; }
.watchtower-metric-card__sub dl dd { display: inline; margin: 0 0 0 4px; }
```

#### ④ 次要信息（折叠区）

`<details>` 块（参考 SettingsPage UX polish 任务用法）：

```tsx
<details className="watchtower-secondary">
  <summary>标签与备注</summary>
  <NodeLabelsAndNote ... />
</details>
<details className="watchtower-secondary">
  <summary>生命周期</summary>
  <NodeLifecyclePanel ... />
</details>
<details className="watchtower-secondary">
  <summary>接入凭证状态</summary>
  <p>当前 token 状态：{onboarding.phase}</p>
  <Link to={`/nodes/${nodeId}/onboarding`}>查看接入工作台 →</Link>
</details>
```

注意：
- 现有 `NodeLabelsAndNote` 子组件保留复用
- "运行控制"中的非危险动作（维护 / 暂停）已迁到 ① 右上"…"菜单；"生命周期"折叠区仅留真危险动作（退役 / 恢复观察），仍带 `<ActionConfirmationCard>` 二次确认

#### 页面底部数据快照行

参考 NodeOnboardingPage 同款写法：
```tsx
<p className="watchtower-snapshot-meta">
  数据快照时间：<Timestamp value={now} mode="absolute" />，刷新页面获取最新。
</p>
```

#### 删除

- 现有 `NodeStatusSummary` 组件（信息已被 ①身份条 + ②危险区吸纳）
- 现有 `NodeHostMetrics` 组件（被 ③ 主视图栅格 + MetricChart 取代）
- 现有 `NodeHero` 组件（被 ① 新身份条取代）
- 现有 page 内 NodeTrendCards 引用（已在 redesign-node-pages 任务删除，确认无残留）

被删组件的对应测试也一并删除；新增 watchtower 相关单测放在 NodeDetailPage.test.tsx。

### 抽屉 + 历史 incident（PR3）

#### 前端：Drawer 组件

新增 `web/src/components/atoms/Drawer.tsx` + 测试：

```ts
export interface DrawerProps {
  open: boolean
  onClose: () => void
  title: string
  side?: 'right' | 'left'  // 默认 right
  width?: string  // 默认 'min(440px, 40vw)'
  children: React.ReactNode
}
```

实现：
- 用 `<dialog>` 原生元素（自带 ESC 关闭 + 焦点管理）或 portal + overlay
- 滑入动画 `transition: transform var(--dur-soft, 200ms) var(--ease-calm, ease-out)`
- 点击 overlay 或 ESC → onClose
- aria-label / focus trap

CSS（atoms.css 末尾追加）：
```css
.drawer-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.4);
  z-index: 100;
}
.drawer {
  position: fixed;
  top: 0;
  bottom: 0;
  right: 0;
  width: min(440px, 40vw);
  background: var(--surface);
  border-left: 1px solid var(--border);
  box-shadow: -8px 0 24px rgba(0, 0, 0, 0.4);
  z-index: 101;
  display: flex;
  flex-direction: column;
  transform: translateX(0);
  transition: transform var(--dur-soft, 200ms) var(--ease-calm, ease-out);
}
.drawer--closed { transform: translateX(100%); }
.drawer__header { padding: var(--space-4); border-bottom: 1px solid var(--border); display: flex; justify-content: space-between; align-items: center; }
.drawer__body { flex: 1; overflow-y: auto; padding: var(--space-4); }
```

#### NodeDetailPage 接入 Drawer + Tabs

```tsx
const [historyOpen, setHistoryOpen] = useState(false)
const [historyTab, setHistoryTab] = useState<'events' | 'incidents'>('events')

return (
  <>
    {/* ... main page content ... */}
    <Drawer open={historyOpen} onClose={() => setHistoryOpen(false)} title={`${node.display_name} · 历史`}>
      <Tabs variant="pill" value={historyTab} onChange={setHistoryTab} tabs={[
        { value: 'events', label: '事件时间线' },
        { value: 'incidents', label: '历史异常' },
      ]} />
      {historyTab === 'events' ? (
        <EventList events={events} />
      ) : (
        <IncidentList incidents={historyIncidents} />
      )}
    </Drawer>
  </>
)
```

新增 API client 函数（`web/src/lib/api.ts`）：
```ts
export function listHistoricalIncidents(objectType: string, objectId: string) {
  return requestJSON<ActiveIncidentRecord[]>(
    `/api/incidents?object_type=${objectType}&object_id=${objectId}&include_resolved=true`
  )
}
```

#### 后端：`/api/incidents` 加 include_resolved

文件：`internal/center/http/handlers/incidents.go`（grep 现有 handler）

改动：
- handler 解析 `include_resolved=true|false` query 参数（默认 false 保持现有行为）
- repo 接口加 `IncludeResolved bool` 选项
- store SQL where 条件分支：默认 `where status = 'active'`；`include_resolved=true` 时不加 status filter

新增单测 ≥1：`TestListIncidents_IncludeResolved`

### Mono 包装兜底

继承前置任务模式：所有数字 / 时间戳 / ID 包 `<MonoDigits>` / `<Timestamp>` / `<Hostname>`。

## Acceptance Criteria

### 通用
- [ ] 现有功能零回归：metadata 编辑 / 维护暂停退役恢复 / 绑定冲突处置 / 接入工作台跳转
- [ ] `cd web && npm run lint && npm run test && npm run build` 全绿（基线 366）
- [ ] `make verify-go` 全绿
- [ ] 三 PR 各自 trellis-implement → trellis-check → main commit 节奏

### PR1 MetricChart 原子
- [ ] 新原子 `<MetricChart>` 在 atoms barrel 导出
- [ ] X 轴时间格式化 / Y 轴值标尺 / 阈值线（含 tone）/ 维护窗口阴影 / 十字线 hover tooltip 全部可见
- [ ] 0 samples / 1 sample / maintenance windows / 阈值超出数据范围 边界态正确渲染
- [ ] ≥5 测试用例覆盖

### PR2 NodeDetailPage 重构
- [ ] ① 身份条 2 行布局，右上 sticky "查看历史" + "…" 操作菜单
- [ ] ② 危险区前置：仅 active_incident_count > 0 显示，绛红 ribbon + 大字摘要
- [ ] ③ 主视图 8 张 MetricChart 4×2 栅格，每张含次指标 dl
- [ ] ④ 次要信息默认 collapsed
- [ ] 页面底部数据快照行
- [ ] 删除 NodeHero / NodeStatusSummary / NodeHostMetrics 旧组件 + 对应测试
- [ ] NodeDetailPage.test.tsx 既有断言更新；新增 ≥4 用例（无异常时危险区不显 / 有异常时显 / 主图 8 张 / 次要折叠）

### PR3 抽屉 + 后端
- [ ] 新原子 `<Drawer>` + 单测
- [ ] NodeDetailPage 接入 Drawer + Tabs（事件 / 历史异常）
- [ ] 后端 `/api/incidents` handler 接受 `include_resolved` 参数；现有不带参数行为不变（仅 active）
- [ ] 后端新增 ≥1 单测 `TestListIncidents_IncludeResolved`
- [ ] component-spec.md §五 NodeDetailPage 段落同步重写为 watchtower 形态
- [ ] design-language.md §12 加注：当前仍不引图表库，但有 MetricChart 自研 + 未来视觉化高阶需求时再单独评审 visx

## Definition of Done

- TypeScript strict 0 error
- ESLint 0 warning
- vitest 全 pass，新增覆盖 ≥10 用例（PR1 5 + PR2 4 + PR3 1）
- Go 单测：`internal/center/http/handlers/incidents.go` 新增 1 用例 + repo 单测覆盖
- spec / 文档同步完成（component-spec.md §五 重写 + design-language.md §12 加注）
- 不需要 trellis-update-spec（按既有 v2 模式执行 + 自研原子；无新工程规范沉淀）

## Out of Scope

### 后端
- retention / sample 频率任何变更
- 时间窗口切换（24h 硬编码不变）
- metric 历史 API / time travel
- multi-server overlay 接口

### 前端
- TargetDetailPage / Dashboard / 节点列表页同步改造（实施顺序 2/3 单独任务）
- 引入 visx / recharts / echarts / chart.js 等图表库
- 时间窗口切换 24h ↔ 7d UI（架构层面也不预留）
- multi-server overlay 对比视图
- metric time travel / point-in-time view UI
- 阈值线动态化读 center_settings（首期固定值）
- 实时 polling 节点状态（保留"页面打开 = 静态快照"模型）
- 移动端响应式 / i18n
- Drawer 组件用于其他场景（仅本任务消费）

## Technical Approach

3 PR 拆分，每 PR 完成后 trellis-implement → trellis-check → 用户视觉走查 → main commit。

**关键技术点**：
- MetricChart 纯 SVG，CSS grid 内置 viewBox-based 标尺；hover 用 React useState 追踪 hovered index（参考 Sparkline interactive 模式）
- 数据派生：`recent_host_samples` 按 metric 字段拆 8 个 SparklineSample 数组（参考 NodeHostMetrics 既有 deriveSeries 但扩到 8 个）
- maintenance 窗口：暂时简化 — 当 sample.maintenance_context === true 时构造一个段；首期不引入"维护窗口表"概念
- 阈值固定值：CPU/Mem/Disk/Inode 80/95；IOWait 20/50；Load5 4.0/8.0；Net 不画
- 操作菜单 popover：用 `<details><summary>` 原生（参考 settings UX polish 同款），无需新组件
- Drawer 用 `<dialog>` 原生 + slide-in animation（不依赖 React portal 库）

## Decision (ADR-lite)

**Context**：候风现有节点详情页"装饰多 / 趋势配角 / 异常等权"，用户判定"整体掌握困难"。前置任务建立的 v2 模式（mono / DataTable / Sparkline）原料够用，但信息架构和视觉层级需要重排，明确 ops-first 优先于 aesthetic-first。

**Decision**：
1. 新建 `<MetricChart>` 原子（纯 SVG），保留 Sparkline 作为小尺寸配角
2. 主视图栅格 8 张图（4×2），明确 sparkline 是主体不是装饰
3. 危险区前置（仅有异常时显示），打破 "8 个 section 等权堆叠" 模式
4. 次要信息全部 `<details>` 折叠，砍装饰 chrome
5. 历史信息出抽屉，不再页内底部
6. 设计语言 §12 不引图表库的硬约束**仍然保留**，但加注未来评估路径
7. 后端最小化改动（仅 incidents include_resolved）

**Consequences**：
- 收益：用例 #1（异常）#2（趋势）#3（历史稳定度）#4（操作）全部直击；信息架构跟"看一台机器"的工作流匹配；趋势图占主体大幅提升监控可用性
- 取舍：MetricChart 自研意味着无现成"缩放 / 刷选 / 多线 overlay"等高阶能力（用户已知；未来如真有需求再升级 visx）
- 风险：3 PR 是中-大规模重构；NodeDetailPage 现有结构会大改，需 trellis-check 严格回归既有功能；Drawer 是新原子需打磨
- 未来：本任务完成后 → 节点列表对齐新模板 → Dashboard 节点块再调；TargetDetailPage 同款 watchtower 改造单独任务

## Technical Notes

**关键文件**：
- 新增：`web/src/components/atoms/MetricChart.tsx` + `Drawer.tsx` + 各自测试
- 改造：`web/src/pages/NodeDetailPage.tsx`（约 900 行 → 大改）
- 删除：`web/src/components/node-detail/{NodeHero,NodeStatusSummary,NodeHostMetrics}.tsx` + 对应测试
- 保留：`web/src/components/node-detail/NodeLabelsAndNote.tsx`（折叠区复用）
- 改造：`web/src/lib/api.ts`（加 listHistoricalIncidents）
- 改造：`internal/center/http/handlers/incidents.go` + 对应 repo
- 同步：`docs/design/v2-houfeng/component-spec.md` §五（重写 NodeDetailPage 段）+ `docs/design/v2-houfeng/design-language.md` §12（加注）

**关键参考**：
- 设计语言：`docs/design/v2-houfeng/design-language.md` §3 字体 / §6 状态 / §12 不做的事
- v2 spec：`docs/design/v2-houfeng/component-spec.md` §五 NodeDetailPage（本任务大幅重写）
- 业务：`docs/design/v1-baseline/architecture-data-model.md` §节点 / §健康状态派生
- 数据：`internal/center/store/runtime_facts.go` SQL `limit 288`（24h × 5min）

## Research References

- [`research/codebase-watchtower.md`](research/codebase-watchtower.md) — 待补：实施前 explore 现有 NodeDetailPage 结构 + 8 子组件 + 接入工作台跳转关系。可在 PR1 开始前由 trellis-implement sub-agent 自行 grep 完成
