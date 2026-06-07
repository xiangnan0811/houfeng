# 统一交互弹窗修复 - Design

## Architecture

本任务沿用现有前端分层：

- `web/src/components/atoms/Modal.tsx` 继续作为唯一弹窗原子，负责 portal、role、标题关联和焦点管理。
- `web/src/components/ActionConfirmationModal.tsx` 承载业务确认展示，替代旧的页面内联 `ActionConfirmationCard`。
- 页面仍负责 API 调用、本地 pending/error/draft 状态和刷新逻辑；共享组件保持受控，不新增全局状态。

## Modal Contract

`Modal` 新增 `dialogRole?: 'dialog' | 'alertdialog'`，默认仍为 `dialog`。组件内部使用 `useId()` 生成标题 id，并把 `aria-labelledby` 指向对应标题。打开、关闭、overlay click、Escape 和焦点恢复继续由当前行为和 `useModalFocus` 负责。

确认类弹窗使用 `dialogRole="alertdialog"`。普通编辑弹窗保持 `dialog`。

## Action Confirmation Contract

`ActionConfirmationModal` 复用旧 `ActionConfirmationCard` 的可见信息结构：

- 当前状态。
- 之后状态。
- 会发生。
- 不变。
- 错误。
- 取消与确认按钮。

组件是受控弹窗：由调用方传入 `open`、`onCancel`、`onConfirm`、`submitting` 和 `error`。取消时调用方清理 pending state；确认失败时调用方保留 `open` 并传入错误。

为降低迁移风险，旧 `ActionConfirmationCard` 可以在过渡中保留导出，但生产调用应切到 Modal 版本，测试也应迁移到 Modal 行为。

## Page State Flow

页面迁移按现有状态模型最小改动：

- 原先渲染内联确认块的位置改为渲染受控 `ActionConfirmationModal` 或普通 `Modal`。
- 原先 inline draft 的编辑入口改为 `editingX` / `draftX` state 驱动的编辑 Modal。
- 成功后关闭 Modal 并刷新或更新本地列表。
- 失败时保留 Modal，显示错误，不清理 pending state，允许用户重试或取消。
- 取消、Escape、overlay close 都必须丢弃 draft/error/pending。

## Nested Modal Rule

已有 Modal 内触发的二次确认不能再渲染子 `Modal`。Asset Decisions 这类已在 Modal 内的操作通过当前 Modal 内部步骤完成：

- 默认步骤展示编辑/详情内容。
- 确认步骤展示二次确认内容。
- 取消确认步骤回到默认步骤或关闭当前 Modal。
- 确认成功后刷新当前 Modal 内容或关闭。

## Probe Delete Summary

Probe 删除不再命令式操作 DOM。删除确认的 summary 由 `formatConfigSummary(probeItem.config)` 在 React render path 中声明式传给确认 Modal。关闭或失败重试不会留下外部 DOM 残留。

## Compatibility

不改变 API 请求体、响应类型、权限和业务状态机。路由、URL-state、Drawer 交互和只读详情弹窗保持现状。样式复用现有 Modal、page panel、button、badge 等 BEM 类与 tokens，不引入新依赖。

## Rollback

若某个页面迁移出现高风险，可以按调用点回退到旧内联确认，但必须保留 `Modal` 的 role/id 修复和已通过的共享组件测试。回退时不得恢复 Probe 删除的 DOM 注入副作用。
