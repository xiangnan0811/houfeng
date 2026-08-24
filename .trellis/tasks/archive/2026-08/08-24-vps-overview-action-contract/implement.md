# VPS 概览动作与关系目的地实施计划

> 依赖 I-03 relation.section 已冻结。每个行为先建立会因当前 invalid/same-page route 失败的
> RED，再做最小 GREEN；“Link 存在”不是行为证据。

## Phase 0: Start gate

- [x] 用户批准 parent 最终规划；task 正式 start。
- [x] 运行 `trellis-before-dev`，读取 Web component/state/quality、backend quality 与 route specs。
- [x] 确认 freshness child 已 GREEN，读取最终 Go/TS relation shape 与 canonical fixtures。
- [x] Node 22 记录 `npm audit --omit=dev --json` RED 及 focused baseline。

## Phase 1: RED — backend semantic destinations

**Files:** `internal/center/vpsoverview/anomalies_test.go`、`internal/center/store/vps_overview_test.go`

- [x] 表驱动每个 stable rule 的 exact action ID/label/route-vs-command；command route 为空。
- [x] 表驱动 relation order/kind：subscription exact filtered route，其他三类 route 为空。
- [x] 确认当前 nonexistent/same-page/all-relations-same-route 实现按预期 RED。

## Phase 2: RED/GREEN — closed Web resolver

**Files:** 新 `vpsOverviewDestination.ts`/test、types、router tests

- [x] RED 覆盖每个 rule/action pair 与 relation kind 的 exact route/command。
- [x] RED：unknown token、command 带 route、same-origin mismatch、https、`//`、backslash 均 no destination。
- [x] 对每个 route 用 `matchRoutes(appRoutes,to)` 证明命中 owner 而非 wildcard。
- [x] 实现 closed resolver、narrow unions 与 backend producer matrix；Go/Web focused tests至 GREEN。

## Phase 3: RED/GREEN — page-owned commands and relation panels

**Files:** Anomalies、Relations、PageView、management controller/actions、复用 relation sections/tests

- [x] RED：route 是 Link；subscription/decision/menu/retry 是 Button 且调用 exact callback。
- [x] RED：monitoring/services/domains relation 打开 exact VPS-scoped panel，含 loading/error/retry/empty。
- [x] RED：unknown/mismatched destination 不可点击；freshness retry 不嵌套另一 interactive owner。
- [x] RED：panel close/Escape focus 返回 relation trigger，route navigation 保持 keyboard semantics。
- [x] 实现三个 focused read-only panel，复用现有 component/API，不 import Legacy 整页。
- [x] 完整 focused Vitest 与 bundle import contract 至 GREEN。

## Phase 4: React Router 7.18.2

- [x] 更新 direct range并用 Node 22 重新生成 lock；核对两个 router packages exact 7.18.2。
- [x] 运行 `npm audit --omit=dev`，确认 React Router production finding 清零。
- [x] 跑 router match、full Vitest、production build、bundle/CSS budgets；不做 v8 migration。

## Phase 5: Production browser behavior

- [x] 修正 fixture `monitoring_instance`→`monitoring_instances` 并纳入 relation.section。
- [x] 点击每个 route action，断言最终 pathname/search；点击每个 command，断言 dialog/menu/request。
- [x] 点击四类 relation，断言 subscription filter 或 exact VPS panel。
- [x] 注入 external/protocol-relative/backslash/mismatched routes，断言无 Link/外部导航。
- [x] 验证 390px、Axe、keyboard、focus return、console/network diagnostics。

## Phase 6: Quality and handoff

- [x] Go focused/full；Node 22 focused/full、`make verify-web`、完整 Chromium、production audit。
- [x] `git diff --check`；`trellis-check` 独立复核完整 token matrix、route registration、bundle/focus。
- [x] 保存 RED/GREEN/browser/audit evidence，冻结最终 wire shape给 I-02。

## Stop conditions

- 需要伪造不存在/不相关 route 才能让 relation 可点击；
- 复用 relation 内容要求静态 import 整个 Legacy page 或明显超出 bundle budget；
- API route 不能和 closed token 的唯一 destination 对齐；
- 升级必须跨到 React Router v8 或破坏 Data Mode；
- relation panel 需要新增 backend/write/permission contract。
