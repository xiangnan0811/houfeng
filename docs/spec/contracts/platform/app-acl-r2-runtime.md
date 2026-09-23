# APP ACL R2 冻结 runtime 合同

## Scenario: APP ACL R2 runtime admission/startup

### 1. Scope / Trigger

- Trigger：新增或修改 `AdmitAppACLR1OnlyRuntime`、`AdmitAppACLR2Runtime`、`StartAppACLR2Runtime`，或改变其跨 PostgreSQL connection、advisory lock、transaction、classifier、frozen verifier、direct-runtime predicate、startup admission contract 时。本场景命中新公开 API 与跨 DB runtime admission/startup 的 code-spec mandatory trigger。
- 此场景只定义 transition-aware R2 runtime route：R1-only route 只 admit exact R1；R2/startup route 在 transition 前 admit exact R1、在 shared proof 已完成后 admit exact `FINALIZED`。不得修改冻结的 R1 parser、reader、converger、`AdmitAppACLRuntime`、既有 R1 startup route 或 generic `migrate --scope app`，也不得加入 R2 dispatch dependency。
- 当前冻结摘要必须按 closed source contract 与独立 vector 使用：R1 `0051_create_record_platform_foundation.sql` SHA-256 为 `503d58670dc790c4b852bfb58cf93d2b816c1ce956958567dc605cb28d5cd23f`，共 52 sources、204 tuples；R2 `0052_app_acl_r2_privileged_transition.sql` SHA-256 为 `23f79c60dcede45a42aae82da5a9de0d3d650d7eef64dbfd7ce96c6dd5d95fff`，53-source digest 为 `1d9dc20e71e9f319f8b1cef4b22f9dc92051a88dc9cb8a892b69494658c44dd3`，共 53 sources、206 tuples。

### 2. Signatures

```go
func AdmitAppACLR1OnlyRuntime(ctx context.Context, db *pgxpool.Pool) error
func AdmitAppACLR2Runtime(ctx context.Context, db *pgxpool.Pool) (AppACLR2State, error)
func StartAppACLR2Runtime(ctx context.Context, db *pgxpool.Pool) (AppACLR2State, error)
```

- `StartAppACLR2Runtime` 是 opt-in startup wrapper，必须只委派给 `AdmitAppACLR2Runtime`；它不得创建第二套 classifier、predicate、connection 或 cleanup lifecycle。

### 3. Contracts

- 必须先 `Acquire` 一个 reserved connection；在该连接的任何 snapshot query 前，以 exact key `houfeng.app-acl-r2-privileged-transition.v1` 和 `hashtextextended($1, 0)` 取得 **session-scoped shared** advisory lock：`pg_advisory_lock_shared(...)`。seed 固定为 `0`，key、shared mode、session scope 与 bootstrap 的 exclusive transition lock 必须是同一物理 lock identity。
- 同一已锁 connection 只允许开启一个 `pgx.Tx`：`REPEATABLE READ, READ ONLY`。classify、R1 verifier/predicate 与 commit/rollback 都必须在这个 transaction/connection lifecycle 内；不得在 lock 后另取 connection 或另开 snapshot。
- `R1`：只能按 `ClassifyAppACLR2State(ctx, tx)` -> `VerifyFrozenAppACLR1StateInTx(ctx, tx)` -> `RequireDirectFrozenAppACLR1RuntimeInTx(ctx, tx, frozen)` 的共享顺序在同一 tx 中执行。direct-runtime identity mismatch fail closed。
- `FINALIZED`：只消费 shared classifier 的 exact finalized result；不得再调用 frozen R1 verifier、direct-runtime predicate、frozen `AdmitAppACLRuntime`、R2 parser 或另一条 R2 admission path。
- `PREPARED`、`CORRUPT` 和 unknown state 只能由 classifier 识别后 fail closed；它们不得调用 frozen verifier/predicate、frozen `AdmitAppACLRuntime`，也不得进行 R2 payload、receipt 或 manifest parsing/admission。
- 成功 `Commit` 或 `Rollback` 后必须用相同 key/seed/mode/scope 执行 `pg_advisory_unlock_shared(...)`；unlock 成功才 `Release` connection。任何 lock/begin/finish/unlock 异常都必须 discard reserved connection，不能让可能持有 session lock 的连接回到 pool；discard 使用 fresh、bounded background cleanup context。

### 4. Validation & Error Matrix

| 条件 | 外显结果与 cleanup 不变量 |
| --- | --- |
| public API 的 `db == nil` | R1-only 返回 error；R2/startup 返回 `AppACLR2StateCorrupt` + error。不得 acquire connection。 |
| nil begin dependency，或 classifier/verifier/predicate dependency 不完整 | fail closed（内部 state 为 `AppACLR2StateCorrupt`；R1-only 对外仅返回 error）；不得 reserve、lock 或 begin tx。 |
| acquire error 或 acquire 返回 nil connection | 返回 wrapped reserve error；没有可 cleanup 的 connection，不得 begin tx。 |
| session shared lock error | 返回 lock error；不得 begin/unlock/release，已 reserve 的 connection 必须 discard。 |
| begin error 或 begin 返回 nil tx | 返回 wrapped begin/nil-tx error；在已取得 lock 的连接上尝试 matching unlock，unlock 成功才 release，否则 discard。 |
| classifier、frozen verifier 或 direct-runtime predicate error | fail closed；primary body error 对外返回。deferred rollback 必须尝试完成 locked tx；rollback 成功时 matching unlock + release，rollback failure 使连接状态不确定并 discard，不使用 `errors.Join` 返回 rollback error。 |
| commit error | explicit `Commit` finish API error 返回给其 caller；admission 返回 `AppACLR2StateCorrupt` + wrapped commit error；不执行 unlock/release，直接 discard connection。 |
| rollback finish error | explicit `Rollback` finish API error 返回给其 caller；admission 的 deferred rollback 保留 primary body error，对 rollback failure 只 discard connection，不使用 `errors.Join`。 |
| unlock query error 或 unlock 返回 `false` | 返回 unlock error（`false` 也不是成功）；不 release，必须 discard connection。 |
| request context 在 finish 前取消 | 返回 cancellation error；不得把 canceled request context 用于回收。discard 必须使用 fresh、bounded `context.Background()` timeout。 |
| 同一个 wrapped tx double finish | 第二次返回 `pgx.ErrTxClosed`；不得重复底层 commit/rollback、unlock、release 或 discard。 |

### 5. Good / Base / Bad Cases

- Good：shared classifier 在 locked read-only snapshot 中返回 exact `FINALIZED`；R2/startup route 不调用 R1 verifier/predicate，commit、matching session-shared unlock、release 各一次。
- Base：shared classifier 返回 exact `R1`；同一 locked `REPEATABLE READ, READ ONLY` tx 依次完成 frozen verifier 与 direct-runtime predicate，再 commit/unlock/release。
- Bad：`PREPARED` 或 `CORRUPT` 被 classifier 识别后仍调用 frozen verifier、direct-runtime predicate、R2 parser 或 admission；必须拒绝且这些额外调用次数为零。
- Bad：R1 direct-runtime identity mismatch，或 lock key、seed、mode、scope、owner lifecycle 任一漂移；必须 fail closed，不得让 connection 以可复用状态返回 pool。

### 6. Tests Required

```bash
go test ./internal/center/store/migrate \
  -run 'AppACLR2(State|RuntimeAdmission|Startup)|AppACLRuntimeAdmission|Canonical.*V1' -count=1
```

- Focused unit tests 必须断言三个 public API 的 exact-state routing、single reserved connection、lock-before-snapshot、一个 `REPEATABLE READ, READ ONLY` tx、R1 同-tx shared classifier/verifier/predicate，以及 FINALIZED 的 classifier-only trace。
- 必须保留 adversarial `TestAdmitAppACLR2RuntimeRejectsR1ToPreparedRace`：R1 classifier 暂停于 shared lock 持有期间时，bootstrap exclusive transition 不能 acquire/commit；admission 结束后 bootstrap 才能 commit `PREPARED`；随后 fresh admission 只能 classify 后拒绝，且 paired PREPARED assertion 证明没有额外 frozen/R2 call。
- exact-R1 frozen verifier error propagation 是当前 相关协议 gate：injected sentinel 必须返回 `AppACLR2StateCorrupt`，`errors.Is` 保留 primary error，direct-runtime predicate 调用次数为零；deferred rollback 成功时 unlock/release，rollback failure 只 discard，不能以 `errors.Join` 对外返回 deferred rollback error。
- `StartAppACLR2Runtime` 必须有 `go/parser` AST static binding proof：其 sole return expression 直接调用 `AdmitAppACLR2Runtime(ctx, db)`，不得只做 source substring assertion，也不得依赖 production seam。
- public nil-pool defensive check 可保留。nil dependency、acquire/nil connection、session-lock、begin/nil tx、classifier/predicate injection、commit/rollback/unlock/cancel/double-finish 等 internal seam fault cases 是 optional hardening，不是当前 相关协议 acceptance gate；保留现有 race、lock-identity 与 fault tests 时，不得把它们扩写为新公开合同。
- Lock mutation tests 必须对 key、mode、seed、scope 和 owner lifecycle 敏感；race fixture 只用于证明这些要求和并发阻塞，不能作为生产 PostgreSQL semantics 的来源。
- 真 PostgreSQL 16 authority/physical-lock evidence 明确属于 相关协议；在该 lane 未运行时，不得写成已验证的 PG16 结论。

### 7. Wrong vs Correct

#### Wrong

- 创建 runtime-private classifier，或让 `FINALIZED` 调用 R1 verifier/direct-runtime predicate。
- 先开始 snapshot 再加锁，或用 xact advisory lock 代替 session-shared lock。
- 只比较锁名称、未同时绑定 exact key/seed/mode/scope，或让 unlock 使用不同 lock identity。
- 在 caller cancellation 后用其 canceled context release/discard connection，随后把污染连接交回 pool。

#### Correct

- 只复用 shared classifier；exact R1 在一个 locked snapshot 中串联 frozen verifier 与 direct-runtime predicate，`FINALIZED` 只消费 classifier。
- 在第一个 snapshot query 前，用 exact key + seed `0` 的 session-shared lock 固定单一 connection/`REPEATABLE READ, READ ONLY` snapshot。
- 对 key/mode/seed/scope/owner lifecycle 做精确匹配；每次成功 finish 后 matching unlock 再 release，任何异常走 fresh bounded discard cleanup。

---
