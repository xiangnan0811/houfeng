# 参与开发

先阅读 [AGENTS.md](AGENTS.md) 和 [相关规范](docs/spec/README.md)。个人 harness 不是开发依赖。
在非主分支工作，保护现有修改，运行 `sh scripts/setup-git-hooks.sh` 启用版本化 Git 安全 hooks。

## 工作目录和工具链

以下命令在 checkout 根运行（`git rev-parse --show-toplevel`）。Go module 在根，后端源码在
`cmd/`、`agent/`、`internal/` 和 `db/`；前端在 `web/`。从子目录运行 Makefile 时使用
`make -C "$(git rev-parse --show-toplevel)" <target>`，不要假设存在 `backend/`。

- Go 版本以 `go.mod` 为准，Node 以 `.node-version` 为准；Web 依赖使用 `web/package-lock.json`。
- `make verify-go`：非修改式 `check-fmt-go`、vet、Go tests；完整迁移链验证使用每包 30 分钟超时，保留全部链路断言。`make check-fmt-go` 只报告未格式化文件并失败；修复请显式运行 `make fmt-go`。
- `make test-web-toolchain`：Node-major checker 行为与保留的 Web/staging 静态合同检查；不是实际 staging 验收。
- `make verify-docs`：文档链接、锚点、导航、平台导入和旧路径检查，含检查器回归。
- `make verify-web`：独立于调用者 NODE_ENV 安装依赖，lint、覆盖率测试、build、bundle 和 CSS 检查。
- `make verify`：保留 docs → Go → Web，调用 `scripts/verify.sh`；不包含真实数据库、浏览器或镜像门禁。
- `make verify-ci`：完整 CI 对齐入口，顺序执行 docs → Go → 三版本 PG16 catalog → business PostgreSQL → Web → 完整 Chromium suite → image build，首个失败立即停止。
- `make test-ci-tools`：Node 标准库行为测试，覆盖分类、manifest、候选身份及 required 聚合；不启动重门禁。
- `bash scripts/test-business-postgres.sh`：真实隔离 PostgreSQL 业务门禁，固定执行认证、设置、
  归档、维护和附件/租约 anchor；每个顶层测试与包必须通过，任何 skip/fail 或遗漏都失败。
  需要 Docker、bash、jq、ss 和 setsid；临时状态保存在 checkout 的 `tmp/` 下。
  下载的真实 S3/MinIO 验收另用 `scripts/run-records-integration.sh --profile s3`，
  不由此 PostgreSQL 门禁代替。
- `make build-center`、`make build-agent`：本地二进制构建。
- `npm --prefix web run dev`：前端开发服务（先按锁文件安装依赖）。

聚焦检查使用根目录 `go test ./internal/center/<package>`，或 `web/` 下
`npx vitest run <test-path>`，用于迭代，不代替提交/推送时的完整适用门禁。
真实 lane 需要对应工具：Git、Node/npm、Go/gofmt；数据库需要可用 Docker、bash、jq、ss、
setsid，镜像需要 Docker daemon/buildx，浏览器需要 lockfile-compatible Chromium 和系统库。
适用门禁缺工具、daemon、镜像或构建失败均阻止提交/推送，不能以 CI 将来会跑为由放行。
只读调查不运行会改格式、安装依赖或写私有状态的命令。

### 影响范围与轻量门禁

`scripts/ci-scope.mjs` 是 hooks/CI 共用分类器，多个路径取并集，不按文件扩展名或分支名豁免：

| Git 根相对路径 | 适用重门禁 |
| --- | --- |
| 精确根 `CHANGELOG.md`、`.release-please-manifest.json`；普通 `docs/**` | 无，仍运行轻量门禁 |
| `docs/spec/web/**`、`docs/development/ui-preview-and-browser-sanity.md` | Web、browser |
| `README.md`、`docs/deploy/**`、`docs/spec/contracts/platform/{production-compose,compose-authority}.md` | 全部 |
| `web/**`、`.node-version` | Go、Web、browser、image，不选 PostgreSQL |
| 后端、数据库、fixtures、构建/治理脚本及其他未列出路径 | 全部 |

特殊 docs 规则优先于普通 docs；Web 类型/browser corpus 与 Go、后端 CSP/fixtures 与 Web
有跨层消费者，新增此类依赖必须同步分类和行为测试。重命名同时计入删除/新增路径。
轻量门禁始终执行 `make test-ci-tools`、`make verify-docs` 和 release manifest 校验：
manifest 必须是仅有 `.` 键的对象，值为无 `v` 前缀、无前导零的三段非负整数稳定版本字符串
（零本身允许）；缺失、无效 JSON、多包、非字符串、预发行或非法版本均失败，不自动修复。
无改动也运行轻量门禁；未知/不能无损解码的路径或不可靠 baseline 选择全套，
Git 查询错误则失败，不能当空 diff 跳过。

## 提交和 PR

提交前确认实际候选、规范同步、适用本地检查和 [独立审查](docs/development/agent-collaboration.md)。
commit/merge hooks 验证完整 staged 候选，push hook 重新验证 HEAD；未暂存 tracked 改动或
nonignored untracked 文件一律拒绝，不自动 stage/stash/clean，不支持部分暂存快照。
本地分类比较 `origin/main` 与 HEAD 的唯一 merge-base 到整个候选，覆盖分支累计改动，
不只看本次 staged diff；浅历史、缺 baseline、无/多 merge-base 时不 fetch，改跑全套。
每个 lane 前后及结束核对 index/HEAD/branch 和干净状态，变化即使检查失效。
push 使用完整 stdin 的实际 OID，只接受可 peel 到当前 HEAD 的非删除 ref；其他提交/分支须切换后单独推送。
protected main/master 更新、删除、force 均拒绝，非保护 ref 纯删除无需门禁。
详见 [Local Hook Contract](docs/development/branch-workflow-governance.md#local-hook-contract)。
不复用 commit 成功结果、不绕过 hooks；清晰的小改动无强制任务文档、规划审批或日志。

PR 后持续监控实际 CI；required 名称保留 `go`、`web`、`web-browser`、`docker-image`，
以及三个精确 `record-platform-pg16-catalog (postgres:16.0)`、
`record-platform-pg16-catalog (postgres:16.6)`、`record-platform-pg16-catalog (postgres:16.12)`；
分支保护实际 required 集合须现场核对，不修改保护或 bypass 来绕过缺失 context。
catalog matrix 始终展开；每个实例先要求 `changes=success` 与严格字符串 `postgres=true|false`。
选中时执行原有 checkout、Go/Node setup 和 strict PostgreSQL runner；未选中时仅报告不适用并成功，
不 checkout、不安装工具链、不启动数据库，也不构成真实 PostgreSQL 验收证据。
`go` 聚合要求 catalog 在两种模式下都 success；Go check 按自身选择、业务 PostgreSQL 按 postgres 选择，
选中必须 success、未选中必须 skipped。`web-browser` 聚合两个独立构建的 browser shards，
选中必须全部 success、未选中必须 skipped。分类失败、无效选择、任何 catalog 的失败/取消/skipped/缺失，
或其他不符合选择的结果，都不能得到绿色聚合；metadata/普通 docs 同样遵守此合同。
CI 比较实际 PR merge candidate 与 event base，main push 比较 before 与 checkout SHA；
不可靠 event baseline 跑全套。旧 PR run 可取消，main run 不因此中断。
本地聚焦测试不能代替 CI；数据库 skip 不能写成真实 PostgreSQL 验收通过。

合并、发布、部署需要对应授权；按 [交付规范](docs/development/branch-workflow-governance.md)
处理适用的合并后检查。真实 staging 有 main 限制和独立凭据，见
[浏览器验收](docs/development/ui-preview-and-browser-sanity.md)；本地 mock 不能替代真实环境。
生产变更遵循 [运维入口](docs/README.md)，不要把构建/发布成功说成已部署。
