# VPS 心跳异常通知策略根因分析

## 结论

用户设置的 `stale_threshold_intervals = 20` 已经由 Web 页面提交并持久化；失效发生在事件判定层：周期扫描路径会读取该设置，但每次 Agent 成功同步后的即时判定路径没有传入阈值，最终由 evaluator 静默回退到默认值 `3`。因此同一监控实例会同时受到两套不同阈值约束。

除此之外，当前阈值语义、恢复判定和通知文案分别造成了提前告警、抖动式恢复通知以及主体不可识别的问题。

## 数据流证据

### 设置写入与读取正常

- `web/src/pages/SettingsPage.tsx` 会序列化 `stale_threshold_intervals`。
- `internal/center/settings/types.go` 定义并校验该字段。
- `internal/center/store/settings.go` 将完整的 `incident_defaults` 写入 PostgreSQL。
- `cmd/houfeng-center/bootstrap.go` 将持久化设置接入 incident evaluator 的 settings source。

因此，“20 没有生效”不是前端未提交或数据库未保存导致的。

### 两条判定路径使用了不同阈值

1. 周期扫描：`internal/center/incidents/service.go` 的 `EvaluateStaleMonitoringInstances` 调用 `incidentTimingFor`，并把心跳间隔及 `StaleThresholdIntervals` 一起传给 evaluator。
2. 成功同步：同一文件的 `AfterSuccessfulSync` 经 `evaluateMonitoringInstance` 只读取心跳间隔，调用 `EvaluateMonitoringInstanceHeartbeatMissing` 时省略失联阈值。
3. `internal/center/incidents/evaluator.go` 将该参数定义为可选参数；省略后静默采用默认值 `3`。

结果是：即使设置为 20，每次成功同步后的即时判定仍可能在 2–3 个周期生成失联事件与通知。

## 其他已确认问题

### 阈值语义提前一个周期触发

`heartbeatSeverity` 当前把设置值解释为“alert 阶段阈值”，并在 `threshold - 1` 时创建 notice。页面标签却是“失联判定阈值”。因此即使所有路径都正确传入 20，首次异常仍会在第 19 个周期产生，违反“达到 20 才判定失联”的直觉与用户预期。

默认值为 3 时，对应 notice / alert / critical 为 2 / 3 / 5 个周期；在三类通知开关都开启时，一次很短的网络抖动就可能连续产生开始、升级和恢复通知。

### 一次新心跳便立即恢复

心跳遗漏数跌回活动阈值以下时，evaluator 会立即调用 `recoverIfNeeded` 并发送“心跳已恢复”，没有连续成功次数或稳定时间窗口。单个偶发心跳即可造成“失联—恢复—再次失联”的抖动。

仓库中已有可复用的稳定恢复模式：目标探测失败要求连续成功后恢复，资源压力事件要求安全窗口后恢复。心跳历史表也包含按监控实例和时间排序的记录，并可排除回填心跳，具备实现连续实时心跳恢复的基础。

### 通知链路丢失监控实例身份

心跳判定生成的 summary 仅包含“最近 N 个心跳周期未收到心跳”或“心跳已恢复”。通知分发器只把 summary 交给 Telegram / 飞书适配器；对象类型和对象 ID 虽写入审计记录，却没有进入通知正文。

监控实例记录已经包含 `DisplayName` 和 `MonitoringInstanceID`，无需跨仓库查询即可在开始、升级、恢复通知中同时显示可读名称和稳定标识。

## 历史修复缺口

历史任务曾识别“设置阈值未生效”，提交 `39271b69` 为周期扫描路径补入了持久化阈值，但没有覆盖既有的成功同步即时判定路径。现有回归测试也只验证 `EvaluateStaleMonitoringInstances`，没有通过 `AfterSuccessfulSync` 验证自定义阈值，导致该缺口长期未被发现。

## 设计约束

- 所有心跳异常入口必须使用同一个 resolved timing 对象，禁止 evaluator 对业务阈值静默回退。
- `stale_threshold_intervals = N` 应统一表示“累计达到 N 个遗漏周期后首次创建异常”，N 之前不得创建或通知。
- 恢复必须基于非回填、连续的实时心跳证据，避免单点抖动。
- 开始、升级、恢复消息都必须携带监控实例显示名和稳定 ID。
- 测试必须覆盖周期扫描和成功同步两条入口，并验证通知开关与消息正文。

## Break-loop retrospective

### 1. Root Cause Category

- **B — Cross-Layer Contract**：同一 persisted policy 经过 settings/store 后，在 periodic 与 post-sync 两个服务入口发生分叉。
- **C — Change Propagation Failure**：历史修复只补了周期扫描；evaluator 的 optional 参数和静默默认让遗漏调用点仍可编译。
- **D — Test Coverage Gap**：回归只调用周期入口，没有以非默认值调用公开 `AfterSuccessfulSync`；恢复测试也没有冻结稳定 live receipt 与 ingress batch identity。

### 2. Why earlier fixes failed

历史改动修正了一个调用点的症状，但没有枚举所有触发入口，也没有删除可选参数这一逃生通道。单路径测试因此无法发现同一实例同时受两套阈值约束。

### 3. Prevention Mechanisms

| Priority | Mechanism | Specific action | Status |
| --- | --- | --- | --- |
| P0 | Compile-time | evaluator 必须接收显式 `HeartbeatIncidentPolicy`，删除 variadic/default fallback | DONE |
| P0 | Domain validation | 固定恢复次数为 3、最大 gap 为 `2 * interval`，拒绝直接构造的非合同 policy | DONE |
| P0 | Cross-entry regression | persisted `N=20` 分别通过 periodic 和公开 post-sync 验证 19/20 边界与错误 fail-closed | DONE |
| P0 | Ingress invariant | HTTP 拒绝同一请求内混用 heartbeat batch ID，并与共享 256 上限共同证明 768 候选边界 | DONE |
| P1 | Executable spec | database policy scenario 与 cross-layer checklist 记录入口传播、恢复证据和查询上界 | DONE |

### 4. Systematic Expansion

- **Similar issues**：任何同时由 worker、请求后 hook 和 reconciliation 触发的 settings-backed evaluator 都可能出现“一个入口读配置、另一个入口静默默认”。
- **Design improvement**：用必填领域策略对象和单一 resolver 让漏传成为编译错误；CAS retry 每次重读事实与策略。
- **Process improvement**：跨层检查必须列出所有公开入口；SQL 上界若依赖入口数量/分组约束，必须在真实入口和生产查询两端各有测试。

### 5. Knowledge Capture

- [x] `.trellis/spec/backend/database-guidelines.md`：完整 heartbeat policy、恢复、迁移和有界 SQL 合同。
- [x] `.trellis/spec/guides/cross-layer-thinking-guide.md`：多入口配置传播与派生查询上界检查项。
- [x] `.trellis/spec/backend/quality-guidelines.md`：本地完成门使用 `go.mod` 精确工具链，避免未来工具链 byte-golden 假失败。
