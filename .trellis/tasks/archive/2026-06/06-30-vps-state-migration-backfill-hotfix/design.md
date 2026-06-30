# Hotfix VPS state migration backfill design

## Root Cause

Migration 0049 adds a validated cross-column check constraint to `vps_assets`.
That is the correct final guardrail, but the migration currently assumes every
existing row already satisfies the new invariant. Production has at least one
legacy row with a now-invalid combination, so PostgreSQL rejects the constraint
and the center cannot finish bootstrap.

## Fix Shape

Edit migration 0049 in place because the migration fails before being recorded
in `schema_migrations`; a follow-up 0050 migration would never run on the broken
install.

Before adding the constraint, run deterministic updates that only normalize the
four hard-failure combinations:

- `cancelled` with a non-cancellation renewal decision -> `renewal_decision =
  'cancel'`.
- `cancelled` with `usage_status = 'in_use'` -> `usage_status = 'idle'`.
- `to_cancel` with a non-cancellation renewal decision -> `renewal_decision =
  'cancel'`.
- `to_migrate` with any non-`migrate` renewal decision -> `renewal_decision =
  'migrate'`.
- `renewal_decision = 'replaced'` while `lifecycle_status = 'active'` or
  `usage_status = 'in_use'` -> move current facts to a non-current explanation:
  `lifecycle_status = 'idle'` when active and `usage_status = 'idle'` when
  in_use.

Each update touches `updated_at = now()` only for rows it changes. `archived_at`
is left unchanged because 0049 does not alter archive semantics.

## Compatibility / Breakage

This is a destructive data normalization for rows already contradicting the new
state invariant. It changes invalid current facts to the nearest valid workflow
fact instead of preventing startup. The alternative is leaving affected
installations unable to start.

The constraint remains validated and fail-fast after normalization. Invalid enum
values remain protected by existing allowed-value constraints and are not
rewritten here.

## Validation

Add migration tests that assert:

- 0049 contains normalization updates for the conflicting combinations.
- Normalization appears before `add constraint`.
- 0049 still does not use `not valid`.

Run focused store migration tests, then the Go verification gate.
