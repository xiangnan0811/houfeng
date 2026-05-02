# Houfeng Dashboard Empty Onboarding Design

## Context

Frozen V1 requires the Dashboard to show current problem distribution, high-priority objects, and recent changes. It also explicitly defines a first-run empty state:

1. Create the first Node.
2. Enroll / bind the agent.
3. Create the first Target.
4. Add the first ProbeItem.

The current Dashboard only receives abnormal / severe / maintenance counts and recent events. When a new project has no Node or Target, it still renders normal metric cards full of zeroes, which does not provide the V1 onboarding path.

## Scope

Implement Dashboard first-run onboarding state:

- Add total Node and Target counts to `/api/dashboard`.
- Render a clear Dashboard empty state when both total counts are zero.
- Keep normal Dashboard overview rendering when either total count is non-zero.
- Provide one primary next step: go to `/nodes` and create the first Node.
- Show the frozen V1 sequence as explanatory guidance:
  - 创建第一个 Node
  - 接入 agent
  - 创建第一个 Target
  - 添加第一个 ProbeItem

Out of scope:

- Automatically opening the Node create form from Dashboard.
- Creating a separate onboarding wizard.
- Adding new backend setup state beyond total counts.
- Changing Node/Target creation behavior.
- Redesigning the Dashboard visual hierarchy.

## Approach

Add `total_node_count` and `total_target_count` to the existing `DashboardOverview` contract.

The frontend uses those values to derive:

```ts
const isFreshInstall = overview.total_node_count === 0 && overview.total_target_count === 0
```

When `isFreshInstall` is true, Dashboard renders the standard page header plus an empty-state card. Normal aggregate sections and recent event sections stay hidden to avoid presenting zero-count dashboard chrome as if there were active monitoring data.

## Backend Design

Extend `incidents.DashboardOverview`:

```go
TotalNodeCount   int `json:"total_node_count"`
TotalTargetCount int `json:"total_target_count"`
```

Extend `loadDashboardCounts` to scan:

- total nodes from `nodes`
- total targets from `targets`
- existing abnormal/severe/maintenance/recent counts unchanged

No database migration is required because both tables already exist.

## Frontend Design

Extend `DashboardOverview` in `web/src/lib/types.ts` with:

```ts
total_node_count: number
total_target_count: number
```

In `DashboardPage`:

- Import `Link` from `react-router-dom`.
- Compute `isFreshInstall`.
- Render:
  - Eyebrow: `First Run`
  - Title: `还没有节点与目标`
  - Description: `这不是异常。候风需要先有一个 Node 接入 agent，然后才能创建 Target 并添加 ProbeItem。`
  - Ordered list:
    1. `创建第一个 Node`
    2. `接入 agent`
    3. `创建第一个 Target`
    4. `添加第一个 ProbeItem`
  - Primary link: `创建第一个节点` → `/nodes`

Normal dashboard continues to render when totals are not both zero.

## Error Handling

- Dashboard request errors still render `集群概览不可用`.
- Empty state only depends on successful Dashboard data.
- Missing total fields are not handled as a compatibility mode; this is an implementation repo and backend/frontend are versioned together.

## Testing Strategy

Backend:

- Store test confirms `GetDashboardOverview` scans total node/target counts.
- Handler test confirms `/api/dashboard` response includes total counts.

Frontend:

- API helper test updates the dashboard fixture with total counts.
- Dashboard page test for fresh install:
  - renders empty-state title and explanation,
  - shows all four V1 onboarding steps,
  - links `创建第一个节点` to `/nodes`,
  - does not render aggregate metric cards.
- Existing Dashboard normal rendering test includes non-zero totals and remains unchanged semantically.

## Self-Review

- This does not add a new V1 capability; it implements an explicitly frozen empty-state flow.
- The backend contract change is minimal and derived from existing tables.
- The UI has one recommended next step and does not introduce an onboarding wizard.
- Existing Dashboard error and normal states remain intact.
