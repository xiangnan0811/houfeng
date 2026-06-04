# 服务商页轻量上下文升级实施计划

## Steps

1. 绑定 Trellis 任务到 `worktree/provider-directory-context`，设置 base branch 为 `main`，启动任务。
2. 读取 web spec：component conventions、state/data、styling、quality、shared guides。
3. 更新 `ProvidersPage`：
   - 增加 VPS / subscriptions 加载和上下文错误降级。
   - 增加 provider row 派生、quick view、搜索。
   - 替换 header、轻量摘要 rail、增强表格和行操作。
   - 支持从 `account_hint` 拆分多账号提示。
   - 增加外部口碑入口列，明确与我的评分分离；不抓取或宣称实时外部分数。
   - 重组创建/编辑表单。
4. 补充 `ProvidersPage.test.tsx`：
   - 覆盖上下文派生、降级、筛选、搜索、多账号、外部口碑入口、链接、表单兼容。
   - 保留现有创建/编辑/错误/取消重置断言。
5. 如需要，补充 `pages.css` 中页面级 BEM 样式，不新增 page-local CSS。
6. 运行验证并修复失败：
   - `cd web && npm run test -- --run ProvidersPage`
   - `cd web && npm run test -- --run ProvidersPage SubscriptionsPage`
   - `cd web && npm run lint`
   - `cd web && npm run build`
   - 可行时运行完整 `cd web && npm run test -- --run`

## Rollback

若上下文派生导致测试或 UX 风险过高，可保留 header / 表单分组 / 搜索筛选，移除 VPS / subscription 派生；后端和数据库没有变更，无迁移回滚。
