# Stage 2: 历史数据 — 突破 24h 窗口

## Goal

把节点详情 / 目标详情页的主视图从固定 24h 扩展为三档时间窗口（24h / 7d / 30d），让用户能看历史趋势。Stage 2 MVP 第一期：只做"可切换时间窗口看趋势"，不做独立周报页。

## Background

- Stage 1 全完成（P0/P1/P2 清），385 tests
- 当前 host_observations / probe_observations 表存全量原始数据，PG 单表无需额外处理
- `/runtime-facts` SQL `limit 288` 硬编码 / sparklines 接口 24h 固定 / MetricChart 已接受任意长度数组
- 路线：A（扩容原始表窗口）→ B（未来日聚合表）渐进

## Decisions

| ID | 决策 |
|---|---|
| Q-PAIN | 历史数据与报表 |
| Q-RETENTION | A → B 渐进路线（先扩原始表窗口，好再升级聚合表） |
| Q-WINDOW | B（24h + 7d + 30d 三档） |
| Q-SCOPE | B（主机指标 + Probe 延迟都做） |
| Q-UI | A（每个详情页主视图上方 Tabs pill） |
| Q-REPORT | 暂不做独立周报页 |
| Q-PR-SPLIT | A（2 PR：PR1 后端 / PR2 前端） |

## Requirements

### PR1：后端 3 个 endpoint 加 window 参数

#### `/api/nodes/{id}/runtime-facts`

加 query param `window`：
- `24h`（default，保持现有行为）：SQL `limit 288`
- `7d`：SQL `limit 2016`
- `30d`：SQL `limit 8640`

实现：handler 解析 `window` param → `parseWindow(raw)` 返回 `(time.Duration, int limit)`。repo `GetNodeRuntimeFacts` 加 `limit int` 参数（当前硬编码 288）。

改动文件：`internal/center/http/handlers/runtime_facts.go` + `internal/center/store/runtime_facts.go`

Go 测试：≥2 用例（default 24h / 7d window 返回不同 limit）

#### `/api/nodes/sparklines`

已有 `window` 和 `downsample` query param（首期默认 24h/24）。PR1 让它真正起作用：
- `window=7d` → `since = now - 7d`，`downsample` 仍 24（weekly hourly avg 扩展为 7d hourly avg → 168 buckets）
- `window=30d` → `since = now - 30d`，`downsample` 仍 24 → 每天 avg → 30 buckets
- 或者更简单：`downsample` 保持调用方传入的默认值 24，`window` 只影响数据范围。**首期保持 downsample=24 不变**，window 越长每个 bucket 覆盖的时间段越宽——这对趋势图足够了

改动文件：`internal/center/http/handlers/node_sparklines.go` + 对应 store

#### `/api/targets/sparklines`

同上加 window 参数实际生效。

Go 测试：≥2 用例（7d / 30d 不同的 since cutoff）

### PR2：前端详情页加时间窗口 Tabs

#### NodeDetailPage

watchtower ③主视图栅格上方加：

```tsx
<Tabs<'24h' | '7d' | '30d'> variant="pill" value={timeWindow} onChange={setTimeWindow}
  options={[
    { value: '24h', label: '24h' },
    { value: '7d', label: '7d' },
    { value: '30d', label: '30d' },
  ]} />
```

`timeWindow` state 变化时：
- `getNodeRuntimeFacts(nodeId, timeWindow)` — API client 加 window 参数
- MetricChart 的 samples 数组长度随 window 变化（288 / 2016 / 8640），MetricChart 内自动适配

#### TargetDetailPage

LatencyTrends 上方加同款 Tabs。切换后调 `getTargetRuntimeFacts(targetId, timeWindow)`。

#### 不改动的

- Dashboard stat strip（保持 24h 当前语义）
- 节点列表 sparkline strip（保持 24h）
- 目标列表 sparkline strip（保持 24h）
- sparklines 接口的 downsample 参数（保持 24）

#### 加载态

切换 timeWindow 时 MetricChart 区不闪白（保持旧数据渲染，后台 fetch 新数据，loaded 后替换）。如果旧数据长度不匹配新窗口 → 显示旧数据 + loading indicator，loaded 后替换。简化处理：直接显示 loading 也行。

#### 测试

- NodeDetailPage.test.tsx：新增 ≥2 用例（Tabs 渲染 3 个 option / 切换到 7d 后 API fetch 带 `window=7d` 参数）
- TargetDetailPage.test.tsx：新增 ≥1 用例（Tabs 渲染）

## Acceptance Criteria

### PR1 Backend
- [ ] 3 个 endpoint 接受 `window` query param
- [ ] default 24h 向后兼容（现有调用方不传 `window` 行为不变）
- [ ] Go 测试覆盖 24h / 7d / 30d

### PR2 Frontend
- [ ] NodeDetailPage 主视图上方 Tabs 24h | 7d | 30d
- [ ] TargetDetailPage LatencyTrends 上方同款 Tabs
- [ ] 切换后 API 调新 window 参数
- [ ] Dashboard / 列表 sparkline 不受影响
- [ ] lint / test / build 全绿（基线 385）

## Out of Scope

- 日聚合表（路线 B，效果好再升级）
- 独立周报页 / PDF 导出
- Dashboard / 列表的时间窗口切换
- 自定义时间窗口
- 后端 schema 变更（仅 parameterize 已有接口）

## Technical Approach

2 PR 拆分。PR1 后端 contract 先稳定；PR2 前端消费。

**关键技术点**：
- `parseWindow(raw) → (time.Duration, int)` — 白名单 24h/7d/30d，unknown → 400 error 或 fallback 24h
- Go 侧 window → since = now.Add(-duration)，传入既有 SQL 的 `$2` 参数（`observed_at >= $2`）
- limit = duration / (24h/288) × 288 → 精确公式
- Tabs 位置：MetricChart 栅格与危险区之间的一个独立行，不占用 MetricChart 卡的空间
