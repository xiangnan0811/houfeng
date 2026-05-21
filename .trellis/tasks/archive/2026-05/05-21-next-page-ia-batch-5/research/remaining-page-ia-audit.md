# Research: remaining page IA audit after Settings v0.16.0

- **Query**: Research the next best Houfeng frontend page IA batch after SettingsPage shipped as v0.16.0. Inspect current routes/pages/tests and archived Trellis IA tasks under `.trellis/tasks/archive/2026-05/` to identify which pages have already been optimized and which remaining pages still have meaningful IA polish value. Focus on frontend-only IA/copy/layout improvements; exclude backend/API/model/security-sensitive contract changes.
- **Scope**: internal
- **Date**: 2026-05-21

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/app/router.tsx` | Canonical SPA route inventory. Authenticated routes include Dashboard, Asset Ledger pages, observability pages, Events, and Settings; `/login` remains the only public page. |
| `web/src/app/metadata.ts` | Primary navigation grouping: 总览, 资产, 观测, 系统. Current grouping is already explicit and does not suggest a new page IA target. |
| `web/src/pages/LoginPage.tsx` | Auth-gate page. It is compact and behaviorally stable; remaining deltas are only small v2 template/copy consistency items. |
| `web/src/pages/LoginPage.css` | Existing page-local CSS exception for the v2 login aurora/seal/card treatment. |
| `web/src/pages/LoginPage.test.tsx` | Tests preserve generic auth behavior and guard against single-user/full-permission/personal-system phrasing. |
| `web/src/components/atoms/Button.tsx` | Button atom already supports `size="lg"`, so LoginPage can align to the v2 template without atom/API changes. |
| `web/src/pages/DashboardPage.tsx` / `web/src/pages/DashboardPage.test.tsx` | Dashboard is already command-surface aligned and tests guard against older KPI/shortcut-summary anti-patterns. |
| `web/src/pages/EventsPage.tsx` / `web/src/pages/EventsPage.test.tsx` | Events page already implements URL-backed filters, Drawer draft behavior, support surfaces, timeline sections, and regression-sensitive tests. |
| `web/src/pages/AssetDecisionsPage.tsx` / `web/src/pages/AssetDecisionsPage.test.tsx` | Decision queue is freshly optimized and explicitly tested for queue semantics, Drawer isolation, and subscription-evidence truthfulness. |
| `web/src/pages/SettingsPage.tsx`, `web/src/pages/settings/*`, `web/src/pages/SettingsPage.test.tsx` | SettingsPage section hierarchy/ribbon residual gap was the previous batch and has shipped as v0.16.0. Current tests assert hierarchy and secret-safety contracts. |
| `web/src/pages/ProvidersPage.tsx` | Provider support page is already optimized; only tiny technical-ID typography/atom consistency remains. |
| `web/src/pages/SubscriptionsPage.tsx` | Subscription support page is already optimized; only tiny technical-ID typography/atom consistency remains. |
| `web/src/pages/NodeComparePage.tsx` | Node compare page is recently polished and has no active page-specific template gap. |
| `web/src/pages/NodeOnboardingPage.tsx` | Security-sensitive onboarding route is recently optimized; center-generated install command and token secrecy contracts must remain frozen. |
| `docs/design/v2-houfeng/component-spec.md` | Active v2 page/component template reference. LoginPage template calls for full-screen login, seal/aurora, brand/motto, username/password form, `Button(primary lg)`, and `role="alert"` errors. |
| `docs/design/v2-houfeng/design-language.md` | Active visual language authority: dark-first, dense engineering UI, no speculative backend/API/data-shape changes for visual polish. |
| `.trellis/spec/web/component-conventions.md` | Page/component layering, PageState, Drawer reset, and interaction semantics. |
| `.trellis/spec/web/styling-guidelines.md` | Pure CSS, tokens, BEM, and no new page-local CSS except the historical LoginPage exception. |
| `.trellis/spec/web/state-and-data.md` | API, route/query state, Asset Ledger, onboarding, and notification secret boundaries. |
| `.trellis/spec/web/quality-guidelines.md` | Frontend lint/test/build expectations. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-4/prd.md` | Previous batch PRD: SettingsPage section hierarchy IA micro-polish, shipped after the residual gap identified in earlier audits. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-4/research/remaining-page-ia-audit.md` | Pre-v0.16.0 audit that selected SettingsPage only because it still had a concrete v2 Settings template residual gap. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-4/research/design-spec-candidate-audit.md` | Pre-v0.16.0 design/spec audit that documented SettingsPage section hierarchy/ribbon as the last meaningful residual spec gap. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-3/research/design-spec-candidate-audit.md` | Earlier audit that identified LoginPage as the only safe residual under-aligned page if another IA batch was mandatory after stronger candidates were exhausted. |

### Current Routed Pages Inventory

Source: `web/src/app/router.tsx`.

| Route | Page / Behavior | Audit conclusion |
|---|---|---|
| `/login` | `LoginPage` | Only safe isolated residual candidate, but value is tiny. |
| `/` | `DashboardPage` | Already command-surface aligned; defer speculative IA. |
| `/vps` | `VPSPage` | Recently optimized inventory page; do not repeat. |
| `/vps/:vpsId` | `VPSDetailPage` | Recently optimized detail page; do not repeat. |
| `/providers` | `ProvidersPage` | Recently optimized support page; only atom-level residue. |
| `/subscriptions` | `SubscriptionsPage` | Recently optimized support page; only atom-level residue. |
| `/asset-decisions` | `AssetDecisionsPage` | Freshly optimized as v0.15.0; do not repeat. |
| `/nodes` | `NodesPage` | Recently optimized list controls; do not repeat. |
| `/nodes/compare` | `NodeComparePage` | Recently optimized; no active page-specific template gap. |
| `/nodes/:nodeId` | `NodeDetailPage` | Recently optimized watchtower/detail page; do not repeat. |
| `/nodes/:nodeId/onboarding` | `NodeOnboardingPage` | Recently optimized and security-sensitive; do not touch without concrete defect. |
| `/targets` | `TargetsPage` | Recently optimized list controls; do not repeat. |
| `/targets/:targetId` | `TargetDetailPage` | Recently optimized detail page; do not repeat. |
| `/events` | `EventsPage` | Already diagnostic-timeline aligned and URL-state heavy; defer. |
| `/settings` | `SettingsPage` | Previous batch completed the remaining v2 section hierarchy/ribbon gap as v0.16.0. |
| `*` | `<Navigate to="/" replace />` | Authenticated fallback; no IA target. |

Primary navigation grouping from `web/src/app/metadata.ts`:

| Nav Group | Routes |
|---|---|
| 总览 | `/` |
| 资产 | `/asset-decisions`, `/vps`, `/providers`, `/subscriptions` |
| 观测 | `/nodes`, `/targets`, `/events` |
| 系统 | `/settings` |

### Completed / Recently Optimized IA Batches

| Page / Area | Archive evidence | Current conclusion |
|---|---|---|
| `NodeDetailPage` | `.trellis/tasks/archive/2026-05/05-19-optimize-node-detail-information-architecture/prd.md` | Completed; avoid repeat broad IA. |
| `VPSDetailPage` | `.trellis/tasks/archive/2026-05/05-19-optimize-vps-detail-information-architecture/prd.md` | Completed; avoid repeat broad IA. |
| `TargetsPage` + `NodesPage` list controls | `.trellis/tasks/archive/2026-05/05-20-continue-next-page-information-architecture-optimization/prd.md` | Completed; URL/filter/list semantics are regression-sensitive. |
| `NodeComparePage` | `.trellis/tasks/archive/2026-05/05-20-continue-remaining-page-information-architecture-optimization/prd.md` | Completed; no concrete spec gap remains. |
| `VPSPage` inventory | `.trellis/tasks/archive/2026-05/05-20-vps-page-inventory-ia-polish/prd.md` | Completed; preserve inventory/subscription evidence semantics. |
| `NodeOnboardingPage` | `.trellis/tasks/archive/2026-05/05-20-next-page-ia-batch/prd.md` | Completed; security-sensitive installer/token contracts are frozen. |
| `TargetDetailPage` | `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch/prd.md` | Completed; avoid repeat broad IA. |
| `ProvidersPage` + `SubscriptionsPage` | `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-2/prd.md` | Completed; only tiny atom/ID presentation residue remains. |
| `AssetDecisionsPage` | `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-3/prd.md` | Completed as v0.15.0; do not reopen without concrete defect. |
| `SettingsPage` | `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-4/prd.md` | Completed as v0.16.0; previous section hierarchy/ribbon residual gap is now addressed. |
| `DashboardPage` | Earlier command-surface work including `.trellis/tasks/archive/2026-05/05-11-ux-dashboard-command-surface/prd.md` and `.trellis/tasks/archive/2026-05/05-13-ux3-dashboard-command-surface-polish/prd.md` | Already strongly aligned; regression surface is larger than speculative polish value. |
| `EventsPage` | Earlier timeline/filter work including `.trellis/tasks/archive/2026-05/05-10-events-filter-drawer/prd.md` and `.trellis/tasks/archive/2026-05/05-13-ux6c-events-timeline-evidence/prd.md` | Already diagnostic-timeline aligned; URL-state behavior makes speculative churn risky. |

### Code Patterns

#### Route and navigation topology

- Routing remains centralized in `web/src/app/router.tsx`; all page IA candidates are visible from the route inventory above.
- Navigation is already grouped by operational area in `web/src/app/metadata.ts`; no route or nav-group change is needed for a frontend-only IA polish batch.

#### LoginPage residuals

- `LoginPage` already preserves the core auth behavior: `useAuth().login(username, password)`, `next` redirect, generic error message, and `role="alert"` error rendering.
- `LoginPage.test.tsx` already guards against copy that would misrepresent the product as a single-user/full-permission/personal system.
- v2 LoginPage template in `docs/design/v2-houfeng/component-spec.md` calls for `Button(primary lg)`. Current `LoginPage.tsx` uses `<Button type="submit" disabled={submitting} variant="primary">` without `size="lg"`; `web/src/components/atoms/Button.tsx` already supports `size="lg"`, so the change would be local and low risk.
- v2 LoginPage template calls for brand small text `HOUFENG`. Current implementation uses `FLEET CONTROL PLANE`; this is not harmful, but is a small template-consistency seam.
- Current login footer hardcodes `v1.0`; after release flow has reached v0.16.0, this is the clearest truthfulness/copy seam on the remaining page set. A safe pass should avoid adding dynamic version plumbing unless an existing frontend version source already exists.

#### Pages to avoid reopening

- `SettingsPage` was the previous selected candidate because it had a concrete v2 Settings template residual gap around section hierarchy/ribbons. The archived v0.16.0 PRD confirms that gap was the scope and should not be reopened as a broad second Settings rewrite.
- `NodeOnboardingPage` includes center-generated install-command and enrollment-token flows; it must not be touched for speculative IA because install-command origin, one-time token, token display, and manual fallback language are security-sensitive.
- `EventsPage` has URL-backed filter state and Drawer draft semantics. Existing tests cover URL filters, `include_backfilled`, time ranges, Drawer apply/close/Escape/discard, reset, and load-more behavior; speculative IA churn has high regression risk.
- `DashboardPage` tests explicitly guard against old global-KPI and shortcut-summary anti-patterns, so broad dashboard IA should wait for concrete real-use findings.
- Asset Ledger support pages (`ProvidersPage`, `SubscriptionsPage`, `AssetDecisionsPage`, `VPSPage`) are recently polished and have data-contract/truthfulness boundaries around subscription evidence, linked-node signals, and decision semantics.

### Remaining Candidate Ranking

| Rank | Candidate | Value | Risk | Rationale | Recommended handling |
|---:|---|---|---|---|---|
| 1 | `LoginPage` auth-card visual/truthfulness consistency pass | Very low to low | Very low | It is the only isolated routed page that has not been recently selected as a batch and still has small concrete v2-template/copy deltas: `primary lg` button, brand small text, and hardcoded footer version. | Choose only if another frontend page IA batch is mandatory. Keep scope tiny and behavior-frozen. |
| 2 | Stop speculative page IA batches | High product honesty | Lowest implementation risk | After SettingsPage v0.16.0, no broad high-value/low-risk page IA target remains. User memory also says current priority has moved from UI polish toward agent/function delivery. | Preferred product-direction recommendation if the task does not require selecting another page. |
| 3 | `ProvidersPage` + `SubscriptionsPage` technical-ID atom consistency | Tiny | Low | Some technical IDs are still plain spans instead of mono/technical presentation, but this is atom-level consistency rather than IA. | Not worth a dedicated IA batch; consider only as incidental cleanup in a relevant future task. |
| 4 | `DashboardPage` | Potentially important, but currently saturated | High | Already command-surface aligned and guarded by tests against prior anti-patterns. | Defer until concrete dashboard defects or real-use findings. |
| 5 | `EventsPage` | Potentially important, but currently saturated | High | Already diagnostic-timeline aligned; filter/query semantics are regression-sensitive. | Defer until concrete timeline/filter defects. |
| 6 | `SettingsPage` | Recently completed | Medium/high due security and settings semantics | v2 Settings residual gap was just completed as v0.16.0. | Do not reopen without concrete defect. |
| 7 | Recent detail/list/onboarding/asset-decision pages | Low | Varies, often high | Explicitly completed in archived IA tasks. | Out of scope for this batch. |

### Recommended Next Page / Scope

**Recommendation if a page must be selected: `LoginPage`, limited to a tiny auth-card visual/truthfulness consistency pass.**

Safe scope:

- Keep `/login` route, public auth-gate behavior, username/password fields, `useAuth().login(...)`, `next` redirect, generic auth error, and `role="alert"` error rendering unchanged.
- Align the submit button with the v2 template by using existing `Button` atom support for primary large size.
- Consider aligning the brand subline to the active v2 template (`HOUFENG`) while preserving the Chinese product identity and motto.
- Remove or reframe the hardcoded `v1.0` footer so the login page does not imply an obsolete version after v0.16.0; do not add backend/API/version plumbing as part of this IA pass.
- Keep styling inside existing `LoginPage.css`; do not introduce a new page-local CSS file or new styling system.
- Update `LoginPage.test.tsx` only for visible copy/structure and to preserve existing behavior/copy guards.

**Preferred product-honest alternative: stop speculative page IA batches until concrete real-use findings appear.** The previous SettingsPage batch exhausted the last meaningful page-specific v2 spec gap; broad IA churn now has low expected value relative to agent/function delivery and real testing feedback.

### Frozen Contracts / Risks

For a tiny `LoginPage` pass:

| Contract / Risk | Must Preserve |
|---|---|
| Frontend-only boundary | Limit changes to `web/src/pages/LoginPage.tsx`, `web/src/pages/LoginPage.css`, and `web/src/pages/LoginPage.test.tsx` unless an existing shared test helper requires adjustment. |
| Auth behavior | Keep `useAuth().login(username, password)`, submit flow, disabled submitting state, and generic error handling unchanged. |
| Redirect behavior | Keep `next` query redirect fallback to `/` after successful login. |
| Security copy | Do not introduce single-user/full-permission/personal-system phrasing; preserve the existing test guard. |
| API/session contracts | Do not change auth API helpers, cookies/session handling, route guards, or backend handlers. |
| Styling | Use existing `LoginPage.css` and design tokens/BEM; do not add Tailwind, CSS-in-JS, new dependencies, or broad responsive work. |
| Version truthfulness | Do not hardcode a newer release value; either avoid version claims or use an existing frontend source if one already exists. |
| Tests | Preserve credential submission and bad-credential alert coverage; add only visible IA/copy/structure assertions. |

### External References

None. This was an internal code/spec/archive audit only.

### Related Specs

- `docs/design/v2-houfeng/design-language.md` — active visual language and hard boundaries for visual-only work.
- `docs/design/v2-houfeng/component-spec.md` — active page/component templates; LoginPage is the only remaining small isolated template-consistency candidate.
- `.trellis/spec/web/component-conventions.md` — component/page layering, PageState, Drawer reset, and interactive semantics.
- `.trellis/spec/web/styling-guidelines.md` — pure CSS, design tokens, BEM, and LoginPage as the only page-local CSS exception.
- `.trellis/spec/web/state-and-data.md` — API/client/data-state and security-sensitive secret/onboarding boundaries.
- `.trellis/spec/web/quality-guidelines.md` — lint/test/build expectations for frontend changes.

## Caveats / Not Found

- Remaining IA value is low. SettingsPage v0.16.0 closed the last concrete page-specific v2 Settings template gap identified by previous audits.
- `LoginPage` is recommended only because the task asks for the next page candidate; it is not a broad or high-impact IA batch.
- The strongest recommendation from a product-direction perspective is to stop speculative page IA polishing and move to concrete real-use findings or agent/function delivery.
- This audit did not run browser visual evidence; it is based on route/page/test source, active specs, and archived Trellis task/research inspection.
- No external search was required.
