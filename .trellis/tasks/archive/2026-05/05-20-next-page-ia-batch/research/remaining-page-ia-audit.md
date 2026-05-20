# Remaining page IA audit

## Scope

Audit the remaining Houfeng routed pages after recent page information-architecture releases to choose the next highest-value IA batch without asking the user to confirm page order.

Recent completed/released IA batches include NodeDetail, VPSDetail, Providers/Subscriptions, TargetDetail, TargetsPage + NodesPage controls, NodeComparePage, SettingsPage, and VPSPage.

## Candidate matrix

| Candidate | Current IA quality | Risk | Safe polish seams | Recommendation |
|---|---:|---:|---|---|
| `NodeOnboardingPage` | Medium-high, but still has meaningful hierarchy tension around install, binding conflict, evidence, and manual fallback | High because install/token/binding contracts are security-sensitive | Presentation-only safety-preserving polish: raise binding conflict priority, make one-command install clearer as primary path, reduce manual fallback visual weight, clarify phase/sample evidence | Choose now, with tight frozen contracts |
| `DashboardPage` | High; already command-surface + workbench oriented | Medium-high due broad dashboard regression surface and deep-link expectations | Minor copy only | Defer |
| `EventsPage` | High; already URL-backed applied filters + draft Drawer + stream | High due URL/filter/load-more/backfill semantics and dense tests | Minor filter-context copy only | Defer |
| `AssetDecisionsPage` | High; already unified queue + Drawer work panel + renewal evidence | Medium-high due evidence truthfulness, row navigation, update payload semantics | Minor framing/copy only | Defer |
| `LoginPage` | Adequate; intentionally small auth gate | Low | Possible small footer/version truthfulness cleanup, but not a meaningful IA batch | Defer |
| Smaller residual pages | Mostly already covered by recent IA batches or not routed as standalone major work surfaces | Varies | No clear page IA batch candidate | Defer |

## Recommended page/scope

Recommended next batch: **`NodeOnboardingPage` safety-preserving information architecture polish**.

Target outcome:

1. Make the page’s primary job clearer: safely bring a Node from pending access to trusted agent sync.
2. Keep one-command install as the dominant primary path.
3. Elevate binding conflict / fingerprint-change state closer to the hero and progress path when present.
4. Treat manual fallback as secondary troubleshooting, not an equal primary path.
5. Clarify how host sample / accepted observation evidence relates to onboarding progress.
6. Keep install-command, token, binding, endpoint, and placeholder behavior frozen.

## Why this beats alternatives

- `DashboardPage`, `EventsPage`, and `AssetDecisionsPage` already match the current v2 page templates closely enough that another broad pass would be low-value or mostly copy churn.
- `LoginPage` is too small and intentionally focused for a Trellis IA batch.
- `NodeOnboardingPage` is the only remaining high-value routed page where clearer hierarchy can reduce operator confusion, especially around generated install command, manual fallback, and fingerprint conflict handling.
- The recommended scope is narrow because onboarding is security-sensitive.

## Frozen contracts for `NodeOnboardingPage`

Do not change:

- Generated install commands must come from center via `POST /api/nodes/{node_id}/install-command`.
- Browser must not synthesize production install commands from `window.location.origin`.
- Frontend may only display the backend-returned install `command`.
- Enrollment tokens are secrets.
- Full tokens must not appear outside the deliberate authenticated command reveal/copy surface.
- Hide/reveal/copy/regenerate behavior must remain unchanged.
- Regeneration must invalidate/replace the previous visible command.
- Binding conflict confirm/reject/reset endpoints and confirmation semantics must remain unchanged.
- Masked fingerprint display in conflict UI must remain masked.
- Manual fallback placeholders must remain placeholders: `<30-minute enrollment token>` and `<center public base URL>`.
- Manual fallback must not be replaced with generated or real tokens.
- API request shapes and backend data model must remain unchanged.
- Existing copy that the command is center-generated and uses `HOUFENG_PUBLIC_BASE_URL` must remain truthful.

## Likely implementation seams

Likely files:

- `web/src/pages/NodeOnboardingPage.tsx`
- `web/src/pages/NodeOnboardingPage.test.tsx`
- `web/src/styles/pages.css`

Safe seams:

- Reorder or reframe existing sections without changing state machines.
- Add class-based structural wrappers using existing BEM-ish style patterns.
- Move conflict handling visually higher when `binding_status === '指纹变更待确认'`.
- Make manual fallback visually subordinate/troubleshooting-oriented while preserving all snippets and placeholders.
- Improve copy around current phase, last host sample, and accepted observation as evidence.
- Keep existing atoms/components; do not introduce new CSS systems, dependencies, or backend fields.

## Suggested tests

Keep existing tests and add/adjust coverage for:

- One-command install remains primary before command generation.
- Generated command is still fetched from `/api/nodes/{nodeId}/install-command`.
- No browser-origin fallback appears in UI.
- Missing center config error does not reveal or synthesize an install URL.
- Manual fallback still uses placeholders only.
- Generated command hide/reveal/copy/regenerate behavior remains unchanged.
- Regeneration removes/replaces prior command text.
- Binding conflict appears in the elevated priority area when present.
- Binding conflict actions still use two-step confirmation and masked fingerprints.
- No full token appears in conflict copy, summary text, manual fallback, or non-command areas.
- Sample/observation summary remains visible but does not imply enrollment success unless existing data supports it.

## Suggested browser sanity

Cover `/nodes/:nodeId/onboarding` with mock-backed data for:

1. Normal pending onboarding state before command generation.
2. Command generated state with hide/reveal/copy controls.
3. Backend install-command configuration error.
4. Binding conflict / fingerprint-change-pending state.
5. Mobile/narrow viewport to ensure the primary install path and conflict warning remain discoverable.

If browser sanity uses mock data rather than a real authenticated center/PostgreSQL environment, record the caveat explicitly.
