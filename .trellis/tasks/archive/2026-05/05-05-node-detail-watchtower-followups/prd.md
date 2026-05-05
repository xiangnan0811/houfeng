# 节点详情 watchtower 3 项 check 报告 follow-up

## Goal

清掉 trellis-check 报告中 watchtower 节点详情页的 3 个小负债（来自 session 29 的 check 报告 §I）。

## Background

commit `67cd668`（watchtower 任务）的 trellis-check 报告标注了 3 项"不阻塞 commit 但建议后续处理"的 follow-up：

1. **主页面冗余 IncidentList / EventList** — 同样的组件在主页面（当前异常 / 事件 DetailSections）+ 抽屉里双重渲染。PRD wireframe 暗示这两块应只在抽屉，但 implementer 选择保留以防业务回归
2. **身份条 sticky** — PRD 提的 "右上 sticky" 当前是 header-internal placement，不是 viewport-scroll-sticky。滚动时操作菜单/查看历史按钮随页面滚走
3. **危险区 wireframe 完整化** — 当前简化为 "摘要 + 异常计数 + 状态徽标"，PRD 提到的 "持续 X · 状态从 Y 升级为 Z" 暂未实现（PRD prose 已 sanction 简化，但可补 duration）

## Decision

- **Q-SCOPE** — **A（全部 3 项）** — 单 PR，半小时工作量

## Requirements

### #1 清理主页面冗余 IncidentList / EventList

删除 NodeDetailPage 主视图与折叠区之间的两个 DetailSections（"当前异常" / "事件"）。这两块的信息已在抽屉（事件 tab + 历史异常 tab）完整呈现。

同时删除对应测试断言（grep `当前异常` / `事件` 在主页面渲染部分的断言，确认不破坏其他用例）。

### #2 身份条 sticky

`.watchtower-header` 加：
```css
position: sticky;
top: 0;
z-index: 10;
background: var(--bg); /* 或 var(--surface) 避免下方 metric cards 穿透 */
```

`--bg` token 需确认存在（grep tokens.css）或直接用 `var(--bg)` / `var(--surface)`。

### #3 危险区补 duration

当前危险区：
```tsx
<p>共 <MonoDigits>{node.current_active_incident_count}</MonoDigits> 个活跃异常 · 健康状态 <StatusBadge ...></p>
```

补 duration（从第一个 incident 的 `started_at` 派生）：
- 需要读 incidents state 取最新一条的 `started_at`，计算 `Date.now() - started_at` 差值
- 或更简单：从 `active_incidents[0]?.started_at` 取，用 `<Timestamp mode="relative">` 显示。"持续 <Timestamp value={started_at} mode="relative" />"

## Out of Scope

- 其他页面的 follow-up
- 后端改动

## Technical Notes

- 改造文件：`web/src/pages/NodeDetailPage.tsx` + `NodeDetailPage.test.tsx` + `web/src/styles/pages.css`
- 参考：现有 incidents state / active_incidents 数组
