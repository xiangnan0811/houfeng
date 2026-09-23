# 事件合同

## Scenario: Events backfilled filter contract

### 1. Scope / Trigger

- Trigger: 修改 `/api/events` 查询参数、`store.EventsFilter`、`PostgresDashboardRepository.ListEvents`，或改动 `state_change_events` 与 runtime facts 的 backfill 展示语义。

### 2. Signatures

- Backend API: `GET /api/events?include_backfilled=<bool>` -> `{"items":[]}`。
- Handler field: `store.EventsFilter.IncludeBackfilled bool`。
- DB source rows: `state_change_events e`；backfill provenance lives in `monitoring_instance_heartbeats.is_backfilled`、`host_samples.is_backfilled`、`probe_observations.is_backfilled`。

### 3. Contracts

- `/api/events` 成功响应返回 envelope：`{"items":[...]}`；错误响应保持通用 `{"error":"..."}`。
- `include_backfilled` 使用 Go `strconv.ParseBool` 解析；前端 URL 用 `include_backfilled=1`，API 请求用 `include_backfilled=true`。
- 默认 `IncludeBackfilled=false` 时，`ListEvents` 必须排除可关联到 backfilled runtime facts 的事件。
- `IncludeBackfilled=true` 时不得添加 backfill exclusion predicate。
- read model 优先读取事件 payload 中显式 `event_at` / `is_backfilled` provenance；只有缺少显式元数据的 legacy row 才通过对象与 `created_at` 推断 backfill 关联。`state_change_events` 没有独立 `is_backfilled` 列，legacy fallback 为：
  - monitoring instance event: `e.object_type = 'monitoring_instance'` 且存在同 `monitoring_instance_id`、同 `observed_at = e.created_at` 的 backfilled `monitoring_instance_heartbeats` 或 `host_samples`。
  - target event: `e.object_type = 'target'` 且存在同 `target_id`、同 `observed_at = e.created_at` 的 backfilled `probe_observations`。
- 不要为了这个筛选改写 raw facts、incident mutation、notification 记录或 retention 语义。

### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| `include_backfilled` 缺失 | handler 传 `IncludeBackfilled=false`，store 默认排除 backfilled 关联事件 |
| `include_backfilled=true` / `1` | handler 传 `IncludeBackfilled=true`，store 不添加 backfill exclusion |
| `include_backfilled=bad` | handler 返回 400 `invalid include_backfilled` |
| runtime fact backfilled row exists but事件对象/时间不匹配 | 不视为该事件的 backfill provenance |
| store 查询失败 | handler 返回 500 `internal server error`，store error 用 `%w` 包装 |

### 5. Good/Base/Bad Cases

- Good: backfilled probe recovery event 默认不出现在事件流；用户打开“包含补传事件”后 `/api/events?include_backfilled=true` 返回它。
- Base: 普通 live event 没有关联 backfilled facts，默认和 include 模式都能返回。
- Bad: 在 `state_change_events` 写路径临时塞 `is_backfilled`、或丢弃 backfilled raw facts 来实现 UI 筛选。
- Bad: handler 解析成功但前端仍禁用 toggle，造成 UI 与 API 契约漂移。

### 6. Tests Required

- Handler test: 成功响应顶层必须是 object，并包含 `items` 数组，不能回退为 bare JSON array。
- Handler test: `include_backfilled=true` 进入 `store.EventsFilter.IncludeBackfilled`。
- Handler test: `include_backfilled=bad` 返回 400。
- Store test: 默认 SQL 包含 monitoring instance heartbeat / host sample / probe observation 的 `is_backfilled` exclusion。
- Store test: `IncludeBackfilled=true` 时 SQL 不包含 backfill exclusion。
- Cross-layer test: `web/src/lib/api.test.ts` 断言 `include_backfilled: true` 序列化为 `include_backfilled=true`。

### 7. Wrong vs Correct

```go
// 错误：默认查询把 backfilled provenance 忽略掉，UI toggle 只是空操作。
records, err := repo.ListEvents(ctx, store.EventsFilter{Limit: 50})

// 正确：默认 store 查询排除可关联 backfilled facts 的事件。
records, err := repo.ListEvents(ctx, store.EventsFilter{Limit: 50, IncludeBackfilled: false})
```

```sql
-- 错误：假设 state_change_events 有 is_backfilled 列。
where e.is_backfilled = false

-- 正确：从 runtime facts 建立只读关联。
where not exists (
  select 1 from probe_observations po
  where po.target_id = e.object_id
    and po.is_backfilled
    and po.observed_at = e.created_at
)
```

---
