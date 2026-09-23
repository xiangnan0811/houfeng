# 入口探测 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

资产详情页（VPS `/vps/:id`、入口 `/targets/:id`）统一采用「判断在顶、证据居中、配置进弹层」的三段式信息架构。新详情页应对齐：

- **顶部放决策板**：页面第一屏是 DecisionBoard——一张「下一步动作」卡（按运行/健康状态优先级选出单条 CTA）+ 一条 tone 着色的证据条。参考 `web/src/pages/vps-detail/VPSDecisionBoard.tsx` + `vpsDecisionModel.ts` 与镜像它的 `web/src/pages/target-detail/TargetDecisionBoard.tsx` + `targetDecisionModel.ts`。决策模型（`build<X>DecisionModel`）是纯函数，只消费已有 contract 字段算出 `nextAction` 与 `evidenceItems`，不发请求、不发明字段。

- **tone 系统统一四档**：`'normal' | 'notice' | 'alert' | 'critical'`，经 `toneToGlyphState` 映射到 `StatusGlyph` 的 state；CSS 类前缀按页面命名但结构对齐（`target-decision-*` 镜像 `vps-decision-*`），着色走 `var(--color-state-*)` / `color-mix`，不写 hex。新增详情页复制这套 tone→glyph→CSS 约定，不要另造一套色彩语义。
- **二级 / 编辑操作收进右上角 `…` 菜单**：非主 CTA 的操作（查看历史、运行控制、编辑基础信息、资料维护）放进 `watchtower-header` 的 `details.watchtower-actions-menu`，不要散落成页面底部的独立按钮。参考 VPS hero「编辑基础信息」(`web/src/pages/vps-detail/VPSDetailHero.tsx`) 与 Target「资料维护」(`web/src/components/target-detail/TargetWatchtowerHeader.tsx`)。该菜单**始终渲染**——即使某状态（如已归档）下运行控制动作为空，菜单仍要在，否则会丢失维护入口；运行控制按钮列表按状态条件渲染，查看历史/维护项常驻。

- **非实时配置 / 维护 demote 进 modal**：标签备注编辑、归档生命周期这类低频配置从页面主体移入 modal（`web/src/components/atoms/Modal.tsx`），页面主体只保留实时观测证据（决策板、运行控制、ProbeItem 列表与观测、当前异常、事件）。**例外**：本身会再开一个表单 modal 的入口（如 ProbeItem 表单）不要嵌进维护 modal，避免 modal 套 modal——让它贴着对应的实时列表区就近呈现。


用户文案使用“入口探测”，字段标识继续使用 `target_id`。入口详情保持紧凑身份和当前观测；管理与历史为次级，incident/event 请求失败独立可重试，缺失或禁用 probe 不等同于失败观测。空 incident 列表不能证明所有源健康。Probe 对话框操作 footer 保持在滚动正文外。
