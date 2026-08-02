# 记录、修订、草稿与状态核心

## Goal

实现稳定记录根、不可变完整修订、私有草稿、类型状态、生命周期、并发保存与记录永久删除核心。

## 2026-08-02 Development Rebaseline

本任务是 Child 1 关闭后的首个功能实现，拥有 `0052_create_records_core.sql`。只支持 fresh/current development database 与 exact repeat；`0052` 必须在同一 PR 登记 current APP ACL managed objects/privileges/admission tests。旧 `experience_logs` 不迁移、不回填、不双写，也不再作为生产兼容验收来源。

## Requirements

- 父设计：`../07-13-vps-detail-experience-design/design.md` §9–§11、§15、§19–§22。
- 直接依赖：`07-14-vps-records-platform-foundation` 必须已合入 main 且 post-merge CI 通过。
- 使用稳定 record root + 不可变完整 revision + 可重建 current projection；title、Markdown、类型/状态、影响/时间、可见范围、主体/关联、标签、负责人/参与者、跟进、template provenance 均由 revision 权威保存。
- primary/related subject 使用版本化 kind/role registry、当时身份快照、可空 live route/tombstone；来源删除不 cascade 记录。
- 新建与编辑均先形成作者私有 server draft；浏览器缓冲不是权威。草稿保存独立 ETag、基准 revision、恢复点和 90 天/7 天提示合同。
- 正式保存使用 base revision + Idempotency-Key + CAS；一次事务写 revision/ref/current pointer/current projection/domain activity/outbox，并为搜索 participant 保留 transaction hook。
- 类型化状态、统一状态组、可选版本化 Markdown 模板和无状态类型不渲染空状态。
- 文档生命周期只有 active/archived；restore old revision 复制成新 revision，不覆盖历史。
- 实现 record permanent-delete preview/operation/core purge adapter，复用子任务 1 reservation/ledger/fence；所有其他 adapter 未注册前 capability 保持关闭。
- 旧 `experience_logs` 不迁移、不回填、不双写；现有表/代码可暂时保留，但新 Records 合同不读取它。
- 提供版本化 record/draft/revision API DTO：canonical类型追加到现有 `web/src/lib/types.ts`；仅被lazy records routes消费的transport放在 `web/src/lib/recordsApi.ts` 并提供fresh bundle证明，不交付完整页面。AppShell-eager能力不得导入该domain façade。

## Acceptance Criteria

- [ ] 任一 revision 可独立还原父设计要求的全部结构化字段、关系和 Markdown；root projection 与 current revision 对账差异为 0。
- [ ] revision 1、后续保存、无变化保存、幂等重试、CAS conflict、restore old revision 均有单元和真实 PostgreSQL测试。
- [ ] stale draft 打开/保存返回字段+Markdown conflict DTO，不发生 last-write-wins；草稿恢复点/TTL/清理不进入搜索/活动/通知。
- [ ] subject kind/role 非法、live source 已删除、多来源 auth scope 收窄与 tombstone 导航具有稳定响应。
- [ ] archive/restore 保留全部 revision/evidence/attachment 引用；永久删除 reservation 后 core 新写/新读命中为 0。
- [ ] preview 后撤权、依赖变化、幂等 key 复用、ledger unknown/not-committed 状态符合父合同。
- [ ] `/api/records`、draft/revision/archive/permanent-delete endpoints 受统一 auth middleware保护并有 response allowlist。
- [ ] feature 默认关闭时旧 VPS experience/timeline API 与 UI 行为完全不变。
- [ ] `make verify-go`、focused Web API tests、PostgreSQL fresh/repeat migration 与 current APP ACL/admission 通过。

## Out of Scope

- Blob bytes、附件处理、证据 capture、全文搜索、评论/行动项和导入导出分别由后续子任务实现；本程序不包含 legacy 转换。
- 不开放用户入口或切换旧 experience 写路径。

## Execution Gate

- 状态保持 `planning`；依赖合入并获用户执行授权后才可 start。
