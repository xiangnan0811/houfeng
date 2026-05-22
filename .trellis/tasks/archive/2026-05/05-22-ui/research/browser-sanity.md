# Research: Houfeng frontend browser sanity

- **Query**: Use Chrome DevTools to inspect `/login`, `/dashboard`, `/nodes`, `/vps`, and `/asset-decisions` at `http://127.0.0.1:5173/` for obvious visual-pass CSS regressions. Do not modify source code.
- **Scope**: browser sanity check
- **Date**: 2026-05-22

## Findings

### Routes Checked

| Route | Observed URL | Result |
|---|---|---|
| `/login` | `http://127.0.0.1:5173/login` | Accessible. Login card, username/password fields, login button, and footer copy are visible. |
| `/dashboard` | `http://127.0.0.1:5173/login?next=%2Fdashboard` | Redirected to login because `/api/auth/me` returned `401 Unauthorized`. |
| `/nodes` | `http://127.0.0.1:5173/login?next=%2Fnodes` | Redirected to login because `/api/auth/me` returned `401 Unauthorized`. |
| `/vps` | `http://127.0.0.1:5173/login?next=%2Fvps` | Redirected to login because `/api/auth/me` returned `401 Unauthorized`. |
| `/asset-decisions` | `http://127.0.0.1:5173/login?next=%2Fasset-decisions` | Redirected to login because `/api/auth/me` returned `401 Unauthorized`. |

### Visual Sanity Notes

- Desktop viewport `1366x900`: `/login` rendered a centered dark login panel with visible Chinese title/copy, two input controls, and a gold primary login button. No horizontal overflow detected (`scrollWidth` = `clientWidth` = `1366`).
- Mobile-ish viewport `390x844`: `/login` remained readable, controls stayed within viewport, and no horizontal overflow was detected (`scrollWidth` = `clientWidth` = `390`).
- DevTools DOM/CSS sampling found no visible large aurora/gradient/glass overlay candidates on the accessible login page; sampled visible elements reported `gradientElements: 0` and `backdropFilterElements: 0`.
- Text contrast looked usable on the inspected login page screenshots: light title/labels on dark surface and dark text on gold login button.
- Basic visible controls on `/login`: username input, password input, login button.

### Browser / Network Evidence

- Browser: Chrome `148.0.7778.97` via DevTools Protocol.
- Dev server responded at `http://127.0.0.1:5173/`.
- Console/network showed repeated expected auth-check failures: `GET /api/auth/me` -> `401 Unauthorized`.
- Screenshots captured outside the repo for inspection: `/tmp/houfeng-browser-sanity/login-desktop.png`, `/tmp/houfeng-browser-sanity/login-mobile.png`.

## Caveats / Not Found

- Authenticated application routes could not be visually inspected because unauthenticated navigation redirected to login. This prevented checking dashboard/nodes/vps/asset-decisions page-specific layouts and controls.
- No source files were modified.
