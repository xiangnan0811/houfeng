# 实施计划

## 准备

- 确认当前 worktree 在 `worktree/asset-decisions-execution-orchestration`。
- 运行 `sh scripts/setup-git-hooks.sh`。
- 读取 backend/web/guides spec 与本任务 artifacts。
- `task.py start` 后开始代码修改。

## Backend

- 新增 execution plan 类型与评估器。
- 将 `RecordSummary`、`RecordMember` 扩展 `ExecutionPlan` 字段。
- 在 `ApplyExecutionReadback` / `ApplyExecutionReadbackToSummaries` 中派生 plan。
- 增加 domain tests：
  - action -> lane/step kind。
  - abandoned inactive。
  - completed drift 仍 actionable。
  - blocked 优先。
  - done 但事实不一致为 drift plan。
  - complete_evidence 只用现有 evidence gap。
  - current fact missing。
- 更新 store/handler tests，断言 API 响应包含 plan 且 ListRecords 继续批量读取。

## Frontend

- 更新 `web/src/lib/types.ts` 和测试 fixtures。
- `AssetDecisionsPage`：
  - 增加 plan label/tone/lane helpers。
  - 记录列表增加 plan 列/片段。
  - 记录详情增加 execution board。
  - 增加快速跟进 helper，复用 record PATCH。
  - 保留既有明细表。
- 更新前端 tests：
  - 列表展示 plan。
  - 详情 lane board 和 CTA。
  - followup PATCH payload 不变。
  - drift 不触发业务对象写请求。

## Specs

- 更新 `.trellis/spec/backend/database-guidelines.md`。
- 更新 `.trellis/spec/web/state-and-data.md`。
- 更新 `docs/design/v2-houfeng/component-spec.md`。

## Validation

- `go test ./internal/center/assetdecisions ./internal/center/store ./internal/center/http/handlers`
- `cd web && npm run test -- --run AssetDecisionsPage api`
- `cd web && npm run lint`
- `cd web && npm run build`
- `git diff --check`
- 启动前端本地服务并检查 `/asset-decisions?view=needs_decision&renew_within_days=30` 桌面与移动端。

## Finish

- 运行 Trellis check。
- 记录 journal / archive task。
- commit、push、PR、监控 CI。
- 如果触发 Release Please / release / Docker automation，继续监控到最终状态。
