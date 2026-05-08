# Refine VPS Asset Ledger Plan

## Goal

把根目录 `houfeng_codex_下一步开发计划.md` 从方向性建议整理成可执行、可验收的下一阶段计划。计划应保留 `VPS Asset Ledger + Fleet Observability` 的产品方向，但修正当前文档中会误导后续实现的问题，确保后续进入开发前有稳定依据。

## What I Already Know

- 用户明确要求：计划存在问题时，先完善计划，计划没有问题才能进入后续执行。
- 当前根目录计划文档已提出正确方向：保留现有监控底座，新增 VPS 资产层，通过 `vps_node_links` 关联现有 Node。
- 当前仓库 migration 已经到 `0015_add_host_containers.sql`，原计划中的 `0011_create_asset_ledger.sql` 会冲突。
- 当前仓库仍处于 V1 收口期，README/CLAUDE 指向 `docs/release/next-phase-plan.md` 和 frozen V1 baseline；资产台账应被定位为 post-V1 / MVP 扩展方向，而不是改写 V1 baseline。
- 现有 Node / Target 领域状态使用中文业务值；资产层若使用英文内部值，需要显式定义 API/DB/UI 文案策略。
- 现有 `nodes.provider` 是监控节点元数据，新增 `providers` 表后需要定义迁移期关系，避免双来源混乱。
- 当前 Dashboard 已有系统概览工作台契约；资产指标应作为少量摘要入口后置接入，不能重新变成字段堆叠页。

## Requirements

- 修订根目录计划文档，使其成为正式执行前的计划基线。
- 明确下一阶段定位为 post-V1 / MVP 扩展，不改变当前 V1 baseline 权威性。
- 修正 migration 编号，从当前最大编号之后开始。
- 明确资产层核心模型、状态策略、Node/Provider 关系、Dashboard 边界。
- 将后续任务拆成可运行闭环，避免只有 skeleton、没有 API/测试的虚任务。
- 将真实 40 多台 VPS 的 JSON dry-run/import 提前作为模型验证步骤。
- 不修改 Go/React 业务实现。

## Acceptance Criteria

- [ ] 根目录计划文档不再要求新增冲突的 `0011_create_asset_ledger.sql`。
- [ ] 计划明确 `VPS Asset Ledger + Fleet Observability` 是 post-V1 / MVP 扩展方向。
- [ ] 计划明确现有 Node/Target/Agent 语义不变，资产层通过关联增强监控层。
- [ ] 计划明确资产状态值和中文展示策略，避免未定义的中英文混用。
- [ ] 计划明确 `providers` 与现有 `nodes.provider` 的迁移期关系。
- [ ] 计划把第一批执行任务拆成可验收闭环，并列出测试/验证命令。
- [ ] 如发现本地规范文档存在过期迁移编号描述，同步修正，避免后续 agent 继续按旧编号执行。

## Out of Scope

- 不实现 asset ledger migration。
- 不实现 providers / vps / subscriptions API。
- 不改 Dashboard、Node、Target、Agent 的运行行为。
- 不导入真实 VPS 数据。
- 不调整现有 V1 frozen baseline 文档。

## Technical Notes

- 根计划文档：`houfeng_codex_下一步开发计划.md`
- 当前 migration：`db/migrations/0015_add_host_containers.sql` 是最大编号；新增资产迁移应从 `0016_*` 开始。
- 数据库规范：`.trellis/spec/backend/database-guidelines.md`
- 当前路由新增约定见 `CLAUDE.md`：新增 endpoint 需要 handler、RouterOptions/router.New、bootstrap wiring、handler tests。
- 当前前端 API 约定：页面通过 `web/src/lib/api.ts` 和 `web/src/lib/types.ts`，不要页面内直接 fetch。
