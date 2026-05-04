# SettingsPage UX 收尾（form 密度 / textarea code / RetentionInput unit）

## Goal

完成 SettingsPage 的 3 项非 mono 类 v2 漂移收尾（来自前置任务 05-04-mono-events-settings 备忘）：
1. 频率档位 + 保留策略 section 迁紧凑 grid
2. 覆盖规则 textarea 加可折叠 JSON 预览
3. RetentionInput input 加 " 天" unit suffix

总工作量 ~150 行代码，单 PR 一气呵成。

## Background

- 详见 [`research/codebase-settings-ux.md`](research/codebase-settings-ux.md)：3 项各自现状审计 + 4 备选方案对比 + 推荐方案 + 风险评估
- 前置任务 `05-04-mono-events-settings`（commit 24fd997）已关闭 v1-gap-checklist gap #20 整条；本任务是它的 follow-up（备忘 §6 中范围外 3 项）
- 复用：`<MonoDigits>` / 既有 `summary-grid` / 既有 `summary-card` / `<details>` 原生 HTML / `metric-grid--quad` 风格参考
- v2 设计权威：`docs/design/v2-houfeng/component-spec.md` §五 SettingsPage（spec 已对齐当前实现，本任务不改 spec）

## Decisions

3 项都按 research 推荐 **方案 B**（最佳投入产出比）：

- **(a) 表单密度** — **方案 B**（仅频率档位 + 保留策略 section 紧凑）：新建 `.summary-grid--numeric` variant，应用到 2 处；Telegram / 全局默认 / 覆盖规则保留原 grid（避免长 label 折行风险）
- **(b) 覆盖规则 textarea code 容器** — **方案 B**（可折叠预览）：OverrideTextarea 内部新增 `<details>` 元素，valid JSON 时显示格式化 `<pre><code>`；invalid JSON 时静默隐藏预览（不做错误提示）
- **(c) RetentionInput 旁注** — **方案 B**（input 内嵌 unit suffix " 天"）：新建 `.input-with-suffix` flex 容器；无需扩 props

## Requirements

### (a) 紧凑 grid

新增 CSS（追加 `web/src/styles/pages.css`）：
```css
.summary-grid--numeric {
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-2);
}
.summary-grid--numeric .summary-card {
  padding: var(--space-2) var(--space-3);
  gap: 4px;
}
@media (max-width: 1080px) {
  .summary-grid--numeric { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
```

修改 SettingsPage.tsx（2 处）：
- 频率档位 section grid（行 ~624）：`<div className="summary-grid summary-grid--numeric">`
- 保留策略 section grid（行 ~751）：`<div className="summary-grid summary-grid--numeric">`

不动：Telegram / 全局默认（IncidentDefaultsEditor 内部）/ 覆盖规则。

### (b) 覆盖规则可折叠预览

修改 OverrideTextarea（行 391-412）：
```tsx
function OverrideTextarea({ ariaLabel, value, onChange }: OverrideTextareaProps) {
  let previewContent: string | null = null
  if (value.trim()) {
    try {
      previewContent = JSON.stringify(JSON.parse(value), null, 2)
    } catch {
      previewContent = null
    }
  }

  return (
    <div className="override-rule-field">
      <label>
        <span>{ariaLabel}</span>
        <textarea aria-label={ariaLabel} className="mono" rows={10} value={value} onChange={(e) => onChange(e.target.value)} />
      </label>
      {previewContent ? (
        <details className="override-rule-preview">
          <summary>预览</summary>
          <pre><code>{previewContent}</code></pre>
        </details>
      ) : null}
    </div>
  )
}
```

新增 CSS：
```css
.override-rule-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}
.override-rule-preview {
  background: var(--bg-sidebar);
  border: 1px solid var(--border);
  border-radius: var(--radius-2);
  padding: var(--space-2) var(--space-3);
}
.override-rule-preview > summary {
  cursor: pointer;
  font-family: var(--font-sans);
  font-size: var(--type-small-size);
  color: var(--text-muted);
  user-select: none;
}
.override-rule-preview > pre {
  margin: var(--space-2) 0 0 0;
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--text-primary);
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 320px;
  overflow: auto;
}
```

### (c) RetentionInput unit suffix

修改 RetentionInput（行 290-310）：
```tsx
function RetentionInput({ ariaLabel, value, onChange }: RetentionInputProps) {
  return (
    <label className="summary-card">
      <span className="summary-card__label">{ariaLabel}</span>
      <span className="input-with-suffix">
        <input aria-label={ariaLabel} inputMode="numeric" value={value} onChange={(e) => onChange(e.target.value)} />
        <span className="input-with-suffix__unit">天</span>
      </span>
    </label>
  )
}
```

新增 CSS：
```css
.input-with-suffix {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  width: 100%;
}
.input-with-suffix > input { flex: 1; min-width: 0; }
.input-with-suffix__unit {
  font-family: var(--font-sans);
  font-size: var(--type-small-size);
  color: var(--text-muted);
  white-space: nowrap;
}
```

## Acceptance Criteria

- [ ] 频率档位 section 与 保留策略 section 的 grid 应用 `summary-grid--numeric`
- [ ] OverrideTextarea 在 valid JSON 输入时显示可折叠预览，预览内容是 `JSON.stringify(parsed, null, 2)`
- [ ] OverrideTextarea 在 invalid JSON 或空字符串输入时不渲染预览块
- [ ] RetentionInput 4 处使用都显示 input + " 天" unit suffix
- [ ] 现有功能零回归（保存表单 / 加载持久化 / 错误反馈）
- [ ] `cd web && npm run lint && npm run test && npm run build` 全绿（基线 364）
- [ ] `make verify-go` 全绿
- [ ] 新增 ≥2 个用例：valid JSON 显预览 / invalid JSON 不显预览（项目 b 关键交互）

## Definition of Done

- TypeScript strict 0 error
- ESLint 0 warning
- vitest 全 pass
- 不需要更新 v2 spec（实施细节，spec §五已涵盖整体）
- 不需要更新 v1-gap-checklist（这 3 项不在 checklist 内）
- 不需要 trellis-update-spec

## Out of Scope

- 后端任何改动
- Telegram / 全局默认 / 主题 section 视觉调整
- JSON 高亮（prismjs / highlight.js 等）— 保持纯 mono 文本
- Split view 编辑模式 — 用 details 折叠预览替代
- RetentionInput 显示已保存值对比 — 单位 suffix 已足够
- 引入新依赖 / 新 atom / 新 hook
- 移动端响应式（除 metric-grid--quad 已有的 1080px breakpoint）

## Technical Approach

**单 PR**：~150 行（3 项内聚都在 `SettingsPage.tsx` + `pages.css`）。

**关键技术点**：
- 复用 `<details>` 原生 HTML（无需 React state，零依赖）
- `summary-grid--numeric` 模式从 `metric-grid--quad` 学习（固定 4 列 + 1080px 降 2 列）
- `.input-with-suffix` 用 flex 实现 input + unit 内嵌（不破坏 input 原生行为）
- JSON 预览仅在 valid 时显示，invalid 静默隐藏（简化 — 用户写错了在保存时会看到 error）

## Decision (ADR-lite)

**Context**：SettingsPage 完成 mono 收尾后，仍有 3 项 UX polish 备忘（form 密度 / textarea code 容器 / RetentionInput 旁注）。各项有 3-4 个备选方案，工作量从极小到中等不等。

**Decision**：
1. 3 项都选**方案 B**（最佳投入产出比，详见 research）
2. 单 PR 完成（3 项内聚，无独立验证价值）
3. 不引高亮库 / 不做 split view / 不做已保存值对比 — 这些都属过度设计

**Consequences**：
- 收益：3 项 UX 痛点收尾，SettingsPage 视觉密度对齐 v2 风格，编辑覆盖规则有实时反馈，retention 单位语义明确
- 取舍：JSON 预览无高亮（纯 mono 文本）；invalid JSON 静默隐藏不报错（编辑态无即时验证反馈）
- 风险：极低，纯 UI/UX 增强，无 API 改动

## Technical Notes

**关键文件**：
- 改造：`web/src/pages/SettingsPage.tsx`（OverrideTextarea + RetentionInput + 2 处 grid className）
- 改造：`web/src/styles/pages.css`（追加 `.summary-grid--numeric` / `.override-rule-field` / `.override-rule-preview` / `.input-with-suffix` 共 5 个 class）
- 测试：`web/src/pages/SettingsPage.test.tsx`（≥2 新用例）

**复用**：
- `<details>` `<pre>` `<code>` 原生 HTML
- 既有 `.summary-grid` `.summary-card` `.mono`
- `metric-grid--quad` 风格参考（pages.css 既有）

## Research References

- [`research/codebase-settings-ux.md`](research/codebase-settings-ux.md) — 3 项现状审计 + 4 备选方案对比 + 风险评估
