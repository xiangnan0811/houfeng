# Houfeng Settings Execution Integration Design

## Context

The Settings/global-control slice is mostly implemented, but final review found a truthfulness gap: several saved settings are still not consumed by the live runtime paths they appear to control.

The gap is concentrated in three places:
1. Telegram settings are persisted but do not drive the live notifier.
2. Host-sample/probe default frequency settings and small override rules are persisted but not consumed by sync-plan generation.
3. Incident default settings are persisted but not consumed by incident evaluation configuration.

This follow-up slice closes the “stored but not effective” gap for the settings that should be operative now, while keeping any remaining not-yet-executed settings explicitly labeled as policy.

## Recommendation

Implement a narrow execution-integration slice that does three things:
- make persisted Telegram settings drive notification delivery at runtime
- make persisted host-sample defaults and narrow override rules affect sync-plan generation
- make persisted incident defaults drive incident-service timing behavior

Do **not** broaden into a generic settings engine or a retention executor.

## Scope decision

### Approach A — Wire everything, including retention executors
Reject. Too broad and drifts into a new subsystem.

### Approach B — Wire only the settings that already have natural consumers (recommended)
Accept. Telegram already has a notifier path, agent plan already has a cadence-resolution path, and incident service already takes timing parameters. These are the cleanest execution hookups.

### Approach C — Leave everything stored-only and only change page copy
Reject. That keeps the truthfulness problem only cosmetically reduced, not structurally fixed.

## In scope

1. Persisted Telegram settings become the source of truth for live notifier behavior.
2. `runtime_apply_active` becomes truthful.
3. Agent-plan generation uses persisted host-sample frequency defaults.
4. Agent-plan generation applies the narrow override rules for host sample/probe frequency where supported.
5. Incident service timing uses persisted incident defaults.
6. Settings page copy is tightened so any still-unapplied sections are explicitly labeled as policy only.

## Out of scope

- retention executors / pruning jobs
- generic override rule language
- per-object rule editors
- fully dynamic reconfiguration for every long-lived object if a simpler read-on-use path suffices

## Execution semantics

### 1. Telegram notifier
The live notifier should consult persisted settings, not just bootstrap env.

Preferred design:
- keep env as bootstrap seed/fallback only if needed
- add a small settings-aware notifier wrapper that reads current settings at send time
- if persisted Telegram is disabled, treat it as notifier-disabled rather than send-failure
- `runtime_apply_active` should reflect that the runtime path actually reads persisted settings

### 2. Host sample frequency defaults
These can be made live cleanly.

Plan generation should resolve host sample cadence as:
1. global persisted `host_sample_frequency_tier`
2. overridden by matching node-label override if present

This replaces the current hardcoded label special case.

### 3. Probe frequency defaults and narrow overrides
This area has to stay truthful because probe items already store `frequency_tier`.

To avoid inventing a new precedence model, use this rule:
- explicit stored `probe_items.frequency_tier` remains the base
- matching target-type / target-label overrides may override it for plan generation
- the plain global `probe_frequency_defaults` section is policy-only unless and until we define how it should interact with explicit per-probe tiers

That means this slice makes **override rules operative**, while the global probe-defaults section may still remain policy-only and should be labeled that way in the page.

### 4. Incident defaults
The incident service already accepts:
- heartbeat interval
- sweep interval

Use persisted settings for these values instead of hardcoded/env constructor defaults where possible.

For now, heartbeat/stale-threshold timing should be sourced from persisted settings in a way that preserves the existing evaluator semantics.

## Settings page truthfulness after this slice

After this integration:
- Telegram section should be labeled as live/applied
- host sample default + supported override behavior should be labeled as live
- incident defaults should be labeled as live
- any still-unapplied policy-only section must say so explicitly
  - likely `retention_policy`
  - possibly global `probe_frequency_defaults` if left as future/default policy only

## Backend shape

### New helper/service layer
A small integration layer is acceptable if it avoids overloading unrelated repositories.

Likely pieces:
- settings-aware notifier wrapper
- settings-aware sync-plan builder / repository adapter
- settings-aware incident-service defaults source

## Testing strategy

### Backend
- notifier behavior with persisted settings enabled/disabled
- sync-plan cadence resolution from settings + overrides
- incident-service constructor/use path honoring persisted defaults
- bootstrap wiring tests proving live paths consult settings repo

### Frontend
- focused settings-page truthfulness copy updates if any text changes
- no new page slice beyond copy adjustments unless required by the integration

## Expected outcome

After this slice, the Settings page will no longer be mostly a stored-policy surface. The key controls that users reasonably expect to be active—Telegram delivery, host-sample cadence, supported frequency overrides, and incident timing defaults—will actually affect live system behavior.
