# IP质量页面体验优化设计

## Overview

本任务只优化 IP 质量前端展示层，不改变 agent 采集、center 入库或 `/api/vps/:id/ip-quality` contract。用户已认可信息架构和布局方向，并补充要求：正式实现必须使用当前项目主题配色和页面氛围，不能沿用 demo 初版的独立蓝绿色板。

本设计以 `.superpowers/brainstorm/733632-1781022377/content/ip-quality-ux-demo.html` 的第二版为视觉参考：布局沿用已确认结构，配色收敛到 `web/src/index.css` 当前主题变量。

## Boundaries

- 修改范围：
  - `web/src/pages/vps-detail/VPSIPQualitySection.tsx`
  - `web/src/components/ip-quality/IPQualityDashboard.tsx`
  - `web/src/components/ip-quality/ipQualityPresentation.ts`
  - `web/src/index.css`
  - 相关同目录测试
- 不修改范围：
  - Go backend / agent / migrations
  - `web/src/lib/types.ts` 和 `web/src/lib/api.ts`
  - IP 质量 API response shape
  - 资产决策组合中枢页面

## UX Architecture

### VPS 详情页摘要

VPS 详情页只承担“快速判断 + 进入完整报告”的职责：

- header 右上角只保留 `查看完整 IP 质量报告` 链接。
- 不显示 `IPQualityBadge`、风险等级 badge、国家/地区 badge 或完整服务 chip 列表。
- 主体保留一张质量评分卡和 3 个紧凑指标：风险信号、服务解锁、采集覆盖。
- 服务解锁改为合并概览：`3 可用`、`2 受阻`、`1 部分`、`1 未知`，并最多列出重点异常服务，不在详情页左下角堆全部服务。
- 基础 IP / ASN / 组织等事实不在摘要主视图展开；完整上下文进入独立报告页。

### IP 质量详情页

详情页继续作为独立深度报告页面：

- hero/header 右上角只保留 `返回 VPS 详情`。
- 风险等级放到正文原因卡或指标卡中，不与返回按钮并列。
- 首屏结构为 `score + reasons + 2x2 metrics`，其中 2x2 指标保留风险信号、解锁可用、数据库一致性、采集完整性。
- 风险信号矩阵保留 6 个信号卡；Server/Datacenter 用上下文语义，不作为负面风险。
- Provider 表格保留逐库证据，但主表不展示长诊断文案。当前“证据说明”列改为“信号”或紧凑证据 chip，只展示 Proxy/VPN/Tor/Server/Abuse/Robot 等。
- 服务解锁矩阵右上统计横向右对齐。服务卡不展示 `probe_status`、`source`、`default_probe`、`latency` 等技术字段；只展示服务名、解锁状态、区域、解锁类型或用户可理解说明。
- unknown / 未检测 / 失败类状态使用 neutral/offline 弱化样式，避免白色或高亮造成误判。
- 原始 JSON、source、latency、错误详情仅可进入折叠诊断层，不进入主视图。

## Visual Contract

正式实现不得另起色板，必须复用当前项目变量和既有组件语义：

- 容器：`page-panel`、`panel-bg`、`surface`、`surface-elevated`、`border`、`shadow-soft`。
- 强调：`--accent` / `--accent-strong`，只用于入口、score 左边线、hero 顶线、eyebrow 或焦点。
- 状态：`Badge` tone 与 `--color-state-normal`、`--color-state-notice`、`--color-state-alert`、`--color-state-critical`、`--text-muted`。
- 表格：沿用 `data-table data-table--compact asset-table`，设置稳定 `table-layout: fixed` 和明确列宽。
- 字号/密度：使用当前 `--type-*` 和 `--space-*`。不引入新字体、不写独立大屏风格。
- CSS class 继续使用 `vps-ip-quality-summary__*` 和 `vps-ip-quality-dashboard__*` BEM 命名。

## Data Flow

现有 page 已获取 `VPSIPQualityReport` 并传入摘要/详情组件，本任务不改变数据流：

- `VPSIPQualitySection` 从 `report.summary`、`report.provider_results`、`report.service_unlocks` 派生摘要。
- `IPQualityDashboard` 从完整 report 派生 score、risk reasons、provider table、service cards、coverage、history 和 diagnostics。
- 新增或调整的派生 helper 应放在 `ipQualityPresentation.ts`，保持组件 JSX 清晰。

## Presentation Helpers

需要新增或调整的 helper：

- `strongestRiskFlags` 或等价导出：只返回负面命中信号，数量限制用于摘要。
- `visibleUnlockHighlights`：从 blocked、partial、unknown 中挑选少量重点服务，避免摘要页罗列全部服务。
- `providerEvidenceSignals`：把 provider active flags 转成紧凑 chip 数据；无 active flag 时返回 `无用户证据`，而不是返回 `error_summary`。
- `serviceCardDescription`：把 unlock status、region、unlock_type、error_summary 归一为用户可理解说明，过滤 `default_probe`、`not_configured` 等内部文本。

## Compatibility

- API contract 不变，旧报告数据可直接渲染。
- `latest_report.raw_json`、provider `extra_json`、service `extra_json` 保留，但主视图不展开。
- 对失败、跳过、未配置状态保持可诊断但低权重展示，避免用户误认为数据缺失就是 IP 质量结论。

## Validation

- 浏览器 demo 已在 `http://localhost:53326` 展示并检查桌面/移动端。
- 正式实现后至少运行：
  - `cd web && npm run lint`
  - `cd web && npm run test -- --run`
  - `cd web && npm run build`
- 相关测试重点：
  - VPS 摘要 header 只出现完整报告入口，不出现风险 badge。
  - 摘要页服务解锁使用概览，不渲染全部服务 chip。
  - 详情页 header 只出现返回按钮。
  - Provider 表格不展示长 `error_summary` / `not_configured` 到证据列。
  - 服务卡不展示 `default_probe` / source / probe status badge。
  - unknown 服务使用 neutral 文案和样式。

## Rollback

本任务仅涉及前端展示。若上线后发现展示误导或布局问题，可回滚相关 React/CSS 改动，不影响 IP 质量数据采集、入库或历史数据。
