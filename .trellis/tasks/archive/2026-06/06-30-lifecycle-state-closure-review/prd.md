# Review lifecycle state closure

## Goal

再次全方位审查上一轮生命周期状态闭环修改，确认先前发现的问题均已正确修复，且本次修复没有引入新的状态、状态变化、决策链路、页面体验或破坏性处理问题。本轮目标是审查和出具证据化结论，不直接实施修复；如发现问题，只给出可执行解决方案和破坏性影响说明。

## User Value

- 用户在 VPS、订阅、监控、Target、归档、资产决策和设置页面看到的状态应无歧义，并能对应真实系统行为。
- 由于项目当前没有实际用户，可以接受移除不必要兼容或破坏性清理，但必须明确指出其破坏性和收益。
- 维护者需要一份独立复审报告，判断上一轮提交是否可以作为状态生命周期收口的可信基础。

## Confirmed Facts

- 上一轮工作提交为 `ec00ea3 fix: close lifecycle state gaps`，随后 Trellis 归档提交 `3d1ddbe` 和 journal 提交 `2ce83db`。
- 该提交涉及 VPS 状态矩阵、`asset_scope=historical`、订阅 `renewal_mode=gift`、incident 非运行态收敛、前端标签和 Archive 页面请求、DB migration、spec 更新。
- 用户明确声明：本项目开源但当前没有任何用户，不需要为用户兼容牺牲模型正确性；破坏性处理需要明确指出。
- 用户明确授权：本项目后续遇到这种关键复杂审查/状态复审时，允许直接创建 Trellis 任务，不再在关键时刻反复询问。

## Requirements

- 对上一轮提交的 backend、frontend、DB migration、tests、spec、Trellis task artifacts 做全链路审查。
- 逐项确认上一轮目标：
  - VPS create/patch 状态矩阵是否合理、是否遗漏导入/专用 action 路径。
  - `asset_scope=historical` 与 `archived` 是否命名合理；在“无用户”前提下是否应保留兼容别名或建议破坏性移除。
  - 订阅 `gift` 是否真正与 `lottery`、`bonus`、legacy auto-renew flags、price histories 和 UI 标签区分。
  - MonitoringInstance / Target inactive convergence 是否不会制造新 active incidents、不会吞掉重要事件、不会误发通知。
  - 状态对 Dashboard、Asset Decisions、Archive、Subscriptions、Monitoring、Targets 等页面决策链路的影响是否合理。
  - 页面实际效果、mock fixtures、browser sanity warnings 是否存在会影响用户理解的问题。
- 搜索全仓库关键枚举/文案/查询分支，确认没有漏同步、重复定义、旧语义残留或隐藏反向写入。
- 运行必要验证命令；如果只审查不改代码，仍需至少运行能证明当前提交健康的检查。
- 输出 `research/state-closure-final-review.md`，列出：
  - 已确认闭环项。
  - 新发现问题或风险，按严重度排序。
  - 破坏性建议与理由。
  - 不实施原因和后续可执行方案。
  - 验证命令与结果。

## Acceptance Criteria

- [ ] 有独立复审报告，逐项对应上一轮问题并给出证据。
- [ ] 明确说明是否发现新的 blocker / regression。
- [ ] 对保留兼容别名、破坏性移除旧状态/旧 scope、历史迁移约束等给出开源无用户前提下的建议。
- [ ] 至少完成代码搜索、diff 审查、相关测试/全量验证和浏览器 sanity 复核中的合理子集；任何未运行项必须说明原因。
- [ ] 不直接实施业务修复；如发现需要改动，形成方案和影响面。
- [ ] 项目记忆已记录“关键复杂状态审查可直接创建 Trellis 任务”的预授权。

## Notes

- 本轮是审查任务，不是修复任务。若发现必须修复的严重问题，先报告并另建/延展修复计划。
