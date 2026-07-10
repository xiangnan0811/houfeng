# 严格 CSP 兼容设计

## Policy

`internal/center/http/csp-policy.txt` 是唯一权威文本：

```text
default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'
```

Go 通过 `go:embed` 和 `strings.TrimSpace` 读取；Vite preview/e2e 从同一仓库文件读取 response header，禁止复制第二份策略。

## Resource Migration

- 在 `web/public/fonts` 保存七个 WOFF2 与 OFL，CSS 用 `@font-face` 和 `font-display: swap`。
- `web/public/theme-bootstrap.js` 在 CSS 前同步执行，只接受已知 preset/mode allowlist。
- 三种主题的 select caret 指向同源 SVG。
- 动态比例用 `<progress>` 或 SVG attributes；静态 spacing/alignment 迁 CSS class；不使用 nonce 作为绕过。

## Rollback

theme、font、caret、inline-style 分类提交。字体异常时退回系统字体栈，绝不回退远程字体或放宽 CSP。
