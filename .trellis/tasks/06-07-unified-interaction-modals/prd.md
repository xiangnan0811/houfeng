# 统一交互弹窗修复

## Goal

将需要二次确认、编辑输入、危险写入前确认的临时页面内联交互统一迁移到现有 `Modal` 体系，减少页面主体中突然插入/残留的交互块，并保持错误、取消和提交中状态可控。

## Confirmed Facts

- 项目已有 `web/src/components/atoms/Modal.tsx` 与 `web/src/lib/useModalFocus.ts`，弹层应 portal 到 `document.body` 并复用焦点管理。
- 当前确认类体验存在页面内联承载和命令式 DOM 注入风险，尤其是 `ActionConfirmationCard` 调用、Probe 删除摘要注入、列表/详情页快速编辑、绑定冲突确认和资产决策内已有 Modal 的动作。
- 任务范围只覆盖当前以页面内联方式承载的确认、编辑、危险写入入口；筛选 Drawer、只读详情、页面主工作台表单、普通状态提示不纳入。
- 用户提供的修订方案是本任务的实施来源，已明确禁止嵌套 Modal。

## Requirements

- 扩展 `Modal`：
  - 支持 `dialogRole?: 'dialog' | 'alertdialog'`，确认类使用 `alertdialog`。
  - 使用 `useId()` 生成标题 id，避免硬编码 `modal-title`。
  - 保持现有 Escape、overlay、close 行为和全局关闭语义。
- 用 `ActionConfirmationModal` 替代内联 `ActionConfirmationCard`：
  - 复用当前“当前 -> 之后 / 会发生 / 不变 / 错误 / 按钮”展示内容。
  - 保留提交中禁用、失败留在弹窗内、取消清理 pending state。
  - 覆盖所有既有 `ActionConfirmationCard` 调用，包括列表页、详情页、批量操作和 `TargetRuntimeControls` 测试覆盖组件。
- 迁移确认/编辑入口：
  - Monitoring：行级暂停、批量命令输入、批量暂停、列表标签/分组快速编辑、详情暂停。
  - Targets：行级暂停/归档、批量暂停、列表标签/分组快速编辑、详情暂停/归档、ProbeItem 删除。
  - VPS：归档/恢复确认、解除监控实例关联确认。
  - Monitoring binding conflict：确认重绑、拒绝新指纹、重置绑定必须先进入确认弹窗或确认步骤再调用 API。
  - Asset Decisions：已有 Modal 内的单台决策编辑、成员移除、模板归档/启用使用同一 Modal 的内部步骤/确认状态，不新增嵌套 Modal。
- 移除 Probe 删除的命令式 DOM 注入：
  - 不再通过 `querySelector('.page-stack')` 追加摘要。
  - `formatConfigSummary(probeItem.config)` 作为声明式内容传给确认 Modal。
- 禁止嵌套弹窗：
  - 若动作发生在已有 Modal 内，切换当前 Modal 的内容步骤或关闭当前 Modal 后打开目标 Modal。
  - 不允许 `Modal` 内再渲染另一个 `Modal` 处理二次确认。

## Acceptance Criteria

- [ ] `Modal` 测试覆盖 `dialogRole="alertdialog"`、标题 id 唯一、焦点进入与关闭恢复。
- [ ] `ActionConfirmationModal` 测试覆盖 role/name、错误留在弹窗内、取消/确认回调和禁用状态。
- [ ] Monitoring / Targets 列表与详情点击暂停、归档、批量暂停后出现弹窗，不再出现页面内联确认块；失败时弹窗保留。
- [ ] 标签/分组编辑点击后打开弹窗；取消重置草稿；保存成功关闭并刷新或更新可见数据。
- [ ] Target Probe 删除弹窗内展示 config summary；确认失败不关闭。
- [ ] VPS 归档/恢复与解除关联的二次确认在弹窗或当前 Modal 步骤中完成。
- [ ] Binding conflict 三个动作首次点击只打开确认，不直接请求 API。
- [ ] Asset Decisions 既有 Modal 内成员移除、模板归档/启用通过内部确认步骤完成，并且不产生嵌套 dialog。
- [ ] 验证通过：`cd web && npm run lint`、`cd web && npm run test -- --run`、`cd web && npm run build`、`make verify-web`。

## Out Of Scope

- Settings 页面主配置表单。
- 筛选 Drawer。
- 历史 Drawer。
- 只读详情 Modal。
- VPS 取消/退役工作台。
- 后端 API 或业务 contract 变更。
- 引入新的 UI 库或状态管理库。
