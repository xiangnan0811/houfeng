# VPS 概览管理操作闭环

## Goal

把新版 `/vps/:id` overview 管理菜单中的五个可见入口接入现有、已验证的 VPS
写入合同，使用户不必等待 capability fallback 或返回旧详情页，就能完成基础资料、
续费决策、订阅事实、取消/退役和归档操作。

## Current Problem

- `VPSManagementMenu` 已展示五个动作并切换 panel。
- `VPSDetailPage` 传入的 `onManagePanel` 是空函数。
- `VPSOverviewPageView` 只显示“写入动作由管理控制器持有”的占位状态。
- 真实表单、payload 转换、preview、blocker、确认和写接口仍只由
  `LegacyVPSDetail` 持有。

因此，具有 `records_v2_read` capability 的新页面能发现动作却不能执行，这是本轮
父任务归档前唯一需要关闭的产品遗留。

## Requirements

### R1. Five real actions

- `facts` 复用 `VPSFactsEditForm`、`detailToFactEditForm`、
  `buildFactEditInput` 和 `updateVPSAsset`；按需加载 providers。
- `decision` 复用 `VPSRenewalDecisionForm` 和既有 decision payload/linkage 语义；
  不允许提交未变化的决策。
- `subscription` 复用 `VPSSubscriptionForm`、`INITIAL_SUBSCRIPTION_DRAFT`、
  `buildSubscriptionInput` 和 `createVPSSubscription`。
- `cancellation` 在展示 workbench 前加载 `getVPSCancellationPreview`，复用
  `VPSCancellationWorkbench`、blockers/warnings 和 `applyVPSCancellation`；执行后
  刷新 preview 与 overview，并保留结果反馈。
- `archive` 在确认前加载 `getVPSArchiveReview`，保留 eligible/blockers、完整展示
  名称确认和服务端复检，调用 `archiveVPS` 后导航到 `/archive/:id`。

### R2. One focused mutation owner

- 新 overview route 持有专用管理 action host/controller；panel visibility 与实际
  loading/draft/submitting/error/success 状态有单一 owner。
- 按需读取 `getVPSAsset`，不得从 overview 的精简 identity 推造完整写入 detail。
- 复用或抽取 Legacy 现有纯转换器和领域辅助函数；不得复制一套 payload 校验、
  cancellation/archival 安全语义或导入整个 `LegacyVPSDetail` 作为新页面 modal。
- overview 是只读聚合接口；所有写操作继续使用现有资产/订阅/生命周期 API。

### R3. Refresh and feedback

- facts、decision、subscription 成功后等待 `commands.refresh`，再关闭 action panel；
  overview 上必须出现可感知的成功状态。
- 写入错误留在当前 action panel，用户输入不丢失，可直接重试。
- cancellation 成功后刷新 overview 和 cancellation preview，并展示审计步骤结果。
- refresh 失败不得把已经成功的写入误报为失败；必须区分“写入成功、概览刷新失败”。
- panel 关闭、路由参数变化和异步响应竞态不得把旧 VPS 的状态写入新 VPS 页面。

### R4. Compatibility and accessibility

- capability off / overview unavailable 时的 lazy legacy fallback 保持原样，且不把
  legacy bundle 引入 overview 首屏 chunk。
- modal、确认和菜单支持键盘操作、Escape、焦点进入/返回、可感知标题与错误；
  关键触控目标不小于 44px。
- desktop 与 390px 不出现不可访问的横向溢出；取消 workbench 使用现有大 modal
  布局。
- 保持中文主界面、现有 dark-first 视觉语言和危险动作层级。

### R5. Delivery boundaries

- 使用 TDD，先建立从“菜单可见但无动作”到五类行为的 RED。
- 通过非 main 分支、PR、required CI 和合入后 main 验证交付。
- 不修改后端 API、数据库迁移、权限模型、feature flag 默认值或永久删除 wiring，
  除非实施中发现现有前端合同无法使用并先返回重新评审。

## Acceptance Criteria

- [x] `AC-01` 在 overview capability 开启时，五个菜单项都打开真实表单/workbench/
  确认流，不再显示占位 `PageState` 或调用空 handler。
- [x] `AC-02` facts、decision、subscription 使用现有转换器和 API，成功后只产生
  一次预期写入，刷新 overview，并显示成功状态；错误可重试且草稿不丢失。
- [x] `AC-03` cancellation 必须先显示 preview、warnings/blockers，阻止项存在时
  不可绕过；成功后显示结果并刷新 preview/overview。
- [x] `AC-04` archive 必须等待资格 review、阻止 ineligible/blockers、要求完整展示
  名称；成功后导航 `/archive/:id`。
- [x] `AC-05` 没有复制领域校验或挂载整个 `LegacyVPSDetail`；legacy fallback 的
  capability/error 行为和异步 chunk 边界保持通过测试。
- [x] `AC-06` 异步请求在关闭 panel、卸载或切换 `vpsId` 后不会提交陈旧 UI 状态；
  重复点击在 submitting 期间不会重复写入。
- [x] `AC-07` focused Vitest 覆盖五类成功/失败/安全门禁，相关 legacy 回归、
  type-check、lint、web verify 与全量相关门禁通过。
- [x] `AC-08` desktop 和 390px 浏览器验证、键盘/Escape/焦点返回、44px 目标与 Axe
  验证通过，并留存可审计证据。
- [ ] `AC-09` PR required CI、protected-main 合入和合入后 main 验证均成功，证据
  回写 child 后才允许父收口任务进入最终审计。

## Out of Scope

- 运维记录永久删除或任何 deletion/recovery/backup adapter。
- activity group-granted digest、comparison sticky 行标题、mixed-load harness。
- 重写新版 overview 信息架构或 Legacy 详情页的其他抽屉。
- 新增 backend endpoint、迁移或权限范围。
- staging 数据清理、测试环境重建或把一次性部署转为正式多用户环境。

## Dependencies

- 父任务：`08-23-vps-records-parent-closeout`。
- 后续阻塞：`08-23-vps-records-final-audit-archive` 必须等待本 child 合入
  protected main 并完成合入后验证。
