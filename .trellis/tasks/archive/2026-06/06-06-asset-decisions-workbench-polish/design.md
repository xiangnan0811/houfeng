# 资产组合决策工作台体验升级 · Design

## Boundary

本任务只改前端页面体验、前端测试和相应设计/Trellis spec。后端 Asset Decisions read model、records API、manual groups、scenario templates、execution readback 和 execution plan 均保持现有合同。

允许改动：

- `web/src/pages/AssetDecisionsPage.tsx`
- `web/src/pages/AssetDecisionsPage.test.tsx`
- 页面所需的 `web/src/index.css` 局部样式
- `.trellis/spec/web/state-and-data.md`
- `docs/design/v2-houfeng/component-spec.md`
- 必要的 visual evidence 文档

不允许改动：

- Go backend handler/store/domain 行为
- API request/response 语义
- VPS / Subscription / MonitoringInstance / Target 写接口
- 全局 design tokens、AppShell、Sidebar 或其它页面布局，除非测试暴露必须修复的直接依赖

## UX Architecture

页面结构保留现有分层，但增强扫描路径：

1. `Portfolio Command Summary`
   - 取代松散统计卡的认知角色，但可复用现有 `asset-decision-focus` 区域。
   - 生成一个“今日第一步”/“优先处理”提示，来源于 next work、overview 和当前 view。
   - 展示组合范围、续费窗口、执行闭环风险、资料可用性和上下文筛选摘要。
   - 不新增数据请求，只从当前 `overview`、`closedLoopMetrics`、`contextFilterChips`、`nextWorkItems` 派生。

2. `Primary Workbench`
   - 左侧继续是自动组列表，右侧继续是下一步导览。
   - 自动组卡片调整为：rank/source → main issue/recommendation → evidence assessment → decision facts → action。
   - 指标从 5 个同权小格改为 3-4 个更可读的事实块，避免移动端压缩过密。

3. `Scenario & Memory`
   - 保持模板、自定义组合、已保存记录三个 surface，但标题文案强调流程关系。
   - 已保存记录列表保留 readback/plan 信息，但视觉低于自动组，不作为第一主 surface。

4. `Record Detail Execution Board`
   - Board 顶部显示计划摘要、lane counts 和 actionable/blocked。
   - Lane 卡片内突出当前事实和 issue，而不是把所有字段堆成表格列。
   - CTA URL 映射仍由现有 `actionHrefForMember` 本地函数完成；`review_record` 仍留在当前记录详情或普通 VPS 详情。
   - 快速跟进继续调用 `saveRecordMemberFollowup`，不自动改 record status。

5. `Support Surfaces`
   - Renewal evidence 和 single queue 保持现有功能和顺序，降低视觉张力。
   - 旧 `single_queue` URL 继续承接到底部队列。

## Data Flow

- `overview`、`groups`、`records`、`manualGroups`、`templates` 已有并行加载逻辑不改。
- 新增的 summary/priority 文案使用纯函数从当前 state 派生：
  - `buildPortfolioLead`
  - `portfolioContextSummary`
  - `firstActionFromNextWork`
  - 需要时只放在 `AssetDecisionsPage.tsx` 内部，不新增全局 helper。
- 任一来源加载失败：
  - 自动组失败只影响自动组 surface。
  - records/manual/templates 失败只影响下一步导览相应来源和自身 surface。
  - 不把失败解释成 aligned、无漂移、无缺口或资料健康。

## Compatibility

- URL-state 合同不变：`view`、`renew_within_days`、context filters 和 open state 继续保持。
- 旧 `view=single_queue` 降级行为不变。
- 单台续费决策 PATCH payload 不变。
- record followup PATCH payload 不变。
- 测试 fixtures 中已有 readback/plan 字段继续使用，不改类型合同。

## Risks And Mitigations

- 风险：视觉升级把记录执行面板提升为主路径。
  - 规避：自动组列表仍在第一主 grid 左侧；记录详情只在 modal 内强化。
- 风险：更强 CTA 被误解为自动执行。
  - 规避：所有 CTA 保持 link 或 record followup PATCH，文案写“打开/核对/复核”，不写“执行取消/完成迁移”。
- 风险：页面移动端横向溢出。
  - 规避：组卡片和 command summary 使用 responsive grid；表格继续包在 `.asset-table-scroll`。
- 风险：测试只覆盖文字，无法证明写接口边界。
  - 规避：保留并扩展“不调用 VPS/Subscription/MonitoringInstance/Target 写接口”的测试断言。

## Rollback

本任务是前端体验层改动。若出现风险，可回滚本分支中 `AssetDecisionsPage.tsx`、`AssetDecisionsPage.test.tsx`、`index.css` 和文档/spec 改动，不涉及数据库或后端迁移。
