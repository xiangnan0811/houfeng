# Record Activity Projection Contract

> 修改 `internal/center/activity/`、`internal/center/store/record_activity*.go`、
> `0057_create_record_activity.sql`、subject activity / VPS overview 读取路径，
> 或 activity export readiness 时，必须加载本文件。

## Scenario: Projection scan, auth digests, and export readiness

### 1. Scope / Trigger

- Trigger：投影扫描分页、`auth_scope_digest` 盖章、viewer allowlist、checkpoint /
  `ActiveGeneration`、Export `Readiness`、subject timeline / VPS overview freshness，
  或修改 projection publisher 的去重查询、head lock 与 current runtime ACL。
- 权威：`activity` 包合同 + Postgres 投影表；禁止在 handler/web 重述投影语义。

### 2. Signatures

```go
type ScanWindow struct {
	From         time.Time
	Through      time.Time
	AfterEventID string // exclusive keyset at From; empty = include From
}

func AuthFilterForActor(actor recordauth.ActorScope) (AuthFilter, error)
func ProjectAuthScope(projectID recordauth.ProjectID) (recordauth.ResourceScope, error)
func AuthScopeFromVisibility(visibility recordauth.VisibilityScope) recordauth.ResourceScope
func (repository *ActivityProjectionRepository) ActiveGeneration(ctx context.Context) (uint64, error)
func (source *RecordDomainActivitySource) Readiness(ctx, ExportScope, SourceHead) (SourceReadiness, error)
func PublishActivityBatch(ctx context.Context, pool *pgxpool.Pool, generation uint64, candidates []activity.CandidateEvent) (ActivityPublishResult, error)
```

- Runtime ACL：`record_activity_projection` 精确允许 table `SELECT/INSERT/DELETE`，禁止 table `UPDATE` 且不得存在 column ACL；`record_activity_projection_heads` 与 revision intervals 保留生产所需 `UPDATE`。

### 3. Contracts

- Scan：`(recorded_at, source_event_id)` keyset。同刻满页必须用 `AfterEventID` 前进；
  禁止只抬时间下界导致永久 stalled。
- Durable checkpoint 仍只存 `RecordedThrough`；跨 pass 可幂等重读边界瞬间。
- `record_domain` 必须从 revision `visibility_scope` + `visibility_digest` 盖章；
  系统事实四路（monitoring/command/evidence/asset）可用 `ProjectAuthScope`。
- Viewer allowlist 仅 project-visibility digest；空 digest（`sha256(nil)`）不得放行。
  Admin unrestricted。
- `ActiveGeneration` 只读 `head_state = 'active'`。
- Export `Readiness.CaughtUp` 读 active generation checkpoint，禁止写死 `true`。
- Subject list `Freshness.State` 不得被全局 projector source health 翻掉；
  `source_statuses` 可作诊断，overview recent section 用可见信号。
- publisher/rebuild 必须先 `SELECT ... FOR UPDATE` 锁定 `(project_id, active_generation)` 的 active head；该 head 是 candidate 分类、sequence 分配和 watermark 推进的串行化根。
- 持有 active head lock 后，已有 projection identity/canonical hash 只做普通 `SELECT`。禁止为读取 immutable fact 添加 `FOR UPDATE`，因为 PostgreSQL 会要求事实表 `UPDATE` ACL，且缺失候选本来也无法被行锁锁定。
- candidate 必须分类为 insert、exact duplicate 或 canonical hash mismatch；mismatch 整批回滚，insert 与 head watermark 同事务提交，最终 strict insert/unique constraint 是最后防线。
- 不得为 publisher 新增 migration、扩大 projection UPDATE/column ACL 或做现场 GRANT；current exact-baseline 的生产升级边界必须保持不变。

### 4. Validation & Error Matrix

| Condition | Expected |
| --- | --- |
| 同刻 ≥ page size 行 | keyset 排完；不得永久 stalled |
| 受限 revision + plain viewer | 时间线不可见该行 |
| 项目可见 + viewer | 可见 |
| Admin | 可见全部 |
| Incremental head 作 export readiness | 拒绝 |
| Settled head 但 checkpoint 未 caught up | `CaughtUp=false` |
| 退役 generation | `ActiveGeneration` 不选中 |
| runtime 首次 publish | 在 current ACL 下成功插入 facts 并连续推进 head watermark |
| exact retry | 不重复 facts，不改变 canonical hash，watermark 保持连续 |
| same identity / different canonical hash | 整批失败并回滚 facts、subjects、published/allocated watermark |
| 两个 runtime publisher 同一 active generation | 由 active head lock 串行化；只提交 canonical facts，sequence 无 gap/回退 |
| projection hash 查询带 `FOR UPDATE` | direct-runtime PG16 返回 `42501`；必须改查询，不能授予 UPDATE |

### 5. Good / Base / Bad Cases

- Good：`ScanWindow{From:T, AfterEventID:id}` 跳过已消费同刻行。
- Good：publisher 先锁 active head，再用普通 projection SELECT 分类；first/retry/concurrent publish 在 current runtime ACL 下均成功且 watermark 连续。
- Base：空 `AfterEventID` 从 `From` 起读。
- Bad：满页后 `from = pageMax` 且无 AfterEventID → 死循环/stall。
- Bad：一律 `ProjectAuthScope` 盖受限记录 → viewer 仍可见。
- Bad：给 immutable projection fact 的分类 SELECT 加 `FOR UPDATE`，迫使 runtime 获得不应有的 UPDATE 权限。
- Bad：绕过 active head lock 依赖事实行锁；两个 publisher 可同时把同一缺失 identity 判为 insert。

### 6. Tests Required

- Projector：同刻满页可 catch up；keyset 违规页 fail-closed。
- `TestPostgresIntegrationRecordDomainRestrictedVisibilityHidesFromViewer`。
- Readiness：无 checkpoint → 未 caught up；写入 caught_up 后为 true。
- AuthFilter viewer：不含 `sha256(nil)`。
- `TestPostgresIntegrationRecordActivityRuntimeACL` 必须用 current convergence 后的 direct runtime role，在 PostgreSQL 16 实际覆盖 first insert、exact retry、mixed hash mismatch rollback、两个 publisher 与后续连续 sequence/hash/watermark；catalog 同时断言 projection `S/I/D=true,U=false,column ACL=0`。
- deterministic contention test 必须真实持有 active head lock，证明 contender 等待、rollback 后 sequence 可复用；catalog-only、fake tx 或 SKIP-as-pass 不构成并发证据。
- migration/current ACL compiler diff 必须为空；不得用新 fragment 或现场 GRANT 让 acceptance 变绿。

### 7. Wrong vs Correct

- Wrong：adapter 不盖章 + viewer 放行空 digest。  
  Correct：权威 visibility 盖章；viewer 只认 project digest。
- Wrong：`CaughtUp: true` 写死。  
  Correct：读 `record_activity_projection_checkpoints.caught_up`。
- Wrong：`SELECT ... FROM record_activity_projection ... FOR UPDATE` 后补 projection `UPDATE` grant。
  Correct：先锁 active head，事实分类使用普通 `SELECT`，保持 projection immutable ACL。
