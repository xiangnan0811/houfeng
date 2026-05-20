# Research: SettingsPage browser sanity

- **Query**: Perform a browser sanity pass for the implemented SettingsPage IA changes and persist findings to `.trellis/tasks/05-20-settings-page-limited-ia-cleanup/research/browser-sanity.md`.
- **Scope**: mixed — internal route/code/spec context plus local browser evidence with a mock API
- **Date**: 2026-05-20

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/pages/SettingsPage.tsx` | Settings route page under test; renders the IA tabs, notification channel manager/modal, runtime takeover toggle, advanced policy sections, and save footer. |
| `web/src/pages/settings/TelegramSettingsSection.tsx` | Telegram notification settings section; renders masked token summary and `运行时接管` switch. |
| `web/src/app/router.tsx` | Registers protected `/settings` route inside the SPA shell. |
| `web/src/app/RequireAuth.tsx` | Protected route guard; requires `/api/auth/me` to resolve to a user before rendering `/settings`. |
| `web/src/app/layout/AppShell.tsx` | Authenticated shell; calls `/api/dashboard` for shell summary while rendering protected routes. |
| `web/src/lib/api.ts` | Frontend API helpers for `/api/dashboard` and `/api/settings` GET/PUT. |
| `web/src/pages/SettingsPage.test.tsx` | Representative SettingsPage fixture and text expectations used to shape the mock response. |
| `.trellis/spec/web/quality-guidelines.md` | Project guidance allowing local browser sanity outside `verify-web`, with local-only/mock caveat when applicable. |
| `.trellis/tasks/05-20-settings-page-limited-ia-cleanup/prd.md` | Active task acceptance item requiring UI/browser sanity and fixture/mock caveat if no real backend is available. |

### Code Patterns

- `/settings` is a protected SPA route: `web/src/app/router.tsx:65-94` nests `settings` under `RequireAuth` and `AppShell`, so the sanity pass mocked both `/api/auth/me` and `/api/dashboard` before the page-specific `/api/settings` request.
- `RequireAuth` gates on `useAuth()` and redirects when no user exists (`web/src/app/RequireAuth.tsx:4-12`); the mock returned a user object from `/api/auth/me`.
- The shell calls `getDashboard()` on mount (`web/src/app/layout/AppShell.tsx:49-76`); the mock returned a zero-count dashboard summary so the shell could render without a real center.
- `SettingsPage` loads settings once through `getSettings()` (`web/src/pages/SettingsPage.tsx:360-392`) and renders:
  - hero heading and tab context (`web/src/pages/SettingsPage.tsx:498-510`),
  - notification tab/channel state and modal (`web/src/pages/SettingsPage.tsx:529-600`, `web/src/pages/SettingsPage.tsx:657-758`),
  - advanced override/retention sections (`web/src/pages/SettingsPage.tsx:608-623`),
  - page-level save footer (`web/src/pages/SettingsPage.tsx:625-655`).
- Telegram masked-token and runtime toggle rendering are in `web/src/pages/settings/TelegramSettingsSection.tsx:50-83`; the token summary renders only from `token_masked_summary`, and the switch uses aria label `运行时接管`.

### Browser Evidence

- Browser: Chrome/148.0.7778.168, driven locally through Chrome DevTools Protocol against a Vite dev server.
- Served URL: `http://127.0.0.1:55518/settings`.
- Final URL after protected-shell render: `http://127.0.0.1:55518/settings`.
- Viewports exercised:
  - Desktop main pass: 1440 x 1100.
  - Narrow reload smoke: 390 x 844.

#### Mock API endpoints observed

| Method | Endpoint | Count | Response purpose |
|---|---:|---:|---|
| GET | `/api/auth/me` | 4 | Authenticated shell user fixture. |
| GET | `/api/dashboard` | 4 | Zero-count shell dashboard summary. |
| GET | `/api/settings` | 4 | SettingsPage fixture matching the requested representative settings. |

Duplicate request counts are expected under React StrictMode and the second narrow-viewport reload.

#### Mock settings fixture used

- Telegram: `chat_id: "chat-id"`, `token_present: true`, `token_masked_summary: "****************oken"`, `runtime_managed: false`, `runtime_apply_active: false`.
- Feishu: disabled, empty webhook URL.
- Host sample frequency tier: `5s`.
- Probe defaults: TCP `5s`, HTTP `5s`, TLS `6h`.
- Incident defaults: positive heartbeat/sweep/threshold values and notification booleans enabled.
- Override rules: one node-label rule for `edge`, plus representative target-type and target-label rules.
- Retention policy: raw 7 days, aggregate 30 days, event 90 days, notification 180 days.

#### Checks performed

| Check | Result | Evidence |
|---|---|---|
| URL stayed on `/settings` | Pass | Final URL was `http://127.0.0.1:55518/settings`. |
| Default heading visible | Pass | `设置 / Settings` present. |
| Default tab context visible | Pass | `当前分组：通用与外观。先确认浏览器外观、本地主题与中心下发的默认采样/Probe 频率。` present. |
| Save footer visible/reachable on default view | Pass | `保存状态与风险边界` present after scrolling into view. |
| Save footer risk-boundary copy visible | Pass | `本页保持单个全量保存：通知密钥、运行时接管、覆盖 JSON 与保留策略会一起校验并提交。` present. |
| Notifications tab context visible | Pass | `当前分组：通知与告警。先看通道状态，再维护新增渠道与 incident 默认通知策略。` present after clicking `通知与告警`. |
| Notification channel status heading visible | Pass | `通知通道状态` present. |
| Telegram masked token summary visible | Pass | `已配置 Telegram Bot Token：****************oken` present after opening Telegram edit panel. |
| Raw `bot-token` absent from visible text | Pass | Visible body text did not contain `bot-token`. |
| Runtime management toggle present | Pass | `[role="switch"][aria-label="运行时接管"]` present. |
| Runtime management toggle initially off | Pass | `aria-checked="false"`. |
| Channel add modal opens | Pass | `新增通知渠道` dialog opened via `+ 新增通知渠道`. |
| Channel add modal closes | Pass | Dialog removed/hidden after close button. |
| Modal content present | Pass | `飞书 (Feishu)` option visible in the add-channel dialog. |
| Advanced tab context visible | Pass | `当前分组：高级与策略。集中处理覆盖 JSON 与保留策略，保存前请确认风险边界。` present after clicking `高级与策略`. |
| Override rules section visible | Pass | `少量覆盖规则` present. |
| Retention policy section visible | Pass | `数据保留策略` present. |
| Save footer reachable on advanced tab | Pass | `保存状态与风险边界` still present/reachable. |
| Node label override fixture present | Pass | `"label": "edge"` found in `节点标签覆盖规则 JSON` textarea. |
| Narrow viewport load smoke | Pass | `设置 / Settings` present at 390 x 844 after reload. |

#### Browser/runtime errors

- Console errors: none captured.
- Page/runtime exceptions: none captured.
- Failed network requests: none captured.
- HTTP errors: none captured.

### External References

- None. This pass used local repository code/spec context and local browser evidence only.

### Related Specs

- `.trellis/spec/web/quality-guidelines.md:162-166` — browser sanity is outside `verify-web`; local-only evidence must be marked, and adding browser automation dependencies to `web/package.json` is not the workaround for missing local tooling.
- `.trellis/tasks/05-20-settings-page-limited-ia-cleanup/prd.md:38` — task requires UI/browser sanity and explicit fixture/mock caveat if no real backend is available.

## Caveats / Not Found

- This was mock-backed local browser sanity, not a real authenticated center/PostgreSQL session.
- No real `/api/settings` persistence was exercised; PUT was available in the mock handler for completeness but the required pass did not submit the form.
- No screenshots or raster artifacts were written.
- Temporary Vite/Chrome processes started for the pass were terminated after the run.
