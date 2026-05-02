# Houfeng Stateful Confirmations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace remaining browser-native confirmations with frozen V1 stateful confirmation cards for risky pause/archive/delete actions.

**Architecture:** Add a small presentational confirmation-card component, then wire page-local pending-confirmation state into Node, Target, and ProbeItem actions. Keep existing backend APIs, runtime semantics, stale-route guards, and local error placement.

**Tech Stack:** React/Vite/TypeScript, Testing Library, Vitest, existing Go API contracts

---

## Planned File Structure

- Create: `web/src/components/ActionConfirmationCard.tsx`
  - Shared presentational confirmation card for current/result/impact/unchanged copy and confirm/cancel actions.
- Modify: `web/src/pages/NodesPage.tsx`
  - Replace `window.confirm` for row-level Node pause with row-local confirmation state.
- Modify: `web/src/pages/NodesPage.test.tsx`
  - Add red/green coverage for Node list pause confirmation.
- Modify: `web/src/pages/NodeDetailPage.tsx`
  - Replace `window.confirm` for Node detail pause with Runtime Control confirmation state.
- Modify: `web/src/pages/NodeDetailPage.test.tsx`
  - Update existing pause test from browser confirm to inline confirmation.
- Modify: `web/src/pages/TargetsPage.tsx`
  - Replace `window.confirm` for row-level Target pause/archive.
- Modify: `web/src/pages/TargetsPage.test.tsx`
  - Add red/green coverage for Target list pause/archive confirmations.
- Modify: `web/src/pages/TargetDetailPage.tsx`
  - Replace `window.confirm` for Target detail pause/archive and ProbeItem delete.
- Modify: `web/src/pages/TargetDetailPage.test.tsx`
  - Update/add coverage for Target detail runtime confirmations and ProbeItem delete confirmation.

No backend files should change.

## Shared Copy

Use the exact copy from `docs/superpowers/specs/2026-04-27-houfeng-stateful-confirmations-design.md`.

## Shared Component

The component should have this shape:

```tsx
type ActionConfirmationCardProps = {
  title: string
  current: string
  result: string
  impact: string
  unchanged: string
  confirmLabel: string
  cancelLabel?: string
  disabled?: boolean
  onConfirm: () => void
  onCancel: () => void
}
```

Render:

```tsx
<section
  className="page-panel"
  role="alertdialog"
  aria-labelledby={titleId}
  aria-describedby={descriptionId}
  tabIndex={-1}
>
  <p className="page-panel__eyebrow">Confirmation</p>
  <h3 id={titleId} className="page-panel__title">{title}</h3>
  <div id={descriptionId} className="page-stack">
    <p>{current}</p>
    <p>{result}</p>
    <p>{impact}</p>
    <p>{unchanged}</p>
    <div className="badge-row badge-row--wrap">
      <button type="button" disabled={disabled} onClick={onConfirm}>
        {confirmLabel}
      </button>
      <button type="button" disabled={disabled} onClick={onCancel}>
        {cancelLabel ?? '取消'}
      </button>
    </div>
  </div>
</section>
```

---

### Task 1: Add shared confirmation component and Node list pause confirmation

**Files:**
- Create: `web/src/components/ActionConfirmationCard.tsx`
- Modify: `web/src/pages/NodesPage.tsx`
- Modify: `web/src/pages/NodesPage.test.tsx`

- [x] **Step 1: Add failing NodesPage test**

In `web/src/pages/NodesPage.test.tsx`, replace the browser-confirm expectation in `requires strong confirmation before pausing monitoring and keeps runtime errors local` with a new inline-confirmation flow.

Use this test body:

```tsx
it('uses an inline stateful confirmation before pausing node monitoring from the list', async () => {
  const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(
      mockJSONResponse([
        nodeRecord({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          binding_status: '已绑定',
        }),
      ]),
    )
    .mockResolvedValueOnce(
      mockJSONResponse(
        nodeRecord({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          binding_status: '已绑定',
          monitoring_status: '暂停',
        }),
      ),
    )
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter initialEntries={['/nodes']}>
      <Routes>
        <Route path="/nodes" element={<NodesPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

  fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))

  expect(screen.getByRole('alertdialog', { name: '确认暂停节点监控' })).toBeInTheDocument()
  expect(screen.getByText('当前：监控运行状态为启用。')).toBeInTheDocument()
  expect(screen.getByText('操作后：监控运行状态变为暂停。')).toBeInTheDocument()
  expect(screen.getByText('会停止主机指标采集，并停止该节点承担的探针执行。趋势图会从此开始出现数据空档。')).toBeInTheDocument()
  expect(screen.getByText('不会删除历史事件、观测记录或 agent 绑定关系。')).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledTimes(1)

  fireEvent.click(screen.getByRole('button', { name: '取消' }))
  expect(screen.queryByRole('heading', { name: '确认暂停节点监控' })).not.toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledTimes(1)

  fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))
  fireEvent.click(screen.getByRole('button', { name: '确认暂停监控' }))

  expect(confirmMock).not.toHaveBeenCalled()
  await waitFor(() =>
    expect(screen.getByRole('button', { name: '恢复监控' })).toBeInTheDocument(),
  )
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/runtime/pause', {
    method: 'POST',
    headers: { Accept: 'application/json' },
    cache: 'no-store',
  })
})
```

Run:

```bash
cd web && npm test -- --run NodesPage
```

Expected: fail because inline confirmation card does not exist and `window.confirm` is still used.

- [x] **Step 2: Add `ActionConfirmationCard`**

Create `web/src/components/ActionConfirmationCard.tsx` using the shared component shape above.

- [x] **Step 3: Implement NodesPage pause confirmation**

In `web/src/pages/NodesPage.tsx`:

1. Import `ActionConfirmationCard`.
2. Remove `NODE_PAUSE_CONFIRM_MESSAGE`.
3. Add:

```ts
type PendingNodeConfirmation = {
  nodeId: string
  action: 'pause'
}
```

4. Add state:

```ts
const [pendingConfirmation, setPendingConfirmation] = useState<PendingNodeConfirmation | null>(null)
```

5. Change `handleRuntimeAction` so pause opens the confirmation unless explicitly confirmed:

```ts
async function handleRuntimeAction(
  node: NodeRecord,
  action: NodeRuntimeAction,
  confirmed = false,
) {
  if (action === 'pause' && !confirmed) {
    setPendingConfirmation({ nodeId: node.node_id, action })
    return
  }
  // existing API logic continues
}
```

6. After a successful update, clear the confirmation:

```ts
setPendingConfirmation((current) =>
  current?.nodeId === updated.node_id ? null : current,
)
```

7. Render this card inside the matching row:

```tsx
{pendingConfirmation?.nodeId === node.node_id && pendingConfirmation.action === 'pause' ? (
  <ActionConfirmationCard
    title="确认暂停节点监控"
    current={node.monitoring_status === '维护中' ? '当前：监控运行状态为维护中。' : '当前：监控运行状态为启用。'}
    result="操作后：监控运行状态变为暂停。"
    impact="会停止主机指标采集，并停止该节点承担的探针执行。趋势图会从此开始出现数据空档。"
    unchanged="不会删除历史事件、观测记录或 agent 绑定关系。"
    confirmLabel="确认暂停监控"
    disabled={runtimeBusyNodeId === node.node_id}
    onConfirm={() => void handleRuntimeAction(node, 'pause', true)}
    onCancel={() => setPendingConfirmation(null)}
  />
) : null}
```

- [x] **Step 4: Run focused NodesPage tests and commit**

Run:

```bash
cd web && npm test -- --run NodesPage
```

Expected: pass.

Commit:

```bash
git add web/src/components/ActionConfirmationCard.tsx web/src/pages/NodesPage.tsx web/src/pages/NodesPage.test.tsx
git commit -m "Replace Node list pause confirm with a stateful card"
```

---

### Task 2: Replace Node detail pause browser confirmation

**Files:**
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Modify: `web/src/pages/NodeDetailPage.test.tsx`

- [x] **Step 1: Update failing NodeDetailPage test**

In `web/src/pages/NodeDetailPage.test.tsx`, update `pauses node monitoring from detail with strong confirmation` so it:

- clicks `暂停监控`
- asserts heading `确认暂停节点监控`
- asserts the four Node pause copy lines
- clicks `取消` and verifies the pause API was not called
- clicks `暂停监控` again
- clicks `确认暂停监控`
- asserts `window.confirm` was not called
- asserts the existing `/api/nodes/nd_001/runtime/pause` call

Run:

```bash
cd web && npm test -- --run NodeDetailPage
```

Expected: fail because Node detail still uses `window.confirm`.

- [x] **Step 2: Implement NodeDetailPage confirmation state**

In `web/src/pages/NodeDetailPage.tsx`:

1. Import `ActionConfirmationCard`.
2. Remove `NODE_PAUSE_CONFIRM_MESSAGE`.
3. Add:

```ts
const [pendingRuntimeConfirmation, setPendingRuntimeConfirmation] = useState<NodeRuntimeAction | null>(null)
```

4. Change `handleRuntimeAction(action, confirmed = false)` so pause opens the confirmation when not confirmed.
5. Clear `pendingRuntimeConfirmation` after a successful state update.
6. Render the same Node pause `ActionConfirmationCard` in Runtime Control when `pendingRuntimeConfirmation === 'pause'`.

- [x] **Step 3: Run focused NodeDetailPage tests and commit**

Run:

```bash
cd web && npm test -- --run NodeDetailPage
```

Expected: pass.

Commit:

```bash
git add web/src/pages/NodeDetailPage.tsx web/src/pages/NodeDetailPage.test.tsx
git commit -m "Replace Node detail pause confirm with a stateful card"
```

---

### Task 3: Replace Target list pause/archive browser confirmations

**Files:**
- Modify: `web/src/pages/TargetsPage.tsx`
- Modify: `web/src/pages/TargetsPage.test.tsx`

- [x] **Step 1: Add failing TargetsPage tests**

Add one test for pause and one for archive:

```tsx
it('uses an inline stateful confirmation before pausing a target from the list', async () => {
  const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(mockJSONResponse([targetRecord({ target_id: 'tg_pause', name: 'Blog' })]))
    .mockResolvedValueOnce(mockJSONResponse(targetRecord({ target_id: 'tg_pause', name: 'Blog', run_status: '暂停' })))
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter initialEntries={['/targets']}>
      <Routes>
        <Route path="/targets" element={<TargetsPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

  fireEvent.click(screen.getByRole('button', { name: '暂停' }))
  expect(screen.getByRole('heading', { name: '确认暂停目标监控' })).toBeInTheDocument()
  expect(screen.getByText('当前：目标运行状态为启用或维护中。')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '取消' }))
  expect(fetchMock).toHaveBeenCalledTimes(1)

  fireEvent.click(screen.getByRole('button', { name: '暂停' }))
  fireEvent.click(screen.getByRole('button', { name: '确认暂停目标' }))

  expect(confirmMock).not.toHaveBeenCalled()
  await waitFor(() => expect(screen.getByRole('button', { name: '恢复' })).toBeInTheDocument())
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_pause/runtime/pause', {
    method: 'POST',
    headers: { Accept: 'application/json' },
    cache: 'no-store',
  })
})
```

```tsx
it('uses an inline stateful confirmation before archiving a target from the list', async () => {
  const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(mockJSONResponse([targetRecord({ target_id: 'tg_archive', name: 'Blog' })]))
    .mockResolvedValueOnce(mockJSONResponse(targetRecord({ target_id: 'tg_archive', name: 'Blog', run_status: '已归档' })))
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter initialEntries={['/targets']}>
      <Routes>
        <Route path="/targets" element={<TargetsPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

  fireEvent.click(screen.getByRole('button', { name: '归档' }))
  expect(screen.getByRole('heading', { name: '确认归档目标' })).toBeInTheDocument()
  expect(screen.getByText('当前：目标仍在当前工作集中。')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '确认归档' }))

  expect(confirmMock).not.toHaveBeenCalled()
  await waitFor(() => expect(screen.getByRole('button', { name: '恢复到暂停' })).toBeInTheDocument())
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_archive/runtime/archive', {
    method: 'POST',
    headers: { Accept: 'application/json' },
    cache: 'no-store',
  })
})
```

Run:

```bash
cd web && npm test -- --run TargetsPage
```

Expected: fail because Target list still uses `window.confirm`.

- [x] **Step 2: Implement TargetsPage confirmation state**

In `web/src/pages/TargetsPage.tsx`:

1. Import `ActionConfirmationCard`.
2. Remove `TARGET_PAUSE_CONFIRM_MESSAGE` and `TARGET_ARCHIVE_CONFIRM_MESSAGE`.
3. Add:

```ts
type PendingTargetConfirmation = {
  targetId: string
  action: 'pause' | 'archive'
}
```

4. Add state:

```ts
const [pendingConfirmation, setPendingConfirmation] = useState<PendingTargetConfirmation | null>(null)
```

5. Change `handleRuntimeAction(target, action, confirmed = false)` so pause/archive open the confirmation when not confirmed.
6. Clear confirmation after a successful update for the same target.
7. Render `ActionConfirmationCard` in the matching row with Target pause or archive copy.

- [x] **Step 3: Run focused TargetsPage tests and commit**

Run:

```bash
cd web && npm test -- --run TargetsPage
```

Expected: pass.

Commit:

```bash
git add web/src/pages/TargetsPage.tsx web/src/pages/TargetsPage.test.tsx
git commit -m "Replace Target list risky confirms with stateful cards"
```

---

### Task 4: Replace Target detail runtime and ProbeItem delete browser confirmations

**Files:**
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.test.tsx`

- [x] **Step 1: Update/add failing TargetDetailPage tests**

Update existing tests that assert `window.confirm` for target pause/archive and ProbeItem delete so they instead:

- assert the corresponding confirmation heading/copy appears
- click `取消` where useful and verify no mutation call happened
- click the confirm button
- assert `window.confirm` was not called
- assert the existing API path was called

For ProbeItem delete, preserve the existing test name intent but assert:

```tsx
expect(screen.getByRole('heading', { name: '确认删除 ProbeItem' })).toBeInTheDocument()
expect(screen.getByText('当前：这条 ProbeItem 仍属于当前 Target。')).toBeInTheDocument()
expect(screen.getByText('操作后：这条观测方式会被移除。')).toBeInTheDocument()
expect(screen.getByText('仅用于误建场景。删除后该 ProbeItem 不再产生新的 observation。')).toBeInTheDocument()
expect(screen.getByText('不会删除 Target，也不会删除既有事件或历史观测记录。')).toBeInTheDocument()
fireEvent.click(screen.getByRole('button', { name: '确认删除 ProbeItem' }))
expect(confirmMock).not.toHaveBeenCalled()
```

Run:

```bash
cd web && npm test -- --run TargetDetailPage
```

Expected: fail because Target detail and ProbeItem delete still use `window.confirm`.

- [x] **Step 2: Implement TargetDetailPage confirmation state**

In `web/src/pages/TargetDetailPage.tsx`:

1. Import `ActionConfirmationCard`.
2. Remove `TARGET_PAUSE_CONFIRM_MESSAGE`, `TARGET_ARCHIVE_CONFIRM_MESSAGE`, and `PROBE_DELETE_CONFIRM_MESSAGE`.
3. Add:

```ts
type PendingTargetRuntimeConfirmation = 'pause' | 'archive'
const [pendingRuntimeConfirmation, setPendingRuntimeConfirmation] = useState<PendingTargetRuntimeConfirmation | null>(null)
const [pendingProbeDeleteId, setPendingProbeDeleteId] = useState<string | null>(null)
```

4. Change `handleRuntimeAction(action, confirmed = false)` so pause/archive open the runtime confirmation when not confirmed.
5. Clear `pendingRuntimeConfirmation` after a successful runtime update.
6. Change `handleDeleteProbeItem(probeItem, confirmed = false)` so it opens delete confirmation when not confirmed.
7. Clear `pendingProbeDeleteId` after successful delete and when canceling.
8. Render Target pause/archive `ActionConfirmationCard` in Runtime Control.
9. Render ProbeItem delete `ActionConfirmationCard` near the matching ProbeItem controls.

- [x] **Step 3: Run focused TargetDetailPage tests and commit**

Run:

```bash
cd web && npm test -- --run TargetDetailPage
```

Expected: pass.

Commit:

```bash
git add web/src/pages/TargetDetailPage.tsx web/src/pages/TargetDetailPage.test.tsx
git commit -m "Replace Target detail risky confirms with stateful cards"
```

---

### Task 5: Verification and review

**Files:**
- No planned edits unless verification exposes issues.

- [x] **Step 1: Run focused frontend checks**

Run:

```bash
cd web && npm test -- --run NodesPage NodeDetailPage TargetsPage TargetDetailPage
cd web && npm run build
```

Expected: pass.

- [x] **Step 2: Confirm no browser confirms remain in production page code**

Run:

```bash
grep -RIn "window\\.confirm\\|confirm(" web/src/pages web/src/components || true
```

Expected: no production `window.confirm` or bare `confirm(` usage remains. Test spies may still exist in test files.

- [x] **Step 3: Run full verification**

Run:

```bash
go test ./...
cd web && npm test -- --run
cd web && npm run build
cd web && npm run lint
./scripts/verify.sh
```

Expected: pass.

- [x] **Step 4: Scope review**

Confirm:

- No backend files changed.
- Light actions still execute immediately.
- Risky actions now explain current/result/impact/unchanged before mutation.
- Existing stale-route guards and local errors still apply.

- [x] **Step 5: Final code review**

Dispatch a fresh code-review subagent for this slice. If review finds issues, apply `superpowers:receiving-code-review`, fix minimally, rerun focused and full verification, and re-review.

---

## Self-review

### Spec coverage

- Covers all remaining production `window.confirm` usage identified before this slice.
- Covers Node pause, Target pause/archive, and ProbeItem delete.
- Preserves quick handling for light reversible actions.

### Placeholder scan

- No TBD/TODO placeholders remain.
- Each task names concrete files, exact copy, exact commands, and expected outcomes.

### Type consistency

- Pending-confirmation state is page-local and narrow.
- Shared component is presentational only.
- No backend or API type changes are introduced.
