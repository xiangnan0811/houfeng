# VPS 记录平台父任务收口设计

## 1. 设计目标

本任务只负责程序级依赖、范围决策和最终归档边界，不直接承载产品实现。原父任务
继续按 `12/12` 个功能 child 计数；本收口任务及其两个 child 是后续协调节点，
不是新增的第 13 个产品能力。

## 2. 任务拓扑与顺序

```text
07-13-vps-detail-experience-design（原父任务，保持 planning）
└── 08-23-vps-records-parent-closeout（本协调父任务）
    ├── 08-23-vps-overview-management-actions
    │   └── 产品改动、测试、PR、required CI、protected-main 合入后验证
    └── 08-23-vps-records-final-audit-archive
        └── 决策固化、跨任务审计、文档 PR、归档
```

依赖是硬串行关系：最终审计 child 只有在 overview 管理 child 的选定提交已经
合入 protected main，且所需 CI 和合入后验证有可引用证据后才允许启动。

## 3. 范围分类

| 遗留 | 当前处置 | 是否阻止归档 | Future trigger |
|---|---|---:|---|
| 新 overview 管理菜单为空操作 | 独立产品 child 修复 | 是 | 本轮直接完成 |
| activity group-granted digest | 明确延期，保持未实现 | 否 | viewer 需要跨 project digest 权限时新建任务 |
| comparison sticky 行标题 | 明确延期，保持未实现 | 否 | 390px 对比出现实际定位/可用性问题时新建任务 |
| mixed-load harness | 明确延期，保持未验证 | 否 | 建立正式容量 SLO、目标硬件或回归基准时新建任务 |
| 运维记录永久删除 | 放弃当前方案，保持关闭 | 否 | 真实用户、合规承诺、长期备份或单记录不可恢复删除需求出现时新建任务 |

永久删除延期后，当前七项 readiness 缺口、production backup/restore pairing
和 nil HTTP handler 是“为什么能力仍安全关闭”的证据，不再是本父任务的交付
清单。普通归档/恢复与整体重建一次性测试环境不受影响。

## 4. 权威工件策略

- 原父任务的当前 `prd.md`、`implement.md` 和最新 handoff 必须在最终审计 child
  中一起更新，避免同一决定出现多个口径。
- 已归档 child 的 PRD、设计、实现和验收结论保持历史原样；只在原父任务 current
  pointer 中解释后续范围决定。
- overview 管理产品 child 自己持有代码、测试、PR 和 CI 证据；本协调父任务只
  引用它，不复制实现完成声明。
- 最终 handoff 同时列出“已完成”“明确延期”“未来触发条件”和“验证证据”，
  不用“已接受”替代“已实现”。

## 5. 归档门禁

允许归档的必要条件：

1. overview 管理 child 已在 protected main，五类动作的测试和 required CI 有
   当前提交证据；
2. 永久删除保持默认关闭，production handler 仍未注册；
3. 三项非删除遗留明确标为未实现/未验证且已延期；
4. 原父任务与本收口任务的 PRD、implement、handoff 和 task tree 数字一致；
5. Trellis validate、文档一致性检查和完整 diff 复核通过；
6. 所有变更通过非 main 分支和 PR 进入 protected main。

若任一门禁失败，保持原父任务和本收口任务为 `planning`，修复对应 child；不得
通过改写历史状态或降低证据口径强行归档。

## 6. 回退与停止边界

- overview 产品 PR 未合入时，最终审计 child 不启动，原父任务不归档。
- 文档 PR 出现矛盾时，只修正文档分支；不顺带启用永久删除或修改产品默认值。
- 归档动作若发现 Trellis child 树或 protected-main 事实不一致，停止并重新审计；
  已归档历史 child 不回滚、不重开。
