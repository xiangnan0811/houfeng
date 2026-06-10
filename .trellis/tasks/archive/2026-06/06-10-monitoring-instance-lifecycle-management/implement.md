# 监控实例生命周期管理实施方案

## Execution Rules

- 当前任务在 `feature/monitoring-instance-lifecycle-management` 非 main 分支实施。
- 先写失败测试，再写实现；每个行为改动至少有一个红绿验证。
- 如果实现中发现设计遗漏，先更新 `prd.md` / `design.md` / `implement.md`，再继续代码。
- 任务完成前可以本地 commit；push 和 PR 必须等 Trellis 任务完成并归档后再进行。
- 不直接修改 local/remote `main` 或 `master`。

## Ordered Checklist

### 1. 启动任务和加载规范

- [ ] 运行 `python3 ./.trellis/scripts/task.py start .trellis/tasks/06-10-monitoring-instance-lifecycle-management`。
- [ ] 使用 `trellis-before-dev` 加载任务 artifacts、package 列表、backend/web/guides spec。
- [ ] 记录需要遵守的后端数据库、错误处理、质量规范，以及前端数据和组件规范。

### 2. 数据模型和类型

- [ ] 写迁移/scan 失败测试：`monitoring_instances` 新增 `archived_at`、`archived_reason` 后，未归档实例 JSON 包含空归档状态且查询可扫描。
- [ ] 添加数据库迁移。
- [ ] 更新 `monitoringinstances.Record`、select columns、scan 函数和相关 fixture。
- [ ] 新增管理 review、action input、list scope、错误类型和校验函数测试。
- [ ] 实现类型和校验函数。

### 3. 列表 scope

- [ ] 写 store 测试覆盖 `active`、`archived`、`all` scope。
- [ ] 将 repository `ListMonitoringInstances(ctx)` 改为接收 scope/filter，默认 active。
- [ ] 更新 handler 解析 `scope` query，非法 scope 返回 `400`。
- [ ] 更新 dashboard/页面调用方，保持默认 active 行为。

### 4. 管理审查

- [ ] 写 store 测试构造实例、VPS 链接、心跳、样本、探测、聚合、IP 质量、事件、通知、生命周期步骤，断言 review counts、warnings、blockers 和 allowed actions。
- [ ] 新增 repository 方法 `GetMonitoringInstanceManagementReview`。
- [ ] 复用或补齐 VPS 链接 summary 查询。
- [ ] 新增 handler `management-review`，更新 router/bootstrap。

### 5. 生命周期和归档操作

- [ ] 写 retire 测试：状态变为 `已退役 + 暂停`，tokens 和 pending action 清空，归档实例被拒绝。
- [ ] 写 restore lifecycle 测试：仅退役实例可恢复到 `观察中 + 暂停`。
- [ ] 写 archive 测试：事务内 review 阻塞活跃 VPS、要求确认名、成功后设置归档字段并撤销 token/action。
- [ ] 写 restore-from-archive 测试：清空归档字段，恢复为 `观察中 + 暂停`。
- [ ] 实现 store 方法和 handler。
- [ ] 确保 metadata update、runtime resume、onboarding/binding/action 对归档或不允许状态返回 `409`。

### 6. 永久清理

- [ ] 写 cleanup 测试：空误创建实例即使有活跃 VPS link 也可删除，link 随实例级联删除。
- [ ] 写 cleanup 阻塞测试：有观测/历史/事件/通知/动作引用且未归档时返回 blocked。
- [ ] 写 cleanup 删除测试：归档非空实例确认后删除非 FK 引用，再删除实例本体，无孤儿引用。
- [ ] 实现事务内 lock、review 重算、确认名校验、非 FK 引用删除、实例删除和结果返回。
- [ ] 前端 permanent cleanup 成功后导航回列表并提示结果。

### 7. 同步和 agent plan 门禁

- [ ] 写 sync 测试：暂停实例收到带心跳/样本/探测/IP 质量的 batch 时返回空 plan 且没有新增数据。
- [ ] 写 sync 测试：退役实例同样不写入新数据。
- [ ] 写 sync 测试：归档实例不写入新数据，token 被撤销或请求被拒绝。
- [ ] 更新 `validateAcceptedSyncBatch` 读取 monitoring status 和 archived_at。
- [ ] 在 `ApplyBatch` 中对暂停/退役短路到空 plan，跳过观测和 action result 持久化。
- [ ] 更新 `BuildSyncPlan` 对归档实例返回空 plan。

### 8. 前端 API 和类型

- [ ] 更新 `web/src/lib/types.ts`：归档字段、scope、review、actions、cleanup result。
- [ ] 更新 `web/src/lib/api.ts`：list scope、review、retire、restore、archive、restore archive、cleanup。
- [ ] 写 API/helper 或页面测试覆盖 endpoint、query 和 body。

### 9. 前端页面和组件

- [ ] 写 MonitoringPage 测试：默认 active，切换 archived/all，批量操作过滤归档实例。
- [ ] 实现列表范围切换和空态文案。
- [ ] 写管理面板测试：加载 review、显示 VPS 关联/计数/阻塞项、确认名校验、操作后刷新。
- [ ] 新增或改造详情页管理组件，复用 `ActionConfirmationModal`。
- [ ] 确保归档实例详情页能浏览但不能触发运行控制、接入或元数据编辑。

### 10. 质量门禁

- [ ] 运行后端目标测试：`go test ./internal/center/... ./cmd/...`。
- [ ] 运行前端测试：按项目规范运行 `npm`/`pnpm`/`vitest` 对应命令。
- [ ] 运行格式化和 lint/typecheck，如果项目配置存在。
- [ ] 手动核对关键页面或用浏览器验证：列表 scope、详情管理入口、确认弹窗无文字溢出。
- [ ] 修正所有失败后重新运行相关验证。

### 11. Trellis finish 顺序

- [ ] 使用 `trellis-check` 做最终质量审查。
- [ ] 如产生可复用约定，使用 `trellis-update-spec` 更新 spec。
- [ ] 本地 commit 代码和 Trellis 任务 artifacts。
- [ ] 运行 finish/archive 流程，将任务归档。
- [ ] 归档完成并提交后，才允许 push feature branch 和创建 PR。

## Validation Commands

实际命令以项目脚本为准，开发前通过 `package.json`、`go.mod` 和 Trellis spec 确认。初始验证命令：

```bash
go test ./internal/center/... ./cmd/...
```

```bash
npm --prefix web test -- --run
```

```bash
npm --prefix web run typecheck
```

```bash
npm --prefix web run lint
```

如果命令不存在，记录实际可用替代命令，并在最终说明里明确未运行项和原因。

## Risk Files

- `internal/center/store/monitoring_instances.go`：select/scan 字段顺序必须同步。
- `internal/center/store/sync_batches.go`：必须保证短路发生在任何观测/IP 质量写入前。
- `internal/center/http/router.go`：新子路由不能被 `{id}` catch-all 抢先匹配。
- `cmd/houfeng-center/bootstrap.go`：repository 接口扩展后要同步注入。
- `web/src/pages/MonitoringDetailPage.tsx` 和 detail body：避免重复请求和状态刷新遗漏。
- `web/src/lib/types.ts`：后端 JSON 契约变更必须同步。

## Rollback Points

- 数据模型迁移独立提交，若后续逻辑失败可保留字段不启用。
- 后端 API 和前端入口分阶段提交，前端可暂时隐藏管理入口但保留后端测试。
- 永久清理为最高风险功能，若验证不充分，最后保留 review/归档/恢复，暂不开放 cleanup endpoint；但本任务目标要求完整交付，只有遇到无法安全实现的事实阻塞时才采用该降级。

## Self Review

- 方案覆盖了用户指出的“不只是删除，其他管理操作也没考虑全面”：退役、恢复、归档、恢复归档、永久清理、关联查看、同步门禁和列表范围都包含在内。
- 方案避免把归档塞进 `lifecycle_status`，不会破坏已有 VPS 生命周期联动。
- 方案修复隐藏数据问题：暂停/退役不再写入新观测，归档撤销 token。
- 方案对永久删除设置 review、确认名、事务内重算和非空先归档，降低误删风险。
- 方案明确 push/PR 只能发生在 Trellis 任务完成并归档之后，修正此前流程错误。
