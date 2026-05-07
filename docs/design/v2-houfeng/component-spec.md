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

### MetricChart
- 纯 SVG 完整时序图（360×140 default），与 `<Sparkline>`（240×60 配角）并存不互替
- X 轴时间格式化（`HH:mm` / `dd HH:mm`）+ Y 轴值标尺 4 刻度 + 网格虚线
- 阈值线（`thresholds: {value, tone, label}[]`）— 水平虚线 + tone 颜色 + 右侧标签
- 维护窗口阴影（`maintenanceWindows: {startedAt, endedAt}[]`）— 竖向半透明带 + 三角 marker
- 十字线 hover tooltip（垂直 hairline + 值 + 时间浮窗）
- 边界态：0 sample → "暂无观测数据" / 1 sample → "样本不足"
- 不引图表库（纯 SVG 自研，守 §12 硬约束）
- 类名：`.metric-chart` `.metric-chart--empty` `.metric-chart-shell`

### Drawer
- 右侧/左侧滑入面板（`side: 'right'|'left'`，default `'right'`，width `min(440px, 40vw)`）
- 当前实现：fixed-position inline render + ESC 关闭 + overlay 点击关闭 + `aria-modal="true"`；React portal、初始焦点、Tab containment、触发器焦点恢复仍是可访问性 hardening follow-up，未在本轮视为已完成
- header：title + × 关闭按钮 / body：scroll-y auto
- 类名：`.drawer-overlay` `.drawer` `.drawer--right/--left` `.drawer--open`

### Stepper
- 水平 4 步进度条（`steps: {label, state: 'pending'|'current'|'done'|'error'}[]`）
- 圆点 SVG + 连接线 + label / 状态色（pending→offline / current→accent / done→normal / error→critical）
- 不内置 vertical / clickable（YAGNI；未来扩 props）
- 类名：`.stepper` `.stepper__step--{state}`

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
1. Fleet State hero：动态状态结论作为 h1（`需要处理严重异常` / `存在活跃异常` / `系统处于维护观察中` / `系统运行正常` / `开始接入第一台服务器`），eyebrow `Fleet State`；说明文案必须基于现有 `/api/dashboard` 事实，不声明后端未提供的全量库存或 shell health。
   - 右侧 facts：`API 已加载 /api/dashboard`、`快照时间 接口暂未提供`、库存总数、当前异常/严重/维护队列计数；这是数据可信度提示，不等同 AppShell SyncStatus。
   - CTA：主按钮随状态切换（异常态 → `/events`，正常态 → `/nodes`，首次接入 → `/nodes`），次按钮固定 `查看事件流` / `进入设置`。
2. **Global KPI strip**（5 列等宽 Link 卡）：节点、目标、严重、维护、`24h 变化`。每列含 `[label · MonoDigits · 描述 · 可选 Sparkline]`，点击进入节点/目标/事件页；状态色仅通过底部 2px rail 提示，不把 nav/count 语义染成告警。
3. DetailSection `当前需要处理`，eyebrow `处理队列`：统一处理队列，合并异常节点与异常目标，按 severity + active incident count 排序。
   - DataTable 列：`[StatusGlyph, 对象(Hostname + 名字 + freshness Timestamp), 类型(Badge + group/location/type), 状态(Badge state), 当前主问题(MonoDigits count + 摘要), 操作]`。
   - 行点击进入节点/目标详情；操作列 link `stopPropagation()`；section aside 提供 `查看全部异常节点` / `查看全部异常目标` / `查看事件流`。
   - 空态：`当前没有活跃异常`，说明处理队列为空并保留最近事件回溯入口。
4. DetailSection `系统入口`：四个高密度 Link 入口：节点 / 目标 / 事件 / 设置。每个入口只展示现有 `/api/dashboard` 可支撑的状态数字；通知配置、真实 group 分布、shell health 等未接入 contract 前不得展示。
5. 首次接入时用 DetailSection `首次接入工作台` 替代处理队列：4 步卡片 `[创建节点, 接入 agent, 创建目标, 添加 ProbeItem]`，每步给出具体入口。
6. DetailSection `最近事件`：复用 EventList timeline，aside 固定 `查看全部事件`。复杂历史筛选仍归 EventsPage，Dashboard 不复制 EventsPage。
7. 已移除旧 `按 Group 分布` 区：当前 dashboard API 只提供异常对象摘要，不能伪装成全量 group distribution；如需恢复，必须先扩展 `/api/dashboard` 的真实 group summary contract。

### NodesPage
1. Section heading + 「新建节点」按钮
2. （可选）创建节点表单（page-panel）
3. 视图切换：segmented control 「全部节点 N」/「绑定异常 M」
4. **DataTable**（density compact）：列 `[StatusGlyph, 节点(Hostname + 名字 + 心跳/同步 mono), 位置, 标签, 当前主问题, 近 24h 趋势(sparkline strip), 操作 hover]`
   - 节点身份列三行：第 1 行 `<Hostname truncate>` node_id（mono 小字）、第 2 行 display_name（sans 粗体 link）、第 3 行 `心跳 X 分钟前 · 同步 Y 分钟前`（mono 10px `--text-muted`）
   - 趋势列（~220px）：CPU / Mem / Disk 三指标 mini sparkline strip，每项含上方 mono 当前值（9px）+ 下方 `<Sparkline>` 64×14，tone 按阈值择色（CPU 80/95、Mem 85/95、Disk 80/95）
   - Sparklines 延迟加载（不阻塞列表首屏），缺失 / 加载中 / 失败均显示 "—"
   - 原"心跳·同步"独立列已删除，信息合并入节点身份列第三行
5. 行 hover：操作列显示「快速编辑标签」「进入维护」「暂停监控」三个 ghost 按钮
6. 行点击：导航到节点详情

### NodeDetailPage（watchtower）

ops-first 视图，把"当前主问题 + 8 张时序大图"前置作为视觉主体，砍装饰、折叠次要、历史进抽屉。

1. ① 身份条（2 行 + 右上 sticky 操作）：
   - 行 1：display_name h1 + 4 状态 Badge（lifecycle / monitoring / binding / current_health）+ "数据新鲜度" mono 行（心跳 `<Timestamp mode="relative">` + 运行 `formatUptime(uptime_seconds)`）
   - 行 2：mono 元数据条 — `<Hostname truncate>` (node_id) · 位置 (region · city · provider) · 标签 (`labels.join(' · ')`) · agent_version (`<MonoDigits>`)
   - 右上：「查看历史」ghost button + "…" 操作 popover（`<details><summary>` 原生，按 monitoring_status 条件性显示 进入/退出维护、暂停/恢复 按钮，复用既有 `nodeRuntimeActions`）
2. ② 危险区前置（条件性 — 仅 `current_active_incident_count > 0` 才渲染，**不有异常时整块不渲染**）：
   - `<Card cardRole="warning" className="watchtower-danger">` 包；eyebrow "当前主问题" + 大字 h2 摘要 (`current_primary_issue_summary`) + meta 行（活跃异常 `<MonoDigits>` 个 · 健康状态 `<StatusBadge>`） + ghost button「查看完整时间线 →」（触发抽屉，默认事件 tab）
3. （最高优先 / 条件性）绑定冲突 DetailSection（仅 `binding_status === '指纹变更待确认'`，在 ② 之上）：保留既有"高优先级：绑定冲突待处理"卡片 + 当前/待确认指纹 + 三个动作按钮（确认重绑 / 拒绝 / 重置）
4. ③ 主视图栅格 `.watchtower-metrics`：8 张 `.watchtower-metric-card` 4×2 栅格（≤ 1280px 自动 2×4），每张含
   - 卡头：`<h3>` eyebrow 标签 + `<MonoDigits>` 当前值大字
   - `<MetricChart width={360} height={140}>` 含 X/Y 轴 + 阈值线（CPU/Mem/Disk/Inode 80/95；IOWait 20/50；Load5 4.0/8.0；Net 不画阈值）+ 维护窗口阴影（按 sample.maintenance_context 派生）+ 十字线 hover tooltip
   - 次指标 dl（CPU 卡含 steal%；Memory 卡含 Swap used%；Disk 卡含 disk_busy% / read+write；其他单值）
5. ④ 次要信息默认 collapsed `<details className="watchtower-secondary">` 折叠：
   - 「标签与备注」（NodeLabelsAndNote 子组件，编辑/查看切换 + 乐观锁）
   - 「生命周期」（退役按钮 + 二次确认；已退役时显示「恢复到观察中」）
   - 「接入凭证状态」（当前 binding_status `<StatusBadge>` + `<Link>` 到 `/nodes/:id/onboarding` 接入工作台）
6. 页面底部 mono 小字：数据快照时间 (`<Timestamp value={now} mode="absolute">`)，刷新页面获取最新（不做实时 polling，保留"页面打开 = 静态快照"模型）
7. 历史抽屉（右侧 `min(440px, 40vw)` `<Drawer>`）：标题 `${display_name} · 历史`；抽屉内 `<Tabs variant="pill">` 切换 [事件时间线] / [历史异常]；
   - 事件 tab：复用 `<EventList>`，数据来自 page 已加载的 `state.events`（节点详情主入口已拉取）
   - 历史异常 tab：调 `listHistoricalIncidents('node', nodeId)`（`/api/incidents?...&include_resolved=true`）懒加载；首次切换时触发 fetch；通过 ref 防止 setState 引起 effect 重入；切换节点 (nodeId 变化) 时清缓存以免显示前一节点的数据
   - 抽屉支持 Esc 关闭 / overlay 点击关闭 / × 按钮关闭；当 `open=false` 时不渲染 children，避免 DOM 中事件文案重复

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

### TargetsPage
1. Section heading + 「新建目标」按钮（右对齐，primary）
2. （可选）创建目标表单（page-panel，可折叠）
3. 筛选栏：6 项（type / run_status / health / labels / execution_node_labels / abnormal toggle）
4. **DataTable**（density compact）：列 `[StatusGlyph, 目标(名字 + Hostname target_id), 类型, Host(Hostname host[:base_port]), 标签(截断+overflow+inline 编辑), 状态(StatusBadge run_status + health + 执行节点标签), 最近成功/失败(Timestamp relative), 当前主问题(MonoDigits incident_count + 摘要), 操作]`
5. 行 hover：操作列显示「快速编辑标签 / 进入维护 / 暂停 / 归档 / 恢复」等条件性 ghost 按钮（hover-only opacity 模式）
6. 行点击：导航到目标详情；操作列内部 `event.stopPropagation()` 防误触发

### TargetDetailPage
1. Hero：目标名 (display_name) + 4 个 hero meta card（标签 / 执行节点标签 / 最近成功 Timestamp / 最近失败 Timestamp，全部 mono 包装）
2. Summary grid：3 KPI 卡（健康状态 / ProbeItem 数量 MonoDigits / 当前主问题）
3. DetailSection `标签与备注`（编辑/查看切换）
4. DetailSection `运行控制` + ActionConfirmationCard（pause/archive 二次确认）
5. DetailSection `近期延迟趋势`：每 enabled ProbeItem 一张 metric-card 含 [卡头 (kindLabel `<KIND> · <config 摘要>` · MonoDigits 当前延迟) · Sparkline 240×60 interactive (hover tooltip 显时间 + formatLatency 值) · 次指标 dl (平均 / 最大 / 样本数 / 覆盖节点)]。section aside 显采样元信息（`24h N 样本 · 最早 ... · 最新 ... · backfill M`）。维护态 → 整张 section 加 `ribbon='maintenance'`
6. DetailSection `ProbeItem 列表`：每 ProbeItem 一张卡（保留卡片栈，适合 TCP/HTTP/TLS 多样配置）
   - 卡 header：`<KIND>` 大标题 + config 摘要 + Badge 行（启用 / 频率档位）
   - 卡操作行：编辑 / 启用停用 / 删除（删除走 ActionConfirmationCard 二次确认）
   - 卡 meta dl：超时（MonoDigits 秒数）+ 最近观测（Timestamp mode="both"）
   - 卡内嵌 observation `<DataTable density="compact">` 6 列：`[StatusGlyph result_kind, Hostname node_id, Timestamp 观测时间(relative), MonoDigits 延迟, MonoDigits HTTP/TLS, mono 错误摘要]`
   - 0 ProbeItem 空态：empty-state + ghost CTA「添加第一个 Probe」（trigger `onAddProbe`）
   - 0 observations 空态：dashed 占位 + "尚未收到观测"
7. DetailSection `当前异常`：IncidentList
8. DetailSection `事件`：EventList timeline

### NodeOnboardingPage
- 顶部 hero：节点身份卡（display_name + region/city/provider + 状态 badges + node_id 用 `<Hostname truncate>`）
- 接入进度：DetailSection 包 `<Stepper>` 4-phase 横向进度条（未开始接入 / 等待绑定 / 等待稳定观测 / 接入完成）。当前 phase 由 `binding_status × has_accepted_observation` 派生（`derivePhaseSteps`）；绑定冲突走 error 分支
- 绑定冲突（条件性，最高优先）：DetailSection ribbon='critical'，metric-card 展示指纹/时间/尝试次数（`<Hostname>` / `<Timestamp mode="absolute">` / `<MonoDigits>` 包装），三个动作（确认重绑 / 拒绝 / 重置）采用**两步式**——默认仅显示 ghost button「确认重新绑定…」「拒绝新指纹…」「重置绑定…」；点击后展开对应的 `<ActionConfirmationCard>`，描述当前→之后的状态迁移、「会发生」「不变」两行 callout，再二次确认才真正调 API
- 接入凭证：DetailSection ribbon='accent' 包 token 区块。token 是会话级一次性（localStorage cache + issued_at 匹配检查）：
  - 明文展开态：`Card cardRole='warning'`，token 用 `<MonoDigits>` + 复制按钮（`navigator.clipboard.writeText` + `document.execCommand` fallback），底部「已保存，关闭」secondary button
  - 折叠态：`Card cardRole='dim'` + critical 文案"已隐藏，本会话内可重新展开"+ ghost「重新展开 token 明文」。**注：本会话内 cache 不清，避免误点导致旧 token 作废 + agent 重发的高代价**
  - 错误态：`Card cardRole='warning'` + mono 错误摘要 + 重试按钮（design-language §7.2）
  - 未生成态：`.empty-state` + primary button「重新生成接入 Token」
  - 关于"倒计时"：当前后端 enrollment token 无 TTL（会话级一次性语义无法支撑倒计时），不实现倒计时；如未来引入 token TTL 再加（详见 task `05-03-redesign-node-onboarding` ADR-lite）
- 安装步骤：步骤 ol，env 行 / shell 命令在 mono `<pre><code>` 容器内 + 各自复制按钮；`server_url` 用 `window.location.origin` 派生（reverse proxy 场景需手动调整，页面有 hint）；token 占位 `###TOKEN###` 在 token 已生成且未折叠时自动填真值
- 接入完成：在 Phase 进度区下方一行小字 link「查看节点详情 →」

### LoginPage
- 全屏居中
- 装饰：印章感 SVG（"候"字）+ aurora 增强
- 卡片：候风 serif 大字 + HOUFENG sans 小字 + 「察变 · 守望」motto
- 表单：username / password / 登录 Button(primary lg)
- 错误：就地 alert（`role="alert"`）

---

## 六、文档导航

- 上层：[design-language.md](./design-language.md)
- 旧版：[v1-baseline/ui-ux-spec.md](../../_archive/design/v1-baseline/ui-ux-spec.md)（仅历史参考）
