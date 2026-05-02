# Houfeng Target and ProbeItem Creation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the frozen V1 workflow for creating Targets from the list page and adding ProbeItems from Target detail.

**Architecture:** Reuse the existing backend create endpoints. Add typed web API helpers, a compact Target creation panel in `TargetsPage`, and an inline ProbeItem creation panel in `TargetDetailPage`. Keep edit/delete/enable-disable flows outside this slice.

**Tech Stack:** React, Vite, TypeScript, existing Go center HTTP/store contracts, Vitest, Testing Library

---

## Planned File Structure

- Modify: `web/src/lib/types.ts`
  - Add `CreateTargetInput`, `ProbeKind`, `FrequencyTier`, `CreateProbeItemInput`.
- Modify: `web/src/lib/api.ts`
  - Add `createTarget(input)` and `createProbeItem(targetId, input)`.
- Modify: `web/src/lib/api.test.ts`
  - Lock POST request paths and JSON bodies.
- Modify: `web/src/pages/TargetsPage.tsx`
  - Add Target creation form and navigate to created detail page.
- Modify: `web/src/pages/TargetsPage.test.tsx`
  - Cover primary action, empty-state action, validation, POST payload, and navigation.
- Modify: `web/src/pages/TargetDetailPage.tsx`
  - Add ProbeItem creation form scoped to the loaded target.
- Modify: `web/src/pages/TargetDetailPage.test.tsx`
  - Cover empty-state add action, kind-specific config body, appended ProbeItem, and local error handling.

## Shared Constants and Shapes

Use these exact frontend option values:

```ts
const TARGET_TYPE_OPTIONS = [
  { value: 'service', label: 'service' },
  { value: 'china_reference', label: 'china_reference' },
] as const

const TARGET_RUN_STATUS_OPTIONS = [
  { value: '启用', label: '启用' },
  { value: '维护中', label: '维护中' },
  { value: '暂停', label: '暂停' },
] as const

const FREQUENCY_TIER_OPTIONS = [
  { value: '1m', label: '1 分钟' },
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
  { value: '6h', label: '6 小时' },
] as const
```

Use this existing label parser pattern from `NodesPage`:

```ts
function parseLabels(value: string) {
  return value
    .split(/[,，]/)
    .map((item) => item.trim())
    .filter(Boolean)
}
```

Use this positive integer parser for Target/Probe forms:

```ts
function parseOptionalPositiveInteger(value: string, label: string): number | undefined {
  const normalized = value.trim()
  if (normalized === '') return undefined
  if (!/^[1-9]\d*$/.test(normalized)) {
    throw new Error(`${label}必须为正整数。`)
  }
  return Number.parseInt(normalized, 10)
}

function parseRequiredPositiveInteger(value: string, label: string): number {
  const parsed = parseOptionalPositiveInteger(value, label)
  if (parsed == null) {
    throw new Error(`${label}必须为正整数。`)
  }
  return parsed
}
```

---

### Task 1: Add Typed Web API Helpers for Target and ProbeItem Creation

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`

- [ ] **Step 1: Add failing API helper tests**

Add imports in `web/src/lib/api.test.ts`:

```ts
import {
  createProbeItem,
  createTarget,
  // existing imports stay unchanged
} from './api'
```

Add this test before the existing target runtime-control test:

```ts
it('creates targets with POST /api/targets', async () => {
  const responseBody = {
    target_id: 'tg_new',
    name: 'Blog',
    target_type: 'service',
    host: 'blog.example.com',
    base_port: 443,
    execution_node_labels: ['edge', 'core'],
    run_status: '启用',
    labels: ['public'],
    note: 'primary blog',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-27T09:00:00Z',
    updated_at: '2026-04-27T09:00:00Z',
  }
  const requestBody = {
    name: 'Blog',
    target_type: 'service',
    host: 'blog.example.com',
    base_port: 443,
    execution_node_labels: ['edge', 'core'],
    run_status: '启用',
    labels: ['public'],
    note: 'primary blog',
  }
  const fetchMock = vi.fn().mockResolvedValue(mockResponse(201, JSON.stringify(responseBody)))
  vi.stubGlobal('fetch', fetchMock)

  await expect(createTarget(requestBody)).resolves.toEqual(responseBody)
  expect(fetchMock).toHaveBeenCalledWith('/api/targets', {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    body: JSON.stringify(requestBody),
  })
})
```

Add this test next to it:

```ts
it('creates probe items with POST /api/targets/:targetId/probe-items', async () => {
  const responseBody = {
    probe_item_id: 'pb_new',
    target_id: 'tg_new',
    probe_kind: 'http',
    enabled: true,
    frequency_tier: '1m',
    timeout_seconds: 5,
    config: {
      scheme: 'https',
      path: '/healthz',
      method: 'GET',
      expected_status_range: [200, 299],
    },
    created_at: '2026-04-27T09:00:00Z',
    updated_at: '2026-04-27T09:00:00Z',
  }
  const requestBody = {
    probe_kind: 'http',
    enabled: true,
    frequency_tier: '1m',
    timeout_seconds: 5,
    config: {
      scheme: 'https',
      path: '/healthz',
      method: 'GET',
      expected_status_range: [200, 299],
    },
  }
  const fetchMock = vi.fn().mockResolvedValue(mockResponse(201, JSON.stringify(responseBody)))
  vi.stubGlobal('fetch', fetchMock)

  await expect(createProbeItem('tg_new', requestBody)).resolves.toEqual(responseBody)
  expect(fetchMock).toHaveBeenCalledWith('/api/targets/tg_new/probe-items', {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    body: JSON.stringify(requestBody),
  })
})
```

- [ ] **Step 2: Run focused API tests and confirm failure**

Run:

```bash
cd web && npm test -- --run api
```

Expected: fail because `createTarget`, `createProbeItem`, and input types do not exist.

- [ ] **Step 3: Add frontend input types**

In `web/src/lib/types.ts`, after `TargetRecord`, add:

```ts
export type TargetType = 'service' | 'china_reference'
export type TargetRunStatus = '启用' | '维护中' | '暂停' | '已归档'

export type CreateTargetInput = {
  name: string
  target_type: TargetType
  host: string
  base_port?: number
  execution_node_labels: string[]
  run_status: TargetRunStatus
  labels: string[]
  note: string
}
```

After `ProbeItemRecord`, add:

```ts
export type ProbeKind = 'tcp' | 'http' | 'tls'
export type FrequencyTier = '1m' | '5m' | '15m' | '6h'

export type CreateProbeItemInput = {
  probe_kind: ProbeKind
  enabled: boolean
  frequency_tier: FrequencyTier
  timeout_seconds: number
  config: Record<string, unknown>
}
```

- [ ] **Step 4: Add API helpers**

In `web/src/lib/api.ts`, add `CreateTargetInput` and `CreateProbeItemInput` to the type imports.

Add this helper near `postJSON`:

```ts
function postJSONBody<T>(path: string, body: unknown): Promise<T> {
  return requestJSON<T>(path, {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
}
```

Add:

```ts
export function createTarget(input: CreateTargetInput) {
  return postJSONBody<TargetRecord>('/api/targets', input)
}

export function createProbeItem(targetId: string, input: CreateProbeItemInput) {
  return postJSONBody<ProbeItemRecord>(`/api/targets/${targetId}/probe-items`, input)
}
```

- [ ] **Step 5: Run focused API tests and commit**

Run:

```bash
cd web && npm test -- --run api
```

Expected: pass.

Commit:

```bash
git add web/src/lib/types.ts web/src/lib/api.ts web/src/lib/api.test.ts
git commit -m "Expose typed web helpers for target and probe creation"
```

---

### Task 2: Add Target Creation Flow on Targets Page

**Files:**
- Modify: `web/src/pages/TargetsPage.tsx`
- Modify: `web/src/pages/TargetsPage.test.tsx`

- [ ] **Step 1: Write failing TargetsPage tests**

In `web/src/pages/TargetsPage.test.tsx`, add a test that starts from an empty target list:

```ts
it('creates the first target and navigates to its detail page', async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(
      mockJSONResponse(
        {
          target_id: 'tg_new',
          name: 'Blog',
          target_type: 'service',
          host: 'blog.example.com',
          base_port: 443,
          execution_node_labels: ['edge', 'core'],
          run_status: '启用',
          labels: ['public'],
          note: 'primary blog',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-27T09:00:00Z',
          updated_at: '2026-04-27T09:00:00Z',
        },
        201,
      ),
    )
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter initialEntries={['/targets']}>
      <Routes>
        <Route path="/targets" element={<TargetsPage />} />
        <Route path="/targets/:targetId" element={<div>target detail route</div>} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() => expect(screen.getByRole('button', { name: '创建第一个目标' })).toBeInTheDocument())

  fireEvent.click(screen.getByRole('button', { name: '创建第一个目标' }))
  fireEvent.change(screen.getByLabelText('目标名称'), { target: { value: 'Blog' } })
  fireEvent.change(screen.getByLabelText('目标类型'), { target: { value: 'service' } })
  fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'blog.example.com' } })
  fireEvent.change(screen.getByLabelText('Base Port'), { target: { value: '443' } })
  fireEvent.change(screen.getByLabelText('执行节点标签'), { target: { value: 'edge, core' } })
  fireEvent.change(screen.getByLabelText('运行状态'), { target: { value: '启用' } })
  fireEvent.change(screen.getByLabelText('目标标签'), { target: { value: 'public' } })
  fireEvent.change(screen.getByLabelText('备注'), { target: { value: 'primary blog' } })
  fireEvent.click(screen.getByRole('button', { name: '创建目标' }))

  await waitFor(() => expect(screen.getByText('target detail route')).toBeInTheDocument())
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets', {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    body: JSON.stringify({
      name: 'Blog',
      target_type: 'service',
      host: 'blog.example.com',
      base_port: 443,
      execution_node_labels: ['edge', 'core'],
      run_status: '启用',
      labels: ['public'],
      note: 'primary blog',
    }),
  })
})
```

Add a second test for missing execution labels:

```ts
it('keeps target creation errors inside the create panel', async () => {
  const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([]))
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter initialEntries={['/targets']}>
      <Routes>
        <Route path="/targets" element={<TargetsPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() => expect(screen.getByRole('button', { name: '创建第一个目标' })).toBeInTheDocument())

  fireEvent.click(screen.getByRole('button', { name: '创建第一个目标' }))
  fireEvent.change(screen.getByLabelText('目标名称'), { target: { value: 'Blog' } })
  fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'blog.example.com' } })
  fireEvent.click(screen.getByRole('button', { name: '创建目标' }))

  expect(screen.getByText('执行节点标签至少需要填写一个。')).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledTimes(1)
})
```

- [ ] **Step 2: Run focused TargetsPage tests and confirm failure**

Run:

```bash
cd web && npm test -- --run TargetsPage
```

Expected: fail because create controls do not exist.

- [ ] **Step 3: Implement Target creation state and helpers**

In `TargetsPage.tsx`:

- Import `type FormEvent`, `useNavigate`, `createTarget`, and `CreateTargetInput`.
- Add `CreateTargetFormState` with string fields for all inputs.
- Add `initialCreateForm`:

```ts
const initialCreateForm = {
  name: '',
  targetType: 'service',
  host: '',
  basePort: '',
  executionNodeLabels: '',
  runStatus: '启用',
  labels: '',
  note: '',
}
```

- Add state: `createOpen`, `createSubmitting`, `createError`, `createForm`.
- Add `parseLabels`, `parseOptionalPositiveInteger`, and `buildCreateTargetInput`.
- `buildCreateTargetInput` must trim strings, require at least one execution label, and omit `base_port` when blank.

- [ ] **Step 4: Render creation controls**

Update the header to include:

```tsx
<button type="button" onClick={() => setCreateOpen((current) => !current)}>
  新建目标
</button>
```

When `targets.length === 0`, render an empty state with:

```tsx
<button type="button" onClick={() => setCreateOpen(true)}>
  创建第一个目标
</button>
```

When `createOpen`, render a `page-panel` form with labels exactly:

- `目标名称`
- `目标类型`
- `Host`
- `Base Port`
- `执行节点标签`
- `运行状态`
- `目标标签`
- `备注`

Submit button text:

- normal: `创建目标`
- submitting: `正在创建…`

On successful create:

```ts
setTargets((current) => [created, ...current.filter((item) => item.target_id !== created.target_id)])
navigate(`/targets/${created.target_id}`)
```

- [ ] **Step 5: Run focused TargetsPage tests and commit**

Run:

```bash
cd web && npm test -- --run TargetsPage
```

Expected: pass.

Commit:

```bash
git add web/src/pages/TargetsPage.tsx web/src/pages/TargetsPage.test.tsx
git commit -m "Add the V1 target creation entry point"
```

---

### Task 3: Add ProbeItem Creation Flow on Target Detail

**Files:**
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.test.tsx`

- [ ] **Step 1: Write failing TargetDetailPage tests**

Add a test after the existing empty-state test:

```ts
it('creates an HTTP ProbeItem from the empty state and appends it to the list', async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(
      mockJSONResponse({
        target_id: 'tg_002',
        name: 'Cache',
        target_type: 'service',
        host: 'cache.example.com',
        base_port: 443,
        execution_node_labels: ['edge'],
        run_status: '启用',
        labels: [],
        note: '',
        current_health_status: '正常',
        current_active_incident_count: 0,
        current_primary_issue_summary: '',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T09:05:00Z',
      }),
    )
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse({ target_id: 'tg_002', latest_probe_observations: [] }))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(
      mockJSONResponse(
        {
          probe_item_id: 'pb_new',
          target_id: 'tg_002',
          probe_kind: 'http',
          enabled: true,
          frequency_tier: '1m',
          timeout_seconds: 5,
          config: {
            scheme: 'https',
            path: '/healthz',
            method: 'GET',
            expected_status_range: [200, 299],
          },
          created_at: '2026-04-27T09:00:00Z',
          updated_at: '2026-04-27T09:00:00Z',
        },
        201,
      ),
    )
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter initialEntries={['/targets/tg_002']}>
      <Routes>
        <Route path="/targets/:targetId" element={<TargetDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() => expect(screen.getByText('当前还没有 ProbeItem')).toBeInTheDocument())

  fireEvent.click(screen.getByRole('button', { name: '添加 ProbeItem' }))
  fireEvent.change(screen.getByLabelText('Probe 类型'), { target: { value: 'http' } })
  fireEvent.change(screen.getByLabelText('HTTP Scheme'), { target: { value: 'https' } })
  fireEvent.change(screen.getByLabelText('HTTP Path'), { target: { value: '/healthz' } })
  fireEvent.change(screen.getByLabelText('HTTP Method'), { target: { value: 'GET' } })
  fireEvent.change(screen.getByLabelText('期望状态码起点'), { target: { value: '200' } })
  fireEvent.change(screen.getByLabelText('期望状态码终点'), { target: { value: '299' } })
  fireEvent.change(screen.getByLabelText('超时秒数'), { target: { value: '5' } })
  fireEvent.change(screen.getByLabelText('频率档位'), { target: { value: '1m' } })
  fireEvent.click(screen.getByRole('button', { name: '创建 ProbeItem' }))

  await waitFor(() => expect(screen.getByText('HTTP')).toBeInTheDocument())
  expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_002/probe-items', {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    body: JSON.stringify({
      probe_kind: 'http',
      enabled: true,
      frequency_tier: '1m',
      timeout_seconds: 5,
      config: {
        scheme: 'https',
        path: '/healthz',
        method: 'GET',
        expected_status_range: [200, 299],
      },
    }),
  })
})
```

Add a validation/error test:

```ts
it('keeps ProbeItem creation validation errors inside the probe panel', async () => {
  vi.stubGlobal(
    'fetch',
    vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse({
        target_id: 'tg_002',
        name: 'Cache',
        target_type: 'service',
        host: 'cache.example.com',
        execution_node_labels: ['edge'],
        run_status: '启用',
        labels: [],
        note: '',
        current_health_status: '正常',
        current_active_incident_count: 0,
        current_primary_issue_summary: '',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T09:05:00Z',
      }))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse({ target_id: 'tg_002', latest_probe_observations: [] }))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([])),
  )

  render(
    <MemoryRouter initialEntries={['/targets/tg_002']}>
      <Routes>
        <Route path="/targets/:targetId" element={<TargetDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() => expect(screen.getByRole('button', { name: '添加 ProbeItem' })).toBeInTheDocument())

  fireEvent.click(screen.getByRole('button', { name: '添加 ProbeItem' }))
  fireEvent.change(screen.getByLabelText('端口'), { target: { value: '' } })
  fireEvent.click(screen.getByRole('button', { name: '创建 ProbeItem' }))

  expect(screen.getByText('端口必须为正整数。')).toBeInTheDocument()
  expect(screen.getByText('当前还没有 ProbeItem')).toBeInTheDocument()
})
```

- [ ] **Step 2: Run focused TargetDetailPage tests and confirm failure**

Run:

```bash
cd web && npm test -- --run TargetDetailPage
```

Expected: fail because ProbeItem creation controls do not exist.

- [ ] **Step 3: Implement ProbeItem creation state and builders**

In `TargetDetailPage.tsx`:

- Import `type FormEvent`, `createProbeItem`, `CreateProbeItemInput`, `FrequencyTier`, and `ProbeKind`.
- Add constants for probe kind and frequency options.
- Add form state:

```ts
type ProbeCreateFormState = {
  probeKind: ProbeKind
  enabled: boolean
  frequencyTier: FrequencyTier
  timeoutSeconds: string
  port: string
  httpScheme: string
  httpPath: string
  httpMethod: 'GET' | 'HEAD'
  expectedStatusStart: string
  expectedStatusEnd: string
  tlsExpiryWarningDays: string
}
```

Use defaults:

```ts
const initialProbeCreateForm: ProbeCreateFormState = {
  probeKind: 'tcp',
  enabled: true,
  frequencyTier: '5m',
  timeoutSeconds: '5',
  port: '',
  httpScheme: 'https',
  httpPath: '/',
  httpMethod: 'GET',
  expectedStatusStart: '200',
  expectedStatusEnd: '299',
  tlsExpiryWarningDays: '14',
}
```

When target has `base_port`, opening the form may prefill `port` from `String(target.base_port)`.

Build config with:

```ts
function buildProbeCreateInput(form: ProbeCreateFormState): CreateProbeItemInput {
  const timeoutSeconds = parseRequiredPositiveInteger(form.timeoutSeconds, '超时秒数')
  if (form.probeKind === 'tcp') {
    return {
      probe_kind: 'tcp',
      enabled: form.enabled,
      frequency_tier: form.frequencyTier,
      timeout_seconds: timeoutSeconds,
      config: { port: parseRequiredPositiveInteger(form.port, '端口') },
    }
  }
  if (form.probeKind === 'http') {
    const start = parseRequiredPositiveInteger(form.expectedStatusStart, '期望状态码起点')
    const end = parseRequiredPositiveInteger(form.expectedStatusEnd, '期望状态码终点')
    if (start > end) {
      throw new Error('期望状态码起点不能大于终点。')
    }
    return {
      probe_kind: 'http',
      enabled: form.enabled,
      frequency_tier: form.frequencyTier,
      timeout_seconds: timeoutSeconds,
      config: {
        scheme: form.httpScheme.trim(),
        path: form.httpPath.trim() || '/',
        method: form.httpMethod,
        expected_status_range: [start, end],
      },
    }
  }
  return {
    probe_kind: 'tls',
    enabled: form.enabled,
    frequency_tier: form.frequencyTier,
    timeout_seconds: timeoutSeconds,
    config: {
      port: parseRequiredPositiveInteger(form.port, '端口'),
      expiry_warning_days: parseRequiredPositiveInteger(form.tlsExpiryWarningDays, '证书预警天数'),
    },
  }
}
```

- [ ] **Step 4: Render ProbeItem creation controls**

In the `Probe Items` section, add an `添加 ProbeItem` button above the empty/list content.

When `probeCreateOpen` is true, render a `page-panel` form with labels exactly:

- `Probe 类型`
- `启用 ProbeItem`
- `频率档位`
- `超时秒数`
- TCP/TLS label: `端口`
- HTTP labels: `HTTP Scheme`, `HTTP Path`, `HTTP Method`, `期望状态码起点`, `期望状态码终点`
- TLS label: `证书预警天数`

Submit button:

- normal: `创建 ProbeItem`
- submitting: `正在创建…`

On successful create:

```ts
setState((current) => ({
  ...current,
  probeItems: [...current.probeItems, created],
}))
setProbeCreateOpen(false)
setProbeCreateForm(initialProbeCreateFormForTarget(target))
```

- [ ] **Step 5: Run focused TargetDetailPage tests and commit**

Run:

```bash
cd web && npm test -- --run TargetDetailPage
```

Expected: pass.

Commit:

```bash
git add web/src/pages/TargetDetailPage.tsx web/src/pages/TargetDetailPage.test.tsx
git commit -m "Add probe creation to target detail"
```

---

### Task 4: Full Verification and Review

**Files:**
- No planned source edits unless verification exposes a small issue.

- [ ] **Step 1: Run focused web tests**

Run:

```bash
cd web && npm test -- --run api
cd web && npm test -- --run TargetsPage
cd web && npm test -- --run TargetDetailPage
```

Expected: all pass.

- [ ] **Step 2: Run all repository verification**

Run:

```bash
go test ./...
cd web && npm test -- --run
cd web && npm run build
./scripts/verify.sh
```

Expected: all pass.

- [ ] **Step 3: Final review**

Review that:

- Target create remains short form only.
- ProbeItem create only exists in Target detail.
- No edit/delete/enable-disable behavior was added.
- Existing runtime controls still work.
- No stale route update behavior was introduced.

Commit only if the review finds a small final fix.

---

## Self-Review

Spec coverage:

- Covers Target list create entry and empty-state CTA.
- Covers Target create POST helper and detail navigation.
- Covers ProbeItem create helper and Target detail inline form.
- Explicitly excludes edit/delete/status work.

Placeholder scan:

- No placeholder markers remain.
- Each task has concrete files, test names, commands, and expected outcomes.

Type consistency:

- `CreateTargetInput` maps to the Go handler JSON body.
- `CreateProbeItemInput` maps to the Go handler JSON body.
- Probe kind and frequency unions match existing backend constants.
