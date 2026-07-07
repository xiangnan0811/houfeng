# 前端视觉重构与统计卡片统一 - Implementation Plan

## Current Implementation State

- [x] 新增 `StatCard` atom 与 `StatCard.test.tsx`。
- [x] 新增 `SectionTitle` atom 与 `SectionTitle.test.tsx`。
- [x] 更新 `atoms/index.ts` barrel export。
- [x] 重构 `DashboardPage.tsx` 为 atom-based 概览布局。
- [x] 迁移 `EventsPage.tsx`、`TargetsPage.tsx`、`MonitoringHero.tsx` 到 `StatCard`。
- [x] 新增 `AppBoot.tsx` 并在 `main.tsx` 挂载。
- [x] 更新 `index.css` 中 Dashboard / stat / boot animation 相关样式。
- [x] 更新 Dashboard 测试选择器。
- [x] 补充 CDP/Chromium browser sanity 规则到项目规范。

## Verification Already Run

- [x] `make verify-web`
  - lint passed
  - Vitest: 73 files / 570 tests passed
  - `tsc -b && vite build` passed
  - Note: environment warning remains because current Node is `v24.18.0`, while `web/package.json` requires `22.x`.
- [x] `git diff --check`
- [x] Browser sanity via local CDP/Chromium evidence supplied by user:
  - Dashboard renders normally.
  - No error state / abnormal render failure.
  - `dashAtt:5`
  - `dashAttClickable:2`

## Final Review Checklist

- [x] Before commit, re-run `git status --short --branch` and verify task-owned paths.
- [x] Re-run `make verify-web` if any web source changes after this point.
- [x] Confirm `.trellis/spec/web/quality-guidelines.md` and `docs/operations/ui-preview-and-browser-sanity.md` are intentional small spec/doc updates, not a separate Trellis task.
- [ ] Archive this task only after the branch work is committed or the user explicitly chooses to leave it unarchived.

## Rollback Notes

- If `StatCard` causes cross-page visual regression, revert page migrations first while keeping atom files isolated.
- If Dashboard click semantics regress, inspect `.dash-att--clickable` usage; do not put cursor/hover back on base `.dash-att`.
- If animation suppression breaks first-load motion, revert `AppBoot.tsx`, its `main.tsx` mount, and `.app-booted .animate-in` CSS together.
