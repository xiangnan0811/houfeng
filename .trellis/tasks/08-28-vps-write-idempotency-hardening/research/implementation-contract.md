# Bounded implementation contract

This task-local contract narrows the project specs to the rules that directly govern the three review findings. The source specs remain authoritative; this file exists so Trellis can inject the applicable slice without truncating oversized documents.

## Web ownership and state

- The authenticated `AppShell` is the lifetime boundary for shared VPS write state. Create one registry per authenticated user; never use a module-global singleton and never carry owner state across users.
- Legacy and Records v2 Overview must acquire the same per-VPS authority for every write. A pending write blocks another write for the same VPS, while different VPS IDs may proceed independently.
- Acquiring and releasing ownership must use opaque exact tokens. An old callback may not release or clear a newer owner/attempt.
- Route remount and Legacy/Overview gate changes must preserve an in-flight owner. When an owner started by an older view settles, the currently mounted view revalidates authoritative state.
- Idempotency attempt identity is caller-owned. For subscription and the four scoped creates, a transport-unknown result retains the key for an identical retry; changed input or `idempotency_key_reused` rotates it; confirmed success clears it.
- Compute a canonical request digest from the actual wire identity and VPS scope. Keep only the digest/key/operation metadata in shared state, never the raw note, domain, address, credential, or other form body.
- Collection creates (`POST /api/services`, `/api/domains`, `/api/monitoring-instances`) retain their existing signatures and behavior.

## HTTP and error mapping

- Each named VPS-scoped create accepts exactly one valid `Idempotency-Key`. Missing, duplicate, malformed, or otherwise invalid values fail before any repository create with HTTP 400 and code `invalid_idempotency_key`.
- First execution returns 201. Same key plus the same normalized digest returns the original resource with 200 and no extra write. Same key plus a different digest returns 409 and code `idempotency_key_reused`.
- Map only allowlisted stable errors. HTTP bodies and logs must not disclose the key, digest, request body, SQL text, wrapped internal error, or secret-bearing data.
- Preserve the path VPS ID as scope authority and preserve existing response DTO fields, capability gates, and lazy-loading behavior.

## PostgreSQL transaction and migration

- Use the repository's existing raw `pgx` style and injectable transaction boundary. Result insert and receipt insert must commit in one transaction; any begin, lock, lookup, scan, insert, receipt, or commit error fails closed.
- Normalize/validate, begin, acquire a transaction-scoped advisory lock namespaced by operation, look up the receipt, then replay/reject/create. Never rely on a process-local cache or a business-field uniqueness guess.
- Add the next unoccupied migration after the current released `0061`; do not edit any released migration. Register the new source and update the current APP ACL fragment and migration tests together.
- Store request digest and result foreign keys in explicit receipt tables. Deleting the result may cascade its receipt; do not introduce TTL, janitor, update, or best-effort cleanup behavior.
- Runtime APP access is least privilege: the current fragment grants only the relation/sequence permissions actually required for create and replay, including receipt `SELECT`/`INSERT`, with no broad write grant.
- Monitoring instance, VPS link, and receipt belong to the same existing linked-create transaction and must replay both original IDs.

## DTO mirror

- Contract fields represent base JSON `type`, optional `format`, `required`, and `nullable` separately.
- A date is `type: string` with `format: date`. A nullable ordinary string remains `type: string`, nullable, and has no date format.
- TypeScript uses an exact source-verified date alias whose underlying definition is `string`; widening or an unknown alias fails closed. Go date types map to the same base-type/format representation.
- Preserve existing strict parser behavior for missing/null semantic keys, mixed primitives, anonymous embedding, unsupported aliases, and malformed manifests.

## Change discipline

- Keep the diff limited to the three findings and their tests, migration, ACL, and evidence. Reuse the subscription receipt pattern where its invariants match, while keeping operation-specific domain scanners and error mapping clear.
- Work only in `codex/vps-write-idempotency-hardening`. Do not stage, commit, push, open a PR, merge, release, archive, or clean the worktree.
