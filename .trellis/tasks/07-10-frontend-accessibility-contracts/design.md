# 可访问性交互契约设计

## 1. Design Principles

1. **原生优先**：导航用 Link/anchor，命令用 button，表单状态落到原生 control；只在原生元素无法表达复合模式时使用 ARIA。
2. **语义与视觉解耦**：pill 外观不等于 tabs。互斥内容面用 Tabs；过滤器/值选择用 pressed button group。
3. **一个行为一个 owner**：Input/Select 拥有 field description，Tabs 拥有 roving focus/id，UserChip 与 ThemeSwitcher 各自拥有菜单状态；Task 10 只消费这些稳定合同。
4. **失败关闭**：动态 tab value 缺失、无 panel 的 tab、未解释的 non-semantic click、菜单焦点丢失都必须由测试暴露，不能退化为静态视觉通过。
5. **不伪造验证层级**：RTL 证明 DOM/事件合同，本地 Chromium/axe 证明浏览器行为，Task 10 才建立持久化 CI gate。
6. **最小业务变化**：URL、API、可见菜单命令、主题值、列表数据和 mutation callback 保持不变；本任务改变的是入口语义与焦点路径。

## 1.1 Alternatives Considered

### A. 所有 pill 都继续作为 Tabs

实现量最少，但主题、时间范围和快速筛选没有 tabpanel，读屏会宣布不存在的 tab 结构。该方案违反 ARIA tabs 模式，不采用。

### B. Tabs 与 SegmentedControl 分离（采用）

真正视图切换获得完整 tab/tabpanel 与 roving focus；值选择器使用 `role="group"` + native buttons + `aria-pressed`。可以保留同一 pill CSS 与泛型 value 形状，同时让语义准确。代价是迁移 16 个调用，但 TypeScript 会帮助完成全量迁移。

### C. 建立通用 Menu framework

User menu 与 Theme menu 数据形状、选择语义、关闭后的业务动作不同。现在引入 render-prop/global menu framework 会把两个局部问题变成新的设计系统层。本任务采用两个小型受控实现和一致测试；若未来出现第三个同类 menu，再基于已验证行为抽象。

### D. 给 clickable div/tr 统一补 role/tabIndex/keyDown

这会继续把大量容器模拟成控件，制造嵌套交互和重复 tab stop。真实命令改用 native element；只有 backdrop、事件隔离、keyboard-complete composite row 与有主 Link 的 pointer enhancement 进入注释化例外。

## 2. Current Inventory

| Surface | Current evidence | Target |
| --- | --- | --- |
| Input | 110 production calls；无 hint/error 描述关系 | atom 一次修复且兼容全部 callers |
| Select | 17 production calls；required 被解构后丢失；无测试 | 原生 required/ref/description/invalid |
| Tabs | 16 production calls；全部 tab stop；无 label/panel | 10 个真实 Tabs，6 个 SegmentedControl |
| User menu | Sidebar clickable div；另有未使用 UserChip | Sidebar 只复用一套 UserChip menu button |
| Theme menu | trigger button + 4 clickable div | menuitemradio buttons + keyboard/focus return |
| Skip link | main 有 id，无 tabIndex/skip link | 第一 focusable anchor + programmatic-focus target |
| Nonsemantic click | AST 9 个，3 个真实缺口 | 真实缺口归零；6 类现存结构逐项解释/收敛 |

## 3. Target Component Boundaries

```text
Input / Select
  -> field id + visible description ids + required/invalid/ref

Tabs + TabPanel
  -> actual mutually exclusive content surfaces
  -> automatic activation + roving focus + deterministic ids

SegmentedControl
  -> filter/value selection
  -> native buttons + aria-pressed; no tabpanel claim

UserChip (used by Sidebar)
  -> user menu trigger/menu/actions/focus

ThemeSwitcher (TopBar-local)
  -> radio menu + theme context action/focus

semanticInteractionContract.test.ts
  -> TypeScript AST inventory + finite comment reasons
```

No component above imports API clients or business record types. Page/domain callers keep their existing controlled state.

## 4. Form Field Contract

### 4.1 IDs And Description Merge

Input and Select share a small non-React helper colocated under `components/atoms/`:

```ts
function mergeAriaTokens(...values: Array<string | undefined>): string | undefined {
  const tokens = values.flatMap((value) => value?.split(/\s+/).filter(Boolean) ?? [])
  const merged = [...new Set(tokens)]
  return merged.length > 0 ? merged.join(' ') : undefined
}
```

For `controlId = id ?? useId()`:

- visible error id: `${controlId}-error`;
- visible hint id: `${controlId}-hint`;
- `aria-describedby`: caller tokens first, then the one visible internal id;
- no error/hint: do not emit an empty attribute;
- error: force `aria-invalid=true`;
- no error: preserve caller `aria-invalid`, including an explicitly supplied true.

The helper only manipulates idref tokens. It does not inspect ReactNode text or create hidden duplicate content.

### 4.2 Required And Ref

Both components destructure `required` only to derive the label class, then explicitly pass it to the native element. `forwardRef` remains unchanged and the ref points to `<input>` / `<select>`, not the shell.

Caller compatibility:

- explicit `id`, `className`, `name`, `disabled`, `readOnly`, `onChange`, data attributes and all remaining native props still pass through;
- Select keeps both `options` and `children` modes;
- visible error-over-hint precedence stays unchanged;
- no `aria-errormessage` is added because the approved contract uses describedby and some callers may render non-validation guidance as error text.

## 5. Tabs And Selection Design

### 5.1 Tabs API

```ts
export interface TabsProps<V extends string = string> {
  label: string
  idBase: string
  items: readonly TabItem<V>[]
  value: V
  onChange: (next: V) => void
  variant?: 'underline' | 'pill'
}

export function tabId(idBase: string, value: string): string
export function tabPanelId(idBase: string, value: string): string

export interface TabPanelProps<V extends string = string> {
  idBase: string
  value: V
  className?: string
  children: ReactNode
}
```

`TabPanel` renders `role="tabpanel"`, deterministic id/labelledby and `tabIndex=0`. Callers render the active panel only; inactive content keeps the current unmounted behavior.

### 5.2 Roving Focus

- selected item gets `tabIndex=0`; all others get `-1`.
- if controlled `value` is temporarily absent from dynamic items, the first item becomes the single tab stop without firing `onChange` during render.
- ArrowRight/ArrowLeft wrap; Home/End select boundaries.
- ArrowLeft/ArrowRight/Home/End handling always calls `preventDefault()`, focuses the target button, then calls `onChange` with that item value (automatic activation).
- empty items render a named empty tablist with no crash; production callsites remain nonempty.
- click uses the same `onChange` path; count Badge is presentational content inside the tab accessible name.

### 5.3 SegmentedControl API

```ts
export interface SegmentedItem<V extends string = string> {
  value: V
  label: string
  count?: number
}

export interface SegmentedControlProps<V extends string = string> {
  label: string
  items: readonly SegmentedItem<V>[]
  value: V
  onChange: (next: V) => void
}
```

It renders a named `role="group"` containing native buttons. Selected state is `aria-pressed`; buttons keep normal Tab order because this is a compact command/value group, not a composite tab/radio widget. Existing `.tabs--pill` / `.tab` classes may be reused during Task 6 so visual output is stable; Task 9 later owns CSS naming/cleanup.

### 5.4 Complete Migration Matrix

| Caller | Semantic target | Label / idBase |
| --- | --- | --- |
| `SettingsPage` | Tabs + active panel | `系统设置分区` / `settings-sections` |
| `SubscriptionInsights` month view | Tabs + active panel | `月成本展示` / `subscription-month-cost` |
| `TargetHistoryDrawer` | Tabs + active panel | `目标历史类型` / `target-history` |
| `MonitoringInstanceHistoryDrawer` | Tabs + active panel | `监控实例历史类型` / `monitoring-history` |
| `PortfolioWorkbench` | Tabs + active panel | `资产决策组合视图` / `asset-portfolio-workbench` |
| `SecondaryWorkbenches` queue | Tabs + active panel | `单台辅助队列视图` / `asset-decision-queue` |
| Group detail modal | Tabs + active panel | `决策组详情分区` / `asset-group-detail` |
| Template detail modal | Tabs + active panel | `场景模板详情分区` / `asset-template-detail` |
| Manual group detail modal | Tabs + active panel | `自定义组合详情分区` / `asset-manual-group-detail` |
| Record detail modal | Tabs + active panel | `决策记录详情分区` / `asset-record-detail` |
| `VPSPage` quick views | SegmentedControl | `VPS 快速视图` |
| Events time range | SegmentedControl | `事件时间范围` |
| Target time window | SegmentedControl | `目标观测时间窗口` |
| Monitoring time window | SegmentedControl | `监控实例观测时间窗口` |
| Theme preset | SegmentedControl | `主题风格` |
| Theme mode | SegmentedControl | `主题明暗` |

The exact Chinese accessible names are stable test selectors. `idBase` values are unique where multiple surfaces or nested modals can coexist.

## 6. Menu Interaction Design

### 6.1 Shared Behavioral Contract Without Shared Framework

Each menu owns:

- trigger ref;
- menu item ref array;
- open state;
- pending initial focus index;
- outside `mousedown` listener only while open.

Common behavior is asserted identically in tests. No document-level listener survives close/unmount.

### 6.2 User Menu

`Sidebar` renders the existing `UserChip` component instead of its inline clickable div/menu duplicate. UserChip markup is aligned to the current sidebar CSS and retains only the currently visible commands: 修改密码 and 退出登录.

Trigger:

- native `button type="button"`;
- `aria-label="<username> 用户菜单"`;
- `aria-haspopup="menu"`, `aria-expanded`, `aria-controls`.

Menu:

- named `role="menu"`, two `button role="menuitem"`;
- open by click/Enter/Space; ArrowDown can open at first, ArrowUp at last;
- open focuses requested item;
- ArrowDown/Up wrap, Home/End jump;
- Escape closes and synchronously restores trigger focus;
- Tab closes but does not trap focus;
- item activation closes exactly once before invoking the callback. The ChangePassword Modal remains responsible for moving focus into itself.

### 6.3 Theme Menu

The trigger gains the same menu button attributes. The four existing options become `button role="menuitemradio" aria-checked` and keep preset/mode/icon/label values.

Selection updates both context values, closes the menu and returns focus to the trigger. Arrow navigation only moves focus; it does not change theme until Enter/Space/click activates the item. This avoids changing the entire visual theme while a keyboard user is only exploring options.

## 7. Skip Link And Row Navigation

### 7.1 AppShell

Authenticated markup becomes:

```tsx
<>
  <a className="skip-link" href="#main-content">跳到主内容</a>
  <div className="layout">…<main id="main-content" tabIndex={-1}>…</main></div>
</>
```

The CSS owner for this task is the current shell/base partial. The link is positioned outside the viewport by default and appears at a high z-index on `:focus-visible`. It remains in DOM/accessibility order and does not use `display:none`, `visibility:hidden` or negative tabIndex.

### 7.2 VPS Row

The primary cell changes to a real Link with the existing detail URL and visible asset name. The row handler first ignores interactive descendants; background click can retain pointer convenience. The row itself does not gain tabIndex or simulated Link role, so keyboard users encounter one meaningful link instead of a fake row plus a link.

No API call, filter state or navigation destination changes. Tests assert both the Link href and background-click enhancement without double navigation.

## 8. AST Guard Design

Create `web/src/security/semanticInteractionContract.test.ts`. It imports the already-direct TypeScript dependency and parses every non-test production `.tsx` with `ts.createSourceFile(..., ScriptKind.TSX)`.

For intrinsic non-control tags (`div`, `span`, `tr`, `td`, `li`, `article`, `section`, `p`, `label`) containing a JSX `onClick` attribute:

1. accept native elements outside that set automatically;
2. inspect the immediately preceding JSX comment for `a11y-allow-nonsemantic-click: <reason>`;
3. accept only the finite reasons below;
4. otherwise report `relative/path:line <tag>` and fail.

Approved reasons:

- `modal-backdrop`;
- `event-propagation`;
- `keyboard-complete-row`;
- `primary-link-row-enhancement`.

The AST locates elements and attributes; a small regex only parses the comment reason. There is no regex scan for JSX and no path/line allowlist. Unit fixtures prove semantic button passes, unmarked div fails, unknown reason fails and each approved reason is recognized. Production inventory is then asserted empty after allowed entries are removed.

## 9. Test And Evidence Architecture

### RTL / Vitest

- Input/Select: native attributes, accessible description, token order/dedup, invalid precedence, ref target, options/children.
- Tabs: named tablist, exactly one tab stop, click and four keyboard keys, wrap, dynamic missing value, id/panel helpers.
- SegmentedControl: named group, native buttons, pressed state, generic values/count and click.
- Settings/history/Asset representative tests: active tab has a matching panel; compile plus targeted tests cover every migrated caller.
- UserChip/TopBar/AppShell/VPS: menu and focus flows, skip link/main contract, primary row Link.
- AST guard: synthetic source cases plus repository inventory.

Use `fireEvent` with explicit focus because the repository does not currently depend on `@testing-library/user-event`; Task 6 does not add a dependency just to rewrite event syntax. Browser evidence covers real key dispatch.

### Local Browser / Axe

- Run production preview through the current local browser evidence path with fixture auth/data.
- Verify `/`, `/settings`, `/vps`, and representative history/Asset modal flows in Chromium using Tab, Shift+Tab, Arrow keys, Home, End, Enter, Space and Escape.
- Inject an exact, recorded local axe-core build through the browser debugging context; do not add it to package.json/lockfile in this task. Scan settled AppShell, Dashboard, Settings and VPS surfaces; serious/critical must be zero.
- Capture route, viewport, browser/tool version, violation counts, focus sequence and console/CSP/network summary. This evidence is task-local and does not claim CI persistence.

## 10. Compatibility And Data Flow

- Input/Select public props remain supersets of native HTML attributes; existing 127 call sites compile without behavior-specific edits.
- Tabs intentionally introduces required props, so TypeScript forces every callsite decision. SegmentedControl preserves controlled `value -> onChange` flow and does not alter URL/state owners.
- Theme actions still call `setPreset` then `setMode`; user menu callbacks remain supplied by AppShell/Sidebar.
- VPS route stays `/vps/:id`; row pointer handler and Link converge on the same URL.
- Modal, auth, API and backend layers are read-only for this task.

## 11. Rollout And Rollback

- Commit 1: field atoms/tests. Safe to retain independently.
- Commit 2: Tabs/TabPanel/SegmentedControl plus all 16 callers. Roll back as one atomic semantic migration if a caller relationship is wrong.
- Commit 3: UserChip/Theme menu/skip link/VPS/AST guard and tests.
- Commit 4: spec and evidence updates after implementation paths are real.

If a page regression appears, revert its owning migration commit; do not weaken the atom contract, delete the AST guard, replace native controls with role simulations or add broad allowlists. Task 7 consumes the final component APIs only after Task 6 has merged and released.
