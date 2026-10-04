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
  - `stale_after_seconds int`
  - `services []string`
- Agent plan: `agentapi.SyncPlan.IPQualityPlan`:
  - `enabled`
  - `frequency_seconds`
  - `timeout_seconds`
  - `services`
  - `collect_request_id`（可选，立即采集请求 ID）
- Agent sync payload: `agentapi.SyncRequest.IPQualityReports []IPQualityReportPayload`:
  - report fields: `observed_at`、`agent_version`、`fingerprint`、`sync_batch_id`、`ip_address`、`ip_version`、`status`
  - 立即采集的报告在 `diagnostics_json.collect_request_id`（`agentapi.IPQualityDiagnosticsCollectRequestIDKey`）回传请求 ID；报告顶层不新增字段
  - optional normalized facts: ASN、organization、coordinates、use/registered region、risk level、error fields、`raw_json`、`coverage`、`diagnostics_json`
  - nested provider rows `provider_results[]`: `provider`、`status`、`source_type`、usage/company/risk/region、proxy/tor/vpn/server/abuser/robot flags、`latency_ms`、`extra_json`、error fields
  - nested service rows `service_unlocks[]`: `service`、`source`、`status`、`probe_status`、`region`、`unlock_type`、`latency_ms`、`extra_json`、error fields
- DB tables:
  - `ip_quality_reports`
  - `ip_quality_provider_results`
  - `ip_quality_service_unlocks`
- DB read views:
  - `ip_quality_assigned_vps_reports`
  - `ip_quality_latest_vps_summaries`
- HTTP API:
  - `GET /api/vps/{vps_id}/ip-quality`
  - `GET /api/vps/{vps_id}/ip-quality/reports/{report_id}`
  - `GET /api/vps/{vps_id}/ip-quality/collect`：`enabled`、`available`、`unavailable_reason`（`disabled` / `no_monitoring_instance` / `agent_not_bound` / `monitoring_paused`）、`monitoring_instance_id`、`agent_last_sync_at`、最近一次 `request`
  - `POST /api/vps/{vps_id}/ip-quality/collect`：可用时 202 返回同结构并带 `request`；不可用时 409 `{error, reason}`
  - `request`：`request_id`、`monitoring_instance_id`、`status`（`pending` / `dispatched` / `completed` / `expired`）、`requested_at`、`expires_at`、`dispatched_at`、`completed_at`、`report_status`、`error_summary`
  - VPS list/detail records may include `ip_quality_summary`.
- Asset decision evidence kinds:
  - `ip_quality_missing`
  - `ip_quality_stale`
  - `ip_quality_risk`
  - `ip_egress_mismatch`
  - `media_unlock_blocked`

### 3. Contracts

- Agent 必须用 Go 原生 HTTP collector；不得执行 `check.unlock.media`、`IP.Check.Place`、`run.NodeQuality.com`、`ecs.sh` 或任何远程 shell 脚本。
- Agent 默认 provider registry 必须是多源采集，至少覆盖 `ipapi.is`、`ipquery.io`、`proxycheck.io`、`ip2location.io`、`ipwho.is` 这类 Go-native HTTP JSON/稳定响应源；不能退化为只采 `ipapi.is` 一个 provider 后把覆盖率展示为完整。
- 需要账号、API key、商业授权、临时网页 key、登录 cookie、浏览器挑战或第三方聚合后端的来源必须作为 optional source 上报 `not_configured` / `skipped` 诊断行，不能伪造脚本级覆盖率，也不能隐藏覆盖缺口。
- `ipapi.is` 请求可返回 JSON 的 `https://api.ipapi.is`，查询当前出口 IP 时不拼接 `?q=self`；`https://ipapi.is/?q=self` 返回 HTML 首页，禁止作为默认采集源。`ipquery.io` 的当前出口 IP 请求必须使用 `https://api.ipquery.io/?format=json`；纯文本根路径不是有效 JSON 来源，指定 IP 查询继续使用转义后的路径。
- Agent collector 必须兼容 ipapi.is 当前嵌套 JSON 结构：top-level `ip` / `is_datacenter` / `is_proxy` / `is_vpn` / `is_tor` / `is_abuser` / `is_crawler`，以及 `asn.asn` / `asn.org` / `asn.country` / `asn.type`、`company.name` / `company.type`、`location.country_code` / `location.country` / `location.latitude` / `location.longitude`。
- Legacy/custom service unlock URL 默认必须为空；保留 `HTTPCollectorOptions.LookupURL` / `ServiceURL` 及其 legacy 分流，只有调用方显式提供 `ServiceURL` 时才请求 custom JSON service adapter；不得自动回退或把内置网站协议当作已验证能力。
- 默认 service registry 不执行网页/HTTP service probe；它必须对 normalized settings service 集合按输入顺序生成逐服务诊断。当前七个默认服务及其历史 source 身份为：`netflix` → `netflix_title_probe`、`chatgpt` → `openai_status_probe`、`youtube-premium` → `youtube_premium_page_probe`、`amazon-prime-video` → `prime_video_page_probe`、`disney-plus` → `disney_default_probe`、`tiktok` → `tiktok_home_probe`、`reddit` → `reddit_home_probe`。
- 对上述七个默认服务，诊断固定为 `status=unknown`、`probe_status=skipped`、`error_code=unsupported_default_probe`、`error_summary=safe default probe is not available without verified business evidence`；不得填写 `region`、`unlock_type`、`latency_ms`、虚假的 HTTP raw 或 `extra_json`。默认七项的 coverage 必须为 expected 7、successful 0、failed 0、skipped 7。
- 默认诊断生成器不得启动 goroutine、timer 或 HTTP 请求；任何默认服务域名都不得被请求。未知服务保留 `source=default_probe_registry`、`status=unknown`、`probe_status=skipped`、`error_code=unsupported_service` 及既有摘要；空输入返回空结果，去空白/小写/去重继续由既有 `normalizedServices` 负责。
- 显式 custom JSON service adapter 的字段优先级为 `status` → `unlock_status`，只有状态为空才使用 `unlocked` bool 映射（`true` → `unlocked`、`false` → `blocked`）；`region` / `unlock_type` 及其既有别名保持不变。状态经 trim/lower 后仅允许 `unlocked`、`blocked`、`partial`、`unknown`。
- 字段选择保留既有 `stringFromMap` 行为：若 `status` 是已存在的空字符串（包括 trim 后为空），不继续读取 `unlock_status`，而是进入 `unlocked` bool 回退；例如 `{"status":"","unlock_status":"partial","unlocked":true}` 的结论是 `unlocked`。没有有效 bool 结论时仍按无结论失败处理。
- custom JSON 的 `unlocked`、`blocked`、`partial` 均写 `probe_status=success`；保留已有业务错误说明（例如 `blocked` 的业务 `error_code` 不是探测失败）。`unknown`、空对象、缺少结论或非法状态写 `status=unknown`、`probe_status=failure`、`error_code=invalid_response`、固定摘要 `service response did not establish a business conclusion`，并清空 region/unlock_type。HTTP/JSON 读取错误（包括 HTML、空 body、JSON 解析失败和所有非 2xx）写 `probe_status=failure`、`status=unknown`，保留 `probe_failed` 语义和安全 raw。
- custom failure 会在 lookup 成功时使报告为 `partial`；默认 skipped 只是能力诊断，不等于执行失败，也不把成功的 IP lookup 改成 partial。每个 provider source 仍有独立 timeout，且受总采集 context 约束；一个慢源只能生成该源 failure/timeout 行，不能吃完整体 timeout 或阻塞其他结果。
- IP 质量采集默认开启；默认配置为 86400 秒周期、15 秒 timeout 和默认服务集合。没有 settings 行、JSON 缺 `enabled` 字段时（`settings.Default()`、sync plan SQL 兜底、`IPQualityEnabled` 读取）都按开启处理；已保存的显式值（包括关闭）原样保留，不做迁移覆盖。只传零值对象或 `{"enabled":false}` 时视为显式关闭，其余字段补默认值。低频报告与脱敏原始结果长期保留，不再提供 raw/history 按天自动删除配置，见 [数据保留合同](retention.md)。
- 立即采集：`POST /api/vps/{vps_id}/ip-quality/collect` 只为该 VPS 当前监控实例（未退役、未归档、所属 VPS 未归档）登记请求；采集关闭、无当前实例、agent 未绑定或监控暂停时返回 409 与原因，不登记请求。同一实例已有 `pending` / `dispatched` 请求时返回该请求，不重复触发外部查询。
- 立即采集请求只保存在 center 进程内存（`ipquality.CollectRequests`），TTL 10 分钟，结束后保留 30 分钟供页面展示；center 重启即丢失，用户重新发起即可。该机制假设单 center 进程；多副本部署前必须改为持久化协调。不新增数据库表，不复用命令白名单/pending action，也不写命令审计。
- `syncing.Service` 在 `ApplyBatch` 成功后，只在 `Disposition=recorded`（报告确已入库）时用本批报告完成进行中的请求；`suppressed`（暂停/退役等未写入）与 `exact_duplicate` 不改变请求。随后在 plan `enabled=true` 且未 `stop_collection` 时把仍待执行的请求 ID 写入 plan 副本并标记 `dispatched`；不得修改仓库返回的 plan。IP 质量关闭或实例暂停时不下发，请求保持原状直到过期。
- 请求与报告按 ID 精确关联：agent 把请求 ID 写进报告的 `diagnostics_json.collect_request_id`，center 在脱敏前从原始 diagnostics 读取并按 `[A-Za-z0-9_-]{1,64}` 规整，只有带回同一 ID 的已入库报告才能完成请求。周期报告、离线补传或其他请求的报告不论时间多新都不算。不在报告顶层加字段：旧 center 的 sync 解码拒绝未知字段，回滚期间排队的整批数据会被 agent 丢弃；`diagnostics_json` 是旧 center 已接受的自由 JSON，该键不是敏感字段，会随诊断保存。
- Agent 收到新的 `collect_request_id` 时不看周期立即采集一次；周期采集进行中收到请求时由这一轮认领并在报告中带回该 ID，不再重复采集。已产出报告的请求 ID 只在 agent 内存中去重（center 收到报告前会继续下发同一 ID）；进程重启或 `Stop` 后清空，此时若 center 仍下发同一 ID 会再采一次——宁可多采一次也不漏采。旧 agent 忽略该 plan 字段，请求到期后显示超时。
- 过期后到达的报告照常入库，但不再改变请求状态；失败报告使请求 `completed` 且 `report_status=failure`，用户侧报告仍按下述 read view 规则只展示有效事实。
- IP 质量 stale 窗口由 `stale_after_seconds` 控制，默认 604800 秒。该值必须不小于 `frequency_seconds`，避免还没到下一次采集就判过期。API、Settings 页面、Go/TS 类型和迁移默认必须同步该字段。
- IP 质量采集频率独立于 host sample / probe frequency。Agent 心跳 tick 只 drain 已完成报告，不在同步路径内阻塞外部 HTTP 请求。
- Agent due 判断必须按 `LastAttemptedAt` 节流；lookup 持续失败时也只能按 `frequency_seconds` 周期重试，不能因为 `LastSucceededAt` 为空而在每个 heartbeat tick 重复采集/上报 failure。
- Agent 本地状态通过 `agent/ipquality.StateStore` 记录上次采集时间；sync queue 持久化包含 IP 质量报告的整条 `SyncRequest`。
- `status` 只允许 `success`、`partial`、`failure`。失败报告允许没有 provider/service 细节，但仍必须带合法 `ip_address`、`ip_version`、metadata 和错误摘要。
- Provider row `status` 只允许 `success`、`failure`、`skipped`、`not_configured`；`source_type` 只允许 `default`、`optional`、`custom`。Service row `probe_status` 只允许 `success`、`failure`、`skipped`、`not_configured`。
- `coverage` 必须记录 expected/successful/failed/skipped/not_configured provider 与 service 计数；前端完整页优先使用 summary 的 `coverage`，其次使用 selected report 的 `coverage`。两者均缺失时显示“—”，不得从历史行数或默认 success 重建成功率、归一成 100%。
- Service coverage 只有 `probe_status=success` 且 `status` 为 `unlocked`、`blocked` 或 `partial` 才计 successful；`skipped` 与 `not_configured` 各计原分类，其余组合（包括空 probe_status、`unknown/success`、`unlocked/failure`）计 failed。该谓词只约束新报告计数，历史入库/读取的兼容事实不回写。
- 默认采集路径的新报告在 `diagnostics_json` 中必须保留 `"source_version":"v2"` 并加入数值 `"service_probe_revision":1`；该 revision 只表示默认诊断政策，不表示七项服务已验证成功，不新增报告顶层字段或数据库列。显式 custom/legacy 路径不附加该默认来源标记。历史报告的 rows、coverage、评分事实不回写、不重算。
- selected report 有 service rows，但既无非空 `diagnostics_json.source_version`、也无任何非空 service `source` 时，页面显示来源不可核验提示；不能凭缺失来源把历史结果归类为默认或 custom。已知默认来源继续按 revision 显示停用/旧规则提示，显式 custom 来源不显示默认停用警告；提示使用独立可信度 note，不占用立即采集状态 live region。
- HTTP lookup 返回 HTML、非 JSON、空 body 或 JSON 解析失败时，Agent 必须生成短诊断 failure（如 `non_json_response`），不得把 HTML 原文写入 `error_summary` 或 raw envelope。
- Center 必须在 sync 事务内保存完整 IP 质量报告、provider/service 扩展元数据、coverage 与 diagnostics，与心跳、样本、观测和 batch receipt 一起提交或回滚；不得另开 SaveReports 事务。先通过 sync token/fingerprint 验证，fingerprint 不匹配的报告不得入库。相同 service 不同 source 是不同事实，相同 `(service, source)` 重复须失败并回滚整批。exact replay 不增事实、不重复完成立即采集请求。
- 两个写入入口共享完整 writer 与 sanitizer，但保留入口语义：缺 raw 的 sync 写 JSON null，SaveReports 写 SQL NULL；非空非法 raw 经 sanitizer 得到 SQL NULL。显式 received_at 原样保留，零值分别使用批次 received_at 与 SaveReports 的当前 UTC 时间；缺 coverage/diagnostics/extra 保持 SQL NULL。
- Raw JSON、provider `extra_json`、service `extra_json` 和 report `diagnostics_json` 必须通过 `ipquality.SanitizeRawJSON` / extra JSON sanitizer 兜底处理：递归替换 token/key/authorization/cookie/password 类字段为 `[redacted]`，并限制到对应最大字节数内；超限时存合法 JSON truncation marker，不做字节截断。
- VPS 归属优先使用 active `vps_monitoring_instance_links`；有 active link 时即使没有有效报告也禁止地址 fallback。没有 active link 时，当前 `ipv4`/`ipv6` 与报告出口 IP 按解析后的 host address 身份比较：IPv6 展开/压缩/大小写等价，IPv4-mapped IPv6 只与同一 mapped 地址等价，不与普通 IPv4 合并。同一报告匹配多个 VPS 时标记 `ambiguous=true`；unlink 后恢复 fallback。SQL view 与 Overview 使用相同规则，不改原始存储文本或历史报告事实。
- 地址 fallback 的报告身份与无 active link 资产身份分别在已过滤的 MATERIALIZED CTE 中计算；JOIN 只比较已解析值，避免对每个报告/资产组合重复执行 parser。assigned view 与 Overview 同步这一计算边界，不以新增索引或历史回填替代。
- Overview 的窄 summary 不新增字段：先按原排序选定最新归属报告，再检查 fallback 是否被另一台无 active link 的 VPS 同址命中。选定报告归属歧义时返回无 summary，沿用 `missing` 缺口提示，不把其 risk/partial/stale 归给该 VPS，也不得跳过它改用更旧的唯一归属报告；最新报告唯一归属与 active-link 分支的既有结果保持不变。
- Go/SQL host identity 解析均 trim Unicode White_Space，拒绝 CIDR、zone、端口、短 IPv4、前导零及越界十进制 octet；合法 unspecified 可解析，既有业务过滤不变。无效文本不参与 fallback，但不妨碍 active-link 归属。Agent 候选只在存在不同有效地址时确认冲突；出口不一致 evidence 需 success、非 ambiguous、有效报告地址和至少一个有效当前地址。
- VPS PATCH 仅改变地址表示不新增 IP history；真实地址改变仍记史。任一侧不可解析时按 trim 后原文比较，修正/删除非法旧地址也记史。独立创建 IP history 使用同一无变化判定；四个历史地址字段保留原文。VPS 输入仍 trim-only，不新增 API 拒绝规则，不放宽 evidence 地址族校验。
- 用户侧 read model（`ip_quality_assigned_vps_reports` / `ip_quality_latest_vps_summaries`）只能包含真实 IP 事实：`status in ('success','partial')`、`ip_address <> '0.0.0.0'`、`ip_version in (4,6)`。原始 failure 报告继续保存在 `ip_quality_reports` 供诊断，但 VPS API、VPS 列表/详情和资产决策不得展示这些 failure 占位事实。
- 历史详情 API 必须按 VPS assignment 规则读取 selected report，响应中必须同时返回该 report 的 `summary`、`latest_report`、provider rows、service rows；不能只返回 row 细节而让前端历史视图空态或退回 latest summary。
- VPS Overview 在 IP 质量关闭时只做 availability 检查，立即返回 `not_configured` + `SectionReady`，不得查询 `GetLatestVPSIPQualitySummary`，也不得发出 `ip_quality_disabled_has_history`。历史注释不是当前健康判断的一部分；历史只在 `GET /api/vps/{vps_id}/ip-quality` 详情页展示。关闭路径上的 summary 超时不得把 Overview 标成 `source.unavailable.v1`。
- 资产决策只能把 IP 质量作为 evidence / scoring / readback 输入，不自动执行迁移、取消或续费动作。
- `ip_quality.report/v1`是authoritative evidence source kind；只有生成新的logical evidence snapshot时才按该snapshot的`logical_size_bytes`消耗project evidence capacity。IP质量report/provider/service表大小、raw retention、coverage、风险等级或source availability都不能成为quota counter、capacity fallback或janitor删除依据。
- evidence capacity/maintenance alert只来自evidence-owned aggregate store state。IP质量source失败/partial/stale/ambiguous仍按本合同产生缺口或复核语义，不能被改写成`capacity_unavailable`、`quota_exceeded`或janitor failure；反向也不能用capacity alert伪造IP质量风险。
- 失败、partial、ambiguous 的 IP 质量报告只能产生缺口/需复核 evidence，不能产生 `ip_quality_risk` 负面风险；provider `status != success`、service `probe_status != success`、`skipped`、`not_configured`、`unknown` 不得被计入负面风险或服务阻断。
- `is_server` / datacenter / hosting 本身不构成负面风险；只有 successful provider 的 proxy/vpn/tor/abuser/robot、高风险等级、出口不一致，或成功的显式 custom service adapter 报告服务解锁阻断等信号才进入风险 evidence；默认 `skipped` 行不产生服务阻断。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Settings `frequency_seconds < 60` | `settings.Validate` 返回 `ErrInvalidSettings` |
| Settings `timeout_seconds < 1` 或 `> 300` | `settings.Validate` 返回 `ErrInvalidSettings` |
| Settings 带 `raw_retention_days` 或 `history_retention_days` | 严格 JSON 解码拒绝已移除字段；低频报告长期保留 |
| Settings service 不在默认允许集合 | `settings.Validate` 返回 `ErrInvalidSettings` |
| Settings `stale_after_seconds < frequency_seconds` | `settings.Validate` 返回 `ErrInvalidSettings` |
| Sync report 缺 observed_at / metadata / ip / status | `/api/agent/sync` 返回 400 `invalid_request` |
| Provider result 缺 provider | `/api/agent/sync` 返回 400 `invalid_request` |
| Provider result `status` / `source_type` 非法 | `/api/agent/sync` 返回 400 `invalid_request` |
| Service unlock 缺 service 或 status | `/api/agent/sync` 返回 400 `invalid_request` |
| Service unlock `probe_status` 非法 | `/api/agent/sync` 返回 400 `invalid_request` |
| Sync token 或 fingerprint 不匹配 | sync ingest 拒绝，IP 质量报告不入库 |
| 某个 provider timeout / rate limit / non-json | 该 provider 写 `status=failure`、短错误摘要和 latency；其他 provider 继续采集 |
| Lookup 成功但显式 custom JSON service adapter 失败 | Agent report `status=partial`，保留成功的 normalized facts，失败 service 写 `probe_status=failure`、`status=unknown` + error；默认 service `skipped` 不使报告 partial |
| 显式 custom service adapter 返回 429 / 403 / 451 / 404 / 5xx，或 HTML/非 JSON/空 body | 不判定解锁；写 `probe_status=failure`、`status=unknown`，不根据裸 HTTP 状态码形成 blocked |
| 显式 custom JSON 缺少结论、状态为 unknown 或状态非法 | 写 `probe_status=failure`、`status=unknown`、`error_code=invalid_response`，清空 region/unlock_type；lookup 成功时报告为 partial |
| 默认服务不可安全探测 | 不发服务网络请求；写 `probe_status=skipped`、`status=unknown`、`error_code=unsupported_default_probe`，页面显示诊断，不计入 blocked 或 successful |
| 默认七项服务集合 | 逐服务按规范化输入顺序生成 source 连续的诊断行；coverage 为 expected 7 / successful 0 / failed 0 / skipped 7，并写 `diagnostics_json["service_probe_revision"]=1` |
| Lookup 失败 | Agent report `status=failure`，不采 service unlock，error_code/error_summary 必填 |
| Lookup 返回 HTML / 非 JSON | Agent report `status=failure` + 短 `non_json_response` 诊断，raw_json 为空或合法 JSON，不保存 HTML |
| Lookup 持续失败 | Agent 更新 `LastAttemptedAt`，在 `frequency_seconds` 内不因 heartbeat 重复采集 |
| Center 只有 `failure` 或 `0.0.0.0` 报告 | 原始报告保留；VPS IP quality API 返回空 summary/latest/history，资产决策视为 `ip_quality_missing` |
| 较新 failure 与较旧有效报告并存 | 用户侧 latest/history 基于 filtered read view，展示较旧有效报告 |
| `partial` 包含真实 IP | 进入用户侧 read view，服务解锁维度可为空或 unknown |
| Raw JSON 含敏感字段 | 存库前替换为 `[redacted]` |
| Raw JSON 超过上限 | 存合法 truncation marker，不存无效 JSON |
| Provider/service `extra_json` 超过上限 | 存合法 extra truncation marker，不存无效 JSON |
| 历史详情 report 属于其他 VPS 或未被该 VPS assignment 命中 | API 不返回该 report 细节 |
| 立即采集时采集关闭 / 无当前监控实例 / agent 未绑定 / 监控暂停 | `POST .../ip-quality/collect` 返回 409 与 `reason`，不登记请求 |
| 已有进行中的立即采集请求 | POST 返回同一请求，不新建 |
| 请求 10 分钟内未收到回传 | 状态变为 `expired`，不再下发；迟到报告照常入库 |
| Agent 重复收到同一 `collect_request_id` | 只采集一次 |
| 周期采集进行中收到立即采集请求 | 当前这一轮认领请求，完成后不再补采 |
| 立即采集中途或报告取走前被 `Stop`，或 agent 重启 | 请求不再算已处理，再次下发时重新采集 |
| 请求下发后实例被暂停，报告随 suppressed 批次到达 | 报告未入库，请求不完成，直到过期 |
| 请求下发后到达的周期报告或离线补传（未带回该请求 ID） | 不完成请求 |
| 被认领的周期采集在发起前已开始（例如 300 秒超时） | 报告带回请求 ID，照常完成 |
| diagnostics 中回传的 ID 超长、非字符串或含非法字符 | 忽略该 ID，报告照常入库 |
| Settings `enabled=false` 且 latest summary 查询超时/失败 | Overview IP Quality 仍为 `not_configured` + `SectionReady`；不得把 `ip_quality` 写入 `JudgementSourcesUnavailable`，也不得发出 `source.unavailable.v1` 或抬升 overall status。历史注释只能 best-effort |

### 5. Good/Base/Bad Cases

- Good: operator 在 Settings 开启 IP 质量，agent 根据 plan 后台采集，sync 上报后 center 在一个事务里保存主报告、provider 矩阵和 service unlock 矩阵，VPS 详情显示最新报告，资产决策显示风险/缺口 evidence。
- Good: 多个默认 provider 成功/失败混合时，页面展示 provider/source 状态、coverage、失败诊断和 extra details；成功 provider 的风险信号进入风险矩阵，失败/未配置来源只进入采集完整性。
- Good: 默认七项服务生成逐服务 `unknown/skipped` 能力诊断、零服务网络请求和 0/7 service coverage；这不改变成功 provider 的 IP 事实，也不把未知显示为 blocked/unlocked。
- Good: 用户从历史列表打开旧 report，API 返回该 report 的 summary、provider rows、service rows、coverage 和 diagnostics。
- Good: 新接入 agent 的 VPS 在 IP 质量页点击“立即采集”，下一次 sync 收到带 `collect_request_id` 的 plan，agent 不等 86400 秒周期立即采集，回传后页面自动刷新到新报告。
- Base: IP 质量关闭时 plan 仍可下发 `enabled=false`，agent 不启动外部采集，Overview 判断为 `not_configured` 而不是 missing/risk。关闭后的历史 summary 查询失败不能改变当前健康判断。
- Base: 调用方显式配置的 custom JSON service adapter 报告 ChatGPT 或 Netflix `blocked` 时，资产决策可显示 `media_unlock_blocked`，但不自动迁移资产；默认诊断的 `skipped/unknown` 不产生该 evidence。
- Bad: agent 运行 `bash <(curl -Ls IP.Check.Place)` 或下载远程脚本解析 stdout。
- Bad: 把 `status=failure` 且 `risk_level=high` 的报告当作真实高风险 IP。
- Bad: raw JSON 直接 `append([]byte(nil), report.RawJSON...)` 入库，导致旧 agent 泄露 token 或写入超大 JSON。
- Bad: 通过出口 IP 同时匹配多台 VPS 后仍把风险 evidence 归到某一台 VPS。
- Bad: 对默认服务发起网页/HTTP 请求，或把页面可达、裸 HTTP 状态码、`unknown`/`skipped` 伪造成 `unlocked` / `blocked`；默认服务必须保持零网络请求和严格未知语义。
- Bad: VPS 详情/API/history 展示 `status=failure`、`ip_address=0.0.0.0` 的 lookup 占位报告，让用户误以为 VPS 出口 IP 是 `0.0.0.0`。
- Bad: 只有 `ipapi.is` 一行时把采集完整性展示为 100%，隐藏 optional/default source 缺口。
- Bad: 历史详情 endpoint 只返回 provider/service rows，不返回 selected report summary，导致前端无法展示历史报告上下文。
- Bad: 把 `probe_status=failure` 的 service `status=unknown` 统计成 blocked 或 unlocked。
- Bad: 用`ip_quality_reports.raw_json`或provider/service row bytes估算evidence quota，或把IP source failure当成capacity unavailable fallback。

### 6. Tests Required

- `internal/contracts/agentapi`: sync plan 和 sync request JSON round-trip，覆盖 `ip_quality_plan` 与 `ip_quality_reports` 字段。
- `internal/contracts/agentapi`: provider/service v2 字段、`coverage`、`diagnostics_json` JSON round-trip，并覆盖旧 payload 兼容。
- `internal/center/settings`: 默认开启、零值/仅 `enabled=false` 视为显式关闭、低频默认值、`stale_after_seconds` 默认值与校验、service normalization、非法频率/timeout/service；不暴露低频 TTL 配置。
- `agent/ipquality`: due 判断、state store、HTTP collector 成功/partial/failure、显式 custom JSON service status 映射、raw JSON 脱敏和合法 JSON。
- `agent/ipquality`: 默认多 provider registry、optional not_configured rows、默认七项零 I/O `skipped` 诊断、provider stage budget/取消回收、ipapi.is 嵌套 JSON 与 IPQuery `format=json` 解析、HTML/非 JSON 清洁 failure、失败 attempt 节流。
- `agent/ipquality`: custom service 的 `unlocked` / `blocked` / `partial` 成功矩阵、unknown/空对象/非法状态与 HTTP/JSON 异常失败矩阵，以及 429/403/451/404/5xx 不得形成解锁结论；coverage 非法组合与 `service_probe_revision` round-trip。
- `agent/runtime`: plan 到后台 collector 的启动/drain 行为，disabled plan 不启动采集。
- `agent/ipquality`: 立即采集绕过周期且只执行一次、ID 写入 diagnostics 且保留原诊断、周期报告不带 ID、周期采集中认领请求、Stop/重启后可重新执行、disabled plan 忽略请求、失败的立即采集保留上次成功时间。
- `internal/center/ipquality`: 立即采集请求复用、只按回传 ID 完成（排除周期报告与其他 ID）、diagnostics ID 解析与规整、过期与清理。
- `internal/center/syncing`: 待执行请求写入 plan 副本、关闭/停止采集时不下发、只有 recorded 批次完成请求，并用真实注册表覆盖“下发—suppressed 不完成—recorded 完成”。
- `internal/center/store`: 完整 settings 读取与 sync plan 对缺少 `enabled` 的已存 JSON 结论一致。
- `internal/center/http/handlers`: 立即采集 GET/POST 可用性原因、409、请求复用；agent sync 透传 plan 的 `collect_request_id`，并从原始 diagnostics 读出回传 ID。
- `agent/syncqueue`: IP 质量报告随 sync request 离线队列 round-trip。
- `internal/center/http/handlers`: agent sync 写入 IP 质量 DTO、非法报告拒绝、raw/extra/diagnostics JSON 脱敏；VPS IP quality API 返回 report/matrix/history；历史详情 endpoint 返回 selected report summary。
- `internal/center/store`: sync batch 事务内写三表，repository latest/history 查询、历史详情查询、migration view 使用正确 alias；retention 长期保留报告与脱敏 raw。
- `internal/center/store` 强制业务 PG anchor：`TestPostgresIntegrationIPQualitySyncMetadataRoundTrip`、`TestPostgresIntegrationIPQualitySyncRollbackAndReplay`、`TestPostgresIntegrationIPQualityAddressIdentity`，验证真实 runtime-role sync/readback、事务回滚/replay、Go/SQL 身份矩阵、link/fallback 及 PATCH history；`scripts/test-business-postgres.sh` 必须观察全部 RUN/PASS，任何 skip/fail 均拒绝。
- `internal/center/store/migrate`: IP 质量 read view 重建迁移必须过滤 failure、`0.0.0.0` 和非法 IP version，并保留 partial 真实 IP 报告。
- `internal/center/assetdecisions`: IP 质量缺失/过期/失败/partial/ambiguous/风险/解锁阻断 evidence 与 readback 语义，确认只统计 successful provider/probe rows。
- `web`: API client、Settings、VPS list badge、VPS detail section、完整 IP 质量页、历史详情、Asset Decisions evidence/current facts 展示。
- `internal/center/evidence` / `internal/center/store`: `ip_quality.report/v1`只通过logical snapshot统一计费；capacity tests不得查询IP质量或attachment source tables。

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

### 8. Provider outcomes and scheduling evidence

默认采集先并发执行 `ipapi.is` 与 `ipquery.io` 这两个 canonical source，并按
registry 顺序选择第一个返回合法 IP 的成功结果。只有 canonical source 成功后才
启动 `proxycheck.io`、`ip2location.io` 和 `ipwho.is`；canonical 全部失败时，
依赖来源只写 `missing_target_ip` 诊断，不发出网络请求。canonical/dependent 两个
阶段各自保留原 total budget，请求 timeout 取阶段预算与 5 秒中的较小值，结果按
registry 输入顺序归并。

默认服务阶段不再执行 HTTP 探测：provider lookup 正常完成后，诊断生成器按
normalized service 输入顺序一次性填充结果 slice；它不启动 worker、timer 或
额外网络请求。父采集 context 是硬总上限，provider worker 在取消/超时后必须回收，
任何 worker 在返回后都不得继续修改报告或其 maps。

provider 的 HTTP/JSON 成功不等于业务成功。`ipapi.is`、`ipquery.io`、
`ip2location.io` 与 `ipwho.is` 必须返回有效 IP；`proxycheck.io` 只接受
`status=ok|warning` 且必须存在 canonical IP 对应的非空对象。业务错误写
`provider_error`，协议必需值缺失或类型不正确写 `invalid_response`；普通错误摘要
使用固定短语，脱敏后的上游细节只保留在 raw/extra JSON。失败 provider 不进入
canonical 候选、preferred/fallback 或成功 coverage。

`ipapi.is` 的嵌套和匿名扁平响应共用同一事实读取路径。扁平 `asn` 形如
`AS123 Example Org` 时拆成 ASN 与组织；无法解析时只保留原始 ASN 值，不臆造
组织、国家代码或风险字段。
