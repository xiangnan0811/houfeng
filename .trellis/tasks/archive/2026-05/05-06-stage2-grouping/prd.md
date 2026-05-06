# Stage 2 Phase 3: 视图分组（group 字段）

## Goal

给 Node / Target 加 `group` 自由文本字段，解决"30 台机器哪台属于哪个业务"的核心组织问题。Dashboard 按组拆分、列表按组过滤。

## Background

- Node 已有 `labels` 自由文本标签（扁平、无层次）
- Dashboard 当前 flat list 异常节点/目标，无分组视图
- 列表已有 6 项筛选栏（NodesPage）+ 6 项（TargetsPage），可直接加 "Group" 筛选

## Decisions

| ID | 决策 |
|---|---|
| Q-FOCUS | **A（视图分组优先）** |
| Q-MODEL | **A（自由文本，同 labels 模式）** |

## Requirements

### 1. DB migration

`0013_add_node_target_group.sql`：
```sql
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS "group" TEXT NOT NULL DEFAULT '';
ALTER TABLE targets ADD COLUMN IF NOT EXISTS "group" TEXT NOT NULL DEFAULT '';
```

### 2. 后端

- Go Record struct 加 `Group string` 字段（JSON: `"group"`）
- NodeRecord / TargetRecord TypeScript type 加 `group: string`
- `POST/PATCH` create/update handler 接受 group 字段
- `GET /api/nodes` / `GET /api/targets` 返回 group

### 3. 前端

**列表页**：
- NodesPage / TargetsPage 列定义加 `group` 列（位置：标签列旁或独立列）
- 筛选栏加 Group 筛选（`FilterSelect` 从已有 group 值自动派生 options，类似 labels 筛选）

**详情页**：
- NodeDetailPage watchtower 身份条 row 2（元数据条）加 group 显示
- TargetDetailPage 同

**Dashboard**：
- AbnormalNodeList / AbnormalTargetList 加 group 列或合入位置列（`group · region · city`）

**创建/编辑表单**：
- Node 创建表单加 group 输入字段
- Target 创建表单加 group 输入字段
- 内联编辑标签时也可编辑 group

### 4. Dashboard stat strip 按 group 拆分

当前 5 个 stat tile（风险对象/严重/维护/新增异常/恢复）。不拆——stat strip 保持全局语义。新增一个 DetailSection "按 Group 分布"：

```tsx
<DetailSection eyebrow="分组视图" title="按 Group 分布">
  <div className="summary-grid">
    {groupSummaries.map(g => (
      <article className="summary-card" key={g.group}>
        <p className="summary-card__label">{g.group || '未分组'}</p>
        <p className="summary-card__value">
          节点 {g.nodeCount} / 目标 {g.targetCount}
          {g.abnormalCount > 0 ? ` · 异常 ${g.abnormalCount}` : ''}
        </p>
      </article>
    ))}
  </div>
</DetailSection>
```

`groupSummaries` 从 `overview.abnormal_nodes` + `overview.abnormal_targets` 前端聚合。

## Acceptance Criteria

- [ ] Node / Target 有 `group` 字段（DB + API + 前端 type）
- [ ] 列表页列定义含 group + 筛选栏含 Group 过滤
- [ ] 详情页身份条元数据行含 group
- [ ] 创建/编辑表单可设 group
- [ ] Dashboard "按 Group 分布" section 新增
- [ ] lint / test / build 全绿（基线 390）
- [ ] make verify-go 全绿

## Out of Scope

- 批量操作（留后续 Phase）
- 模板化探针
- Group 的 Settings 管理页
- 层级分组（嵌套 group）

## Technical Approach

单 PR（改动内聚，体积中等）：
- 1 migration + 2 Go models + 4 前端页面改动 + Dashboard section

## Technical Notes

- 改造文件：`web/src/pages/NodesPage.tsx` / `TargetsPage.tsx` / `NodeDetailPage.tsx` / `TargetDetailPage.tsx` / `DashboardPage.tsx`
- 后端：`internal/center/nodes/types.go` / `internal/center/targets/types.go` + store 对应 scan
- Migration: `db/migrations/0013_add_node_target_group.sql`
