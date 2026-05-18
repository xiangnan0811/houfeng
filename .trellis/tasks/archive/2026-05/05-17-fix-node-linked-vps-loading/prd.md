# Fix node detail linked VPS loading

## Goal

修复节点详情页“关联 VPS”区域在真实环境中请求已完成但 UI 一直停留在“正在加载关联 VPS…”的问题，并在页面语义上维持 VPS Asset Ledger 与 Node observability 的清晰边界。

## Requirements

- 节点详情页进入“关联 VPS”区域后必须稳定请求 `GET /api/nodes/{node_id}/vps`。
- 当关联 VPS 请求成功返回时，UI 必须从 loading 收敛到记录表格或空态，不能因为本地 loading state 变更取消唯一请求。
- 当请求失败时，UI 必须从 loading 收敛到错误状态。
- 保持现有 API contract：节点详情页通过 `listVPSForNode(nodeId)` 读取 `VPSSummary[]`，不新增后端字段或 endpoint。
- 页面文案应帮助操作者理解：VPS 是资产账本对象，Node 是 agent 上报的运行实例；两者可以关联，但不是同一个业务对象。

## Acceptance Criteria

- [ ] 慢速/延迟的 `GET /api/nodes/{node_id}/vps` 响应完成后，节点详情页“关联 VPS”不再停留在“正在加载关联 VPS…”。
- [ ] 已关联 VPS 时显示 VPS 行；未关联时显示“尚未关联 VPS”。
- [ ] 测试覆盖延迟响应场景，防止 loading state effect cleanup 重新引入卡死。
- [ ] `cd web && npm run test -- --run NodeDetailPage.test.tsx` 通过。
- [ ] `cd web && npm run lint` 与 `cd web && npm run build` 通过。

## Technical Approach

当前 `NodeDetailPage` 的 linked VPS effect 在发起请求后设置 `loading=true`，而 effect dependencies 又包含 `linkedVPSState.loading`。这会导致 effect 重新运行并执行 cleanup，把正在进行的唯一请求标记为 cancelled；真实网络稍慢时，成功响应被忽略，UI 永久停在 loading。

修复方向：让 linked VPS 请求完成由当前 route/node refs 或稳定触发条件保护，而不是被自身 loading state 的 effect cleanup 取消。补充一个 deferred fetch 测试复现慢响应，验证响应完成后 UI 收敛。

## Decision (ADR-lite)

**Context**: 该问题只出现在节点详情页的前端状态机；VPS 详情页反向关联节点可见，说明后端 link 数据与 `GET /api/vps/{vps_id}/nodes` 路径可用。

**Decision**: 保持现有懒加载和 API contract，仅修正节点详情页 `GET /api/nodes/{node_id}/vps` 的 effect cancellation/settlement 行为，并用测试锁住慢响应场景。

**Consequences**: 不引入 React Query/SWR 或全局缓存；未来若多个页面出现相同模式，再考虑抽通用 resource hook。

## Out of Scope

- 不改变 VPS/Node 数据模型。
- 不新增后端 endpoint 或数据库迁移。
- 不把 VPS 与 Node 合并为一个对象。
- 不引入第三方状态管理或请求缓存库。

## Technical Notes

- `web/src/pages/NodeDetailPage.tsx`：linked VPS lazy-load 状态机。
- `web/src/pages/node-detail/NodeLinkedVPSSection.tsx`：关联 VPS 展示区。
- `web/src/lib/api.ts`：`listVPSForNode(nodeId)` 调用 `/api/nodes/{node_id}/vps`。
- `internal/center/http/handlers/asset_links.go`：后端 `NodeVPS` handler。
- `internal/center/store/vps_node_links.go`：`ListVPSForNode` 查询 active links。
