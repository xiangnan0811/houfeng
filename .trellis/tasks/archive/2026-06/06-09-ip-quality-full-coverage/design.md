# IP质量采集覆盖与展示完整性修复 - 技术设计

## Design Goal

本任务修复的是 IP 质量事实链路，不是单纯改页面。最终链路应满足：

- agent 默认不再只采 `ipapi.is`，而是采多源 IP 数据库判断和服务解锁结果。
- center 完整保存每次报告的主事实、逐源状态、结构化字段、extra JSON、raw JSON 和历史。
- API 和前端能展示“采到了什么、没采到什么、为什么没采到、各源之间是否分歧”，而不是只展示基础 IP 信息。
- 失败、未知、未配置不会被当作高风险；真实风险信号也不能被合并丢失。

## Root Cause Boundary

真实环境数据少的主因是 agent 采集覆盖不足：

- 当前 `agent/ipquality/collector.go` 默认只请求 `https://api.ipapi.is`。
- `defaultServiceURL` 为空，所以默认不会采任何服务解锁。
- center 当前 schema/API 只接收窄 provider/service 字段，没有 extra/source status/coverage，导致即便后续采集更多字段也容易丢失。
- 前端页面已经有独立 IP 质量页，但它只能展示 API 返回的窄数据，并且当前覆盖率基于“已有行数”，会把 1/1 误展示为完整。

## Source Policy

agent 仍然必须使用 Go 原生 HTTP collector。禁止执行：

- `bash <(curl -L -s check.unlock.media)`
- `bash <(curl -Ls IP.Check.Place)`
- `bash <(curl -sL https://run.NodeQuality.com)`
- `ecs.sh`
- 任何下载运行的远程 shell、二进制或未知脚本

第三方脚本和网页工具只作为字段覆盖和展示方式参考。默认上游必须满足：

- 无 API key 或用户已显式配置 key。
- 直接返回 JSON 或可稳定解析的公开网页响应。
- 不依赖临时前端 key、私有后端、Cloudflare 浏览器校验、内嵌登录 cookie、被盗用的会话、或需要执行脚本。
- 每个请求都有 timeout、User-Agent、大小限制、错误隔离和 source status。

## Default Provider Sources

默认 provider 源以“多源可用、失败可诊断、字段不丢失”为目标，首批设计如下：

| Source | Default | Role | Notes |
| --- | --- | --- | --- |
| `ipapi.is` | enabled | ASN/company/location/risk flags | 现有来源，继续保留，解析嵌套 JSON 和 abuser score。 |
| `ipquery.io` | enabled | ASN/location/risk flags/risk score | 无 key JSON，补充 datacenter/proxy/vpn/tor/risk_score。 |
| `proxycheck.io` | enabled | proxy/risk/type/location | 无 key JSON，补充 proxy/risk/type，失败按 source failure 保留。 |
| `ip2location.io` | enabled | geo/proxy/fraud context | 无 key JSON，有免费查询限制；限流时保留 failure row。 |
| `ipwho.is` | enabled | geo/ASN consistency | 无 risk flags，但对使用地/ASN 一致性有价值。 |

以下来源不作为默认启用，但作为 optional source 进入覆盖矩阵：

| Source | Reason |
| --- | --- |
| MaxMind official | 需要账号/license，不能默认无 key 使用。 |
| IPinfo official | privacy 字段需要 token；`widget/demo` 不是生产 API。 |
| IPRegistry | 脚本通过前端临时 key 获取，不能作为生产默认。 |
| IPData/IPQS/AbuseIPDB/Scamalytics | 商业/配额/API key 来源，默认展示未配置。 |
| `ipapi.co` | 实测容易 429，作为 optional/fallback，不计入默认完整性。 |
| `ip-api.com` | HTTP/free terms 限制，作为 optional，不默认启用。 |
| `ipinfo.check.place` 聚合端点 | 实测可能 Access denied，且属于脚本聚合服务，不作为默认生产依赖。 |
| IPPure/MeowVPS/Net.Coffee 聚合 API | 用户参考网站，后端是各自自有聚合服务，不直接作为默认上游。 |
| DB-IP HTML 页面 | 需要 HTML 解析，稳定性低，默认不启用。 |

## Default Service Probes

服务解锁不再由一个空 `defaultServiceURL` 代替。agent 内置服务 probe registry，每个 settings service 都会产生结果行或诊断行。

默认低侵入 probe：

| Service | Default behavior |
| --- | --- |
| Netflix | 请求两个公开 title 页面，区分 full/originals/block/unknown，解析 region。 |
| ChatGPT/OpenAI | 请求 OpenAI compliance/ChatGPT endpoints 和 Cloudflare trace，区分 available/blocked/web-only/app-only/unknown。 |
| YouTube Premium | 请求 Premium 页面，解析是否可用、region、China/no-premium/unknown。 |
| Amazon Prime Video | 请求 Prime Video 公开页面，解析 currentTerritory。 |
| TikTok | 请求主页或 explore 页面，解析 region；遇到 bot/JS challenge 时返回 unknown + diagnostic。 |
| Reddit | 请求 `www.reddit.com`，用 HTTP status 和 country attribute 判断可用性。 |

Disney+ 保留为默认服务集合中的一项，但实现必须谨慎：

- 如果可以用公开浏览器客户端流程完成且不依赖私有 cookie，则采集真实结果。
- 如果需要脚本仓库里的 cookie/token 才能稳定运行，则不得默认使用该路径；agent 仍上报 `service=disney-plus`、`status=unknown`、`probe_status=skipped`、`error_code=unsupported_default_probe`，页面显示“未采集/需可选配置”。

这样可以避免真实环境继续显示 `0 / 0`，同时不把不可验证的探测伪装成准确结果。

## Contract Changes

保持旧字段兼容，新增字段一律 optional，旧 agent 上报仍可入库。

### Provider Result v2

在 `agentapi.IPQualityProviderResultPayload`、center write/read DTO、DB row、web type 中新增：

- `status`: `success | failure | skipped | not_configured`
- `source_type`: `default | optional | custom`
- `latency_ms`: 请求耗时
- `extra_json`: provider-specific JSON，脱敏限长后保存

旧字段继续保留：

- `provider`
- `usage_type`
- `company_type`
- `risk_level`
- `risk_score`
- `region_code`
- `region_name`
- `is_proxy`
- `is_tor`
- `is_vpn`
- `is_server`
- `is_abuser`
- `is_robot`
- `error_code`
- `error_summary`

### Service Unlock v2

在 `agentapi.IPQualityServiceUnlockPayload`、center write/read DTO、DB row、web type 中新增：

- `source`: probe source name，例如 `netflix_title_probe`、`openai_compliance_probe`
- `probe_status`: `success | failure | skipped | not_configured`
- `latency_ms`
- `extra_json`

旧字段继续保留：

- `service`
- `status`: `unlocked | blocked | partial | unknown`
- `region`
- `unlock_type`
- `error_code`
- `error_summary`

### Report Coverage

在 report/API 层新增 `coverage` 对象：

- `expected_provider_count`
- `successful_provider_count`
- `failed_provider_count`
- `skipped_provider_count`
- `not_configured_provider_count`
- `expected_service_count`
- `successful_service_count`
- `failed_service_count`
- `skipped_service_count`
- `not_configured_service_count`

`summary.provider_count` 和 `summary.unlockable_count` 保持兼容，但前端新页面必须优先使用 `coverage`，不能再用 `Math.max(existing, existing)` 这种自洽式完整率。

## Agent Design

### Source Registry

新增 source registry，避免一个巨大的 collector 文件继续增长：

- `agent/ipquality/source.go`: source interface、result envelope、status constants。
- `agent/ipquality/providers.go`: default provider registry。
- `agent/ipquality/provider_parsers.go`: provider-specific parser。
- `agent/ipquality/services.go`: service probe registry。
- `agent/ipquality/service_parsers.go`: service-specific parser。
- `agent/ipquality/collector.go`: orchestration、timeout、status aggregation、raw envelope。

核心接口：

```go
type ProviderSource interface {
	Name() string
	SourceType() string
	Collect(context.Context, HTTPClient, string) ProviderSourceResult
}

type ServiceProbe interface {
	Service() string
	Source() string
	Collect(context.Context, HTTPClient) ServiceProbeResult
}
```

`Collect` 的 `targetIP` 可以为空；provider 可先通过 self endpoint 获取出口 IP。report 的 canonical IP 优先来自成功 provider 的一致 IP，顺序为 `ipapi.is`、`ipquery.io`、`ipwho.is`、其他来源。多个成功 provider 返回不同出口 IP 时，report 标记 `partial`，`extra_json` 中写入 `ip_candidates`，center 继续按现有 ambiguous 规则归属。

### Failure Isolation

- 每个 source 独立 timeout，默认不超过全局 timeout 的一半，且受总 context 控制。
- provider 失败不影响其他 provider。
- service 失败不影响 provider。
- 至少一个 provider 成功且 IP 合法时，report 为 `success` 或 `partial`。
- 所有 provider 均失败时，report 为 `failure`，继续保留 `0.0.0.0` 占位，但 center 用户侧 read view 仍过滤 failure/0.0.0.0。
- `not_configured/skipped` 不计为风险，也不把服务判定为 blocked。

### Raw And Extra

agent raw envelope 结构：

```json
{
  "providers": {
    "ipapi.is": { "status": "success", "raw": {} },
    "ipquery.io": { "status": "failure", "error_code": "timeout" }
  },
  "services": {
    "netflix": { "status": "success", "raw": {} }
  },
  "diagnostics": {
    "source_version": "v2",
    "elapsed_ms": 1234
  }
}
```

agent 先做基础敏感字段脱敏，center 仍必须再次调用 `ipquality.SanitizeRawJSON`。

## Center Design

### Migration

新增迁移 `0042_extend_ip_quality_source_details.sql`：

- `ip_quality_reports`:
  - add `coverage_json jsonb`
  - add `diagnostics_json jsonb`
- `ip_quality_provider_results`:
  - add `status text not null default 'success'`
  - add `source_type text not null default 'default'`
  - add `latency_ms integer`
  - add `extra_json jsonb`
- `ip_quality_service_unlocks`:
  - add `source text not null default ''`
  - add `probe_status text not null default 'success'`
  - add `latency_ms integer`
  - add `extra_json jsonb`
  - replace unique index `(report_id, service)` with `(report_id, service, source)` while preserving old rows by using empty source.

Read views must continue filtering:

- `status in ('success', 'partial')`
- `ip_address <> '0.0.0.0'`
- `ip_version in (4, 6)`

View summary counts should expose both old counts and coverage JSON through repository reads.

### Ingest

`internal/center/http/handlers/agent.go` maps new fields into `ipquality.ReportWrite`:

- Validate provider `provider` nonblank and status allowed.
- Validate service `service`, `status`, `probe_status` allowed.
- Sanitize `raw_json`, provider `extra_json`, service `extra_json`, report `diagnostics_json`.
- Truncate via legal JSON marker, never byte-cut invalid JSON.

### API

Existing endpoint remains:

- `GET /api/vps/{vps_id}/ip-quality`

Response gains optional fields:

- `coverage`
- provider rows with `status/source_type/latency_ms/extra_json`
- service rows with `source/probe_status/latency_ms/extra_json`

Add history detail endpoint:

- `GET /api/vps/{vps_id}/ip-quality/reports/{report_id}`

The new endpoint returns one historical report with the same provider/service/detail shape as latest. It must use VPS assignment rules so one VPS cannot read an unrelated report.

## Frontend Design

Use the existing full IP quality page as the main surface. VPS detail page stays as summary card plus jump link.

Changes:

- Hero metrics use `coverage`, not current row count.
- Provider matrix groups rows by `source_type` and shows `status`, latency, risk fields, and detail drawer for `extra_json`.
- Optional not configured providers appear in a separate “未配置/未采集来源” block.
- Service unlock cards show `probe_status`, `source`, latency, status, region, unlock type, and error diagnostic.
- Risk matrix counts only successful provider rows with actual true/false risk fields.
- Unknown/skipped/not_configured service rows do not count as blocked.
- History list supports selecting a historical report and loading full provider/service/detail data.
- Raw/extra JSON viewer remains collapsed by default and shows only sanitized JSON.

## Asset Decision Compatibility

Asset decision evidence remains summary-driven:

- Missing/old/ambiguous/partial continues producing gap/review evidence.
- Negative risk evidence only uses true risk signals from successful provider rows and risky `risk_level`.
- `is_server`/datacenter remains context, not standalone negative risk.
- Blocked service evidence only uses `status=blocked` with `probe_status=success`.

This prevents unknown/skipped/not_configured rows from causing false migration/cancellation pressure.

## Operational Notes

- IP quality remains low frequency and background-only.
- Default timeout remains governed by center settings.
- Source failures are expected and visible; the system should degrade to partial, not go silent.
- Rate limited providers stay in history as source failure rows, making real environment issues diagnosable.
- Existing reports remain readable because all new fields have defaults and API fields are optional.

## Rollback Shape

If a source proves noisy after release:

- Disable the source in agent registry defaults in a follow-up release.
- Existing rows remain historical evidence.
- Frontend continues displaying skipped/failure rows without breaking.
- DB migration is additive except the service unique index change; rollback should recreate `(report_id, service)` only after de-duplicating by source, so normal deployment should rely on forward migration rather than DB rollback.
