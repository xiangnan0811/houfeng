# Tune agent observability cadence

## Goal

把候风默认观测链路从“几分钟级可见”调整到“5 秒级有意义可观测”：agent 默认每 5 秒形成同步心跳，center 默认下发 5 秒主机样本频率，TCP/HTTP 探测默认进入 5 秒档，同时让告警 sweep 与已部署默认设置不继续卡在旧的 5 分钟 / 60 秒节奏。

## Requirements

- 新增 `5s` frequency tier，并贯穿 agent ↔ center contract、center validation、agent host sample / probe scheduling、前端类型与配置表单。
- agent 默认 sync ticker 从 30 秒降到 5 秒；若 center plan 下发 `5s`，host sample 与 due probe 必须能按 5 秒节奏执行。
- center 默认设置改为：host sample `5s`，TCP/HTTP probe defaults `5s`，TLS probe default 保持 `6h`（证书过期检查不是秒级观测信号）。
- center agent plan 的 legacy fallback / missing settings fallback 不能继续下发旧 `5m`；普通节点也应拿到 `5s` host sample tier。
- 已部署实例升级后，旧默认值应迁移到 5 秒级：`center_settings` 中旧 `5m` host/TCP/HTTP 默认改为 `5s`，旧 incident heartbeat/sweep 默认改为 5 秒级；既有 TCP/HTTP probe item 如果仍是旧默认 `5m`，迁移到 `5s`。
- incident stale 判断与 sweep 默认要匹配 5 秒观测：默认 heartbeat interval `5s`，stale threshold 仍为 3 个 interval，默认 sweep interval `5s`，使心跳丢失通常在十几秒级被发现，而不是继续等 60 秒 sweep。
- agent 离线缓冲默认容量要与 5 秒 ticker 匹配，避免 2048 条默认容量在高频同步下只保留约 2.8 小时。
- raw observations / heartbeats / probe observations 表结构本次不重构；沿用当前按对象+时间索引、7 天 raw retention 与 daily aggregates。
- 前端设置页、Target probe 创建/编辑表单必须能选择/显示 `5 秒`，新建 TCP/HTTP probe 默认使用 `5s`。

## Acceptance Criteria

- [ ] `internal/contracts/agentapi` 暴露 `FrequencyTier5s`，Go contract JSON round-trip 测试覆盖 `5s`。
- [ ] `internal/center/targets.IsValidFrequencyTier("5s")` 为 true，settings / probe handler 接受 `5s` 并仍拒绝未知档位。
- [ ] `centersettings.Default()` 返回 host sample `5s`、TCP/HTTP `5s`、TLS `6h`、heartbeat interval `5`、sweep interval `5`。
- [ ] agent runtime 默认 interval 为 5 秒，`hostSampleDue("5s", ...)` 与 probe provider `5s` 调度有单测覆盖。
- [ ] center store agent plan 在缺 settings row / legacy fallback 路径不再返回普通节点 `5m`；已有 override 仍优先。
- [ ] 新迁移更新 `center_settings` defaults 与旧默认行，并把既有 TCP/HTTP `5m` probe items 迁移到 `5s`。
- [ ] 前端 `FrequencyTier` 类型、设置页选项、Target probe form 选项和新建 TCP/HTTP 默认值包含 `5s`。
- [ ] 相关 Go tests 通过；跨前后端改动后 `./scripts/verify.sh` 通过或记录明确阻塞。
- [ ] 如本地可启动 UI，设置页与目标详情 probe 表单完成 browser sanity；如无法做浏览器验证，最终说明未验证原因。

## Technical Approach

完整数据流：

`center settings / probe_items` → `store/agent_plan.go` → `agentapi.SyncPlan` → `agent/runtime` `hostSampleDue` + `agent/probe` `CollectDue` → `POST /api/agent/sync` raw facts → incident worker sweep → React settings/probe forms。

实现按单一频率字符串 `"5s"` 扩展现有枚举，不引入独立 sampling profile 或 per-agent env。默认从 center-owned plan 下发，保持 thin agent 模型：agent 只执行 plan，不本地决定业务频率。

数据库层只做默认值与旧默认数据迁移，不新增 TSDB / partition / aggregate schema。当前 append-only fact 表已有 `node_id/target_id + observed_at desc` 索引，retention worker 已清理 raw 表并生成 daily aggregates，MVP 可以先用现有结构承载单操作者场景。

## Decision (ADR-lite)

**Context**: 用户真实部署后发现“约 5 分钟一次”失去可观测意义。代码定位显示 agent 本地 sync ticker 是 30 秒，真正让主机样本和 probe 慢下来的，是 center settings / agent plan 的 `5m` 默认档位，以及 incident sweep 仍为 60 秒。

**Decision**: 引入 `5s` 作为正式 frequency tier，并把默认观测链路调整为 5 秒级；同时迁移旧默认设置和既有 TCP/HTTP 默认 probe，确保已部署实例升级后立即改善。暂不重构 raw facts 表结构。

**Consequences**: 数据写入量会明显增加，尤其是 heartbeats / host_samples / TCP/HTTP probe observations。风险由现有 retention 控制，并通过提高 agent buffer 默认容量避免高频 ticker 过早丢离线队列。若未来多节点规模扩大，再评估分区、降采样、按节点 profile 或聚合表策略。

## Out of Scope

- 不引入 MQ、TSDB、Prometheus、ClickHouse、数据库分区或新服务。
- 不做 per-node UI profile / adaptive sampling / dynamic backpressure。
- 不把 TLS expiry check 默认改成 5 秒。
- 不改变 Node / Target / ProbeItem 的业务模型。
- 不改变 agent 安装拓扑或让 center 主动连接 agent。

## Technical Notes

- `agent/runtime/runtime.go`：默认 ticker 与 host sample due 判断。
- `agent/probe/provider.go`：probe assignment due 判断。
- `agent/syncqueue/store.go` 与 `agent/config/config.go`：离线队列默认容量。
- `internal/contracts/agentapi/types.go`：agent ↔ center frequency tier contract。
- `internal/center/settings/types.go`：center 默认 settings 与 validation。
- `internal/center/targets/types.go`：probe item frequency validation。
- `internal/center/store/agent_plan.go`：settings fallback、legacy fallback、override resolution。
- `internal/center/incidents/service.go`：heartbeat interval / sweep interval 使用 settings defaults。
- `db/migrations/`：新增迁移更新 defaults 与旧默认数据。
- `web/src/lib/types.ts`、`web/src/pages/settings/FrequencyDefaultsSection.tsx`、`web/src/pages/target-detail/targetDetailConstants.ts`、`web/src/components/target-detail/TargetProbeForm.tsx`：前端类型、选项与默认值。
