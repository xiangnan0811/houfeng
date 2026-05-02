---
date: 2026-04-29
status: superseded-by-v2-houfeng
superseded-on: 2026-04-30
superseded-by: docs/design/v2-houfeng/design-language.md
supersedes:
  - docs/design/v1-baseline/ui-ux-spec.md (visual portion only)
  - docs/design/v1-baseline/baseline-screens.md
  - docs/design/v1-baseline/visual-review-round2.md
  - docs/design/v1-baseline/stitch/* (deprecated)
preserves:
  - docs/design/v1-baseline/architecture-data-model.md
  - docs/design/v1-baseline/rules-and-interaction.md
  - docs/design/v1-baseline/tech-selection.md
---

> **SUPERSEDED 2026-04-30.**
> 本文档及 `plans/`、`status.md` 是 v1.x-frontend-redesign 的设计产物，落地后用户判定为简陋、缺乏成体系的设计语言。
> 现行视觉权威是 [`docs/design/v2-houfeng/design-language.md`](../v2-houfeng/design-language.md)。
> 本目录仅作历史记录，不再驱动开发。

# 候风 V1.x 前端视觉重新设计（已被 v2-houfeng 取代）

## 1. 背景

V1 视觉基线（Stitch Unified / Baseline 屏幕 + `ui-ux-spec.md`）在实施阶段被用户判定为审美不可接受、不堪使用。2026-04-29 由用户正式解冻 V1 视觉基线，启动 V1.x 视觉重新设计。

V1 的产品边界、对象模型、状态机、规则、API 路由保持不变；仅替换：

- 视觉系统（颜色、字体、间距、组件外观、布局节奏）
- 5 个主页面 + 节点详情 / 节点接入 / 目标详情 的视觉呈现
- 新增登录页与用户位（依赖后端方案 2，详见 §11）

## 2. 整体决定

| 维度 | 决定 |
| --- | --- |
| 风格预设 | 2 套预设：**候风原色**（默认）+ **经典** |
| 明暗模式 | 深 / 浅 双向，独立轴 |
| 主题切换矩阵 | 2×2 = 4 个组合，外加「跟随系统」 |
| 自定义粒度 | 无字体上传、无调色板、无密度开关。仅暴露主题二选一 + 明暗三选一 |
| 默认主题 | 候风原色 · 跟随系统（首次访问检测系统偏好，可手动覆盖） |
| 信息密度 | 单一密度，按页面类型内置策略（紧凑列表 / 中等摘要 / 舒展详情） |
| 字体策略 | Web Font 打包，~2.5 MB 缓存后零额外请求 |
| 顶层导航 | 5 项保持：**首页 / 节点 / 目标 / 事件 / 设置**（导航壳改为侧栏） |
| 用户标识 | 侧栏底部 user chip + 下拉菜单（主题、修改密码、退出） |
| 登录认证 | 后端方案 2：users 表 + 用户名 + 密码 + session cookie |
| 措辞清扫 | 前端禁出现「单用户 / 全权限 / 个人系统」措辞，UI 表达和未来多用户兼容 |

## 3. 范围

### In scope

- 视觉系统全部 token（颜色 / 字体 / 间距 / 圆角 / 边框 / 阴影 / 密度）
- 基础组件原子（按钮 / 输入 / 徽章 / 卡片 / Tab）
- 9 个页面布局（首页、节点列表、节点详情、节点接入、目标列表、目标详情、事件、设置、**登录**）
- 主题切换交互（设置页 + 用户菜单两处入口）
- 跟随系统的运行时检测（`prefers-color-scheme`）
- Web Font 打包与子集化
- 用户位 chip + 下拉菜单
- 登录页与登录流程
- 侧栏 shell（导航 + 同步状态 + 用户）
- 与后端方案 2 对应的 users 表、密码管理、session、API 鉴权（详见 §11）

### Out of scope

- 多用户、角色、权限、协作（虽然 schema 留口，UI 不暴露）
- OAuth / SSO / 双因素
- 操作审计日志面板
- 主题包市场 / 第三方主题
- 字体上传 / 用户自定义字体
- 大屏 / 移动端独立布局（响应式只到平板宽度，不做手机优先）
- 国际化（运行时语言切换）。注：mockup 中出现的「首页 / Dashboard」「节点 / Nodes」等双语标签是装饰性排版（西文小字作为视觉锚点），**不是** i18n 字典——硬编码即可，不引入 i18n 框架
- ECharts / 复杂图表组件库（仅极简 div-based 趋势条）
- 节点列表的地图视图

## 4. 视觉方向：双预设

两预设共享：布局、组件形状、间距、信息密度、状态语义、6 状态色槽位、字号尺度。差异仅在 5 处：

1. 品牌字体（衬线汉字使用程度）
2. H1 / Eyebrow / Body 是否使用衬线汉字
3. 鎏金重音色饱和度
4. 卡片角色与装饰（印章 / 角章 vs. 极淡光晕）
5. 副字行排版（汉字字距 / 等宽前缀）

### 4.1 候风原色（Hybrid · 默认）

- 基色：暖偏冷的中性深（`#131419`）；浅色：暖白（`#FAF7EF`）
- 仅品牌、状态文字、KPI 大数字使用衬线汉字（Source Han Serif SC）
- 其余使用无衬线（Source Han Sans SC + Inter）
- 鎏金 `#B89968`（深色）/ `#8B6B3D`（浅色）作为主重音
- 极淡角落光晕（rgba 鎏金 + rgba indigo）替代装饰
- 卡片：圆角 7px、左色带（2px）承载状态、半透明白底

### 4.2 经典（Classic · 候风正稿）

- 基色：墨黑（`#15140F`）；浅色：宣纸（`#F5F0E1`）
- H1 / Eyebrow / Body 全部使用衬线汉字
- 装饰：右上印章角章（候字旋转 45°）
- 鎏金 `#C09A5C` 饱和度更高
- 分隔靠竖线 / 虚线，不靠卡片
- 整体「印章+纸质」感更强

## 5. 主题切换矩阵

```
                   深色          浅色
候风原色（默认）   #131419     #FAF7EF
经典               #15140F     #F5F0E1
```

### 5.1 切换入口

1. **设置页 → 主题** Pill Tab：「风格」二选一 + 「明暗」三选一（深 / 浅 / 跟随系统）
2. **用户菜单 → 主题设置**（快捷跳转到 1）

### 5.2 默认与持久化

- 首次访问：候风原色 + 跟随系统
- 用户偏好持久化在浏览器 `localStorage`（keys: `houfeng.theme.preset`、`houfeng.theme.mode`）
- 后端不持久化主题偏好（避免不同设备切换时被覆盖；多设备一致性靠浏览器同步即可）

### 5.3 跟随系统

监听 `prefers-color-scheme` 媒体查询。模式设为「跟随系统」时，UI 实时跟随系统切换。模式设为「深」或「浅」时，覆盖系统偏好。

### 5.4 SSR / FOUC 防护

应用是 SPA，不做 SSR；在 `index.html` 头部内联一段同步脚本，根据 localStorage + 系统偏好立即给 `<html>` 加 class（`theme-houfeng-dark` / `theme-houfeng-light` / `theme-classic-dark` / `theme-classic-light`），避免 React 渲染前白屏闪一下。

## 6. 颜色 token

### 6.1 6 状态色（语义槽 · 全主题共享）

| 语义 | 候风原色 深 | 候风原色 浅 | 经典 深 | 经典 浅 |
| --- | --- | --- | --- | --- |
| 正常 | `#5BA88E` | `#2F8265` | `#4D9C7C` | `#2F8265` |
| 关注 | `#B89968` | `#8B6B3D` | `#C09A5C` | `#9A7A3D` |
| 告警 | `#C4814E` | `#A06434` | `#C97847` | `#A06434` |
| 严重 | `#B85042` | `#A53D2F` | `#B85042` | `#A53D2F` |
| 维护 | `#6E8FA8` | `#4F7393` | `#618BA8` | `#4F7393` |
| 离线 | `#6B6760` | `#7A7568` | `#6B6760` | `#7A7568` |

设计意图：

- **正常** = 翡翠（东方调性，避免工程绿）
- **关注 / 告警 / 严重** = 鎏金 → 暖橙 → 朱砂的暖系递进
- **维护** = 苍青冷色，与异常路线物理隔离
- **离线** = 烟灰 + dashed 边框，自然「退场」

### 6.2 表面色 token（按主题）

| Token | 候风原色 深 | 候风原色 浅 | 经典 深 | 经典 浅 |
| --- | --- | --- | --- | --- |
| `--bg` | `#131419` | `#FAF7EF` | `#15140F` | `#F5F0E1` |
| `--bg-sidebar` | `#0E0F13` | `#F2EDDF` | `#100F0B` | `#ECE5D2` |
| `--surface` | `rgba(255,255,255,0.025)` | `#FFFEFA` | `rgba(255,255,255,0.020)` | `#FBF7E8` |
| `--surface-elevated` | `rgba(255,255,255,0.040)` | `#FFFEFA` | `rgba(255,255,255,0.035)` | `#FFFEFA` |
| `--border` | `#26241D` | `#E8E2D5` | `#2A2823` | `#D8CFB8` |
| `--border-dashed` | `#3A3833` | `#C7BCA0` | `#3A3833` | `#C7BCA0` |
| `--text-primary` | `#F1ECE0` | `#2C2A24` | `#E8E1D5` | `#2A2620` |
| `--text-secondary` | `#A39E94` | `#5C5849` | `#A39884` | `#5C5849` |
| `--text-muted` | `#8A8576` | `#857F6E` | `#8C8472` | `#7A6F58` |
| `--text-disabled` | `#5C584F` | `#B5AC95` | `#5C584F` | `#B5AC95` |

落到 `web/src/styles/tokens.css` 时四套主题用 4 个 class 切换（`theme-houfeng-dark` / `theme-houfeng-light` / `theme-classic-dark` / `theme-classic-light`）。组件全部用 `var(--*)` 引用，不写死颜色。

### 6.3 重音渐变（仅候风原色）

- 角落光晕：`radial-gradient(ellipse at top right, rgba(184,153,104,0.10) 0%, transparent 50%), radial-gradient(ellipse at bottom left, rgba(99,102,241,0.05) 0%, transparent 50%)`
- 仅用在大背景 / 登录页 / 首页主区
- 经典预设不使用渐变光晕

## 7. 字体 token

### 7.1 10 个 Type Token

| Token | 字号 / 字重 / 行高 / 其它 | 用途 |
| --- | --- | --- |
| `--type-display` | 28 / 600 / 1.1 | 品牌、KPI 大数字 |
| `--type-h1` | 22 / 600 / 1.3 | 页面标题 |
| `--type-h2` | 16 / 600 / 1.4 | 区块标题 |
| `--type-body` | 14 / 400 / 1.6 | 默认正文 |
| `--type-small` | 12 / 400 / 1.5 | 说明、元数据 |
| `--type-eyebrow` | 10 / 500 / letter-spacing 0.18em | KPI 标签、横幅 |
| `--type-metric` | 14 / 500 / tabular-nums | 指标数字 |
| `--type-state` | 11 / 400 / serif / 0.06em | 健康/状态文字 |
| `--type-code` | 12 / 400 / mono | 配置、错误码、日志 |
| `--type-link` | 14 / 500 / underline / underline-offset 3px | 链接、行动文字 |

### 7.2 经典预设字体差异（仅 3 处）

- `--type-h1` 用衬线汉字
- `--type-eyebrow` 用衬线汉字 + 字间距更大（0.20em）
- `--type-body` 用衬线汉字

### 7.3 字体降级链

```css
--font-serif:   'Source Han Serif SC', 'Noto Serif SC', 'Songti SC', 'STSong', serif;
--font-sans:    ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont,
                'PingFang SC', 'Microsoft YaHei UI', 'Noto Sans SC', sans-serif;
--font-mono:    ui-monospace, 'JetBrains Mono', 'SF Mono', 'Cascadia Code',
                Menlo, Consolas, monospace;
--font-numeric: var(--font-sans);
```

`font-feature-settings: 'tnum', 'ss01'` 仅应用于 `--type-metric` 和 `--type-code`。

### 7.4 Web Font 打包清单

| Font | 字重 | 体积 (woff2) | 用途 |
| --- | --- | --- | --- |
| Source Han Serif SC | 500 | ~ 950 KB | 品牌 / 标题 / 状态 / 经典预设的数据标签 |
| Source Han Sans SC | 400, 500, 600 | ~ 1.4 MB | 所有正文 / 标题 / 元数据（中文） |
| Inter | 400, 500, 600 | ~ 90 KB | 西文正文 / 数据数字 |
| JetBrains Mono | 400 | ~ 80 KB | 代码 / 配置 / 错误码 |
| **总计** | | **~ 2.5 MB** | 首次访问后浏览器缓存 |

加载策略：

- 字符集子集化：常用 5000 字 + 标点 + 拉丁基本集；不打包 GB18030 全集
- `font-display: swap`：首屏先用系统字体，woff2 ready 后无跳动 swap
- 关键字重 `<link rel="preload" as="font">`（如：Sans 400 / 500，Serif 500）
- 字体文件托管：与中心二进制嵌入（中心 `WEB_DIST_DIR` 服务），不依赖外部 CDN

## 8. 物理参数 token

### 8.1 间距尺度（4 px 基础，常用 8 档）

```
--space-1   4px    icon gap
--space-2   8px    badge / tag 内边距
--space-3   12px   行间紧凑
--space-4   16px   默认 padding
--space-5   20px   卡片内边距
--space-6   24px   区块间距
--space-8   32px   页面 gutter
--space-12  48px   大区块分隔
```

### 8.2 圆角

```
--radius-0    0      印章 / 分隔点
--radius-1    4px    小标签
--radius-2    7px    默认（按钮、卡片、输入）
--radius-3    12px   模态、抽屉、Toast
--radius-pill 999px  胶囊（状态徽章、图标按钮）
```

### 8.3 边框 4 种处理

- `1px solid var(--border)`：默认卡片
- `1px dashed var(--border-dashed)`：离线 / 退场
- `1px solid var(--border) + border-left: 2px solid <state>`：状态承载
- `1px solid transparent`：分组背景（仅 bg 区分）

### 8.4 阴影哲学：默认 none

仅 3 档允许：

```
--shadow-glow     0 0 16px rgba(<accent>, 0.25-0.45)
                  仅状态点 / 重音元素
--shadow-soft     0 1px 2px rgba(0,0,0,0.18), 0 4px 8px rgba(0,0,0,0.10)
                  浮起卡片（hover）
--shadow-overlay  0 12px 40px rgba(0,0,0,0.55), 0 4px 12px rgba(0,0,0,0.35)
                  弹窗 / 抽屉 / Toast / 下拉菜单
```

**永远不要给一行节点列表加投影**。

### 8.5 信息密度（按页面内置）

- **紧凑**（行高 36–40px、padding 8–10px、字号 11–13px）：节点列表、目标列表、事件列表
- **中等**（行高 48–56px、padding 16–20px、字号 13–14px）：首页 KPI 卡、节点摘要、目标摘要
- **舒展**（区块间距 24–32px、padding 20–24px、字号 14–16px）：节点详情、目标详情、设置、登录

不向用户暴露密度开关。

## 9. 基础组件原子

### 9.1 按钮（4 语义 × 3 尺寸）

| 语义 | 视觉 | 用途 |
| --- | --- | --- |
| 主要 | 鎏金边框 + 半透明鎏金渐变底 | 主操作（保存、新建、登录） |
| 次要 | 中性深灰边 + 半透明白底 | 一般操作 |
| 幽灵 | 无边框 + 鎏金文字 | 内联操作（详情链接式） |
| 危险 | 朱砂边 + 朱砂半透明底 + 朱砂文字 | 重置、删除、清空 |

尺寸：sm（5/11、11px、radius 6）/ md（8/16、13px、radius 7）/ lg（11/22、14px、radius 8）

状态：默认 / Hover（边色加深 + 3px 鎏金 outline）/ Active（背景再加深）/ Disabled（文字 muted、不可点）/ Focus（2px 鎏金 outline + 2px offset）

### 9.2 输入

- 文本框 / 搜索框（带前缀 ⌕ 与 ⌘K 键位提示）/ 选择 / 文本域 / 标签输入（带 chip + 内联添加）/ 错误态（朱砂边 + 朱砂错误文字）
- Focus：1px 鎏金边 + 3px rgba 鎏金 outline + 半透明鎏金底

### 9.3 徽章 / Pill / Tag

- **状态徽章**（pill）：圆点 + 衬线汉字 + 半透明色底 + 状态色边
- **信息标签**（squircle）：中性 4px 圆角，用于元数据 / 标签 chip
- **计数徽章**（pill）：朱砂（严重）/ 鎏金（关注）/ 中灰（中性）

### 9.4 卡片（4 角色）

- 默认：中性 + 1px 边
- 状态卡：左色带 2px + 状态色衬底（淡）
- 重音卡：鎏金渐变底 + 鎏金边
- 警示卡：朱砂渐变底 + 朱砂边

### 9.5 Tab

- **底线式**（主用）：水平排列、当前项底部 2px 鎏金线、其余 muted；可带计数徽章
- **Pill Tab**（备用，仅设置子分区）：水平 segmented 控件，当前项鎏金底 + 主文本

## 10. 页面布局

所有页面共享：

- 左侧固定侧栏（220 px）
- 主内容区域（flex-1，padding `--space-6`）

详细布局见对应 mockup HTML（保留在 `.superpowers/brainstorm/4179663-1777445430/content/`，需要长期保留时另行迁移到 `docs/design/v1.x-frontend-redesign/mockups/`）。

### 10.1 侧栏 Shell

从上到下：

1. 品牌区（候风衬线 + FLEET CONTROL PLANE eyebrow）
2. 主导航 5 项：首页 / 节点（带异常计数） / 目标（带异常计数） / 事件 / 设置
3. flex spacer
4. 同步状态卡（中心运行正常 + 翡翠点 + glow + 等宽 sync 时间）
5. **用户 chip**（鎏金渐变小头像 + 用户名 + 角色「管理员」+ ▾）

用户 chip 点击 → 浮层下拉菜单（`--shadow-overlay`）：主题设置 / 修改密码 / 分隔线 / 退出登录（朱砂红）。

### 10.2 登录页（新增）

- 居中 380 px 卡片（圆角 12 px + 1 px 边）
- 卡片内：候风衬线大字（28 px）+ FLEET CONTROL PLANE eyebrow + 「察 变 · 守 望」副字
- 用户名 + 密码 + 登录主按钮
- 卡片底部仅显示版本号（如 `v1.0`），不显示「单用户 / 个人系统」字样
- 左上角鎏金印章（旋转 45° 候字框）
- 极淡角落光晕（仅候风原色预设）

### 10.3 首页

- 页面顶：H1「首页 / Dashboard」+ 副题 + sync 时间 + 刷新按钮
- 4 张 KPI 卡：异常节点 / 异常目标（鎏金重音）/ 严重态（朱砂警示）/ 维护中（苍青）
- 双列：左「当前异常节点」（最多 3-5 行，左色带状态卡）+ 右「当前异常目标」（同左色带）+ 翡翠淡块「其余 N 项均正常」空状态
- 全宽：「最近异常事件」表格（最多 6-8 行）
- **不要图表**

### 10.4 节点列表

- 顶：H1 + 副题（22 节点 · 3 异常 · 1 严重 · 2 维护中）+ 搜索 + + 新建节点
- 筛选条：6 个下拉 / 切换（仅看异常 / 供应商 / 地区 / 生命周期 / 运行状态 / 健康 / 标签）
- 已激活筛选用鎏金 chip + × 移除
- 表格：复选框 + 节点名（带状态点 + 异常摘要副字） + 地区/供应商 + 标签 chips + 生命周期/运行状态 + 健康（衬线汉字状态色）+ 等宽时间戳 + 相对时间
- 离线行整体降饱和（opacity 0.65）；维护行 opacity 0.85
- 分页（< 1 2 3 >）

### 10.5 节点详情

- 面包屑：首页 / 节点 / `<节点名>`
- 身份卡（鎏金左色带）：状态点 + H1 + 衬线状态文字 + 4 个微状态（生命周期 / 监控 / 绑定 + 自动派生健康）+ 标签 chips + 主操作（进入维护苍青重音）+ 次操作 + ⋯ 更多
- 派生元数据条（4 项 KV，等宽字体）
- 5 Tabs：概览 / 指标趋势 / 活跃异常（带计数）/ 最近事件 / 运维操作
- 概览 Tab 内容：
  - 左列（1.4 fr）：当前活跃异常 / 关键指标（4 张卡 + 极简 div 趋势条）/ 最近事件 mini 表
  - 右列（1 fr）：承担的探针列表 / 基础信息 KV / 危险区（朱砂虚线）

### 10.6 节点接入

- 面包屑深至「接入」
- 节点身份头：节点名 + 「等待绑定」鎏金 pill + 创建时间副字
- 4 阶段进度指示：已创建 ✓ → 等待绑定（当前，含 box-shadow glow）→ 等待稳定观测（dashed）→ 接入完成（dashed）
- 当前阶段操作：
  - **接入 Token 行**：鎏金重音底 + 等宽 token 文本 + 复制按钮 + 朱砂警示「关闭页面后不再展示」
  - **安装命令代码块**：深色底 #0D0E12 + 注释 muted + token 高亮鎏金 + 复制按钮
- 自动刷新（5 s）的接入尝试日志

### 10.7 目标列表

布局与节点列表一致，列：复选框 / 目标名（状态点 + 异常摘要） / 类型 chip / host:port / 标签 / 运行状态 / 健康 / ProbeItem 数量。

### 10.8 目标详情

- 面包屑 + 身份卡（与节点详情相似但更紧凑）
- 区块：**ProbeItem 列表**（每行 = Toggle + 类型 chip + 配置（GET / + 期望 + 超时）+ 频率档 + 状态 + ⋯ + 副字「最近 100 次成功/失败 + 平均 latency」+ 「查看趋势 →」）+ + 新增 ProbeItem 按钮
- 区块：执行节点视角（哪些节点承担了该 Target，含成功率）

### 10.9 事件页

- 高级筛选区：时间 segmented（24h/7d/30d/自定义）+ 对象类型 segmented + 严重度多选 pill + 事件类型下拉
- 4 个布尔开关：仅看通知 / 仅看恢复 / 仅看维护 / 含 backfill 补传
- 时间分组（今天 N / 昨天 N / 本周）
- 事件行：时间戳 / 对象+类型+变化 / Telegram 状态（✓ 已发送 / — 静默）
- 维护事件整体降饱和；恢复事件用淡翡翠底
- 「加载更早事件 ↓」分页

### 10.10 设置页

- H1 + 副题
- Pill Tab：Telegram 通知 / 频率档位 / 默认规则 / 数据保留 / **主题**（**主题**项是 V1.x 新增，承担风格预设 + 明暗模式两个开关；其余四项保持 V1 基线 §6.6 的语义不变）
- 表单布局：左 280 px（区块标题 + 说明）+ 右（表单字段）
- Toggle 开关（鎏金底 + 黑色滑钮）
- 危险区（朱砂虚线，每个区块底部）
- 保存条（鎏金重音底，仅在 dirty 时显示，「⚠ 你已修改 N 处尚未保存」+ 放弃 / 保存按钮）

#### 10.10.1 主题 Tab

- 风格预设：候风原色 / 经典（segmented + 各自缩略图）
- 明暗：深 / 浅 / 跟随系统（segmented）

## 11. 后端：登录 / 用户 / 鉴权（方案 2）

### 11.1 决定

方案 2：users 表 + 用户名 + 密码 + session cookie。预留多用户能力但 V1.x 仅单条用户记录。

### 11.2 数据模型

新增迁移 `db/migrations/0010_add_users_and_sessions.sql`：

```sql
create table users (
  user_id              text primary key,
  username             text not null unique,
  password_hash        text not null,            -- bcrypt
  display_name         text not null default '',
  role                 text not null default 'admin',
  created_at           timestamptz not null default now(),
  password_changed_at  timestamptz not null default now()
);

create table sessions (
  session_id    text primary key,                -- 256-bit random hex
  user_id       text not null references users(user_id),
  issued_at     timestamptz not null default now(),
  last_seen_at  timestamptz not null default now(),
  expires_at    timestamptz not null,
  user_agent    text not null default '',
  client_ip     text not null default ''
);

create index sessions_user_idx on sessions(user_id);
create index sessions_expires_idx on sessions(expires_at);
```

### 11.3 启动种子

中心首启动：

1. 若 users 表为空：从环境变量读取 `HOUFENG_INITIAL_USERNAME` 与 `HOUFENG_INITIAL_PASSWORD`（必填）创建首用户。否则中心拒绝启动并打印明确错误。
2. 用户登录后可在「修改密码」流程改密码；无注册流程。

### 11.4 接口

| 路径 | 方法 | 用途 |
| --- | --- | --- |
| `/api/auth/login` | POST | 用户名 + 密码 → set-cookie session |
| `/api/auth/logout` | POST | 销毁当前 session |
| `/api/auth/me` | GET | 返回当前用户信息（用户名 / role / display_name） |
| `/api/auth/password` | PUT | 修改密码（需提供旧密码） |

所有现有 `/api/...` 路由（除 `/api/healthz` 与 `/api/agent/...`）增加鉴权中间件。Agent 接口仍走 `enrollment token`，与 user session 解耦。

### 11.5 Session 策略

- TTL 7 天滚动（每次请求刷新 `last_seen_at` 与 `expires_at`）
- HTTP-only cookie，SameSite=Lax，HTTPS-only（部署时通过反向代理保障）
- 无 CSRF token（同源 + SameSite 已足够 V1.x，规避复杂度）
- 退出 = 删除 sessions 行 + clear cookie

### 11.6 前端集成

- 路由守卫：未登录访问任何非 `/login` 路径 → 重定向 `/login?next=<原路径>`
- `/api/auth/me` 在 SPA 启动时拉取，缓存到全局 store
- 401 拦截：所有 API 401 → 自动跳 `/login`
- 用户菜单的「修改密码」打开模态弹窗（旧密码 / 新密码 / 确认新密码）

### 11.7 措辞清扫

前端代码与文案均不出现：「单用户」「全权限」「个人系统」「sole user」等措辞。用户 chip 副字使用 `users.role`（默认显示「管理员」）。

## 12. 实施顺序提示（具体步骤交给 writing-plans）

下面只是提示，方便后续 plan 切片：

1. **基础工程**：tokens.css（4 主题）+ font-face + 主题切换运行时（含跟随系统 + FOUC 防护）
2. **基础组件**：button / input / badge / card / tab + 单元测试（Vitest 渲染覆盖）
3. **侧栏 Shell**：导航 + 同步状态 + 用户 chip + 下拉菜单
4. **后端 auth（方案 2）**：迁移 + handlers + 中间件 + 种子流程 + 单元测试
5. **登录页**：表单 + 路由守卫 + 401 拦截
6. **首页**重写
7. **节点列表 + 节点详情 + 节点接入**重写
8. **目标列表 + 目标详情**重写
9. **事件页**重写
10. **设置页**重写（含主题 Tab + 修改密码模态）
11. **视觉证据更新**：`docs/operations/visual-evidence/` 重抓 4 主题 × 9 页 = 36 张（实际可能合并至 8-10 张代表性截图）
12. **gap-checklist 更新**：标记 V1 视觉项已被 V1.x 替代

## 13. 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| Web Font 2.5 MB 影响首次访问 | preload + swap + 子集化 + 浏览器缓存；首次后 0 字体请求 |
| 4 主题切换的 CSS token 量爆炸 | 用 CSS 变量 + 主题 class 切换，不复制组件代码 |
| 经典预设衬线在数据型页面降低阅读效率 | 经典仅 3 处用衬线（H1 / Eyebrow / Body）；数据数字始终无衬线 + tabular-nums |
| 浅色主题状态色对比度不足 | 已为浅色单独深一档（如 关注 #B89968 → #8B6B3D）；上线前用 axe / Lighthouse 验证 WCAG AA |
| 鎏金 / 朱砂在色弱用户上分辨率不足 | 不依赖单一色彩传递状态：每行 = 色点 + 衬线汉字 + 左色带 + 副字描述四重冗余 |
| 登录方案 2 多出 ~3 天工程量 | 写入 §11，明确 schema 与接口；future-multi-user 价值高于 1 天工时 |
| 主题 + 明暗双开关增加测试负担 | E2E 仅覆盖默认主题；token-level 单测覆盖 4 主题；视觉证据轮换抓图 |

## 14. 验证

### 14.1 自动化

- `make verify` 通过（Go + Web tsc + Vitest + ESLint）
- 4 主题各跑一次组件渲染快照（snapshot test）
- WCAG AA 对比度自动测试（脚本扫每个 token 组合）

### 14.2 视觉

- 主要页面（9 页）× 4 主题 × 1 viewport（1440×1024）= 截图证据集
- `docs/operations/visual-evidence/manifest.json` 重写

### 14.3 用户验收

- 用户在 4 主题下走过完整冒烟路径（Node 接入 → Target 创建 → ProbeItem → 异常 → 通知 → 恢复）
- 字体加载、主题切换、跟随系统 3 个体感项各专项检查

## 15. 旧基线处理

- `docs/design/v1-baseline/ui-ux-spec.md` / `visual-review-round2.md` / `baseline-screens.md` / `stitch/*` 标记为 historical，文件保留作为决策记录但不再作为开发基线
- 在 `docs/design/v1-baseline/README.md` 顶部加注：「视觉部分自 2026-04-29 起被 `docs/design/v1.x-frontend-redesign/` 替代，结构与规则部分仍然冻结生效」
- 项目 README.md「Guardrails」段更新「Visual authority stays」一句，指向 V1.x

## 16. 变更日志

| 日期 | 变更 |
| --- | --- |
| 2026-04-29 | 初版，由 brainstorming session 生成并经用户分块逐项确认 |

## 17. mockup 引用

视觉设计稿（HTML 静态稿）当前位于 brainstorm session 临时目录：

```
.superpowers/brainstorm/<session>/content/
  visual-direction.html
  visual-direction-v2.html
  presets-comparison.html
  light-mode-comparison.html
  status-palette.html
  typography.html
  spacing-borders.html
  components.html
  page-dashboard.html
  auth-and-user.html
  page-node-detail.html
  pages-batch-1.html        (节点列表 / 设置页 / 节点接入)
  pages-batch-2.html        (事件页 / 目标详情 / 首页浅色)
```

如需长期保留，建议复制到 `docs/design/v1.x-frontend-redesign/mockups/` 并 commit。
