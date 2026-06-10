# 修复 IP 质量详情展示与采集覆盖

## Goal

修复 IP 质量详情页在 v0.53.2 后仍存在的展示瑕疵与采集覆盖疑点，让页面只展示对用户判断有价值的信息，同时确保 agent 对默认 IP 数据库来源的采集、解析、上报足够完整。用户侧看到的主表不得被大量 `not_configured`、空字段、内部英文诊断或无意义占位淹没；真实 provider 返回的风险、地区、类型、proxy/vpn/server 等信号必须尽量解析并保留下来。

本任务是上次 IP 质量 UX refinement 的 follow-up。它不是单纯前端文案修补：第 6 项必须追查 agent -> center -> API -> web 链路，确认字段为空到底是采集不足、解析不足、center 丢字段，还是前端把诊断行当成主判断行展示。

参考测试 IP：`209.33.173.4`。

已现场确认的上游响应样例：

- `ipapi.is` 对 `209.33.173.4` 返回 ASN `63150`、组织 `BAGE CLOUD LLC`、使用地 `JP/Tokyo`、`is_datacenter=true`、`is_proxy=false`、`is_vpn=false`、`company.type=hosting`。
- `proxycheck.io` 返回 `risk=66`、`proxy=yes`、`type=VPN`、地区 `JP/Tokyo`。
- `ip2location.io` 返回 `country_code=JP`、`region_name=Tokyo`、`is_proxy=false`、ASN 信息。
- `ipwho.is` 返回 `country_code=JP`、`connection.asn=63150`、`connection.org=BAGE CLOUD LLC`。
- `ipquery.io` 当前返回 `US/Windstream` 且与其他源分歧，应进入 provider 分歧/诊断，而不是被合并吞掉。

## Requirements

1. IP 质量详情页驾驶舱标题区去掉说明文字：
   - 删除“完整展示低频 IP 质量报告：风险来源、provider 分歧、服务解锁、覆盖率、历史和诊断。”
   - 保留标题、最近采集时间、agent/provider/service 元信息和返回 VPS 详情入口。
2. 风险信号矩阵去掉右侧说明文字：
   - 删除“Server / Datacenter 本身只作为上下文，不单独构成负面风险。”
   - 规则本身仍保持：server/datacenter 只作为上下文，不计入负面风险。
3. 各 IP 数据库判断去掉右侧说明文字：
   - 删除“逐 provider 展示，不把分歧合并掉。”
   - 逐 provider 展示的行为仍保留。
4. 服务解锁矩阵不得向主视图展示内部英文诊断：
   - `safe default probe is not available without optional service configuration`、`default_probe`、`unsupported_default_probe`、`not_configured`、source/probe 技术名不得作为服务卡片说明出现。
   - 未知、跳过、未配置、探测失败必须映射为用户可理解的中文中性说明，例如“本轮未形成可靠解锁结论”“默认探测暂不支持该服务”“探测失败，未判定为受阻”。
   - unknown 不得被渲染成白色突兀状态，应使用项目主题里的中性/灰色状态。
5. 服务解锁矩阵右上角状态统计必须横向排列并右对齐：
   - 桌面视口保持一行横排；空间不足时才允许自然换行，但不得竖向一列堆叠。
   - 实现必须使用本项目主题配色与现有 badge 风格，不另起独立色板。
6. 各 IP 数据库判断必须修复大量空字段/无意义值问题：
   - agent 默认可安全采集的 provider 源必须尽量解析并填充已有结构化字段：使用类型、公司类型、风险等级/风险值、地区、Proxy、Tor、VPN、Server、Abuse、Robot、错误/诊断、extra JSON。
   - 用 `209.33.173.4` 的固定 fixture 覆盖 parser 回归，至少验证 `ipapi.is`、`proxycheck.io`、`ip2location.io`、`ipwho.is`、`ipquery.io` 的字段不会被解析成大面积空值。
   - 需要账号、商业授权、前端临时 key、浏览器挑战或网页聚合的来源可以继续作为采集缺口/诊断，但不得混入主判断表形成大量“未评级、未知、无用户证据”的空行。
   - 如果 center/API 已完整保存但前端误展示，修前端；如果 agent 已采集但 parser 丢字段，修 agent parser；如果 center 入库/API 丢字段，修 center contract 和测试。
7. 各 IP 数据库判断的风险列必须紧凑：
   - 风险等级和风险值在同一行内展示或以紧凑 inline chip 展示，不得上下换行撑高表格行。
   - 表格行高应主要由 provider 名称和证据 chip 决定，不因风险值或空诊断文本异常增高。
8. 历史数据保留：
   - 本任务不得删除或重写旧 IP 质量历史报告。
   - 旧报告如果缺字段，应按现有数据展示为缺口；新 agent 采集的报告应体现修复后的字段完整性。
9. 不允许执行用户提供的远程 shell 测试脚本作为 agent 实现方式：
   - `check.unlock.media`、`IP.Check.Place`、`run.NodeQuality.com`、`ecs.sh` 只作为字段和展示参考。
   - agent 仍必须使用 Go-native HTTP collector，遵守 `.trellis/spec/backend/ip-quality-contract.md`。

## Acceptance Criteria

- [ ] IP 质量详情页驾驶舱不再出现“完整展示低频 IP 质量报告...”说明。
- [ ] 风险信号矩阵不再出现“Server / Datacenter...”说明。
- [ ] 各 IP 数据库判断不再出现“逐 provider 展示...”说明。
- [ ] 服务解锁卡片在 unknown/skipped/not_configured/failure 场景不展示 `safe default probe...`、`default_probe`、`unsupported_default_probe`、`not_configured` 等内部文本。
- [ ] 服务解锁统计在桌面视口横向右对齐；移动视口不溢出、不遮挡、不出现一列堆叠导致的丑陋布局。
- [ ] 各 IP 数据库判断主表不再被 optional `not_configured` 来源的空行淹没；采集缺口在 coverage/diagnostics 中以低权重展示。
- [ ] `209.33.173.4` fixture 测试证明默认 provider parser 能解析出关键字段：
  - `ipapi.is`: `hosting` / `JP` / `is_server=true` / proxy-vpn-tor false。
  - `proxycheck.io`: `risk_score=66`、中/高风险等级映射、`proxy=true`、`usage_type=VPN` 或等价结构化字段、`JP`。
  - `ip2location.io`: `JP/Tokyo`、`is_proxy=false`，并保留 ASN/原始上下文到 extra JSON。
  - `ipwho.is`: `JP/Tokyo`、ASN/组织上下文进入 report fallback 或 extra JSON。
  - `ipquery.io`: 成功采集但与其他源 IP 地区/ASN 分歧时保留为 provider 分歧，不覆盖其他来源结论。
- [ ] 风险列的风险等级和风险值不换行撑高表格行。
- [ ] 新增或更新 Go/Web 测试覆盖 parser、展示 helper、页面文本/布局关键断言。
- [ ] 正式实现后通过浏览器视觉检查 IP 质量详情页，覆盖桌面和移动视口。
- [ ] 质量门通过：相关 Go 测试、`web` lint/test/build、`git diff --check`。

## Notes

- Out of scope: CPU/磁盘/内存性能、路由质量、资产决策组合中枢改版。
- Out of scope: 接入需要账号/API key/商业授权的第三方源配置系统。可以保留为 future optional source，不在本任务强行完成。
- Out of scope: 执行或解析远程 shell 脚本 stdout。
