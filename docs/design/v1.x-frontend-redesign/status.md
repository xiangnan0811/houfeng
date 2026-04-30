# V1.x Frontend Redesign — Branch Status

**Branch:** `feat/v1.x-plan-1-backend-auth` (despite the name, this branch carries Plan 1 **and** Plan 2; Plan 3 is pending and will land on the same or a sibling branch)

**As of:** 2026-04-30, 25 commits ahead of `main`

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

## Plan 3 — 8 page rewrites + visual evidence ⏸️ Pending

0/15 tasks shipped. Pending because:

- 8 pages × ~700-1700 lines each + matching tests = ~16,000 lines under active edit
- Inline same-session execution risks context-pressure errors on a complex page (NodeDetail, TargetDetail) that already encode binding-state machines, runtime control flows, etc.
- The skill-prescribed flow (subagent-driven-development) is currently blocked by the org monthly subagent quota. Best executed once the quota refreshes.

When Plan 3 resumes the foundation laid in Plan 2 is fully ready: every page will compose existing atoms + token classes; no new infrastructure required.

### Current state of the 8 pages

The 8 page components (`Dashboard`, `Nodes`, `NodeDetail`, `NodeOnboarding`, `Targets`, `TargetDetail`, `Events`, `Settings`) **still render their pre-V1.x visuals** inside the new sidebar shell. They are functional — login → page → all CRUD flows work — they just aren't yet on the new design. This is the deliberate intermediate state Plan 3 was sized to address.

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

## Resume protocol

When ready to restart Plan 3:

1. Spawn `oh-my-claudecode:executor` per page (8 dispatches expected).
2. After each page lands, run `npm run build && npx vitest run` before moving on.
3. Tasks 12–14 (visual evidence regen, gap-checklist, v1-baseline cross-link) close out the plan.
