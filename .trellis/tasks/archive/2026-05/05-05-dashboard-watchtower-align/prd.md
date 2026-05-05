# Dashboard 节点概览块对齐 watchtower 模板

## Goal

Dashboard AbnormalNodeList 身份列对齐新版 NodesPage（2→3 行 + freshness）+ 异常节点行加 CPU/Mem/Disk 趋势 sparkline strip。实施顺序 3️⃣ 收尾。

## Background

- Dashboard 已迁 DataTable + Mono + Hostname（05-04-redesign-dashboard，commit 5a85db5）
- 节点列表刚完成 3 行身份列 + 趋势 sparkline strip（05-05-nodes-list-watchtower-align，commit 78fbfb4）
- `/api/nodes/sparklines` 接口已可用
- Dashboard AbnormalNodeList 当前 6 列，身份列 2 行（Hostname + display_name），无 freshness 行，无趋势列

## Decision

- **Q-SPARKLINE** — **B（完整对齐：身份列 3 行 + 调用 sparklines 接口给异常节点加趋势 strip）**
  - 复用 NodesPage 同款模式：身份列三行 + 趋势列 3×64×14 sparkline strip
  - Dashboard 异常节点 ≤10 个，额外调 sparklines 一次很轻量
  - sparkline strip 在首页快速诊断"异常节点当前状态是在恶化还是恢复"价值大

## Requirements

### 单 PR 改造 `AbnormalNodeList` 函数

文件：`web/src/pages/DashboardPage.tsx`（AbnormalNodeList 约 ~90 行）

#### 身份列从 2 行扩为 3 行

当前：
```tsx
<div className="dashboard-table__identity">
  <Hostname truncate maxChars={14} className="dashboard-table__id">{node.node_id}</Hostname>
  <span className="dashboard-table__display-name">{node.display_name}</span>
</div>
```

改为：
```tsx
<div className="dashboard-table__identity">
  <Hostname truncate maxChars={14} className="dashboard-table__id">{node.node_id}</Hostname>
  <span className="dashboard-table__display-name">{node.display_name}</span>
  <span className="dashboard-table__freshness">
    心跳 <Timestamp value={node.last_heartbeat_at} mode="relative" />
  </span>
</div>
```

注：DashboardNodeSummary 有 `last_heartbeat_at` 但没有 `last_sync_at`，所以 freshness 行只显示心跳（不显示同步）。

#### 新增"近 24h"趋势列

在现有列定义中加一列（可在 `issue` 列之后、`actions` 列之前）或替换某列。Dashboard 现有 6 列：`glyph / identity / location / issue / heartbeat / actions`。**heartbeat 列删除**（信息已合并到身份列 freshness 行），原位放趋势列。

新 6 列：`[glyph | identity(3行) | location | issue | trends(~220px) | actions]`

趋势列 JSX：直接拷贝 NodesPage 同款趋势列 render 函数（CPU + Mem + Disk，3×64×14 sparkline + mono 当前值 + 阈值 tone）。

#### Sparklines 数据加载

在 AbnormalNodeList 内加：
- import `listNodeSparklines` `NodeSparklinesResponse` `Sparkline` `MonoDigits` `formatPercent`
- `useState<NodeSparklinesResponse | null>(null)` 
- `useEffect`：仅在 `nodes.length > 0` 时调 `listNodeSparklines(['cpu_usage_pct', 'mem_used_pct', 'disk_used_pct'])`，silent fail
- 趋势列 render 消费 `sparklines?.nodes?.[node.node_id]`

#### CSS

`web/src/styles/pages.css` 追加（在既有 `.dashboard-table__*` 段之后）：

```css
.dashboard-table__freshness {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: 10px;
  color: var(--text-muted);
  line-height: 1.3;
}
.dashboard-table__trends {
  width: 220px;
  min-width: 220px;
}
.dashboard-table__trend-strip {
  display: inline-flex;
  align-items: flex-end;
  gap: var(--space-2);
}
.dashboard-table__trend-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
}
.dashboard-table__trend-value {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: 9px;
  color: var(--text-secondary);
  line-height: 1;
}
.dashboard-table__trends-empty {
  font-size: var(--type-small-size);
  color: var(--text-muted);
  font-style: italic;
}
```

#### 测试

`web/src/pages/DashboardPage.test.tsx`：更新 selector（原 heartbeat 列断言改为检查 freshness 行）+ 新增 ≥2 用例：
- 身份列 freshness 行含心跳 Timestamp
- sparklines 加载后趋势列渲染 polyline

mock `listNodeSparklines` 同 NodesPage.test.tsx 模式（`vi.mock` 默认返回 `{ nodes: {} }`）。

### 不动

- AbnormalTargetList（Target watchtower 改造未做，Dashboard 目标行留到后续）
- Stat strip / Hero / Events
- DashboardPage 主容器

## Acceptance Criteria

- [ ] AbnormalNodeList 身份列 3 行（Hostname / display_name / 心跳 freshness）
- [ ] 原 heartbeat 独立列删除
- [ ] 异常节点行趋势列含 3×64×14 sparkline strip + mono 当前值 + 阈值 tone
- [ ] Sparklines 加载中/失败/节点缺失 → 趋势列 "—"
- [ ] 现有功能零回归（stat strip / 异常目标 / 事件 / 加载/错误/fresh-install）
- [ ] lint / test / build 全绿（基线 380）
- [ ] 新增 ≥2 用例

## Out of Scope

- AbnormalTargetList 趋势列（留到 Target watchtower 改造）
- 后端改动
- 其他页面

## Technical Notes

- 改造文件：`web/src/pages/DashboardPage.tsx` + `DashboardPage.test.tsx` + `web/src/styles/pages.css`
- 复用：`listNodeSparklines()` / `<Sparkline>` / `<MonoDigits>` / `<Timestamp>` / `<Hostname>`
- 模板参考：`NodesPage.tsx` 身份列 3 行 + 趋势列 render（可 copy-paste-adapt）
