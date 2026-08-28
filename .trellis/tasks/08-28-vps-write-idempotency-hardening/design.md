# VPS 写入生命周期与幂等合同设计

## 1. Design choice

采用“认证 AppShell 内存 registry + 服务端事务 receipt”的组合方案。

- 只在页面卸载时 abort 请求不能证明服务器未提交，也不能覆盖浏览器历史返回或 Legacy/Overview 双视图。
- 只增加数据库业务唯一约束无法表达“同一逻辑请求重放”，且服务、域名、经验记录并不存在可靠的自然唯一键。
- AppShell registry 负责浏览器会话内的单一逻辑操作与稳定 key；PostgreSQL receipt 负责网络未知结果和进程/浏览器边界。两层缺一不可。

三个 finding 保持在一个任务内：P2-01 与 P2-02 共用同一 create identity；P3 独立但改动小，并与同一轮合同测试/外部复审一起交付。无需创建 parent/child 树。

## 2. Authenticated shell registry

将 store 移到 web shared state 层，并由 `AuthenticatedAppShell` 通过 context provider 创建一次：

```text
RequireAuth
└── AuthenticatedAppShell(key=user.user_id)
    └── VPSWriteRegistryProvider
        └── Outlet
            └── VPSDetailPage
                ├── LegacyVPSDetail
                └── VPSOverviewManagementActions
```

`key=user.user_id` 保证用户变化时 registry 销毁。生产组件必须消费 provider；direct component tests 可显式注入测试 store，不使用 module-global singleton。

active owner 仍按 `vpsId` 建索引：

```ts
type VPSWriteOwner = {
  vpsId: string
  operation: VPSWriteOperation
  token: string
  viewToken: string
  generation: number
  startedAt: string
  monitoringInstanceId?: string
}
```

`begin` 只在该 VPS 无 owner 时成功；`finish` 只接受当前 `vpsId + token`。Legacy 与 Overview 都从 `useSyncExternalStore` 读取同一 snapshot，因此任一视图 pending 时另一视图显示“操作处理中”并禁用/拒绝提交。不同 VPS 使用不同 map entry，继续并行。公开 snapshot 只含上述协调元数据；digest/key 不得作为 owner 字段发布。

`VPSDetailPage` 为每个实际 view authority 生成 `viewToken`。页面若在旧 owner pending 时重新挂载，或 gate 已从 Legacy 切到 Overview，新 view 会观察到不同 token；当旧 owner 从 snapshot 消失时触发 gate/overview authoritative reload。当前 view 自己的正常 owner settle 不额外打断其 mutation-owned refresh。

## 3. Stable create attempt identity

registry 内部另存以 `vpsId + operation` 为键的 create attempt identity，不把原始请求体保存在 shell：

```ts
type VPSCreateAttempt = {
  requestDigest: string
  idempotencyKey: string
}
```

该 attempt 只存在于 registry 私有 map；`prepareCreate` 只向 exact owner 的调用栈返回所需 digest/key，`useSyncExternalStore` snapshot、UI、调试输出与测试失败信息均不可获得这些材料。

create submit 先取得 provisional owner，再用 Web Crypto SHA-256 对 canonical JSON request identity（包含 VPS ID、operation 与实际 wire body）计算 digest，并由 exact owner token 绑定/复用 key。provisional owner 在 digest await 期间已经阻止第二次写入。

settle 规则：

| 结果 | registry 行为 |
|---|---|
| transport failure / 未确认失败 | release active owner，保留 digest + key |
| 相同 body 再次提交 | 复用保留 key |
| body digest 改变 | 生成新 key并替换 attempt |
| `409 idempotency_key_reused` | 为当前 digest 轮换 key，再 release |
| 服务端确认成功 | 清除匹配 attempt，再 release |

清理 attempt 时必须同时匹配 operation、digest 与 key，旧回调不能清除后继尝试。subscription 移除组件本地 UUID ref，和另外四个 create 统一使用 registry identity。

## 4. HTTP and domain contract

新增 shared `internal/center/createidempotency` 包，提供：

- key 长度/字符规范化；
- canonical JSON SHA-256 digest helper；
- `ErrInvalidIdempotencyKey` 与 `ErrIdempotencyKeyReused`；
- operation namespacing helper，避免四类 key 的 advisory-lock 相互阻塞。

subscription 现有公开 error 与 helper 保持兼容，内部可委托 shared 包；不改变已发布 subscription receipt schema。

四个 VPS scoped POST 在 strict JSON decode 与 domain validation 后、任何 create 调用前校验单值 `Idempotency-Key`。首次创建返回 `201`；replay 返回 `200`；key/digest mismatch 返回 `409 idempotency_key_reused`；缺失/非法 key 返回 `400 invalid_idempotency_key`。错误响应只包含稳定 allowlisted code，不暴露 digest、key、SQL 或内部错误。

## 5. Transactional persistence

新增 successor migration `0062_create_vps_create_idempotency.sql`，不修改 `0061` 或更早 migration。migration 创建四个显式 receipt 表：

- `experience_log_create_idempotency` → `experience_logs(experience_log_id)`；
- `asset_service_create_idempotency` → `asset_services(service_id)`；
- `asset_domain_create_idempotency` → `asset_domains(domain_id)`；
- `vps_monitoring_instance_create_idempotency` → `monitoring_instances(monitoring_instance_id)` 与 `vps_monitoring_instance_links(link_id)`。

每表以 `idempotency_key` 为主键，保存 `request_digest`、结果 FK 与 `created_at`；key/digest check 与 `0061` 一致，结果删除时 cascade receipt。APP current ACL 只授予 runtime `SELECT/INSERT`，不授予显式 update/delete。

每个 repository 新增 scoped idempotent create 方法。事务顺序统一为：normalize/validate → begin → namespaced advisory xact lock → lookup receipt → mismatch/replay 或 insert result → insert receipt → commit。monitoring create 保持 instance + link + receipt 在已有同一事务。collection create 继续调用原非幂等方法，wire contract 不变。

## 6. DTO base type and format

`vps_subscription_create_fields.json` 从四维升级为五维：`name`、`type`、可选 `format`、`required`、`nullable`。日期字段表示为：

```json
{"name":"renew_at","type":"string","format":"date","required":false,"nullable":true}
```

普通 nullable string 表示为 `type: string` 且无 `format`。TypeScript 增加同源 `ISODate = string`，scoped create 的 `started_at` / `renew_at` 使用 `ISODate | null`。两个 mirror 只在 alias 定义精确为 `string` 且字段使用该 alias 时输出 `format: date`；raw `string | null` 永远只是 nullable string。Go `subscriptions.Date` / `OptionalDate` 同样输出 `type: string, format: date`。manifest decoder 验证 format 只能缺失或为 `date`，并继续拒绝缺失/null语义键与未知类型。

## 7. Compatibility and rollback

- 这是四个 VPS scoped POST 的有意 wire hardening：旧客户端缺 key 将收到稳定 400；仓库内所有调用点同时升级。
- response DTO 不变；replay 仅把成功状态从首次 `201` 区分为 `200`。
- migration 是 additive；代码回滚时新 receipt 表可留存，不影响旧读写。若必须数据库回滚，只能在确认没有依赖新幂等合同的客户端后通过后续 migration 处理，不能修改已发布文件。
- 实现先停在 dirty feature worktree 供外部审查。审查通过并获用户授权后，交付必须走 feature PR → protected main → Release Please release PR → GitHub Release/publish-images；发布产物核验完成后才归档任务和清理 worktree。

## 8. Security and privacy

- registry 仅保存 SHA-256 digest、UUID key 和操作元数据，不保存表单正文。
- 日志、HTTP 错误、UI 与测试失败信息不得输出真实 idempotency key、digest 或潜在 note/details 内容。
- receipt 无 actor secret、credentials 或 raw body；FK 保证 replay 指向真实结果。
