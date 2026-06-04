# 服务商页二次信息架构修正设计

## Scope

只调整 `web` 前端中的服务商列表和创建 / 编辑弹窗，不新增后端 API，不改变 providers 数据 contract，不改变 VPS / 订阅页面行为。

## UI Direction

- 列表保持低频维护目录定位，减少解释性状态文本。
- 数据列以真实内容和可执行入口为主，空值保持安静。
- 表格列宽重新平衡：服务商名称不再占用过宽空间，服务入口列容纳短入口链接，标签 / 备注单列截断。
- 编辑弹窗使用更轻的分组、紧凑栅格和一致的控件间距，避免大面积嵌套卡片。

## Data Rules

- `vpsCount` 仍从 `vps.provider_id === provider.provider_id` 派生。
- `subscriptionCount` 仍通过订阅 `vps_id` 映射到 VPS 后归属到 provider。
- 列表不再展示月成本摘要，但保留现有前端加载 subscriptions 的上下文能力。
- 服务入口只在目标存在时展示：
  - `website` 存在时展示官网外链。
  - `panel_url` 存在时展示面板外链。
  - `vpsCount > 0` 时展示 VPS 内链。
  - `subscriptionCount > 0` 时展示订阅内链。

## Test Focus

- 列标题、空值静默、入口移动、名称编辑触发。
- 资产上下文简化为数量。
- 标签 / 备注单列展示。
- 创建 / 编辑行为回归。
