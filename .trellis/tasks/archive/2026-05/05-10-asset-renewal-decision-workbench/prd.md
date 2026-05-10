# Asset renewal decision workbench

## Goal

补齐计划文档中尚未落地的独立资产决策页，让续费候选、未评估 VPS、待迁移/待取消队列从 Dashboard 摘要和列表筛选升级为一个可操作工作台。完成后，用户可以先在工作台集中判断资产续费方向，再进入 VPS 详情处理更细的 Node 关联、基础事实或历史追踪。

## What I already know

* `houfeng_codex_下一步开发计划.md` 的第一阶段资产台账链路已完成 Provider/VPS/Subscription 后端、导入 dry-run 命令、Node 关联、Dashboard 摘要、历史与前端列表/详情编辑。
* 用户已明确暂不处理真实 40+ VPS 数据导入/验证，本任务继续不执行真实数据 dry-run/import。
* 之前的 `asset-operation-frontend-closure` 明确把 standalone decision workbench page 排除在外；本任务正是补齐该缺口。
* `DashboardPage` 当前把 30 天续费、待决策、待迁移/待取消链接到 `/subscriptions` 或 `/vps` 的筛选页，缺少一个集中处理入口。
* 前端已有 `listSubscriptions()`、`listVPSAssets()`、`updateVPSAsset()` API helper 和对应类型，不需要新增后端接口。
* 当前导航包含 `/vps`、`/providers`、`/subscriptions`，尚无资产决策工作台路由。
* 本任务按用户要求不使用 subagent；规划、实现、检查和 PR 流程均由主会话直接完成。

## Scope

### In Scope

* 新增前端路由页 `/asset-decisions`，导航名称为 `资产决策`。
* 工作台默认聚合这些队列：
  * 未来 30 天续费订阅，支持切换 30/60/90 天窗口。
  * 续费决策为 `unreviewed` 的 VPS。
  * 续费决策为 `migrate` 的 VPS。
  * 续费决策为 `cancel` 的 VPS。
* 工作台展示每个队列的数量、关键字段和去 VPS 详情/订阅筛选的链接。
* 对 VPS 队列提供直接更新续费决策的操作：
  * 选择一台 VPS 进入处理面板。
  * 选择新的 `renewal_decision` 和可选 `renewal_reason`。
  * 通过 `PATCH /api/vps/{vps_id}` 更新，成功后用后端返回的 VPS 记录更新队列。
* 将 Dashboard 资产摘要中的续费/待决策/待迁移取消入口指向 `/asset-decisions`，保留 VPS/订阅列表作为明细入口。
* 增加页面测试、导航/路由相关测试，以及 Dashboard 深链断言更新。

### Out of Scope

* 不执行真实 VPS JSON 数据导入、dry-run 验证或真实生产数据写入。
* 不新增后端 API、数据库迁移或资产评分算法。
* 不新增删除、归档、批量编辑、恢复或审计审批流。
* 不做 Provider API 同步、DNS/Web SSH/service discovery。
* 不把 Dashboard 扩展成资产明细页面。
* 不引入新的状态管理库、表单库、UI 库或 e2e 框架。

## Requirements

* 新业务请求必须经过 `web/src/lib/api.ts`，页面不得直接 `fetch()`。
* 请求/响应类型继续使用 `web/src/lib/types.ts` 中的 snake_case 类型，不做前端驼峰化。
* 页面按现有 loading/error/data 三态和 `cancelled` flag 模式实现。
* 工作台 UI 保持高密度、操作型，不做营销式说明页。
* 保存成功后使用后端返回的 VPS 记录更新本地队列，不前端自行猜测持久化结果。
* Dashboard 深链变更必须由目标页面真实承接，不能只改链接。

## Acceptance Criteria

* [ ] `/asset-decisions` 可以从主导航进入，并渲染资产决策工作台。
* [ ] 页面加载时请求 30 天续费订阅、未评估 VPS、待迁移 VPS、待取消 VPS。
* [ ] 切换 30/60/90 天窗口会重新请求订阅续费队列，并保留 VPS 队列。
* [ ] 用户可以从未评估/待迁移/待取消队列选择 VPS，提交新的续费决策和可选理由。
* [ ] 保存成功后当前 VPS 从旧队列移除，并按新决策进入对应队列或从工作台队列消失。
* [ ] Dashboard 资产摘要的续费、待决策、待取消/待迁移入口指向 `/asset-decisions`。
* [ ] 新页面至少有 happy path 与 PATCH 行为测试。
* [ ] `git diff --check`、lint、focused Vitest、build、`make verify-web` 通过。
* [ ] 分支通过 PR、CI 全绿后合并，并同步本地 `main`。

## Definition of Done

* Trellis task active context 已配置并启动。
* 代码和测试完成后本地质量门通过。
* 变更提交在非 main 分支。
* PR 创建后监控 CI；CI 绿后合并。
* 合并后同步本地 `main`，确认工作树干净。
* Trellis task 归档并记录工作日志。

## Technical Notes

* 计划锚点：`houfeng_codex_下一步开发计划.md` Task 8 的“决策页”与第一阶段续费决策入口。
* 现有 API helper：`listSubscriptions(filter)`、`listVPSAssets(filter)`、`updateVPSAsset(vpsId, input)`。
* 现有类型：`SubscriptionRecord`、`VPSAssetRecord`、`VPSRenewalDecision`、`VPS_RENEWAL_DECISION_LABELS`。
* 相关页面：`DashboardPage`、`VPSPage`、`SubscriptionsPage`、`VPSDetailPage`。
* 前端规范：
  * `.trellis/spec/web/state-and-data.md`
  * `.trellis/spec/web/component-conventions.md`
  * `.trellis/spec/web/styling-guidelines.md`
  * `.trellis/spec/web/quality-guidelines.md`
  * `.trellis/spec/guides/branch-workflow-governance.md`

## Execution Notes

* 本任务按用户要求不使用 subagent。
* 真实数据导入问题继续保持 deferred，不在本任务处理。
