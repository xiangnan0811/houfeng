# Records Web 合同

通用数据层见 [Web 规范](../../web/README.md)。

## 运行能力与关闭状态

- 身份读取的 `runtime_capabilities` 是 Records、比较、导入导出入口的统一依据，不从请求 404 推断平台是否启用。加载/读取失败时不挂载业务子树；非认证错误提供重试，不呈现撤权。
- `records=false` 时隐藏记录侧栏、记录通知、全局搜索记录来源和各主体的活动/记录/证据/新建操作。直达上述路由显示“记录平台未启用”，门禁必须位于 lazy import 与取数 hooks 之前。命令审计、核心监控比较与 IP 质量历史仍可用。
- `comparison=false` 隐藏 Records 横向比较入口，直达显示关闭状态；`portability=false` 不挂载导入/导出面板及其请求。子能力均受 records 有效开关约束。
- VPS overview 的 `records_v2_read` 安全读取合同不变；平台关闭时最近活动 section 为 `state=unavailable, reason_code=records_disabled`，显示“未启用”，且后端不再把该关闭状态写入核心资产来源故障异常。前端按概览返回的 `anomalies` 原样展示，不根据 detail 文本丢弃或改写 `source.unavailable`。真正的活动投影失败仍按原合同报告。资产决策保存记录是独立 API，不受 Records 开关门禁。
- 平台开启后的资源 capabilities、授权与 opaque 403/404 保持原义，不能把资源 404 解释成单纯不存在。

### Scenario: AppShell 动态到达 records transport（global search 分组）

#### 1. Scope / Trigger

- Trigger: AppShell 常驻组件（当前只有 `GlobalSearch`）需要 server-backed records 数据，或改动 records 分组的配额/失败语义时。
- 目标：让 shell 能搜到记录，同时不让 records transport 回到 entry chunk。

#### 2. Signatures

- `web/src/pages/records/globalRecordSearch.ts`：`searchRecordsForGlobalSearch(query, limit)` 返回 `{matches, error}`，命中项为 `GlobalRecordSearchHit`；保留 `RECORD_SEARCH_ALL_HIT_ID` 的末位入口身份。
- 记录检索页无关键词与筛选条件（排序、每页条数不算）时的空结果写「还没有运维记录」并提示新建入口；有条件时才写「没有匹配的记录」。
- `GlobalSearch.tsx` 侧：`RECORD_RESULTS = 4`（records 配额）、`MAX_RESULTS = 10`（asset 配额）。订阅命中以订阅名或「所属 VPS 名 的订阅」为标题，金额写作「原币 金额/月」，不把 `sub_`/`vps_` 内部 ID 当标题；仍可按订阅名、VPS 名与 ID 检索。

#### 3. Contracts

- shell **只能**用 `import('../../pages/records/globalRecordSearch')` 动态到达；不得在 shell 里静态 import `recordsApi.ts`、`searchFilterModel.ts` 或 records DTO 的 value import。type-only import 不构成 runtime edge，但没有必要时也不要加。
- 领域模块负责映射与明确的来源结果，shell 不认识 records 类型。canonical `/records?...` 链接由该模块用 `recordSearchParamsFromFilters` 生成——URL 编解码只有一个 owner。
- **失败必须隔离且可见**：两个来源独立完成，成功结果不等待另一来源，也不被另一来源失败清空。记录异常（索引未就绪、网络失败或动态模块加载失败）显示“运维记录搜索暂不可用”，资产失败单独显示；不得把错误降级成成功空数组。`records=false` 时不 import、不请求记录来源，提示范围只包含资产。
- 401 的 session 结束副作用属于 transport（`requestJSON` 先通知 unauthorized handler 再 reject），来源错误结果不能吞掉该副作用。
- records 有独立配额，不与 asset 上限共享：asset 是客户端未排序匹配，records 由服务端排序，共享上限会让高频关键词把记录全部挤掉。
- 服务端返回非空时才追加通向 `/records` 的末位入口；索引不可用时指向搜索页是误导。
- 输入、提交与结果各有归属：输入变化、清空、提交、能力变化与卸载均递增 generation 并清掉旧结果、焦点、错误和加载状态。响应只有在 generation、提交词、结果词与当前输入同时匹配时才能呈现或激活。A 完成或在途时改输 B，Enter 必须搜索 B，不能激活 A。
- 非空未提交显示“按 Enter 搜索”；空提交不请求。只有提交已完成、来源均无失败且结果确实为空才显示“没有匹配项”。保留 combobox ARIA、方向键选择、Escape 与焦点恢复。

#### 4. Tests Required

- `globalRecordSearch.test.ts`：限额、服务端关键词匹配、canonical 末位链接、成功空结果与失败结果区分。
- `GlobalSearch.test.tsx`：独立来源完成/失败保留、未提交与真实空结果、输入变化/清空/能力变化使迟到结果失效、Enter 不激活旧查询及键盘操作。
- `recordsTransportArchitectureContract.test.ts` 检查 shell 的 eager-import 边界；transport 的请求行为由实际 API 测试覆盖，不以导出名或依赖列表快照代替。fresh build 后 `bundle:check` 检查 entry 不含 records transport。

## 浏览器草稿持久失效

- `draftBuffer.ts` 使用 IndexedDB version 2；正文仍在 `unsynced`，用户 generation、永久 record tombstone、已应用事件身份及每个 user/browser cursor 在 `cleanup`。生产不可退回内存存储；内存实现只供显式注入的测试使用。
- workspace 打开时捕获用户、真实 record ID（独立新草稿无 record ID）、用户与记录各自的 durable generation，先完成普通缓冲的全库 24 小时过期扫描，再恢复缓冲。未决发布 envelope 不因缓冲 TTL 自动删除，必须先查询原请求结果。所有保存、失败回写、切换与卸载分支传递原 scope，不能在迟到写入时重新申请有效 scope。单条恢复的过期判断与删除也在同一事务内，不可用一次旧读取删除后来写入的新缓冲。
- 每次写入与 generation/tombstone 检查同属一个事务。cleanup event 的版本为 1，cursor/epoch 是正整数十进制字符串，按 bigint 精确比较；禁止经 JS Number。事件的 operation/record/epoch/cursor 身份不可变，重复应用幂等，cursor 不倒退。
- 同一个 readwrite 事务写 tombstone、删除该用户该记录的全部缓冲并推进 cursor。只有 `transaction.oncomplete` 才成功；abort/error 不得当成已清理、不得据此 ack。tombstone 跨 reload 和 logout 保留，旧 tab 的 scope 不能复活正文。新建草稿的 subject-prefill key 不是 record 身份，不因其他记录 cleanup 删除。
- 显式 logout 原子推进该用户 generation 并删除其缓冲，其他用户保持。失败显示可重试的本地清理故障，并在成功前阻止新 login；不会伪称清理成功。401 只结束会话身份，不删除本地草稿、不推进 durable generation。迟到会话刷新不能恢复已退出身份。
- 启动 reconcile 对已确认无权或不存在的普通记录返回 invalidate：同步关闭本地工作区、先清比较重试元数据，再原子推进该记录 generation 并删缓冲；不捏造 purge event 或永久 tombstone。待创建目标通过 allocation 归属核对，不能因 root 尚不存在就清除未决发布。allocation、连续性或 read fence 无法确定时返回 unavailable，不据此 invalidate。后来重新授权可取得新 scope，但旧 scope 不能回写；真实 cleanup tombstone 不因 reconcile 或重新登录解除。
- cleanup consumer 串行应用事件，先同步关闭同 tab 工作区并通过 BroadcastChannel 通知其他 tab，sessionStorage 清除成功后才提交 IndexedDB cursor；任何一步失败均不 ack。普通断线只停止同步并显示可重试故障，不删除草稿。401 暂停访问并保留未决发布及比较重试身份，同用户重新登录后按原请求恢复；显式 logout、切换用户或来源失效按各自清理规则处理。
- 普通新建、证据采集与比较另存取得 allocation 后，立即将目标 record ID 绑定到当前工作区的清理监听；恢复持久请求时同样绑定。cleanup 先于异步绑定到达时，后续绑定也必须拒绝恢复旧内容。目标清理关闭已打开标签页中的标题、正文、材料与重试入口，并使迟到结果失效；不能仅删除持久缓冲而保留内存中的未决请求。
- `GET /api/records/client-cleanup?browser_id=…&cursor=…` 使用同源 WebSocket；server session 决定 user，周期重验 session、每次发送及 ack 前再次重验，写入有界。`POST /api/records/client-cleanup/reconcile` 接收 `{browser_id,record_ids,allocation_references?}`，其中 allocation reference 为 `{record_id,allocation_id}`，两类合计每批最多 100 项，拒绝重复或矛盾归属。成功响应返回互斥且完整的 `allowed` / `pending` / `invalidate`；pending 只登记 cleanup 元数据，不授予读取记录权限。缺少 allocation 的未决旧版目标不作为普通 record ID 登记，待原请求完成重放后再处理。
- `browser_id` 是当前 document 内按用户隔离的身份，不从 sessionStorage 继承。新标签页、opener 复制页和 reload 各自生成新身份，启动时先 reconcile 全部本地已知目标，再连接无旧 cursor 的事件流；同 document 内停止/重启复用当前身份与 durable cursor，未知 cursor 协调生成的身份也成为当前身份。比较重试 envelope 仍在各 tab 的 sessionStorage 中，因此不能共用 opener 的 receipt 来代替复制页自己的清理与 ack。
- WebSocket server frame 为 `{type:"cleanup",event:{version:1,cursor,operation_id,record_id,cleanup_epoch,scopes}}`；client frame 为 `{type:"ack",ack:{cursor,operation_id,record_id,cleanup_epoch}}`。同一连接一次只发送一个未确认事件，四字段必须完整匹配当前事件及持久 target。服务端先重放最早未确认事件，不因客户端声称更大 cursor 而跳过丢失 ack 的事件。未知或不属于该 user/browser 的 cursor 返回 `{type:"reconcile_required"}`；客户端重新协调全部已知本地目标并建立无旧 cursor 的 browser 身份，不静默跳过。
- 存储协议的实际 IndexedDB 验证不替代服务端事件、target/receipt、在线 consumer 与 comparison 工作台的端到端验收。allocation lookup 本身不证明 APP 回滚后的连续性，unknown recovery 仍须由受信恢复与离线准入关闭；pending 登记 → 真实出生 → V2 签发 → cleanup event 仍需真实整合验收。永久删除 HTTP 保持关闭。

## 草稿与比较 transport

- `CreateRecordDraftInput` 是关闭联合：新记录草稿只发送 `payload`，已有记录草稿必须同时发送 `record_id` 与 `base_revision_id`；不得在 TypeScript contract 中强迫新草稿伪造空 ID，也不得允许两个 routing fields 只出现一个。
- 新记录身份由 `POST /api/records/allocations` 分配，浏览器不得随机生成或选择认证域。普通发布和带 evidence 的新建在首次 create HTTP 前，将 allocation、publication key、draft ID/ETag、完整请求及 digest 写入本用户的持久缓冲。丢响应或 reload 先恢复原请求，再考虑自动保存、已消费 draft 或 intent TTL；未知结果不得换 key、换 allocation 或覆盖请求。显式放弃只清理本地重试状态，不宣称服务端请求未提交。已确认发布后再编辑必须使用新的发布身份。
- 新建工作区先读取本用户的持久 evidence reservation，再允许采集或发布；读取失败保持不可用并提示刷新，不把未知状态当成空预留。reload 恢复未决提示与放弃入口，已绑定结果继续复用；已清理或丢失的绑定不能静默换一次 allocation。预留确认、发布准备及放弃操作串行，进行中阻止重复提交。
- 采集弹窗中的预留失败只显示一个可操作提示；成功放弃清除该提示和旧采集错误，焦点回到弹窗内的采集或来源控件。关闭弹窗后不再显示已经处理的错误。未决发布重试保留键盘操作位置，放弃后焦点回到发布控件。
- 已有记录发布新修订时，证据不在草稿 payload 中：`createRecordRevision` 必须按基准修订 `evidence_snapshot_ids` 的原顺序逐项发送 `evidence_items: [{ existing_snapshot_id }]`（无证据时为空数组），否则后端会把新修订证据置空；本次新采集的证据按加入顺序以 `{ capture_intent_id }` 追加在其后。新记录没有采集证据时不发送该字段，有时发送 `record_id`（首次预览时服务端预分配）与非空 `evidence_items: [{ capture_intent_id }]`；比较另存不发送该字段；恢复历史修订由后端重建证据。
- 已有记录编辑以加载时（或上次发布后）的记录头为编辑基准，用于创建草稿与发布的 `base_revision_id`、锁版本、授权代次和证据；后台重新校验与冲突读取只更新展示，较早发出的记录读取晚到时丢弃。正式发布或已有记录草稿创建遇到修订冲突（`409 record_revision_conflict`，无论是否带 recovery，包括只有锁版本/授权代次推进的情况）时，工作区读取服务端当前头作为待确认头，解决器的服务端内容绑定这同一份快照；读取失败（非撤销）时提示稍后重试，不打开无法确认的解决器。只有用户在解决器中确认后它才成为确认头：下一次保存对同一草稿（ID 不变，附件归属随之保留）发送带 `If-Match` 的 `PATCH { payload, base_revision_id: <确认头> }`，无草稿时以确认头创建；发布按确认头的基准、锁版本、授权代次与证据提交。确认头不被后台刷新替换，头再变由服务端 409 重新打开解决器；关闭冲突或草稿 ETag 冲突不改编辑基准。保存进入冲突或失败后，发布不得再补存一次绕过解决器；任一冲突落地后（包括工作区已卸载、不再显示解决器时）暂停所有服务端保存（含定时器已触发、仍在保存链上排队的自动保存），本地缓冲照常写入；解决、关闭冲突或继续编辑后恢复，并重新排定一次自动保存；草稿 ETag 冲突替换解决器内容时清除待确认头，草稿合并不得顺带确认用户未看到的记录头。
- Records 草稿 PATCH 原样发送响应中的 `If-Match: <draft-etag>`，不得套用 legacy metadata helper 的额外引号；formal mutation 使用独立 `Idempotency-Key`。permanent-delete execute 的 `DeletionRequestTokenV1` 是唯一 `Idempotency-Key`，JSON body 只能含 `reservation_id`。
- `/records/compare` 使用 `comparison-url/v1` query `state`（canonical key order、UTC、整数秒）。state 不含 `token` / `comparison_intent` / `payload` / `title` / `body_markdown`。candidate 确认前 `POST /api/evidence/comparisons` 次数为 0。另存必须走 `createRecordDraft` + `saveComparisonRecord`，不得调用 `createRecord` / `useRecordDraft.publish()`。同一 digest 重试必须复用 `record_id` 与 `Idempotency-Key`。证据类型切换是 SegmentedControl 值选择，不是无 panel 的 Tabs。`HOUFENG_COMPARISON_ENABLED` 默认关。
- 能力门禁先于 URL 解析与取数。缺少 state 是正常空篮；invalid 是损坏链接，unknown_version 是不支持版本，不能混成同一故障。显式传入 items 的比较入口始终 fixed，即使同时有多个 subjects，也不能丢弃历史 record_id/revision_id 改为候选模式。
- fixed 篮唯一持久状态是 URL；空篮使用无 state URL，parser 不接受空 fixed 数组。支持添加、移除、替换和清空，上限 6 项，超限明确拒绝而非截断。身份键为 `snapshot:<id>` 或 `revision:<record_id>:<revision_id>`，不按主体去重；同一主体可有两份证据或两份修订，重复修订的新证据选择替换该项 snapshot_ids。
- 删除基准项后选原位置下一项，没有下一项则选最后项，空篮 baseline=0。篮变化使旧评估/保存结果失效并取消或丢弃在途结果；0/1 项不 evaluate，2–6 项才使用 fixed 合同。失败和重试不丢篮。普通传输失败仍继续使用服务端签名意图，并按原有幂等协议重试。另存返回 403、404，或 422 `comparison_intent_invalid` / `comparison_intent_stale` 时，立即清除比较结果、候选和已签名意图，并禁用另存，直到下一次授权评估成功；选择篮保留。另存草稿的主体只来自当前成功的 fixed 评估：按比较项顺序收集 `vps` / `monitoring_instance` / `target`，同一 `kind+id` 只保留第一次，第一项为 primary affected。清空、添加、移除、替换、历史前进/后退或外部写入 URL 之后不得沿用上一次评估的主体；没有当前成功评估时不保留旧主体。工作台不提供主体覆盖编辑。候选模式确认前不另存，其主体只反映当前 URL 的 subjects。仅证据类型或指标的展示切换不打断已开始的另存；篮身份变化仍递增保存代次并取消在途另存。同一 digest 仍复用 `record_id` 与 `Idempotency-Key`。
- 比较重试 envelope 使用 `houfeng.comparison-save/<encodedUserId>/<digest>`，v2 保存 user、source record IDs、allocation 请求与结果、目标 record ID、幂等 key、draft 身份、完整另存请求及 digest、用户 generation。来源采用已授权响应的 `source_record_id`（以及实际 revision 所属 record），不能猜 `subject.id`。首次发送前持久化，未知结果和 reload 复用原身份；已完成结果的自动恢复只适用于初始视图，离开篮后不得让旧结果重新出现。v1 已提交请求保留原 key 和目标用于完成结果重放，未提交旧域 ID 不创建新 root。401 不等于显式放弃；logout、换用户、source invalidate 或用户明确放弃才按对应规则清理元数据。旧 digest-only 无归属项仅删除元数据，不迁给当前用户，不触碰正文或已保存 B。
- 比较读取、草稿创建和最终另存之间检查原捕获的来源 scope；切换用户立即清除旧标题、结论与保存状态，迟到结果不得复活。删除 A 的 cleanup 关闭依赖 A 的工作台并删除其 retry envelope，不删除独立 B 内容。存储失败必须停止发布或确认，不能退成仅内存幂等身份。
- 比较另存的目标 B 被清理时，即使来源仍可读，也必须清除绑定 B 的重试 envelope 并终止该次另存。来源 A 的清理与目标 B 的清理各按实际身份处理，不把清理来源扩展为删除独立 B 的正文。

| 条件 | 预期 |
| --- | --- |
| 新记录草稿携带一个或伪造两个空 routing fields | TypeScript union/source review 阻断；body 只含 `payload` |
| 修订冲突解决后再次发布 | 同一草稿 PATCH 携带确认头 `base_revision_id` 后按确认头发布，不创建/删除草稿；后台刷新到更新头或旧读取晚到都不改确认头与解决器内容；只推进锁版本/授权代次的无 recovery 409 也要求确认；改基准再遇 409 时重新打开解决器且不发布；关闭冲突（含首次创建冲突）不改编辑基准；新头读取失败时不打开解决器；草稿冲突接替修订冲突后确认不改基准；冲突期间（含卸载后晚到的冲突）不向服务端保存（含已排队的自动保存）但仍写本地缓冲，关闭或保留本地后恢复自动保存；`useRecordDraft.test.ts` 与 `record-workspace.spec.ts` 回归 |
| 编辑带证据的记录后发布修订 | body 含与基准修订同序的 `existing_snapshot_id` 全集；`useRecordDraft.test.ts` 与 `record-workspace.spec.ts` 回归 |
| Records draft PATCH 给 ETag 增加引号 | 后端 exact `If-Match` 拒绝；原样发送 draft response 的 `etag` |
| deletion token 同时进入 header 和 JSON body | body unknown-field decode 失败；只保留 header token 与 body `reservation_id` |
| Records error body 含 malformed `code/field_errors` 或未知 debug 字段 | 显式 decoder 保留 status/message，忽略 malformed/未知元数据；`recovery` 仍按 unknown 处理 |
| 评估篮 A 后清空再评估篮 B，或增删主体、历史前进/后退、外部替换 URL 后另存 | `createRecordDraft` 的 subjects 只含当前成功评估的主体，不保留上一篮；无 `setSaveSubjects`。展示切换不取消在途另存，篮身份变化仍取消。`useComparisonWorkbench.test.tsx` 回归 |
| 另存返回 403、404 或 422 `comparison_intent_invalid` / `comparison_intent_stale` | 比较结果、候选和签名意图从界面消失，另存禁用直到下一次授权评估；选择篮保留。普通传输失败仍保留意图并复用 `Idempotency-Key`。`useComparisonWorkbench.test.tsx` 与 `RecordComparisonPage.test.tsx` |

## 记录工作区版式（`/records/:id`、`/new`、`/:id/edit`、`/:id/revisions/:rid`）

- 页头压缩身份：阅读 / 修订态显示标题、类型 / 业务状态 / 影响级别徽标，以及主体、负责人；阅读态另显示修订号与更新时间，修订态显示该修订的创建时间；不显示草稿同步状态，也不提供指向自身的"阅读"入口。编辑 / 新建态标题固定为"编辑运维记录 / 新建运维记录"，只在页头显示草稿同步状态（`role="status"`）。历史修订加"历史修订 #N"徽标，操作为"当前版本""横向比较"。
- 未决发布显示被冻结请求的标题、属性与正文，并保持只读；重试沿用原请求，不能在确认结果前编辑请求。待确认的 evidence 引用显示确认中，不误报为失效或允许跳转到尚未确认的记录。
- 主栏 + 侧栏：≥1200px 为 `minmax(0,1fr) 320px` 双栏；更窄时单栏，顺序固定为正文 → 侧栏卡片（大纲 / 材料 / 关注或属性 / 协作 / 发布）→ 行动与评论。阅读态正文占满主栏，大纲少于 2 个标题时不显示；编辑态大纲有标题即显示。源文回退解析按 CommonMark 跳过围栏代码块（闭合围栏须同字符、不短于开启围栏且其后只有空白）。正文宽表包在具名（"正文表格"）、可聚焦的滚动区域里，任务列表两条渲染路径共用 `record-task-list` 版式，只读勾选框以"已完成 / 未完成"命名。
- 编辑态把标题与正文放在同一张编辑卡，`编辑 / 分栏 / 预览` 是 `role="toolbar"` 的 pressed 按钮组，分栏两侧等高；记录类型、业务状态、影响级别（下拉：低/中/高/严重，存储值仍为 low/medium/high/critical，既有自定义值保留为选项，页头徽标显示中文）、可见性、主体进入侧栏"属性"，负责人 / 跟进 / 参与人进入"协作"，保存原因与保存影响进入"发布"，材料卡的"管理材料"打开材料抽屉。可见性不再只改种类：切回项目内会清空角色和权限组，详见下文。
- 导出 / 导入是页头按钮打开的 `导出记录 / 导入记录` 弹窗，面板按需懒加载，不再使用原生 `<details>`；面板不重复标题与说明文字。阅读 / 修订态材料直接列在侧栏（证据带"查看证据"），不打开只读抽屉。
- 历史修订的恢复原因（默认"恢复历史修订"）与"恢复为新修订"在侧栏"恢复此修订"卡；"与当前版本的差异"卡直接列字段差异，正文 diff 默认折叠为 `+N −M 行`，展开后是可聚焦的具名滚动区。
- 计数徽标为零时保持中性，大于零才用强调色（行动项按未完成数判断），不得拉伸成整行。按需展开的行动表单打开时焦点进入标题，取消后回到"新增行动"。

## 记录可见性（编辑侧栏）

- 侧栏“属性”的可见性由 `RecordVisibilityFields` 编辑。切到“项目内”提交 `{kind:'project', allowed_roles:[], allowed_group_ids:[]}`，不能留下角色或权限组。新的“受限”记录不默认勾选角色。
- “受限”同时列出角色“项目管理员 / 只读成员”和 `GET /api/record-access-groups/mine` 的具名权限组。角色与权限组是或关系。`project_admin` 表示全部当前管理员，不能排除某一个；当前没有只读成员登录账号时，勾选“只读成员”仍写入 `viewer`。
- 目录只属于这个组件，不改变 `useAuth` 的身份来源，也不提供手填组 ID。目录读取中或失败时保留已选组，空目录和失败都不能改写草稿，失败也不把已选组标成“不可用”。成功目录里没有的已选 ID 保留为可移除的“不可用”行。
- 未选择任何角色或组，或当前账号对不上所选角色且不在已确认的组里，编辑区说明发布后将无法查看。这只是编辑前提示，服务端发布仍是最终判定。

| 条件 | 预期 |
| --- | --- |
| 受限且已有角色或组时改回项目内 | 草稿可见性变成空授权的 project；再进入受限时不默认勾选角色 |
| 只勾选目录中的一个组 | 草稿只增加该组，不带上目录里的其他组 |
| 目录失败后重试得到空目录 | 已选组 ID 保留；失败时显示“目录暂不可用，已保留”，成功后才显示可移除的“不可用”；不请求 `/api/admin` |
| 当前管理员勾选“只读成员”或“项目管理员” | 只读成员显示无法查看；项目管理员清除该提示。`RecordVisibilityFields.test.tsx` |

## 记录附件（材料对话框、阅读侧栏）

- 材料清单中的附件按 `GET /api/attachments/{id}` 元数据显示文件名与"类型 · 大小"，不显示原始 ID（元数据未到时显示"附件 · 读取中"）。记录与修订只给出 `attachment_ids`、没有批量接口，元数据逐个读取并按 ID 缓存；读不到（无权、已删除、服务异常）统一显示"引用已失效"，不区分原因。
- 编辑 / 新建态的材料对话框顶部是上传区（选择文件或拖入），提示"图片、PDF、文本与日志、压缩包，单个不超过 50 MiB"。上传前按后端准入（`internal/center/attachments/admission.go`）的扩展名—类型配对在本地校验，并按扩展名声明后端接受的媒体类型（浏览器的 `file.type` 不可靠）；不支持、为空或超限的文件只在队列里显示原因，不向后端预留配额。
- 上传必须挂在草稿下：没有草稿时先保存一次；冲突未解决（即使已有服务端草稿）或保存失败时不上传并提示"草稿暂不可保存，附件未上传"；发布进行中不接受新文件也不重试（提示"正在发布，完成后再上传附件"，对话框的选择与重试按钮禁用）。等草稿期间离开工作区则不再开始上传。草稿被发布或丢弃后，其名下剩下的失败 / 取消 / 过期上传随队列一并清除，不能再对已消费的草稿重试。流程为 create → PUT content（`X-Houfeng-Draft-ID`、`X-Content-SHA256`）→ complete → 轮询元数据；队列显示等待上传 / 正在校验 / 正在上传 / 安全检查中 / 未通过 / 已过期 / 上传失败 / 已取消，进行中可取消、失败或过期可重试、结束可移除。
- 附件通过安全检查（available）后加入草稿 `attachment_ids`：这一步不改变冲突状态，解决器打开时同时加入其本地一侧；随后该项从队列移除、以材料行出现。从接受文件（含等待草稿保存）到上传与安全检查结束，页头同步状态显示"附件上传中，完成后可发布"，"发布修订"禁用，发布入口还会同步复查上传状态。
- 移除附件只是让新修订不再引用：草稿名下未被引用的上传在发布或丢弃草稿时由后端释放（见 [drafts.md](drafts.md)），已在记录上的附件在历史修订中仍可查看和下载。发布遇到 `409 draft_attachments_busy` 时提示"附件仍在安全检查，请稍后再发布"。
- 阅读 / 修订态侧栏的附件行提供"预览"（仅 available 且后端生成了安全预览）与"下载"。下载经授权读取原文件后以对象 URL 触发保存，保存名按后端 `Content-Disposition` 同一规则清理（最多 180 字符，控制字符与 `/ \ " ; :` 换成 `_`，去掉首尾空格和点）。图片与 PDF 的预览是后端重新渲染的 PNG（PDF 只预览第一页）；站点 CSP `img-src 'self'` 不含 `blob:`，因此直接以同源 `/api/attachments/{id}/content?variant=preview` 显示。文本预览读取为纯文本放进 `<pre>`，超过 256K 字符只显示开头并提示下载原文件。读取失败显示"预览不可用或授权已撤销"。压缩包没有预览。

| 条件 | 预期 |
| --- | --- |
| 上传不支持的类型（如 `.pcap`）、空文件或超过 50 MiB | 只在队列显示原因，不创建草稿、不调用 `POST /api/attachment-uploads`；`useRecordAttachmentUploads.test.ts`、`record-attachments.spec.ts` 回归 |
| 上传进入安全检查 | 队列显示"安全检查中"，页头禁用发布；检查通过后加入草稿 `attachment_ids` 并自动保存；`record-attachments.spec.ts` 回归 |
| 阅读页预览与下载 | 文本读取为纯文本、图片以同源预览地址显示，下载文件名为附件显示名；1440 / 1024 / 390 无横向溢出；`record-attachments.spec.ts` 回归 |

## 记录证据采集（材料对话框）

- 编辑 / 新建态的材料对话框顶部用 `SegmentedControl`（"添加材料"）在"上传附件 / 采集证据"之间切换；采集区是 `EvidenceCapturePicker`，只在切到采集时挂载。采集合同（预览、配额、确认）见 [证据 Web 合同](../evidence-web.md)。
- 来源优先取正在编辑的记录主体：主体本身（VPS、监控实例、入口探测目标）在前，主体 VPS 名下的当前监控实例随后（标为"VPS · 实例"，已是主体的实例不重复）；读取中来源显示"正在读取…"且不能预览。来源下拉末尾的"其他 VPS…"可任选一台 VPS，监控类证据再选它的监控实例，用于跨主机对比；没有匹配来源时直接进入该模式。VPS 与实例列表在需要时才读取，读取失败提示"列表读取失败，请稍后重试"。入口探测目标不挂在 VPS 下，只能取记录主体。
- 加入记录后的证据只保留在当前页面（不进草稿、不进本地缓冲），以"类型 · 来源"和"待保存"徽标出现在材料清单，可插入引用、可移除；已保存的证据暂不支持移除，也只有已保存的证据有"查看证据"。存在待保存证据时离开页面由浏览器 `beforeunload` 提示。同一采集意图只加入一次。
- 发布时把待保存证据随修订（或新记录）一起提交，见上文证据顺序；发布进行中不重入，也不能加入或移除待保存证据（新记录发布成功会跳走，期间加入的证据会丢失）。任一项已过有效期（`valid_until`）时不发请求（保存草稿后、正式提交前再查一次），提示"有证据预览已过期，请移除后重新采集"；服务端重新采集与预览不一致返回 `409 evidence_preview_stale` 时提示"有证据预览已失效（过期或来源数据已变化），请移除后重新采集"，待保存证据全部保留。记录或修订写入后（即使随后读取失败）只放下这次发布的证据；新修订已带回的快照不再显示为待保存。
- 材料清单不显示快照 ID：已保存证据逐个读取 `GET /api/evidence/{id}` 并按 ID 缓存，读到前显示"证据"，读到后显示"证据类型 · 标题"。

| 条件 | 预期 |
| --- | --- |
| 主体只有 VPS，采集主机监控 | 来源默认是该 VPS 名下的监控实例，预览带 `record_id`（已有记录）；`EvidenceCapturePicker.test.tsx`、`RecordWorkspace.test.tsx` 回归 |
| 加入两份后移除一份再发布修订 | `evidence_items` 为既有快照加剩下的一个 `capture_intent_id`，发布后"待保存"消失；`RecordWorkspace.test.tsx`、`record-evidence-capture.spec.ts` 回归 |
| 新建记录采集后发布 | `createRecord` body 带首次预览的 `record_id` 与 `evidence_items`；`useRecordDraft.test.ts`、`record-evidence-capture.spec.ts` 回归 |
| 待保存证据已过期（含保存草稿期间过期）或服务端返回 `evidence_preview_stale` | 不发请求或提示重新采集，证据保留；`useRecordDraft.test.ts` 回归 |
| 发布进行中再次发布、加入或移除待保存证据 | 不重复提交；"加入记录"与待保存证据的"移除"禁用；`useRecordDraft.test.ts`、`RecordWorkspace.test.tsx` 回归 |

## 横向比较工作台版式

- 页头只保留"横向比较"与范围摘要（对象数、基准项、UTC 窗口的本地时间），不再使用眉题 + 大标题 + 解释句的分节写法。
- ≥1200px 左栏 340px 放"比较对象 / 比较条件"，右栏依次为可比性审查 → 比较结果 → 结论与另存；更窄时单栏同序。比较条件仍是默认展开、可折叠的 `<details>`，折叠摘要显示对齐 / 容差 / 桶宽。
- 候选按 snapshot_id 去重，由用户勾选最多 6 项；确认前不 evaluate，确认只提交选中的 snapshot_id，不自动选择前六项或 revision_ids[0]。固定篮提供“添加对象”、单项移除、修订证据选择和清空，超限提示不隐藏未选候选。
- “添加对象”对话框从授权资产列表选择 VPS/监控实例/目标，按 `view=evidence, source=evidence_snapshot, versions=history` 读取主体证据活动；可调整时间窗口并使用 opaque cursor 加载更多，切换查询重置分页。活动投影 503 明示暂不可用和重试，权限错误不显示为无证据。`freshness.state` 为 ready 不代表证据来源完整；`evidence_snapshot` 为 stale 或 unavailable 时保留已返回的行，用既有来源状态文案提示列表可能不完整并提供重试，不把空列表说成当前窗口没有证据，也不把 reason code 当作主文案。来源完整的空结果仍链接所选主体证据工作区，保留当前篮。
- 证据行显示标题、时间、种类和质量，技术 ID 折叠。证据读取失败标为不可读；历史修订从 `getRecordRevision` 提供的 evidence_snapshot_ids 中选择，不伪造证据可读性或评估成功。
- 比较结果卡头部放证据类型与指标 `SegmentedControl`；趋势把所有比较项叠加在一张固定高度（约 200px）的图里，按项序使用固定配色加线型（颜色在某些主题相同也能区分）与图例；横轴是相对各自首个桶的时间偏移（任一序列时间不可解析时全部改用桶序），纵轴标签、网格与数据共用同一外扩 8% 的值域；单桶段画成标记点（奇数项圆形、偶数项方形），缺口断线不连线。
- 对齐矩阵只列项 / 类型 / 覆盖 / 桶数 / 质量 / 修订 / 说明，基准项带"基准"标记，使用 `ScrollRegion`；快照 ID、规范哈希与比较摘要只出现在默认折叠的"技术细节"中。另存阻断原因显示中文说明，不显示原始 reason code。

## Subject workspace

Activity, records, and evidence for VPS, monitoring instances, and entrypoints remain views of the shared subject workspace. Preserve the existing URL filter codec and location.state return context through record publication, restoration, and evidence links. Interactive filters match each view's server predicate; retained incompatible URL filters stay visible and removable. A scoped new record consumes the same canonical subject reference emitted by its entry link and isolates its unsynced buffer from other subjects and unscoped creation. Reopening the same entry restores its buffer without overwriting newer edits; unscoped draft recovery remains available from /records/new. A valid canonical return_to provides an explicit subject-return link, without changing the post-publication record destination. Record search prioritizes results; import/export remain secondary tools. Evidence reading follows the [evidence Web contract](../evidence-web.md).

主体工作区版式（`SubjectIdentityBar` + `UnifiedTimeline`，样式在全局 `page.css`）：

- 页头与 VPS 概览同一结构：类型图标 + 标题 + 在册 / 已删除主体徽标，一行身份信息（类型、ID，主机名与标题不同时附加）；不再使用眉题。「返回详情」与「新建记录 / 横向比较 / 刷新」同为页头按钮，返回链接仍保留 VPS 的 `return_vps` 与 navigation state，入口探测不带 state。
- 时间线按本地日历日分组，每天一张面板；每条一行：通道形状标记、本地时分、标题、通道标签、`回填` 标签、与事件时间不同的记入时间、查看 / 加入横向比较动作（右对齐）；摘要或证据覆盖信息另起一行。系统事实只显示通道标签，不再附加"不可编辑"说明。条目链接与页头一致：入口探测不携带 navigation state（`omitLinkState`）。时间与记入时间用 `<time datetime>`，记入跨日时写完整日期。640px 以下标题行移到时间下一行，动作不再右对齐。
- 部分来源不可用时页面给出一句状态提示，时间线顶部以徽标列出各来源状态。

Source presence 在册 is a neutral identity badge, not a green health state. VPS, monitoring, and 入口探测 activity/records/evidence share `SubjectActivityWorkspace` and `SubjectLocalNavigation`; only VPS exposes an overview hop because only that source has a detail workspace. Do not duplicate those surfaces or invent missing source fields.
