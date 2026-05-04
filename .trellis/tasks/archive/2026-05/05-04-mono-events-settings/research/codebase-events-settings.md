# Events + Settings Mono 字体落地审计

**日期**: 2026-05-04
**任务**: gap #20 收尾（Events + Settings 部分）— 关闭 v1-gap-checklist 整条
**结论摘要**: 极小任务，6 处 mono 包装（EventsPage 2 处 + SettingsPage 4 处），单 commit ~45 min；EventList 共享组件已 v2 合规。

---

## 1. EventsPage 现状

**文件**：`web/src/pages/EventsPage.tsx`（498 行），无单测；消费 `<EventList>` 共享组件。

### 关键 markup

```jsx
// 筛选栏（行 255-447）：summary-grid 多 select + input + checkbox
<select value={size}>
  <option value="10">10</option>   // 数字 plain text
  <option value="25">25</option>
  <option value="50">50</option>
  <option value="100">100</option>
</select>

// 事件流（行 449-495）：分组渲染
{groupedEvents.map((group) => (
  <div className="event-group">
    <header>
      <h3>{EVENT_GROUP_LABELS[group.key]}</h3>
      <span className="section-heading__eyebrow">
        {group.events.length}      // 数字 plain text，非 mono
      </span>
    </header>
    <EventList events={group.events} />   // ✓ 已 v2
  </div>
))}
```

### Mono 漂移点（2 处）

| 行 | 现状 | 应改为 |
|----|------|--------|
| 471 | `{group.events.length}` 裸渲 | `<MonoDigits>` 包装 |
| 333-337 | select 4 个 option `10`/`25`/`50`/`100` 裸渲 | option 内或外层 mono class |

---

## 2. SettingsPage 现状

**文件**：`web/src/pages/SettingsPage.tsx`（873 行）；含子组件 `ThemeSettingsSection` / `FrequencySelect` / `RetentionInput` / `IncidentDefaultsEditor`。

### v2 spec §五 SettingsPage 摘录

```
1. Hero panel
2. DetailSection 主题（ribbon notice）
3. DetailSection Telegram（ribbon accent-2）：token 输入 + chat-id + 状态卡（mono token-masked）+ runtime checkbox
4. DetailSection 频率档位（ribbon normal）：4 segmented
5. DetailSection 全局默认（ribbon notice）：6 字段
6. DetailSection 覆盖规则（ribbon notice）：3 textarea（mono 字体）   ← 强调 mono
7. DetailSection 保留策略（ribbon notice）：4 retention 输入
```

### 当前 section 排列：与 spec 完全对齐 ✓

### Mono 漂移点（4 处）

| 行 | 现状 | 应改为 |
|----|------|--------|
| 579 | Telegram `{settings.telegram.token_masked_summary}` 裸渲 | `<MonoDigits>` 或 mono class（spec §五明示 "mono token-masked"） |
| 707 | 覆盖规则 textarea raw JSON | `className="mono"`（spec §五明示 "3 textarea mono 字体"） |
| 719 | 同上 textarea | `className="mono"` |
| 730 | 同上 textarea | `className="mono"` |

---

## 3. EventList 共享组件现状

**文件**：`web/src/components/EventList.tsx`（120 行）

```jsx
<dd>
  <Hostname>{event.object_id}</Hostname>            // ✓ Mono
</dd>
<dd>
  <Timestamp value={event.created_at} mode="absolute" />   // ✓ Mono
</dd>
```

**结论**：已完全 v2 合规，本任务零工作量。EventsPage / Dashboard / NodeDetail / TargetDetail 多页面消费均受益。

---

## 4. Mono 漂移点完整清单

| 页面 | 文件:行号 | 现状 | 应改为 | 优先级 |
|------|---------|------|--------|--------|
| EventsPage | 471 | `{group.events.length}` | `<MonoDigits>` | P1 |
| EventsPage | 333-337 | select option 数字 | mono 包装 | P2 |
| SettingsPage | 579 | token_masked_summary | `<MonoDigits>`（含 spec §五要求） | P1 |
| SettingsPage | 707 | 覆盖规则 textarea | `className="mono"`（spec §五要求） | P1 |
| SettingsPage | 719 | 同 | 同 | P1 |
| SettingsPage | 730 | 同 | 同 | P1 |

合计 6 处（5 P1 + 1 P2）。EventList 0 处。

---

## 5. 改造体量预估

参考对标：Dashboard 任务（"已 9/10 完成，仅差一处" → 单 PR ~30 min）。

本任务 6 处 vs Dashboard 1 处 → **单 PR ~45 min**（含测试）。**不需拆 PR**。

体量明显小于节点页面 / 目标页面 / 接入工作台等任务。

---

## 6. 重要发现（mono 范围外的 v2 漂移）

以下漂移**不纳入本任务**（保持 #20 单一目标），但建议未来单独跟踪：

### (a) SettingsPage 表单密度偏松
- 当前用 `summary-card` 嵌套 + `summary-grid`（多列等宽），跟 v2 节点详情页 `metric-grid--quad` 紧凑模式有差距
- 不影响 mono 闭合，但视觉密度稍松
- 建议：未来发起单独"SettingsPage 视觉密度优化"任务

### (b) 覆盖规则 textarea 无 code 容器
- 当前是纯 `<textarea rows={10}>`
- v2 NodeOnboardingPage 安装步骤用 `<pre><code>` mono 容器（编辑态用 textarea + 显示态用 code）
- 不影响 mono 闭合（本任务只加 className="mono" 让字体变 mono）
- 建议：未来若有 SettingsPage 编辑器增强需求，考虑 split view（编辑 textarea + 渲染 code）

### (c) RetentionInput 显示已保存值无 Mono 旁注
- 当前是 plain `<input type="number">`，无显示旁注
- 跟 NodeHostMetrics dl 模式不一致
- 设计上"输入框"vs"显示值"是两种形态，不强制旁注
- 建议：保留现状

---

## 总结

- **本任务**：6 处 wrapping + 0 处新组件 + 0 后端 = 单 PR ~45 min
- **EventList**：已 v2，零工作量
- **超出范围的漂移**：3 处（密度 / textarea code 容器 / RetentionInput 旁注），均建议跟踪到独立任务
- **关闭 #20**：本任务完成后整条 #20 可标 Closed
