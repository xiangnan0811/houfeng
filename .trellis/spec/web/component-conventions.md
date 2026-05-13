# 组件约定

> **权威来源**：`CLAUDE.md` + 业务/结构以 `docs/design/v1-baseline/` frozen 子集（architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow）为准，视觉以 `docs/design/v2-houfeng/`（design-language / component-spec）为准。冲突时以前述顺序为准。本项目处于初始开发阶段，规则会随代码演进调整。

---

## Overview

候风前端组件统一采用 **函数式组件 + React 19 hooks**，TypeScript 严格 props（`tsconfig.app.json` 启用 strict）。视觉权威是 `docs/design/v2-houfeng/design-language.md` 与 `docs/design/v2-houfeng/component-spec.md`；新组件只参考这两份 v2 文档与现有 `components/atoms/` / `components/filters/` 实现。早期 v1-baseline 视觉稿、stitch 截图和 `v1.x-frontend-redesign/` 已 archive，仅作历史记录，不再作为 active implementation authority。

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
- **跨页业务组合组件可接收 action/meta/glyph 这类 `ReactNode` 插槽**：当组件需要保持 route-agnostic，但页面仍要传入 `<Link>`、`<Button>`、`<StatusGlyph>`、`<Hostname>` 等具体组合时，使用受控插槽，不在组件内 import `react-router-dom` 或领域 helper。参考 `web/src/components/ObservabilityEvidenceLead.tsx` 与 `web/src/components/ObservabilityEvidenceFocus.tsx`：Nodes / Targets / Events 共享 lead/focus 骨架，但页面自己决定 action、glyph、meta 和路由。
- **route/detail/list 的 loading / error / empty 状态优先复用 `PageState`**：`web/src/components/PageState.tsx` 是跨页展示 primitive，保持 route-agnostic，通过 `action` slot 接收 `<Link>` / `<Button>` / page callback；页面不要继续手写裸 `page-panel` loading/error，列表空态需要 v2 空态装饰和 CTA 时使用 `surface="empty"`。错误摘要用 `technicalSummary`，组件会截断并避免和 description 重复显示。
- **DataTable 可点击行与行内操作的合同**：`web/src/components/atoms/DataTable.tsx` 的 `onRowClick` 只处理非交互子节点上的 click / Enter / Space；事件目标落在 `a[href]`、`button`、`input`、`select`、`textarea`、`role="button"`、`role="link"` 内时，表格行不得触发行导航。page 的 action cell 不需要再为了 DataTable 行点击重复写 `stopPropagation`，但自绘 list/queue 不是 DataTable 时仍必须在内部 `<Link>` / `<Button>` 上显式阻止冒泡。
- **自绘可点击队列不要制造嵌套交互语义**：如果一个 `<li>` / `<article>` 内部已经有可见 `<Link>` 和 `<Button>`，可以让鼠标点击行背景进入主详情，但不要给外层容器加 `role="link"` / `tabIndex=0` 再包住内部交互控件；键盘入口应落在可见 action 上，外层只用 `:focus-within` 做焦点视觉辅助。
- 当前**未单独建 `hooks/` 目录**；本地 hook 内联在使用文件内即可，需要跨文件再考虑提取（届时落点为 `web/src/lib/use<Name>.ts` 或新增 `web/src/lib/hooks/`，需另做决策）。
- **modal / drawer focus 行为复用 `web/src/lib/useModalFocus.ts`**：可访问性弹层必须 portal 到 `document.body`，声明 `role="dialog"` / `aria-modal="true"`（确认类用 `alertdialog`），打开后移动初始焦点，Tab / Shift+Tab containment，Escape 关闭，关闭后恢复触发器焦点；不要在各组件里复制 ad-hoc `document.addEventListener('keydown')` + 手写 focus trap。

---

## 命名约定

| 对象 | 规则 | 例子 |
|------|------|------|
| 组件名 | PascalCase；与文件名一致 | `Sparkline`、`IncidentList`、`StatusBadge` |
| 文件名 | `<ComponentName>.tsx`，与组件名一致 | `Sparkline.tsx`、`IncidentList.tsx` |
| 路由页 | `<Name>Page.tsx`；组件 `export function <Name>Page()` | `NodesPage.tsx` → `export function NodesPage()` |
| 测试文件 | 同目录、同名 + `.test.tsx` | `Sparkline.test.tsx`、`NodesPage.test.tsx` |
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

1. 同样的 JSX 结构在 ≥ 2 个 page 出现（参考 `EventList`：被 `EventsPage` + `NodeDetailPage` 复用）。
2. 一段 JSX 在单个 page 内重复 ≥ 3 次且参数化清晰（如卡片列表项）。
3. 单文件超过 ~300 行且其中一段已经有清晰边界（注意：当前 `pages/NodeDetailPage.tsx` 1138 行、`pages/TargetDetailPage.tsx` 1731 行——属于"已知 gap"，新页面不要再走这条路）。

**不要**为了"将来可能复用"而提前抽组件——内联到稳定后再抽。

抽 hook（即使内联）的触发条件：

1. 同一 page 内有 ≥ 2 个 `useEffect` 围绕同一份 state 协作。
2. 状态更新逻辑超过 ~30 行且有清晰名字（如 `useNodeRuntimeActions(node)`）。

---

## 反模式

> 这些当前代码已经回避或承认为偿还点，**新代码不要做**。

- ❌ **page 内手写 `fetch()`**：必须走 `lib/api.ts`。历史上 `NodesPage` 曾直连创建节点 API，已偿还为 `createNode` helper；新代码不要恢复直连请求。
- ❌ **components/ 内调 API client**：组合组件保持纯展示 / 受控，数据由 page 拉好后 props 传入。
- ❌ **atoms/ 引用 `lib/types.ts`**：业务无关原则。
- ❌ **`React.FC` / `React.memo` / `defaultProps`**：当前代码风格未用，新代码也别引入。
- ❌ **`any` / 不带类型的 `useState()`**：`useState<T>(initial)` 必须给出 T，或让 initial 推导出 T。
- ❌ **暴露内部 state setter 给父组件**（如 `setOpen` 直接 props 出去）：用受控模式（`open` + `onOpenChange`）或非受控模式（仅 `defaultOpen`），不要混。
- ❌ **组件文件 > 300 行不拆**（已知偿还点：`NodeDetailPage.tsx`、`TargetDetailPage.tsx`、`SettingsPage.tsx`、`TargetsPage.tsx` 都超）。新页面应主动按 section 拆 `components/`。
- ❌ **CSS in JS / inline style 写业务样式**：颜色 / 间距用 `tokens.css` 变量；inline `style={}` 仅限尺寸 / 计算量（参考 `web/src/components/atoms/Sparkline.tsx:71`）。
- ❌ **从 `pages/` import 别的 page**：要复用就升到 `components/`。
- ❌ **共享业务组件内写死 `<Link to=...>` 或 import page-private helper**：这会让组件反向感知路由或领域排名逻辑。正确做法是由 page 传入 `action` / `secondaryAction` / `glyph` / `meta` 节点。
- ❌ **绕过 `app/router.tsx` 私加路由 / 用 `<BrowserRouter>` 包裹**：路由唯一入口在 `web/src/app/router.tsx`。
- ❌ **把可点击容器伪装成 link 再嵌套真实 link/button**：这会让屏幕阅读器与键盘行为变得含混。自绘队列需要整行鼠标点击时，外层只处理 pointer click，键盘路径交给内部可见 action；DataTable 行点击使用原子内置的 interactive target guard。

---

## 与 CLAUDE.md 的差异 / 已知 gap

> 用于喂 `docs/release/v1-gap-checklist.md`。

- **多个页面单文件超过 1000 行**（`NodeDetailPage.tsx` 1138、`TargetDetailPage.tsx` 1731、`TargetsPage.tsx` 740、`SettingsPage.tsx` 873）。是**已知技术债**，需在后续按 detail section / form section 拆 `components/`。新页面不要再走这条路。
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
