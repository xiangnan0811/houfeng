# Current project remaining work audit and next-stage planning

## Goal

梳理 `houfeng_codex_下一步开发计划.md` 与仓库当前实现状态，产出一份可作为后续推进入口的剩余工作审计 / 下一阶段规划文档。该任务只做规划与文档收口，不继续做前端大文件机械拆分。

## What I Already Know

- 用户确认本轮先做“当前项目剩余工作审计/下一阶段规划”。
- 用户明确前端大文件拆分可以留给后续，因为当前页面暂时不满意，后续肯定还需要大调整。
- 根目录计划 `houfeng_codex_下一步开发计划.md` 2026-05-10 状态段已写明：Task 1-3、Task 5-8、VPS-scoped service/domain 轻量扩展已闭合；Task 4 dry-run/import 工具链完成，真实 40+ VPS 数据执行仍依赖用户数据与授权。
- `docs/release/asset-ledger-roadmap-completion.md` 已给出 Asset Ledger 主线完成度审计，结论是旧计划没有新的立即功能实现任务。
- `docs/release/next-phase-plan.md` 仍保留 Stage 2 / long-page 拆分等历史状态，需要补充 2026-05-11 的当前决策，避免后续继续机械推进不合时宜的前端拆分。
- 本仓库必须遵守 Trellis 任务记录与 PR/CI/main 同步流程；本轮不使用 subagent。

## Requirements

- 新增或更新 release/roadmap 层文档，作为“当前项目剩余工作审计/下一阶段规划”的权威入口。
- 明确旧计划完成状态：
  - Asset Ledger 主线对当前计划而言已功能闭合。
  - 真实 VPS 数据 dry-run/import 是条件性剩余项，本轮不处理。
  - Provider/DNS 同步、Web SSH、插件、服务发现/注册表、完整域名管理、RBAC、汇率、评分算法等扩展必须另起产品计划，不能自动归入旧计划。
- 明确前端机械拆分暂停：
  - 近期不把“继续拆长页面”作为独立推进任务。
  - 若用户对页面不满意，应先做产品/UX/信息架构规划，再在新设计方向内决定组件拆分。
- 更新现有 `docs/release/next-phase-plan.md`，把新审计文档挂到 Stage 2 当前状态中。
- 不修改业务代码、不修改前端页面实现、不引入 release/publish workflow。

## Acceptance Criteria

- [x] 存在一份清晰的当前状态与下一阶段规划文档，列出已完成、条件性剩余、暂停/延期、需要新计划的工作。
- [x] 文档明确声明：前端长页面/大文件拆分暂停，等待页面产品与 UX 方向重新确定。
- [x] `docs/release/next-phase-plan.md` 指向该文档，并记录 2026-05-11 状态更新。
- [x] Trellis task 中保留 PRD 和 research 记录。
- [x] 本地文档校验通过，至少运行 `git diff --check`；PR CI 通过后合并。

## Definition of Done

- 文档变更已提交到非 main 分支。
- Trellis task 已归档并记录 journal。
- PR 已创建，CI 全绿后合并。
- 合并后监控 main CI，并同步本地 `main`。

## Out of Scope

- 不运行真实 40+ VPS JSON dry-run/import。
- 不继续拆分前端长页面或大文件。
- 不重做当前页面视觉 / 信息架构。
- 不实现 Provider/DNS 同步、Web SSH、插件、服务发现、完整域名管理、RBAC、汇率或评分算法。
- 不处理 release/publish workflow。

## Technical Notes

- 计划依据：
  - `houfeng_codex_下一步开发计划.md`
  - `docs/release/asset-ledger-roadmap-completion.md`
  - `docs/release/next-phase-plan.md`
  - `docs/release/v1-gap-checklist.md`
- Trellis / workflow 依据：
  - `.trellis/spec/guides/branch-workflow-governance.md`
  - `.trellis/spec/backend/quality-guidelines.md`
  - `.trellis/spec/web/quality-guidelines.md`
- 本轮使用主会话完成，不使用 subagent。
