# VPS IP 质量采集与决策证据实施计划

## Setup

- 在 `.worktree/vps-ip-quality` 的 `feature/vps-ip-quality` 分支开发。
- 已启用 `scripts/setup-git-hooks.sh`。
- 代码前读取 backend/web/guides 相关规范。
- 使用 TDD：先写失败测试，再实现。

## Implementation Checklist

1. Contract and Settings
   - 扩展 `internal/contracts/agentapi` 类型和 round-trip 测试。
   - 扩展 `internal/center/settings` 默认值、校验、store scan/upsert、HTTP settings request/response。
   - 扩展 `agent_plan` 构造与 handler plan conversion。

2. Database and Center Ingest
   - 新增 `0039_create_ip_quality_reports.sql`。
   - 新增 `internal/center/ipquality` 领域类型和 repository interface。
   - 新增 `internal/center/store/ip_quality.go`，实现保存报告、latest summary、VPS history 查询。
   - 扩展 `syncing.Batch`、`handlers.AgentSync`、`store/sync_batches.go`，在 sync 事务内保存 IP 质量报告。
   - 扩展 retention repository 删除过期 raw/history。

3. Agent Collector
   - 新增 `agent/ipquality` 包：plan due 判断、state store、collector、HTTP client/provider parser、service registry。
   - 扩展 agent config 默认 state path。
   - 扩展 runtime 注入 `IPQualityProvider`，后台 collect，sync tick drain reports。
   - 初始服务实现 Netflix、ChatGPT/OpenAI、YouTube Premium、Amazon Prime Video、Disney+、TikTok、Reddit；外部调用可测试替换。

4. Center APIs and VPS Summary
   - 新增 `handlers/ip_quality.go` 与 router VPS 子树 `ip-quality`。
   - Bootstrap wire `PostgresIPQualityRepository`。
   - VPS list/detail 增加 `ip_quality_summary`，避免详情页额外重复请求时也能展示 badge。

5. Asset Decisions
   - 扩展 `assetdecisions.Fact` 与 store `loadFacts`，引入 latest IP quality summary。
   - 新增 evidence kinds 和 scoring/comparison/readback 行为。
   - 失败/partial/ambiguous 不进入负面风险，只进入 evidence gap。

6. Web
   - 扩展 `web/src/lib/types.ts`、`api.ts` 和 API tests。
   - SettingsPage 表单加入 IP 质量设置。
   - VPSPage badge、VPSDetailPage section。
   - AssetDecisionsPage chips/detail 展示。

7. Review and Verification
   - 运行 targeted Go tests：contracts、settings、agent_plan、agent runtime/ipquality、sync ingest、ip_quality repo/handler、assetdecisions。
   - 运行 targeted Web tests：api、SettingsPage、VPSPage、VPSDetailPage、AssetDecisionsPage。
   - 运行 broader verification：`make verify-go` 和 `cd web && npm test -- --run` / 项目可用验证命令。
   - 自审 diff，修复发现的问题并复验。

## Validation Commands

- `go test ./internal/contracts/agentapi ./internal/center/settings ./internal/center/store ./internal/center/http/handlers ./internal/center/http ./internal/center/assetdecisions ./agent/...`
- `make verify-go`
- `cd web && npm test -- --run`
- `cd web && npm run build`

## Risk Points

- Agent 外部请求必须不阻塞 heartbeat。
- Sync request 可能变大，必须限制 raw JSON 和 report 数量。
- IP 归属 ambiguous 时不能错误影响资产决策。
- Settings 密钥类字段不得回显。
- Router 子树必须防止落入 SPA fallback。
