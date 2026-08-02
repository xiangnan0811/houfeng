# Records Import, Export, and Portability Implementation Plan

> **For agentic workers:** Select a reviewed execution mode before starting.
> Work in bounded RED -> verified RED -> minimal GREEN slices and review domain
> adapter changes with each owning child contract.

**Goal:** Deliver safe human/machine export and atomic import for Records without
legacy conversion.

**Architecture:** A portability service composes domain-owned providers and
participants, stores bounded job/plan/origin metadata in `0058`, stages artifact
bytes in local/S3 storage, and rebuilds mutable projections from authoritative
imported domain rows.

**Tech Stack:** Go/pgx/PostgreSQL, standard ZIP/JSON/SHA-256/Ed25519, local/S3
ArtifactStore, isolated Chromium PDF renderer, React/TypeScript.

---

## Preconditions

- [ ] Children 1-9 are merged and accepted on protected main.
- [ ] Run `trellis-before-dev` for backend/database/http/security and Web
  component/state/quality guidance.
- [ ] Confirm `0058` is free and all owning domain provider/participant APIs
  exist; update this plan to their actual signatures before start.
- [ ] Run supported Go/Web baseline and hooks in a clean non-main worktree.

## Task 1: Archive types and hostile conformance

**Files:** create `internal/center/portability/{types,canonical,archive,manifest,signature}.go`
and focused tests/testdata.

- [ ] Write deterministic canonical/manifest/signature golden tests.
- [ ] Verify RED for path normalization, links/devices, duplicate/collision,
  truncation, size/count/ratio/depth/working-set, and hash mismatch cases.
- [ ] Implement streaming typed archive read/write and optional signature.
- [ ] Run deterministic tests repeatedly plus bounded fuzz tests.

## Task 2: 0058 schema and current ACL fragment

**Files:** create `db/migrations/0058_create_record_portability.sql` and
`internal/center/store/record_portability.go`; modify current APP ACL registry
and migration tests.

- [ ] Write RED schema/constraint/index/TTL/content-allowlist tests.
- [ ] Add the `0058` fragment with exact objects and runtime/admin privileges.
- [ ] Implement store CAS/idempotency operations and typed row mapping.
- [ ] Run fresh/repeat migration, current convergence/admission, and real
  PostgreSQL store tests.

## Task 3: ArtifactStore conformance

**Files:** create `artifact_store.go`, `artifact_local.go`, `artifact_s3.go` and
one conformance suite.

- [ ] Test conditional staging, streaming hash/size, atomic publish, immutable
  version open, revoke, purge, multipart cleanup, and version mismatch.
- [ ] Implement local fsync/rename and S3-compatible staged copy/publish.
- [ ] Run the same suite for local and real MinIO profiles.

## Task 4: Export providers, preview, and human rendering

- [ ] Define provider registry and fixed-preview contracts with all owning domains.
- [ ] Test missing provider, unauthorized material, partial source, inventory
  drift, capacity, expiry, and deletion fence.
- [ ] Implement Markdown/PDF RenderModel exporters and attachment/evidence/
  collaboration/activity/comparison providers.
- [ ] Prove PDF isolation/no-network and semantic parity with Markdown.

## Task 5: Machine export worker and download

- [ ] Test staging cutpoints, canonical manifest, optional signature, publish
  idempotency, cancellation, janitor, and no partial visibility.
- [ ] Implement worker and artifact lifecycle.
- [ ] Test authorization/fence/content lease before headers and during stream;
  verify revoke/reservation yields no new bytes.

## Task 6: Import quarantine and dry-run

- [ ] Test structural/integrity/schema/security/capacity validation and prove
  dry-run writes no authoritative domain row.
- [ ] Implement quarantine registration, validation, ID preallocation, exact
  plan digest, warnings/blockers, and expiry.
- [ ] Add hostile archive corpus and bounded fuzz coverage.

## Task 7: Atomic import apply

- [ ] Test reference remapping, author provenance, local authority, duplicate
  origin, idempotent replay, CAS drift, and every participant cutpoint.
- [ ] Implement staged blobs plus one Records transaction and compensation
  receipts.
- [ ] Rebuild search/activity projections; never import their checkpoints.
- [ ] Run race and real PostgreSQL tests.

## Task 8: HTTP, Web, workers, and adapters

- [ ] Add authenticated preview/export/download/upload/dry-run/apply/status/
  cancel endpoints and response allowlist tests.
- [ ] Add lazy Records import/export UI with loading/progress/warning/error/
  revoked/deleted and 390px/keyboard contracts.
- [ ] Register deletion, backup, restore, and janitor adapters.
- [ ] Prove permanent deletion plus official restore/re-import cannot resurrect
  target content.

## Task 9: Quality and handoff

- [ ] Run focused archive/import/export races and local/MinIO integration.
- [ ] Run full Go/Web/browser gates, `git diff --check`, and `trellis-check`.
- [ ] Update specs for the implemented archive/provider/participant contracts.
- [ ] Merge through protected main and archive this child before Child 11 starts.

## Rollback

Disable routes/workers and purge unpublished staging data. `0058` is additive
but the development database may be rebuilt when returning to a code version
without it. Do not create a legacy compatibility migration.
