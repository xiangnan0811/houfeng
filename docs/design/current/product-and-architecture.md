# Current product and architecture guidance

## Product shape

Houfeng is an early-stage, self-hosted fleet control plane for one operator. Its current center of gravity is:

- monitoring servers and service entrypoints;
- keeping runtime evidence, incidents, events, and settings in one local control plane;
- maintaining a lightweight VPS Asset Ledger so inventory, subscriptions, renewal decisions, and observability evidence can be reviewed together.

This is not a claim that Houfeng is a production-ready platform. Current docs and UI must not claim package-manager distribution, Kubernetes support, containerized agents, automatic upgrades, completed real-inventory validation, provider account truth, billing accuracy, or exchange-rate truth unless current code and evidence prove it.

## Current topology

The supported topology is intentionally small:

```text
operator browser
      |
      v
houfeng-center (Go API + React SPA)
      |
      v
PostgreSQL

houfeng-agent(s) --outbound enroll/sync--> houfeng-center
```

The center serves the API and built SPA, applies embedded PostgreSQL migrations, owns auth/session/settings/incidents/events/retention, and exposes Asset Ledger APIs.

Agents run on monitored hosts, read local credentials, fingerprint the host, sample host/probe/container facts, buffer sync data locally, and initiate communication to the center. The center does not SSH into agents. Agents do not accept arbitrary user scripts or shell commands; the current action surface is bounded to compiled-in diagnostic action IDs.

## Current domain model

These names describe the current codebase. They can evolve, but changes must be reflected through code, contracts, migrations, tests, and docs together.

- `MonitoringInstance`: an agent-backed runtime observation object.
- `Target`: a service or entrypoint that can be probed.
- `ProbeItem`: one concrete TCP, HTTP/HTTPS, or TLS observation method under a target.
- `HostSample` and `ProbeObservation`: raw observation facts.
- `Provider`, `VPSAsset`, `Subscription`, lifecycle/history records, and service/domain records: Asset Ledger facts.

Asset Ledger facts are manual, API, or imported records. They are not provider-account truth unless a task adds and verifies that integration.

## Durable safety boundaries

These are hard requirements because violating them risks correctness, data loss, or false operator confidence:

- Schema changes go through checked-in PostgreSQL migrations, not ORM auto-migration, console edits, or untracked scripts.
- Multi-table writes and write-then-read flows that must remain consistent use transactions.
- Enrollment tokens, sync tokens, session cookies, passwords, SSH keys, provider credentials, webhook URLs, and real inventory data are secrets.
- Generated install commands contain one-time credentials and must not be pasted into public logs, docs, screenshots, or transcripts.
- Lifecycle actions that can retire, archive, delete, cancel, or sever evidence paths must be explicit, reviewable, and auditable.
- Backfilled or historical facts must not be presented as live incidents unless current rules explicitly classify them that way.

## Flexible boundaries

The following are current defaults, not permanent limits:

- Single-operator orientation.
- Small-center deployment topology.
- Linux + systemd agent onboarding.
- Current page hierarchy and navigation grouping.
- Current visual language, density, and component mix.
- Current Asset Ledger and observability relationship.

Future tasks may change these when the user intent and implementation evidence justify it. The correct workflow is to update current guidance and tests with the change, not to treat older design bundles as a freeze.
