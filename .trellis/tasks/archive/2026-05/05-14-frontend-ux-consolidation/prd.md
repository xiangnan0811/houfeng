# Frontend UX Consolidation

## Goal

Rework the Houfeng frontend experience so the nearly-complete product becomes usable for real VPS data validation. The focus is not to add backend capabilities or replace the design system, but to make the existing asset ledger, renewal decision, dashboard, observation, and settings capabilities feel like a coherent high-density engineering workbench.

## What I already know

* The user agrees with the proposed direction and asked to proceed.
* External UX review in `.tmp/houfeng_frontend_ux_review.md` is intentionally temporary and must not be committed.
* The project’s active product direction is `候风 = 资产决策工作台 + 观测证据系统`, documented in `docs/release/core-pages-product-ux-replan.md`.
* v2 visual authority is `docs/design/v2-houfeng/design-language.md` and `docs/design/v2-houfeng/component-spec.md`.
* Current frontend already uses React, Vite, TypeScript, self-hosted CSS tokens and atoms; do not introduce Tailwind, CSS-in-JS, chart libraries, or large UI frameworks.
* The current AppShell already calls `getDashboard()` and derives severe/abnormal counts, but only uses them for Sidebar/SyncStatus, not for a global critical banner.
* Current GlobalSearch only searches Node and Target.
* Current navigation is grouped, but labels still use `资产决策` and `目标`; UI wording should better express renewal decision and entry-point observation while keeping internal model names stable.
* Current VPS page has quick views and high-density DataTable, but VPS creation still expands as a large inline form in the main page.
* Current Node watchtower metric thresholds use `DEFAULT_THRESHOLDS`, not effective runtime settings.
* Current Settings incident defaults are rendered as multiple threshold input groups, not rule cards.

## Requirements (evolving)

* Preserve existing backend API semantics unless a later child task explicitly plans a small backend addition.
* Preserve existing self-built atoms/tokens/DataTable/Drawer/Tabs/MetricChart system.
* Make serious fleet/observation problems globally visible from any page.
* Expand search so real VPS validation can find assets and key context, not only Node/Target.
* Align shell/navigation wording and visual hierarchy with the current UX replan and v2 design language.
* Convert high-friction page-embedded forms into Drawer-based workflows where this directly improves the primary scan path.
* Keep dashboard focused on “what should I handle now?” rather than a KPI wall or API-field display.

## Proposed First Batch

1. App shell / navigation / visual baseline
   * Add `GlobalCriticalAlert` in AppShell using already-loaded dashboard summary.
   * Add skip link and `main#main-content`.
   * Refine navigation labels where safe: `资产决策` → renewal-oriented wording; `目标` → entry-point observation wording in UI.
   * Add compatible token aliases for surface/border/text hierarchy without renaming existing variables.

2. CommandSearch v1
   * Expand GlobalSearch to cover VPS, Node, Target, Provider, and Subscription.
   * Group results by type.
   * Use link semantics for results so browser navigation behaviors are preserved.
   * Keep Service, Domain, and Event search out of v1 unless the user chooses a broader batch.

3. Dashboard command surface
   * Rework Dashboard around asset decision queue, observation anomaly queue, and next actions.
   * Preserve deep links into VPS, asset decisions, nodes, targets, and events.
   * Do not display every dashboard API field as peer sections.

4. VPS inventory scan path
   * Move VPS creation from inline main-page form to Drawer.
   * Keep high-density inventory table as the primary VPS list mode.
   * Strengthen inventory evidence/quality states without inventing new backend health semantics.

## Acceptance Criteria (evolving)

* [ ] A user can see serious/severe problems from any routed page when dashboard summary reports severe node/target counts.
* [ ] Search can find at least VPS, Node, Target, Provider, and Subscription records with grouped results.
* [ ] Search result activation preserves normal link behavior where practical.
* [ ] App shell includes a skip link and a main content anchor.
* [ ] Navigation and page chrome better reflect 工作台 / 资产 / 观测 / 系统 product structure.
* [ ] Dashboard first screen prioritizes current work and next actions, not a KPI wall.
* [ ] VPS page no longer exposes a long create form in the main scan path.
* [ ] Existing tests are updated or added for changed UI behavior.
* [ ] `cd web && npm run lint`, `npm run test`, and `npm run build` pass.
* [ ] Dev server is run and key pages are manually checked in browser before completion.

## Definition of Done

* Tests added/updated where behavior changes.
* Web lint/test/build pass.
* UI is manually checked in browser via dev server.
* No large UI framework, CSS framework, or charting dependency is introduced.
* `.tmp/houfeng_frontend_ux_review.md` remains uncommitted.
* Trellis check is run before finishing implementation.

## Out of Scope (explicit)

* Real 40+ VPS data import or dry-run.
* Provider/DNS synchronization.
* Web SSH.
* Full service registry or full domain/DNS management.
* Multi-user RBAC.
* Large UI rewrite or design-system replacement.
* Playwright screenshot infrastructure as a repo dependency.
* Full Event/Service/Domain search in CommandSearch v1 unless explicitly pulled into this batch.
* VPS card/decision alternate views unless explicitly pulled into this batch.
* Settings rule-center redesign unless explicitly pulled into this batch.
* Node/Target shared WatchtowerDetailLayout unless explicitly pulled into this batch.

## Decision (ADR-lite)

**Context**: The frontend needs enough UX consolidation to make real VPS data validation attractive, but this work touches many visible routes and should avoid a single high-risk mega-redesign.

**Decision**: Use a restrained first batch: AppShell/global critical alert, navigation/visual baseline, CommandSearch v1 for VPS/Node/Target/Provider/Subscription, Dashboard command surface, and VPS create Drawer + inventory scan-path cleanup.

**Consequences**: Events object aggregation, Settings rule cards, VPS Detail tabs, and Node/Target shared layout remain follow-up tasks. This keeps the first implementation focused on the highest-leverage entry points and lowers regression risk.

## Open Questions

* None for the first implementation batch.

## Implementation Plan

1. Prepare implementation context
   * Load Trellis before-dev guidance for web/frontend scope.
   * Curate implementation/check context files for the task.

2. App shell and visual baseline
   * Add a global critical/abnormal alert banner using AppShell dashboard summary.
   * Add skip link and main content anchor.
   * Adjust navigation labels and compatible token aliases without renaming existing variables.

3. CommandSearch v1
   * Expand search data sources to VPS, Node, Target, Provider, and Subscription.
   * Group result rendering by type.
   * Prefer link semantics for result activation while preserving keyboard selection.

4. Dashboard command surface
   * Rework first-screen structure around asset decision queue, observation anomaly queue, and next actions.
   * Keep existing deep links and avoid turning the dashboard into a full API field display.

5. VPS inventory scan path
   * Move VPS creation into Drawer.
   * Keep the high-density DataTable as the primary mode.
   * Preserve URL-state filters and existing inventory evidence behavior.

6. Verification
   * Update tests for changed behavior.
   * Run web lint, tests, build.
   * Start the dev server and manually inspect key routes.
   * Run Trellis check before finishing.

## Technical Notes

* `web/src/app/layout/AppShell.tsx` already loads dashboard summary and derives severe/abnormal counts.
* `web/src/app/layout/GlobalSearch.tsx` currently imports only `listNodes` and `listTargets` and renders results as buttons with imperative navigation.
* `web/src/app/metadata.ts` owns primary navigation labels/groups.
* `web/src/pages/VPSPage.tsx` has quick views and DataTable, but also contains the inline create form.
* `web/src/components/node-detail/NodeWatchtowerMetrics.tsx` currently imports `DEFAULT_THRESHOLDS`.
* `web/src/pages/settings/IncidentDefaultsSection.tsx` renders threshold groups directly.
* Active design constraints: `docs/design/v2-houfeng/design-language.md`, `docs/design/v2-houfeng/component-spec.md`.
* Active product plan: `docs/release/core-pages-product-ux-replan.md`.
