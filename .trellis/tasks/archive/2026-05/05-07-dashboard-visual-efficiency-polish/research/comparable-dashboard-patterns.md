# Dashboard 同类系统与当前实现研究

## 参考来源

* Grafana 官方 Dashboard best practices：`https://grafana.com/docs/grafana/latest/dashboards/build-dashboards/best-practices/`
* Datadog 官方 Dashboard getting started：`https://docs.datadoghq.com/getting_started/dashboards/`
* Web Interface Guidelines：`https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md`
* 候风当前实现：`web/src/pages/DashboardPage.tsx`、`web/src/styles/pages.css`、`web/src/pages/DashboardPage.test.tsx`
* 既有 Dashboard 任务：`05-07-dashboard-decision-simplification`、`05-07-dashboard-visual-audit-polish`

## 可采纳模式

### 1. Overview 要有明确优先级

Grafana 文档强调 dashboard panels 要服务特定目标，避免把所有数据都塞进一个页面。对应到候风：Dashboard 不应该把 `/api/dashboard` contract 当作展示清单，而是按状态选择最相关事实。当前约束是正确的，但视觉上还要让“状态 → 处理队列 → 上下文/入口”的顺序更明显。

### 2. 监控首页常见信息不等于完整展开

Datadog 的 Dashboard 以 widgets 聚合不同数据源，但仍通过 widget 类型与布局控制信息用途。候风可以吸收“库存、事件、状态、通知都应有入口”的系统性，但不能恢复四五个同权区块。更适合候风的是 compact rail、line item 和深链。

### 3. 高效界面依赖扫描路径

Web Interface Guidelines 的方向与本项目规范一致：清晰层级、文本不溢出、控件和状态要符合预期。候风 Dashboard 当前最大风险不是字段少，而是列表项、上下文、管理入口的视觉重量相近。需要通过排版、边界、间距、状态色和按钮权重形成扫描路径。

## 当前代码观察

### 状态栏

优点：

* `FleetStatePanel` 已经把全局状态、生成时间、关键指标和入口收敛在一处。
* `snapshot_generated_at` 文案没有伪装成健康同步状态。

问题：

* 关键指标仍像小卡片，和下方工作台的卡片/列表视觉语言接近。
* 状态栏主动作与次动作没有形成足够明显的视觉优先级。

### 异常队列

优点：

* `AttentionQueue` 已经替代了 DataTable，更适合处理队列。
* 每个 item 都有对象详情深链。

问题：

* 列表项结构偏平均：对象身份、异常摘要、动作链接的主次可以更清楚。
* 独立 `进入` 文案和整行 link 容易显得重复。

### 运行上下文

优点：

* 只有 3 个 item，没有恢复旧的 Group / Recent 展开。
* 每个 item 都有真实深链。

问题：

* 三个 item 仍呈现为小卡片，容易重新形成区块堆叠感。
* 与管理入口之间缺少“辅助上下文”的低权重视觉定位。

### 正常态 / 维护态

优点：

* 保留了管理入口，Dashboard 没有变成空白页。
* 维护态与异常态有清晰区分。

问题：

* 运行概览数字偏大，可能比其真实信息优先级更重。
* 管理入口仍像文字列表，缺少可操作 surface 的质感。

## 本轮设计取舍

1. 优先改现有 Dashboard 内部结构和 CSS，不新增依赖。
2. 保持 Dashboard contract 不变，避免跨层风险。
3. 不用大图、装饰渐变或营销 hero；候风是服务器管理工具，视觉质感来自秩序与密度。
4. 正常态增加有效管理感，异常态强化处理感。
5. 以测试防止旧混乱回归，以浏览器截图和人工检查验证视觉体验。

