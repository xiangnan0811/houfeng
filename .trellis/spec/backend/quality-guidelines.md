# 代码质量规范

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

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
| `make verify-web` | `cd web && npm ci && npm run lint && npm run test -- --run && npm run build` | `Makefile:65-70` |
| `make verify` | `./scripts/verify.sh` 即 `verify-go + verify-web` | `Makefile:72-73`、`scripts/verify.sh` |

**注意**：

- `make test-go` **不带 `-race`**。新加并发原语时如果担心 race condition，可以本地手工跑 `go test -race ./...`，但默认 verify 链路不开。
- `verify-go` 的范围是 `./agent/... ./cmd/... ./db/... ./internal/...`，不包括 `web/`、`docs/`、`scripts/`。新增 Go 顶层目录时记得改 Makefile 第 7 行 `GO_VERIFY_PATTERNS`。
- 单测可用 `go test ./internal/center/store -run TestPostgresMonitoringInstanceRepository` 这种点对点形式（`CLAUDE.md` 第 37-38 行）。

---

## GitHub Actions workflow 合约

### 1. Scope / Trigger

触发条件：改 `.github/workflows/ci.yml`、`Makefile` 的 verify target、或调整 CI job 条件时，必须保持 GitHub Actions 能先创建实际 job，再由 Makefile 执行质量门。

### 2. Signatures

当前 workflow job 合约：

- `jobs.go.steps[*].run`: `make verify-go`
- `jobs.web.steps[*].run`: `make verify-web`
- Go 版本：`actions/setup-go@v6` + `go-version-file: go.mod`
- Node 版本：`actions/setup-node@v6` + `node-version: 22`
- npm cache dependency path：`web/package-lock.json`

### 3. Contracts

- CI 与本地共用 Makefile target；不要把 lint/test/build 细节复制进 YAML。
- `web` workspace 是否存在由 `make verify-web` 的 shell 判断负责；workflow 不需要再用 `hashFiles('web/package.json')` 判断。
- 如果未来确实需要条件跳过 job，`jobs.<job_id>.if` 只能使用 GitHub Actions 在 job-level 支持的上下文和 status 函数；不要把 step-level/file-hash 表达式搬到 job-level。
- Docker image build verification belongs to GitHub Actions: `ci.yml` keeps a `docker-image` job using `docker/setup-buildx-action@v4` + `docker/build-push-action@v7` with `push: false`, while `publish-images.yml` builds and pushes release images. A local environment without `/var/run/docker.sock` is not evidence that image verification is missing if these workflow jobs remain present.

### 4. Validation & Error Matrix

| 条件 | 预期 | 失败表现 |
|------|------|----------|
| `jobs.<job_id>.if` 使用 unsupported function，例如 `hashFiles(...)` | 禁止 | GitHub Actions run 直接 `failure`，`jobs=[]`，`log not found`，check suite `latest_check_runs_count=0` |
| `.github/workflows/ci.yml` 包含 `hashFiles` | 只有在 step-level 支持位置才允许 | job 创建前失败或表达式校验失败 |
| `make verify-go` / `make verify-web` 失败 | workflow job 应创建并输出日志 | 正常红 job，有可读失败日志 |
| `docker-image` job missing from CI | 禁止 | PR 不再验证 Dockerfile can build |
| local Docker daemon unavailable | 不作为本地阻塞项 | 依赖 GitHub Actions `docker-image` / `publish-images` jobs 验证 |

### 5. Good/Base/Bad Cases

- Good：`web` job 总是创建，`run: make verify-web`；不存在 `web/package.json` 时由 Makefile 输出 `web workspace not initialized yet`。
- Base：`go` job 总是创建，`run: make verify-go`；Go 工具链版本来自 `go.mod`。
- Good：`docker-image` job 总是创建，`docker/build-push-action@v7` 对 root `Dockerfile` 执行 `push: false` 构建。
- Bad：`web` job 写 `if: ${{ hashFiles('web/package.json') != '' }}`，push 后 run 在 job 创建前失败。
- Bad：因为本地机器没有 Docker daemon 就把 Docker image build 从上线门禁里删除；正确做法是保留 GitHub Actions Buildx job。

### 6. Tests Required

- 改 workflow 后跑 `git diff --check`。
- 用 `rg -n "hashFiles" .github/workflows/ci.yml` 确认没有 job-level `hashFiles`。
- 本地跑 `make verify-go`；如果 workflow 或 Makefile touch 到 web 质量门，同时跑 `make verify-web`。
- 修复必须推送后观察一次 GitHub Actions run，确认 check suite 创建了实际 `go` / `web` jobs。
- 改 Dockerfile / entrypoint / Compose / image workflow 时，确认 CI `docker-image` job 仍存在，并在 PR 上观察该 job 成功；发布前再观察 `publish-images` 的 build/publish/inspect steps。

### 7. Wrong vs Correct

#### Wrong

```yaml
web:
  if: ${{ hashFiles('web/package.json') != '' }}
  runs-on: ubuntu-latest
```

#### Correct

```yaml
web:
  runs-on: ubuntu-latest
  steps:
    - run: make verify-web
```

---

## 测试约定

### 测试文件位置 / 命名

- 单元测试与被测文件**同目录同包**：`store/sync_batches.go` ↔ `store/sync_batches_test.go`。
- 跨包黑盒测试用 `<package>_test` 包名：`internal/center/http/handlers/monitoring_instances_test.go` 第 1 行 `package handlers_test`。
- 端到端测试加 `_e2e_test.go` 后缀：唯一例子 `internal/center/http/auth_e2e_test.go`，配套 helper `auth_e2e_helpers_test.go`。
- 唯一窄例外见下方 APP ACL R2 Slice 7 场景：批准的
  `internal/center/store/migrate/app_acl_r2_postgres_integration_test.go` 保持
  该既有所有权名称；它不为任何其他真实数据库测试建立命名先例。
- 路由级集成测试单独文件：`internal/center/http/router_api_test.go`、`router_test.go`。

### Table-driven 测试

约定为**主流**写法（凡是有 ≥3 个相似 case 的场景）。模板（来自 `internal/center/http/handlers/monitoring_instances_test.go:302-339`）：

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
- 循环变量 `tt`（主流，`monitoring_instances_test.go`、`targets_test.go`、`monitoring_instance_onboarding_test.go`）或 `tc`（见 `targets_test.go:514`）。新代码沿用 `tt`。
- 用 `t.Run(tt.name, func(t *testing.T) { ... })` 子测试，便于 `-run TestX/case_name` 精准触发。

### `t.Parallel()`

**纯函数 / 无共享状态**的测试可以加 `t.Parallel()`：参见 `internal/center/settings/types_test.go:13`、`internal/center/store/monitoring_instances_test.go:20`、`internal/center/enrollment/service_test.go:13`。

涉及 `httptest.NewRecorder` + 内存 fake repository 的 handler 测试**当前没有用 `t.Parallel()`**——保持现状即可，不要为了快几毫秒强加 parallel。

### Handler 测试

模式 = `httptest.NewRequest` + `httptest.NewRecorder` + `handler.ServeHTTP(recorder, req)` + 直接断言 `recorder.Code` / `recorder.Body`。**不启动真实 HTTP server**。仓库依赖通过手写 `fake<X>Repository` struct 实现领域接口，例如 `internal/center/http/handlers/monitoring_instances_test.go:17-57` 的 `fakeMonitoringInstanceRepository`。

**路由级**测试用 `centerhttp.New(centerhttp.RouterOptions{...})` 真实拼装 mux，但所有 handler 字段塞 `http.HandlerFunc(...)` 假实现，仅校验 SPA fallback / 路由优先级（参见 `router_api_test.go` 全文）。给既有 subtree 增加 handler 时，必须同时检查 `/api/<resource>/` 外层注册条件、`<resource>SubtreePath` classifier 和 `switch subtree` dispatch 三处；只让 classifier 返回新 enum 但不在 switch 里转发 handler，会让已实现 endpoint 继续 404。

### Store 测试

**当前所有 `internal/center/store/*_test.go` 均使用 fake `pgx.Tx` / `pgx.Row` 实现接口**，**不依赖真实 Postgres**。`grep -l "OpenPostgres\|pgxpool.New" internal/center/store/*_test.go` 结果为空。

参考模板 `internal/center/store/sync_batches_test.go:175-260` 的 `fakeSyncBatchTx`：

- 实现 `Exec` / `QueryRow` / `Query` / `Commit` / `Rollback`，记录调用。
- 通过 `execErrForSQLSubstring` 等字段按 SQL 文本子串决定哪一步注入错误，验证事务 rollback 行为。
- 仓库构造时直接替换 `beginTx` 字段（`PostgresSyncRepository.beginTx`，见 `store/sync_batches.go:30-36`），跳过真 pgxpool。

**真实 Postgres 烟囱测试不在 verify 链路里**，由 `docs/operations/fresh-install-smoke-run.md` 的人工 fresh-install 流程补齐。新加 store 方法时如果只能靠真 DB 验证（例如 trigger / 外键级联），应在 `docs/operations/` 下补描述，不要把 verify 弄成"必须有本地 Postgres"。

时间窗口类 store 测试要避免夹具随真实日期漂移失效：如果生产逻辑用 `time.Now()` / 当前日期判断续费窗口、过期、TTL、retention 等，测试中的"未来日期"必须相对 `time.Now().UTC()` 生成，或者把时间源注入被测代码。不要把 `2026-06-11` 这类固定日期当作"未来 7 天"写进会长期运行的 CI 夹具；到了真实日期之后，它会变成过去日期并让测试从行为验证退化成日历炸弹。固定 `created_at` / `updated_at` 这类展示或排序时间可以继续用稳定常量。

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

### Scenario: agent sync queue disk bounds

1. **Scope / Trigger**
   - Trigger: 修改 `agent/syncqueue/`、`agent/config/` durable buffer env、`agent/runtime` queue construction、installer `agent.env`，或离线队列保留策略。
   - 目标：agent 离线时保留可重试 sync request，但必须同时受 entry 数、年龄、磁盘字节上限约束，避免长期离线写满磁盘。

2. **Signatures**
   - Config env:
     - `HOUFENG_AGENT_BUFFER_FILE`
     - `HOUFENG_AGENT_BUFFER_MAX_ENTRIES`
     - `HOUFENG_AGENT_BUFFER_MAX_AGE`
     - `HOUFENG_AGENT_BUFFER_MAX_BYTES`
   - Go config: `agent/config.AgentConfig{BufferFile, BufferMaxEntries, BufferMaxAge, BufferMaxBytes}`。
   - Queue options: `syncqueue.Options{MaxEntries, MaxAge, MaxBytes, SkipFsync}`。

3. **Contracts**
   - Defaults: `MaxEntries=65536`、`MaxAge=72h`、`MaxBytes=64MiB`。
   - `MaxBytes <= 0` uses the default; env override must be a positive integer.
   - Queue pruning order is oldest first after sorting by `CreatedAt` / ID, then max entries, then max bytes.
   - If even the newest entry cannot fit the configured byte cap, the queue may be empty after pruning; do not write a file larger than the cap.
   - Production runtime must pass `BufferMaxBytes` into `syncqueue.NewFileStore`; tests may set small caps and `SkipFsync: true`.

4. **Validation & Error Matrix**
   | Condition | Expected behavior |
   | --- | --- |
   | `HOUFENG_AGENT_BUFFER_MAX_BYTES` missing | default 64MiB |
   | non-integer / `<=0` max bytes | config load error |
   | two entries exceed max bytes but newest fits | oldest dropped, newest remains, file size <= cap |
   | newest entry exceeds max bytes | all entries dropped, file size <= cap |

5. **Good / Base / Bad Cases**
   - Good: center is offline for days; queue keeps recent facts within 64MiB and drops oldest entries predictably.
   - Base: operator lowers max bytes for a tiny VPS; queue may keep fewer entries but never grows past cap.
   - Bad: only limiting entry count lets a few very large command/IP quality payloads fill disk.

6. **Tests Required**
   - `agent/config/config_test.go`: defaults, override, invalid max bytes.
   - `agent/syncqueue/store_test.go`: byte pruning keeps newest fitting entries and file size stays below cap.
   - `agent/runtime` or construction coverage proving `BufferMaxBytes` reaches `syncqueue.Options`.
   - Installer embedded-script check that generated `agent.env` includes `HOUFENG_AGENT_BUFFER_MAX_BYTES`.

7. **Wrong vs Correct**

```go
// 错误：只限制 entries/age，忽略单条 payload 大小。
syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: cfg.BufferMaxEntries, MaxAge: cfg.BufferMaxAge})
```

```go
// 正确：同时传入字节上限。
syncqueue.NewFileStore(path, syncqueue.Options{
	MaxEntries: cfg.BufferMaxEntries,
	MaxAge:     cfg.BufferMaxAge,
	MaxBytes:   cfg.BufferMaxBytes,
})
```

---

### Scenario: center credential and database hardening config

1. **Scope / Trigger**
   - Trigger: 修改 `internal/center/config`、`internal/center/auth/password.go`、`internal/center/auth/service.go`、`cmd/houfeng-center/bootstrap.go`、部署 env docs，或中心启动安全配置。
   - 目标：密码 hash 成本、session ID HMAC secret 和生产 PostgreSQL TLS 要成为可测试的启动合同，避免安全调优只停留在文档建议。

2. **Signatures**
   - Config env:
     - `HOUFENG_PASSWORD_BCRYPT_COST`
     - `HOUFENG_SESSION_HMAC_KEY`
     - `HOUFENG_SESSION_HMAC_KEY_FILE`
     - `HOUFENG_DATABASE_REQUIRE_TLS`
     - `HOUFENG_DATABASE_URL`
   - Go config: `config.CenterConfig{PasswordBcryptCost int, SessionHMACKey []byte, DatabaseURL string}`。
   - Auth options: `auth.Options{PasswordBcryptCost int}`。
   - Seed options: `auth.SeedInitialUserOptions{PasswordBcryptCost int}`。
   - Store constructors:
     - `store.NewPostgresSessionRepository(pool *pgxpool.Pool, hmacKey []byte) (*PostgresSessionRepository, error)`
     - `store.NewPostgresMonitoringInstanceRepositoryWithTokenHMACKey(pool *pgxpool.Pool, hmacKey []byte) *PostgresMonitoringInstanceRepository`
     - `store.NewPostgresSyncRepositoryWithTokenHMACKey(pool *pgxpool.Pool, hmacKey []byte) *PostgresSyncRepository`

3. **Contracts**
   - `HOUFENG_PASSWORD_BCRYPT_COST` missing uses `auth.DefaultPasswordBcryptCost`（当前等于 Go bcrypt `DefaultCost`）。
   - Bcrypt cost must be within Go bcrypt `MinCost..MaxCost`; invalid, empty, non-integer, too-low, or too-high values fail config load.
   - `auth.HashPasswordWithCost` validates normal password policy first, then validates cost, then calls bcrypt with the configured cost.
   - `auth.Service.ChangePassword` and first-user seeding must use `cfg.PasswordBcryptCost`; package-level `HashPassword` remains the compatibility/default helper.
   - `HOUFENG_SESSION_HMAC_KEY` is required, must be at least 32 bytes, and is copied into `config.CenterConfig.SessionHMACKey`.
   - `HOUFENG_SESSION_HMAC_KEY_FILE` takes precedence over `HOUFENG_SESSION_HMAC_KEY` for secret-mount deployments.
   - `cmd/houfeng-center/bootstrap.go` must pass `cfg.SessionHMACKey` into `store.NewPostgresSessionRepository`; the session repository must not have a static production default HMAC key.
   - `cmd/houfeng-center/bootstrap.go` must pass the same `cfg.SessionHMACKey` into agent enrollment/sync token repositories through `NewPostgresMonitoringInstanceRepositoryWithTokenHMACKey` and `NewPostgresSyncRepositoryWithTokenHMACKey`; production agent token hashing must not use repository default test key material.
   - New agent enrollment and sync token hashes must use versioned purpose-separated HMAC-SHA256 values. Legacy plain SHA-256 token hashes may only remain as a verification-and-migration compatibility path.
   - Rotating the session HMAC secret invalidates existing browser sessions because database lookup hashes no longer match existing rows. It also invalidates agent enrollment/sync token hashes that have migrated to the HMAC format; rollback/rotation requires planned re-enrollment or token reissue.
   - `HOUFENG_DATABASE_REQUIRE_TLS=true` means `HOUFENG_DATABASE_URL` must include `sslmode=require`、`sslmode=verify-ca`、or `sslmode=verify-full`.
   - Missing `sslmode` or weak modes (`disable`、`allow`、`prefer`) fail startup only when the require-TLS flag is true, so local Compose / localhost development can keep `sslmode=disable`.

4. **Validation & Error Matrix**
   | Condition | Expected behavior |
   | --- | --- |
   | missing bcrypt cost | use default |
   | bcrypt cost below min / above max / non-integer | `LoadCenterConfig` error |
   | change password with configured cost | stored bcrypt hash reports that cost via `bcrypt.Cost` |
   | seed initial user with configured cost | stored bcrypt hash reports that cost via `bcrypt.Cost` |
   | missing session HMAC key | `LoadCenterConfig` error |
   | session HMAC key shorter than 32 bytes | `LoadCenterConfig` error |
   | `HOUFENG_SESSION_HMAC_KEY_FILE` set | file content wins over env key |
   | bootstrap session repository wiring | receives exactly `cfg.SessionHMACKey` |
   | bootstrap agent token repository wiring | monitoring instance and sync repositories receive exactly `cfg.SessionHMACKey` |
   | legacy SHA-256 agent token hash validates | request succeeds and rewrites stored hash to versioned HMAC |
   | `HOUFENG_DATABASE_REQUIRE_TLS=true` and `sslmode=disable` / missing | `LoadCenterConfig` error |
   | `HOUFENG_DATABASE_REQUIRE_TLS=true` and `sslmode=verify-full` | config load succeeds |
   | invalid boolean require-TLS value | `LoadCenterConfig` error |

5. **Good / Base / Bad Cases**
   - Good: production external PostgreSQL uses `HOUFENG_DATABASE_REQUIRE_TLS=true` and `sslmode=verify-full`.
   - Good: production sets a stable random `HOUFENG_SESSION_HMAC_KEY_FILE` through a secret mount; rolling restart keeps browser sessions and migrated agent token hashes valid.
   - Base: local Docker Compose uses co-located `db` service with generated `sslmode=disable` and leaves `HOUFENG_DATABASE_REQUIRE_TLS` unset/false.
   - Bad: raising bcrypt cost globally by changing a package constant without config tests or benchmark guidance.
   - Bad: `PostgresSessionRepository` silently falls back to `[]byte("houfeng-session-hmac-v1")`, so every deployment shares the same public HMAC key.
   - Bad: production center uses `store.NewPostgresMonitoringInstanceRepository(pool)` or `store.NewPostgresSyncRepository(pool)`, causing agent token hashes to use test/default HMAC key material.
   - Bad: documenting production database TLS while code still accepts `sslmode=disable` with no startup guard.

6. **Tests Required**
   - `internal/center/config/config_test.go`: default/override/invalid bcrypt cost and require-TLS accepted/rejected `sslmode` cases.
   - `internal/center/config/config_test.go`: required session HMAC key, `_FILE` precedence, and short-key rejection.
   - `internal/center/auth/password_test.go`: `HashPasswordWithCost` embeds requested cost and rejects invalid cost.
   - `internal/center/auth/service_test.go`: password change stores a hash with configured cost.
   - `internal/center/auth/seed_test.go`: first-user seed stores a hash with configured cost.
   - `internal/center/store/sessions_test.go`: repository rejects missing HMAC key and stores/queries session IDs only by HMAC hash.
   - `internal/center/store/agent_token_hash_test.go` plus monitoring/sync repository tests: new agent token hashes are versioned HMAC values, legacy SHA-256 hashes still verify, and successful use migrates legacy rows.
   - `cmd/houfeng-center/bootstrap_test.go`: default seed dependency and bootstrap auth service wiring pass `cfg.PasswordBcryptCost`; session repository wiring passes `cfg.SessionHMACKey`; source/wiring checks cover agent token repositories receiving `cfg.SessionHMACKey`.

7. **Wrong vs Correct**

```go
// 错误：配置有 cost，但写 hash 时仍走 package-level default。
hash, err := auth.HashPassword(newPassword)
```

```go
// 正确：服务使用启动配置里的 cost。
hash, err := auth.HashPasswordWithCost(newPassword, s.passwordBcryptCost)
```

```go
// 错误：session repository 内部使用所有部署共享的静态 key。
sessionRepo := store.NewPostgresSessionRepository(pool)
```

```go
// 正确：启动配置显式传入部署 secret。
sessionRepo, err := store.NewPostgresSessionRepository(pool, cfg.SessionHMACKey)
```

```go
// 错误：生产 agent token hash 使用 repository 默认测试 key。
syncRepo := store.NewPostgresSyncRepository(pool)
```

```go
// 正确：生产 agent token hash 从启动 secret 派生用途隔离 HMAC key。
syncRepo := store.NewPostgresSyncRepositoryWithTokenHMACKey(pool, cfg.SessionHMACKey)
```

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

`actions/setup-go@v6` 通过 `go-version-file: go.mod`（`.github/workflows/ci.yml:16`）锁定。本地 Go 必须 ≥ `go.mod` 声明的版本。

---

## 提交前清单

下面这条清单是 happy-path，按顺序勾：

1. [ ] **`make verify-go`** —— 永远必须通过。fmt 修了直接重跑。
2. [ ] **改了 `web/` 里任何文件 → `cd web && npm run lint && npm run test`**（CLAUDE.md 第 31 行）。
3. [ ] **同时改了前后端 → `./scripts/verify.sh`** 一把跑完。
4. [ ] **改了迁移 / 表结构** → 跑一次 `docs/operations/fresh-install-smoke-run.md` 的 fresh-install；若发现可复用的 gap 或规则，补到 `.trellis/spec/` 或当前 active docs。
5. [ ] **改了 user-visible 的 UI** → 对照 `docs/design/current/{interface-language.md,component-patterns.md}`，并按 `docs/operations/ui-preview-and-browser-sanity.md` 给出 preview URL、已检查 routes / viewports、browser sanity、local screenshot notes（如有，默认不提交）。如果任务改变了可复用 UI 方向，同步更新 `docs/design/current/` 或相关 `.trellis/spec/`；历史版本目录只保留背景材料。
6. [ ] 如果 worker / 调度类改动，本地用注入的小间隔跑 `go test -count=10` 看下抖动。

---

## 跨层一致性（PR review checklist）

下面是 reviewer 必看的"改一处必带的另一处"。代码已经按这个习惯写，不要破坏：

| 改动 | 必须连带的修改 |
|------|----------------|
| 新增 HTTP endpoint | 1) `internal/center/http/handlers/<resource>.go` handler 工厂；2) `internal/center/http/router.go` 的 `RouterOptions` 字段 + mux 注册；3) `cmd/houfeng-center/bootstrap.go` `bootstrapCenter` 显式构造并塞进 `RouterOptions`；4) `cmd/houfeng-center/bootstrap_test.go` 的 `TestBootstrapCenterBuildsAppOnSuccess` 增 nil 断言；5) `internal/center/http/handlers/<resource>_test.go` 增 table-driven handler 测试；6) `internal/center/http/router_api_test.go` 增 SPA fallback 隔离测试 |
| 修复或接入既有 subtree endpoint | 1) 确认 `/api/<resource>/` 外层 mux 注册条件包含对应 `RouterOptions` handler；2) `<resource>SubtreePath` 能识别目标 path；3) `switch subtree` 有对应 dispatch case；4) `router_api_test.go` 用 fake handler 断言不会落到 SPA fallback / 404；5) handler 测试覆盖 method、invalid body、not found、domain state conflict、repo failure |
| 新增/修改 agent ↔ center 字段 | 1) `internal/contracts/agentapi/types.go` 改 DTO；2) center 端在 `internal/center/syncing/` 或对应 handler 处理；3) **agent 端在 `agent/runtime/` 或采集子包同 PR 内改完**；4) 两侧测试同 PR 通过 |
| 新增领域 sentinel error | 1) `internal/center/<domain>/types.go` 加 `Err...`；2) handler 加 `errors.Is` case + `agentapi.ErrorCode*` 映射（如属 agent endpoint） |
| 新增持久化字段 / 表 | 走 `database-guidelines.md` 的 4 步流程；reviewer 在 PR 内确认迁移序号未撞车 |
| 新增 VPS ↔ MonitoringInstance 关联能力 | 1) store 测试证明 link/unlink 只写 `vps_monitoring_instance_links` 且保留历史；2) handler 测试覆盖 duplicate conflict、missing VPS/MonitoringInstance、invalid `monitoring_instance_id`、query summaries；3) router 测试覆盖 `/api/vps/{id}/monitoring-instances`、`link-monitoring-instance`、`unlink-monitoring-instance`、`/api/monitoring-instances/{id}/vps` 不落到 item handler / SPA；4) bootstrap_test 增 nil 断言；5) 验证不改 Agent / Target / MonitoringInstance 状态写路径 |
| 新增 VPS timeline / Asset Ledger 历史 | 1) migration 测试证明 `renewal_decisions`、`price_histories`、`ip_histories`、`vps_spec_snapshots` 约束、索引、枚举；2) store 测试证明 VPS / subscription PATCH 在事务中 `select ... for update`、更新当前状态并只按真实变化插入历史；3) handler 测试覆盖 `/api/vps/{id}/timeline` success、missing VPS、invalid input、method，并断言所有历史数组；4) router 测试证明 timeline 不落到 item handler / SPA；5) bootstrap_test 增 nil 断言；6) 验证不改 MonitoringInstance / Target / Agent 状态写路径 |
| 新增运维型 CLI / import 命令 | 1) `cmd/<binary>/main.go` 只测 flag / 模式互斥 / 基础错误；2) 业务逻辑包增加纯 Go table-driven tests；3) 至少跑一次 `go run ./cmd/<binary> ... -dry-run` 样例命令；4) 涉及写库时确认事务边界与 dry-run 不写库 |
| 引入新 worker | 1) `internal/center/<x>/worker.go` 实现 `Worker.Run(ctx) error`；2) `cmd/houfeng-center/bootstrap.go` 添加构造与传给 `centerapp.New(...)`；3) `bootstrap_test.go` 的 `TestBootstrapCenterBuildsAppOnSuccess` 把 `len(workers)` 期望值从 N 改为 N+1（**当前为 3**：incident、retention、session cleanup） |
| agent 端新采集 / 新探针 | 1) `agent/hostsample/` 或 `agent/probe/` 实现采集；2) 通过 `agent/runtime/runtime.go` 的 `buildSyncRequest` 串接；3) 必要时改 `internal/contracts/agentapi/` DTO（不可单边） |

### Scenario: Center 与 Web 严格 CSP 同源合同

#### 1. Scope / Trigger

- Trigger: 修改 `internal/center/http/SecurityHeaders`、CSP policy、`web/index.html`、Vite dev/preview headers、字体/图标等静态资源、主题 bootstrap，或生产 TSX/CSS 的资源与样式表达时，必须遵守本合同。
- 目标：Center、前端开发预览和 production build 共享一份精确的严格同源策略；不靠 `unsafe-inline`、nonce、data URI 或远程 origin 掩盖资源迁移缺口。

#### 2. Signatures

- Policy source: `internal/center/http/csp-policy.txt`，单行精确文本。
- Go embed: `//go:embed csp-policy.txt` → `contentSecurityPolicySource` → `strings.TrimSpace(...)` → `SecurityHeaders(enableHSTS bool)` 的 `Content-Security-Policy` response header。
- Vite boundary: `web/vite.config.ts` 从仓库同一 policy 文件读取，并赋给 `server.headers` 与 `preview.headers`。
- Docker boundary: root `Dockerfile` 的 `web-build` stage 在 `npm run build` 前把同一文件复制到 `/src/internal/center/http/csp-policy.txt`，保持 Vite 的仓库相对路径成立。
- Browser resources: `/theme-bootstrap.js`、`/fonts/*.woff2`、`/select-caret-*.svg` 均来自 `web/public/` 的同源 URL。

#### 3. Contracts

- 唯一批准策略是：`default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'`。
- Go runtime 与 Vite 不得各维护策略副本；运行时唯一来源是 `csp-policy.txt`。测试中的 expected literal 只用于发现 policy 漂移，不能成为第二个运行时来源。
- Docker web stage 不能只复制 `web/`：它必须复制原始 `csp-policy.txt` 到 Vite 解析的 `/src/internal/center/http/` 路径；禁止在 `web/` 下生成或维护第二份 policy。
- HTML 不得含 inline script 或远程 font；主题 bootstrap 必须在 React 入口前同步加载同源文件，并只接受 `houfeng|classic` 与 `dark|light|system` allowlist。
- CSS 不得引用 remote font 或 `data:` image。IBM Plex Sans 400/500/600/700、Mono 400/500/600、OFL 和三套主题 caret 必须作为受跟踪的 `web/public/` 资源存在。
- 所有生产 `.tsx` 禁止 JSX `style=`。静态视觉使用 BEM/令牌，SVG 动态几何使用 attributes，比例与列宽优先使用 `<progress>` 与 `<col width>`。Modal scroll lock / clipboard fallback 的窄范围 CSSOM 写入必须保留行为测试与真实 Chromium CSP 证据，不得扩展成业务样式通道。
- CSP 合格需要三层证据同时成立：source contract、Go exact-header/Vite shared-header tests、真实 production build 浏览器 violation gate；只通过其中一层不能宣称兼容。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| `csp-policy.txt` 与批准文本不一致 | Go exact-header、Vite header 或 Web source contract 至少一项失败 |
| Docker web stage 未复制 policy，或复制发生在 `npm run build` 之后 | source contract 失败；镜像构建在加载 Vite config 时以 `ENOENT /src/internal/center/http/csp-policy.txt` 失败 |
| HTML 出现 inline script / Google Fonts | `cspContract.test.ts` 失败，production browser 不得放宽策略通过 |
| CSS 出现 `data:` image / remote font | source contract 失败；禁止加入 `data:` 或远程 origin 到 policy |
| production TSX 出现 `style=` | source contract 报出文件与行号；改用 class/attribute/原生元素 |
| bootstrap persisted preset/mode 非 allowlist | 回退到 `theme-houfeng-dark`，不得生成任意 class |
| 字体、caret、bootstrap 或 OFL 缺失 | public-resource contract 失败；浏览器 resource/network gate 不得标绿 |
| 任一核心路由触发 `securitypolicyviolation`、console/runtime error 或非预期 4xx/5xx | browser gate 失败并保留 route + viewport + directive/URL 证据 |
| 登录页 `/api/auth/me` 返回预期 401 | 只作为未认证基线，不计入非预期网络错误；Document 仍必须带精确 CSP |

#### 5. Good/Base/Bad Cases

- Good: Center production、Vite dev/preview 都返回同一策略；七个字体文件、主题脚本和 caret 同源加载，核心路由在三档视口均零 violation。
- Base: 新增动态 SVG 图表，用 presentation attributes + class 表达几何和视觉，并在 source/browser gate 中通过。
- Bad: 为保留 `<script>...</script>` 或 React `style={{...}}` 把 `unsafe-inline` 加回 `script-src` / `style-src`。
- Bad: Go 与 Vite 各复制一份 policy；一次安全收紧只改其中一处，导致本地预览与 production 行为分叉。

#### 6. Tests Required

- `internal/center/http/middleware_test.go`: `TestSecurityHeadersSetsBaselineHeaders` 必须断言完整、精确 CSP header。
- `web/vite.config.test.ts`: 断言 dev 与 preview headers 等于批准 policy。
- `web/src/security/cspContract.test.ts`: 断言唯一 policy 文件、无 remote/inline/data/JSX style、所有同源资源与 license 存在、font/caret wiring 完整、theme allowlist 与 `classic-light` 回退一致。
- `web/src/security/cspContract.test.ts`: 同时断言 Docker `web-build` stage 在 `npm run build` 前把原始 policy 复制到 `/src/internal/center/http/csp-policy.txt`。
- 改到的 chart/table/progress/component 必须有 focused unit test，断言不再生成 `style` prop 且运行时值仍进入对应 attribute/value。
- production build 后用真实 Chromium 覆盖 login 与核心路由、`1440x1000` / `1024x768` / `390x900`，捕获 `securitypolicyviolation`、console/runtime、network、Document header、字体、caret、主题切换与动态图表交互；持久化 CI browser gate 由前端质量 ratchet 任务维护。

#### 7. Wrong vs Correct

```go
// 错误：在 Go 内复制策略并为现有 inline 资源放宽。
header.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")
```

```go
// 正确：运行时只消费嵌入的同一 policy 文件。
//go:embed csp-policy.txt
var contentSecurityPolicySource string

header.Set("Content-Security-Policy", strings.TrimSpace(contentSecurityPolicySource))
```

```tsx
// 错误：React 会生成被严格 style-src 拒绝的内联样式。
<div style={{ width: `${ratio}%` }} />

// 正确：用原生语义元素携带动态比例，视觉由 CSS class 负责。
<progress className="score-bar" value={ratio} max={100} />
```

```dockerfile
# 错误：web stage 看不到 Vite 读取的仓库级 policy。
COPY web/ ./
RUN npm run build

# 正确：复制同一个源文件到 Vite 预期路径，再构建 web。
COPY internal/center/http/csp-policy.txt /src/internal/center/http/csp-policy.txt
COPY web/ ./
RUN npm run build
```

---

### Scenario: incident threshold settings order

#### 1. Scope / Trigger

- Trigger: 修改 `centersettings.IncidentDefaults`、`IncidentDefaultsOverride`、`internal/center/incidents.MetricThresholds`、`/api/settings` 的 incident defaults 请求/响应，或前端监控阈值展示/设置提交。
- 目标：异常等级阈值必须严格递进，避免用户保存倒序配置后 evaluator、图表阈值线和中文等级文案互相矛盾。

#### 2. Signatures

- Backend settings type: `internal/center/settings.IncidentDefaults`。
- Backend override type: `internal/center/settings.IncidentDefaultsOverride`。
- Frontend settings type: `web/src/lib/types.ts` `IncidentDefaults` / `IncidentDefaultsOverride`。
- Frontend runtime resolver: `web/src/config/thresholds.ts` `resolveThresholds(...)`。

#### 3. Contracts

- CPU / memory / disk / inode 三段阈值必须满足 `warning < alert < critical`。
- IOWait / Load5 两段设置必须满足 `warning < critical`；alert 只能由中点派生。
- `settings.Validate` 是持久化与 API 输入的权威校验入口，必须拒绝倒序或相等阈值并返回可 `errors.Is(err, ErrInvalidSettings)` 判定的错误。
- override 只给出部分 incident threshold 字段时，必须先与当前已规范化的全局 `IncidentDefaults` 合成，再校验有效阈值顺序；不能与代码默认值合成。
- 前端 Settings 页提交前必须做同样校验，错误文案使用中文等级名，例如 `CPU 阈值必须满足 关注 < 告警 < 严重。`。
- 前端展示用 `resolveThresholds` 收到旧脏数据时，按 metric 回退到 `DEFAULT_THRESHOLDS`，不要渲染倒序阈值线。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| CPU `warning >= alert` 或 `alert >= critical` | `settings.Validate` 返回 `ErrInvalidSettings` |
| Load5 / IOWait `warning >= critical` | `settings.Validate` 返回 `ErrInvalidSettings` |
| override 局部字段与当前全局 defaults 合成后倒序 | `settings.Validate` 返回 `ErrInvalidSettings` |
| Settings 页用户输入倒序阈值 | 页面显示中文校验错误且不发 `PUT /api/settings` |
| `resolveThresholds` 收到倒序 runtime settings | 该 metric 使用默认阈值 |

#### 5. Good/Base/Bad Cases

- Good: `CPU 80/90/95`、`IOWait 20/50`、`Load5 4/8` 保存成功，evaluator 和图表均按递进等级工作。
- Base: 全局 CPU 改成 `50/60/70`，某个 override 只把 critical 改成 `75`，按 `50/60/75` 校验后允许。
- Bad: `CPU 95/80/90` 被保存，导致关注/告警/严重语义在 evaluator 中反转。
- Bad: override 与代码默认值合成而不是当前全局配置合成，误放行或误拒绝局部阈值。

#### 6. Tests Required

- `internal/center/settings/types_test.go`: 覆盖默认阈值三段/两段倒序与相等拒绝、override 局部字段按当前全局 defaults 合成校验。
- `web/src/pages/SettingsPage.test.tsx`: 覆盖倒序设置显示中文错误，且 `fetch` 只发生初始 GET、不发生 PUT。
- `web/src/config/thresholds.test.ts`: 覆盖 runtime settings 倒序时按 metric 回退默认阈值。

#### 7. Wrong vs Correct

```go
// 错误：只校验范围，不校验等级顺序。
if warning < 1 || warning > 100 { return err }
```

```go
// 正确：范围校验后必须校验有效等级顺序。
if !(warning < alert && alert < critical) {
	return invalidSettings("cpu thresholds must satisfy warning < alert < critical")
}
```

---

### Scenario: APP ACL R2 Slice 7 严格 PostgreSQL 16 catalog lane

#### 1. Scope / Trigger

- Trigger：修改
  `internal/center/store/migrate/app_acl_r2_postgres_integration_test.go`、
  `scripts/test-record-platform-integration.sh` 的 `pg16-catalog` mode，或
  `.github/workflows/ci.yml` 的 `record-platform-pg16-catalog` job 时。
- 本场景是批准文件 `app_acl_r2_postgres_integration_test.go` 的唯一命名
  例外。它是 required-CI catalog lane，不替代普通 `_e2e_test.go` 测试，也不
  替代父任务的 record-platform fixture modes。
- 它只覆盖 APP ACL R2 的 PG16 authority/catalog evidence，不改变冻结的
  R1/R2 source、tuple、ACL、data、permission、state 或 clone/restore 合同。
- 这是 cross-layer runner、Actions job、GitHub app-bound required-check 和
  `go test -json` evidence contract；实现时四个边界必须一起验证，不能把
  package compilation 或 zero-test result 标成 PG16 evidence。

#### 2. Signatures

```bash
HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=<exact-image> \
  scripts/test-record-platform-integration.sh pg16-catalog -- <command> [args...]
```

- `pg16-catalog` 是唯一严格的 Slice 7 mode。`<exact-image>` 只能是
  `postgres:16.0`、`postgres:16.6` 或 `postgres:16.12`。
- `postgres` 与 `postgres-s3` 保持原有父任务合同：
  `scripts/test-record-platform-integration.sh <mode> -- <command> [args...]`。
  Slice 7 image variable 不对两者施加 validation 或 defaulting。
- required workflow job id 是 `record-platform-pg16-catalog`；显式 job name
  是 `record-platform-pg16-catalog (${{ matrix.postgres_image }})`。literal
  matrix 必须产生以下精确 check contexts：
  `record-platform-pg16-catalog (postgres:16.0)`,
  `record-platform-pg16-catalog (postgres:16.6)`，和
  `record-platform-pg16-catalog (postgres:16.12)`.
- job 的 fresh-runner signature 是 `runs-on: ubuntu-latest`、
  `actions/checkout@v6`、`actions/setup-go@v6` + `go-version-file: go.mod`，
  随后每一个 matrix lane 执行同一个 strict command：

  ```bash
  scripts/test-record-platform-integration.sh pg16-catalog -- \
    go test -json ./internal/center/store/migrate \
    -run '^TestPostgresIntegrationAppACLR2$' -count=1
  ```

  `TestPostgresIntegrationAppACLR2` 是批准文件中的唯一 top-level PG16
  anchor；subtest 名可附在该 anchor 后。`platformmigrate` 与 record-platform
  admin CLI 不属于这个 command。
- controller 的 protection read/write signatures 是 `GET` 加 ETag/`If-None-Match`
  和 scoped `PATCH repos/$OWNER/$REPO/branches/main/protection/required_status_checks`。
  internal merge 是 `{"strict":true,"checks":[{"context":"<name>","app_id":<number|null>},...]}`；
  PATCH request 对 null pair 只发送 `{"context":"<name>"}`，numeric binding
  发送 `{context,app_id}`，绝不发送 deprecated top-level `contexts`。

#### 3. Contracts

- runner 必须在 `mktemp`、random material、port probing、Docker、container、
  fixture URL 或 child execution 之前验证 mode、`--`、child argv 和严格 image
  值。每个被接受的 strict lane 都把所选 image 用于所有
  APP/ledger/witness/recovery fixture databases。
- `postgres`、`postgres-s3`、两者名称和未来父任务调用都是 compatibility
  boundary。加入 `pg16-catalog` 时不得 rename、delete、route through strict
  allowlist 或以其他方式改变它们。
- 批准文件只有直接执行且未设置
  `HOUFENG_POSTGRES_INTEGRATION=1` 时才可走普通 `t.Skip`。strict runner
  export 该变量；child output 中任意 `--- SKIP:` 都必须使 runner exit 1，
  enabled-test prerequisite failure 必须使用 `t.Fatal`/error 而非 skip。没有
  其他 real-PostgreSQL test 获得此例外。
- workflow matrix 只能包含三个 quoted literal，不得有 `include`、default
  expression 或不同 entry point；每个 lane 运行同一 `pg16-catalog` command。
  job 必须先 checkout 和按仓库既有 `setup-go@v6`/`go.mod` 模式安装 Go；显式
  job name 是三个 check contexts 的 evidence contract；workflow 文件本身不能
  把 context 设为 required。
- strict child 只运行 migrate package 的 anchored JSON selector。它必须将
  stdout 保存为 JSONL，并以 `jq -se` 证明匹配
  `^TestPostgresIntegrationAppACLR2($|/)` 的 `run` 和 `pass` event 各至少一条，
  且该 package 的 `skip` 和 `fail` event 均为零。runner 的 child exit 和
  `--- SKIP:` failure 保持有效；event proof 额外拒绝 zero-test false green。
  `internal/center/platformmigrate` 与
  `cmd/houfeng-record-platform-admin` 的回归只能走独立非 PG16
  `go`/`make verify-go` full-test gate。
- branch protection 是 controller-owned external governance。controller 必须先
  取得 auditable external exclusive required-checks mutation lease，覆盖 GitHub
  UI、owner/admin token、GitHub App 与其他 automation writer，并从 first GET
  持有到 post-PATCH readback 和释放。GitHub ETag 与 `If-None-Match` 只支持
  conditional GET；该 endpoint 没有 supported conditional unsafe write，故
  `If-Match` 不能作为 PATCH CAS。没有该 lease 必须返回 `NEEDS_CONTROLLER`，
  不能 PATCH。
- 在租约内，controller first GET `.../required_status_checks`，要求
  `strict == true` 和合法 `checks[]`，保存 ETag 与 canonical `{strict,checks}`，
  并读取同一 `$HEAD` 的所有 check-run pages。它以该 ETag conditional GET：
  只有 304 才可 merge；200、changed ETag 或 changed canonical checks 都必须
  fresh GET、re-read all pages、re-merge，绝不从旧快照 PATCH。最多三次仍未
  stable 即 `NEEDS_CONTROLLER`。
- fresh `checks[]` 的每个 `{context, app_id}` 都保留；numeric app_id 必须保持
  numeric，合法 `app_id:null`（当前 `web-browser`）在 internal merge 保留 null，
  PATCH 中只省略该 `checks[]` object 的 app_id。三个 named contexts 只能各取
  同一 `$HEAD` 上唯一 `completed` / `success` 且 numeric `.app.id` 的 run。
  existing/new pairs 按 `[context, app_id]` 去重，PATCH `{strict:true,checks}`
  到该子端点；不得 hard-code 当前四个 context、提交 top-level `contexts`、或
  PATCH whole protection object。post-readback 必须是 merge 的 exact superset，
  保持 strict、old numeric/null pairs、future pairs 和三条 new numeric pairs；
  PATCH transport ambiguity 或任何 validation/readback failure 都
  `NEEDS_CONTROLLER`，不得 replay。该 endpoint 不会修改 `enforce_admins` 或
  required conversation resolution。
- 记录本合同时，`main` 的 `strict=true`、`enforce_admins=true` 与 required
  conversation resolution 已启用，观察到的 contexts 为 `go`、`web`、
  `web-browser`、`docker-image`；这些是 fresh-GET audit facts，不是重建
  required-check payload 的 name list。future existing checks 只能来自刚读取的
  `checks[]`。

#### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| `pg16-catalog` 缺少 image，或 image 为 `postgres`、`postgres:16`、`postgres:16-alpine`、其他非 allowlist 值 | 在任何 Docker 或 fixture side effect 前 exit 2。 |
| `pg16-catalog` 使用一个 allowlist image | 用该 exact image 启动四个 fixture database 并执行 child command。 |
| 调用 `postgres` 或 `postgres-s3` | 保持各自 mode contract；不能仅因为 strict image variable 缺少或不同而拒绝。 |
| strict child 输出 `--- SKIP:` | cleanup 后 runner nonzero exit；该 lane 不是 evidence。 |
| fresh job 缺少 `runs-on`、checkout 或按 `go.mod` 的 setup-go，或 lane 运行不同 child command | workflow review 拒绝；不能声称 fresh Actions runner 可执行。 |
| matrix 添加 `include`、第四个值、shell/default fallback 或不同 entry point | workflow/runner contract review 必须拒绝；CI matrix 不再 deterministic。 |
| anchored JSON stream 没有 matching `run`/`pass`，或 package 有 `skip`/`fail` event | lane nonzero exit；zero-test、skip 或 fail 不是 PG16 evidence。 |
| strict PG16 command 包含 `platformmigrate` 或 admin CLI | review 拒绝；该 result 会混入非 Slice 7 file ownership 的 package gate。 |
| lease 缺失，`checks[]` 缺失/非法 app_id，`strict` 不为 true，target run 的 `head_sha` 不同、未完成、非 success、app id 非 numeric/歧义 | `NEEDS_CONTROLLER`；不得 PATCH。 |
| conditional GET 返回 200、ETag 改变或 canonical `{strict,checks}` 改变 | 回到 first GET，重新读取 pages 并重算；三次仍不 stable 为 `NEEDS_CONTROLLER`，不得 PATCH old snapshot。 |
| fresh GET 含 `{context,app_id:null}` | internal merge 保留 null，PATCH `checks[]` 以 `{context}` 编码；readback 若未保留 null pair 则 `NEEDS_CONTROLLER`。 |
| 新 head 上三个 exact numeric-app contexts 都唯一成功、lease held、conditional GET 为 304 | controller 可 PATCH merged/deduplicated `{strict:true,checks}` 到 required-status-checks，并以 strict exact-superset readback 后释放 lease。 |

#### 5. Good / Base / Bad Cases

- Good：用 `pg16-catalog` 提供 `postgres:16.6`；四个 fixture database 都使用
  该 literal image；fresh job checkout/setup Go 后，migrate anchor 产生
  nonzero `run`/`pass` 和 zero `skip`/`fail` JSON events，check context 为
  `record-platform-pg16-catalog (postgres:16.6)`。
- Base：开发者直接运行且没有 fixture environment 时，批准 integration test
  按普通规则 skip；它不是 required-CI result。
- Bad：改变 `postgres` 以拒绝 `postgres:16-alpine`，从而破坏父任务
  Task 10/14/15/16/17/18 commands。
- Bad：只在 YAML 中把 `record-platform-pg16-catalog` 标为 "required"，或在
  新 head 的三个 checks 变绿前添加 branch-protection context。
- Good：controller-held lease 内 first GET 后的 conditional GET 为 304；merge
  保留 `web-browser` 的 `{context,app_id:null}`，PATCH sends `{context}`, and
  readback is a strict exact superset before lease release。
- Base：conditional GET returns 200 while check-run pages are read; controller
  discards that snapshot and restarts the whole GET/pages/merge cycle。
- Bad：将当前 `go`、`web`、`web-browser`、`docker-image` strings 重写为
  固定 payload，或只从 context name 推断 app；这会丢失现有或未来的 app binding。
- Bad：把 ETag 的 `If-None-Match` GET 语义当成 `If-Match` PATCH CAS，或没有
  all-writer lease 就 PATCH；GitHub 没有提供该 endpoint 的安全条件写。

#### 6. Tests Required

- 批准 integration file 覆盖 real PG16 authority matrix；由 strict runner 执行
  时不得以 skip 作为 evidence。runner coverage 必须证明三个 allowlist literal、
  missing/invalid input 在 side effect 前拒绝、selected image 传到四个 fixtures、
  cleanup 与 skip-to-failure behavior。该文件必须声明唯一 top-level
  `TestPostgresIntegrationAppACLR2` anchor；CI/local selector 完全锚定该名字。
- 三个 image 都必须在本地运行同一 command；可用 Docker Server 是 local
  evidence，不是把 lane 推给 CI 的理由。随后每个 CI matrix lane 也运行完全相同的
  strict entry point。每次运行把 `go test -json` output 保存为 JSONL，并执行：

  ```bash
  # events is the file populated by tee from the strict child command above.
  jq -se '
    def package_event:
      .Package == "houfeng/internal/center/store/migrate";
    def anchored_event:
      package_event and
      ((.Test // "") | test("^TestPostgresIntegrationAppACLR2($|/)"));
    [.[] | select(package_event)] as $package_events
    | [$package_events[] | select(anchored_event)] as $anchored_events
    | (($anchored_events | map(select(.Action == "run")) | length) > 0)
      and (($anchored_events | map(select(.Action == "pass")) | length) > 0)
      and (($package_events | map(select(.Action == "skip")) | length) == 0)
      and (($package_events | map(select(.Action == "fail")) | length) == 0)
  ' "$events" >/dev/null
  ```

  Pipeline/runner nonzero exit plus this query proves nonzero execution, zero
  skip and zero fail. `platformmigrate` 与 admin CLI 的必要回归在独立
  `go`/`make verify-go` full-test gate 执行；其 green result 不得成为 PG16
  catalog assertion。
- branch-protection jq fixture must contain current `web-browser`
  `{context,app_id:null}`, at least one future numeric pair, and three unique
  successful same-head PG16 runs. It must assert the internal merge retains all
  pairs, the PATCH payload omits only the null pair's app_id, and malformed
  existing pair/non-numeric target app id fails before PATCH.
- protocol fixture must assert first-GET ETag plus a 304 permits the merge, while
  200, changed ETag, or changed canonical checks forces a fresh GET/pages/merge;
  lease absence, retry exhaustion, PATCH ambiguity, or non-superset readback is
  `NEEDS_CONTROLLER` and never a replay.
- controller 是 branch-protection 的唯一执行者。测试必须证明：external
  all-writer mutation lease is present before first GET；the ETag conditional
  GET is 304 before PATCH；200/changed state restarts the full read/merge;
  and post-PATCH state is the exact superset before lease release。任何失败为
  `NEEDS_CONTROLLER`，不得 replay old payload。示例：

  ```bash
  set -euo pipefail
  HEAD=$(git rev-parse HEAD)
  endpoint="repos/$OWNER/$REPO/branches/main/protection/required_status_checks"
  if [ -z "${REQUIRED_CHECKS_MUTATION_LEASE_ID:-}" ]; then
    echo "NEEDS_CONTROLLER: required-check mutation lease is absent" >&2
    exit 1
  fi
  gh api "repos/$OWNER/$REPO/branches/main/protection" \
    --jq '{enforce_admins, required_conversation_resolution}'

  unchanged=0
  for attempt in 1 2 3; do
    snapshot=$(mktemp)
    gh api --include "$endpoint" >"$snapshot"
    required_etag=$(awk 'BEGIN { IGNORECASE=1 } /^etag:/{sub(/^[^:]*: /, ""); print; exit}' "$snapshot")
    required_status_checks=$(awk 'BEGIN { body=0 } /^\r?$/{body=1; next} body{print}' "$snapshot")
    rm -f "$snapshot"
    test -n "$required_etag"
    check_run_pages=$(gh api --paginate --slurp \
      "repos/$OWNER/$REPO/commits/$HEAD/check-runs?per_page=100")
    probe=$(mktemp)
    set +e
    gh api --include -H "If-None-Match: $required_etag" "$endpoint" >"$probe" 2>/dev/null
    set -e
    case "$(awk 'NR == 1 { print $2; exit }' "$probe")" in
      304) rm -f "$probe"; unchanged=1; break ;;
      200) rm -f "$probe"; continue ;;
      *) rm -f "$probe"
         echo "NEEDS_CONTROLLER: conditional required-check GET failed" >&2
         exit 1 ;;
    esac
  done
  test "$unchanged" -eq 1 || {
    echo "NEEDS_CONTROLLER: required checks changed during all retries" >&2
    exit 1
  }
  ```

  controller builds and sends exactly this app-bound merge. It retains all fresh
  existing `checks[]` pairs, gets target `app.id` only from unique successful
  `$HEAD` check-runs, and deduplicates by `[context, app_id]`:

  ```bash
  set -euo pipefail
  merged=$(jq -cen \
    --argjson required "$required_status_checks" \
    --argjson pages "$check_run_pages" \
    --arg head "$HEAD" '
      def numeric_app_id:
        ((.app_id | type) == "number")
        and (.app_id == (.app_id | floor));
      def existing_check:
        if type == "object"
          and ((.context | type) == "string")
          and ((.context | length) > 0)
          and has("app_id")
          and ((.app_id == null) or numeric_app_id)
        then {context, app_id}
        else error("invalid existing required check")
        end;
      [
        "record-platform-pg16-catalog (postgres:16.0)",
        "record-platform-pg16-catalog (postgres:16.6)",
        "record-platform-pg16-catalog (postgres:16.12)"
      ] as $targets
      | ($required.checks
         | if type == "array" then map(existing_check)
           else error("required_status_checks.checks is missing") end) as $existing
      | if $required.strict == true then . else error("strict must remain true") end
      | [ $pages[] | (.check_runs
          | if type == "array" then . else error("check_runs missing") end)[]
          | {context: .name, app_id: .app.id, head_sha, status, conclusion}
        ] as $runs
      | [ $targets[] as $target
          | [ $runs[]
              | select(.context == $target and .head_sha == $head
                       and .status == "completed" and .conclusion == "success"
                       and numeric_app_id)
            ] as $matches
          | if ($matches | length) == 1
            then $matches[0] | {context, app_id}
            else error("expected one successful app-bound check-run for " + $target)
            end
        ] as $slice7
      | {strict: true,
         checks: (($existing + $slice7) | unique_by([.context, .app_id]))}
    ')
  payload=$(jq -ce '
    def request_check:
      if .app_id == null then {context} else {context, app_id} end;
    {strict, checks: (.checks | map(request_check))}
  ' <<<"$merged")
  patch_status=0
  printf '%s\n' "$payload" | gh api --method PATCH "$endpoint" --input - ||
    patch_status=$?
  post_status_checks=$(gh api "$endpoint")
  if ! jq -e --argjson expected "$merged" '
    def numeric_app_id:
      ((.app_id | type) == "number")
      and (.app_id == (.app_id | floor));
    def existing_check:
      if type == "object"
        and ((.context | type) == "string")
        and ((.context | length) > 0)
        and has("app_id")
        and ((.app_id == null) or numeric_app_id)
      then {context, app_id}
      else error("invalid post-PATCH required check")
      end;
    (if type == "object" and .strict == true and (.checks | type) == "array"
     then {strict: true, checks: (.checks | map(existing_check))}
     else error("invalid post-PATCH required-check state")
     end) as $actual
    | ($expected.strict == true)
      and (($expected.checks - $actual.checks) == [])
  ' <<<"$post_status_checks" >/dev/null; then
    echo "NEEDS_CONTROLLER: PATCH readback lost or rewrote a required check" >&2
    exit 1
  fi
  test "$patch_status" -eq 0 || {
    echo "NEEDS_CONTROLLER: PATCH transport result was ambiguous; do not replay" >&2
    exit 1
  }
  ```

  This subendpoint leaves `enforce_admins` and required conversation resolution
  untouched; malformed/stale/ambiguous input is a no-PATCH failure. The null
  request form is only `{context}` inside `checks[]`, never a top-level contexts
  replacement. This controller action must not be claimed before it happens.

#### 7. Wrong vs Correct

```bash
# Wrong：劫持 parent mode 并接受 fallback image。
scripts/test-record-platform-integration.sh postgres -- go test ./...
# HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE defaults to postgres:16-alpine
```

```bash
# Correct：隔离 strict lane 并提供一个 literal image。
HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=postgres:16.12 \
  scripts/test-record-platform-integration.sh pg16-catalog -- \
  go test -json ./internal/center/store/migrate \
  -run '^TestPostgresIntegrationAppACLR2$' -count=1
```

```yaml
# Wrong：未固定的 include/default 能创建第四个 context 或 fallback。
matrix:
  include:
    - postgres_image: postgres:16

# Correct：精确三个 literal value 驱动 named checks。
matrix:
  postgres_image: ["postgres:16.0", "postgres:16.6", "postgres:16.12"]
```

```json
// Wrong: contexts-only overwrite or unsupported conditional PATCH loses binding/CAS safety.
{"strict": true, "contexts": ["web-browser", "record-platform-pg16-catalog (postgres:16.0)"]}

// Correct: retain GET null internally, then omit only its PATCH app_id field.
{"strict": true, "checks": [{"context": "web-browser"}, {"context": "record-platform-pg16-catalog (postgres:16.0)", "app_id": 12345}]}
```

---

## 反模式 / Common Mistakes

- ❌ **跳过 verify 提交**：哪怕"只是改了一行注释"也跑 `make verify-go`，3 秒的事。
- ❌ **`git commit --no-verify`**：当前仓库**没有** pre-commit hook，但如果哪天加了，禁止用 `--no-verify` 绕过。
- ❌ **CI 红了直接 force-push 改一行**：先在本地复现 `make verify-go`，找到根因再提 commit。
- ❌ **TODO 不带 issue 链接**：`grep -rn "TODO\|FIXME" internal/ agent/ cmd/` 当前为空，保持纪录干净。如果必须 TODO，写完整理由 + 跟踪 issue 编号。
- ❌ **新增 handler 但不更新 `bootstrap_test.go` 的 nil 断言**：会让"装配缺失"绕过 verify。
- ❌ **测试用 `time.Sleep(N seconds)` 等 worker tick**：用注入小间隔 + ctx cancel 的 deterministic 模式（参考 `retention/worker_test.go:92`）。
- ❌ **store 测试为了"覆盖更全"启动真 Postgres**：当前生态依赖 `fakeSyncBatchTx` 风格。如果确实要写真 DB 测试，单独走 `_e2e_test.go` 后缀并默认 `t.Skip` 在 env 缺失时跳过；唯一例外是上方已逐项限定的 APP ACL R2 Slice 7 文件/strict runner，不能外推到其他测试。
- ❌ **在窗口/过期判断测试里写会过期的固定未来日期**：例如生产逻辑按真实 `time.Now()` 判定 `renew_at` 是否在 30 天内时，测试夹具不能用 `time.Date(2026, time.June, 11, ...)` 表达"7 天后"。用 `time.Now().UTC().AddDate(0, 0, 7)`，或注入时钟后固定测试时钟。
- ❌ **改 contract 包但不同 PR 改 agent**：`internal/contracts/agentapi/` 的任何 breaking 改动**必须**当 PR 把 agent 也升级，否则 fleet 会立即崩。
- ❌ **改 `db/migrations/` 已合入的 SQL 文件**：见 `database-guidelines.md`。reviewer 看到这种 diff 应直接 reject。
- ❌ **真实 PostgreSQL helper 在返回前 `defer adminPool.Close()`**：`t.Cleanup` 在 test 结束才 drop 临时 database/schema，helper-level defer 会先关闭 admin pool，留下 `closed pool` 和泄漏数据库。正确顺序是先注册 `t.Cleanup(adminPool.Close)`，再注册 drop cleanup，最后注册 test pool close；利用 LIFO 得到 test pool close → drop → admin close，并把 drop error 作为测试失败而不是只记日志。

---

## 已知 gap

- 仓库**没有** `golangci-lint`、没有 race-by-default、没有 coverage 阈值。如果未来引入，应同步更新 `.github/workflows/ci.yml`、`Makefile`、本文件，并在当前 active docs 或 `.trellis/spec/` 记录变更。
- `make verify-web` 跑 `npm ci`，每次清空 `node_modules` 后重装，本地反复跑会比较慢；若需要本地速度，单独 `cd web && npm test` / `npm run lint` 即可，CI 仍然走完整 `verify-web`。
- center / agent 的 slog handler 仍是 stdlib text 输出，未配置最小 level、source 行号或 trace id。center 支持 `HOUFENG_LOG_FILE` tee 到文件，但不做内建轮转；见 `logging-guidelines.md`。
