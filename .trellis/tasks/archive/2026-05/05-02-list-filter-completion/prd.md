# Front-end list-page filter completion (parent task)

> Parent task / coordination only. 实施落到 3 个 child task。

## Goal

补齐 3 个前端 list page 的筛选完成度，闭合 V1 收口期最后一类 P0 工作（实证 root cause of "实现连 V0.1 都不到"）。

## Context

来源：`docs/release/next-phase-plan.md` Stage 1 P0 第 1 项 + `docs/release/v1-gap-checklist.md` 4 个 Partial 行中的 3 个。

用户方法论指引（2026-05-02）："早期项目、根本没有用户、按功能合理推进、无需考虑兼容"——这意味着可以激进抽象、可以重写现有半成品。

## Decision (ADR-lite) — workstream organization

**Context**: 3 个 list page 都需要 filter；如果各自独立实现，会有大量重复 UI / 状态管理代码。

**Decision**: 1 parent + 3 child task：
- **child 1**: `Build shared FilterBar component + apply to TargetsPage` —— TargetsPage 是 from scratch 最干净起点；同时抽出 `<FilterBar>` / `<FilterChip>` / `<FilterSelect>` / `<FilterToggle>` 通用组件落到 `web/src/components/filters/`
- **child 2**: `Apply FilterBar to NodesPage` —— 直接重写现有 2 toggle（按 "无需兼容"），按新通用 pattern 实现 §6.3 全部 7 项筛选
- **child 3**: `Apply FilterBar to EventsPage` —— 增量补 §10.9 缺失 4 项（backfill toggle / 时间 segmented / 时间分组 / 加载更早分页）

依赖：child 1 → (child 2 + child 3 可并行)

**Consequences**:
- 1 次抽象 + 3 次套用 = 最少重复
- 共用组件成为前端 design system 一员（atoms 之外，独立 filters/ 子目录）
- 每 child 独立 commit + review，符合 "一口一口吃"
- parent 在 3 child 全部 archive 后归档

## Implementation Plan

| Slug | Title | 依赖 | 输出物 |
|---|---|---|---|
| `child 1: filterbar-and-targets` | Build shared FilterBar + apply to TargetsPage | 无 | `web/src/components/filters/*` 新组件 + TargetsPage 6 项筛选 |
| `child 2: filterbar-nodes` | Apply FilterBar to NodesPage | child 1 | NodesPage 7 项筛选（删除现有 2 toggle 半成品） |
| `child 3: filterbar-events` | Apply FilterBar to EventsPage | child 1 | EventsPage 增 4 项筛选 |

Parent 完成判定：3 child 全 archive。

## Out of Scope

- list page 之外的 UI 改动
- 业务后端改动（如果需要新 API 参数支持筛选——按 child 内具体决策；优先在前端用现有 API + client filter 解决）
- 长 page 文件拆分（gap #11，独立 task）
- NodesPage createNode bypass 修复（gap #10，独立 task 或并入 child 2）
