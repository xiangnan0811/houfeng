# 收紧 Legacy operation 与 refresh 归属

## Goal

将 Legacy VPS detail 的异步写入 pending 状态归属到精确的 VPS/token/generation/operation，并让 detail/services/domains 二阶段刷新在 state commit 前校验 generation owner，消除跨 VPS 阻塞、旧 finally 清新状态及 A→B→A 覆盖。

## Requirements

1. 每个异步写入 owner 包含 `vpsId`、唯一 token、mutation generation 和 operation；同一 VPS 同时最多一个写入，不同 VPS 写入互不阻塞。
2. 所有网络写入的 submitting/disabled UI 从当前路由 VPS 的 owner 派生，不再由组件级布尔值表示。A pending 时切到 B，B 可提交；返回 A 时仍显示 A pending，直到 A 自己完成。
3. `finishVpsWrite` 仅在 VPS 和 token 同时匹配时删除 owner；旧请求的 `finally` 不能清除另一个 owner。
4. link、monitoring create、subscription、validity extension、unlink monitoring、lifecycle/archive、cancellation、experience、service、domain、facts、decision 全部使用同一 ownership boundary。
5. 每个 post-await notice/error、draft reset、drawer collapse、navigate、refresh 都必须先通过该 mutation generation 的 current 检查。
6. `refreshDetail`、`refreshServices`、`refreshDomains` 在 functional state commit 内校验目标 VPS 和调用方传入的 generation owner；默认 route-load refresh 仍可正常工作。
7. 路由切换使 mutation response generation 失效，但不得删除 transport owner；已经发送的服务端写入不被客户端假装取消。
8. 通过 controlled deferred tests 确定性覆盖跨 VPS并发和 A→B→A ABA，不使用 sleep。
9. VPS route effect 的正常与 early-return cleanup 都必须失效 mutation response generation 和 latest-load authority，同时保留 transport owner 直到 promise settle。
10. 关闭 cancellation Drawer 必须递增 preview generation；关闭前 pending 的普通 preview 不能提交，重开必须重新获取当前 preview。
11. 生产 write-owner store 必须由持续挂载的 `VPSDetailPage` page shell 持有并注入 Legacy；capability probe 对 Legacy 的 remount 不得重置 transport owner。禁止用 module-global singleton 掩盖生命周期问题。
12. 正常 route load 必须捕获复用的 mutation generation；same-VPS 旧 load 的 terminal navigation、payload、catch 与 functional state setter 不能覆盖后继 mutation/refresh 或 reload。
13. cancellation deep-link route preview 必须捕获 preview generation；旧 route preview 不能覆盖更新的普通 preview。非 cancellation route payload 在清空/switch Drawer 前必须失效期间打开的 preview。
14. same-VPS stale-success 回归必须让旧 route detail 先通过最早 guard，再延迟二阶段请求；另以受控调度让 payload outer guard 已通过但 functional updater 执行前 generation 失效，分别证明两个 commit guard。
15. 返回仍持有 inherited transport owner 的 VPS 时，page route effect 必须在 probe 前读取/订阅 exact token；若返回页 GET 先完成而旧 POST 后 settle，仅当前 route 仍为该 VPS时触发一次权威 re-probe。旧 A settle 不得触发当前 B probe。
16. ordinary cancellation preview 在 await 前取得 generation；双 deferred 证明 A2 pending 已夺权。非 cancellation route payload 必须同时清除已提交 preview、error/result 与 cancellation attention，而非只拒绝迟到 response。

## Out of scope

- archive eligibility review GET 的 modal request ownership；由独立子任务处理。
- 中止 fetch、改变后端写入语义、允许同一 VPS 的危险重复提交。
- 把 Legacy 页面整体迁移到新的状态管理框架。

## Acceptance Criteria

- [x] A 上 service POST pending 时切 B，B 的 service 表单可用并可发起请求；A settle 不得清除、关闭或覆盖 B；回到 A 时 pending 归 A 自己持有。
- [x] subscription 表单具有同样的跨 VPS owner 行为，避免只修 service 特例。
- [x] 每个异步写入 handler 都通过 `beginVpsWrite(vpsId, operation)` 取得 owner，并在 finally 使用精确 owner 完成；没有遗留无条件 `set*Submitting(false)`。
- [x] detail、services、domains 各有 A→B→A deferred test，旧 refresh 响应不能覆盖新 generation 的 A 状态。
- [x] 现有 facts/decision CAS、通知、drawer、navigation 和同 VPS duplicate-submit 行为保持通过。
- [x] focused `LegacyVPSDetail.test.tsx`、web lint/type/coverage/build gates 通过。
- [x] query-skip effect 后启动 deferred archive write，再卸载；迟到 success 不导航，owner 仅由自己的 finally 释放。
- [x] pending 或已加载 cancellation preview 在 workbench 按钮 / Modal X close 后失效，reopen 发出新 GET 并只显示新 blockers/warnings。
- [x] 真实 `VPSDetailPage` 入口中 A subscription pending → B → A-before-settle：A 仍显示 pending 且只有一个 POST/idempotency key，B 可独立提交；A settle 后才解锁。
- [x] same-VPS route A1 pending 后由 mutation/refresh 或 reload A2 先提交；A1 payload/error/functional commit/navigation 全部失效。
- [x] cancellation deep-link route preview A1 不覆盖 A2；reload payload reset 后普通 preview 的 late settle 不改变页面 attention/state。
- [x] stale route success 的二阶段 deferred 用例在删除 outer payload guard 时 RED；commit-time updater interleaving 在删除 inner functional-setter guard 时 RED，且两者都不靠 sleep/source-only assertion。
- [x] 真实入口返回 A 的旧 GET 先完成、POST 后 settle 时自动重载权威 subscription/service/lifecycle；A settle 时当前 B 的 probe count 不变。多 remount 用例只使用 scoped timeout。
- [x] route preview A1/A2 双 deferred 按 A2 pending → A1 → A2 settle；payload reset 同时覆盖 late preview 与已提交 preview/error/result/attention 的反向顺序。

## Constraints

- 复用现有 `mutationGenerationRef` 和 per-VPS write lock 语义，不建立相互竞争的第二套 response generation。
- owner state 必须能表示 A 与 B 同时各有一个 pending；单个全局 current owner 不满足要求。
- UI 中文提示与现有交互语义保持不变。
