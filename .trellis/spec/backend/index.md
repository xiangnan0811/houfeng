# 后端开发规范

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

> 候风 / Houfeng Fleet Control Plane 后端（Go center + systemd agent + PostgreSQL）开发的最佳实践。

---

## Overview

本目录汇集后端（Go + pgx/v5 + PostgreSQL + systemd agent）相关规范。每份文件都记录当前代码库实际存在的约定、真实文件路径、禁止模式与已知 gap，供后续 Trellis implement/check agent 执行任务时加载。

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | `cmd/`、`agent/`、`internal/center/`、`internal/contracts/`、`db/migrations/` 的目录归属与新增代码落点 | 已填实 |
| [Database Guidelines](./database-guidelines.md) | PostgreSQL + pgx/v5、手写 SQL、迁移、事务与模型不变量 | 已填实 |
| [Error Handling](./error-handling.md) | sentinel error、`errors.Is`、HTTP/agent 错误转换与 panic 政策 | 已填实 |
| [Record Authorization](./record-authorization.md) | `recordauth` 可信 actor、持久化 group hydration、canonical scope/source evidence 与统一资源授权边界 | 已填实 |
| [Records Collaboration and Notification Contract](./record-collaboration-notification-contract.md) | owner/action/comment/watch/inbox、permission-safe delivery、typed provider 与 exact deletion/restore 合同 | 已填实 |
| [Quality Guidelines](./quality-guidelines.md) | Makefile 质量门、测试约定、review checklist 与禁止模式 | 已填实 |
| [Logging Guidelines](./logging-guidelines.md) | `log/slog` 使用、level、结构化字段、敏感信息与 agent/center 差异 | 已填实 |
| [Subscription Cost Center](./subscription-cost-center.md) | 订阅成本中枢、汇率、预算、续费提醒、通知审计与 Dashboard 边界 | 已填实 |
| [IP Quality Contract](./ip-quality-contract.md) | VPS IP 质量低频采集、agent sync、center 入库/API、资产决策证据与 raw JSON 安全边界 | 已填实 |
| [Evidence Snapshot Contract](./evidence-snapshot-contract.md) | evidence authoritative source、canonical adapter、监控事件 producer、成本/命令/资产历史失败关闭合同 | 已填实 |
| [Record Search Index Contract](./record-search-index-contract.md) | 搜索派生投影、代次发布 CAS、影子重建租约/断点、上下文绑定游标、单一搜索路径与删除穿透 | 已填实 |
| [Record Activity Projection Contract](./record-activity-projection.md) | 活动投影 keyset 扫描、auth_scope 盖章、ActiveGeneration、Export Readiness、subject freshness 边界 | 已填实 |

---

## How to Fill These Guidelines

维护这些 guide 时：

1. 写**当前代码里实际存在**的约定（而不是想要的理想形态）
2. 引用仓库内**真实文件路径**（不要凭空假设）
3. 列出明确禁止的反模式与原因
4. 把团队踩过的坑沉淀进对应 guide 的已知 gap / Common Mistakes

目标是让 AI 助手与新成员都能基于本仓库现状写出风格一致、可验证的后端代码。

---

**语言**：所有文档使用**中文**撰写，必要时英文术语、Go/SQL 标识符和路径原样保留（与项目 `CLAUDE.md` 风格一致）。
