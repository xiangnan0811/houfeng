# Design

## Root Cause

- `agent/ipquality` 默认 lookup URL 为 `https://ipapi.is/?q=self`，真实返回 HTML 首页，不是 JSON API。
- 默认 service unlock URL `https://ipapi.is/unlock/{service}` 真实返回 404 HTML，目前不能作为默认能力。
- agent due 判断只看 `LastSucceededAt`，lookup 持续失败时每个 heartbeat tick 都会再次执行采集，形成 5 秒一条 failure 报告。
- center read views 对所有 `ip_quality_reports` 做 VPS 归属，active link 模式下即使失败报告 IP 为 `0.0.0.0` 也会归属到 VPS 并成为 latest。

## Approach

- Agent lookup endpoint 改为 `https://api.ipapi.is`，省略 `q` 查询当前出口 IP；HTTP collector 继续支持测试/未来自定义 URL 注入。
- Collector 增强解析 ipapi.is 当前 JSON：top-level flags、`asn` object、`company` object、`location` object；保留现有扁平字段兼容。
- Collector 对非 JSON 响应返回简短错误，例如 `non_json_response: http status/content-type`，raw envelope 只保留合法 JSON；不把 HTML 当 raw JSON 存入 report。
- 默认 service unlock URL 为空。URL 为空时 collector 跳过 service probing；只有显式传入 `ServiceURL` 的测试/未来配置才执行服务探测。
- Agent due 判断改为按 `LastAttemptedAt` 节流，首次开启立即执行；成功时同时记录 `LastSucceededAt`，失败时也记录 `LastAttemptedAt` 和 `LastStatus`。
- Center 新增迁移 drop/recreate IP quality read views，`ip_quality_assigned_vps_reports` 只包含有效 IP fact：
  - `status in ('success','partial')`
  - `ip_address <> '0.0.0.0'`
  - `ip_version in (4,6)`
- 原始表不清理、不拒绝 failure；sync 入库路径保持诊断数据完整。

## Compatibility

- 旧 agent 继续可上报 failure；center read views 会隐藏失败占位报告。
- 已有失败历史保留，retention 仍按现有策略清理。
- API wire shape 不变；只改变哪些报告进入 latest/history。
- `partial` 的真实 IP 报告保留，所以 service unlock 不稳定不会遮蔽 IP 归属和 provider 风险。

## Risks

- 默认 service unlock 暂时为空，用户不会看到 Netflix/ChatGPT 等服务解锁结果；这是比错误 404 更安全的降级。
- 若 ipapi.is 后续字段变化，collector 仍会通过扁平 fallback 尽量解析；测试固定当前已验证结构。
