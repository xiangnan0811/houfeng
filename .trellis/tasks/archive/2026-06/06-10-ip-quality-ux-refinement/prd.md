# IP质量页面体验优化设计

## Goal

修复 IP 质量相关页面的信息架构和视觉体验问题，让已采集的 IP 质量事实以清晰、紧凑、可判断的方式呈现。当前 center / agent 已具备 MVP 数据采集与保存能力，本任务聚焦前端展示质量：VPS 详情页只保留摘要和入口，IP 质量详情页完整展示质量结论、provider 判断、服务解锁矩阵、上下文、历史与诊断，但不得把技术采集字段直接暴露为用户主要信息。

本任务必须先产出浏览器可审查的设计 demo，经用户确认后才进入正式实现。

## Confirmed Facts

- 现有正式实现文件：
  - `web/src/pages/vps-detail/VPSIPQualitySection.tsx`
  - `web/src/components/ip-quality/IPQualityDashboard.tsx`
  - `web/src/components/ip-quality/ipQualityPresentation.ts`
  - `web/src/index.css`
- VPS 详情页当前 IP 质量摘要同时展示完整报告入口、风险等级、国家/基础 IP 信息和一组服务解锁 chip，导致入口与结论混杂。
- IP 质量详情页 header 当前同时展示风险等级和返回按钮，用户期望 header 行只保留返回 VPS 详情按钮。
- Provider 判断表格当前“证据说明”列会展示 `error_summary` 等采集诊断文本，导致行高失控和信息污染。
- 服务解锁矩阵当前会展示采集状态、`default_probe`、source、latency 等技术字段；状态统计竖向堆叠，卡片 grid 在 7 个服务时呈现 6 + 1，空白过大。
- `.trellis/spec/web/component-conventions.md` 已记录“低频深度报告使用独立页面承载”和“复杂前端变更先走视觉伴随评审”的项目规则。
- 视觉必须贴合当前项目主题与现有页面气质：暗色氛围、克制工程密度、精致亮色强调；正式实现不得另起独立色板或让 IP 质量页与系统其他页面割裂。
- 当前项目实际主题变量集中在 `web/src/index.css`，IP 质量展示应复用 `--bg`、`--panel-bg`、`--surface`、`--border`、`--text-*`、`--accent`、`--color-state-*`、`.badge`、`.btn`、`.page-panel`、`.section-heading` 等现有令牌与组件样式语义。
- `.superpowers/` 已被 `.gitignore` 忽略，可用于本地浏览器 mockup，不进入正式产品代码。

## Requirements

- VPS 详情页 IP 质量摘要：
  - 右上角只保留“查看完整 IP 质量报告”入口，不展示风险等级或国家标签。
  - 摘要区保留可快速判断的质量结论、风险/覆盖/解锁概览，但不能罗列大量服务 chip。
  - 服务解锁信息需要整合为可扫读的概览，例如可用、受阻、部分、未知数量，以及少量重点异常。
- IP 质量详情页驾驶舱：
  - Header 右上角只保留“返回 VPS 详情”按钮；风险等级必须放入正文质量/风险区域，而不是与返回按钮并列。
  - 保持独立页面承载完整 IP 质量报告，不把大矩阵塞回 VPS 详情页。
  - 首屏应强调 IP 质量判断，而不是基础 IP 信息。基础 IP、ASN、组织、使用地、注册地作为证据上下文展示。
- Provider 判断表格：
  - 不再把长 `error_summary`、`not_configured`、原始 JSON 或采集诊断文本直接展示在主要列。
  - “证据说明”列应改为紧凑风险/属性信号 chip，只展示 Proxy、VPN、Tor、Abuse、Robot、Server 等用户可理解证据。
  - 未配置、跳过、失败等采集诊断应压缩为短状态或放入折叠详情，不得撑高表格行。
- 服务解锁矩阵：
  - 右上角状态统计横向排列并右对齐。
  - 卡片根据实际数量合理排列，避免宽屏 6 + 1 造成大量空白；7 个服务时桌面目标为 4 + 3 或类似均衡布局。
  - 卡片主体只展示服务名、解锁状态、区域、解锁类型和必要说明。
  - 不展示采集状态 badge、`default_probe`、source、latency 等技术文本作为主要信息。
  - unknown / 检测失败 / 解锁失败应使用统一的灰色或明确弱化样式，不使用突兀白色卡片。
- 设计流程：
  - 正式改 `web/src` 前，必须先提供浏览器 demo 给用户审核。
  - 用户确认 demo 后，再补齐正式 `design.md` / `implement.md` 并申请进入执行阶段。
- 视觉一致性：
  - Demo 与正式实现必须使用项目现有主题配色和页面氛围，不使用临时蓝绿/外部 dashboard 色板。
  - 状态色使用现有 `normal / notice / alert / critical / neutral` 语义；unknown / 失败类状态走 neutral/offline 弱化，不使用突兀白底或高亮。
  - 组件密度、边框、圆角、字号、按钮、badge、表格应与系统现有 `page-panel`、`data-table`、`badge`、`btn` 风格保持一致。

## Acceptance Criteria

- [ ] 已在浏览器中展示可审查的 UX demo，包含 VPS 详情摘要卡和 IP 质量详情页关键区域。
- [ ] Demo 明确解决用户提出的 5 类问题，并体现字段优先级、布局密度、状态样式、技术字段降噪策略。
- [ ] Demo 使用项目现有主题配色与氛围，视觉上不与其他页面割裂。
- [ ] 用户审核通过前，没有修改正式产品页面代码。
- [ ] 正式实现后，VPS 详情页摘要右上角只保留完整报告入口。
- [ ] 正式实现后，VPS 详情页服务解锁摘要不再罗列大量服务 chip，而是整合为紧凑概览。
- [ ] 正式实现后，IP 质量详情页 header 右上角只保留返回按钮。
- [ ] 正式实现后，Provider 表格主视图不再展示长诊断文案，不产生异常大行高。
- [ ] 正式实现后，服务解锁矩阵状态统计横向右对齐，卡片布局均衡，unknown 状态统一弱化。
- [ ] 正式实现后，不向用户主要视图暴露 `default_probe`、`not_configured` 等内部技术文本。
- [ ] 正式实现后，前端验证通过，至少包含相关组件/页面测试与 `npm run lint`、`npm run test -- --run`、`npm run build`。

## Out Of Scope

- 不改变 agent 采集逻辑、center 入库逻辑或 IP 质量 API contract。
- 不新增 IP 质量字段，不修复数据采集完整性问题。
- 不处理后续 CPU、磁盘、内存性能或路由质量报告。
- 本阶段不提交 PR、不发布镜像；待正式实现和验证完成后再走后续流程。

## Open Questions

- 无阻塞性产品问题。用户已指定总体方向：先做浏览器 demo，审核通过后再开发。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
