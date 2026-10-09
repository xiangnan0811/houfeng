# 监控 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

### 全新安装的真实生命周期验收

- `scripts/test-vps-lifecycle-live.sh` 构建匹配的 Center/Agent，在独立 PostgreSQL 中验证首次接入、暂停后最小在线证据、退役、同实例新会话、共享关联、归档恢复及归档后上线跟进。受控时钟夹具与真实经过 180 分钟的验收必须分别报告。
- `scripts/test-vps-state-local-live.sh` 构建当前 Web/Center 并创建独立数据库运行 `web/e2e/local-live/vps-state-live.spec.ts`。浏览器真实登录，验证不可转移监控归属、续费意向不改账单、共享服务/域名关联、失效归档摘要、详情页人工例外归档、历史恢复、显式重新接入，并保存桌面/移动与暗亮主题截图。该入口不安装依赖，不模拟 Agent sync，也不能替代上述真实 Agent 验收。
- 已运行的独立 loopback Center 可通过 `LOCAL_LIVE_CENTER_URL`、`LOCAL_LIVE_USERNAME`、`LOCAL_LIVE_PASSWORD` 环境变量传给浏览器脚本；只允许专用可写验收环境，不复用其他正在验收的实例。凭据不写入证据。脚本默认启动自己的隔离环境；常规 Chromium 配置继续排除此真实后端测试。

### Runtime WebSocket 持续会话授权

- runtime stream 使用真实 Cookie 经 RequireSession 认证及授权后写入的可信 session context。握手前校验会话；缺 context 返回 401，缺 validator 或权威读取故障返回 500；不存在/过期会话返回 401。握手失败不启动订阅或 watcher，同源规则不变。
- 已连接后每条满足原实例 fingerprint/epoch/time 条件的消息在写入前重新验证会话；实例与会话查询各最多 2 秒，socket 写入仍最多 5 秒。独立 watcher 每 5 秒执行不续期的会话验证，繁忙消息、空闲或慢写均不能绕过它。
- 撤销、自然过期或校验故障均取消 stream 并 CloseNow，正常调度下周期加查询最多 7 秒发起关闭，真实端到端验收给 10 秒上限。清理顺序为 cancel、强制关闭、等待 watcher 退出、关闭订阅；不等待 graceful close 握手。
- 每次读取最新持久化 expiry，正常 HTTP 续期继续有效；改密保留当前 A 会话/连接，撤销 B 只关闭 B。被动验证不修改 last_seen_at/expires_at。已经通过校验并在途的旧帧不追回，不承诺撤销与网络发送全局原子；不持数据库锁跨网络。
- 真实 PostgreSQL/RequireSession/WebSocket 回归必须观察首帧、撤销后连接终止、保留会话继续收到合法新样本，并覆盖空闲过期、续期、查询失败、客户端断开与订阅清理；不能用直接 handler mock 替代认证证据。

### Incident threshold settings contract

#### 1. Scope / Trigger

- Trigger: 修改 `IncidentDefaults` / `IncidentDefaultsOverride` 前端类型、Settings 页监控策略表单、`web/src/config/thresholds.ts`、监控列表/详情阈值展示，或后端 `internal/center/settings.IncidentDefaults`。
- 目标：前端提交、API 类型和监控图表阈值语义必须与后端 settings 校验一致，不能让用户保存或看到倒序等级。

#### 2. Signatures

- API response/request fields: `incident_defaults.heartbeat_interval_seconds`、`stale_threshold_intervals`、`sweep_interval_seconds`、`notify_on_*`、`cpu_warning_pct`、`cpu_alert_pct`、`cpu_critical_pct`、`mem_*`、`disk_*`、`inode_*`、`iowait_warning_pct`、`iowait_critical_pct`、`load5_warning`、`load5_critical`。
- Settings page builder: `web/src/pages/SettingsPage.tsx` `buildIncidentDefaults(form)`。
- Runtime presentation resolver: `web/src/config/thresholds.ts` `resolveThresholds(incidentDefaults)`.

#### 3. Contracts

- CPU / 内存 / 磁盘 / Inode 三段阈值必须满足 `关注 < 告警 < 严重`，对应后端 `warning < alert < critical`。
- IOWait / Load5 只有关注与严重输入，必须满足 `关注 < 严重`；告警阈值由中点派生，不在 Settings 页暴露独立输入。
- `stale_threshold_intervals = N` 表示首次失联事件的精确边界；默认 `N=12`，按默认 5 秒心跳约 60 秒。后端在 `2N` 升级为告警、`4N` 升级为严重，并要求连续 3 次不同 batch 的非回填实时心跳稳定恢复。
- `IncidentDefaultsSection` 必须直接解释首次 `N`、默认 `12≈60s`、`2N/4N` 升级和 3 次实时心跳恢复；继续复用现有输入和 Settings façade，不新增字段、Context、依赖或组件内 direct fetch。
- 自定义阈值必须按原值 round-trip；例如 GET 返回 `N=20`，即使用户只改扫描间隔，PUT 仍提交 `20`。只把代表全局默认配置的 fixture 从 3 更新为 12，包括 `scripts/visual_evidence.py` 当前 `/api/settings` browser-sanity mock；显式 override/迁移反例中的 3 不得机械替换。
- Settings 页本地校验失败时必须显示中文错误并阻止 `PUT /api/settings`。
- `resolveThresholds` 是展示层防御边界：即使 API 返回历史脏数据，倒序 metric 也必须回退到 `DEFAULT_THRESHOLDS`，避免图表阈值线和等级 tooltip 反向。
- 修改默认阈值时必须同时检查 `web/src/config/thresholds.ts` 与后端 `internal/center/settings/types.go` 的默认值是否一致。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| 用户输入 CPU `95 / 90 / 96` | Settings 页显示 `CPU 阈值必须满足 关注 < 告警 < 严重。`，不提交 |
| 用户输入 Load5 `4 / 4` | Settings 页显示 `Load5 阈值必须满足 关注 < 严重。`，不提交 |
| 默认 settings 返回 `stale_threshold_intervals=12` | 输入显示 12，并展示约 60 秒、2N/4N 与三次实时心跳恢复说明 |
| persisted settings 返回 `stale_threshold_intervals=20` | 修改其他字段后的 PUT 仍携带 20 |
| API 返回倒序 CPU 阈值 | 图表/列表使用 CPU 默认阈值 |
| API 返回有效 IOWait `10 / 40` | 展示阈值为 `10 / 25 / 40` |

#### 5. Good/Base/Bad Cases

- Good: Settings 页保存 `CPU 80/90/95` 后，监控详情 tooltip、阈值线和后端 evaluator 语义一致。
- Good: Settings 页读取自定义 `N=20` 后只修改扫描间隔，保存 payload 保持 `stale_threshold_intervals=20`。
- Base: `/api/settings` 暂时失败或返回缺失阈值，页面继续使用 `DEFAULT_THRESHOLDS`。
- Bad: UI 把默认 12 写死回所有响应，覆盖已有全局 20 或 override 中显式 3。
- Bad: Settings 页只校验正整数，允许 `CPU 95/80/90` 发到后端。
- Bad: `resolveThresholds` 对 `IOWait 50/20` 派生 `35` 并渲染倒序阈值线。

#### 6. Tests Required

- `web/src/pages/SettingsPage.test.tsx`: 覆盖倒序阈值本地拒绝和不发 PUT，以及默认 12 文案与自定义 20 原样 PUT。
- `web/src/lib/api.test.ts`: GET response 与 PUT request 的代表默认 fixture 都固定 `stale_threshold_intervals=12`；不得用旧默认 3 伪装 fresh-install API 形状。
- `scripts/test_visual_evidence.py`: 调用 active mock 的 `/api/settings` 并断言 `stale_threshold_intervals=12`，防止 browser-sanity 与真实 fresh-install 默认漂移。
- `web/src/config/thresholds.test.ts`: 覆盖有效两段阈值派生 alert，以及倒序 runtime settings 回退默认值。
- 跨后端改动时同步跑 `go test ./internal/center/settings`。

#### 7. Wrong vs Correct

```tsx
// 错误：逐项解析后直接提交，等级顺序未校验。
cpu_warning_pct: parsePositiveInteger(form.cpuWarningPct, 'CPU 关注')
```

```tsx
// 正确：解析后校验递进关系，再提交。
assertThreeLevelThresholdOrder('CPU', cpuWarning, cpuAlert, cpuCritical)
```

### MonitoringInstance onboarding 一键安装数据流

#### 1. Scope / Trigger

- Trigger: 修改 `MonitoringDetailPage`、`MonitoringInstanceInstallCommandIssue`、`issueMonitoringInstanceInstallCommand`、MonitoringInstance 创建后跳转 onboarding 的流程，或任何安装命令展示/复制行为。

#### 2. Signatures

- Frontend type: `MonitoringInstanceInstallCommandIssue` fields mirror center JSON snake_case: `command`, `issued_at`, `expires_at`, `installer_url`, `public_base_url`, `agent_version`, `release_repo`。
- Frontend API: `issueMonitoringInstanceInstallCommand(monitoringInstanceId)` -> `POST /api/monitoring-instances/{monitoring_instance_id}/install-command`。
- Page flow: VPS 详情创建其专属 MonitoringInstance 后跳转 onboarding；`MonitoringDetailPage` 按用户操作生成/重新生成 center command，不再依赖 create flow 预发 plaintext token。

#### 3. Contracts

- Browser must never construct the production install command from `window.location.origin`, route params, or request metadata. The center-generated `issue.command` is the only command shown for copy.
- The command contains a one-time enrollment token; UI should hide/reveal/copy deliberately, show expiry metadata, and avoid rendering full token in incidental notices or conflict-resolution copy.
- Config errors from backend 409 (`public base URL is not configured`, `agent release version is not configured`) are actionable deployment errors and should be displayed as-is.
- Binding conflict UI must not say the enrollment token is unchanged; a pending fingerprint attempt may have consumed it, so operators may need to regenerate after confirm/reject.
- Manual fallback can remain for troubleshooting, but it must be secondary to center-generated one-command install.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Generate command succeeds | page shows command, expiry, installer URL, version/repo metadata, and copy controls |
| User regenerates command | old visible command is replaced by new center response |
| 409 public URL/version config error | page shows backend message and does not synthesize fallback command |
| Binding conflict displayed | copy explains one-time token may be consumed and regeneration may be required |
| Create MonitoringInstance succeeds | navigate to onboarding page instead of caching a plaintext token in module global state |

#### 5. Good/Base/Bad Cases

- Good: user clicks generate, reviews masked/revealed command, copies the exact backend command, and runs it on a VPS.
- Base: command expired; user clicks regenerate and copies the replacement.
- Bad: page builds `curl ${window.location.origin}/api/agent/install.sh ...` and silently ignores missing `HOUFENG_PUBLIC_BASE_URL`.
- Bad: create flow stores plaintext enrollment token in module/global state for cross-page transfer.

#### 6. Tests Required

- `web/src/lib/api.test.ts`: `issueMonitoringInstanceInstallCommand` posts to `/api/monitoring-instances/{id}/install-command`.
- `MonitoringDetailPage.test.tsx`: generate success, regenerate, reveal/hide/copy, config errors, metadata display, and conflict copy warning about consumed one-time tokens.
- `MonitoringPage.test.tsx`: create flow navigates to onboarding without pre-issuing or caching an enrollment token.

#### 7. Wrong vs Correct

```tsx
// 错误：浏览器自己拼安装命令，绕过 center 的 URL/version/token contract。
const command = `curl -fsSL ${window.location.origin}/api/agent/install.sh | sudo sh -s -- --server-url ${window.location.origin}`
```

```tsx
// 正确：只展示后端返回的命令。
const issue = await issueMonitoringInstanceInstallCommand(monitoringInstanceId)
setInstallIssue(issue)
```

### MonitoringInstance 详情 metadata 与运行态合并

#### Contracts

- `MonitoringDetailPage` 把 `group`、`labels`、`note` 视为资料维护字段；保存必须走 `updateMonitoringInstanceMetadata(monitoringInstanceId, input, { expectedUpdatedAt })`，并通过 `If-Match` 使用当前 `updated_at`。创建目标对话框的“给实例加上执行标签”是同一接口的另一个调用方：写入前重读实例，只追加一个标签，带回当前 group 与 note，`If-Match` 用重读到的 `updated_at`，409 时重读重试一次（见 targets-web）。
- 运行控制、绑定接入确认、命令轮询、实时样本等非资料更新即使返回整条 `MonitoringInstanceRecord`，前端合并到当前 state 时也必须保留当前 `group`、`labels`、`note`。这些响应可能来自资料保存之前发出的请求，不能覆盖用户刚保存的资料。
- 资料保存成功后只把 `group`、`labels`、`note`、`updated_at` 合并回当前监控实例，不能把保存响应里的运行态字段反向覆盖掉更新后的 `monitoring_status`、`binding_status`、`last_action` 或心跳事实。
- 切换 `monitoringInstanceId` 时必须重建 metadata draft、清理提交中状态和错误，避免旧实例的资料草稿或错误泄漏到新实例。

#### Tests Required

- `MonitoringDetailPage.test.tsx` 覆盖：编辑 Group / labels / note、取消后 draft 重置、PATCH payload 含 `If-Match`、保存后页面显示新资料。
- `MonitoringDetailPage.test.tsx` 覆盖：非资料运行态响应返回旧 `group` / `labels` / `note` 时，详情页仍保留当前资料字段。

#### Wrong vs Correct

```tsx
// 错误：运行态响应整条覆盖，可能把刚保存的 Group/标签/备注打回旧值。
setMonitoringInstance(updated)
```

```tsx
// 正确：非资料响应只更新运行态字段，保留当前资料字段。
setMonitoringInstance({
  ...updated,
  group: current.group,
  labels: current.labels,
  note: current.note,
})
```

### MonitoringInstance 命令操作确认与输出过期

#### 1. Scope / Trigger

- Trigger: 修改 `MonitoringDetailPage` 命令抽屉、`postMonitoringInstanceAction`、`MonitoringInstanceRecord.last_action`、命令常量、或命令输出展示组件。
- 目标：前端只做 presentation 和用户确认辅助；backend-owned sensitivity / confirmation / audit / TTL 是安全边界。UI 必须避免绕过 sensitive confirmation，也不能展示已经过期的 stdout/stderr。

#### 2. Signatures

- Frontend API: `postMonitoringInstanceAction(monitoringInstanceId, commandId, options?)`。
- Sensitive options: `{ confirmedSensitive: true }` serializes as `confirmed_sensitive:true` in `POST /api/monitoring-instances/{id}/actions` body.
- Command metadata: `MonitoringInstanceCommand.sensitivity` is `'standard' | 'sensitive'` and must mirror backend command tiers for presentation.
- `LastAction` fields: `sensitivity?`, `queued_at?`, `completed_at?`, `output_expires_at?`, `output_expired?`, plus existing `action_id`, `command_id`, `status`, `stdout`, `stderr`, `exit_code`.

#### 3. Contracts

- Standard commands (`df_h`, `free_m`, `uptime`) post immediately with body `{"command_id":"..."}`.
- Sensitive commands (`top_head`, `journalctl_u`, `systemctl_status`, `dmesg_err`, `docker_ps`) must open an `ActionConfirmationModal` before POST. Clicking a sensitive command must not call `fetch` until the user confirms.
- Confirming a sensitive command must call `postMonitoringInstanceAction(..., { confirmedSensitive: true })`; canceling must close the confirmation and leave fetch call count unchanged.
- Command rows must visually distinguish sensitive commands with a compact marker, but that marker is not authorization.
- Pending-action disabling stays tied to `last_action.status === 'pending'`; adding confirmation must not allow another command while one is pending.
- Optimistic pending state must include `action_id`, `command_id`, `status:'pending'`, `sensitivity`, and `queued_at` so the drawer can keep identity visible until polling refreshes.
- When `last_action.output_expired` is true, `MonitoringInstanceCommandResult` must not render stdout/stderr sections from stale data. It should show a short expired-output message while preserving command label and exit code.
- Global audit browsing lives at `/command-audit` and is metadata-only；role-based command/audit authorization remains GitHub #381 follow-up，Monitoring detail 不得自行实现第二套权限或审计列表。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Standard command click | One POST with `{"command_id":"uptime"}` |
| Sensitive command click | Open confirmation; no POST yet |
| Sensitive confirm | POST includes `confirmed_sensitive:true` |
| Sensitive cancel | Confirmation closes; no POST |
| Existing `last_action.status === 'pending'` | All command buttons disabled |
| `last_action.output_expired === true` | No stdout/stderr blocks; expired-output message visible |

#### 5. Good/Base/Bad Cases

- Good: 用户点击 `systemctl status`，看到二次确认；确认后 payload 含 `confirmed_sensitive:true`，抽屉显示 `systemctl status · 等待 agent 执行…`。
- Good: 24h 后后端返回 `output_expired:true`，UI 显示“命令输出已过期”，不把旧 stdout/stderr 文本重新展示出来。
- Base: 用户点击 `uptime`，没有二次确认，保持原有快速诊断流程。
- Bad: 前端只靠“敏感”标记但点击后直接 POST，后端会拒绝且用户体验混乱。
- Bad: `output_expired:true` 时仍根据残留 `stdout` 字段渲染 `<pre>`，会把后端已经判定过期的输出重新暴露。

#### 6. Tests Required

- `web/src/lib/api.test.ts`: standard command body不带 `confirmed_sensitive`；confirmed sensitive body 带 `confirmed_sensitive:true`。
- `MonitoringDetailPage.test.tsx`: sensitive click opens confirmation and does not POST immediately；confirm posts with confirmation field；cancel does not POST；standard command still posts immediately；expired command output hides stdout/stderr and shows expired state。

#### 7. Wrong vs Correct

```tsx
// 错误：sensitive command 直接 POST，绕过用户确认辅助。
onClick={() => onExecute(command.id)}
```

```tsx
// 正确：sensitive command 先打开确认，确认后才发送 confirmed_sensitive。
if (command.sensitivity === 'sensitive') {
  setPendingSensitiveCommand(command)
  return
}
onExecute(command.id)
```

```tsx
// 错误：output_expired=true 时仍使用残留 stdout。
const stdout = action.stdout ?? ''
```

```tsx
// 正确：过期输出在展示层也强制清空。
const stdout = action.output_expired ? '' : (action.stdout ?? '')
```

### Global command audit URL, cursor, and metadata-only contract

#### 1. Scope / Trigger

- Trigger: 修改 `/command-audit`、`CommandAuditPage`、`pages/command-audit/`、`listCommandAudits`、`CommandAudit*` types、共享 command metadata 或 MonitoringInstance 审计入口时必须加载本节。
- 目标：让 URL、API snapshot cursor、action/event 展示和危险输出 allowlist 只有一个明确 owner；筛选、翻页或恶意附加字段都不能把旧结果或 stdout/stderr 带进 DOM。

#### 2. Signatures

- Page route: private lazy `/command-audit`；MonitoringInstance 入口为 `/command-audit?monitoring_instance=<stable-id>`。
- API: `observabilityApi.ts` 的 `listCommandAudits(filter?: CommandAuditListFilter) -> Promise<CommandAuditListResponse>`；cursor continuation 只序列化 `cursor`。
- URL filters: `window=24h|7d|30d|all|custom`、`started_from`、`started_to`、`monitoring_instance`、`command_id`、`sensitivity`、`outcome`、`actor`、`action_id`；默认 `30d` 不写 URL。
- Response types: `CommandAuditAction` / `CommandAuditEvent` only include stable identity, snapshots, command/sensitivity/outcome, actor, timestamps, exit code and normalized rejection reason；类型中不存在 `details`、stdout、stderr。

#### 3. Contracts

- `filterModel.ts` 是 parse → normalize → canonical URL → API query 的唯一 owner。非法/冗余参数用 replace canonicalize；custom browser `datetime-local` 在 API query 转 RFC3339。日期校验必须先验证真实日历日和支持的 datetime/RFC3339 结构，不能只用会把 `2026-02-30` 滚动到三月的 `Date.parse`。
- 页面保持 applied filters、primary/advanced draft、items、next cursor、expanded IDs 和 request generation。筛选提交必须先清空旧 items/cursor/expanded；过期 response 按 generation 丢弃。
- 高级 Modal 打开时从当前 applied/primary draft 初始化；Cancel/Escape/close 丢弃 draft，Reset 只清 advanced fields，Apply 才写 URL/发请求。
- 首次请求按 URL filters；加载更多只调用 `listCommandAudits({cursor})`，按 action `id` 去重；无 cursor 不发请求。加载更多失败必须保留现有 items 与原 cursor，显示局部错误并允许使用同一 cursor 重试。
- 表格/时间线只能读取显式字段，禁止 `Object.entries`、spread-to-DOM 或 JSON dump。即使 runtime response 额外带 `stdout` / `stderr` / `details`，页面也不得渲染。
- 当前实例名称链接 `/monitoring/:id`；`deleted=true` 显示快照名 + 稳定 ID + “已删除”，不生成失效链接。actor 显示优先 display name → username → user ID → 系统。
- command labels/options/sensitivity 由 `web/src/config/commands.ts` 共享；Monitoring detail 与审计页不得复制 command list。页面不新增 Context、dependency 或 route-private CSS。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| `/command-audit` 或 `?window=30d` | 请求 `/api/command-audits`，canonical URL 无默认参数 |
| invalid enum/unknown parameter/incomplete custom time | 回退合法 filters 并 replace URL，不发送非法 query |
| impossible custom calendar date such as `2026-02-30` | 回退默认 30d 并 canonicalize；不得静默查询滚动后的三月日期 |
| advanced draft Cancel/Escape | URL/request 不变；重新打开恢复 applied values |
| initial/filter request races | 只接受最新 generation；旧响应不回填列表 |
| load-more response repeats an existing ID | 保留已有项一次，追加新 ID；按 cursor 继续 |
| deleted monitoring instance | snapshot/ID/“已删除”可见；无 detail link |
| runtime payload adds stdout/stderr/details | types 不声明；table/timeline DOM 不出现其 key/value |
| 390px viewport | document 不横向溢出；named focusable table wrapper owns horizontal scroll |

#### 5. Good / Base / Bad Cases

- Good: 用户从 MonitoringInstance 菜单进入预筛选审计，展开两条 allowlisted event，再用 opaque cursor 加载更多；URL 只保存可分享 filters。
- Good: 实例和 actor 已删除，表格仍显示稳定 ID 与快照； hostile fixture 附带 output 字段但 DOM 没有输出文本。
- Base: 当前筛选无结果，页面显示 bounded empty state；加载更多失败保留已加载 items 并提供局部错误。
- Bad: 把 cursor 与 filter 一起发给 API，或筛选改变后继续 append 旧 cursor page。
- Bad: 为“调试方便”遍历 event object 并输出所有 key，导致恶意/未来 stdout 字段泄漏。

#### 6. Tests Required

- `filterModel.test.ts`: default omission、custom RFC3339、trim、非法/不存在日历日期 canonicalization 和 stable filter key。
- `api.test.ts` / `commandAuditContract.test.ts`: `observabilityApi` 的 default no-query、normalized initial query、cursor-only 和 source/type output-field absence。
- `CommandAuditPage.test.tsx` + private component tests: drafts、race、load-more success/failure retry/dedupe/reset、five outcomes、actor fallback、deleted link、event order、hostile output 和 named local scroll region；宽表尺寸使用共享 spacing token，不新增硬编码像素。
- `web/e2e`: exact default fixture + undeclared cursor fail-closed，10×3 core route，axe，390px keyboard scroll/expand/Modal focus；staging 只要求 metadata-only copy，不依赖存在真实 audit rows。

#### 7. Wrong vs Correct

```tsx
// 错误：把 runtime response 当任意对象 dump，未来字段会直接进入 DOM。
<pre>{JSON.stringify(action, null, 2)}</pre>

// 正确：presentation 只消费显式 metadata contract。
<CommandAuditTable
  rows={response.items}
  expandedIDs={expandedIDs}
  onToggle={toggleExpanded}
/>
```

```ts
// 错误：续页重新带 filters，可能与 cursor snapshot 漂移。
listCommandAudits({ ...appliedFilters, cursor: nextCursor })

// 正确：cursor 自含规范化 filters/bounds/last key。
listCommandAudits({ cursor: nextCursor })
```

### MonitoringInstance 归属、生命周期与当前工作集

- 实例永久归属于一台 VPS，生命周期仅为 `待接入 / 已接入 / 已退役`。运行控制 `启用 / 维护中 / 暂停`、在线证据、绑定确认、页面数据新鲜度分别展示。维护、暂停、待接入和数据不可用不得显示为健康。
- 创建入口仅从 VPS 详情发起 `POST /api/vps/{id}/monitoring-instances`。监控列表接入入口导航到 VPS 库存；不提供独立实例创建、共享或跨 VPS 转绑。
- `MonitoringPage` 默认只读取管理中 VPS 的当前实例；前端也排除 `已退役`、`is_current=false` 和所属 VPS 已归档的记录。计数、筛选及批量操作使用同一工作集。历史实例从 VPS 监控历史访问。
- VPS 接入面板在操作前重新读取归属与当前实例数量。历史实例不计入当前数量；归属不匹配、数量证据不一致或多个当前实例时阻止创建。已有当前实例直接进入该实例接入页；仅有历史实例时允许显式重新接入或创建新实例。
- 退役入口只在非退役实例且当前管理审查允许时显示。即使旧审查误称允许，也不得向已退役实例再次提供退役。独立归档、恢复生命周期、恢复归档和永久清理入口全部移除。
- 退役使用原因、当前预览摘要和稳定 `Idempotency-Key`；网络重试保留同一用户意图的键，失败不关闭确认框，成功刷新对象与操作资格。重复点击不得并发产生新意图。
- 已接入实例和已退役实例的重新接入均由用户显式生成命令触发：先调用 `binding/reset` 建立新的接入阶段，再签发安装命令；仅打开抽屉不重置。已退役实例要求所属 VPS 管理中，后端同时确认无其他当前实例。旧会话仅保留最小在线证据权限，不恢复采集和命令。
- 所属 VPS 已归档时只查看监控历史，不提供采集控制、命令、资料修改或接入。恢复 VPS 不自动恢复历史实例；重新接入必须再次显式进行。
- 详情提供按需加载的接入阶段历史，读取 `/api/monitoring-instances/{id}/phases`，展示会话权限、指纹摘要、起止时间及最后可信在线。旧的仅在线证据会话可在结束后继续产生在线证据，不能将其当成采集恢复；不展示凭据或原始指纹。
- 管理审查、安装命令和动作响应继续绑定实例 ID、请求代次与当前页面身份；旧对象慢响应不得改变新对象，过期预览不得自动重新提交。关闭接入抽屉后刷新实例与管理审查。接入抽屉不外露实例/VPS 内部 ID（标题已写实例名称）。签发命令后抽屉立即读取一次会话列表（`/phases`）作为基线，之后每 5 秒例行读取一次；每次签发同一时刻只保留一个在途请求，上一次签发遗留的请求不阻塞新签发，其迟到响应被忽略。只有不在基线中、`started_at` 不早于命令 `issued_at`（均为服务端时间）、能力为 full 且已连上的会话才判定接入完成——此前命令建立的会话、降级的旧会话以及与签发并发而在签发返回前提交的接入都已在基线里，不会被误认；基线读取失败时以第一次成功读取为基线，宁可不自动推进；此时显示“接入完成”、隐藏命令且不再提供重新生成（避免重置刚建立的绑定）。命令生成后“完成并返回 VPS / 查看监控实例”为主操作，重新生成降为次操作，并提示窗口会自动检测首次心跳。
- 回归覆盖 `MonitoringPage.test.tsx`、`MonitoringDetailPage.test.tsx`、`MonitoringDetailManagementMenu.test.tsx`、`MonitoringInstanceOnboardingDrawer.test.tsx` 与 `VPSOverviewMonitoringOnboarding.test.tsx`：退役入口消失、历史排除、稳定重试键、归档所属对象禁用、显式新阶段、归属异常拒绝与异步请求归属。

### Monitoring 列表工作台状态

MonitoringPage 是运行证据扫描页。主路径是 attention tabs → 可见 FilterBar → 监控实例列表 → 行级处理，批量操作为次级控制。

#### Contracts

- Quick view 负责表达当前扫描主线，至少覆盖全部、异常、待接入、维护/暂停、绑定异常；维护/暂停视图必须同时包含 `monitoring_status === '维护中'` 与 `monitoring_status === '暂停'`，不要用单个 `run_status` 推断。
- 筛选使用可见的 FilterBar 并即时应用；更新 URL 使用 replace，保留无关 query 和 history state。没有 Drawer draft 或 Apply 步骤。≤760px 时 FilterBar 启用窄屏收起：搜索、已生效筛选的 chip 与“筛选 (N)”按钮常驻，其余筛选点开后原位展开，仍即时应用（见 component-patterns）。
- 高级筛选计数只统计已应用字段筛选，必须覆盖 lifecycle、health、monitoring/run status、group、region、labels、search 等会改变列表的维度；quick view 本身不混入字段筛选计数。
- 批量操作区默认隐藏；只有用户显式打开批量操作、已经选择全量/部分监控实例、存在待确认批量动作、提交中或错误需要展示时才出现。批量动作按钮仍必须以明确选择为前提，不因列表有数据而默认高亮。
- MonitoringPage 不承载资产判断支撑面，也不展示 MonitoringInstance 资产上下文列；Hero 之后应直接进入 toolbar/filter/batch/table。资产侧判断导向资产决策页、VPS 库存 / 详情和结束使用并归档入口，Monitoring 列表只保留运行观测扫描职责。

#### Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| 修改可见 FilterBar 筛选 | 立即应用并 replace URL，保留无关 query 与 history state |

| runtime attention quick view | 同时显示维护中与暂停监控实例；空态不得误报为全局无监控实例 |
| 无选择且未打开批量操作 | 批量 bar 不渲染 |
| 打开批量操作但未全选 | 显示范围/选择入口，不显示实际批量动作按钮 |

- 监控详情默认使用 URL 中的历史窗口（缺省24h）；显式进入实时窗口后才连接同源 runtime-stream WebSocket，退出窗口或实例时关闭。连接状态、agent 心跳与主机采样是不同事实：在线状态仅使用 Center 持久化的 record.last_trusted_online_at，包含仅在线证据会话；样本与回填不得覆盖此字段，uptime 不由浏览器递增。命令抽屉在命令待完成时使用有界生命周期的轮询；不要据此给其他页面增加常驻轮询。
- 详情 record、runtime-facts、settings、异常、事件与关联各自维护请求代次和局部错误/重试；新窗口未成功读取前不能把旧窗口数据标成新窗口。runtime-facts.read_at 是快照读取时间；最新样本来自服务端当前绑定投影，历史桶可保留旧观测。空桶及 network_rates_valid 非 true 的网络证据保持缺测，有效零值保留。较新的权威空快照可以清除旧绑定样本；迟到的较旧快照不能覆盖其 read_at 之后已收到的新实时证据。

## Monitoring detail diagnostics

 `/monitoring/:id` answers three questions in order: is it abnormal now, is the evidence fresh, and what changed inside the selected window. It orders a name-first identity header with two actions, one status band, conditional notice rows, a page-level time range, one chart section, recent events, then the header management menu. Metadata and destructive lifecycle moved into that menu; there is no page-bottom disclosure. Keep health/freshness separate from pause, maintenance and binding, and show `待接入 / 已接入 / 已退役` independently of the owning VPS lifecycle; monitoring control and online evidence are separate facts.

 The header title is the display name only, with a read-only-preview badge when the build is read-only. The identity list renders only rows that have a value, in the fixed order link / provider / location / group / labels / note / ID; labels stop at three plus a count, and the note clamps to two lines with the full text in `title`. Omit cloud region codes but keep the city, and never write a "location unconfirmed" or "provider unconfirmed" sentence. Default actions are refresh and management; an unbound current instance owned by a managed VPS gets onboarding as the header primary action. Retired instances and instances whose owning VPS is archived hide the runtime group.

 The status band does not repeat the health word (正常/关注/告警/严重). It combines heartbeat, sample age, and sampled uptime on one line. Only badges that mean something appear: monitoring lifecycle, maintenance or pause, unbound or pending-fingerprint, and owning VPS archived. Enabled and bound states need no badge. Omit agent version. Heartbeat age is its own evidence and never inherits the sample time, and `last_sync` stays unrendered because batch arrival is not evidence freshness.

 Abnormal states use a filled notice well: 4px severity rail, tinted surface, mark + title + detail, and the action inside the well next to the copy (`查看事件`, `处置绑定冲突`). Binding conflict, active incidents, stale-without-incident, missing heartbeat on a bound instance, maintenance, pause, runtime-action error, and runtime-metric error share that surface. Never-bound stays the header CTA and does not add a missing-heartbeat well. Incident duration reads 已持续 N, not 持续 N 前. Provider-relation failures belong in the identity row, not here.

 The URL-backed default window is 24h, and the single range control sits in the resource-trend section head. It still governs the whole chart section. Only an explicit realtime window opens the runtime WebSocket; the connection word sits beside the control, and connection status is not heartbeat evidence.

 The detail column fills the same `.main` well as `/monitoring`; it is not a centered 1600px reading column. Eight equal tiles: CPU, memory (used + swap), disk, Inode, load 1/5/15, I/O wait + disk busy, network in+out, disk read+write. Wide consoles are 4×2, medium 2×4, narrow one column. Plot height is a fixed 200px and does not grow with the viewport. Dual-series tiles share one vertical scale and aligned hover. CPU and I/O wait are never stacked on one axis. Snapshot notes under memory are available and capacity only; swap is a line. Disk notes keep capacity only.

Each tile sits on the page ground with a real border and a 2px top edge that takes the warning/alert/critical color when the current value crosses a threshold. The tile header keeps the title (ellipsized, full hint on hover) and the current value on the first row and moves the series legend to its own second row; tiles without a legend reserve that row so plots in one row stay aligned, and dual-series values never overlap the title. The same section padding variables apply when target detail reuses these sections. They are not the old uppercase `watchtower-metric-card` tiles and not unframed plots inside one mural.

Percentage plots mark their warning level with a 1px dashed warning line and no filled band. Data-scaled plots draw only the threshold lines that fall inside their axis and label only the topmost, and their axis follows the data with no artificial lower bound.

 Facts with no history annotate the relevant plot as a definition list. The section footer shows the available window range (`近 24h · start – end`); empty windows keep `近 24h` and must not invent the requested-window clock span. Uptime never ticks locally. Hover can add a selected timestamp; realtime can add the live point. Unknown network validity renders an em dash, explicitly valid zero stays zero, and a window whose buckets are all gaps reports no samples rather than zero. Detail-specific scaling, alert bands and second-series options are opt-in and must not silently change Target or Compare charts. Compare uses a compact dual-column cut of the same tile language, not two copies of the 200px eight-tile workbench. Target latency charts must not reuse host CPU/disk tiles.

 Recent events show at most three rows without object ids, and the history / activity / records / evidence entries are always present, so the history drawer stays reachable with zero events. Empty, loading and failed event states are one quiet line beside those entries, not a zero-count card.

 Dangerous confirmations freeze the subject, display name, action and record version, and require renewed review if that version changes; review counts, blockers and warnings appear above the reason field inside the confirmation. Preserve metadata If-Match protection, center-issued installation commands and token secrecy. Detail, comparison and history returns retain validated list query/selection and source-VPS navigation state.

## Monitoring list workbench

`/monitoring` is the current VPS runtime-monitoring list, with a separate detail page; it has no archived/all scope switch. Place attention views below the compact title using the same shared Tabs pattern as subscriptions. Below them, keep every immediate filter visible in the shared FilterBar, followed by compact search on the right and the batch dropdown at the far right. Do not collapse filters behind a disclosure or an Apply step. The first table column is fixed-width multiselect; comparison is a batch-menu action available only for exactly two selected instances. Give name, health, and heartbeat bounded practical widths; resource history takes all remaining table width rather than proportionally stretching the earlier columns. Version persisted column layouts when their schema changes so old preferences cannot misassign widths. Names are the only identity content and remain native detail links; rows also navigate to detail.

The health column is an attention cell, not a reassuring status. Do not render 正常. Show 关注/告警/严重 with the issue summary and incident count. Maintenance, pause, unbound, and pending-fingerprint occupy that cell and must not stack with 正常. Missing heartbeat on a bound instance is 未知; never-bound is 未绑定, not a second 未知. Fresh heartbeats need no extra copy. The list header action “从未关联 VPS 接入” creates a monitoring instance from an unlinked VPS; the detail header “接入 agent…” issues a command for an existing instance — do not merge the two. 24h trends stay three sparklines (CPU / memory / disk); do not put eight detail tiles in a row. Preserve resizing for data columns and a named keyboard-focusable narrow-window table scroller rather than shrinking text.

The list is a manually refreshed snapshot, not the detail page's live stream. Relative heartbeat age and freshness use the same successful list-read instant. Only validated, successfully read global incident defaults provide the heartbeat threshold; settings failure leaves timestamps readable without an invented policy. Online freshness uses only Center-persisted `last_trusted_online_at`; accepted backfill and sample timestamps cannot refresh it. Evidence-only sessions can refresh online evidence while collection remains stopped. Missing or invalid heartbeat evidence is unknown consistently across cells, health filters, abnormal counts, and ordering. Pause, maintenance, and binding remain independent. List, trend, and policy failures have local retry paths; an in-place list refresh failure retains explicitly labelled prior data.
Uptime and two-line upload/download network rates follow heartbeat, before **24h 资源趋势**. They come from one authenticated batch of latest host observations, not per-row detail fetches or trend averages. Display the actual sample age and timestamp; uptime is the sampled system uptime and never advances in the browser. Host sampling cadence is independent of heartbeat freshness, so do not invent a shared stale cutoff or describe these values as live. Missing samples and unknown/invalid rates display an em dash; an explicitly valid zero rate remains zero. Runtime-summary refresh failures retain a labelled previous snapshot with their own retry. Keep these two columns bounded and preserve enough trend width through local horizontal scrolling on narrow screens. Cycle traffic and country information are not part of this list.

The 24-hour resource values are downsampled history, not instantaneous metrics. Empty buckets are missing evidence, not zero utilization; genuine zero remains a value. Gaps retain their time position and all segments share one vertical scale. A missing source and a failed source are not the same empty state.

Search, quick view, filters, sort, and row selection are URL-backed. View navigation pushes history; filter edits apply immediately, replace history, and preserve unrelated query fields and history state. Selection is limited to visible eligible instances, with no two-row cap on batch selection. Detail and comparison return through the validated `monitoringListHref`; the existing source-VPS navigation remains independent. Batch confirmation uses frozen selected IDs and the same frozen count, excludes retired records, historical instances and instances whose owning VPS is archived, and keeps failures visible after submission.

Monitoring and entrypoint detail pages lead with compact identity and current observations. Keep management and history subordinate, incident/event failures independently retryable, and missing or disabled probes distinct from failed observations. An empty incident list is not evidence that every runtime source is healthy. Onboarding names its monitoring instance, obtains installation commands only after an explicit request to the center, and keeps sensitive copy/reveal controls inside the authenticated dialog. Pending issuance or copying prevents dismissal; a closed, replaced, or unmounted dialog must not start copying a late response or restore its command. An already-started clipboard write cannot be cancelled. Probe and onboarding dialogs keep their action footer outside the scrolling body.

VPS-to-monitoring navigation retains `return_vps` and inventory history state independently of the one-shot `onboarding` flag. This is source navigation, not proof of a current association: keep actual association results truthful and provide an explicit source-VPS return when that destination is otherwise unavailable, including relation failures and unavailable monitoring details. Install-command errors use the center’s stable error code: an owning-VPS-archived rejection is not a configuration failure, and an unclassified conflict must not be guessed to be one. Existing configuration preflight remains before token issuance; these codes do not change eligibility-check precedence.

VPS monitoring freshness uses the center’s existing persisted heartbeat interval and stale-interval threshold. An old observation is stale evidence, not a new business state or an instruction to notify; a missing first heartbeat remains distinct from a previously observed instance becoming stale. Keep known historical health visible with its freshness qualifier, and do not summarize incomplete observations as currently healthy.


## 图表兼容与安全回归

已落地详情重做的长期要求：不得为只有快照的字段伪造历史；无效网络与缺桶保持缺测，显式零值保留。`MetricChart` 的第二序列必须等长且逐点 `observedAt` 相同，只有通过校验的序列参与空态并集；每条序列独立保留 null/gap。新增单点、阈值标注和布局选项必须 opt-in，未传入时保留既有消费者默认行为。

回归覆盖主序列全空而有效第二序列有值、非法第二序列、单点默认与 opt-in、空阈值 label、Compare 与 Evidence renderer。Target 图表使用自己的真实消费者，不借用主机指标语义。路由 CSS 只依赖已登记 owner；清理样式前验证动态 modifier 和其他页面消费者，不能依据单次字面 grep 删除共享规则。安装 token 保密、metadata If-Match、冻结危险操作主体与版本、绑定确认和零事件历史入口继续由本合同对应章节保护。

## CPU 缺测呈现

- `HostSample.cpu_rates_valid?: boolean | null` 与 usage/iowait/steal 整组有限 `[0,100]` 共同判定。nil legacy 允许范围内值，false 始终不可用；HTTP latest/recent 与 WebSocket seed/append 都经 `hostCPURate` 收敛，真实零不变 null。
- 详情 CPU/iowait 和对比 CPU 卡无 hover 时定位最后一个 `sample_count > 0` 的 host point，跳过尾部空桶，但不能越过有样本的 CPU null 回看旧值。该点 CPU null 显示 `—`、`CPU 采样不可用`及中性样式；完全没有 host 点才显示“暂无观测数据”。hover 保留所指桶值/缺口/时间，其他指标行为不变。
- 列表 CPU 使用同窗同桶内存/磁盘最后有限下标的较大者定位当前 host 点，再读取对应 CPU；不能对 CPU 使用 lastNonNull。当前 null 不隐藏历史曲线，任何有限历史值（含零）仍绘制，全部 null 才用空态。没有非 CPU host 点才是“暂无观测数据”。
- CPU 缺测不推导整机无数据、在线变化或健康色，不增加列表 CPU summary，也不改变 runtime source 的身份/权限/时序策略。
