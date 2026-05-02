# Apply FilterBar to EventsPage (child 3 of list-filter-completion)

> Final child. **增量补 4 项**（不是 from-scratch FilterBar adoption）。

## Goal

EventsPage 增 4 项缺失功能（per reassess batch 3 finding + §10.9 / v2 设计稿）：
1. **含 backfill** boolean toggle（4th boolean，与现有 notification/recovery/maintenance 并列）
2. **时间 segmented control**（24h / 7d / 30d / 自定义；自定义展开现有 created_from/created_to date picker）
3. **时间分组**（事件按 today / yesterday / 本周 / 更早 分组渲染）
4. **加载更早事件 ↓** 分页（增量 fetch + append）

## What I already know

### EventsPage 现状（实读 web/src/pages/EventsPage.tsx）

- 已有 7+3 filter：object_type / severity / event_type / limit / created_from / created_to / label + notification_only / recovery_only / maintenance_only
- state 用 useState（**不**用 URL query string，与 Targets/Nodes 不同——但本任务 scope 不强制改造此点）
- `buildFilterQuery()` 把 FilterState 映射 EventListFilter 给 `listEvents()` 调用
- 列表 render 是平铺时间倒序，**无分组**
- limit 是单值（默认 50），无 load-more 概念

### 待 sub-agent 实地决策的点

- **backend `is_backfilled` filter 支持**：sub-agent 需读 `web/src/lib/types.ts` EventListFilter + `internal/center/http/handlers/events.go` 看是否已有 is_backfilled query param
  - 有 → server-side filter（最佳）
  - 无 → 本任务**不**改后端，client-side filter 显示 toggle（限制：client-side filter 只过滤当前已 fetch 的批次）
- **load-more cursor**：是否后端已支持 offset / cursor / before_id？sub-agent 实地看 events handler

### 复用 child 1/2 模式

- FilterToggle 用于 "含 backfill" 第 4 boolean
- 时间 segmented control 可用 atoms/Tabs（已有）或新建 SegmentedControl（视情况，建议复用 Tabs）
- 时间分组 render 用 useMemo 对 events 数组分组
- load-more 按钮用 atoms/Button

## Requirements

1. **含 backfill toggle**：
   - 加到现有 3 boolean 后面（FilterToggle 或仿现有 boolean checkbox 风格——保持一致）
   - server-side 优先（如 backend 支持）；否则 client-side 注释清楚

2. **时间 segmented control**：
   - 4 选项：24h / 7d / 30d / 自定义
   - 选 24h/7d/30d 自动设 created_from = now - X，created_to = now，禁用 date input
   - 选自定义 显示现有 created_from/created_to date picker
   - 默认选项：参考 baseline（如 24h，与设计稿一致）

3. **时间分组**：
   - 事件按 created_at 分组：今天 / 昨天 / 本周 / 更早
   - 每组 header 显示 "今天 N" 等 count 摘要
   - 组之间加间距（v2 设计令牌 `--space-6`）

4. **加载更早事件**：
   - 列表底部显示 "加载更早事件 ↓" 按钮
   - 增量 fetch（增加 limit 或加 before_created_at param，看 backend 支持）
   - append 到现有 events 数组
   - 已加载到底部时按钮禁用 + 文案 "无更多事件"

5. **不改 EventsPage filter 现有 7+3 项的 UI 结构**（最小入侵）；仅追加 4 项功能

6. 加 page test：1-2 个新功能测试（如 segmented "7d" 设置 created_from / load-more 按钮 click 触发 fetch）

7. `make verify-web` 全绿

## Acceptance Criteria

- [ ] EventsPage 4 项新功能可见可用
- [ ] backfill toggle 实现（server 或 client-side，文档清楚）
- [ ] 时间 segmented 切换正确设置 created_from/created_to
- [ ] 时间分组按 today/yesterday/this week/earlier 渲染
- [ ] load-more 按钮工作（点击 fetch + append）
- [ ] 现有 7+3 filter 保持工作
- [ ] EventsPage 业务保持（loading/error 状态、reset、submit 都不动）
- [ ] `make verify-web` 全绿
- [ ] git diff 范围只在 `web/src/pages/EventsPage.tsx` + `web/src/pages/EventsPage.test.tsx` (可能 + `web/src/lib/types.ts` 如要扩 EventListFilter)（如发现需改 backend → 停下报告，不扩 scope）

## Out of Scope

- 重构 EventsPage state 用 URL query string（与 Targets/Nodes 不一致，但本任务不改造）
- 后端 events handler 改动（如 backfill / cursor 不支持，client-side 解决 + 留 follow-up）
- 长 page 文件拆分
- list-filter-completion parent task archive（main agent 在本 child commit 后处理）

## Final Confirmation

**Goal**: 增 4 项 EventsPage 功能，不动后端，不改现有 filter UI。
**Approach**: 一个 trellis-implement sub-agent，~2-3h。
**Plan**: PR1 = sub-agent 改 EventsPage + test；main commit + finish-work + parent archive。
