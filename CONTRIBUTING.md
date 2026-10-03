# 参与开发

先阅读 [AGENTS.md](AGENTS.md) 和 [相关规范](docs/spec/README.md)。个人 harness 不是开发依赖。
在非主分支工作，保护现有修改，运行 `sh scripts/setup-git-hooks.sh` 启用版本化 Git 安全 hooks。

## 工作目录和工具链

以下命令在 checkout 根运行（`git rev-parse --show-toplevel`）。Go module 在根，后端源码在
`cmd/`、`agent/`、`internal/` 和 `db/`；前端在 `web/`。从子目录运行 Makefile 时使用
`make -C "$(git rev-parse --show-toplevel)" <target>`，不要假设存在 `backend/`。

- Go 版本以 `go.mod` 为准，Node 以 `.node-version` 为准；Web 依赖使用 `web/package-lock.json`。
- `make verify-go`：format、vet、Go tests；format 可能改文件，运行后复查候选。
- `make test-web-toolchain`：工具链与 Web 质量门禁脚本回归。
- `make verify-docs`：文档链接、锚点、导航、平台导入和旧路径检查，含检查器回归。
- `make verify-web`：独立于调用者 NODE_ENV 安装依赖，lint、覆盖率测试、build、bundle 和 CSS 检查。
- `make verify`：项目完整本地入口，调用 `scripts/verify.sh`。
- `bash scripts/test-business-postgres.sh`：真实隔离 PostgreSQL 业务门禁，固定执行认证、设置、
  归档、维护和附件/租约 anchor；每个顶层测试与包必须通过，任何 skip/fail 或遗漏都失败。
  需要 Docker、bash、jq、ss 和 setsid；临时状态保存在 checkout 的 `tmp/` 下。
  下载的真实 S3/MinIO 验收另用 `scripts/run-records-integration.sh --profile s3`，
  不由此 PostgreSQL 门禁代替。
- `make build-center`、`make build-agent`：本地二进制构建。
- `npm --prefix web run dev`：前端开发服务（先按锁文件安装依赖）。

聚焦检查使用根目录 `go test ./internal/center/<package>`，或 `web/` 下
`npx vitest run <test-path>`。检查范围取决于改动和 [后端](docs/spec/backend/quality-guidelines.md)、
[Web](docs/spec/web/quality-guidelines.md) 合同，文档改动需检查真实链接及依赖该文档的测试。
只读调查不运行会改格式、安装依赖或写私有状态的命令。

## 提交和 PR

提交前确认实际候选、规范同步、适用本地检查和 [独立审查](docs/development/agent-collaboration.md)。
暂存后、推送前再核对内容与验证条件；不能只凭相同 HEAD 复用旧结果。不绕过 hooks。
清晰的小改动直接完成，无强制任务文档、规划审批或日志。

PR 后持续监控 required CI；`.github/workflows/ci.yml` 包含 Go、三个 PG16 catalog 版本、
独立业务 PostgreSQL 门禁、Web、Chromium 和 Docker build。分支保护实际 required 集合须现场核对。
本地聚焦测试不能代替 CI；数据库测试跳过不能写成真实 PostgreSQL 验收通过。

合并、发布、部署需要对应授权；按 [交付规范](docs/development/branch-workflow-governance.md)
处理适用的合并后检查。真实 staging 有 main 限制和独立凭据，见
[浏览器验收](docs/development/ui-preview-and-browser-sanity.md)；本地 mock 不能替代真实环境。
生产变更遵循 [运维入口](docs/README.md)，不要把构建/发布成功说成已部署。
