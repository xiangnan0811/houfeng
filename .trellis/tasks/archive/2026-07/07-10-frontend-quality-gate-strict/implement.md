# 可重复前端质量门实施计划

## Files

- Modify: `Makefile`, `.github/workflows/ci.yml`, `web/package.json`, `web/package-lock.json`
- Modify: `web/tsconfig.app.json`, `web/tsconfig.node.json`
- Create: `.node-version`, `scripts/check-web-toolchain.sh`

## Checklist

- [x] 在 CI 增加 `NODE_ENV=production make verify-web`，先证明当前 recipe 失败或不稳定。
- [x] 为 toolchain preflight 增加 shell 自检：Node major 22 通过，其他 major 返回非零并包含实际版本。
- [x] 实现只接受 Node 22 的 preflight，并在 `verify-web` 第一行调用。
- [x] 将 install 改为清除 `NODE_ENV` 且显式包含 devDependencies；为 lint/test/build 固定各自环境。
- [x] 写入 `.node-version`，将 `@types/node` 更新到 `^22` 并更新 lockfile。
- [x] 在两个 tsconfig 中显式加入 `strict: true`。
- [x] 使用 Node 22.23.1 分别运行污染环境与干净环境的 `make verify-web`，再运行 audit 与 `git diff --check`。
- [x] 以 `build(web): isolate verification environment` 提交；不夹带运行时代码。

## Verification

```bash
NODE_ENV=production make verify-web
env -u NODE_ENV make verify-web
npm --prefix web audit --include=dev
git diff --check
```

Expected: 两次完整验证均通过，测试数量不低于 74 files / 578 tests，production build 成功。
