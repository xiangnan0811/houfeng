# Design: IP质量驾驶舱页面与VPS摘要入口

## Architecture

本次是前端拆页与展示增强，不改变后端 contract。

- 路由层：在 `web/src/app/router.tsx` 增加 lazy route `/vps/:vpsId/ip-quality`，加载新页面 `VPSIPQualityPage`。
- 数据层：新页面直接调用 `getVPSIPQuality(vpsId)`。VPS 详情页继续沿用当前加载逻辑，避免摘要卡再发一次请求。
- 展示层：
  - `web/src/pages/vps-detail/VPSIPQualitySection.tsx` 改为摘要卡。
  - 新增 `web/src/pages/VPSIPQualityPage.tsx` 承载完整驾驶舱。
  - 为避免重复风险/服务标签逻辑，可在 `web/src/pages/vps-detail/ipQualityPresentation.ts` 提取纯展示 helper。
- 样式层：新增 BEM block 写入 `web/src/styles/pages.css`。不新增局部 CSS 文件。

## Data Flow

### VPS Detail

1. `VPSDetailPage` 已在初始数据加载中调用 `getVPSIPQuality(vpsId)`。
2. `VPSIPQualitySection` 接收 `report/error/vpsId`。
3. 摘要卡从 `report.summary` 和当前 provider/service 派生：
   - 风险 flags 数量与名称。
   - unlock success/blocked/partial/unknown 数量。
   - provider count / service count。
   - observed_at、ip_address、asn、organization、region。
4. 点击“查看完整 IP 质量报告”进入 `/vps/{vpsId}/ip-quality`。

### Full Page

1. `VPSIPQualityPage` 从 `useParams()` 读取 `vpsId`。
2. 调用 `getVPSIPQuality(vpsId)` 并管理 loading/error/success state。
3. 成功后渲染驾驶舱；失败或缺失使用 `PageState`。
4. 页面提供返回 `/vps/{vpsId}` 的链接。

## Derived Presentation Contracts

- Risk signal flags:
  - `is_proxy` -> Proxy
  - `is_tor` -> Tor
  - `is_vpn` -> VPN
  - `is_server` -> Server / Datacenter
  - `is_abuser` -> Abuse
  - `is_robot` -> Robot
- Risk severity:
  - `critical/high` -> critical/alert
  - `medium/moderate` -> notice
  - `low/clean/safe` -> normal
  - missing -> unknown
- Service unlock:
  - `unlocked` -> 解锁
  - `partial` -> 部分
  - `blocked` -> 受阻
  - missing/error/unknown -> 未知/异常
- Quality score:
  - 当前 API 没有后端评分字段，因此前端只能派生“展示评分”。
  - 评分用于 UI 排序和视觉提示，不写回后端，不作为资产决策 scoring source。
  - 派生规则必须简单透明：从 100 起，risk_level、proxy/vpn/tor/abuse/robot、blocked service、partial service 扣分；server/datacenter 本身不扣分或低权重提示。
- Database consistency:
  - 当前 normalized provider 数量有限，因此展示为“已归一字段一致性”。
  - 可基于 provider 的 usage/company/risk/region/signals 是否分歧派生百分比；provider 少于 2 时显示“样本不足”。
- Coverage:
  - provider 覆盖 = `provider_results.length / max(summary.provider_count, provider_results.length)`。
  - service 覆盖 = `service_unlocks.length / max(summary.unlockable_count, service_unlocks.length)`。
  - 当 summary count 为 0 时显示“未采集/未配置”，不伪造百分比。

## Empty / Error / Stale Behavior

- `report.summary` 为空：展示“尚无可展示的 IP 质量事实”，说明 agent 低频采集后显示。
- `report.latest_report.status === failure` 但 summary 为空：只在诊断中提示最近失败，不把 `ip_address` 展示为真实出口 IP。
- provider 为空：风险矩阵显示未知/未覆盖。
- service 为空：服务解锁显示未配置或暂无结果。
- history 为空：历史区显示“暂无历史变化”。
- `summary.stale`：摘要和完整页均显著提示“数据过期”。
- `summary.ambiguous`：提示“归属不唯一，需复核”，不输出强风险结论。

## Routing And Navigation

- 新路由：`/vps/:vpsId/ip-quality`。
- VPS 详情摘要卡链接：`/vps/${encodeURIComponent(vpsId)}/ip-quality`。
- 完整页返回链接：`/vps/${encodeURIComponent(vpsId)}`。
- 该页面作为 VPS 子资源详情页，不需要侧边栏新增主入口。

## Compatibility

- 不改变 API、Go 类型、DB migration 或 agent payload。
- 不改变资产决策读模型。
- 若未来 center 提供更多 provider/script 字段，可扩展 `VPSIPQualityReport` 类型和完整页表格，而 VPS 详情摘要卡保持稳定。

## Tradeoffs

- 选择“独立页面 + 摘要卡”而不是“详情页完整嵌入”：避免 VPS 详情页过长，并为性能/路由报告建立可复用模式。
- 选择“前端派生质量评分”而不是“后端评分”：本次避免后端变更；但页面必须标明这是基于当前归一字段的展示判断，资产决策仍以后端 evidence 为准。
- 选择“复用现有 API”而不是“新增聚合 API”：降低上线风险；后续如果字段增多或页面性能受影响，再考虑专用 read model。

## Testing Strategy

- API 层不变，已有 `getVPSIPQuality` 测试保留。
- 新增 `VPSIPQualityPage.test.tsx` 覆盖：
  - 成功加载完整驾驶舱。
  - loading/error/empty。
  - provider/service/history 为空时降级。
  - 返回 VPS 详情链接。
- 更新 `VPSDetailPage.test.tsx` 或新增 `VPSIPQualitySection.test.tsx` 覆盖：
  - 摘要卡展示关键结论。
  - “查看完整 IP 质量报告”链接路径正确。
  - 详情页不再展示完整 provider/service 大矩阵。
- 更新 router 测试（如必要）确保新 route 被注册。
