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
- the shared shell and workspaces consume the same semantic theme tokens; layout choices must not introduce independent palettes or overwrite the global theme preference;
- compact spacing based on the existing token scale;
- high-density tables for list scanning;
- cards for repeated items, modals, warnings, and contained tools, not for wrapping every page section;
- monospace treatment for IDs, hostnames, IPs, timestamps, versions, durations, prices, counts, and other technical facts;
- status shown by both color and shape where possible.

These defaults are expected for ordinary UI work. They can change through a task that updates the tokens/components/tests/docs together.

### VPS workspace views

The VPS inventory offers two layouts within one visual system:

- **目录视图 (ledger)** is the default: an asset directory on the left and a readable inspector on the right.
- **表格视图 (workbench)** is a scanning table. Clicking an asset name toggles a compact accordion directly beneath that row; only one row is expanded at a time. Do not put the inspector at the bottom of the inventory.

Both views share the shell and other pages' semantic surface, text, border, accent, state, spacing, and typography tokens. Use the same sans-serif heading hierarchy, technical monospace facts, shared Badge treatment, and focus language. Do not restore separate graphite/brown palettes or editorial serif headings. Layout and density may differ; the visual identity must not. Both follow global dark/light and system-resolved modes. The view switch changes neither global theme nor business semantics.

Both layouts use the same inventory, subscription evidence, search, filters, selection, and management destinations, and open one independent VPS detail workspace. Keep return navigation and section hierarchy explicit. Inspectors are reading aids, not alternative detail routes or separate business forms.

The detail workspace uses the shared shell palette in both capability modes: a unified identity header, one validated inventory return in the top-bar breadcrumb, and a primary management action only when writable. Light mode uses coordinated neutral, slightly green-tinted shell/canvas surfaces and white content surfaces; dark mode retains the same semantic hierarchy. Do not restore route-only canvas colors or beige controls beside neutral cards. Separate subscription/renewal, runtime observations, asset facts, resources, and a quieter activity timeline with spacing, fine borders, and headings rather than permanent drop shadows or nested field cards.

Treat VPS detail as a personal infrastructure asset-and-facts workspace, not a realtime operations cockpit. Allocate space by content: flexible connection facts beside a compact billing summary in the first zone, followed by independent observation rows, resource lists and lightweight activity. Do not impose a permanent page-wide main/aside ratio. The route currently caps comfortable reading width at 1600 CSS px; collapse the first zone when its contents cannot fit. Use approximately 22px page titles, 15px module headings, 14px body/field values, 12–13px helper text and 20px amounts, with 1.45 body leading, 16px section padding and 14px gaps. Do not enlarge typography with viewport width or manufacture card height.

Keep primary route navigation distinct from the closed in-page directory. Normal readiness is quiet; stale or unavailable evidence, applicable timestamps, and retry actions remain local to the affected source. Resource actions belong beside their actual group or record and must not imply a record-specific destination when only a collection action exists. Menus must remain readable and reachable when their trigger moves to the left on narrow screens.

Keep the established density baseline while restoring moderate surface contrast: primary content uses the shared content surface against the shell canvas, without heavy shadows, enlarged radii, or disabled-looking page opacity. Observation rows share stable project/conclusion/explanation/data-time/action positions; put long failure explanations beneath their object. Use consistent state badges rather than colored words embedded in ordinary prose. Natural remaining whitespace is acceptable; do not stretch sections to equal heights.

Keep detail body text comfortably readable at approximately 14px and helper text at 12–13px; density comes from removing repetition and shortening information distances, not indiscriminate shrinking. Use sans-serif for reading and technical monospace for useful identifiers/IP/SSH. Preserve hover/focus contrast, natural long-name/note wrapping, adjacent full-value copy feedback and touch-sized actions. Browser zoom must remain usable; never implement density with CSS zoom or page transforms.

Use concise labels and factual loading/error/empty states. Do not add instructional lead-ins, self-descriptions of the inspector, or prose that restates the adjacent facts.

User-facing copy uses Chinese product words, not stored enums: subscription `active` is 生效中; Target is 入口探测 while IDs stay `target_id`. Source presence 在册 is a neutral identity badge, not a green health state. IP quality is a report page titled IP 质量报告, not a 驾驶舱, and does not add local tabs or a second risk algorithm. VPS, monitoring, and 入口探测 activity/records/evidence share `SubjectActivityWorkspace` and `SubjectLocalNavigation`; only VPS exposes an overview hop because only that source has a detail workspace. Do not duplicate those surfaces or invent missing source fields.


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
