# Security review remediation

## Goal

修复外部安全审查报告中经当前代码验证真实存在的上线前安全问题，重点降低未认证入口 DoS、agent token 数据库泄露后的离线验证、诊断输出泄露、部署边界误配和安装链路供应链风险，同时确认已修复项没有回退。

## Requirements

- 审查来源：`/home/murray/.codex/attachments/0321052c-d788-43c2-9a35-e5734f908f6e/pasted-text-1.txt`，报告生成时间为 2026-06-26。
- 当前分支：`fix/security-review-remediation`。不得在 `main` / `master` 上提交、合并、重置或直接修改。
- 已确认真实存在的问题：
  - P1-01：`internal/center/http/handlers/auth.go` 和 `agent.go` 的登录 / agent 限流器使用无界 `map[string][]time.Time`，只清理当前 key，没有全局 key 上限或 sweep。
  - P1-02：`/api/agent/sync` 在 `decodeJSONLimited(..., AgentSyncBodyLimit)` 解析 4 MiB body 后才校验 sync token 是否为空 / 过长 / 格式明显无效。
  - P2-01：`internal/center/store/monitoring_instances.go` 和 `internal/center/enrollment/service.go` 对 enrollment/sync token 使用普通 SHA-256 存储 / 校验，缺少服务端 secret/pepper。
  - P2-02：agent 诊断命令 `stdout` / `stderr` 在 `agent/exec/runner.go` 和 center `store/sync_batches.go` 路径中按原文返回 / 持久化，未做 secret redaction。命令仍是白名单，不是任意命令执行问题。
  - P2-03：installer 只验证 release asset 的 `sha256sums.txt`，而 checksum manifest 与 binary 来自同一个 GitHub Release；release asset 可同时替换时 checksum 不提供额外信任。
  - P2/P3-04：`HOUFENG_TRUSTED_PROXIES` 只校验 CIDR 格式，未拒绝 `0.0.0.0/0` / `::/0`；生产 Host allowlist middleware 未实现。
  - P3-01：`writeJSON` JSON encode 失败时向客户端返回 `encode json: <detail>`。
- 已确认当前代码已经修复 / 有测试覆盖的问题：
  - P3-02：SPA handler 已通过 `filepath.EvalSymlinks` 和 `isPathWithinRoot` 限制 symlink 逃逸，`TestSPAHandlerDoesNotServeSymlinkEscapingWebDist` 覆盖该场景；本任务只保留验证，不重复重构。
- 实现必须保持现有 agent enrollment/sync JSON 协议兼容，不能要求已部署 agent 立即升级才能 sync。
- 修复 agent token hash 时必须提供旧 SHA-256 hash 的迁移路径，不能让已有实例全量失效。
- 高敏诊断命令的“默认关闭 / UI 二次确认 / 审计 / TTL”范围较大；本任务先完成可低风险落地的输出 redaction 和文档化最小权限风险，不在本次新增完整 RBAC/UI/审计/TTL 子系统。
- installer checksum 签名应优先使用 release workflow 可自动产出的 detached signature，并让 installer 默认验证签名；若验证工具缺失，安装必须失败而不是静默退回 checksum-only。
- 所有行为改动必须有失败优先的回归测试；最终至少运行 `make verify-go`。

## Acceptance Criteria

- [ ] P1-01：登录限流器和 agent endpoint 限流器有硬性 key 上限、过期 key sweep、全局请求预算；不同 username / forged trusted-proxy IP 不会让 map 无界增长。
- [ ] P1-02：`/api/agent/sync` 对缺失 / 明显非法 token 在读取大 JSON body 前拒绝；sync endpoint 有独立全局 inflight 限制，超限返回可重试的 503/agent error。
- [ ] P2-01：新生成 enrollment token 和 sync token 使用基于服务端 secret 的 HMAC-SHA256 存储；旧 SHA-256 hash 仍能验证并在成功使用后迁移为 HMAC hash；session HMAC secret 通过用途隔离派生 agent token key。
- [ ] P2-02：agent 上传前会 redact command stdout/stderr 中常见 secret；center 持久化前再次 redact，覆盖旧 agent 或第三方 agent；相关测试证明 token/password/authorization/private-key 等值不会进入 persisted `last_action`。
- [ ] P2-03：release workflow 生成并上传 `sha256sums.txt.minisig`；installer 下载 checksum manifest 签名并用固定 public key 验签后才校验 binary hash；文档说明生产固定版本 / digest 和签名验证要求。
- [ ] P2/P3-04：配置加载拒绝过宽 trusted proxy CIDR；当配置了 `HOUFENG_PUBLIC_BASE_URL` 时 Host 不匹配该 URL 的请求被拒绝；本地未配置 public base URL 的开发模式不被 Host allowlist 破坏。
- [ ] P3-01：`writeJSON` encode 失败时客户端只看到通用 `internal server error`，详细错误只进入服务端日志。
- [ ] P3-02：现有 symlink 边界防护测试继续通过。
- [ ] 文档更新覆盖生产部署基线：`HOUFENG_PUBLIC_BASE_URL`、trusted proxies、agent 最小权限、诊断输出、release 签名和生产版本 pinning。
- [ ] `make verify-go` 通过；如果只改 Go/docs/workflows/installer，不要求 `make verify-web`。

## Notes

- 外部报告中没有发现当前命令执行链路存在 shell injection；不要把本任务改成任意命令执行重构。
- 若 installer 签名工具选择在实现阶段发现不可用或会破坏支持矩阵，必须回到设计阶段重新确认方案，而不是降级为文档提示。
