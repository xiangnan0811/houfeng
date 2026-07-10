# CSS owner 化与减债实施计划

## Files

- Create: `scripts/analyze-web-css.mjs`, `web/css-budget.json`
- Modify: `web/package.json`, lockfile, `web/src/index.css`, legacy partials
- Rewrite: `web/src/styles/indexCssContract.test.ts`
- Add: route visual/interaction baselines

## Checklist

- [ ] 安装 direct `postcss`，先生成 fresh AST/build baseline 并与审查值解释差异。
- [ ] 实现稳定 JSON analyzer 和 budget failure，覆盖 source/selector/context/raw+gzip。
- [ ] 用 PostCSS AST 重写 index CSS contract，支持唯一最终定义或显式 allowlist。
- [ ] 建立七类 owner map，不可归属规则标为删除候选。
- [ ] 先删除 Task 3/8 已删除组件的 Dashboard/Asset 不可达规则，每批运行视觉 gate。
- [ ] 按 owner 合并相同 intent 的重复 selector，保留 media/theme context。
- [ ] 执行 Asset route-owned CSS pilot，仅在三项成功标准同时满足时保留。
- [ ] 一个 owner 一个提交，最终断言所有预算指标下降且九条 route 无回归。

## Verification

```bash
npm --prefix web run css:analyze
NODE_ENV=test npm --prefix web run test -- --run src/styles/indexCssContract.test.ts
NODE_ENV=production npm --prefix web run build
npm --prefix web run test:e2e -- visual-contracts.spec.ts
git diff --check
```
