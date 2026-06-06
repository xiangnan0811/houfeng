# 资产组合决策场景推进链路

## Goal

把 `/asset-decisions` 已有的自动决策组、场景模板、自定义组合、已保存决策记录、执行回读和执行编排串成清晰的场景推进链路，让用户能理解一次组合决策从“发现问题”到“形成比较场景”、再到“保存判断”和“执行回读跟进”的完整路径。

本任务不新增智能评分、不新增后端状态机、不新增业务执行动作；它聚焦页面信息架构、导览、来源连续性和可验证的前端交互。

## Confirmed Facts

- 当前 `main` 已发布到 `v0.47.0`，本任务从干净的 `origin/main` 创建 worktree 分支 `worktree/asset-decisions-scenario-progression`。
- `/asset-decisions` 已经具备自动组、场景模板、自定义组合、保存记录、执行回读、执行计划、底部单台队列和续费 evidence。
- 当前页面的主要不足不是缺少数据能力，而是各 surface 仍偏并列：用户需要自己理解“发现 -> 比较 -> 判断 -> 执行回读”的路径。
- `.trellis/spec/web/state-and-data.md` 与 `docs/design/v2-houfeng/component-spec.md` 已明确 `/asset-decisions` 是组合决策中枢，不是 VPS 队列或订阅续费页的复刻。
- IP 质量、路由、性能衰退、CPU/IO、超售判断依赖 agent 与观测语义成熟，当前阶段明确不纳入。

## Requirements

- 保持自动决策组为首屏主 surface，不能让记录、模板、单台队列或续费 evidence 反向成为主视觉。
- 新增或增强一个轻量“决策路径 / 场景推进”导览，用已加载的自动组、自定义组合、模板和记录事实派生，不新增 API。
- 自动组详情必须更清楚地区分“直接保存为决策记录”和“先创建自定义组合继续比较”两条路径。
- 自定义组合详情必须展示组合推进状态，例如目标、成员、成员意图、证据缺口、是否适合保存记录。
- 已保存记录详情必须强化来源连续性和执行回读闭环，让用户看清保存时判断、当前事实和下一步导览之间的关系。
- 场景模板、自定义组合、决策记录的视觉关系必须表达顺序：模板启动场景，自定义组合承接真实比较，记录保存判断和跟进记忆。
- 新增 CTA 只能打开已有详情、已有表单或已有本地深链；不得直接执行 VPS、Subscription、MonitoringInstance 或 Target 写动作。
- 底部单台队列和续费 evidence 必须保持辅助定位。
- 更新 Trellis / v2 设计规范，记录场景推进链路的展示层级与禁止事项。
- 测试必须覆盖新增导览、自动组到场景/记录的路径提示、自定义组合推进状态、记录来源连续性，以及新增 CTA 不触发业务对象写请求。

## Acceptance Criteria

- [ ] `/asset-decisions?view=needs_decision&renew_within_days=30` 渲染“资产组合决策”主工作台，首屏仍以自动决策组为主体。
- [ ] 页面展示清晰的决策推进链路，能从当前加载数据派生发现、比较、保存、执行回读阶段状态。
- [ ] 自动组详情展示场景推进建议，并保留创建自定义组合与保存决策记录入口。
- [ ] 自定义组合详情展示组合推进状态，能提示目标、成员、成员意图、证据缺口和保存记录准备度。
- [ ] 记录详情展示来源连续性、执行计划摘要和执行回读闭环，不隐藏 `drift` / `blocked` / `needs_evidence`。
- [ ] 场景模板、自定义组合、记录的主页面区块表达正确层级，不盖过自动组主 surface。
- [ ] 新增 UI 不调用任何 VPS / Subscription / MonitoringInstance / Target 写接口。
- [ ] `AssetDecisionsPage` 相关 tests 覆盖新增行为并通过。
- [ ] Web lint、test、build 和 `git diff --check` 通过。
- [ ] 桌面与移动端视觉 sanity 无横向溢出，续费 evidence 和单台队列仍低于组合工作台。

## Out of Scope

- 新增或修改后端 endpoint。
- 新增 migration 或持久化执行状态。
- 批量执行、自动取消、自动迁移、自动改订阅或自动改监控/Target。
- 自动 PATCH record status 或成员 followup。
- 修改 record member 的 historical `decided_action` / `decided_role`。
- 引入 IP 质量、路由质量、CPU/IO、性能衰退、超售判断。
- 大范围重做 VPS、订阅、服务商或 Dashboard 页面。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
