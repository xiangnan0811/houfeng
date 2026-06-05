# 资产组合决策中枢 Phase 1 实施计划

## 0. 前置

- [x] 在非 main worktree 中实施：`.worktree/asset-decisions-phase1`
- [x] 启用 hooks：`sh scripts/setup-git-hooks.sh`
- [x] `task.py start` 前完成 PRD / design / implement review
- [x] 加载 `trellis-before-dev` 相关规范

## 1. 后端领域与读模型

- [x] 新增 `internal/center/assetdecisions/types.go`
  - 枚举 view / group type / suggested role / suggested action。
  - request filters 校验。
  - Overview / GroupSummary / GroupDetail / Member DTO。
  - sentinel errors：invalid input、group not found。
- [x] 新增 `internal/center/store/asset_decisions.go`
  - 一次性读取 member facts。
  - Go 层派生 automatic groups。
  - 稳定 group ID。
  - overview 从 group summaries 派生。
- [x] Store tests
  - renewal / cancellation / region / provider / cost / evidence gap。
  - archived 不进普通组合。
  - cancelled/to_cancel 只进入取消联动相关证据。
  - 缺订阅 / 缺监控 / 缺服务域名返回 evidence gap。
  - 不依赖 runtime facts detail。

## 2. 后端 HTTP

- [x] 新增 `internal/center/http/handlers/asset_decisions.go`
  - overview。
  - groups list。
  - group detail。
  - method not allowed、invalid query、missing group、repo failure。
- [x] 更新 `internal/center/http/router.go`
  - RouterOptions 字段。
  - `/api/asset-decisions/overview`
  - `/api/asset-decisions/groups`
  - `/api/asset-decisions/groups/`
- [x] 更新 `cmd/houfeng-center/bootstrap.go`
  - repo construction。
  - handler wiring。
- [x] 更新 bootstrap / router tests
  - bootstrap non-nil wiring。
  - API 不落 SPA fallback。
  - auth middleware 覆盖。

## 3. 前端 API / 类型

- [x] 更新 `web/src/lib/types.ts`
  - AssetDecision* 类型。
- [x] 更新 `web/src/lib/api.ts`
  - `getAssetDecisionOverview`
  - `listAssetDecisionGroups`
  - `getAssetDecisionGroup`
- [x] 更新 `web/src/lib/api.test.ts`
  - query string contract。

## 4. AssetDecisionsPage 重构

- [x] 页面标题改为 `资产组合决策`。
- [x] URL-state tabs：needs_decision / renewal / region / provider / cost / evidence / single_queue。
- [x] 主 surface 决策组列表。
- [x] group detail drawer。
- [x] 保留单台辅助队列和 `AssetDecisionWorkPanel`。
- [x] 续费候选表降级为 `RENEWAL EVIDENCE`。
- [x] 错误降级：组合 API、group detail、续费 evidence、单台 queue 分开处理。
- [x] 页面测试迁移 / 扩展。

## 5. 入口融合

- [x] Dashboard links 指向 `/asset-decisions?view=...`。
- [x] VPS 页加入进入组合决策入口。
- [x] Subscriptions 行加入资产判断链接。
- [x] Providers 行加入 provider combination decision 链接。
- [x] 对应测试补充或调整。

## 6. 规范更新

- [x] `.trellis/spec/web/state-and-data.md`
- [x] `docs/design/v2-houfeng/component-spec.md`
- [x] 如后端读模型形成新约定，补 `.trellis/spec/backend/database-guidelines.md` 或新增后端规范段落。

## 7. 验证

后端：

- [x] `go test ./internal/center/assetdecisions ./internal/center/store ./internal/center/http/handlers ./internal/center/http ./cmd/houfeng-center`
- [x] 必要时 `make verify-go`

前端：

- [x] `cd web && npm run test -- --run src/lib/api.test.ts src/pages/AssetDecisionsPage.test.tsx src/pages/DashboardPage.test.tsx src/pages/VPSPage.test.tsx src/pages/SubscriptionsPage.test.tsx src/pages/ProvidersPage.test.tsx`
- [x] `cd web && npm run lint`
- [x] `cd web && npm run build`

视觉：

- [x] 启动 dev server。
- [x] 用 asset-workflows mock 或本地 center 检查 `/asset-decisions` 桌面与移动端。
- [x] 确认决策组列表和 drawer 无横向页面溢出。

备注：标准 `scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5183 --mock-api asset-workflows --route /asset-decisions --viewport 1440x1000 --viewport 390x900` 因本机未安装 Python Playwright 被阻塞；项目脚本明确不把它作为依赖。本轮改用 in-app Browser 对 1440x1000 与 390x900 完成 sanity，根页面、panel 与 drawer 均无横向页面溢出，宽表格由内部 `asset-table-scroll` 承接。

## Rollback Points

- 后端 API wiring 前，单独回滚 `assetdecisions` 包和 store 不影响现有路由。
- 前端页面重构前，保留现有单台 queue 辅助逻辑，避免丢失 `PATCH /api/vps/{id}` 流程。
- 入口融合可独立回滚，不影响主 API。
