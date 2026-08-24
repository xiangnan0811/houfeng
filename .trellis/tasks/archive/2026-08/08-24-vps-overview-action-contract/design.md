# VPS 概览动作与关系目的地技术设计

## 1. 合同选择

wire 已有 anomaly `rule_id/action.id` 与 relation `kind`，因此不新增 free-form command，也不把
任意 API path 交给 `Link`。route-private resolver 把 closed token 解析为：

```ts
type Destination =
  | { kind: 'route'; to: string }
  | { kind: 'command'; command: VPSOverviewCommand }
```

route destination 只有在 API route 与该 token 对应的 computed allowlisted route 完全相等时
成立；command 必须没有 route。未知 token、mismatch、scheme、protocol-relative、backslash
全部返回 no destination。presentation 用 Link 导航、Button 执行命令；page composition 将
command key 绑定到 controller/refresh，不从 label/detail 推断行为。

相比仅检查 `/` 前缀，这能阻止合法站内但错误/惰性的 current-page route；相比新增通用 action
registry/free-form command，它只覆盖当前 overview 的固定小矩阵且复用 router authority。

## 2. Anomaly destination matrix

| Stable rule | Action | 类型与 owner |
| --- | --- | --- |
| `monitoring.health.abnormal.v1` | `open_monitoring` | route `/monitoring?abnormal=1` |
| `monitoring.incidents.open.v1` | `open_incidents` | route `/events?object_type=monitoring_instance` |
| 三个 `ip_quality.*.v1` | `open_ip_quality` | route `/vps/<encoded-id>/ip-quality` |
| `renewal.subscription.missing.v1` | `open_subscription` | command `openPanel('subscription')` |
| `renewal.due.soon.v1` | `open_renewal_decision` | command `openPanel('decision')` |
| `lifecycle.blocker.v1` | `open_management` | command `openMenu()` |
| `source.unavailable.v1` | `retry_overview` | command `commands.refresh()` |

rule IDs 保持既有稳定性；当前含混的 `open_subscriptions` 拆成两个明确 action IDs，并由完整
Go/TS/browser matrix 同步冻结。若发现必须兼容旧 ID，只能按 exact rule+ID 映射，不能保留含混
单 ID 或从文案推断。

monitoring/events 是当前已注册且能处理相应过滤参数的 work surface，但不是 VPS-scoped；本任务
不扩展它们的 query/API。IP route 是精确 VPS-scoped page。

## 3. Relation destination matrix

| kind | 类型与 owner |
| --- | --- |
| `subscriptions` | route `/subscriptions?vps_id=<encoded-id>` |
| `monitoring_instances` | command 打开 canonical monitoring-instance-evidence panel |
| `services` | command 打开 canonical services-detail panel |
| `domains` | command 打开 canonical domains-detail panel |

Go relation `route` 改为 `omitempty`，TS `route?: string`；保留 I-03 required `section`。backend 只
给 subscriptions 生成 route，其他三个不生成 route。canonical controller 增加既有语义的三个
panel token；action host 复用 `VPSMonitoringInstanceLinksSection`、`VPSServicesSection`、
`VPSDomainsSection` 及现有 list API，按打开时加载并提供 bounded error/retry/empty。不得 import
整个 `LegacyVPSDetail` 或复制其大状态机。

relation card 作为 command trigger 时是 Button；modal 关闭后 focus 返回原 card。I-03 的 local
freshness retry 是 sibling Button，不能嵌入 route/command trigger。unknown kind/route mismatch
只呈现非交互信息。

## 4. React Router 依赖边界

将 direct `react-router-dom` minimum 改为 `^7.18.2` 并重新生成 lock，使 transitive
`react-router` 也为 7.18.2。不升 v8。项目 Node 22/React 19 满足兼容范围；当前是 client Data
Mode，无 Framework/RSC runtime，但 7.17.0 production audit 红且 API path 进入真实 Link sink。
应用级 exact resolver 仍是必要纵深合同，依赖升级不替代它。

由于 7.18.0+ 涉及 route matching/URL normalization，必须跑 matchRoutes、完整 Vitest、production
build/bundle 与 Chromium 行为断言。最终 `npm audit --omit=dev` 不得再报告 React Router finding。

## 5. 文件所有权

- Backend：`internal/center/vpsoverview/{anomalies,types}*.go`、
  `internal/center/store/vps_overview*.go`
- Web DTO/resolver：`web/src/lib/types.ts`、新 `vpsOverviewDestination.ts` 及测试
- UI：Anomalies、Relations、PageView、management controller/actions、三个复用 relation sections
  及 focused tests/CSS owner
- Router/dependency：router tests、`web/package.json`、`web/package-lock.json`
- Browser：overview destinations fixture/spec

这些文件在 I-03 后修改，消费已冻结 relation.section；完成后把最终 action/relation shape 冻结给
I-02 decoder。

## 6. 兼容、安全与回滚

无 backend route、新写 API、migration 或权限变化。三个 relation panels 是现有 VPS-scoped read
behavior 在 canonical owner 中的复用。API route 不被当成 generic URL；unknown/mismatch fail
closed。rollback 可关闭 capability 回 legacy 或 revert本 child，但不能恢复 unchecked Link sink。
