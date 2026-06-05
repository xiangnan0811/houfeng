# Design

## Architecture

资产组合决策中枢在本阶段形成三层模型：

- 自动组 read model：继续由现有 `assetdecisions.DeriveGroups` 从 VPS、订阅、服务、域名、Target、监控关联派生，不落库，用于系统发现。
- 手工组合 scenario layer：新增持久化 manual group，保存用户定义的真实决策场景、目标、成员意图和备注。
- 决策记录 memory layer：继续保存某一次判断、证据快照、成员跟进和 execution readback。

手工组合不是业务状态机。它只表达“用户正在比较这一组资产，为了某个目标做判断”。真正业务修改仍发生在 VPS detail、单台续费决策或 lifecycle workbench。

## Backend Domain

在 `internal/center/assetdecisions` 扩展类型：

- `ManualGroupStatus`: `active | archived`
- `ManualGroupScenario`: `general | primary_standby | budget_reduction | provider_review | region_review | migration_retirement | evidence_cleanup`
- `ManualGroupSummary`
- `ManualGroupDetail`
- `ManualGroupMember`
- create/patch/member input structs

`ManualGroupSummary` 包含：

- manual group identity/status/scenario/title/goal/note/source metadata。
- 成员数、生命周期/用途/续费决策分布。
- 月成本/年成本、base currency。
- 服务/域名/Target/监控/incident 聚合。
- evidence chips、evidence assessment。
- created/updated/archived 时间。

`ManualGroupDetail` 复用 `GroupMember` 作为当前成员事实对比，并叠加 manual member metadata：

- intended_role
- intended_action
- reason
- note
- sort_order

## Persistence

新增 migration `0037_create_asset_decision_manual_groups.sql`：

- `asset_decision_manual_groups`
  - `manual_group_id text primary key`
  - `status text not null default 'active'`
  - `scenario text not null default 'general'`
  - `title text not null`
  - `goal text not null default ''`
  - `note text not null default ''`
  - `source_type text not null default 'manual'`
  - `source_group_id text not null default ''`
  - `source_group_type text not null default ''`
  - `source_view text not null default ''`
  - `scope_key text not null default ''`
  - `scope_label text not null default ''`
  - `renew_within_days integer not null default 30`
  - timestamps
- `asset_decision_manual_group_members`
  - primary key `(manual_group_id, vps_id)`
  - `intended_role`
  - `intended_action`
  - `reason`
  - `note`
  - `sort_order`
  - `evidence_snapshot jsonb`
  - timestamps

The same migration updates the existing record constraint so `asset_decision_records.source_type` allows `auto_group` and `manual_group`.

No foreign key from decision records to manual groups is required. Records keep immutable source metadata and evidence snapshot; archived manual groups must not break historical records.

## API Contract

Manual group endpoints:

```text
GET    /api/asset-decisions/manual-groups
POST   /api/asset-decisions/manual-groups
GET    /api/asset-decisions/manual-groups/{manual_group_id}
PATCH  /api/asset-decisions/manual-groups/{manual_group_id}
POST   /api/asset-decisions/manual-groups/{manual_group_id}/members
PATCH  /api/asset-decisions/manual-groups/{manual_group_id}/members/{vps_id}
DELETE /api/asset-decisions/manual-groups/{manual_group_id}/members/{vps_id}
```

Create manual group supports two modes:

- `source_type=manual`: create empty or explicit VPS member list.
- `source_type=auto_group`: require `source_group_id` and `renew_within_days`; store resolves the current auto group and copies its current members into the manual group.

Record creation extends the existing endpoint:

```text
POST /api/asset-decisions/records
```

Request adds optional `source_type`. Default remains `auto_group` for backward compatibility. `source_type=manual_group` uses `source_group_id` as `manual_group_id` and builds snapshots from current manual group detail.

## Store Data Flow

Manual group list:

1. Query manual group rows.
2. Query members for all group IDs in one batch.
3. Call `loadFacts` once.
4. Build summaries in Go by joining members to facts.

Manual group detail:

1. Load group row and members.
2. Call `loadFacts` once.
3. Build detail members by joining each member to current fact.
4. If a member's VPS fact is missing, retain manual metadata and surface evidence chip `current_fact_missing`.

Manual group create:

- Validate title/status/scenario/source.
- For auto source, load facts and find auto group before transaction.
- For manual source, validate member IDs against facts if members are supplied.
- Insert group and member rows in one transaction.
- Return detail after commit.

Member add/patch/delete:

- Only mutate manual group membership rows.
- Never update VPS, subscriptions, targets, monitoring instances, or record followups.

Record create from manual group:

- Load manual group detail and current facts.
- Convert manual detail into a `GroupDetail` shaped snapshot:
  - `source_type=manual_group`
  - `source_group_id=manual_group_id`
  - group type can reuse source group type when present; otherwise use `evidence_gap` as a neutral, valid fallback only for old record enum compatibility.
  - view can reuse source view when present; otherwise `needs_decision`.
- Use intended role/action as decided role/action defaults.
- Apply existing execution readback after persistence.

## Frontend Contract

`web/src/lib/types.ts` adds snake_case manual group types and create/patch payloads.

`web/src/lib/api.ts` adds:

- `listAssetDecisionManualGroups`
- `createAssetDecisionManualGroup`
- `getAssetDecisionManualGroup`
- `patchAssetDecisionManualGroup`
- `addAssetDecisionManualGroupMember`
- `patchAssetDecisionManualGroupMember`
- `deleteAssetDecisionManualGroupMember`

`AssetDecisionsPage` adds a custom group surface:

- A compact list of active manual groups above saved decision records and below automatic discovery.
- A create-from-auto-group action in group detail.
- A manual group detail modal/drawer with member comparison table.
- Inline member intent editing and add/remove controls.
- A save-as-record form that uses the existing record creation path with `source_type=manual_group`.

The UI must preserve the current hierarchy:

- Automatic decision groups remain the discovery surface.
- Manual groups become the user scenario surface.
- Saved records remain the tracking/readback surface.
- Single VPS queue and renewal evidence stay secondary.

## Compatibility

- Existing `POST /api/asset-decisions/records` calls without `source_type` continue to create records from automatic groups.
- Existing readback, followup, and record list/detail logic must work for both `auto_group` and `manual_group`.
- The migration is additive and idempotent where possible.
- Existing source_group_type/source_view database checks remain valid; manual records use current allowed enum values for compatibility.

## Risks

- Page complexity can grow quickly. The UI should use one compact manual-group surface and a detail modal rather than adding another full page.
- Manual group facts can drift. This is expected; current facts are always read live and records preserve snapshots.
- Existing `source_type` constraint only allows `auto_group`. The migration must drop and recreate the constraint by name safely.
- Current fact missing should be visible, not silently dropped, because a manual group may outlive an archived/deleted VPS reference.

## Non Goals

- No IP quality, route quality, CPU/IO trend, performance decay, or oversell judgment.
- No batch execution.
- No automatic status updates for VPS, Subscription, MonitoringInstance, Target, or decision record.
- No delete endpoint for manual groups in this stage; archive is enough and safer.
