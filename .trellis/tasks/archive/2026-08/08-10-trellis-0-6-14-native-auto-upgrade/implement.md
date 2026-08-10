# Trellis 0.6.14 native/auto Upgrade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 安全升级 Trellis 到稳定版 0.6.14，并启用 native workflow 下的 Codex auto 子智能体分派，同时保留 channel 按需能力和 worktree-safe hooks 定制。

**Architecture:** 先固定 CLI 版本，再以 `--create-new` 生成可审查的模板升级；假阳性冲突采用新版模板，真实 hook 定制做结构化合并。最后从 CLI、模板、配置、hook、任务和 Git 六个层面验证。

**Tech Stack:** npm、Trellis CLI、YAML/JSON/TOML、Python hooks、Git。

---

## 1. Prepare The Maintenance Task

- [x] 将干净功能分支改名为 `codex/trellis-0.6.14-upgrade`。
- [x] 创建独立任务 `08-10-trellis-0-6-14-native-auto-upgrade`，不挂接 VPS 父任务。
- [x] 运行 `python3 ./.trellis/scripts/task.py validate 08-10-trellis-0-6-14-native-auto-upgrade`，确认规划文件有效。
- [x] 运行 `python3 ./.trellis/scripts/task.py start 08-10-trellis-0-6-14-native-auto-upgrade`，将状态切换为 `in_progress`。

## 2. Upgrade The CLI And Managed Templates

- [x] 运行 `npm install -g @mindfoldhq/trellis@0.6.14`。
- [x] 运行 `trellis --version`，预期输出 `0.6.14`。
- [x] 查看 `trellis update --help`，确认 `--create-new` 语义后运行 `trellis update --create-new`；禁止 `--force`。
- [x] 审查所有 `.new` sidecar。对已验证的旧 hash 假阳性采用 `0.6.14` 模板，不覆盖用户数据。

## 3. Merge Project And Codex Configuration

- [x] 在 `.trellis/config.yaml` 中显式写入 `codex.dispatch_mode: auto`，保留 channel guard `idle_timeout: 5m`、`max_live_workers: 6`。
- [x] 保持 bundled `native` workflow，不运行 channel-driven workflow 切换。
- [x] 合并 `.codex/hooks.json`：注册 `UserPromptSubmit` 和限定 matcher 的 `SubagentStart`，两个命令都保留 `git-common-dir` 定位方式。
- [x] 确认 `.codex/hooks/inject-subagent-context.py` 与 Codex 三个 Trellis agent 定义存在。
- [x] 删除或处理完所有 `.new` sidecar。

## 4. Verify The Upgrade

- [x] 检查 `trellis --version` 和 `.trellis/.version` 均为 `0.6.14`。
- [x] 运行 `trellis platforms` 与 `trellis update --dry-run`。
- [x] 运行 `python3 -m compileall -q .trellis/scripts .codex/hooks`。
- [x] 模拟 `UserPromptSubmit` 和三个 `SubagentStart` hook，确认 JSON 输出与上下文注入成功。
- [x] 运行任务 `list`、`current --source`、`validate`，确认维护任务为当前 in-progress 任务且 VPS 任务树未改变。
- [x] 搜索仓库确认无遗留 `.new`；运行 `git diff --check`。
- [x] 分别审查 tracked diff、忽略的 `.codex` 文件和全局 CLI 状态。
- [x] 停在未提交、未 push、无 PR 状态，等待用户确认。

## 5. Spec Review

- [x] 无需修改 `.trellis/spec/`：本任务没有改变 Houfeng 的 backend/web 可执行契约。Trellis 通用行为已由 0.6.14 managed `trellis-meta` 文档覆盖；项目特定的 worktree hook 决策已记录在本任务 `research/upgrade-baseline.md`。
