# Design

## Root Cause

VPS 详情页当前存在两套“注意力入口”：

- `buildVPSDetailOverviewModel()` 生成顶部 `judgement`，但只把取消 / 退役特殊提升为 `judgement.primaryAction`。
- 同一个 model 还生成 `contextAction`，由 `VPSContextActionPanel` 在页面中部渲染“运行观测需要核对 / 缺少当前订阅 / 缺少运行观测”等横条。

这导致用户必须在顶部和中部两处理解“当前到底要不要处理”，并且中部横条会破坏 V15/V17 已确定的主体结构。

## Architecture

保持 `VPSDetailPage` 的主体布局：

1. 顶部 `VPSDetailOverviewPanel`
2. 操作反馈 stack
3. 关联概览
4. 单机台账
5. IP 质量概况

移除中部 `VPSContextActionPanel` 渲染。`contextAction` 逻辑不再作为页面 section 输出，而是转化为顶部“当前判断”的关注项。

## Data Model

在 `vpsDetailOverviewModel.ts` 中扩展顶部判断模型：

- 保留 `rows` 用于稳定基础判断：决策、续费、动作。
- 新增 `attentionItems`，每项包含 title、reason、tone、primaryAction、secondaryActions。
- 将现有 `VPSContextAction` 复用于 attention item，避免复制动作字段结构。
- `contextAction` 可保留为兼容字段，但页面不再消费它；后续可在单独清理任务中移除。

优先级：

1. 取消 / 退役 / 迁移联动：critical，动作 `处理取消/退役`。
2. 运行观测异常：按监控状态 tone，动作 `查看监控实例`，次动作 `监控观测`。
3. 订阅读取失败：notice，动作 `核对订阅`。
4. 缺少当前订阅：critical，动作 `创建/更新订阅`。
5. 临近续费 / 已取消自动续费：notice/critical，动作 `调整决策`，次动作 `延长有效期`。
6. 缺少运行观测：alert，动作 `接入/升级 agent`，次动作 `关联已有监控实例`。
7. IP 质量暂不可用：notice，动作 `查看 IP 质量`。

允许多个 attention item 同时显示，避免只选单一“下一步动作”。

## UI Behavior

`VPSDetailOverviewPanel` 中的“当前判断”改为：

- 标题仍为“当前判断”。
- `rows` 保持短字段。
- 若有 `attentionItems`，在 rows 下方显示紧凑列表，每项只显示标题、短原因和动作按钮。
- 若无 attention item，不显示额外列表，只保留“动作 无”。
- 按钮使用现有 `Button` / `Link`，动作通过现有 modal mode 或 route 处理。

文案原则：

- 不显示“标题进入详情”“只保留临时动作”等解释性说明。
- 不增加页面级新横条。
- 不引入大段说明文字。

## Compatibility

- 不改 API。
- 不改 modal 内容。
- 不改关联概览和单机台账的数据模型。
- 删除 `VPSContextActionPanel` 的页面引用后，该组件可以保留未使用，或若 lint 报未使用 import 则移除 import；文件本身可留作后续清理。

## Test Strategy

- Model unit tests：
  - 稳定 VPS 无 attention item。
  - 运行异常生成顶部 attention item。
  - 缺订阅 / 订阅读取失败生成顶部 attention item。
  - 取消 / 退役与运行异常同时存在时生成多个 attention item。
- Page tests：
  - “运行观测需要核对”出现在 `aria-label="当前判断"` 内。
  - 页面不再出现 `aria-label="需要处理的状态"` section。
  - 顶部 attention 的 link / modal 动作可用。
  - 取消 / 退役入口仍从顶部打开 modal，更多菜单不出现危险入口。

## Rollback

如视觉或交互不符合预期，可只回退 `VPSDetailOverviewPanel`、`vpsDetailOverviewModel.ts`、`VPSDetailPage.tsx` 和对应 CSS / 测试，不涉及后端数据迁移。
