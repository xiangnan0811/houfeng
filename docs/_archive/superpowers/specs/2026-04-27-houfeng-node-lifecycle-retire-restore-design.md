# Houfeng Node Lifecycle Retire / Restore Design

## Context

Frozen V1 distinguishes Node lifecycle state from monitoring runtime state:

- **Retire (`已退役`)** means a server leaves the active fleet while Node history remains.
- Retirement is not deletion, does not erase observations/events, and does not necessarily remove the agent immediately.
- A retired Node can be explicitly restored to **observing (`观察中`)**.
- The frozen interaction baseline says retirement should live on the **Node detail page**, not as a list-page quick action.

The current implementation already supports Node runtime controls (`启用` / `维护中` / `暂停`) and Target archive/restore, but Node lifecycle retire/restore is absent.

## Scope

Implement V1 Node lifecycle controls:

1. Backend state transitions:
   - Any non-retired lifecycle status can transition to `已退役`.
   - `已退役` can transition only to `观察中`.
   - Invalid lifecycle transitions return conflict.
   - Missing Node returns not found.
2. Event history:
   - Retirement appends a state-change event.
   - Restore appends a state-change event.
   - Events are additive; no historical records are deleted or rewritten.
3. Frontend Node Detail:
   - Non-retired nodes expose a detail-page-only retirement flow.
   - Retired nodes expose `恢复到观察中`.
   - Retired nodes explicitly explain that direct restore to `在用` is not available in V1.
   - Retirement uses an inline confirmation panel, not `window.confirm`.
   - Action errors remain local to the lifecycle card.
4. Event labels:
   - The global event filter and event list can display the new lifecycle event types.

Out of scope:

- Node list bulk retirement.
- Node deletion.
- Editing arbitrary lifecycle statuses.
- Automatically pausing monitoring or disconnecting the agent on retirement.
- Recomputing historical observations/incidents.

## Approaches Considered

### Option A — Reuse `/runtime/*` routes

Add `retire` and `restore-to-observing` under `/api/nodes/:id/runtime/*`.

Rejected because V1 explicitly separates lifecycle state from runtime monitoring state. Mixing them would make the API mirror the current implementation shortcut rather than the frozen domain model.

### Option B — Generic Node update API

Add `PUT /api/nodes/:id` and allow lifecycle edits through a general update payload.

Rejected for this slice because V1 retirement is a high-semantics state transition that must emit events and enforce a narrow recovery path. A generic update endpoint would require broader edit semantics not needed here.

### Option C — Dedicated lifecycle transition routes

Add `/api/nodes/:id/lifecycle/retire` and `/api/nodes/:id/lifecycle/restore-to-observing`.

Selected. This keeps lifecycle semantics explicit, mirrors Target archive/restore as a domain transition, and avoids expanding V1 into arbitrary lifecycle editing.

## Backend Design

Add a lifecycle-control interface and handler parallel to existing runtime controls:

- `POST /api/nodes/{node_id}/lifecycle/retire`
- `POST /api/nodes/{node_id}/lifecycle/restore-to-observing`

Repository methods:

- `RetireNode(ctx, nodeID) (nodes.Record, error)`
- `RestoreRetiredNodeToObserving(ctx, nodeID) (nodes.Record, error)`

Transition rules:

| Current lifecycle | Retire | Restore to observing |
| --- | --- | --- |
| `待接入` | allowed | conflict |
| `在用` | allowed | conflict |
| `观察中` | allowed | conflict |
| `不续费` | allowed | conflict |
| `已退役` | conflict | allowed |

Event types:

- `node_retired` → label `节点已退役`
- `node_restored_to_observing` → label `节点恢复到观察中`

Event payload should include the resulting `lifecycle_status`. The summary should communicate the user-facing semantics:

- Retire: `节点已退役并退出活跃舰队，历史记录保留`
- Restore: `节点已从退役恢复到观察中`

## Frontend Design

Add API helpers:

- `retireNode(nodeId): Promise<NodeRecord>`
- `restoreRetiredNodeToObserving(nodeId): Promise<NodeRecord>`

Node Detail adds a `Lifecycle Control / 生命周期` card near runtime control:

- For non-retired nodes:
  - Show current lifecycle status.
  - Show `退役节点` as an explicit dangerous action.
  - First click opens an inline confirmation panel.
  - Confirmation copy states:
    - Node leaves active fleet semantics.
    - History remains.
    - This is not deletion and does not clear agent/history.
  - Confirmation buttons:
    - `确认退役`
    - `取消`
- For retired nodes:
  - Show `恢复到观察中`.
  - Show disabled explanatory text: `已退役节点在 V1 中只能先恢复到观察中，不能直接恢复为在用。`

After successful action, update local Node state from the response so the hero status and action card change immediately. Errors render inside the card with `role="alert"`.

## Error Handling

- Backend:
  - 404 for missing Node.
  - 409 for invalid lifecycle transitions.
  - 500 for unexpected repository failures.
- Frontend:
  - Use backend error message when available.
  - Fallback copy: `节点生命周期操作失败`.
  - Do not replace the whole detail page for action failures.
  - Guard stale route/action responses with the existing mounted/current-route pattern.

## Testing Strategy

Backend:

- Store tests for SQL transition constraints and event insertion.
- Handler tests for success, invalid transition conflict, not found, and method rejection.
- Router tests for lifecycle subtree dispatch and unknown lifecycle action 404.
- API helper tests for the new frontend helper URLs.

Frontend:

- NodeDetail test for non-retired retirement confirmation flow:
  - Initial action shows inline confirmation.
  - `取消` closes it without fetch.
  - `确认退役` posts to lifecycle retire route.
  - Returned `已退役` record updates visible status and action set.
- NodeDetail test for retired restore flow:
  - Shows `恢复到观察中`.
  - Posts to restore route.
  - Returned `观察中` record updates visible status.
  - Direct restore-to-in-use explanation is visible.
- NodeDetail test for local lifecycle action error with `role="alert"`.
- Event page/event type tests cover the two new labels.

## Self-Review

- No new V1 capability is introduced beyond frozen lifecycle retirement semantics.
- Lifecycle and runtime states stay separate.
- The design avoids generic object editing and bulk operations.
- Historical preservation is implemented through additive events rather than destructive mutation.
- All user-visible labels are concrete and testable.
