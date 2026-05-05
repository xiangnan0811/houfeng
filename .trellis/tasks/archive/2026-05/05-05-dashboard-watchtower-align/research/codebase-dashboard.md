# Dashboard 页面重设计 — 现状研究

**日期**: 2026-05-04
**范围**: DashboardPage 现状、数据契约、v2 设计权威、Mono 漂移盘点
**结论摘要**: Dashboard **已接近 v2 形态**（Mono 9/10、v2 对齐 8/10），唯一显著缺口是 AbnormalNodeList 未显示 `node_id` 的 `<Hostname>`；可选项是迁到 DataTable 以求与 NodesPage 严格视觉统一。

---

## 1. 文件清单与职责

**主文件**：`web/src/pages/DashboardPage.tsx`（382 行）

| Section | 行数 | 职责 |
|---------|------|------|
| 导入与类型 | 1-23 | atoms / DetailSection / EventList / API |
| `StatTile` 组件 | 25-64 | KPI 卡（数值 + 可选 24h Sparkline）。**已用 `<MonoDigits>`** ✓ |
| 辅助函数 | 66-91 | `statusTone()`、`statusGlyph()`、`severityWeight()`、`hostPortSummary()` |
| `AbnormalNodeList` | 92-168 | 异常节点行列表。已用 `<StatusGlyph>` ✓、`<MonoDigits>` ✓、`<Timestamp>` ✓；**缺 `<Hostname>` 显示 node_id** |
| `AbnormalTargetList` | 170-247 | 异常目标行列表。已用 `<Hostname>` ✓、`<MonoDigits>` ✓、`<Timestamp>` ✓、`<StatusGlyph>` ✓ |
| `DashboardPage` 主容器 | 250-382 | Hero panel + StatStrip + 3×DetailSection（节点/目标/事件） |

**测试**：`DashboardPage.test.tsx`（190 行），覆盖加载/异常/fresh-install/错误态
**复用组件**：`EventList.tsx` 已包 `<Hostname>` `<Timestamp>` ✓

无独立 lib summarizer；数据直接从 `getDashboard()` API。

---

## 2. 当前 Markup 结构（关键 JSX 片段）

### Hero（无问题）
```jsx
<section className="hero-panel">
  <div className="hero-panel__content">
    <p className="hero-panel__eyebrow">当前风险总览</p>
    <h2 className="hero-panel__title">首页 / Dashboard</h2>
    <p className="hero-panel__description">先处理当前异常，再查看趋势与事件历史。</p>
  </div>
</section>
```

### Stat Strip（5 卡，已 v2 对齐）
```jsx
<div className="summary-grid summary-grid--strip">
  <StatTile label="风险对象" value={abnormalTotal} />
  <StatTile label="严重对象总数" value={severeTotal} />
  <StatTile label="维护对象总数" value={maintenanceTotal} />
  <StatTile label="新增异常" value={overview.recent_new_incident_count}
            trend={overview.new_incident_trend_24h} trendTone="critical" />
  <StatTile label="恢复事件" value={overview.recent_recovery_count}
            trend={overview.recovery_trend_24h} trendTone="normal" />
</div>
```

`StatTile` 内部：`<MonoDigits>{value}</MonoDigits>` + `<Sparkline>` ✓

### AbnormalNodeList（**唯一明显 gap**：缺 node_id 的 Hostname）
```jsx
<div className="probe-list probe-list--rows">
  {sorted.map((node) => (
    <article key={node.node_id} className="probe-card probe-card--row">
      <div className="probe-card__lead">
        <StatusGlyph state={statusGlyph(node.current_health_status)} />  {/* ✓ */}
      </div>
      <div className="probe-card__body">
        <header className="probe-card__header">
          <div>
            <h3>{node.display_name}</h3>
            {/* ⚠️ 这里没有 node_id 的显示 */}
            <p>{node.current_primary_issue_summary || '暂无关键异常摘要'}</p>
          </div>
          <div className="badge-row badge-row--wrap">
            <StatusBadge label={node.current_health_status} tone={statusTone(...)} />
            <StatusBadge label={node.monitoring_status} tone="cyan" />
          </div>
        </header>
        <dl className="probe-card__meta">
          <div><dt>位置</dt><dd>{node.region} / {node.city}</dd></div>
          <div><dt>供应商</dt><dd>{node.provider}</dd></div>
          <div><dt>生命周期</dt><dd>{node.lifecycle_status}</dd></div>
          <div><dt>活跃异常</dt><dd><MonoDigits>{node.current_active_incident_count}</MonoDigits></dd></div>  {/* ✓ */}
          <div><dt>最近心跳</dt><dd><Timestamp value={node.last_heartbeat_at} mode="relative" /></dd></div>  {/* ✓ */}
        </dl>
        <Link className="text-link" to={`/nodes/${node.node_id}`}>查看节点</Link>
      </div>
    </article>
  ))}
</div>
```

### AbnormalTargetList（10/10 已对齐 v2）
```jsx
{/* 同结构，所有 Mono 包装齐全 */}
<dl>
  <div><dt>地址</dt><dd><Hostname>{hostPortSummary(target)}</Hostname></dd></div>  {/* ✓ */}
  <div><dt>活跃异常</dt><dd><MonoDigits>{target.current_active_incident_count}</MonoDigits></dd></div>
  <div><dt>最近成功</dt><dd><Timestamp value={target.last_success_at} mode="relative" /></dd></div>
  <div><dt>最近失败</dt><dd><Timestamp value={target.last_failure_at} mode="relative" /></dd></div>
</dl>
```

---

## 3. 数据契约

**前端**：
```ts
export function getDashboard() {
  return requestJSON<DashboardOverview>('/api/dashboard')
}

export type DashboardOverview = {
  total_node_count: number
  abnormal_node_count: number
  abnormal_target_count: number
  severe_node_count, severe_target_count, maintenance_node_count, maintenance_target_count: number
  recent_new_incident_count, recent_recovery_count: number
  abnormal_nodes: DashboardNodeSummary[]
  abnormal_targets: DashboardTargetSummary[]
  recent_events: StateChangeEventRecord[]
  new_incident_trend_24h?: number[]   // 24 元素
  recovery_trend_24h?: number[]       // 24 元素
}

export type DashboardNodeSummary = {
  node_id, display_name, region, city, provider: string
  lifecycle_status, monitoring_status: string
  current_health_status: IncidentSeverity
  last_heartbeat_at?: string
  current_active_incident_count: number
  current_primary_issue_summary: string
}
```

**后端 handler**：`internal/center/http/handlers/dashboard.go`，limit 默认 10，返回 DashboardOverview。

**数据完整性**：✓ 所有需要的字段都有（node_id 在数据里，只是前端没显示）

---

## 4. v2 设计权威

来自 `docs/design/v2-houfeng/component-spec.md` §五 DashboardPage：

```
1. Hero panel：当前风险总览 eyebrow + 首页/Dashboard h1 + 描述
2. Stat strip（5 列等宽）：每列 [label · MonoDigits · TrendArrow]
3. DetailSection 异常节点概览：summary-grid (3 KPI) + 紧凑节点行
   （StatusGlyph + Hostname + 位置 + 当前问题 + Timestamp）
4. DetailSection 异常目标概览：同样模式
5. DetailSection 最近事件：EventList timeline
```

---

## 5. Mono 漂移点 grep 结果

| 行号 | 位置 | 形态 | 状态 |
|------|------|------|------|
| 49 | StatTile 值 | `<MonoDigits>` | ✓ |
| 146 | AbnormalNodeList 异常数 | `<MonoDigits>` | ✓ |
| 152 | AbnormalNodeList 心跳 | `<Timestamp mode="relative">` | ✓ |
| 214 | AbnormalTargetList host | `<Hostname>` | ✓ |
| 220 | AbnormalTargetList 异常数 | `<MonoDigits>` | ✓ |
| 226 | AbnormalTargetList 成功时间 | `<Timestamp>` | ✓ |
| 232 | AbnormalTargetList 失败时间 | `<Timestamp>` | ✓ |
| **N/A** | **AbnormalNodeList node_id** | **未显示** | **❌** |

无 `formatPercent` / `formatBytes` / `formatDateTime` 裸调用 —— 全文 Mono 包装合规。

---

## 6. 缺口对照与优先级

| P | 缺口 | 工作量 | 价值 |
|---|------|--------|------|
| **P0** | AbnormalNodeList 补 `<Hostname>{node.node_id}</Hostname>` | < 5 min（1 行 JSX + 测试） | 关闭 #20-Dashboard 唯一显著漂移 |
| **P1**（可选） | AbnormalNodeList / TargetList 自渲 → `<DataTable>` | 30-60 min | 与 NodesPage 风格严格统一；但 dl 形态对"摘要视图"也合理，未必必要 |
| 不动 | StatTile / EventList | 0 | 已 v2 对齐 |

---

## 7. 与前置任务的复用

- `<DataTable>` 已实装且在 NodesPage 消费过，可直接 import 用
- `<Hostname truncate maxChars={N}>` / `<Timestamp>` / `<MonoDigits>` 全已可用
- `statusGlyph()` mapper 当前在 DashboardPage 内是私有 helper；NodesPage 也有类似的 `nodeGlyphState()` —— 不抽到 lib 也行（YAGNI）

---

## 8. 总结

**Dashboard 实质性 gap 极小**：

- gap #19（节点概览块对齐 v2）：实际只剩 1 处（缺 node_id Hostname）
- gap #20-Dashboard（Mono 字体落地）：基本已完成

如果只关 P0：1 行 JSX + 1 个测试断言即可；
如果连 P1 一起做：把两个 list 迁到 DataTable，跟 NodesPage 一致风格。

**任务 scope 比预期小很多**，可能 1 个小 PR 即可完成。
