# 修复 Agent 队列回放实时性与 Activity Projection ACL

## Goal

修复 v0.79.3 现场验证中暴露的两项独立生产缺陷：Agent 在回放大量 durable backlog 时长时间不产生实时心跳/主机样本，以及 Center 的 activity projection 在当前 runtime ACL 下每分钟以 SQLSTATE `42501` 失败。修复后，离线积压应继续可靠回放，同时在线实例能在有界时间内恢复新鲜心跳和主机观测；activity projection 应在明确、最小且可执行的数据库权限合同下稳定运行。

## Background

- v0.79.3 已修复 `agent_sync_batches` 的 INSERT-only 冲突目标权限问题。测试环境中，新 Center 于 `2026-08-30T07:13:48Z` 启动后，该表的权限错误为 0。
- 两台新版 Agent 已成功向 Center 同步，但持续提交旧队列中的 `agent_version=v0.79.1`、`is_backfilled=true` 数据。`last_sync_at` 按当前接收时间推进，`last_heartbeat_at` 仍按旧 `observed_at` 缓慢前进，且尚无新的 host sample。
- 当前 Agent runtime 会在一次 tick 内同步遍历 durable queue，整个 flush 返回前不会处理下一次实时 tick。队列默认可保留 65,536 项、72 小时或 64 MiB，因此大积压可独占事件循环数小时，让一个实际在线且正在成功传输的 Agent 看起来仍无新鲜心跳。
- 同一现场的 Center 从启动后约一分钟开始，每分钟报告一次 `activity projection: source pass failed`。生产查询在 `record_activity_projection` 上使用 `SELECT ... FOR UPDATE`，而 current runtime ACL 只有 `SELECT, INSERT, DELETE`，没有 PostgreSQL 执行行锁读取所需的 `UPDATE` 权限。
- `docker compose ps -a` 中退出的 `houfeng-storage-init`、`houfeng-secrets-init` 和 `houfeng-db-init` 均为预期的一次性初始化服务：退出码 0、无 OOM/错误/重启，`restart: no`，后续服务通过 `service_completed_successfully` 等待它们。它们不是本任务的缺陷。

## Requirements

- Agent 的 backlog 回放不得无限期独占 runtime tick；必须设计并实现可测试的有界调度/公平性合同，使实时采样与心跳在 backlog 尚未排空时仍能取得进展。
- 保留 durable queue 的可靠性、幂等、5xx/429 重试和 backfill 语义。不得通过删除队列、丢弃旧事实、重装 Agent 或伪造当前心跳来获得表面恢复。
- 明确并验证旧 backfill 与新实时事实交错时的顺序和单调性：旧事实不得把实例的新鲜状态、时间戳或最新观测回退；重复提交不得产生重复副作用。
- 在最小必要范围内提供可运维辨识的状态，使“传输正常但正在追赶 backlog”可与“Agent 断连/同步失败”区分；任何日志、API 或页面证据均不得泄漏 token、Authorization、DSN、fingerprint 或原始请求载荷。
- 修复 activity projection 的真实 production DML 与 current runtime ACL 不兼容问题。方案必须同时说明并验证并发分类/去重语义和最小权限边界，不得以现场手工 `GRANT` 作为修复。
- activity projection 必须使用 current ACL convergence 后的 direct-runtime PostgreSQL 16 测试执行生产 store 路径；catalog-only、fake transaction 或 skip-as-pass 均不能作为权限正确性的证据。
- 将这两类缺陷分别建立 RED，再实施最小修复；不要把 Agent 回放调度问题误归因于已修复的 `agent_sync_batches` ACL，也不要把 activity projection 失败描述为心跳同步故障。
- 保持三个 init 服务的一次性生命周期。除非后续证据发现非零退出或依赖失败，不修改其重启策略或把它们改成长驻容器。
- 远端验证只能操作用户授权的 Houfeng 测试部署目录及其 Compose 资源；不得改动服务器其他位置。用户已于 2026-08-30 授权在独立审查归零、项目门禁通过后完成 commit、push、PR、merge、release、Center-first/Agent-second 升级、发布后验收和精确清理。部署前仍须从私有交接上下文重新验证精确目标和冷恢复点；仓库任务材料不保存私有主机、路径、凭据或环境定位信息。

## Acceptance Criteria

- [ ] Agent RED 能稳定证明：当 durable queue 大于单次允许工作量时，旧实现会在 backlog 完整 flush 前阻塞新的实时 tick/采样。
- [ ] Agent GREEN 能证明：backlog 尚未排空时，新鲜 heartbeat 与 host sample 在设计规定的上界内提交；旧队列仍持续缩短，暂时失败仍保留并重试，最终无丢失、无重复副作用。
- [ ] 乱序/交错回放回归证明 Center 的实例状态和相关“最新”时间戳不会被较旧 backfill 回退，现有 paused/retired/binding/token/fingerprint 安全边界不变。
- [ ] 运维证据可以区分实时同步、backlog replay 和真正的同步失败；响应、日志、测试失败和任务证据均通过敏感信息扫描。
- [ ] Activity projection RED 在 current ACL convergence 后以真实 direct-runtime PostgreSQL 16 执行生产路径，并精确复现 SQLSTATE `42501`，不输出原始数据库错误或凭据。
- [ ] Activity projection GREEN 在选定的最小权限合同下完成 source pass；并发候选分类、canonical hash/去重和事务锁定语义由真实 PostgreSQL 回归覆盖，且没有现场手工授权。
- [ ] 相关 focused、race、真实 PostgreSQL strict lane、`make verify-go`、格式、diff scope 与 `git diff --check` 全部通过；strict lane 必须实际 RUN/PASS，不得 SKIP。
- [ ] 发布后现场证明：`agent_sync_batches` 42501 仍为 0；两台 Agent 在 backlog 尚存或完成追赶后产生当前版本的新鲜 heartbeat 和 host sample；`record_activity_projection` 42501 不再出现且 projection 事实推进。
- [ ] Compose 现场保持常驻服务 healthy、无 OOM/异常重启；三个 init 服务仍以 exit 0 成功完成。非零退出、OOM 或依赖未满足才判定为异常。

## Out of Scope

- 删除/截断 Agent durable queue、重装或重新接入 Agent、伪造当前观测、手工修改数据库事实。
- 把一次性 init 服务的 `Exited (0)` 改造成常驻运行状态。
- 与 backlog 公平性、同步可观测性或 activity projection ACL 无关的 UI、协议、Compose 或数据库重构。
- 操作授权测试部署目录之外的服务器文件；删除 durable queue、重装 Agent、手工修改数据库事实/ACL，或绕过项目 PR、CI、发布和冷恢复流程。

## Notes

- 本任务是复杂跨层 bugfix；规划、代码/并发语义研究、`design.md`、可执行 `implement.md`、本地 TDD 与独立审查均已推进。用户于 2026-08-30 接受 `K=2` 设计，并随后明确授权按项目规范完成提交、PR/CI、合并、发布、测试环境部署、验收与最终清理；Trellis 规定的一次性提交计划确认仍必须执行。
- 现场证据见 `research/field-audit.md`。前序 `agent_sync_batches` 修复任务已随 v0.79.3 完成交付，本任务不得重做或回滚该修复。
