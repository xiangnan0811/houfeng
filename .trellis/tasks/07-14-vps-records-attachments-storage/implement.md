# Blob、附件、配额与扫描 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` to execute this plan task-by-task. Every behavior change follows RED -> confirm expected failure -> minimal GREEN -> focused regression. Steps use checkbox syntax for tracking.

**Goal:** 交付可被 Records revision transaction 原子引用、可在 local/S3 持久存储、经过隔离准入并受统一授权/删除 fence 保护的 attachment platform。

**Architecture:** PostgreSQL 保存 logical attachment、upload/processor/quota/ref/pin/receipt 状态，immutable bytes 保存于 content-addressed local/S3 BlobStore。HTTP 与 processor 在事务外处理字节；`RevisionParticipant` 只在 caller-owned PostgreSQL transaction 内转移 ownership 并写 revision refs。一个 PR 分三个强制检查点，每个检查点独立验证和报告。

**Tech Stack:** Go 1.26.2、pgx/v5、PostgreSQL、`github.com/minio/minio-go/v7`、ClamAV INSTREAM、Poppler、local filesystem、React 19、TypeScript、Vitest。

---

## Preconditions

- [x] Worktree: `/home/murray/code/houfeng/.worktree/vps-records-attachments-storage`。
- [x] Branch: `codex/vps-records-attachments-storage`，base `origin/main@db8bca69`。
- [x] Hooks: `core.hooksPath=.githooks`。
- [x] Baseline: fresh `make verify-go` GREEN；Node 22 fresh `make verify-web` GREEN。
- [x] Child 1/2 merged and post-merge verified。
- [x] User approved the bounded three-checkpoint design on 2026-08-04。
- [x] Planning artifacts self-review GREEN。
- [x] Task activated before implementation edits。

## Delivery rules

- 只在当前 worktree/branch 修改；不触碰 primary checkout 的 unrelated dirty files。
- 不提前实现下一 checkpoint。每个 checkpoint 完成 focused gate、范围复核和进度报告后再继续。
- 新 behavior 必须先有失败测试并记录预期失败原因；migration/config 静态声明仍先写会失败的 source/static test。
- 每个 checkpoint 形成可审查 commit；PR 在三个 checkpoint 全部完成且 full gates GREEN 后创建。
- 若某 checkpoint 出现无法在其验收边界内解释的新子系统，暂停并重新规划；不以“顺便支持”扩大范围。

---

# Checkpoint 1: schema, domain, quota and Blob foundation

## Task 1.1: Freeze attachment domain contracts

**Files:**

- Create: `internal/center/attachments/types.go`
- Create: `internal/center/attachments/types_test.go`
- Create: `internal/center/attachments/validation.go`
- Create: `internal/center/attachments/validation_test.go`

- [x] Write table-driven RED tests for `att_`/`aup_`/`apj_`/`cpw_`/`bgp_` IDs, ordered duplicate-free attachment references, upload state transitions, byte limits, archive scanner readiness and typed errors.
- [x] Run `go test ./internal/center/attachments -run 'Test(Validate|Normalize|UploadState)' -count=1`; confirm failure is missing contracts, not test setup.
- [x] Implement minimal immutable types, validators and state transition table. Centralize 50 MiB/500 MiB/10 GiB/80%/5 MiB defaults once.
- [x] Re-run the focused tests and `go test ./internal/center/attachments -count=1`; expect GREEN.

## Task 1.2: Add `0053` schema and current APP ACL fragment

**Files:**

- Create: `db/migrations/0053_create_record_attachments.sql`
- Create: `internal/center/store/migrate/record_attachments_migration_test.go`
- Create: `internal/center/store/migrate/record_attachments_app_acl_test.go`
- Modify: `internal/center/store/migrate/app_acl_current_contract.go`
- Modify: `internal/center/store/migrate/app_acl_current_postgres_integration_test.go`
- Modify: `internal/center/store/migrate/postgres_integration_test.go`

- [x] Write source RED tests fixing the exact table/function/index/check/FK inventory and exact current APP ACL managed objects, privileges and hardening for `0053`.
- [x] Run `go test ./internal/center/store/migrate -run 'TestRecordAttachments|TestAppACLCurrent' -count=1`; confirm missing migration/fragment failures.
- [x] Add the migration with `blob_objects`, uploads/parts, logical attachments, revision refs, processor jobs/workspaces, pins and purge receipts. Add constraints for ownership XOR, states, bytes, digest/version, unique refs/reservations and immutable receipts.
- [x] Register the exact `0053` current fragment and minimum runtime/admin grants; do not modify frozen R1 inventory.
- [x] Run source tests, then real PostgreSQL fresh/repeat/convergence/runtime admission tests using the repository integration profile; expect GREEN with no privilege overgrant.

## Task 1.3: Implement transactional quota and metadata store

**Files:**

- Create: `internal/center/store/attachments.go`
- Create: `internal/center/store/attachments_test.go`
- Create: `internal/center/store/attachments_postgres_integration_test.go`
- Modify: `internal/center/attachments/types.go`

- [x] Write RED tests for project/draft/effective-record locks, reservation-before-bytes, concurrent creates, complete/reject/expire release, logical-vs-physical usage, 80% warning, copied logical attachment and pin/ref protection.
- [x] Run focused unit and PostgreSQL tests; confirm failures are absent repository behavior.
- [x] Implement pgx transactions with deterministic lock order and typed domain results. Keep all arithmetic in checked `int64`; reject overflow and negative persisted values.
- [x] Re-run focused tests and `go test -race ./internal/center/attachments ./internal/center/store -run 'Attachment|Quota|BlobPin' -count=1`; expect GREEN.

## Task 1.4: Define BlobStore and local conformance

**Files:**

- Create: `internal/center/attachments/blob.go`
- Create: `internal/center/attachments/blob_conformance_test.go`
- Create: `internal/center/attachments/local_blob.go`
- Create: `internal/center/attachments/local_blob_test.go`

- [x] Write a reusable RED conformance suite for conditional put, exact digest/size, dedupe, full/range open, invalid range, version mismatch, idempotent delete and failure-cutpoint cleanup.
- [x] Run `go test ./internal/center/attachments -run 'TestLocalBlobStoreConformance' -count=1`; confirm missing local adapter failure.
- [x] Implement same-directory private temp files, streaming verification, file fsync, atomic conditional publish, directory fsync, defensive path construction and exact version/hash stat/open/delete.
- [x] Re-run local conformance including race and injected interruption; verify no temp residue and private modes.

## Task 1.5: Implement S3/MinIO conformance

**Files:**

- Create: `internal/center/attachments/s3_blob.go`
- Create: `internal/center/attachments/s3_blob_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [x] Add `github.com/minio/minio-go/v7` as the S3-compatible SDK and a RED real-MinIO invocation of the same BlobStore conformance suite; record its module/license/build impact in the checkpoint review.
- [x] Run the real MinIO suite and confirm the adapter is missing; do not replace it with an in-memory fake as acceptance evidence.
- [x] Implement private temporary keys, exact stat/read verification, conditional digest-key publish, version identity, range open and idempotent delete. Unknown version/Object Lock/noncurrent behavior fails closed.
- [x] Re-run local and MinIO suites together. Inspect `go mod tidy` diff and run `go mod verify`; expect GREEN.

## Checkpoint 1 gate

- [x] Run focused domain/store/migration/local/MinIO tests fresh.
- [x] Run `make fmt-go`, `make vet-go`, `make test-go`, then `make verify-go`.
- [x] Review diff against Checkpoint 1 only; verify no HTTP/processor/Web implementation leaked in.
- [x] Commit Checkpoint 1 and report completed contracts, exact commands/results, remaining Checkpoint 2/3 risks before continuing.

---

# Checkpoint 2: secure upload, admission, processor and content streaming

## Task 2.1: Upload session service and transport

**Files:**

- Create: `internal/center/attachments/service.go`
- Create: `internal/center/attachments/service_test.go`
- Extend: `internal/center/store/attachments.go`
- Extend: `internal/center/store/attachments_postgres_integration_test.go`

- [ ] Write RED service tests for draft-owner authorization, quota reservation, local upload target, S3 temporary instructions, state CAS, content length/hash/version, idempotent complete, expiry and stable conflicts. S3 cases must prove the random temporary key is persisted before `BlobStore.Put`, reused across retries and never replaced by an in-memory-only key.
- [ ] Confirm RED with `go test ./internal/center/attachments -run 'TestUploadService' -count=1`.
- [ ] Implement orchestration through narrow authorization/store/Blob interfaces. Persist the S3 temporary key before object I/O, pass it through `PutRequest.TemporaryKey`, and CAS the exact observed temporary/final version identities. Complete performs server-side byte verification and enqueues processing; it does not scan synchronously.
- [ ] Re-run service and real store/backend integration tests; expect GREEN.

## Task 2.2: Attachment HTTP API

**Files:**

- Create: `internal/center/http/handlers/attachments.go`
- Create: `internal/center/http/handlers/attachments_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] Write RED handler/router/bootstrap tests for the five fixed endpoints, exact DTO arrays, local streaming/S3 instructions, request limits separate from 256 KiB JSON, and 400/404/409/413/422/503 mapping.
- [ ] Confirm RED with focused handler/router/bootstrap commands.
- [ ] Implement resource handlers and explicit wiring; use shared JSON/session/error patterns and never log display name, object key, upload URL or content.
- [ ] Re-run focused HTTP tests and existing records/router/bootstrap regressions; expect GREEN.

## Task 2.3: Content admission and hostile archive corpus

**Files:**

- Create: `internal/center/attachments/admission.go`
- Create: `internal/center/attachments/admission_test.go`
- Create: `internal/center/attachments/archive.go`
- Create: `internal/center/attachments/archive_test.go`
- Create: `internal/center/attachments/testdata/` hostile and golden fixtures

- [ ] Add minimal generated/binary fixtures and RED tests for magic/MIME/extension spoof, invalid UTF-8, image dimensions, PDF complexity, zip-slip, duplicate normalized path, symlink/hardlink, encryption, nested/expanded size bombs, unsupported active content and archive scanner unavailable-before-bytes.
- [ ] Confirm each test fails for the missing classifier/validator, not malformed fixture setup.
- [ ] Implement streaming/bounded classifiers and structural archive inspection with standard parsers; do not extract archive members to shared disk and do not invoke shell.
- [ ] Re-run hostile corpus with time/memory bounds and fuzz seeds; all dangerous cases must fail closed.

## Task 2.4: Isolated processor, scanner, preview and janitor

**Files:**

- Create: `internal/center/attachments/processor.go`
- Create: `internal/center/attachments/processor_test.go`
- Create: `internal/center/attachments/scanner_clamav.go`
- Create: `internal/center/attachments/scanner_clamav_test.go`
- Create: `internal/center/attachments/preview.go`
- Create: `internal/center/attachments/preview_test.go`
- Create: `internal/center/attachments/workspace.go`
- Create: `internal/center/attachments/workspace_test.go`
- Create: `cmd/houfeng-content-processor/main.go`
- Create: `cmd/houfeng-content-processor/bootstrap.go`
- Create: `cmd/houfeng-content-processor/bootstrap_test.go`

- [ ] Write RED tests for database claim/lease/attempt, ClamAV INSTREAM framing and limits, fixed Poppler/image/text profiles, typed results, retry/expiry, crash cutpoints and idempotent workspace purge receipts. Include restart reconciliation for a persisted S3 temporary key whose `VersionID` was not committed before the crash.
- [ ] Confirm RED with focused package/command tests.
- [ ] Implement worker orchestration and fixed external command runners via `exec.CommandContext`; no shell, arbitrary args, external network, shared profile/cache or content-bearing logs. Janitor cleanup resolves only the persisted temporary key's current version, CASes the observed identity and exact-deletes that version without object/version listing; ambiguous or replaced identity fails closed.
- [ ] Run real/fake-protocol scanner tests plus processor integration profile; kill each registered cutpoint and verify workspace/part/cache residue is zero with one terminal receipt.

## Task 2.5: Authorized preview/download and GC worker

**Files:**

- Create: `internal/center/attachments/download.go`
- Create: `internal/center/attachments/download_test.go`
- Create: `internal/center/attachments/gc.go`
- Create: `internal/center/attachments/gc_test.go`
- Extend: `internal/center/http/handlers/attachments.go`
- Extend: `internal/center/http/handlers/attachments_test.go`
- Extend: `internal/center/store/attachments.go`

- [ ] Write RED tests for draft/record authorization, source/visibility intersection, safe filename/headers, original-vs-preview enum, byte ranges, 5 MiB text preview, lease expiry, revoke/reservation during stream, 24-hour orphan watermark, pin/ref CAS and physical delete receipts.
- [ ] Confirm RED in domain/handler/store tests.
- [ ] Implement per-request authorization and short stream leases, checking cancellation before writes. Implement ordinary GC and permanent-purge primitive separately so only the time watermark differs.
- [ ] Re-run focused tests including slow-reader cancellation and real local/MinIO GC; prove no new bytes after fence and no referenced/pinned Blob deletion.

## Checkpoint 2 gate

- [ ] Run attachment HTTP/admission/processor/download/GC unit, race and local/MinIO/processor integration tests fresh.
- [ ] Run `make verify-go` and Docker/static tests affected by the new processor binary.
- [ ] Review diff against Checkpoint 2; verify no Records canonical DTO or full Web drawer implementation leaked in.
- [ ] Commit Checkpoint 2 and report behavior evidence, hostile corpus coverage and remaining Checkpoint 3 integration risks.

---

# Checkpoint 3: Records integration, deletion/recovery seams, Web primitives and deploy gates

## Task 3.1: Add canonical attachment references to Records

**Files:**

- Modify: `internal/center/records/types.go`
- Modify: `internal/center/records/types_test.go`
- Modify: `internal/center/records/validation.go`
- Modify: `internal/center/records/validation_test.go`
- Modify: `internal/center/records/revisions.go`
- Modify: `internal/center/records/revisions_test.go`
- Modify: `internal/center/records/application.go`
- Modify: `internal/center/records/application_test.go`

- [ ] Write RED tests for ordered/duplicate-free `AttachmentIDs`, defensive copies, canonical hash changes, equivalent normalization, request fingerprint, `RevisionCommitted.DraftID` and restore round trip.
- [ ] Confirm RED with focused records tests.
- [ ] Add the minimal immutable contract and canonical encoder field. Preserve existing author/save-reason metadata semantics and ensure an empty list has a stable encoding.
- [ ] Re-run all `internal/center/records` tests; expect GREEN.

## Task 3.2: Integrate draft/HTTP/store revision transaction

**Files:**

- Modify: `internal/center/http/handlers/records.go`
- Modify: `internal/center/http/handlers/records_test.go`
- Modify: `internal/center/http/handlers/record_drafts.go`
- Modify: `internal/center/http/handlers/record_drafts_test.go`
- Modify: `internal/center/store/records.go`
- Modify: `internal/center/store/records_test.go`
- Modify: `internal/center/store/records_postgres_integration_test.go`
- Create: `internal/center/attachments/revision_participant.go`
- Create: `internal/center/attachments/revision_participant_test.go`

- [ ] Write RED tests that require non-null `attachment_ids` in draft/revision DTOs, preserve order through publish/read/restore, transfer exact draft-owned available attachments, permit same-record existing refs, reject foreign/pending/reserved refs and roll back every core/attachment write on failure.
- [ ] Confirm RED across handler/store/attachment packages and real PostgreSQL transaction tests.
- [ ] Implement transport mapping, ordered ref reads and the transaction-only participant. Register it in `NewPostgresRecordRepository` bootstrap wiring without adding network calls to the transaction.
- [ ] Re-run complete Records Core and attachment PostgreSQL tests; verify no-change and idempotency behavior with empty/same/changed attachment lists.

## Task 3.3: Implement `record_attachments` deletion adapter and recovery seams

**Files:**

- Create: `internal/center/attachments/deletion_adapter.go`
- Create: `internal/center/attachments/deletion_adapter_test.go`
- Create: `internal/center/attachments/recovery.go`
- Create: `internal/center/attachments/recovery_test.go`
- Extend: `internal/center/store/attachments.go`
- Extend: `internal/center/store/attachments_postgres_integration_test.go`
- Modify: `internal/center/recorddeletion/types_test.go`

- [ ] Write RED tests for exact descriptor surfaces, deterministic health proof, preview/surviving copies, cancel/purge/verify, cross-record dedupe, pins, immediate exclusive Blob delete, idempotent receipts, exact inventory ordering and restore hash/version mismatch.
- [ ] Confirm RED independently of the incomplete production adapter registry.
- [ ] Implement the closed-name adapter and typed inventory/pin/restore verifier interfaces. Do not create a global RecoveryPointManifest or open production permanent deletion.
- [ ] Re-run adapter/store/local/MinIO tests and existing `recorddeletion` registry tests; expect missing later adapters to remain fail closed.

## Task 3.4: Add lazy Web DTO/API/queue primitives

**Files:**

- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/recordsApi.ts`
- Modify: `web/src/lib/recordsApi.test.ts`
- Create: `web/src/pages/asset-decisions/recordAttachments.ts`
- Create: `web/src/pages/asset-decisions/recordAttachments.test.ts`
- Create: `web/src/pages/asset-decisions/AttachmentUploadQueue.tsx`
- Create: `web/src/pages/asset-decisions/AttachmentUploadQueue.test.tsx`
- Create: `web/src/pages/asset-decisions/AuthorizedAttachmentDownload.tsx`
- Create: `web/src/pages/asset-decisions/AuthorizedAttachmentDownload.test.tsx`

- [ ] Write RED contract/controller/component tests for non-null ordered IDs, local/S3 instructions, upload/poll state transitions, retry/cancel/remove, revoked/denied download, object URL cleanup, unmount and 390px no-overflow primitive layout.
- [ ] Confirm RED with focused Vitest files and architecture tests; components must not call raw `fetch`.
- [ ] Implement lazy records attachment facade and controlled primitives using existing request/error patterns. Do not integrate a full editor/material drawer or evidence picker.
- [ ] Run focused Vitest, ESLint, TypeScript, production build, bundle and CSS budgets; verify attachment code remains out of the entry chunk.

## Task 3.5: Configuration, Compose/systemd and final integration

**Files:**

- Modify: `internal/center/config/config.go`
- Modify: `internal/center/config/config_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`
- Modify: `compose.yaml`
- Modify: `Dockerfile`
- Modify: `docs/deploy/compose.env.example`
- Modify: `docs/deploy/local-and-systemd.md`
- Create: `docs/deploy/systemd/houfeng-content-processor.service`
- Modify: `internal/center/deploy/docker_static_test.go`

- [ ] Write RED config/static tests for explicit backend, persistent local path, private S3 settings, scanner/processor readiness, tmpfs/read-only/non-root/cap-drop/core=0, bounded queue/workspace and secret-free diagnostics.
- [ ] Confirm RED in config/bootstrap/deploy tests.
- [ ] Implement explicit configuration and wiring. Local Compose profile is a development/conformance topology and must not claim an independent production recovery domain.
- [ ] Run config/bootstrap/static tests, Compose config validation, processor/center Docker builds and focused local/MinIO/processor end-to-end workflow.

## Checkpoint 3 and PR gate

- [ ] Run all focused Records/attachment/deletion/Web tests fresh.
- [ ] Run `go test -race` for changed Go packages and real PostgreSQL/local/MinIO/processor integration profiles.
- [ ] Run `make verify-go`.
- [ ] Use Node 22 and run `make verify-web`; record files/tests/typecheck/build/bundle/CSS results and existing audit findings without automatic audit fixes.
- [ ] Run Docker static tests, Compose validation and required image builds.
- [ ] Run Trellis spec/quality/cross-layer review and reconcile every PRD acceptance criterion to evidence.
- [ ] Review the complete diff for scope, generated artifacts, secrets/content logs and primary-checkout isolation.
- [ ] Commit Checkpoint 3, push the feature branch, open one PR, monitor required CI, fix failures on the same branch and proceed through merge/post-merge/release only with fresh evidence and repository policy.

## Rollback

- Feature/config disable stops new upload admission; existing authorized available attachments remain readable while backend is healthy.
- Processor failure leaves new material quarantined and bounded for retry/expiry; it does not relabel unsafe bytes available.
- There is no down migration. Returning to code without `0053` requires rebuilding the current development database.
- No production deletion path is opened until every required adapter from later children is registered and healthy.
