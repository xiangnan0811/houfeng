# 入口探测 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

资产详情页（VPS `/vps/:id`、入口 `/targets/:id`）统一采用「判断在顶、证据居中、配置进弹层」的三段式信息架构。新详情页应对齐：

- **顶部放决策板**：页面第一屏是 DecisionBoard——一张「下一步动作」卡（按运行/健康状态优先级选出单条 CTA）+ 一条 tone 着色的证据条。决策模型（`build<X>DecisionModel`）是纯函数，只消费已有 contract 字段算出 `nextAction` 与 `evidenceItems`，不发请求、不发明字段。

- **tone 系统统一四档**：`'normal' | 'notice' | 'alert' | 'critical'`，映射到 `StatusGlyph` 的 state；CSS 类前缀按页面命名但结构保持一致，着色走 `var(--color-state-*)` / `color-mix`，不写 hex。新增详情页复制这套 tone→glyph→CSS 约定，不要另造一套色彩语义。
- **页头操作分层**：主 CTA 之外的页头操作收在右上角：「查看历史」「资料维护」是并列按钮且常驻（只读预览只保留「查看历史」），保证退役等状态下仍有维护入口；运行控制（暂停、恢复等）收进 `details.watchtower-actions-menu` 的 `…` 菜单，按状态条件渲染，列表为空或只读预览时不渲染菜单。不要把这些操作散落成页面底部的独立按钮。参考 `web/src/components/target-detail/TargetWatchtowerHeader.tsx`。

- **非实时配置 / 维护 demote 进 modal**：标签备注编辑、退役生命周期这类低频配置从页面主体移入 modal（`web/src/components/atoms/Modal.tsx`），页面主体只保留实时观测证据（决策板、运行控制、ProbeItem 列表与观测、当前异常、事件）。**例外**：本身会再开一个表单 modal 的入口（如 ProbeItem 表单）不要嵌进维护 modal，避免 modal 套 modal——让它贴着对应的实时列表区就近呈现。


用户文案使用“入口探测”，字段标识继续使用 `target_id`。入口详情保持紧凑身份和当前观测；管理与历史为次级，incident/event 请求失败独立可重试，缺失或禁用 probe 不等同于失败观测。空 incident 列表不能证明所有源健康。Probe 对话框操作 footer 保持在滚动正文外。

Target 的 `lifecycle_status` 使用 `active | retired`；`run_status` 仅使用 `启用 | 维护中 | 暂停`。退役状态不能由暂停推断，也不能作为运行控制输入。默认 API 查询只返回当前目标；列表分别批量读取 `scope=current` 与 `scope=retired` 以支持当前及历史页签，不把 `scope=all` 中仅关联归档 VPS 的 active 目标计入当前集合。退役页签使用 `lifecycle_status=retired`。退役目标从异常、暂停、覆盖缺口计数和运行批量操作中排除，详情隐藏运行控制和 Probe 修改；历史仍可查看。

Target 退役操作使用 `/runtime/archive`，将生命周期设为 `retired` 并暂停；显式 `/runtime/restore-to-paused` 恢复到 `active` 与暂停，后续启用另行确认。共享依赖审查、摘要失效重读和明确确认保持生效。回归由 `TargetsPage.test.tsx`、`TargetDetailPage.test.tsx` 和 `targetHelpers.test.ts` 覆盖。

## 观测与执行覆盖

- 列表异常及 `abnormal=1` 只包含当前可见、启用、有成功或失败观测时间且健康为关注/告警/严重的目标。`view=unobserved` 承接当前可见、启用且从未产生成功/失败观测的目标；两类计数不相加。健康筛选包含“数据不可用”，未知徽章使用中性色，运行控制独立显示。
- Target 的 `last_success_at` / `last_failure_at` 只从 `result_kind=success/failure` 且 `maintenance_context=false`、`is_backfilled=false` 的实时探测观测按 `observed_at` 投影；原始观测仍保留，乱序到达不能使任一时间倒退。
- TargetRecord 的只读 `enabled_probe_count` 表示配置为 enabled 的探测项数，暂停目标也保留配置事实。`matching_executor_count` 表示依照 Agent plan 标签交集和 archived/retired/paused 排除规则可接收任务的实例数；仅启用/维护目标可匹配，其余为零。列表与详情由后端一次聚合读取，不逐行请求。匹配不保证在线，不等于已有样本。
- 执行标签允许未来值；无启用探测项、无匹配实例、匹配但尚无样本分别解释。已有历史观测不保证当前健康，此次不新增样本过期阈值。
