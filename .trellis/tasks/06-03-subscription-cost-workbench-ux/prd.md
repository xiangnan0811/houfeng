# 订阅成本中枢 UX 重构

## Goal

将 `/subscriptions` 从混合配置页重构为订阅成本工作台，聚焦查看、判断、筛选和处理；将预算、汇率、提醒等低频配置迁移到 `/settings?tab=subscriptions` 统一管理。

## Requirements

- `/subscriptions` 不再展示预算、汇率、提醒配置表单，只保留订阅配置入口与汇率刷新动作。
- `/settings?tab=subscriptions` 新增订阅设置 tab，独立管理成本基准、汇率刷新、提醒窗口和预算规则。
- 设置页避免嵌套表单：根容器不能再是整页 `<form>`，系统设置与订阅设置分别拥有自己的保存边界。
- 订阅页顶部四个核心指标在桌面宽度下一行展示，窄屏才降为两列或一列，文案和数字不能撑破容器。
- 订阅页新增成本洞察，不做图表墙：本月 VPS 成本占用、年度成本趋势、续费月份、预算风险、provider/category/currency 构成需要被合并组织。
- 筛选控件、active filter chips、结果数量、清除按钮和订阅明细表格必须位于同一个列表工作区，续费队列和洞察不能隔断筛选与列表。
- 年度趋势必须来自后端 `SubscriptionStatistics.cost_month_buckets`，前端不得用当前成本伪造历史。
- statistics/图表加载失败时，订阅列表和核心 overview 仍应可用。
- 创建/编辑订阅 Modal、`/subscriptions?vps_id=<id>&create=1` 自动打开、关闭 URL 清理和错误态需要保持。

## Acceptance Criteria

- [ ] `/subscriptions` 底部不再出现预算、汇率、提醒配置表单。
- [ ] `/subscriptions` 右上角有“订阅配置”入口跳转到 `/settings?tab=subscriptions`。
- [ ] 1440px 下四个顶部指标一行展示，移动端无页面级横向溢出。
- [ ] 成本洞察包含 VPS 成本占用甜甜圈/饼图、年度趋势、续费月份标记、预算风险摘要和可切换成本构成排行。
- [ ] VPS 成本占用 Top 5 + 其他，图例或扇区可键盘访问；点击具体 VPS 能应用 `vps_id` 筛选，点击“其他”不应用模糊筛选。
- [ ] 筛选控件与订阅明细在同一 `page-panel`，active chips 与结果数量可见。
- [ ] `/settings?tab=subscriptions` 初始打开订阅 tab；tab 点击同步 URL；非法 tab 回退默认 tab。
- [ ] 系统设置保存不清空或覆盖订阅成本设置。
- [ ] 订阅 tab 能保存成本设置、刷新汇率、展示/创建/编辑/启停预算。
- [ ] 后端 `/api/subscriptions/statistics?window=year` 返回 `cost_month_buckets`，非法 window 仍 400。
- [ ] 相关前后端测试、lint/build 或等价验证通过，无法执行的验证需要明确说明。

## Out Of Scope

- 不引入第三方图表库或新状态管理库。
- 不做账单导入、成本预测、成本优化建议或报表导出。
- 不给订阅明细新增没有后端合同支撑的健康语义。
