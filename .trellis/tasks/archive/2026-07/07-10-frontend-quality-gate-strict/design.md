# 可重复前端质量门设计

## Boundary

该任务把环境规范收敛在 Make recipe 和一个只读 preflight 脚本中。package scripts 保持可单独调用，但 CI 的权威入口仍是 `make verify-web`。

## Contracts

- `scripts/check-web-toolchain.sh` 读取 `process.versions.node`，只接受 major 22；不得自动安装或切换 runtime。
- install 使用 `env -u NODE_ENV npm --prefix web ci --include=dev`。
- lint/test 使用 `NODE_ENV=test`；build 使用 `NODE_ENV=production`。
- `.node-version` 固定 `22.23.1`，`package.json.engines.node` 保持 22.x 范围，`@types/node` 使用 `^22`。
- TypeScript strict 同时写入 `tsconfig.app.json` 与 `tsconfig.node.json`，避免继承关系变化后退化。

## Compatibility And Rollback

不改变浏览器产物或 API。所有变更可作为单一工具链 commit 回滚；CI 污染环境测试必须与 recipe 修复一起存在，避免只修本机路径。
