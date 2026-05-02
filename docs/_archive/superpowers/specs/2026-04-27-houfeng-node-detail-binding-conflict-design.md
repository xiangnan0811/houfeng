# Houfeng Node Detail Binding Conflict Design

## Context

The frozen V1 flow requires binding-conflict visibility in the node list, node detail, and event stream. The current implementation already has a dedicated Node Onboarding page with conflict actions, but Node Detail only shows the `binding_status` badge. That leaves a V1 gap: operators can land on the normal node detail page and miss the high-risk “指纹变更待确认” decision surface.

This slice keeps the existing onboarding model and APIs. It does not redesign the identity workflow and does not introduce automatic rebinding or history splitting.

## Goal

Add a high-priority binding-conflict card to `NodeDetailPage` when a node is in `指纹变更待确认`, with enough context and actions to resolve the conflict from the detail page.

## Frozen V1 requirements used

- A binding conflict means the token is valid, but the arriving machine fingerprint differs from the current binding.
- Node Detail must visibly surface the conflict near the top of the page.
- The card should show current binding summary, new fingerprint metadata, first/last seen time, and attempt count.
- The UI should help answer whether this is the original machine returning after reinstall or a different machine using an old token.
- Supported V1 actions are:
  - 确认重绑定
  - 拒绝新指纹
  - 重置绑定
- V1 should not auto-create a new node, split history, clone node config, or silently accept a new fingerprint.

## Approach

Use the existing onboarding read/action APIs from Node Detail:

- Load normal node detail and runtime facts as today.
- Only when `node.binding_status === '指纹变更待确认'`, fetch `/api/nodes/:nodeId/onboarding` to obtain `pending_binding` metadata.
- Render a high-priority card directly below the hero panel.
- Use existing helpers:
  - `getNodeOnboarding`
  - `confirmNodeRebind`
  - `rejectPendingNodeBinding`
  - `resetNodeBinding`
- After an action succeeds, update the local `node` record from the returned onboarding state and hide the card when the conflict is resolved.

## Alternatives considered

### A. Only link from Node Detail to Node Onboarding

Rejected. It improves discoverability but still leaves the frozen Node Detail conflict-card requirement unmet.

### B. Duplicate the full onboarding workflow in Node Detail

Accepted in a narrow form. Node Detail should surface the same decision, but only the conflict card/actions. Enrollment-token generation and install guidance remain on the onboarding page.

### C. Build general Node edit/lifecycle management first

Rejected for this slice. Node edit APIs are a broader V1 gap, but they are not required to close the binding-conflict detail-card gap.

## UX contract

The card appears only for `指纹变更待确认`.

It shows:

- Eyebrow/title indicating high priority binding conflict.
- Current bound fingerprint summary.
- Pending fingerprint, masked for readability.
- Pending first seen time.
- Pending last seen time.
- Pending attempt count.
- A short decision guide:
  - accept only if this is the same machine returning after reinstall or legitimate replacement;
  - reject if a different machine reused an old token;
  - reset only when clearing the current binding and returning to waiting-for-binding is intended.

Actions use Chinese labels:

- `确认重绑定`
- `拒绝新指纹`
- `重置绑定`

While one action is pending, all card actions are disabled. Errors stay inside the card and do not replace the whole Node Detail page.

If onboarding metadata cannot be loaded, the page still shows the node detail and a local conflict-card error state with a link to the onboarding page.

## Data flow and stale-safety

Node Detail keeps normal route-local stale guards:

- A conflict metadata response only updates state if the component is still mounted and the current route/requested node ID still matches.
- A conflict action response only updates state under the same route/request guard.
- Switching routes while a conflict request is in flight must not update the new node page with stale conflict data.

## Scope

In scope:

- `NodeDetailPage` state and rendering changes.
- `NodeDetailPage` focused tests.
- Reusing existing web API helpers and existing backend routes.

Out of scope:

- Backend schema/API changes.
- Node list conflict filter.
- Event-stream enhancements.
- Node edit APIs.
- New top-level pages.
- Changing the onboarding page workflow.

## Testing strategy

Focused frontend tests should cover:

1. Conflict card loads onboarding metadata and renders current/pending fingerprint details.
2. Confirm/reject/reset actions call the existing endpoints and update/hide the card after success.
3. Action failures stay local to the conflict card.
4. Onboarding metadata load failure keeps the node detail page visible.
5. Stale conflict metadata/action responses after route switch do not update the new route.

Existing Node Detail runtime/action tests should continue passing.

## Self-review

- Placeholder scan: no placeholders remain.
- Scope check: one frontend page plus tests; no backend work.
- Ambiguity check: actions, display location, error handling, and stale-safety are explicit.
- V1 alignment: uses frozen binding-conflict semantics without expanding the identity model.
