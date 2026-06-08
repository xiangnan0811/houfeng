# Design: Monitor List Display Polish

## Recommended Approach

Use a focused frontend-only refactor of MonitoringPage list columns and detail metadata ownership.

Alternative A would only remove the action column and move heartbeat into the issue column. It is smaller, but it would delete the list label-edit capability without a replacement.

Alternative B, the recommended approach, removes the action column, simplifies list status rendering, moves heartbeat semantics into the issue column, and adds a monitoring detail metadata maintenance path for Group / labels / note. This keeps the list scan-focused while preserving all existing user capabilities.

Alternative C would introduce shared status-normalization utilities across Monitoring, Targets, VPS, and Asset Decisions. That may be valuable later, but it is too broad for this task and would touch unrelated pages.

## Architecture And Boundaries

Frontend only. No backend schema, API contract, or enum changes.

Primary files:

- `web/src/pages/monitoring/MonitoringInstancesTableColumns.tsx`: table column definitions, issue/freshness/status rendering, action column removal.
- `web/src/pages/MonitoringPage.tsx`: remove list-level metadata modal/state/action wiring that only served quick label editing.
- `web/src/pages/MonitoringDetailPage.tsx` and `web/src/pages/monitoring-detail/MonitoringDetailPageBody.tsx`: own metadata edit state, call `updateMonitoringInstanceMetadata`, update the loaded monitoring instance after save, and render a `标签与备注` maintenance section aligned with Target detail.
- `web/src/index.css`: monitoring table column density, nowrap protections, status/asset context chip behavior.
- `web/src/pages/MonitoringPage.test.tsx` and `web/src/pages/MonitoringDetailPage.test.tsx`: cover list display changes and detail metadata maintenance.

Existing detail page runtime controls and onboarding drawer already satisfy the “move operations to detail” requirement for run control and agent onboarding. The missing capability is monitoring instance Group / labels / note editing; this task adds it to detail as a secondary `标签与备注` section, matching the existing Target detail pattern rather than mixing metadata maintenance into the runtime control menu.

## List Column Contract

The list should keep these columns:

- Compare checkbox: narrow fixed width.
- Glyph: narrow fixed width.
- Monitoring instance identity: name and compact identity facts only; no heartbeat or sync time.
- Location: group / region / city / provider.
- Asset context: one short primary context chip plus optional short secondary text, both nowrap/truncated.
- Labels: read-only label summary.
- Current main issue: issue summary plus heartbeat fact.
- Last 24h trends: fixed width.

The `actions` column is removed. “详情” is covered by row click and the name link. “接入 agent”, runtime controls, command execution, and history are covered by Monitoring detail’s watchtower menu.

The read-only labels column remains. The user request removes the quick edit operation, not the labels as scan context.

## Current Main Issue Semantics

Issue priority:

1. Binding conflict: `等待绑定确认`.
2. Backend `current_primary_issue_summary`, when present.
3. Missing heartbeat: `未收到心跳`.
4. Normal heartbeat fact: `心跳 <relative timestamp>`.
5. Fallback: `暂无明显异常`.

If a problem summary is shown and `last_heartbeat_at` exists, heartbeat can appear as low-weight supporting text in the same cell. `last_sync_at` is not shown in the list.

## Status Normalization

The list should avoid stacking lifecycle, monitoring, binding, health, VPS lifecycle, and subscription state as many badges in one row.

Recommended row scan model:

- Glyph represents runtime/health at a glance.
- Identity column does not render lifecycle or monitoring badges.
- Asset context column renders one primary short chip: `待取消`, `已取消`, `已过期`, `缺订阅`, `未关联`, or `已关联` style text derived from the existing asset context helper outputs.
- If a secondary detail is useful, it stays as short muted text and must not duplicate the chip.
- Binding conflict appears in the issue column only, not as another long status tag.

This is display normalization only; backend enum values remain unchanged and filters keep using current values.

## Layout

Use `table-layout: fixed` and a monitoring-specific `min-width` so desktop columns do not leave the former action-column gap and narrow screens scroll within `.page-panel--scroll-x`.

Short status texts and chips use `white-space: nowrap`, `overflow: hidden`, and `text-overflow: ellipsis`. The issue summary and asset context must not force row height growth.

## Compatibility

No API changes. Existing list filters, quick views, compare checkbox, batch operations, trend loading, row click, and name link behavior remain.

Removing the list metadata modal requires deleting or rewriting tests that expected `快速编辑标签` on the list, and replacing them with detail-page metadata tests.

## Rollback

Rollback is localized: restore the previous table column definition and MonitoringPage metadata modal wiring. Detail metadata editor can be removed independently if the list quick edit is restored.
