# 前端 Shell 摘要与错误恢复

## Goal

让 AppShell 准确表达 Dashboard snapshot 的可用性与新鲜度，并为渲染错误、lazy chunk 失败和顶栏通知入口提供真实、可恢复的用户路径。

## Confirmed Facts

- AppShell 仅 mount 时请求一次 dashboard，却把结果展示为“系统正常”。
- Router 没有 `errorElement`，应用顶层没有 React error boundary。
- 顶栏通知按钮无 handler、route 或真实 count contract，只显示固定 0。
- `/events` 已支持 `notification_only` 布尔查询参数。

## Requirements

- Shell 只表达 loading、clear、anomaly、stale、unavailable 五种摘要状态，并显示后端生成时间。
- 页面重新可见或窗口 focus 时刷新；本任务不引入常驻轮询。
- route tree 与 Router 外层各有一层错误恢复，UI 不泄露异常对象。
- 通知入口链接至 `/events?notification_only=1`，删除虚假 badge。

## Dependency And Scope

- 依赖 `frontend-dashboard-trust` 合并，复用已澄清的 dashboard snapshot contract。
- 不新增通知 API，不把 snapshot 解释为 Center、agent 或消息链路健康。

## Acceptance Criteria

- [ ] UI 和测试中不再出现“系统正常”；stale/error/loading 不以 0 暗示无异常。
- [ ] visibility/focus 触发刷新，超出 freshness window 后显示“摘要已过期”。
- [ ] route render throw 与 rejected lazy import 均出现安全恢复页。
- [ ] 通知图标是可访问链接，目标 query 与后端现有 contract 一致。
