# 修复 CI web 测试时区依赖

## 背景

修复 `.github/workflows/ci.yml` 后，GitHub Actions 已经能正常创建 jobs：

- run `25551777831`
- `go` job 通过
- `web` job 正常创建并运行 `make verify-web`

现在暴露出第二个真实 CI 问题：`web` 测试在 GitHub runner 的 UTC 时区下失败，但本地 Asia/Shanghai 时区下通过。

## 远端失败证据

`web` job 失败位置：

- `web/src/pages/NodeOnboardingPage.test.tsx:362`
- `web/src/pages/NodeOnboardingPage.test.tsx:751`
- `web/src/pages/DashboardPage.test.tsx:168`

失败形态：

- 测试期望 `2026/04/26 17:15` / `2026/04/25 16:30`
- CI 实际渲染 `2026/04/26 09:15` / `2026/04/25 08:30`

这些值对应同一个 `Z` 时间戳在 Asia/Shanghai 与 UTC 下的显示差异。

## 目标

1. 修复前端测试对本地时区的隐含依赖，使 `make verify-web` 在 UTC 与本地时区下都稳定。
2. 不改变用户可见时间格式，除非发现现有产品代码本身有明确 bug。
3. 优先让测试期望复用 `formatDateTime(...)` 这类产品格式函数，或在测试环境中显式固定时区；不要继续硬编码只适用于当前机器时区的时间字符串。
4. 复跑失败测试和完整 `make verify-web`。

## 非目标

- 不重构 Dashboard / NodeOnboarding 页面。
- 不处理 GitHub Actions Node 20 runtime deprecation annotation。
- 不调整 `.github/workflows/ci.yml`，除非发现必须显式设置 `TZ` 才是更合适的产品测试契约。

## 验收标准

- `TZ=UTC npm run test -- --run DashboardPage NodeOnboardingPage` 或等效命令通过。
- `make verify-web` 通过。
- 如修改测试，测试仍断言时间内容存在并保持 `Timestamp` class 等关键 DOM 语义。
- `git diff --check` 通过。
