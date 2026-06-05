# 资产组合决策中枢持续交付

## Goal

把 `/asset-decisions` 从只读的组合发现工作台继续推进为真正可用的资产组合决策中枢。当前任务不再让用户参与阶段切分；由实现者按产品价值和工程风险自行排序，优先交付最能闭环的能力。

## User Value

用户不只是需要看到“哪些 VPS 组合值得判断”，还需要能保存本次判断、记录依据、回看当时证据，并从决策中枢继续推进到 VPS、订阅、监控和取消/退役主路径。

当前系统仍处于早期开发阶段，没有线上用户迁移压力。因此实现顺序可以更偏向建立核心产品能力，而不是过度保守地保留所有临时边界。

## Product Direction

第一条交付主线是“决策记忆层”：

- 自动组继续作为发现入口。
- 用户可以把自动组保存为持久的组合决策记录。
- 决策记录保存组合目标、状态、成员级角色/动作/理由和当时证据快照。
- 页面展示已保存组合决策，并允许回看详情、推进状态。
- 执行动作仍回到已有主路径，例如 VPS 详情、订阅页、监控/Target 页面或 VPS lifecycle workbench。

性能衰退、路由质量、IP 质量、超售趋势等智能证据是高价值后续能力，但不阻塞当前闭环。后续应作为新的 evidence source 接入决策记录，而不是替代决策记录本身。

## Confirmed Baseline

- Phase 1 已交付只读 API：
  - `GET /api/asset-decisions/overview`
  - `GET /api/asset-decisions/groups?view=&renew_within_days=`
  - `GET /api/asset-decisions/groups/{group_id}?renew_within_days=`
- 自动组覆盖 `renewal_attention`、`cancellation_attention`、`region_portfolio`、`provider_portfolio`、`cost_pressure`、`evidence_gap`。
- 当前页面第一主 surface 是 `资产组合决策` / `决策组列表`，单台待处理队列已降级为辅助区。
- 取消/退役执行必须回到 VPS 详情 lifecycle workbench。
- 现有规范和前端只认可续费窗口 `30/60/90`，后端当前允许 `1..365`，需要收口。

详见 `phase1-review.md`。

## Requirements

- 新增持久化资产组合决策记录，但不把自动组 ID 当长期外键。
- 决策记录必须保存：
  - 标题、目标/备注、状态。
  - 来源自动组 ID、组类型、视图、scope key/label 和续费窗口。
  - 组级证据快照。
  - 成员 VPS、建议角色/动作、用户决定角色/动作、成员理由、成员证据快照。
- 创建记录时以后端重新计算的当前组详情为事实源；如果自动组不存在，返回 404。
- 保存证据源失败时不生成记录，避免把证据缺失保存成真实判断。
- 列表页展示已保存组合决策，用户可以回看详情并更新状态。
- `/asset-decisions` 继续保留 Phase 1 自动组、续费 evidence 和单台队列，不恢复旧的三张单台表格主视觉。
- 续费窗口合同收紧为 `30/60/90`，非法值后端返回 400。
- 不新增危险批量执行；`cancel` / `open_cancellation_workbench` 等动作只提供明确跳转。
- 所有前端请求必须走 `web/src/lib/api.ts`，类型继续使用 snake_case。

## Non-Goals

- 不实现性能衰退、路由质量、IP 质量、超售趋势采集与评分。
- 不做批量取消、批量退役、批量迁移。
- 不直接修改 Subscription、MonitoringInstance、Target 的业务状态。
- 不引入新的前端状态库。
- 不把 VPS、订阅、监控各自页面改造成组合决策编辑入口；它们只展示提示和跳转。

## Acceptance Criteria

- [x] `phase1-review.md` 记录 Phase 1 的完成情况、边界、优势、局限和合同漂移。
- [x] PRD 改为成果导向，不再要求用户确认阶段边界。
- [ ] 新增 `design.md`，覆盖数据流、持久化、API、前端信息架构、兼容和风险。
- [ ] 新增 `implement.md`，列出执行顺序、验证命令和回滚点。
- [ ] 后端新增资产组合决策记录 schema、domain、store、handler、router/bootstrap wiring 和测试。
- [ ] 前端新增 API helper、类型和 `/asset-decisions` 保存/回看/推进状态 UI，并保留 Phase 1 主工作台。
- [ ] 测试覆盖后端 domain/store/handler/router/bootstrap、前端 API/Page 关键路径。
- [ ] 质量检查通过或明确记录未能运行的检查。
