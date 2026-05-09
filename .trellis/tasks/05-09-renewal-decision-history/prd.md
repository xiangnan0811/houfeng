# Renewal decision history and VPS timeline backend

## Goal

为 VPS Asset Ledger 增加第一批历史能力：记录续费决策变更历史，并提供 VPS timeline 后端接口，让后续真实资产复核、续费判断和前端决策页有可查询的审计轨迹。

## What I already know

* 用户要求继续依据 `houfeng_codex_下一步开发计划.md` 推进，并明确不使用 subagent。
* 计划文档的 Task 8 是“历史与决策增强”，原始范围包含 `renewal_decisions`、`price_histories`、`ip_histories`、`vps_spec_snapshots`、VPS timeline API 和决策页。
* Task 8 原始范围过大；当前批次应交付一个可验证后端纵向闭环，而不是一次实现所有历史与前端页面。
* 用户已暂不处理真实数据问题，因此本批不执行真实 VPS JSON dry-run/import。
* 任务开始时当前最大 migration 是 `0019_create_vps_node_links.sql`，本批新增 `0020_create_renewal_decisions.sql`。
* 当前 VPS 资产通过 `vps_assets.renewal_decision` 存储稳定英文机器值，合法值在 `internal/center/vpsassets/types.go` 中定义。
* 当前 `/api/vps/{vps_id}` PATCH 会直接更新 VPS 记录；本批需要在续费决策发生变化时追加历史记录。
* 资产层不得改写 Node / Target / Agent 语义；历史查询只能读取资产层数据和必要摘要。

## Scope

本 Task 收敛为 Task 8 的第一批后端闭环：

* 新增 `renewal_decisions` 历史表。
* 新增 `internal/center/renewals/` 领域类型、校验、repository interface。
* 新增 `internal/center/store/renewal_decisions.go` 仓库。
* 在 VPS PATCH 续费决策变化时自动记录一条决策历史。
* 新增 `GET /api/vps/{vps_id}/timeline`，返回该 VPS 的续费决策历史。
* 注册 router、bootstrap wiring，并补 handler/router/bootstrap/store/migration 测试。
* 更新 Trellis backend spec，记录 renewal decision history 的表、写入和边界约束。

## Requirements

* `renewal_decisions` 使用独立 history 表，不替代 `vps_assets.renewal_decision` 当前状态字段。
* 历史记录至少包含：
  * `decision_id`
  * `vps_id`
  * `from_decision`
  * `to_decision`
  * `reason`
  * `decided_at`
  * `created_at`
* `from_decision` 允许 `null`，用于未来导入或补录；本批自动记录 PATCH 时应写入变更前的决策值。
* `to_decision` 必须是现有 `vpsassets.RenewalDecision` 合法值。
* `reason` 可为空字符串，但不能为 SQL null；入口层 trim。
* `decided_at` 省略时由后端使用当前时间。
* PATCH 没有 `renewal_decision` 字段，或 PATCH 后决策值没有变化时，不得写历史。
* 记录历史与更新 VPS 当前状态必须在同一个数据库事务中完成，避免当前状态和历史漂移。
* timeline 第一版只返回续费决策历史，不返回价格/IP/规格历史占位假数据。
* timeline API 必须受现有 auth middleware 保护；agent route 不受影响。

## Acceptance Criteria

* [x] `db/migrations/0020_*` 创建 `renewal_decisions`，包含外键、枚举约束、索引与幂等写法。
* [x] `internal/center/renewals` 定义记录、创建输入、timeline DTO、sentinel errors 和校验。
* [x] store 可以创建决策历史、按 VPS 查询 timeline，并在 VPS PATCH 决策变更时用事务写入当前状态与历史。
* [x] PATCH `renewal_decision` 从 `keep` 改为 `cancel` 后，返回更新后的 VPS，且新增一条 `keep -> cancel` 历史。
* [x] PATCH 其他字段或把 `renewal_decision` 设置为原值时，不新增历史。
* [x] `GET /api/vps/{vps_id}/timeline` 返回 JSON snake_case，包含 `vps_id` 和 `renewal_decisions[]`。
* [x] 不存在 VPS 的 timeline 查询返回 404 或等价领域 not found 映射，不返回空假数据掩盖错误。
* [x] Router 不把 `/api/vps/{vps_id}/timeline` 落到 VPS item handler 或 SPA fallback。
* [x] Bootstrap 成功路径显式 wire timeline handler。
* [x] 本地验证通过：`git diff --check`、相关 Go 测试、`make verify-go`。

## Out of Scope

* 不实现 `price_histories`。
* 不实现 `ip_histories`。
* 不实现 `vps_spec_snapshots`。
* 不实现前端决策页。
* 不执行真实 VPS JSON dry-run/import。
* 不新增汇率换算、评分算法、provider API 自动同步或复杂 Agent 行为。
* 不修改 Node / Target / Agent 状态机，不把历史字段混入 Node 基础 record。

## Technical Notes

* 相关计划文档：`houfeng_codex_下一步开发计划.md` Task 8。
* 主要后端路径：
  * `db/migrations/0020_create_renewal_decisions.sql`
  * `internal/center/renewals/types.go`
  * `internal/center/store/renewal_decisions.go`
  * `internal/center/store/vps_assets.go`
  * `internal/center/http/handlers/vps.go`
  * `internal/center/http/router.go`
  * `cmd/houfeng-center/bootstrap.go`
* 当前模式参考：
  * `internal/center/vpsassets/types.go`
  * `internal/center/store/vps_assets.go`
  * `internal/center/http/handlers/vps.go`
  * `internal/center/assetlinks/types.go`
  * `internal/center/store/vps_node_links.go`
* 数据流：
  * HTTP PATCH `/api/vps/{vps_id}` decode + normalize + validate
  * handler 调用 VPS repository
  * store 在事务内读取当前 VPS、更新 `vps_assets.renewal_decision`、必要时 insert `renewal_decisions`
  * timeline handler 读取 `renewal_decisions`，返回稳定 JSON
* 已通过验证命令：
  * `git diff --check`
  * `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build go test ./internal/center/renewals -v`
  * `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build go test ./internal/center/store/migrate -v`
  * `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build go test ./internal/center/store -run 'TestPostgresRenewal|TestPostgresVPSAsset' -v`
  * `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build go test ./internal/center/http/handlers -run 'TestVPS' -v`
  * `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build go test ./internal/center/http -run 'TestRouterKeepsVPS|TestRouter' -v`
  * `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build go test ./cmd/houfeng-center -run TestBootstrapCenterBuildsAppOnSuccess -v`
  * `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build make verify-go`

## Definition of Done

* 代码实现与测试在 `feat/renewal-decision-history` 分支完成。
* Trellis task 上下文、spec 更新、archive 和 journal 按流程完成。
* 提交 feature branch，推送，创建 PR。
* 监控 PR CI，全部 green 后合并。
* 合并后同步本地 `main`，并确认主分支工作区干净。
