# IP质量采集覆盖与展示完整性修复

## Goal

修复真实环境中 VPS IP 质量数据覆盖严重不足的问题：agent 必须采集准确且足够全面的 IP 质量与服务解锁数据，完整上报给 center；center 必须完整保存结构化结果和历史；API 与前端必须展示这些数据，而不是只展示单一 provider 的基础 IP 信息。

本任务的目标不是美化现有页面，而是补齐 IP 质量事实链路：多源 IP 数据库判断、风险因素、服务解锁、采集覆盖率、诊断和历史都要可以从 agent 采集结果一路追踪到页面。

## User Value

- 用户购买 VPS 后，需要通过真实检测判断 IP 纯净度、代理/VPN/Tor/abuse/robot 等风险、各数据库分歧、流媒体/AI 服务解锁情况，而不是看到已经知道的 IP/ASN/组织基础信息。
- 资产组合决策后续依赖 IP 质量证据；缺少完整数据会导致决策中枢无法判断 VPS 是否适合主力节点、流媒体节点、AI 访问节点或需要复核。
- 历史数据必须保留，以便观察 IP 质量变化、服务解锁变化、provider 判断变化和采集异常。

## Confirmed Facts

- 真实测试环境截图中 `/vps/:id/ip-quality` 只有 `1 个 provider`、`0 个服务`，风险信号几乎全来自 `ipapi.is`。
- 用户已确认采用“两层采集源”方案：默认启用无需 API key、可稳定访问的数据源和服务探测；需要 API key、配额或商业授权的数据源作为可选配置接入，并在页面明确显示“未配置/未采集”，不伪造脚本级覆盖率。
- 当前 agent 默认 lookup URL 是 `https://api.ipapi.is`，`defaultServiceURL` 为空；因此默认只会采一个 IP lookup provider，且不会采 Netflix/ChatGPT/YouTube 等服务解锁结果。
- 当前 center contract / DB 主表和 provider/service 子表只结构化保存：
  - 报告主信息：IP、版本、ASN、组织、坐标、使用地、注册地、风险等级、错误与 raw JSON。
  - provider：provider、usage/company/risk/region、proxy/tor/vpn/server/abuser/robot、错误。
  - service unlock：service、status、region、unlock_type、错误。
- 当前 API 会返回 `latest_report.raw_json`，但前端完整页没有展示 raw JSON 中未归一字段；且 agent 当前 raw 主要也只有 ipapi.is 的 lookup envelope。
- 当前 DB 已有历史报告表和 retention：raw 默认 90 天、history 默认 365 天；历史保留能力存在，但只保留当前窄数据。
- 之前任务明确为了止血禁用了默认 service unlock URL，避免请求不存在的 404/HTML endpoint；这解决了失败刷屏，但牺牲了服务解锁覆盖。
- `IP.Check.Place` 脚本静态分析显示它本身是多源 IP 质量采集器，覆盖 Maxmind、IPinfo、Scamalytics、IPRegistry、ipapi、AbuseIPDB、IP2Location、DB-IP、IPData、IPQualityScore，以及 TikTok、Disney、Netflix、YouTube、Amazon、Reddit、ChatGPT、邮件/DNSBL 等分组。
- `NodeQuality` 脚本会下载独立 BenchOS 并生成 `ip_quality.log` / `ip_quality.json`，不是当前 Go collector 的能力。
- `ecs.sh` 是综合性能/路由/流媒体脚本，本任务只处理 IP 质量和服务解锁，不处理 CPU、磁盘、内存、路由。
- 项目现有 spec 要求 agent 不执行远程 shell 脚本；IP 质量采集应继续使用 Go 原生 HTTP collector 和受控 parser。

## Root Cause Analysis

- **主要根因是 agent 采集覆盖不足**：当前默认只接入 `ipapi.is` 一个 provider，且 service unlock 默认不执行，所以 center 收到的数据天然很少。
- **次要根因是上报/入库 schema 偏窄**：当前结构化字段只覆盖通用 provider 风险列和简单 service unlock 列，无法表达 IP.Check.Place/IPPure/MeowVPS/Net.Coffee 这类工具展示的全部维度，例如 provider-specific score/detail、数据源分组、邮件/DNSBL、地理一致性、多源解释、采集覆盖状态等。
- **展示层也存在不足，但不是首要原因**：页面能展示现有 provider/service 矩阵；但没有 raw/extra details 面板，也无法展示尚未进入 API contract 的字段。
- **center 当前历史保留能力存在**，但保留的是不完整报告；修复后需要保证新字段随每次报告一起保留并能按历史查询。

## Requirements

- Agent 采集必须从单 provider 扩展为两层多源 IP 质量采集：默认层覆盖无需 API key 且可稳定访问的数据源；可选层为需要 API key、配额或商业许可的数据源保留配置与展示入口，不能硬编码或伪造结果。
- Agent 采集必须继续使用 Go 原生实现，不得执行 `bash <(curl ...)`、下载运行远程脚本、执行未知二进制或解析任意 shell stdout 作为默认生产路径。
- Agent 必须采集并上报每个数据源的原始状态：
  - provider/source 名称、请求是否成功、错误摘要、采集耗时或超时状态。
  - usage type、company type、risk level、risk score、region、proxy、tor、vpn、server/datacenter、abuser、robot/bot/crawler 等通用字段。
  - provider-specific extra fields，以安全 JSON 形式保留，不因为当前 normalized schema 没有列而丢失。
- Agent 必须采集并上报服务解锁矩阵：
  - 至少覆盖当前 settings 默认服务集合：Netflix、ChatGPT、YouTube Premium、Amazon Prime Video、Disney+、TikTok、Reddit。
  - 每个服务要包含 status、region、unlock_type、错误、source/provider、extra details。
  - 服务探测失败不能拖垮 IP 数据库结果；整份报告可为 `partial`。
- Center 必须保存完整报告历史：
  - 主报告、provider rows、service rows、每行 extra/raw、安全脱敏 raw。
  - 对同一 report 的 provider/service 不应因为同名来源重复或字段缺失而静默丢掉重要细节。
  - 历史查询必须能看到过去报告的 provider/service/detail，而不是只有 summary。
- Center API 必须返回完整结构化字段：
  - 最新报告完整 provider matrix、service unlock matrix、coverage/diagnostics。
  - 历史报告列表至少能展示每次采集的 provider_count、service_count、风险/解锁摘要；必要时提供单次历史详情。
- 前端完整 IP 质量页必须展示所有新结构化字段：
  - 多源 provider 分组/表格和 provider-specific details。
  - 服务解锁详情，不仅是数量。
  - 采集覆盖、失败 provider、失败 service、错误摘要。
  - raw/extra 字段的安全查看方式，避免只保存在 API 里而页面不可见。
- VPS 详情页保持摘要卡，不承载全部字段，但必须准确提示覆盖不足、跳转完整页面。
- 资产组合决策可以继续只消费摘要/evidence；本任务不要求完整嵌入资产决策页，但不能破坏现有 evidence 语义。
- 历史 retention 仍应保留 raw 与 normalized history 的独立期限；新增 extra/raw 字段必须纳入脱敏和 retention。
- 真实采集频率仍保持低频，默认一天一次，可配置为三天/一周；不应阻塞普通心跳同步。

## Acceptance Criteria

- [ ] 真实 agent 默认启用后，不再只上报 `ipapi.is` 一个 provider；API 中 `provider_results` 能包含多个实际数据源或明确的 source failure 记录。
- [ ] 真实 agent 默认启用后，服务解锁结果不再长期为 `0 / 0`；默认服务集合有逐服务结果或逐服务失败诊断。
- [ ] Agent payload 包含 provider/service extra details；center 入库后通过 API 可以取回，不丢字段。
- [ ] Center 存储迁移保留旧报告兼容，并支持新报告的 provider/service extra/raw 历史。
- [ ] `/api/vps/{vps_id}/ip-quality` 返回最新完整报告和历史摘要；如新增历史详情接口，必须有测试覆盖。
- [ ] 前端完整页能展示多 provider、多服务、extra details、失败源、覆盖率和历史；不再只展示少量基础 IP 信息。
- [ ] 失败、partial、ambiguous、stale 的语义保持正确：不能把采集失败当作 IP 高风险，也不能把未知服务当作受阻。
- [ ] `status=failure` 和 `0.0.0.0` 仍不进入用户侧 latest/history 真实 IP 事实。
- [ ] Agent 不执行远程 shell 脚本；所有第三方请求有 timeout、User-Agent、错误隔离和测试覆盖。
- [ ] 新增字段经过 raw JSON 脱敏和大小限制测试。
- [ ] 后端测试覆盖 agent collector、多源 parser、sync payload、center ingest、store/API、migration/retention。
- [ ] 前端测试覆盖完整页多源展示、服务解锁详情、失败/空态、历史和摘要跳转。
- [ ] 上线后新版本 Docker image 和 agent release assets 能按既有 release 流程发布。

## Out of Scope

- CPU、磁盘、内存、路由质量采集和展示。
- 在 agent 中执行第三方远程 shell 脚本或未知二进制。
- 自动购买/配置商业 API key；本任务只支持可选 key 配置和无 key 降级。
- 本任务不要求把完整 IP 质量页嵌入资产组合决策页面。

## Source Decisions

- 已确认：采用两层采集源模型。
  - 默认层只启用无需 API key、能直接返回结构化结果、不会依赖远程 shell、未知二进制、临时网页 key、私有后端或嵌入式会话 cookie 的来源。
  - 可选层保留 MaxMind、IPinfo official、IPRegistry、IPData、IPQS、AbuseIPDB、Scamalytics、ipapi.co、ip-api.com、IP.Check.Place 聚合端点、IPPure/MeowVPS/Net.Coffee 自有聚合 API 等入口，但未配置时必须展示为“未配置/未采集”，不能隐藏覆盖缺口，也不能伪造脚本级覆盖率。
  - 默认服务探测先覆盖可低侵入实现的 Netflix、ChatGPT/OpenAI、YouTube Premium、Amazon Prime Video、TikTok、Reddit；Disney+ 等需要更复杂公共客户端流程或存在 cookie/token 稳定性风险的服务，必须至少产生逐服务诊断行，不允许继续表现为 0 个服务。

## Open Questions

- No blocking open questions remain. Implementation still requires user approval to activate the Trellis task.

## Notes

- 真实测试截图中的展示问题是症状；当前证据指向 agent 采集覆盖不足是首要根因。
- 当前页面的“采集完整性 100%”是相对当前窄 report 的 1/1，并不代表真实 IP 质量采集完整，这需要在数据模型中引入 expected sources / source status 后修正。
