# Notification channel model

## Goal

补齐 `houfeng_codex_下一步开发计划.md` / `docs/release/v1-gap-checklist.md` 中的 gap #22：当前 Feishu 设置和 sender 已存在，但 incident 通知记录仍由 evaluator 的单一 `decision.Channel` 驱动，默认值是 `telegram`。这会让 Feishu-only 或 Telegram+Feishu mixed delivery 在 `notification_records.channel` 中被错误标记。该任务要把通知发送结果按真实 channel 展开并落库。

## What I already know

- `internal/center/incidents/evaluator.go` 在 started / escalated / recovered 的 `NotificationDecision` 中固定写入 `Channel: "telegram"`。
- `internal/center/incidents/service.go` 的 `settingsAwareNotifier.Send` 会根据 settings 尝试 Telegram fallback / persisted Telegram / Feishu，但接口只返回一个聚合 `error`，无法表达每个 channel 的独立状态。
- `appendNotificationRecords` 目前每个 notification decision 只追加一条 record，`Channel` 直接取 `decision.Channel`。
- `internal/center/notify/feishu.go` 已实现 Feishu text webhook sender，SettingsPage / Dashboard notification status 也已有 Feishu 配置摘要。
- 任务开始时，`docs/release/next-phase-plan.md` 明确指出“正式多通知通道模型”仍属于待完成范围；`docs/release/v1-gap-checklist.md` #22 仍为 Open。
- Dashboard contract 只能暴露通知配置布尔摘要，不能泄露 Telegram token/chat id 或 Feishu webhook URL。

## Requirements

- 引入稳定的 notification channel 领域类型/常量，至少覆盖 `telegram` 与 `feishu`，避免业务代码继续散落裸字符串。
- incident service 的通知发送链路必须能返回每个 channel 的 delivery result：`sent`、`failed`、`suppressed`。
- Feishu-only 设置生效时，通知必须通过 Feishu sender 发送，并写入 `notification_records.channel = "feishu"`，不能再记录为 `telegram`。
- Telegram + Feishu 同时配置时，同一次 incident notification 应按 channel 写多条记录，互不覆盖状态；一个 channel 失败不得抹掉另一个 channel 成功的记录。
- 保留既有 Telegram 行为：
  - fresh DB / 未持久化 Telegram settings 时继续允许 env fallback。
  - persisted Telegram `RuntimeManaged=false` 时继续走 env fallback。
  - persisted Telegram runtime-managed 但缺 token/chat id 时继续 suppressed，且不回退 env。
- 不改变 `notification_records` 表结构；当前 `channel text` 已能承载正式 channel 值。
- 不改变 Dashboard / Settings 的敏感字段暴露边界。

## Acceptance Criteria

- [ ] Feishu-only settings 会发送 Feishu，并生成 `channel=feishu`、`delivery_status=sent` 的 notification record。
- [ ] Telegram + Feishu settings 会为同一 incident notification 生成两条 records，channel 分别为 `telegram` / `feishu`。
- [ ] Mixed delivery 中任一 channel 失败时，该 channel 记录 `failed`，其他成功 channel 仍记录 `sent`。
- [ ] 通知策略关闭、maintenance/backfill suppression 或无可用 channel 时仍写入清晰的 suppressed record，不误记 Feishu-only 为 Telegram。
- [ ] 旧的 `Notifier` 调用方和 Telegram 单通道测试继续通过。
- [ ] gap #22 文档状态更新，Trellis 规范补充 notification channel model 契约。

## Definition of Done

- Go 单元测试覆盖 channel-aware dispatch 的 Feishu-only、mixed success、mixed partial failure、Telegram fallback/suppressed 回归。
- `make verify-go` 通过。
- Trellis check 完成；任务归档并记录 journal。
- 通过 feature branch 提 PR，PR CI 全绿后合并，随后同步本地 `main`。
- release/publish workflow 暂不处理。

## Out of Scope

- 不处理真实 VPS 数据问题。
- 不引入邮件、企业微信或用户可配置 channel priority。
- 不新增通知重试队列、幂等去重表或 notification_records schema 变更。
- 不改变 Dashboard 首屏信息架构或 SettingsPage 表单功能。
- 不处理 release/publish workflow。

## Technical Notes

- 主要代码路径：`internal/center/incidents/service.go`、`internal/center/incidents/types.go`、`internal/center/incidents/evaluator.go`、`internal/center/notify/feishu.go`、`cmd/houfeng-center/bootstrap.go`。
- Store 层 `notification_records.channel` 已是 `text not null`，本任务应复用现有 schema。
- Data flow: evaluator 产生 notification intent -> incident service 根据 policy/channel dispatcher 展开 delivery attempts -> store 逐条 insert notification record -> Dashboard 仍只读取配置布尔摘要。
- 日志不能包含 Telegram bot token/chat id 或 Feishu webhook URL；发送失败日志只记录 object identity 和 error。
