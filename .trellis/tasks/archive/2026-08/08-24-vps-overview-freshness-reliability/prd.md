# Restore VPS overview source isolation and freshness

## Goal

关闭 parent 的 I-03：source 读取独立退化，relation 具备完整 section state，renewal freshness
真实，Web 在每个 section 本地呈现 freshness 与 retry。

## Requirements

- identity 保持 fatal；identity 成功后 monitoring、IP、subscription、services、domains、
  activity 必须并行或以等价机制获得独立执行机会，并在整体 budget 内分别映射
  timeout/unavailable。
- `RelationSummary` 增加 `SectionState`；monitoring/subscription 复用其 authority section，
  services/domains 成功与失败各自保留 state/reason，failure count 不得冒充可信零。
- 成功读取的 last-success 以本次读取或权威 persisted observation 为准；`next_renew_at` 不得
  写入 observation/last-success；任何 timestamp 不晚于 `generated_at`。
- summary、recent activity 和 relation card 本地显示 state、安全 reason、last success 与
  refresh callback；ready/empty 与 unavailable 视觉和语义可区分。
- 不暴露 projector global head、raw backend error 或 secret；保持 overview collection 非 null。

## Acceptance Criteria

- [x] slow monitoring/timeout 测试证明 IP、renewal、services、domains、activity 仍能完成并保持 ready。
- [x] monitoring/IP/subscription/activity/service/domain 的 error matrix 都生成独立 section；
  relation failure 有 unavailable/reason 且 UI 不显示成可信普通零。
- [x] future renewal fixture 的 observed/last-success 均 `<= generated_at`，deadline 仍正确保留
  在 anomaly/event 语义。
- [x] Web section 本地 state/reason/last-success/retry 通过 unit、Axe、390px 与 production
  preview；健康态不增加 anomaly chrome。
- [x] focused Go/Web、full Go/Web/Chromium 与 Records browser 通过。

## Out of Scope

- 新 source、数据库 schema/migration、正式 mixed-load harness。
- anomaly route/command 与 React Router dependency（由 sibling action child 负责）。
