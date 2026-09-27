# VPS Detail 异步所有权合同

> 本文件维护 VPS 详情写操作、mutation refresh 与结束使用归档预览的异步所有权合同。页面组成调整不能削弱跨对象、重复点击和迟到响应保护。旧取消工作台已移除，不构成另一个生命周期入口。

## Scenario: Mutation transport 与 view commit 所有权

### 1. Scope / Trigger

- 修改 authenticated `AppShell` 的 VPS write registry、`VPSDetailPage` capability gate、详情视图 的任一网络写操作、same-VPS query reload、create retry、409 recovery、Drawer 生命周期、mutation 触发的权威 refresh，或跨 `vpsId` 的异步提交时适用。
- 写操作 inventory 覆盖资料、续费意向、有效期、自动续费核对、监控创建/重新接入/退役、订阅、结束使用归档/恢复、服务/域名关联、维护、跟进事项和历史补录；内部 operation 名称不构成旧 API 的兼容授权。

### 2. Signatures

- View generation: `mutationGenerationRef: MutableRefObject<number>`；`mutationIsCurrent(generation: number): boolean` 只比较 generation。
- Transport owner store: `VPSWriteOwnerStore { getSnapshot, subscribe, begin, prepareCreate, finishCreate, finish }`。公开 snapshot 持有 `Map<vpsId, { vpsId, token, viewToken, generation, operation, startedAt, monitoringInstanceId? }>`；digest/key attempt material 只留在 registry 私有 map 与 `prepareCreate` 的 exact-owner 返回值，不发布到 external-store snapshot。`finish*` 仅在 `vpsId + token` 精确匹配并释放时返回 `true`，stale token 返回 `false`。
- Production lifecycle: `AuthenticatedAppShell key={user.user_id}` 内只挂一份 `VPSWriteRegistryProvider` 并包住 `<Outlet />`；详情视图 与 Overview 都消费该 user-scoped store。直接挂载 page/component 的测试兼容入口才允许实例内 fallback store；禁止 module-global singleton。
- Owner lifecycle: `beginVpsWrite(vpsId, operation, monitoringInstanceId?): VPSWriteOwner | null` 委托 store 获取 owner；同 VPS 已有在途写入时返回 `null`，调用方必须在解引用前结束本次提交。`finishVpsWrite(owner)` 委托 store 精确释放并根据其 boolean 结果判断自己是否真正完成该 transport owner。
- Mutation refresh: `refreshDetail` / `refreshServices` / `refreshDomains` 接收 `ownsRefresh: () => boolean`，在 await 后和 functional state setter 内都检查 VPS identity 与 predicate。
- Route load owner: 正常 route-load entry 从 `mutationGenerationRef` 捕获唯一 `routeGeneration`；`routeIsCurrent()` 同时要求 effect 未 cancelled、generation 仍 current、目标 `vpsId` 仍是当前 view。payload、catch、terminal navigation 与 functional state setter 都必须在提交点检查该 owner。
- Settle convergence owner: 每个实际 gate/view 有唯一 `viewToken`。当前 page 若继承同 VPS 的不同-token owner，记录 exact token；该 owner 从 snapshot 消失后只触发一次权威 re-probe。详情视图 的 stale-generation callback 还必须校验当前 `vpsId + viewToken`；当前 view 正常 settle 由自己的 mutation refresh 收敛，不额外 probe。

### 3. Contracts

- generation 控制 view commit，owner 控制 transport lifetime。route/Drawer invalidation 使旧 notice、draft、drawer、navigate、preview/result 与 refresh commit 失效，但不得提前清除仍在途的 transport owner。
- `VPSDetailPage` 在参数变化时会进入 `probing` 并卸载 详情视图 子树；生产 transport store 必须归属于 user-keyed authenticated AppShell，不能归属于 page/gate 重挂载的实例，也不能跨用户保留。
- 任一 write owner settle 时，若其 owning 详情视图 view generation 已因 remount、Drawer close 或 same-VPS query reload 失效，且 page shell 仍 mounted/current VPS 仍匹配，必须触发一次权威 re-probe/reload。该 reload 用来收敛旧 详情视图 generation 已拒绝的 POST 结果；不得允许旧 target closure 在用户已切到另一 VPS 时触发其 re-probe，也不得给仍由当前 view 正常提交/refresh 的写入增加重复 probe。
- route effect entry 先失效 archive review request。该 effect 的正常路径、`!vpsId` early return 与 query-driven reload skip early return 都必须注册同一 authority cleanup：失效 archive request、递增 mutation generation，并释放 latest-load view lock。正常路径还取消本次 route load。
- 正常 route load 是一个 generation-owned view transaction，而不是只依赖 effect-local `cancelled`。同 VPS reload A1 pending 后，任何 A2 reload、mutation 或 mutation-owned refresh 都会推进 generation；A1 的 payload、catch、functional state commit 或 terminal navigation 随后必须被丢弃。
- 组件卸载或 route 切换后，旧 mutation 即使成功也不得更新 UI 或导航；其 `finally` 仍必须用 exact token 释放自己的 transport owner。
- 详情视图 owner 按 VPS 隔离、同 VPS 互斥。A pending 不阻止 B；A 的迟到 `finally` 不得释放 B 或同 VPS后继请求。
- 同 VPS 任一 operation 已有 owner 时，详情视图 的所有写入 submit/confirm/link/unlink 控件都必须从同一 snapshot 派生 blocked 状态；handler 仍须消费 `begin(...)` 的 nullable 返回值立即结束，不能只靠按钮禁用。
- subscription、experience、service、domain、monitoring create 先取得 provisional owner，再对 path VPS ID + operation + 实际 wire body 做 canonical SHA-256；transport/unconfirmed failure 保留相同 digest/key，body 变化或 `idempotency_key_reused` 轮换，confirmed success 清除。raw body 不进入 registry、UI、日志、错误或测试 snapshot。
- `finishVpsWrite` 的 release identity 是 `vpsId + token`。`generation`、`operation` 与 stable monitoring identity 用于 commit/UI 语义，不是额外的 release key；不要把未实现的“全部字段匹配”写成合同。
- mutation refresh 的 predicate 必须在 functional setter 内重检，阻止 A→B→A 的旧 A response 覆盖当前 A。
- 409 recovery 先加载最新版；facts 三方 merge，decision 保留本地 decision/reason，并用新 `updated_at` 重试。terminal identity 只有 generation 仍 current 时才 replace 到 `/archive/:vpsId`。
- load-latest 使用独立 in-flight lock；并发点击只发一个 GET，关闭、route 切换或卸载后迟到结果不得提交。
- 归档预览过期后仅刷新预览，保留确认名称与原因草稿，重新展示最新影响和阻止项。不得把 GET 后的新 digest 自动用于重发旧确认；条件变化后必须重新确认。
- archive 409 带 review 时直接采用最新 blocker_details；缺 review 再 GET，失败不能推定 eligible。恢复、人工迁移跟进、服务/域名关联结束与显式重新接入都受同一 VPS transport owner/view generation 约束。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| query-skip effect 后启动 mutation，再卸载组件 | cleanup 使 mutation generation 失效；迟到 response 不更新、不导航，settle 精确释放 transport owner。生产 Page 若仍 mounted/current VPS 匹配则由 stale-settle callback 权威 re-probe；无 callback 的 direct-详情视图 harness 只释放 owner |
| A mutation pending 时切到 B | B 可独立写；A settle 不改变 B owner/draft/notice |
| 真实 AppShell 中 A write pending → Dashboard/B/详情视图 → 返回 A-before-settle → A POST settle | user-scoped store 保留 A owner；A POST/key count 保持 1；返回视图的全部写控件 blocked；settle 后自动 re-probe A 并展示权威状态 |
| A write pending → 当前 route 已是 B → A settle | 不 re-probe B；B heading/data 与 capability probe count 不变 |
| 当前 A subscription pending → 关闭并重开 Drawer → POST settle | 重开时仍只有原 POST/幂等 key且旧快照保持；stale generation settle 后自动 re-probe A 并展示权威 subscription |
| 当前 A subscription pending → same-VPS query reload 完成旧 route load → POST settle | query reload 不产生第二 POST；stale generation settle 后再做一次权威 re-probe并收敛服务端 subscription |
| 当前 A subscription 正常 settle，owning generation 仍 current | 详情视图 自己提交 notice/refresh；page capability/detail probe count 不增加 |
| A→B→A 且旧 A refresh 最后返回 | detail/services/domains functional commit 丢弃旧结果 |
| same-VPS route reload A1 pending → mutation/refresh 或 reload A2 先提交 → A1 最后 settle | A1 payload、error、draft/drawer reset 与 navigation 全部丢弃；functional state setter 不覆盖最新 A |
| pending Drawer 关闭并重开同一 VPS | 不发第二次写请求；原 Promise settle 后才解锁 |
| `vps_asset_readonly` + latest terminal | current generation 才导航；失效后不导航 |
| load-latest 连点或关闭后迟到 | GET count 为 1；迟到 draft/ETag/error/navigation 不提交 |

### 5. Good / Base / Bad Cases

- Good: A/B 各有 deferred mutation，往返路由后各自只由自己的 token 解锁。
- Good: route/gate 卸载或 详情视图 切换时，AppShell user-scoped store 的 snapshot 与 exact-token finalizer 都继续有效；用户切换会销毁旧 registry。
- Good: exact owner release 后，详情视图 只在自己的 generation 已 stale 时通知 page；page 只在 mounted/current VPS 匹配时 bump revision 一次。
- Good: route effect 任一分支返回的 teardown 都会失效 write generation，不只失效 archive read。
- Good: same-VPS route request 在开始时捕获 generation，并在 payload、catch 与 functional setter 三处重检，而不只依赖 `cancelled`。
- Base: 用户关闭保存中的 Drawer；服务端请求继续，旧 view commit 被丢弃，transport owner 在 settle 后释放。
- Bad: route cleanup 只递增 archive request ID，导致卸载后的旧 mutation 仍可导航。
- Bad: route request 只用 effect-local `cancelled`；同一 effect 生命周期内的 mutation 可先提交，随后旧 route payload 仍覆盖它。
- Bad: `invalidateMutations()` 清 transport owner，允许原请求未 settle 时同 VPS重复提交。
- Bad: 用组件级 submitting boolean 让 A 阻止 B，或 route 切换时提前解锁 A。
- Bad: 在 `VPSDetailOverview` 或 `VPSDetailPage` 内创建生产 owner Map；route/page remount 会得到空 Map 并允许同 VPS重复 POST。
- Bad: snapshot 发布 digest/key，或按 `owner.operation` 只禁用当前 Drawer，导致另一 operation 控件仍显示 enabled 但 `begin` 静默拒绝。

### 6. Tests Required

- 用 controlled deferred promise 覆盖 query-driven reload skip effect → mutation pending → unmount → late success；断言无旧 archive navigation/notice/state commit。禁止 sleep。
- 覆盖 service/subscription A/B 双 deferred、同 VPS close/reopen、exact-token cleanup、全部现行写 operation，以及 facts/decision 代表路径。
- 真实 authenticated AppShell/VPSDetailPage 回归不得 mock 详情视图：覆盖 pending → Dashboard/返回、详情视图、Overview→离开/返回、同 VPS query reload、A/B 并行与旧 view settle 收敛；每条交错都要求 POST/key 唯一。store 单测覆盖 stale/exact token、instance/user 隔离、same/changed digest、409 rotation、confirmed clear，并断言 external snapshot 不包含 raw body、digest 或 key。
- 分别覆盖 detail/services/domains A→B→A stale refresh，predicate 必须在 functional setter 内生效。
- 用 controlled deferred 覆盖 same-VPS route load A1 与后继 mutation/refresh 或 reload A2 的交错；至少分别证明旧 payload、旧 catch 与 functional state commit 都不能覆盖最新 view。
- stale-success runtime 证明必须让 A1 detail 先通过最早 route guard，再延迟 timeline/services/domains/subscription 等二阶段请求。payload guard 用例在二阶段 settle 前推进 generation；functional-setter 用例则先让 payload guard 通过并排队 updater，再在 updater 执行前推进 generation。两条用例必须能分别对 outer/inner guard 做 mutation RED，不能只靠 source inventory。
- 覆盖 conflict/load-latest/readonly terminal routing 与 current-generation guards。

### 7. Wrong vs Correct

```ts
// 错误：effect early return 只失效 archive read，卸载后的 mutation 仍 authoritative。
if (skipNextQueryDrivenReload.current) {
  return () => { archiveReviewRequestRef.current += 1 }
}
```

```ts
// 正确：所有分支共用完整 authority cleanup；owner map 留到 Promise settle。
const invalidateRouteAuthority = () => {
  archiveReviewRequestRef.current += 1
  mutationGenerationRef.current += 1
  latestLoadLockRef.current = false
}
if (skipNextQueryDrivenReload.current) return invalidateRouteAuthority

const owner = beginVpsWrite(detail.vps_id, 'service')
if (!owner) {
  setServiceError('上一次保存仍在进行，请稍后再试')
  return
}
try {
  await createVPSService(owner.vpsId, input)
  if (!mutationIsCurrent(owner.generation)) return
  await refreshServices(owner.vpsId, () => mutationIsCurrent(owner.generation))
} finally {
  finishVpsWrite(owner) // Map key vpsId + unique token 精确释放
}
```

```tsx
// 错误：生产 owner store 属于会被 route/gate 卸载的 page 实例。
export function VPSDetailOverview() {
  const [writeOwnerStore] = useState(createVPSWriteOwnerStore)
}

// 正确：user-keyed authenticated shell 持有 provider；所有受保护 route 复用。
function AuthenticatedAppShell({ user }: { user: User }) {
  return (
    <VPSWriteRegistryProvider key={user.user_id}>
      <Outlet />
    </VPSWriteRegistryProvider>
  )
}
```

## Scenario: Archive review request supersession

### 1. Scope / Trigger

- 修改 archive eligibility review GET、归档确认 Modal open/close、route effect，或预览的 open/close/reopen 时适用。
- 这些 read owner 只约束客户端 UI authority；服务端仍是归档资格、权限与写入的最终权威。

### 2. Signatures

- Archive request ID: `archiveReviewRequestRef: MutableRefObject<number>`；owner predicate 同时比较 captured request ID 和 `currentVpsIdRef.current`。
- Preview generation：普通 refresh 在 await 前递增并捕获 generation；新 preview、关闭确认界面、route 切换及 unmount 都使旧请求失效。
- Route-owned preview：route payload 只有在 route generation 与 preview generation 都仍 authoritative 时才提交 digest/warnings/blockers；若普通 A2 preview 已推进 generation，route 的其余 owned payload 可提交，但 preview/error 必须保留 A2 当前值。

### 3. Contracts

- archive review request ID 不复用 mutation generation 或 write token。每次 open、close、route effect entry/cleanup 单调递增；success/catch/finally 各自独立检查 request ID + target VPS。
- 任一 route payload 若会关闭 lifecycle modal 或清空 review/error/loading，必须在这些 reset 之前递增 archive request ID；同 VPS query reload 期间新开的 review 也必须被 payload reset supersede。
- route payload 若会切换/关闭界面或清空 preview/error/result，必须在 reset 前失效旧 preview，并仅在 reset generation 仍 current 时清空状态。既要拒绝 reload pending 期间 preview 的 late success/failure，也要清除已提交的 blockers/warnings、preview error、mutation result/error 与旧资格提示。route-owned preview 不得为提交自己的结果而自我失效。
- close 先失效 read owner，再清可见 Modal/Drawer state。同 VPS close→reopen 也必须由新 ID/generation 区分 A1/A2。
- 关闭预览时先递增 preview generation；关闭前的 GET 不得在关闭后提交，也不得被重开后的界面复用。
- archive mutation 同时受 mutation generation 与 captured preview generation 约束；关闭不会提前释放 write transport owner。
- archive confirm 继续使用 lifecycle write owner和服务端重新校验；read owner 不改变 endpoint、错误文案或成功导航合同。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| A review pending → route B → A settles | B data/error/loading 不被 A 改写 |
| A1 review pending → close/reopen A2 → A1 settles | A2 data/error/loading 不变 |
| same-VPS query reload pending → open review → route payload reset → review settles | payload 先失效 request ID 再关 modal；迟到 success/rejection/finally 不写页面 feedback 或 loading |
| archive deep-link route A1 pending → route A2 先提交 → A1 settles | A1 的 digest/warnings/blockers 不覆盖 A2；route generation 与 captured preview generation 任一失效都拒绝提交 |
| route preview A1 pending → ordinary preview A2 starts and remains pending → A1 settles → A2 settles | A2 在 await 前取得 generation；A1 settle 后 loading 保持且旧 blockers/warnings 不出现；最终只显示 A2 |
| same-VPS reload pending → 手动打开 archive preview → route payload reset/switches surface → preview settles | payload 在 reset 前失效 preview generation；迟到 preview 不改变 attention/state，也不在关闭后的界面中复用 |
| 已提交 preview blockers/warnings + preview/mutation error/result → route payload switches Drawer | preview、error、result 与页面 attention 同时清除，不靠 Drawer 隐藏旧 state |
| archive preview A1 pending → close → settle → reopen | reopen 前 A1 不得改变页面 attention/state；reopen 发 A2 GET，只显示 A2 blockers/warnings |
| current archive/preview owner succeeds or fails | 仅自己的 data/error/loading 分支提交 |

### 5. Good / Base / Bad Cases

- Good: archive then/catch/finally 都调用同一 exact-owner predicate。
- Good: archive close/reopen 使用 controlled A1/A2 deferred，重开一定重新请求。
- Base: current request 正常成功或失败并由自己的 finally 收尾。
- Bad: 只比较 VPS ID；same-VPS ABA 会让 A1污染 A2。
- Bad: 无条件 finally 结束新 owner 的 loading。
- Bad: route payload 只 `setArchiveReview(null)` / `setLifecycleConfirmingAction(null)`，却不先递增 request ID；迟到 catch 会把错误泄漏到页面。
- Bad: route payload 清空 preview state 并切走界面，却不推进 preview generation；刚被关闭的 preview 仍能在稍后写回 blockers。
- Bad: close preview 不递增 generation，导致旧 blockers 在关闭后写回并被重用。

### 6. Tests Required

- Archive review 用 deferred promise 覆盖跨路由 late success/rejection/finally、same-VPS close/reopen A1/A2、current-owner success/failure及所有 effect branch cleanup。
- 覆盖同 VPS query reload 与 review 交错：先让 route load pending，再 open review，payload settle 关闭 modal，最后 settle review；断言迟到 data/error/finally 均无页面提交。
- 上述 query-reload/archive 交错必须分别用 late rejection 与 late success 证明 catch/then/finally。stale-finally 用例必须在 A2 review 仍 pending 时先 settle A1，断言 A2 loading/disabled-confirm authority 保持，再由 A2 自己 settle；该 loading 分支会隐藏 stale data，不能独立证明 `then`。因此另需先完成 eligible A2、启用 confirm，再 settle 带 blocker 的 A1，断言 A2 data/confirm 不变；删除对应 `then` 或 `finally` guard 必须分别 mutation RED。
- 用两个 controlled deferred 覆盖 route-owned archive preview A1/A2 ABA：A2 start 后保持 pending，先 settle A1 并断言 loading/attention/state 未被旧结果改变，再 settle A2 且只显示 A2。另覆盖 reload payload reset 后手动 preview 的 late settle。
- payload reset 还必须有反向顺序回归：先提交 preview blockers/warnings、error/result 与页面 attention，再 settle pending 的其他 route payload，断言这些可见状态与底层 attention 全部清除。
- Archive preview 用 controlled deferred 覆盖 close 后 stale settle 与 reopen；必须在 reopen 前断言 A1 未改变页面 attention/state，避免随后清空缓存掩盖旧提交。另用已加载 A1 覆盖关闭按钮和 Modal `onClose`；都要断言 fresh GET、旧内容缺席、新内容唯一可见。
- 测试必须断言 data/error/loading/confirm authority，不得只断言未崩溃。

### 7. Wrong vs Correct

```ts
// 错误：旧 preview 在 Drawer 关闭后仍可写回。
function closeDrawer() {
  invalidateMutations()
  collapseDrawer()
}
```

```ts
// 正确：关闭归档 surface 先 supersede preview；写 owner 仍由 Promise settle。
function closeDrawer() {
  invalidateMutations()
  archiveReviewRequestRef.current += 1
  collapseDrawer()
}
```
