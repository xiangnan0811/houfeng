# 运维与实际数据验收

| 文档或资源 | 用途 |
| --- | --- |
| [首次安装 smoke](fresh-install-smoke-run.md) | 真实登录、agent 接入、观测、incident 和通知证据 |
| [资产数据验收](asset-ledger-real-data-validation-readiness.md) | 样例 dry-run、可丢弃数据库导入和授权真实数据验证 |
| [非敏感资产样例](asset-ledger-local-sample.json) | 固定样例；续费日期的历史窗口须按数据验收说明处理 |

安装、升级及 PostgreSQL/附件/Records authority 协调恢复见 [部署指南](../deploy/local-and-systemd.md)。
UI 预览、Chromium 和 staging 方法见 [浏览器验证](../development/ui-preview-and-browser-sanity.md)。
本地、CI、发行资产、真实安装和生产验收分别报告，不能互相替代。
