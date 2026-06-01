# VPS and monitoring instance concept refactor

## Goal

破坏式全栈迁移当前 `Node/节点` 概念，将其统一改为 `Monitoring Instance/监控实例`，机器名为 `monitoring_instances`，用户入口为 `监控 /monitoring`。通过明确 `VPS = 服务器资产账本对象`、`MonitoringInstance = agent 接入后的运行观测对象`，消除 VPS 与节点在用户理解上的冲突。

## Requirements

- 不保留旧 `/nodes` 前端路由、`/api/nodes` 后端 API、`node_id` JSON 字段或旧 agent token 文件兼容。
- 数据库核心表、列、索引、外键、聚合表和资产关联表迁移到 monitoring instance 命名。
- 后端领域包、仓库、handler、router、bootstrap、sync/onboarding/runtime controls、asset links、dashboard、incidents、runtime facts、sparklines、batch action 改用 MonitoringInstance 命名。
- Agent 合同改为 `monitoring_instance_id`；enroll/sync/token file/log/error code 同步迁移。
- 前端入口改为 `/monitoring`、`/monitoring/compare`、`/monitoring/:monitoringInstanceId`；导航、面包屑、全局搜索、Dashboard 深链和 mock visual evidence 同步替换。
- 页面职责清晰区分：VPS 管服务器资产，订阅管成本事实，资产决策管人工队列，监控管运行观测，入口探测/事件继续属于观测体系。
- 文档和 active Trellis specs 更新新领域不变量；不修改 frozen v1 baseline 来追认新行为。
- 不新增 TSDB、MQ、图表库或外部监控系统；性能监控、路由监控只作为 `/monitoring` 信息架构预留。

## Acceptance Criteria

- [x] `rg "/nodes\\b|/api/nodes|node_id|NodeRecord|节点" web/src internal agent cmd db docs/operations CLAUDE.md .trellis/spec` 不再发现公开契约层的旧节点命名；允许历史 frozen v1 baseline、第三方 Node.js 语境和必要的迁移兼容 SQL 注释。
- [x] 新 API 使用 `/api/monitoring-instances*`、`/api/asset-context/monitoring-instances`、`/api/vps/{id}/monitoring-instances` 和 `link-monitoring-instance` / `unlink-monitoring-instance`。
- [x] 新前端路由使用 `/monitoring*`，Sidebar 的观测分组显示 `监控 / 入口探测 / 事件`。
- [x] Agent enroll/sync JSON 使用 `monitoring_instance_id`，token file 也保存 `monitoring_instance_id`。
- [x] VPS、订阅、资产决策、监控、入口探测、事件页面的跳转和文案符合职责边界。
- [x] `go test ./internal/contracts/agentapi`、`go test ./agent/...`、`go test ./internal/center/http/...`、`make verify-go` 通过。
- [x] `cd web && npm run lint`、`cd web && npm run test -- --run`、`cd web && npm run build` 通过。
- [x] `./scripts/verify.sh` 与 `git diff --check` 通过，或记录明确的外部/环境阻塞。
- [x] 浏览器 sanity 或等价本地视觉检查覆盖 `/monitoring`、`/monitoring/:id`、`/vps`、`/asset-decisions`、`/subscriptions`、`/targets`、`/events`。

## Notes

- 用户已确认采用破坏式迁移，不保留旧接口兼容期。
- 当前仓库处于早期开发且无用户，允许本地开发数据库重建或通过迁移进入新 schema；已有测试 agent 需要重新接入。
