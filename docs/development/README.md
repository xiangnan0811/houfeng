# 开发与验证

开发命令从根目录 [CONTRIBUTING.md](../../CONTRIBUTING.md) 开始；行为要求查阅 [规范入口](../spec/README.md)，产品理由查阅 [设计入口](../design/README.md)。

| 文档 | 何时阅读 |
| --- | --- |
| [分支与交付](branch-workflow-governance.md) | 选择 checkout、修改、提交、PR、发布或清理前 |
| [协作与验证](agent-collaboration.md) | 委派实现、独立审查、项目加载变更和证据交付 |
| [代码复用](code-reuse-thinking-guide.md) | 新增 helper、重复逻辑、配置或批量修改 |
| [跨层开发](cross-layer-thinking-guide.md) | API、数据库、服务与界面的边界变化 |
| [UI 预览与浏览器验证](ui-preview-and-browser-sanity.md) | 页面预览、Chromium、局部浏览器检查与 staging 验证 |

选择与改动相关的指南，先搜索现有实现及消费者，再定义边界和执行对应检查。真实安装和数据验收见 [运维入口](../operations/README.md)。
