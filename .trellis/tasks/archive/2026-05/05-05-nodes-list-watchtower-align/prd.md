# 节点列表对齐 watchtower 模板（行内趋势 sparkline）

## Goal

让节点列表行内呈现"CPU / Mem / Disk 三指标 mini sparkline 趋势"，呼应详情页 watchtower 的"趋势是主体"理念，关闭 5 卡点中的 **C - 跨服务器对比难**。

**当前状态**：节点列表已迁 DataTable 紧凑 36px + StatusGlyph 行首 + Mono 全栈 + hover 操作（前置任务 05-03-redesign-node-pages 完成）。**唯一缺的就是行内趋势缩略 sparkline**。

## Background

- **战略反馈**（用户 2026-05-05）："很难从整体上掌握一个服务器的综合情况"
- 5 卡点：A 缺中央视图（详情页 watchtower 已解决）/ B 异常被埋（同）/ **C 跨服务器对比难（本任务）** / D 趋势是装饰（已解决）/ E 视觉装饰偷密度（已解决）
- 详情页 watchtower 已立 8 张 MetricChart 主视图为模板；列表应呼应
- 数据契约现状：`/api/nodes` 仅返 `NodeRecord[]`（无时序）；`/api/nodes/{id}/runtime-facts` 返 288 样本

## Decisions

| ID | 决策 |
|---|---|
| Q-DATA | **C** — 后端新增 `/api/nodes/sparklines?metrics=cpu_usage,mem_used,disk_used&window=24h&downsample=24` |
| Q-METRIC | **C** — CPU + Mem + Disk 三指标 strip，列宽 ~200px |
| Q-COLUMN | **B** — 替换"心跳·同步"列为"趋势"列；心跳同步移入节点身份列第三行 mono 小字 |
| Q-PR-SPLIT | **A** — 2 PR：PR1 后端 sparklines 接口 / PR2 前端 NodesPage 列改造 |

## Requirements

### PR1：后端 sparklines 聚合接口（Go）

#### 新 handler

`POST` 或 `GET` `/api/nodes/sparklines?metrics=cpu_usage,mem_used,disk_used&window=24h&downsample=24`

- metrics：逗号分隔，合法值按 host_observations 数值列名（cpu_usage_pct, load_5, mem_used_pct, disk_used_pct, inode_used_pct, net_in_bytes_per_sec, net_out_bytes_per_sec, cpu_iowait_pct 等）
- window：暂时固定 24h（与 runtime-facts 同源数据范围）
- downsample：暂时固定 24（24h / 24 buckets = 每 bucket 每小时平均；应从 288 样本中 GROUP BY 时 bucket）
- 返回结构：
```json
{
  "nodes": {
    "nd_001": { "cpu_usage_pct": [12.5, 13.0, ...], "mem_used_pct": [65.0, 64.2, ...], "disk_used_pct": [52.0, 52.1, ...] },
    "nd_002": { ... }
  }
}
```

可选：如后端已有"获取所有节点 ID list"便利函数，可直接从 host_observations 表 `select distinct node_id` 取，或从 nodes 表 join。

**实现要点**：
- repo 函数 `GetNodeSparklines(ctx, metrics []string, window time.Duration, downsample int) (map[string]map[string][]float64, error)`
- SQL：对每个 metric，按 node_id + time_bucket（如 `date_trunc('hour', observed_at)`）GROUP BY，AVG 聚合，出 bucket 数 ≤ downsample 个值
- 为简化：分 N 个 query 循环各 node_id（N ≤ 几十，N+1 在后端可控；prefer 单 query 用 window function 或 array_agg 更优）
- 测试：≥2 Go 表驱动用例（基本请求 / 无数据空返回）
- 文件：`internal/center/http/handlers/node_sparklines.go` + 对应 store repo

#### agentapi types / 前端 type

CSV 返回或对应 Go struct + JSON 序列化。前端 type：
```ts
export type NodeSparklinesResponse = {
  nodes: Record<string, Record<string, number[]>>
}
```

API client（`web/src/lib/api.ts`）：
```ts
export function listNodeSparklines(metrics: string[]) {
  const qs = new URLSearchParams({
    metrics: metrics.join(','),
    window: '24h',
    downsample: '24',
  })
  return requestJSON<NodeSparklinesResponse>(`/api/nodes/sparklines?${qs}`)
}
```

### PR2：NodesPage 列定义改造（React）

#### 列结构

7 列，总列数不变：
```
[Glyph | 节点(三行) | 位置 | 标签 | 当前主问题 | 趋势(~200px) | 操作 hover]
```

#### 节点身份列（列 2）从 2 行扩为 3 行

```tsx
{
  key: 'identity',
  header: '节点',
  render: (node) => (
    <div className="nodes-table__identity">
      <Hostname truncate maxChars={14}>{node.node_id}</Hostname>
      <span className="nodes-table__display-name">{node.display_name}</span>
      <span className="nodes-table__freshness">
        心跳 <Timestamp value={node.last_heartbeat_at} mode="relative" />
        {last_sync_at ? <> · 同步 <Timestamp value={node.last_sync_at} mode="relative" /></> : null}
      </span>
    </div>
  ),
}
```

新增 CSS：
```css
.nodes-table__freshness {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: 10px;
  color: var(--text-muted);
}
```

#### 删除原"心跳·同步"列（当前列 6）

原列 key: 'heartbeat' 整列移除；相应测试 selector（`screen.getByText(/24\/04\/24 09:00/` 等）改为检查节点身份列内的 `<Timestamp>` 渲染。

#### 新增"趋势"列（新列 6）

```tsx
{
  key: 'trends',
  header: '近 24h',
  className: 'nodes-table__trends',
  render: (node) => {
    const series = sparklines?.nodes?.[node.node_id]
    if (!series) {
      return <span className="nodes-table__trends-empty">—</span>
    }
    return (
      <span className="nodes-table__trend-strip">
        <span className="nodes-table__trend-item">
          <span className="nodes-table__trend-value"><MonoDigits>{formatPercent(latestCpu)}</MonoDigits></span>
          <Sparkline values={series.cpu_usage_pct} tone={cpuTone} width={64} height={14} />
        </span>
        <span className="nodes-table__trend-item">
          <span className="nodes-table__trend-value"><MonoDigits>{formatPercent(latestMem)}</MonoDigits></span>
          <Sparkline values={series.mem_used_pct} tone={memTone} width={64} height={14} />
        </span>
        <span className="nodes-table__trend-item">
          <span className="nodes-table__trend-value"><MonoDigits>{formatPercent(latestDisk)}</MonoDigits></span>
          <Sparkline values={series.disk_used_pct} tone={diskTone} width={64} height={14} />
        </span>
      </span>
    )
  },
}
```

tone 映射：CPU > 80% → alert, > 95% → critical, otherwise accent；Mem > 85% → alert, > 95% → critical；Disk > 80% → alert, > 95% → critical。

CSS：
```css
.nodes-table__trends {
  width: 200px;
  min-width: 200px;
}
.nodes-table__trend-strip {
  display: inline-flex;
  align-items: flex-end;
  gap: var(--space-2);
}
.nodes-table__trend-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1px;
}
.nodes-table__trend-value {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: 9px;
  color: var(--text-secondary);
  line-height: 1;
}
.nodes-table__trends-empty {
  font-size: var(--type-small-size);
  color: var(--text-muted);
  font-style: italic;
}
```

#### Sparklines 数据加载

`useEffect` 在 `filteredNodes` 变化时（首次加载完 + 筛选后），调 `listNodeSparklines(['cpu_usage_pct', 'mem_used_pct', 'disk_used_pct'])`，存 state。

- Sparklines 在加载中时（state 为 null）：趋势列显示 "—" placeholder
- Sparklines 加载失败：趋势列显示 "—"（或 silent fail，不阻塞列表）
- Sparklines 加载成功：逐行消费 `sparklines.nodes[node.node_id]` 渲染；对于 sparklines 缺失的 node_id（如刚创建还没观测），仍显示 "—"
- defer 加载：sparklines 不阻塞列表首次渲染（列表先出文字+StatusGlyph，sparkline 在 data 回来后再出现）

#### Mock 测试

NodesPage.test.tsx 新增 ≥3 用例：
- sparklines 加载后 3 个 mini sparkline 列渲染（查 DOM 中 polyline 数量）
- sparklines 未加载时趋势列显示 "—" placeholder
- 节点身份列包含心跳 Timestamp（原来的独立列改为子行）

mock `listNodeSparklines` 或 mock fetch 模式参照既有 test 中 mockJSONResponse 风格。

### 边界态

- 0 节点 → 空态保持不变
- Sparklines 接口失败 → 趋势列 silent fail（显示 "—"，不打断列表）
- 单样本 → Sparkline 已有单样本边界态处理（末点 + "样本不足"）
- 维护态节点 → 该行 trends 仍渲染但 tone='maintenance' 择色（由现有 tone 计算逻辑给）

## Acceptance Criteria

### PR1 Backend
- [ ] `GET /api/nodes/sparklines?metrics=cpu_usage_pct,mem_used_pct,disk_used_pct&window=24h&downsample=24` 可用
- [ ] 返回结构含 `nodes[node_id]` 每个节点每 metric 数组
- [ ] 下采样正确（24h 288 样本 → 24 个 hourly avg bucket）
- [ ] 空数据时返回 `{"nodes": {}}` 不报错
- [ ] `make verify-go` 全绿；新增 ≥2 Go 表驱动用例

### PR2 Frontend
- [ ] 列定义：`[Glyph | 节点(三行) | 位置 | 标签 | 当前主问题 | 趋势(~200px) | 操作 hover]`
- [ ] 节点身份列第三行含 `心跳 X 分钟前 · 同步 Y 分钟前`
- [ ] 趋势列含 3 个 mini sparkline 64×14 + 各自上方 mono 当前值
- [ ] Sparklines 加载中 / 失败 / 某节点缺失 时显示 "—"
- [ ] 原"心跳·同步"独立列已删除，相关测试 selector 已更新
- [ ] `cd web && TMPDIR=/tmp npm run lint && npm run test && npm run build` 全绿（基线 377）
- [ ] NodesPage.test.tsx 既有断言全过 + 新增 ≥3 用例

### 通用
- [ ] 现有功能零回归：行内编辑标签 / 维护暂停 / 绑定冲突切换 / 创建节点 / 筛选栏
- [ ] `make verify-go` 全绿

## Definition of Done

- Go 单测覆盖新接口
- TypeScript strict 0 error / ESLint 0 warning
- vitest 全 pass
- `docs/design/v2-houfeng/component-spec.md` §五 NodesPage 段如有调整同步（"心跳同步列删除 / 节点列三行 / 趋势列 mini sparkline strip"）
- 不需要 trellis-update-spec

## Out of Scope

- TargetsPage 列表对齐（同款改造，单独后续任务）
- Dashboard 节点概览块对齐（实施顺序 3，单独任务）
- 后端时间窗口动态化（`window` 与 `downsample` 参数已宣告但首期仅支持固定 24h/24）
- 引入图表库
- 列表分页 / 虚拟滚动
- 移动端响应式

## Technical Approach

2 PR 拆分：PR1 后端 sparklines 接口先立稳 contract → 单独 commit；PR2 前端 NodesPage 列改造消费 → 单独 commit。

**关键技术点**：
- SQL 下采样：对每个 metric，按 `date_trunc('hour', observed_at)` GROUP BY node_id，AVG 值聚合，limit 24 个 bucket per node
- Sparkline 64×14 是最小可用尺寸（sparkline 已支持多宽度），tone 按阈值分别
- trendValue 取 sparklines 系列最后一个值（latest）做当前值显示；如果 sparklines 最后一元素恰好跟 `NodeRecord` latest_host_sample 不同（后端的 sparklines 可能滞后一个周期），那也接受微偏差
- 后端 handler 文件：新建 `internal/center/http/handlers/node_sparklines.go`；注册在 `router.go`
- 后端 store：`internal/center/store/node_sparklines.go`

## Decision (ADR-lite)

**Context**：节点列表无法进行跨服务器对比（C 卡点）；用户需要一眼看出 30 台机器中哪几台 CPU / Mem / Disk 趋势异常。前置任务因后端接口缺失选择了"不画 sparkline"(D)，watchtower 详情页立稳后本次重新评估。

**Decision**：
1. 后端新增专用聚合接口 `GET /api/nodes/sparklines`（与 `/api/nodes` 列表接口及 `/runtime-facts` 详情接口均解耦）
2. 行内 3 指标 mini sparkline strip（CPU usage / Mem used / Disk used，24h × 24 点 hourly avg）
3. 心跳同步列删除，信息合并到节点身份列第三行
4. 2 PR 拆分（后端 contract 先稳定，前端消费）

**Consequences**：
- 收益：C 卡点完全关闭；跨服务器对比"CPU high / Mem high / Disk high"现在可在列表内完成
- 取舍：列表行高从 36px 增加到约 52px（放 3 个 sparkline 14px + 当前值 9px ~ 30px stacked），行密度降低但信息密度提升
- 风险：后端 SQL 下采样首次实现，需 Go 测试确保 24 bucket 分组正确；sparklines 接口失败不影响列表核心功能（silent fallback "—"）

## Technical Notes

- 改造文件：`web/src/pages/NodesPage.tsx`（~1100 行）+ `NodesPage.test.tsx`
- 新建后端：`internal/center/http/handlers/node_sparklines.go` + `internal/center/store/node_sparklines.go` + `.go 测试`
- 复用：`<Sparkline>` `<MonoDigits>` `<Hostname>` `<Timestamp>` `<StatusGlyph>` `<DataTable>`
- 参考：watchtower 详情页身份条 3 行模式（`NodeWatchtowerHeader.tsx`）

## Research References

- `05-03-redesign-node-pages/research/codebase-frontend.md` §8 — 原 Q1 ABC 三方案对比
- `05-05-node-detail-watchtower/prd.md` — watchtower 详情页 Q-DATA 决策对比
