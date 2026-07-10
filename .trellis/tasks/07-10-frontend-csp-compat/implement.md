# 严格 CSP 兼容实施计划

## Files

- Create: `internal/center/http/csp-policy.txt`, `web/public/theme-bootstrap.js`, fonts/license, select caret SVG
- Modify: `internal/center/http/middleware.go`, `middleware_test.go`, `web/index.html`, `web/vite.config.ts`, `tokens.css`
- Modify: 当前 9 个含 21 处 inline style 的生产文件
- Create: `web/src/security/cspContract.test.ts`; Task 5 不创建 `web/e2e/` 或新增 Playwright 依赖

## Checklist

- [x] 先写 source contract，枚举并使 remote/inline/data/style violations 失败。
- [x] 建立单一 policy 文件，并让 Go header exact test 和 preview server 读取它。
- [x] 下载并核对指定 IBM Plex WOFF2/OFL；加入 `@font-face` 后移除 Google Fonts。
- [x] 将 theme bootstrap 外置并增加 preset/mode allowlist 单测。
- [x] 将 select caret 改为同源 SVG。
- [x] 按 score/progress、chart/tooltip、Stepper、static spacing 分类清除 21 处 inline style。
- [x] 本地 Chromium/CDP 捕获 console 与 `securitypolicyviolation`，覆盖 login、Dashboard、select、chart、theme；把可重复场景与证据交给 Task 10 固化为 Playwright CI gate。
- [x] 运行 Go、source、build 和浏览器 gates。
- [x] 按资源类别分 commit。

## Execution Evidence

- Source contract: `web/src/security/cspContract.test.ts` 9/9 通过；最后发现并以 RED→GREEN 修复 `legacy-subscriptions.css` 独立渐变 caret。
- Font provenance: `@fontsource/ibm-plex-sans@5.2.8` tarball SHA256 `30b10b936b72f3a07f15f3efdd9dc4ee18bbbd52bb61342543b20bee56534796d`；`@fontsource/ibm-plex-mono@5.2.7` tarball SHA256 `ec4d21b63e7df7fe6c8d4c841630cefcd729b5f85f5e6a3e91718a0cb5f78a6d`。
- Browser gate: Chrome `150.0.7871.114`，11 routes × 3 viewports（`1440x1000`、`1024x768`、`390x900`）= 33/33 PASS。
- Browser assertions: CSP violation、console/runtime error、非预期 network error、DOM `style`、document/body 横向溢出均为 0；七个字体、深浅主题 caret、主题切换、subscription progress、MetricChart hover tooltip 均通过。
- Final repository gates: Node `22.23.1` 下 `NODE_ENV=production make verify-web` 与 `make verify` 均通过（86 test files、633 tests、lint、strict TS build、全部 Go fmt/vet/test）；`npm audit --include=dev` 为 0 vulnerabilities。
- Dev-server boundary: Vite dev response 返回同一精确 CSP，转换后的 HTML 仅含同源外部 `/@vite/client`、`/theme-bootstrap.js` 与 `/src/main.tsx`，没有 Fast Refresh inline preamble。
- Work commits: `77f9d51`（inline-style migration）与 `254959a`（strict policy/resources/source contract）。
- Docker boundary regression: PR #352 首次 CI 揭示 `web-build` stage 未复制仓库级 policy；新增 source contract 先 RED，再最小增加原文件 COPY 后 GREEN。本地完整 `docker build --network=host --build-arg VERSION=dev -t houfeng:csp-compat-test .` 成功；host network 仅用于绕过本机 legacy builder 的 bridge veth 限制。
- Local-only screenshots（不提交）：`/tmp/houfeng-csp-subscriptions-1440x1000.png`、`/tmp/houfeng-csp-settings-1440x1000.png`、`/tmp/houfeng-csp-dashboard-390x900.png`、`/tmp/houfeng-csp-metric-chart-hover-1440x1000.png`。
- Scope note: Providers 页既有 390px 链接裁切仍归 `frontend-responsive-workflows`，本任务不越界修复。

## Verification

```bash
go test ./internal/center/http/...
NODE_ENV=test npm --prefix web run test -- --run src/security/cspContract.test.ts
NODE_ENV=production npm --prefix web run build
# local-only Chromium/CDP CSP gate（记录版本、路由、console 与 violation 结果）
git diff --check
```
