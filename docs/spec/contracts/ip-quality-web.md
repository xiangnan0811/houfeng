# IP 质量 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

IP quality remains a read-only report, not another asset overview. Keep report identity/time, provider and service results, and every returned historical report reachable; put collection diagnostics behind an explicit disclosure. Reuse existing risk calculations without introducing another score or treating unknown results as failures. Wide observation/provider tables have named, keyboard-focusable local scroll regions. These related workspaces use the VPS title/body density and stack title/actions and fact groups when narrow.

- **低频深度报告使用独立页面承载**：IP 质量、性能基准、路由质量这类“买完 VPS 后才通过 agent 测得”的深度报告，字段多、矩阵多、历史/诊断多，不适合塞进 VPS 详情页的一个普通 section。VPS 详情页只保留摘要结论、关键风险/缺口和“查看完整报告”入口；完整驾驶舱应使用独立 route/page 展示质量结论、provider/service 矩阵、覆盖率、历史变化和诊断。这样后续性能、路由报告可以复用同一 IA，而不会把 VPS 详情页变成所有低频事实的长表堆叠。
- **低频报告主视图必须降噪采集字段**：这类报告的 API 往往同时返回用户事实和采集诊断。主视图只能展示用户可判断的质量事实，例如风险信号、provider 证据 chip、服务解锁状态、区域、解锁类型、覆盖率和历史；`source`、`probe_status`、`default_probe`、`not_configured`、latency、长 `error_summary`、raw JSON 等内部采集字段不得进入主卡片、摘要区或表格证据列。需要排障时放入低权重诊断层或折叠详情，并在测试里断言这些内部文本不会出现在主要视图。


页面标题为 IP 质量报告，不使用“驾驶舱”，不新增本地 Tabs 或第二套风险算法。

## 报告页结构

报告页与 VPS 详情页共用工作区版式：根节点为 `.page.vps-detail-workspace.ipq-page`，页面自行导入 `VPSDetailWorkspace.css`（直达该路由时 VPS 详情样式可能尚未加载），各区块使用 `.vps-detail-workspace__section`。自上而下：

1. **页头**：沿用 VPS 身份页头（`vps-overview-identity`）。报告身份压缩为一行 `dl[aria-label="报告身份"]`：出口 IP、网络（ASN + 组织）、使用地、与使用地不同时的注册地、报告时间（相对 + 绝对）。Agent 版本、报告标识、采集状态、归属方式、坐标只进诊断区。操作：最新报告显示“立即采集”（`md` 主按钮）+“返回 VPS 详情”；历史报告显示“历史报告”徽标，操作换成“查看最新报告”，不提供立即采集。
2. **采集状态条**：`role="status"`、名为“IP 质量采集状态”，只在有事可说时出现——不可用原因优先（请求已登记但采集随后关闭/暂停时也先显示原因）、进行中（等待 agent 同步 / agent 已接收，正在检测 + 已用时间）、本页发起或接手的请求完成（成功 / 失败仍展示上一份报告 / 超时）、不可用原因（采集关闭时附“前往设置开启”到 `/settings?tab=monitoring`）。不常驻解释性文字；历史上已结束的请求不提示。
2a. **服务探测可信度提示**：在采集状态条之后、质量结论之前，使用现有 `ipq-notice ipq-notice--warning` 与 title/detail 类显示能力边界；提示独立使用 `role="note"`、`aria-label="服务探测可信度"`，不得占用采集状态的 live region。最新报告和 selected-history 必须用同一个 helper，并只依据当前 API 返回的 selected report，不查询或拼接另一份最新报告。
3. **质量结论**（`aria-label="质量结论"`）：质量分 + 结论文案 + 风险等级/过期/归属复核/部分采集徽标；四个指标：风险信号、服务解锁、数据库一致性、采集完整性。不再展示“原因说明”列表。
4. **服务解锁**：标题旁计数（可用/受阻/部分/未知，只有计数大于 0 才着色），逐服务卡片显示服务名与“状态 · 区域”；补充行只写解锁类型或“未知”的原因；探测失败原因优先按 agent 的 `error_code`（`http_status`、`timeout`、`non_json_response`、`request_failed`、`read_failed`、`invalid_request`、`invalid_response`、`probe_failed`、`unsupported_service`、`unsupported_default_probe`）给出中文短句；`invalid_response` 显示“响应未形成可靠业务结论”，`unsupported_default_probe` 保留“默认探测暂不支持该服务”；`http_status` 只从完整的 `http status NNN` 取状态码（如 403 “服务拒绝了探测请求（HTTP 403）”、429 限流、5xx 暂不可用），无 code 的旧数据仅在 summary 恰为该格式时兼容；不展示这些原始 `error_summary`，原文只留在折叠的采集诊断里；未知 code 的安全说明原样显示。
5. **IP 数据库判断**：标题旁只列出被命中的信号（如 `VPN 1/3`，机房为中性色）；表格只放成功且至少给出一个判断字段的数据库，按负面信号、风险等级排序；信号列“是”用徽标、“否”用弱文本、未知用“—”。成功但无判断字段的数据库只计数；失败/跳过的默认来源以“未返回结果”徽标列出；未配置的可选来源只在诊断区出现。
6. **历史报告**：紧凑列表（时间、风险、出口 IP、查看），当前报告标记“当前查看”；超过 5 份时折叠并提供“显示全部 N 份”，当前报告在折叠部分时自动展开，保证每份报告可达。
7. **采集诊断**：默认折叠；展开后是报告事实、来源明细表、服务探测表和“原始数据”。原始 JSON（诊断、原始报告、来源/服务附加数据）每块再单独折叠，只显示标题和大小；展开时才格式化，放在限高（20rem）可滚动、可键盘聚焦的 `pre` 中，以文本渲染并可复制，不得撑开页面。

报告为空时保留页头与状态条，正文是空态卡片“暂无 IP 质量报告”，主操作为“立即采集”（页头不重复按钮）。报告首次加载失败才进入错误页；立即采集完成后的后台刷新失败保留当前报告。错误页不读取采集状态。

立即采集由 `useIPQualityCollect` 管理：报告加载成功后读取一次 `GET .../ip-quality/collect`；有进行中且仍可用的请求时每 3 秒轮询，请求结束、过期或变为不可用即停止，单次轮询失败继续下一轮；所有状态请求使用全局单调序号，每次切换 VPS 与组件卸载都开启新的会话代次（卸载时仍挂起的 POST 之后失败也不再补读状态）；迟到的旧响应、上一台 VPS 或同一 VPS 上一次访问的回调都不得覆盖当前结果；提交中状态只由 POST 自己结束，进行中时重复点击不再发请求；只读预览不显示立即采集按钮；本页发起或接手的请求变为 `completed` 时原地刷新最新报告（不回到加载态）。409 不单独提示错误文案，以随后读取的不可用原因为准。

不可用状态保留原因对应的操作入口：`no_monitoring_instance` / `agent_not_bound` 在有明确 VPS 上下文时进入 `/vps/{id}?workbench=monitoring`，复用现有创建或接入流程；`monitoring_paused` 只使用当前 `monitoring_instance_id` 进入实例运行控制，可附有效 `return_vps`，不从历史请求猜实例、不自动恢复监控；`disabled` 进入监控设置。缺少必要 ID 时只解释原因，不生成猜测链接。采集失败的主要文案使用安全中文说明，任意语言的原始错误均放在默认关闭的诊断详情中。


## 服务探测能力与历史可信边界

默认服务解锁当前是能力诊断，不是成功的网页 HTTP 探针。七个默认服务及其历史 source 身份保持连续：`netflix` → `netflix_title_probe`、`chatgpt` → `openai_status_probe`、`youtube-premium` → `youtube_premium_page_probe`、`amazon-prime-video` → `prime_video_page_probe`、`disney-plus` → `disney_default_probe`、`tiktok` → `tiktok_home_probe`、`reddit` → `reddit_home_probe`。

- 默认七项服务不得发起服务域名网络请求；每项都显示 `status=unknown`、`probe_status=skipped`、`error_code=unsupported_default_probe` 的能力诊断，不填写区域、解锁类型、延迟或虚假的 raw/extra。settings 包含完整默认集合时，service coverage 固定为 expected 7 / successful 0 / failed 0 / skipped 7（即 0/7），unknown 不表示服务受阻；provider lookup 成功时顶层报告仍可为 success，skipped 不制造 partial。
- 默认采集路径的新报告 diagnostics 必须包含 `"source_version":"v2"` 和数值 `"service_probe_revision":1`；显式 custom/legacy 路径不附加该默认来源标记。该 revision 只标记当前安全诊断政策，不代表默认服务已验证成功；不新增顶层 DTO 或数据库字段。默认 provider 的预算、取消和结果顺序合同不因服务阶段停用而改变。
- 显式调用方选择的 custom JSON service adapter 仍可作为程序接口使用，不代表内置网站协议已验证，也不自动回退。它按 `status` → `unlock_status` 优先，状态为空才读取 `unlocked` bool（`true` → `unlocked`、`false` → `blocked`）；`unlocked`、`blocked`、`partial` 为成功探测结论并保留业务错误说明，`unknown`、空对象、缺少结论、非法状态为 `unknown` + `failure` + `invalid_response`，读取/HTTP/JSON 错误也为 `unknown` + `failure`，不得用裸 HTTP 状态码形成解锁结论。
- 既有字段优先级包含空字符串边界：已存在的空 `status` 不继续读取 `unlock_status`，而使用 `unlocked` bool 回退；例如 `{"status":"","unlock_status":"partial","unlocked":true}` 显示 `unlocked`。没有有效 bool 结论时显示无结论失败，不新增同字段冲突策略。
- 服务 coverage 使用报告提供的计数，不把已有行数归一成 100%；新报告只有 `probe_status=success` 且状态为 `unlocked`、`blocked` 或 `partial` 才计 successful，`skipped` / `not_configured` 各保留原分类，其余非法组合计 failed。默认 skipped 行不参与 blocked/unlocked 或风险 evidence。
- `defaultServiceProbeNotice(report)` 只在有 service rows 时评估，并从 `report.latest_report?.diagnostics_json` 安全读取非 null、非数组的 diagnostics 对象，不把 unknown 直接类型强转后取属性。`diagnostics.source_version === "v2"` 或任一 row.source 属于上述七个 source 时，报告才视为默认来源；custom-only 来源不显示默认能力警示。revision 精确为数值 `1` 且至少一条 row 为 `skipped`/`unsupported_default_probe` 时显示：“默认服务解锁探测已停用：缺少可靠业务证据。未知不表示服务受阻，服务覆盖与质量评级可能不足。”
- 默认来源报告的 revision 缺失、字符串 `"1"`、非法或不认识的其他数值时显示：“此报告使用旧版或未识别的服务探测规则，解锁结果及依赖它的质量评分可能不可靠；历史原始结果予以保留。”revision 为数值 `1` 且没有上述 skipped 行时不显示额外警示。selected-history 使用该 selected report 自己的 diagnostics；不把新政策回写或冒充为历史重新验证。

## 评分证据门槛与风险结论

质量分仍沿用既有风险等级、负面信号、服务状态、过期和归属扣分公式，但只有
在报告存在 summary、五个负面风险字段（proxy、tor、vpn、abuse、robot）都
至少由一个成功 provider 明确返回 `true` 或 `false`，且至少有一条满足
`probe_status=success` 且 `status` 为 `unlocked`、`blocked` 或 `partial` 的
已知成功服务探测后才生成分数。`probe_status=success` 单独存在、或
`status=unknown` 与 success 组合，均不构成已知成功；当前默认七项全为 skipped，
所以服务覆盖为 0/7，质量分显示 `—`。
历史兼容数据仅在 `probe_status` 缺失/null 且 status 为 unlocked、partial 或
blocked 时视为已知成功；显式空字符串、failure、skipped、not_configured 和
unknown 均不满足门槛。失败 provider 的残留 flags、失败 service 的残留
blocked/partial 文本不得参与评分，缺证据显示 `—` 与“证据不足，暂不评级”。

风险事实与评级是两层信息：即使不能形成分数，成功 provider 已明确返回的负面
事实仍展示。五个必需风险字段均已返回且没有命中时，风险指标显示“已返回的
风险字段未命中”；任一字段缺失时显示“风险证据不足，未形成完整结论”。机房/
server 不属于评级门槛，也不作为负面风险。
