# Production Docker Compose deployment design

## 1. User workflow and release assets

The production path is release-asset based:

```text
create deployment directory
  -> download compose.yaml
  -> download .env.example as .env
  -> edit every Must change value and review Recommended / Optional settings
  -> docker compose config
  -> docker compose up -d
  -> configure Nginx Proxy Manager to houfeng:16001
```

The release workflow checks out the exact release source, creates a version-pinned env asset, verifies that Compose resolves to the same `linnea7171/houfeng:vX.Y.Z`, and uploads both files to the matching GitHub Release. The repository templates stay reviewable; the published env asset carries the concrete release tag.

## 2. Service graph

```text
houfeng-storage-init (one shot) --> houfeng-secrets-init (one shot)
                                             |
db (healthy) --------------------------> houfeng-db-init (one shot) --> houfeng-record-authority (healthy)
clamav (healthy) --------------------------------------------------------------+            |
                                                                                           +--> houfeng (Center + Web) --> NPM
                                                                                           +--> houfeng-content-processor
```

- `houfeng-storage-init`: published project image, explicit root override, no secrets/network dependency, creates the UID/GID-10001 application paths plus separate public/private Records authority paths.
- `houfeng-secrets-init`: published project image, no network, stages only the four database-role secrets into service-specific private files below `./data/secrets` so read-only consumers remain compatible with Compose environment-backed secrets.
- `houfeng-db-init`: published project image, no restart, receives only staged provisioning secrets and the private authority state mount, runs idempotent deploy initialization/activation, and exits.
- `houfeng-record-authority`: published project image, long running, owns only its private signed state and auto-generated constrained authority database credential; validates the active contract and renews exact known memberships.
- `houfeng`: non-root release default, runtime DB secret plus Center-only initial admin/session secrets, baked Web UI, required Records/local-attachment/ClamAV settings.
- `houfeng-content-processor`: non-root read-only sandbox, runtime DB secret only, shared attachment bind and ClamAV.
- `clamav`: pinned scanner with `./data/clamav` cache.
- `db`: pinned PostgreSQL 16 with `./data/postgres`.

Center waits for the authority health check as well as one-shot initialization and ClamAV. The processor remains dependent on storage/database/ClamAV readiness. A failed initializer or authority verification blocks the application rather than starting a false-ready Records deployment.

## 3. Automatic database initialization

Add a bounded deployment route to the existing record-platform admin binary and include that binary in the project image. The route reads only its explicit Compose initialization inputs and secret files. It:

1. Builds a bootstrap DSN without logging credentials and opens a direct bootstrap connection.
2. Verifies PostgreSQL 16/bootstrap identity and applies the existing pre-R1 `pg_control_system()` ACL contract idempotently.
3. In a transaction, creates or tightens the fixed direct-login roles `houfeng_runtime`, `houfeng_platform_admin`, and `houfeng_migrator`; rejects role membership/attribute drift that cannot be safely converged; and makes the migrator the database owner.
4. Builds a direct migrator DSN from the same endpoint and the migrator secret.
5. Calls `ConvergeAppACLCurrent(ctx, migratorPool, "houfeng_runtime", "houfeng_platform_admin")`—the existing sole current APP schema/ACL writer.
6. Reopens a direct runtime connection and proves `AdmitAppACLCurrentRuntime` before reporting success.

Exact repeat performs verification and approved password rotation but does not append migrations/manifests or start application services early. The bootstrap, migrator, and platform-admin credentials never reach Center or processor.

## 4. Single-host Records authority

### Decision and rejected alternatives

The approved profile adds a self-contained authority for one Docker host. It preserves the existing fail-closed admission model while keeping the operator workflow automatic. Two shortcuts are rejected: db-init must not directly fabricate `deployment_membership` or `deployment_contract_state`, because that bypasses the external-ledger/full-witness projector contract; and Center must not receive an allow-all or relaxed admission gate.

### Durable state and proof

`./data/records-authority` is part of the coordinated recovery unit:

```text
records-authority/
  public/deployment-id
  private/authority-key
  private/database-secret
  private/activation-ledger.jsonl
  private/activation-receipt.json
```

The state is generated atomically only when both the authority directory and database contract are inactive. It contains a stable deployment ID, an Ed25519 key, the generated authority database credential, and a closed versioned activation bundle for the exact Compose membership inventory. The complete bounded local ledger is external to PostgreSQL, hash chained, canonicalized, and signed. Verification derives every digest and field of the existing `ContractActivationProjectionCommandV1`; no caller supplies trusted digests. Db-init invokes the existing migrator-owned projector, verifies its CAS receipt, and writes the receipt atomically. Exact repeat verifies byte-equivalent state and performs no new activation.

This profile provides integrity, consistency, exact-repeat, and fail-closed recovery for a single host. It does not claim off-host quorum or protection when an attacker controls both the host and its backup; tested off-host backups remain required.

Recovery is closed:

| Authority state | Database contract | Result |
| --- | --- | --- |
| absent | inactive/fresh | generate state, activate, verify receipt |
| valid | inactive/fresh | activate from existing state |
| valid | exactly active | verify and continue |
| absent/corrupt/mismatched | active | fail closed; restore PostgreSQL and authority state together |

### Least-privilege database authority

An additive migration creates the direct constrained login `houfeng_records_authority` and a single security-definer membership-heartbeat interface. The function accepts only a closed versioned command, checks the active matching deployment, exact known instance identity/capability, positive epoch/fence, and a bounded expiry, then upserts only those memberships. `PUBLIC`, runtime, admin, and migrator do not gain this execution path; the authority has no direct table DML and cannot execute contract projectors. The current APP ACL manifest owns the exact grant and deny set.

The authority process reads only its private state and constrained database secret, validates the signed ledger and database contract, and refreshes exact membership on a fixed internal cadence (nominally 30 seconds with a 90-second TTL). It becomes healthy only after the contract is active and membership is fresh. On shutdown it does not delete membership; expiry preserves fail-closed behavior. Center receives only the public deployment ID directory read-only plus fixed internal instance/kind/capability values. The content processor receives no authority secret and, unless it becomes an admission-gate consumer, is not added as cosmetic membership.

### Configuration boundary

No new operator-required variable is added. Fixed internal role/service/instance names and heartbeat timing remain image/Compose-owned. The Center configuration gains file-based deployment-ID loading so the public ID can be mounted without exposing the private authority directory.

## 5. Configuration model

The root `.env` is the only operator-edited configuration file. Secret values source service-scoped Compose secrets; ordinary values are explicitly mapped only to services that use them.

### Must change

- existing `HOUFENG_PROXY_NETWORK`
- external HTTPS `HOUFENG_PUBLIC_BASE_URL`
- PostgreSQL bootstrap/runtime/migrator/platform-admin passwords
- initial administrator username/password
- stable session HMAC key

Every required placeholder fails Compose interpolation or the init/app preflight. Fixed internal role/database/service names are not exposed as unnecessary knobs.

### Recommended

- release-pinned `HOUFENG_IMAGE`, already filled by the downloaded asset and changed only during a reviewed upgrade
- `HOUFENG_RECORDS_ENABLED=true` (approved default)
- portability capability, trusted proxy CIDRs, bcrypt cost, session TTL, incident sweep, processor bounds, and capacity/retention values actually supported by the binaries
- these carry conservative defaults but are grouped for review before long-term use

### Optional

- Telegram
- comparison workbench and its separately mounted keyring
- alternate S3 attachment backend with service-scoped credentials
- permanent-delete/witness options only when their full required backing contract is configured
- additional logging and performance tuning supported by current config

The default local full stack uses local attachment storage. Optional S3 values never weaken or silently override that default.

## 6. Nginx Proxy Manager contract

`HOUFENG_PROXY_NETWORK` names an already existing Docker network used by NPM. Center joins it with the stable alias `houfeng`; no other Houfeng service joins it. NPM configuration is:

- Scheme: `http`
- Forward Hostname/IP: `houfeng`
- Forward Port: `16001`
- Websocket support: enabled
- Public certificate: configured in NPM
- Force SSL: enabled

`HOUFENG_PUBLIC_BASE_URL` is the matching external `https://` origin. The operator sets `HOUFENG_TRUSTED_PROXIES` to the exact NPM-network CIDR when forwarded client addresses are required; the template never guesses an all-address CIDR.

## 7. Portable storage and lifecycle

The deployment unit is:

```text
compose.yaml
.env
data/
  postgres/
  attachments/
  logs/
  clamav/
  center-config/
  secrets/
  records-authority/
    public/
    private/
optional-secrets/
```

All business data is visible below `data/`. Upgrade means back up `.env` + coordinated PostgreSQL/attachments/authority state, update only the pinned image tag, pull, validate, and run `up -d`. Rollback requires database compatibility evidence; it is not presented as blindly changing a tag. Host migration stops writes, copies this directory preserving permissions, joins the same-named NPM network on the destination, then starts and verifies health. Restoring an active PostgreSQL database without its matching authority state is explicitly rejected.

## 8. Tests and failure matrix

TDD begins with deployment static/behavior tests that encode the new contract and fail against the old files.

| Condition | Expected result |
| --- | --- |
| missing required env placeholder | `docker compose config` fails before create |
| NPM network absent | `up` fails visibly; no public fallback port |
| storage init fails | Center/processor remain unstarted |
| DB unhealthy or deploy init fails | Center/processor remain unstarted; init logs safe diagnostic |
| fresh database | roles + current schema converge; runtime admission passes |
| exact repeat | succeeds without additional manifest/migration mutation |
| active DB + missing/corrupt/mismatched authority state | fail closed; never regenerate or overwrite active authority |
| authority unavailable or heartbeat stale | Center readiness/writes fail closed after bounded TTL |
| runtime/admin/PUBLIC attempts authority function or authority attempts table DML/projector | denied |
| role membership/unsafe attributes | fail closed before application startup |
| Records enabled | Center and processor start against current runtime role |
| one secret not granted to a service | it is absent from that service configuration/container |
| image/template release version mismatch | release job fails before upload |

Verification includes fake/isolated command tests, strict PostgreSQL 16 integration for provisioning and repeat, Compose config, real isolated Docker smoke where available, release-workflow static checks, full Go verification, and independent review.

## 9. Rollback and scope

Code rollback restores the previous deployment artifacts and image contents; existing new-format databases must not be started by an older image without compatibility proof. This task does not delete user data, migrate an existing non-compliant OID-10 database in place, manage NPM itself, or add the agent to Compose.
