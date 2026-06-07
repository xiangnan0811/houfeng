# P0 Login Upgrade Regression Design

## Diagnosis Boundary

The visible failure is an authentication failure after upgrade, but the current service boundary collapses all `FindByUsername` errors into `ErrInvalidCredentials`. This hides the difference between:

- real invalid credentials (`ErrUserNotFound`, bcrypt mismatch, invalid username shape);
- repository/database failures (`query user: ...`, scan errors, missing/changed columns, connectivity or permission issues).

The first class must remain a generic 401. The second class must be a server-side failure so operators do not chase phantom password changes.

## Auth Contract

- Keep account-existence protection: unknown username and wrong password still return `ErrInvalidCredentials`.
- Preserve timing equalization for unknown users.
- Propagate non-`ErrUserNotFound` user repository errors from `Service.Login` with context.
- Handler keeps mapping `ErrInvalidCredentials` to 401; propagated repository errors map to 500 `login failed`.
- Do not expose password hash values, usernames beyond operator input, or raw SQL details in browser responses.

## Upgrade Contract

- `SeedInitialUser` remains create-only when `CountUsers == 0`.
- Current migrations must not modify `users.password_hash`.
- Add an integration-style store/auth test that creates a legacy/current user row with a real hash, applies current repository/service login, and confirms login succeeds.
- Add a failure-path test with a repository that returns a generic error from `FindByUsername`; this must not become `ErrInvalidCredentials`.

## Operational Notes

If the real deployment still fails after this patch, the browser should report a server-side login failure rather than "用户名或密码不正确", and logs should identify the underlying repository problem. That is a safer failure mode for upgrade diagnosis and prevents accidental password resets.

