# Bounded verification contract

This task-local contract narrows the quality specs to the evidence required before the uncommitted external-review handoff.

## Evidence order

1. Run the existing focused baseline before implementation and record any pre-existing failure separately.
2. For every behavior change, observe the intended focused RED before implementation, then rerun the same test to GREEN.
3. Run touched-package and cross-layer tests after focused GREEN.
4. Run the repository-wide gates. Focused or spy-only GREEN never substitutes for an integration, PostgreSQL, browser, or full-gate result.

## Web checks

- Test the real authenticated shell and route lifecycle, not only an injected store: pending Legacy write across Dashboard return, Legacy-to-Overview gate change, and pending Overview write across route return.
- Assert no second POST or logical key is produced for an identical pending/transport-unknown attempt, and assert current-view authoritative reload after an old view settles.
- Cover same-VPS exclusion, different-VPS parallelism, exact-token release, user-shell reset, same-digest reuse, changed-digest and 409 rotation, and confirmed-success clearing.
- Verify all five scoped create clients send caller-owned keys and preserve response/status handling. Keep collection-create regression coverage.
- Run the relevant Vitest/React tests, lint, type-check, production build, bundle/CSS checks, then `make verify-web` under Node 22. Run the relevant Chromium Playwright route scenarios and report exact counts.

## Go and database checks

- Apply `gofmt` to touched Go files and run focused package tests plus `go vet` for the touched packages.
- Handler tests for each of the four scoped creates must cover missing/invalid key with zero create calls, first 201, replay 200 with the same ID and no second write, and different-digest 409 with the exact stable code.
- Store tests must exercise begin/lock/lookup/scan/result insert/receipt insert/commit failures and prove rollback/fail-closed behavior.
- Migration tests must verify successor ordering, immutable released files, all receipt objects/FKs/checks, the migration registry, and exact current APP ACL permissions.
- PostgreSQL integration must simulate a committed-but-response-unknown retry for every create and prove one result plus one receipt; monitoring must additionally prove one instance and one link. Use the repository's strict PostgreSQL runner. A missing required DSN or fixture is a blocker, never a skip-as-pass and never permission to create or replace infrastructure.
- Run `make verify-go` after focused checks.

## Contract, privacy, and handoff checks

- Go/TypeScript/manifest contract tests must agree on base type, optional format, required, and nullable. Include a nullable ordinary-string negative control and an explicit date-alias positive control without weakening existing fail-closed parser cases.
- Search touched logs, errors, snapshots, and fixtures for raw request bodies, idempotency keys/digests, notes/details, credentials, or internal SQL/error leakage.
- Reconcile any attachment PNG golden digest difference with the supplied review and label it pre-existing if reproduced; do not claim the full gate passed when it did not.
- Before handoff run `git diff --check`, inspect the full diff/stat/status, and prove the index is empty. Record exact commands, pass counts, blockers, and expected dirty files in task evidence.
- Stop with all intended changes uncommitted and the Trellis task open for external review. Do not stage, commit, push, create a PR, merge, release, archive, or clean up.
