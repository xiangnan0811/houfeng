# VPS 概览来源隔离与 freshness 实施计划

> 所有 production behavior 按 RED → 验证 RED → 最小 GREEN → 验证 GREEN。时间测试使用
> clock/channel authority，不用脆弱的毫秒 elapsed 断言。

## Phase 0: Start gate

- [ ] 用户批准 parent 最终规划；task 正式 start。
- [ ] 运行 `trellis-before-dev`，读取 backend error/quality、Web state/component/quality 与 cross-layer specs。
- [ ] 记录 focused Go/Web baseline，并核对真实 PostgreSQL performance fixture 可用；缺 DSN 不得 skip。

## Phase 1: RED — granular source isolation

**Files:** `internal/center/vpsoverview/service_test.go`、`internal/center/store/vps_overview_test.go`

- [ ] 写 slow monitoring barrier RED：所有 siblings 已开始并 ready，只有 monitoring timeout。
- [ ] 表驱动六类 failure：monitoring/IP/subscription/service/domain/activity 只降级自身。
- [ ] 写 total-budget partial、identity-before-readers fatal、caller-cancel fatal RED。
- [ ] 写 reverse-completion stable relation order 与 monitoring/subscription query reuse RED。
- [ ] 记录当前 bundled sequential model 的预期失败。

## Phase 2: GREEN — reader split and bounded concurrency

- [ ] 拆 granular SourceReader/result types 与 validated SourceBudgets，默认 total/per-source 800ms。
- [ ] store method 每个只做一次 bounded authority query并返回 error，不在 store 偷做 unavailable mapping。
- [ ] service identity-first，随后 buffered typed channel 并发收集六源；映射 closed safe reason。
- [ ] pending total-timeout 只降级自己，完成值保持；goroutines 观察 cancellation，无共享 Overview mutation。
- [ ] 运行 service/store focused tests与 `-race -count=10` 至 GREEN。

## Phase 3: RED/GREEN — truthful freshness and wire

**Files:** types/store/service/handler tests、`web/src/lib/types.ts`

- [ ] RED：未来 RenewAt 与过去 UpdatedAt 分离；empty renewal ready/null；service/domain max UpdatedAt。
- [ ] RED：每个 section timestamp `<= generated_at`，未来 source clock 降 stale 且不 clamp；deadline 保留。
- [ ] RED：relation marshal/handler JSON required section，同时保留旧字段和 non-null collections。
- [ ] 实现 authority mapping、post-collection timestamp validator 与 required relation.section。
- [ ] 对齐 TypeScript三态 union、canonical fixtures；通知 action/gate child shape 已冻结。

## Phase 4: RED/GREEN — local Web presentation

**Files:** freshness component、SummaryGrid、RecentActivity、Relations、PageView、hook及测试

- [ ] 先写三态/时间/known+unknown reason/no-raw-code/retry accessible-name RED。
- [ ] RED：ready zero=0；unavailable zero=“—”；activity unavailable 不是 true empty；stale rows 保留。
- [ ] RED：六个 degraded surface 只有自己本地 retry，siblings ready，旧全局 note 不存在。
- [ ] 实现 route-private freshness component并接入三个 local owners；retry 不嵌套 Link。
- [ ] 复用 full refresh，loading 保留 page 并禁用多按钮；focused Vitest 至 GREEN。

## Phase 5: Browser and performance proof

- [ ] E2E partial profile 覆盖 stale IP、unavailable renewal/service/activity，桌面与 390px。
- [ ] 断言 retry 只增加现有 overview request、page 保持挂载、无 document overflow、Axe/keyboard 通过。
- [ ] 真实 PostgreSQL store/handler `TestPostgresIntegrationVPSOverviewPerformance`：零 skip，记录
  query count/error rate/p95，继续满足既有 750ms endpoint evidence。

## Phase 6: Quality and handoff

- [ ] Go 1.26.2 focused/race、`make verify-go`；Node 22 focused/full、`make verify-web`、完整 Chromium。
- [ ] `git diff --check` 与 non-leak scan；`trellis-check` 独立复核 concurrency、time authority、UI ownership。
- [ ] 保存 RED/GREEN、PostgreSQL/browser evidence，冻结 DTO 给后续 children。

## Stop conditions

- 缺少 required PostgreSQL fixture而只能 skip performance；
- 并发需要共享 transaction/connection或造成 N+1；
- source freshness 没有权威却要求伪造 last success；
- raw error/reason/checkpoint/worker timestamp 进入 wire/UI；
- 需要新增 schema/cache/source-specific endpoint或扩大 production timeout 才能变绿。
