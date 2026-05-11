# UX-4 VPS detail asset workbench

## Goal

Turn `VPSDetailPage` from an operation-first stack into a single-VPS asset judgment workbench. The page should help a user decide whether to keep, observe, migrate, cancel, or archive this VPS by putting identity, renewal/cost, Node evidence, service/domain context, and history in that order.

## Background

- Parent plan: `docs/release/core-pages-product-ux-replan.md` defines VPS detail as the single asset judgment and evidence page.
- UX-3 already improved `AssetDecisionsPage` and `VPSPage`, so the detail page should now close the loop for one selected VPS.
- Current detail page already has working backend contracts for VPS detail, timeline, service records, domain records, Node links, decision updates, lifecycle updates, and experience logs.

## Requirements

### Information hierarchy

1. Hero shows identity and current asset state: name, provider/location, lifecycle, usage, renewal decision, linked Node count, and primary actions.
2. First major content surface is an asset judgment workbench:
   - current renewal decision and reason/history signal,
   - current subscription/cost and renewal timing,
   - linked Node observability evidence,
   - data quality gaps that block confident decisions.
3. Basic facts remain available but should not dominate the page before judgment.
4. Node, services, domains, and timeline are presented as evidence/context, not as competing page subjects.
5. Connection/access details stay visible as operational evidence.

### Editing model

- Frequent and focused edits are opened from clear action buttons.
- Complex forms move into Drawer surfaces instead of staying permanently visible in the main scan path.
- Dangerous lifecycle actions remain isolated and confirm before archive.
- Existing API behavior and validation should be preserved.

### Data

- Use existing APIs only.
- Fetch VPS-scoped subscriptions with `listSubscriptions({ vps_id, sort: 'renew_at', order: 'asc' })` so renewal/cost can be shown on the detail page.
- Do not infer missing renewal/cost facts as real values. Show explicit missing or fallback states.

### Documentation and tests

- Update frontend tests for the new layout and Drawer edit flow.
- Preserve coverage for decision update, fact edit, Node link/unlink, experience log, lifecycle archive/restore, service creation, and domain creation.
- Add or update design/spec notes so future work knows the VPS detail page is an asset judgment workbench.

## Out of Scope

- No backend schema changes.
- No complete service registry.
- No complete domain management, DNS record management, or provider import.
- No real external provider data integration.
- No release/publish workflow changes.

## Acceptance

- From the first screen, a user can understand what this VPS is, what the current decision is, whether cost/renewal data exists, and whether Node evidence supports the decision.
- Main page scan path is not dominated by forms.
- Decision, facts, Node link, experience log, service, domain, and lifecycle actions remain usable.
- Automated frontend checks pass.
- Work is delivered through feature branch, PR, green CI, merge, and local `main` sync.
