# 前端可重复质量门与 TypeScript strict

## Goal

消除前端验证命令对调用者环境和本机 Node 版本的隐式依赖，使本地与 CI 对同一提交给出一致结果，并在零迁移成本下启用 TypeScript strict。

## Confirmed Facts

- 父任务基线为 74 个测试文件、578 个测试通过，lint、production build 与依赖审计通过。
- `make verify-web` 当前继承外部 `NODE_ENV`；`NODE_ENV=production` 会省略 devDependencies 或让 Vitest加载 production React。
- 项目声明 Node 22.x，当前审查环境为 Node 24.18.0；`tsc --strict` 探针已经通过。

## Requirements

- `verify-web` 的 install、lint/test、build 必须分别显式使用无 `NODE_ENV`、test、production 环境。
- 增加 Node 22 preflight 与 `.node-version=22.23.1`，并将 `@types/node` 对齐 22.x。
- 在 app 与 node tsconfig 中显式启用 `strict`；本任务不启用 `noUncheckedIndexedAccess` 或 `exactOptionalPropertyTypes`。
- CI 必须在外�� `NODE_ENV=production` 污染下运行完整 `make verify-web` 回归。

## Dependency And Scope

- 无前置 child task，是所有后续修复的共同入口门。
- 只修改工具链、配置与 CI，不修改运行时业务语义。

## Acceptance Criteria

- [ ] Node 非 22 时 preflight 在安装前失败，并输出实际版本与修复指引。
- [ ] `NODE_ENV=production make verify-web` 与 `env -u NODE_ENV make verify-web` 均通过。
- [ ] 两个 tsconfig 均启用 strict，lint/test/build 仍通过且测试不少于 74 files / 578 tests。
- [ ] lockfile、CI recipe 与文档化 Node pin 一致。
