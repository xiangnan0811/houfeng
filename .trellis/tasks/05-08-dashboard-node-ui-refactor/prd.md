# refactor dashboard node pages

## Goal

重构候风 Web 端首页、节点列表页、节点详情页，在不改变后端 API contract 的前提下提升任务完成效率、视觉层级、可读性和交互体验。美观与可用性是并列目标：页面应保持候风 v2 的“东方观象台气质的工程工具”方向，同时让运维用户更快发现问题、筛选节点、诊断节点。

## What I Already Know

* 用户提供了当前首页、节点列表页、节点详情页截图，并明确要求三个页面更加易用、美观、体验良好。
* 现有前端是 React 19 + TypeScript + Vite SPA，无 Tailwind / CSS-in-JS / 状态库。
* 样式体系使用 `tokens.css` + `atoms.css` + `pages.css`，新增页面样式应写入全局 page/atom CSS 并使用 BEM + token。
* 视觉权威是 `docs/design/v2-houfeng/design-language.md` 与 `docs/design/v2-houfeng/component-spec.md`。
* 现有三页已经有部分 v2 能力：Dashboard 状态栏、处理队列、节点列表 DataTable、URL 筛选、趋势列、批量操作、节点详情 watchtower 指标、历史/命令抽屉。

## Requirements

* 首页继续以 `/api/dashboard` 已有事实为数据来源，不新增字段，不伪造健康/同步状态。
* 首页首屏强化“全局状态 + 当前处理队列 + 关键指标 + 趋势 + 明确操作”，严重异常必须比普通信息更醒目。
* 节点列表保留创建节点、URL 筛选、绑定异常视图、趋势列、批量操作、对比、行点击详情、行内运行控制。
* 节点列表改进页面说明、筛选/工具栏层级、选择与批量操作反馈、表格扫描效率、空态与错误呈现。
* 节点详情保留加载节点、runtime facts、incidents/events、绑定冲突处理、运行控制、生命周期、标签备注、容器、历史抽屉、命令抽屉。
* 节点详情首屏强化节点健康摘要、当前问题、关键指标优先级、时间范围控制和次级折叠信息。
* 后续追加范围：目标页、事件页、设置页需要与首页、节点列表页、节点详情页使用统一的设计令牌、页面面板、筛选控件、表格和表单卡片视觉语言，避免早期系统出现页面割裂。
* 不引入新依赖，不改变 API 形状，不把页面代码迁移到新的样式体系。

## Acceptance Criteria

* [ ] 首页打开后 5 秒内可判断当前健康状态、严重对象数量、影响对象和下一步入口。
* [ ] 节点列表能清楚展示总量、异常、待接入、维护/暂停、筛选结果数量；表格行能快速识别状态、节点身份、位置、标签、当前问题和趋势。
* [ ] 节点详情打开后 15 秒内能判断节点是否健康、为什么、最后心跳、运行时长、关键资源指标和可用动作。
* [ ] 目标、事件、设置页入口面板、筛选/表单/表格表面、圆角和控件状态与三大核心页面保持一致。
* [ ] 所有关键状态既有颜色也有文字/形状，不只依赖颜色。
* [ ] 浅色主题下边框、状态、文字对比明显改善，避免整页同一米黄色层级不清。
* [ ] `cd web && npm run lint`、`cd web && npm run test -- --run`、`cd web && npm run build` 通过，或记录无法通过的具体原因。

## Out Of Scope

* 不新增后端接口或 dashboard contract 字段。
* 不引入 React Query、图表库、Tailwind、CSS Modules、图标库或可视化回归框架。
* 不重构目标详情的数据结构与交互主线。
* 不改变认证、AppShell 全局导航和后端监控语义。

## Technical Notes

* 相关规范：`.trellis/spec/web/*`、`.trellis/spec/guides/index.md`。
* 视觉规范：`docs/design/v2-houfeng/design-language.md`、`docs/design/v2-houfeng/component-spec.md`。
* 主要文件：`web/src/pages/DashboardPage.tsx`、`web/src/pages/NodesPage.tsx`、`web/src/pages/NodeDetailPage.tsx`、`web/src/components/node-detail/*`、`web/src/styles/pages.css`。
