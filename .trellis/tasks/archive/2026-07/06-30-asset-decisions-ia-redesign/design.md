# 技术设计

## Architecture

本次改动只重构前端信息架构和组件边界。`AssetDecisionsPage` 继续作为路由装配点负责 API 调用、URL 参数、弹层选择和写操作；新拆出的组件只接收 props、渲染 UI、通过回调上报用户动作。

默认保持现有 API。主工作项由已加载的 overview、groups、records、manual groups 和 templates 在前端合成，但展示层只突出一个优先工作，不把所有中间解释模块铺开。

## Page Structure

- 顶部：保留页面标题和简短副标题，筛选集中到同一行或紧凑工具条。
- 主工作卡：展示当前最高优先级对象、风险/证据/续费窗口摘要和一个主 CTA。
- 自动组扫描：作为页面主内容，按优先级显示组卡片，字段控制在标题、范围、关键风险、证据强度、成本/承载摘要和入口。
- 次级工作区：记录、场景/自定义组合/模板、续费证据、单台队列通过紧凑入口打开；页面只维护一个 `secondaryWorkbench` 展开值，默认 `null`，同一时间最多展示一个次级面板。
- `下一步导览` 和原四步 `决策路径` 不再作为默认独立 section。下一步信号只能压缩到主工作卡或次级入口摘要中，避免形成第二主面板。
- 弹层：自动组详情、自定义组合、模板、记录详情保留现有 modal 行为，但可删减冗余解释。

## Component Boundaries

- `AssetDecisionsPage.tsx`：保留路由、数据加载、URL 状态、写操作、弹层状态。
- `pages/asset-decisions/AssetDecisionLead.tsx`：主工作卡和轻量状态摘要。
- `pages/asset-decisions/AssetDecisionGroupList.tsx`：自动组扫描列表。
- `pages/asset-decisions/AssetDecisionSecondaryNav.tsx`：记录、场景、续费证据、单台队列入口。
- `pages/asset-decisions/assetDecisionViewModel.ts`：纯函数，合成主工作项、状态摘要、次级入口计数和排序所需展示字段。

实际文件名可在实施时按现有依赖进一步收敛，但必须保持“页面负责状态，组件负责展示”的边界。

## Data Flow

- 加载顺序继续沿用当前页面：overview/groups、records、manual groups、templates、subscriptions、VPS catalog。
- `assetDecisionViewModel.ts` 接收已加载结果和错误状态，返回展示模型；它不得发请求、不得读 URL、不得调用 React hooks。
- 部分接口失败时，展示模型必须保留 source error 标记，让 UI 显示局部不可用，而不是用空数组推断“无风险”。

## Compatibility

- URL 参数保持兼容；旧 `view=single_queue` 不成为顶层主 tab，而是显示承接提示和单台队列入口。
- 深链到辅助对象时，页面自动展开对应次级工作区：`record_id` -> records，`manual_group_id` / `template_id` -> scenarios，`view=renewal` -> renewals，`view=single_queue` -> single_queue。`group_id` 仍直接打开自动组详情，不要求展开辅助区。
- 次级区关闭或切换只影响本地展开状态，不主动删除 `view`、`renew_within_days`、`provider_id`、`vps_id`、`country`、`region`、`city`、`scenario` 等筛选参数。
- 现有 API helper 名称和请求路径保持不变。
- 现有写操作 payload 保持不变，尤其是单台续费决策 PATCH 和记录/成员 followup PATCH。

## Risk Controls

- 先写失败测试，证明新信息架构与旧常驻模块不同。
- 测试必须覆盖默认不渲染旧模块标题、点击次级入口后对应工作区可用、深链自动展开对应工作区，以及旧 `view=single_queue` 的承接提示不把单台队列提升为主视觉主体。
- 分阶段提交：任务文档、测试、视图模型/组件拆分、样式与页面接入、验证修复。
- 浏览器检查必须覆盖桌面和窄屏，重点查首屏主次、横向溢出、文字重叠和弹层入口。
