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
- **modal / dialog focus 行为复用 `web/src/lib/useModalFocus.ts` 与 `web/src/lib/modalStack.ts`**：可访问性弹层必须 portal 到 `document.body`，确认类用 `alertdialog`；只有栈顶声明 `aria-modal="true"` 并处理 Tab / Escape / backdrop，非栈顶设置 `aria-hidden` + `inert`。`persistent` 栈顶忽略 Escape/backdrop，只允许显式关闭。不要在各组件里复制 ad-hoc keydown、focus trap、body overflow 或 overlay Escape。
- **Drawer 取消/关闭必须清理未提交本地状态**：page 用 Drawer 承载 create/edit 表单时，`onClose` / 取消按钮 / Escape / overlay 关闭都必须丢弃 draft、表单错误和保存反馈；重新打开应从当前已保存数据或初始空表单重建。测试至少覆盖“编辑草稿 → 取消关闭 → 重新打开草稿已重置”以及取消不触发提交。
- **复杂表单 modal 必须有可读宽度和收束行为**：创建/编辑订阅、VPS 基础信息、监控实例接入、服务商等字段密集表单使用 `Modal` 的 `md` / `lg` / `xl` 尺寸，避免默认窄弹窗造成标签和命令无意义换行。提交成功必须关闭或跳转；取消必须丢弃草稿；失败留在当前弹层并展示错误。由 URL deep-link 打开的弹层在消费或关闭时必须清理 `create=1`、`onboarding=1` 等临时参数，同时保留承接上下文参数。
- **常见有界字段优先选择器 + 自定义入口**：VPS 国家/地区、订阅币种、支付方式、计费周期单位、续费方式这类高频字段必须使用共享 option/helper 与 `<select>` / radio 控件；常见值内置，确实不在范围内才进入“自定义/其他”。不要把这些字段退回裸文本输入，也不要在创建和编辑表单各自散落字符串。
- **关联表单优先选择器，不让用户复制内部 ID**：Provider、MonitoringInstance、Target、Service 这类已有业务对象的常规关联表单应渲染可辨识 `<select>`/selector（名称 + ID + 状态/位置等辅助信息），并保留空值/不关联能力。无候选或加载失败时用说明 + `<Link>`/action 指向对应列表/创建入口；MonitoringInstance/Target 选择只作为用户确认的资产关联，不在表单提交时隐式修改观测运行态。
- **复杂前端变更先走视觉伴随评审**：涉及新页面、重要详情区块、dashboard/工作台、复杂数据矩阵、资产决策、低频深度报告这类信息架构或视觉层级不明确的任务，开发前必须使用相关 skills（尤其是 brainstorming / frontend-design）产出可在浏览器打开的 mockup，让用户先确认展示密度、字段优先级和页面承载方式。不要只用文字方案或直接实现来猜 UI；mockup 可放在 ignored 的 `.superpowers/brainstorm/`，正式实现仍遵守本 spec 的 tokens、BEM、中文文案和测试要求。用户已项目级预授权：以后类似 UI/IA/视觉层级任务直接启动可视化 companion，不再先询问是否启用；只有当任务完全不涉及视觉判断，或运行环境无法启动本地 companion 时，才用文字说明替代并记录原因。

### AppShell / Command Search 交互合同

- **Skip link 必须有可聚焦目标**：AppShell 顶部使用 `<a className="skip-link" href="#main-content">跳到主内容</a>`，主区域必须是 `<main id="main-content" tabIndex={-1}>`。测试断言 skip link 的 `href` 与 main 的 `tabindex="-1"`，避免只滚动不转移焦点。
- **GlobalSearch 结果必须是可访问链接语义**：可点击结果用 `<Link role="option" to={result.to}>`，不要用 `<button>` + pointer-only `navigate()` 伪装跳转；键盘 Enter 可以继续调用 `navigate(result.to)` 来激活当前 focusIndex。
- **Search result 只能指向已注册 / 可落地的前端路由**：有详情页的对象链接详情，如 VPS `/vps/:id`、监控实例 `/monitoring/:id`、入口 `/targets/:id`；没有详情页的对象链接列表页或列表筛选，如服务商 `/providers`、订阅 `/subscriptions?vps_id=<vps_id>`。不要生成不存在的 `/providers/:id` 或 `/subscriptions/:id`。
- **通知入口必须是真实链接**：TopBar 通知图标使用 `<Link to="/events?notification_only=1" aria-label="查看通知事件">`，由 EventsPage 已有 query contract 承接。没有真实 count contract 时不得渲染固定 0、占位 badge 或无 handler 的 button。
- **列表主扫描路径上的创建/编辑表单优先放 Drawer**：如果创建表单会挤占库存表 / 队列主视图，应使用 `Drawer` 承载，并保留页面主列表可见。关闭 Drawer 时重置草稿/错误；提交成功后的跳转和 payload 合同仍由 page 测试断言。

### Scenario: 原生字段、Tabs、值选择与菜单合同

#### 1. Scope / Trigger

- Trigger: 修改 `Input` / `Select`、互斥内容切换、pill 值选择器、AppShell 菜单或带整行 pointer 导航的列表时，必须使用本合同。
- 目标：表单状态落到原生 control；真正的内容面使用 Tabs/TabPanel；过滤/值选择使用 SegmentedControl；命令与导航使用 button/Link，容器点击只作为受控增强。

#### 2. Signatures

```ts
interface TabsProps<V extends string = string> {
  label: string
  idBase: string
  items: readonly TabItem<V>[]
  value: V
  onChange: (next: V) => void
  variant?: 'underline' | 'pill'
}

interface TabPanelProps<V extends string = string> {
  idBase: string
  value: V
  className?: string
  children: ReactNode
}

interface SegmentedControlProps<V extends string = string> {
  label: string
  items: readonly SegmentedItem<V>[]
  value: V
  onChange: (next: V) => void
}
```

- `tabId(idBase, value)` 与 `tabPanelId(idBase, value)` 位于 `web/src/components/atoms/tabIds.ts`，是 tab/panel id 的唯一 owner。
- `InputProps` / `SelectProps` 继续扩展原生 HTML attributes，并用 `forwardRef` 把 ref 指向真实 `<input>` / `<select>`。

#### 3. Contracts

- Input/Select 以 `id ?? useId()` 为 control id，当前可见 error/hint 分别使用 `<id>-error` / `<id>-hint`。`aria-describedby` 按“调用者 token → 当前内部 id”合并、按空白拆分去重；error 覆盖 hint，并强制 `aria-invalid=true`。无 error 时保留调用者的 `aria-invalid`。
- `required` 同时传给原生 control 和 required label class；options/children、原生 onChange、className 和其余 DOM attributes 不得被 atom 吞掉。
- Tabs 必须有可访问名称；selected tab 是唯一 `tabIndex=0`。ArrowLeft/Right 循环，Home/End 到边界，移动焦点并自动调用 `onChange`。controlled value 暂时不在 items 中时，第一项成为唯一 tab stop；空 items 不抛错。
- 真正的互斥内容面用 `Tabs` + active `TabPanel`，双方 id/labelledby/controls 必须闭环。主题、时间范围、快速视图等值选择使用 `SegmentedControl` 的 native buttons + `aria-pressed`，不得伪装成无 panel 的 tabs。
- User/theme menu trigger 使用 native button + `aria-haspopup="menu"` / `aria-controls` / `aria-expanded`。menu command 使用 `menuitem`，单选主题使用 `menuitemradio` + `aria-checked`；Arrow/Home/End 导航、Escape 返回 trigger、Tab 离开并关闭、outside pointer 关闭。
- 列表主导航必须有可见 Link。保留整行 click 时，事件目标位于 link/button/input/select/textarea 或对应 role 内必须忽略；外层不得再增加模拟 link 的 role/tabIndex，键盘只依赖主 Link。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| caller describedby 与内部 error id 重复 | 输出一次，caller token 顺序保留 |
| error 从无变有 | hint 不渲染/不被引用；control invalid=true |
| Tabs value 不在动态 items 中 | 第一项是唯一 tab stop，不在 render 中隐式改 value |
| ArrowRight 位于最后一个 tab | focus/selection 循环到第一项，onChange 一次 |
| 值选择器没有对应内容 panel | 使用 SegmentedControl，不产生 tab/tablist/tabpanel role |
| menu Escape / Tab | Escape 关闭并回 trigger；Tab 关闭且真实焦点继续前移 |
| pointer 点击 VPS 名称 Link | Link 自己导航，row enhancement 不重复 navigate |

#### 5. Good / Base / Bad Cases

- Good：Settings 的“外观/通知”使用 Tabs + TabPanel；VPS 的“全部/未关联”使用 SegmentedControl；浏览器中 Tab 进入一个 selected tab，ArrowRight 激活下一 panel。
- Base：Input 只有调用者 `aria-describedby` 且无 hint/error，原样保留；普通原生 attrs/ref 继续工作。
- Bad：所有 pill 都写 `role="tab"` 却没有 panel；在 `div` 上补 `role="button"` 模拟命令；整行 `<tr>` 成为 VPS 详情唯一键盘入口。

#### 6. Tests Required

- `Input.test.tsx` / `Select.test.tsx`：required、ref、generated/explicit id、error/hint、describedby 去重、invalid precedence、options/children。
- `Tabs.test.tsx` / `SegmentedControl.test.tsx`：命名、唯一 tab stop、四个导航键、动态缺失 value、id 闭环、pressed buttons 与 generic value。
- 所有 Tabs 调用迁移后用 page tests 断言 active tab 的 `aria-controls` 指向 DOM panel，panel `aria-labelledby` 指回 tab；值选择器断言 group/button，不查询虚假 tab。
- `UserChip.test.tsx` / `TopBar.test.tsx`：menu ownership、Arrow/Home/End、Escape/Tab/outside、callback 单次调用与 focus return；真实 Chromium补 Tab 默认焦点前移证据。
- `AppShell.test.tsx` / `VPSPage.test.tsx`：skip link/main target、主 Link href、Link click 不触发行增强、背景 click 仍可用。

#### 7. Wrong vs Correct

```tsx
// Wrong: 过滤器冒充 tab，整行是唯一导航入口。
<div role="tab" onClick={() => setView('unlinked')}>未关联</div>
<tr onClick={() => navigate(`/vps/${id}`)}><td>{name}</td></tr>

// Correct: 值选择与主导航都使用原生元素。
<SegmentedControl label="VPS 快速视图" items={views} value={view} onChange={setView} />
<tr onClick={handleBackgroundClick}>
  <td><Link to={`/vps/${id}`}>{name}</Link></td>
</tr>
```

### Scenario: 应用与路由错误恢复

#### 1. Scope / Trigger

- Trigger: 修改 `web/src/main.tsx` provider/root 结构、`app/router.tsx` route tree、React.lazy 路由加载边界、`AppErrorBoundary` 或 `RouteErrorPage` 时，必须遵守本合同。
- 目标：provider/render exception 与 route render/lazy chunk failure 都有安全恢复面，不让用户停在空白页，也不把异常对象、URL、token 或 stack 直接渲染到 DOM。

#### 2. Signatures

```tsx
<AppErrorBoundary>
  <ThemeProvider>
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>
  </ThemeProvider>
</AppErrorBoundary>

type RouteObject = {
  element: ReactNode
  errorElement: ReactNode
}
```

- `AppErrorBoundary` 是 class error boundary；`RouteErrorPage` 通过 `useRouteError()` 读取错误，但只能把完整对象写入 console/log，不能交给 `PageState.technicalSummary`。

#### 3. Contracts

- `AppErrorBoundary` 必须位于 Theme/Auth/Router provider tree 外层，覆盖 provider 初始化与 Router render；fallback 不依赖这些 provider 才能显示。
- 登录 route 与受保护 route tree 都必须配置 `errorElement={<RouteErrorPage />}`，让 render throw 和 rejected dynamic import 使用产品恢复面，不落到 React Router 默认开发错误页。
- 根恢复面提供“重试渲染”“刷新页面”“返回工作台”；路由恢复面提供“重试当前页面”“刷新页面”“返回工作台”。根层返回使用普通 `<a href="/">`，路由层可使用 `<Link>`。
- retry 只重建当前 React/route surface；lazy import rejection 可能被 React.lazy 缓存，因此必须同时保留 full reload 路径。
- 用户可见描述使用固定安全中文摘要；禁止渲染 `error.message`、`String(error)`、stack、chunk URL 或任意 raw exception。完整异常只进入 `console.error` / 运行日志。
- error boundary 不承诺捕获 event handler、timer 或任意异步 Promise 错误；这些路径仍需在各自调用边界显式处理。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| provider / Router 外层 render throw | `AppErrorBoundary` 显示应用恢复面；可重试、刷新、返回 `/` |
| protected route component render throw | `RouteErrorPage` 显示页面恢复面；raw error 不在 DOM |
| React.lazy import rejects | 同一安全页面；full reload 可在 chunk 恢复后重新加载 |
| route retry 后组件不再抛错 | errorElement 清除，当前 route 正常显示 |
| 异常 message 含 URL/token/stack | 只出现在 console/log，DOM 文本不包含该字符串 |

#### 5. Good / Base / Bad Cases

- Good：Events route chunk 首次请求失败，用户看到安全恢复页；资源恢复后点击“刷新页面”回到事件流。
- Base：一次 transient render error 点击“重试当前页面”后恢复，无需 full reload。
- Bad：只保留 `Suspense` loading fallback；import reject 后页面空白。
- Bad：`<PageState technicalSummary={String(useRouteError())}>` 把内部 URL 或异常详情暴露给用户。
- Bad：只在 Router 内加一层 boundary，Theme/Auth provider 自身抛错时仍全白。

#### 6. Tests Required

- `AppErrorBoundary.test.tsx`：child render throw、安全文案、raw secret 不在 DOM、reload callback、retry 后恢复、返回工作台 href。
- `RouteErrorPage.test.tsx`：route render throw、rejected lazy import、安全文案、raw secret 不在 DOM、route retry、full reload 与返回工作台。
- `router.test.tsx`：受保护 route match 中至少一层存在 `errorElement`；入口合同测试确保根 provider tree 被 `AppErrorBoundary` 包裹。
- 本地 Chromium 故障注入：阻断一个未加载 route chunk，确认恢复面出现；解除阻断并刷新后 route 恢复。该证据不替代 unit/build gate。

#### 7. Wrong vs Correct

```tsx
// Wrong: Suspense 只覆盖 pending，不覆盖 rejected import；raw error 还会泄露。
<Suspense fallback={<RouteModuleFallback />}>
  <LazyPage />
</Suspense>
<p>{String(error)}</p>

// Correct: route 与 provider tree 各有恢复边界，UI 使用固定安全摘要。
{
  element: <RequireAuth />,
  errorElement: <RouteErrorPage />,
  children: protectedRoutes,
}
```

### 详情页 IA 合同（决策板 + 操作菜单 + 维护 modal）

资产详情页（VPS `/vps/:id`、入口 `/targets/:id`）统一采用「判断在顶、证据居中、配置进弹层」的三段式信息架构。新详情页应对齐：

- **顶部放决策板**：页面第一屏是 DecisionBoard——一张「下一步动作」卡（按运行/健康状态优先级选出单条 CTA）+ 一条 tone 着色的证据条。参考 `web/src/pages/vps-detail/VPSDecisionBoard.tsx` + `vpsDecisionModel.ts` 与镜像它的 `web/src/pages/target-detail/TargetDecisionBoard.tsx` + `targetDecisionModel.ts`。决策模型（`build<X>DecisionModel`）是纯函数，只消费已有 contract 字段算出 `nextAction` 与 `evidenceItems`，不发请求、不发明字段。
- **决策类页面信息层级契约**（适用 `/asset-decisions` 及同类决策/工作台页面）：
  1. **默认层只回答一个问题**：“现在最该处理什么？”——一个主判断 + 一个主动作。无待办时显示一行稳定提示，不渲染 CTA、警示色、统计卡。
  2. **次级层是扫描列表**：每项一行，身份 + 状态 + 单一入口，无解释句。辅助入口（历史/模板/续费事实/单台队列）默认收起为工具条，桌面一行、移动端 2×2，点击展开对应单一面板。
  3. **详情层是弹窗**：弹窗内 ≤3 个 Tab，每个 Tab 单一任务。默认 Tab 只含对象名 + 一句判断 + 主动作。API 长文案（`comparison_insight.summary`、`execution_readback.summary` 等）裁成短判断，不原样展示。
  4. **底稿层是原始数据**：成员全量、宽表、执行细节默认折叠，显式进入。成员数组默认预览 ≤3 行 + “查看全部”跳底稿；写入 payload 仍用全量。
  5. **文案零解释**：无说明性段落；eyebrow 全中文或去除（不渲染 `PORTFOLIO`/`RENEWAL`/`WORKBENCH` 等英文噪声）；字段含义靠标签和占位符自解释。内部 ID（`adg_`/`admg_`/`adr_`/`adt_`）、后端 group type 机器值不进入用户可见层。
  6. **弹窗内容面不透明**：`var(--surface-elevated)`，overlay 半透明，底层页面文字不得透进弹窗。
  7. **测试以用户任务为正向断言**（能在 ≤3 步内完成 X），辅以行数守护；不以“旧 marker 不出现”为唯一标准。
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
- 自定义 hook 当前未抽出独立目录；`useAuth` / `useTheme` / `useThemeOptional` 都在对应 context 文件内导出。跨组件复用的 modal focus 例外落在 `web/src/lib/useModalFocus.ts`，供通用 `Modal` 与 `ChangePasswordModal` 共享。

### Scenario: Modal 栈、嵌套焦点与滚动锁

#### 1. Scope / Trigger

- 新建或修改 portal dialog、确认弹层、persistent 表单，或让一个弹层再打开另一个弹层时，必须使用本合同。
- `Modal` 公共 props 保持业务兼容；栈、父子关系、top class 和滚动锁是内部实现，不由业务 page 自行维护。

#### 2. Signatures

```ts
type ModalStackEntry = {
  id: string
  container: HTMLElement
  restoreTarget: HTMLElement | null
  parentId?: string | null
}

registerModal(entry: ModalStackEntry): () => void
isTopModal(id: string): boolean
getModalDepth(id: string): number
subscribeModalStack(listener: () => void): () => void
acquireBodyScrollLock(): () => void

useModalFocus<T extends HTMLElement>(
  active: boolean,
  onClose: () => void,
  modalId: string,
  dismissOnEscape?: boolean,
  parentModalId?: string | null,
): RefObject<T | null>
```

`modalId` 使用 `useId()` 保持实例稳定；cleanup 必须幂等。重复 id 的旧 cleanup 通过 registration token 隔离，不得删除新 registration。

#### 3. Contracts

- 栈按父子拓扑排序，再以注册顺序处理无父子关系的弹层；即使子层比父层先执行 effect，同次挂载后也必须是子层置顶。
- 嵌套 `Modal` 通过内部 React context 声明 `parentId`；业务上作为 sibling 渲染的确认弹层可从打开时的 restore target 所在 dialog 推断父层。推断时不得把已经指向当前 modal 的后代误认成父层并形成环。
- 只有 top dialog 处理 Escape、Tab / Shift+Tab 和 backdrop。非 top dialog 移除 `aria-modal`，设置 `aria-hidden="true"`、`inert`，对应 overlay 不带 `modal-stack-layer--top`。
- top overlay 使用 CSS class `modal-stack-layer--top` 提升层级；禁止为了排序移动 React portal DOM，也禁止新增 inline `z-index`，前者会丢焦点，后者会破坏严格 CSP。
- `persistent` 只禁止 Escape/backdrop dismiss；标题栏关闭、取消或业务显式关闭仍有效。
- body scroll lock 是引用计数：首次 acquire 保存原 `body.style.overflow`，最后一次 release 才恢复原值；每个 release 可重复调用。
- 子层关闭时，父层解除 `inert` 前不能同步聚焦其触发器。若 restore target 位于 inert 子树，使用 microtask 延后恢复，并在执行前重新检查 target 仍连接且已不在 inert 子树；父层仍保持 body lock。
- 延迟恢复不得覆盖更晚的显式业务焦点：microtask 执行时若已有连接、非 inert、非 `body` 的 active element，保留该焦点。若状态更新把原触发按钮替换成 successor（例如“归档”变“恢复到暂停”），业务 focus effect 也要延后到父层解除 inert，再聚焦已连接的新 ref。
- 关闭最后一层后恢复页面触发器与原 body overflow；异常 unmount、StrictMode effect 重放和已被移除的 restore target 均不得抛错或产生负计数。

#### 4. Validation & Error Matrix

| 条件 | 必须结果 |
|------|----------|
| 单层普通 Modal | 初始焦点进入本层；Escape/backdrop 关闭；焦点回触发器 |
| 两层或三层 Modal | 仅最上层可交互；一次 Escape 只关闭一层；父层 scroll lock 保留 |
| 父子同次挂载、子 effect 先运行 | 父深度小于子深度；子层拥有 top class 与焦点；不得形成 parent cycle |
| persistent 位于栈顶 | Escape/backdrop 被消费但不关闭；显式关闭可用 |
| restore target 仍在 inert 父层 | 等父层解除 inert 后再聚焦，不得落到 `body` |
| restore target 已卸载 | 跳过恢复，不抛异常 |
| 原触发按钮被状态更新替换 | 保留业务显式聚焦的新 successor，不被旧 restore target 抢回 |
| cleanup 重复、旧 registration 晚清理 | 当前 registration 与其他 scroll owner 不受影响 |

#### 5. Good / Base / Bad Cases

- Good：Asset Decisions 详情 Modal 打开确认 `alertdialog`；父层立即 inert，Tab 只在确认层循环；第一次 Escape 回到父层原按钮且 body 仍锁定，第二次 Escape 回页面触发器并释放滚动锁。
- Base：单层 `Modal` / `ChangePasswordModal` 使用稳定 id、共享 hook 和协调器，调用方 props 不变。
- Bad：每个实例各自清空 `body.style.overflow`，overlay 和 document 同时监听 Escape，或在父层仍 inert 时直接 `restoreTarget.focus()`。

#### 6. Tests Required

- `modalStack.test.ts`：one-based depth、top、父子反序注册、重复 id stale cleanup、subscribe 和引用计数滚动锁。
- `useModalFocus.test.tsx`：StrictMode 仅一份 registration、最新 onClose callback、unmount 恢复与 scroll release。
- `Modal.test.tsx`：单/双/三层、父子同次挂载、persistent、top class、ARIA/inert、Tab、逐层 Escape、真实 inert 延迟恢复和显式业务焦点优先级。
- 业务回归至少覆盖一个真实嵌套确认流程，断言父层 tab / URL / 草稿保持；修改共享行为后搜索并迁移所有 persistent Modal 的旧 Escape 期望。
- 浏览器 sanity 至少覆盖 `1440x1000`、`1024x768`、`390x900`：无页面横向溢出；top dialog 在视口内；真实键盘 Tab/Escape；父层 focus restore；最后一层关闭后 body unlock；无 console/page/CSP error。

#### 7. Wrong vs Correct

```tsx
// Wrong: 每层重复键盘与滚动副作用，子层关闭会提前解锁并可能连关父层。
useEffect(() => {
  document.body.style.overflow = open ? 'hidden' : ''
  document.addEventListener('keydown', onEscape)
  return () => {
    document.body.style.overflow = ''
    document.removeEventListener('keydown', onEscape)
  }
}, [open])

// Correct: 稳定 id 接入共享栈；persistent 只改变栈顶 Escape 合同。
const modalId = useId()
const modalRef = useModalFocus<HTMLDivElement>(
  open,
  onClose,
  modalId,
  !persistent,
  parentModalId,
)
```

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
- ❌ **CSS in JS / 生产 JSX `style=`**：严格 CSP 下颜色 / 间距走 `tokens.css` + BEM，动态 SVG 几何走 attributes，比例与列宽分别使用 `<progress>` / `<col width>`；详见 `styling-guidelines.md` 的严格 CSP 合同。
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
