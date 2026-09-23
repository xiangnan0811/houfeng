# 项目规范

规范正文属于本仓库，根 [AGENTS.md](../AGENTS.md) 为入口，基本开发与检查见
[CONTRIBUTING.md](../CONTRIBUTING.md)。按修改范围阅读，不要求加载全部规范。

- [后端](backend/index.md)：Go center、systemd agent、PostgreSQL、部署、授权、证据与兼容合同。
- [Web](web/index.md)：页面与组件、类型与状态、异步所有权、样式和质量门禁。
- [通用指南](guides/index.md)：分支、协作、跨层与复用。
- [现行产品和设计](../docs/design/current/README.md)：产品语义、架构、界面语言、组件模式。
- [运维文档](../docs/README.md)：安装、部署、备份恢复和真实环境验收。

保留兼容性、失败恢复、权限、安全及回归承诺；实现改变时同步相应规范。
历史任务标签仅说明来源，不代表活动任务或要求恢复旧生命周期。历史详情可查 Git，
不要重建任务/日志目录。当前代码与合同冲突时需调查，不能以“代码如此”为由静默取消合同。
