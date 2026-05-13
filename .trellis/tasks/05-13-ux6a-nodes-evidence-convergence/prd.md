# UX-6A Nodes evidence convergence

## Goal

Turn `/nodes` from a capable node inventory page into the first observability evidence workbench for asset decisions. The page should still support dense node operations, but its first viewport should make the evidence path clearer: what needs attention, why the current filtered set matters, which node is the best next investigation target, and how Node facts connect back to VPS asset decisions.

## What I already know

* The user approved continuing the UI evolution route after UX-5 and explicitly required work to continue in the main session without subagents.
* `docs/release/ui-evolution-roadmap.md` marks UX-6 as `Observability support pages`, with Nodes, Targets and Events positioned as evidence for asset decisions.
* `docs/design/v2-houfeng/component-spec.md` already defines `NodesPage` as `节点观测`, with a support surface named `资产判断支撑`.
* Existing `/nodes` implementation already has a hero, support surface, URL-state filters, DataTable rows, trend sparklines, runtime actions, batch actions and onboarding deep-link support.
* Current support surface is useful but still reads as four summary cards. It does not yet create a strong operational lead from the current evidence set.
* The list contract has `NodeRecord` only. There is no per-row linked VPS summary in the node list response, so the page must not query every node for VPS links or imply row-level linked VPS health.
* Dashboard deep links already rely on `/nodes?abnormal=1`, `/nodes?onboarding=pending`, `/nodes?run_status=暂停`, and `/nodes?lifecycle=已退役`.

## Requirements

* Strengthen `NodesSupportSurface` into an evidence workbench using only the already loaded node list and current URL-state filters.
* Keep the current dense table and operational controls intact.
* Add a current evidence lead that states the next action for the current data set:
  * abnormal nodes first;
  * onboarding or binding issues next;
  * maintenance / paused runtime context next;
  * empty filtered set should tell the user to clear or adjust filters;
  * stable state should point back to VPS inventory / asset decisions.
* Surface the highest-priority node evidence item without expanding into a second table.
* Make filter context visible in the support surface so Dashboard deep links are obvious and removable through existing chips / clear behavior.
* Improve the VPS association lane so it is explicitly about context and links, not invented list-level linked health.
* Preserve existing URL-state behavior for support-surface buttons and filter chips.
* Preserve row click navigation, hover/touch row actions, label edit behavior, pause confirmation and batch actions.
* Maintain mobile usability at 390px: no page-level horizontal overflow; large tables may scroll inside their panel.

## Acceptance Criteria

* [ ] `/nodes` first viewport shows an evidence lead with the current next action and a clear scope summary.
* [ ] The support surface identifies the top node evidence item when a loaded node needs attention.
* [ ] `/nodes?abnormal=1` renders a visible filtered-context state and keeps only abnormal rows visible.
* [ ] `/nodes?onboarding=pending` continues to match lifecycle pending, unbound, and binding-conflict nodes.
* [ ] Support-surface quick actions keep writing URL-state through existing filter functions.
* [ ] No new backend request is added for linked VPS or node detail summaries.
* [ ] Existing node operations still pass tests.
* [ ] Visual evidence captures `/nodes` at `1440x1000` and `390x900` with browser sanity overflow checks.

## Definition of Done

* Tests added/updated for the new evidence lead, filtered context, top node evidence and support-surface actions.
* `cd web && TMPDIR=$PWD/.tmp npm run test -- --run src/pages/NodesPage.test.tsx` passes.
* `cd web && TMPDIR=$PWD/.tmp npm run lint` passes.
* `cd web && TMPDIR=$PWD/.tmp npm run test -- --run` passes.
* `cd web && TMPDIR=$PWD/.tmp npm run build` passes.
* `make verify-web` passes or any environment-only warning is explicitly recorded.
* Visual evidence screenshots and `docs/operations/v2-visual-evidence/manifest.md` are updated.
* PR CI is green before merge; local `main` is synced afterward.
* Trellis task is archived and journaled after work PR merge.

## Out of Scope

* No backend schema, API, handler or migration changes.
* No per-row `GET /api/nodes/:id/vps` fan-out.
* No linked VPS health in the node list.
* No full Targets or Events redesign in this task.
* No new frontend library, charting library, visual regression dependency, or CI screenshot job.
* No large refactor of `NodesPage` state management beyond what is necessary for the evidence workbench.

## Technical Notes

* Primary files likely touched:
  * `web/src/pages/NodesPage.tsx`
  * `web/src/pages/nodes/NodesSupportSurface.tsx`
  * `web/src/pages/nodes/nodeHelpers.ts`
  * `web/src/pages/NodesPage.test.tsx`
  * `web/src/styles/pages.css`
  * `docs/release/ui-evolution-roadmap.md`
  * `docs/operations/v2-visual-evidence/manifest.md`
* Relevant specs:
  * `.trellis/spec/web/index.md`
  * `.trellis/spec/web/component-conventions.md`
  * `.trellis/spec/web/state-and-data.md`
  * `.trellis/spec/web/styling-guidelines.md`
  * `.trellis/spec/web/quality-guidelines.md`
  * `.trellis/spec/guides/index.md`
  * `docs/design/v2-houfeng/design-language.md`
  * `docs/design/v2-houfeng/component-spec.md`
  * `docs/operations/v2-visual-evidence.md`
* Local Vitest should use repo-local temp dir: `cd web && TMPDIR=$PWD/.tmp npm run test -- --run`.
