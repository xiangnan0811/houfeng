# Page State Audit

## Summary

当前页面三态已经有基础可用性，但 loading/error/empty 视觉和语义分散在多个 page/component 中。UX-7C 应优先抽取共享展示 primitive，再替换影响真实数据验证路径的高频 route/page 状态。

## Design Authority

- `docs/design/v2-houfeng/design-language.md` 第 7 节定义三态：
  - Loading：surface 底色、一行 mono “正在加载…”和时间戳；不使用 spinner / skeleton。
  - Error：warning/critical surface；中文说明、截断技术摘要、可重试时 retry action；不依赖 toast。
  - Empty：复用 `.empty-state`，需要小型单色装饰、解释文案和必要 CTA。
- `web/src/styles/pages.css` 已有 `.page-panel`、`.page-panel__eyebrow`、`.page-panel__title`、`.page-panel__description`、`.empty-state`，适合在其上扩展，不需要新增 CSS 文件。
- `web/src/app/RouteModuleFallback.tsx` 已经证明 route fallback 可以是纯展示 component + `page-panel route-module-fallback`。

## Current Repetition

Route/page loading/error 手写点：

- `web/src/pages/DashboardPage.tsx`：loading 返回裸 `page-panel` 文本；error 手写 page-panel。
- `web/src/pages/NodesPage.tsx`：loading/error 手写 page-panel。
- `web/src/pages/TargetsPage.tsx`：loading/error 手写 page-panel。
- `web/src/pages/EventsPage.tsx`：loading/error 手写 page-panel。
- `web/src/pages/SettingsPage.tsx`：loading/error 手写 page-panel。
- `web/src/pages/node-detail/NodeDetailLoading.tsx` / `NodeDetailUnavailable.tsx`：独立手写。
- `web/src/pages/target-detail/TargetDetailLoading.tsx` / `TargetDetailUnavailable.tsx`：独立手写。
- `web/src/pages/vps-detail/VPSDetailLoading.tsx` / `VPSDetailErrorPanel.tsx`：独立手写，VPS loading 还嵌了一层 `.empty-state`。

High-impact empty states:

- `web/src/pages/nodes/NodesListSection.tsx`：
  - baseNodes empty currently uses “暂无节点”；
  - filtered empty currently uses “没有匹配当前筛选的节点”。
- `web/src/pages/TargetsPage.tsx`：
  - no targets currently uses “当前还没有目标”；
  - filtered empty currently uses “没有匹配当前筛选的目标”。
- `web/src/components/EventList.tsx`：
  - empty event stream currently defaults to “最近没有状态变更事件”；
  - `EventsPage` evidence lead handles URL-state empty, but list empty should still align action/CTA.
- `web/src/components/target-detail/TargetProbeList.tsx`：
  - no ProbeItem currently uses “当前还没有 ProbeItem”。

## Recommended UX-7C MVP

1. Add `web/src/components/PageState.tsx`.
2. Add `web/src/components/PageState.test.tsx`.
3. Extend `web/src/styles/pages.css` with:
   - `.page-state`
   - `.page-state--loading`
   - `.page-state--error`
   - `.page-state--empty`
   - `.page-state--compact`
   - `.page-state__mark`
   - `.page-state__eyebrow`
   - `.page-state__title`
   - `.page-state__description`
   - `.page-state__summary`
   - `.page-state__actions`
4. Replace the high-impact loading/error states listed above.
5. Replace Nodes/Targets/Events/Probe list empty states where the empty state is central to the workflow.

## Constraints

- Keep shared component route-agnostic: pass `<Link>` or `<Button>` via `action`, do not import router into `PageState`.
- Do not change data fetching or URL-state logic.
- Keep existing visible strings when tests depend on them unless the PR intentionally updates them to v2 anchor wording.
- Use existing `.btn` classes or `Button` atom in page/components; `PageState` only renders the slot.
- Do not use inline SVG assets from outside the repo. A small inline SVG glyph in the component is acceptable because it is decorative and token-colored.

## Verification Notes

- Tests should assert user-visible behavior instead of CSS pixel values.
- Existing page tests may need expected text updates for empty anchors.
- Local Vitest should use repo-local temp dir:

```bash
TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- --run
```
