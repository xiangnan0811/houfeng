# Research: design spec candidate audit

- **Query**: Compare remaining Houfeng frontend pages against `docs/design/v2-houfeng/design-language.md`, `docs/design/v2-houfeng/component-spec.md`, and `.trellis/spec/web/*` after AssetDecisionsPage shipped. Identify any remaining page whose visual/IA alignment gap is meaningful enough for a low-risk frontend-only IA polish batch.
- **Scope**: internal
- **Date**: 2026-05-21

## Findings

### Files Found

| File Path | Description |
|---|---|
| `docs/design/v2-houfeng/design-language.md` | Active visual language: dark-first, high-density, font roles, states, loading/error/empty, hard boundaries. |
| `docs/design/v2-houfeng/component-spec.md` | Active component/page visual contracts, including page templates for Dashboard, VPS, VPS Detail, Nodes, Node Detail, Events, Settings, Targets, Target Detail, Node Onboarding, Login. |
| `.trellis/spec/web/index.md` | Web spec index; confirms visual authority is v2 Houfeng docs. |
| `.trellis/spec/web/styling-guidelines.md` | CSS/token/BEM/styling boundaries; no new per-page CSS except Login historical exception. |
| `.trellis/spec/web/component-conventions.md` | Component layering, PageState, Drawer/modal, no direct API in components, drawer state reset, no nested interactive semantics. |
| `.trellis/spec/web/state-and-data.md` | API/data contracts, no direct `fetch`, secret/token handling, Dashboard/Asset Ledger data boundaries, URL-state rules. |
| `.trellis/spec/web/directory-structure.md` | Route/page/component/lib/style organization. |
| `.trellis/spec/web/quality-guidelines.md` | Web verification and test conventions. |
| `web/src/app/router.tsx` | Current routed page inventory: Dashboard, VPS, VPS Detail, Providers, Subscriptions, AssetDecisions, Nodes, NodeCompare, NodeDetail, NodeOnboarding, Targets, TargetDetail, Events, Settings, Login. |
| `web/src/pages/SettingsPage.tsx` | Highest residual candidate: explicit Settings page spec exists, current page uses tabbed/channel-manager IA and mixed section ribbons. |
| `web/src/pages/settings/*.tsx` | Settings sub-sections; most use `DetailSection`, but only Feishu currently passes an explicit ribbon. |
| `web/src/pages/ProvidersPage.tsx` | Auxiliary Asset Ledger page, already uses asset summary, `DataTable`, `Drawer`, and `PageState`; no page-specific v2 template. |
| `web/src/pages/SubscriptionsPage.tsx` | Auxiliary Asset Ledger page, already uses URL-state, asset summary, `DataTable`, `Drawer`, VPS selector; no page-specific v2 template. |
| `web/src/pages/NodeComparePage.tsx` | Routed utility page with bespoke compare command surface; no page-specific v2 template. |
| `web/src/pages/LoginPage.tsx` / `web/src/pages/LoginPage.css` | Login page largely matches the v2 Login template; only tiny button-size/detail deltas. |
| `web/src/pages/DashboardPage.tsx` and `web/src/pages/dashboard/*` | Already follows command-surface + workbench pattern. |
| `web/src/pages/VPSPage.tsx` | Already follows VPS inventory quick-view/table/drawer pattern. |
| `web/src/pages/VPSDetailPage.tsx` and `web/src/pages/vps-detail/*` | Already follows VPS detail workbench/drawer/evidence pattern. |
| `web/src/pages/NodesPage.tsx` and `web/src/pages/nodes/*` | Already follows Node observability support + compact list frame pattern. |
| `web/src/pages/TargetsPage.tsx` and `web/src/pages/targets/*` | Already follows Target observability support + compact table pattern. |
| `web/src/pages/NodeDetailPage.tsx` and `web/src/pages/node-detail/*` | Already follows watchtower-style node detail pattern. |
| `web/src/pages/TargetDetailPage.tsx` and `web/src/pages/target-detail/*` | Already follows target detail workbench/watchtower pattern. |
| `web/src/pages/NodeOnboardingPage.tsx` | Already follows center-generated install command / token secrecy template. |
| `web/src/pages/EventsPage.tsx` and `web/src/pages/events/*` | Already follows diagnostic support surface + drawer filter + event stream pattern. |
| `web/src/pages/AssetDecisionsPage.tsx` | Recently shipped reference page; already implements unified decision queue + renewal evidence split. |

### Design / Spec Expectations Relevant to Residual Pages

1. **Visual authority and hard boundaries**
   - Visual authority is `docs/design/v2-houfeng/design-language.md` and `docs/design/v2-houfeng/component-spec.md`; `.trellis/spec/web/index.md:1-3` and `.trellis/spec/web/styling-guidelines.md:21-34` reinforce that older v1/Stitch visual material is not active.
   - Product tone is “冷静、克制、高密度、工程师长期使用友好” (`docs/design/v2-houfeng/design-language.md:38-39`).
   - UI must remain token/BEM based and avoid hard-coded colors outside tokens (`docs/design/v2-houfeng/design-language.md:111-115`, `.trellis/spec/web/styling-guidelines.md:51-57`).
   - No backend/API/data-shape changes, no Tailwind/CSS-in-JS/chart library, no i18n expansion, and no screenshot regression infra in visual polish batches (`docs/design/v2-houfeng/design-language.md:312-325`).

2. **Typography, density, and atoms**
   - Numeric metrics, technical IDs, and timestamps should use mono wrappers (`MonoDigits`, `Hostname`, `Timestamp`) (`docs/design/v2-houfeng/design-language.md:127-145`, `docs/design/v2-houfeng/component-spec.md:78-85`).
   - High-density surfaces should prefer compact tables/rows and avoid low-density KPI card walls (`docs/design/v2-houfeng/design-language.md:147-170`).
   - `DataTable` contract is compact semantic table with row hover / optional row click (`docs/design/v2-houfeng/component-spec.md:87-93`).
   - `Drawer` contract includes portal/focus/ESC/overlay behavior and is the preferred place for secondary create/edit forms (`docs/design/v2-houfeng/component-spec.md:105-115`, `.trellis/spec/web/component-conventions.md:48-50`, `.trellis/spec/web/component-conventions.md:57-58`).
   - Page loading/error/empty should use unified local surfaces, no spinner/toast (`docs/design/v2-houfeng/design-language.md:232-260`, `.trellis/spec/web/component-conventions.md:44-45`).

3. **SettingsPage explicit page contract**
   - The current v2 component spec gives SettingsPage a simple vertical section contract: Hero, `DetailSection` Theme (`ribbon notice`), Telegram (`ribbon accent-2`), Frequency (`ribbon normal`), Global Defaults (`ribbon notice`), Override Rules (`ribbon notice`), Retention (`ribbon notice`), and bottom unified save/errors (`docs/design/v2-houfeng/component-spec.md:291-299`).
   - Settings also intersects security-sensitive notification contracts: tokens/webhook values must not leak outside deliberate inputs/status, and Settings tests assert token masking and payload omission behavior (`web/src/pages/SettingsPage.test.tsx:137-145`, `web/src/pages/SettingsPage.test.tsx:202-286`, `web/src/pages/SettingsPage.test.tsx:563-617`).

4. **Asset Ledger residual list pages**
   - Component spec has detailed contracts for `AssetDecisionsPage`, `VPSPage`, and `VPSDetailPage`, but not separate page templates for Providers or Subscriptions (`docs/design/v2-houfeng/component-spec.md:221-241`).
   - `.trellis/spec/web/state-and-data.md:147-203` covers Asset Ledger list/decision data flow, selector-based associations, and front-end lightweight joins, which Providers/Subscriptions already follow.

5. **Utility / special pages**
   - `NodeComparePage` is routed (`web/src/app/router.tsx:80-82`) but has no explicit page template in `component-spec.md`. It should therefore be judged against generic design principles and existing atoms, not a missing page-specific spec.
   - Login does have a simple template (`docs/design/v2-houfeng/component-spec.md:339-345`) and current implementation is close (`web/src/pages/LoginPage.tsx:31-65`, `web/src/pages/LoginPage.css:4-122`).

### Current Alignment by Page Group

#### Already aligned / not good next-batch candidates

- **DashboardPage**: Uses `DashboardCommandSurface` and `DashboardWorkbench`, matching the command-surface contract rather than old KPI strips (`web/src/pages/DashboardPage.tsx:97-120`; expected at `docs/design/v2-houfeng/component-spec.md:202-219`).
- **AssetDecisionsPage**: Already shipped as unified decision queue + lower-weight renewal evidence (`web/src/pages/AssetDecisionsPage.tsx:555-691`; expected at `docs/design/v2-houfeng/component-spec.md:221-227`).
- **VPSPage**: Implements quick views via `Tabs`, field filters via drawer/state chips, high-density `DataTable`, and subscription evidence boundary copy (`web/src/pages/VPSPage.tsx:697-839`; expected at `docs/design/v2-houfeng/component-spec.md:228-234`).
- **VPSDetailPage**: Uses identity hero, decision workbench, operations summary, lifecycle confirmation, and one Drawer host for secondary details/forms (`web/src/pages/VPSDetailPage.tsx:880-976`; expected at `docs/design/v2-houfeng/component-spec.md:235-241`).
- **NodesPage**: Uses `NodesHero`, support surface, create drawer, observability list frame, toolbar, filters, compact table section (`web/src/pages/NodesPage.tsx:662-772`; expected at `docs/design/v2-houfeng/component-spec.md:243-256`).
- **TargetsPage**: Uses Target hero, create drawer, support surface, list command band, filters/batch panel, compact `DataTable` (`web/src/pages/TargetsPage.tsx:570-738`; expected at `docs/design/v2-houfeng/component-spec.md:301-308`).
- **EventsPage**: Uses hero, diagnostic support surface, filter overview, filter Drawer, event stream section (`web/src/pages/EventsPage.tsx:373-420`; expected at `docs/design/v2-houfeng/component-spec.md:282-290`).
- **NodeDetailPage**: Uses watchtower header, conditional danger/binding sections, time-window tabs, `NodeWatchtowerMetrics`, VPS evidence, history and command drawers (`web/src/pages/node-detail/NodeDetailPageBody.tsx:157-273`; expected at `docs/design/v2-houfeng/component-spec.md:257-280`).
- **TargetDetailPage**: Uses watchtower header, target judgment summary, danger card, observation workbench, probe list, maintenance section, activity grid, drawers (`web/src/pages/target-detail/TargetDetailPageBody.tsx:310-496`; expected at `docs/design/v2-houfeng/component-spec.md:310-324`).
- **NodeOnboardingPage**: Covered by explicit token/install-command contracts and tests; current implementation avoids `window.location.origin` command synthesis and warns about one-time token consumption (`web/src/pages/NodeOnboardingPage.tsx:282-385`, `web/src/pages/NodeOnboardingPage.tsx:636-826`; tests at `web/src/pages/NodeOnboardingPage.test.tsx:62-224`, `web/src/pages/NodeOnboardingPage.test.tsx:396-484`).
- **LoginPage**: Matches full-screen centered aurora/seal/card/motto/error pattern (`web/src/pages/LoginPage.tsx:31-65`, `web/src/pages/LoginPage.css:4-122`; expected at `docs/design/v2-houfeng/component-spec.md:339-345`). Tiny delta: submit button uses default size rather than explicit `lg` (`web/src/pages/LoginPage.tsx:60-62`), not enough for a batch.

#### Residual / auxiliary pages

- **ProvidersPage**:
  - Strong alignment: page panel, asset context summary, `DataTable`, `PageState`, create/edit Drawers (`web/src/pages/ProvidersPage.tsx:272-384`, `web/src/pages/ProvidersPage.tsx:386-477`).
  - Residual tiny gap: technical `provider_id` is rendered as a plain `span` (`web/src/pages/ProvidersPage.tsx:213-216`), whereas design-language prefers mono wrappers for technical IDs (`docs/design/v2-houfeng/design-language.md:127-145`).
  - No page-specific template exists, so this is a small typography/atom compliance item, not meaningful IA work.

- **SubscriptionsPage**:
  - Strong alignment: URL-state filters, context panel for `vps_id`, prerequisite panel, renewal/cost summary, filter chips, `DataTable`, create/edit Drawers with selector-based VPS association (`web/src/pages/SubscriptionsPage.tsx:445-620`, `web/src/pages/SubscriptionsPage.tsx:622-761`).
  - Residual tiny gap: technical `subscription_id` is plain text in the identity cell (`web/src/pages/SubscriptionsPage.tsx:371-374`); money/number strings mostly come from formatters rather than `MonoDigits` in all table cells. This is small visual consistency work, not a meaningful IA gap.
  - Current behavior directly supports `.trellis/spec/web/state-and-data.md:171-183` for `/subscriptions?vps_id=<id>&create=1` context and selector binding.

- **NodeComparePage**:
  - Current page already has a bespoke v2-like command surface, A/B identity cards, summary strip, and metric comparison detail section (`web/src/pages/NodeComparePage.tsx:89-127`, `web/src/pages/NodeComparePage.tsx:150-225`, `web/src/pages/NodeComparePage.tsx:319-403`).
  - CSS exists for this surface (`web/src/styles/pages.css:7484-7816`).
  - No component-spec page template exists. The gap is mainly “undocumented special page,” not a visual/IA mismatch against an active page template. Treat as low-value unless product wants Node compare to become a first-class workflow.

- **SettingsPage**:
  - Current page loads settings with standard PageState and data flow (`web/src/pages/SettingsPage.tsx:322-408`) and maintains one bottom save section (`web/src/pages/SettingsPage.tsx:625-655`).
  - Current IA differs from the explicit Settings page template by using a three-tab structure (`web/src/pages/SettingsPage.tsx:506-510`, `web/src/pages/SettingsPage.tsx:512-623`) and a notification channel manager/modal (`web/src/pages/SettingsPage.tsx:529-600`, `web/src/pages/SettingsPage.tsx:657-758`).
  - Most settings sub-sections use `DetailSection`, but expected ribbons are not consistently applied: Theme has no ribbon (`web/src/pages/settings/ThemeSettingsSection.tsx:21-25`), Telegram has no ribbon (`web/src/pages/settings/TelegramSettingsSection.tsx:93-110`), Frequency has no ribbon (`web/src/pages/settings/FrequencyDefaultsSection.tsx:50-75`), Global Defaults has no ribbon (`web/src/pages/settings/IncidentDefaultsSection.tsx:232-238`), Override Rules has no ribbon (`web/src/pages/settings/OverrideRulesSection.tsx:67-90`), Retention has no ribbon (`web/src/pages/settings/RetentionPolicySection.tsx:29-55`); Feishu does use `ribbon="accent-2"` (`web/src/pages/settings/FeishuSettingsSection.tsx:61-80`).
  - Tests currently assert the tabbed/channel-manager IA and secret-safe behaviors (`web/src/pages/SettingsPage.test.tsx:116-174`, `web/src/pages/SettingsPage.test.tsx:563-617`). A full return to the vertical spec would be behavioral churn, not low-risk. A narrow visual/IA polish inside the current tested structure is low-risk.

### Candidate Fit / Gap Ranking

| Rank | Candidate | Fit for low-risk frontend-only batch | Meaningful gap | Evidence | Recommendation |
|---:|---|---|---|---|---|
| 1 | `SettingsPage` | Medium-high if scoped narrowly; low-risk only if no API/payload/security logic changes. | Explicit Settings template expects section ribbons and vertical section hierarchy; current page uses tabs/channel manager and missing ribbons on most `DetailSection`s. | Spec: `docs/design/v2-houfeng/component-spec.md:291-299`; current page: `web/src/pages/SettingsPage.tsx:499-655`; section ribbons above. | Best remaining candidate, but scope must be UI-only and preserve tested tab/channel behavior. |
| 2 | `ProvidersPage` | High implementation safety, low value. | Only minor mono/ID atom consistency and perhaps copy density. No explicit page template gap. | `web/src/pages/ProvidersPage.tsx:213-216`, `web/src/pages/ProvidersPage.tsx:272-384`. | Not worth a dedicated batch unless bundled with Subscriptions as “tiny Asset Ledger residual polish.” |
| 3 | `SubscriptionsPage` | High implementation safety, low-medium value. | Minor mono/numeric consistency; IA already aligned with Asset Ledger data-flow specs. | `web/src/pages/SubscriptionsPage.tsx:371-374`, `web/src/pages/SubscriptionsPage.tsx:445-620`. | Not worth a dedicated batch unless bundled with Providers as a tiny pass. |
| 4 | `NodeComparePage` | Medium safety, low value. | No page-specific spec exists; current bespoke surface is already v2-like. | `web/src/pages/NodeComparePage.tsx:89-127`, `web/src/pages/NodeComparePage.tsx:150-225`. | Do not choose unless product decides Node compare needs a formal page contract. |
| 5 | `LoginPage` | Very safe, very low value. | Tiny delta only: button not explicitly `lg`; otherwise matches v2 login template. | `web/src/pages/LoginPage.tsx:31-65`, `web/src/pages/LoginPage.css:4-122`. | Too small for a batch. |
| 6 | Dashboard / VPS / VPSDetail / Nodes / Targets / NodeDetail / TargetDetail / NodeOnboarding / Events / AssetDecisions | Low need. | Current implementations visibly follow their active page templates. | See “Already aligned” section. | Do not select for this batch. |

### Recommendation for Next Batch

**Recommended next batch: a narrow `SettingsPage` visual/IA alignment pass.**

Reasoning:

- It is the only remaining routed page with an explicit page template in `component-spec.md` and a visible residual mismatch that is more than a tiny atom/typography fix.
- The mismatch is not that Settings is broken; it is that its current tabbed/channel-manager IA and inconsistent `DetailSection` ribbons do not cleanly express the v2 Settings section contract (`docs/design/v2-houfeng/component-spec.md:291-299`).
- A full IA rewrite back to one vertical sequence would conflict with existing tests and would touch security-sensitive notification UX. The low-risk batch should therefore **polish within the current tested IA**, not replace it.

Recommended safe shape:

1. Keep the current three top-level tabs and channel-manager modal unless the main task explicitly decides to change behavior.
2. Add/normalize v2 section hierarchy inside the existing tabs:
   - consistent `DetailSection` ribbons matching the Settings template (`notice`, `accent-2`, `normal` as applicable);
   - clearer top-level “settings workbench/status” framing if done with existing CSS classes/components;
   - keep the bottom unified save footer as the single page-level submit point.
3. Preserve all current Settings tests and add only frontend tests for visible hierarchy/ribbon/secret-safe rendering if code changes.
4. Do not change settings data fetching, payload shape, validation, notification runtime semantics, or secret display behavior.

If the next batch should avoid settings/security-sensitive surfaces entirely, then **only tiny low-value residual work remains**: Providers/Subscriptions mono-wrapper and numeric consistency, plus Login button size. That bundle would be safe but likely too small to justify a full IA polish batch.

### Scope Boundaries and Frozen Contracts for Recommended Candidate

For `SettingsPage`:

- **Frontend-only**: limit changes to `web/src/pages/SettingsPage.tsx`, `web/src/pages/settings/*`, existing tests, and existing shared CSS if needed. No Go/backend/API changes.
- **No API contract changes**: keep `getSettings()` / `updateSettings()` usage, `SettingsRecord`, `SettingsUpdateInput`, and `/api/settings` payload shape unchanged (`.trellis/spec/web/state-and-data.md:24-37`).
- **Secret handling frozen**:
  - Do not render cleartext Telegram bot tokens outside the password input the user types into.
  - Keep masked token summary only (`web/src/pages/settings/TelegramSettingsSection.tsx:50-62`; tests at `web/src/pages/SettingsPage.test.tsx:137-145`).
  - Keep “omit `bot_token` when not replacing” behavior (`web/src/pages/SettingsPage.test.tsx:202-286`).
  - Keep dismissed add-channel modal drafts out of the saved payload (`web/src/pages/SettingsPage.test.tsx:563-617`).
  - Do not add Dashboard/AppShell notification secret displays; Dashboard spec explicitly allows only boolean notification summaries (`.trellis/spec/web/state-and-data.md:111-114`).
- **Runtime semantics frozen**:
  - Do not change Telegram runtime-managed validation (`web/src/pages/SettingsPage.tsx:151-160`).
  - Do not claim runtime notification delivery beyond existing `runtime_managed` / `runtime_apply_active` fields.
- **Persistence semantics frozen**:
  - Keep one full-page save action (`web/src/pages/SettingsPage.tsx:625-655`).
  - Do not split Settings into independent save APIs unless a separate product/API task is approved.
- **Style boundaries**:
  - Use tokens/BEM and existing global style files; do not create a new page CSS file for Settings (`.trellis/spec/web/styling-guidelines.md:93-112`, `.trellis/spec/web/styling-guidelines.md:138-150`).
  - Do not introduce new UI libraries or chart libraries (`docs/design/v2-houfeng/design-language.md:312-325`).

### Warnings Against Speculative Rewrites or Security-Sensitive Changes

- Do not use this batch to redesign notification provider semantics, add new notification channels beyond existing Telegram/Feishu UI, or move secrets into global status surfaces.
- Do not infer or expose webhook/token values from settings records; only use fields already returned by the API.
- Do not convert Settings tabs to vertical sections unless the main task explicitly accepts the larger UX/test churn; current tests assert tab and channel modal behavior.
- Do not change retention worker behavior, incident thresholds semantics, runtime frequency planning, or override-rule data shape. A visual/IA batch should not alter these operational contracts.
- Do not touch `NodeOnboardingPage` one-command installer flow in this batch; it is token/security-sensitive and already has a strong spec/test contract around center-generated commands and one-time tokens.
- Do not broaden `NodeComparePage` into a new product workflow without first adding/confirming a page contract; the current gap is absence of a page-specific template, not evidence of an implementation mismatch.

### External References

None. This was an internal repository/spec audit only.

### Related Specs

- `docs/design/v2-houfeng/design-language.md` — visual language, density, typography, state, loading/error/empty, and hard boundaries.
- `docs/design/v2-houfeng/component-spec.md` — atom/shared component/page templates; SettingsPage contract is the key candidate reference.
- `.trellis/spec/web/styling-guidelines.md` — CSS/token/BEM/styling boundaries.
- `.trellis/spec/web/component-conventions.md` — component layering, Drawer/modal contracts, PageState, form/draft reset rules.
- `.trellis/spec/web/state-and-data.md` — Settings/API/secret/data-flow constraints and Asset Ledger data contracts.
- `.trellis/spec/web/quality-guidelines.md` — verification and test expectations for any frontend changes.

## Caveats / Not Found

- No external research was required; all relevant authority is in tracked repository docs/specs and code.
- ProvidersPage, SubscriptionsPage, and NodeComparePage do not have explicit page templates in `component-spec.md`; their ranking is based on generic design-language/spec expectations rather than a page-specific contract.
- This audit did not run browser visual evidence; it compares source structure and active specs only.
- No code was modified outside this research file.
