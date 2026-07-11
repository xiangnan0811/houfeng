# CSS owner 化 fresh baseline

## 坐标

- 日期：2026-07-11（Asia/Shanghai）
- 分支：`codex/frontend-css-ownership`
- 基线：`origin/main@27bacd46a97bac8775f32bc0b025c250c70c7043`
- Node / npm：`22.23.1` / `10.9.8`
- PostCSS：direct devDependency `8.5.16`
- 测量入口：`scripts/analyze-web-css.mjs`；source 扫描 `web/src/**/*.css`，production 扫描 fresh `web/dist/**/*.css`，gzip 由 Node `gzipSync(..., {level: 9})` 对每个网络 CSS 产物独立计算后求和。

## 质量基线

fresh worktree 先运行：

```bash
env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify-web
```

结果：113 个 Vitest 文件、758 个测试全部通过；lint、strict TypeScript 和 Vite production build 通过；npm install audit 为 0 vulnerabilities。入口 CSS build 输出为 396.15 kB / 49.61 kB gzip（Vite 格式化值）。

## AST / production 初始值

| Metric | 规划快照 | fresh PostCSS baseline | 差异说明 |
| --- | ---: | ---: | --- |
| source files | 未记录 | 31 | 覆盖全部 `web/src/**/*.css`，包括 Login route CSS 与 import manifest |
| source bytes | 435,865 | 419,695 | Task 6–8 合并后的真实 source 已下降 16,170 bytes |
| rules | 3,044 | 2,930 | PostCSS `walkRules`，Task 6–8 后下降 114 |
| declarations | 11,892 | 11,566 | PostCSS `walkDecls`，下降 326 |
| repeated selector texts | 约 178 | 189 | 规划值是旧快照近似值；fresh 合同对全部 31 个文件按规范化完整 selector 文本聚合，并保留 at-rule context；这是口径校正，不是放宽后续预算 |
| literal-color declarations | 未记录 | 274 | 统计包含 hex 或显式 CSS color function 的 declaration；token owner 也进入 inventory |
| `!important` declarations | 未记录 | 15 | 使用 PostCSS declaration `important` flag |
| production CSS raw | 415,864（主 CSS） | 399,514（全部 2 个 CSS chunk） | 当前包含 entry 396,151 与 Login 3,363 bytes；比旧主 CSS 单项仍低 16,350 bytes |
| production CSS gzip | 约 52 kB | 50,095 | 两个网络 CSS chunk 独立 level-9 gzip 后求和 |

`web/css-budget.json` 首先精确保存这组 fresh 值，证明零清理时不会凭空宣称改善。owner 清理完成后用最终更低值覆盖 limits；此初始表保留为下降证据，默认 analyzer 不提供自动抬高预算的路径。

## 环境污染发现

首次 `npm install --save-dev postcss` 继承了调用者 `NODE_ENV=production`，只安装 production tree 并审计 8 个包。根因由 `env` 与 `npm config get omit` 证实为 dev omit，而不是 lockfile 损坏；随后使用 `env -u NODE_ENV ... npm install --include=dev` 恢复完整 289-package tree。所有后续 install/gate 均显式隔离 `NODE_ENV`。
