# 前端 Modal 栈与嵌套焦点

## Goal

让单层及任意嵌套深度的 Modal 具有一致栈语义：只有栈顶处理键盘、backdrop 和焦点约束，滚动锁与焦点恢复不会被子层关闭破坏。

## Confirmed Facts

- 每个 Modal 当前独立注册 document Escape/Tab handler，overlay 还重复处理 Escape。
- 每个实例独立写入和清空 `body.style.overflow`。
- Asset Decisions 存在真实嵌套确认流程；浏览器已复现 Tab 被父层抢回、一次 Escape 关闭两层和滚动锁提前释放。

## Requirements

- 新增单一 modal stack 协调器，注册稳定 id、container 与 restore target。
- 只有栈顶可处理 Escape、Tab、backdrop；persistent 栈顶不因 Escape/backdrop 关闭。
- 非栈顶 dialog 对辅助技术和指针交互不可用；最后一层关闭前 body 始终锁定。
- cleanup 必须幂等并兼容 React StrictMode、异步关闭和异常 unmount。

## Dependency And Scope

- 依赖 `frontend-quality-gate-strict` 合并。
- 保持现有 `Modal` 调用方 props 和业务 API 不变。

## Acceptance Criteria

- [x] 单层、双层、三层、persistent 与 unmount 测试通过。
- [x] 一次 Escape 只关闭最上层；子层关闭后父层仍存在且 body 仍锁定。
- [x] 焦点只在栈顶循环，关闭子层后回到父层原触发按钮。
- [x] Asset Decisions 真实嵌套确认流程保持 URL、草稿和父层 tab 状态。
