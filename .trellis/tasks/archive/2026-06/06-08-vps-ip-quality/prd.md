# VPS IP 质量采集与决策证据

## Goal

实现 VPS IP 质量的低频自动采集、上报、入库、展示与资产组合决策证据接入，让资产组合决策中枢可以使用 IP 质量、IP 数据库判断和媒体/AI 服务解锁情况作为可解释证据。

## Requirements

- Agent 必须通过 Go 原生实现采集，不执行远程脚本或 shell 测试脚本。
- Center 统一控制 IP 质量采集开关、频率、超时、服务集合和保留策略；默认低频采集，频率独立于 host sample / probe frequency。
- Agent 采集不能阻塞心跳同步；采集结果通过现有 sync 机制附带上报，并支持离线队列。
- Center 必须保存报告元数据、基础 ASN/组织/地理位置/注册地、各 IP 数据库的类型/风险/因素结果、各服务商解锁结果，以及受限 raw JSON。
- 入库模型必须支持按 VPS 查询最新 IP 质量、历史报告、provider matrix 和 service unlock matrix。
- VPS 归属优先使用活跃 VPS-MonitoringInstance link；没有 link 时可用 VPS 当前 IPv4/IPv6 与报告出口 IP 精确匹配作补充，多 VPS 同 IP 时不得误归属为决策证据。
- 失败/部分失败报告必须可保存和展示，但不能被资产决策误判为 IP 质量差。
- 前端必须在 Settings、VPS 列表/详情和资产决策页面展示相关信息，遵循现有 API client/type/state 模式。
- 资产决策只把 IP 质量作为解释性证据和评分输入，不自动执行迁移、取消或续费动作。
- 本任务只实现 IP 质量；CPU/磁盘/内存性能与路由质量留给后续任务。

## Acceptance Criteria

- [ ] Center 设置可以启停 IP 质量采集，并配置低频采集周期。
- [ ] Agent 从 SyncPlan 获取 IP 质量计划，后台采集并在 SyncRequest 上报成功、失败或部分失败报告。
- [ ] Center 验证并原子保存 IP 质量报告、provider 结果和 service unlock 结果。
- [ ] `/api/vps/{vps_id}/ip-quality` 返回最新摘要、provider/service 矩阵和近期历史；VPS 列表/详情可返回轻量摘要。
- [ ] VPS 详情页展示 IP 基础信息、风险摘要、provider 判断和服务解锁；VPS 列表展示轻量 badge。
- [ ] Asset Decisions 的 facts/evidence/assessment/comparison/readback 接入 IP 质量缺口、过期、风险、出口不一致和服务解锁阻断。
- [ ] 原始 JSON 有大小限制并清理敏感信息；密钥类设置不回显到前端。
- [ ] 数据保留策略不会让 raw JSON 无限增长。
- [ ] Go 和 Web 相关测试覆盖契约、采集、入库、API、UI 和决策语义。
- [ ] 实施后完成审查，发现问题必须修复并重新验证。

## Notes

- 字段模型参考 `IP.Check.Place` JSON 分组：`Head`、`Info`、`Type`、`Score`、`Factor`、`Media`、`Mail`。
- 初始服务集合实现稳定核心服务：Netflix、ChatGPT/OpenAI、YouTube Premium、Amazon Prime Video、Disney+、TikTok、Reddit。
- 外部参考脚本只作为字段和行为参考，不作为运行依赖。
