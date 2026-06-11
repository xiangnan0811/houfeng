# 前端开发规范

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

> 候风 / Houfeng Fleet Control Plane 前端（`web/`）开发的最佳实践。

---

## Overview

本目录汇集前端（React 19 + TypeScript + Vite SPA）相关规范。每份文件聚焦一个主题，请按主题填充团队真实约定，不要写理想化模式。

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | `web/` 目录结构与文件归属 | 已填实 |
| [Component Conventions](./component-conventions.md) | 页面 / 组件 / 共享原子的拆分与命名 | 已填实 |
| [State and Data](./state-and-data.md) | API client、数据获取、状态管理与类型 | 已填实 |
| [Styling Guidelines](./styling-guidelines.md) | 设计令牌、CSS / 类名约定与暗色优先 | 已填实 |
| [Quality Guidelines](./quality-guidelines.md) | Lint / 测试 / 评审标准与禁止模式 | 已填实 |

---

## How to Fill These Guidelines

针对每份 guide：

1. 写**当前代码里实际存在**的约定（而不是想要的理想形态）
2. 引用 `web/` 下的**真实文件路径**（不要凭空假设）
3. 列出明确禁止的反模式与原因
4. 把团队踩过的坑沉淀进来

目标是让 AI 助手与新成员都能基于本仓库现状写出风格一致的代码。

---

**语言**：所有文档使用**中文**撰写，必要时英文术语原样保留（与项目 `CLAUDE.md` 风格一致）。
