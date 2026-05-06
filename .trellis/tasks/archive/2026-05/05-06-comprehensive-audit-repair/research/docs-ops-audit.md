# Research: docs / ops / release / verification consistency audit

- Query: Research docs, operations, release, and verification consistency risks for the active Trellis task. Inspect README.md, CLAUDE.md, docs/release, docs/deploy, docs/operations, Makefile, scripts, .github workflows, and env examples. Focus on misleading public/AI entry claims, release gate contradictions, stale archive references, CI/local verification drift, deployment/runtime setup issues, and follow-up classification.
- Scope: internal
- Date: 2026-05-06

## Findings

### Files Found

| Path | Description |
| --- | --- |
| `README.md` | Public repo entry. Declares V1 implementation scope, active design authorities, delivery artifacts, and automated verification commands. |
| `CLAUDE.md` | AI entry guide. Declares project identity, common commands, runtime layout, env expectations, visual authority, and V1 verification artifacts. |
| `docs/release/v1-gap-checklist.md` | Release/gap authority. Contains V1 release gate, verified/deferred rows, and gap follow-up statuses. |
| `docs/release/next-phase-plan.md` | Roadmap/release gate framing. Declares Stage 1 P0/P1/P2 status, Stage 1 completion, and Stage 2 trigger. |
| `docs/release/docs-audit.md` | Prior docs archive audit. Records keep/archive decisions and known stale reference classes. |
| `docs/deploy/local-and-systemd.md` | Canonical local/systemd deployment guide. Includes center/agent env examples, systemd install, auth note, TLS warning, and operational checks. |
| `docs/deploy/systemd/houfeng-center.service` | Center systemd unit template. Uses `/etc/houfeng/center.env`, `/usr/local/bin/houfeng-center`, and hardening options. |
| `docs/deploy/systemd/houfeng-agent.service` | Agent systemd unit template. Uses `/etc/houfeng-agent/agent.env`, `/usr/local/bin/houfeng-agent`, state directory, and hardening options. |
| `docs/operations/v1-smoke-run.md` | Fresh-install smoke procedure and evidence tables. Includes 2026-04-29 and 2026-05-02 live evidence plus caveats. |
| `docs/operations/*.jpg` | Current v2 visual evidence files: `Dashboard.jpg`, `节点列表页面.jpg`, `节点详情页面.jpg`, `目标列表页面.jpg`, `目标详情页面.jpg`. |
| `.env.example` | Local env example for center and agent, including auth seed variables. |
| `Makefile` | Local build/test/verify target definitions. |
| `scripts/verify.sh` | Full repo verification wrapper calling `make verify-go` then `make verify-web`. |
| `scripts/subset-fonts.sh` | Optional font-subsetting helper, unrelated to release verification but part of scripts inventory. |
| `.github/workflows/ci.yml` | CI workflow for Go and web verification on push/PR. |
| `internal/center/config/config.go` | Center runtime config loader; used to validate env claims in docs. |
| `agent/config/config.go` | Agent runtime config loader; used to validate env claims in docs. |

### Code Patterns

- Center config has defaults for `HOUFENG_HTTP_ADDR`, `HOUFENG_WEB_DIST_DIR`, and `HOUFENG_INCIDENT_SWEEP_INTERVAL`, but `HOUFENG_DATABASE_URL`, `HOUFENG_INITIAL_USERNAME`, and `HOUFENG_INITIAL_PASSWORD` are required and empty values fail startup (`internal/center/config/config.go:29`, `internal/center/config/config.go:40`, `internal/center/config/config.go:56`, `internal/center/config/config.go:60`, `internal/center/config/config.go:112`).
- Telegram env must be both set or both empty, so examples that leave both blank are valid disabled-Telegram configs (`internal/center/config/config.go:50`, `internal/center/config/config.go:52`).
- Agent config requires only `HOUFENG_AGENT_SERVER_URL` and `HOUFENG_AGENT_TOKEN_FILE`; buffer file, max entries, and max age have defaults (`agent/config/config.go:25`, `agent/config/config.go:31`, `agent/config/config.go:36`, `agent/config/config.go:41`, `agent/config/config.go:49`).
- `make verify-web` currently runs `npm ci`, `npm run lint`, `npm run test -- --run`, and `npm run build`; `scripts/verify.sh` runs `make verify-go` then `make verify-web` (`Makefile:65`, `Makefile:67`, `scripts/verify.sh:4`, `scripts/verify.sh:5`).
- CI mirrors the local split by running `make verify-go` and `make verify-web`, using `go-version-file: go.mod` and Node 22 (`.github/workflows/ci.yml:14`, `.github/workflows/ci.yml:16`, `.github/workflows/ci.yml:24`, `.github/workflows/ci.yml:26`, `.github/workflows/ci.yml:29`).

### Risk 1: deploy guide and AI entry omit auth seed vars from "minimum center env"

Classification: **Blocker / same-task fix candidate**.

Evidence:
- `CLAUDE.md` says "Required at minimum" for center env but lists only `HOUFENG_HTTP_ADDR`, `HOUFENG_DATABASE_URL`, `HOUFENG_WEB_DIST_DIR`, and `HOUFENG_INCIDENT_SWEEP_INTERVAL` (`CLAUDE.md:66`, `CLAUDE.md:68`).
- `docs/deploy/local-and-systemd.md` presents the "Minimum `/etc/houfeng/center.env`" without `HOUFENG_INITIAL_USERNAME` or `HOUFENG_INITIAL_PASSWORD` (`docs/deploy/local-and-systemd.md:33`, `docs/deploy/local-and-systemd.md:35`, `docs/deploy/local-and-systemd.md:37`).
- The systemd example writes `/etc/houfeng/center.env` with no initial-user values (`docs/deploy/local-and-systemd.md:102`, `docs/deploy/local-and-systemd.md:103`, `docs/deploy/local-and-systemd.md:109`).
- The same deploy guide later says both `HOUFENG_INITIAL_USERNAME` and `HOUFENG_INITIAL_PASSWORD` are required (`docs/deploy/local-and-systemd.md:143`, `docs/deploy/local-and-systemd.md:149`, `docs/deploy/local-and-systemd.md:151`, `docs/deploy/local-and-systemd.md:152`).
- `.env.example` includes the initial-user values, so the repo has the correct canonical example elsewhere (`.env.example:13`, `.env.example:14`, `.env.example:15`).
- Code confirms both values are required at config load, not merely used later when the users table is empty (`internal/center/config/config.go:56`, `internal/center/config/config.go:60`).

Impact:
- A new operator following the "minimum" center env or systemd snippet will start a service that fails before serving health/API/UI.
- AI agents using `CLAUDE.md` as the entry guide can incorrectly diagnose first-start failures as DB/systemd issues instead of missing auth seed env.

Suggested disposition:
- Fix `CLAUDE.md` and `docs/deploy/local-and-systemd.md` in this task. The change is factual, low-risk, and directly affects startup reliability.
- Add `HOUFENG_INITIAL_USERNAME` / `HOUFENG_INITIAL_PASSWORD` to the minimum env block and the systemd `tee /etc/houfeng/center.env` example. Optionally mention that after first user creation the values are ignored by seed logic, but still currently required by config load.

### Risk 2: smoke-run runnable steps omit auth login/cookie, but protected API calls require it

Classification: **Blocker / same-task fix candidate**.

Evidence:
- The smoke prerequisite exports center env and starts the center, then immediately documents protected calls such as `POST /api/nodes` without a login step or cookie jar (`docs/operations/v1-smoke-run.md:30`, `docs/operations/v1-smoke-run.md:48`, `docs/operations/v1-smoke-run.md:63`, `docs/operations/v1-smoke-run.md:66`).
- The 2026-05-02 evidence table records a login step and cookie reuse as necessary for protected calls (`docs/operations/v1-smoke-run.md:244`, `docs/operations/v1-smoke-run.md:246`, `docs/operations/v1-smoke-run.md:261`, `docs/operations/v1-smoke-run.md:263`, `docs/operations/v1-smoke-run.md:302`).
- `docs/deploy/local-and-systemd.md` correctly states every `/api/*` route except `/api/healthz` and `/api/agent/*` is session-cookie protected (`docs/deploy/local-and-systemd.md:143`, `docs/deploy/local-and-systemd.md:145`, `docs/deploy/local-and-systemd.md:146`).
- `CLAUDE.md` also says every non-agent / non-health route is protected by auth, and CI/docs rely on this as current truth (`CLAUDE.md:68`, `CLAUDE.md:120`).

Impact:
- A user following the smoke procedure from the top will get 401s at Step 1 and may conclude the API or center is broken.
- The guide's evidence section has the missing step, but the executable recipe does not, which is a high-friction documentation bug.

Suggested disposition:
- Fix `docs/operations/v1-smoke-run.md` in this task by adding a Step 0 login using a cookie jar before Step 1, and add `-b/-c` cookie arguments to protected API examples or define a `curl` pattern variable.
- Keep `/api/agent/*` examples unauthenticated because those use enrollment tokens.

### Risk 3: active smoke evidence still points at an archived visual-verification doc

Classification: **Same-task fix candidate**.

Evidence:
- Active `README.md` and `CLAUDE.md` both say the old `docs/operations/v1-visual-verification.md` and `docs/operations/visual-evidence/` were archived (`README.md:55`, `CLAUDE.md:127`).
- The active operations directory no longer contains those paths; current visual evidence is five JPEGs directly under `docs/operations/`.
- `docs/operations/v1-smoke-run.md` still says screenshot/visual evidence remains tracked in `docs/operations/v1-visual-verification.md` (`docs/operations/v1-smoke-run.md:238`).
- `docs/release/docs-audit.md` already identified this line as a known archived reference class during the archive cleanup (`docs/release/docs-audit.md:197`).

Impact:
- A user looking for visual evidence from the smoke table hits a missing path, despite current JPEG evidence existing.
- It creates a misleading impression that the active visual-verification process is still the archived v1/stitch process.

Suggested disposition:
- Fix the smoke table row to point to the current `docs/operations/*.jpg` evidence and/or to `docs/release/v1-gap-checklist.md` row "Visual screenshot comparison against baseline PNGs".

### Risk 4: release gate documents contradict each other on Telegram as Stage 1 completion input

Classification: **Needs product/release decision before edit**.

Evidence:
- `docs/release/v1-gap-checklist.md` says the final V1 gate requires "Telegram delivery proof or an explicit note that Telegram is disabled for the deployment" (`docs/release/v1-gap-checklist.md:115`, `docs/release/v1-gap-checklist.md:117`, `docs/release/v1-gap-checklist.md:124`).
- The same gap checklist says the remaining release-gate items are Telegram real delivery and strict visual evidence, deferred ops follow-up (`docs/release/v1-gap-checklist.md:126`).
- `docs/release/next-phase-plan.md` says Telegram true-send is deferred, user-env-required, and "does not block Stage 1 closure" (`docs/release/next-phase-plan.md:72`, `docs/release/next-phase-plan.md:73`, `docs/release/next-phase-plan.md:75`).
- But the same completion checklist says real environment smoke must include Telegram true-send evidence (`docs/release/next-phase-plan.md:86`, `docs/release/next-phase-plan.md:88`, `docs/release/next-phase-plan.md:92`), then immediately declares Stage 1 completion passed (`docs/release/next-phase-plan.md:97`).

Impact:
- This is the main release-gate truth contradiction. One reader may block V1 tag on Telegram true delivery; another may tag because "explicit disabled note" or "not blocking" is enough.
- Since this is release policy, fixing wording without a decision could unintentionally lower or raise the release gate.

Suggested disposition:
- Ask/decide whether the V1 gate accepts "Telegram intentionally disabled, notification records verified" as sufficient for this deployment. If yes, update `next-phase-plan.md` line 92 wording from "含 Telegram 真发证据" to "含 Telegram 真发证据或明确禁用说明", matching `v1-gap-checklist.md`.
- If true delivery is required before any V1 tag, then `next-phase-plan.md` should not say Stage 1 completion passed.

### Risk 5: release gate duplicates web build verification

Classification: **Minor cleanup / same-task optional**.

Evidence:
- `README.md` lists `go test ./...`, `./scripts/verify.sh`, and `cd web && npm run build` as automated verification (`README.md:57`, `README.md:59`, `README.md:60`, `README.md:61`, `README.md:62`).
- `docs/release/v1-gap-checklist.md` repeats the same three gate commands (`docs/release/v1-gap-checklist.md:117`, `docs/release/v1-gap-checklist.md:119`, `docs/release/v1-gap-checklist.md:120`, `docs/release/v1-gap-checklist.md:121`).
- `scripts/verify.sh` runs `make verify-web`, and `make verify-web` already runs `npm run build` (`scripts/verify.sh:4`, `scripts/verify.sh:5`, `Makefile:65`, `Makefile:67`).

Impact:
- Not harmful, but it makes the release gate look like three independent checks when `cd web && npm run build` is a subset/repeat of `./scripts/verify.sh`.
- During a failure, operators may waste time rerunning the same build stage without realizing it is already embedded in verify.

Suggested disposition:
- Keep as explicit if the team values redundancy for clarity, or reword to "`./scripts/verify.sh` (includes web lint/test/build) plus optional focused `cd web && npm run build` when diagnosing frontend failures."

### Risk 6: local and CI verification are currently aligned; no drift found in executable paths

Classification: **No action**.

Evidence:
- `CLAUDE.md` says full repo verification is `./scripts/verify.sh`, equivalent to `make verify-go && make verify-web` (`CLAUDE.md:39`, `CLAUDE.md:40`).
- `scripts/verify.sh` implements exactly that (`scripts/verify.sh:4`, `scripts/verify.sh:5`).
- `Makefile` `verify-web` includes lint, vitest run mode, and build (`Makefile:65`, `Makefile:67`).
- CI runs `make verify-go` and `make verify-web` (`.github/workflows/ci.yml:17`, `.github/workflows/ci.yml:29`).

Impact:
- The old gap #12 statement that `verify-web` did not run lint is stale in historical rows, but active executable paths are aligned now.

Suggested disposition:
- No executable change. Avoid reintroducing separate CI commands that differ from Makefile targets.

### Risk 7: active Trellis spec still points implement/check agents at archived visual authorities

Classification: **Already in PRD as must-fix; same-task fix candidate**.

Evidence:
- Task PRD explicitly records stale `.trellis/spec` visual references as a must-fix process/docs issue (`.trellis/tasks/05-06-comprehensive-audit-repair/prd.md:22`, `.trellis/tasks/05-06-comprehensive-audit-repair/prd.md:43`, `.trellis/tasks/05-06-comprehensive-audit-repair/prd.md:45`, `.trellis/tasks/05-06-comprehensive-audit-repair/prd.md:55`).
- `.trellis/spec/web/component-conventions.md` overview still names archived v1 visual docs as visual authority (`.trellis/spec/web/component-conventions.md:9`).
- `.trellis/spec/web/styling-guidelines.md` says visual authority is v1 baseline screenshots/spec and tells contributors to use `docs/operations/visual-evidence/` (`.trellis/spec/web/styling-guidelines.md:21`, `.trellis/spec/web/styling-guidelines.md:25`, `.trellis/spec/web/styling-guidelines.md:26`, `.trellis/spec/web/styling-guidelines.md:33`, `.trellis/spec/web/styling-guidelines.md:141`).
- `.trellis/spec/web/quality-guidelines.md` still tells contributors to use the archived visual-verification flow and says `make verify-web` does not run lint (`.trellis/spec/web/quality-guidelines.md:161`, `.trellis/spec/web/quality-guidelines.md:163`, `.trellis/spec/web/quality-guidelines.md:207`, `.trellis/spec/web/quality-guidelines.md:211`).
- `.trellis/spec/backend/quality-guidelines.md` also points UI changes at the archived visual-verification flow (`.trellis/spec/backend/quality-guidelines.md:161`, `.trellis/spec/backend/quality-guidelines.md:162`).

Impact:
- This is especially dangerous for Trellis because implement/check agents load specs automatically. They can follow stale visual authorities even though README and CLAUDE point to v2.

Suggested disposition:
- Fix through the Trellis spec-update path, not by ad hoc docs edits. Update active spec files to point visual authority at `docs/design/v2-houfeng/{design-language.md,component-spec.md}` and current screenshot evidence under `docs/operations/*.jpg`, or explicitly state that current v2 screenshot procedure is pending if no formal process exists.
- Correct the `make verify-web` lint statement in web quality guidelines.

### Risk 8: v2 design docs contain broken relative links to archived paths

Classification: **Follow-up / likely same-task if docs cleanup scope includes design docs**.

Evidence:
- `docs/design/v2-houfeng/design-language.md` frontmatter supersedes `docs/design/v1-baseline/ui-ux-spec.md` and `docs/design/v1.x-frontend-redesign/` (`docs/design/v2-houfeng/design-language.md:1`, `docs/design/v2-houfeng/design-language.md:4`, `docs/design/v2-houfeng/design-language.md:5`, `docs/design/v2-houfeng/design-language.md:6`).
- The navigation links still point to non-existent non-archive paths (`docs/design/v2-houfeng/design-language.md:369`, `docs/design/v2-houfeng/design-language.md:372`, `docs/design/v2-houfeng/design-language.md:373`).
- `docs/design/v2-houfeng/component-spec.md` links to `../v1-baseline/ui-ux-spec.md`, which no longer exists at that path (`docs/design/v2-houfeng/component-spec.md:301`, `docs/design/v2-houfeng/component-spec.md:304`).
- File presence check confirmed `docs/design/v1-baseline/ui-ux-spec.md`, `docs/design/v1.x-frontend-redesign/README.md`, `docs/operations/v1-visual-verification.md`, and `docs/operations/visual-evidence` are missing from active paths; archive copies exist under `docs/_archive/`.

Impact:
- Active design authority docs contain broken links in their "history" navigation. This is lower severity than AI entry docs because the surrounding text says superseded/history, but links should still resolve.

Suggested disposition:
- Update links to `docs/_archive/design/...` if preserving historical navigation matters in this task. Otherwise record as docs follow-up.

### Risk 9: current visual evidence claim is mostly backed, but one page naming/detail is imprecise

Classification: **Minor docs precision**.

Evidence:
- `CLAUDE.md` says v2 screenshots exist for Dashboard / 节点列表 / 节点详情 / 目标列表 / 目标详情 on 2026-05-06 (`CLAUDE.md:120`, `CLAUDE.md:121`).
- Active `docs/operations/` contains exactly five JPEGs matching those surfaces: `Dashboard.jpg`, `节点列表页面.jpg`, `节点详情页面.jpg`, `目标列表页面.jpg`, `目标详情页面.jpg`.
- `README.md` says v2 visual evidence flow is pending after V1 closure, while `CLAUDE.md` says evidence screenshots exist (`README.md:55`, `CLAUDE.md:121`). `next-phase-plan.md` also says visual evidence is done (`docs/release/next-phase-plan.md:77`, `docs/release/next-phase-plan.md:83`).

Impact:
- The current evidence files exist, so this is not a false claim. The risk is process wording: README still says the v2 evidence flow is pending "另议", while release docs say evidence screenshots have been captured.

Suggested disposition:
- Low-priority wording cleanup: change README note to distinguish "formal repeatable v2 visual evidence workflow" from "one-time v2 screenshot evidence captured under `docs/operations/*.jpg`".

### Risk 10: `.env.example` is more accurate than deploy guide, but docs do not point operators to it during systemd setup

Classification: **Same-task optional**.

Evidence:
- `.env.example` has all current center and agent env fields, including initial user seed and session TTL (`.env.example:3`, `.env.example:4`, `.env.example:13`, `.env.example:14`, `.env.example:18`, `.env.example:21`, `.env.example:22`).
- `CLAUDE.md` references `.env.example` and deployment guide together (`CLAUDE.md:64`, `CLAUDE.md:66`).
- `docs/deploy/local-and-systemd.md` does not mention copying from `.env.example` or reconciling against it in the systemd snippet.

Impact:
- Operators may use the deploy doc's incomplete inline env block instead of the accurate example.

Suggested disposition:
- After fixing Risk 1, add a short note in deploy guide: "Use `.env.example` as the full local variable inventory; the systemd snippets below are deployment-shaped examples and must include required auth seed vars on first startup."

## External References

- None. This audit used repository-local files only. External docs were not needed because the task is about consistency among local project artifacts and runtime code.

## Related Specs

- `.trellis/spec/backend/index.md` declares `CLAUDE.md`, frozen v1 business docs, and v2 visual docs as authority, and says guide files should record current codebase conventions, not idealized patterns (`.trellis/spec/backend/index.md:3`, `.trellis/spec/backend/index.md:31`).
- `.trellis/spec/web/index.md` declares the same authority stack for the web layer (`.trellis/spec/web/index.md:3`).
- `.trellis/spec/web/component-conventions.md`, `.trellis/spec/web/styling-guidelines.md`, `.trellis/spec/web/quality-guidelines.md`, and `.trellis/spec/backend/quality-guidelines.md` are related because they still contain stale visual/verification guidance that can mislead future Trellis agents.

## Follow-up Classification Summary

| Item | Classification | Suggested owner |
| --- | --- | --- |
| Missing auth seed vars in deploy/AI minimum env docs | Blocker / same-task fix | docs/ops implementer |
| Smoke-run missing login/cookie setup | Blocker / same-task fix | docs/ops implementer |
| Smoke-run archived visual-verification reference | Same-task fix | docs/ops implementer |
| Telegram release-gate contradiction | Decision needed before edit | main task owner / user |
| Duplicate web build command in release gate | Optional cleanup | main task owner |
| CI/local verification drift | No action | none |
| Stale `.trellis/spec` visual/verify guidance | Same-task spec-update fix | trellis-update-spec |
| Broken active v2 docs links to archived history | Follow-up or same-task docs cleanup | docs implementer |
| README visual evidence wording lag | Minor docs precision | docs implementer |
| `.env.example` not connected to deploy setup | Optional cleanup after Risk 1 | docs/ops implementer |

## Caveats / Not Found

- `python3 ./.trellis/scripts/task.py current --source` still reported `Current task: (none)` in this session. The write target came from explicit user confirmation: `.trellis/tasks/05-06-comprehensive-audit-repair`.
- This audit did not run `go test`, `./scripts/verify.sh`, `npm run build`, or a live smoke. It inspected docs, scripts, workflow definitions, env examples, and config code only.
- No source or docs files outside this research artifact were edited.
- No unexpected CI/local verification drift was found in executable paths. The drift is in stale textual guidance, especially `.trellis/spec` and deploy/smoke docs.
