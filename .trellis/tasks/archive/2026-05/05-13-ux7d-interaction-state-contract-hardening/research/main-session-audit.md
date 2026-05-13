# UX-7D main-session audit

> 说明：用户明确要求本轮不使用 subagent。本文件记录主会话代码审查结论，替代 Trellis research 子代理输出。

## 读取的规范与权威

- `.trellis/spec/web/component-conventions.md`
  - route/detail/list 的 loading/error/empty 状态优先复用 `PageState`。
  - 组件保持命名导出，普通 page/component 不预先抽象。
  - modal/drawer focus 行为复用既有 `Drawer` / `useModalFocus`。
- `.trellis/spec/web/state-and-data.md`
  - VPS inventory URL-state 必须承接 Dashboard 深链，并通过 tab/chip/drawer 可见。
  - Asset Decisions 的主 surface 是统一工作队列，决策编辑在 drawer 中完成。
  - VPS 列表不得从列表 contract 推导 linked node health；订阅读取失败不得误报为真实缺订阅。
- `.trellis/spec/web/styling-guidelines.md`
  - 新样式只能落 `pages.css` / `atoms.css`，使用 token 和 BEM。
  - 页面不要复制裸 `page-panel` loading/error；空态需要 v2 装饰时使用 `PageState surface="empty"`。
- `.trellis/spec/web/quality-guidelines.md`
  - 质量门为 `make verify-web`；本地 Vitest 使用 repo-local `TMPDIR`。
  - 不新增 Playwright/Cypress/e2e 依赖。
- `docs/release/ui-evolution-roadmap.md`
  - UX-7D 当前推荐切片是 Interaction state contract hardening。

## 现状发现

### 已经较稳的部分

- `VPSPage.test.tsx` 已覆盖 quick views、drawer apply、chip 移除、订阅证据失败不误报缺订阅、缺订阅证据 ready 后才显示缺订阅。
- `AssetDecisionsPage.test.tsx` 已覆盖统一队列、续费窗口 reload、保存后队列移动，以及全量订阅证据失败时不误报缺订阅。
- `EventsPage.test.tsx` 已覆盖 drawer close without applying draft、reset、非法 URL 参数、filter apply 等交互，不需要在 UX-7D 重复展开。
- Nodes / Targets 已有 URL-state、clear-all 和 empty-filter lead 测试，UX-7D 不需要把已稳定观测页重新作为主实现对象。
- `Drawer.test.tsx` 已覆盖 portal、overlay close、close button、Escape、focus restore、Tab containment。

### 缺口

- `VPSPage.tsx` 主表区域仍用裸 `<div className="empty-state">正在加载 VPS…</div>` 和错误文本。
- `AssetDecisionsPage.tsx` 队列 loading/error/empty 仍是裸 `empty-state`；`AssetDecisionRenewalTable.tsx` 的续费候选 loading/error 也是裸状态。
- `ProvidersPage.tsx` / `SubscriptionsPage.tsx` 主表 loading/error 仍是裸 `empty-state`，这些页面属于真实数据验证入口。
- `DataTable.tsx` 可点击行没有统一屏蔽 interactive descendant 的冒泡。Nodes/Targets 用 action cell 手写 stopPropagation，但 VPS/未来页面容易遗漏。
- `AssetDecisionsPage.tsx` 的自绘队列只有详情 link 和处理 button，鼠标整行不可导航；同时 action 是否冒泡没有可测试合同。
- `VPSPage.test.tsx` 缺少 drawer close/ESC/overlay 不提交 draft 的显式测试。
- `AssetDecisionsPage.test.tsx` 缺少处理 drawer cancel/ESC/overlay 不 PATCH 的显式测试。

## 本轮取舍

- 处理资产主路径的局部状态，暂不碰 detail 子组件和 watchtower 历史抽屉的局部空态。
- 不抽 `FilterState` / `DrawerState` helper；当前只有 VPS 的缺口需要补，重复尚不足以证明抽象收益。
- 可以在 `DataTable` 做小型通用 guard，因为可点击行 + action cell 已经横跨 VPS、Nodes、Targets，是稳定模式。
- Asset Decisions 队列是自绘 `<li>`，不能复用 `DataTable` guard；本页局部实现鼠标 row click 和 action stopPropagation，键盘入口保留为可见的详情 link / 处理 button，避免把 role=link 容器套在交互元素外层。
