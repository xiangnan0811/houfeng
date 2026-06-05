# 资产组合决策中枢 Phase 1

## Goal

把 `/asset-decisions` 从单台 VPS 续费 / 待评估队列升级为候风的资产组合决策工作台骨架。

Phase 1 的用户价值是：当用户拥有多台 VPS 时，可以按续费压力、取消联动、同区组合、同服务商组合、预算压力和资料缺口查看“需要组合判断的资产组”，而不是在 VPS 库存、订阅队列、服务商目录之间来回人工拼接证据。

## Requirements

- 新增登录保护的只读 center API：
  - `GET /api/asset-decisions/overview`
  - `GET /api/asset-decisions/groups?view=&renew_within_days=`
  - `GET /api/asset-decisions/groups/{group_id}?renew_within_days=`
- 自动派生决策组，不新增 migration，不新增持久化状态，不写入 VPS / Subscription / MonitoringInstance / Target。
- 自动组覆盖：
  - `renewal_attention`：续费窗口内且需要业务判断的 VPS。
  - `cancellation_attention`：取消 / 过期 / 迁移状态割裂或仍有运行对象的 VPS。
  - `region_portfolio`：同国家 / 地区 / 城市下 2 台及以上 VPS，用于同区取舍。
  - `provider_portfolio`：同服务商下 2 台及以上 VPS，用于服务商组合比较。
  - `cost_pressure`：预算风险、汇率异常、较高成本且服务承载薄弱的 VPS 组合。
  - `evidence_gap`：缺订阅、缺监控关联、缺服务 / 域名上下文或基础资料不足的组合。
- 自动组 `group_id` 必须稳定、确定性派生，例如 `adg_auto_<12hex>`；详情接口重新计算组列表并按 ID 查找。
- 组级摘要必须聚合 VPS 数量、生命周期 / 用途 / 决策分布、成本、续费、取消联动、服务 / 域名 / Target、监控关联、异常和 evidence chips。
- 组详情成员必须展示 VPS 基础事实、主订阅、服务 / 域名 / Target / 监控摘要、建议角色、建议动作和 evidence chips。
- `/asset-decisions` 前端主视觉升级为“资产组合决策”，第一主 surface 是“决策组列表”，主 tabs 覆盖 `需要决策`、`续费取舍`、`同区比较`、`服务商组合`、`预算压力`、`资料缺口`、`单台队列`。
- 点击决策组打开详情 drawer，展示组内 VPS 对比和单台处理入口。
- 页面底部保留现有单台待处理队列；单台续费决策仍使用 `AssetDecisionWorkPanel` 与 `PATCH /api/vps/{id}`，取消 / 退役仍跳转 VPS detail lifecycle workbench。
- 续费候选表降级为 `RENEWAL EVIDENCE` 次级证据区，视觉权重低于组合工作台。
- 轻量融合其他页面：
  - Dashboard 资产 lane 深链到 `/asset-decisions?view=...`。
  - VPS 页提供进入组合决策的入口，不改变 VPS 库存主路径。
  - 订阅页只展示需要资产判断的链接，不在订阅页修改 VPS 决策。
  - 服务商页提供查看该服务商组合决策入口。
- 更新项目规范，明确 AssetDecisionsPage 主体从单台 VPS 队列升级为组合决策组 + 单台辅助队列。

## Constraints

- VPS 仍是业务状态主体；Subscription、MonitoringInstance、Target 只作为证据来源。
- Phase 1 不新增数据库表、不新增用户保存的手工决策组、不新增批量执行。
- Phase 1 不承诺 CPU / IO / 路由 / IP 质量 / 超售智能判断；这些只能作为后续 Phase 2/3 能力。
- 任何会修改 Subscription、MonitoringInstance、Target 或执行取消 / 退役的动作都不属于本阶段。
- 订阅 evidence 不可用时，不能把所有 VPS 误报为缺订阅；应显示错误或 evidence unavailable。

## Acceptance Criteria

- [ ] 后端新增 `assetdecisions` 领域类型和只读 repository 接口，并通过 `internal/center/store/asset_decisions.go` 聚合现有表。
- [ ] `/api/asset-decisions/overview` 返回组合工作台摘要。
- [ ] `/api/asset-decisions/groups` 支持合法 `view` 和 `renew_within_days` 查询，拒绝非法 view / window。
- [ ] `/api/asset-decisions/groups/{group_id}` 能返回稳定自动组详情，缺失组返回 404。
- [ ] Router / bootstrap 显式 wiring 完成，`/api/asset-decisions/*` 不落 SPA fallback。
- [ ] `/asset-decisions` 页面渲染“资产组合决策”、tabs、决策组列表和组详情 drawer。
- [ ] 切换 tabs 调用正确 query 并展示组数量。
- [ ] 组详情展示组内 VPS 对比、成本、服务 / 域名 / Target、监控关联、建议动作和入口。
- [ ] 单台队列仍可更新 `renewal_decision`，提交 payload 与现有行为一致。
- [ ] 续费 evidence 失败不影响已加载的组合工作台，也不误报缺订阅。
- [ ] Dashboard、VPS、Subscriptions、Providers 的入口融合完成且不改变各自主职责。
- [ ] 后端 store / handler / router / bootstrap 测试覆盖成功、错误和边界。
- [ ] 前端 API / 页面测试覆盖主要交互。
- [ ] 更新 `.trellis/spec/web/state-and-data.md`、`docs/design/v2-houfeng/component-spec.md`，必要时补充后端规范。
- [ ] 运行 Go / Web 相关验证；用户可见 UI 变更完成桌面与移动端 visual sanity。

## Notes

- 本任务是复杂任务，必须有 `design.md` 和 `implement.md` 后才能 `task.py start`。
- 既有 active tasks 与 main checkout 不作为本次实现位置；本任务在 `.worktree/asset-decisions-phase1` 隔离 worktree 内实施。
