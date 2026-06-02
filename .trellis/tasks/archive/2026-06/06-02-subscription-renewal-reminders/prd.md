# 续费提醒

## Goal

Subscription reminder scan, notification reuse, audit records, dedupe deliveries and decision-attention reminders.

## Requirements

- 续费提醒作为 subscription reminder，不伪装成 incident。
- 提醒发送复用现有 Telegram/Feishu 通知能力和 `notification_records` 审计。
- 提醒投递按 `subscription_id + renew_at + offset_days + kind + channel` 去重。
- 已取消/过期订阅、已归档/取消 VPS 不发普通续费提醒；取消/迁移/已取消自动续费但仍有临近风险的资产进入决策关注提醒。

## Acceptance Criteria

- [ ] 默认提醒窗口为 `14/7/1`，用户设置窗口不能超过 `max_reminder_lead_days`。
- [ ] 重复扫描不会重复审计或发送同一个提醒组合。
- [ ] 发送、失败、抑制状态可落到 reminder delivery 与 notification audit。
- [ ] Worker 支持上下文取消，失败后下轮可重试。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
