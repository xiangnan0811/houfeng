# 归档功能体验与规则重构设计

## 后端设计

- 在 `assetlifecycle` 增加 Archive Review / Apply Archive / Restore From Archive 合同。
- Archive Review 复用现有 lifecycle 读取能力：VPS、subscriptions、monitoring instance links、services、domains、target impacts。新增 archive-specific blockers：
  - active subscription 阻止；
  - MonitoringInstance lifecycle 非 `不续费` / `已退役` 或 monitoring status 非 `暂停` 阻止；
  - Target run status 非 `暂停` / `已归档` 阻止。
- `POST /api/vps/:id/archive` 在同一事务中锁定 VPS、重新计算 review、校验 confirmation name 和 blockers，再写入 `lifecycle_status=archived`。
- `POST /api/vps/:id/restore-from-archive` 锁定 VPS，仅当当前状态为 `archived` 时写入 `idle`。沿用 `patchVPSAssetRow` 清空 `archived_at` 的既有行为。
- 普通 `PATCH /api/vps/:id` 阻止：
  - 非 archived -> archived；
  - archived -> 非 archived。
  这样归档/恢复只能走受控接口。
- Router 需要显式接入新 subtree：`archive-review`、`archive`、`restore-from-archive`。同时更新 `RouterOptions`、`vpsSubtreePath`、router tests 和 `cmd/houfeng-center/bootstrap.go` 注入。
- HTTP 错误仍按现有短英文 JSON message：invalid input、lifecycle action blocked、vps asset not found、internal server error。

## 前端设计

- API client 新增：
  - `getVPSArchiveReview(vpsId)`
  - `archiveVPS(vpsId, { confirmation_name })`
  - `restoreVPSFromArchive(vpsId)`
- 类型文件新增 Archive Review / Archive input 合同，与 Go JSON snake_case 对齐。
- 路由拆分：
  - `/archive` -> `ArchivePage` 列表页；
  - `/archive/:vpsId` -> `ArchiveDetailPage` 只读详情页。
- `VPSDetailPage`：
  - 加载到 `cancelled` / `archived` 后使用 `navigate('/archive/:id', { replace: true })`。
  - 归档操作先读取 review；有 blockers 时展示并禁用提交；无 blockers 时要求输入展示名。
  - 成功后 `navigate('/archive/:id', { replace: true })`。
  - 订阅读取在归档相关刷新路径使用 `asset_scope=all`，避免刚归档后订阅证据消失。
- Archive 列表页只加载 `listVPSAssets({ asset_scope: 'archived' })` 和 `listSubscriptions({ asset_scope: 'archived' })` 形成摘要；不自动拉单台 services/domains/timeline。
- Archive 详情页加载 `getVPSArchiveReview`、`getVPSTimeline`、`listSubscriptions({ vps_id, asset_scope: 'all' })`，并从 review 拿 monitoring links、services、domains、target links。
- Archive 详情页先加载 `getVPSArchiveReview` 判断 VPS 状态；只有 `cancelled` / `archived` 才继续拉 timeline 和订阅历史，其他状态 `replace` 跳回 `/vps/:id`。
- Archive 详情页所有区块默认只读。`archived` 可显示低权重恢复入口；`cancelled` 只显示不可恢复说明。
- Archive 详情页的层级必须体现用户记录优先：上方摘要卡之后先展示 `experience_logs`，再展示续费/价格/规格/IP 历史和订阅/服务/域名明细；监控与 Target 历史仍在页面最底部全宽展示。

## 数据流

归档写入：

`VPSDetailPage -> getVPSArchiveReview -> archiveVPS -> PostgresAssetLifecycleRepository.ApplyVPSArchive -> patch vps_assets -> navigate /archive/:id`

归档详情读取：

`ArchiveDetailPage -> getVPSArchiveReview + getVPSTimeline + listSubscriptions(asset_scope=all) -> read-only sections`

恢复写入：

`ArchiveDetailPage -> restoreVPSFromArchive -> PostgresAssetLifecycleRepository.RestoreVPSFromArchive -> patch vps_assets -> navigate /vps/:id`

## 风险与约束

- 不新增 DB schema，因此 action audit 可以复用 `LifecycleActionResult` 结构，但不强制新建持久 action type；如写 action 审计会扩大迁移范围，本任务优先保证受控状态变更和 UI 体验。
- 不能让普通 `/api/targets` 承担 archive target history；它会过滤只关联归档 VPS 的 Target。
- 前端不能在组件里直接 `fetch`；所有 API 通过 `web/src/lib/api.ts`。
- Archive 详情新组件应拆在 `web/src/pages/archive/`，避免继续膨胀单个页面文件。
