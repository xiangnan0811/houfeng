# 当前项目剩余工作审计与下一阶段规划

> 日期：2026-05-11
>
> 范围：基于 `houfeng_codex_下一步开发计划.md`、`docs/release/asset-ledger-roadmap-completion.md`、`docs/release/next-phase-plan.md` 与当前仓库实现状态，确认旧计划剩余工作和下一阶段进入方式。
>
> 结论：旧计划没有新的立即开发任务；真实 VPS 数据验证保持条件性延期；前端大文件机械拆分暂停，等待页面产品/UX 方向重新确定。

## 决策摘要

1. `houfeng_codex_下一步开发计划.md` 对应的 VPS Asset Ledger 主线已按当前计划功能闭合。
2. Task 4 的真实 40+ VPS 数据 dry-run/import 仍是条件性剩余项：只有在用户提供数据文件并授权执行后才进入。
3. Provider/DNS 同步、Web SSH、插件系统、服务发现/完整注册表、完整域名管理、多用户 RBAC、汇率换算、复杂评分算法等方向不能继续挂在旧计划下推进；它们需要新的产品计划与 Trellis task。
4. 前端长页面/大文件继续拆分不再作为下一步默认任务。当前页面体验尚未被接受，继续机械拆分会把代码结构固化在可能被大改的页面形态上。
5. 下一阶段应在“真实数据验证”和“新产品/UX 规划”之间明确选择；如果目标是页面大调整，应先做 UX/信息架构规划，再决定具体组件拆分。

## 当前已完成状态

### V1 / Fleet Observability

V1 收口的核心能力已经完成并有 release 文档记录：

- Node / Target / ProbeItem / Agent sync / Incident / Event / Settings / Auth 等主链路存在。
- Stage 1 P0/P1 已在 `docs/release/next-phase-plan.md` 标记完成。
- 后续 event envelope、notification channel model、command result durability、Agent command/Docker boundary、modal focus 等审计后补项已经以独立任务闭合，并记录在 `docs/release/v1-gap-checklist.md`。

### VPS Asset Ledger

`houfeng_codex_下一步开发计划.md` 的主要任务已按当前仓库状态闭合：

- Providers、VPS assets、subscriptions、VPS-to-Node links、renewal decisions、asset histories、timeline/experience logs 已实现。
- VPS-scoped service/domain 轻量扩展已实现。
- 资产页面、决策页面、Dashboard asset summary 已实现。
- 详细证据矩阵见 `docs/release/asset-ledger-roadmap-completion.md`。

### Import Tooling

真实 VPS JSON dry-run/import 的工具链已经完成：

```bash
go run ./cmd/houfeng-import-vps-json -file ./tmp/vps-assets.json -dry-run
go run ./cmd/houfeng-import-vps-json -file ./tmp/vps-assets.json -import
```

尚未完成的是实际跑用户真实 40+ VPS 数据。该项不是仓库内代码缺口，而是数据与授权边界。

### Frontend Technical Debt

近期已经完成多轮页面 section 抽离，降低了部分页面文件长度和局部复杂度。但当前页面体验本身仍需要重新评估，后续大概率会有页面结构级调整。

因此，剩余前端技术债应重新排序：

- **暂停**：继续按文件行数机械拆分页面。
- **优先**：先确定页面产品目标、信息架构、关键工作流、密度和交互模式。
- **再拆分**：只在新 UX 方向明确后，把稳定的页面区域提取为组件。

## 剩余工作分类

### A. 条件性剩余：真实数据验证

触发条件：用户提供或授权访问真实 40+ VPS JSON。

处理方式：

1. 先运行 dry-run。
2. 根据报告确认字段覆盖、重复识别、金额/周期/续费日期、provider 映射、subscription 映射是否足够。
3. 如果 dry-run 暴露模型缺口，先创建模型修正 Trellis task。
4. 只有 dry-run 报告可信且用户确认后，才执行 import。

本阶段暂不主动处理真实数据问题。

### B. 需要新产品计划：能力扩张

以下方向不是旧计划未完成项，不能直接进入实现：

- Provider/DNS 同步。
- Web SSH / 远程操作工作台。
- 插件系统。
- 服务发现、完整服务注册表、自动化服务资产采集。
- 完整域名管理、DNS 记录管理、Registrar 同步。
- 多用户 RBAC / 协作模型。
- 汇率换算与多币种归一化。
- 复杂资产评分、迁移评分或风险算法。
- 可重复视觉回归 / release publish workflow。

这些方向必须先做独立的 product/architecture planning，再拆开发任务。

### C. 暂停的技术债：前端长页面拆分

前端长页面拆分曾经是 Stage 2 技术债，但当前状态下不应继续自动推进。

原因：

- 用户已经明确当前页面不满意，后续需要大调整。
- 机械拆分会增加跨文件跳转成本，却未必改善页面任务流。
- 如果即将重做页面信息架构，提前抽象会产生返工。

恢复条件：

- 新页面结构和关键交互已经确定。
- 某个页面区域被确认会长期保留。
- 拆分能直接降低当前开发风险，而不是只追求行数下降。

### D. 持续流程要求

所有后续开发仍必须遵守：

- 新非 main 分支。
- Trellis task / PRD / context 记录。
- 本地按改动范围跑质量门。
- PR CI 全绿后合并。
- 合并后监控 main CI，并同步本地主分支。

release/publish workflow 当前按用户决策后续再考虑，本规划不把它纳入立即执行范围。

## 下一阶段推荐入口

### 入口 1：真实数据 dry-run

适用场景：用户准备提供真实 VPS JSON，并希望验证资产模型是否覆盖实际情况。

下一步任务范围：

- 读取真实数据文件。
- 运行 dry-run。
- 输出模型缺口报告。
- 决定是否 import 或创建模型修正任务。

### 入口 2：产品/UX 重新规划

适用场景：用户对当前页面不满意，希望大幅调整页面体验。

下一步任务范围：

- 选定要重做的核心页面集。
- 梳理目标用户的日常工作流。
- 确认页面信息架构、密度、首屏优先级和跨页面导航。
- 形成 UI/UX PRD，再拆实现任务。

该入口优先于继续拆分大文件。

### 入口 3：新能力产品计划

适用场景：用户要进入 Provider/DNS、Web SSH、插件、服务发现、域名管理、RBAC、汇率、评分算法等方向。

下一步任务范围：

- 明确产品边界和非目标。
- 确认安全/权限/数据模型影响。
- 做跨层设计和验收标准。
- 再分批实现。

## 当前不建议创建的任务

- “继续完成 `houfeng_codex_下一步开发计划.md` 的下一个 Task”：旧计划无新的立即 Task。
- “继续按行数拆前端页面”：暂停，等待 UX 方向。
- “直接实现 Web SSH / DNS sync / 插件”：缺产品边界和安全设计。
- “直接 import 真实 VPS 数据”：需要先有数据文件、授权和 dry-run 报告。
- “补 release/publish workflow”：用户已明确后续再考虑。

## 与其他文档的关系

- `houfeng_codex_下一步开发计划.md`：旧计划正文与任务拆分来源；当前状态段仍有效。
- `docs/release/asset-ledger-roadmap-completion.md`：Asset Ledger 完成度证据矩阵。
- `docs/release/next-phase-plan.md`：Stage 1 / Stage 2 总体 roadmap 入口。
- 本文档：2026-05-11 之后判断“接下来做什么 / 不做什么”的当前状态快照。
