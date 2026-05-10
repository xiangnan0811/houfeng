# Asset current-state edit workflows

## Goal

补齐 Asset Ledger 已有 PATCH 后端对应的前端编辑入口，让服务商、订阅和 VPS 基础事实不再只能创建后只读。完成后，用户可以在 UI 内维护资产当前状态，并触发已落地的价格、IP、规格与续费决策历史记录。

## What I already know

* `houfeng_codex_下一步开发计划.md` 的资产台账阶段已经完成 Provider/VPS/Subscription 后端、Node 关联、Dashboard 摘要、历史记录与 VPS 详情操作闭环。
* 用户已明确暂不处理真实 40+ VPS 数据导入/验证，本任务不执行真实数据 dry-run/import。
* 当前 `web/src/pages/ProvidersPage.tsx` 只有创建与列表，没有编辑入口；`web/src/lib/api.ts` 也没有 `updateProvider` helper。
* 当前 `web/src/pages/SubscriptionsPage.tsx` 只有创建、列表、筛选，没有订阅编辑入口；`api.ts` 没有 `updateSubscription` helper。
* 当前 `web/src/pages/VPSDetailPage.tsx` 已有续费决策和 Node 关联/解除，但基础事实区仍只读；`api.ts` 已有 `updateVPSAsset`。
* Provider/Subscription/VPS 类型都集中在 `web/src/lib/types.ts`，字段保持后端 JSON snake_case。

## Scope

### In Scope

* 在 `web/src/lib/types.ts` 增加 Provider/Subscription update input 类型，复用现有 snake_case contract。
* 在 `web/src/lib/api.ts` 增加 `PATCH /api/providers/:id` 与 `PATCH /api/subscriptions/:id` helper。
* `ProvidersPage` 增加按行编辑入口，支持维护名称、网站、面板地址、账号提示、国家/地区、评分、标签和备注。
* `SubscriptionsPage` 增加按行编辑入口，支持维护价格、币种、计费周期、计费月数、开始日期、续费日期、自动续费、自动续费已取消、状态、支付方式和备注；保存后展示后端返回的 `monthly_price`。
* `VPSDetailPage` 增加基础事实编辑入口，支持维护显示名、服务商快照/位置、产品/订单/机房、IP、SSH、OS/虚拟化、生命周期、用途、重要性、标签和备注；保存后刷新详情和 timeline。
* 更新 API 与页面测试，覆盖 PATCH 请求体、错误/本地校验、保存后 UI 刷新与历史刷新行为。

### Out of Scope

* 不执行真实 VPS JSON 数据导入、dry-run 验证或真实生产数据写入。
* 不新增删除、归档、批量编辑、恢复或审计审批流。
* 不改后端 API contract，除非实现中发现前端无法调用现有 PATCH contract。
* 不引入新的状态管理库、表单库、UI 库或端到端测试框架。
* 不做 Provider API 同步、DNS/Web SSH/service discovery。
* 不把 Dashboard 扩展成资产明细页面。

## Requirements

* 新增业务请求必须经过 `web/src/lib/api.ts`，页面不得直接 `fetch()`。
* 新增请求/响应类型必须集中在 `web/src/lib/types.ts`，字段保持 snake_case。
* 页面继续使用现有 loading/error/local state 模式，不引入 React Query/Zustand 等依赖。
* 表单校验至少覆盖必填名称、非负价格、正整数计费月数、3 位币种、合法评分、正整数 SSH 端口。
* 保存成功后必须用后端返回/重新拉取的数据更新页面，不用前端自行推导 `monthly_price` 或 timeline。
* UI 文案以中文为主，样式使用现有 `page-panel`、`asset-table`、`asset-operation-form` 等 BEM/令牌体系。

## Acceptance Criteria

* [ ] Provider 行可以进入编辑状态并提交 PATCH；保存后列表展示后端返回的新值。
* [ ] Subscription 行可以进入编辑状态并提交 PATCH；保存后列表展示后端返回的价格与月付折算。
* [ ] VPS 详情基础事实可以编辑并提交 PATCH；保存后详情和资产历史 timeline 重新加载。
* [ ] 校验失败时不发 PATCH，并在当前表单区域显示中文错误。
* [ ] API helper tests 覆盖 `updateProvider`、`updateSubscription` 与既有 `updateVPSAsset` 请求形态。
* [ ] Page tests 覆盖 Provider、Subscription、VPS 基础事实编辑 happy path。
* [ ] `cd web && npm run lint` 通过。
* [ ] `cd web && npm run test -- --run` 通过。
* [ ] `cd web && npm run build` 通过。
* [ ] `make verify-web` 通过。
* [ ] 分支通过 PR、CI 全绿后合并，并同步本地 `main`。

## Definition of Done

* Trellis task active context 已配置并启动。
* 代码和测试完成后本地质量门通过。
* 变更提交在非 main 分支。
* PR 创建后监控 CI；CI 绿后合并。
* 合并后同步本地 `main`，确认工作树干净。
* Trellis task 归档并记录工作日志。

## Technical Notes

* 前端规范读取：
  * `.trellis/spec/web/state-and-data.md`
  * `.trellis/spec/web/component-conventions.md`
  * `.trellis/spec/web/styling-guidelines.md`
  * `.trellis/spec/web/quality-guidelines.md`
  * `.trellis/spec/guides/branch-workflow-governance.md`
* 主要文件预计：
  * `web/src/lib/types.ts`
  * `web/src/lib/api.ts`
  * `web/src/lib/api.test.ts`
  * `web/src/pages/ProvidersPage.tsx`
  * `web/src/pages/ProvidersPage.test.tsx`
  * `web/src/pages/SubscriptionsPage.tsx`
  * `web/src/pages/SubscriptionsPage.test.tsx`
  * `web/src/pages/VPSDetailPage.tsx`
  * `web/src/pages/VPSDetailPage.test.tsx`
  * `web/src/styles/pages.css`

## Execution Notes

* 本任务按用户要求不使用 subagent；实现与检查均由主会话直接完成。
* 真实数据导入问题继续保持 deferred，不在本任务处理。
