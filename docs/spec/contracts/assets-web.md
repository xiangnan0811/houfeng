# 资产 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

- Legacy VPS 详情页（`LegacyVPSDetail.tsx`）所有会在 await 之后改 notice / draft / drawer / navigate 的写操作都必须走现有 `beginVpsWrite` + `mutationIsCurrent`。不得只给 facts/decision 加归属。晚到的 VPS A 结果不得关闭 B 的抽屉或导航离开 B。服务端请求不必取消；`finishVpsWrite` 仍在 `finally` 释放同 VPS 锁。
- Asset Ledger 共享枚举必须跟后端机器值同步：`AssetScope = 'current'|'historical'|'archived'|'all'`，其中 `archived` 是旧 API 兼容别名；`RenewalMode = 'auto'|'manual'|'auto_cancelled'|'lottery'|'gift'|'bonus'|'other'`，其中 `lottery` 展示为“抽奖”，`gift` 展示为“赠送”。选项和标签集中在 `web/src/lib/assetOptions.ts`，页面不得散落 `抽奖/赠送` 这种混合标签。

### VPS 详情 agent 接入/升级复用已有 MonitoringInstance

#### 1. Scope / Trigger

- Trigger: 修改 VPS 详情页普通 agent 接入入口、`workbench=monitoring` / `workbench=monitoring-instance-create` 深链、VPS ↔ MonitoringInstance link 写路径、或 MonitoringInstance onboarding 文案。
- 目标：VPS 已有 active MonitoringInstance link 时，普通 agent 接入入口必须复用现有监控实例进入升级/重新接入流程，避免误创建第二个 active 监控实例。

#### 2. Signatures

- Frontend deep link: `/vps/{vps_id}?workbench=monitoring` and `/vps/{vps_id}?workbench=monitoring-instance-create`。
- Frontend upgrade target: `/monitoring/{monitoring_instance_id}?onboarding=1&return_vps={vps_id}`。
- Backend create API: `POST /api/vps/{vps_id}/monitoring-instances`。
- Backend link API: `POST /api/vps/{vps_id}/link-monitoring-instance`。
- Backend domain error: `assetlinks.ErrVPSActiveMonitoringInstanceExists` maps to HTTP 409.

#### 3. Contracts

- 0 active links: VPS detail may show `创建并接入 agent`; submit may call `createVPSMonitoringInstance`, then navigate to MonitoringInstance onboarding.
- 1 active link: VPS detail must show `升级/重新接入 agent`; clicking or opening either monitoring workbench deep link must navigate to the existing MonitoringInstance onboarding and must not call the create API.
- More than 1 active link: do not auto-clean historical data; hide create/link entry, show a duplicate-active-link warning, and keep per-row `升级/重新接入 agent` plus `解除关联`.
- Backend create/link write paths must lock/check active links before inserting and return 409 on existing active links. The create path must not insert an orphan `monitoring_instances` row on conflict.
- Monitoring detail onboarding keeps using `issueMonitoringInstanceInstallCommand`; only copy/title changes between `接入 agent` and `升级/重新接入 agent` based on bound/observed state.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| VPS has 0 active links and user opens monitoring workbench | Open create drawer; create API allowed |
| VPS has 1 active link and user opens monitoring workbench | Navigate to existing `/monitoring/{id}?onboarding=1&return_vps={vps_id}` |
| VPS has >1 active links | Show manual duplicate review warning; no create/link CTA |
| Create/link API sees existing active link | 409 `vps active monitoring instance exists` |
| Create API sees existing active link | No `monitoring_instances` insert before returning error |

#### 5. Good/Base/Bad Cases

- Good: already-connected VPS upgrades agent by reusing its existing MonitoringInstance and generating a fresh install command there.
- Base: brand-new VPS without active link creates one MonitoringInstance and enters onboarding.
- Bad: a button labeled `创建并接入 agent` on an already-linked VPS calls `POST /api/vps/{id}/monitoring-instances` and creates a second active monitoring instance.
- Bad: only the frontend blocks duplicate creation while backend link/create endpoints still allow another active link.

#### 6. Tests Required

- `VPSDetailPage.test.tsx`: 0/1/multiple active-link behavior, monitoring workbench deep-link branching, and no create API call when reusing an active link.
- `MonitoringDetailPage.test.tsx`: bound or already-observed instances use `升级/重新接入 agent` wording while still issuing install commands through the existing API.
- Handler/store Go tests: create/link return 409 for existing active links; store create checks active links before inserting MonitoringInstance.

#### 7. Wrong vs Correct

```tsx
// 错误：深链和按钮都无条件打开创建流程。
if (workbench === 'monitoring') {
  setActiveDrawer('monitoring-instance-create')
}
await createVPSMonitoringInstance(vpsId, input)
```

```tsx
// 正确：先按 active link 数量分流。
if (activeLinks.length === 1) {
  navigate(`/monitoring/${activeLinks[0].monitoring_instance_id}?onboarding=1&return_vps=${vpsId}`)
} else if (activeLinks.length === 0) {
  setActiveDrawer('monitoring-instance-create')
} else {
  setActiveDrawer('monitoring-instance-evidence')
}
```

### Asset service 数据流

VPS 服务资产是 VPS 详情页内的独立手工记录区块，前端必须把它当作 `asset_services` contract 消费，而不是从 timeline、Dashboard 或 Target probe 状态推导。

#### 1. Scope / Trigger

- Trigger: 修改 `web/src/lib/types.ts` 中 `AssetService*` 类型、`web/src/lib/api.ts` 中 service API helper，或 `web/src/pages/VPSDetailPage.tsx` 的服务资产区块。

#### 2. Signatures

- Frontend type: `AssetServiceRecord` 字段保持 center JSON snake_case：`service_id`、`vps_id`、`target_id`、`name`、`service_type`、`status`、`url`、`port`、`labels`、`note`、`created_at`、`updated_at`。
- Frontend input: `CreateAssetServiceInput` 允许 collection create 带 `vps_id`，也允许 VPS scoped create 不带 `vps_id`。
- Frontend API: `listAssetServices(filter)`, `createAssetService(input)`, `listVPSServices(vpsId)`, `createVPSService(vpsId, input)`。
- Page data: `VPSDetailPage` 初始加载 `getVPSAsset(vpsId)`、`getVPSTimeline(vpsId)`、`listVPSServices(vpsId)`、`listVPSDomains(vpsId)` 和 `listSubscriptions({ vps_id: vpsId, sort: 'renew_at', order: 'asc' })`；创建服务后只刷新 `listVPSServices(vpsId)`。

#### 3. Contracts

- 机器值标签集中在 `ASSET_SERVICE_TYPE_LABELS` 与 `ASSET_SERVICE_STATUS_LABELS`，组件内不得散落中文枚举文案。
- `createVPSService(vpsId, input)` 必须去掉 `input.vps_id`，保证 path 是唯一 VPS 来源。
- VPS 服务区块展示 name、type、status、url/port、optional Target link、labels、note；Target link 只跳转，不触发 Target 创建或修改。
- 服务创建表单负责本地校验 blank name 和 port `1..65535`，但最终校验仍以后端为准。
- 服务资产不是 timeline item；创建服务不得刷新或插入 `VPSTimeline.experience_logs`、续费历史、价格历史、IP 历史或规格快照。
- 服务创建表单应在 对话框 或同等次级 surface 中打开。主扫描路径展示服务表格和保存后的 notice，不常驻创建表单。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| blank service name in form | 页面本地显示 `服务名称不能为空。`，不发 POST |
| invalid port in form | 页面本地显示 `服务端口必须为 1 到 65535。`，不发 POST |
| API returns `vps asset not found` | `ApiError.message` 原样展示在当前服务表单错误区 |
| API returns `target not found` | `ApiError.message` 原样展示在当前服务表单错误区 |
| services list is empty | `DataTable` emptyContent 显示 `尚未记录服务` |

#### 5. Good/Base/Bad Cases

- Good: 创建服务成功后显示 `服务记录已创建`，表格出现新服务，并且只追加一次 `/api/vps/{id}/services` refresh。
- Base: 初始 services 为空时，页面仍正常展示 VPS facts、MonitoringInstance links、timeline 和连接摘要。
- Bad: 页面直接 `fetch('/api/vps/.../services')`，绕过 `api.ts` 和 `ApiError`。
- Bad: 把服务数量写进 Dashboard asset summary，或从 Target 列表自动反推 services。

#### 6. Tests Required

- `web/src/lib/api.test.ts`: collection list/create、VPS scoped list/create，并断言 scoped create body 不含 `vps_id`。
- `web/src/pages/VPSDetailPage.test.tsx`: 初始 services 请求、空态、服务创建 happy path、本地校验失败。
- 改 `AssetService*` 类型或标签时同步测试 fixture 和页面断言。

#### 7. Wrong vs Correct

```tsx
// 错误：业务 page 直接 fetch，错误处理和认证钩子会漂移。
await fetch(`/api/vps/${vpsId}/services`)

// 正确：统一通过 API client。
await listVPSServices(vpsId)
```

```tsx
// 错误：VPS scoped create 把 body 里的 vps_id 带过去。
createVPSService(vpsId, { ...form, vps_id: otherVpsId })

// 正确：helper 丢弃 vps_id，path 是唯一 VPS 来源。
const { vps_id: _ignored, ...body } = input
postJSONBody(`/api/vps/${vpsId}/services`, body)
```

### Asset domain 数据流

VPS 域名资产是 VPS 详情页内的独立手工记录区块，前端必须把它当作 `asset_domains` contract 消费，而不是从 services、timeline、Dashboard、Target probe 或 DNS provider 自动推导。

#### 1. Scope / Trigger

- Trigger: 修改 `web/src/lib/types.ts` 中 `AssetDomain*` 类型、`web/src/lib/api.ts` 中 domain API helper，或 `web/src/pages/VPSDetailPage.tsx` 的域名资产区块。

#### 2. Signatures

- Frontend type: `AssetDomainRecord` 字段保持 center JSON snake_case：`domain_id`、`vps_id`、`service_id`、`target_id`、`domain_name`、`purpose`、`status`、`registrar`、`expires_at`、`auto_renew`、`https_enabled`、`labels`、`note`、`created_at`、`updated_at`。
- Frontend input: `CreateAssetDomainInput` 允许 collection create 带 `vps_id`，也允许 VPS scoped create 不带 `vps_id`。
- Frontend API: `listAssetDomains(filter)`, `createAssetDomain(input)`, `listVPSDomains(vpsId)`, `createVPSDomain(vpsId, input)`。
- Page data: `VPSDetailPage` 初始加载 `getVPSAsset(vpsId)`、`getVPSTimeline(vpsId)`、`listVPSServices(vpsId)`、`listVPSDomains(vpsId)` 和 VPS scoped `listSubscriptions`；创建域名后只刷新 `listVPSDomains(vpsId)`；刷新 detail + timeline 的动作必须保留 services/domains 两个独立列表，并重新读取 subscription evidence。

#### 3. Contracts

- 机器值标签集中在 `ASSET_DOMAIN_STATUS_LABELS`，组件内不得散落中文枚举文案。
- `createVPSDomain(vpsId, input)` 必须去掉 `input.vps_id`，保证 path 是唯一 VPS 来源。
- VPS 域名区块展示 domain name、status、HTTPS、purpose、registrar、expires_at、auto_renew、optional Service / Target link、labels、note；Target link 只跳转，不触发 Target 创建或修改。
- 域名创建表单负责本地校验 blank domain、URL/path/space 和裸主机名，但最终校验仍以后端为准。
- 域名资产不是 timeline item；创建域名不得刷新或插入 `VPSTimeline.experience_logs`、续费历史、价格历史、IP 历史或规格快照。
- 域名创建表单应在 对话框 或同等次级 surface 中打开。主扫描路径展示域名表格和保存后的 notice，不常驻创建表单。


#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| blank domain in form | 页面本地显示 `域名不能为空。`，不发 POST |
| URL/path/space/bare host in form | 页面本地显示 `域名必须是不带协议、路径和空格的完整域名。`，不发 POST |
| API returns `vps asset not found` | `ApiError.message` 原样展示在当前域名表单错误区 |
| API returns `asset service not found` | `ApiError.message` 原样展示在当前域名表单错误区 |
| API returns `target not found` | `ApiError.message` 原样展示在当前域名表单错误区 |
| API returns `asset domain conflict` | `ApiError.message` 原样展示在当前域名表单错误区 |
| domains list is empty | `DataTable` emptyContent 显示 `尚未记录域名` |

#### 5. Good/Base/Bad Cases

- Good: 创建域名成功后显示 `域名记录已创建`，表格出现新域名，并且只追加一次 `/api/vps/{id}/domains` refresh。
- Base: 初始 domains 为空时，页面仍正常展示 VPS facts、MonitoringInstance links、services、timeline 和连接摘要。
- Bad: 页面直接 `fetch('/api/vps/.../domains')`，绕过 `api.ts` 和 `ApiError`。
- Bad: 把域名数量写进 Dashboard asset summary，或从 Target / service 列表自动反推 domains。

#### 6. Tests Required

- `web/src/lib/api.test.ts`: collection list/create、VPS scoped list/create，并断言 scoped create body 不含 `vps_id`。
- `web/src/pages/VPSDetailPage.test.tsx`: 初始 domains 请求、空态、域名创建 happy path、本地校验失败。
- 改 `AssetDomain*` 类型或标签时同步测试 fixture 和页面断言。

#### 7. Wrong vs Correct

```tsx
// 错误：业务 page 直接 fetch，错误处理和认证钩子会漂移。
await fetch(`/api/vps/${vpsId}/domains`)

// 正确：统一通过 API client。
await listVPSDomains(vpsId)
```

```tsx
// 错误：VPS scoped create 把 body 里的 vps_id 带过去。
createVPSDomain(vpsId, { ...form, vps_id: otherVpsId })

// 正确：helper 丢弃 vps_id，path 是唯一 VPS 来源。
const { vps_id: _ignored, ...body } = input
postJSONBody(`/api/vps/${vpsId}/domains`, body)
```


### VPS detail 判断工作台数据流

VPS 详情页可以把 VPS detail、timeline、VPS scoped subscriptions、VPS scoped services/domains 组合成单台资产判断 workbench。它只使用现有 contract，不能发明不存在的资产风险或 provider facts。

#### Contracts

- `VPSDetailPage` 初始加载必须包括 `getVPSAsset(vpsId)`、`getVPSTimeline(vpsId)`、`listVPSServices(vpsId)`、`listVPSDomains(vpsId)` 和 `listSubscriptions({ vps_id: vpsId, sort: 'renew_at', order: 'asc' })`。Provider/MonitoringInstance/Target selector 数据可在对应 对话框 打开时懒加载，避免主详情首屏为选择器阻塞。
- VPS scoped subscription 只作为续费/成本 evidence。订阅请求失败时显示请求错误和未知状态，不得把 failure 当成真实 `缺订阅`。
- VPS Detail 是普通补录入口；草稿字段与调用方幂等键生命周期见 [订阅 Web 合同](subscriptions-web.md)，请求与错误协议见 [订阅合同](subscriptions.md)。`createVPSMonitoringInstance(vpsId, input?)` 默认空 body，由后端派生身份并自动 link。
- VPS 详情页必须可加载 `getVPSCancellationPreview(vpsId)` 并在资产判断 workbench 显示取消 / 过期影响范围；URL `?workbench=cancellation` 应直接打开统一取消 / 退役工作台。
- 如果 preview 显示 subscription 已非活跃但 VPS 仍未取消，页面不得引导“创建订阅”作为主路径，而应引导用户处理 VPS、MonitoringInstance 与 Target/实例的 lifecycle action。
- `VPSAssetDetail.monitoring_instance_links` 可以在 Detail 页展示 health、heartbeat、active incident count 和 issue summary，因为后端 detail contract 已返回这些字段；这不改变 `VPSAssetRecord.active_monitoring_instance_link_count` 在列表页只能代表数量的限制。
- 决策、facts、MonitoringInstance link、experience log、service create、domain create、取消 / 退役 action 的复杂输入使用受控 Modal；其余表单使用各自当前对话框。关闭 对话框 后，保存成功 notice 必须留在主页面可见 surface 内。
- Facts 对话框 只编辑基础事实和用途状态；不得包含 lifecycle status，也不得在 facts PATCH payload 中发送 `lifecycle_status`。
- Archive/restore 仍是 lifecycle 危险操作，使用独立 confirmation，不放入 routine edit 对话框。

#### Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| scoped subscriptions request fails | Workbench 显示订阅读取失败和错误提示，不显示真实 `缺订阅` 质量缺口 |
| scoped subscriptions empty | Workbench 显示 `缺订阅`，资料质量 badge 标记缺订阅 |
| decision/facts/experience save succeeds | 对话框 关闭，主页面出现成功 notice，detail/timeline/services/domains/subscriptions 刷新 |
| service/domain create succeeds | 对话框 关闭，只刷新对应 service/domain list，主页面表格出现新记录 |
| local service/domain validation fails | 对话框 内显示本地错误，不发 POST；主页面不重复渲染同一错误 |

#### Specialized contract: VPS Detail async ownership

Legacy VPS Detail 的 mutation transport、view generation、refresh commit、archive review 与 cancellation preview supersession 以 bounded [VPS Detail 异步所有权合同](./assets-async-ownership.md) 为唯一权威来源。本通用文档不重复该长场景，避免任务上下文截断与双份合同漂移。

### VPS inventory dual-view contract

`/vps` owns one data and action flow with two presentations: a grouped scanning table with a compact quick inspector, and a directory with a richer reading inspector. Both reuse existing create/filter dialogs, fact formatters, and canonical management destinations; neither is a replacement for the independent VPS detail workspace. Inspectors summarize available list and subscription evidence, not a second detail API or an independent business-state source.

The switch is labeled 表格视图 / 目录视图; persisted `workbench` / `ledger` values remain unchanged. Both layouts consume the existing common theme and component language rather than maintaining page-local palettes, badge skins, or a separate editorial type scale. Shared-control contrast fixes belong to their shared semantic styles, not another VPS override.

- `workspace=ledger|workbench` selects presentation. A valid explicit URL wins over the browser preference `houfeng.vps.workspace`; absent a preference, use `ledger`. Storage denial must not prevent switching.
- Existing `view` is a business quick filter. `q` is local inventory search and `selected` identifies the inspected asset. Workspace changes and filter edits preserve these fields and unrelated URL parameters. Only the non-sensitive workspace preference is stored in localStorage.
- Clicking a table row or its native asset-name button toggles that row's accordion, without navigation. Selecting another row moves the single open accordion. Collapsing keeps the selected URL value. Use the explicit detail link inside the accordion to open management; the unlinked quick view retains the existing `workbench=monitoring` onboarding destination.
- If the selected asset is outside the filtered results, ledger shows an empty inspector and workbench shows no accordion rather than silently inspecting another asset. Clearing filters can restore the selection.
- Subscription loading and failure are not missing-subscription evidence. Do not infer runtime health, successful sync, or trends from the asset ledger.
- Browser verification must cover continuous text entry, view switching, selection, history, and narrow screens; a single synthetic change event does not prove typing works.

### List, quick inspection, and independent detail

- The table view prioritizes scanning an inventory of tens of machines. Its quick inspector starts collapsed and expands directly after the selected row, inside the list scroller. There is no bottom inspector strip. Expanded inspection is limited to identity, business/renewal facts, monitoring association, and available observation evidence; it must not become a second full detail page. On narrow screens, the list remains independently scrollable.
- Both inventory views enter the same canonical `/vps/:id` workspace. Both capability modes use a compact identity/action header beneath the sole validated top-bar inventory return. Organize by content rather than a permanent main/aside split: flexible asset/connection facts and a bounded billing summary form the first zone; observations, named service/domain lists and recent activity remain independent. The billing area is approximately 320–420px only while beside sufficiently wide facts, and stacks when needed. The route uses a comfortable 1600px ceiling, 16px padding, 14px gaps and natural heights. Keep shared palette and fine borders without decorative card shadows. The closed 页面目录 remains subordinate to real business tabs and preserves hash positioning/focus.
- Use the true subscription name and put its weak record ID on a separate subordinate line, distinct from asset identity. Combine amount and normalized period, then keep absolute renewal/expiry date and the VPS decision close; do not interpret 保留 as an automated resource action. Labeled record updates do not imply staleness. Existing scoped APIs or loaded legacy collections remain the source; loading/error are not empty collections. Resource rows have two content layers: name/status/explicit details action, then address/type/port/purpose. The row itself is not a button; full values and independent copy/links must remain usable. Copy stays adjacent to full wrapping IP/SSH values and announces success/failure; missing values are not copyable. Compact desktop controls retain adequate touch targets on narrow/coarse-pointer surfaces.
- Runtime observation rows keep scope, known result, source time and real actions nearby. Remove exact same-object status repetition without erasing distinct results or interpreting lifecycle as health. Missing/stale/unavailable evidence cannot expand a healthy claim; IP query failures do not erase valid monitoring or known historical IP findings. Keep not-configured, failed, absent and historical facts distinct according to the contract. Do not invent scores, denominators, configuration routes or incidents. Activity highlights action, event time and type; shorten only an exact current-asset prefix when distinguishing other-object information is retained, keeping the full accessible title. The top-bar 系统摘要 belongs to the fleet snapshot and is not asset health.
- Use one shared observation-row layout for project, primary conclusion, explanation, data time, and actions across capability modes. Desktop columns share stable tracks; narrow layouts reflow into consistent labelled slots. Keep long source errors in a subordinate row under their object, not interleaved with the main conclusion or action track. Existing result, latest read outcome, and source age are independent: name retained content only when it exists, and never describe visible retained evidence as wholly unavailable. Activity data time and event time are different fields.
- Shared fact rendering preserves all facts supplied by each existing source, including importance and labels already present in overview identity. Short fields may share columns; SSH and notes retain continuous width. Do not fabricate fields absent from a source or add requests to make capability modes identical. Successful empty resource groups use lightweight summaries; unavailable is not empty, and an empty report does not disable a real authorized report destination.
- The six bounded VPS detail dialogs share three content templates inside the existing Modal: short renewal decision, structured VPS/subscription forms, and associated-object reading. Keep the accepted main workspace unchanged. Use one task title and light asset context; renewal decisions describe saved intent, never supplier execution. Show current versus draft decision only when they differ.
- Service, domain, validity, monitoring-link, monitoring-create, and legacy experience forms reuse `.vps-form` / `VPSFormSection` and Modal title+fixed footer; do not repeat the task title in an inner `asset-operation-form__header`. Keep current subscription facts and prerequisite warnings. Do not delete shared `.vps-create-form*` or decision-work `asset-operation-form__header` styles still used outside VPS dialogs. Inventory list and inspector status use `LifecycleBadge` / `UsageBadge` / `RenewalBadge`; ledger directory may omit usage for density.
- Match width to content: decision 380px, facts 680px, services 520px, domains 560px, and subscription/monitoring reading 780px. Facts and service/domain reading become full-viewport sheets on narrow screens; other dialogs retain their existing gutters. Keep a fixed header, an independently scrolling body, and fixed actions only for actual forms. Subscription facts remain a new record, not an edit; preserve their existing fields and behavior.
- Service/domain reading keeps complete collections, neutral record status (not observed health), names, notes, labels and subordinate IDs. Show service URLs as full monospaced text with adjacent copy and HTTP(S)-only open actions on the following row; neither read panel has a save/cancel footer or a pretend edit action. Domains may enrich service IDs from the scoped service API; missing optional metadata must not block reading or reuse another VPS’s data. Existing legacy create actions remain separate. Monitoring reading and all ownership/focus/scroll safeguards are unchanged.
- Freshness belongs beside each object's own source timestamp, not inside its conclusion. Adjacent view and refresh actions share a wide-screen row without changing ordinary single-action columns. Overview refresh/retry rereads the overview; it does not request agent sync or a probe. Activity source time denotes latest visible recorded intake, independently of event time.
- Empty navigation retains the current VPS: a subscription collection is not an existing subscription object, zero monitoring opens association status rather than a fabricated instance, and a report route remains available when its real capability permits reading empty/history/error states. Label historical report entry only from known historical evidence. A lone unlinked-monitoring notice states its reason once, its observation impact, and the existing action or read-only explanation.
- Cancellation plans are not supplier cancellation confirmations. Keep registered renewal/billing dates separate from the VPS plan. Preserve the center's overall conclusion and name a pending cancellation plan as an additional attention basis when applicable, rather than attributing the difference from incomplete observations solely to missing IP evidence.
- Pending cancellation labels registered renewal and billing-period dates consistently for primary and extra subscriptions; trial expiry remains trial expiry. Report entry names distinguish actual report/history evidence from the generic results destination; successful unconfigured/empty responses are not read failures.
- An explicitly disabled optional IP-quality source with a ready section is outside the enabled-observation assessment, not a failed source. Keep its local 未启用 label and state the limited scope beside an otherwise healthy judgment. Stale or unavailable source metadata still limits the judgment, and known adverse findings must not be downgraded.
- Full-context reading uses the independent page. Management entries are grouped by business/billing, runtime, associations, and applicable lifecycle operations; grouping must not remove a destination or callback. Existing bounded edits, onboarding panels, and dangerous-action confirmations remain shared management operations, with their capability, freshness, read-only, and write-ownership checks preserved. The overview-enabled and capability-off presentations do not define separate business workflows.
- Inventory entries carry the current `/vps` query in React Router history state `vpsInventoryHref`. The top-bar return accepts only `/vps` with optional search parameters; absent or invalid context falls back to `/vps`. Onboarding query consumption, in-page sections, VPS activity/records/evidence navigation, and activity filter/retry replacements preserve that state. This is navigation context, not another persisted preference.
- Browser acceptance covers both layouts in global dark/light themes, selecting from a 30-asset inventory, collapsing/reopening inspection, opening actual independent detail, accessing and closing management, and returning with view/search/filter/selection intact. Entering the base detail page resets the shared main scroller; explicit hash navigation retains its section target. Fixed top bars must not cover return controls or in-page section targets.

## VPS facts editor

The shared editor is the create (添加 VPS) and edit facts form. It shows common identity/provider, country, city/IPv4, usage status, importance, labels and notes first. Put all lower-frequency facts in one default-collapsed 可选设置 without nested categories. IPv6 and custom SSH host/port are hidden until shown; existing configuration initializes them visible, and hiding or collapsing never erases values. Editing IPv4 still derives an automatic SSH host but must preserve a hidden custom host. The country field is one editable in-flow combo: 17 common locations grouped by geography, 232 remaining ISO locations behind one expansion, full Chinese/English/code search and custom values. Reopening a committed value returns to browsing. Usage status remains the existing five-value business enum, not free-text purpose; importance remains a native select. Keep the header and feedback/save/cancel footer fixed while the body scrolls. Create keeps inline provider creation, reset/error/pending/submit and navigation, and must not duplicate these fields. Facts dialog chrome (680px desktop, fullscreen on narrow screens) lives only in the registered VPS style owner in `web/css-owners.json`. Both overview and legacy retain the same real PATCH, provider snapshots, conflict recovery and write ownership. Service/domain update APIs are not part of this frontend change.

Archive remains a read-only historical VPS ledger with a compact identity/status/time/cost scanning table and an independent detail workspace. Distinguish missing archive time from the record update time. Organize detail evidence by identity, billing, runtime links, services/domains, and dated records; order history by absolute event time, not timestamp-string order. Subscription and timeline failures stay local with source-specific retries. A successful archive review can retain its known billing records while the separate subscription read fails; a successful empty list replaces that retained evidence. Wide tables have uniquely named, keyboard-focusable local scroll regions, and narrow headers/actions wrap without clipping. Only archived assets offer an explicit restore-to-idle confirmation; cancelled assets remain read-only. Pending restoration cannot be dismissed or submitted twice, failures remain retryable, and successful restoration returns to the existing VPS detail route without rewriting associations/history. Requests from an obsolete visit must not overwrite data or navigate a later visit.

Domain reading retains its domain records and service IDs when optional service-name enrichment fails. Announce that auxiliary failure locally and retry only the scoped service read; ignore responses belonging to a closed panel or another VPS. Cancellation/retirement uses a compact impact summary and one confirmation heading without changing the authoritative preview, explicit object selections, or audited execution contract.

- **危险联动流程使用状态驱动入口，不做常驻菜单项**：取消 / 退役 / 迁移这类会影响订阅、监控实例、探测对象或其它关联对象的流程，不能作为普通 `…` 菜单项常驻展示，也不能在详情页中部单独铺一个“待处理”工作区。它应由顶部决策 / 当前判断模型按状态暴露 action，例如 VPS 详情页的 `judgement.primaryAction = { label: '处理取消/退役', mode: 'cancellation' }`；稳定状态返回 `null`。点击后打开居中 `Modal` 加载 preview，让用户显式选择影响对象并确认执行，不得直接提交危险操作。测试必须覆盖：相关状态显示入口、稳定状态隐藏入口、更多菜单没有该危险项、deep link 仍可打开既有 modal。
- **迁移在工作台完成前只能表达为意向**：VPS 页面、资产决策页和 execution plan 不得写“推进迁移”“迁移流程”“迁移工作台”这类暗示已有受控迁移闭环的文案。当前只能写“标记迁移意向”“人工跟进”“复核迁移意向”等，并继续把真实取消/退役动作引导到既有 workbench。
- **当前关注状态归入顶部判断，不铺中部提醒条**：VPS 详情页里的“运行观测需要核对”、缺订阅、缺运行观测、订阅读取失败、IP 质量暂不可用、续费临期 / 自动续费取消等当前需要用户处理或核对的状态，必须进入顶部 `VPSDetailOverviewPanel` 的“当前判断”模型，例如 `judgement.attentionItems`。页面中段的“关联概览”“单机台账”“IP 质量概况”只承载详情摘要和管理入口，不再渲染 `VPSContextActionPanel` / `vps-detail-context-action` 这类横条。多个关注状态必须可并列展示，不能被单个 `primaryAction` 覆盖；稳定状态下不展示额外列表。

### VPS workspace visual language

The VPS inventory offers two layouts within one visual system:

- **目录视图 (ledger)** is the default: an asset directory on the left and a readable inspector on the right.
- **表格视图 (workbench)** is a scanning table. Clicking an asset name toggles a compact accordion directly beneath that row; only one row is expanded at a time. Do not put the inspector at the bottom of the inventory.

Both views share the shell and other pages' semantic surface, text, border, accent, state, spacing, and typography tokens. Use the same sans-serif heading hierarchy, technical monospace facts, shared Badge treatment, and focus language. Do not restore separate graphite/brown palettes or editorial serif headings. Layout and density may differ; the visual identity must not. Both follow global dark/light and system-resolved modes. The view switch changes neither global theme nor business semantics.

Both layouts use the same inventory, subscription evidence, search, filters, selection, and management destinations, and open one independent VPS detail workspace. Keep return navigation and section hierarchy explicit. Inspectors are reading aids, not alternative detail routes or separate business forms.

The detail workspace uses the shared shell palette in both capability modes: a unified identity header, one validated inventory return in the top-bar breadcrumb, and a primary management action only when writable. Light mode uses coordinated neutral, slightly green-tinted shell/canvas surfaces and white content surfaces; dark mode retains the same semantic hierarchy. Do not restore route-only canvas colors or beige controls beside neutral cards. Separate subscription/renewal, runtime observations, asset facts, resources, and a quieter activity timeline with spacing, fine borders, and headings rather than permanent drop shadows or nested field cards.

Treat VPS detail as a personal infrastructure asset-and-facts workspace, not a realtime operations cockpit. Allocate space by content: flexible connection facts beside a compact billing summary in the first zone, followed by independent observation rows, resource lists and lightweight activity. Do not impose a permanent page-wide main/aside ratio. The route currently caps comfortable reading width at 1600 CSS px; collapse the first zone when its contents cannot fit. Use approximately 22px page titles, 15px module headings, 14px body/field values, 12–13px helper text and 20px amounts, with 1.45 body leading, 16px section padding and 14px gaps. Do not enlarge typography with viewport width or manufacture card height.

Keep primary route navigation distinct from the closed in-page directory. Normal readiness is quiet; stale or unavailable evidence, applicable timestamps, and retry actions remain local to the affected source. Resource actions belong beside their actual group or record and must not imply a record-specific destination when only a collection action exists. Menus must remain readable and reachable when their trigger moves to the left on narrow screens.

Keep the established density baseline while restoring moderate surface contrast: primary content uses the shared content surface against the shell canvas, without heavy shadows, enlarged radii, or disabled-looking page opacity. Observation rows share stable project/conclusion/explanation/data-time/action positions; put long failure explanations beneath their object. Use consistent state badges rather than colored words embedded in ordinary prose. Natural remaining whitespace is acceptable; do not stretch sections to equal heights.

Keep detail body text comfortably readable at approximately 14px and helper text at 12–13px; density comes from removing repetition and shortening information distances, not indiscriminate shrinking. Use sans-serif for reading and technical monospace for useful identifiers/IP/SSH. Preserve hover/focus contrast, natural long-name/note wrapping, adjacent full-value copy feedback and touch-sized actions. Browser zoom must remain usable; never implement density with CSS zoom or page transforms.

Use concise labels and factual loading/error/empty states. Do not add instructional lead-ins, self-descriptions of the inspector, or prose that restates the adjacent facts.


## 库存与归档数据流

- VPS inventory data: `VPSPage` 拉取 current `listVPSAssets()`、`listProviders()` 和 current `listSubscriptions({ sort: 'renew_at', order: 'asc' })`，在前端按 URL-state 做 derived quick views；已 `cancelled` / `archived` VPS 不在主库存页展示，只通过 `/archive` 只读入口查看。
- Archive data: `/archive` 是列表页，只显式请求 `listVPSAssets({asset_scope:'historical'})` 和 `listSubscriptions({asset_scope:'historical', sort:'renew_at', order:'asc'})` 形成摘要，不自动拉单台 detail、services、domains 或 timeline。点击行 / 操作进入 `/archive/:vpsId`。`/archive/:vpsId` 先读取 `getVPSArchiveReview(vpsId)`，只有 review 返回 `cancelled` / `archived` 才继续读取 `getVPSTimeline(vpsId)` 与 `listSubscriptions({vps_id, asset_scope:'all', sort:'renew_at', order:'asc'})`；其他 lifecycle 必须 `replace` 跳回 `/vps/:vpsId`。详情页从 archive review 读取 services、domains、monitoring links 和 target links，不依赖普通 `/api/targets` 列表。详情页只读展示身份说明、上方摘要卡、用户记录、续费/价格/规格/IP 历史、订阅/服务/域名明细、底部全宽监控与 Target 历史；用户记录必须排在订阅/服务/域名明细之前。`archived` 可显示受控恢复入口，`cancelled` 不显示恢复入口；不得出现编辑、取消、添加、创建或关联按钮。多币种订阅摘要必须按币种分开显示（例如 `USD 24.00/月 + EUR 9.00/月`），不得把不同币种相加后套用单一币种。
- URL-state: VPS inventory 支持 `view=all|renewal|unreviewed|unlinked|missing_subscription|missing_facts|cancellation_attention`，并继续支持 `provider_id`、`lifecycle_status`、`usage_status`、`renewal_decision`；lifecycle filter 只提供 current 生命周期，不提供 `cancelled` / `archived` 选项。Target inventory 支持 `coverage_gap=1` 表达执行监控实例覆盖缺口，供 TargetsSupportSurface 快捷入口和 Dashboard/资产证据支撑场景承接。
- `VPSAssetRecord.active_monitoring_instance_link_count` 只能展示 MonitoringInstance 关联数量或未关联状态，**不得**展示 linked monitoring instance health、最近心跳或异常，除非后端 contract 新增并同步类型/测试。
- VPS inventory quick views 中 derived filters 在前端执行即可；40+ VPS 量级不引入新缓存/状态库，不新增 API 字段。
- Dashboard 深链进入 VPS 页时，query 必须被页面首屏可见的 tab/chip/drawer 状态承接；不能静默丢弃。
- 常规业务对象关联输入不得要求用户复制内部 ID：VPS facts 的 Provider、VPS↔MonitoringInstance link 的 MonitoringInstance、VPS service/domain 的 Target、domain 的 Service 都应使用页面加载的数据选择器，并保留“未关联/不关联”选项。选择器为空或加载失败时必须给出明确说明和到对应列表/创建流程的入口；选择监控实例/Target 只创建资产引用或链接，不隐式修改 MonitoringInstance/Target/Agent/ProbeItem 语义。
- VPS inventory subscriptions empty：行级展示 `缺订阅`，quick view `缺订阅` 可筛出对应 VPS
- VPS inventory URL has unsupported `view`：降级为 `all`，下次用户操作时写回合法 query
- `/archive` loads archived subscriptions in multiple currencies：订阅历史行逐条展示原币种价格；摘要按币种分组，不跨币种求和
- `/archive` renders archive list：请求 `asset_scope=historical` 只读展示已取消 / 已归档 VPS 摘要，不自动请求单台 services/domains/timeline；行级入口进入 `/archive/:vpsId`
- `/archive/:vpsId` renders detail context：只读展示 archive review、timeline、全量订阅历史、services、domains、monitoring links 和 Target links；用户记录排在订阅/服务/域名明细之前；archived 才显示受控恢复，cancelled 不显示恢复；不得出现编辑、取消、添加、创建或关联按钮
- selector candidate list is empty：表单保留空值能力，显示去对应列表/创建流程的 Link/action，不要求手输内部 ID
- selector list request fails：表单显示局部错误/提示，已保存主页面数据仍可查看；不得把加载失败当成真实“无候选”
- Good: `/vps?view=unlinked&renewal_decision=unreviewed` 首屏显示 `视图: 未关联` 和 `续费: 未评估` chips，列表只显示同时满足条件的 rows。
- Good: VPS 详情打开 MonitoringInstance link 对话框 时懒加载 `listMonitoringInstances()`，用 `选择监控实例` selector 展示名称、ID、provider、生命周期和健康状态。
- Good: VPS 详情无订阅时显示“快速创建订阅”，调用 `/api/vps/{vps_id}/subscriptions`，表单只收账单事实，不出现订阅状态。
- Good: VPS 详情无监控实例时显示“创建并接入 agent”，调用 `/api/vps/{vps_id}/monitoring-instances`，后端从 VPS 派生身份字段，成功后跳转 MonitoringInstance onboarding。
- Bad: Dashboard 或 VPSPage 从 `abnormal_linked_vps_count` 反推单台 VPS linked monitoring instance health。
- Bad: 在 VPS 详情表单里让用户输入 `mi_...`、`tg_...`、`svc_...` 作为常规路径，且不给候选列表或落地入口。
- Bad: 让用户先去 Monitoring 列表创建监控实例、重复填写名称 / 地区 / 服务商，再回 VPS 详情关联，作为普通接入路径。
- `VPSPage.test.tsx`: initial fetch、quick view、active chips、高级筛选 drawer、client-side filtering、订阅/监控实例/资料质量展示、创建 VPS 流程和 provider selector 可访问标签。
- `VPSDetailPage.test.tsx`: Provider/MonitoringInstance/Target/Service selectors 的候选加载、空态/错误提示、提交 payload 仍只发送被选 ID 或空值。
