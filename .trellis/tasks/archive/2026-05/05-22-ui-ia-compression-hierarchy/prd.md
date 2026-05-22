# frontend IA compression and hierarchy pass

## Goal

修正上一轮前端 IA 优化不达标的问题：在仍然只修改 `web/src`、不改变后端/API/路由/依赖的前提下，对 Dashboard、Nodes、VPS、Asset Decisions 做一次更严格的密度审计和层级收敛，让页面少一点发光、少一点渐变、少一点解释，多一点层级、多一点节奏、多一点工作流。

## What I already know

- 用户明确认为上一轮结果不达标；“页面仍有大量解释文案”只是一个例子，不是唯一问题。
- 正确方向是：少一点发光，少一点渐变，少一点解释；多一点层级，多一点节奏，多一点工作流。
- 范围沿用上一轮边界：只改 `web/src`；不改后端、不改 API 调用合同、不改路由、不引入大型依赖。
- 重点页面仍是 Dashboard、Nodes、VPS、Asset Decisions；必要时可改其 `web/src/pages/*` 子组件、测试和现有 CSS。
- 质量门槛必须从“元素已移动/可用”升级为“首屏读起来更短、主路径更明显、次级解释更少”。

## Requirements

- 对四个页面做逐页密度审计：删除或压缩不直接支撑当前决策的解释句、重复说明和长段落。
- Dashboard：继续保留 `今日第一步` 主叙事，但压缩 command surface 内的说明文字，降低刷新/自动刷新/次级动作的存在感，避免首屏像说明书。
- NodesPage：进一步降低 Hero、SupportSurface、Toolbar 之间重复解释；quick view 应成为清晰扫描入口，批量/趋势/刷新/高级筛选继续作为次级控制。
- NodesSupportSurface：保留资产判断支撑语义，但文案必须短，lane 数量和解释密度继续收敛；避免把“为什么”铺满首屏。
- VPSPage：保留 quick view + 高级筛选 Drawer 模式，压缩资产资料质量、订阅证据、筛选说明等非必要解释，强化库存核对的工作流节奏。
- AssetDecisionsPage：保留优先级队列主视觉，继续弱化证据边界和续费候选说明；处理动作必须比解释更显眼。
- CSS：继续减少残余 glow、强渐变、装饰性 overlay 和 hover 浮动；使用现有 tokens/CSS variables，不硬编码大批新颜色。
- 保持可访问标签、测试可定位文本和现有交互能力；如文案压缩导致测试变更，更新测试断言而非保留冗余文案。

## Acceptance Criteria

- [ ] Dashboard、Nodes、VPS、Asset Decisions 每页首屏的长解释句明显减少，主 CTA / 主队列 / quick view 更先被看到。
- [ ] 每个重点页面仍遵循“一页一个主叙事、一个主工作流入口”，次级动作不与主路径竞争。
- [ ] NodesPage 不再同时用 Hero、SupportSurface、Toolbar 重复解释同一组异常/接入/维护事实；重复说明被删减或降级。
- [ ] AssetDecisionsPage 中证据边界和候选证据仍可访问，但不抢占主队列视觉。
- [ ] CSS 中残余强 glow、强渐变、装饰性 overlay 进一步弱化；视觉仍保持候风暗色服务器工作台气质。
- [ ] 不修改后端、不修改 API 调用语义、不新增大型依赖、不改变路由结构。
- [ ] 受影响页面测试更新并通过；`make verify-web` 通过。
- [ ] 使用本地浏览器检查四个页面桌面与窄屏首屏，确认解释文案密度和视觉装饰已明显下降。

## Definition of Done

- Tests added/updated where text, roles, or visibility changed.
- `make verify-web` passes.
- Browser sanity covers Dashboard、Nodes、VPS、Asset Decisions on desktop and narrow viewport.
- Any reusable IA rule learned from this correction is reflected in Trellis web spec if not already covered.

## Out of Scope

- Backend/API/DB changes.
- Route structure changes.
- New UI framework, CSS-in-JS, charting, or large dependency.
- Full redesign or component library rewrite.
- Mobile-first rewrite; narrow viewport only needs preserve usable workflow scanning.
- Changing product semantics or inventing new data fields.

## Technical Approach

1. Audit four pages for visible explanatory copy, repeated helper text, decorative CSS, and competing CTAs.
2. Convert non-decision explanations into short labels, terse helper text, collapsible secondary info, or remove them entirely.
3. Tighten CSS rhythm using existing tokens: clearer section spacing, quieter surfaces, less decoration, stronger hierarchy through layout and typography rather than glow/gradient.
4. Update tests to assert the new compact IA and preserve behavior.
5. Run verification and browser sanity.

## Decision (ADR-lite)

**Context**: The previous IA pass improved structure but did not meet the user's quality bar because explanatory copy and decorative noise remained too high.

**Decision**: Treat this as a correction pass focused on density, hierarchy, rhythm, and workflow. Do not ask another preference question; the user already supplied the design direction.

**Consequences**: Some existing Chinese copy and test assertions will change. The UI may become less self-explanatory for first-time readers, but more operator-focused and closer to a real fleet workbench.

## Technical Notes

- Current branch: `feat/ui-ia-phase-2`.
- Previous phase-two work commit: `4ff1e02 feat: refine frontend workflow information architecture`.
- Relevant files likely include:
  - `web/src/pages/DashboardPage.tsx`
  - `web/src/pages/dashboard/DashboardCommandSurface.tsx`
  - `web/src/pages/NodesPage.tsx`
  - `web/src/pages/nodes/*`
  - `web/src/pages/VPSPage.tsx`
  - `web/src/pages/AssetDecisionsPage.tsx`
  - `web/src/styles/pages.css`
  - affected `*.test.tsx`
