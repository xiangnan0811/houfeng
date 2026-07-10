# 质量 Ratchet 与浏览器门实施计划

## Files

- Modify: `web/package.json`, lockfile, `web/vitest.config.ts`, `.github/workflows/ci.yml`
- Create: `web/playwright.config.ts`, `web/e2e/fixtures/*`, core/accessibility/visual specs
- Update through Trellis: `.trellis/spec/web/*.md`

## Checklist

- [ ] 安装 `@playwright/test`、`@axe-core/playwright`、`@vitest/coverage-v8`，固定 Chromium 安装与 cache。
- [ ] 生成 coverage baseline；配置全局 ratchet 与五类高风险模块 branch 90% 门。
- [ ] 建立 mock API fixtures 和 core routes loading/empty/error/非空白测试。
- [ ] 建立 Modal/Tabs/menu 键盘与 axe serious/critical gate。
- [ ] 建立 Dashboard/Asset/Providers/Settings 1440/390 visual/text-clipping gate。
- [ ] 复用精确 CSP policy，阻断 console error、page error、unhandled rejection 和 violation。
- [ ] 增加 bundle/font/CSS AST budgets，超限必须显式更新并在 PR 解释。
- [ ] 按目录探测并偿还两个 TypeScript ratchet 与 type-aware lint。
- [ ] 使用 `trellis-update-spec` 修正 CSS owner、Modal、Dashboard、Node、CSP 和 browser contracts。
- [ ] 在 staging 完成并保存规定 smoke evidence；随后运行完整 Gate C。

## Verification

```bash
NODE_ENV=test npm --prefix web run lint
NODE_ENV=test npm --prefix web run test:coverage
NODE_ENV=production npm --prefix web run build
npm --prefix web run css:analyze
npm --prefix web run test:e2e
npm --prefix web audit --include=dev
git diff --check
```
