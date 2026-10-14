# IP 质量 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

IP quality remains a read-only report, not another asset overview. Keep report identity/time, provider and service results, and every returned historical report reachable; put collection diagnostics behind an explicit disclosure. Database risk and service availability are separate facts: never fold them into a composite score, and never treat unknown results as failures or as clean. Wide observation/provider tables have named, keyboard-focusable local scroll regions. These related workspaces use the VPS title/body density and stack title/actions and fact groups when narrow.

- **低频深度报告使用独立页面承载**：IP 质量、性能基准、路由质量这类“买完 VPS 后才通过 agent 测得”的深度报告，字段多、矩阵多、历史/诊断多，不适合塞进 VPS 详情页的一个普通 section。VPS 详情页只保留摘要结论、关键风险/缺口和“查看完整报告”入口；完整驾驶舱应使用独立 route/page 展示质量结论、provider/service 矩阵、覆盖率、历史变化和诊断。这样后续性能、路由报告可以复用同一 IA，而不会把 VPS 详情页变成所有低频事实的长表堆叠。
- **低频报告主视图必须降噪采集字段**：这类报告的 API 往往同时返回用户事实和采集诊断。主视图只能展示用户可判断的质量事实，例如风险信号、provider 证据 chip、服务解锁状态、区域、解锁类型、覆盖率和历史；`source`、`probe_status`、`default_probe`、`not_configured`、latency、长 `error_summary`、raw JSON 等内部采集字段不得进入主卡片、摘要区或表格证据列。需要排障时放入低权重诊断层或折叠详情，并在测试里断言这些内部文本不会出现在主要视图。


页面标题为 IP 质量报告，不使用“驾驶舱”，不新增本地 Tabs 或第二套风险算法。

## 报告页结构

报告页与 VPS 详情页共用工作区版式：根节点为 `.page.vps-detail-workspace.ipq-page`，页面自行导入 `VPSDetailWorkspace.css`（直达该路由时 VPS 详情样式可能尚未加载），各区块使用 `.vps-detail-workspace__section`。自上而下：

1. **页头**：沿用 VPS 身份页头（`vps-overview-identity`）。报告身份压缩为一行 `dl[aria-label="报告身份"]`：出口 IP、网络（ASN + 组织）、使用地、与使用地不同时的注册地、报告时间（相对 + 绝对）。Agent 版本、报告标识、采集状态、归属方式、坐标只进诊断区。操作：最新报告显示“立即采集”（`md` 主按钮）+“返回 VPS 详情”；历史报告显示“历史报告”徽标，操作换成“查看最新报告”，不提供立即采集。
2. **采集状态条**：`role="status"`、名为“IP 质量采集状态”，只在有事可说时出现——不可用原因优先（请求已登记但采集随后关闭/暂停时也先显示原因）、进行中（等待 agent 同步 / agent 已接收，正在检测 + 已用时间）、本页发起或接手的请求完成（成功 / 失败仍展示上一份报告 / 超时）、不可用原因（采集关闭时附“前往设置开启”到 `/settings?tab=monitoring`）。不常驻解释性文字；历史上已结束的请求不提示。
2a. **服务探测可信度提示**：在采集状态条之后、风险评级之前，使用现有 `ipq-notice` 与 title/detail 类显示能力边界；提示独立使用 `role="note"`、`aria-label="服务探测可信度"`，不得占用采集状态的 live region。最新报告和 selected-history 必须用同一个 helper，并只依据当前 API 返回的 selected report，不查询或拼接另一份最新报告。revision 2 使用中性 `ipq-notice--muted`，历史停用/不可核验提示使用 `ipq-notice--warning`。
3. **数据库风险评级**（`aria-label="数据库风险评级"`）：大字为成功来源标准等级的最高值（低/中/高/严重风险）或 “—”，下附结论文案、证据范围（如“风险字段 4/5，结论不完整”）以及过期/归属复核/来源 IP 冲突/部分采集徽标；四个指标：风险信号、服务可用性（旧 revision 为“服务解锁”）、数据库一致性、已启用来源成功率。不展示综合质量分、“适合作为主力节点”一类建议或 summary 自带的风险徽标。
4. **服务**：revision 2 标题为“服务可用性（免登录探测）”，状态词为可用/部分可用/受限/未知；其他报告标题保留“服务解锁”，状态词为解锁/部分/受阻/未知。标题旁计数（`aria-label="服务状态统计"`，只有计数大于 0 才着色），逐服务卡片显示服务名与“状态 · 区域”；补充行只写中文检测范围或“未知”的原因。失败原因优先按 agent 的 `error_code`（`http_status`、`timeout`、`non_json_response`、`request_failed`、`read_failed`、`invalid_request`、`invalid_response`、`probe_failed`、`unsupported_service`、`unsupported_default_probe`、`response_too_large`、`unexpected_redirect`、`challenge_detected`、`egress_ambiguous`、`ip_mismatch`）给出中文短句；`invalid_response` 显示“响应未形成可靠业务结论”，`unsupported_default_probe` 保留“默认探测暂不支持该服务”，`response_too_large`“服务响应过大，未形成结论”，`unexpected_redirect`“探测被重定向到非预期入口”，`challenge_detected`“服务要求人机验证”，`egress_ambiguous`“当前出口不唯一，无法确认探测归属”，`ip_mismatch`“来源返回了不同IP”；`http_status` 只从完整的 `http status NNN` 取状态码（如 403 “服务拒绝了探测请求（HTTP 403）”、429 限流、5xx 暂不可用），无 code 的旧数据仅在 summary 恰为该格式时兼容；不展示这些原始 `error_summary`，原文只留在折叠的采集诊断里；未知 code 的安全说明原样显示。
5. **IP 数据库判断**：标题旁只列出被命中的信号（如 `VPN 1/3`，机房为中性色）；没有负面命中时，五个负面字段均有成功来源明确返回才写“已返回风险字段未命中”，否则写“风险字段不完整”，不得无条件宣称未发现代理/VPN/Tor/滥用。表格只放成功且至少给出一个判断字段的数据库，按负面信号、风险等级排序；信号列“是”用徽标、“否”用弱文本、未知用“—”；公司类型只读 `company_type`，不拿公司名冒充。来源 `extra_json.capability === "geo_only"` 时在名称下注明“该来源本次仅返回地理信息”。存在原始 `risk_score` 时脚注说明“风险分：数值越高风险越高；不同数据库不可直接比较”，原始 0 分照常显示。成功但无判断字段的数据库只计数；失败/跳过的默认来源以“未返回结果”徽标列出；未配置的可选来源只在诊断区出现。
6. **历史报告**：紧凑列表（时间、风险、出口 IP、查看），当前报告标记“当前查看”；超过 5 份时折叠并提供“显示全部 N 份”，当前报告在折叠部分时自动展开，保证每份报告可达。
7. **采集诊断**：默认折叠；摘要行写“N 个来源 · M 项服务探测”，再按来源行状态分别附“失败 a / 跳过 b / 未配置 c”（只写大于 0 的类别），不得把未配置的可选来源概括成失败。展开后是报告事实（含原始“来源覆盖（全部）”与“服务覆盖”的成功/失败/跳过/未配置计数，例如 `成功 5/18 · 失败 0 · 跳过 0 · 未配置 13`）、来源明细表、服务探测表和“原始数据”。原始 JSON（诊断、原始报告、来源/服务附加数据）每块再单独折叠，只显示标题和大小；展开时才格式化，放在限高（20rem）可滚动、可键盘聚焦的 `pre` 中，以文本渲染并可复制，不得撑开页面。

报告为空时保留页头与状态条，正文是空态卡片“暂无 IP 质量报告”，主操作为“立即采集”（页头不重复按钮）。报告首次加载失败才进入错误页；立即采集完成后的后台刷新失败保留当前报告。错误页不读取采集状态。

立即采集由 `useIPQualityCollect` 管理：报告加载成功后读取一次 `GET .../ip-quality/collect`；有进行中且仍可用的请求时每 3 秒轮询，请求结束、过期或变为不可用即停止，单次轮询失败继续下一轮；所有状态请求使用全局单调序号，每次切换 VPS 与组件卸载都开启新的会话代次（卸载时仍挂起的 POST 之后失败也不再补读状态）；迟到的旧响应、上一台 VPS 或同一 VPS 上一次访问的回调都不得覆盖当前结果；提交中状态只由 POST 自己结束，进行中时重复点击不再发请求；只读预览不显示立即采集按钮；本页发起或接手的请求变为 `completed` 时原地刷新最新报告（不回到加载态）。409 不单独提示错误文案，以随后读取的不可用原因为准。

不可用状态保留原因对应的操作入口：`no_monitoring_instance` / `agent_not_bound` 在有明确 VPS 上下文时进入 `/vps/{id}?workbench=monitoring`，复用现有创建或接入流程；`monitoring_paused` 只使用当前 `monitoring_instance_id` 进入实例运行控制，可附有效 `return_vps`，不从历史请求猜实例、不自动恢复监控；`disabled` 进入监控设置。缺少必要 ID 时只解释原因，不生成猜测链接。采集失败的主要文案使用安全中文说明，任意语言的原始错误均放在默认关闭的诊断详情中。


## 服务探测范围与历史可信边界

七个默认服务及其历史 source 身份保持连续：`netflix` → `netflix_title_probe`、`chatgpt` → `openai_status_probe`、`youtube-premium` → `youtube_premium_page_probe`、`amazon-prime-video` → `prime_video_page_probe`、`disney-plus` → `disney_default_probe`、`tiktok` → `tiktok_home_probe`、`reddit` → `reddit_home_probe`。agent 侧请求、判据与 revision 语义见 [IP 质量合同](./ip-quality.md)；页面只消费其中的 diagnostics/extra_json 元数据，不新增 DTO 字段。

- `effectiveUnlockStatus(unlock)` 是服务状态的唯一入口：只有 `probe_status=success` 且 `status` 为 `unlocked`、`partial` 或 `blocked` 才是已知结果；历史数据仅在 `probe_status` 缺失/null 时兼容旧语义。显式空字符串、failure、skipped、not_configured 与 unknown 均显示“未知”。统计、卡片颜色、状态词、区域与检测范围全部经由该入口，失败行残留的 blocked/region/unlock_type 不得进入卡片。
- `UnlockType` 显示为中文检测范围，不显示内部枚举：`catalog_sample`“抽样目录可访问”、`sample_titles_unavailable`“抽样片目不可用，未验证完整片库”、`web_and_app`“Web 与 App 入口”、`web_only`“仅 Web 入口可用”、`app_only`“仅 App 入口可用”、`web_verified_app_unknown`“Web 已验证，App 入口未知”、`app_verified_web_unknown`“App 已验证，Web 入口未知”、`premium_offer`“Premium 地区展示”、`territory_offer`“Prime 地区展示”、`supported_location`“地区支持检测”、`web_region`“网页地区检测”、`anonymous_web`“匿名网页入口”。表外的自定义/历史类型保留“类型 X”。
- 显式调用方选择的 custom JSON service adapter 仍可作为程序接口使用，不代表内置网站协议已验证，也不自动回退；其结论合同见 [IP 质量合同](./ip-quality.md)。
- 服务 coverage 使用报告提供的计数，不把已有行数归一成 100%；`skipped` / `not_configured` 各保留原分类。
- `defaultServiceProbeNotice(report)` 只在有 service rows 时评估，并从 `report.latest_report?.diagnostics_json` 安全读取非 null、非数组的 diagnostics 对象。`diagnostics.source_version === "v2"` 或任一 row.source 属于上述七个 source 时，报告才视为默认来源；custom-only 来源不显示默认能力警示。
  - `service_probe_revision` 精确为数值 `2`：中性提示“免登录服务探测，仅反映当前入口和地区可用性，不代表账号或付费内容权限。”页面同时把服务区切换为“服务可用性（免登录探测）”措辞。单个服务行的安全 `extra_json.probe_revision === 2` 也只让该卡片使用新状态词，不把新范围套到其他旧行。
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
- 展示规则：有等级且完整 → 等级 + “风险字段 5/5 已返回”；有等级但不完整 → “已返回来源：X风险” + “风险字段 N/5，结论不完整”，此时 low 不着绿色；无等级但有负面命中 → “发现风险信号”；无等级、无命中且五项全为 false → “已返回风险字段未命中”；其余 → “证据不足”。过期报告保留事实并注明“报告已过期，仅作历史风险信息”，不按当前评级着色。
- “风险信号”指标按信号类去重计数（多个数据库都判 VPN 只算一项），高/严重等级的来源另列“高风险来源：…”，不计入项数；有命中显示“N 项”，无命中时完整显示“未命中”，不完整显示“未知”。原始每数据库 `risk_score` 只在来源行展示，不跨数据库平均。
- “数据库一致性”只比较至少有两个已知值的维度：boolean null/undefined 不当作值，空字符串不计；区域优先比较至少两个 `region_code`，否则比较至少两个 `region_name`，不混用代码与全名。没有可比较维度显示 “—”。
- “已启用来源成功率”只用 API coverage：分母 `expected_provider_count - not_configured_provider_count`（已启用但跳过的来源仍在分母），辅助文字 `成功 X/Y · 未配置 Z`；无 coverage、分母 ≤ 0、负数或成功数大于分母时显示 “—”，不夹到 100%，不从行数重建。服务探测比例同样只用 coverage，异常时显示 “—”。原始 18 来源总口径只在诊断区展示。

## VPS 列表与详情的 IP 质量徽标

`IPQualityBadge` 只读 summary：`ambiguous` 显示“IP 归属不唯一”；`status=partial` 显示“IP 部分采集”，`failure` 显示“IP 采集失败”，其他非 success 显示“IP 状态未知”；success 显示“IP {风险等级}”并在未过期时附使用地区。任何已有报告 `stale` 都追加“ · 过期”，不得被 partial 等分支吞掉；没有 summary 显示“未采集”。不改变后端 success/partial/failure 的含义。
