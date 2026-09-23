# 领域合同

按业务对象查找完整规则。服务端合同负责持久化、API、授权与兼容；对应 Web 合同负责页面状态、
交互与异步所有权，并引用服务端协议。修改跨层行为时同时检查这两类正文及其列出的回归测试。

| 领域 | 行为与数据 | 页面与交互 |
| --- | --- | --- |
| 资产、服务和域名 | [资产](assets.md) | [资产 Web](assets-web.md)、[VPS 异步所有权](assets-async-ownership.md) |
| 资产组合决策 | [资产决策](asset-decisions.md) | [决策 Web](asset-decisions-web.md) |
| 订阅与成本 | [订阅](subscriptions.md) | [订阅 Web](subscriptions-web.md) |
| 监控与命令审计 | [监控](monitoring.md) | [监控 Web](monitoring-web.md) |
| Agent 安装和同步 | [Agent](agent-sync.md) | 接入交互见监控与资产 Web |
| 入口探测 | 探测事实与 incident 投影见监控合同 | [入口探测 Web](targets-web.md) |
| 事件与补传 | [事件](events.md) | [事件 Web](events-web.md) |
| Dashboard | [Dashboard](dashboard.md) | [Dashboard Web](dashboard-web.md) |
| IP 质量 | [IP 质量](ip-quality.md) | [IP 质量 Web](ip-quality-web.md) |
| 证据快照 | [证据快照](evidence-snapshot.md) | [证据 Web](evidence-web.md) |
| Records | [Records 索引](records/README.md)：授权、草稿、删除、搜索、协作、导入导出与恢复 | Web 合同也由该索引导航 |
| 平台与部署 | [平台索引](platform/README.md)：ACL、迁移、准入、authority、安全与协调恢复 | 部署执行步骤见运维文档 |

通用编码规则见 [后端](../backend/README.md) 与 [Web](../web/README.md)，
开发检查见 [开发指南](../../development/README.md)，安装与协调恢复操作见
[部署](../../deploy/README.md)。合同中的实现缺口不代表放宽要求；本地回归也不替代真实环境验收。
