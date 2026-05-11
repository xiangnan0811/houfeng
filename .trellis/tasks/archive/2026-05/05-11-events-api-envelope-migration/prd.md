# Events API envelope migration

## Goal

关闭 v1 gap #16：将 `GET /api/events` 的成功响应从裸 JSON 数组迁移为稳定 envelope：

```json
{
  "items": []
}
```

同时保持页面层消费语义稳定：`web/src/lib/api.ts` 的 `listEvents(...)` 继续向页面返回 `StateChangeEventRecord[]`，由 API client 负责解包 envelope。

## Requirements

- 后端 `GET /api/events` 成功响应必须返回 `{"items":[...]}`。
- 错误响应保持现有 `{"error": ...}` 结构，不引入新的错误模型。
- 事件查询语义保持不变，包括 `object_type`、`object_id`、`severity`、`event_type`、`limit`、`created_from`、`created_to`、`label`、`notification`、`recovery`、`maintenance`、`include_backfilled`。
- store / read model / 数据库结构不变，本 task 只迁移 HTTP 成功响应形状与调用方。
- 前端 `listEvents(...)` 读取 envelope 并返回数组，避免 `EventsPage`、`NodeDetailPage`、`TargetDetailPage` 等页面重复处理响应形状。
- 后端 handler 测试覆盖 envelope；前端 API client 测试覆盖 envelope 解包；页面/详情页测试按需更新到新响应形状。
- `.trellis/spec/`、`docs/release/v1-gap-checklist.md`、`docs/operations/v1-smoke-run.md` 及相关 release docs 必须同步，不能继续声明 `/api/events` 返回裸数组。

## Out of scope

- 分页字段、cursor、total count、schema version 等新响应元数据。
- 事件模型、事件写入、backfill 逻辑或真实数据补采。
- release/publish workflow；用户已明确后续再考虑。
- 真实环境 smoke run；本 task 仅更新 smoke 文档与本地验证。

## Acceptance criteria

- `GET /api/events` handler 测试证明响应顶层为 object 且包含 `items` 数组。
- `listEvents(...)` 测试证明客户端从 `{"items":[...]}` 解包为页面需要的数组。
- 事件列表页与节点/目标详情事件流测试在新响应形状下通过。
- gap #16 在文档中标记关闭，并说明验证范围。
- 本地 Go 与 Web 相关质量门通过，至少包括后端测试、前端测试、lint/type/build 或等价 `make verify-*`。
