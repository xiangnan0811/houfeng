# VPS-first Control Plane Design

## Architecture

VPS becomes the business aggregate root. `vps_assets` owns lifecycle, usage, renewal/cancel/migrate decisions, provider identity, location, labels, and notes. Subscriptions become VPS-scoped billing facts. MonitoringInstance becomes a runtime observation attachment with binding, health, heartbeat, incident, pause, and maintenance facts.

Existing list/read APIs remain available, but primary creation flows move under `/api/vps/{vps_id}` so the frontend does not force users through independent resource lists.

## Backend Contracts

- `POST /api/vps/{vps_id}/monitoring-instances`
  - Input: optional overrides for display name, group, region, city, provider, labels, note, and link note.
  - Behavior: load VPS, derive missing fields from VPS, create MonitoringInstance with lifecycle `待接入`, create VPS-monitoring active link, return the created MonitoringInstance plus link metadata if useful.
  - Errors: 404 for missing VPS, 400 for invalid derived/override input, 409 for link conflicts.
- `POST /api/vps/{vps_id}/subscriptions`
  - Input: price, currency, billing cycle, billing months, started_at, renew_at, auto_renew, auto_renew_cancelled, payment_method, note.
  - Behavior: force `vps_id` from the path, default internal subscription status to active if the database still requires it, return SubscriptionRecord.
- Existing `/api/monitoring-instances` remains for advanced/non-VPS observability creation, but frontend ordinary flow de-emphasizes it.
- Existing `/api/subscriptions` remains for listing/report editing; frontend create/edit no longer exposes `status`.

## Data And Migration

- Add a migration that marks subscription status and monitoring lifecycle as legacy/derived in schema comments where practical; avoid destructive column removal in this slice unless all queries/tests can be updated safely.
- Normalize old rows enough that UI no longer sees subscription status as a required user decision:
  - cancelled/expired subscription rows should influence VPS cancellation attention through existing lifecycle preview logic.
  - active/default subscriptions stay usable as billing facts.
- Keep `monitoring_instances.lifecycle_status` stored for current repository/runtime compatibility, but stop presenting it as the user's VPS business state in new VPS-first flows.

## Frontend Flow

- `VPSCreateModal`: short, first-run-friendly modal. Required user decision: VPS name and provider choice/inline provider creation. Network/location fields are optional. Business states are hidden and sent as defaults.
- `VPSDetailPage`: add a first-class workbench section for:
  - identity and base facts;
  - subscription evidence with quick create;
  - monitoring evidence with quick create + onboarding;
  - services/domains/timeline access;
  - conditional lifecycle coordination.
- Replace “associate existing monitoring instance” as the default empty-state action with “create monitoring for this VPS”. Keep existing link form as secondary.
- `SubscriptionsPage`: remove status select from create/edit forms and status-heavy primary messaging. Status badges may remain in existing rows only as legacy facts if returned by API.
- `MonitoringPage`: rename/create CTA toward observability inventory, de-emphasize onboarding as primary path, and keep runtime controls intact.
- `MonitoringDetailPage`: surface linked VPS context and return-to-VPS path near onboarding.

## Documentation

Update `CLAUDE.md` and relevant active `.trellis/spec` guidance to describe VPS as the business aggregate root. Do not edit frozen `docs/design/v1-baseline/*`.

