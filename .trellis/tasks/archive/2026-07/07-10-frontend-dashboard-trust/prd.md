# 前端 Dashboard 事实可信度

## Goal

修正 Dashboard 的错误聚合与失败伪空状态，并将首屏恢复为符合现行规范的五状态、单一主行动 command surface。

## Confirmed Facts

- `abnormal_monitoring_instance_count` 已包含 severe，前端相加会重复计数。
- VPS/订阅请求失败当前被转换为 `[]`/`null`，可能错误触发首次接入。
- 现行 Dashboard 首屏为四张等权 KPI，390px 主行动落在首屏以下；测试名称与断言语义漂移。
- 旧 `DashboardCommandSurface` 约 692 行且无生产调用。

## Requirements

- Dashboard、VPS、subscription overview 分别使用可区分 loading/success/error 的 remote state。
- abnormal 总数直接使用后端 abnormal 字段；severe 只做分层。
- 固定 onboarding、critical、abnormal、maintenance、stable 五种模式，每种只有一个 primary action。
- 首屏最多三个低权重判断摘要；删除重复 KPI、事件列和不可达第二套实现。
- 测试同时断言必须出现与禁止出现内容，并覆盖后端 subset contract。

## Dependency And Scope

- 依赖 `frontend-quality-gate-strict` 合并。
- 不改变 Go JSON wire shape；高风险字段通过共享 fixture/contract test 固化。

## Acceptance Criteria

- [ ] abnormal=2、severe=1 时 UI 总数为 2。
- [ ] VPS 503 时不出现“先创建第一台 VPS”，成功空数组才允许 onboarding。
- [ ] 五种 mode 均有唯一主行动、正确 deep link 和禁止内容测试。
- [ ] 390x900 首屏可看到页面标题、今日第一步及至少一个证据摘要。
- [ ] 无生产调用的旧 command-surface 组件被删除或缩减为真实使用的纯展示组件。
