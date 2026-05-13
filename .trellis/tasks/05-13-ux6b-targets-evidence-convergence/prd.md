# UX-6B Targets Evidence Convergence

## Problem

UX-6A already made Nodes a first-class evidence surface for asset decisions. Targets still have the right operational primitives, but the first viewport does not yet answer which entry needs attention, why the current filter matters, or how service entry evidence connects back to VPS and asset decisions.

The page should keep its high-density table and existing target operations, while adding a clearer evidence layer for service entry, probe coverage, inactive targets, and Dashboard deep links.

## Goals

- Add a Targets evidence lead that summarizes the current list/filter result and gives one clear next action.
- Show the current Target filter context in the support surface, including Dashboard deep links such as `abnormal=1`, `run_status=暂停`, and `run_status=已归档`.
- Add a top evidence focus item for the highest-priority Target in the current result set.
- Keep the existing create flow, filter panel, batch actions, dense DataTable, row click, runtime confirmations, and metadata editing behavior intact.
- Keep Target semantics scoped to service entry observability and probe coverage. Do not expand this into a complete service registry, DNS management, provider sync, or asset scoring system.
- Update tests, roadmap state, and v2 visual evidence for `/targets` at desktop and mobile viewports.

## Non-Goals

- No backend API or database changes.
- No new UI framework, charting dependency, or browser automation dependency.
- No redesign of Target detail, Events, Dashboard, VPS, or Asset Decisions beyond links/copy needed for this page.
- No inferred VPS health or invented asset relation facts from Targets.

## UX Contract

- The support surface keeps the four existing lanes:
  - abnormal entry;
  - pause/archive context;
  - execution coverage;
  - asset service context.
- Counts and focus items are derived from the current Target list returned by the API.
- The evidence lead uses filtered result counts so Dashboard deep links feel acknowledged immediately.
- The page still exposes clear actions:
  - abnormal lead/lane writes `abnormal=1`;
  - paused lead/lane writes `run_status=暂停`;
  - archived lead/lane writes `run_status=已归档`;
  - empty filtered state clears URL-state;
  - stable/asset context links back to VPS ledger and asset decisions.
- Coverage gaps are represented only when the current Target list contains missing `execution_node_labels`; this is an evidence-boundary signal, not an automatic asset decision.

## Acceptance

- `/targets?abnormal=1` shows an evidence lead for abnormal entries, visible filter context, and a top evidence item.
- `/targets?run_status=暂停` and `/targets?run_status=已归档` preserve visible filter context and inactive-entry lead behavior.
- An empty filtered result can clear filters from the support lead without breaking the existing empty-table clear action.
- A stable Target list shows a calm evidence lead and an explicit no-priority-focus state.
- Tests cover the new evidence lead, filter context, priority item, coverage gap, and stable state.
- Visual evidence is added under `docs/operations/v2-visual-evidence/` with manifest rows for 1440x1000 and 390x900.
- `npm run lint`, targeted Vitest, `npm run build`, and repo web verification pass locally before PR.
