# CSS owner 化本地验收证据

## 最终指标

测量坐标：2026-07-11，Node 22.23.1，PostCSS 8.5.16，production dist fresh build。

| Metric | Fresh baseline | Final | Delta |
| --- | ---: | ---: | ---: |
| source files | 31 | 26 | -5 |
| source bytes | 419,695 | 311,063 | -108,632 |
| rules | 2,930 | 2,107 | -823 |
| declarations | 11,566 | 8,517 | -3,049 |
| repeated selector texts | 189 | 151 | -38 |
| literal-color declarations | 274 | 247 | -27 |
| `!important` declarations | 15 | 11 | -4 |
| production CSS raw | 399,514 | 293,270 | -106,244 |
| production CSS gzip | 50,095 | 38,119 | -11,976 |

`web/css-budget.json` 已精确降至 Final 列；analyzer 不提供自动抬高预算路径。

## 结构合同

- production CSS reachability 从首轮 607 个不可达 selector branch 降为 0；最后一轮 273 个 RED 分支由同一 TypeScript/PostCSS inventory 机械删除。
- `legacy-misc.css`、`misc.css` 与误名的 `legacy-dashboard.css` 已删除；`overlays.css`、`pagination.css` 注释空壳也已删除。
- `index.css` 以七个显式 owner section 固定 import 顺序；owner map 对 26 个 source CSS 文件唯一且穷尽。
- 同一 at-rule context 的重复 selector 只保留 `.login-page` allowlist；它分别位于入口 CSS 与既有 Login 懒加载 CSS，属于两个网络 bundle 的兼容定义。
- 正则 first-match 合同已替换为 PostCSS import graph、selector/context、declaration 唯一性合同，并包含 conflicting second match synthetic fixture。

## Events 局部滚动修复

`/events` 的宽表现在由 `.events-table-scroll` 独占横向滚动，分页保持在 region 外：

| Viewport | document | card client/scroll | region client/scroll | 语义与键盘 |
| --- | --- | --- | --- | --- |
| 1024×768 | 1024 / 1024 | 736 / 736 | 686 / 1024 | role=region，名称“事件流”，tabIndex=0，scrollLeft 0→120 |
| 390×900 | 390 / 390 | 308 / 308 | 268 / 1024 | role=region，名称“事件流”，tabIndex=0，scrollLeft 0→120 |

两档均显示“表格可横向滚动查看完整事件字段”提示，region 获得真实焦点，且不包含分页按钮。

## Chromium local-only gate

- 浏览器：Chromium 150.0.7871.114，原生 CDP；Python Playwright helper 在本机不可用，因此本证据明确为 local-only，Task 10 才固化 Playwright CI。
- 路由：`/`、`/asset-decisions`、`/vps`、`/providers`、`/subscriptions`、`/settings`、`/monitoring`、`/targets`、`/events`。
- 视口：1440×1000、1024×768、390×900，共 27 个组合。
- 最终 owner 重排与去重后复跑：27/27 通过；document overflow、关键文字裁切、offscreen command、console exception/error、network failure、HTTP >=400 与 CSP violation 均为 0。

## Focused verification

- `cssReachabilityContract.test.ts`、`indexCssContract.test.ts`、`cssAnalyzerContract.test.ts`：3 files / 16 tests 通过。
- `NODE_ENV=production npm --prefix web run build`：通过，无 large chunk warning。
- `npm --prefix web run css:analyze`：预算通过。

## 完整质量门

- `NODE_ENV=production make verify-web`：重新安装 286 packages 后 lint 通过；115 个 Vitest files / 769 tests 全部通过；strict TypeScript 与 Vite production build 通过，0 large chunk warning。
- `npm --prefix web audit --include=dev`：0 vulnerabilities。
- clean-CI 回归：source-only same-context contract 使用测试创建的空 `--dist`，不依赖测试阶段之前不存在的 `web/dist`；focused 4/4 通过。
- `npm --prefix web run css:analyze`：最终 budget pass，数值与本文件 Final 列一致。
- `git diff --check` 与 `task.py validate 07-10-frontend-css-ownership`：通过。

## PR、主干与发布证据

- 功能 PR [#365](https://github.com/xiangnan0811/houfeng/pull/365) 在 head `9856a94bd058bb127e26cef72e5284632839a5d9` 的 Go、Web、Docker image 与 GitGuardian 检查全部通过后合并；merge commit 为 `2a26a55e7921b39775e1d4dd1baec319ee609dc9`。
- 合并后的 `main` CI run `29154186205` 在同一 merge commit 上通过 Go、Web 与 Docker image；Release Please run `29154186212` 成功创建 release PR。
- release PR [#366](https://github.com/xiangnan0811/houfeng/pull/366) 的四项检查全部通过后合并为 `e1910d50b297320d4e7ae940b153b4a906286c28`；其 post-merge `main` CI run `29154283793` 全绿。
- GitHub Release [v0.58.4](https://github.com/xiangnan0811/houfeng/releases/tag/v0.58.4) 指向 `e1910d50b297320d4e7ae940b153b4a906286c28`；publish-images run `29154288713` 完成 agent 资产、minisign checksum、linux/amd64、linux/arm64 与多架构 manifest 发布。
- Docker Hub `docker.io/linnea7171/houfeng:v0.58.4` 远端 OCI index digest 为 `sha256:33a80e264f1b7e6fd21fe5584ab5a8eadb5f24b6a13d0fa3aee27716a27d2203`；linux/amd64 digest 为 `sha256:9bcd429b4ec981298f27262f673563bf5db862427851218823f2ad89fc0e952f`，linux/arm64 digest 为 `sha256:71530154138ccf280002a9e5ec940a507e2b494152594729ec79244d46e3516d`。
