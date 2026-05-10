# Experience logs for VPS assets

## Goal

在现有 VPS Asset Ledger 的时间线能力旁边补齐 `experience_logs` MVP，用于记录单台 VPS 的使用体验、稳定性事件、迁移/取消原因和人工备注。该任务延续 `houfeng_codex_下一步开发计划.md` 的后续对象规划，但不处理真实 VPS 数据导入，也不推进服务目录、域名管理或 Provider API 自动同步。

## What I Already Know

* 当前资产层已经有 `providers`、`vps_assets`、`subscriptions`、`vps_node_links`、`renewal_decisions`、`price_histories`、`ip_histories` 和 `vps_spec_snapshots`。
* VPS 详情页已经通过 `GET /api/vps/{vps_id}/timeline` 展示续费、价格、IP、规格历史。
* 经验日志最自然的落点是同一个 VPS 详情/时间线路径，而不是新建一个独立总览页。
* 本轮不处理真实 40+ VPS JSON dry-run/import 的实际执行。
* 本轮不使用 subagent；实现、检查、PR/CI/merge 流程由主会话完成。

## Requirements

* 新增 `experience_logs` 持久化表，挂在 `vps_assets.vps_id` 下，删除 VPS 时级联清理。
* 后端提供创建和按 VPS 列表读取能力：
  * `POST /api/vps/{vps_id}/experience-logs`
  * `GET /api/vps/{vps_id}/experience-logs`
* `GET /api/vps/{vps_id}/timeline` 返回 `experience_logs[]`，让 VPS 详情页能在已有时间线中看到经验记录。
* 经验日志字段聚焦 MVP：
  * `experience_log_id`
  * `vps_id`
  * `category`
  * `severity`
  * `summary`
  * `details`
  * `occurred_at`
  * `created_at`
* `category` 使用稳定英文机器值：
  * `note`
  * `stability`
  * `network`
  * `support`
  * `billing`
  * `migration`
  * `cancellation`
* `severity` 使用稳定英文机器值：
  * `info`
  * `warning`
  * `critical`
* 后端负责 trim、枚举校验、必填字段校验和时间归一到 UTC。
* 前端通过 `web/src/lib/api.ts` 调 API，通过 `web/src/lib/types.ts` 定义类型，不在页面直接 `fetch`。
* VPS 详情页提供一个轻量表单创建经验日志，成功后刷新详情时间线。
* 时间线组件展示经验日志的分类、严重程度、摘要、详情和发生时间。

## Acceptance Criteria

* [ ] 新迁移使用当前最大 migration 之后的下一个编号，幂等创建 `experience_logs` 表、约束和索引。
* [ ] 领域层有 `ExperienceLogRecord` / `CreateExperienceLogInput` / repository contract、normalize、validate 和单元测试。
* [ ] Postgres repository 能创建日志、按 VPS 列表日志、在 timeline 中聚合日志，并将 not found / invalid input 映射为领域错误。
* [ ] HTTP handler 覆盖 GET / POST、invalid JSON、invalid input、not found、method not allowed 和 repo failure。
* [ ] Router / bootstrap 显式注册新 endpoint，并更新 bootstrap/router 测试。
* [ ] 前端类型、API client 和 API 测试覆盖新 contract。
* [ ] VPS 详情页能渲染经验日志、创建日志、展示 loading/error/success 状态，并有页面测试覆盖。
* [ ] 本地 `./scripts/verify.sh` 通过。
* [ ] 分支通过 PR 创建、CI 全绿、合并、同步本地 `main`。

## Out of Scope

* 真实 VPS JSON dry-run/import 的实际执行与真实数据修正。
* Provider API / DNS Provider API 自动同步。
* 服务目录、域名管理、Web SSH、插件系统、多用户 RBAC。
* 经验评分算法、跨 VPS 经验总览、复杂搜索筛选、批量导入经验日志。
* 修改 Node / Target / Agent 的既有语义。

## Technical Notes

* 后端模式参考 `internal/center/renewals/types.go`、`internal/center/store/renewal_decisions.go`、`internal/center/http/handlers/vps.go`。
* 前端模式参考 `web/src/pages/VPSDetailPage.tsx`、`web/src/components/VPSTimelinePanel.tsx`、`web/src/lib/api.ts`、`web/src/lib/types.ts`。
* `experience_logs` 属于 Asset Ledger 历史数据，不得创建 `vps_node_links`，不得改写 `nodes.provider`、Node lifecycle / monitoring / health、Target、Agent、Provider 或 Subscription。
* 数据流：UI form -> `createVPSExperienceLog` -> HTTP handler -> `renewals.Repository` -> `experience_logs` -> `GET /timeline` -> `VPSTimelinePanel` 展示。

## Definition of Done

* 代码、测试、Trellis 归档和开发日志完成。
* 本地质量门和 PR CI 通过。
* PR 合并后本地 `main` 更新到远端最新状态。
