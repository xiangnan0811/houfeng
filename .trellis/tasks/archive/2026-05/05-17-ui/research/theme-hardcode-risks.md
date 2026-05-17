# Research: theme-token compliance risks

- **Query**: Research theme-token compliance risks in the Houfeng frontend for task `.trellis/tasks/05-17-ui`. Inspect CSS/TSX for hardcoded colors, rgba white/black usages, theme class assumptions, and components likely to break in houfeng light vs dark. Distinguish acceptable alpha overlays from suspect token bypasses.
- **Scope**: internal
- **Date**: 2026-05-17

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/styles/tokens.css` | Central theme token definitions for default houfeng dark plus `theme-houfeng-light`, `theme-classic-dark`, and `theme-classic-light`; hardcoded colors here are expected token source values. |
| `web/src/styles/atoms.css` | Main concentration of non-token color literals outside `tokens.css`, especially legacy atom states, Toggle, Drawer, Modal, tooltip shadows. |
| `web/src/styles/pages.css` | Mostly token-driven page styles; remaining non-token literals appear in probe/command error text, menu/dropdown shadows, and stderr output. |
| `web/src/pages/LoginPage.css` | Login-only CSS exception; mostly token-driven, with hardcoded black shadows and white color-mix for error text. |
| `web/src/app/layout/layout.css` | AppShell styles are largely token-driven; no non-token rgba/hex color hits outside normal `color-mix(..., transparent)` usage in inspected search. |
| `web/src/components/filters/filters.css` | Filter primitives are token-driven; content search hits were false positives from `white-space`, not color literals. |
| `web/src/components/atoms/Sparkline.tsx` | SVG color mapping uses CSS variables for tones; inline styles are layout/position only. |
| `web/src/components/atoms/MetricChart.tsx` | SVG chart colors use CSS variables for tones, threshold lines, axes, and maintenance windows; inline styles are layout/position only. |
| `web/src/components/atoms/StatusGlyph.tsx` | SVG glyph colors use CSS variables and `var(--bg)` for contrast strokes. |
| `web/src/components/atoms/Stepper.tsx` | Step colors map to CSS variables, but connector color is applied via inline `style={{ background: connectorColor }}`. |
| `web/src/lib/theme.ts` | Runtime theme class application; removes existing `theme-*` classes and adds `theme-${preset}-${scheme}`. |
| `web/src/lib/theme-context.tsx` | Theme provider applies selected theme and resubscribes to system scheme changes when mode is `system`. |
| `web/index.html` | Pre-React no-flash script reads localStorage and adds the initial theme class. |

### Code Patterns

#### Token system and acceptable token-source literals

- `web/src/styles/tokens.css` is the expected source of hardcoded theme color values. The file comment says theme classes on `<html>` override surface/accent variables and state semantic colors stay intentionally consistent across themes (`web/src/styles/tokens.css:1-6`).
- Default houfeng dark values live in `:root`, including rgba surfaces and shadows (`web/src/styles/tokens.css:41-110`). Example token-source usages include `--surface: rgba(255, 255, 255, 0.022)` and `--bg: #0B0E13` (`web/src/styles/tokens.css:52-56`).
- Houfeng light overrides the same semantic variables with light-safe values (`web/src/styles/tokens.css:114-162`), e.g. `--surface: #FBF7EC`, `--surface-elevated: #FFFEFA`, and darker state colors (`web/src/styles/tokens.css:117-147`).
- Classic dark/light have their own theme blocks (`web/src/styles/tokens.css:166-254`). Hardcoded rgba/hex inside these theme blocks is acceptable because these are token definitions, not component bypasses.
- Search found heavy token usage in CSS: 2616 `var(--...)` references across the main style files. Most gradients and overlays outside `tokens.css` use `color-mix(in srgb, var(--...), transparent)` rather than fixed RGB literals.

#### Suspect token bypasses in atoms.css

`web/src/styles/atoms.css` contains the densest set of non-token color literals outside token definitions:

- Ghost button hover uses a fixed white overlay instead of a surface/control token: `background: rgba(255, 255, 255, 0.05)` (`web/src/styles/atoms.css:61-62`). In houfeng light this remains a white wash rather than adapting to `--surface-pressed` / `--control-bg-hover`.
- Info badge uses fixed white background and border overlays (`web/src/styles/atoms.css:149-155`). The same literal white overlay is dark-theme-oriented.
- Tone state classes use hardcoded palette RGB values for borders/backgrounds while their foreground color uses semantic tokens (`web/src/styles/atoms.css:176-207`). Examples: normal uses `rgba(16, 185, 129, ...)` while `color: var(--color-state-normal)`, notice uses `rgba(245, 158, 11, ...)`, alert uses `rgba(249, 115, 22, ...)`, critical uses `rgba(239, 68, 68, ...)`, and maintenance uses `rgba(59, 130, 246, ...)`. These fixed Tailwind-like RGBs bypass the houfeng light/dark state token values.
- Offline tone uses white overlays instead of theme-derived border/surface tokens (`web/src/styles/atoms.css:211-215`).
- Card role variants hardcode accent/warning/dim colors (`web/src/styles/atoms.css:229-240`): `card--accent` uses `rgba(212, 160, 83, ...)`, `card--warning` uses `rgba(239, 68, 68, ...)`, and `card--dim` uses black/white overlays. These do not follow light-theme token values.
- Toggle off state uses fixed white overlays (`web/src/styles/atoms.css:460-464`), while the on state uses `var(--accent)` (`web/src/styles/atoms.css:471-477`). The off state is therefore dark-surface-specific.
- Drawer styles are dark-glass literals: overlay `rgba(0,0,0,0.6)`, drawer background `rgba(10, 10, 15, 0.85)`, border `rgba(255, 255, 255, 0.1)`, and header border `rgba(255, 255, 255, 0.05)` (`web/src/styles/atoms.css:479-484`). This is likely to stay visually dark in houfeng light.
- Modal overlay uses fixed black alpha (`web/src/styles/atoms.css:490-507`). The backdrop itself may be an acceptable modal dimming pattern, but its shadow literal duplicates theme shadow tokens.
- Tooltip shadows for sparkline/metric chart use fixed black alpha (`web/src/styles/atoms.css:664-685`, `web/src/styles/atoms.css:718-739`). These are overlay shadows rather than semantic colors, but they bypass `--shadow-overlay` / `--shadow-soft`.

#### Suspect token bypasses in pages.css

`web/src/styles/pages.css` is mostly token-driven; the remaining non-token color literals are localized:

- Probe observation error text mixes a state token with literal white: `color: color-mix(in srgb, var(--color-state-critical) 70%, white 30%)` (`web/src/styles/pages.css:5839-5843`). This deliberately lightens critical text, but literal `white` is fixed and may not match the light theme paper surface.
- Batch bar error repeats the same critical + white mix (`web/src/styles/pages.css:6599-6601`).
- Actions menu panel shadow uses fixed black alpha (`web/src/styles/pages.css:6005-6018`). This is an overlay shadow, but it bypasses theme shadow tokens.
- Command stderr output first sets token critical background and immediately overrides it with hardcoded critical RGB: `background: var(--color-state-critical); background: rgba(182, 73, 58, 0.08); border-color: rgba(182, 73, 58, 0.25)` (`web/src/styles/pages.css:6451-6455`). The hardcoded RGB happens to match the houfeng dark `--color-state-critical` hex, so houfeng light's darker critical token is bypassed.
- The only explicit light-theme selector outside `tokens.css` is a dashboard override: `html.theme-houfeng-light .dashboard-command-surface, html.theme-classic-light .dashboard-command-surface { --dashboard-command-panel: var(--panel-bg); --dashboard-command-panel-soft: var(--surface); }` (`web/src/styles/pages.css:353-357`). Search found `--dashboard-command-panel-soft` defined but not used elsewhere (`web/src/styles/pages.css:319-328`, `web/src/styles/pages.css:353-357`). This is a theme-class assumption, but not a hardcoded color.

#### Login page literals

- `web/src/pages/LoginPage.css` is the documented page-local CSS exception. It is mostly token-driven (`var(--bg)`, `var(--bg-aurora)`, `var(--surface-elevated)`, `var(--border)`, `var(--accent)`) (`web/src/pages/LoginPage.css:4-79`).
- Login card shadows use fixed black alpha (`web/src/pages/LoginPage.css:74-78`). These are shadow overlays, not semantic foreground/background colors, but they bypass theme shadow tokens.
- Error text mixes critical token with literal white (`web/src/pages/LoginPage.css:106-113`). This mirrors the pages.css critical text pattern and may be dark-theme-oriented.

#### Inline TSX styles and SVG colors

- Search for non-test TS/TSX color literals found no hardcoded hex or rgba color strings in production TSX.
- Several production TSX files use inline `style={{ color: 'var(...)' }}` or `style={{ fontSize: ... }}`. Examples include `SectionIntro` with `style={{ fontSize: 'var(--type-small-size)', color: 'var(--text-secondary)', lineHeight: 1.5 }}` (`web/src/pages/settings/SectionIntro.tsx:8`), error spans with `style={{ color: 'var(--color-state-critical)' }}` (`web/src/pages/target-detail/TargetProbeManagementSection.tsx:48`; `web/src/components/node-detail/NodeLabelsAndNote.tsx:87,102`; `web/src/components/target-detail/TargetLabelsAndNote.tsx:81,100`), and muted empty copy (`web/src/pages/node-detail/NodeContainersSection.tsx:74`). These use tokens rather than hardcoded colors, but they are still inline business styles.
- `Sparkline` maps all tones to CSS variables (`web/src/components/atoms/Sparkline.tsx:37-47`) and uses SVG `stroke={stroke}`, `fill={stroke}`, `stroke="var(--text-muted)"`, and `stroke="var(--bg)"` (`web/src/components/atoms/Sparkline.tsx:150-183`). Inline styles there are sizing/positioning (`web/src/components/atoms/Sparkline.tsx:82-89`, `web/src/components/atoms/Sparkline.tsx:132-135`, `web/src/components/atoms/Sparkline.tsx:195-201`, `web/src/components/atoms/Sparkline.tsx:209-225`).
- `MetricChart` maps tones and thresholds to CSS variables (`web/src/components/atoms/MetricChart.tsx:45-59`), renders maintenance windows with `fill="var(--color-state-maintenance)"` and opacity (`web/src/components/atoms/MetricChart.tsx:296-310`), axes with `stroke="var(--border)"` / `fill="var(--text-muted)"` (`web/src/components/atoms/MetricChart.tsx:336-388`), and hover dot stroke with `var(--bg)` (`web/src/components/atoms/MetricChart.tsx:437-459`). These are acceptable token-backed SVG colors.
- `StatusGlyph` maps health states to CSS variables (`web/src/components/atoms/StatusGlyph.tsx:18-25`) and uses token-backed fills/strokes plus `var(--bg)` contrast marks (`web/src/components/atoms/StatusGlyph.tsx:67-105`).
- `Stepper` maps step states to CSS variables (`web/src/components/atoms/Stepper.tsx:14-19`), uses token-backed SVG colors (`web/src/components/atoms/Stepper.tsx:41-85`), and applies connector color via inline `style={{ background: connectorColor }}` (`web/src/components/atoms/Stepper.tsx:118-123`). This is token-backed but still an inline color/background style.

#### Theme class assumptions and first-paint behavior

- Theme application uses explicit classes of the form `theme-${preset}-${scheme}`. `applyTheme` removes any existing `theme-*` class and adds the resolved class (`web/src/lib/theme.ts:36-44`).
- `ThemeProvider` applies the selected theme in an effect and, when `mode === 'system'`, updates on system scheme changes (`web/src/lib/theme-context.tsx:22-32`).
- The no-flash script in `web/index.html` reads raw localStorage values and adds `theme-` + preset + scheme without validating preset/mode (`web/index.html:8-18`). `detectInitialTheme` validates values in React (`web/src/lib/theme.ts:18-24`), so an invalid pre-paint class can exist briefly until React applies the validated class.
- Search found no CSS `@media (prefers-color-scheme: dark)` in production CSS. The only `prefers-color-scheme` usage is runtime scheme resolution in `web/index.html` and `web/src/lib/theme.ts` (`web/index.html:13-15`; `web/src/lib/theme.ts:27-30`, `web/src/lib/theme.ts:55-60`).
- Search found no component-level `.foo--dark` / `.foo--light` branch classes. Explicit theme selectors outside `tokens.css` are limited to the dashboard light override in `pages.css` (`web/src/styles/pages.css:353-357`).

#### Acceptable alpha overlays vs suspect token bypasses

| Category | Examples | Classification |
|---|---|---|
| Token definitions | `--surface: rgba(255, 255, 255, 0.022)` and theme hex values in `tokens.css` (`web/src/styles/tokens.css:52-110`, `web/src/styles/tokens.css:114-254`) | Acceptable; these define the token system. |
| Token-derived alpha overlays | `color-mix(in srgb, var(--accent) 18%, transparent)`, `color-mix(in srgb, var(--surface-elevated) 72%, transparent)`, state overlays from CSS variables throughout layout/pages | Acceptable; these adapt across theme token values. |
| SVG runtime colors from token maps | `Sparkline`, `MetricChart`, `StatusGlyph`, `Stepper` tone maps to `var(--...)` (`web/src/components/atoms/Sparkline.tsx:37-47`; `web/src/components/atoms/MetricChart.tsx:45-59`; `web/src/components/atoms/StatusGlyph.tsx:18-25`; `web/src/components/atoms/Stepper.tsx:14-19`) | Acceptable token-backed runtime color use. |
| Modal/backdrop dimming | Fixed black alpha overlays in modal/drawer/login/menu shadows (`web/src/styles/atoms.css:480`, `web/src/styles/atoms.css:494`, `web/src/pages/LoginPage.css:76-77`, `web/src/styles/pages.css:6018`) | Contextually plausible as dimming/shadow overlays, but they bypass shadow/backdrop tokens and remain dark in light themes. |
| Semantic state backgrounds/borders hardcoded as rgba | `.tone--*`, `.card--warning`, `.command-output--stderr` (`web/src/styles/atoms.css:176-207`, `web/src/styles/atoms.css:229-240`, `web/src/styles/pages.css:6451-6455`) | Suspect; these bypass `--color-state-*` values and houfeng light state tuning. |
| Literal white color mixing | critical text mixed with `white` (`web/src/pages/LoginPage.css:112`; `web/src/styles/pages.css:5842`, `web/src/styles/pages.css:6601`) | Suspect for light theme because it assumes white is the desired contrast mix endpoint. |
| Dark-glass component surfaces | Drawer background/borders in fixed dark rgba (`web/src/styles/atoms.css:482-484`) | High light-theme break risk; the component remains dark regardless of selected theme. |

### Related Specs

- `docs/design/v2-houfeng/design-language.md` — Visual authority says light theme must be equally usable, forbids pure black/white and hardcoded colors outside tokens (`docs/design/v2-houfeng/design-language.md:72-90`, `docs/design/v2-houfeng/design-language.md:111-114`).
- `.trellis/spec/web/styling-guidelines.md` — States all design tokens live in `tokens.css`; colors/spacing/type/radius/borders/shadows/motion should use variables, state derivatives should use `color-mix(... var(--color-state-xxx) ...)`, new tokens must be defined in all theme blocks, and components should not use dark/light branch classes (`.trellis/spec/web/styling-guidelines.md:38-75`).
- `.trellis/spec/web/styling-guidelines.md` — Inline style policy limits `style={{ ... }}` to sizing/calculation and forbids inline color/background/border/shadow/font business styling (`.trellis/spec/web/styling-guidelines.md:115-126`, `.trellis/spec/web/styling-guidelines.md:138-150`).
- `.trellis/spec/web/component-conventions.md` — Component-level convention says colors/spacing/type always use `tokens.css` variables and atoms should only depend on `tokens.css` / `atoms.css` (`.trellis/spec/web/component-conventions.md:13-18`, `.trellis/spec/web/component-conventions.md:21-36`).
- `.trellis/spec/web/quality-guidelines.md` — Checklist says changing CSS tokens requires synchronizing all four theme blocks and changing first-paint theme script must stay aligned with `web/src/lib/theme.ts` (`.trellis/spec/web/quality-guidelines.md:224-232`).

### External References

No external search was needed; the task is an internal code/spec compliance inventory.

## Caveats / Not Found

- No production TS/TSX hardcoded hex/rgb color literals were found outside CSS. TSX inline style hits use CSS variables or layout values, but some still violate the inline-style shape described in specs.
- The search used content patterns for `white` / `black`; many `white-space` matches are false positives and were excluded from the risk categories.
- Visual behavior was inferred from code and tokens only; no browser screenshots or computed contrast measurements were taken.
- `tokens.css` line comments in `.trellis/spec/web/styling-guidelines.md` appear slightly stale for exact token line ranges, but the contract itself matches the current token architecture.
