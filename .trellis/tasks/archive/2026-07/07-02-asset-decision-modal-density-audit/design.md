# Asset Decision Modal Density Follow-up Design

## Problem

上轮修改降低了默认封面密度，但用户仍在实际使用中看到组详情弹窗像长报告：大量说明、证据、成员内容、操作和底稿堆在一个滚动窗口里。当前测试主要验证旧标题/旧 marker 不出现，无法证明真实弹窗已经低密度。

## Root Cause Hypothesis

1. 验证维度太窄：测试只查几个旧文案，缺少通用密度上限、首屏结构约束和跨弹窗路径覆盖。
2. 二级面板仍偏“展示完整信息”：成员、保存、执行、模板创建等面板仍可能包含标题、摘要、成员行、多个 badge、多个按钮和表单，视觉承载可能超出用户可快速判断的范围。
3. 浏览器 sanity 只检查 overflow，不检查弹窗内容密度、面板类型隔离和首屏噪声。

## Target UX

详情弹窗统一遵守四层结构：

1. **Cover**：一句判断 + 少量状态 badge + 一个主动作 + 详情入口。
2. **Directory**：任务入口列表，只显示 label/count/短 meta。
3. **Task Panel**：单任务面板，每屏只服务一个动作；成员行紧凑，表单按需展开。
4. **Raw / Advanced**：完整字段、宽表、低频解释只在显式底稿入口。

核心原则：

- 用户打开一个组时，先知道“现在该做什么”，而不是先读报告。
- 用户点击详情时，先选择任务，而不是立即进入报告。
- 用户进入任务面板时，只处理当前任务，不被其它任务的证据和表单干扰。

## Implementation Shape

修改范围保持 frontend-only：

- `web/src/pages/AssetDecisionsPage.tsx`
  - 增加通用密度/任务面板 helper，而不是为单个组写特例。
  - 收紧 `renderMemberDecisionRows`、保存表单成员复核、记录执行面板和模板/自定义组合普通态文案。
  - 如运行态证明当前 JSX 仍有过载路径，改为更短的 task panel header、紧凑行和按需展开。
- `web/src/pages/AssetDecisionsPage.test.tsx`
  - 新增通用密度断言：文本长度、段落/表单/按钮数量、禁止跨任务元素同屏出现。
  - 新增成本/预算组、非成本组、自定义组合、模板、保存记录来源复核路径覆盖。
  - 保留写入 payload 断言。
- `web/src/styles/pages.css`
  - 必要时调整弹窗、任务面板、紧凑行、移动端约束。
- `.trellis/spec/web/component-conventions.md`
  - 如本轮形成新的“运行态密度测试”规则则更新。

## Non-goals

- 不新增后端字段。
- 不改变 API request/response contract。
- 不新建独立 route。
- 不引入 Playwright/Cypress 作为项目依赖。
- 不移除底稿和写入能力，只移动到明确入口或按需展开。

## Verification

1. RED：先加失败测试证明当前测试覆盖不足或当前 UI 仍过载。
2. GREEN：实现通用修复。
3. Unit/page tests：
   - `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
   - `cd web && npm run test -- --run`
4. Static checks：
   - `cd web && npm run lint`
   - `cd web && npm run build`
   - `git diff --check`
5. Browser sanity：
   - dev server + `mock-api asset-workflows`
   - desktop `1440x1000` and mobile `390x900`
   - inspect automatic cost group, automatic non-cost group, manual group, template, saved record, source review.
   - record document/body overflow and modal text/block metrics.
