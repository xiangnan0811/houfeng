# Web 规范

前端通用工程约定在这里维护。页面、业务 DTO 与跨层行为见 [领域合同](../contracts/README.md)，产品和视觉理由见 [设计](../../design/README.md)，开发命令见 [CONTRIBUTING](../../../CONTRIBUTING.md)。

| 规范 | 适用范围 |
| --- | --- |
| [目录结构](directory-structure.md) | 模块职责与代码归属 |
| [组件约定](component-conventions.md) | 原生控件、错误恢复、Modal 与组件拆分 |
| [状态与数据](state-and-data.md) | transport、类型、hooks 与 Context |
| [样式规范](styling-guidelines.md) | CSS owner、令牌、CSP 与可访问性 |
| [质量规范](quality-guidelines.md) | 类型、测试、质量门与证据边界 |

优先运行与修改范围相关的测试；完整前端门禁为根目录 `make verify-web`。浏览器检查与真实安装验收是独立证据。
