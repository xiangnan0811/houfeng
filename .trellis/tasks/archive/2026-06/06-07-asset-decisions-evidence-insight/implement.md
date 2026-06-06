# 资产组合决策证据洞察与对比增强实施计划

## Preconditions

- 当前 worktree：`.worktree/asset-decisions-evidence-insight`
- 当前分支：`worktree/asset-decisions-evidence-insight`
- PR base：`main`
- hooks：已运行 `sh scripts/setup-git-hooks.sh`
- Trellis 状态：planning；实现前必须通过 review gate 并执行 `python3 ./.trellis/scripts/task.py start .trellis/tasks/06-07-asset-decisions-evidence-insight`

## Implementation Checklist

1. 后端类型与纯函数
   - 在 `internal/center/assetdecisions` 增加 comparison insight 类型。
   - 新增或扩展纯函数派生成员 lane、rank、strength/risk/gap/tradeoff。
   - 复用现有 `EvidenceChip` / recommendation reason 风格，避免重复 tone/kind 结构。
   - 把 group/member comparison insight 接入 `buildMember`、`buildGroup`、manual group 当前 facts 回读。

2. Snapshot 与兼容
   - 更新 `RecordSnapshotFromGroup`、`RecordSnapshotFromMember` 保存 comparison insight。
   - 确保旧 snapshot 缺字段时不影响 readback、execution plan、record detail。

3. Store / handler / API contract
   - 确认 `ListGroups`、`GetGroup`、manual groups list/detail/create/patch/member operations、records create/detail/list/patch 返回新增字段。
   - 不新增 SQL 表或 migration。
   - 不新增 endpoint，不调用 runtime facts detail。

4. 前端类型与展示 helper
   - 在 `web/src/lib/types.ts` 增加 comparison insight 类型。
   - 在 `AssetDecisionsPage.tsx` 增加解析旧 snapshot 的 helper。
   - 抽出矩阵展示 helper，复用到自动组详情和自定义组合详情。
   - 限制 chips 数量，保证移动端不横向溢出页面主体。

5. 页面 UX
   - 组卡新增比较结论 / priority VPS / lane counts。
   - 自动组详情新增 `EVIDENCE MATRIX / 证据矩阵`，宽表保留在其后。
   - 自定义组合详情新增同款矩阵，并展示 intended action 与当前证据对照。
   - 记录详情新增 `SAVED EVIDENCE / 保存时依据`，只读展示 snapshot comparison insight，旧记录降级。
   - 保持续费 evidence 和单台队列的辅助层级。

6. 测试
   - Go domain tests：lane 分类、rank、tradeoffs、source unavailable、current fact missing、禁止 IP/路由/性能语义。
   - Go store tests：list/detail/create/patch 返回 comparison insight，snapshot 保存字段，不新增 runtime facts detail。
   - Handler tests：records/groups/manual groups 响应包含字段，错误合同不变。
   - Frontend tests：组卡比较结论、自动组矩阵、自定义组合矩阵、记录 snapshot 回看、旧 snapshot 降级、不触发业务写请求。
   - API helper/types fixture 测试覆盖新增字段。

7. Spec 更新
   - 更新 backend database guideline、web state-and-data、v2 component spec。

8. Visual sanity
   - 启动本地 dev server。
   - 用 in-app Browser 检查 `/asset-decisions?view=needs_decision&renew_within_days=30`。
   - 检查桌面与移动端：主工作台、组卡、组详情矩阵、自定义组合详情矩阵、记录详情保存时依据、宽表 scroll 不造成页面横向溢出。

## Validation Commands

后端：

```bash
go test ./internal/center/assetdecisions ./internal/center/store ./internal/center/http/handlers
```

前端：

```bash
cd web && npm test -- AssetDecisionsPage api
cd web && npm run typecheck
```

全局质量门按实际耗时选择：

```bash
make test
```

Trellis / git：

```bash
python3 ./.trellis/scripts/get_context.py
python3 ./.trellis/scripts/task.py current
git status --short --branch
```

## Review Gates

- 任何新增 comparison 字段必须能从现有 facts 与 evidence 派生。
- 页面不得新增直接业务写接口；允许的写入仍只有 manual group/template/record followup 和单台 VPS renewal decision 的既有路径。
- 新文案不得暗示已支持 IP 质量、路由质量、性能趋势、CPU/IO 或超售判断。
- 如果需要大量拆分 `AssetDecisionsPage.tsx`，先只抽出页面内纯 helper / 小组件，避免本阶段演变成大规模文件重构。

## Rollback Points

- 如果后端 contract 变更造成测试面过大，可降级为前端只读展示现有 `evidence_assessment` / `decision_recommendation` 的矩阵，但必须记录为 scope downgrade。
- 如果移动端视觉无法在当前 CSS 下稳定，可先保留矩阵为纵向成员卡片，不强行做复杂表格。
- 如果 snapshot 兼容复杂度过高，优先保证新记录写入；旧记录只显示已有 evidence assessment 降级文案。
