# VPS 详情、活动历史与 Markdown 记录产品调研

> 调研日期：2026-07-13。以下只记录可从官方文档或公开开源产品文档核对的模式；页面截图和营销文案不作为产品事实。

## 1. AWS Cloudscape：资源详情页先按任务模型选结构

来源：[Resource details](https://cloudscape.design/patterns/resource-management/details/)、[View resources](https://cloudscape.design/patterns/resource-management/view/)

Cloudscape 将资源详情分为三种模式：

- 普通详情页：一个主要任务，信息一览，不承担导航。
- 带 tabs 的详情页：同一资源上有多个独立任务，按可行动分组切换。
- hub 型详情页：同一资源具有多个任务及多个关联资源；展示关联资源预览，并提供进入完整集合的明确链接。

官方判据强调：hub 型详情页应同时包含资源本身的详情和关联资源预览；关联资源容器必须提供明确链接。它不是只有入口的中转页，也不是把所有关联资源完整展开。

对候风的启发：

- VPS 详情天然同时包含 VPS 本身、订阅、监控实例、IP 质量、服务、域名与历史，产品形态更接近 hub，而不是单一详情页。
- 当前“三层结构”方向仍有价值，但每个关联项必须有稳定、显式的进入语法，不能只依赖标题 hover。
- 当资产历史/运维记录升级为真正工作区后，可考虑在详情页内部使用少量任务 tabs；不能把每个 API 子域都变成 tab。
- Cloudscape 的 split view 适合从列表连续比较多个资源；当前请求聚焦单台 VPS，不应为此把详情页退化成列表侧栏。

## 2. NetBox：自动 change log 与人工 journal 明确分离

来源：[Journaling](https://netboxlabs.com/docs/netbox/features/journaling/)、[Change Logging](https://netboxlabs.com/docs/netbox/features/change-logging/)

NetBox 的 journaling 定义非常清楚：journal 是围绕对象、由人生成的长期备注/评论集合，用来补充“为什么这样改”以及系统外发生的事件；它与自动 change log 分离，并随对象生命周期长期保留。每条 journal entry 有 kind、comments，并自动记录时间和用户。

Change log 则保存对象 create/update/delete 前后的序列化快照、时间、用户；同一请求产生的多个变更共享 request UUID，便于关联。编辑/删除/批量操作可附加一个简短 changelog message，记录原因或外部工单。

对候风的启发：

- 当前 `VPSTimeline` 把决策、价格、IP、规格和经验五个数组并称“资产历史”，但既不是完整 change log，也没有把 human journal 当作独立对象。
- 推荐把产品概念拆成：
  - `资产活动 / 变更记录`：系统生成、不可随意修改、带 before/after、来源、操作者、时间和相关请求/动作；
  - `运维记录 / 经验`：人工编写、Markdown、长期可读，解释问题、排查、修复与结论。
- 默认页可以将二者按时间合并为“最近活动”，但完整视图必须能按来源区分，不能让用户误以为系统变更就是人工经验。
- 对自动变更提供可选“变更原因/关联工单”比让用户事后另写一条孤立备注更有上下文。

## 3. Sentry：高层结论、证据详情、活动评论分区

来源：[Issue Details](https://docs.sentry.io/product/issues/issue-details/)

Sentry Issue Details 的组织方式是：header 承载高层信息和主要动作；主要区域展示一个具体事件的完整证据；sidebar 放首次/最近发生时间、外部链接、issue activity/comments 等元数据。Activity 以时间顺序呈现 assignment、regression、escalation 和用户评论。搜索与时间范围只影响相关证据，不把所有内容同权堆叠。

对候风的启发：

- 顶部只应保留身份、当前结论、最紧迫动作和少量关键状态；具体证据与活动记录应有各自工作区。
- “最近活动”需要真实的跨类型时间排序；不能像当前模型一样按数组类型优先级截取。
- 一条运维记录应能关联监控事件、命令审计、服务商工单或外部 URL，但 Sentry 级别的附件/replay/trace 复杂度不适合首轮 MVP。

## 4. GitLab Incidents：Markdown 摘要、系统事件与时间线协作

来源：[Incidents](https://docs.gitlab.com/operations/incident_management/incidents/)、[Rich text editor](https://docs.gitlab.com/user/rich_text_editor/)

GitLab Incident 使用 Markdown summary 表达关键事实，可由模板和告警内容预填；系统状态变化生成 system notes；timeline events 概括“发生了什么、采取了哪些步骤”；评论既可按线程看，也可切换为按时间的 recent updates。告警详情放在单独 tab，不与人工叙述混为一块。

GitLab 的编辑器可在 rich text 与 Markdown source 间切换且不丢数据，支持表格和图等复杂结构。对候风而言，这证明“存储 Markdown 源文本 + 提供更友好的编辑视图”可行，但首轮没必要复制完整 WYSIWYG、diagram 或协作评论系统。

推荐借鉴：

- 为“问题排查 / 修复记录 / 重要发现”等类型提供可选模板，而不是强迫所有记录填一套结构化字段。
- Markdown 编辑至少提供 `编辑 / 预览`、基础工具栏、语法帮助和足够大的编辑区。
- 自动活动与人工记录可在统一时间轴关联，但来源和对象必须清晰可辨。

## 5. GitHub：Markdown 是内容能力，不只是 textarea 语法

来源：[Basic writing and formatting syntax](https://docs.github.com/en/get-started/writing-on-github/getting-started-with-writing-and-formatting-on-github/basic-writing-and-formatting-syntax)、[Attaching files](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/attaching-files)

GitHub 的 Markdown 体验覆盖标题、强调、引用、代码、链接、列表、任务列表、图片、外部引用和 callout；编辑器还提供快捷键与资源上传。

对候风首轮 MVP 的合理子集：

- 标题、段落、列表、任务列表、引用、行内/块代码、链接、表格；
- 编辑/预览；
- 常用插入工具栏与快捷键；
- 禁止或过滤原始 HTML，外链使用安全协议。

暂不应直接照搬：附件上传、mentions、协作通知、Mermaid、脚注、评论线程。它们分别引入存储、权限、清理、安全和协作模型，不能因为“Markdown 富文本”顺带加入。

## 6. Carbon：链接负责导航，按钮负责动作，入口要有稳定 signifier

来源：[Link usage](https://carbondesignsystem.com/components/link/usage/)

Carbon 的规则：链接用于导航，按钮用于改变状态或触发动作；standalone link 可配合一致的内部箭头或外部 launch icon；同页应避免用相同 link text 指向不同目的地；链接文本要说明目的。

对当前页面的直接映射：

- 关联概览标题跳页应是 link；打开本页 modal 或创建/修改记录应是 button。
- 当前同页多组 `资产历史 / 服务 / 域名` 同名入口，以及无图标、无常驻下划线的标题按钮，增加了语义和扫描成本。
- 推荐使用统一的末端 chevron/arrow、可见 hover/focus 之外的静止 signifier，以及更具体的 accessible name；不需要把每个入口变成高强调实心按钮。

## 7. W3C WCAG 2.2：24×24 是最低直接命中目标，不是理想尺寸

来源：[Understanding SC 2.5.8 Target Size (Minimum)](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html)

WCAG 2.5.8 要求 pointer target 至少 24×24 CSS px，或满足充分间距等例外；W3C 同时说明即使满足间距例外，小目标仍可能难以操作，重要控制应考虑更大的目标。

当前 staging 的关联标题仅约 18px 高、快捷按钮多为 24px；Axe 没有报告违规，说明不能武断声称全部违反 WCAG，因为可能存在 spacing exception。但从首次使用和移动触摸角度，关联项应让整行/明确的末端动作形成至少 32–40px 的稳定命中区，而不是只让 18px 标题文字可点。

## 8. DigitalOcean：深度监控进入任务 tab

来源：[How to Track Performance with Droplet Graphs](https://docs.digitalocean.com/products/droplets/how-to/track-performance/)

DigitalOcean 将 Droplet 的监控图表放在详情页 `Insights` tab；用户从 Droplet 列表进入具体 Droplet，再切换到该 tab。基础图表默认可用，安装 metrics agent 后再提供更多指标和告警。

对候风的启发：

- 监控和 IP 质量这类深度证据不应在概览页重复完整展开；概览负责状态、摘要和明确入口，完整证据进入专门任务面。
- “未安装/未接入 agent”与“监控正常”不是同一强度的卡片；缺能力时入口才升权。

## 综合结论

### 可迁移的模式

1. 保留 VPS 详情作为 hub，但同时保证它本身是完整的 VPS 概览，而不是入口集合。
2. 用少量任务分组处理多个独立工作流；关联对象预览使用显式链接进入完整内容。
3. 将自动资产活动与人工运维记录拆成两个真实对象，再在“最近活动”中统一排序。
4. 顶部承担身份、状态、最重要动作；证据、关系、活动各自有明确结构。
5. 导航用 link/arrow，修改用 button；静止状态就能识别，不依赖 hover 猜测。
6. Markdown 记录需要编辑/预览、模板、足够编辑空间和安全渲染，而不是只扩大 textarea。

### 不宜照搬

- AWS/Sentry 级别的多栏、多 tab、海量证据面会让单用户自托管产品重新变复杂。
- GitHub/GitLab 的附件、mentions、评论线程与协作通知超出当前单操作者边界。
- 所有子域都做固定 tab 会隐藏整体态势并增加切换成本；只为真实独立任务建立 tab。
- 为解决朴素感而加入装饰图表、渐变、品牌插画或多套状态色，不解决功能可发现性。

## 待通过用户意图决定

- 人工记录究竟是不可变的“操作日志”，还是可持续编辑的“故障案例/知识条目”；两者的 API、修订与审计模型不同。
- 默认 VPS 详情最终采用单页 hub、`概览 + 少量 tabs`，还是双栏工作台；需要结合核心任务和使用频率决策。
- 自动活动需要扩展到哪些对象变化，以及历史保留/分页边界。
