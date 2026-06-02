# 汇率与基准货币

## Goal

Subscription cost settings, base currency, exchange rate providers, cache table, refresh API and worker.

## Requirements

- 订阅成本设置默认 `base_currency=CNY`、`exchange_rate_provider=frankfurter`、提醒窗口 `14/7/1`、汇率过期阈值 36 小时。
- 支持 Frankfurter 与 Fixer Provider；Fixer key 只能通过设置或环境注入，API 响应、日志、迁移、测试不得包含明文 key。
- 汇率缓存保存 provider/base/quote/rate/rate_date/fetched_at/stale 可判断所需事实。
- 提供手动刷新 API 和后台刷新 worker，Provider 失败时不阻塞订阅 CRUD。

## Acceptance Criteria

- [ ] `GET/PUT /api/subscriptions/settings` 可读写订阅成本设置，响应只暴露 `fixer_configured`/masked summary。
- [ ] `POST /api/subscriptions/exchange-rates/refresh` 按活跃订阅币种刷新到基准货币。
- [ ] Frankfurter 默认 Provider 可用；Fixer Provider 在缺 key 时返回明确错误且不泄漏密钥。
- [ ] 汇率失败、缺失或过期时成本 read model 可标记 stale。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
