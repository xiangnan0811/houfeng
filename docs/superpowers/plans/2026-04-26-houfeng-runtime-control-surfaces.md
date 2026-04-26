# Houfeng Runtime Control Surfaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the first usable runtime-control surface for Node and Target objects: maintenance, pause/resume, and Target archive/restore, with correct state-machine enforcement and event history.

**Architecture:** Add explicit repository transition methods and event writes for runtime-control actions, expose them over focused HTTP admin endpoints, then wire minimal but truthful controls into list/detail surfaces. Keep structural editing separate from runtime control, and make the existing Events surface understand the new operational event types.

**Tech Stack:** Go, PostgreSQL, net/http, React, TypeScript, Vite, Vitest

---

## Planned file structure

### Backend repository and transition/event logic
- Modify: `internal/center/incidents/types.go`
- Modify: `internal/center/store/nodes.go`
- Modify: `internal/center/store/nodes_test.go`
- Modify: `internal/center/store/targets.go`
- Create: `internal/center/store/targets_test.go`

### Backend HTTP/admin handlers and routing
- Create: `internal/center/http/handlers/runtime_controls.go`
- Create: `internal/center/http/handlers/runtime_controls_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_api_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

### Frontend shared event typing/render updates
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/components/EventList.tsx`
- Modify: `web/src/components/EventList.test.tsx`
- Modify: `web/src/pages/EventsPage.tsx`
- Modify: `web/src/pages/EventsPage.test.tsx`

### Frontend list/detail controls
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`
- Modify: `web/src/pages/NodesPage.tsx`
- Modify: `web/src/pages/NodesPage.test.tsx`
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Modify: `web/src/pages/NodeDetailPage.test.tsx`
- Modify: `web/src/pages/TargetsPage.tsx`
- Modify: `web/src/pages/TargetsPage.test.tsx`
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.test.tsx`

---

### Task 1: Add repository-backed runtime transitions and event writes

**Files:**
- Modify: `internal/center/incidents/types.go`
- Modify: `internal/center/store/nodes.go`
- Modify: `internal/center/store/nodes_test.go`
- Modify: `internal/center/store/targets.go`
- Create: `internal/center/store/targets_test.go`

- [ ] **Step 1: Write failing repository tests for node transitions**

Add tests that lock:
- node `启用 -> 维护中`
- node `维护中 -> 启用`
- node `启用 -> 暂停`
- node `暂停 -> 启用`
- node `维护中 -> 暂停`
- invalid node `暂停 -> 维护中` is rejected
- each success writes a `state_change_events` row with the right event type and summary

Run:
- `go test ./internal/center/store -run 'TestNodeRuntimeControl' -v`
Expected: FAIL because the transition methods/tests do not exist yet.

- [ ] **Step 2: Write failing repository tests for target transitions**

Add tests that lock:
- target `启用 -> 维护中`
- target `维护中 -> 启用`
- target `启用 -> 暂停`
- target `暂停 -> 启用`
- target `维护中 -> 暂停`
- target archive from active states
- target `已归档 -> 暂停`
- invalid target `已归档 -> 启用` is rejected
- each success writes a runtime-control event row

Run:
- `go test ./internal/center/store -run 'TestTargetRuntimeControl' -v`
Expected: FAIL because the transition methods/tests do not exist yet.

- [ ] **Step 3: Add runtime-control event types**

Extend `internal/center/incidents/types.go` with explicit operational event types:
- `node_monitoring_maintenance_entered`
- `node_monitoring_maintenance_exited`
- `node_monitoring_paused`
- `node_monitoring_resumed`
- `target_maintenance_entered`
- `target_maintenance_exited`
- `target_paused`
- `target_resumed`
- `target_archived`
- `target_restored_to_paused`

Keep these distinct from incident events.

- [ ] **Step 4: Extend node repository with explicit runtime transitions**

Implement guarded methods in `internal/center/store/nodes.go`:
- `SetNodeMonitoringMaintenance`
- `ResumeNodeMonitoring`
- `PauseNodeMonitoring`

Each method should:
- validate source state at SQL boundary
- update `monitoring_status`
- write a `state_change_events` row in the same transaction
- return `nodes.ErrInvalidBindingTransition`-style equivalent for invalid runtime transitions (define a runtime transition error if needed)

- [ ] **Step 5: Extend target repository with explicit runtime transitions**

Implement guarded methods in `internal/center/store/targets.go`:
- `SetTargetMaintenance`
- `ResumeTargetRun`
- `PauseTargetRun`
- `ArchiveTarget`
- `RestoreArchivedTargetToPaused`

Each method should:
- validate source state at SQL boundary
- update `run_status`
- write event history in the same transaction

- [ ] **Step 6: Re-run focused backend repository tests**

Run:
- `go test ./internal/center/store -run 'Test(NodeRuntimeControl|TargetRuntimeControl)' -v`
Expected: PASS

- [ ] **Step 7: Commit the backend transition slice**

Suggested Lore intent line:
- `Make runtime status changes explicit and event-backed`

---

### Task 2: Expose runtime-control HTTP/admin APIs

**Files:**
- Create: `internal/center/http/handlers/runtime_controls.go`
- Create: `internal/center/http/handlers/runtime_controls_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_api_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] **Step 1: Write failing handler/router tests for runtime-control endpoints**

Add tests for:
- `POST /api/nodes/:id/runtime/enter-maintenance`
- `POST /api/nodes/:id/runtime/exit-maintenance`
- `POST /api/nodes/:id/runtime/pause`
- `POST /api/nodes/:id/runtime/resume`
- `POST /api/targets/:id/runtime/enter-maintenance`
- `POST /api/targets/:id/runtime/exit-maintenance`
- `POST /api/targets/:id/runtime/pause`
- `POST /api/targets/:id/runtime/resume`
- `POST /api/targets/:id/runtime/archive`
- `POST /api/targets/:id/runtime/restore-to-paused`
- router keeps these paths outside SPA fallback

Run:
- `go test ./internal/center/http/... -run 'Test(RuntimeControl|Router)' -v`
Expected: FAIL because handlers/routes do not exist yet.

- [ ] **Step 2: Implement runtime-control handlers**

Handlers should:
- be POST-only
- call the explicit repository/service transition methods
- map invalid transitions distinctly from not-found
- return the updated object record (or a minimal updated status payload) for the frontend

- [ ] **Step 3: Wire router and bootstrap**

Add route dispatch into:
- `internal/center/http/router.go`
- `cmd/houfeng-center/bootstrap.go`
- bootstrap tests / router tests

- [ ] **Step 4: Re-run focused backend HTTP tests**

Run:
- `go test ./internal/center/http/... -run 'Test(RuntimeControl|Router)' -v`
- `go test ./cmd/houfeng-center -run 'TestBootstrapCenterBuildsAppOnSuccess' -v`
Expected: PASS

- [ ] **Step 5: Commit the HTTP/admin slice**

Suggested Lore intent line:
- `Expose runtime controls as explicit admin actions`

---

### Task 3: Teach event surfaces about runtime-control events

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/components/EventList.tsx`
- Modify: `web/src/components/EventList.test.tsx`
- Modify: `web/src/pages/EventsPage.tsx`
- Modify: `web/src/pages/EventsPage.test.tsx`

- [ ] **Step 1: Write failing frontend tests for runtime-control event rendering**

Add tests that lock:
- event labels for the new `node_monitoring_*` and `target_*` runtime events
- no blank incident-specific secondary field when the event is operational rather than incident-based
- Events page filter options include the new runtime event types

Run:
- `cd web && npm test -- --run EventList EventsPage`
Expected: FAIL because the event model/render/filter layer does not know these event types yet.

- [ ] **Step 2: Extend frontend event typing**

Update `web/src/lib/types.ts` to include the new runtime-control event types.

- [ ] **Step 3: Update `EventList` labels and rendering**

Add readable labels for all runtime-control event types and avoid incident-shaped meta rendering when `incident_class` is empty.

- [ ] **Step 4: Update Events-page filter options**

Add the new runtime event types as selectable filter options in `web/src/pages/EventsPage.tsx` and lock them with tests.

- [ ] **Step 5: Re-run focused frontend tests**

Run:
- `cd web && npm test -- --run EventList EventsPage`
Expected: PASS

- [ ] **Step 6: Commit the event-surface slice**

Suggested Lore intent line:
- `Make runtime-control history readable in existing event surfaces`

---

### Task 4: Implement list/detail runtime controls in the web UI

**Files:**
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`
- Modify: `web/src/pages/NodesPage.tsx`
- Modify: `web/src/pages/NodesPage.test.tsx`
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Modify: `web/src/pages/NodeDetailPage.test.tsx`
- Modify: `web/src/pages/TargetsPage.tsx`
- Modify: `web/src/pages/TargetsPage.test.tsx`
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.test.tsx`

- [ ] **Step 1: Write failing frontend API helper tests for runtime actions**

Add helper tests for:
- node runtime action calls
- target runtime action calls
- correct POST endpoints

Run:
- `cd web && npm test -- --run api`
Expected: FAIL because runtime-control client helpers do not exist yet.

- [ ] **Step 2: Add runtime-control API helpers**

Extend `web/src/lib/api.ts` with explicit helpers for all node/target runtime transitions.

- [ ] **Step 3: Write failing page tests for list/detail actions**

Nodes/Targets list/detail tests should lock:
- runtime action buttons render according to current state
- maintenance entry/exit uses light confirmation or immediate action per design choice
- pause/archive require stronger confirmation
- action success updates the visible status
- action failure stays local

Run:
- `cd web && npm test -- --run NodesPage NodeDetailPage TargetsPage TargetDetailPage`
Expected: FAIL because runtime actions are not wired yet.

- [ ] **Step 4: Implement Node list/detail runtime controls**

Add:
- `进入维护` / `退出维护`
- `暂停监控` / `恢复监控`

Keep structural editing separate; do not overload existing create/onboarding surfaces.

- [ ] **Step 5: Implement Target list/detail runtime controls**

Add:
- `进入维护` / `退出维护`
- `暂停` / `恢复`
- `归档`
- `恢复到暂停`

Honor the frozen rule that `已归档 -> 启用` is not a direct path.

- [ ] **Step 6: Re-run focused page tests**

Run:
- `cd web && npm test -- --run NodesPage NodeDetailPage TargetsPage TargetDetailPage`
Expected: PASS

- [ ] **Step 7: Commit the runtime-control UI slice**

Suggested Lore intent line:
- `Make runtime status changes operable from node and target surfaces`

---

### Task 5: Full verification and final review

**Files:**
- No new planned files; use verification only unless the previous tasks reveal a small required cleanup.

- [ ] **Step 1: Run full backend verification**

Run:
- `go test ./internal/center/store -v`
- `go test ./internal/center/http/... -v`
- `go test ./internal/center/... -v`
- `go test ./...`
Expected: PASS

- [ ] **Step 2: Run full frontend verification**

Run:
- `cd web && npm run lint && npm test -- --run && npm run build`
Expected: PASS

- [ ] **Step 3: Run repository verify script**

Run:
- `./scripts/verify.sh`
Expected: PASS

- [ ] **Step 4: Commit any final minimal cleanup if the verification loop required it**

Only commit here if Tasks 1-4 required a small post-verification correction.

---

## Self-review

### Spec coverage
- Covers Node maintenance/pause/resume
- Covers Target maintenance/pause/resume/archive/restore-to-paused
- Adds guarded backend transitions, HTTP APIs, event writes, event-surface support, and minimal UI actions
- Keeps runtime control separate from structural editing and onboarding flows

### Placeholder scan
- No TBD/TODO placeholders remain
- Each task names exact files, responsibilities, and verification commands

### Type consistency
- Runtime action event types are distinct from incident events
- Target archive restore path remains `已归档 -> 暂停`, not direct enable
- Frontend event/filter layer and runtime-control helpers stay aligned with backend action names
