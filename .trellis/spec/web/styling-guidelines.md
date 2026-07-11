# 样式规范

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

---

## Overview

候风前端**没有引入任何 CSS 框架 / CSS-in-JS / 预处理器**——`web/package.json` `dependencies` 与 `devDependencies` 都没有 Tailwind / Emotion / styled-components / Sass / Less / Stitches / vanilla-extract。所有浏览器样式都是**纯 CSS**，借助 CSS 自定义属性（设计令牌）+ BEM 类名 + `color-mix(in srgb, ...)` 函数实现暗 / 亮 / 多预设主题。`postcss` 是仅供 AST inventory/contract 使用的 direct devDependency，不参与浏览器 CSS 转换。

**当前的样式组织**：

- `web/src/main.tsx` 固定按 reset → tokens → `index.css` owner manifest → `modernize.css` 顺序导入；tokens 必须在所有 `var(--...)` 消费方之前。
- `web/src/index.css` 只承载七个显式 owner section 与本地 `@import`，不承载规则。owner 顺序为 shared-atoms-page → app-shell → dashboard → assets → vps → observability → settings-subscriptions。
- 规则落点位于 `web/src/styles/partials/`；`web/css-owners.json` 对 `web/src/**/*.css`（包括 reset、tokens、modernize、Login route CSS）做唯一且穷尽的 owner 映射。
- 唯一一处 page 自带 CSS 的例外：`web/src/pages/LoginPage.css`，由 `LoginPage.tsx:5` 自身 import（首屏前 AppShell 还没挂上）。其余所有 page 与 component **不写 `import './foo.css'`**。

> **未来留余地**：如果团队后续决定引入 Tailwind / CSS Modules / Vanilla-Extract 之类的方案，需要做独立技术决策并整体迁移，**不要**让两套体系并存。

---

## 当前视觉指导

当前视觉指导入口：

1. `docs/design/current/interface-language.md` —— UI tone、视觉默认、状态语言、证据语言与浏览器 sanity 边界。
2. `docs/design/current/component-patterns.md` —— 当前组件默认、页面职责、测试期望与历史参考边界。
3. `docs/design/current/product-and-architecture.md` —— 产品形态、拓扑、领域模型与安全边界。

历史版本化设计目录只用于追溯背景。普通 UI work 应从当前代码、当前 task、`.trellis/spec/` 和 `docs/design/current/` 出发；如果新方向更合理，更新当前指导和测试，不要用旧版本标签阻止探索。

**禁止**：

- 不要把早期 concept 屏 / `stitch/` 子目录视觉恢复成当前实现目标——这些是历史素材。
- 不要在历史版本目录里承载新的当前决策；可复用结论应写入当前 task、`docs/design/current/` 或 `.trellis/spec/`。
- 预览、浏览器 sanity 与本地截图政策见 `docs/operations/ui-preview-and-browser-sanity.md`；不要提交 screenshot manifest 或 bulk raster screenshots。只有用户明确批准的 public README/docs 图片资产才可放入 allowlisted docs asset path。不要恢复旧视觉验证流程或一次性历史截图作为当前 workflow。

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

- 颜色 / 间距 / 字号 / 圆角 / 边框 / 阴影 / 动效**一律走 `var(--xxx)`**，**严禁组件 / 全局样式里写硬编码 hex 或像素**。运行时 SVG 几何使用 `width` / `height` / `x` / `y` / `points` 等 presentation attributes，不通过 JSX `style` 绕过令牌与 CSP。
- 状态色派生写法用 `color-mix(in srgb, var(--color-state-xxx) NN%, transparent)`，参见 `atoms.css:155-196` 的 `tone--*` 系列。**不要**自己另算 RGBA / 引入额外色板。
- 新增令牌：先在 `tokens.css` `:root` 段加默认值，再到每个 `html.theme-*` 块补对应主题值——**漏一个主题会让该主题视觉破洞**。
- 兼容别名令牌必须跨主题一致：如果引入 `--surface-0..3`、`--border-muted`、`--border-default`、`--text-tertiary` 这类 alias，必须在 `:root` 与每个 `html.theme-*` 块都定义，并让 alias 指向当前主题的基础令牌（如 `--surface-2: var(--surface-elevated)`），不要只在默认主题补别名。

### 状态前景与主按钮对比度合同

- 带文字的 `.tone--*` badge、状态文本和 `.btn.primary` 必须在三套运行时主题上达到 WCAG AA 普通文本对比度；不能因为 dark-first 就把 500 色阶原样用于 light surface。
- light 主题使用对比度安全的状态 owner tokens：normal `#047857`、notice/alert/warn `#92400e`、critical/error `#b91c1c`、maintenance/offline `#475569`。`--badge-*-c` 与 `--color-state-*` 引用这些 owner，不在组件规则重新硬编码第二套颜色。
- `.btn.primary` 使用 `background: var(--accent); color: var(--bg)`：暗色主题得到深色前景配亮蓝，亮色主题得到白色前景配深蓝。不要恢复 `#fff`，默认/经典暗色的 `#3b82f6` 对白色小字只有约 3.68:1。
- `.badge--info.tone--critical` 的 11px 文字不能直接使用 critical token；`#ef4444` 在自身 10% tint 背景上只有约 4.0:1。共享 atom 使用 `color-mix(in srgb, var(--color-state-critical) 78%, var(--text-primary))` 提升暗色前景，同时让亮色 critical 继续向深色文本收束；border/background 仍由原 critical token 驱动。
- 11.5px 的 `--text-secondary` 不能直接放在 alert/critical tint 上并假设基础 surface 对比度仍成立；例如亮色 Asset group card 的 alert 4% 背景上只有 4.46:1。该 card 的 context owner 使用 `color-mix(in srgb, var(--text-secondary) 90%, var(--text-primary))`，修改后必须在三主题 settled axe 中复验真实复合背景。
- 修改任一状态/背景 token 后，先跑相关组件/页面测试，再运行 `web/e2e/accessibility.spec.ts` 的 settled axe 与三视口 browser contracts；serious/critical 必须为 0。额外主题人工/CDP 检查可以补充，但不能替代 repository gate。

```css
/* Wrong: dark theme 可见，不代表 light theme 仍有对比度。 */
.tone--notice{color:#f59e0b}
.btn.primary{background:var(--accent);color:#fff}

/* Correct: 组件消费主题 owner token。 */
.tone--notice{color:var(--color-state-notice)}
.btn.primary{background:var(--accent);color:var(--bg)}
```

---

## 主题与暗色优先

候风是 **dark-first**，默认主题在 `tokens.css:51-89` 的 `:root` 块（即 `houfeng-dark`），CSS 在 `<html>` 没有 `theme-*` 类时就直接用它。

**主题切换实现**：

- 主题状态由 `web/src/lib/theme-context.tsx` 管理（`Preset = 'houfeng' | 'classic'`、`Mode = 'dark' | 'light' | 'system'`）。
- `web/src/lib/theme.ts` 的 `applyTheme(preset, mode)` 根据 preset + 解析后的 scheme 在 `<html>` 上加 `theme-houfeng-dark` / `theme-houfeng-light` / `theme-classic-dark` 三个 class 之一；`classic-light` 明确回退到 `theme-houfeng-light`。
- 首屏防闪烁逻辑位于同源静态资源 `web/public/theme-bootstrap.js`，由 `web/index.html` 在 React 入口前以 `<script src="/theme-bootstrap.js"></script>` 同步加载。脚本只接受已知 preset/mode allowlist，并与 `web/src/lib/theme.ts` 保持相同的 `classic-light` 回退；禁止改回 inline script 或把预热延后到 React。
- 持久化 key 由 `THEME_STORAGE_KEYS` 集中（`web/src/lib/theme.ts:5-8`）：`houfeng.theme.preset` / `houfeng.theme.mode`。

**新组件准备暗色**：

- 直接用 `var(--surface)` / `var(--text-primary)` / `var(--border)` 等令牌，**不要**写"`.foo--dark { ... }`"派生类。
- 不要写 `@media (prefers-color-scheme: dark)` —— 主题切换走显式 `theme-*` class，让用户在偏好之外可以强切。
- `@media (prefers-reduced-motion: reduce)` 已在 `tokens.css:216-223` 全局生效，组件自定义动画**不要**单独再禁。

---

## 类名约定

实读 `web/src/styles/partials/atoms.css` / `page.css` / `layout.css`，**统一使用 BEM**：`block__element--modifier`。

- **block**：组件 / 区域名小写连字符。例：`btn` / `card` / `input` / `badge` / `sparkline` / `tabs` / `sidebar` / `top-bar` / `breadcrumb` / `sync-status` / `user-chip` / `app-shell` / `page-stack` / `page-panel` / `hero-panel`.
- **element**：双下划线后跟元素名。例：`sidebar__brand`、`sidebar__brand-zh`、`page-panel__title`、`sync-status__dot`、`modal__actions`。
- **modifier**：双连字符后跟变体名。例：`btn--primary` / `btn--sm` / `card--accent` / `card--ribbon-left` / `tabs--underline` / `sync-status--ok` / `sync-status--degraded`.

**复合 modifier 实读规则**：组件叠加多 modifier 时直接空格拼接，常见模式见 `web/src/components/atoms/Card.tsx:23-33`（`['card', 'card--state', 'card--ribbon-left', 'tone--alert'].filter(Boolean).join(' ')`）。

**辅助类**：仅在 `reset.css` 提供两个全局工具类——`.tnum`（启用 `tabular-nums`）与 `.mono`（切到 `var(--font-mono)`）。**不要**自己再造工具类（如 `.mt-2`、`.flex-center`）；要排版 / 间距请进对应 owner 的 `partials/page.css` / `partials/atoms.css` 用 BEM 表达。

---

## 全局样式 vs 组件局部样式

**当前唯一允许的样式落点**：

| 文件 | 范围 | 示例规则 |
|------|------|----------|
| `web/src/styles/reset.css` | box-sizing / body 默认排版 / aurora 背景 / `.tnum` `.mono` 工具类 | `body { background: var(--bg); ... }` |
| `web/src/styles/tokens.css` | 设计令牌 + 主题覆盖 + reduce-motion | `:root { --space-1: 4px; ... }` |
| `web/src/styles/partials/atoms.css` | 设计系统原子样式（`.btn` / `.card` / `.input` / `.badge` / `.sparkline` / `.tabs` / `.data-table` / `.tone--*`） | `.btn--primary { ... }` |
| `web/src/styles/partials/page.css` | 页面级共享版式与跨域 workflow primitives（`.page-stack` / `.page-panel` / `.hero-panel` / `.section-heading`） | `.page-panel__title { ... }` |
| `web/src/styles/partials/layout.css` | AppShell 子树（Sidebar / TopBar / Breadcrumb / GlobalSearch / SyncStatus / UserChip） | `.sidebar { ... }` |
| `web/src/styles/partials/{dashboard,legacy-assets,legacy-vps,legacy-observability,legacy-subscriptions,...}.css` | `web/css-owners.json` 指定的业务 owner；新规则按真实 BEM/domain 归属进入现有 owner 文件 | `.asset-decision-*` / `.vps-*` / `.monitoring-*` |
| `web/src/styles/modernize.css` | 已有全站兼容覆盖；不是新规则的默认 catch-all | `.settings-save-footer { ... }` |
| `web/src/pages/LoginPage.css` | 唯一 page 局部样式例外（首屏前缺壳） | `.login-page__card { ... }` |

**原则**：

- 跨页复用样式 → `styles/partials/page.css`（业务级版式）或 `styles/partials/atoms.css`（原子）。
- 仅 AppShell 子树用 → `styles/partials/layout.css`；业务规则按 `css-owners.json` 进入其现有 owner 文件。
- **不要**为某个 page 单独建 `.css` 文件（LoginPage 是历史例外），也不要新建 `misc.css` / `legacy-misc.css` / “final overrides” bucket。无法归属的规则先作为删除候选。
- loading / error / empty 的共享页面状态样式统一用 `.page-state` 系列，落在 `styles/partials/page.css`。页面不要复制 `.page-panel` + 裸文本；空态如果需要当前装饰和 CTA，使用 `PageState surface="empty"` 复用 `.empty-state.page-state`。

## Scenario: CSS owner manifest 与 AST budget ratchet

### 1. Scope / Trigger

- 新增、删除、移动任何 production CSS，修改 `index.css` import 顺序、owner map、CSS budget，或尝试 route-owned CSS 时，必须使用本合同。

### 2. Signatures

- `npm --prefix web run css:analyze`：读取 `web/css-owners.json`、`web/css-budget.json` 与 fresh `web/dist/**/*.css`，输出文本摘要；owner/config/预算失败时返回非零。
- `node scripts/analyze-web-css.mjs --format json|text [--web-root PATH --owners PATH --budget PATH --dist PATH]`：供 synthetic fixture 与 `make verify-web` 的 CI gate 调用。
- `web/css-owners.json`：`version=1`，恰好包含 app-shell、dashboard、assets、vps、observability、settings-subscriptions、shared-atoms-page 七个 key。
- `web/css-budget.json`：`version=1`，固定 source files/bytes、rules、declarations、repeated selector texts、literal colors、`!important`、production raw/gzip 九个上限。

### 3. Contracts

- 每个 `web/src/**/*.css` 必须且只能出现在一个 owner；unknown path、重复 owner、漏 owner 均 fail closed。
- `index.css` 的每个 partial import 必须紧随唯一 `/* owner: <name> */` section，所有 imported partial 必须含非 comment node；manifest 与 owner map 的 partial 集合完全相等。
- production selector branch 的每个 class 必须由非测试 TS/TSX/HTML 字符串 inventory 或明确动态 modifier prefix 拥有。不可达 branch 删除；同一 rule 仍可达的 selector branch 保留。
- 同一 selector 在同一 at-rule context 只有一个定义。唯一 allowlist 是入口包与 Login 懒加载包各自需要的 root `.login-page`；media/theme context 不做机械合并。
- 预算只能随有证据的清理降低；不得为让 CI 变绿而抬高上限。production 指标必须在 fresh build 后测量。
- route CSS 只有在 owner 真正 route-private、无 FOUC、workflow/browser gate 通过且入口 raw+gzip 下降时保留；跨 VPS/Subscriptions/Providers/Archive 共享的 Assets owner 不得伪装成 Asset Decisions 单路由 CSS。

### 4. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| CSS 未配置 owner，或同一路径配置两个 owner | analyzer 返回 1，并列出路径与 exactly one owner 错误 |
| 任一九项 actual > max | analyzer 返回 1，输出 metric、actual、max |
| budget/owner JSON 缺字段、类型错误或引用未知 CSS | analyzer 返回 1，不回退默认值 |
| analyzer 的 `--dist` 不存在 | 返回非零；正式 production 分析不得使用陈旧/虚构产物 |
| Vitest 仅检查 source、且运行阶段早于 build | 为该测试传入已存在的空临时 `--dist`；不要依赖本机上次留下的 `web/dist` |
| imported partial 仅剩注释，出现 misc catch-all，或同 context 重复 selector | repository AST contract 失败 |
| 只删除 selector list 中一个失联 branch | 保留其它 branch 与原 at-rule context，reachability contract 变绿 |

### 5. Good / Base / Bad Cases

- Good：先 fresh build，再运行 analyzer；指标低于现有 budget 后把 limits 精确降至新值，并用 9 route × 3 viewport 复验。
- Base：只改一个已归属 owner 的规则，focused AST tests 通过，最终仍跑 build + analyzer。
- Bad：把新规则塞进 `misc.css`；删除整个含可达 branch 的 selector list；测试阶段读取偶然存在的旧 `dist`；无证据提高 budget。

### 6. Tests Required

- `cssAnalyzerContract.test.ts`：JSON inventory、唯一 owner、超预算 fail-closed、同 context 唯一性与 Login bundle allowlist。
- `cssReachabilityContract.test.ts`：扫描真实 production glob，并列出 file/line/selector/unowned class。
- `indexCssContract.test.ts`：递归本地 import graph、cycle/owner/non-empty partial、selector/context/declaration 唯一性与 synthetic first-match loophole。
- 提交前运行 `make verify-web`（production build → bundle/font → `css:analyze`）；涉及布局删除/重排时再跑 `npm --prefix web run test:e2e`，断言 1440/1024/390 的 document、关键命令、局部 scroll owner 与 console/network/CSP。

### 7. Wrong vs Correct

```css
/* Wrong: 新 catch-all 隐藏 owner，重复定义靠后写覆盖。 */
/* owner: shared-atoms-page */
@import './styles/partials/misc.css';
.command{display:block}
.command{display:none}

/* Correct: manifest 明确 owner；同 context 只有一个最终定义。 */
/* owner: observability */
@import './styles/partials/legacy-events.css';
.command{display:none}
```

```ts
// Wrong: Vitest 先于 build，却读取默认 web/dist，clean CI 会 ENOENT。
spawnSync(process.execPath, [analyzerPath, '--format', 'json'])

// Correct: source-only contract 显式传入测试创建的空 dist；正式 analyzer 仍 fail closed。
spawnSync(process.execPath, [analyzerPath, '--dist', emptyDist, '--format', 'json'])
```

---

## JSX 内联样式与严格 CSP

`style-src 'self'` 不允许 React 把 JSX `style` 序列化成元素内联样式；`web/src/security/cspContract.test.ts` 因此扫描所有生产 `.tsx` 并禁止任何 `style=`，包括过去认为可接受的运行时尺寸。

**替代方式**：

- 静态视觉与 spacing 使用 BEM class + `tokens.css`。
- SVG 的动态几何使用 `width` / `height` / `x` / `y` / `points` / `strokeDasharray` 等 SVG attributes；HTML tooltip 需要动态定位时可放进带动态 SVG 几何属性的 `<foreignObject>`，内部样式仍走 class。
- 动态比例优先使用原生 `<progress value={value} max={max}>`；表格列宽使用 `<col width={column.width}>`，不要在 cell/header 上生成 inline style。
- 不要用 `setAttribute('style', ...)`、运行时 `<style>` 或给元素写 CSS custom property 来伪装规避 source scan。

**窄例外**：`web/src/lib/modalStack.ts` 的 `document.body.style.overflow` 引用计数滚动锁，以及 `web/src/lib/useCopyToClipboard.ts` 的临时 textarea CSSOM 写入，是浏览器行为型 imperative API；已在真实 Chromium 严格 CSP 下验证不会触发 violation。它们不得承载业务视觉，也不构成新增 `.style.*` 或 JSX `style` 的通行证。

---

## 中文为主 + 高密度工程工具感

- UI 文案默认中文（参见 `web/index.html:2` `lang="zh-CN"`、`web/index.html:7` `<title>候风 · 服务器舰队控制面</title>`、`Sidebar.tsx`、`Breadcrumb.tsx` 等所有可见字符串）。必要英文术语原样保留（如 `OBSERVABILITY` / `houfeng-center` / `houfeng-agent`）。
- 排版按 `tokens.css:10-21` 的 type scale 走（`display` / `h1` / `h2` / `body` / `small` / `eyebrow` / `metric` / `state` / `code` / `link`），**不要**自己造 `font-size: 13.5px` 这种破阶梯。
- 字体角色固定（`tokens.css:22-27`）：标题 / 强调字段用 `--font-serif`（思源宋体回退栈）；正文 / UI 用 `--font-sans`；ID / 数字 / 代码用 `--font-mono`。`Mono` / `Hostname` / `Timestamp` 原子（`web/src/components/atoms/Mono.tsx`）已封装好，不要在 page 里自己写 `font-family: monospace`。
- 工程工具感的关键在密度：留白用 `--space-2` / `--space-3` 而非 `--space-6`；表格 / 列表行高用 `--type-body-leading` 默认 1.6（紧凑场景压到 1.4 时显式声明）。

### 窄视口命令与局部 overflow 合同

- 可操作标题必须完整可见；不能用 `max-width` + `overflow:hidden` + `text-overflow:ellipsis`，再靠 `aria-label` 或 title tooltip 补救。空间不足时让 badge/标题换行、改变 grid，或让明确 owner 局部滚动。
- `.tabs--pill` / `.tabs--underline` 均使用 `max-width:100%`、`overflow-x:auto`、`overscroll-behavior-x:contain`；tab 使用 `flex:0 0 auto` 与 `white-space:nowrap`。pill 额外使用 `width:fit-content`，桌面仍按内容收束。
- Asset Decisions 辅助入口在 `max-width:920px` 只保留一套两列 grid；item 是单列 grid、最小高度 72px，title/badge 可换行。不要在 640px 再重复同一 selector 的 display/grid/min-height。
- 宽表的 section 不拥有水平滚动；只有带 region/name/hint/focus 合同的 wrapper 使用 `overflow-x:auto`。浏览器中必须同时断言 section `scrollWidth <= clientWidth + 1`、wrapper 确实可滚，以及 document 无横向 overflow。

```css
.tabs--pill,.tabs--underline{max-width:100%;overflow-x:auto;overscroll-behavior-x:contain}
.tabs--pill .tab,.tabs--underline .tab{flex:0 0 auto;white-space:nowrap}
.provider-directory-table-scroll{max-width:100%;min-width:0;overflow-x:auto;scrollbar-gutter:stable}
```

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
{ key: 'entry', label: '服务入口', width: '232px', render: ... }
```
```css
.provider-directory-table-scroll{max-width:100%;overflow-x:auto}
.provider-directory-table{min-width:1000px;table-layout:fixed}
.provider-directory-entry-links{flex-wrap:wrap;overflow:visible}
.provider-directory-entry-link{max-width:none;overflow:visible;text-overflow:clip}
```

---

## Select 下拉箭头（caret）约定

**What**：所有 form `<select>` 必须用 `appearance:none` 关掉原生 OS 箭头，并用 `background-image:var(--select-caret)` 画候风自定义箭头。

**Why**：原生箭头在暗色主题下突兀，且颜色不随主题切换；`url(data:image/svg+xml,...)` 又会被严格 `img-src 'self'` 拒绝。箭头因此必须是同源静态 SVG，并由主题令牌选择资源。

**约定**：
- `--select-caret` 在 `web/src/styles/tokens.css` 的 `:root`、`.theme-houfeng-light`、`.theme-classic-dark` 分别指向 `/select-caret-houfeng-dark.svg`、`/select-caret-houfeng-light.svg`、`/select-caret-classic-dark.svg`；`classic-light` 使用既定的 `houfeng-light` 回退。三个 SVG 必须保存在 `web/public/` 并纳入 CSP source contract。
- 新增 form select 时**复用现有带 caret 的规则**（`select.input` / `.filter-select__control` / `.page-stack select` / `.asset-operation-field select` / `.target-create-drawer__form select` / `.filter-panel select.filter-select` 等），优先走 `Select` 原子（`web/src/components/atoms/Select.tsx`）或 `FilterSelect`，不要新造裸 select。
- 紧凑型 select：`padding-right` ≈ 24-26px、`background-position:right 8px center`；标准型：`padding-right` ≈ 30px、`right 12px center`。padding-right 必须够大,否则箭头压字。

**Wrong**：
```tsx
<select className="input">  // 若该 select 经 Drawer portal 逃逸 .page-stack 链，或裸 select 无 className，
                            // 会落到无 caret 规则 → 原生 OS 箭头
```
```css
select.input{appearance:none;background-image:url("data:image/svg+xml,...")}  /* img-src 'self' 下被阻止 */
```

**Correct**：
```tsx
import { Select } from '../components/atoms'
<Select label="服务商" required value={v} onChange={...}>{options}</Select>
```
```css
:root{ --select-caret:url('/select-caret-houfeng-dark.svg'); }
.theme-houfeng-light{ --select-caret:url('/select-caret-houfeng-light.svg'); }
.theme-classic-dark{ --select-caret:url('/select-caret-classic-dark.svg'); }
select.input{appearance:none;-webkit-appearance:none;background-image:var(--select-caret);padding-right:36px;background-position:right 14px center}
```

**Related**：`Select` 原子对标 `Input` 原子（forwardRef + `input-field` 包裹 + label/error/hint/required）。

---

## Panel 内弹层裁剪约定

**What**：`.page-panel` 默认 `overflow:hidden`，放在其中的绝对定位菜单、popover 或 `details` 下拉面板会被父容器裁剪。若业务需要在 panel 内展开弹层，必须显式处理裁剪边界。

**约定**：
- 优先把复杂弹层做成 portal modal；VPS 详情页这类快速管理入口统一使用居中 `Modal`。
- 如果保留轻量 `details.watchtower-actions-menu` / popover，所在 panel 或局部 section 必须允许外溢，例如 `.vps-detail-overview{overflow:visible}`，并设置合适 `z-index`。
- 下拉面板必须限制高度并允许内部滚动：`max-height` + `overflow-y:auto` + `overscroll-behavior:contain`。不能只把父级改成 `overflow:visible` 后让菜单无限延伸到视口外。
- 轻量 `details` / popover 展开后必须有完整关闭路径：点击菜单项关闭，点击页面其它位置也关闭；不要只依赖用户再次点击 summary。
- 浏览器验收必须点开菜单确认：桌面不被 panel 裁剪，窄屏不产生页面横向溢出，菜单内容可滚动并能点击关闭。

**Wrong**：
```css
.page-panel{overflow:hidden}
.my-actions-menu__panel{position:absolute;right:0;top:100%}
```

**Correct**：
```css
.my-panel-with-popover{overflow:visible;z-index:2}
.my-actions-menu__panel{
  z-index:60;
  max-height:45vh;
  overflow-y:auto;
  overscroll-behavior:contain;
}
```

---

## 反模式

> 这些是当前代码已经回避（或承认偿还）的写法，**新代码不要做**。

- ❌ **硬编码颜色 / 像素值**：颜色一律 `var(--color-state-*)` / `var(--accent*)` / `var(--surface*)`；间距走 `--space-N`；圆角走 `--radius-N`。
- ❌ **任何生产 JSX `style=`**：严格 CSP 下静态视觉走 BEM/令牌，动态 SVG 几何走 attributes，比例和列宽分别用 `<progress>` / `<col width>`。
- ❌ **回归早期 concept 屏 / `stitch/` 子目录视觉**：当前 UI 指导在 `docs/design/current/`；历史素材不能直接成为实现目标。
- ❌ **`@media (prefers-color-scheme: dark)`**：主题切换走 `theme-*` class，不监听系统偏好分支（用户可在 system / dark / light 三档显式选）。
- ❌ **新建 `.css` 文件给单个组件 / page 用**：LoginPage 是历史例外；新增样式落真实 owner 的 `styles/partials/page.css` / `atoms.css` / 业务文件，靠 BEM 隔离。
- ❌ **CSS-in-JS / Tailwind / styled-components**：当前不用；要引入需独立技术决策与整体迁移。
- ❌ **类名简写 / 工具类滥用**（`mt-2`、`flex`、`text-red`）：`reset.css` 仅留 `.tnum` `.mono` 两个工具类，其余走 BEM 表达语义。
- ❌ **令牌只改一份主题**：`tokens.css` 改了 `:root` 的主题令牌，必须同步检查 `.theme-houfeng-light` / `.theme-classic-dark`；`classic-light` 明确复用 `houfeng-light`，不要私自新增第四套漂移值。
- ❌ **在组件文件 `import './x.css'`**（除 `LoginPage.tsx` 这个历史例外）：全局样式由 `main.tsx` 固定入口与 `index.css` owner manifest 集中管理。
- ❌ **DataTable 可排序表头双 padding**：`.data-table__th--sortable` 自身必须清零 padding，实际间距由 `.data-table__sort-btn` 承担；若密度规则用 `.data-table--compact .data-table__head th` 这类更高特异性选择器，清零规则也必须带上同等上下文（如 `th.data-table__th--sortable`），否则 sortable 表头会比普通表头更宽。

---

## 与 CLAUDE.md / 设计基线的差异 / 已知 gap

> 用于后续任务评审；若形成可复用规则，更新 `.trellis/spec/` 或当前 active docs。

1. **`web/src/pages/LoginPage.css` 是 page 局部 CSS 唯一例外**，与"组件文件不 import css"的规则冲突。当前合理（首屏前 AppShell 未挂），不打算回头消除。
2. **`atoms.css` 内某些渐变 / 阴影直接用 `rgba(255,255,255,0.x)`**（如 `atoms.css:149` `background: rgba(255, 255, 255, 0.08);`），未走令牌——这是为高光 / 镜面层效果保留的允许例外，写新原子时如果需要类似效果可参考。

---

## Examples

仓库内"样式写法符合基线"的真实参考点：

- **设计系统原子（BEM + 令牌 + 复合 modifier）**：`web/src/components/atoms/Button.tsx` 的 `['btn', 'btn--' + variant, 'btn--' + size, className].filter(Boolean).join(' ')`，配 `web/src/styles/partials/atoms.css` 的 `.btn` / `.btn--primary` / `.btn--sm` 等。
- **状态色派生（`color-mix` + 状态令牌）**：`web/src/styles/partials/atoms.css` 的 `.tone--normal` / `.tone--alert` / `.tone--critical` 系列。
- **多主题令牌覆盖**：`web/src/styles/tokens.css` 的 `:root` / `.theme-houfeng-light` / `.theme-classic-dark` 为三套运行时主题赋值，`classic-light` 由运行时回退到 `houfeng-light`，组件代码无感知。
- **严格 CSP 下的首屏主题预热**：`web/index.html` 同步加载同源 `web/public/theme-bootstrap.js`，其 allowlist 与 `web/src/lib/theme.ts` 的 `applyTheme` 保持一致。
- **chart 调色板使用**：`web/src/components/atoms/Sparkline.tsx:23-33` 的 `TONE_VAR` 把状态色 / accent 映射到 `var(--color-state-*)` / `var(--accent*)`，没有任何 hex。
