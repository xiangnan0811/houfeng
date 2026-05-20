# Design/spec candidate audit: next page IA batch

## Visual / IA criteria

Use the active v2 Houfeng guidance as the selection filter:

1. **One page, one primary job**: default view should answer the page’s operational question, not enumerate all available API fields.
2. **High-density Chinese dark-first engineering UI**: preserve cold, restrained, dense engineering-tool UI; avoid SaaS card sprawl, KPI walls, and decorative big-screen monitoring style.
3. **Evidence hierarchy**: prioritize current decision/evidence above secondary history or low-value details. Do not invent facts not backed by current contracts.
4. **Page hierarchy**: page identity → current problem / highest-priority work → context/evidence → history/events → danger/sensitive actions.
5. **Command surfaces only where justified**: use them for pages that answer “what should I do first?”; do not force command surfaces onto simple auth or narrow troubleshooting pages.
6. **Drawer discipline**: create/edit/advanced filters/deferred details belong in Drawer when they interrupt a list or workbench scan path.
7. **DataTable vs queue**: DataTable for dense peer scanning; ordered custom queues for ranked work items. Preserve DataTable interactive descendant guards.
8. **PageState consistency**: loading/error/empty should use v2 PageState conventions: no spinner, no skeleton, Chinese explanation, technical summary for errors, retry where applicable.
9. **Security and truthfulness**: especially for onboarding and settings, IA changes must not weaken token secrecy, masking, payload omission, or center-owned command generation.
10. **Verification practicality**: prefer a batch with clear page tests and browser sanity scope; avoid low-value churn on heavily tested pages unless there is a concrete user-facing gap.

## Remaining page alignment

| Page | Current alignment | Notes |
|---|---:|---|
| `DashboardPage` | Already aligned / defer | Already follows asset-decision-first command surface, not KPI hero. Current structure matches v2 workbench intent and has strong regression tests against old KPI/dashboard patterns. |
| `EventsPage` | Already aligned / defer | Already has diagnostic timeline posture, support surface, applied/draft filter Drawer, URL-backed filters, filter chips, and event stream. High test density; change only for concrete usability issues. |
| `AssetDecisionsPage` | Already aligned / defer | Already implements unified asset decision work queue plus Drawer work panel and secondary renewal evidence. Broad IA rewrite would likely be churn. |
| `NodeOnboardingPage` | Under-aligned but security-sensitive | Highest remaining IA value. Existing seams: binding conflict is not visually first after hero, summary cards duplicate Stepper semantics, manual fallback has high page weight despite being troubleshooting-only. |
| `LoginPage` | Mostly aligned / low value | Matches simple centered auth spec. Possible version/footer truthfulness cleanup is metadata work, not a meaningful IA batch. |
| Residual small/status surfaces | No clear batch candidate | Should be handled only if tied to a concrete route/test failure or real operator feedback. |

## Recommended target scope

Recommended next batch: **`NodeOnboardingPage` safety-frozen display IA cleanup only**.

Reasoning:

- It is the only remaining page with a meaningful operator workflow and clear documented IA seams.
- Dashboard, Events, and AssetDecisions are already strongly aligned with their v2 templates.
- Login is too small to justify a page IA batch.
- Onboarding is high-value because it governs agent installation and binding, but the scope must be deliberately narrow.

Suggested scope boundaries:

1. Reorder or visually elevate binding conflict so it becomes the highest-priority conditional section when `binding_status === '指纹变更待确认'`.
2. Keep one-command install as the primary path.
3. Reduce visual weight of manual fallback so it reads as troubleshooting fallback, not a peer installation path.
4. Make summary cards subordinate to Stepper/install/conflict evidence; avoid duplicating Stepper status as a competing decision surface.
5. Limit changes to page composition/copy/CSS/tests; no backend/API/data/security changes.

## Key design / frozen-contract implications

For `NodeOnboardingPage`, freeze these contracts explicitly:

- Browser must never construct the production install command from `window.location.origin`, route params, or request metadata.
- The center-generated `issue.command` from `POST /api/nodes/{node_id}/install-command` is the only command shown for copy.
- Full enrollment tokens must appear only in the deliberate reveal/copy surface, not in incidental notices, conflict copy, logs, summaries, or screenshots.
- Manual fallback snippets must continue to use placeholders such as `<center public base URL>` and `<30-minute enrollment token>`.
- Backend 409 configuration errors such as missing public base URL or agent release version must be displayed as deployment/configuration errors; the frontend must not synthesize a fallback production command.
- Binding conflict copy must continue to warn that a pending fingerprint attempt may have consumed the one-time token and regeneration may be required.
- Existing generate/regenerate/hide/reveal/copy behavior must remain intact.
- No changes to backend handlers, API types, token issuing, enrollment semantics, data model, or installer contract.

## Verification implications

For a `NodeOnboardingPage` safety-only IA batch:

1. Run focused tests:
   - `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run src/pages/NodeOnboardingPage.test.tsx`
2. Preserve or strengthen assertions for backend-issued install command only, no `window.location.origin` production command synthesis, generate/regenerate, reveal/hide/copy, metadata display, 409 config errors, binding conflict confirm/reject/reset, conflict copy warning, and manual fallback placeholders.
3. Run standard web gates: `npm --prefix web run lint`, `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`, `npm --prefix web run build`.
4. If UI-visible changes are implemented, run browser sanity for `/nodes/:nodeId/onboarding` normal unbound state, generated command reveal/hide state, binding conflict state, and manual fallback/troubleshooting section.
5. If browser sanity uses mock data rather than a real authenticated center/PostgreSQL session, record that caveat explicitly.
