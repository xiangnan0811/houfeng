# 技术设计：资产决策弹窗信息架构收敛

## 范围与边界

主要修改 `web/src/pages/AssetDecisionsPage.tsx`、同文件测试和必要 CSS。前端只重排和裁剪展示，不改变 API 请求、响应类型、保存记录 payload、VPS 单台处理 payload 或后端语义。

适用弹窗：

- `selectedGroupID` 自动组 modal。
- `selectedManualGroupID` 自定义组合 modal。
- `selectedTemplateID` 场景模板 modal。
- `selectedRecordID` 保存记录 modal。

## 信息架构

### 默认层：Decision Cover

默认层统一只承担“现在该怎么看”：

- `aria-label` 标明当前判断。
- 单句 summary，强制压缩到短句。
- 最多 2 个 compact chips。
- 一个主动作和一个详情入口。
- 可选短 context，必须由短事实组成，不得使用内部 ID 或长说明。

默认层不得渲染：

- `renderDetailPanelNav`。
- `renderDetailDirectory`。
- `DataTable` / `.asset-table-scroll`。
- `renderMemberDecisionRows`。
- 保存记录表单、执行面板、来源连续性面板、VPS 处理面板。

### 详情目录：Directory

点击“详情/查看详情”只进入目录。目录项允许：

- label。
- count badge。
- status 或非常短的 meta。

目录项禁止：

- `来自 <id>` 这类内部 ID。
- `成员 2 / 可推进 ... / 人工跟进...` 这类完整说明串。
- provider/product/cost/facts、成员名、表格说明、来源回读详情。

### 二级面板：Single Task Panel

保持现有面板拆分，但进一步去掉跨任务混杂内容：

- 成员面板：紧凑行，不展示 provider、location、product、cost、facts 串。动作只保留处理/VPS/移除等必要按钮。
- 保存面板：基础字段 + 成员理由折叠编辑。
- 执行面板：状态推进 + 执行 board。
- 来源面板：来源标签、当前状态、复核入口。
- 底稿面板：唯一允许渲染完整 DataTable 的位置。

## 文案裁剪策略

新增小型 helper，用于默认层和目录：

- `compactDecisionText(value, fallback, maxLength)`：规范空白，裁剪长句到上限，避免 API 长摘要撑开 modal。
- `compactDirectoryMeta(value, maxLength)`：只保留短状态；超长或包含内部 ID / URL-like 片段时降级为短标签。

这些 helper 只做展示层裁剪，不改原始数据。底稿和显式详情仍可访问完整字段。

## 测试设计

在 `AssetDecisionsPage.test.tsx` 增加泛化检查：

- `expectDialogCoverIsCompact(dialog, options)`：
  - 默认层可见文本长度上限。
  - 禁止 `.asset-table-scroll`、详情面板 nav、目录、成员行、member card、DataTable 区域。
  - 禁止旧 marker、英文报告 eyebrow、保存/执行/来源/底稿结构。
- `expectDirectoryIsCompact(dialog, ariaLabel)`：
  - 目录按钮文字/meta 长度上限。
  - 禁止完整说明句、内部 ID、成员名、provider/product/cost/facts 串。

覆盖路径：

- 成本自动组默认层和目录。
- 非成本自动组默认层和目录。
- 自定义组合默认层和目录。
- 模板默认层和目录。
- 保存记录默认层和目录。

## 兼容与回滚

- 不改 API、路由、保存/更新 payload，回滚可限制在前端页面和测试。
- 如浏览器 sanity 发现移动端溢出，优先调整现有 BEM 样式和 modal width/max-width，不引入新布局系统。
- 如某个 API 文案被裁剪影响理解，用户仍可通过详情/底稿进入完整信息，默认层保持低密度。
