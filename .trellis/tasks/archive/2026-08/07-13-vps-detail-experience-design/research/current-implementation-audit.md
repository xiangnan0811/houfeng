# VPS 详情页当前实现审查（初稿）

## 证据范围

- `web/src/pages/VPSDetailPage.tsx`
- `web/src/pages/vps-detail/VPSDetailOverviewPanel.tsx`
- `web/src/pages/vps-detail/VPSRelatedOverview.tsx`
- `web/src/pages/vps-detail/VPSSingleMachineLedger.tsx`
- `web/src/pages/vps-detail/VPSIPQualitySection.tsx`
- `web/src/pages/vps-detail/VPSExperienceLogForm.tsx`
- `web/src/pages/vps-detail/vpsDetailOverviewModel.ts`
- `web/src/components/VPSTimelinePanel.tsx`
- `web/src/styles/partials/legacy-vps.css`
- `web/src/styles/partials/legacy-assets.css`
- `db/migrations/0022_create_experience_logs.sql`
- `internal/center/renewals/types.go`
- `internal/center/store/renewal_decisions.go`
- `internal/center/store/vps_assets.go`
- `docs/design/current/interface-language.md`
- `docs/design/current/component-patterns.md`

## 已确认的问题

### 1. 页面仍有大量重复事实

- 生命周期、用途、续费决策、监控数量先出现在顶部 badges，又在事实网格或“当前判断”重复。
- 订阅金额/续费时间同时出现在事实网格、当前判断和关联概览。
- IP 质量同时出现在事实网格、关联概览和独立概况区。
- 资产历史同时出现在顶部按钮、关联概览、单机台账与 modal。

这不是必要的“概览—详情”重复，因为多个位置使用近似相同的短摘要，却没有分别承担状态、解释、导航或操作职责。

### 2. 顶部操作密度重新膨胀

`VPSDetailOverviewPanel` 同时常驻：资产历史、服务、域名、调整决策、基础资料、更多、VPS 列表。更多菜单内又包含十余个动作。顶部没有按“查看、编辑、关联、生命周期”建立稳定分组，用户必须逐个阅读按钮文本。

### 3. 可点击入口依赖隐式样式

- `VPSRelatedOverview` 的标题使用无背景、无边框的 button/link；静止态与普通标题近似，只有 hover 后才变色和出现下划线。
- `VPSSingleMachineLedger` 的记录、承载和变化整行由透明 button 承载，静止态没有图标、箭头或明确链接形态。
- IP 质量卡标题可跳转但没有常驻的进入指示；相关卡与普通事实块形态接近。
- “更多”使用 `…` 的原生 `details/summary`，没有动作分组标题，也不符合当前项目对完整键盘 menu 行为的最新组件合同。

这直接验证了用户关于“按钮等可点击内容与普通文本差异不明显”的判断。

### 4. 视觉层级主要由细边框和小字构成

- 页面以多个白色 `page-panel` 包裹内部网格；面板、网格、单元格和状态轨都使用相近的 1px 边界。
- eyebrow 为 10px，辅助文字为 11.5px；在大屏高密度布局中，重要性差异主要靠字号和字重的细微变化。
- 正常状态轨只有 1px 且使用透明混色；顶部四个 badges 一律 neutral，不表达状态形状。
- 各区都使用相似的“eyebrow + 标题 + 细分隔线 + 网格”，缺少身份区、状态区、关系区、日志区各自可辨认的结构。

当前实现与 `docs/design/current/interface-language.md` 的两项指导发生漂移：状态应尽量同时使用颜色和形状；卡片不应包裹每个页面区块。

### 5. “当前判断”在稳定态价值不足

截图中的 `决策=保留 / 续费=8 个月后续费 / 动作=无` 与左侧 badge、订阅事实重复，却固定占用接近三成的顶部内容宽度。它没有提供理由、置信度、最新评估时间或证据变化，只是再次陈述结果。

### 6. 单机台账不是可靠的时间摘要

`buildLedger` 先拼接最多三条经验记录，再拼接决策记录，最后整体截取三条；它没有把不同类型按时间排序。只要经验记录达到三条，任何更晚的决策都不会出现。

`latestLedgerRecord` 也固定优先选择经验，其次决策、价格，没有比较时间；关联概览中的“最近记录”可能并不是实际最近记录。

“关键变化”只取一条价格和一条 IP 变化，忽略决策、规格及其他变化，也没有按时间排序。

### 7. 资产历史展示和能力都过于简单

前端把五种记录分别放进两列分组卡：续费决策、价格、IP、规格、经验。结果是：

- 不存在跨类型的统一时间顺序，无法还原“先发生什么、随后做了什么”。
- 没有类型筛选、时间范围、搜索、聚合/折叠或分页。
- 默认展示 Decision ID、Log ID、VPS ID 等内部字段，仍出现 `ASSET TIMELINE` 与 `timeline 历史证据`，与上一轮确认文案相冲突。
- 经验详情被压成副标题，长内容没有阅读层级或独立详情。
- API 一次返回五个完整数组；记录增长后，详情页首次加载和 modal 渲染都没有容量边界。

底层仅自动记录续费决策、价格、IP 和部分规格字段。名称、Provider/位置、用途、重要性、标签、备注、生命周期、监控关联、服务/域名关系等变化并不进入该时间线，因此“资产历史”这个名称暗示的完整性高于真实能力。

### 8. 经验记录是追加式短备注，不是运维知识记录

当前记录字段为：领域分类、严重级别、摘要、发生时间、详情。数据库的 `details text` 可以容纳 Markdown 源文本，但系统当前：

- 没有 Markdown 解析、渲染和消毒依赖；
- textarea 很矮，placeholder 引导一两句话；
- 没有编辑/预览、工具栏、语法帮助、全屏、草稿恢复或模板；
- 没有“问题、排查、根因、修复、验证、后续行动”结构；
- 没有记录类型与领域分类的区分；
- 没有编辑、删除、修订历史、作者/操作者、附件或关联对象；
- 创建接口只有 GET 列表与 POST 新增，数据模型没有 `updated_at`。

因此用户的判断成立：仅把现有 textarea 宣称为 Markdown 不足以解决问题，需要先定义记录作为“日志、案例、知识条目或事件复盘”中的哪一种产品对象。

## 尚待真实环境验证

- 各入口的点击路径、焦点返回、Escape/Tab、触摸目标和窄屏折行体验。
- 资产历史 modal 在真实两条记录、更多记录以及长详情下的布局。
- 三套主题中的扫描层级与状态差异。
- 网络加载、局部失败、空态与异常态是否产生误导。
- 真实用户完成典型任务所需的点击数与回退路径。

## staging 验证补充

0.59.0 staging 已验证上述重复入口、隐式标题入口、移动端逐项堆叠、资产历史分组/内部术语和经验详情编辑空间不足等判断。自动化 Axe 为 0 违规，说明问题主要位于信息架构、静止可发现性、触摸目标、内容模型和任务效率，而不是基础 DOM 语义完全缺失。详细数据见 `research/staging-walkthrough.md`。

以上问题按“已确认事实 / 待实测”分开记录；严重度与最终取舍将在 staging 走查后确定。
