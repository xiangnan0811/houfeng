# APP projection CAS 实施计划

> Active task: `.trellis/tasks/07-14-vps-records-platform-foundation`
> 范围：补齐 r1 必需的 APP projection SQL primitive；不把尚未实现的 ledger/full-witness saga 伪装为已经完成。

**Goal:** 让真实 `0051` schema 提供两项唯一、最小权限、可重试的 runtime APP projection 函数，并以一个闭合的 v1 `bytea` 命令合同驱动 `deployment_contract_state` 的 activation / domain-rotation CAS。

**Architecture:** runtime 在调用前负责从 primary + full witness 验证外部治理证据；本计划中的 APP 函数只接受已经验证后的 canonical projection command，锁定本地 singleton state，比较完整前态，并原子写入唯一可由 APP 保存的投影。函数返回由 command 派生的确定性 CAS receipt digest，供后续 ledger/witness receipt 持久化；它不声称能跨 DSN 自行验证外部 witness。

**Tech stack:** Go 1.24 canonical codec + PostgreSQL 16 PL/pgSQL `SECURITY DEFINER` functions + pgx integration tests.

---

## 1. 已确认的边界

- r1 runtime 的持久函数 `EXECUTE` 集精确为 `public.record_platform_cas_contract_activation_projection(bytea)` 与 `public.record_platform_cas_domain_rotation_projection(bytea)`；admin 没有这两项权限，也没有 `deployment_contract_state` 直写权限。
- 真实 `0051` 当前缺失这两个函数，现有 catalog fixture 会自行造 no-op 函数，不能证明真实 migration。
- `0051` 尚未进入 `origin/main` 或远端分支；Task 11 也明确该未发布 0051 baseline 应直接修订，不能新建与其它 Child 所有权冲突的临时 migration。
- 本次不实现 ledger/witness/recovery-control 的 typed entry / receipt 验证、外部 saga 或 production caller wiring。调用者必须在未来实现中先验证 witnessed `(sequence, hash)`，再调用本地 CAS；该依赖要继续作为 Child 1 未完成项保留。

## 2. `ProjectionCommandV1` 的闭合字节合同

命令不是 JSON、文本 DSL 或不透明 blob。Go encoder/decoder 与 SQL decoder 都只接受下列单一二进制形状，任何错误 magic、version、operation、field count、长度、token、数值范围或尾随 byte 都拒绝。

共同 header（37 bytes）：

| Offset | Bytes | Value |
| --- | ---: | --- |
| `0` | 33 | ASCII `HOUFENG-APP-PROJECTION-COMMAND-V1` |
| `33` | 2 | unsigned big-endian version `1` |
| `35` | 1 | operation: `1=activation`, `2=rotation` |
| `36` | 1 | exact field count |

所有整数是 big-endian unsigned 64-bit，且必须落在 PostgreSQL positive `bigint` 范围；32-byte digest 原样携带；`DeploymentIDV1` 与 `MutationIDV1` 是固定 67-byte ASCII token（因此不使用可变字符串编码）：`dp-[0-9a-f]{64}`、`tm-[0-9a-f]{64}`。profile code 是 `1=postgres_sync` 或 `2=s3_worm`。固定宽度 token 无 trim、case-fold 或 Unicode normalization 路径。

### Activation command (`operation=1`, `field_count=18`, exact 532 bytes)

从 offset 37 起依序编码：

1. `deployment_id[67]`
2. `active_profile[1]`
3. `activation_mutation_id[67]`
4. `witnessed_ledger_sequence[u64]`（必须为 1）
5. `witnessed_ledger_hash[32]`
6. `plan_digest[32]`
7. `authorization_artifact_digest[32]`
8. `activation_bundle_digest[32]`
9. `trust_revision[u64]`（正数）
10. `trust_head_hash[32]`
11. `inventory_digest[32]`
12. `approval_policy_digest[32]`
13. `adapter_policy_generation[u64]`（必须为 1）
14. `adapter_policy_digest[32]`
15. `drain_receipt_digest[32]`
16. `identity_set_epoch[u64]`（必须为 1）
17. `identity_set_digest[32]`
18. `minimum_fence_contract_version[u64]`（正数）

SQL 只允许 singleton 的 inactive all-null/zero branch 转换；它设置 activation + active policy/identity 字段、`last_domain_identity_{sequence,entry_hash}` 与 `witnessed_ledger_{sequence,hash}` 为 command 的 sequence/hash。完全相同的已投影 state 是 no-op retry；其它 active state 为稳定 CAS conflict。

### Rotation command (`operation=2`, `field_count=21`, exact 508 bytes)

从 offset 37 起依序编码：

1. `deployment_id[67]`
2. `active_profile[1]`
3. `rotation_mutation_id[67]`
4. `expected_witnessed_ledger_sequence[u64]`
5. `expected_witnessed_ledger_hash[32]`
6. `expected_identity_set_epoch[u64]`
7. `expected_identity_set_digest[32]`
8. `expected_adapter_policy_generation[u64]`
9. `expected_adapter_policy_digest[32]`
10. `expected_minimum_fence_contract_version[u64]`
11. `expected_trust_revision[u64]`
12. `expected_trust_head_hash[32]`
13. `next_witnessed_ledger_sequence[u64]`
14. `next_witnessed_ledger_hash[32]`
15. `next_identity_set_epoch[u64]`
16. `next_identity_set_digest[32]`
17. `next_adapter_policy_generation[u64]`
18. `next_adapter_policy_digest[32]`
19. `next_minimum_fence_contract_version[u64]`
20. `next_trust_revision[u64]`
21. `next_trust_head_hash[32]`

The rotation list has exactly **21 semantic fields**; byte 508 is EOF, so an omitted field or a 22nd field is noncanonical. The mutation ID binds the command to the external typed rotation proof; APP state does not persist a second mutable mutation register, so only the fully witnessed future saga may treat its returned receipt as authoritative.

SQL requires the expected deployment/profile/current ledger tuple/current identity/current policy/current fence/current trust tuple to exact-match the locked row. It then requires: next ledger sequence strictly advances; next identity epoch equals current + 1 and digest differs; profile and all activation provenance remain unchanged; policy is either byte-identical at the same generation or a one-generation advance with a different digest; fence cannot decrease; trust revision cannot decrease and equal revision requires equal hash. It writes only active projection fields, last identity entry tuple, witnessed ledger tuple, trust tuple and timestamp. An already exact next state returns the same retry result; every other state is a conflict.

## 3. SQL security and receipt contract

- Define both public functions in unreleased `db/migrations/0051_create_record_platform_foundation.sql`, after the singleton state and local internal schema exist.
- Both are `SECURITY DEFINER`, `SET search_path = pg_catalog`, use fully qualified `public.*` / `record_platform_internal.*` references, accept exactly one `bytea`, and have no overload or convenience entrypoint.
- Internal fixed-width reader helpers are private to `record_platform_internal`, are revoked from `PUBLIC`, and exist only to avoid duplicate byte arithmetic. The two public functions explicitly `REVOKE ALL ... FROM PUBLIC`; runtime-specific grants remain the scoped manifest-convergence responsibility.
- Each public function locks `public.deployment_contract_state` for `project_id='default'` before deciding apply / exact-retry / conflict. It returns `bytea`:

```text
SHA-256(
  "HOUFENG-APP-PROJECTION-CAS-RECEIPT-V1"
  || u32be(command_length)
  || exact_command_bytes
)
```

  The receipt digest is deterministic, never self-referential, and is only a local CAS receipt. A later runtime saga must embed and witness it alongside the validated external ledger/witness proof.

## 4. TDD implementation order

### Task 1: Freeze the shared Go command codec

**Files:**

- Create: `internal/center/recordplatform/projection_command.go`
- Create: `internal/center/recordplatform/projection_command_test.go`

- [x] Write RED tests for exact activation/rotation encodings, parser round trips, foreign operation, malformed header/count/length, trailing bytes, bad token/profile, high-bit integer, invalid activation invariants, non-next epoch, non-monotonic ledger/fence/trust/policy transitions.
- [x] Run `go test ./internal/center/recordplatform -run ProjectionCommand -count=1`; observe missing-package/function failures.
- [x] Implement the minimal closed types, marshal, parse and validation needed for the tests. No database access or caller wiring belongs in this package.
- [x] Re-run the focused test command; it must pass before any DDL uses a generated command.

### Task 2: Add real 0051 projector DDL and migration regression

**Files:**

- Modify: `db/migrations/0051_create_record_platform_foundation.sql`
- Modify: `internal/center/store/migrate/record_platform_migration_test.go`

- [x] Write RED source-level assertions for both exact public identities, security-definer/search-path/PUBLIC revoke fragments and the two closed operation markers.
- [x] Run `go test ./internal/center/store/migrate -run RecordPlatformFoundationMigration -count=1`; observe failure because 0051 has neither function.
- [x] Add the internal reader helpers and two public functions. Keep parsing and validation before mutation, use `FOR UPDATE`, and return the exact receipt digest on apply and exact retry.
- [x] Re-run the focused migration test; it must pass.

### Task 3: Prove behavior against a real migrated PostgreSQL database

**Files:**

- Modify: `internal/center/store/migrate/postgres_integration_test.go`

- [x] Write RED integration cases that apply the real 0051 as a temporary migrator role, inspect `pg_proc`/ACLs, grant runtime only the two exact `EXECUTE` identities, and prove direct runtime `UPDATE` plus admin function calls fail with `42501`.
- [x] Add valid activation, byte-identical retry, valid next-epoch rotation, stale ledger/hash/epoch, profile mismatch, lower fence, malformed/trailing command and concurrent contender cases. Build commands only through the production Go codec.
- [x] Run the focused real PostgreSQL test through `scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store/migrate -run '^TestPostgresIntegrationRecordPlatformProjectorFunctions$' -count=1`; first observe the intended RED failures, then GREEN after the DDL is complete.

### Task 4: Review and verification

- [x] Run `gofmt -w internal/center/recordplatform/*.go` and `git diff --check`.
- [x] Run `GOTMPDIR=/home/murray/.codex GOFLAGS=-p=1 make verify-go`.
- [x] Run the focused PostgreSQL integration command again (the wrapper rejects skip).
- [x] Require a fresh spec/security review first, then a separate code-quality review. Fix every blocking finding and repeat the affected review.

## 5. 验证证据（2026-07-23）

- `d57d65d3` 交付闭合的 Go codec 和两个真实 `0051` projector 函数；`527118af` 补齐 rotation 拒绝、畸形/尾随命令和过期 sequence/epoch 的覆盖。
- `8b9767fc` 加强相邻 ACL runtime regression：独立认证的受限成员登录获得临时 runtime-role membership 后执行 `SET ROLE`，以精确的 `session_user`/`current_user` 不匹配被拒绝。该 membership 是仅限测试的对抗性漂移，并会在清理时移除。
- 控制会话独立运行了 focused codec、migration-source、projector PostgreSQL 和 member-login PostgreSQL selectors，随后执行 `GOTMPDIR=/home/murray/.codex GOFLAGS=-p=1 make verify-go`；均已成功完成。新的 spec/security 与独立 code-quality review 对各自范围均未报告 P0/P1/P2。

## 6. 非目标与后续准入门

本计划刻意不把 Child 1 标记为已可准入 Child 2。完成此切片后，控制会话仍须处理生产 runtime wiring、migration/ACL convergence、外部 typed ledger + full-witness validation 与 receipt persistence、owner matrix/default ACL policy，以及 Child 1 的其余 acceptance evidence。仅因这些函数存在，Child 2–11 不得获得准入。
