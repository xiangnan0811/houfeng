# Houfeng Node and Target Runtime Control Surfaces Design

## Context

The previous two V1 slices completed:
- observability surfaces
- node onboarding and binding-state workflow

The next frozen V1 gap is runtime control: operators can now see and onboard objects, but they still cannot actually manage Node/Target runtime state from the product.

This slice implements the first usable runtime control surface for:
- Node: enter maintenance, exit maintenance, pause monitoring, resume monitoring
- Target: enter maintenance, exit maintenance, pause, resume, archive, restore from archive to paused

## Why this slice now

The V1 design consistently treats runtime control as a first-class operator flow, distinct from structural editing. The current repo already has the status fields and status semantics, but not the APIs or UI actions needed to operate them.

After observability and onboarding, this is the next highest-value gap because it turns the product from “can observe and admit nodes” into “can actively control monitoring behavior”.

## Approaches considered

### Approach A — backend-first status APIs only
Add status-transition APIs and repository logic now, defer the UI.

**Pros**
- smaller step
- lower frontend scope

**Cons**
- low user-visible value
- leaves frozen runtime-control flows unavailable in-product

### Approach B — full vertical runtime-control slice (recommended)
Implement repository transitions, event writes, HTTP APIs, and the minimal UI actions on list/detail surfaces in one coherent slice.

**Pros**
- closes a real operator loop
- aligns with frozen V1 interaction semantics
- makes status changes visible in the already-built event surfaces

**Cons**
- touches both backend and frontend
- requires careful state-machine enforcement

### Approach C — only list-page quick actions
Implement row-level buttons without deeper state guards or detail-page confirmation surfaces.

**Pros**
- fast UI progress

**Cons**
- pushes too much risk into shallow interactions
- conflicts with the frozen “light vs strong confirmation” distinction

## Recommendation

Use **Approach B**.

Runtime control is a vertical behavior slice. Shipping only the API or only shallow row actions would leave the product internally inconsistent.

## Constraints

- V1 product, interaction, and visual baselines remain frozen.
- Keep structural editing separate from runtime control.
- Do not mix in lifecycle-status editing, deletion, or ProbeItem editing.
- Do not add bulk operations in this slice.
- Reuse existing event surfaces; new runtime actions should emit state-change history that those surfaces can show.

## In scope

### Node runtime control
- `启用 -> 维护中`
- `维护中 -> 启用`
- `启用 -> 暂停`
- `暂停 -> 启用`
- `维护中 -> 暂停`

### Target runtime control
- `启用 -> 维护中`
- `维护中 -> 启用`
- `启用 -> 暂停`
- `暂停 -> 启用`
- `维护中 -> 暂停`
- `启用|维护中|暂停 -> 已归档`
- `已归档 -> 暂停`

### UI surfaces
- Node list quick actions
- Target list quick actions
- Node detail header/runtime control area
- Target detail header/runtime control area
- confirmation treatments for pause/archive

## Out of scope

- lifecycle status changes
- target deletion
- node retirement
- onboarding/binding logic changes
- bulk maintenance/pause/archive actions
- rule editing/settings work

## Chosen backend shape

### Repository transitions
Add explicit repository methods for status transitions rather than generic patch/update calls. This keeps state-machine rules centralized and testable.

Suggested Node methods:
- `SetNodeMonitoringMaintenance(nodeID)`
- `ResumeNodeMonitoring(nodeID)`
- `PauseNodeMonitoring(nodeID)`

Suggested Target methods:
- `SetTargetMaintenance(targetID)`
- `ResumeTargetRun(targetID)`
- `PauseTargetRun(targetID)`
- `ArchiveTarget(targetID)`
- `RestoreArchivedTargetToPaused(targetID)`

### Event writing
Each successful runtime action should persist a node/target state-change event so the existing event surfaces show a visible history.

Suggested event types to add:
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

These are operational state-change events, not incidents.

### State enforcement
Repository methods must reject invalid transitions rather than silently normalizing them.
Examples:
- Node `暂停 -> 维护中` remains invalid in this slice
- Target `已归档 -> 启用` remains invalid; must go through `暂停`

## Chosen frontend shape

### List pages
Add clearly scoped runtime-control entry points without turning rows into button walls.

For Node list rows:
- `进入维护` / `退出维护`
- `暂停监控` / `恢复监控`
- `接入工作台`
- `查看详情`

For Target list rows:
- `进入维护` / `退出维护`
- `暂停` / `恢复`
- `归档`
- `查看详情`

### Detail pages
Use detail headers for clearer risk communication and confirmations.

Node detail:
- lightweight maintenance exit/enter
- stronger confirmation for pause

Target detail:
- maintenance enter/exit
- stronger confirmation for pause
- stronger confirmation for archive
- restore archived target to paused

## Confirmation design

### Light confirmation
- enter maintenance
- exit maintenance
- resume from paused
- restore archived target to paused

### Strong confirmation
- pause node monitoring
- pause target
- archive target

Confirmations must state runtime impact clearly:
- maintenance = continue collecting, do not interpret
- pause = stop collection and create data gaps
- archive = exit active working set but preserve history

## Event-surface requirement

Because the Events page already exists, these runtime events must not appear as blank or incident-shaped entries. This slice should extend the existing frontend event labels/filter options accordingly.

## Testing strategy

### Backend
- repository state-transition tests
- invalid-transition tests
- event-write tests per successful action
- handler/router tests for runtime-control endpoints

### Frontend
- list-page runtime action tests
- detail-page confirmation/action tests
- Events-page regression tests for new runtime-control event types

## Expected outcome

After this slice, Houfeng will support the main V1 runtime controls directly in-product:
- silence observation interpretation via maintenance
- stop and resume collection via pause/resume
- archive and restore Targets without deleting history
- record all of those choices in event history

That closes the next operator workflow after observability and onboarding.
