# Browser sanity

## Scope

- Route: `/vps/vps_fra_legacy`
- Viewports:
  - `1440x1000`
  - `390x900`
- Data source: local temporary mock API, reusing `scripts/visual_evidence.py` asset-workflows fixture functions.
- Preview URL during check: `http://127.0.0.1:5181/`
- Mock API during check: `http://127.0.0.1:18080/`

## Result

PASS with local-only evidence.

- Top "当前判断" rendered multiple attention items:
  - `取消/退役`
  - `运行观测需要核对`
  - `自动续费已取消`
- Old middle `vps-detail-context-action` / `aria-label="需要处理的状态"` region was absent.
- Desktop and mobile geometry checks reported:
  - no page-level horizontal overflow;
  - no visible element escaping viewport horizontally;
  - no button text overflow;
  - no `[role="alert"]` runtime errors.
- Top `更多` menu opened without viewport clipping in both checked viewports.
- Clicking outside the top menu closed it.
- Top attention actions opened the expected modals:
  - `处理取消/退役` opened the cancellation workbench modal.
  - `监控观测` opened the monitoring evidence modal.

## Limitation

`python3 scripts/visual_evidence.py browser-sanity` was not used directly because local Python Playwright is not installed in this environment (`No module named 'playwright'`). The equivalent check used local Chromium DevTools Protocol with the same route/viewports and fixture data. No screenshots were committed.
