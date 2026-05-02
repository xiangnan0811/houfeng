# Stage 1 P1 quickwins (gap #4 + gap #10)

> 同 P0 quickwins 模式，2 个低风险 gap 打包，每个独立 commit。

## Goal

修 2 个 P1 gap：
- **gap #4**: `0010_add_users_and_sessions.sql` 索引命名 `sessions_user_idx` / `sessions_expires_idx` 不遵循其他迁移的 `idx_<table>_<purpose>` 规则
- **gap #10**: `web/src/pages/NodesPage.tsx:119` `createNode` 函数 inline `fetch('/api/nodes')` 绕过 `lib/api.ts`

## What I already know

### gap #4 现状

- `db/migrations/0010_add_users_and_sessions.sql` 末尾：
  ```sql
  create index sessions_user_idx on sessions(user_id);
  create index sessions_expires_idx on sessions(expires_at);
  ```
- 与 gap #3 (0004 序号撞车) 不同：rename **索引**通过新 migration 是**安全**的（schema_migrations ledger 跟踪文件名，不是 schema object）
- 修法：加新 0011 migration `ALTER INDEX old RENAME TO new`

### gap #10 现状

- `web/src/pages/NodesPage.tsx:119-150` 内含 80+ 行 inline `createNode` 函数（手写 fetch + JSON 解析 + ApiError 抛出）
- `web/src/lib/api.ts` 已有 `listNodes` (line 133) / `getNode` / `updateNodeMetadata` 等 helper（用 `requestJSON` / `patchJSONBody` 内部抽象）
- `lib/api.ts` 应已有 `postJSONBody` 或类似 helper —— sub-agent 实地查
- 重构方法：在 `lib/api.ts` 加 `export function createNode(input)` 用现有 helper，NodesPage import + 删 inline

## Requirements

1. **gap #4**:
   - 新建 `db/migrations/0011_normalize_sessions_index_names.sql`：
     ```sql
     alter index if exists sessions_user_idx rename to idx_sessions_user;
     alter index if exists sessions_expires_idx rename to idx_sessions_expires;
     ```
   - **不动** 0010_add_users_and_sessions.sql 本身（已 apply，rename 等价 alter）
   - 跑 `make verify-go`（新 migration 不影响 Go 单测）
   - **可选**：在本机 PG `192.168.100.192:5432/houfeng` 触发 0011 apply（重启 center 或直接 psql），验证 `\d sessions` 显示新索引名

2. **gap #10**:
   - 在 `web/src/lib/api.ts` 加 `export function createNode(input: CreateNodeInput): Promise<NodeRecord>`
   - **必须**保留 lifecycle_status='待接入' 默认值（NodesPage 现 inline 加了）—— 在 lib/api.ts 内 spread input 时同样加该默认
   - `web/src/pages/NodesPage.tsx` import createNode from lib/api，删除 line 119-150 的 inline 函数
   - 跑 `make verify-web`（lint + 287 tests + build 全绿）

3. **每 gap 独立 commit**（2 work commits）+ 1 trellis bookkeeping

## Acceptance Criteria

- [ ] `db/migrations/0011_normalize_sessions_index_names.sql` 落地
- [ ] `web/src/lib/api.ts` 含 createNode export
- [ ] `web/src/pages/NodesPage.tsx` 不再含 inline `async function createNode` 和裸 `fetch('/api/nodes')`
- [ ] `make verify-go` 全绿
- [ ] `make verify-web` 全绿
- [ ] git diff 范围只在 `db/migrations/0011_*.sql` + `web/src/lib/api.ts` + `web/src/pages/NodesPage.tsx` (可能 + test)
- [ ] gap-checklist 末尾 12 条新 gap 段中 #4 + #10 行**不更新**（独立 docs task 处理）

## Out of Scope

- gap #9 / gap #11 / 其他 P1 / P2
- smoke 4 caveats 入 gap-checklist
- gap #4 真实环境 PG 触发验证（推荐做但非阻塞——center 启动时自动 apply 即可，本机 verify 是 nice-to-have）

## Final Confirmation

**Goal**: 修 gap #4 + gap #10，2 commits + 1 trellis bookkeeping。
**Approach**: 一个 trellis-implement sub-agent 一次完成。
