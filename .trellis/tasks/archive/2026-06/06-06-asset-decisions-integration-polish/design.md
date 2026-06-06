# 资产决策中枢集成体验收敛 Design

## Current State

`/asset-decisions` 已经具备自动组、自定义组合、场景模板、决策记录、执行回读、执行编排、续费 evidence 和单台辅助队列。当前缺口主要在 web 集成层：

- 一些跨页入口仍裸跳 `/asset-decisions`，用户进入后无法从首屏上下文 chips 看出来源意图。
- Dashboard 和观测支撑面仍残留 `资产决策队列` / `决策队列` 文案，容易把组合中枢理解成旧单台队列。
- `dashboardLinks.ts` 仍保留 `assetDecisionsSingleQueue` 常量，后续开发容易误用。
- 视觉证据文档对资产决策页的说明没有充分表达自动组 + 场景 + 记录执行编排的当前结构。

## Boundaries

- 本任务只修改 web 层、前端测试、docs / Trellis spec。后端 assetdecisions API、store、migration 均不在范围内。
- URL-state 是跨页入口的主合同。后端 API 仍只消费现有 `view`、`renew_within_days`、`provider_id`、`vps_id`、`country`、`region`、`city`、`scenario`、`group_id`、`manual_group_id`、`record_id`、`template_id`。
- `single_queue` 是 legacy URL。页面可接受并显示提示，但其他页面不再把它作为目标。
- 业务写入仍只发生在：
  - `AssetDecisionWorkPanel` -> `PATCH /api/vps/{id}` 的单台续费决策。
  - record status / followup -> `PATCH /api/asset-decisions/records/{id}`。
  - manual group / template / record 创建编辑的既有 asset-decisions endpoints。

## URL Strategy

Use explicit context URLs:

- Dashboard cancellation / migration attention: `/asset-decisions?view=needs_decision&renew_within_days=30&scenario=migration_retirement`
- Dashboard budget: `/asset-decisions?view=cost&renew_within_days=30&scenario=budget_reduction`
- Dashboard evidence: `/asset-decisions?view=evidence&renew_within_days=30&scenario=evidence_cleanup`
- VPS list: derive from current filters. Provider filters use `view=provider`; renewal quick view uses `view=renewal`; cancellation attention uses `scenario=migration_retirement`; evidence gaps use `view=evidence&scenario=evidence_cleanup`.
- VPS detail: `/asset-decisions?view=needs_decision&renew_within_days=30&vps_id=<id>`
- Subscription row: `/asset-decisions?view=renewal&renew_within_days=30&vps_id=<id>`
- Provider row: `/asset-decisions?view=provider&renew_within_days=30&provider_id=<id>`
- Monitoring / Target support surfaces without a specific VPS: `/asset-decisions?view=evidence&renew_within_days=30&scenario=evidence_cleanup`
- Monitoring / Target detail with a linked VPS: keep `vps_id` and `scenario=migration_retirement`.

## UX Strategy

- Rename old queue phrasing to `资产组合决策`、`组合决策中枢`、`资产组合工作台` or `资料缺口组合判断` depending context.
- Keep the AssetDecisionsPage information architecture:
  1. Summary focus metrics.
  2. Decision group list as primary surface.
  3. Closed-loop next-work rail.
  4. Scenario/templates/manual groups/records.
  5. Renewal evidence and single VPS queue as support surfaces.
- Improve context chip scanning only if needed by existing JSX; avoid introducing a large new component.
- Keep table-heavy support surfaces where they are explicitly auxiliary. Do not spend this task turning every table into cards.

## Tests

- Prefer existing page tests and route helper tests over broad snapshots.
- Add or update tests for:
  - Dashboard asset links no longer use `single_queue` and use combination URLs.
  - Monitoring / Target support links point to evidence cleanup instead of bare `/asset-decisions`.
  - AssetDecisionsPage context chips preserve and clear provider/vps/scenario query state.
  - Legacy `single_queue` still maps to `needs_decision`.
  - Existing record execution plan no-business-write guarantees remain intact.

## Compatibility

- Existing external links to `/asset-decisions?view=single_queue` remain supported by the current fallback behavior.
- Existing API contracts do not change.
- Visual evidence docs are updated to explain current mock profile purpose; no screenshot assets are tracked.

## Rollback

Rollback is a normal git revert of the web/docs changes. Since this task does not add schema or endpoint changes, rollback has no database or runtime migration concerns.
