# Houfeng Settings Execution Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the key saved settings actually affect live behavior by wiring persisted Telegram settings, host-sample cadence defaults, supported frequency overrides, and incident timing defaults into runtime execution paths.

**Architecture:** Keep the integration narrow and explicit. Add a settings-aware notifier wrapper for live Telegram behavior, a settings-aware sync-plan resolution path for host-sample cadence and supported overrides, and a settings-backed incident-service configuration path for timing defaults. Leave retention policy as persisted policy only.

**Tech Stack:** Go, PostgreSQL, net/http, existing store/repository paths, React copy-only touchups when needed

---

## Planned file structure

### Backend notifier/runtime integration
- Modify: `internal/center/incidents/service.go`
- Modify: `internal/center/incidents/service_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

### Backend settings-aware planning integration
- Modify: `internal/center/store/agent_plan.go`
- Modify: `internal/center/store/agent_plan_test.go`
- Modify: `internal/center/store/settings.go` (only if helper queries are needed)

### Frontend settings truthfulness copy adjustments
- Modify: `web/src/pages/SettingsPage.tsx`
- Modify: `web/src/pages/SettingsPage.test.tsx`

---

### Task 1: Wire persisted Telegram settings into live notifier behavior

**Files:**
- Modify: `internal/center/incidents/service.go`
- Modify: `internal/center/incidents/service_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] **Step 1: Write failing backend tests for settings-aware notification behavior**

Add tests that lock:
- persisted Telegram disabled => notification remains suppressed, not failed
- persisted Telegram enabled => send uses persisted token/chat config
- runtime_apply_active can truthfully become true once the notifier path reads persisted settings

Run:
- `go test ./internal/center/incidents -run 'Test(SettingsAware|Notification)' -v`
Expected: FAIL because notifier/runtime still use bootstrap env only.

- [ ] **Step 2: Add a settings-aware notifier path**

Implement a narrow integration so notifier behavior reads current persisted settings when sending. Keep the disabled case distinct from real send failure so notification records stay truthful.

- [ ] **Step 3: Rewire bootstrap to use the settings-aware notifier**

Update bootstrap to pass the settings-aware notifier into incident service instead of the static env-only notifier.

- [ ] **Step 4: Re-run focused notification integration tests**

Run:
- `go test ./internal/center/incidents -run 'Test(SettingsAware|Notification)' -v`
- `go test ./cmd/houfeng-center -run 'TestBootstrapCenterBuildsAppOnSuccess' -v`
Expected: PASS

- [ ] **Step 5: Commit the notifier integration slice**

Suggested Lore intent line:
- `Make persisted Telegram settings drive live notification delivery`

---

### Task 2: Wire persisted host-sample cadence and supported overrides into sync-plan generation

**Files:**
- Modify: `internal/center/store/agent_plan.go`
- Modify: `internal/center/store/agent_plan_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go` (only if constructor/wiring changes are needed)

- [ ] **Step 1: Write failing sync-plan tests for settings-backed cadence resolution**

Add tests that lock:
- host sample cadence comes from persisted `host_sample_frequency_tier`
- matching node-label override can change host sample cadence
- matching target-type or target-label override can change assignment frequency tier
- explicit unsupported precedence cases are kept out of scope and documented in test naming

Run:
- `go test ./internal/center/store -run 'TestBuildSyncPlan(UsesPersistedSettings|AppliesSettingsOverrides)' -v`
Expected: FAIL because the plan builder still uses hardcoded cadence/defaults.

- [ ] **Step 2: Implement settings-aware sync-plan resolution**

Apply these precedence rules explicitly:
1. host sample cadence: settings default -> matching node-label override
2. probe assignment cadence: existing probe_item tier as base -> matching target-type/target-label override can override

Do not invent a generic precedence engine.

- [ ] **Step 3: Re-run focused sync-plan tests**

Run:
- `go test ./internal/center/store -run 'TestBuildSyncPlan(UsesPersistedSettings|AppliesSettingsOverrides)' -v`
Expected: PASS

- [ ] **Step 4: Commit the sync-plan integration slice**

Suggested Lore intent line:
- `Make saved cadence controls affect sync-plan generation`

---

### Task 3: Wire persisted incident defaults into incident-service timing

**Files:**
- Modify: `internal/center/incidents/service.go`
- Modify: `internal/center/incidents/service_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`

- [ ] **Step 1: Write failing incident-service tests for settings-backed timing**

Add tests that lock:
- heartbeat interval comes from persisted settings
- sweep interval comes from persisted settings
- defaults still fall back safely when settings are absent/unavailable

Run:
- `go test ./internal/center/incidents -run 'Test(SettingsBacked|IncidentTiming)' -v`
Expected: FAIL because incident service still uses fixed/env constructor values.

- [ ] **Step 2: Implement settings-backed incident timing resolution**

Source incident-service timing from persisted settings with a clear fallback path. Keep the logic explicit and easy to reason about.

- [ ] **Step 3: Re-run focused incident timing tests**

Run:
- `go test ./internal/center/incidents -run 'Test(SettingsBacked|IncidentTiming)' -v`
Expected: PASS

- [ ] **Step 4: Commit the incident-timing integration slice**

Suggested Lore intent line:
- `Make saved incident defaults affect live timing behavior`

---

### Task 4: Tighten Settings page truthfulness copy for any still policy-only sections

**Files:**
- Modify: `web/src/pages/SettingsPage.tsx`
- Modify: `web/src/pages/SettingsPage.test.tsx`

- [ ] **Step 1: Write failing truthfulness-copy tests if the current page copy becomes stale**

Add or adjust tests to lock the final semantics after integration:
- Telegram now indicates live runtime usage when appropriate
- host-sample cadence / supported overrides no longer claim stored-only if now active
- retention policy remains policy-only if no executor exists
- global probe defaults stay labeled correctly if still not fully active

Run:
- `cd web && npm test -- --run SettingsPage`
Expected: FAIL if current copy no longer matches actual behavior.

- [ ] **Step 2: Update the page copy only where behavior changed**

Keep the copy honest and minimal.

- [ ] **Step 3: Re-run focused Settings page tests**

Run:
- `cd web && npm test -- --run SettingsPage`
Expected: PASS

- [ ] **Step 4: Commit the truthfulness-copy cleanup**

Suggested Lore intent line:
- `Keep settings copy aligned with live execution behavior`

---

### Task 5: Full verification and final review

**Files:**
- No new planned files; verification only unless a tiny final cleanup is required.

- [ ] **Step 1: Run backend verification**

Run:
- `go test ./internal/center/store -v`
- `go test ./internal/center/http/... -v`
- `go test ./internal/center/incidents -v`
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

- [ ] **Step 4: Commit only if a final minimal cleanup is required**

Only use this if verification uncovers one small follow-up fix.

---

## Self-review

### Spec coverage
- Covers persisted Telegram settings → live notifier
- Covers host-sample cadence and supported frequency overrides → sync plan
- Covers incident defaults → incident service timing
- Preserves retention policy as policy-only if executor remains absent

### Placeholder scan
- No TBD/TODO placeholders remain
- Each task names concrete files, behaviors, and commands

### Type consistency
- Integration stays explicit: settings repo feeds notifier, plan, and incident timing through clear narrow paths
- Does not invent a generic settings engine
- Copy changes are tied to actual live behavior changes only
