# 资产组合决策执行编排工作台设计

## Backend Design

- 在 `internal/center/assetdecisions` 增加 execution plan 类型与纯函数评估器。
- `ApplyExecutionReadback` 在成员 readback 完成后继续派生成员 `ExecutionPlan`，再聚合记录级 `ExecutionPlan`。
- `ApplyExecutionReadbackToSummaries` 继续使用批量 facts 与 members 派生 summary plan，不能逐条调用 `GetRecord`。
- 后端只返回语义字段，不返回前端 URL：
  - lane: `cancel_retire | migration | keep_observe | evidence | review`
  - step kind: `open_cancellation_workbench | open_vps_detail | open_subscription_context | review_record`
  - tone: `critical | alert | notice | normal | neutral`
  - summary、step_label、issue_count、blocked、actionable
- 记录级 plan 聚合 lane counts、actionable_count、blocked_count，并根据 drift/blocked/needs_evidence/open/aligned/inactive 给出中文 summary。
- 评估只消费 `RecordMember`、`MemberExecutionReadback`、当前 facts 和既有 issue；不得新增表、runtime facts detail 或 agent 性能数据依赖。

## Frontend Design

- 在 `web/src/lib/types.ts` 添加 record/member execution plan 类型；现有 API helper 名称不变。
- `AssetDecisionsPage` 记录列表新增 plan summary 与 lane/actionable count 展示，保留 readback 列。
- 记录详情 modal 增加执行编排 section：
  - 顶部 summary 加 `执行计划` 指标。
  - lane board 使用密集 workbench 布局，按 lane 展示成员。
  - 每个成员展示 decided action、readback、plan summary、current facts、issue chips、CTA、快速跟进按钮。
  - 保留既有成员明细表作为证据快照与详细跟进表单。
- 前端本地映射 step kind 到 URL：
  - cancellation workbench、subscription context、VPS detail。
  - `review_record` 不自动跳转执行，只提示留在记录详情复核并提供 VPS detail 辅助入口。
- 快速跟进只复用现有 `patchAssetDecisionRecord` 的 member followup payload。

## Compatibility

- API 响应为 additive field，旧客户端可忽略。
- 无 migration，无 endpoint 变更。
- 记录成员决定保持历史不可变；修正判断通过新记录表达。
- completed 只代表人工状态；execution readback/plan 仍可指出当前事实漂移。

## Risks

- 执行编排被误解为真实执行：UI 文案和按钮必须表达“下一步入口/跟进”，不写“批量执行”。
- 后端前端耦合：后端返回 step kind，不返回 route。
- 页面主次倒置：执行编排只在记录 surface 内增强，不改变自动组作为主 surface 的定位。
- 横向溢出：lane board 使用响应式 grid/flex，不用更宽表格承载全部信息。
