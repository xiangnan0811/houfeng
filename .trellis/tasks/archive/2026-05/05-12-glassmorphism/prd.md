# 修复前端暂存区 Glassmorphism 改动的样式缺失与工程问题

## 背景
外部专家对候风前端进行了"美化"，改动全部位于 git 暂存区。经审查发现，改动在交互架构（Tabs 分组、通知渠道管理、属性列表统一）上有价值，但在视觉执行上存在大量样式遗漏和工程问题，会导致运行时组件损坏。

## 目标
保留有价值的交互架构改进，修复所有样式缺失、运行时问题、测试契约漂移和工程卫生问题，使暂存区代码可安全提交。

## 当前审查结论（2026-05-12）

主会话审查确认：本次改动方向有价值，但当前不是可提交版本。`lint`、`tsc --noEmit`、`build` 可通过，但 `git diff --cached --check` 失败，`TMPDIR=$PWD/.tmp npm run test -- --run` 失败。关键失败包含：

- `MetricChart` 直接使用 `ResizeObserver`，Vitest/jsdom 环境抛出 `ResizeObserver is not defined`。
- `layout.css` 删除了 `GlobalSearch` 菜单样式和移动端响应式断点，只留下注释，导致搜索下拉与小屏布局回归。
- CSS 中存在未定义 token：`--ease-fluid`、`--ease-bounce`、`--dur-normal`、`--ease-spring`、`--type-h3-size`、`--type-heading-line`、`--type-display-font`、`--type-h1-font`。
- Settings 通知渠道 Modal 直接写入正式 form state，关闭 Modal 后仍可能保存未确认的渠道草稿。
- Node/Target Detail 将二级折叠区改成 `watchtower-property-list`，需要明确新契约并同步测试。
- 多处 `transition: all`、全局 `outline: none`、trailing whitespace 与设计/工程规范不符。

## 必须修复的问题清单

### P0 - 运行时损坏
1. **补回 atoms.css 中被删除的样式**（约 50+ 个 CSS 类）
   - `.metric-chart` 及所有子类（tooltip、axis-text、placeholder、threshold-label 等）
   - `.sparkline` 及所有子类（tooltip、hint、placeholder 等）
   - `.collapsible-section` 及所有子类（文件仍存在 `web/src/components/CollapsibleSection.tsx`）
   - `.data-table__sort-*`, `.data-table__caption`, `.data-table__th--*`, `.data-table__cell--*`
   - `.drawer--left`, `.drawer--right`, `.drawer--open` 方向类
   - `.card--state.card--ribbon-left/top.tone--*`（7 个状态色带）
   - `.btn:focus-visible`
   - `.hostname`, `.hostname--truncate`, `.mono-digits`, `.timestamp__abs`
   - `.stepper__connector`, `.stepper__dot`, `.stepper__label`, `.stepper__step--*`
   - `.toggle:disabled`, `.toggle:focus-visible`
   - `.badge--success`（新增，被 Telegram/Feishu Settings 引用）
2. **修复 Modal 焦点管理双重竞争**
   - `Modal.tsx` 自己维护 `previousFocusRef` 与 `useModalFocus` hook 内的 `restoreFocusRef` 冲突
   - 移除 Modal.tsx 内的焦点恢复逻辑，统一由 useModalFocus 管理
3. **修复 MetricChart ResizeObserver 运行时/测试环境崩溃**
   - `ResizeObserver` 不存在时不得抛错
   - 无测量能力时使用固定 fallback width，并保持 `viewBox` / hover 计算一致
   - Vitest 中 MetricChart 与 NodeDetail 相关测试不得再因 ResizeObserver 崩溃
4. **恢复 AppShell 搜索菜单与移动端响应式断点**
   - 补回 `.global-search__menu`、hint、item、focused item 等样式
   - 补回 `max-width: 860px` / `max-width: 560px` 下 sidebar/main/search 响应式布局
   - 不允许用注释替代被删除的 CSS 契约

### P1 - 工程/可访问性
5. **移除 Google Fonts 外部依赖**
   - `tokens.css` 中 `@import url('https://fonts.googleapis.com/...')` 在内网/离线环境失效
   - 改为纯系统字体栈，保留 `--font-sans` / `--font-mono` / `--font-serif` 定义但去除外部请求
6. **修复全局 focus-visible 覆盖**
   - `reset.css` 中 `*:focus-visible { ... !important }` 过于激进
   - 去掉 `!important`，或改为 `:root:focus-visible` 等更精确的选择器
7. **修复 page-fade-in 动画重复触发**
   - `.page-body { animation: page-fade-in 0.4s }` 在 React re-render 时会不断重播
   - 改为只在路由切换时触发，或改为更不易重触发的实现
8. **修复 MetricChart viewBox 与 width 不一致**
   - 当 `propsWidth` 未传入时，`svg width='100%'` 但 `viewBox` 使用固定像素
   - 确保 ResizeObserver 变化时 SVG 正确重绘
9. **补回 --chart-* 调色板**
   - 原 tokens.css 有 `--chart-1` 到 `--chart-6`，新版本完全删除
   - 这些 token 可能被图表组件使用
10. **清理未定义 CSS token**
    - 不新增与 v2 token 体系不一致的 `--ease-fluid` / `--ease-bounce` / `--dur-normal` / `--ease-spring`
    - 改回 `--ease-calm` / `--dur-micro` / `--dur-state` / `--dur-page`
    - `reset.css` 不引用不存在的 font token
    - `pages.css` 不引用不存在的 h3 / heading line token
11. **修复 Settings 通知渠道 Modal 状态机**
    - Modal 中的草稿关闭/返回时不得悄悄写入可保存 form state，除非用户确认添加
    - 新增渠道确认后展示对应配置区并保持“未保存”语义清晰
    - 已配置渠道不应因为本地 activeChannels 初始为空而被错误隐藏
12. **同步 Watchtower 二级信息新契约与测试**
    - 若保留 `watchtower-property-list`，测试改为验证属性列表契约，而不是旧折叠区契约
    - 关键行为（metadata 保存、生命周期确认、Probe 删除/归档确认、焦点 ref）必须继续覆盖

### P2 - 设计一致性
13. **恢复候风视觉辨识度**
   - 暗色主题 accent 从科技蓝 `#3B82F6` 恢复为金色系 `#C9A56F`（或兼容的候风金色）
   - Light theme 从 Slate 灰恢复为「宣纸白」基底的原有配色
   - `--color-state-*` 在 light theme 中补定义
14. **中文字体优先**
   - `--font-sans` 将中文字体放回首选位置，`Inter`/`Outfit` 放到后面作为西文 fallback
15. **SettingsPage Modal 语义优化**
    - 「确认新增」按钮文案改为「添加并编辑」，避免用户误以为已保存
16. **控制 Glassmorphism 的使用强度**
    - 保持 v2 “冷静、克制、高密度、长期使用友好”方向
    - 修复 `transition: all`、过度模糊、过大 padding、纯黑/纯白等偏离 v2 规范的样式
17. **工程卫生**
    - 清理 trailing whitespace
    - 保持当前暂存区在非 main 分支上
    - 不直接提交到 `main`

## 不修复/保留的改动
- SettingsPage Tabs 分组（general/notifications/advanced）
- 通知渠道管理 Modal（Telegram/Feishu 的添加流程）
- `watchtower-property-list` 统一属性列表布局
- `Button` forwardRef 改进
- `MetricChart` ResizeObserver 自适应宽度
- Settings Section 改用 `Input` / `Toggle` 原子组件
- `NodeDiagnosisSummary` 删除

## 验收标准
- [x] 当前分支不是 `main` / `master`（`fix/glassmorphism-web-hardening`）
- [x] `git diff --check` 成功
- [x] `cd web && TMPDIR=$PWD/.tmp npm run test -- --run` 成功（60 files / 458 tests）
- [x] `cd web && npm run build` 成功（仅 Vite >500 kB chunk 警告）
- [x] `cd web && npm run lint` 无错误
- [x] `cd web && npx tsc --noEmit` 无错误
- [x] 暂存区 CSS 中没有本轮已知未定义 token / `transition: all` / `outline: none` 等高风险命中
- [x] AppShell 搜索菜单和移动端响应式 CSS 恢复
- [x] Settings 通知渠道 Modal 不会保存未确认草稿
- [x] 所有高风险 atoms（Button, MetricChart, Modal, DataTable, Sparkline, Drawer, Stepper 等）样式/测试契约恢复
- [x] 无 Google Fonts import
- [x] Light theme 和 Dark theme 的 state 颜色补齐
- [x] focus-visible 不破坏组件内部定制

## 本轮修复结果（2026-05-12）
- `MetricChart` 对缺失 `ResizeObserver` 做降级，jsdom 与真实布局都不会崩溃。
- `layout.css` 恢复 GlobalSearch 菜单和 860px / 560px 移动端断点。
- `SettingsPage` 通知渠道 Modal 改为独立 draft，关闭/返回不会污染可保存 form。
- Node Detail 恢复四个低频折叠区：标签与备注、生命周期、接入凭证状态、容器列表。
- Target Detail 恢复归档确认 `alertdialog` 与本地错误，保留生命周期低频区和 Probe 并发锁定按钮。
- 标签与备注组件恢复 heading、`标签：`、`备注：` 文案，避免只读态语义丢失。
