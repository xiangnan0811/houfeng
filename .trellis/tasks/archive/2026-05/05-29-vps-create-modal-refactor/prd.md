---
title: VPS 创建流程优化：Drawer → Modal + 表单重组
status: approved
---

# VPS 创建流程优化

## 目标

将 VPS 创建从右侧 Drawer 改为居中 Modal，重组表单为分层结构，优化首次使用体验。

## 验收标准

1. 按钮文案从"导入"改为"添加 VPS"
2. 使用 Modal（非 Drawer），点击外部不关闭
3. 表单分三层：核心信息 → 网络入口 → 补充信息（折叠）
4. 移除"服务商名称快照"字段
5. 支持内联创建服务商
6. 必填字段有 * 标记
7. 相关字段同行展示（lifecycle+usage, ssh host+port+user）
8. 构建通过，测试通过

## 实际交付范围扩展（check 阶段衍生）

实现验证时发现 Modal 下拉框与候风设计语言不一致，溯源后扩展了以下三项（均已 build/lint/471 测试通过）：

9. **新增 `Select` 原子**（`web/src/components/atoms/Select.tsx`），对标 `Input` 原子（forwardRef + label/error/hint/required + `input-field` 包裹），统一取代散落的 `<select className="input">`。
10. **全局 select 箭头 token 化**：原生 OS 箭头会在暗色主题下突兀且不随主题。改为三主题各定义 `--select-caret`（houfeng-dark / houfeng-light / classic-dark，对应各自 `--text-muted`），所有 form select 走 `appearance:none + background-image:var(--select-caret)`。一次 18 文件审计确认全项目 select 全覆盖，清除了硬编码 `#7B7F88` 与死规则 `select.fi`。详见 `styling-guidelines.md` 的「Select 下拉箭头」约定。
11. **`TargetProbeForm` 接入设计系统**：它是全 app 唯一完全未接入设计系统的裸表单（裸 `<select>`+裸 `<input>`，经 Drawer portal 逃逸 `.page-stack`）。复用 `.target-create-drawer__form` 规则组接入，保留 `<label>` 文本内嵌以维持 `getByLabelText` 关联。
