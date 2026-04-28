# Houfeng Dashboard and Events Acceptance Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the remaining V1 Phase 4 acceptance gaps by showing current abnormal Node/Target summaries on Dashboard and adding advanced Events filters.

**Architecture:** Extend the existing SQL-first `/api/dashboard` and `/api/events` read models. Add only query-support indexes, reuse current object summary fields, and keep frontend changes inside the current page/card vocabulary.

**Tech Stack:** Go, pgx, raw SQL migrations, net/http, React, TypeScript, Vite, Vitest

---

## Scope

In scope:

- Dashboard abnormal Node summaries.
- Dashboard abnormal Target summaries.
- Events filters for time range, label, notification-only, recovery-only, and maintenance-only.
- Query-support indexes.

Out of scope:

- trend degradation rules;
- trend charts;
- saved filters/workbench views;
- notification delivery management UI;
- custom dashboard layout;
- generic search language.

## Planned file structure

### Query indexes

- Create: `db/migrations/0009_add_observability_filter_indexes.sql`
- Modify: `internal/center/store/migrate/migrate_test.go`

### Dashboard summaries

- Modify: `internal/center/incidents/types.go`
- Modify: `internal/center/store/dashboard.go`
- Modify: `internal/center/store/dashboard_test.go`
- Modify: `internal/center/http/handlers/dashboard_test.go`
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/pages/DashboardPage.tsx`
- Modify: `web/src/pages/DashboardPage.test.tsx`

### Events filters

- Modify: `internal/center/store/dashboard.go`
- Modify: `internal/center/store/dashboard_test.go`
- Modify: `internal/center/http/handlers/events.go`
- Modify: `internal/center/http/handlers/events_test.go`
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`
- Modify: `web/src/pages/EventsPage.tsx`
- Modify: `web/src/pages/EventsPage.test.tsx`

---

## Task 1: Add observability filter indexes

**Files:**
- Create: `db/migrations/0009_add_observability_filter_indexes.sql`
- Modify: `internal/center/store/migrate/migrate_test.go`

- [ ] **Step 1: Write the failing migration-order test**

Update `TestNamesIncludesBaselineAndFollowupMigrations` so it expects a tenth migration:

```go
if len(names) < 10 {
	t.Fatalf("len(Names()) = %d, want at least 10", len(names))
}
if names[9] != "0009_add_observability_filter_indexes.sql" {
	t.Fatalf("tenth migration = %q, want %q", names[9], "0009_add_observability_filter_indexes.sql")
}
```

- [ ] **Step 2: Run migration test and confirm failure**

Run:

```bash
go test ./internal/center/store/migrate -run TestNamesIncludesBaselineAndFollowupMigrations -v
```

Expected: FAIL because migration `0009_add_observability_filter_indexes.sql` does not exist yet.

- [ ] **Step 3: Add the migration**

Create `db/migrations/0009_add_observability_filter_indexes.sql`:

```sql
create index if not exists idx_state_change_events_created_at
  on state_change_events (created_at desc);

create index if not exists idx_notification_records_incident_object
  on notification_records (incident_id, object_type, object_id);

create index if not exists idx_nodes_labels_gin
  on nodes using gin (labels);

create index if not exists idx_targets_labels_gin
  on targets using gin (labels);
```

- [ ] **Step 4: Verify migration tests**

Run:

```bash
go test ./internal/center/store/migrate -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

Suggested Lore intent line:

```text
Support V1 observability filters with narrow indexes
```

---

## Task 2: Add Dashboard abnormal summary backend contract

**Files:**
- Modify: `internal/center/incidents/types.go`
- Modify: `internal/center/store/dashboard.go`
- Modify: `internal/center/store/dashboard_test.go`
- Modify: `internal/center/http/handlers/dashboard_test.go`

- [ ] **Step 1: Add failing dashboard store tests**

Extend `TestPostgresDashboardRepositoryReturnsOverviewAndRecentEvents` or add a new test proving:

- `GetDashboardOverview(ctx, 10)` populates `AbnormalNodes`;
- `GetDashboardOverview(ctx, 10)` populates `AbnormalTargets`;
- summary queries order by severity rank and active incident count;
- empty result sets become empty arrays, not `nil` JSON surprises.

Run:

```bash
go test ./internal/center/store -run TestPostgresDashboardRepository -v
```

Expected: FAIL because the summary fields do not exist yet.

- [ ] **Step 2: Add failing dashboard handler assertion**

Extend `TestDashboardHandlerReturnsOverview` to assert snake_case keys:

- `abnormal_nodes`
- `abnormal_targets`

and no exported Go field names.

Run:

```bash
go test ./internal/center/http/handlers -run TestDashboardHandlerReturnsOverview -v
```

Expected: FAIL until the contract exists.

- [ ] **Step 3: Add backend response types**

In `internal/center/incidents/types.go`, add:

```go
type DashboardNodeSummary struct {
	NodeID                     string     `json:"node_id"`
	DisplayName                string     `json:"display_name"`
	Region                     string     `json:"region"`
	City                       string     `json:"city"`
	Provider                   string     `json:"provider"`
	LifecycleStatus            string     `json:"lifecycle_status"`
	MonitoringStatus           string     `json:"monitoring_status"`
	CurrentHealthStatus        string     `json:"current_health_status"`
	LastHeartbeatAt            *time.Time `json:"last_heartbeat_at,omitempty"`
	CurrentActiveIncidentCount int        `json:"current_active_incident_count"`
	CurrentPrimaryIssueSummary string     `json:"current_primary_issue_summary"`
}

type DashboardTargetSummary struct {
	TargetID                   string     `json:"target_id"`
	Name                       string     `json:"name"`
	TargetType                 string     `json:"target_type"`
	Host                       string     `json:"host"`
	BasePort                   *int       `json:"base_port,omitempty"`
	RunStatus                  string     `json:"run_status"`
	CurrentHealthStatus        string     `json:"current_health_status"`
	LastSuccessAt              *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt              *time.Time `json:"last_failure_at,omitempty"`
	CurrentActiveIncidentCount int        `json:"current_active_incident_count"`
	CurrentPrimaryIssueSummary string     `json:"current_primary_issue_summary"`
}
```

Then extend `DashboardOverview`:

```go
AbnormalNodes   []DashboardNodeSummary   `json:"abnormal_nodes"`
AbnormalTargets []DashboardTargetSummary `json:"abnormal_targets"`
```

- [ ] **Step 4: Implement SQL readers**

In `internal/center/store/dashboard.go`, add helper functions:

- `loadAbnormalNodeSummaries(ctx, queryer, limit)`
- `loadAbnormalTargetSummaries(ctx, queryer, limit)`

Both should filter `current_health_status <> '正常'`, order by severity rank, active incident count, update recency, and id, then apply `limit`.

- [ ] **Step 5: Wire summaries into overview**

In `GetDashboardOverview`, call the new helpers after loading counts and before returning:

```go
overview.AbnormalNodes, err = loadAbnormalNodeSummaries(ctx, r.db, limit)
if err != nil {
	return incidents.DashboardOverview{}, err
}
overview.AbnormalTargets, err = loadAbnormalTargetSummaries(ctx, r.db, limit)
if err != nil {
	return incidents.DashboardOverview{}, err
}
```

- [ ] **Step 6: Verify backend tests**

Run:

```bash
go test ./internal/center/store -run TestPostgresDashboardRepository -v
go test ./internal/center/http/handlers -run TestDashboardHandlerReturnsOverview -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

Suggested Lore intent line:

```text
Expose current abnormal objects in the dashboard contract
```

---

## Task 3: Render Dashboard abnormal summaries

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/pages/DashboardPage.tsx`
- Modify: `web/src/pages/DashboardPage.test.tsx`

- [ ] **Step 1: Write failing frontend test**

Extend `DashboardPage.test.tsx` so the mocked `/api/dashboard` response includes:

- one `abnormal_nodes` item;
- one `abnormal_targets` item.

Assert the page renders:

- node display name;
- node primary issue summary;
- target name;
- target primary issue summary;
- links to `/nodes/:nodeId` and `/targets/:targetId`.

Run:

```bash
cd web && npm test -- --run DashboardPage
```

Expected: FAIL because current page renders only count cards.

- [ ] **Step 2: Add frontend types**

In `web/src/lib/types.ts`, add matching `DashboardNodeSummary` and `DashboardTargetSummary` types and extend `DashboardOverview` with:

```ts
abnormal_nodes: DashboardNodeSummary[]
abnormal_targets: DashboardTargetSummary[]
```

- [ ] **Step 3: Render summary cards**

In `DashboardPage.tsx`, replace count-only content inside “异常节点概览” and “异常目标概览” with bounded summary lists. Keep count cards, then render object cards below them.

Use existing `Link`, `StatusBadge`, and `formatDateTime` patterns. Do not add charts.

- [ ] **Step 4: Verify frontend tests and build**

Run:

```bash
cd web && npm test -- --run DashboardPage
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 5: Commit**

Suggested Lore intent line:

```text
Show abnormal object summaries on the V1 dashboard
```

---

## Task 4: Add Events advanced backend filters

**Files:**
- Modify: `internal/center/store/dashboard.go`
- Modify: `internal/center/store/dashboard_test.go`
- Modify: `internal/center/http/handlers/events.go`
- Modify: `internal/center/http/handlers/events_test.go`

- [ ] **Step 1: Add failing store filter tests**

Extend `TestPostgresDashboardRepositoryListEventsBuildsFilters` or add focused tests proving SQL includes:

- `created_at >=`
- `created_at <=`
- node/target label predicates using `nodes.labels` and `targets.labels`;
- `exists` against `notification_records`;
- `event_type = incident_recovered` for `RecoveryOnly`;
- maintenance event-type `in (...)` for `MaintenanceOnly`.

Run:

```bash
go test ./internal/center/store -run TestPostgresDashboardRepositoryListEvents -v
```

Expected: FAIL because filter fields do not exist yet.

- [ ] **Step 2: Add failing handler parse tests**

Extend `events_test.go` to cover:

- valid `created_from`, `created_to`, `label`, and boolean filters;
- invalid timestamp returns `400`;
- invalid boolean returns `400`.

Run:

```bash
go test ./internal/center/http/handlers -run TestEventsHandler -v
```

Expected: FAIL until parsing exists.

- [ ] **Step 3: Extend `EventsFilter`**

In `internal/center/store/dashboard.go`, extend `EventsFilter`:

```go
CreatedFrom      *time.Time
CreatedTo        *time.Time
Label            string
NotificationOnly bool
RecoveryOnly     bool
MaintenanceOnly  bool
```

- [ ] **Step 4: Implement SQL predicates**

Update `ListEvents` to alias `state_change_events` as `e` and add predicates for the new fields.

Use this maintenance event set:

```go
[]incidents.EventType{
	incidents.EventNodeMonitoringMaintenanceEntered,
	incidents.EventNodeMonitoringMaintenanceExited,
	incidents.EventTargetMaintenanceEntered,
	incidents.EventTargetMaintenanceExited,
}
```

Use notification predicate:

```sql
exists (
  select 1
  from notification_records nr
  where nr.incident_id = e.payload ->> 'incident_id'
    and nr.object_type = e.object_type
    and nr.object_id = e.object_id
)
```

Use label predicate:

```sql
(
  (e.object_type = 'node' and exists (
    select 1 from nodes n where n.node_id = e.object_id and n.labels @> array[$N]::text[]
  ))
  or
  (e.object_type = 'target' and exists (
    select 1 from targets t where t.target_id = e.object_id and t.labels @> array[$N]::text[]
  ))
)
```

- [ ] **Step 5: Implement handler parsing**

In `handlers/events.go`, parse:

- `created_from` and `created_to` with `time.Parse(time.RFC3339, raw)`;
- booleans with `strconv.ParseBool`;
- trim `label`.

Invalid values return `400`.

- [ ] **Step 6: Verify backend tests**

Run:

```bash
go test ./internal/center/store -run TestPostgresDashboardRepositoryListEvents -v
go test ./internal/center/http/handlers -run TestEventsHandler -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

Suggested Lore intent line:

```text
Make the V1 event stream filterable by operational context
```

---

## Task 5: Add Events advanced filter UI

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`
- Modify: `web/src/pages/EventsPage.tsx`
- Modify: `web/src/pages/EventsPage.test.tsx`

- [ ] **Step 1: Add failing API serialization test**

Extend `api.test.ts` to call `listEvents` with:

```ts
{
  object_type: 'node',
  created_from: '2026-04-25T00:00:00Z',
  created_to: '2026-04-26T00:00:00Z',
  label: 'edge',
  notification_only: true,
  recovery_only: true,
  maintenance_only: false,
  limit: 25,
}
```

Assert the URL omits false booleans and includes true booleans.

Run:

```bash
cd web && npm test -- --run api
```

Expected: FAIL until `withQuery` supports booleans and the filter type has the new fields.

- [ ] **Step 2: Add failing Events page test**

Extend `EventsPage.test.tsx` to assert the UI submits:

- start time;
- end time;
- label;
- notification-only;
- recovery-only;
- maintenance-only.

Also assert the reset button returns to the default `/api/events?limit=50` request.

Run:

```bash
cd web && npm test -- --run EventsPage
```

Expected: FAIL until UI controls exist.

- [ ] **Step 3: Extend frontend filter types and query serialization**

In `web/src/lib/types.ts`, extend `EventListFilter` with:

```ts
created_from?: string
created_to?: string
label?: string
notification_only?: boolean
recovery_only?: boolean
maintenance_only?: boolean
```

In `web/src/lib/api.ts`, update `withQuery` to accept booleans and skip `false`.

- [ ] **Step 4: Add Events page controls**

In `EventsPage.tsx`, extend `FilterState` and render controls:

- `开始时间`
- `结束时间`
- `标签`
- `仅看通知事件`
- `仅看恢复事件`
- `仅看维护事件`
- `重置筛选`

Keep existing controls and page structure.

- [ ] **Step 5: Verify frontend tests and build**

Run:

```bash
cd web && npm test -- --run api EventsPage
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 6: Commit**

Suggested Lore intent line:

```text
Expose advanced event filters in the V1 events page
```

---

## Task 6: Phase-level verification and review

**Files:**
- No planned source edits unless review finds issues.

- [ ] **Step 1: Run focused backend verification**

Run:

```bash
go test ./internal/center/store/migrate -v
go test ./internal/center/store -run 'TestPostgresDashboardRepository' -v
go test ./internal/center/http/handlers -run 'Test(Dashboard|Events)Handler' -v
```

- [ ] **Step 2: Run focused frontend verification**

Run:

```bash
cd web && npm test -- --run api DashboardPage EventsPage
cd web && npm run build
```

- [ ] **Step 3: Run full verification**

Run:

```bash
go test ./...
./scripts/verify.sh
```

- [ ] **Step 4: Final review**

Dispatch read-only reviewers:

- spec compliance against this plan and the design spec;
- code quality review for the full Phase 4 closure diff.

- [ ] **Step 5: Inspect git status**

Run:

```bash
git status --short --branch
```

Expected: clean `main`.

## Execution ordering

1. Task 1 first, because Task 4 benefits from the indexes.
2. Task 2 before Task 3, because frontend Dashboard types depend on the API response.
3. Task 4 before Task 5, because frontend Events filters depend on backend query contract.
4. Task 6 last.

Low-conflict parallelism:

- Task 3 frontend Dashboard can be prepared after Task 2 lands.
- Task 5 frontend Events can be prepared after Task 4 lands.
- Reviewers can run in parallel after Task 6 verification.

