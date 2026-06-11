# Implementation Plan

## Pre-implementation

- [x] 确认位于非 main 分支。
- [x] 启用 `scripts/setup-git-hooks.sh`。
- [x] 读取 web/backend/guides 相关 Trellis specs。

## Red Tests

- [x] 更新 `web/src/pages/MonitoringPage.test.tsx`：期望不出现「资产判断支撑」「资产上下文」「资产组合决策」，并断言 fetch 不请求 `/api/asset-context/monitoring-instances`。
- [x] 更新 `web/src/lib/api.test.ts`：删除 monitoring-instance asset-context 期望，保留 Target asset-context 期望。
- [x] 更新 Go router/handler/store/bootstrap 测试期望，删除 monitoring-instance asset-context 分发和 wiring 断言，保留 Target asset-context。
- [x] 运行 targeted tests，确认因 production code 未改而失败。

## Implementation

- [x] 删除 `MonitoringSupportSurface` 组件和 MonitoringPage 中的渲染/派生数据。
- [x] 删除 Monitoring 列表页 asset-context 请求、state、error UI。
- [x] 删除 Monitoring 表格 `asset_context` 列和 `assetContexts` 入参。
- [x] 删除前端 API/type 中 monitoring-instance asset-context 导出；收缩 `assetContextSummary` 到 Target/shared helper。
- [x] 删除后端 monitoring-instance asset-context handler/router option/bootstrap wiring/repository/store query。
- [x] 更新 `scripts/visual_evidence.py` mock。
- [x] 更新 `docs/design/v2-houfeng/component-spec.md`、`.trellis/spec/web/state-and-data.md`、`.trellis/spec/backend/database-guidelines.md`。

## Verification

- [x] `npm --prefix web test -- MonitoringPage api`
- [x] `go test ./...`
- [x] `sh scripts/verify.sh`
- [x] `rg` 确认 monitoring-instance asset-context 合同无残留，Target asset-context 仍存在。
