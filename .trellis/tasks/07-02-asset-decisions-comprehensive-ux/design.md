# 资产组合决策页面全面 UX 重构设计

## Problem

当前 `/asset-decisions` 的弹窗默认层已经基本从“报告页”收敛为短封面，但页面主体仍然混乱：默认首屏同时展示主判断、统计矩阵、四个辅助入口、决策组扫描、筛选 tabs 和组列表；移动端第一屏几乎看不到真正需要处理的组合。稳定态也不符合“不打扰”：没有待决策组时，页面仍把场景模板推成主任务。

本轮设计基准已由用户确认：**决策优先，稳定静默**。

## Evidence

运行态审计记录见 `research/runtime-audit.md`：

- 70 个状态完成审计，脚本错误 0。
- document/body 横向溢出 0。
- 弹窗封面旧重报告 marker 0。
- 页面主体仍出现英文噪音：`PORTFOLIO`、`RENEWAL`、`CLOSED LOOP`、`EVIDENCE`、`WORKBENCH`、`SCENARIO`、`DECISION MEMORY`、`SINGLE VPS QUEUE`。
- 页面主体密度最高：
  - 默认页：文本 767、按钮 18、链接 13、徽标 22、英文噪音 7。
  - 稳定态：文本 577、按钮 17、链接 13、英文噪音 7。
  - 单台队列：文本 1354、按钮 32、表格 1、徽标 36、英文噪音 8。

## Target IA

页面主体分为三层，视觉权重逐层降低：

1. **当前判断**
   - 有高优先级问题时：只突出一个主工作项，展示短标题、单句判断、少量事实和一个主动作。
   - 无问题时：显示安静稳定状态，不推模板、不制造任务感。
   - 不再渲染四格统计矩阵；统计改为低权重事实条或辅助入口中的 count。

2. **决策组扫描**
   - 作为默认主路径，紧跟当前判断。
   - 列表行/卡只展示：优先级、组类型、标题、短判断、最多 2 个信号、成员/窗口短 meta、查看入口。
   - 不展示成本长串、服务/域名/Target/监控细项、英文 eyebrow 或重复徽标矩阵。

3. **辅助入口**
   - 保存记录、场景模板/自定义组合、续费事实、单台队列降为紧凑工具条。
   - 默认不展开表格或解释文案。
   - 用户点击后才展开对应次级区；深链仍自动打开对应区。
   - 桌面展示为一行低权重入口；移动端展示为 2x2 小入口。
   - 不使用单一“辅助工具”下拉菜单，因为用户仍需要一眼看到历史、场景、续费和单台队列是否可用，但这些入口不能与主判断和决策组扫描同权重。

## Stable State

稳定态定义：

- 当前视图没有自动组。
- 没有记录漂移、阻塞、回读缺证据。
- 没有局部 API 错误。
- 当前筛选上下文没有其它必须处理的资料缺口。

稳定态行为：

- 当前判断标题类似“当前没有需要处理的组合决策”。
- 文案只说明“已加载视图内暂无待处理项，可按需查看历史、模板或单台队列”。
- 不展示主警示色，不展示“处理”类主 CTA。
- 可保留低权重入口：查看全部视图、历史记录、场景模板、单台队列。
- `buildPortfolioLead` 不得在稳定态 fallback 到场景模板或自定义组合。

## Current Work Model

新增或调整纯函数，保持页面数据合同不变：

- `hasBlockingPortfolioWork(metrics, groups, sourceErrors)`：判断是否存在需要打扰用户的组合工作。
- `buildPortfolioLead(...)`：在无 blocking work 时返回 `kind: 'stable'` 的 lead；模板/自定义组合不再自动成为主 lead，只进入辅助入口。
- `buildSecondaryNavItems(...)`：只产出短入口 label、count 和状态，不含英文 sourceLabel 或说明句；支持紧凑工具条渲染。
- `compactPortfolioLeadSummary(...)` / `compactGroupCardSummary(...)`：把 API 长 `summary` 裁成短判断。

不改变：

- `/api/asset-decisions/*` 请求。
- URL 参数合同。
- 记录、模板、自定义组合、单台续费写入 payload。

## UI Components

在当前任务内优先做局部组件提取，降低 `AssetDecisionsPage.tsx` 单文件风险：

- `AssetDecisionLeadPanel`：当前判断 / 稳定态展示。
- `AssetDecisionGroupScan`：决策组扫描标题、tabs、筛选 chips 和列表。
- `AssetDecisionSupportStrip` 或复用 `AssetDecisionSecondaryNav`：辅助入口工具条。组件应渲染四个小入口，桌面一行、移动端 2x2；入口只包含中文短标题、数量/状态和打开动作。

这些组件保持纯展示/受控：

- 不直接调用 API。
- 不读取路由。
- 通过 props 接收数据和 callbacks。

若提取成本高于收益，可先在 `AssetDecisionsPage.tsx` 内重构 helper 和 JSX，但必须保持边界清晰，避免继续堆叠巨型片段。

## Visual Rules

- 页面用户可见文案中文化；英文只允许出现在真实专有名词、ID、VPS 名称、货币代码或机器值必须展示的底稿里。
- 不使用 hero/营销式布局，不新增渐变大背景或装饰物。
- 当前仓库页面样式仍集中由 `web/src/main.tsx` 引入的 `web/src/index.css` 承载，本任务沿用现有 `asset-decision-*` BEM block 在 `web/src/index.css` 局部修改；不新建未被引入的 `pages.css`。
- 颜色、间距、圆角、边框走现有 CSS tokens；不写硬编码 hex。
- 移动端首屏应先看到当前判断和决策组扫描入口，不被辅助入口卡片占满。

## Modal Scope

弹窗不是本轮最大问题，但必须保持并补强已建立的分层：

- 保留 `Cover -> Directory -> Task Panel -> Raw`。
- 自动组、模板、保存记录、自定义组合默认层继续短封面。
- 二级面板不恢复英文 marker 或跨任务内容。
- raw/底稿允许完整表格，但移动端应避免不可读的半列和巨大空白；若实现成本过高，本轮至少保证页面不横向溢出并把 raw 明确低频化。

## Test Design

更新 `AssetDecisionsPage.test.tsx`：

- 默认页：
  - 断言用户可见英文 eyebrow 不出现。
  - 断言辅助入口没有同权重铺开长说明。
  - 断言决策组扫描为主路径，关键组可打开。
- 稳定态：
  - 构造空 `overview/groups/records/manualGroups`。
  - 断言不出现模板主 CTA、不出现“处理阻塞/使用模板”作为主工作。
  - 断言出现安静稳定态文案和低权重入口。
- 深链：
  - `record_id`、`manual_group_id`、`template_id`、`view=renewal`、`view=single_queue` 继续打开对应次级区或弹窗。
- 弹窗保护：
  - 保留当前 cover/directory/panel 密度断言。
  - 增加页面级英文噪音禁止列表。

## Browser Verification

实施后复跑 CDP 审计：

- 桌面 `1440x1000` 和移动 `390x900`。
- 覆盖默认页、稳定态、续费区、单台队列、自动组、自定义组合、模板、保存记录、来源复核。
- 验证：
  - 0 document/body 横向溢出。
  - 页面主体英文噪音为空。
  - 稳定态没有主任务 CTA。
  - 弹窗 cover 指标不回退。

## Risks

- `AssetDecisionsPage.tsx` 已超过 6000 行，局部重构容易误伤 URL 深链和写流程。实现计划必须先补测试再改 JSX。
- 稳定态 fixture 需要在测试中明确构造，不能依赖本轮 ignored mock helper。
- 降低页面密度时不能隐藏必要写入口；辅助入口必须仍可完成记录、模板、续费事实和单台队列流程。
