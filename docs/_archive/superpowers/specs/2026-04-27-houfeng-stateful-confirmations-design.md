# Houfeng Stateful Confirmations Design

## Context

Frozen V1 separates quick reversible runtime controls from high-risk actions. The current frontend still uses browser-native `window.confirm` for several high-risk flows:

- Node pause from Node list
- Node pause from Node detail
- Target pause/archive from Target list
- Target pause/archive from Target detail
- ProbeItem delete from Target detail

Those confirms technically block accidental clicks, but they do not satisfy the frozen V1 confirmation baseline. V1 confirmation surfaces must explain current state, operation result, impact, and what will not change. Browser confirms also cannot be styled to the Unified / Baseline visual system.

Node retirement already uses an inline confirmation card, so this slice brings the remaining runtime/delete confirmations to the same stateful pattern.

## Scope

Implement frontend-only stateful confirmation cards for the remaining browser-confirmed actions:

1. Node pause in `NodesPage`
2. Node pause in `NodeDetailPage`
3. Target pause and archive in `TargetsPage`
4. Target pause and archive in `TargetDetailPage`
5. ProbeItem delete in `TargetDetailPage`

Out of scope:

- Backend API changes
- New action semantics
- Confirmation for light actions (`进入维护`, `退出维护`, `恢复监控`, `恢复到暂停`)
- Generic modal framework
- Bulk operations

## Chosen approach

Use local inline confirmation cards instead of a new modal system.

Why:

- Existing pages already use page panels, detail sections, row-local copy, and inline error placement.
- Inline cards avoid focus-trap/modal infrastructure that would be larger than this V1 gap.
- List pages can keep the confirmation near the affected row, which makes object identity explicit.
- Detail pages can keep confirmation inside the existing runtime or ProbeItem section.

## Shared confirmation contract

Every confirmation card must contain:

- a clear title
- current state
- operation result
- impact
- what will not change
- a final confirm button
- a cancel button

- lightweight announcement/focus behavior so the newly opened confirmation is exposed with dialog semantics and receives focus

The confirm card must replace `window.confirm`; tests should spy on `window.confirm` and assert it is not called.

## Copy

Use these exact action titles and button labels where applicable:

### Node pause

- Title: `确认暂停节点监控`
- Current: `当前：监控运行状态为启用。`（当行当前为启用）
- Current: `当前：监控运行状态为维护中。`（当行当前为维护中）
- Result: `操作后：监控运行状态变为暂停。`
- Impact: `会停止主机指标采集，并停止该节点承担的探针执行。趋势图会从此开始出现数据空档。`
- Unchanged: `不会删除历史事件、观测记录或 agent 绑定关系。`
- Confirm: `确认暂停监控`
- Cancel: `取消`

### Target pause

- Title: `确认暂停目标监控`
- Current: `当前：目标运行状态为启用或维护中。`
- Result: `操作后：目标运行状态变为暂停。`
- Impact: `会停止该 Target 下所有 ProbeItem 的执行，不再产生新的目标 observation。`
- Unchanged: `不会删除历史事件、观测记录或 ProbeItem 配置。`
- Confirm: `确认暂停目标`
- Cancel: `取消`

### Target archive

- Title: `确认归档目标`
- Current: `当前：目标仍在当前工作集中。`
- Result: `操作后：目标退出当前工作集，运行状态变为已归档。`
- Impact: `归档后不会继续作为活跃目标参与观测、异常判定或通知。`
- Unchanged: `不会删除历史事件、观测记录或 ProbeItem 配置。后续可恢复到暂停。`
- Confirm: `确认归档`
- Cancel: `取消`

### ProbeItem delete

- Title: `确认删除 ProbeItem`
- Current: `当前：这条 ProbeItem 仍属于当前 Target。`
- Result: `操作后：这条观测方式会被移除。`
- Impact: `仅用于误建场景。删除后该 ProbeItem 不再产生新的 observation。`
- Unchanged: `不会删除 Target，也不会删除既有事件或历史观测记录。`
- Confirm: `确认删除 ProbeItem`
- Cancel: `取消`

## Page behavior

### List pages

For `NodesPage` and `TargetsPage`:

- Clicking a high-risk row action opens a confirmation card in that row.
- Only one confirmation can be open per page.
- Clicking another high-risk row action moves the confirmation to the newly selected row/action.
- Cancel closes the card and does not call the API.
- Confirm calls the same existing API and then closes the card after a successful state update.
- If the API fails, keep the card visible and show the existing local row error.

### Detail pages

For `NodeDetailPage` and `TargetDetailPage`:

- Clicking a high-risk runtime action opens a confirmation card in the Runtime Control section.
- Cancel closes the card and does not call the API.
- Confirm calls the existing API and closes the card after a successful state update.
- If the API fails, keep the card visible and show the existing local error.
- Route-stale guards remain unchanged.

For ProbeItem delete:

- Clicking `删除` opens a card near the ProbeItem controls.
- The card includes the same configured ProbeItem summary already used in the row.
- Confirm calls the existing delete API.
- Cancel closes the card and does not call the API.
- Existing row-mutation serialization and stale-route guards remain unchanged.

## Testing strategy

Add focused frontend tests that lock:

1. Node list pause opens the inline confirmation, cancels without API, confirms through the existing pause API, and never calls `window.confirm`.
2. Node detail pause uses the same inline confirmation and does not call `window.confirm`.
3. Target list pause/archive use inline confirmations and do not call `window.confirm`.
4. Target detail pause/archive use inline confirmations and do not call `window.confirm`.
5. ProbeItem delete uses inline confirmation and does not call `window.confirm`.

Run:

```bash
cd web && npm test -- --run NodesPage NodeDetailPage TargetsPage TargetDetailPage
cd web && npm run build
```

Then run full repository verification before marking the slice complete.

## Self-review

- This closes a frozen V1 interaction gap without changing product capability.
- The scope is frontend-only and uses existing APIs.
- The design keeps light actions fast and only replaces high-risk browser confirms.
- It avoids adding a generic modal system before V1 needs one.
