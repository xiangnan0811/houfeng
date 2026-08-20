# Record Activity Projection Contract

> 修改 `internal/center/activity/`、`internal/center/store/record_activity*.go`、
> `0057_create_record_activity.sql`、subject activity / VPS overview 读取路径，
> 或 activity export readiness 时，必须加载本文件。

## Scenario: Projection scan, auth digests, and export readiness

### 1. Scope / Trigger

- Trigger：投影扫描分页、`auth_scope_digest` 盖章、viewer allowlist、checkpoint /
  `ActiveGeneration`、Export `Readiness`、subject timeline / VPS overview freshness。
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
```

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

### 5. Good / Base / Bad Cases

- Good：`ScanWindow{From:T, AfterEventID:id}` 跳过已消费同刻行。
- Base：空 `AfterEventID` 从 `From` 起读。
- Bad：满页后 `from = pageMax` 且无 AfterEventID → 死循环/stall。
- Bad：一律 `ProjectAuthScope` 盖受限记录 → viewer 仍可见。

### 6. Tests Required

- Projector：同刻满页可 catch up；keyset 违规页 fail-closed。
- `TestPostgresIntegrationRecordDomainRestrictedVisibilityHidesFromViewer`。
- Readiness：无 checkpoint → 未 caught up；写入 caught_up 后为 true。
- AuthFilter viewer：不含 `sha256(nil)`。

### 7. Wrong vs Correct

- Wrong：adapter 不盖章 + viewer 放行空 digest。  
  Correct：权威 visibility 盖章；viewer 只认 project digest。
- Wrong：`CaughtUp: true` 写死。  
  Correct：读 `record_activity_projection_checkpoints.caught_up`。
