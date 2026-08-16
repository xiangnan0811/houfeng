# Task 4 implementation brief

Active task: `.trellis/tasks/07-14-vps-records-evidence-platform`

You are already the Trellis implement worker. Implement Task 4 directly and do
not spawn another implement/check worker. You are not alone in this checkout:
preserve every existing tracked, untracked, and user-owned change. Do not
commit, push, merge, reset, clean, switch branches, or modify worktrees.

## Owned scope

Implement only Task 4 from `implement.md`:

- `monitoring.event/v2`
- `subscription.cost/v1`
- `command.audit/v1`
- asset history authoritative source/activity adapter only

Create the adapter and colocated test files named by the plan, plus the minimum
store source/query files and focused real-PostgreSQL tests required to prove
the authoritative read contracts. Follow existing evidence/adapters and
evidence source repository patterns. Do not add an `asset.history/*` registry
kind.

## Required behavior

- Follow strict RED -> verify expected RED -> minimal GREEN -> verify GREEN.
  Preserve the failing command/output in the final report.
- Freeze event identity, observed/event time, recorded time, backfill,
  correction linkage, provenance, producer/rule versions, prior/resulting
  state, and bounded metric context. Do not infer live/backfilled semantics
  from retained raw facts.
- Freeze original subscription amount/currency and billing period, conversion
  rate/provider/date/fetched-at/staleness, base amount/currency, budget
  source/month/limits/status, and actual coverage. Never expose provider
  secrets or raw responses.
- The asset-history adapter covers renewal decisions, price histories, IP
  histories, and VPS spec snapshots as versioned authoritative source/activity
  facts. It must not implement or register a new evidence kind.
- Command audit evidence is metadata-only: stable audit/action/instance/actor
  identity snapshots, command ID, sensitivity, event/outcome/source/exit/time.
  Permanently exclude `details`, stdout, stderr, raw output, URLs with query or
  userinfo, tokens, secrets, arbitrary JSON, and source-only diagnostics.
- Each registered kind must provide versioned allowlisted Summary/read-model,
  Compare compatibility/deltas, Export, renderer/conformance metadata, and
  conformance coverage from the start. Do not leave generic hash-only DTOs for
  a later review.
- Custom source implementations are untrusted boundaries: defensively copy,
  canonicalize order where order is not semantic, reject malformed enums,
  duplicates, count drift, non-PostgreSQL timestamps, chronology drift,
  hostile sizes, forbidden fields, and source/authorization mismatch before
  canonicalization.
- Add hostile corpus tests proving zero command output/details/raw URL/secret
  leakage through preview, capture, summarize, compare, and export.
- Add focused source package tests, adapter conformance tests, and strict real
  PostgreSQL coverage with no acceptance by `SKIP`.

## Explicit exclusions

Do not modify API/router/bootstrap/Web/workers/janitors/capacity/alerts,
AdmissionGate wiring, deletion/export/recovery adapters owned by later tasks,
or the intentionally unchecked Task 2B production gate. Do not change the
registry key set beyond the three Task 4 registered kinds above.

## Finish gate

Run affected focused tests, affected race tests, vet, gofmt diff, the strict
PostgreSQL source test, `make verify-go`, `go mod verify`, Trellis task
validation, and `git diff --check HEAD`. Update Task 4 checklist/evidence only
after those commands pass. Report all touched files and any residual concern.
