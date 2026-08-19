# 活动投影、单主体页面与 VPS 概览 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`; Codex inline only. Every production behavior follows RED → verify RED → minimal GREEN → verify GREEN → refactor.

**Goal:** 交付无重复、可固定水位分页的 canonical activity，以及稳定态真正无异常占位的 VPS overview 和三类主体纵向工作区。

**Architecture:** `activity` 把 records/evidence/asset/monitoring/command 权威事件投影到 0057 派生表；`vpsoverview` 聚合现有 source reader 和同一 activity service；Web 用一套 subject query/controller 组合 activity/records/evidence 路由，并在 canonical VPS route 内以 capability 保留 legacy 回退。

**Tech Stack:** Go 1.26.2、pgx/v5、PostgreSQL、React 19、TypeScript 6、React Router 7、Vitest/Testing Library、Playwright/axe、纯 CSS owner manifest。

---

## 2026-08-02 execution override

- 从 Child 2/4/6/9 已接受的 protected main 开始。
- `0057` 同时交付 current APP ACL managed surface/privileges/admission tests。
- 只验证 fresh/repeat；不做 old-database/`experience_logs`/staging/release work。
- 保留固定水位、授权、局部失败、性能和响应式门，因为它们是当前功能正确性。

## Preconditions

- [x] 先读 `research/current-main-rebaseline-2026-08-19.md`。本计划的依赖意图仍然成立，
  但其中的 `recordcursor` / AES-GCM 游标、`act_` 前缀、`record_state_changed` 等事件名、
  `LegacyVPSDetail.tsx` 起点和 `records_v2_read` 现状都与当前 main 不符；以 rebaseline 为准。
- [x] 直接依赖子任务 2、4、6、9 已合入 `main` 且 CI 通过；`record_domain_activities` 能承载真实 comment/action events。
  已核实：comment/action/协作字段变更均有真实写入方（`store/record_comments.go:518`、
  `store/record_actions.go:530`、`store/record_collaboration_participant.go:368`）。
- [x] 从最新受保护主线创建非 `main` 分支 `codex/vps-records-activity-overview`，运行 `sh scripts/setup-git-hooks.sh`，
  并按 `trellis-before-dev` 读取 backend spec（`database-guidelines.md`、`record-search-index-contract.md`、
  `record-authorization.md`）与 `guides/index.md`。
- [x] 检查当前 migration 序列：`db/migrations/` 最高为 `0056_create_record_search.sql`，`0057` 未被占用。
- [x] 记录 baseline：`make verify-go` exit 0；`go test ./internal/center/http/handlers ./internal/center/store -run 'VPSTimeline|Experience' -count=1` 全 GREEN。
  本地无 PostgreSQL，改用一次性 `postgres:16` 容器提供 `HOUFENG_DATABASE_URL` 跑 integration 门。

## Task 1: 0057 schema、envelope、event-time 与 cursor

**Files:**

- Create: `db/migrations/0057_create_record_activity.sql`
- Create: `internal/center/activity/types.go`
- Create: `internal/center/activity/types_test.go`
- Create: `internal/center/activity/event_time.go`
- Create: `internal/center/activity/event_time_test.go`
- Create: `internal/center/activity/cursor.go`
- Create: `internal/center/activity/cursor_test.go`
- Modify: `internal/center/store/migrate/migrate_test.go`
- Modify: `internal/center/store/migrate/postgres_integration_test.go`

- [x] 写 RED tests 固定 non-null slices、source identity→deterministic activity ID golden、correction、event/recorded time、
  backfilled、confidential cursor namespace/query/auth/generation/as-of/full sort tuple、revision validity interval，
  以及 0057 published-head 行锁、relation 全过滤字段冗余 hash/索引/CHECK/unique/no-cascade 合同。已确认 RED。
- [x] 实现 immutable value types、UTC normalization、confidential cursor codec 与幂等 0057 migration；
  响应不暴露 global generation/head/checkpoint（`Event` 的 `IngestSequence`/`AuthScope` 为 `json:"-"`），
  presentation 只接受注册版本并受 `pg_column_size <= 4096` 约束，拒绝 arbitrary map/raw payload。
  **计划偏差**：Child 6 未交付可复用的 `recordcursor` confidential codec（其游标是可解码的 base64 JSON），
  因此本任务自建 `activity.CursorCodec`（AES-256-GCM + 固定 512B 明文桶 + 随机 nonce，密钥由
  `HOUFENG_SESSION_HMAC_KEY` 经 HMAC-SHA256 域分离派生）。加密而非签名是硬要求：payload 内含
  `projection_generation` 与 `as_of_ingest_sequence`，可解码的游标等于把全局水位交给浏览器。
- [x] 运行 `go test ./internal/center/activity ./internal/center/store/migrate -run 'Activity|RecordActivity' -count=1` PASS（activity 30 项、migrate 17 项、0 SKIP）。
  以一次性 `postgres:16` 容器提供 `HOUFENG_DATABASE_URL`，运行
  `HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store/migrate -run 'RecordActivity|AppACLCurrent' -count=1 -v`：
  fresh apply、exact repeat、current APP ACL convergence/admission 全 PASS 且无 SKIP；另直接对同一 SQL 连跑两次确认幂等。

## Task 2: Source adapters、projector、checkpoint 与 deletion fence

**Files:**

- Create: `internal/center/activity/projector.go`
- Create: `internal/center/activity/projector_test.go`
- Create: `internal/center/activity/worker.go`
- Create: `internal/center/activity/worker_test.go`
- Create: `internal/center/activity/recovery_adapter.go`
- Create: `internal/center/activity/recovery_adapter_test.go`
- Create: `internal/center/activity/deletion_adapter.go`
- Create: `internal/center/activity/deletion_adapter_test.go`
- Create: `internal/center/activity/export_readiness.go`
- Create: `internal/center/activity/export_readiness_test.go`
- Create: `internal/center/activity/adapters/record_domain.go`
- Create: `internal/center/activity/adapters/record_domain_test.go`
- Create: `internal/center/activity/adapters/evidence.go`
- Create: `internal/center/activity/adapters/evidence_test.go`
- Create: `internal/center/activity/adapters/asset_history.go`
- Create: `internal/center/activity/adapters/asset_history_test.go`
- Create: `internal/center/activity/adapters/monitoring_events.go`
- Create: `internal/center/activity/adapters/monitoring_events_test.go`
- Create: `internal/center/activity/adapters/command_audits.go`
- Create: `internal/center/activity/adapters/command_audits_test.go`
- Create: `internal/center/store/record_activity.go`
- Create: `internal/center/store/record_activity_test.go`
- Modify (created by child 2): `internal/center/recorddeletion/service.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

### Task 2a: source 契约、连续发号与 projector 核心（已完成）

- [x] 写 projector/head-lock RED matrix 并确认 RED：worker A 持 published-head 行锁延迟 commit、worker B 竞争，
  证明 B 不能越过且 A rollback 后 B 重新取到连续 range；全重复/部分重复 batch retry 先分类 existing/hash、
  只为 missing 分号；同 identity 不同 hash 拒绝；retired generation 拒绝写入。
- [x] 冻结 activity-owned `SourceAdapter` / `ExportReadySourceAdapter` / `ActivitySnapshot` / `ReadinessVector`；
  实现 store 侧 head-lock 连续发号（`PublishActivityBatch`）、checkpoint 仓储与 projector 核心。
  final path 无 `ON CONFLICT DO NOTHING`：意外冲突整批 rollback，rollback 释放号而不烧号。
- [x] **`AuthoritativeHead` 语义定案（混合式，用户已确认）**：五个 source 都没有提交序单调列，
  因此拆成两种强度。增量投影用 `NewIncrementalSourceHead`（滞后 `recorded_at` 水位）+ 尾部窗口幂等重扫；
  导出完整性用 `NewSettledSourceHead`（带事务视界），`ReadinessVector.ValidateForExport` 在证据不足时 fail closed。
  一个只有滞后水位的 head 无法支撑完整性声明（`SupportsCompletenessClaim` 返回 false）。
- [x] **扫描窗口拆成前向/尾部两段**：`FrontierWindow` 双端闭区间（边界同刻行重读而非跨过，
  因为按 recorded_at 分页只能推进到该时刻），`TrailingWindow` 每轮回扫 checkpoint 之下的重叠区，
  这是晚提交行唯一被看到的途径。原先单一 `ScanWindow` 会让「整页都是已投影的旧行」永久卡住 checkpoint。
- [x] adapter 输出按不可信输入校验：跨 source 冒名、非派生 activity ID、hash 不覆盖内容、
  超出请求窗口的 recorded_at、无可达 subject、未注册 presentation 版本一律拒绝且不推进 checkpoint。
- [x] checkpoint 推进纪律：整窗读完才推进到 head，截断页只推进到实际读到的最后一行；
  失败只累加 attempt/error code，位置不动；`caught_up` 要求前向与尾部都读完。
  0057 checkpoint 表改用 `recorded_through timestamptz`（替换原 `source_head_digest`/`source_cursor`），
  因为位置是 source 自己的时间而不是我们输出里的序号。
- [x] 验证：`go test ./internal/center/activity -count=1` 42 项 PASS（projector 29 个子测试），
  `-race -count=10` PASS；变异测试 5/6 被捕获（第 6 个是我写的空操作变异，非测试缺口）。
  真实 PostgreSQL：`PublishActivityBatch` 5 项 + checkpoint 5 项 + projector 端到端 3 项全 PASS，
  含「晚到行拿到更晚的 ingest_sequence 但按真实 event_at 落到时间线中间」与「backlog 跨页 drain 后仍无洞」。
  改过 migration 后跑完整 `./internal/center/store` integration 套件（205s）全 PASS。
  注：`VerifyAppACLEffectiveCatalogR1` 系列在本地单 superuser 容器里失败，已用 `origin/main` worktree 确认同样失败，
  属环境缺少 migrator/runtime 角色分离，非本次回归。

### Task 2b: 五个 source adapter、worker 与 deletion/recovery（待做）

- [ ] 写每个 source 的 adapter RED matrix：revision 1 去重、后续 revision、状态/visibility、evidence coverage、
  历史 VPS link interval、asset histories、monitoring correction/backfill、command metadata-only、
  comment/action domain events、旧 reservation epoch。
- [ ] 实现五个具体 adapter（`record_domain`、`evidence`、`asset_history`、`monitoring_events`、`command_audits`）
  与 platform lease / outbox wake-up；每个 adapter 需同时提供滞后 head 与带事务视界的 settled head，
  后者用 `xmin` / `pg_snapshot` 判定，才能进入 export readiness。legacy `experience_logs` 不直接注册 adapter。
  `ActivityExportReader` type 与 revision interval 写入也在本子任务。
- [ ] 实现并注册 `activity.NewDeletionAdapter`：reservation后阻止/等待旧publish，清目标record的presentation主行、relations、revision intervals、overview recent/summary cache并独立verify零命中；receipt不含identity/content且不跨包删除。实现 `activity.NewRecoveryAdapter`，只清空/重建canonical activity、revision intervals、generation/checkpoint与overview summary；灾难rebuild递增generation，删除重放不能恢复旧presentation行。
- [ ] 运行 `go test -race ./internal/center/activity/... ./internal/center/store -run 'Activity|Projector|RecordDomain|EvidenceActivity|AssetHistoryActivity|MonitoringEventActivity|CommandAuditActivity' -count=10`，预期 PASS、duplicate count=0、hostile stdout/stderr/raw payload corpus 命中=0。

## Task 3: 单主体 query、fixed-watermark API 与 tombstone

**Files:**

- Create: `internal/center/activity/service.go`
- Create: `internal/center/activity/service_test.go`
- Create: `internal/center/activity/export_reader.go`
- Create: `internal/center/activity/export_reader_test.go`
- Create: `internal/center/store/record_activity_postgres_integration_test.go`
- Create: `internal/center/http/handlers/activity.go`
- Create: `internal/center/http/handlers/activity_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`
- Modify: `internal/center/http/router_api_test.go`

- [ ] 写 RED tests 覆盖严格 subject kind、`activity|records|evidence`、source/kind/time/version filters、limit 50/100、auth-first、non-null arrays、confidential cursor generation/as-of 400/409 recovery、响应字段global-head denylist、灾难rebuild后旧cursor过期、deleted subject tombstone、projection 503 和 content lease/fence。两个auth scope共享global projector时，隐藏活动不得改变受限用户的`new_items_available`或暴露可解码/可比较token；只有同query授权新项才变true。
- [ ] 写真实 PostgreSQL RED scenarios：固定 as-of 后跨3页插入live/backfilled rows并推进某record current revision，期望旧分页的current/history成员与顺序完全不变且无漏项/重复；刷新后新revision membership与backfill才按新水位/event time出现。另把匹配的稀疏event kind/evidence/current revision全部放在未过滤排序前101项之后，仍须返回完整page/真实末页。EXPLAIN必须在denormalized subject relation候选阶段、ORDER/LIMIT之前应用subject/auth/as-of/view/source/kind/time与revision-validity predicates，再取≤101 IDs并PK join projection；不能先LIMIT后过滤、固定overfetch、join live current pointer、全量join后sort或在application做per-source limit/union。
- [ ] 写`ActivityExportReader` RED tests：Readiness固定同一generation/published head与逐source vector/digest；ScanRecordPage只返回选定record+actor scope envelopes，重复分页无漏项/重复。head/checkpoint/status/generation/selection/auth任一漂移、裸sequence、source未caught-up均失败；接口与golden compile test由本child冻结，task10无须修改activity文件。
- [ ] 实现 `GET /api/subjects/:type/:id/activity`、service 和 store query；records/evidence 只作为 server predicate，subject snapshot/live route 由 registry 解析，权限/不存在统一 404。
- [ ] 运行 `go test -race ./internal/center/activity ./internal/center/http/handlers -run 'SubjectActivity|ActivityQuery|ActivityCursor' -count=10`，预期 PASS；再以预设 `HOUFENG_DATABASE_URL` 运行 `HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store -run 'RecordActivityPostgres' -count=1 -v`，预期 PASS 且 integration 未 SKIP。

## Task 4: VPS overview aggregator、动态 anomaly 与局部失败

**Files:**

- Create: `internal/center/vpsoverview/types.go`
- Create: `internal/center/vpsoverview/types_test.go`
- Create: `internal/center/vpsoverview/anomalies.go`
- Create: `internal/center/vpsoverview/anomalies_test.go`
- Create: `internal/center/vpsoverview/service.go`
- Create: `internal/center/vpsoverview/service_test.go`
- Create: `internal/center/store/vps_overview.go`
- Create: `internal/center/store/vps_overview_test.go`
- Create: `internal/center/http/handlers/vps_overview.go`
- Create: `internal/center/http/handlers/vps_overview_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`
- Modify: `internal/center/http/router_api_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] 写 RED contract/failure matrix：identity fatal、monitoring/IP/subscription/relation/activity 单区失败与 timeout、uniform generated time、scope-safe freshness/last success、bounded recent 5、facts/status 去重、all slices non-null、capability off、auth/fence 404/503；隐藏auth scope活动推进不能改变当前VPS overview activity freshness/recent/anomaly。
- [ ] 写 anomaly RED table：healthy produces zero rows；monitoring health、IP risk/stale/partial、renewal due/no active subscription、lifecycle blocker、judgement-affecting source unavailable 各有稳定 rule/severity/order/action；恢复后条目消失且不写 record state。
- [ ] 实现 `vpsoverview.Service`、dedicated store reader 和 `GET /api/vps/:id/overview`；每区有独立预算和 safe reason code，recent activity 调同一 activity service，不读取旧五数组 timeline。
- [ ] bootstrap 显式构造 projector worker、activity service 和 overview handler；更新 worker count/readiness tests。运行 `go test -race ./internal/center/vpsoverview ./internal/center/store ./internal/center/http/handlers ./cmd/houfeng-center -run 'VPSOverview|ActivityWorker|Bootstrap' -count=10`，预期 PASS。

## Task 5: Web contract、三种 subject route 与 unified timeline

**Files:**

- Modify: `web/src/lib/types.ts`
- Modify (created by child 2): `web/src/lib/recordsApi.ts`
- Modify (created by child 2): `web/src/lib/recordsApi.test.ts`
- Create: `web/src/components/SubjectIdentityBar.tsx`
- Create: `web/src/components/SubjectIdentityBar.test.tsx`
- Create: `web/src/components/UnifiedTimeline.tsx`
- Create: `web/src/components/UnifiedTimeline.test.tsx`
- Create: `web/src/pages/records/activity/activityQueryState.ts`
- Create: `web/src/pages/records/activity/activityQueryState.test.ts`
- Create: `web/src/pages/records/activity/useSubjectActivity.ts`
- Create: `web/src/pages/records/activity/useSubjectActivity.test.tsx`
- Create: `web/src/pages/records/activity/SubjectLocalNavigation.tsx`
- Create: `web/src/pages/records/activity/SubjectActivityFilters.tsx`
- Create: `web/src/pages/SubjectActivityPage.tsx`
- Create: `web/src/pages/SubjectActivityPage.test.tsx`
- Create: `web/src/pages/SubjectRecordsPage.tsx`
- Create: `web/src/pages/SubjectRecordsPage.test.tsx`
- Create: `web/src/pages/SubjectEvidencePage.tsx`
- Create: `web/src/pages/SubjectEvidencePage.test.tsx`
- Modify: `web/src/app/router.tsx`
- Modify: `web/src/app/router.test.tsx`
- Modify: `web/src/app/layout/Breadcrumb.tsx`
- Modify (created by child 6): `web/src/app/layout/Breadcrumb.test.tsx`
- Modify: `web/src/styles/partials/legacy-assets.css`
- Modify: `web/src/styles/partials/page.css`

- [ ] 写 API/query RED tests固定 exact encoded URL、default omission、多值 OR、view reset cursor、opaque snapshot/next-cursor append/refresh、授权scope内`new_items_available`、invalid recovery、Abort/latest guard、non-null normalization、global head字段denylist和 VPS/monitoring/Target allowlist route parsing；前端不解码、不比较token。
- [ ] 写 component/page RED matrix：loading、subject empty、query no-result、single-source error、lag/append、tombstone、revoke；人工/系统/证据以文字+形状+颜色区分，revision/snapshot route 固定，system row 没有 edit action。
- [ ] 实现 pure query codec、`{state, commands}` controller、三个 lazy pages、identity/timeline components、VPS-local preselected `/records/new` 和 return URL；static subject routes 注册在 detail catch-all 之前。
- [ ] 运行 `NODE_ENV=test npm --prefix web run test -- --run src/lib/recordsApi.test.ts src/components/SubjectIdentityBar.test.tsx src/components/UnifiedTimeline.test.tsx src/pages/records/activity/activityQueryState.test.ts src/pages/records/activity/useSubjectActivity.test.tsx src/pages/SubjectActivityPage.test.tsx src/pages/SubjectRecordsPage.test.tsx src/pages/SubjectEvidencePage.test.tsx src/app/router.test.tsx`，预期 PASS；bundle/import contract仍证明recordsApi未进入AppShell eager chunk。

## Task 6: Canonical VPS route 的 overview composition 与 legacy 回退

**Files:**

- Create: `web/src/pages/vps-detail/LegacyVPSDetail.tsx` (move current `VPSDetailPage` implementation without behavior changes)
- Create: `web/src/pages/vps-detail/LegacyVPSDetail.test.tsx` (move current legacy assertions)
- Create: `web/src/pages/vps-detail/hooks/useVPSOverview.ts`
- Create: `web/src/pages/vps-detail/hooks/useVPSOverview.test.tsx`
- Create: `web/src/pages/vps-detail/hooks/useVPSManagementController.ts`
- Create: `web/src/pages/vps-detail/hooks/useVPSManagementController.test.tsx`
- Create: `web/src/pages/vps-detail/VPSOverviewPageView.tsx`
- Create: `web/src/pages/vps-detail/VPSOverviewPageView.test.tsx`
- Create: `web/src/pages/vps-detail/VPSOverviewIdentityHeader.tsx`
- Create: `web/src/pages/vps-detail/VPSOverviewAnomalies.tsx`
- Create: `web/src/pages/vps-detail/VPSOverviewAnomalies.test.tsx`
- Create: `web/src/pages/vps-detail/VPSOverviewSummaryGrid.tsx`
- Create: `web/src/pages/vps-detail/VPSOverviewRecentActivity.tsx`
- Create: `web/src/pages/vps-detail/VPSOverviewFacts.tsx`
- Create: `web/src/pages/vps-detail/VPSOverviewRelations.tsx`
- Create: `web/src/pages/vps-detail/VPSManagementMenu.tsx`
- Modify: `web/src/pages/VPSDetailPage.tsx`
- Modify: `web/src/pages/VPSDetailPage.test.tsx`
- Modify: `web/src/styles/partials/legacy-vps.css`

- [ ] 先移动 legacy controller并运行其原测试，预期行为保持 GREEN；再为新 route wrapper/overview 写 RED tests，固定 capability off→legacy、on→overview、三个首层动作、local nav、section order 和管理 mutation 只刷新对应 source。
- [ ] 写 healthy/anomaly DOM RED contract：healthy fixture 对 anomaly heading/container/action/disabled placeholder/`动作：无` 的 query count 全为 0；异常插入 identity 后、summary 前；rerender 恢复后节点和保留高度为 0，不抢焦点。
- [ ] 实现 overview composition；把旧 page 的管理 draft/submit state 收敛进 management controller/modal owner，新 page 不 import旧 timeline/experience form，不把写行为放进 `vpsoverview` API。
- [ ] 实现 390px 2×2 summary、recent 最多 3 条、semantic reorder、静止态 entry affordance、44px/focus/reduced-motion；没有新 CSS owner 或 inline style。运行 `NODE_ENV=test npm --prefix web run test -- --run src/pages/VPSDetailPage.test.tsx src/pages/vps-detail/LegacyVPSDetail.test.tsx src/pages/vps-detail/hooks/useVPSOverview.test.tsx src/pages/vps-detail/hooks/useVPSManagementController.test.tsx src/pages/vps-detail/VPSOverviewPageView.test.tsx src/pages/vps-detail/VPSOverviewAnomalies.test.tsx`，预期 PASS。

## Task 7: PostgreSQL 性能、Artifact v1 浏览器矩阵与完整门

**Files:**

- Create: `internal/center/store/record_activity_performance_postgres_integration_test.go`
- Create: `internal/center/http/handlers/vps_overview_postgres_integration_test.go`
- Modify: `web/e2e/fixtures/contracts.ts`
- Modify: `web/e2e/fixtures/profiles.ts`
- Modify: `web/e2e/fixtures/router.ts`
- Modify: `web/e2e/page-states.spec.ts`
- Modify: `web/e2e/visual-contracts.spec.ts`
- Modify: `web/e2e/accessibility.spec.ts`
- Modify: `web/e2e/security.spec.ts`

- [ ] 在真实 PostgreSQL 16 seed 10k records/200k revisions/1m activities；用三个 clean runs 测 subject first 50 和 overview，保存 p50/p95/p99/query count/EXPLAIN。运行 `HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store ./internal/center/http/handlers -run 'RecordActivityPerformance|VPSOverviewPerformance' -count=1 -v`，预期每轮 timeline p95≤1s、overview p95≤750ms且测试不 SKIP。
- [ ] 扩展 overview healthy/anomaly 和 subject timeline 的 desktop/390、loading/empty/no-result/local-error/lag/revoke fixtures；浏览器断言 semantic geometry/overflow，不新增与当前 Web spec 冲突的 tracked pixel baseline。
- [ ] 运行 `npm --prefix web run test:e2e -- --grep "VPS 概览|单主体时间线"`，预期 Artifact v1、Axe、keyboard/focus/44px、document overflow、console/network/CSP 全 PASS。
- [ ] fresh 运行 `go test -race ./internal/center/activity/... ./internal/center/vpsoverview ./internal/center/store ./internal/center/http/handlers -run 'Activity|Overview' -count=10`、`make verify-go`、Node 22 `make verify-web`、`npm --prefix web run test:e2e`、`git diff --check`，预期全部 exit 0。
- [ ] 执行 `trellis-check`、更新 activity/VPS overview 可执行 spec、开 PR 并监控 required CI/post-merge CI；`records_v2_read` 的完整集成默认行为由子任务 11 验证。

## Review and rollback points

- Task 2 review：逐 source 审查 event-time、identity snapshot、auth scope 和禁止字段；发现同 identity/hash 漂移先修 source，禁止让 projector UPDATE 覆盖。
- Task 4 review：用 healthy fixture 人工检查 API `anomalies=[]` 和 DOM absence；任何常驻异常槽都阻断合并。
- Task 6 review：legacy feature-off suite 必须 fresh GREEN；不能用新 API 失败后静默 fallback 模糊真实错误。
- Rollback：关闭 `records_v2_read`、停止 activity worker并恢复当前旧页面 composition；0057 projection 保留，可从权威 sources 幂等补建。不执行 down migration；返回不含 `0057` 的代码版本时重建开发数据库。
