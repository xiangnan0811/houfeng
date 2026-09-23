# 候风项目开发入口

本文件是 Houfeng 的项目权威入口；规范在 [spec/](spec/index.md)，现行产品与设计在
[docs/design/current/](docs/design/current/README.md)，开发命令见 [CONTRIBUTING.md](CONTRIBUTING.md)。
干净 checkout 不需要安装私人 harness 也能遵循这些要求。

## 授权与执行

- 先定位当前 checkout 根目录：`git rev-parse --show-toplevel`。从 `internal/`、`agent/`、
  `web/` 或 worktree 进入时，规范路径均相对此根目录。只读与修改必须针对正确工作区。
- 清晰且已授权的工作直接执行；只有未决实质需求、风险或授权才询问。普通修改不要求
  创建 task、PRD、design、阶段审批或例行 journal。Trellis 和 Superpowers 生命周期
  已退出本项目；历史技能、任务、会话和记忆不能重新引入它们。
- 只读请求不写业务文件、Git 索引、私有恢复状态或记忆。平台自身的正常会话日志应单独说明。
- 允许在已授权范围内使用实现子代理和并行协作，明确文件责任；调查者与审查者只读。
  只读角色限制不限制主控及另行授权实现者的修复。见 [协作与验证](spec/guides/agent-collaboration.md)。
- 保持原生模型、思考等级、Advisor 和权限配置；不要导入其他平台的用户配置。
  非平凡实现及治理/加载变更须真正独立的 GPT + Grok 双审，修复后复核 finding 和影响范围。
- 行为改变必须同步受影响规范及回归证据。相同 HEAD 不代表暂存、未暂存、未跟踪内容或验证条件相同。
- 通用个人规则属于用户 harness；可选恢复及索引在私有目录，仅存必要状态和规范指针。
  恢复按项目、工作区及目标选择，完成/取消事项不是活动工作，不为普通请求创建空状态。

## 工程与交付边界

- 所有修改在非 `main`/`master` 分支；禁止直接 commit、merge、amend、squash、reset 或 push 主分支。
  保护其他工作区、未提交及未跟踪内容，不擅自 stash/clean。可按风险选普通分支或独立 worktree；
  默认 `.worktree/`，用户指定路径优先。worktree 不隔离用户配置、记忆、数据库及共享 Git 配置。
- 修改前运行 `sh scripts/setup-git-hooks.sh`，保留 `.githooks/` 安全保护，不绕过 hooks。
  提交前与推送前核对实际候选、规范同步、独立审查及适用检查，见 [分支与交付](spec/guides/branch-workflow-governance.md)。
- PR 交付包含持续监控 required CI，失败在同一分支修复。合并、发布、部署分别受用户授权约束；
  有合并授权也必须等 required CI 通过。适用时跟进 main CI、Release Please、镜像和签名发行资产。
  文档/流程变更不要求发布；绿色 release PR 本身不是发布授权。
- 本地测试、CI、签名发行物、真实安装及生产验收是不同证据层。不得用前者宣称后者通过。
  不擅自改变生产；遵循 [部署文档](docs/deploy/local-and-systemd.md) 和相关 [运维文档](docs/README.md)。

## 按范围阅读与验证

| 范围 | 权威与入口 |
| --- | --- |
| Go center、agent、PostgreSQL | [后端规范](spec/backend/index.md)，根目录 `make verify-go` |
| React SPA | [Web 规范](spec/web/index.md)，根目录 `make verify-web` |
| 跨层、分支、协作 | [通用指南](spec/guides/index.md)，根目录 `make verify` |
| 产品、架构与 UI | [现行设计](docs/design/current/README.md)；历史版本目录仅为背景 |

后端 Go module 在仓库根，**没有 `backend/` 工作目录**。Makefile 和 `scripts/` 命令在根运行；
子目录可用 `make -C "$(git rev-parse --show-toplevel)" <target>`。工具链来自 `go.mod`、`.node-version`
和 `web/package.json`。前端保持暗色优先、中文为主，真实浏览器验收不能由 build 替代。

VPS 是业务状态主体，Subscription 是账单事实，MonitoringInstance 是运行观测；维护不等于健康。
保持 outbound thin-agent、固定命令白名单、token 保密、回填不追发通知和 Records 权限/证据边界。
部署、数据库及附件/authority 协调恢复要求以相关后端规范和当前部署文档为准。
