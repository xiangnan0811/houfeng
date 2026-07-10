# Modal 栈与嵌套焦点实施计划

## Files

- Create: `web/src/lib/modalStack.ts`, `web/src/lib/modalStack.test.ts`
- Modify: `web/src/lib/useModalFocus.ts`; create its focused test
- Modify: `web/src/components/atoms/Modal.tsx` and tests
- Modify tests: `ActionConfirmationModal.test.tsx`, `AssetDecisionsPage.test.tsx`

## Checklist

- [ ] 先写父 dialog + 子 alertdialog 失败测试，覆盖 Tab、Escape、focus restore 与 body lock。
- [ ] 用纯单测实现并验证 register/top/depth/subscribe 和引用计数 scroll lock，包含重复 cleanup。
- [ ] 让 `useModalFocus` 接收 stable id，只在 `isTopModal(id)` 时响应键盘并仅查询本层 focusables。
- [ ] `Modal` 通过生命周期注册栈，删除 overlay 重复 Escape；backdrop 同时检查 top 与 persistent。
- [ ] 为非栈顶层应用 inert/aria-hidden，并验证关闭栈顶后属性恢复。
- [ ] 增加 Asset Decisions 模板归档或成员移除嵌套确认回归测试。
- [ ] 运行 focused tests、全量 web gate 和 390/1440 浏览器键盘流程。
- [ ] 以 `fix(web): make modal behavior stack aware` 提交。

## Verification

```bash
NODE_ENV=test npm --prefix web run test -- --run src/lib/modalStack.test.ts src/lib/useModalFocus.test.tsx src/components/atoms/Modal.test.tsx src/components/ActionConfirmationModal.test.tsx src/pages/AssetDecisionsPage.test.tsx
NODE_ENV=test npm --prefix web run test -- --run
NODE_ENV=production npm --prefix web run build
```
