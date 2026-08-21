# 横向比较工作台 Implementation Plan

> **For agentic workers:** Use `trellis-before-dev` before product edits. Every production behavior follows RED → verify RED → minimal GREEN → verify GREEN. Do not execute the 2026-08-02 task list.

**Goal:** 交付 2–6 个 immutable revision/snapshot 的可比性优先工作台，并把可验证差异和人工结论原子另存为新记录。

**Architecture:** evidence 包编排 candidate / fixed compare；非时序 kind 复用 pairwise `Kind.Compare`；host/probe 才做 `actual_coverage` series。HMAC intent + copy-lineage 写路径 + `comparison.result/v1` 走 records 事务。Web 用 versioned URL state。capability 默认关。4 GiB harness 不在本计划。

**Tech Stack:** Go、pgx/v5、PostgreSQL、stdlib HMAC/SHA-256、React 19、TypeScript、React Router 7、SVG、Vitest、Playwright/axe、纯 CSS。

---

## 2026-08-20 approved scope A

- 从 `origin/main` `ffda9a07` / `v0.70.0` 的非 `main` 分支/worktree 开始。
- 无 migration。不改 `/monitoring/compare`。
- 不写 overview 管理面板，不扩 activity group digest。
- 对照：`research/current-main-reconciliation-2026-08-20.md`。

## Preconditions

- [x] Child 2/4/5/7 已在 protected main（`#422` → `#420` → `v0.70.0`）。
- [x] `sh scripts/setup-git-hooks.sh`；`trellis-before-dev` 读 backend/web spec。
- [x] 基线 GREEN：`make verify-go`、`make verify-web`、`NODE_ENV=test npm --prefix web run test -- --run src/pages/MonitoringComparePage.test.tsx`。

## Task 1: Domain、reason、host/probe series

**Files:**

- Create: `internal/center/evidence/comparison.go`
- Create: `internal/center/evidence/comparison_test.go`
- Create: `internal/center/evidence/comparison_alignment.go`
- Create: `internal/center/evidence/comparison_alignment_test.go`
- Modify: `internal/center/evidence/types.go`（reason / alignment 常量；不要改成 registry-wide Compare descriptor）

- [x] RED：2–6、baseline、revision vs snapshot-only metadata、`impact_level`、reason enum、pairwise Compare 包装、host/probe actual_coverage 匹配、`common_overlap_unsupported`、0-vs-missing、iteration permutation、digest。
- [x] GREEN：编排调用现有 `Kind.Compare`；只给 host/probe 实现 series；generic 路径不读 payload map / Markdown。
- [x] `go test -race ./internal/center/evidence -run 'Comparison|Comparability|Alignment' -count=10`

## Task 2: Candidates API

**Files:**

- Create: `internal/center/evidence/comparison_candidates.go`
- Create: `internal/center/evidence/comparison_candidates_test.go`
- Modify: `internal/center/store/evidence.go`（或并列 `evidence_comparison.go`）
- Create: `internal/center/store/evidence_comparison_postgres_integration_test.go`
- Create: `internal/center/http/handlers/evidence_comparisons.go`
- Create: `internal/center/http/handlers/evidence_comparisons_test.go`
- Modify: `internal/center/http/router.go`、`router_test.go`、`router_api_test.go`
- Modify: `internal/center/config/config.go`、`config_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`、`bootstrap_test.go`
- Modify: `.env.example`（`HOUFENG_COMPARISON_ENABLED` 默认 false）

- [x] RED：2–6 subject、窗口、kind filter、404 不泄露计数、capability off、body 256 KiB、batched query。
- [x] GREEN：`POST /api/evidence/comparison-candidates`；不 capture、不签发 intent。
- [x] `go test -race ./internal/center/evidence ./internal/center/http/handlers ./cmd/houfeng-center -run 'ComparisonCandidate|EvidenceComparison|Bootstrap' -count=10`
- [x] `HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store -run 'EvidenceComparisonCandidatePostgres' -count=1 -v`（有 DATABASE_URL 时不得 SKIP）

## Task 3: Fixed compare、admission、HMAC intent

**Files:**

- Create: `internal/center/evidence/comparison_service.go`
- Create: `internal/center/evidence/comparison_service_test.go`
- Create: `internal/center/evidence/comparison_intent.go`
- Create: `internal/center/evidence/comparison_intent_test.go`
- Create: `internal/center/evidence/comparison_intent_signer.go`
- Create: `internal/center/evidence/comparison_intent_signer_test.go`
- Create: `internal/center/evidence/comparison_admission.go`
- Create: `internal/center/evidence/comparison_admission_test.go`
- Modify: handlers / bootstrap / config / `.env.example` / `docs/deploy/local-and-systemd.md`

- [x] RED：snapshot XOR revision、`impact_level`、`revision_context=not_applicable`、summary-before-detail、host/probe series bound、common_overlap blocked、unreadable 不签 intent、进程内 admission 429/422/cancel drain。
- [x] GREEN：`POST /api/evidence/comparisons`。进程内 weighted semaphore，不要求 cgroup v2 readiness。
- [x] RED/GREEN HMAC：0400、`O_NOFOLLOW`、symlink/device/hard-link 拒绝、15m TTL、purpose/key ID、禁止复用 deletion/backup key。Compose/image 只挂载 keyring 路径，不 COPY key。不写 4 GiB disposable harness。
- [x] `go test -race ./internal/center/evidence ./internal/center/http/handlers -run 'FixedComparison|ComparisonIntent|ComparisonSummary|ComparisonDetail|ComparisonAdmission' -count=10`

## Task 4: `comparison.result/v1` 与另存

**Files:**

- Create: `internal/center/evidence/comparison_result_kind.go`
- Create: `internal/center/evidence/comparison_result_kind_test.go`
- Create: `internal/center/evidence/comparison_participant.go`
- Create: `internal/center/evidence/comparison_participant_test.go`
- Modify: `internal/center/store/evidence.go` / `record_evidence_participant.go`（copy-lineage 写路径）
- Modify: `internal/center/http/handlers/records.go`、`records` application/service（intent 字段）
- Modify: `cmd/houfeng-center/bootstrap.go`（注册 `Name()=="comparison"` participant）

- [x] RED cutpoints：伪造/过期/stale intent、copy 失败、quota、idempotent retry、source mutation=0。
- [x] GREEN：注册 `comparison.result/v1`；logical copy + `copied_from_snapshot_id`；result 不含 human conclusion。
- [x] `go test -race ./internal/center/evidence ./internal/center/records ./internal/center/store -run 'ComparisonParticipant|ComparisonResult|SaveComparisonRecord' -count=10`
- [x] 现有 kind Export/conformance 仍 GREEN。

## Task 5: Web façade、URL codec、controller

**Files:**

- Modify: `web/src/lib/types.ts`、`web/src/lib/recordsApi.ts`、`web/src/lib/recordsApi.test.ts`
- Create: `web/src/pages/records/compare/comparisonQueryState.ts`
- Create: `web/src/pages/records/compare/comparisonQueryState.test.ts`
- Create: `web/src/pages/records/compare/useComparisonWorkbench.ts`
- Create: `web/src/pages/records/compare/useComparisonWorkbench.test.tsx`

- [x] RED：candidate/compare/save wire、AbortSignal、`comparison-url/v1` roundtrip、无 payload/secret。
- [x] GREEN：lazy-only `recordsApi`；bundle contract 仍证明不进 AppShell。
- [x] `NODE_ENV=test npm --prefix web run test -- --run src/lib/recordsApi.test.ts src/pages/records/compare/comparisonQueryState.test.ts src/pages/records/compare/useComparisonWorkbench.test.tsx`

## Task 6: `/records/compare` UI 与入口

**Files:**

- Create: `web/src/pages/RecordComparisonPage.tsx` + test
- Create: `web/src/pages/records/compare/ComparisonSelectionBasket.tsx` + test
- Create: `web/src/pages/records/compare/ComparisonConditions.tsx`
- Create: `web/src/pages/records/compare/ComparabilityReview.tsx` + test
- Create: `web/src/pages/records/compare/ComparisonKindPanel.tsx`
- Create: `web/src/pages/records/compare/ComparisonTrendChart.tsx` + test
- Create: `web/src/pages/records/compare/ComparisonMatrix.tsx` + test
- Create: `web/src/pages/records/compare/ComparisonSaveRecord.tsx` + test
- Modify: `web/src/pages/records/RecordSearchPage.tsx` + test
- Modify: `web/src/pages/records/RecordRevisionPage.tsx` + test
- Modify: `web/src/pages/SubjectEvidencePage.tsx` + `web/src/pages/records/SubjectActivityWorkspace.tsx`（若入口必须挂在 workspace）+ tests
- Modify: `web/src/app/router.tsx`、`router.test.tsx`、`Breadcrumb.tsx` + test
- Modify: `web/src/styles/partials/page.css`（及现有 records CSS owner，避免误改 `legacy-assets.css` 除非必要）

- [x] RED 状态矩阵：少于 2 项、candidate empty、metadata-only、无兼容 kind、unreadable 无保存按钮、review heading 在 chart 前。
- [x] host/probe：每 gap 一条 polyline。其他 kind：无假 series。
- [x] 三个入口只建 URL；`/records/compare` 在 `/records/:recordId` 前；`MonitoringComparePage` tests 保持 GREEN。
- [x] `NODE_ENV=test npm --prefix web run test -- --run src/pages/RecordComparisonPage.test.tsx src/pages/records/compare/ src/pages/records/RecordSearchPage.test.tsx src/pages/records/RecordRevisionPage.test.tsx src/pages/SubjectEvidencePage.test.tsx src/app/router.test.tsx`

## Task 7: Focused gates，不含 4 GiB harness

**Files:**

- Create: `internal/center/evidence/comparison_benchmark_test.go`（记录 p50/p95/allocs；不用 `-benchmem` 冒充 cgroup peak）
- Create: `internal/center/store/evidence_comparison_performance_postgres_integration_test.go`（query 有界 + EXPLAIN）
- Modify: `web/e2e/fixtures/*`、`page-states.spec.ts`、`visual-contracts.spec.ts`、`accessibility.spec.ts`、`security.spec.ts`

不要创建 `scripts/run-comparison-capacity.sh` 或 `test/integration/comparison/compose.yaml`。

- [x] Go detail performance test 记录 p95 与 response bytes；作为回归信号，不把 96 MiB cgroup peak 当退出门。
- [x] Playwright Artifact v1：desktop/390 选择、metadata-only、incompatible、partial、cancel、save/revoke。Axe critical/serious = 0。
- [x] `npm --prefix web run test:e2e -- --grep "横向比较工作台"`
- [x] `go test -race ./internal/center/evidence ./internal/center/records ./internal/center/store ./internal/center/http/handlers -run 'Comparison|Comparability|Alignment' -count=10`
- [x] `make verify-go`、Node 22 `make verify-web`、`npm --prefix web run test:e2e`、`git diff --check`
- [x] `trellis-check`；更新 `.trellis/spec/backend/evidence-snapshot-contract.md` 与 web state spec；capability 默认关。

## Review and rollback

- Task 1：禁止 generic JSON/Markdown 抽指标或 cross-kind 总分。
- Task 4：source mutation=0；human conclusion 不进 derived evidence。
- Task 6：review DOM 早于 chart；`/monitoring/compare` GREEN。
- Rollback：关 `HOUFENG_COMPARISON_ENABLED`。无 down migration。已保存 `comparison.result/v1` 必须仍能被 registry 读取。
