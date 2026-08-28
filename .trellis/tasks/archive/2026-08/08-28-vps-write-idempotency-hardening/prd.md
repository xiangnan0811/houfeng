# 收口 VPS 写入生命周期与幂等合同

## Goal

以 `v0.77.6` 发布提交 `da83a96769b618c6e223f71a1d2c6645d54c853b` 为基线，关闭全面复审中的两个 P2 与一个 P3，使 VPS 详情写入在路由卸载、Legacy/Overview 切换和 HTTP 未知结果下仍保持单一逻辑操作，并让 Go/TypeScript DTO mirror 显式区分 JSON 基础类型与日期格式。

## Background

- `VPSWriteOwnerStore` 当前由 `VPSDetailPage` 实例创建；离开 `/vps/:id` 后 store 生命周期结束，但普通 `fetch` 仍可继续，因此返回相同 VPS 可产生新的 owner 与新的幂等键。
- `VPSOverviewManagementActions` 当前使用独立 `submissionLockRef`，没有与 Legacy 共享同一 VPS 写 authority。
- `POST /api/vps/{vps_id}/experience-logs|services|domains|monitoring-instances` 当前不要求 `Idempotency-Key`，服务端成功提交但响应丢失后，客户端无法安全重试。
- scoped subscription DTO mirror 当前以 “nullable string 即 date” 推断格式；普通 `string | null` 会被错误分类为 date。
- 发布提交与正式 CI 已有绿色证据；本任务仍必须以当前分支的 RED/GREEN 和本地质量门禁作为修复证据。审查报告提到的既有 attachment PNG golden digest 本地差异不属于本任务改动范围，但若复现必须如实记录。

## Requirements

1. **P2-01 — owner 生命周期。** 唯一的 VPS write registry 必须由已认证 `AppShell` 持有，并以登录用户为生命周期边界；退出登录或切换用户后不得复用上一用户的 owner 或幂等尝试。
2. Legacy 与 Records v2 Overview 的全部 VPS 写入口必须通过同一 registry 获取 owner。同一 VPS 同时只允许一个写请求，不同 VPS 继续允许并行；精确 token 之外的旧 settle 不得释放当前 owner。
3. 公开 owner snapshot 至少包含 `vpsId`、`operation`、`token`、`viewToken`、`startedAt`；幂等 create 的 `requestDigest` 与 `idempotencyKey` 只能保存在 registry 私有 attempt state 与 exact-owner `prepareCreate` 返回值中，不得发布到 `useSyncExternalStore` snapshot。返回相同 VPS 或发生 Legacy/Overview gate 切换时，仍在途的 owner 必须显示“操作处理中”并阻止第二次写入。
4. 旧 view 的请求结算后，当前 `/vps/:id` view 必须重新读取 authoritative state；当前 view 自己已完成的正常刷新不得因 registry 提升而被旧 settle 覆盖。
5. **P2-02 — 未知结果重试。** 下列 VPS scoped create API 必须要求合法、调用方持有的 `Idempotency-Key`：
   - `POST /api/vps/{vps_id}/experience-logs`
   - `POST /api/vps/{vps_id}/services`
   - `POST /api/vps/{vps_id}/domains`
   - `POST /api/vps/{vps_id}/monitoring-instances`
6. 四个 create 的服务端合同必须为：same key + same normalized request digest 返回原记录且不新增行；same key + different digest 返回 `409 idempotency_key_reused`；缺失或非法 key 返回 `400 invalid_idempotency_key`，并且不得调用 create repository。
7. 资源记录与 idempotency receipt 必须在同一数据库事务内提交。receipt 必须绑定真实结果行并随结果永久保存到该结果被删除为止；不引入 TTL、janitor、缓存或尽力而为去重。
8. 客户端对 subscription、experience、service、domain、monitoring-instance create 统一执行：相同请求体在 transport failure 后复用原 key；请求体变化后生成新 key；收到 `idempotency_key_reused` 后轮换 key；服务端确认成功后清除已完成尝试，使未来相同内容仍可作为新的显式逻辑操作提交。
9. collection create API（`POST /api/services`、`POST /api/domains`、`POST /api/monitoring-instances`）的既有合同保持不变；本任务只收口复审点名的 VPS 详情写入链路。
10. **P3-01 — mirror 格式。** manifest、Go mirror 与 TypeScript mirror 必须分别表达 JSON 基础 `type`、可选 `format`、`required`、`nullable`；date 为 `type: string` + `format: date`，不得通过 nullable 猜测。
11. TypeScript 日期字段必须使用同源、被 mirror 严格验证的显式日期别名；普通 `string | null` 必须保持 `type: string`、无 date format。未知/扩宽日期别名、混合 primitive、缺失/null manifest 语义键继续 fail closed。
12. 保持现有 API 响应字段、VPS path authority、错误脱敏、Legacy lazy chunk 与 Records v2 capability gate 行为，不夹带无关重构。
13. 所有修复与 Trellis 工件先留在 `codex/vps-write-idempotency-hardening` worktree 中接受外部复审。外部复审通过并取得用户明确授权后，必须按逻辑批次提交，经 protected feature PR、required CI、main merge、Release Please release PR、发布产物与多架构镜像核验完成交付；最后归档任务、记录 journal，并清理/同步现场。

## Acceptance Criteria

- [x] 真实 AppShell/路由回归证明：Legacy subscription pending → Dashboard → 返回同一 VPS，第二次 POST 被阻止；旧请求 settle 后当前页面重新读取 authoritative state。
- [x] 路由回归证明：Legacy pending → Overview gate 激活，Overview 写入口仍被同一 owner 阻止；Overview pending → 离开页面 → 返回，也不会生成第二个逻辑操作。
- [x] registry 单元测试覆盖同 VPS互斥、不同 VPS 并行、exact-token release、用户 shell 生命周期、same-digest key 复用、changed-digest/409 key 轮换、confirmed-success 清理，并证明公开 snapshot 不含 raw body、digest 或 key。
- [x] 四个 VPS scoped frontend API 都发送调用方传入的 `Idempotency-Key`，Legacy create 提交都使用 registry 返回的稳定 key。
- [x] 四个真实 HTTP handler 各自覆盖 missing/invalid key、same-key replay、different-digest reuse；精确断言 `400/409` code、首次 `201`、replay `200` 与 repository 调用次数。
- [x] PostgreSQL 集成覆盖“事务已提交但响应未知后以相同 key 重试”，证明每一种 create 只存在一条结果记录并返回相同资源 ID；monitoring create 同时复用原 link。
- [x] 新 migration、migration registry、APP ACL current fragment 与迁移/权限测试一致；既有 released migrations 不被修改。
- [x] DTO contract 测试证明 nullable ordinary string 不再变成 date，显式日期别名映射为 `type: string` + `format: date`，Go/TS/manifest 四维加 format 完全一致。
- [x] Focused Web/Go/handler/store/migration tests、lint/type-check/build、Node 22 `make verify-web` 与相关 Chromium 路由场景通过；`make verify-go` 已执行且只复现任务外 attachment PNG golden 差异，已在最终证据中单独记录，未伪装为通过。
- [x] 外部复审通过后的交付链完整闭环：逻辑分批提交、feature PR required CI、protected main merge、post-merge main CI、Release Please release PR 合并、GitHub Release/签名 agent 与部署资产、多架构 Docker image 均已核验；任务归档和现场清理完成。

## Out of Scope

- 为通用 `fetch` 增加全局 AbortController、deadline 或页面卸载自动取消；这些不能替代跨路由 owner 与服务端幂等。
- 对 collection create API 扩大强制幂等合同。
- 修改已经发布的 migration、重写 VPS/Records v2 页面架构、引入全局状态库或新增后台清理 worker。
- 绕过 protected branch、required CI、Release Please 或发布工作流直接修改 main、伪造 tag/release，或在发布证据完成前清理唯一工作现场。
