# Research: NodeComparePage Browser Sanity

- **Query**: Perform browser sanity for the NodeComparePage IA implementation at `/nodes/compare?id=nd_a&id=nd_b` using Chrome DevTools MCP/local browser automation; use a local mock API if no real authenticated center/backend is available.
- **Scope**: Internal browser sanity; NodeComparePage-only
- **Date**: 2026-05-20

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/pages/NodeComparePage.tsx` | NodeComparePage implementation under sanity check. |
| `web/src/lib/api.ts` | Confirms NodeComparePage fetches `/api/nodes/{id}` and `/api/nodes/{id}/runtime-facts?window=24h`. |
| `web/src/components/node-detail/NodeWatchtowerMetrics.tsx` | Detailed host metric panel and existing no-HostSample empty state used by NodeComparePage. |
| `web/vite.config.ts` | Vite dev proxy target defaults to `http://127.0.0.1:8080`. |

### Browser Setup

- Started local mock API on `127.0.0.1:8080` because no real authenticated center/backend was used.
- Started web dev server with `npm --prefix /Users/weibo/Code/houfeng/web run dev -- --host 127.0.0.1`.
- Started local Chrome with remote debugging on `127.0.0.1:9333` and loaded `http://127.0.0.1:5173/nodes/compare?id=nd_a&id=nd_b`.
- Mock API caveat: the mock included the requested page endpoints plus `/api/auth/me` and `/api/dashboard`, because the authenticated app shell loads those before rendering protected pages.

### Mock Endpoints Exercised

| Endpoint | Mock Response Purpose |
|---|---|
| `GET /api/auth/me` | Authenticated shell gate for protected route. |
| `GET /api/dashboard` | App shell summary request. |
| `GET /api/nodes/nd_a` | A-side node identity with normal health and bound/running statuses. |
| `GET /api/nodes/nd_a/runtime-facts?window=24h` | A-side runtime facts with latest HostSample and five recent HostSamples. |
| `GET /api/nodes/nd_b` | B-side node identity with notice health and bound/running statuses. |
| `GET /api/nodes/nd_b/runtime-facts?window=24h` | B-side runtime facts with `latest_host_sample: null` and no recent samples. |

Mock API log confirmed the required page GET endpoints were requested:

```text
GET /api/nodes/nd_a
GET /api/nodes/nd_b
GET /api/nodes/nd_a/runtime-facts?window=24h
GET /api/nodes/nd_b/runtime-facts?window=24h
```

React StrictMode/dev rendering caused the same page data requests to appear twice; both rounds returned successful mock responses.

### Visual / Behavioral Checks

| Check | Result | Evidence |
|---|---|---|
| Command/header panel is visible | Pass | `.compare-command` was visible and contained `节点对比 · 24H RUNTIME FACTS` plus `判断两个 Node 是否需要深入排查`. |
| 24h runtime facts context is visible | Pass | Page text included both `24h runtime facts` and `主机指标对比`. |
| A/B identity cards are visible | Pass | Two `.compare-identity__card` elements were visible. A card showed `A 样本节点`; B card showed `B 无样本节点`. |
| A/B summary strip is visible | Pass | `.compare-summary-strip` was visible with two summary cards and heading `A/B 摘要判断`. |
| Node detail links exist | Pass | Links existed for `/nodes/nd_a` and `/nodes/nd_b`, including visible `节点详情` links for both sides. |
| NodeWatchtowerMetrics detailed metrics remain below summary | Pass | One visible `.watchtower-metrics-panel` appeared below the summary strip, showing `关键资源趋势`, `CPU 使用率`, and `内存使用率`. |
| Side with no sample shows existing empty metric state | Pass | B metrics column showed `尚未收到主机样本` and `该节点已存在，但首批主机采样（HostSample）还未到达。请等待下一次 agent 同步。` |
| Summary distinguishes sample availability | Pass | A summary showed `有样本` and `窗口样本 5 条`; B summary showed `无样本` and `24h runtime facts 暂无 HostSample`. |
| Console/runtime errors | Pass | Chrome DevTools protocol collection reported no `Runtime.exceptionThrown`, no log errors, no network loading failures, and no HTTP errors for the loaded page. |

### Captured DOM Facts

- URL loaded: `http://127.0.0.1:5173/nodes/compare?id=nd_a&id=nd_b`.
- Viewport used: `1440 x 1013`.
- Identity card count: `2`.
- Summary card count: `2`.
- Metric column count: `2`.
- Metrics panel count: `1`.
- A metric column contained detailed Host Metrics charts.
- B metric column contained the existing `NodeWatchtowerMetrics` no-sample empty state.

### Related Specs

- No additional spec documents were needed for this browser-only sanity run.

## Caveats / Not Found

- This was a mock-backed browser sanity pass, not a real center/PostgreSQL/auth session pass.
- The mock API intentionally covered only the endpoints required to render the protected shell and NodeComparePage; unrelated routes/actions were not exercised.
- Chrome process logs contained browser-level background updater/GCM messages, but DevTools page-level collection found no console/runtime errors or page network failures.
- Temporary mock API, Vite dev server, and Chrome debugging session were stopped after the run.
