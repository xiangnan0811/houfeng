# Compose authority 与协调恢复

## Scenario: single-host Compose Records authority and membership heartbeat

### 1. Scope / Trigger

- Trigger：修改 `internal/center/recordauthority/`、Compose deploy init/authority runtime、`0060_create_records_authority_heartbeat.sql`、current APP ACL authority fragment、activation projector wiring、deployment-ID file loading、或 `deployment_contract_state` / `deployment_membership` 的 Compose recovery contract。
- 此场景只批准 single-host Compose Records authority：durable signed state 位于 PostgreSQL 外，activation 必须复用现有 migrator-owned projector，membership renewal 必须通过一个窄 heartbeat function。不得让 db-init fabricate contract/membership rows，也不得给 Center allow gate。

### 2. Signatures

```go
func recordauthority.CreateComposeState(root string) (recordauthority.VerifiedComposeState, error)
func recordauthority.LoadComposeState(root string) (recordauthority.VerifiedComposeState, error)
func recordauthority.PersistActivationReceipt(root string, state recordauthority.VerifiedComposeState, receipt []byte) error
func recordauthority.VerifyActivationReceipt(root string, state recordauthority.VerifiedComposeState) error
func recordauthority.MarshalMembershipHeartbeatCommandV1(state recordauthority.VerifiedComposeState, issuedAt time.Time) ([]byte, time.Time, error)

func deploy.InitializeCompose(ctx context.Context, config deploy.ComposeInitConfig) error
func deploy.RunComposeAuthority(ctx context.Context) error
```

- Verified state derives the existing `recordplatform.ContractActivationProjectionCommandV1`; trusted callers do not supply activation/ledger/identity digests.
- Database boundary is exact `public.record_platform_cas_contract_activation_projection(bytea)` for activation and `public.record_platform_compose_membership_heartbeat(bytea)` for renewal.

### 3. Contracts

- State root is atomic and closed: stable deployment ID, Ed25519 private key, generated constrained database password, canonical bounded signed hash-chained activation ledger, and canonical CAS receipt. The signed ledger binds a domain-separated digest of the exact database credential so a different valid-looking 64-hex secret is corruption, not rotation. `LoadComposeState` rechecks file type/mode/size, canonical JSON, complete ledger sequence, every derived digest, signature, fixed Compose inventory, credential shape, and signed credential digest before returning a capability-bearing `VerifiedComposeState`.
- Recovery matrix is exact: absent state + inactive/fresh DB may call `CreateComposeState`; valid state + inactive DB activates; valid state + exact active DB verifies; absent/corrupt/mismatched state + active DB fails closed. Never regenerate over an active contract. Cold backup/host migration must restore PostgreSQL and authority state together.
- Activation marshals only `VerifiedComposeState.ActivationCommand`, calls the existing migrator-owned CAS projector, verifies/persists its returned receipt, and publishes only a derived deployment ID for Center. Db-init must not insert/update `deployment_contract_state` or `deployment_membership` directly.
- Compose provisions `houfeng_records_authority` as a distinct direct login with constrained attributes and no membership edges. The current APP ACL grants only database connect, `public` schema usage, and execute on the heartbeat function; it grants no table/sequence DML or activation/domain-rotation projector execution. `PUBLIC`, runtime, platform-admin, and migrator cannot execute the heartbeat function.
- Migration `0060` owns a single `SECURITY DEFINER`, `search_path=pg_catalog`, one-`bytea` heartbeat overload with PUBLIC revoked. The closed 177-byte command fixes project `default`, instance `compose-center`, kind `api`, capability `records.runtime`, load-balancer admitted true, queue admitted false, and binds the active deployment ID/epoch/fence.
- Heartbeat accepts issued time only within the bounded clock window and exact 90-second expiry; expiry must be future and at most 120 seconds from DB transaction time. It locks/checks the exact active contract and existing exact membership before inserting or updating only `heartbeat_expires_at`/`updated_at`. Foreign/stale/future/mismatched commands produce zero mutation.
- Authority reloads/reverifies the same state before every renewal, uses only its state-derived DB credential, refreshes every fixed 30 seconds, and is healthy only after a verified successful membership receipt that remains fresh. Shutdown never deletes membership; expiry restores fail-closed admission.
- Center reads only the bounded deployment-ID file plus fixed internal instance/kind/capability. Missing/malformed/oversized/control-character content fails config; the processor receives no authority identity or secret.

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| fresh DB + absent authority state | atomically create one state, provision role, converge current ACL, activate via projector, persist receipt, heartbeat, admit runtime |
| exact repeat | reuse byte-identical state/identity and active epoch/fence; only bounded membership expiry and approved operator-role passwords may change |
| active DB + absent/corrupt/foreign state | fail before application startup; zero replacement state/contract/membership mutation |
| inactive DB + valid state | activate from the verified existing bundle; never mint a second identity |
| heartbeat deployment/epoch/fence/identity/time mismatch | function rejects with zero membership mutation |
| authority tries table DML/projector or runtime/admin/PUBLIC tries heartbeat | PostgreSQL permission denied |
| authority state changes while process runs | re-verification fails before next heartbeat; health becomes unavailable/stale |
| membership expires | `AdmitAppACLCurrentRuntime`/Records write path fails closed; no local bypass |
| DB-only or state-only restore | unsupported split recovery; restore PostgreSQL and authority state together |

### 5. Good / Base / Bad Cases

- Good：fresh Compose init creates signed state outside DB, derives `ContractActivationProjectionCommandV1`, calls the existing projector, persists/verifies receipt, then the constrained role establishes exact membership and Center publishes a real admitted Record.
- Good：authority restart loads the same key/credential/deployment ID, renews the same membership, and becomes healthy without changing contract epoch/fence.
- Base：brief authority outage leaves existing membership usable only until its DB-bounded expiry; afterward Records transactions fail closed until verified renewal returns.
- Bad：db-init writes membership rows, derives trust only from current DB rows, accepts caller-provided witness digests, rotates to new local state over an active DB, or gives authority/privileged credentials to Center.

### 6. Tests Required

```bash
go test ./internal/center/recordauthority ./internal/center/deploy \
  ./internal/center/store/migrate ./cmd/houfeng-record-platform-admin -count=1

scripts/test-record-platform-integration.sh postgres -- \
  go test ./internal/center/deploy -run '^TestPostgresIntegrationComposeInitialize$' -count=1 -v
```

- Unit tests cover atomic create, exact repeat, canonical ledger/signature/digest derivation, hostile/truncated/oversized/mode-drift state, receipt CAS, closed heartbeat codec, Center deployment-ID file, safe stage errors, refresh/health/shutdown, and complete recovery matrix.
- Migration/current-ACL tests freeze the one function/role/object set and prove no privilege expansion. Strict PostgreSQL 16 evidence must use unique test role/database names, never pre-drop fixed production names, and prove authority-only heartbeat plus direct-DML/projector/runtime/admin/PUBLIC denials.
- PostgreSQL tests cover exact active contract/membership, TTL, restart renewal, stable epoch/fence/key across operator password rotations, and byte-for-byte zero mutation after stale/foreign/truncated/future commands.
- Real isolated Compose smoke proves fresh automatic init with no SQL helper, authority health, actual admitted Records write, attachment/ClamAV flow, restart/exact repeat, corrupt-state fail-closed, and coordinated portable copy. Task-owned containers/networks/data must be unique and cleaned without touching unrelated resources.

### 7. Wrong vs Correct

```sql
-- Wrong: db-init fabricates authority rows or grants runtime a writer.
insert into public.deployment_membership (...) values (...);
grant update on public.deployment_membership to houfeng_runtime;

-- Correct: only the constrained authority calls the closed security-definer codec.
select public.record_platform_compose_membership_heartbeat($1::bytea);
```

```go
// Wrong: trust caller digests or DB rows alone.
command := recordplatform.ContractActivationProjectionCommandV1{ActivationBundleDigest: callerDigest}

// Correct: LoadComposeState verifies the complete external ledger and returns
// the already-derived command capability used by the existing projector.
state, err := recordauthority.LoadComposeState(root)
command := state.ActivationCommand
```

---
