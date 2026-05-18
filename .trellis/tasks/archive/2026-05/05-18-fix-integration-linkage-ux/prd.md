# 修复系统联动割裂与弱关联交互

## Goal

把 Asset Ledger、订阅、服务商、Node/Target 支撑面之间的关键用户流程从“页面各自为政、靠人工复制 ID/跳转补齐”调整为一个整体：明确主从业务事实时自动联动；存在歧义或跨观测边界时，提供可见的选择、跳转、快捷创建或确认动作，避免静默不动。

## What I Already Know

- 用户已确认本任务按“强联动推荐”作为验收原则：同一业务事实有明确主从关系时自动同步；有歧义时给出提示/跳转/快捷创建，不静默不动。
- 已知痛点：VPS 续费决策改为取消续费时，对应订阅没有联动取消自动续费。
- 已知痛点：VPS 关联 Node 需要手动输入 `node_id`，而不是从现有 Node 中选择。
- 已知痛点：创建第一台 VPS 或第一个节点时，如果没有服务商，当前流程不能在原地创建服务商或提供明确跳转。
- 前端审计发现多个 raw ID 输入点：VPS facts 的 `provider_id`、VPS-Node link 的 `node_id`、服务/域名的 `target_id`、域名的 `service_id`。
- 后端审计发现当前没有跨聚合事务来同时更新 VPS 续费决策和订阅自动续费状态；VPS PATCH 与 Subscription PATCH 各自维护自己的当前行和历史行。
- 当前 Trellis backend spec 明确写着 subscription CRUD 不反写 VPS，renewal decision history 不反写 subscription；本任务若要实现强联动，需要更新这些 spec 边界，至少为用户显式决策流开一个受控例外。
- 当前 spec 也明确 Asset Ledger 不应自动改写 Node/Target/Agent 语义；因此 Node/Target 相关联动应以选择、确认、跳转、快捷创建为主，不应自动改变运行时观测状态。

## Research References

- [`research/frontend-linkage-audit.md`](research/frontend-linkage-audit.md) — 前端联动断点：raw ID 输入、空态/前置数据创建、跨页订阅与续费决策割裂、Node/Target/Service/Domain 单向入口。
- [`research/backend-linkage-audit.md`](research/backend-linkage-audit.md) — 后端联动断点：VPS/订阅无跨聚合同步事务，Provider/Node/Target 边界被 spec 明确隔离，服务/域名仅 list/create。
- [`research/spec-linkage-context.md`](research/spec-linkage-context.md) — 规范约束：Asset Ledger 是观测模型扩展层，Node/Target/Agent 语义不能被资产页隐式改写；URL state、Drawer、PageState、设计语言与质量门槛。

## Requirements (Evolving)

### R1. 续费决策与订阅自动续费强联动

- 当用户在 VPS 详情或资产决策队列把 VPS `renewal_decision` 改为取消类决策时，系统必须同步处理该 VPS 的明确关联订阅。
- 推荐规则：若存在该 VPS 的 active/current 订阅，则将订阅 `auto_renew=false` 且 `auto_renew_cancelled=true`，并保留订阅价格历史；页面保存成功后显示联动结果。
- 如果没有订阅，保存续费决策后应提示“缺少订阅记录”并提供创建/跳转入口，而不是静默。
- 如果存在多个可能受影响的订阅，应进入确认/选择路径，避免误改。

### R2. 用选择器替代业务对象 raw ID 输入

- VPS 关联 Node 改为从 Node 列表中选择，选项至少显示名称、ID、provider/状态等可辨识信息。
- VPS facts 中的服务商关联改为 Provider 选择器，并在无 Provider 时提供创建/跳转入口。
- VPS 服务/域名关联 Target、域名关联 Service 时，优先提供下拉选择；保留必要的空值能力。
- 用户不应为了完成常规联动而复制粘贴内部 ID。

### R3. 前置数据缺失时提供原地动作或明确跳转

- 创建 VPS 时如果没有服务商，应提供“创建服务商”或明确跳转，而不是只允许留空/手动离开当前流程。
- 创建/编辑订阅时如果没有 VPS，应提供去 VPS 创建的入口；从 VPS 详情进入订阅创建时应携带当前 VPS 上下文。
- Node 详情看到未关联 VPS 时，应提供关联/跳转到 VPS 选择流程的动作，而不是只有说明文本。

### R4. 跨页上下文必须可见、可逆、可落地

- 从 VPS 详情跳到订阅页时，应携带 `vps_id` 或等价上下文，并在订阅页以可见筛选/预填方式呈现。
- 资产决策队列中的订阅证据不应只显示 raw `vps_id` / `subscription_id`；应尽量展示 VPS 名称和可落地链接。
- 有 detail 路由的对象应链接到 detail；无 detail 路由的对象应链接到带筛选/预填上下文的列表页。

### R5. 保持观测边界安全

- VPS/订阅/服务商的 Asset Ledger 联动不得隐式修改 Node lifecycle、Node provider、Target、ProbeItem、Agent 计划或运行时控制。
- Node/Target 相关联动以用户选择、确认、跳转、可见链接为主。
- 不引入 MQ、TSDB、状态库、前端状态库、Tailwind/CSS-in-JS 或新图表库。

## Acceptance Criteria (Evolving)

- [ ] 用户把某 VPS 的续费决策改为取消类决策后，明确关联的 active/current 订阅自动取消自动续费，并能在订阅页看到结果。
- [ ] 续费决策联动订阅时，VPS renewal decision history 与 subscription price history 都按现有历史机制保留。
- [ ] 多订阅或缺订阅场景有可见提示/选择/创建入口，不静默失败。
- [ ] VPS 关联 Node 的表单不再要求手动输入 `node_id`。
- [ ] VPS 服务商、服务/域名 Target/Service 关联的常规路径不再要求手动复制内部 ID。
- [ ] 空服务商、空 VPS、空 Node/未关联 VPS 等前置数据缺失场景都有原地创建或明确跳转。
- [ ] 从 VPS 详情/资产决策进入订阅页能保留并展示当前 VPS 上下文。
- [ ] 后端/前端 tests 覆盖新增联动规则、选择器数据加载、URL 上下文、空态动作。
- [ ] `make verify-go`、`cd web && npm run lint && npm run test && npm run build` 通过。
- [ ] 完成浏览器手动验证：至少覆盖 `/asset-decisions`、`/vps`、`/vps/:id`、`/subscriptions`、`/providers`、`/nodes/:id` 的关键路径。

## Definition of Done

- 代码实现、测试、lint/typecheck/build 全部通过。
- 与现有设计语言一致：暗色、高密度、中文主界面、纯 CSS/BEM/tokens。
- 任何新增或调整的跨聚合规则同步到 `.trellis/spec/`，避免未来实现继续按旧“禁止联动”边界回退。
- 不把 Asset Ledger 联动扩张为自动修改 Node/Target/Agent 运行时语义。

## Technical Approach (Draft)

1. 后端增加受控的资产决策联动路径：在保存 VPS 续费决策时，根据明确订阅关系同步订阅自动续费字段，并保持现有历史写入事务语义。
2. 前端将常规对象关联从 raw ID 输入改成由页面加载数据驱动的选择器，并在缺数据时给出快捷创建/跳转。
3. 增强 URL 上下文：`/subscriptions?vps_id=<id>` 用于筛选/预填；从 VPS 详情和决策队列跳转时带上上下文。
4. 增强空态和回链：Node detail、VPS detail、订阅页、服务商页等页面给出下一步动作，不只显示静态说明。
5. 更新 Trellis specs 中与本次改变冲突的 Asset Ledger 联动边界。

## Decisions

### D1. 自动写回边界

**Decision**: 自动写回严格限制在 Asset Ledger 内部的 VPS↔Subscription 用户显式决策流。Node/Target/Agent 相关只做选择、确认、跳转和快捷创建，不由资产页隐式修改观测运行态。

**Consequences**: 续费决策可以强联动订阅自动续费；VPS↔Node、Service/Domain↔Target、Provider↔Node provider 等跨观测边界场景以用户可见动作解决割裂感，避免误改运行时事实。

## Out of Scope (Draft)

- 不自动从 Node onboarding、agent enrollment 或 JSON import 创建 VPS↔Node link；这些仍需要用户确认。
- 不自动同步 Provider 名称到 `nodes.provider`；Node provider 仍属观测侧事实。
- 不自动从 VPS 服务/域名创建 Target 或 ProbeItem；Target/ProbeItem 仍遵循观测入口建模流程。
- 不新增 providers/subscriptions detail 页面，除非实现中发现列表筛选无法支撑可落地链接。
- 不新增删除语义；继续遵守“优先状态变化/软 unlink，而非物理删除”。

## Technical Notes

- 任务目录：`.trellis/tasks/05-18-fix-integration-linkage-ux`
- 当前分支：`fix/integration-linkage-ux`
- 相关前端页面：`web/src/pages/VPSPage.tsx`、`web/src/pages/VPSDetailPage.tsx`、`web/src/pages/SubscriptionsPage.tsx`、`web/src/pages/ProvidersPage.tsx`、`web/src/pages/NodesPage.tsx`、`web/src/pages/node-detail/NodeLinkedVPSSection.tsx`、`web/src/pages/vps-detail/*`、`web/src/components/AssetDecision*`。
- 相关 API/types：`web/src/lib/api.ts`、`web/src/lib/types.ts`。
- 相关后端包：`internal/center/vpsassets`、`internal/center/subscriptions`、`internal/center/assetlinks`、`internal/center/providers`、`internal/center/http/handlers`、`internal/center/store`。
- 相关 specs：`.trellis/spec/backend/database-guidelines.md`、`.trellis/spec/web/state-and-data.md`、`.trellis/spec/web/component-conventions.md`、`.trellis/spec/guides/cross-layer-thinking-guide.md`。
