# Asset Decisions 领域拆分实施计划

## Files

- Modify: `web/src/pages/AssetDecisionsPage.tsx`; delete `AssetDecisionsPageContent.tsx`
- Create: `web/src/pages/asset-decisions/hooks/useAssetDecision{RouteState,Portfolio,Groups,ManualGroups,Templates,Records,RenewalQueue}.ts`
- Split: `AssetDecisionsPage.test.tsx` into route, hook, modal and business-logic ownership
- Create/modify: TypeScript AST structural contract test

## Checklist

- [ ] 冻结 initial request set、allSettled partial error、URL open/deep link 与七类 mutation contract。
- [ ] 提取唯一 route state hook，并验证 typed selection/commands 与 history 语义。
- [ ] 按计划顺序逐域提取 read state/effect，每次 focused tests 通过后提交。
- [ ] 将 mutation、saving/error 与最小 refresh 收进对应 command，使用 typed invalidation 协调跨域。
- [ ] 收缩 route page，删除 `AssetDecisionsPageContent.tsx` 和原始 setter 穿透。
- [ ] 拆分 route composition、hooks、modals、pure business logic 测试所有权。
- [ ] 用 AST 断言无总控替身、route 不 import API、展示层不 import controller、目录阈值覆盖 glob。
- [ ] 运行完整 web gate、Asset route 桌面/移动 workflow 和嵌套确认键盘流程。

## Verification

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/AssetDecisionsPage.test.tsx src/pages/asset-decisions
NODE_ENV=test npm --prefix web run test -- --run
NODE_ENV=production npm --prefix web run build
npm --prefix web run test:e2e -- asset-decisions.spec.ts
```
