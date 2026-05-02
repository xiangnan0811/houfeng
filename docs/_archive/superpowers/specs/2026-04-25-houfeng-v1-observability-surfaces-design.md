# Houfeng V1 Observability Surfaces Design

## Context

The Houfeng backend now has a working first-pass incident / event / notification pipeline, plus read-only `/api/dashboard` and `/api/events` endpoints. The largest remaining V1 product gap is that the web application still exposes placeholder dashboard, events, and detail-page incident/event sections, so the implemented backend value is not yet visible through the frozen V1 interface.

This design selects the next implementation slice after the incidents backend merge. It does **not** reopen V1 product scope or visual direction.

## Constraints

- V1 product, interaction, and visual baselines remain frozen.
- Use the current Stitch baseline hierarchy, especially:
  - `Global App Shell Baseline`
  - `Global Control Center (Unified)`
  - `Node Detail Center (Unified)`
  - `Target Detail` as provisional target-detail content reference
- No new V1一级能力.
- Prefer reusing existing backend read models and page shell patterns.
- Keep this slice read-mostly. Do not mix in maintenance / pause / onboarding control flows.

## Approaches considered

### Approach A — Observability read-surface slice (recommended)
Build the real dashboard page, real events page, and real detail-page incident/event sections on top of the existing backend, adding only the minimum missing read API for active incidents.

**Pros**
- Highest user-visible value from already-landed backend work
- Low product risk because visual baseline is already frozen
- Keeps scope coherent: “make current monitoring state visible”
- Unblocks later control-flow work by giving users truthful current-state views first

**Cons**
- Requires a small extra backend read surface for active incidents
- Touches both backend and frontend in one slice

### Approach B — Operational control-flow slice
Implement onboarding, binding-conflict, maintenance/pause/resume, archive/retire, and reset-binding flows next.

**Pros**
- Moves the product from “can observe” toward “can operate”
- Aligns with several frozen V1 interaction docs

**Cons**
- Higher API and state-machine surface area
- More destructive branches and confirmation UX
- Less leverage from the incidents backend that already exists

### Approach C — Backend completeness slice
Implement trend-degradation incidents, retention/aggregation, and agent-side durable buffering next.

**Pros**
- Improves system depth and long-term correctness
- Closes hidden infrastructure gaps

**Cons**
- Much lower immediate user-visible value
- Higher ambiguity and more backend complexity
- Leaves core V1 pages still looking incomplete

## Recommendation

Proceed with **Approach A** first.

This is the best next slice because Houfeng already has the underlying read data for the home/event experience, but the current web UI still presents placeholders. Turning the frozen observability surfaces into real product screens closes the largest visible gap without reopening design questions.

## Scope

### In scope

1. Replace the dashboard placeholder with a real overview page using `/api/dashboard`
2. Replace the events placeholder with a real timeline page using `/api/events`
3. Add a minimal active-incidents read API for detail pages
4. Replace the Node Detail placeholder incident/event area with real data
5. Replace the Target Detail placeholder incident/event area with real data
6. Reuse existing shell, card, table, and badge language instead of introducing a new design system

### Out of scope

- Settings page implementation
- Onboarding / binding conflict pages
- Maintenance / pause / resume / archive / retire / reset-binding controls
- Trend charts / historical graphing
- Trend-degradation incident classes
- Notification-center UI
- Retention / rollup / aggregation jobs

## Chosen backend shape

### Existing APIs reused as-is

- `GET /api/dashboard`
- `GET /api/events`
- `GET /api/nodes`
- `GET /api/nodes/:id`
- `GET /api/nodes/:id/runtime-facts`
- `GET /api/targets`
- `GET /api/targets/:id`
- `GET /api/targets/:id/probe-items`
- `GET /api/targets/:id/runtime-facts`

### New API to add

- `GET /api/incidents`

Query parameters:
- `object_type` (`node` or `target`, optional)
- `object_id` (optional)
- `severity` (optional)
- `limit` (optional, positive integer, defaulted server-side)

Response shape:
- a flat list of current active incidents, each including
  - `incident_id`
  - `incident_class`
  - `object_type`
  - `object_id`
  - `severity`
  - `started_at`
  - `last_evaluated_at`
  - `source_summary`

### Why a generic incidents endpoint

This keeps the read model aligned with the existing generic `/api/events` filter style and avoids duplicating nearly identical node/target sub-resource handlers. It also keeps current node/target detail payloads lightweight instead of embedding secondary lists into summary records.

## Chosen frontend shape

### Dashboard page

Render the frozen V1 home responsibilities:
- top-level summary cards
- abnormal node summary section
- abnormal target summary section
- recent event stream section

Important constraint:
- do not invent trend charts here
- stay consistent with “current problem first, history second”

### Events page

Render a read-only event timeline/table using `/api/events` with lightweight filters:
- object type
- severity
- event type
- limit

This page should prioritize scanability and chronology, not audit-detail density.

### Node Detail page

Replace the placeholder area with:
- active incidents list from `/api/incidents?object_type=node&object_id=:id`
- recent related events from `/api/events?object_type=node&object_id=:id`

The existing runtime-facts section stays as-is.

### Target Detail page

Replace the placeholder area with:
- active incidents list from `/api/incidents?object_type=target&object_id=:id`
- recent related events from `/api/events?object_type=target&object_id=:id`

The existing probe/runtime section stays as-is.

## Component / file strategy

### Backend

- Keep incident mutation/evaluation logic untouched in this slice
- Add a small read model for active incidents in store layer
- Add one new HTTP handler for `/api/incidents`
- Wire the handler in bootstrap/router without changing existing route semantics

### Frontend

- Extend `web/src/lib/api.ts` and `web/src/lib/types.ts` with dashboard/event/incident types and clients
- Introduce small read-only presentation helpers/components only if they simplify reuse between Dashboard, Events, Node Detail, and Target Detail
- Keep page logic page-local where reuse would be artificial

## Error handling

- Follow existing JSON handler behavior on backend: `400` for invalid limit, `500` for repository failures
- Frontend should continue using `ApiError`
- Pages should render explicit empty states rather than placeholder copy when data is absent
- Empty dashboard/events/incidents are valid states, not errors

## Testing strategy

### Backend

- store tests for active-incident list query/filter behavior
- handler tests for `/api/incidents`
- router tests to keep API/SPA separation intact

### Frontend

- unit/page tests for dashboard rendering
- unit/page tests for events rendering and empty/error states
- detail page tests proving placeholder sections are replaced with real incident/event content

### Verification

- `go test ./internal/center/store -v`
- `go test ./internal/center/http/... -v`
- `go test ./internal/center/... -v`
- `go test ./...`
- `./scripts/verify.sh`

## Risks

1. Dashboard page can drift into “new design work” if we over-customize layout. Avoid that; reuse the frozen shell and current card vocabulary.
2. Detail pages can become overstuffed if we try to solve trend visualization in the same slice. Do not add charts here.
3. There is already a known non-blocking backend risk where notification records are appended outside the primary incident mutation transaction. This slice should not expand that behavior.

## Expected outcome

After this slice, Houfeng should no longer feel like a backend with placeholder pages. Users should be able to open the web app and see:
- current abnormal counts and recent changes on the dashboard
- a real event stream page
- real active incidents and recent events inside node/target detail views

That will make the existing V1 monitoring model legible in the UI while preserving scope for later operational-control and infrastructure-completeness slices.
