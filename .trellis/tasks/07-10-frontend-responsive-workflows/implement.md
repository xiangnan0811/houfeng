# 窄视口核心流程实施计划

## Files

- Modify: `web/src/styles/partials/atoms.css`, Asset/Provider/Dashboard owner CSS
- Modify: `AssetDecisionSecondaryNav.tsx`, `ProvidersPage.tsx`
- Add: responsive Playwright specs and stable screenshots

## Checklist

- [ ] 先在 390x900 写三个关键文本完整、in-viewport 和 document width 失败断言。
- [ ] 实现通用 tabs 横向滚动策略并删除逐字折行规则。
- [ ] 收敛 Asset secondary nav breakpoint，保证标题完整与触控高度。
- [ ] 移除 Provider entry link 裁切，建立局部可访问 table scroll region。
- [ ] 验证 Task 3 Dashboard 主行动首屏，以及九条核心 route 的末尾命令不被遮挡。
- [ ] 在 1024x768 与 1440x1000 复查，避免移动修复破坏桌面。
- [ ] 按 Settings/Asset/Provider owner 分 commit，PR 标题 `fix(web): close narrow viewport workflow gaps`。

## Verification

```bash
NODE_ENV=test npm --prefix web run test -- --run
NODE_ENV=production npm --prefix web run build
npm --prefix web run test:e2e -- responsive-workflows.spec.ts
git diff --check
```
