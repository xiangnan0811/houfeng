# Design — operator UI night observatory

## Boundaries

- **改**：`web/src/styles/**`、`web/src/app/layout/**`、`web/src/pages/LoginPage.*`、日常路径页面与其测试、`docs/design/current/interface-language.md`、`component-patterns.md`、必要的 e2e 文案断言。
- **不改**：Go API、agent 合同、迁移、领域字段、CSP 政策文本（除非 dev 已有的 Fast Refresh 例外）、VPS write-owner 语义。
- **基线**：`origin/main` 的新 git worktree。当前 checkout 上的 `feat/ui-ux-craftsmanship-redesign` 与 21 个未提交文件保持不动，避免和心跳策略 worktree 缠在一起。

## Visual system

### Thesis

候风是夜间观测仪。材料来自「候风」本身：灯黑、青瓷、铁红、纸色文字。不是 Tailwind 石板蓝，不是矩阵绿 HUD，不是 Linear 微光。

### Color (designed in dark; hex is a starting point, browser-tuned to 4.5:1)

| Token | Dark role | Notes |
|---|---|---|
| `--bg` | `#0B0C0A` 灯黑 | 页底，不是蓝黑 |
| `--bg-sidebar` | `#10110E` | 略抬，无 aurora |
| `--surface` | `#141613` | 仅用于模态、警告、工具，不用于包表 |
| `--text-primary` | `#E8E6DF` 纸色 | 偏暖，避免冷灰蓝 |
| `--text-secondary` | `#A3A394` | |
| `--accent` / `--color-state-normal` | 青瓷 | 主交互与「正常」。小字不够对比时加亮，不要加 glow |
| `--color-state-notice` | 铁赭 | 关注 |
| `--color-state-alert` | 更深铁赭 | 告警；可与 notice 同族，靠形状区分 |
| `--color-state-critical` | 铁红 | 严重 |
| `--color-state-maintenance` / `--offline` | 青灰 | 虚线或缺角形状 |
| `--border` | 纸色 10% | 细、少 |

浅色映射：暖纸底 `#F4F1EA`、墨字、更深青瓷/铁红，过 axe。`classic` → 新暗色 class，不再维护第三套色。

删除 `--glow-*` 的使用。`--accent` 不再是 `#3B82F6`。

### Type

- UI：`--font-sans` = IBM Plex Sans + PingFang SC（已有 woff2）。
- 数据：`--font-mono` = IBM Plex Mono。
- 词标：`--font-serif` 用系统宋（Source Han Serif SC / Songti SC / Georgia），**不新增字体文件**。
- 杀掉中文 `text-transform: uppercase` 与 0.08em 级 tracking。
- 一页一个 `h1`。不要 eyebrow。

### Motion

`--dur-micro: 100ms` 只用于 `color` / `background-color` / `border-color`。禁止 `translateY` 入场、`fadeUp`、`page-enter`、stagger delay。

### Layout grammar

```
+--+------------------------------+
|轨| 标题                  主按钮 |
|  |------------------------------|
|  | 筛选一行或抽屉触发器         |
|  |------------------------------|
|  | 工作面：表 / 决策台          |
|  | 行 = 身份 + 状态形 + 下一跳  |
+--+------------------------------+
```

- 侧栏是静轨，不是分组海报。第一刀主项：工作台、VPS、监控、探测；底：设置。其余进「更多」（资产决策、订阅、服务商、归档、记录、事件、命令审计）。路由不删。
- 表直接坐在主列上，用行分割，不用 `page-panel` 包一层再滚动。宽表的横向滚动只发生在具名 region 内，document 不得横向溢出。
- 卡片只用于：确认、空/错、抽屉、模态、真正的工具块。
- 第一屏禁止防御性段落。判断一句话可以留；「不声明外部真相」一类删除。

### Shell

- 去掉侧栏「运营 / 资产 / 观测 / 系统」分组标签。
- TopBar：当前页名 + 搜索 + 新鲜度（已有摘要模型保留）。归档等缺标题的路由补 `PAGE_TITLES`，禁止回落到「候风」。
- 登录：去掉浮动「候」印。灯黑底、词标、两个字段、一个按钮。

### Status

继续用 `StatusGlyph`（色 + 形）。列表里状态文字可并列，不要 6px 发光圆点冒充系统。

### Page jobs (slice 1)

| 面 | 第一屏的一个问题 | 主操作 |
|---|---|---|
| 工作台 | 现在最该处理什么 | 一条 CTA，进具体对象 |
| VPS 列表 | 哪些机器要续费/取消/补事实 | 添加 VPS；行进详情 |
| VPS 详情 | 这台机器现在什么状态、下一步 | 决策/接入，危险动作隔离 |
| 监控列表 | 哪些实例不新鲜或在告警 | 行进详情；趋势列必须在 1440 第一屏可见 |
| 监控详情 | 这台实例的心跳与主机证据 | 受控动作，不发明新命令 |
| 入口探测 | 哪个入口在失败 | 新建目标；行进详情 |
| 设置 | 改阈值/通知并保存 | 保存条不遮挡字段 |

Slice 2 路由只换令牌/壳/去 eyebrow，不重做 IA。

## CSS architecture

- 不新增 CSS 文件。改现有 owner：`tokens.css`、`layout.css`、`page.css`、`atoms.css`、`LoginPage.css`、各业务 partial。
- `modernize.css` 是 catch-all：能删的规则删，剩下的搬回真正 owner。目标是缩小它，而不是再堆一层。
- 预算：实现开始时记录 `origin/main` 的 `css-budget.json` / `bundle-budget.json`。禁止上调。删除死动画、重复表皮肤、shine 来腾空间。
- 三主题令牌必须同步；classic 的 class 可以仍存在但变量指向新暗色。

## Data flow

- 页面仍走现有 `lib/api` 与 fixture e2e。不改 JSON 形状。
- 工作台继续用现有 dashboard model 的判断/CTA；删掉空话建议段，CTA 用模型里的具体标题。
- VPS 详情写入仍走 AppShell 级 write registry。视觉重建不得放宽 generation/owner。

## Compatibility

- 浅色映射必须让 `web/e2e/accessibility.spec.ts` settled axe 的 serious/critical 为 0。
- e2e `CORE_ROUTES` 的 heading / workflow name 随文案更新，不断言旧 CTA。
- 主题存储 key 不变。已选 `classic` 的浏览器会看到新暗色，这是有意的。

## Tradeoffs

- 第一刀不重做资产决策等页：产品会暂时「新壳旧内页」。用同一令牌降低分裂感，并在 docs 里写明这是切片，不是做完。
- 青瓷代替蓝：所有 `var(--accent)` 消费方会变。必须全站扫一遍，避免残留蓝边。
- 侧栏收进「更多」：少见页面多一次点击。单操作员可接受；路由仍可直达。

## Rollback

- 工作区是独立 worktree + 独立分支。回滚 = 不合并该分支。不改数据库，无需数据回滚。
- 不在实现中途把 Gemini 分支合进来。
