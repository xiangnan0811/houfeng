# Audit VPS detail page refactor completion

## Goal

对当前 protected-main 上的 VPS 详情页重构及其 Records 支撑链路进行一次独立、
全面、只读的完成性审查。审查必须以当前代码和新鲜验证结果为准，判断已批准范围
是否完整交付、实现是否合理可靠、是否引入回归，以及是否仍存在有证据支持的必要
完善项；不得因为历史任务、PR 或 CI 曾标记成功就直接给出通过结论。

最终结论只能是以下两类之一：

1. 在明确范围、环境和证据限制内，没有未解决的 Critical、Important 或 Minor
   问题，也没有值得继续投入的具体优化项，可以确认本轮重构完成；
2. 存在可复现问题、证据缺口或范围漂移，按严重度、绝对 `file:line`、影响、复现
   方法和建议处置报告，因此不能确认“无需进一步完善”。

## Background and Confirmed Facts

- 本任务从 `origin/main` 的 `08730e79` 创建，创建前主工作区干净；规划工件位于
  `codex/vps-detail-refactor-completion-audit`，本任务不直接修改 local/remote
  `main`。
- 原父任务 `07-13-vps-detail-experience-design` 及其 12 个功能 child 已归档；
  current handoff 声称这些 child 均已合入 protected main，overview 管理操作随后由
  PR #438 补齐并发布于 `v0.75.0`。这些是待重新核验的历史主张，不是本次结论。
- 当前产品边界明确排除单条运维记录不可逆永久删除。该能力必须继续保持未实现、
  未启用和 fail-closed；缺失的 readiness 成员与 nil production handler 不是本次
  应当补齐的缺陷。
- 三项 current-authority 延期同样不作为本次失败项：activity group-granted digest、
  comparison 390px sticky body-row headers、4 GiB / 512 MiB mixed-load harness。
  只有发现实现或文档把它们误报为已完成、边界已失效，或其 future trigger 已在
  当前代码中成立时，才形成 finding。
- 普通记录 archive/restore、整体重建可放弃的测试环境、单条记录永久删除是三种
  不同语义，审查和报告不得混淆。

## Requirements

### R1. Reconcile the authoritative scope

- 以当前 protected-main 代码、配置、迁移、测试和运行脚本为最高事实来源；其次
  核对 selected commit、PR、required CI、post-merge main CI、release/tag/image；
  再核对 Trellis task 状态与历史研究工件。
- 重新验证原父任务 12 个功能 child、overview 管理收口和后续修复提交的 ancestry、
  当前文件落点与任务归档状态，识别未合入、被后续回退或只有文档没有实现的部分。
- 从第一项功能 child 的 protected-main 基线到当前 `HEAD` 审查完整相关 change set，
  并回读当前依赖调用点；不得只审查最后一个 PR 或只读取历史审计摘要。

### R2. Review the complete product flow

- 覆盖 `/vps/:id` 的 capability gate、overview/legacy fallback、身份与异常、状态/
  续费摘要、最近活动、事实、关系、局部失败、加载/空/错态和五类管理操作。
- 覆盖从 VPS 详情进入的 activity、records、evidence 路由，以及项目级记录搜索、
  Markdown 阅读/编辑/修订、证据/附件、比较、协作通知、导入导出与归档恢复链路。
- 对关键读写流执行跨层追踪：UI → API client → router/handler → application/service
  → PostgreSQL/Blob/projection，以及反向 DTO/error/freshness/authorization 回传。
- 检查路由与 URL 状态、刷新和局部失败、重复提交、过期响应、切换 VPS、乐观锁、
  幂等、事务边界、异步 worker 和恢复语义，查找不可达入口、占位实现、静默失败、
  陈旧状态、重复事实或 legacy/new 路径漂移。

### R3. Review correctness, safety, and maintainability

- 核对当前 `.trellis/spec/`、已批准 current-authority 需求与实际实现，不把历史
  non-gating 设计矩阵重新升级为当前交付门。
- 检查授权一致性、无权 404、不泄露存在性、Markdown/XSS、附件/证据 allowlist、
  secret/command-output 禁止持久化、导入不可信边界、下载/导出重鉴权和日志内容边界。
- 检查数据完整性、migration/ACL/admission、不可变修订、草稿隔离、来源删除、
  projection rebuild、cursor、水位、Blob 引用、local/S3 与 backup/restore 合同。
- 检查 permanent-delete production handler、flags、readiness 和 adapter pairing 仍按
  current-authority 失败关闭；不得通过测试 helper 或存在部分 package 误判为可用。
- 检查重复逻辑、职责过载、循环依赖、类型绕过、warning suppression、debug logging、
  未测试公共行为和会造成现实维护/可靠性风险的复杂度。纯审美偏好或无明确收益的
  “可以再优化”不形成 finding。

### R4. Verify UX, responsiveness, and accessibility

- 使用当前 production build 与受控 fixture/API 数据，检查稳定、异常、首次空、
  查询无结果、加载、局部失败、提交中和无权/删除态。
- 至少覆盖 1440px desktop 与 390px mobile：语义顺序、无意横向溢出、具名局部
  滚动区、触摸目标、管理 dialogs、菜单/Modal 焦点和关闭后焦点恢复。
- 验证静止态可发现性、状态不只依赖颜色、键盘完成主要流程、Axe 无 serious/
  critical；已有 moderate 项也必须重新判断是否已成为当前范围内的真实问题。
- 不把 mock/unit 测试冒充真实浏览器验证，也不把本次自动化检查冒充新的 20 人
  30 秒理解研究。

### R5. Run fresh, non-skipping verification

- 使用仓库要求的 Node 22，执行 Go formatting check、vet、tests，Web lint、coverage、
  type/build、bundle/CSS budgets，以及完整 Chromium E2E。
- 执行 Records browser、安全、容量、local/S3 integration 与 local/S3 recovery
  脚本；凡脚本要求 Docker/PostgreSQL/MinIO 必须真正运行，缺少基础设施或出现 skip
  计为未通过证据，不得改成“可选通过”。
- 先执行有界 focused gates，再执行全量门禁。任何失败都要区分当前代码缺陷、
  环境阻塞和已批准延期，并保留精确命令与输出摘要。
- 验证前后检查 tracked/untracked 状态；测试生成物不得混入审查任务交付。

### R6. Verify delivery evidence

- 通过当前 Git/GitHub 状态复核相关 PR 是否 merged、selected/merge commit 是否一致、
  required checks 与 post-merge main CI 是否成功、release/tag/image 是否实际包含目标
  merge，不依赖可能过时的任务文本。
- 核对当前 branch/base、local/remote refs 和工作区状态，不清理或删除历史分支、
  worktree、tag、artifact 或用户数据。

### R7. Produce a falsifiable verdict

- Findings 优先，按 Critical → Important → Minor 排序；每项包含绝对文件路径和行号、
  受影响行为、证据/复现、为何现有测试未阻止以及最小修复方向。
- 若无 findings，明确列出审查范围、实际通过的门禁、浏览器/集成/交付证据、已批准
  边界和剩余不可证明限制；结论只覆盖当前 commit 与已批准范围。
- 本任务默认 findings-only。发现缺陷后不得修改产品代码、测试、spec、配置、迁移、
  PR 或外部系统；修复必须由用户另行授权。

## Acceptance Criteria

- [x] `AC-01` 当前代码、Trellis task tree、12 个功能 child、overview 收口、相关
  PR/CI/release 和 Git ancestry 已独立对账，无未解释矛盾。
- [x] `AC-02` `/vps/:id` 及其 activity/records/evidence/management 主路径的 UI → API
  → service → storage/projection 数据流已逐段审查，稳定/异常/局部失败与 legacy
  fallback 没有未解决缺陷。
- [x] `AC-03` Records 搜索、编辑修订、附件/证据、比较、协作、可移植性和恢复的
  关键共享合同已审查，无跨 child 漂移、隐式权限扩大、数据丢失或事实伪造。
- [x] `AC-04` permanent delete 保持 fail-closed，普通 archive/restore 可用；三项接受
  延期仍被准确描述且未被误接线或误报为完成。
- [x] `AC-05` Go formatting check、vet、tests 与 Web lint/coverage/build/budget 全部
  在当前 commit 新鲜通过，没有被隐藏的 skip、warning suppression 或测试绕过。
- [x] `AC-06` 完整 Chromium E2E 以及 Records browser、安全、容量、local/S3
  integration 和 local/S3 recovery 门禁均新鲜通过；任何未执行项都会阻止无条件
  完成结论。
- [x] `AC-07` 1440px 与 390px 的主要状态和管理操作已在真实 production preview
  中检查；键盘、焦点、overflow、44px 和 Axe serious/critical 合同通过。
- [x] `AC-08` 最终报告没有未解决的 Critical、Important 或 Minor finding，也没有
  能以现实风险/成本/用户收益证明的必要优化项；否则明确判定本轮不能确认完成。
- [x] `AC-09` 审查未修改任何产品代码、测试、迁移、spec、配置、部署或 CI，也未
  清理 Git/环境资源；仅本任务规划、研究和审查报告可产生 tracked diff。
- [x] `AC-10` 最终报告记录当前 commit、命令、结果、证据日期、边界和限制，使结论
  可被另一名审查者复现，而不是绝对化承诺“未来不会出现问题”。

## Out of Scope

- 修复、重构或优化任何产品代码、测试、spec、迁移、配置、部署和 CI。
- 实现或启用单条记录永久删除，补齐其 readiness/adapters，或重新打开历史 child。
- 把 activity group digest、390px sticky row headers 或完整 mixed-load harness 作为
  当前完成门；若 future trigger 当前已成立则只报告，不在本任务实现。
- 修改 staging/线上数据、重建数据库、部署新版本、创建/合并 PR、发布 release 或
  删除 branch/worktree/ref/artifact。
- 新组织 20 人理解研究、长期 soak 或正式容量 SLO；本次只能验证仓库现有的自动化
  合同并诚实标注该证据边界。

## Open Questions

- 无阻塞产品或范围决策。最终规划摘要仍需用户单独批准后才能启动审查执行。
