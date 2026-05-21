# Browser sanity evidence

## Scope

- Page: `/settings`
- Task: `.trellis/tasks/05-21-next-page-ia-batch-4`
- Implementation scope: SettingsPage section hierarchy IA micro-polish.

## Attempted workflow

Both `trellis-implement` and independent `trellis-check` attempted the existing browser sanity workflow against `/settings` with a local Vite preview server:

```bash
TMPDIR="$PWD/.tmp/playwright" python3 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --mock-api asset-workflows \
  --route /settings \
  --viewport 1440x1000 \
  --viewport 390x900
```

## Result

Browser sanity was blocked by local tooling: the Python environment available in this session does not have the Playwright package installed.

Per `docs/operations/v2-visual-evidence.md`, missing local Python Playwright/browser tooling is a local limitation to report, not a reason to add browser automation dependencies to `web/package.json`.

## Automated verification that did pass

- Focused SettingsPage test passed.
- Web lint passed.
- Full web Vitest suite passed.
- Web build passed.
- Full repository verification passed.
- Independent `trellis-check` also ran `git diff --check` successfully.

## Caveats

- No browser-rendered visual evidence or screenshots were produced for `/settings` in this environment.
- The attempted route used mock API data rather than a real authenticated local center session.
- Full verification reports the existing local Node engine caveat: `web` requires Node `22.x`, while the local shell uses Node `v24.14.1`; tests/build still passed.
- `npm ci` during full verification reports one existing moderate npm audit finding; this IA task did not introduce or address dependency changes.
