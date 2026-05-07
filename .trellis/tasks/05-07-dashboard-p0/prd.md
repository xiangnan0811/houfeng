# Dashboard、节点列表、节点详情三页面 P0 改进

## Goal

修复三个核心页面最影响用户体验的 4 个 P0 问题，使页面更功能化、美观。

## Decisions

- **范围**：四项 P0 全部做
- **Dashboard 正常态**：方案 B — 展开 `group_summaries` 为分组健康总览表，信息量偏多，后期可删减
- **Watchtower 指标分层**：方案 C — 8 张图全保留，按异常优先级排序 + ribbon 高亮

## Requirements

### 1. 批量操作入口修正（NodesPage）
- 批量操作栏不再依赖 `groupFilterActive`，改为 `filteredNodes.length > 0` 即显示
- 全选 checkbox + 进入维护/退出维护/暂停/恢复/执行命令按钮对任何筛选结果集可见

### 2. 节点列表加排序（NodesPage + DataTable）
- DataTable atom 新增可选排序支持：`sortable?: boolean` on column + `sortState`/`onSortChange` on table
- 排序列头可点击，显示 ↑/↓ 指示符
- 排序逻辑在 NodesPage 中执行（DataTable 只负责渲染 UI）
- 排序列：节点名（display_name）、异常数（current_active_incident_count）、心跳时间（last_heartbeat_at）
- 默认无排序（保持 API 返回顺序）

### 3. Dashboard 正常态充实内容（DashboardPage）
- `RunningOverview` 主体区域新增**分组健康总览表**
- 使用 `overview.group_summaries` 数据，紧凑 DataTable 展示：
  - Group 名称 | 节点数（异常）| 目标数（异常）| 严重数 | 状态 Badge
- 行点击可跳转对应 Group 的节点列表（`/nodes?group=xxx`）
- 保留现有库存概览数字和管理入口在表格下方

### 4. Watchtower 指标优先级排序（NodeWatchtowerMetrics）
- 为每张 metric card 计算"异常分数"：
  - CPU ≥ 95%→critical(3), ≥ 80%→notice(2), < 80%→normal(0)
  - Mem ≥ 95%→critical(3), ≥ 85%→notice(2), < 85%→normal(0)
  - Disk/Inode ≥ 95%→critical(3), ≥ 80%→notice(2), < 80%→normal(0)
  - IOWait ≥ 50%→critical(3), ≥ 20%→notice(2), < 20%→normal(0)
  - Load5 ≥ 8→critical(3), ≥ 4→notice(2), < 4→normal(0)
  - Net In/Out 无阈值 → normal(0)
- 按分数降序排列，异常指标自动置顶
- 异常卡片加对应 tone 的 ribbon 高亮（notice/alert/critical）
- MetricChart tone 随异常等级自动切换

## Acceptance Criteria

### 批量操作
- [ ] 节点列表页：按地区/城市/健康状态等任意维度筛选后，全选 checkbox 和批量按钮可见
- [ ] 节点列表页：无筛选时（全量列表），批量操作栏也可见
- [ ] 批量暂停仍弹出 ActionConfirmationCard 二次确认

### 排序
- [ ] 节点列表页：点击"节点"列头可按名称排序，再次点击切换升序/降序
- [ ] 节点列表页：点击"当前主问题"列头可按异常数排序
- [ ] 排序状态有视觉指示（列头 ↑/↓ 箭头 + accent 色）
- [ ] 排序不破坏行点击导航

### Dashboard 正常态
- [ ] 无异常时首页展示分组健康总览表
- [ ] 表格每行显示 Group 名称、节点数、目标数、异常数、状态
- [ ] 行点击跳转到 `/nodes?group=xxx` 深链
- [ ] 库存概览和管理入口保持在表格下方

### Watchtower 指标
- [ ] 异常指标（CPU 高、内存高等）自动置顶
- [ ] 异常卡片有对应 tone 的 ribbon 高亮
- [ ] 正常指标保持在下方
- [ ] 维护态节点整体以 maintenance tone 展示

## Definition of Done

- TypeScript 编译通过（`cd web && npm run build`）
- ESLint 通过（`cd web && npm run lint`）
- Vitest 现有测试通过（`cd web && npm run test`）
- 视觉符合 design-language.md v2 规范

## Out of Scope

- 后端 API 改动
- 引入图表库 / CSS 框架
- 移动端适配
- 实时 WebSocket / polling 机制
- 全局搜索功能
- Dashboard 异常态/维护态改动（本次仅改正常态）

## Technical Approach

### 受影响文件
| 文件 | 改动 |
|------|------|
| `web/src/pages/NodesPage.tsx` | 批量栏条件 + 排序状态 |
| `web/src/components/atoms/DataTable.tsx` | 新增 `sortable`/`sortState`/`onSortChange` props |
| `web/src/pages/DashboardPage.tsx` | RunningOverview 新增分组健康表 |
| `web/src/components/node-detail/NodeWatchtowerMetrics.tsx` | 指标排序 + ribbon 高亮 |

### 不改的文件
- `web/src/components/atoms/MetricChart.tsx` — 已支持 thresholds/tone，无需改动
- `web/src/lib/api.ts` — 不改 API 调用
- `web/src/lib/types.ts` — 不改类型定义

### 排序实现方案（DataTable）
```typescript
// DataTableColumn 新增
sortable?: boolean
sortKey?: string  // 实际排序字段名

// DataTableProps 新增
sortState?: { key: string; direction: 'asc' | 'desc' } | null
onSortChange?: (key: string) => void

// 列头渲染：sortable 列显示可点击按钮 + ↑↓ 指示符
```

### Watchtower 排序方案
```typescript
// 每张 card 计算优先级的函数
function metricPriority(sample: HostSample): number {
  // 返回 0 (normal) / 2 (notice) / 3 (critical)
}
// 渲染前 sort cards by priority desc
// 异常 card 加 className `watchtower-metric-card--notice/critical`
```

## Technical Notes

- 设计权威：`docs/design/v2-houfeng/design-language.md`、`docs/design/v2-houfeng/component-spec.md`
- 当前代码：DashboardPage.tsx (1022行)、NodesPage.tsx (1395行)、NodeDetailPage.tsx (1272行)、NodeWatchtowerMetrics.tsx (276行)、DataTable.tsx (122行)
- 不改 API contract，所有数据来自现有 `/api/dashboard`、`/api/nodes`、`/api/nodes/:id/runtime-facts` 接口
