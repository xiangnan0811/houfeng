# UX-2 Dashboard command surface

## Goal

把 Dashboard 从“系统概览 + 多块摘要拼接”推进为真实 VPS 管理路径的日常工作台。用户进入工作台后，首屏必须先回答“现在有什么需要处理”，并把资产决策压力、续费窗口、缺信息资产、观测异常和下一步动作组织成一个 command surface。

## What I already know

- 用户已明确认为当前核心页面“太丑、非常混乱”，需要先重塑 UI/UX 才能进入真实 VPS 数据测试。
- 父级规划 `docs/release/core-pages-product-ux-replan.md` 已将候风定位为“资产决策工作台 + 观测证据系统”。
- UX-1 已完成 AppShell 导航分组，`首页` 心智已向 `工作台` 收敛。
- 当前 Dashboard 已具备 `/api/dashboard`、资产摘要、异常节点/目标、24h 趋势和深链能力，但视觉重心仍偏 fleet state 和多个信息块拼接。
- 本任务不使用 subagent，由主会话直接执行 Trellis 等价流程。

## Scope

- 新增或重塑 Dashboard 首屏 command surface：
  - `资产决策队列`：展示 30 天续费、待决策、取消/迁移、未关联 Node、关联异常、成本摘要。
  - `观测异常队列`：展示严重、异常节点、异常目标、维护、24h 变化和最高优先级异常对象。
  - `下一步动作`：根据资产压力和观测状态生成少量排序动作，首要动作必须可直接跳转到决策/异常处理路径。
- 将工作台可见文案从 `首页 / Dashboard` 收敛为 `工作台`。
- 保留必要深链到 VPS、资产决策、节点、目标、事件、设置，不新增后端 API 或复杂评分算法。
- 将 Dashboard 下半部分降级为处理队列、运行上下文和管理入口的辅助区域，避免重复展示资产摘要。
- 同步更新 active visual / web contract 文档，避免实现和规范漂移。
- 更新 `DashboardPage.test.tsx` 覆盖异常、正常、维护、首次接入、错误态和深链语义。

## Out of Scope

- 不导入真实 40+ VPS 数据。
- 不新增复杂资产评分算法。
- 不把所有 `/api/dashboard` 字段展示出来。
- 不重做资产决策页、VPS 列表页或 VPS 详情页，那些属于 UX-3 / UX-4。
- 不新增 CSS 框架、UI 库、状态管理库或 E2E 依赖。
- 不处理 release / publish workflow。

## Acceptance Criteria

- [ ] Dashboard 数据加载文案和错误态使用 `工作台`，不再显示 `首页 / Dashboard` 或 `首页不可用`。
- [ ] 数据态首屏存在唯一主 command surface，包含 `资产决策队列`、`观测异常队列`、`下一步动作`。
- [ ] 资产决策入口在首屏权重高于普通管理入口，且包含 30 天续费、待决策、取消/迁移、未关联 Node、关联异常、成本入口。
- [ ] 观测异常入口保留 PR4 深链：严重 `/events?severity=严重`、24h `/events?time_range=24h`、维护 `/events?maintenance_only=1`、异常节点 `/nodes?abnormal=1`、异常目标 `/targets?abnormal=1`。
- [ ] 异常态仍展示统一处理队列，且队列项包含 `当前问题` 文案、对象名优先、技术 ID 次级、`处理` action。
- [ ] 正常态、维护态和首次接入态不回退为 KPI 墙、Group 摘要、最近事件摘要或 API facts。
- [ ] `asset_summary` 不展开成 VPS / subscription 明细，只做决策入口和聚合摘要。
- [ ] UI 使用现有 token、BEM class 和 atoms，不新增单页 CSS 文件，不硬编码颜色。
- [ ] `cd web && npm run lint`、`cd web && npm run test -- --run DashboardPage`、`cd web && npm run test -- --run`、`cd web && npm run build` 通过。
- [ ] 本地 dev server 可打开，完成桌面和移动视口 Playwright sanity screenshot，无明显空白、重叠或首屏信息拥挤。

## Technical Notes

- 主要文件预计涉及：
  - `web/src/pages/DashboardPage.tsx`
  - `web/src/pages/dashboard/*`
  - `web/src/pages/DashboardPage.test.tsx`
  - `web/src/styles/pages.css`
  - `docs/design/v2-houfeng/component-spec.md`
  - `.trellis/spec/web/state-and-data.md`
- 当前 Dashboard API contract 已足够支撑本任务，不需要后端字段变更。
- 视觉权威：`docs/design/v2-houfeng/design-language.md`、`docs/design/v2-houfeng/component-spec.md`。
- 任务审计见 `research/dashboard-current-structure-audit.md`。

## Definition of Done

- 非 main 分支开发并提交。
- Trellis task archive + journal 完成。
- PR 创建，CI 绿后合并。
- main CI 监控通过。
- 本地 `main` 同步到 `origin/main`。
