# Events filter drawer

## Goal

关闭 `docs/release/v1-gap-checklist.md` 中 `Events advanced filters` 的最后一个明确 UI 缺口：把 EventsPage 的高级筛选从页面内大块表单收敛为 v2 风格的筛选入口 + active chips + Drawer 编辑流，同时保留现有 URL-state 与 `/api/events` 查询契约。

## What I already know

- 用户要求继续推进，并要求遵守 Trellis 与 feature branch -> PR -> CI -> merge -> sync local main 的流程。
- 用户此前明确要求不使用 subagent；本任务在主会话直接执行，但仍按 Trellis task / PRD / context / finish-work 流程落地。
- 当前分支是 `feat/events-filter-drawer`，从干净 `main` 创建。
- `docs/release/asset-ledger-roadmap-completion.md` 说明 Asset Ledger 计划已闭合，真实 40+ VPS 数据 deferred，不作为本任务。
- `docs/release/v1-gap-checklist.md` 当前 `Events advanced filters` 仍是 Partial，剩余 Partial 仅指 v2 建议的高级筛选 Drawer / chip flow 未系统落地。
- `docs/design/v2-houfeng/component-spec.md` 的 EventsPage contract 已列出支持的 query：`object_type`、`severity`、`event_type`、`limit`、`created_from`、`created_to`、`label`、`notification_only=1`、`recovery_only=1`、`maintenance_only=1`、`include_backfilled=1`、`time_range=24h|7d|30d|custom`。
- `EventsPage.tsx` 已有完整 parse / normalize / URL serialize / API query / chip remove / reset / load more 逻辑。
- 当前 UI 把所有筛选控件直接放在页面内，包括时间 Tabs、FilterBar、日期/标签 summary cards 和操作按钮，页面占用较高。
- 现有 `Drawer` 原子支持 inline fixed render、ESC、overlay close、左右侧、`aria-modal`。Portal/focus trap 是独立 a11y hardening follow-up，不在本任务中扩展。

## Assumptions

- 本任务只改 EventsPage 的筛选交互结构，不新增后端参数、不修改 `/api/events` 返回形状。
- Drawer 使用现有 `Drawer` 原子，不在本任务中实现 portal、focus trap 或 ChangePasswordModal 改造。
- active chips 应在页面主视图可见；Drawer 用于编辑筛选条件。
- URL 是唯一 applied filter truth；Drawer 内 draft 可以编辑但只有点击“应用筛选”后提交。
- 打开 Drawer 时应以当前 applied filters 作为 draft 基线，避免用户上一次未提交草稿污染下一次打开。

## Requirements

- EventsPage 主视图保留：
  - Hero panel。
  - 一个轻量筛选概览区，显示 active chips、清空所有、打开高级筛选 Drawer 的按钮。
  - 事件流 DetailSection 和 load-more。
- 筛选控件迁移到 Drawer：
  - 时间范围 Tabs。
  - object_type / severity / event_type / limit selects。
  - notification_only / recovery_only / maintenance_only / include_backfilled toggles。
  - custom 时间输入与 label 输入。
  - 当前 include_backfilled 状态说明。
  - “应用筛选”“重置筛选”“关闭”动作。
- 保持现有行为：
  - 首次 URL filters 仍会请求后端并展示 chips。
  - 应用筛选仍更新 URL 与 API query。
  - reset 仍清空 URL 并请求 `/api/events?limit=50`。
  - chip remove 仍立即更新 URL 与 refetch。
  - invalid URL params 仍 canonicalize 掉。
  - load-more 仍按当前 applied filters 增加 limit。
- Drawer open / close 行为：
  - 点击“高级筛选”打开 Drawer。
  - ESC、overlay、关闭按钮、Drawer 内“关闭”均可关闭。
  - 关闭但不应用时不改变 URL/API 请求。
  - 重新打开时 draft 重置为当前 applied filters。
- 更新 `docs/design/v2-houfeng/component-spec.md`，记录 EventsPage 筛选 flow 已为 overview + Drawer + chips。
- 更新 `docs/release/v1-gap-checklist.md`，将 `Events advanced filters` 从 Partial 改为 Closed，并说明 backfill 与 Drawer/chip flow 均已闭合。
- 如形成新的可执行前端契约，更新 `.trellis/spec/web/state-and-data.md`。

## Acceptance Criteria

- [x] 初始 `/events?...` URL filters 请求后端并在主视图显示 active chips。
- [x] 主视图存在“高级筛选”按钮；点击后出现 Drawer dialog。
- [x] Drawer 内编辑 object_type / limit / date / label / boolean toggles 后，点击“应用筛选”会请求对应 API，并更新 URL/chips。
- [x] Drawer 内点击“关闭”或 ESC 关闭，不提交未应用的 draft。
- [x] Drawer 内点击“重置筛选”会关闭 Drawer、清空 URL，并请求 `/api/events?limit=50`。
- [x] chip remove 仍可以在 Drawer 关闭时直接更新 URL/API。
- [x] `include_backfilled=1` 的 URL/API/chip 行为保持。
- [x] invalid URL params 仍 canonicalize。
- [x] Docs / Trellis spec 与实现同步。
- [x] `git diff --check` 通过。
- [x] 相关 Web tests 通过；最终运行与范围匹配的质量门。

## Definition of Done

- Work committed on a non-main branch.
- Trellis task archived and journal recorded after work commits.
- PR opened, PR CI monitored until green, then merged.
- Local `main` synced to `origin/main`.
- Post-merge main CI monitored to success.

## Out of Scope

- 不处理真实 40+ VPS 数据 dry-run/import。
- 不实现 full service registry / DNS provider sync / registrar sync。
- 不新增 `/api/events` 参数或改变 bare array response。
- 不改 `Drawer` 原子的 portal、focus trap、初始焦点、触发器焦点恢复；这些仍属于 gap #24 的 accessibility hardening。
- 不重做 EventList 行样式或时间分组算法。
- 不引入新的前端依赖。

## Technical Notes

- Frontend entrypoints:
  - `web/src/pages/EventsPage.tsx`
  - `web/src/pages/EventsPage.test.tsx`
  - `web/src/components/atoms/Drawer.tsx`
  - `web/src/components/filters/*`
  - `web/src/styles/pages.css`
- Docs:
  - `docs/design/v2-houfeng/component-spec.md`
  - `docs/release/v1-gap-checklist.md`
- Relevant specs:
  - `.trellis/spec/guides/branch-workflow-governance.md`
  - `.trellis/spec/guides/cross-layer-thinking-guide.md`
  - `.trellis/spec/web/index.md`
  - `.trellis/spec/web/component-conventions.md`
  - `.trellis/spec/web/state-and-data.md`
  - `.trellis/spec/web/styling-guidelines.md`
  - `.trellis/spec/web/quality-guidelines.md`
