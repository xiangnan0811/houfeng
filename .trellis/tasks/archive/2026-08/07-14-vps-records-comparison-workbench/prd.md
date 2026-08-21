# 横向比较工作台

## Goal

交付可深链、可审计的 2–6 项横向比较工作台：用户先确认不可变 record revision / evidence snapshot，再按已注册 evidence kind 做可比性审查与差异；系统可验证差异与人工结论严格分开，并可原子另存为新记录。

## Background

依赖 Child 2/4/5/7 已在 `origin/main` `ffda9a07` / `v0.70.0`。本任务无 root migration，不承担旧库、legacy、staging/release 或跨版本 token compatibility。已保存的 `comparison.result/v1` 一旦注册，自身 schema/renderer/export 必须可回读。

2026-08-20 对照（`research/current-main-reconciliation-2026-08-20.md`）后，用户批准范围 **A**：

- 复用已落地 pairwise `Kind.Compare(..., AlignmentExact)` 处理非时序 kind。
- trend / matrix series 只为 `monitoring.host/v1` 与 `monitoring.probe/v2` 新建。
- `common_overlap` 在 kind 未声明重聚合前只返回稳定 blocked reason。
- 另存与 HMAC intent 留在本 child。
- 4 GiB cgroup / 512 MiB mixed-load harness 交给 Child 11。
- Overview 管理面板写入、activity group-granted digest 扩组不是本任务。

当前 main 合同：

- 已注册 kind：`ip_quality.report/v1`、`monitoring.host/v1`、`monitoring.probe/v2`、`monitoring.event/v2`、`subscription.cost/v1`、`command.audit/v1`。不存在 `monitoring_timeseries/v1`。
- `Kind.Compare` 只做 pairwise 精确窗口聚合差，不是 2–6 项 series 编排器。
- 比较 HTTP 与 `/records/compare` 不存在。`GET /api/evidence/{id}`、`POST /api/records`、`evidence_items`、`comparison.read`、`evidence_copy_lineage` 表已存在；copy 写路径与 `comparison.result/v1` 不存在。
- 记录中心是 `RecordSearchPage`。修订字段是 `record_type` / `business_status` / `status_group` / `impact_level` / `occurred_at`。
- subject evidence 走 `GET /api/subjects/{vps|monitoring_instance|target}/{id}/activity?view=evidence`。
- `useRecordDraft.publish()` 不传 `evidence_items`。另存必须自建 records create/revision 请求。
- `recordauth` 已支持 restricted group grant；activity viewer 只放行 project digest。比较必须重鉴权，不扩 activity allowlist。
- `/monitoring/compare` 是独立 live A/B 工具，必须保持原样。

## Requirements

- 父设计：`../07-13-vps-detail-experience-design/design.md` §8、§12、§15、§18–§21、§24–§25，以 2026-08-20 范围 A 收口；视觉基线：`research/visual-design-contract.md` Artifact `vps-records-visual-contract/v1` §4、§6。
- 入口挂在 `RecordSearchPage`、`RecordRevisionPage`、`SubjectEvidencePage`，都进入同一 `/records/compare` query contract。comparison backend 不 import `internal/center/activity` 或 `recordsearch`。
- 无数据库 migration。交互运行不持久化。另存复用 records create/revision transaction、已有 `evidence_snapshots` / payload / `evidence_copy_lineage` 表和 versioned kind registry。禁止另建 saved-comparison 表。
- 两阶段选择：`POST /api/evidence/comparison-candidates` 只接受 2–6 个 registry subject（`vps` / `monitoring_instance` / `target`）与绝对 UTC 窗口，返回权限安全的 snapshot/revision 候选，不 capture、不签发 intent。`POST /api/evidence/comparisons` 只接受 2–6 个已确认 snapshot XOR `record_id+revision_id`、显式 baseline、alignment、tolerance、bucket width、requested window 和可选 kind/metric detail。客户端不能提交 payload、单位转换或已计算差值。
- revision 只展开该不可变修订的 refs，并返回当时的 `record_type` / `business_status` / `status_group` / `impact_level` / `occurred_at`。同 kind 多 snapshot 必须用户选择。无证据修订为 `metadata_only`。纯 snapshot 项固定 `revision_context=not_applicable` 且 `revision_metadata=null`，不得从 current root 或任意引用记录回填。
- baseline 由用户确认；首项只作有文字说明的默认建议。切换 baseline、alignment、tolerance、bucket、window、fixed IDs 或 kind/metric 都产生新的规范化条件与 digest。
- 非时序 kind（ip / cost / event / command）的数值差只来自现有 pairwise `Kind.Compare` allowlisted DTO。host/probe 才允许 `actual_coverage` 单调一对一 nearest 匹配生成 ≤2,000 aligned buckets 的 gap-aware series。`common_overlap` 在当前 kind 上返回 `common_overlap_unsupported` 或 `common_overlap_empty`，不实现无消费者的重聚合引擎。
- 时序匹配（仅 host/probe、`actual_coverage`）必须确定性：相同 bucket 语义/宽度且起止偏移在显式 tolerance 内才配对；并列时按较早 UTC、canonical source ordinal、stable hash 决胜；map/decoder 迭代顺序不能改变结果；ordered item 顺序进入 digest。禁止插值、补零、外推、跨 gap 连线。
- 稳定 reason：`metadata_only`、`kind_missing`、`metric_missing`、`coverage_partial`、`coverage_truncated`、`common_overlap_unsupported`、`common_overlap_empty`、`schema_incompatible`、`unit_incompatible`、`precision_incompatible`、`source_tombstoned`、`source_unavailable`、`snapshot_unreadable`。0 只在 snapshot 明确观测为数值 0 时显示。
- source 生命周期只映射 `live|tombstoned`；另列 `source_available` 与 retention reason（如 `snapshot_retained_source_unavailable`）。restricted 是 visibility，不是 source state。freshness 拆成 `freshness_at_capture`、`snapshot_age`、current source 状态。
- 跨 kind 总分、Markdown/附件抽指标、live runtime 比较均为 0。
- `/records/compare?state=` 使用 `comparison-url/v1`。state 只含 IDs/条件。每次打开重鉴权并重算。未授权 selection 统一 404，不泄露身份或计数。
- eligible compare 才签发 15 分钟 HMAC `comparison_intent`（purpose `comparison-save/v1`）。另存走标准 records create/revision + `Idempotency-Key` + `evidence_items`（copied snapshot IDs）+ comparison intent 字段；在同一 transaction 写 logical copies（`copied_from_snapshot_id`）、`comparison.result/v1`、revision refs。source record/snapshot 不修改。不能复用 `useRecordDraft.publish()`。
- HMAC keyring 独立 0400 regular file、no-follow/openat、不复用 deletion/backup/session key。capability 开启且要签发 intent 时，缺完整 verify set 则不签发 intent。
- `save_eligibility` 明确返回。`metadata_only` / 不兼容 / 无数值仍可保存条件与告警。任一 `snapshot_unreadable`、hash mismatch、copy/auth/fence blocker 时不签发 intent，Web 不渲染保存动作。
- `comparison.result/v1` 只固化系统可验证选择、条件、差异和告警。人工标题、主体、可见范围与 conclusion Markdown 留在 revision。renderer / Summarize / Export 必须可供 Child 10 复用；本任务不提供下载面。
- 页面顺序：selection → conditions → comparability review → kind tabs → trend/matrix（仅 host/probe series；其他 kind 用 Compare DTO / 矩阵 reason）→ system differences → human conclusion → save。comparability 必须先于任何指标。遵守 Artifact v1 desktop/390px，不提交 pixel golden。
- 硬限制：2–6 items、body 256 KiB、单 snapshot 既有 50k points / 5 MiB、detail 每 series ≤2,000 buckets、response ≤2 MiB。candidate/summary 批量查询；detail 按 active kind+metric lazy 请求。
- comparison capability 默认关，叠在 `HOUFENG_RECORDS_ENABLED` 之上，不新增 `comparison.read` 字符串。关闭 route 不影响 records/evidence。进程内 weighted admission 在 payload 解码前申请；饱和返回 429 `comparison_capacity_exhausted`，越界返回 422 `comparison_request_memory_limit`。4 GiB 容器 peak 与混合负载门归 Child 11。

## Acceptance Criteria

- [x] subject candidate 对 2–6 个主体返回确定性、权限安全的兼容候选；确认 exact IDs 前 `POST /api/evidence/comparisons` 次数为 0；推荐不创建 snapshot 或改 source。
- [x] fixed revision 只展开该修订引用；current 推进或新 capture 后，已分享 URL 的 IDs/hash/结果不漂移。同 kind 多 snapshot 未选择时稳定阻止比较。
- [x] revision-bound 的 `record_type` / `business_status` / `status_group` / `impact_level` / `occurred_at` 来自该不可变修订，并进入 response / digest / saved result / export。snapshot-only 项在这些表面保持 `revision_context=not_applicable` / null metadata。
- [x] baseline 与 alignment 切换进入 URL / request / intent / provenance。当前 kind 选择 `common_overlap` 时数值 cell / host-probe 图表数为 0，并显示 `common_overlap_unsupported` 或 `common_overlap_empty`。
- [x] host/probe `actual_coverage` 的单调一对一匹配、tolerance 与 UTC/ordinal/hash tie-break 在 decoder/map 迭代打乱后 digest 不变；ordered item 顺序变化会改变 digest。
- [x] missing / partial / truncated / tombstoned / unavailable / Compare-incompatible 在 API、Web、saved result、export DTO 语义一致；缺失被渲染为 0 / 插值 / 外推的次数为 0。
- [x] 非时序 kind 的数值只来自 pairwise `Kind.Compare` DTO；Markdown-derived metric 与 cross-kind 总分生成数为 0。
- [x] host/probe 趋势每个 gap 一个独立 SVG segment；其他 kind 不画假 series。不可计算 cell 有可读 reason，颜色不是唯一通道。
- [x] URL codec 对 2–6 selections 与条件往返稳定；无权 identity/count 泄露为 0。
- [x] save-as-record 在同一 transaction 创建 record/revision、logical copies、`comparison.result/v1`、refs；失败全不变。同 Idempotency-Key 不产生第二条记录。source mutation 为 0。
- [x] unreadable / hash / copy blocker 时 intent 与保存动作不存在；伪造 token 失败。
- [x] saved result 能还原 baseline、original/copied IDs+hashes、条件、warnings 和系统差异；human conclusion 只在 revision Markdown。
- [x] `comparison.result/v1` renderer / Export DTO roundtrip 等价；工作台无直接下载面。
- [x] 进程内 admission：饱和 429、越界 422、cancel 后 token 在测试时限内释放；不把 4 GiB cgroup peak 或混合负载作为本 child 退出门。
- [x] HMAC intent 在 restart、未知/撤销 key、purpose 混用下失败关闭；key 不入 DB/log，不复用 deletion/backup key。
- [x] desktop/390 Artifact v1 fixture：Axe critical/serious = 0，键盘可完成选择/切换/保存，触摸目标 ≥44px，document 无横向溢出。
- [x] 三个入口进入同一 `/records/compare` contract；`/monitoring/compare` suite 保持 GREEN。
- [x] focused Go/PostgreSQL/Web/Playwright、`make verify-go`、Node 22 `make verify-web`、`npm --prefix web run test:e2e`、`git diff --check` 已通过。required CI 待 PR，本 worktree 未推送。

## Out of Scope

- 不比较 mutable live runtime，不自动 capture，不从 Markdown/附件抽指标，不生成跨 kind 总分或采购/迁移动作。
- 不提供匿名/公开 comparison link。
- 不新增 comparison 表 / migration，不做通用 dashboard 或任意 JSON renderer。
- 不生成 Markdown/PDF/archive 下载；Child 10 拥有 export job。
- 不替换 `/monitoring/compare`，不用它做 fallback。
- 不写 VPS overview 管理面板真实写入。
- 不扩展 activity viewer 的 group-granted digest allowlist。
- 不实现 registry-wide Compare descriptor 语言，不为当前 kind 实现 `common_overlap` 重聚合。
- 不交付 disposable 4 GiB 容器 peak / 512 MiB mixed-load harness（Child 11）。

## Execution Gate

- 范围 A 已实现。工作提交：`d91987e6`、`3f95a3db`。
- 390 Artifact 以 named scroll + 指标选择器交付；sticky 行标题在 CSS 棘轮下未做，不重勾为完成。
- 4 GiB / mixed-load harness 仍归 Child 11。
- 禁止按 2026-08-02 `implement.md` 原文回改范围。禁止直接改 main。
