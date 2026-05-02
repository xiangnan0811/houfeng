# Stage 1 P0 quickwins

> Per `docs/release/next-phase-plan.md` Stage 1 P0。本任务打包 3 个 P0 quick-win 修订（每个 30-60 min，独立 commit）。

## Goal

修 `docs/release/v1-gap-checklist.md` "V1 收口期发现的 gap 项 (新增 2026-05-02)" 段中的 3 条 P0 gap：

- **gap #12**: `make verify-web` 加 `npm run lint` —— 让 CI 抓得到 lint 失败
- **gap #3**: `db/migrations/` 0004 序号撞车 —— **rename 不安全**（详见 finding），改为 P0 文档约定项
- **gap #7**: `cmd/houfeng-center/main.go` stdlib `"log"` → `slog` —— 全仓 logger 一致

## What I already know

### gap #12 现状

- `Makefile:65-70`：`verify-web` 当前是 `cd web && npm ci && npm run test -- --run && npm run build`
- 缺：`npm run lint`
- 修：在 `npm run test` 之前加 `npm run lint`（lint 早跑 + 失败早停）
- 1 行改动，无业务影响
- 验证：随便给 `web/src/` 加一个 lint 错误，跑 `make verify-web` 应该 fail；移除后再 fail

### gap #3 现状（**关键 finding**）

- `db/migrations/` 当前两份 0004：
  - `0004_add_node_onboarding_binding_state.sql`（commit 6460fe7）
  - `0004_add_observation_provenance.sql`（commit 4940dba，更早）
- `internal/center/store/migrate/migrate.go:16-19` 关键代码：
  ```sql
  create table if not exists schema_migrations (
    name text primary key,
    applied_at timestamptz not null default now()
  )
  ```
  **`schema_migrations` 用文件名作 primary key**。
- `migrate.Apply` 流程（line 80-101）：扫 embed.FS → 字典序排序 → 对每个文件名查 `schema_migrations` 表 → 没记录就执行 → 记录文件名
- **后果**：若 rename `0004_add_node_onboarding_binding_state.sql` → `0011_*.sql`：
  - 已部署环境的 `schema_migrations` 表里仍有旧名 `0004_add_node_onboarding_binding_state.sql`
  - 下次 startup migrate.Apply 看新文件名 `0011_*.sql` → 不在表里 → **重新执行 CREATE TABLE / ALTER TABLE** → SQL 失败 → 中心启动失败
  - **rename = 破坏所有现存部署**
- 字典序当前排序（实测）：
  ```
  0001_initial_schema.sql
  0002_normalize_status_defaults.sql
  0003_add_sync_token_hash.sql
  0004_add_node_onboarding_binding_state.sql  ← node 在前
  0004_add_observation_provenance.sql          ← obs 在后
  0005_add_node_binding_epoch.sql
  ...
  ```
  字典序工作正常（撞车后两份 0004 仍能稳定排序），既存环境跑得通

### gap #7 现状

- `cmd/houfeng-center/main.go` 4 处 stdlib `log` 用法：
  - line 5: `import "log"`
  - line 17: `log.Fatalf("load center config: %v", err)`
  - line 25: `log.Fatalf("bootstrap center: %v", err)`
  - line 31: `log.Fatalf("run center app: %v", err)`
- 其他文件（worker / handler / runtime）均用 `log/slog`
- `slog` 没有 Fatalf 等价，需拆 2 行：`slog.Error(...) + os.Exit(1)`
- 推荐封装 local helper：
  ```go
  func fatal(msg string, err error) {
      slog.Error(msg, "error", err)
      os.Exit(1)
  }
  ```

## Decision (ADR-lite) — gap #3 处理

**Context**: rename 0004 文件 = 破坏现存部署。但 0004 撞车作为约定违反值得记入文档。

**Decision**: 选 **Option A** — 接受现状 + 仅文档约定（待用户确认）。
- **不动 0004 文件**（保守安全）
- 更新 `docs/release/v1-gap-checklist.md` 第 3 行 gap：标注"已确认无法 rename（schema_migrations 用文件名作主键 + 已部署环境锁定），约定下次 migration 序号从 0011 起"
- 更新 `docs/release/next-phase-plan.md` Stage 1：把 gap #3 从"序号修正"改为"约定下次起 0011"

**Alternatives 不取**：
- Option B：在 migrate.go 加 oldToNew rename map —— 长期维护负担
- Option C：写 0011 SQL `update schema_migrations set name=...` —— 跨环境一致性难

## Open Questions

- ✅ Q1 gap #3 处理 → Option A (仅修文档，不动代码)

## Requirements

1. **gap #12**: `Makefile:65-70` `verify-web` target 改为：
   ```
   cd web && $(NPM) ci && $(NPM) run lint && $(NPM) run test -- --run && $(NPM) run build
   ```
   （在 `npm run test` 之前插入 `npm run lint`）

2. **gap #3** (仅文档):
   - `docs/release/v1-gap-checklist.md` 末尾 Backend 表第 3 条："现象"列保持不变；"证据"列加注："**已确认无法 rename**：`schema_migrations` 用文件名作主键（`migrate.go:16-19`），rename 会让已部署环境 re-apply 失败。**约定下次 migration 序号从 0011 起**（已落地 `.trellis/spec/backend/database-guidelines.md`）"
   - `docs/release/next-phase-plan.md` Stage 1 P0 列表："`db/migrations/` 0004 序号撞车修正" 改为 "约定下次 migration 序号从 0011 起（0004 撞车不动，rename 不安全）"

3. **gap #7**: `cmd/houfeng-center/main.go` 重构：
   - `import "log"` → `import "log/slog"` 和 `"os"`
   - 引入 local helper：
     ```go
     func fatal(msg string, err error) {
         slog.Error(msg, "error", err)
         os.Exit(1)
     }
     ```
   - 3 处 `log.Fatalf("xxx: %v", err)` → `fatal("xxx", err)`

## Final Confirmation

**Goal**: 修 3 个 P0 gap：1 个代码（Makefile 1 行）+ 1 个仅文档 + 1 个代码（main.go 4 处替换）。

**Implementation Plan (3 commits in order)**:
- PR1: `chore(make): add npm run lint to verify-web` (Makefile)
- PR2: `chore(center): unify cmd/houfeng-center logger to slog` (main.go)
- PR3: `docs: clarify gap #3 (migration 0004 collision) cannot be renamed safely` (gap-checklist + next-phase-plan)

**Sub-agent 不能做**：
- 改 db/migrations/ 任何 .sql 文件（gap #3 的 Option A 决策禁止）
- 批量替换其他位置 stdlib log（仅 cmd/houfeng-center/main.go，cmd/houfeng-agent 等不动）
- 改 web/ 业务代码（仅 Makefile）
- 改其他 docs（仅 gap-checklist 第 3 行 + next-phase-plan Stage 1 P0 列表）
- git commit / 跑 task.py

## Acceptance Criteria (evolving)

- [ ] gap #12: Makefile `verify-web` target 在 `npm run test` 前增加 `npm run lint`
- [ ] gap #12 验证：手动 break + 跑 `make verify-web` 应 fail；恢复后绿
- [ ] gap #3: gap-checklist + next-phase-plan.md 文档备注更新
- [ ] gap #7: main.go 4 处 `log.Fatalf` 替换为 slog；引入 `fatal()` helper
- [ ] gap #7 验证：`go vet ./cmd/houfeng-center` + `go build ./cmd/houfeng-center` + `grep "\"log\"" cmd/houfeng-center/main.go` 应 0 命中
- [ ] 3 个 gap 各自独立 commit
- [ ] `make verify-go` + `make verify-web` 全绿（gap #12 修后 verify-web 含 lint）

## Definition of Done

- 所有 commit 通过 verify
- gap-checklist + next-phase-plan 关于 gap #3/#12/#7 的状态更新到一致
- 不动业务逻辑、不改其他 docs

## Out of Scope

- gap #3 不动 0004 文件本身
- 不批量替换全仓其他位置的 stdlib log（如 agent/main.go 也可能有；本 task 只动 cmd/houfeng-center/main.go）
- ~~不改 web/ 业务代码（仅 Makefile）~~ → **scope 扩展**：sub-agent 实证发现 `npm run lint` 在 baseline 上有 4 errors（gap #12 隐性前置），增补 1 个 commit 修 4 处 lint 错误，详见 ## Decision (ADR-lite) — gap #12 scope 扩展

## Decision (ADR-lite) — gap #12 scope 扩展

**Context**: implement sub-agent 实证发现 `npm run lint` 在 main HEAD baseline 上有 4 errors（`web/src/lib/auth-context.tsx:28` react-hooks/set-state-in-effect；`auth-context.tsx:48` + `theme-context.tsx:57,64` react-refresh/only-export-components）。直接 commit gap #12 (verify-web 加 lint) 会让 CI 立即红——违背 gap #12 本意。

**Decision**: 选 **Option A** — 本 task 扩大 scope，**先修 4 errors，再 commit gap #12**。

**Consequences**:
- 4 commits 顺序：(1) `fix(web): clear lint baseline (4 errors)` → (2) `chore(center): unify cmd/houfeng-center logger to slog` → (3) `docs: clarify gap #3 cannot be renamed safely` → (4) `chore(make): add npm run lint to verify-web`
- gap #12 闭环（含必然前置），不留拖尾 task
- 4 errors 都是 react-refresh / react-hooks 类小修，预计 sub-agent 30-45 min 完成

## Technical Notes

- 验证 gap #12 时不需要真 break web 代码——可看 `web/eslint.config.*` 现有规则推断 lint 已稳定通过
- gap #7 替换时 stderr 输出格式会变（log: `2026/05/02 11:30:00 main.go:17: load center config: ...` → slog: `time=2026-05-02T11:30:00 level=ERROR msg="load center config" error=...`）；这是预期，不影响 systemd 抓取
