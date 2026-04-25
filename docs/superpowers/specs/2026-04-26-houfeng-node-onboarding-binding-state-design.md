# Houfeng V1 Node Onboarding and Binding-State Design

## Context

The previous V1 slice completed the observability surfaces: dashboard, events, and node/target detail pages now show real incidents and event history. The next largest frozen-V1 gap is the Node access lifecycle:
- nodes can be created in the backend,
- agents can enroll and trigger binding conflicts,
- but the product still lacks the actual onboarding workspace and operator controls needed to complete or resolve that lifecycle.

This slice implements the first usable Node onboarding and binding-state administration surface.

## Why this slice now

Houfeng already supports:
- node creation
- enrollment token issuance in the service/repository layer
- agent enrollment
- automatic transition to `指纹变更待确认`

But none of that is visible or operable from the web UI yet. That makes the V1 node lifecycle incomplete even though the backend primitives already exist.

## Scope decomposition decision

This area could be split two ways:

### Approach A — onboarding page only
Build a create-node flow and onboarding page, but defer binding-conflict handling.

**Pros**
- smaller first slice
- lower backend change surface

**Cons**
- the shared baseline screen is specifically about onboarding **and** binding conflict
- backend already produces `指纹变更待确认`, but the product would still have no operator path to resolve it
- likely leaves a visible half-finished state in V1

### Approach B — full onboarding + binding-state admin slice (recommended)
Build the Node create → onboarding path, token issuance/regeneration, onboarding-state read model, and binding conflict resolution actions in one vertical slice.

**Pros**
- closes the whole “Node enters system” loop
- makes existing backend binding semantics operable
- matches the frozen baseline screen and interaction docs more closely

**Cons**
- broader than a pure page slice
- requires small schema/store/API additions

### Approach C — backend-only binding admin APIs first
Add token/binding action APIs now and leave the UI for later.

**Pros**
- backend-first and easy to validate

**Cons**
- low user-visible value
- keeps the frozen onboarding/binding pages missing

## Recommendation

Use **Approach B**.

The onboarding and binding-conflict surfaces are one coherent product area: they both represent the same object identity workflow. Doing them together yields a usable V1 slice instead of a partial one.

## Constraints

- V1 product, interaction, and visual baseline remain frozen.
- Do not reopen lifecycle or binding semantics.
- Reuse the current app shell and the `Node Onboarding & Binding Conflict (Unified)` baseline as the primary UI anchor.
- No new dependencies.
- Keep implementation compatible with the current backend truth, even where the original early design sketches were richer than the actual current data model.

## Important implementation adaptations to current project reality

### 1. Enrollment token visibility
Current backend stores only `enrollment_token_hash`, not the plaintext token. That means the system cannot “re-read” a previously issued token.

So this slice will use the following V1-safe behavior:
- create or regenerate token via admin API
- return plaintext token exactly once in the API response
- frontend stores that token in session-scoped client state (e.g. session storage) for the onboarding page
- if the page is reopened later without cached plaintext, show a clear message that the current token cannot be re-displayed and must be regenerated

This preserves a secure backend shape while still making the onboarding page usable.

### 2. Binding conflict comparison detail
Current agent only submits an opaque fingerprint hash. It does **not** send decomposed hardware identity fields such as MAC / board serial / OS UUID.

So the binding conflict UI in this slice will show:
- current bound fingerprint summary (masked hash)
- pending fingerprint summary (masked hash)
- first seen / last seen / attempt count
- operator actions: confirm rebind / reject pending / reset binding

It will **not** fabricate a richer hardware-comparison card that the current backend cannot support truthfully.

## In scope

1. Node create flow in the web UI
2. Redirect from node creation into a dedicated onboarding workspace
3. Admin API to issue/regenerate enrollment token
4. Backend read model for node onboarding/binding state
5. Web onboarding page for the four main access-lifecycle states the current backend can support
6. Binding conflict callouts and action buttons
7. Admin APIs to:
   - confirm rebind
   - reject pending fingerprint
   - reset binding
8. Event generation for binding-state admin actions when appropriate

## Out of scope

- maintenance/pause/resume controls
- lifecycle-status editing beyond create-time defaults
- node deletion/retirement flows
- target-side actions
- richer hardware-fingerprint comparison than the backend can support
- token expiry semantics / countdown timers
- agent installation automation beyond showing instructions

## Chosen backend shape

### New onboarding read model
Add a read-only onboarding state payload for one node.

Suggested endpoint:
- `GET /api/nodes/:id/onboarding`

Suggested response includes:
- node summary fields already shown in `nodes.Record`
- `binding_status`
- `current_health_status`
- `monitoring_status`
- `last_heartbeat_at`
- `last_sync_at`
- whether any host sample exists yet
- whether any accepted observation exists yet
- onboarding phase derived server-side
- pending binding metadata if present:
  - `pending_fingerprint`
  - `pending_first_seen_at`
  - `pending_last_seen_at`
  - `pending_attempt_count`
- token issuance metadata if present:
  - `enrollment_token_issued_at`

### New admin action endpoints
- `POST /api/nodes/:id/enrollment-token`
  - issues/regenerates token
  - returns plaintext token and issued timestamp
- `POST /api/nodes/:id/binding/confirm-rebind`
- `POST /api/nodes/:id/binding/reject-pending`
- `POST /api/nodes/:id/binding/reset`

### Minimal persistence additions
The current schema is missing the metadata needed to make binding conflict operable. This slice should add only the minimum columns required:
- `enrollment_token_issued_at`
- `pending_binding_fingerprint`
- `pending_binding_first_seen_at`
- `pending_binding_last_seen_at`
- `pending_binding_attempt_count`

### Binding action semantics
- **Confirm rebind**
  - move `pending_binding_fingerprint` into the active bound fingerprint
  - set `binding_status = 已绑定`
  - clear pending fields
  - rotate sync token
- **Reject pending**
  - keep current bound fingerprint unchanged
  - set `binding_status = 已绑定`
  - clear pending fields
- **Reset binding**
  - clear bound fingerprint
  - clear pending fields
  - clear sync token hash
  - set `binding_status = 未绑定`

## Onboarding phase model

This slice will render one of four operator-facing states:

1. **未开始接入**
   - node exists
   - binding status = 未绑定
   - no accepted heartbeat/sample yet
2. **已绑定，等待稳定观测**
   - binding status = 已绑定
   - has binding but no host sample yet
3. **接入完成**
   - binding status = 已绑定
   - has heartbeat and at least one accepted runtime fact
4. **绑定冲突待处理**
   - binding status = 指纹变更待确认
   - pending fingerprint metadata present

Note: the original broader “接入尝试中 / 最近失败摘要” concept is only partially supportable with the current backend. This slice does not invent nonexistent failure telemetry.

## Chosen frontend shape

### Nodes page
Add a primary create action and a compact create form/drawer. On successful creation:
1. create node
2. issue enrollment token immediately
3. navigate to `/nodes/:id/onboarding`
4. place plaintext token + issued timestamp into session-scoped browser storage for that node

### Onboarding page
Add route:
- `/nodes/:nodeId/onboarding`

The page should include:
- node identity card
- token display section
- installation steps section
- state feedback section
- conflict card + action bar when `binding_status = 指纹变更待确认`

### Detail page interaction
Node detail should remain the long-term operational page. The onboarding page is a temporary stateful workspace for establishing trust and first contact.

A bound and stable node can keep a clear CTA back to `/nodes/:id`.

## Error handling

- Node create failure should keep the create form open with inline error copy.
- Token generation failure after node creation should surface a recoverable onboarding error, not silently drop the operator on the list page.
- Binding action failures should stay local to the conflict card and preserve page visibility.
- Onboarding page should remain visible even if one secondary section fails.

## Testing strategy

### Backend
- migration tests / repository tests for new columns and transitions
- handler tests for token issuance, onboarding read payload, and binding actions
- router tests for the new onboarding/admin endpoints staying outside SPA fallback

### Frontend
- Nodes page create flow tests
- onboarding page tests per main onboarding state
- binding conflict action tests
- session token persistence tests for post-create onboarding visibility

## Expected outcome

After this slice, Houfeng should support the first complete Node access loop:
- create node
- get onboarding token
- land on onboarding workspace
- see whether the node is still unbound, bound-but-not-stable, fully connected, or in conflict
- resolve a binding conflict without leaving the product

That will make the frozen onboarding/binding baseline operational instead of decorative.
