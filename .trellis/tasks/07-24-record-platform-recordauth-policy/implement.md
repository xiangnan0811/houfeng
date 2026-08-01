# Actor scope 与统一 recordauth.Policy — Implementation Plan

> **For agentic workers:** Use a fresh implementation worker for this task, then run a spec-compliance review followed by a code-quality review. Follow TDD: every production behavior begins with an observed failing test.

## Scope and files

Create:

- `internal/center/recordauth/types.go`
- `internal/center/recordauth/policy.go`
- `internal/center/recordauth/policy_test.go`
- `internal/center/store/record_auth.go`
- `internal/center/store/record_auth_test.go`

Modify:

- `internal/center/http/sessionctx/sessionctx.go`
- `internal/center/http/middleware.go`
- `internal/center/http/middleware_test.go`
- `internal/center/http/auth_e2e_test.go`
- `cmd/houfeng-center/bootstrap.go`
- `cmd/houfeng-center/bootstrap_test.go`

Do not modify migrations, ACL/admission code, router options, unrelated handlers, frozen worktrees, or the stale APP handoff task.

## Ordered TDD plan

1. **Lock the pure-policy contract in RED tests.** Add table cases for project admin allow; viewer role and viewer group allow; intersection deny; cross-project deny; unknown capability/source kind/scope version/visibility; empty restricted deny-all; live-source widening; missing or altered tombstone floor/`LastLiveScope`; `capture=project → last live=restricted → final floor=project` reopening; strict live/tombstone union; and canonical reordering/digest drift. Run:

   ```sh
   go test ./internal/center/recordauth -run RecordAuth -count=1
   ```

   Confirm the tests fail because the package or symbols do not yet exist.

2. **Implement minimal closed types and canonicalization.** Add `ProjectIDDefault`, role/capability/source registries, strict ID validators, `ActorScope`, resource/source/floor types, and the tombstone-only canonical `LastLiveScope` witness; include it in source canonical bytes/digest and enforce `LastLiveScope <= CaptureScope` plus `FinalFloor <= LastLiveScope`. Implement only enough policy code to make the first test group pass. Do not use generic JSON, maps, loose string normalization or a business-package dependency.

3. **Add RED store repository tests.** With a narrow `Query` seam, assert the exact stable-ID SQL shape, project/user arguments, ordered rows, query/scan errors and malformed group ID behavior. The tests must prove no display/content fields are requested. Run:

   ```sh
   go test ./internal/center/store -run RecordAuth -count=1
   ```

4. **Implement the only production scope repository.** Define `recordauth.ScopeRepository` for group lookup and implement `store.NewPostgresRecordAuthorizationRepository` using the APP runtime pool. Return only group IDs; normalization remains in `recordauth`. Re-run the focused store test until green.

5. **Add RED middleware/session tests.** Cover a typed actor with sorted/deduplicated persisted groups, legacy `UserIDFromContext` compatibility, no actor when absent, forged `X-Project-ID/X-Role/X-Group-ID` headers, scope-store error 503, and unknown/empty authenticated principal fail-closed. Update the existing HTTP end-to-end fixture to pass an explicit successful test scope repository; do not preserve an optional production scope fallback merely to keep the old one-argument call compiling. Run:

   ```sh
   go test ./internal/center/http -run 'SessionScope|RequireSession' -count=1
   ```

   Confirm failures identify the missing typed context and/or repository parameter rather than a fixture typo.

6. **Wire trusted actor creation.** Add typed session context helpers while preserving the legacy user helper. Change middleware to `RequireSession(authn, scopes)`, map only the known server-side admin role, list persistent groups, call the one normalization function, and return fixed 503 for scope infrastructure failure. Do not expand `handlers.AuthService`; it already returns `auth.User`.

7. **Wire bootstrap and prove no nil fallback.** Construct the store repository from the existing APP runtime pool, pass it into the middleware closure already supplied as `RouterOptions.AuthMiddleware`, and update bootstrap tests. RouterOptions is intentionally not expanded with an unused scope field.

8. **Run the focused and compatibility gates.**

   ```sh
   go test ./internal/center/recordauth ./internal/center/store ./internal/center/http -run 'RecordAuth|SessionScope|RequireSession' -count=1
   go test ./cmd/houfeng-center -run 'Bootstrap|Router' -count=1
   go test ./internal/center/http ./internal/center/http/handlers -count=1
   gofmt -w internal/center/recordauth internal/center/store/record_auth.go internal/center/store/record_auth_test.go internal/center/http/sessionctx/sessionctx.go internal/center/http/middleware.go internal/center/http/middleware_test.go internal/center/http/auth_e2e_test.go cmd/houfeng-center/bootstrap.go cmd/houfeng-center/bootstrap_test.go
   git diff --check
   ```

   Run the repository Go verification gate after focused gates are green. A pre-existing unrelated failure is evidence to report, not a reason to weaken authorization tests.

9. **Review and commit.** First dispatch a spec-compliance reviewer against this PRD/design/plan, then a code-quality reviewer. Resolve every finding and re-run the affected checks before staging only the listed files and the task artifacts. Commit on `codex/vps-records-platform-recordauth-policy`; do not push, create a PR, merge, archive the parent, or admit Child 2–11 in this slice.

## Completion boundary

This task establishes the reusable policy and trusted session seam. It does not complete the parent PF parity criterion until later concrete records API, query-builder and worker implementations all use the same policy.
