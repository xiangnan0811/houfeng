# Targets 页面重设计 — 现状研究

**日期**: 2026-05-04
**任务**: gap #18（TargetsPage / TargetDetailPage 镜像 v2）+ gap #20-Targets 部分（Mono 落地）
**结论摘要**: TargetsPage 是 NodesPage 模式的镜像改造（中-大体量）；TargetDetailPage 1127 行有独特的 ProbeItem 列表 + Latency Trends 区，需要决策"Probe 列表是否表格化"与"是否引入 Sparkline"。

---

## 1. TargetsPage 现状

**文件**：`web/src/pages/TargetsPage.tsx` (985 行)，无独立测试

### 当前 Markup（自渲 .resource-table，非 DataTable）

```jsx
<div className="resource-table">
  <div className="resource-table__head">
    <span>目标</span>
    <span>执行与状态</span>
    <span>最近成功 / 失败</span>
    <span>当前主问题</span>
  </div>
  {filteredTargets.map((target) => (
    <article key={target.target_id} className="resource-table__row">
      <div>{/* name + type · host[:port] + 标签编辑 + 行内运行控制按钮 */}</div>
      <div>{/* run_status + health Badge + execution_node_labels */}</div>
      <div>{/* formatDateTime last_success_at + last_failure_at */}</div>
      <div>{/* current_active_incident_count + 主问题摘要 */}</div>
    </article>
  ))}
</div>
```

**结构特征**：
- 4 列、自渲 `<article>` 行（与节点列表 PR2 改造前相同模式）
- 行内常驻多按钮（快速编辑标签 + 运行控制 enter-maintenance/pause/archive 等）
- 视图切换：无（全量列表）
- 筛选栏：6 项（type / run_status / health / labels / execution_labels / abnormal toggle）— 已是 v2 模式
- 创建表单：可折叠 page-panel（8 字段）— 已 v2 风格

### Mono 漂移（4 处）

| 行 | 位置 | 现状 |
|----|------|------|
| 970 | `last_success_at` | `formatDateTime` 裸渲 → 应用 `<Timestamp mode="relative">` |
| 971 | `last_failure_at` | 同上 |
| 974 | `current_active_incident_count` | 裸渲 → `<MonoDigits>` |
| 967 | `execution_node_labels` | 标签未 Mono 包装 |

---

## 2. TargetDetailPage 现状（1127 行）

按 section 拆解：

| Section | 子组件 / 行数 | v2 atoms 用况 | 缺口 |
|---------|--------------|--------------|------|
| Hero | TargetHero (46 行) | 无 | hero-meta-card 4 个时间用 formatDateTime 裸渲 |
| StatusSummary | TargetStatusSummary (28 行) | 无 | 3 KPI 数字未 Mono |
| LabelsAndNote | TargetLabelsAndNote | 标准 | 无 |
| RuntimeControls | TargetRuntimeControls | ActionConfirmationCard ✓ | mono 待包 |
| ~~生命周期~~ | **不存在**（Target 只有 run_status，无 lifecycle） | — | — |
| **ProbeItem 列表** | **TargetProbeList (180 行)** — 核心 | 无 | 卡片栈，每卡内有横向 observation 行；node_id / 时间 / latency 全裸渲 |
| **LatencyTrends** | **TargetLatencyTrends (99 行)** | 无 | 文字数字阵，**无 Sparkline** |
| ActiveIncidents | (PR3 已重构) | ✓ v2 | 无 |
| RecentEvents | EventList | ✓ v2 | 无 |

### TargetProbeList markup（最独特核心）

```jsx
<div className="probe-list">
  {probeItems.map((probeItem) => (
    <article className="probe-card">
      <header>
        <h3>{probe_kind.toUpperCase()}</h3>  {/* TCP / HTTP / TLS */}
        <p>{formatConfigSummary(config)}</p>
        <div className="badge-row">
          <StatusBadge>{enabled ? '启用' : '停用'}</StatusBadge>
          <StatusBadge tone="cyan">{frequency_tier}</StatusBadge>
        </div>
      </header>
      <div className="badge-row">
        <button>编辑</button> <button>停用/启用</button> <button>删除</button>
      </div>
      <dl><dt>超时</dt><dd>{timeout_seconds}s</dd>
          <dt>最近观测</dt><dd>{formatDateTime(observations[0].observed_at)}</dd></dl>
      <div className="observation-list">
        {observations.map((obs) => (
          <div className="observation-row">
            <div><strong>{node_id}</strong><p>{formatDateTime}</p></div>
            <div><StatusBadge>{result_kind}</StatusBadge></div>
            <div><span>延迟</span><strong>{formatLatency}</strong></div>
            <div><span>HTTP / TLS</span><strong>{http_status ?? tls_expiry_days}</strong></div>
            <div><span>错误摘要</span><strong>{error_summary}</strong></div>
          </div>
        ))}
      </div>
    </article>
  ))}
</div>
```

### TargetLatencyTrends 现状

DetailSection 包 probe-list/probe-card 栈，每卡显示某 probe_item_id 的：count / distinctNodeCount / averageLatency / maxLatency / latestLatency / 样本窗口 / 覆盖节点数 —— **6-7 个数字阵，无图**。

---

## 3. 数据契约

### API 路径与类型

| API | 返回类型 | 关键字段 |
|-----|---------|----------|
| `GET /api/targets` | `TargetRecord[]` | target_id, name, target_type, host, base_port, execution_node_labels, run_status, labels, current_health_status, current_active_incident_count, last_success_at, last_failure_at, current_primary_issue_summary |
| `GET /api/targets/{id}` | `TargetRecord` | 同上 |
| `GET /api/targets/{id}/runtime-facts` | `TargetRuntimeFacts` | latest_probe_observations[], **recent_probe_observations[]** |
| `GET /api/targets/{id}/probes` | `ProbeItemRecord[]` | probe_item_id, probe_kind, enabled, frequency_tier, timeout_seconds, config |

### ProbeObservation（时序数据）

```ts
type ProbeObservation = {
  node_id, target_id, probe_item_id, probe_kind: string
  observed_at, received_at: string  // ISO 8601
  result_kind: 'success' | 'failure'
  latency_ms: number | null
  http_status: number | null
  tls_expiry_days: number | null
  error_code, error_summary?: string
  maintenance_context, is_backfilled: boolean
}
```

**Sparkline 可用性**：
- `recent_probe_observations` 提供逐点 latency_ms 时间序列（按 frequency_tier 决定密度：1m / 5m / 15m / 6h）
- 与 HostSample 不同的是：观测点稀疏（视 frequency_tier 而定），不是均匀 5min 步长
- 但仍可逐点画 Sparkline（reuse 现有 `<Sparkline interactive samples={...}>`）

---

## 4. v2 设计权威

`docs/design/v2-houfeng/component-spec.md` §五：

```
### TargetsPage / TargetDetailPage
- 镜像 NodesPage / NodeDetailPage 的 DataTable + 详情结构
- 列略不同：[StatusGlyph, 名字, 类型, host, 标签, 状态, 操作]
```

**唯一明确条款**：列表镜像 NodesPage（DataTable 紧凑 36px / StatusGlyph 行首列 / hover 操作）。详情未明说 Probe 列表形态。

---

## 5. ProbeItem 业务模型

### 三种 ProbeKind（v1 baseline frozen）

- **TCP**：config = `{port}`
- **HTTP**：config = `{scheme, path, method, expected_status_range}`
- **TLS**：config = `{port, expiry_warning_days}`

### 当前 ProbeItem 行为

- enabled / disabled toggle
- frequency_tier: `1m | 5m | 15m | 6h`
- timeout_seconds 配置
- 对应 ProbeObservation 时序记录
- **无聚合指标**（成功率 / 连续失败次数等都是前端按需算）

---

## 6. 缺口对照

### TargetsPage vs NodesPage

| 维度 | NodesPage (v2 ✓) | TargetsPage (gap) |
|------|------------------|-------------------|
| 表格 | DataTable density="compact" 36px | .resource-table div 自渲 |
| 列定义 | 7 列含 StatusGlyph 行首 | 4 列无 StatusGlyph |
| 操作按钮 | hover 才显 | 行内常驻 |
| Mono | 全栈包装 | 0 处 |

### TargetDetailPage vs NodeDetailPage

| 维度 | NodeDetailPage (v2 ✓) | TargetDetailPage (gap) |
|------|----------------------|------------------------|
| Hero meta 时间 | `<Timestamp>` mono | formatDateTime 裸渲 |
| 主指标可视 | 4 metric-card 含 Sparkline 240×60 interactive | 无对应（Target 无主机概念，只有 ProbeItem 观测） |
| ProbeItem 列表 | N/A | 卡片栈（独特，待决策） |
| LatencyTrends | （并入主机指标 Sparkline） | 数字阵无图 |
| Mono | 全栈 | 0 处 |

### Mono 漂移点（共 12+ 处）

集中在：TargetsPage（4 处）、TargetHero（2 处）、TargetProbeList（4-5 处）、TargetLatencyTrends（3 处）。

---

## 7. 改造体量预估

### TargetsPage：中-大

参考 NodesPage 改造（节点列表 1077 行，PR2 体量）。约 300-400 行重写：
- DataTable 7 列定义（StatusGlyph + 目标 + 类型 + Host + 标签 + 状态 + 操作）
- hover 操作模式（复用 NodesPage 同款 CSS）
- 行点击导航
- Mono 全栈包装

风险低（参考完整案例）。

### TargetDetailPage：大

ProbeList + Trends 占 280 行，待决策项：

**关键决策点 1：ProbeItem 列表是否表格化？**
- A) 保持卡片栈（每卡 1 个 ProbeItem + 内嵌 observation 行）— 信息密度高、跟 ProbeKind 多样配置匹配好；但与 NodesPage DataTable 风格不一致
- B) ProbeItem 整体 DataTable 化（列：kind / config / enabled / frequency / 最近观测 / 操作）— 更工业；但 observation 详情只能展开/抽屉
- C) 混合：保持卡片，但内嵌 observation-list 改为 36px 紧凑表格

**关键决策点 2：是否引入 Sparkline 替代 LatencyTrends 数字阵？**
- 数据：每 probe_item 有 recent_probe_observations（按 frequency_tier 稀疏，但可画）
- 可参考 NodeHostMetrics 的 sparkline interactive 模式
- 如不做：LatencyTrends 仍只是数字阵

预估：
- 仅 Mono 包装 + DataTable 列表 + Probe 卡内 observation 行紧凑：**3-4 个 PR**
- + Sparkline 引入：再 +1 PR

### 总体

参考节点页面任务（PR1+PR2+PR3 共 3 PR），TargetsPage + TargetDetailPage 改造类似量级或略大（取决于 Probe 形态决策与 Sparkline）。
