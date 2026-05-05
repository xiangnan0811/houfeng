# Targets 全栈 watchtower 改造（核心二阶段）

## Goal

把 TargetDetailPage + TargetsPage 对齐节点线已完成的 watchtower 模板（身份条 + 危险区 + 折叠次要 + 历史抽屉 + 列表 latency sparkline）。Target 无 CPU/Mem/Disk 主机指标，用 ProbeItem latency_ms 作为核心趋势指标。Dashboard AbnormalTargetList 跳过（已 DataTable 90% 对齐度）。

## Background

- 节点 watchtower 三阶段全闭环（详情 → 列表 → Dashboard + follow-up 小债清零）
- 前置 targets 改造（05-04-redesign-targets-pages，commit bba86ff）已完成 Mono / DataTable / LatencyTrends MetricChart sparklines
- TargetDetailPage 当前仍是 hero-panel 身份区 + DetailSection 堆叠，未做 watchtower 布局重构
- TargetsPage 已是 DataTable 9 列，无 sparkline strip
- MetricChart / Drawer 原子已就绪（watchtower 任务建立）
- 后端 node_sparklines.go 模式可直接 copy-paste-adapt 为 target_sparklines.go

## Decisions

| ID | 决策 |
|---|---|
| Q-SCOPE | **B**（核心二阶段：TargetDetailPage + TargetsPage；Dashboard 跳过） |
| Q-SPARKLINE | **B**（单指标 avg latency；24 点 hourly avg） |
| Q-PR-SPLIT | **A**（2 PR：PR1 详情页 / PR2 后端 + 列表） |

## Requirements

### PR1：TargetDetailPage watchtower 对齐

参考 NodeDetailPage watchtower 模式，逐块改造 TargetDetailPage（~1127 行）：

#### ① 身份条替换 hero-panel

删除 `<section className="hero-panel">` 及内嵌 TargetHero 调用，改为 watchtower-header 紧凑 2 行：

- 行 1：target.name h1 + 4 StatusBadge（run_status / current_health / target_type）+ 数据新鲜度（`最近成功 <Timestamp> · 最近失败 <Timestamp>`）
- 行 2：mono 元数据条 — `<Hostname>{target_id}</Hostname>` · `<Hostname>{host}:{base_port}</Hostname>` · 标签 · 执行节点标签
- 右上 sticky：ghost "查看历史" button（触发抽屉）+ "…" 操作 popover（进入维护 / 暂停 / 恢复 / 归档 — 复用现有 `targetRuntimeActions` 逻辑）

新建 composite `web/src/components/target-detail/TargetWatchtowerHeader.tsx`。

删除旧 `TargetHero.tsx` + 对应测试。更新 barrel `target-detail/index.ts`。

#### ② 危险区前置（条件性）

`target.current_active_incident_count > 0` 时才渲染：

```tsx
<Card cardRole="warning" ribbonPlacement="top" className="watchtower-danger">
  <h2>{target.current_primary_issue_summary}</h2>
  <p>共 <MonoDigits>{count}</MonoDigits> 个活跃异常 · 健康状态 <StatusBadge> · 持续 <Timestamp mode="relative" /></p>
  <Button variant="ghost" onClick={openHistory}>查看完整时间线 →</Button>
</Card>
```

duration 从 `state.incidents[0]?.started_at` 派生。

#### ③ LatencyTrends 对齐 watchtower-metric-card 布局

LatencyTrends 已有每 probe_item 一张 MetricChart 360×140 sparkline（来自 bba86ff PR3）。当前用 `.metric-grid` 布局 + metric-card。**保留 sparkline 逻辑不改**，只换 CSS class 为 `.watchtower-metrics` 栅格（4 列 × N 行，probe_item 数决定行数），每张 `.watchtower-metric-card`：

```css
.watchtower-metrics {  /* 复用 NodeDetailPage 同款 */
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
}
```

#### ④ 次要信息折叠

从页面主体中把 TargetProbeList / TargetLabelsAndNote / 生命周期动作 包进 3 个 `<details className="watchtower-secondary">`：

- 标签与备注（TargetLabelsAndNote）
- ProbeItem 列表（TargetProbeList，原有完整卡片栈 + observation DataTable 保留）
- 生命周期（归档 / 恢复 + ConfirmationCard）

#### ⑤ 历史抽屉

复用 `<Drawer>` 原子。Tabs：事件时间线（EventList，已有 `/api/events`）+ 历史异常（IncidentList，已有 `/api/incidents` + `listHistoricalIncidents` 复用 watchtower PR3 建立的后端 include_resolved 参数）。

#### ⑥ 页面底部数据快照行

同 NodeDetailPage 风格：`数据快照时间：<Timestamp absolute>，刷新页面获取最新。`

#### 删除

- `TargetHero.tsx` + `.test.tsx`
- `TargetStatusSummary.tsx` + `.test.tsx`（KPI 信息吸收到身份条）

保留：`TargetProbeList.tsx` / `TargetLatencyTrends.tsx` / `TargetLabelsAndNote.tsx` / `TargetRuntimeControls.tsx` / `TargetActiveIncidents.tsx` / `TargetRecentEvents.tsx`。

#### 测试

TargetDetailPage.test.tsx 既有断言更新 + 新增 ≥3 用例（危险区条件显隐 / 次要 details collapsed / 8 张 LatencyTrends 卡渲染）。

### PR2：后端 sparklines + 列表 latency strip

#### 后端

新建 `internal/center/store/target_sparklines.go` + `internal/center/http/handlers/target_sparklines.go`：
- `GET /api/targets/sparklines?metrics=latency&window=24h&downsample=24`
- 从 `probe_observations` 表按 target_id group，avg latency_ms per hourly bucket
- 返回 `{"targets": {"tg_001": {"latency": [12.5, 13.0, ...]}, ...}}`
- router 注册：`/api/targets/sparklines`（同节点 sparklines 模式，加 protect 中间件）
- Go 测试 ≥2 用例

前端 `web/src/lib/api.ts`：
```ts
export function listTargetSparklines() {
  return requestJSON<TargetSparklinesResponse>(
    '/api/targets/sparklines?metrics=latency&window=24h&downsample=24'
  )
}
```

#### 前端 TargetsPage 加 latency sparkline 列

现有 9 列中加趋势列（可替换某列或插入）。推荐替换"最近成功/失败"列（信息移入身份列），原位放趋势列。

趋势列：1 个 64×14 sparkline + 当前延迟 mono 值。阈值：≤10ms → accent / ≤200ms → notice / ≤1000ms → alert / >1000ms → critical。

Sparklines 数据加载同 NodesPage 模式：`useEffect` + `listTargetSparklines()` + silent fail fallback "—"。

#### CSS

pages.css 追加 latency threshold tone 的样式（Sparkline tone class 已支持 accent/notice/alert/critical）。

## Acceptance Criteria

### PR1 TargetDetailPage
- [ ] 身份条 2 行 + 数据新鲜度 + 右上 sticky 操作
- [ ] 危险区条件性渲染
- [ ] LatencyTrends 卡用 `.watchtower-metric-card` 布局
- [ ] 次要信息 3 个 `<details>` collapsed
- [ ] 历史抽屉（events + incidents tabs）
- [ ] 删除 TargetHero / TargetStatusSummary
- [ ] 现有功能零回归
- [ ] lint / test / build 全绿（基线 382）
- [ ] 新增 ≥3 用例

### PR2 Backend + List
- [ ] `/api/targets/sparklines` 可用
- [ ] Go 测试 ≥2 用例
- [ ] TargetsPage latency sparkline 列
- [ ] 数据加载中/失败/缺失 fallback "—"
- [ ] lint / test / build 全绿
- [ ] make verify-go 全绿

## Out of Scope

- Dashboard AbnormalTargetList（对齐度 90%，跳过）
- ProbeItem 创建/编辑表单视觉重做
- 后端 schema / retention 变更
- 节点页面 / 其他页面改造（已闭环）

## Technical Approach

2 PR 拆分。每个 PR 独立闭环可验证。

PR1 参考模板：NodeDetailPage watchtower (commit 67cd668) 的 `NodeWatchtowerHeader.tsx` / `NodeDetailPage.tsx` watchtower 布局。
PR2 参考模板：`node_sparklines.go` + `NodesPage.tsx` sparkline strip 列。

所有新组件复用既有 atoms（MetricChart / Drawer / Sparkline / Mono / DataTable）。
