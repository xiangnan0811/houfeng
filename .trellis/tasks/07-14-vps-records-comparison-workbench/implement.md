# 横向比较工作台 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`; Codex inline only. Every production behavior follows RED → verify RED → minimal GREEN → verify GREEN → refactor.

**Goal:** 交付 2–6 个 immutable revision/snapshot 的可比性优先工作台，并把可验证差异和人工结论原子另存为新记录。

**Architecture:** evidence registry 负责 candidate resolution、fixed selection、alignment 和 derived result；comparison intent 接入 records material participant，在同一 revision transaction复制 logical snapshots并写 `comparison.result/v1`；Web controller以 versioned URL state分阶段加载 review/detail。

**Tech Stack:** Go 1.26.2、pgx/v5、PostgreSQL、stdlib HMAC/SHA-256、React 19、TypeScript 6、React Router 7、SVG、Vitest/Testing Library、Playwright/axe、纯 CSS owner manifest。

---

## 2026-08-02 execution override

- 从 Child 2/4/5/7 已接受的 protected main 开始。
- 不创建 migration；不做 old-database/legacy/staging/release work。
- 保留 bounded comparison memory/admission、安全 intent 和 saved-result
  renderer/exporter，因为它们直接保护当前功能正确性。

## Preconditions

- [ ] 直接依赖子任务 2、4、5、7 已合入 `main` 且 post-merge CI 通过；确认 record material participant、evidence copy lineage、renderer/exporter、record center/revision/subject evidence pages 的实际合同未漂移。
- [ ] 从最新受保护主线创建非 `main` 分支，运行 `sh scripts/setup-git-hooks.sh`，再运行 `trellis-before-dev` 读取 backend/web/cross-layer 规范。
- [ ] 确认本任务不创建 migration；记录 task 4 kind conformance、task 5 revision save 和现有 `/monitoring/compare` baseline。运行 `make verify-go`、`make verify-web` 与 `NODE_ENV=test npm --prefix web run test -- --run src/pages/MonitoringComparePage.test.tsx`，预期全部 GREEN。

## Task 1: Comparison domain、compatibility descriptor 与 reason taxonomy

**Files:**

- Create: `internal/center/evidence/comparison.go`
- Create: `internal/center/evidence/comparison_test.go`
- Create: `internal/center/evidence/comparison_alignment.go`
- Create: `internal/center/evidence/comparison_alignment_test.go`
- Create: `internal/center/evidence/comparison_result.go`
- Create: `internal/center/evidence/comparison_result_test.go`
- Modify (created by child 4): `internal/center/evidence/types.go`
- Modify (created by child 4): `internal/center/evidence/registry.go`
- Modify (created by child 4): `internal/center/evidence/conformance.go`

- [ ] 写 RED conformance table：2–6、baseline、actual/common overlap、schema pair、semantic metric、unit conversion、bucket compatibility、empty intersection、partial/truncated/gap/maintenance/source status、registered-readable但Compare-incompatible schema、权威unregistered schema fail-closed、0-vs-missing、UTC/canonical-ordinal/hash tie-break、decoder/map iteration permutation和deterministic digest；ordered item permutation单独断言会改变显式条件digest。
- [ ] 扩展 kind Compare descriptor，只允许注册表声明的 schema/unit/metric/reaggregation；实现actual模式单调一对一nearest匹配、UTC epoch common grid、完整edge bucket规则和完整reason enum/condition/result DTO/gap-aware `[][]Point`。generic orchestration 不读取 payload map key或 Markdown，tolerance不能扩张交集或跨gap。
- [ ] 为首批 task 4 kinds 增加 compatibility fixtures；cross-kind、lossy conversion、upsample/interpolation、raw JSON/stdout/stderr 全部拒绝。
- [ ] 运行 `go test -race ./internal/center/evidence -run 'Comparison|Comparability|Alignment|CompareConformance' -count=10`，预期 PASS，gap crossing/zero fill/extrapolation/cross-kind score 计数均为 0。

## Task 2: Subject candidates、batched resolver 与权限安全 API

**Files:**

- Create: `internal/center/evidence/comparison_candidates.go`
- Create: `internal/center/evidence/comparison_candidates_test.go`
- Modify (created by child 4): `internal/center/store/evidence.go`
- Create: `internal/center/store/evidence_comparison_postgres_integration_test.go`
- Create: `internal/center/http/handlers/evidence_comparisons.go`
- Create: `internal/center/http/handlers/evidence_comparisons_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`
- Modify: `internal/center/http/router_api_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] 写 candidate RED matrix：2–6 registry subjects、absolute window、kind filter、deterministic ranking/tie、no evidence、deleted/tombstoned、mixed auth、reservation/fence、batched query count、non-null arrays和 body 256 KiB。
- [ ] 实现批量 revision/snapshot/subject resolver 与 `POST /api/evidence/comparison-candidates`；只返回 authorized fixed IDs/schema/hash/window/quality/reason，不 capture、不创建 intent、不泄露 denied subject/count。
- [ ] 写 handler/router/bootstrap tests固定 strict JSON/method/400/404/409/413/503、`Cache-Control: private, no-store`、content lease before payload read 和 capability off。
- [ ] 运行 `go test -race ./internal/center/evidence ./internal/center/store ./internal/center/http/handlers ./cmd/houfeng-center -run 'ComparisonCandidate|EvidenceComparison|Bootstrap' -count=10`，预期 PASS。使用已设置 `HOUFENG_DATABASE_URL` 运行 `HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store -run 'EvidenceComparisonCandidatePostgres' -count=1 -v`，预期 PASS 且测试未 SKIP。

## Task 3: Fixed comparison、summary/detail staging 与 signed intent

**Files:**

- Create: `internal/center/evidence/comparison_service.go`
- Create: `internal/center/evidence/comparison_service_test.go`
- Create: `internal/center/evidence/comparison_intent.go`
- Create: `internal/center/evidence/comparison_intent_test.go`
- Create: `internal/center/evidence/comparison_intent_signer.go`
- Create: `internal/center/evidence/comparison_intent_signer_test.go`
- Create: `internal/center/evidence/comparison_admission.go`
- Create: `internal/center/evidence/comparison_admission_test.go`
- Modify (created by child 4): `internal/center/store/evidence.go`
- Modify: `internal/center/http/handlers/evidence_comparisons.go`
- Modify: `internal/center/http/handlers/evidence_comparisons_test.go`
- Modify: `internal/center/config/config.go`
- Modify: `internal/center/config/config_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`
- Modify: `.env.example`
- Modify: `Dockerfile`
- Modify: `compose.yaml`
- Modify: `docs/deploy/compose.env.example`
- Modify: `docs/deploy/local-and-systemd.md`
- Modify: `internal/center/deploy/docker_static_test.go`

- [ ] 写 fixed-selection RED matrix：snapshot XOR revision、revision ownership、revision-bound immutable type/status/status-group/impact/occurred-at、snapshot-only `revision_context=not_applicable`/null metadata、多record refs也不回填、chosen same-kind snapshot、metadata-only、current root/revision advance、hash mismatch、baseline、summary first、one kind/metric detail、source tombstone/unavailable、snapshot unreadable、save eligibility/intent absence、context cancel；另写weighted admission RED matrix覆盖8MiB estimate、512MiB budget、5个96MiB请求、16 queue/2s timeout、429+Retry-After、actual overrun、disconnect与5s join-before-release、cgroup/config/readiness失败。
- [ ] 实现 `POST /api/evidence/comparisons`；无 detail只返回 normalized selection/comparability/available kinds，有 detail先取得`ComparisonAdmission` token再只解码一个 kind/metric并返回≤2,000 buckets/series。超 response bound返回 `comparison_result_too_large`，actual working-set越权返回`comparison_request_memory_limit`，capacity饱和返回`comparison_capacity_exhausted`；均不截断、不签intent，token只在全部worker/writer停止后释放。
- [ ] 先写signer/config RED tests：regular file、0400 owner/mode、dirfd+`O_NOFOLLOW`、symlink/directory/device/hard-link/inode-swap拒绝、purpose/key ID、15m TTL/2m skew、restart、two-replica verify-set、两阶段rotation、old key expiry、unknown/revoked key、purpose混用、member digest drift，以及禁止复用deletion/backup key。
- [ ] 实现独立 `ComparisonIntentSigner` keyring与bootstrap/readiness；key material不入DB/log，capability on时任一member缺完整verify set即不接流量。
- [ ] 固定容器runtime UID/GID并增加真实部署RED/static tests：Compose只挂载host 0400 keyring到`/run/secrets/houfeng-comparison-intent-keyring.json:ro`，路径变量不含key material，host文件owner与non-root `houfeng` UID一致；以最终image的`USER houfeng`证明可读、不可写，root-owned/0444/缺失/错误mount均让comparison readiness失败。key绝不COPY进image/layer，systemd使用同一owner/mode和两阶段原子替换/滚动流程。
- [ ] 实现 `save_eligibility`：metadata-only/incompatible/no-numeric仍可eligible；任一unreadable/hash/copy/auth/fence blocker时不签发intent。eligible时才用signer生成versioned HMAC intent并绑定purpose/key ID/actor/project/IDs/hashes/conditions/registry+calculation versions/warnings/auth+fence heads/expiry；token不含 payload/Markdown/identity text。
- [ ] 运行 `go test -race ./internal/center/evidence ./internal/center/http/handlers -run 'FixedComparison|ComparisonIntent|ComparisonSummary|ComparisonDetail' -count=10`，预期 PASS；同 fixed request digest稳定，source/current revision变化不使 immutable selection漂移。

## Task 4: `comparison.result/v1` 与 records 原子 save-as-record

**Files:**

- Create: `internal/center/evidence/comparison_participant.go`
- Create: `internal/center/evidence/comparison_participant_test.go`
- Create: `internal/center/evidence/comparison_result_kind.go`
- Create: `internal/center/evidence/comparison_result_kind_test.go`
- Modify (created by child 2): `internal/center/records/types.go`
- Modify (created by child 2): `internal/center/records/service.go`
- Modify (created by child 2): `internal/center/records/service_test.go`
- Modify (created by child 4): `internal/center/store/evidence.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] 写 transaction RED cutpoints：伪造blocked/unreadable intent、expired/stale intent、auth/fence drift、copy 1..N failure、quota、derived payload、revision participant、DB commit、Idempotency-Key retry。断言不能跳过任一selection，record/revision/logical copies/result/ref/activity/outbox全变或全不变。
- [ ] 注册 `comparison.result/v1` descriptor/renderer/Summarize/Export；canonical payload固定 original→copied mapping、每个revision-bound item的type/status/status-group/impact/occurred-at metadata snapshot、每个snapshot-only item的`revision_context=not_applicable`、baseline、windows/conditions、schema/registry/calculation version、quality/warnings/system differences，永久排除 human conclusion。
- [ ] 实现 `ComparisonRevisionParticipant`：正式保存时重算 intent、创建独立 logical copies with `copied_from_snapshot_id`、复用 payload bytes、写 result snapshot/ordered refs；source record/revision/snapshot update/delete count必须为0。
- [ ] 运行 `go test -race ./internal/center/evidence ./internal/center/records ./internal/center/store -run 'ComparisonParticipant|ComparisonResult|SaveComparisonRecord' -count=10`，预期 PASS；再运行task4 exporter roundtrip tests，预期renderer/export material同义、registered incompatible保留reason、unregistered version失败关闭。

## Task 5: Web API、versioned URL codec 与 workbench controller

**Files:**

- Modify: `web/src/lib/types.ts`
- Modify (created by child 2): `web/src/lib/recordsApi.ts`
- Modify (created by child 2): `web/src/lib/recordsApi.test.ts`
- Create: `web/src/pages/records/compare/comparisonQueryState.ts`
- Create: `web/src/pages/records/compare/comparisonQueryState.test.ts`
- Create: `web/src/pages/records/compare/useComparisonWorkbench.ts`
- Create: `web/src/pages/records/compare/useComparisonWorkbench.test.tsx`

- [ ] 写 API RED tests固定 candidate/fixed summary/detail/save URLs、strict body、Idempotency-Key、stable error/recovery、AbortSignal、no-store semantics 和 response allowlist。
- [ ] 写 `comparison-url/v1` property/table RED tests：ordered 2–6 items、snapshot/revision XOR、chosen IDs、UTC/default omission、baseline/alignment/window/tolerance/bucket/kind/metric、canonical base64url roundtrip、duplicate/unknown/oversize/corrupt rejection和 no payload/secret。
- [ ] 写 controller fake-timer/Abort RED tests：candidate→confirm fixed、condition replace URL、summary before detail、latest request wins、cancel、old result stale shell、back navigation、intent expiry/save conflict/revoke local cleanup。
- [ ] 实现 typed façade、pure codec和 `{state, commands}` controller；页面/children不直接 fetch、不持久化 result cache。运行 `NODE_ENV=test npm --prefix web run test -- --run src/lib/recordsApi.test.ts src/pages/records/compare/comparisonQueryState.test.ts src/pages/records/compare/useComparisonWorkbench.test.tsx`，预期 PASS；bundle/import contract仍证明recordsApi未进入AppShell eager chunk。

## Task 6: `/records/compare` UI、entry integration 与 390px contract

**Files:**

- Create: `web/src/pages/RecordComparisonPage.tsx`
- Create: `web/src/pages/RecordComparisonPage.test.tsx`
- Create: `web/src/pages/records/compare/ComparisonSelectionBasket.tsx`
- Create: `web/src/pages/records/compare/ComparisonSelectionBasket.test.tsx`
- Create: `web/src/pages/records/compare/ComparisonConditions.tsx`
- Create: `web/src/pages/records/compare/ComparabilityReview.tsx`
- Create: `web/src/pages/records/compare/ComparabilityReview.test.tsx`
- Create: `web/src/pages/records/compare/ComparisonKindPanel.tsx`
- Create: `web/src/pages/records/compare/ComparisonTrendChart.tsx`
- Create: `web/src/pages/records/compare/ComparisonTrendChart.test.tsx`
- Create: `web/src/pages/records/compare/ComparisonMatrix.tsx`
- Create: `web/src/pages/records/compare/ComparisonMatrix.test.tsx`
- Create: `web/src/pages/records/compare/ComparisonSaveRecord.tsx`
- Create: `web/src/pages/records/compare/ComparisonSaveRecord.test.tsx`
- Modify (created by child 6): `web/src/pages/RecordsPage.tsx`
- Modify (created by child 6): `web/src/pages/RecordsPage.test.tsx`
- Modify (created by child 5): `web/src/pages/RecordRevisionPage.tsx`
- Modify (created by child 5): `web/src/pages/RecordRevisionPage.test.tsx`
- Modify (created by child 7): `web/src/pages/SubjectEvidencePage.tsx`
- Modify (created by child 7): `web/src/pages/SubjectEvidencePage.test.tsx`
- Modify: `web/src/app/router.tsx`
- Modify: `web/src/app/router.test.tsx`
- Modify: `web/src/app/layout/Breadcrumb.tsx`
- Modify (created by child 6, extended by child 7): `web/src/app/layout/Breadcrumb.test.tsx`
- Modify: `web/src/styles/partials/legacy-assets.css`
- Modify: `web/src/styles/partials/page.css`

- [ ] 写 page/component RED state matrix：少于2项、candidate empty、metadata-only、no compatible kind、partial/truncated/stale/tombstoned/unreadable、summary/detail loading/cancel、save eligibility/blocker/success/conflict/error、revoke。comparability heading在任何 metric/chart DOM之前；unreadable blocker时保存动作不在DOM。
- [ ] 写 chart/matrix RED contracts：每 gap一个独立 polyline、0 point保留、missing无 point、6 labels/markers、accessible summary/data、不可计算 cell有reason；named scroll region具备 visible heading/hint/role/name/tabIndex/sticky row且document不滚动。
- [ ] 实现 selection→conditions→review→kind/detail→system differences→human conclusion/save顺序；390px一次一个kind/metric，save位于 conclusion后，状态用文字+形状+颜色，controls≥44px。
- [ ] 在 record center、fixed revision和subject evidence页加入纯 URL builder selection入口；不复制 compare算法。注册 static `/records/compare` 在 dynamic `/records/:recordId` 前；保持 `/monitoring/compare` route/tests不变。
- [ ] 运行 `NODE_ENV=test npm --prefix web run test -- --run src/pages/RecordComparisonPage.test.tsx src/pages/records/compare/ComparisonSelectionBasket.test.tsx src/pages/records/compare/ComparabilityReview.test.tsx src/pages/records/compare/ComparisonTrendChart.test.tsx src/pages/records/compare/ComparisonMatrix.test.tsx src/pages/records/compare/ComparisonSaveRecord.test.tsx src/pages/RecordsPage.test.tsx src/pages/RecordRevisionPage.test.tsx src/pages/SubjectEvidencePage.test.tsx src/app/router.test.tsx`，预期 PASS。

## Task 7: Scale、Artifact v1 浏览器矩阵与完整门

**Files:**

- Create: `internal/center/evidence/comparison_benchmark_test.go`
- Create: `internal/center/store/evidence_comparison_performance_postgres_integration_test.go`
- Create: `test/integration/comparison/compose.yaml`
- Create: `scripts/run-comparison-capacity.sh`
- Modify: `web/e2e/fixtures/contracts.ts`
- Modify: `web/e2e/fixtures/profiles.ts`
- Modify: `web/e2e/fixtures/router.ts`
- Modify: `web/e2e/page-states.spec.ts`
- Modify: `web/e2e/visual-contracts.spec.ts`
- Modify: `web/e2e/accessibility.spec.ts`
- Modify: `web/e2e/security.spec.ts`

- [ ] 用 fixed immutable fixtures写150-sample performance test和benchmark，覆盖6×2,000 aligned buckets、partial/gap/common overlap和cancel；运行 `go test ./internal/center/evidence -run 'ComparisonDetailPerformance' -count=1 -v`，预期输出p50/p95/p99且p95≤2s、encoded response≤2 MiB。另运行 `go test ./internal/center/evidence -run '^$' -bench 'ComparisonDetail' -benchmem -count=3`只记录alloc count/bytes；peak memory每次用fresh隔离container、同一外部seed DB、GC后测idle、只发一个最大请求并读取cgroup v2 `memory.peak`，3个新container的peak-idle均≤96 MiB，禁止用`-benchmem`冒充peak证明。
- [ ] 实现可执行outer harness `scripts/run-comparison-capacity.sh`：构建当前commit disposable test image，启动外部seed PostgreSQL，并为每次single/aggregate run创建全新`--memory=4g --cpus=4`容器；容器内先GC/测idle并清/读取cgroup v2 `memory.peak/events`，宿主driver发请求且验证退出后容器/DB fixture清理。禁止要求operator预先“已经在fresh container”再手跑`go test`。
- [ ] 运行 `./scripts/run-comparison-capacity.sh --profile single --runs 3`，每个fresh container一个最大请求且peak-idle≤96MiB；再运行`--profile aggregate --runs 3`，512MiB budget下同时保持5个最大请求并发，再发第6–21个请求，active weight始终≤512MiB，未准入请求不读取payload并在queue满或2s内429，取消全部后5s内goroutine/writer/token归零。aggregate profile还并发task11同规格overview/search/timeline/draft/revision/evidence流量，记录cgroup peak/events、admission wait/reject/drain且无OOM/throttle。
- [ ] 以真实 PostgreSQL relation/evidence seed运行 `HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store -run 'EvidenceComparisonPerformance' -count=1 -v`，预期 candidate/summary p95≤1s、query count有界、测试不 SKIP并输出代表性 EXPLAIN。
- [ ] 扩展 desktop/390 的 selection、metadata-only、incompatible、partial、processing/cancel、save/revoke fixtures；Artifact v1 视觉基线定义为 Playwright语义/几何/overflow/Axe/focus合同与短期人工评审证据，不创建 tracked pixel golden、screenshot manifest 或 bulk raster。
- [ ] 运行 `npm --prefix web run test:e2e -- --grep "横向比较工作台"`，预期 Axe critical/serious=0、keyboard/focus/44px、named matrix scroll、document overflow、console/network/CSP全部 PASS。
- [ ] fresh 运行 `go test -race ./internal/center/evidence ./internal/center/records ./internal/center/store ./internal/center/http/handlers -run 'Comparison|Comparability|Alignment' -count=10`、`make verify-go`、Node 22 `make verify-web`、`npm --prefix web run test:e2e`、`git diff --check`，预期全部 exit 0。
- [ ] 执行 `trellis-check`、更新 evidence comparison/record save/Web state可执行 spec、开 PR并监控required CI/post-merge CI；comparison capability仍默认关闭，最终全链路切换留给子任务11。

## Review and rollback points

- Task 1 review：逐 kind 签署 schema/unit/metric/reaggregation矩阵；任何 generic JSON/Markdown extraction、silent conversion或cross-kind score阻断合并。
- Task 4 review：用 transaction cutpoint证明 source mutation=0、半份 record/copy/result=0，并确认 human conclusion不进入 derived evidence。
- Task 6 review：comparability review DOM必须早于 chart，390px只有局部 named scroll；旧 `/monitoring/compare` suite fresh GREEN。
- Rollback：关闭 comparison capability/route和停止相关请求即可；没有 migration/down migration。已保存 record、copied snapshots与`comparison.result/v1`继续可读/可导出，renderer/exporter版本不得删除；禁止回退到 mutable live comparison或任意JSON。
