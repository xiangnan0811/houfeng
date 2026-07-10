# 质量 Ratchet 与浏览器门设计

## Test Pyramid

纯 model/contract 使用 Vitest；组件交互使用 Testing Library；少量核心 workflow 使用 Chromium Playwright。axe 只阻断 serious/critical，键盘行为由显式操作断言，不用静态扫描代替。

## Coverage And Budgets

首次生成 statements/branches/functions/lines baseline。全局不得下降；Modal stack、Dashboard model、API request helpers、auth、Asset command hooks branch 至少 90%。bundle budget记录入口 JS/CSS gzip、最大 lazy chunk、字体总量；CSS budget复用 Task 9。

## Type Ratchet

分别探测 `noUncheckedIndexedAccess` 和 `exactOptionalPropertyTypes`，按 lib、atoms、dashboard、routes 目录清零后扩大配置。type-aware ESLint 从 lib 与新 hooks 开始；不得用全局 disable 或不安全 cast 消除报告。

## Staging Evidence

使用真实认证 Center/PostgreSQL，记录浏览器版本、commit、响应头、console/network 和截图。缺少环境/凭据时任务保持未完成，不把 mock 结果声明为生产通过。

## Rollback

coverage、Playwright、bundle、type ratchet、spec 分提交。CI 时间问题优先通过 cache/shard 解决，不删除能捕获 P1 的 gate。
