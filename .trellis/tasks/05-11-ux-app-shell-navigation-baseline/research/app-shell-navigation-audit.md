# App shell / navigation audit

> 日期：2026-05-11
>
> 目的：为 UX-1 实现确认当前壳层、导航和测试状态。

## 已查看文件

- `docs/release/core-pages-product-ux-replan.md`
- `docs/design/v2-houfeng/design-language.md`
- `docs/design/v2-houfeng/component-spec.md`
- `.trellis/spec/web/styling-guidelines.md`
- `.trellis/spec/web/component-conventions.md`
- `.trellis/spec/web/quality-guidelines.md`
- `web/src/app/metadata.ts`
- `web/src/app/layout/AppShell.tsx`
- `web/src/app/layout/Sidebar.tsx`
- `web/src/app/layout/TopBar.tsx`
- `web/src/app/layout/Breadcrumb.tsx`
- `web/src/app/layout/GlobalSearch.tsx`
- `web/src/app/layout/layout.css`
- `web/src/app/layout/Sidebar.test.tsx`
- `web/src/app/layout/AppShell.test.tsx`
- `web/src/app/router.tsx`
- `web/src/styles/tokens.css`

## 当前状态

### 导航模型

`metadata.ts` 当前只有扁平 `PRIMARY_NAV_ITEMS`：

```text
首页 / VPS / 服务商 / 订阅 / 资产决策 / 节点 / 目标 / 事件 / 设置
```

这个结构没有表达 `总览 / 资产 / 观测 / 系统` 的产品心智模型，也没有让资产决策成为真实数据测试路径中的优先入口。

### Sidebar 渲染

`Sidebar.tsx` 直接 `PRIMARY_NAV_ITEMS.map`，每个 item 一条 link。节点/目标 anomaly count 通过 `item.to === '/nodes'` 和 `item.to === '/targets'` 判断，badge tone 固定为 neutral。

现有行为需要保留：

- brand 文案；
- active NavLink；
- 节点/目标 count；
- SyncStatus；
- UserChip；
- `aria-label="主导航"`。

### CSS

`layout.css` 已满足基本 v2 sidebar contract：

- app shell grid：`220px 1fr`；
- sidebar sticky；
- brand hairline；
- nav active 左侧 accent 条；
- mobile 下 sidebar 变为顶部 grid。

但当前 nav item 没有分组标题，9 个资源项连续排列，视觉上仍像后台资源表。

### 测试

`Sidebar.test.tsx` 当前断言所有旧 label 和 link text 顺序：

```text
首页 / VPS / 服务商 / 订阅 / 资产决策 / 节点3 / 目标1 / 事件 / 设置
```

UX-1 需要更新为新分组顺序，并将 `首页` 改为 `工作台`。

`AppShell.test.tsx` 遍历 `PRIMARY_NAV_ITEMS` 断言 link 存在。若 `PRIMARY_NAV_ITEMS` 从分组模型派生，该测试仍可保留。

## 实现约束

1. 保留路由路径，不改 `router.tsx`。
2. 保留 `PRIMARY_NAV_ITEMS` 对外导出，降低影响面。
3. `PRIMARY_NAV_GROUPS` 可以成为新的权威模型，Sidebar 渲染分组，扁平数组从它派生。
4. 节点/目标 anomaly count 不抽成新后端字段，继续使用现有 `anomalyCounts` props。
5. 样式只改 `layout.css`，不碰 page 级 CSS。
6. 不引入 lucide 或其它图标库；当前任务不需要图标。

## 推荐实现

- 新增 `NavGroup` 类型，包含 `label`、`items`。
- 将顺序调整为：工作台、资产决策、VPS、服务商、订阅、节点、目标、事件、设置。
- Sidebar 外层循环 group，渲染 `.sidebar__nav-group`、`.sidebar__nav-group-title`、`.sidebar__nav-list`。
- CSS 增加 group title eyebrow 样式和 group 间 hairline，mobile 下隐藏或压缩 group title，避免顶部导航过高。
- 更新 Sidebar/AppShell tests。
