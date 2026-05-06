# 全方位审查与早期问题修复

## Goal

对候风当前早期实现做一次全方位审查，找出会误导后续开发、阻断验证、偏离已冻结 V1 / v2 权威文档、或影响真实运行可信度的问题，并在本任务内修复高确定性、高收益、低争议的问题。较大的产品扩展、架构重设计、需要用户环境凭据的验证项，只登记为后续工作，不在本任务中无限扩张。

## What I Already Know

* 用户认为项目仍处于非常早期阶段，很多功能还不完善，但希望先通过一次全面审查及时修复问题，为后续开发打基础。
* 根 README 明确本仓库是 `候风 / Houfeng Fleet Control Plane` 的 V1 implementation repo；V1 业务结构权威在 `docs/design/v1-baseline/` 的 4 份 frozen 子集，视觉权威在 `docs/design/v2-houfeng/`。
* README 和 `CLAUDE.md` 都要求实现阶段不要重新设计 V1 一级能力；发现实现与 frozen baseline mismatch 时，先记录到 `docs/release/v1-gap-checklist.md`，再参考 `docs/release/next-phase-plan.md` 排优先级。
* `docs/release/next-phase-plan.md` 当前声明 Stage 1 完成判定已通过，可启动 Stage 2 brainstorm；同时也明确长 page 拆分、Telegram 真发等仍有 deferred / ops follow-up。
* `docs/release/v1-gap-checklist.md` 是当前 V1 设计意图和已完成度的双重权威，并包含历史重审结果和新增 gap 状态。
* 当前验证入口包括 `go test ./...`、`./scripts/verify.sh`、`cd web && npm run build`；Makefile 的 `verify-web` 实际会跑 `npm ci && npm run lint && npm run test -- --run && npm run build`。
* 仓库结构覆盖 Go center / Go agent / PostgreSQL migrations / React Vite web / docs / deploy / operations / release artifacts。
* 2026-05-06 baseline `./scripts/verify.sh` 结果：Go 全部通过，Web `npm run lint` 通过，Vitest 54 files / 394 tests 通过，但 `npm run build` 的 `tsc -b` 失败。
* 当前 build blocker 集中在：
  * 测试 fixture 和 mock 未补新增 `group` 字段，影响 `NodeRecord` / `TargetRecord` / `CreateTargetInput`。
  * `NodeLabelsAndNote` / `TargetLabelsAndNote` tests 未补 `groupDraft` / `onGroupDraftChange` props。
  * `SettingsRecord` / `SettingsUpdateInput` tests 未补新增 `feishu` 字段。
  * `TargetsPage.tsx` 的 DataTable `host` 列 render 返回了 `Element | null`，与列类型推导出的 `string` 不匹配。
* `.trellis/spec` 仍存在 stale 视觉验证指引：`web/component-conventions.md`、`web/styling-guidelines.md`、`web/quality-guidelines.md`、`backend/quality-guidelines.md` 仍指向 archived 的 `docs/operations/v1-visual-verification.md` / `docs/operations/visual-evidence/` 或旧 v1 baseline visual docs；README / CLAUDE 已改为 v2 视觉权威和 `docs/operations/*.jpg` 证据。这会误导后续代理，属于本轮必须修的流程/文档问题。
* docs/ops research 发现：
  * `CLAUDE.md` 与 `docs/deploy/local-and-systemd.md` 的 minimum center env 漏写代码实际要求的 `HOUFENG_INITIAL_USERNAME` / `HOUFENG_INITIAL_PASSWORD`，会导致首次启动失败。
  * `docs/operations/v1-smoke-run.md` 在可执行步骤中直接调用受保护 API，但没有先说明登录并复用 session cookie；同文后续证据表反而记录了 login/cookie 是必要步骤。
  * `docs/operations/v1-smoke-run.md` 的 visual evidence 行仍指向已 archive 的 `docs/operations/v1-visual-verification.md`。
  * `docs/release/next-phase-plan.md` 内对 Telegram 真发是否阻塞 Stage 1/V1 完成有措辞冲突；`v1-gap-checklist.md` 已允许 “Telegram delivery proof or an explicit note that Telegram is disabled for the deployment”，应优先保持两份 release 文档一致，不猜测真实凭据。

## Assumptions

* 本次“全方位”默认覆盖代码、测试、配置、构建验证、文档入口、release/gap 事实、部署说明、公开/AI 入口文档、以及前端可见体验，不仅是代码 review。
* 本次任务优先修复明确问题；如果发现需要大范围产品决策或 Stage 2 新功能定义的问题，只记录证据和建议，不直接扩展范围。
* 如果发现已知 deferred 项仍被文档写成 release 阻塞项或完成项，应优先修正文档事实，避免误导后续开发。

## Requirements

* 审查并修复 repo-wide 高确定性问题：
  * 构建、lint、typecheck、单测或验证脚本失败。
  * README / CLAUDE / release docs / deploy docs / operations docs 之间的事实不一致或过时状态。
  * V1 frozen 业务结构、v2 视觉权威、实现状态之间的明显 mismatch。
  * 后端、agent、web 中会造成真实运行错误、状态误报、数据流断裂或测试误导的问题。
  * 前端关键页面中明显破坏可用性、v2 规范一致性或中文高密度工程工具体验的问题。
  * 开发流程层面的明显问题，例如 verify 覆盖不一致、CI 与本地命令不一致、文档指向已 archive 权威。
* 对每个已修复问题保留可验证证据，优先通过测试、构建、静态检查或文档交叉引用来确认。
* 对不适合本任务修复的问题，记录为后续项，给出证据、影响和建议归属。
* 不引入 V1 之外的新一级能力，不把 Stage 2/MVP 范围混入本轮修复。
* 不猜测需要凭据或真实部署环境的事实，例如 Telegram 真发，只能检查代码路径和文档证据；真实凭据验证应保持 user-env-required。
* 首批必须修复项：
  * 修复 `./scripts/verify.sh` 暴露的 TypeScript build failures，使 Web build 恢复绿色。
  * 修复 `.trellis/spec` stale 视觉权威/截图流程引用，使后续开发代理加载到的规范与 README / CLAUDE / v2 设计权威一致。
  * 修复 deploy / AI entry / smoke docs 中关于 initial auth env、session cookie、当前 visual evidence 的事实错误。
  * 收敛 release docs 中 Telegram gate 的措辞：不要求在无凭据环境猜测或伪造真发证据；明确真发证据或禁用说明的接受条件。

## Acceptance Criteria

* [x] 有一份明确的问题清单，覆盖代码、文档、配置/验证、运行/部署、前端体验等主要面向。见 `research/backend-agent-audit.md`、`research/docs-ops-audit.md`、`research/web-ui-audit.md`。
* [x] 高确定性问题已在本任务内修复，并能用具体命令或文件证据验证。修复范围包括 node actions router dispatch、Web TS fixture/build blocker、Events backfill 控件真实状态、active docs/spec truth drift。
* [x] 不能或不应在本任务修复的问题已记录到合适文档，且不会被误写成已完成。见 `docs/release/v1-gap-checklist.md` 的 comprehensive audit follow-up rows #21-#24。
* [x] `go test ./...` 通过。2026-05-06 运行：`TMPDIR=/tmp GOTMPDIR=/tmp/houfeng-go-build GOCACHE=/tmp/houfeng-gocache go test ./...`。
* [x] `./scripts/verify.sh` 通过。2026-05-06 运行：`TMPDIR=/tmp GOTMPDIR=/tmp/houfeng-go-build GOCACHE=/tmp/houfeng-gocache ./scripts/verify.sh`，覆盖 Go、web lint、Vitest 54 files / 395 tests、web build。
* [x] `cd web && npm run build` 通过。完整 `./scripts/verify.sh` 已包含 `npm run build`；单独 `cd web && npm run build` 也在修复后通过。
* [x] `.trellis/spec` 中 active guidelines 不再要求使用已 archive 的 v1 视觉验证路径作为当前流程。active visual authority 已改为 `docs/design/v2-houfeng/{design-language.md,component-spec.md}`，一次性 v2 截图证据为 `docs/operations/*.jpg`。
* [x] 部署文档和 AI 入口清楚列出首次启动需要的 initial user seed env。见 `CLAUDE.md` 与 `docs/deploy/local-and-systemd.md`。
* [x] Fresh-install smoke run 的受保护 API 示例包含登录/session cookie 前置步骤。见 `docs/operations/v1-smoke-run.md` Step 0 与后续 `curl -b "$COOKIE_JAR"` 示例。
* [x] 相关 Trellis spec 是否需要更新已评估。已更新 backend/web quality、directory、state/data、styling/component specs，并新增 router subtree dispatch 防回归规则。

## Completion Evidence

* `git diff --check` 通过。
* `TMPDIR=/tmp GOTMPDIR=/tmp/houfeng-go-build GOCACHE=/tmp/houfeng-gocache go test ./...` 通过。
* `TMPDIR=/tmp GOTMPDIR=/tmp/houfeng-go-build GOCACHE=/tmp/houfeng-gocache ./scripts/verify.sh` 通过；本机 Node 为 v24.14.1，会对 `web/package.json` 的 Node 22.x engine 要求输出 `EBADENGINE` warning，但 lint/test/build 均通过。

## Definition of Done

* Tests / lint / typecheck / build 按风险面运行并记录结果。
* 修复尽量小步、可回滚，不做无关重构。
* 文档事实与当前代码、release gate、已知 deferred 项保持一致。
* 若产生新长期约束，更新 `.trellis/spec/`。
* 完成后按 Trellis 流程提交工作改动，并提醒运行 finish-work。

## Out of Scope

* 定义 Stage 2 / MVP 具体产品范围。
* 引入新探针类型、新通知通道、新存储后端、多用户权限体系、Docker/脚本执行等 V1 外能力。
* 需要用户私有凭据或真实生产环境才能完成的 Telegram 真发、部署上线、外部域名等验证。
* 大规模 UI 重设计或长页面系统性拆分，除非审查发现有明确 blocker。

## Technical Notes

* Current task: `.trellis/tasks/05-06-comprehensive-audit-repair`
* Relevant specs: `.trellis/spec/backend/index.md`, `.trellis/spec/backend/*.md`, `.trellis/spec/web/index.md`, `.trellis/spec/web/*.md`, `.trellis/spec/guides/*.md`
* Important repo entry docs: `README.md`, `CLAUDE.md`, `docs/release/next-phase-plan.md`, `docs/release/v1-gap-checklist.md`, `docs/release/docs-audit.md`
* Important verification commands: `go test ./...`, `./scripts/verify.sh`, `cd web && npm run build`
