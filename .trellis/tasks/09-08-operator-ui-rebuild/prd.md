# Rebuild Houfeng operator UI as a night observatory

## Goal

让单操作员在长会话里愿意打开候风，并在第一屏回答：发生了什么、证据是否新鲜、下一步能安全做什么。界面是夜间观测仪：统一、安静、高密度、诚实。

## Background

操作员是目前唯一使用者，不急于上线，线上测试数据不重要。当前每一页都难以使用。Gemini 在 `feat/ui-ux-craftsmanship-redesign` 上的两轮「工艺重构」质量过低，连同未提交的五页重绘一并丢弃。候风当前蓝黑 SaaS 视觉也可以推翻。

已核实：

- 领域对象身份（VPS / MonitoringInstance / Target / ProbeItem / 订阅）不改。观测运行时合同允许为重建后的工作台做加性扩展（HostMetricPoint 增列、`network_rates_valid`、runtime-summaries、runtime-stream 收据时间），与前端同分支落地。硬约束仍在：React 19 SPA、纯 CSS + BEM + `tokens.css`、禁止 JSX `style=`、严格 CSP、CSS owner/budget ratchet、现有 Vitest + Playwright。不引入 Tailwind、CSS-in-JS、新字体文件、新图表库。
- `docs/design/current/interface-language.md` 允许改视觉默认。不可推翻的是证据诚实、密钥、生命周期确认、拓扑。
- 现有三层 IA（一问 / 一表 / 一模态）仍然适用，改的是材料和页面语法，不是工作流发明。
- 侧栏现状：四组十二项。e2e `core-routes.spec.ts` 对 11 条路由 × 3 视口断言标题与主操作，改文案或主 CTA 必须同步改测试。
- CSS 预算以 **`origin/main` 的上限** 为任务起点，不以 Gemini 抬高后的数字为准。

## Requirements

- R1. 全站页面语法：一页一个中文标题、一组动作、一块工作面（表或决策台）。禁止 padded 卡片包裹每张表；禁止双语/英文大写眉题；禁止解释性防御文案进第一屏。
- R2. 视觉身份为夜间观测仪：灯黑底、青瓷表示平静/正常与主交互、铁红表示严重、琥珀表示注意、中性灰表示维护/离线。禁止电光蓝品牌色、运维绿 HUD、Orbitron、微光圆点、标题渐变、卡片反光。宋体仅用于「候风」词标；UI/数据继续用仓库内 IBM Plex Sans / Mono。
- R3. 动效只允许 ≤120ms 的颜色/表面变化。禁止入场位移、阶梯延迟、状态呼吸光。
- R4. 状态同时用颜色和形状（现有 `StatusGlyph` 或等价）。ID / IP / 时间 / 数字走等宽。文案默认中文。
- R5. 空态、错误、加载落在出问题的局部表面，并标明证据新鲜度。
- R6. 危险生命周期动作保持显式预览 + 确认 + 审计。VPS 详情写入仍遵守 `.trellis/spec/web/vps-detail-ownership.md`。
- R7. `make verify-web` 与 `npm run test:e2e` 通过。CSS/bundle 上限不高于任务开始时 `origin/main` 的值，只许随清理下降。
- R8. 更新 `docs/design/current/interface-language.md` 与 `component-patterns.md`。
- R9. 第一刀交付日常路径（登录、壳、工作台、VPS 列表/详情、监控列表/详情、入口探测、设置）。其余已有路由当天换上同一壳和令牌，内部 IA 可留到后续刀。
- R10. 暗色是设计身份。浅色只做对比度安全映射，须过现有 settled axe。`classic` 不再维持第三套调色，运行时回退到新暗色。

## Out of scope

- 重做后端领域模型、通知渠道语义、安装器、多用户 SaaS。观测工作台所需的 agent/center **加字段**与对应迁移不在此列，允许与 UI 同分支。
- Tailwind、CSS-in-JS、新字体 CDN/文件、Playwright 之外的视觉回归框架。
- 营销站、霓虹 SOC、廉价中国风装饰（浮动「候」印、祥云、渐变书法标题）。
- 对比引擎、事件流完整产品方案（视觉对齐可以做；不另开 IA）。
- 把浅色主题设计成第二套完整身份。
- 把本次重建拆成 Go-only / UI-only PR（2026-09-21 已否决）。

## Acceptance Criteria

- [ ] AC1. 日常路径在 1440×1000 与 390×900 下，第一屏能完成该页主任务，无需先读解释段。
- [ ] AC2. 登录、壳、工作台、VPS 列表/详情、监控列表/详情、入口探测、设置已按夜间观测仪语法重建，并经浏览器实操（点、填、进详情），不是只截一张图。
- [ ] AC3. 其余已有路由使用同一壳、令牌、标题语法；允许内部仍密，但不允许旧电光蓝/eyebrow/shine 作为品牌残留。
- [ ] AC4. 无入场位移动画、无标题渐变、无卡片 shine、无未定义 CSS 变量、无状态呼吸光。
- [ ] AC5. `make verify-web` 与 `npm run test:e2e` 通过；`web/css-budget.json` 与 `web/bundle-budget.json` 上限 ≤ `origin/main` 任务开始时的值。
- [ ] AC6. 三套运行时 class（新暗色、浅色映射、classic 回退到新暗色）下，带文字的状态色与主按钮对比度仍达 WCAG AA。
- [ ] AC7. 当前设计文档与实现一致。

## Decisions

- 视觉：夜间观测仪（2026-09-08，选 A）。
- 范围：日常路径先做完，其余路由先换皮（2026-09-08，按推荐）。
- 节奏：不赶工；每一刀浏览器验收后再进入下一刀。测试环境数据可以打乱。
- 主题：暗色为身份；浅色为映射；classic 回退到新暗色。
- 基线：从 `origin/main` 干净工作区重建，不基于 Gemini 分支打补丁。
- 落地：单分支混装（2026-09-21）。前端重建与其所需的 Go / agent / 迁移在 `feat/operator-ui-observatory` 一次合入、一次部署到第一次生产环境。F34 关闭为已接受过程决策，不再拆 PR。
