# Final verification evidence

- Date: 2026-08-28 (Asia/Shanghai)
- Initial review baseline: `da83a96769b618c6e223f71a1d2c6645d54c853b`
- Worktree: `/home/murray/code/houfeng/.worktree/vps-write-idempotency-hardening`
- Branch: `codex/vps-write-idempotency-hardening`
- Initial handoff state: `in_progress`; retained unstaged and uncommitted for external review before the later approved delivery

## Task-scope results

- Final Trellis full-scope check fixed Legacy global write blocking and the original registry snapshot publication defect. A later external review found that a structurally compatible prepared-owner variable could still reintroduce digest/key fields through `begin()`; that runtime boundary was reproduced under TDD, fixed, and independently rechecked with no remaining Critical, Important, or Minor findings.
- Node 22.23.1 `make verify-web`: PASS; fresh pre-delivery run completed 205 Vitest files / 1570 tests, coverage, TypeScript production build, Vite build, bundle budget and CSS budget. Current budgets: entry JS `110463/110738`, entry CSS `38530/38530`, max async JS `48452/48455`, fonts `139072/139072` bytes.
- Focused cross-layer Web: PASS; 8 files / 289 tests.
- External-review snapshot-boundary TDD: RED was 1 failed / 5 passed because `begin(preparedOwner)` published `requestDigest`; GREEN was 6/6 owner-store tests. Fresh post-fix Node 22.23.1 verification passed 7 files / 258 tests, full Web ESLint, `tsc -b`, production build, and bundle check. Post-fix budgets: entry JS `110463/110738`, entry CSS `38530/38530`, max async JS `48452/48455`, fonts `139072/139072` bytes.
- Chromium route regression: PASS; `vps-overview-cancellation.spec.ts` and `vps-overview-destinations.spec.ts`, 20/20 tests.
- Task-scoped Go packages: PASS; fresh tests and `go vet` across 11 affected package paths (`createidempotency`, four domain packages, `store`, `store/migrate`, `http/handlers`, `http`, and `cmd/houfeng-center`).
- Strict disposable PostgreSQL lost-response: PASS; all four create subtests, zero skips. See `p2-02-postgres-verification-evidence.md`.
- Strict current APP ACL: PASS; all four top-level scenario groups, zero skips. See `p2-02-postgres-verification-evidence.md`.
- Privacy AST contract: PASS; all eight scoped test families. Public VPS write snapshots contain no raw body, digest, or idempotency key, including when a prepared-owner variable is legally passed back to `begin()` through TypeScript structural compatibility.
- Formatting and patch integrity: touched Go files `gofmt -l` empty; `git diff --check HEAD` passed.

## Repository-wide Go gate

`make verify-go` was executed and did not pass. A fresh `make test-go` repeated the same result. The only failure was the pre-existing, task-out-of-scope attachment golden mismatch:

```text
TestPreviewImageGoldenMetadataFreeBoundedPNG
got  0d749fd4e5010a847bd9b8872b56cf56049caa705d45616e4a823cc2a4768c6e
want dac4e6f598e26f4dcfb32ea88f81375f42a14739719a9761db54160b1267ed9d
```

All task-affected Go packages passed. The attachment golden was not rewritten and is not claimed as green.

## Initial external-review handoff state

- Git index: empty.
- Worktree: intentionally dirty with task files only; all changes remain unstaged/uncommitted.
- No commit, push, PR, merge, release, archive, clean, or task-complete transition was performed.
- Task remains `in_progress` while awaiting external review.

This handoff state was superseded after external review passed and the user authorized full protected delivery. Feature PR #468, release PR #469, `v0.78.0`, the signed assets, deployment assets, and multi-architecture images all completed successfully. See `delivery-release-evidence.md` for the exact final record.

This evidence intentionally omits DSNs, SQL, idempotency keys, digests, request bodies, note/details content, and raw internal errors.
