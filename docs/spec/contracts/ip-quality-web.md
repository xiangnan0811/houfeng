# IP 质量 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

IP quality remains a read-only report, not another asset overview. Keep report identity/time, provider and service results, and every returned historical report reachable; put collection diagnostics behind an explicit disclosure. Database risk and service availability are separate facts: never fold them into a composite score, and never treat unknown results as failures or as clean. Wide observation/provider tables have named, keyboard-focusable local scroll regions. These related workspaces use the VPS title/body density and stack title/actions and fact groups when narrow.

- **低频深度报告使用独立页面承载**：IP 质量、性能基准、路由质量这类“买完 VPS 后才通过 agent 测得”的深度报告，字段多、矩阵多、历史/诊断多，不适合塞进 VPS 详情页的一个普通 section。VPS 详情页只保留摘要结论、关键风险/缺口和“查看完整报告”入口；完整驾驶舱应使用独立 route/page 展示质量结论、provider/service 矩阵、覆盖率、历史变化和诊断。这样后续性能、路由报告可以复用同一 IA，而不会把 VPS 详情页变成所有低频事实的长表堆叠。
- **低频报告主视图必须降噪采集字段**：这类报告的 API 往往同时返回用户事实和采集诊断。主视图只能展示用户可判断的质量事实，例如风险信号、provider 证据 chip、服务解锁状态、区域、解锁类型、覆盖率和历史；`source`、`probe_status`、`default_probe`、`not_configured`、latency、长 `error_summary`、raw JSON 等内部采集字段不得进入主卡片、摘要区或表格证据列。需要排障时放入低权重诊断层或折叠详情，并在测试里断言这些内部文本不会出现在主要视图。


页面标题为 IP 质量报告，不使用“驾驶舱”。复用 SegmentedControl 提供 IPv4 / IPv6，group label 为“IP 地址族”；不新造 Tabs 或第二套风险算法。页头和空态始终保留两个可选族。

## 地址族、URL 与尝试状态

- 普通新入口无族参数时请求 API default，不跨 VPS 记忆上次 IPv6。首次响应把实际 selected_ip_version 以 replace 写入 URL；先迁移已加载响应的 requestKey，再规范 URL，不额外请求或闪空态。仅 IPv6 且 IPv4 新鲜 unavailable 时可默认 IPv6；IPv4 lookup 失败不能静默切族。
- 手动切换写 ip_version 并清除 report_id；历史链接同时带族和 report_id，但 detail API 只带 ID。历史 ID 的族优先于冲突 query，旧链接据响应补正 URL。非法/重复/空族参数显示可恢复错误，提供“查看默认报告”，不发送非法 API 请求；有效历史 ID 仍以其自身族为准。
- requestKey 包括 VPS、family/default 和历史 ID，保留取消/代次保护。切族、切 VPS、后退前进和卸载的迟到响应不得覆盖当前页面。采集完成刷新当前有效选择，历史查看不跳 latest。
- families 的 latest_attempt 与 selected report 分层：新鲜 unavailable 提示“本轮未检测到 IPvN 可用本地地址”；lookup failure 提示“IPvN 出口探测失败”；无结果 unknown 提示“IPvN 尚无采集结果”。过期 attempt 必须注明过期，不宣称当前缺族。旧有效报告可继续展示，但明确“上次有效结果”及其独立时间；不显示占位 IP 为出口。
- 缺族结论只信任服务端 `families[].availability`；`latest_attempt.error_code` 即使为 `address_family_unavailable`，也不能把服务端拒绝的非法/未来 metadata 所产生的 `unknown` 升格为当前或历史缺族证明。
- API default_ip_version 是唯一默认选择依据，Web 不另算。所有矩阵、风险、时间、历史只来自所选族报告，导航元数据不混入 facts。首次请求失败显示 PageState error；同族后台刷新失败保留原结果并提示，不冒充更新成功。

## 报告页结构

报告页与 VPS 详情页共用工作区版式：根节点为 `.page.vps-detail-workspace.ipq-page`，页面自行导入 `VPSDetailWorkspace.css`（直达该路由时 VPS 详情样式可能尚未加载），各区块使用 `.vps-detail-workspace__section`。自上而下：

1. **页头**：沿用 VPS 身份页头与报告身份 dl，出口/ASN组织/地区/时间仅为所选报告；长 IPv6 不撑开布局。身份下方是地址族选择器。诊断元数据不进入主身份。历史视图保留历史标识及“查看最新报告”族链接。
2. **采集状态条**：role=status、名为“IP 质量采集状态”，不可用原因优先，其次进行中/本页接手的结束状态。立即采集始终一轮覆盖两族，按钮旁说明“分别检测 IPv4 / IPv6”；请求中切族不重复 POST。
2a. **完成与更新不同**：collectNotice 接收当前 ipVersion、viewingHistory、reportCollectionID、reportCollectRequestID、refreshState。仅当前族 success/partial、非历史、刷新成功且展示报告 collection_id 与完成组一致时说报告已更新；legacy 则精确匹配 collect_request_id。族 failure 保留上次结果，unavailable 提示缺族，历史只提示轮次结束且仍看历史，刷新中/失败分别说明；aggregate partial 不能触发绿色更新。legacy 明示“本次仅收到单地址族报告，未验证另一地址族”。
2b. **服务可信度**：独立 role=note，selected report 自身 service revision2/3 使用免登录能力说明，旧/未知规则保留警告，不借另一报告。低权重显示，不占采集 live region。
3. **数据库风险评级**：沿用唯一风险 helper；low 且五字段不完整时大字“证据不完整”，副文“已返回来源：低风险”及 N/5。中/高/严重已发现等级仍明确展示并标不完整，不以缺口遮蔽负面。四指标为风险信号、服务可用性、已知字段一致性、已启用来源响应成功率，不算综合分。
4. **服务**：revision2/3 显示免登录探测的可用/部分可用/受限/未知。只有业务证据决定状态，失败不泄露残留 region/unlock_type。错误码映射为短中文，新增 dns_failed/connection_refused/network_unreachable/tls_failed；HTTP 整数状态从 endpoint 安全诊断读取，不从泛化 error_summary 反解析。原始异常不进主卡片。Netflix 补充“连接方式未检测”，不伪造原生/DNS；ChatGPT 无服务地区证据时地区未知，Web/App 各自 endpoint 结果可诊断。
5. **IP 数据库判断**：标题旁只列出被命中的信号（如 `VPN 1/3`，机房为中性色）；没有负面命中时，五个负面字段均有成功来源明确返回才写“已返回风险字段未命中”，否则写“风险字段不完整”，不得无条件宣称未发现代理/VPN/Tor/滥用。表格只放成功且至少给出一个判断字段的数据库，按负面信号、风险等级排序；信号列“是”用徽标、“否”用弱文本、未知用“—”；公司类型只读 `company_type`，不拿公司名冒充。来源 `extra_json.capability === "geo_only"` 时在名称下注明“该来源本次仅返回地理信息”。存在原始 `risk_score` 时脚注说明“风险分：数值越高风险越高；不同数据库不可直接比较”，原始 0 分照常显示。成功但无判断字段的数据库只计数；失败/跳过的默认来源以“未返回结果”徽标列出；未配置的可选来源只在诊断区出现。
6. **历史报告**：紧凑列表（时间、风险、出口 IP、查看），当前报告标记“当前查看”；超过 5 份时折叠并提供“显示全部 N 份”，当前报告在折叠部分时自动展开，保证每份报告可达。
7. **采集诊断**：默认折叠；来源状态分失败/跳过/未接入，十三个无 adapter 的 optional 来源 wire 仍 not_configured，但 UI 称“未接入”，不给不存在的配置入口。全部 coverage 与服务 endpoint 安全 HTTP 状态/错误/规则可追溯；未接入不算执行失败。raw/extra/diagnostics 单独折叠、文本渲染、限高可键盘滚动，不撑页面。

无有效报告时保留页头、族选择与尝试状态，空态按所选族解释而不是整页空白。首次加载失败才进入错误页；后台刷新失败保留同族旧结果。错误页不启动采集状态查询。

立即采集由 useIPQualityCollect 管理：报告加载成功后读状态，可用进行中每3秒轮询；结束/过期/不可用停止，轮询失败继续下一轮。全局单调序号及 VPS/卸载会话代次隔离迟到响应；提交中只由本 POST 结束，重复点击不重复请求。族选择不是调度维度，不重置进行中请求。完成时刷新当前族/历史身份，刷新不回可见加载态；409 以重读可用性原因为准。只读预览不提供立即采集。

不可用状态保留原因对应的操作入口：`no_monitoring_instance` / `agent_not_bound` 在有明确 VPS 上下文时进入 `/vps/{id}?workbench=monitoring`，复用现有创建或接入流程；`monitoring_paused` 只使用当前 `monitoring_instance_id` 进入实例运行控制，可附有效 `return_vps`，不从历史请求猜实例、不自动恢复监控；`disabled` 进入监控设置。缺少必要 ID 时只解释原因，不生成猜测链接。采集失败的主要文案使用安全中文说明，任意语言的原始错误均放在默认关闭的诊断详情中。


## 服务探测范围与历史可信边界

七个默认服务及其历史 source 身份保持连续：`netflix` → `netflix_title_probe`、`chatgpt` → `openai_status_probe`、`youtube-premium` → `youtube_premium_page_probe`、`amazon-prime-video` → `prime_video_page_probe`、`disney-plus` → `disney_default_probe`、`tiktok` → `tiktok_home_probe`、`reddit` → `reddit_home_probe`。agent 侧请求、判据与 revision 语义见 [IP 质量合同](./ip-quality.md)；页面只消费其中的 diagnostics/extra_json 元数据，不新增 DTO 字段。

- `effectiveUnlockStatus(unlock)` 是服务状态的唯一入口：只有 `probe_status=success` 且 `status` 为 `unlocked`、`partial` 或 `blocked` 才是已知结果；历史数据仅在 `probe_status` 缺失/null 时兼容旧语义。显式空字符串、failure、skipped、not_configured 与 unknown 均显示“未知”。统计、卡片颜色、状态词、区域与检测范围全部经由该入口，失败行残留的 blocked/region/unlock_type 不得进入卡片。
- revision3 可单独展示可信“页面地区”：helper 必须验证 selected report 的族匹配 extra.ip_version、probe_revision=3、evidence_kind=anonymous_service_probe、region_evidence 为 youtube_content_region/youtube_context_gl 以及合法两字母 observed_region。unknown 时展示“页面地区：CN；Premium 可用性未知”，不能读取残留 normalized Region；此证据不改变 effectiveUnlockStatus、coverage 或风险。
- `UnlockType` 显示为中文检测范围，不显示内部枚举：`catalog_sample`“抽样目录可访问”、`sample_titles_unavailable`“抽样片目不可用，未验证完整片库”、`web_and_app`“Web 与 App 入口”、`web_only`“仅 Web 入口可用”、`app_only`“仅 App 入口可用”、`web_verified_app_unknown`“Web 已验证，App 入口未知”、`app_verified_web_unknown`“App 已验证，Web 入口未知”、`premium_offer`“Premium 地区展示”、`territory_offer`“Prime 地区展示”、`supported_location`“地区支持检测”、`web_region`“网页地区检测”、`anonymous_web`“匿名网页入口”。表外的自定义/历史类型保留“类型 X”。
- 显式调用方选择的 custom JSON service adapter 仍可作为程序接口使用，不代表内置网站协议已验证，也不自动回退；其结论合同见 [IP 质量合同](./ip-quality.md)。
- 服务 coverage 使用报告提供的计数，不把已有行数归一成 100%；`skipped` / `not_configured` 各保留原分类。
- `defaultServiceProbeNotice(report)` 只在有 service rows 时评估，并从 `report.latest_report?.diagnostics_json` 安全读取非 null、非数组的 diagnostics 对象。`diagnostics.source_version === "v2"` 或任一 row.source 属于上述七个 source 时，报告才视为默认来源；custom-only 来源不显示默认能力警示。
  - service_probe_revision 精确为数值2或3：中性免登录能力提示；行级 probe_revision 数值2/3只改变该卡片状态词。页面地区新证据仅接受3，不能重新解释旧报告为双栈或新验证。
  - 数值 `1` 且至少一条 row 为 `skipped`/`unsupported_default_probe`：“默认服务解锁探测已停用：缺少可靠业务证据。未知不表示服务受阻，服务覆盖可能不足。”数值 `1` 且没有上述 skipped 行时不显示额外警示。
  - 缺失、字符串（如 `"1"`、`"2"`）、非法或不认识的其他数值：“此报告使用旧版或未识别的服务探测规则，解锁结果可能不可靠；历史原始结果予以保留。”
  - 非默认来源且既无 `source_version` 又所有 row 均无 source：“此报告缺少服务探测来源信息，无法核验解锁结果；历史原始结果予以保留。”
- selected-history 使用该 selected report 自己的 diagnostics、coverage 与 provider 结果；不把新政策回写或冒充为历史重新验证。

## 数据库风险评级与覆盖口径

页面不计算综合质量分。`deriveRiskAssessment(report)` 返回 `{ level, complete, knownFlags, totalFlags }`：

- `level` 只取成功 provider 的标准 `risk_level`（low/medium/high/critical，兼容 clean/safe/moderate）中的最高等级，没有则为 null；不读取 summary 的 `risk_level`，失败/跳过来源的残留字段不参与。summary 与成功来源矛盾时（例如 summary low、某成功来源 high）以成功来源为准。
- `knownFlags` 为五个负面字段（proxy、tor、vpn、abuse、robot）中至少由一个成功 provider 明确返回 `true`/`false` 的数量，`totalFlags` 固定 5，`complete` 当且仅当 5/5。机房/server 不属于负面风险，也不进入完整性。
- 服务为空、未知、失败或已知都不改变评级；同一 provider 矩阵在服务结果不同时必须得到相同评级。
- 无 summary、`summary.ambiguous` 或所选报告 `diagnostics_json.ip_conflict === true` 时不输出聚合 level/complete，主文改为“IP 归属不唯一，需先复核”或“来源返回了不同 IP，需复核来源”，provider 明细仍可查看。
- 展示规则：等级完整则显示等级与5/5；low 不完整时大字“证据不完整”，副文“已返回来源：低风险”及N/5，不着绿。medium/high/critical 不完整仍显示已发现等级并标缺口。无等级有负面则“发现风险信号”；全部明确 false 则“已返回风险字段未命中”；其余“证据不足”。过期仍注明仅作历史，不作当前绿色结论。
- “风险信号”指标按信号类去重计数（多个数据库都判 VPN 只算一项），高/严重等级的来源另列“高风险来源：…”，不计入项数；有命中显示“N 项”，无命中时完整显示“未命中”，不完整显示“未知”。原始每数据库 `risk_score` 只在来源行展示，不跨数据库平均。
- “已知字段一致性”只比较至少两个已知值的维度，保留 N 个可比较维度；boolean null/undefined、空字符串不当值；地区优先代码否则全名，不混比。没有维度显示“—”。
- “已启用来源响应成功率”分母为合法 coverage 的 expected_provider_count - not_configured_provider_count，skipped 仍在分母；无效 coverage 不构造比例。4/5响应成功不等于风险完整，未接入数量只放诊断。
- 服务指标分子仅完全 unlocked，分母为合法 coverage 的完整 expected_service_count。2 unlocked、1 partial、4 unknown 显示2/7可用并保留1部分可用/4未知；partial 不算完全可用，unknown 不算 blocked。coverage 缺失/非法只保留状态计数，不从行数重建比例。

## VPS 列表与详情的 IP 质量徽标

IPQualityBadge 只读默认族 summary，文本包含真实 summary.ip_version 的 IPv4/IPv6；缺族不猜。ambiguous/partial/failure/unknown/success 与 stale 原语义保留，无 summary 为未采集，不借另一族报告。Overview 由后端 SummaryCell.ip_version 贯穿生产/接收链，仅 IP-quality cell 有合法族时显示“IPv4/IPv6 · 默认族报告”，disabled/missing 不伪造。
