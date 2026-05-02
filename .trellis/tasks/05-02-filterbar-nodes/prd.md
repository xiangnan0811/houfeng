# Apply FilterBar to NodesPage (child 2 of list-filter-completion)

> Child 2 复用 child 1 (archived) 抽出的 web/src/components/filters/。pattern 已建立，本任务直接 apply。

## Goal

在 NodesPage 应用 FilterBar，实现 §6.3 全部 7 项筛选；删除现有任何与 §6.3 不一致的 filter 半成品（按 "无需兼容" 授权）。

## What I already know

### §6.3 7 项筛选清单（实读 rules-and-interaction.md:244-252）

1. 地区 / 城市 (`region` / `city` 复合 select)
2. 供应商 (`provider`)
3. 生命周期状态 (`lifecycle_status`)
4. 监控运行状态 (`run_status` / monitoring_status)
5. 健康状态 (`health_status`)
6. 标签 (`labels` multi)
7. 仅看异常节点 (boolean toggle)

### NodesPage 现状

- `web/src/pages/NodesPage.tsx` 当前**无任何 list filter UI**（grep 确认）
- batch 3 reassess 时说"2 toggle"——可能指别的（行内 inline action / 创建表单内的 select）；本任务**实地确认 + 删除任何 §6.3 不符的 filter UI**
- 已知反模式（gap #10）：line 60 `createNode` 直接 `fetch('/api/nodes')` 绕 `lib/api.ts`——**不在本任务 scope 修复**（独立 follow-up）

### 复用 child 1 模式

- 组件：`<FilterBar>` / `<FilterSelect>` / `<FilterMultiSelect>` / `<FilterToggle>` / `<FilterChip>`
- 状态：URL query string via `useSearchParams`
- 应用：client-side `useMemo` 在 list render 前过滤
- 视觉：filters.css 鎏金 chip per v2 token

## Requirements

1. NodesPage 加 FilterBar（在 list section 上方）含 §6.3 7 项 filter：

   | Filter | 组件 | URL param | 数据来源 |
   |---|---|---|---|
   | 地区 | FilterSelect | `region` | NodeRecord.region distinct |
   | 城市 | FilterSelect | `city` | NodeRecord.city distinct |
   | 供应商 | FilterSelect | `provider` | NodeRecord.provider distinct |
   | 生命周期 | FilterSelect | `lifecycle` | hardcode 5 status (待接入/在用/观察中/不续费/已退役) |
   | 运行状态 | FilterSelect | `run_status` | hardcode (启用/暂停/维护中) |
   | 健康状态 | FilterSelect | `health` | hardcode (正常/关注/告警/严重) |
   | 标签 | FilterMultiSelect | `labels` | NodeRecord.labels distinct |
   | 仅看异常 | FilterToggle | `abnormal=1` | health != "正常" |

   注：地区 / 城市拆 2 个 FilterSelect（保持简单；不做级联）。共 8 个 control（PRD 说 7 项 filter，但地区+城市 = 2 control 是合理拆分）。

2. 删除 NodesPage 现有任何 filter 半成品（实地判断）；保留所有非 filter 业务（创建 / runtime actions / detail link / binding conflict 等）

3. 加 page test：1-2 个 filter 测试用例（如选 lifecycle="在用" 后列表只剩对应节点）

4. `make verify-web` 全绿（lint + test + build）

## Acceptance Criteria

- [ ] NodesPage 7 项 filter（含地区+城市拆 2 control）UI 可见可用
- [ ] URL query string 同步（如 `/nodes?region=华东&lifecycle=在用&abnormal=1`）
- [ ] 已激活筛选 chip + × 移除 + 清空所有都工作
- [ ] NodesPage 业务保持：创建节点 / runtime actions / detail navigate / binding conflict / 接入入口都不动
- [ ] `cd web && npm run lint` 0 errors / 0 warnings
- [ ] `cd web && npm run test -- --run` 全绿
- [ ] `cd web && npm run build` 成功
- [ ] `make verify-web` 全绿
- [ ] git diff 范围只在 `web/src/pages/NodesPage.tsx` + `web/src/pages/NodesPage.test.tsx` (+ 任务脚手架)

## Out of Scope

- gap #10 修复（NodesPage createNode 重构进 lib/api）—— 独立 follow-up，且本 task 不允许扩 scope
- EventsPage 应用（child 3）
- 长 page 文件拆分（gap #11）
- 后端 API 加 filter 参数支持
- 引入新组件（仅复用 child 1 已有 5 个）

## Final Confirmation

**Goal**: NodesPage apply FilterBar with §6.3 7 项筛选，复用 child 1 pattern。
**Approach**: 一个 trellis-implement sub-agent，~1-2h（pattern 已建立）。
**Plan**: PR1 = sub-agent 改 NodesPage + test；main commit 1 work + 1 trellis bookkeeping。
