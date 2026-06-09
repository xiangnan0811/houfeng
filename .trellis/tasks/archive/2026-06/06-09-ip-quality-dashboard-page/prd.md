# IP质量驾驶舱页面与VPS摘要入口

## Goal

把已确认的 IP 质量 mockup 落地到正式前端：VPS 详情页只展示可扫描的 IP 质量摘要卡，并提供到完整 IP 质量驾驶舱页面的跳转；完整页面展示质量结论、风险矩阵、provider 判断、服务解锁、证据上下文、覆盖率、历史和诊断。

## User Value

- 用户购买 VPS 后，可以快速判断 IP 是否适合流媒体、AI、普通业务承载或需要复核。
- VPS 详情页不会被大量低频深度报告淹没，但能提示关键风险并跳转到完整证据。
- 后续性能、路由等低频报告可以复用“详情摘要 + 独立驾驶舱”的页面模式。

## Confirmed Facts

- 用户已确认视觉伴随 mockup 的整体展示效果符合预期，要求“就按这个来”。
- 用户要求 VPS 详情页摘要卡和完整 IP 质量页面之间做好跳转。
- 当前 center 已有 `GET /api/vps/{vps_id}/ip-quality`，前端 `getVPSIPQuality(vpsId)` 已封装。
- 当前 `VPSIPQualityReport` 已包含 `summary`、`latest_report`、`provider_results`、`service_unlocks`、`history`，足以支撑第一版驾驶舱展示。
- 当前 VPS 详情页已经加载 IP 质量数据并渲染 `VPSIPQualitySection`，但展示深度不足。
- 新规范已记录：复杂前端设计先用 skills + 浏览器 mockup 评审；低频深度报告使用独立页面承载。

## Requirements

- 新增完整 IP 质量页面路由，建议路径为 `/vps/:vpsId/ip-quality`。
- 完整页面必须复用 `getVPSIPQuality(vpsId)`，不新增后端接口。
- 完整页面必须包含已确认 mockup 的信息架构：
  - IP 质量驾驶舱标题区与采集元信息。
  - 质量结论和核心扣分/加分原因。
  - 右上四个摘要指标为两行两列：风险信号、解锁可用、数据库一致性、采集完整性。
  - Proxy、Tor、VPN、Server、Abuse、Robot 风险信号矩阵。
  - provider 逐行判断表，不合并掉 provider 分歧。
  - 服务解锁矩阵，区分解锁、部分、受阻、未知/失败。
  - IP 身份与地区一致性上下文。
  - 采集覆盖率和原始/失败报告处理提示。
  - 质量变化历史。
  - 诊断与异常区。
- VPS 详情页 `VPSIPQualitySection` 必须降级为摘要卡：
  - 显示质量结论、关键风险/服务解锁摘要、采集时间、provider/service 覆盖、出口 IP/ASN 的少量上下文。
  - 提供“查看完整 IP 质量报告”链接到 `/vps/:vpsId/ip-quality`。
  - 如果没有报告或加载失败，应保留清晰空态/错误态，不把 failure 占位事实误展示为真实 IP。
- 完整页面必须提供返回 VPS 详情页的链接。
- 展示文案以中文为主，英文术语保留必要原文。
- 样式必须走现有 tokens、BEM、`web/src/styles/pages.css`，不新增单独 page CSS 文件。
- 必须在现有 API 数据不足时诚实降级：例如数据库一致性、质量分数、覆盖率可以用当前字段派生或标成“基于已归一字段”，不能伪造未采集 provider。

## Acceptance Criteria

- [ ] `/vps/:vpsId/ip-quality` 路由可访问并加载对应 VPS 的 IP 质量报告。
- [ ] 完整 IP 质量页面展示质量结论、2x2 指标、风险矩阵、provider 表、服务解锁、上下文、覆盖率、历史和诊断。
- [ ] VPS 详情页只展示摘要卡，不再把完整 provider/service 矩阵塞在详情页中。
- [ ] VPS 详情页摘要卡能跳转到完整 IP 质量页面；完整页面能返回 VPS 详情。
- [ ] 无报告、加载失败、summary 缺失、provider/service 为空、history 为空时都有可理解的降级展示。
- [ ] `status=failure` 或 `summary` 缺失时不把 `0.0.0.0` 这类 failure 占位当作真实 IP 质量展示。
- [ ] provider 风险 flags 至少覆盖 proxy、tor、vpn、server、abuser、robot。
- [ ] service unlock 至少覆盖 unlocked、blocked、partial、unknown/error 的展示语义。
- [ ] 新增/调整测试覆盖路由、VPS 详情跳转、完整页主要区块和空态/错误态。
- [ ] `npm test -- --run ...`、`npm run typecheck`、`npm run lint` 在 web 包内通过。

## Out of Scope

- 不新增 agent 采集字段。
- 不新增或修改 center 后端 API / DB schema。
- 不实现性能基准、磁盘/CPU/内存或路由质量页面。
- 不把完整 IP 质量驾驶舱嵌入资产组合决策页；资产决策只继续消费摘要/evidence。
- 不实现真实商业 IP 数据库 provider 集成；本次只展示当前已入库字段。

## Open Questions

无阻塞开放问题。用户已确认展示方向、拆页策略和跳转要求。
