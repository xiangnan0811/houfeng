# 实施计划

## Ordered Checklist

1. 启动阶段
   - 确认当前分支为 `ux/asset-decisions-ia-redesign`。
   - 启用 hooks：`sh scripts/setup-git-hooks.sh`。
   - 启动任务：`python3 ./.trellis/scripts/task.py start .trellis/tasks/06-30-asset-decisions-ia-redesign`。

2. Mockup / 审查阶段
   - 在 ignored 的 `.superpowers/brainstorm/asset-decisions-redesign/` 创建静态 HTML mockup。
   - 使用现有截图和 mock-api 数据设计组合优先首屏。
   - 不提交 mockup，实施时以该结构作为视觉参照。

3. TDD 阶段
   - 修改 `web/src/pages/AssetDecisionsPage.test.tsx`，先写失败测试：
     - 首屏显示主工作卡和自动组扫描。
     - 首页不再常驻“决策路径”“下一步导览”“场景与记录”“续费证据区”“单台待处理队列”。
     - 次级入口仍能打开记录/场景/续费证据/单台队列，且同一时间只展开一个辅助区。
     - 深链继续打开对应 modal。
     - `record_id`、`manual_group_id`、`template_id`、`view=renewal`、`view=single_queue` 自动展开对应辅助区。
   - 运行目标测试并确认失败原因是旧 IA。

4. View model 与组件拆分
   - 新建 `web/src/pages/asset-decisions/assetDecisionViewModel.ts`，搬迁纯函数并补单测（若函数复杂）。
   - 新建主工作卡、自动组列表、次级入口组件。
   - 从 `AssetDecisionsPage.tsx` 移除对应 JSX，改为组合新组件。

5. 页面结构与样式
   - 删除首页常驻决策路径 section。
   - 将场景/记录/续费证据/单台队列改为一个 `secondaryWorkbench` 控制的按需展开区域；默认不渲染旧辅助 surface。
   - 把 `下一步导览` 的关键计数和优先工作压缩进主工作卡或次级入口摘要，不保留独立右侧导览 panel。
   - 在 `web/src/index.css` 收束 `asset-decision-*` 样式，保证桌面密度和移动端布局。

6. 回归工作流
   - 保留自动组详情、保存记录、自定义组合、模板、记录跟进、单台决策 drawer 的行为。
   - 更新所有受影响测试断言为新文案和新结构；旧测试要先点击 `打开记录`、`打开场景`、`查看续费` 或 `查看单台队列` 后再断言辅助 surface 内容。

7. 验证和深度审查
   - `cd web && npm run lint`
   - `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
   - `cd web && npm run build`
   - `npm run dev -- --host 127.0.0.1 --port 5178`
   - `scripts/visual_evidence.py browser-sanity --mock-api asset-workflows --route /asset-decisions --viewport 1440x1000 --viewport 390x900`
   - 如 Playwright 不可用，记录原因并使用可用浏览器/截图做人工审查。
   - 审查 diff、测试覆盖、页面视觉、移动端、深链和局部错误降级；发现问题立即修复并重跑相关验证。

8. 收尾
   - 如形成可复用规则，更新 `.trellis/spec/` 或 `docs/design/current/`。
   - 运行 Trellis check/finish 所需步骤。
   - 提交所有变更到同一分支。

## Validation Commands

```bash
cd web && npm run lint
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
cd web && npm run build
```

Browser sanity:

```bash
cd web && npm run dev -- --host 127.0.0.1 --port 5178
TMPDIR="$PWD/../.tmp/playwright" python3 ../scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --mock-api asset-workflows \
  --route /asset-decisions \
  --viewport 1440x1000 \
  --viewport 390x900
```

## Rollback Points

- 如果新 IA 测试无法稳定表达需求，先回到 PRD/design 修订，不改生产代码。
- 如果拆分导致工作流测试大面积失败，先恢复写路径，保留纯展示拆分。
- 如果发现确需 API 字段，停止前端继续猜测，补设计后再改后端和合同测试。
