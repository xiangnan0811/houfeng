# 资产组合决策闭环工作台收敛

## Goal

在已经存在的资产组合决策能力上收敛 `/asset-decisions` 的主工作流，让用户从跨页面上下文进入后，能沿着一条清晰路径完成：

1. 识别当前最值得处理的组合问题。
2. 打开自动组 / 场景模板 / 自定义组合 / 已保存记录。
3. 从自动组或模板沉淀自定义组合，或直接保存一次组合决策记录。
4. 在记录中推进成员跟进。
5. 通过执行回读判断当前事实是否已经对齐、漂移、阻塞或缺证据。

本阶段不是新增智能执行系统，而是在项目当前真实能力基础上，把自动组 read model、场景模板、自定义组合、决策记录、执行回读和单台辅助队列串成可理解、可操作、可验证的闭环工作台。

## Confirmed Current Facts

- 仓库当前在 `origin/main` / `v0.42.0` 上已经存在资产决策核心层：
  - 后端 domain：`internal/center/assetdecisions/`。
  - 后端 store：`internal/center/store/asset_decisions.go`、`asset_decision_manual_groups.go`、`asset_decision_scenario_templates.go`。
  - 后端 handler/router：`internal/center/http/handlers/asset_decisions.go`、`internal/center/http/router.go`。
  - 前端页面：`web/src/pages/AssetDecisionsPage.tsx`。
  - 前端组件：`AssetDecisionWorkPanel`、`AssetDecisionRenewalTable`。
- 当前 `/asset-decisions` 已经有自动组列表、场景模板、自定义组合、已保存组合决策、记录执行回读、续费 evidence、单台待处理队列和跨页面深链承接。
- 当前问题不是缺基础 API，而是页面的多 surface 已经纵向堆叠：自动组、模板、自定义组合、记录、续费 evidence、单台队列都存在，但缺少一个首屏闭环导览和统一优先级解释。用户进入页面后仍需要自己判断“下一步应该点哪里”。
- `.trellis/spec/backend/database-guidelines.md` 已明确三层边界：
  - 自动组是只读派生 read model。
  - 手工组合是 scenario layer，只保存场景、成员意图和备注。
  - 决策记录是 memory layer，只保存用户判断、证据快照和执行回读。
  - 三者都不能成为 VPS / Subscription / MonitoringInstance / Target 的第二套状态机。
- `.trellis/spec/web/state-and-data.md` 与 `docs/design/v2-houfeng/component-spec.md` 已明确：
  - 第一主 surface 仍是决策组列表。
  - 场景模板和自定义组合位于自动组与记录之间。
  - 已保存记录是 memory/readback surface。
  - 单台队列和 renewal evidence 是辅助层级。
- 代码中已有可实际修复的小瑕疵，例如 `AssetDecisionsPage.tsx` 的 `submitRecordSave` 成功后重复调用 `setOpenState('record_id', record.record_id)`。

## Requirements

- 增加“闭环导览 / next-work surface”：
  - 位于页面 summary 和自动组列表之间或与自动组列表头部合并。
  - 只从当前已加载的自动组、场景模板、自定义组合、决策记录、执行回读和上下文筛选中派生。
  - 展示当前最值得处理的 3-6 个工作项，例如 drift 记录、blocked 记录、needs_evidence 记录、当前上下文下的自动组、进行中的自定义组合、可启动的模板。
  - 每个工作项必须明确来源层级、问题摘要、下一步入口，并能打开对应 group/manual group/record/template。
- 收敛首屏信息架构：
  - 保持 `资产组合决策` 标题和自动组为第一主语义。
  - 避免继续把模板、自定义组合、记录做成彼此割裂的“平铺清单”体验。
  - 用户从 Dashboard/VPS/订阅/服务商/监控/Target 深链进入时，首屏能看到上下文筛选 chip 和与该上下文相关的下一步。
- 强化闭环状态摘要：
  - 在不新增后端状态机的前提下，前端聚合显示记录 readback 状态分布、成员跟进未关闭数量、自定义组合进行中数量、当前自动组数量和资料缺口/预算压力数量。
  - 聚合只用于展示和排序，不自动 PATCH record status，也不触发 VPS / Subscription / MonitoringInstance / Target 写请求。
- 改善“从发现到记忆”的路径：
  - 自动组详情中的“创建自定义组合”和“保存为决策记录”要被工作台导览承接，保存成功后明确进入记录详情。
  - 自定义组合详情中的“保存为决策记录”和“另存为模板”要保留，且入口文案表达清楚“组合意图 -> 一次决策记录”的关系。
  - 记录详情应继续显示执行回读和成员跟进；漂移/阻塞/缺证据只作为复核证据，不自动执行。
- 改善 URL 与状态一致性：
  - 继续支持 `group_id/manual_group_id/record_id/template_id` 打开状态，打开对象只读取不创建。
  - 修复已发现的重复 open-state 更新。
  - 清除上下文筛选不得清掉打开对象，关闭对象不得清掉上下文筛选，除非当前行为已有明确相反约定并有测试保护。
- 错误边界保持局部：
  - 自动组失败不影响记录/自定义组合/单台队列。
  - 模板失败不影响自动组、记录、自定义组合。
  - 记录失败不影响自动组发现。
  - 单台队列和 renewal evidence 继续作为辅助区独立加载。
- 更新规范：
  - 把“闭环导览 / next-work surface”和排序边界写入 web spec 与 v2 component spec。
  - 后端 spec 只在实际新增或调整 API/read model 时更新；如果本阶段纯前端派生，不凭空扩写后端合同。

## Acceptance Criteria

- [ ] `/asset-decisions?view=needs_decision&renew_within_days=30` 首屏展示“下一步 / 闭环导览”类 surface，能从当前已加载数据中呈现高价值工作项。
- [ ] 导览工作项至少覆盖记录 `drift`、`blocked`、`needs_evidence`、自动组、进行中自定义组合、可用模板中的真实已加载数据；无对应数据时显示克制空态，不编造工作项。
- [ ] 点击导览项能打开对应 `record_id`、`group_id`、`manual_group_id` 或 `template_id`，并保持 URL-state 可复制。
- [ ] 页面 summary 或导览中能看出闭环状态：自动组发现、进行中自定义组合、已保存记录、执行回读问题和资料缺口的数量关系。
- [ ] 从自动组保存记录、从自定义组合保存记录、从模板创建组合的现有流程继续可用，成功后仍打开正确详情。
- [ ] 记录详情继续支持状态推进和成员跟进；成员跟进 PATCH payload 与现有合同一致。
- [ ] readback `drift` / `blocked` / `needs_evidence` 展示不会触发任何 VPS、Subscription、MonitoringInstance、Target 写请求。
- [ ] 修复重复 `setOpenState('record_id', ...)` 等当前代码中发现的实际状态一致性问题，并增加或调整测试覆盖。
- [ ] `AssetDecisionsPage.test.tsx` 覆盖闭环导览渲染、导览项打开目标详情、上下文筛选保留、无数据空态、readback 不触发业务写请求。
- [ ] `web/src/lib/api.test.ts` 仅在 API helper 或 query 发生实际变化时更新；不做空泛改动。
- [ ] 视觉 sanity 检查桌面与移动端：闭环导览、自动组列表、记录详情和单台队列不产生页面级横向溢出。
- [ ] 更新 `.trellis/spec/web/state-and-data.md` 与 `docs/design/v2-houfeng/component-spec.md`，只记录本阶段实际实现的合同。

## Notes

- 本任务以项目当前真实实现为准：不假设后端已经有新的“统一待办 API”，也不在没有必要时新增后端 endpoint。
- 本任务可以是前端主导；如果实现中发现需要后端补只读字段，必须先证明现有 API 无法派生，并在 `design.md` 中补充合同。
- 由于用户希望阶段任务不宜过小，本任务范围包含工作台闭环导览、状态收敛、URL/open-state 一致性、测试和规范更新，但不包含后续智能观测能力。

## Out of Scope

- 不实现 IP 质量、路由质量、性能衰退、CPU/IO 趋势、超售判断。
- 不做批量 keep / migrate / cancel。
- 不自动取消、迁移、退役 VPS。
- 不自动修改 Subscription、MonitoringInstance、Target、Service、Domain。
- 不新增第二套资产执行状态机。
- 不新增持久化自动组表。
- 不把执行回读用于自动 PATCH record status。
- 不重构整个 `AssetDecisionsPage.tsx` 为多文件架构，除非实现过程中出现无法控制的重复或测试困难；本阶段优先做可控收敛。
