# VPS Detail Refactor Completion Audit Execution Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use the Trellis check workflow.
> This is a findings-only audit: do not modify product code, tests, specs, archived
> task artifacts, configuration, migrations, CI, Git refs, PRs, or external data.

**Goal:** Independently determine whether the current protected-main VPS detail
refactor is complete, reasonable, reliable, regression-free, and free of any
remaining justified improvement within its approved scope.

**Architecture:** Reconcile current authority and the complete delivered change
set, perform risk-focused cross-layer review, run fresh non-skipping verification,
exercise the real production browser build, and recheck protected delivery. The
main session validates every reported finding and owns the final verdict.

**Tech Stack:** Go, pgx/PostgreSQL 16, React 19, TypeScript, Vite, Vitest,
Playwright Chromium, Docker/MinIO, Git/GitHub CLI, and Trellis.

---

## Phase 0: Freeze the review boundary

- [x] Record `git rev-parse HEAD main origin/main`, branch, hooks, status, Go,
  Node/npm, Docker and Chromium/Playwright versions in
  `research/final-audit-report.md`.
- [x] Fetch/inspect current remote refs without merging or switching branches.
  If `origin/main` advanced in relevant paths after task creation, record and use
  one explicit reviewed commit; do not combine evidence from two trees.
- [x] Confirm only this task directory is changed and hooks point to `.githooks`.
- [x] Derive the pre-program baseline from the first protected-main functional
  merge (`2cbeb1bb^1`) and inventory all relevant commits through the reviewed
  commit.

## Phase 1: Reconcile scope and delivery claims

- [x] Validate the original parent, 12 functional archived children, overview
  follow-up and current task with `task.py validate`/task metadata inspection.
- [x] For PRs #394, #397, #400, #408, #410, #413, #416, #422, #423, #425,
  #428, #433, #436 and #438, recheck state, selected/merge commits, ancestry,
  required checks and relevant post-merge main CI.
- [x] Recheck `v0.75.0` tag/release assets and image manifest ancestry; distinguish
  product releases from task-only archive commits #440...#443.
- [x] Build an acceptance-to-owner matrix in `research/final-audit-report.md` for
  overview, activity, records core, attachments, evidence, Markdown, search,
  comparison, collaboration, portability, recovery and management actions.
- [x] Reconfirm the explicit current boundary for permanent deletion and the three
  accepted deferrals; detect any current code/doc that contradicts it.

## Phase 2: Independent code and contract review

- [x] Dispatch one `trellis-check` reviewer with the curated `check.jsonl`, explicit
  active task path, full approved scope, and a strict no-edit/findings-only rule.
  Persist its report under this task `research/`. The reviewer must open every
  manifest-listed current spec from disk and read it completely; context-injection
  truncation of a large spec is not sufficient evidence of contract review.
- [x] Review the complete program diff from the derived baseline and every later
  relevant fix, then inspect current callers/consumers instead of relying only on
  patch context.
- [x] Trace `/vps/:id` capability selection, overview fetch/composition, legacy
  fallback, five management mutations, refresh/failure behavior and navigation.
- [x] Trace VPS activity/records/evidence routes and project Records search,
  workspace, comparison, collaboration and portability flows from Web DTO to
  handler/service/store and back.
- [x] Review permission/no-leakage, strict decoding, errors, CAS/idempotency,
  transactions/outbox, stale-response guards, cursor/watermark, source deletion,
  Blob ownership and local/S3 recovery parity.
- [x] Review migration 0052...0059 plus ACL/admission/bootstrap/config/readiness;
  confirm permanent-delete flags and production handler remain fail-closed.
- [x] Search for placeholders, debug output, ignored tests, unsafe type/lint
  suppressions, broad `any`, duplicated authoritative constants and TODO/FIXME in
  the delivered scope; classify only evidence-backed current problems.
- [x] Re-open every sub-agent finding at current absolute `file:line`, reproduce it,
  and reject stale/speculative findings before adding it to the report.

## Phase 3: Focused non-mutating verification

- [x] Go format check (must print no paths):

  ```bash
  git ls-files '*.go' -z | xargs -0 gofmt -l
  ```

- [x] Run focused backend owners with no cache:

  ```bash
  go test ./internal/center/vpsoverview ./internal/center/activity \
    ./internal/center/records ./internal/center/recordsearch \
    ./internal/center/evidence ./internal/center/attachments \
    ./internal/center/recordmarkdown ./internal/center/recordauth \
    ./internal/center/portability ./internal/center/recordreadiness \
    ./internal/center/recordbackup ./internal/center/recordrestore \
    ./internal/center/http/handlers ./cmd/houfeng-center -count=1
  ```

- [x] Run focused Web suites for VPS overview/management, route gates, records
  activity/workspace/search/comparison/evidence and API contracts with Vitest
  `--run`; save exact file/test counts.
- [x] Run focused Playwright specs for VPS overview and records page states before
  the complete E2E suite, preserving Axe, keyboard and 390px assertions.

## Phase 4: Full repository gates

- [x] Run read-only Go equivalents of the repository gate:

  ```bash
  go vet ./agent/... ./cmd/... ./db/... ./internal/...
  go test ./agent/... ./cmd/... ./db/... ./internal/... -count=1
  ```

- [x] Under Node 22, run the complete Web gate:

  ```bash
  make verify-web
  ```

- [x] Run the complete Chromium contract gate:

  ```bash
  npm --prefix web run test:e2e
  ```

- [x] Confirm production build contains no fixture/helper leakage and record
  coverage, bundle and CSS budgets.
- [x] Check Git status after each full gate; ignored cache/build output is allowed,
  tracked product changes are a stop condition.

## Phase 5: Strict Records reliability gates

- [x] Run the bounded browser contract wrapper:

  ```bash
  scripts/run-records-browser.sh
  ```

- [x] Run the closed security corpus:

  ```bash
  scripts/run-records-security.sh
  ```

- [x] Run the current bounded capacity gate with real PostgreSQL:

  ```bash
  scripts/run-records-capacity.sh --profile local
  ```

  Record that this is not the explicitly deferred 4 GiB/512 MiB mixed-load
  harness and do not inflate its claim.

- [x] Run both integration profiles; any skip is failure:

  ```bash
  scripts/run-records-integration.sh --profile local
  scripts/run-records-integration.sh --profile s3
  ```

- [x] Run complete local and S3 recovery profiles; any skip is failure:

  ```bash
  scripts/run-records-recovery.sh --profile local --all
  scripts/run-records-recovery.sh --profile s3 --all
  ```

- [x] Re-run the focused config/bootstrap/readiness assertions that prove Records
  flags/defaults and nil permanent-delete transport boundary.

## Phase 6: Production browser inspection

- [x] Use the Playwright skill and current production preview with controlled
  fixtures/API mocks; do not access or mutate staging data.
- [x] Inspect 1440×1000 and 390×900 for stable, anomaly, initial loading, first
  empty, query-no-results, local failure, submitting/background and revoked states.
- [x] Exercise facts, decision, subscription, cancellation and archive dialogs;
  check authoritative blockers/confirmation, duplicate submit, stale route change,
  post-write refresh failure and legacy fallback.
- [x] Verify menu/dialog keyboard sequence, Escape/focus return, touch targets,
  named overflow regions and document/dialog horizontal overflow.
- [x] Run Axe against the overview and deep Records workspaces; record every
  serious/critical result and independently judge non-blocking moderate results.
- [x] Save only non-sensitive temporary screenshots/logs outside tracked paths;
  summarize observed geometry and behavior in the report.

## Phase 7: Synthesize and challenge the verdict

- [x] Merge current code review, sub-agent report, automated results, browser
  observations and GitHub delivery evidence into
  `research/final-audit-report.md`.
- [x] List findings first in Critical/Important/Minor order. For each candidate,
  verify current absolute `file:line`, impact, reproduction and scope ownership.
- [x] Explicitly audit for false positives caused by abandoned permanent deletion,
  the three accepted deferrals, historical non-gating requirements or purely
  subjective optimization preferences.
- [x] Map every PRD acceptance criterion to evidence or a named blocker. Do not
  mark the task complete while any criterion lacks current evidence.
- [x] Run `task.py validate` for this task and `git diff --check`; review the full
  task-only diff and confirm product/spec/archive diff remains zero.
- [x] Present the evidence-backed final verdict to the user. Do not implement any
  fix without new authorization.

## Rollback and stop conditions

- Any tracked product/spec/archive file changes: stop and report exact paths; do
  not reset, checkout or overwrite them.
- Any failed/skip gate or missing required infrastructure: preserve the failure
  evidence and report a blocker; do not weaken the command.
- Any current code finding: complete bounded reproduction and report it; do not
  patch it in this task.
- Any remote-main drift that affects scope: freeze the reviewed SHA and replan or
  request user approval before changing the audit target.
