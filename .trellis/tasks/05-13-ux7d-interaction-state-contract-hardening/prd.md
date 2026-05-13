# UX-7D interaction state contract hardening

## 背景

UX-7C 已经把高影响 route/detail/list 的 loading、error、empty 状态收敛到 `PageState`。UX-7D 继续处理真实数据验证前最容易影响操作手感的交互合同：URL-state 是否可见且可清除、Drawer draft/apply/cancel 是否可靠、行点击与行内 action 是否互不干扰，以及资产主路径局部状态是否仍然回退到裸 `empty-state`。

本轮不重写页面信息架构，也不引入 e2e/Playwright/Cypress 依赖；测试仍以 Vitest/jsdom 覆盖可验证的交互合同。

## 目标

1. 资产主路径页面的局部 loading/error/empty 状态更一致，优先复用 `PageState`。
2. VPS 高级筛选 drawer 的 draft、apply、reset、close 行为有回归测试：关闭/ESC/overlay 不写 URL，不触发额外请求；应用/重置才改变 visible state。
3. Asset Decisions 的处理 drawer 有回归测试：draft 从所选 VPS 初始化，取消/ESC/overlay 不提交，保存才 PATCH。
4. 行点击和行内 action 的交互合同更清楚：可点击行由 `DataTable` 防止按钮/链接冒泡；Asset Decision 自绘队列支持鼠标整行进入详情，键盘用户使用可见的详情/处理 action，详情/处理 action 不触发行导航。
5. 更新 UI 演进路线，记录 UX-7D 本轮完成范围和仍然有意保留的非目标。

## 范围

- `web/src/components/PageState.tsx` 既有 primitive 复用，不新增新视觉框架。
- `web/src/components/AssetDecisionRenewalTable.tsx` 的加载/错误状态迁到 `PageState surface="empty"`。
- `web/src/pages/VPSPage.tsx`、`ProvidersPage.tsx`、`SubscriptionsPage.tsx` 的主表局部加载/错误状态迁到 `PageState surface="empty"`。
- `web/src/components/atoms/DataTable.tsx` 在可点击行中屏蔽 interactive descendant 冒泡，避免每个 action cell 手写 `stopPropagation`。
- `web/src/pages/AssetDecisionsPage.tsx` 的自绘队列行补齐 row click / keyboard navigation，并让详情 link、处理 button 明确 stopPropagation。
- 针对上述行为补充 focused Vitest。
- `docs/release/ui-evolution-roadmap.md` 更新 UX-7D 状态与后续建议。

## 非目标

- 不新增后端字段或改变 API contract。
- 不展示列表 API 未提供的 linked node health / subscription certainty。
- 不把所有历史 `.empty-state` 一次性替换完；detail 子组件和 watchtower 局部历史状态留给后续具体任务。
- 不拆分 VPS、Asset Decisions、Providers、Subscriptions 大页面结构。
- 不引入正式 e2e、视觉回归或新状态管理依赖。

## 验收

- VPS drawer close / ESC / overlay 不改变 applied URL-state，不重新请求数据；apply/reset 行为可测。
- Asset Decisions drawer cancel / ESC / overlay 不 PATCH，重新打开时 draft 仍从所选 VPS 当前决策初始化。
- VPS DataTable 行点击仍导航；行内 action 不冒泡的合同在 DataTable 单测中覆盖。
- Asset Decisions 队列行可点击进入详情；键盘焦点落到可见 action 时保留在 action 上，详情 link 和处理 button 不触发整行导航。
- Providers / Subscriptions / VPS / Asset Decisions 的主路径 loading/error 状态使用 `PageState`，测试仍能断言用户可见文本。
- 本地 `npm --prefix web run lint`、`TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- --run`、`npm --prefix web run build`、`TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest make verify-web` 通过，PR CI 全绿后合并。
