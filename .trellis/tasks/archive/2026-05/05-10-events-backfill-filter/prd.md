# Events backfill filter

## Goal

关闭 `docs/release/v1-gap-checklist.md` 中 `Events advanced filters` 的 backfill 维度缺口：后端 `/api/events` 支持筛选与补传观测有关的事件，前端 `EventsPage` 的“包含补传事件”控件从禁用提示变成真实可用筛选，并同步更新 v2 组件规范与 gap 状态。

## What I already know

- 当前仓库 `main` 干净，上一轮 Asset Ledger 计划已审计闭合。
- 用户要求继续推进，并要求遵守 Trellis 与 feature branch -> PR -> CI -> merge -> sync local main 的流程。
- 用户此前明确要求不使用 subagent；本任务在主会话直接执行，但仍按 Trellis task / PRD / context / finish-work 流程落地。
- `docs/release/v1-gap-checklist.md` 的 `Events advanced filters` 当前是 Partial，剩余明确缺口之一是后端事件 API 尚无 backfill 维度。
- `docs/design/v2-houfeng/component-spec.md` 当前写明 `include_backfilled` 后端不支持，只能禁用。
- `web/src/pages/EventsPage.tsx` 已有 `include_backfilled` 状态字段，但解析、normalize、URL、API 请求均强制置 `false`，控件 disabled。
- `/api/events` 目前由 `internal/center/http/handlers/events.go` 解析 query，再调用 `store.EventsFilter` / `PostgresDashboardRepository.ListEvents`。
- `state_change_events` 自身没有 `is_backfilled` 列；补传事实存在于 `node_heartbeats`、`host_samples`、`probe_observations`。
- Backfilled 观测的规则是：数据落库，但不 retroactively trigger notification / incident；因此事件流的 backfill 筛选应作为运维排查维度，不能改变 incident 语义。

## Assumptions

- `include_backfilled=true` 表示“事件列表额外包含与 backfilled runtime facts 相关的事件”；默认事件流排除可关联到补传事实的事件，兑现前端控件一直表达的“包含补传事件”语义。
- 由于 `state_change_events` 没有直接 backfill 标记，本任务可以用 event payload 或关联 runtime facts 的存在性建立筛选语义；不做 schema migration，除非实现过程中发现不可避免。
- 本任务只关闭 backfill 维度，不做 v2 建议的高级筛选 Drawer / chip flow 大改版。

## Requirements

- 后端 `/api/events` 增加 `include_backfilled` boolean query 参数。
- `include_backfilled` 解析失败时返回 400，错误语义与现有 `notification_only` / `recovery_only` / `maintenance_only` 一致。
- `store.EventsFilter` 增加 backfill 维度，并在 `ListEvents` 查询中生效。
- 默认事件列表排除可关联到补传 runtime facts 的事件；`include_backfilled=true` 解除该排除。该语义变化必须在文档和测试中明确记录。
- 前端 `EventListFilter`、`listEvents`、`EventsPage` 串起 `include_backfilled`：
  - URL 参数使用 `include_backfilled=1`
  - API 参数使用 `include_backfilled=true`
  - chip 支持展示和移除
  - 重置筛选会清除 backfill 维度
  - invalid URL 参数仍会被 canonicalize 掉
- 删除“待后端支持”的禁用态，控件可操作。
- 更新 `docs/design/v2-houfeng/component-spec.md` 的 EventsPage contract。
- 更新 `docs/release/v1-gap-checklist.md` 的 `Events advanced filters` 状态说明：backfill API/UI 维度关闭；Drawer/chip flow 如仍未做，应独立保留为后续 UX scope，不能误写为完成。
- 增加/更新后端 handler/store/router 或 API tests、前端 API/page tests。

## Acceptance Criteria

- [ ] `GET /api/events?include_backfilled=true` 能被 handler 解析到 `store.EventsFilter`。
- [ ] `GET /api/events?include_backfilled=bad` 返回 400。
- [ ] `PostgresDashboardRepository.ListEvents` 对 backfill 维度有测试覆盖，SQL 和参数可验证。
- [ ] `listEvents({ include_backfilled: true })` 序列化为 `/api/events?...&include_backfilled=true`。
- [ ] `EventsPage` 初始 URL `include_backfilled=1` 会请求后端并展示 chip。
- [ ] 用户可以通过“包含补传事件” toggle 应用筛选，并能重置/移除。
- [ ] 设计规范和 gap checklist 已同步。
- [ ] `git diff --check` 通过。
- [ ] 相关 Go/Web 测试通过；最终运行与范围匹配的质量门。

## Definition of Done

- Work committed on a non-main branch.
- Trellis task archived and journal recorded after work commits.
- PR opened, PR CI monitored until green, then merged.
- Local `main` synced to `origin/main`.
- Post-merge main CI monitored to success.

## Out of Scope

- 不做真实 Telegram 交付验证。
- 不做 command result durability / notification channel model / Docker boundary / modal focus 管理。
- 不把 `/api/events` 返回结构从 bare array 改成 `{items: []}`。
- 不做 EventsPage Drawer 或全面 chip-flow 改版。
- 不新增 runtime facts 写入路径或 incident 评估语义。

## Technical Notes

- Backfill facts:
  - `node_heartbeats.is_backfilled`
  - `host_samples.is_backfilled`
  - `probe_observations.is_backfilled`
- Event query entrypoints:
  - `internal/center/http/handlers/events.go`
  - `internal/center/store/dashboard.go`
  - `internal/center/http/handlers/events_test.go`
  - `internal/center/store/dashboard_test.go`
- Frontend entrypoints:
  - `web/src/lib/types.ts`
  - `web/src/lib/api.ts`
  - `web/src/lib/api.test.ts`
  - `web/src/pages/EventsPage.tsx`
  - `web/src/pages/EventsPage.test.tsx`
- Docs:
  - `docs/design/v2-houfeng/component-spec.md`
  - `docs/release/v1-gap-checklist.md`
