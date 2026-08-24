# VPS 详情页重构审查修复实施计划

> **执行纪律：** 每个 child 在开始前运行 `trellis-before-dev`，按 RED → 记录预期失败 →
> 最小 GREEN → focused check → 独立 `trellis-check`。parent 不直接承载产品实现。

## Phase 0: Final approval and start

- [x] 用户在收到本版 PRD/design/implementation summary 后明确批准实施。
- [x] `task.py validate` 通过 parent 与四个 child；context manifests 无空项或未决标记。
- [x] `task.py start` parent，确认 branch/worktree metadata 与 hooks。
- [x] 保留已记录 baseline：Go 3 packages GREEN，Web 7 files / 74 tests GREEN。

## Phase 1: Child A — source isolation and freshness

**Task:** `08-24-vps-overview-freshness-reliability`

- [x] 分派 `trellis-implement`；按 child plan 先写 slow-source、failure matrix、renewal timestamp、
  relation section 与 local UI retry RED，并保存失败证据。
- [x] 实现 identity-first bounded concurrency、truthful SectionState、Go/TS DTO 与局部 UI。
- [x] 跑 focused Go/Web、race/Node 22/browser checks；分派 `trellis-check` 修至无 finding。
- [x] 冻结 relation DTO 与 fixture，作为 action/gate child 输入；更新 parent evidence。

## Phase 2: Child B — action/navigation contract and dependency patch

**Task:** `08-24-vps-overview-action-contract`

- [x] 在 freshness DTO 上分派 `trellis-implement`；写每个 anomaly/relation destination、command
  callback、unsafe route 与 dependency audit RED。
- [x] 实现 backend owner mapping、optional relation route、Web closed destination resolver、
  management/retry/relation-panel callbacks；升级 React Router 7.18.2。
- [x] 跑 focused Go/Web、router/audit/build/bundle/Chromium；分派 `trellis-check` 修至无 finding。
- [x] 冻结最终 action/relation wire shape，交给 gate child。

## Phase 3: Child C — validating decoder and explicit gate

**Task:** `08-24-vps-overview-gate-contract`

- [x] 在最终 DTO 上分派 `trellis-implement`；写 malformed/invalid/network/503/retry RED。
- [x] 实现 local typed decoder、allowlisted projection、显式 fallback/error state 与 retry。
- [x] 跑 focused/full Node 22、production invalid-200 E2E；分派 `trellis-check` 修至无 finding。

## Phase 4: Child D — S3 runner lifecycle

**Task:** `08-24-records-s3-harness-lifecycle`

- [x] 可与 Phase 1-3 的非冲突工作并行分派 `trellis-implement`；先以 fake toolchain 得到 cleanup
  false-green RED，不直接重跑旧的泄漏 S3 gate。
- [x] 实现 shared lifecycle、labeled named volume、direct-child teardown 与 exact no-leak wrapper。
- [x] 跑 fault matrix、local/S3 integration/recovery、security scan、Go gate；分派
  `trellis-check` 修至无 finding。

## Phase 5: Cross-child integration verification

- [x] 对最终 Go/TS wire 做一次表驱动 backend → handler JSON → decoder → UI closure test。
- [x] Go 1.26.2 format、vet、focused/race/full `make verify-go`，确认零 skip/环境替代。
- [x] Node 22 lint、coverage、production build、bundle/CSS budgets、`make verify-web`。
- [x] 完整 Chromium E2E；production preview 复核动作、invalid DTO、局部失败、1440/390、五类
  write management panels、三类 read relation panels、keyboard/focus/Axe。
- [x] 完整 Records browser/security/capacity 与 local+S3 integration/recovery；按 unique labels 和
  test-owned TMPDIR 断言零残留。
- [x] 复核 permissions/non-leakage/archive/restore/permanent-delete fail-closed 与三项延期。
- [x] `git diff --check`、依赖 production audit、全 diff scope review。

## Phase 6: Independent closure review

- [x] 分派新的 `trellis-check`，不复用 implementer 自证；按 Critical/Important/Minor 报告。
- [x] 原 I-01..I-04/M-01 每项记录绝对 file:line、RED、GREEN、集成/browser/cleanup 证据。
- [x] 任何 finding 回到 owning child 同 branch 修复并重跑受影响及 parent gates，直至清零。
- [x] 必要时运行 `trellis-update-spec` 固化 overview decoder/freshness 与 harness lifecycle 合同。

### Final local verification snapshot

- Go 1.26.2：隔离副本完整 `make GO=go verify-go` 通过；最终相关八包 `-race` 通过。
- Node 22：`make verify-web` 通过，196 files / 1359 tests；Chromium 127/127。
- PostgreSQL 16.14：最终无缩放百万行 store/HTTP 各三轮均为 21 queries / 0 errors / 0 skips；
  store p95 558ms、HTTP p95 701ms，预算 750ms。bulk seed 后显式完成 checkpoint，且 warmup 必须
  先通过完整 healthy DTO 与 21/0 合同，避免迟到 warmup 查询污染测量。
- Records：browser/security/capacity/local 与 local/S3 integration/recovery 均通过；本次标签资源、
  volume、workspace/TMPDIR 为零残留，历史 residue 未变。
- Web production audit 为 0；默认 audit 的 5 个 high 均在未改动的 dev-only dependency chain，
  不属于本任务 AC-02 的 production dependency finding。
- 最终独立综合复审：Critical 0 / Important 0 / Minor 0。

## Phase 7: Protected delivery and final reconciliation

- [ ] 提交 feature branch、push、开 PR；等待 required CI，失败在同 branch 修复。
- [ ] required CI 全绿后合入 protected main；不直接修改 local/remote main。
- [ ] 监控 post-merge main CI、Release Please、release jobs 与多架构 image publishing。
- [ ] 在最终 artifact 对应提交做必要 smoke，核对 PR/merge/release/image digest。
- [ ] 更新 parent/children task metadata、evidence 与 archive；安全清理选定 worktree/refs 前先核对
  dirty state，不删除任何用户状态。

## Stop conditions

- sibling wire shape 未冻结却开始最终 decoder；
- required PostgreSQL/MinIO/browser gate 被 skip、替代或 cleanup false-green；
- 需要扩展 permanent delete、三项延期或新增 schema/migration；
- 无法保持权限、非泄露、archive/restore 或 capability rollback；
- protected CI/release/image 尚未完成但有人试图宣称“无需再完善”。
