# UX-1 App Shell / Navigation / Visual Baseline

## Goal

把候风的应用壳层从“功能正确”推进到稳定、克制、高密度、工程师长期使用友好的 v2 产品基线。范围聚焦 AppShell、Sidebar、TopBar、GlobalSearch、Breadcrumb、SyncStatus、UserChip 与页面通用 chrome，不重做业务页面主体。

## What I Already Know

- 用户已同意从 `docs/release/ui-evolution-roadmap.md` 的 UX-1 开始推进。
- 当前 `main` 已合并 PR #50，UI 路线文档明确 UX-1 是下一批实现入口。
- 当前 AppShell 已具备分组导航、工作台命名、Dashboard-backed shell summary 和异常 count；不是从零重写。
- 当前 `layout.css` 存在“Premium Glassmorphism”倾向：260px sidebar、较强玻璃/动效、sans 品牌字、部分硬编码 rgba，与 v2 component spec 中 232px sidebar、serif 品牌、弱 hairline、克制密度不完全一致。
- 当前 Breadcrumb 只识别 `nodes` / `targets` / `events` / `settings`；资产路由如 `/vps/:id` 没有 Breadcrumb 语义。
- 用户要求本轮不使用 subagent / 子代理，所以实现与检查在主会话完成。

## Requirements

- Sidebar 宽度、品牌、分组、active 状态、异常 count、底部 SyncStatus/UserChip 对齐 v2 壳层契约。
- TopBar、GlobalSearch、Breadcrumb 与页面主体形成稳定层级，不能挤压首屏工作内容。
- Breadcrumb 覆盖资产主路径：`/vps/:id`、`/providers`、`/subscriptions`、`/asset-decisions`，并继续避免一级路由重复 sidebar 链接。
- GlobalSearch 在桌面和窄屏都不能把 menu 撑出视口；搜索输入保留快捷键提示，但不要作为页面说明文案。
- 页面主区域宽度、padding、sticky top bar、背景 aurora 强度要更克制，减少“玻璃卡片堆叠”感。
- 暗色主题为默认视觉基线，浅色主题不能明显退化。
- 所有样式走现有 CSS tokens / BEM；不引入 CSS 框架、图标库、图表库或视觉回归依赖。
- 用户可见 UI 变化必须给出本地 preview URL、检查路由、视口和限制说明。

## Implementation Scope

- `web/src/app/layout/layout.css`
- `web/src/app/layout/Breadcrumb.tsx`
- `web/src/app/layout/Breadcrumb.test.tsx`
- `web/src/app/layout/Sidebar.test.tsx`
- `web/src/app/layout/GlobalSearch.test.tsx` if behavior/markup changes require it
- Optional small doc update if a new shell contract is worth recording after implementation

## Acceptance Criteria

- [ ] Sidebar visually follows v2 shell contract: stable 232px desktop rail, serif brand, grouped nav, neutral count badges, compact bottom status/user area.
- [ ] 1440x1000 viewport: first viewport has a clear work area and the shell does not dominate the page.
- [ ] 390x900 viewport: sidebar/nav/search/menu do not overflow incoherently; text does not overlap controls or badges.
- [ ] Breadcrumb supports nested asset routes and remains hidden on root/level-1 routes to avoid duplicate sidebar links.
- [ ] GlobalSearch menu uses viewport-safe sizing and long labels/hints truncate instead of expanding the shell.
- [ ] Shell summary copy continues to identify dashboard as the source; no fake `center ok` / `sync HH:mm:ss` semantics.
- [ ] Existing layout tests pass and new/updated tests cover Breadcrumb asset routes and sidebar shell contract.
- [ ] Local validation passes: `cd web && npm run lint`, `cd web && TMPDIR=$PWD/.tmp npm run test -- --run`, `cd web && npm run build`.
- [ ] Visual sanity performed with dev server for `/`, `/vps`, `/asset-decisions`, `/nodes`, `/targets`, `/events`, `/settings` at `1440x1000` and `390x900`.

## Definition Of Done

- Work commit contains implementation and tests.
- Trellis task archived and journal recorded after verification.
- PR created, PR CI green, merged, and local `main` synced.
- Main CI monitored after merge.

## Out Of Scope

- No backend/API changes.
- No Dashboard command surface redesign beyond shell/chrome interactions.
- No VPS/AssetDecisions/Nodes/Targets/Events page body redesign.
- No real data import.
- No Provider/DNS/Web SSH/plugin/RBAC expansion.
- No new visual regression framework or browser automation dependency.
- No mechanical long-page component splitting.

## Technical Notes

- UI route authority: `docs/release/ui-evolution-roadmap.md`.
- v2 visual authority: `docs/design/v2-houfeng/design-language.md`, `docs/design/v2-houfeng/component-spec.md`.
- Active visual evidence workflow: `docs/operations/v2-visual-evidence.md`.
- AppShell summary contract: `.trellis/spec/web/state-and-data.md` Dashboard / AppShell section.
- Styling constraints: `.trellis/spec/web/styling-guidelines.md`.
- Component conventions: `.trellis/spec/web/component-conventions.md`.
