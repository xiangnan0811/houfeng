# VPS 概览门控与 DTO 校验实施计划

> 所有行为按 RED → 验证 RED → 最小 GREEN → 验证 GREEN 执行。最终 decoder 必须基于
> freshness/action sibling 已冻结的完整 wire contract。

## Phase 0: Start gate

- [x] 用户批准 parent 最终规划；task 正式 start。
- [x] 运行 `trellis-before-dev`，读取 Web state/data、quality、component 与 cross-layer specs。
- [x] 确认 sibling DTO 已合入当前 remediation branch，重新读取 Go `types.go` 与 TS types。
- [x] 记录 focused baseline；不得把当前静默 fallback 当作通过证据。

## Phase 1: RED — success-body boundary

**Files:** `web/src/lib/recordsApi.test.ts`、`web/src/lib/recordsApi.ts`

- [x] 为完整 valid wire、valid capability-off、malformed JSON、`null`、`[]`、`{}` 写 RED。
- [x] 表驱动覆盖 identity/summary/section/activity/anomaly/action/relation/capability 嵌套失真。
- [x] 断言 additive `internal_debug` 被忽略且不进入返回 object；错误不保存或显示 raw body。
- [x] 运行 focused Vitest，记录这些 case 在 permissive normalizer 下失败。

## Phase 2: GREEN — local typed decoder

- [x] 增加 `InvalidVPSOverviewResponseError` 和小型 unknown-narrowing helpers。
- [x] 只在 `getVPSOverview` 将 success `SyntaxError` 转为 typed malformed error；保留其他错误。
- [x] 实现完整 validating projection；不引入 dependency，不扩大共享 `apiRequest` 行为。
- [x] 运行 `recordsApi.test.ts` 与 `apiRequest.test.ts` 至 GREEN，并跑 transport architecture test。

## Phase 3: RED/GREEN — explicit gate and retry

**Files:** `web/src/pages/VPSDetailPage.tsx`、对应测试、`useVPSOverview.test.tsx`

- [x] 先写 RED：typed invalid、network TypeError、unknown/other 503 不加载 legacy，显示错误和重试。
- [x] 保留 valid capability-off、explicit overview-unavailable、404、500 的既有合同测试。
- [x] 写 retry RED：首次失败后点击重试只发第二次 overview probe，并可恢复 overview。
- [x] 实现显式 error allowlist、稳定 safe copy 和 probe revision/latest guard。
- [x] 验证 seeded first paint 仍只请求一次；invalid refresh 保留 last valid overview。

## Phase 4: Production browser proof

- [x] 在 `page-states.spec.ts` 加 HTTP 200 `{}` fixture：error 可见、legacy 不出现、retry 计数 1→2。
- [x] 运行 production-build Chromium case，确认 fixture diagnostics、console、network 均干净。
- [x] 检查 error copy 不含 body、URL、credential 或 server detail。

## Phase 5: Quality and handoff

- [x] Node 22 focused Vitest：`apiRequest`、`recordsApi`、`VPSDetailPage`、`useVPSOverview`、architecture。
- [x] Node 22 `npm --prefix web run lint`、production build、bundle check、`make verify-web`。
- [x] Node 22 完整 `npm --prefix web run test:e2e`；`git diff --check`。
- [x] `trellis-check` 独立复核；把 RED/GREEN、browser 和 no-leak evidence 写回 task。

## Stop conditions

- 最终 Go/TS wire shape 尚未冻结；
- 必须改变共享 transport 或新增 schema dependency 才能继续；
- backend 没有稳定的 `overview_unavailable` code；
- decoder 会把原始 payload/error detail带入 UI 或日志。
