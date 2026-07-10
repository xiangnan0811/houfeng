# Modal 栈与嵌套焦点实施计划

## Files

- Create: `web/src/lib/modalStack.ts`, `web/src/lib/modalStack.test.ts`
- Modify: `web/src/lib/useModalFocus.ts`; create its focused test
- Modify: `web/src/components/atoms/Modal.tsx` and tests
- Modify: `web/src/app/layout/ChangePasswordModal.tsx`, Modal overlay CSS and `TargetDetailPage.tsx` focus-successor timing
- Modify tests: `ActionConfirmationModal.test.tsx`, `AssetDecisionsPage.test.tsx`, `TargetsPage.test.tsx`, `TargetDetailPage.test.tsx`

## Checklist

- [x] 先写父 dialog + 子 alertdialog 失败测试，覆盖 Tab、Escape、focus restore 与 body lock。
- [x] 用纯单测实现并验证 register/top/depth/subscribe 和引用计数 scroll lock，包含重复 cleanup。
- [x] 让 `useModalFocus` 接收 stable id，只在 `isTopModal(id)` 时响应键盘并仅查询本层 focusables。
- [x] `Modal` 通过生命周期注册栈，删除 overlay 重复 Escape；backdrop 同时检查 top 与 persistent。
- [x] 为非栈顶层应用 inert/aria-hidden，并验证关闭栈顶后属性恢复。
- [x] 增加 Asset Decisions 模板归档或成员移除嵌套确认回归测试。
- [x] 运行 focused tests、全量 web gate 和 390/1440 浏览器键盘流程。
- [x] 以 `fix(web): make modal behavior stack aware` 提交。

## Verification

```bash
NODE_ENV=test npm --prefix web run test -- --run src/lib/modalStack.test.ts src/lib/useModalFocus.test.tsx src/components/atoms/Modal.test.tsx src/components/ActionConfirmationModal.test.tsx src/pages/AssetDecisionsPage.test.tsx
NODE_ENV=test npm --prefix web run test -- --run
NODE_ENV=production npm --prefix web run build
```

## Verification Evidence

- Focused：8 files / 135 tests，覆盖纯栈、hook、通用 Modal、修改密码、Action Confirmation、Asset Decisions、persistent Targets 与 TargetDetail successor focus 流程。
- Browser：Chromium production preview，数据源 `mock-api asset-workflows`；`1440x1000`、`1024x768`、`390x900` 均无页面横向溢出，嵌套 dialog 完整落在视口内。
- Keyboard：真实 CDP Tab 从子层末按钮循环到首按钮；第一次 Escape 只关闭子层、恢复父层“归档模板”并保持 body lock；第二次 Escape 关闭父层、恢复“使用模板”并释放 lock。
- Runtime：production preview 冷 reload 后无 console warning/error、page exception、CSP violation、HTTP 4xx/5xx 或 network failure。
- Local-only limitation：浏览器证据使用仓库 fixture，不证明真实后端数据或 staging；截图保留在本机临时目录且不提交。
