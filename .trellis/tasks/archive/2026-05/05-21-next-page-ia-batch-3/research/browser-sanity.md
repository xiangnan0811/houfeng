# Browser sanity evidence

## Scope

- Page: `/asset-decisions`
- Task: `.trellis/tasks/05-21-next-page-ia-batch-3`
- Implementation scope: AssetDecisionsPage decision queue IA micro-polish.

## Evidence

`trellis-implement` and independent `trellis-check` both ran browser sanity against `/asset-decisions` using the local Vite preview/dev server with mock API data.

Covered viewports:

- `1440x1000`
- `390x900`

Checked golden path:

- Asset decision page loads with the updated Asset Ledger queue framing.
- Queue evidence boundary chips are visible.
- Renewal-window selector and queue-filter context remain visible.
- Decision queue rows remain high-density and navigable.
- Drawer workflow remains reachable from row actions.
- Renewal evidence stays secondary to the decision queue.
- Mobile layout keeps queue context, tabs, and evidence sections usable without introducing a card-wall rewrite.

## Caveats

- Browser sanity used local/mock data (`mock-api asset-workflows`), not authenticated real production data.
- Automated command verification in this environment reports the existing Node engine caveat: `web` requires Node `22.x`, while the local shell uses Node `v24.14.1`; tests/build still passed.
- `npm ci` during full verification reports one existing moderate npm audit finding; this IA task did not introduce or address dependency changes.
