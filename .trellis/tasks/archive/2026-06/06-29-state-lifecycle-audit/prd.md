# Audit and repair state lifecycle consistency

## Goal

对本项目中会影响 VPS 管理、订阅/续费、监控阈值、自动化决策和用户体验的各种状态与状态变化做全链路审查，找出状态建模、排他性、阈值、页面呈现、数据流与决策逻辑中的不合理或错误；随后对低风险、高确定性的缺陷直接修复并复审证明，剩余需要产品级闭环的新能力拆分为明确后续任务。

## User Value

- 降低状态/阈值错误导致的误删、误取消、误迁移、误判质量、误触发续费或通知风险。
- 让用户在页面上理解资产、订阅和监控状态时看到一致、可信、可操作的信息。
- 为后续实施提供问题清单、优先级、影响链路和修复方向，并让本轮可安全修复的状态链路实际收口。

## Scope

- VPS 生命周期与关注/决策状态：观察、保留、取消、迁移、归档等，以及它们对列表、详情、成本、续费、监控和通知的影响。
- 订阅与续费状态：自动续费、手动续费、赠送、抽奖、到期、续费提醒、成本归属与预算影响。
- 监控与阈值状态：agent/center 采集、状态计算、阈值配置、前端呈现、告警/决策使用。
- 跨层链路：数据库 schema/迁移、Go service/API、contracts、agent sync、React 页面与测试。
- 页面实际效果：状态标签、筛选、操作入口、空值/过期/冲突状态展示与用户决策风险。
- 本轮直接修复范围：
  - 订阅列表 `renewal_decision` 筛选与 VPS renewal decision 枚举对齐。
  - 普通 VPS PATCH 不再绕过取消、迁移、归档等受控 lifecycle 流程状态。
  - 前端监控阈值默认值、层级和后端 IncidentDefaults 对齐，并优先使用 settings 响应。
  - 后端 IOWait、Load5、heartbeat stale 阈值使用用户设置。
  - 暂停、维护、退役、归档监控实例不再产生新的 heartbeat stale active incident。
  - IP 质量 stale 窗口从硬编码 7 天转为可配置/派生合同。
  - 对页面迁移文案做最小收口，避免暗示不存在的迁移工作台。

## Constraints

- 本任务先审查，再按 TDD 实施低风险、高确定性的修复。需要完整产品设计的新功能只做后续任务拆分，不在本轮半实现。
- 以当前代码、迁移、测试、`.trellis/spec/` 和 `docs/design/current/` 为事实来源。
- 审查必须优先从用户体验和误决策风险出发，再追踪到后端和数据模型。
- 发现问题时需要说明影响链路、风险、建议方案与验证方式。
- 所有行为变化先写失败测试并观察失败，再写实现。
- 不在本轮实现完整迁移工作台、完整 lifecycle action 审计重构、订阅来源拆分或新的大规模页面 IA。

## Acceptance Criteria

- [ ] 建立状态清单，覆盖 VPS、订阅、监控、通知/决策相关状态和主要阈值。
- [ ] 建立关键状态转换图或表，明确合法转换、排他关系、未定义/冲突状态。
- [ ] 审查后端数据库、API/service、contracts/agent、前端页面和测试中的状态定义是否一致。
- [ ] 审查状态对自动化决策、提醒、成本预算、页面筛选/展示和用户操作的影响。
- [ ] 对每个问题给出严重程度、证据路径、用户影响、根因判断、可行解决方案和建议验证。
- [ ] SLC-01 修复：`/api/subscriptions?renewal_decision=migrate` 不再 400，`manual` 这类订阅 renewal mode 不被误认为 VPS renewal decision。
- [ ] SLC-02/SLC-04 最小修复：普通 VPS PATCH 不能直接进入 `to_cancel`、`cancelled`、`to_migrate`、`archived`，且测试覆盖受控流程边界；完整审计矩阵形成后续任务。
- [ ] SLC-05/SLC-06/SLC-07 修复：后端 settings、evaluator、前端阈值线/趋势使用一致阈值合同。
- [ ] SLC-08 修复：非运行态监控实例不会被 heartbeat stale sweep 生成新 active incident，行为有测试覆盖。
- [ ] SLC-09 修复：IP 质量 stale 窗口不再在 read model 中固定为 7 天，设置/API/前端类型同步。
- [ ] SLC-03/SLC-12 收口：页面和执行计划文案不再承诺“打开 VPS 详情推进迁移”这类不存在的工作流；完整迁移工作台拆为后续任务。
- [ ] 复审报告逐条标明已修复、已缓解、需后续产品任务的项，并列出验证命令结果。

## Out of Scope

- 不实现完整迁移工作台及旧/新 VPS cutover step。
- 不重构所有 lifecycle action 审计表，也不回填历史线上数据。
- 不拆分订阅 renewal mode 与账单来源的完整新模型。
- 不提交 PR、不执行发布流程，除非用户后续明确要求。

## Open Questions

- 暂无阻塞问题。用户已授权创建 Trellis 任务，并已授权后续实施和复审，无需再次询问。
