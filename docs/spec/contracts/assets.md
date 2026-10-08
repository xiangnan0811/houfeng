# 资产合同

## VPS 维护的作用范围

`GET /api/vps/{id}/maintenance-review` 返回当前监控、明确专属探测和共享探测及预览摘要。
`POST /api/vps/{id}/maintenance` 使用摘要、原因和明确选中的共享 Target ID 开始维护；
`DELETE /api/vps/{id}/maintenance` 带原因结束本次维护。所有变更在资产关系锁下同事务记账。

- 默认覆盖当前监控和明确专属探测；共享探测必须逐个确认。原有暂停或独立维护不被改写。
- 每次直接监控控制操作递增控制版本，即便重复设置同一个值。维护保存原控制及自己产生的版本。
- 多台 VPS 可以共同持有同一共享探测的维护作用，只有最后一个作用结束且控制版本没有被后续操作取代时才恢复原控制。
- 结束维护不覆盖期间的人工暂停或维护；归档结束本 VPS 的维护作用但不恢复采集或探测。

## VPS 独立事实与全新部署边界

- VPS 对应实际资源实例，重装沿用身份，回收后重新购买新建；生命周期只有 `active`（管理中）和 `archived`。临时关机、维护和暂停不改变生命周期。
- 用途使用任意多选 `usage_tags[]`，trim、去空、按原始大小写去重；普通 `labels[]` 独立。用途不联动生命周期、采集、告警或续费。
- `renewal_decision` 只有 `unreviewed|keep|cancel`；`renewal_reason` 与 `renewal_review_at` 保存原因和复核时间。更改意向不得改写订阅或服务商自动续费事实。
- `validity_mode=fixed|unlimited|unknown` 独立于订阅；只有 fixed 必须有 `expires_at`（YYYY-MM-DD），其他模式必须为 null。合并 PATCH 后校验完整状态，避免只改一半。
- `auto_renew_check=unchecked|enabled|disabled|never_enabled|unsupported` 是人工核对服务商事实；unchecked 的 `auto_renew_checked_at` 必须为空，其余必须保存核对时间。不续费且尚未核对仍需提示；不能推断服务商已关闭。
- `acquisition_source` 独立记录购买、抽奖、赠送等来源，不将来源解释为续费方式。
- 新模型仅保证全新安装；追加迁移保持已发布迁移和冻结权限基线不变。遇到需要转换的业务数据明确拒绝，不做状态映射、自动删除或线上重建。
- JSON 创建、更新和导入拒绝旧生命周期、旧续费决策和 `usage_status` 字段，不提供兼容别名。导入复用同一 Normalize/Validate 规则。

## Asset Ledger providers

`db/migrations/0016_create_asset_ledger.sql` 是 post-V1 Asset Ledger 的 schema 入口，当前落 `providers` 服务商主数据表：

- `providers.provider_id` 使用 `ids.New("pv")` 生成，字段和 JSON contract 保持英文稳定值。
- `name` 必须通过数据库 `providers_name_not_blank` 约束保证 trim 后非空；领域层也必须在 create / patch 时校验。
- `rating` 是 nullable `integer`，只允许 `null` 或 `1..5`，数据库约束为 `providers_rating_range`。
- `labels` 使用 `text[] not null default '{}'`；领域层负责 trim 和过滤空标签。
- provider CRUD 不得自动改写、规范化或 backfill `monitoring_instances.provider`。`monitoring_instances.provider` 仍是 Fleet Observability 的监控实例元数据字符串，Asset Ledger provider 是独立资产层主数据。

## Asset Ledger VPS assets

`db/migrations/0017_add_vps_assets.sql` 添加 `vps_assets`，代表资产层 VPS 账本。它依赖 `providers.provider_id`，但仍与 Fleet Observability 的 `monitoring_instances.provider` 字符串保持分离。

- `vps_assets.vps_id` 使用 `ids.New("vps")` 生成。
- `provider_id` 可为 `null`；存在时必须引用 `providers(provider_id)`，并在 provider 删除时 `on delete set null`。
- `provider_name` 是导入 / 展示兼容字符串，不能创建、更新或回填 `providers`。
- `display_name` 必须由数据库 `vps_assets_display_name_not_blank` 约束保证 trim 后非空；领域层 create / patch 也必须校验。
- `lifecycle_status`、`renewal_decision` 使用上述机器值；数据库约束与领域校验共同保护。创建和普通 PATCH 仅可写 active，归档/恢复由专用动作执行。
- 列表默认 `asset_scope=current`，只返回管理中资源；`archived` 返回归档资源，`all` 返回全部。可按 `usage_tag` 精确匹配任意一个用途；旧 `usage_status` 查询参数返回 400。
- VPS 是业务状态主体，Subscription 是账单事实，MonitoringInstance 是运行观测。普通 CRUD 不得改变监控、命令、探测或订阅。
- `ssh_port` 默认为 22，范围 1..65535。`archived_at` 只能由生命周期动作派生。
- `PATCH /api/vps/{id}` 对实际变更要求 `If-Match`；读取当前行后合并输入，校验有效期与核对时间成对事实，避免部分更新产生矛盾。
- 归档后允许窄范围补充 `auto_renew_check/auto_renew_checked_at`、`renewal_reason/renewal_review_at` 和 note；名称、用途、生命周期等仍不可普通修改。核对与补充修订同事务写入 `experience_logs` 的 before/after，不能借补录恢复资源。
- 续费意向变更保留 `renewal_decisions` 历史。保存 cancel 不调用订阅自动续费更新，也不改变用途。
- subscription summary 属于订阅查询；运行摘要在 HTTP 展示层补充，不成为生命周期权威。

### Scenario: 独立有效期、用途与续费

- 无订阅的 VPS 可直接设置 fixed + expires_at；unlimited / unknown 必须清除 expires_at。
- `active + cancel`、`archived + keep` 均可表达真实事实；用途任意，不设跨轴隐式规则。
- 服务商 enabled 与决定不续费可以并存，应提示人工核对；禁用事实必须带核对时间。
- 创建旧 idle/testing/to_migrate/to_cancel/cancelled 或 PATCH 旧 observe/migrate/auto_renew_cancelled/replaced 返回 400。
- 回归覆盖日期合法性、部分 PATCH 合并后校验、任意用途去重、无订阅有效期、旧字段拒绝及意向不修改订阅。

## Asset lifecycle actions

- 生命周期仅 `active`（管理中）、`archived`（已归档）。`POST /api/vps/{vps_id}/archive` 直接结束使用并归档，不要求先取消或改变续费意向；旧取消入口已移除。
- `GET /api/vps/{vps_id}/archive-review` 返回关系影响、warnings、结构化 `blocker_details`、`online_evidence`、`eligible` 和 `preview_digest`。没有订阅、服务或域名、潜在扣费以及共享探测核对事项只产生说明或跟进，不阻止归档。
- 提交字段为 `confirmation_name`、非空 `reason`、`preview_digest`、`idempotency_key`；从未形成有效 Agent 会话时，另要求 `never_connected_confirmation=true`，并用原因记录人工确认依据。
- 提交取得资产图独占事务锁后重新读取所有依赖、持久在线证据与健康观察记录。条件变化返回 HTTP 409 及最新 review；预览有效期为 5 分钟，`preview_expires_at` 明确返回截止时间，过期提交返回 `archive_preview_stale`；不同请求复用幂等键返回 `archive_idempotency_conflict`。同一请求重试返回原始结果，不能重复写成功生命周期事件。
- 归档事务将 VPS 置为 archived，保存归档快照，退役当前监控、将所有历史会话降为 evidence_only、审计清除待执行命令、结束当前服务/域名关联、停止明确专属 Target，并写生命周期审计及跟进事项。任何一步失败均回滚业务变化。
- 共享服务、域名和 Target 保留；归档仅结束该 VPS 的关联。共享 Target 保持当前控制并留下核对事项。常规异常以 `incident_closed_by_management` 及原因结束，不发送自然恢复通知。
- `POST /api/vps/{vps_id}/restore-from-archive` 要求非空 reason，仅恢复 VPS 为 active、用途为 `['闲置']`；保留归档快照和续费意向，不恢复历史监控、凭据权限、关联、探测或待执行命令。重新接入必须显式操作。

### 连续 180 分钟归档安全观察

- 安全事实来自 `monitoring_instances.ever_connected/last_trusted_online_at` 及永久保留的 `monitoring_agent_sessions`，包含同一 VPS 下所有历史实例和所有接入阶段；原始心跳清理不能清除安全事实。
- 每 5 秒由 Center 同一运行角色执行接收就绪检查及 PostgreSQL 读写；`receiver_health` 最新健康记录有效期为 15 秒。启动时同步使旧观察失效；重启、接收/持久化故障、记录超时及异常时钟跳变均重置连续健康起点。健康观察不获取资产图锁；归档先读取健康快照，完成业务写入后才短暂锁定健康记录、重新检查连续性和有效期并提交，避免业务事务阻塞观察。查询发现时钟异常会立即锁存故障，时钟在下一次观察前恢复也不能沿用原健康窗口。
- 只有接收链路连续健康且所有实例/会话连续 180 分钟无可信实时在线信号，才可归档。180 分钟整允许，179 分 59 秒阻止；Center 接收时间是唯一时间权威。会话新签发时同样重新开始该阶段观察，不能以无性能样本冒充未接入。
- 真正从未形成有效会话的对象可使用人工确认例外。曾接入后重装成为“待接入”的对象不得使用例外。暂停、维护、退役的 Agent 仍可提交最小在线证据，并继续阻止归档。
- `online_evidence` 返回健康代次、健康起点、最新检查时间、所有实例/会话最后可信在线时间和最早可归档时间；接收链路不健康时最早时间为空。没有强制归档或自动预约归档。
- 归档后 Agent 再上线只更新持久在线事实并去重创建待核对跟进；不能恢复 VPS 或常规监控。

### 人工迁移与续费事实

- 迁移通过 VPS 跟进事项记录来源、目标及结果，不改变 VPS 生命周期、续费意向、服务关联或 Agent 状态；迁移结果通过显式关联结束/新增及跟进结果表达。旧 `start-migration` 生命周期入口已移除。
- 决定不续费、续费记录和服务商自动续费核对是独立事实。用户确认新的资源有效期时独立写 VPS 有效期，不由账单日期自动覆盖；归档也不代表服务商已停止扣费。

### 验证入口

- `archive_safety_test.go` 覆盖精确 180 分钟、未接入例外、会话无样本、健康超时及预览摘要。
- `asset_lifecycle_archive_safety_postgres_test.go` 使用真实 PostgreSQL 覆盖持久信号、归档/心跳锁竞争、并发重试、末步失败完整回滚与恢复边界。

## Asset Ledger VPS MonitoringInstance links

`db/migrations/0019_create_vps_node_links.sql` 添加历史 link 表，`0029_rename_nodes_to_monitoring_instances.sql` 将其迁移为 `vps_monitoring_instance_links`，用于连接资产层 VPS 与 Fleet Observability 的 `monitoring_instances`。它是关联历史表，不是 MonitoringInstance 状态机的一部分。

- `vps_monitoring_instance_links.link_id` 使用 `ids.New("vnl")` 生成，避免用 `(vps_id, monitoring_instance_id, linked_at)` 做 API identity。
- `vps_id` 必须引用 `vps_assets(vps_id)`，`monitoring_instance_id` 必须引用 `monitoring_instances(monitoring_instance_id)`；删除 VPS 或 MonitoringInstance 时可以级联清理 link 历史。
- active link 定义为 `unlinked_at is null`。`idx_vps_monitoring_instance_links_pair_active` 必须保证同一 `(vps_id, monitoring_instance_id)` 同时最多一条 active link。
- unlink 必须写 `unlinked_at`，不得物理删除；如果提供 note，只更新 link note，不改 MonitoringInstance 或 VPS 业务字段。
- link / unlink 不得改写 `monitoring_instances.provider`、MonitoringInstance `lifecycle_status`、`monitoring_status`、`current_health_status`、Target、Agent 或 subscription。
- VPS item/list API 可以补 `active_monitoring_instance_link_count`，VPS detail 可以返回 active MonitoringInstance 摘要；这些摘要通过 `internal/center/assetlinks.Repository` 查询，不要把 MonitoringInstance 查询 SQL 塞进 `store/vps_assets.go`。
- MonitoringInstance 侧 VPS 摘要使用独立 `/api/monitoring-instances/{monitoring_instance_id}/vps` 查询，不把资产字段混入基础 `monitoringinstances.Record`。

## VPS-scoped MonitoringInstance creation

`POST /api/vps/{vps_id}/monitoring-instances` 是普通 agent 接入的主合同。它从 VPS 创建 MonitoringInstance 并在同一个事务内写入 active `vps_monitoring_instance_links`，避免先创建孤立监控实例再回 VPS 关联。

- 请求允许覆盖 `display_name`、`group`、`region`、`city`、`provider`、`labels`、`note`、`link_note`，另可发送 `clear_fields`。创建时复制 VPS 资料形成独立副本，之后编辑 VPS 不覆盖实例。region 默认按 VPS region→country→空，city 按 city→datacenter→空，provider 按 provider_name→空；未知可以为空，不写入“未确认”“未关联服务商”等占位词。link_note 默认空。name/group/labels/note 既有规则不变，历史占位内容不清洗。
- 为兼容已有请求，region/city/provider 省略或空串仍表示继承；显式清空必须发送空值及 `clear_fields`。该数组只允许 `region|city|provider`，trim 后排序去重；未知字段或与同字段非空覆盖冲突返回 400。清空在继承后施加。前端输入 placeholder 仅作显示，不能作为实际 value 提交；重新填值会移除对应清空意图。
- 创建出的 MonitoringInstance 默认是运行观测附属事实：`lifecycle_status='待接入'`，binding / health / heartbeat 等仍由 onboarding 和 agent sync 推进。
- 如果 VPS 不存在、MonitoringInstance insert 失败或 link 失败，整个事务必须回滚，不留下孤立 MonitoringInstance。
- 该路径不得修改 VPS lifecycle / usage / renewal decision，也不得修改 Subscription、Target、ProbeItem 或 Agent plan；它只创建观测对象和 VPS 关联证据。

### Scenario: Idempotent VPS-scoped detail creates

#### 1. Scope / Trigger

- Trigger: 修改 VPS scoped experience/service/domain/monitoring create handler、对应 repository、`internal/center/createidempotency`、`0062_create_vps_create_idempotency.sql` 或 APP ACL current fragment。Collection `POST /api/services|domains|monitoring-instances` 不在此合同内，必须保持原签名和状态语义。

#### 2. Signatures

- HTTP: `POST /api/vps/{vps_id}/experience-logs|services|domains|monitoring-instances`，必须恰有一个合法 `Idempotency-Key`。
- Store signatures:

  ```go
  CreateExperienceLogIdempotent(context.Context, renewals.CreateExperienceLogInput, string) (renewals.ExperienceLogRecord, bool, error)
  CreateAssetServiceIdempotent(context.Context, assetservices.CreateInput, string) (assetservices.Record, bool, error)
  CreateAssetDomainIdempotent(context.Context, assetdomains.CreateInput, string) (assetdomains.Record, bool, error)
  CreateLinkedMonitoringInstanceIdempotent(context.Context, pathVPSID string, wire monitoringinstances.LinkedCreateWireIdentity, key string) (monitoringinstances.Record, assetlinks.Record, bool, error)
  ```

  `bool` 为 `replayed`；monitoring 同时返回原 instance 与 link，且 handler 不在调用前读取 VPS defaults。
- DB: `experience_log_create_idempotency`、`asset_service_create_idempotency`、`asset_domain_create_idempotency`、`vps_monitoring_instance_create_idempotency`。每表以 key 为 PK，保存 digest、结果 FK、`created_at`；结果删除级联 receipt，无 TTL/janitor/update 路径。

#### 3. Contracts

- shared key trim/format 为 8..128 且只允许 `[A-Za-z0-9._:-]`；缺失、重复或非法 header 为 400 `invalid_idempotency_key`，且不得调用 create repository。
- digest 必须来自 normalize 后的 path VPS scope + 实际 wire identity。Monitoring digest 不包含从可变 VPS 状态派生的 persistence defaults；相同 wire retry 即使 VPS 默认值变化也必须 replay 原 instance/link。
- `LinkedCreateWireIdentity.ClearFields` 必须是末尾的 `json:"clear_fields,omitempty"` 字段，空数组标准化为 nil；没有清空意图时 canonical payload 与旧摘要逐字节相同，不增加版本或改变旧字段编码。非空清空意图进入摘要；同 key 改清空集合返回既有 409，排序/重复的等价集合可 replay。响应丢失后重试即使 VPS 资料已经修改，也不能重新派生副本或增建实例。
- 顺序为 normalize/validate→READ COMMITTED transaction→graph 锁（service/domain/MI）→operation-namespaced receipt lock→receipt lookup→mismatch/replay 或 result+receipt insert→commit。experience-log 不修改依赖图，保留原 receipt 协议。任一 cut point 失败都 rollback/fail closed。
- first create 为 201；same key + same digest 为 200 原 ID 且无额外写；same key + different digest 为 409 `idempotency_key_reused`。HTTP 错误/日志不得包含 key、digest、body、note/details、SQL 或 wrapped internal error。
- `0062` 是 `0061` 后的 additive migration；runtime APP 对四张 receipt 表只有 `select`/`insert`，无 update/delete/sequence 权限。不得修改已发布 migration。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| body/领域输入非法 | 400，事务前拒绝；不得创建 result/receipt |
| receipt replay 指向的 result/link 缺失 | fail closed internal error；不得静默重建 |
| monitoring 的 VPS 不存在或已有 active link | 404/409 既有稳定语义；instance/link/receipt 均不落单 |
| committed response unknown 后同 key/body retry | 200 原 ID；每类 result/receipt 各一行，monitoring link 也只有一行 |

#### 5. Good / Base / Bad Cases

- Good: monitoring 首次提交只给实际 wire 字段，repository 在 receipt miss 后于同一事务锁定 VPS、派生 defaults 并原子创建 instance + link + receipt；随后 VPS defaults 改变，同 key/wire 仍返回原双 ID。
- Base: 新 key + 合法新 body 创建新记录并返回 201；collection create 继续走旧签名，不要求 `Idempotency-Key`。
- Bad: handler 先读取 VPS defaults 再查 receipt；后续 VPS lookup 失败会让已提交请求的 replay 错误返回 404/500。
- Bad: 把派生 VPS defaults 放进 digest，或把 monitoring instance/link/receipt 拆到不同事务，都会破坏 lost-response replay。

#### 6. Tests Required

- Handler tests 覆盖四类 missing/duplicate/invalid、201/200/409 与 zero/one repository create count。
- Store unit 覆盖 begin/lock/lookup/replay scan/result insert/receipt insert/commit rollback cut points；PostgreSQL lost-response integration 覆盖四类真实表和 monitoring 双 ID。
- Migration/ACL tests 校验 successor 顺序、四表 FK/check/index、migration registry 与 current APP exact privileges；严格 PostgreSQL runner 缺 fixture/DSN 时是 blocker，不得 skip-as-pass。
- Monitoring regression 必须让 receipt replay 路径上的 VPS/default lookup 成为 unexpected，并在真实 PostgreSQL 中修改首次派生所依赖的 defaults 后证明原 ID、原持久化值及单行 materialization 不变。
- 测试失败输出不得打印 body、key、digest、note/details、SQL、DSN、record 或 raw error；AST/privacy contract 应覆盖本场景新增 handler/store/PG tests。

#### 7. Wrong vs Correct

```go
// 错误：handler 先读可变 VPS，再把派生 persistence 交给幂等仓储。
vps, err := vpsRepo.GetVPSAsset(ctx, pathVPSID)
record, link, replayed, err := repo.CreateLinkedMonitoringInstanceIdempotent(ctx, pathVPSID, derive(vps, wire), key)
```

```go
// 正确：handler 只传 path + normalized wire；repository 先查 receipt，miss 才在事务内锁 VPS 并派生。
record, link, replayed, err := repo.CreateLinkedMonitoringInstanceIdempotent(ctx, pathVPSID, wire, key)
```

## Asset Ledger timeline histories

`db/migrations/0020_create_renewal_decisions.sql` 添加 `renewal_decisions`，用于记录资产层 VPS 续费决策变化历史。它补充 `vps_assets.renewal_decision` 当前状态，不替代当前状态字段。

- `renewal_decisions.decision_id` 使用 `ids.New("rdec")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时级联清理历史。
- `from_decision` 允许 `null`，用于未来导入或补录；正常 VPS PATCH 自动记录时应写入变更前的决策值。
- `to_decision` 必须是 `vpsassets.RenewalDecision` 合法英文机器值，数据库 check 约束与领域校验共同保护。
- `reason` 是 trim 后的可空字符串语义，但数据库列必须 `not null default ''`，避免 timeline JSON 出现 null 文案。
- `decided_at` 默认 `now()`，领域入口可传入 UTC 时间；timeline 按 `decided_at desc, created_at desc, decision_id desc` 排序。
- `PATCH /api/vps/{vps_id}` 只有在显式设置 `renewal_decision` 且最终值发生变化时才插入历史；只改其他字段或设置为原值不得插入历史。
- VPS 当前状态更新与 history insert 必须在同一个事务中完成，并先 `select ... for update` 锁定 VPS 行，避免当前状态和历史漂移。
- `GET /api/vps/{vps_id}/timeline` 返回真实表驱动的 `renewal_decisions[]`、`price_histories[]`、`ip_histories[]`、`spec_snapshots[]`、`experience_logs[]`，不得返回占位假数据。
- 续费决策历史本身不得创建 `vps_monitoring_instance_links`，不得改写 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target 或 Agent。`PATCH /api/vps/{vps_id}` 保存续费意向不改写 subscription 或服务商自动续费核对事实；订阅账单事实必须通过独立订阅入口修改并保留 price history。

`db/migrations/0021_create_asset_histories.sql` 添加 `price_histories`、`ip_histories`、`vps_spec_snapshots`，用于补齐资产层价格、IP、规格变化历史。三张表补充当前状态字段，不替代 `subscriptions` 或 `vps_assets` 当前状态。

- `price_histories.price_history_id` 使用 `ids.New("ph")` 生成；`ip_histories.ip_history_id` 使用 `ids.New("iph")`；`vps_spec_snapshots.snapshot_id` 使用 `ids.New("vss")`。
- `price_histories` 必须同时引用 `subscriptions(subscription_id)` 和 `vps_assets(vps_id)`；subscription PATCH 只有在价格、币种、计费周期、计费月数、月付折算、续费日、自动续费标记或状态最终发生变化时才插入历史。
- subscription 当前状态更新与 price history insert 必须在同一个事务中完成，并先 `select ... for update` 锁定 subscription 行，避免当前订阅和历史漂移。
- `ip_histories` 必须记录 IPv4 / IPv6 前后值，且只有至少一个 IP 字段变化时才插入；数据库约束 `ip_histories_changed` 兜底拒绝无变化历史。
- `vps_spec_snapshots` 记录变化后的规格快照：`product_name`、`ssh_host`、`ssh_port`、`ssh_user`、`os_name`、`virtualization`。VPS PATCH 只有这些字段最终发生变化时才插入 snapshot。
- VPS 当前状态更新与 IP / spec history insert 必须复用 VPS history 事务路径，并先 `select ... for update` 锁定 VPS 行。
- 所有 history 仍属于 Asset Ledger，不得创建 `vps_monitoring_instance_links`，不得改写 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target、Agent 或 provider。

`db/migrations/0022_create_experience_logs.sql` 添加 `experience_logs`，用于记录单台 VPS 的人工体验、稳定性、网络、账单、服务支持、迁移或取消原因。它补充资产历史，不替代 `vps_assets.note` 或续费决策历史。

- `experience_logs.experience_log_id` 使用 `ids.New("elog")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时级联清理历史。
- `category` 使用稳定英文机器值：`note`、`stability`、`network`、`support`、`billing`、`migration`、`cancellation`；数据库 check 约束与领域校验共同保护。
- `severity` 使用稳定英文机器值：`info`、`warning`、`critical`；数据库 check 约束与领域校验共同保护。
- `summary` 必须 trim 后非空；`details` 是 trim 后的可空字符串语义，但数据库列必须 `not null default ''`，避免 timeline JSON 出现 null 文案。
- `occurred_at` 默认 `now()`，领域入口可传入 UTC 时间；experience log 列表按 `occurred_at desc, created_at desc, experience_log_id desc` 排序。
- `GET /api/vps/{vps_id}/experience-logs` 只返回该 VPS 的经验记录；VPS 不存在时返回 asset timeline not found 语义。
- `POST /api/vps/{vps_id}/experience-logs` 的 path `vps_id` 是唯一 VPS 来源，请求 body 不接受覆盖 `vps_id`；写入只创建 experience log，不改写 VPS 当前字段。
- experience log 不得创建 `vps_monitoring_instance_links`，不得改写 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target、Agent、Provider、VPS 当前状态或 subscription。

### Scenario: VPS experience logs contract

#### 1. Scope / Trigger

- Trigger: 修改 `experience_logs` schema、`/api/vps/{vps_id}/experience-logs`、`GET /api/vps/{vps_id}/timeline` 的 `experience_logs[]` 字段，或前端 VPS 详情页经验记录表单。

#### 2. Signatures

- DB table: `experience_logs(experience_log_id text primary key, vps_id text references vps_assets(vps_id) on delete cascade, category text, severity text, summary text, details text, occurred_at timestamptz, created_at timestamptz)`.
- Backend API: `GET /api/vps/{vps_id}/experience-logs` -> `[]renewals.ExperienceLogRecord`。
- Backend API: `POST /api/vps/{vps_id}/experience-logs` with JSON body `{category, severity, summary, details?, occurred_at?}` -> `renewals.ExperienceLogRecord`。
- Timeline API: `GET /api/vps/{vps_id}/timeline` includes `experience_logs: []`.
- Frontend API: `createVPSExperienceLog(vpsId, input)` and `listVPSExperienceLogs(vpsId)`.

#### 3. Contracts

- `category` and `severity` are machine values; UI maps them to Chinese labels in `web/src/lib/types.ts`.
- `occurred_at` is an RFC3339 timestamp when supplied; browser `datetime-local` values must be converted to ISO before posting.
- `summary` is the short user-facing title; `details` carries optional longer context and is never `null` in responses.
- `experience_logs[]` belongs to the VPS timeline contract; adding it requires updating Go DTOs, `web/src/lib/types.ts`, API tests, VPS detail tests, and timeline rendering together.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| blank path `vps_id` | 400 from domain validation or 404 from route parsing |
| missing VPS foreign key | 404 `vps asset not found` at HTTP layer |
| invalid `category` / `severity` | 400 `invalid input` |
| blank `summary` | 400 `invalid input` |
| zero / invalid `occurred_at` | 400 `invalid input` / `invalid json` |
| repository query failure | 500 `internal server error` |
| unsupported method | 405 `method not allowed` |

#### 5. Good/Base/Bad Cases

- Good: 用户在 VPS 详情页记录“晚高峰丢包”，POST 成功后刷新 timeline，并在 `experience_logs[]` 里按发生时间展示。
- Base: VPS 没有经验记录时，`experience_logs` 返回空数组，前端展示空态。
- Bad: 把 `vps_id` 放进 body 并允许覆盖 path VPS；这会制造跨资产误写风险。
- Bad: 经验记录写入后顺手修改 `vps_assets.note`、续费决策或 MonitoringInstance 状态。

#### 6. Tests Required

- Migration test: 断言表、外键、枚举 check、summary not blank、排序索引。
- Domain test: normalize / validate 覆盖 trim、UTC、invalid category/severity、blank summary。
- Store test: create/list/timeline 聚合、排序 SQL、missing VPS / foreign-key mapping。
- Handler/router/bootstrap tests: GET/POST、invalid JSON、invalid input、not found、method not allowed、subtree routing、bootstrap non-nil wiring。
- Frontend tests: API client URL/body，VPS 详情页创建经验记录后刷新 timeline，空态显示。

#### 7. Wrong vs Correct

```go
// 错误：body 里的 vps_id 覆盖 path，可能写入另一台 VPS。
input.VPSID = input.VPSID

// 正确：path 是唯一 VPS 来源。
input.VPSID = vpsID
```

```tsx
// 错误：直接提交 datetime-local 字符串，Go time.Time 不能稳定解析。
occurred_at: form.occurredAt

// 正确：提交 ISO/RFC3339 时间。
occurred_at: form.occurredAt ? new Date(form.occurredAt).toISOString() : null
```

## Asset Ledger service assets

`asset_services` 保存独立服务身份；0067 的 `asset_service_associations` 保存该服务在不同 VPS 上的承载事实。它不是自动服务发现或 Agent 采集入口。

- `asset_services.service_id` 使用 `ids.New("svc")` 生成。
- 服务名称、类型、对象状态、标签与备注属于对象；VPS、地址、端口和 Target 引用属于关联。新写入不再填充对象表的历史 placement 列；这些列不能作为归属或依赖权威。
- 一个服务可同时关联多台 VPS；同一服务与 VPS 最多一条未结束关联。结束某一关联不改变服务状态及其他 VPS 的关联。
- `name` 必须 trim 后非空；数据库约束为 `asset_services_name_not_blank`，领域 create 也必须校验。
- `service_type` 使用稳定英文机器值：`web`、`api`、`database`、`worker`、`proxy`、`other`；空输入默认 `other`。
- `status` 使用稳定英文机器值：`active`、`paused`、`retired`、`unknown`；空输入默认 `active`。
- `port` 可为 `null`；存在时必须在 `1..65535`。
- `labels` 使用 `text[] not null default '{}'`；领域层负责 trim、过滤空标签和去重。
- service asset 写入不得改变 VPS 当前状态、subscription、experience log、MonitoringInstance、Target、ProbeItem、Agent 或 `monitoring_instances.provider`。

### Scenario: VPS service assets contract

#### 1. Scope / Trigger

- Trigger: 修改 `asset_services` schema、`internal/center/assetservices/`、`/api/services`、`/api/vps/{vps_id}/services`，或前端 VPS 详情页服务资产区块。

#### 2. Signatures

- DB identity: `asset_services(service_id, name, service_type, status, labels, note, created_at, updated_at)`；placement authority: `asset_service_associations(id, service_id, vps_id, target_id, address, port, started_at, ended_at, end_reason, ended_by, snapshot)`。
- Backend API: `GET /api/services?vps_id=&target_id=&service_type=&status=` -> `[]assetservices.Record`。
- Backend API: `POST /api/services` with JSON body `{vps_id, target_id?, name, service_type?, status?, url?, port?, labels?, note?}` -> `assetservices.Record`。
- Backend API: `GET /api/vps/{vps_id}/services` -> `[]assetservices.Record`。
- Backend API: `POST /api/vps/{vps_id}/services` with JSON body `{target_id?, name, service_type?, status?, url?, port?, labels?, note?}` -> `assetservices.Record`。
- Frontend API: `listAssetServices(filter)`, `createAssetService(input)`, `listVPSServices(vpsId)`, `createVPSService(vpsId, input)`。

#### 3. Contracts

- `POST /api/vps/{vps_id}/services` 的 path `vps_id` 是唯一 VPS 来源；body 中的 `vps_id` 必须被忽略，不能覆盖 path。
- `GET /api/vps/{vps_id}/services` 必须先确认 VPS 存在；VPS 不存在时返回 not-found 语义，不把缺失 VPS 静默表现为空列表。
- `target_id` 是可选关联，必须引用存在且未退役的 Target；只做引用校验和展示跳转，不得创建 Target、改写 Target、改 ProbeItem 或改变观测语义。
- 全局无 placement 过滤的 `GET /api/services` 列出独立身份，包括只剩历史关联的对象。VPS/Target 过滤读取当前关联；非法枚举必须在进入 store 查询前返回 400。
- service asset 不进入 `/api/dashboard` 的资产摘要，也不进入 `GET /api/vps/{vps_id}/timeline`；它在 VPS 详情页作为独立服务区块加载。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| missing / blank `vps_id` on collection POST | 400 `invalid input` |
| body `vps_id` conflicts with path VPS | path VPS wins; write to path VPS only |
| missing VPS on path GET/POST or collection POST FK | 404 `vps asset not found` |
| missing Target FK | 404 `target not found` |
| create with retired Target, including idempotent create | 409 `target metadata conflict`; no object, association or receipt is committed |
| blank `name` | 400 `invalid input` |
| invalid `service_type` / `status` | 400 `invalid input` |
| `port < 1` or `port > 65535` | 400 `invalid input` |
| repository query failure | 500 `internal server error` |
| unsupported method | 405 `method not allowed` |

#### 5. Good/Base/Bad Cases

- Good: 用户在 VPS 详情页为 `vps_001` 创建 `Blog` 服务，带 `target_id=tg_001`，写入后只刷新服务列表。
- Base: VPS 存在但没有服务时，path GET 返回空数组，前端展示 `尚未记录服务`。
- Bad: VPS 不存在时 path GET 返回空数组，用户会误以为资产存在但没有服务。
- Bad: 创建 service asset 时自动创建 Target 或修改 Target 探针，越过了本 MVP 的人工关联边界。

#### 6. Tests Required

- Migration test: 断言表、命名外键、枚举 check、name not blank、port range 和索引。
- Domain test: normalize / validate 覆盖空名称、枚举、labels、optional Target、port 边界和默认值。
- Store test: create、list all、list by VPS、排序 SQL、missing VPS exists check、missing Target FK、check violation 映射。
- Handler/router/bootstrap tests: collection/path GET/POST、invalid JSON、invalid input、not found、method not allowed、subtree routing、bootstrap non-nil wiring。
- Frontend tests: API helper URL/body，尤其 path-scoped create 不带 body `vps_id`；VPS 详情页服务加载、空态、创建成功和本地校验失败。

#### 7. Wrong vs Correct

```go
// 错误：让 body 覆盖 path，可能写到另一台 VPS。
input = assetservices.NormalizeCreateInput(input)

// 正确：先用 path 写入，再 normalize/validate。
input.VPSID = vpsID
input = assetservices.NormalizeCreateInput(input)
```

```tsx
// 错误：path scoped create 仍把 body.vps_id 传给后端。
postJSONBody(`/api/vps/${vpsId}/services`, input)

// 正确：path scoped create 去掉 vps_id，只传服务字段。
postJSONBody(`/api/vps/${vpsId}/services`, { name, service_type, status, target_id, url, port, labels, note })
```

`asset_domains` 保存独立域名身份；0067 的 `asset_domain_associations` 保存域名与 VPS 的当前和历史关联。它不是 DNS provider、注册商同步或解析记录管理入口。

- `asset_domains.domain_id` 使用 `ids.New("dom")` 生成。
- 域名名称、用途、注册商、有效期、对象状态及标签属于对象；VPS、地址、该 VPS 的服务与 Target 引用属于关联。新写入不再填充对象表的历史 placement 列。
- 关联的可选 `service_id` 必须在同一 VPS 上存在未结束服务关联。可选 `target_id` 必须存在且未退役；创建或列出域名不得修改 Target / ProbeItem。
- `domain_name` 必须是归一化的小写 ASCII 域名，不含协议、路径、空白或尾随点；数据库用 `asset_domains_name_unique` 保证全局唯一，领域层负责 trim/lower/remove trailing dot 和 label 校验。
- `status` 使用稳定英文机器值：`active`、`paused`、`retired`、`unknown`；空输入默认 `active`。
- `expires_at` 是 nullable `date`，未知日期用 `null`，API 复用 subscription `Date` 的 `YYYY-MM-DD` JSON 语义。
- `auto_renew` 与 `https_enabled` 只记录人工事实，不触发续费决策、证书检查或 Target probe 修改。
- domain asset 写入不得改变 VPS 当前状态、subscription、experience log、service asset、MonitoringInstance、Target、ProbeItem、Agent 或 `monitoring_instances.provider`。

### Scenario: VPS domain assets contract

#### 1. Scope / Trigger

- Trigger: 修改 `asset_domains` schema、`internal/center/assetdomains/`、`/api/domains`、`/api/vps/{vps_id}/domains`，或前端 VPS 详情页域名资产区块。

#### 2. Signatures

- DB identity: `asset_domains(domain_id, domain_name, purpose, status, registrar, expires_at, auto_renew, https_enabled, labels, note, created_at, updated_at)`；placement authority: `asset_domain_associations(id, domain_id, vps_id, service_id, target_id, address, started_at, ended_at, end_reason, ended_by, snapshot)`。
- Backend API: `GET /api/domains?vps_id=&service_id=&target_id=&status=` -> `[]assetdomains.Record`。
- Backend API: `POST /api/domains` with JSON body `{vps_id, service_id?, target_id?, domain_name, purpose?, status?, registrar?, expires_at?, auto_renew?, https_enabled?, labels?, note?}` -> `assetdomains.Record`。
- Backend API: `GET /api/vps/{vps_id}/domains` -> `[]assetdomains.Record`。
- Backend API: `POST /api/vps/{vps_id}/domains` with JSON body `{service_id?, target_id?, domain_name, purpose?, status?, registrar?, expires_at?, auto_renew?, https_enabled?, labels?, note?}` -> `assetdomains.Record`。
- Frontend API: `listAssetDomains(filter)`, `createAssetDomain(input)`, `listVPSDomains(vpsId)`, `createVPSDomain(vpsId, input)`。

#### 3. Contracts

- `POST /api/vps/{vps_id}/domains` 的 path `vps_id` 是唯一 VPS 来源；body 中的 `vps_id` 必须被忽略，不能覆盖 path。
- `GET /api/vps/{vps_id}/domains` 必须先确认 VPS 存在；VPS 不存在时返回 not-found 语义，不把缺失 VPS 静默表现为空列表。
- `service_id` 和 `target_id` 都是可选关联，只做引用校验和展示跳转；不得创建或修改 Service、Target、ProbeItem 或观测语义。
- `service_id` 若存在，必须有同一 VPS 上的当前服务关联；仅在另一 VPS 有关联应返回 `asset service not found` 语义。
- 全局无 placement 过滤的 `GET /api/domains` 列出独立身份，包括只剩历史关联的对象。VPS/Service/Target 过滤读取当前关联；非法枚举必须在进入 store 查询前返回 400。
- domain asset 不进入 `/api/dashboard` 的资产摘要，也不进入 `GET /api/vps/{vps_id}/timeline`；它在 VPS 详情页作为独立域名区块加载。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| missing / blank `vps_id` on collection POST | 400 `invalid input` |
| body `vps_id` conflicts with path VPS | path VPS wins; write to path VPS only |
| missing VPS on path GET/POST or collection POST FK | 404 `vps asset not found` |
| missing / cross-VPS Service FK | 404 `asset service not found` |
| missing Target FK | 404 `target not found` |
| create with retired Target, including idempotent create | 409 `target metadata conflict`; no object, association or receipt is committed |
| duplicate `domain_name` | 409 `asset domain conflict` |
| invalid `domain_name` | 400 `invalid input` |
| invalid `status` | 400 `invalid input` |
| repository query failure | 500 `internal server error` |
| unsupported method | 405 `method not allowed` |

#### 5. Good/Base/Bad Cases

- Good: 用户在 VPS 详情页为 `vps_001` 创建 `api.example.com`，可选带 `service_id=svc_001` 与 `target_id=tg_001`，写入后只刷新域名列表。
- Base: VPS 存在但没有域名时，path GET 返回空数组，前端展示 `尚未记录域名`。
- Bad: VPS 不存在时 path GET 返回空数组，用户会误以为资产存在但没有域名。
- Bad: 创建 domain asset 时自动创建 DNS 记录、创建 Target、修改 Target 探针或尝试注册商同步，越过了本 MVP 的人工维护边界。

#### 6. Tests Required

- Migration test: 断言表、命名外键、唯一约束、域名归一化 check、枚举 check、date 字段和索引。
- Domain test: normalize / validate 覆盖空域名、URL/path、裸主机名、非法 label、枚举、labels、默认值。
- Store test: create、list all、list by VPS、排序 SQL、missing VPS exists check、service 同 VPS 校验、missing Service/Target FK、unique/check violation 映射。
- Handler/router/bootstrap tests: collection/path GET/POST、invalid JSON、invalid input、conflict、not found、method not allowed、subtree routing、bootstrap non-nil wiring。
- Frontend tests: API helper URL/body，尤其 path-scoped create 不带 body `vps_id`；VPS 详情页域名加载、空态、创建成功和本地校验失败。

#### 7. Wrong vs Correct

```go
// 错误：只靠 FK，允许 dom(vps_a) 关联 svc(vps_b)。
insert into asset_domains (vps_id, service_id, domain_name) values (...)

// 正确：写入前确认同一个 VPS 上存在当前服务关联。
select exists (select 1 from asset_service_associations where service_id = $1 and vps_id = $2 and ended_at is null)
```

```tsx
// 错误：VPS scoped create 仍把 body.vps_id 传给后端。
postJSONBody(`/api/vps/${vpsId}/domains`, input)

// 正确：path scoped create 去掉 vps_id，只传域名字段。
postJSONBody(`/api/vps/${vpsId}/domains`, { domain_name, service_id, target_id, status, expires_at, labels, note })
```

### 关联历史与 VPS 跟进事项

- 创建服务/域名及首条关联在同一 graph 事务完成，任一步失败全部回滚。已归档 VPS 不接受新关联。
- `GET /api/vps/{id}/services`、`domains` 和 canonical overview 只投影管理中 VPS 的未结束关联。恢复 VPS 不重新显示已结束服务或域名为当前承载；历史统一从下述关联历史接口读取。
- `GET /api/vps/{id}/service-associations` 与 `domain-associations` 返回完整关联历史；`?current=true` 只返回未结束关联。响应使用 `association_id/object_id/vps_id/target_id/service_id/address/port/started_at/ended_at/end_reason/ended_by/snapshot`。
- 同路径 `POST {object_id,target_id?,service_id?,address?,port?}` 关联已有对象；service_id 仅用于域名，port 仅用于服务且范围 1..65535。当前重复关联返回 409。
- 关联已有对象时若 `target_id` 指向已退役 Target，返回 409 `association conflict`，不写入关联；对象自身状态不影响此限制。普通/幂等创建对象时同类拒绝为 409 `target metadata conflict`，整个创建事务回滚。
- `PATCH /api/vps/{id}/{service|domain}-associations/{association_id}/end {reason}` 结束指定关联并冻结对象快照、原因、操作者及结束时间。必须同时匹配 VPS 与关联 ID，重复结束返回 409。历史快照不可被后续对象修改覆盖。
- 归档事务只结束该 VPS 的当前关联；其他 VPS 及共享服务、域名身份继续存在。对象状态修改独立于单个 VPS 生命周期，并在所有关联 VPS 的生命周期历史中留痕。迁移以新增目标关联、结束来源关联表达，不自动执行搬迁或结束旧 VPS。
- `GET/POST /api/vps/{id}/followups` 查询或创建事项；创建输入为 `{kind,summary,details}`，kind 为 migration、potential_charge、archived_online、review、annotation。迁移来源、目标、结果、证据引用保存在 details；创建操作者由可信会话写入，归档 VPS 仍可追加事项。
- `PATCH /api/vps/{id}/followups/{followup_id} {status,reason}` 仅允许 pending→resolved/ignored，非空原因及可信操作者必填，保存 resolution_reason/resolved_by/resolved_at。关闭后不可覆写，新修订通过追加事项关联原 followup_id 留痕。
- 系统提醒按 `(vps_id,kind,dedupe_key)` 合并一条 pending 事项；同一归档周期以 VPS ID 与 archived_at 标识。已解决/忽略的同周期事项保持关闭，后续每次心跳不能重建提醒；恢复后重新归档构成新周期，可形成新事项。
- 验收须覆盖同一对象跨两个 VPS、归档单侧、关联重复与结束、快照不变、对象状态独立、跟进事项去重及关闭留痕，并在真实 PostgreSQL 上验证事务回滚和约束。

## Asset Ledger JSON import

`internal/center/importing/` 是真实 VPS JSON dry-run/import 的领域入口。它复用 `providers`、`vpsassets`、`subscriptions` 的 normalize / validate 规则，不维护第二套枚举、金额、日期或 provider 校验。

- dry-run 是默认路径，只解析、归一化、校验和产出报告，不写数据库。
- dry-run 必须报告 provider 创建候选、VPS 创建候选、subscription 创建候选、缺失 provider、缺失续费日期、非法字段、重复候选、MonitoringInstance 关联候选、未来 30 天续费候选和闲置但付费候选。
- 数据库可用时，dry-run 可以读取现有 providers / vps_assets / subscriptions / monitoring_instances 做重复和 MonitoringInstance 候选诊断；数据库不可用时仍应能完成纯文件模型校验。
- `-import` 必须显式开启，且在一个事务中按 provider → VPS asset → subscription 顺序写入；校验错误或重复候选存在时拒绝写入。
- import 不接受也不写 `monthly_price`，仍由 subscription 后端计算。
- import 不创建 `vps_monitoring_instance_links`，不改写 `monitoring_instances.provider`，不改变 MonitoringInstance / Target / Agent 语义。MonitoringInstance 相关输入只能作为人工确认候选进入报告。
