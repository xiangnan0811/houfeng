# 记录、修订、草稿与状态核心

## Goal

实现稳定记录根、不可变完整修订、作者私有服务端草稿、类型化状态、生命周期、并发保存和记录永久删除核心，为后续材料、搜索、活动、协作与可移植性子任务提供可执行合同。

## 2026-08-03 Development Rebaseline

- 本任务是 Child 1 关闭后的首个功能实现，拥有 root migration `0052_create_records_core.sql`。
- 只支持 fresh/current development database 与 exact repeat；项目目前没有用户或部署，不建设旧库升级、混合版本或 staged cutover。
- 旧 `experience_logs` 不迁移、不回填、不双写，新 Records 合同不读取它；现有旧功能可继续存在但不是本任务的兼容数据源。
- `0052` 必须在同一 PR 登记 current APP ACL managed objects、privileges 与 admission tests。PR 合并前可随本任务发现继续修正；三检查点未全部闭合前不得把它视为已发布不可变 migration。
- Child 1 的 protected-main 合入、post-merge CI 和本任务的 Go/Web baseline 已验证通过。

## Delivery Controls

- 使用一个 Trellis Child 2 任务、一个分支 `codex/vps-records-core`、一个 worktree `.worktree/vps-records-core`，不新建孙任务。
- inline 实施；不使用巨型 goal，也不启动无边界总控循环。只有确有独立工作且用户明确同意时才另行使用子智能体。
- 分三个硬检查点推进，每个检查点分别提交、验证并向用户报告：
  1. `0052` schema、current APP ACL 集成、领域合同；
  2. revision、draft 与 API 行为；
  3. permanent deletion、Web transport 与完整验收。
- 第一个实现检查点形成可审查提交后创建早期 Draft PR；三个检查点整体一致且 required CI 通过前不合并。
- 进度以可验收合同、实现闭环、测试证据和未决风险计算，不以运行时长或代码量替代。

## Requirements

### 1. Record and revision authority

- 父设计权威范围：`../07-13-vps-detail-experience-design/design.md` §9-§11、§15、§19-§22；2026-08-02 development rebaseline 优先于其中历史升级/切换描述。
- 使用稳定 record root、不可变完整 revision 与可重建 current projection。
- 任一 revision 独立保存 title、Markdown、方言版本、类型/业务状态/统一状态组、影响、发生/完成时间、可见范围、primary/related subject、标签、负责人、参与者、跟进、template provenance、作者、保存原因和 canonical hash。
- `record_revision_participants` 独立保存修订时参与者与身份快照；不得把 participant IDs 数组塞入 `record_revisions`。
- 正式保存使用 `base_revision_id`、record lock version、`Idempotency-Key` 和 CAS。无变化保存返回当前 revision 且 `created=false`；恢复旧版复制为新 revision，绝不覆盖历史。
- 文档生命周期只有 `active|archived`；草稿和永久删除均不是 lifecycle 值。

### 2. Subject and authorization contracts

- 初始 subject kinds 固定为 `vps|monitoring_instance|target`；每条 revision 恰有一个 primary subject，可有多个 related subject。
- 初始 relation roles 固定为 `affected|context|evidence_source`，并通过版本化 registry 校验 kind/role 组合、顺序和重复项。
- 三类 source adapter 必须从当前权威 repository 解析稳定 ID、project、权限 scope、权限 revision、显示身份快照与可空 live route；客户端不得提交可信快照或任意 ACL 字符串。
- revision 固化 capture authorization 与当时身份快照。live source 每次读取/保存都与 current scope 取交集；来源删除后保留快照、断开 live route，并只接受 full-witness `authorization_floor_snapshot` 的 tombstoned union。
- 来源不存在、跨项目、current scope 变宽、floor 缺失/未知、kind/role 未登记均 fail closed；来源删除不得 cascade record/revision。
- 所有 Records handlers 复用当前 session middleware、actor scope 与 `recordauth.Policy`；权限拒绝和不存在对外统一为无泄露 404。

### 3. Drafts and conflicts

- 新建和编辑都先形成作者私有 server draft；浏览器缓冲不是权威。
- 草稿保存完整输入、独立 ETag 与 `base_revision_id`；PATCH 使用 `If-Match`。
- 恢复点表统一命名为 `record_draft_checkpoints`。内容变化时最多每 5 分钟一个，保留最近 20 个且不超过 7 天；草稿默认 90 天清理并提供提前 7 天提示合同。
- server current 已推进或 ETag 过期时返回稳定 conflict DTO，包含 server revision、draft/local snapshot 和字段/Markdown diff inputs；不得 last-write-wins。
- 草稿不进入 search、activity、notification 或正式 outbox；正式保存或明确丢弃后结束草稿。

### 4. Type, status, activity, and transport seams

- builtin type registry 固定 `troubleshooting|maintenance|migration|provider_communication|billing|important_finding|note`，状态与统一 group 遵守父设计；无状态类型不生成空状态。
- template registry 使用版本化 Markdown template；切换类型只给出建议/diff，未经用户明确操作不覆盖正文。
- 本任务拥有 `record_domain_activities` append-only 写入 seam，并在正式业务事务内写记录/修订/归档/恢复领域事实。
- 后续“活动投影、单主体页面与 VPS 概览”子任务拥有 canonical `record_activities` 读投影、合流水位、分页与 UI；本任务不得预建第二套活动读模型。
- 提供版本化 record/draft/revision/deletion DTO 与 HTTP transport。canonical Web 类型追加到 `web/src/lib/types.ts`；lazy Records transport 位于 `web/src/lib/recordsApi.ts`，不得被 AppShell/TopBar/Sidebar 或 eager `api.ts` 导入。
- 本任务交付 transport contract，不交付 Records 页面、编辑器或完整 UI。

### 5. Permanent-delete fail-closed core

- 实现 permanent-delete preview、operation orchestration 和 core purge adapter，复用 Child 1 reservation、ledger、witness、lease 与 fence primitives。
- core adapter 只拥有 root、revisions、subjects、tags、participants、drafts、checkpoints、domain activities/current projection 与无正文 core purge receipt。
- evidence、attachment、search、activity read projection、collaboration、export/import 与 managed-client adapters 由其后续 owner child 注册。
- 只要完整 adapter set、健康证明、backup/processor inventory 或 witness 任一未就绪，permanent-delete capability 就保持关闭；不得以“当前表为空”绕过 readiness。
- reservation 后 core 新读/新写命中必须为 0；delete/outcome 结果未知持续 fail closed；`attempt_not_committed` durable 后才可释放 provisional fence。

## Acceptance Criteria

### Checkpoint 1: schema, ACL, domain contracts

- [ ] `0052` fresh/repeat migration创建全部 core objects、约束和索引；命名只使用 `record_draft_checkpoints`，参与者只使用独立关系表。
- [ ] current APP ACL fragment完整登记 `0052` 新对象及 center runtime/platform admin 精确权限；source、convergence、runtime admission 和真实 PostgreSQL tests 通过。
- [ ] revision、subject/relation、type/status/template、authorization snapshot/tombstone contracts 有 table/compile tests；三类初始 source adapter 合同明确且未知值 fail closed。

### Checkpoint 2: revision, draft, API behavior

- [ ] revision 1、后续保存、无变化保存、幂等重试、CAS conflict、restore old revision 均有 unit 和真实 PostgreSQL tests。
- [ ] transaction 原子写 revision/relations/current pointer/current projection/`record_domain_activities`/registered participants/outbox，任一步失败均不留下半份 revision。
- [ ] draft author isolation、ETag conflict、base revision conflict、checkpoint retention、TTL/cleanup 和 publish/discard 有稳定 tests；草稿不产生正式副作用。
- [ ] `/api/records`、record/draft/revision/archive endpoints 受统一 auth middleware、source ACL、reservation fence 和 response allowlist 保护；404/409/413/422/503 行为固定。

### Checkpoint 3: deletion, Web transport, full acceptance

- [ ] preview 后撤权、依赖变化、幂等 key 复用、ledger unknown、witness pending、`attempt_not_committed` 与 core purge receipt 符合父合同。
- [ ] 未注册或不健康的后续 content adapter 使 permanent delete 稳定不可用；能力不会因 core adapter 单独存在而开放。
- [ ] Web DTO/transport 固定 URL、headers、query、allowlisted response/error shape，并有 eager import 与 fresh bundle isolation 证明。
- [ ] feature 默认关闭时旧 VPS experience/timeline API 与 UI 不变，且新 Records production path 不读取或写入 `experience_logs`。
- [ ] focused unit/race/real PostgreSQL tests、`make verify-go`、Node 22 `make verify-web`、`git diff --check`、Trellis check、Draft PR required CI 全部通过。

## Out of Scope

- Blob bytes、附件处理、证据 capture、全文搜索/read projection、合流活动页面、评论/行动项、Markdown 编辑/阅读页面、导入导出和 VPS 概览由后续子任务实现。
- 不开放用户入口，不切换旧 experience 写路径，不进行 legacy 转换、staging、发布或产品推广验收。

## Execution Gate

用户已于 2026-08-03 明确批准本计划及三检查点执行方式。规划工件校验并提交后可执行 `task.py start`；只实施到当前检查点的验证与汇报边界，再继续下一检查点。
