# 活动投影、单主体页面与 VPS 概览

## Goal

交付 canonical 活动投影、VPS/监控实例/Target 的单主体纵向工作区，以及任务导向的 VPS 30 秒概览；人工记录、系统事实和不可变证据在展示层按真实发生时间合流，但继续保持各自权威性、授权和可编辑边界。

## Requirements

- 父设计：`../07-13-vps-detail-experience-design/design.md` §5–§8、§16、§18–§25；视觉基线：`../07-13-vps-detail-experience-design/research/visual-design-contract.md` Artifact `vps-records-visual-contract/v1` §2、§3.2、§6。
- 直接依赖：子任务 2（records core）、4（evidence）、6（record search/cursor）和 9（collaboration）已合入 `main` 且 post-merge CI 通过。子任务 9 仍通过 core 的 `record_domain_activities` source contract 解耦编译，但它是“评论/行动项进入完整时间线”可交付、可验收的真实前置；依赖与父任务表一致。
- 使用父任务预留的 `db/migrations/0057_create_record_activity.sql`。本任务启动时 0051–0056 已经合入并全部冻结；若 0057 被任务外受保护主线占用，只顺延尚未发布的 0057–0060 并同步父表/source/upgrade tests，禁止改名、重排或改写 0051–0056。
- canonical activity 必须保存 `event_kind`、`event_at`、`recorded_at`、全局单调 `ingest_sequence`、稳定 `source_kind/source_event_id/source_version`、`backfilled`、actor、primary/related subject identity snapshot、权限范围、allowlisted presentation 和 correction link。`ingest_sequence` 不使用 PostgreSQL sequence 的 `max()` 冒充可分页水位；projector 在单一 generation head row 上以事务行锁串行分配连续 batch，并与 projection/relations/revision intervals/checkpoint 在同一事务提交后原子推进 `published_ingest_sequence`，因此水位内不存在尚未提交的低序号空洞。`activity_id` 由deployment/project namespace与完整source identity的长度前缀canonical bytes确定性哈希生成；唯一 source identity 保证 live projector、retry 和从空投影rebuild后ID与业务全序不漂移，也不原地改写事实。cursor另绑定单调`projection_generation`；会重分配ingest sequence的灾难重建必须递增generation并让旧cursor明确过期，不能假装原cursor仍可续页。
- event-time 规则固定为：新记录/revision 1 使用用户确认的 `occurred_at` 且只生成一个创建事件；后续正式 revision/状态/可见范围变化使用本次 `saved_at`；legacy/晚到事实保留原发生时间并标 `backfilled`；系统事实使用来源权威发生时间；证据使用实际观测终点并另显捕获时间；评论/行动项使用自身不可变变更时间。纠正通过新 correction event 引用旧事件。
- 首批 source adapter 覆盖 `record_domain_activities`、evidence snapshots、续费/价格/IP/规格与生命周期事实、监控 `state_change_events`、command audit metadata；不得复制 raw observation、命令 stdout/stderr、任意 event payload 或自由 Markdown 到系统事实 presentation。
- 每个 `SourceAdapter` 除扫描与规范化外，必须提供 scope-bounded `AuthoritativeHead` 和 `Readiness`：head 是可比较、无内容的 committed-contiguous source prefix，scan 明确受该 head 截止，readiness 证明 source 可枚举且 checkpoint 已追到目标 head。projector 通过 outbox/worker lease 增量运行，按 source identity 幂等插入并保存 checkpoint；重建只补缺并核对 canonical hash，不 truncate 后重新发号。发现同一 source identity 对应不同 canonical bytes 时停止该 source、暴露安全错误并等待修复。该合同同时是 task 10 导出 activity 完整性向量的唯一来源，缺任一 adapter head/readiness 时导出失败关闭。
- `GET /api/subjects/:type/:id/activity` 是 VPS、monitoring instance、Target 三种主体和 `activity|records|evidence` 三个局部视图的唯一查询合同。`records` 只过滤人工记录/正式 revision 事件，`evidence` 只过滤不可变证据；不得另建第二份列表、浏览器合并来源或按来源分别 `LIMIT` 后拼接。
- 首请求在服务端固定已发布的 committed-contiguous `as_of_ingest_sequence`，并在该水位内按 `(event_at DESC, recorded_at DESC, source_kind, activity_id)` 排序；task 6 的 confidential cursor绑定规范化 query、授权 scope、generation、水位和完整末项排序键，客户端与响应正文不得看到、解码或比较全局 generation/head。`view`、`source`、`event_kind`、时间、授权、fence 与 `versions=current` validity 必须全部在关系候选的 ORDER/LIMIT 之前执行；禁止先取 101 个 ID 再过滤造成稀疏查询短页/跳项。`versions=current` 的“当前修订”由 0057 中按 ingest sequence 维护的 revision validity interval在同一水位解析，禁止分页时 join records live current pointer。分页期间新到达、修订推进或晚到事件只在刷新后的新水位改变成员关系，并明确标识回填。
- 单主体 query/URL 覆盖 view、source、event kind、绝对起止时间、current/history revision scope 和 cursor；非法、过期、跨 query 或权限变化的 cursor 给出稳定恢复语义，不静默跳页。
- subject 删除后，仍获授权的记录/证据保留当时 identity snapshot 和 tombstone；不得按名称重连。无权用户在时间线、计数、空态、cursor、recent activity 和 overview 中均看不到对象或活动存在性。
- activity API 只返回授权 query-scope 的 `new_items_available` 与安全 freshness state；不得返回全局 `projection_generation`、`as_of_ingest_sequence`、`current_ingest_sequence`、全局 checkpoint 或会随隐藏活动推进的精确时间。`snapshot_cursor`/`next_cursor` 为固定长度桶的不可比较 confidential token；只有同一 subject/query/auth scope 中新增的可见活动才能令 `new_items_available=true`。
- `/vps/:id/activity`、`/vps/:id/records`、`/vps/:id/evidence` 是 VPS-local 深链；monitoring instance 和 Target 提供同义子路由。页面共享轻量 `SubjectIdentityBar`、概览/活动/记录/证据局部导航、返回来源与规范化筛选，不创建固定大标签壳。
- `/vps/:id/records` 复用项目级 `/records` 的服务端记录筛选语义；`新建记录` 进入 `/records/new` 并预选当前 VPS primary subject，保留 `return_to`。一级 `/records` 与其侧边栏入口仍由子任务 6 拥有，本任务只完成 VPS-local 集成。
- `GET /api/vps/:id/overview` 是专用只读聚合，不替代既有写 API。DTO 具有统一 `generated_at`、身份、动态 anomalies、综合状态/监控/IP 质量/续费、最近活动、稳定资产事实、关联上下文、capabilities，以及每区 `ready|stale|unavailable`、观测时间、最后成功时间和安全 reason code；所有 Go slice 对 JSON 保证 `[]` 而不是 `null`。
- VPS 稳定态首屏只显示常规身份、状态、关系和真实最近活动；`anomalies=[]` 时前端不得渲染异常标题、容器、占位高度、禁用动作或 `动作：无`。异常、临期、缺证据、来源不可用或生命周期阻塞由版本化 rule ID 按严重度/时间稳定排序后动态插入；事实恢复后对应 block 从 DOM 和布局移除，且不会自动关闭人工记录。
- 身份头首层动作固定为 `新建记录`、`查看时间线`、分组 `管理` 三项；异常入口只在对应事实存在时显示。正常内容顺序、桌面/390px 重排、静止态可发现性、颜色语义、焦点顺序和局部失败遵守 Artifact v1。
- 本任务的 Artifact v1 视觉基线证据是 Playwright 语义、几何、overflow、Axe、键盘/focus 合同与短期人工评审材料；遵守现有 Web spec，不提交 tracked pixel golden、screenshot manifest 或 bulk raster。
- identity 读取失败产生整页 404/error；监控、IP、订阅或 activity 失败只降低对应区。若失败使当前判断不可信，anomaly 插槽显示一次来源不可用摘要，区内只显示当前授权范围的安全 freshness/retry，不复制长错误。projector lag 显示状态和刷新入口，不展示全局水位也不伪造空列表。
- `records_v2_read` capability/feature flag 在 staging 才打开新 overview 和 subject routes；旧 `/api/vps/:id/timeline`、`experience-logs` 与 legacy VPS 页面在最终切换前保持原行为且不双写。回退只关闭新 route/capability，不删除 records、evidence 或 activity projection。
- activity 同时提供领域自有 `DeletionAdapter` 与 `RecoveryAdapter`。在线永久删除 adapter 在 reservation/epoch fence 后清目标 record 的 activity presentation、subject relations、revision intervals与overview recent/summary cache，并以无内容 receipt 证明命中为0；它不清其他包的权威行。recovery adapter只重建本领域派生数据，二者都必须阻止旧 projector/checkpoint 复活已删 presentation。

## Acceptance Criteria

- [ ] `0057` fresh/0056-upgrade/repeat apply 均通过；projection 表、subject relation、source unique key、ingest sequence、checkpoint、查询索引和无 source cascade 约束与设计一致。
- [ ] 新记录/revision 1、后续 revision、状态/visibility、legacy、资产事实、监控事件、证据、评论和行动项逐类 event-time golden tests 通过；同一 source event/version 在retry和从空投影rebuild后的activity ID/业务全序一致、重复数为0；灾难重建递增generation，旧cursor稳定过期并给同query恢复入口，correction不改写旧行。
- [ ] 每个source的`AuthoritativeHead`/`Readiness(head)`与bounded scan conformance通过；activity child导出的`ActivitySnapshot{projection_generation,published_ingest_sequence,readiness_digest}`和`ActivityExportReader.ScanRecordPage`在head/checkpoint/status/generation/auth/selection漂移时失败关闭，caught-up时record-scoped重复分页无漏项/重复。task10只消费接口，不修改activity/store文件补合同。
- [ ] 固定水位的 50 条多页查询在并发 live insert、record current revision推进、晚到 backfill 和 projector retry 下成员/顺序保持不变且漏项/重复均为 0；低序号事务持有 head lock/延迟提交时高序号 batch不能越过发布，读取到的 published head 始终连续。刷新后才按新水位解析新current revision，晚到项按 `event_at` 进入正确位置且带 `backfilled`。
- [ ] 稀疏 `event_kind`、`view=evidence` 与 `versions=current` 的匹配项全部位于原始排序前 101 条之后时，服务端仍返回完整 page 或真实末页，连续分页无短页、跳项、重复；EXPLAIN证明全部 predicate 在 relation候选 LIMIT 前生效。
- [ ] activity/records/evidence 三个视图对同一 subject/query 的结果等于 canonical projection 的显式过滤；没有浏览器拼接、来源级预截断或两套人工记录数据。
- [ ] auth scope 变化、subject/source 删除、record reservation/permanent-delete、stale serving fence 和 client revoke 路径均 fail closed；activity领域 deletion receipt 前目标 presentation/relation/interval/cache命中为0且旧 projector不能复活；未授权 title/summary/count/identity/cursor 泄露命中为 0。
- [ ] 两个权限范围观察同一全局 projector：隐藏 scope 活动推进时，受限用户activity响应与VPS overview均不存在全局generation/head/checkpoint字段、confidential token不可解码/按水位比较，`new_items_available`保持false且overview freshness/recent/anomaly不变；只有其授权query出现新项才变化。
- [ ] VPS、monitoring instance、Target 的 activity/records/evidence 深链、URL 往返、返回来源、刷新和 tombstone 导航可用；VPS-local 新建记录准确预选 primary subject。
- [ ] healthy VPS API 返回非 `null` 的空 anomalies；desktop 与 390px DOM 对异常 heading/container/action/placeholder 的命中数均为 0。异常出现时 block 位于 identity 与常规摘要之间，恢复后 block/高度/焦点副作用均为 0。
- [ ] overview 四个常规摘要、最近真实变化、去重资产事实和关联入口不重复同一状态；首层显式动作恒为三项，管理/生命周期动作只在允许状态出现。
- [ ] identity fatal、monitoring/IP/subscription/relations/activity五类局部失败、stale、空 activity、query 无结果、projector lag、提交中和 revoke fixture 均符合 Artifact v1；Axe critical/serious 为 0，纯键盘可完成主要流程，焦点恢复正确，触摸目标 ≥44px，390px 无 document 横向溢出。
- [ ] 固定父设计数据集（10,000 current records、200,000 revisions、1,000,000 activities）下，overview API warm p95≤750ms、subject timeline 首 50 条 warm p95≤1s；SQL query 数有界，代表性 `EXPLAIN (ANALYZE, BUFFERS)` 命中 subject/watermark/order 索引。
- [ ] 新/旧 feature flag 对照下 legacy timeline/experience 计数和写行为不变；关闭 flag 可恢复 legacy VPS 页面且新 projection/records/evidence 保持可读数据不被删除。
- [ ] focused Go/PostgreSQL/Web/Playwright、`make verify-go`、Node 22 `make verify-web`、`npm --prefix web run test:e2e`、`git diff --check` 和 required CI 全部通过。

## Out of Scope

- 不实现记录 core、evidence capture/renderer、项目级记录中心、Markdown 编辑器、横向比较、评论/行动项业务写入或 legacy experience 转换；它们由对应子任务拥有，本任务只消费已合入合同。
- 不允许系统活动或监控恢复自动修改人工记录业务状态，不提供系统事实编辑/删除入口。
- 不以旧 `VPSTimeline` 五组完整数组、客户端全量排序或旧 `MonitoringComparePage` 作为新 activity fallback。
- 不在本任务删除 legacy API/table/UI，不执行破坏性 down migration，不把 feature flag 回退解释为删除新数据。

## Execution Gate

- 保持 `planning`；直接依赖、父顺序前置和用户独立执行授权全部满足后才可运行 `task.py start`。
