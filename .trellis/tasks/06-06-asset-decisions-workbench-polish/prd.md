# 资产组合决策工作台体验升级

## Goal

把 `/asset-decisions` 从“功能完整但信息堆叠”的页面继续打磨成一个更成熟的资产组合决策工作台：用户打开页面后能先看见当前组合风险、下一步处理顺序和上下文筛选，再进入自动组、场景/记录、执行编排、续费证据和单台辅助队列。

本阶段不新增后端状态机、不新增 migration、不承诺 IP 质量、路由质量、性能衰退、CPU/IO 或超售判断；所有优化只消费已有 Asset Decisions API、VPS、订阅和当前页面状态。

## Confirmed Facts

- `/asset-decisions` 当前已经具备自动决策组、下一步导览、自定义组合、场景模板、已保存组合决策、执行回读、执行编排、续费证据区和单台待处理队列。
- 自动组是首屏主 surface，单台队列已经降级为底部辅助队列。
- 记录详情已经有执行编排 board，但列表页和详情页仍偏表格化，用户需要自己推断优先级、场景和执行路径。
- Dashboard、VPS、订阅、服务商、Monitoring、Targets 已经通过显式 `view` / `scenario` / `renew_within_days` / context filters 深链到资产组合决策。
- 当前前端视觉权威是 `.trellis/spec/web/index.md` 和 `docs/design/v2-houfeng/`，不应通过全局 token 或 AppShell 改动来完成局部页面 polish。

## Requirements

- 首屏必须继续以“资产组合决策”和“决策组列表”为主，不得把保存记录、续费 evidence 或单台队列提升为主视觉。
- 顶部摘要需要从单纯计数升级为“当前判断轨道”：显示组合范围、近期续费压力、执行闭环风险、资料可用性，并给出清晰的第一行动。
- 现有上下文 filters 必须更容易被理解：从其它页面深链进入时，用户应能在首屏看见筛选范围、来源场景和清除入口。
- 决策组列表需要更适合扫描：每个组应突出原因、压力、证据质量、承载/监控/成本事实和推荐下一步，避免让用户先读一排相同权重的小指标。
- 下一步导览需要更像工作队列：优先级、来源、动作对象和打开行为必须清楚；数据源失败时仍只显示局部不可用，不伪造无问题。
- 场景模板、自定义组合和已保存记录需要保持辅助定位，但要更清楚表达三者关系：模板启动场景，自定义组合承接真实比较，记录保存判断和回读。
- 记录详情中的执行编排需要更像执行导览而不是密集卡片堆叠：按 lane 展示成员、当前事实、问题 chips、CTA 和快速跟进，且不触发任何业务资产写接口。
- 续费证据区和单台队列继续低权重展示，视觉上不得抢占组合工作台。
- 桌面和移动端不得出现页面级横向溢出；表格允许在自己的 scroll region 内横向滚动。
- 更新测试和设计/Trellis spec，避免后续重新引入旧的“资产决策队列”主路径。

## Out Of Scope

- 不新增、修改后端 endpoint 或数据库 migration。
- 不新增批量执行、自动取消、自动迁移、自动 PATCH record status 或业务对象写入。
- 不修改 VPS、Subscription、MonitoringInstance、Target 的业务状态机。
- 不纳入 IP 质量、路由质量、性能衰退、CPU/IO 趋势、超售判断。
- 不重做 AppShell、Dashboard、VPS、订阅、服务商、Monitoring、Targets 的已完成入口优化。

## Acceptance Criteria

- [ ] `/asset-decisions` 首屏展示一个更清晰的 portfolio command summary，包含第一行动、组合范围、执行闭环风险、资料可用性和当前上下文。
- [ ] 自动组列表仍是主 surface，且组卡片更突出主问题、证据评估、组合压力和下一步；测试断言主 surface 未被记录/单台队列取代。
- [ ] 下一步导览展示更清楚的工作项结构；点击仍只打开 group/manual group/record/template 详情，不写业务资产。
- [ ] 场景/记录区域视觉层级低于自动组，高于续费 evidence 和单台队列，并清楚展示模板、自定义组合、记录的关系。
- [ ] 记录详情执行编排 board 展示 lane summary、成员当前事实、issue chips、CTA 和快速跟进；快速跟进只 PATCH record followup。
- [ ] 续费 evidence 和单台队列保留现有功能和 PATCH payload 合同。
- [ ] 修改后的页面在 390px 和 1440px 视口下没有页面级横向溢出，记录详情和组卡片内容不互相遮挡。
- [ ] 相关 Vitest、lint、build、Trellis validate、visual sanity 均通过或明确说明本地工具限制。
