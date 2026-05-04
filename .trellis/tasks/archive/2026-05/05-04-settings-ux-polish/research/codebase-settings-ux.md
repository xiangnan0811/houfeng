# SettingsPage UX 收尾 — 3 项优化点现状审计

**日期**: 2026-05-04
**任务**: SettingsPage 3 项非 mono 类 v2 漂移收尾（来自前置任务 05-04-mono-events-settings 备忘）
**结论摘要**: 3 项均找到方案 B 为最优解，总工作量 ~150 行代码 + 5 个 CSS class，可单 PR 一气呵成。

---

## 1. SettingsPage 当前结构

文件：`web/src/pages/SettingsPage.tsx`（880 行）

| Section | 行号 | 关键 grid | summary-card 数 |
|---------|------|----------|----------------|
| Hero Panel | 520-528 | — | 0 |
| 主题 | 530 | — | 0 |
| Telegram | 532-620 | summary-grid | 4 |
| 频率档位 | 622-689 | summary-grid | 4 |
| 全局默认 | 691-704（IncidentDefaultsEditor）| summary-grid | 6 |
| 覆盖规则 | 706-748 | page-stack | 0（3 textarea） |
| 保留策略 | 750-822 | summary-grid | 4 |

**关键子组件**：
- `FrequencySelect`（267-288）：summary-card 嵌 select；标签语义 OK，无 mono 漂移
- `RetentionInput`（290-310）：summary-card 嵌 plain numeric input，**无 unit suffix / 无旁注**
- `IncidentDefaultsEditor`（312-389）：6-card summary-grid
- `OverrideTextarea`（391-412）：已带 `className="mono"`（前置任务实施），**无预览**
- `ThemeSettingsSection`（841-873）

---

## 2. 项目 (a) 表单密度审计

**当前 summary-grid 4 处**：Telegram / 频率档位 / 全局默认 / 保留策略

**v2 紧凑模式参考**：NodeHostMetrics 的 `metric-grid--quad`：固定 4 列 + 紧 padding（`var(--space-4)`）+ 紧 gap（`var(--space-3)`）

**密度对比**：
| 维度 | summary-card | metric-card |
|------|-------------|-----------|
| Padding | space-3 / space-4（16/24px）| space-4（24px 全向）|
| Gap | 6px 卡内 | space-3 卡间 |
| Grid | minmax(220px, 1fr) 响应式 | repeat(4, minmax(0,1fr)) 固定 |

**各 section 适配性**：
| Section | 字段 | Label 长度 | 适合紧凑？ |
|---------|------|-----------|-----------|
| Telegram | 4 | 较长（"新的 Telegram Bot Token"）| ❌ 不适合（label 会折行）|
| 频率档位 | 4 | 中等 | ✅ 最适合 |
| 全局默认 | 6 | 较长 | ⚠️ 调成 3 列可考虑，但因 label 长保守 |
| 保留策略 | 4 | 中等 | ✅ 最适合 |

**3 个方案**：
- **A 全 section 紧凑**：风险高（Telegram label 折行）
- **B 仅频率档位 + 保留策略紧凑**（推荐）：精准、低风险
- **C 全局收紧 padding**：最小改动但 Telegram 仍未解决

**推荐方案 B**：新建 `.summary-grid--numeric` variant，应用到频率档位 + 保留策略 2 处，~30 行。

---

## 3. 项目 (b) 覆盖规则 textarea code 容器

**OverrideTextarea 现状**（391-412）：`<textarea className="mono" rows={10}>`，3 处使用（节点标签 / Target 类型 / Target 标签 各一）。

**v2 NodeOnboardingPage 的 `<pre><code>` 参考**（pages.css `.onboarding-snippet`）：bg-sidebar 背景 + border + mono + word-break

**4 个方案**：
- **A Split view**：左编辑 textarea + 右实时预览 `<pre><code>`，可能还要引 prismjs 高亮库；过度设计
- **B 可折叠预览**（推荐）：保留 textarea；新增 `<details><summary>预览</summary><pre><code>{格式化 JSON}</code></pre></details>`，invalid JSON 时不显示预览
- **C 显示已保存值 pre**：需扩 props 加 savedValue；占空间多
- **D 不动**：当前已 mono 字体

**推荐方案 B**：投入产出比好，~50 行（component 内部增强 + CSS）。invalid JSON 时静默隐藏预览（无错误提示，简化）。

---

## 4. 项目 (c) RetentionInput 旁注

**RetentionInput 现状**（290-310）：summary-card + plain numeric input，4 处（rawLayer/aggregateLayer/eventLayer/notificationLayer Days）。

**v2 NodeHostMetrics 参考**：每 metric-card head 用 `<MonoDigits>` 显当前值

**4 个方案**：
- **A input 下方 hint** "当前: N 天"：需扩 props 加 savedValue；占空间
- **B input 内嵌 unit suffix** " 天"（推荐）：内嵌 flex 容器，无 props 改动
- **C label 旁 badge** "当前: N"：易混淆为默认值
- **D 不动**：form input 通常不需要旁注，但 retention 字段单位（天）确实需要明示

**推荐方案 B**：清晰单位 + 极小改动（~30 行 + CSS class）。

---

## 5. 改造体量与建议

| 项目 | 推荐方案 | 行数 | 新 class | 风险 |
|------|---------|------|---------|------|
| (a) 表单密度 | B numeric grid | ~30 | 1 | 低 |
| (b) textarea code | B 折叠预览 | ~70 | 2 | 低 |
| (c) retention unit | B suffix | ~50 | 2 | 低 |
| **合计** | — | **~150** | **5** | **低** |

**PR 拆分**：
- 可单 PR 完成（3 项内聚都在 SettingsPage.tsx + pages.css）
- 也可拆 3 PR 独立 review

**新 atom / hook 需求**：无。复用既有 `<MonoDigits>` + `<details>` + 纯 CSS。

**测试影响**：
- (a) 现有测试零破坏（layout 改）
- (b) 可加 ≥2 用例（valid JSON 显预览 / invalid JSON 不显）
- (c) 现有 `getByLabelText` 不破坏，可加单元 "renders 天 suffix"

**总结**：3 项都是低风险纯 UI 增强，建议**单 PR**完成（~150 行）。
