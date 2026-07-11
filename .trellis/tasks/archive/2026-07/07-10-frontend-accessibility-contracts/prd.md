# 前端可访问性交互契约

## Goal

在不改变后端业务语义、路由结构或页面视觉方向的前提下，把共享表单、真正的 Tabs、值选择器、应用菜单、skip link 和表格主导航统一到原生 HTML 与可验证的 ARIA/键盘合同，关闭 P2-01、P2-02、P2-03，并为 Task 7 的窄视口修复和 Task 10 的正式 Playwright/axe 门提供稳定 selector 与行为基线。

## User Value

- 只使用键盘的操作员可以进入主内容、切换设置与详情视图、使用用户/主题菜单，并从 VPS 列表进入详情。
- 浏览器原生校验和读屏能够识别必填、错误、提示、当前 tab、关联 panel、当前主题选择和菜单展开状态。
- 页面不再用 `div` / `tr` 模拟唯一命令入口；鼠标整行点击即使保留，也只是可见链接或按钮之外的增强。
- 后续新增非语义点击、虚假 tab 或遗漏 panel 关系会被 source contract、TypeScript 和组件测试阻断。

## Confirmed Facts

### Baseline And Dependency

- 工作基线是 `origin/main@07a9a77`；Node 合同为 `22.23.1`。
- `env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify` 已通过：Go、ESLint、86 个 Vitest 文件 / 633 tests、TypeScript 与 production build 全绿；npm audit 为 0 vulnerabilities。
- 直接前置 `frontend-modal-stack-focus` 已归档，implementation PR 为 #344；当前 Modal 栈、焦点恢复、inert、Escape 与滚动锁合同可复用，不在本任务重新实现。
- 本任务从 `codex/frontend-accessibility-contracts` 独立 worktree 执行，base 为 `main`；当前状态必须保持 `planning`，直到详细方案通过 review。

### Form Atoms

- production TSX 当前有 110 个 `<Input>` 和 17 个 `<Select>` 调用，修复 atom 可以覆盖大量表单而不逐页复制逻辑。
- `Input` 已经通过原生 attributes spread 传递 `required` 并 forward ref，但 error/hint 没有 id、`aria-describedby` 或自动 `aria-invalid`，required label 也与 Select 不一致。
- `Select` 在 props 中解构 `required`，只用它显示 required label class，却没有把它传给原生 `<select>`；当前没有 `Select.test.tsx`。
- 两个 atom 都是 `forwardRef`，调用者未来可能提供自己的 `id`、`aria-describedby` 或 `aria-invalid`，内部增强不能覆盖或重复这些 token。
- 当前 error 与 hint 是互斥展示：有 error 时不渲染 hint。本任务保持这一可见行为，不把隐藏节点写进描述关系。

### Tabs And Selection Controls

- production 当前有 16 个 `<Tabs>` 调用；atom 没有 `label` / `idBase`，所有 tab 都能被 Tab 键到达，没有 ArrowLeft/ArrowRight/Home/End，也没有 `aria-controls` / tabpanel 关系。
- 其中主题 preset/mode、事件时间范围、监控/目标时间窗口和 VPS 快速视图是值选择/过滤器，不是文档意义上的 tab panel。继续给它们 `role="tab"` 会制造虚假 tabs。
- Settings、订阅图表视图、历史抽屉和 Asset 工作台/详情确实在切换互斥内容面，继续使用 Tabs，并迁移到完整 tab/tabpanel 合同。
- Task 7 拥有 390px tab/命令裁切与 overflow 策略；本任务只保证语义、焦点和必要 focus-visible/skip-link 样式，不提前宣称窄视口布局已关闭。

### Menus, Skip Link And Semantic Commands

- 当前 TypeScript AST inventory 找到 9 个 production `div/span/tr/...` 带 `onClick`：Sidebar user chip、TopBar theme item、VPS row 是 3 个真实缺口；Modal backdrop、DataTable/Monitoring/Targets 已有键盘行为的行，以及 Targets propagation containers 是 6 个需显式说明的例外。
- Dashboard 原审查中的模拟命令已经由 Task 3 改为 Link/button，不再属于本任务实际修复面。
- Sidebar 自己复制了一套不可聚焦 user menu；仓库另有未被 production 引用的 `UserChip.tsx`。本任务应收敛为一个真实按钮/menu 实现，而不是继续维护两套不同合同。
- TopBar 主题 trigger 是 button，但缺少 `aria-haspopup` / `aria-expanded`；四个选项仍是 clickable div，没有 menuitemradio、Arrow、Escape 或 focus return。
- `AppShell` 已有 `<main id="main-content">`，但没有 skip link，main 也没有 `tabIndex={-1}`。
- VPS 名称当前只是 `<div>` 文本，整行 click 是唯一导航入口；Link 已在该页面使用，可在不改变 route 的情况下把名称变成主键盘入口。

### Verification Ownership

- 仓库当前没有正式 Playwright/axe dependency 或 `npm run test:e2e`；这些由依赖本任务的 Task 10 统一写入 lockfile 和 CI。
- Task 6 仍需留下真实浏览器键盘证据和固定版本的一次性本地 axe 结果，但不得把它描述成 Task 10 的持久化 browser gate。
- Task 7 在 Task 6 合并、发布、归档并完成 post-merge 检查后才能启动；Task 8 可在 Gate B 后启动，Task 9 必须等待 Task 8。

## Requirements

### 1. Dependency And Scope Gate

- 只修改前端共享交互合同及其直接调用方、测试、必要 CSS 和 Trellis spec；不改后端 JSON、API 请求、权限、路由 URL 或领域状态机。
- 复用 Task 2 的 Modal 行为，不复制新的全局 keydown、focus trap、body lock 或 overlay 实现。
- 不运行 `task.py start`，直到 `prd.md`、`design.md`、`implement.md` 通过 Trellis 校验并由用户 review。

### 2. Input And Select Contract

- Input/Select 必须以最终原生 control id 派生稳定的 `-hint` / `-error` id；调用者显式 id 与 `useId` fallback 都必须工作。
- `aria-describedby` 按“调用者 token → 当前可见内部描述”合并、按空白拆分并去重；不能覆盖外部说明，也不能引用未渲染的 hint。
- 有 error 时自动设置 `aria-invalid="true"`；无 error 时保留调用者自己的 `aria-invalid` 值。
- `required` 必须落到原生 input/select，并让两者使用一致的 required label class；ref 必须指向真实原生元素。
- options 与 children 两种 Select 用法、原生 onChange、className 和其余 HTML attributes 保持兼容。

### 3. Tabs And Segmented Selection Contract

- `TabsProps` 强制 `label`、`idBase`、items、value、onChange；tablist 有可访问名称，每个 tab 有稳定 id / `aria-controls` / `aria-selected`。
- selected tab 是唯一 `tabIndex=0`；ArrowLeft/ArrowRight 循环，Home/End 跳首尾，移动焦点并自动调用 `onChange`。当前 value 暂时不在动态 items 中时必须有确定 fallback，不能留下零个或多个 tab stop。
- 真正 Tabs 的 active content 使用共享 id helper 或 `TabPanel`，设置 `role="tabpanel"`、id、`aria-labelledby` 与 `tabIndex=0`；每个 idBase 在同页唯一。
- 新增语义明确的 segmented/value-selection atom，使用带 `aria-pressed` 的原生 button group；它不冒充 tab/tabpanel，并保留现有 pill 视觉与泛型 value 合同。
- 16 个现有调用必须逐一迁移：真正 panel 使用 Tabs；主题、时间范围/窗口和 VPS 快速视图使用 segmented contract。迁移后不允许无 panel 的 `role="tab"`。

### 4. User And Theme Menu Contract

- Sidebar 入口必须是 native button，带稳定 menu id、`aria-haspopup="menu"`、`aria-controls`、`aria-expanded` 和可辨识名称。
- User 与 theme menu 使用 `role="menu"`；命令使用 `menuitem` button，主题选择使用 `menuitemradio` + `aria-checked`。
- trigger 的 Enter/Space/click 打开菜单；ArrowDown/ArrowUp/Home/End 在 menu items 间移动；Escape 关闭并把焦点还给 trigger；Tab 允许离开并关闭；外部 pointer click 关闭。
- 主题选择保持现有 preset/mode、local storage、CSS class 与四个可见选项，不新增后端或全局状态。
- Sidebar 只保留一套 user menu 实现；修改密码与退出登录 callback 时序保持兼容，不能因焦点处理重复调用。

### 5. Skip Link And Navigation Semantics

- 认证后的 AppShell 第一可聚焦项是 `<a className="skip-link" href="#main-content">跳到主内容</a>`；main 使用 `id="main-content" tabIndex={-1}`。
- skip link 平时视觉隐藏、focus-visible 时进入视口，不得被 sidebar/topbar z-index 遮挡。
- VPS 名称使用真实 `<Link to="/vps/:id">`；整行 click 若保留，只处理非交互背景并作为 pointer enhancement，键盘流程只依赖 Link。
- 已有 keyboard-complete table row、backdrop 和 propagation container 不被伪装成新 button；每个保留例外都必须有机器可读的注释理由。

### 6. AST Semantic Guard

- 使用 TypeScript compiler AST 扫描全部 production TSX，不使用正则判断 JSX。
- 非语义 intrinsic element 上的 `onClick` 默认失败；允许项必须在节点附近声明受控 reason，例如 modal backdrop、event propagation、keyboard-complete composite row 或 primary-link pointer enhancement。
- allowlist reason 是有限集合；未知 reason、无注释、测试文件伪装、路径/行号静态白名单或新增真实命令均失败。
- 修复后 inventory 中 Sidebar/TopBar 不再出现，VPS 只以主 Link + 明示 pointer enhancement 形式存在。

### 7. Evidence And Delivery

- 先写失败测试，再做最小实现；atoms、调用迁移、shell/menu/guard 分为可独立回滚的提交边界。
- focused Vitest、全量 633+ tests、lint、production build、npm audit、`make verify` 与 `git diff --check` 全部通过。
- 使用本地 Chromium 对 AppShell、Settings、VPS、Dashboard 做纯键盘流程；使用固定版本但不写入仓库 lockfile的一次性 axe scan，serious/critical 为零并记录工具版本。
- implementation PR checks、main CI、Release Please、release/publish-images 和镜像证据完成后才归档 Task 6；纯 mock/RTL 不能替代发布后浏览器 evidence。

## Out Of Scope

- Task 7 的 390px command clipping、Tabs 横向 overflow、表格局部滚动和页面级 responsive 重排。
- Task 8 的 Asset controller/domain 拆分、Task 9 的 CSS owner/AST 预算、Task 10 的正式 Playwright/axe/coverage/CI ratchet。
- 新视觉语言、菜单内容重设计、路由变化、后端 schema、OpenAPI/codegen、跨浏览器矩阵或长期截图基线。
- 把所有表格重写成 ARIA grid；本任务只消除唯一鼠标入口并对既有复合行建立受控例外。

## Acceptance Criteria

- [x] Modal 前置任务已完成；Task 6 分支/worktree/Node 22.23.1 和 baseline 证据成立。
- [x] Input/Select required、ref、error/hint、外部 describedby 合并/去重与 caller invalid 保留测试全部通过。
- [x] Select 原生 control `required` 生效，新增 `Select.test.tsx`；Input/Select required label 行为一致。
- [x] 真正 Tabs 只有一个 tab stop，ArrowLeft/Right/Home/End 自动激活，tab 与 active panel ids 双向一致。
- [x] 16 个旧 Tabs 调用已全部迁移为真实 Tabs + panel 或 segmented button group；不存在无 panel 的虚假 tab。
- [x] User/theme menu 的 button/menu/menuitemradio、展开状态、Arrow/Home/End/Escape/Tab、outside click 与 focus return 有回归测试。
- [x] AppShell skip link 与可聚焦 main 合同通过；VPS 名称是详情 Link，整行不再是唯一键盘路径。
- [x] TypeScript AST guard 报告零个未解释的 non-semantic click，并能用 synthetic fixture 证明违规会失败。
- [x] Task 6 修改范围的本地 Chromium 键盘流程通过；AppShell、Settings、VPS、Dashboard axe serious/critical 为零，且证据明确标注为 Task 10 前的本地门。
- [x] 测试文件/用例数不低于 86/633，或每个替换都有等价覆盖证据；lint、test、build、audit、make verify、diff check 全绿。
- [x] PR、main CI、release、publish-images、镜像 tag/digest 与发布产物 browser smoke 已核验；parent Gate B 已补 Task 6 证据，Task 7 仍等待独立 archive PR 合并与 post-merge 检查。
