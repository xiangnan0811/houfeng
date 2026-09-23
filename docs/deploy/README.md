# 安装与部署

[部署指南](local-and-systemd.md) 包含本地、Docker Compose、systemd、认证、升级及协调恢复步骤。
示例中的凭据、主机名和路径需要按目标环境配置；执行部署仍需对应授权。

| 资源 | 用途 |
| --- | --- |
| [pre-R1 provisioning SQL](app-acl-r2-pre-r1-provisioning.sql) | 应用迁移前收紧 PostgreSQL catalog 函数权限 |
| [Compose application role SQL](compose-application-role.sql) | Compose 初始化器创建和校验受限应用角色 |
| [Compose 环境模板](compose.env.example) | 发布包环境配置模板；本地秘密不应提交 |
| [systemd 单元](systemd/README.md) | Center、内容处理器及 agent 的服务资源 |

资源保留固定路径供脚本、安装器及发行流程消费。安装后执行
[全链路 smoke](../operations/fresh-install-smoke-run.md)；开发检查见 [开发入口](../development/README.md)。
