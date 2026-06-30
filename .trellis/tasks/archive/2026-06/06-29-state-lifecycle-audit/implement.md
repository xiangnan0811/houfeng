# 状态生命周期修复实施计划

## 原则

- 每个行为变化先写失败测试并运行到 RED。
- 每个修复尽量按现有边界落地：domain validation、service evaluator、store read correction、front-end props。
- 先修 P1/P2 的真实错误，再做文案降级和复审报告。

## 步骤

### 1. 启动任务

- [x] 运行 `python3 ./.trellis/scripts/task.py start .trellis/tasks/06-29-state-lifecycle-audit`。

### 2. 订阅筛选枚举合同

- [x] 在 `internal/center/subscriptions/types_test.go` 写 RED：`RenewalDecision: "migrate"` 合法，`"manual"` 非法。
- [x] 如已有 handler 测试覆盖 query，增加 `/api/subscriptions?renewal_decision=migrate` 不返回 400 的断言。
- [x] 修改 `internal/center/subscriptions/types.go`，用 `vpsassets.NormalizeRenewalDecision` / `IsValidRenewalDecision`。
- [x] 运行 `go test ./internal/center/subscriptions ./internal/center/http/handlers ./internal/center/store`。

### 3. VPS 普通 PATCH lifecycle 边界

- [x] 在 `internal/center/vpsassets/types_test.go` 或 handler 测试写 RED：普通 PATCH 到 `to_cancel`、`cancelled`、`to_migrate`、`archived` 均拒绝；`active`、`idle`、`testing` 允许。
- [x] 增加 domain helper，例如 `IsOrdinaryPatchLifecycleStatus`，由 handler 调用。
- [x] 保持已有 archive/cancellation endpoint 不受影响。
- [x] 运行 `go test ./internal/center/vpsassets ./internal/center/http/handlers`。

### 4. 后端监控阈值与 heartbeat stale

- [x] 在 `internal/center/incidents/evaluator_test.go` 写 RED：自定义 Load5/IOWait 阈值会改变 severity；`stale_threshold_intervals=4` 时 missed=3 为 notice、missed=4 为 alert。
- [x] 扩展 `MetricThresholds` 支持 IOWait/Load5 warning/alert/critical。
- [x] 将 `MetricThresholdsFromDefaults` 接入 settings 的 IOWait/Load5，并派生 alert。
- [x] 扩展 `incidentTiming`，把 stale threshold 传入 heartbeat evaluator。
- [x] 运行 `go test ./internal/center/incidents ./internal/center/settings`。

### 5. 非运行态 heartbeat sweep

- [x] 在 `internal/center/incidents/service_test.go` 写 RED：暂停、维护、已退役监控实例不会由 `EvaluateStaleMonitoringInstances` 生成 heartbeat incident。
- [x] 在 incident service sweep 前过滤非运行态，或在 heartbeat-only evaluator 返回 recovered/skipped。
- [x] 保持维护采样上下文已有 suppress/recover 测试不回退。
- [x] 运行 `go test ./internal/center/incidents ./internal/center/store`。

### 6. IP 质量 stale 配置化

- [x] 在 `internal/center/settings/types_test.go` 写 RED：默认 `StaleAfterSeconds=604800`；小于 frequency 的 stale window 被拒绝或归一到合法值。
- [x] 新增 migration `db/migrations/0047_add_ip_quality_stale_after_seconds.sql`，更新 settings JSON default/backfill。
- [x] 扩展 Go/TS settings 类型、handler DTO、settings page 表单。
- [x] 在 IP quality store 或消费 store 测试写 RED：summary stale 会按 settings-derived window 修正，而不是固定 view 值。
- [x] 运行 `go test ./internal/center/settings ./internal/center/store ./internal/center/http/handlers` 与相关 web tests。

### 7. 前端阈值对齐

- [x] 新增/更新 `web/src/config/thresholds.test.ts`：默认 CPU/Mem/Disk/Inode 三层与后端默认一致，`resolveThresholds` 支持 alert 和 IOWait/Load5 派生 alert。
- [x] 让 `MonitoringInstanceWatchtowerMetrics` 接受 `thresholds` prop，默认 fallback。
- [x] 让 `MonitoringInstancesTrendCell` 接受 `thresholds` prop。
- [x] 在 Monitoring detail/list 页面加载 settings 并传入阈值；loading/error 时用 fallback，不阻塞主数据。
- [x] 更新相关页面测试，运行 `cd web && npm test -- --run MonitoringDetailPage.test.tsx MonitoringPage.test.tsx SettingsPage.test.tsx`。

### 8. 迁移文案收口

- [x] 写前端/后端测试或快照断言，确保执行计划文案不再出现“打开 VPS 详情推进迁移”。
- [x] 修改 asset decisions execution plan 和可见 UI 文案为“标记迁移意向/人工跟进”。
- [x] 运行 `go test ./internal/center/assetdecisions` 和 `cd web && npm test -- --run AssetDecisionsPage.test.tsx VPSDetailPage.test.tsx`。

### 9. 全量验证与复审

- [x] 运行 `go test ./internal/center/subscriptions ./internal/center/vpsassets ./internal/center/incidents ./internal/center/settings ./internal/center/store ./internal/center/http/handlers ./internal/center/assetdecisions`。
- [x] 运行 `cd web && npm test -- --run MonitoringPage.test.tsx MonitoringDetailPage.test.tsx SettingsPage.test.tsx AssetDecisionsPage.test.tsx VPSDetailPage.test.tsx SubscriptionsPage.test.tsx`。
- [x] 运行 `cd web && npm run build`。
- [x] 视时间运行 `make verify-go`、`cd web && npm run lint`。
- [x] 运行 browser sanity：使用 `uv run --with playwright` 的本地 Playwright，不把 Playwright/Cypress/WebDriverIO 加入 `web/package.json` 或 CI。
- [x] 新增 `research/post-fix-review.md`，逐条复审 SLC-01 到 SLC-12：已修复、已缓解、后续任务。
