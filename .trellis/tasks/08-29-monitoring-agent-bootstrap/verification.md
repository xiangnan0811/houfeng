# Verification — 首次监控 Agent 接入死锁

日期：2026-08-29

## 实现证据

- 后端稳定 action ID `open_monitoring_instances` 保持不变，仅把 `monitoring.unlinked.v1` 的用户文案修正为“创建并接入 agent”。
- Overview 精确区分未关联异常的创建/onboarding 与既有关联的只读证据，并以权威 VPS detail 执行 0/1/多 active links 分流。
- 创建复用现有 VPS-scoped API、共享 `VPSWriteOwnerStore`、幂等 attempt 与 onboarding URL；没有新增 endpoint、DTO、schema、migration 或 agent 协议。
- Monitoring 页头与首次空状态统一进入 `/vps?view=unlinked`；未关联库存选择 VPS 后用 `workbench=monitoring` 打开同一 Overview owner。
- 任何同 VPS 写 owner 都会禁用创建提交；只有当前精确 `monitoring-create` owner 会显示提交进度、锁定 modal 并禁用取消。

## 最终命令证据

- `GOTOOLCHAIN=go1.26.2 make verify-go`：通过；使用 `go.mod` / CI 固定的项目工具链，包含附件 PNG golden 与全部 Go package、fmt、vet、test。
- Node 22.23.1 `make verify-web`：通过；206 个测试文件、1622/1622 测试，coverage、ESLint、strict TypeScript/Vite build、bundle 与 CSS budgets 全部通过。
- Node 22.23.1 `npm --prefix web run test:e2e`：通过；Chromium 136/136，包括首次创建、Monitoring → 未关联 VPS → workbench、390px overflow/axe、关系只读与 fail-closed 目的地。
- `git diff --check`：通过；index 为空，没有 staged 文件。
- 隐私/范围审查：没有新增日志或 UI 暴露 agent secret、幂等键值、digest 或原始敏感请求体；没有新增 CSS、API/DTO/数据库/协议边界。
- 各轮独立规格、异步安全、测试/交付审查发现的问题均已修复并复验：archived scope 空态、关闭/卸载 authority、稳定 ID allowlist、persistent 显式关闭、外部 owner 提交禁用、后继 owner 抢占旧 re-probe 的确定性交错回归与浏览器 wire enum。

## 工具链与现有 advisory

- 本机默认 Go 1.27.0-X 不属于项目支持工具链，曾产生附件 PNG digest 差异；在项目固定 Go 1.26.2 下全量 Go 门禁通过，本任务未修改附件代码或 golden。
- `npm ci` 报告现有 5 个 high severity audit advisory；仓库 Web 质量门仍以退出码 0 完成，本任务未改变依赖或 lockfile。

## 交付边界

- 分支：`fix/monitoring-agent-bootstrap`。
- 工作树：`/home/murray/code/houfeng/.worktree/monitoring-agent-bootstrap`。
- 最终审查阶段仍保持 dirty：未 stage、未 commit、未 push、未创建 PR、未发布；用户已授权审查通过后按项目规范继续完整交付。
- `.trellis/spec/web/state-and-data.md` 已覆盖两个 monitoring workbench 深链、0/1/多 active-link 分流、API 与测试点；本任务没有产生需要重复写入的新 code-spec 合同。
