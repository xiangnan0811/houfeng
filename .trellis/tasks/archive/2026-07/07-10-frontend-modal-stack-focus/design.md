# Modal 栈与焦点设计

## Architecture

使用模块级、可订阅的纯栈作为 overlay 协调器。`Modal` 负责生命周期注册，`useModalFocus` 负责栈顶键盘与 focus trap；业务 Modal 不感知栈实现。

## Internal API

```ts
export type ModalStackEntry = {
  id: string
  container: HTMLElement
  restoreTarget: HTMLElement | null
}

export function registerModal(entry: ModalStackEntry): () => void
export function isTopModal(id: string): boolean
export function getModalDepth(id: string): number
export function subscribeModalStack(listener: () => void): () => void
export function acquireBodyScrollLock(): () => void
```

注册和 scroll-lock cleanup 均幂等。栈变化通过 `useSyncExternalStore` 或等价订阅触发 Modal 的 top/depth 状态更新；重复 id 不产生多个有效 entry。

## Interaction Rules

- document 只有 focus hook 的一条 Escape/Tab 路径；overlay 不再处理键盘 Escape。
- backdrop click 在关闭前检查 top 与 persistent。
- 非栈顶容器设置 `aria-hidden` 和 `inert`，栈顶关闭后恢复父层可交互性。
- scroll lock 保存首次获取前的 body overflow，计数归零时恢复原值。

## Rollback

单独 revert Modal stack commit 即可；不迁移业务数据或 API。
