# Complete lifecycle state closure

## Goal

继续推进状态生命周期一致性目标，把上一轮复审中仍未闭环的状态歧义收口为可测试、可解释、用户无歧义的系统行为。重点不是扩大新功能，而是关闭已经确认会影响决策、提醒、告警和页面理解的剩余状态链路。

## User Value

- 用户在 VPS、订阅、监控、Target、归档和资产决策页面看到的状态必须能对应真实系统行为。
- 暂停、维护、退役、归档等用户主动下线状态不能继续制造“当前 active 风险”的误导。
- 订阅的抽奖、赠送、Bonus/余额抵扣等来源语义必须可区分或至少不再用错误标签混合展示。
- 开发者通过 API scope 和状态枚举时不应被 `archived` 这类历史兼容命名误导。

## Confirmed Facts

- 上轮任务已修复 SLC-01、SLC-05、SLC-06、SLC-07、SLC-09，并对 SLC-02/SLC-03/SLC-08/SLC-12 做了最小缓解。
- `vps_assets.lifecycle_status` 仍包含流程态/终态：`to_migrate`、`to_cancel`、`cancelled`、`archived`。
- 普通 VPS PATCH 已禁止进入受控流程态，但 VPS create/import 仍可能接受不合理 lifecycle/renewal/usage 组合。
- 迁移 workbench 尚不存在，当前规格要求页面只能表达“迁移意向/人工跟进”。
- `incidents.Service` 已跳过暂停/维护/退役监控实例的 heartbeat stale sweep，但已有 active incidents 的收敛策略还没有闭环。
- Target periodic sweep 当前遍历 `ListTargets` 后直接评估 target，不按 `暂停` / `已归档` 收敛 active incidents。
- `subscriptions.renewal_mode` 有 `lottery` 和 `bonus`，前端把 `lottery` 显示为“抽奖/赠送”，没有 `gift`。
- `asset_scope=archived` 实际查询 `cancelled` 与 `archived`，页面文案准确，但 API 命名容易误导；可兼容新增 `historical` 作为更准确别名。

## Requirements

- VPS 状态矩阵收口：为 lifecycle、usage、renewal decision 定义禁止组合、warning 组合和允许组合，并在 create/patch/import 入口中阻止最危险的冲突组合。
- 迁移语义收口：迁移在工作台完成前继续作为人工意向，不新增半成品迁移工作台；任何生产文案不得承诺迁移流程闭环。
- Incident 收敛：暂停、维护、退役、归档的 MonitoringInstance 和暂停/归档 Target 不产生新的 active incidents，并清空已有 active incidents，写 recovered event 作为历史说明，不发送恢复通知。
- 订阅来源语义：新增或明确 `gift` renewal mode，使“抽奖”和“赠送”不再混合；同步 DB constraint、Go enum、前端类型、选项、标签和测试。
- Asset scope 命名兼容：保留 `asset_scope=archived`，新增 `asset_scope=historical` 作为 cancelled+archived 的准确别名；前端 archive 页面可继续用旧参数或切换到新参数，但 API/类型/测试必须说明兼容行为。
- 页面实际效果：关键状态页面在 mock API 下通过 browser sanity；如果新增文案或局部状态说明，移动/桌面都不能产生页面级横向溢出。

## Acceptance Criteria

- [ ] VPS create/patch 状态矩阵测试覆盖并拒绝明显冲突组合，例如 `cancelled + keep`、`to_migrate + cancel`、`replaced + active/in_use` 等；允许历史/人工意向需要有明确例外。
- [ ] 导入或创建 VPS 时不能创建已经处于归档/取消/迁移流程但缺少 lifecycle action 审计的“当前事实”。
- [ ] MonitoringInstance 非运行态会使已有 active incidents 收敛：mutation active 为空，产生 recovered events，且不发送恢复通知。
- [ ] Target `暂停` / `已归档` 会在周期 sweep 中收敛已有 target active incidents，不再按探测事实继续评估。
- [ ] `gift` renewal mode 从 DB constraint、Go validation、历史 price mode validation、前端 `RenewalMode` 类型、表单选项、label 到 tests 全链路可用。
- [ ] 前端不再把 `lottery` 显示成“抽奖/赠送”；`lottery` 为“抽奖”，`gift` 为“赠送”。
- [ ] `asset_scope=historical` 在 VPS 和 subscriptions API 中合法，并与旧 `archived` scope 等价；现有 `archived` 兼容不破坏。
- [ ] 复审报告更新，逐条说明上一轮剩余项已闭环、仍不做的超大能力及其原因。
- [ ] 后端、前端、脚本和浏览器 sanity 验证通过；若仍有局部 warning，必须记录其是否影响本目标。

## Out of Scope

- 不在本轮实现完整迁移 workbench、source/target VPS cutover、服务/domain/Target/Monitoring 自动迁移。
- 不回填线上历史订阅 renewal mode；只扩展 schema/validation 和默认展示语义。
- 不做大规模 UI IA 重构；只修状态合同、标签、筛选和解释。
- 不引入正式 Playwright/Cypress CI 依赖；继续使用 `uv run --with playwright` 做 local browser sanity。

## Open Questions

- 暂无阻塞问题。当前范围来自已归档审查任务和代码事实；用户已要求继续推进直到 goal 完成。
