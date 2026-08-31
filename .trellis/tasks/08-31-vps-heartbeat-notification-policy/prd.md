# 修复 VPS 心跳异常通知策略

## Goal

让 VPS 心跳异常通知严格遵循用户保存的阈值，默认容忍短暂网络抖动，并用稳定恢复窗口消除“失联后立即恢复”的通知骚扰；每条心跳通知都必须直接指出受影响的 VPS/监控实例，使用户无需打开页面即可判断对象和事件。

## Background

- 用户报告当前 VPS 在连续约 2–3 个心跳周期未上报时即触发失联通知，短暂网络波动会造成高频误报。
- 用户报告失联后很快又收到恢复通知，形成高频、低价值的成对提醒。
- 用户报告失联与恢复通知仅描述“最近 N 个周期未收到心跳”，未显示 VPS 身份。
- 用户提供的设置页截图显示：心跳间隔 `5 s`、失联判定阈值 `20 次`、扫描间隔 `60 s`，开始/升级/恢复通知均为开启；但现场仍按约 2–3 个周期触发。
- 代码追踪已确认：设置写入和持久化正常；周期扫描路径使用持久化阈值，但 Agent 成功同步后的即时判定路径没有传入阈值，并由 evaluator 静默回退到默认值 `3`。完整证据见 `research/root-cause.md`。

## Requirements

- R1. 全局“失联判定阈值” `N` 统一表示首次创建心跳失联异常的遗漏周期数；`N - 1` 及以前不得创建事件或发送通知。
- R2. 默认 `N` 从 `3` 调整为 `12`。默认心跳间隔 `5s` 时，首次异常约在持续失联 `60s` 后产生；实际发现时间允许再受扫描间隔影响。
- R3. 严重度边界固定为：`N <= missed < 2N` 为关注，`2N <= missed < 4N` 为告警，`missed >= 4N` 为严重。扫描跨越多个边界时只创建当前实际等级，不补发虚构的中间通知。
- R4. 任意用户保存的非旧默认值必须原样生效。例如 `N = 20` 时，首次关注/告警/严重边界分别为 `20 / 40 / 80`，成功同步即时判定和周期扫描不得使用不同阈值。
- R5. 升级迁移把全局 `incident_defaults.stale_threshold_intervals == 3` 视为旧默认并改为 `12`；其他全局值以及 `override_rules` 中的显式覆盖全部保持原值。升级前显式选择全局 `3` 与旧默认不可区分，这是已接受的兼容取舍。
- R6. 已有心跳失联异常只有在事件开始后收到连续 `3` 个不同同步批次的非回填实时心跳时才能恢复。恢复证据使用服务端 `received_at`，相邻批次接收间隔不得超过 `2 * heartbeat_interval`；回填、重复批次、事件开始前事实或稀疏心跳均不得恢复。
- R7. 恢复证据读取失败时必须 fail closed：保留当前 active incident，不创建恢复事件、不发送恢复通知。提高阈值本身也不得被误解释成“心跳已恢复”。
- R8. 心跳失联开始、升级和恢复的通知正文必须包含监控实例显示名与稳定 ID；显示名在投递边界移除换行/控制/bidi 风险、折叠空白并做 Unicode 安全长度限制，净化后为空时使用明确的“未命名监控实例”回退，稳定 ID 始终显示。
- R9. 事件流中的领域摘要保持简洁；发送给 Telegram/飞书并写入 `notification_records.summary` 的心跳消息使用同一份带主体信息的正文。
- R10. 保持现有开始/升级/恢复通知开关、多通道逐通道记录、维护/暂停/退役/归档行政恢复静默、回填抑制和 CAS 提交后通知语义。
- R11. 设置页必须解释 `N`、`2N`、`4N` 与 3 次实时心跳恢复的含义，并展示默认 `12` 在 `5s` 心跳下约为 `60s`，避免用户把该字段误解为告警等级阈值。
- R12. 阈值策略必须以显式、必填的领域对象传入 evaluator；不得保留可选参数或硬编码 `3` 的静默回退路径。
- R13. HTTP 接收的一次 heartbeat carrier 内所有 heartbeat 必须共享同一 `sync_batch_id`，并继续受共享的每批 256 条上限约束；这是恢复查询 768 行候选上界的入口不变量，不改变 Agent DTO shape 或正常 Agent 发包行为。

## Acceptance Criteria

- [x] AC1. 默认设置、无 `center_settings` 行的代码回退和新数据库列默认均为 `12`；默认 `5s` 心跳时页面说明约 `60s` 后首次判定。
- [x] AC2. 升级数据迁移把全局旧值 `3` 改成 `12`，保留全局 `20`、其他非 `3` 值和 `override_rules` 内显式 `3`；历史迁移文件不被修改。
- [x] AC3. 保存 `N = 20` 后，遗漏 `19` 个周期时无心跳事件/通知；达到 `20` 时关注，`40` 时告警，`80` 时严重。周期扫描与 `AfterSuccessfulSync` 使用同一策略。
- [x] AC4. evaluator 不再提供可省略的阈值参数；设置读取失败不会退回 `3` 或产生新的心跳通知。
- [x] AC5. active 心跳异常在第 1、2 个合格实时心跳后仍保持 active；第 3 个合格心跳后只恢复一次并按开关决定是否通知。
- [x] AC6. 回填心跳、相同 `sync_batch_id`、事件开始前心跳、相邻 `received_at` 间隔大于 `2 * heartbeat_interval` 或恢复查询错误均不能恢复 active incident。
- [x] AC7. 仅提高阈值但没有稳定实时心跳证据时，已有 active incident 不得生成虚假的恢复事件或恢复通知。
- [x] AC8. 开始、升级、恢复的 Telegram/飞书正文及对应通知记录都包含 `安全净化后的显示名（monitoring_instance_id）`；CRLF/控制/bidi 不得注入正文，超长多字节名称安全截断，空名称回退仍包含稳定 ID，且不同实例不会串用身份。
- [x] AC9. 关闭开始、升级或恢复通知时，对应通道保持 suppressed；维护/暂停/退役/归档行政恢复仍不产生通知记录或外发消息。
- [x] AC10. focused Go/Vitest、迁移与 current APP ACL 合同、真实 PostgreSQL 数据迁移/恢复读取、`make verify-go`、Node 22 `make verify-web`、`git diff --check` 全部通过且没有 SKIP 冒充数据库证据。
- [x] AC11. evaluator 对恢复次数不是 3 或最大 gap 不是 `2 * heartbeat_interval` 的直接 policy 构造 fail closed，不恢复 active incident。
- [x] AC12. Agent sync handler 在调用 service 前拒绝同一请求内混用 heartbeat `sync_batch_id`，并与 `syncing.MaxBatchItems` 共用单一数量上限。

## Confirmed Product Decisions

- 采用方案 A（平衡模式）：默认阈值 `12`，不是 `20`。
- 升级等级采用 `N / 2N / 4N`，不再使用旧的 `N - 1 / N / N + 2`。
- 恢复固定要求 3 次连续非回填实时心跳，本任务不增加新的可配置字段。
- 旧全局值 `3` 迁移到 `12`；其他自定义值和显式覆盖不变。
- 通知以监控实例显示名 + 稳定 ID 标识 VPS；不要求用户进入页面才能确定对象。

## In Scope

- 心跳失联 evaluator、周期扫描与成功同步后判定的数据流统一。
- 默认值、现有全局旧默认迁移、恢复证据查询及必要索引。
- 心跳通知正文与通知记录的主体补全。
- Settings 页面语义说明及默认 fixture 更新。
- 后端、Web、迁移/current APP ACL 和真实 PostgreSQL 回归测试。

## Out of Scope

- 本任务不重做通用通知渠道、订阅模型或消息中心 UI。
- 本任务不改变 Agent 心跳 DTO shape、采集频率或正常发包内容；Center 仅显式校验一次请求对应一个既有同步批次。
- 本任务不处理与心跳无关的监控类型。
- 本任务不新增通知冷却、摘要聚合、静默时段或按 VPS 单独配置恢复次数。
- 本任务不把监控实例强制关联到 VPS 资产；未关联实例仍以显示名和稳定监控实例 ID 标识。
- 本任务不修改历史已发布 migration；所有 schema/default/data 调整追加在新 migration 中。

## Risks and Compatibility

- 全局旧默认 `3` 与用户过去显式保存的全局 `3` 无法从现有 JSON 区分；本方案按已确认产品决策统一迁移为 `12`。标签覆盖中的显式 `3` 可区分，因此保留。
- 新恢复查询必须在任何 WindowAgg/去重前按共享 Agent ingress 上限保持候选集有界，并走包含 batch ID 的专用 covering partial index，避免长期 active incident 扫描/排序完整心跳历史；绕过 ingress 不变量时只允许 fail-closed 地延迟恢复。
- post-`0051` migration 必须注册 explicit current APP ACL fragment；本 migration 不新增 APP-managed relation/privilege，因此使用显式 empty fragment并更新 exact-source/count 合同。
- 应用代码回滚不会自动撤销已迁移的 `12` 或新索引；这是安全的前向数据状态，必要时用户仍可在设置页显式调整阈值。
