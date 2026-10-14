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
  - `ip_quality_vps_family_state`：每个 VPS 固定 IPv4/IPv6 两行，统一默认族与最新尝试选择
- HTTP API:
  - `GET /api/vps/{vps_id}/ip-quality?ip_version=4|6`：可省略参数使用服务端默认族；空值、重复值或非法族返回 400
  - `GET /api/vps/{vps_id}/ip-quality/reports/{report_id}`：身份由历史 ID 确定，禁止 `ip_version` 参数
  - `GET /api/vps/{vps_id}/ip-quality/collect`：`enabled`、`available`、`unavailable_reason`（`disabled` / `no_monitoring_instance` / `agent_not_bound` / `monitoring_paused`）、`monitoring_instance_id`、`agent_last_sync_at`、最近一次 `request`
  - `POST /api/vps/{vps_id}/ip-quality/collect`：可用时 202 返回同结构并带 `request`；不可用时 409 `{error, reason}`
  - `request`：`request_id`、`monitoring_instance_id`、`status`（`pending` / `dispatched` / `completed` / `expired`）、`requested_at`、`expires_at`、`dispatched_at`、`completed_at`、`report_status`、`error_summary`
  - 请求结果新增 `collection_scope`（有结果时为 `dual_stack` / `legacy_single_family`）、可选 `collection_id`、`family_results`（每行含 `ip_version`、`availability`、`report_status`、安全 `error_code`）；只投影一个采集组，不把不同组的族拼接
  - 报告响应新增 `selected_ip_version`、`default_ip_version`、固定 [4,6] 的 `families`；每族含 availability、可空 latest_attempt 与 latest_report_summary。attempt 只公开时间、stale、status、安全 error_code、scope/id，不公开占位出口或原始异常
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
- Legacy/custom service unlock URL 默认必须为空；保留 `HTTPCollectorOptions.LookupURL` / `ServiceURL` 及其 legacy 分流，只有调用方显式提供 `ServiceURL` 时才请求 custom JSON service adapter；默认匿名网页探测不作为 custom adapter 的失败兜底。
- 默认 service registry 以 Go 原生固定协议执行七项免登录探测，历史 source 身份连续：`netflix` → `netflix_title_probe`、`chatgpt` → `openai_status_probe`、`youtube-premium` → `youtube_premium_page_probe`、`amazon-prime-video` → `prime_video_page_probe`、`disney-plus` → `disney_default_probe`、`tiktok` → `tiktok_home_probe`、`reddit` → `reddit_home_probe`。检测范围仅为本轮匿名入口/地区可用性，不承诺账号资格、付费播放、原生/DNS 解锁或完整片库。
- 只有服务专属业务证据可产生 `unlocked|partial|blocked`，三者均为 `probe_status=success`；通用 HTTP 200、空 JSON、无阻断文字不能作为成功。网络错误、挑战、登录页、未知响应为 `unknown/failure`，清空 region/unlock_type。429/5xx 一律未知；普通 401/403 不是地区封锁证据。
- 服务页脚本仅静态提取可解码的 JSON，不执行 JavaScript；字符串、模板文本、注释和已识别正则字面量中的 JSON 样式文本不构成业务证据。正则识别须覆盖赋值、参数、一元/二元运算及表达式关键字边界，并保留普通除法、属性名及后缀自增/自减之后的真实 JSON。该提取器不是完整 JavaScript 解释器；未形成服务专属事实仍返回未知。
- YouTube 仅信任直接绑定到 `ytcfg.set({...})` 或 `ytInitialData = {...}`（可带 `window.`）的静态 JSON 对象；同一 script 中的无关对象、其他对象的同名属性，以及注释/字符串/正则中的标记都不能建立页面有效性或地区证据。配置对象缺地区时仍保留独立的有效页面业务拒绝判据。
- 服务结果按 normalized settings 输入顺序回收；未知服务仍为 `default_probe_registry/unknown/skipped/unsupported_service`，空输入不请求。历史 revision1 的 `unknown/skipped/unsupported_default_probe` 原样保留，不重新探测或回写。
- 显式 custom JSON service adapter 的字段优先级为 `status` → `unlock_status`，只有状态为空才使用 `unlocked` bool 映射（`true` → `unlocked`、`false` → `blocked`）；`region` / `unlock_type` 及其既有别名保持不变。状态经 trim/lower 后仅允许 `unlocked`、`blocked`、`partial`、`unknown`。
- 字段选择保留既有 `stringFromMap` 行为：若 `status` 是已存在的空字符串（包括 trim 后为空），不继续读取 `unlock_status`，而是进入 `unlocked` bool 回退；例如 `{"status":"","unlock_status":"partial","unlocked":true}` 的结论是 `unlocked`。没有有效 bool 结论时仍按无结论失败处理。
- custom JSON 的 `unlocked`、`blocked`、`partial` 均写 `probe_status=success`；保留已有业务错误说明（例如 `blocked` 的业务 `error_code` 不是探测失败）。`unknown`、空对象、缺少结论或非法状态写 `status=unknown`、`probe_status=failure`、`error_code=invalid_response`、固定摘要 `service response did not establish a business conclusion`，并清空 region/unlock_type。HTTP/JSON 读取错误（包括 HTML、空 body、JSON 解析失败和所有非 2xx）写 `probe_status=failure`、`status=unknown`，保留 `probe_failed` 语义和安全 raw。
- Lookup 成功后任一最终 default provider failure 或 service failure 使报告为 `partial`；skipped/not_configured 是能力诊断，不单独制造 partial。每个来源及服务有独立 timeout，并受总采集 context 约束；慢数据库来源不得吞掉并行服务阶段的预算。
- 两族服务共享最多7个并发槽；排队只受整轮 context 约束，取得槽位后才开始 `min(父 context 剩余时间, 10秒)` 的服务预算。排队取消不发送 HTTP，也不让已等待的服务提前耗尽自己的探测预算。
- IP 质量采集默认开启；默认配置为 86400 秒周期、15 秒 timeout 和默认服务集合。没有 settings 行、JSON 缺 `enabled` 字段时（`settings.Default()`、sync plan SQL 兜底、`IPQualityEnabled` 读取）都按开启处理；已保存的显式值（包括关闭）原样保留，不做迁移覆盖。只传零值对象或 `{"enabled":false}` 时视为显式关闭，其余字段补默认值。低频报告与脱敏原始结果长期保留，不再提供 raw/history 按天自动删除配置，见 [数据保留合同](retention.md)。
- 立即采集：`POST /api/vps/{vps_id}/ip-quality/collect` 只为该 VPS 当前监控实例（未退役、未归档、所属 VPS 未归档）登记请求；采集关闭、无当前实例、agent 未绑定或监控暂停时返回 409 与原因，不登记请求。同一实例已有 `pending` / `dispatched` 请求时返回该请求，不重复触发外部查询。
- 立即采集请求只保存在 center 进程内存（`ipquality.CollectRequests`），TTL 10 分钟，结束后保留 30 分钟供页面展示；center 重启即丢失，用户重新发起即可。该机制假设单 center 进程；多副本部署前必须改为持久化协调。不新增数据库表，不复用命令白名单/pending action，也不写命令审计。
- `syncing.Service` 在 `ApplyBatch` 成功后，只在 `Disposition=recorded`（报告确已入库）时用本批报告完成进行中的请求；`suppressed`（暂停/退役等未写入）与 `exact_duplicate` 不改变请求。随后在 plan `enabled=true` 且未 `stop_collection` 时把仍待执行的请求 ID 写入 plan 副本并标记 `dispatched`；不得修改仓库返回的 plan。IP 质量关闭或实例暂停时不下发，请求保持原状直到过期。
- 请求与报告按 ID 精确关联：agent 把请求 ID 写进 `diagnostics_json.collect_request_id`，center 脱敏前使用共享 `agentapi.ParseCollectionMetadata` 解析；request ID 按原 `[A-Za-z0-9_-]{1,64}` 规整。revision4 必须同实例、同 request ID、同 collection ID、同 observed_at 的 IPv4/IPv6 两个合法终态齐全才完成；允许分批 recorded 回传，重复族保留首次值，不同组不得拼接。一次 ObserveReports 先应用全部行，再选最新 observed_at、同时间 collection_id 字典序最大的完整组并冻结；进行中按同排序投影单组，公开数组在锁内复制。非法或未来配对元数据不能降为 legacy。无配对标记旧报告可单行完成，但 scope 明确为 legacy_single_family，未验证另一族。metadata 族必须与报告一致；占位仅可用于 failure 的缺族/lookup_failed。周期/其他请求、suppressed、exact_duplicate 不推进。新元数据只放已有 JSON，不增加 sync 顶层字段。
- Agent 收到新的 `collect_request_id` 时不看周期立即采集一次；周期采集进行中收到请求时由这一轮认领并在报告中带回该 ID，不再重复采集。已产出报告的请求 ID 只在 agent 内存中去重（center 收到报告前会继续下发同一 ID）；进程重启或 `Stop` 后清空，此时若 center 仍下发同一 ID 会再采一次——宁可多采一次也不漏采。旧 agent 忽略该 plan 字段，请求到期后显示超时。
- 过期后到达的报告照常入库，不改变请求。双族聚合忽略明确 unavailable；其余全 success 为 success，有有效结果且任一 partial/failure 为 partial，全失败或两族 unavailable 为 failure。LastStatus 使用相同规则，LastSucceededAt 仅聚合 success 时更新；失败终态不变成 IP 事实。
- IP 质量 stale 窗口由 `stale_after_seconds` 控制，默认 604800 秒。该值必须不小于 `frequency_seconds`，避免还没到下一次采集就判过期。API、Settings 页面、Go/TS 类型和迁移默认必须同步该字段。
- IP 质量采集频率独立于 host sample / probe frequency。Agent 心跳 tick 只 drain 已完成报告，不在同步路径内阻塞外部 HTTP 请求。
- Agent due 判断必须按 `LastAttemptedAt` 节流；lookup 持续失败时也只能按 `frequency_seconds` 周期重试，不能因为 `LastSucceededAt` 为空而在每个 heartbeat tick 重复采集/上报 failure。
- Agent 本地状态通过 `agent/ipquality.StateStore` 记录上次采集时间；sync queue 持久化包含 IP 质量报告的整条 `SyncRequest`。
- `status` 只允许 `success`、`partial`、`failure`。失败报告允许没有 provider/service 细节，但仍必须带合法 `ip_address`、`ip_version`、metadata 和错误摘要。
- Provider row `status` 只允许 `success`、`failure`、`skipped`、`not_configured`；`source_type` 只允许 `default`、`optional`、`custom`。Service row `probe_status` 只允许 `success`、`failure`、`skipped`、`not_configured`。
- `coverage` 必须记录 expected/successful/failed/skipped/not_configured provider 与 service 计数；前端完整页优先使用 summary 的 `coverage`，其次使用 selected report 的 `coverage`。两者均缺失时显示“—”，不得从历史行数或默认 success 重建成功率、归一成 100%。
- Service coverage 只有 `probe_status=success` 且 `status` 为 `unlocked`、`blocked` 或 `partial` 才计 successful；`skipped` 与 `not_configured` 各计原分类，其余组合（包括空 probe_status、`unknown/success`、`unlocked/failure`）计 failed。该谓词只约束新报告计数，历史入库/读取的兼容事实不回写。
- 默认每轮固定返回 [IPv4, IPv6]，共享 observed_at 与 crypto/rand.Text collection_id；diagnostics 保留 source_version=v2，collection_revision=4、service_probe_revision=3、expected_ip_versions=[4,6]、ip_address_family、family_availability。共享 parser 只有精确 revision4、合法 1..64 字符 ID、[4,6]、合法族/availability 才 Valid；出现配对键或数值 revision>=4 即 Paired，未知版本不得 legacy 回退。显式 legacy/custom 仍返回单元素 slice，不附默认配对元数据。历史数据不回写。
- family_availability 仅 available/unavailable/unknown：UP 非 loopback 接口有该族 global-unicast（包括私网/ULA）或强制出口发现成功即 available；成功枚举却没有该族可用地址才 unavailable；枚举失败且发现失败为 unknown，仍尝试发现。忽略 unspecified/loopback/multicast/link-local。unavailable 使用族对应 0.0.0.0/::、failure/address_family_unavailable、安全摘要 no usable local address for this IP family，不发该族外部请求；配置来源/服务计 skipped，可选来源仍 not_configured。available/unknown 发现失败用 lookup_failed，不能冒充缺族。
- selected report 有 service rows 但无 source_version 或任何 service source 时提示来源不可核验。默认 service revision2/3 均显示免登录能力边界；revision1 保留停用说明，旧/未知 revision 保留不可靠警告；custom-only 不套默认来源提示。历史切换依据所选报告自身 diagnostics。
- HTTP lookup 返回 HTML、非 JSON、空 body 或 JSON 解析失败时，Agent 必须生成短诊断 failure（如 `non_json_response`），不得把 HTML 原文写入 `error_summary` 或 raw envelope。
- Center 必须在 sync 事务内保存完整 IP 质量报告、provider/service 扩展元数据、coverage 与 diagnostics，与心跳、样本、观测和 batch receipt 一起提交或回滚；不得另开 SaveReports 事务。先通过 sync token/fingerprint 验证，fingerprint 不匹配的报告不得入库。相同 service 不同 source 是不同事实，相同 `(service, source)` 重复须失败并回滚整批。exact replay 不增事实、不重复完成立即采集请求。
- 两个写入入口共享完整 writer 与 sanitizer，但保留入口语义：缺 raw 的 sync 写 JSON null，SaveReports 写 SQL NULL；非空非法 raw 经 sanitizer 得到 SQL NULL。显式 received_at 原样保留，零值分别使用批次 received_at 与 SaveReports 的当前 UTC 时间；缺 coverage/diagnostics/extra 保持 SQL NULL。
- Raw JSON、provider `extra_json`、service `extra_json` 和 report `diagnostics_json` 必须通过 `ipquality.SanitizeRawJSON` / extra JSON sanitizer 兜底处理：递归替换 token/key/authorization/cookie/password 类字段为 `[redacted]`，并限制到对应最大字节数内；超限时存合法 JSON truncation marker，不做字节截断。
- VPS 归属优先使用 active `vps_monitoring_instance_links`；有 active link 时即使没有有效报告也禁止地址 fallback。没有 active link 时，当前 `ipv4`/`ipv6` 与报告出口 IP 按解析后的 host address 身份比较：IPv6 展开/压缩/大小写等价，IPv4-mapped IPv6 只与同一 mapped 地址等价，不与普通 IPv4 合并。同一报告匹配多个 VPS 时标记 `ambiguous=true`；unlink 后恢复 fallback。SQL view 与 Overview 使用相同规则，不改原始存储文本或历史报告事实。
- 地址 fallback 的报告身份与无 active link 资产身份分别在已过滤的 MATERIALIZED CTE 中计算；JOIN 只比较已解析值，避免对每个报告/资产组合重复执行 parser。assigned view 与 Overview 同步这一计算边界，不以新增索引或历史回填替代。
- Overview 窄 summary 使用同一 family-state view 的默认族报告，先选定再检查归属歧义；歧义返回 missing，不偷换更旧报告。SELECT/Scan、IPQualitySource 与 SummaryCell 传递 ip_version；仅 IP-quality cell 有合法族时显示“IPv4/IPv6 · 默认族报告”，不改机器 status/detail。disabled/missing 不伪造族，保持 disabled 不读事实与单查询边界。
- Go/SQL host identity 解析均 trim Unicode White_Space，拒绝 CIDR、zone、端口、短 IPv4、前导零及越界十进制 octet；合法 unspecified 可解析，既有业务过滤不变。无效文本不参与 fallback，但不妨碍 active-link 归属。Agent 候选只在存在不同有效地址时确认冲突；出口不一致 evidence 需 success、非 ambiguous、有效报告地址和至少一个有效当前地址。
- VPS PATCH 仅改变地址表示不新增 IP history；真实地址改变仍记史。任一侧不可解析时按 trim 后原文比较，修正/删除非法旧地址也记史。独立创建 IP history 使用同一无变化判定；四个历史地址字段只去掉首尾空白、不做规范化，保留原文（存量非法文本 ` 999.1.1 ` 清空时记为 `999.1.1` → 空）。VPS create/导入拒绝非空的非法地址；PATCH 只在地址按本条无变化判定确有改变时才校验格式，并在版本前提检查之后执行，未改动（含只差首尾空白）的存量非法原文不被拒绝（见 assets 合同）。不放宽 evidence 地址族校验。
- 有效 IP 事实只包含 success/partial、真实非 unspecified IP 与匹配族。failure 及 0.0.0.0/:: 占位不得进入有效 history、矩阵、风险、evidence。最新尝试独立保留，只有当前 active link 可归属无出口失败；无 active link 仅沿用有效报告 host-address fallback，旧实例失败不污染新 link。
- 默认选择只由 family-state view 决定：排除新鲜 unavailable 后，IPv4 有有效报告/尝试/合法配置则选4，否则 IPv6 有依据选6，皆无依据选4。过期 unavailable 不是当前缺族证明；显式4/6不受自动规则限制。只有旧 IPv6 可默认6，但 IPv4 unknown 不等于单栈。
- 最新与历史详情在单个 REPEATABLE READ READ ONLY transaction 内读取 family state、所选摘要、报告、children 和每族最多30份有效 history；同时间按 live 优先、received_at、report_id 稳定排序。历史 ID 决定族及事实，families 仅导航。并发 link/地址/报告变化不能拼快照。
- 历史详情也返回所选报告自身地址族的 history，而不是空列表；更新的同族报告只能在新读快照中出现，不替换历史 ID 对应的摘要或 children。
- 显式族无报告返回本族空 facts + attempt，不404、不借另一族。可以展示同族上次有效报告，但本轮失败/时间独立标识。缺已选 assigned report 或 summary 必须失败，禁止重新选 latest；begin/query/commit 失败不返回部分结果。默认族只有失败无有效报告时公共 summary 为空，不退另一族。
- VPS Overview 在 IP 质量关闭时只做 availability 检查，立即返回 `not_configured` + `SectionReady`，不得查询 `GetLatestVPSIPQualitySummary`，也不得发出 `ip_quality_disabled_has_history`。历史注释不是当前健康判断的一部分；历史只在 `GET /api/vps/{vps_id}/ip-quality` 详情页展示。关闭路径上的 summary 超时不得把 Overview 标成 `source.unavailable.v1`。
- 资产决策只能把 IP 质量作为 evidence / scoring / readback 输入，不自动执行迁移、取消或续费动作。
- `ip_quality.report/v1`是authoritative evidence source kind；只有生成新的logical evidence snapshot时才按该snapshot的`logical_size_bytes`消耗project evidence capacity。IP质量report/provider/service表大小、raw retention、coverage、风险等级或source availability都不能成为quota counter、capacity fallback或janitor删除依据。
- evidence capacity/maintenance alert只来自evidence-owned aggregate store state。IP质量source失败/partial/stale/ambiguous仍按本合同产生缺口或复核语义，不能被改写成`capacity_unavailable`、`quota_exceeded`或janitor failure；反向也不能用capacity alert伪造IP质量风险。
- 失败、partial、ambiguous 的 IP 质量报告只能产生缺口/需复核 evidence，不能产生 `ip_quality_risk` 负面风险；provider `status != success`、service `probe_status != success`、`skipped`、`not_configured`、`unknown` 不得被计入负面风险或服务阻断。
- `is_server` / datacenter / hosting 本身不构成负面风险；只有 successful provider 的 proxy/vpn/tor/abuser/robot、高风险等级、出口不一致，或成功服务判定的阻断等信号才进入风险 evidence；failure/skipped/unknown 不产生服务阻断。Evidence adapter 接受 `partial/success` 并保存检测范围；`unknown/failure` 不泄露残留 region/unlock_type。Evidence 完整性仍使用原始全来源 coverage，不随页面成功率分母改变。

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
| Lookup 成功但任一已启用来源或服务执行失败 | Agent report `status=partial`，保留同 IP 成功 facts；失败服务为 `unknown/failure`，not_configured/skipped 不单独制造 partial |
| 显式 custom service adapter 返回 429 / 403 / 451 / 404 / 5xx，或 HTML/非 JSON/空 body | 不判定解锁；写 `probe_status=failure`、`status=unknown`，不根据裸 HTTP 状态码形成 blocked |
| 显式 custom JSON 缺少结论、状态为 unknown 或状态非法 | 写 `probe_status=failure`、`status=unknown`、`error_code=invalid_response`，清空 region/unlock_type；lookup 成功时报告为 partial |
| 默认服务未形成固定规则的业务证据 | 写 `unknown/failure` 及安全错误；裸 HTTP 状态或页面可达不等于可用/受限 |
| 默认七项服务集合 | 按输入顺序回收真实探测结果，coverage 如实分类，service_probe_revision=3；历史不回写 |
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
- Good: 默认七项服务实际执行免登录固定协议；已验证可用、部分可用及明确受限均作为成功判定，未识别页面如实未知，不把无法探测当作受阻。
- Good: 用户从历史列表打开旧 report，API 返回该 report 的 summary、provider rows、service rows、coverage 和 diagnostics。
- Good: 新接入 agent 的 VPS 在 IP 质量页点击“立即采集”，下一次 sync 收到带 `collect_request_id` 的 plan，agent 不等 86400 秒周期立即采集，回传后页面自动刷新到新报告。
- Base: IP 质量关闭时 plan 仍可下发 `enabled=false`，agent 不启动外部采集，Overview 判断为 `not_configured` 而不是 missing/risk。关闭后的历史 summary 查询失败不能改变当前健康判断。
- Base: 成功服务判定的 blocked 可以产生 `media_unlock_blocked`，但报告 partial/歧义时仍按缺口规则处理，不自动迁移资产；历史 skipped/unknown 不产生该 evidence。
- Bad: agent 运行 `bash <(curl -Ls IP.Check.Place)` 或下载远程脚本解析 stdout。
- Bad: 把 `status=failure` 且 `risk_level=high` 的报告当作真实高风险 IP。
- Bad: raw JSON 直接 `append([]byte(nil), report.RawJSON...)` 入库，导致旧 agent 泄露 token 或写入超大 JSON。
- Bad: 通过出口 IP 同时匹配多台 VPS 后仍把风险 evidence 归到某一台 VPS。
- Bad: 把页面可达、裸 HTTP 状态码、缺少阻断文字或 `unknown`/`skipped` 伪造成 `unlocked` / `blocked`，或下载执行外部脚本、引入账户 cookie 来绕过匿名探测边界。
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
- `agent/ipquality`: 双族不同出口与来源/服务独立、单栈零缺族外部请求、枚举失败 unknown、强制族 mismatch、canonical 全局 barrier、共享预算/并发上限、timeout 双终态与 Stop 整轮丢弃、lookup 失败节流；指定 IP 重查、风险来源组与 typed HTTP 错误回归。
- `agent/ipquality`: 七服务阳性/限制/partial/挑战/429/5xx、重定向/大小/固定地址族、YouTube 无 offer 地区与无地区拒绝独立、ChatGPT bool 与 endpoint 失败、collection revision4/service revision3 round-trip；旧 JSON shape 兼容。
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
- `TestPostgresIntegrationIPQualityReadSnapshot` 同为强制业务 PG anchor：使用真实连接和首个 SELECT barrier 覆盖 A→B、空→首次报告、同时间戳 live/backfill、link 增删、地址与共享地址歧义、历史选择；旧请求返回完整旧快照，新请求返回完整新快照，不用 sleep 猜测提交交错。
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

### 8. 出口身份、来源事实与调度

默认每轮取一次本地接口快照、一个总 context 和 collection ID，IPv4/IPv6 分别使用固定 tcp4/tcp6 client。两族至多四个 canonical self 请求作为一个全局阶段并发完成或到 canonical deadline，任一 dependent 不得抢占尚未完成 canonical 的槽位。canonical 预算为总 timeout 三分之一、单请求最多5秒；响应必须合法、非 unspecified 且与所请求族相同，异族是 ip_mismatch，不借另一族结果。

每族按 ipapi.is → IPQuery 选择 target；三个 dependent 显式查询该 target，另一成功 canonical 地址不同才重查一次。整轮最多12次数据库请求，不隐式重试。dependent 允许普通直连 tcp，因为目标由参数确定；成功结果仍必须与 target 精确一致，mismatch 清空 normalized facts，安全摘要固定。summary/fallback/risk 全部按族独立。

跨族不比较出口、不制造冲突；同族多出口 egress_ambiguous 时服务未知，不能猜归属。默认 client Proxy=nil，不继承环境代理；Client4/Client6 显式优先，其次已有显式 Client 测试入口；生产绝不跨族 fallback。

target 选定后 dependent 与服务并发；整轮共享 provider 上限4、service上限7，等槽也受父 context 取消。dependent min(total/3,5s)，服务 min(parent remaining,10s)。总 timeout 不翻倍。所有 goroutine 回收后按[4,6]组装；deadline 给未完成族 failure 终态，主动 Stop 丢弃整轮，不发布半轮。manager 在一个 mutex 临界区给全部行写同一认领 request ID 并原子 append；runtime/queue 保持整批 slice 持久化。

### 9. 数据库字段与风险边界

默认 summary 按 provider 名解析，不按 payload 形状猜类型。地理 `(code,name,lat,lon)`、网络 `(ASN,organization)`、注册地 `(code,name)` 各为独立来源组；按 ipapi.is、IPQuery、ipwho.is、ProxyCheck、IP2Location 顺序选首个有有效值的组，不跨来源补半组，不从使用地推断注册地。固定 IP/version 不被 summary helper 改写。地区 code 只接受两个 ASCII 字母并大写，公司名不冒充公司类型。

- ipapi.is 兼容嵌套及匿名 flat ASN/地区；匿名层没有 flags/company type 就未知。注册地读取明确 registered 字段，空 code 才考虑 asn.country/country_code。
- IPQuery 网络取 isp.asn/isp.org，risk_score=0 保留 `"0"`；datacenter=false 不推出 residential，无 abuse/crawler 不推出 false。
- ipwho.is 网络取 connection.asn/org（org 空时 isp），不臆测 connection.type；security 仅在响应实际存在时读取 proxy/vpn/tor/hosting，不增加商业参数。
- IP2Location 读取已有基础字段及实际出现的 usage_type/as_info.as_usage_type、fraud_score、proxy.is_vpn/is_tor/is_data_center/is_web_crawler；明确顶层 bool 优先。is_spammer 不映射 IsAbuser，不新增收费参数。
- ProxyCheck 固定 `https://proxycheck.io/v3/{IP}?ver=24-June-2026`，无 v2 fallback。要求 status=ok|warning、version 精确匹配和 target 下 network/location/detections 均为 object。network.type/asn/organisation（空则 provider）、location.country_code/country_name/latitude/longitude、detections.proxy/vpn/tor/hosting/scraper/risk 各按语义读取。可选字段 null 保持未知；检测非 bool、非数值/越界 risk 为 invalid_response。compromised/anonymous/confidence/attack_history 仅存 extra，不冒充 abuse；extra 记录 api_version。

风险数字必须完整有限十进制且在 0..100：`[0,40)` low、`[40,80)` medium、`[80,100]` high 是“候风参考分档”，不是供应商共同标准。支持真实小数，拒绝 NaN/Inf、负值、越界及尾随垃圾。报告 RiskLevel 取当前同 IP 成功来源已知标准等级最大值，无等级留空；各来源原分分别保留，不平均。hosting/risk33 不单独构成负面信号。

响应 extra 的 capability 只说明本次：有任一风险 flag/score 为 risk，否则 geo_only。保留十三条 optional/not_configured 与原始 expected=18 分类计数，不把未配置称为执行失败，不宣称免费取得未公开商业字段。

### 10. 匿名服务协议与安全证据

固定 UA 为 Chrome/125.0.0.0 Windows browser 字符串，Accept-Language 为 en-US,en;q=0.9。每响应解压后最多 2 MiB，超限 response_too_large，不截断后判成功。请求使用 client 浅副本、Jar=nil；仅 HTTPS、最多五次重定向、精确服务 host allowlist，Disney POST 不跟随重定向，Authorization 不跨 host。网络/读取/重定向/大小错误使用固定短摘要，不拼入正文。

HTML 用 tokenizer 提取结构/可见正文（排除 script/style），内嵌 JSON 解码转义但不执行脚本。挑战 `cf-chl-`、challenge-platform、g-recaptcha、hcaptcha、Verify you are human、Just a moment... 优先于 HTML 阳性；登录路径、consent 及未知形状保持未知，不因任意脚本出现 captcha 单词判挑战。region 只接受两个 ASCII 字母。

|服务 / 检测范围|固定协议与可核验结论|
|---|---|
|Netflix / catalog_sample、sample_titles_unavailable|依次 GET www.netflix.com/title/81280792 和 /70143836。Netflix app meta、对应 title ID Android URL、对应可见 LEGO Ninjago / Breaking Bad h1 才为有效目录；另一页也有效或明确 Netflix Oh no! 不可用页时为可用。两页均明确不可用为 partial，不声称仅自制剧。region 仅取同 JSON object 的两字母 id + countryName，冲突未知。|
|ChatGPT / web_and_app、web_only、app_only、verified_*_unknown|GET api.openai.com/compliance/cookie_requirements（固定 Origin/Referer，无账号 Authorization）和 ios.chat.openai.com/。Web 2xx JSON cookie_consent_required bool（false 有效）；App 2xx HTML 标识 ChatGPT/OpenAI 且非挑战才阳性。结构化 unsupported_country/unsupported_country_region_territory 或带服务标识的明确 VPN 拒绝完整文案才限制。双阳性 unlocked、一阳性另一受限/未知 partial、双限制 blocked；dc 403 不猜地区/VPN，不请求 favicon/trace 兜底。|
|YouTube Premium / premium_offer|有效 ytcfg/ytInitialData 页面通过 fatal/challenge/consent 排除后，明确 Premium 拒绝正文独立构成 blocked（包括有效403及无地区200拒绝页）。地区证据另要求2xx：优先 contentRegion，仅缺失时用 INNERTUBE_CONTEXT_GL；冲突/非法不 fallback。只有可信地区而无 offer 是 unknown，CN 本身不等于 blocked。unknown 顶层 Region/UnlockType 清空，安全 observed_region 单独保存页面地区。|
|Prime Video / territory_offer|GET www.primevideo.com/，title 含 Prime Video、内嵌 JSON currentTerritory 合法且 isServiceRestricted 非 true 才可用；明确 bool true 才受限，false 不受限，字段类型异常未知。|
|Disney+ / supported_location|disney.api.edge.bamgrid.com 上 devices → token → graph/v1/device/graphql 三步匿名 browser client 交换，共享 deadline，前步失败即停。仅 sdk.session.location.countryCode 合法且 inSupportedLocation bool 才支持/受限；forbidden-location 是明确受限，缺 token、GraphQL errors 或普通 401/403 未知，无 JP 例外。公开 browser 标识不是用户 key，本轮 assertion/refresh token/sessionId 不持久化。|
|TikTok / web_region|GET www.tiktok.com/，仅可见 Please wait... 时在同 deadline GET /explore 一次；TikTok title 加 script JSON region 才可用，带页面标识的明确 not available in your country/region 正文才受限。|
|Reddit / anonymous_web|GET www.reddit.com/svc/shreddit/reddit-chat，2xx HTML shreddit 组件和两字母 country 才可用；可见 blocked by network security 仅表示匿名入口受拒。裸403/登录/挑战未知，IPv6 不借 IPv4 或第三方 DNS 兜底。|

允许 host：Netflix netflix.com/www.netflix.com；ChatGPT api.openai.com/ios.chat.openai.com；YouTube www.youtube.com/consent.youtube.com/consent.google.com；Prime www.primevideo.com/primevideo.com；Disney disney.api.edge.bamgrid.com；TikTok www.tiktok.com/tiktok.com；Reddit www.reddit.com/reddit.com。其他跳转 unexpected_redirect。

Raw/Extra 使用 probe_revision=3、evidence_kind=anonymous_service_probe，保留 matched_rule/http_status/region/ip_version 等固定安全证据。可选 observed_region 仅规范国家码、region_evidence 仅 youtube_content_region/youtube_context_gl；端点只存固定标签、HTTP整数状态、安全 error_code 与 outcome=available/restricted/unknown。ChatGPT Web/App 独立诊断，cookie_consent_required true/false 都合法，不从 IP 数据库补地区；Netflix 只检测抽样目录，连接方式未检测。不存 HTML、URL query、Authorization、cookie、token/session 或原始异常。

HTTP 错误通过 sourceHTTPError 与标准库 typed errors 分类，禁止匹配 error.String：dns_failed、connection_refused、network_unreachable、tls_failed、timeout、request_failed；无细分依据保持 request_failed。getJSON 先识别非对象/解码失败 non_json_response，再识别合法 JSON 的非2xx http_status 并保留整数status；不把429/503/HTML折叠成 request_failed。固定安全摘要与既有大小/重定向/挑战边界不变。五个实际 provider 与十三个未接入来源范围不扩大。
