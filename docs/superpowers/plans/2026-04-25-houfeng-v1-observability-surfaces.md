# Houfeng V1 Observability Surfaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the current placeholder observability pages into real V1 monitoring surfaces by wiring the existing incidents/events backend into the dashboard, events page, and node/target detail views.

**Architecture:** Reuse the existing dashboard/events read models and add one minimal read-only `/api/incidents` surface for active incidents. Keep the slice read-focused: backend evaluation logic stays unchanged, while the frontend replaces placeholders with real summary, incident, and event sections using the frozen app shell and page hierarchy.

**Tech Stack:** Go, net/http, PostgreSQL read queries, React, TypeScript, Vite, Vitest

---

## Planned file structure

### Backend read API
- Modify: `internal/center/store/incidents.go`
- Modify: `internal/center/store/incidents_test.go`
- Create: `internal/center/http/handlers/incidents.go`
- Create: `internal/center/http/handlers/incidents_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_api_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

### Frontend data layer and shared read components
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Create: `web/src/components/IncidentList.tsx`
- Create: `web/src/components/EventList.tsx`

### Dashboard and events pages
- Modify: `web/src/pages/DashboardPage.tsx`
- Create: `web/src/pages/DashboardPage.test.tsx`
- Modify: `web/src/pages/EventsPage.tsx`
- Create: `web/src/pages/EventsPage.test.tsx`

### Detail pages
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Modify: `web/src/pages/NodeDetailPage.test.tsx`
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.test.tsx`

---

### Task 1: Add read-only active incidents backend API

**Files:**
- Modify: `internal/center/store/incidents.go`
- Modify: `internal/center/store/incidents_test.go`
- Create: `internal/center/http/handlers/incidents.go`
- Create: `internal/center/http/handlers/incidents_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_api_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] **Step 1: Write failing store tests for active-incident list/filter behavior**

Add tests that lock:
- list all active incidents with default limit
- filter by `object_type`
- filter by `object_id`
- filter by `severity`
- newest-first ordering

Run: `go test ./internal/center/store -run 'TestPostgresIncidentRepositoryListActiveIncidents' -v`
Expected: FAIL because the list/read API does not exist yet.

- [ ] **Step 2: Write failing handler/router tests for `/api/incidents`**

Add tests that lock:
- `GET /api/incidents?object_type=node&object_id=nd_001&limit=25`
- invalid `limit` returns `400`
- route stays outside SPA fallback

Run: `go test ./internal/center/http/... -run 'Test(Incidents|Router)' -v`
Expected: FAIL because the handler/route is not wired yet.

- [ ] **Step 3: Add the minimal read model to `internal/center/store/incidents.go`**

Implement:
- `type ActiveIncidentListItem struct { ... }`
- `type IncidentsFilter struct { ObjectType, ObjectID, Severity, Limit }`
- `func (r *PostgresIncidentRepository) ListActiveIncidents(ctx context.Context, filter IncidentsFilter) ([]ActiveIncidentListItem, error)`

Query shape should read from `active_incidents`, allow optional filters, order by:
1. severity rank desc
2. started_at desc
3. incident_id asc

- [ ] **Step 4: Add `/api/incidents` handler and route wiring**

Implement handler behavior:
- `GET` only
- parse `object_type`, `object_id`, `severity`, `limit`
- invalid limit => `400`
- repository failure => `500`
- success => JSON list

Wire through:
- `internal/center/http/router.go`
- `cmd/houfeng-center/bootstrap.go`
- bootstrap tests / router tests

- [ ] **Step 5: Re-run focused backend tests**

Run:
- `go test ./internal/center/store -run 'TestPostgresIncidentRepositoryListActiveIncidents' -v`
- `go test ./internal/center/http/... -run 'Test(Incidents|Router)' -v`
Expected: PASS

- [ ] **Step 6: Commit the backend read-API slice**

Suggested Lore intent line:
- `Expose active incidents as a first-class read surface for V1 pages`

---

### Task 2: Extend frontend data layer and shared observability presentation

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Create: `web/src/components/IncidentList.tsx`
- Create: `web/src/components/EventList.tsx`

- [ ] **Step 1: Write failing frontend data-layer tests for new API helpers**

Extend `web/src/lib/api.test.ts` or create a nearby test to lock:
- `getDashboard()` parses `/api/dashboard`
- `listEvents()` supports query serialization
- `listIncidents()` supports query serialization

Run: `cd web && npm test -- --run api`
Expected: FAIL because helpers/types do not exist yet.

- [ ] **Step 2: Add dashboard/event/incident client types**

Extend `web/src/lib/types.ts` with:
- `DashboardOverview`
- `StateChangeEventRecord` or page-level event item type
- `ActiveIncidentRecord`
- lightweight filter types if helpful

- [ ] **Step 3: Add new API helpers to `web/src/lib/api.ts`**

Implement:
- `getDashboard()`
- `listEvents(filter?)`
- `listIncidents(filter?)`

Keep them on top of existing `requestJSON()` and serialize only non-empty query params.

- [ ] **Step 4: Add reusable read-only list components**

Create:
- `web/src/components/IncidentList.tsx`
- `web/src/components/EventList.tsx`

Component responsibilities:
- accept already-fetched data
- render empty state messages when arrays are empty
- stay presentational only
- reuse `StatusBadge` and existing text/card styling patterns

- [ ] **Step 5: Re-run focused frontend tests**

Run: `cd web && npm test -- --run api`
Expected: PASS

- [ ] **Step 6: Commit the frontend data/presentation slice**

Suggested Lore intent line:
- `Give the web app shared readers for incident and event data`

---

### Task 3: Replace dashboard and events placeholders with real pages

**Files:**
- Modify: `web/src/pages/DashboardPage.tsx`
- Create: `web/src/pages/DashboardPage.test.tsx`
- Modify: `web/src/pages/EventsPage.tsx`
- Create: `web/src/pages/EventsPage.test.tsx`

- [ ] **Step 1: Write failing dashboard/events page tests**

Dashboard test should lock:
- loading state
- summary counts render
- abnormal node/target sections render
- recent events render

Events page test should lock:
- loading state
- fetched events render chronologically
- empty state copy renders when no events exist

Run: `cd web && npm test -- --run DashboardPage EventsPage`
Expected: FAIL because pages are still placeholders.

- [ ] **Step 2: Implement `DashboardPage` using `/api/dashboard`**

Render:
- summary cards for abnormal/severe/maintenance/recent counts
- abnormal node summary section (at minimum use counts + strongest summary blocks from dashboard payload)
- abnormal target summary section
- recent events section via `EventList`

Important: do not introduce charts or new visual motifs.

- [ ] **Step 3: Implement `EventsPage` using `/api/events`**

Render:
- page title and frozen shell hierarchy
- lightweight filter controls for object type / severity / event type / limit
- read-only list via `EventList`
- empty and error states

- [ ] **Step 4: Re-run focused page tests**

Run: `cd web && npm test -- --run DashboardPage EventsPage`
Expected: PASS

- [ ] **Step 5: Commit the page-replacement slice**

Suggested Lore intent line:
- `Turn observability placeholders into real dashboard and timeline pages`

---

### Task 4: Replace node/target detail placeholders with real incidents and events

**Files:**
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Modify: `web/src/pages/NodeDetailPage.test.tsx`
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.test.tsx`

- [ ] **Step 1: Write failing detail-page tests for incident/event sections**

Node detail test should lock:
- active incidents fetched and rendered
- related events fetched and rendered
- empty active-incident state is explicit

Target detail test should lock the same for target filters.

Run: `cd web && npm test -- --run NodeDetailPage TargetDetailPage`
Expected: FAIL because these sections are still placeholders and no extra fetches happen.

- [ ] **Step 2: Extend `NodeDetailPage`**

Add fetches for:
- `/api/incidents?object_type=node&object_id=:id`
- `/api/events?object_type=node&object_id=:id`

Replace the reserved block with:
- `IncidentList`
- `EventList`

Keep existing runtime-facts section untouched.

- [ ] **Step 3: Extend `TargetDetailPage`**

Add fetches for:
- `/api/incidents?object_type=target&object_id=:id`
- `/api/events?object_type=target&object_id=:id`

Replace the reserved block with:
- `IncidentList`
- `EventList`

Keep existing probe/runtime section untouched.

- [ ] **Step 4: Re-run focused detail-page tests**

Run: `cd web && npm test -- --run NodeDetailPage TargetDetailPage`
Expected: PASS

- [ ] **Step 5: Run full verification for the slice**

Run:
- `go test ./internal/center/store -v`
- `go test ./internal/center/http/... -v`
- `go test ./internal/center/... -v`
- `go test ./...`
- `./scripts/verify.sh`
Expected: PASS

- [ ] **Step 6: Commit the detail-surface slice**

Suggested Lore intent line:
- `Make node and target detail pages show current incidents and recent changes`

---

## Self-review

### Spec coverage
- Covers the largest current V1 UI gap: dashboard/events/detail observability placeholders
- Adds the single missing backend read API needed for active incidents
- Does not drift into control actions, settings, trend charts, or new incident classes

### Placeholder scan
- No TBD/TODO placeholders remain in the plan
- Each task has named files, focused steps, and concrete verification commands

### Type consistency
- Backend read shape stays centered on incidents/events as flat read models
- Frontend uses shared list components rather than embedding duplicate markup into four pages
- Detail pages depend on `/api/incidents` + `/api/events`, not ad hoc embedded payloads
