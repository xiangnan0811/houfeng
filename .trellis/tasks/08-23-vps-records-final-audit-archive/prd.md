# VPS 记录平台最终审计与父任务归档

## Goal

在 overview 管理操作闭环已合入 protected main 并完成合入后验证后，基于实际
代码、测试、Trellis、Git、PR/CI 证据固化最终范围决定，归档本收口树和原 VPS
详情/记录平台父任务。

## Entry Gate

- `08-23-vps-overview-management-actions` 已完成并归档。
- 其 selected commit 已在 protected main；required PR CI 和合入后 main CI/
  必要验证均有当前证据。
- 五类 overview 管理动作不再是占位，legacy fallback 与生命周期安全合同通过。
- 若任一条件不满足，本任务保持 `planning`，不得启动审计或归档。

## Requirements

### R1. Reconcile current facts

- 重新读取当前代码与配置，确认永久删除仍保持关闭：production handler 未注册，
  相关 flags 默认关闭，缺失 readiness capabilities 未被误接线。
- 核对 overview 管理 child 的实际代码、测试、提交、PR、required CI、main CI 和
  未提交状态，不只引用 child 自报完成。
- 核对原父任务 12 个功能 child 的归档/合入事实以及本收口树两个 child 的状态。

### R2. Update current authority consistently

- 同步更新原父任务当前 `prd.md`、`implement.md` 与
  `../07-13-vps-detail-experience-design/research/handoff-2026-08-23.md`，使用同一口径记录：
  - 用户放弃当前永久删除方案，未来需求必须新建独立任务；
  - 七项 readiness 缺口、production pairing 和 nil handler 是关闭边界证据，
    不再是当前归档 blocker，也不代表已实现；
  - overview 管理入口已以 protected-main 证据关闭；
  - activity group-granted digest、comparison sticky 行标题和 mixed-load harness
    未实现/未验证且明确延期；
  - `12/12` 是功能 child，收口节点不计为第 13 个产品能力。
- 已归档 child 工件保持历史原样，不回写其当时的验收结论。

### R3. Preserve future triggers

- 永久删除 future trigger：真实外部用户、合规删除承诺、长期受管备份、正式灾备
  或产品内单记录不可恢复删除需求。
- activity digest future trigger：viewer 权限需要超出 project digest。
- sticky headers future trigger：390px 对比出现实际定位/可用性问题。
- mixed-load future trigger：定义正式容量 SLO、目标硬件或持续回归基准。
- 任何 future trigger 被满足后都创建独立 Trellis task，不重开历史 child。

### R4. Archive safely

- 所有文档更改通过非 main 分支和 protected-main PR 集成。
- 先在 PR 中完成 current authority 更新和一致性验证；只有 protected main 已包含
  最终事实后，才按“最终审计 child → 收口父任务 → 原父任务”顺序归档。
- 归档前后记录 task tree、状态、路径和 commit；发现矛盾立即停止。

## Acceptance Criteria

- [x] `AC-01` Entry Gate 有 selected commit、PR、required CI 和 main 验证证据，且
  现场代码确认五类管理动作已真实接线。
- [x] `AC-02` 原父任务三个 current authority 工件对永久删除决定、三项延期遗留、
  future triggers 和计数口径无矛盾。
- [x] `AC-03` 永久删除仍是未实现/未启用；readiness 缺口继续可见但不再阻止
  当前父任务归档。
- [x] `AC-04` 普通归档/恢复、整体重建测试环境、单记录永久删除三种语义明确。
- [x] `AC-05` 已归档 child 历史工件 diff 为零；产品代码、迁移、配置、部署、测试
  与 CI diff 为零。
- [x] `AC-06` 原父任务、本收口任务及两个 child 的状态、路径、child 树和
  `12/12` 功能交付数字一致。
- [x] `AC-07` 所有相关 task 通过 `task.py validate`，文档引用、
  `git diff --check` 与完整 diff 人工复核通过。
- [ ] `AC-08` 文档 PR required/main CI 通过，随后按批准顺序完成归档并留下可恢复
  的 Git 提交证据。

## Out of Scope

- 任何产品代码、测试、迁移、配置、部署或 CI 修改。
- 实现/启用永久删除或补齐 deletion/recovery/backup adapters。
- 实现 activity group digest、sticky headers 或 mixed-load harness。
- 重开、重写或更改任一已归档功能 child 的历史完成状态。
- 清理线上测试环境数据、数据库或对象存储。

## Dependencies

- 父任务：`08-23-vps-records-parent-closeout`。
- 硬依赖：`08-23-vps-overview-management-actions` 在 protected main 完成交付。
