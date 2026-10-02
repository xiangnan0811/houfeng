# 入口探测 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

资产详情页（VPS `/vps/:id`、入口 `/targets/:id`）统一采用「判断在顶、证据居中、配置进弹层」的三段式信息架构。新详情页应对齐：

- **顶部放决策板**：页面第一屏是 DecisionBoard——一张「下一步动作」卡（按运行/健康状态优先级选出单条 CTA）+ 一条 tone 着色的证据条。决策模型（`build<X>DecisionModel`）是纯函数，只消费已有 contract 字段算出 `nextAction` 与 `evidenceItems`，不发请求、不发明字段。

- **tone 系统统一四档**：`'normal' | 'notice' | 'alert' | 'critical'`，映射到 `StatusGlyph` 的 state；CSS 类前缀按页面命名但结构保持一致，着色走 `var(--color-state-*)` / `color-mix`，不写 hex。新增详情页复制这套 tone→glyph→CSS 约定，不要另造一套色彩语义。
- **页头操作分层**：主 CTA 之外的页头操作收在右上角：「查看历史」「资料维护」是并列按钮且常驻（只读预览只保留「查看历史」），保证退役等状态下仍有维护入口；运行控制（暂停、恢复等）收进 `details.watchtower-actions-menu` 的 `…` 菜单，按状态条件渲染，列表为空或只读预览时不渲染菜单。不要把这些操作散落成页面底部的独立按钮。参考 `web/src/components/target-detail/TargetWatchtowerHeader.tsx`。

- **非实时配置 / 维护 demote 进 modal**：标签备注编辑、退役生命周期这类低频配置从页面主体移入 modal（`web/src/components/atoms/Modal.tsx`），页面主体只保留实时观测证据（决策板、运行控制、ProbeItem 列表与观测、当前异常、事件）。**例外**：本身会再开一个表单 modal 的入口（如 ProbeItem 表单）不要嵌进维护 modal，避免 modal 套 modal——让它贴着对应的实时列表区就近呈现。


用户文案使用“入口探测”，字段标识继续使用 `target_id`。入口详情保持紧凑身份和当前观测；管理与历史为次级，incident/event 请求失败独立可重试，缺失或禁用 probe 不等同于失败观测。空 incident 列表不能证明所有源健康。Probe 对话框操作 footer 保持在滚动正文外。

Target 的 `lifecycle_status` 使用 `active | retired`；`run_status` 仅使用 `启用 | 维护中 | 暂停`。退役状态不能由暂停推断，也不能作为运行控制输入。默认 API 查询只返回当前目标；列表为历史页签显式读取 `scope=all`，退役页签使用 `lifecycle_status=retired`。退役目标从异常、暂停、覆盖缺口计数和运行批量操作中排除，详情隐藏运行控制和 Probe 修改；历史仍可查看。

Target 退役操作使用 `/runtime/archive`，将生命周期设为 `retired` 并暂停；显式 `/runtime/restore-to-paused` 恢复到 `active` 与暂停，后续启用另行确认。共享依赖审查、摘要失效重读和明确确认保持生效。回归由 `TargetsPage.test.tsx`、`TargetDetailPage.test.tsx` 和 `targetHelpers.test.ts` 覆盖。
