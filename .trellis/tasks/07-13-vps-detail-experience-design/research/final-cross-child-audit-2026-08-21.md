# Parent final cross-child audit 2026-08-21

Program children 1–12 are on protected main. Parent stays `planning`.
No staging cutover. No Release Please merge. Latest published tag remains
`v0.73.1`. Release Please opened
https://github.com/xiangnan0811/houfeng/pull/434 (`chore(main): release 0.74.0`)
and it is left unmerged.

## Delivery

| Child | Merge | Note |
|---|---|---|
| 1–9 | earlier PRs | archived |
| 10 | `9e910d7c` #425 | narrowed portability; `0058` max |
| 12 | `c7081519` #428 | archive restore fidelity; `v0.73.0` |
| 11 | `79f62aac` #433 | readiness + backup/restore assembly |

Child 11 main CI: run `32497370438` green (`go`, `web`, `web-browser`,
`docker-image`, PG16 catalog 16.0/16.6/16.12).

## Permanent delete

**Disabled.** Production HTTP is `handlers.RecordDeletions(nil)`.
Missing: `deletion.record_markdown_client`, `deletion.record_comparison`,
`recovery.record_search`, `recovery.record_collaboration`,
`recovery.record_portability`, and the production backup/restore pair.
`RBL-AC-06` holds as fail-closed.

## Returned owning-child defects

1. Alpine/musl `0058` `blob_key` CHECK rejects `rxa_portdelete`
   (`invalid regular expression: invalid repetition count(s)`).
2. MinIO import staging `invalid Blob request`.
3. No markdown/comparison deletion adapters (do not invent here).

Local/S3 integration profiles are not passing evidence because of (1)
and (2). Local recovery `--all`, security, local capacity, and browser
64/64 did pass.

## Migrations / ACL

Root migrations still end at `0058`. Child 11 added none.
Do not treat #434 or image publish as parent completion.
