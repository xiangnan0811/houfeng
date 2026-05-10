# Asset Ledger roadmap completion audit

## Goal

对照 `houfeng_codex_下一步开发计划.md` 和当前仓库实际实现，确认 VPS Asset Ledger + Fleet Observability 下一阶段计划的完成度、剩余缺口和不应继续扩张的边界。该任务先做证据化对账；如发现小而明确的缺口，可在同一任务内修复；如发现较大缺口，拆成后续独立 task。

## What I already know

- 当前仓库在 `main` 基线干净，最近已合入 PR #21 `feat: add VPS domain assets MVP`。
- 用户希望后续可以合理规划后自动推进至完成计划文档中规划的所有内容。
- 用户明确要求继续遵守 Trellis 规范和此前 Git 流程：feature branch、PR、CI 全绿后 merge、同步本地 `main`。
- 用户此前明确要求后续不使用 subagent，本任务在主会话内完成。
- `houfeng_codex_下一步开发计划.md` 的 Task 1-8 已有大量内容落地，当前仓库已有 `0016` 到 `0024` 资产相关 migrations。
- 当前代码已包含 providers、VPS assets、subscriptions、VPS-node links、renewal decisions、asset histories、experience logs、asset services、asset domains、importing、前端资产页面和 Dashboard 资产摘要等模块。
- 真实 40 多台 VPS 数据问题此前被用户暂缓；本任务不能宣称真实数据导入已完成，只能判断 dry-run/import 工具链是否具备。

## Assumptions

- 计划文档仍是本轮完成度对账的产品范围基线。
- frozen V1 baseline 不因本轮资产扩展对账而被改写。
- 如果真实数据文件不在仓库内，本任务只记录真实数据验证处于用户数据等待状态。

## Requirements

- 建立计划完成度矩阵，覆盖 `houfeng_codex_下一步开发计划.md` 第 9 节 Task 1-8。
- 建立第一阶段完成标准矩阵，覆盖计划文档第 11 节 12 条完成标准。
- 每个判断必须有仓库证据，例如 migration、domain types、store、handler、router/bootstrap、API client、页面、测试或 CI/local verify。
- 明确区分：
  - 已完成
  - 部分完成
  - 暂缓
  - 阻塞
  - 应拆分为后续 task
- 审计当前 real-data dry-run/import 状态：工具链、校验、报告、实际数据验证边界。
- 审计 services/domains 当前实现是否超出计划暂缓边界；如只是 VPS-scoped 轻量记录，则记录为已完成扩展，不扩张为完整服务注册表或完整域名管理。
- 若发现小而明确、低风险的缺口，可以在本任务内修复并测试。
- 若发现大缺口，不在本任务内扩张实现，改为在审计文档中拆出后续 task。
- 最终把对账结果固化到仓库文档，避免后续重复推进或误判。

## Acceptance Criteria

- [ ] 有一份仓库内审计文档记录 Task 1-8 完成度矩阵和证据。
- [ ] 有一份仓库内审计文档记录第 11 节第一阶段完成标准的完成度矩阵和证据。
- [ ] 明确记录真实数据 dry-run/import 的当前状态和暂缓边界。
- [ ] 明确记录后续剩余 task 队列；若无应立即开发的剩余功能，应说明原因。
- [ ] 若进行了代码或文档修复，相关测试通过。
- [ ] 运行 `git diff --check`。
- [ ] 运行项目全量验证或与本任务范围匹配的质量门；如无法运行，记录原因。
- [ ] Trellis task 被归档，journal 被记录。
- [ ] 通过 PR 合并，PR CI 和 main CI 均为 green。

## Definition of Done

- Tests/checks pass locally.
- Trellis specs/docs updated if audit reveals durable workflow or cross-layer contract changes.
- Work commits completed before finish-work archival/journal commits.
- PR created, monitored, merged only after checks pass.
- Local `main` synced after merge and post-merge main CI monitored to completion.

## Out of Scope

- 不处理真实 40 多台 VPS 数据文件本身。
- 不新增 Provider API 自动同步、DNS Provider API 自动同步、Web SSH、插件系统、服务自动发现、多用户 RBAC、汇率换算或复杂评分算法。
- 不把当前 VPS-scoped services/domains 扩张为完整服务注册表或完整域名管理。
- 不重写现有 Fleet Observability 语义。

## Technical Notes

- 计划文档：`houfeng_codex_下一步开发计划.md`
- 当前迁移尾部：`db/migrations/0016_create_asset_ledger.sql` 到 `db/migrations/0024_create_asset_domains.sql`
- 相关后端模块：`internal/center/providers/`、`internal/center/vpsassets/`、`internal/center/subscriptions/`、`internal/center/assetlinks/`、`internal/center/renewals/`、`internal/center/importing/`、`internal/center/assetservices/`、`internal/center/assetdomains/`
- 相关前端页面：`web/src/pages/ProvidersPage.tsx`、`web/src/pages/VPSPage.tsx`、`web/src/pages/VPSDetailPage.tsx`、`web/src/pages/SubscriptionsPage.tsx`、`web/src/pages/AssetDecisionsPage.tsx`、`web/src/pages/DashboardPage.tsx`
- 本机验证应使用 repo-local temp dir：`TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build`
