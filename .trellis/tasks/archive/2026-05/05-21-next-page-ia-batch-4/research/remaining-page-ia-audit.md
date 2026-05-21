# Research: remaining page IA audit

- **Query**: Audit remaining Houfeng frontend routes/pages after completed IA batches (NodeDetail, VPSDetail, Settings, Targets+Nodes lists, NodeCompare, VPSPage inventory, NodeOnboarding, TargetDetailPage, ProvidersPage + SubscriptionsPage, AssetDecisionsPage). Identify the next highest-value low-risk frontend-only IA polish candidate/page group, including current routed pages inventory, recently optimized areas to avoid, candidate ranking, recommended scope, frozen contracts/risks, and caveat if remaining value is low.
- **Scope**: internal
- **Date**: 2026-05-21

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/app/router.tsx` | Canonical route registry and lazy page mapping for the SPA. Current route inventory is defined at `web/src/app/router.tsx:62-99`. |
| `web/src/app/metadata.ts` | Primary navigation grouping: 总览, 资产, 观测, 系统 at `web/src/app/metadata.ts:17-43`. |
| `web/src/pages/LoginPage.tsx` / `web/src/pages/LoginPage.css` | Auth-gate route. Implementation is compact and already matches the full-screen login template closely; submit/redirect/error flow is at `web/src/pages/LoginPage.tsx:16-63`. |
| `web/src/pages/DashboardPage.tsx` and `web/src/pages/dashboard/*` | Dashboard command-surface/workbench implementation; previously optimized and already aligned to v2 command-surface intent. |
| `web/src/pages/EventsPage.tsx` and `web/src/pages/events/*` | Diagnostic timeline implementation with URL-backed applied filters and draft Drawer; previously optimized and regression-sensitive. |
| `web/src/pages/SettingsPage.tsx` and `web/src/pages/settings/*` | Settings route. Recently optimized, but the separate design/spec audit found one concrete residual visual/IA alignment gap around section hierarchy/ribbons versus the explicit Settings template. |
| `web/src/pages/ProvidersPage.tsx` / `web/src/pages/SubscriptionsPage.tsx` | Recently optimized Asset Ledger support pages. Current structure uses summary/context panels, tables, and Drawers. |
| `web/src/pages/AssetDecisionsPage.tsx` | Recently optimized Asset Ledger decision queue. Should not be repeated without a concrete defect. |
| `web/src/pages/NodeDetailPage.tsx`, `web/src/pages/VPSDetailPage.tsx`, `web/src/pages/NodesPage.tsx`, `web/src/pages/TargetsPage.tsx`, `web/src/pages/TargetDetailPage.tsx`, `web/src/pages/NodeComparePage.tsx`, `web/src/pages/NodeOnboardingPage.tsx`, `web/src/pages/VPSPage.tsx` | Recently optimized or already strongly aligned page implementations; not good speculative repeat candidates. |
| `docs/design/v2-houfeng/design-language.md` | Active visual language authority: dark-first, restrained, high-density engineering UI, no speculative backend/API/data changes for visual work. |
| `docs/design/v2-houfeng/component-spec.md` | Active page/component template reference. The explicit SettingsPage template is the key residual spec reference. |
| `.trellis/spec/web/component-conventions.md` | Page/component layering, PageState, Drawer/modal reset, and interactive-row semantics. |
| `.trellis/spec/web/styling-guidelines.md` | Pure CSS, tokens, BEM, and no new page-local CSS except historical `LoginPage.css`. |
| `.trellis/spec/web/state-and-data.md` | API/client/data-state constraints, settings secret handling, URL state rules, and Asset Ledger data boundaries. |
| `.trellis/tasks/05-21-next-page-ia-batch-4/research/design-spec-candidate-audit.md` | Related audit comparing remaining pages against v2 design/spec. It recommends a narrow SettingsPage alignment pass only if scoped inside existing tested behavior. |

### Current Routed Pages Inventory

Source: `web/src/app/router.tsx:62-99`.

| Route | Page / Behavior | Notes |
|---|---|---|
| `/login` | `LoginPage` | Public auth gate outside `RequireAuth`; redirects to `next` query or `/` after login. |
| `/` | `DashboardPage` | Authenticated index route under `AppShell`. |
| `/vps` | `VPSPage` | Asset Ledger VPS inventory route. |
| `/vps/:vpsId` | `VPSDetailPage` | VPS detail/workbench route. |
| `/providers` | `ProvidersPage` | Asset Ledger provider master-data route. |
| `/subscriptions` | `SubscriptionsPage` | Asset Ledger subscription/renewal evidence route. |
| `/asset-decisions` | `AssetDecisionsPage` | Asset decision queue route. |
| `/nodes` | `NodesPage` | Node observability list route. |
| `/nodes/compare` | `NodeComparePage` | Node comparison utility route. |
| `/nodes/:nodeId` | `NodeDetailPage` | Node detail/watchtower route. |
| `/nodes/:nodeId/onboarding` | `NodeOnboardingPage` | Security-sensitive one-command install/onboarding route. |
| `/targets` | `TargetsPage` | Target observability list route. |
| `/targets/:targetId` | `TargetDetailPage` | Target detail/workbench route. |
| `/events` | `EventsPage` | Event timeline/diagnostic route. |
| `/settings` | `SettingsPage` | Settings and notification/retention/incident defaults route. |
| `*` | `<Navigate to="/" replace />` | Authenticated wildcard fallback. |

Primary navigation groups are defined separately at `web/src/app/metadata.ts:17-43`:

| Nav Group | Routes |
|---|---|
| 总览 | `/` |
| 资产 | `/asset-decisions`, `/vps`, `/providers`, `/subscriptions` |
| 观测 | `/nodes`, `/targets`, `/events` |
| 系统 | `/settings` |

### Recently Optimized / Avoid Repeating Without Concrete Defects

| Page / Area | Recent IA Work Evidence | Audit Conclusion |
|---|---|---|
| `NodeDetailPage` | `.trellis/tasks/archive/2026-05/05-19-optimize-node-detail-information-architecture/prd.md` | Do not repeat broad IA; current page follows watchtower/detail section patterns. |
| `VPSDetailPage` | `.trellis/tasks/archive/2026-05/05-19-optimize-vps-detail-information-architecture/prd.md` | Do not repeat broad IA; preserve decision workbench/detail semantics. |
| `SettingsPage` | `.trellis/tasks/archive/2026-05/05-20-settings-page-limited-ia-cleanup/prd.md` | Recently optimized; do not repeat broadly. Only a concrete residual spec-alignment gap justifies a narrow pass. |
| `TargetsPage` + `NodesPage` lists | `.trellis/tasks/archive/2026-05/05-20-continue-next-page-information-architecture-optimization/prd.md` | Do not repeat broad list IA; URL/filter/list semantics are regression-sensitive. |
| `NodeComparePage` | `.trellis/tasks/archive/2026-05/05-20-continue-remaining-page-information-architecture-optimization/prd.md` | Do not repeat; current bespoke compare surface is already v2-like and no explicit page template gap was found. |
| `VPSPage` inventory | `.trellis/tasks/archive/2026-05/05-20-vps-page-inventory-ia-polish/prd.md` | Do not repeat; preserve inventory filters, subscription evidence state, and row navigation. |
| `NodeOnboardingPage` | `.trellis/tasks/archive/2026-05/05-20-next-page-ia-batch/prd.md` | Do not repeat; security-sensitive one-command install/token contracts are frozen and well-covered. |
| `TargetDetailPage` | `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch/prd.md` | Do not repeat; current detail workbench/watchtower alignment is recent. |
| `ProvidersPage` + `SubscriptionsPage` | `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-2/prd.md` | Do not repeat broad support-page IA; only tiny atom/typography consistency remains. |
| `AssetDecisionsPage` | `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-3/prd.md` | Do not repeat; it just shipped as v0.15.0 and is the freshest IA batch. |
| `DashboardPage` | Earlier command-surface tasks including `.trellis/tasks/archive/2026-05/05-11-ux-dashboard-command-surface/prd.md` and `.trellis/tasks/archive/2026-05/05-13-ux3-dashboard-command-surface-polish/prd.md` | Not in the latest completed list, but already heavily optimized; no broad speculative dashboard IA pass recommended. |
| `EventsPage` | Earlier filter/timeline tasks including `.trellis/tasks/archive/2026-05/05-10-events-filter-drawer/prd.md` and `.trellis/tasks/archive/2026-05/05-13-ux6c-events-timeline-evidence/prd.md` | Already diagnostic-timeline aligned and URL-state heavy; no broad speculative events IA pass recommended. |

### Code Patterns

#### Route and nav topology

- Routing is centralized in `web/src/app/router.tsx`; route registration is the source of truth for the page inventory. The authenticated routes are nested under `RequireAuth` and `AppShell` (`web/src/app/router.tsx:62-99`).
- Navigation is intentionally grouped by operational area in `web/src/app/metadata.ts:17-43`; no new route or nav group is required for a frontend-only IA polish batch.

#### Completed page patterns to preserve

- Asset Ledger support pages now use compact page panels, summary/context surfaces, `DataTable`, `PageState`, and secondary Drawers. Example: `ProvidersPage` uses an Asset Ledger hero and provider evidence table at `web/src/pages/ProvidersPage.tsx:272-384`, then create/edit Drawers at `web/src/pages/ProvidersPage.tsx:386-477`.
- `AssetDecisionsPage` is already the current reference for the unified decision queue and should not be selected again without a concrete defect because the latest archived PRD explicitly scoped it as a micro-polish.
- `NodeOnboardingPage` must remain security-frozen: generated install commands come from the center, manual snippets use placeholders, and full enrollment tokens must not be exposed outside deliberate authenticated reveal/copy surfaces.

#### Residual candidate patterns

- `LoginPage` is very small and low-risk. It already implements username/password state, generic error, `next` redirect, and the centered login card (`web/src/pages/LoginPage.tsx:16-63`). Residual value is limited to tiny copy/visual details, not a meaningful IA batch.
- `SettingsPage` is the only remaining route where the related design/spec audit found a concrete residual gap large enough to justify a narrow batch: the active v2 component spec describes a Settings section hierarchy with consistent `DetailSection` ribbons, while the current implementation keeps a tested tab/channel-manager IA and inconsistent section ribbon usage. This is a concrete alignment gap, but the page is also recently optimized and security-sensitive.

### Remaining Candidate Ranking

| Rank | Candidate | Value | Risk | Rationale | Recommended Handling |
|---:|---|---|---|---|---|
| 1 | `SettingsPage` narrow visual/IA alignment pass | Medium | Medium unless tightly scoped | It is recently optimized, so broad repeat work is not appropriate. However, the related design/spec audit found a concrete residual mismatch against the explicit Settings template: section hierarchy/ribbons are not normalized, and Settings is the only remaining routed page with a meaningful page-specific spec gap. | Best next candidate only as a small, frontend-only, security-frozen pass inside the existing tab/channel-manager structure. |
| 2 | `LoginPage` auth-gate micro-polish | Very low | Very low | It is not in the recent IA batch list and is implementation-small. But current `LoginPage` already matches the v2 login template closely (`web/src/pages/LoginPage.tsx:31-65`, `web/src/pages/LoginPage.css`), so there is little user value. | Use only if avoiding Settings/security-sensitive surfaces entirely; scope should be tiny. |
| 3 | `ProvidersPage` + `SubscriptionsPage` residual atom/typography pass | Low | Low | Both were just optimized. The only residuals found are small technical-ID/mono/numeric consistency items, not IA problems. | Not worth a dedicated IA batch unless bundled as a tiny cleanup; avoid broad repeat. |
| 4 | `NodeComparePage` | Low | Medium | It is routed and bespoke, but recently optimized and already v2-like. No active page-specific template exists, so the gap is documentation/formalization rather than implementation IA. | Do not choose unless product explicitly promotes compare to a first-class workflow. |
| 5 | `DashboardPage` | Potentially high, but currently saturated | High | It is operationally important but already command-surface aligned and heavily worked. Regression surface is larger than the likely polish value. | Defer until concrete dashboard defects or real-use findings appear. |
| 6 | `EventsPage` | Potentially high, but currently saturated | High | It already has URL-state filters, draft Drawer, diagnostic support surface, and event stream sections. Filter/query semantics make speculative IA churn risky. | Defer until concrete event-timeline/filter defects appear. |
| 7 | Recently completed detail/list/onboarding/asset-decision pages | Low | Varies, often high | They were explicitly listed as completed by the task and should not be repeated without concrete defects. | Out of scope for this batch. |

### Recommended Next Page / Page Group and Narrow Scope

**Recommended next candidate: `SettingsPage`, but only as a narrow, security-frozen visual/IA alignment pass.**

Why this is the best remaining candidate:

1. The audit found no broad high-value low-risk page left after the latest IA batches.
2. Most remaining routes are either recently optimized or already aligned with their v2 page templates.
3. `SettingsPage` is also recently optimized, but unlike the other residual pages it has a concrete remaining spec-alignment issue documented in `research/design-spec-candidate-audit.md`: the active Settings template expects clearer section hierarchy/ribbon treatment, while the current page uses a tested tab/channel-manager structure and inconsistent `DetailSection` ribbons.
4. A safe pass can improve hierarchy without changing any settings data flow, payload, notification semantics, or secret display behavior.

Narrow scope:

- Keep the current `SettingsPage` route, three top-level tabs, notification channel manager/modal, and bottom unified save action.
- Normalize section framing inside the current tested IA rather than converting the page to a full vertical rewrite.
- Prefer copy/section hierarchy/ribbon/class-hook refinements using existing components and CSS tokens.
- Add/update tests only for visible IA text/structure and existing secret-safety contracts.
- Do not change backend, API client calls, request/response types, route paths, nav groups, auth/session behavior, notification runtime semantics, retention semantics, or incident threshold semantics.

Fallback if Settings is considered too recently optimized or too security-sensitive:

- The only safer fallback is a tiny `LoginPage` polish. It is very low-risk but also very low-value; it should not be framed as a meaningful IA batch.
- Providers/Subscriptions residual atom consistency is similarly safe but too small and too recent to justify another broad page batch.

### Frozen Contracts / Risks to Preserve for Recommended Scope

For a narrow `SettingsPage` pass:

| Contract / Risk | Must Preserve |
|---|---|
| Frontend-only boundary | Limit implementation to `web/src/pages/SettingsPage.tsx`, `web/src/pages/settings/*`, `web/src/pages/SettingsPage.test.tsx`, and existing global CSS if necessary. No Go/backend/migration/API changes. |
| API usage | Keep `getSettings()` / `updateSettings()` usage, `SettingsRecord`, `SettingsUpdateInput`, and `/api/settings` payload semantics unchanged. |
| Save model | Keep one page-level save action; do not split settings into independent save APIs or independent persistence flows. |
| Existing IA | Preserve the current three-tab structure and notification channel manager/modal unless a separate task explicitly accepts larger UX/test churn. |
| Secret handling | Do not render cleartext Telegram bot tokens outside deliberate password input/edit surfaces; keep masked token summaries; keep “omit `bot_token` when unchanged” behavior; keep dismissed modal drafts out of saved payloads. |
| Notification runtime semantics | Do not claim delivery/runtime state beyond existing fields such as `runtime_managed` and `runtime_apply_active`; do not add new notification providers or channels. |
| Dashboard/AppShell secrecy | Do not surface notification secrets or webhook/token values in Dashboard, AppShell, nav, or global summaries. |
| Operational semantics | Do not alter retention worker behavior, incident thresholds, override-rule data shapes, runtime frequency planning, or notification delivery behavior. |
| Styling | Use existing tokens/BEM/global CSS patterns; do not add a new Settings page-local CSS file, Tailwind, CSS-in-JS, chart library, or dependency. |
| Tests | Preserve current Settings tests for token masking, payload omission, tab/channel modal behavior, and draft discard; add only visible-structure assertions for any IA copy/ribbon changes. |

### External References

None. This was an internal code/spec/archive audit only.

### Related Specs

- `docs/design/v2-houfeng/design-language.md` — active visual language and hard boundaries for visual-only work.
- `docs/design/v2-houfeng/component-spec.md` — active page/component templates; SettingsPage section hierarchy is the relevant residual reference.
- `.trellis/spec/web/component-conventions.md` — component layering, PageState, Drawer/modal reset, and interactive semantics.
- `.trellis/spec/web/styling-guidelines.md` — pure CSS, tokens, BEM, and no new page-local CSS except historical LoginPage exception.
- `.trellis/spec/web/state-and-data.md` — API/data/secret/URL-state constraints.
- `.trellis/spec/web/quality-guidelines.md` — lint/test/build expectations for frontend changes.

## Caveats / Not Found

- Remaining value is low. After AssetDecisionsPage v0.15.0 and the preceding page IA batches, there is no broad high-value low-risk page left for speculative IA polish.
- The `SettingsPage` recommendation is conditional: it is justified only because a concrete residual v2 spec-alignment gap was found. It should not become a broad second Settings rewrite.
- If the project policy treats any recently optimized page as fully off-limits regardless of residual spec mismatch, then the next safest candidate is `LoginPage`, but its value is very small.
- This audit did not run browser visual evidence; it is based on route/page source, active specs, related research, and archived Trellis PRDs.
- No external search was required.
