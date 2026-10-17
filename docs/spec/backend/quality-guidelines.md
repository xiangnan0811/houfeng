# 代码质量规范

> 项目权威入口为根 `AGENTS.md`；领域行为见 [合同索引](../contracts/README.md)。

---

## Overview

后端 source 质量门为 `make verify-go`（=`check-fmt-go` + `vet-go` + `test-go`），不修改源码；格式修复使用显式 `make fmt-go`。CI 与本地共享 `scripts/verify-ci.mjs`，其 Go lane 调用同一 Make target。

`make verify` / `./scripts/verify.sh` 保留 docs → Go → Web；完整 CI 对齐入口为 `make verify-ci`，另含真实 PostgreSQL catalog/业务、Chromium 和 Docker image build。hooks 按统一分类执行全部适用门禁，不由手工挑选替代。

不存在以下东西，**不要新增**（除非通过设计基线评审）：

- ❌ `golangci-lint`（仓库根 `find . -name '.golangci*' -maxdepth 3` 为空）
- ❌ 另行引入 pre-commit framework / husky / lefthook。已有 `.githooks/` 通过 `sh scripts/setup-git-hooks.sh` 启用，同时执行 main/master 保护与完整适用候选门禁，见 [Local Hook Contract](../../development/branch-workflow-governance.md#local-hook-contract)。
- ❌ goimports 单独配置（`go fmt` 已经统一格式）
- ❌ 任何 ORM、SQL 生成器（参见 `docs/spec/backend/database-guidelines.md`）

---

## 命令门户

| 命令 | 实际行为 | 来源 |
|------|----------|------|
| `make fmt-go` | `go fmt $(go list ./agent/... ./cmd/... ./db/... ./internal/...)` | `Makefile` 的 `fmt-go` |
| `make check-fmt-go` | 枚举同范围实际 Go/Cgo/Test/XTest 文件，`gofmt -l` 报路径并失败，不写文件 | `Makefile` 的 `check-fmt-go` |
| `make vet-go` | 同范围 `go vet` | `Makefile` 的 `vet-go` |
| `make test-go` | 同范围 `go test` | `Makefile` 的 `test-go` |
| `make verify-go` | `check-fmt-go + vet-go + test-go` | `Makefile` 的 `verify-go` |
| `make build-center` | 构建 `./cmd/houfeng-center` 到 `./bin/houfeng-center`，注入 `main.version` | `Makefile` 的 `build-center` |
| `make build-agent` | 构建 `./cmd/houfeng-agent` 到 `./bin/houfeng-agent` | `Makefile` 的 `build-agent` |
| `make verify-web` | 根目录执行工具链回归及检查；`env -u NODE_ENV npm --prefix web ci --include=dev`，test 环境 lint/coverage、production build、bundle:check、css:analyze | `Makefile` 的 `verify-web` |
| `make verify-docs` | Node 标准库检查本地链接、图片、标题锚点、目录索引/可达性、旧路径残留与平台导入，并运行检查器正反例 | `Makefile` 的 `verify-docs` |
| `make verify` | `./scripts/verify.sh` 串起 `verify-docs + verify-go + verify-web` | `Makefile` 的 `verify`、`scripts/verify.sh` |
| `make test-ci-tools` | Node 标准库分类/manifest/候选/聚合行为测试，不启动重门禁 | `Makefile`、`scripts/*.test.mjs` |
| `make verify-ci` | docs、Go、三个 PG16 catalog、business PostgreSQL、Web、完整 Chromium、image 顺序执行 | `scripts/verify-ci.mjs` |

**注意**：

- `make test-go` **不带 `-race`**。新加并发原语时如果担心 race condition，可以本地手工跑 `go test -race ./...`，但默认 verify 链路不开。
- `verify-go` 的范围是 `./agent/... ./cmd/... ./db/... ./internal/...`，不包括 `web/`、`docs/`、`scripts/`。新增 Go 顶层目录时记得改 Makefile 的 `GO_VERIFY_PATTERNS`。
- 单测可在仓库根使用 `go test ./internal/center/store -run TestPostgresMonitoringInstanceRepository` 这种点对点形式；按本规范和实际改动范围补齐适用门禁。

---

## GitHub Actions workflow 合约

### 1. Scope / Trigger

触发条件：修改 workflow、共享 runner、分类器、hooks 或 Make verify targets 时，必须保持本地与 CI 的完整适用门禁一致，且保留远端 required 名称；不以源码中出现命令视为真实执行证据。

### 2. Signatures

当前 workflow job 合约：

- `changes` 执行 CI-tool 行为测试并分类实际 tree：PR event base → checkout merge candidate，main push before → checkout SHA；无可靠 baseline 时选择全部。
- `go-check` 与 `business-postgres` 按分类运行共享 runner；三个 `record-platform-pg16-catalog` 实例始终展开并验证分类，按选择运行原有 strict runner 或明确报告不适用。required `go` 聚合这些结果，数据库失败同样阻断它。
- `web` 始终验证分类输出，运行 docs lane（工具测试、docs、严格 manifest 校验），再按选择运行 Web lane。
- `web-browser-shard` 为两个独立构建、互不等待 Web coverage 的 shard；required `web-browser` 只聚合选择与矩阵结果。
- `docker-image` 验证分类，再按选择使用 Buildx/build-push-action 构建 root Dockerfile，`VERSION=dev`、`push: false`；发布仍由 `publish-images.yml` 单独负责。
- Go 使用 `actions/setup-go@v6` / `go-version-file: go.mod`，Node 使用 `actions/setup-node@v6` / `node-version-file: .node-version`，npm cache 使用 `web/package-lock.json`。

### 3. Contracts

- 统一分类见 [参与开发](../../../CONTRIBUTING.md#影响范围与轻量门禁)：metadata/普通 docs 仅轻量；Web 合同文档选 Web/browser；部署 docs、README、特殊 Compose 合同选全套；`web/**` / `.node-version` 选 Go/Web/browser/image，不选 PG；后端/自动化及未知输入选全套。新增跨层消费者须同步分类和行为测试。
- 本地按 `origin/main` 与 HEAD 的唯一 merge-base 比较整个 staged/HEAD 候选，覆盖累计分支改动；无可靠历史跑全套，Git 错误失败。混合候选、index/HEAD/branch 稳定性和实际 push OID 限制以 [Local Hook Contract](../../development/branch-workflow-governance.md#local-hook-contract) 为准。
- runner 固定 docs → go → postgres → business-postgres → web → browser → image，首个失败停止，不保存成功凭证。单 lane 调试可用 `node scripts/verify-ci.mjs --gates <lane>`；hooks 不接受环境变量跳过适用门禁。
- postgres lane 顺序执行 `postgres:16.0`、`postgres:16.6`、`postgres:16.12`，strict catalog 要求 `TestPostgresIntegrationAppACLR2`、`TestPostgresIntegrationAppACLCurrent` 及包实际 RUN/PASS；业务 runner 同样要求既有 anchors。skip/fail、遗漏或无效 JSON 都失败，不以普通 Go 测试的缺环境 skip 充数。
- 四个 host-network PostgreSQL fixture 仅监听 `127.0.0.1`，逐个启动并通过 TCP readiness 后才启动下一个，避免无必要的通配地址绑定冲突。端口检查不是预留；真正的 loopback 冲突仍失败并清理，不重试、不回退 IPv6/Unix socket，四个 system identifier 必须两两不同。
- `web/package.json` 缺失必须失败。本地适用数据库/image lane 需要真实 Docker daemon，image 还需要 buildx；缺依赖或 build 失败明确阻止 commit/push，不能留给 CI 或发布 workflow 兜底。image lane 只构建，不登录/push/签名。
- runner 子进程清除 database/PG、`HOUFENG_*`、Vite、Node/Go/Make 选择与 Git-local/config overrides，固定 `GOTOOLCHAIN=local`、`GOWORK=off`、`GOENV=off`；普通 Go 不 opt-in 数据库。strict runner 自建隔离 URL，保留 PATH/HOME/缓存/必要代理，不能宣称恶意代码沙箱。不改调用者环境/index，不读取凭据。未跟踪（含 ignored）的私有 Web dotenv 文件只报路径并阻止 Web/browser。
- required `go` / `web-browser` 聚合只接受 `changes=success` 和严格字符串 `true|false` 选择。catalog 在 postgres 两种选择模式下都必须 success；Go check、业务 PostgreSQL、browser shards 各按对应选择，选中必须 success，未选中必须 skipped。missing/failure/cancelled、catalog skipped 或其他不合选择的结果一律失败。`web` / `docker-image` 也必须验证全部五个分类输出。
- catalog job 使用 `if: always()` 保持三个精确 required contexts；每个实例在 checkout/setup/数据库操作前要求 `changes=success` 和严格字符串 `postgres=true|false`，无效或缺失值失败。true 保持原有 checkout、Go/Node setup 与 strict PostgreSQL 命令；false 仅输出“不适用”并成功，不 checkout、不安装工具链、不启动数据库，不能记为真实 PostgreSQL 验收。不修改 required 保护或 bypass 来解决 context 缺失。
- job-level 条件只用 Actions 支持的上下文/status 函数；`hashFiles` 仅用于支持它的 step/cache 位置。过期 PR run 可以取消，main run 不因此取消；不改变发布/部署顺序或授权。

### 4. Validation & Error Matrix

| 条件 | 预期 | 失败表现 |
|------|------|----------|
| `jobs.<job_id>.if` 使用 unsupported function，例如 `hashFiles(...)` | 禁止 | GitHub Actions run 直接 `failure`，`jobs=[]`，`log not found`，check suite `latest_check_runs_count=0` |
| `.github/workflows/ci.yml` 包含 `hashFiles` | 只有在 step-level 支持位置才允许 | job 创建前失败或表达式校验失败 |
| 任一选中的 Go/business/browser 子项失败、取消或缺失 | required 聚合失败 | 不能因其他子项绿而放行 |
| 任一 catalog 在 postgres=true 或 false 时失败、取消、skipped 或缺失 | required `go` 失败 | false 也须有三个明确成功的“不适用” contexts |
| 未选中的 Go/business/browser 子项不是 skipped；分类失败或选择无效 | required 聚合失败 | 不能把未知当“不适用” |
| `web/package.json` 缺失 | Web lane 失败 | 不输出占位成功 |
| 适用 image/数据库 lane 缺 Docker daemon/buildx/工具 | 本地阻塞 | 修复环境后执行，不跳过 |

### 5. Good/Base/Bad Cases

- Good：metadata-only/普通 docs 候选只跑轻量门禁；三个 catalog 实例验证合法分类并明确“不适用”成功，其余未选中子项为 expected skipped，required 聚合据此通过，不宣称数据库验收。
- Base：后端变更选全部；Go 使用精确工具链与非修改式格式检查，三个 catalog 和业务数据库结果进入 required `go`。
- Good：本地真实 image build 与 PR `docker-image` 都成功；仍不等同于发布、真实安装或生产验收。
- Bad：job-level 使用 `hashFiles`，或简单让 required job 随条件消失。
- Bad：本地缺 Docker 就告警放行，或数据库/任一 browser shard 红灯而聚合绿灯。

### 6. Tests Required

- `make test-ci-tools` 覆盖分类例外、删除/重命名/特殊路径、manifest 正反例、真实 Git 候选与聚合真值表，包括 postgres 两种模式下 catalog failure/cancelled/skipped/missing 的拒绝，以及 false 时业务 PostgreSQL 必须 skipped；不要用 Make/YAML 字符串断言重新钉死 DAG。
- workflow 修改使用 actionlint 校验表达式/矩阵上下文；语法检查不替代实际 Actions。
- `make verify-ci` 执行所有真实 lanes；额外 focused acceptance 按包合同运行。小 Go fixture 应证明未格式化时失败且字节不变、显式 fmt 后通过。
- PR 观察全部适用实例，不仅 required 摘要：Go check、三个 PG16 catalog、业务 PostgreSQL、两个 browser shards、Web 和 image。metadata-only/普通 docs 也必须观察三个精确 catalog contexts 的明确“不适用”成功；轻量分类不等于执行真实数据库重门禁。
- image build 只证明可构建；发布仅在另有授权时观察 `publish-images` 的 build/publish/inspect，不以本地/PR 构建宣称发布或生产成功。

### 7. Wrong vs Correct

#### Wrong

```yaml
web:
  if: ${{ hashFiles('web/package.json') != '' }}
  runs-on: ubuntu-latest
```

#### Correct

```bash
# 使用共享入口；完整调用不省略 PostgreSQL、browser 或 image。
make verify-ci
# 单项调试入口；提交/推送仍由 hooks 选择完整适用集合。
node scripts/verify-ci.mjs --gates go
```

---

## 测试约定

### 测试文件位置 / 命名

- 单元测试与被测文件**同目录同包**：`store/sync_batches.go` ↔ `store/sync_batches_test.go`。
- 跨包黑盒测试用 `<package>_test` 包名：`internal/center/http/handlers/monitoring_instances_test.go` 第 1 行 `package handlers_test`。
- 集成测试按边界选清晰后缀，而不是规定唯一命名：HTTP end-to-end 可沿用
  `auth_e2e_test.go`；真实 PostgreSQL store 测试沿用
  `*_postgres_integration_test.go`，例如
  `sync_batches_postgres_integration_test.go`、
  `app_acl_r2_postgres_integration_test.go` 与
  `app_acl_current_postgres_integration_test.go`。测试名和文件名必须让 focused
  selector、fixture ownership 与 cleanup scope 可辨认。
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

普通 store 单元测试默认使用 fake `pgx.Tx` / `pgx.Row`，保持快速、确定且不依赖外部服务；真实 PostgreSQL 的权限检查、trigger、catalog、外键级联或事务语义则使用现有 `*_postgres_integration_test.go`、隔离 fixture 和 runner，不能用 fake 结果代替。

参考模板 `internal/center/store/sync_batches_test.go:175-260` 的 `fakeSyncBatchTx`：

- 实现 `Exec` / `QueryRow` / `Query` / `Commit` / `Rollback`，记录调用。
- 通过 `execErrForSQLSubstring` 等字段按 SQL 文本子串决定哪一步注入错误，验证事务 rollback 行为。
- 仓库构造时直接替换 `beginTx` 字段（`PostgresSyncRepository.beginTx`，见 `store/sync_batches.go:30-36`），跳过真 pgxpool。

- 普通 `make verify-go` 保留现有行为：真实 PostgreSQL fixture / environment 缺失时 integration test 可以 skip，因此 broad local verify 不要求本机常驻 PostgreSQL。
- 当 database/package spec 把某个真实数据库测试指定为 acceptance gate 时，必须通过隔离 strict runner 实际产生 RUN/PASS；runner 看到 SKIP 必须失败。示例：`TestPostgresIntegrationAgentSyncBatchRuntimeACL` 按 `database-guidelines.md` 的 “Agent sync batch INSERT-only idempotency” scenario 使用 `postgres` mode 验证 direct-runtime ACL。
- `postgres` mode 的此类 focused acceptance 是任务/包合同，不自动成为默认 CI required job；只有 workflow 与对应 spec 明确声明的 job 才是 required CI gate。
- 不要为了“覆盖更全”随意启动 PostgreSQL；仅在 fake 无法证明的权限、trigger、catalog 或事务边界使用既有 runner/fixture，并保持临时角色、数据库与连接的隔离 cleanup。

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

`agent/syncqueue.Options.SkipFsync` 是**测试专用开关**，避免 macOS APFS 等慢文件系统的 fsync 拖慢运行时计时类测试（修复历史见 `git log -- agent/syncqueue/`）。

约定：

- **生产调用必须留 `SkipFsync = false`**（默认零值），保证崩溃恢复语义。
- 测试里如果跑的是运行时 retry / 重启逻辑、不在乎崩溃恢复，可以显式置 `SkipFsync: true`。
- 不要把 `SkipFsync` 暴露到 env 配置里。

## 工具链

### `go fmt`

- 强制：`make check-fmt-go` 是 verify-go 第一项，未格式化代码返回非零且不改文件；`make fmt-go` 是开发者显式修复入口。
- 不引入 `goimports` 单独 hook：`go fmt` 已经够用，import 顺序按 stdlib → 三方 → houfeng 内部三段，靠手动维持（参考 `cmd/houfeng-center/bootstrap.go:1-30` 的 import 块写法）。

### `go vet`

- 强制：`make vet-go` 抓 printf-arg 不匹配、shadow 变量、未对齐 struct tag 等。
- 当前没有 `//go:vet` 跳过指令，也没有 `// nolint` 注释。**新代码不要随便加忽略**。

### `go test`

- `go test` 默认开 `-cover` 不输出，CI 也不强制 coverage 阈值。**不要在测试文件里写 `t.Skip("coverage too low")` 这种 hack**。
- 启用 race 检测靠手动 `go test -race ./...`，不在 verify 链路。

### Go 版本

`actions/setup-go@v6` 通过 `go-version-file: go.mod` 锁定精确工具链。本地完成/发布门禁也必须使用 `go.mod` 声明的精确版本；仅仅“更高版本”不能替代该证据，因为标准库编码器等实现细节可能让 byte-level golden 在未来工具链上产生不同输出。
共享 CI runner 在首个 Go/catalog/business-PostgreSQL lane 执行前核对实际 Go 版本，必须与 `go.mod` 精确一致；不匹配时失败并提示调整 PATH，不自动下载或接受更新版本。轻量 docs/Web-only 门禁不因此依赖 Go。

当前仓库门禁示例为 `make verify-go`。如果默认 `go version` 不同，使用精确工具链完成验收；不得因为未来/实验性工具链的不同摘要直接更新 golden 或修改生产代码。

图片预览的合同是可解码、尺寸与像素正确、去元数据、资源限制有效，不是标准库 PNG 编码字节跨工具链恒定。此类测试应比较解码后的输出与实际输入图像（JPEG 须与 JPEG 解码结果比较），不能重钉压缩字节哈希。内容寻址存储仍必须对实际输出字节计算摘要；该身份合同与预览编码的偶然字节表示不可混淆。

手工 focused Go 测试可将 `GOTMPDIR` 放到有余量的文件系统，但长目录可能超过 Unix socket 限制，不能通过跳过测试解决。共享 runner 不采用 ambient tmp/Go 选择，而在 checkout `tmp/agent/` 创建唯一自有 scratch，统一临时目录并清理自有资源；不要清理其他任务或读取私有凭据文件。

---

## 提交前清单

下面这条清单是 happy-path，按顺序勾：

1. [ ] **确认实际完整候选与统一分类**；hooks 执行适用门禁，未知/缺 baseline 走全套，不按“只是文档”自行跳过。
2. [ ] **Go 选中时用 `go.mod` 精确工具链通过非修改式 `verify-go`**；需修复格式时先显式 `make fmt-go`，再重新验证。
3. [ ] **区分 `make verify` 与 `make verify-ci`**：前者仅 docs/Go/Web，后者包括真实 PG、完整浏览器和 image；相关 UI/生产验收仍按各自合同。
4. [ ] **改了迁移 / 表结构** → 跑一次 `docs/operations/fresh-install-smoke-run.md` 的 fresh-install；若发现可复用的 gap 或规则，补到 `docs/spec/` 或当前 active docs。
5. [ ] **改了 user-visible 的 UI** → 对照 `docs/design/{interface-language.md,component-patterns.md}`，并按 `docs/development/ui-preview-and-browser-sanity.md` 给出 preview URL、已检查 routes / viewports、browser sanity、local screenshot notes（如有，默认不提交）。如果任务改变了可复用 UI 方向，同步更新 `docs/design/` 或相关 `docs/spec/`；历史内容通过 Git 追溯，不恢复占位或已完成提案。
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
| 引入新 worker | 1) `internal/center/<x>/worker.go` 实现 `Worker.Run(ctx) error`；2) `cmd/houfeng-center/bootstrap.go` 添加构造与传给 `centerapp.New(...)`；3) 更新 bootstrap 装配测试。基础 worker 为 incident、retention、session cleanup、订阅汇率与订阅提醒五个，Records 根据配置继续追加；按实际模式核对集合，不固定为旧的三个。 |
| agent 端新采集 / 新探针 | 1) `agent/hostsample/` 或 `agent/probe/` 实现采集；2) 通过 `agent/runtime/runtime.go` 的 `buildSyncRequest` 串接；3) 必要时改 `internal/contracts/agentapi/` DTO（不可单边） |

---

## 反模式 / Common Mistakes

- ❌ **手选更小检查代替适用门禁**：以统一分类及累计候选为准；普通 docs 可轻量，后端即使只改注释也选择全部。
- ❌ **`git commit --no-verify`**：先启用 `.githooks/`，保留本地/远端 main/master 保护与候选检查；不绕过混合候选拒绝或因缺 Docker 放行。
- ❌ **CI 红了直接 force-push 改一行**：根据实际失败 lane 找根因，通过受影响本地门禁后再提交。
- ❌ **TODO 不带 issue 链接**：`grep -rn "TODO\|FIXME" internal/ agent/ cmd/` 当前为空，保持纪录干净。如果必须 TODO，写完整理由 + 跟踪 issue 编号。
- ❌ **新增 handler 但不更新 `bootstrap_test.go` 的 nil 断言**：会让"装配缺失"绕过 verify。
- ❌ **测试用 `time.Sleep(N seconds)` 等 worker tick**：用注入小间隔 + ctx cancel 的 deterministic 模式（参考 `retention/worker_test.go:92`）。
- ❌ **store 测试为了“覆盖更全”随意启动真 PostgreSQL**：普通单元测试优先 `fakeSyncBatchTx` 风格；只有权限、trigger、catalog、外键或事务语义无法由 fake 证明时，才沿用隔离的 `*_postgres_integration_test.go` fixture/runner 和完整 cleanup。普通 broad verify 可在环境缺失时 skip；被 database/package spec 指定为 acceptance gate 的测试必须经 strict runner 实际 RUN/PASS，SKIP 不是证据。
- ❌ **在窗口/过期判断测试里写会过期的固定未来日期**：例如生产逻辑按真实 `time.Now()` 判定 `renew_at` 是否在 30 天内时，测试夹具不能用 `time.Date(2026, time.June, 11, ...)` 表达"7 天后"。用 `time.Now().UTC().AddDate(0, 0, 7)`，或注入时钟后固定测试时钟。
- ❌ **改 contract 包但不同 PR 改 agent**：`internal/contracts/agentapi/` 的任何 breaking 改动**必须**当 PR 把 agent 也升级，否则 fleet 会立即崩。
- ❌ **改 `db/migrations/` 已合入的 SQL 文件**：见 `database-guidelines.md`。reviewer 看到这种 diff 应直接 reject。
- ❌ **真实 PostgreSQL helper 在返回前 `defer adminPool.Close()`**：`t.Cleanup` 在 test 结束才 drop 临时 database/schema，helper-level defer 会先关闭 admin pool，留下 `closed pool` 和泄漏数据库。正确顺序是先注册 `t.Cleanup(adminPool.Close)`，再注册 drop cleanup，最后注册 test pool close；利用 LIFO 得到 test pool close → drop → admin close，并把 drop error 作为测试失败而不是只记日志。

---

## 已知 gap

- 仓库**没有** `golangci-lint`、没有 race-by-default、没有 coverage 阈值。如果未来引入，应同步更新 `.github/workflows/ci.yml`、`Makefile`、本文件，并在当前 active docs 或 `docs/spec/` 记录变更。
- `make verify-web` 按锁文件重新安装依赖；本地 focused 测试不代替完整适用门禁。CI Web lane 通过 `node scripts/verify-ci.mjs --gates web` 调用 `NODE_ENV=production make verify-web`。
- center / agent 的 slog handler 仍是 stdlib text 输出，未配置最小 level、source 行号或 trace id。center 支持 `HOUFENG_LOG_FILE` tee 到文件，但不做内建轮转；见 `logging-guidelines.md`。
