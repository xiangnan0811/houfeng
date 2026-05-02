# V1.x Frontend Redesign — Branch Status

**Branch:** `feat/v1.x-plan-1-backend-auth` (carries Plan 1, Plan 2, and the Plan 3 visual-skin
pass — the deeper page-level redesign per spec §10 is deferred as V1.x.1, see below)

**As of:** 2026-04-30, ~30 commits ahead of `main`

## Plan 1 — Backend auth ✅ Complete

22/22 tasks shipped.

- `golang.org/x/crypto/bcrypt` dependency
- Migration `0010_add_users_and_sessions.sql`
- `internal/center/auth/` package: types, password hashing, session ID generation, cookie helpers, service (Login/Logout/Touch/UserBySession/ChangePassword), session cleanup worker, initial-user seeding
- `internal/center/store/users.go` and `internal/center/store/sessions.go` Postgres repositories
- `internal/center/http/handlers/auth.go` exposing `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/me`, `PUT /api/auth/password`
- `internal/center/http/middleware.go` `RequireSession` middleware applied to every `/api/*` route except `/api/healthz` and `/api/agent/*`
- Bootstrap wiring: repos, service, seed, middleware, cleanup worker
- Config: `HOUFENG_INITIAL_USERNAME`, `HOUFENG_INITIAL_PASSWORD`, `HOUFENG_INITIAL_DISPLAY_NAME`, `HOUFENG_SESSION_TTL` (rolling 7 d default)
- End-to-end smoke test in `internal/center/http/auth_e2e_test.go` covering login → 200 protected → logout → 401
- `.env.example`, `docs/deploy/local-and-systemd.md`, `docs/release/v1-gap-checklist.md` updates

**Tests:** 60+ new Go tests, all green via `make verify-go`.

## Plan 2 — Frontend foundation + login ✅ Complete

26/27 tasks shipped (1 deferred — see below).

- `web/src/styles/tokens.css` — 4 themes (候风原色 / 经典 × dark / light) as CSS variables
- `web/src/styles/reset.css`, `web/src/styles/atoms.css` — minimal reset + atom styles, all token-driven
- `web/index.html` — inline FOUC script that resolves theme from `localStorage` + `prefers-color-scheme` synchronously
- `web/src/lib/theme.ts` + `theme-context.tsx` — runtime theme switching with system-follow listener and `localStorage` persistence
- `web/src/lib/fetcher.ts` — credentialed fetch wrapper with 401 hook; `auth-client.ts` for the four API endpoints
- `web/src/lib/auth-context.tsx` — boot-time `/me`, login, logout, refresh; routes 401s back to `/login`
- `web/src/lib/api.ts` updated to send `credentials: include` and surface 401 to the auth context
- 6 component atoms: `Button`, `Input`, `Badge`, `Card`, `Tabs`, `Toggle`
- Sidebar shell: `Sidebar`, `UserChip`, `SyncStatus`, `ChangePasswordModal`, all token-driven, no "单用户/全权限/个人系统" copy
- `LoginPage` with seal, motto, brand, error surface, `?next=` redirect
- `RequireAuth` route guard
- `AppShell` rewritten to compose `Sidebar` + `Outlet` + change-password modal slot
- Router updated to `/login` public + `/*` protected
- `web/src/main.tsx` wires `ThemeProvider` + `AuthProvider` + `RouterProvider`
- `scripts/subset-fonts.sh` documented as optional follow-up; the system-font fallback chain (PingFang SC / Microsoft YaHei UI / Noto Sans CJK SC) covers the 4-theme rendering at ~90% fidelity until bundled woff2 are added

**Tests:** 234/234 frontend tests pass via `npm run test`. Production `npm run build` produces 414 KB JS / 18 KB CSS.

### Deferred (intentionally) into Plan 3

- **Settings page Theme Tab** — Plan 2 §10.10.1 spec adds a 5th Pill Tab inside `SettingsPage.tsx`. Since Plan 3 wholesale rewrites SettingsPage anyway, layering the partial Theme tab now would be undone in Plan 3. Theme switching is still fully functional from the user-chip dropdown route to `/settings`, just not yet exposed as a Pill Tab section.

## Plan 3 — page visual skin + Theme Tab + cross-link docs ✅ Complete (skin pass)

Pragmatic scope reduction agreed with the user: instead of a full per-page rewrite per spec §10,
this branch ships a **visual skin pass** that gives every existing page V1.x tokens, atoms, and
the new shell, while keeping all business logic untouched. Tasks 13–14 (gap-checklist + cross-link)
are also closed here.

What landed:

- `web/src/styles/pages.css` — single token-driven sheet that re-skins every shared page class
  (`.page-panel`, `.page-stack`, `.summary-card`, `.detail-section*`, `.resource-table`,
  `.metric-card`, `.probe-card`, `.observation-row`, etc.) so all 8 pages immediately render in
  the new V1.x palette / typography / spacing across all four themes.
- Settings page **Theme tab** (`ThemeSettingsSection`) wired to `useTheme` with two pill tabs:
  风格 (候风原色 / 经典) and 明暗 (深色 / 浅色 / 跟随系统).
- DashboardPage H1 renamed to `首页 / Dashboard` (per spec §10.1) with matching test updates.
- `docs/release/v1-gap-checklist.md` — V1.x visual baseline section added with status rows.
- `docs/design/v1-baseline/README.md` — `VISUAL PORTION UNFROZEN 2026-04-29` banner pointing to
  `v1.x-frontend-redesign/`.
- Top-level `README.md` — Guardrails updated so `Visual authority` points to the V1.x folder.

Tests: 234/234 frontend tests pass after the skin pass. Production build still ~414 KB JS / ~27 KB
CSS (pages.css adds ~9 KB).

### Deferred to V1.x.1 (separate follow-up plan)

The deeper per-page restructure per spec §10 — identity card with status dot, 5-tab layout on Node
detail, danger zone, ProbeItem inline toggle list, advanced events filter card, KPI 4-card row on
Dashboard, etc. — is intentionally **not** in this branch. Reasons:

- Each of the 8 pages currently encodes business logic (binding-state machines, runtime control
  flows, ProbeItem CRUD, dirty-bar form tracking) whose rewrite alongside layout changes is a
  large surface area best handled by subagent-driven plans.
- The skin pass already gives users a unified visual experience and keeps every existing test
  passing, so the branch is safe to merge as-is.
- A follow-up plan (V1.x.1) under `docs/design/v1.x-frontend-redesign/plans/` should pick this up
  when subagent dispatch budget permits and tackle one page per plan.

### Current state of the 8 pages

`Dashboard`, `Nodes`, `NodeDetail`, `NodeOnboarding`, `Targets`, `TargetDetail`, `Events`, `Settings`
all render with V1.x tokens (palette, typography, borders, spacing) under the new sidebar shell.
The information density and component grouping inside each page reflect the pre-V1.x layout — for
example NodeDetail still uses its existing summary cards rather than the spec §10.5 「身份卡 + 5 Tab + 危险区」
structure. Functionality is unchanged.

## How to review the branch

Suggested review order (each link points to the relevant commit):

1. `dd7f3cc` (`Add V1.x frontend redesign spec and CLAUDE.md`) — start with the spec under `docs/design/v1.x-frontend-redesign/README.md`
2. `cccdeee` and `9625181` — the three plan files
3. Plan 1 implementation: `git log 23c7f94..520f40e` walks Plan 1 commit by commit
4. Plan 2 implementation: `git log 20c3613..c839639` walks Plan 2 commit by commit

A focused review of Plan 2's UI surface is best done by running:

```
make verify-go
cd web && npm ci && npm run build && npm run test
```

then booting the center against a local Postgres (see `docs/deploy/local-and-systemd.md` Authentication section) and walking the login → dashboard flow in 4 themes via the user-chip dropdown's localStorage entries, since the in-page Theme Tab is deferred (see above).

## Resume protocol — V1.x.1 (deeper per-page redesign)

When subagent budget permits:

1. Author a per-page V1.x.1 plan under `docs/design/v1.x-frontend-redesign/plans/` for one page
   at a time. Each plan should explicitly enumerate the §10 layout primitives the page should
   adopt (身份卡, Tab strip, 危险区, etc.) and how each call site rebinds existing API hooks
   onto the new layout.
2. Spawn `oh-my-claudecode:executor` for the implementation, with `oh-my-claudecode:code-reviewer`
   as the second-stage reviewer. Run `make verify-go && cd web && npm run build && npx vitest run`
   between pages.
3. After all pages land, regenerate `docs/operations/visual-evidence/` (4 themes × representative
   pages) and run a manual contrast smoke for WCAG AA.
