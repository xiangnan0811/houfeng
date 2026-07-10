# 前端严格 CSP 兼容

## Goal

保持严格同源安全策略且不引入 `unsafe-inline`，让 production build 的登录、主题、表单、图表和核心路由在真实 CSP 下零 violation。

## Confirmed Facts

- Center 当前 CSP 只有 `default-src 'self'` 等兜底指令。
- `index.html` 使用 Google Fonts 与 inline theme bootstrap；CSS 有 data SVG；生产 TSX 有 16 处 inline style。
- 所有已抽查核心路由均产生 CSP violation，现有 Go test 只断言 header 非空。

## Requirements

- 使用单一精确 CSP policy，Center 与前端浏览器测试读取同一来源。
- IBM Plex Sans 400/500/600/700、Mono 400/500/600 自托管 WOFF2 并附 OFL。
- 主题 bootstrap 与 select caret 改为同源静态资源；清除生产 source 中 inline style。
- source scan、Go exact-header test 与真实浏览器 violation gate 三层验证。

## Dependency And Scope

- 依赖 `frontend-quality-gate-strict` 合并。
- 可以修改 Center security header，但不放宽为远程字体、data image 或 inline script/style。

## Acceptance Criteria

- [ ] production HTML/CSS/TSX source 不含 remote font、inline script、data image 或 `style={{`。
- [ ] Center 返回精确 CSP，保留 frame/object/base/form 限制。
- [ ] login、Dashboard、select、图表和主题切换没有 console 或 `securitypolicyviolation`。
- [ ] 字体 license 被跟踪，字体总量进入后续 bundle budget。
