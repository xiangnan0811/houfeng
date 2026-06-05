# Design

## Architecture

本任务延续现有两层模型：

- 自动组 read model：继续负责发现组合决策机会，不落库。
- 决策记录 memory layer：继续保存用户结论和证据快照，本任务只在成员级增加 follow-up tracking。

跟进状态属于“决策记录成员的执行记忆”，不修改 VPS、Subscription、MonitoringInstance、Target 或 lifecycle action 状态。真实操作仍通过现有页面完成。

## Data Model

新增迁移 `0036_add_asset_decision_member_followups.sql`：

- `asset_decision_record_members.followup_status text not null default 'todo'`
- `asset_decision_record_members.followup_note text not null default ''`
- `asset_decision_record_members.followup_updated_at timestamptz null`
- check constraint：`todo`、`in_progress`、`blocked`、`done`、`skipped`
- `(record_id, followup_status, updated_at desc)` 索引，服务记录详情与未来过滤
- 更新 `asset_decision_records_with_counts` view，增加跟进聚合计数

聚合计数使用明确字段，避免前端从成员列表二次推断：

- `followup_todo_count`
- `followup_in_progress_count`
- `followup_blocked_count`
- `followup_done_count`
- `followup_skipped_count`

## Backend Contract

沿用现有 endpoint：

`PATCH /api/asset-decisions/records/{record_id}`

现有 payload 继续可用：

```json
{
  "status": "in_progress"
}
```

新增成员跟进 payload：

```json
{
  "members": [
    {
      "vps_id": "vps_123",
      "followup_status": "blocked",
      "followup_note": "等待迁移窗口"
    }
  ]
}
```

一次 PATCH 可同时包含记录级字段和成员字段。成员 patch 语义：

- `vps_id` 必填。
- `followup_status` 可选；存在时必须合法。
- `followup_note` 可选；存在时 trim 后保存，允许清空。
- 同一 payload 中重复 `vps_id` 为 invalid input。
- 记录不存在返回 404。
- 成员不属于该记录返回 400。

Store 使用事务：

1. 校验输入。
2. 确认 record 存在。
3. 更新记录级字段。
4. 更新每个成员 follow-up 字段。
5. 有任何字段更新时刷新 record `updated_at`。
6. 返回 `GetRecord` 风格的 detail。

## Frontend Contract

类型更新：

- 新增 `AssetDecisionFollowupStatus`。
- `AssetDecisionRecordSummary` 增加跟进计数字段。
- `AssetDecisionRecordMember` 增加跟进字段。
- `PatchAssetDecisionRecordInput` 增加 `members`。

页面更新：

- 已保存记录列表在状态列展示跟进进度，例如 `推进 2/5`，并突出阻塞数量。
- 记录详情成员表增加“跟进”列：
  - 状态 select。
  - 备注输入。
  - 保存按钮。
  - 当前状态 badge 和最后更新时间。
- 保存成员跟进后更新 record detail、records list 和提示信息。
- 推进链接仍保留，且与 decided action 对齐。

## Compatibility

迁移默认值保证旧记录成员进入 `todo`。API 新字段为向后兼容新增字段；现有 PATCH status/title/goal payload 不需要调整。

前端测试 fixture 需要补齐新字段，视觉 mock 需要同步，否则页面测试会遗漏新合同。

## Risks And Boundaries

- 不做自动完成推断：真实 VPS 状态变化可能与跟进状态不一致，这属于后续“执行证据回读”能力。
- 不做批量操作：避免把组合页变成危险执行面。
- 不把成员状态反推整条 record 状态：用户仍需要显式决定整条组合决策是否完成或放弃。
