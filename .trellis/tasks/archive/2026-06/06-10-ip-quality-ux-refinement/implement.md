# IP质量页面体验优化实施计划

## Preconditions

- 用户已审核并同意浏览器 demo 的信息架构和布局。
- 视觉要求已更新：正式实现必须贴合当前项目主题配色和氛围。
- 进入执行前必须由用户明确批准 `task.py start`。

## Implementation Checklist

1. 读取开发规范
   - `.trellis/spec/web/component-conventions.md`
   - `.trellis/spec/web/styling-guidelines.md`
   - `.trellis/spec/web/state-and-data.md`
   - `.trellis/spec/web/quality-guidelines.md`

2. 调整展示 helper
   - 在 `web/src/components/ip-quality/ipQualityPresentation.ts` 增加或调整摘要、证据 chip、服务说明 helper。
   - 确保 helper 不返回 `default_probe`、`not_configured` 等内部文本给主视图。

3. 调整 VPS 详情摘要
   - 修改 `VPSIPQualitySection.tsx`。
   - 移除 header 中的 `IPQualityBadge`。
   - 移除底部重复风险 badge 和服务 chip 列表。
   - 保留 score、风险信号、服务解锁、采集覆盖和完整报告入口。

4. 调整 IP 质量详情页
   - 修改 `IPQualityDashboard.tsx`。
   - Header 右上角只保留返回按钮。
   - Provider 表格证据列改为紧凑信号 chip，不显示长错误摘要。
   - Service unlock header 统计横排右对齐。
   - Service card 移除 probe status、source、latency、default_probe 主视图文案。
   - unknown / failed / not configured 用 neutral/offline 弱化。

5. 调整 CSS
   - 修改 `web/src/index.css` 中 `vps-ip-quality-*` 相关样式。
   - 使用现有变量，不写独立色板。
   - 服务矩阵桌面使用稳定列数，7 张卡避免 6 + 1。
   - Provider 表格保持内部横向滚动，不造成页面整体横向溢出。

6. 补充测试
   - 增加或更新 `VPSIPQualitySection` / `IPQualityDashboard` 相关测试。
   - 断言不展示用户指出的异常文本和重复 badge。
   - 断言关键入口仍存在。

7. 本地验证
   - `cd web && npm run lint`
   - `cd web && npm run test -- --run`
   - `cd web && npm run build`
   - 必要时启动 `npm run dev` 做浏览器 sanity，检查桌面和移动端。

8. 审查与修复
   - 对照 PRD acceptance criteria 做自审。
   - 若发现布局、文案、主题或测试缺口，先修复再进入 finish 流程。

## Risk Points

- `VPSIPQualitySection.tsx` 当前有多段重复 badge 和 chip，删除时要确认空态与 error 态不受影响。
- `IPQualityDashboard.tsx` 表格列多，CSS 调整需要保证桌面内部滚动和移动端页面不整体横向溢出。
- `error_summary` 仍应在诊断层可见，但不能进入主证据列。
- 服务解锁卡片文案需要过滤内部字段，同时不能隐藏真正失败原因。

## Validation Commands

```bash
cd web && npm run lint
cd web && npm run test -- --run
cd web && npm run build
```

## Review Gate

当前任务仍处于 planning。用户确认这些规划产物后，才能执行：

```bash
python3 ./.trellis/scripts/task.py start .trellis/tasks/06-10-ip-quality-ux-refinement
```
