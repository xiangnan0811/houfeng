# VPS IP 质量采集与决策证据设计

## Architecture

新增一条低频事实链路：Center settings 生成 agent `ip_quality_plan`，Agent 后台 collector 执行 Go 原生 IP/API/服务解锁测试，完成后通过现有 `SyncRequest` 附带报告；Center 在 sync 事务中验证 fingerprint/token 后保存报告，并在 VPS/API/资产决策读模型中读取 latest summary。

Agent 心跳 tick 不直接访问外部服务，只 drain 已完成报告。采集状态写入 `/var/lib/houfeng-agent/ip-quality-state.json`，避免重启后重复采集。Sync queue 已持久化整条 `SyncRequest`，IP 质量报告随 sync 一起离线缓冲。

## Data Model

使用混合表结构：

- `ip_quality_reports`：报告主表，包含 monitoring_instance_id、observed_at/received_at、agent_version、fingerprint、sync_batch_id、出口 IP、IP version、status、ASN/组织/坐标/使用地/注册地、风险摘要、错误摘要、raw_json、created_at。
- `ip_quality_provider_results`：每个 provider 一行，保存 usage/company/risk/region 与 proxy/tor/vpn/server/abuser/robot 等因素。
- `ip_quality_service_unlocks`：每个服务一行，保存 service、status、region、unlock_type、error_summary。

主表生成 `report_id`，provider/service 表通过外键级联。latest 查询按 `observed_at desc, report_id desc`。raw JSON 限制大小并做敏感字段清理。

## Contracts

- `agentapi.SyncPlan` 增加 `ip_quality_plan`：`enabled`、`frequency_seconds`、`timeout_seconds`、`services`。
- `agentapi.SyncRequest` 增加 `ip_quality_reports`。报告必须携带 observed_at、agent_version、fingerprint、sync_batch_id、status 和 ip_address；失败报告允许缺少 provider/service 细节。
- Settings 增加 `ip_quality_settings`：enabled、frequency_seconds、timeout_seconds、raw_retention_days、history_retention_days、services。
- HTTP 增加 `GET /api/vps/{vps_id}/ip-quality`；VPS record 增加可选轻量 `ip_quality_summary`。

## Assignment and Decision Semantics

VPS 归属优先级：

1. 通过 active `vps_monitoring_instance_links` 关联 monitoring instance。
2. 若无 active link，用 VPS 当前 IPv4/IPv6 与报告出口 IP 精确匹配。
3. 若同一出口 IP 匹配多个 current VPS，返回 ambiguous 标识并禁止进入资产决策评分。

资产决策新增 evidence kinds：`ip_quality_missing`、`ip_quality_stale`、`ip_quality_risk`、`ip_egress_mismatch`、`media_unlock_blocked`。失败或 partial 采集只产生缺口/失败提示，不产生负面 IP 质量风险。`server/datacenter` 本身不作为负面风险，必须与 proxy/vpn/tor/abuser/robot/high risk 等结合。

## UI

Settings 在监控策略 tab 下增加 IP 质量采集区域。VPS 列表展示轻量状态 badge。VPS 详情加入 IP 质量 section，展示基础信息、风险因素、provider matrix、服务解锁和更新时间。Asset Decisions 在 evidence chips、member detail 和 comparison/readback 中展示 IP 质量证据。

## Rollout and Safety

默认关闭或低频启用，避免新版本立即产生大量外部请求。保留策略默认 normalized history 365 天、raw JSON 90 天。所有第三方请求使用固定 timeout 和 User-Agent，不保存 cookie/token/Authorization 请求头。未知字段保留在 sanitized raw JSON 中，不阻塞入库。
