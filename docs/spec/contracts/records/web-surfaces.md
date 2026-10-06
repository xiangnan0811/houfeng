# Records Web 合同

通用数据层见 [Web 规范](../../web/README.md)。

### Scenario: AppShell 动态到达 records transport（global search 分组）

#### 1. Scope / Trigger

- Trigger: AppShell 常驻组件（当前只有 `GlobalSearch`）需要 server-backed records 数据，或改动 records 分组的配额/失败语义时。
- 目标：让 shell 能搜到记录，同时不让 records transport 回到 entry chunk。

#### 2. Signatures

- `web/src/pages/records/globalRecordSearch.ts`：`searchRecordsForGlobalSearch(query, limit): Promise<GlobalRecordSearchHit[]>`、`GlobalRecordSearchHit`、`RECORD_SEARCH_ALL_HIT_ID`。
- `GlobalSearch.tsx` 侧：`RECORD_RESULTS = 4`（records 配额）、`MAX_RESULTS = 10`（asset 配额）。

#### 3. Contracts

- shell **只能**用 `import('../../pages/records/globalRecordSearch')` 动态到达；不得在 shell 里静态 import `recordsApi.ts`、`searchFilterModel.ts` 或 records DTO 的 value import。type-only import 不构成 runtime edge，但没有必要时也不要加。
- 领域模块负责映射与降级，shell 不认识 records 类型。canonical `/records?...` 链接由该模块用 `recordSearchParamsFromFilters` 生成——URL 编解码只有一个 owner。
- **失败必须隔离**：records 分组任何异常（索引未就绪、平台关闭、401、网络失败）都 resolve 成空数组，不得阻断 asset 分组；asset 分组失败也不得清空已经返回的 records。`Promise.all` 的每个分支必须自带 catch。
- 401 的 session 结束副作用属于 transport（`requestJSON` 先通知 unauthorized handler 再 reject），领域模块吞掉 rejection **不等于**吞掉该副作用；不要为了"提前失败"在请求前自行判断登录态。
- records 有独立配额，不与 asset 上限共享：asset 是客户端未排序匹配，records 由服务端排序，共享上限会让高频关键词把记录全部挤掉。
- 服务端返回非空时才追加通向 `/records` 的末位入口；索引不可用时指向搜索页是误导。
- 迟到的旧查询不得覆盖新查询结果：`GlobalSearch` 用单调 generation ref 丢弃过期响应。

#### 4. Tests Required

- `globalRecordSearch.test.ts`：bounded limit、raw query 透传（服务端匹配，不做客户端小写化）、hit 映射与 id 回退、末位 canonical 链接、空结果不追加链接、三类失败均降级为空。
- `GlobalSearch.test.tsx`：records 分组渲染与链接、无 asset 命中时仍出结果、asset 失败仍保留 records 并展示错误、迟到响应被丢弃。
- `recordsTransportArchitectureContract.test.ts` 的 `EXPECTED_EXPORTS` 必须包含 `searchRecords`；fresh build 后 `bundle:check` 证明 entry 不含 records transport。

## 草稿与比较 transport

- `CreateRecordDraftInput` 是关闭联合：新记录草稿只发送 `payload`，已有记录草稿必须同时发送 `record_id` 与 `base_revision_id`；不得在 TypeScript contract 中强迫新草稿伪造空 ID，也不得允许两个 routing fields 只出现一个。
- 已有记录发布新修订时，证据不在草稿 payload 中：`createRecordRevision` 必须按基准修订 `evidence_snapshot_ids` 的原顺序逐项发送 `evidence_items: [{ existing_snapshot_id }]`（无证据时为空数组），否则后端会把新修订证据置空。新记录与比较另存不发送该字段；恢复历史修订由后端重建证据。
- 已有记录编辑以加载时（或上次发布后）的记录头为编辑基准，用于创建草稿与发布的 `base_revision_id`、锁版本、授权代次和证据；后台重新校验与冲突读取只更新展示，较早发出的记录读取晚到时丢弃。正式发布或已有记录草稿创建遇到修订冲突（`409 record_revision_conflict`，无论是否带 recovery，包括只有锁版本/授权代次推进的情况）时，工作区读取服务端当前头作为待确认头，解决器的服务端内容绑定这同一份快照；读取失败（非撤销）时提示稍后重试，不打开无法确认的解决器。只有用户在解决器中确认后它才成为确认头：下一次保存对同一草稿（ID 不变，附件归属随之保留）发送带 `If-Match` 的 `PATCH { payload, base_revision_id: <确认头> }`，无草稿时以确认头创建；发布按确认头的基准、锁版本、授权代次与证据提交。确认头不被后台刷新替换，头再变由服务端 409 重新打开解决器；关闭冲突或草稿 ETag 冲突不改编辑基准。保存进入冲突或失败后，发布不得再补存一次绕过解决器；任一冲突落地后（包括工作区已卸载、不再显示解决器时）暂停所有服务端保存（含定时器已触发、仍在保存链上排队的自动保存），本地缓冲照常写入；解决、关闭冲突或继续编辑后恢复，并重新排定一次自动保存；草稿 ETag 冲突替换解决器内容时清除待确认头，草稿合并不得顺带确认用户未看到的记录头。
- Records 草稿 PATCH 原样发送响应中的 `If-Match: <draft-etag>`，不得套用 legacy metadata helper 的额外引号；formal mutation 使用独立 `Idempotency-Key`。permanent-delete execute 的 `DeletionRequestTokenV1` 是唯一 `Idempotency-Key`，JSON body 只能含 `reservation_id`。
- `/records/compare` 使用 `comparison-url/v1` query `state`（canonical key order、UTC、整数秒）。state 不含 `token` / `comparison_intent` / `payload` / `title` / `body_markdown`。candidate 确认前 `POST /api/evidence/comparisons` 次数为 0。另存必须走 `createRecordDraft` + `saveComparisonRecord`，不得调用 `createRecord` / `useRecordDraft.publish()`。同一 digest 重试必须复用 `record_id` 与 `Idempotency-Key`。证据类型切换是 SegmentedControl 值选择，不是无 panel 的 Tabs。`HOUFENG_COMPARISON_ENABLED` 默认关。

| 条件 | 预期 |
| --- | --- |
| 新记录草稿携带一个或伪造两个空 routing fields | TypeScript union/source review 阻断；body 只含 `payload` |
| 修订冲突解决后再次发布 | 同一草稿 PATCH 携带确认头 `base_revision_id` 后按确认头发布，不创建/删除草稿；后台刷新到更新头或旧读取晚到都不改确认头与解决器内容；只推进锁版本/授权代次的无 recovery 409 也要求确认；改基准再遇 409 时重新打开解决器且不发布；关闭冲突（含首次创建冲突）不改编辑基准；新头读取失败时不打开解决器；草稿冲突接替修订冲突后确认不改基准；冲突期间（含卸载后晚到的冲突）不向服务端保存（含已排队的自动保存）但仍写本地缓冲，关闭或保留本地后恢复自动保存；`useRecordDraft.test.ts` 与 `record-workspace.spec.ts` 回归 |
| 编辑带证据的记录后发布修订 | body 含与基准修订同序的 `existing_snapshot_id` 全集；`useRecordDraft.test.ts` 与 `record-workspace.spec.ts` 回归 |
| Records draft PATCH 给 ETag 增加引号 | 后端 exact `If-Match` 拒绝；原样发送 draft response 的 `etag` |
| deletion token 同时进入 header 和 JSON body | body unknown-field decode 失败；只保留 header token 与 body `reservation_id` |
| Records error body 含 malformed `code/field_errors` 或未知 debug 字段 | 显式 decoder 保留 status/message，忽略 malformed/未知元数据；`recovery` 仍按 unknown 处理 |

## 记录工作区版式（`/records/:id`、`/new`、`/:id/edit`、`/:id/revisions/:rid`）

- 页头压缩身份：阅读 / 修订态显示标题、类型 / 业务状态 / 影响级别徽标，以及主体、负责人；阅读态另显示修订号与更新时间，修订态显示该修订的创建时间；不显示草稿同步状态，也不提供指向自身的"阅读"入口。编辑 / 新建态标题固定为"编辑运维记录 / 新建运维记录"，只在页头显示草稿同步状态（`role="status"`）。历史修订加"历史修订 #N"徽标，操作为"当前版本""横向比较"。
- 主栏 + 侧栏：≥1200px 为 `minmax(0,1fr) 320px` 双栏；更窄时单栏，顺序固定为正文 → 侧栏卡片（大纲 / 材料 / 关注或属性 / 协作 / 发布）→ 行动与评论。阅读态正文占满主栏，大纲少于 2 个标题时不显示；编辑态大纲有标题即显示。源文回退解析按 CommonMark 跳过围栏代码块（闭合围栏须同字符、不短于开启围栏且其后只有空白）。正文宽表包在具名（"正文表格"）、可聚焦的滚动区域里，任务列表两条渲染路径共用 `record-task-list` 版式，只读勾选框以"已完成 / 未完成"命名。
- 编辑态把标题与正文放在同一张编辑卡，`编辑 / 分栏 / 预览` 是 `role="toolbar"` 的 pressed 按钮组，分栏两侧等高；记录类型、业务状态、影响级别、可见性、主体进入侧栏"属性"，负责人 / 跟进 / 参与人进入"协作"，保存原因与保存影响进入"发布"，材料卡的"管理材料"打开材料抽屉。
- 导出 / 导入是页头按钮打开的 `导出记录 / 导入记录` 弹窗，面板按需懒加载，不再使用原生 `<details>`；面板不重复标题与说明文字。阅读 / 修订态材料直接列在侧栏（证据带"查看证据"），不打开只读抽屉。
- 历史修订的恢复原因（默认"恢复历史修订"）与"恢复为新修订"在侧栏"恢复此修订"卡；"与当前版本的差异"卡直接列字段差异，正文 diff 默认折叠为 `+N −M 行`，展开后是可聚焦的具名滚动区。
- 计数徽标为零时保持中性，大于零才用强调色（行动项按未完成数判断），不得拉伸成整行。按需展开的行动表单打开时焦点进入标题，取消后回到"新增行动"。

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

## 横向比较工作台版式

- 页头只保留"横向比较"与范围摘要（对象数、基准项、UTC 窗口的本地时间），不再使用眉题 + 大标题 + 解释句的分节写法。
- ≥1200px 左栏 340px 放"比较对象 / 比较条件"，右栏依次为可比性审查 → 比较结果 → 结论与另存；更窄时单栏同序。比较条件仍是默认展开、可折叠的 `<details>`，折叠摘要显示对齐 / 容差 / 桶宽。
- 比较对象卡在候选模式下，计数、列表与确认都只取前 6 个候选，超出时说明"只比较前 6 个"。
- 比较结果卡头部放证据类型与指标 `SegmentedControl`；趋势把所有比较项叠加在一张固定高度（约 200px）的图里，按项序使用固定配色加线型（颜色在某些主题相同也能区分）与图例；横轴是相对各自首个桶的时间偏移（任一序列时间不可解析时全部改用桶序），纵轴标签、网格与数据共用同一外扩 8% 的值域；单桶段画成标记点（奇数项圆形、偶数项方形），缺口断线不连线。
- 对齐矩阵只列项 / 类型 / 覆盖 / 桶数 / 质量 / 修订 / 说明，基准项带"基准"标记，使用 `ScrollRegion`；快照 ID、规范哈希与比较摘要只出现在默认折叠的"技术细节"中。另存阻断原因显示中文说明，不显示原始 reason code。

## Subject workspace

Activity, records, and evidence for VPS, monitoring instances, and entrypoints remain views of the shared subject workspace. Preserve the existing URL filter codec and location.state return context through record publication, restoration, and evidence links. Interactive filters match each view's server predicate; retained incompatible URL filters stay visible and removable. A scoped new record consumes the same canonical subject reference emitted by its entry link and isolates its unsynced buffer from other subjects and unscoped creation. Reopening the same entry restores its buffer without overwriting newer edits; unscoped draft recovery remains available from /records/new. A valid canonical return_to provides an explicit subject-return link, without changing the post-publication record destination. Record search prioritizes results; import/export remain secondary tools. Evidence reading follows the [evidence Web contract](../evidence-web.md).

主体工作区版式（`SubjectIdentityBar` + `UnifiedTimeline`，样式在全局 `page.css`）：

- 页头与 VPS 概览同一结构：类型图标 + 标题 + 在册 / 已删除主体徽标，一行身份信息（类型、ID，主机名与标题不同时附加）；不再使用眉题。「返回详情」与「新建记录 / 横向比较 / 刷新」同为页头按钮，返回链接仍保留 VPS 的 `return_vps` 与 navigation state，入口探测不带 state。
- 时间线按本地日历日分组，每天一张面板；每条一行：通道形状标记、本地时分、标题、通道标签、`回填` 标签、与事件时间不同的记入时间、查看 / 加入横向比较动作（右对齐）；摘要或证据覆盖信息另起一行。系统事实只显示通道标签，不再附加"不可编辑"说明。条目链接与页头一致：入口探测不携带 navigation state（`omitLinkState`）。时间与记入时间用 `<time datetime>`，记入跨日时写完整日期。640px 以下标题行移到时间下一行，动作不再右对齐。
- 部分来源不可用时页面给出一句状态提示，时间线顶部以徽标列出各来源状态。

Source presence 在册 is a neutral identity badge, not a green health state. VPS, monitoring, and 入口探测 activity/records/evidence share `SubjectActivityWorkspace` and `SubjectLocalNavigation`; only VPS exposes an overview hop because only that source has a detail workspace. Do not duplicate those surfaces or invent missing source fields.
