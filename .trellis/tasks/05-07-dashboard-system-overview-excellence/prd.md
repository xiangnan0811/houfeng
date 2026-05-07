# Dashboard system overview excellence

## Goal

把首页从“可用但像压缩列表”的状态提升为候风的系统总览工作台：用户进入首页后应立即理解全局态势、下一步处置对象、24h 变化趋势和管理入口，并且页面视觉需要更成熟、更高效、更有秩序。

## What I Already Know

- 用户明确要求首页必须达标，其他页面可以后置；目标不只是可用，还要美观、高效、用户体验良好。
- 当前截图中首页已比早期少了杂乱区块，但仍偏空、偏线框、偏“异常列表”，没有形成服务器管理系统首页应有的全局掌控感。
- `DashboardOverview` 已提供首页可用事实：生成时间、节点/目标总量、异常/严重/维护计数、库存完整度、24h 新增/恢复、group summaries、通知配置摘要、异常队列、最近事件，以及可选 24h trend 数组。
- 现有设计契约禁止回退到全字段摊开、Group 列表、最近事件列表、独立 KPI strip；允许在主工作台内展示最多 3 个低权重运行上下文 link item。
- 代码已有 `Sparkline` atom，可用于轻量趋势表达，不需要引入依赖。

## UX Diagnosis

- 首屏焦点仍不够强：标题说明了异常，但状态、趋势、入口之间缺少一个顺滑的信息路径。
- 视觉语言仍像原型：大量边线和等权矩形让页面显得“拼装”，不够像成熟系统。
- 异常队列的信息主次需要调整：对象名称和当前问题应比技术 ID 更突出。
- 信息密度不稳定：去掉杂乱后，首页下半部分过空，缺少同类服务器管理首页常见的资源/事件/配置入口。
- 跳转路径不够自然：用户能看到侧边栏和按钮，但首页内部没有把“处理当前问题”和“管理整个系统”连成一个工作台。

## Research References

- [`research/dashboard-patterns.md`](research/dashboard-patterns.md) — 同类监控/运维首页通常组合状态摘要、趋势变化、待处理项和资源入口；候风应吸收这些共性，但继续保持渐进披露。

## Requirements

- 顶部状态区必须保持单一主结论，不增加第二个 hero 或同权摘要区。
- 顶部状态区需要补充更明确的 24h 变化/趋势表达，帮助用户判断异常是在扩散、恢复还是持平。
- 关键指标仍最多 4 个，且必须保留现有深链契约。
- 异常工作台必须把处置对象、严重等级、当前问题、时间/位置上下文和处理入口组织成清晰的一行，不让 ID 抢占主要注意力。
- 异常态也需要保留 compact 管理入口，方便用户跳转到节点、目标、事件、设置，不再让首页只像“异常列表”。
- 运行上下文仍最多 3 项，不恢复 Group 摘要列表或最近事件列表。
- 不新增后端字段、不新增依赖、不引入新的全局状态机制。

## Acceptance Criteria

- [x] 严重异常态首屏清楚呈现：全局结论、24h 趋势/变化、4 个关键指标、异常处理队列、运行上下文、管理入口。
- [x] 异常队列中对象名称与当前问题优先，技术 ID 退居次级信息。
- [x] Dashboard 仍不展示 `已加载 /api/dashboard`、`首页数据可信度`、`系统全局指标`、`Dashboard 摘要指标`、`系统快捷入口`、`Group 摘要`、`最近事件摘要`。
- [x] 所有新增入口使用既有 URL 深链，不引入未承接的筛选参数。
- [x] `DashboardPage.test.tsx` 覆盖趋势入口、异常态管理入口、主次信息结构和不回退断言。
- [x] `docs/design/v2-houfeng/component-spec.md` 同步记录本轮首页契约变化。
- [x] `cd web && npm run lint`、`cd web && npm run test -- --run`、`cd web && npm run build` 通过。

## Out Of Scope

- 不改后端 dashboard contract。
- 不重做节点、目标、事件、设置页面。
- 不引入图表库或 E2E/截图测试框架。
- 不把 Dashboard 做成全量报表页。

## Technical Notes

- 主要代码：`web/src/pages/DashboardPage.tsx`、`web/src/styles/pages.css`、`web/src/pages/DashboardPage.test.tsx`。
- 设计契约：`docs/design/v2-houfeng/component-spec.md`。
- 需遵守 `.trellis/spec/web/state-and-data.md` 的 Dashboard 渐进披露约束。
- 需复用 `Sparkline` atom 与现有 `Badge`、`StatusGlyph`、`Timestamp`、`MonoDigits`。
