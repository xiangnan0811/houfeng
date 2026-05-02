# Houfeng V1 Stitch Baseline Screens

> Copy target: `docs/design/v1-baseline/stitch/baseline-screens.md`

## Purpose

This file defines the **current authoritative Stitch screen baseline** for Houfeng V1.
Use it to prevent implementation from mixing:

- current unified baseline screens
- older concept screens
- archived exploratory screens

This file should be read together with:

- `../handoff.md`
- `../ui-ux-spec.md`
- `../visual-review-round2.md`

---

## Project Identity

- Product name: **候风**
- English name: **Houfeng**
- Full name: **Houfeng Fleet Control Plane**
- Stitch project: `projects/11890404311726538701`

---

## Design System Anchor

Primary design-system reference:

- `./obsidian_core/DESIGN.md`

This file defines the tonal system, dark-first direction, surface hierarchy, accent usage, and component styling philosophy.

---

## How to Use This File

### Rule 1
Classify screens in **two steps**, not one:

1. **Generation / lineage**
   - current optimized generation
   - older exploratory generation
2. **Implementation role**
   - primary baseline
   - supporting reference
   - historical / archive

### Rule 2
Only screens in **Current Generation / Primary Baseline** are authoritative for implementation decisions.

### Rule 3
Screens in **Current Generation / Supporting Screens** are still valid current references, but they do **not** override the primary baseline.

### Rule 4
Screens in **Legacy / Historical Screens** are retained for history only, unless a page has no newer dedicated replacement yet.

### Rule 5
If there is any conflict, use this priority order:

1. `handoff.md`
2. `ui-ux-spec.md`
3. `visual-review-round2.md`
4. `baseline-screens.md`
5. archived screens

---

## Current Generation / Primary Baseline Screens

These are the screens that should drive implementation first.

### 1. Global App Shell Baseline
- Stitch screen: `projects/11890404311726538701/screens/e5b6094d068546b2bc23603b57c2e46f`
- Local screenshot: `./global_app_shell_baseline_obsidian_core/screen.png`
- Local HTML: `./global_app_shell_baseline_obsidian_core/code.html`
- Role:
  - authoritative app shell
  - left navigation baseline
  - top header baseline
  - content slot order baseline
- Use this when implementing:
  - global layout
  - page frame
  - shell-level spacing and hierarchy

### 2. Global Control Center (Unified)
- Stitch screen: `projects/11890404311726538701/screens/f7b6b6e62eb248e78e9d614b83cef182`
- Local screenshot: `./global_control_center_unified/screen.png`
- Local HTML: `./global_control_center_unified/code.html`
- Role:
  - authoritative home/dashboard baseline
  - current global health + anomalies overview reference
- Use this when implementing:
  - dashboard page
  - current risk overview
  - event stream placement

### 3. Fleet Nodes List
- Stitch screen: `projects/11890404311726538701/screens/c0b5d59a1e734e078ad7a5a229501010`
- Local screenshot: `./fleet_nodes_list/screen.png`
- Local HTML: `./fleet_nodes_list/code.html`
- Role:
  - authoritative nodes list baseline
  - row density and list hierarchy baseline
- Use this when implementing:
  - node list page
  - filter and row layout
  - list density and summary fields

### 4. Node Detail Center (Unified)
- Stitch screen: `projects/11890404311726538701/screens/c235cf6ebedd4e478b55d16e0f53aec5`
- Local screenshot: `./node_detail_center_unified/screen.png`
- Local HTML: `./node_detail_center_unified/code.html`
- Role:
  - authoritative node detail baseline
  - current issue first, trend second
- Use this when implementing:
  - node detail page
  - summary cards
  - active incidents area
  - event stream area

### 5. Node Onboarding & Binding Conflict (Unified)
- Stitch screen: `projects/11890404311726538701/screens/7cd216d3585746b895180169bce1bbc2`
- Local screenshot: `./node_onboarding_binding_conflict_unified/screen.png`
- Local HTML: `./node_onboarding_binding_conflict_unified/code.html`
- Role:
  - authoritative onboarding / binding conflict baseline
  - special-state page baseline
- Use this when implementing:
  - node enrollment page
  - binding conflict handling page
  - dangerous action hierarchy in stateful pages

---

## Current Generation / Supporting Screens

These screens are still part of the newer optimized generation, but they are not the first-line implementation anchors.

### 6. Security Audit & Events
- Stitch screen: `projects/11890404311726538701/screens/e5b75de584d44c898f9e8fda110b6cd6`
- Local screenshot: `./security_audit_events/screen.png`
- Local HTML: `./security_audit_events/code.html`
- Role:
  - supporting reference for event-heavy secondary pages
- Note:
  - shell, typography hierarchy, and spacing should still follow the primary baseline

### 7. Global Logs Explorer
- Stitch screen: `projects/11890404311726538701/screens/4e7ccd646f884e8f89dde84c40efb711`
- Local screenshot: `./global_logs_explorer/screen.png`
- Local HTML: `./global_logs_explorer/code.html`
- Role:
  - supporting reference for log / explorer style secondary views
- Note:
  - should still inherit the primary shell and visual hierarchy

### 8. System Configuration
- Stitch screen: `projects/11890404311726538701/screens/2076954cf395463db4c8ad9cd4be4577`
- Local screenshot: `./system_configuration/screen.png`
- Local HTML: `./system_configuration/code.html`
- Role:
  - supporting reference for settings/configuration pages
- Note:
  - should still inherit the primary shell and visual hierarchy

---

## Legacy but Still Usable Reference

These screens are from the earlier generation, but may still be consulted if a page does not yet have a newer dedicated unified replacement.

### 9. Target Detail
- Stitch screen: `projects/11890404311726538701/screens/afaf842d60e14971a7574889be4db62c`
- Local screenshot: `./target_details_blog.example.com/screen.png`
- Local HTML: `./target_details_blog.example.com/code.html`
- Role:
  - provisional target detail content reference
- Note:
  - there is not yet a dedicated “target detail unified” shell artifact
  - implement target detail by combining:
    - `Global App Shell Baseline`
    - this target detail content reference
    - `ui-ux-spec.md` hierarchy rules
  - if a future unified target-detail screen is produced, this screen should be downgraded to history

---

## Legacy / Historical Screens

These are retained for design history and traceability only.
Do not use them as the current implementation baseline.

- `./fleet_control_plane_dashboard/`
- `./node_details_nd_us_east_04a/`
- `./node_onboarding_binding_conflict/`

These screens may still be useful for:
- understanding evolution
- mining secondary ideas
- identifying previously rejected patterns

But they must not override the current primary baseline.

---

## Practical Implementation Guidance

When starting implementation in a new Codex session:

1. Read `../handoff.md`
2. Read `../ui-ux-spec.md`
3. Read `../visual-review-round2.md`
4. Read this file
5. Use only the Current Generation / Primary Baseline Screens as implementation anchors
6. Use Current Generation / Supporting Screens only when they do not conflict with the primary baseline
7. Use Legacy but Still Usable Reference only when a page has no newer dedicated replacement
8. Treat Legacy / Historical Screens as traceability material only

---

## One-line Rule

> Implement from the current optimized baseline, not from the full export history.
