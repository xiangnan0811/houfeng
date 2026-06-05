# Design

## Scope

本任务在 `internal/center/assetdecisions` 的派生读模型中增加结构化证据评估，并把同一评估透传到前端类型和工作台 UI。评估只由现有 `Fact`、`GroupMember`、`GroupSummary` 和 `EvidenceChip` 推导，不读取 runtime facts，不新增 endpoint，不新增持久化表。

## Domain Model

新增证据评估结构：

- `EvidenceQualityTier`：`strong`、`usable`、`weak`、`blocked`。
- `EvidenceDecisionBias`：`keep`、`observe`、`complete_evidence`、`retire`、`migrate`、`review`。
- `EvidenceAssessment`：
  - `confidence_score`：0-100，表达证据是否足以支撑判断。
  - `pressure_score`：0-100，表达预算、续费、异常、取消联动等决策压力。
  - `readiness_score`：0-100，表达是否可以进入记录/推进，而不是先补资料。
  - `quality_tier`：由评分、缺口和阻塞信号归类。
  - `decision_bias`：由建议动作、风险和缺口派生。
  - `support_signal_count`、`risk_signal_count`、`gap_signal_count`。
  - `summary`：短文本，供 UI 和快照回看。

成员评估优先从成员事实和 chips 推导。组级评估由成员评估聚合：取平均可信度/准备度、最高或加权压力，累计信号计数，并根据组类型和最差成员状态决定 tier。

## Scoring Rules

成员评估以可解释规则为主，不做“智能预测”：

- 支撑信号：
  - 有活跃订阅。
  - 有服务、域名或 running target 上下文。
  - 有运行监控关联。
  - 承载服务。
  - 生命周期仍是普通 active/testing/to_migrate 场景。
- 风险信号：
  - 续费窗口内未评估。
  - budget risk、闲置付费、异常监控、active incident。
  - cancel/migrate 续费决策或取消联动。
  - inactive subscription 与 VPS 生命周期不一致。
- 缺口信号：
  - 缺订阅、缺监控、缺 provider/location/access、无服务上下文。
  - 证据源不可用，按来源不可用计入缺口，但不等同业务缺失。

评分保持保守：

- `confidence_score` 以 50 起步，支撑信号加分，缺口/证据源不可用降分。
- `pressure_score` 以 0 起步，风险信号和 group priority 加分。
- `readiness_score` 以 confidence 为基础，风险可提升决策必要性，缺口和 blocked tier 降低准备度。
- 评分统一 clamp 到 0-100。

## API Compatibility

`GroupSummary` 和 `GroupMember` 增加 `evidence_assessment` 字段。现有客户端字段保持不变，旧记录的 `evidence_snapshot` 仍作为 `Record<string, unknown>` 读取；只有新保存的记录包含 assessment。

## Frontend

UI 接入点：

- 组列表：新增“判断尺度”列，展示 tier、confidence / pressure / readiness 三个刻度和摘要。
- 组详情顶部：在 summary cards 中加入证据质量 card，详情 evidence 区展示摘要。
- 成员表：在建议列或独立列展示成员 evidence assessment，让同区/同服务商比较时能快速看出哪个节点证据更可靠、哪个只是缺证据。
- 记录详情：从 `evidence_snapshot.evidence_assessment` 读取快照并展示，缺失时优雅降级。

视觉约束：

- 保持运营工具的高密度、可扫描风格。
- 不增加营销式说明文案；页面内文字只服务决策。
- 避免横向溢出，表格已有横向滚动时只调整列宽和固定 min-width。

## Rollout

这是纯读模型和 UI 增强。失败回滚可以移除新增字段和 UI 显示，不影响已存在的组合派生和决策记录表结构。
