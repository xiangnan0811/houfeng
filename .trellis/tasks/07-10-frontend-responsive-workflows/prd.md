# 前端窄视口核心流程

## Goal

在不改变业务请求、路由、信息架构或桌面视觉方向的前提下，关闭 P2-04：让 390px 及窄容器中的关键 Tabs、Asset Decisions 辅助入口和 Provider 决策入口完整可见、可读、可操作；把必要的横向 overflow 限制在有名称、可聚焦、可用键盘滚动的局部区域。

## User Value

- 手机或窄分屏中的操作员可以读到完整的“监控策略”“场景与组合”“组合决策”，不需要猜测被省略的命令。
- Settings 的标签保持单行，超出可用宽度时只在 tablist 内横向滚动；焦点移动会把目标 tab 带入视口。
- Provider 表格的 heading、计数和筛选工具保持固定，只有宽表本体横向滚动；键盘用户能聚焦该区域并理解滚动用途。
- Dashboard 的“今日第一步”继续位于 390x900 首屏；九条核心路由不新增 document 级横向溢出、遮挡或浏览器错误。

## Confirmed Facts

### Dependency And Baseline

- 当前基线是 `origin/main@dfe11a8`；Node 合同为 `22.23.1`，Task 6 已归档，父任务进度为 6/10。
- `frontend-dashboard-trust` 已由 Task 3 关闭四卡阻塞；`frontend-accessibility-contracts` 已由 Task 6 交付 Tabs roving focus、TabPanel、菜单、skip link 与 semantic AST contract。
- `v0.58.1` 发布产物与 merge `f8fdb30` 已通过 90 个 Vitest files / 669 tests、main CI、双架构镜像和 Task 6 browser/axe 门；Task 7 从其后的 docs-only archive merge `dfe11a8` 开始，不包含未合并业务代码。

### 390px Browser RED Evidence

- 使用 Chromium `150.0.7871.114`、发布镜像 `/app/web/dist`、仓库 `asset-workflows + observability-support` fixture 与 `390x900` 实测，九条核心路由均无 document overflow，console/runtime/CSP/HTTP/network 计数为 0。
- Settings 的“监控策略”按钮为 `78×49px`、`white-space: normal`；同组 tab 被内容高度拉成多行，而合同要求单行局部横向滚动。
- Asset Decisions 的“场景与组合”可见标题 `clientWidth=26`、`scrollWidth=58`，且 `overflow:hidden; text-overflow:ellipsis; white-space:nowrap`，真实发生主动裁切。
- Provider 的“组合决策”链接 `clientWidth=46`、`scrollWidth=52`，同样由 `max-width:48px + overflow:hidden + ellipsis` 主动裁切。
- Provider 整个 `.provider-directory-panel` 承担横向滚动：`clientWidth=298`、`scrollWidth=986`、`overflow-x:auto`，但无 role、accessible name 或键盘入口；heading 与 toolbar 也位于同一滚动容器。
- Dashboard `.dashboard-primary-action` 在 390x900 的 `top≈272`、`bottom≈372`，已经位于首屏；本任务只做回归保护，不重新设计 Dashboard。

### Current Ownership

- 通用 Tabs 规则位于 `web/src/styles/partials/atoms.css`；Asset support strip 位于 `legacy-assets.css`，响应式覆盖散在 `legacy-misc.css` 的 920px 与两组 640px media rule。
- Provider 目录规则位于 `legacy-provider.css`；`ProvidersPage.tsx` 目前把 `page-panel--scroll-x` 加在整个 section 上，未给 DataTable 单独 wrapper。
- `index.css` 明确要求新全局规则进入 `modernize.css`，但本任务优先修既有 owner 并删除矛盾 selector；Task 9 才负责 owner map、PostCSS AST 和大规模 CSS 减债。

## Requirements

### 1. Dependency And Scope Gate

- 只修改响应式交互合同、直接 owner CSS、Provider wrapper/测试、Task 7/parent 证据与必要 spec；不改 API、wire types、请求 query、权限、路由 URL 或领域状态。
- 不回退 Task 6 的 Tabs/Segmented/Menu/semantic contract，不复制新的键盘状态机或引入第三方 UI/CSS 依赖。
- 不进行 Task 8 的 Asset controller 拆分或 Task 9 的 CSS 文件搬迁、owner map、AST budget；只删除本次命中的冲突规则。

### 2. Tabs Overflow Contract

- `tabs--pill` 与 `tabs--underline` 都保持单行；tab 为 `flex: 0 0 auto`、`white-space: nowrap`，容器超宽时自身 `overflow-x:auto`，不得把 document 撑宽。
- pill 容器桌面宽度仍按内容收束，不扩成整行大底板；窄容器时 `max-width:100%`，保留当前颜色、圆角和 selected state。
- ArrowLeft/Right/Home/End 继续由 Task 6 的 `Tabs` 实现自动激活；浏览器 focus 必须把目标 tab 滚入 tablist 可视范围。
- 不隐藏 tab label、不用 aria-label 替代可见文本、不减少现有 10 Tabs / 6 SegmentedControl 语义合同。

### 3. Asset Decision Command Contract

- “保存记录”“场景与组合”“续费窗口”“单台队列”四个可见标题不得使用 ellipsis、clip 或 `overflow:hidden`。
- 920px 以下统一为一套两列策略；每个 button 自身使用明确 grid 布局，title 与 badge 可以分行，badge 可以换行，目标高度不得低于 40px。
- 删除 920px/640px 对同一 support-strip selector 的矛盾 display/grid/min-height 覆盖；不能靠新增更晚 override 掩盖旧规则。
- 现有 button、`aria-pressed`、tone、meta 数量、打开 workbench 的状态与 URL 行为保持不变。

### 4. Provider Link And Local Table Scroll Contract

- 官网、面板、VPS、订阅、组合决策的可见文本均完整；entry links 不使用统一 `max-width:48px`、hidden 或 ellipsis，长“组合决策”有明确 modifier。
- `DataTable` 外新增 Provider-owned scroll wrapper，使用 `role="region"`、heading 关联的 accessible name、可见滚动提示和 `tabIndex=0`；焦点环清晰，ArrowLeft/Right 能改变 `scrollLeft`。
- 移除 section 的 `page-panel--scroll-x`，让 heading、条数、search 和 quick views 保持固定；只允许 table wrapper 横向滚动。
- Provider table 可增加最小宽度与 entry column 宽度以容纳完整命令，但桌面 1440px 不应出现无意义的页面级滚动。

### 5. Browser And Regression Evidence

- 本地 Chromium 覆盖 `/`、`/settings`、`/vps`、`/asset-decisions`、`/providers`、`/subscriptions`、`/monitoring`、`/targets`、`/events`，视口为 `1440x1000`、`1024x768`、`390x900`。
- 每个组合断言无 document overflow、空白 surface、console/runtime/CSP/HTTP/network error；关键命令无文本裁切，fixed/sticky surface 不遮挡末尾命令。
- Settings、Asset Decisions、Providers 运行 settled local axe，serious/critical 为 0；该证据明确标为 Task 10 前的 local-only gate。
- Task 7 不新增 Playwright/axe package、lockfile、`test:e2e` 或截图 manifest。正式 Playwright/axe、coverage 与 CI browser gate 仍由 Task 10 统一实现。

## Out Of Scope

- 新的视觉语言、移动端导航重设计、所有表格统一抽象、跨浏览器矩阵或长期截图基线。
- 解决历史上每个 `page-panel--scroll-x`；本任务修复审查中确认的 Provider 目录，同时验证九条核心路由不发生 document 级回归。后续 owner/通用 wrapper 抽取必须有独立证据。
- Task 8 Asset controller/domain 拆分、Task 9 CSS AST/owner 减债、Task 10 Playwright/axe/coverage/staging gate。

## Acceptance Criteria

- [ ] 390x900 下“监控策略”保持单行且 tablist 局部可滚动；Arrow/Home/End 后 focus target 进入可视范围。
- [ ] 390x900 下“场景与组合”标题 `scrollWidth <= clientWidth`，无 hidden/ellipsis；四个辅助按钮均完整可操作且高度不少于 40px。
- [ ] Provider “组合决策”可见文本完整；table scroll region 有 heading-derived name、可见提示、`tabIndex=0`，键盘 ArrowRight 能滚动。
- [ ] Provider heading/toolbar 不随 table 横向滚动；Settings、Asset Decisions、Providers 与 Dashboard 无 document overflow。
- [ ] Dashboard 主行动继续位于 390x900 首屏；九条核心路由在三视口无 shell/末尾命令遮挡和浏览器错误。
- [ ] Settings、Asset Decisions、Providers axe serious/critical 为 0；90/669 测试基线只允许增加或有证据地替换。
- [ ] lint、全量 Vitest、production build、audit、`make verify`、Trellis validate 与 `git diff --check` 全绿，且 package/lockfile 无变化。
