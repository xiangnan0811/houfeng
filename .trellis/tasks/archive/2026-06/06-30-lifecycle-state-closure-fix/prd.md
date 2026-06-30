# Fix lifecycle state closure gaps

## Goal

修复复审报告中确认的生命周期/状态闭环缺口，使 VPS 当前状态三元组、订阅赠送/抽奖语义、导入链路、测试覆盖和页面验证达到一致、无歧义、可回归的状态。本轮在当前分支实施修复，并在修复后再次审查确认没有引入新的状态问题。

## User Value

- 用户不能通过普通页面操作把仍在用的 VPS 标记成已替换等矛盾状态。
- 批量导入与页面录入一样能表达 `renewal_mode=gift` / `lottery`，避免“赠送”“抽奖”“手动续费”混淆。
- 归档、监控行政恢复、浏览器 mock 证据和测试命名与实际语义一致，减少维护者和用户理解成本。
- 因项目当前没有实际用户，可以做破坏性数据约束收口，但必须明确说明影响。

## Confirmed Facts

- 上一轮复审报告位于 `.trellis/tasks/archive/2026-06/06-30-lifecycle-state-closure-review/research/state-closure-final-review.md`。
- 复审确认两个真实问题：
  - 普通 VPS PATCH 只校验请求内字段组合，未校验“当前记录 + patch”的合成状态，可能写出 `renewal_decision=replaced` 与 `active/in_use` 共存。
  - JSON 导入的 `SubscriptionInput` 缺少 `renewal_mode`，且 decoder 禁止未知字段，导致导入链路不能表达 `gift`。
- 复审还确认三个低风险收尾项：
  - archived MonitoringInstance 行政恢复缺直接测试。
  - `ArchivePage.test.tsx` 测试名仍写 archived scope。
  - `scripts/visual_evidence.py` asset workflow fixture 没覆盖 `gift` / `lottery` 标签实际页面效果。
- 用户要求继续在当前分支处理这些问题，直到 goal 目标完美达成；修复完成后必须再次审查，发现问题要继续修复。

## Requirements

- 使用 TDD：先添加能暴露缺陷的失败测试，再实施生产代码修复。
- VPS PATCH 合成状态校验：
  - 普通 PATCH、带订阅联动的 PATCH、任何 store-level patch 都必须验证最终 `lifecycle_status / usage_status / renewal_decision` 三元组。
  - `replaced + active`、`replaced + in_use`、`cancelled + in_use`、`cancelled + 非取消决策`、`to_cancel + 非取消决策`、`to_migrate + 非 migrate` 等硬失败不能通过“只提交单字段 patch”绕过。
  - 继续保留普通 PATCH 对 `to_cancel/to_migrate/cancelled/archived` lifecycle 的禁止；受控 lifecycle/archive action 若使用下层 store，也必须符合最终三元组。
  - 可增加 DB 级跨列 check constraint 兜底；因为当前无用户，若这会阻塞已有矛盾数据，视为可接受的破坏性收口，并在报告中说明。
- JSON 导入 renewal mode：
  - `subscription.renewal_mode` 在导入 JSON 中合法，支持 `auto|manual|auto_cancelled|lottery|gift|bonus|other`。
  - `gift` 和 `lottery` 导入后 legacy auto-renew flags 均归一为 `false,false`。
  - 非法 renewal mode 产生导入 validation error；未知字段仍被拒绝。
  - 示例数据或 mock fixture 至少覆盖 `gift` / `lottery` 的一个实际用户可见路径。
- Low-risk closure：
  - 增加 archived MonitoringInstance 行政恢复测试。
  - 修正 ArchivePage 测试描述，使 scope 命名与 `historical` 一致。
  - 更新 visual evidence fixture 覆盖 `gift` / `lottery`，并确保 browser sanity 能覆盖相关页面。
- 更新 `.trellis/spec/` 中相关可执行合同，避免未来再次漏掉 merged-state validation 或导入字段同步。
- 修复后执行 targeted tests、全量质量门、browser sanity，并写复审报告。

## Acceptance Criteria

- [ ] 新增回归测试在修复前能失败，修复后通过。
- [ ] 普通 VPS PATCH 不能写出任何已知硬失败三元组；尤其 `active/in_use + renewal_decision=replaced` 返回 invalid input。
- [ ] 导入 JSON 支持 `subscription.renewal_mode=gift`，report/import candidate 能保留该事实；非法 mode 被拦截。
- [ ] 订阅页面或相关浏览器 mock 数据能实际展示 “抽奖” 和 “赠送” 的区分。
- [ ] archived MonitoringInstance 行政恢复有直接测试覆盖，且无通知发送。
- [ ] ArchivePage 测试命名与 `historical` scope 一致。
- [ ] DB / domain / store / API / frontend / import / specs 状态语义保持一致。
- [ ] 全量 `make verify-go`、`make verify-web`、`git diff --check` 通过；browser sanity 覆盖资产和观测相关路由。
- [ ] 修复后有独立复审报告，确认上一轮和本轮发现的问题均已闭环或明确剩余风险。

## Notes

- 本轮允许破坏性 DB constraint 收口；必须在复审报告中说明会阻塞已有不合法数据。
- 不在本轮移除 `asset_scope=archived` 兼容别名，除非修复过程中发现它造成真实矛盾。
