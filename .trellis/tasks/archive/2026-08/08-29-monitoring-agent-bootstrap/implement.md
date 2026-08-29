# 首次监控 Agent 接入死锁 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkboxes (`- [ ]`) for tracking. Current collaboration policy forbids spawning subagents without explicit user authorization, so execute inline unless that authorization is later given.

**Goal:** 让已有 VPS、零监控实例的用户从新版 VPS Overview 或 Monitoring 页进入同一个 VPS-scoped Agent 创建/onboarding 流程，同时保留既有关系证据与写入幂等合同。

**Architecture:** 保持后端 action ID 兼容，在前端 resolver 中按 anomaly/relation 来源拆分内部命令；新增一个 page-private Overview onboarding owner，复用现有表单、API 和 AppShell-scoped write store；Monitoring 页仅导向未关联 VPS 库存。

**Tech Stack:** Go、React 19、TypeScript、React Router、Vitest/Testing Library、Playwright、Trellis；Web 验证固定 Node 22.23.1。

---

## Risk boundaries

- 不新增 endpoint、DTO、migration、Agent 协议或 Monitoring 页写入 owner。
- 不改变稳定 wire action ID `open_monitoring_instances`；只按来源改变前端内部命令。
- 没有权威 0-link 证据时不允许 POST；1 link 复用，>1 fail closed。
- 所有 create 必须经过共享 `VPSWriteOwnerStore` 的 `monitoring-create` owner 和 `prepareCreate`。
- 服务端已确认成功后，refresh 失败不能触发第二次 POST 或显示为创建失败。
- 附件 PNG golden 不在本任务修改范围；只接受 Go 1.26.2 项目工具链的验证结果，不用本机 Go 1.27.0-X 的 digest 输出定义仓库基线。

## TDD execution checklist

- [x] **1. Re-establish baseline and observe first RED**
  - 确认工作树仍为 `fix/monitoring-agent-bootstrap`，hooks 已启用，任务仍处于 planning/approved 后才运行 `task.py start`。
  - 运行现有 resolver、Overview management、Monitoring page 与 Go anomaly focused tests，记录 baseline。
  - 先改测试断言：`monitoring.unlinked.v1` 应解析为 `open_monitoring_onboarding`，而 relation 仍为 `open_monitoring_instances`；观察当前 resolver 的预期 RED。

- [x] **2. GREEN — separate anomaly onboarding from relation evidence**
  - 修改 `web/src/pages/vps-detail/vpsOverviewDestination.ts`：为 command union 增加 `open_monitoring_onboarding`，仅修改精确 anomaly pair 的 internal destination。
  - 修改 `web/src/pages/vps-detail/VPSOverviewPageView.tsx`：新命令打开 `monitoring-instance-create`，原命令继续打开 `monitoring-instance-evidence`。
  - 修改 `web/src/pages/vps-detail/hooks/useVPSManagementController.ts` 增加新 panel。
  - 更新 `vpsOverviewDestination.test.ts` 与 `VPSOverviewPageView.test.tsx`，同时断言伪造 route/action fail closed 和 relation 行为未回退。

- [x] **3. RED — authoritative 0/1/many Overview workflow**
  - 新增 `web/src/pages/vps-detail/VPSOverviewMonitoringOnboarding.test.tsx`。
  - 先写并观察失败：打开时必须读 `getVPSAsset`；0 links 显示既有 create form，1 link 直接导航且零 POST，>1 转到 evidence 且零 POST。
  - 覆盖加载失败、关闭后 stale GET 不提交 UI、路由切换后旧结果不导航。

- [x] **4. GREEN — focused Overview onboarding owner**
  - 新增 `web/src/pages/vps-detail/VPSOverviewMonitoringOnboarding.tsx`，复用 `VPSMonitoringInstanceCreateForm`、draft/input helpers、API 和现有样式。
  - 修改 `VPSOverviewManagementActions.tsx`：选择 store/token 后挂载新 owner并传入父级 authority；不要把 workflow 复制进 900 行父组件。
  - 实现打开/关闭、权威 detail load、0/1/>1 分流、focus return 和 stale generation 防护。
  - 运行步骤 3 focused tests直至 GREEN，并确认既有 `VPSOverviewManagementActions.test.tsx` 通过。

- [x] **5. RED — create ownership, idempotency and post-create truth**
  - 扩充 onboarding focused tests：精确 POST body、`monitoring-create` owner、same-body transport retry key 复用、body changed/key reused 轮换、同 VPS 并发写入拒绝。
  - 增加成功路径：confirmed create 后 refresh 成功进入 onboarding；refresh 失败显示“继续接入 agent”且不重发 POST。
  - 增加并发 409：重新读取后 1 link 收敛导航，>1 evidence，仍为 0 显示错误；`idempotency_key_reused` 不走 active-link猜测并按 store 合同轮换。

- [x] **6. GREEN — create and convergence behavior**
  - 使用 `buildMonitoringInstanceCreateInput`、`writeOwnerStore.begin`、`prepareCreate`、`createVPSMonitoringInstance`、`finishCreate` 实现 submit。
  - 所有 completion UI 和 navigation 先检查 generation；`finally` 使用 exact owner settle。
  - confirmed create 后构造 allowlisted onboarding URL；refresh false 时关闭 panel并保留 continuation action，不把请求降级为 unknown。
  - 对符合设计的 409 执行一次权威 GET 分流，不自动 POST，不解析自由文本错误。

- [x] **7. RED/GREEN — deep links and Monitoring copy**
  - 在 `vpsManagementHelpers.test.ts` 先要求 `monitoring` 与 `monitoring-instance-create` 都解析到 create panel，未知值仍返回 null；实现 parser 扩展。
  - 在 `VPSDetailPage.test.tsx` 中断言 workbench 只打开一次并以 replace 删除 query；由 `VPSDetailPage` 保持唯一 query owner，删除 `VPSOverviewManagementActions` 内重复的 `useSearchParams` effect/import，并把既有“未知 archive workbench 被清除但不打开”测试移到 route 层。
  - 在 `VPSPage.test.tsx` 先断言 `view=unlinked` 的名称链接和非交互行点击都进入 `/vps/{id}?workbench=monitoring`，而其他 view 仍进入普通详情；再用一个共享 href helper 更新 `VPSPage.tsx` 的两个导航点。
  - 修改 `MonitoringPage.test.tsx`，先让当前 `/vps`、创建第一台 VPS 文案产生 RED；再修改 `MonitoringHero.tsx`、`MonitoringInstancesListSection.tsx`、`MonitoringPage.tsx`，统一导航 `/vps?view=unlinked` 和批准文案。
  - 保留非 first-run filter empty 状态及统计卡行为。

- [x] **8. RED/GREEN — backend action label contract**
  - 修改 `internal/center/vpsoverview/anomalies_test.go::TestEvaluateAnomaliesActionDestinations`，要求 ID 仍为 `open_monitoring_instances`、label 为“创建并接入 agent”，先观察 RED。
  - 仅修改 `internal/center/vpsoverview/anomalies.go` 的 label，运行该 package 全量 tests；确认 golden/排序/route 未变化。

- [x] **9. Browser regression for the reported deadlock**
  - 扩充 `web/e2e/vps-overview-destinations.spec.ts` fixture：Overview capability 开启且 0 active links。
  - 断言从未关联异常点击后出现“接入/升级 agent”表单；提交只发一个 VPS-scoped POST，使用幂等 header，返回后进入带 `onboarding=1&return_vps=` 的监控详情。
  - 增加 Monitoring → 未关联库存 → 选择 VPS 的链路断言，确认选择后直接由 `workbench=monitoring` 打开同一表单，而不是进入普通详情后再次寻找动作。
  - 保留/补强 relation `monitoring_instances` 的 read-only evidence 断言，并检查 390px viewport 下对话框可操作、无横向溢出。

- [x] **10. Focused and full verification**
  - Node 22 下运行全部 touched Vitest files、ESLint/type-check/build，再运行 `make verify-web`。
  - 运行 `go test ./internal/center/vpsoverview -count=1`、相关 handler/store regression（若未改后端数据流则不虚构数据库验证）。
  - 运行 focused Playwright 后运行仓库 Web e2e gate；检查 console error、可访问名称、focus/keyboard close 与 responsive layout。
  - 使用 `go.mod` / CI 固定的 Go 工具链运行 `make verify-go`；本机其他 Go 版本的输出不能替代项目工具链证据。
  - 搜索 touched diff，确认没有 idempotency key/digest/agent secret/raw body 泄露；运行 `git diff --check` 与 status/index 检查。

- [x] **11. Review and protected delivery checkpoint**
  - 使用 `trellis-check` 与 `superpowers:verification-before-completion` 对 PRD acceptance、0/1/>1、owner settle、Monitoring 文案和 browser evidence逐项复核。
  - 在用户未授权提交/发布时，保持 feature worktree dirty，报告精确文件与验证证据后停止。
  - 若用户后续明确授权完整交付，再按逻辑批次提交、push feature branch、创建 PR、等待 required CI；merge/release/image 发布必须分别核验，不能把 PR 创建当作完成。

## Planned validation commands

```bash
npx --yes --package node@22.23.1 --call 'npm --prefix web run test -- --run src/pages/vps-detail/vpsOverviewDestination.test.ts src/pages/vps-detail/VPSOverviewPageView.test.tsx src/pages/vps-detail/VPSOverviewMonitoringOnboarding.test.tsx src/pages/vps-detail/VPSOverviewManagementActions.test.tsx src/pages/vps-detail/vpsManagementHelpers.test.ts src/pages/VPSDetailPage.test.tsx src/pages/VPSPage.test.tsx src/pages/MonitoringPage.test.tsx'
GOTOOLCHAIN=go1.26.2 make verify-go
npx --yes --package node@22.23.1 --call 'make verify-web'
npx --yes --package node@22.23.1 --call 'npm --prefix web run test:e2e -- e2e/vps-overview-destinations.spec.ts'
./scripts/verify.sh
git diff --check
git status --short --branch
```

Playwright CLI 参数若 package script 不透传，实施时必须从当前 `web/package.json` 解析准确入口并记录真实命令，不用猜测命令替代证据。
