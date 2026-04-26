# Houfeng Settings and Global Control Surfaces Design

## Context

The runtime-control slice is underway / near completion, but the Settings page is still a placeholder. The frozen V1 design expects a real settings/global-control surface that is lighter than the observability surfaces while still exposing the minimum centralized controls:
- Telegram notification settings
- frequency tiers
- global default rules
- a small amount of override capability
- data retention strategy

The key constraint is truthfulness: this slice must not present controls as “live” if the repo has no real persistence or execution path behind them.

## Recommendation

Implement the settings slice as a **real configuration surface backed by explicit persistence**, but only for controls that the current project can support truthfully. Do not fabricate a full generic rules engine or a live retention executor.

## Scope decision

### Approach A — Full settings illusion
Make the Settings page look complete with toggles/forms for everything, even where there is no backend model yet.

**Reject.** This would make the product look ahead of the implementation and violate the current repo’s truthfulness standard.

### Approach B — Real persisted settings for minimal V1 controls (recommended)
Add a small persisted center-settings model and expose only settings that have a truthful data model:
- Telegram settings
- frequency tiers
- global runtime/incident defaults
- small override rules (node label / target type / target label)
- retention strategy as persisted policy metadata, explicitly marked as policy/config if no executor exists yet

**Accept.** This gives the V1 settings page real value while staying honest about what the system actually enforces.

### Approach C — Read-only settings summary
Show config but do not allow edits yet.

**Reject.** The frozen V1 settings page is intended as an operator surface, not just a documentation mirror.

## Constraints

- V1 design/visual/interaction baseline remains frozen.
- Settings must remain lighter than the observability surfaces.
- Do not introduce a generic expression-based rules engine.
- Do not add a fake retention executor if the backend has no execution loop for it yet.
- No new dependencies.

## In scope

### Persisted center settings model
Introduce a single persisted settings object for V1.

Suggested sections:
1. **Telegram settings**
   - enabled/disabled (derived from token+chat presence)
   - bot token
   - chat id
2. **Frequency tiers**
   - host sample default tier
   - probe default tiers by kind
3. **Global incident defaults**
   - heartbeat interval / stale threshold policy
   - sweep interval
   - notification enablement flags by event kind if needed
4. **Small override rules**
   - node-label overrides
   - target-type overrides
   - target-label overrides
5. **Retention strategy policy**
   - raw layer policy
   - aggregate layer policy
   - event/notification layer policy
   - clear UI note when retention execution is not yet automated

### Settings page
Replace the placeholder Settings page with a real page containing those sections.

### Backend APIs
Add a small `GET/PUT` settings API for the persisted model.

## Out of scope

- Generic rule DSL
- arbitrary per-object overrides
- runtime preview/simulation engine
- secret-management abstraction beyond current env/database model
- retention jobs that prune live data automatically
- multi-user or role-based settings access

## Truthfulness decisions

### Telegram settings
The system already has real Telegram config plumbing, so this should be fully editable/persisted.

### Frequency tiers
These should be real persisted defaults and used by planning logic where applicable.

### Rule overrides
Keep them structured and narrow. Example override dimensions only:
- node label
- target type
- target label

### Retention policy
Persist the policy, but if there is not yet a background executor that applies it, the UI must explicitly say:
- policy is stored
- automatic cleanup/rollup is not yet active (if that remains true when implemented)

That keeps the surface honest while still allowing V1 configuration work to start.

## Backend shape

### Persistence
Add a small store-backed settings model.
A single-row table or settings document is acceptable for V1.

Suggested backend representation:
- `center_settings`
  - `settings_id`
  - `telegram_bot_token`
  - `telegram_chat_id`
  - `host_sample_frequency_tier`
  - `probe_frequency_defaults` (jsonb)
  - `incident_defaults` (jsonb)
  - `override_rules` (jsonb)
  - `retention_policy` (jsonb)
  - timestamps

### API
- `GET /api/settings`
- `PUT /api/settings`

Validation rules should stay simple and explicit, not dynamic.

## Frontend shape

### Settings page sections
1. **通知 / Telegram**
2. **默认频率档位**
3. **全局默认规则**
4. **少量覆盖规则**
5. **数据保留策略**

### Interaction style
- sectioned page, not wizard
- inline save / section save is acceptable
- destructive or high-risk fields should be visually separated
- secrets should not be overexposed; token masking or “replace current value” behavior is acceptable

## Testing strategy

### Backend
- store tests for settings read/write and validation
- handler tests for GET/PUT and invalid payloads
- router/bootstrap wiring tests

### Frontend
- Settings page load/render
- section save behavior
- validation / error states
- retention-policy truthfulness copy when executor is absent

## Expected outcome

After this slice, Houfeng will have a real Settings/global-control surface instead of a placeholder. Operators will be able to manage the centralized controls that V1 actually depends on, while the UI remains honest about which policies are already enforced and which are only configured so far.
