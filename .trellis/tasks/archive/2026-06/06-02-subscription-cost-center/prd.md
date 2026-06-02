# 订阅模块 VPS 成本中枢

## Goal

把当前“订阅列表”升级为 VPS-first 成本中枢，用于服务器账单事实管理、统一币种成本判断、预算风险识别、续费提醒和资产决策联动。

## Requirements

- 订阅模块必须继续服务 VPS 资产决策，不能把 `subscription.status` 变成第二套用户侧业务状态。
- 默认基准货币为 `CNY`，用户可在订阅设置中修改。
- 默认汇率 Provider 使用 Frankfurter；Fixer 作为可配置 Provider 支持，API key 只能来自设置或环境注入，响应与日志必须脱敏。
- 订阅列表响应需要返回后端统一计算的基准货币月/年成本、汇率来源、汇率日期和 stale 状态。
- 新增订阅总览与统计能力：总成本、未来续费、预算风险、供应商/币种/VPS 拆分、缺失订阅事实资产。
- 新增预算能力：支持全局、供应商、标签/分类、单 VPS scope，预算比较统一使用基准货币。
- 新增续费提醒能力：默认 `14/7/1` 天，支持用户设置最远提前提醒天数；提醒走现有 Telegram/Feishu 通知能力和通知审计，不伪装为 incident。
- 提醒必须去重，重复 worker 扫描不得重复发送同一个 `subscription + renew_at + offset_days` 提醒。
- 已取消/过期订阅、已取消/归档 VPS 不发普通续费提醒；取消/迁移/已取消自动续费但仍有临近续费风险的 VPS 进入决策关注提醒。
- `/subscriptions` 前端升级为成本工作台，保留现有创建/编辑订阅流程。
- VPS 详情、Asset Decisions、Dashboard 只接入必要成本信号；Dashboard 保持系统概览定位，不展示订阅明细仓库。
- 第一版不做通用个人订阅 App、App Store 导入、银行账单识别、AI 推荐、第三方自动取消或具体 VPS 性价比公式。

## Acceptance Criteria

- [ ] 可以通过 API 设置和读取订阅成本设置，默认 `base_currency=CNY`、默认提醒 offsets 为 `14/7/1`。
- [ ] 可以刷新并缓存汇率；无汇率或 Provider 失败时 API 返回明确 stale/error 状态，成本计算使用最近可用缓存或标记不可折算。
- [ ] `GET /api/subscriptions` 兼容旧字段，同时返回基准货币成本和预算/提醒派生字段。
- [ ] `GET /api/subscriptions/overview` 与 `GET /api/subscriptions/statistics` 返回多角度成本统计和未来续费摘要。
- [ ] `GET/POST/PATCH /api/subscription-budgets` 可以管理预算，并影响 overview 与订阅派生字段。
- [ ] `POST /api/subscriptions/exchange-rates/refresh` 可以手动触发汇率刷新。
- [ ] 续费提醒 worker 使用现有通知通道发送/抑制/失败审计记录，并通过投递表去重。
- [ ] 前端 `/subscriptions` 展示总览、续费队列、明细、预算、汇率/提醒设置，并能完成订阅创建/编辑。
- [ ] VPS 详情展示当前 VPS 的成本卡片；Asset Decisions 展示成本风险；Dashboard 只展示高信号成本摘要。
- [ ] 后端、前端和跨层测试覆盖设置、汇率、预算、提醒、统计、页面关键状态。
- [ ] `make verify-go`、`make verify-web` 或 `./scripts/verify.sh` 在最终修复后通过，若某个本地视觉验证工具不可用，需要如实记录。

## Notes

- 业务权威仍以 `CLAUDE.md` 的 VPS-first invariant 为准：VPS 是主资产对象，Subscription 只是 VPS-scoped billing facts。
- 当前 `origin/main` 已有订阅周期、续费模式、有效期延长相关迁移，本任务在其上增量实现。
