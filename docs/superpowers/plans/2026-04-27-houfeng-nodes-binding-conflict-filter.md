# Houfeng Nodes Binding Conflict Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `指纹变更待确认` nodes easy to discover from the Node list without adding list-page binding decisions.

**Architecture:** Use the existing `listNodes()` response and add a client-side view filter in `NodesPage`. Conflict rows get a visible binding badge and a `等待绑定确认` summary; final actions remain on Node Detail / Onboarding.

**Tech Stack:** React/Vite/TypeScript, Testing Library, Vitest, existing Node list API.

---

## Planned File Structure

- Modify: `web/src/pages/NodesPage.tsx`
  - Add binding conflict constants, client-side view state, derived visible rows, filter controls, conflict badge/summary, and filtered empty state.
- Modify: `web/src/pages/NodesPage.test.tsx`
  - Add tests for filter count, row badge, filtered list behavior, empty filtered state, and absence of final binding actions on the list page.

No backend files should change.

## Shared Copy

Use these strings:

```text
全部节点
绑定异常
指纹变更待确认
等待绑定确认
没有绑定异常节点
当前没有等待绑定确认的节点。
确认重绑定
拒绝新指纹
重置绑定
```

---

### Task 1: Node list binding-conflict filter

**Files:**
- Modify: `web/src/pages/NodesPage.tsx`
- Modify: `web/src/pages/NodesPage.test.tsx`

- [x] **Step 1: Add failing NodesPage tests**

In `web/src/pages/NodesPage.test.tsx`, add a helper near the top:

```ts
function nodeRecord(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    node_id: 'nd_001',
    display_name: 'Tokyo Edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-26T09:00:00Z',
    updated_at: '2026-04-26T09:00:00Z',
    ...overrides,
  }
}
```

Add this test:

```tsx
it('surfaces and filters binding-conflict nodes without exposing final binding actions', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValueOnce(
      mockJSONResponse([
        nodeRecord({
          node_id: 'nd_conflict',
          display_name: 'Tokyo Edge',
          binding_status: '指纹变更待确认',
          current_health_status: '关注',
        }),
        nodeRecord({
          node_id: 'nd_normal',
          display_name: 'Seoul Edge',
          region: 'ap-northeast-2',
          city: 'Seoul',
        }),
      ]),
    ),
  )

  render(
    <MemoryRouter initialEntries={['/nodes']}>
      <Routes>
        <Route path="/nodes" element={<NodesPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

  expect(screen.getByRole('button', { name: '绑定异常 1' })).toBeInTheDocument()
  const conflictRow = screen.getByText('Tokyo Edge').closest('article')
  expect(conflictRow).not.toBeNull()
  expect(within(conflictRow!).getByText('指纹变更待确认')).toBeInTheDocument()
  expect(within(conflictRow!).getByText('等待绑定确认')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '确认重绑定' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '拒绝新指纹' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '重置绑定' })).not.toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: '绑定异常 1' }))

  expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
  expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: '绑定异常 1' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )

  fireEvent.click(screen.getByRole('button', { name: '全部节点 2' }))
  expect(screen.getByText('Seoul Edge')).toBeInTheDocument()
})
```

Add this test:

```tsx
it('renders an empty state when the binding-conflict filter has no rows', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValueOnce(
      mockJSONResponse([
        nodeRecord({
          node_id: 'nd_normal',
          display_name: 'Seoul Edge',
          region: 'ap-northeast-2',
          city: 'Seoul',
        }),
      ]),
    ),
  )

  render(
    <MemoryRouter initialEntries={['/nodes']}>
      <Routes>
        <Route path="/nodes" element={<NodesPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() => expect(screen.getByText('Seoul Edge')).toBeInTheDocument())

  fireEvent.click(screen.getByRole('button', { name: '绑定异常 0' }))

  expect(screen.getByText('没有绑定异常节点')).toBeInTheDocument()
  expect(screen.getByText('当前没有等待绑定确认的节点。')).toBeInTheDocument()
  expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument()
})
```

Run:

```bash
cd web && npm test -- --run NodesPage
```

Expected: fail because the filter controls and conflict row summary do not exist.

- [x] **Step 2: Implement client-side binding-conflict filter**

In `web/src/pages/NodesPage.tsx`, add constants and type near existing constants:

```ts
const NODE_BINDING_CONFLICT_STATUS = '指纹变更待确认'
const NODE_BINDING_CONFLICT_SUMMARY = '等待绑定确认'

type NodeListView = 'all' | 'binding-conflict'
```

Add helper:

```ts
function isBindingConflictNode(node: NodeRecord) {
  return node.binding_status === NODE_BINDING_CONFLICT_STATUS
}
```

Add state:

```ts
const [nodeListView, setNodeListView] = useState<NodeListView>('all')
```

Derive visible rows after the error/loading branches:

```ts
const bindingConflictNodes = nodes.filter(isBindingConflictNode)
const visibleNodes = nodeListView === 'binding-conflict' ? bindingConflictNodes : nodes
```

Render filter controls above `.resource-table`:

```tsx
<section className="page-panel">
  <p className="page-panel__eyebrow">List View</p>
  <h3 className="page-panel__title">列表视图</h3>
  <div className="badge-row badge-row--wrap">
    <button
      type="button"
      aria-pressed={nodeListView === 'all'}
      onClick={() => setNodeListView('all')}
    >
      全部节点 {nodes.length}
    </button>
    <button
      type="button"
      aria-pressed={nodeListView === 'binding-conflict'}
      onClick={() => setNodeListView('binding-conflict')}
    >
      绑定异常 {bindingConflictNodes.length}
    </button>
  </div>
</section>
```

If `visibleNodes.length === 0`, render before the table:

```tsx
<div className="empty-state">
  <h3>{nodeListView === 'binding-conflict' ? '没有绑定异常节点' : '暂无节点'}</h3>
  <p>{nodeListView === 'binding-conflict' ? '当前没有等待绑定确认的节点。' : '请先创建第一个节点。'}</p>
</div>
```

Change table mapping from `nodes.map` to `visibleNodes.map`.

In each row status badge area, add:

```tsx
{isBindingConflictNode(node) ? <StatusBadge label={NODE_BINDING_CONFLICT_STATUS} /> : null}
```

In the current issue column, change the summary expression to:

```tsx
{isBindingConflictNode(node)
  ? NODE_BINDING_CONFLICT_SUMMARY
  : node.current_primary_issue_summary || '暂无明显异常'}
```

Do not add binding action buttons.

- [x] **Step 3: Run focused frontend tests and commit**

Run:

```bash
cd web && npm test -- --run NodesPage
cd web && npm run build
```

Expected: pass.

Commit:

```bash
git add web/src/pages/NodesPage.tsx web/src/pages/NodesPage.test.tsx
git commit -m "Surface binding conflicts in the Node list"
```

---

### Task 2: Verification and review

**Files:**
- No planned edits unless verification exposes issues.

- [x] **Step 1: Run focused checks**

Run:

```bash
cd web && npm test -- --run NodesPage
cd web && npm run build
```

Expected: pass.

- [x] **Step 2: Run full verification**

Run:

```bash
go test ./...
cd web && npm test -- --run
cd web && npm run build
cd web && npm run lint
./scripts/verify.sh
```

Expected: pass.

- [x] **Step 3: Scope review**

Confirm:

- No backend files changed.
- Binding-conflict final actions are absent from the list page.
- Conflict nodes are filterable and visibly marked.
- Node Detail remains the decision surface.

- [x] **Step 4: Final code review**

Dispatch a fresh code-review subagent for this slice. If blocked, apply `superpowers:receiving-code-review`, fix minimally, rerun focused and full verification, and re-review.
