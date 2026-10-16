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
  - `collection_revision=5`、`collect_ip_versions=[4]|[6]|[4,6]`（仅本次 live sync 确认能力后下发的手动请求；旧 Agent plan 省略）
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
  - `POST /api/vps/{vps_id}/ip-quality/collect`：可选 JSON `{ip_versions:[4]|[6]|[4,6]}`；无 body/省略字段默认双栈，null、空数组、重复、逆序和非法族为400。可用时202返回同结构并带 `request`；不可用时409 `{error,reason}`；异范围在途请求为409 `collection_scope_conflict`并返回当前请求。
  - `request`：`request_id`、`monitoring_instance_id`、`requested_ip_versions`、`status`（`pending` / `dispatched` / `completed` / `expired` / `failed`）、`requested_at`、`expires_at`、`dispatched_at`、`completed_at`、`report_status`、安全 `error_code`、`error_summary`
  - 请求结果 `collection_scope` 为 `single_family` / `dual_stack` / `legacy_single_family`，可带 `collection_id`、`family_results`（每行含 `ip_version`、`availability`、`report_status`、安全 `error_code`）；只投影一个采集组，不把不同组的族拼接。
  - 报告响应新增 `selected_ip_version`、`default_ip_version`、固定 [4,6] 的 `families`；每族含 availability、可空 latest_attempt 与 latest_report_summary。attempt 只公开时间、stale、status、安全 error_code、scope/id，不公开占位出口或原始异常
  - VPS list/detail records may include `ip_quality_summary`.
- Asset decision evidence kinds:
  - `ip_quality_missing`
  - `ip_quality_stale`
  - `ip_quality_risk`
  - `ip_egress_mismatch`
  - `media_unlock_blocked`

### 3. Contracts

- 默认采集使用 Go HTTP 与内嵌、固定、本地审查的 IPQuality 裁剪脚本；禁止下载执行远程 shell，包括 `bash <(curl ...)`、浮动 main、启动更新或远程资源替换。批准的本地脚本不等于批准运行上游完整交互脚本。
- 保留五个 Go-native 公开风险来源 `ipapi.is`、`ipquery.io`、`proxycheck.io`、`ip2location.io`、`ipwho.is`；geo 模块另产生真实 `maxmind`/`ipinfo` 匿名来源，不用未实现 optional 占位凑覆盖率。
- 历史已存/排队的五真实＋十三 optional 行仍接受且仅在历史诊断显示；自有密钥增强仅在 Center 运行，未配置不生成占位。
- `ipapi.is` 请求可返回 JSON 的 `https://api.ipapi.is`，查询当前出口 IP 时不拼接 `?q=self`；`https://ipapi.is/?q=self` 返回 HTML 首页，禁止作为默认采集源。`ipquery.io` 的当前出口 IP 请求必须使用 `https://api.ipquery.io/?format=json`；纯文本根路径不是有效 JSON 来源，指定 IP 查询继续使用转义后的路径。
- Agent collector 必须兼容 ipapi.is 当前嵌套 JSON 结构：top-level `ip` / `is_datacenter` / `is_proxy` / `is_vpn` / `is_tor` / `is_abuser` / `is_crawler`，以及 `asn.asn` / `asn.org` / `asn.country` / `asn.type`、`company.name` / `company.type`、`location.country_code` / `location.country` / `location.latitude` / `location.longitude`。
- Legacy/custom service unlock URL 默认必须为空；保留 `HTTPCollectorOptions.LookupURL` / `ServiceURL` 及其 legacy 分流，只有调用方显式提供 `ServiceURL` 时才请求 custom JSON service adapter；默认匿名网页探测不作为 custom adapter 的失败兜底。
- 默认 registry 仍执行七项免登录探测，历史 source 身份连续：`netflix` → `netflix_title_probe`、`chatgpt` → `openai_status_probe`、`youtube-premium` → `youtube_premium_page_probe`、`amazon-prime-video` → `prime_video_page_probe` 使用固定脚本；`disney-plus` → `disney_default_probe`、`tiktok` → `tiktok_home_probe`、`reddit` → `reddit_home_probe` 保留 Go revision4。范围仅匿名 IP／地区准入，不承诺账号登录、付费播放、实际 App 登录、DNS 解锁或完整片库；无关入口风控仅为详情诊断。
- 只有服务专属业务证据可产生 `unlocked|partial|blocked`，三者均为 `probe_status=success`；通用 HTTP 200、空 JSON、无阻断文字不能作为成功。网络错误、挑战、登录页、未知响应为 `unknown/failure`，清空 region/unlock_type。429/5xx 一律未知；普通 401/403 不是地区封锁证据。
- 服务页脚本仅静态提取可解码的 JSON，不执行 JavaScript；字符串、模板文本、注释和已识别正则字面量中的 JSON 样式文本不构成业务证据。正则识别须覆盖赋值、参数、一元/二元运算及表达式关键字边界，并保留普通除法、属性名及后缀自增/自减之后的真实 JSON。该提取器不是完整 JavaScript 解释器；未形成服务专属事实仍返回未知。
- YouTube 仅信任直接绑定到 `ytcfg.set({...})` 或 `ytInitialData = {...}`（可带 `window.`）的静态 JSON 对象；同一 script 中的无关对象、其他对象的同名属性，以及注释/字符串/正则中的标记都不能建立页面有效性或地区证据。配置对象缺地区时仍保留独立的有效页面业务拒绝判据。
- 服务结果按 normalized settings 输入顺序回收；未知服务仍为 `default_probe_registry/unknown/skipped/unsupported_service`，空输入不请求。历史 revision1 的 `unknown/skipped/unsupported_default_probe` 原样保留，不重新探测或回写。
- 显式 custom JSON service adapter 的字段优先级为 `status` → `unlock_status`，只有状态为空才使用 `unlocked` bool 映射（`true` → `unlocked`、`false` → `blocked`）；`region` / `unlock_type` 及其既有别名保持不变。状态经 trim/lower 后仅允许 `unlocked`、`blocked`、`partial`、`unknown`。
- 字段选择保留既有 `stringFromMap` 行为：若 `status` 是已存在的空字符串（包括 trim 后为空），不继续读取 `unlock_status`，而是进入 `unlocked` bool 回退；例如 `{"status":"","unlock_status":"partial","unlocked":true}` 的结论是 `unlocked`。没有有效 bool 结论时仍按无结论失败处理。
- custom JSON 的 `unlocked`、`blocked`、`partial` 均写 `probe_status=success`；保留已有业务错误说明（例如 `blocked` 的业务 `error_code` 不是探测失败）。`unknown`、空对象、缺少结论或非法状态写 `status=unknown`、`probe_status=failure`、`error_code=invalid_response`、固定摘要 `service response did not establish a business conclusion`，并清空 region/unlock_type。HTTP/JSON 读取错误（包括 HTML、空 body、JSON 解析失败和所有非 2xx）写 `probe_status=failure`、`status=unknown`，保留 `probe_failed` 语义和安全 raw。
- Lookup 成功后任一最终 default provider failure 或 service failure 使报告为 `partial`；skipped/not_configured 是能力诊断，不单独制造 partial。每个来源及服务有独立 timeout，并受总采集 context 约束；慢数据库来源不得吞掉并行服务阶段的预算。
- 两族服务共享最多7个并发槽；排队只受整轮 context 约束，取得槽位后才开始 `min(父 context 剩余时间, 10秒)` 的服务预算。排队取消不发送 HTTP，也不让已等待的服务提前耗尽自己的探测预算。
- IP 质量采集默认开启；新安装默认 86400 秒周期、60 秒 timeout、邮件启用和默认服务集合。已有显式 timeout/启停值原样保留。没有 settings 行或 JSON 缺 enabled 时按开启；省略 mail/sources 的旧客户端更新保留当前值。低频报告与脱敏原始结果长期保留，见 [数据保留合同](retention.md)。
- 立即采集仅为当前合格监控实例登记；关闭、无当前实例、未绑定或暂停返回409与原因。同实例同范围 pending/dispatched 复用，不同范围409 `collection_scope_conflict`，绝不静默升级双栈。
- 立即采集请求只保存在 center 进程内存（`ipquality.CollectRequests`），TTL 10 分钟，结束后保留 30 分钟供页面展示：completed/failed 从 `CompletedAt` 起算，expired 从逻辑到期的 `ExpiresAt` 起算，等于保留截止即清理。每次清理先把所有已到 TTL 的 active 请求转为 expired 并释放配对组；无人访问的请求也不会按 RequestedAt 提前删除，未到 TTL 的 active 不清理。防御性遇到 completed/failed 缺 CompletedAt 时使用 ExpiresAt，不伪造完成时间。center 重启即丢失，用户重新发起即可；新请求仍可替换最近一次终态请求。该机制假设单 center 进程；多副本部署前必须改为持久化协调。不新增数据库表，不复用命令白名单/pending action，也不写命令审计。
- `syncing.Service` 仅在 `ApplyBatch` 成功且 `Disposition=recorded` 时观察报告；suppressed/exact_duplicate 不推进。下发必须本次 accepted live batch、full 可写、enabled 且非 stop；修改 plan 副本，不改仓库 plan。缺 live 时 pending 至TTL。
- 新 Agent 只在实际发送含 live signal 的 sync HTTP 附单值 `X-Houfeng-IPQuality-Collection-Revision: 5`，不写 heartbeat/queue JSON。Center 只放本次内部 Batch，不长期缓存；缺失、重复、非法值视为不支持，不拒收无关数据。旧 Agent live 确认不支持时，单族请求 failed/unsupported_agent，不下发ID；双栈仍沿用旧兼容路径，旧 plan 不加新字段。旧 Center 忽略 header，queue 重放不冒充 live 能力。
- `PendingRequest` 在一次锁内取得不可变ID和族集合，所有 slice 锁内复制。完成必须同实例、request ID、collection ID、observed_at、revision、expected集合一致，已选族合法终态齐全；revision4只允许[4,6]，revision5允许[4]、[6]、[4,6]。允许分批 recorded，重复族首值保留；先应用全部行再按 observed_at/collection_id 稳定顺序选完整组冻结，禁止跨组拼接。配对识别阈值始终>=4，非法/未来配对不能降级legacy；无配对旧行可单行完成且标legacy_single_family。族必须与报告相符，占位仅failure缺族/lookup_failed。
- Agent 新手动ID绕过周期；selector仅revision5且未处理manual ID生效，周期始终双栈。in-flight 族集合精确相等才认领当前轮；单族不能认领双栈周期，必须等旧轮结束再启动，不给旧轮补ID。已产出ID内存去重，Stop/重启清空，宁可重采不漏采；去重后的ID不得缩窄周期。认领/完成/Stop保持原子生命周期。
- 过期后到达的报告照常入库，不改变请求。双族聚合忽略明确 unavailable；其余全 success 为 success，有有效结果且任一 partial/failure 为 partial，全失败或两族 unavailable 为 failure。LastStatus 使用相同规则，LastSucceededAt 仅聚合 success 时更新；失败终态不变成 IP 事实。
- IP 质量 stale 窗口由 `stale_after_seconds` 控制，默认 604800 秒。该值必须不小于 `frequency_seconds`，避免还没到下一次采集就判过期。API、Settings 页面、Go/TS 类型和迁移默认必须同步该字段。
- IP 质量采集频率独立于 host sample / probe frequency。Agent 心跳 tick 只 drain 已完成报告，不在同步路径内阻塞外部 HTTP 请求。
- Agent due 判断必须按 `LastAttemptedAt` 节流；lookup 持续失败时也只能按 `frequency_seconds` 周期重试，不能因为 `LastSucceededAt` 为空而在每个 heartbeat tick 重复采集/上报 failure。
- Agent 本地状态通过 `agent/ipquality.StateStore` 记录上次采集时间；sync queue 持久化包含 IP 质量报告的整条 `SyncRequest`。
- `status` 只允许 `success`、`partial`、`failure`。失败报告允许没有 provider/service 细节，但仍必须带合法 `ip_address`、`ip_version`、metadata 和错误摘要。
- Provider row `status` 只允许 `success`、`failure`、`skipped`、`not_configured`；`source_type` 只允许 `default`、`optional`、`custom`。Service row `probe_status` 只允许 `success`、`failure`、`skipped`、`not_configured`。
- `coverage` 保留 expected/successful/failed/skipped/not_configured provider 与 service 计数，放在折叠诊断，不作为主视图质量百分比。诊断优先使用 summary，其次 selected report；缺失不从历史行数重建成功率。
- Service coverage 只有 `probe_status=success` 且 `status` 为 `unlocked`、`blocked` 或 `partial` 才计 successful；`skipped` 与 `not_configured` 各计原分类，其余组合（包括空 probe_status、`unknown/success`、`unlocked/failure`）计 failed。该谓词只约束新报告计数，历史入库/读取的兼容事实不回写。
- 每轮只返回所选族，顺序规范化，共享 observed_at 与随机 collection_id。无revision5协商使用collection_revision=4、expected_ip_versions=[4,6]；协商revision5时metadata为5，手动轮使用选集，周期仍[4,6]。source_version=v2；报告service_probe_revision=5只标混合算法，product revision仍1；每行证据优先。未选族零请求、零占位、不更新attempt、不清除旧结果。legacy/custom保留原单元素语义，历史不回写。
- family_availability 仅 available/unavailable/unknown：UP非loopback接口有该族global-unicast（含私网/ULA）或强制出口发现成功即available；成功枚举无可用地址才unavailable；枚举/发现均失败为unknown，仍尝试发现。忽略unspecified/loopback/multicast/link-local。缺族占位0.0.0.0/::、failure/address_family_unavailable，不发该族外部请求；available/unknown发现失败为lookup_failed，绝不当缺族。
- 配对终态的真实地址在Go协调器与0078 SQL视图使用相同global-unicast边界：私网/ULA仍合法；排除unspecified、loopback、link-local、multicast、IPv4全1广播及IPv4-mapped IPv6。unspecified只允许规范的缺族或lookup_failed占位，不把非法真实地址投影成available。
- 服务版本取每行 `extra_json.probe_revision`，再兼容旧报告diagnostics。脚本行revision5无legacy typed checks；保留三服务revision4与旧revision1/2/3/4原始历史解释，不能用新聚合覆盖旧报告。
- HTTP lookup 返回 HTML、非 JSON、空 body 或 JSON 解析失败时，Agent 必须生成短诊断 failure（如 `non_json_response`），不得把 HTML 原文写入 `error_summary` 或 raw envelope。
- Center 必须在 sync 事务内保存完整 IP 质量报告、provider/service 扩展元数据、coverage 与 diagnostics，与心跳、样本、观测和 batch receipt 一起提交或回滚；不得另开 SaveReports 事务。先通过 sync token/fingerprint 验证，fingerprint 不匹配的报告不得入库。相同 service 不同 source 是不同事实，相同 `(service, source)` 重复须失败并回滚整批。exact replay 不增事实、不重复完成立即采集请求。
- 两个写入入口共享完整 writer 与 sanitizer，但保留入口语义：缺 raw 的 sync 写 JSON null，SaveReports 写 SQL NULL；非空非法 raw 经 sanitizer 得到 SQL NULL。显式 received_at 原样保留，零值分别使用批次 received_at 与 SaveReports 的当前 UTC 时间；缺 coverage/diagnostics/extra 保持 SQL NULL。
- Raw JSON、provider `extra_json`、service `extra_json` 和 report `diagnostics_json` 必须通过 `ipquality.SanitizeRawJSON` / extra JSON sanitizer 兜底处理：递归替换 token/key/authorization/cookie/password/secret 类字段为 `[redacted]`。Agent 与 Center 各自按既有 trim、大小写和连字符归一化规则识别 `_secret` 后缀（如 client_secret、api_secret、app_secret、shared_secret），不误删 ordinary、secretary、secret_count 等普通字段。保留对应最大字节数及两端既有 truncation 语义；超限时存合法 JSON truncation marker，不做字节截断。此规则只约束新输入，不自动扫描、回写旧报告或宣称覆盖任意格式凭据。
- VPS 归属优先使用 active `vps_monitoring_instance_links`；有 active link 时即使没有有效报告也禁止地址 fallback。没有 active link 时，当前 `ipv4`/`ipv6` 与报告出口 IP 按解析后的 host address 身份比较：IPv6 展开/压缩/大小写等价，IPv4-mapped IPv6 只与同一 mapped 地址等价，不与普通 IPv4 合并。同一报告匹配多个 VPS 时标记 `ambiguous=true`；unlink 后恢复 fallback。SQL view 与 Overview 使用相同规则，不改原始存储文本或历史报告事实。
- 地址 fallback 的报告身份与无 active link 资产身份分别在已过滤的 MATERIALIZED CTE 中计算；JOIN 只比较已解析值，避免对每个报告/资产组合重复执行 parser。assigned view 与 Overview 同步这一计算边界，不以新增索引或历史回填替代。
- Overview 窄 summary 使用同一 family-state view 的默认族报告，先选定再检查归属歧义；歧义返回 missing，不偷换更旧报告。SELECT/Scan、IPQualitySource 与 SummaryCell 传递 ip_version 和 product_revision/product_summary；revision1 正向风险在 partial 时仍保留，详情与 Overview 一致。仅 IP-quality cell 有合法族时显示“IPv4/IPv6 · 默认族报告”；disabled/missing 不伪造族，保持 disabled 不读事实与单查询边界。
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
- revision0 保持原失败/partial/ambiguous 缺口边界。revision1 归属有效且非 ambiguous 的 success/partial 报告，其明确风险 flags、IP 高风险供应商标签或 blacklist listed 均形成风险证据；服务级 blocked 形成受阻证据。邮件超时或 Center pending/failed 不清除 Agent 已知事实，未知不当安全，provider failure 不参与风险。
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
| 默认七项服务集合 | 按输入顺序回收；报告service revision5，四脚本行revision5、其余三行revision4；coverage如实，历史不回写 |
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
| 已有进行中的立即采集请求 | 同范围复用；异范围409 collection_scope_conflict并返回当前request |
| 请求 10 分钟内未收到回传 | 状态变为 `expired`，不再下发；迟到报告照常入库 |
| Agent 重复收到同一 `collect_request_id` | 只采集一次 |
| 周期采集进行中收到立即采集请求 | 仅精确同范围可认领；不同范围等旧轮结束后启动所选范围 |
| 立即采集中途或报告取走前被 `Stop`，或 agent 重启 | 请求不再算已处理，再次下发时重新采集 |
| 请求下发后实例被暂停，报告随 suppressed 批次到达 | 报告未入库，请求不完成，直到过期 |
| 请求下发后到达的周期报告或离线补传（未带回该请求 ID） | 不完成请求 |
| 同范围被认领的周期采集在请求前已开始 | 同组合法终态带回ID可完成；不同范围不得补ID |
| diagnostics 中回传的 ID 超长、非字符串或含非法字符 | 忽略该 ID，报告照常入库 |
| Settings `enabled=false` 且 latest summary 查询超时/失败 | Overview IP Quality 仍为 `not_configured` + `SectionReady`；不得把 `ip_quality` 写入 `JudgementSourcesUnavailable`，也不得发出 `source.unavailable.v1` 或抬升 overall status。历史注释只能 best-effort |

### 5. Good/Base/Bad Cases

- Good: operator 在 Settings 开启 IP 质量，agent 根据 plan 后台采集，sync 上报后 center 在一个事务里保存主报告、provider 矩阵和 service unlock 矩阵，VPS 详情显示最新报告，资产决策显示风险/缺口 evidence。
- Good: 多源成功/失败混合时，成功来源的风险和位置事实仍可见；失败来源短状态与诊断保留，不用覆盖率或完整性主标题遮蔽已知事实。
- Good: 默认七项服务实际执行免登录固定协议；已验证可用、部分可用及明确受限均作为成功判定，未识别页面如实未知，不把无法探测当作受阻。
- Good: 用户从历史列表打开旧 report，API 返回该 report 的 summary、provider rows、service rows、coverage 和 diagnostics。
- Good: 新接入 agent 的 VPS 在 IP 质量页点击“立即采集”，下一次 sync 收到带 `collect_request_id` 的 plan，agent 不等 86400 秒周期立即采集，回传后页面自动刷新到新报告。
- Base: IP 质量关闭时 plan 仍可下发 `enabled=false`，agent 不启动外部采集，Overview 判断为 `not_configured` 而不是 missing/risk。关闭后的历史 summary 查询失败不能改变当前健康判断。
- Base: 成功服务的明确 blocked 可产生 `media_unlock_blocked`；revision1 partial 仍保留已知服务/风险事实，歧义不归属资产，不自动迁移。无 product 的旧报告保持原缺口规则；skipped/unknown 不产生该 evidence。
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
- `agent/ipquality`: 实际嵌入Bash模块与受控HTTP边界回归，四项新真值、保留三服务、Prime完整8MiB限额、固定family/redirect、依赖缺失、取消/后代回收、stdout/stderr限额及恶意jq字符串；collection4/5与混合service5/4、旧plan/queue兼容、SMTP双族边界。
- `agent/runtime`: plan 到后台 collector 的启动/drain 行为，disabled plan 不启动采集。
- `agent/ipquality`: 立即采集绕过周期且只执行一次、ID 写入 diagnostics 且保留原诊断、周期报告不带 ID、周期采集中认领请求、Stop/重启后可重新执行、disabled plan 忽略请求、失败的立即采集保留上次成功时间。
- `internal/center/ipquality`: 立即采集请求复用、只按回传 ID 完成（排除周期报告与其他 ID）、diagnostics ID 解析与规整、过期与清理；覆盖 t9 完成后 t31 仍可读/t39 删除、无人访问请求 t35 首读 expired/t40 删除、失败与交错实例、TTL/终态保留截止前/等于/后，迟到报告不复活终态。
- `internal/center/syncing`: 待执行请求写入 plan 副本、关闭/停止采集时不下发、只有 recorded 批次完成请求，并用真实注册表覆盖“下发—suppressed 不完成—recorded 完成”。
- `internal/center/store`: 完整 settings 读取与 sync plan 对缺少 `enabled` 的已存 JSON 结论一致。
- `internal/center/http/handlers`: 立即采集 GET/POST 可用性原因、409、请求复用；agent sync 透传 plan 的 `collect_request_id`，并从原始 diagnostics 读出回传 ID。
- `agent/syncqueue`: IP 质量报告随 sync request 离线队列 round-trip。
- `internal/center/http/handlers`: agent sync 写入 IP 质量 DTO、非法报告拒绝、raw/extra/diagnostics JSON 脱敏；VPS IP quality API 返回 report/matrix/history；历史详情 endpoint 返回 selected report summary。
- `internal/center/store`: sync batch 事务内写三表，repository latest/history 查询、历史详情查询、migration view 使用正确 alias；retention 长期保留报告与脱敏 raw。
- `internal/center/store` 强制业务 PG anchor：`TestPostgresIntegrationIPQualitySyncMetadataRoundTrip`、`TestPostgresIntegrationIPQualitySyncRollbackAndReplay`、`TestPostgresIntegrationIPQualityAddressIdentity`，验证真实 runtime-role sync/readback、事务回滚/replay、Go/SQL 身份矩阵、link/fallback 及 PATCH history；`scripts/test-business-postgres.sh` 必须观察全部 RUN/PASS，任何 skip/fail 均拒绝。
- `TestPostgresIntegrationIPQualityReadSnapshot` 同为强制业务 PG anchor：使用真实连接和首个 SELECT barrier 覆盖 A→B、空→首次报告、同时间戳 live/backfill、link 增删、地址与共享地址歧义、历史选择；旧请求返回完整旧快照，新请求返回完整新快照，不用 sleep 猜测提交交错。
- `internal/center/store/migrate`: IP 质量 read view 重建迁移必须过滤 failure、`0.0.0.0` 和非法 IP version，并保留 partial 真实 IP 报告。
- `internal/center/store/migrate`：0078/C78同列形更新family-state，独立C77 source/privilege前驱、P77及合法pending链，无新grant、不改0076/0077、不回填；真实PostgreSQL覆盖fresh/upgrade/repeat/rollback/runtime-role和Go/SQL revision4/5选集一致。
- `internal/center/store/migrate`：0079/C79 仅新增 `idx_ip_quality_source_cache_expires_at` 普通 B-tree，fragment 无新增权限；独立 C78 前驱、P78 及所有既有 suffix 追加 0079。真实 PostgreSQL 必须覆盖 fresh、独立 C78 genesis/历史链升级、exact repeat、DDL 后与 manifest 写后回滚、runtime reject-before/admit-after、ACL drift，证明索引定义及业务/settings/cache/历史 manifest 字节保持；不改旧迁移或扩大运行权限。
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

默认每轮取一次本地接口快照、一个总context和collection ID，仅所选族使用固定tcp4/tcp6。canonical self请求先全局并发完成或到deadline，dependent不得抢占canonical槽位。预算总timeout三分之一、单请求最多5秒；地址必须合法非unspecified且族一致。

每族按ipapi.is→IPQuery确定target；dependent显式查询该target，只有另一成功canonical地址不同才重查一次，整轮不隐式重试。canonical发现、dependent及canonical重查均强制所选族tcp4/tcp6，并校验返回地址族；dependent及重查结果还必须匹配target。脚本的所有补充请求同样强制所选族，禁止普通tcp跨族拨号。跨族不比较出口；同族egress_ambiguous禁止脚本，不能猜归属。默认Proxy=nil，不跨族fallback。

target确定后dependent、geo、服务并行；共享provider4槽、service7槽，等待受父context约束。dependent min(total/3,5s)，服务取得槽后min(parent剩余,10s)，总timeout仍默认60秒。全部worker回收后按选集组装；deadline产生选集完整终态，Stop丢弃整轮。manager一次mutex写同一ID并原子append，runtime/queue保留整批。

### 9. 数据库字段与风险边界

默认 summary 按 provider 名解析，不按 payload 形状猜类型。地理 `(code,name,lat,lon)`、网络 `(ASN,organization)`、注册地 `(code,name)` 各为独立来源组；按 ipapi.is、IPQuery、ipwho.is、ProxyCheck、IP2Location 顺序选首个有有效值的组，不跨来源补半组，不从使用地推断注册地。固定 IP/version 不被 summary helper 改写。地区 code 只接受两个 ASCII 字母并大写，公司名不冒充公司类型。

- ipapi.is 兼容嵌套及官方匿名 flat company/asn/city/region/country/lat/lon/timezone；匿名套餐不提供的风险项为 unsupported，预期字段缺失为 unknown。注册网络国家只读明确注册字段，不从 asn.country 回填。
- IPQuery 网络取 isp.asn/isp.org，risk_score=0 保留 `"0"`；datacenter=false 不推出 residential，无 abuse/crawler 不推出 false。
- ipwho.is 网络取 connection.asn/org（org 空时 isp），不臆测 connection.type；security 仅在响应实际存在时读取 proxy/vpn/tor/hosting，不增加商业参数。
- IP2Location 读取已有基础字段及实际出现的 usage_type/as_info.as_usage_type、fraud_score、proxy.is_vpn/is_tor/is_data_center/is_web_crawler；明确顶层 bool 优先。is_spammer 不映射 IsAbuser，不新增收费参数。
- ProxyCheck 固定 `https://proxycheck.io/v3/{IP}?ver=24-June-2026`，无 v2 fallback。要求 status=ok|warning、version 精确匹配和 target 下 network/location/detections 均为 object。network.type/asn/organisation（空则 provider）、location.country_code/country_name/latitude/longitude、detections.proxy/vpn/tor/hosting/scraper/risk 各按语义读取。可选字段 null 保持未知；检测非 bool、非数值/越界 risk 为 invalid_response。compromised/anonymous/confidence/attack_history 仅存 extra，不冒充 abuse；extra 记录 api_version。

风险数字必须完整有限且位于供应商真实范围。IPQuery、ProxyCheck、IP2Location 与 AbuseIPDB 原分为0–100；ipapi company/asn abuser_score 是0–1 ratio，分别保留 company_network/asn scope 和真实文字等级。不存在通用40/80“参考分档”，新 carrier RiskLevel 留空；旧评级只作历史读取。零分保留，缺失不补0，不跨源平均。

各来源保留自己的坐标、位置、网络与公司资料，字段状态与原值独立；零经纬度有效，单一坐标不拼另一来源补齐。hosting/ISP不推导家宽，不构成负面风险。只有明确 residential/fixed-line residential 才标家宽。

### 10. 匿名服务协议与安全证据

固定英文请求头与UA，不带上游跟踪/会话cookie；Prime解压后最多8MiB，其他服务/geo为2MiB，读取上限+1确认完整性，超限整份失败，不解析截断前缀。Jar=nil，HTTPS、有限精确host重定向、不转发认证、不访问本机/私网；Disney POST仍不跟跳。响应正文只在私有worker文件中短存，不进报告/argv/日志。

HTML 用 tokenizer 提取结构/可见正文（排除 script/style），内嵌 JSON 解码转义但不执行脚本。挑战 `cf-chl-`、challenge-platform、g-recaptcha、hcaptcha、Verify you are human、Just a moment... 优先于 HTML 阳性；登录路径、consent 及未知形状保持未知，不因任意脚本出现 captcha 单词判挑战。region 只接受两个 ASCII 字母。

|服务 / 检测范围|固定协议与可核验结论|
|---|---|
|Netflix / catalog_sample、originals_only|两个固定非自制81280792/70143836先判有效身份与挑战；明确国家拒绝优先于Android meta。任一样本完整正向→unlocked/catalog_sample；两者均明确拒绝才查原作80057281：正向→partial/originals_only（仅自制剧），明确拒绝→blocked，未知→unknown。不以拒绝文字缺失判成功，不声称付费播放。|
|ChatGPT / anonymous_region_access|有效2xx cookie JSON且cookie_consent_required为bool才形成已识别的正向；无明确地区拒绝→unlocked，正向与地区拒绝冲突→partial，仅明确拒绝→blocked，皆无→unknown。ios根页只提供明确地区拒绝或诊断：尚无核实的正向页面结构，HTTP200、ChatGPT/OpenAI品牌、VPN帮助及维护页均不能解锁。type:dc/普通403仅端点风控，不是unsupported_country或App实测。无favicon/trace回退。|
|YouTube Premium / premium|仅可信绑定ytInitialData/ytcfg业务对象，读取已核实renderer报价/ad-free子树；排除挑战/consent，明确地区拒绝优先。无关script、注释、任意字符串不作阳性；country仅服务证据存在时保存。|
|Prime Video / home|完整有效Prime页面及currentTerritory作为匿名地区准入证据，明确业务拒绝优先；不强求不存在的isServiceRestricted字段。缺业务数据、429/5xx、挑战保持unknown。|
|Disney+ / supported_location|disney.api.edge.bamgrid.com 上 devices → token → graph/v1/device/graphql 三步匿名 browser client 交换，共享 deadline，前步失败即停。仅 sdk.session.location.countryCode 合法且 inSupportedLocation bool 才支持/受限；forbidden-location 是明确受限，缺 token、GraphQL errors 或普通 401/403 未知，无 JP 例外。公开 browser 标识不是用户 key，本轮 assertion/refresh token/sessionId 不持久化。|
|TikTok / web_region|GET www.tiktok.com/，仅可见 Please wait... 时在同 deadline GET /explore 一次；TikTok title 加 script JSON region 才可用，带页面标识的明确 not available in your country/region 正文才受限。|
|Reddit / anonymous_web|GET www.reddit.com/svc/shreddit/reddit-chat，2xx HTML shreddit 组件和两字母 country 才可用；可见 blocked by network security 仅表示匿名入口受拒。裸403/登录/挑战未知，IPv6 不借 IPv4 或第三方 DNS 兜底。|

允许 host：Netflix netflix.com/www.netflix.com；ChatGPT api.openai.com/ios.chat.openai.com；YouTube www.youtube.com/consent.youtube.com/consent.google.com；Prime www.primevideo.com/primevideo.com；Disney disney.api.edge.bamgrid.com；TikTok www.tiktok.com/tiktok.com；Reddit www.reddit.com/reddit.com。其他跳转 unexpected_redirect。

四脚本行extra为 `probe_revision=5`、`evidence_kind=ipquality_script`、固定upstream_commit/fork_revision/schema_revision、rule和有限endpoint诊断，`Checks=nil`；三个保留Go行继续revision4/anonymous_service_probe与typed checks。端点仅固定标签、HTTP整数状态、安全error_code/outcome，不存HTML、cookie/token/session或原始异常。报告级service5不替代每行版本，旧revision4 ChatGPT/Netflix聚合仅用于旧行。

HTTP 错误通过 sourceHTTPError 与标准库 typed errors 分类，不匹配 error.String：dns_failed、connection_refused、network_unreachable、tls_failed、timeout、request_failed。非 JSON 与非2xx独立诊断，保留安全 HTTP 状态；五个真实 provider 独立失败，不添加未实现来源。

### 11. Product revision 1

- collection revision4双栈/revision5选集、product revision1结构、service revision5混合算法独立。Center plan显式product_revision=1；Agent仅精确支持时发product/provider details/service checks（脚本行仍nil）。缺失/0/未知product版省略新结构并停邮件，严格解码和400丢弃政策不变；旧carrier接受。
- Agent `product` 固定为 `{revision,ptr_names,mail}`。PTR 与邮件属于所选 Agent
  时点报告，不查询当前值补历史。provider `details` 保留查询 IP、时间、方法、
  API 层级、各自位置/ASN/公司资料、nullable 布尔/坐标、typed scores 与 field_states。
  `checks` 保留服务独立子结果，状态仅 `available|restricted|unknown`。
- 分数的 `scope=ip|company_network|asn` 与
  `direction=higher_risk|higher_trust|unspecified` 必须显式保存；数值、上下限为
  nullable。不跨源平均，不把公司/ASN 滥用比例当作单 IP 欺诈概率。
  字段状态区分 `known|unsupported|unknown|error`；明确 false 是已知阴性。
- 新结构使用注册表和独立业务校验：provider≤16、每源 score≤8、mail≤12、
  每服务 checks≤4，普通字符串≤256 bytes、错误码≤64 bytes；坐标成对且合法、
  queried_ip 与报告一致。Agent product≤64KiB、details≤8KiB、checks≤16KiB；
  拒绝非法或超限对象，不使用 raw 截断 marker 替代业务事实。上述新限制不收窄旧 carrier。
- C77 增加 reports `product_json/product_summary_json`、providers `details_json`、
  services `checks_json`，旧行 NULL 且不回填。每份 revision1（包括 backfilled）
  在原写事务持久化 Agent-only summary；仅合法真实出口且非回填报告同事务创建
  `ip_quality_intelligence` pending job。重放不重复创建，回滚不留孤儿。
- 完整 API 的 `product` 包含 revision/report_id/IP/family/state/Agent时间/完成时间、
  PTR、全部来源样本、邮件、黑名单、注册网络、ASN 和 summary；数组总为 `[]`，
  未终结 completed_at 为 null。所有事实来自所选报告及 report_id 绑定的一次性快照。
- `BuildProductView` 是唯一纯业务投影。同 vendor 优先认证成功行；认证失败不能
  覆盖匿名成功事实，原始两行仍可展开。失败来源不参与摘要；不同方法不增加独立来源数。
  `risk_flags` 仅明确 proxy/vpn/tor/abuse/robot true，server/ISP 不是风险；
  `high_risk_sources` 仅IP scope且明确High/Very High/Critical（大小写无关），不对数值分档；ASN/company scope不升级单IP风险。
  blacklist checked 只计 listed/not_listed。service 每服务恰计一组，不按 endpoint 翻倍。
- 服务聚合按行证据版本：revision5脚本直接消费本轮规范化结论，不让legacy endpoint聚合覆盖；revision4 ChatGPT正向加未知为partial、Netflix双样片拒绝为partial/sample_titles_unavailable，仅解释历史旧行。
- 同服务多source按成功业务结论确定性聚合，不能first-row-wins：失败/未知不盖掉已有结论；仅可用为unlocked，仅受限为blocked，任一partial或可用/受限冲突为partial，皆无为unknown。每服务计数一次；任一成功受限source都保留在排序后的restricted_services，包括冲突partial，不因source顺序或Center最终快照丢失限制证据。原始source行仍独立保存。
- `summary.ip_type` 为只读、报告本地一次性快照：`status=native|broadcast|unknown|conflict`、`basis=registered_country|abuse_contact_country`、provider/method、country_code/comparison_country_code和可选detector版本。`country_code`始终是使用国；`comparison_country_code`是basis指定的真实注册国或滥用联系国家，前端不得颠倒。优先同源完整真实registered-country对（认证MaxMind优于匿名），没有才用明确标注IPinfo abuse-contact后备；相等native、不同broadcast、缺项unknown、同优先级冲突conflict。低优先级分歧留详情，禁止跨源/跨族/历史拼对。
- “原生/广播”只是IPQuality地理国家比较，不是物理anycast、住宅/机房或风险判断。主注册地只读真实注册国；abuse/ASN/RIR不能冒充。旧geo_status维持历史注册网络与至少两vendor使用国比较，不再作新IP类型；缺ip_type旧报告不自动重分类。
- `Summary` 增加 product_revision/intelligence_state/product_summary。revision1
  即使 pending/running/failed/skipped 或无 job，仍返回 Agent-only summary；
  不回退旧评级。只有 complete/partial 快照读取同事务提交的增强 summary；failed
  仍展示已保存的逐源失败诊断，但不替换 Agent-only summary。清扫失败或 skipped 无增强事实。
  full list/detail/families/history 在同一 RR 快照中批量加载，历史 ID 不跳最新。
- 密钥只存内部 `IPQualitySettings.sources`，HTTP response 仅返回来源
  configured/masked_summary。update omission 保留、空字符串清除、null 拒绝；
  所有更新在 `MutateSettings` 原始 current 锁内合并。旧客户端省略 sources/mail
  不丢密钥或关闭邮件；新安装 timeout60、mail enabled，显式旧配置不覆盖。
- Records `ip-quality-evidence/v1` / `IPQualityReportV1Key` 仍是不可变的
  **Agent 采集，不含 Center 增强**。不把异步快照加入旧 schema、SourceRevision
  或 ReceivedAt 水位，不改已捕获 snapshot/hash，邮件不改变 v1 provider 事实合格性。

### 12. Agent 邮件连通性

固定 gmail/gmail.com、outlook/outlook.com、yahoo/yahoo.com、apple/me.com、
mailru/mail.ru、aol/aol.com、gmx/gmx.com、mailcom/mail.com、163/163.com、
sohu/sohu.com、sina/sina.com、qq/qq.com。邮件和服务/数据库并行；
双族共享6个 mail slots，取得 slot 后每邮局5秒（DNS/dial/greeting），等待受总 deadline约束。
MX按preference/hostname排序去重，最多检查20候选；先找目标族公网地址再计最多2次TCP连接。
首MX无AAAA不阻止下一MX；只有TCP失败才换候选，读到greeting或greeting超时后不再拨。
Null MX为no_mx；真实无MX可按SMTP标准回退域名A/AAAA，DNS失败不是无MX。
只有完整查完候选且均无目标族地址才no_address；截断、DNS失败、私有目标分别unknown，
预算耗尽timeout，不借另一地址族。

固定目标端口25，首个完整SMTP多行greeting最多4KiB/20行；最终220 reachable，
明确4xx/5xx refused。只发送QUIT，不EHLO、不发信、不登录、不测试中继，
不绑定本地25端口。不保存banner，不以可达宣称邮件送达或信誉，不以失败推断本地端口封锁。
PTR仅系统LookupAddr，最多4名/1KiB；失败不改变风险。业务拒绝是完成结论；
网络失败/超时使采集partial，但邮件从不改变IP风险分。


### 13. Center 资料查询与一次性快照

查询不进入 heartbeat/sync 事务，不在报告 GET 中发外网请求，不下发凭据给 Agent。
固定来源为RIPE RIS、五个RIR RDAP、SpamCop、DroneBL、Spamhaus DROP及可配置ipapi.is、AbuseIPDB、官方GeoLite。无Key不加入增强预期数、不生成占位；Agent固定geo后备不要求Key。

- RIS 使用 `stat.ripe.net/data/network-info/data.json` 的 prefix/asns；主 ASN 必须匹配
  当前 Agent ASN 与 origins，或 origins 唯一。MOAS 无唯一匹配时只保留 origins/route。
  overview/routing-status 提取 holder、announced、announced_space 与 query_time；
  地址规模使用 decimal string，不损失 JS 精度。命中 IP cache 后仍按当前报告重新选主 ASN，
  换主 ASN 不保留旧组织/规模；只获取 ASN metadata 不能延长继承的 prefix/origins 缓存寿命。
- RDAP 使用 `data.iana.org/rdap/{ipv4,ipv6,asn}.json` bootstrap，IP 最长前缀、ASN 区间匹配。
  只允许 rdap.arin.net、rdap.db.ripe.net、rdap.apnic.net、rdap.lacnic.net、
  rdap.afrinic.net；不遍历任意 entity links，不采集个人联系人。network country 只读网络
  对象 country，registrant country 只读 registrant 角色的结构化 jCard，不从地址文本或 ASN 推断。
- ipapi.is 使用固定 `POST https://api.ipapi.is/`，请求为目标 IP 或所选 ASN 与自有 Key；
  共用纯 parser，不导入 Agent runtime。AbuseIPDB 只调用
  `GET https://api.abuseipdb.com/api/v2/check`，目标 IP 与 maxAgeInDays=90，Key 在头中，
  不带 verbose，不调用举报、清除等写接口。响应只持久化白名单事实，不保存未经筛选的回显。
- GeoLite配对 `maxmind_account_id` / `maxmind_license_key`：首次齐全、字段省略保留、null拒绝、明确清除整对；safe DTO仅maxmind configured/masked_summary，绝不回显原值。固定 `GET https://geolite.info/geoip/v2.1/city/{canonical-IP}`、TLS Basic认证，禁止跨源转发认证；不请求me。traits.ip_address必须匹配，接受vendor +json，解析真实country/registered_country/city，不制造风险。两凭据作无歧义fingerprint，任一变化换cache namespace，沿用1小时cache/revocation。401/403/402/429/404及非法JSON为安全失败，不覆盖匿名成功。
- 认证成功优先显示，失败则保存 metadata-only 的失败样本和安全错误码，保留匿名成功事实。
  请求 headers/body、Key、cache fingerprint 不进入报告、Agent plan 或日志。配置清除/替换
  不修改已终结历史；要更新事实必须采集新报告。
- HTTPS 固定 host，禁用环境代理，DNS 与实际拨号均拒绝私网/loopback/link-local/metadata，
  固定解析后拨号以防 DNS 重绑定。最多三次受限重定向，跨 host 不携带认证。单请求8秒、
  单报告60秒、最多4个并发请求；源失败不取消其他源。403/429 停本轮，合法 Retry-After
  约束后续调用，不轮换出口或隐式重试绕过限制。普通响应上限2MiB，DROP上限8MiB。
- cache 以 source/resource/credential fingerprint 隔离，仅缓存验证后的成功事实；
  bootstrap/RDAP 24小时、RIS 8小时、DROP 1小时、认证 IP/ASN 1小时。过期不能当本轮结果，
  Key 变化立即换 namespace，清除后不读取旧认证 cache。quota 是独立限流标记，不当成功数据。
- `ClaimIPQualityIntelligence` 在原事务和 settings 锁内清理缓存：以同一语句的 `statement_timestamp()` 为基准，仅删除 `expires_at < T - interval '24 hours'` 且 `blocked_until is null or blocked_until <= T` 的行。过期不足或等于 24 小时宽限仍保留，活跃 quota 不提前解除；普通缓存与各凭据 scope 的 quota 使用同一保护条件。0079 为 expires_at 增加普通 B-tree 索引，不用 current-time 部分索引，不增加 janitor、批处理机制或改变 worker 调度、TTL/upsert/load 路径。

`ip_quality_intelligence` 仅为合法真实出口、non-backfilled revision1 报告同事务入队；
deadline=received_at+10分钟。状态为 pending/running/complete/partial/failed/skipped；
资料状态不是风险状态。Worker 串行领取 job，网络在事务外；DB-time + SKIP LOCKED，
lease=min(now+120秒,deadline)，owner/generation 隔离旧执行者，job context 取60秒与剩余
deadline较小者。最多三次领取用于崩溃恢复，不是来源 HTTP 重试。

清扫只终结 deadline 已过，或达到三次且不再持有有效 running lease 的 job；第三次活跃
lease 不可被另一个 worker 清扫。领取/清扫批次有界。finalize 必须仍匹配 owner/generation，
且 DB now 同时早于 lease 与 deadline；成功终态不可重开，过期 owner 不能写 snapshot 或 cache。
父进程取消不把未完成工作提交成成功；生命周期/配置权威复核与 snapshot、summary、cache 同事务。

网络前检查 enabled、active、绑定、未暂停；finalize 按 settings → report/关联资产 → job 的锁序
重新检查。只有确认 disabled 或生命周期不再合格才 skipped/lifecycle_changed 并清空增强数据。
资格查询错误、预算到期不是撤销证据：保留已取得的事实为 partial/failed，不伪称生命周期改变。
finalize 重新比较当前凭据指纹，撤销先提交则该来源事实/cache 均不保存，并记录 settings_changed；
认证 ASN 撤销恢复公开 ASN 快照，不误删公开来源资料。终态提交先于清除时历史保持不可变。

### 14. DNSBL 与 DROP 事实边界

- SpamCop `bl.spamcop.net` 只实现 IPv4 A 反转，业务答案仅127.0.0.2为 listed；
  DroneBL `dnsbl.dronebl.org` 保留公开 class 2、3、5–18、255 和原 code。未知答案为 unknown。
- 系统 `/etc/resolv.conf` recursive resolver，最多前三个，绝对 FQDN，不使用 search suffix，
  不强制公共 DNS。只有 resolver 网络失败才切下一个，使用同一 deadline。miekg/dns 验证
  ID/QR/question/RCODE，TC 时同 resolver TCP 补全，拒绝无关答案。
- 每源先验证127.0.0.2正控制和127.0.0.1负控制。SpamCop 正控制127.0.0.2，
  DroneBL 正控制127.0.0.1仅是控制值，不是业务 listed class；负控制须 NXDOMAIN。
  控制缓存取10分钟与 DNS TTL 较小值。控制通过的业务 NXDOMAIN 才 not_listed；
  控制失败、SERVFAIL/REFUSED/NODATA/timeout、127.255.255.* 均 unknown。每查询2秒，
  单源含控制6秒，共享同 job 并发限制。无 resolver 如实 resolver_unavailable。
- 两个 DNSBL 未核实 IPv6 协议，IPv6 返回 unsupported，不降级 IPv4。DROP 各族使用固定
  `https://www.spamhaus.org/drop/drop_v4.json` / `drop_v6.json`，IPv6 仍有独立真实 dataset 检查。
- DROP 按 NDJSON 严格读取 cidr/sblid/rir 和末尾 metadata，要求实际数据行数等于 records、
  CIDR 合法且族匹配，去重后匹配目标；保留 timestamp/copyright/terms。无 metadata、
  截断、计数不符、非法行、下载失败或超限都 unknown，不能把残缺数据集判全未命中。
  官方数据行 `rir: null` 表示注册管理机构未知，仍保留该 CIDR/SBL 并参与匹配；验证后的缓存以空字符串保留此未知状态。
  缺少 rir、非字符串且非 null、空白字符串均为非法行；不能跳过未知 RIR 的记录来制造未命中结论。
- 主表只计实际 listed/not_listed 为 checked，unknown 不当正常；保留来源、类别、
  匹配范围、原 code、查询时间、dataset 时间与安全错误码。不默认启用 Spamhaus ZEN，
  不动态下载第三方名单，不修改防火墙、不自动封禁。

### 15. 固定检测器、无 Key 后备与分发

- 固定xykt/IPQuality提交 `2384a67c756eb35231f5982b34731e522be3653e`（v2026-09-16），fork_revision=1、schema_revision=1。`agent/ipquality/detector_assets/`内嵌detector.sh、iso3166.json、完整AGPL-3.0 LICENSE、原始upstream-ip.sh.txt、MODIFICATIONS.md与实际SHA256SUMS。不打包cookies.txt，不联网更新。
- 模块封闭为geo/netflix/youtube-premium/amazon-prime-video/chatgpt。输入仅Go已确定且无歧义的canonical IP/单族；JSON只一份≤128KiB，严格匹配module/target/family/pin/版本/枚举/长度，缺字段或"null"不变false。jq固定filter及--arg/--argjson，清除控制字符，不拼数据成程序。
- 脚本只用Bash>=4、jq及shell builtins；不需curl/dig/bc。仅/usr/bin或/bin查找，白名单PATH。安装前检查并安全报错，不自动安装新依赖；保留独立minisign引导策略。运行时缺依赖只产生detector_dependency_missing，影响四服务和geo后备，不退出监控、不回退被替代算法；五风险源、三保留服务、PTR/邮件仍独立。
- Bash --noprofile --norc、白名单环境、关闭stdin、私有0700 cwd；独立进程组，取消/超时/输出超限杀并回收后代、关管道后清目录。stderr≤64KiB且不原样进报告。不是OS沙箱，不能声称封锁任意本机代码联网。
- HTTP由同Agent二进制私有ip-quality-fetch子命令在读取配置/token前分流，仅封闭endpoint ID及合法canonical IP/family；不接受任意URL/header/shell/config path。端点仅三个Netflix title、YouTube Premium、Prime home、OpenAI cookie/iOS、Check.Place target、IPinfo widget target。强制tcp4/tcp6、禁环境代理、HTTPS/host/跳数/公网地址限制；完整body流式写worker固定独占0600文件，不把大HTML塞JSON或argv。
- geo先Check.Place MaxMind，无效/挑战/缺必要国家才IPinfo widget后备；返回IP存在须匹配，IPinfo data.ip必须匹配。实际maxmind registered国只读Country.RegisteredCountry.IsoCode；IPinfo使用国data.country，data.abuse.country仅带来源的分类依据，绝不写RegisteredCountryCode。保留失败诊断与成功后备；布尔缺失为null，Details为agent_public/anonymous。
- release source archive `houfeng-agent_<version>_source.tar.gz`来自同一干净发布HEAD完整tracked源码，含detector资产、许可证/来源/修改/哈希、Go源码/go.mod/go.sum、Makefile与发布workflow。与两个二进制同进sha256sums.txt，沿用minisign签名和上传机制。dirty本地build不构成对应源码或发布证据。
- release target构建前必须拒绝tracked工作树/index与HEAD不同、或存在非ignored untracked文件；不创建构建/发布输出。ignored scratch允许且不修改，dirty开发候选走build-agent，不允许当前树二进制与HEAD源码静默混配。
- Houfeng自有代码经权利人明确授权采用根目录LICENSE的AGPL-3.0-only；第三方组件保留各自许可证及声明，子进程调用不构成豁免。公开组合分发保留完整对应源码、构建材料及归属；修改后通过网络提供交互的版本须按AGPL第13条向用户提供实际运行版本的对应源码，不能仅链接未修改上游。本地READY仍不等于已发布或生产部署，发布/部署分别需要操作授权。
