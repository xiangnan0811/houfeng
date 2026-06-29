# 调整 VPS 详情页单机台账入口

## Goal

降低 VPS 详情页 `单机台账` 区域的导航噪音，把 `资产历史`、`服务`、`域名` 这三个入口从单机台账迁移到顶部 `VPS 综合基础信息` 操作区，并放在 `调整决策` 左侧。

## Requirements

- 顶部 `VPS 综合基础信息` 操作区新增三个显式入口：
  - `资产历史`：打开现有 `timeline-detail` modal。
  - `服务`：打开现有 `services-detail` modal。
  - `域名`：打开现有 `domains-detail` modal。
- 新入口位置必须在 `调整决策` 左侧，优先级高于 `调整决策`，但仍使用项目现有按钮/样式体系。
- `单机台账` 区域不再展示 `资产历史`、`服务`、`域名` 这三个导航按钮，避免台账区变成第二个入口集合。
- 保留单机台账内的事实展示能力，例如近期记录、承载清单、关键变化；只移除这三个入口按钮，不删除台账内容。
- 保持现有 modal-only 交互：点击三个入口都打开居中 modal，不跳转、不在页面内插入新区域。
- 不调整外部订阅、监控、IP 质量、Target 等页面。

## Acceptance Criteria

- [ ] 顶部 `VPS 综合基础信息` 中 `资产历史`、`服务`、`域名` 三个按钮出现在 `调整决策` 左侧。
- [ ] 点击顶部 `资产历史` 打开 `资产历史` dialog。
- [ ] 点击顶部 `服务` 打开 `服务详情` dialog。
- [ ] 点击顶部 `域名` 打开 `域名详情` dialog。
- [ ] `单机台账` 区域不再渲染这三个入口按钮。
- [ ] 关联概览和单机台账的事实展示不回归。
- [ ] `VPSDetailPage.test.tsx` 覆盖入口位置和 modal 行为。
- [ ] 通过 web lint、相关测试和 build。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
