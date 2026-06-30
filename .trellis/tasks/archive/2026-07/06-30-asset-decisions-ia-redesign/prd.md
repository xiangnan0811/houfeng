# 重构资产组合决策页面信息架构

## Goal

把 `/asset-decisions` 从三屏平铺、说明文字过多、主次混乱的页面，重构为组合优先的资产决策工作台。首屏必须让操作者迅速判断“现在最该处理哪一组资产”，并把记录、场景、续费证据和单台队列降为次级入口。

## Confirmed Facts

- 当前实现集中在 `web/src/pages/AssetDecisionsPage.tsx`，约 6018 行，混合数据获取、URL 状态、决策合成、表格列、弹层、记录、场景和单台队列。
- 当前测试 `web/src/pages/AssetDecisionsPage.test.tsx` 已固化旧页面结构，显式期待“决策路径”“下一步导览”“场景与记录”“续费证据区”“单台待处理队列”等同页常驻模块。
- 当前后端 `/api/asset-decisions/*` 已提供自动组、手动组、场景模板、保存记录、执行回读、执行编排等数据；本次默认不需要改 API 或数据库。
- 项目前端规范要求复杂工作台/资产决策类 UI 开发前先做可浏览器查看的 mockup，并经过浏览器 sanity 验收。
- 所有实施阶段必须在同一条分支 `ux/asset-decisions-ia-redesign` 下完成。

## Requirements

- 首屏采用“组合优先”信息架构：紧凑标题、视图/续费窗口筛选、一个主工作卡、自动组扫描列表和轻量状态摘要。
- 删除或降级教学式解释内容，特别是首页常驻的四步决策路径和大段说明文案。
- 自动决策组列表成为主扫描路径；记录、场景模板/自定义组合、续费证据、单台队列改为次级入口，不再三屏同权平铺。
- 次级工作区必须是用户触发或深链触发的单一展开区；默认首屏不展开记录、场景、续费证据或单台队列，也不把这些区域的表格和解释文案保留在 DOM 主扫描流里。
- `record_id`、`manual_group_id`、`template_id`、`view=renewal`、legacy `view=single_queue` 等深链必须自动打开对应次级工作区或弹层入口；关闭弹层不得清掉用户原本的筛选上下文。
- 保持现有 URL 合同和主要工作流：`view`、`renew_within_days`、`group_id`、`manual_group_id`、`record_id`、`template_id`、legacy `view=single_queue`。
- 保持现有业务写路径：打开自动组、创建自定义组合、保存记录、从模板创建组合、记录跟进、单台续费决策 PATCH。
- 组件拆分必须降低 `AssetDecisionsPage.tsx` 的职责密度；新组件保持纯展示/受控，不直接调用 API。
- 样式沿用当前 `web/src/index.css` 和 `asset-decision-*` BEM 命名，不引入 CSS 框架、新状态库或 e2e 依赖。
- 如果 API 不改，不能修改 Go contract、数据库迁移或后端行为。
- 文案不得暗示已有受控迁移闭环；只允许“迁移意向”“人工跟进”等现有能力表达。

## Acceptance Criteria

- [ ] 1440x1000 首屏能看见主工作项、当前筛选、自动组扫描列表入口和核心 CTA。
- [ ] 默认首页不渲染旧主模块标题“决策路径”“场景与记录”“续费证据区”“单台待处理队列”“下一步导览”；相关能力只能在主工作卡、自动组扫描或次级工作区中低权重承接。
- [ ] 记录、场景、续费证据和单台队列仍可进入并完成既有操作，但不再作为首屏同级模块铺开。
- [ ] `view=single_queue` legacy URL 被承接到单台辅助入口，并继续使用组合优先的页面框架。
- [ ] `view=renewal` 打开续费证据次级区；`record_id` 打开记录区；`manual_group_id` / `template_id` 打开场景区；这些深链在完成对应弹层读取后仍保留视图和续费窗口参数。
- [ ] `group_id`、`manual_group_id`、`record_id`、`template_id` 深链打开对应弹层。
- [ ] 部分接口失败时只显示局部错误或降级状态，不发明“闭环稳定”等过度结论。
- [ ] `cd web && npm run lint` 通过。
- [ ] `cd web && npm run test -- --run AssetDecisionsPage.test.tsx` 通过。
- [ ] `cd web && npm run build` 通过。
- [ ] `/asset-decisions` 使用 `mock-api asset-workflows` 完成 1440x1000 和 390x900 浏览器 sanity；若本机缺 Playwright，必须记录阻塞原因并用可用浏览器/截图做人工审查。

## Out of Scope

- 不重写资产决策后端算法。
- 不新增数据库迁移。
- 不引入 React Query、Redux、Tailwind、Playwright/Cypress 依赖或截图回归框架。
- 不改变 VPS 详情取消/退役工作台的危险操作边界。
