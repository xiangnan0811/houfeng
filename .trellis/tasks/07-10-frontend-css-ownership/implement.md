# CSS owner 化与减债实施计划

## Workflow State

- Task 8 已通过实现 PR、release/released-dist 验收与独立 archive PR，归档状态位于 `origin/main@27bacd46a97bac8775f32bc0b025c250c70c7043`。
- 用户已批准在 Task 8 完成后启动本 task；保持既定 inline 模式，不分派运行时子代理。
- 本 task 拥有 PostCSS direct dependency、CSS analyzer、owner map 与 budget；Task 10 只消费这些输出并接入 CI，不复制 analyzer 或 baseline。
- Task 10 才建立 `test:e2e`/Playwright。当前 task 的浏览器验收使用 production dist + mock API + 本机固定 Chromium/CDP，并明确记录为 local-only evidence。

## Files

- Create: `scripts/analyze-web-css.mjs`, `web/css-budget.json`
- Modify: `web/package.json`, lockfile, `web/src/index.css`, legacy partials
- Rewrite: `web/src/styles/indexCssContract.test.ts`
- Add: route visual/interaction baselines

## Checklist

- [x] 安装 direct `postcss`，先生成 fresh AST/build baseline 并与审查值解释差异。
- [x] 实现稳定 JSON analyzer 和 budget failure，覆盖 source/selector/context/raw+gzip。
- [x] 用 PostCSS AST 重写 index CSS contract，支持唯一最终定义或显式 allowlist。
- [x] 建立七类 owner map，不可归属规则标为删除候选。
- [x] 先删除 Task 3/8 已删除组件的 Dashboard/Asset 不可达规则，每批运行视觉 gate。
- [x] 按 owner 合并相同 intent 的重复 selector，保留 media/theme context。
- [x] 评估 Asset route-owned CSS pilot；跨路由 owner 无安全单路由收益，按设计回滚/不保留，见 `research/route-css-pilot.md`。
- [x] 按 owner 保持可审查边界，最终断言所有预算指标下降且九条 route 无回归。

## Verification

```bash
npm --prefix web run css:analyze
NODE_ENV=test npm --prefix web run test -- --run src/styles/indexCssContract.test.ts
NODE_ENV=production npm --prefix web run build
npm --prefix web audit --include=dev
git diff --check
```

浏览器门覆盖 Chromium `1440x1000`、`1024x768`、`390x900` 与九条核心路由，断言 console/page/network/CSP error 为零、document 无横向溢出、关键文字不裁切；对删除过样式的 owner 额外走其主 workflow。若仓库现有 Python helper 不可用，可使用本机 Chromium 原生 CDP 临时 harness，但不得把它提交为 Task 10 的持久浏览器基础设施。
