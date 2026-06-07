# 统一交互弹窗修复 - Implement

## Checklist

1. 准备执行环境：
   - 使用 `fix/unified-interaction-modals` 非 main 分支。
   - 在 worktree 内启用 `sh scripts/setup-git-hooks.sh`。
   - 读取 web 组件、状态、样式和质量规范。
2. 盘点调用点：
   - `ActionConfirmationCard` 使用点。
   - `Modal` 使用点和已有嵌套风险。
   - Probe 删除摘要 DOM 注入。
   - Monitoring、Targets、VPS、Asset Decisions 页面内联编辑/确认状态。
3. 按 TDD 增量写测试：
   - `Modal` role/id/focus 测试。
   - `ActionConfirmationModal` 测试。
   - 页面关键迁移行为测试，优先覆盖 API 调用前必须出现确认弹窗、失败不关闭、取消清理。
4. 实现共享组件：
   - 修改 `Modal`。
   - 新增 `ActionConfirmationModal` 并迁移测试。
   - 保留或删除旧 `ActionConfirmationCard` 取决于是否还有使用点；最终生产代码不应使用旧内联确认。
5. 迁移页面：
   - Monitoring 列表/详情相关确认和快速编辑。
   - Targets 列表/详情相关确认、快速编辑和 Probe 删除。
   - VPS 归档/恢复与解除关联确认。
   - Monitoring binding conflict 三个动作的确认前置。
   - Asset Decisions 既有 Modal 内确认步骤。
6. 验证：
   - 先跑受影响测试。
   - `cd web && npm run lint`
   - `cd web && npm run test -- --run`
   - `cd web && npm run build`
   - `make verify-web`
7. 收尾：
   - 检查无嵌套 `dialog` 行为、无 `ActionConfirmationCard` 生产调用、无 Probe DOM 注入。
   - 更新任务状态和最终说明。

## Risky Files

- `web/src/components/atoms/Modal.tsx`
- `web/src/components/ActionConfirmationCard.tsx`
- `web/src/pages/MonitoringPage.tsx`
- `web/src/pages/MonitoringDetailPage.tsx`
- `web/src/pages/TargetsPage.tsx`
- `web/src/pages/TargetDetailPage.tsx`
- `web/src/pages/VPSPage.tsx`
- `web/src/pages/VPSDetailPage.tsx`
- `web/src/pages/AssetDecisionsPage.tsx`

## Validation Commands

```bash
cd web && npm run lint
cd web && npm run test -- --run
cd web && npm run build
make verify-web
```
