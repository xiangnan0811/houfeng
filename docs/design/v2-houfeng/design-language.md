---
date: 2026-04-30
status: active
supersedes:
  - removed v1-baseline visual specification
  - removed v1.x frontend redesign process materials
tags:
  - 候风
  - 设计语言
  - dark-first
  - v2
---

# 候风原色 v2 · 设计语言

## 0. 前言：这份文档解决什么

V1 的 token 系统本身质量很高（4 主题、状态色齐全、字体三套、8pt 间距），但 V1 实现层与基线文档严重漂移：浅色主题主导、mono 字体未启用、节点列表是卡片堆而不是高密度表格、首页是单值 KPI 阵列、无 sparkline、`bg-aurora` 定义了未启用。

v2 不是"换一层皮"，而是把**已经写好的设计语言真正贯彻到所有页面**，并且重新提炼"候风"意象，让产品具备一个**东方观象台气质的工程工具**形象。

## 1. 视觉北极星

> **候 = 观察 / 候气候**
> **风 = 信号 / 风信物候**
> 候风是一台为远端服务器持续候气候、看风信的器物。

### 1.1 意象锚点
- **古观象台** — 北京古观象台的青铜仪器架在深石板上，沉静、精密、可长时间凝视
- **节气 / 物候** — 时间被切分成稳定循环的色彩节奏（不是节日喜庆，而是物候日志）
- **山水留白** — 信息密度高的同时仍保留呼吸节奏，不堆挤

### 1.2 避免的反例
- **大屏监控中心** — 夸张霓虹、对比过强、为可视化而可视化
- **普通 SaaS 后台** — 白盒卡片、空旷无信息、CRUD 后台味
- **"中国风"廉价化** — 红金灯笼、毛笔字图标、过度装饰

### 1.3 一句话气质
**冷静、克制、高密度、工程师长期使用友好。**

## 2. 色彩系统

### 2.1 Dark hero（默认调性）

| 角色 | 名称 | 值 | 用途 |
|---|---|---|---|
| `--bg` | 玄夜青 | `#0B0E13` | 页面底色 |
| `--bg-sidebar` | 墨青 | `#070A0E` | 侧栏、低层背景 |
| `--surface` | — | `rgba(255,255,255,0.022)` | 一级容器 |
| `--surface-elevated` | — | `rgba(255,255,255,0.045)` | 二级容器、hover |
| `--surface-pressed` | NEW | `rgba(255,255,255,0.07)` | 按下/选中、危险区 |
| `--border` | — | `#1F2128` | 主边框 |
| `--border-strong` | NEW | `#2A2D36` | 强分隔 |
| `--border-dashed` | — | `#383B45` | 虚线边框 |
| `--text-primary` | 月白 | `#EDE8DA` | 主文本 |
| `--text-secondary` | 烟青 | `#9EA3AE` | 次文本 |
| `--text-muted` | 远岚 | `#7B7F88` | 弱文本（已抬亮以达 AA） |
| `--text-disabled` | — | `#48494E` | 禁用 |
| `--accent` | 晨晖金 | `#C9A56F` | 主强调 |
| `--accent-strong` | — | `#E0BE85` | 高亮版 |
| `--accent-soft` | — | `rgba(201,165,111,0.12)` | 软背景 |
| `--accent-border` | — | `rgba(201,165,111,0.32)` | 强调边 |
| `--accent-2` | NEW 远岚青 | `#5C8FA8` | 次强调（图表、维护色根） |
| `--color-state-normal` | 松青 | `#4FA08A` | 正常 |
| `--color-state-notice` | 杏黄 | `#D4A053` | 关注 |
| `--color-state-alert` | 朱砂 | `#C97247` | 告警 |
| `--color-state-critical` | 绛 | `#B6493A` | 严重 |
| `--color-state-maintenance` | 烟蓝 | `#638DA9` | 维护中 |
| `--color-state-offline` | 远岚灰 | `#6F6B62` | 暂停/离线 |
| `--bg-aurora` | NEW 启用 | 双径向（详见 2.4） | 极光氛围层 |

### 2.2 Light secondary（同等品质）

light 主题不是次要主题。它必须独立打磨到同等可用：

| 角色 | 值 | 备注 |
|---|---|---|
| `--bg` | `#F4EFE3` | 宣纸白基底（不奶油） |
| `--bg-sidebar` | `#EAE3D0` | 比 bg 低半档 |
| `--surface` | `#FBF7EC` | 一级 |
| `--surface-elevated` | `#FFFEFA` | 二级、hover |
| `--surface-pressed` | `#F0E9D6` | 按下 |
| `--border` | `#D8CFB8` | 实线边 |
| `--border-strong` | `#C4B89E` | 强分隔 |
| `--text-primary` | `#1F1C16` | 主文本（接近油墨） |
| `--text-secondary` | `#4A4638` | 次文本 |
| `--text-muted` | `#6E6856` | 弱文本 |
| `--accent` | `#9A7A3D` | 主强调（暗化版晨晖） |
| `--accent-2` | `#406D86` | 次强调（暗化烟蓝） |
| state colors | 各深 1 档 | 保持语义对应、提升对比 |

### 2.3 Classic 主题（历史保留）

`classic-dark` / `classic-light` 不在 v2 创新范围内，只确保切换不破。配色保留接近 v1 即可。

### 2.4 Aurora 极光氛围

`--bg-aurora` 在 dark 默认值：

```css
--bg-aurora:
  radial-gradient(ellipse at top right,    rgba(201, 165, 111, 0.10) 0%, transparent 55%),
  radial-gradient(ellipse at bottom left,  rgba(92, 143, 168, 0.06) 0%, transparent 55%);
```

**应用范围**：
- ✅ `body` 加全局弱版本（强度 / 不透明度 减半，~5% / 3%）
- ✅ `.hero-panel::before` 保持现有用法（强度 100%）
- ❌ `.sidebar` 不加（侧栏要保持稳定锚点感）

### 2.5 不做的事
- 不引入纯黑（`#000`）— 永远用 `--bg-sidebar` 或更深的 `--surface-pressed`
- 不引入纯白（`#FFF`）— light 主题用 `#FFFEFA`
- 不在 token 之外硬编码颜色 — 一律走 CSS 变量

## 3. 字体角色（强制约束）

### 3.1 字体栈

```css
--font-serif: 'Source Han Serif SC', 'Noto Serif SC', 'Songti SC', 'STSong', serif;
--font-sans:  'Source Han Sans SC', 'Noto Sans SC', Inter, ui-sans-serif, system-ui,
              -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei UI', sans-serif;
--font-mono:  'JetBrains Mono', ui-monospace, 'SF Mono', 'Cascadia Code', Menlo, Consolas, monospace;
```

### 3.2 角色映射（**强制**）

| 角色 | 字体 | 应用场景（举例不穷举） |
|---|---|---|
| 品牌 | serif | 侧栏 `候风` logo、页面装饰性 eyebrow |
| 标题 / 正文 | sans | 页面标题、区块标题、说明文案、按钮文案 |
| **数字度量** | mono `tabular-nums` | KPI 大数字、表格数字列、duration、百分比、毫秒 |
| **技术 ID** | mono | hostname、`nd_xxx` 节点 ID、IP、token、SHA、UA |
| **时间戳** | mono | 全部绝对时间戳（YYYY/MM/DD HH:mm:ss） |
| 状态徽标 | serif | `Badge` 的 state 变体保留宋体 + small caps tracking |

### 3.3 落地组件

通过 `Mono.tsx` 三个包装组件保证落地：
- `<MonoDigits>{42.7}</MonoDigits>` — 任意数字度量
- `<Hostname>{'nd_89b8...'}</Hostname>` — ID / hostname / IP
- `<Timestamp value={iso} mode="absolute|relative" />` — 时间戳（hover 切显另一面）

实施期 grep 标准：搜索"`%` `ms` `KB/s` `MB`"以及节点 ID 字符串，确保它们都被 `MonoDigits` / `Hostname` 包裹。

## 4. 密度与节奏

### 4.1 间距系统（不变）
- 8pt 节奏：`--space-1: 4px` 起步，到 `--space-12: 48px`
- 区块间距 = `--space-5` (20px)，不是 `--space-6` (24px)
- 卡片 padding = `--space-3` ~ `--space-4` (12-16px)，不是 `--space-6`

### 4.2 表格行高
- 紧凑模式：36px / row（默认）
- 标准模式：44px / row

### 4.3 KPI 单元
- 单值 KPI 卡片 → 复合 stat-strip（5 列等宽，每列：数值 + delta + TrendArrow）
- 不再用"一个数字一张大卡片"的低密度阵列

### 4.4 区块层级
页面默认 5 级层级：
1. 页面身份区（hero / page-panel）
2. 当前问题区（最高优先信息）
3. 趋势 / 上下文区
4. 历史 / 事件区
5. 危险区（明显隔离）

不是所有页面都会用满 5 级，但顺序固定。

## 5. 运动语言

### 5.1 时间常量

```css
--ease-calm: cubic-bezier(0.22, 1, 0.36, 1);
--dur-micro: 120ms;   /* hover, focus */
--dur-state: 220ms;   /* toggle, modal open */
--dur-page:  360ms;   /* page enter / drawer */
```

### 5.2 保守原则

仅以下场景做动画：
- ✅ hover / focus（背景色 + 阴影 fade）
- ✅ Toggle 滑块、Tabs 指示器位移
- ✅ 模态开合（fade + 微 translate）
- ✅ SyncStatus 状态点呼吸（仅 ok 状态、1.6s 周期）

**不做**：
- ❌ 状态色变化（severity 升降）— 必须立即变色，监控类 UI 把渐变误认成"还在生效中"会导致误判
- ❌ 数值变化的 count-up 动画 — 干扰阅读
- ❌ 列表行进场 stagger — 制造障眼

### 5.3 减少动效偏好

```css
@media (prefers-reduced-motion: reduce) {
  * {
    animation-duration: 0.01ms !important;
    transition-duration: 0.01ms !important;
  }
}
```

## 6. 状态语言

### 6.1 健康状态六态

| 状态 | 颜色 | StatusGlyph 形状（SVG） |
|---|---|---|
| 正常 normal | 松青 | 实心圆 ● |
| 关注 notice | 杏黄 | 半填圆（左实右空） ◐ |
| 告警 alert | 朱砂 | 空心圆 + 下半填 |
| 严重 critical | 绛红 | 实心方 + 对角斜线 |
| 维护 maintenance | 烟蓝 | 空心方框 |
| 离线 / 暂停 offline | 远岚灰 | 虚线圆 |

**为什么要形状不仅靠色**：色弱用户在 dark 主题下区分朱砂 / 绛红 / 杏黄 比较吃力，形状是无障碍兜底。

### 6.2 实施

通过 `<StatusGlyph state={...} size="sm|md" />` 内联 SVG 实现，不依赖 Unicode 字符在不同 OS / 字体下的渲染一致性。

### 6.3 状态优先级

视觉权重：critical > alert > notice > normal > maintenance > offline

排序、徽标背景透明度、ribbon 强度都遵守这个优先级。

## 7. Loading / Error / Empty 三态规范

监控产品的三态比"快好看"更重要，必须有统一处理。

### 7.1 Loading
- 数据加载用 `surface` 底色 + 一行 mono 文案 `"正在加载…"`+ 时间戳
- **不做骨架屏**（数据形状变化大、骨架屏不准反而误导）
- 列表 / 表格行级 loading 用单行 ghost row（高度等同 row、底色 `surface-elevated`、内容仅左对齐 mono `"…"`）
- **不用 spinner**

### 7.2 Error
- 用 `card--warning` 风格（虚线 critical 边 + critical-soft 背景）
- 三件必备：① 一句中文描述 ② mono 错误摘要（API status code 或 `Error.message` 截断 120 char）③ 重试按钮（如果可重试）
- **不弹 toast**；错误就地展示，让用户知道哪一块崩了

### 7.3 Empty
- 复用 `.empty-state` 类，视觉升级为：① 居中 SVG 装饰小图（云气 / 风信 / 卦象一类抽象符号，~40×40px，单色 `--text-muted`）② 一行解释 ③ 一个 ghost button CTA

#### 必须穷举的空态及文案锚点

| 场景 | 文案 | CTA |
|---|---|---|
| 无 Node | 候风尚未接入任何节点 | 「新建第一个节点」 |
| Node 未绑定 Agent | 节点已创建但 Agent 未上线 | 「查看接入指引」 |
| Dashboard 异常目标块空 | 当前没有异常目标 | 无 CTA |
| Node Detail 当前异常块空 | 节点当前没有活跃异常 | 无 CTA |
| Events 主页空查询 | 没有匹配的事件 | 「重置筛选」 |
| 无目标 | 候风尚未配置任何观测目标 | 「新建第一个目标」 |
| Probe 列表空 | 目标尚未配置 ProbeItem | 「添加 Probe」 |
| 设置无覆盖规则 | 未设置任何覆盖规则 | 无 CTA（这是常态） |

## 8. 视觉原语清单

不增加 atom 库的总数太多。新增 5 个，删 1 个旧概念：

| 原语 | 类型 | 说明 |
|---|---|---|
| Sparkline | 新 atom | 纯 SVG `<polyline>`，64×16px 默认，根据 tone 着色 |
| TrendArrow | 新 atom | 上升 ↗ / 下降 ↘ / 平 → ＋ delta；可设 `inverse` 反转语义 |
| StatusGlyph | 新 atom | SVG 状态形状指示符，6 个固定形状 |
| MonoDigits / Hostname / Timestamp | 新 atom（同文件） | mono 字体包装组件 |
| DataTable | 新 atom | 高密度表格，`<table>` 语义化、行 hover、tabular-nums |
| ~~WeatherRibbon~~ | **不新增** | 折叠为 `Card` 的 `ribbonPlacement` prop |

新增的 EmptyState v2 不是新 atom，只是 `.empty-state` 类样式升级。

## 9. 既有 atom 升级要点

| atom | 升级点 |
|---|---|
| Button | 更精炼的 hover/active 阴影；focus ring 用 accent 三层光晕 |
| Badge | `count` 变体强制 mono；`state` 变体内部 hairline 装饰 |
| Card | padding 收紧；`cardRole='state'` 加 `ribbonPlacement?: 'left' \| 'top'`（默认 left）；新增 `cardRole='dim'` 弱化 |
| Input | label `letter-spacing` 增强；focus 三层光晕 |
| Toggle | 36→40px；thumb 过渡换 calm 曲线 |
| Tabs | underline 加粗 1px；pill 加深 |

**API 不变**：现有 props 与 CSS 类名一律保留，最大化保护既有调用方与测试。

## 10. 共享组件升级要点

| 组件 | 升级点 |
|---|---|
| StatusBadge | 重写为 `Badge` 瘦包装，做 cyan→neutral / green→normal / yellow→notice / red→critical / slate→offline 映射 |
| DetailSection | 加可选 `ribbon?: Tone` prop（顶 hairline） |
| IncidentList | **不**做时间线（活跃异常不是按时间消费）。改为按 severity 排序紧凑数据行 |
| EventList | 改为纵向时间线 + 日期 sticky 分组（"今天" / "昨天" / "YYYY/MM/DD"） |
| ActionConfirmationCard | 重做为状态迁移卡（详见 component-spec.md） |

## 11. 壳层升级要点

| 模块 | 升级点 |
|---|---|
| Sidebar | 品牌区下加一道 hairline divider；nav 激活态加 2px accent 左条；**不**为 nav 项添加状态点 |
| SyncStatus | 状态行下方加 7-bar 心跳 sparkline（数据缺失降级到只显示状态行） |
| AppShell | 主区 padding 改 `24px 28px`；侧栏保持 220px |
| UserChip | 头像渐变更精致；role 改 serif eyebrow |

**不做**：top bar / breadcrumb / 全局搜索（控制范围；后期独立项目）。

## 12. 不做的事（约束）

| 类目 | 不做 |
|---|---|
| 框架 | 不引入 Tailwind / UnoCSS / styled-components |
| 库 | 不引入图表库（recharts / echarts） — sparkline 用纯 SVG |
| 主题强制 | 不强制覆盖 OS 偏好（`mode='system'` 保持） |
| 后端 | 不改 API、数据形状、retention、聚合 |
| 文案 | 不改既有中文文案（除非新布局逼迫） |
| 变量 | 不重命名既有 CSS 变量（`--accent` `--surface` `--border` 等保持） |
| 类名 | 不重命名 atom 类名（`.btn--primary` `.tone--critical` `.summary-card` `.probe-card`） |
| 移动端 | 不适配响应式 — 这是单用户桌面工程工具 |
| i18n | 不引入英文界面 — 中文为主 |
| 视觉回归测试 | 不引入 Playwright 截图对比；如需视觉语境，使用本地/外部截图说明，默认不提交截图目录或 manifest |

### 12.x 监控视图与图表自研路径（2026-05-05 更新）

候风的"不引图表库"约束**仍然保留**。但因 watchtower 视图（节点详情主视图 8 张时序大图）需要更完整的时序图能力，新建 `<MetricChart>` 原子（纯 SVG，含 X/Y 轴 / 阈值线 / 维护窗口阴影 / 十字线 hover tooltip / 0-sample / 1-sample 边界态），与既有 `<Sparkline>`（240×60 配角，仍由其他页面消费）并行存在不互相替代。

未来如出现"读不准 / 缺缩放刷选 / 多线 overlay 对比 / event 标记叠加在趋势线"等真实需求，再单独发起 task 评估升级到 visx（@visx/* 按需引入约 30-50KB），不当下松动；自研 `<MetricChart>` 在那之前作为统一时序图入口。

## 13. 对比度结果（Phase 1 实测）

按 WCAG 2.1 相对亮度公式手算（验证日 2026-04-30）。

### 13.1 Dark 主题（bg = `#0B0E13`，L ≈ 0.005）

| 组合 | 期望 | 实测比 | 达标 |
|---|---|---|---|
| `text-primary` `#EDE8DA` on `bg` | ≥ 7.0:1 (AAA) | **15.71:1** | ✅ AAA |
| `text-secondary` `#9EA3AE` on `bg` | ≥ 4.5:1 (AA) | **7.57:1** | ✅ AAA |
| `text-muted` `#7B7F88` on `bg`（已抬亮 from `#6E727B`） | ≥ 4.5:1 (AA) | **4.76:1** | ✅ AA |
| `state-normal` `#4FA08A` on `bg` | ≥ 3.0:1 | **6.15:1** | ✅ AA |
| `state-notice` `#D4A053` on `bg` | ≥ 3.0:1 | **8.20:1** | ✅ AAA |
| `state-alert` `#C97247` on `bg` | ≥ 3.0:1 | **5.48:1** | ✅ AA |
| `state-critical` `#B6493A` on `bg` | ≥ 3.0:1 | **3.71:1** | ✅ AA Large |
| `state-maintenance` `#638DA9` on `bg` | ≥ 3.0:1 | **5.39:1** | ✅ AA |
| `state-offline` `#6F6B62` on `bg` | ≥ 3.0:1 | **3.64:1** | ✅ AA Large |
| `text-primary` on `accent-soft` 叠层（≈`#222220`） | ≥ 4.5:1 | **13.25:1** | ✅ AAA |

**注**：critical / offline 仅过 AA Large（≥3:1）。这两个色在产品里只用于状态徽标（≥14px bold = WCAG 大字符）和 ribbon / glyph，**不**单独承担正文文案，所以 AA Large 已足够。

### 13.2 Light 主题（bg = `#F4EFE3`，L ≈ 0.865）

| 组合 | 期望 | 实测比 | 达标 |
|---|---|---|---|
| `text-primary` `#1F1C16` on `bg` | ≥ 7.0:1 (AAA) | **14.50:1** | ✅ AAA |
| `text-secondary` `#4A4638` on `bg` | ≥ 4.5:1 (AA) | **8.08:1** | ✅ AAA |
| `text-muted` `#6E6856` on `bg` | ≥ 4.5:1 (AA) | **4.78:1** | ✅ AA |
| `state-normal` `#2F8265` on `bg` | ≥ 3.0:1 | **4.07:1** | ✅ AA Large；≈ AA Normal |
| state 其他色（已分别加深） | ≥ 3.0:1 | 全部 ≥ 3.5:1 | ✅ |

### 13.3 调整记录

- `text-muted` 从 `#6E727B`（dark）抬到 `#7B7F88` — 原值 4.21:1，紧贴 AA 边缘，抬亮后 4.76:1 安全
- light 主题 state 色统一加深一档（从 `#3FB48E` → `#2F8265` 等），保证白底也过 AA

## 14. 文档导航

- [`component-spec.md`](./component-spec.md) — 每个原语 / atom / 共享组件 / 页面壳的视觉契约
- [v1-baseline](../v1-baseline/README.md) — active 路径仅保留架构 / 数据模型 / 交互规则等结构层；视觉权威已转到 v2-houfeng

历史 v1 视觉与 v1.x frontend redesign 过程材料已从 tracked docs 移除；如需追溯，请使用 git history，不要把它们当作当前实现指导。

## 15. 一句话

> v2 不是"换皮"，而是把 V1 写得已经很好的设计语言**真正贯彻到所有页面**，并把"候风"的东方观象台气质从名字落到屏幕上。
