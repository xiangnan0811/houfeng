# VPS 详情页 V17 实施方案

## 当前阶段

当前任务已处于 Trellis `in_progress`。用户已确认设计方案按 V17 推进；本文件的目标是把已确认设计转成可审查、可执行、可验证的实施方案，并在实现中随最新审查反馈修正入口职责。

本方案不继续发散设计，不重做 mockup，不重新讨论外部订阅页、监控页或 IP 质量详情页设计。

## 实施目标

把 `/vps/:vpsId` 默认页重构为 V17 主体结构：

1. `VPS 综合基础信息`
2. `关联概览`
3. `单机台账`
4. `IP 质量概况`

并收敛所有本页快速管理和局部详情为居中 `Modal`：

- `基础资料`
- `调整决策`
- `创建/更新订阅`
- `延长有效期`
- `监控观测`
- `接入/升级 agent`
- `关联已有监控实例`
- `服务详情`
- `域名详情`
- `资产历史`
- `记录经验`
- `取消/退役`
- `新增服务`
- `新增域名`

## 不做范围

- 不重构 `/subscriptions`、`/monitoring/:id`、`/targets/:id`、`/asset-decisions`。
- 不重构 `/vps/:vpsId/ip-quality` 独立详情页，只保证 VPS 默认页能跳转过去。
- 不引入新 UI 框架、CSS-in-JS、Tailwind、截图回归框架或新状态管理库。
- 不复制 V17 mockup 的 `.v17-*` 类名、硬编码配色、白底卡片、阴影或局部 CSS。
- 不改变后端 API contract；本轮只消费现有 `VPSAssetDetail`、`SubscriptionRecord`、`VPSIPQualityReport`、`VPSTimeline`、服务/域名和监控实例摘要。
- 不新增取消/退役后端 API；现有 cancellation preview / apply contract 已覆盖影响范围预览与确认执行。

## 代码现实与约束

### 已确认代码事实

- 当前分支：`feat/vps-detail-redesign`。
- 当前 task：`.trellis/tasks/06-26-vps-detail-redesign`，状态 `in_progress`。
- 当前样式入口实际是 `web/src/index.css` 与 `web/src/styles/reset.css`；spec 中提到的 `web/src/styles/pages.css`、`atoms.css`、`tokens.css` 在当前工作区不存在。实施时必须按当前实际入口落样式，并继续使用现有 token 变量和 BEM 风格。
- `web/src/components/atoms/Modal.tsx` 已是居中 portal modal，支持 `size="sm" | "md" | "lg" | "xl"`、`persistent`、`aria-modal`、focus trap。
- `VPSDetailPage.tsx` 当前已经用 `Modal` 承载所谓 `activeDrawer`，不是实体抽屉；但变量名、CSS class 和用户可见标题仍有抽屉/旧工作台遗留。
- `openMonitoringAgentWorkbench` 已具备 0/1/多 active monitoring links 分流：
  - 0 个：打开创建监控实例流程。
  - 1 个：跳转 `/monitoring/:id?onboarding=1&return_vps=:vpsId`。
  - 多个：打开监控证据 modal。
- 现有深链能力：
  - `/vps/:id?workbench=subscription`
  - `/vps/:id?workbench=monitoring`
  - `/vps/:id?workbench=monitoring-instance-create`
  - `/vps/:id?workbench=cancellation`
- 已确认取消/退役 API 与 modal 能力：
  - `getVPSCancellationPreview(vpsId)` -> `/api/vps/:id/cancellation-preview`
  - `applyVPSCancellation(vpsId, input)` -> `/api/vps/:id/cancellation`
  - `VPSCancellationWorkbench` 已承载影响范围摘要、订阅处理、监控实例处理、探测对象处理、原因与确认执行。
- 现有 IP 质量摘要组件：`web/src/pages/vps-detail/VPSIPQualitySection.tsx`。
- 当前默认页仍渲染旧结构：`VPSDetailHero`、`VPSDecisionBoard`、`VPSIPQualitySection`、独立 `成本卡片`、生命周期确认卡和 modal。

### 必须保留的能力

- 续费决策编辑。
- 创建/更新订阅。
- 延长有效期。
- 接入/升级 agent。
- 关联已有监控实例。
- 监控实例证据查看与解除关联。
- 基础资料查看与编辑。
- 服务详情、域名详情、新增服务、新增域名。
- 资产历史查看与记录经验。
- 取消/退役工作流：只在状态合适时由顶部 `当前判断` 显示入口；正常状态下不在默认页和更多菜单常驻展示。
- 归档/恢复确认流程。
- IP 质量完整报告入口 `/vps/:vpsId/ip-quality`。
- 订阅读取失败不能误判为缺订阅。

## 文件地图

### 修改文件

- `web/src/pages/VPSDetailPage.tsx`
  - 页面装配点。
  - 替换默认页结构。
  - 保留数据加载、提交 handler、深链分流和 modal 渲染。
  - 继续复用 `shouldExposeCancellationWorkbench(detail, preview)` 或等价 predicate，作为顶部判断动作的显示条件。
  - 收敛 modal 标题和 `activeDrawer` 命名遗留。

- `web/src/pages/vps-detail/types.ts`
  - 新增 V17 presenter 类型。
  - 保留现有 form draft 和 page state 类型。
  - 不做全量历史重命名；新增 `export type VPSDetailModalMode = VPSDetailDrawerMode`，新 presenter 和新组件使用 `VPSDetailModalMode`，主页面的 `activeDrawer` 状态可在本轮保留。

- `web/src/pages/vps-detail/vpsDetailHelpers.ts`
  - 新增或导出续费/到期文案 helper。
  - 保留表单 input builder。

- `web/src/pages/vps-detail/vpsDecisionModel.ts`
  - 保留 `buildVPSDecisionModel` 作为决策来源。
  - 调整旧 `NextAction` 按钮文案：`快速创建订阅` -> `创建/更新订阅`；`创建并接入 agent` -> `接入/升级 agent`。
  - 保持纯函数，不发请求。

- `web/src/pages/vps-detail/VPSDetailHero.tsx`
  - 本轮不继续扩写旧 hero。
  - 默认页停止使用旧 hero，改用新增 `VPSDetailOverviewPanel.tsx`。

- `web/src/pages/vps-detail/VPSIPQualitySection.tsx`
  - 改为默认页底部 `IP 质量概况`。
  - 保留完整报告 link。
  - 去掉评审说明文案。

- `web/src/index.css`
  - 按当前实际样式入口新增 V17 BEM 样式。
  - 收窄作用域到 `.vps-detail-page` 下的新 block，避免影响其它页面。
  - 删除或停止依赖旧默认页样式时谨慎处理，避免 IP 质量独立页和其它资产页面被误伤。

- `web/src/pages/VPSDetailPage.test.tsx`
  - 更新默认页断言和 modal 标题。
  - 移除对旧大块 `资产判断`、`下一步动作`、`成本卡片`、`快速创建订阅`、`创建并接入 agent` 等默认 UI 的断言。

### 新增或继续修改文件

- `web/src/pages/vps-detail/vpsDetailOverviewModel.ts`
  - V17 默认页 presenter。
  - 输入：`detail`、`timeline`、`primarySubscription`、`activeSubscription`、`subscriptionsError`、`services`、`domains`、`ipQuality`、`ipQualityError` 和 action callbacks。
  - 输出：综合基础信息字段、判断摘要、情境工作区、关联概览项、单机台账、IP 质量概况。
  - 继续扩展 `judgement.primaryAction`，承载 `处理取消/退役`。
  - 纯函数，不依赖 React，不发请求，便于单测或页面测试间接验证。

- `web/src/pages/vps-detail/VPSDetailOverviewPanel.tsx`
  - 渲染 `VPS 综合基础信息`。
  - 消费 presenter 生成的字段与判断摘要。
  - 在 `当前判断` 内渲染状态动作按钮，例如 `处理取消/退役`。
  - 顶部显示 `调整决策`、`基础资料`、`更多`、`返回列表`。
  - `更多` 菜单不渲染 `取消/退役`。

- `web/src/pages/vps-detail/VPSRelatedOverview.tsx`
  - 渲染 `关联概览`。
  - 自适应多列，一行能放多少放多少，不固定 2x2。
  - 标题承担详情入口；不显示 `查看` / `管理` 通用按钮。
  - 订阅、监控、IP 质量、服务、域名、资产历史分别作为 6 个概览项渲染。

- `web/src/pages/vps-detail/VPSContextActionPanel.tsx`
  - 只在 `model.contextAction` 非空时渲染。
  - 展示最高优先级问题的一行标题、一行短原因和主/次操作。
  - 稳定期不渲染，不占位。

- `web/src/pages/vps-detail/VPSSingleMachineLedger.tsx`
  - 渲染 `单机台账`。
  - 展示运维字段、近期记录、承载清单和关键变化。
  - 不做第二个导航页，不写长说明。

### 不新增文件

- 不新增 page-local CSS 文件。
- 不新增外部页面。
- 不新增 API client。
- 不新增全局 store。

## Presenter 设计

### `buildVPSDetailOverviewModel`

建议签名：

```ts
export type VPSDetailOverviewModelInput = {
  detail: VPSAssetDetail
  timeline: VPSTimeline
  primarySubscription: SubscriptionRecord | null
  activeSubscription: SubscriptionRecord | null
  subscriptionLoadFailed: boolean
  subscriptionError: string | null
  services: AssetServiceRecord[]
  domains: AssetDomainRecord[]
  ipQuality: VPSIPQualityReport | null
  ipQualityError: string | null
}

export function buildVPSDetailOverviewModel(input: VPSDetailOverviewModelInput): VPSDetailOverviewModel
```

输出职责：

- `facts`：综合基础信息字段。
- `judgement`：右侧短判断，最多三行：`决策`、`续费`、`动作`。
- `judgement.primaryAction`：当前判断里的状态动作；取消/退役状态时为 `处理取消/退役`，稳定期为 `null`。
- `contextAction`：最高优先级情境工作区；稳定期为 `null`。
- `relatedItems`：关联概览项。
- `ledger`：单机台账数据。
- `ipOverview`：默认页底部 IP 质量概况数据。

### 默认字段

`facts` 只包含默认页应展示字段：

- Provider。
- 地区 / 数据中心。
- 产品规格。
- 访问：`ssh_host || ipv4 || ipv6 || display_name` + `ssh_port`。
- 操作系统。
- 生命周期。
- 使用状态。
- 续费决策。
- 订阅：金额/周期 + 续费/到期。
- 监控：实例数量 + 健康摘要。
- IP 质量一句话摘要。

不进入默认字段：

- VPS ID。
- Provider ID。
- 订单号。
- IPv6 明细。
- SSH 用户。
- 虚拟化。
- 标签全集。
- 备注全文。
- 归档时间。

这些字段只进 `基础资料` modal。

### 续费/到期文案 helper

新增 helper，避免散落：

```ts
export function renewalDueLabel(subscription: SubscriptionRecord | null): string {
  if (!subscription?.renew_at && !subscription?.ends_at) return '尚无续费日'

  const targetDate = subscription.auto_renew_cancelled
    ? subscription.ends_at ?? subscription.renew_at
    : subscription.renew_at
  const days = daysUntilDate(targetDate)
  const unitLabel = relativeDayMonthLabel(days)

  if (subscription.auto_renew_cancelled) {
    return `已取消自动续费 · ${unitLabel}到期`
  }
  return `${unitLabel}续费`
}

function relativeDayMonthLabel(days: number | null): string {
  if (days == null) return '未知时间'
  if (days < 0) return `已过期 ${Math.abs(days)} 天`
  if (days === 0) return '今天'
  if (days <= 30) return `${days} 天后`
  return `${Math.ceil(days / 30)} 个月后`
}
```

说明：

- `> 30 天` 显示月。
- `> 1 年` 仍显示月，例如 `14 个月后续费`。
- 已取消自动续费用 `到期`，不是 `续费`。
- `ends_at` 缺失时退回 `renew_at`。

### 取消/退役状态动作

`取消/退役` 不进入中部情境工作区，也不进入更多菜单。它由 `judgement.primaryAction` 承载：

```ts
type VPSJudgementModel = {
  tone: VPSOverviewTone
  rows: Array<{ label: string; value: string }>
  primaryAction: VPSOverviewAction | null
}
```

显示条件：

- `renewal_decision` 为 `migrate` / `cancel` / `auto_renew_cancelled`。
- 或 `lifecycle_status` 为 `to_migrate` / `to_cancel` / `cancelled`。
- 或已加载 cancellation preview 且 `warnings` / `blockers` 非空。

隐藏条件：

- 稳定 active / keep / normal 状态。
- 未加载 cancellation preview 且没有取消、迁移、已取消自动续费等状态信号。

行为：

- `动作` 行显示 `取消/退役`。
- `primaryAction.label` 显示 `处理取消/退役`。
- 点击调用 `openDrawer('cancellation')`，触发现有 cancellation preview 懒加载。
- 不直接提交 `applyVPSCancellation`。
- `workbench=cancellation` 深链继续打开 modal，即使默认状态下不展示入口。

### 情境工作区优先级

只渲染一个最高优先级问题：

1. 监控严重/告警异常。
2. 续费 7 天内或已过期。
3. 已取消自动续费且临近到期，但不走取消/退役；优先提示 `调整决策` 或 `延长有效期`。
4. 成功加载后缺 active 订阅。
5. 未关联监控实例。
6. IP 质量高风险、失败或过期。
7. 关键基础字段缺失。
8. 稳定期：`null`。

取消/退役 blockers 只影响 `judgement.tone` 和 `judgement.primaryAction`，不生成中部情境工作区，避免页面重复出现两个处理入口。

情境工作区文案限制：

- 一行标题。
- 一行短原因。
- 一个主 CTA。
- 最多两个次级入口。

稳定期不渲染情境工作区，不占位。

### 关联概览项

统一结构：

```ts
export type VPSRelatedOverviewItem = {
  key: 'subscription' | 'monitoring' | 'ip-quality' | 'services' | 'domains' | 'history'
  title: string
  tone: 'normal' | 'notice' | 'alert' | 'critical'
  primary: string
  secondary?: string
  titleAction: { kind: 'link'; to: string } | { kind: 'modal'; mode: NonNullable<VPSDetailModalMode> }
  quickActions: Array<{
    label: string
    mode?: NonNullable<VPSDetailModalMode>
    to?: string
    variant?: 'secondary' | 'ghost'
  }>
}
```

具体项：

- 订阅：
  - 标题：`订阅`
  - titleAction：`/subscriptions?vps_id=<id>`
  - quickActions：`创建/更新订阅`、`延长`
  - 失败：`订阅证据暂不可用`
  - 无 active 订阅：`未记录当前订阅`

- 监控：
  - 标题：`监控观测`
  - 0 个实例：titleAction 打开 `monitoring-instance-evidence`
  - 1 个实例：titleAction 跳转 `/monitoring/<id>`
  - 多个实例：titleAction 打开 `monitoring-instance-evidence`
  - quickActions：`接入/升级`、`关联`

- IP 质量：
  - 标题：`IP 质量`
  - titleAction：`/vps/<id>/ip-quality`
  - quickActions：无
  - 显示评分/风险/可用概况，失败或无报告诚实显示。

- 服务：
  - 标题：`服务`
  - titleAction：打开 `services-detail` modal，标题为 `服务详情`。
  - quickActions：`新增服务`
  - 0 服务不默认升权为风险。

- 域名：
  - 标题：`域名`
  - titleAction：打开 `domains-detail` modal，标题为 `域名详情`。
  - quickActions：`新增域名`
  - 0 域名不默认升权为风险。

- 资产历史：
  - 标题：`资产历史`
  - titleAction：打开 `timeline-detail`
  - quickActions：`记录`
  - 显示最近一条经验/决策/价格变化摘要。

## 组件实施方案

### 1. `VPSDetailOverviewPanel`

职责：

- 取代旧 `VPSDetailHero` 默认渲染。
- 展示 `VPS 综合基础信息`。
- 展示短字段判断摘要。
- 承载顶部操作。

布局：

- 外层：`section.page-panel.vps-detail-overview`。
- 顶部：名称、状态 badges、操作区。
- 主体：左侧事实 grid，右侧 `当前判断` 或 `判断摘要`。
- 不再显示 provider/地区/IP pills，避免与事实 grid 重复。
- 不再显示顶部详情入口 chip 行，避免与关联概览重复。

顶部操作：

- `调整决策`：打开 `decision` modal。
- `基础资料`：打开 `facts-detail` modal。
- `更多`：保留 `details.watchtower-actions-menu` 样式。
- `VPS 列表`：保留链接。

更多菜单文案：

- `记录经验`
- `组合决策`
- `创建/更新订阅`
- `延长有效期`
- `接入/升级 agent`
- `关联已有监控实例`
- `新增服务`
- `新增域名`
- `归档 VPS` 或 `恢复为闲置`

`取消/退役` 从更多菜单移除。它只在 `model.judgement.primaryAction` 非空时显示在 `当前判断` 内。

### 2. 情境工作区

使用新增 `VPSContextActionPanel.tsx` 渲染，`VPSDetailPage.tsx` 只负责把 `model.contextAction` 和 click/link handlers 传入。

渲染规则：

- `model.contextAction === null` 时不渲染。
- 非空时渲染为窄 `page-panel` 或综合面板下方的小行动块。
- 不出现固定标题 `下一步动作`。
- 不渲染旧 `VPSDecisionBoard` 大面板。

### 3. `VPSRelatedOverview`

职责：

- 常驻在综合基础信息下方。
- 展示订阅、监控、IP 质量、服务、域名、资产历史。
- 每项标题可点击。
- 不显示 `查看` / `管理` 通用按钮。

交互：

- 标题是 `<Link>` 或 `<button>`，根据 `titleAction.kind` 决定。
- 快捷动作只保留对象独有动作。
- action button 必须阻止触发卡片标题行为，避免嵌套点击混乱。

响应式：

- 使用 `grid-template-columns: repeat(auto-fit, minmax(...))`。
- 卡片数量 5、7、8 时自然流动。
- 720px 以下单列，不产生页面整体横向滚动。

### 4. `VPSSingleMachineLedger`

职责：

- 让 VPS 详情页不是纯中转页。
- 展示单台 VPS 的近期记录、承载信息和维护事实。

内容：

- `运维字段`：用途/标签/备注状态/重要性，短字段化。
- `近期记录`：最近 3 条经验或决策记录。
- `承载清单`：最多 3 条服务/域名。
- `关键变化`：续费决策、价格、IP、规格变化中最近 1 到 3 条；无变化则隐藏。

交互：

- `记录经验` 打开 `experience` modal。
- 近期记录行点击打开 `timeline-detail` modal。
- 服务/域名详情打开对应 modal。
- 不写长说明，不渲染大面积空态。

### 5. `VPSIPQualitySection`

保留 `VPSIPQualitySection` 文件名和导出名，降低 import churn；只把默认页 UI 标题和内容改为 `IP 质量概况`。

默认页展示：

- 评分/结论。
- 风险信号数量和最高风险。
- 服务解锁概览。
- 证据覆盖或报告时间。
- 完整报告入口 `/vps/:vpsId/ip-quality`。

删除：

- `摘要只保留关键质量结论；完整 provider...` 这类评审说明文案。
- `未发现 proxy / VPN / abuse 等负面信号` 这类过长默认说明可压缩为 `无明显负面信号`。

失败/空态：

- `报告暂不可用`：显示 error，保留报告入口。
- `尚无 IP 质量报告`：短空态，不写长教学文案。

### 6. Modal 收敛

用户可见标题改为：

- `decision` -> `调整决策`
- `cancellation` -> `取消/退役`
- `facts` -> `编辑基础资料`
- `subscription` -> `创建/更新订阅`
- `validity-extension` -> `延长有效期`
- `monitoring-instance-create` -> `接入/升级 agent`
- `monitoring-instance-link` -> `关联已有监控实例`
- `experience` -> `记录经验`
- `service` -> `新增服务`
- `domain` -> `新增域名`
- `monitoring-instance-evidence` -> `监控观测`
- `services-detail` -> `服务详情`
- `domains-detail` -> `域名详情`
- `timeline-detail` -> `资产历史`
- `facts-detail` -> `基础资料`

CSS class：

- 当前 modal body wrapper 是 `.vps-detail-drawer`。
- 实施时改为 `.vps-detail-modal`，并同步 CSS。
- 内部 state 可以先保留 `activeDrawer` 以降低重命名风险，但新增代码和用户可见 class 不继续使用 drawer 语义。

禁止：

- 不使用抽屉组件。
- 不点击后在页面内插入详情区域。
- 不在 modal 中再打开第二层 modal，除非现有确认组件已是独立安全流程。

## 页面装配替换

`VPSDetailPage.tsx` 默认 return 从当前结构：

1. `VPSDetailHero`
2. subscription error / decision notice
3. `VPSDecisionBoard`
4. `VPSIPQualitySection`
5. `成本卡片`
6. lifecycle card / confirmation
7. modal

替换为：

1. `VPSDetailOverviewPanel`
2. 顶部短反馈：只显示全局错误/成功，且不长期占位
3. 可选情境工作区
4. `VPSRelatedOverview`
5. `VPSSingleMachineLedger`
6. `VPSIPQualitySection`，标题 `IP 质量概况`
7. lifecycle confirmation modal/card，如现有归档流程需要保留
8. 统一 `Modal`

删除默认渲染：

- `VPSDecisionBoard`
- 独立 `成本卡片`
- 顶部重复详情入口
- 页面提示/说明型文字

保留逻辑但迁移入口：

- `buildVPSDecisionModel` 仍参与 presenter。
- 成本字段进入订阅关联项和综合信息订阅字段。
- 决策 notice 与 action link 保留为短反馈。
- lifecycle confirmation 继续使用 `ActionConfirmationModal`。

## 样式方案

### 落点

按当前代码实际情况，新增样式写入 `web/src/index.css`，不要新建局部 CSS 文件。

### 新 BEM block

- `.vps-detail-overview`
- `.vps-detail-overview__header`
- `.vps-detail-overview__facts`
- `.vps-detail-overview__fact`
- `.vps-detail-overview__judgement`
- `.vps-detail-context-action`
- `.vps-related-overview`
- `.vps-related-overview__grid`
- `.vps-related-card`
- `.vps-related-card__title`
- `.vps-related-card__actions`
- `.vps-single-ledger`
- `.vps-single-ledger__grid`
- `.vps-ledger-block`
- `.vps-detail-modal`
- 继续复用 `.vps-ip-quality-summary`

### 样式约束

- 颜色使用现有变量：`--panel-bg`、`--panel-border`、`--surface`、`--surface-elevated`、`--text-primary`、`--text-secondary`、`--text-muted`、`--accent`、`--color-state-*`。
- 间距使用 `--space-*`。
- 圆角使用 `--radius-*`。
- 状态色派生使用 `color-mix(in srgb, var(--color-state-*) NN%, transparent)`。
- 不写新 hex 色，不写 page-local CSS import。
- 小屏按钮允许换行，禁止整体页面横向滚动。

## 测试方案

### 更新 `VPSDetailPage.test.tsx`

必须新增或更新以下断言：

1. 默认页渲染 V17 主体结构：
   - `VPS 综合基础信息`
   - `关联概览`
   - `单机台账`
   - `IP 质量概况`

2. 默认基础信息可见：
   - Provider。
   - 地区/数据中心。
   - 产品规格。
   - 访问地址与 SSH 端口。
   - 操作系统。
   - 生命周期。
   - 使用状态。
   - 续费决策。
   - 监控实例数。
   - IP 质量一句话摘要。

3. 旧默认大块不再出现：
   - `资产判断` 不作为默认 section heading 出现。
   - `下一步动作` 不作为常驻 section 出现。
   - `成本卡片` 不出现。
   - `快速创建订阅` 不出现。
   - `创建并接入 agent` 不出现。
   - `取消/退役工作台` 不出现。

4. 关联概览行为：
   - 订阅标题链接到 `/subscriptions?vps_id=<id>`。
   - 监控单实例标题链接到 `/monitoring/<id>`。
   - IP 质量标题或完整报告入口链接到 `/vps/<id>/ip-quality`。
   - 服务标题打开 `服务详情` modal。
   - 域名标题打开 `域名详情` modal。
   - 资产历史标题打开 `资产历史` modal。
   - 不出现 `查看` / `管理` 通用按钮。

5. Modal-only 行为：
   - `基础资料` 打开 `role="dialog"` 且 name 为 `基础资料`。
   - `调整决策` 打开 `调整决策`。
   - `创建/更新订阅` 打开 `创建/更新订阅`。
   - `接入/升级 agent` 在 0 active link 场景打开 `接入/升级 agent`。
   - `监控观测` 打开 `监控观测`。
   - `服务详情`、`域名详情`、`资产历史` 都通过 dialog 打开。
   - 关闭 modal 后 URL 中 `workbench` 被清理。

6. 监控接入分流回归：
   - 0 active link：`workbench=monitoring` 打开 `接入/升级 agent` modal。
   - 1 active link：`workbench=monitoring` 跳转 `/monitoring/<id>?onboarding=1&return_vps=<vpsId>`，不调用创建 API。
   - 多 active links：打开 `监控观测` modal，不显示创建入口。

7. 订阅失败与缺订阅区分：
   - `listSubscriptions` 失败时显示 `订阅证据暂不可用` 或实际错误，不显示 `未记录当前订阅`。
   - 成功返回空数组时才显示缺订阅，并提供 `创建/更新订阅`。

8. IP 质量回归：
   - 有 summary 时显示评分/风险/解锁概况。
   - `getVPSIPQuality` 失败时显示 `报告暂不可用` 或错误，仍保留完整报告入口。
   - 无 summary 时显示短空态。

9. 取消/退役：
   - `workbench=cancellation` 打开 `取消/退役` dialog。
   - 取消/迁移/已取消自动续费等状态下，顶部 `当前判断` 显示 `动作：取消/退役` 和按钮 `处理取消/退役`。
   - 点击 `处理取消/退役` 打开 `取消/退役` dialog，并懒加载 cancellation preview。
   - 稳定 active / keep 状态下，顶部 `当前判断` 不显示 `处理取消/退役`，更多菜单也不显示 `取消/退役`。
   - 中部不再渲染 `取消/退役待处理` 或任何取消/退役情境工作区。
   - 确认执行后继续显示审计结果或短反馈。

### 新增 presenter 单测

新增：

- `web/src/pages/vps-detail/vpsDetailOverviewModel.test.ts`

覆盖：

- `renewalDueLabel` 天/月/取消自动续费。
- 订阅失败不等于缺订阅。
- 情境工作区优先级只返回一个最高优先级。
- 取消/退役状态只生成 `judgement.primaryAction`，不生成 `contextAction`。
- 稳定状态下 `judgement.primaryAction === null`。
- IP 质量无报告/失败/有报告三种摘要。

### 验证命令

从 `web/` 目录运行：

```bash
npm run lint
npm run test -- --run VPSDetailPage
npm run test -- --run VPSIPQualityPage
npm run build
```

如果 targeted Vitest 参数在当前版本匹配异常，则使用：

```bash
npm run test -- --run src/pages/VPSDetailPage.test.tsx
npm run test -- --run src/pages/VPSIPQualityPage.test.tsx
```

最终提交前可运行：

```bash
make verify-web
```

## 浏览器核查方案

实现后启动 dev server：

```bash
cd web
npm run dev -- --host 127.0.0.1
```

需要人工核查：

- 桌面宽度：综合基础信息、关联概览、单机台账、IP 质量概况均可扫描。
- 720px 以下：字段纵向排列，按钮换行正常，不出现页面整体横向滚动。
- houfeng dark、houfeng light、classic dark：颜色均来自当前主题变量，不能像 mockup 白底硬编码。
- 点击 `基础资料`、`调整决策`、`创建/更新订阅`、`接入/升级 agent`、`监控观测`、`服务详情`、`域名详情`、`资产历史`、`取消/退役` 均打开居中 modal。
- 关闭 modal 后页面不新增局部详情区域。
- 稳定期不出现大块 `下一步动作`。

## 实施顺序

### Phase 2.0 前置

- 读取 `.trellis/spec/web/index.md`、`component-conventions.md`、`state-and-data.md`、`styling-guidelines.md`、`quality-guidelines.md`。
- 运行 `git status --short --branch`。
- 如 hooks 未启用，运行 `sh scripts/setup-git-hooks.sh`。
- 不再执行 `task.py start`，当前 task 已是 `in_progress`。

### Phase 2.1 Presenter 与 helper

1. 先在 `web/src/pages/vps-detail/vpsDetailOverviewModel.test.ts` 写失败用例：
   - cancellation 状态时 `model.judgement.primaryAction?.label === '处理取消/退役'`。
   - cancellation 状态时 `model.contextAction === null`。
   - stable keep 状态时 `model.judgement.primaryAction === null`。
2. 更新 `vpsDetailOverviewModel.ts`。
3. 增加 `renewalDueLabel` / `relativeDayMonthLabel`。
4. 复用 `buildVPSDecisionModel` 生成普通 `contextAction` 和 `judgement`。
5. 将 cancellation predicate 映射到 `judgement.primaryAction`，不要映射到 `contextAction`。
6. 确保 presenter 无请求、无 React hook。

验收：

- `npm run test -- --run src/pages/vps-detail/vpsDetailOverviewModel.test.ts`
- `npm run lint` 无 unused type / any。

### Phase 2.2 顶部综合基础信息

1. 先在 `VPSDetailPage.test.tsx` 写/更新失败用例：
   - cancellation 状态下能看到 `处理取消/退役`。
   - stable 状态下更多菜单内没有 `取消/退役`。
   - 点击页面其他位置可关闭更多菜单。
2. 新增或更新 `VPSDetailOverviewPanel.tsx`。
3. 停止默认渲染旧 `VPSDetailHero`。
4. 顶部操作改为 `调整决策`、`基础资料`、`更多`、`VPS 列表`。
5. `更多` 菜单收纳低频操作，但不包含 `取消/退役`。
6. 在 `当前判断` 内渲染 `model.judgement.primaryAction`。
7. 删除旧 provider/地区/IP pills。

验收：

- 页面测试能找到 `VPS 综合基础信息`。
- 旧 `处理决策`、`创建订阅`、`创建并接入 agent` 默认按钮不再出现。
- cancellation 状态的 `处理取消/退役` 位于 `当前判断` 内。
- stable 状态下默认页和更多菜单均无 `取消/退役`。

### Phase 2.3 关联概览

1. 新增 `VPSRelatedOverview.tsx`。
2. 渲染订阅、监控、IP 质量、服务、域名、资产历史。
3. 标题实现 link/modal action。
4. 快捷动作只保留对象独有动作。
5. 不渲染 `查看` / `管理` 通用按钮。

验收：

- 页面测试覆盖各标题入口。
- `getByText('查看')` / `getByText('管理')` 不因关联卡通用按钮出现。

### Phase 2.4 单机台账

1. 新增 `VPSSingleMachineLedger.tsx`。
2. 展示运维字段、近期记录、承载清单、关键变化。
3. 空数据短显示或隐藏。
4. 记录经验、历史详情、服务详情、域名详情入口都走 modal。

验收：

- 页面测试能找到 `单机台账`。
- 最近记录或空态不出现长段说明。

### Phase 2.5 IP 质量概况

1. 调整 `VPSIPQualitySection.tsx` 标题为 `IP 质量概况`。
2. 删除说明型长文案。
3. 保留完整报告入口。
4. 区分有报告、无报告、读取失败。

验收：

- `VPSDetailPage.test.tsx` 断言完整报告 href。
- `VPSIPQualityPage.test.tsx` 仍通过，独立报告页未被破坏。

### Phase 2.6 页面装配与 modal 标题

1. 在 `VPSDetailPage.tsx` 中构建 `overviewModel`。
2. 替换 return 中默认 page sections。
3. 移除默认 `VPSDecisionBoard` 和 `成本卡片`。
4. 更新 `monitoringAgentActionLabel` 为 `接入/升级 agent`。
5. 将 `shouldExposeCancellationWorkbench(detail, preview)` 只用于 presenter/action 显示，不再作为更多菜单项开关。
6. 确保 `openDrawer('cancellation')` 仍负责懒加载 preview，深链 `workbench=cancellation` 仍可打开 modal。
7. 更新 `drawerTitle()` 为新标题。
8. `.vps-detail-drawer` 改为 `.vps-detail-modal`。
9. 保留现有 form submit、刷新和 error handling。

验收：

- modal 标题测试全部更新。
- 深链测试仍通过。
- 取消/退役没有重新出现在中部 `VPSContextActionPanel`。

### Phase 2.7 样式

1. 在 `web/src/index.css` 中新增 V17 block 样式。
2. 使用现有 token 变量和 BEM。
3. 调整移动端 media rules。
4. 删除不再使用的旧 VPS 默认页样式时要谨慎；无法确认无影响时先保留未用样式，后续清理。

验收：

- `npm run build` 无 CSS/TS 错误。
- 浏览器核查无横向溢出。

### Phase 2.8 测试更新与回归

1. 更新 `VPSDetailPage.test.tsx` 所有旧文案断言。
2. 保留关键业务回归。
3. 跑 targeted tests。
4. 跑 lint/build。

验收：

- `npm run lint`
- `npm run test -- --run VPSDetailPage`
- `npm run test -- --run VPSIPQualityPage`
- `npm run build`

## 风险与处理

- `VPSDetailPage.tsx` 已经很大：新增 presenter 和 section 组件，避免继续把 JSX 堆回主文件。
- 当前 spec 与实际样式文件有漂移：按当前代码写入 `index.css`，同时不违反 token/BEM 约束。
- `VPSDecisionBoard` 可能被测试或未来页面引用：本轮先停止在默认页渲染，不急于删除文件。
- `activeDrawer` 命名有历史包袱：用户可见 UI 和 CSS 先消除 drawer 语义；变量是否彻底重命名可作为实施中低风险偿还。
- `VPSIPQualitySection` 与独立 IP 质量页概念相近：默认页只调整摘要组件，不能删除 `/vps/:id/ip-quality` route 或报告页 presenter。
- 订阅失败场景容易误判：所有 presenter 分支必须把 `subscriptionLoadFailed` 与 `primarySubscription === null` 分开。
- 监控接入已有防重复 contract：不得因为改入口文案而恢复无条件创建监控实例。
- 服务和域名在关联概览中分成两项，分别打开 `服务详情` 与 `域名详情` modal；不能合并成一个含糊入口，也不能因此新增页面内展开区域。

## 回滚点

- 如果默认页装配风险过大，优先保留旧 submit handlers 和 modal content，只回滚 page section 替换。
- 如果样式影响其它页面，回滚新增 CSS block 或加 `.vps-detail-page` 作用域收窄。
- 如果 presenter 逻辑导致测试大面积不稳定，保留组件结构，先把复杂派生逻辑内联回 `VPSDetailPage` 过渡，但不得恢复旧默认大块。
- 如果 `VPSDecisionBoard` 删除引发引用风险，保留文件不渲染。

## 方案自审

### 覆盖性

- 已覆盖 V17 四段默认结构。
- 已覆盖 modal-only 要求。
- 已覆盖 IP 质量默认页概况和完整报告入口。
- 已覆盖订阅、监控、服务/域名、历史、基础资料、取消/退役、组合决策的入口分级。
- 已覆盖稳定期不渲染大块下一步动作。
- 已覆盖测试、lint、build、浏览器核查。

### 冲突检查

- 不再继续设计发散，符合用户“设计方案已经确定”的要求。
- 不开始生产代码，符合用户“完成实施方案并审查后再实施”的要求。
- 不复制 mockup CSS，符合生产视觉使用项目整体风格的要求。
- 不使用抽屉或页面内新增展开区，符合 modal-only 要求。
- 不重构外部页面，符合任务范围。

### 可行性

- 所需 API 和表单能力已存在。
- 主要工作是 presenter + 页面结构重排 + 样式 + 测试更新。
- 高风险点集中在 `VPSDetailPage.tsx` 和 `VPSDetailPage.test.tsx`，可按阶段逐步替换。
- 验证命令与当前 `web/package.json` 脚本一致。

### 无占位检查

- 本方案不含 `TODO`、`TBD`、`以后再说` 类型占位。
- “可选”只用于低风险实现路径选择，不作为验收缺口。
