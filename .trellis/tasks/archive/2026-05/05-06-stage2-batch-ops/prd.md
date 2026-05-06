# Stage 2 Phase 4: 批量操作（基于 group）

## Goal

Phase 3 group 字段让用户能"看清"。Phase 4 让它能"动"：筛选 group → 全选 → 批量维护/暂停/命令执行。

## Decisions

| ID | 决策 |
|---|---|
| Q-SCOPE | 1+2（批量维护/暂停/恢复 + 批量命令执行） |
| Q-UI | A（select-all toggle + action bar） |

## Requirements

### 1. 后端：`POST /api/nodes/batch`

`internal/center/http/handlers/node_batch.go`：

```go
type BatchActionRequest struct {
    NodeIDs []string `json:"node_ids"`
    Action  string   `json:"action"` // "enter-maintenance" | "exit-maintenance" | "pause" | "resume"
}

type BatchActionResult struct {
    NodeID string `json:"node_id"`
    OK     bool   `json:"ok"`
    Error  string `json:"error,omitempty"`
}
```

遍历 `node_ids`，对每个调已有 repo 的单节点 action 方法。返回 `[]BatchActionResult`。

- 验证 node 存在 + 可执行该 action
- 单个 node 失败不 block 其他
- 1 个事务包所有，还是每个 node 独立事务？——独立事务（单 node 失败不回滚其他）

### 2. 前端 select-all toggle + action bar

**NodesPage**：列表 DataTable 上方加一行：

```tsx
{groupFilterActive ? (
  <div className="batch-bar">
    <Toggle checked={selectAll} onChange={setSelectAll} label={`全选 (${filteredNodes.length})`} />
    {selectAll && filteredNodes.length > 0 ? (
      <div className="batch-bar__actions">
        <Button variant="secondary" size="sm" onClick={batchAction('enter-maintenance')}>进入维护</Button>
        <Button variant="secondary" size="sm" onClick={batchAction('exit-maintenance')}>退出维护</Button>
        <Button variant="secondary" size="sm" onClick={batchPause}>暂停监控</Button>
        <Button variant="secondary" size="sm" onClick={batchResume}>恢复监控</Button>
      </div>
    ) : null}
  </div>
) : null}
```

groupFilterActive：仅当 group 筛选已激活（用户选了一个具体 group）时，才显示批量操作栏（非 group 筛选状态下批量操作无意义）。

提交前弹 ConfirmationCard："将对 N 个节点执行 X"，确认后调 API。每个 node 独立调已有 API（或调新 batch endpoint）。

批量命令执行：action bar 加一个"执行命令…"按钮 → 打开 command picker（复用 Phase 2 PR3 的 Drawer）→ 选命令 → 对每个 node 调 postNodeAction → 结果显示 "N 个节点已下发，等待 agent 执行"。

### 3. TargetsPage：同模式

Target 的批量操作：进入维护/暂停/恢复（Target 无退役的批量风险考虑）。同样仅 group 筛选激活时显示。

### 4. CSS

```css
.batch-bar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) 0;
  border-bottom: 1px solid var(--border);
  margin-bottom: var(--space-2);
}
.batch-bar__actions {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}
```

### 4. 测试

- handler test ≥2 用例（batch enter-maintenance / batch pause + single failure）
- NodesPage.test.tsx：新增 ≥1 用例（group 筛选后 select-all + batch bar 渲染）
- 现有测试零破坏

## Acceptance Criteria

- [ ] `POST /api/nodes/batch` 可用，接受 node_ids[] + action
- [ ] 单个 node 失败不 block 其他
- [ ] NodesPage group 筛选后 select-all toggle + action bar 出现
- [ ] TargetsPage 同模式
- [ ] 批量命令执行对每个 node 调 postNodeAction
- [ ] ConfirmationCard 二次确认
- [ ] lint / test / build 全绿（基线 390）
- [ ] Go test 全绿

## Out of Scope

- 批量 ProbeItem 模板
- 批量退役
- Target 批量执行的 batch endpoint（用已有单 target API 逐调）
- 非 group 筛选下的批量操作
- 进度条/实时反馈

## Technical Notes

- 后端 handler：`internal/center/http/handlers/node_batch.go`
- Router 注册 + bootstrap wiring
- 前端：NodesPage / TargetsPage 的 DataTable 上方 action bar
