# Dashboard 同类系统信息补强

## Goal

上一轮已经把 Dashboard 从混乱的信息堆叠收敛成“状态判断 + 当前处理区”，但现在信息密度偏低。此轮目标是在不恢复混乱的前提下，吸收同类服务器管理 / 监控系统的优点，为 Dashboard 补回必要的运行上下文，让用户不仅知道“现在要处理什么”，也能快速理解“影响范围、库存状态、最近发生了什么”。

核心原则：补上下文，不补噪音；吸收成熟系统的信息组织方式，不照搬 widget 化大面板。

## What I Already Know

* 用户反馈当前已经“不混乱”，但信息似乎过少，希望吸取同类或相似系统优点。
* 最新 Dashboard 顶层只有状态栏和一个工作台；异常态重点明确，但缺少影响范围、最近活动、库存完整度这些服务器管理首页常见上下文。
* 当前 `/api/dashboard` 已有可用事实池：`group_summaries`、`recent_events`、库存完整度计数、异常/严重/维护计数、通知配置、异常节点/目标摘要。
* 上一轮规范明确禁止恢复独立 KPI/summary strip、Group 摘要列表、最近事件摘要列表、系统快捷入口 rail。
* 此轮应在工作台内部补一个低噪音 context strip，而不是新增同权大 section。

## Research References

* `research/comparable-systems.md` — 同类系统 Dashboard 信息组织模式和候风可吸收点。

## Requirements

### 1. 补运行上下文，不恢复信息仓库

在 Dashboard 主工作台内新增一个紧凑“运行上下文”区域，建议包含 3 个信息点：

* 影响范围：从 `group_summaries` 得出受影响分组数量 / 当前最大影响分组，不展开完整 group list。
* 库存状态：节点/目标总量、待接入、暂停、退役/归档等管理状态，不恢复大卡片。
* 最近活动：只展示最近事件类型、严重度和时间，不展示事件摘要列表，避免把 EventsPage 搬回首页。

### 2. 状态适配

* 异常态：上下文放在异常处理队列下方，作为辅助判断，不抢“当前需要处理”的主位置。
* 正常/维护态：上下文可放在运行概览后方，补足系统状态解释。
* 首次接入态：不显示运行上下文，保持 onboarding-only。

### 3. 链接与可达性

每个 context item 都应是明确入口：

* 影响范围 → 优先跳异常节点/异常目标；无异常时跳节点。
* 库存状态 → 根据当前最需要管理的库存状态跳转到 nodes/targets filtered route。
* 最近活动 → `/events?time_range=24h`。

### 4. 视觉约束

* 只在现有 `DetailSection` 内部新增一个低权重 strip。
* 不使用大数字 hero、不新增 card-heavy KPI row。
* 不显示 `Group 摘要` heading，不显示 `最近事件摘要` heading。
* 长文本可截断或换行，移动端单列。
* 使用现有 tokens、BEM、Link、Badge、Timestamp、MonoDigits。

### 5. Tests

更新 `DashboardPage.test.tsx`：

* 异常态展示运行上下文，但仍不出现 `Group 摘要` / `最近事件摘要`。
* 最近活动不直接展示事件 summary 文案，只展示事件类型和时间。
* 首次接入态不展示运行上下文。
* 正常态可看到运行上下文与管理入口，但不恢复 Group/recent 列表。
* 链接 href 保持 PR4 deep-link contract。

## Acceptance Criteria

* [ ] Dashboard 信息密度高于上一轮，但顶层仍保持状态栏 + 工作台。
* [ ] 新增 `运行上下文` 仅在工作台内部出现，最多 3 个 context item。
* [ ] 不恢复独立 summary/KPI strip、Group list、Recent event list、shortcut rail。
* [ ] 异常态 context 不显示事件 summary 文案。
* [ ] 首次接入态仍 onboarding-only。
* [ ] 测试覆盖新增 context 及防回归约束。
* [ ] 更新 Dashboard 相关 spec / design docs。
* [ ] `DashboardPage.test.tsx`、全量测试、lint、build、`git diff --check` 通过。

## Out of Scope

* 不新增后端字段或修改 `/api/dashboard` contract。
* 不做可配置/可拖拽 dashboard。
* 不新增图表、资源曲线、CPU/内存/磁盘概览，因为当前 contract 没有这些事实。
* 不引入第三方 UI 或图表库。

## Technical Notes

* 主要代码：`web/src/pages/DashboardPage.tsx`。
* 样式：`web/src/styles/pages.css`。
* 测试：`web/src/pages/DashboardPage.test.tsx`。
* 规范：`.trellis/spec/web/state-and-data.md`、`docs/design/v2-houfeng/component-spec.md`。
* 外部参考只作为信息结构启发，不复制品牌样式或 widget 系统。
