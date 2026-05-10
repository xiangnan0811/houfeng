# Modal focus management

## Goal

关闭 `docs/release/v1-gap-checklist.md` 中 gap #24：让 `Drawer` 与 `ChangePasswordModal` 具备可复用的 modal focus 管理能力，包括 portal render、打开后的初始焦点、Tab containment、Escape/overlay/关闭按钮关闭，以及关闭后恢复触发器焦点。

## What I already know

- 当前用户要求继续推进，且此前明确要求不使用 subagent；本任务在主会话直接执行。
- 当前分支是 `fix/modal-focus-management`，从干净 `main` 创建。
- `docs/release/asset-ledger-roadmap-completion.md` 判定 Asset Ledger 计划当前已功能闭合，真实 40+ VPS 数据 deferred，不作为本任务。
- `docs/release/v1-gap-checklist.md` 中 gap #24 原为 open：`Drawer / ChangePasswordModal` 缺 portal、初始焦点、Tab containment、触发器焦点恢复。
- `web/src/components/atoms/Drawer.tsx` 原为 inline fixed render，支持 ESC、overlay、左右侧、`aria-modal`，但没有 portal/focus trap/focus restore。
- `web/src/app/layout/ChangePasswordModal.tsx` 原为自定义 modal，也没有统一 focus 管理。
- 本任务已新增 `web/src/lib/useModalFocus.ts`，并由 `Drawer` / `ChangePasswordModal` 复用。
- 项目不引入第三方依赖；实现应使用 React/DOM 原生能力。

## Assumptions

- 本任务只做 modal 可访问性与行为硬化，不改变业务 API、认证逻辑、密码修改 payload、Drawer 业务内容或视觉设计主线。
- 可新增一个前端内部 hook/utility 来复用 modal focus 管理；不引入 UI 库。
- Portal target 使用 `document.body`；SSR 不在本项目范围内，jsdom 测试需要覆盖正常行为。
- 触发器焦点恢复以打开前 `document.activeElement` 为准；如果该元素已经不在文档中，则跳过恢复。
- Tab containment 只在 modal 打开时生效；没有可聚焦元素时让 modal 容器本身可聚焦。

## Requirements

- `Drawer`：
  - 使用 portal 挂载到 `document.body`。
  - 打开时把焦点移动到第一个可聚焦元素；若没有可聚焦子元素，则聚焦 dialog 容器。
  - Tab / Shift+Tab 被限制在 Drawer 内。
  - Escape、overlay click、头部关闭按钮保持可关闭。
  - 关闭后恢复到打开 Drawer 前的触发器焦点。
  - 继续保持 `role="dialog"`、`aria-modal="true"`、`aria-label` / `title`。
- `ChangePasswordModal`：
  - 使用同一套 focus 管理 / portal 行为。
  - 打开时初始焦点落到可操作输入或容器。
  - Tab / Shift+Tab 被限制在 modal 内。
  - Escape、overlay click、关闭按钮保持可关闭；表单提交与错误展示逻辑不变。
  - 关闭后恢复到触发按钮焦点。
- 覆盖测试：
  - Drawer portal render、初始焦点、Tab containment、close restore focus。
  - ChangePasswordModal portal/focus trap/focus restore；现有 password submit tests 不回归。
- 文档同步：
  - `docs/design/v2-houfeng/component-spec.md` 更新 Drawer / modal 当前实现说明。
  - `docs/release/v1-gap-checklist.md` 将 gap #24 标记为 Closed，说明仍不引入第三方 modal 库。
  - 如形成可复用 modal focus 约定，更新 `.trellis/spec/web/component-conventions.md`。

## Acceptance Criteria

- [x] `Drawer` children 不在原组件位置 inline render，而是 portal 到 `document.body`。
- [x] 打开 `Drawer` 后焦点进入 dialog；Tab / Shift+Tab 不离开 dialog。
- [x] 关闭 `Drawer` 后焦点回到打开前的触发按钮。
- [x] `ChangePasswordModal` 打开后焦点进入 modal；Tab / Shift+Tab 不离开 modal。
- [x] 关闭 `ChangePasswordModal` 后焦点回到打开前的触发按钮。
- [x] Escape / overlay / 关闭按钮行为不回归。
- [x] 现有密码修改成功/失败流程不回归。
- [x] docs / Trellis spec 同步。
- [x] `git diff --check` 通过。
- [x] `make verify-web` 通过。

## Verification

- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm run lint` — pass.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm run test -- --run` — pass, 60 files / 453 tests.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm run build` — pass, with existing Vite >500 kB chunk warning.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp make verify-web` — pass, with local Node v24 vs project Node 22.x EBADENGINE warning during `npm ci`; CI uses Node 22.
- `git diff --check` — pass.

## Definition of Done

- Work committed on a non-main branch.
- Trellis task archived and journal recorded after work commits.
- PR opened, PR CI monitored until green, then merged.
- Local `main` synced to `origin/main`.
- Post-merge main CI monitored to success.

## Out of Scope

- 不改密码修改 API、session/auth 行为或错误文案。
- 不重做 Drawer / modal 视觉设计。
- 不引入 Radix、Headless UI、focus-trap 等第三方依赖。
- 不把所有页面弹层一次性迁移到新 API；只处理现有 `Drawer` 和 `ChangePasswordModal`。
- 不引入 e2e 框架或截图回归流程。

## Technical Notes

- Likely files:
  - `web/src/components/atoms/Drawer.tsx`
  - `web/src/components/atoms/Drawer.test.tsx`
  - `web/src/app/layout/ChangePasswordModal.tsx`
  - `web/src/app/layout/ChangePasswordModal.test.tsx`
  - possible shared helper under `web/src/lib/` or `web/src/components/atoms/`
  - `docs/design/v2-houfeng/component-spec.md`
  - `docs/release/v1-gap-checklist.md`
  - `.trellis/spec/web/component-conventions.md`
- Relevant specs:
  - `.trellis/spec/guides/branch-workflow-governance.md`
  - `.trellis/spec/web/component-conventions.md`
  - `.trellis/spec/web/styling-guidelines.md`
  - `.trellis/spec/web/quality-guidelines.md`
