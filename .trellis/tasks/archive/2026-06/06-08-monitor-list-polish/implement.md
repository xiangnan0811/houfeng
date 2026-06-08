# Implementation Plan

## Preconditions

- Stay on non-main branch `feature/monitor-list-polish`.
- Before editing code, load `trellis-before-dev` for the web layer.

## Steps

1. Add or update failing tests first.
   - `MonitoringPage.test.tsx`: assert no `操作` column, no `快速编辑标签`, no row-level `接入 agent` / runtime buttons, row/name navigation still work.
   - `MonitoringPage.test.tsx`: replace the old freshness-row test with issue-column heartbeat semantics: normal heartbeat, missing heartbeat, binding conflict, and backend primary issue.
   - `MonitoringPage.test.tsx`: assert identity row no longer contains `心跳` / `同步`.
   - `MonitoringPage.test.tsx`: assert asset context/status cells render short text and do not duplicate long binding/status labels.
   - `MonitoringDetailPage.test.tsx`: add coverage that detail page exposes metadata maintenance for Group / labels / note and saves through `PATCH /api/monitoring-instances/{id}/metadata`.

2. Refactor monitoring list columns.
   - Remove `MonitoringInstancesActionsCell` import and `actions` column from `MonitoringInstancesTableColumns.tsx`.
   - Remove action-related args from `buildMonitoringInstancesTableColumns`.
   - Remove lifecycle / monitoring badges from identity column.
   - Remove `last_heartbeat_at` and `last_sync_at` from identity column.
   - Add helper rendering for current issue + heartbeat.
   - Simplify asset context rendering to one primary short chip plus optional short secondary text.
   - Set explicit widths for the remaining columns.

3. Remove list quick-edit metadata state.
   - Remove `Input`, `Modal`, `updateMonitoringInstanceMetadata`, label draft state, metadata busy/error state, and quick-edit modal from `MonitoringPage.tsx`.
   - Remove `shouldNavigateOnRowClick` dependency on label edit state.
   - Keep batch runtime overlays only for batch operations; row-level runtime actions are gone from list.
   - Delete `MonitoringInstancesActionsCell.tsx` if no imports remain.
   - Simplify or keep `MonitoringInstancesLabelsCell.tsx` as read-only only if still useful; remove editing props if no longer needed.

4. Add detail metadata maintenance.
   - Add detail state for metadata draft, open/close, saving, and error in `MonitoringDetailPage.tsx`.
   - Use existing `updateMonitoringInstanceMetadata` API helper with `expectedUpdatedAt`.
   - Update current monitoring instance state with returned group / labels / note / updated_at after save.
   - Render a secondary `标签与备注` section in `MonitoringDetailPageBody`, aligned with the existing Target detail `TargetMetadataSection` / `TargetLabelsAndNote` pattern.
   - The section shows read-only Group, labels, and note by default, then switches to inline edit on `编辑标签与备注`.
   - Cancel must discard drafts and errors.

5. Adjust CSS.
   - Set `.monitoring-table` min width and fixed layout.
   - Remove action-column styling if unused.
   - Add nowrap/truncation rules for issue heartbeat, asset context chip/secondary text, labels, and short status texts.
   - Keep narrow-screen behavior inside `.page-panel--scroll-x`.

6. Cleanup.
   - Remove unused imports, types, helpers, and CSS selectors where appropriate.
   - Re-run targeted tests and fix failures.

## Validation Commands

Run from repository root unless noted:

1. `cd web && npm run test -- --run src/pages/MonitoringPage.test.tsx src/pages/MonitoringDetailPage.test.tsx`
2. `cd web && npm run lint`
3. `cd web && npm run test -- --run`
4. `cd web && npm run build`

For browser sanity after implementation:

- Start dev server if needed: `cd web && npm run dev -- --host 127.0.0.1`
- Check `/monitoring` at desktop and narrow widths for table density, no text overlap, no page-level horizontal overflow beyond the table scroller, and no wrapped short status text.

## Risk Points

- Many existing `MonitoringPage.test.tsx` tests assert list quick-edit behavior; they must be removed or moved to detail-page coverage rather than blindly updated.
- Detail page already has a dense watchtower menu; new metadata entry should not make runtime controls ambiguous.
- `last_heartbeat_at` can be absent in tests and possibly in real records even if TypeScript type is permissive; issue rendering must handle null/undefined.
- Removing row-level runtime buttons changes focus-restore code paths; list-only focus restore can be deleted, but batch pause confirmation still needs to work.

## Review Gate

Do not run `task.py start` or edit implementation code until the user reviews and approves `prd.md`, `design.md`, and `implement.md`.
