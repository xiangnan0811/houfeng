# 修复首次监控 Agent 接入死锁

## Goal

让尚无监控实例的新部署能够从现有 VPS 出发完成首个 Agent 接入，消除 VPS 详情页与监控页之间的循环空状态，使用户无需预先拥有监控实例即可进入可执行的接入流程。

## Background

- 用户在部署 `v0.79.0` 后确认：VPS 详情页只提供“关联监控”，弹窗只能选择已有监控实例。
- 当监控实例为零时，监控页空状态提示先创建 VPS，再从 VPS 详情页接入；页面虽显示“从 VPS 接入 agent”，但当前路径无法完成首次接入。
- 两个入口互相依赖已有监控实例，形成首次接入死锁。

## Confirmed Facts

- 用户已经拥有可用 VPS；本问题不是“尚未创建 VPS”的空状态。
- VPS 详情页的“关联监控”语义是关联既有实例，不能替代创建并接入 Agent。
- 监控页统计为 0 时不存在可供 VPS 详情页关联的监控实例。
- `records_v2_read` capability 会让 `/vps/:id` 进入新版 Overview；该页面只把未关联异常映射到只读 monitoring relation panel。
- Legacy VPS 详情已经实现并测试了 0/1/多 active links 的正确分流和 VPS-scoped MonitoringInstance 创建/onboarding；新版 Overview 未迁入该能力。
- `v0.79.0` 至当前 `main` 没有相关产品代码变化，缺陷仍存在于当前基线。
- 既有 Web spec 明确要求 VPS 详情承接创建并接入主流程，Monitoring 列表不复制资产创建 owner。

## Requirements

- 系统在监控实例为零、但已存在 VPS 时，必须提供至少一条从该 VPS 启动 Agent 接入的可执行路径。
- “关联已有监控实例”与“为此 VPS 接入新 Agent”必须在文案、入口和行为上清晰区分。
- 修复不能要求用户重复创建 VPS，也不能伪造或预创建监控实例来绕过空状态。
- 现有已有关联、重新接入及监控实例管理流程不得回退。
- 新版 Overview 必须遵守既有 0/1/多 active-link 分流和 VPS write ownership/idempotency 合同。
- Monitoring 页的页头动作与首次空状态动作必须统一导向未关联 VPS 库存；不得在已存在 VPS 时继续显示“创建第一台 VPS”。
- Monitoring 页继续作为运行观测扫描页，不复制 VPS-scoped MonitoringInstance 创建表单或写入 owner。

## Acceptance Criteria

- [x] 已存在 VPS 且监控实例为 0 时，用户能从可发现的入口进入该 VPS 的 Agent 接入流程。
- [x] VPS 详情页仍能关联已有监控实例，且该能力与新 Agent 接入入口不混淆。
- [x] 监控页的主要操作和空状态不会把已有 VPS 的用户引导去重复创建 VPS。
- [x] 从监控页两个 Agent 接入入口进入后，只显示未关联 VPS 的候选库存；选择 VPS 后能进入同一 VPS-scoped 创建/onboarding 流程。
- [x] 接入流程成功后，新监控实例与选定 VPS 建立正确关系，并出现在监控列表及 VPS 详情页。
- [x] 失败、取消或过期不会留下被错误视为已接入的监控实例或关联关系。
- [x] 相关前后端自动化测试覆盖“零监控实例 + 已有 VPS”的首次接入路径。

## Out of Scope

- 重新设计 Agent 协议、采集模型或监控信息架构。
- 与首次接入死锁无关的 VPS 创建、归档或监控数据展示改版。
- 在 Monitoring 页内新增 VPS 选择器、复制 MonitoringInstance 创建表单或直接拥有创建写入。

## Notes

- 任务在隔离工作树 `/home/murray/code/houfeng/.worktree/monitoring-agent-bootstrap`、分支 `fix/monitoring-agent-bootstrap` 上规划。
- 基线 Web 测试通过（205 个测试文件、1570 项测试）；Go 全量基线存在一个附件 PNG 金样摘要失败，监控相关包通过，需在实施验证时与本任务结果分开记录。
- 根因与证据见 `research/current-flow-audit.md`。
- 用户已确认把 Monitoring 页入口目标与错误空状态文案纳入本任务。
- 用户已批准方案 A：补齐新版 Overview 的 VPS-scoped 创建/onboarding 能力，复用既有表单、API 与 write ownership；Monitoring 页只把入口导向未关联 VPS 库存，不新增第二个创建 owner。
