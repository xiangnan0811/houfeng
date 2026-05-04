# EventsPage + SettingsPage Mono 字体落地（关闭 gap #20）

## Goal

在 EventsPage（498 行）与 SettingsPage（873 行）补齐 6 处 Mono 漂移点（`<MonoDigits>` 包数字 / mono className 包覆盖规则 textarea），关闭 v1-gap-checklist 整条 gap #20。

## Background

- 详见 [`research/codebase-events-settings.md`](research/codebase-events-settings.md)：6 处 mono 漂移点完整清单 + 改造体量预估 + 范围外的 v2 漂移备忘
- 前置任务建立的 v2 模式直接复用：`<MonoDigits>` `<Hostname>` `<Timestamp>`
- EventList 共享组件**已完全 v2 合规**（在 EventsPage / Dashboard / NodeDetail / TargetDetail 多处消费），本任务零修改
- v2 spec 权威：`docs/design/v2-houfeng/component-spec.md` §五 SettingsPage 段，明确要求 "Telegram token-masked 用 mono"、"覆盖规则 3 textarea mono 字体"
- 设计语言 §3.2 强制约束：所有数字度量 / 技术 ID / 时间戳必须 mono

## Decision (only one)

任务 scope 完全由 research §4 漂移清单确定，无设计 / 架构 / UX 决策点。直接执行 6 处包装。

**P3 优化项（RetentionInput 旁注 mono、textarea code 容器）不纳入本任务**（保持 #20 单一目标）。

## Requirements

### EventsPage（2 处）

1. **行 471 事件分组计数**：
   ```jsx
   // 改前
   <span className="section-heading__eyebrow">{group.events.length}</span>
   // 改后
   <span className="section-heading__eyebrow"><MonoDigits>{group.events.length}</MonoDigits></span>
   ```
2. **行 333-337 select 数量选项**：option 内文本是数字 (`10/25/50/100`)，包 `<MonoDigits>`：
   ```jsx
   <option value="10"><MonoDigits>10</MonoDigits></option>
   ```
   注：浏览器原生 `<select>` 内的 `<option>` 通常忽略子元素 styling（仅渲染纯文本）。如包装无效（option 内 spans 被忽略），改为整个 `<select className="mono">`，让 select 显示态走 mono 字体。

### SettingsPage（4 处）

1. **行 579 Telegram token 掩码**：
   ```jsx
   // 改前
   <p>{settings.telegram.token_masked_summary}</p>
   // 改后
   <p><MonoDigits>{settings.telegram.token_masked_summary}</MonoDigits></p>
   ```
2. **行 707 / 719 / 730 三个覆盖规则 textarea**：每个 textarea 加 `className="mono"`（覆盖现有 className 时合并），让 textarea 内文字走 mono 字体（spec §五明示要求）

### Mono import 校验

- EventsPage 与 SettingsPage 当前未 import `MonoDigits`，需要 import
- mono className 走 atoms.css 已有的 `.mono` class（仅 font-family）

### 测试

- EventsPage 当前**无单元测试**：本任务**不**新建（边界微小，且 mono 包装是渲染层细节，单元测试边际价值低）
- SettingsPage：grep 看是否有现有测试，如有则跑确保零回归；如无，本任务也不新建
- 全量 vitest 必须 pass（基线 364）

## Acceptance Criteria

- [ ] EventsPage 事件分组计数用 `<MonoDigits>` 包装
- [ ] EventsPage select 数量选项 mono 化（option 子标签或 select className="mono" 二选一）
- [ ] SettingsPage Telegram token_masked_summary 用 `<MonoDigits>` 包装
- [ ] SettingsPage 3 个覆盖规则 textarea 均含 `mono` class
- [ ] 现有功能零回归（事件加载/筛选切换/表单提交）
- [ ] `cd web && npm run lint && npm run test && npm run build` 全绿
- [ ] `make verify-go` 全绿（前端改动应不影响后端）

## Definition of Done

- TypeScript strict 0 error
- ESLint 0 warning
- vitest 364/364 pass（基线不动）
- 关闭 `docs/release/v1-gap-checklist.md` gap #20 整条
- 不需要更新 `docs/design/v2-houfeng/component-spec.md`（实施完全符合既有 spec 文字）
- 不需要 trellis-update-spec

## Out of Scope

- 后端任何改动
- 新建组件 / 新建 atoms（全部复用既有）
- EventsPage / SettingsPage 视觉布局重做
- SettingsPage 表单密度优化（建议未来单独任务）
- SettingsPage textarea 升级为 code editor 形态（建议未来单独任务）
- RetentionInput 旁注 mono 显示（设计上不强制）
- 节点 / Dashboard / Targets / Onboarding 等其他页面（已完成）
- 引入图表库

## Technical Approach

**单 PR**：~45 min 实现 + 文档同步（关闭 gap #20）。

直接 wrap 包装，无新组件、无新 hook、无新 CSS class（`mono` class 已存在于 atoms.css）。

## Decision (ADR-lite)

**Context**：v1-gap-checklist #20 全站 Mono 字体落地剩 EventsPage + SettingsPage 两块。前置任务（节点页面 / Onboarding / Dashboard / Targets）已建立完整 v2 模式，剩余 mono 漂移点经 explore 审计仅 6 处，无设计决策。

**Decision**：单 PR 包装 6 处 + 文档关闭 gap #20，超出范围的 3 项 v2 漂移（表单密度 / textarea code 容器 / RetentionInput 旁注）不在本任务范围。

**Consequences**：
- 收益：v1-gap-checklist 整条 #20 关闭，全站 mono 字体合规
- 取舍：SettingsPage 仍有 3 处非 mono 类的 v2 漂移，留作未来 UX 任务

## Technical Notes

**关键文件**：
- 改造：`web/src/pages/EventsPage.tsx` / `web/src/pages/SettingsPage.tsx`
- 复用：`web/src/components/atoms/Mono.tsx`（`<MonoDigits>`）
- 同步：`docs/release/v1-gap-checklist.md`（关闭 #20）

## Research References

- [`research/codebase-events-settings.md`](research/codebase-events-settings.md) — 现状审计 + 6 处漂移完整清单 + 范围外 v2 漂移备忘
