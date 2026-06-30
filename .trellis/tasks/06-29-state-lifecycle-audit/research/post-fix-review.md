# 状态生命周期修复后复审

日期：2026-06-30  
范围：本任务实施后的 SLC-01 到 SLC-12 复审、剩余风险和验证证据。  
结论：本轮已修复低风险且高确定性的跨层状态/阈值缺陷；需要完整产品设计的新流程仍拆为后续任务。

## 总览

| 编号 | 状态 | 复审结论 |
| --- | --- | --- |
| SLC-01 | 已修复 | 订阅 `renewal_decision` 筛选改用 VPS renewal decision 合同，`migrate` 合法、`manual` 非法。 |
| SLC-02 | 已缓解 | 普通 VPS PATCH 不再允许写入取消、迁移、归档等受控 lifecycle 流程/终态。 |
| SLC-03 | 已缓解 | 迁移执行计划降级为“标记迁移意向并人工跟进”，不再承诺 VPS 详情内推进迁移。 |
| SLC-04 | 部分缓解 | SLC-02 收住最高风险写入口；完整多轴状态矩阵和冲突 UI 仍需后续任务。 |
| SLC-05 | 已修复 | 前端监控阈值默认值、三层级、settings 解析和图表/趋势显示已对齐后端合同。 |
| SLC-06 | 已修复 | IOWait/Load5 evaluator 接入 settings，并派生 alert 层级。 |
| SLC-07 | 已修复 | heartbeat stale severity 使用 settings 的 stale threshold intervals。 |
| SLC-08 | 已缓解 | 暂停、维护、已退役监控实例不会由 heartbeat stale sweep 产生新的 active incident。 |
| SLC-09 | 已修复 | IP 质量 stale window 新增 settings 字段，迁移、API、前端类型和设置页同步。 |
| SLC-10 | 后续任务 | 抽奖、赠送、Bonus/余额抵扣仍需产品级字段拆分，不在本轮半实现。 |
| SLC-11 | 文档型风险 | API `asset_scope=archived` 命名风险仍存在，但当前页面文案准确；建议后续兼容新增 `historical`。 |
| SLC-12 | 已缓解 | 可见文案改为迁移意向/人工跟进，完整迁移 workbench 仍是后续产品任务。 |

## 已修复或已缓解项

### SLC-01：订阅筛选枚举合同

- 修复点：`internal/center/subscriptions/types.go` 的 `RenewalDecision` normalization/validation 使用 `vpsassets` renewal decision 语义。
- 测试：`internal/center/subscriptions/types_test.go` 覆盖 `migrate` 合法、`manual` 非法；handler 测试覆盖 query 不再 400。
- 用户影响：订阅页和未来深链可按 VPS 决策查看取消/迁移相关订阅，不再被订阅 renewal mode 枚举误拦截。

### SLC-02 / SLC-04：VPS 普通 PATCH lifecycle 边界

- 修复点：新增 ordinary PATCH lifecycle 合同，只允许 `active`、`idle`、`testing`；`to_cancel`、`cancelled`、`to_migrate`、`archived` 必须走受控流程或专用 endpoint。
- 测试：domain 与 handler 测试覆盖允许/拒绝边界。
- 剩余风险：完整状态组合矩阵尚未实现，例如 lifecycle、usage、renewal decision 之间的冲突组合和 UI 告警仍需专门产品/技术任务。

### SLC-03 / SLC-12：迁移文案收口

- 修复点：asset decision execution plan 的 migrate step label 改为“标记迁移意向并人工跟进”；VPS 详情生命周期联动文案不再写“迁移流程”；测试 fixture 移除“推进迁移”噪声。
- 测试：后端执行计划测试断言不得包含“推进迁移”；前端 `VPSDecisionBoard` 测试覆盖取消相关状态下文案显示“迁移意向”且不显示“迁移流程”。
- 剩余风险：系统仍没有完整迁移 workbench、source/target VPS 关系、cutover step、旧机 closure 和回滚记录。

### SLC-05 / SLC-06 / SLC-07：监控阈值对齐

- 修复点：
  - 前端 `DEFAULT_THRESHOLDS` 改为 warning/alert/critical 三层，并与后端默认 CPU/Mem/Disk/Inode 对齐。
  - `resolveThresholds` 读取 settings 响应；IOWait/Load5 派生 alert 中点。
  - 监控列表 trend cell、监控详情 watchtower metrics 接受同一 thresholds props。
  - 后端 evaluator 接入 settings 的 IOWait/Load5 与 heartbeat stale threshold。
- 测试：前端阈值解析、trend cell、watchtower metrics、Monitoring list/detail 页面；后端 incidents evaluator/service tests。
- 用户影响：设置页阈值、后端告警判断、监控图表和列表趋势的解释口径一致。

### SLC-08：非运行态 heartbeat sweep

- 修复点：incident service stale sweep 跳过暂停、维护、已退役监控实例，避免生成新的 heartbeat stale active incident。
- 测试：service tests 覆盖暂停、维护、已退役实例不会产生新 heartbeat incident。
- 剩余风险：进入暂停/退役/归档时，已有 active incident 的收敛策略仍需后续任务决定是 recovered、suspended 还是保留历史提示；Target pause/archive 的 probe/resource incident 收敛也未在本轮完成。

### SLC-09：IP 质量 stale 配置化

- 修复点：新增 `ip_quality_settings.stale_after_seconds`，默认 604800；validation 要求不小于采集周期；迁移回填 settings JSON 并重建 IP 质量 read models；Go/TS 类型、Settings 页面和 API 测试同步。
- 测试：settings defaults/validation、settings store round-trip、migration read model、handler settings、Settings 页面和 API client。
- 用户影响：IP 质量过期判断不再只能固定 7 天，后续可以和采集频率形成一致合同。

## 后续任务建议

1. 完整 VPS lifecycle 状态矩阵与审计：定义 lifecycle、usage、renewal decision 的合法组合、warning 组合、禁止组合；归档/恢复、取消/退役、迁移、替换全部进入统一 action/step 审计。
2. 迁移 workbench 产品任务：新增 `migrate_vps` action type、source/target VPS 关系、服务/domain/Target/Monitoring cutover step、readback、旧机 closure 和回滚记录。
3. Active incident 收敛任务：定义 MonitoringInstance pause/maintenance/retire/archive 以及 Target pause/archive 对已有 active incident 的 recovered/suspended/保留策略，并让页面可解释。
4. 订阅来源拆分任务：将“续费方式”和“费用/权益来源”拆开，明确抽奖、赠送、Bonus/余额抵扣的字段、筛选、统计和 UI label。
5. `asset_scope` 命名兼容任务：保留 `archived` scope 兼容，同时新增 `historical` 或 `inactive`，文档说明包含 `cancelled` 与 `archived`。

## 验证记录

- `go test ./internal/center/assetdecisions`：通过。
- `cd web && npm test -- --run src/pages/AssetDecisionsPage.test.tsx src/pages/VPSDetailPage.test.tsx src/pages/vps-detail/VPSDecisionBoard.test.tsx`：3 个文件、55 个测试通过。
- `go test ./internal/center/subscriptions ./internal/center/vpsassets ./internal/center/incidents ./internal/center/settings ./internal/center/store ./internal/center/http/handlers ./internal/center/assetdecisions`：7 个包通过。
- `cd web && npm test -- --run src/pages/MonitoringPage.test.tsx src/pages/MonitoringDetailPage.test.tsx src/pages/SettingsPage.test.tsx src/pages/AssetDecisionsPage.test.tsx src/pages/VPSDetailPage.test.tsx src/pages/SubscriptionsPage.test.tsx src/pages/vps-detail/VPSDecisionBoard.test.tsx src/config/thresholds.test.ts src/pages/monitoring/MonitoringInstancesTrendCell.test.tsx src/components/monitoring-detail/MonitoringInstanceWatchtowerMetrics.test.tsx`：10 个文件、156 个测试通过。
- `cd web && npm run build`：通过。
- `cd web && npm run lint`：通过。
- `make verify-go`：通过。
- `python3 -m unittest scripts/test_visual_evidence.py`：6 个测试通过，覆盖 mock API 的 Settings fixture、资产工作流和监控支持 fixture。
- `TMPDIR="$PWD/.tmp/playwright" uv run --with playwright python scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --mock-api asset-workflows --route /asset-decisions --route /vps --route /vps/vps_fra_legacy --route /subscriptions --route /settings --viewport 1440x1000 --viewport 390x900 --timeout-ms 30000`：全部通过，无页面级横向溢出；长数字/摘要存在 helper warning，但不阻断页面可用性。
- `TMPDIR="$PWD/.tmp/playwright" uv run --with playwright python scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --mock-api observability-support --route /monitoring --route /monitoring/mi_hkg_edge_01 --route /targets --route /events --viewport 1440x1000 --viewport 390x900 --timeout-ms 30000`：全部通过，无页面级横向溢出；监控表格和 Target 卡片长字段存在 helper warning，属于表格/长文本扫描提示。
- `rg -n "打开 VPS 详情推进迁移|推进迁移|迁移工作台|迁移流程" internal web/src --glob '!**/*_test.go' --glob '!**/*.test.tsx' --glob '!**/*.test.ts' -S`：生产代码无匹配。

## 页面实际效果复审

- 预览 URL：`http://127.0.0.1:5178/`，验证后已停止本地 Vite dev server。
- 数据源：`mock-api asset-workflows` 与 `mock-api observability-support`，覆盖资产决策、VPS 列表、VPS 详情、订阅、设置、监控列表、监控详情、Target、事件。
- 视口：`1440x1000` 与 `390x900`。
- 工具：通过 `uv run --with playwright` 提供本地 Python Playwright，并下载 Playwright Chromium 到用户缓存；未修改 `web/package.json`，未把浏览器自动化加入 CI。
- 结果：核心状态页面均能渲染主工作流，无 blank viewport、无页面级横向溢出、无 mock API visible error marker。helper 对部分数字、长位置/摘要字段报告 overflow-risk warning；这些位于表格/卡片局部，不影响本轮阈值、迁移文案和 settings 状态合同的可用性。
