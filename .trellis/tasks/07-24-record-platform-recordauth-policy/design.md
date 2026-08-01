# Actor scope 与统一 recordauth.Policy — Design

## 1. Boundary and dependency direction

```text
session cookie -> auth.Service.UserBySession -> RequireSession
                                              |
                         PostgresRecordAuthorizationRepository
                                              |
                                              v
                          recordauth.NormalizeActorScope
                                              |
                                              v
                         sessionctx.ActorScope (typed context value)
                                              |
                            future API / store / worker callers
                                              v
                                  recordauth.Policy
```

`recordauth` is pure: it imports only Go standard library packages. It does not import `auth`, `http`, `store`, a future record domain, or a database driver. HTTP performs the one allowed translation from the current `auth.RoleAdmin` string to `recordauth.RoleProjectAdmin`. Store imports `recordauth` to implement its narrow repository interface. This prevents a future business package from becoming a second authorization authority.

## 2. Trusted actor construction

The current product has one project and one persisted login role. The middleware therefore uses:

```go
groups, err := scopes.ListActorGroupIDs(ctx, recordauth.ProjectIDDefault, u.UserID)
actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
    UserID: u.UserID,
    Role: recordauth.RoleProjectAdmin, // only after u.Role == auth.RoleAdmin
    ProjectID: recordauth.ProjectIDDefault,
    GroupIDs: groups,
})
```

There is no case-insensitive role conversion and no fallback for an unsupported role. `NormalizeActorScope` validates opaque ID grammars, checks the exact project/role registry, sorts and de-duplicates group IDs, and returns a copy so callers cannot mutate a stored scope. Middleware writes both this typed value and the legacy user ID to context; existing handlers continue to use the latter until they are converted.

The repository query is limited to:

```sql
select g.group_id
from public.record_access_groups g
join public.record_access_group_members m on m.group_id = g.group_id
where g.project_id = $1 and m.user_id = $2
order by g.group_id asc
```

No header participates in construction. A repository/database/normalization failure is authorization infrastructure unavailable and returns a fixed 503 envelope. Invalid/expired authentication remains the existing fixed 401 path.

## 3. Canonical authorization model

The package defines closed v1 types:

- `ActorScope`: user, canonical role, exact project, sorted stable group IDs.
- `Capability`: an exhaustive v1 set for record, draft, evidence, attachment, search, activity, comparison, notification, import/export and permanent deletion operations. Unknown strings fail before scope evaluation.
- `VisibilityScope`: exact version, kind, project, allowed roles/groups and policy version/revision. `project` and `restricted` are distinct; an empty restricted list means deny-all, not project-wide access.
- `SourceAuthorization`: a tagged `live | tombstoned` union with capture scope. A live source has exactly `CurrentScope`; a tombstoned source has exactly `FinalFloor` plus the tombstone-only canonical `LastLiveScope` transition witness. `LastLiveScope` is validation evidence, not a second authorization scope. Initial registered source kinds are only `vps`, `monitoring_instance` and `target`; later owners must extend the registry with their adapter and tests.
- `ResourceScope`: project visibility plus one or more source authorizations.

Canonical encoding is a fixed field-order, length-prefixed binary body with sorted unique role/group arrays. The SHA-256 of a normalized visibility body is the final floor/witness hash. The source body includes its kind/ID/state/capture/current or tombstone floor plus `LastLiveScope`, so its digest cannot be replayed under another source, state, or transition witness. JSON, maps, trim/case-fold behavior and caller-supplied digest values are never authoritative.

## 4. Authorization decision

The sole `Policy.Authorize` implementation:

1. validates actor and capability;
2. validates and normalizes the resource/visibility;
3. rejects cross-project access;
4. verifies the actor can perform the capability (`project_admin` is the current full-capability project role; `viewer` is read-only);
5. verifies a tombstone's `LastLiveScope <= CaptureScope` and `FinalFloor <= LastLiveScope`, then evaluates the actor against visibility, capture scope, and each selected source's current scope or final floor;
6. rejects if any component denies, a live or tombstone transition widens, the strict union is malformed, or any recomputed hash/digest differs.

Project admin does not bypass malformed/tampered scope evidence or project boundaries. A non-admin needs its role/group to survive every applicable restricted intersection. The policy returns internal typed reasons without resource IDs; HTTP record callers will map `ErrDenied` to their ordinary opaque 404 response. No endpoint exists in this slice, so the mapping is a contract rather than a new route.

## 5. Compatibility and deferred integration

`RequireSession` changes from one argument to `RequireSession(authn, scopes)`. Production bootstrap constructs the repository from the already-open APP runtime pool and passes the resulting middleware closure through the existing RouterOptions.AuthMiddleware seam. RouterOptions itself remains unchanged because adding an unused repository field would create a misleading second configuration path.

The revision-checkpoint deletion filter is deliberately not implemented here: the current clean baseline has neither its required source columns nor a production caller capable of retaining the mandatory final per-row policy check. The later deletion owner must call this policy and add the query predicate in the same change; a standalone helper would be test-only security theater.

## 6. Risks and rollback

The main compatibility risk is breaking handlers that still read user ID. The legacy context value remains populated and focused regression tests cover it. The safety risk is availability during scope-store failure; the fixed 503 makes the refusal visible without pretending authentication failed. There is no migration or write path in this slice, so reverting the feature branch restores the earlier runtime behavior.
