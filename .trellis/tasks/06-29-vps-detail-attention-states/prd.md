# Integrate VPS detail attention states

## Goal

修复 VPS 详情页“当前需要用户处理 / 核对”的状态来源不统一问题。所有持续性的单台 VPS 当前状态，包括运行观测异常、取消/退役、订阅异常、缺运行观测、IP 质量暂不可用等，都必须有机整合到顶部基础信息右侧的“当前判断”中；页面中部不再出现同类状态提示或旧的 context action 出口。

## Confirmed Facts

- 上一轮任务 `.trellis/tasks/archive/2026-06/06-29-vps-attention-current-judgement` 已经把方向定为：中部 `VPSContextActionPanel` 不再显示，顶部 `judgement.attentionItems` 显示多个关注状态。
- 当前 `web/src/pages/vps-detail/vpsDetailOverviewModel.ts` 已经构造 `judgement.attentionItems`，其中包括：
  - `取消/退役`
  - `运行观测需要核对`
  - `订阅证据暂不可用`
  - `缺少当前订阅`
  - `续费时间需要关注` / `自动续费已取消`
  - `缺少运行观测`
  - `IP 质量暂不可用`
- 当前 model 仍保留 `contextAction` 字段，并且 `vpsDetailOverviewModel.test.ts` 仍断言 `contextAction` 会返回 `运行观测需要核对`、`缺少当前订阅`、`订阅证据暂不可用` 等旧状态。
- 当前 `VPSDetailPage.tsx` 不再渲染 `VPSContextActionPanel`，但仍有页面中部 `vps-detail-feedback-stack`，其中 `state.subscriptionsError` 会作为中部错误反馈显示。订阅读取失败已经同时进入顶部 attention，这会造成“持续状态顶部 + 中部重复提示”的风险。
- 现有页面测试只断言没有 `aria-label="需要处理的状态"` section，没有断言 attention 状态不再从 `contextAction` 输出，也没有覆盖多个异常在顶部的实际页面表现。

## Root Cause

上一轮修改只切断了页面中部显式 `VPSContextActionPanel` 渲染，但没有切断旧数据出口。`contextAction` 仍然由 model 返回一个 attention item，并被测试视为合法行为。这使系统同时存在两套语义：

- 新语义：`judgement.attentionItems` 是顶部“当前判断”的多状态列表。
- 旧语义：`contextAction` 是中部“需要处理的状态”的单状态入口。

只要旧字段继续承载 `运行观测需要核对` 这类状态，后续组件、调试代码、局部回退或页面反馈都可能把这些状态重新放回中部。订阅加载错误也暴露了同类问题：持续性的当前状态已经属于顶部判断，但中部反馈栈仍会重复展示。

## Requirements

- `judgement.attentionItems` 必须成为 VPS 详情页持续性待处理状态的唯一前端事实源。
- `contextAction` 不得再返回运行观测、订阅、缺运行观测、IP 质量等 attention 状态；优先从 model 类型中移除该字段。
- 中部页面不得显示 `运行观测需要核对`、`缺少运行观测`、`缺少当前订阅`、`订阅证据暂不可用`、`IP 质量暂不可用` 等持续状态提醒。
- `vps-detail-feedback-stack` 只保留用户刚刚执行操作后的短期结果或提交错误，例如创建订阅失败、经验记录已写入、生命周期动作失败；不得用于订阅读取失败这类由 model attention 覆盖的持续事实。
- 多异常同时存在时，顶部“当前判断”必须显示多个 attention item，而不是只显示一个主动作：
  - 取消/退役优先；
  - 运行观测异常其次；
  - 订阅读取失败 / 缺当前订阅 / 续费临近 / 自动续费已取消；
  - 缺少运行观测；
  - IP 质量暂不可用。
- 顶部 attention 文案必须短，不增加解释性提示文字，不恢复“下一步动作”或中转页式区域。
- 现有关联概览、单机台账、IP 质量概况和 modal / route 行为不得回退。
- 不改后端 API、数据库、MonitoringInstance/Subscription 数据结构。

## Acceptance Criteria

- [ ] `VPSDetailOverviewModel` 不再暴露 `contextAction`，或者该字段恒为空且没有任何页面消费；相关测试不再断言旧字段承载 attention 状态。
- [ ] 当 MonitoringInstance 健康状态为 `关注` / `告警` / `严重` 或活跃异常数大于 0 时，“运行观测需要核对”只出现在顶部 `aria-label="当前判断"` 内，并提供“查看监控实例”和“监控观测”入口。
- [ ] 当取消/退役、运行观测异常、自动续费取消或临近续费等多个异常同时存在时，顶部 `当前判断` 同时显示对应 attention items，动作不被单个 `primaryAction` 覆盖。
- [ ] 订阅读取失败、缺订阅、缺运行观测、IP 质量暂不可用都在顶部 attention 中有入口；页面中部不出现独立的同类提示区域或重复文本。
- [ ] `vps-detail-feedback-stack` 不再渲染 `state.subscriptionsError`，但用户操作后的 notice/error 仍可显示。
- [ ] `VPSDetailPage.test.tsx` 覆盖运行观测异常、多个异常组合、订阅读取失败不在中部重复显示。
- [ ] `vpsDetailOverviewModel.test.ts` 覆盖 attention item 是唯一状态源。
- [ ] 浏览器 sanity 用 mock 数据验证 VPS 详情页顶部当前判断展示多个状态，页面中部只剩关联概览、单机台账、IP 质量概况等主体内容。

## Notes

- 用户对页面中部突兀状态区域和啰嗦提示非常敏感；本任务只收敛状态入口，不做新的解释性 UI。
- 这不是新信息架构任务，而是修复上一轮“顶部当前判断是唯一待处理入口”的不完整实现。
