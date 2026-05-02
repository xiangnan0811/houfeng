# Houfeng Node Onboarding and Binding-State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the first usable Node onboarding and binding-state workflow: create a node, issue or regenerate its enrollment token, enter a dedicated onboarding workspace, and resolve fingerprint binding conflicts from the web UI.

**Architecture:** Add the minimum backend persistence and admin APIs needed to make onboarding and binding conflict truthful and operable, then wire them into a dedicated onboarding page and node create flow. Keep the slice vertically coherent: this is about Node identity/access lifecycle, not general node operations.

**Tech Stack:** Go, PostgreSQL migrations, net/http, React, TypeScript, Vite, Vitest

---

## Planned file structure

### Backend schema and repository/admin state
- Create: `db/migrations/0004_add_node_onboarding_binding_state.sql`
- Modify: `internal/center/nodes/types.go`
- Modify: `internal/center/store/nodes.go`
- Modify: `internal/center/store/nodes_test.go`

### Backend HTTP/admin handlers and wiring
- Create: `internal/center/http/handlers/node_onboarding.go`
- Create: `internal/center/http/handlers/node_onboarding_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_api_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

### Frontend data layer, routes, and session token cache
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`
- Create: `web/src/lib/onboardingTokenCache.ts`
- Create: `web/src/lib/onboardingTokenCache.test.ts`
- Modify: `web/src/app/router.tsx`

### Frontend pages
- Modify: `web/src/pages/NodesPage.tsx`
- Create: `web/src/pages/NodesPage.test.tsx`
- Create: `web/src/pages/NodeOnboardingPage.tsx`
- Create: `web/src/pages/NodeOnboardingPage.test.tsx`

---

### Task 1: Add backend persistence and repository support for onboarding/binding state

**Files:**
- Create: `db/migrations/0004_add_node_onboarding_binding_state.sql`
- Modify: `internal/center/nodes/types.go`
- Modify: `internal/center/store/nodes.go`
- Modify: `internal/center/store/nodes_test.go`

- [ ] **Step 1: Write failing repository and migration-oriented tests**

Add tests that lock:
- pending binding metadata is persisted when a new fingerprint collides with an existing bound node
- confirm rebind clears pending metadata and moves pending fingerprint into the active binding
- reject pending clears pending metadata while preserving active binding
- reset binding clears both active and pending binding state
- onboarding read model derives the expected phase from node/runtime state

Run:
- `go test ./internal/center/store -run 'Test(NodeOnboarding|Binding)' -v`
Expected: FAIL because the schema/types/repo methods do not exist yet.

- [ ] **Step 2: Add the migration and node onboarding/binding metadata fields**

Migration should add only the minimum columns needed:
- `enrollment_token_issued_at timestamptz`
- `pending_binding_fingerprint text`
- `pending_binding_first_seen_at timestamptz`
- `pending_binding_last_seen_at timestamptz`
- `pending_binding_attempt_count integer not null default 0`

Update `internal/center/nodes/types.go` with:
- onboarding phase constants
- onboarding read-model type
- optional pending-binding metadata fields on the internal record if needed

- [ ] **Step 3: Extend `internal/center/store/nodes.go`**

Implement repository support for:
- issuing enrollment token while recording `enrollment_token_issued_at`
- reading onboarding state for one node
- confirming rebind
- rejecting pending fingerprint
- resetting binding

Also update the enrollment transition path so a conflicting fingerprint stores pending metadata instead of merely flipping `binding_status`.

- [ ] **Step 4: Re-run focused backend repository tests**

Run:
- `go test ./internal/center/store -run 'Test(NodeOnboarding|Binding)' -v`
Expected: PASS

- [ ] **Step 5: Commit the backend state slice**

Suggested Lore intent line:
- `Persist the minimum onboarding and binding-conflict state V1 needs`

---

### Task 2: Expose onboarding and binding admin APIs

**Files:**
- Create: `internal/center/http/handlers/node_onboarding.go`
- Create: `internal/center/http/handlers/node_onboarding_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_api_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] **Step 1: Write failing handler/router tests for onboarding and binding actions**

Add tests for:
- `GET /api/nodes/:id/onboarding`
- `POST /api/nodes/:id/enrollment-token`
- `POST /api/nodes/:id/binding/confirm-rebind`
- `POST /api/nodes/:id/binding/reject-pending`
- `POST /api/nodes/:id/binding/reset`
- API routes remain outside SPA fallback

Run:
- `go test ./internal/center/http/... -run 'Test(NodeOnboarding|Router)' -v`
Expected: FAIL because handlers/routes do not exist yet.

- [ ] **Step 2: Implement the handler surface**

Handler responsibilities:
- onboarding read endpoint returns the onboarding view model
- token issuance endpoint returns plaintext token + `issued_at`
- confirm/reject/reset endpoints return updated onboarding state or node summary
- method validation and `404` / `500` mapping follow repo conventions

- [ ] **Step 3: Wire router and bootstrap dependencies**

Wire the new handlers through:
- router options
- bootstrap dependency graph
- API router tests

- [ ] **Step 4: Re-run focused backend HTTP tests**

Run:
- `go test ./internal/center/http/... -run 'Test(NodeOnboarding|Router)' -v`
- `go test ./cmd/houfeng-center -run 'TestBootstrapCenterBuildsAppOnSuccess' -v`
Expected: PASS

- [ ] **Step 5: Commit the handler/API slice**

Suggested Lore intent line:
- `Make node onboarding and binding-state transitions operable over HTTP`

---

### Task 3: Add frontend onboarding data layer, route, and session token cache

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`
- Create: `web/src/lib/onboardingTokenCache.ts`
- Create: `web/src/lib/onboardingTokenCache.test.ts`
- Modify: `web/src/app/router.tsx`

- [ ] **Step 1: Write failing frontend data-layer/cache tests**

Add tests that lock:
- onboarding state fetch helper
- token issue/regenerate helper
- binding action helpers
- session-scoped token cache set/get/clear behavior

Run:
- `cd web && npm test -- --run api onboardingTokenCache`
Expected: FAIL because these types/helpers/cache do not exist yet.

- [ ] **Step 2: Extend `web/src/lib/types.ts`**

Add client types for:
- `NodeOnboardingState`
- onboarding phase enum/string union
- token issuance response
- pending binding metadata

- [ ] **Step 3: Extend `web/src/lib/api.ts`**

Add helpers:
- `getNodeOnboarding(nodeId)`
- `issueNodeEnrollmentToken(nodeId)`
- `confirmNodeRebind(nodeId)`
- `rejectPendingNodeBinding(nodeId)`
- `resetNodeBinding(nodeId)`

Keep query/body handling minimal and consistent with existing helpers.

- [ ] **Step 4: Add session token cache helper**

Create `web/src/lib/onboardingTokenCache.ts` to store the plaintext enrollment token and issued time per node in session storage. This is the bridge that lets the onboarding page remain useful after immediate redirect without pretending the backend can re-read plaintext.

- [ ] **Step 5: Add onboarding route**

Add route:
- `/nodes/:nodeId/onboarding`

Do not wire UI yet; just make the route available for Task 4.

- [ ] **Step 6: Re-run focused frontend tests**

Run:
- `cd web && npm test -- --run api onboardingTokenCache`
Expected: PASS

- [ ] **Step 7: Commit the frontend data/cache slice**

Suggested Lore intent line:
- `Give the web app typed access to onboarding and binding-state APIs`

---

### Task 4: Implement node create flow and onboarding workspace

**Files:**
- Modify: `web/src/pages/NodesPage.tsx`
- Create: `web/src/pages/NodesPage.test.tsx`
- Create: `web/src/pages/NodeOnboardingPage.tsx`
- Create: `web/src/pages/NodeOnboardingPage.test.tsx`

- [ ] **Step 1: Write failing page tests**

Nodes page tests should lock:
- create action is visible
- successful create issues token and navigates to onboarding route
- token gets cached in session storage
- create errors stay local to the page

Onboarding page tests should lock:
- unbound state with token + steps
- bound-but-awaiting-first-sample state
- fully connected state with CTA back to detail page
- token-missing fallback message that asks for regeneration instead of pretending plaintext is still available

Run:
- `cd web && npm test -- --run NodesPage NodeOnboardingPage`
Expected: FAIL because the pages do not implement these flows yet.

- [ ] **Step 2: Extend `NodesPage` with create-node flow**

Implement a minimal create form/drawer using existing page vocabulary.
On success:
1. create node
2. issue token
3. cache plaintext token in session helper
4. navigate to `/nodes/:id/onboarding`

- [ ] **Step 3: Implement `NodeOnboardingPage`**

Render:
- node identity card
- token display card
- installation steps
- state feedback card
- CTA to node detail when onboarding is complete

Keep the page honest:
- if no cached plaintext token exists, say it cannot be redisplayed and offer regeneration
- do not invent token expiry countdowns or hidden backend state that does not exist

- [ ] **Step 4: Re-run focused page tests**

Run:
- `cd web && npm test -- --run NodesPage NodeOnboardingPage`
Expected: PASS

- [ ] **Step 5: Commit the onboarding page slice**

Suggested Lore intent line:
- `Turn node creation into a real onboarding workflow`

---

### Task 5: Add binding conflict UI and complete verification

**Files:**
- Modify: `web/src/pages/NodeOnboardingPage.tsx`
- Modify: `web/src/pages/NodeOnboardingPage.test.tsx`

- [ ] **Step 1: Write failing binding-conflict tests**

Add tests that lock:
- conflict state renders a high-priority conflict card
- masked current vs pending fingerprint summaries render
- confirm/reject/reset actions call the right APIs
- action failures stay local to the conflict section
- successful actions refresh onboarding state

Run:
- `cd web && npm test -- --run NodeOnboardingPage`
Expected: FAIL because the conflict UI/actions do not exist yet.

- [ ] **Step 2: Implement conflict card and actions**

Add a high-priority card inside onboarding page for `指纹变更待确认` with:
- current/pending fingerprint summaries
- first seen / last seen / attempt count
- action buttons:
  - confirm rebind
  - reject fingerprint
  - reset binding

Use existing button/card hierarchy and isolate high-risk actions clearly.

- [ ] **Step 3: Re-run focused page tests**

Run:
- `cd web && npm test -- --run NodeOnboardingPage`
Expected: PASS

- [ ] **Step 4: Run full verification**

Run:
- `go test ./internal/center/store -v`
- `go test ./internal/center/http/... -v`
- `go test ./internal/center/... -v`
- `go test ./...`
- `cd web && npm run lint && npm test -- --run && npm run build`
- `./scripts/verify.sh`
Expected: PASS

- [ ] **Step 5: Commit the binding-conflict and verification slice**

Suggested Lore intent line:
- `Make binding conflicts resolvable from the onboarding workspace`

---

## Self-review

### Spec coverage
- Covers node creation handoff into onboarding
- Covers token issuance/regeneration
- Covers onboarding read model and supported phases
- Covers binding conflict operator actions
- Preserves honesty around one-time token visibility and opaque fingerprint data

### Placeholder scan
- No TBD/TODO placeholders remain
- Each task names exact files, responsibilities, and verification commands

### Type consistency
- Backend and frontend both pivot around an explicit onboarding read model
- Token issuance remains a one-time response, with session cache as the UI bridge
- Binding admin actions remain node-scoped and do not leak into unrelated control surfaces
