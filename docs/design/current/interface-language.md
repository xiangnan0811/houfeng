# Current interface language

## Intent

Houfeng should feel like a quiet engineering instrument: calm, high-density, legible for long sessions, and honest about what the system does and does not know.

The useful imagery from earlier exploration still applies as direction, not doctrine:

- observation and signal over decoration;
- restrained surfaces over marketing layout;
- dense but scannable work areas;
- state and evidence made visible without exaggerated dashboard theater.

Avoid cheap "Chinese style" decoration, large empty SaaS panels, neon monitoring-center theatrics, and UI that implies confidence the backend does not have.

## Visual defaults

Current UI defaults:

- dark-first theme with an equally usable light theme;
- CSS custom properties in `web/src/styles/tokens.css`;
- the current accent direction is operational blue (`--accent`) with amber secondary emphasis (`--accent-2`), kept restrained rather than neon;
- compact spacing based on the existing token scale;
- high-density tables for list scanning;
- cards for repeated items, modals, warnings, and contained tools, not for wrapping every page section;
- monospace treatment for IDs, hostnames, IPs, timestamps, versions, durations, prices, counts, and other technical facts;
- status shown by both color and shape where possible.

These defaults are expected for ordinary UI work. They can change through a task that updates the tokens/components/tests/docs together.

## State language

State UI should help the operator answer "what happened, where, how fresh is it, and what can I safely do next?"

Current health states include normal, notice, alert, critical, maintenance, and offline/paused. Severity or health color changes should be immediate, not animated as if the state were still settling.

Loading, error, and empty states should be local to the affected surface:

- Loading should state that data is loading and avoid fake precision.
- Errors should include a human summary, a bounded technical summary, and a retry path when retry is meaningful.
- Empty states should explain whether the absence is normal, a setup gap, or a filter result.

## Evidence language

Every UI, doc, PR, and final report should name the evidence level honestly:

- mock API proves representative frontend rendering, not backend correctness;
- local center sample proves local integration against disposable or manually entered data, not real inventory completeness;
- real data requires privacy review and still only proves the specific data and account scope used;
- browser sanity proves route rendering and obvious layout failures, not visual taste or product correctness.

Do not present screenshots, browser checks, unit tests, or sample data as broader proof than they actually provide.

## Current browser-sanity workflow

Use `docs/operations/ui-preview-and-browser-sanity.md` for user-visible frontend work. It defines:

- preview URL reporting;
- route and viewport checks;
- mock API profiles for protected routes;
- local screenshot policy;
- what browser sanity can and cannot prove.

Do not add Playwright, Cypress, WebDriverIO, screenshot manifests, or CI visual regression as a side effect of ordinary UI tasks. Those require a dedicated technical decision.

## Change rule

When a future task needs a different visual direction, do not argue from "v1" or "v2". Compare against current code, current user workflow, evidence quality, and the safety boundaries in `product-and-architecture.md`, then update this file if the new direction should guide future work.
