# 下一阶段 UI 演进路线

> 日期：2026-05-13
>
> 状态：Active route for the next UI implementation batch
>
> 前置状态：PR #49 `Harden glassmorphism web redesign` 已合并，当前 `main` 已同步。

## 结论

下一阶段 UI 工作应先建立稳定的产品体验基线，再逐页深化。推荐顺序是：

```text
UX-1 App shell / navigation / visual baseline
UX-2 Dashboard command surface polish
UX-3 Asset decision + VPS inventory real-data path
UX-4 VPS detail decision workbench
UX-5 Observability support pages
UX-6 Design system / evidence / performance hardening
```

这条路线继承 `docs/release/core-pages-product-ux-replan.md`，但把它压缩为后续 Trellis 任务可以直接执行的批次。它不替代 `docs/design/v2-houfeng/design-language.md` 与 `docs/design/v2-houfeng/component-spec.md`。

## 现在不应做什么

- 不回到旧 Asset Ledger 计划继续找“下一个 Task”；当前旧计划没有新的立即开发任务。
- 不直接运行真实 40+ VPS import；这仍需要真实数据文件、dry-run 报告和用户授权。
- 不继续按文件行数机械拆分页面；页面结构还会演进，过早抽象会增加返工。
- 不把 Provider/DNS 同步、Web SSH、插件、服务发现、完整域名管理、RBAC、汇率换算或复杂评分算法混入本轮 UI 改造。
- 不用营销首页、大屏监控中心或普通 SaaS 后台的视觉模式替代候风的工程工作台气质。

## 产品原则

候风的核心页面应服务长期打开、反复扫描、快速判断的工程工作流：

- **任务优先**：首屏回答“现在该处理什么”，不是展示所有可用字段。
- **资产主线**：VPS、续费、成本、订阅、服务商、决策队列是 MVP 真实数据测试入口。
- **观测作证**：Node、Target、Event 是资产判断的运行证据，不与资产主路径争抢中心位置。
- **高密度但可读**：密度来自表格层级、mono 数字、状态 glyph、drawer 和渐进披露，不来自卡片堆叠。
- **视觉证据必备**：UI 改造不能只用 lint/test/build 证明，必须给出预览、路由、视口和必要截图或人工判断记录。

## UX-1：App Shell / Navigation / Visual Baseline

### 目标

建立后续页面共同继承的壳层、导航、页面 chrome、主题和响应式基线。当前 AppShell 已经有分组导航和 Dashboard 摘要状态，因此 UX-1 不是重写，而是把已有基础打磨到“长期使用的工程工具”水准。

### 范围

- Sidebar 分组、active 状态、异常计数、SyncStatus、UserChip 的视觉一致性。
- TopBar、GlobalSearch、Breadcrumb 与页面主体之间的层级关系。
- `工作台 / 资产 / 观测 / 系统` 心智在导航、标题、面包屑和搜索结果中的一致表达。
- 主区域宽度、padding、section 间距、page panel 视觉权重和移动端折叠。
- 暗色主题默认体验和浅色主题不劣化。
- 本地预览与视觉证据流程。

### 验收

- 1440x1000 下，首屏有明确工作区域，不只有导航 chrome。
- 390x900 下，导航、搜索、按钮、badge 和表格入口不发生文本溢出或互相遮挡。
- Sidebar count 只表达异常对象数量，不制造“该导航项自身告警”的误读。
- GlobalSearch 可用且不会把 menu 布局撑出视口。
- 可见主导航不再回退为“首页”。
- PR 报告包含 preview URL、检查路由、检查视口、证据级别和限制。

### 非目标

- 不改后端 API。
- 不重排全部业务页面。
- 不新增 UI 框架或图表库。
- 不创建 repo 级视觉回归依赖。

## UX-2：Dashboard Command Surface Polish

### 目标

在现有工作台 command surface 基础上，把首屏视觉判断做得更有吸引力、更清晰。Dashboard 要像日常 command desk，而不是 KPI 墙、API 字段展板或模块目录。

### 范围

- 强化 h1、三条 lane、主按钮、次级动作和状态摘要之间的主次。
- 资产决策队列、观测异常队列、下一步动作保留固定结构，但视觉上更容易 5 秒内判断优先级。
- 当前处理队列、运行上下文、管理入口保持渐进披露，不恢复同权 section 堆叠。
- 空态、正常态、维护态、严重异常态都要有明确视觉节奏。

### 验收

- 有资产压力时，资产决策入口比普通管理入口更突出。
- 有严重异常时，严重异常和资产压力不会互相淹没。
- 正常态不显示大型空队列表格。
- Dashboard 深链继续被 Nodes/Targets/Events/VPS 页面承接。

## UX-3：Asset Decision + VPS Inventory Real-Data Path

### 目标

让用户愿意把 40+ VPS 带进界面核对。资产决策页是处理队列，VPS 页是高密度资产库存表，两者共同承担真实数据验证前的产品吸引力。

### 范围

- 资产决策页强化续费窗口、未评估、迁移/取消、缺订阅、未关联 Node 和缺信息优先级。
- VPS 列表强化 quick views、filter chips、drawer filters、订阅 join、Node 关联数量、数据质量 badge。
- 保持首屏数据行密度，避免筛选器和说明文案成为视觉主体。
- URL-state 与 Dashboard 深链保持可见承接。

### 验收

- 40+ VPS 行在桌面上可扫描、可比较、可定位问题。
- 用户能一眼看到哪些 VPS 需要优先评估或补录。
- 订阅读取失败不被渲染成真实“缺订阅”事实。
- linked node health 只在 contract 明确支持的页面展示，不在 VPS 列表中推导。

## UX-4：VPS Detail Decision Workbench

### 目标

单台 VPS 详情页要帮助用户做出“保留 / 观察 / 迁移 / 取消”的判断，而不是把所有表单和历史平铺成长卷轴。

### 范围

- 身份、当前决策、续费/成本、资料质量、Node 证据、服务/域名、时间线的层级重排。
- 高频判断信息留在主页面；复杂编辑进入 Drawer。
- 生命周期危险动作保持隔离确认。
- 服务/域名仍是 VPS scoped manual records，不扩成完整服务注册表或 DNS 管理。

### 验收

- 首屏能表达这台 VPS 的状态、成本压力、决策和证据。
- 用户不需要滚完整页才能知道下一步该做什么。
- Node/Target/Event 作为证据出现，不抢占 VPS 资产主体。

## UX-5：Observability Support Pages

### 目标

Nodes、Targets、Events 保持专业观测能力，但产品定位收敛为资产判断的证据系统。

### 范围

- Nodes 强调 linked VPS、异常证据、维护/暂停、接入/绑定状态。
- Targets 强调服务入口、探测覆盖、资产服务上下文。
- Events 强调审计与诊断时间线，承接 Dashboard、VPS、Node、Target 深链。
- 列表页保持高密度，筛选器以 chips/drawer/segmented controls 降低视觉占用。

### 验收

- 观测页能解释资产风险，而不是孤立展示运行对象。
- Dashboard 深链进入后，筛选状态首屏可见且可清除。
- 行点击、hover actions、drawer 和 keyboard 行为不回退。

## UX-6：Design System / Evidence / Performance Hardening

### 目标

在页面结构稳定后，把已经证明会长期保留的 UI 模式沉淀为组件、测试和证据，而不是提前抽象临时页面形态。

### 范围

- 抽取稳定的 shell/page/table/filter/workbench 组件。
- 维护 `docs/operations/v2-visual-evidence/manifest.md` 和关键截图。
- 处理明显 bundle/chunk 警告和过重页面加载路径。
- 对跨页面深链、URL-state、Drawer 状态和空/错/加载态补测试。

### 验收

- 组件抽取降低后续修改成本，而不是只降低单文件行数。
- UI 任务 PR 都能说明预览、路由、视口和视觉判断限制。
- `npm run lint`、Vitest、build 和 PR CI 全绿。

## 后续执行规则

每个 UX 批次都应独立创建 Trellis task，并遵守：

1. 非 main 分支开发。
2. 先写 PRD/context，再实现。
3. 本地验证：lint、Vitest、build；Vitest 在本机优先使用 repo-local `TMPDIR`。
4. UI 任务启动 dev server，记录 preview URL、routes checked、viewports checked。
5. PR CI 全绿后合并。
6. 合并后同步本地 `main`，再进入下一批。

## 推荐下一步

下一步创建并启动 UX-1 Trellis task：`App shell / navigation / visual baseline reset`。

它应该先做壳层和视觉基线，不应直接进入 Dashboard/VPS 细节。原因是后续所有页面都会继承壳层密度、导航心智、页面 chrome 和视觉证据流程；如果先改单页，容易让各页继续各自变漂亮但整体产品感仍然割裂。
