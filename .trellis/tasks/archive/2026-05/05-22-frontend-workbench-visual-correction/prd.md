# Frontend workbench visual correction + design system migration

## Goal

本任务从视觉密度修正出发，演进为完整的前端设计系统迁移和页面 IA 重构：

1. 建立纯 CSS + BEM + 3 主题 token 设计系统（houfeng-dark / houfeng-light / classic-dark）
2. 将所有页面从旧组件/类名迁移到新设计系统
3. 完成事件页面的全面功能重构（分页表格 + 趋势图 + 名称解析 + 本地筛选 + CSV 导出）
4. 统一按钮尺度、工具栏布局、表格密度、筛选面板交互模式

## 已完成的工作

### 设计系统基础
- 纯 CSS 设计系统：`web/src/index.css` 统一 token 层（3 主题）
- Button 组件从 BEM (`btn--variant`) 迁移到扁平命名 (`btn sm primary`)
- 统一 page-header / hero-stats / filter-panel / table / card / animate-in 模式
- 删除旧 CSS 文件（layout.css / filters.css / LoginPage.css）

### 页面迁移（已完成）
- **Dashboard**：wb-cards / wb-columns / table / animate-in
- **Nodes**：hero-stats + filter-panel + DataTable + 批量操作
- **Targets**：hero-stats + filter-panel + DataTable + 批量操作
- **Providers**：page-header + DataTable（简洁表格页）
- **Subscriptions**：filter-panel + filter-bar + table + Drawer 表单
- **Events**：hero-stats(Sparkline) + filter-panel(内联) + 分页表格 + 名称解析 + CSV 导出
- **Settings**：Tabs + settings-section（表单密集型布局）
- **Login**：简化为原生表单
- **VPS / Asset Decisions / Node Compare / Node Onboarding**：轻量调整

### 事件页面重构（本次会话完成）
- 时间分组卡片 → 分页表格（6 列：时间/严重度/事件类型/异常类别/摘要/对象链接）
- 新增 hero-stats 趋势区（getDashboard API）
- 名称解析（listNodes + listTargets → Map<id, name>）
- 客户端分页（200/次，20/页，加载更多）
- 本地筛选（incident_class 精确匹配 + keyword 模糊匹配）
- CSV 导出（全部 filteredEvents）
- 内联筛选面板（EventsFilterPanel，立即应用）+ 高级筛选抽屉（draft/apply）
- URL 状态完整保留（useSearchParams 双向同步）
