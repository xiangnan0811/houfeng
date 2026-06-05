# Phase 1 Review

## What Was Delivered

Phase 1 已经把 `/asset-decisions` 建成资产组合决策的可用骨架。它不再是单台 VPS 续费队列，而是以后端只读聚合 API 为主语义来源的组合工作台。

已确认交付：

- 后端只读领域：`internal/center/assetdecisions`。
- 后端 API：
  - `GET /api/asset-decisions/overview`
  - `GET /api/asset-decisions/groups?view=&renew_within_days=`
  - `GET /api/asset-decisions/groups/{group_id}?renew_within_days=`
- 只读 store 聚合：VPS、Provider、Subscription、Service、Domain、VPS-Monitoring link、MonitoringInstance、Target。
- 自动派生组：续费取舍、取消联动、同区比较、服务商组合、预算压力、资料缺口。
- 前端主页面：`AssetDecisionsPage` 使用“资产组合决策”标题，第一主 surface 是“决策组列表”。
- 组详情：展示成员 VPS 对比、订阅、服务/域名/Target、监控、建议角色、建议动作和 evidence chips。
- 单台续费处理：保留 `AssetDecisionWorkPanel` 和 `PATCH /api/vps/{id}`，位置降级为组详情和底部单台辅助队列。
- 跨页面入口：Dashboard、VPS、订阅、服务商、监控/Target 支持面已有深链或入口。
- 发布状态：Phase 1 已合入主线并发布到 v0.36.0。

## Evidence From Current Code And Specs

- `.trellis/spec/backend/database-guidelines.md` 明确当前资产决策是只读 portfolio read model，不新增持久化状态，不成为第二套业务状态机。
- `.trellis/spec/web/state-and-data.md` 明确自动组只读派生，单台决策仍使用现有 VPS PATCH，取消/退役执行必须回到 VPS lifecycle workbench。
- `internal/center/assetdecisions/types.go` 已定义 group type、view、suggested role/action、evidence kind、overview、group summary/detail/member，以及自动组派生逻辑。
- `internal/center/store/asset_decisions.go` 已实现现有表聚合，不依赖逐台 runtime facts detail endpoint。
- `internal/center/http/handlers/asset_decisions.go` 已提供 overview、groups list、group detail handler 和错误映射。
- `web/src/pages/AssetDecisionsPage.tsx` 已形成组合工作台、续费 evidence、单台辅助队列和组详情处理入口。
- `web/src/pages/AssetDecisionsPage.test.tsx` 与 `web/src/lib/api.test.ts` 已覆盖页面主 surface、tabs query、组详情、单台 PATCH payload、API helper 等关键合同。

## What Works Well

- 职责划分已经变清楚：订阅页处理订阅事实，VPS 页处理库存和单台生命周期，资产决策页处理跨 VPS 组合判断。
- 自动组覆盖了用户提出的核心场景：
  - 同一区域多台 VPS 的保留/迁移/退役取舍。
  - 同服务商多台 VPS 的组合风险与成本比较。
  - 续费窗口内需要人工判断的 VPS。
  - 取消、过期、迁移状态割裂或仍有关联运行对象的 VPS。
  - 预算压力、闲置付费、资料缺口和异常关联。
- 源数据失败边界正确：查询失败不被误报成缺证据，避免用户基于假 evidence 做出取消判断。
- 取消/退役执行边界正确：组合页不直接执行危险动作，而是跳转 VPS lifecycle workbench。
- 页面主次关系正确：组合组是主工作台，续费 evidence 和单台队列是辅助面。

## Limitations

- 决策不能沉淀。用户看完一个自动组后，无法保存“本轮组合判断”的结论、理由和后续动作。
- 自动组不是长期对象。`adg_auto_<12hex>` 由当前事实派生，适合详情查询，不适合作为用户长期追踪对象的唯一身份。
- 建议不可反馈。用户不能接受、驳回或覆盖 `suggested_role` / `suggested_action`，系统也无法学习用户判断。
- 缺少证据快照。当前事实变化后，系统无法解释历史决策当时基于什么成本、订阅、服务、监控或资料质量做出。
- 缺少组合目标。系统不能表达“德国主备”“美国日用 + 容灾”“高配机器预算压缩”“服务商分散”等用户真实策略。
- 缺少执行跟踪。组合判断之后，用户仍需要自己记住下一步去 VPS 详情、订阅页或监控页处理什么。
- 暂无性能、路由、IP 质量、超售和长期服务质量趋势。Phase 1 只使用已有资产账本和监控摘要证据。

## Contract Drift To Resolve

`renew_within_days` 的产品合同存在轻微不一致：

- 前端 `AssetDecisionsPage` 只提供 `30/60/90`。
- `.trellis/spec/web/state-and-data.md` 和 `.trellis/spec/backend/database-guidelines.md` 也写明当前窗口是 `30/60/90`。
- 后端 `assetdecisions.ValidateFilters` 当前允许 `1..365`。

这不是当前页面主路径的功能问题，但 Phase 2 应明确后端是否收紧为固定窗口，或把自定义续费窗口正式产品化。

## Phase 2 Implications

Phase 2 不应该推翻 Phase 1 的读模型，而应该在它旁边增加“可沉淀的用户决策层”：

- 自动组继续作为发现入口。
- 持久组或决策记录成为长期追踪对象。
- 保存决策时记录证据快照。
- 执行动作仍回到现有对象主路径。
- 性能/路由/IP/超售趋势可以作为后续更强证据层，但不宜在没有决策记忆之前抢先成为 Phase 2 主线。
