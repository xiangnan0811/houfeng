# 全局命令审计中心技术设计

## 1. Architecture and ownership

本功能沿用现有单体边界，不增加依赖或后台服务：

1. `0050_extend_command_action_audit.sql` 把 0046 表升级为永久身份快照、可表达 rejected 且可删除原实体的 append-only 元数据表。
2. `internal/center/store/command_actions.go` 继续拥有唯一审计写 helper；排队、派发、完成调用点仍在原事务，可信拒绝由同一 helper 写入。
3. 新建 `internal/center/commandaudits/types.go` 定义只读领域契约，新建 `internal/center/store/command_audits.go` 实现固定两次 SQL 查询的 action 聚合分页。
4. 新建 `internal/center/http/handlers/command_audits.go` 和 `command_audits_cursor.go`，由 handler 负责输入规范化、时间快照和 opaque cursor，由 store 负责 SQL 过滤、keyset 和安全映射。
5. 新建 `web/src/pages/CommandAuditPage.tsx` 作为私有 controller；`web/src/pages/command-audit/` 分别拥有 filter model、筛选 UI、表格和事件时间线。命令审计读取与既有 event/incident 读取由 `web/src/lib/observabilityApi.ts` 组成 route-lazy domain façade，继续复用 `api.ts` 的 `withQuery` 与唯一 transport，避免 route-private endpoint 实现进入启动 chunk。
6. 命令展示定义从 monitoring detail 提升到 `web/src/config/commands.ts`，命令抽屉和审计页共享同一来源。

不创建父子 Trellis 任务：迁移、写路径、API 与页面共同构成一个端到端可验收闭环，拆分会让中间任务无法独立提供用户价值；执行计划仍设置分层 review gate。

## 2. Database migration

### 2.1 Columns and backfill

在 `monitoring_instance_command_action_audit` 增加：

- `monitoring_instance_name_snapshot text not null default ''`
- `actor_username_snapshot text not null default ''`
- `actor_display_name_snapshot text not null default ''`

迁移只回填空快照：实例名从 `monitoring_instances.display_name`，actor 名称从 `users` 获取。重复执行或实体后来改名不得覆盖已经形成的快照。空默认既允许旧二进制继续原式 INSERT，也让 API 能对极旧/回滚写入回退到稳定 ID。

### 2.2 Referential lifetime and constraints

迁移动态删除审计表上的所有外键（当前仅实例和 actor），然后解除 `action_id NOT NULL`。用可重复的 `DO` block 安装具名约束：

- event type 仅 `queued|dispatched|completed|rejected`
- sensitivity 仅 `standard|sensitive`
- source 仅 `web|agent_sync`
- rejected 时 action ID 必须为空，其他事件必须非空
- rejected 必须来自 web
- rejected 的 `details` 必须精确等于 `{"reason":"sensitive_confirmation_required"}`
- 任意 `details` 顶层不得含 `stdout` 或 `stderr`

原有 queued/dispatched/completed + `{}` details 均满足新约束。索引保留：

- `(monitoring_instance_id, occurred_at desc, audit_id desc)`
- `(action_id, occurred_at asc, audit_id asc)`
- 新增 `(occurred_at desc, audit_id desc)` 全局时间索引

### 2.3 Single write helper

`insertCommandActionAudit` 改为 `INSERT … SELECT`：

```sql
insert into monitoring_instance_command_action_audit (...snapshots...)
select ..., mi.display_name, nullif($actor_user_id, ''),
       coalesce(u.username, ''), coalesce(u.display_name, '')
from monitoring_instances mi
left join users u on u.user_id = nullif($actor_user_id, '')
where mi.monitoring_instance_id = $monitoring_instance_id
  and ($actor_user_id = '' or u.user_id is not null)
```

helper 检查 `RowsAffected() == 1`，否则返回完整性错误。这样解除外键后仍不可能为从未存在的实例或伪造 actor 生成新记录。queued/dispatched/completed 的调用点不直接写 SQL，并保留其现有事务边界。

## 3. Trusted rejection state machine

handler 先完成 JSON 与 command ID 校验。对敏感且未确认的请求执行以下只读判定：

```text
decode/known command
  -> GetMonitoringInstance
     -> infrastructure error: 500
     -> missing or not executable: 400 confirmation required, no audit
     -> bound + active + monitoring enabled:
          insert rejected audit
          -> write failure: 500
          -> success: 400 confirmation required
```

此分支在任何 action ID 生成之前结束，因此不会排队或触碰 `last_action`。对已确认敏感命令和标准命令继续现有实例校验、action ID 生成与 queue 流程。可执行条件与现有正常路径一致：未归档、binding 为 bound、monitoring 非 paused。

## 4. Read model and API contract

### 4.1 Domain types

`commandaudits` 定义：

- `Query`：固定 `StartedFrom`/`StartedTo`、规范化筛选、limit、可选 `BeforeStartedAt`/`BeforeID`
- `Action`：`id`、可选 `action_id`、实例身份/存在状态、command/sensitivity/outcome、可选 actor、started_at、events
- `Event`：audit ID、event type、source、occurred_at、可选 exit code、可选规范化 rejection reason
- `Page`：items、`HasMore`

不在类型中定义 details/stdout/stderr。

### 4.2 Query shape and no-N+1 guarantee

Store 固定执行两次查询：

1. 从窗口内 `queued|rejected` 起始事件选 action，按起始快照筛选；用 action 索引判断 completed/dispatched 并计算 outcome；应用 `(started_at,id) < (cursor_time,cursor_id)`，排序后取 `limit+1`。
2. 对当页 action IDs 使用一次集合查询取所有 `occurred_at <= fixed_to` 的事件并排序，再在 Go 中归组。

起始事件是现有写入不变量（普通 action 恰好先 queued，拒绝本身即起始）；固定上界让后续新派发/完成事件不会改变同一 pagination snapshot。第二次查询只取返回页，不随 action 数量增加 SQL 次数。若 PostgreSQL `EXPLAIN` 显示该设计不能在代表性数据上利用全局时间/action 索引，则停止并回到设计。

`monitoring_instance` 在稳定 ID/实例名快照上匹配；`actor` 在 user ID/username/display name 快照上匹配。输入先把 `\\`、`%`、`_` 转义，再使用 `ILIKE ... ESCAPE '\\'`。action ID、command、sensitivity 精确匹配。

### 4.3 Cursor snapshot

首次请求由 handler 使用单次 UTC `now` 固定边界：24h/7d/30d 为 `now-duration .. now`，all 为无下界 `.. now`，custom 必须有两个 RFC3339 边界且 from < to。默认 limit 20，最大 100。

下一页 cursor 是 Raw URL-safe base64 编码的版本化 JSON：

```json
{
  "v": 1,
  "filters": {"window":"30d","monitoring_instance":"","command_id":"","sensitivity":"","outcome":"","actor":"","action_id":""},
  "started_from": "...",
  "started_to": "...",
  "limit": 20,
  "before_started_at": "...",
  "before_id": "act_..."
}
```

decode 后重新执行与首次请求相同的枚举、长度、边界和排序键验证；cursor 请求不得携带任何其他 query key。base64 只提供 opaque transport，不作为授权边界；所有值仍按不可信输入处理。

### 4.4 Response

```json
{
  "items": [
    {
      "id": "act_123",
      "action_id": "act_123",
      "monitoring_instance": {"id":"mi_1","name":"Tokyo Edge","deleted":false},
      "command_id": "uptime",
      "sensitivity": "standard",
      "outcome": "succeeded",
      "actor": {"user_id":"usr_1","username":"admin","display_name":"管理员"},
      "started_at": "...",
      "events": [
        {"audit_id":"cmd_aud_1","event_type":"queued","source":"web","occurred_at":"..."},
        {"audit_id":"cmd_aud_2","event_type":"completed","source":"agent_sync","exit_code":0,"occurred_at":"..."}
      ]
    }
  ],
  "next_cursor": "..."
}
```

空实例快照回退实例 ID；actor 显示名依次回退 display name → username → user ID。没有 actor 的 agent-only 数据返回 null。`deleted` 由起始查询 left join 当前实例计算。

## 5. Monitoring-instance cleanup integration

`ManagementCounts` 增加 `CommandActionAuditCount`，`EvidenceCount()` 纳入该值；管理 review SQL 加 count 子查询。永久清理仍只删除原有可删除引用和实例本身，不触碰审计表，因此结果的 `DeletedReferenceCount` 不含审计。归档实例依旧允许永久清理；未归档且存在审计时不再被误判为空实例。Web 管理确认区显示审计计数并明确“命令审计将永久保留”。

## 6. Web state and rendering design

### 6.1 Filter controller

`filterModel.ts` 是 URL 与 API 查询的唯一规范化 owner：默认 `30d` 不序列化；custom 才写两个时间；空值和默认值删除；非法值回退默认后用 replace canonicalize。页面保持 applied filters、drawer draft、items、next cursor、expanded IDs 和 request generation。筛选应用时先清空 items/cursor/expanded，再发首次请求；加载更多只传 cursor，并丢弃过期 generation 的响应。

### 6.2 Presentation components

- `CommandAuditFilterPanel.tsx`：时间、实例、命令、结果与高级筛选入口。
- `CommandAuditFilterDrawer.tsx`：完整草稿、Apply/Reset/Cancel；关闭不污染 applied state。
- `CommandAuditTable.tsx`：DataTable action 汇总、局部横向滚动、语义展开按钮、当前实例链接/删除态文本。
- `CommandAuditEventTimeline.tsx`：只从显式 Event allowlist 渲染事件类型、时间、来源、exit code、拒绝原因。

页面顶部明确“仅展示命令、身份、时间和结果元数据，不保存命令输出”。组件不得遍历未知对象字段，因此附带的恶意 stdout/stderr 不可能进入 DOM。

### 6.3 Shared command metadata

`web/src/config/commands.ts` 导出 command 类型、`COMMANDS`、`COMMAND_LABELS`、select options。monitoring detail 删除本地重复定义并导入共享配置；审计页使用相同 label/sensitivity。

### 6.4 CSS and bundle ratchets

命令审计页面组合现有 `page-stack`、`filter-bar`、observability drawer、`DataTable`、`metadata-list` 与 events scroll owner，只保留宽表最小宽度规则；Events 空态同时复用 `page-sub`，因此新增页面不增加 CSS rule/declaration/repeated-selector debt。fresh build 后把 CSS source/raw/gzip budget 向下锁到实测值。

完整 Web gate 证明把 `listCommandAudits` 放入 eager `api.ts` 会让 entry JS 超预算。最终把只被 lazy routes 使用的 event/incident/command-audit helpers 收敛到 `observabilityApi.ts` async shared chunk；AppShell 使用的 `getDashboard` 等仍留在 eager façade。该拆分不得改变 wire shape，且 entry/max-async 必须同时通过现有 budget；预算不因新增路由而上调。

## 7. Compatibility, security, and rollback

- 新二进制依赖 0050；部署仍按现有“先迁移再启动”流程。
- 回滚到旧二进制后，snapshot 默认值和旧事件约束让旧 INSERT 继续成功；rejected 行对旧二进制是只读未知数据，不影响原写路径。
- 回滚二进制不会恢复外键，这是有意的数据保留语义；如需完全回退，只能在确认没有 orphan audit 后另写前向迁移，不能危险地自动重建级联 FK。
- API 沿用 session + same-origin；未来角色安全门由 #381 处理。
- 查询不读取 output 字段；日志和错误不回显 cursor 内容或数据库 details。

## 8. Verification evidence layers

- 单元/fake：SQL 文本约束、事务错误路径、handler 验证、cursor、Web state/render。
- 真实 PostgreSQL：0046 升级/重复、旧式 INSERT、删除保留、真实聚合/筛选/keyset/rejection/cleanup，fresh install 与 EXPLAIN。
- fixture browser：10×3 core、accessibility、fail-closed、交互与窄屏滚动。
- production bundle：fresh build 后 CSS 为 311047 source bytes / 293254 raw / 38108 gzip；entry JS 为 110736 gzip，观测 API 形成独立 async shared chunk，预算仅向下收紧。
- staging：真实部署 smoke 只在环境可用时记录；缺失时明确标注未执行。
- PR CI：required checks 单独记录，不能替代 PostgreSQL 或浏览器证据。
