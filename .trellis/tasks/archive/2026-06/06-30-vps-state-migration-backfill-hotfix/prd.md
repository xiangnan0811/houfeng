# Hotfix VPS state migration backfill

## Goal

Fix the `v0.55.6` startup failure caused by migration
`0049_vps_asset_state_combination_constraint.sql` adding a cross-column VPS
state constraint before existing historical rows have been normalized.

## Requirements

- The fix must keep the VPS state combination invariant introduced by 0049:
  `cancelled` requires a cancellation renewal decision and cannot be `in_use`;
  `to_cancel` requires a cancellation renewal decision; `to_migrate` requires
  `migrate`; `replaced` cannot remain `active` or `in_use`.
- Because 0049 fails before it is recorded in `schema_migrations`, it may be
  edited in place under the repository database guideline exception.
- The migration must be idempotent and safe to re-run on installations where
  0049 has not been recorded.
- Existing rows that can be deterministically normalized must be repaired before
  the constraint is added. The normalization must not invent lifecycle audit
  actions or hide invalid enum values outside the constraint's scope.
- The migration must still add a validated check constraint; it must not use
  `NOT VALID` to silently permit bad current rows.
- Tests must capture the production failure class so future edits cannot remove
  the backfill before the constraint.
- Local verification must cover Go tests/format/vet for the touched backend
  area before delivery.

## Acceptance Criteria

- [ ] `0049_vps_asset_state_combination_constraint.sql` normalizes existing
      conflicting VPS state combinations before adding
      `vps_assets_state_combination_valid`.
- [ ] Migration tests prove the normalization statements exist and run before
      `add constraint`.
- [ ] Relevant Go tests pass locally.
- [ ] The change is committed on a non-main branch and delivered through PR,
      CI, Release Please, release artifact, image publishing, and cleanup.

## Notes

- Production symptom:
  `apply migration 0049_vps_asset_state_combination_constraint.sql: ERROR:
  check constraint "vps_assets_state_combination_valid" of relation
  "vps_assets" is violated by some row (SQLSTATE 23514)`.
