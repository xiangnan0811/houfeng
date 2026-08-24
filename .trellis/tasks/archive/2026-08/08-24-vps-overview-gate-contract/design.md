# VPS 概览门控与 DTO 校验技术设计

## 1. 决策与边界

本 child 只修复 overview success-body 的运行时合同与 route gate 分类，不改变共享
`apiRequest` 的全仓语义。`apiRequest.requestJSON` 继续把非 2xx 转为 `ApiError`，并让
fetch/decode 原始错误向上抛出；`recordsApi.getVPSOverview` 是唯一知道完整 overview wire
shape 的 owner，因此在这里完成 typed decode、投影和安全错误归类。

采用局部手写 decoder，而不是引入 schema dependency，也不把 success decoder 塞进共享
transport：这保持 records lazy chunk 的依赖边界，避免改变其他 API 对 malformed 2xx 的既有
合同，并让 I-01/I-03 的最终 action/relation shape 有一个明确校验点。

## 2. Success decoder

在 `web/src/lib/recordsApi.ts` 导出不保存原始 payload 的
`InvalidVPSOverviewResponseError`，reason 只允许 `malformed_json|invalid_shape`。
`getVPSOverview` 仍请求 `unknown`：只把 success JSON 的 `SyntaxError` 转为
`malformed_json`；`ApiError`、network、abort 和其他异常保持原型。解析成功后由
`decodeVPSOverview` 校验并创建全新的 allowlisted object，不 spread 源 object。

decoder 必须覆盖 root、identity、summary section、recent activity、facts、capabilities、
anomaly/action 与 relation，包括 sibling child 增加的 relation `section` 和 route 可选性。
已知 required 字段不完整即失败；未知附加字段允许但不会投影。collection 必须是 array，
标量按 wire type 校验，relation count 必须是非负 safe integer，section state 必须是三值
enum，timestamp 只能是有效 RFC3339 string 或 null。capability 只有在完整 DTO 校验通过后才
参与门控。

## 3. Gate 状态机

`VPSDetailPage` 的初始 probe 使用显式 allowlist：

| 结果 | 页面模式 |
| --- | --- |
| valid DTO 含 `records_v2_read` | overview |
| valid DTO 不含 capability | legacy |
| 404 或 `resource_not_found` | not found |
| `ApiError.code === overview_unavailable` | legacy |
| 其他 ApiError、任意未标识 503、typed decoder error、network/unknown | error |

error 页面使用固定中文文案，不渲染 exception/body/URL，并提供 probe revision 驱动的“重试”。
每次 route param 或 revision 变化都开启新 probe；现有 cancellation/latest guard 阻止旧 promise
覆盖新 VPS。probe 成功后 seed 给 `useVPSOverview`，不触发重复首屏请求。

刷新阶段保留最后一次有效 overview；局部 freshness notice/retry 由 I-03 owner 呈现，避免两个
child 同时修改同一 UI 合同。本 child 只保证 invalid refresh 返回失败且不会降级 legacy。

## 4. 兼容、安全与回滚

- 无数据库、Go API 或 route 迁移；只校验既有及 sibling 已批准的 wire shape。
- additive unknown fields 兼容，missing/invalid known fields fail closed。
- typed error 不携带 response body、rejected value、internal URL 或原生 message。
- rollback 为还原本 child Web diff；不能以恢复静默 fallback 作为长期故障处理。

## 5. 文件所有权

- `web/src/lib/recordsApi.ts`、`web/src/lib/recordsApi.test.ts`
- `web/src/pages/VPSDetailPage.tsx`、`web/src/pages/VPSDetailPage.test.tsx`
- `web/src/pages/vps-detail/hooks/useVPSOverview.test.tsx`
- `web/e2e/page-states.spec.ts` 及必要的既有 fixture/profile

实现必须等待 freshness/action child 冻结最终 DTO 后完成 decoder；若并行，只允许先写不依赖
最终 relation/action shape 的 RED tests。
