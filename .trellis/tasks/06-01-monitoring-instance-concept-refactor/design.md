# VPS / Monitoring Instance Concept Refactor Design

## Architecture

The refactor preserves the product split between Asset Ledger and Observability, but renames the former Node concept to Monitoring Instance everywhere it is a product/domain concept.

- Asset Ledger: `VPS` remains the purchased server asset, including provider, location, access, lifecycle, subscription/cost, services, domains, and asset decisions.
- Observability: `MonitoringInstance` represents the agent-bound runtime observation identity for a server. It owns enrollment, sync credentials, host samples, probe execution perspective, runtime controls, health, incidents, and events.
- Linking: VPS and MonitoringInstance remain separate aggregates connected through `vps_monitoring_instance_links`. Link/unlink only changes association history and never mutates VPS lifecycle, subscription, Target, agent plan, or monitoring runtime state.

## Contracts

- API routes become `/api/monitoring-instances*`; old `/api/nodes*` routes are removed.
- Frontend routes become `/monitoring*`; old `/nodes*` routes are removed.
- JSON uses `monitoring_instance_id`; old `node_id` is not accepted by frontend/backend/agent contracts.
- Agent token file stores `monitoring_instance_id` and `sync_token`.
- Event/object contracts use `monitoring_instance` object type and monitoring-instance event / incident names.
- Target execution labels become `execution_monitoring_instance_labels`.

## Data Flow

Enrollment flow:

1. Operator creates a MonitoringInstance from `/monitoring`.
2. Center issues one-time enrollment token via `/api/monitoring-instances/{id}/install-command`.
3. Agent enrolls with `/api/agent/enroll`.
4. Center returns `monitoring_instance_id`, binding status, and sync token.
5. Agent persists `monitoring_instance_id` + sync token and posts sync batches with `monitoring_instance_id`.

Observation flow:

1. Agent sync writes monitoring instance heartbeats, host samples, probe observations, and command results.
2. Incident service evaluates monitoring instance and target observations.
3. Dashboard summarizes monitoring instance health and asset summary independently.
4. Frontend `/monitoring` displays runtime observation evidence; VPS pages consume only explicit association summaries.

## Compatibility

This is a deliberate breaking migration. Existing local agents, token files, scripts, API clients, browser bookmarks, and local database rows using old Node contracts are not compatibility targets. Development data may be recreated or migrated by the new schema migration.

## UI Responsibility Boundaries

- `/vps`: asset inventory and single-server management facts.
- `/subscriptions`: cost and renewal facts only.
- `/asset-decisions`: human decision queues and cancellation/migration linkage.
- `/monitoring`: monitoring instances, agent onboarding, runtime control, host performance evidence, and future monitoring sub-surfaces.
- `/targets`: entrypoint probe configuration and coverage, using “执行监控实例” terminology.
- `/events`: audit and diagnostic timeline, using monitoring instance object labels.

## Risks

- Broad rename can accidentally alter Node.js / ReactNode / DOM Node references. Mechanical replacements must exclude third-party/runtime language usages.
- SQL migrations must keep fresh database creation and upgraded database paths coherent.
- Tests and visual evidence fixtures are expected to be the main drift detector.
