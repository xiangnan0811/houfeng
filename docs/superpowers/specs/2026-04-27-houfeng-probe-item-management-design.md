# Houfeng ProbeItem Management Design

## Context

Houfeng now supports the frozen V1 sequence of creating a Target and then adding ProbeItems from Target detail. The remaining gap in that same object workflow is ProbeItem management after creation. The V1 baseline explicitly keeps ProbeItem management attached to Target detail and limits it to simple structural edits, enable/disable, and delete only for mistaken creation.

Frozen baseline references:

- `docs/design/v1-baseline/architecture-data-model.md` — ProbeItem supports TCP, HTTP/HTTPS, and TLS with controlled config schemas.
- `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md` — ProbeItem creation/editing stays inside Target detail; ProbeItem has no complex runtime status.
- `docs/design/v1-baseline/rules-and-interaction.md` — Target detail centers on the ProbeItem list, target-level trend, active incidents, and recent events.
- `docs/design/v1-baseline/ui-ux-spec.md` — structure editing and runtime control must remain visually distinct; dangerous actions require clear confirmation.
- `docs/design/v1-baseline/baseline-screens.md` — use the Unified/Baseline app shell and Target Detail reference, not older concepts.

## Non-goals

This slice does not add:

- A standalone Probe center.
- ProbeItem-level incident rules or a rules engine.
- ProbeItem-specific event timeline semantics beyond existing Target detail display.
- Bulk ProbeItem editing.
- Delete as a routine lifecycle mechanism.
- Target edit/delete or Node edit/delete.

## Approaches considered

### Approach A — Full PUT + DELETE endpoints, UI reuses the Target detail form

Add scoped endpoints under `/api/targets/:targetId/probe-items/:probeItemId`. `PUT` accepts the same normalized body shape as create and returns the updated ProbeItem. `DELETE` removes a mistaken ProbeItem. The Target detail UI reuses the current kind-specific form for edit mode and uses quick buttons for enable/disable by submitting a full update with only `enabled` changed.

Trade-off: smallest domain surface and least frontend duplication, but toggling enabled requires sending the full current config back.

### Approach B — Separate PATCH endpoints for enable/disable plus PUT for edit

Add dedicated endpoints such as `/enable` and `/disable`, then a separate full-edit endpoint. This gives explicit runtime-like actions.

Trade-off: clearer per-action API, but it expands routing and makes ProbeItem look more like a runtime-controlled resource, which V1 explicitly avoids.

### Approach C — Frontend-only edit draft with no backend persistence yet

Expose the UI affordance but do not persist edits.

Trade-off: unacceptable for V1 implementation because it creates false affordance and diverges from the implementation repository’s goal.

## Decision

Use Approach A.

Reasons:

- It matches V1’s “ProbeItem is structural configuration, not a separate runtime domain” boundary.
- It reuses the already validated ProbeItem create schema, keeping backend validation strict and small.
- It keeps UI inside Target detail and avoids introducing new top-level navigation.
- It supports quick enable/disable without adding separate ProbeItem runtime state.

## Backend design

Add ProbeItem item operations to the existing Target repository and handler surface.

### Domain types

Add:

- `ErrProbeItemNotFound`
- `UpdateProbeItemInput`

`UpdateProbeItemInput` uses the same JSON contract as create:

```json
{
  "probe_kind": "http",
  "enabled": true,
  "frequency_tier": "1m",
  "timeout_seconds": 5,
  "config": {
    "scheme": "https",
    "path": "/healthz",
    "method": "GET",
    "expected_status_range": [200, 299]
  }
}
```

Validation stays strict:

- `probe_kind` must be `tcp`, `http`, or `tls`.
- `frequency_tier` must be `1m`, `5m`, `15m`, or `6h`.
- `timeout_seconds` must be positive.
- Config must match the selected kind exactly; unknown fields are rejected.

### HTTP contract

Add scoped item behavior under the existing handler:

- `PUT /api/targets/:targetId/probe-items/:probeItemId`
  - request body: `UpdateProbeItemInput`
  - `200`: updated `ProbeItemRecord`
  - `400`: malformed or invalid input
  - `404`: missing target or missing scoped ProbeItem
- `DELETE /api/targets/:targetId/probe-items/:probeItemId`
  - `204`: deleted
  - `404`: missing target or missing scoped ProbeItem

Keep collection behavior unchanged:

- `GET /api/targets/:targetId/probe-items`
- `POST /api/targets/:targetId/probe-items`

### Store behavior

- `UpdateProbeItem` updates only a ProbeItem whose `probe_item_id` and `target_id` both match.
- `DeleteProbeItem` deletes only a ProbeItem whose `probe_item_id` and `target_id` both match.
- Missing target maps to `ErrTargetNotFound`.
- Missing ProbeItem under an existing target maps to `ErrProbeItemNotFound`.
- Deletion physically removes the row. This is acceptable only because V1 labels delete as “误建删除”; UI copy must not frame it as a routine lifecycle action.

## Frontend design

### API helpers

Add typed helpers:

- `updateProbeItem(targetId, probeItemId, input)`
- `deleteProbeItem(targetId, probeItemId)`

Add `UpdateProbeItemInput` as the same shape as `CreateProbeItemInput`.

### Target detail UI

The ProbeItem list receives lightweight per-row actions:

- `编辑`
- `启用` or `停用`
- `删除`

`编辑` opens the existing ProbeItem form in edit mode:

- The panel title changes to `编辑 ProbeItem`.
- The submit button changes to `保存 ProbeItem` / `正在保存…`.
- The form is prefilled from the selected ProbeItem’s current kind/config.
- Successful save replaces that ProbeItem in the list and closes/resets the panel.

`启用` / `停用`:

- Sends `updateProbeItem` with the current ProbeItem payload and inverted `enabled`.
- Replaces the row on success.
- Keeps errors local to the ProbeItem panel/list area.

`删除`:

- Requires `window.confirm('删除 ProbeItem 会移除这条观测方式，仅应用于误建场景，确定继续吗？')`.
- On success removes the row from the list.
- Does not navigate or mutate Target state.

### Async safety

All ProbeItem mutations must be scoped to the current route Target:

- If the user switches route before a mutation returns, ignore the result.
- If the user closes the edit/create panel while a save is pending, ignore that panel’s late response.
- Do not clear loaded target data when a ProbeItem mutation fails.

## Testing design

Backend tests:

- Handler accepts valid `PUT` and returns updated ProbeItem.
- Handler accepts valid `DELETE` and returns `204`.
- Handler rejects invalid update config.
- Handler maps missing target / missing ProbeItem to `404`.
- Store updates scoped ProbeItem rows.
- Store deletes scoped ProbeItem rows.
- Store does not update/delete a ProbeItem under the wrong Target.

Frontend tests:

- API helper tests verify `PUT` and `DELETE` paths and JSON bodies.
- Target detail edit test verifies prefill, update payload, and row replacement.
- Enable/disable test verifies current config is preserved and only `enabled` changes.
- Delete test verifies strong confirmation and row removal.
- Local error test verifies mutation failures do not clear target detail data.
- Stale route test verifies a late ProbeItem mutation response cannot update a different Target route.

## Acceptance criteria

- ProbeItem management exists only inside Target detail.
- Create flow remains unchanged.
- Edit/enable/disable/delete use strict backend validation and scoped target/probe IDs.
- Delete is strongly confirmed and framed as mistaken-creation cleanup.
- Existing target runtime controls still work.
- Full repo verification passes.
