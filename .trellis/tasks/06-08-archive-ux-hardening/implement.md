# 归档功能体验与规则重构实施计划

## 顺序

1. 基线检查与规范读取
   - 读取 backend/web/spec/guides。
   - 确认当前路由、handlers、store、ArchivePage、VPSDetailPage 和测试现状。

2. 后端 TDD
   - 为 router 添加 archive-review / archive / restore-from-archive dispatch 测试。
   - 为 VPS PATCH 添加禁止直接 archive / restore 的 handler 测试。
   - 为 asset lifecycle store 添加 archive review blockers、成功 archive、restore 限制测试。
   - 实现 `assetlifecycle` types、store 方法、handlers、router 和 bootstrap。

3. 前端 TDD
   - `api.test.ts` 覆盖三个新 API helper 和 `asset_scope=all` query。
   - `VPSDetailPage.test.tsx` 覆盖归档 review、强确认、blockers、成功跳转、归档/取消详情重定向。
   - `ArchivePage.test.tsx` 改为列表页测试，确认不自动拉详情。
   - 新增 `ArchiveDetailPage.test.tsx` 覆盖只读详情分区、用户记录优先、底部监控/Target、恢复入口限制。
   - 实现 router split、Archive list/detail 组件、VPSDetailPage flow。

4. 样式与可视化检查
   - 更新 `web/src/index.css` archive 相关样式。
   - 启动本地前端/应用可用服务后，用浏览器检查 `/archive` 与 `/archive/:vpsId` 桌面和移动布局；如真实后端不可用，使用测试覆盖作为最低验证并记录原因。

5. 质量验证
   - 运行 focused Go tests：router、handlers、store lifecycle。
   - 运行 focused web tests：api、VPSDetailPage、ArchivePage、ArchiveDetailPage。
   - 运行项目质量门：`./scripts/verify.sh` 或等价分项命令。
   - 按 PRD acceptance 逐项审查，发现问题继续修改。

## 风险文件

- `internal/center/http/router.go`
- `internal/center/http/handlers/vps.go`
- `internal/center/http/handlers/asset_lifecycle.go`
- `internal/center/assetlifecycle/types.go`
- `internal/center/store/asset_lifecycle.go`
- `cmd/houfeng-center/bootstrap.go`
- `web/src/app/router.tsx`
- `web/src/lib/api.ts`
- `web/src/lib/types.ts`
- `web/src/pages/VPSDetailPage.tsx`
- `web/src/pages/ArchivePage.tsx`
- `web/src/pages/archive/*`
- `web/src/index.css`

## 验证命令

- `go test ./internal/center/http/... ./internal/center/store/... ./internal/center/assetlifecycle/...`
- `npm test -- --run src/lib/api.test.ts src/pages/VPSDetailPage.test.tsx src/pages/ArchivePage.test.tsx src/pages/ArchiveDetailPage.test.tsx`
- `./scripts/verify.sh`
