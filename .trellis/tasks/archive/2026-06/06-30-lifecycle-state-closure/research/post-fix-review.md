# Lifecycle state closure post-fix review

## Scope

本复审只关闭上一轮状态生命周期审查中仍会影响当前系统判断、告警、提醒和用户理解的缺口。完整迁移工作台、source/target cutover、续费方式与权益来源拆表、历史数据回填仍属于独立大能力，不在本轮半实现。

## Closed Findings

### VPS state matrix

- Create/import 边界现在只接受 `active`、`idle`、`testing` 三种当前事实状态，不能直接创建 `to_migrate`、`to_cancel`、`cancelled`、`archived` 这类流程态或终态。
- `ValidateVPSStateCombination` 明确拒绝最危险矛盾：`cancelled + keep`、`cancelled + in_use`、`to_cancel` 非取消决策、`to_migrate` 非迁移决策、`replaced + active/in_use`。
- 普通 PATCH 仍只允许当前事实 lifecycle；PATCH delta 额外拒绝本次提交字段之间的矛盾，但不凭空推断未提交的当前值，避免误伤已有专用 lifecycle/archive 路径。

### Historical asset scope

- `asset_scope=historical` 已在 VPS 与 subscriptions 的 domain、handler、store、前端类型和 Archive 页面打通。
- `asset_scope=archived` 保留为兼容别名；两者都表达 `cancelled + archived` 历史工作集，不破坏旧客户端。
- Archive 列表页改用 `historical`，让 network evidence 与用户看到的“归档/历史资产”含义一致。

### Subscription renewal source semantics

- `renewal_mode` 新增 `gift`，DB constraint、Go validation、price history validation、前端类型和选项全链路同步。
- 前端 `lottery` 标签改为“抽奖”，`gift` 标签为“赠送”，不再把抽奖和赠送混成一个标签。
- `gift` 不映射 legacy `auto_renew` / `auto_renew_cancelled` flag，避免把权益来源误写成续费行为。

### Incident inactive convergence

- 暂停、维护、退役、归档 MonitoringInstance 在周期 stale sweep 和 `AfterSuccessfulSync` 中都会收敛已有 active incidents，写 recovered events，且不产生 notification records / sends。
- 暂停、已归档 Target 在周期 sweep 中收敛已有 target active incidents 并跳过 probe/TLS/trend 评估。
- `AfterSuccessfulSync` 对 touched Target 通过可选 `TargetGetter` 读取当前 Target 状态；若目标已暂停/归档，则先行政恢复已有 active incidents，不再用刚同步的观测制造新 active 风险。

### Migration wording and UX scan

- 生产代码扫描未发现“迁移流程”“迁移工作台”“推进迁移”等暗示已有迁移闭环的文案。
- 当前生产文案保留“标记迁移意向并人工跟进”，符合迁移 workbench 未落地前的人工意向合同。
- `推进迁移` 仅出现在测试输入、测试断言或任务文档语境中，不作为用户界面生产文案。

## Remaining Out of Scope

- 完整迁移 workbench、替代 VPS cutover、服务/域名/Target/MonitoringInstance 自动迁移：需要单独设计源/目标资产、影响范围、回滚、审计和用户确认，不应在本轮状态修复中半成品落地。
- 续费行为与权益来源正式拆表：本轮用 `gift` 最小消除标签歧义；长期模型拆分需要兼容旧数据、回填策略和 UI 表单重构。
- 线上历史 `renewal_mode` 回填：旧 `lottery` 记录继续显示“抽奖”，不会自动改成 `gift`，避免无证据重写历史事实。

## Verification Status

Fresh verification completed in this task:

- `make verify-go`
- `python3 -m unittest scripts/test_visual_evidence.py`
- `git diff --check`
- `cd web && npm run lint`
- `cd web && npm run test -- --run` (`71` files, `544` tests)
- `cd web && npm run build`
- Vite preview at `http://127.0.0.1:5178/` with `scripts/visual_evidence.py browser-sanity`:
  - `mock-api asset-workflows`: `/asset-decisions`, `/vps`, `/vps/vps_fra_legacy`, `/subscriptions`, `/settings`
  - `mock-api observability-support`: `/monitoring`, `/monitoring/mi_hkg_edge_01`, `/targets`, `/events`
  - Viewports: `1440x1000`, `390x900`

Browser sanity produced local-only overflow-risk warnings for long table text, numeric KPI fields and existing dense cards, but no route failed and no page-level horizontal overflow was reported. These warnings are visual review notes, not state-contract blockers for this task.
