# 横向比较工作台

## Goal

交付可深链、可审计的 2–6 项横向比较工作台：用户先确认不可变 record revision/evidence snapshot，再由 evidence kind 合同执行时间、覆盖、schema 与单位对齐；系统可验证差异与人工结论严格分开，并可原子另存为新记录。

## Requirements

- 父设计：`../07-13-vps-detail-experience-design/design.md` §8、§12、§15、§18–§21、§24–§25；视觉基线：`../07-13-vps-detail-experience-design/research/visual-design-contract.md` Artifact `vps-records-visual-contract/v1` §4、§6。
- 直接依赖：子任务 2（records core）、4（evidence registry/adapters）、5（Markdown workspace）和 7（subject evidence page/activity）已合入 `main` 且 post-merge CI 通过；子任务 6 由 7 传递。本任务在 record center/revision/subject evidence 页面接入选择入口，但 comparison backend 不 import search/activity package。任务 7 是已批准 subject evidence 精确选择入口的真实前置，依赖与父任务表一致。
- 本任务不分配数据库 migration。比较运行是由不可变 revision/snapshot 重放的派生结果；持久化复用 task 2 的 draft/revision transaction、task 4 的 evidence snapshot/payload/copy-lineage 和 versioned kind registry，禁止另建可漂移的 saved-comparison 表。
- 工作台支持两阶段主体模式：用户选择 2–6 个 VPS/monitoring instance/Target 与绝对请求窗口，服务端只推荐 observation time 接近且 kind/schema 兼容的候选；用户必须确认每个 exact snapshot ID 后才比较。推荐不会采集新证据、自动改窗口或静默替用户选择。
- 精确模式接受 snapshot ID，或固定 `record_id + revision_id`。record selection 只展开该 revision 实际引用的 snapshots；同 kind 多 snapshot 必须用户明确选择；无证据 revision 保留为 `metadata_only`，不生成 0、空图或“最新”替代。
- `POST /api/evidence/comparison-candidates` 只负责 subject/time 候选；批准的 `POST /api/evidence/comparisons` 只接受 2–6 个 fixed revision/snapshot selections、显式 baseline、alignment、tolerance、bucket width、requested window 和可选 kind/metric detail。客户端不能提交任意 payload、单位转换或已计算差值。
- comparison response 逐项返回 subject/snapshot identity、schema/content hash、requested/actual/observed/captured window、bucket/point/sample counts、quality、source status 和 freshness-at-capture；`record_id + revision_id` selection 必须另返回该固定revision当时的type/business status/status group/impact/occurred-at结构化元数据。纯 snapshot selection 明确返回 `revision_context=not_applicable` 与 `revision_metadata=null`，因为它未绑定record revision；不得从current root、任意引用它的record或“最近revision”回填。metadata-only revision行仍返回完整不可变revision元数据。逐 kind 返回 compatibility、common window、calculation version、warnings 与可计算结果。
- baseline 由用户显式确认；首项只能作为有文字说明的默认建议。切换 baseline、`actual_coverage|common_overlap`、tolerance、bucket width、requested window、fixed IDs 或 kind/metric 都产生新的规范化条件和 request digest。
- `actual_coverage` 保留各 snapshot 真实覆盖；`common_overlap` 只有 kind.Compare 明确允许在真实交集上重聚合时才启用，bucket 不得细于任一 source bucket且不得插值。交集为空、schema/unit/precision 不兼容或 kind 不支持重聚合时阻止数值计算。
- 时序匹配必须确定性：`actual_coverage` 不重采样，只有相同bucket语义/宽度且起止偏移在显式tolerance内才按单调一对一最近匹配计算逐桶差；最小绝对偏移相同则按较早UTC时间、canonical decode后由时间/metric/sample identity排序得到的source ordinal、stable hash依次决胜，同一bucket不可复用。ordered comparison items本身有业务意义并进入digest，但底层map/decoder迭代顺序打乱不能改变结果。`common_overlap` 以UTC epoch锚定、bucket不细于任一source的canonical grid，只纳入完整落在真实交集内的bucket，edge partial不进入数值；tolerance不能扩张交集、跨gap或制造点。
- missing/quality 状态使用稳定 reason code 和文字+形状+颜色：`metadata_only`、`kind_missing`、`metric_missing`、`coverage_partial`、`coverage_truncated`、`common_overlap_empty`、`schema_incompatible`、`unit_incompatible`、`precision_incompatible`、`source_tombstoned`、`source_unavailable`、`snapshot_unreadable`。0 只在 snapshot 明确观测为数值 0 时显示；空白不表示缺失。
- snapshot 本身不可变；“stale”描述 capture 时 source policy/observed-to-captured freshness，“age”描述该历史 snapshot 距当前时间，current source `live|tombstoned|unavailable|restricted` 另列。工作台不得重读 live source 后改写旧 snapshot 或把 source 删除误报为 snapshot 数值缺失。
- 数值差、变化率、百分点和 risk band 只由相同 evidence kind 的 versioned Compare contract在相同语义/单位或显式无损 conversion 下生成；Markdown、附件文本和跨 IP/成本/监控/路由/性能证据不参与数值推导，不生成“最佳 VPS”总分。
- 趋势图按真实 segment 绘制并在数据终点停止；gap、maintenance 和 backfill 不连线、不补零、不外推。矩阵列头同时显示主体、fixed revision/snapshot、实际覆盖、桶数和质量；不可计算 cell 显示 reason label。
- 规范化 comparison state 写入 `/records/compare` URL：entry mode、ordered fixed selections、baseline、alignment、requested UTC window、tolerance、bucket width、active kind/metric。它是登录内可分享的重放说明，不是 bearer link；每次打开重新授权并重新计算，未授权 selection 统一 404且不泄露其身份。
- compare response 返回短期、server-bound `comparison_intent`，绑定 actor/project、fixed IDs/hashes、conditions、registry/calculation versions、warnings、auth/fence head 和 expiry。另存记录使用标准 records create/revision transaction消费 intent：为目标记录创建独立 logical snapshot copies（保存 `copied_from_snapshot_id`）、生成 `comparison.result/v1` derived evidence snapshot，并原子写 revision refs/domain activity/outbox；source records/snapshots 永不修改。
- comparison intent使用独立、版本化、跨实例共享的HMAC keyring；token包含purpose与key ID，TTL固定15分钟。key file必须是预期owner的0400 regular file，使用no-follow/openat语义拒绝symlink、目录、device、hard-link替换与读取后inode漂移；签名key与历史verify key按“两阶段分发verify key→确认全部member→切current”轮换，至少保留到最后签发时间+TTL+允许时钟偏差；不得使用进程随机key、deletion-ledger idempotency key或backup签名key。
- response明确返回 `save_eligibility` 与blocker reason。`metadata_only`、schema不兼容或无数值结果仍可把可验证条件/告警另存；但任一选择的payload/hash无法读取、权限/fence不可续租或logical copy不可完成时不签发comparison intent，Web不渲染保存动作而显示实际blocker。禁止跳过unreadable selection后保存一份看似完整的结果。
- `comparison.result/v1` 只固化系统可验证选择、条件、差异和告警；人工标题、primary subject、类型/可见范围与 Markdown conclusion 继续是用户 revision 内容。保存重验权限、intent digest、source hashes、quota、reservation 和 Idempotency-Key；任一步失败不产生半份 record/copy/result。
- traceability 至少覆盖 comparison digest、baseline、ordered original/copied snapshot IDs 与 hashes、每个revision-bound item的type/status/status-group/impact/occurred-at元数据快照、每个snapshot-only item的`revision_context=not_applicable`、requested/actual/common windows、alignment/tolerance/bucket、schema/registry/calculation version、quality/warnings、created record/revision 和 actor/time。renderer 与 Export contract 必须能让 task 10 的 Markdown/PDF/archive 导出同义复现；本任务不创建未审计的客户端 CSV/文件下载。
- 工作台按“selection conditions → comparability review → evidence kind tabs → trend/matrix → system differences → human conclusion → save”排序。comparability review 必须先于任何指标。desktop 与 390px、loading、少于 2 项、无兼容 evidence、单项/kind 失败、长计算、cancel、save conflict 和 revoke 均遵守 Artifact v1。
- 390px 一次只显示一个 kind/metric；conditions 可折叠但可回看，comparability review 始终在首指标前；matrix 只有具名、可聚焦的局部横向滚动区和 sticky row header，保存动作位于 conclusion 后，不覆盖内容。
- 本任务的 Artifact v1 视觉基线证据是 Playwright 语义、几何、overflow、Axe、键盘/focus 合同与短期人工评审材料；遵守现有 Web spec，不提交 tracked pixel golden、screenshot manifest 或 bulk raster。
- 服务端硬限制 2–6 items、每 item 每 kind 一个用户确认 snapshot、输入 body 256 KiB、沿用单 snapshot 50,000 points/5 MiB上限。summary/candidate 使用 batched query；series detail 按 active kind+metric lazy request，每 series 最多 2,000 aligned buckets。context cancel 终止解压/对齐，Web AbortController 取消旧条件请求。
- 单请求 96 MiB 只是局部上限，不能作为并发安全证明。comparison capability 开启时必须配置 cgroup-aware weighted admission：按 payload/point/series 上界保守估算并以 8 MiB token计费，参考 4 GiB application container 的 aggregate comparison budget 固定 512 MiB、最大等待 2 秒、队列最多 16 个请求；超过预算返回 429 `comparison_capacity_exhausted` + `Retry-After`，单请求实际 working-set 越界返回 `comparison_request_memory_limit`，不截断或签发 intent。取消/断连后只有计算goroutine和response writer均停止才释放token，最长5秒；超时使comparison readiness失败关闭。生产可降低budget；提高必须仍保留至少1 GiB非comparison headroom并通过task11同规格混合负载与cgroup peak gate。
- comparison capability 默认关闭；关闭 API/route 不影响 records/evidence。已保存的 `comparison.result/v1` 必须继续由 registry renderer/exporter 读取，不能因工作台 rollback 变成任意 JSON fallback。

## Acceptance Criteria

- [ ] subject candidate 对 2–6 个主体返回确定性、权限安全的兼容候选；用户未确认 exact IDs 前 comparison request 数为 0，推荐不会创建 snapshot 或改变 source。
- [ ] fixed revision 只展开该 revision 引用；current revision 后续推进、页面刷新或 source 新 capture 后，已分享 URL 的 revision/snapshot IDs、hash 和结果不漂移。同 kind 多 snapshot 未选择时稳定阻止比较。
- [ ] fixed revision的type/status/status-group/impact/occurred-at来自该immutable revision并进入response/digest/saved result/export；root/current变化后metadata-only与有证据行均不漂移。snapshot-only item在四个表面都保持`revision_context=not_applicable`/null metadata，测试证明不会借用任一current或引用revision。
- [ ] baseline 与 actual/common-overlap 切换完整进入 URL/request/intent/provenance；common overlap 为空或不支持时数值 cell/图表数为 0并显示 reason。
- [ ] actual coverage的单调一对一匹配、tolerance边界和UTC/canonical-ordinal/hash tie-break在decoder/map迭代顺序变化后结果/digest不变；ordered comparison item顺序变化会按设计改变baseline/摘要digest。common grid不含partial edge、跨gap、bucket复用、插值或由tolerance扩张出的点。
- [ ] missing、partial、truncated、不同bucket、time offset、stale-at-capture、aged、tombstoned、unavailable与“已注册可读但Compare-incompatible schema”在API、Web、saved result和export DTO中语义一致；缺失被渲染为0/空白/插值/外推的次数为0。真正unregistered权威schema与external quarantine artifact均不能进入selection/result/intent。
- [ ] kind Compare conformance 证明只有兼容 schema/unit/meaning 计算差异；Markdown-derived metric 和 cross-kind aggregate score 生成数均为 0。
- [ ] 趋势每个 gap 产生独立 SVG segment，线条跨越无数据区间数为 0；矩阵每个不可计算 cell 都有可读 reason，颜色不是唯一状态通道。
- [ ] URL codec 对 ordered 2–6 selections、UTC window、baseline/alignment/tolerance/bucket/kind/metric 往返稳定；损坏、重复、超限、未知参数规范化或拒绝，不携带 payload/secret。分享 URL 每次重鉴权，无权 identity/count 泄露命中为 0。
- [ ] save-as-record 在同一 transaction创建 record/revision、独立 logical copies、`comparison.result/v1`、refs/domain activity/outbox；DB/copy/quota/fence/auth/idempotency failure 要么全部提交，要么全部不变。同 key retry 不产生第二条记录。
- [ ] readable metadata-only/incompatible选择可保存完整告警；任一`snapshot_unreadable`/hash mismatch/copy blocker时intent和保存动作均不存在，尝试伪造token/请求稳定失败且不会跳过该选择。
- [ ] saved result 独立还原 baseline、original/copied IDs+hashes、全部窗口/条件/schema/version/warnings和系统差异；human conclusion 只存在 revision Markdown，不被标为系统事实；source record/snapshot mutation count 为 0。
- [ ] comparison renderer 与 task 4 Export DTO roundtrip 等价；task 10 可从同一 DTO生成安全人类/机器导出。工作台没有绕过 export preview/auth/audit 的直接下载面。
- [ ] 6 items × 每项 2,000 aligned buckets 的 detail benchmark 在父参考资源上 p95≤2s、response≤2 MiB、fresh隔离container单request cgroup peak-memory相对GC后idle增量≤96 MiB；candidate/summary p95≤1s，数据库 query count 有界。512 MiB aggregate budget下5个最大请求可并发、额外请求在≤2秒内稳定429且不启动payload decode；取消/断连后后台计算、response write和weighted token在≤5秒收敛为0，混合负载时container无OOM/throttle且comparison tracked working set不越界。
- [ ] comparison intent在restart、两replica滚动、verify-key预分发、current切换、旧key过TTL、未知/撤销key、purpose混用和时钟边界下结果稳定；任一实例不具备完整verify set时comparison capability不接流量。
- [ ] desktop/390 的初始选择、metadata-only、无兼容 kind、partial、processing/cancel、save success/conflict/error、revoke fixtures 符合 Artifact v1；Axe critical/serious 为 0，纯键盘可完成选择/切换/保存，焦点恢复正确，触摸目标≥44px，document无横向溢出。
- [ ] record center、record revision/evidence list 和 subject evidence page 的精确选择入口都进入同一 `/records/compare` query contract；旧 `/monitoring/compare` 行为不变且不被当作 fallback。
- [ ] focused Go/PostgreSQL/Web/Playwright、`make verify-go`、Node 22 `make verify-web`、`npm --prefix web run test:e2e`、`git diff --check` 和 required CI 全部通过。

## Out of Scope

- 不比较 mutable live runtime response，不自动 capture 新证据，不从 Markdown/附件抽取指标，不构建跨 kind 总分或自动采购/迁移动作。
- 不提供匿名/公开 comparison link；URL 分享仅在候风登录与统一授权内重放。
- 不新增 comparison persistence/migration、通用 dashboard builder、任意 JSON renderer 或客户端持久 comparison cache。
- 不在本任务生成 Markdown/PDF/archive 文件、敏感拓扑导出或下载链接；task 10 拥有 export job/preview/authorization，task 8 只提供完整 renderer/export material contract。
- 不替换或删除现有 `/monitoring/compare`，不修改 source record/revision/snapshot，不用旧 live A/B 页面回退不可变比较。

## Execution Gate

- 保持 `planning`；直接依赖、父顺序集成面和用户独立执行授权全部满足后才可运行 `task.py start`。
