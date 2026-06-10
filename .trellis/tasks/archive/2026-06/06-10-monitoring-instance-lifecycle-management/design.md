# 监控实例生命周期管理设计

## Overview

本设计把 MonitoringInstance 管理拆成四个互相独立但可组合的状态面：

- 接入/观测生命周期：沿用 `lifecycle_status`。
- 运行控制：沿用 `monitoring_status`。
- 工作集可见性：新增 `archived_at` 和 `archived_reason`。
- 不可逆清理：通过受审查保护的 `permanent-cleanup` 删除实例和关联数据。

这样可以避免把“归档”“删除”“暂停采集”“退役观察对象”混在同一个字段里，也不会破坏现有 `lifecycle_status` 在 VPS 生命周期联动中的含义。

## Current Code Facts

- `monitoringinstances.Record` 目前没有归档字段，列表接口没有 scope 参数。
- `ListMonitoringInstances` 已经按 VPS 关联状态过滤掉只挂在 cancelled/archived VPS 上的实例，但没有实例自身的归档概念。
- `sync_batches.go` 在 `buildSyncPlan` 前已经写入心跳、样本、探测观测和 IP 质量报告，导致暂停/退役仍会产生新数据。
- `agent_plan.go` 对 `LifecycleRetired` 或 `MonitoringPaused` 返回空计划，这个行为应保留并扩展到归档。
- 监控实例相关数据里有一部分依赖外键级联，一部分只是对象 ID 引用，需要永久清理时显式删除。
- VPS 归档已有 review/apply 模式和前端确认弹窗，可作为管理审查和危险操作的工程参照。

## Data Model

在 `monitoring_instances` 表新增：

- `archived_at timestamptz null`
- `archived_reason text not null default ''`

`monitoringinstances.Record` 增加：

- `ArchivedAt *time.Time json:"archived_at,omitempty"`
- `ArchivedReason string json:"archived_reason,omitempty"`

不新增 `已归档` 生命周期状态。归档通过 `archived_at is not null` 判断。

迁移要求：

- 既有数据 `archived_at = null`，`archived_reason = ''`。
- 如果迁移框架需要 down migration，则删除这两个字段。
- SQL 查询列、scan 函数和前端类型同步更新。

## Domain Types

在 `internal/center/monitoringinstances/types.go` 新增管理相关契约：

- `ListScope`：`active`、`archived`、`all`。
- `ManagementReview`：实例、活跃 VPS 链接、计数、警告、阻塞项、可执行动作。
- `ManagementCounts`：心跳、主机样本、探测观测、日聚合、IP 质量报告、活跃事件、状态变更事件、通知记录、生命周期动作步骤、活跃 VPS 链接。
- `ManagementActions`：`can_retire`、`can_restore_lifecycle`、`can_archive`、`can_restore_archive`、`can_permanent_cleanup`。
- 输入类型：
  - `LifecycleActionInput{Reason string}`
  - `ArchiveInput{Reason string; ConfirmationName string}`
  - `PermanentCleanupInput{Reason string; ConfirmationName string}`

新增错误：

- `ErrInvalidManagementInput`
- `ErrManagementActionBlocked`
- `ErrArchivedMonitoringInstance`

## API Design

保持现有集合和详情接口，扩展集合：

- `GET /api/monitoring-instances?scope=active|archived|all`

新增子资源：

- `GET /api/monitoring-instances/{id}/management-review`
- `POST /api/monitoring-instances/{id}/lifecycle/retire`
- `POST /api/monitoring-instances/{id}/lifecycle/restore`
- `POST /api/monitoring-instances/{id}/archive`
- `POST /api/monitoring-instances/{id}/restore-from-archive`
- `POST /api/monitoring-instances/{id}/permanent-cleanup`

HTTP 行为：

- 无效 scope 或输入：`400`。
- 找不到实例：`404`。
- 状态或阻塞项不允许执行：`409`。
- 永久清理成功：返回清理结果，至少包含已删除实例 ID 和删除计数；前端随后导航回列表或展示已清理状态。

## Review Rules

`management-review` 必须包含前端执行危险操作所需的完整事实，避免前端自行拼接多个接口后做不一致判断。

计数来源：

- `monitoring_instance_heartbeats`
- `host_samples`
- `probe_observations`
- `monitoring_instance_host_sample_daily_aggregates`
- `ip_quality_reports`
- `active_incidents`
- `state_change_events`
- `notification_records`
- `asset_lifecycle_action_steps`
- `vps_monitoring_instance_links where unlinked_at is null`

活跃 VPS 关联返回 `vps_id`、display name、lifecycle status、usage status、link timestamps。已有 `assetlinks` summary 可复用时优先复用；否则在管理 repository 内做只读查询，避免前端再查。

阻塞建议：

- `archive` 阻塞：实例未退役，或存在活跃且未 cancelled/archived 的 VPS 链接。
- `permanent_cleanup` 阻塞：确认名不匹配；存在真实历史/观测/事件/通知/动作引用且实例未归档。
- `restore_archive` 阻塞：实例未归档。
- `restore_lifecycle` 阻塞：实例不是 `已退役`。

空误创建实例定义：

- 无心跳、主机样本、探测观测、日聚合、IP 质量报告、活跃事件、状态变更事件、通知记录、生命周期动作步骤。
- 允许存在活跃 VPS 链接，因为这正是误创建实例需要通过删除解除的关系，删除实例时由 `vps_monitoring_instance_links` 外键级联清理。

## State Transitions

### Retire

输入：`reason`。

行为：

- 要求未归档。
- 设置 `lifecycle_status = 已退役`。
- 设置 `monitoring_status = 暂停`。
- 清空 `enrollment_token_hash`、`enrollment_token_issued_at`、`sync_token_hash`、`pending_action_id`、`pending_action_command_id`。
- 保留 `binding_fingerprint` 和历史数据，用于审查和追溯。
- 写入状态变更事件，沿用现有 monitoring instance lifecycle 事件类型。

### Restore Lifecycle

输入：`reason`。

行为：

- 要求未归档且当前 `lifecycle_status = 已退役`。
- 设置 `lifecycle_status = 观察中`。
- 设置 `monitoring_status = 暂停`。
- 不自动恢复 token，不自动恢复监控采集。
- 用户后续通过既有接入/恢复监控路径显式恢复。

### Archive

输入：`reason` + `confirmation_name`。

行为：

- 在事务内锁定实例并重新计算 review。
- 要求确认名等于实例 display name。
- 要求 `lifecycle_status = 已退役`。
- 要求无活跃且未 cancelled/archived 的 VPS 链接。
- 设置 `archived_at = now()`，`archived_reason = reason`。
- 确保 `monitoring_status = 暂停`。
- 清空接入 token、同步 token、待绑定指纹和待执行 action。
- 归档后默认列表不可见。

### Restore From Archive

输入：可无 body 或 `reason`。

行为：

- 要求已归档。
- 清空 `archived_at`、`archived_reason`。
- 设置 `lifecycle_status = 观察中`。
- 设置 `monitoring_status = 暂停`。
- 不恢复 token 和 action。

### Permanent Cleanup

输入：`reason` + `confirmation_name`。

行为：

- 在事务内锁定实例并重新计算 review。
- 要求确认名等于实例 display name。
- 如果实例不是空误创建实例，则必须先归档。
- 先删除没有外键级联保护的引用：
  - `notification_records where object_type/object_id` 指向该实例，或 payload 中明确引用该实例时只处理已有 schema 支持的直接列。
  - `active_incidents where monitoring_instance_id = $1`。
  - `state_change_events where object_type/object_id` 指向该实例。
  - `asset_lifecycle_action_steps where object_type='monitoring_instance' and object_id=$1`。
- 再删除 `monitoring_instances where monitoring_instance_id=$1`。
- 依赖外键级联清理：心跳、主机样本、探测观测、日聚合、VPS 链接、IP 质量报告及 provider/service 子表。
- 返回删除前 review 计数和删除计数，供前端展示。

## Sync And Agent Gating

`validateAcceptedSyncBatch` 需要读取：

- `monitoring_status`
- `archived_at`

同步策略：

- 未绑定或 token 不匹配仍按现有错误返回。
- 已归档实例返回无效 token或归档错误，不能写入任何数据。
- `monitoring_status = 暂停` 或 `lifecycle_status = 已退役` 的实例，允许已绑定 agent 请求成功返回空计划，但不写入心跳、样本、探测观测或 IP 质量报告，也不推进 `last_sync_at`。
- 如果请求包含 action result，可以在暂停/退役场景下保留结果写入还是丢弃。为避免旧 action 继续影响状态，退役/归档已清空 pending action；本任务采用“暂停/退役短路，不写入观测与 action result”的一致策略。

`BuildSyncPlan` 扩展：

- `archived_at is not null` 与 `已退役` / `暂停` 一样返回空计划。

其他阻断：

- issue enrollment token、install command、binding confirm/reject/reset：归档实例返回 `409`。
- runtime resume：归档或退役实例返回 `409`；从退役恢复需走 lifecycle restore。
- metadata update：归档实例只读，返回 `409`。
- pending action 设置和 dispatch：归档、暂停、退役实例不应下发新 action。

## Frontend Design

### List

`listMonitoringInstances(scope?: MonitoringInstanceListScope)` 追加 query 参数。`MonitoringPage` 增加范围 segmented control：

- 当前：默认。
- 已归档。
- 全部。

批量面板只接收 active 且 `archived_at == null` 的实例。归档和 all 视图下仍可浏览，但不对归档项开放批量运行控制。

### Detail

详情页新增统一“管理实例”入口。建议放在 Watchtower header 的 action menu 中，同时详情主体新增管理摘要卡片，避免危险操作只藏在菜单里。

管理面板内容：

- 当前状态：生命周期、运行状态、绑定状态、归档时间。
- 关联：活跃 VPS 链接，提供跳转到 VPS 或现有解绑入口。
- 数据影响：review counts。
- 警告/阻塞项。
- 操作区：退役、恢复、归档、恢复归档、永久清理。

确认规则：

- 退役/恢复：要求 reason，可用普通确认。
- 归档/永久清理：使用 `ActionConfirmationModal`，需要输入实例显示名。
- 所有操作完成后刷新 detail、review、runtime facts、onboarding state；永久清理完成后回到监控实例列表。

## Tests

后端：

- 迁移测试：新增字段默认值、scan JSON 输出。
- store 测试：
  - list scope。
  - management review counts。
  - retire/restore/archive/restore archive 状态转移。
  - cleanup 删除非 FK 引用并删除实例。
  - cleanup 对非空未归档实例返回 blocked。
- sync 测试：
  - paused/retired 不写入心跳、样本、探测观测、IP 质量，返回空 plan。
  - archived 不接受继续同步或至少不写任何数据。
- handler/router/bootstrap 测试：
  - 新路由方法、错误码、输入校验、scope 校验。

前端：

- API helper 测试：scope query、review/action endpoint body。
- MonitoringPage 测试：默认 active、归档/all 切换、批量过滤。
- MonitoringDetailPage 或新管理组件测试：review 展示、阻塞项、确认名、操作后刷新、cleanup 后导航。

## Compatibility And Rollback

- 迁移向后兼容：新增 nullable/default 字段不会影响既有数据。
- API 默认 scope 为 active，保持现有页面默认行为，额外隐藏已归档实例。
- 归档不是硬删除，可恢复。
- 永久清理不可恢复，因此必须通过 review、确认名、归档前置条件保护。
- 如果发布后发现归档 gating 过严，可调整 review 规则；数据模型无需变更。

## Plan Review

该方案能直接修复已发现的问题：

- 误创建空实例有永久清理路径，不再只能暂停或归档。
- 已有实例升级造成重复实例的问题不会靠删除功能掩盖；管理 review 会让重复实例和关联关系可见，并允许清理空实例。
- 暂停/退役仍写入观测数据的隐藏问题被纳入同步门禁。
- 归档与生命周期分离，避免把 `lifecycle_status` 语义进一步混乱化。

主要风险和对应约束：

- 永久删除范围过大：通过 review、确认名、事务内重算和“非空先归档”降低风险。
- 阻断 agent 同步影响旧 agent：暂停/退役返回空 plan 而不是错误；归档才强制撤销 token。
- 前端入口过散：统一管理面板承载所有实例管理动作，列表只做范围切换和导航。
