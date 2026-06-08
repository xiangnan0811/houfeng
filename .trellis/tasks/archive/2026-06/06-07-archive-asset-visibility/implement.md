# 归档资产可见性改造实施计划

## 执行顺序

1. 后端 TDD：为 VPS scope、订阅 scope、Dashboard cost/summary、Asset Decisions 归档边界、Monitoring/Targets scope 写失败测试。
2. 后端实现：新增共享 `AssetScope`、接入 handlers/types/store SQL，修正成本中心和 Dashboard 查询，更新 Asset Decisions 派生逻辑。
3. 前端 TDD：为 API query、VPS 归档入口、ArchivePage happy path、普通页面默认过滤补失败测试。
4. 前端实现：更新 `types.ts` / `api.ts`，新增 `/archive` 路由与只读页面，调整 VPS/Subscriptions/Monitoring/Targets/GlobalSearch/Events 调用。
5. 运行验证：优先跑聚焦测试，再跑 `make verify-go`、`cd web && npm run lint`、`cd web && npm run test -- --run`、`cd web && npm run build`；必要时最后跑 `./scripts/verify.sh`。

## 风险点

- Dashboard 和订阅成本 SQL 不应只改页面过滤，否则指标仍然污染。
- Monitoring/Targets 不能用自身状态替代 VPS scope，必须用资产关联上下文判断。
- 归档页不能调用写接口或显示现有详情页的恢复/编辑操作。
- 全局搜索和事件筛选需要保留可搜索/筛选归档历史的能力时，应链接到 `/archive` 或显式使用 `asset_scope=all`，不能把普通结果重新污染。

## 验证命令

- `go test ./internal/center/vpsassets ./internal/center/store ./internal/center/http/handlers ./internal/center/assetdecisions`
- `make verify-go`
- `cd web && npm run lint`
- `cd web && npm run test -- --run`
- `cd web && npm run build`
- `./scripts/verify.sh`
