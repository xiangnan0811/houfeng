# 可访问性交互契约实施计划

## Files

- Modify/test: `web/src/components/atoms/Input.tsx`, `Select.tsx`, `Tabs.tsx`
- Modify: `web/src/app/layout/Sidebar.tsx`, `TopBar.tsx`, `AppShell.tsx`
- Modify: `web/src/pages/VPSPage.tsx` 与 AST inventory 中剩余真实命令
- Create: production JSX semantic guard test

## Checklist

- [ ] 写 Select required/error/hint/describedby/ref 失败测试并修复 Input/Select 共同行为。
- [ ] 写 Tabs roving focus、ArrowLeft/Right、Home/End 与 tabpanel ids 失败测试。
- [ ] 实现新 TabsProps，并一次迁移所有调用页面与 panels。
- [ ] 将 Sidebar、theme menu、VPS row 和 inventory 中真实命令改为原生 button/Link。
- [ ] 增加 Escape、Arrow、focus return 与 AppShell skip link 测试。
- [ ] 实现 TypeScript AST guard；仅 allowlist backdrop 和阻止冒泡容器。
- [ ] 对 AppShell、Settings、VPS、Dashboard 运行 axe 与完整键盘验收。
- [ ] atoms 与页面迁移分 commit，PR 标题 `fix(web): restore native interaction contracts`。

## Verification

```bash
NODE_ENV=test npm --prefix web run test -- --run src/components/atoms/Input.test.tsx src/components/atoms/Select.test.tsx src/components/atoms/Tabs.test.tsx
NODE_ENV=test npm --prefix web run test -- --run
NODE_ENV=production npm --prefix web run build
npm --prefix web run test:e2e -- accessibility.spec.ts
```
