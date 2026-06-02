# 后端开发规范

> **权威来源**：`CLAUDE.md` + 业务/结构以 `docs/design/v1-baseline/` frozen 子集（architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow）为准，视觉以 `docs/design/v2-houfeng/`（design-language / component-spec）为准。冲突时以前述顺序为准。本项目处于初始开发阶段，规则会随代码演进调整。

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
| [Quality Guidelines](./quality-guidelines.md) | Makefile 质量门、测试约定、review checklist 与禁止模式 | 已填实 |
| [Logging Guidelines](./logging-guidelines.md) | `log/slog` 使用、level、结构化字段、敏感信息与 agent/center 差异 | 已填实 |
| [Subscription Cost Center](./subscription-cost-center.md) | 订阅成本中枢、汇率、预算、续费提醒、通知审计与 Dashboard 边界 | 已填实 |

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
