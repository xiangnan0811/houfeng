# 候风节点页面前端现状 & 数据契约分析

> 研究产出：作为 PRD 决策依据。

## 1. 文件清单与职责

### 页面核心（`web/src/pages/`）

| 文件 | 职责 |
|------|------|
| `NodesPage.tsx` | 节点列表：表格 4 列（节点 / 状态 / 最近心跳·同步 / 当前主问题）+ 筛选栏（地区·城市·供应商·生命周期·运行状态·健康状态·标签·仅看异常）+ 创建表单 + 绑定冲突视图切换 + 行内运行控制按钮 |
| `NodeDetailPage.tsx` | 节点详情：并行加载 `getNode()` + `getNodeRuntimeFacts()`，处理绑定冲突、metadata 编辑、运行控制、生命周期操作、异常列表、事件流 |
| `NodeOnboardingPage.tsx` | 接入工作台：phase + token 发放 + 绑定冲突处置 + 安装步骤，数据来自 `getNodeOnboarding()` |

### 节点详情子组件（`web/src/components/node-detail/`）

| 文件 | 职责 |
|------|------|
| `NodeHero.tsx` | hero 顶部：名称 + 地区/城市/供应商 + 状态徽标行 |
| `NodeStatusSummary.tsx` | summary-grid 三卡（健康状态 / 活跃异常数 / 当前主问题） |
| `NodeLabelsAndNote.tsx` | 标签/备注 inline 编辑 |
| `NodeHostMetrics.tsx` | "当前主机指标" 4 张 metric-card（CPU/Load、内存/Swap、磁盘/Inode、网络/吞吐），每卡 `<dl>` 罗列键值 |
| `NodeTrendCards.tsx` | "近期趋势" 4 张 metric-card（样本概览 / Load5 趋势 / CPU 等待 / CPU steal），后三个**已含 Sparkline**（`tone="accent|alert|critical"`） |

### 共享组件（`web/src/components/`）

| 组件 | 用途 |
|------|------|
| `DetailSection` | 详情 section 包装：eyebrow + title + aside，可选 ribbon |
| `StatusBadge` | 6 态徽标，复用 Badge 与状态色 |
| `ActionConfirmationCard` | 维护/暂停确认卡 |
| `EventList` / `IncidentList` | 事件流、异常列表 |

### 原子（`web/src/components/atoms/`）

| 原子 | 状态 |
|------|------|
| `Sparkline.tsx` | ✅ 已实装；64×16 SVG polyline + 末点圆，8 种 tone |
| `StatusGlyph.tsx` | ✅ 6 态形状指示符 |
| `TrendArrow.tsx` | ✅ ↗↘→ + delta |
| `DataTable.tsx` | ✅ 已存在但 NodesPage 当前 **未使用**（用的是 `.resource-table` 自渲染 `<article>`） |
| `Mono.tsx` | ✅ 文件存在（`MonoDigits` / `Hostname` / `Timestamp`），但节点页面未消费 |

## 2. 现有数据契约

### 节点列表 `GET /api/nodes` → `NodeRecord[]`

字段：
- 标识：`node_id`, `display_name`, `region`, `city`, `provider`
- 状态：`lifecycle_status`, `monitoring_status`, `binding_status`
- 元数据：`labels[]`, `note`
- 健康：`current_health_status`, `current_active_incident_count`, `current_primary_issue_summary`
- 时间：`last_heartbeat_at`, `last_sync_at`, `created_at`, `updated_at`

⚠️ **没有任何时序字段**，列表只有最新一刻快照 → 列表里要画 sparkline 必须扩字段或新增聚合接口。

### 节点详情

```
GET /api/nodes/{nodeId} → NodeRecord
GET /api/nodes/{nodeId}/runtime-facts → NodeRuntimeFacts {
  node_id, latest_host_sample, recent_host_samples[]
}
```

`recent_host_samples` 关键发现：
- **后端 SQL**：`where observed_at >= $2 order by observed_at desc, id desc limit 288`
- 返回**最近 24h × 5min 步长 ≈ 288 个 HostSample**
- 前端 `summarizeRecentHostSamples()` 把它转成 Series 数组喂 Sparkline（已实装）

`HostSample` 全字段（25 个，全部都是 5 分钟步长的时序）：
```
cpu_usage_pct, load_1, load_5, load_15,
mem_used_pct, mem_available_bytes, swap_used_pct,
disk_used_pct, inode_used_pct, disk_busy_pct,
net_in_bytes_per_sec, net_out_bytes_per_sec,
cpu_iowait_pct, cpu_steal_pct,
disk_read_bytes_per_sec, disk_write_bytes_per_sec,
uptime_seconds, fingerprint, agent_version,
observed_at, received_at, maintenance_context,
is_backfilled, sync_batch_id, node_id
```

→ 后端**所有指标都已有 24h 时序数据**，前端却只用了 3 个画 sparkline。**这是最大的浪费**。

### 接入工作台

```
GET /api/nodes/{nodeId}/onboarding → NodeOnboardingState
  extends NodeRecord
  + phase, has_host_sample, has_accepted_observation
  + enrollment_token_issued_at, current_binding_fingerprint_summary
  + pending_binding {fingerprint, first_seen_at, last_seen_at, attempt_count}

POST /api/nodes/{nodeId}/enrollment-token → {token, issued_at}
```

## 3. 视觉/布局现状

### 节点列表（截图 1）

```jsx
<div className="resource-table">
  <div className="resource-table__head">节点 / 状态 / 最近心跳·同步 / 当前主问题</div>
  {nodes.map(n => (
    <article className="resource-table__row">
      <div>…名称 / 位置 / 标签 / 内联编辑 / 多个按钮…</div>
      <div>3 个 StatusBadge</div>
      <div>2 个时间戳</div>
      <div>异常数 + 文案</div>
    </article>
  ))}
</div>
```

**问题**：
- 视图切换器 `[全部节点 N | 绑定异常 M]` 占了一整张 hero 卡（`page-panel` 高度大）
- 「新建节点」整行宽度的 ghost button 占了一大块空白
- 行内嵌入了"快速编辑标签 / 进入维护 / 暂停监控"按钮，导致行高变化、密度紊乱
- 列定义少、且都是文本，**完全没有趋势可视化**

### 节点详情（截图 2 + 3）

```
<page-stack>
  NodeHero (hero-panel)
  NodeStatusSummary (summary-grid 3 卡)
  NodeLabelsAndNote
  DetailSection 运行控制
  DetailSection 生命周期
  NodeHostMetrics  (metric-grid 4 卡 — 全数字)
  NodeTrendCards   (metric-grid 4 卡 — 仅后 3 个有 sparkline)
  DetailSection 当前异常
  DetailSection 事件
</page-stack>
```

**问题**：
- 第一屏几乎全是文字 / 数字大字 / 状态徽标，**完全没有图**
- "当前主机指标" 4 卡每卡 3-6 行 `<dl>`，密度低、信息分散
- "近期趋势" 第一卡是**样本概览**（24h 样本数、最早/最新观测）—— 这在用户视角是元信息，不是"趋势"
- 真正的 trend sparkline 只有 Load5 / iowait / steal 三条，CPU/内存/磁盘/网络全无趋势

### 接入工作台（截图 4）

```
<page-stack>
  hero-panel 节点接入
  summary-grid 3 卡（当前阶段 / 首批样本 / 已接收观测）
  DetailSection 接入凭证（含 token 区块）
  DetailSection 安装步骤
  DetailSection 状态反馈
</page-stack>
```

**问题**：
- "已接入完成" 状态下 token 区块还是占了大半个屏幕，文案重复
- 安装步骤当前是静态 markdown，没有针对节点的 `enrollment_token` / `server_url` 做模板替换
- 缺少"当前进度"的可视化（4 个 phase 应有进度条/stepper）

## 4. 共享能力盘点

### 已就绪
- ✅ `Sparkline`（SVG，零依赖）
- ✅ `StatusGlyph` `TrendArrow` `DataTable`
- ✅ `Mono.tsx` 三件套
- ✅ Formatter：`formatPercent` `formatBytes` `formatBytesPerSecond` `formatLatency` `formatUptime` `formatDateTime` `formatNumber` `formatLabelList`

### 缺失
- ❌ **图表库**：`web/package.json` 无 recharts/visx/d3/echarts。设计语言 §12 明确写 **不引入图表库**，sparkline 用纯 SVG
- ❌ 没有"双指标叠加" / "面积图" / "Y 轴标尺" / "时间轴" 等更复杂的 SVG 原语
- ❌ 没有 `MonoDigits/Hostname/Timestamp` 在节点页的实际使用（grep 见 0 处）
- ❌ NodesPage 没用 `DataTable`（自渲染 `.resource-table`）

## 5. 设计语言关键约束

来自 `docs/design/v2-houfeng/design-language.md`：

- **气质**：冷静、克制、高密度、工程师长期使用友好（§1.3）
- **空间**：区块间距 `--space-5`(20px)；卡片 padding `--space-3~4`(12-16px)（§4.1）
- **行高**：紧凑 36px / 标准 44px（§4.2）
- **KPI 形态**：单值大卡 → **stat-strip 5 列等宽**（数值+delta+TrendArrow），不再"一个数字一张大卡"（§4.3）
- **页面 5 级层级**：hero → 当前问题 → 趋势/上下文 → 历史/事件 → 危险区（§4.4）
- **状态优先级**：critical > alert > notice > normal > maintenance > offline（§6.3）
- **Loading**：不做骨架屏 / 不用 spinner，用 mono 文案 + ghost row（§7.1）
- **不做的事**（§12）：
  - 不引入图表库（recharts/echarts）— sparkline 用纯 SVG
  - 不改 API、数据形状、retention
  - 不做响应式（单用户桌面工程工具）

## 6. v2 已规约的页面模板（重要！）

`docs/design/v2-houfeng/component-spec.md` §五 **已经写了新版 NodesPage / NodeDetailPage / NodeOnboardingPage 的目标形态**：

### NodesPage（§5）
1. Section heading + 「新建节点」按钮（**不是整行 ghost button**）
2. 可选创建表单（page-panel）
3. 视图切换：**segmented control**（不是大卡）
4. **DataTable**（density compact）：`[StatusGlyph, 节点(Hostname+名字), 位置, 标签, 当前主问题, 心跳(Timestamp), 操作]`
5. 行 hover：操作列显示 ghost 按钮（**不再常驻**）
6. 行点击：导航到节点详情

### NodeDetailPage（§5）
1. Hero：名 + 状态 Badge + **4 hero meta card**（标签/心跳/同步/主问题）
2. （可选）绑定冲突 DetailSection
3. Summary grid：3 卡
4. DetailSection 标签与备注
5. DetailSection 运行控制 + ActionConfirmationCard
6. DetailSection 生命周期
7. DetailSection 当前主机指标：**4 metric-card 各含 [label · MonoDigits · Sparkline (12-24h)]**
8. DetailSection 近期趋势：**4 卡 + 各自 Sparkline**
9. DetailSection 当前异常：IncidentList
10. DetailSection 事件：EventList timeline

### NodeOnboardingPage（§5）
- "安全感重"：cardRole='warning' ribbon top critical
- token 一次性显示：mono + 复制 + 倒计时 + 「已保存关闭」按钮
- 关闭后无法再获取：dim card + critical 文案再次警示

## 7. 当前实装与 v2 spec 漂移点（核心）

| 漂移项 | 当前 | v2 目标 | 优先级 |
|--------|------|---------|--------|
| NodesPage 不用 DataTable | `.resource-table` 自渲 | `<DataTable>` + StatusGlyph 列 | P0 |
| NodesPage 行内常驻多按钮 | 标签编辑/进入维护/暂停监控 全显 | hover 才显操作列 ghost 按钮 | P0 |
| NodesPage 视图切换占 hero | 大卡按钮组 | segmented control | P1 |
| NodeDetail 主机指标无图 | 纯数字 dl | 每卡 [label · MonoDigits · Sparkline 12-24h] | P0 |
| NodeDetail 趋势只画 3 条 | Load5/iowait/steal | 4 卡全画图 | P0 |
| NodeDetail 趋势第一卡是元信息 | "样本概览" | 应为真实趋势 | P0 |
| Mono 包装未使用 | 普通字体 | MonoDigits/Hostname/Timestamp 必须包 | P1 |
| Onboarding 无 stepper | 三个 KPI 卡描述 phase | 4-phase 进度可视化 | P2 |
| Onboarding 安装步骤静态 | 不替换 token/server_url | 模板替换 + 复制按钮 | P1 |

## 8. 列表页趋势可视化的可行性

要在列表行内画 sparkline 需要每行至少 24 个数据点，方案：

**A. 后端扩 `/api/nodes` 返回 `recent_metrics_summary?: { load5_series: number[], cpu_series: number[] }`**
- Pro：一次拉全；前端简单
- Con：列表 N 个节点 × 24 点 × M 指标，payload 增长；retention 一致性问题

**B. 前端 N+1 懒加载：列表加载完成后异步并发拉每个节点 `/runtime-facts`，数据返回逐行渲染 sparkline**
- Pro：复用现有接口，零后端改动
- Con：N 个节点 = N 次请求；可能给后端压力（N=2 没事，N=50 就要批量）

**C. 后端新增聚合接口 `GET /api/nodes/sparklines?metrics=load5,cpu&window=24h`**
- Pro：批量、低耦合
- Con：要新写 handler + repo + 测试，不在当前任务初衷

→ 需要跟用户对齐这个抉择。

## 9. 关键引用

- 设计语言：`docs/design/v2-houfeng/design-language.md`
- 组件规范：`docs/design/v2-houfeng/component-spec.md`（§五已规约目标页面形态）
- v1 baseline 业务：`docs/design/v1-baseline/architecture-data-model.md` §节点（Node = 服务器，健康状态派生）
- 数据接口：`internal/center/http/handlers/runtime_facts.go`、`internal/center/store/runtime_facts.go`
- 后端 SQL 限制：`limit 288` 即 24h × 5min
