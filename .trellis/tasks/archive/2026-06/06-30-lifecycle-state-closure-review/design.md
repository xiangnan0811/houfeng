# Lifecycle state closure final review design

## Architecture and Boundaries

本轮按只读审查执行，不直接修改业务代码。审查边界覆盖上一轮提交 `ec00ea3` 及其影响的状态合同：

1. **Domain / validation**：`internal/center/vpsassets`、`internal/center/subscriptions`、`internal/center/renewals`。
2. **Persistence / migration**：`internal/center/store/*`、`internal/center/store/migrate`、`db/migrations/0048_subscription_gift_renewal_mode.sql`。
3. **Incident service**：`internal/center/incidents/service.go` 和 service tests。
4. **HTTP / API**：VPS、subscriptions handlers 的 scope parsing。
5. **Frontend state / UX**：`web/src/lib/types.ts`、`assetOptions.ts`、`ArchivePage`、受影响页面的 browser sanity。
6. **Specs and task record**：`.trellis/spec/backend/database-guidelines.md`、`.trellis/spec/web/state-and-data.md`、上一轮 archived task research。

## Review Method

- Use `git show ec00ea3` and current HEAD to inspect exact implementation.
- Search repository for stale state strings, scope usage, old labels, migration wording, and potentially divergent validation.
- Trace write paths from UI/API/domain/store for the four affected concepts:
  - VPS lifecycle / usage / renewal decision.
  - Asset scope current / historical / archived / all.
  - Subscription renewal mode lottery / gift / bonus / auto.
  - Incident active/recovered state for inactive MonitoringInstance / Target.
- Run fresh verification sufficient to support the conclusion.
- Produce a review report rather than applying fixes.

## Compatibility and Destructive-Change Review

因为当前没有用户，review should not assume compatibility is always valuable. For each compatibility path:

- Identify whether it exists only for older clients or fixture convenience.
- State whether removing it would be destructive.
- Recommend keep/remove/defer with rationale.

Examples:

- `asset_scope=archived` alias for `historical`.
- Old migration `0031` still lacking `gift` before new migration `0048`.
- Existing `lottery` rows remaining lottery rather than auto-backfilled to `gift`.

## Outputs

- `research/state-closure-final-review.md`
- Optional project guide/spec update for persistent process memory.
- If no implementation changes are needed, finish with docs/task commits only.
