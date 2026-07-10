# Dashboard 事实可信度实施计划

## Files

- Create: `web/src/pages/dashboard/dashboardRemoteState.ts`, `dashboardModel.ts` and tests
- Rewrite: `web/src/pages/DashboardPage.tsx`, `DashboardPage.test.tsx`
- Modify/delete after usage inventory: `web/src/pages/dashboard/*.tsx`
- Modify: Dashboard owner CSS only

## Checklist

- [ ] 先写 abnormal subset 与 VPS failure-not-onboarding 的纯 model 失败测试。
- [ ] 实现 `RemoteState<T>` helpers 和五状态 `buildDashboardModel`，保持无 React 依赖。
- [ ] 为五种模式建立 fixture，逐一声明 primary action、judgement、evidence 与 deep link。
- [ ] 页面分别保存 dashboard/VPS/subscription 状态，不再用空值表达失败。
- [ ] 重写 command surface：唯一“今日第一步”，最多三个判断摘要，局部失败可重试。
- [ ] 清点旧 dashboard 组件；删除无引用实现，缩小仍使用组件的 props。
- [ ] 重写页面测试，加入必须出现、禁止出现、VPS 503 与 subset contract。
- [ ] 在 1440x1000 和 390x900 验证五类 fixture 的首屏与深链。
- [ ] 将 model 修正与 presentation 作为可独立回滚的提交，最终 PR 标题 `fix(web): restore trustworthy dashboard decisions`。

## Verification

```bash
NODE_ENV=test npm --prefix web run test -- --run src/pages/dashboard/dashboardModel.test.ts src/pages/DashboardPage.test.tsx
NODE_ENV=test npm --prefix web run test -- --run
NODE_ENV=production npm --prefix web run build
git diff --check
```
