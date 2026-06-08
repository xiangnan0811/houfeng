# Polish monitor list display

## Goal

调整监控列表页面的信息层级，使列表更适合快速扫描运行证据：监控名称列只承载身份，当前主问题列承载心跳/异常主线，监控实例状态与资产上下文状态保持统一、短文本且不换行，低频操作移到监控详情页处理。

## Confirmed Facts

- 当前实现位于 `web/src/pages/monitoring/MonitoringInstancesTableColumns.tsx` 和 `web/src/pages/monitoring/MonitoringInstancesListSection.tsx`。
- 监控名称列现在显示 `display_name`，并在同一列追加 `monitoring_status`、部分 `lifecycle_status`、`last_heartbeat_at`、`last_sync_at`。
- 当前主问题列现在显示活跃异常数量、`current_primary_issue_summary`，绑定冲突时显示“等待绑定确认”和“指纹变更待确认”标签。
- 资产上下文列现在从 `assetContexts` 读取 linked VPS summary，显示 `assetContextMessage(context)` 以及 `vpsLifecycleLabel(primary.lifecycle_status) · subscriptionStateLabel(primary.subscription_state)`。
- 操作列现在包含快速编辑标签、接入 agent、详情、进入/退出维护、暂停/恢复监控等行级动作。
- 监控列表已有整行点击进入详情的行为；行内 `<Link>` / `<button>` 被交互目标 guard 排除，不会触发行导航。
- 前端规范要求高密度表格短状态列有明确宽度和 `white-space: nowrap` 保护，窄屏只允许表格容器内部横向滚动，不允许页面整体横向溢出。

## Requirements

- 监控名称列不得显示心跳时间和同步时间。
- 当前主问题列必须显示心跳时间作为正常扫描事实。
- 当前主问题列在未收到心跳时必须显示对应问题，而不是空白或“暂无明显异常”。
- 当前主问题列在有绑定冲突或后端主问题摘要时，仍应优先显示对应问题。
- 监控实例自身状态、资产上下文状态和订阅/VPS 状态必须统一为少量短状态，不得在同一行堆叠多种长标签。
- 状态文本必须短且不可换行。
- 操作列应移除；相关行级操作进入监控详情页处理。
- 列表里的“快速编辑标签”入口也应一并移除；标签 / Group 维护迁移到或保留在监控详情页处理。
- 移除操作列后，监控表各列宽度需要重新分配，保证桌面、窄屏和多种页面宽度下视觉密度合理。
- 不改变后端 API contract，不新增监控列表接口字段。
- 不改变监控详情页现有运行控制、接入和维护能力。

## Out of Scope

- 不重做 MonitoringPage 的整体信息架构、quick view、筛选 Drawer 或批量操作区。
- 不改变 MonitoringInstance / VPS / Subscription 的后端状态机和枚举值。
- 不新增真正的自动状态迁移或资产处置动作。
- 不新增截图资产或视觉回归测试框架。

## Decisions

- 已确认：移除操作列时，列表里的“快速编辑标签”也一并移除。

## Acceptance Criteria

- [ ] 监控名称列只展示监控实例名称、必要 ID/位置类身份信息，不展示心跳时间或同步时间。
- [ ] 当前主问题列对有心跳的正常实例显示“心跳 <相对时间>”或等价短文案。
- [ ] 当前主问题列对从未收到心跳的实例显示“未收到心跳”或等价问题文案。
- [ ] 当前主问题列对绑定冲突实例显示“等待绑定确认”，并避免再堆叠长状态标签。
- [ ] 当前主问题列对有 `current_primary_issue_summary` 的实例优先显示该主问题，同时保留心跳事实的低权重辅助信息或不与问题冲突的短文案。
- [ ] 监控实例状态与资产上下文状态在列表中统一为短状态文案；同一状态语义不得在同一行以多种标签重复出现。
- [ ] 状态文本在表格中不换行。
- [ ] 表格不再渲染“操作”列；列表行仍可点击进入监控详情。
- [ ] 原操作列中的详情、接入 agent、运行控制、标签编辑能力不从系统中消失，改由监控详情页承接或保留已有详情页能力；列表不再提供快速编辑标签入口。
- [ ] 桌面和窄屏下表格列宽保持高密度、可扫描；窄屏通过表格容器横向滚动承接，不造成页面整体横向溢出。
- [ ] 相关 Vitest 用例覆盖列显示、无心跳、绑定冲突、操作列移除和关键交互承接。

## Notes

- 质量门：前端改动至少运行 `cd web && npm run lint`、`cd web && npm run test -- --run`、`cd web && npm run build`。用户可见 UI 改动需要本地浏览器 sanity 或等价证据说明。
