# IP 质量采集合同

> 本节记录 VPS IP 质量从 center settings 到 agent 采集、sync 上报、PostgreSQL 入库、VPS/API/资产决策读模型的可执行合同。修改 `agent/ipquality/`、`internal/contracts/agentapi`、`internal/center/ipquality`、`internal/center/store/*ip_quality*`、`internal/center/assetdecisions/*`、`db/migrations/*ip_quality*` 或前端 IP 质量展示时必须加载本文件。

## Scenario: VPS IP Quality Low-Frequency Facts

### 1. Scope / Trigger

- Trigger: 修改 IP 质量采集计划、agent collector、agent sync payload、center ingest、IP 质量 schema/view、VPS IP 质量 API、资产决策 IP 质量 evidence/readback。
- 目标：把低频 IP 质量事实纳入资产决策证据，但不把失败、部分失败、归属歧义误判成 VPS 质量差。
- 边界：本合同只覆盖 IP 质量和服务解锁；CPU/磁盘/内存性能、路由质量不在本合同内。

### 2. Signatures

- Settings JSON: `CenterSettings.IPQuality` / API field `ip_quality_settings`:
  - `enabled bool`
  - `frequency_seconds int`
  - `timeout_seconds int`
  - `raw_retention_days int`
  - `history_retention_days int`
  - `services []string`
- Agent plan: `agentapi.SyncPlan.IPQualityPlan`:
  - `enabled`
  - `frequency_seconds`
  - `timeout_seconds`
  - `services`
- Agent sync payload: `agentapi.SyncRequest.IPQualityReports []IPQualityReportPayload`:
  - report fields: `observed_at`、`agent_version`、`fingerprint`、`sync_batch_id`、`ip_address`、`ip_version`、`status`
  - optional normalized facts: ASN、organization、coordinates、use/registered region、risk level、error fields、`raw_json`
  - nested rows: `provider_results[]`、`service_unlocks[]`
- DB tables:
  - `ip_quality_reports`
  - `ip_quality_provider_results`
  - `ip_quality_service_unlocks`
- DB read views:
  - `ip_quality_assigned_vps_reports`
  - `ip_quality_latest_vps_summaries`
- HTTP API:
  - `GET /api/vps/{vps_id}/ip-quality`
  - VPS list/detail records may include `ip_quality_summary`.
- Asset decision evidence kinds:
  - `ip_quality_missing`
  - `ip_quality_stale`
  - `ip_quality_risk`
  - `ip_egress_mismatch`
  - `media_unlock_blocked`

### 3. Contracts

- Agent 必须用 Go 原生 HTTP collector；不得执行 `check.unlock.media`、`IP.Check.Place`、`run.NodeQuality.com`、`ecs.sh` 或任何远程 shell 脚本。
- Agent 默认 lookup provider 必须请求可返回 JSON 的 `https://api.ipapi.is`，查询当前出口 IP 时不拼接 `?q=self`；`https://ipapi.is/?q=self` 返回 HTML 首页，禁止作为默认采集源。
- Agent collector 必须兼容 ipapi.is 当前嵌套 JSON 结构：top-level `ip` / `is_datacenter` / `is_proxy` / `is_vpn` / `is_tor` / `is_abuser` / `is_crawler`，以及 `asn.asn` / `asn.org` / `asn.country` / `asn.type`、`company.name` / `company.type`、`location.country_code` / `location.country` / `location.latitude` / `location.longitude`。
- 默认 service unlock URL 必须为空；在没有明确可用 provider endpoint 前，不得默认请求 `unlock/{service}` 这类会返回 404/HTML 的 URL。服务解锁失败或禁用不能拖垮已经成功的 IP lookup 事实。
- IP 质量采集默认关闭；默认配置保留 86400 秒周期、15 秒 timeout、raw 90 天、history 365 天和默认服务集合。用户必须在 Settings 显式开启。
- IP 质量采集频率独立于 host sample / probe frequency。Agent 心跳 tick 只 drain 已完成报告，不在同步路径内阻塞外部 HTTP 请求。
- Agent due 判断必须按 `LastAttemptedAt` 节流；lookup 持续失败时也只能按 `frequency_seconds` 周期重试，不能因为 `LastSucceededAt` 为空而在每个 heartbeat tick 重复采集/上报 failure。
- Agent 本地状态通过 `agent/ipquality.StateStore` 记录上次采集时间；sync queue 持久化包含 IP 质量报告的整条 `SyncRequest`。
- `status` 只允许 `success`、`partial`、`failure`。失败报告允许没有 provider/service 细节，但仍必须带合法 `ip_address`、`ip_version`、metadata 和错误摘要。
- HTTP lookup 返回 HTML、非 JSON、空 body 或 JSON 解析失败时，Agent 必须生成短诊断 failure（如 `non_json_response`），不得把 HTML 原文写入 `error_summary` 或 raw envelope。
- Center 必须在 sync 事务内保存 IP 质量报告，且先通过 sync token/fingerprint 验证。fingerprint 不匹配的 IP 质量报告不得入库。
- Raw JSON 必须通过 `ipquality.SanitizeRawJSON` 兜底处理：递归替换 token/key/authorization/cookie/password 类字段为 `[redacted]`，并限制到 `ipquality.MaxRawJSONBytes` 内；超限时存合法 JSON truncation marker，不做字节截断。
- VPS 归属优先使用 active `vps_monitoring_instance_links`；没有 active link 时只用当前 `ipv4`/`ipv6` 与报告出口 IP 精确匹配。同一报告匹配多个 VPS 时标记 `ambiguous=true`。
- 用户侧 read model（`ip_quality_assigned_vps_reports` / `ip_quality_latest_vps_summaries`）只能包含真实 IP 事实：`status in ('success','partial')`、`ip_address <> '0.0.0.0'`、`ip_version in (4,6)`。原始 failure 报告继续保存在 `ip_quality_reports` 供诊断，但 VPS API、VPS 列表/详情和资产决策不得展示这些 failure 占位事实。
- 资产决策只能把 IP 质量作为 evidence / scoring / readback 输入，不自动执行迁移、取消或续费动作。
- 失败、partial、ambiguous 的 IP 质量报告只能产生缺口/需复核 evidence，不能产生 `ip_quality_risk` 负面风险。
- `is_server` / datacenter / hosting 本身不构成负面风险；只有 proxy/vpn/tor/abuser/robot、高风险等级、出口不一致或服务解锁阻断等信号才进入风险 evidence。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Settings `frequency_seconds < 60` | `settings.Validate` 返回 `ErrInvalidSettings` |
| Settings `timeout_seconds < 1` 或 `> 300` | `settings.Validate` 返回 `ErrInvalidSettings` |
| Settings `raw_retention_days < 7` | `settings.Validate` 返回 `ErrInvalidSettings` |
| Settings `history_retention_days < raw_retention_days` | `settings.Validate` 返回 `ErrInvalidSettings` |
| Settings service 不在默认允许集合 | `settings.Validate` 返回 `ErrInvalidSettings` |
| Sync report 缺 observed_at / metadata / ip / status | `/api/agent/sync` 返回 400 `invalid_request` |
| Provider result 缺 provider | `/api/agent/sync` 返回 400 `invalid_request` |
| Service unlock 缺 service 或 status | `/api/agent/sync` 返回 400 `invalid_request` |
| Sync token 或 fingerprint 不匹配 | sync ingest 拒绝，IP 质量报告不入库 |
| Lookup 成功但某个 service probe 失败 | Agent report `status=partial`，保留成功的 normalized facts，失败 service 写 `unknown` + error |
| Lookup 失败 | Agent report `status=failure`，不采 service unlock，error_code/error_summary 必填 |
| Lookup 返回 HTML / 非 JSON | Agent report `status=failure` + 短 `non_json_response` 诊断，raw_json 为空或合法 JSON，不保存 HTML |
| Lookup 持续失败 | Agent 更新 `LastAttemptedAt`，在 `frequency_seconds` 内不因 heartbeat 重复采集 |
| Center 只有 `failure` 或 `0.0.0.0` 报告 | 原始报告保留；VPS IP quality API 返回空 summary/latest/history，资产决策视为 `ip_quality_missing` |
| 较新 failure 与较旧有效报告并存 | 用户侧 latest/history 基于 filtered read view，展示较旧有效报告 |
| `partial` 包含真实 IP | 进入用户侧 read view，服务解锁维度可为空或 unknown |
| Raw JSON 含敏感字段 | 存库前替换为 `[redacted]` |
| Raw JSON 超过上限 | 存合法 truncation marker，不存无效 JSON |

### 5. Good/Base/Bad Cases

- Good: operator 在 Settings 开启 IP 质量，agent 根据 plan 后台采集，sync 上报后 center 在一个事务里保存主报告、provider 矩阵和 service unlock 矩阵，VPS 详情显示最新报告，资产决策显示风险/缺口 evidence。
- Base: IP 质量关闭时 plan 仍可下发 `enabled=false`，agent 不启动外部采集，UI 显示未采集/缺口而不是风险。
- Base: ChatGPT 和 Netflix service unlock 被阻断时，资产决策显示 `media_unlock_blocked`，但不自动迁移资产。
- Bad: agent 运行 `bash <(curl -Ls IP.Check.Place)` 或下载远程脚本解析 stdout。
- Bad: 把 `status=failure` 且 `risk_level=high` 的报告当作真实高风险 IP。
- Bad: raw JSON 直接 `append([]byte(nil), report.RawJSON...)` 入库，导致旧 agent 泄露 token 或写入超大 JSON。
- Bad: 通过出口 IP 同时匹配多台 VPS 后仍把风险 evidence 归到某一台 VPS。
- Bad: 默认请求 `https://ipapi.is/unlock/netflix` 或 `https://api.ipapi.is/unlock/netflix`，把 404/HTML 当作服务解锁失败并导致整份报告异常。
- Bad: VPS 详情/API/history 展示 `status=failure`、`ip_address=0.0.0.0` 的 lookup 占位报告，让用户误以为 VPS 出口 IP 是 `0.0.0.0`。

### 6. Tests Required

- `internal/contracts/agentapi`: sync plan 和 sync request JSON round-trip，覆盖 `ip_quality_plan` 与 `ip_quality_reports` 字段。
- `internal/center/settings`: 默认关闭、低频默认值、service normalization、非法频率/timeout/retention/service。
- `agent/ipquality`: due 判断、state store、HTTP collector 成功/partial/failure、service bool unlock 映射、raw JSON 脱敏和合法 JSON。
- `agent/ipquality`: 默认 lookup URL、默认 service probing 跳过、ipapi.is 嵌套 JSON 解析、HTML/非 JSON 清洁 failure、失败 attempt 节流。
- `agent/runtime`: plan 到后台 collector 的启动/drain 行为，disabled plan 不启动采集。
- `agent/syncqueue`: IP 质量报告随 sync request 离线队列 round-trip。
- `internal/center/http/handlers`: agent sync 写入 IP 质量 DTO、非法报告拒绝、raw JSON 脱敏；VPS IP quality API 返回 report/matrix/history。
- `internal/center/store`: sync batch 事务内写三表，repository latest/history 查询，migration view 使用正确 alias，retention 清 raw/history。
- `internal/center/store/migrate`: IP 质量 read view 重建迁移必须过滤 failure、`0.0.0.0` 和非法 IP version，并保留 partial 真实 IP 报告。
- `internal/center/assetdecisions`: IP 质量缺失/过期/失败/partial/ambiguous/风险/解锁阻断 evidence 与 readback 语义。
- `web`: API client、Settings、VPS list badge、VPS detail section、Asset Decisions evidence/current facts 展示。

### 7. Wrong vs Correct

#### Wrong

```go
// 错误：把上游 raw 原样存库，旧 agent 或异常 payload 可能泄露 token。
write.RawJSON = append([]byte(nil), report.RawJSON...)
```

#### Correct

```go
// 正确：center ingest 和 repository 直写路径都兜底脱敏/限长。
write.RawJSON = ipquality.SanitizeRawJSON(report.RawJSON)
```

#### Wrong

```go
// 错误：服务返回 {"unlocked": true} 时比较不同指针，永远不会命中。
if boolFromMap(payload, "unlocked") == boolPtr(true) {
	result.Status = "unlocked"
}
```

#### Correct

```go
// 正确：取出 bool 指针后比较值；false 明确映射为 blocked。
if unlocked := boolFromMap(payload, "unlocked"); unlocked != nil && *unlocked {
	result.Status = "unlocked"
} else if unlocked != nil {
	result.Status = "blocked"
}
```
