# 统一资产取消与生命周期协同设计

## Architecture

新增 `internal/center/assetlifecycle` 领域包，集中定义 cancellation preview、apply request、action record、step record、node/target asset context DTO 和领域 sentinel。新增 `internal/center/store/asset_lifecycle.go` 作为 Postgres 实现，使用手写 SQL 聚合现有 Asset Ledger、Node 和 Target 数据。

跨域写入只能通过 `ApplyVPSCancellation` 完成。该方法开启单事务，锁定 VPS、选中的订阅、Node、Target 行，依次写入当前状态、历史表、state change events 和 lifecycle action steps。普通 `subscriptions.PatchSubscription`、`vpsassets.PatchVPSAsset`、Node/Target runtime controls 不新增隐式级联。

## Data Model

新增 migration `0028_create_asset_lifecycle_actions.sql`：

- `asset_lifecycle_actions`: `action_id`、`vps_id`、`action_type`、`status`、`reason`、`effective_date`、`created_at`、`confirmed_at`、`completed_at`、`summary jsonb`。
- `asset_lifecycle_action_steps`: `step_id`、`action_id`、`object_type`、`object_id`、`step_type`、`status`、`before_state jsonb`、`after_state jsonb`、`message`、`executed_at`、`created_at`。

现有历史表继续保留当前职责：VPS 续费决策写 `renewal_decisions`，订阅状态/续费/自动续费写 `price_histories`，Node/Target 状态写 `state_change_events`。Lifecycle action 表提供跨对象审计线索，不替代这些已有历史。

## API Contracts

- `GET /api/vps/{vps_id}/cancellation-preview` 返回 `CancellationPreview`，包含 `vps`、`subscriptions`、`node_links`、`services`、`domains`、`target_links`、`recommended_steps`、`warnings`、`blockers`。
- `POST /api/vps/{vps_id}/cancellation` 接收 `reason`、`effective_date`、`subscription_ids`、`vps_lifecycle_status`、`node_actions`、`target_actions`，返回 `LifecycleActionResult`。
- `GET /api/asset-context/nodes` 返回 `AssetContextForNode[]`，供 Node 列表和详情批量显示关联 VPS 取消上下文。
- `GET /api/asset-context/targets` 返回 `AssetContextForTarget[]`，供 Target/实例列表和详情显示 service/domain/VPS 上下文。

状态默认规则：

- 已过期或已取消订阅证据存在时，VPS 推荐 `renewal_decision=cancel`、`lifecycle_status=cancelled`。
- 未来到期但已决定不续费时，VPS 推荐 `renewal_decision=cancel`、`lifecycle_status=to_cancel`。
- 未来取消且仍需观察的 Node 推荐 `不续费`；实际退役 Node 推荐 `已退役`，监控暂停必须显式确认。
- Target 随服务下线推荐 `已归档`；临时停用才用 `暂停`。

## Frontend Flow

新增受控取消/退役工作台组件，优先复用现有 atoms、DataTable、Modal/Drawer 和页面状态模式。入口来自 SubscriptionsPage、VPSPage、VPSDetailPage 和 AssetDecisionsPage。工作台先加载 preview，再让用户选择要执行的 subscription、Node、Target 步骤，最后 POST apply。

VPSPage 新增 `view=cancellation_attention` 派生视图；SubscriptionsPage 在订阅变成 inactive 后展示联动处理入口；Node/Target 列表加载 asset context 批量接口并展示取消/过期 badge；详情页使用同一 context 数据展示证据。

### Cancellation Workbench UX

取消/退役工作台是一个受控决策表单，不是若干普通 card 纵向堆叠。它必须服务“先确认影响面，再选择要执行的变更，再审计提交”的扫描路径：

- 顶部摘要使用稳定的 `auto-fit/minmax` summary strip，最少宽度足够时四项同排；中等视口自动 2×2；窄屏单列，不允许 3+1 断行留空。
- 主体使用响应式 `asset-cancel-workbench__body`：桌面下左侧为 decision rail（VPS state + audit），右侧为 confirmation rail（订阅、Node、Target）；小桌面/平板降为单列但每个 section 内仍保持紧凑 grid。
- VPS state 只展示“推荐/当前状态 + lifecycle select + effective date”，select 宽度由 field grid 控制；`取消` / `待取消` 不做全宽 pill。
- 订阅、Node、Target 确认项使用 compact choice row：checkbox + identity + current fact + proposed action controls。说明文字必须低权重，状态 badge 靠近事实，不与 identity 拼成长句。
- Audit/确认执行与 VPS state 合并在同一决策 rail，减少底部大空白；错误/成功反馈就地显示，提交按钮固定在 audit action row。
- 所有颜色、间距、圆角、字体使用现有 token 和 atoms；样式落 `web/src/index.css` 的 `.asset-cancel-workbench*` BEM 块，不新增局部 CSS 文件。

## Compatibility

既有 VPS PATCH 的 `renewal_subscription_linkage` 保持兼容，但其 `no_active_subscription` 文案需区分“真的没有订阅”和“已有 inactive 订阅”。新工作台是推荐路径，不破坏旧按钮和旧测试。

新增 API 都是 additive；前端类型手写同步。数据库迁移幂等，不修改既有迁移。
