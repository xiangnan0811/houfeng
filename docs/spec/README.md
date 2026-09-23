# 规范与领域合同

这里维护可验证的行为承诺。先按功能找 [领域合同](contracts/README.md)，再按实现层阅读
[后端通用规范](backend/README.md) 或 [Web 通用规范](web/README.md)。

- [领域合同](contracts/README.md)：资产、订阅、监控、agent、事件、Records 与平台边界。
- [后端](backend/README.md)：Go、数据库、错误、日志和测试方法。
- [Web](web/README.md)：组件、状态、样式和前端测试方法。

产品理由见 [设计](../design/README.md)，开发流程见 [开发指南](../development/README.md)，
安装和恢复见 [部署](../deploy/README.md)。修改行为时同步合同、调用方与回归证据；代码现状
不自动取消兼容、安全、权限及恢复承诺。各合同内的实现缺口和待决项仍需单独验证。
