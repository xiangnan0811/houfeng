# 证据快照 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

### Scenario: route-private Evidence capacity state

#### 1. Scope / Trigger

- Trigger：修改 `EvidenceQuota` DTO、`EvidenceCapturePicker` 的 preview/confirm 逻辑、Records evidence API transport 或现有 Records lazy route 接线时。
- `EvidenceCapturePicker` 仍是尚未生产挂载的 route-private injected component；受保护的 `EvidenceSnapshotPage` reader 已由 `web/src/app/router.tsx` 懒加载，并调用真实 evidence API 与 renderer registry。本节约束 server-owned quota 状态，不因 capacity 功能新建 route、不把 `recordsApi.ts` 带入 eager graph、不引入轮询或全局 store；reader 合同见下方正文。

#### 2. Signatures

```ts
export type EvidenceQuota = {
  status: 'allowed' | 'warning' | 'exceeded' | 'unavailable'
  reason?: string
}

export type EvidenceCaptureReference = {
  record_id: string
  capture_intent_id: string
}
```

#### 3. Contracts

- quota完全由preview response提供；Web不得读取attachment quota、根据estimated bytes推断threshold、缓存project usage或启动polling。上游selection任一变化仍清空preview与confirm state。
- `allowed`与带精确固定server reason的`warning`可确认；`warning|exceeded|unavailable`的reason分别只能是`project evidence quota warning threshold reached`、`project evidence quota exceeded`、`project evidence capacity unavailable`。状态与reason不匹配、任意dependency/identity文本、unknown/missing quota或stale preview必须禁用confirm且不得进入preview DOM；`handleConfirm`本身也要重复失败关闭，不能只依赖disabled样式。
- preview展示allowlisted quota status/reason与estimated canonical bytes；不得展示payload、metadata、authorization、digest、stdout/stderr，也不得用`JSON.stringify`或object spread renderer回退。
- confirm payload仍只能是`record_id + capture_intent_id`，不能把quota、estimated size、payload或客户端同意标志回传。warning的确认不改变server canonical status/reason。
- `recordsApi.ts`继续lazy-only；capacity状态不构成新增eager endpoint、Context、常驻缓存或production route挂载理由。

#### 4. Validation & Error Matrix

| preview quota | Confirm | UI state |
| --- | --- | --- |
| `allowed` | enabled | 展示server status |
| `warning` | enabled | 展示server status/reason |
| `exceeded` | disabled | 保留preview供解释，不发送confirm |
| `unavailable` | disabled | 保留preview供解释，不把unknown当零usage |
| missing/unknown/stale/reason mismatch | disabled/fail closed | 不推断、不渲染非闭合reason、不自动刷新、不静默替换intent |

#### 5. Tests Required

- `EvidenceCapturePicker.test.tsx`覆盖warning可确认、exceeded/unavailable不可确认、stale、upstream reset与confirm body exact allowlist。
- `recordsApi.test.ts`与architecture/bundle contracts继续证明唯一transport、lazy-only graph和wire shape不变。
- 使用Node 22运行focused Vitest、lint、strict TypeScript/build、bundle/CSS contracts及`make verify-web`；不得抬bundle/CSS budget。


## Protected evidence reader

Evidence links open the protected `/evidence/:evidenceId` reader using the existing exact renderer tuple and validated read model. Unsupported or invalid evidence fails closed without exposing raw payloads; source deletion does not erase an authorized retained snapshot. Subject filter and return context belong to [Records subject workspace](records/web-surfaces.md).

### 阅读页版式

- 页头与记录工作区同一版式（`RecordWorkspace.css`）：标题 + 证据类型徽标（中文）、质量徽标（数据完整 / 部分覆盖 / 质量降级 / 质量未知，按状态着色）、`来源已不可用`、`含回填样本` 徽标，替代原来的整行说明句；身份行只列主体、来源（类型用中文标签）与观测时间。操作保持「打开记录 / 返回主体证据 / 横向比较」。
- 正文由 renderer registry 输出一张分区卡：主机 / 探测趋势以摘要 chip（覆盖、精度、缺口、峰值）加每指标一张约 140px 高的小图（两列网格，指标中文名，峰值写在图头）；监控事件与命令审计为时间线（严重度 / 结果徽标，恢复事件不沿用告警色，退出码只在非 0 时显示）；IP 质量为摘要 chip 加数据库 / 服务解锁两列；订阅成本为账单、折算、预算（`<progress>`）、覆盖四个瓷砖，预算状态未知或月度额度为 0 时只写「无法判定」及原因，不画进度。图头、提示与峰值用完整单位格式（如 `1.4 MB/s`、`1小时 30分钟`）；纵轴保持同一量纲但用紧凑刻度（至多三位有效数字，速率保留 `/s`；按绝对值、以舍入后的系数选单位：字节到 1000 即按 1024 进位，时长依次为秒 / 分 / 时 / 天 / 年（阈值 60 / 60 / 24 / 365，年按 365 天），负刻度补回负号，如 `999KB/s`、`1.5时`、`-37天`、`2.7年`），刻度槽固定 56px 不裁切。图表可访问名称用中文指标名。服务端枚举只按自有键映射中文（`Object.hasOwn`，`__proto__` 等原样显示），未知值原样显示，不猜测含义；监控事件类型沿用 `STATE_CHANGE_EVENT_TYPE_LABELS`。
- 快照 ID、类型 / schema / renderer 版本、观测 / 捕获 / 引用时间、请求 / 实际窗口、精度、来源修订与水位、生成版本、敏感级别、保留策略与字段处理明细只在默认折叠的「技术细节」中；折叠摘要显示已处理字段数。
- 不支持或解码失败仍 fail closed：只显示「不支持的证据类型」，不渲染载荷；技术细节仍可查看信封元数据。
- 浏览器合同：`web/e2e/evidence-snapshot.spec.ts` 覆盖六种证据在 1440/1024/390 无横向溢出、技术细节折叠、回填 / 来源不可用徽标、fail closed，以及五主题 settled axe。

### Host evidence calculation v2

- host read model 的 `calculation_version` 为 `monitoring-evidence/v2` 时，每个 metric 必须完整包含 `sample_count`、`maintenance_count`、`backfilled_count`、`source_layer`、`source_granularity_seconds`；缺字段、非法范围/来源/秒粒度或不符合参考指标选择的 bucket 失败关闭。版本缺失或 v1 按旧 host 合同；未知版本拒绝。probe 不升级。
- v2 bucket 接受 `mixed`，呈现为“混合来源”；metric/peak 只接受 raw/daily_aggregate。指标显示自身有效样本数和来源，共享数标为“覆盖参考样本”，不得冒充 CPU 有效数；旧 v1 缺 metric 元数据时仅在读模型沿用 bucket 元数据。
- v2 gap 必须有合法 host metric，按 `(series_id,metric,start)` 排序/校验互斥，只与含同一指标的 bucket 检查重叠。不同指标缺口可以同窗重叠，内存正常桶不遮蔽 CPU 缺口；quality.gap_count 计指标缺口，有任一 gap 必须 partial。
- 曲线断开只受同指标的新 gap 或无 metric 的 legacy series gap 影响。旧 v1/probe 保持原 series gap 规则；不填缺失值，不把未知当零。v1/v2 跨版本比较由原 calculation_version_incompatible 边界拒绝，导出保留指标元数据。
