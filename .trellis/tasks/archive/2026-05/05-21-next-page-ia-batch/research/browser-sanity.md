# Research: TargetDetailPage browser sanity

- **Query**: Run UI/browser sanity for the current Trellis task `.trellis/tasks/05-21-next-page-ia-batch` on the current branch. Scope: TargetDetailPage golden path after the TargetDetailPage IA polish.
- **Scope**: internal browser sanity
- **Date**: 2026-05-21

## Findings

### Files Found

| File Path | Description |
|---|---|
| `docs/operations/v2-visual-evidence.md` | Active local preview/browser-sanity workflow; recommends Vite preview, repo helper, `1440x1000` and `390x900` viewports, and mock profiles for protected observability routes. |
| `scripts/visual_evidence.py` | Repo-local browser sanity helper using local Python Playwright and `--mock-api observability-support`. |
| `web/src/pages/TargetDetailPage.tsx` | Target detail route container that fetches target identity, ProbeItems, runtime facts, incidents, and events. |
| `web/src/pages/target-detail/TargetDetailPageBody.tsx` | IA body for target identity/summary, observation workbench, ProbeItem list, metadata/lifecycle, current incidents, recent events, and history drawer. |
| `web/src/app/router.tsx` | Registers `/targets/:targetId` to `TargetDetailPage`. |

### Browser Sanity Runs

#### Local preview

- Preview URL: `http://127.0.0.1:5178/`
- Server command used: `npm --prefix /Users/weibo/Code/houfeng/web run dev -- --host 127.0.0.1 --port 5178`
- Server status: stopped after checks.
- Browser tooling: `/opt/homebrew/opt/python@3.11/bin/python3.11` with local Python Playwright.
- Temp directory: `TMPDIR=/Users/weibo/Code/houfeng/.tmp/playwright`.

#### Repo helper run

Command:

```bash
TMPDIR=/Users/weibo/Code/houfeng/.tmp/playwright /opt/homebrew/opt/python@3.11/bin/python3.11 /Users/weibo/Code/houfeng/scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --mock-api observability-support --route /targets/target_api_core --viewport 1440x1000 --viewport 390x900
```

Result:

| Route | Viewport | Result | Notes |
|---|---:|---|---|
| `/targets/target_api_core` | `1440x1000` | PASS | `text=249 doc=1440 body=1440 panels=2`; no page-level horizontal overflow reported. |
| `/targets/target_api_core` | `390x900` | PASS | `text=237 doc=390 body=390 panels=2`; no page-level horizontal overflow reported. |

Data source caveat for this run: the built-in `observability-support` mock profile currently covers list routes (`/targets`, `/nodes`, `/events`) but does not provide detail-route fixtures for `/api/targets/{id}`, `/api/targets/{id}/probe-items`, or `/api/targets/{id}/runtime-facts`. Therefore this helper run primarily proves the protected route loads and avoids blank/overflow failure under the selected mock profile; it does not exercise the full TargetDetailPage golden-path content.

#### Detailed DevTools/Playwright IA check

Because authenticated real center/PostgreSQL was unavailable, a local Playwright route fixture was used for the TargetDetailPage API calls already required by the app:

- `GET /api/auth/me`
- `GET /api/dashboard`
- `GET /api/targets/target_api_core`
- `GET /api/targets/target_api_core/probe-items`
- `GET /api/targets/target_api_core/runtime-facts?window=24h`
- `GET /api/incidents?object_type=target&object_id=target_api_core`
- `GET /api/events?object_type=target&object_id=target_api_core`
- `GET /api/incidents?object_type=target&object_id=target_api_core&include_resolved=true` support was included for the history drawer incidents tab path.

Fixture shape covered:

- Target identity: `api-core.example.test`, `target_api_core`, `service`, `host:443`, group, labels, execution-node labels.
- Health summary: `告警`, two active incidents, current issue summary.
- Runtime/latency: enabled target, recent HTTP/TLS latency samples, failed HTTP 5xx observations, TLS expiry observations, time-window tabs.
- ProbeItems: enabled HTTP, enabled TLS, disabled TCP, per-probe latest observations, table rows, edit/toggle/delete actions.
- Metadata/lifecycle: ProbeItem add entry, labels/notes details, lifecycle/archive entry.
- Activity: current active incidents and recent target events.
- History: `查看历史` opens the target history drawer and Escape closes it.

Result:

| Route | Viewport | Result | Geometry / Interaction |
|---|---:|---|---|
| `/targets/target_api_core` | `1440x1000` | PASS | `text=2281 doc=1440 body=1440 sections=34`; expected IA text present; history drawer opened with target event history and closed on Escape; no page-level horizontal overflow. |
| `/targets/target_api_core` | `390x900` | PASS | `text=2262 doc=390 body=390 sections=34`; expected IA text present; history drawer opened with target event history and closed on Escape; no page-level horizontal overflow. |

### Code Patterns

- `TargetDetailPage.tsx` fetches target identity, ProbeItems, and runtime facts together, then independently fetches incidents/events for activity evidence.
- `TargetDetailPageBody.tsx` renders the checked IA surfaces in this order: target watchtower header, target judgment summary, optional danger card, observation workbench, ProbeItem list, metadata/lifecycle maintenance, current incidents/recent events grid, snapshot metadata, ProbeItem drawer, and history drawer.
- `docs/operations/v2-visual-evidence.md` explicitly accepts local mock API profile checks when protected observability routes cannot use a real center, while requiring the caveat that mock checks do not prove backend correctness.

### Related Specs

- No `.trellis/spec` file was required for this browser sanity run. The applicable operational guidance is `docs/operations/v2-visual-evidence.md`.

## Caveats / Not Found

- Real authenticated center/PostgreSQL data was not available in this agent session; detailed IA validation used a local browser route fixture rather than a live center database.
- The repo helper's built-in `observability-support` mock profile does not currently include TargetDetailPage detail endpoints, so a second focused Playwright check supplied those endpoints locally for golden-path coverage.
- No screenshots were committed or written into tracked docs, per `docs/operations/v2-visual-evidence.md`.
- Initial attempt to start the dev server from the repository root failed because `package.json` is under `web/`; the successful run used `npm --prefix /Users/weibo/Code/houfeng/web ...`.

## PASS/FAIL Summary

PASS — TargetDetailPage golden path rendered the requested IA surfaces at desktop `1440x1000` and narrow `390x900` viewports using local browser tooling, with no obvious page-level horizontal overflow/regression observed.