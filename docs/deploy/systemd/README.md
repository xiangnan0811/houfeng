# systemd 服务单元

安装顺序、用户、目录权限、环境和依赖见 [部署指南](../local-and-systemd.md#systemd-installation-example)。
安装前配置对应环境文件，不能以服务启动替代真实业务验收。

- [houfeng-center.service](houfeng-center.service)：Center API 与 SPA。
- [houfeng-content-processor.service](houfeng-content-processor.service)：隔离的附件内容处理进程。
- [houfeng-agent.service](houfeng-agent.service)：受监控主机的 outbound agent。
