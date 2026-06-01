# P0 login fails after VPS-first upgrade

## Goal

Fix the v0.26.0 upgrade regression where a real deployment rejects the existing username/password even though the operator did not change credentials or configuration. Upgrades must preserve existing users and must not silently report repository, migration, or database-shape failures as an ordinary wrong-password error.

## Confirmed Facts

- User reports the failure immediately after pulling the new image and upgrading the real environment.
- The visible UI error is `用户名或密码不正确`.
- `v0.25.1..v0.26.0` did not intentionally change password hashing, user seeding, users table schema, or Docker entrypoint credential handling.
- Current `auth.Service.Login` maps any `FindByUsername` error to `ErrInvalidCredentials`, not only `ErrUserNotFound`; this can hide database scan/query failures behind the same 401 message as a wrong password.
- Initial admin env vars are only supposed to seed the first user when `users` is empty. They must never overwrite an existing password during upgrade.

## Requirements

- Existing `users` rows and bcrypt hashes must remain valid across the v0.26.x upgrade.
- Login must still return 401 for genuine unknown username or wrong password without leaking account existence.
- Login must not turn repository/query/scan/database-shape errors into 401 wrong-credentials responses; those must surface as internal errors and be logged/testable.
- The fix must include regression tests that simulate an existing user from a previous version and confirm the same password logs in after applying current migrations/bootstrap.
- The fix must include regression tests that distinguish repository failures from invalid credentials at the service/handler boundary.
- No migration or bootstrap path may reset, rewrite, or reseed an existing user password.
- The release path must produce a new image tag for real-environment retesting.

## Acceptance Criteria

- [ ] A test with an existing bcrypt-backed user row logs in successfully after current migrations/bootstrap.
- [ ] A repository/query error during login produces an internal login failure instead of `ErrInvalidCredentials` / 401 wrong-password semantics.
- [ ] Wrong password and unknown user still return the existing 401 behavior.
- [ ] `make verify-go` passes.
- [ ] Targeted auth/bootstrap/store tests pass.
- [ ] A patch release is merged and Docker image publishing completes.

## Out Of Scope

- Password reset UI or manual account recovery tooling unless required to unblock the regression.
- Changing the public login copy beyond distinguishing server failure from wrong credentials where backend semantics already support it.
- Rotating or inspecting the user's real password.

## Notes

- Treat production data as sensitive. Any database inspection must be read-only unless the user explicitly authorizes a repair operation.
