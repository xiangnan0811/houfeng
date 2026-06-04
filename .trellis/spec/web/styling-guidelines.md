# 样式规范

> **权威来源**：`CLAUDE.md` + 业务/结构以 `docs/design/v1-baseline/` frozen 子集（architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow）为准，视觉以 `docs/design/v2-houfeng/`（design-language / component-spec）为准。冲突时以前述顺序为准。本项目处于初始开发阶段，规则会随代码演进调整。

---

## Overview

候风前端**没有引入任何 CSS 框架 / CSS-in-JS / 预处理器**——`web/package.json` `dependencies` 与 `devDependencies` 都没有 Tailwind / Emotion / styled-components / Sass / Less / Stitches / vanilla-extract。所有样式都是**纯 CSS**，借助 CSS 自定义属性（设计令牌）+ BEM 类名 + `color-mix(in srgb, ...)` 函数实现暗 / 亮 / 多预设主题。

**当前的样式组织**：

- 全局样式集中在 `web/src/styles/`：`reset.css` / `tokens.css` / `atoms.css` / `pages.css`，由 `web/src/main.tsx:5-9` 顺序导入；导入顺序固定为 reset → tokens → atoms → pages，**不要打乱**（tokens 必须在所有引用 `var(--...)` 的样式之前）。
- 应用壳样式落 `web/src/app/layout/layout.css`，由 `main.tsx:9` 单独导入。
- 唯一一处 page 自带 CSS 的例外：`web/src/pages/LoginPage.css`，由 `LoginPage.tsx:5` 自身 import（首屏前 AppShell 还没挂上）。其余所有 page 与 component **不写 `import './foo.css'`**。

> **未来留余地**：如果团队后续决定引入 Tailwind / CSS Modules / Vanilla-Extract 之类的方案，需要做独立技术决策并整体迁移，**不要**让两套体系并存。

---

## 视觉权威

视觉权威**只有两份 active 文档**：

1. `docs/design/v2-houfeng/design-language.md` —— v2 候风设计语言、主题、密度、状态色、排版与反模式。
2. `docs/design/v2-houfeng/component-spec.md` —— 原语、atoms、共享组件、页面壳和关键页面的视觉契约。

早期 v1 视觉规格、Stitch 素材和 v1.x frontend redesign 过程材料不再保留在 tracked docs 中；如需追溯使用 git history。业务结构仍以 v1-baseline frozen 子集为准，但视觉实现不再回归 v1/stitch。

**禁止**：

- 不要回归早期 concept 屏 / `stitch/` 子目录视觉——这些是历史素材，不是 active guidance。
- 不要修改 `docs/design/v1-baseline/` frozen 业务结构文档来承载视觉变更；视觉确实需要变更时，在当前任务 PRD、active docs 或 `.trellis/spec/` 中记录可复用结论，再更新 v2 文档 / 代码。
- 当前 v2 预览、浏览器 sanity 与本地截图政策见 `docs/operations/v2-visual-evidence.md`；不要提交 screenshot manifest 或 bulk raster screenshots。只有用户明确批准的 public README/docs 图片资产才可放入 allowlisted docs asset path。不要恢复旧视觉验证流程或一次性历史截图作为 active workflow。

---

## 设计令牌

所有令牌**集中在** `web/src/styles/tokens.css`：

- **类型 / 字体**：`--type-display-size` / `--type-h1-size` / `--type-body-size` / `--type-eyebrow-tracking` / `--font-serif` / `--font-sans` / `--font-mono` / `--font-numeric`（见 `tokens.css:10-27`）。
- **间距**（8pt 阶梯）：`--space-1`(4) / `--space-2`(8) / `--space-3`(12) / `--space-4`(16) / `--space-5`(20) / `--space-6`(24) / `--space-8`(32) / `--space-12`(48)（`tokens.css:29-31`）。
- **圆角**：`--radius-0` / `--radius-1`(4) / `--radius-2`(7) / `--radius-3`(12) / `--radius-pill`（`tokens.css:33-34`）。
- **边框 / 阴影 / 动效**：`--border-w` / `--border-w-strong` / `--shadow-glow` / `--shadow-soft` / `--shadow-overlay` / `--ease-calm` / `--dur-micro` / `--dur-state` / `--dur-page`（`tokens.css:36-49`）。
- **表面 / 文本色**（按主题切换）：`--bg` / `--bg-sidebar` / `--surface` / `--surface-elevated` / `--surface-pressed` / `--border` / `--border-strong` / `--border-dashed` / `--text-primary` / `--text-secondary` / `--text-muted` / `--text-disabled`。
- **强调色**：`--accent` / `--accent-strong` / `--accent-soft` / `--accent-border` / `--accent-2`。
- **状态色**（**跨主题语义稳定**）：`--color-state-normal` / `--color-state-notice` / `--color-state-alert` / `--color-state-critical` / `--color-state-maintenance` / `--color-state-offline`。
- **图表调色板**：`--chart-1` … `--chart-6`，sparkline / 趋势图按色序使用（参见 `web/src/components/atoms/Sparkline.tsx:23-33` 的 `TONE_VAR` 映射）。

**使用规则**：

- 颜色 / 间距 / 字号 / 圆角 / 边框 / 阴影 / 动效**一律走 `var(--xxx)`**，**严禁组件 / 全局样式里写硬编码 hex 或像素**（除非是 SVG 内部计算尺寸，参考 `Sparkline.tsx:71` 的 `style={{ width, height }}`）。
- 状态色派生写法用 `color-mix(in srgb, var(--color-state-xxx) NN%, transparent)`，参见 `atoms.css:155-196` 的 `tone--*` 系列。**不要**自己另算 RGBA / 引入额外色板。
- 新增令牌：先在 `tokens.css` `:root` 段加默认值，再到每个 `html.theme-*` 块补对应主题值——**漏一个主题会让该主题视觉破洞**。
- 兼容别名令牌必须跨主题一致：如果引入 `--surface-0..3`、`--border-muted`、`--border-default`、`--text-tertiary` 这类 alias，必须在 `:root` 与每个 `html.theme-*` 块都定义，并让 alias 指向当前主题的基础令牌（如 `--surface-2: var(--surface-elevated)`），不要只在默认主题补别名。

---

## 主题与暗色优先

候风是 **dark-first**，默认主题在 `tokens.css:51-89` 的 `:root` 块（即 `houfeng-dark`），CSS 在 `<html>` 没有 `theme-*` 类时就直接用它。

**主题切换实现**：

- 主题状态由 `web/src/lib/theme-context.tsx` 管理（`Preset = 'houfeng' | 'classic'`、`Mode = 'dark' | 'light' | 'system'`）。
- `web/src/lib/theme.ts:36-44` 的 `applyTheme(preset, mode)` 根据 preset + 解析后的 scheme 在 `<html>` 上加 `theme-houfeng-dark` / `theme-houfeng-light` / `theme-classic-dark` / `theme-classic-light` 四个 class 之一，CSS 由对应的 `html.theme-xxx { ... }` 块覆盖令牌值（`tokens.css:91-213`）。
- 首屏防闪烁脚本在 `web/index.html:8-19`：内联 JS 在 React 挂载前就读 localStorage 加上正确的 `theme-*` class。**不要**把这段逻辑搬到 React 内执行。
- 持久化 key 由 `THEME_STORAGE_KEYS` 集中（`web/src/lib/theme.ts:5-8`）：`houfeng.theme.preset` / `houfeng.theme.mode`。

**新组件准备暗色**：

- 直接用 `var(--surface)` / `var(--text-primary)` / `var(--border)` 等令牌，**不要**写"`.foo--dark { ... }`"派生类。
- 不要写 `@media (prefers-color-scheme: dark)` —— 主题切换走显式 `theme-*` class，让用户在偏好之外可以强切。
- `@media (prefers-reduced-motion: reduce)` 已在 `tokens.css:216-223` 全局生效，组件自定义动画**不要**单独再禁。

---

## 类名约定

实读 `web/src/styles/atoms.css` / `pages.css` / `app/layout/layout.css`，**统一使用 BEM**：`block__element--modifier`。

- **block**：组件 / 区域名小写连字符。例：`btn` / `card` / `input` / `badge` / `sparkline` / `tabs` / `sidebar` / `top-bar` / `breadcrumb` / `sync-status` / `user-chip` / `app-shell` / `page-stack` / `page-panel` / `hero-panel`.
- **element**：双下划线后跟元素名。例：`sidebar__brand`、`sidebar__brand-zh`、`page-panel__title`、`sync-status__dot`、`modal__actions`。
- **modifier**：双连字符后跟变体名。例：`btn--primary` / `btn--sm` / `card--accent` / `card--ribbon-left` / `tabs--underline` / `sync-status--ok` / `sync-status--degraded`.

**复合 modifier 实读规则**：组件叠加多 modifier 时直接空格拼接，常见模式见 `web/src/components/atoms/Card.tsx:23-33`（`['card', 'card--state', 'card--ribbon-left', 'tone--alert'].filter(Boolean).join(' ')`）。

**辅助类**：仅在 `reset.css` 提供两个全局工具类——`.tnum`（启用 `tabular-nums`）与 `.mono`（切到 `var(--font-mono)`）。**不要**自己再造工具类（如 `.mt-2`、`.flex-center`）；要排版 / 间距请进 `pages.css` / `atoms.css` 用 BEM 表达。

---

## 全局样式 vs 组件局部样式

**当前唯一允许的样式落点**：

| 文件 | 范围 | 示例规则 |
|------|------|----------|
| `web/src/styles/reset.css` | box-sizing / body 默认排版 / aurora 背景 / `.tnum` `.mono` 工具类 | `body { background: var(--bg); ... }` |
| `web/src/styles/tokens.css` | 设计令牌 + 主题覆盖 + reduce-motion | `:root { --space-1: 4px; ... }` |
| `web/src/styles/atoms.css` | 设计系统原子样式（`.btn` / `.card` / `.input` / `.badge` / `.sparkline` / `.tabs` / `.toggle` / `.data-table` / `.tone--*`） | `.btn--primary { ... }` |
| `web/src/styles/pages.css` | 页面级共享版式（`.page-stack` / `.page-panel` / `.hero-panel` / `.section-heading` / 表格 / 列表卡片等所有 page 通用块） | `.page-panel__title { ... }` |
| `web/src/app/layout/layout.css` | AppShell 子树（Sidebar / TopBar / Breadcrumb / GlobalSearch / SyncStatus / UserChip / Modal） | `.sidebar { ... }` |
| `web/src/pages/LoginPage.css` | 唯一 page 局部样式例外（首屏前缺壳） | `.login-page__card { ... }` |

**原则**：

- 跨页复用样式 → `styles/pages.css`（业务级版式）或 `styles/atoms.css`（原子）。
- 仅 AppShell 子树用 → `app/layout/layout.css`。
- **不要**为某个 page 单独建 `.css` 文件（LoginPage 是历史例外）。要做局部 → 用 BEM 命名 + 写进 `pages.css`，靠 page 根容器的 block class 隔离。
- loading / error / empty 的共享页面状态样式统一用 `.page-state` 系列，落在 `styles/pages.css`。页面不要复制 `.page-panel` + 裸文本；空态如果需要 v2 装饰和 CTA，使用 `PageState surface="empty"` 复用 `.empty-state.page-state`。

---

## 内联样式（`style={{ ... }}`）

**仅限尺寸 / 计算量**。允许的真实场景：

- SVG / Canvas 的 `width` / `height` 等运行时计算尺寸：`web/src/components/atoms/Sparkline.tsx:49`、`StatusGlyph.tsx:65`。
- 已知历史偿还点（**不要复制**）：`web/src/pages/SettingsPage.tsx:853` / `:862` 的 `style={{ marginBottom: 8 }}` —— 应该走类名表达。

**严禁**：

- ❌ 在 `style={{ ... }}` 里写颜色 / 背景 / 边框 / 阴影 / 字体（这些必须走令牌 + BEM 类）。
- ❌ 在 `style={{ ... }}` 里写硬编码间距（除非是 SVG / 像素级精算）；间距走 `var(--space-N)` 或 BEM modifier。

---

## 中文为主 + 高密度工程工具感

- UI 文案默认中文（参见 `web/index.html:2` `lang="zh-CN"`、`web/index.html:7` `<title>候风 · 服务器舰队控制面</title>`、`Sidebar.tsx`、`Breadcrumb.tsx` 等所有可见字符串）。必要英文术语原样保留（如 `OBSERVABILITY` / `houfeng-center` / `houfeng-agent`）。
- 排版按 `tokens.css:10-21` 的 type scale 走（`display` / `h1` / `h2` / `body` / `small` / `eyebrow` / `metric` / `state` / `code` / `link`），**不要**自己造 `font-size: 13.5px` 这种破阶梯。
- 字体角色固定（`tokens.css:22-27`）：标题 / 强调字段用 `--font-serif`（思源宋体回退栈）；正文 / UI 用 `--font-sans`；ID / 数字 / 代码用 `--font-mono`。`Mono` / `Hostname` / `Timestamp` 原子（`web/src/components/atoms/Mono.tsx`）已封装好，不要在 page 里自己写 `font-family: monospace`。
- 工程工具感的关键在密度：留白用 `--space-2` / `--space-3` 而非 `--space-6`；表格 / 列表行高用 `--type-body-leading` 默认 1.6（紧凑场景压到 1.4 时显式声明）。

### 高密度 DataTable 列宽合同

服务商、订阅、资产等事实目录页使用 `DataTable` 时，短状态列和操作列必须有明确宽度与 `white-space: nowrap` 保护；身份 / 名称列不能用大比例宽度挤压右侧列。上线前浏览器核查要同时看桌面与窄屏：桌面不应出现“大量空白 + 短中文状态换行”的组合，窄屏只允许表格容器内部横向滚动，不允许页面整体横向溢出。

**Wrong**：
```tsx
{ key: 'identity', label: '服务商', width: '30%', render: ... }
{ key: 'entry', label: '服务入口', render: () => <span>缺面板入口</span> }
```

**Correct**：
```tsx
{ key: 'identity', label: '服务商', width: '196px', render: ... }
{ key: 'entry', label: '服务入口', width: '136px', render: ... }
```
```css
.provider-directory-table{min-width:1104px;table-layout:fixed}
.provider-directory-entry .badge{white-space:nowrap}
.provider-directory-actions{flex-wrap:nowrap}
```

---

## Select 下拉箭头（caret）约定

**What**：所有 form `<select>` 必须用 `appearance:none` 关掉原生 OS 箭头，并用 `background-image:var(--select-caret)` 画候风自定义箭头。

**Why**：原生箭头在暗色主题下突兀，且颜色不随主题切换。`background-image:url(data:svg)` 无法引用 CSS 变量，所以箭头颜色不能写死（曾硬编码 `#7B7F88`，三主题皆不匹配）——改为每个主题块各定义一个 `--select-caret`（stroke = 该主题 `--text-muted`）。

**约定**：
- `--select-caret` 在 `web/src/index.css` 三个主题块各定义一次：`:root, .theme-houfeng-dark`（stroke `%2394a3b8`）/ `.theme-houfeng-light`（`%23475569`）/ `.theme-classic-dark`（`%23a1a1aa`）。运行时只有这 3 个主题（`theme.ts` 的 `applyTheme` 把 `classic-light` 回退到 `houfeng-light`），漏一个主题会让该主题 select 只有 `appearance:none` 而无箭头（空白指示符）。
- 新增 form select 时**复用现有带 caret 的规则**（`select.input` / `.filter-select__control` / `.page-stack select` / `.asset-operation-field select` / `.target-create-drawer__form select` / `.filter-panel select.filter-select` 等），优先走 `Select` 原子（`web/src/components/atoms/Select.tsx`）或 `FilterSelect`，不要新造裸 select。
- 紧凑型 select：`padding-right` ≈ 24-26px、`background-position:right 8px center`；标准型：`padding-right` ≈ 30px、`right 12px center`。padding-right 必须够大,否则箭头压字。

**Wrong**：
```tsx
<select className="input">  // 若该 select 经 Drawer portal 逃逸 .page-stack 链，或裸 select 无 className，
                            // 会落到无 caret 规则 → 原生 OS 箭头
```
```css
select.input{appearance:none;background-image:url("...stroke='%237B7F88'...")}  /* 硬编码色,不随主题 */
```

**Correct**：
```tsx
import { Select } from '../components/atoms'
<Select label="服务商" required value={v} onChange={...}>{options}</Select>
```
```css
:root, .theme-houfeng-dark{ --select-caret:url("...stroke='%2394a3b8'..."); }
.theme-houfeng-light{ --select-caret:url("...stroke='%23475569'..."); }
.theme-classic-dark{ --select-caret:url("...stroke='%23a1a1aa'..."); }
select.input{appearance:none;-webkit-appearance:none;background-image:var(--select-caret);padding-right:36px;background-position:right 14px center}
```

**Related**：`Select` 原子对标 `Input` 原子（forwardRef + `input-field` 包裹 + label/error/hint/required）。

---

## 反模式

> 这些是当前代码已经回避（或承认偿还）的写法，**新代码不要做**。

- ❌ **硬编码颜色 / 像素值**：颜色一律 `var(--color-state-*)` / `var(--accent*)` / `var(--surface*)`；间距走 `--space-N`；圆角走 `--radius-N`。
- ❌ **`style={{ color/background/border/font: ... }}` 写业务样式**：内联只用于运行时计算尺寸（Sparkline / StatusGlyph）。
- ❌ **回归早期 concept 屏 / `stitch/` 子目录视觉**：视觉权威只有 `docs/design/v2-houfeng/design-language.md` + `docs/design/v2-houfeng/component-spec.md`。
- ❌ **`@media (prefers-color-scheme: dark)`**：主题切换走 `theme-*` class，不监听系统偏好分支（用户可在 system / dark / light 三档显式选）。
- ❌ **新建 `.css` 文件给单个组件 / page 用**：LoginPage 是历史例外；新增样式落 `styles/pages.css` 或 `styles/atoms.css`，靠 BEM 隔离。
- ❌ **CSS-in-JS / Tailwind / styled-components**：当前不用；要引入需独立技术决策与整体迁移。
- ❌ **类名简写 / 工具类滥用**（`mt-2`、`flex`、`text-red`）：`reset.css` 仅留 `.tnum` `.mono` 两个工具类，其余走 BEM 表达语义。
- ❌ **令牌只改一份主题**：`tokens.css` 改了 `:root` 的 `--surface`，必须同步 `theme-houfeng-light` / `theme-classic-dark` / `theme-classic-light` 三个块。
- ❌ **在组件文件 `import './x.css'`**（除 `LoginPage.tsx` 这个历史例外）：全局样式由 `main.tsx` 顶部 + `app/layout/layout.css` 集中管理。
- ❌ **DataTable 可排序表头双 padding**：`.data-table__th--sortable` 自身必须清零 padding，实际间距由 `.data-table__sort-btn` 承担；若密度规则用 `.data-table--compact .data-table__head th` 这类更高特异性选择器，清零规则也必须带上同等上下文（如 `th.data-table__th--sortable`），否则 sortable 表头会比普通表头更宽。

---

## 与 CLAUDE.md / 设计基线的差异 / 已知 gap

> 用于后续任务评审；若形成可复用规则，更新 `.trellis/spec/` 或当前 active docs。

1. **SettingsPage 仍有少量 inline spacing/layout style**，绕过了令牌 + BEM 的规则。属已知小额偿还，新代码不要复制。
2. **`web/src/pages/LoginPage.css` 是 page 局部 CSS 唯一例外**，与"组件文件不 import css"的规则冲突。当前合理（首屏前 AppShell 未挂），不打算回头消除。
3. **`atoms.css` 内某些渐变 / 阴影直接用 `rgba(255,255,255,0.x)`**（如 `atoms.css:149` `background: rgba(255, 255, 255, 0.08);`），未走令牌——这是为高光 / 镜面层效果保留的允许例外，写新原子时如果需要类似效果可参考。

---

## Examples

仓库内"样式写法符合基线"的真实参考点：

- **设计系统原子（BEM + 令牌 + 复合 modifier）**：`web/src/components/atoms/Button.tsx:18` 的 `['btn', 'btn--' + variant, 'btn--' + size, className].filter(Boolean).join(' ')`，配 `web/src/styles/atoms.css:7-66` 的 `.btn` / `.btn--primary` / `.btn--sm` 等。
- **状态色派生（`color-mix` + 状态令牌）**：`web/src/styles/atoms.css:155-196` 的 `.tone--normal` / `.tone--alert` / `.tone--critical` 系列。
- **多主题令牌覆盖**：`web/src/styles/tokens.css:91-213` 同一组 `--surface` / `--accent` / `--color-state-*` 在 `theme-houfeng-light` / `theme-classic-dark` / `theme-classic-light` 三个块各自重新赋值，组件代码无感知。
- **首屏防闪烁主题预热**：`web/index.html:8-19` 内联脚本 + `web/src/lib/theme.ts:36-44` 的 `applyTheme` 配合的 SSR-less 暗色优先方案。
- **chart 调色板使用**：`web/src/components/atoms/Sparkline.tsx:23-33` 的 `TONE_VAR` 把状态色 / accent 映射到 `var(--color-state-*)` / `var(--accent*)`，没有任何 hex。
