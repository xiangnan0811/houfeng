# Shell 摘要与错误恢复实施计划

## Files

- Modify: `web/src/app/layout/AppShell.tsx`, `SyncStatus.tsx`, `TopBar.tsx`, `AppShell.test.tsx`
- Create: `web/src/app/AppErrorBoundary.tsx` and test
- Create: `web/src/app/RouteErrorPage.tsx` and test
- Modify: `web/src/app/router.tsx`

## Checklist

- [ ] 先写“摘要无异常但不是系统正常”、stale、failure 和 visibility refresh 失败测试。
- [ ] 提取 summary model，使用 `snapshot_generated_at` 和固定 freshness window。
- [ ] 实现 mount + visibility/focus 去重刷新，不引入常驻 interval。
- [ ] 为 protected route tree 增加 route error page，为 Router 外层增加 error boundary。
- [ ] 测试 route render throw、lazy import rejection、retry/refresh/home 操作和敏感详情不渲染。
- [ ] 用 Link 替换 NotificationBell button，删除固定 0 badge。
- [ ] 运行 focused tests、全量 gate 与浏览器长会话 refresh 检查。
- [ ] summary refresh 与 error recovery 分 commit，PR 标题 `fix(web): make shell status honest and recoverable`。

## Verification

```bash
NODE_ENV=test npm --prefix web run test -- --run src/app/layout/AppShell.test.tsx src/app/AppErrorBoundary.test.tsx src/app/RouteErrorPage.test.tsx
NODE_ENV=test npm --prefix web run test -- --run
NODE_ENV=production npm --prefix web run build
```
