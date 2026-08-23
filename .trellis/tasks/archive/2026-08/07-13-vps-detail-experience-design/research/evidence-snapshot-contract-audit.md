# 运维记录证据快照合同审查

## 1. 问题重述

记录中的证据不只要能“跳转到当前数据”，还要能在原始数据超过留存期、源对象被归档/删除、阈值或聚合算法变更后，准确回答“当时看到了什么，为什么做出这个判断”。

因此，证据模型必须同时保留：

- 可导航、可继续分析的源引用；
- 不随源数据变化的不可变快照；
- 观测时间、数据可用范围、采样/聚合方式、单位与数据质量；
- 人类可读的来源身份快照，以及稳定 ID 和 schema 版本。

## 2. 仓库已确认的数据层

### 2.1 监控实例与探测对象

- `host_samples` 是高频原始主机观测，包括 CPU、load、内存、swap、磁盘、inode、网络、IO、uptime，以及 `maintenance_context`、`is_backfilled` 和 `sync_batch_id`；新 schema 还保存容器摘要。证据：`db/migrations/0001_initial_schema.sql:65-90`、`internal/center/runtimefacts/types.go:10-39`。
- `probe_observations` 保存 TCP/HTTP/TLS 探测的成败、延迟、HTTP 状态、TLS 剩余天数、错误摘要，同样带维护与补传语义。证据：`db/migrations/0001_initial_schema.sql:92-108`、`internal/center/runtimefacts/types.go:71-89`。
- 默认原始层留存 30 天，且设置合同要求不少于 30 天。retention worker 会直接删除过期 heartbeat、host sample 和 probe observation。证据：`internal/center/settings/types.go:218-223,663-676`、`internal/center/store/retention.go:42-75,191-197`。
- 默认主机样本、TCP 和 HTTP 探测频率都是 5 秒，TLS 是 6 小时。单监控实例仅主机样本就约为 720 条/小时、17,280 条/天、518,400 条/30 天，所以“完整 raw 时间窗复制进记录”在容量上不成立。证据：`internal/center/settings/types.go:181-188`。
- 在删除原始层前，worker 生成 UTC 日粒度聚合。主机聚合保留部分指标的平均/最大值、样本数、补传样本数与维护样本数；探测聚合保留成功/失败计数、平均/P95 延迟、最小 TLS 剩余天数和数据质量计数。证据：`db/migrations/0008_add_retention_aggregates.sql:1-43`、`internal/center/store/retention.go:112-189`。
- 默认聚合层也只留存 30 天；这并非一个默认长期历史库。记录如果只链接 runtime facts 或聚合表，一个月后就可能无法复现当时证据。

### 2.2 当前读 API 不等于可持久证据合同

- 单监控实例 runtime facts 支持 `realtime` / `24h` / `7d` / `30d`，返回请求时间窗、实际可用时间、样本数、最新样本与分桶平均点。证据：`internal/center/http/handlers/runtime_facts.go:162-190`、`internal/center/runtimefacts/types.go:41-69`、`internal/center/store/runtime_facts.go:59-104,246-304`。
- 分桶点只有平均值和 sample count，没有最大值、缺失桶解释、维护/补传样本数、告警阈值版本或聚合算法版本。直接把这个响应当作快照可能隐藏短时尖峰，也无法解释事后为何与图表或告警不一致。
- 探测对象 runtime facts 的 `24h` / `7d` / `30d` 不是完整窗口聚合，而是分别最多取最新 288 / 2016 / 8640 条原始观测，且响应没有实际覆盖时间或截断标志。在单路 5 秒探测下，它们实际只约覆盖 24 分钟 / 2.8 小时 / 12 小时；多探针、多监控实例时更短。这是现有“近 24h/7d/30d”读模型的真实性缺口，不能原样固化或用于比较。证据：`internal/center/http/handlers/monitoring_instance_sparklines.go:98-113`、`internal/center/store/runtime_facts.go:193-217,337-350`。
- 全局 sparkline API 把时间窗均分为数值数组；空桶默认为 `0`，响应本身不带每个点的时间、样本数或数据质量。证据：`internal/center/store/monitoring_instance_sparklines.go:13-17,234-249`、`internal/center/store/target_sparklines.go:13-17,120-135`。它适合列表微缩图，不适合作为法证性快照。
- runtime facts 中的 `fingerprint`、`sync_batch_id`、容器 ID/名称/镜像等是运行与库存上下文，不应因为现有 DTO 有这些字段就默认进入永久记录。必须按证据类型做 allowlist，并对可能暴露内部库存的字段单独评估。证据：`internal/contracts/agentapi/types.go:74-120`。
- 主监控实例模型会隐藏 fingerprint，但 runtime facts DTO 目前会暴露完整 fingerprint，边界并不一致。证据快照默认只能保存“绑定一致/不一致”或掩码摘要，不得因复用 runtime response 而永久扩大暴露。证据：`internal/center/monitoringinstances/types.go:82-119`、`internal/center/runtimefacts/types.go:10-39`。

### 2.3 事件与当前状态

- `active_incidents` 是当前状态投影，不是审计历史；历史由 `state_change_events` 承担。事件底层有 `payload jsonb`，但用户侧列表只 allowlist 返回事件/异常 ID、对象、类型、严重度、摘要和时间。证据：`db/migrations/0001_initial_schema.sql:110-131`、`internal/center/store/dashboard.go:17-27,721-765`。
- incident 事件 payload 目前主要是 `incident_id` + `incident_class`，没有指标值、计算时间窗、阈值/规则版本、actor、producer 或独立的 observed/recorded time。recovery event 的 severity 是恢复前等级，不是“正常”；严重度下降也不一定生成事件。因此事件序列不能完整重建 incident 状态，也不能单独作为监控诊断快照。证据：`internal/center/store/incidents.go:222-253`、`internal/center/incidents/evaluator.go:288-350`。
- `/api/events` 列表有稳定 `event_id`，但 Dashboard 在转换 recent events 时丢失该字段，前端因此把 `event_id` 定义为可选。证据选择器不能从无 ID 的 Dashboard 摘要直接生成稳定引用。证据：`internal/center/store/dashboard.go:153-171`、`web/src/lib/types.ts:426-436`。
- 事件层默认留存 90 天。因此关联事件 ID 也不足以支持长期记录。证据：`internal/center/settings/types.go:218-223`、`internal/center/store/retention.go:45,72-73,196`。
- 事件本身没有 `is_backfilled`；当前通过对象 + 精确时间关联仍存在的 raw facts 判断补传来源。证据：`.trellis/spec/backend/database-guidelines.md:1224-1245`。当 raw facts 被 30 天留存清理、事件却仍在 90 天内时，这个来源判断可能无法重建。证据快照若包含事件，必须在捕获时固化已知的 live/backfilled/maintenance 语义，不能以后再从可能已删除的原始行推导。
- 普通事件读模型还会根据对象当前是否属于未归档资产过滤；`notification_only` 只代表存在 sent/failed/suppressed 任一通知记录，不代表实际发送成功，且通知有独立 TTL。证据快照必须保存捕获时的明确语义，不能保存会随当前可见性/留存变化的布尔查询结果。证据：`internal/center/store/dashboard.go:57-112,684-710`。

### 2.4 IP 质量、订阅/预算与命令审计

- IP 质量已有 normalized report、provider/service 明细、coverage、新鲜度和 raw/history 两层留存；默认 raw 90 天、history 365 天。raw/extra/diagnostics JSON 在 center 端必须递归脱敏并限长，failure/partial/ambiguous 不得伪装成确定风险。证据：`.trellis/spec/backend/ip-quality-contract.md`。
- 订阅成本是带原币种、汇率来源/日期/新鲜度和预算来源的派生事实。证据快照不能只固化一个折算金额，否则未来无法判断是价格、汇率、预算还是数据过期导致异常。Fixer API key 等 secret 永不能进入快照。证据：`.trellis/spec/backend/subscription-cost-center.md`。
- 命令审计是永久元数据审计，而 stdout/stderr 仅有 24 小时 TTL；审计 `details` 和读 API 都递归禁止输出。运维记录不得通过“证据快照”另存一份 stdout/stderr 来绕过该边界。证据：`.trellis/spec/backend/database-guidelines.md:188-267`。

### 2.5 对象归档、永久清理与现有历史表

- VPS 归档主要是可恢复的状态变更；MonitoringInstance 归档会停止运行与撤销访问，但永久清理会删除事件、通知和 active incident，仅显式保留命令审计。证据：`internal/center/store/asset_lifecycle.go:137-179`、`internal/center/store/monitoring_instances.go:514-695`。
- 现有 VPS 价格/IP/规格历史与 experience logs 都对 VPS 外键使用 `on delete cascade`。它们不具备“源对象永久删除后记录/证据仍存续”的合同。证据：`db/migrations/0021_create_asset_histories.sql:1-68`、`db/migrations/0022_create_experience_logs.sql:1-22`。
- 新记录与证据如果需要独立存续，不能延用 cascade 模型。它们应保存稳定来源 ID、人类可读的身份快照和源删除状态，当前可导航关系则使用软引用或 `on delete set null`。

## 3. 外部成熟模式与边界

- [Grafana IRM Incident timeline](https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/manage-incidents/incident-timeline/) 要求用户选数据源、编写查询、运行验证、补标题/说明后才加入时间线；加入后的结果会冻结而不自动刷新。这与“显式选择 + 保存前预览 + 不可变结果”一致。
- [Datadog Incident timeline](https://docs.datadoghq.com/incident_response/incident_management/investigate/timeline/) 的图表先保留 24 小时可交互状态，之后替换为静态图；[Graph snapshot API](https://docs.datadoghq.com/api/latest/snapshots/take-graph-snapshots/) 强制提供 `start`、`end` 和 `metric_query` 或 `graph_def`。这说明短期源数据与长期可读副本可以并存，但绝对时间窗与查询定义必须固化。
- [GitLab Incidents](https://docs.gitlab.com/operations/incident_management/incidents/) 允许上传指标截图、说明和原图链接。它验证了“静态副本 + 源入口”双轨，但截图本身缺少时间窗、聚合和样本数，不能作为本项目唯一的规范化证据。
- [AWS Incident Manager post-incident analysis](https://docs.aws.amazon.com/incident-manager/latest/userguide/analysis.html) 会自动带入事件中使用过的指标，但由用户整理图表、标题、说明和关键时间点。可迁移的是“系统推荐候选、用户确认写入”，不是静默复制全部数据。AWS 已不再向新客户开放该服务，这里只借鉴分析模式。

这些产品都没有承诺把全部 raw samples 永久复制到事故/记录中。它们保存的是查询结果、可见数据或静态渲染。因此，本项目应保存经 allowlist 的规范化摘要与有界序列，而不把“证据快照”解释成另一套无限留存的原始时序库。

## 4. 对证据模型的直接约束

1. **快照必须是类型化合同，不是任意 JSON 桶。** 每个 evidence kind 必须有自己的 schema version、allowlist、单位、大小上限、脱敏和渲染器。
   客户端只能提交来源和选择参数，服务端在事务中重新读取源事实并生成 allowlist 快照；不得相信客户端上传的任意 evidence JSON。
2. **快照保留观测语义，而不是截图或某个页面响应。** 时序证据至少需要指标、单位、时间窗、聚合方法、桶宽、样本数、空缺、最值/分位数、补传/维护语义和生成时间。
3. **记录和证据的不可变性分开。** 人工 Markdown 可修订，已保存证据不就地刷新；新数据形成新 evidence revision/snapshot，修订历史记录当时使用了哪组快照。
4. **快照不得升级源数据的保密级别和留存期。** 禁止凭据、token、provider secret、原始命令输出和未经脱敏的 payload；内部库存字段需要单独准入。
5. **保留来源状态而不依赖来源存续。** 快照保留稳定 ID + 名称/地址等当时身份摘要；读取时另行显示源对象当前是可用、归档还是已删除。
6. **可比较性在写入时建立。** 同类证据需要稳定的 normalized summary 和指标定义；渲染时再从任意 raw JSON 推测字段会使跨时间/跨主体比较不可靠。
7. **相对时间只是选择交互。** 如“过去 24 小时”在写入时必须物化为绝对 UTC 起止时间，并同时保存请求时间窗、实际可用时间窗、捕获时间和记录引用时间。
8. **三层表达各自承担职责。** 规范化结构化证据用于比较与验证，有界的静态渲染用于长期阅读，源引用用于继续调查；截图、结构数据或链接均不能单独替代另两层。
9. **对拓扑/工作负载字段分级。** 数值指标、数据质量和阈值上下文可默认固化；Target host/端口/HTTP path 以及容器名称/镜像/状态只在证据类型允许且用户显式选择时有界保存；容器 ID、逐样本容器数组、完整 fingerprint、任意 raw JSON 和未处理 error summary 默认禁止。
10. **权限要绑定主体和证据。** 当前只有 admin 角色，监控 API 也只有会话保护，没有对象级授权。新记录设计不应伪造尚不存在的 RBAC，但数据模型必须保留 creator/captured_by 与主体边界，为未来真实角色出现后做统一授权留出合同。证据：`internal/center/auth/types.go:9-27`、`internal/center/http/middleware.go:25-74`。

## 5. 待用户确认的高价值决策

是否把“证据捕获”定义为一次显式、可预览的用户动作：用户选择数据源、报告/时间窗、指标和精度，系统在保存前显示将写入的范围、大小、缺口与敏感性；模板只能推荐证据，不静默复制当前所有数据。

当前研究建议采用这个决策，因为它同时保护了证据相关性、数据真实性、容量可控和敏感边界。反之，模板自动复制全部数据会降低操作成本，但会制造大量与当前判断无关、无法解释且可能超越留存边界的副本。
