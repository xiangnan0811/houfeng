# 修复 GitHub Actions CI workflow 创建失败

## 背景

用户反馈每次推送到远程仓库时，GitHub Actions 都会失败：

- 仓库：`xiangnan0811/houfeng`
- Actions 页面：`https://github.com/xiangnan0811/houfeng/actions`
- 当前 workflow：`.github/workflows/ci.yml`

本地已确认的远程失败证据：

- 最新 push run `25548987720`，commit `875da2b63df34e59e9fc150b7a7ebf1a390d38a5`
- run 状态：`completed` / `failure`
- `jobs` 为空，`log not found`
- GitHub Actions check suite `status=completed` / `conclusion=failure` / `latest_check_runs_count=0`

这说明失败发生在 job 创建前，更像 workflow YAML / 表达式校验问题，而不是 Go 或 Web 测试失败。

## 当前可疑点

`.github/workflows/ci.yml` 当前在 `web` job 上使用：

```yaml
if: ${{ hashFiles('web/package.json') != '' }}
```

`hashFiles` 通常用于 step 级表达式或 cache key；放在 job-level `if` 里会导致 workflow 无法创建 job。

## 目标

1. 修复 `.github/workflows/ci.yml`，使 push / pull_request 能正常创建 CI jobs。
2. 保留现有验证语义：
   - Go job 运行 `make verify-go`
   - Web job 在仓库有 `web/package.json` 时运行 `make verify-web`
3. 保持 workflow 简洁，避免把验证逻辑重复写进 Actions YAML。
4. 本地运行可行的验证命令，至少确认 workflow YAML 已去掉 job-level unsupported expression，并确认现有 Go/Web 质量门仍可运行或清楚记录失败原因。

## 非目标

- 不修复与本次 workflow 创建失败无关的业务代码问题。
- 不调整 Dashboard P0/P1/P2 功能。
- 不改 GitHub 仓库设置、分支保护或第三方集成。

## 建议实现方向

由于当前仓库已经有 `web/package.json`，最小修复可以移除 `web` job 的 job-level `if`。`make verify-web` 本身也已经在没有 `web/package.json` 时输出跳过信息，因此 YAML 不需要再次判断。

如需保留可选 web workspace 语义，可改成先运行 shell 判断的 step，而不是 job-level `hashFiles`。

## 验收标准

- `.github/workflows/ci.yml` 不再使用 job-level `hashFiles(...)`。
- `rg -n "hashFiles" .github/workflows/ci.yml` 无结果，或仅用于 GitHub Actions 支持的上下文位置。
- `make verify-go` 通过。
- `make verify-web` 通过；如果本地环境问题导致失败，需要记录确切原因并说明是否与 CI YAML 修复无关。
- `git diff --check` 通过。
