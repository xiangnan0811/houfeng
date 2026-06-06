# Technical Design

## Scope

This task is a frontend information-architecture and workflow-continuity improvement for `AssetDecisionsPage`. It uses existing API responses and existing write helpers only. It must not introduce backend state, migrations, new endpoints, or new business-object mutations.

## Current Shape

`/asset-decisions` already has these layers:

- Automatic groups from `/api/asset-decisions/overview` and `/api/asset-decisions/groups`.
- Scenario templates from `/api/asset-decisions/scenario-templates`.
- Manual groups from `/api/asset-decisions/manual-groups`.
- Decision records from `/api/asset-decisions/records`.
- Record execution readback and execution plan.
- Auxiliary renewal evidence and single VPS decision queue.

The gap is not data availability. The gap is user path clarity: the page has multiple powerful surfaces, but the transition between them is not explicit enough.

## Proposed UX Model

Represent the decision center as four stages:

1. `discover`: automatic groups identify portfolio-level pressure and evidence gaps.
2. `compare`: scenario templates and manual groups turn discovered pressure into a real comparison basket.
3. `decide`: decision records preserve a judgment and evidence snapshot.
4. `execute`: execution readback and plan show whether the saved judgment is aligned, drifting, blocked, or still needs evidence.

These stages are derived locally from loaded data:

- automatic group count, group pressure, current tab, context chips
- manual group count, active/archived status, member count, evidence assessment
- record count, record status, readback status, plan actionable/blocked counts
- local loading/error states

The derived stage model is a scan aid only. It is not persisted and does not change API contracts.

## Page-Level Changes

- Add a compact decision-path surface near the existing portfolio command summary or scenario/records area.
- The surface should be operational rather than tutorial-like: short stage labels, counts, current risk, and one safe CTA per stage when available.
- CTAs can only open existing detail URL-state or existing forms:
  - open automatic group
  - open manual group
  - open record
  - open template
  - switch `view`
- Loading or failed data sources must be displayed as partial availability, not interpreted as healthy or complete.

## Group Detail Changes

Automatic group detail should show a “scenario progression” recommendation:

- Direct record save is appropriate when the current group is already the decision scope.
- Manual group creation is appropriate when the user needs to curate members, add context, or compare a real-life basket.
- Existing `create manual group` and `save record` paths stay unchanged.

No additional backend calls are needed beyond the current create/save actions.

## Manual Group Detail Changes

Manual group detail should show a local progress/checklist summary:

- goal/title readiness
- member readiness
- intended role/action readiness
- evidence gap state
- record-save readiness

This summary is derived from `AssetDecisionManualGroupDetail` and existing member fields. It must not infer IP/routing/performance health.

Existing form semantics remain unchanged:

- group PATCH edits title, goal, note, scenario, status
- member add/patch/delete edits manual group membership and intent only
- save as record posts `source_type=manual_group`
- save as template uses existing template helper

## Record Detail Changes

Record detail should strengthen continuity:

- show source type and source group id in a readable panel
- keep saved evidence snapshot separate from current readback
- keep execution plan board as guidance, not execution
- preserve member followup form semantics

Record detail may offer local navigation to source-related surfaces when safe, but must not auto-create missing source objects or hide missing source drift.

## Styling

- Use existing `page-panel`, `Badge`, `DataTable`, modal, and asset-decision class patterns.
- Keep density aligned with v2 docs: compact operational surfaces, no oversized hero, no decorative card nesting.
- Avoid global token changes.
- Ensure long labels and chips wrap instead of causing horizontal page overflow.

## Compatibility

- API helper names and snake_case types remain unchanged.
- Existing route query keys remain unchanged.
- Existing tests for single queue, renewal evidence, record followup, plan CTA, and manual group writes must continue to pass.

## Risk Controls

- No backend changes means no migration/startup risk.
- New derived helpers should be pure functions where possible and unit-covered through page tests.
- CTA tests must assert no accidental business-object write calls occur.
- Keep automatic groups visually primary to avoid regressing the core information architecture.
