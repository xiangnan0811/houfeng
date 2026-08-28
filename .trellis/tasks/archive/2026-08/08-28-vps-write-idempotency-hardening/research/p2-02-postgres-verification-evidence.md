# P2-02 strict PostgreSQL verification evidence

- Date: 2026-08-28 (Asia/Shanghai)
- Worktree: `/home/murray/code/houfeng/.worktree/vps-write-idempotency-hardening`
- Branch: `codex/vps-write-idempotency-hardening`
- Current commit: `da83a96769b618c6e223f71a1d2c6645d54c853b`
- Task state: in progress; retained for external review
- Runner: repository-owned disposable PostgreSQL `postgres` mode; temporary database/container state was cleaned by the runner

## VPS create lost-response replay

Command:

```bash
./scripts/test-record-platform-integration.sh postgres -- go test ./internal/center/store -run '^TestVPSCreateIdempotencyLostResponsePostgres$' -count=1 -v
```

Result: PASS. Four of four required subtests executed and passed with zero skips:

- `experience_log`
- `asset_service`
- `asset_domain`
- `linked_monitoring_instance`

The linked-monitoring subtest used an empty/defaultable wire identity plus explicit group, changed the VPS defaults after the first commit, and replayed the original monitoring/link IDs and stored first defaults with one materialization.

## Current APP ACL scenarios

Command:

```bash
./scripts/test-record-platform-integration.sh postgres -- go test ./internal/center/store/migrate -run '^TestPostgresIntegrationAppACLCurrent$' -count=1 -v
```

Result: PASS. All four top-level scenario groups executed and passed with zero skips:

- `fresh_and_runtime`
- `exact_repeat_is_read_only`
- `prior_baseline_requires_rebuild_without_mutation`
- `unrelated_same_name_objects_are_ignored`

This evidence records only allowlisted command metadata and pass counts; it does not include a DSN, SQL text, idempotency key, digest, request content, note/details, or raw error output.
