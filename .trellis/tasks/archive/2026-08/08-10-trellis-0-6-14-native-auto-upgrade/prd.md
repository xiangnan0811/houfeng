# Trellis 0.6.14 native/auto 升级

## Goal

升级本机与项目 Trellis 至稳定版 0.6.14，采用 native workflow 和 Codex auto 子智能体分派，保留 channel 按需能力与现有 hooks 定制。

## Requirements

- 将本机全局 Trellis CLI 固定升级到稳定版 `0.6.14`，不采用 `0.7.x` beta。
- 将本项目的 Trellis 管理文件从 `0.6.4` 升级到 `0.6.14`，保留 `.trellis/tasks/`、`.trellis/spec/`、`.trellis/workspace/` 与开发者数据。
- 使用 bundled `native` workflow；不得切换到 `channel-driven-subagent-dispatch`。
- 在 `.trellis/config.yaml` 显式配置 `codex.dispatch_mode: auto`，让 Codex 在支持时使用原生 `trellis-research`、`trellis-implement`、`trellis-check` 子智能体，并保留安全的 inline fallback。
- 保留 `trellis channel` 作为按需能力，以及 `idle_timeout: 5m`、`max_live_workers: 6` 的 worker guard；不得把 channel 设为默认总控路径。
- 保留 `.codex/hooks.json` 的 worktree 安全定制：hook 命令必须通过 `git rev-parse --git-common-dir` 定位 root checkout 下的 `.codex/hooks/`。
- 新版 Codex hooks 同时注册 `UserPromptSubmit` 工作流状态注入与匹配 Trellis 三个原生子智能体的 `SubagentStart` 上下文注入。
- 升级使用非破坏性的冲突处理方式；禁止 `trellis update --force`，不得静默覆盖真实本地定制。
- 本维护任务独立于 VPS 重构父/子任务；不得自动开始 VPS Child 4。
- 本轮不得 commit、push 或创建 PR，完成后停在已验证的未提交状态等待用户确认。

## Acceptance Criteria

- [x] `trellis --version` 与 `.trellis/.version` 均为 `0.6.14`。
- [x] 当前 workflow 为 bundled `native`，且 `.trellis/config.yaml` 显式包含 `codex.dispatch_mode: auto`。
- [x] Codex 原生 research/implement/check agent 定义存在，`SubagentStart` hook 能为三者注入活动任务上下文。
- [x] `.codex/hooks.json` 中两个 hook 命令均保留 `git-common-dir` worktree 定位逻辑。
- [x] channel runtime 仍可用，但仅为按需能力；worker guard 保持 `5m` / `6`。
- [x] 更新后没有未处理的 `.new` sidecar，且 `trellis update --dry-run` 不暴露未处置的模板升级项。
- [x] Python hooks/scripts 可编译，任务可 validate/current，Git whitespace 检查通过。
- [x] VPS 任务树和已有项目数据未被改写；当前分支无 commit、push、PR。

## Out of Scope

- 不修改 Trellis 上游源码或全局 npm 包内容。
- 不改变 VPS 详情页重构的需求、拆分、状态或代码。
- 不启用自动连续执行多个 VPS child 的总控循环。
- 不清理或改写 Trellis memory/channel 的用户级历史数据。
