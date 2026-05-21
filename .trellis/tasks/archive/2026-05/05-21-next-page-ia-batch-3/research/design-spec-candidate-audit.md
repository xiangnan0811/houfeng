# Research: design/spec candidate audit

- **Query**: Research design/spec fit for the next Houfeng frontend page IA batch after ProvidersPage + SubscriptionsPage. Use v2 Houfeng design docs, web specs, active page source/tests, and recent IA archives to identify which remaining page/page group is under-aligned with v2 visual/component language and still safe to polish without API/data/security changes.
- **Scope**: internal
- **Date**: 2026-05-21

## Findings

### Files Found

| File Path | Description |
|---|---|
| `docs/design/v2-houfeng/design-language.md` | Active visual authority for dark-first, restrained, high-density Houfeng v2 UI; includes page hierarchy, state handling, and no-backend-change boundaries. |
| `docs/design/v2-houfeng/component-spec.md` | Active component/page visual contracts; explicit templates exist for Dashboard, AssetDecisions, VPS/VPSDetail, Nodes/NodeDetail, Events, Settings, Targets/TargetDetail, NodeOnboarding, and Login. |
| `.trellis/spec/web/index.md` | Web spec index and authority chain: CLAUDE.md + v1 business baseline + v2 visual docs. |
| `.trellis/spec/web/component-conventions.md` | Page/component layering, PageState, Drawer reset/focus, DataTable row guards, association selector, and anti-pattern contracts. |
| `.trellis/spec/web/styling-guidelines.md` | Styling constraints: token/BEM/global CSS, no new page-local CSS except existing LoginPage exception, no CSS frameworks, PageState styles. |
| `.trellis/spec/web/state-and-data.md` | API/data contracts for Dashboard, Asset Ledger pages, Events filters, Node onboarding install command, and no invented facts. |
| `.trellis/spec/web/quality-guidelines.md` | Verification and test conventions for page changes. |
| `.trellis/tasks/05-21-next-page-ia-batch-3/prd.md` | Current task context: recent IA work includes NodeDetail, VPSDetail, Settings, Targets+Nodes list controls, NodeCompare, VPSPage inventory, NodeOnboarding, TargetDetailPage, ProvidersPage, and SubscriptionsPage. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch/prd.md` | TargetDetailPage IA polish archive; confirms Target detail has already been selected and completed in the previous batch. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-2/prd.md` | ProvidersPage + SubscriptionsPage IA polish archive; confirms the immediately previous batch scope and frozen contracts. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-2/research/design-spec-candidate-audit.md` | Prior candidate audit after TargetDetailPage; recommended Providers + Subscriptions, now completed. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-2/research/remaining-page-ia-audit.md` | Prior remaining-pages audit; useful as the stale-before-current-batch baseline. |
| `.trellis/tasks/archive/2026-05/05-20-continue-remaining-page-information-architecture-optimization/research/remaining-pages-ia-audit.md` | Earlier broad remaining IA audit; ranked NodeCompare and Settings before later work completed those pages. |
| `.trellis/tasks/archive/2026-05/05-20-continue-page-information-architecture-optimization/research/next-candidate-pages-audit.md` | Earlier next-candidate audit; useful for prior TargetDetail/NodeCompare/Nodes/Targets ranking and test-risk notes. |
| `.trellis/tasks/archive/2026-05/05-11-ux-asset-decision-vps-list/prd.md` | Asset Decisions + VPS inventory redesign archive; explains why current AssetDecisionsPage already has a unified decision queue and Drawer work panel. |
| `web/src/app/router.tsx` | Route registry for all current page candidates. |
| `web/src/pages/DashboardPage.tsx` and `web/src/pages/dashboard/*` | Current Dashboard command-surface/workbench implementation. |
| `web/src/pages/DashboardPage.test.tsx` | Regression tests proving Dashboard remains asset-decision-first and avoids old KPI/group/recent-event expansion patterns. |
| `web/src/pages/EventsPage.tsx` and `web/src/pages/events/*` | Current Events diagnostic timeline, support surface, applied/draft filter Drawer, and event stream implementation. |
| `web/src/pages/EventsPage.test.tsx` | Dense Events regression tests for URL filters, drawer draft/apply behavior, backfilled events, links, empty states, and load more. |
| `web/src/pages/AssetDecisionsPage.tsx` | Current unified asset decision queue, priority rows, tabs, renewal evidence section, and Drawer work panel. |
| `web/src/pages/AssetDecisionsPage.test.tsx` | Tests for unified queue, renewal-window reload, update/movement, row/action isolation, and empty states. |
| `web/src/pages/ProvidersPage.tsx` | Already-polished Asset Ledger provider support page from the immediately previous batch. |
| `web/src/pages/SubscriptionsPage.tsx` | Already-polished Asset Ledger subscription evidence page from the immediately previous batch. |
| `web/src/pages/NodeOnboardingPage.tsx` | Recently-polished security-sensitive onboarding page; current structure already elevates binding conflict, center-generated command, Stepper evidence, and low-weight manual fallback. |
| `web/src/pages/LoginPage.tsx` and `web/src/pages/LoginPage.css` | Small remaining public login route; the only page-local CSS exception and the safest residual visual/IA polish candidate. |
| `web/src/pages/LoginPage.test.tsx` | Small login test suite for no single-user phrasing, credential submit, and error alert. |

### Design / Spec Constraints

- **Visual authority** is only the v2 Houfeng docs plus current web specs. The web index states visual authority is `docs/design/v2-houfeng/design-language.md` and `docs/design/v2-houfeng/component-spec.md` (`.trellis/spec/web/index.md:1-4`).
- **Page hierarchy** should stay dense and ordered by operational relevance: page identity, current problem / highest-priority work, context/trend, history/events, then danger/sensitive actions (`docs/design/v2-houfeng/design-language.md:147-170`).
- **Loading/error/empty** should use consistent v2 PageState behavior: no spinner/skeleton, Chinese explanation, technical summary for errors, retry where applicable (`docs/design/v2-houfeng/design-language.md:232-249`; `.trellis/spec/web/component-conventions.md:44-50`).
- **Frontend-only IA polish must not change backend/API/data shape**. v2 explicitly disallows backend/API/data-shape changes, new CSS frameworks, chart libraries, i18n, broad responsive work, or new visual regression infrastructure for this class of polish (`docs/design/v2-houfeng/design-language.md:312-325`).
- **Styling touch points are constrained**. Page styles normally belong in `web/src/styles/pages.css` or atom styles, with `LoginPage.css` documented as the only page-local CSS exception (`.trellis/spec/web/styling-guidelines.md:13-16`, `:95-111`).
- **No direct fetch from pages/components**. Business API calls must remain in `web/src/lib/api.ts`; frontend types mirror backend JSON snake_case (`.trellis/spec/web/state-and-data.md:20-37`).
- **Dashboard is contract-sensitive**. It can show only `/api/dashboard` facts that support the command/workbench decision path, not every returned field or inferred health/sync state (`.trellis/spec/web/state-and-data.md:102-145`).
- **Asset Ledger list/decision pages are evidence-boundary-sensitive**. Client-side VPS/Subscription joins are allowed, but list records must not invent linked-node health or missing-subscription truth when evidence failed (`.trellis/spec/web/state-and-data.md:147-203`).
- **Events is URL-state-sensitive**. Applied URL filters are request truth, Drawer changes are draft-only until apply/reset, and `include_backfilled=1` maps to API `include_backfilled=true` (`.trellis/spec/web/state-and-data.md:469-514`).
- **Node onboarding is security-sensitive**. Production install commands must be center-generated; browser code must not synthesize commands from `window.location.origin`; full tokens must appear only in deliberate reveal/copy surfaces (`.trellis/spec/web/state-and-data.md:38-80`).
- **LoginPage has an explicit compact template**: full-screen centered, seal/aurora, brand card, username/password form, primary large login button, and inline alert (`docs/design/v2-houfeng/component-spec.md:339-345`).

### Recent IA History / Already-Completed Scope

| Recent task / area | Evidence | Current implication |
|---|---|---|
| TargetDetailPage | `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch/prd.md:3-15`, `:50-57` | Completed; no longer a candidate for this batch. |
| ProvidersPage + SubscriptionsPage | `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-2/prd.md:3-13`, `:15-28`, `:70-76` | Immediately previous batch; current task is explicitly after this work. |
| NodeDetail, VPSDetail, Settings, Targets+Nodes lists, NodeCompare, VPSPage inventory, NodeOnboarding, TargetDetail, Providers, Subscriptions | Current PRD records these as recently completed (`.trellis/tasks/05-21-next-page-ia-batch-3/prd.md:9-12`). | Treat as out of broad IA scope unless a concrete bug/security issue appears. |
| Asset Decisions + VPS inventory redesign | `.trellis/tasks/archive/2026-05/05-11-ux-asset-decision-vps-list/prd.md:18-33`, `:35-44` | Explains why AssetDecisionsPage already has a unified work queue and Drawer decision panel despite not being listed in the latest small-batch sequence. |

### Code Patterns / Current Page Alignment

#### DashboardPage — already aligned, high contract risk for broad changes

- Current route renders only `DashboardCommandSurface` and `DashboardWorkbench` after loading/error (`web/src/pages/DashboardPage.tsx:59-72`, `:96-120`).
- This matches the component-spec Dashboard template: command surface first, three lanes, and one workbench instead of same-weight KPI/Group/recent-event sections (`docs/design/v2-houfeng/component-spec.md:202-219`).
- Tests assert command-surface behavior and negative guardrails against old dashboard patterns, including asset decision links and summary semantics (`web/src/pages/DashboardPage.test.tsx:86-220`).
- Candidate implication: high-value page, but not meaningfully under-aligned for this batch and sensitive to dashboard contract drift.

#### EventsPage — already aligned, dense URL/filter test surface

- Current route keeps applied filters derived from URL, Drawer draft state separate, canonicalizes invalid params, fetches through `listEvents`, then renders hero, support surface, filter overview, filter Drawer, and event stream (`web/src/pages/EventsPage.tsx:195-237`, `:255-279`, `:373-420`).
- This maps closely to the Events component-spec template: hero, diagnostic support surface, filter overview/Drawer, and event stream (`docs/design/v2-houfeng/component-spec.md:282-290`).
- Tests cover valid URL filters, `include_backfilled`, drawer behavior, support surface links, empty filter clearing, and stable focus copy (`web/src/pages/EventsPage.test.tsx:51-168`, `:170-220`).
- Candidate implication: already aligned; broad polish would mostly churn a contract-dense page.

#### AssetDecisionsPage — already aligned with unified decision queue

- Current route starts with asset page identity, then a single `资产决策工作队列` surface with summary metrics, renewal-window select, queue tabs, focus items, priority queue rows, queue notice, PageState loading/error/empty, then secondary `续费候选证据`, and a Drawer work panel (`web/src/pages/AssetDecisionsPage.tsx:536-680`).
- This matches the component-spec AssetDecisionsPage contract: unified work queue, few top-level numbers, ranked rows, Drawer work panel, and secondary renewal evidence (`docs/design/v2-houfeng/component-spec.md:221-227`).
- Tests assert unified queue rendering, renewal-window request shape, decision save/move between queues, empty view action, and row/action isolation (`web/src/pages/AssetDecisionsPage.test.tsx:90-206`, `:208-260`).
- Candidate implication: not a good next broad IA target; only issue-driven tiny polish is justified.

#### ProvidersPage + SubscriptionsPage — just completed; exclude from new scope

- ProvidersPage now frames service providers as Asset Ledger master-data evidence, keeps a high-density table as the scan path, and uses Drawer create/edit with no Node provider-hint mutation claims (`web/src/pages/ProvidersPage.tsx:272-384`, `:386-477`).
- SubscriptionsPage now frames subscriptions as renewal/cost evidence, preserves VPS URL context and selector-based association, shows summary/filter/table surfaces, and uses create/edit Drawer (`web/src/pages/SubscriptionsPage.tsx:444-620`, `:622-759`).
- Candidate implication: do not immediately rework these pages; they are the batch this audit follows.

#### NodeOnboardingPage — recently aligned but security-sensitive

- Current route already follows the safety-oriented IA from prior audits: hero identity, explicit priority card, binding conflict before install when present, center-generated one-command install, Stepper/evidence context, installer behavior, and low-weight manual fallback (`web/src/pages/NodeOnboardingPage.tsx:571-836`).
- Binding conflict copy includes the one-time token consumption caveat and two-step `ActionConfirmationCard` flow (`web/src/pages/NodeOnboardingPage.tsx:625-730`).
- Install command generation calls `issueNodeInstallCommand(nodeId)` and updates only the local install command state (`web/src/pages/NodeOnboardingPage.tsx:533-569`).
- Manual fallback uses placeholders, not generated secrets (`web/src/pages/NodeOnboardingPage.tsx:56-64`, `:803-828`).
- Candidate implication: high operational value but not a safe casual IA batch; only touch for concrete security/usability defects.

#### LoginPage — only remaining safe residual under-aligned page, but low IA leverage

- Component spec expects full-screen centered login, seal/aurora, brand card, username/password, primary large login button, and inline alert (`docs/design/v2-houfeng/component-spec.md:339-345`).
- Current route is small and isolated: it renders the seal, brand card, username/password Inputs, submit Button, inline alert, and redirects to `next` after `useAuth().login` (`web/src/pages/LoginPage.tsx:7-29`, `:31-64`).
- Current CSS is already a v2-style aurora/seal/card treatment and uses the documented page-local CSS exception (`web/src/pages/LoginPage.css:1-122`; `.trellis/spec/web/styling-guidelines.md:13-16`, `:159-160`).
- Minor observed fit gaps versus the explicit template/current truthfulness expectations:
  - Submit button uses the default Button size rather than the spec’s `primary lg` posture (`web/src/pages/LoginPage.tsx:60-62`; `docs/design/v2-houfeng/component-spec.md:339-345`).
  - English brand subline is `FLEET CONTROL PLANE`, while the component spec says `HOUFENG` sans subline (`web/src/pages/LoginPage.tsx:37-41`; `docs/design/v2-houfeng/component-spec.md:339-345`).
  - Footer is hardcoded `v1.0`, and no active spec was found that makes a hardcoded login-page version part of the contract (`web/src/pages/LoginPage.tsx:63`).
- Existing tests are small and focused on auth behavior/no misleading single-user phrasing (`web/src/pages/LoginPage.test.tsx:9-62`).
- Candidate implication: best remaining **safe** page if the team insists on one more IA/visual batch, but the value is visual consistency/truthfulness rather than operational workflow improvement.

## Candidate Fit Analysis

| Candidate | Design/spec gap | User value | Implementation risk | Contract/security risk | Fit after Providers+Subscriptions |
|---|---|---|---|---|---|
| `LoginPage` | Low-Medium: small explicit template mismatches (button size/brand subline/footer truthfulness) | Low-Medium: first unauthenticated touchpoint, but not an operator workbench | Low: isolated route + existing CSS exception + small tests | Low: preserve `useAuth().login`, `next` redirect, alert semantics | **Recommended only as a deliberately small finishing pass**. It is the only remaining safe residual under-aligned page. |
| `AssetDecisionsPage` | Low: already matches unified queue spec | High operational value | Medium: queue/update behavior and subscription evidence tests | Medium: asset evidence boundary must not drift | Defer. Use only for issue-driven micro-polish. |
| `EventsPage` | Low: already matches diagnostic timeline/filter Drawer spec | Medium | Medium: URL-state + Drawer draft/apply complexity | Medium-High: filter contract dense | Defer. Use only for a concrete usability defect. |
| `DashboardPage` | Low: already command-surface/workbench-first | High | Medium | High: dashboard facts are shared and tightly bounded | Defer. Avoid reopening settled dashboard IA without a concrete problem. |
| `NodeOnboardingPage` | Low after recent cleanup | High | Medium | Very High: token/command/binding contract | Exclude from broad IA. Safety-only if real testing finds a defect. |
| `SettingsPage` | Recently polished per current PRD; known settings/security surface | Medium | Medium | High: notification secrets/payload omission | Exclude from this batch unless a specific settings bug appears. |
| `NodeComparePage` | Recently polished per current PRD | Medium | Low | Low-Medium | Exclude from this batch. |
| `ProvidersPage` + `SubscriptionsPage` | Just completed | Medium | Low-Medium | Medium | Exclude; this audit is explicitly after that batch. |
| `NodeDetailPage`, `VPSDetailPage`, `TargetDetailPage`, `NodesPage`, `TargetsPage`, `VPSPage` | Recently polished per current PRD/current archives | High | Medium-High | Medium-High | Exclude from this batch. |

## Ranking Table

| Rank | Candidate / page group | Why it ranks here | Allowed posture |
|---:|---|---|---|
| 1 | `LoginPage` visual/auth-card consistency pass | Only remaining page with a visible spec fit gap and low risk after the recent IA sequence. Small, isolated, no backend/API/data/security behavior needed. | Tiny frontend-only polish, not a broad workflow redesign. |
| 2 | `AssetDecisionsPage` issue-driven micro-polish | Operationally important, but current code already implements the v2 unified work queue and tests freeze the behavior. | Defer unless a concrete real-use issue appears. |
| 3 | `EventsPage` issue-driven micro-polish | Already aligned with diagnostic timeline and URL-backed filter Drawer; high test/URL-state surface. | Defer; only copy/empty-state fixes with explicit acceptance criteria. |
| 4 | `DashboardPage` issue-driven micro-polish | High-value page but already strongly aligned and dashboard contract is sensitive. | Defer; no broad IA pass. |
| 5 | `NodeOnboardingPage` safety-only | Recent cleanup already addressed the previously identified safety/IA gaps; security-sensitive. | Touch only for verified token/command/binding issues. |
| 6 | Completed/recently polished pages | Current task PRD marks them as done. | Out of scope. |

## Recommended Scope

### Recommendation

Choose **`LoginPage` as a very small frontend-only visual/IA consistency pass** if the workflow needs a next page IA batch after ProvidersPage + SubscriptionsPage.

This is not a high-value operational IA batch. The audit did **not** find another remaining broad page/page group that is both meaningfully under-aligned and low-risk. Dashboard, Events, and AssetDecisions are already aligned with their v2 templates; NodeOnboarding is security-sensitive and already recently cleaned up. Therefore, the safest honest next scope is a narrow login/auth-card consistency pass, or no broad IA batch until a concrete user-facing defect appears.

### Allowed touch points

- `web/src/pages/LoginPage.tsx`
- `web/src/pages/LoginPage.css` (existing documented exception; do not add another page-local CSS file)
- `web/src/pages/LoginPage.test.tsx`

Optional only if needed for assertions, not expected:

- Existing shared atoms already used by LoginPage (`Button`, `Input`) via current props only.

### Recommended changes to keep within scope

- Align the login card with the explicit v2 LoginPage contract without changing auth behavior:
  - keep full-screen centered seal/aurora/card/form/alert structure;
  - use the intended primary login-button prominence;
  - keep brand copy truthful to Houfeng naming;
  - treat any version/footer text as a truthfulness decision rather than a new product claim.
- Keep LoginPage.css token/BEM-based and within the existing exception.
- Add/update small tests only for intentional visible copy/structure and existing frozen behavior.

### Frozen contracts

- Do **not** change route registration, `/login`, or protected-route behavior.
- Preserve `useAuth().login(username, password)` submit flow (`web/src/pages/LoginPage.tsx:16-29`).
- Preserve `next` query redirect behavior after successful login (`web/src/pages/LoginPage.tsx:22-23`).
- Preserve username/password labels and autocomplete semantics (`web/src/pages/LoginPage.tsx:47-59`).
- Preserve inline error alert behavior with `role="alert"` (`web/src/pages/LoginPage.tsx:42-45`).
- Preserve the existing test guard that LoginPage must not display misleading `单用户` / `全权限` / `个人系统` phrasing (`web/src/pages/LoginPage.test.tsx:9-23`).
- Do not add backend/API calls, auth API changes, new dependencies, CSS frameworks, a new CSS file, AppShell coupling, or screenshots/manifests.

## External References

- None. The request is an internal Houfeng design/spec/code fit audit.

## Related Specs

- `docs/design/v2-houfeng/design-language.md` — v2 visual language, density, page hierarchy, state language, and no-backend-change boundaries.
- `docs/design/v2-houfeng/component-spec.md` — explicit page templates including LoginPage, DashboardPage, AssetDecisionsPage, EventsPage, and NodeOnboardingPage.
- `.trellis/spec/web/component-conventions.md` — page/component boundaries, PageState, Drawer/DataTable contracts, and anti-patterns.
- `.trellis/spec/web/styling-guidelines.md` — styling placement, tokens/BEM, LoginPage.css exception, no new page-local CSS.
- `.trellis/spec/web/state-and-data.md` — no direct fetch, Dashboard/Asset/Events/onboarding data boundaries.
- `.trellis/spec/web/quality-guidelines.md` — page test patterns and lint/test/build expectations.

## Caveats / Not Found

- No external search was used or needed.
- No browser visual inspection was performed; this is a source/spec/archive audit.
- No dedicated remaining v2 page template was found for a new post-Providers/Subscriptions operational page group beyond the pages already aligned or recently completed.
- AssetDecisionsPage is not listed in the latest PRD’s recent-small-batch list, but current code and earlier UX-3 archive show it already implements the intended unified queue + Drawer decision model.
- LoginPage is the safest residual candidate but has limited operator-workflow value. If the goal is strictly high operational IA value, the audit result is effectively: **defer broad IA polish and wait for concrete defects/real-use findings**.
