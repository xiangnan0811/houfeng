# 修复 agent 接入后无心跳与主机样本

## Goal

修复 v0.79.1 中 MonitoringInstance 已经通过 VPS 接入流程创建并与 VPS 绑定、agent 服务也持续运行，但中心端始终没有把该 agent 识别为已接入、没有记录心跳或主机样本的问题，使用户能够从“待接入”稳定收敛到可观测的已接入状态。

## Background

- v0.79.1 已修复首次 Agent 接入入口死锁；用户升级后可以从未关联 VPS 创建 MonitoringInstance 并进入 Agent onboarding。
- 用户确认 agent 端没有可见报错，`systemctl status houfeng-agent` 为 `active (running)`。
- 页面当前仍显示 MonitoringInstance 生命周期“待接入”、agent 为 `—`，心跳与运行数据均为空；VPS 详情同时把关联监控实例与总体监控标为正常。
- 截图中的同一实例 `netcup-de` 已绑定 VPS，说明本任务不是再次修复入口、创建或关联流程。

## Requirements

- 必须沿 agent 配置/身份获取、注册或认证、心跳/主机样本上送、中心端接收与持久化、读取 API、页面状态映射的完整链路定位根因，不能用 UI 兜底掩盖服务端未收到数据。
- 必须区分“进程正在运行”与“中心端已验证接入”：只有权威心跳/样本证据到达后才能把实例从“待接入”推进到已接入状态。
- 修复必须兼容 v0.79.1 已生成的 onboarding 配置和既有 MonitoringInstance/VPS 关系；不得要求用户删除 VPS、伪造心跳、手工改数据库或重新创建监控实例。
- agent 端传输或中心端拒绝数据时必须留下不泄露凭据的可操作诊断证据，避免再次出现服务 active 但无可见原因的静默失败。
- 保持既有认证、租户边界、幂等/重试、隐私与 fail-closed 合同；不得把 agent secret、令牌、原始敏感载荷或内部凭据暴露到页面、日志、URL 或测试输出。
- 修复范围必须聚焦本次数据接入断点，不回退 v0.79.1 的首次接入 UX，也不顺带重构无关监控页面。

## Acceptance Criteria

- [x] RED 回归使用真实 `syncqueue.FileStore` 复现：旧身份/旧凭据请求位于持久队列头部并被 center 以 401/404/409 拒绝时，当前已保存凭据生成的新心跳始终不会被尝试，而进程仍持续运行。
- [x] 修复后，agent 能识别并原子批量删除与当前 MonitoringInstance ID、sync token 或 fingerprint 不一致的陈旧队列项，并在同一次 flush 中继续发送当前心跳；72 小时大积压不能退化成逐项 rewrite/fsync 或日志洪泛，无需重建 VPS、MonitoringInstance 或人工改库。
- [x] 当前身份的合法离线积压仍按 oldest-first/backfill 语义发送；即使 legacy/hostile 持久文件中的多个 current facts 复用同一 entry ID，第一个 ack/delete 也不能删除尚未发送的后续 fact；command result/IP-quality report 只在 durable Enqueue 后从 runtime buffer 清理；typed remote status 0/2xx/3xx、网络错误、429、503 与其他 5xx 保留队头并在后续 tick 重试，不因修复而丢失可恢复数据。
- [x] center 明确返回 `invalid_json` / `invalid_request` 的脏队列项会留下脱敏错误证据后被删除，不再永久阻塞后续心跳；当前身份的 401/404/409/405 或其他不可恢复 4xx 会使 runtime 返回可操作的永久错误，不再伪装成健康循环。
- [x] 永久/丢弃/重试、enrollment 与兼容 non-queue 路径的日志/返回错误只包含稳定 phase/action/status/allowlisted code/reason/count；不包含 sync/enrollment token、Authorization、原始 fingerprint、请求/响应 body、远端自由文本、persisted queue/instance ID 或 raw local cause。`agent enrolled` 只在凭据持久化后记录，start/stop URL 只记录 origin。
- [x] 当前心跳被真实 center 合同接受后，既有持久化/读取链自然更新 `last_heartbeat_at`；后续 tick 根据返回 plan 上送 HostSample，并由既有读取面把生命周期从“待接入”推进到“在用”。不得用绑定关系或 systemd active 替代该权威证据。
- [x] focused agent 测试、相关 center handler/store 回归、`-race`/重复运行、`make verify-go`、隐私扫描与项目质量审查通过；若未改 Web，不以浏览器截图代替 agent→center 协议证据。

## Out of Scope

- 重做监控信息架构、图表或页面视觉设计。
- 新增监控指标类型、告警功能或与本缺陷无关的 agent 能力。
- 通过伪造心跳、前端强制改状态或人工数据库修复来绕过真实传输链路。
- 改变“同一已绑定实例重复执行安装命令时保留现有凭据”的既有安装器合同；跨实例复用主机、绑定重置后的显式 re-enroll 语义需要独立产品决策。
- 修改 agent↔center DTO、HTTP endpoint、数据库 schema、生命周期定义或 Monitoring 页面轮询策略，除非实施中的 RED 证明这些边界另有独立缺陷。

## Root Cause and Boundaries

- 已确认代码级故障机制：runtime 每个 tick 先把当前请求入队，再 oldest-first flush；任意队头错误都会 `MarkAttempt` 后立即返回。队列项保存创建时的完整 MonitoringInstance ID、sync token 与 fingerprint，而所有远端错误目前都被当作可重试错误，外层只写日志并继续运行。因此一个永久被拒绝的旧身份条目可以持续阻塞所有新心跳，直到最长 72 小时的年龄裁剪。
- 已确认中心端 happy path 连续：有效 Bearer、bound 状态与 fingerprint 在事务写入前验证；接受的 heartbeat 会更新 heartbeat/sync 时间，后续 HostSample 推进生命周期。未发现有效请求被普通路径静默丢弃的独立断点。
- 项目 spec 已要求 agent 用 `errors.As` 消费 `*enroll.RemoteError`：身份/绑定错误不得无限重试，`invalid_json`/`invalid_request` 脏项必须记录后丢弃，5xx 才继续排队重试；当前 runtime 与该合同发生实现漂移。
- 尚无本次部署的 agent journal、脱敏队列元数据、反向代理 access status 或数据库行，因此“究竟是哪一个旧身份/凭据/指纹条件触发”仍是待运行时证据确认项。该限制不影响上述队头阻塞机制的确定性 RED 与修复。

## Notes

- 规划工作树：`/home/murray/code/houfeng/.worktree/monitoring-agent-heartbeat-ingestion`。
- 规划分支：`fix/monitoring-agent-heartbeat-ingestion`，基线 `bbf4f043`（已合并 v0.79.1 首次接入修复）。
- 前序任务仅修复创建/onboarding 入口，明确把 Agent 协议与采集模型列为范围外；本任务接续检查创建之后的数据面。
