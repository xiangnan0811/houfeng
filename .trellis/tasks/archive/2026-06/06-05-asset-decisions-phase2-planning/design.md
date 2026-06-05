# Design

## Architecture

资产组合决策中枢分为两层：

- 自动组 read model：继续由 `internal/center/assetdecisions` 从 VPS、订阅、服务、域名、Target 和监控关联派生，不落库，作为发现入口。
- 决策记录 memory layer：新增持久化记录，保存用户在某个自动组上形成的组合判断和证据快照，作为长期追踪对象。

自动组 ID 只作为“创建记录时定位当前组”的输入，不作为长期外键。决策记录拥有自己的 `record_id`。

## Data Flow

```
existing facts -> DeriveGroups -> GroupDetail
GroupDetail + user input -> asset_decision_records + asset_decision_record_members
records list/detail -> AssetDecisionsPage saved decision surface
```

创建记录时后端重新读取事实并查找 `source_group_id`，使用当前 `GroupDetail` 生成组级和成员级 `evidence_snapshot`。如果组已变化或不存在，返回 not found，让前端提示用户回到组列表重新选择。

## Persistence

新增迁移：

- `asset_decision_records`
  - `record_id text primary key`，使用 `ids.New("adr")`。
  - 来源字段：`source_type`、`source_group_id`、`source_group_type`、`source_view`、`scope_key`、`scope_label`、`renew_within_days`。
  - 用户字段：`title`、`goal`、`status`。
  - 快照字段：`evidence_snapshot jsonb`。
  - 时间字段：`created_at`、`updated_at`、`decided_at`、`completed_at`。
- `asset_decision_record_members`
  - 主键 `(record_id, vps_id)`。
  - 成员字段：`display_name`、`suggested_role`、`decided_role`、`suggested_action`、`decided_action`、`reason`。
  - 快照字段：`evidence_snapshot jsonb`。

状态枚举：

- `draft`
- `decided`
- `in_progress`
- `completed`
- `abandoned`

## Backend Contracts

在现有 `/api/asset-decisions/*` 下新增：

- `GET /api/asset-decisions/records`
- `POST /api/asset-decisions/records`
- `GET /api/asset-decisions/records/{record_id}`
- `PATCH /api/asset-decisions/records/{record_id}`

错误映射：

- invalid input -> 400 `invalid input`
- missing auto group -> 404 `asset decision group not found`
- missing decision record -> 404 `asset decision record not found`
- unsupported method -> 405 `method not allowed`
- repository failure -> 500 `internal server error`

## Frontend Contracts

新增 API helper 和 snake_case 类型：

- `listAssetDecisionRecords`
- `createAssetDecisionRecord`
- `getAssetDecisionRecord`
- `patchAssetDecisionRecord`

`AssetDecisionsPage` 新增“已保存组合决策”辅助 surface，展示记录标题、状态、目标、成员数、来源组、更新时间和操作。组详情中提供保存当前组为决策记录的入口，保存表单允许编辑标题、目标、状态和成员级角色/动作/理由。

记录详情 modal 展示证据快照和成员判断，并允许推进状态。执行类动作仍以链接跳转为主，不在记录详情中执行批量动作。

## Compatibility

Phase 1 的 overview/groups/group detail API 保持兼容，只收紧 `renew_within_days` 为 `30/60/90`。前端原本已经只会发这三个值，所以主路径不受影响。

新增表不会修改现有 VPS、Subscription、MonitoringInstance、Target 状态。决策记录删除能力暂不提供，避免早期 UI 中把审计记忆误当临时草稿随手删除。

## Risks

- 自动组变化导致创建记录失败：返回 404 并提示用户刷新组列表。
- 证据快照过大：首版只保存摘要字段、成员统计和 chips，不保存完整嵌套业务对象。
- 状态机膨胀：记录状态只表达决策推进，不执行业务动作。
- 页面信息过载：已保存记录作为辅助 surface，不取代自动组列表主 surface。
