# VPS first control plane

## Goal

将 Houfeng 的资产体验重构为 VPS-first control plane：VPS 是唯一业务主体，订阅、监控实例、接入 agent、服务、域名、资产决策都围绕 VPS 详情页展开。人工业务状态只保留在 VPS 上；订阅和监控实例只保留账单事实、运行事实和观测事实。

## Requirements

- VPS 创建流程必须是短表单，不再要求用户在新建时选择生命周期、用途状态或续费决策；默认由后端写入 VPS 主体状态。
- VPS 详情页必须成为主工作台：展示基础身份、订阅证据、监控/接入证据、服务/域名上下文和下一步动作。
- 用户在 VPS 详情页没有订阅时，可以直接快速创建订阅，不需要跳转到订阅列表再回来。
- 用户在 VPS 详情页没有监控实例时，可以直接为该 VPS 创建监控实例并进入 agent 接入流程；已有监控实例关联降级为高级/次级操作。
- 订阅表单不再要求用户填写订阅状态；订阅页面作为账单事实列表，不再把状态割裂当作主流程。
- 监控实例创建不再要求重复填写名称、地区、城市、供应商、标签、备注等 VPS 已有信息；从 VPS 创建时默认继承或派生。
- 监控页面保留健康、心跳、异常、运行控制、绑定冲突等观测能力，但不再作为普通 agent 接入主入口。
- 取消/退役工作台只在 VPS 进入取消/迁移/归档/冲突等需要协调时突出出现；刚创建的普通 VPS 不应显示危险主 CTA。
- Dashboard 与 Asset Decisions 的主任务队列按 VPS 聚合，优先引导从 VPS 建立资产、订阅和观测链路。
- 更新当前产品语义文档，废止“VPS 和 MonitoringInstance 只显式 link 且互不改状态”的主导原则；不修改 frozen v1 baseline 文档来追认新行为。

## Acceptance Criteria

- [ ] 后端提供 `POST /api/vps/{vps_id}/monitoring-instances`，创建监控实例、从 VPS 派生默认身份字段、创建 active link，并返回可继续进入 onboarding 的记录。
- [ ] 后端提供 `POST /api/vps/{vps_id}/subscriptions`，创建订阅账单事实；普通前端创建流不再传入或显示订阅状态。
- [ ] 新建 VPS 表单没有生命周期、用途状态、续费决策控件；创建后的 VPS 仍有有效默认业务状态。
- [ ] VPS 详情页空订阅/空监控时显示就地快速创建入口，并成功刷新详情证据。
- [ ] 从 VPS 快速创建监控实例后，页面进入对应监控实例 onboarding 流程，且监控实例字段继承 VPS 名称/服务商/地区/城市/标签等可用上下文。
- [ ] 订阅页面新建/编辑表单不展示“状态”控件；从 `?vps_id=&create=1` 打开时仍预填 VPS。
- [ ] 监控页面普通主 CTA 不再强调“创建并接入”；监控详情能显示关联 VPS 并提供返回 VPS 的路径。
- [ ] 普通刚创建 VPS 的详情首屏不突出“打开取消/退役工作台”；取消/迁移/冲突状态仍可进入工作台。
- [ ] 当前产品语义文档、类型、API client、后端 tests、前端 tests 同步更新。
- [ ] 目标验证命令通过或记录明确阻塞：后端 targeted tests、前端 targeted tests、`make verify-go`、`cd web && npm run lint`、`cd web && npm run test -- --run`、`cd web && npm run build`、`git diff --check`。

## Notes

- Scope is intentionally broad and breaking. The project has no existing user compatibility burden.
- Runtime observability states remain: health, binding, heartbeat, sync freshness, pause, and maintenance are not VPS business states and should not be removed.
