# Dashboard、节点列表、节点详情三页面 P1 改进

## Goal

在 P0 基础上继续提升交互体验和 UI 细节。全局搜索和面包屑已实现，本 task 聚焦剩余 3 项原 P1 + 5 项新补充。

## Decisions

- **范围**：8 项全部做（3 项原 P1 剩余 + 5 项新补充）
- 全局搜索和面包屑已存在（`GlobalSearch.tsx`、`Breadcrumb.tsx`、`TopBar.tsx`），不在本 task 范围

## Requirements

### 1. 创建节点表单 Drawer 化（NodesPage）
- 将内联 `nodes-create-panel` 表单移入右侧 Drawer
- "新建节点"按钮打开 Drawer，表单提交逻辑不变
- 创建成功后跳转接入工作台（保持现有行为）
- Drawer 关闭时重置表单

### 2. `<details>` 替换为动画折叠组件（NodeDetailPage）
- 替换节点详情页 4 个原生 `<details>` 折叠区：
  - 标签与备注 / 生命周期 / 接入凭证状态 / 容器列表
- 新建 `CollapsibleSection` 组件：animated 展开/收起 + header 标题 + caret 旋转
- 复用 `--ease-calm` 和 `--dur-state` 时间常量

### 3. 命令结果结构化展示（NodeDetailPage）
- exit code 用 Badge 展示（0 → normal, 非0 → critical）
- stderr 用 warning 底色区域展示
- stdout 长输出（> 20 行）默认折叠，可展开
- 命令执行中显示 pending 状态指示

### 4. ⌘K 全局搜索快捷键（GlobalSearch）
- `⌘K` / `Ctrl+K` 聚焦搜索框
- 已有搜索框时再按关闭
- Escape 关闭搜索
- 不与浏览器/OS 原生快捷键冲突

### 5. 节点详情指标说明 tooltip（NodeWatchtowerMetrics）
- hover 每张 metric card 的 h3 标题时显示 tooltip
- tooltip 内容：指标含义 + 正常范围 + 当前状态评价
- 用 CSS-only 或简单 title 实现（不引入 tooltip 库）
- 8 个指标的文案预定义

### 6. 数据快照时间修正（NodeDetailPage）
- 当前使用 `new Date().toISOString()` 改为 `runtimeFacts` 中最新 sample 的 `observed_at`
- 如无 sample，显示"尚未收到主机样本"

### 7. 节点列表趋势列可隐藏（NodesPage）
- DataTable 趋势列（24h CPU/Mem/Disk sparkline）右侧加列可见性切换
- 默认显示，用户可手动隐藏
- 用简单的 toggle button 或 dropdown 实现

### 8. Dashboard 手动刷新按钮（DashboardPage）
- FleetStatePanel 的 action 区域加一个刷新按钮（ghost 变体）
- 点击重新调用 `getDashboard()` 并刷新数据
- 加载中显示 "刷新中…" disabled 态
- 不做 auto-refresh / polling

## Acceptance Criteria

### Drawer 创建表单
- [ ] 点击"新建节点"打开右侧 Drawer，内嵌创建表单
- [ ] 提交成功 → Drawer 关闭 → 跳转接入工作台
- [ ] 关闭 Drawer 时表单重置
- [ ] Drawer 外点击 / ESC 可关闭

### CollapsibleSection
- [ ] 4 个 `<details>` 全部替换为 CollapsibleSection
- [ ] 展开/收起有 220ms calm 动画
- [ ] caret 图标旋转 90°
- [ ] 视觉符合 v2 设计规范

### 命令结果
- [ ] exit code 0 显示绿色 Badge，非 0 显示红色 Badge
- [ ] stderr 有 warning 背景色
- [ ] stdout > 20 行默认折叠
- [ ] pending 态显示加载文案

### ⌘K 快捷键
- [ ] ⌘K / Ctrl+K 唤起全局搜索，已有搜索时关闭
- [ ] Escape 关闭搜索
- [ ] 不拦截浏览器原生快捷键

### 指标 Tooltip
- [ ] hover 指标标题显示 tooltip
- [ ] tooltip 包含含义、正常范围、当前评价
- [ ] 8 个指标全覆盖

### 其他
- [ ] 数据快照时间使用实际 observed_at
- [ ] 趋势列可隐藏/显示
- [ ] Dashboard 有手动刷新按钮，加载态正确

## Definition of Done

- TypeScript 编译通过
- ESLint 通过
- Vitest 现有测试通过
- 视觉符合 design-language.md v2 规范

## Out of Scope

- 后端 API 改动
- 引入外部库/框架
- 移动端适配
- 实时推送 / auto-refresh
- 全新全局搜索实现（已有）
- 全新面包屑实现（已有）

## Technical Notes

- P0 已提交：`d5280a8`
- 已有组件：GlobalSearch、Breadcrumb、TopBar、Drawer
- 需新建：CollapsibleSection
- 设计权威：`docs/design/v2-houfeng/design-language.md`、`docs/design/v2-houfeng/component-spec.md`
- 受影响文件：NodesPage.tsx、NodeDetailPage.tsx、NodeWatchtowerMetrics.tsx、DashboardPage.tsx、GlobalSearch.tsx
