# Fix VPS overview action and navigation contract

## Goal

关闭 parent 的 I-01 与 M-01：overview 只提供可达站内 route 或当前 page 拥有的 command，
relation 不再空跳转，API route 输入 fail closed，React Router 升级到已修复版本。

## Requirements

- monitoring abnormal → `/monitoring?abnormal=1`；incident →
  `/events?object_type=monitoring_instance`；IP quality → VPS IP-quality route。
- missing subscription → subscription panel；renewal due → decision panel；lifecycle blocker →
  management menu；retry → current refresh owner；command 不得携带/依赖 same-route navigation。
- subscription relation 使用 `/subscriptions?vps_id=...`；monitoring/service/domain relation 复用
  已有 VPS-scoped components/API，在 canonical page 建立有 loading/error/focus owner 的只读 panel。
- relation route 在 Go/Web wire 中允许缺省；未知 relation 只呈现信息，不得猜 route。
- Web 按 exact `(rule_id, action.id)` 或 `relation.kind` 解析，并核对 API route 与唯一 allowlisted
  目标完全相等；external、`//host`、backslash、未知/不匹配 token 一律不可导航/执行。
- stable rule/action token 的映射只存在一个 route-private resolver；不新增 free-form command。
- 将 `react-router`/`react-router-dom` lock 升级到 7.18.2，并运行完整 Web gate。

## Acceptance Criteria

- [ ] backend rule/relation destination 表驱动测试覆盖每个 kind，且 router-level test 证明路径
  注册或 command callback 被调用。
- [ ] production behavior 中“查看监控/事件/IP/续费”到达对应 owner；“打开管理/重试”不换页
  且触发正确 callback。
- [ ] monitoring/service/domain relation 打开各自 VPS-scoped panel；external/protocol-relative/
  backslash/mismatched route 与未知 action/relation fail closed。
- [ ] React Router production audit finding 清零；Web lint/unit/build/budget/Chromium 通过。
- [ ] 不改变 management modal、focus return、legacy route ordering 或非相关 navigation。

## Out of Scope

- 新建 service/domain 顶级资产页面；修改 relation 的 freshness 来源（由 sibling child 负责）。
- 扩展永久删除、Records 权限或业务 route 集合。
