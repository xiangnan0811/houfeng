# 代码质量规范

> **权威来源**：`CLAUDE.md` + 业务/结构以 `docs/design/v1-baseline/` frozen 子集（architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow）为准，视觉以 `docs/design/v2-houfeng/`（design-language / component-spec）为准。冲突时以前述顺序为准。本项目处于初始开发阶段，规则会随代码演进调整。

---

## Overview

候风后端的质量门是一条命令：`make verify-go`（=`fmt-go` + `vet-go` + `test-go`）。**CI 与本地跑同一套 Makefile target**，`.github/workflows/ci.yml` 第 17 行直接 `run: make verify-go`，没有任何 CI-only lint 偏方。

跨前端改动（同时动了 web/）使用 `./scripts/verify.sh` 一把跑完前后端，等价于 `make verify-go && make verify-web`（`scripts/verify.sh` 文件总共只有 5 行）。

不存在以下东西，**不要新增**（除非通过设计基线评审）：

- ❌ `golangci-lint`（仓库根 `find . -name '.golangci*' -maxdepth 3` 为空）
- ❌ pre-commit / husky / lefthook（同样 `find` 为空）
- ❌ goimports 单独配置（`go fmt` 已经统一格式）
- ❌ 任何 ORM、SQL 生成器（参见 `.trellis/spec/backend/database-guidelines.md`）

---

## 命令门户

| 命令 | 实际行为 | 来源 |
|------|----------|------|
| `make fmt-go` | `go fmt $(go list ./agent/... ./cmd/... ./db/... ./internal/...)` | `Makefile:11-21` |
| `make vet-go` | 同范围 `go vet` | `Makefile:35-45` |
| `make test-go` | 同范围 `go test` | `Makefile:23-33` |
| `make verify-go` | `fmt-go + vet-go + test-go` | `Makefile:63` |
| `make build-center` | 当 `cmd/houfeng-center/*.go` 存在时 `go build -o ./bin/houfeng-center ./cmd/houfeng-center` | `Makefile:47-53` |
| `make build-agent` | 同上，输出 `./bin/houfeng-agent` | `Makefile:55-61` |
| `make verify-web` | `cd web && npm ci && npm run test -- --run && npm run build` | `Makefile:65-70` |
| `make verify` | `./scripts/verify.sh` 即 `verify-go + verify-web` | `Makefile:72-73`、`scripts/verify.sh` |

**注意**：

- `make test-go` **不带 `-race`**。新加并发原语时如果担心 race condition，可以本地手工跑 `go test -race ./...`，但默认 verify 链路不开。
- `verify-go` 的范围是 `./agent/... ./cmd/... ./db/... ./internal/...`，不包括 `web/`、`docs/`、`scripts/`。新增 Go 顶层目录时记得改 Makefile 第 7 行 `GO_VERIFY_PATTERNS`。
- 单测可用 `go test ./internal/center/store -run TestPostgresNodeRepository` 这种点对点形式（`CLAUDE.md` 第 37-38 行）。

---

## 测试约定

### 测试文件位置 / 命名

- 单元测试与被测文件**同目录同包**：`store/sync_batches.go` ↔ `store/sync_batches_test.go`。
- 跨包黑盒测试用 `<package>_test` 包名：`internal/center/http/handlers/nodes_test.go` 第 1 行 `package handlers_test`。
- 端到端测试加 `_e2e_test.go` 后缀：唯一例子 `internal/center/http/auth_e2e_test.go`，配套 helper `auth_e2e_helpers_test.go`。
- 路由级集成测试单独文件：`internal/center/http/router_api_test.go`、`router_test.go`。

### Table-driven 测试

约定为**主流**写法（凡是有 ≥3 个相似 case 的场景）。模板（来自 `internal/center/http/handlers/nodes_test.go:302-339`）：

```go
tests := []struct {
    name string
    body string
}{
    {name: "empty object", body: `{}`},
    {name: "labels only", body: `{"labels":["edge"]}`},
    {name: "note only", body: `{"note":"updated"}`},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // ...
    })
}
```

变量名约定：

- 切片变量名 `tests`（或 `testCases`，见 `targets_test.go:514`）。
- 循环变量 `tt`（主流，`nodes_test.go`、`targets_test.go`、`node_onboarding_test.go`）或 `tc`（见 `targets_test.go:514`）。新代码沿用 `tt`。
- 用 `t.Run(tt.name, func(t *testing.T) { ... })` 子测试，便于 `-run TestX/case_name` 精准触发。

### `t.Parallel()`

**纯函数 / 无共享状态**的测试可以加 `t.Parallel()`：参见 `internal/center/settings/types_test.go:13`、`internal/center/store/nodes_test.go:20`、`internal/center/enrollment/service_test.go:13`。

涉及 `httptest.NewRecorder` + 内存 fake repository 的 handler 测试**当前没有用 `t.Parallel()`**——保持现状即可，不要为了快几毫秒强加 parallel。

### Handler 测试

模式 = `httptest.NewRequest` + `httptest.NewRecorder` + `handler.ServeHTTP(recorder, req)` + 直接断言 `recorder.Code` / `recorder.Body`。**不启动真实 HTTP server**。仓库依赖通过手写 `fake<X>Repository` struct 实现领域接口，例如 `internal/center/http/handlers/nodes_test.go:17-57` 的 `fakeNodeRepository`。

**路由级**测试用 `centerhttp.New(centerhttp.RouterOptions{...})` 真实拼装 mux，但所有 handler 字段塞 `http.HandlerFunc(...)` 假实现，仅校验 SPA fallback / 路由优先级（参见 `router_api_test.go` 全文）。

### Store 测试

**当前所有 `internal/center/store/*_test.go` 均使用 fake `pgx.Tx` / `pgx.Row` 实现接口**，**不依赖真实 Postgres**。`grep -l "OpenPostgres\|pgxpool.New" internal/center/store/*_test.go` 结果为空。

参考模板 `internal/center/store/sync_batches_test.go:175-260` 的 `fakeSyncBatchTx`：

- 实现 `Exec` / `QueryRow` / `Query` / `Commit` / `Rollback`，记录调用。
- 通过 `execErrForSQLSubstring` 等字段按 SQL 文本子串决定哪一步注入错误，验证事务 rollback 行为。
- 仓库构造时直接替换 `beginTx` 字段（`PostgresSyncRepository.beginTx`，见 `store/sync_batches.go:30-36`），跳过真 pgxpool。

**真实 Postgres 烟囱测试不在 verify 链路里**，由 `docs/operations/v1-smoke-run.md` 的人工 fresh-install 流程补齐。新加 store 方法时如果只能靠真 DB 验证（例如 trigger / 外键级联），应在 `docs/operations/` 下补描述，不要把 verify 弄成"必须有本地 Postgres"。

### Bootstrap 装配测试

`cmd/houfeng-center/bootstrap_test.go` 用工厂替换模式：把 `bootstrapDeps` 内每个工厂（`openPostgres` / `applyMigrations` / `seedInitialUser` / `newIncidentNotifier` / `newRouter` / `newApp`）替成测试桩，验证：

1. 失败路径正确传播错误并清理资源（`TestBootstrapCenterReturnsOpenPostgresError`、`TestBootstrapCenterClosesDBOnMigrationFailure`）。
2. 成功路径把每一个 `RouterOptions` 字段都塞了 handler（`TestBootstrapCenterBuildsAppOnSuccess` 内逐个 `if gotOpts.X == nil { t.Fatal(...) }`，行 177-238）。

**新增 handler / worker 时必须更新 bootstrap_test 的成功用例**，否则装配遗漏不会被 verify 抓到。

### Worker 测试

worker（retention、auth/cleanup、incidents、agent runtime）测试通过：

- 注入小间隔（`time.Millisecond`）与 `t.Cleanup(cancel)` 让 worker 跑 1-2 轮就退出，参考 `retention/worker_test.go:92`、`auth/cleanup_test.go:24`。
- 注入 `slog.New(slog.NewTextHandler(io.Discard, nil))` 抑制 log 噪音，或 `slog.New(slog.NewTextHandler(&logs, nil))` 捕获 log 内容做断言（`retention/worker_test.go:133/169/204`）。

### 特殊：syncqueue fsync

`agent/syncqueue/store.go:22-32` 的 `Options.SkipFsync` 是**测试专用开关**，避免 macOS APFS fsync 拖慢运行时计时类测试（CLAUDE.md 提到的"slow filesystems"问题，最近一次修复见 `git log -- agent/syncqueue/`）。

约定：

- **生产调用必须留 `SkipFsync = false`**（默认零值），保证崩溃恢复语义。
- 测试里如果跑的是运行时 retry / 重启逻辑、不在乎崩溃恢复，可以显式置 `SkipFsync: true`。
- 不要把 `SkipFsync` 暴露到 env 配置里。

---

## 工具链

### `go fmt`

- 强制：`make fmt-go` 是 verify 链路第一步，未格式化代码 CI 直接挂。
- 不引入 `goimports` 单独 hook：`go fmt` 已经够用，import 顺序按 stdlib → 三方 → houfeng 内部三段，靠手动维持（参考 `cmd/houfeng-center/bootstrap.go:1-30` 的 import 块写法）。

### `go vet`

- 强制：`make vet-go` 抓 printf-arg 不匹配、shadow 变量、未对齐 struct tag 等。
- 当前没有 `//go:vet` 跳过指令，也没有 `// nolint` 注释。**新代码不要随便加忽略**。

### `go test`

- `go test` 默认开 `-cover` 不输出，CI 也不强制 coverage 阈值。**不要在测试文件里写 `t.Skip("coverage too low")` 这种 hack**。
- 启用 race 检测靠手动 `go test -race ./...`，不在 verify 链路。

### Go 版本

`actions/setup-go@v5` 通过 `go-version-file: go.mod`（`.github/workflows/ci.yml:16`）锁定。本地 Go 必须 ≥ `go.mod` 声明的版本。

---

## 提交前清单

下面这条清单是 happy-path，按顺序勾：

1. [ ] **`make verify-go`** —— 永远必须通过。fmt 修了直接重跑。
2. [ ] **改了 `web/` 里任何文件 → `cd web && npm run lint && npm run test`**（CLAUDE.md 第 31 行）。
3. [ ] **同时改了前后端 → `./scripts/verify.sh`** 一把跑完。
4. [ ] **改了迁移 / 表结构** → 跑一次 `docs/operations/v1-smoke-run.md` 的 fresh-install，补 `docs/release/v1-gap-checklist.md`。
5. [ ] **改了 user-visible 的 UI** → 按 `docs/operations/v1-visual-verification.md` 录截图到 `docs/operations/visual-evidence/`，**不要回写 `docs/design/v1-baseline/`**（基线已冻结）。
6. [ ] 如果 worker / 调度类改动，本地用注入的小间隔跑 `go test -count=10` 看下抖动。

---

## 跨层一致性（PR review checklist）

下面是 reviewer 必看的"改一处必带的另一处"。代码已经按这个习惯写，不要破坏：

| 改动 | 必须连带的修改 |
|------|----------------|
| 新增 HTTP endpoint | 1) `internal/center/http/handlers/<resource>.go` handler 工厂；2) `internal/center/http/router.go` 的 `RouterOptions` 字段 + mux 注册；3) `cmd/houfeng-center/bootstrap.go` `bootstrapCenter` 显式构造并塞进 `RouterOptions`；4) `cmd/houfeng-center/bootstrap_test.go` 的 `TestBootstrapCenterBuildsAppOnSuccess` 增 nil 断言；5) `internal/center/http/handlers/<resource>_test.go` 增 table-driven handler 测试；6) `internal/center/http/router_api_test.go` 增 SPA fallback 隔离测试 |
| 新增/修改 agent ↔ center 字段 | 1) `internal/contracts/agentapi/types.go` 改 DTO；2) center 端在 `internal/center/syncing/` 或对应 handler 处理；3) **agent 端在 `agent/runtime/` 或采集子包同 PR 内改完**；4) 两侧测试同 PR 通过 |
| 新增领域 sentinel error | 1) `internal/center/<domain>/types.go` 加 `Err...`；2) handler 加 `errors.Is` case + `agentapi.ErrorCode*` 映射（如属 agent endpoint） |
| 新增持久化字段 / 表 | 走 `database-guidelines.md` 的 4 步流程；reviewer 在 PR 内确认迁移序号未撞车 |
| 引入新 worker | 1) `internal/center/<x>/worker.go` 实现 `Worker.Run(ctx) error`；2) `cmd/houfeng-center/bootstrap.go` 添加构造与传给 `centerapp.New(...)`；3) `bootstrap_test.go` 的 `TestBootstrapCenterBuildsAppOnSuccess` 把 `len(workers)` 期望值从 N 改为 N+1（**当前为 3**：incident、retention、session cleanup） |
| agent 端新采集 / 新探针 | 1) `agent/hostsample/` 或 `agent/probe/` 实现采集；2) 通过 `agent/runtime/runtime.go` 的 `buildSyncRequest` 串接；3) 必要时改 `internal/contracts/agentapi/` DTO（不可单边） |

---

## 反模式 / Common Mistakes

- ❌ **跳过 verify 提交**：哪怕"只是改了一行注释"也跑 `make verify-go`，3 秒的事。
- ❌ **`git commit --no-verify`**：当前仓库**没有** pre-commit hook，但如果哪天加了，禁止用 `--no-verify` 绕过。
- ❌ **CI 红了直接 force-push 改一行**：先在本地复现 `make verify-go`，找到根因再提 commit。
- ❌ **TODO 不带 issue 链接**：`grep -rn "TODO\|FIXME" internal/ agent/ cmd/` 当前为空，保持纪录干净。如果必须 TODO，写完整理由 + 跟踪 issue 编号。
- ❌ **新增 handler 但不更新 `bootstrap_test.go` 的 nil 断言**：会让"装配缺失"绕过 verify。
- ❌ **测试用 `time.Sleep(N seconds)` 等 worker tick**：用注入小间隔 + ctx cancel 的 deterministic 模式（参考 `retention/worker_test.go:92`）。
- ❌ **store 测试为了"覆盖更全"启动真 Postgres**：当前生态依赖 `fakeSyncBatchTx` 风格。如果确实要写真 DB 测试，单独走 `_e2e_test.go` 后缀并默认 `t.Skip` 在 env 缺失时跳过——但这条**目前还没人做过**，先和团队确认再加。
- ❌ **改 contract 包但不同 PR 改 agent**：`internal/contracts/agentapi/` 的任何 breaking 改动**必须**当 PR 把 agent 也升级，否则 fleet 会立即崩。
- ❌ **改 `db/migrations/` 已合入的 SQL 文件**：见 `database-guidelines.md`。reviewer 看到这种 diff 应直接 reject。

---

## 已知 gap

- 仓库**没有** `golangci-lint`、没有 race-by-default、没有 coverage 阈值。如果未来引入，应同步更新 `.github/workflows/ci.yml`、`Makefile`、本文件，并在 `docs/release/v1-gap-checklist.md` 记录变更。
- `make verify-web` 跑 `npm ci`，每次清空 `node_modules` 后重装，本地反复跑会比较慢；若需要本地速度，单独 `cd web && npm test` / `npm run lint` 即可，CI 仍然走完整 `verify-web`。
- `cmd/houfeng-center/main.go` 启动期失败仍走 stdlib `log.Fatalf`，与全仓 `log/slog` 不一致——见 `logging-guidelines.md` 的同款 gap，质量 reviewer 不要把这个当 lint 错误反复提。
