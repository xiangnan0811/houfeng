# Harden VPS overview gate and DTO validation

## Goal

关闭 parent 的 I-02：runtime-validate overview success DTO，把 transport、JSON decode 与
2xx contract drift 显示为可重试错误，legacy 只由明确 feature/capability contract 选择。

## Requirements

- `recordsApi.getVPSOverview` 必须在 overview 自己的 façade 边界把 malformed success JSON
  转成稳定、安全的 typed decode error；共享 `requestJSON` 的既有全仓行为保持不变，response
  body 不进入用户消息或日志。
- overview decoder 必须验证 object、identity、summary cells/section、recent activity、arrays、
  capability、anomaly action 和 relation 的必需结构；先验证再做允许的 collection normalization。
- valid DTO 缺少 `records_v2_read` 仍按 compatibility contract 进入 legacy；404 仍显示 not
  found；只有显式 `overview_unavailable` 可 legacy；其他 503、`ApiError` 和 unknown error 显示
  overview error/retry。
- gate 在 route-param 变化和 stale promise 处理上保持现有 cancellation/seed 行为。

## Acceptance Criteria

- [x] malformed JSON、fetch `TypeError`、`SyntaxError`、`null`、`{}`、invalid identity/section/
  action/relation 的 RED 测试全部转 GREEN，且不会加载 legacy chunk。
- [x] valid capability-on 只请求一次并 seed overview；valid capability-off 与明确 unavailable
  才加载 legacy；404 与 500 行为保持合同。
- [x] error copy 稳定且不泄露 body/URL/internal detail；retry 能重新执行 gate probe。
- [x] focused API/client/page tests、Web lint/build/full suite 与 Chromium 通过。

## Out of Scope

- 为整个仓库引入新的 schema-validation dependency；重写其他 API DTO normalizer。
- 改变 backend overview JSON 合同（除 sibling freshness/action child 已批准字段）。
