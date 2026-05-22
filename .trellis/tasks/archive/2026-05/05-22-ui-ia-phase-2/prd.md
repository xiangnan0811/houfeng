# 第二阶段前端页面信息架构优化

## Goal

在不改变后端、API 调用合同、路由结构和大型依赖的前提下，将候风前端重点页面从“功能平铺”收敛为“工作流驱动”的服务器舰队工作台：每页首屏只突出一个主叙事和一个主 CTA，次级动作、说明信息、刷新/筛选/批量操作下沉或弱化。

## Requirements

* 仅修改 `web/src` 内前端代码。
* 不修改后端代码，不修改 API 调用逻辑，不改变路由结构，不引入新的 UI 框架或大型依赖。
* 保留现有页面决策逻辑、数据来源、URL-state 与可测试行为；只调整信息架构、组件编排和必要样式。
* Dashboard / `DashboardCommandSurface`：保留现有决策逻辑，但首屏视觉上突出“今日第一步”，降低刷新、自动刷新、次级链接的视觉权重。
* NodesPage 及 nodes 子组件：参考 VPSPage 的 quick view + 高级筛选 Drawer 模式，减少首屏同时暴露的筛选项和批量操作。
* NodesPage 批量操作默认不高亮展示；仅在用户选择节点或显式打开操作区时展示。
* `NodesSupportSurface` 保留资产判断支撑语义，但压缩说明文案，减少同时展示的 support lane 数量。
* VPSPage：保持既有 quick view / 高级筛选工作流，作为本阶段 IA 对齐参考；必要时弱化非主路径信息，不扩展功能。
* AssetDecisionsPage：保留优先级队列，但将“证据边界说明”降级为次级信息，避免占据主视觉。
* 所有目标页面遵循：一个页面一个主叙事、一个主 CTA、次级动作弱化。
* 保留现有 className/API/路由结构，新增样式遵循 BEM 与现有 CSS variables。

## Acceptance Criteria

* [ ] Dashboard 首屏能明确识别“今日第一步”主任务；刷新、自动刷新、管理入口等次级动作不再与主 CTA 同权。
* [ ] NodesPage 首屏只展示 quick view / 主列表 / 主 CTA；高级筛选进入 Drawer 或等价次级 surface。
* [ ] NodesPage 批量操作在未选择节点且未显式打开操作区时不占据主视觉。
* [ ] NodesSupportSurface 文案更短，同时展示的支撑 lane 数量减少，但资产判断支撑语义保留。
* [ ] AssetDecisionsPage 优先级队列仍是主视觉；证据边界说明降为次级/可折叠/低权重信息。
* [ ] VPSPage 与 NodesPage 的 quick view + advanced filter 模式在视觉和交互层面保持一致。
* [ ] Dashboard、Nodes、VPS、Asset Decisions 四页仍能完成原有核心交互，现有测试可更新并通过。
* [ ] 移动端断点不被破坏，首屏不会因次级说明/批量动作过度挤占。
* [ ] `make verify-web` 通过。

## Definition of Done

* 相关页面测试按新 IA 更新，覆盖主 CTA、筛选 Drawer/操作区展示状态、证据说明降级等可见行为。
* 运行 `make verify-web`。
* 对本地前端进行浏览器 sanity，至少覆盖 Dashboard、Nodes、VPS、Asset Decisions 的桌面与窄视口主路径；如受本地服务状态限制，明确说明。
* 不提交截图 manifest 或 bulk raster screenshots。

## Out of Scope

* 后端、数据库、Go contract、API client 语义变更。
* 新路由、新全局状态库、React Query/SWR/Redux/Zustand 等依赖。
* 重做视觉主题或第三阶段视觉细节打磨。
* 改变业务决策排序、健康/资产状态语义、安装命令或 token 展示合同。
* 引入正式 e2e 框架或可视化回归系统。

## Technical Approach

* 以现有 `DashboardPage.tsx`、`NodesPage.tsx`、`VPSPage.tsx`、`AssetDecisionsPage.tsx` 为装配点做 IA 调整，优先复用现有 atoms、Drawer、Filter primitives、PageState、DataTable。
* Dashboard 不扩展 contract，只重排 command surface：主任务/主 CTA 优先，其余刷新、自动刷新、入口链接收为低权重 action rail。
* NodesPage 参考 VPSPage：把常驻筛选从首屏移入 quick view + advanced filter Drawer；批量操作改为选择触发或显式展开。
* `NodesSupportSurface` 保留“资产判断支撑”但压缩为少量高价值 lane，更多说明走次级文案。
* Asset Decisions 保留队列数据与编辑 drawer，只降低 evidence boundary 说明的权重。
* 样式只改 `web/src/styles/pages.css` 等既有 CSS 文件，使用现有 tokens，避免新增 page CSS。

## Technical Notes

* 适用规范：`.trellis/spec/web/{directory-structure,component-conventions,state-and-data,styling-guidelines,quality-guidelines}.md`。
* 分支治理：开发必须在 feature branch；本任务分支为 `feat/ui-ia-phase-2`。
* 质量命令：`make verify-web`。
