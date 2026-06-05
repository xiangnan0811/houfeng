# Implementation Plan

## 1. Task Setup

- [x] 使用独立 worktree：`.worktree/asset-decisions-execution-readback`
- [x] 使用分支：`worktree/asset-decisions-execution-readback`
- [x] 启用 hooks：`sh scripts/setup-git-hooks.sh`
- [x] `task.py start` 后开始代码修改

## 2. Backend Domain

- [x] 在 `internal/center/assetdecisions` 增加 readback status、issue、facts、record/member readback 类型。
- [x] 增加纯函数评估器：record 聚合、member 判定、facts map、evidence gap 检测。
- [x] 覆盖 cancel、migrate、keep、observe、complete_evidence、review、done drift、blocked、skipped、abandoned、facts missing。

## 3. Backend Store / Handler

- [x] `RecordSummary` / `RecordDetail` / `RecordMember` 返回 `execution_readback`。
- [x] `CreateRecord`、`GetRecord`、`PatchRecord` 返回前计算 readback。
- [x] `ListRecords` 批量读取 record members 并聚合 readback，避免逐条 `GetRecord`。
- [x] Store 测试覆盖 readback 返回、批量成员读取、facts failure、PATCH 后刷新。
- [x] Handler 测试覆盖 records list/detail/create/patch success 响应包含 readback。

## 4. Frontend

- [x] 更新 `web/src/lib/types.ts` readback 类型。
- [x] 更新 API / page fixtures。
- [x] 已保存记录列表展示 readback 状态和 drift / blocked / needs_evidence 计数。
- [x] 记录详情 summary 和成员表展示当前回读、issues、当前事实摘要。
- [x] 测试覆盖列表、详情、跟进 PATCH 不变，以及 readback 不触发业务对象写请求。

## 5. Specs

- [x] 更新 `.trellis/spec/backend/database-guidelines.md` 的 asset decisions scenario。
- [x] 更新 `.trellis/spec/web/state-and-data.md` 的 Asset Decisions contract。
- [x] 明确 IP / 路由 / 性能 / CPU / IO / 超售延后，不进入本任务。

## 6. Verification

- [x] `go test ./internal/center/assetdecisions ./internal/center/store ./internal/center/http/handlers ./internal/center/http/router ./internal/center/bootstrap`
- [x] `cd web && npm run test -- api AssetDecisionsPage`
- [x] `cd web && npm run lint`
- [x] `cd web && npm run build`
- [x] `cd web && npm run test -- --run`
- [x] `make verify-web`
- [x] `git diff --check`
- [ ] `make verify-go`（blocked: `agent/containersample TestCollect_DockerAvailable_ReturnsContainers` 在本机 Docker 采样返回 0 个容器，期望 2 个；目标 Go 包已通过）
- [ ] Browser sanity: `/asset-decisions?view=needs_decision&renew_within_days=30` desktop/mobile（blocked: repo helper 缺本机 Python Playwright；Browser 插件拒绝访问本地 URL）

## 7. Finish

- [ ] Run Trellis check workflow.
- [ ] Archive completed task and record journal.
- [ ] Commit with message `feat(asset-decisions): add execution readback`
- [ ] Push branch, open PR, monitor CI.
- [ ] Merge only when checks pass, then monitor release / publish automation.
