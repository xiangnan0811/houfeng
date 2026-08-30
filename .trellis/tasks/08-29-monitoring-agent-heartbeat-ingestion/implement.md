# Agent 心跳队列永久拒绝修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` task-by-task, plus `superpowers:test-driven-development`. Do not run `task.py start` until the user explicitly approves this plan.

**Goal:** 让 v0.79.1 已接入流程产生的 agent 在存在永久拒绝的旧持久队列项时仍能把当前心跳送达 center，同时把当前凭据/绑定永久失效暴露为脱敏、可操作的失败。

**Architecture:** 保留 enqueue-before-send、oldest-first 和 backfill；在 runtime flush 增加当前 authority 校验，并按 `*enroll.RemoteError` 的 status/code 执行 discard、terminal 或 retry。center 与 Web 继续只消费真实持久化事实。

**Tech Stack:** Go 1.26.2、stdlib `errors`/`log/slog`、agent persistent `syncqueue.FileStore`、Trellis。

---

## Risk boundaries

- 不改 agent↔center DTO、endpoint、数据库、Web 状态映射或生命周期定义。
- 不削弱 token/binding/fingerprint 校验，也不自动 re-enroll。
- 同一当前 authority 的合法积压必须保留；不按年龄或 attempts 任意丢弃。
- 只有 stale authority 和明确 `invalid_json`/`invalid_request` 才删除；429/5xx/transport 必须保留。
- 日志和返回错误不得含 token、Authorization、原始 fingerprint、request/response body 或 RemoteError 自由文本。

## TDD execution checklist

- [x] **1. Start the approved task and re-establish baseline**
  - 用户批准后，在 worktree 运行 `python3 ./.trellis/scripts/task.py start .trellis/tasks/08-29-monitoring-agent-heartbeat-ingestion`。
  - 确认分支、hooks、任务 context 与 dirty state；不触碰主 checkout/main。
  - 重跑基线：`GOTOOLCHAIN=go1.26.2 go test ./agent/runtime ./agent/syncqueue ./agent/token ./agent/enroll ./internal/center/http/handlers ./internal/center/store -count=1`。

- [x] **2. RED — rejected persisted authority must not block current heartbeat**
  - 在 `agent/runtime/runtime_test.go` 新增 `TestRuntimeRejectedPersistedIdentityDoesNotBlockCurrentCredentialHeartbeat`。
  - 使用真实 `syncqueue.FileStore` seed instance A/old token/old fingerprint；runtime 使用持久 current instance B/current token/current fingerprint。
  - fake client 对 A 返回 typed 401 或 409，对 B 接受；断言 B 必须被尝试、当前 batch 不丢失、A 不再留在队头。
  - 运行单测并保存当前代码“只反复尝试 A，B 未发送”的真实 RED。

- [x] **3. RED — freeze the full queue decision matrix**
  - 表驱动覆盖 ID、token、heartbeat/carrier fingerprint 任一不匹配均属于 stale authority；同 authority 的 old timestamp/attempts 不是 stale。
  - 覆盖 typed `invalid_json` / `invalid_request`：poison entry 删除并继续下一项。
  - 覆盖 current-authority 401/404/409/405/其他非 429 4xx：runtime 返回 terminal error，entry 保留并已标记 attempt。
  - 覆盖 429、503、其他 5xx 与 transport error：entry 保留，下一 tick 重试且标记 backfilled。
  - 覆盖有效 current-authority 多条 backlog 仍 oldest-first 全部发送并删除。

- [x] **4. GREEN — current-authority queue filtering**
  - 把 current MonitoringInstance ID、token、fingerprint 传入 queue flush 边界。
  - 实现小型纯函数验证 queued request authority/carrier 完整性，返回稳定 reason 枚举，不返回或格式化秘密值。
  - stale/local-poison entry 先 durable 删除、成功后才写脱敏 Error 并继续；stale 大积压必须一次 batch delete 后按 reason 聚合记录，删除失败仍作为本地 durability error 终止。
  - 保持当前 request enqueue-before-prune-before-flush 和 backfill 判定。

- [x] **5. GREEN — typed remote-error policy and sanitized failures**
  - 用 `errors.As` 提取 `*enroll.RemoteError`；只依赖 HTTP status 与 `agentapi.ErrorCode*` 常量，不比较字符串 error 文案。
  - 实现 discard/terminal/retry decision；`MarkAttempt`、Delete、continue/return 的顺序与设计矩阵一致。
  - terminal wrapper 的 `Error()` 只输出 status/code/action，必要时 `Unwrap()` 保留类型链，但不得拼接远端 `Message`。
  - 保持 `context.Canceled` 正常退出与 local queue error 既有行为。

- [x] **6. RED/GREEN — diagnostics and privacy**
  - 用 `slog.NewTextHandler` + buffer 捕获 stale discard、invalid-request discard 和 transient retry 日志；terminal current rejection 断言返回错误脱敏且 runtime 不重复记录，交由进程入口唯一记录。
  - 正向断言稳定 action/reason/status/code 可见；负向断言 fixture token、Authorization、raw fingerprint、remote message/body 与 payload 字段全部不可见。
  - 确认成功 hot path 不新增逐 batch info log。

- [x] **7. Focused regression and stress**
  - 运行 runtime/syncqueue/enroll 全包 tests；修正任何旧测试对“所有 remote error 都继续 loop”的过期假设。
  - 运行 `GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -count=10`，避免 tick/cancel 抖动。
  - 运行 `GOTOOLCHAIN=go1.26.2 go test -race ./agent/runtime ./agent/syncqueue ./agent/enroll -count=1`。
  - 运行 center handler/store 回归，确认 agent 错误码、身份拒绝和真实写入合同未回退。

- [x] **8. Full verification and independent review**
  - 运行 `GOTOOLCHAIN=go1.26.2 make verify-go` 与 `git diff --check`。
  - 检索 touched diff 中的 token/fingerprint/Authorization/RemoteError message/logging 路径，逐项对照 PRD 与 `design.md`。
  - 使用 `trellis-check` 执行 spec、复用、lint/vet/test、跨层数据流与任务范围审查。
  - 使用独立 reviewer 按 findings-first、Critical/Important/Minor 审查 queue liveness、terminal/retry 分类、数据保留和秘密泄漏；修复后重跑相同 gates。

- [ ] **9. Protected delivery and released-agent proof**
  - 在用户批准完整交付后按逻辑批次 commit，push feature branch，创建 PR 并等待 required CI；失败只在同一分支修复。
  - required checks 全绿后 merge；监控 main CI、Release Please/release job 与 agent asset 发布。
  - 对最终 release 的目标架构 agent asset 校验版本、checksum/signature，并确认升级后的 runtime regression 通过；不要把 PR 创建或 merge 单独声称为用户问题已交付。
  - 同步/清理 worktree 前报告精确状态；不直接修改本地或远端 main。

## Planned validation commands

```bash
GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -run 'TestRuntimeRejectedPersistedIdentityDoesNotBlockCurrentCredentialHeartbeat|TestRuntime.*(Remote|Queue|Authority|Poison)' -count=1
GOTOOLCHAIN=go1.26.2 go test ./agent/runtime ./agent/syncqueue ./agent/token ./agent/enroll ./internal/center/http/handlers ./internal/center/store -count=1
GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -count=10
GOTOOLCHAIN=go1.26.2 go test -race ./agent/runtime ./agent/syncqueue ./agent/enroll -count=1
GOTOOLCHAIN=go1.26.2 make verify-go
git diff --check
git status --short --branch
```

The exact focused test selector may be narrowed after the RED names exist, but it must not replace the full package and project gates.

## Implementation evidence — 2026-08-30

- Pre-implementation RED: `GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -run '^TestRuntimeRejectedPersistedIdentityDoesNotBlockCurrentCredentialHeartbeat$' -count=1` failed because only the persisted old authority was attempted and the current heartbeat was blocked. The final regression failure text was subsequently reduced to structural counts; it does not dump requests, queue entries, persisted IDs, tokens, fingerprints, or remote bodies.
- Focused decision matrix: `GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -run 'TestRuntime(RejectedPersistedIdentity|DiscardsPersistedQueueEntries|DiscardsExplicitInvalidQueueEntry|CurrentAuthorityPermanentRemoteErrors|TransientQueueFailures|PreservesOldestFirst)' -count=1` passed.
- Related packages: `GOTOOLCHAIN=go1.26.2 go test ./agent/runtime ./agent/syncqueue ./agent/token ./agent/enroll ./internal/center/http/handlers ./internal/center/store -count=1` passed.
- Stress: `GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -count=10` passed.
- Race: `GOTOOLCHAIN=go1.26.2 go test -race ./agent/runtime ./agent/syncqueue ./agent/enroll -count=1` passed.
- Project verification: `GOTOOLCHAIN=go1.26.2 make verify-go` passed.
- Diff hygiene: `git diff --check` passed; the touched diff was manually scanned for token, fingerprint, Authorization, raw request/response, `RemoteError.Message`, and logging exposure.
- Step 8 was completed by the Trellis checker after the implementation pass. The user subsequently approved protected delivery through final cleanup; step 9 remains unchecked until the main session completes that external workflow.

## Trellis check evidence — 2026-08-30

- **Important fixed — duplicate terminal logging:** runtime logged a terminal policy error and then returned it to `cmd/houfeng-agent`, which logged it again. A RED now proves runtime emits no `action=terminal` log; the sanitized returned error remains the single process-entrypoint diagnostic.
- **Important fixed — status/code precedence:** `invalid_json` / `invalid_request` previously caused discard even on non-400 status. Discard now requires HTTP 400 plus the stable code; 404 and every other non-429 4xx remain terminal. A mismatched 404/`invalid_request` regression is GREEN.
- **Important fixed — premature discard evidence:** stale and remotely rejected poison entries were logged as discarded before durable delete succeeded. Both paths now delete first, log only after success, and return the local durability error without a false discard event when deletion fails.
- **Spec synchronized:** `.trellis/spec/backend/error-handling.md` now records the executable 400-discard / permanent-4xx-terminal / transient-retry matrix, single outer terminal log boundary, and sanitized unwrap contract.
- Reviewer focused package set, runtime `-count=10`, runtime/syncqueue/enroll `-race`, `make verify-go`, `git diff --check`, and privacy-path inspection all passed after the fixes. Step 9 is approved but remains the main session's pending protected-delivery work.

## Fresh security/privacy/operational review — 2026-08-30

- **Important fixed — remote free-text leakage outside queue path:** production enrollment and the compatibility no-queue sync path previously wrapped raw `RemoteError`, so `Message` or fallback response body reached `cmd/houfeng-agent` logging. Sanitized wrappers now expose only operation/kind/status/allowlisted code while preserving the typed cause through `Unwrap`; parent cancellation remains a clean shutdown.
- **Important fixed — false enrollment success and URL credential leakage:** `agent enrolled` was emitted before binding/token validation and credential persistence, and runtime start/stop logged the complete configured URL. Success is now logged only after durable credentials; server logs contain parsed scheme+host origin only, never userinfo/path/query/fragment.
- **Important fixed — persisted identifier and local-cause leakage:** stale/poison logs and queue durability errors exposed persisted entry/instance IDs and arbitrary local error text. Main-process-visible Error/log output now contains only stable operation/action/reason/count/status/code; `errors.Is` / `errors.As` still reach the original cause without formatting it.
- **Important fixed — large-backlog O(n²) recovery:** per-entry stale delete rewrote/fsynced the whole queue and emitted one log per entry. Runtime now uses one atomic `FileStore.DeleteMany` and reason-count aggregation; a 256-entry RED proves one batch delete and immediate current-heartbeat delivery.
- **Important fixed — persisted ID collision:** an ID-wide batch delete could remove a retained current-authority fact when a malformed stale entry reused its ID. Runtime collision IDs are excluded from stale batch deletion for arbitrary queue implementations; the later data-correctness review additionally made production FileStore entry IDs unique before runtime classification.
- Hostile status/code precedence now explicitly covers HTTP 400 with unknown code as retained terminal state and HTTP 500 with `invalid_request` as retryable, so code text cannot override status into data loss.
- Final focused packages passed: `GOTOOLCHAIN=go1.26.2 go test ./agent/runtime ./agent/syncqueue ./agent/token ./agent/enroll ./internal/center/http/handlers ./internal/center/store -count=1`.
- Stress passed: `GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -count=10` (`60.629s`).
- Race passed: `GOTOOLCHAIN=go1.26.2 go test -race ./agent/runtime ./agent/syncqueue ./agent/enroll -count=1`.
- Final project gate passed after all product/test changes: `GOTOOLCHAIN=go1.26.2 make verify-go`.
- `git diff --check`, task JSON/JSONL parsing and touched production privacy-path inspection passed. No Web/DTO/database/center contract change was introduced.

## Fresh data-correctness/liveness review — 2026-08-30

- **Important fixed — duplicate persisted entry IDs lost unsent facts:** FileStore derived `Entry.ID` directly from heartbeat batch ID and legacy files could already contain duplicates, while Delete removes every matching ID. Sanitized REDs proved the first ack could erase a second retained current-authority fact before send. Enqueue now suffixes collisions; read deterministically normalizes duplicate/empty IDs across List/Delete/DeleteMany/MarkAttempt; a real-FileStore runtime regression proves the second fact remains durable and no newer heartbeat jumps it. This changes only local entry identity: duplicate carrier `SyncBatchID` remains one center idempotency fact and is not claimed as two facts.
- **Important fixed — duplicate-ID recovery was O(n²):** initial suffix normalization restarted at `-1` for every duplicate. `TestFileStoreNormalizesLargePersistedDuplicateIDBacklog` with 32,768 entries timed out at 2 seconds in `normalizeEntryIDs`; a per-base monotonic cursor made the same test pass in `0.089s`.
- **Important fixed — cancellation after lock wait still mutated disk:** FileStore checked context only before acquiring its path lock. A deterministic context RED showed DeleteMany writing after cancellation; every operation now rechecks after lock acquisition, List after read, and mutators before atomic write. Fingerprint and credential-load parent cancellation also now exits Runtime cleanly instead of returning a false wrapped failure.
- **Important fixed — auxiliary payloads were consumed before durability:** `buildSyncRequest` cleared command results and drained IP-quality reports before Enqueue. Two restart-on-the-same-Runtime REDs proved an Enqueue failure lost both payload types. Runtime now buffers them until queue Enqueue succeeds (or compatibility no-queue Sync succeeds).
- **Important fixed — oversized current entry silently vanished:** FileStore byte pruning could remove the newly enqueued entry itself, still return success, and cause runtime to clear buffered payloads without durable storage or send. Enqueue now returns a stable local durability error without rewriting the prior queue when newest cannot fit.
- **Important fixed — remaining privacy gaps:** store/runtime failure output dumped request/entry structs or identifiers; enrollment success logged persisted MonitoringInstance ID; fingerprint/credential/token/persistence failures formatted raw local causes. Tests now emit sanitized structural diagnostics only, enrollment success logs allowlisted status/binding status only, and stable local-operation wrappers preserve `errors.Is` without formatting causes.
- **Decision lattice completed:** retry tests now cover typed remote status 0/2xx/3xx, wrapped RemoteError, 429/5xx and transport failures; existing 400/401/404/405/409/other-4xx and hostile status/code precedence remain GREEN.
- Focused/related packages passed: `GOTOOLCHAIN=go1.26.2 go test ./agent/runtime ./agent/syncqueue ./agent/token ./agent/enroll ./internal/center/http/handlers ./internal/center/store -count=1`.
- Stress passed after all changes: `GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -count=10` (`60.627s`).
- Race passed after all changes: `GOTOOLCHAIN=go1.26.2 go test -race ./agent/runtime ./agent/syncqueue ./agent/enroll -count=1`.
- Project lint/vet/type/build/test gate passed after all changes: `GOTOOLCHAIN=go1.26.2 make verify-go`.
- `git diff --check` passed. Final task JSON/JSONL parsing and privacy scans are recorded after this evidence update; protected delivery remains owned by the main session.

## Final independent zero-finding review — 2026-08-30

- **Important fixed — dense heartbeat suffix allocation was O(n²):** although duplicate-ID normalization was linear, `entryIDForRequest` still rescanned the full queue for every occupied `base-N` suffix. `TestFileStoreEntryIDScalesAcrossDenseHeartbeatBatchSuffixes` with 32,768 dense suffixes timed out at 2 seconds in the allocator; one precomputed ID set reduced the focused run to `0.146s` without changing carrier `SyncBatchID`.
- **Important fixed — byte pruning repeatedly encoded the full queue tail:** an oversized legacy/hostile persisted queue caused `pruneEntriesByBytes` to marshal the remaining slice once per oldest-entry eviction. A 32,768-entry RED timed out at 2 seconds; the implementation now marshals each entry once, computes exact JSON array size, and selects the oldest-first cut in one linear pass. The same focused group passed in `0.305s`.
- **Important fixed — MaxBytes and logging specs had drifted:** `quality-guidelines.md` incorrectly allowed an individually oversized newest entry to clear the queue and succeed, while `logging-guidelines.md` still required agent enrollment identity/raw status logging. Both specs now match the fail-closed queue and allowlisted, origin-only diagnostic boundary; fragile runtime line anchors were replaced by function-level references.
- Byte-cap writes now fail before temp-file creation when even the selected representation exceeds `MaxBytes`; `TestFileStorePruneDoesNotWriteAnEmptyQueueBeyondMaxBytes` proves an impossible one-byte cap leaves the prior file unchanged.
- Final related packages passed; runtime `-count=10` passed in `60.657s`; runtime/syncqueue/enroll race tests passed; `GOTOOLCHAIN=go1.26.2 make verify-go` passed after every final code/spec change. `git diff --check`, task JSON/JSONL parsing, production `SkipFsync` scan, and touched production error/log privacy inspection passed after this evidence update. No Critical, Important, or Minor finding remains in the reviewed scope; protected delivery is still step 9 and remains owned by the main session.
