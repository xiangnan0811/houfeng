# VPS 记录平台最终审计与归档执行计划

## Goal

用当前 protected-main 事实固化范围决定，完成文档一致性 PR，并安全归档收口树
与原父任务。

## Phase 0: Entry gate

- [x] 确认 overview 管理 child 已归档，记录其 selected commit、PR URL、required
  CI、merge commit/main inclusion 和合入后验证。
- [x] 现场检查五类 action wiring、相关 tests 和 legacy fallback；不只读取 handoff。
- [x] 检查当前 branch/worktree、local/remote refs、dirty/untracked 状态和用户改动。
- [x] Entry gate 事实完整；未执行 archive。活动状态由根会话启动，本次只按要求
  补充 branch metadata。

Evidence: PR #438 selected `7e9080f208a5f1f5cce7e563f5030b9d068629de`,
merged as `af23844adc82ce97e6815a3dbd8706f7fdab10e8`, 7/7 checks and post-merge
main CI `32637395760` successful, released in `v0.75.0`. Archive PR #440 merged
as current main `62f975c535f076ef7c322a07e25c4c158a9efe34`; post-merge main CI
`32638216017` succeeded. Full audit:
`research/final-audit-evidence-2026-08-23.md`.

## Phase 1: Permanent-delete boundary audit

- [x] 检查 production bootstrap 仍未注册 RecordDeletions service/HTTP handler。
- [x] 检查 Records、Portability、Comparison/永久删除相关默认 flags 未被意外开启。
- [x] 重新列出 readiness matrix 未满足能力和 production backup/restore pairing，
  只作为 fail-closed 证据，不转为实现任务。
- [x] 确认普通记录归档/恢复仍可用，并与整体测试环境重建、单记录永久删除分开。

## Phase 2: Cross-task reconciliation

- [x] 核对原父任务 12 个功能 child 的 archived path、selected commit、main inclusion
  和当前 handoff；不更改历史工件。
- [x] 核对本收口父任务两个 child 的依赖与状态，保持“12 功能 + 收口协调”计数。
- [x] 现场确认 activity group digest、sticky row headers、mixed-load harness 仍是
  未实现/未验证，记录精确代码/测试证据和 future trigger。
- [x] 未发现阻止 current-authority 更新的事实冲突。

## Phase 3: Current authority update

- [x] 在原父任务 `prd.md` 中把永久删除移出当前范围，加入用户决定、语义边界和
  future triggers。
- [x] 在原父任务 `implement.md` 中更新 authority/current state、overview child
  交付证据、三项延期遗留和最终 `12/12` 计数。
- [x] 更新 `../07-13-vps-detail-experience-design/research/handoff-2026-08-23.md`
  为最终 current handoff，区分：
  - 已完成并验证；
  - 未实现/未验证且已延期；
  - future trigger；
  - 永久删除为何仍 fail-closed。
- [x] 更新本收口任务/child 的执行证据和 current pointer；不编辑 archived child。

## Phase 4: Validation and protected delivery

- [x] 逐个运行原父任务、本收口任务及两个 child 的 `task.py validate`。
- [x] 检查文档链接、task tree、路径、状态、日期、commit/PR/CI 引用与进度数字。
- [x] 运行 `git diff --check`，确认产品、迁移、配置、部署、测试和 CI diff 为零。
- [x] 人工复核完整 diff，确认没有把延期写成实现或删除安全门禁。
- [ ] 独立审查后提交非 main 分支、push、创建文档 PR并等待 required CI。
- [ ] 合入 protected main 后确认 main CI/文档检查和 selected commit inclusion。

## Phase 5: Archive sequence

- [ ] 在非 main 受保护流程中归档本 final-audit child，并 validate 状态/路径。
- [ ] 确认 overview 与 final-audit 两个 child 都完成，归档
  `08-23-vps-records-parent-closeout`，再 validate。
- [ ] 最后归档 `07-13-vps-detail-experience-design`，确认其 current authority 和
  child tree 随归档保留。
- [ ] 将归档 metadata/path 改动通过 PR 合入 protected main；确认 required/main CI。
- [ ] 输出最终交接：12/12 功能交付、overview 收口证据、明确延期、future triggers、
  永久删除关闭状态和本地未清理资源。

## Stop conditions

- overview child 未在 protected main 或 required/main CI 未通过；
- 实际代码与 handoff/PR 证据不一致；
- 永久删除 handler/flags 被启用或 readiness 结论不再成立；
- 产品/测试/配置 diff 混入文档 child；
- archived child 历史工件出现改动；
- 归档命令会覆盖用户 dirty state 或要求直接修改 local/remote main。
