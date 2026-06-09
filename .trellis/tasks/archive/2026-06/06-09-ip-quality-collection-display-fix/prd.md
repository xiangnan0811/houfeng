# Fix IP quality collection and display

## Goal

修复 VPS IP 质量采集上线后的真实环境问题：agent 默认 lookup endpoint 返回 HTML 导致连续失败，center 将失败占位报告作为用户侧 IP 质量事实展示，VPS 详情/API/history 被 `0.0.0.0` 失败报告污染。

## Requirements

- Agent 默认应使用可返回 JSON 的 ipapi.is API endpoint，并能解析当前 ipapi.is 响应结构中的 IP、ASN、组织、坐标、使用地、风险/代理/IDC 等字段。
- Agent 遇到 lookup 失败、HTML、非 JSON 或上游错误时，应生成诊断性 failure 报告，但不得把 HTML 大段错误展示为用户侧事实。
- Agent 失败重试必须受 IP 质量采集周期约束，不能每个 5 秒 heartbeat 都重新采集并上报失败。
- 当前没有可靠 service unlock endpoint 时，默认不得请求不存在的 service URL；服务解锁字段保留，但默认空结果不能拖垮 IP lookup 报告。
- Center 可以继续保存失败报告作为诊断数据，但用户侧 VPS IP 质量 read model 只能展示有效 IP 事实。
- Center 用户侧 read model 必须隐藏 lookup 失败占位报告，尤其是 `status=failure` 或 `ip_address=0.0.0.0` 的报告。
- `status=partial` 且包含真实出口 IP 的报告应保留展示，用于展示 IP 归属/provider 风险；失败的服务解锁只影响服务维度。
- 已有失败刷屏历史不需要删除；迁移 read views 后，页面/API/资产决策应自动不再显示这些失败报告。
- 本任务不实现新的第三方服务解锁 provider；服务解锁恢复可后续单独处理。

## Acceptance Criteria

- [x] 默认 agent lookup 能对 `https://api.ipapi.is` 风格 JSON 生成成功 IP 质量报告。
- [x] HTML/非 JSON lookup 响应生成明确 failure，不泄露 HTML 到用户侧展示。
- [x] 默认配置不会调用不存在的 `unlock/{service}` endpoint。
- [x] 失败采集在 `frequency_seconds` 内不会被 heartbeat tick 反复触发。
- [x] Center sync 仍保存 failure 报告，但 VPS IP quality API 在只有 failure/`0.0.0.0` 报告时返回空 summary/latest/history。
- [x] Center 在较新的 failure 与较旧的有效报告并存时，用户侧展示较旧的有效报告。
- [x] Center 允许真实 IP 的 `partial` 报告进入 VPS latest/history。
- [x] 资产决策在没有有效 IP 质量报告时显示 `ip_quality_missing`，不产生风险、出口不一致或解锁受阻结论。
- [x] 相关 backend specs 已更新，测试覆盖 agent、center store/API、资产决策与 migration view。

## Notes

- 用户已选择“同任务全修”：agent 采集修复与 center 展示止血在同一任务完成。
