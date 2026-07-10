# 严格 CSP 兼容实施计划

## Files

- Create: `internal/center/http/csp-policy.txt`, `web/public/theme-bootstrap.js`, fonts/license, select caret SVG
- Modify: `internal/center/http/middleware.go`, `middleware_test.go`, `web/index.html`, `web/vite.config.ts`, `tokens.css`
- Modify: 当前七个含 16 处 inline style 的生产文件
- Create: `web/src/security/cspContract.test.ts`, `web/e2e/csp.spec.ts`

## Checklist

- [ ] 先写 source contract，枚举并使 remote/inline/data/style violations 失败。
- [ ] 建立单一 policy 文件，并让 Go header exact test 和 preview server 读取它。
- [ ] 下载并核对指定 IBM Plex WOFF2/OFL；加入 `@font-face` 后移除 Google Fonts。
- [ ] 将 theme bootstrap 外置并增加 preset/mode allowlist 单测。
- [ ] 将 select caret 改为同源 SVG。
- [ ] 按 score/progress、chart/tooltip、Stepper、static spacing 分类清除 16 处 inline style。
- [ ] Playwright 捕获 console 与 `securitypolicyviolation`，覆盖 login、Dashboard、select、chart、theme。
- [ ] 运行 Go、source、build 和 e2e gates；按资源类别分 commit。

## Verification

```bash
go test ./internal/center/http/...
NODE_ENV=test npm --prefix web run test -- --run src/security/cspContract.test.ts
NODE_ENV=production npm --prefix web run build
npm --prefix web run test:e2e -- csp.spec.ts
git diff --check
```
