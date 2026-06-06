# Asset decision scenario templates recommendations and deep links

## Goal

把 `/asset-decisions` 继续推进为可用的资产组合决策中枢：用户可以从真实业务场景模板启动组合判断，可以看懂系统建议的证据理由，并且能从 Dashboard、VPS、订阅、服务商、监控、Target 等页面带着上下文进入资产决策页。

本阶段不实现批量执行，不自动修改 VPS / Subscription / MonitoringInstance / Target，不引入 IP 质量、路由质量、性能衰退、CPU/IO 趋势或超售判断。

## Requirements

- 场景模板采用混合形态：
  - 系统内置模板由代码定义，稳定可版本化，不允许 PATCH。
  - 用户自定义模板可从现有自定义组合另存，落库保存模板元数据和成员 blueprint。
  - 模板只能创建或预填自定义组合，不能直接生成决策记录，不能修改资产业务对象。
- 决策建议质量增强：
  - 自动组、手工组合、成员返回只读 `decision_recommendation`。
  - recommendation 是 `evidence_assessment` 和现有 evidence chips 的中文解释层，不新增第二套评分引擎。
  - recommendation 不消费 runtime facts detail、HostSample、ProbeObservation、IP/路由/性能数据。
- 深链和筛选增强：
  - `/asset-decisions` 支持 `provider_id`、`vps_id`、`country`、`region`、`city`、`scenario`、`group_id`、`manual_group_id`、`record_id`、`template_id` URL-state。
  - 列表筛选用于筛出相关组并展示可见 chips；组详情必须保留完整组合成员，不因 `vps_id/provider_id` 深链裁剪成员。
  - `overview` 与 `groups` 对列表筛选口径保持一致。
- 跨页面入口：
  - Dashboard、VPS 列表、VPS 详情、订阅、服务商、Monitoring、Targets 页面链接到 `/asset-decisions` 时必须携带可被目标页承接的 query。
  - 目标页必须显示筛选 chip，并支持移除 chip / 清空筛选后回写 URL。
- 视觉层级：
  - 自动决策组列表仍是首屏第一主 surface。
  - 场景模板和自定义组合是 scenario surface，位于自动组与记录之间。
  - 已保存记录是 memory/readback surface，单台队列和 renewal evidence 保持辅助层级。

## Acceptance Criteria

- [ ] 后端提供模板 API：list/create/get/patch/create-manual-group，内置模板和自定义模板可区分。
- [ ] 自定义模板落库但只保存模板 blueprint，不保存当前成本、订阅、监控、服务等实时事实为长期事实。
- [ ] 从模板创建自定义组合时重新读取当前 facts；成员缺失、重复或非法输入 fail closed。
- [ ] 自动组 / 手工组合 / 成员返回 `decision_recommendation`，且建议文案可解释当前证据、风险、缺口和下一步。
- [ ] `overview`、`groups`、`manual-groups`、`records` 支持新增筛选参数，非法参数返回 400。
- [ ] `/asset-decisions` 能解析筛选与打开参数，显示 chips，并按 `group_id/manual_group_id/record_id/template_id` 自动打开对应 drawer/modal。
- [ ] 从模板创建组合、从自定义组合另存模板、从模板进入组合全流程可用。
- [ ] Dashboard/VPS/VPSDetail/Subscriptions/Providers/Monitoring/Targets 的资产决策入口带上目标页可见承接的 query。
- [ ] 深链、筛选、recommendation 展示不会触发 VPS / Subscription / MonitoringInstance / Target 写请求。
- [ ] 更新 Trellis backend/web spec 和 v2 component spec，固化模板、推荐和深链合同。

## Out of Scope

- 不做批量 keep / migrate / cancel。
- 不自动完成记录、不自动推进成员跟进状态。
- 不新增资产执行状态机，不新增持久化自动组表。
- 不实现 IP 质量、路由质量、性能衰退、CPU/IO、超售判断。
- 不把模板直接保存为决策记录；模板必须先进入自定义组合。
