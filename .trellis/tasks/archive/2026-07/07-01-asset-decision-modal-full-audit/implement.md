# 实施计划：资产决策弹窗全量收敛

## 步骤

1. 启动 Trellis 任务。
2. TDD red：
   - 在 `AssetDecisionsPage.test.tsx` 添加默认层密度和目录短元信息 helper。
   - 增加/扩展成本自动组、非成本自动组、自定义组合、模板、保存记录测试。
   - 先运行目标测试，确认新增断言失败。
3. Green：
   - 在 `AssetDecisionsPage.tsx` 增加默认层/目录文案裁剪 helper。
   - 收敛自动组、自定义组合、模板、保存记录默认 summary/footer/meta。
   - 收敛目录 meta，去掉内部 ID 和长说明串。
   - 必要时调整 CSS，确保 modal 内容不会在移动端横向溢出。
4. Refactor：
   - 去除重复裁剪逻辑。
   - 确保没有无用 import、无 `any`、无 debug 文案。
5. 验证：
   - `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
   - `cd web && npm run lint`
   - `cd web && npm run build`
   - `git diff --check`
   - 浏览器 sanity：`/asset-decisions`，桌面与 390px 移动端，至少打开自动成本组、自动非成本组、自定义组合、模板、保存记录。
6. Trellis finish：
   - 如发现可复用规范缺口，更新 `.trellis/spec/`。
   - 提交功能修改和任务归档/会话记录。
7. Delivery：
   - 推送当前 feature branch。
   - 创建 PR 到 `main`。
   - 监控 PR checks。
   - checks 通过后按仓库策略合并。
   - 监控 main CI、Release Please / release / publish-images，如本次变更触发发布，跟进到有明确成功或不适用证据。

## 重点风险

- 当前截图旧标题已不在源码默认层，可能存在 stale build 或遗漏入口；不能只靠字符串搜索判定完成。
- `summary` / `goal` / `note` 是数据驱动字段，必须在默认层裁剪。
- `record` 和 `template` 目录目前可能显示内部 ID 或较长执行摘要，需统一处理。
- 测试必须避免只断言旧标题，否则未来换一组长文案仍会回归。

## 回滚点

- 测试 helper 可独立回滚。
- 展示 helper 不影响数据层，可单独回滚。
- CSS 调整仅限 `asset-decision-*` 类名，避免影响其它页面。
