# Current Branch Multi-Task Review

日期：2026-06-30  
分支：`audit-state-lifecycle-consistency`  
基线：`origin/main` / `c22fa8727c65e0e3db51c723d27f730d2f400056`  
范围：`origin/main...HEAD` 的 12 个提交、96 个文件变更。  

## Findings

### Medium: 监控阈值允许倒序配置，接入 settings 后会造成告警和图表语义混乱

证据：

- 后端 `validateIncidentDefaults` 只把 CPU/Mem/Disk/Inode/IOWait 阈值限制在 `1..100`，Load5 限制为正数；没有校验 `warning < alert < critical` 或 `warning < critical`：`internal/center/settings/types.go:307`。
- 前端 Settings 页提交时也只是逐项解析正整数/正数，没有做层级顺序校验：`web/src/pages/SettingsPage.tsx:150`。
- 本分支把前端阈值线和列表趋势接入 settings，并用 IOWait/Load5 的 warning/critical 中点派生 alert：`web/src/config/thresholds.ts:53`。
- 后端 evaluator 现在按 `critical -> alert -> warning` 的顺序使用这些阈值：`internal/center/incidents/evaluator.go:421`。

影响：

- 用户可以保存 `CPU 关注=95 / 告警=80 / 严重=90` 这类配置。后端会接受并持久化，前端也会展示倒序阈值线。
- evaluator 会按代码顺序先命中 critical，再命中 alert；这会让“关注阈值”在部分区间失效，用户看到的设置文案与实际告警等级不一致。
- IOWait/Load5 若配置 `warning > critical`，前端会派生一个介于二者之间但语义反向的 alert；后端 `midpointInt/midpointFloat` 在 `high <= low` 时把 alert 设为 warning，进一步放大歧义。

建议修复：

1. 后端 `settings.Validate` 增加阈值顺序校验：
   - CPU/Mem/Disk/Inode: `warning < alert < critical`。
   - IOWait/Load5: `warning < critical`，派生 alert 保持在中点。
2. Settings 页在 `buildIncidentDefaults` 或提交前做同样校验，给出用户可理解错误，例如“CPU 阈值必须满足 关注 < 告警 < 严重”。
3. 给 `internal/center/settings/types_test.go`、`web/src/pages/SettingsPage.test.tsx`、`web/src/config/thresholds.test.ts` 补倒序拒绝测试。
4. 如果需要允许相等阈值表达“跳过某一级”，必须显式设计并同步 UI 文案；当前文案是三层递进，不应接受相等或倒序。

## Confirmed Closed

- VPS `lifecycle_status` / `usage_status` / `renewal_decision` 已有 domain、store merged-patch 和 DB check constraint 三层兜底。普通 PATCH、带订阅联动 PATCH、受控取消路径和 create/import 路径均覆盖了关键矛盾组合。
- `ApplyVPSCancellation` 在取消到 `cancelled` 时会把 `in_use` VPS 改为 `idle`，避免被新 DB constraint 拦截，并在 lifecycle step 中记录 usage before/after。
- `subscription.renewal_mode=gift` 已贯通 DB constraint、Go domain、price history、JSON import、dry-run report、前端类型和标签；`lottery` 和 `gift` 展示语义已拆开。
- `asset_scope=historical` 已贯通 VPS/subscriptions handler、store、前端 API/types 和 Archive 页面；`archived` 作为旧别名保留且行为一致。
- 暂停、维护、退役、归档 MonitoringInstance 以及暂停/归档 Target 的 active incident 会行政恢复，不产生通知风暴。
- 迁移相关生产文案已降级为“迁移意向/人工跟进”，没有暗示已存在完整迁移 workbench。

## Destructive / Compatibility Notes

- `0049_vps_asset_state_combination_constraint.sql` 是破坏性数据完整性收口：已有矛盾 VPS 三元组会阻塞迁移。当前项目没有用户，因此 fail-fast 合理；如果未来有真实数据，需要先审计/清洗再执行。
- `asset_scope=archived` 仍保留为兼容别名。当前无用户时可以选择移除，但保留别名不会产生运行时歧义，且已写入 spec。
- Load5 默认告警敏感度从旧 evaluator 硬编码 `1.2/1.8/2.5` 对齐到 Settings 默认 `4/6/8`。这是行为变化，但任务设计中已经明确以 Settings 合同为权威；建议在 release note 中说明。

## Verification Notes

- 本次为 review-only，没有修改业务代码，也未重新跑完整 verify。
- 已审查差异范围、关键后端状态链路、迁移、前端阈值/归档/订阅展示、导入链路、visual evidence fixture、Trellis task reports 和 spec 更新。
- 之前任务报告记录的验证包括 `make verify-go`、`make verify-web`、`git diff --check`、targeted Go/Vitest、browser sanity；本次审查未发现这些验证结论之外的测试失败证据。
