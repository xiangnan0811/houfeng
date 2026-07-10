# 前端可访问性交互契约

## Goal

在共享 atoms 修复原生与读屏契约，并把常用流程中的鼠标专属模拟控件替换为可聚焦、可键盘操作的语义控件。

## Confirmed Facts

- Select 接收 `required` 却不传给原生 select；Input/Select 的 hint/error 没有完整 ARIA 关联。
- Tabs 缺少 roving tabindex、方向键/Home/End 和 tabpanel 关联。
- AST 审查发现至少 12 个真实命令使用不可聚焦 div/span/tr。
- 主题菜单、用户入口与 AppShell 缺少完整菜单键盘行为和 skip link。

## Requirements

- Input/Select 生成稳定 description id，合并调用者 `aria-describedby`，传递 required/ref 并设置 invalid。
- Tabs 强制 label/idBase，采用 roving focus 与 Arrow/Home/End，关联 tabpanel。
- 导航使用 Link/NavLink，命令使用 button；表格主单元格提供真实链接。
- 增加 AST guard，禁止新增非语义 onClick；只允许注释化 backdrop/propagation allowlist。

## Dependency And Scope

- 依赖 `frontend-modal-stack-focus`，避免菜单/焦点改造与 overlay 行为冲突。
- Dashboard 命令由 Task 3 处理，本任务迁移剩余站点。

## Acceptance Criteria

- [ ] Select required、error/hint、自定义 describedby 与 ref 测试通过。
- [ ] Tabs 只有当前项 `tabIndex=0`，Arrow/Home/End 和 panel ids 正确。
- [ ] 核心 workflow 不依赖不可聚焦 div/span/tr。
- [ ] AppShell、Settings、VPS、Dashboard axe serious/critical 为零，键盘流程可完成。
