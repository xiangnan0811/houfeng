# VPS 概览管理操作技术设计

## 1. 现状与采用方向

当前新 overview 已有菜单和 visibility controller，但真正的 mutation state 全部
内嵌在 `LegacyVPSDetail.tsx`。直接复用整页会把 1600+ 行 legacy 图、额外读取和
旧版布局带入 overview；复制 handlers 又会产生两套生命周期安全合同。

采用方向：建立一个只服务五类 overview 管理动作的 focused action host，复用现有
表单与纯转换器，并把两页共同需要的小型领域 helper 从 Legacy 抽出。Legacy 页面
继续使用相同 helper，不改变其 capability fallback 和其他抽屉。

## 2. 组件与数据流

```text
VPSDetailPage / VPSOverviewRoute
├── useVPSOverview(vpsId) ─────────────── read-only overview + refresh
├── useVPSManagementController() ──────── menu / selected panel
├── VPSOverviewPageView ───────────────── identity, menu, overview sections
└── VPSOverviewManagementActions ──────── mutation owner
    ├── lazy getVPSAsset(panel open)
    ├── facts: providers + facts form
    ├── decision: decision form
    ├── subscription: subscription form
    ├── cancellation: preview + workbench
    └── archive: review + typed confirmation
```

`VPSOverviewPageView` 不再渲染 placeholder `PageState`。菜单只改变
`VPSManagementController.panel`；route 同级渲染 action host，使 mutation owner
可以调用 `commands.refresh` 和 `navigate`，而展示组件仍保持只读。

## 3. 建议文件边界

### 新增

- `web/src/pages/vps-detail/VPSOverviewManagementActions.tsx`
  - modal/confirmation host；
  - panel-specific load、draft、submitting、feedback 和 submit orchestration；
  - 对外只接收 `vpsId`、management controller、`onOverviewRefresh`。
- `web/src/pages/vps-detail/VPSOverviewManagementActions.test.tsx`
  - 五类动作、错误、竞态、安全门禁和 refresh 合同。
- `web/src/pages/vps-detail/vpsManagementHelpers.ts`
  - 从 Legacy 抽出的、无 React 状态的 shared helper，例如 error 描述、decision
    linkage copy/action、cancellation preview loader；只在确有复用时抽取。

### 修改

- `web/src/pages/VPSDetailPage.tsx`
  - 删除空 `onManagePanel`；挂载 focused action host 并传入 `commands.refresh`。
- `web/src/pages/vps-detail/VPSOverviewPageView.tsx`
  - 删除 placeholder panel 与无效 callback contract。
- `web/src/pages/vps-detail/VPSManagementMenu.tsx`
  - 让 panel selection 只有一个 owner；补齐菜单焦点/Escape 合同。
- `web/src/pages/vps-detail/LegacyVPSDetail.tsx`
  - 仅改为引用被抽取的 shared helper；其余抽屉和行为保持不变。
- `web/src/pages/VPSDetailPage.test.tsx`、`VPSOverviewPageView.test.tsx`、
  `LegacyVPSDetail.test.tsx`
  - route wiring、占位移除、fallback 和 shared-helper 回归。
- `web/src/styles/partials/legacy-vps.css`
  - 只添加 action host 必需的 modal/feedback/390px 样式；优先复用现有 class。

实施时可根据 RED 暴露的最小边界调整文件名，但不得把所有 Legacy state 搬入新
hook，也不得建立第二套 API client。

## 4. Panel 状态模型

每次 `panel` 从 null/menu 进入实际动作时：

1. 记录当前 `vpsId` 和 request generation；
2. 加载最新 `VPSAssetDetail`；
3. facts 额外按需加载 provider selector，cancellation 加载 preview，archive 加载
   review；其他 panel 不加载无关集合；
4. 用 shared helpers 初始化 draft；
5. 只在 generation 与当前 `vpsId/panel` 一致时提交状态。

关闭 panel 或切换 `vpsId` 时使 generation 失效。`submitting` 为 true 时禁用关闭
中的危险确认和重复提交；普通读取失败保留明确重试入口。

## 5. 写入语义

### Facts

`buildFactEditInput` 在客户端做现有验证，随后 `updateVPSAsset`。成功后重新读取/
同步 detail、等待 overview refresh、关闭 panel，并在 overview action host 的
`role=status` 区显示成功。

### Decision

比较 draft 与最新 detail，未变化不提交。成功后保留现有 renewal subscription
linkage 文案/后续链接，等待 overview refresh 后关闭；后续链接显示在 page-level
反馈中。

### Subscription

`buildSubscriptionInput` 保持币种、周期与 renewal flags 转换。成功创建/更新后
等待 overview refresh；不在新 owner 中维护一套长期 subscriptions cache。

### Cancellation

workbench 只接受服务端 preview。blockers、active subscription 显式选择、监控和
target 选择继续由现有组件/服务端保护。写入成功后先保留 result，再并行刷新
overview 和重新拉取 preview；任一 refresh 失败显示局部错误，不否认已成功写入。

### Archive

每次打开都读取当前 review，不复用旧 overview 判定。只有 review eligible、无
blockers、输入完整展示名时确认可用。服务端成功后 replace 导航到
`/archive/:id`，不再刷新当前 overview。

## 6. Feedback 与错误模型

- mutation API 失败：表单保持打开、draft 保留、错误 `role=alert`。
- mutation 成功 + overview refresh 成功：关闭普通 panel，page-level
  `role=status` 显示成功。
- mutation 成功 + overview refresh 失败：显示“写入已成功，概览刷新失败”的
  warning 和手动重试，不允许显示成“写入失败”。
- detail/preview/review 读取失败：panel 显示读取错误、关闭与重试；危险确认禁用。
- route 变更/卸载：忽略陈旧 response，不能把 A VPS detail 写入 B VPS panel。

## 7. 可访问性与响应式

- 复用 `Modal` 和 `ActionConfirmationModal` 的 focus trap/return；新增测试验证从
  “管理”按钮进入菜单、选项进入 modal、Escape 返回合理触发点。
- menuitem 支持 Tab/Enter/Space，Escape 关闭；若加入方向键导航，必须有测试。
- 所有操作保持文本 label，不以颜色单独表达危险/成功。
- 390px 下 modal 内容可纵向滚动，facts 和 cancellation 不产生页面级横向溢出；
  关键按钮和菜单项 44px。

## 8. 非目标与失败关闭

- 不调用 overview endpoint 做写入。
- 不启用永久删除，不添加“永久清理”菜单项。
- detail、preview 或 review 读取不完整时不猜测 payload、不放行危险动作。
- 现有 API 若不能表达任一需求，停止并返回设计评审，不在本 child 偷增后端范围。
