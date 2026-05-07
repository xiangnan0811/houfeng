# Dashboard、节点列表、节点详情三页面 P2 改进

## Goal

缩小与 Datadog / Grafana / Netdata 的核心体验差距：自动刷新、节点对比、阈值可配置。

## Decisions

- **范围**：3 项 P2 全部做
- **自动刷新**：方案 A — 30s / 60s / 5m / 关闭 四档，默认关闭，Dashboard 和节点列表各独立控制
- **阈值可配置**：方案 A — 后端 center_settings 持久化，含迁移 + API + 设置页 + 前端消费

## Requirements

### 1. 自动刷新（Dashboard + NodesPage）
- 各页面独立的下拉选择器：关闭 / 30s / 60s / 5m
- 默认关闭，用户手动选择间隔
- 使用 setInterval 定时调用现有数据加载函数
- 页面不可见时暂停（`document.visibilitychange`），可见时恢复
- Dashboard：刷新 `getDashboard()`
- 节点列表：刷新 `listNodes()`
- 组件卸载时清理 interval
- 复用现有 loading 状态（不额外显示 loading 指示），静默刷新

### 2. 节点对比视图
- 在节点列表页选中 2 个节点 → "对比选中节点"按钮
- 新路由 `/nodes/compare?id=xxx&id=yyy`
- 页面并排展示两个节点的关键指标对比：
  - 身份信息（名称、地址、状态 Badge）
  - 8 张 MetricChart 并排（CPU/Mem/Disk/Inode/Load5/IOWait/NetIn/NetOut）
  - 每个指标两张图左右对比，左节点用 accent tone，右节点用 accent-2 tone
- 两个节点的 runtimeFacts 并行拉取
- 任一节点数据为空时显示对应 empty state

### 3. 指标阈值可配置
#### 后端
- 在 `center_settings` 表新增 `metric_thresholds` JSONB 字段（或单独 migration 加行）
- 默认值与当前硬编码一致：CPU notice=80/critical=95, Mem notice=85/critical=95, Disk notice=80/critical=95, Inode notice=80/critical=95, IOWait notice=20/critical=50, Load5 notice=4.0/critical=8.0
- 新增 `GET /api/settings/metric-thresholds` 和 `PUT /api/settings/metric-thresholds` 接口
- 设置页不需要独立 section——在现有"全局默认" section 中追加阈值字段

#### 前端
- NodeWatchtowerMetrics：从 API 读取阈值替代硬编码，加载中/失败时 fallback 到默认值
- NodesPage Sparkline：从 API 读取阈值替代硬编码 tone 判断
- 设置页：在 DetailSection「全局默认」中追加 6 组阈值输入（每组 notice + critical 两个值）
- 阈值变更后无需重启，即时生效

## Acceptance Criteria

### 自动刷新
- [ ] Dashboard 有刷新间隔选择器（关闭/30s/60s/5m），默认关闭
- [ ] 节点列表有刷新间隔选择器，默认关闭
- [ ] 选择间隔后定时刷新数据
- [ ] 页面切后台暂停刷新，切回前台恢复
- [ ] 组件卸载后 interval 被清理

### 节点对比
- [ ] 节点列表批量栏："对比选中节点"按钮在选中 2 个节点时可用
- [ ] 点击后跳转 `/nodes/compare?id=xxx&id=yyy`
- [ ] 对比页并排展示两个节点的 8 张指标图 + 身份信息
- [ ] 两个节点数据独立加载，一个失败不影响另一个

### 阈值可配置
- [ ] 后端提供 metric-thresholds 读写接口
- [ ] 默认阈值写入 migration
- [ ] 设置页可编辑 6 组阈值
- [ ] NodeWatchtowerMetrics 使用接口阈值
- [ ] NodesPage Sparkline tone 使用接口阈值
- [ ] 接口不可用时 fallback 默认值

## Definition of Done

- Go 编译 + vet + test 通过
- TypeScript 编译通过
- ESLint 通过
- 现有测试通过
- 视觉符合 design-language.md v2 规范

## Out of Scope

- 移动端适配
- 引入外部库/框架
- 实时 WebSocket 推送
- 节点对比中超过 2 个节点
- 阈值中的网络指标阈值（Net In/Out 无固定阈值概念）

## Technical Approach

### 受影响文件
| 层 | 文件 | 改动 |
|------|------|------|
| 后端 | `db/migrations/` | 新 migration：metric_thresholds 字段 |
| 后端 | `internal/center/store/` | settings repository 扩展 |
| 后端 | `internal/center/http/handlers/` | 新 handler 或扩展 settings handler |
| 后端 | `internal/center/http/router.go` | 注册新路由 |
| 后端 | `cmd/houfeng-center/bootstrap.go` | 如需要 wiring |
| 前端 | `web/src/pages/DashboardPage.tsx` | 自动刷新 |
| 前端 | `web/src/pages/NodesPage.tsx` | 自动刷新 + 节点对比入口 |
| 前端 | `web/src/pages/NodeComparePage.tsx` | **新页面** |
| 前端 | `web/src/app/router.tsx` | 新路由 |
| 前端 | `web/src/components/node-detail/NodeWatchtowerMetrics.tsx` | 消费 API 阈值 |
| 前端 | `web/src/pages/SettingsPage.tsx` | 阈值编辑 UI |
| 前端 | `web/src/lib/api.ts` | 新 API 调用 |
| 前端 | `web/src/lib/types.ts` | 阈值类型 |

### Auto-refresh 实现方案
```typescript
// 共享 hook
function useAutoRefresh(interval: number | null, callback: () => void) {
  useEffect(() => {
    if (interval == null) return
    let id: ReturnType<typeof setInterval>
    function start() { id = setInterval(callback, interval) }
    function onVis() { if (document.visibilityState === 'visible') start() }
    function onHid() { clearInterval(id) }
    start()
    document.addEventListener('visibilitychange', onVis)
    document.addEventListener('visibilitychange', onHid)
    return () => { clearInterval(id); /* remove listeners */ }
  }, [interval, callback])
}
```

### 阈值 API contract
```json
// GET /api/settings/metric-thresholds
{
  "cpu":  { "notice": 80, "critical": 95 },
  "mem":  { "notice": 85, "critical": 95 },
  "disk": { "notice": 80, "critical": 95 },
  "inode": { "notice": 80, "critical": 95 },
  "iowait": { "notice": 20, "critical": 50 },
  "load5": { "notice": 4.0, "critical": 8.0 }
}
```

## Technical Notes

- P0: `d5280a8`, P1: `df7d0f5`
- 设计权威：`docs/design/v2-houfeng/design-language.md`、`docs/design/v2-houfeng/component-spec.md`
- 既然后端配合，需要先完成 Go 层改动再改前端
- 阈值 fallback 关键：前端必须在 API 不可用时使用硬编码默认值，不可白屏
