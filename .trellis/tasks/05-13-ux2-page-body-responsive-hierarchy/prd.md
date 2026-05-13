# UX-2 Page Body Responsive Hierarchy Baseline

## Goal

在 UX-1 壳层基线之后，把核心页面主体从“桌面页面压缩到移动端”推进到稳定、可扫描、可长期使用的响应式产品基线。重点不是重做业务功能，而是统一 page header、support surface、filter / tabs / table 区域在桌面与 390px 窄屏下的层级、密度和溢出行为。

## What I Already Know

- 用户已同意从上一轮 UX-1 之后继续推进下一阶段 UI 演进。
- UX-1 已合并到 `main`：App shell / Sidebar / TopBar / Breadcrumb / mobile nav 已有可用基线。
- UX-1 视觉证据暴露的下一层问题在 page body：移动端页面主体仍容易被 inline header、统计块、筛选区和表格 min-width 撑开。
- `docs/release/ui-evolution-roadmap.md` 当前仍把 UX-2 写成 Dashboard polish；本任务按用户刚确认的新排序执行：先做 page-body responsive hierarchy，再回到单页深度 polish。
- 本轮不改变后端 API、业务 contract、真实数据导入流程或页面信息架构的大方向。
- 视觉权威仍是 `docs/design/v2-houfeng/design-language.md` 与 `docs/design/v2-houfeng/component-spec.md`。

## Requirements

- 核心路由在 `1440x1000` 下保持高密度工作台气质，首屏能看到页面主体任务，而不是被 header / filter / chrome 占满。
- 核心路由在 `390x900` 下不能出现页面级横向溢出、文本互相遮挡、按钮文字挤出、badge 覆盖文字或 support surface 裁切。
- 大表格可以在局部 table surface 内横向滚动，但不能把整个 document 撑宽；表格前的 header / filters / support panels 必须在视口内换行或收敛。
- 统一修复优先：优先改 `page-panel`、`section-heading`、asset page、observability support、filter bar、tabs、table wrapper 等共享规则；只有页面确有特殊结构时才加局部 class。
- `/vps`、`/asset-decisions`、`/nodes`、`/targets`、`/events`、`/settings` 是本轮主要验收页面；`/` 只做回归视觉 sanity，不做 Dashboard command surface 深度重排。
- 保持现有 URL-state、drawer、filter chip、DataTable、API helper、测试 contract 不变。
- 如果路线文档仍指向旧 UX-2，应同步更新 docs，让后续执行顺序不再漂移。

## Acceptance Criteria

- [x] `docs/release/ui-evolution-roadmap.md` 反映 UX-2 的实际执行目标或明确记录顺序调整。
- [x] Shared page primitives 在窄屏下有稳定规则：page panel padding 收敛，inline heading 切单列，actions 全宽/换行，tabs/filter bar 不撑宽 document。
- [x] `/vps` 390px：header、创建入口、quick views、filter chips、库存表入口无页面级横向溢出；table 横向滚动只发生在 table surface 内。
- [x] `/asset-decisions` 390px：summary、决策队列、renewal evidence 入口无裁切；队列行信号区切单列或两列时不压缩文字。
- [x] `/nodes`、`/targets`、`/events` 390px：support surface、filter overview、hero/heading 无裁切；主要按钮仍可触达。
- [x] `/settings` 390px：配置 section、channel selector、form grid 不产生页面级横向溢出。
- [x] `1440x1000` 桌面 sanity：页面主体仍保持高密度和清晰主次，不因移动端规则退化成过宽留白或低密度卡片墙。
- [x] Local validation passes: `cd web && npm run lint`, `cd web && TMPDIR=$PWD/.tmp npm run test -- --run`, `cd web && npm run build`.
- [x] Visual sanity performed for `/`, `/vps`, `/asset-decisions`, `/nodes`, `/targets`, `/events`, `/settings` at `1440x1000` and `390x900`.

## Definition Of Done

- Implementation and tests/docs are committed on `ux2-page-body-responsive-hierarchy`.
- Trellis task is archived and journal recorded after verification.
- PR is created, PR CI is green, PR is merged, main CI is monitored, and local `main` is synced.

## Out Of Scope

- No backend/API changes.
- No real 40+ VPS import.
- No full Dashboard command surface redesign.
- No complete mobile card replacement for every table.
- No page-file component splitting unless required to fix a concrete layout issue.
- No new UI framework, browser automation dependency, chart library, or visual regression CI.
- No Provider/DNS/Web SSH/plugin/RBAC/FX-rate/product expansion.

## Technical Notes

- UI route authority: `docs/release/ui-evolution-roadmap.md`, updated by this task if needed.
- Visual authority: `docs/design/v2-houfeng/design-language.md`, `docs/design/v2-houfeng/component-spec.md`.
- Visual evidence workflow: `docs/operations/v2-visual-evidence.md`.
- Web specs: `.trellis/spec/web/{index,styling-guidelines,component-conventions,state-and-data,quality-guidelines}.md`.
- Expected primary style file: `web/src/styles/pages.css`.
- Vitest on this macOS checkout should use repo-local temp dir: `cd web && TMPDIR=$PWD/.tmp npm run test -- --run`, then remove `web/.tmp`.
