# Journal - xiangnan-mac (Part 1)

> AI development session journal
> Started: 2026-05-02

---



## Session 1: Bootstrap trellis: backend & web spec

**Date**: 2026-05-02
**Task**: Bootstrap trellis: backend & web spec
**Branch**: `main`

### Summary

Replaced omc/superpowers with trellis. Filled .trellis/spec/ backend (5 files) and added web layer (5 files + index), each citing real code paths. Surfaced 12 CLAUDE.md vs code gap items for v1-gap-checklist (e.g. worker count, ProbeKind, double fetch wrappers, make verify-web missing lint).

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `aed6a65` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 2: Docs audit + T1 archive (V1 收口 stage)

**Date**: 2026-05-02
**Task**: Docs audit + T1 archive (V1 收口 stage)
**Branch**: `main`

### Summary

Started docs-roadmap parent (Stage 1/2/3 framing, V1 收口 direction, V1 != MVP) + scaffolded 3 children (T1 docs-audit-cleanup, T2 roadmap-and-claude-md, T3 spec-sync). Completed T1: 82 files git mv to docs/_archive/<mirror>/, audit report 320 lines, D-class unintended links 0. Accumulated for follow-ups: README.md 6 stale refs (T2), gap-checklist Closed status severely outdated since user判定 实现连 V0.1 都不到 (T3).

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `9e8c3c0` | (see git log) |
| `882d89c` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 3: T3 spec-sync: align spec authority + merge V1 gap items

**Date**: 2026-05-02
**Task**: T3 spec-sync: align spec authority + merge V1 gap items
**Branch**: `main`

### Summary

Completed T3: replaced authority-source line in 11 .trellis/spec/*.md (5 backend + 5 web + web/index.md) with unified clause (CLAUDE.md > v1-baseline frozen subset > v2-houfeng). Merged 12 newly-found gap items (7 backend + 5 web) into docs/release/v1-gap-checklist.md as new section. Tagged all 42 existing Closed rows with (⚠️ need-reassess) + added top-of-file banner explaining 2026-04-30 vs 2026-05-02 mismatch (实现连 V0.1 都不到). Per-row Closed reassessment deferred to T2 next-phase plan.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `103b23d` | (see git log) |
| `64d7a87` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 4: T2 roadmap + CLAUDE.md/README revision; docs-roadmap workstream complete

**Date**: 2026-05-02
**Task**: T2 roadmap + CLAUDE.md/README revision; docs-roadmap workstream complete
**Branch**: `main`

### Summary

Completed T2 (roadmap-and-claude-md): drafted docs/release/next-phase-plan.md (131 lines, mid-grain Stage 1 V1 收口 with P0/P1/P2 + Stage 2/3 placeholders). Targeted-rewrote CLAUDE.md (3 sections + 8 minimal patches: worker count, handler list, subpackages, ProbeKind, visual authority -> v2-houfeng). Targeted-rewrote README.md (3 sections, 6 stale refs cleared). Minimal-patched v1-baseline/README.md (Line 35 frozen-completion wording softened + doc-nav archive note). Also archived parent docs-roadmap (3 children all done). docs-roadmap workstream complete.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `d23071f` | (see git log) |
| `5927994` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 5: Stage 1 P0 quickwins: 3 gaps + lint baseline

**Date**: 2026-05-02
**Task**: Stage 1 P0 quickwins: 3 gaps + lint baseline
**Branch**: `main`

### Summary

Stage 1 P0 quickwins task complete. Fixed 3 gaps from V1 收口期 gap list: (gap #12) Makefile verify-web now runs npm run lint; (gap #7) cmd/houfeng-center/main.go unified to log/slog with fatal() helper; (gap #3) docs-only — confirmed migration 0004 collision cannot be renamed (schema_migrations uses filename as primary key per migrate.go:16-19), updated v1-gap-checklist.md and next-phase-plan.md to record convention 'next migration starts at 0011'. Scope-extended mid-task to fix 4 baseline lint errors (auth-context.tsx + theme-context.tsx, inline disable comments with rationale per early-stage Provider+hook colocation convention) — pre-requisite for gap #12. make verify-web fully green end-to-end (lint -> test 268 -> build).

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `75d4034` | (see git log) |
| `a613f8e` | (see git log) |
| `6a52ced` | (see git log) |
| `1704c02` | (see git log) |
| `bc7f5b2` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 6: Reassess gap-checklist Closed batch 1 (Product/arch + Core model)

**Date**: 2026-05-02
**Task**: Reassess gap-checklist Closed batch 1 (Product/arch + Core model)
**Branch**: `main`

### Summary

Reassessed 9 Closed (⚠️ need-reassess) rows in v1-gap-checklist.md first two sections (Product/architecture baseline 4 rows + Core object model 5 rows): 8 verified Closed + 1 Partial (Row 5 Node persistence and UI, due to NodesPage.tsx:60 createNode bypassing lib/api.ts -- already gap #10) + 0 Not Closed + 0 inconclusive. Key reframe: foundational layers are largely aligned with v1-baseline design; user judgment '实现连 V0.1 都不到' likely originates in UI / runtime end-to-end / notification delivery (batch 2/3/4 focus). Verdict scheme: 4 tiers (Closed verified / Partial / Not Closed / Reassess inconclusive) written to Status column + Reassessed 2026-05-02 evidence note appended. Subsequent 33 rows still tagged need-reassess for batch 2/3/4.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `dfa32fc` | (see git log) |
| `6777c32` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 7: Reassess gap-checklist Closed batch 2 (Runtime + Notifications)

**Date**: 2026-05-02
**Task**: Reassess gap-checklist Closed batch 2 (Runtime + Notifications)
**Branch**: `main`

### Summary

Reassessed 8 Closed (⚠️ need-reassess) rows in v1-gap-checklist.md Runtime behavior (6) + Notifications (2). All 8 verdict = Closed (verified 2026-05-02), 0 Partial / Not Closed / Inconclusive. Cumulative batch 1+2 = 17 rows: 16 Closed verified + 1 Partial (NodesPage createNode reuse of gap #10). Reframe: foundational + runtime + notification layers substantively aligned with design — user 实现连 V0.1 都不到 judgment must originate in UI / e2e / delivery hardening (batch 3/4 focus). Live Telegram delivery (Partial) untouched.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `3f7cca9` | (see git log) |
| `c4ecc3f` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 8: Reassess gap-checklist Closed batch 3 (UI surfaces) - found root cause

**Date**: 2026-05-02
**Task**: Reassess gap-checklist Closed batch 3 (UI surfaces) - found root cause
**Branch**: `main`

### Summary

Reassessed 8 Closed (⚠️ need-reassess) UI rows. 5 verified + 3 Partial (NodesPage / TargetsPage / EventsPage list filter incompleteness, all per §6.3/§6.4 design vs current code). Critical finding: this is the most plausible source of user judgment '实现连 V0.1 都不到' -- the list-filter pattern across 3 pages is materially missing. Backend + runtime + notifications are aligned; UI list-filter completion is the gap. Cumulative batch 1+2+3 = 25 rows: 21 verified + 4 Partial. Visual screenshot comparison row untouched. Recommend opening Stage 1 P1 follow-up tasks for the 3 Partial rows after batch 4.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `f64c431` | (see git log) |
| `f09bdf7` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 9: Reassess gap-checklist Closed batch 4 (Delivery + Auth + V1.x visual) - workstream complete

**Date**: 2026-05-02
**Task**: Reassess gap-checklist Closed batch 4 (Delivery + Auth + V1.x visual) - workstream complete
**Branch**: `main`

### Summary

FINAL batch 4 of 4 reassess. 17 rows (Delivery 5 + Auth 4 + V1.x visual 8) all Closed verified. V1.x visual rows include supersession note (implementations preserved by v2-houfeng reuse; v1.x docs archived). Cumulative 42 rows: 38 verified + 4 Partial. The 4 Partial rows are: NodesPage createNode bypass (gap #10) + Nodes list filters missing 5/7 + TargetsPage zero filter UI + EventsPage filter incompleteness. ROOT CAUSE LOCATED: user judgment '实现连 V0.1 都不到' is entirely from front-end list-page filter incompleteness, not backend / runtime / notifications / delivery / auth / visual.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `dbdb383` | (see git log) |
| `227537d` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 10: Reframe next-phase-plan + gap-checklist banner with reassess root cause

**Date**: 2026-05-02
**Task**: Reframe next-phase-plan + gap-checklist banner with reassess root cause
**Branch**: `main`

### Summary

Captured gap-reassess workstream root-cause finding into next-phase-plan.md (new top-priority Stage 1 P0 work item 'Front-end list-page filter completion' + new Reassess findings section) and into v1-gap-checklist.md banner (L5/L7 replaced; banner now reflects reassess completion = 38 verified + 4 Partial = root cause = list-page filters). Sub-agent identified and fixed an internal banner inconsistency (L7 vs L5) mid-task. Stage 1 P0 list now leads with list-filter completion work.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `2ff1670` | (see git log) |
| `4cbbed9` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 11: Build shared FilterBar + apply to TargetsPage (child 1 of list-filter-completion)

**Date**: 2026-05-02
**Task**: Build shared FilterBar + apply to TargetsPage (child 1 of list-filter-completion)
**Branch**: `main`

### Summary

Child 1 complete: introduced web/src/components/filters/ with 5 reusable components (FilterBar / FilterSelect / FilterMultiSelect / FilterToggle / FilterChip) + 13 unit tests + token-driven CSS. Applied to TargetsPage with §6.4 6 filters (类型 / 运行状态 / 健康状态 / 标签 / 执行节点标签 / 仅看异常); client-side filter via useMemo, URL query string state via useSearchParams. 285 web tests pass, make verify-web fully green. TargetsPage business behavior preserved. Sets the pattern for child 2 (NodesPage) + child 3 (EventsPage).

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `05cb274` | (see git log) |
| `7cbf8d6` | (see git log) |
| `746994c` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 12: Apply FilterBar to NodesPage (child 2 of list-filter-completion)

**Date**: 2026-05-02
**Task**: Apply FilterBar to NodesPage (child 2 of list-filter-completion)
**Branch**: `main`

### Summary

Child 2 complete: NodesPage 8 control FilterBar (7 §6.3 filters; 地区/城市 拆 2 select). Reused child 1's filters/ components and pattern (URL query string + client-side useMemo). NodesPage business preserved (createNode still has gap #10 follow-up; runtime actions / binding-conflict view / onboarding all intact). 287 web tests pass, make verify-web fully green. EventsPage child 3 remains; then list-filter-completion parent archive.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `896b22e` | (see git log) |
| `43af18b` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 13: EventsPage 4 missing features + list-filter-completion workstream complete

**Date**: 2026-05-02
**Task**: EventsPage 4 missing features + list-filter-completion workstream complete
**Branch**: `main`

### Summary

Final child 3 + parent archived. EventsPage 4 features: (1) 含 backfill toggle UI-only forward-compat (backend no is_backfilled column today; toggle wired but no-op until backend supports); (2) Time segmented Tabs (24h/7d/30d/自定义); (3) client-side time grouping today/yesterday/this week/earlier; (4) server-side load-more via incrementing limit + exhausted state. 289 web tests pass; make verify-web green. list-filter-completion workstream complete: 3 children + parent all archived. NodesPage / TargetsPage / EventsPage list-filter root cause closed; '实现连 V0.1 都不到' user judgment addressed at the V1 收口 layer.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `88c1c24` | (see git log) |
| `3398109` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 14: V1 live PostgreSQL smoke run + Stage 1 P0 complete

**Date**: 2026-05-02
**Task**: V1 live PostgreSQL smoke run + Stage 1 P0 complete
**Branch**: `main`

### Summary

Stage 1 P0 last item complete. Live smoke against 192.168.100.192:5432/houfeng: 6 PASS + 1 PARTIAL (agent macOS /proc/loadavg incompatibility) + 1 INCONCLUSIVE (center root 404, HOUFENG_WEB_DIST_DIR unset). Incident detection 3m25s, recovery 2m05s. 4 new gap candidates surfaced (enrollment-token key name, agent macOS, center root no SPA, /api/events bare array). docs/operations/v1-smoke-run.md updated with new evidence sub-table + dated section + caveats. ENTIRE Stage 1 P0 NOW COMPLETE.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `efaaa72` | (see git log) |
| `6394b29` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete
