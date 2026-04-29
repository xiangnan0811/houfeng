# Houfeng V1 Visual Verification

## Authority

The only visual authority for V1 is the Unified / Baseline Stitch export under `docs/design/v1-baseline/`:

- `docs/design/v1-baseline/handoff.md`
- `docs/design/v1-baseline/ui-ux-spec.md`
- `docs/design/v1-baseline/visual-review-round2.md`
- `docs/design/v1-baseline/baseline-screens.md`
- `docs/design/v1-baseline/stitch/**/screen.png`

Legacy concept screens are retained for history and must not override the current baseline.

## Primary baseline coverage

| Baseline screen | Reference image | Implementation route | V1 status |
| --- | --- | --- | --- |
| Global App Shell Baseline | `docs/design/v1-baseline/stitch/global_app_shell_baseline_obsidian_core/screen.png` | all app routes through `web/src/app/layout/AppShell.tsx` | Requires screenshot comparison |
| Global Control Center (Unified) | `docs/design/v1-baseline/stitch/global_control_center_unified/screen.png` | `/` | Requires screenshot comparison |
| Fleet Nodes List | `docs/design/v1-baseline/stitch/fleet_nodes_list/screen.png` | `/nodes` | Requires screenshot comparison |
| Node Detail Center (Unified) | `docs/design/v1-baseline/stitch/node_detail_center_unified/screen.png` | `/nodes/:nodeId` | Requires screenshot comparison with seeded data |
| Node Onboarding & Binding Conflict (Unified) | `docs/design/v1-baseline/stitch/node_onboarding_binding_conflict_unified/screen.png` | `/nodes/:nodeId/onboarding` | Requires screenshot comparison with seeded conflict state |

## Supporting coverage

| Baseline screen | Reference image | Implementation route | V1 status |
| --- | --- | --- | --- |
| Security Audit & Events | `docs/design/v1-baseline/stitch/security_audit_events/screen.png` | `/events` | Requires screenshot comparison |
| Global Logs Explorer | `docs/design/v1-baseline/stitch/global_logs_explorer/screen.png` | `/events` as event explorer surface | Supporting reference only |
| System Configuration | `docs/design/v1-baseline/stitch/system_configuration/screen.png` | `/settings` | Requires screenshot comparison |
| Target Detail | `docs/design/v1-baseline/stitch/target_details_blog.example.com/screen.png` | `/targets/:targetId` | Legacy-but-usable reference until a unified target detail baseline exists |

## Reproducible capture path

Build and run the app:

```bash
cd web && npm ci && npm run build
cd ..
export HOUFENG_HTTP_ADDR=:8080
export HOUFENG_WEB_DIST_DIR=web/dist
export HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@localhost:5432/houfeng?sslmode=disable'
./bin/houfeng-center
```

Capture screenshots with a browser automation tool at viewport `1440x1024` after seeding smoke data:

```bash
mkdir -p docs/operations/visual-evidence
# Capture route screenshots for:
# /, /nodes, /nodes/<node_id>, /nodes/<node_id>/onboarding,
# /targets/<target_id>, /events, /settings
```

Compare each captured screenshot with the matching reference PNG. Record results in this table:

| Route | Captured screenshot | Reference screenshot | Verdict | Notes |
| --- | --- | --- | --- | --- |
| `/` | `docs/operations/visual-evidence/dashboard.png` | `docs/design/v1-baseline/stitch/global_control_center_unified/screen.png` | Pending live capture | Requires seeded data |
| `/nodes` | `docs/operations/visual-evidence/nodes.png` | `docs/design/v1-baseline/stitch/fleet_nodes_list/screen.png` | Pending live capture | Requires seeded node |
| `/nodes/<node_id>` | `docs/operations/visual-evidence/node-detail.png` | `docs/design/v1-baseline/stitch/node_detail_center_unified/screen.png` | Pending live capture | Requires runtime facts/incidents |
| `/nodes/<node_id>/onboarding` | `docs/operations/visual-evidence/node-onboarding.png` | `docs/design/v1-baseline/stitch/node_onboarding_binding_conflict_unified/screen.png` | Pending live capture | Requires onboarding or conflict state |
| `/events` | `docs/operations/visual-evidence/events.png` | `docs/design/v1-baseline/stitch/security_audit_events/screen.png` | Pending live capture | Requires event stream |
| `/settings` | `docs/operations/visual-evidence/settings.png` | `docs/design/v1-baseline/stitch/system_configuration/screen.png` | Pending live capture | Requires settings page |
| `/targets/<target_id>` | `docs/operations/visual-evidence/target-detail.png` | `docs/design/v1-baseline/stitch/target_details_blog.example.com/screen.png` | Pending live capture | Legacy reference |

## Current evidence status

This document records the authoritative comparison set and reproducible capture path. If no `docs/operations/visual-evidence/*.png` files are committed, visual verification remains a tracked evidence gap rather than completed visual proof.

## Read-only visual inspection findings

The current frontend routes and tests cover the V1 page family, but screenshot evidence has not yet been captured. A read-only inspection against the Unified / Baseline references identifies these visual-alignment risks to verify before release:

- The app shell is functionally present, but current `web/src/app/layout/AppShell.tsx` is materially simpler than the frozen shell baseline. It needs screenshot comparison for left navigation, top header, density, and surface hierarchy.
- The dashboard has the required operational data surfaces, but its current composition is summary-card/list oriented. The Unified dashboard baseline has stronger anomaly, topology, and danger-zone hierarchy.
- Node detail is semantically close, but the baseline emphasizes “current issue first, trend second” in a tighter control-center composition. Current section ordering needs visual review with seeded incident/trend data.
- Node onboarding has the right domain workflow, but any remaining English UI fragments should be treated as a Chinese-first V1 language risk.
- Events and Settings are functional but need comparison against the denser supporting Security Audit / Logs Explorer / System Configuration references.
- `/targets` has no dedicated frozen screen. It should be verified through the shared shell rules and target-detail reference until a future baseline adds a dedicated target-list screen.

These findings are not design changes. They are implementation verification risks against the existing frozen baseline.
