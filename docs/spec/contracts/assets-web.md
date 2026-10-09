# 资产 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

- VPS 详情页所有 await 后更改 notice/draft/dialog/navigation 的写操作都必须受 VPS identity、view generation 和 transport owner 保护。不得只给 facts/decision 加归属。晚到的 A 结果不得关闭 B 的界面或导航离开 B；服务端请求不必取消，但 finally 仅释放自身 token。
- Asset Ledger 共享值必须跟后端同步：`AssetScope = 'current'|'archived'|'all'`；VPS 生命周期仅 active/archived，用途使用自由多选 `usage_tags[]`，续费意向仅 unreviewed/keep/cancel。`RenewalMode = 'auto'|'manual'|'auto_cancelled'`；抽奖、赠送等获取来源使用 VPS `acquisition_source`，不能作为续费方式。

## 生命周期处理与恢复入口

- 管理中 VPS 始终提供“结束使用并归档”，不受续费意向控制；归档资产展示历史与带原因恢复，不能重复显示归档资格。恢复默认用途为闲置，不恢复历史监控、会话权限、关联、探测或命令。
- 单次归档确认展示「会发生什么」「会影响」和「需要知道」，有阻止项时展示阻止原因并隐藏原因与名称确认。已接入对象在「安全观察」中展示接收链路、最早可归档时间和各实例最后可信在线时间，每项一次，时间用本地日期时间。`online_evidence.manual_confirmation_required` 的从未接入对象不展示接收链路、健康起点、最早可归档时间或「等待安全观察」，也不重复「从未形成有效 Agent 会话」说明；改为说明不用等待 180 分钟安全观察，并必须勾选与说明同行的「这台 VPS 从未接入过 Agent。我已确认它不再使用。」。没有服务关联、没有域名关联不单独重复，影响行已写「无」；订阅缺失和潜在扣费说明保留。带确认名称、原因、预览摘要、幂等键提交；名称框不预填，提示需要完整匹配。无订阅或未解决跟进不阻止归档；潜在扣费保留提醒，共享对象不能随此 VPS 结束。没有强制归档或自动预约。阻止详情展示对象名称和当前状态，不展示内部 code。
- stale preview 保留确认名称与原因草稿，刷新事实与摘要；不自动重提，最新条件必须重新确认。
- 归档 409 优先使用响应 review/blocker_details；没有 review 才刷新，刷新失败明确未知。按 object_type/ID 构造受控本地路由，不接受服务端任意链接。已接入对象的在线证据展示涉及实例、最后可信在线、连续健康起点和最早归档时间，且各时间只出现一次；从未接入对象不展示这些观察字段。
- 恢复后提供用途/续费整理和显式重新接入；历史监控仍退役。接入失败明确显示尚未接入，可重试；不会复活旧关系。操作完成后刷新当前事实与资格，迟到回包不覆盖较新结果。
- 开始迁移提交非空原因到 start-migration，不把普通 PATCH 包装为迁移审计。服务/域名状态纠正必须显式选择 active/paused/retired 并填写原因。
- 表单使用精确机器值及当前值，禁止用中文展示词回写、把用途当作主生命周期，或把未编辑状态降为默认值。
- MI 管理按 action_reviews 分别展示能力/阻塞/警告；Target lifecycle-review 和 MI review 的共享确认不能被列表或批量入口绕过。所有请求复用既有 mutation/read owner 与 generation，迟到回包不覆盖其他路由。
- MI/Target 详情危险动作（含运行暂停）只要已读取 review，就必须随请求发送该 preview_digest；confirm_shared_impact 仅在需要共享确认时反映勾选，是否要求勾选与是否发送摘要独立判断，review 未加载或失败时禁止提交。确认参数须经各层回调完整转发。摘要过期或共享确认冲突时重新读取 review、清空旧共享确认、保留原因等草稿并提示，不重用旧缓存或自动重提。

### VPS 详情 agent 接入/升级复用已有 MonitoringInstance

#### 1. Scope / Trigger

- Trigger: 修改 VPS 详情页普通 agent 接入入口、`workbench=monitoring` / `workbench=monitoring-instance-create` 深链、VPS ↔ MonitoringInstance link 写路径、或 MonitoringInstance onboarding 文案。
- 目标：VPS 已有 active MonitoringInstance link 时，普通 agent 接入入口必须复用现有监控实例进入升级/重新接入流程，避免误创建第二个 active 监控实例。

#### 2. Signatures

- Frontend deep link: `/vps/{vps_id}?workbench=monitoring` and `/vps/{vps_id}?workbench=monitoring-instance-create`。
- Frontend upgrade target: `/monitoring/{monitoring_instance_id}?onboarding=1&return_vps={vps_id}`。
- Backend create API: `POST /api/vps/{vps_id}/monitoring-instances`。
- MonitoringInstance 永久归属创建时 VPS；普通孤立创建、跨 VPS 转绑和共享实例入口已移除。
- Backend domain error: `assetlinks.ErrVPSActiveMonitoringInstanceExists` maps to HTTP 409.

#### 3. Contracts

- 0 active links: VPS detail may show `创建并接入 agent`; submit may call `createVPSMonitoringInstance`, then navigate to MonitoringInstance onboarding.
- 1 active link: VPS detail must show `升级/重新接入 agent`; clicking or opening either monitoring workbench deep link must navigate to the existing MonitoringInstance onboarding and must not call the create API.
- Each VPS has at most one current instance, enforced by a database unique constraint. Inconsistent duplicate facts show an error without offering transfer, sharing or historical deletion.
- Backend create write paths must lock/check the current instance before inserting and return 409 on conflicts. The create path must not insert an orphan `monitoring_instances` row on conflict.
- Monitoring detail onboarding keeps using `issueMonitoringInstanceInstallCommand`; only copy/title changes between `接入 agent` and `升级/重新接入 agent` based on bound/observed state.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| VPS has 0 active links and user opens monitoring workbench | Open create drawer; create API allowed |
| VPS has 1 active link and user opens monitoring workbench | Navigate to existing `/monitoring/{id}?onboarding=1&return_vps={vps_id}` |
| VPS has >1 active links | Show manual duplicate review warning; no create/link CTA |
| Create API sees existing current instance | 409 `vps active monitoring instance exists` |
| Create API sees existing active link | No `monitoring_instances` insert before returning error |

#### 5. Good/Base/Bad Cases

- Good: already-connected VPS upgrades agent by reusing its existing MonitoringInstance and generating a fresh install command there.
- Base: brand-new VPS without active link creates one MonitoringInstance and enters onboarding.
- Bad: a button labeled `创建并接入 agent` on an already-linked VPS calls `POST /api/vps/{id}/monitoring-instances` and creates a second active monitoring instance.
- Bad: only the frontend blocks duplicate creation while backend creation still allows another current instance.

#### 6. Tests Required

- `VPSDetailPage.test.tsx`: 0/1/multiple active-link behavior, monitoring workbench deep-link branching, and no create API call when reusing an active link.
- `MonitoringDetailPage.test.tsx`: bound or already-observed instances use `升级/重新接入 agent` wording while still issuing install commands through the existing API.
- Handler/store Go tests: creation returns 409 for an existing current instance; no orphan is inserted; cross-VPS ownership changes are rejected.

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

服务和域名身份可由多台 VPS 共同关联。对象状态与某台 VPS 的关联起止状态分别展示；不能把 scoped 投影的 vps_id 当作对象的永久单一所有权。关联 DTO 包含 association_id/object_id/vps_id、target_id/service_id、address/port、started_at/ended_at、end_reason/ended_by 和结束时 snapshot。结束指定关联后保留历史，不删除共享对象；详情支持创建及选择已有对象，关联 Target 归属不明确时仅产生核对事项。

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
- VPS 详情可加载 `getVPSArchiveReview(vpsId)` 展示结束使用影响与在线安全条件；`?workbench=archive` 打开同一预览确认。
- 订阅非活跃不代表 VPS 已结束；页面展示独立账单事实、有效期和跟进事项，由用户显式决定是否结束使用。
- `VPSAssetDetail.monitoring_instance_links` 可以在 Detail 页展示 health、heartbeat、active incident count 和 issue summary，因为后端 detail contract 已返回这些字段；这不改变 `VPSAssetRecord.active_monitoring_instance_link_count` 在列表页只能代表数量的限制。
- 决策、facts、experience、service/domain 创建与关联、归档及监控退役的复杂输入使用受控 Modal。关闭对话框后保存成功 notice 保留在主页面；状态和资格重新读取。
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
- If the selected asset is outside the filtered results, ledger states 选中项不在当前筛选结果中 above the list summary (or above 暂无匹配的 VPS when nothing matches) and workbench shows no accordion, rather than silently inspecting another asset. Clearing filters can restore the selection.
- Subscription loading and failure are not missing-subscription evidence. Do not infer runtime health, successful sync, or trends from the asset ledger.
- Browser verification must cover continuous text entry, view switching, selection, history, and narrow screens; a single synthetic change event does not prove typing works.

### List, quick inspection, and independent detail

- The table view prioritizes scanning an inventory of tens of machines. Its quick inspector starts collapsed and expands directly after the selected row, inside the list scroller. There is no bottom inspector strip. Expanded inspection is limited to identity, business/renewal facts, monitoring association, and available observation evidence; it must not become a second full detail page. On narrow screens, the list remains independently scrollable.
- The directory (ledger) inspector is the richer reading surface and never leaves the desktop pane blank while the list has results. Without a selection it summarizes the currently visible list: a bounded 续费排期 (the five nearest primary-subscription renewal dates by shared day counting, overdue first, each a button that selects its asset; later and undated assets are counted, not listed, and an entirely undated list is stated once), then 续费意向, 用途 (multi-tag counts, one per asset per tag, untagged as 未标注用途), 服务商 and 地区 counts, where longer distributions show four items plus a counted 其余 N 类 remainder that cannot be confused with a user-entered 其他. Subscription loading or failure is stated as such, never as missing renewal dates, and the summary adds no requests or cost interpretation. The selected view states the renewal date once, inside the subscription fact. With a selection, a compact identity row carries the name, lifecycle/renewal status and the 打开 VPS 详情 link, followed by any cancellation attention and the 资产身份 / 经营与续费 / 监控与证据 groups plus full-width notes. The groups sit in two columns (经营与续费 spanning both rows) when the inspector is at least 44rem wide and stack in reading order otherwise; the list summary uses two columns from 28rem and four count columns from 60rem.
- Both inventory views enter the same canonical `/vps/:id` workspace. Both capability modes use a compact identity/action header beneath the sole validated top-bar inventory return. Organize by content rather than a permanent main/aside split: flexible asset/connection facts and a bounded billing summary form the first zone; observations, named service/domain lists and recent activity remain independent. The billing area is approximately 320–420px only while beside sufficiently wide facts, and stacks when needed. The route uses a comfortable 1600px ceiling, 16px padding, 14px gaps and natural heights. Keep shared palette and fine borders without decorative card shadows. The closed 页面目录 remains subordinate to real business tabs and preserves hash positioning/focus.
- Use the true subscription name and put its weak record ID on a separate subordinate line, distinct from asset identity. Combine amount and normalized period, then keep absolute renewal/expiry date and the VPS decision close; do not interpret 保留 as an automated resource action. Labeled record updates do not imply staleness. Existing scoped APIs or loaded legacy collections remain the source; loading/error are not empty collections. Resource rows have two content layers: name/status/explicit details action, then address/type/port/purpose. The row itself is not a button; full values and independent copy/links must remain usable. Copy stays adjacent to full wrapping IP/SSH values and announces success/failure; missing values are not copyable. Compact desktop controls retain adequate touch targets on narrow/coarse-pointer surfaces.
- Runtime observation rows keep scope, known result, source time and real actions nearby. Remove exact same-object status repetition without erasing distinct results or interpreting lifecycle as health. Missing/stale/unavailable evidence cannot expand a healthy claim; IP query failures do not erase valid monitoring or known historical IP findings. Keep not-configured, failed, absent and historical facts distinct according to the contract. Do not invent scores, denominators, configuration routes or incidents. Activity highlights action, event time and type; shorten only an exact current-asset prefix when distinguishing other-object information is retained, keeping the full accessible title. The top-bar 系统摘要 belongs to the fleet snapshot and is not asset health.
- Use one shared observation-row layout for project, primary conclusion, explanation, data time, and actions across capability modes. Desktop columns share stable tracks; narrow layouts reflow into consistent labelled slots. Keep long source errors in a subordinate row under their object, not interleaved with the main conclusion or action track. Existing result, latest read outcome, and source age are independent: name retained content only when it exists, and never describe visible retained evidence as wholly unavailable. Activity data time and event time are different fields.
- Shared fact rendering preserves all facts supplied by each existing source, including importance and labels already present in overview identity. Short fields may share columns; SSH and notes retain continuous width. Do not fabricate fields absent from a source or add requests to make capability modes identical. Successful empty resource groups use lightweight summaries; unavailable is not empty, and an empty report does not disable a real authorized report destination.
- The six bounded VPS detail dialogs share three content templates inside the existing Modal: short renewal decision, structured VPS/subscription forms, and associated-object reading. Keep the accepted main workspace unchanged. Use one task title and light asset context; renewal decisions describe saved intent, never supplier execution. Show current versus draft decision only when they differ.
- Service, domain, validity, monitoring-link, monitoring-create, and legacy experience forms reuse `.vps-form` / `VPSFormSection` and Modal title+fixed footer; do not repeat the task title in an inner `asset-operation-form__header`. Keep current subscription facts and prerequisite warnings. Do not delete shared `.vps-create-form*` or decision-work `asset-operation-form__header` styles still used outside VPS dialogs. Inventory list and inspector status use `LifecycleBadge` / `UsageBadge` / `RenewalBadge`; ledger directory may omit usage for density, and the table view shows usage as plain secondary text via `usageLabel`.
- 空 `usage_tags` 统一由 `usageLabel` 显示为「未标注用途」，任何 UsageBadge 或用途文本都不得渲染空徽章或空分隔段。表格视图行按 机器 / 位置与规格 / 状态与用途 / 续费 / 监控与证据 五列扫描：机器名前有展开指示，每格一个主事实加一行弱化说明；生命周期、续费意向和 IP 质量用状态点加文字，不用胶囊按钮样式，状态点仅作装饰（aria-hidden），不得以健康词汇重复朗读；「规格未填写」「未关联监控」「未采集」「无订阅」等空事实以弱化文字呈现；30 天内续费日和「自动续费待核对」用警示色简短提示，完整原因在展开区；续费日已过时在日期后直接写出「已逾期 N 天」（告警色，状态点 `aria-hidden`），不能只放在悬停提示或读屏文字里；续费剩余天数（含 30 天提示与逾期）统一按 UTC 日历日计算（`daysUntilDate`），与后端 30 天计数、`overdue_renewals`、`renewal.overdue.v1` 一致。多段事实以「 · 」分隔并只在分段处换行。展开的快速检查不是嵌套卡片，也不重复行内摘要：所在行与展开区共用细强调边，顶部一行放完整取消提醒与「打开 VPS 详情」链接（保证窄屏展开后操作立即可见），其下为 资产身份（IPv4/IPv6、SSH `user@host:port`、服务商、位置）、经营与续费（用途、续费意向、VPS 有效期、订阅明细）、监控与证据（监控实例、IP 质量）三组标签-值列表，分组标题为 h2；画布宽度 ≥ 64rem 时三组并列，50–64rem 之间两列排布。行与分组的堆叠由 `.vps-canvas` 容器宽度（≤ 50rem）决定，而不是视口宽度；堆叠时每格显示字段小标题。
- Match width to content: decision 380px, facts 680px, services 520px, domains 560px, and subscription/monitoring reading 780px. Facts and service/domain reading become full-viewport sheets on narrow screens; other dialogs retain their existing gutters. Keep a fixed header, an independently scrolling body, and fixed actions only for actual forms. Subscription facts remain a new record, not an edit; preserve their existing fields and behavior.
- Service/domain reading keeps complete collections, neutral record status (not observed health), names, notes, labels and subordinate IDs. Show service URLs as full monospaced text with adjacent copy and HTTP(S)-only open actions on the following row; neither read panel has a save/cancel footer or a pretend edit action. Domains may enrich service IDs from the scoped service API; missing optional metadata must not block reading or reuse another VPS’s data. Existing legacy create actions remain separate. Monitoring reading and all ownership/focus/scroll safeguards are unchanged.
- VPS overview and lifecycle management remain available when Records is disabled. Only the activity section becomes unavailable; the core profile must not query Records projections or report an existing VPS as missing because that optional capability is off.
- Freshness belongs beside each object's own source timestamp, not inside its conclusion. Adjacent view and refresh actions share a wide-screen row without changing ordinary single-action columns. Overview refresh/retry rereads the overview; it does not request agent sync or a probe. Activity source time denotes latest visible recorded intake, independently of event time.
- Empty navigation retains the current VPS: a subscription collection is not an existing subscription object, zero monitoring opens association status rather than a fabricated instance, and a report route remains available when its real capability permits reading empty/history/error states. Label historical report entry only from known historical evidence. A lone unlinked-monitoring notice states its reason once, its observation impact, and the existing action or read-only explanation.
- 决定不续费（`renewal_decision=cancel`）后，概览“需要关注”不再发 `renewal.due.soon.v1` / `renewal.overdue.v1`：登记续费日只是到期日，不是续费待办。改由 `renewal.cancel.auto_renew_unverified.v1`（「决定不续费，自动续费待核对」，主操作「核对自动续费」打开续费决策面板）提示 `auto_renew_check` 为 unchecked（notice）或 enabled（warning，可能继续扣费），不受续费窗口限制，且不带 `event_at`（当前待核对状态，远期续费日不得把它排到实时告警前面）；核对为 disabled/never_enabled/unsupported 后不再提示。unreviewed/keep 的续费临近/逾期规则不变。
- Cancellation plans are not supplier cancellation confirmations. Keep registered renewal/billing dates separate from the VPS plan. Preserve the center's overall conclusion and name a pending cancellation plan as an additional attention basis when applicable, rather than attributing the difference from incomplete observations solely to missing IP evidence.
- Pending cancellation labels registered renewal and billing-period dates consistently for primary and extra subscriptions; trial expiry remains trial expiry. Report entry names distinguish actual report/history evidence from the generic results destination; successful unconfigured/empty responses are not read failures.
- An explicitly disabled optional IP-quality source with a ready section is outside the enabled-observation assessment, not a failed source. Keep its local 未启用 label and state the limited scope beside an otherwise healthy judgment. Stale or unavailable source metadata still limits the judgment, and known adverse findings must not be downgraded.
- Full-context reading uses the independent page. Management entries are grouped by business/billing, runtime, associations, and applicable lifecycle operations; grouping must not remove a destination or callback. Existing bounded edits, onboarding panels, and dangerous-action confirmations remain shared management operations, with their capability, freshness, read-only, and write-ownership checks preserved. The overview-enabled and capability-off presentations do not define separate business workflows.
- Inventory entries carry the current `/vps` query in React Router history state `vpsInventoryHref`. The top-bar return accepts only `/vps` with optional search parameters; absent or invalid context falls back to `/vps`. Onboarding query consumption, in-page sections, VPS activity/records/evidence navigation, and activity filter/retry replacements preserve that state. This is navigation context, not another persisted preference.
- Browser acceptance covers both layouts in global dark/light themes, selecting from a 30-asset inventory, collapsing/reopening inspection, opening actual independent detail, accessing and closing management, and returning with view/search/filter/selection intact. Entering the base detail page resets the shared main scroller; explicit hash navigation retains its section target. Fixed top bars must not cover return controls or in-page section targets.

## VPS facts editor

The shared editor is the create (添加 VPS) and edit facts form. It shows identity/provider, country, city/IPv4, free-form multi-select usage tags, importance, independent ordinary labels and notes first. Suggestions and existing usage tags remain reusable; a new purpose can be entered directly. Usage never changes lifecycle or monitoring control. Put lower-frequency facts in one default-collapsed 可选设置 without nested categories. IPv6 and custom SSH host/port are hidden until shown; existing configuration initializes them visible, and hiding never erases values. Editing IPv4 derives an automatic SSH host while preserving a custom host. The country field retains its editable in-flow combo, grouped common locations, full Chinese/English/code search and custom values. Importance remains a native select. Keep header and feedback/save/cancel footer fixed while the body scrolls. Preserve inline provider creation, reset/error/pending/submit, real PATCH, snapshots, conflict recovery and write ownership. Facts dialog chrome (680px desktop, fullscreen on narrow screens) lives only in the registered VPS style owner in `web/css-owners.json`.

Archive is a historical VPS ledger with a compact identity/status/time/cost scanning table and an independent detail workspace. Distinguish missing archive time from update time. Organize evidence by identity, billing, monitoring sessions, service/domain associations and dated records; sort by absolute event time. Show snapshots at archive separately from current shared-object facts. Subscription and timeline failures stay local with source-specific retries. Known billing evidence may remain while a separate subscription read fails; a successful empty list replaces it. Wide tables have uniquely named keyboard-focusable local scroll regions; narrow headers/actions wrap without clipping. Archived assets offer explicit restore-to-idle confirmation, supplemental bills/refunds/notes/migration outcomes/evidence and follow-up resolution with reasons and audit. Pending restoration cannot be dismissed or duplicated; success returns to VPS detail without reopening associations, commands or old session permissions. Obsolete requests must not overwrite or navigate a later visit.

Domain reading retains its records and association service IDs when optional service-name enrichment fails. Announce that failure locally and retry only the scoped service read; ignore responses belonging to a closed panel or another VPS. Archive/retirement uses a compact impact summary and one confirmation heading without bypassing authoritative preview or audit.

- **生命周期入口稳定可达**：每台管理中 VPS 的详情管理菜单始终提供“结束使用并归档”，不能因续费意向或当前判断稳定而隐藏。点击后打开预览确认，提交前重新校验。测试覆盖稳定资产、决定不续费、暂停/退役监控及 deep link；VPS 列表继续只读。
- **迁移仅人工计划和跟进**：记录来源、目标与结果，不自动迁移服务或结束旧 VPS，不暗示存在自动执行工作台。迁移表达为新增目标关联、结束来源关联；结束旧资源须独立确认归档。
- **当前关注状态归入顶部判断，不铺中部提醒条**：VPS 详情页里的“运行观测需要核对”、缺订阅、缺运行观测、订阅读取失败、IP 质量暂不可用、续费临期 / 自动续费取消等当前需要用户处理或核对的状态，必须进入概览顶部的“需要关注”区（`VPSOverviewAnomalies`，数据来自概览接口的 `anomalies`）。页面中段的“资产信息”“订阅与续费”“运行观测”“服务与域名”只承载详情摘要和管理入口，不再渲染 `VPSContextActionPanel` / `vps-detail-context-action` 这类横条。多个关注状态必须可并列展示，不能被单个 `primaryAction` 覆盖；稳定状态下不展示额外列表。概览 `summary.monitoring.section.reason_code=monitoring_heartbeat_stale` 时，`monitoring.health.abnormal.v1` 改写为一条「agent 已失联」并写出「最后心跳 <时间>」（取 `last_success_at`），同源的 `monitoring.incidents.open.v1` 合并进该条：不再单列，但其「查看事件」入口按原 rule_id 校验后保留在同一条里；原始心跳周期文案收进诊断信息。没有健康异常项或心跳未超时时，两条原样并列。

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

- VPS inventory data: `VPSPage` 拉取 current `listVPSAssets()`、`listProviders()` 和 current `listSubscriptions({ sort: 'renew_at', order: 'asc' })`，在前端按 URL-state 做 derived quick views；归档 VPS 不在主库存页展示，通过 `/archive` 查看历史。
- Archive data: `/archive` 显式请求 `asset_scope:'archived'` 的 VPS 和订阅形成摘要，不自动拉单台 detail/services/domains/timeline。点击进入 `/archive/:vpsId`，只有 review 返回 archived 才读取 timeline 与 `asset_scope:'all'` 订阅历史；管理中资源 replace 回 `/vps/:vpsId`。详情读取历史关联、历次监控、Target、业务记录和跟进事项，不依赖当前 Target 列表。提供明确的历史补录、跟进及恢复操作，不允许普通当前资源编辑或自动重开关联。多币种摘要分币种显示，不跨币种求和。
- Archive detail layout: 首屏依次为身份与归档时间、归档摘要（服役时长、末期月费、续费决策、服务商自动续费核对、留存资产），以及回看标签与侧栏。回看标签按「用户记录」（默认）→ 账单与订阅 → 服务与域名 → 监控与探测 → 变更时间线排列，计数来自已加载事实；时间线合并续费决策、价格、规格和 IP 变化并按时间倒序。侧栏「归档后待办」承载自动续费核对、归档后备注与跟进事项（逐条处理原因；迁移跟进的目标与结果独立填写），「访问与规格」只读展示服务商、位置、IP 与 SSH。自动续费核对为 unchecked/enabled 时摘要以提醒色提示可能仍在扣费。空分组显示一行说明，不渲染大面积空表；timeline/订阅加载失败只在所属标签内报错并可独立重试。不重复显示归档资格。
- URL-state: VPS inventory 支持 `view=all|renewal|unreviewed|unlinked|missing_subscription|missing_facts|cancellation_attention`，及 `provider_id`、`lifecycle_status`、`usage_tag`、`renewal_decision`；旧 usage_status 不再是合法筛选。Target inventory 支持 `coverage_gap=1` 表达执行监控实例覆盖缺口。
- `VPSAssetRecord.active_monitoring_instance_link_count` 只能展示 MonitoringInstance 关联数量或未关联状态，**不得**展示 linked monitoring instance health、最近心跳或异常，除非后端 contract 新增并同步类型/测试。
- VPS inventory quick views 中 derived filters 在前端执行即可；40+ VPS 量级不引入新缓存/状态库，不新增 API 字段。
- Dashboard 深链进入 VPS 页时，query 必须被页面首屏可见的 tab/chip/drawer 状态承接；不能静默丢弃。
- 常规关联输入不得要求用户复制内部 ID：Provider、已有服务/域名、关联的 Target/Service 使用可读选择器；空候选或加载失败有明确说明和合法创建入口。监控实例不提供跨 VPS 选择器；新实例只能在所属 VPS 下创建。
- VPS inventory subscriptions empty：行级展示 `缺订阅`，quick view `缺订阅` 可筛出对应 VPS
- VPS inventory URL has unsupported `view`：降级为 `all`，下次用户操作时写回合法 query
- `/archive` loads archived subscriptions in multiple currencies：订阅历史行逐条展示原币种价格；摘要按币种分组，不跨币种求和
- `/archive` renders archive list：请求 `asset_scope=archived` 展示归档摘要，不自动请求单台 services/domains/timeline；行级入口进入 `/archive/:vpsId`
- `/archive/:vpsId` renders detail context：历史事实与共享对象当前事实分开；支持明确的历史补录、跟进处理和受控恢复，不恢复旧关联或监控权限
- selector candidate list is empty：表单保留空值能力，显示去对应列表/创建流程的 Link/action，不要求手输内部 ID
- selector list request fails：表单显示局部错误/提示，已保存主页面数据仍可查看；不得把加载失败当成真实“无候选”
- Good: `/vps?view=unlinked&renewal_decision=unreviewed` 首屏显示 `视图: 未关联` 和 `续费: 未评估` chips，列表只显示同时满足条件的 rows。
- Good: VPS 当前实例在详情直接可达；历史退役实例留在历史区，显式重新接入保持永久 VPS 归属。
- Good: VPS 详情无订阅时显示“快速创建订阅”，调用 `/api/vps/{vps_id}/subscriptions`，表单只收账单事实，不出现订阅状态。
- Good: VPS 详情无监控实例时显示“创建并接入 agent”，调用 `/api/vps/{vps_id}/monitoring-instances`，后端从 VPS 派生身份字段，成功后跳转 MonitoringInstance onboarding。
- Bad: Dashboard 或 VPSPage 从 `abnormal_linked_vps_count` 反推单台 VPS linked monitoring instance health。
- Bad: 在 VPS 详情表单里让用户输入 `mi_...`、`tg_...`、`svc_...` 作为常规路径，且不给候选列表或落地入口。
- Bad: 让用户先去 Monitoring 列表创建监控实例、重复填写名称 / 地区 / 服务商，再回 VPS 详情关联，作为普通接入路径。
- `VPSPage.test.tsx`: initial fetch、quick view、active chips、高级筛选 drawer、client-side filtering、订阅/监控实例/资料质量展示、创建 VPS 流程和 provider selector 可访问标签。
- `VPSDetailPage.test.tsx`: Provider/MonitoringInstance/Target/Service selectors 的候选加载、空态/错误提示、提交 payload 仍只发送被选 ID 或空值。
