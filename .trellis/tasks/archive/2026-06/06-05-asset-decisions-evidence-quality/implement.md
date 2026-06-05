# Implementation Plan

## Steps

- [x] 读取 Trellis backend/web/guides 规范，确认当前任务适用约束。
- [x] 在 `internal/center/assetdecisions` 增加 evidence assessment 类型、评分函数、组级聚合函数。
- [x] 在 `buildMember`、`buildGroup` 和 record snapshot 中接入 assessment。
- [x] 增加/更新 Go domain tests，覆盖完整证据、资料缺口、证据源不可用、取消联动/预算压力、record snapshot。
- [x] 更新前端类型。
- [x] 更新 `/asset-decisions` 组列表、组详情、记录详情展示。
- [x] 更新前端测试 fixtures 和断言。
- [x] 如 visual mock 需要显式字段，更新 `scripts/visual_evidence.py`。
- [x] 运行质量检查：Go tests、frontend tests、typecheck/build、Trellis check。
- [x] 使用浏览器检查 `/asset-decisions?view=needs_decision&renew_within_days=30` 桌面和移动端视觉 sanity。

## Validation Commands

- `go test ./internal/center/assetdecisions ./internal/center/store`
- `npm --prefix web test -- AssetDecisionsPage`
- `npm --prefix web run typecheck`
- `npm --prefix web run build`

## Rollback Points

- 后端新增字段无法保持兼容时，先保留 domain assessment 函数但不挂到 API。
- UI 视觉复杂度过高时，保留 group/member 字段，只在详情 drawer 展示，不动列表列宽。
- 如果快照读取出现旧记录兼容问题，快照展示保持 optional parser，不要求历史记录包含 assessment。
