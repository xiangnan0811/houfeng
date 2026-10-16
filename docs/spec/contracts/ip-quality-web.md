# IP 质量 Web 合同

页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)，数据和探测判据见 [IP 质量合同](./ip-quality.md)。

IP 质量报告使用独立 `/vps/:vpsId/ip-quality` 页面，不是资产总览，也不计算综合质量分。数据库风险、服务可用性、邮件连通性和名单命中是独立事实；unknown 不等于失败或正常。主视图结果优先，错误、过期和历史身份仍以短标记保留；技术说明和原始诊断按需展开。

## 地址族、URL 与请求身份

- 复用 SegmentedControl，group label 为“IP 地址族”，页头和空态始终保留 IPv4 / IPv6 两个选项。
- 普通入口无族参数时请求 API default，不跨 VPS 记忆选择。首次响应按 selected_ip_version replace URL，先迁移已加载响应的 requestKey，不重复请求或闪空态。默认族只信任服务端；IPv4 lookup 失败不能静默切换 IPv6。
- 手动切族写 ip_version 并清除 report_id；历史链接同时带族和 report_id，detail API 只带 ID。历史 ID 的实际族优先于冲突 query，旧链接据响应补正。非法、重复或空族参数显示可恢复错误与“查看默认报告”，不发送非法 API 请求。
- requestKey 包含 VPS、family/default、历史 ID，取消与代次保护覆盖切族、切 VPS、前进后退及卸载。迟到响应不能覆盖当前选择；完成刷新不把历史切到 latest。
- families.latest_attempt 与 selected report 分层。仅服务端 availability 可证明缺族；错误码不能升级为缺族事实。新鲜 unavailable、lookup failure、尚无结果分别解释，过期 attempt 标为过期。旧有效报告可展示为“上次有效结果”，保留独立时间，不显示占位 IP 为出口。
- 首次或换选择请求失败仍使用同一页面外壳、族选择器和报告身份结构，正文显示 PageState error 与原重试/返回操作；错误身份为“加载失败，报告状态未知”，不能冒充无报告。同族后台刷新失败保留原结果并标记“刷新前报告”，不冒充成功。所有矩阵、时间、风险和历史只来自选定报告，不能借另一地址族填空。
- 切族时选择器保持 DOM 身份及焦点，位于上下文第一行，不被后方身份内容高度变化推动；loading 保留身份骨架与非交互动作占位，绝不沿用旧族 IP、时间或风险。尚未确定的默认族、历史真实族显示“待确定”，不把 query 族当作历史事实。empty、error、invalid 分别显示“暂无报告”“加载失败，报告状态未知”“地址族参数无效”，不靠 summary 是否为空推断状态。
- 无报告时由当前族 availability、attempt 与 stale 统一说明原因：从未采集可首次采集；新鲜缺族建议检查网络配置后重查；过期缺族/失败仅说明当时情况，不当当前故障；available/unknown 的出口失败不推断缺族。最近尝试时间和安全原因仅在空态正文展示。功能不可用仍保留无结果的原因，并复用采集通知中的恢复入口；首次采集状态读取进行中时说明正在检查，且采集入口禁用、不展开菜单、不发 POST；读取失败后不再称检查中，入口恢复可用，由 POST 给出明确结果。旧 VPS 会话的读取结果不改变新 VPS 的检查状态。
- 在途请求覆盖当前族时只承诺完成后刷新该族结果、是否形成报告以结果为准；仅另一族在途时说明本轮未包含当前族，结束后可选本族采集。任何在途请求都禁重复 POST，不承诺必然生成报告，不自动采集或恢复监控。

## 七模块单页

根节点为 `.page.vps-detail-workspace.ipq-page`，直接导入 `VPSDetailWorkspace.css`，区块复用 `.vps-detail-workspace__section`。暗色优先、中文为主，固定顺序如下：

1. **身份条**：静态页头保留标题、返回及动作区；紧随其后的独立“报告上下文”区直接位于 `.ipq-page` 下，只有该紧凑区域 sticky，吸顶基线与 `.main` scrollport 对齐（抵消其响应式 padding，不叠加外部 TopBar 高度）。第一行是族选择器，其后显示所选报告 IP/复制、真实族、观察时间；历史、已过期、上次有效结果、刷新前报告或非本轮结果按各自条件并列，不互相覆盖。不满足特殊条件仅称“所选报告”，不称实时。非历史且有报告时显示独立标记的最近尝试时间与必要过期说明；历史不混入当前 attempts。“查看最新报告”仅在历史报告真实族已知时可用并保留该族，否则为非交互占位。采集按钮 report（含历史）时仅页头一处、empty 时仅正文一处，loading 为 aria-hidden 占位，error/invalid 不提供采集。上下文高度随布局变化测量写入页面变量，正文标题与可聚焦元素使用相应 scroll-margin，长 IPv6、窄屏与缩放不能被遮挡。
2. **基础信息**：默认仅六项，顺序自治系统号、组织、城市、注册地、使用地、IP类型；桌面三列两行、中宽两列、手机一列。ASN与组织用同份关联证据，大整数保留decimal string。IP类型仅显示短badge（含未知），不附概念解释、比较说明或可展开的解释入口；只消费summary.ip_type，不从旧geo_status自动分类。ip_type方向固定：`country_code`是使用国，`comparison_country_code`是注册国（basis=registered_country）或滥用联系国家（basis=abuse_contact_country）；这些判定来源保留在报告数据中。主注册地只用真实registered-country：basis=registered_country时取comparison_country_code，否则只取来源明确的registered_country_code，不拿使用国、abuse、ASN或RIR国家补。其余域名、usage/company、PTR、省州、RIR、网段/前缀/规模/MOAS、时间与来源证据收进默认关闭的原生“更多信息”details，不重复六项。
3. **服务结果网格**：保留七服务，默认仅名称、短结论、已知地区；未知只写“未知”，原因进详情。revision5 Netflix originals_only短标“仅自制剧”；ChatGPT按匿名地区准入显示，不因dc403等无关入口风控降成“仅Web/App未知”，不声称App实测。规则、endpoint、版本、checks和失败原因折叠；来源和检测范围用短标签，不附通用概念解释。available/restricted/unknown视觉独立。
4. **逐源评分和风险因子**：评分与因子为紧邻子区；评分逐源对齐来源、scope、原始分值、短等级、条形，无分数不造空行，原标签/单位可展开。旧行（无typed details）只有真实risk_score时才占一行“旧版评分”并附旧版评级；仅有评级无分数不占评分行，旧版评级保留在区块头部标记。同vendor认证成功优先，否则匿名成功，失败增强不覆盖已知。Very Low/Low→normal，Elevated/Medium→notice，High/Very High→alert，Critical→critical（大小写无关）；未知标签原文neutral。仅数值无等级为原尺度中性条，零值严格0，不用统一40/80阈值、不跨源平均。风险矩阵保留来源×Proxy/VPN/Tor/Abuse/Robot/机房；负面因子“是”alert实心、“否”normal实心，机房是/否neutral；未知?、不支持—、错误!分别带可访问文字和可区分样式，不只弱文字/斜纹、不常驻图例。仅用现有主题state变量及vendorGroups/scoreDisplay/flagCellState/FieldStateMark/Badge/ScrollRegion；移动端局部滚动可聚焦且有名称，页面不溢出。ISP或is_server=false不推家宽。
5. **多源定位表**：来源、国家／省／市、经纬度、采集时间。保留每源独立数据和零坐标；不把 ASN 国家、注册地或另一来源城市混入。不新增地图依赖。基础信息与定位表的国家分歧只比较来源明确给出的国家代码（忽略代码大小写）：revision1 取 typed country_code，旧行取 region_code；国家名称仅展示，不把 `US` 与 `United States` 判为分歧，也不据名称或其他来源回填缺失代码。所有来源均无代码时，仅在显示值唯一时给出单值，不据名称宣称分歧；服务端 geo_status 的地理判据保持独立。
6. **邮件与黑名单双栏**：十二邮局 provider、状态、latency，MX 与目标地址详情可展开；端口固定 25。名单展示来源、类别、结果、匹配范围及 listed/checked。unknown 不计 checked，不显示成正常；邮件 220 不是送达或信誉保证。不显示无法证明的“本地 25 开放／封锁”。
7. **历史与诊断**：时间、出口、风险标记、服务可用数、查看；当前项可见，每份返回历史均可达。诊断默认关闭，raw 更深一层按需格式化。历史十三条未实现 optional 来源仅在诊断保留，不产生新占位或虚构配置入口。

主视图不展示覆盖率／一致性百分比、“五字段完整”口号或“证据不完整”主评级。采集覆盖和安全错误码属于诊断，不替代实际结果。技术文本按文本渲染、限高、键盘可滚动；横向宽表使用有名称、可聚焦的局部滚动区域。长 IPv6、域名和错误不得撑开页面。五主题在 1440、1024、390 宽度可用，移动端双栏自然堆叠。

来源分歧必须可核对对应字段：城市、使用地、省州链接至多源定位，使用类型、公司类型链接至风险因子；真实 href 保留原生滚动与修饰键/新窗口语义，普通激活同时把焦点移至可程序聚焦的目标标题。注册地、公司名称分别在本字段内提供默认关闭的原生 details，名称为“查看注册地来源分歧”“查看公司名称来源分歧”，列出同一 vendorGroups 成功 primary 的供应商、方法和值；空白值不参与，不借 ASN/RIR/使用国填注册地。一致/缺值不伪造分歧入口；国家继续按明确代码比较。样本展开按钮的可访问名称包含供应商和展开/收起动作，vendor-keyed 状态独立保持，不虚构 aria-controls。

定位、邮件、黑名单及两个诊断表均使用显式列头 scope，保留隐藏详情列名、原生 table 和可聚焦局部滚动。邮件原因使用正文排版，MX、目标地址和端口保留技术字体。服务 checks、endpoint 与诊断中的 HTTP 状态和安全原因统一组合，每条 HTTP N 只出现一次，不从原始异常提取数字；保留 endpoint、rule、类别、地区与结果。403 请求拒绝、429 限流、5xx 暂不可用仍为探测原因，不升级地区封锁；已通过入口校验的未知安全码仍可见。

## 事实、版本与历史

- revision1 直接消费后端 product.summary，详情、历史、资产入口和 Overview 使用同一事实口径。mail timeout、单源失败、pending/failed 的 Center 资料状态不抹去 Agent 已知 VPN 或名单阳性；不回退旧 risk_level。没有 product 的旧报告才以“旧版评级”展示历史等级。
- 来源风险因子仅呈现已知 risk_flags/high_risk_sources 阳性；product 存在但两者为空不显示“未见风险命中”或其他安全/阴性总评，不回退旧评级。黑名单仍独立展示服务端 listed/checked 与证据，不构成第二套综合评分。
- product.providers 保留各方法和时间的样本；默认选择同 vendor 的认证成功样本，否则匿名成功。不融合不同 vendor，不把匿名与认证当两个独立地理数据库。
- product.state 描述 Center 一次性资料任务，不是 IP 风险；completed_at 为空表示尚未终结。刷新、换 Key 不重查历史。PTR、mail 和观察时间都来自所选 Agent 报告。
- 服务聚合由共享后端算法确定，前端不另算风险或把 endpoint 数量当服务数。只有成功 probe 的 unlocked/partial/blocked 是已知业务结论；failure/skipped/not_configured/unknown 不得泄露残留 region、unlock_type 或 blocked。历史 probe_status 缺失/null 可按旧协议兼容，显式空值不视为成功。
- 四脚本行revision5直接消费规范化服务结论且无legacy typed checks；另三行revision4保持既有语义。报告级service5只标混合算法，每行版本优先；旧revision4 ChatGPT正向加未知为partial、Netflix双样片拒绝为partial/sample_titles_unavailable，仅用于历史解释，不重写旧事实。既有source身份连续。
- IPQualityBadge 只读默认族 summary，带真实族和 stale 状态。Overview 的 SummaryCell 透传 product_revision/product_summary 与 ip_version，风险阳性优先保留，采集 partial 不等于无风险；disabled/missing 不伪造族。

## 采集与资料轮询

立即采集由useIPQualityCollect管理；点击按钮只在正下方展开“采集 IPv4”“采集 IPv6”“采集 IPv4 / IPv6”三个动作，不带旧hint或aria-describedby、不替换解释段。复用action-menu方向键/Home/End/Escape/Tab/外部点击/焦点模式；选择即关闭，进行中禁重复提交，只读预览不可采集。POST body分别为ip_versions:[4]/[6]/[4,6]，省略默认双栈。

成功加载报告才读可用性，pending/dispatched每三秒轮询；completed/failed/expired或不可用停止。代次隔离迟到响应；409保留服务器真实在途requested_ip_versions，不静默改范围，unsupported_agent给可操作短错误。切族或重复点击不额外POST；请求scope支持single_family/dual_stack/legacy_single_family，族导航仍固定两页签。

完成不等于更新：不擅自切当前族或历史，仅非历史、当前族本轮success/partial、刷新成功且collection_id（legacy为collect_request_id）精确匹配才称“报告已更新”。仅采另一族短提示“IPv4采集完成”/“IPv6采集完成”，不把当前旧报告当更新；失败/unavailable/刷新失败只在当前族确有展示中的旧报告时才说“保留/仍展示上一份结果”，该族从无报告时只报本轮结论；unavailable如实缺族，aggregate partial不自动绿色成功。

报告上下文直接消费同一次采集通知的结构化结果：当前族失败/缺族且已有报告标“上次有效结果”，刷新中或失败的旧报告标“刷新前报告”，完成但身份未匹配标“非本轮结果”；历史、仅另一族、无报告分支不借此推断当前成功。只有完全没有采集通知时，同选择刷新失败才由外壳补标刷新前报告；存在采集通知（例如新一轮进度）时只认其自身 reportContext。安全 collect.error 提示优先级不变，不公开 raw error，也不根据中文通知文字反推状态。

资料 product.state=pending/running 时，页面可见才每三秒刷新同一选择；切族、切历史、卸载、终态停止。错误重试有界，失败保留既有结果和短错误，不无限后台请求。资料刷新不 POST 采集、不跳最新、不重新发外网查询。

不可用操作按事实导航：no_monitoring_instance/agent_not_bound 有 VPS ID 才进入监控工作台；monitoring_paused 仅使用当前 monitoring_instance_id 进入运行控制，不自动恢复；disabled 进入监控设置。缺 ID 不猜链接。首次错误页不启动采集查询。

## 来源设置

复用IPQualitySettingsSection，已实现ipapi.is、AbuseIPDB和MaxMind GeoLite三行来源及邮件开关。GeoLite配对maxmind_account_id/maxmind_license_key首次必须齐全，省略保留、null非法、明确清除整对；响应仅来源configured/masked_summary，不回显账号/Key原值。密码输入不把掩码回写，不输入即保留；错误/保存态保留安全操作语义。所有凭据不进报告、Agent plan、日志或URL。无Key默认geo后备仍运行：IPinfo abuse-contact-country可支持注明来源的IP类型，但不能冒充主注册地。

## 验收

测试覆盖族/历史请求身份、选择菜单键盘与409真实范围、failed/expired停轮询、非本族完成反馈、迟到响应、资料可见性轮询、未知与阴性/不支持/错误、逐源原始分数/scope/等级/零值、六默认字段/更多信息、ip_type provenance、部分失败保留风险、GeoLite配对省略/清除/掩码。历史夹具每个报告 ID 唯一且始终绑定同一 IP/族/观察时间/风险，前插新报告后打开旧 ID 不改变事实，不修改生产 key 或运行时去重掩盖问题。浏览器须访问隔离真实Center SPA/Agent/PostgreSQL，不以API拦截或全unknown截图替代；1440/1024/390与五主题、200%缩放、持续上下文/锚点/焦点、慢响应切族/错误恢复、局部滚动分别留证。报告真实网络与受控 fixture 的不同证据层，缺真实读屏工具不得宣称全页 WCAG AA；Hostcram四项新结果和三个保留服务的实机要求仍需独立授权与证据。真实IPv6、商业凭据、用户VPS网络与fixture分层报告；本地READY不是公开分发许可证审定或发布部署。
