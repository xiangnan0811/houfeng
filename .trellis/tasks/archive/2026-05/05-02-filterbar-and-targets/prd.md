# Build shared FilterBar + apply to TargetsPage (child 1 of list-filter-completion)

> Child 1 of `.trellis/tasks/05-02-list-filter-completion`. 第一次抽象 filter 通用组件，并 apply 到 TargetsPage（from scratch，最干净起点）。

## Goal

(1) 在 `web/src/components/filters/` 抽 4 个 filter 通用组件；(2) 在 TargetsPage 应用，实现 §6.4 全部 6 项筛选。

## What I already know

### §6.4 6 项筛选清单（实读 rules-and-interaction.md:289-296）

1. 类型（`target_type`）— select：service / china_reference
2. 运行状态（`run_status`）— select：启用 / 维护中 / 暂停 / 已归档
3. 健康状态（`health_status`）— select：正常 / 关注 / 告警 / 严重
4. 标签（`labels`）— multi-select / chip-input
5. 执行节点标签（`execution_node_labels`）— multi-select / chip-input
6. 当前异常目标（boolean）— toggle "仅看异常"

### TargetsPage 现状

- `web/src/pages/TargetsPage.tsx` 740 行
- 当前**无任何 list filter UI**（仅 create form 内有 `<select>`）
- `listTargets()` 返回完整 target 列表（client-side filter 友好）
- 用 `useState` + `useEffect` 模式管理 fetch + state
- 引用 `lib/api.ts`、`lib/format.ts`、`lib/types.ts`

### 现有 atoms 风格

- `Button` / `Input` / `Badge` / `Card` / `Tabs` / `Toggle`
- 命名：`PascalCase` 组件 + `XxxProps` interface + 默认 export 名
- className 拼接：`['base', `base--${variant}`, className].filter(Boolean).join(' ')`
- 样式靠 atoms.css 全局类（不用 CSS-in-JS）

### Filter 视觉设计来源

- §6.3 节点页（同 §6.4 引用）描述 "已激活筛选用鎏金 chip + × 移除"——FilterChip 是关键
- v2-houfeng `design-language.md` 已有 `--accent` (晨晖金) token 可直接用

## Decision (ADR-lite) — 多个自决

### D1: client-side vs server-side filter

**Decision**: **client-side**。
- 项目早期 + 没有用户 + 数据量小（V1 单用户 fleet）
- 后端无需改动，listTargets() 已返回全列表
- 未来需要时改 server-side 不会破坏 FilterBar API（client-side filter 在 page 内处理，FilterBar 只管 UI 状态）

### D2: filter 状态存哪里

**Decision**: **URL query string**（用 `useSearchParams` from react-router-dom）。
- 可分享 / 刷新保持 / 浏览器后退恢复
- 实现成本低（react-router 已有）
- child 2/3 应用时复用相同 hook 模式
- 状态序列化用简单 join `,`（multi-value）/ 单值 / `1`（boolean）

### D3: 通用组件清单（MVP 4 个）

**Decision**: 4 个组件落 `web/src/components/filters/`：

1. `<FilterBar>` — 容器 + 已激活 chip 区 + 清空按钮
2. `<FilterSelect label, value, options, onChange>` — 下拉单选（类型 / 运行状态 / 健康状态用）
3. `<FilterMultiSelect label, value, options, onChange>` — 多选（标签 / 执行节点标签用，简化为 chip 选择 popup 而非 tag input）
4. `<FilterToggle label, checked, onChange>` — 布尔开关（仅看异常用）

**不做的**：
- FilterTagInput（自由文本 tag 输入）—— 标签和执行节点标签都从已有列表选，无需自由输入
- FilterDateRange / FilterTimeSegmented —— EventsPage 才需要，由 child 3 增补

### D4: 状态序列化约定

URL 参数命名：
- `type` (single)
- `run_status` (single)
- `health` (single)
- `labels` (comma-joined multi)
- `execution_labels` (comma-joined multi)
- `abnormal=1` (boolean)

清空 filter 删除对应 query param（不是设空字符串）。

### D5: 已激活筛选 chip 视觉

按 §6.3："鎏金 chip + × 移除"。
- FilterChip 用 `--accent` 边 + 半透明 `--accent` 底 + `×` 按钮
- 点 × 移除该 filter（更新 URL）
- "清空所有" 按钮 in FilterBar 右侧

## Open Questions

- 暂无（D1-D5 都自决；用户审视 final confirmation 时如有不同看法可提）

## Requirements

1. 新建 `web/src/components/filters/` 含 4 个 .tsx + barrel `index.ts`
2. 新建 `web/src/components/filters/filters.css` 含相关 class
3. 在 `web/src/styles/main.tsx` 或 main 入口处确认 filters.css 引入（如需）
4. 修改 `web/src/pages/TargetsPage.tsx`：在 list 上方加 FilterBar + 6 项 filter，client-side 应用 filter 到 `listTargets()` 返回的数组
5. 加 page 测试：`web/src/pages/TargetsPage.test.tsx` 增 1-2 个 filter 测试用例（如选 type=service 后列表只剩对应行）
6. 加 atoms 测试：4 个新组件至少各 1 个 unit test
7. 更新 `.trellis/spec/web/component-conventions.md` 在"组件分层"段提及 `filters/` 子目录（可选，本任务范围内做）
8. `make verify-web` 全绿（lint + test + build）

## Acceptance Criteria

- [ ] `web/src/components/filters/{FilterBar,FilterSelect,FilterMultiSelect,FilterToggle,index}.tsx + filters.css` 落地
- [ ] TargetsPage 6 项 filter UI 可见可用（手动验证：选 type=service → 列表只显示 service 类型）
- [ ] URL query string 同步（地址栏含 `?type=service&abnormal=1` 之类）
- [ ] 已激活筛选 chip + × 移除 + 清空所有都工作
- [ ] `cd web && npm run lint` 0 errors / 0 warnings
- [ ] `cd web && npm run test -- --run` 全绿（含新增测试）
- [ ] `cd web && npm run build` 成功
- [ ] `make verify-web` 全绿
- [ ] git diff 范围只在 web/src/components/filters/ + web/src/pages/TargetsPage.tsx + web/src/pages/TargetsPage.test.tsx + (optional) .trellis/spec/web/component-conventions.md

## Definition of Done

- 通用 FilterBar API 稳定（child 2/3 可直接复用，无需大改）
- TargetsPage filter 端到端可用（手动 verify）
- 测试覆盖 happy path + 1 filter 组合
- 不破坏现有 TargetsPage 业务（create form / runtime actions / detail link 全保留）

## Out of Scope

- NodesPage / EventsPage 应用（child 2 / child 3）
- 长 page 文件拆分（gap #11）
- 后端 API 加 filter 参数支持（client-side 已够）
- E2E 测试（Vitest 单元测试足够）
- TagInput 自由输入组件（D3 已 deferred）

## Final Confirmation

**Goal**: 抽 4 个 filter 组件 + TargetsPage 应用 §6.4 6 项筛选 + URL query string 状态。

**Approach**: 一个 trellis-implement sub-agent 一次完成；预估工作量 2-4h（含测试）。

**Implementation Plan**:
- PR1: sub-agent 一次性产出 filters/ 4 组件 + TargetsPage 改造 + 测试
- main agent commit 拆 2-3 个：(a) filters/ 新组件，(b) TargetsPage filter 应用，(c) trellis bookkeeping（如要分得更细可以再拆）
