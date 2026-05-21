# SubscriptionsPage evidence path IA micro-polish

## Goal

Optimize `SubscriptionsPage` as the next highest-value remaining Houfeng frontend page IA batch after SettingsPage shipped as v0.16.0: collapse the current same-weight pre-table panels into a clearer subscription evidence command path, preserve URL/filter/create/edit contracts, and keep the page truthful about renewal/cost evidence boundaries.

## Requirements

* Treat `SubscriptionsPage` as an Asset Ledger evidence-support route, not a generic CRUD table.
* Preserve the page's existing data and mutation behavior:
  * `listSubscriptions`, `createSubscription`, `updateSubscription`, and `listVPSAssets` remain the API helpers.
  * URL-state remains `vps_id`, `status`, `renew_within_days`, and `create=1`.
  * `renew_within_days` continues to map to `sort=renew_at&order=asc`.
  * `/subscriptions?vps_id=<id>&create=1` still opens the create Drawer, preselects the VPS, and closing the Drawer removes only `create=1` while preserving `vps_id`.
  * create/update payloads must not send backend-computed `monthly_price`.
  * VPS binding stays selector-based; do not ask operators to hand-enter internal IDs.
  * create/edit Drawer cancel/close still resets draft and validation errors.
* Reframe the default scan path into a single higher-level `subscriptions-evidence-workbench` surface that includes:
  * filtered evidence summary,
  * active filter/URL truth context,
  * optional VPS context action when `vps_id` is present,
  * optional prerequisite action when no VPS assets exist.
* Keep the actual `订阅续费证据表` as the primary data table immediately after the workbench.
* Preserve error/empty truthfulness: failed subscription reads are unavailable evidence, not factual missing-subscription data.
* Use existing atoms/components/CSS tokens only; keep styles in `web/src/styles/pages.css` if needed.

## Acceptance Criteria

* [ ] `SubscriptionsPage` has one clear evidence workbench before the table instead of multiple same-weight pre-table panels.
* [ ] Workbench copy explains filtered-count scope, URL truth, renewal-window sorting, backend-computed monthly price, and Drawer-only create/edit boundary.
* [ ] VPS-scoped context remains visible and actionable when `vps_id` is present.
* [ ] No-VPS prerequisite remains visible and actionable when `listVPSAssets()` returns empty.
* [ ] Existing URL, API, payload, Drawer-reset, and error-boundary tests continue to pass.
* [ ] Focused `SubscriptionsPage` tests assert the new IA copy/structure.
* [ ] Web lint/test/build and full repo verification pass, or local browser tooling limitations are recorded truthfully.
* [ ] Trellis task is archived, PR/release follow-through completes, and final local/task state is clean.

## Definition of Done

* Focused tests updated and passing.
* `npm --prefix web run lint` passes.
* `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run` passes.
* `npm --prefix web run build` passes.
* `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh` passes.
* Browser sanity is attempted per `docs/operations/v2-visual-evidence.md`; if local Playwright tooling is unavailable, record the limitation under `research/browser-sanity.md` instead of adding dependencies.
* PR/release/main CI/publish-images follow-through completes per user memory.

## Technical Approach

* Keep implementation scoped to `web/src/pages/SubscriptionsPage.tsx`, `web/src/pages/SubscriptionsPage.test.tsx`, and possibly `web/src/styles/pages.css`.
* Add small derived view-model/context arrays only if they make the workbench copy less repetitive.
* Combine the summary and filter-context presentation into one high-density surface while keeping the `FilterBar` and chips visible.
* Use existing `PageStateView` for loading/error/empty states and keep DataTable untouched except for surrounding framing/copy.
* Avoid broad shared abstractions; this is a page-specific IA pass.

## Decision (ADR-lite)

**Context**: After SettingsPage v0.16.0, research found little broad IA value remains. One audit recommends `LoginPage` only as a tiny low-value residual; the design/spec audit identifies `SubscriptionsPage` as the more valuable remaining primary-nav Asset route because it has important state/data contracts but no explicit v2 page template and still shows several same-weight pre-table panels before the evidence table.

**Decision**: Select `SubscriptionsPage` for a narrow, frontend-only evidence path IA micro-polish. Do not reopen ProvidersPage or recently shipped pages in this batch.

**Consequences**: The batch improves a real Asset Ledger primary route without touching backend/API/security contracts. The scope is intentionally smaller than earlier page batches because most high-value IA surfaces are already shipped; speculative Dashboard/Events/LoginPage churn remains out of scope until concrete defects appear.

## Out of Scope

* Backend, database migration, API request/response shape, or Asset Ledger model changes.
* Changing `monthly_price` semantics or computing/sending it from the frontend.
* New subscription detail routes, provider joins, linked-node health, real inventory claims, or new cross-page data facts.
* Changing auth/session/install/token/notification contracts.
* New dependencies, CSS frameworks, CSS-in-JS, chart libraries, page-local CSS, or new navigation groups.
* Reworking `ProvidersPage`, `AssetDecisionsPage`, `VPSPage`, `SettingsPage`, `DashboardPage`, `EventsPage`, `LoginPage`, or onboarding/security-sensitive pages.

## Research References

* [`research/remaining-page-ia-audit.md`](research/remaining-page-ia-audit.md) — Finds little broad IA value remains and recommends LoginPage only as a tiny fallback candidate.
* [`research/design-spec-candidate-audit.md`](research/design-spec-candidate-audit.md) — Recommends SubscriptionsPage as the highest-value remaining primary-nav Asset route with a page hierarchy mismatch.

## Technical Notes

* Current task: `.trellis/tasks/05-21-next-page-ia-batch-5`.
* Feature branch: `feature/next-page-ia-batch-5-20260521`.
* Candidate decision: choose SubscriptionsPage over LoginPage because the user asked to continue page IA batches and SubscriptionsPage has greater primary-route IA value; keep scope narrow to respect the remaining low-value landscape.
* Relevant specs: `.trellis/spec/web/component-conventions.md`, `.trellis/spec/web/styling-guidelines.md`, `.trellis/spec/web/state-and-data.md`, `.trellis/spec/web/quality-guidelines.md`, `docs/design/v2-houfeng/design-language.md`, `docs/design/v2-houfeng/component-spec.md`.
