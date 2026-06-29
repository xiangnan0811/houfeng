# Fix Debian bullseye minisign agent upgrade path

## Goal

把真实 Debian 11 bullseye 主机上 agent 升级失败的路径固化为可诊断、可执行的运维说明，并验证当前代码不会再次生成缺少 `--install-missing-deps` 的一键安装/升级命令。

本任务不重新设计安装器签名校验。当前 `main` / `v0.55.1+` 已经实现缺失 `minisign` 时的固定版本 bootstrap；这次要处理的是 `v0.55.0` center 已经发布、真实用户可能复制到旧命令并在 Debian 11 上失败的升级路径。

## Confirmed Facts

- 真实目标主机：
  - Debian 11 bullseye，`x86_64`，Linux kernel `5.10.0-8-amd64`。
  - `apt install minisign` 和 `apt-get install minisign` 都返回 `E: Unable to locate package minisign`。
- 失败命令的关键形状：
  - `sudo sh "$tmp_installer" --server-url 'https://fleet.yading.de' --enrollment-token-stdin --version 'v0.55.0' --release-repo 'xiangnan0811/houfeng'`
  - 命令里没有 `--install-missing-deps`。
- `v0.55.0` 的命令生成器没有注入 `--install-missing-deps`。
- `v0.55.0` 的 installer 在缺少 `minisign` 时直接失败，无法自行恢复。
- `v0.55.1` 起的 installer 支持：
  - `--install-missing-deps` / `--no-install-missing-deps`；
  - 下载固定 upstream `minisign` 0.12 tarball；
  - 验证 installer 内置的 tarball SHA256；
  - 安装 `/usr/local/bin/minisign` 后继续验证 Houfeng release 的 signed checksum manifest。
- 当前最新已发布版本为 `v0.55.3`，GitHub Release 包含 Linux agent binaries、`sha256sums.txt` 和 `sha256sums.txt.minisig`。

## Root Cause

这是一个旧发布版本的安装命令/安装器能力缺口，不是 Debian `apt` 操作错误。

`v0.55.0` 已经把 agent release 校验切换到必须使用 `minisign` 验证 signed checksum manifest，但还没有实现缺失 `minisign` 时的 bootstrap。Debian 11 bullseye 默认仓库又没有可直接安装的 `minisign` 包，所以旧命令必然卡在：

```text
houfeng-agent install: minisign is required to verify release checksums
```

当前代码无法改变已经复制出去或由 `v0.55.0` center 生成的旧命令。正确修复路径是升级 center 到 `v0.55.1+`，最好使用当前最新 patch release，重启 center 后重新生成 MonitoringInstance install/upgrade command，并确认新命令包含 `--install-missing-deps`。

## Requirements

- 任务文档必须记录真实失败、影响版本、已修复版本和根因，不保存真实 enrollment token。
- 运维文档必须给出 `v0.55.0` / Debian 11 上遇到 `minisign is required to verify release checksums` 时的具体处理步骤。
- 文档必须明确：
  - 不要把 `apt install minisign` 当作通用修复；
  - 不要关闭 signed checksum verification；
  - 不要手工编辑 `sha256sums.txt` 或使用未签名/未校验二进制；
  - 旧命令必须丢弃，升级 center 后重新生成命令。
- 文档必须给出验证 fixed center 的方式：
  - center 已经升级到 `v0.55.1+` / 最新 patch release；
  - `/api/agent/install.sh` 的 usage 或生成命令中出现 `--install-missing-deps`；
  - 生成命令的 `--version` 指向 fixed release；
  - GitHub Release 存在 matching agent assets 和 signed checksum manifest。
- 当前代码测试必须证明 install-command generation 仍包含 `--install-missing-deps`，installer missing-minisign recovery 仍保留。
- 不做后端 API、数据库、签名信任根或 agent token 格式修改。

## Acceptance Criteria

- [x] PRD 记录真实失败、根因、影响版本、已修复版本和推荐操作，不包含真实 enrollment token。
- [x] `docs/deploy/local-and-systemd.md` 包含旧 `v0.55.0` 命令在 Debian 11 上失败后的可执行 runbook。
- [x] `docs/operations/fresh-install-smoke-run.md` 包含 smoke-run 失败时的诊断步骤，并要求重新生成包含 `--install-missing-deps` 的命令。
- [x] 文档明确禁止 checksum-only / unsigned fallback。
- [x] 聚焦测试覆盖当前 install-command generation 和 installer missing-minisign recovery。
- [x] `make verify-go` 通过。

## Notes

- 相关已归档任务：`.trellis/tasks/archive/2026-06/06-29-installer-minisign-recovery`、`.trellis/tasks/archive/2026-06/06-29-installer-minisign-dependency`。
- 这次是轻量文档/验证任务。只有发现当前代码回归时才进入代码修复。
