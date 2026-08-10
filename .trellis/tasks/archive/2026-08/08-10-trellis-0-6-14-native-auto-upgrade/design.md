# Design: Trellis 0.6.14 native/auto 升级

## Boundaries

本次改动只覆盖本机安装的 Trellis CLI、仓库内 Trellis 管理模板、Codex 平台集成和该维护任务本身。VPS 重构任务树是只读边界，不作为此任务的父任务，也不会被启动。

## Upgrade Strategy

1. 在干净的非 main 分支上执行，保留当前仓库和用户数据。
2. 安装固定版本 `@mindfoldhq/trellis@0.6.14`，避免 beta 行为进入项目。
3. 使用 `trellis update --create-new` 让 Trellis 自动更新未修改模板，并把冲突写成 `.new` sidecar。
4. 对已确认属于旧 hash manifest 假阳性的模板采用 `0.6.14` 版本；对真实定制 `.codex/hooks.json` 手工合并。
5. 显式设置 `codex.dispatch_mode: auto`。native workflow 负责阶段和人工门控；Codex 原生子智能体只处理有界 research/implement/check 工作。
6. channel runtime 与 bundled role cards 保留，但不会成为默认 workflow。需要多轮跨 provider 协作、持久审查或可观察 peer worker 时才按需使用。

## Codex Hook Contract

`UserPromptSubmit` 调用 `inject-workflow-state.py`；`SubagentStart` 只匹配 `trellis-implement|trellis-check|trellis-research` 并调用 `inject-subagent-context.py`。两个命令都从 `git rev-parse --path-format=absolute --git-common-dir` 的父目录定位 `.codex/hooks`，从任何已登记 worktree 启动时都解析到 root checkout 的忽略型 Codex 配置。

## Compatibility And Safety

- `.trellis/tasks`、`.trellis/spec`、`.trellis/workspace`、`.trellis/.developer` 是用户数据，不用模板覆盖。
- `.codex/` 被项目忽略，因此既要单独验证，也不能仅依赖 tracked Git diff 判断升级是否完整。
- 不使用 `--force`；每个 sidecar 都必须被明确采用或合并。
- 出现安装失败、模板差异无法解释或 hook 模拟失败时停止，不通过重置/清理绕过。

## Rollback

在未提交阶段，回滚仅针对本任务已识别的文件；不得使用 `git reset --hard`、`git clean` 或影响用户数据的广域命令。若全局 CLI 有问题，可重新安装已知版本，但项目文件仍以逐文件审查结果为准。
