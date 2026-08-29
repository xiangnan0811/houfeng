# 首次监控 Agent 接入死锁修复设计

## 1. Approved approach

采用方案 A：在新版 VPS Overview 内补齐已有 Legacy 流程的创建/onboarding 能力，并把 Monitoring 页两个入口统一导向未关联 VPS 库存。

- VPS 详情继续是 VPS-scoped MonitoringInstance 创建的唯一 UI owner。
- Monitoring 列表继续只负责监控实例扫描，不新增选择器、表单或写入 owner。
- 复用现有 `VPSMonitoringInstanceCreateForm`、`createVPSMonitoringInstance`、`VPSWriteOwnerStore` 和 onboarding URL，不新增后端 endpoint、schema 或 migration。
- 保留“关联已有监控实例”的只读关系证据面板，与“创建并接入 agent”分开。

不采用以下方案：

- 回退到 Legacy：会绕过已发布的 `records_v2_read` Overview gate，扩大回归面。
- 在 Monitoring 页直接创建：会产生第二个资产写入 owner，与现有页面职责和幂等合同冲突。

## 2. Action semantics and compatibility

后端 `monitoring.unlinked.v1` 保持稳定 action ID `open_monitoring_instances`，仅把用户文案从“关联监控”改为“创建并接入 agent”。这避免改变已发布 wire contract。

前端按来源精确解析同一 action token：

- anomaly `(monitoring.unlinked.v1, open_monitoring_instances)` → 内部命令 `open_monitoring_onboarding`；
- relation `monitoring_instances` → 既有内部命令 `open_monitoring_instances`。

`VPSOverviewPageView` 将新命令打开 `monitoring-instance-create` panel；既有 relation command 仍打开 `monitoring-instance-evidence`。未知 rule/action、带 route 的伪造 command 或错误 relation 继续 fail closed。

## 3. Overview onboarding owner

新增 page-private `VPSOverviewMonitoringOnboarding.tsx`，由 `VPSOverviewManagementActions` 挂载。该组件只承担监控创建这一条边界，避免继续膨胀已经较大的 management actions 文件。

组件接收：

- `vpsId`、`management`、`managementTriggerRef`；
- `onOverviewRefresh`；
- 父级已经选定的共享 `writeOwnerStore` 与 `viewToken`。

组件不得创建第二个生产 store，也不得把 request body、digest 或 idempotency key 放入可观察 UI state。API 调用位于 `pages/` 私有组件，符合当前页面/组件分层。

打开 panel 时先调用 `getVPSAsset(vpsId)` 获取权威 active links，再执行分流：

| active links | 行为 |
|---|---|
| 0 | 用 `monitoringInstanceCreateDraftFromDetail` 预填并显示现有创建表单 |
| 1 | 不创建，直接进入该实例的 onboarding |
| >1 | 不创建，关闭创建 panel、打开只读关系证据，并提示先人工核对 |

不能只依赖 Overview summary count，因为打开动作和提交之间可能发生并发关联变化。

## 4. Create, ownership and race handling

提交沿用现有合同：

1. `buildMonitoringInstanceCreateInput` 完成客户端规范化。
2. 通过共享 store 取得 `monitoring-create` owner；同一 VPS 其他写入进行中时拒绝第二次提交。
3. `prepareCreate` 为 canonical body 绑定稳定 idempotency key。
4. 调用 `createVPSMonitoringInstance(vpsId, input, key)`；服务端在同一事务创建 instance、active link 和 receipt。
5. 成功后生成 `/monitoring/{id}?onboarding=1&return_vps={vpsId}`，刷新 Overview 并进入 onboarding。

settle 规则保持现有 registry 合同：

- transport/未知结果保留同 body 的 key；
- body 改变生成新 key；
- `idempotency_key_reused` 轮换 key；
- 服务端确认成功清除 attempt；
- exact owner 才能 finish，旧异步回调不能释放或覆盖后继 owner。

对非 `idempotency_key_reused` 的 HTTP 409，先重新读取权威 VPS detail：若已经出现唯一 active link，则视为并发收敛并进入该实例 onboarding；若出现多条则转入关系证据；仍为 0 才显示原错误。不得根据自由文本猜测错误类别，也不得自动发第二个 POST。

## 5. Async and feedback behavior

- 每次 VPS identity/panel 打开使用 generation/request token；路由切换、关闭或新请求使旧读结果失效。
- submitting 时 Modal persistent，取消按钮禁用；按项目级 Modal 合同，persistent 只阻止 Escape/backdrop，标题栏显式关闭仍可用。显式关闭会重置 draft/error/notice、失效本地 continuation authority 并把焦点归还触发入口，但不得清除或窃取共享 transport owner。
- 服务端确认创建后，即使 Overview refresh 返回失败，也不得把创建显示为失败或重试 POST。关闭 panel，显示“已创建并关联，但概览刷新失败”，提供“继续接入 agent”链接。
- route identity 已变化、panel 显式关闭或组件卸载时，旧回调不得继续 POST、导航、刷新、显示 feedback 或改写新 VPS state；已经发出的 transport 仍由 exact owner 在 `finally` settle。
- 当前 panel 若观察到同 VPS 的外部 owner，必须持续 fail closed；该 exact owner 消失时只发起一次权威 re-probe。旧 probe 只有在 VPS/view/panel generation、观察 token 均仍匹配且不存在后继 owner 时才能提交；后继 owner 抢占后，旧结果不得短暂解锁表单，其 owner 消失后再由新一轮权威证据执行 0/1/多分流。
- 多关联、加载失败和并发冲突都 fail closed：没有权威 0-link 证据时不显示可提交创建表单。

## 6. Deep link and Monitoring entrypoints

扩展 Overview workbench parser，接受 `workbench=monitoring` 与 canonical `workbench=monitoring-instance-create`，统一打开 `monitoring-instance-create`，随后以 replace 删除 query，避免刷新或后续 search edit 重复打开。`VPSDetailPage` 保持唯一 query owner；删除 `VPSOverviewManagementActions` 内重复的 `useSearchParams` effect，并把“拒绝未知 workbench 但仍清除 query”的断言移到 route-level test，避免父子同时开 panel/replace URL。

Monitoring 页两个入口统一为：

- 页头按钮：文案“从未关联 VPS 接入 agent”，目标 `/vps?view=unlinked`；
- 首次空状态：说明“从未关联 VPS 中选择一台，创建监控实例并接入 agent”，按钮“选择未关联 VPS”，目标 `/vps?view=unlinked`。

VPS 库存已有 `view=unlinked`，本任务不增加新的 inventory filter 或选择器。当当前 view 为 `unlinked` 时，行点击和名称链接都导航到 `/vps/{id}?workbench=monitoring`；其他库存 view 仍进入普通详情。这样用户从 Monitoring 入口选择 VPS 后会直接打开同一 Overview onboarding owner，而不是再次寻找入口。

## 7. Files and boundaries

主要修改：

- `internal/center/vpsoverview/anomalies.go` 与测试：只改未关联动作 label。
- `web/src/pages/vps-detail/vpsOverviewDestination.ts` 与测试：区分 anomaly onboarding 与 relation evidence。
- `web/src/pages/VPSDetailPage.tsx`、`web/src/pages/vps-detail/hooks/useVPSManagementController.ts`、`VPSOverviewPageView.tsx`、`vpsManagementHelpers.ts`：由 route 作为唯一 workbench query owner并增加 panel/command/deep-link 路由。
- 新增 `web/src/pages/vps-detail/VPSOverviewMonitoringOnboarding.tsx` 及 focused test。
- `VPSOverviewManagementActions.tsx`：仅挂载新 owner 并传递共享 store/token，不内联完整 workflow。
- `web/src/pages/VPSPage.tsx` 与测试：未关联 view 的行/名称链接携带 `workbench=monitoring`，其他 view 不变。
- Monitoring page 组件与测试：修正文案和 `/vps?view=unlinked` 目标。
- `web/e2e/vps-overview-destinations.spec.ts`：覆盖真实首次接入路径及关系面板回归。

不修改 MonitoringInstance API DTO、数据库、Agent 协议、Legacy 行为或通用 CSS。新 UI 复用现有 Modal、Button、Input 与表单类名，因此不新增视觉层级或 mock。

## 8. Test and acceptance model

测试按 RED → GREEN 推进：

- resolver：未关联 anomaly 解析为 onboarding，relation 仍解析为 evidence；
- Go：action ID 不变、label 更新；
- focused component：0/1/多分流、成功创建、刷新失败 continuation、transport retry、body/key rotation、409 convergence、取消与 stale result；
- inventory/workbench：未关联行进入 monitoring workbench，两个 alias 一次性打开并清除 query；
- Monitoring：两个入口的文案与精确 URL；
- Playwright：Overview capability 开启、0 links 时从异常动作打开表单，POST 后进入 onboarding；relation 点击仍只读。

最终验证同时记录 Node 22 Web gate、Go 1.26.2 项目工具链全量 gate 与浏览器结果，不能用 focused GREEN 或不受支持的 Go 1.27.0-X 输出冒充项目工具链结论；附件 golden 不在本任务修改范围。

## 9. Security, privacy and rollback

- UI、日志和测试输出不泄露 idempotency key、digest、agent secret 或原始敏感 request body。
- URL 只带 allowlisted monitoring instance/VPS IDs；沿用 resolver 的 app-relative 校验。
- 回滚只需撤销前端命令/panel/文案与后端 label；无数据迁移和持久化回滚。
- 交付继续遵守 feature branch → protected PR → required CI；未经后续明确授权，本规划不启动实现、不提交也不发布。
