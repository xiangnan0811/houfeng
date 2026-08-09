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

- [x] Write RED service tests for draft-owner authorization, quota reservation, local upload target, S3 temporary instructions, state CAS, content length/hash/version, idempotent complete, expiry and stable conflicts. S3 cases must prove the random temporary key is persisted before `BlobStore.Put`, reused across retries and never replaced by an in-memory-only key.
- [x] Confirm RED with `go test ./internal/center/attachments -run 'TestUploadService' -count=1`.
- [x] Implement orchestration through narrow authorization/store/Blob interfaces. Persist the S3 temporary key before object I/O, pass it through `PutRequest.TemporaryKey`, and CAS the exact observed temporary/final version identities. Complete performs server-side byte verification and enqueues processing; it does not scan synchronously.
- [x] Re-run service and real store/backend integration tests; expect GREEN.

## Task 2.2: Attachment HTTP API

**Files:**

- Create: `internal/center/http/handlers/attachments.go`
- Create: `internal/center/http/handlers/attachments_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [x] Write RED handler/router/bootstrap tests for the five fixed endpoints, exact DTO arrays, local streaming/S3 instructions, request limits separate from 256 KiB JSON, and 400/404/409/413/422/503 mapping.
- [x] Confirm RED with focused handler/router/bootstrap commands.
- [x] Implement resource handlers and explicit wiring; use shared JSON/session/error patterns and never log display name, object key, upload URL or content.
- [x] Re-run focused HTTP tests and existing records/router/bootstrap regressions; expect GREEN.

## Task 2.3: Content admission and hostile archive corpus

**Files:**

- Create: `internal/center/attachments/admission.go`
- Create: `internal/center/attachments/admission_test.go`
- Create: `internal/center/attachments/archive.go`
- Create: `internal/center/attachments/archive_test.go`
- Create: `internal/center/attachments/testdata/` hostile and golden fixtures

- [x] Add minimal generated/binary fixtures and RED tests for magic/MIME/extension spoof, invalid UTF-8, image dimensions, PDF complexity, zip-slip, duplicate normalized path, symlink/hardlink, encryption, nested/expanded size bombs, unsupported active content and archive scanner unavailable-before-bytes.
- [x] Confirm each test fails for the missing classifier/validator, not malformed fixture setup.
- [x] Implement streaming/bounded classifiers and structural archive inspection with standard parsers; do not extract archive members to shared disk and do not invoke shell.
- [x] Re-run hostile corpus with time/memory bounds and fuzz seeds; all dangerous cases must fail closed.

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
- Modify: `db/migrations/0053_create_record_attachments.sql`
- Modify: `internal/center/store/migrate/record_attachments_migration_test.go`
- Modify: `internal/center/store/migrate/postgres_integration_test.go`
- Extend: `internal/center/store/attachments.go`
- Extend: `internal/center/store/attachments_test.go`
- Extend: `internal/center/store/attachments_postgres_integration_test.go`

### Slice 2.4.A: Durable processor result and preview contract

- [x] Write RED domain and migration tests for closed processor result codes, canonical typed-result digesting, optional preview validation, and the all-null/all-present preview Blob identity. Prove the current schema cannot durably distinguish original from preview or explain a retry/terminal result.
- [x] Confirm RED with `go test ./internal/center/attachments ./internal/center/store/migrate -run 'Test(ProcessorResult|RecordAttachmentsMigration.*Processor|RecordAttachmentsMigration.*Preview)' -count=1`.
- [x] Add the minimal typed result/preview contract in `processor.go` and extend the still-unmerged `0053` current-development migration with preview Blob identity/media/size plus a closed content-free processor result code. Keep the original Blob identity unchanged; add exact FK/check/index/source assertions and no free-text processor output.
- [x] Re-run focused domain/migration tests, fresh/repeat PostgreSQL migration tests, `go test ./internal/center/store/migrate -count=1`, `go vet ./internal/center/attachments ./internal/center/store/migrate`, and `git diff --check`.

### Slice 2.4.B: Bounded ClamAV INSTREAM protocol

- [x] Write RED fake TCP/Unix protocol tests for exact `zINSTREAM\0` command, big-endian bounded chunks, zero terminator, clean/malware/error replies, truncated/oversized replies, input limit, unavailable endpoint, deadline and cancellation. Add an opt-in real ClamAV probe that uses the same scanner contract.
- [x] Confirm RED with `go test ./internal/center/attachments -run 'TestClamAVScanner' -count=1`.
- [x] Implement a narrow scanner with fixed network/address/timeout/chunk/input/response bounds. It must stream once, never invoke a shell, never include daemon reply/content/object identity in errors or logs, and close the connection on every terminal path.
- [x] Re-run focused scanner tests at `-count=20`, focused race tests, `go vet ./internal/center/attachments`, and the opt-in real scanner probe when configured.

### Slice 2.4.C: Durable claim, lease, attempt and workspace state

- [x] Write RED repository and real PostgreSQL tests for `FOR UPDATE SKIP LOCKED` claim ordering, exact owner/generation/observed-expiry fencing, attempt increment, lease renewal, stale-lease reclaim, due retry, max-attempt/overall expiry, workspace registration-before-materialization, result replay and terminal upload/quota transitions. Scanner unavailable must remain quarantined while retryable and become expired, never available, at its bound.
- [x] Confirm RED with focused store unit tests and `scripts/test-record-platform-integration.sh postgres -- go test ./internal/center/store -run '^TestPostgresIntegrationAttachmentProcessor' -count=1`.
- [x] Implement the processor repository methods in `internal/center/store/attachments.go`. A clean result atomically commits the exact original and optional preview Blob identities, result digest, available state and quota; malware/unsafe results reject and release reservation; unavailable/timeout/processing failures retry or expire according to the claimed attempt and job deadline.
- [x] Re-run focused unit/PostgreSQL tests plus `go test -race ./internal/center/attachments ./internal/center/store -run 'Processor|Workspace' -count=1`, `go vet`, and `git diff --check`.

### Slice 2.4.D: Fixed preview profiles and idempotent workspace janitor

- [x] Write RED tests for a private derived workspace path, symlink/path escape rejection, register/materialize/purge transitions, idempotent content-free purge receipts, and cleanup after cancellation/timeout. Add golden profile tests for metadata-free bounded PNG image output, fixed first-page Poppler PNG arguments/output bounds, bounded UTF-8 text preview and archive-without-preview.
- [x] Confirm RED with `go test ./internal/center/attachments -run 'Test(Preview|ContentProcessorWorkspace|WorkspaceJanitor)' -count=1`.
- [x] Implement image/text processing in-process and invoke only fixed `pdfinfo`/`pdftoppm` argument shapes through `exec.CommandContext`; binary paths are configuration, content paths are workspace-derived, and callers cannot supply flags. No shell, external URL, shared cache/profile, stdout/stderr logging or content-bearing receipt is allowed.
- [x] Re-run focused tests, race tests and the opt-in real Poppler profile. Verify every success/error/cancel replay removes the registered workspace tree and produces exactly one immutable receipt.

### Slice 2.4.E: Worker, restart reconciliation and command wiring

- [x] Write RED orchestration/bootstrap tests for claim -> materialize -> scan/preview -> typed completion -> janitor, bounded backoff, shutdown cancellation, secret/content-free logs and startup cleanup. Register cutpoints after claim, mkdir, source materialization, processing, result commit and physical purge. Include a persisted S3 temporary key whose `VersionID` was not committed before restart.
- [x] Confirm RED with `go test ./internal/center/attachments ./cmd/houfeng-content-processor -run 'Test(ContentProcessor|Bootstrap|RestartReconciliation|CrashCutpoint)' -count=1`.
- [x] Implement thin `cmd/houfeng-content-processor` wiring around domain/store interfaces. Startup reconciliation resolves only the persisted temporary key's current version, CASes that observed version and exact-deletes it; it never lists objects/versions or uses an unversioned delete, and missing/replaced/ambiguous identity remains bounded and fail closed.
- [x] Run fake/real protocol and PostgreSQL/local/MinIO processor integration profiles. Kill and restart at every registered cutpoint; after convergence, workspace/part/cache residue must be zero and each registered workspace must have exactly one terminal purge receipt. Finish with focused race/vet, `make verify-go`, module checks and `git diff --check` before checking Task 2.4 complete.

#### Slice 2.4.E verification evidence (2026-08-07)

- `go test ./cmd/houfeng-content-processor -run 'Test(ContentProcessor|ProcessorReconcilerGroup|Bootstrap|RestartReconciliation|CrashCutpoint)' -count=1`, package `-count=10`, package race and package vet all passed.
- Focused processor/workspace/reconciliation/ClamAV/preview tests passed across `internal/center/attachments`, `internal/center/store` and `cmd/houfeng-content-processor`; worker/reconciler tests passed at `-count=10`, followed by focused race and vet across all three packages.
- The real PostgreSQL crash/restart test passed all six `os.Exit` cutpoints: after claim, mkdir, source materialization, processing, result commit and physical purge. The complete `^TestPostgresIntegrationAttachmentProcessor` selector passed all 19 top-level processor integration tests.
- The real MinIO `^TestS3` suite and the real PostgreSQL + MinIO `TestPostgresMinIOIntegrationAttachmentProcessorS3WorkspaceWorkflow` both passed. The restart bootstrap test proves an unresolved persisted S3 key remains versionless after the first process interruption, then a new bootstrap performs one observed-version CAS, one exact-version delete and replay with no additional I/O.
- `make verify-go`, `go mod verify` and `git diff --check` passed. The opt-in real ClamAV probe was not configured in this environment and is not claimed as executed; the fake TCP/Unix INSTREAM protocol suite remains the deterministic protocol evidence.

## Task 2.5: Authorized preview/download and GC worker

**Files:**

- Create: `internal/center/attachments/download.go`
- Create: `internal/center/attachments/download_test.go`
- Create: `internal/center/attachments/gc.go`
- Create: `internal/center/attachments/gc_test.go`
- Extend: `internal/center/http/handlers/attachments.go`
- Extend: `internal/center/http/handlers/attachments_test.go`
- Extend: `internal/center/store/attachments.go`
- Create: `internal/center/attachments/publication.go`
- Create: `internal/center/attachments/publication_test.go`
- Create: `internal/center/store/attachments_publication.go`
- Create: `internal/center/store/attachments_publication_test.go`
- Extend: `internal/center/attachments/reconciliation.go`
- Extend: `internal/center/attachments/reconciliation_test.go`
- Modify: `db/migrations/0053_create_record_attachments.sql`
- Extend: `internal/center/store/migrate/record_attachments_migration_test.go`
- Extend: `internal/center/store/migrate/postgres_integration_test.go`
- Extend: `internal/center/store/migrate/app_acl_current_contract.go`

### Slice 2.5.A: Authorized stream and renewable lease

- [x] Write and confirm RED tests for draft/record authorization, source/visibility intersection, safe filename/headers, closed original/preview variant, byte ranges, 5 MiB text preview, a reader blocked longer than one lease, background renewal, renewal failure/owner drift cancellation before expiry, the strict one-second maximum, final assertion before every chunk, and concurrent renewal/close without database I/O under the state mutex.
- [x] Implement per-request authorization and a delivery-owned renewal/cancel loop. Serving renewal must atomically re-check the captured epoch, absence of a live deletion fence and the exact owner tuple under the canonical lock order. The authoritative successful database read inside the final serving assertion is the chunk linearization point; a pre-fence assertion may authorize its one in-flight chunk, but every later chunk must assert again. Provisional deletion fencing may commit while the old content lease is live so it becomes the durable cancellation marker; deletion work cannot append to the ledger until that lease is released or expired. Close cancels in-flight renewal, waits for its bounded completion and releases the latest exact owner tuple. Database and delivery-state locks never span arbitrary writer I/O.
- [x] Run focused attachment/handler/store tests, race coverage and real PostgreSQL renewal-vs-reservation/drain cases; verify zero new bytes after a failed assertion or renewal, at most one already-linearized chunk across a concurrent fence, and no ledger append claim while a content lease is live.

#### Slice 2.5.A verification evidence (2026-08-07)

- The download-only file-list suite passed fresh, at `-count=20`, and under `-race -count=5`. The exact file-list command intentionally compiles all production attachment files plus only `download_test.go`, because Slice 2.5.C keeps package-level publication tests RED until its missing contract is implemented.
- `go test ./internal/center/http/handlers -run '^TestAttachmentsDownload' -count=20` and the same selector under `-race -count=1` passed. The partial-stream and pre-first-byte fixtures now inject at assertions four and three respectively, matching the two assertions performed by `Open`.
- `go test ./internal/center/store -run 'RecordPlatform|RecordDeletion' -count=1` and the same selector under `-race -count=5` passed.
- The strict PostgreSQL `^TestPostgresIntegrationRecord(Platform|Deletion)` selector passed all 18 top-level tests, including `RecordPlatformReservationFenceCancelsServingRenewal`, serving-lease database state, provisional deletion reservation, stale-owner takeover, delete commit and recovery paths.
- Focused download, handler and store vet plus `git diff --check` passed. No package-wide attachment GREEN claim is made while Slice 2.5.C remains intentionally RED.

#### Slice 2.5.A concurrent-fence contract correction (2026-08-09)

- [x] User approved the final serving assertion's authoritative database read as the chunk linearization point instead of the later physical network flush timestamp.
- [x] Add a deterministic interleaving test in which the first assertion observes admissible pre-fence state, the fence commits before that assertion returns, exactly that chunk completes, and the next assertion rejects the next chunk.
- [x] Confirm the current short critical-section state machine already implements the approved contract, or make only the minimal production change exposed by the new test. Re-run focused normal/race download and handler suites, then obtain specification and independent quality approval.

#### Slice 2.5.A concurrent-fence correction verification evidence (2026-08-09)

- `TestContentDeliveryAllowsOnlyChunkLinearizedBeforeConcurrentFence` deterministically snapshots the first serving assertion's pre-fence read, commits the fence before that assertion returns, then proves exactly one 4-byte writer call and a second assertion rejected with both `ErrContentDeliveryRevoked` and `recordplatform.ErrLostOwnerLease`. The exact test passed at `-count=100` and under `-race -count=20`.
- The current production order was already `assert -> beginWrite -> writer.Write`, so the approved contract required no production-code change. Focused download tests passed at `-count=20` and under `-race -count=5`; handler download tests passed at `-count=20` and under `-race -count=1`.
- Complete attachment, content-processor command, store, migration and center HTTP/bootstrap package tests passed both normally and under the race detector where applicable. Focused vet, `gofmt -d`, tracked/untracked whitespace checks passed. Separate read-only specification and independent quality reviews both approved the correction with zero Critical, Important or Minor findings.

### Slice 2.5.B: Durable Blob GC protocol

- [x] Write and confirm RED tests for ordinary/permanent candidates, 24-hour database watermark, original/preview/revision/upload-part/pin blockers, durable claim/retry/takeover/completion, exact deletion receipt and one-time physical quota decrement.
- [x] Implement `blob_gc_deletions`, exact-version external delete outside PostgreSQL and generation-fenced replay. Do not use `FOR UPDATE` on immutable `blob_objects`; serialize publication through the documented table-lock order.
- [x] Run focused store/domain tests and real PostgreSQL/local/MinIO GC paths.

### Slice 2.5.C: Final-object publication intent and reconciliation

- [x] Write RED domain/migration/store tests for owner-bound prepared/published/cleanup/retry/terminal states, one active intent per digest key, exact version CAS, metadata-transaction consumption, GC exclusion and stale-owner rejection.
- [x] Implement the `blob_publication_intents` schema/current APP ACL inventory and repository state machine. Intent creation locks `blob_objects` then `attachment_upload_parts`, rejects an active GC fence and persists before any final-object I/O.
- [x] Add an exact-key resolver for local/S3 and a restart reconciler with claim generation, lease, retry and exact-version delete. It may close an intent as consumed when a durable reference exists; it must never list keys/versions or perform an unversioned delete.
- [x] Integrate local upload, direct S3 complete and processor preview publication. Each path records the exact returned version and consumes the intent in the same transaction that creates the first durable upload-part/Blob metadata reference.
- [x] Run crash cutpoints before publish, after publish, after version CAS, after metadata commit and after physical cleanup across fake/local/PostgreSQL/MinIO profiles. Prove no untracked final object, stale metadata publisher or referenced-object deletion remains.

#### Slice 2.5.C verification evidence (2026-08-08)

- Focused publication domain/store/bootstrap tests passed across `internal/center/attachments`, `internal/center/store` and `cmd/houfeng-content-processor`; the same selector passed under `-race`. Focused `blob_publication_intents` migration, exact owned-table/DDL inventory and current APP ACL fragment source tests also passed.
- The default processor bootstrap now constructs the publication reconciler for both local and S3, fails closed when cleanup-repository or exact-key resolver contracts are missing, and orders reconciliation as workspace -> publication -> optional S3 temporary cleanup.
- The real PostgreSQL + local crash/restart test passed all five stages: before publish, after physical publication, after exact-version CAS, after metadata commit and after physical cleanup. Fresh repository/Blob/reconciler instances prove exact-key resolution, exact-version deletion, consumed-reference preservation, expired-lease takeover, stale-owner rejection and idempotent `already_absent` convergence.
- The same backend-neutral five-stage runner passed twice against real PostgreSQL + versioned MinIO in one fresh invocation (`-count=2`). Every S3 stage first persists `ReserveUpload -> PrepareUpload` with an exact `temporary/<64 lowercase hex>` key and null temporary version, then uses only the returned key; no `RecordTemporaryObjectVersion`, object/version listing or unversioned delete is used by the publication test/reconciler path.
- Focused `go vet`, `gofmt -d`, tracked/untracked whitespace checks and temporary MinIO cleanup checks passed. Specification review returned `✅ Spec compliant`; quality review found no Critical/Important issue, and its one Minor helper-naming concern was fixed and re-reviewed closed.

### Task 2.5 verification evidence

- Durable GC and Slice 2.5.A are GREEN with replacement RED/GREEN, repeated race coverage and the full real PostgreSQL platform/deletion selector. The reopened defects were closed by a strict one-second maximum, delivery-owned background renewal/cancellation, reservation-aware atomic renewal, provisional fence cancellation and mutex-free database I/O.
- Slice 2.5.C now closes the final-object publish-before-metadata crash window with durable intent, exact-version reconciliation and symmetric real local/MinIO restart evidence. All Task 2.5 implementation slices are GREEN, but Task 2.5 closeout and Checkpoint 2 remain open until the complete attachment/processor integration, full Go and affected Docker/static gates below pass fresh.

#### Checkpoint 2 reopened-defect closure evidence (2026-08-09)

- Processor preview publication takeover now rebinds the same prepared/published intent only for a strictly newer claim generation, preserving the immutable target and deadline. Version recording and consumption retain exact generation fences; prepared and published takeover plus stale-version rejection tests pass.
- Expired abandoned `created`/`uploading` sessions are now reclaimed by the processor loop through one backend-neutral transaction. The transaction locks the existing project quota row, claims one expired upload without a processor job, updates upload and logical attachment state to `expired`, and releases the exact reservation once. Local and S3 real PostgreSQL tests pass, including replay and the `SKIP LOCKED` candidate-selection contract.
- An expired S3 upload whose temporary key is persisted but whose version is still null can now record the resolver's exact version after expiry and enter temporary-object cleanup; the exact-key/version cleanup claim path passes real PostgreSQL evidence.
- ClamAV daemon `ERROR` replies now return `scanner_unavailable`, matching the worker error mapping and bounded retry contract; the fake TCP/Unix protocol suite and race selector pass. The opt-in real ClamAV probe remains unconfigured and is not claimed.

#### Checkpoint 2 final commit gate evidence (2026-08-09)

- `make verify-go`, `go mod verify`, the trimmed `houfeng-content-processor` build and `go test ./internal/center/deploy -count=1` passed fresh.
- The affected command, attachment, HTTP, handler, store and migration packages passed one fresh combined race run.
- The real PostgreSQL selector covering every attachment processor integration test plus abandoned `created`/`uploading` expiry and expired S3 exact-version cleanup passed fresh.
- The four Checkpoint 2 PRD acceptance criteria were reconciled to the completed endpoint, hostile-corpus, scanner/processor restart and content-delivery evidence before commit.

## Checkpoint 2 gate

- [x] Run attachment HTTP/admission/processor/download/GC unit, race and local/MinIO/processor integration tests fresh.
- [x] Run `make verify-go` and Docker/static tests affected by the new processor binary.
- [x] Review diff against Checkpoint 2; verify no Records canonical DTO or full Web drawer implementation leaked in.
- [x] Commit Checkpoint 2 and report behavior evidence, hostile corpus coverage and remaining Checkpoint 3 integration risks.

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

- [x] Write RED tests for ordered/duplicate-free `AttachmentIDs`, defensive copies, canonical hash changes, equivalent normalization, request fingerprint, `RevisionCommitted.DraftID` and restore round trip.
- [x] Confirm RED with focused records tests.
- [x] Add the minimal immutable contract and canonical encoder field. Preserve existing author/save-reason metadata semantics and ensure an empty list has a stable encoding.
- [x] Re-run all `internal/center/records` tests; expect GREEN.

#### Task 3.1 verification evidence (2026-08-09)

- The focused RED selector failed only because `CompleteRevisionValues.AttachmentIDs`, immutable `CompleteRevisionInput.AttachmentIDs()` and `RevisionCommitted.DraftID` did not exist.
- The minimal GREEN reuses `attachments.NormalizeAttachmentReferences`, preserves caller order, returns defensive non-nil empty slices, adds the ordered IDs to the canonical encoder and copies historical IDs during restore without adding author/save-reason metadata to the content hash.
- Focused tests, the complete `internal/center/records` package, the package race run and vet passed fresh. Store/HTTP/handler compile-only checks and `git diff --check HEAD` also passed.

## Task 3.2: Integrate draft/HTTP/store revision transaction

**Files:**

- Modify: `internal/center/http/handlers/records.go`
- Modify: `internal/center/http/handlers/records_test.go`
- Modify: `internal/center/http/handlers/record_drafts.go`
- Modify: `internal/center/http/handlers/record_drafts_test.go`
- Modify: `internal/center/store/records.go`
- Modify: `internal/center/store/records_test.go`
- Modify: `internal/center/store/records_postgres_integration_test.go`
- Modify: `internal/center/store/record_reads.go`
- Modify: `internal/center/store/record_reads_test.go`
- Modify: `internal/center/store/record_reads_postgres_integration_test.go`
- Create: `internal/center/attachments/revision_participant.go`
- Create: `internal/center/attachments/revision_participant_test.go`
- Create: `internal/center/store/record_attachment_participant.go`
- Create: `internal/center/store/record_attachment_participant_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [x] Write RED tests that require non-null `attachment_ids` in draft/revision DTOs, preserve order through publish/read/restore, transfer exact draft-owned available attachments, permit same-record existing refs, reject foreign/pending/reserved refs and roll back every core/attachment write on failure.
- [x] Confirm RED across handler/store/attachment packages and real PostgreSQL transaction tests.
- [x] Implement transport mapping, ordered ref reads and the transaction-only participant. Register it in `NewPostgresRecordRepository` bootstrap wiring without adding network calls to the transaction.
- [x] Re-run complete Records Core and attachment PostgreSQL tests; verify no-change and idempotency behavior with empty/same/changed attachment lists.

#### Task 3.2 verification evidence (2026-08-09)

- Focused handler RED proved `attachment_ids` was unknown/missing from draft decode, publish mapping and revision responses; focused read, participant, `RevisionCommitted.DraftID` forwarding and bootstrap tests each failed for the absent contract before the minimal GREEN.
- Because `records` already imports `attachments` for canonical reference normalization, a direct `attachments -> records` participant would create an import cycle. The transaction-only SQL behavior remains in `attachments/revision_participant.go`; the thin `store/record_attachment_participant.go` adapter converts the existing Records callback without duplicating validation or performing external I/O.
- The real PostgreSQL transaction test was run once with attachment IDs deliberately removed from the adapter and failed at draft cleanup with the expected untransferred-owner FK; after restoring the adapter it passed ordered draft transfer, same-record reduced/restored refs, historical preservation, read round trip and full rollback after an available row was followed by a quarantined row.
- Complete affected package tests, the affected package race run, focused vet, the full real PostgreSQL `Record(Attachment|Draft|Read|Revision)` selector and whitespace checks passed fresh. Database failures remain internal errors; only typed attachment validation/ownership/state conflicts are classified as Records semantic input failures.

## Task 3.3: Implement `record_attachments` deletion adapter and recovery seams

**Files:**

- Create: `internal/center/attachments/deletion_adapter.go`
- Create: `internal/center/attachments/deletion_adapter_test.go`
- Create: `internal/center/attachments/recovery.go`
- Create: `internal/center/attachments/recovery_test.go`
- Extend: `internal/center/store/attachments.go`
- Extend: `internal/center/store/attachments_postgres_integration_test.go`
- Modify: `internal/center/recorddeletion/types_test.go`

- [x] Write RED tests for exact descriptor surfaces, deterministic health proof, preview/surviving copies, cancel/purge/verify, cross-record dedupe, pins, immediate exclusive Blob delete, idempotent receipts, exact inventory ordering and restore hash/version mismatch.
- [x] Confirm RED independently of the incomplete production adapter registry.
- [x] Implement the closed-name adapter and typed inventory/pin/restore verifier interfaces. Do not create a global RecoveryPointManifest or open production permanent deletion.
- [x] Re-run adapter/store/local/MinIO tests and existing `recorddeletion` registry tests; expect missing later adapters to remain fail closed.

#### Task 3.3 verification evidence (2026-08-09)

- The deletion adapter owns the exact `record_attachments` surfaces and exposes deterministic health/preview proofs. PostgreSQL purge removes target logical/ref/terminal-partial rows in dependency order, retains shared cross-record Blobs, rejects live pins and nonterminal upload/processor/workspace/publication work, commits a permanent exact-version GC claim before external deletion, verifies completed physical proof and replays immutable receipts idempotently. Registry readiness and aggregate receipt encoding remain root-first while purge/verify run in reverse dependency order so `record_core` executes last.
- Recovery inventory uses one `REPEATABLE READ READ ONLY` snapshot, preserves non-nil empty collections and deterministic Blob/upload/job/workspace ordering, reuses bounded GC pins, and rejects expected/actual key, version, hash, size or backend drift before verifying PostgreSQL Blob metadata. The later recovery controller remains responsible for supplying `actual` from byte-verifying local/S3 `BlobStore.Stat`; no global `RecoveryPointManifest` or production deletion bypass was added.
- Fresh `go test ./internal/center/attachments ./internal/center/recorddeletion ./internal/center/store -count=1`, focused `go vet`, and the same three-package `go test -race ... -count=1` passed. The real PostgreSQL `^TestPostgresIntegrationAttachment(Deletion|Recovery)` selector passed, including active publication rollback and a completed/consumed publication positive control.
- A fresh versioned MinIO `RELEASE.2025-04-22T22-12-26Z` plus PostgreSQL run passed `TestPostgresMinIOIntegrationAttachmentDeletionPurgesExclusiveExactVersion`; the temporary MinIO container was removed afterward. `gofmt -d` and `git diff --check HEAD` passed, and the incomplete nine-adapter production registry remains fail closed.

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

- [x] Write RED contract/controller/component tests for non-null ordered IDs, local/S3 instructions, upload/poll state transitions, retry/cancel/remove, revoked/denied download, object URL cleanup, unmount and 390px no-overflow primitive layout.
- [x] Confirm RED with focused Vitest files and architecture tests; components must not call raw `fetch`.
- [x] Implement lazy records attachment facade and controlled primitives using existing request/error patterns. Do not integrate a full editor/material drawer or evidence picker.
- [x] Run focused Vitest, ESLint, TypeScript, production build, bundle and CSS budgets; verify attachment code remains out of the entry chunk.

#### Task 3.4 verification evidence (2026-08-09)

- RED first proved that required non-null ordered `attachment_ids`, the five attachment façade exports, queue/controller behavior and controlled upload/download components were absent. The initial CSS addition also exceeded the existing ratchets; the final implementation reuses the existing Asset Decisions layout primitives and raises no budget.
- The final shared transport keeps JSON, empty and Blob responses behind `apiRequest.ts`; local uploads send only instructed draft/hash/content headers, S3 uploads use `credentials: 'omit'` and reject unsupported required headers, and authorized content remains a Blob response. The controller recreates sessions on retry, treats cancel as frontend abort, only removes terminal queue entries and aborts/suppresses work after disposal.
- Focused ESLint and `tsc -b` passed. The six attachment transport/controller/component Vitest files passed all 42 tests; the fresh complete coverage run passed 129 files and 897 tests. `apiRequest.ts` retained 100% statements, branches, functions and lines, above its 90% critical branch ratchet.
- A fresh Node 22 production build, `bundle:check`, `css:analyze` and `git diff --check HEAD` passed. Exact bundle results were entry JS `110733 / 110738`, entry CSS `37125 / 37125`, max async JS `31902 / 32052` and fonts `139072 / 139072`; the CSS analyzer passed without source or budget changes. No attachment API/controller/component identifier appeared in any built JS chunk, proving the unconsumed façade and primitives remain tree-shaken until a future lazy consumer.
- Node 22 `make verify-web` then passed the complete lint, 129-file/897-test coverage, strict TypeScript production build, bundle/font and CSS gates again. `npm ci` reported the existing audit inventory of one moderate and six high findings; no automatic audit mutation was made. A browser route gate was not claimed because Task 3.4 intentionally has no production page consumer and does not integrate the later Child 5 drawer.

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

- [x] Write RED config/static tests for explicit backend, persistent local path, private S3 settings, scanner/processor readiness, tmpfs/read-only/non-root/cap-drop/core=0, bounded queue/workspace and secret-free diagnostics.
- [x] Confirm RED in config/bootstrap/deploy tests.
- [x] Implement explicit configuration and wiring. Local Compose profile is a development/conformance topology and must not claim an independent production recovery domain.
- [x] Run config/bootstrap/static tests, Compose config validation, processor/center Docker builds and focused local/MinIO/processor end-to-end workflow.

#### Task 3.5 verification evidence (2026-08-09)

- RED config/bootstrap/deploy contracts failed for the absent shared attachment config, implicit processor local fallback, missing production draft authorizer and upload/download/scanner wiring, missing processor/systemd deployment, and stale two-service recovery claims. The new Compose service-block helper was separately corrected after it initially truncated at the first nested YAML line; focused RED then represented the production contract rather than fixture setup.
- `LoadAttachmentConfig` now requires explicit local/S3 backend configuration, rejects broad/non-absolute local roots, honors S3 `_FILE` secrets, centralizes ClamAV limits and processor attempts, and is shared by center and processor. Center wires real Blob/scanner/upload/download services only in Records runtime-admission mode; legacy mode remains compatible and the still-unowned transaction admission gate remains fail closed.
- Fresh config, center/processor bootstrap, draft authorization/store and deploy tests passed. `docker compose --env-file docs/deploy/compose.env.example -f compose.yaml config --quiet` passed with exact services `db`, pinned healthy `clamav`, `houfeng`, and `houfeng-content-processor`. The pinned ClamAV 1.4.3 image contains its configured healthcheck, and an opt-in real TCP scanner clean probe passed.
- `go build -trimpath -o /dev/null` passed for center and processor. `docker build --build-arg VERSION=dev -t houfeng:task-3-5 .` passed; runtime inspection proved UID/GID 10001, both binaries, `pdfinfo`/`pdftoppm`, and a mode-0700 Blob root. Processor entrypoint runs without center-only admin/session secrets.
- Real PostgreSQL local processor/revision/deletion/recovery focused workflows passed. A temporary real versioned MinIO `RELEASE.2025-04-22T22-12-26Z` passed S3 direct upload, processor workspace, exact-version GC, publication crash/restart and permanent deletion selectors; the temporary MinIO and ClamAV containers were stopped and auto-removed afterward.
- `make verify-go`, `go mod verify`, the affected eleven-package race run, Node 22 `make verify-web` (129 files / 897 tests plus TypeScript/build/bundle/CSS), fresh deploy/static tests, `gofmt -d`, Compose validation and `git diff --check HEAD` passed. `npm ci` retained the existing one moderate/six high audit inventory; no automatic audit mutation was made.

#### Checkpoint 3 final quality review evidence (2026-08-09)

- The required full-scope Trellis 2.2 review re-read every applicable backend/Web quality contract and traced configuration, authorization, ordered attachment IDs, revision transaction participation, deletion/recovery seams, lazy Web transport, Compose and systemd across the complete branch diff. Every Checkpoint 3 PRD acceptance criterion was reconciled to the focused, race, real PostgreSQL/MinIO/ClamAV, Docker and Web evidence above; no Critical or Important finding remained.
- The complete branch diff against `origin/main` contains 135 task-owned source/spec/test/deployment paths. It contains no generated build output, screenshot manifest, bulk raster artifact, credential value or content-bearing diagnostic. Processor logging remains limited to fixed lifecycle messages and a closed error class; the only production Web `fetch` owners remain the shared transport façade, including the credential-omitting S3 request helper.
- Primary-checkout state was inspected read-only and left unchanged; all task edits remain confined to `/home/murray/code/houfeng/.worktree/vps-records-attachments-storage`. A final light gate passed `go test ./internal/center/deploy -count=1`, Compose config validation with the exact four services, and `git diff --check HEAD`.

## Checkpoint 3 and PR gate

- [x] Run all focused Records/attachment/deletion/Web tests fresh.
- [x] Run `go test -race` for changed Go packages and real PostgreSQL/local/MinIO/processor integration profiles.
- [x] Run `make verify-go`.
- [x] Use Node 22 and run `make verify-web`; record files/tests/typecheck/build/bundle/CSS results and existing audit findings without automatic audit fixes.
- [x] Run Docker static tests, Compose validation and required image builds.
- [x] Run Trellis spec/quality/cross-layer review and reconcile every PRD acceptance criterion to evidence.
- [x] Review the complete diff for scope, generated artifacts, secrets/content logs and primary-checkout isolation.
- [x] Commit Checkpoint 3, push the feature branch, open one PR, monitor required CI, fix failures on the same branch and proceed through merge/post-merge/release only with fresh evidence and repository policy.

#### Final delivery evidence (2026-08-09)

- Checkpoint 3 committed as `1887821c`; the three task commits were pushed on `codex/vps-records-attachments-storage`. PR #400 passed all seven required checks and merged at the exact reviewed head as `78bf44c1`.
- Protected-main CI run `31317881804` passed all seven Go, Web, browser, Docker and PostgreSQL 16.0/16.6/16.12 jobs. Release Please updated PR #399 for `v0.63.0`; that PR passed the same seven checks and merged as `541bd7aa`.
- Release `v0.63.0` targets `541bd7aa` and contains signed/checksummed linux/amd64 and linux/arm64 agent assets. Publish run `31318104764` passed asset publication plus amd64/arm64 image builds and manifest inspection; `v0.63.0`, `0.63.0` and `latest` resolve to multi-architecture digest `sha256:8b4a6980573eb8d819327a2575ed09d35669ceac29d8bfef4853ba613be0085b`.
- The release merge's protected-main CI run `31318098864` also passed all seven jobs. The dedicated task worktree remains clean apart from this Trellis closeout record and was not switched, reset or cleaned.

## Rollback

- Feature/config disable stops new upload admission; existing authorized available attachments remain readable while backend is healthy.
- Processor failure leaves new material quarantined and bounded for retry/expiry; it does not relabel unsafe bytes available.
- There is no down migration. Returning to code without `0053` requires rebuilding the current development database.
- No production deletion path is opened until every required adapter from later children is registered and healthy.
