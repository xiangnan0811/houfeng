# 处理 GitHub Actions Node.js 20 runtime deprecation

## 背景

CI 已经恢复为绿色，但 GitHub Actions run `25552801601` 仍有 annotation：

```text
Node.js 20 actions are deprecated. The following actions are running on Node.js 20 and may not work as expected:
actions/checkout@v4, actions/setup-go@v5, actions/setup-node@v4.
Actions will be forced to run with Node.js 24 by default starting June 2nd, 2026.
Node.js 20 will be removed from the runner on September 16th, 2026.
```

当前 `.github/workflows/ci.yml` 使用：

- `actions/checkout@v4`
- `actions/setup-go@v5`
- `actions/setup-node@v4`

## 官方证据

通过 GitHub API 检查官方 action metadata：

- `actions/checkout` latest release: `v6.0.2`，`action.yml` at `v6` shows `runs.using: node24`
- `actions/setup-go` latest release: `v6.4.0`，`action.yml` at `v6` shows `runs.using: node24`
- `actions/setup-node` latest release: `v6.4.0`，`action.yml` at `v6` shows `runs.using: node24`

说明最小迁移路径是把 CI 里的官方 actions 升级到 Node 24 runtime 的主版本：

- `actions/checkout@v6`
- `actions/setup-go@v6`
- `actions/setup-node@v6`

## 目标

1. 更新 `.github/workflows/ci.yml`，消除 Node.js 20 action runtime deprecation annotation。
2. 保持现有 CI 行为不变：
   - Go job 仍运行 `make verify-go`
   - Web job 仍运行 `make verify-web`
   - Go 版本仍来自 `go.mod`
   - Web Node 版本仍为 `22`
   - npm cache 仍使用 `web/package-lock.json`
3. 本地完成静态检查和质量门；推送后监控 GitHub Actions run，确认 `go` / `web` jobs 通过，且 Node.js 20 runtime annotation 不再出现。

## 非目标

- 不改变项目 runtime Node 版本要求（`web/package.json` 仍是 `22.x`，CI 仍安装 Node 22 给项目使用）。
- 不处理 unrelated Dashboard P2 任务。
- 不引入第三方 actions 或额外 workflow。

## 验收标准

- `.github/workflows/ci.yml` 不再包含：
  - `actions/checkout@v4`
  - `actions/setup-go@v5`
  - `actions/setup-node@v4`
- `.github/workflows/ci.yml` 包含：
  - `actions/checkout@v6`
  - `actions/setup-go@v6`
  - `actions/setup-node@v6`
- `git diff --check` 通过。
- `make verify-go` 通过。
- `make verify-web` 通过。
- 推送后的 GitHub Actions run 成功，且不再出现 Node.js 20 actions deprecation annotation。
