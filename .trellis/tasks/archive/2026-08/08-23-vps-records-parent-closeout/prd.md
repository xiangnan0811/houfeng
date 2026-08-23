# VPS 记录平台父任务收口与范围重基线

## Goal

在不修改产品代码、不启用永久删除、也不重开已归档 Child 11 的前提下，
把用户对运维记录永久删除的延期决策写入当前父任务权威工件，重新分类现存
UI/验证遗留，并形成可审计、不会把延期能力误报为已完成的父任务归档边界。

## Background and Confirmed Facts

- Houfeng 当前没有任何外部用户，只有用户本人部署的一个线上测试环境；其中
  数据可以随时整体删除或通过重建环境放弃。
- 用户于 2026-08-23 明确决定：运维记录永久删除退出当前 VPS 详情页重构
  范围，暂不考虑；未来出现真实需要时必须单独创建新任务，重新完成需求、
  设计、实现和验收，不得从本任务自动恢复旧计划。
- 原父任务 12 个功能 child 已全部归档并合入 protected main；Child 11 已归档，
  不得重开或扩张为永久删除补洞任务。
- 当前生产代码保持 `handlers.RecordDeletions(nil)`；Records、Portability 与永久
  删除相关配置默认关闭。缺少的 Markdown/Comparison deletion、Search/
  Collaboration/Portability recovery 和 production backup/restore pairing 是
  永久删除能力关闭的可解释原因，不再是当前父任务必须实现的交付项。
- 普通记录归档与恢复继续属于已交付生命周期；延期的是“单条记录跨在线存储、
  受管副本和官方备份重放的不复活永久删除”，不是禁止整体重建一次性测试环境。
- 原四项遗留中的 overview 管理操作已经关闭：PR #438 selected commit
  `7e9080f208a5f1f5cce7e563f5030b9d068629de`、merge commit
  `af23844adc82ce97e6815a3dbd8706f7fdab10e8`，7/7 checks 与合入后 main CI
  `32637395760` 成功，并发布于 `v0.75.0`；历史 task archive PR #440 合入为
  当前 `origin/main` `62f975c535f076ef7c322a07e25c4c158a9efe34`，合入后 main CI
  `32638216017` 成功。
- 其余三项已记录遗留保持延期：activity viewer 只接受 project digest，未扩展
  group-granted digest；comparison 390px 有具名滚动区但没有 sticky 行标题；
  4 GiB / 512 MiB mixed-load harness 未实现正式混合负载合同。三项都不阻止本轮
  归档，也都不得标记为已实现/已验证。
- 本轮独立复核的完整证据位于
  `../08-23-vps-records-final-audit-archive/research/final-audit-evidence-2026-08-23.md`。

## Requirements

### R1. Persist the product decision

- 更新原父任务当前权威 `prd.md`、`implement.md` 与最新 handoff，使它们一致表达：
  永久删除不属于当前范围、保持关闭、未来只通过全新任务重新评估。
- 原父任务仍须诚实记录生产 handler、默认 flags 和能力矩阵现状；不得把
  “已明确延期”改写成“已经实现”或“已经验证”。
- 2026-08-21 等历史审计保留为历史证据，只允许增加 current-pointer 或历史
  标记；不得回写、重开或伪造已归档 child 的实现证据。

### R2. Rebaseline parent completion semantics

- 功能交付计数继续区分“12/12 功能 child”与本次非功能收口 child；不得把
  Trellis 新增的收口节点误报为第 13 个产品能力。
- 永久删除相关七项 readiness 缺口、production pairing 和 nil HTTP handler 从
  “父任务归档 blocker”调整为“明确接受的延期能力边界”。
- 父任务归档后仍需保留清晰 future trigger：出现真实用户、合规删除承诺、
  长期受管备份、正式灾备或产品内单记录不可恢复删除需求时，新建独立任务。

### R3. Classify remaining UI and verification leftovers

- 对原四项非永久删除遗留逐项记录当前证据、用户影响、是否阻止父任务归档、
  处置方式和未来触发条件。
- 新 VPS overview 管理菜单的可见空操作由 child
  `08-23-vps-overview-management-actions` 修复；该 child 合入 protected main
  并通过合入后验证之前，原父任务不得归档。
- activity group-granted digest、comparison sticky 行标题和 4 GiB / 512 MiB
  mixed-load harness 明确接受延期，不阻止本轮归档；它们保持“未实现/未验证”，
  未来只有在权限范围扩大、移动端对比可用性成为实际问题或建立正式容量 SLO 时，
  才各自新建独立任务。
- 本任务只做范围与归档决策，不顺带修改 VPS 页面、activity 授权、comparison
  CSS 或性能脚本。
- 任何被延期的 UI/验证项都必须写为“未实现/未验证且已接受延期”，不能写成
  完成。

### R4. Preserve repository and delivery boundaries

- 不修改 Go/React 产品代码、迁移、配置默认值、部署文件、测试 fixture 或 CI。
- 不实现或注册 deletion/recovery/backup/restore adapter，不把
  `RecordDeletions(nil)` 改为生产服务。
- 所有修改在非 `main` 分支完成，通过 protected-main PR 交付；不直接修改或
  推送 local/remote `main`。

## Acceptance Criteria

- [x] `AC-01` 原父任务当前权威工件逐字一致表达用户的永久删除延期决定、未来
  新任务触发条件和“延期不等于已实现”。
- [x] `AC-02` 七项 readiness 缺口、production backup/restore pairing 与 nil HTTP
  handler 不再阻止父任务归档，同时继续作为永久删除保持关闭的可验证原因。
- [x] `AC-03` 普通归档/恢复、整体删除测试环境与单记录永久删除的语义边界清楚，
  不会让后续 agent 误删现有安全门禁或错误启用能力。
- [x] `AC-04` 四项 UI/验证遗留均有证据、处置和 future trigger；延期项明确标注
  未实现/未验证；overview 管理操作 child 在父任务归档前已合入 protected main。
- [x] `AC-05` 12/12 功能交付与收口 task 的 Trellis 计数分开表述，父任务最终
  状态、child 树和 handoff 无互相矛盾的进度数字。
- [x] `AC-06` 变更范围只包含 Trellis task/workspace 工件；产品、迁移、配置、
  部署与测试代码 diff 为零。
- [x] `AC-07` 收口任务与父任务通过 `task.py validate`、`git diff --check` 和
  文档一致性检查；完整 diff 经人工复核后才允许进入父任务归档流程。

## Out of Scope

- 实现、启用或测试运维记录永久删除。
- 补齐任何 deletion/recovery adapter 或 production backup/restore pairing。
- 修改 Records、Portability、Comparison 或永久删除配置默认值。
- 重开 Child 11，修改任一已归档 child 的完成状态，或重写其历史验收结论。
- 在本收口任务内修复 VPS overview、activity viewer、comparison CSS 或新增
  mixed-load harness。
- staging 部署、环境数据清理、数据库重建、Release Please 或镜像发布。

## Confirmed Disposition

- 用户于 2026-08-23 确认采用推荐方案：只修复可见的 overview 管理入口后再
  归档；activity group-granted digest、comparison sticky 行标题和 mixed-load
  harness 明确延期。
- Overview 入口现已通过 PR #438、post-merge main CI `32637395760` 和
  `v0.75.0` 完成该前置条件；其归档 PR #440 与 post-merge main CI
  `32638216017` 也已完成。最终 current-authority 文档由 PR #441 合入为
  `6e9be76e73783ba6867de2dabf9ab3edc24cf67b`；批准的有序归档由 PR #442 合入为
  `8615679cfccfc5ac00115c184ff3d67a94be5511`，PR/main CI 均 7/7 通过。
- 收口按两个 child 串行执行：先完成 overview 管理操作闭环，再完成最终审计、
  决策固化和父任务归档。第二个 child 不得在第一个 child 合入 protected main
  前启动。
- 原父任务和本收口树现均已归档为 `completed`。本收口节点是协调与审计节点，
  不计为第 13 个产品能力。
