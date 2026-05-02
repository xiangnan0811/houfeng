# Houfeng Node Detail Binding Conflict Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the frozen V1 high-priority binding-conflict decision card to Node Detail.

**Architecture:** Reuse existing onboarding APIs from the detail page. Load onboarding metadata only when the current Node is in `指纹变更待确认`, render a local card below the hero, and use existing binding action helpers to resolve the conflict without adding backend routes or new identity behavior.

**Tech Stack:** React/Vite/TypeScript, Testing Library, Vitest, existing Go center API

---

## Planned File Structure

- Modify: `web/src/pages/NodeDetailPage.tsx`
  - Add conditional onboarding metadata loading for conflict nodes.
  - Add high-priority binding-conflict card.
  - Add confirm/reject/reset handlers using existing API helpers.
- Modify: `web/src/pages/NodeDetailPage.test.tsx`
  - Add fixtures and tests for card rendering, load failure, actions, local errors, and stale route safety.

No backend files should change in this slice.

## Shared UI Semantics

Use these visible action labels:

```ts
const NODE_BINDING_CONFIRM_REBIND_LABEL = '确认重绑定'
const NODE_BINDING_REJECT_PENDING_LABEL = '拒绝新指纹'
const NODE_BINDING_RESET_LABEL = '重置绑定'
```

Use this local fallback error copy when onboarding metadata cannot load:

```ts
'绑定冲突详情暂不可用'
```

Use this local action fallback error copy:

```ts
'更新绑定冲突状态失败'
```

---

### Task 1: Render Node Detail binding-conflict metadata

**Files:**
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Modify: `web/src/pages/NodeDetailPage.test.tsx`

- [x] **Step 1: Add focused failing tests for metadata rendering and metadata load failure**

In `web/src/pages/NodeDetailPage.test.tsx`, add local fixture helpers near `deferredResponse()`:

```ts
function nodeRecord(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    node_id: 'nd_conflict',
    display_name: 'Tokyo Edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '指纹变更待确认',
    labels: ['core'],
    note: '',
    current_health_status: '关注',
    last_heartbeat_at: '2026-04-27T09:00:00Z',
    last_sync_at: '2026-04-27T09:05:00Z',
    current_active_incident_count: 1,
    current_primary_issue_summary: '检测到新的指纹接入请求',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-27T09:05:00Z',
    ...overrides,
  }
}

function emptyRuntimeFacts(nodeId = 'nd_conflict') {
  return {
    node_id: nodeId,
    latest_host_sample: null,
  }
}

function onboardingConflictState(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    ...nodeRecord(),
    phase: '绑定冲突待处理',
    has_host_sample: true,
    has_accepted_observation: true,
    enrollment_token_issued_at: '2026-04-26T08:00:00Z',
    current_binding_fingerprint_summary: 'fp-current-1234567890',
    pending_binding: {
      fingerprint: 'fp-pending-abcdefghijklmnopqrstuvwxyz',
      first_seen_at: '2026-04-27T08:55:00Z',
      last_seen_at: '2026-04-27T09:04:00Z',
      attempt_count: 4,
    },
    ...overrides,
  }
}
```

Add this test:

```ts
it('renders a high-priority binding conflict card on Node detail', async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
    .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState()))
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
      <Routes>
        <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() =>
    expect(screen.getByRole('heading', { name: '绑定冲突处置' })).toBeInTheDocument(),
  )

  expect(screen.getByText('高优先级：绑定冲突待处理')).toBeInTheDocument()
  expect(screen.getByText('fp-current-1234567890')).toBeInTheDocument()
  expect(screen.getByText('fp-pendi…uvwxyz')).toBeInTheDocument()
  expect(screen.getByText('2026/04/27 08:55')).toBeInTheDocument()
  expect(screen.getByText('2026/04/27 09:04')).toBeInTheDocument()
  expect(screen.getByText('4')).toBeInTheDocument()
  expect(screen.getByText(/同一台机器重装或合法替换/)).toBeInTheDocument()
  expect(screen.getByRole('link', { name: '打开接入工作台' })).toHaveAttribute(
    'href',
    '/nodes/nd_conflict/onboarding',
  )
  expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_conflict/onboarding', {
    headers: { Accept: 'application/json' },
    cache: 'no-store',
  })
})
```

Add this test:

```ts
it('keeps Node detail visible when binding conflict metadata fails to load', async () => {
  vi.stubGlobal(
    'fetch',
    vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'onboarding unavailable' }, 503)),
  )

  render(
    <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
      <Routes>
        <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() =>
    expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
  )
  await waitFor(() => expect(screen.getByText('onboarding unavailable')).toBeInTheDocument())
  expect(screen.getByRole('heading', { name: '绑定冲突处置' })).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: '节点详情不可用' })).not.toBeInTheDocument()
})
```

- [x] **Step 2: Run focused NodeDetailPage tests and confirm failure**

Run:

```bash
cd web && npm test -- --run NodeDetailPage
```

Expected: fail because Node Detail does not fetch onboarding metadata or render the binding conflict card.

- [x] **Step 3: Implement conditional metadata loading and read-only card**

In `web/src/pages/NodeDetailPage.tsx`, extend imports:

```ts
import {
  ApiError,
  confirmNodeRebind,
  enterNodeMaintenance,
  exitNodeMaintenance,
  getNode,
  getNodeOnboarding,
  getNodeRuntimeFacts,
  listEvents,
  listIncidents,
  pauseNodeMonitoring,
  rejectPendingNodeBinding,
  resetNodeBinding,
  resumeNodeMonitoring,
} from '../lib/api'
```

Extend type imports:

```ts
import type {
  ActiveIncidentRecord,
  NodeOnboardingState,
  NodeRecord,
  NodeRuntimeFacts,
  PendingBindingMetadata,
  StateChangeEventRecord,
} from '../lib/types'
```

Add local state types and constants after `type State`:

```ts
type BindingConflictState = {
  requestedNodeId: string | null
  onboarding: NodeOnboardingState | null
  loading: boolean
  error: string | null
}

type BindingConflictAction = 'confirm' | 'reject' | 'reset'

const NODE_BINDING_CONFIRM_REBIND_LABEL = '确认重绑定'
const NODE_BINDING_REJECT_PENDING_LABEL = '拒绝新指纹'
const NODE_BINDING_RESET_LABEL = '重置绑定'
```

Add helpers near `describeError`:

```ts
function maskFingerprint(value?: string | null) {
  if (!value) return '尚无'
  const normalized = value.trim()
  if (!normalized) return '尚无'
  if (normalized.length <= 14) return normalized
  return `${normalized.slice(0, 8)}…${normalized.slice(-6)}`
}

function currentFingerprintSummary(onboarding: NodeOnboardingState | null) {
  if (onboarding?.current_binding_fingerprint_summary?.trim()) {
    return onboarding.current_binding_fingerprint_summary.trim()
  }
  return '服务端当前未提供已绑定指纹摘要'
}

function hasBindingConflict(node: NodeRecord | null) {
  return node?.binding_status === '指纹变更待确认'
}

function pendingBindingAttemptCount(pendingBinding?: PendingBindingMetadata) {
  return pendingBinding?.attempt_count ?? 0
}
```

In `NodeDetailPageContent`, add state:

```ts
const [bindingConflict, setBindingConflict] = useState<BindingConflictState>({
  requestedNodeId: null,
  onboarding: null,
  loading: false,
  error: null,
})
const [bindingAction, setBindingAction] = useState<BindingConflictAction | null>(null)
```

Add a conditional onboarding effect after the core node load effect:

```ts
useEffect(() => {
  let cancelled = false
  if (!nodeId) return
  const currentNode = state.requestedNodeId === nodeId ? state.node : null
  if (!hasBindingConflict(currentNode)) {
    setBindingConflict({
      requestedNodeId: nodeId,
      onboarding: null,
      loading: false,
      error: null,
    })
    return
  }

  setBindingConflict({
    requestedNodeId: nodeId,
    onboarding: null,
    loading: true,
    error: null,
  })

  getNodeOnboarding(nodeId)
    .then((onboarding) => {
      if (cancelled || !isMountedRef.current || currentRouteNodeIdRef.current !== nodeId) return
      setBindingConflict({
        requestedNodeId: nodeId,
        onboarding,
        loading: false,
        error: null,
      })
    })
    .catch((error: unknown) => {
      if (cancelled || !isMountedRef.current || currentRouteNodeIdRef.current !== nodeId) return
      setBindingConflict({
        requestedNodeId: nodeId,
        onboarding: null,
        loading: false,
        error: describeError(error, '绑定冲突详情暂不可用'),
      })
    })

  return () => {
    cancelled = true
  }
}, [nodeId, state.node, state.requestedNodeId])
```

Before `return`, derive:

```ts
const showBindingConflict = hasBindingConflict(node)
const currentBindingConflict =
  bindingConflict.requestedNodeId === nodeId ? bindingConflict : null
const pendingBinding = currentBindingConflict?.onboarding?.pending_binding
```

Render the card immediately after the hero panel:

```tsx
{showBindingConflict ? (
  <DetailSection eyebrow="Binding Conflict" title="绑定冲突处置" aside="高优先级">
    <article className="metric-card" aria-label="高优先级：绑定冲突待处理">
      <h3>高优先级：绑定冲突待处理</h3>
      <p>
        系统检测到新的指纹接入请求。请判断这是同一台机器重装或合法替换，
        还是另一台机器误用了旧 token。
      </p>
      {currentBindingConflict?.loading ? (
        <p>正在加载绑定冲突详情…</p>
      ) : currentBindingConflict?.error ? (
        <p role="alert">{currentBindingConflict.error}</p>
      ) : (
        <dl>
          <div>
            <dt>当前已绑定指纹</dt>
            <dd>{currentFingerprintSummary(currentBindingConflict?.onboarding ?? null)}</dd>
          </div>
          <div>
            <dt>待确认指纹</dt>
            <dd>{maskFingerprint(pendingBinding?.fingerprint)}</dd>
          </div>
          <div>
            <dt>首次出现</dt>
            <dd>{formatDateTime(pendingBinding?.first_seen_at)}</dd>
          </div>
          <div>
            <dt>最近出现</dt>
            <dd>{formatDateTime(pendingBinding?.last_seen_at)}</dd>
          </div>
          <div>
            <dt>尝试次数</dt>
            <dd>{pendingBindingAttemptCount(pendingBinding)}</dd>
          </div>
        </dl>
      )}
      <p>
        确认重绑定会让新指纹接管该 Node，历史数据仍保留在原 Node；拒绝会保留当前绑定；
        重置会清除当前绑定并回到等待重新绑定状态。
      </p>
      <Link className="text-link" to={`/nodes/${node.node_id}/onboarding`}>
        打开接入工作台
      </Link>
    </article>
  </DetailSection>
) : null}
```

- [x] **Step 4: Re-run focused NodeDetailPage tests and commit**

Run:

```bash
cd web && npm test -- --run NodeDetailPage
```

Expected: pass.

Commit:

```bash
git add web/src/pages/NodeDetailPage.tsx web/src/pages/NodeDetailPage.test.tsx
git commit -m "Surface binding conflicts on Node detail"
```

---

### Task 2: Add Node Detail binding-conflict actions

**Files:**
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Modify: `web/src/pages/NodeDetailPage.test.tsx`

- [x] **Step 1: Add failing tests for confirm, reject/reset calls, and local action errors**

Add this success test:

```ts
it('confirms a pending node rebind from Node detail and hides the conflict card', async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
    .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState()))
    .mockResolvedValueOnce(
      mockJSONResponse(
        onboardingConflictState({
          binding_status: '已绑定',
          phase: '已绑定，等待稳定观测',
          pending_binding: undefined,
          updated_at: '2026-04-27T09:20:00Z',
        }),
      ),
    )
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
      <Routes>
        <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() =>
    expect(screen.getByRole('button', { name: '确认重绑定' })).toBeInTheDocument(),
  )

  fireEvent.click(screen.getByRole('button', { name: '确认重绑定' }))

  await waitFor(() =>
    expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument(),
  )
  expect(screen.getByText('已绑定')).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_conflict/binding/confirm-rebind', {
    method: 'POST',
    headers: { Accept: 'application/json' },
    cache: 'no-store',
  })
})
```

Add this test for the two remaining endpoints:

```ts
it('exposes reject and reset binding actions from Node detail', async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
    .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState()))
    .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState({ binding_status: '已绑定', pending_binding: undefined })))
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
      <Routes>
        <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() =>
    expect(screen.getByRole('button', { name: '拒绝新指纹' })).toBeInTheDocument(),
  )
  expect(screen.getByRole('button', { name: '重置绑定' })).toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: '拒绝新指纹' }))
  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_conflict/binding/reject-pending', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    }),
  )
})
```

Add this error test:

```ts
it('keeps binding action errors local to the conflict card', async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
    .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState()))
    .mockResolvedValueOnce(mockJSONResponse({ error: 'invalid binding transition' }, 409))
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
      <Routes>
        <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() =>
    expect(screen.getByRole('button', { name: '重置绑定' })).toBeInTheDocument(),
  )
  fireEvent.click(screen.getByRole('button', { name: '重置绑定' }))

  await waitFor(() => expect(screen.getByText('invalid binding transition')).toBeInTheDocument())
  expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: '绑定冲突处置' })).toBeInTheDocument()
})
```

- [x] **Step 2: Run focused NodeDetailPage tests and confirm failure**

Run:

```bash
cd web && npm test -- --run NodeDetailPage
```

Expected: fail because the card has no action buttons/handlers yet.

- [x] **Step 3: Implement action handling**

In `NodeDetailPage.tsx`, add this helper inside `NodeDetailPageContent`:

```ts
function applyOnboardingToNode(actionNodeId: string, onboarding: NodeOnboardingState) {
  setState((current) => {
    if (current.requestedNodeId !== actionNodeId) return current
    return {
      ...current,
      node: onboarding,
    }
  })
  setBindingConflict({
    requestedNodeId: actionNodeId,
    onboarding: onboarding.binding_status === '指纹变更待确认' ? onboarding : null,
    loading: false,
    error: null,
  })
}
```

Add this action handler:

```ts
async function handleBindingAction(
  action: BindingConflictAction,
  request: (targetNodeId: string) => Promise<NodeOnboardingState>,
) {
  if (!node) return
  const actionNodeId = node.node_id
  setBindingAction(action)
  setBindingConflict((current) => ({
    ...current,
    requestedNodeId: actionNodeId,
    error: null,
  }))

  try {
    const nextOnboarding = await request(actionNodeId)
    if (
      !isMountedRef.current ||
      currentRouteNodeIdRef.current !== actionNodeId ||
      currentRequestedNodeIdRef.current !== actionNodeId
    ) {
      return
    }
    applyOnboardingToNode(actionNodeId, nextOnboarding)
  } catch (error: unknown) {
    if (
      !isMountedRef.current ||
      currentRouteNodeIdRef.current !== actionNodeId ||
      currentRequestedNodeIdRef.current !== actionNodeId
    ) {
      return
    }
    setBindingConflict((current) => ({
      ...current,
      requestedNodeId: actionNodeId,
      error: describeError(error, '更新绑定冲突状态失败'),
    }))
  } finally {
    if (
      isMountedRef.current &&
      currentRouteNodeIdRef.current === actionNodeId &&
      currentRequestedNodeIdRef.current === actionNodeId
    ) {
      setBindingAction(null)
    }
  }
}
```

Add the buttons after the conflict explanation/link in the card:

```tsx
<div className="badge-row badge-row--wrap">
  <button
    type="button"
    disabled={bindingAction !== null || currentBindingConflict?.loading}
    onClick={() => void handleBindingAction('confirm', confirmNodeRebind)}
  >
    {bindingAction === 'confirm' ? '正在确认…' : NODE_BINDING_CONFIRM_REBIND_LABEL}
  </button>
  <button
    type="button"
    disabled={bindingAction !== null || currentBindingConflict?.loading}
    onClick={() => void handleBindingAction('reject', rejectPendingNodeBinding)}
  >
    {bindingAction === 'reject' ? '正在拒绝…' : NODE_BINDING_REJECT_PENDING_LABEL}
  </button>
  <button
    type="button"
    disabled={bindingAction !== null || currentBindingConflict?.loading}
    onClick={() => void handleBindingAction('reset', resetNodeBinding)}
  >
    {bindingAction === 'reset' ? '正在重置…' : NODE_BINDING_RESET_LABEL}
  </button>
</div>
```

- [x] **Step 4: Re-run focused NodeDetailPage tests and commit**

Run:

```bash
cd web && npm test -- --run NodeDetailPage
```

Expected: pass.

Commit:

```bash
git add web/src/pages/NodeDetailPage.tsx web/src/pages/NodeDetailPage.test.tsx
git commit -m "Resolve binding conflicts from Node detail"
```

---

### Task 3: Harden stale-route conflict behavior

**Files:**
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Modify: `web/src/pages/NodeDetailPage.test.tsx`

- [x] **Step 1: Add a failing stale action regression**

Add this test:

```ts
it('ignores stale binding action success after switching to another node route', async () => {
  const bindingAction = deferredResponse()

  vi.stubGlobal(
    'fetch',
    vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord({ node_id: 'nd_001' })))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_001')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState({ node_id: 'nd_001' })))
      .mockImplementationOnce(() => bindingAction.promise)
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord({ node_id: 'nd_002', display_name: 'Seoul Edge', binding_status: '已绑定' })))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_002')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([])),
  )

  render(
    <MemoryRouter initialEntries={['/nodes/nd_001']}>
      <NodeDetailTestHarness />
    </MemoryRouter>,
  )

  await waitFor(() =>
    expect(screen.getByRole('button', { name: '确认重绑定' })).toBeInTheDocument(),
  )
  fireEvent.click(screen.getByRole('button', { name: '确认重绑定' }))
  fireEvent.click(screen.getByRole('button', { name: 'switch node' }))

  await waitFor(() =>
    expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
  )

  bindingAction.resolve(
    mockJSONResponse(
      onboardingConflictState({
        node_id: 'nd_001',
        binding_status: '已绑定',
        phase: '已绑定，等待稳定观测',
        pending_binding: undefined,
      }),
    ),
  )

  await waitFor(() =>
    expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
  )
  expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument()
})
```

- [x] **Step 2: Run focused NodeDetailPage tests**

Run:

```bash
cd web && npm test -- --run NodeDetailPage
```

Expected: pass if Task 2's stale guards already cover the case; fail if an action cleanup path still updates the wrong route.

- [x] **Step 3: If needed, add a binding action request token**

Only if Step 2 fails due to overlapping or stale action cleanup, add:

```ts
const bindingActionRequestRef = useRef(0)
```

Increment it before each binding action and include `bindingActionRequestRef.current === requestId` in success/error/finally guards.

- [x] **Step 4: Re-run focused tests and commit if code changed**

Run:

```bash
cd web && npm test -- --run NodeDetailPage
```

Expected: pass.

If only tests were added, commit them:

```bash
git add web/src/pages/NodeDetailPage.test.tsx web/src/pages/NodeDetailPage.tsx
git commit -m "Guard stale binding actions on Node detail"
```

---

### Task 4: Full verification and review

**Files:**
- No planned edits unless verification exposes a small issue.

- [x] **Step 1: Run focused frontend checks**

Run:

```bash
cd web && npm test -- --run NodeDetailPage
cd web && npm run build
cd web && npm run lint
```

Expected: pass.

- [x] **Step 2: Run repository verification**

Run:

```bash
go test ./...
cd web && npm test -- --run
cd web && npm run build
./scripts/verify.sh
```

Expected: pass.

- [x] **Step 3: Final scope review**

Confirm:

- No backend routes or schemas changed.
- Conflict card appears only for `指纹变更待确认`.
- Card is on Node Detail, not a new top-level page.
- Onboarding install/token flow remains on Node Onboarding page.
- Confirm/reject/reset use existing endpoints.
- Errors stay local to the conflict card.
- Route switches do not apply stale conflict data.

Commit only if this review finds a small final fix.

---

## Self-Review

Spec coverage:

- Covers Node Detail high-priority binding-conflict card.
- Covers current/pending fingerprint details and first/last/attempt metadata.
- Covers confirm/reject/reset actions from detail.
- Covers local error handling and stale route safety.

Placeholder scan:

- No TBD/TODO placeholders remain.
- Each task has concrete file paths, test content, commands, and expected outcomes.

Type consistency:

- Uses existing `NodeOnboardingState`, `PendingBindingMetadata`, and API helper names from `web/src/lib/types.ts` / `web/src/lib/api.ts`.
- Does not introduce backend changes.
