# 全站 UI 主题与布局质量排查修复

## Goal

修复生产部署中暴露的全站 UI 主题与布局质量问题，尤其是浅色主题下右侧 Drawer 仍呈深色、内容不可读、创建节点表单缺少结构化布局的问题；本任务不是局部补丁，而是一次覆盖已登录主页面、Drawer、表单、表格、弹窗/交互面板的全面排查与修复。

## What I already know

- 生产部署已经可登录，进入系统后新建第一个节点时右侧抽屉漆黑一片，浅色主题下仍为暗色。
- 用户要求“全面、全面、还是全面排查”，认为问题可能不止新建节点抽屉。
- 当前前端路由覆盖：Dashboard、VPS、VPS 详情、Providers、Subscriptions、Asset Decisions、Nodes、Node Compare、Node Detail、Node Onboarding、Targets、Target Detail、Events、Settings、Login。
- Drawer 组件通过 portal 挂载到 `document.body`，样式集中在 `web/src/styles/atoms.css`。
- 初步根因：`.drawer` 直接硬编码 `rgba(10, 10, 15, 0.85)` 和白色边框，没有使用主题 token；因此浅色主题下 Drawer 不跟随 `html.theme-houfeng-light`。
- 初步根因：`CreateNodeDrawer` 表单直接使用裸 `<form><p><label><input>` 堆叠，没有使用已有 Drawer/form 布局模式；`CreateTargetPanel` 已有更完整的 `target-create-drawer__*` 结构可参考。
- 设计规范要求 light 主题“不是次要主题”，必须同等可用；不在 token 之外硬编码颜色。

## Requirements

- 所有 Drawer 必须使用主题 token 渲染背景、边框、文字、遮罩和焦点状态，浅色/深色主题都可读。
- 新建节点 Drawer 必须具备明确的信息层级、字段分组、说明文案、错误态和操作区，不得只是裸输入框堆叠。
- 全站已登录主页面必须逐页排查：页面骨架、表格、卡片、表单、筛选、详情区、危险区、空态、错误态、Drawer/Modal 在浅色/深色主题下不能出现不可读或明显未设计状态。
- 验收覆盖默认按“全站 UI 质量门禁”执行：每个已登录路由在 houfeng light/dark 下逐页打开，并触发该页主要 Drawer/Modal/Form/Table。
- 排查中发现的同类主题/布局缺陷必须在本任务内一并修复；只有需要真实生产数据且本地无法构造的页面或状态可以记录为未覆盖。
- 修复应优先复用现有 atoms、CSS token 和 v2 设计语言，不引入 Tailwind/CSS-in-JS/新 UI 库。
- 视觉修复必须保持当前产品定位：高密度、工程工具、中文主界面、dark-first 但 light 同等可用。

## Acceptance Criteria

- [ ] `CreateNodeDrawer` 在 houfeng light、houfeng dark 下背景、文本、输入框、按钮、错误信息都清晰可读。
- [ ] 所有 `<Drawer>` 使用点在 light/dark 下不再出现硬编码暗色面板导致的不可辨认问题。
- [ ] 新建节点流程从 Nodes 页面打开 Drawer、填写必填项、提交到接入工作台的黄金路径可用。
- [ ] Events 筛选 Drawer、VPS 创建/筛选 Drawer、VPS 详情 Drawer、Asset Decisions Drawer、Target 创建 Drawer、Node Detail 历史/命令 Drawer、Target Detail 历史 Drawer 至少完成一次视觉/可读性检查。
- [ ] 所有已登录路由主页面完成 houfeng light 与 houfeng dark 双主题视觉扫查：`/`、`/vps`、`/providers`、`/subscriptions`、`/asset-decisions`、`/nodes`、`/nodes/compare`、`/targets`、`/events`、`/settings`，以及可进入的详情/接入页面。
- [ ] 主要 Modal/Form/Table 状态完成检查：空态、加载态、错误态、筛选态、提交中/禁用态、危险操作区无不可读或明显未设计状态。
- [ ] 前端 lint、test、build 通过。
- [ ] 本地浏览器验证完成；若无法验证某些需要真实数据的页面，需明确记录未覆盖原因和替代验证。

## Definition of Done

- Tests added/updated where behavior or structure changes can be covered.
- `cd web && npm run lint`, `cd web && npm run test`, `cd web && npm run build` pass.
- Dev server/browser manual verification covers logged-in golden path and affected UI surfaces.
- No new dependency or design system introduced.
- Any discovered durable UI convention worth preserving is captured via Trellis spec update after implementation.

## Research References

- [`research/drawer-form-surfaces.md`](research/drawer-form-surfaces.md) — catalogs every production Drawer surface and identifies Drawer/form layout and theme risks.
- [`research/route-verification-matrix.md`](research/route-verification-matrix.md) — maps all logged-in routes to browser verification surfaces and known page states.
- [`research/theme-hardcode-risks.md`](research/theme-hardcode-risks.md) — inventories hardcoded color/token bypass risks and likely light-theme break points.

## Technical Approach

- Fix shared primitives first: make the Drawer atom token-driven and structurally columnar so every Drawer inherits the active theme even though it portals to `document.body`.
- Normalize Drawer-hosted forms where risks were identified: start with `CreateNodeDrawer`, then inspect VPS detail operation forms and any raw controls that rely on page-scoped CSS.
- Address high-risk token bypasses discovered in `atoms.css` / `pages.css` when they affect shared surfaces, while avoiding broad redesign or new dependencies.
- Verify through tests/build and browser sweep rather than relying on jsdom style assertions.

## Decision (ADR-lite)

**Context**: A production deployment surfaced a light-theme Drawer that stayed dark and an unstructured node-create form, indicating systemic theme/layout drift.
**Decision**: Treat this as a full logged-in UI quality gate, not a local node-create patch: shared primitives, affected Drawer/form surfaces, and every logged-in route must be checked in houfeng light/dark.
**Consequences**: Scope is larger than the initial bug, but it reduces the chance of the user finding the same class of issue while continuing deployment testing.

## Out of Scope

- 不重做整体信息架构或导航。
- 不引入新的 UI 框架、图表库、CSS-in-JS 或 Tailwind。
- 不把 dark-first 改成 light-first；只确保 light 主题同等可用。
- 不修改后端业务语义，除非前端验证暴露出必须修复的 API 合约问题。

## Technical Notes

- `web/src/components/atoms/Drawer.tsx`：portal Drawer 实现，负责语义与关闭行为。
- `web/src/styles/atoms.css`：当前 `.drawer` 硬编码深色背景和白色边框，是浅色主题失效的直接候选根因。
- `web/src/pages/nodes/CreateNodeDrawer.tsx`：创建节点表单缺少结构化 Drawer/form class。
- `web/src/pages/targets/CreateTargetPanel.tsx` 与 `web/src/styles/pages.css` 中 `target-create-drawer__*` 可作为已有结构化表单参考。
- `web/src/styles/tokens.css`：主题 token 定义，`html.theme-houfeng-light` 已提供 light surface/text/border/control token。
- `docs/design/v2-houfeng/design-language.md`：light 主题同等可用、禁止 token 外硬编码颜色。
- `docs/design/v2-houfeng/component-spec.md`：Drawer 行为和页面级 Drawer 使用约束。
