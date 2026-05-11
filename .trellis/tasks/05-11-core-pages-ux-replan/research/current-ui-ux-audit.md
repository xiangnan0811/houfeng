# 当前核心页面 UI/UX 审计

> 日期：2026-05-11
>
> 范围：基于当前仓库页面代码、`docs/operations/` 截图、v2 设计语言、当前状态审计文档与用户反馈，确认核心页面为什么需要产品/UX 重新规划。

## 用户反馈

用户明确指出当前页面“太丑、非常混乱、UI/UX 以及用户体验都是非常差的水平”，并且当前页面状态会阻碍其进入真实数据测试。

这意味着下一步不能继续以“已有页面功能完整”为完成标准，也不能先做真实数据 import 或机械拆分页面文件。真实数据测试本身需要一个让用户愿意录入、检查和判断资产状态的界面。

## 已查看材料

- `docs/release/current-state-and-next-stage-plan.md`
- `docs/design/v2-houfeng/design-language.md`
- `docs/design/v2-houfeng/component-spec.md`
- `.trellis/spec/web/index.md`
- `.trellis/spec/web/styling-guidelines.md`
- `.trellis/spec/web/component-conventions.md`
- `web/src/app/metadata.ts`
- `web/src/pages/DashboardPage.tsx`
- `web/src/pages/dashboard/DashboardWorkbench.tsx`
- `web/src/pages/dashboard/AssetDecisionSummary.tsx`
- `web/src/pages/VPSPage.tsx`
- `web/src/pages/VPSDetailPage.tsx`
- `web/src/pages/AssetDecisionsPage.tsx`
- `web/src/pages/NodesPage.tsx`
- `web/src/pages/TargetsPage.tsx`
- `web/src/pages/EventsPage.tsx`
- `docs/operations/Dashboard.jpg`
- `docs/operations/节点列表页面.jpg`
- `docs/operations/节点详情页面.jpg`
- `docs/operations/目标列表页面.jpg`
- `docs/operations/目标详情页面.jpg`

## 当前页面主要问题

### 1. 视觉方向没有兑现 v2 目标

v2 设计语言要求候风是 dark-first、冷静、克制、高密度、工程师长期使用友好的工具。但现有截图读起来更接近浅色、大留白、卡片化的普通 SaaS 后台：

- 大量浅色米白表面削弱了“观象台 / 工程工具”的产品记忆。
- 页面容器和卡片过多，视觉层级靠边框和留白堆叠，缺少明确行动焦点。
- 首屏信息被摊开，用户需要自己判断哪个区块最重要。
- 当前页面没有形成“我愿意在这里持续管理 VPS”的吸引力。

### 2. 信息架构仍是资源平铺

当前主导航为：

```text
首页 / VPS / 服务商 / 订阅 / 资产决策 / 节点 / 目标 / 事件 / 设置
```

Asset Ledger 已经成为产品主线之一，但导航仍像后端资源表平铺：

- `VPS`、`服务商`、`订阅`、`资产决策` 与 `节点`、`目标`、`事件` 同级。
- 用户无法从导航上理解“资产管理”和“观测信号”之间的主从关系。
- 真实 VPS 数据测试的主路径不清晰：应先看资产压力、续费决策和 VPS 详情，还是先看节点/目标/事件。
- Dashboard 作为首页仍带有早期 Fleet Observability 工作台惯性，不足以承载 Asset Ledger 的第一入口。

### 3. Dashboard 缺少强产品中心

Dashboard 已有工作台和资产摘要，但当前问题是产品中心不够明确：

- 它既想展示 fleet 状态，又想展示资产决策，还保留多种 summary。
- 首屏缺少一个强判断：“今天我需要处理什么？”
- KPI、队列、入口、概览之间权重接近，容易形成信息噪音。
- 资产决策没有成为 Dashboard 的主要行动入口。

后续 Dashboard 应从“仪表盘”转为 command surface：资产压力、续费决策、异常信号和下一步动作必须汇聚成一个清晰首页。

### 4. 列表页像筛选器展示页，不像工作列表

从节点和目标截图看，列表页的问题不是字段不足，而是：

- 页头和筛选区占据过多注意力。
- 数据表内容密度不足，少量行被包在过大的页面结构里。
- 筛选条件是重要工具，但不应比数据更像页面主体。
- 用户进入列表页应立刻能比较、扫描、选择下一步，而不是先面对一个大筛选面板。

VPS 列表和资产决策页后续必须避免复用这种“头部 + 大筛选 + 稀疏表格”的形态。

### 5. 详情页缺少决策驱动结构

节点详情已有图表区域和历史抽屉方向，但整体仍偏技术信息陈列。VPS 详情页更需要避免成为字段长卷轴。

资产详情页的核心应是：

- 这台 VPS 当前值不值得保留。
- 什么时候续费，多少钱，风险是什么。
- 它关联了哪个 Node、哪些 service/domain、哪些事件。
- 下一步动作是续费、迁移、观察、退役还是补录信息。

这要求 VPS Detail 与 Node Detail 的关系重新定义：VPS 是资产与决策对象，Node 是观测对象，二者不应互相替代。

### 6. 缺少 Asset Ledger 页面视觉证据

`docs/operations/` 当前只保存 Dashboard、节点、目标相关截图。VPS、资产决策、服务商、订阅页面没有可复核视觉证据。

这会导致后续验收只围绕旧观测页面，而忽略 Asset Ledger 主线。后续实现任务应补充可预览截图或至少启动 dev server 供人工检查。

## 为什么这会阻碍真实数据测试

真实数据测试不是单纯把 JSON import 进数据库。用户需要在页面上确认：

- 40+ VPS 是否被正确识别和分组。
- provider、region、cost、billing cycle、renewal date 是否可读。
- 哪些资产该续费、迁移、退役或继续观察。
- 哪些 VPS 关联了已有节点、服务、域名和异常。
- 页面是否能支撑反复修正字段、补录信息和确认决策。

如果页面本身混乱、丑、缺少行动路径，真实数据测试会变成纯数据库核对，无法验证产品是否真的可用。

## 重新规划约束

1. 第一屏必须是可工作的产品界面，不是展示型 Dashboard。
2. Asset Ledger 与 Fleet Observability 要形成主从关系：资产管理是当前真实数据测试入口，观测信号是资产判断的证据。
3. 页面视觉应回到 v2 的 dark-first、高密度、克制工程工具方向。
4. 列表页应优先服务扫描和比较，筛选器后置或收敛。
5. 详情页应优先服务判断和动作，技术上下文次级呈现。
6. 后续拆组件必须跟随新页面结构，不能先把旧页面形态固化为更多文件。

## 推荐下一步

本轮产出核心页面产品/UX 重新规划文档，作为后续实现任务的父级依据。规划完成后，再按批次进入：

1. App shell / 导航 / 视觉基线重置。
2. Dashboard 工作台重塑。
3. 资产决策 + VPS 列表重塑。
4. VPS 详情页重塑。
5. 节点 / 目标 / 事件支撑页收敛。
