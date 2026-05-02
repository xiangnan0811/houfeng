# Houfeng Settings and Global Control Surfaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the placeholder Settings page with a real persisted global-control surface for Telegram settings, frequency defaults, global incident defaults, small override rules, and retention policy metadata.

**Architecture:** Add a minimal persisted `center_settings` model with explicit validation and narrow structured fields, expose it through `GET/PUT /api/settings`, then wire a sectioned Settings page on top. Keep the slice truthful: settings that are persisted but not yet executed automatically must be labeled as policy/config rather than live behavior.

**Tech Stack:** Go, PostgreSQL, net/http, React, TypeScript, Vite, Vitest

---

## Planned file structure

### Backend persistence and validation
- Create: `db/migrations/0006_add_center_settings.sql`
- Create: `internal/center/settings/types.go`
- Create: `internal/center/settings/types_test.go`
- Create: `internal/center/store/settings.go`
- Create: `internal/center/store/settings_test.go`

### Backend HTTP API and wiring
- Create: `internal/center/http/handlers/settings.go`
- Create: `internal/center/http/handlers/settings_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_api_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

### Frontend data layer
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`

### Frontend page
- Modify: `web/src/pages/SettingsPage.tsx`
- Create: `web/src/pages/SettingsPage.test.tsx`

---

### Task 1: Add persisted center-settings model and validation

**Files:**
- Create: `db/migrations/0006_add_center_settings.sql`
- Create: `internal/center/settings/types.go`
- Create: `internal/center/settings/types_test.go`
- Create: `internal/center/store/settings.go`
- Create: `internal/center/store/settings_test.go`

- [ ] **Step 1: Write failing settings validation and store tests**

Add tests that lock:
- valid settings round-trip
- Telegram settings require token+chat pair together
- frequency defaults accept only known tiers
- override rules stay within the narrow dimensions (node label / target type / target label)
- retention policy persists structured metadata

Run:
- `go test ./internal/center/settings ./internal/center/store -run 'Test(Settings|CenterSettings)' -v`
Expected: FAIL because the types/store do not exist yet.

- [ ] **Step 2: Add migration and settings types**

Create `center_settings` with explicit columns / jsonb fields:
- `settings_id`
- `telegram_bot_token`
- `telegram_chat_id`
- `host_sample_frequency_tier`
- `probe_frequency_defaults`
- `incident_defaults`
- `override_rules`
- `retention_policy`
- timestamps

Create typed settings model and validation helpers in `internal/center/settings/types.go`.

- [ ] **Step 3: Add store read/write repository**

Implement `internal/center/store/settings.go` with:
- `GetSettings(ctx)`
- `PutSettings(ctx, input)`

Behavior:
- return one persisted settings document
- create initial row lazily if absent, or use a fixed singleton row pattern
- validate before persisting

- [ ] **Step 4: Re-run focused backend model/store tests**

Run:
- `go test ./internal/center/settings ./internal/center/store -run 'Test(Settings|CenterSettings)' -v`
Expected: PASS

- [ ] **Step 5: Commit the settings persistence slice**

Suggested Lore intent line:
- `Persist the minimum global-control settings V1 actually uses`

---

### Task 2: Expose settings HTTP API

**Files:**
- Create: `internal/center/http/handlers/settings.go`
- Create: `internal/center/http/handlers/settings_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_api_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] **Step 1: Write failing handler/router tests for settings API**

Add tests for:
- `GET /api/settings`
- `PUT /api/settings`
- invalid payload returns `400`
- settings route stays outside SPA fallback

Run:
- `go test ./internal/center/http/... -run 'Test(Settings|Router)' -v`
Expected: FAIL because the handler and wiring do not exist yet.

- [ ] **Step 2: Implement GET/PUT settings handler**

Handler behavior:
- `GET` returns current settings
- `PUT` decodes JSON strictly, validates, persists, returns updated settings
- validation failures map to `400`
- repository failures map to `500`

- [ ] **Step 3: Wire router and bootstrap**

Add settings handler support to:
- router options
- router registration
- bootstrap dependency graph
- bootstrap tests

- [ ] **Step 4: Re-run focused backend HTTP tests**

Run:
- `go test ./internal/center/http/... -run 'Test(Settings|Router)' -v`
- `go test ./cmd/houfeng-center -run 'TestBootstrapCenterBuildsAppOnSuccess' -v`
Expected: PASS

- [ ] **Step 5: Commit the settings API slice**

Suggested Lore intent line:
- `Expose persisted global-control settings over HTTP`

---

### Task 3: Add frontend settings data layer

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`

- [ ] **Step 1: Write failing frontend API helper tests**

Add tests that lock:
- `getSettings()`
- `updateSettings()`
- proper payload/response handling
- invalid response / error pass-through via `ApiError`

Run:
- `cd web && npm test -- --run api`
Expected: FAIL because the helpers/types do not exist yet.

- [ ] **Step 2: Extend frontend settings types**

Add typed frontend models matching the new settings payload:
- Telegram settings
- frequency defaults
- incident defaults
- override rules
- retention policy

- [ ] **Step 3: Add settings API helpers**

Implement in `web/src/lib/api.ts`:
- `getSettings()`
- `updateSettings(settings)`

- [ ] **Step 4: Re-run focused frontend data-layer tests**

Run:
- `cd web && npm test -- --run api`
Expected: PASS

- [ ] **Step 5: Commit the frontend settings data slice**

Suggested Lore intent line:
- `Give the web app typed access to global control settings`

---

### Task 4: Replace the placeholder Settings page with a real settings surface

**Files:**
- Modify: `web/src/pages/SettingsPage.tsx`
- Create: `web/src/pages/SettingsPage.test.tsx`

- [ ] **Step 1: Write failing page tests**

Settings page tests should lock:
- page loads current settings
- Telegram section renders persisted values
- frequency/default rule sections render
- save updates settings
- invalid save shows inline error
- retention policy copy truthfully indicates policy/config semantics if executor is not active

Run:
- `cd web && npm test -- --run SettingsPage`
Expected: FAIL because the page is still a placeholder.

- [ ] **Step 2: Implement the Settings page**

Add a sectioned page with:
1. Telegram 通知设置
2. 默认频率档位
3. 全局默认规则
4. 少量覆盖规则
5. 数据保留策略

Constraints:
- reuse existing page vocabulary
- avoid pretending the retention executor already runs if that is not yet true
- keep it lighter than observability pages

- [ ] **Step 3: Re-run focused page tests**

Run:
- `cd web && npm test -- --run SettingsPage`
Expected: PASS

- [ ] **Step 4: Commit the Settings UI slice**

Suggested Lore intent line:
- `Turn settings into a real global-control surface`

---

### Task 5: Full verification and final review

**Files:**
- No new planned files; use verification only unless previous tasks require a tiny cleanup.

- [ ] **Step 1: Run backend verification**

Run:
- `go test ./internal/center/store -v`
- `go test ./internal/center/http/... -v`
- `go test ./internal/center/... -v`
- `go test ./...`
Expected: PASS

- [ ] **Step 2: Run frontend verification**

Run:
- `cd web && npm run lint && npm test -- --run && npm run build`
Expected: PASS

- [ ] **Step 3: Run repository verify script**

Run:
- `./scripts/verify.sh`
Expected: PASS

- [ ] **Step 4: Commit only if a final minimal cleanup was required**

Only use this if the verification loop required one small final correction.

---

## Self-review

### Spec coverage
- Covers Telegram settings, frequency defaults, global incident defaults, small override rules, and retention policy metadata
- Adds real persistence and a real Settings page rather than another placeholder
- Keeps truthfulness around policy-vs-execution where the backend may not yet enforce retention automatically

### Placeholder scan
- No TBD/TODO placeholders remain
- Each task names concrete files, responsibilities, and commands

### Type consistency
- Backend and frontend share the same explicit settings model shape
- GET/PUT API is simple and singleton-shaped
- Settings remain structured and narrow rather than generic-rule based
