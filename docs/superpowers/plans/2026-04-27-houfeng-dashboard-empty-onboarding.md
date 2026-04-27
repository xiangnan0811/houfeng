# Houfeng Dashboard Empty Onboarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Dashboard show the frozen V1 first-run onboarding path when the project has no Node and no Target.

**Architecture:** Extend the existing `/api/dashboard` overview with total Node/Target counts, then derive the frontend first-run state from those counts. Keep normal Dashboard rendering unchanged when either count is non-zero.

**Tech Stack:** Go center API/store, React/Vite/TypeScript, Testing Library, Vitest, existing PostgreSQL tables.

---

## Planned File Structure

- Modify: `internal/center/incidents/types.go`
  - Add `total_node_count` and `total_target_count` to `DashboardOverview`.
- Modify: `internal/center/store/dashboard.go`
  - Query total Node/Target counts in `loadDashboardCounts`.
- Modify: `internal/center/store/dashboard_test.go`
  - Assert total counts are scanned and returned.
- Modify: `internal/center/http/handlers/dashboard_test.go`
  - Assert dashboard JSON includes total counts.
- Modify: `web/src/lib/types.ts`
  - Add total count fields to `DashboardOverview`.
- Modify: `web/src/lib/api.test.ts`
  - Update dashboard API fixture expectations with total counts.
- Modify: `web/src/pages/DashboardPage.tsx`
  - Render first-run onboarding empty state when totals are both zero.
- Modify: `web/src/pages/DashboardPage.test.tsx`
  - Cover fresh-install empty state and update normal dashboard fixture.

No migration is needed.

## Shared User-Facing Copy

Use these strings:

```text
First Run
还没有节点与目标
这不是异常。候风需要先有一个 Node 接入 agent，然后才能创建 Target 并添加 ProbeItem。
创建第一个 Node
接入 agent
创建第一个 Target
添加第一个 ProbeItem
创建第一个节点
```

---

### Task 1: Backend dashboard total counts

**Files:**
- Modify: `internal/center/incidents/types.go`
- Modify: `internal/center/store/dashboard.go`
- Modify: `internal/center/store/dashboard_test.go`
- Modify: `internal/center/http/handlers/dashboard_test.go`

- [ ] **Step 1: Add failing backend tests for total counts**

In `internal/center/store/dashboard_test.go`, update `TestPostgresDashboardRepositoryReturnsOverviewAndRecentEvents` fake scan so `dest[0]` and `dest[1]` are total counts, shifting the existing values down:

```go
*(dest[0].(*int)) = 5 // total nodes
*(dest[1].(*int)) = 4 // total targets
*(dest[2].(*int)) = 2 // abnormal nodes
*(dest[3].(*int)) = 1 // abnormal targets
*(dest[4].(*int)) = 1 // severe nodes
*(dest[5].(*int)) = 0 // severe targets
*(dest[6].(*int)) = 1 // maintenance nodes
*(dest[7].(*int)) = 1 // maintenance targets
*(dest[8].(*int)) = 3 // recent new incidents
*(dest[9].(*int)) = 2 // recent recoveries
```

Add assertions:

```go
if overview.TotalNodeCount != 5 || overview.TotalTargetCount != 4 {
	t.Fatalf("total counts = (%d,%d), want (5,4)", overview.TotalNodeCount, overview.TotalTargetCount)
}
```

In `internal/center/http/handlers/dashboard_test.go`, update the fake result with:

```go
TotalNodeCount:   5,
TotalTargetCount: 4,
```

Then assert decoded JSON:

```go
if body["total_node_count"] != float64(5) {
	t.Fatalf("body = %#v, want total_node_count=5", body)
}
if body["total_target_count"] != float64(4) {
	t.Fatalf("body = %#v, want total_target_count=4", body)
}
```

Run:

```bash
go test ./internal/center/store ./internal/center/http/handlers
```

Expected: fail because `DashboardOverview` lacks total count fields and SQL scan does not provide them.

- [ ] **Step 2: Implement backend total counts**

In `internal/center/incidents/types.go`, update `DashboardOverview`:

```go
type DashboardOverview struct {
	TotalNodeCount          int                      `json:"total_node_count"`
	TotalTargetCount        int                      `json:"total_target_count"`
	AbnormalNodeCount       int                      `json:"abnormal_node_count"`
	AbnormalTargetCount     int                      `json:"abnormal_target_count"`
	SevereNodeCount         int                      `json:"severe_node_count"`
	SevereTargetCount       int                      `json:"severe_target_count"`
	MaintenanceNodeCount    int                      `json:"maintenance_node_count"`
	MaintenanceTargetCount  int                      `json:"maintenance_target_count"`
	RecentNewIncidentCount  int                      `json:"recent_new_incident_count"`
	RecentRecoveryCount     int                      `json:"recent_recovery_count"`
	RecentEvents            []StateChangeEventRecord `json:"recent_events"`
}
```

In `internal/center/store/dashboard.go`, extend the `select` in `loadDashboardCounts` before existing abnormal counts:

```sql
(select count(*) from nodes),
(select count(*) from targets),
```

Extend the `Scan` order:

```go
&overview.TotalNodeCount,
&overview.TotalTargetCount,
&overview.AbnormalNodeCount,
...
```

- [ ] **Step 3: Run backend focused tests and commit**

Run:

```bash
go test ./internal/center/store ./internal/center/http/handlers
```

Expected: pass.

Commit:

```bash
git add internal/center/incidents/types.go internal/center/store/dashboard.go internal/center/store/dashboard_test.go internal/center/http/handlers/dashboard_test.go
git commit -m "Expose Dashboard total resource counts"
```

---

### Task 2: Dashboard first-run empty state

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.test.ts`
- Modify: `web/src/pages/DashboardPage.tsx`
- Modify: `web/src/pages/DashboardPage.test.tsx`

- [ ] **Step 1: Add failing frontend tests**

In `web/src/lib/types.ts`, the type will be updated in Step 2. First update fixtures in tests with total count fields so the later implementation has a stable contract.

In `web/src/lib/api.test.ts`, update the `getDashboard` fixture to include:

```ts
total_node_count: 5,
total_target_count: 4,
```

In `web/src/pages/DashboardPage.test.tsx`, update the existing normal overview fixture to include:

```ts
total_node_count: 5,
total_target_count: 4,
```

Add a new test:

```tsx
it('renders the V1 first-run onboarding path when no nodes or targets exist', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(
      mockJSONResponse({
        total_node_count: 0,
        total_target_count: 0,
        abnormal_node_count: 0,
        abnormal_target_count: 0,
        severe_node_count: 0,
        severe_target_count: 0,
        maintenance_node_count: 0,
        maintenance_target_count: 0,
        recent_new_incident_count: 0,
        recent_recovery_count: 0,
        recent_events: [],
      }),
    ),
  )

  render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  )

  await waitFor(() =>
    expect(screen.getByRole('heading', { name: '还没有节点与目标' })).toBeInTheDocument(),
  )

  expect(screen.getByText('First Run')).toBeInTheDocument()
  expect(
    screen.getByText('这不是异常。候风需要先有一个 Node 接入 agent，然后才能创建 Target 并添加 ProbeItem。'),
  ).toBeInTheDocument()
  expect(screen.getByText('创建第一个 Node')).toBeInTheDocument()
  expect(screen.getByText('接入 agent')).toBeInTheDocument()
  expect(screen.getByText('创建第一个 Target')).toBeInTheDocument()
  expect(screen.getByText('添加第一个 ProbeItem')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: '创建第一个节点' })).toHaveAttribute('href', '/nodes')
  expect(screen.queryByText('异常对象总数')).not.toBeInTheDocument()
})
```

Add `MemoryRouter` import:

```ts
import { MemoryRouter } from 'react-router-dom'
```

Run:

```bash
cd web && npm test -- --run api DashboardPage
```

Expected: fail because DashboardPage does not render the empty state and `DashboardOverview` type lacks total fields.

- [ ] **Step 2: Implement frontend first-run state**

In `web/src/lib/types.ts`, extend `DashboardOverview`:

```ts
total_node_count: number
total_target_count: number
```

In `web/src/pages/DashboardPage.tsx`:

- Import `Link`:

```ts
import { Link } from 'react-router-dom'
```

- After computing `overview`, compute:

```ts
const isFreshInstall = overview.total_node_count === 0 && overview.total_target_count === 0
```

- Before normal summary rendering, return first-run UI when `isFreshInstall`:

```tsx
if (isFreshInstall) {
  return (
    <div className="page-stack">
      <section className="page-panel">
        <p className="page-panel__eyebrow">Dashboard</p>
        <h2 className="page-panel__title">集群概览</h2>
        <p className="page-panel__description">
          查看当前异常、维护与最近状态变更，保持 V1 控制面总览页的信息密度与层级稳定。
        </p>
      </section>

      <section className="page-panel">
        <p className="page-panel__eyebrow">First Run</p>
        <h3 className="page-panel__title">还没有节点与目标</h3>
        <p className="page-panel__description">
          这不是异常。候风需要先有一个 Node 接入 agent，然后才能创建 Target 并添加 ProbeItem。
        </p>
        <ol>
          <li>创建第一个 Node</li>
          <li>接入 agent</li>
          <li>创建第一个 Target</li>
          <li>添加第一个 ProbeItem</li>
        </ol>
        <Link className="text-link" to="/nodes">
          创建第一个节点
        </Link>
      </section>
    </div>
  )
}
```

Keep the normal Dashboard rendering path unchanged.

- [ ] **Step 3: Run focused frontend tests and commit**

Run:

```bash
cd web && npm test -- --run api DashboardPage
cd web && npm run build
```

Expected: pass.

Commit:

```bash
git add web/src/lib/types.ts web/src/lib/api.test.ts web/src/pages/DashboardPage.tsx web/src/pages/DashboardPage.test.tsx
git commit -m "Guide first-run setup from Dashboard"
```

---

### Task 3: Verification and review

**Files:**
- No planned edits unless verification exposes issues.

- [ ] **Step 1: Run focused checks**

Run:

```bash
go test ./internal/center/store ./internal/center/http/handlers
cd web && npm test -- --run api DashboardPage
```

Expected: pass.

- [ ] **Step 2: Run full verification**

Run:

```bash
go test ./...
cd web && npm test -- --run
cd web && npm run build
cd web && npm run lint
./scripts/verify.sh
```

Expected: pass.

- [ ] **Step 3: Scope review**

Confirm:

- Dashboard uses total counts only to identify first-run empty state.
- The CTA points to `/nodes`.
- No wizard, auto-open behavior, or new product capability was added.
- Normal Dashboard still renders when there is at least one Node or Target.

- [ ] **Step 4: Final code review**

Dispatch a fresh code-review subagent for the slice. If blocked, apply `superpowers:receiving-code-review`, fix minimally, rerun focused and full verification, and re-review.
