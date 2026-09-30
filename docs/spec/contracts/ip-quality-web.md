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
3. **质量结论**（`aria-label="质量结论"`）：质量分 + 结论文案 + 风险等级/过期/归属复核/部分采集徽标；四个指标：风险信号、服务解锁、数据库一致性、采集完整性。不再展示“原因说明”列表。
4. **服务解锁**：标题旁计数（可用/受阻/部分/未知，只有计数大于 0 才着色），逐服务卡片显示服务名与“状态 · 区域”；补充行只写解锁类型或“未知”的原因。
5. **IP 数据库判断**：标题旁只列出被命中的信号（如 `VPN 1/3`，机房为中性色）；表格只放成功且至少给出一个判断字段的数据库，按负面信号、风险等级排序；信号列“是”用徽标、“否”用弱文本、未知用“—”。成功但无判断字段的数据库只计数；失败/跳过的默认来源以“未返回结果”徽标列出；未配置的可选来源只在诊断区出现。
6. **历史报告**：紧凑列表（时间、风险、出口 IP、查看），当前报告标记“当前查看”；超过 5 份时折叠并提供“显示全部 N 份”，当前报告在折叠部分时自动展开，保证每份报告可达。
7. **采集诊断**：默认折叠；展开后是报告事实、来源明细表、服务探测表和“原始数据”。原始 JSON（诊断、原始报告、来源/服务附加数据）每块再单独折叠，只显示标题和大小；展开时才格式化，放在限高（20rem）可滚动、可键盘聚焦的 `pre` 中，以文本渲染并可复制，不得撑开页面。

报告为空时保留页头与状态条，正文是空态卡片“暂无 IP 质量报告”，主操作为“立即采集”（页头不重复按钮）。报告首次加载失败才进入错误页；立即采集完成后的后台刷新失败保留当前报告。错误页不读取采集状态。

立即采集由 `useIPQualityCollect` 管理：报告加载成功后读取一次 `GET .../ip-quality/collect`；有进行中且仍可用的请求时每 3 秒轮询，请求结束、过期或变为不可用即停止，单次轮询失败继续下一轮；所有状态请求使用全局单调序号，每次切换 VPS 开启新的会话代次；迟到的旧响应、上一台 VPS 或同一 VPS 上一次访问的回调都不得覆盖当前结果；提交中状态只由 POST 自己结束，进行中时重复点击不再发请求；只读预览不显示立即采集按钮；本页发起或接手的请求变为 `completed` 时原地刷新最新报告（不回到加载态）。409 不单独提示错误文案，以随后读取的不可用原因为准。
