# 修复 agent 全新接入心跳失败

## Goal

修复已绑定且持续运行的 Houfeng agent 向 v0.79.2 Center 同步时稳定收到 HTTP 500、Center 无法持久化心跳的问题，使现有 agent 无需重装、重新接入或人工修改数据库即可通过后续重试恢复心跳与运行观测。

## Background

- 两台临时 agent 主机分别运行官方 v0.79.2 amd64/arm64 二进制；systemd 均为 `active (running)` 且 `NRestarts=0`，每约 5 秒同步一次并收到 `500 internal_error`。
- Center 请求确实穿过反向代理并进入同步 handler；相同时间 PostgreSQL 持续报告 `permission denied for table agent_sync_batches`。
- 失败 SQL 是 `INSERT ... ON CONFLICT (monitoring_instance_id, sync_batch_id) DO NOTHING`。当前 APP ACL manifest 有意只授予 runtime 对 `agent_sync_batches` 的 `INSERT`，不授予 `SELECT`。
- PostgreSQL 16 会把显式 conflict target 中的列视为被读取列，因此上述语句在 INSERT-only 角色下以 SQLSTATE `42501` 失败。现有 catalog verifier 正确验证了静态 ACL；缺口在于生产 SQL 的隐式权限没有被真实 runtime-role DML 测试覆盖。
- 独立 PostgreSQL 16.12 复现已证明：显式 conflict target 在 INSERT-only 角色下失败；授予冲突列 `SELECT` 后可用；保留 INSERT-only 并改用 targetless `ON CONFLICT DO NOTHING` 同样可完成首次插入和重复批次忽略。
- v0.79.2 的 agent durable queue 会把 5xx 保留并重试，因此本问题不是该版本的队列修复引入；它只是让原有 Center 数据库权限缺陷稳定暴露出来。

## Requirements

- 修复必须发生在 Center 的真实同步持久化路径，不得通过页面状态兜底、伪造心跳、手工授权或手工修改数据绕过故障。
- 保持 `agent_sync_batches` 的 runtime 最小权限为 INSERT-only；不得为了现有 SQL 方便而增加表级或列级 `SELECT`，不得修改已发布 migration。
- 保持当前事务顺序：验证实例/token/fingerprint 后先记录同步批次幂等事实，首次批次再写 heartbeat/observations/state；重复批次提交成功但不重复写事实。
- 在当前仅有复合主键唯一性的 schema 下，使用 targetless `ON CONFLICT DO NOTHING` 保持现有幂等语义。未来若新增其他唯一约束，必须重新审查“任意唯一冲突均忽略”是否仍符合合同。
- 不改变 agent↔Center DTO、HTTP endpoint、HTTP 错误码、agent 重试策略、proxy、Compose 配置、MonitoringInstance 生命周期或 Web 展示合同。
- 数据库错误继续通过 `%w` 保留 typed cause，agent 端仍只看到稳定的 `500 internal_error`；日志、测试与任务证据不得包含 token、Authorization、DSN、原始 fingerprint 或请求载荷。
- 修复不得要求数据库迁移或人工 ACL 收敛；升级并重启 Center 后，v0.79.2 agent 的既有 durable retry 应能自动再次送达。

## Acceptance Criteria

- [x] RED：在当前迁移和 `ConvergeAppACLCurrent` 生成的真实 runtime 角色下，生产 `ApplyBatch` 使用旧显式 conflict target 时以 SQLSTATE `42501` 失败；测试不能用 fake transaction 或 skip-as-pass 代替。
- [x] GREEN：同一真实 PostgreSQL 16 runtime 角色保持 `agent_sync_batches` 的 `INSERT=true`、`SELECT/UPDATE/DELETE=false`，首次 heartbeat-only 批次仍能成功提交。
- [x] 同一批次重复提交成功且不重写事实：`agent_sync_batches` 恰好一行、`monitoring_instance_heartbeats` 恰好一行，实例的 `last_heartbeat_at` 与 `last_sync_at` 已推进。
- [x] store 单元回归冻结 SQL 为 targetless `ON CONFLICT DO NOTHING`，并拒绝重新引入显式 `(monitoring_instance_id, sync_batch_id)` target。
- [x] `db/migrations/**`、APP ACL allowlist、agent、proxy、Compose、handler、DTO 与 Web 均无产品变更；现有 enrollment、binding、paused/retired suppression 与 sync error mapping 回归保持 GREEN。
- [x] focused unit、真实 PostgreSQL 16 strict lane、相关 store/migrate/handler 回归、`make verify-go` 与 `git diff --check` 全部通过。
- [ ] 发布后测试环境中不再出现该表的 permission denied；两台现有 agent 无需重装即可由队列重试恢复成功同步，并由数据库/Center 事实确认新心跳，而不是只看 systemd active。

## Out of Scope

- 扩大 runtime 数据库权限、引入 column ACL、修改 APP ACL manifest 或新增数据库 migration。
- 重新设计 agent 队列、安装器、绑定/注册协议、心跳频率或观测 DTO。
- 处理一次性 init 容器成功退出、ClamAV 非致命告警或与本次 `agent_sync_batches` 权限错误无关的部署告警。
- 顺带重构同步仓储、幂等模型或 Monitoring 页面。

## Notes

- 实施分支：`codex/agent-heartbeat-onboarding-failure`；获批的最小 TDD 修复、独立双阶段审查及主会话 fresh verification 已完成。
- `commit`、`push`、PR、`merge`、`release`、`deploy`、测试环境现场恢复及最终清理均已获授权，但尚未开始；当前仍处于 Phase 3 spec/commit 前的知识捕获与复核阶段。
