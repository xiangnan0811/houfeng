# Stage 2 Phase 2: 脚本执行 / 远程操作

## Goal

让候风从"只能看不能动"升级为能在页面上对节点执行命令的运维工具。Sync-based + Agent 白名单防线。

## Background

- Agent 已有 SyncPlan 机制（每次 sync Center 返回 Plan，Agent apply）
- 命令注入通道：SyncPlan 加 `PendingAction`
- V1 baseline "通用脚本执行划在外"——Stage 2 破界

## Decisions

| ID | 决策 |
|---|---|
| Q-MODEL | **A（Sync-based）**：SyncPlan 加 PendingAction，Agent sync 拿到执行，结果下个 batch 回传 |
| Q-SECURITY | **B（白名单 + 无 Shell + 超时+输出限制）**：白名单 hardcode agent 编译时，`exec.Command` 不经过 shell，30s timeout / 64KB output cap |
| Q-UX | **A（扩展 watchtower 操作 popover）**：右下"…"菜单加「执行命令…」→ command picker |
| Q-PRESET | 8 命令：`df -h` / `free -m` / `uptime` / `top -bn1 \| head -20` / `journalctl -u <service> --lines=50` / `systemctl status <service>` / `dmesg --level=err` / `docker ps` |

## End-to-end 数据流

```
User clicks "执行命令" → Pick "df -h" → Center POST /api/nodes/{id}/actions
  → Store writes PendingAction (command_id="df_h", node_id=nd_001)
  → Next agent sync: Center finds pending action → puts in SyncPlan
  → Agent receives SyncPlan with PendingAction{command_id: "df_h"}
  → Agent looks up whitelist[df_h] = {bin:"df", args:["-h"]}
  → Agent exec.Command("df", "-h"), timeout 30s, cap output 64KB
  → Agent puts CommandResult in next SyncRequest
  → Center stores result → Next frontend poll returns result
```

## Requirements

### PR1：Agent 端（最核心，安全防线在这里）

#### 新建 `agent/exec/` package

`agent/exec/whitelist.go`：
- 硬编码白名单 map：`commandID → {bin: string, args: []string}`
- `Lookup(id string) (bin string, args []string, ok bool)` 函数
- 8 个 MVP 命令全注册

`agent/exec/runner.go`：
- `RunCommand(ctx context.Context, bin string, args []string) (stdout string, stderr string, exitCode int, err error)`
- `exec.CommandContext(ctx, bin, args...)` 直接执行，不经过 shell
- ctx 带 30s timeout
- stdout+stderr 合并，cap 64KB（超过截断 + `[output truncated at 64KB]`）
- 返回 exit code（非 0 不报 error，正常返回）

#### 改 `agent/runtime/runtime.go`

`applySyncPlan()` 扩展：
- 如果 `plan.PendingAction != nil` → 调 `exec.Lookup(action.CommandID)` → `exec.RunCommand()` → 将结果存入 `runtime.pendingResult`
- `pendingResult` 在下次 sync 时作为 `SyncRequest.CommandResults` 数组回传

#### 改 `agentapi/types.go`

```go
type PendingAction struct {
    CommandID string `json:"command_id"`
    ActionID  string `json:"action_id"`   // unique per action，用于匹配 response
}

type CommandResult struct {
    ActionID string `json:"action_id"`
    Stdout   string `json:"stdout"`
    Stderr   string `json:"stderr"`
    ExitCode int    `json:"exit_code"`
}

// SyncPlan 加字段：
type SyncPlan struct {
    // ... existing fields ...
    PendingAction *PendingAction `json:"pending_action,omitempty"`
}

// SyncRequest 加字段：
type SyncRequest struct {
    // ... existing fields ...
    CommandResults []CommandResult `json:"command_results,omitempty"`
}
```

#### Agent 编译与部署

白名单是编译时 hardcode，加新命令 = 重新编译 agent binary → 重新部署。Stage 2 MVP 接受这个代价。未来可考虑从 center 下发的"动态白名单"（agent 启动时 fetch），但暂不实现。

### PR2：Center 端

#### 新建 handler

`internal/center/http/handlers/node_actions.go`：
- `POST /api/nodes/{id}/actions` — body `{command_id: "df_h"}`
- 验证 node 存在 + agent 已绑定 + 不在暂停态
- 写入 nodes 表的 `pending_action_id` 和 `pending_action_command_id` 字段（或新建 `node_actions` 表）
- 返回 `{action_id: "act_xxx"}`

**简化方案（不需要新表）**：nodes 表加两个 nullable 列 `pending_action_id text` / `pending_action_command_id text`。下发后清空这两个字段。center 在构建 SyncPlan 时检查这两个列。

#### 改 syncing pipeline

`internal/center/syncing/` 或对应的 sync handler：

- 构建 SyncPlan 时检查该 node 是否有 pending action
- 有 → SyncPlan.PendingAction = {CommandID, ActionID}
- 清空 nodes 表的 pending 列（标记已下发）

#### 处理 command result

`POST /api/agent/sync` handler 收到 SyncRequest 时：
- 解析 `CommandResults[]`
- 存入 nodes 表新列 `last_action_result` (JSON text) 或返回给前端 frontend poll

**返回给前端**：
- `GET /api/nodes/{id}/actions/{action_id}` → 返回 `{status: "pending"|"done", stdout: "...", stderr: "...", exit_code: N}`
- 前端隔 2s poll 一次，直到 status=done

简化方案：不用独立 endpoint。action result 直接返在 `GET /api/nodes/{id}` NodeRecord 的扩展字段 `last_action: {action_id, command_id, status, stdout, stderr, exit_code} | null`。前端不需要额外 poll。

### PR3：前端

#### NodeDetailPage 操作 popover 扩展

"…" popover 倒数第一个按钮（在维护/暂停按钮下方）：`<Button variant="ghost">执行命令…</Button>`

点击后打开 command picker（用 `<details>` 原生 popover 或 `<Drawer>` 面板）：
- 8 个预置命令以卡片/列表展示：命令名 + 简描述
- 点某个命令 → 触发 `POST /api/nodes/{id}/actions` + 显示 "已下发，等待 agent 执行…"
- 结果返回后（poll NodeDetailPage 的 node 数据每 2s），在 picker 里渲染 `<pre>` 输出

简化实现：不用独立 picker，直接弹 `<Drawer>`（已有原子），左侧命令列表 + 右侧输出区。

渲染命令输出：
```tsx
<pre className="command-output"><code>{result.stdout}{result.stderr ? `\n\n[stderr]\n${result.stderr}` : ''}</code></pre>
```
exit_code ≠ 0 时加一行红字 `exit code: {code}`。

#### CSS

```css
.command-output {
  background: var(--bg-sidebar);
  border: 1px solid var(--border);
  border-radius: var(--radius-2);
  padding: var(--space-3);
  font-family: var(--font-mono);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 480px;
  overflow: auto;
  color: var(--text-primary);
}
```

### 不改动

- TargetsPage / Dashboard / Events / Settings
- 列表页
- 多节点批量执行

## Acceptance Criteria

### PR1 Agent
- [ ] whitelist 含 8 个 command ID，Lookup 正确
- [ ] `RunCommand` 直接 exec，不经过 shell
- [ ] 超时 30s 后 kill 进程不 hang
- [ ] 输出超 64KB 时截断 + 标注
- [ ] agent 无 pending action 时 sync 行为不变（零回归）
- [ ] `go test ./agent/exec/...` + `go test ./agent/runtime/...` 全绿

### PR2 Center
- [ ] `POST /api/nodes/{id}/actions` 可用
- [ ] SyncPlan 含 PendingAction 字段，agent sync 时正确下发
- [ ] SyncRequest 含 CommandResults 字段，正确解析存储
- [ ] `GET /api/nodes/{id}` 返回 last_action 字段
- [ ] go test 全绿

### PR3 Frontend
- [ ] watchtower "…" popover 含「执行命令…」
- [ ] command picker 列出 8 个预置命令
- [ ] 执行后显示 "已下发…" → 结果出现后渲染 `<pre>` 输出
- [ ] lint / test / build 全绿

## Out of Scope

- 交互式 shell / 终端模拟器
- 文件上传/下载
- 多节点批量执行
- 定时任务/cron
- 命令参数自由输入（首期仅 command ID 选择，service name 固定为常用值）
- WebSocket 实时通道
- 动态白名单（agent 编译时 hardcode）

## Technical Approach

3 PR 拆分。PR1 Agent（安全防线）→ PR2 Center（pipe）→ PR3 Frontend（UX）。每 PR 独立可验证。

**agent/exec/ 是本次最关键的代码**——它承载安全边界。测试覆盖必须充分：whitelist lookup、正常执行、超时 kill、输出截断、命令不存在。
