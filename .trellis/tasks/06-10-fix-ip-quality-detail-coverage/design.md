# 修复 IP 质量详情展示与采集覆盖 - 技术设计

## Design Goal

本任务同时修复两个层面：

1. 页面体验：去掉用户不需要的说明文字，修正服务解锁统计布局、未知状态文案、provider 表格行高和风险列布局。
2. 数据质量：追查 `各 IP 数据库判断` 大量空字段的根因，保证默认可安全采集源的字段被正确解析、上报、保存和展示。

设计原则：主视图只展示可用于判断 IP 质量的事实；采集缺口、未配置来源和内部 probe 诊断保留在 coverage/diagnostics 中，不进入主判断表制造噪声。

## Current Evidence

代码定位：

- 前端详情页：`web/src/components/ip-quality/IPQualityDashboard.tsx`
- 展示派生 helper：`web/src/components/ip-quality/ipQualityPresentation.ts`
- IP 质量页面样式：`web/src/index.css` 中 `vps-ip-quality-dashboard__*`
- agent 默认 provider：`agent/ipquality/providers.go`
- agent 默认 service probe：`agent/ipquality/services.go`
- agent collector/orchestration：`agent/ipquality/collector.go`
- IP 质量合同：`.trellis/spec/backend/ip-quality-contract.md`

已确认现状：

- 三段用户要求删除的说明文案仍硬编码在 `IPQualityDashboard.tsx`。
- `safeDiagnosticText` 已过滤部分内部词，但没有过滤完整英文句子 `safe default probe is not available without optional service configuration`。
- `services.go` 的 Disney+ 默认 probe 会上报该英文 error summary；前端主视图应映射为中文中性说明。
- 服务统计使用通用 `.asset-context-inline`，需要本页专用 class 保证桌面横排右对齐。
- provider 表格当前直接渲染全部 `provider_results`，包括 optional `not_configured` 来源，因此会出现大量空字段/未评级/未知/无用户证据行。
- provider 风险列用 badge + block `small`，风险值会换行并撑高行。

`209.33.173.4` 现场上游响应显示多个默认源实际有丰富信息：

- `ipapi.is`: `is_datacenter=true`、`company.type=hosting`、`location.country_code=JP`、ASN `63150`。
- `proxycheck.io`: `proxy=yes`、`type=VPN`、`risk=66`、`isocode=JP`。
- `ip2location.io`: `country_code=JP`、`region_name=Tokyo`、`is_proxy=false`、`asn=63150`。
- `ipwho.is`: `country_code=JP`、`connection.asn=63150`、`connection.org=BAGE CLOUD LLC`。
- `ipquery.io`: 当前返回 `US/Windstream`，与其他源分歧，应保留为分歧证据。

因此第 6 项不能只靠隐藏空值解决；必须补 parser fixture 测试，并按测试结果修 agent 解析。

## Frontend Design

### 文案删除

在 `IPQualityDashboard.tsx` 中删除三处说明：

- hero `section-heading__description`
- 风险信号矩阵右侧 `section-heading__meta`
- provider 表格右侧 `section-heading__meta`

保留 heading、eyebrow、采集 meta 和返回按钮。规则说明不再占据主视图空间。

### 服务解锁卡片

`serviceCardDescription` 负责把技术状态映射为用户语言：

- `unlocked`: “区域 XX 可用”或“服务可用”
- `partial`: “区域 XX 部分可用”或“部分内容可用”
- `blocked`: “区域 XX 受阻”或“服务解锁受阻”
- `unknown + unsupported_default_probe/skipped`: “默认探测暂不支持该服务”
- `unknown + not_configured`: “需要可选配置后才能检测”
- `failure`: “探测失败，未形成可靠结论”
- 其他 unknown: “本轮未形成可靠解锁结论”

过滤规则必须覆盖：

- `safe default probe is not available without optional service configuration`
- `default_probe`
- `unsupported_default_probe`
- `not_configured`
- `optional service configuration`
- `optional_service_probe`
- source/probe 技术名

agent 层可以继续保留短英文诊断用于开发排查，但前端主卡片不得直接展示。

### 服务统计布局

给统计容器新增本页专用 class，例如：

```tsx
<div className="vps-ip-quality-dashboard__service-stats" aria-label="服务解锁状态统计">
```

CSS 约束：

- `display:flex`
- `flex-direction:row`
- `flex-wrap:wrap`
- `justify-content:flex-end`
- `align-items:center`
- `gap:var(--space-2)`
- 桌面不使用 column。
- 小屏只改为 `justify-content:flex-start`，不改为竖向 column。

### Provider 主表与采集缺口

主表应聚焦“有判断价值的来源”：

- 显示 default sources 的 success/failure 行。
- 显示已配置 optional/custom sources 的 success/failure 行。
- 不在主表显示 optional `not_configured` 行。
- optional `not_configured/skipped` 来源在采集完整性或诊断区用紧凑 chip/list 展示，例如“未配置来源：maxmind、ipinfo、ipregistry...”，避免污染主判断表。

失败的 default source 仍显示在主表，因为它解释默认覆盖缺口。其空字段列应展示短诊断，而不是“无用户证据”。

`providerEvidenceSignals` 需要区分：

- 成功且有 active 风险/context flag：显示对应 chip。
- 成功但没有风险信号：显示“未发现风险信号”或保持低权重 neutral。
- failure/skipped/not_configured：显示“采集失败 / 未检测 / 未配置”，不显示“无用户证据”。

### 风险列

风险列改为 inline 组合：

```tsx
<span className="vps-ip-quality-dashboard__risk-cell">
  <Badge ...>中风险</Badge>
  <span className="vps-ip-quality-dashboard__risk-score">66</span>
</span>
```

CSS：

- inline-flex
- nowrap
- align-items:center
- gap
- risk score 用 mono 小号文本

不要再用 block `small` 让风险值换行。

## Agent/Data Design

### Parser 回归

新增或扩展 `agent/ipquality/collector_test.go` / provider parser 测试，使用 `209.33.173.4` 的固定 JSON fixture，不做 live network test。

测试目标：

- `parseIPAPIISProvider` 从嵌套字段填充 `company_type=hosting`、`region_code=JP`、`is_server=true`、proxy/vpn/tor/abuser/robot false，并将 ASN/company/location 放入 `extra_json`。
- `parseProxycheckProvider` 解析 string bool：`proxy=yes` -> `is_proxy=true`；`risk=66` -> `risk_score=66` 和对应风险等级；`type=VPN` -> `usage_type=VPN`，并推导 `is_vpn=true` 或至少保留为 usage context。
- `parseIP2LocationProvider` 解析 `country_code`、`country_name`、`region_name`、`is_proxy=false`，并把 ASN/AS 字段保留到 `extra_json`。
- `parseIPWhoIsProvider` 解析 `country_code`、`country`、`connection` 上下文并保留 extra。
- `parseIPQueryProvider` 保留其返回的 US/Windstream 分歧，不覆盖其他 provider 的 JP/BAGE 结论。

### Center/API

优先假设 center schema 已支持当前 v2 字段，因为 v0.53.x 已有 provider `status/source_type/extra_json` 和 service `probe_status/source/extra_json`。执行阶段仍需检查：

- sync ingest 是否保存 parser 新填字段。
- repository/API 是否回读字段。
- web type 是否接收字段。

如果字段在 center 层被丢弃，按最小迁移/DTO 修复；如果已完整保存，不做无意义 schema 改动。

### 历史数据

不回填、不删除旧 report。新 parser 只影响新 agent 上报。页面对旧 report 应优雅展示缺口：

- 主表不展示 optional not_configured 空行。
- success 行缺字段显示“未返回”。
- diagnostics/coverage 解释采集缺口。

## Compatibility And Risk

- 不改变 API 路径。
- 不改变 IP 质量采集频率和 Settings contract。
- 不执行远程 shell。
- 不把 unknown/skipped/failure 统计为 blocked/unlocked 或负面风险。
- 如果 parser 修复导致新报告字段更丰富，历史报告仍按旧数据展示。

## Validation

代码级：

- `go test ./agent/ipquality`
- 如 center DTO 有改动，运行相关 center handler/store 测试。
- `cd web && npm run lint`
- `cd web && npm run test -- --run`
- `cd web && npm run build`
- `git diff --check`

视觉级：

- 启动 web 预览或项目现有 visual helper。
- 检查 IP 质量详情页桌面/移动：
  - 三段说明文字不存在。
  - 服务统计横排。
  - unknown 服务卡使用中性灰色且中文说明。
  - provider 主表没有大量 optional 未配置空行。
  - 风险列不换行撑高。
