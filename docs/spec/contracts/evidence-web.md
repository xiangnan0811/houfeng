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
