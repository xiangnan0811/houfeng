# Plan 2 · 前端基础 + 登录页 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Depends on:** Plan 1 must be merged. Plan 1 ships `/api/auth/{login,logout,me,password}` and global session cookie middleware.

**Goal:** Replace the current `web/` SPA chrome with a fully token-driven design system (4 themes × 系统跟随), 5 component atoms with tests, a new sidebar shell that includes a user chip, a login page wired to Plan 1's API, and a route guard. Existing 8 page components remain unchanged in this plan (they will be rewritten in Plan 3 on top of the foundation).

**Architecture:** Token system in `web/src/styles/tokens.css` (CSS custom properties keyed by theme classes on `<html>`). Web fonts in `web/public/fonts/` are subset .woff2 files served by the Go center alongside the SPA. A small `web/src/lib/theme.ts` runtime reads `localStorage` and `prefers-color-scheme` and toggles the `<html>` class; an inline script in `index.html` does the same synchronously to prevent FOUC. The auth API client lives in `web/src/lib/auth-client.ts`; auth state in a React context (`web/src/lib/auth-context.tsx`); a `<RequireAuth>` wrapper around protected routes redirects to `/login` on 401. Component atoms (Button, Input, Badge, Card, Tabs, Toggle) live in `web/src/components/atoms/`. The new sidebar shell composes them.

**Tech Stack:** React 19, TypeScript, Vite, React Router 7, Vitest + @testing-library/react. No new dependencies (Tailwind / Radix explicitly NOT introduced — keep the toolbox lean; styling = vanilla CSS + tokens). Font subsetting uses `pyftsubset` (Python `fonttools`); subset commands documented in Task 1.

**Out of scope:**
- Page rewrites (Plan 3)
- Visual evidence regeneration (Plan 3)
- Removing existing page components (they keep working, just inside the new shell)
- E2E (Playwright) — defer; covered by Vitest + manual smoke

---

## File Structure

### New
```
web/public/fonts/
  source-han-serif-sc-500.woff2          # subset 5K + ASCII
  source-han-sans-sc-400.woff2
  source-han-sans-sc-500.woff2
  source-han-sans-sc-600.woff2
  inter-400.woff2
  inter-500.woff2
  inter-600.woff2
  jetbrains-mono-400.woff2
web/src/styles/
  tokens.css                              # 4 themes × all CSS vars
  fonts.css                               # @font-face + preload hints
  reset.css                               # very small reset (margin/padding/box-sizing)
web/src/lib/
  theme.ts                                # Theme types, get/set runtime, follow-system listener
  theme-fouc.ts                           # exports the inline script body for index.html
  auth-client.ts                          # login / logout / me / changePassword
  auth-context.tsx                        # AuthProvider + useAuth hook
  fetcher.ts                              # tiny fetch wrapper with 401 interceptor + cookie creds
web/src/components/atoms/
  Button.tsx + Button.test.tsx
  Input.tsx + Input.test.tsx
  Badge.tsx + Badge.test.tsx
  Card.tsx + Card.test.tsx
  Tabs.tsx + Tabs.test.tsx
  Toggle.tsx + Toggle.test.tsx
  index.ts                                # barrel
web/src/app/layout/
  Sidebar.tsx + Sidebar.test.tsx
  UserChip.tsx + UserChip.test.tsx
  SyncStatus.tsx + SyncStatus.test.tsx
web/src/app/
  RequireAuth.tsx + RequireAuth.test.tsx
web/src/pages/
  LoginPage.tsx + LoginPage.test.tsx
```

### Modified
```
web/index.html                                  # inline FOUC script + preload key fonts + root html theme class
web/src/main.tsx                                # import tokens.css / fonts.css / reset.css; wrap router with AuthProvider
web/src/app/router.tsx                          # add /login route, wrap protected routes with RequireAuth
web/src/app/layout/AppShell.tsx                 # rewrite to compose Sidebar + Outlet
web/src/app/layout/AppShell.test.tsx            # rewrite
web/src/app/metadata.ts                         # PRIMARY_NAV_ITEMS label "首页" (revert from "集群概览" per spec §10.1)
web/src/index.css                               # delete (replaced by tokens/fonts/reset)
web/src/lib/api.ts                              # route through fetcher (add credentials: include + 401 hook)
web/src/pages/SettingsPage.tsx                  # add 「主题」Pill Tab + content (风格 + 明暗 + 修改密码 trigger)
web/src/pages/SettingsPage.test.tsx             # cover new tab
web/src/lib/types.ts                            # User, Theme, Mode types
web/eslint.config.js                            # may need adjustment for new file paths (verify)
web/vite.config.ts                              # ensure /fonts/* assets are served and copied to dist; preload optimization
```

---

### Task 1: Subset and stage Web Font assets

**Files:**
- Create: `web/public/fonts/*.woff2` (8 files)
- Create: `scripts/subset-fonts.sh` (one-shot helper, kept for future regen)

- [ ] **Step 1: Install `pyftsubset` locally if absent**

```bash
python3 -m pip install --user fonttools[woff,unicode]
```

- [ ] **Step 2: Download upstream sources to a temp dir**

```bash
mkdir -p /tmp/houfeng-fonts && cd /tmp/houfeng-fonts
# Source Han (Adobe + Google Noto): use Noto Serif SC / Noto Sans SC otf releases
curl -fL -o NotoSerifSC-Medium.otf https://github.com/notofonts/noto-cjk/raw/main/Serif/SubsetOTF/SC/NotoSerifSC-Medium.otf
curl -fL -o NotoSansSC-Regular.otf https://github.com/notofonts/noto-cjk/raw/main/Sans/SubsetOTF/SC/NotoSansSC-Regular.otf
curl -fL -o NotoSansSC-Medium.otf  https://github.com/notofonts/noto-cjk/raw/main/Sans/SubsetOTF/SC/NotoSansSC-Medium.otf
curl -fL -o NotoSansSC-SemiBold.otf https://github.com/notofonts/noto-cjk/raw/main/Sans/SubsetOTF/SC/NotoSansSC-SemiBold.otf
curl -fL -o Inter-Regular.ttf      https://github.com/rsms/inter/raw/master/docs/font-files/Inter-Regular.woff2
curl -fL -o Inter-Medium.ttf       https://github.com/rsms/inter/raw/master/docs/font-files/Inter-Medium.woff2
curl -fL -o Inter-SemiBold.ttf     https://github.com/rsms/inter/raw/master/docs/font-files/Inter-SemiBold.woff2
curl -fL -o JetBrainsMono.ttf      https://github.com/JetBrains/JetBrainsMono/raw/master/fonts/ttf/JetBrainsMono-Regular.ttf
```

> If specific Noto/Inter URLs change upstream, adjust to current location; commit the resulting woff2 files (not the raw downloads).

- [ ] **Step 3: Subset CJK fonts to GB2312 + frequent-5K + ASCII + punctuation**

Create `scripts/subset-fonts.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
SRC="${1:-/tmp/houfeng-fonts}"
DST="${2:-web/public/fonts}"
UNICODES="U+0020-007E,U+00A0-00FF,U+2010-201F,U+2025-2027,U+2030,U+2032-2033,U+2039-203A,U+203E,U+3000-303F,U+FF00-FFEF,U+4E00-9FFF"

mkdir -p "$DST"

subset() {
  local in="$1" out="$2"
  pyftsubset "$in" \
    --unicodes="$UNICODES" \
    --layout-features='*' \
    --no-hinting \
    --desubroutinize \
    --output-file="$out" \
    --flavor=woff2
}

subset "$SRC/NotoSerifSC-Medium.otf"   "$DST/source-han-serif-sc-500.woff2"
subset "$SRC/NotoSansSC-Regular.otf"   "$DST/source-han-sans-sc-400.woff2"
subset "$SRC/NotoSansSC-Medium.otf"    "$DST/source-han-sans-sc-500.woff2"
subset "$SRC/NotoSansSC-SemiBold.otf"  "$DST/source-han-sans-sc-600.woff2"
subset "$SRC/Inter-Regular.ttf"        "$DST/inter-400.woff2"
subset "$SRC/Inter-Medium.ttf"         "$DST/inter-500.woff2"
subset "$SRC/Inter-SemiBold.ttf"       "$DST/inter-600.woff2"
subset "$SRC/JetBrainsMono.ttf"        "$DST/jetbrains-mono-400.woff2"

ls -lh "$DST"
```

Run: `chmod +x scripts/subset-fonts.sh && ./scripts/subset-fonts.sh /tmp/houfeng-fonts web/public/fonts`
Expected: 8 woff2 files, total <= 2.5 MB.

- [ ] **Step 4: Verify file size budget**

Run: `du -ch web/public/fonts/*.woff2 | tail -n1`
Expected: total ≤ 2.6 MB. If over, narrow the unicode range (drop CJK Extension A, retry).

- [ ] **Step 5: Commit**

```bash
git add web/public/fonts/ scripts/subset-fonts.sh
git commit -m "Add subset web font assets and regen script"
```

---

### Task 2: `web/src/styles/tokens.css` (4 themes)

**Files:**
- Create: `web/src/styles/tokens.css`

- [ ] **Step 1: Write the file**

Replicate spec §6.2 / §7.1 / §8 token tables verbatim.

```css
/* Tokens. Default = candidate-原色 dark. Override blocks below by html class. */
:root {
  /* Type tokens (size in px, line-height unitless) */
  --type-display-size: 28px;     --type-display-weight: 600; --type-display-leading: 1.1;
  --type-h1-size: 22px;          --type-h1-weight: 600;      --type-h1-leading: 1.3;
  --type-h2-size: 16px;          --type-h2-weight: 600;      --type-h2-leading: 1.4;
  --type-body-size: 14px;        --type-body-weight: 400;    --type-body-leading: 1.6;
  --type-small-size: 12px;       --type-small-weight: 400;   --type-small-leading: 1.5;
  --type-eyebrow-size: 10px;     --type-eyebrow-weight: 500; --type-eyebrow-tracking: 0.18em;
  --type-metric-size: 14px;      --type-metric-weight: 500;
  --type-state-size: 11px;       --type-state-weight: 400;   --type-state-tracking: 0.06em;
  --type-code-size: 12px;        --type-code-weight: 400;
  --type-link-size: 14px;        --type-link-weight: 500;

  --font-serif: 'Source Han Serif SC', 'Noto Serif SC', 'Songti SC', serif;
  --font-sans:  'Source Han Sans SC', Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei UI', sans-serif;
  --font-mono:  'JetBrains Mono', ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  --font-numeric: var(--font-sans);

  /* Spacing */
  --space-1: 4px; --space-2: 8px; --space-3: 12px; --space-4: 16px;
  --space-5: 20px; --space-6: 24px; --space-8: 32px; --space-12: 48px;

  /* Radius */
  --radius-0: 0; --radius-1: 4px; --radius-2: 7px; --radius-3: 12px; --radius-pill: 999px;

  /* Borders */
  --border-w: 1px;
  --border-w-strong: 2px;

  /* Shadow */
  --shadow-glow:    0 0 16px rgba(184, 153, 104, 0.35);
  --shadow-soft:    0 1px 2px rgba(0,0,0,0.18), 0 4px 8px rgba(0,0,0,0.10);
  --shadow-overlay: 0 12px 40px rgba(0,0,0,0.55), 0 4px 12px rgba(0,0,0,0.35);

  /* State colors — by default, point to candidate-原色 dark. Themes override. */
  --color-state-normal:   #5BA88E;
  --color-state-notice:   #B89968;
  --color-state-alert:    #C4814E;
  --color-state-critical: #B85042;
  --color-state-maintenance: #6E8FA8;
  --color-state-offline:  #6B6760;

  /* Surface (default candidate-原色 dark) */
  --bg:               #131419;
  --bg-sidebar:       #0E0F13;
  --surface:          rgba(255,255,255,0.025);
  --surface-elevated: rgba(255,255,255,0.040);
  --border:           #26241D;
  --border-dashed:    #3A3833;
  --text-primary:     #F1ECE0;
  --text-secondary:   #A39E94;
  --text-muted:       #8A8576;
  --text-disabled:    #5C584F;

  --accent:           #B89968;
  --accent-strong:    #D4B876;
  --accent-soft:      rgba(184,153,104,0.10);
  --accent-border:    rgba(184,153,104,0.30);

  /* Backdrop gradients (only candidate-原色 uses these; classic clears them) */
  --bg-aurora: radial-gradient(ellipse at top right, rgba(184,153,104,0.10) 0%, transparent 50%),
               radial-gradient(ellipse at bottom left, rgba(99,102,241,0.05) 0%, transparent 50%);
}

/* candidate-原色 light */
html.theme-houfeng-light {
  --bg:               #FAF7EF;
  --bg-sidebar:       #F2EDDF;
  --surface:          #FFFEFA;
  --surface-elevated: #FFFEFA;
  --border:           #E8E2D5;
  --border-dashed:    #C7BCA0;
  --text-primary:     #2C2A24;
  --text-secondary:   #5C5849;
  --text-muted:       #857F6E;
  --text-disabled:    #B5AC95;
  --accent:           #8B6B3D;
  --accent-strong:    #8B6B3D;
  --accent-soft:      rgba(139,107,61,0.08);
  --accent-border:    rgba(139,107,61,0.25);
  --color-state-normal:   #2F8265;
  --color-state-notice:   #8B6B3D;
  --color-state-alert:    #A06434;
  --color-state-critical: #A53D2F;
  --color-state-maintenance: #4F7393;
  --color-state-offline:  #7A7568;
  --bg-aurora: radial-gradient(ellipse at top right, rgba(139,107,61,0.10) 0%, transparent 55%),
               radial-gradient(ellipse at bottom left, rgba(99,102,241,0.05) 0%, transparent 50%);
  --shadow-soft: 0 1px 2px rgba(0,0,0,0.05), 0 4px 8px rgba(0,0,0,0.04);
  --shadow-overlay: 0 12px 40px rgba(0,0,0,0.18), 0 4px 12px rgba(0,0,0,0.10);
}

/* classic dark */
html.theme-classic-dark {
  --bg:               #15140F;
  --bg-sidebar:       #100F0B;
  --surface:          rgba(255,255,255,0.020);
  --surface-elevated: rgba(255,255,255,0.035);
  --border:           #2A2823;
  --border-dashed:    #3A3833;
  --text-primary:     #E8E1D5;
  --text-secondary:   #A39884;
  --text-muted:       #8C8472;
  --text-disabled:    #5C584F;
  --accent:           #C09A5C;
  --accent-strong:    #D4B876;
  --accent-soft:      rgba(192,154,92,0.10);
  --accent-border:    rgba(192,154,92,0.30);
  --color-state-normal:   #4D9C7C;
  --color-state-notice:   #C09A5C;
  --color-state-alert:    #C97847;
  --color-state-critical: #B85042;
  --color-state-maintenance: #618BA8;
  --color-state-offline:  #6B6760;
  --bg-aurora: none;
}

/* classic light */
html.theme-classic-light {
  --bg:               #F5F0E1;
  --bg-sidebar:       #ECE5D2;
  --surface:          #FBF7E8;
  --surface-elevated: #FFFEFA;
  --border:           #D8CFB8;
  --border-dashed:    #C7BCA0;
  --text-primary:     #2A2620;
  --text-secondary:   #5C5849;
  --text-muted:       #7A6F58;
  --text-disabled:    #B5AC95;
  --accent:           #9A7A3D;
  --accent-strong:    #9A7A3D;
  --accent-soft:      rgba(154,122,61,0.08);
  --accent-border:    rgba(154,122,61,0.30);
  --color-state-normal:   #2F8265;
  --color-state-notice:   #9A7A3D;
  --color-state-alert:    #A06434;
  --color-state-critical: #A53D2F;
  --color-state-maintenance: #4F7393;
  --color-state-offline:  #7A7568;
  --bg-aurora: none;
  --shadow-soft: 0 1px 2px rgba(0,0,0,0.05), 0 4px 8px rgba(0,0,0,0.04);
  --shadow-overlay: 0 12px 40px rgba(0,0,0,0.15), 0 4px 12px rgba(0,0,0,0.08);
}
```

- [ ] **Step 2: Verify it parses (vite build smoke)**

Run: `cd web && npm run build`
Expected: tsc + vite build green. The CSS file is referenced after Task 6 wires it into `main.tsx`; for now it's an unimported asset and that's fine.

- [ ] **Step 3: Commit**

```bash
git add web/src/styles/tokens.css
git commit -m "Add 4-theme design tokens"
```

---

### Task 3: `web/src/styles/fonts.css`

**Files:**
- Create: `web/src/styles/fonts.css`

- [ ] **Step 1: Write the file**

```css
/* font-display: swap so first paint uses system fallback, no jank. */
@font-face { font-family: 'Source Han Serif SC'; src: url('/fonts/source-han-serif-sc-500.woff2') format('woff2'); font-weight: 500; font-display: swap; }
@font-face { font-family: 'Source Han Sans SC';  src: url('/fonts/source-han-sans-sc-400.woff2')  format('woff2'); font-weight: 400; font-display: swap; }
@font-face { font-family: 'Source Han Sans SC';  src: url('/fonts/source-han-sans-sc-500.woff2')  format('woff2'); font-weight: 500; font-display: swap; }
@font-face { font-family: 'Source Han Sans SC';  src: url('/fonts/source-han-sans-sc-600.woff2')  format('woff2'); font-weight: 600; font-display: swap; }
@font-face { font-family: 'Inter';                src: url('/fonts/inter-400.woff2')               format('woff2'); font-weight: 400; font-display: swap; }
@font-face { font-family: 'Inter';                src: url('/fonts/inter-500.woff2')               format('woff2'); font-weight: 500; font-display: swap; }
@font-face { font-family: 'Inter';                src: url('/fonts/inter-600.woff2')               format('woff2'); font-weight: 600; font-display: swap; }
@font-face { font-family: 'JetBrains Mono';       src: url('/fonts/jetbrains-mono-400.woff2')      format('woff2'); font-weight: 400; font-display: swap; }
```

- [ ] **Step 2: Commit**

```bash
git add web/src/styles/fonts.css
git commit -m "Add font-face declarations for bundled web fonts"
```

---

### Task 4: `web/src/styles/reset.css`

**Files:**
- Create: `web/src/styles/reset.css`

- [ ] **Step 1: Write the file**

```css
*, *::before, *::after { box-sizing: border-box; }
html, body, #root { height: 100%; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--text-primary);
  font-family: var(--font-sans);
  font-size: var(--type-body-size);
  line-height: var(--type-body-leading);
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  font-feature-settings: 'tnum' 0; /* tnum opt-in via class */
}
h1, h2, h3, h4, p, ul, ol { margin: 0; padding: 0; }
ul, ol { list-style: none; }
a { color: inherit; text-decoration: none; }
button { font: inherit; cursor: pointer; }
.tnum { font-variant-numeric: tabular-nums; font-feature-settings: 'tnum'; }
```

- [ ] **Step 2: Commit**

```bash
git add web/src/styles/reset.css
git commit -m "Add minimal reset stylesheet"
```

---

### Task 5: Theme runtime — types + helpers (`lib/theme.ts`)

**Files:**
- Create: `web/src/lib/theme.ts`
- Test: `web/src/lib/theme.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it, beforeEach, vi } from 'vitest'
import {
  applyTheme,
  detectInitialTheme,
  preferredScheme,
  type Preset,
  type Mode,
  THEME_STORAGE_KEYS,
} from './theme'

function setLS(preset: Preset | null, mode: Mode | null) {
  if (preset === null) localStorage.removeItem(THEME_STORAGE_KEYS.preset)
  else localStorage.setItem(THEME_STORAGE_KEYS.preset, preset)
  if (mode === null) localStorage.removeItem(THEME_STORAGE_KEYS.mode)
  else localStorage.setItem(THEME_STORAGE_KEYS.mode, mode)
}

describe('theme runtime', () => {
  beforeEach(() => {
    document.documentElement.className = ''
    setLS(null, null)
  })

  it('applyTheme sets the matching html class', () => {
    applyTheme('houfeng', 'dark')
    expect(document.documentElement.classList.contains('theme-houfeng-dark')).toBe(true)

    applyTheme('classic', 'light')
    expect(document.documentElement.classList.contains('theme-classic-light')).toBe(true)
    expect(document.documentElement.classList.contains('theme-houfeng-dark')).toBe(false)
  })

  it('detectInitialTheme defaults to houfeng + system', () => {
    const t = detectInitialTheme()
    expect(t.preset).toBe('houfeng')
    expect(t.mode).toBe('system')
  })

  it('detectInitialTheme reads localStorage', () => {
    setLS('classic', 'dark')
    const t = detectInitialTheme()
    expect(t.preset).toBe('classic')
    expect(t.mode).toBe('dark')
  })

  it('preferredScheme returns dark when matchMedia matches', () => {
    vi.spyOn(window, 'matchMedia').mockImplementation((q: string) => ({
      matches: q.includes('dark'),
      media: q, addListener: () => {}, removeListener: () => {},
      addEventListener: () => {}, removeEventListener: () => {}, dispatchEvent: () => false,
    }) as unknown as MediaQueryList)
    expect(preferredScheme()).toBe('dark')
  })
})
```

- [ ] **Step 2: Run — fail**

```bash
cd web && npx vitest run src/lib/theme.test.ts
```
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

```ts
export type Preset = 'houfeng' | 'classic'
export type Mode = 'dark' | 'light' | 'system'
export type Scheme = 'dark' | 'light'

export const THEME_STORAGE_KEYS = {
  preset: 'houfeng.theme.preset',
  mode:   'houfeng.theme.mode',
} as const

export interface ThemeChoice { preset: Preset; mode: Mode }

const PRESET_VALUES: ReadonlySet<Preset> = new Set(['houfeng', 'classic'])
const MODE_VALUES:   ReadonlySet<Mode>   = new Set(['dark', 'light', 'system'])

export function detectInitialTheme(): ThemeChoice {
  const presetRaw = safeLocalStorage(THEME_STORAGE_KEYS.preset)
  const modeRaw   = safeLocalStorage(THEME_STORAGE_KEYS.mode)
  return {
    preset: PRESET_VALUES.has(presetRaw as Preset) ? (presetRaw as Preset) : 'houfeng',
    mode:   MODE_VALUES.has(modeRaw as Mode)       ? (modeRaw as Mode)     : 'system',
  }
}

export function preferredScheme(): Scheme {
  if (typeof window === 'undefined' || !window.matchMedia) return 'dark'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function resolveScheme(mode: Mode): Scheme {
  return mode === 'system' ? preferredScheme() : mode
}

export function applyTheme(preset: Preset, mode: Mode | Scheme): void {
  const scheme: Scheme = (mode === 'system') ? preferredScheme() : (mode as Scheme)
  const cls = `theme-${preset}-${scheme}`
  const html = document.documentElement
  for (const c of Array.from(html.classList)) {
    if (c.startsWith('theme-')) html.classList.remove(c)
  }
  html.classList.add(cls)
}

export function persistTheme(choice: ThemeChoice): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEYS.preset, choice.preset)
    localStorage.setItem(THEME_STORAGE_KEYS.mode,   choice.mode)
  } catch {
    /* private mode etc. — ignore */
  }
}

export function subscribeSystemScheme(cb: (s: Scheme) => void): () => void {
  if (typeof window === 'undefined' || !window.matchMedia) return () => {}
  const mql = window.matchMedia('(prefers-color-scheme: dark)')
  const listener = (e: MediaQueryListEvent) => cb(e.matches ? 'dark' : 'light')
  mql.addEventListener('change', listener)
  return () => mql.removeEventListener('change', listener)
}

function safeLocalStorage(key: string): string | null {
  try { return localStorage.getItem(key) } catch { return null }
}
```

- [ ] **Step 4: Run — pass**

```bash
cd web && npx vitest run src/lib/theme.test.ts
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/theme.ts web/src/lib/theme.test.ts
git commit -m "Add theme runtime"
```

---

### Task 6: FOUC inline script (`lib/theme-fouc.ts` + `index.html`)

**Files:**
- Create: `web/src/lib/theme-fouc.ts`
- Modify: `web/index.html`

- [ ] **Step 1: Write the helper**

`web/src/lib/theme-fouc.ts`:

```ts
// Returns the synchronous inline script body that applies the theme class to <html> before
// React hydrates. Keep duplicated in index.html until vite-plugin-html or similar is added.
export const FOUC_SCRIPT = `
(function () {
  try {
    var p = localStorage.getItem('houfeng.theme.preset') || 'houfeng';
    var m = localStorage.getItem('houfeng.theme.mode')   || 'system';
    var scheme = m === 'system'
      ? (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
      : m;
    document.documentElement.classList.add('theme-' + p + '-' + scheme);
  } catch (e) { /* ignore */ }
})();
`.trim()
```

- [ ] **Step 2: Inline the same body into `web/index.html`**

In `<head>`, before any `<link rel="stylesheet">`:

```html
<script>
(function () {
  try {
    var p = localStorage.getItem('houfeng.theme.preset') || 'houfeng';
    var m = localStorage.getItem('houfeng.theme.mode')   || 'system';
    var scheme = m === 'system'
      ? (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
      : m;
    document.documentElement.classList.add('theme-' + p + '-' + scheme);
  } catch (e) { /* ignore */ }
})();
</script>
```

Also add font preload hints for the most-used weights:

```html
<link rel="preload" as="font" type="font/woff2" href="/fonts/source-han-sans-sc-400.woff2" crossorigin>
<link rel="preload" as="font" type="font/woff2" href="/fonts/source-han-sans-sc-500.woff2" crossorigin>
<link rel="preload" as="font" type="font/woff2" href="/fonts/source-han-serif-sc-500.woff2" crossorigin>
```

- [ ] **Step 3: Build**

Run: `cd web && npm run build`
Expected: green; the inline script reaches dist/index.html intact.

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/theme-fouc.ts web/index.html
git commit -m "Inline theme FOUC script and font preload hints"
```

---

### Task 7: Theme React context (`lib/theme-context.tsx`)

**Files:**
- Create: `web/src/lib/theme-context.tsx`
- Test: `web/src/lib/theme-context.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, beforeEach } from 'vitest'
import { ThemeProvider, useTheme } from './theme-context'

function Probe() {
  const { preset, mode, setPreset, setMode } = useTheme()
  return (
    <div>
      <span data-testid="state">{preset}/{mode}</span>
      <button onClick={() => setPreset('classic')}>classic</button>
      <button onClick={() => setMode('light')}>light</button>
    </div>
  )
}

describe('ThemeProvider', () => {
  beforeEach(() => {
    document.documentElement.className = ''
    localStorage.clear()
  })

  it('starts at houfeng/system and applies html class', () => {
    render(<ThemeProvider><Probe /></ThemeProvider>)
    expect(screen.getByTestId('state')).toHaveTextContent('houfeng/system')
    expect(document.documentElement.className).toMatch(/^theme-houfeng-(dark|light)$/)
  })

  it('switching preset updates storage and class', () => {
    render(<ThemeProvider><Probe /></ThemeProvider>)
    fireEvent.click(screen.getByText('classic'))
    expect(localStorage.getItem('houfeng.theme.preset')).toBe('classic')
    expect(document.documentElement.className).toMatch(/^theme-classic-/)
  })

  it('switching mode to light overrides system', () => {
    render(<ThemeProvider><Probe /></ThemeProvider>)
    fireEvent.click(screen.getByText('light'))
    expect(localStorage.getItem('houfeng.theme.mode')).toBe('light')
    expect(document.documentElement.classList.contains('theme-houfeng-light')).toBe(true)
  })
})
```

- [ ] **Step 2: Run — fail**

`cd web && npx vitest run src/lib/theme-context.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

```tsx
import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import {
  applyTheme,
  detectInitialTheme,
  persistTheme,
  subscribeSystemScheme,
  type Preset,
  type Mode,
} from './theme'

interface ThemeContextValue {
  preset: Preset
  mode: Mode
  setPreset: (p: Preset) => void
  setMode: (m: Mode) => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [{ preset, mode }, setChoice] = useState(detectInitialTheme)

  // apply on mount + on every change
  useEffect(() => {
    applyTheme(preset, mode)
  }, [preset, mode])

  // re-apply on system change when mode === 'system'
  useEffect(() => {
    if (mode !== 'system') return
    return subscribeSystemScheme(() => applyTheme(preset, 'system'))
  }, [mode, preset])

  const setPreset = useCallback((next: Preset) => {
    setChoice(prev => {
      const updated = { ...prev, preset: next }
      persistTheme(updated)
      return updated
    })
  }, [])

  const setMode = useCallback((next: Mode) => {
    setChoice(prev => {
      const updated = { ...prev, mode: next }
      persistTheme(updated)
      return updated
    })
  }, [])

  return (
    <ThemeContext.Provider value={{ preset, mode, setPreset, setMode }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be inside <ThemeProvider>')
  return ctx
}
```

- [ ] **Step 4: Run — pass**

`cd web && npx vitest run src/lib/theme-context.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/theme-context.tsx web/src/lib/theme-context.test.tsx
git commit -m "Add theme React context"
```

---

### Task 8: Fetch wrapper with 401 hook (`lib/fetcher.ts`)

**Files:**
- Create: `web/src/lib/fetcher.ts`
- Test: `web/src/lib/fetcher.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fetcher, setUnauthorizedHandler } from './fetcher'

describe('fetcher', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    setUnauthorizedHandler(undefined)
  })

  it('passes credentials: include', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    )
    await fetcher('/api/x')
    expect(fetchSpy).toHaveBeenCalledWith('/api/x', expect.objectContaining({ credentials: 'include' }))
  })

  it('returns parsed JSON on success', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ value: 7 }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    )
    expect(await fetcher<{ value: number }>('/api/x')).toEqual({ value: 7 })
  })

  it('throws AuthError on 401 and calls handler', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 401 }))
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    await expect(fetcher('/api/x')).rejects.toThrow(/unauthenticated/i)
    expect(handler).toHaveBeenCalledTimes(1)
  })

  it('throws Error on other non-2xx', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: 'nope' }), { status: 500, headers: { 'Content-Type': 'application/json' } })
    )
    await expect(fetcher('/api/x')).rejects.toThrow(/nope|500/)
  })
})
```

- [ ] **Step 2: Run — fail**

- [ ] **Step 3: Write the implementation**

```ts
export class AuthError extends Error {
  constructor() { super('unauthenticated') }
}

let onUnauthorized: (() => void) | undefined
export function setUnauthorizedHandler(handler: (() => void) | undefined) {
  onUnauthorized = handler
}

export async function fetcher<T = unknown>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { credentials: 'include', ...init })
  if (res.status === 401) {
    onUnauthorized?.()
    throw new AuthError()
  }
  if (!res.ok) {
    let detail = ''
    try { detail = await res.text() } catch { /* ignore */ }
    throw new Error(`request failed (${res.status}): ${detail}`.trim())
  }
  if (res.status === 204) return undefined as unknown as T
  const ct = res.headers.get('Content-Type') ?? ''
  if (ct.includes('application/json')) return res.json() as Promise<T>
  return (await res.text()) as unknown as T
}
```

- [ ] **Step 4: Run — pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/fetcher.ts web/src/lib/fetcher.test.ts
git commit -m "Add fetch wrapper with 401 hook"
```

---

### Task 9: Auth API client (`lib/auth-client.ts`)

**Files:**
- Create: `web/src/lib/auth-client.ts`
- Test: `web/src/lib/auth-client.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect, vi } from 'vitest'
import { login, logout, me, changePassword, type User } from './auth-client'

describe('auth-client', () => {
  it('login posts JSON and returns user', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ user_id: 'u1', username: 'admin', role: 'admin', display_name: '管理员' }),
        { status: 200, headers: { 'Content-Type': 'application/json' } })
    )
    const u: User = await login('admin', 'pw')
    expect(u.username).toBe('admin')
    const [, init] = fetchSpy.mock.calls[0]
    expect(init?.method).toBe('POST')
    expect(JSON.parse(String(init?.body))).toEqual({ username: 'admin', password: 'pw' })
  })

  it('logout posts to /api/auth/logout', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 204 }))
    await logout()
  })

  it('me returns parsed user', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }),
        { status: 200, headers: { 'Content-Type': 'application/json' } })
    )
    const u = await me()
    expect(u?.user_id).toBe('u1')
  })

  it('me returns null on 401', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 401 }))
    expect(await me()).toBeNull()
  })

  it('changePassword puts JSON', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 204 }))
    await changePassword('old', 'new-correct-horse-battery')
    expect(String(fetchSpy.mock.calls[0][1]?.method)).toBe('PUT')
  })
})
```

- [ ] **Step 2: Run — fail**

- [ ] **Step 3: Write the implementation**

```ts
import { fetcher, AuthError } from './fetcher'

export interface User {
  user_id: string
  username: string
  role: string
  display_name: string
}

export async function login(username: string, password: string): Promise<User> {
  return fetcher<User>('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
}

export async function logout(): Promise<void> {
  await fetcher<void>('/api/auth/logout', { method: 'POST' })
}

export async function me(): Promise<User | null> {
  try {
    return await fetcher<User>('/api/auth/me')
  } catch (e) {
    if (e instanceof AuthError) return null
    throw e
  }
}

export async function changePassword(oldPassword: string, newPassword: string): Promise<void> {
  await fetcher<void>('/api/auth/password', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
  })
}
```

- [ ] **Step 4: Run — pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/auth-client.ts web/src/lib/auth-client.test.ts
git commit -m "Add auth API client"
```

---

### Task 10: Auth React context (`lib/auth-context.tsx`)

**Files:**
- Create: `web/src/lib/auth-context.tsx`
- Test: `web/src/lib/auth-context.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { AuthProvider, useAuth } from './auth-context'
import * as client from './auth-client'

function Probe() {
  const { user, loading, login, logout } = useAuth()
  if (loading) return <div>loading</div>
  return (
    <div>
      <span data-testid="user">{user?.username ?? 'none'}</span>
      <button onClick={() => login('admin', 'pw')}>in</button>
      <button onClick={() => logout()}>out</button>
    </div>
  )
}

describe('AuthProvider', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('boots with /me result', async () => {
    vi.spyOn(client, 'me').mockResolvedValue({ user_id: 'u1', username: 'admin', role: 'admin', display_name: '' })
    render(<AuthProvider><Probe /></AuthProvider>)
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
  })

  it('sets user after login', async () => {
    vi.spyOn(client, 'me').mockResolvedValue(null)
    vi.spyOn(client, 'login').mockResolvedValue({ user_id: 'u1', username: 'admin', role: 'admin', display_name: '' })
    render(<AuthProvider><Probe /></AuthProvider>)
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('none'))
    fireEvent.click(screen.getByText('in'))
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
  })

  it('clears user after logout', async () => {
    vi.spyOn(client, 'me').mockResolvedValue({ user_id: 'u1', username: 'admin', role: 'admin', display_name: '' })
    vi.spyOn(client, 'logout').mockResolvedValue()
    render(<AuthProvider><Probe /></AuthProvider>)
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
    fireEvent.click(screen.getByText('out'))
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('none'))
  })
})
```

- [ ] **Step 2: Run — fail**

- [ ] **Step 3: Write the implementation**

```tsx
import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from 'react'
import * as client from './auth-client'
import { setUnauthorizedHandler } from './fetcher'

export interface AuthValue {
  user: client.User | null
  loading: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
}

const Ctx = createContext<AuthValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<client.User | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    setUser(await client.me())
  }, [])

  useEffect(() => {
    setUnauthorizedHandler(() => setUser(null))
    refresh().finally(() => setLoading(false))
  }, [refresh])

  const login = useCallback(async (u: string, p: string) => {
    const fresh = await client.login(u, p)
    setUser(fresh)
  }, [])

  const logout = useCallback(async () => {
    await client.logout()
    setUser(null)
  }, [])

  return <Ctx.Provider value={{ user, loading, login, logout, refresh }}>{children}</Ctx.Provider>
}

export function useAuth(): AuthValue {
  const v = useContext(Ctx)
  if (!v) throw new Error('useAuth must be inside <AuthProvider>')
  return v
}
```

- [ ] **Step 4: Run — pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/auth-context.tsx web/src/lib/auth-context.test.tsx
git commit -m "Add auth React context"
```

---

### Task 11: `<Button>` atom

**Files:**
- Create: `web/src/components/atoms/Button.tsx`
- Create: `web/src/components/atoms/Button.test.tsx`

Per spec §9.1: 4 variants × 3 sizes + states. Button is a styled `<button>` accepting `variant`, `size`, plus normal HTML props.

- [ ] **Step 1: Write the failing test**

```tsx
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Button } from './Button'

describe('Button', () => {
  it('renders children', () => {
    render(<Button>登 录</Button>)
    expect(screen.getByRole('button', { name: '登 录' })).toBeInTheDocument()
  })

  it('applies variant class', () => {
    render(<Button variant="danger">清空</Button>)
    expect(screen.getByRole('button')).toHaveClass('btn--danger')
  })

  it('applies size class', () => {
    render(<Button size="sm">小</Button>)
    expect(screen.getByRole('button')).toHaveClass('btn--sm')
  })

  it('default variant is primary, default size is md', () => {
    render(<Button>X</Button>)
    const b = screen.getByRole('button')
    expect(b).toHaveClass('btn--primary')
    expect(b).toHaveClass('btn--md')
  })

  it('disabled passes through', () => {
    render(<Button disabled>X</Button>)
    expect(screen.getByRole('button')).toBeDisabled()
  })
})
```

- [ ] **Step 2: Run — fail**

- [ ] **Step 3: Implementation**

```tsx
import type { ButtonHTMLAttributes } from 'react'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md' | 'lg'

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
}

export function Button({ variant = 'primary', size = 'md', className = '', ...rest }: ButtonProps) {
  const classes = ['btn', `btn--${variant}`, `btn--${size}`, className].filter(Boolean).join(' ')
  return <button className={classes} {...rest} />
}
```

Add to `web/src/styles/atoms.css` (also create this file and import from `main.tsx` later):

```css
.btn {
  display: inline-flex; align-items: center; gap: var(--space-2);
  font-family: var(--font-sans); font-weight: var(--type-link-weight);
  border-radius: var(--radius-2);
  border: var(--border-w) solid transparent;
  cursor: pointer; transition: background-color 0.12s, border-color 0.12s, box-shadow 0.12s;
  white-space: nowrap;
}
.btn--sm { padding: 5px 11px; font-size: 11px; border-radius: var(--radius-1); }
.btn--md { padding: 8px 16px; font-size: 13px; }
.btn--lg { padding: 11px 22px; font-size: 14px; border-radius: 8px; }

.btn--primary {
  background: linear-gradient(180deg, color-mix(in srgb, var(--accent) 20%, transparent) 0%, color-mix(in srgb, var(--accent) 10%, transparent) 100%);
  border-color: var(--accent); color: var(--text-primary);
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--accent) 15%, transparent) inset;
}
.btn--primary:hover { box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 12%, transparent); }
.btn--primary:active { background: color-mix(in srgb, var(--accent) 30%, transparent); }

.btn--secondary { background: var(--surface); border-color: var(--border); color: var(--text-primary); }
.btn--secondary:hover { background: var(--surface-elevated); }

.btn--ghost { color: var(--accent); background: transparent; border-color: transparent; }
.btn--ghost:hover { background: var(--accent-soft); }

.btn--danger { color: color-mix(in srgb, var(--color-state-critical) 80%, white 20%); border-color: var(--color-state-critical); background: color-mix(in srgb, var(--color-state-critical) 12%, transparent); }
.btn--danger:hover { background: color-mix(in srgb, var(--color-state-critical) 20%, transparent); }

.btn:disabled { color: var(--text-disabled); cursor: not-allowed; opacity: 0.6; }
.btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
```

- [ ] **Step 4: Run — pass**

`cd web && npx vitest run src/components/atoms/Button.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/atoms/Button.tsx web/src/components/atoms/Button.test.tsx web/src/styles/atoms.css
git commit -m "Add Button atom with 4 variants × 3 sizes"
```

---

### Task 12: `<Input>` atom

**Files:**
- Create: `web/src/components/atoms/Input.tsx` + test
- Modify: `web/src/styles/atoms.css`

- [ ] **Step 1: Failing test**

```tsx
import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { Input } from './Input'

describe('Input', () => {
  it('renders label and input', () => {
    render(<Input label="用户名" id="u" />)
    expect(screen.getByLabelText('用户名')).toBeInTheDocument()
  })
  it('shows error message when invalid', () => {
    render(<Input label="X" error="格式不正确" />)
    expect(screen.getByText('格式不正确')).toBeInTheDocument()
    expect(screen.getByRole('textbox')).toHaveClass('input--error')
  })
  it('forwards onChange', () => {
    const onChange = vi.fn()
    render(<Input label="X" onChange={onChange} />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'a' } })
    expect(onChange).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Implementation**

```tsx
import { forwardRef, type InputHTMLAttributes, useId } from 'react'

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string
  error?: string
  hint?: string
  prefix?: React.ReactNode
  suffix?: React.ReactNode
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { label, error, hint, prefix, suffix, id, className = '', ...rest }, ref,
) {
  const fallbackId = useId()
  const inputId = id ?? fallbackId
  const cls = ['input', error ? 'input--error' : '', className].filter(Boolean).join(' ')
  return (
    <div className="input-field">
      {label && <label className="input-field__label" htmlFor={inputId}>{label}</label>}
      <div className="input-field__shell">
        {prefix && <span className="input-field__prefix">{prefix}</span>}
        <input ref={ref} id={inputId} className={cls} {...rest} />
        {suffix && <span className="input-field__suffix">{suffix}</span>}
      </div>
      {error
        ? <div className="input-field__error">{error}</div>
        : hint ? <div className="input-field__hint">{hint}</div> : null}
    </div>
  )
})
```

CSS to append to `atoms.css`:

```css
.input-field { display: flex; flex-direction: column; gap: 6px; }
.input-field__label { font-family: var(--font-sans); font-size: 11px; letter-spacing: 0.15em; color: var(--text-muted); }
.input-field__shell { position: relative; display: flex; align-items: center; }
.input-field__prefix, .input-field__suffix { position: absolute; color: var(--text-muted); font-size: 13px; pointer-events: none; }
.input-field__prefix { left: 12px; }
.input-field__suffix { right: 12px; }
.input { width: 100%; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-2); padding: 9px 12px; color: var(--text-primary); font: var(--type-body-weight) var(--type-body-size)/1 var(--font-sans); outline: none; }
.input:focus { background: var(--accent-soft); border-color: var(--accent); box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 15%, transparent); }
.input--error { background: color-mix(in srgb, var(--color-state-critical) 6%, var(--surface)); border-color: var(--color-state-critical); }
.input-field__error { font-size: 11px; color: color-mix(in srgb, var(--color-state-critical) 70%, white 30%); }
.input-field__hint { font-size: 11px; color: var(--text-muted); }
```

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/components/atoms/Input.tsx web/src/components/atoms/Input.test.tsx web/src/styles/atoms.css
git commit -m "Add Input atom"
```

---

### Task 13: `<Badge>` atom (state + info + count variants)

**Files:**
- Create: `web/src/components/atoms/Badge.tsx` + test
- Modify: `web/src/styles/atoms.css`

- [ ] **Step 1: Failing test**

```tsx
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Badge } from './Badge'

describe('Badge', () => {
  it('state renders state class + serif text', () => {
    render(<Badge variant="state" tone="critical">严重</Badge>)
    const el = screen.getByText('严重')
    expect(el).toHaveClass('badge--state', 'tone--critical')
  })
  it('info variant default', () => {
    render(<Badge>tcp</Badge>)
    expect(screen.getByText('tcp')).toHaveClass('badge--info')
  })
  it('count variant', () => {
    render(<Badge variant="count" tone="critical">3</Badge>)
    expect(screen.getByText('3')).toHaveClass('badge--count')
  })
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Implementation**

```tsx
import type { ReactNode } from 'react'

export type BadgeVariant = 'state' | 'info' | 'count'
export type BadgeTone = 'normal' | 'notice' | 'alert' | 'critical' | 'maintenance' | 'offline' | 'neutral'

export interface BadgeProps {
  children: ReactNode
  variant?: BadgeVariant
  tone?: BadgeTone
  className?: string
  withDot?: boolean
}

export function Badge({ children, variant = 'info', tone = 'neutral', className = '', withDot = false }: BadgeProps) {
  const cls = ['badge', `badge--${variant}`, tone !== 'neutral' && `tone--${tone}`, className].filter(Boolean).join(' ')
  return (
    <span className={cls}>
      {withDot && <span className="badge__dot" aria-hidden />}
      {children}
    </span>
  )
}
```

CSS appended:

```css
.badge { display: inline-flex; align-items: center; gap: 6px; }
.badge--state { padding: 3px 10px; border-radius: var(--radius-pill); font-family: var(--font-serif); font-size: var(--type-state-size); letter-spacing: var(--type-state-tracking); border: 1px solid var(--border); background: var(--surface); color: var(--text-primary); }
.badge--info  { padding: 2px 8px;  border-radius: var(--radius-1); font-family: var(--font-sans);  font-size: 11px; border: 1px solid var(--border); background: var(--surface); color: var(--text-primary); }
.badge--count { padding: 0 5px; min-width: 18px; height: 18px; border-radius: var(--radius-pill); justify-content: center; font-family: var(--font-mono); font-size: 10px; font-weight: 600; background: rgba(255,255,255,0.08); color: var(--text-primary); }

.badge__dot { width: 5px; height: 5px; border-radius: 50%; background: var(--text-muted); }

.tone--normal      { color: var(--color-state-normal);     border-color: color-mix(in srgb, var(--color-state-normal) 30%, transparent); background: color-mix(in srgb, var(--color-state-normal) 10%, transparent); }
.tone--normal      .badge__dot { background: var(--color-state-normal); }
.tone--notice      { color: var(--color-state-notice);     border-color: color-mix(in srgb, var(--color-state-notice) 30%, transparent); background: color-mix(in srgb, var(--color-state-notice) 10%, transparent); }
.tone--notice      .badge__dot { background: var(--color-state-notice); }
.tone--alert       { color: var(--color-state-alert);      border-color: color-mix(in srgb, var(--color-state-alert) 30%, transparent); background: color-mix(in srgb, var(--color-state-alert) 10%, transparent); }
.tone--alert       .badge__dot { background: var(--color-state-alert); }
.tone--critical    { color: color-mix(in srgb, var(--color-state-critical) 70%, white 30%); border-color: color-mix(in srgb, var(--color-state-critical) 30%, transparent); background: color-mix(in srgb, var(--color-state-critical) 10%, transparent); }
.tone--critical    .badge__dot { background: var(--color-state-critical); }
.tone--maintenance { color: var(--color-state-maintenance); border-color: color-mix(in srgb, var(--color-state-maintenance) 30%, transparent); background: color-mix(in srgb, var(--color-state-maintenance) 10%, transparent); }
.tone--maintenance .badge__dot { background: var(--color-state-maintenance); }
.tone--offline     { color: var(--text-muted); border-style: dashed; border-color: var(--border-dashed); background: color-mix(in srgb, var(--color-state-offline) 6%, transparent); }
.tone--offline     .badge__dot { background: var(--color-state-offline); }
```

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/components/atoms/Badge.tsx web/src/components/atoms/Badge.test.tsx web/src/styles/atoms.css
git commit -m "Add Badge atom (state/info/count × 6 tones)"
```

---

### Task 14: `<Card>` atom

**Files:**
- Create: `web/src/components/atoms/Card.tsx` + test
- Modify: `web/src/styles/atoms.css`

- [ ] **Step 1: Failing test**

```tsx
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Card } from './Card'

describe('Card', () => {
  it('default role', () => {
    render(<Card>X</Card>)
    expect(screen.getByText('X').parentElement).toHaveClass('card', 'card--default')
  })
  it('state card with tone', () => {
    render(<Card role="state" tone="alert">X</Card>)
    expect(screen.getByText('X').parentElement).toHaveClass('card--state', 'tone--alert')
  })
  it('accent card', () => {
    render(<Card role="accent">X</Card>)
    expect(screen.getByText('X').parentElement).toHaveClass('card--accent')
  })
  it('warning card', () => {
    render(<Card role="warning">X</Card>)
    expect(screen.getByText('X').parentElement).toHaveClass('card--warning')
  })
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Implementation**

```tsx
import type { HTMLAttributes, ReactNode } from 'react'

export type CardRole = 'default' | 'state' | 'accent' | 'warning'
export type CardTone = 'normal' | 'notice' | 'alert' | 'critical' | 'maintenance' | 'offline'

export interface CardProps extends HTMLAttributes<HTMLDivElement> {
  role?: CardRole
  tone?: CardTone
  children: ReactNode
}

export function Card({ role = 'default', tone, className = '', children, ...rest }: CardProps) {
  const classes = ['card', `card--${role}`, tone && `tone--${tone}`, className].filter(Boolean).join(' ')
  return <div className={classes} {...rest}>{children}</div>
}
```

CSS:

```css
.card { padding: var(--space-4); border-radius: var(--radius-2); background: var(--surface); border: 1px solid var(--border); }
.card--state { border-left-width: 2px; border-left-style: solid; border-left-color: var(--text-muted); }
.card--state.tone--normal      { border-left-color: var(--color-state-normal); }
.card--state.tone--notice      { border-left-color: var(--color-state-notice); }
.card--state.tone--alert       { border-left-color: var(--color-state-alert); }
.card--state.tone--critical    { border-left-color: var(--color-state-critical); }
.card--state.tone--maintenance { border-left-color: var(--color-state-maintenance); }
.card--state.tone--offline     { border-style: dashed; border-color: var(--border-dashed); opacity: 0.85; }

.card--accent { background: color-mix(in srgb, var(--accent) 8%, var(--surface)); border-color: var(--accent-border); }
.card--warning { background: color-mix(in srgb, var(--color-state-critical) 6%, var(--surface)); border-color: color-mix(in srgb, var(--color-state-critical) 30%, transparent); border-style: dashed; }
```

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/components/atoms/Card.tsx web/src/components/atoms/Card.test.tsx web/src/styles/atoms.css
git commit -m "Add Card atom (4 roles × tones)"
```

---

### Task 15: `<Tabs>` atom (underline + pill)

**Files:**
- Create: `web/src/components/atoms/Tabs.tsx` + test
- Modify: `web/src/styles/atoms.css`

- [ ] **Step 1: Failing test**

```tsx
import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { Tabs } from './Tabs'

const items = [
  { value: 'a', label: '概览' },
  { value: 'b', label: '指标趋势' },
  { value: 'c', label: '活跃异常', count: 2 },
]

describe('Tabs', () => {
  it('renders all tabs and marks active', () => {
    render(<Tabs items={items} value="a" onChange={() => {}} />)
    const active = screen.getByRole('tab', { selected: true })
    expect(active).toHaveTextContent('概览')
  })
  it('calls onChange', () => {
    const onChange = vi.fn()
    render(<Tabs items={items} value="a" onChange={onChange} />)
    fireEvent.click(screen.getByText('指标趋势'))
    expect(onChange).toHaveBeenCalledWith('b')
  })
  it('renders count badge', () => {
    render(<Tabs items={items} value="a" onChange={() => {}} />)
    expect(screen.getByText('2')).toHaveClass('badge--count')
  })
  it('pill variant uses pill class', () => {
    render(<Tabs items={items} value="a" onChange={() => {}} variant="pill" />)
    expect(screen.getByRole('tablist')).toHaveClass('tabs--pill')
  })
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Implementation**

```tsx
import { Badge } from './Badge'

export interface TabItem<V extends string = string> {
  value: V
  label: string
  count?: number
}
export interface TabsProps<V extends string = string> {
  items: TabItem<V>[]
  value: V
  onChange: (next: V) => void
  variant?: 'underline' | 'pill'
}

export function Tabs<V extends string = string>({ items, value, onChange, variant = 'underline' }: TabsProps<V>) {
  const cls = ['tabs', `tabs--${variant}`].join(' ')
  return (
    <div className={cls} role="tablist">
      {items.map(item => {
        const selected = item.value === value
        return (
          <button
            key={item.value}
            role="tab"
            aria-selected={selected}
            className={['tab', selected && 'is-active'].filter(Boolean).join(' ')}
            onClick={() => onChange(item.value)}
            type="button"
          >
            <span>{item.label}</span>
            {typeof item.count === 'number' && item.count > 0 && (
              <Badge variant="count" tone="notice">{item.count}</Badge>
            )}
          </button>
        )
      })}
    </div>
  )
}
```

CSS:

```css
.tabs--underline { display: flex; gap: var(--space-6); border-bottom: 1px solid var(--border); }
.tabs--underline .tab { background: none; border: none; color: var(--text-muted); padding: 8px 0; margin-bottom: -1px; font-family: var(--font-sans); font-size: 13px; font-weight: 500; display: inline-flex; align-items: center; gap: 5px; }
.tabs--underline .tab.is-active { color: var(--text-primary); border-bottom: 2px solid var(--accent); }
.tabs--pill { display: inline-flex; padding: 3px; background: var(--surface-elevated); border: 1px solid var(--border); border-radius: 8px; gap: 2px; }
.tabs--pill .tab { background: none; border: none; padding: 6px 14px; color: var(--text-muted); font-family: var(--font-sans); font-size: 12px; border-radius: 6px; }
.tabs--pill .tab.is-active { background: var(--accent-soft); color: var(--text-primary); font-weight: 500; }
```

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/components/atoms/Tabs.tsx web/src/components/atoms/Tabs.test.tsx web/src/styles/atoms.css
git commit -m "Add Tabs atom (underline + pill variants)"
```

---

### Task 16: `<Toggle>` atom

**Files:**
- Create: `web/src/components/atoms/Toggle.tsx` + test
- Modify: `web/src/styles/atoms.css`

- [ ] **Step 1: Failing test**

```tsx
import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { Toggle } from './Toggle'

describe('Toggle', () => {
  it('renders aria-checked', () => {
    render(<Toggle checked label="启用" onChange={() => {}} />)
    const t = screen.getByRole('switch', { name: '启用' })
    expect(t).toHaveAttribute('aria-checked', 'true')
  })
  it('clicking calls onChange with inverse', () => {
    const onChange = vi.fn()
    render(<Toggle checked={false} label="X" onChange={onChange} />)
    fireEvent.click(screen.getByRole('switch'))
    expect(onChange).toHaveBeenCalledWith(true)
  })
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Implementation**

```tsx
export interface ToggleProps {
  checked: boolean
  onChange: (next: boolean) => void
  label?: string
  disabled?: boolean
}

export function Toggle({ checked, onChange, label, disabled }: ToggleProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      className={['toggle', checked && 'is-on'].filter(Boolean).join(' ')}
      onClick={() => onChange(!checked)}
    >
      <span className="toggle__thumb" />
    </button>
  )
}
```

CSS:

```css
.toggle { width: 36px; height: 20px; padding: 0; border-radius: 10px; background: var(--border); border: none; position: relative; cursor: pointer; }
.toggle.is-on { background: var(--accent); }
.toggle__thumb { position: absolute; top: 2px; left: 2px; width: 16px; height: 16px; border-radius: 50%; background: var(--bg); transition: left 0.15s; }
.toggle.is-on .toggle__thumb { left: 18px; }
.toggle:disabled { opacity: 0.5; cursor: not-allowed; }
.toggle:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
```

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/components/atoms/Toggle.tsx web/src/components/atoms/Toggle.test.tsx web/src/styles/atoms.css
git commit -m "Add Toggle atom"
```

---

### Task 17: Atoms barrel export

**Files:**
- Create: `web/src/components/atoms/index.ts`

- [ ] **Step 1: Write**

```ts
export * from './Button'
export * from './Input'
export * from './Badge'
export * from './Card'
export * from './Tabs'
export * from './Toggle'
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/atoms/index.ts
git commit -m "Add atoms barrel export"
```

---

### Task 18: `<UserChip>` (sidebar bottom)

**Files:**
- Create: `web/src/app/layout/UserChip.tsx` + test

Behavior: shows user.username + user.role (rendered as 「管理员」 when role === 'admin'), with caret. Click opens floating menu with: 「主题设置」 (link to /settings#theme), 「修改密码」 (opens modal — modal in Task 24), divider, 「退出登录」 (calls logout()).

- [ ] **Step 1: Failing test**

```tsx
import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { UserChip } from './UserChip'

describe('UserChip', () => {
  const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

  it('shows username and role label', () => {
    render(<MemoryRouter><UserChip user={user} onLogout={() => {}} onChangePassword={() => {}} /></MemoryRouter>)
    expect(screen.getByText('admin')).toBeInTheDocument()
    expect(screen.getByText('管理员')).toBeInTheDocument()
  })

  it('does not display single-user phrasing', () => {
    render(<MemoryRouter><UserChip user={user} onLogout={() => {}} onChangePassword={() => {}} /></MemoryRouter>)
    expect(screen.queryByText(/单用户|全权限|个人系统/)).toBeNull()
  })

  it('opens menu on click', () => {
    render(<MemoryRouter><UserChip user={user} onLogout={() => {}} onChangePassword={() => {}} /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: /admin/ }))
    expect(screen.getByText('退出登录')).toBeInTheDocument()
  })

  it('logout button calls onLogout', () => {
    const onLogout = vi.fn()
    render(<MemoryRouter><UserChip user={user} onLogout={onLogout} onChangePassword={() => {}} /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: /admin/ }))
    fireEvent.click(screen.getByText('退出登录'))
    expect(onLogout).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Implementation**

```tsx
import { useState, useRef, useEffect } from 'react'
import { Link } from 'react-router-dom'
import type { User } from '../../lib/auth-client'

const ROLE_LABEL: Record<string, string> = { admin: '管理员' }

export interface UserChipProps {
  user: User
  onLogout: () => void
  onChangePassword: () => void
}

export function UserChip({ user, onLogout, onChangePassword }: UserChipProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  const initial = (user.display_name || user.username).slice(0, 1)
  const roleLabel = ROLE_LABEL[user.role] ?? user.role

  return (
    <div className="user-chip" ref={ref}>
      <button type="button" className="user-chip__trigger" onClick={() => setOpen(v => !v)} aria-label={`${user.username} menu`}>
        <span className="user-chip__avatar">{initial}</span>
        <span className="user-chip__body">
          <span className="user-chip__name">{user.display_name || user.username}</span>
          <span className="user-chip__role">{roleLabel}</span>
        </span>
        <span className="user-chip__caret">{open ? '▴' : '▾'}</span>
      </button>
      {open && (
        <div className="user-chip__menu" role="menu">
          <Link to="/settings#theme" className="user-chip__menu-item" onClick={() => setOpen(false)} role="menuitem">主题设置</Link>
          <button type="button" className="user-chip__menu-item" onClick={() => { setOpen(false); onChangePassword() }} role="menuitem">修改密码</button>
          <div className="user-chip__divider" />
          <button type="button" className="user-chip__menu-item user-chip__menu-item--danger" onClick={() => { setOpen(false); onLogout() }} role="menuitem">退出登录</button>
        </div>
      )}
    </div>
  )
}
```

CSS (append to a new `web/src/app/layout/layout.css`, imported from main):

```css
.user-chip { position: relative; }
.user-chip__trigger { display: flex; align-items: center; gap: 9px; padding: 8px 10px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-2); width: 100%; cursor: pointer; }
.user-chip__avatar { width: 26px; height: 26px; border-radius: 50%; background: linear-gradient(135deg, var(--accent), color-mix(in srgb, var(--accent) 60%, black 40%)); display: flex; align-items: center; justify-content: center; font-family: var(--font-serif); font-size: 11px; color: var(--bg); }
.user-chip__body { flex: 1; min-width: 0; text-align: left; }
.user-chip__name { display: block; font-family: var(--font-sans); font-size: 12px; color: var(--text-primary); }
.user-chip__role { display: block; font-family: var(--font-serif); font-size: 10px; color: var(--text-muted); letter-spacing: 0.1em; }
.user-chip__caret { color: var(--text-muted); font-size: 10px; }
.user-chip__menu { position: absolute; left: 0; right: 0; bottom: calc(100% + 6px); background: var(--surface-elevated); border: 1px solid var(--border); border-radius: 8px; box-shadow: var(--shadow-overlay); padding: 5px; z-index: 10; }
.user-chip__menu-item { display: flex; align-items: center; gap: 8px; width: 100%; padding: 8px 10px; background: none; border: none; text-align: left; font-family: var(--font-sans); font-size: 12px; color: var(--text-primary); border-radius: 5px; cursor: pointer; }
.user-chip__menu-item:hover { background: var(--surface); }
.user-chip__menu-item--danger { color: color-mix(in srgb, var(--color-state-critical) 75%, white 25%); }
.user-chip__divider { height: 1px; background: var(--border); margin: 4px 0; }
```

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/app/layout/UserChip.tsx web/src/app/layout/UserChip.test.tsx web/src/app/layout/layout.css
git commit -m "Add UserChip with dropdown menu"
```

---

### Task 19: `<SyncStatus>` (sidebar)

**Files:**
- Create: `web/src/app/layout/SyncStatus.tsx` + test

- [ ] **Step 1: Failing test**

```tsx
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { SyncStatus } from './SyncStatus'

describe('SyncStatus', () => {
  it('shows ok with timestamp', () => {
    render(<SyncStatus state="ok" version="v1.0" lastSync="2026-04-29T14:32:01Z" />)
    expect(screen.getByText('中心运行正常')).toBeInTheDocument()
    expect(screen.getByText(/v1\.0/)).toBeInTheDocument()
  })
  it('shows degraded state', () => {
    render(<SyncStatus state="degraded" version="v1.0" lastSync="2026-04-29T14:32:01Z" />)
    expect(screen.getByText('中心运行降级')).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Implementation**

```tsx
export interface SyncStatusProps {
  state: 'ok' | 'degraded' | 'down'
  version: string
  lastSync: string
}

const LABEL = { ok: '中心运行正常', degraded: '中心运行降级', down: '中心不可达' } as const
const TONE = { ok: 'normal', degraded: 'notice', down: 'critical' } as const

export function SyncStatus({ state, version, lastSync }: SyncStatusProps) {
  const time = new Date(lastSync).toLocaleTimeString('zh-CN', { hour12: false })
  return (
    <div className={`sync-status sync-status--${state}`}>
      <div className="sync-status__line">
        <span className="sync-status__dot" />
        <span className="sync-status__label">{LABEL[state]}</span>
      </div>
      <div className="sync-status__meta">{version} · sync {time}</div>
    </div>
  )
}
```

CSS:

```css
.sync-status { padding: 10px 12px; border-radius: var(--radius-2); border: 1px solid var(--border); }
.sync-status--ok       { background: color-mix(in srgb, var(--color-state-normal) 6%, transparent); border-color: color-mix(in srgb, var(--color-state-normal) 25%, transparent); }
.sync-status--degraded { background: color-mix(in srgb, var(--color-state-notice) 6%, transparent); border-color: color-mix(in srgb, var(--color-state-notice) 25%, transparent); }
.sync-status--down     { background: color-mix(in srgb, var(--color-state-critical) 6%, transparent); border-color: color-mix(in srgb, var(--color-state-critical) 25%, transparent); }
.sync-status__line { display: flex; align-items: center; gap: 6px; }
.sync-status__dot  { width: 6px; height: 6px; border-radius: 50%; }
.sync-status--ok       .sync-status__dot { background: var(--color-state-normal);   box-shadow: 0 0 8px color-mix(in srgb, var(--color-state-normal) 50%, transparent); }
.sync-status--degraded .sync-status__dot { background: var(--color-state-notice); }
.sync-status--down     .sync-status__dot { background: var(--color-state-critical); }
.sync-status__label { font-family: var(--font-sans); font-size: 11px; }
.sync-status--ok       .sync-status__label { color: var(--color-state-normal); }
.sync-status--degraded .sync-status__label { color: var(--color-state-notice); }
.sync-status--down     .sync-status__label { color: color-mix(in srgb, var(--color-state-critical) 70%, white 30%); }
.sync-status__meta { font-family: var(--font-mono); font-size: 9px; color: var(--text-muted); margin-top: 3px; }
```

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/app/layout/SyncStatus.tsx web/src/app/layout/SyncStatus.test.tsx web/src/app/layout/layout.css
git commit -m "Add SyncStatus card for sidebar"
```

---

### Task 20: `<Sidebar>` (compose nav + sync + user)

**Files:**
- Create: `web/src/app/layout/Sidebar.tsx` + test

Sidebar accepts a `currentPath`-driven active state via React Router's `<NavLink>`, which already exists. Render brand + nav (uses `PRIMARY_NAV_ITEMS` from `metadata.ts`) + flex spacer + `<SyncStatus>` + `<UserChip>`.

- [ ] **Step 1: Failing test**

```tsx
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from './Sidebar'

const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

describe('Sidebar', () => {
  it('renders brand and 5 nav items', () => {
    render(<MemoryRouter>
      <Sidebar user={user} sync={{ state: 'ok', version: 'v1.0', lastSync: '2026-04-29T14:32:01Z' }}
               anomalyCounts={{ nodes: 3, targets: 1 }}
               onLogout={() => {}} onChangePassword={() => {}} />
    </MemoryRouter>)
    expect(screen.getByText('候风')).toBeInTheDocument()
    for (const label of ['首页', '节点', '目标', '事件', '设置']) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }
  })
  it('renders anomaly counts on nav', () => {
    render(<MemoryRouter>
      <Sidebar user={user} sync={{ state: 'ok', version: 'v1.0', lastSync: '2026-04-29T14:32:01Z' }}
               anomalyCounts={{ nodes: 3, targets: 1 }}
               onLogout={() => {}} onChangePassword={() => {}} />
    </MemoryRouter>)
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('1')).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Update `metadata.ts`**

```ts
import type { Path } from 'react-router-dom'
export interface NavItem { to: string; label: string; end?: boolean }

export const PRODUCT_NAME_ZH = '候风'
export const PRODUCT_NAME_EN = 'Houfeng'
export const PRODUCT_FULL_NAME_ZH = '候风 · 服务器舰队控制面'
export const PRODUCT_FULL_NAME_EN = 'Houfeng Fleet Control Plane'

export const PRIMARY_NAV_ITEMS: NavItem[] = [
  { to: '/',         label: '首页', end: true },
  { to: '/nodes',    label: '节点' },
  { to: '/targets',  label: '目标' },
  { to: '/events',   label: '事件' },
  { to: '/settings', label: '设置' },
]
```

> Note: this restores the spec §10.1 navigation labeling ("首页"). The previous "集群概览" string is removed.

- [ ] **Step 4: Implementation**

```tsx
import { NavLink } from 'react-router-dom'
import {
  PRODUCT_NAME_ZH,
  PRODUCT_FULL_NAME_EN,
  PRIMARY_NAV_ITEMS,
} from '../metadata'
import { Badge } from '../../components/atoms'
import type { User } from '../../lib/auth-client'
import { SyncStatus, type SyncStatusProps } from './SyncStatus'
import { UserChip } from './UserChip'

export interface SidebarProps {
  user: User
  sync: SyncStatusProps
  anomalyCounts: { nodes: number; targets: number }
  onLogout: () => void
  onChangePassword: () => void
}

const COUNT_TONE: Record<string, 'critical' | 'alert' | 'notice'> = {
  nodes: 'critical',
  targets: 'alert',
}

export function Sidebar({ user, sync, anomalyCounts, onLogout, onChangePassword }: SidebarProps) {
  return (
    <aside className="sidebar">
      <div className="sidebar__brand">
        <p className="sidebar__brand-zh">{PRODUCT_NAME_ZH}</p>
        <p className="sidebar__brand-en">{PRODUCT_FULL_NAME_EN.toUpperCase()}</p>
      </div>
      <nav className="sidebar__nav" aria-label="主导航">
        {PRIMARY_NAV_ITEMS.map(item => {
          const count =
            item.to === '/nodes'   ? anomalyCounts.nodes :
            item.to === '/targets' ? anomalyCounts.targets : 0
          const tone = COUNT_TONE[item.to.replace('/', '')] ?? 'notice'
          return (
            <NavLink key={item.to} to={item.to} end={item.end}
                     className={({ isActive }) => `sidebar__nav-link${isActive ? ' is-active' : ''}`}>
              <span>{item.label}</span>
              {count > 0 && <Badge variant="count" tone={tone}>{count}</Badge>}
            </NavLink>
          )
        })}
      </nav>
      <div className="sidebar__spacer" />
      <SyncStatus {...sync} />
      <UserChip user={user} onLogout={onLogout} onChangePassword={onChangePassword} />
    </aside>
  )
}
```

CSS appended:

```css
.sidebar { display: flex; flex-direction: column; gap: var(--space-2); padding: var(--space-5) var(--space-4); background: var(--bg-sidebar); border-right: 1px solid var(--border); width: 220px; min-height: 100vh; }
.sidebar__brand { margin-bottom: var(--space-6); }
.sidebar__brand-zh { font-family: var(--font-serif); font-size: var(--type-h1-size); font-weight: 500; letter-spacing: 0.06em; color: var(--text-primary); }
.sidebar__brand-en { font-family: var(--font-sans); font-size: 9px; color: var(--text-muted); letter-spacing: 0.25em; margin-top: 3px; }
.sidebar__nav { display: flex; flex-direction: column; gap: 2px; }
.sidebar__nav-link { display: flex; align-items: center; justify-content: space-between; padding: 8px 10px; border-radius: 6px; font-family: var(--font-sans); font-size: 13px; color: var(--text-secondary); }
.sidebar__nav-link:hover { background: var(--surface); }
.sidebar__nav-link.is-active { background: var(--accent-soft); border: 1px solid var(--accent-border); color: var(--text-primary); padding: 7px 9px; }
.sidebar__spacer { flex: 1; }
```

- [ ] **Step 5: Pass**

- [ ] **Step 6: Commit**

```bash
git add web/src/app/layout/Sidebar.tsx web/src/app/layout/Sidebar.test.tsx web/src/app/metadata.ts web/src/app/layout/layout.css
git commit -m "Replace Sidebar with token-driven shell + revert nav labels per spec"
```

---

### Task 21: `<RequireAuth>` route guard

**Files:**
- Create: `web/src/app/RequireAuth.tsx` + test

- [ ] **Step 1: Failing test**

```tsx
import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { RequireAuth } from './RequireAuth'
import * as authCtx from '../lib/auth-context'

function Protected() { return <div>secret</div> }
function Login() { return <div>login</div> }

describe('RequireAuth', () => {
  it('renders children when authenticated', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      user: { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' },
      loading: false,
      login: vi.fn(), logout: vi.fn(), refresh: vi.fn(),
    })
    render(<MemoryRouter initialEntries={['/x']}>
      <Routes>
        <Route element={<RequireAuth />}>
          <Route path="/x" element={<Protected />} />
        </Route>
        <Route path="/login" element={<Login />} />
      </Routes>
    </MemoryRouter>)
    expect(screen.getByText('secret')).toBeInTheDocument()
  })

  it('redirects to /login when unauthenticated', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      user: null, loading: false,
      login: vi.fn(), logout: vi.fn(), refresh: vi.fn(),
    })
    render(<MemoryRouter initialEntries={['/x']}>
      <Routes>
        <Route element={<RequireAuth />}>
          <Route path="/x" element={<Protected />} />
        </Route>
        <Route path="/login" element={<Login />} />
      </Routes>
    </MemoryRouter>)
    expect(screen.getByText('login')).toBeInTheDocument()
  })

  it('shows nothing while loading', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      user: null, loading: true,
      login: vi.fn(), logout: vi.fn(), refresh: vi.fn(),
    })
    const { container } = render(<MemoryRouter initialEntries={['/x']}>
      <Routes>
        <Route element={<RequireAuth />}>
          <Route path="/x" element={<Protected />} />
        </Route>
      </Routes>
    </MemoryRouter>)
    expect(container.querySelector('[data-testid="secret"]')).toBeNull()
  })
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Implementation**

```tsx
import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '../lib/auth-context'

export function RequireAuth() {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return null
  if (!user) {
    return <Navigate to={`/login?next=${encodeURIComponent(location.pathname + location.search)}`} replace />
  }
  return <Outlet />
}
```

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/app/RequireAuth.tsx web/src/app/RequireAuth.test.tsx
git commit -m "Add RequireAuth route guard"
```

---

### Task 22: `<LoginPage>`

**Files:**
- Create: `web/src/pages/LoginPage.tsx` + test

- [ ] **Step 1: Failing test**

```tsx
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { LoginPage } from './LoginPage'
import * as authCtx from '../lib/auth-context'

describe('LoginPage', () => {
  it('does not display single-user phrasing', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      user: null, loading: false, login: vi.fn(), logout: vi.fn(), refresh: vi.fn(),
    })
    render(<MemoryRouter><LoginPage /></MemoryRouter>)
    expect(screen.queryByText(/单用户|全权限|个人系统/)).toBeNull()
  })
  it('submits credentials', async () => {
    const login = vi.fn().mockResolvedValue(undefined)
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      user: null, loading: false, login, logout: vi.fn(), refresh: vi.fn(),
    })
    render(<MemoryRouter><LoginPage /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'pw1234567' } })
    fireEvent.click(screen.getByRole('button', { name: /登/ }))
    await waitFor(() => expect(login).toHaveBeenCalledWith('admin', 'pw1234567'))
  })
  it('shows error on bad credentials', async () => {
    const login = vi.fn().mockRejectedValue(new Error('request failed (401):'))
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      user: null, loading: false, login, logout: vi.fn(), refresh: vi.fn(),
    })
    render(<MemoryRouter><LoginPage /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'wrongpwd' } })
    fireEvent.click(screen.getByRole('button', { name: /登/ }))
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
  })
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Implementation**

```tsx
import { useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Button, Input } from '../components/atoms'
import { useAuth } from '../lib/auth-context'

export function LoginPage() {
  const { login } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()
  const [params] = useSearchParams()

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await login(username, password)
      const next = params.get('next') ?? '/'
      navigate(next, { replace: true })
    } catch {
      setError('用户名或密码不正确')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="login-page">
      <div className="login-page__seal" aria-hidden>候</div>
      <form className="login-page__card" onSubmit={onSubmit}>
        <header className="login-page__brand">
          <div className="login-page__brand-zh">候风</div>
          <div className="login-page__brand-en">FLEET CONTROL PLANE</div>
          <div className="login-page__motto">察 变 · 守 望</div>
        </header>
        {error && <div role="alert" className="login-page__error">{error}</div>}
        <Input label="用户名" autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} />
        <Input label="密码" type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} />
        <Button type="submit" disabled={submitting} variant="primary">登 录</Button>
        <footer className="login-page__footer">v1.0</footer>
      </form>
    </div>
  )
}
```

CSS in a new `web/src/pages/LoginPage.css` imported by the page:

```css
.login-page { min-height: 100vh; display: flex; align-items: center; justify-content: center; background: var(--bg); position: relative; overflow: hidden; }
.login-page::before { content: ''; position: absolute; inset: 0; background: var(--bg-aurora); pointer-events: none; }
.login-page__seal { position: absolute; top: 24px; left: 24px; width: 36px; height: 36px; border: 1px solid var(--accent-border); display: flex; align-items: center; justify-content: center; transform: rotate(45deg); font-family: var(--font-serif); color: var(--accent); font-size: 14px; }
.login-page__seal::after { content: ''; }
.login-page__card { position: relative; width: 380px; padding: 32px 32px 28px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-3); display: flex; flex-direction: column; gap: 12px; }
.login-page__brand { text-align: center; margin-bottom: 16px; }
.login-page__brand-zh { font-family: var(--font-serif); font-size: 32px; font-weight: 500; letter-spacing: 0.08em; color: var(--text-primary); }
.login-page__brand-en { font-family: var(--font-sans); font-size: 10px; color: var(--text-muted); letter-spacing: 0.30em; margin-top: 6px; }
.login-page__motto { font-family: var(--font-serif); font-size: 11px; color: var(--text-muted); letter-spacing: 0.2em; margin-top: 4px; }
.login-page__error { padding: 9px 12px; background: color-mix(in srgb, var(--color-state-critical) 8%, transparent); border: 1px solid color-mix(in srgb, var(--color-state-critical) 30%, transparent); border-radius: var(--radius-2); color: color-mix(in srgb, var(--color-state-critical) 75%, white 25%); font-size: 12px; }
.login-page__footer { text-align: center; font-family: var(--font-sans); font-size: 10px; color: var(--text-muted); margin-top: 14px; }
.login-page__seal::after { content: '候'; transform: rotate(-45deg); /* visual fallback if seal char hidden */ }
```

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/LoginPage.tsx web/src/pages/LoginPage.test.tsx web/src/pages/LoginPage.css
git commit -m "Add login page"
```

---

### Task 23: `<AppShell>` rewrite + router updates

**Files:**
- Modify: `web/src/app/layout/AppShell.tsx`
- Modify: `web/src/app/layout/AppShell.test.tsx`
- Modify: `web/src/app/router.tsx`
- Modify: `web/src/main.tsx`

- [ ] **Step 1: Rewrite AppShell**

```tsx
import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { useAuth } from '../../lib/auth-context'
import { useDashboard } from '../../lib/api' // existing hook supplying anomaly counts; verify signature
import { ChangePasswordModal } from './ChangePasswordModal'
import { useState } from 'react'

export function AppShell() {
  const { user, logout } = useAuth()
  const dashboard = useDashboard() // { nodes_anomaly: number, targets_anomaly: number, sync_state, version, last_sync }
  const [changePwOpen, setChangePwOpen] = useState(false)
  if (!user) return null

  return (
    <div className="app-shell">
      <Sidebar
        user={user}
        sync={{ state: dashboard?.sync_state ?? 'ok', version: dashboard?.version ?? 'v1.0', lastSync: dashboard?.last_sync ?? new Date().toISOString() }}
        anomalyCounts={{ nodes: dashboard?.nodes_anomaly ?? 0, targets: dashboard?.targets_anomaly ?? 0 }}
        onLogout={logout}
        onChangePassword={() => setChangePwOpen(true)}
      />
      <main className="app-shell__main"><Outlet /></main>
      {changePwOpen && <ChangePasswordModal onClose={() => setChangePwOpen(false)} />}
    </div>
  )
}
```

CSS appended:

```css
.app-shell { display: grid; grid-template-columns: 220px 1fr; min-height: 100vh; }
.app-shell__main { padding: var(--space-6) var(--space-8); overflow-x: hidden; }
```

> If `useDashboard` doesn't yet return sync/version/anomaly counts, file a Plan 3 follow-up to fold them into `/api/dashboard`. For now, AppShell can pull `useFetch('/api/dashboard')` and provide reasonable defaults. The AppShell test mocks the hook.

- [ ] **Step 2: Replace `app/router.tsx`**

```tsx
import { createBrowserRouter, RouterProvider, Navigate } from 'react-router-dom'
import { AppShell } from './layout/AppShell'
import { RequireAuth } from './RequireAuth'
import { DashboardPage } from '../pages/DashboardPage'
import { NodesPage } from '../pages/NodesPage'
import { NodeDetailPage } from '../pages/NodeDetailPage'
import { NodeOnboardingPage } from '../pages/NodeOnboardingPage'
import { TargetsPage } from '../pages/TargetsPage'
import { TargetDetailPage } from '../pages/TargetDetailPage'
import { EventsPage } from '../pages/EventsPage'
import { SettingsPage } from '../pages/SettingsPage'
import { LoginPage } from '../pages/LoginPage'

const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  {
    element: <RequireAuth />,
    children: [
      {
        element: <AppShell />,
        children: [
          { index: true, element: <DashboardPage /> },
          { path: 'nodes', element: <NodesPage /> },
          { path: 'nodes/:nodeId', element: <NodeDetailPage /> },
          { path: 'nodes/:nodeId/onboarding', element: <NodeOnboardingPage /> },
          { path: 'targets', element: <TargetsPage /> },
          { path: 'targets/:targetId', element: <TargetDetailPage /> },
          { path: 'events', element: <EventsPage /> },
          { path: 'settings', element: <SettingsPage /> },
          { path: '*', element: <Navigate to="/" replace /> },
        ],
      },
    ],
  },
])

export function AppRouter() { return <RouterProvider router={router} /> }
```

- [ ] **Step 3: Wire AuthProvider + ThemeProvider in `main.tsx`**

```tsx
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles/reset.css'
import './styles/tokens.css'
import './styles/fonts.css'
import './styles/atoms.css'
import { AppRouter } from './app/router'
import { AuthProvider } from './lib/auth-context'
import { ThemeProvider } from './lib/theme-context'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <AuthProvider>
        <AppRouter />
      </AuthProvider>
    </ThemeProvider>
  </StrictMode>,
)
```

Delete `web/src/index.css`.

- [ ] **Step 4: Run tsc + tests**

Run: `cd web && npm run build && npx vitest run`
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add web/src/app/layout/AppShell.tsx web/src/app/layout/AppShell.test.tsx web/src/app/router.tsx web/src/main.tsx
git rm web/src/index.css || true
git commit -m "Rewire AppShell, router, and main entry on the new foundation"
```

---

### Task 24: Change-password modal

**Files:**
- Create: `web/src/app/layout/ChangePasswordModal.tsx` + test

- [ ] **Step 1: Failing test**

```tsx
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { ChangePasswordModal } from './ChangePasswordModal'
import * as client from '../../lib/auth-client'

describe('ChangePasswordModal', () => {
  it('submits old + new password', async () => {
    const spy = vi.spyOn(client, 'changePassword').mockResolvedValue()
    render(<ChangePasswordModal onClose={() => {}} />)
    fireEvent.change(screen.getByLabelText('当前密码'),  { target: { value: 'old-correct-horse' } })
    fireEvent.change(screen.getByLabelText('新密码'),    { target: { value: 'new-correct-horse' } })
    fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'new-correct-horse' } })
    fireEvent.click(screen.getByRole('button', { name: '确认修改' }))
    await waitFor(() => expect(spy).toHaveBeenCalledWith('old-correct-horse', 'new-correct-horse'))
  })
  it('rejects mismatch', () => {
    render(<ChangePasswordModal onClose={() => {}} />)
    fireEvent.change(screen.getByLabelText('当前密码'),  { target: { value: 'old-correct-horse' } })
    fireEvent.change(screen.getByLabelText('新密码'),    { target: { value: 'new-correct-horse' } })
    fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'wrong-confirm-pwd' } })
    fireEvent.click(screen.getByRole('button', { name: '确认修改' }))
    expect(screen.getByRole('alert')).toHaveTextContent('两次输入不一致')
  })
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Implementation**

```tsx
import { useState, type FormEvent } from 'react'
import { Button, Input } from '../../components/atoms'
import { changePassword } from '../../lib/auth-client'

export interface ChangePasswordModalProps {
  onClose: () => void
}

export function ChangePasswordModal({ onClose }: ChangePasswordModalProps) {
  const [oldPw, setOldPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [done, setDone] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    if (newPw !== confirmPw) { setError('两次输入不一致'); return }
    if (newPw.length < 8) { setError('新密码至少 8 个字符'); return }
    setSubmitting(true)
    try {
      await changePassword(oldPw, newPw)
      setDone(true)
      setTimeout(onClose, 1200)
    } catch {
      setError('修改失败：当前密码可能不正确')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="modal-backdrop" onMouseDown={onClose}>
      <form className="modal" onSubmit={onSubmit} onMouseDown={e => e.stopPropagation()}>
        <h2 className="modal__title">修改密码</h2>
        {error && <div role="alert" className="modal__error">{error}</div>}
        {done && <div role="status" className="modal__success">已修改</div>}
        <Input label="当前密码" type="password" value={oldPw} onChange={e => setOldPw(e.target.value)} autoFocus />
        <Input label="新密码" type="password" value={newPw} onChange={e => setNewPw(e.target.value)} />
        <Input label="确认新密码" type="password" value={confirmPw} onChange={e => setConfirmPw(e.target.value)} />
        <div className="modal__actions">
          <Button type="button" variant="secondary" onClick={onClose}>取消</Button>
          <Button type="submit" variant="primary" disabled={submitting}>确认修改</Button>
        </div>
      </form>
    </div>
  )
}
```

CSS appended to layout.css:

```css
.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.55); display: flex; align-items: center; justify-content: center; z-index: 100; }
.modal { width: 380px; padding: var(--space-6); background: var(--surface-elevated); border: 1px solid var(--border); border-radius: var(--radius-3); box-shadow: var(--shadow-overlay); display: flex; flex-direction: column; gap: 12px; }
.modal__title { font-family: var(--font-sans); font-size: var(--type-h2-size); color: var(--text-primary); }
.modal__error { font-size: 12px; color: color-mix(in srgb, var(--color-state-critical) 75%, white 25%); }
.modal__success { font-size: 12px; color: var(--color-state-normal); }
.modal__actions { display: flex; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-2); }
```

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/app/layout/ChangePasswordModal.tsx web/src/app/layout/ChangePasswordModal.test.tsx web/src/app/layout/layout.css
git commit -m "Add change-password modal"
```

---

### Task 25: Settings page — add 「主题」 Pill Tab

**Files:**
- Modify: `web/src/pages/SettingsPage.tsx`
- Modify: `web/src/pages/SettingsPage.test.tsx`

- [ ] **Step 1: Failing test (append)**

```tsx
it('shows Theme tab and switches preset/mode', async () => {
  render(<MemoryRouter><ThemeProvider><SettingsPage /></ThemeProvider></MemoryRouter>)
  fireEvent.click(screen.getByRole('tab', { name: '主题' }))
  fireEvent.click(screen.getByText('经典'))
  expect(document.documentElement.className).toMatch(/^theme-classic-/)
  fireEvent.click(screen.getByText('浅色'))
  expect(document.documentElement.className).toBe('theme-classic-light')
})
```

- [ ] **Step 2: Fail**

- [ ] **Step 3: Edit `SettingsPage.tsx`**

Add a new tab "主题" to the existing Pill Tab strip. Tab content:

```tsx
import { useTheme, type Preset, type Mode } from '../lib/theme-context'
import { Tabs } from '../components/atoms'

function ThemeTabContent() {
  const { preset, mode, setPreset, setMode } = useTheme()
  return (
    <div className="theme-settings">
      <section>
        <h3 className="settings-section__title">风格</h3>
        <Tabs<Preset>
          variant="pill"
          value={preset}
          onChange={setPreset}
          items={[
            { value: 'houfeng', label: '候风原色' },
            { value: 'classic', label: '经典' },
          ]}
        />
      </section>
      <section style={{ marginTop: 'var(--space-6)' }}>
        <h3 className="settings-section__title">明暗</h3>
        <Tabs<Mode>
          variant="pill"
          value={mode}
          onChange={setMode}
          items={[
            { value: 'dark', label: '深色' },
            { value: 'light', label: '浅色' },
            { value: 'system', label: '跟随系统' },
          ]}
        />
      </section>
    </div>
  )
}
```

Wire it as the 5th tab. Existing tabs (Telegram / 频率档位 / 默认规则 / 数据保留) keep their current content; theme tab is purely client-side.

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/SettingsPage.tsx web/src/pages/SettingsPage.test.tsx
git commit -m "Add Theme tab to settings page"
```

---

### Task 26: Update existing `lib/api.ts` to use fetcher

**Files:**
- Modify: `web/src/lib/api.ts`

- [ ] **Step 1: Replace direct `fetch` calls**

Throughout `api.ts`, replace `fetch(...)` with `fetcher(...)` from `./fetcher`. This makes every existing query benefit from credentials: include + 401 redirect via the auth context handler.

- [ ] **Step 2: Run all tests**

Run: `cd web && npm run build && npx vitest run`
Expected: green.

- [ ] **Step 3: Commit**

```bash
git add web/src/lib/api.ts
git commit -m "Route api.ts through credentialed fetcher"
```

---

### Task 27: Final verification + commit

**Files:**
- (verify only)

- [ ] **Step 1: Build + lint + test**

```bash
cd web && npm run build && npm run lint && npx vitest run
```
Expected: all green.

- [ ] **Step 2: Run Go side untouched**

```bash
cd /home/murray/code/houfeng && make verify-go
```
Expected: green (Plan 1 already merged; this plan adds no Go changes).

- [ ] **Step 3: Manual smoke**

Run the center against a local Postgres with seed creds. Open the SPA:
- Without cookie → redirects to `/login`
- Login with seeded creds → redirected to `/`
- Theme Tab in 设置 → switching preset/mode flips the page in real time
- Refresh page → persisted theme survives, no FOUC flash
- 修改密码 modal → posts new password, can re-login

Document outcomes in PR description (no doc commit here).

- [ ] **Step 4: No-op commit (optional)**

If verification needed any small fix, commit it. Otherwise nothing to commit.

---

## Acceptance criteria

- All Vitest tests green: `cd web && npx vitest run`
- `npm run build` green
- `npm run lint` green
- 4 themes switchable in Settings → Theme tab; selection survives reload (`localStorage`)
- "跟随系统" updates immediately when OS scheme changes (DevTools emulation)
- Login flow works against Plan 1 backend; 401 from any /api/* triggers redirect to /login
- No "单用户 / 全权限 / 个人系统" string anywhere in `web/src/**/*.{ts,tsx}` (`grep -r '单用户\|全权限\|个人系统' web/src` returns 0 matches)
- Existing 8 page components still render inside the new shell (no broken pages); their content remains the legacy implementation pending Plan 3

## Cross-plan handoff

Plan 3 begins with the visual foundation in place. Plan 3 will:

- Replace the contents of each existing page (`Dashboard`, `Nodes`, `NodeDetail`, `NodeOnboarding`, `Targets`, `TargetDetail`, `Events`, `Settings`) with redesigned layouts using the atoms shipped in this plan
- Add page-specific styles into per-page `.css` files alongside each page component
- Update `useDashboard` (or its replacement) to expose `sync_state`, `version`, `last_sync`, `nodes_anomaly`, `targets_anomaly` so the sidebar can drop the defaults applied in Task 23
- Capture `docs/operations/visual-evidence/` updates for 4 themes × the 8 + login pages
- Cross-link `docs/design/v1-baseline/README.md` to mark the visual portion as superseded
