---
date: 2026-04-30
status: active
parent: docs/design/v2-houfeng/design-language.md
---

# 候风 v2 · 组件视觉契约

每个组件 ≤ 50 行；只描述**视觉契约**（结构、tone、状态、关键 token），不重复 props 文档（看 `.tsx` 源码）。

---

## 一、Atoms — 既有升级

### Button
- 三档大小：`sm` (5×11px / 11px font) / `md` (8×16 / 13px) / `lg` (11×22 / 14px)
- 四个变体：`primary`（accent 渐变 + 边）/ `secondary`（surface + border）/ `ghost`（透明 + accent 文）/ `danger`（critical 边 + critical-soft 背景）
- hover：阴影叠 3px accent-soft 外环
- focus：`outline: 2px solid accent; outline-offset: 2px`
- disabled：`opacity 0.6` + `--text-disabled`
- 类名 API 保留：`.btn .btn--{variant} .btn--{size}`

### Badge
- 三个变体：`state`（pill / serif / state tracking）`info`（小圆角 / 11px sans）`count`（pill / mono / 18px min-width）
- 七个 tone：`normal | notice | alert | critical | maintenance | offline | neutral`
- 每个 tone 共享 `tone--{tone}` 类名（与 Card / probe-card 通用）
- `count` 变体内部数字必须 mono `tabular-nums`

### Card
- 四个 role：`default` / `state` / `accent` / `warning`，新增 `dim`（弱化退役/危险区背景）
- `cardRole='state'` 新增 `ribbonPlacement?: 'left' | 'top'`，默认 `left`（向后兼容）
  - left：左 2px 实心丝带（state 色）
  - top：顶 2px hairline 丝带
- padding：`var(--space-3)` ~ `var(--space-4)`（收紧）
- 类名：`.card .card--{role} .tone--{tone}`

### Input
- 字段 = `label + shell + (prefix?, input, suffix?) + (error|hint)?`
- `label`：11px / 0.15em tracking / muted（serif 微调可选）
- `input` focus：`--accent-soft` 背景 + 三层光晕（`outline 0` + `box-shadow 0 0 0 3px accent-soft`）
- `error` 状态：critical 边 + critical-soft 背景

### Toggle
- 尺寸 40×22px（从 36×20 抬大）
- thumb 16×16px，圆形，`bg` 色
- 切换过渡 220ms `--ease-calm`
- focus：accent 2px outline

### Tabs
- 两个变体：`underline` / `pill`
- underline：active 加粗 2px accent 下划线、文字 primary
- pill：3px 包裹 padding，active `accent-soft` 背景 + primary 文

---

## 二、Atoms — 新增

### Sparkline
- SVG `<polyline>` 实现，0 依赖
- 默认尺寸 64×16px，可覆盖
- props：`values: number[]`, `tone?: Tone`, `width?`, `height?`
- `tone` 不指定时用 `--text-secondary`；指定时用对应 state 色
- 末点加 1px 实心圆点（视觉锚点）
- 不画坐标轴 / 网格 / 标签

### TrendArrow
- 三个状态：`up` / `down` / `flat`，由 `delta` 符号决定
- `inverse?: boolean` 反转语义（"上升=坏"）：默认 up=绿、down=红；inverse 后 up=红、down=绿
- 视觉：箭头 + 11px mono delta 数字 + 可选单位
- flat 状态显示 `→` 灰色

### StatusGlyph
- 6 个固定形状（SVG），见 design-language.md §6.1
- 两档大小：`sm`（10px）/ `md`（14px）
- 颜色 = 对应 state token
- 用 SVG 内联（不用 Unicode 字符）

### Mono · MonoDigits / Hostname / Timestamp（同文件三导出）
- `<MonoDigits>{42.7}</MonoDigits>` — 包成 mono + tabular-nums
- `<Hostname truncate>{'nd_xxx...'}</Hostname>` — mono + 可选省略号；hover 显完整 ID
- `<Timestamp value={iso} mode="absolute|relative|both" />`
  - absolute：`2026/04/30 15:33:21`
  - relative：`12 分钟前`
  - both：默认显 relative，hover 显 absolute（title 属性）
- 全部加 `font-variant-numeric: tabular-nums`

### DataTable
- 语义化 `<table role="table">` `<thead>` `<tbody> <tr role="row">`
- 紧凑 36px / 标准 44px 行高（`density` prop）
- thead：`bg-sidebar` 底色 + eyebrow 风格列名
- tr：hover 显 `surface-elevated`、可选 `onRowClick`
- 不内置：排序 / 分页 / 虚拟滚动（YAGNI；< 100 行场景足够）
- 类名：`.data-table` `.data-table__row`

---

## 三、共享组件

### StatusBadge
- v2 重写为 `Badge` 的瘦包装
- 老 `tone='cyan|green|yellow|red|slate'` → 新 tone：`cyan→neutral` / `green→normal` / `yellow→notice` / `red→critical` / `slate→offline`
- props 名不变（`label`, `tone`），调用方零改动

### DetailSection
- 结构：`detail-section__header (eyebrow + title + aside) → detail-section__body`
- 新增可选 `ribbon?: Tone` prop — 顶部 1px hairline state 色
- header padding 收紧到 `var(--space-3) var(--space-5)`
- aside 用 mono 字体（一般是时间戳）

### IncidentList
- **不做时间线**（活跃异常按 severity 排序，不按时间消费）
- 每行：`[StatusGlyph · 异常类型 (Badge state) · 对象引用 (Hostname mono) · 摘要 截断 · started_at (Timestamp relative) · last_evaluated_at (Timestamp mono)]`
- 行高 36px、hover surface-elevated
- 外层保留 `.probe-list`、行容器保留 `.probe-card` 类（兼容测试）

### EventList
- 改为纵向时间线
- 左 timeline rail（1px border-left + 节点位置上 StatusGlyph 缩小版）
- 右内容：[事件类型 Badge · 摘要 · Timestamp]
- 按日期 sticky header 分组：`今天` / `昨天` / `YYYY/MM/DD`
- 外层保留 `.probe-list` / `.probe-card` 类

### ActionConfirmationCard
- 视觉：状态迁移卡
- 顶部 ribbon（1px hairline state 色，`--accent-2` 默认）
- 主体三栏（grid 1fr auto 1fr）：
  - 左：StatusGlyph + `current` 文案
  - 中：`→` 箭头 + 操作名（小字 muted）
  - 右：StatusGlyph + `result` 文案
- 三栏下方两行 callout：
  - `✓ 会发生：{impact}` → `accent-soft` 背景 + accent-border 左条
  - `◯ 不变：{unchanged}` → `surface` 背景
- 底部确认/取消按钮右对齐
- props 完全保留（9 个 prop 一字未改）

---

## 四、壳层

### Sidebar
- 宽 220px、`bg-sidebar` 色
- 品牌区：`候风` serif 22px + `HOUFENG FLEET CONTROL PLANE` sans 9px / 0.25em tracking + 一道 hairline divider
- nav：8×10px padding / 13px / 6px 圆角
  - 默认：`text-secondary`
  - hover：`surface` 背景
  - active：`accent-soft` + `accent-border` + `text-primary` + 左侧 2px accent 实心条
  - count Badge（节点/目标 nav 的异常计数）保留，但**强制 `tone='neutral'`** — 不用 critical/alert 色。理由：nav 项本身不承载状态语义（避免红点引发"这个 tab 在告警"的误读），但 count 数字本身仍是有用信息（"节点 (3)" 比无标记更直观）。激活状态的视觉张力由 accent 软背景承担，不靠 nav 项内的 Badge 颜色。
- spacer flex
- 底部：SyncStatus → UserChip（间距 `--space-3`）

### SyncStatus
- 三档 tone：`ok` / `degraded` / `down` 各对应不同色
- 状态行：状态点 + 标签文案（中文）
- ok 状态点：呼吸动画 1.6s
- 状态行下方：7-bar Sparkline（最近 7 个心跳间隔）
- 数据不可用降级：只显示状态行不留空白
- meta 行：mono 9px `v1.0 · sync HH:mm:ss`

### AppShell
- grid `220px 1fr`
- main padding：`24px 28px`（从 24×32 收紧）
- main `overflow-x: hidden`
- aurora：弱版本叠在 main 上（透明度减半）

### UserChip
- 触发器：26px 圆形头像（accent → accent-darker 渐变）+ 名字 sans 12px + role serif 10px / 0.1em tracking + caret
- 菜单弹出：bottom-aligned、`shadow-overlay`、`surface-elevated` 背景
- danger 项（如登出）：critical-tinted 文案

---

## 五、页面模板

### DashboardPage
1. Hero panel：`当前风险总览` eyebrow + `首页 / Dashboard` h1 + 描述
2. **Stat strip**（5 列等宽，`role="group" aria-label="..."`）：每列 `[label · MonoDigits · TrendArrow]`
3. DetailSection `异常节点概览`：summary-grid (3 KPI) + 紧凑节点行（StatusGlyph + Hostname + 位置 + 当前问题 + Timestamp）
4. DetailSection `异常目标概览`：同样模式
5. DetailSection `最近事件`：EventList timeline

### NodesPage
1. Section heading + 「新建节点」按钮
2. （可选）创建节点表单（page-panel）
3. 视图切换：segmented control 「全部节点 N」/「绑定异常 M」
4. **DataTable**（density compact）：列 `[StatusGlyph, 节点(Hostname + 名字), 位置, 标签, 当前主问题, 心跳(Timestamp), 操作]`
5. 行 hover：操作列显示「快速编辑标签」「进入维护」「暂停监控」三个 ghost 按钮
6. 行点击：导航到节点详情

### NodeDetailPage
1. Hero：节点名 (display_name) + 状态 Badge + 4 个 hero meta card（标签 / 最近心跳 / 最近同步 / 当前主问题）
2. （可选）绑定冲突 DetailSection（critical 风格 + 三个动作按钮）
3. Summary grid：3 卡（健康状态 / 活跃异常数 / 当前主问题）
4. DetailSection `标签与备注`（编辑/查看切换）
5. DetailSection `运行控制` + ActionConfirmationCard
6. DetailSection `生命周期`（退役按钮 + ActionConfirmationCard）
7. DetailSection `当前主机指标`：4 metric-card 各含 [label · MonoDigits · Sparkline (12-24h)]
8. DetailSection `近期趋势`：4 卡 + 各自 Sparkline
9. DetailSection `当前异常`：IncidentList
10. DetailSection `事件`：EventList timeline

### EventsPage
1. Hero panel
2. DetailSection `筛选条件`：横向 chip 行（对象类型 / 严重度 / 事件类型 / 数量）+ 「高级筛选」展开抽屉（时间范围 / 标签 / 三个 checkbox）+ 应用/重置按钮
3. DetailSection `事件流`：EventList（按日期 sticky 分组）

### SettingsPage
1. Hero panel
2. DetailSection `主题`（ribbon notice）：preset Tabs + mode Tabs
3. DetailSection `Telegram`（ribbon accent-2）：token 输入 + chat-id 输入 + 状态卡（mono token-masked）+ runtime checkbox
4. DetailSection `频率档位`（ribbon normal）：4 个 segmented（节点 host / TCP / HTTP / TLS）
5. DetailSection `全局默认`（ribbon notice）：6 字段表单
6. DetailSection `覆盖规则`（ribbon notice）：3 个 textarea（mono 字体）
7. DetailSection `保留策略`（ribbon notice）：4 个 retention 输入
8. 底部统一保存按钮 + 错误/成功就地展示

### TargetsPage / TargetDetailPage
- 镜像 NodesPage / NodeDetailPage 的 DataTable + 详情结构
- 列略不同：`[StatusGlyph, 名字, 类型, host, 标签, 状态, 操作]`

### NodeOnboardingPage
- 安全感重：cardRole='warning' ribbon top 用 critical
- token 一次性显示：mono + 复制按钮 + 倒计时 + 「已保存，关闭」按钮
- 关闭后无法再获取：用 dim card + critical 文案再次警示

### LoginPage
- 全屏居中
- 装饰：印章感 SVG（"候"字）+ aurora 增强
- 卡片：候风 serif 大字 + HOUFENG sans 小字 + 「察变 · 守望」motto
- 表单：username / password / 登录 Button(primary lg)
- 错误：就地 alert（`role="alert"`）

---

## 六、文档导航

- 上层：[design-language.md](./design-language.md)
- 旧版：[v1-baseline/ui-ux-spec.md](../v1-baseline/ui-ux-spec.md)（仅参考）
