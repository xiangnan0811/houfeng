# Implementation Plan

## Order

1. 后端领域与迁移
   - 新增资产决策记录类型、输入验证和快照构造。
   - 新增 `0035_create_asset_decision_records.sql`。
   - 收紧 `renew_within_days` 为 `30/60/90`。

2. 后端 store/API/wiring
   - 扩展 `PostgresAssetDecisionRepository` 支持 records list/create/get/patch。
   - 新增 handler、router options、router 注册、bootstrap wiring。
   - 补 domain/store/handler/router/bootstrap 测试。

3. 前端数据层
   - 新增 `web/src/lib/types.ts` 类型。
   - 新增 `web/src/lib/api.ts` helper 和 `api.test.ts` 覆盖路径/payload。

4. 前端页面
   - `AssetDecisionsPage` 加载并展示保存的组合决策记录。
   - 组详情支持保存为决策记录，包含成员级角色/动作/理由。
   - 记录详情支持回看和状态推进。
   - 补页面测试，确保单台队列 PATCH 行为不变。

5. 验证
   - `go test ./internal/center/assetdecisions ./internal/center/store ./internal/center/http ./cmd/houfeng-center`
   - `cd web && npm run test -- --run api AssetDecisionsPage`
   - 视情况运行 `npm run lint` / `npm run build` / 项目 verify 脚本。

## Risk Files

- `internal/center/assetdecisions/types.go`
- `internal/center/store/asset_decisions.go`
- `internal/center/http/handlers/asset_decisions.go`
- `internal/center/http/router.go`
- `cmd/houfeng-center/bootstrap.go`
- `web/src/pages/AssetDecisionsPage.tsx`
- `web/src/lib/api.ts`
- `web/src/lib/types.ts`
- `web/src/styles/pages.css`

## Rollback Points

- 新迁移独立，不修改旧表；如 UI/API 有问题，可暂时隐藏前端入口，但保留表。
- Phase 1 API 不删除，自动组工作台可独立继续工作。
- 单台 `PATCH /api/vps/{id}` 决策路径保持不变。
