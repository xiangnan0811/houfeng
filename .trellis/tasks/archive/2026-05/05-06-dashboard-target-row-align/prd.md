# Dashboard AbnormalTargetList 对齐 watchtower

## Goal

Dashboard 异常目标行身份列 2→3 行（加 freshness）+ 最新延迟 sparkline strip。直接镜像 Dashboard AbnormalNodeList 模式（commit cf7d45b）。

## Background

- Dashboard AbnormalNodeList 已完成 watchtower 对齐（3 行身份列 + CPU/Mem/Disk sparkline strip）
- `/api/targets/sparklines` 接口已完成（commit 65500b5）
- Dashboard AbnormalTargetList 当前 6 列 DataTable，身份列 2 行，有 Hostname + Timestamp，缺 freshness + sparkline
- 极小块：1 文件 ~30 行改动

## Requirements

### AbnormalTargetList 改造

`web/src/pages/DashboardPage.tsx` AbnormalTargetList 函数：

1. **身份列 2→3 行**：第三行加 `最近成功 <Timestamp mode="relative"> · 最近失败 <Timestamp mode="relative">`
2. **原 last-success 列删除**，原位放趋势列（1 个 latency sparkline 64×14 + mono 当前值 + 阈值 tone）
3. **Sparklines 加载**：`useEffect` + `listTargetSparklines()` + silent fail
4. **CSS**：复用既有 `.dashboard-table__freshness` / `.dashboard-table__trends*` 段（已存在，不新增 class）

### 测试

`web/src/pages/DashboardPage.test.tsx`：更新 selector + 新增 ≥1 用例（freshness 行含"成功"文本 或 sparkline polyline）

## Out of Scope

- 其他页面

## Technical Notes

- 文件：`web/src/pages/DashboardPage.tsx` + `DashboardPage.test.tsx`
- 模板参考：同文件 AbnormalNodeList 函数（已完成的同款模式）
