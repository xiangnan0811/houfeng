# VPS JSON dry-run import

## Goal

Implement a repo-local VPS JSON import validation slice so real VPS inventory data can be checked against the current Asset Ledger model before Dashboard work. The first path must support dry-run diagnostics without writing data; an explicit import mode may create providers, VPS assets, and subscriptions only after the same validation passes.

## What I Already Know

- The accepted roadmap says Task 4 comes immediately after providers, VPS assets, and subscriptions backend slices.
- Existing backend slices provide `providers`, `vpsassets`, and `subscriptions` domain normalization and validation; import must reuse those rules rather than duplicating a second contract.
- The roadmap requires real JSON dry-run to report missing fields, invalid fields, duplicate candidates, provider creation candidates, and Node association candidates.
- Node link APIs, Dashboard summaries, currency exchange, provider API sync, and `nodes.provider` migration are out of scope for this task.
- There is no existing production CLI import pattern; this task establishes the first import command pattern.

## Research References

- `research/repo-patterns.md` records local CLI, domain validation, store, transaction, and verification patterns.

## Requirements

- Add a Go package under `internal/center/importing` for VPS JSON import parsing, validation, dry-run reporting, and import orchestration.
- Add a command `cmd/houfeng-import-vps-json` that accepts:
  - `-file <path>` for the JSON input.
  - `-dry-run` to validate and print a report without writing data.
  - `-import` to perform writes.
  - `-format json|text`, defaulting to `text`, for machine-readable or human-readable output.
- Keep `-dry-run` and `-import` mutually exclusive; default to dry-run if neither is supplied.
- Read database settings from the existing `HOUFENG_DATABASE_URL` environment variable only when database-backed checks or imports are required.
- Apply migrations before database-backed checks/imports so the command works against a fresh current database.
- Use strict JSON decoding with unknown fields rejected.
- Input root must be a JSON array. Each item represents one VPS asset and can optionally contain a nested `subscription`.
- The accepted input fields must be the current Asset Ledger fields needed for providers, VPS assets, and subscriptions:
  - VPS: `display_name`, `provider_id`, `provider_name`, `product_name`, `order_ref`, `country`, `region`, `city`, `datacenter`, `ipv4`, `ipv6`, `ssh_host`, `ssh_port`, `ssh_user`, `os_name`, `virtualization`, `lifecycle_status`, `usage_status`, `renewal_decision`, `importance`, `labels`, `note`.
  - Subscription: `price`, `currency`, `billing_cycle`, `billing_months`, `started_at`, `renew_at`, `auto_renew`, `auto_renew_cancelled`, `status`, `payment_method`, `note`.
  - Node candidate hints: `node_id`, `node_name`, `agent_token_hint`, `target_url`.
- Dry-run must reuse provider, VPS asset, and subscription domain normalization and validation.
- Dry-run must report:
  - total input rows.
  - providers that would be created from `provider_name` when no `provider_id` is supplied.
  - VPS records that would be created.
  - subscriptions that would be created.
  - rows missing provider identity.
  - rows missing subscription renew dates.
  - validation errors with row numbers and field/context.
  - duplicate candidates within the input.
  - duplicate candidates against existing data when a database is available.
  - Node association candidates requiring human confirmation.
  - future 30-day renewal candidates based on the current date.
  - idle-but-paid candidates.
- Duplicate detection must be conservative and diagnostic-only in dry-run. It must not silently merge records.
- Import mode must refuse to run when validation errors or duplicate candidates are present, unless a future task explicitly designs conflict resolution.
- Import mode must create missing providers first, then VPS assets, then subscriptions.
- Import mode must not create `vps_node_links`, alter Node records, alter `nodes.provider`, or change target/agent behavior.
- Import mode must not accept or write `monthly_price`; subscription monthly price remains backend-derived.

## Acceptance Criteria

- `go test ./internal/center/importing -v` passes.
- `go run ./cmd/houfeng-import-vps-json -file ./tmp/vps-assets.json -dry-run` can execute against a sample JSON file and returns a complete report.
- Strict JSON rejects unknown top-level and nested fields.
- Invalid provider, VPS, subscription, amount, currency, date, enum, and SSH port inputs are reported with row numbers.
- Dry-run reports provider creation candidates, missing provider identity, missing renew dates, duplicate candidates, Node association candidates, 30-day renewal candidates, and idle-paid candidates.
- Import mode is explicit and guarded against validation or duplicate-risk writes.
- Existing providers/VPS/subscriptions API behavior is unchanged.
- `git diff --check` and `make verify-go` pass.

## Definition of Done

- Task PRD, research reference, implement/check jsonl, code, tests, and any required spec updates are committed on a non-main branch.
- Trellis task is archived and journaled after work commits.
- Feature branch is pushed, PR is opened, PR CI is monitored, PR is merged only when green, local `main` is synced, and post-merge `main` CI is monitored.

## Out of Scope

- HTTP import endpoint.
- Frontend import UI.
- `vps_node_links` API or writes.
- Dashboard asset cards.
- Currency conversion.
- Provider API synchronization.
- Delete/update/upsert conflict resolution.
- Automatic Node matching or Node mutation.
- Real secrets in fixtures or docs.

## Technical Notes

- Keep `cmd/` thin. Business behavior belongs in `internal/center/importing`.
- Use existing `providers.NormalizeCreateInput` / `ValidateCreateInput`, `vpsassets.NormalizeCreateInput` / `ValidateCreateInput`, and `subscriptions.NormalizeCreateInput` / `ValidateCreateInput`.
- Dates use `subscriptions.DateLayout` (`2006-01-02`).
- Price validation must remain the subscription domain contract; do not implement a divergent import money rule.
- The text report is for operators; JSON report is the stable machine surface.
