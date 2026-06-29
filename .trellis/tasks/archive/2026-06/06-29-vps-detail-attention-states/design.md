# VPS detail attention state integration design

## Problem

The VPS detail page already renders top `judgement.attentionItems`, but the model still exposes the old single `contextAction`. Tests also treat `contextAction` as the expected source for `运行观测需要核对`, `缺少当前订阅`, and `订阅证据暂不可用`.

This keeps the old middle-page mental model alive even when the current page no longer renders `VPSContextActionPanel`. The page also still sends subscription load errors to the middle `vps-detail-feedback-stack`, which duplicates a persistent state that the top judgement already owns.

## Design Decision

Make `judgement.attentionItems` the only frontend source of persistent VPS attention states.

Remove `contextAction` from `VPSDetailOverviewModel` and remove `buildContextAction`. The attention-builder remains as the central place that classifies the current VPS state. No page component should receive or render a separate "context action" for VPS detail.

## Attention Item Scope

The top current judgement owns persistent states that describe the current VPS condition:

1. `取消/退役`
   - Trigger: cancellation or migration lifecycle / renewal decision / cancellation preview attention.
   - Tone: `critical`.
   - Action: `处理取消/退役`.
2. `运行观测需要核对`
   - Trigger: primary MonitoringInstance health is not normal, or active incident count is greater than 0.
   - Tone: derived from monitoring health.
   - Actions: `查看监控实例`, `监控观测`.
3. Subscription attention
   - `订阅证据暂不可用` for load failure.
   - `缺少当前订阅` for successful empty subscription response.
   - `续费时间需要关注` / `自动续费已取消` for renewal-risk facts.
   - Actions: existing subscription / decision / validity-extension actions.
4. `缺少运行观测`
   - Trigger: no primary monitoring instance.
   - Actions: `接入/升级 agent`, `关联已有监控实例`.
5. `IP 质量暂不可用`
   - Trigger: IP quality load error.
   - Action: IP quality detail link.

Multiple items render together. The row `动作` remains a short summary of the first attention action, with `取消/退役` taking precedence when cancellation work exists. The detailed item list carries the actual multi-action surface.

## Middle Page Boundary

`VPSDetailPage` keeps the order:

1. `VPSDetailOverviewPanel`
2. short operation feedback stack
3. `VPSRelatedOverview`
4. `VPSSingleMachineLedger`
5. `VPSIPQualitySection`

The feedback stack is only for short-lived action results and submit errors. It must not show persistent classification facts that `attentionItems` already cover. Therefore `state.subscriptionsError` is removed from `pageFeedbackCandidates`.

Examples that still belong in feedback stack:

- renewal decision submit result;
- form validation / submit failures after the user acts;
- link/unlink result;
- lifecycle action result;
- service/domain/experience creation notices.

Examples that do not belong there:

- subscription list failed to load;
- missing subscription;
- missing monitoring instance;
- monitoring health abnormal;
- IP quality unavailable.

## UI Behavior

No new middle section is added. Top attention items stay compact:

- short title;
- one concise reason;
- primary action and optional secondary actions.

The design does not add explanatory helper text such as "click title to view details" or "only temporary actions are kept". The related overview continues to show contextual facts and quick actions, but it is not the current judgement surface.

## Compatibility

- No API change.
- No backend type or database change.
- Existing modal modes and routes remain.
- `VPSContextAction` type may be retained and renamed only if useful for attention items; the old `contextAction` field is removed from the public model shape.

## Test Plan

Model tests:

- stable VPS has no attention items and no `contextAction` field.
- monitoring abnormal state creates only a top attention item.
- subscription load failure and missing subscription create top attention items.
- cancellation + monitoring + renewal risk can coexist.

Page tests:

- `运行观测需要核对` is scoped inside `aria-label="当前判断"`.
- no `aria-label="需要处理的状态"` region exists.
- subscription load failure appears in current judgement but not in `aria-label="VPS 操作反馈"`.
- multi-attention scenario shows cancellation, monitoring, and renewal/subscription items together in current judgement.
- attention action buttons/links keep existing modal / route behavior.

Browser sanity:

- run the local web preview with mock API.
- inspect desktop and mobile VPS detail route containing multiple attention states.
- confirm page middle shows related overview / ledger / IP quality summary, not a standalone attention strip.
