# 组件约定

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

---

## Overview

候风前端组件统一采用 **函数式组件 + React 19 hooks**，TypeScript 严格 props（`tsconfig.app.json` 启用 strict）。当前组件和页面设计先参考 `docs/design/current/interface-language.md`、`docs/design/current/component-patterns.md` 与现有 `components/atoms/` / `components/filters/` 实现。早期版本化设计稿、Stitch 截图和旧 redesign 过程材料只作为历史背景；如需改变当前方向，更新 `docs/design/current/` 和相关测试，而不是回退到旧版本标签。

实读约束（来自 `web/src/components/`、`web/src/pages/`）：

- 全部命名导出（`export function <Name>(...)`）；**未发现任何 `export default`**。
- 组件本体是 `function`；当前**未使用** `React.FC`、`React.memo`、`useMemo`、`useCallback`（除 context Provider 内）这些"先抽象再观察"的模式——保持简单。
- 唯一例外是需要 ref 转发的输入控件：`web/src/components/atoms/Input.tsx:11` 用 `forwardRef` + `export const Input = forwardRef(...)`，仍然是命名导出。
- 组件文件**只导出 1 个组件 + 同文件 props 类型**；多个相关原子合在一个文件的例外见 `components/atoms/Mono.tsx`（`MonoDigits` / `Hostname` / `Timestamp` 同源）。
- 暗色优先 + 中文为主 UI；颜色 / 间距 / 字号永远用 `tokens.css` 里的 CSS 变量，不写 hex。

---

## 组件分层

> **当前分层是这样的；处于初始开发阶段，未来可能简化或合并**。

```
app/layout/      ← 应用壳（Sidebar / TopBar / Breadcrumb / GlobalSearch...）
   ↑ 仅被 AppShell 组合，不应被 pages 直接引用
pages/           ← 路由页装配点（拉数据、编排状态、组合组件）
   ↑ 由 app/router.tsx 唯一注册
components/      ← 跨页业务组合组件（IncidentList / EventList / DetailSection...）
   ↑ 受控、纯展示，不发请求
components/filters/ ← 列表页筛选原语（FilterBar / FilterSelect / FilterMultiSelect / FilterToggle / FilterChip）
   ↑ 业务无关、受控；样式落 components/filters/filters.css，由 main.tsx 集中引入
components/atoms/ ← 设计系统原子（Button / Card / Badge / Sparkline / Mono...）
   ↑ 不感知任何业务类型，仅依赖 tokens.css / atoms.css
```

落地规则：

- **page 不直接 import `app/layout/*`**：layout 仅服务 AppShell。
- **components/ 不依赖路由**：要跳转，由 page 传 callback 或 children 进来。
- **atoms/ 不依赖 `lib/types.ts`**：原子要复用，必须保持业务无关；需要业务感知就升一层到 `components/`。
- **跨页业务组合组件可接收 action/meta/glyph 这类 `ReactNode` 插槽**：当组件需要保持 route-agnostic，但页面仍要传入 `<Link>`、`<Button>`、`<StatusGlyph>`、`<Hostname>` 等具体组合时，使用受控插槽，不在组件内 import `react-router-dom` 或领域 helper。参考 `web/src/components/ObservabilityEvidenceLead.tsx` 与 `web/src/components/ObservabilityEvidenceFocus.tsx`：Monitoring / Targets / Events 共享 lead/focus 骨架，但页面自己决定 action、glyph、meta 和路由。
- **route/detail/list 的 loading / error / empty 状态优先复用 `PageState`**：`web/src/components/PageState.tsx` 是跨页展示 primitive，保持 route-agnostic，通过 `action` slot 接收 `<Link>` / `<Button>` / page callback；页面不要继续手写裸 `page-panel` loading/error，列表空态需要当前空态装饰和 CTA 时使用 `surface="empty"`。错误摘要用 `technicalSummary`，组件会截断并避免和 description 重复显示。
- **DataTable 可点击行与行内操作的合同**：`web/src/components/atoms/DataTable.tsx` 的 `onRowClick` 只处理非交互子节点上的 click / Enter / Space；事件目标落在 `a[href]`、`button`、`input`、`select`、`textarea`、`role="button"`、`role="link"` 内时，表格行不得触发行导航。page 的 action cell 不需要再为了 DataTable 行点击重复写 `stopPropagation`，但自绘 list/queue 不是 DataTable 时仍必须在内部 `<Link>` / `<Button>` 上显式阻止冒泡。
- **自绘可点击队列不要制造嵌套交互语义**：如果一个 `<li>` / `<article>` 内部已经有可见 `<Link>` 和 `<Button>`，可以让鼠标点击行背景进入主详情，但不要给外层容器加 `role="link"` / `tabIndex=0` 再包住内部交互控件；键盘入口应落在可见 action 上，外层只用 `:focus-within` 做焦点视觉辅助。
- 当前**未单独建 `hooks/` 目录**；本地 hook 内联在使用文件内即可，需要跨文件再考虑提取（届时落点为 `web/src/lib/use<Name>.ts` 或新增 `web/src/lib/hooks/`，需另做决策）。
- **modal / drawer focus 行为复用 `web/src/lib/useModalFocus.ts`**：可访问性弹层必须 portal 到 `document.body`，声明 `role="dialog"` / `aria-modal="true"`（确认类用 `alertdialog`），打开后移动初始焦点，Tab / Shift+Tab containment，Escape 关闭，关闭后恢复触发器焦点；不要在各组件里复制 ad-hoc `document.addEventListener('keydown')` + 手写 focus trap。
- **Drawer 取消/关闭必须清理未提交本地状态**：page 用 Drawer 承载 create/edit 表单时，`onClose` / 取消按钮 / Escape / overlay 关闭都必须丢弃 draft、表单错误和保存反馈；重新打开应从当前已保存数据或初始空表单重建。测试至少覆盖“编辑草稿 → 取消关闭 → 重新打开草稿已重置”以及取消不触发提交。
- **复杂表单 modal 必须有可读宽度和收束行为**：创建/编辑订阅、VPS 基础信息、监控实例接入、服务商等字段密集表单使用 `Modal` 的 `md` / `lg` / `xl` 尺寸，避免默认窄弹窗造成标签和命令无意义换行。提交成功必须关闭或跳转；取消必须丢弃草稿；失败留在当前弹层并展示错误。由 URL deep-link 打开的弹层在消费或关闭时必须清理 `create=1`、`onboarding=1` 等临时参数，同时保留承接上下文参数。
- **常见有界字段优先选择器 + 自定义入口**：VPS 国家/地区、订阅币种、支付方式、计费周期单位、续费方式这类高频字段必须使用共享 option/helper 与 `<select>` / radio 控件；常见值内置，确实不在范围内才进入“自定义/其他”。不要把这些字段退回裸文本输入，也不要在创建和编辑表单各自散落字符串。
- **关联表单优先选择器，不让用户复制内部 ID**：Provider、MonitoringInstance、Target、Service 这类已有业务对象的常规关联表单应渲染可辨识 `<select>`/selector（名称 + ID + 状态/位置等辅助信息），并保留空值/不关联能力。无候选或加载失败时用说明 + `<Link>`/action 指向对应列表/创建入口；MonitoringInstance/Target 选择只作为用户确认的资产关联，不在表单提交时隐式修改观测运行态。
- **复杂前端变更先走视觉伴随评审**：涉及新页面、重要详情区块、dashboard/工作台、复杂数据矩阵、资产决策、低频深度报告这类信息架构或视觉层级不明确的任务，开发前必须使用相关 skills（尤其是 brainstorming / frontend-design）产出可在浏览器打开的 mockup，让用户先确认展示密度、字段优先级和页面承载方式。不要只用文字方案或直接实现来猜 UI；mockup 可放在 ignored 的 `.superpowers/brainstorm/`，正式实现仍遵守本 spec 的 tokens、BEM、中文文案和测试要求。

### AppShell / Command Search 交互合同

- **Skip link 必须有可聚焦目标**：AppShell 顶部使用 `<a className="skip-link" href="#main-content">跳到主内容</a>`，主区域必须是 `<main id="main-content" tabIndex={-1}>`。测试断言 skip link 的 `href` 与 main 的 `tabindex="-1"`，避免只滚动不转移焦点。
- **GlobalSearch 结果必须是可访问链接语义**：可点击结果用 `<Link role="option" to={result.to}>`，不要用 `<button>` + pointer-only `navigate()` 伪装跳转；键盘 Enter 可以继续调用 `navigate(result.to)` 来激活当前 focusIndex。
- **Search result 只能指向已注册 / 可落地的前端路由**：有详情页的对象链接详情，如 VPS `/vps/:id`、监控实例 `/monitoring/:id`、入口 `/targets/:id`；没有详情页的对象链接列表页或列表筛选，如服务商 `/providers`、订阅 `/subscriptions?vps_id=<vps_id>`。不要生成不存在的 `/providers/:id` 或 `/subscriptions/:id`。
- **列表主扫描路径上的创建/编辑表单优先放 Drawer**：如果创建表单会挤占库存表 / 队列主视图，应使用 `Drawer` 承载，并保留页面主列表可见。关闭 Drawer 时重置草稿/错误；提交成功后的跳转和 payload 合同仍由 page 测试断言。

### 详情页 IA 合同（决策板 + 操作菜单 + 维护 modal）

资产详情页（VPS `/vps/:id`、入口 `/targets/:id`）统一采用「判断在顶、证据居中、配置进弹层」的三段式信息架构。新详情页应对齐：

- **顶部放决策板**：页面第一屏是 DecisionBoard——一张「下一步动作」卡（按运行/健康状态优先级选出单条 CTA）+ 一条 tone 着色的证据条。参考 `web/src/pages/vps-detail/VPSDecisionBoard.tsx` + `vpsDecisionModel.ts` 与镜像它的 `web/src/pages/target-detail/TargetDecisionBoard.tsx` + `targetDecisionModel.ts`。决策模型（`build<X>DecisionModel`）是纯函数，只消费已有 contract 字段算出 `nextAction` 与 `evidenceItems`，不发请求、不发明字段。
- **低频深度报告使用独立页面承载**：IP 质量、性能基准、路由质量这类“买完 VPS 后才通过 agent 测得”的深度报告，字段多、矩阵多、历史/诊断多，不适合塞进 VPS 详情页的一个普通 section。VPS 详情页只保留摘要结论、关键风险/缺口和“查看完整报告”入口；完整驾驶舱应使用独立 route/page 展示质量结论、provider/service 矩阵、覆盖率、历史变化和诊断。这样后续性能、路由报告可以复用同一 IA，而不会把 VPS 详情页变成所有低频事实的长表堆叠。
- **低频报告主视图必须降噪采集字段**：这类报告的 API 往往同时返回用户事实和采集诊断。主视图只能展示用户可判断的质量事实，例如风险信号、provider 证据 chip、服务解锁状态、区域、解锁类型、覆盖率和历史；`source`、`probe_status`、`default_probe`、`not_configured`、latency、长 `error_summary`、raw JSON 等内部采集字段不得进入主卡片、摘要区或表格证据列。需要排障时放入低权重诊断层或折叠详情，并在测试里断言这些内部文本不会出现在主要视图。
- **tone 系统统一四档**：`'normal' | 'notice' | 'alert' | 'critical'`，经 `toneToGlyphState` 映射到 `StatusGlyph` 的 state；CSS 类前缀按页面命名但结构对齐（`target-decision-*` 镜像 `vps-decision-*`），着色走 `var(--color-state-*)` / `color-mix`，不写 hex。新增详情页复制这套 tone→glyph→CSS 约定，不要另造一套色彩语义。
- **二级 / 编辑操作收进右上角 `…` 菜单**：非主 CTA 的操作（查看历史、运行控制、编辑基础信息、资料维护）放进 `watchtower-header` 的 `details.watchtower-actions-menu`，不要散落成页面底部的独立按钮。参考 VPS hero「编辑基础信息」(`web/src/pages/vps-detail/VPSDetailHero.tsx`) 与 Target「资料维护」(`web/src/components/target-detail/TargetWatchtowerHeader.tsx`)。该菜单**始终渲染**——即使某状态（如已归档）下运行控制动作为空，菜单仍要在，否则会丢失维护入口；运行控制按钮列表按状态条件渲染，查看历史/维护项常驻。
- **危险联动流程使用状态驱动入口，不做常驻菜单项**：取消 / 退役 / 迁移这类会影响订阅、监控实例、探测对象或其它关联对象的流程，不能作为普通 `…` 菜单项常驻展示，也不能在详情页中部单独铺一个“待处理”工作区。它应由顶部决策 / 当前判断模型按状态暴露 action，例如 VPS 详情页的 `judgement.primaryAction = { label: '处理取消/退役', mode: 'cancellation' }`；稳定状态返回 `null`。点击后打开居中 `Modal` 加载 preview，让用户显式选择影响对象并确认执行，不得直接提交危险操作。测试必须覆盖：相关状态显示入口、稳定状态隐藏入口、更多菜单没有该危险项、deep link 仍可打开既有 modal。
- **迁移在工作台完成前只能表达为意向**：VPS 页面、资产决策页和 execution plan 不得写“推进迁移”“迁移流程”“迁移工作台”这类暗示已有受控迁移闭环的文案。当前只能写“标记迁移意向”“人工跟进”“复核迁移意向”等，并继续把真实取消/退役动作引导到既有 workbench。
- **当前关注状态归入顶部判断，不铺中部提醒条**：VPS 详情页里的“运行观测需要核对”、缺订阅、缺运行观测、订阅读取失败、IP 质量暂不可用、续费临期 / 自动续费取消等当前需要用户处理或核对的状态，必须进入顶部 `VPSDetailOverviewPanel` 的“当前判断”模型，例如 `judgement.attentionItems`。页面中段的“关联概览”“单机台账”“IP 质量概况”只承载详情摘要和管理入口，不再渲染 `VPSContextActionPanel` / `vps-detail-context-action` 这类横条。多个关注状态必须可并列展示，不能被单个 `primaryAction` 覆盖；稳定状态下不展示额外列表。
- **非实时配置 / 维护 demote 进 modal**：标签备注编辑、归档生命周期这类低频配置从页面主体移入 modal（`web/src/components/atoms/Modal.tsx`），页面主体只保留实时观测证据（决策板、运行控制、ProbeItem 列表与观测、当前异常、事件）。**例外**：本身会再开一个表单 modal 的入口（如 ProbeItem 表单）不要嵌进维护 modal，避免 modal 套 modal——让它贴着对应的实时列表区就近呈现。

---

## 命名约定

| 对象 | 规则 | 例子 |
|------|------|------|
| 组件名 | PascalCase；与文件名一致 | `Sparkline`、`IncidentList`、`StatusBadge` |
| 文件名 | `<ComponentName>.tsx`，与组件名一致 | `Sparkline.tsx`、`IncidentList.tsx` |
| 路由页 | `<Name>Page.tsx`；组件 `export function <Name>Page()` | `MonitoringPage.tsx` → `export function MonitoringPage()` |
| 测试文件 | 同目录、同名 + `.test.tsx` | `Sparkline.test.tsx`、`MonitoringPage.test.tsx` |
| Props 类型 | `<ComponentName>Props`；下面"Props 类型定义模式"详述 | `SparklineProps`、`CardProps`、`IncidentListProps` |
| Hook | `use<Name>` camelCase；返回值结构清晰 | `useAuth`、`useTheme`、`useThemeOptional` |
| Context | `<Name>Context`（内部）+ `<Name>Provider`（导出）+ `use<Name>`（hook） | `ThemeContext` / `ThemeProvider` / `useTheme` |
| CSS class | BEM `block__element--modifier`，与 `styles/atoms.css` / `pages.css` 对齐 | `card--state`、`probe-card__header`、`btn--primary` |

---

## Props 类型定义模式

仓库当前**两种写法并存**，按层落地：

- **`components/atoms/`：统一用 `export interface <Name>Props`**。原因是原子常常需要 `extends HTMLAttributes<...>` / `extends ButtonHTMLAttributes<...>`。
  - 实例：`web/src/components/atoms/Button.tsx:6` `export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>`、`web/src/components/atoms/Card.tsx:7` `export interface CardProps extends Omit<HTMLAttributes<HTMLDivElement>, 'role'>`、`web/src/components/atoms/Sparkline.tsx:14` `export interface SparklineProps`。
- **`components/`（atoms 上一层）与 `pages/`：用 `type <Name>Props = { ... }`**。这些组件通常不扩展 DOM 元素属性，只声明业务 props。
  - 实例：`web/src/components/IncidentList.tsx:22` `type IncidentListProps = { ... }`、`web/src/components/EventList.tsx:20` `type EventListProps = { ... }`、`web/src/components/StatusBadge.tsx:13` `type StatusBadgeProps = { ... }`。
- **强制约束**：
  - 必须用具体类型，**禁止 `any`**（`web/eslint.config.js` 启用了 `typescript-eslint` 推荐规则）。
  - 子节点用 `ReactNode`（直接显式 import）或 `PropsWithChildren<...>`（见 `web/src/components/DetailSection.tsx:1-13`）。
  - 默认值放在解构里：`function Card({ cardRole = 'default', ... }: CardProps)`，**不要**用 React `defaultProps`（已废弃）。

---

## 默认导出 vs 命名导出

**统一用命名导出**。`grep -rn "^export default" web/src/` 在生产代码里返回空。理由：

- 命名导出对 IDE rename / 全局 grep 友好；
- 与 `components/atoms/index.ts` 的 barrel re-export 直接兼容（`export * from './Button'`）。

**例外**：`forwardRef` 包出来的组件用 `export const`（仍是命名），见 `web/src/components/atoms/Input.tsx:11`。

---

## Hook 使用偏好（实读）

- 主要用 `useState` / `useEffect` / `useRef`；用得最多的状态机模式是 page 内局部 `useState`，**未使用** `useReducer`（`grep` 在 `pages/` 里返回空）。
- `useCallback` / `useMemo` 只在 **Provider** 与少数 layout 子组件里出现（如 `web/src/lib/auth-context.tsx:20-43`、`web/src/lib/theme-context.tsx:34-48`）；**page / 普通 component 不要预先 wrap callback**——除非真的因为传给 memo 子组件触发不必要 render，否则直接传函数。
- **不使用 `React.memo`**；如出现性能问题，先确认 props 是否稳定，再考虑提取。
- **不使用 React 19 的 `use()` API / Server Components**：候风是纯客户端 SPA，center 只静态吐 `web/dist/`。
- 自定义 hook 当前未抽出独立目录；`useAuth` / `useTheme` / `useThemeOptional` 都在对应 context 文件内导出。跨组件复用的 modal focus 例外落在 `web/src/lib/useModalFocus.ts`，供 `Drawer` 与 `ChangePasswordModal` 共享。

---

## 何时拆组件 / 抽 hook

> 当前阶段**宁多保留 inline，不要过度抽象**——抽象的代价是后续重构成本。

拆出 `components/<Name>.tsx` 的触发条件（满足任一即可）：

1. 同样的 JSX 结构在 ≥ 2 个 page 出现（参考 `EventList`：被 `EventsPage` + `MonitoringDetailPage` 复用）。
2. 一段 JSX 在单个 page 内重复 ≥ 3 次且参数化清晰（如卡片列表项）。
3. 单文件超过 ~300 行且其中一段已经有清晰边界（注意：当前 `pages/MonitoringDetailPage.tsx` 1138 行、`pages/TargetDetailPage.tsx` 1731 行——属于"已知 gap"，新页面不要再走这条路）。

**不要**为了"将来可能复用"而提前抽组件——内联到稳定后再抽。

抽 hook（即使内联）的触发条件：

1. 同一 page 内有 ≥ 2 个 `useEffect` 围绕同一份 state 协作。
2. 状态更新逻辑超过 ~30 行且有清晰名字（如 `useMonitoringInstanceRuntimeActions(monitoringInstance)`）。

---

## 反模式

> 这些当前代码已经回避或承认为偿还点，**新代码不要做**。

- ❌ **page 内手写 `fetch()`**：必须走 `lib/api.ts`。历史上 `MonitoringPage` 曾直连创建监控实例 API，已偿还为 `createMonitoringInstance` helper；新代码不要恢复直连请求。
- ❌ **components/ 内调 API client**：组合组件保持纯展示 / 受控，数据由 page 拉好后 props 传入。
- ❌ **atoms/ 引用 `lib/types.ts`**：业务无关原则。
- ❌ **`React.FC` / `React.memo` / `defaultProps`**：当前代码风格未用，新代码也别引入。
- ❌ **`any` / 不带类型的 `useState()`**：`useState<T>(initial)` 必须给出 T，或让 initial 推导出 T。
- ❌ **暴露内部 state setter 给父组件**（如 `setOpen` 直接 props 出去）：用受控模式（`open` + `onOpenChange`）或非受控模式（仅 `defaultOpen`），不要混。
- ❌ **组件文件 > 300 行不拆**（已知偿还点：`MonitoringDetailPage.tsx`、`TargetDetailPage.tsx`、`SettingsPage.tsx`、`TargetsPage.tsx` 都超）。新页面应主动按 section 拆 `components/`。
- ❌ **CSS in JS / inline style 写业务样式**：颜色 / 间距用 `tokens.css` 变量；inline `style={}` 仅限尺寸 / 计算量（参考 `web/src/components/atoms/Sparkline.tsx:71`）。
- ❌ **从 `pages/` import 别的 page**：要复用就升到 `components/`。
- ❌ **共享业务组件内写死 `<Link to=...>` 或 import page-private helper**：这会让组件反向感知路由或领域排名逻辑。正确做法是由 page 传入 `action` / `secondaryAction` / `glyph` / `meta` 节点。
- ❌ **绕过 `app/router.tsx` 私加路由 / 用 `<BrowserRouter>` 包裹**：路由唯一入口在 `web/src/app/router.tsx`。
- ❌ **把可点击容器伪装成 link 再嵌套真实 link/button**：这会让屏幕阅读器与键盘行为变得含混。自绘队列需要整行鼠标点击时，外层只处理 pointer click，键盘路径交给内部可见 action；DataTable 行点击使用原子内置的 interactive target guard。

---

## 与 CLAUDE.md 的差异 / 已知 gap

> 用于后续任务评审；若形成可复用规则，更新 `.trellis/spec/` 或当前 active docs。

- **多个页面单文件超过 1000 行**（`MonitoringDetailPage.tsx` 1138、`TargetDetailPage.tsx` 1731、`TargetsPage.tsx` 740、`SettingsPage.tsx` 873）。是**已知技术债**，需在后续按 detail section / form section 拆 `components/`。新页面不要再走这条路。
- **`atoms/` 与上层 `components/` 在 props 类型写法上不一致**（`interface XxxProps` vs `type XxxProps = {}`）。本 spec 把它确认为分层写法，不打算回头统一——以"原子用 interface（要 extends DOM 属性）、业务组件用 type（不需要扩展）"为准则继续走。

---

## Examples

参考实现，新组件请对齐这些范式：

- **设计系统原子（atoms）**：`web/src/components/atoms/Sparkline.tsx`（`export interface SparklineProps` + 默认值解构 + tokens 变量映射）。
- **可点击表格行（DataTable）**：`web/src/components/atoms/DataTable.tsx`（`onRowClick` + interactive descendant guard，内部 action 不触发行导航）。
- **设计系统原子带 ref 转发**：`web/src/components/atoms/Input.tsx`（`forwardRef` + 命名 const 导出）。
- **跨页业务组合组件**：`web/src/components/IncidentList.tsx`（`type IncidentListProps`、纯展示、引用 `lib/types.ts`、按 severity 排序后渲染）。
- **跨页业务组合组件使用插槽保持路由无关**：`web/src/components/ObservabilityEvidenceLead.tsx` / `ObservabilityEvidenceFocus.tsx`（纯展示、`ReactNode` action/meta/glyph 插槽、页面传入 Link/Button/StatusGlyph/Hostname）。
- **页面三态 primitive**：`web/src/components/PageState.tsx`（`kind="loading|error|empty"`、`surface="panel|empty"`、`action` 插槽、`technicalSummary` 摘要）。
- **路由页装配**：`web/src/pages/EventsPage.tsx`（页面拉数据 → loading / error / data 三态 → 组合 `EventList` 与 `DetailSection`）。
- **Provider + hook 配对**：`web/src/lib/theme-context.tsx`（`createContext` + `<Name>Provider` + `use<Name>` + `use<Name>Optional` 测试便利变体）。
