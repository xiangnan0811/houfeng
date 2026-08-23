# VPS 记录平台父任务收口执行计划

## Goal

先关闭新版 VPS overview 的可见管理空操作，再固化永久删除及其他遗留的延期
决定，完成跨任务审计并归档原父任务。

## Execution rules

- 本任务是协调父任务，不直接实施产品代码。
- 只启动当前获批且依赖已满足的 child；不得把两个 child 并行推进。
- 所有代码和文档变更都在非 main 分支完成，通过 protected-main PR 集成。
- 原父任务在全部门禁满足前保持 `planning`。
- `12/12` 始终表示原计划功能 child；本收口树不增加产品能力计数。

## Phase 1: 完成规划并取得实现批准

- [x] 校验本任务以及两个 child 的 PRD、设计与执行计划。
- [x] 向用户提交完整规划摘要，明确范围、延期项、风险和归档门禁。
- [x] 用户已明确批准实施并启动
  `08-23-vps-overview-management-actions`；不得从规划批准推断代码执行批准。

## Phase 2: Overview 管理操作闭环

- [x] 从当时最新 `origin/main` 建立合规非 main 工作位置并启用仓库 hooks。
- [x] 按 child 的 TDD 计划先建立管理菜单五类动作的失败测试。
- [x] 复用/抽取 Legacy 已有表单、转换器、校验和 API 合同，不复制领域语义，
  不把整个 `LegacyVPSDetail` 挂入 overview。
- [x] 完成 facts、decision、subscription、cancellation、archive 的真实交互，
  写后刷新 overview；取消/归档保留原安全门禁。
- [x] 运行 focused Vitest、type-check/lint、web verify、桌面与 390px 浏览器验证、
  键盘/焦点/Axe 检查和相关全量门禁。
- [x] 独立复核完整 diff，经 PR 合入 protected main，等待 required CI 与合入后
  main 验证通过。
- [x] 将 child 的提交、PR、CI 与验证证据写回其当前工件并归档该 child。

Phase 2 evidence: PR #438 selected
`7e9080f208a5f1f5cce7e563f5030b9d068629de`, merged as
`af23844adc82ce97e6815a3dbd8706f7fdab10e8`, passed 7/7 checks and post-merge
main CI `32637395760`, then shipped in `v0.75.0`. Archive PR #440 merged as
current main `62f975c535f076ef7c322a07e25c4c158a9efe34`; post-merge main CI
`32638216017` succeeded.

## Phase 3: 最终审计与文档集成

- [x] 确认 Phase 2 的 selected commit 已在 protected main；否则停止。
- [x] 启动 `08-23-vps-records-final-audit-archive`，重新核对代码、测试、Trellis
  child 树、local/remote refs、PR/CI 和未提交状态。
- [x] 同步更新原父任务当前 `prd.md`、`implement.md` 和最新 handoff：
  - 永久删除退出当前范围，保持关闭，未来新建独立任务；
  - readiness 缺口、production pairing 与 nil handler 是延期边界证据；
  - activity digest、sticky 行标题、mixed-load harness 未实现/未验证且已延期；
  - overview 管理操作以 protected-main 证据标记为关闭；
  - `12/12` 功能交付与收口 task tree 分开计数。
- [x] 保持已归档 child 历史工件不变，只更新 current authority/pointer。
- [x] 运行 Trellis validate、链接/引用检查、`git diff --check` 和完整 diff 复核。
- [x] 通过独立文档 PR 合入 protected main，并确认 required/main CI。

Phase 3 delivery evidence: PR #441 selected
`290a5c6d08c980f1c9829312722a7884b69c4d7b`, passed 7/7 required checks, and
merged as `6e9be76e73783ba6867de2dabf9ab3edc24cf67b`; post-merge main CI run
`32640659843` passed 7/7.

## Phase 4: 归档顺序与交接

- [x] 在 protected main 事实完整后归档最终审计 child。
- [x] 校验本收口任务两个 child 均完成，再归档本收口任务。
- [x] 最后归档原父任务 `07-13-vps-detail-experience-design`。
- [x] 发布最终交接摘要：完成证据、明确延期、future triggers、当前安全默认值，
  以及任何未清理但不影响交付的本地 branch/worktree 状态。

Phase 4 evidence: archive PR #442 selected
`36d2f80836ea0b3402ff6e7868a73c9a98fe316b`, passed 7/7 required checks in
run `32641264007`, and merged as
`8615679cfccfc5ac00115c184ff3d67a94be5511`. Post-merge main CI run
`32641517555` passed 7/7.

## Stop conditions

- overview 产品改动尚未合入 protected main；
- 五类管理动作任一仍是占位、缺少错误反馈或写后未刷新；
- 取消/归档缺少 preview、blocker、确认或导航安全合同；
- 文档把延期能力误写成已实现，或把收口节点算作第 13 个产品能力；
- Trellis、refs、PR/CI 或实际代码事实互相矛盾。
