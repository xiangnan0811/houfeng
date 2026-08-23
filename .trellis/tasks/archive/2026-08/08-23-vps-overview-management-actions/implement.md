# VPS 概览管理操作实施计划

## Goal

以 TDD 将 overview 五个管理入口接入既有写入与生命周期合同，并保持 legacy
fallback、性能边界和可访问性。

## Phase 0: Start gate

- [x] 获得用户对本规划的后续明确实施批准。
- [x] 从届时最新 `origin/main` 建立/确认非 main 分支或 worktree，启用 hooks。
- [x] 运行 Trellis before-dev，读取 web/frontend/test 相关 specs 和 Node 22 要求。
- [x] 记录干净基线：focused tests、type-check/lint 和 `make verify-web` 当前结果。

## Phase 1: RED — route and panel ownership

- [x] 扩展 `VPSDetailPage.test.tsx`：overview 模式点击“管理 → 编辑事实”必须出现
  真实 facts loading/form；当前空 callback/placeholder 应失败。
- [x] 扩展 `VPSOverviewPageView.test.tsx`：展示层不再渲染管理占位文本。
- [x] 为 menu/controller 增加 selection 单 owner、Escape 与焦点行为 RED。
- [x] 运行 focused Vitest，保存预期失败证据。

## Phase 2: Minimal shell GREEN

- [x] 新建 `VPSOverviewManagementActions`，从 route 接收 `vpsId`、panel 和 refresh。
- [x] 移除 `onManagePanel` 空 contract 与 placeholder；避免双重 `openPanel`。
- [x] 实现 detail 按需读取、loading/error/retry、request generation 和关闭清理。
- [x] 挂载现有 Modal/ActionConfirmationModal，补齐最小样式。
- [x] 跑 Phase 1 focused tests、type-check 与 lint 至 GREEN。

## Phase 3: RED/GREEN — facts, decision, subscription

- [x] 分别先写失败测试，证明正确 API/payload、单次提交、错误保留 draft、成功
  refresh 和可感知反馈。
- [x] facts 复用 `detailToFactEditForm` / `buildFactEditInput`，providers 只在打开
  facts 时加载。
- [x] decision 复用现有 decision/linkage helper，禁止 unchanged submit。
- [x] subscription 复用 `INITIAL_SUBSCRIPTION_DRAFT` / `buildSubscriptionInput`。
- [x] 必要 shared helper 从 Legacy 做小步抽取，每次抽取后运行相关 Legacy tests；
  不做无关重构。

## Phase 4: RED/GREEN — cancellation safety

- [x] 测试 preview loading/failure/retry、blockers、active subscription 选择、
  submitting 去重和现有 workbench payload。
- [x] 测试 mutation 成功后 result 保留、preview/overview 刷新；刷新失败与写失败
  使用不同反馈。
- [x] 接入 `getVPSCancellationPreview`、`VPSCancellationWorkbench` 和
  `applyVPSCancellation`，保持服务端 preview authority。
- [x] 运行 workbench 现有回归测试和 overview focused tests。

## Phase 5: RED/GREEN — archive safety

- [x] 测试 review loading/failure、ineligible/blockers、展示名不匹配、重复提交。
- [x] 测试只有 eligible + 无 blockers + 完整名称时调用 `archiveVPS`，成功 replace
  导航 `/archive/:id`。
- [x] 接入现有 archive review/copy/confirmation 合同；失败关闭，不从 overview
  identity 推断资格。

## Phase 6: Compatibility and quality

- [x] 运行 route gate tests，证明 capability off/503 仍 lazy fallback，404/500
  语义不变，overview 首屏不静态导入 `LegacyVPSDetail`。
- [x] 运行 `LegacyVPSDetail.test.tsx` 相关及完整测试，证明 helper 抽取无回归。
- [x] 用 Node 22 运行 focused Vitest：
  `npm --prefix web run test -- --run src/pages/VPSDetailPage.test.tsx
  src/pages/vps-detail/VPSOverviewManagementActions.test.tsx
  src/pages/vps-detail/VPSOverviewPageView.test.tsx
  src/pages/vps-detail/hooks/useVPSManagementController.test.tsx
  src/components/VPSCancellationWorkbench.test.tsx`。
- [x] 运行 `npm --prefix web run lint`、`npm --prefix web run build`、相关完整
  Vitest/Playwright 套件和 `make verify-web`；`make verify-web` 会执行 toolchain、
  lint、coverage、production build、bundle budget 与 CSS analysis。
- [x] 浏览器验证 desktop 与 390px：五个入口、错误/成功、取消/归档安全流、无
  页面级横向溢出。
- [x] 验证 Tab/Enter/Space/Escape、modal focus enter/return、44px targets 和 Axe。
- [x] 运行 `git diff --check`，复核完整 diff 仅包含批准范围。

## Phase 7: Review and delivery

- [x] 执行独立代码审查，按 Critical/Important/Minor 清零实质问题。
- [x] 将测试、浏览器、可访问性和 non-regression 证据写回 child。
- [x] 提交非 main 分支、push、创建 PR，等待 required CI；失败在同一分支修复。
- [x] 合入 protected main 后等待 main CI，并在选定提交复跑必要 smoke/focused gate。
- [x] 填写 commit/PR/CI 证据并归档本 child；只有此后才解锁最终审计 child。

## Stop conditions

- 现有 API 无法满足任一 panel，需新增 backend/权限/迁移；
- cancellation/archive 需要削弱既有 preview、blocker 或确认合同；
- 为复用必须把整个 Legacy 页面装入 overview，或产生明显首屏 bundle 回归；
- 无法区分写入失败与写后 refresh 失败；
- 任何永久删除、activity、comparison 或性能基准改动进入 diff。
