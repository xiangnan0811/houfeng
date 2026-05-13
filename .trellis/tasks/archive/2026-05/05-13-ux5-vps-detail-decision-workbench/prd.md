# UX-5 VPS Detail Decision Workbench

## Background

UX-4 improved the asset decision queue and VPS inventory real-data path. The next step is the single-VPS follow-through page: after a user chooses one VPS from a list or queue, the detail page must make the renewal and lifecycle decision easier, not just expose every editable field.

The existing detail page already has the data contracts needed for this batch:

- VPS identity, lifecycle, usage, current decision, facts, connection summary, and linked Node evidence via `GET /api/vps/{id}`.
- Timeline via `GET /api/vps/{id}/timeline`.
- VPS-scoped services and domains via existing manual asset APIs.
- VPS-scoped subscription lookup via `listSubscriptions({ vps_id, sort: 'renew_at', order: 'asc' })`.
- Existing edit, link, service, domain, experience log, archive, and restore flows.

## Goal

Turn `VPSDetailPage` into a single-asset decision workbench that answers, in the first scan, whether this VPS should be kept, observed, migrated, cancelled, or supplemented with better evidence.

## Non-Goals

- No backend API, database schema, authentication, or RBAC changes.
- No new front-end state library, table library, chart library, design system dependency, or routing model.
- No service discovery, service registry, DNS provider sync, DNS record management, registrar sync, certificate probing, Web SSH, or provider import.
- No complex scoring algorithm. Any decision framing must remain transparent and derived from already visible facts.
- No change to the contract that linked Node health appears on the VPS detail page, while the VPS list remains limited to linked-node counts and missing-link evidence.

## Requirements

### Decision-first hierarchy

- The first viewport must prioritize identity, lifecycle, current decision, subscription/cost evidence, Node evidence, data quality, services/domains, and recent timeline signals.
- `VPSDecisionWorkbench` should be the primary decision surface, not a secondary summary. It must present the decision, the strongest supporting/weakening evidence, and the next recommended operator action in a compact, scannable structure.
- Basic facts, connection details, and long histories remain available but should not dominate the first scan path.
- Node, service, domain, and timeline sections appear as decision evidence/context for the VPS, not competing page subjects.

### Subscription evidence

- The page must keep subscription evidence states explicit:
  - `loading`: cost/renewal evidence is still loading and must not be treated as a true missing-subscription fact.
  - `ready`: subscription data is loaded; empty subscription data may be shown as a true missing-subscription issue.
  - `error`: subscription evidence is unavailable and must not be presented as true missing subscription.
- If the scoped subscription request fails, the UI should still render the VPS detail and communicate that renewal/cost evidence cannot be trusted yet.

### Editing and dangerous actions

- High-frequency edits may be exposed through clear buttons, but complex forms stay in Drawer surfaces.
- Existing decision, facts, Node link/unlink, experience log, service, domain, archive, and restore flows must remain usable.
- Dangerous lifecycle operations remain isolated and require explicit confirmation.

### Responsive and visual quality

- Desktop `1440x1000` should feel like a dense engineering workbench with clear hierarchy, not a card wall.
- Mobile `390x900` must avoid page-level horizontal overflow. If a specific evidence surface needs horizontal comparison, the overflow must be local to that surface.
- Text must not overlap or overflow buttons, badges, cards, or panels.
- Keep the v2 Houfeng visual direction: quiet operational density, restrained surfaces, and high signal-to-noise.

### Documentation and evidence

- Update `docs/release/ui-evolution-roadmap.md` so UX-4 is complete and UX-5 is the current completed/in-progress batch as appropriate.
- Add v2 visual evidence screenshots for `/vps/:vpsId` at desktop and mobile viewports, recorded in `docs/operations/v2-visual-evidence/manifest.md`.
- Record preview URL, checked route, and checked viewports in the task or PR summary.

## Acceptance Criteria

- `/vps/:vpsId` first screen answers:
  - What is this VPS?
  - What is the current keep/observe/migrate/cancel decision?
  - What renewal/cost evidence exists or is missing?
  - What Node/service/domain/timeline evidence supports or blocks the decision?
  - What should the operator do next?
- Scoped subscription fetch failure shows unknown/unavailable evidence and does not render a false true-missing-subscription claim.
- Existing detail-page mutation flows remain covered by tests and usable after the layout change.
- New or updated tests cover UX-5 decision-workbench assertions, including subscription `ready` empty and `error` evidence states.
- Local checks pass:
  - targeted VPS detail Vitest
  - front-end lint
  - front-end full Vitest
  - front-end build
  - `make verify-web`
- Visual evidence includes `/vps/:vpsId` screenshots for `1440x1000` and `390x900`.
- Work is delivered through a non-main branch, PR, green CI, merge, local `main` sync, then Trellis archive/journal through a follow-up non-main branch if required.
