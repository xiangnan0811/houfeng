# 执行计划：资产决策页面设计重置与弹窗组件化

## 阶段顺序

P0 → P1 → P2 → P3 → P4 → P5

P0 必须先做（spec 不清理，后续在旧禁令雷区打转）。P1 是 P2 的前置（弹窗不提取，IA 重排无法进行）。P3/P4 在 P2 后可并行。P5 贯穿但最终在 P4 后收尾。

---

## P0: spec 清理与正向设计契约

### 步骤

1. 读取 `.trellis/spec/web/component-conventions.md` 全文，定位 8+ 条资产决策补丁规则的行区间。
2. 将这些规则合并为 1 条"决策类页面信息层级契约"，保留业务安全边界规则（迁移意向 / 危险联动 / 低频报告独立页）。
3. 更新 `docs/design/current/component-patterns.md` 补充决策类页面 IA 规范段落。
4. 提交：`docs(spec): 合并资产决策补丁为通用设计契约`

### 验证

- `grep -c "资产决策" .trellis/spec/web/component-conventions.md` 显著下降（从 8+ 降到 ≤2）。
- 通用契约段落存在且为正向表述。

---

## P1: 弹窗组件化提取

### 步骤

1. 创建 `web/src/pages/asset-decisions/modals/` 目录。
2. 逐个提取 5 个弹窗：
   - `GroupDetailModal.tsx`（原 AssetDecisionsPage.tsx 行 2796-2957）
   - `ManualGroupDetailModal.tsx`（原行 2959-3290，含嵌套确认对话框）
   - `TemplateDetailModal.tsx`（原行 3292-3464，含嵌套确认）
   - `RecordDetailModal.tsx`（原行 3485-3582）
   - `RenewalDecisionModal.tsx`（原行 3466-3483，复用 AssetDecisionWorkPanel）
3. 每个弹窗组件：
   - 自管数据拉取（调用 lib/api.ts 拉详情）
   - 自管内部 Tab / 面板状态
   - 接收 `open` / `onClose` / `id` / 跨弹窗导航回调
4. 修改 `AssetDecisionsPage.tsx`：删除内联弹窗 JSX，改为渲染 5 个组件，页面只持有 `openGroupID` 等 state。
5. 更新测试：弹窗行为不变，测试断言不变。
6. 提交：`refactor(asset-decisions): 提取 5 个弹窗为独立组件`

### 验证

- `wc -l web/src/pages/AssetDecisionsPage.tsx` ≤ 1800。
- 每个弹窗文件 ≤ 200 行。
- `npm --prefix web run test -- --run AssetDecisionsPage` 通过。
- `npx tsc --noEmit` 通过。

---

## P2: 页面 IA 三段式重排

### 步骤

1. 重写 `AssetDecisionsPage.tsx` 的顶层 return 为三段式：判断板 → 决策组扫描 → 辅助入口工具条。
2. 重写 `PortfolioWorkbench` → 简化为判断板 + 扫描列表（删除 4 项事实卡，主判断板只留 1 判断 + 1 动作）。
3. 重写 `SecondaryWorkbenches` → 改为 `AuxEntryBar`（收起工具条）+ 展开式单一面板。
4. 删除页面副标题、深链提示 inline-alert。
5. 移动端辅助入口 2×2 网格。
6. 提交：`refactor(asset-decisions): 重排页面 IA 为三段式`

### 验证

- 桌面 1440 首屏只看到主判断 + 扫描列表。
- 移动 390 首屏能看到主判断 + 扫描入口。
- 辅助入口默认收起。
- `npm --prefix web run test -- --run AssetDecisionsPage` 通过。

---

## P3: 文案精简

### 步骤

1. 在 AssetDecisionsPage 及弹窗组件中删除解释性段落。
2. 删除所有英文 eyebrow，改中文短标签或去除。
3. 替换弹窗内嵌确认对话框为 `ActionConfirmationModal`。
4. 同步精简 `VPSCancellationWorkbench.tsx` 的 8 处说明文字（移入 tooltip）。
5. 提交：`refactor(asset-decisions): 精简文案，统一确认对话框`

### 验证

- `grep -rE "PORTFOLIO|RENEWAL|WORKBENCH|SCENARIO|DECISION|EVIDENCE" web/src/pages/asset-decisions/ web/src/pages/AssetDecisionsPage.tsx` 无用户可见 eyebrow（代码标识符除外）。
- 无 `role="alertdialog"` 的内联 section。
- `npm --prefix web run test -- --run AssetDecisionsPage` 通过。

---

## P4: CSS 类名收敛

### 步骤

1. 在 `web/src/styles/pages.css` 中：
   - 删除 `.asset-decision-primary/secondary/tertiary-*` 碎片类。
   - 新增 `.hero-panel--decision` / `.page-panel--scan` / `.page-panel--aux` modifier。
2. 全局替换组件 JSX 中的旧类名。
3. 提交：`refactor(asset-decisions): 收敛 CSS 类名为三层语义`

### 验证

- `grep -E "asset-decision-(primary|secondary|tertiary)" web/src/` 无结果。
- `npm --prefix web run build` 通过（无未定义类引用报错）。
- 浏览器 sanity 视觉无回归。

---

## P5: 测试重写

### 步骤

1. 删除 AssetDecisionsPage.test.tsx 中以"marker 不出现""预览 ≤3"为主的反向断言。
2. 新增正向用户任务断言（进入决策组 / 创建自定义组合 / 保存记录 / 单台续费）。
3. 新增行数守护测试（主文件 ≤800，弹窗 ≤200）。
4. 提交：`test(asset-decisions): 转向用户任务正向断言，新增行数守护`

### 验证

- `npm --prefix web run test -- --run` 全套通过。
- `npm --prefix web run lint` 通过。
- `npm --prefix web run build` 通过。

---

## 最终验证

- 浏览器 sanity：桌面 1440 + 移动 390，默认态 / 稳定态 / 典型弹窗，无横向溢出。
- `wc -l web/src/pages/AssetDecisionsPage.tsx` ≤ 800。
- `grep -c "资产决策" .trellis/spec/web/component-conventions.md` ≤ 2。
- git diff --check 通过。

## 回滚点

每阶段独立提交，可独立 revert。P0/P1 互不依赖。P2 依赖 P1。P3/P4 依赖 P2。P5 贯穿。
