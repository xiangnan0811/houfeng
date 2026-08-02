# 活动投影、单主体页面与 VPS 概览设计

## 0. Development rebaseline

`0057` 与 current APP ACL fragment 是一个原子交付。activity 只消费当前 Records/Collaboration/Evidence 和现有权威系统事实，不接入 `experience_logs`。feature flag 是开发期集成/回滚工具，不承担 staging cutover 或旧数据 compatibility。

## 1. 边界与依赖

本任务新增 `internal/center/activity/` 和 `internal/center/vpsoverview/` 两个单向依赖模块：activity 只把已授权的权威 source event 投影为可重建 read model；vpsoverview 只聚合 VPS 身份/健康/续费/关系与 activity 首屏，不拥有任何写操作。HTTP handler 只解析协议，PostgreSQL 查询位于 `internal/center/store/`，Web route controller 位于 `web/src/pages/`。

直接依赖为 2、4、6、9。records core 的 `record_domain_activities` 是记录、revision、评论和行动项的统一 source seam，因此本模块不 import collaboration 包；但没有 task 9 就不能完成评论/行动项 source 的真实集成验收，所以它不是可省略的“顺序前置”。task 6 提供 versioned cursor/query canonicalization。task 4 提供 immutable evidence summary/renderer DTO。一级 `/records` 仍由 task 6 拥有。

## 2. 0057 schema 与 canonical envelope

`db/migrations/0057_create_record_activity.sql` 创建：

- `record_activity_projection`：`activity_id`、全局唯一 `ingest_sequence`、`event_kind`、`event_at`、`recorded_at`、`source_kind/source_event_id/source_version`、可空 typed `record_id/revision_id/evidence_snapshot_id`、`backfilled`、可空 `actor_id`、`severity`、版本化 allowlisted `presentation_json`、`auth_scope_digest`、`canonical_hash`、可空 `corrects_activity_id` 和投影时间；
- `record_activity_subjects`：activity、`subject_kind/id`、`primary|related`、稳定顺序、allowlisted identity snapshot、可空 live route、tombstone 与 capture/final authorization-floor reference；为保证所有查询 predicate 在 LIMIT 前生效，还事务性冗余并校验 `event_kind`、`source_kind`、`event_at/recorded_at/ingest_sequence`、可空 `record_id/revision_id/evidence_snapshot_id`、`auth_scope_digest` 与canonical row hash；
- `record_activity_projection_heads`：每个 deployment/project generation 一行，保存单调 `projection_generation` 与 `published_ingest_sequence`。projector final publish transaction 对该行 `SELECT ... FOR UPDATE`，在锁内分配连续 batch、写 projection/subjects/revision intervals/checkpoint并推进published head；rollback不消耗编号，后续batch不能越过尚未提交的低序号事务；灾难性empty rebuild开始前原子递增generation并重置该generation head；
- `record_activity_projection_checkpoints`：source kind、source authoritative-head/cursor digest、lease owner/expiry、last success/error code、attempt；checkpoint只在同一published batch transaction中推进，不以数据库sequence的`max()`或已分配未提交值充当完成水位；
- `record_activity_revision_intervals`：`record_id/revision_id`、`valid_from_ingest_sequence`、可空 `valid_to_ingest_sequence` 与canonical source identity；新正式revision投影时关闭上一interval并开启下一interval，历史interval不删除/覆盖；
- published head、subject + 全过滤维度 + event sort、revision validity、source identity 和 correction 索引。

唯一约束为 `(source_kind, source_event_id, source_version, event_kind)`。`activity_id` 是 `act_` + deployment/project namespace与该完整source identity的长度前缀canonical bytes之SHA-256截断base32；碰撞时比较完整identity/hash并失败，不追加随机suffix。同一 source identity 重试只接受相同 canonical hash；hash 不同是 source contract violation，不能 UPDATE 覆盖。`ingest_sequence` 不是裸 PostgreSQL sequence：worker可并发准备/扫描，但最终publish batch必须持有generation head行锁直到commit；锁内先按完整source unique key查询并锁定全部candidate，existing row逐项比对canonical hash且不分配新序号，只有确认缺失的rows按确定顺序取得恰好连续的范围并使用普通严格`INSERT`。final publish路径禁止`ON CONFLICT DO NOTHING`吞掉已分配编号；意外unique conflict使整个transaction rollback并重试locked classification。这样分配顺序、提交顺序和`published_ingest_sequence`一致；若低序号batch延迟或rollback，高序号batch保持阻塞或重新取得连续范围。projection 是可删除重建的派生数据；在线repair采用“枚举权威 source → insert missing → 校验 existing hash → 标记不再存在的非法 projection”。灾难恢复从空投影重建会先递增`projection_generation`，可以重新分配ingest sequence；deterministic activity ID与`(event_at,recorded_at,source_kind,activity_id)`业务全序保持一致，但所有旧generation cursor返回`cursor_expired`，不得跨generation续页。

canonical DTO：

```go
type Event struct {
	ActivityID     string
	EventKind      EventKind
	EventAt        time.Time
	RecordedAt     time.Time
	IngestSequence uint64
	Source         SourceIdentity
	Backfilled     bool
	Actor          *ActorSnapshot
	Subjects       []SubjectSnapshot
	Presentation   Presentation
	Corrects       *string
	AuthScope       recordauth.ResourceScope
}
```

`Presentation` 是按 `event_kind + version` 注册的安全结构，不接收 arbitrary JSON。title/summary 只来自 adapter allowlist；Markdown 正文、附件名、raw event payload、raw URL 和 command output 不进入系统事实。`Subjects`、API `items`、`source_statuses` 与 overview `anomalies` 在 Go 边界初始化为空 slice，JSON 永不返回 `null`。

## 3. Source adapters、event time 与 projector

`activity.SourceAdapter` 提供 `Kind`、`ScanAfter(..., throughHead)`、`Canonicalize` 和 `AuthorizeSnapshot`，并实现导出的 `ExportReadySourceAdapter` seam：scope-bounded `AuthoritativeHead` 与 `Readiness(head)`。`AuthoritativeHead`只能返回该source已经committed且可连续枚举的无内容prefix；`Readiness`证明指定head当前可读、contract version受支持且不会被retention截断；`ScanAfter`不得越过head。projector checkpoint保存head digest与最后cursor，只有完整扫到head且publish transaction提交后才声明ready。`ActivityExportReader.Readiness(scope, selection)`在同一generation聚合所有已注册source的authoritative head/adapter readiness/projected checkpoint向量，供task10固化导出activity archive；任一source unknown/unreadable/truncated/behind都失败关闭，不允许根据时间或全局sequence猜完整性。

activity child自己冻结并导出以下接口，task10只能消费，不能回头修改activity实现来补接口：

```go
type ActivitySnapshot struct {
    ProjectionGeneration    uint64
    PublishedIngestSequence uint64
    ReadinessDigest         [32]byte
}

type ExportReadySourceAdapter interface {
    SourceAdapter
    AuthoritativeHead(context.Context, ExportScope) (SourceHead, error)
    Readiness(context.Context, ExportScope, SourceHead) (SourceReadiness, error)
}

type ActivityExportReader interface {
    Readiness(context.Context, recordauth.ActorScope, RecordSelection) (ReadinessVector, error)
    ScanRecordPage(context.Context, recordauth.ActorScope, RecordSelection, ActivitySnapshot, PageCursor) (ActivityPage, error)
}
```

`ReadinessVector`必须内含同一个`ActivitySnapshot`和逐source head/readiness/checkpoint/hash；`ReadinessDigest`覆盖normalized actor/record selection、generation、published head与完整vector。`ScanRecordPage`只读取该snapshot内与选定record有权关联的versioned envelopes并按canonical keyset分页；generation/digest不符、source vector漂移或未caught-up均失败，不接受裸sequence。它是内部export seam，不把global generation/head暴露给浏览器响应。首批 adapter：

| Source | 稳定 identity / event time | 投影边界 |
|---|---|---|
| `record_domain_activities` | domain event ID + version；revision 1 用 `occurred_at`，后续 revision 用 `saved_at` | revision 1 与 record-created 合并；只投影 title/type/status/版本入口，不复制正文 |
| `evidence_snapshots` | snapshot ID + schema；实际 observed end | 显示 captured time、覆盖、桶宽、质量、source state 和 snapshot route |
| renewals/price/IP/spec/lifecycle | 各表稳定 ID + row version；decided/changed/captured/occurred time | 保持系统事实，不转换为人工记录 |
| `state_change_events` | event ID + write-time provenance version；权威 observed/occurred time | 只读取 task 4 已固化的 provenance/quality 字段，不从已清 raw facts 猜 live/backfill；关联到 VPS 时按 `event_at` 落在 link/unlink 区间内的历史关系，不读取当前 link 猜过去 |
| command audit | audit/event ID + immutable event type；`occurred_at` | metadata only，永久排除 stdout/stderr/details content |

评论/行动项由 task 9 写入 `record_domain_activities`，并绑定事件发生时的 revision/subject snapshot，因此 activity projector 不读取 mutable current record 猜历史主体。纠正产生新的 source version 和 `corrects_activity_id`。所有时间转 UTC；`recorded_at` 始终是候风接受/保存该 source event 的时间。`recorded_at > event_at` 且来源为 migration、晚到系统事实或晚捕获证据时标 `backfilled=true`，但普通保存延迟不凭时间差自动推断。

本任务不投影 `experience_logs`，Child 10 也不会转换它。backfilled 仅表示当前权威系统 source 的晚到/修正事实，不表示 legacy Records 迁移。

projector 使用 platform worker lease 和 source checkpoint，先在head外准备canonical batch，再在短final transaction中锁定generation head行、对candidate unique keys做locked existing/hash分类、只给missing rows分配连续范围并用严格insert，随后原子写subjects/intervals/checkpoint/`published_ingest_sequence`；final publish禁止`ON CONFLICT DO NOTHING`。record/evidence transaction 的 outbox 触发低延迟增量，周期扫描修复丢失触发。任务携带 source version、reservation epoch 和 deletion fence；最终 insert 前复查，旧 epoch 迟到任务只清理/告警，不能复活已删除 projection。并发测试必须让worker A持有head锁和低range后延迟、worker B尝试发布高range，证明B不能先commit且任何first-page as-of都不包含空洞；另以全重复/部分重复retry证明existing rows不消费序号，published range始终无洞。

## 4. Query、cursor 与 API

`GET /api/subjects/:type/:id/activity` 支持：

- `view=activity|records|evidence`；
- 重复 `source`、`event_kind`（同字段 OR，字段组 AND）；
- `from`/`to` 绝对 RFC3339；
- `versions=current|history`；
- `limit` 默认 50、最大 100；
- opaque `cursor`。

首次查询在 authorization 后读取 committed-contiguous published head并在服务端固定 `projection_generation + as_of_ingest_sequence`。relation候选SQL必须在ORDER/LIMIT前应用 subject/auth/fence、`ingest_sequence <= as_of`、view/source/event-kind/time与keyset；`versions=current`在同一候选阶段用typed record/revision列semi-join固定水位的revision validity interval。之后才取limit+1个ID并PK joinpresentation，一次查询所有来源。稀疏filter不允许靠固定overfetch补洞。复用 task 6 `recordcursor` confidential codec，namespace 为 `subject-activity/v1`，加密payload包含normalized query hash、auth scope hash、generation、as-of和完整末项键；AES-GCM token固定长度桶、随机nonce且客户端不可解码/比较。query/auth变化返回`cursor_invalid`；generation变化或超出保留版本返回`cursor_expired`并给同query首屏恢复URL。

响应：

```json
{
  "subject": {"kind":"vps","id":"vps_x","identity":{},"live_route":"/vps/vps_x","status":"live"},
  "view":"activity",
  "snapshot_cursor":"opaque-confidential-token",
  "freshness":{"state":"ready","visible_observed_at":null,"new_items_available":false,"reason_code":""},
  "items":[],
  "source_statuses":[],
  "next_cursor":null
}
```

`projection_generation`、`as_of_ingest_sequence`、`current_ingest_sequence`、global checkpoint与global last-success time只存在服务端/加密token中，永不成为响应字段。`visible_observed_at`只取当前subject/query/auth范围内可见项的最大观测时间；空scope为`null`。`new_items_available`通过相同授权和全部query predicates检查published head后是否存在可见relation，隐藏scope活动不能改变它。`source_statuses`仅返回`ready|stale|unavailable`和安全reason code，不返回会随其他权限范围推进的checkpoint/sequence/精确worker时间。token每次使用随机nonce且不可比较，Web只存储/回传。

`records` 是 `event_kind in (record_created, record_revision, record_state_changed, record_visibility_changed)` 的 relation-stage server filter；`evidence` 是relation中typed evidence snapshot filter。`versions=current` 在加密cursor的 `as_of_ingest_sequence` 上查询 `record_activity_revision_intervals.valid_from <= as_of < valid_to`（open interval用无穷上界），并在LIMIT前按relation的`record_id/revision_id` semi-join，绝不 join 会在页间推进的 records live current pointer；`history` 才包含旧 revision events。interval 与activity由同一published batch transaction推进，retry核对source/hash，刷新取得新水位后才改变current membership。subject 已删除时，service 从仍有权 projection 返回 tombstoned identity；没有存续授权 projection 时统一 404。projection/fence 不健康返回 503 `activity_projection_unavailable`，不能回退到未授权 source UNION。

## 5. VPS overview read model

`GET /api/vps/:id/overview` 由 `vpsoverview.Service` 并发调用受限 source reader，并在一个 request-scoped `generated_at` 下返回：

```go
type Overview struct {
	GeneratedAt    time.Time
	Identity       Identity
	Anomalies      []Anomaly
	Summary        Summary // overall, monitoring, ip_quality, renewal
	RecentActivity ActivitySection
	Facts          []Fact
	Relations      []RelationSummary
	Capabilities   []string
}

type SectionState struct {
	State         string // ready|stale|unavailable
	ObservedAt    *time.Time
	LastSuccessAt *time.Time
	ReasonCode    string
}
```

identity/not-found 是 fatal。监控、IP、订阅、relations、activity 各自返回 `SectionState`；超时预算到达后只降级该区。recent activity 调用同一 activity service，固定首屏水位并取 5 条，不另写“最近记录”查询。activity区的`ObservedAt/LastSuccessAt`不得读取全局projector checkpoint：只使用当前VPS/query/auth scope可见项的`visible_observed_at`，没有可见项为null；worker健康只给安全state/reason code。隐藏scope活动推进不能改变overview activity freshness、recent count或anomaly。Facts 只含稳定身份/配置，Summary 不复制 Facts；relations 只给监控实例、订阅、服务、域名的数量、最近状态和明确 route。

anomaly registry 使用 versioned rule ID、severity、事实来源、影响、freshness 和最多一个 primary/两个 secondary actions。规则只在事实命中时产生条目：健康/监控异常、IP risk/stale/partial、续费临期/缺有效订阅、生命周期 blocker 或会使判断不可信的 source unavailable。排序为 severity、event time、rule ID。没有命中时必须返回 `[]`；不存在 `healthy_placeholder` rule。异常恢复只是下一次 read model 不再返回条目，不写记录状态。

## 6. Web 边界与响应式

Web 新增三个复用同一 controller/query codec 的 route page：`SubjectActivityPage`、`SubjectRecordsPage`、`SubjectEvidencePage`。它们从显式 route map 解析 VPS/monitoring instance/Target，不接受 URL 任意 subject kind。`SubjectIdentityBar` 与 `UnifiedTimeline` 是纯受控跨页组件；Link、筛选和返回行为由 page 传入。

`UnifiedTimeline` 按 event date 分段，人工记录使用圆形+文字“人工记录”，系统事实使用菱形+“系统事实”，证据使用方形+“不可变证据”；颜色只是附加通道。证据行显示 actual coverage/bucket/quality，人工行进入固定 revision，系统行无编辑动作。view 变化规范化 URL 并清 cursor；append 保持既有 items，head 前进显示“有新活动”刷新提示。

现有 `VPSDetailPage.tsx` 拆为 feature-gated composition：旧实现移动到 `web/src/pages/vps-detail/LegacyVPSDetail.tsx` 保持回退；新 page 使用 `useVPSOverview` 和纯展示 sections。管理表单状态移入 `useVPSManagementController`/受控 modal owner，不继续由 overview page 持有。新身份头严格只有新建记录、时间线、管理三项首层动作；local nav 位于第二行。`anomalies.length === 0` 时 JSX 分支完全不创建 anomaly section。

390px 依 Artifact v1：identity/actions → 实际 anomaly（若有）→ 2×2 summary → 最多 3 条 recent → facts → relation links。subject filters 进入 Modal-based drawer；时间线保留必要字段。所有宽内容只有具名、可聚焦的局部 scroll owner，document 不横向溢出。

## 7. 失败、兼容与回滚

- activity adapter 单源失败：保留其他已知条目，`source_statuses` 只显示该 source 的安全状态/reason code和当前授权scope的可见观测时间；全局checkpoint、sequence与精确worker last-success不出现在用户响应中，不得把剩余来源称为完整。
- cursor/source/auth/fence 失败：分别使用稳定错误码；权限拒绝与不存在统一 404。
- overview section failure：显示最后成功与 retry；若影响总体判断，anomaly 只给一次简短影响说明。
- revoke/permanent delete：client content lease shell 先遮蔽，停止请求并清 route state；focus/pageshow/reconnect 重鉴权后才恢复。
- integration：API、routes 和 new VPS page 受 `records_v2_read` 服务端 capability 控制；Child 11 在完整功能矩阵通过后验证默认行为，不需要 staging 对照。
- rollback：停止 projector并关闭 capability即可恢复 legacy page/API；0057 与 projection 保留。不得执行 down migration、删除新 records/evidence，或用旧五数组 timeline 伪装 canonical projection。

在线永久删除由 `activity.DeletionAdapter`拥有本领域范围：在record reservation/epoch生效后阻止新projector publish，等待旧batch退出，删除目标record关联的presentation主行、全部subject relations、revision intervals和overview recent/summary cache，再独立verify零命中并返回无内容receipt。它不删除records/evidence/collaboration/search权威或投影。`activity.RecoveryAdapter`只在隔离恢复中清空/重建activity域；两者都读取core tombstone/fence，旧checkpoint或outbox不能重新发布已删内容。

## 8. 性能与验证

`record_activity_subjects` 作为可重建关系投影，事务性冗余 `ingest_sequence/event_kind/event_at/recorded_at/source_kind/activity_id/record_id/revision_id/evidence_snapshot_id/auth_scope_digest`，并以canonical row hash验证它们与projection主行一致；它们不是第二业务权威。通用有序索引为 `(subject_kind,subject_id,event_at DESC,recorded_at DESC,source_kind ASC,activity_id ASC) INCLUDE (ingest_sequence,event_kind,record_id,revision_id,evidence_snapshot_id,auth_scope_digest,role)`；另有 `(subject_kind,subject_id,event_kind,event_at DESC,recorded_at DESC,source_kind ASC,activity_id ASC)` 与 `(subject_kind,subject_id,source_kind,event_at DESC,recorded_at DESC,activity_id ASC)` 过滤索引，以及 `(subject_kind,subject_id,ingest_sequence)` watermark/rebuild索引。revision interval使用 `(record_id,revision_id,valid_from_ingest_sequence,valid_to_ingest_sequence)` 可用索引。planner可按query选择索引，但所有view/source/kind/time/auth/as-of/current-validity条件必须在ORDER/LIMIT前生效；然后取limit+1 IDs并第二阶段PK join≤101行presentation。EXPLAIN与sparse-filter fixture共同证明无先LIMIT后过滤、固定overfetch、全量join后sort或N+1。

overview source 请求有总预算和单区预算，不进行 per-row N+1。固定 seed 为 1,000,000 activities/10,000 records/200,000 revisions；时间线 50 条 p95≤1s、overview p95≤750ms。报告保存三轮 p50/p95/p99、错误率、query count 和 `EXPLAIN (ANALYZE, BUFFERS)`；最终混合负载由 task 11 按父协议复跑。
