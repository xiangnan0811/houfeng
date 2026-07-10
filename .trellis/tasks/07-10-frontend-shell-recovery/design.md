# Shell 摘要与恢复设计

## Summary Model

提取不依赖 React 的 summary model，输入 dashboard remote state、`snapshot_generated_at` 和当前时间，输出 loading、available-clear、available-with-anomaly、stale 或 unavailable。freshness 只以服务端时间计算。

## Refresh Policy

初始 mount 请求一次；document 从 hidden 变为 visible 或 window focus 时，如果没有同请求 in-flight 则刷新。失败保留最后成功 snapshot 但标为 stale/unavailable，不伪造当前时间。

## Error Recovery

- `router.tsx` 在受保护 route tree 设置 `errorElement={<RouteErrorPage />}`。
- `AppErrorBoundary` 包裹 RouterProvider 外部 provider/render 区域。
- 恢复操作仅包含重试、刷新和返回工作台；生产 UI 只显示安全摘要，完整异常留给日志。

## Notification Contract

使用 React Router `Link` 指向 `/events?notification_only=1`，不显示 count，直到存在真实计数 contract。

## Rollback

summary refresh、error boundary 和通知入口分提交；若刷新造成请求压力，只回滚 refresh，保留语义与恢复页。
