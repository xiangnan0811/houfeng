# 统一资产取消与生命周期协同执行计划

## Checklist

1. 后端领域与迁移
   - 新增 `assetlifecycle` DTO、sentinel 和校验。
   - 新增 `0028_create_asset_lifecycle_actions.sql` 与迁移测试。
   - 新增 Postgres repository，完成 preview、apply、node context、target context。

2. 后端 HTTP 与装配
   - 新增 lifecycle/asset-context handlers。
   - 更新 router、bootstrap 和 bootstrap 测试。
   - 更新 VPS 取消 linkage 文案，区分 inactive subscription 证据。
   - 更新 Dashboard asset summary 统计。

3. 前端数据层
   - 更新 `web/src/lib/types.ts` 和 `web/src/lib/api.ts`。
   - 增加 API 测试覆盖新增 endpoints 和 request body。

4. 前端页面
   - 新增/复用取消工作台组件。
   - SubscriptionsPage、VPSPage、VPSDetailPage、AssetDecisionsPage 接入入口和状态提示。
   - NodesPage/NodeDetailPage、TargetsPage/TargetDetailPage 接入 asset context。
   - 补必要 CSS 到现有全局样式文件，保持 v2 工具型密度。
   - 重构 `VPSCancellationWorkbench` 的信息架构和响应式样式：summary strip、decision rail、confirmation rail、compact choice row、audit footer，并覆盖桌面/小桌面/平板/手机。

5. 文档与规范
   - 更新 `.trellis/spec/backend/database-guidelines.md` 的 Asset Ledger 边界规则。
   - 更新 `.trellis/spec/web/state-and-data.md` 与 `docs/design/v2-houfeng/component-spec.md` 的工作台 contract。

## Validation

- `go test ./...`
- `cd web && npm run lint`
- `cd web && npm run test -- --run`
- `cd web && npm run build`
- 前端完成后启动 `cd web && npm run dev -- --host 127.0.0.1 --port 5178`，用 Browser 检查 `/subscriptions`、`/vps`、`/vps/:id`、`/nodes`、`/targets`。
- 工作台 UI 修复后用 Browser 检查 `/vps/vps_fra_legacy?workbench=cancellation`，至少覆盖 1440×1000、1024×768、768×1024、390×900；确认无 3+1 summary、无全宽单值 pill、无文本重叠/横向溢出。

## Risk Points

- 跨对象事务必须避免调用会自行开事务的既有 repository 方法；在 `asset_lifecycle.go` 中使用 tx-scoped SQL helper。
- Node/Target 状态变更要写现有 `state_change_events`，保持运行证据历史不丢。
- 前端列表 context 必须批量加载，避免 Node/Target 行级 waterfall。
- 现有页面测试很多，优先补高信号场景，不做大范围视觉重写。
- UI 修复只改变工作台结构和样式，不改 lifecycle action payload 语义；测试必须继续证明只提交用户确认的 subscription/node/target step。
