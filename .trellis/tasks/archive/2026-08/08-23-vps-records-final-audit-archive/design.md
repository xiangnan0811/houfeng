# VPS 记录平台最终审计与归档设计

## 1. Authority model

最终审计按以下优先级确定事实：

1. protected main 上的实际代码与配置；
2. selected commit、PR、required CI、main CI/合入后验证；
3. Trellis task 状态、归档路径和 current handoff；
4. 历史 child 的验收工件。

较低优先级若与较高优先级冲突，停止归档并修正 current authority；不得通过修改
历史 child 来消除冲突。

## 2. Audit matrix

| 维度 | 必查事实 | 允许的最终结论 |
|---|---|---|
| 功能交付 | 原 12 child 均在 main 且已归档 | `12/12` 功能 child 完成 |
| 收口交付 | overview child 真实接线并在 main | 可见管理遗留已关闭 |
| 永久删除 | handler/flags/readiness 仍 fail-closed | 未实现、未启用、已退出当前范围 |
| 其他遗留 | activity/sticky/mixed 仍缺失 | 未实现/未验证、已接受延期 |
| Git/CI | selected refs、PR、required/main CI 一致 | 可引用的交付证据 |
| 工作区 | 无未解释的重叠 dirty state | 归档不会吞掉用户改动 |

## 3. Documentation transaction

原父任务 `prd.md`、`implement.md` 和最新 handoff 作为一个逻辑事务在同一文档 PR
中更新。每个工件都要包含：决定、当前安全状态、明确延期、future trigger、
overview 完成证据和计数语义。若三者无法在同一提交通过一致性检查，则不合入。

历史审计和已归档 child 是不可变证据，不做 retrospective rewrite。需要解释旧
blocker 时，在 current handoff 指向历史证据并说明其状态从“未满足能力”变为
“用户决定不在本任务交付的能力边界”。

## 4. Archive state machine

```text
planning
  └─ entry gate verified
      └─ current-authority PR merged + main verified
          └─ archive final-audit child
              └─ validate closeout parent children complete
                  └─ archive closeout parent
                      └─ validate original parent current authority
                          └─ archive original parent
```

每个箭头前重新运行 read-only 状态检查。归档动作产生的路径/metadata 改动必须在
受保护流程中留下 Git 证据；若项目命令不能在不直接修改 main 的情况下表达该
顺序，停止并建立专门非 main 归档提交/PR，不绕过分支治理。

## 5. Failure and recovery

- overview 证据不完整：返回 overview child 修复，不启动文档 PR。
- 文档口径矛盾：保持 planning，修正文档分支。
- dirty state 与归档路径重叠：保留用户状态，停止归档并报告准确路径。
- 归档中途失败：依靠非 main Git 提交恢复 task 目录/metadata，重新 validate；
  不使用 destructive reset/clean。
- post-merge CI 失败：原父任务保持 planning，在同一受保护流程修复后再继续。

## 6. No-release boundary

此 child 只有 Trellis 文档和归档状态，不单独触发产品 release 目标。若仓库自动
创建 Release Please 或镜像工作流，只记录实际状态；不为了归档伪造版本发布，
也不把无产品变更的 release 作为完成门禁。
