# 不可信内容准入合同

## Scenario: 不可信内容准入的 parser 与资源边界

### 1. Scope / Trigger

- Trigger: 修改 `internal/center/attachments` 的 magic/MIME/extension 分类、PDF/图片/archive parser、流式文本主动内容检测或其 context/资源上限。
- 目标：第三方 parser 的容错和修复能力不能代替准入层的精确格式判定；内存有界、解析工作量有界和可取消必须分别证明。

### 2. Signatures

- 顶层入口：`attachments.AdmitContent(context.Context, attachments.AdmissionRequest, attachments.AdmissionLimits) (attachments.AdmissionResult, error)`。
- 流式检测器必须接收 `context.Context`，并通过 `observe(context.Context, []byte) error` 一类接口把取消传播给顶层和 archive member 扫描。
- 结构化格式在调用容错 parser 前必须完成 owned exact magic/container guard；例如 PDF bytes 必须从 offset 0 以 `%PDF-` 开始。

### 3. Contracts

- 扩展名与 declared MIME 只确定候选 profile，不证明实际 bytes 属于该格式。准入层必须先验证 exact leading magic 或完整 container 边界，再调用第三方 parser。
- 不得把第三方 parser 的 header 搜索、offset repair、trailing-data 容忍或错误恢复当作 classifier。外来、active 或其他格式前缀加在合法 payload 前仍必须 fail closed。
- retained bytes 与 parse work 是两个独立预算。限制 candidate 为 4 KiB 不足以证明 CPU 有界；每个 candidate 只能 token 化固定次数，所有 candidate 的累计工作必须是输入字节的固定倍数。
- 长循环和 expensive parse 前必须检查调用方 context。顶层完整文本扫描与 archive chunk 扫描必须共享相同检测策略和取消语义。
- 安全 allowlist 必须按完整候选判定。URI-shaped Markdown 候选只有在无空白、`url.ParseRequestURI` 成功、scheme 为 `http`/`https` 且 host 非空时才可放行；其余 fail closed。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| exact `%PDF-` header + strict-valid PDF | 进入有界 pdfcpu 校验；通过后返回 PDF profile |
| GIF/HTML/其他前缀 + 后置 strict-valid PDF | 在 parser 前返回 `ErrAdmissionRejected` |
| `javascript:` / `data:` / `mailto:` / `http:foo` angle candidate | 顶层文本和 archive member 都返回 `ErrAdmissionRejected` |
| valid host-bearing HTTP(S) autolink | 允许继续文本准入 |
| quoted `>` 反复出现在 incomplete tag candidate | 每 candidate 最多一次 tokenizer 调用，随后 fail closed 或在 retained-byte 上限拒绝 |
| context 在完整文本或 archive member 扫描中取消 | 返回 `context.Canceled` / `context.DeadlineExceeded`，不继续 parser 工作 |

### 5. Good/Base/Bad Cases

- Good: 准入层先验证 offset 0 的 `%PDF-`，再让 pdfcpu 做 object/action/page/累计解码预算校验。
- Good: archive member 按 chunk 复用同一 context-aware active-text detector，不全量保留普通成员。
- Base: 普通未知 angle text 不是 URI-shaped/active markup 时仍可作为 UTF-8 文本接收。
- Bad: 因 pdfcpu 能在流中搜索 `%PDF-` 并修复 xref offset，就接受 `GIF89a%PDF-...`。
- Bad: candidate 只限制为 4 KiB，但每遇到一个 quoted `>` 都从头 token 化增长中的 candidate。

### 6. Tests Required

- RED/GREEN regression 必须同时覆盖 exact-valid 正例与 foreign/active-prefix 反例，并断言反例在准入层返回 `ErrAdmissionRejected`。
- 顶层文本和 archive member 必须共同覆盖 unsafe URI-shaped candidate、valid HTTP(S)、跨 chunk active markup 和取消传播。
- CPU 防线使用确定性工作量断言（例如 tokenizer call count 或累计 parse-work budget）；不能只用易抖动的 wall-clock threshold。
- 保留 candidate retained-byte 上限、完整 package tests、race、vet、module verification，以及 admission/archive hostile fuzz targets；fuzz 不得在仓库留下生成 corpus。

### 7. Wrong vs Correct

```go
// Wrong: tolerant parser acceptance is treated as exact format identity.
pdfCtx, err := pdfcpu.ReadWithContext(ctx, bytes.NewReader(content), cfg)
```

```go
// Correct: the admission layer owns exact magic before parser recovery runs.
if !bytes.HasPrefix(content, []byte("%PDF-")) {
	return admissionRejectedError("PDF signature mismatch")
}
pdfCtx, err := pdfcpu.ReadWithContext(ctx, bytes.NewReader(content), cfg)
```
