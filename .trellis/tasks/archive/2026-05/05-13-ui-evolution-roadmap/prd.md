# 规划下一阶段 UI 演进路线

## Goal

在 `fix/glassmorphism-web-hardening` 合并后，把下一阶段 UI 工作从“当前页面可用但仍不够美观/不够好用”的主观问题，收敛成可执行的产品/UX 路线。输出需要能直接指导后续 Trellis 实现任务，优先让候风成为资产决策工作台与观测证据系统，而不是继续真实数据导入、机械拆分页面或堆叠视觉改色。

## What I Already Know

- PR #49 `Harden glassmorphism web redesign` 已合并，PR CI 中 `go`、`web`、GitGuardian 均通过，本地 `main` 已快进到合并结果。
- 用户明确追求“更加美观、用户体验良好”，当前问题不是功能不可用，而是产品界面还不足以支撑真实数据测试意愿。
- `docs/release/current-state-and-next-stage-plan.md` 已确认旧 Asset Ledger 计划没有新的立即开发任务，真实 40+ VPS 数据验证保持条件性延期。
- `docs/release/core-pages-product-ux-replan.md` 是父级规划，结论是下一批前端工作应从 UX-1 App shell / 导航 / 视觉基线重置开始。
- 当前实现已经不是从零开始：AppShell 已有分组导航、工作台命名、Dashboard 摘要状态；Dashboard 已有 asset-decision-first command surface；VPS/AssetDecisions/VPSDetail/Nodes/Targets/Events 已落入 v2 页面契约的一部分。
- v2 视觉权威是 `docs/design/v2-houfeng/design-language.md` 与 `docs/design/v2-houfeng/component-spec.md`；active visual evidence 流程是 `docs/operations/v2-visual-evidence.md`。
- 用户要求本轮不使用 subagent / 子代理，所以本规划基于主会话本地文档与代码检查完成。

## Requirements

- 输出一个持久化路线文档，明确 UI 演进批次、顺序、每批目标、范围、验收和非目标。
- 明确第一批实施任务应该是 UX-1：App shell / 导航 / 视觉基线重置，而不是真实数据 import、Provider/DNS 同步、Web SSH、插件系统或继续按行数拆页面。
- 路线必须承认当前实现已有可用基础，避免提出“一把推倒重做”的方案。
- 路线必须把视觉证据作为 UI 任务验收的一部分：本地预览 URL、检查路由、桌面/移动视口、必要截图或明确的人工判断限制。
- 路线必须把真实 40+ VPS 测试路径纳入产品目标，但不在本规划任务里运行真实数据 import。
- 后续每个 UI 实现任务必须保持 Trellis + 非 main 分支 + 本地质量门 + PR CI 全绿后合并 + 同步本地分支的流程。

## Proposed Roadmap

1. **UX-1 App Shell / Navigation / Visual Baseline**
   - 收敛应用壳层、导航分组、TopBar/GlobalSearch、页面 chrome、响应式行为和主题基线。
   - 目标是建立后续所有页面可复用的审美和交互地基，而不是先继续堆单页细节。

2. **UX-2 Dashboard Command Surface Polish**
   - 在现有 command surface 基础上强化首屏视觉判断、资产压力/观测异常/下一步动作三 lane 的主次关系。
   - 不恢复 KPI 墙、API 字段展板或独立 Group/Recent Event 列表。

3. **UX-3 Asset Decision + VPS Inventory Real-Data Path**
   - 让资产决策队列和 VPS 库存表真正支撑 40+ VPS 扫描、比较、补录和优先级判断。
   - Quick views、chips、drawer filters、数据质量提示和订阅/Node 关联必须服务扫描。

4. **UX-4 VPS Detail Decision Workbench**
   - 强化单台 VPS 的续费/成本/决策/Node 证据/服务域名/历史时间线组织。
   - 主页面保持判断路径，复杂编辑进入 Drawer 或局部面板。

5. **UX-5 Observability Support Pages**
   - Nodes、Targets、Events 从“主产品中心”收敛为资产判断的证据系统。
   - 保持专业观测能力和 Dashboard/VPS 深链承接，但降低与资产主路径竞争的视觉权重。

6. **UX-6 Design System / Evidence / Performance Hardening**
   - 在页面结构稳定后抽取真正长期保留的组件，补 visual evidence manifest，处理明显 bundle/chunk 警告。
   - 不在早期把临时页面形态过度抽象。

## First Implementation Task: UX-1

### Scope

- AppShell / Sidebar / TopBar / GlobalSearch / Breadcrumb 的视觉一致性和信息层级。
- 页面通用 chrome：`page-stack`、`page-panel`、`hero-panel`、section heading、主区域宽度与密度。
- 导航文案与产品心智：工作台、资产、观测、系统；避免残留“首页”作为主要可见文案。
- 桌面 1440x1000 与窄屏 390x900 的基本布局 sanity。
- 预览与视觉证据记录，至少覆盖 `/`、`/vps`、`/asset-decisions`、`/nodes`、`/targets`、`/events`、`/settings` 的壳层可达性。

### Non-Goals

- 不改后端模型。
- 不新增真实数据 import。
- 不扩展 Provider/DNS/Web SSH/插件/服务发现/RBAC。
- 不重做所有页面业务逻辑。
- 不为了行数继续机械拆分页面。
- 不引入新 UI 框架、图表库或视觉回归 CI。

### Acceptance Criteria

- [ ] App shell 在暗色主题下建立稳定、高密度、工程工具感的视觉基线。
- [ ] 导航分组和页面标题能够清晰表达“工作台 / 资产 / 观测 / 系统”心智。
- [ ] 常用路由在桌面和移动视口无明显文本溢出、控件重叠、空白首屏或不可达入口。
- [ ] GlobalSearch、Breadcrumb、Sidebar active 状态和异常计数不制造错误健康语义。
- [ ] 本地验证至少包括 `cd web && npm run lint`、`cd web && TMPDIR=$PWD/.tmp npm run test -- --run`、`cd web && npm run build`。
- [ ] UI final report / PR body 包含 preview URL、routes checked、viewports checked、evidence level、limitations。

## Acceptance Criteria For This Planning Task

- [x] 当前已合并 UI hardening PR 的结果被纳入路线前提。
- [x] 产出下一阶段 UI 演进路线文档。
- [x] 明确 UX-1 是下一步推荐实现入口。
- [x] 配置 Trellis implement/check 上下文。
- [x] 用户已授权由主会话决定路线；下一步实现入口锁定为 UX-1，后续另起实现任务。

## Out Of Scope

- 本任务不修改前端运行时代码。
- 本任务不运行真实 VPS 数据 dry-run/import。
- 本任务不提交新的 UI 实现 PR。
- 本任务不重新定义 v2 设计语言；如后续实现发现 component-spec 与实际产品目标冲突，再开独立 spec update。

## Technical Notes

- 父级规划：`docs/release/core-pages-product-ux-replan.md`。
- 当前状态审计：`docs/release/current-state-and-next-stage-plan.md`。
- v2 设计权威：`docs/design/v2-houfeng/design-language.md`、`docs/design/v2-houfeng/component-spec.md`。
- 视觉证据流程：`docs/operations/v2-visual-evidence.md`。
- 当前 AppShell 已读事实：`web/src/app/layout/AppShell.tsx` 使用 `getDashboard()` 派生摘要状态；`Sidebar` 已按产品心智分组；相关测试已断言不再显示“首页”导航。
- 当前 Dashboard 已读事实：`web/src/pages/DashboardPage.tsx` 渲染工作台 command surface；测试覆盖资产决策队列、深链和不恢复旧 Dashboard 摘要/KPI 文案。
