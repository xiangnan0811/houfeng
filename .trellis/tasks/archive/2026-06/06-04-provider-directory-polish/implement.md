# 实施顺序

1. 读取前端规范与现有 ProvidersPage / 测试 / 样式结构。
2. 调整 ProvidersPage 表格列、入口渲染、名称编辑触发和可选字段展示逻辑。
3. 调整 provider 表单 JSX 与 CSS，使弹窗更紧凑并统一 v2 视觉。
4. 更新 ProvidersPage 测试，覆盖用户明确指出的回归点。
5. 运行 ProvidersPage 定向测试、相关回归测试、lint、build。
6. 启动本地 dev server，检查 `/providers` 桌面和窄屏视觉。
