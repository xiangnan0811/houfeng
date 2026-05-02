# Houfeng Nodes Binding Conflict Filter Design

## Context

Frozen V1 says a Node in `指纹变更待确认` must be visible in three places:

- Node list
- Node detail
- Event stream

Node Detail already has the high-priority conflict decision card, and events already include binding audit events. The current Node list shows lifecycle / monitoring / health badges, but it does not make binding conflicts easy to scan or filter.

## Scope

Implement Node list discovery for binding conflicts:

- Show a lightweight `绑定异常` filter on the Node list.
- Show the count of nodes whose `binding_status` is `指纹变更待确认`.
- When the filter is active, only show those conflict nodes.
- In conflict rows, show an obvious `指纹变更待确认` badge.
- In the row issue summary, show `等待绑定确认`.
- Keep final risk decisions out of the list page; continue routing users to detail/onboarding for action.

Out of scope:

- Confirm / reject / reset binding from the list page.
- Backend filtering or pagination.
- A full saved-filter/workbench implementation.
- Generic list filter builder for all Node fields.

## Approach

Use the existing `listNodes()` result and filter client-side. V1 only needs lightweight discoverability here, and the current list page already loads the full current Node list.

Add local view state:

```ts
type NodeListView = 'all' | 'binding-conflict'
```

Derived values:

```ts
const bindingConflictNodes = nodes.filter(isBindingConflictNode)
const visibleNodes = nodeListView === 'binding-conflict' ? bindingConflictNodes : nodes
```

The filter should not change routing or backend requests.

## Frontend Design

In `NodesPage`:

- Add a small view/filter panel above the table:
  - `全部节点`
  - `绑定异常`
  - Count text: `绑定异常 1`
- Use `aria-pressed` on filter buttons.
- If the binding filter is active and no conflict nodes exist, show:
  - Title: `没有绑定异常节点`
  - Description: `当前没有等待绑定确认的节点。`
- For conflict rows:
  - Add `StatusBadge label="指纹变更待确认"`.
  - Show `等待绑定确认` in the current issue column, even when the backend issue summary is empty.
  - Keep links to `接入工作台` and `查看详情`.

## Error Handling

No new API errors are introduced.

Existing list load and runtime-action errors remain unchanged.

## Testing Strategy

Add `NodesPage` tests for:

1. Rendering binding-conflict count and badge.
2. Filtering to only conflict nodes.
3. Empty filtered state when the count is zero.
4. Ensuring final binding action labels (`确认重绑定`, `拒绝新指纹`, `重置绑定`) do not appear on the list page.

Run:

```bash
cd web && npm test -- --run NodesPage
cd web && npm run build
```

## Self-Review

- This implements the frozen V1 list-page visibility requirement without adding risky list-page decisions.
- It does not introduce backend API scope or saved filters.
- It keeps the Node Detail page as the only local conflict decision surface.
