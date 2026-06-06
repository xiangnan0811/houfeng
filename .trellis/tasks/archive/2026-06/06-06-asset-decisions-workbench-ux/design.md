# 资产组合决策工作台 UX 收敛与详情体验重构 - Design

## Architecture / Boundaries

- 本任务以前端信息架构和展示层为主，默认只修改 `/asset-decisions` 页面及其样式和测试。
- 后端 API、数据库 schema、asset decision domain 语义保持不变；不得新增 migration、批量执行、自动 PATCH 业务对象或 agent 性能判断。
- 自动组仍是主发现入口；场景模板、自定义组合、已保存记录是辅助闭环层；续费证据和单台队列是底部辅助层。
- 旧 URL-state 保持兼容：`view=single_queue` 可进入页面并定位/提示单台辅助队列，但不再让主组合 tabs 显示空态。

## Data Flow / Contracts

- 继续使用现有 API helper：overview、groups、manual groups、scenario templates、records、subscriptions、VPS queue。
- 组合 tabs 只传递合法 asset decision view；`single_queue` 在前端转为辅助队列模式，groups API 不请求该 view。
- `下一步导览` 仍从当前加载 rows 派生，不新增接口；失败源只影响该来源 work item。
- execution plan CTA 继续按 step kind 在前端本地映射 URL，不触发业务写接口。

## UI Design

- 页面顶部：标题、紧凑 summary、context chips、续费窗口和辅助入口；减少全宽 dashboard 感。
- 主 surface：决策组列表优先，配套紧凑 next-work rail/strip。组行突出“为什么要决策”和主要证据。
- 场景与记录：用一个 workspace 承载模板启动器、自定义组合、已保存记录；模板用 launcher/card，组合和记录可继续用紧凑表格或列表。
- 辅助区：续费证据和单台队列降级为 collapsible/support sections，保留原能力。
- 详情：保留现有 modal 机制，优先改善记录详情和组详情的摘要/卡片层级，明细表作为低层级完整数据。

## Compatibility / Risk Controls

- 不删除现有 API helper、不改变 payload shape、不改变 backend routing。
- 不把 readback drift 或 execution plan 当作自动执行承诺。
- 不让订阅请求失败变成真实缺订阅判断。
- 拆分组件仅在降低风险和提升可维护性时进行；避免为纯重构扩大变更面。
