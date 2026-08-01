# 负责人、行动项、评论、关注与通知

## Goal

在不把记录中心扩展成通用项目管理或聊天产品的前提下，交付可审计的负责人/参与者/跟进、结构化行动项、评论、关注、个人站内通知和权限安全外投，使协作变化可追溯、可筛选、可导出，并在撤权或永久删除后不继续泄露。

## Requirements

- 父设计：`../07-13-vps-detail-experience-design/design.md` §7、§9.3、§10、§14–§15、§19.3、§21、§23–§25。
- 直接依赖：`07-14-vps-records-platform-foundation` 与 `07-14-vps-records-core` 必须已合入受保护主线且 post-merge CI 通过。
- 执行顺序固定在`07-14-vps-records-evidence-platform`之后、`07-14-vps-records-markdown-workspace`之前；本任务拥有保留迁移`0056_create_record_collaboration.sql`。已合入的0051–0054永久冻结；占号时只能整体顺延仍未发布的0055–0060。`0055`继续保留给后续search child，因此0056必须能在主线尚无0055时单独应用；本任务用真实PostgreSQL+custom migration filesystem证明ledger已记录较大文件名后仍会发现缺失的较小文件名，search child再用实际0055验证两种实际schema路径。不能依赖0055对象或假定migration只按编号单调前进。
- 本任务交付完整产品范围，不以 MVP 理由省略负责人、参与者、下次跟进、行动项、评论编辑历史/删除 tombstone、关注、提及、站内通知、外部安全摘要、重试、授权、审计、导出和永久删除适配。
- 主要负责人、参与者、下次跟进和可见范围仍是完整record revision的权威字段；变化必须通过正式保存产生新修订、领域活动和通知，不另建可绕过历史的可变记录元数据。每个非空owner/participant必须在同一事务按稳定user ID验证current project membership，以及post-save record visibility与全部source authorization floor交集下的read policy；跨项目、受限组外、来源floor不允许或本次visibility收窄后不可读的assignment必须使整条revision rollback。
- 一般笔记/重要发现默认不渲染负责人、参与者和跟进字段；用户显式开启跟进后才出现。逾期、受阻、无负责人和临期提示只在事实存在时动态出现，稳定态不得渲染异常标题、空异常卡、禁用异常动作或预留高度。
- 行动项是记录下的独立对象，至少保存内容、`todo|in_progress|blocked|done|cancelled`、负责人、可选截止/完成时间和关联主体；所有变化形成不可变 action event。非空assignee只接受稳定user ID，并在每次create/update时验证其仍是当前项目成员且通过该记录的read policy，不能按显示名绑定或把不可读记录分配给目标。快速更新不制造 Markdown 修订，不自动改变记录业务状态，也不调用 VPS/订阅/监控/Target 等业务写接口。
- Markdown task checkbox 不自动转为行动项；显式提升必须展示将创建的内容/负责人/截止时间并由用户确认。普通流程不硬删除行动项，以 `cancelled` 保留历史。
- 评论使用版本化安全Markdown子集；新增、回复、编辑和普通删除均不改写正式记录修订。POST/PATCH/DELETE都要求`Idempotency-Key`，PATCH/DELETE另要求comment version `If-Match`；同key+fingerprint返回同一revision/tombstone，不同fingerprint或stale version稳定409且不产生mention/outbox副作用。编辑追加不可变comment revision；普通删除显示稳定tombstone并保留作者、创建/编辑/删除时间、revision metadata与回复上下文，但必须以DB one-way redaction清除current及全部历史comment revision的Markdown/render/cache正文bytes/hash。只允许content列non-null→null，metadata不可改、null→non-null及tombstone后新增正文revision均拒绝；历史事件不能静默消失，已删正文也不能被旧writer恢复或继续留在DB、API、activity、通知或export中。
- 评论时间线保持平铺；`reply_to_comment_id` 只保存一层引用上下文，不形成无限嵌套线程。评论不得内联可执行 HTML、结构化 evidence/attachment 引用或任意上传字节。
- 提及使用稳定用户 ID，而不是显示名匹配；保存时重新验证被提及者属于当前项目且可读该记录。评论编辑只对本次新增提及发通知，重试不得重复投递。
- 作者、当前负责人和当前参与者默认形成自动关注来源；有权用户可手动关注或取消可选通知。取消关注不得屏蔽直接提及、分配、评论回复以及权限/安全类强制通知。
- 产品/UI统一使用“关注”，API使用`follow`；外部文档中的watch与follow是同一订阅语义，不建立会漂移的第二套watch模型。
- 站内通知至少覆盖直接提及、记录/行动项分配、评论回复、行动项受阻、跟进/行动项临期与逾期、重要业务状态变化和正式修订；本人触发与草稿自动保存不通知，高频事件按用户、记录和事件族聚合。
- 站内通知、关注、评论、行动项、外投选择、重试和深链打开全部复用服务端`recordauth.Policy`。notification candidate必须在同一policy snapshot下先隐藏/清理失权row，再从authorized projection计算rows、`unread_count`和cursor；禁止返回pre-filter count。权限撤销后列表、未读数、摘要、深链和待重试任务不得泄露记录存在性；外部语义统一404。
- Telegram/飞书只通过管理员显式启用并声明 audience scope 的 binding 外投。外部消息只含版本化事件短句与站内深链，不含标题、Markdown、评论/行动项全文、主体身份、证据、附件、敏感拓扑或永久禁止字段；外投失败不回滚业务事务。
- 外投 outbox 只保存 event/record/revision/recipient/binding/template/幂等引用，不保存已渲染正文。首次发送、每次重试和实际网络调用前都重新检查 reservation/deletion fence、记录/来源授权、recipient、binding 与 integration revision；目标解绑、撤权、可见范围收窄或永久删除必须取消并清理旧 payload/cache。
- 记录存在时站内通知与 delivery audit 默认最多保留 180 天并允许缩短。永久删除立即清除候风侧摘要、record/revision/recipient/integration/channel/message 关联和重试内容，只向最小删除审计提交 `external_copy_disclosed` 与无身份渠道类别/数量/receipt digest；已经投递到外部的消息必须在删除预览中说明无法召回。
- 行动项、评论、关注和通知必须提供 canonical activity/export provider，供后续 activity 与 portability child 消费；provider 缺失或版本不兼容时导出/永久删除失败关闭，不能静默省略协作历史。
- 当前任务不开放新的逐用户记录 ACL、匿名分享、聊天室、看板、Sprint、工时、无限子任务、任意依赖图或自定义字段引擎。

## Acceptance Criteria

- [ ] owner/participants/follow-up的新建、增删、重排、清空、恢复旧revision与并发冲突均只能通过完整正式revision表达；每个非空owner/participant在post-save visibility+source floor下仍为当前项目可读成员，跨项目、restricted-group外、visibility同revision收窄、source-floor拒绝时revision/projection/follower/activity/outbox提交数为0；root/current projection与current revision对账差异为0。
- [ ] 一般笔记/重要发现未显式开启跟进时，API 和 UI 均不伪造 owner/follow-up；稳定记录中逾期/受阻/无负责人提示容器、按钮和预留高度命中数为 0。
- [ ] action create/update/retry/CAS/cancel 的每次实际变化恰有一个不可变 event；非空assignee在每次写入都通过当前项目成员+record read校验，跨项目、撤权或仅display-name匹配的分配数为0；无变化重试不增加 event，全部完成只生成状态建议且记录 revision/status 与关联业务对象变化数为 0。
- [ ] 行动项筛选可独立表达状态、assignee、due state/range；跟进时间与行动项截止时间不会合并口径，服务端 `EXISTS` 查询不重复返回记录。
- [ ] comment create/reply/edit/delete使用安全Markdown；POST/PATCH/DELETE response-loss retry同key不重复comment/revision/tombstone/mention/outbox，不同fingerprint或stale If-Match为409且副作用为0。未删除时编辑历史可枚举；普通删除后原位置稳定显示tombstone、revision metadata与回复上下文，current/全部历史revision/render/cache的原正文在DB、API、activity、notification和export命中数均为0；SQL/service stale writer的null→non-null或tombstone后正文追加全部拒绝，记录revision/evidence/attachment引用均不改变。
- [ ] 非作者不能编辑他人评论；拥有 `record.comment.moderate` 的项目管理员只能以审计化删除处理他人评论，不能冒充作者改写正文。
- [ ] mention token 的用户 ID、显示 label、项目成员和记录授权均由服务端验证；仅改显示名不改变身份，新增提及通知一次，删除/重复提及和本人提及不产生噪声。
- [ ] 自动关注来源、手动 follow/mute、owner/participant 移入移出和恢复旧 revision 可重建；mute 只抑制可选关注通知，mention/assignment/reply/security 强制通知仍产生。
- [ ] 触发矩阵对本人操作、草稿、回复、分配、blocked、T-24h 临期、首次逾期、每 24h 逾期提醒、状态和 revision 聚合逐项可测试；同幂等 event/recipient/binding 不重复创建 inbox 或 delivery。
- [ ] notification list/unread count/mark-read在用户间隔离；在candidate scan、hide/clear、count、cursor和response每个cutpoint注入撤权后，row/title/summary/unread count/cursor/深链和枚举ID泄露数均为0，永不返回pre-filter count；权限恢复不会重放已经取消的外部任务。
- [ ] 外部 binding 未显式启用、audience scope 不覆盖记录、integration revision 漂移或 recipient 无权时投递数为 0；成功消息 hostile fixture 中标题、正文、评论/行动项文本、证据、附件、敏感拓扑和 secret 命中数均为 0。
- [ ] outbox 发送前、HTTP request 前和响应记录前注入撤权/reservation/permanent-delete，旧 payload/cache 清除且网络调用数为 0；外投失败只更新 delivery 状态并退避，不回滚原 revision/action/comment。
- [ ] collaboration activity/export provider 能完整还原action events、未删除comment revisions、已删除comment的revision metadata/tombstone、actor provenance和当前follower preference；删除comment的历史正文导出字节数为0，后续导出未注册provider时返回稳定不可用错误而不是缺段归档。
- [ ] permanent deletion adapter 清除全部协作正文、站内摘要、待投递与成功 delivery 关联；已投递外部消息只留下无身份最小披露聚合，`not_committed` outcome 不执行任何清除。
- [ ] migration在真实PostgreSQL覆盖“已有0051–0054且尚无0055时应用真实0056并repeat”，并用custom migration-filesystem fixture覆盖“先记录较大文件名、再发现并应用缺失较小文件名并repeat”；本任务不伪造未来search schema。错误依赖、重复apply漂移、cascade内容丢失和迁移号抢占数为0。
- [ ] 通知收件箱、TopBar bell、评论/行动项/关注受控组件在 loading/empty/error/submitting/revoked、桌面和 390px 下可访问；稳定态不显示异常占位，未读为 0 时不显示 `0` badge。
- [ ] focused Go/Web、真实 PostgreSQL、worker fake-clock/race、router/bootstrap、`make verify-go`、Node 22 `make verify-web`、focused Playwright 与 `git diff --check` 全部通过。

## Out of Scope

- 不实现实时多人共同编辑、通用聊天、匿名/公开链接或独立消息产品。
- 不把行动项升级为项目管理器，也不自动执行任何资产、订阅、监控或 Target 操作。
- 不在本任务实现记录详情/Markdown 主页面；本任务提供可独立测试的协作组件与 API，紧随其后的 Markdown child 按父任务顺序集成。
- 不把既有 `notification_records` 事件投递审计改名伪装成个人收件箱；两者保持不同资源和保留语义。

## Execution Gate

- 状态保持 `planning`；依赖合入、迁移号复核和用户再次明确批准缺一不可，不执行 `task.py start`。
