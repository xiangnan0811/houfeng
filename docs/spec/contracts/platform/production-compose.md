# 发行部署资产与 Compose 合同

## Scenario: release-asset production Compose and image contract

1. **Scope / Trigger**
   - 触发：修改 `Dockerfile`、`compose.yaml`、`compose.proxy-network.yaml`、`compose.proxy-host.yaml`、`docs/deploy/compose.env.example`、README / deployment docs、Center/processor/ClamAV/PostgreSQL/Records authority 边界，或 `.github/workflows/publish-images.yml`。
   - 目标：同一 GitHub Release 按 `compose.yaml` → `compose.proxy-network.yaml` → `compose.proxy-host.yaml` → `compose.env.example` 的固定顺序发布四个 deployment assets。operator 下载全部四个文件、把 env template 保存为 `.env`，再通过 `COMPOSE_FILE` 选择恰好一个 thin proxy overlay；普通 `docker compose config/pull/up` 命令不变，不 checkout source、不 build、不运行 SQL/helper launcher。
   - 代理责任方向：Houfeng 适配 operator 已有的 NPM，而不是为了 Houfeng 创建 placeholder network、复制整套 Compose，或强迫 operator 改造 NPM。Agent 仍是 monitored host 上的 Linux/systemd workload，不进入 Center Compose。

2. **Signatures**
   - Root `Dockerfile` produces one published image containing `houfeng-center`, `houfeng-content-processor`, `houfeng-record-platform-admin`, baked `web/dist`, curl, and Poppler. Normal runtime stays `USER houfeng:houfeng` (UID/GID 10001); only explicit one-shot/authority services override identity.
   - Common `compose.yaml` is the only full graph: `houfeng-storage-init`, `houfeng-secrets-init`, `db`, `houfeng-db-init`, `houfeng-local-namespace-init`, `houfeng-record-authority`, `clamav`, `houfeng`, and `houfeng-content-processor`. It has no `build:`, Caddy, agent service, `HOUFENG_PROXY_NETWORK`, external proxy network, or Center `ports`; Center explicitly keeps `networks: {default: ...}`.
   - `compose.proxy-network.yaml` is a thin overlay that only adds Center to `houfeng-proxy` with alias `houfeng` and defines `name: "${HOUFENG_PROXY_NETWORK:?set HOUFENG_PROXY_NETWORK for shared-network mode}"` plus `external: true`. It must not duplicate the service graph or publish a port.
   - `compose.proxy-host.yaml` is a thin overlay that only adds one long-syntax TCP mapping with structured fields `target: 16001`, `published: "16001"`, `host_ip: 127.0.0.1`, and `protocol: tcp`. The effective behavior is `127.0.0.1:16001 -> 16001/tcp`; static and rendered-config checks must inspect the structured fields instead of treating a renderer-specific short form as the exact serialized Compose config. The overlay contains no external network, alias, or `HOUFENG_PROXY_NETWORK` reference.
   - The tracked env template has exactly `Must change`, `Recommended`, and `Optional` operator sections and exactly one active default selector: `COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml`. Existing `network_mode: host` NPM operators switch it to `COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml` and leave `HOUFENG_PROXY_NETWORK` blank.
   - The ordered public deployment asset signature is exactly `required_deployment_assets=(compose.yaml compose.proxy-network.yaml compose.proxy-host.yaml compose.env.example)`. The release job writes the matching immutable `docker.io/linnea7171/houfeng:vX.Y.Z` into `HOUFENG_IMAGE`; installation does not ask the user to edit it.
   - The deployment-assets job uses `concurrency.group: deployment-assets-${{ needs.resolve.outputs.version }}` with `cancel-in-progress: false`. This serializes deployment-asset mutation for one resolved version: `cancel-in-progress: false` does not cancel the running job, while queued pending runs may be replaced by a newer run. The group therefore prevents simultaneous mutation by running jobs, but does not promise FIFO ordering or change cancellation outside this job group.
   - GitHub release asset URLs use `https://github.com/xiangnan0811/houfeng/releases/...`; the Docker registry identity remains the separately configured published image path.

3. **Contracts**
   - Mode selection is common base + exactly one selected overlay. Shared-network is the supported default; loading both overlays is unsupported because it combines two distinct proxy reachability surfaces. Compose profiles are not a substitute because interpolation happens per file and profiles cannot make the top-level required external network conditional.
   - In shared-network mode, Houfeng joins the existing NPM user-defined network; NPM remains unchanged and uses upstream `houfeng:16001`. The base and shared-network mode publish no host port, only Center joins the external network, and a blank network value fails interpolation before startup. Do not set `HOUFENG_PROXY_NETWORK=host` or invent a placeholder network.
   - In host-proxy mode, an existing `network_mode: host` NPM remains unchanged and uses upstream `127.0.0.1:16001`. Center stays on the Houfeng private default network for PostgreSQL and ClamAV and gains exactly one fixed IPv4-loopback mapping. Support requires Docker Engine `28.0.0+`; no configurable bind address, all-interface/LAN/IPv6 mapping, old-Engine firewall workaround, or public direct-Center mode is part of the contract.
   - Ordinary fresh Compose initialization sequences storage preparation, secret staging and healthy PostgreSQL, db-init, then the local namespace initializer before either business writer. Db-init converges the current ACL, activates the signed genesis and projects its V2 successor with exact receipt verification. Authority waits for db-init and healthy PostgreSQL; Center also waits for namespace initialization, healthy authority and ClamAV; processor waits for namespace initialization and healthy PostgreSQL/ClamAV. Schema convergence alone does not admit storage. Source-enabled adoption and V2 source initialization are unsupported; do not downgrade to an ordinary path. Existing authenticated source V1 remains a distinct retained contract.
   - DB gets bootstrap only; Center gets runtime, initial-admin and session; processor gets staged runtime only; db-init gets staged provisioning secrets; authority reads its generated private state/database secret. The stager has a read-only data parent with staged secrets masked and a writable secret directory. The namespace initializer alone gets the separate platform-admin file via a read-only mount: owner UID10002, group GID10001, mode `0440`, within a UID10002:GID10001 `0750` directory. No runtime receives that file or other provisioning/authority credentials. Optional comparison keys go only to Center; the S3 credential directory is shared with the processor. Optional-secret directories/files use UID/GID `10001`, modes `0700`/`0400`.
   - Production pins `HOUFENG_RECORDS_ENABLED=true` and permanent delete false. Center uses `compose-center` / `api` / `records.runtime`; processor uses its distinct `compose-processor` / `worker` / `records.runtime`. Both read the published deployment ID and their own launch binding from the read-only public authority mount. V2 heartbeat atomically renews both fixed memberships; a worker's claim, renewal and completion also require the worker effect-time gate. A heartbeat alone is not proof the worker is alive.
   - C81 requires the independent `HOUFENG_RECORD_DELETION_WORKER_PASSWORD` secret for the manifest-bound `houfeng_record_deletion_worker` login. Its staged file is db-init-only; the graph does not start a deletion worker. Neither credentials nor admission enable permanent-deletion HTTP: G2 remains NOT_READY and HTTP CLOSED.
   - For fresh `local`, the one-shot initializer runs as UID/GID10001 with direct platform-admin access and a separate UID10001-owned `0700` `data/local-namespace-init` journal. It verifies authority before modifying BlobRoot, durably records three UUIDs and reuses them for exact replay; the namespace marker is published atomically. For an unbound released root, ordinary cold upgrade accepts only a UID/GID10001 `0700` root whose sole entry is an empty UID/GID10001 `0700` `sha256` directory and whose database has no existing local blob references; missing, empty or populated roots are refused before changes. Fresh installation retains its separate initialization behavior. The controller checks database references before ACTIVE and again after fencing; the initializer checks the physical tree. On a source-enrolled stack the one-shot only verifies an existing matching binding and never provisions one. For `s3` it performs no local namespace initialization. Changed boot or copied physical namespace is not automatically rebound. See [deployment guidance](../../../deploy/local-and-systemd.md#local-blob-namespace-admission).
   - Db-init runs as UID10002 with supplementary GID10001 and publishes two protected `0440` launch bindings. Authority verifies the actual Center and processor executable digests before membership renewal. Fresh V2 and exact repeat preserve signed state and receipts; bounded heartbeat expiry may renew. Active ordinary V1 requires the explicit cold transition, not a normal init or silent launch replacement. An earlier private 0081 checksum is unsupported, not rewritten.
   - Durable local state includes `./data/postgres`, `./data/attachments`, `./data/clamav`, `./data/records-authority`, `./data/center-config`, `./data/local-namespace-init`, and `./data/secrets`. PostgreSQL, local attachments, authority state and namespace journal require a coordinated recovery point; a copy alone does not authorize a new-boot or new-physical namespace rebind. The permanent ordinary management guard and durable disposition under `./maintenance` are outside the rollback unit and must be retained. Storage preparation establishes the root marker and guard; normal management holds the shared guard while checking disposition and performing protected effects. Durable ACTIVE or missing/corrupt established disposition fails closed after a controller dies. Namespace initialization mounts the maintenance parent read-only: its public marker and guard are readable, private operation state is not readable by UID10001. Never delete/reset these records to force progress.
   - All services use one shared logging anchor: Docker `local`, `max-size: "10m"`, `max-file: "3"`, `compress: "true"`. Center uses stdout only, with no `HOUFENG_LOG_FILE`, log bind mount, or storage-init log directory. Health evidence uses Compose logs and actual rotation, not `center.log` existence. Do not modify Docker daemon, NPM, or shared journald configuration.
   - Processor stays non-root, read-only, `cap_drop: ALL`, `no-new-privileges`, core=0, with bounded `noexec,nosuid,nodev` tmpfs. Center and processor share the runtime DB role, public deployment identity and launch directory, Blob contract, attachment bind, and scanner settings.
   - 未启用 source authority 时，`.env` 密码使用独立且互异的 hex 值（`openssl rand -hex 32`），避免 dotenv 插值和跨角色复用；db-init 在 mutation 前拒绝重复 secret。只编辑密码不保证 restaging 或服务重建。普通受控轮换按顺序运行：`docker compose stop houfeng houfeng-content-processor`；`docker compose run --rm houfeng-secrets-init`；`docker compose run --rm houfeng-db-init`；`docker compose up -d --force-recreate houfeng houfeng-content-processor`。Source-enrolled 时 init、stager 和 heartbeat 使用同 kernel 同 flock domain 的共享 maintenance guard，并核验当前 credential commitments；旧 `.env`/staged secret 不可回写修复 mismatch，需受控 credential transition，不能把上述普通轮换序列当作绕过手段。
   - 已存在的 source V1 合同保留原有 enrollment/guard 边界；普通 V2 初始化与 cold controller 不支持 source-enrolled 或 installed-birth 状态。外部旧 writer fence、source reconciliation 与 APP/W2 handoff 是独立工作，普通路径不能绕过或替代它们。
   - `.github/workflows/publish-images.yml` may publish only on `release.published` or explicit maintainer dispatch. It checks out the resolved release source, builds the image, stages all four files under their public names, pins the release image in `compose.env.example`, then renders both modes before upload with explicit `-f` pairs. The shared render supplies a nonblank network; the host render unsets `HOUFENG_PROXY_NETWORK`; their combined project-image set must contain the one exact release image before manifest inspection.
   - Upload is a non-destructive fail-closed idempotent upload. For every exact required name, an absent asset may be uploaded, one byte-identical existing asset is retained, while duplicate names or differing existing bytes stop the job. Never use clobber, delete, replay after ambiguous mutation, or overwrite as recovery behavior.
   - Post-upload public readback uses the same ordered array, queries all release asset names, requires exact-name cardinality of one for each deployment asset, downloads each exact name to a fresh trap-cleaned directory, and proves byte identity with the staged file before success summary. It rejects legacy `.env.example` / `default.env.example` while preserving unrelated agent assets; total Release asset count is not fixed at four.
   - Direct/systemd documentation may retain explicit pre-R1 provisioning as an advanced path, but the Docker quick-start section must contain no manual SQL, local toolchain, source build, or helper launcher.

Ordinary cold upgrade is an explicit root controller operation on local rootful Docker with a pinned locally present target image. Stop unsupervised writers and external restart controllers first; the controller fences Compose and uses the exact extracted static target executables. Preserve its durable operation and guard state and retry the same operation after interruption; do not clean, reset, replace the guard, or restart old writers while ACTIVE. It preserves authentic V1 history while transitioning supported ordinary V1 to V2 and only operates against the same physical admitted namespace. No HTTP enablement follows from this procedure.
Before any change, cold preflight requires `.env` `COMPOSE_FILE` to select exactly `compose.yaml` and one of the two published proxy overlays; it pins the selected files and rejects any other selection. A wrong-checksum or unsupported C81 predecessor is also refused before any change.
The canonical working directory and every ancestor must be protected, free of symlinks and traversable by the UID10002/GID10001 native child; a user-owned `0700` home is unsuitable. Operator Compose assets, `.env`, request and restart recipe must be root-controlled and not writable by that child. The child receives no Docker socket or credentials; the root controller alone handles fencing and recreation. Reject unsafe existing ancestry; never alter a user's home or relocate an established identity automatically.

The host `houfeng-record-platform-admin` running `deploy-ordinary-upgrade` must be root-owned, not group- or world-writable, and executable by UID10002 through root-controlled, UID10002-traversable directories (for example `/usr/local/bin` with binary mode `0755`). The trusted preparation stage re-executes this host controller binary as UID10002 before launching the pinned target. A copy under `/root` or another inaccessible `0700` directory is refused with a preflight error.

The host controller runs as root and retains effective `CAP_KILL` and `CAP_SYS_PTRACE` throughout the operation. Native spawn and probe check both before creating any child and refuse otherwise. `CAP_KILL` permits pidfd cancellation and kernel delivery of parent-death SIGKILL to the UID10002 participant; `CAP_SYS_PTRACE` permits independent `/proc` inspection of the nondumpable child. Plain host root normally has both; capability-dropping sudo, systemd units and containers must retain them. The UID10002 participant itself has zero capabilities.

Native preparation and the participant share the host mount namespace; their filesystem access is the access available to trusted UID10002, not a narrow container mount or kernel filesystem sandbox. Root, UID10002 and the root-selected target code are trusted for the entire operation. The boundary instead relies on the exact root-pinned target, typed selected paths, minimal environment, five database credentials delivered through private pipes, zero capabilities, no-new-privileges, race-free descriptor closure, no Docker access, the per-deployment guard and SQL admission. No Landlock, chroot, separate mount namespace, LSM or sysctl configuration is assumed, and no resistance to a hostile UID10002 peer is claimed.

The controller acquires its permanent `<working-directory>/.ordinary-controller.lock` first, then the ordinary exclusive guard; neither lock is removed or replaced. Bootstrap of a missing `maintenance/ordinary` guard is journalled, and the root marker preserves continuity and blocks a silent rebind. A missing, corrupt or conflicting established record fails closed; retaining the same operation and permanent records is required for recovery.

The cold request binds seven distinct canonical root-owned single-link secret files, mode `0400` or `0600`: bootstrap, runtime, platform-admin, deletion-worker, migrator, Center initial password and session HMAC key. The last two reproduce target Compose secrets, not HTTP-proof credentials. Before fencing, the session key must be witnessed against the old actual Center key; no implicit rotation is allowed. The controller supplies file-backed secrets from a root-only `0700` private store with `0444` files to the selected target override, without raw values in Docker client environment/arguments or the durable journal. Per-secret commitments live in a root-private record; shared intent contains only its nonce-bound digest.

The root controller rejects ambient `PG*` and `SSL_CERT*` configuration; operators must start with a clean environment. The native participant uses a minimal environment and assumes UID10002 peers are trusted throughout the operation; it does not provide hostile-same-UID isolation. Privileged helper SIGKILL/control EOF containment does not alter normal runtime Docker SIGTERM behavior. This contract requires no LSM, sysctl, or persistent system-configuration change.

The implemented `deploy-ordinary-upgrade` path has acceptance evidence from an owned nested Docker host, not a real-host installation. arm64 is cross-built only; lost-commit and pre-lock backend cases have component coverage only. G2 remains NOT_READY, permanent-deletion HTTP remains CLOSED, and the candidate has not been deployed.

Center 默认读取 `birth-launch.json`，processor 默认读取 `birth-launch-processor.json`；两者均支持 `HOUFENG_RECORD_BIRTH_LAUNCH_FILE` 指定受保护绝对路径。已安装 lineage 还需要独立 `HOUFENG_RECORD_BIRTH_READER_DATABASE_URL`、`HOUFENG_RECORD_BIRTH_READER_IDENTITY_FILE`、`HOUFENG_RECORD_BIRTH_READER_CREDENTIAL_FILE`，URL 不含密码，凭据来自对应受限文件，不能复用 source reader 或 birth producer 主体。管理侧独立连接保存在恢复单元外的 sticky maintenance 配置中，不把该私有配置或 Docker socket 挂给 runtime。完整启停/恢复 ceremony 及 later-generation 交接仍是独立未完成门槛。

4. **Validation & Error Matrix**

   | Condition | Expected behavior |
   | --- | --- |
   | shared-network selected + blank `HOUFENG_PROXY_NETWORK` | `docker compose config` fails with the overlay's required-variable diagnostic; no fallback network or port is added |
   | shared-network selected + named external network absent | `docker compose up` fails with external-network-not-found; Houfeng never creates or substitutes an NPM network |
   | shared-network rendered normally | Center has default + `houfeng-proxy`, alias `houfeng`, and no published port; all other services remain off the proxy network |
   | host-proxy selected + blank/unset `HOUFENG_PROXY_NETWORK` | `docker compose config` succeeds; rendered topology has no external proxy network and exactly one TCP port entry with target 16001, published 16001, and host IP 127.0.0.1 |
   | host-proxy selected + Docker Engine older than 28.0.0 | unsupported; operator stops before `up`, with no documented firewall bypass |
   | host port 16001 already occupied | container creation fails with bind error; do not make the port or bind address configurable |
   | both overlay files loaded | unsupported operator edit; validation/docs require exactly one mode, never a merged dual exposure |
   | `HOUFENG_PROXY_NETWORK=host`, placeholder network, `0.0.0.0`, LAN/public IP, `::`, or short/all-interface port | reject static review and deployment configuration |
   | mode file duplicates the full service graph or docs require NPM reconfiguration solely for Houfeng | reject review; restore one common graph and the responsibility direction Houfeng → existing NPM |
   | required non-mode env blank or public URL non-HTTPS | Compose/preflight fails visibly before application startup |
   | storage/secret/db/namespace init fails | dependent Center/processor stay unstarted; safe stage name is visible in init logs |
   | authority state missing/corrupt/mismatched for active DB | db-init/authority fails closed; never regenerate over active state |
   | authority heartbeat stale/unhealthy | Center dependency/readiness or Records writes fail closed |
   | ordinary ACTIVE, guard/disposition drift, unknown predecessor or changed physical namespace | protected work remains closed; retain state and resolve the same operation, never reset or silently rebind |
   | Center/processor receives privileged/authority secret | reject static review and service config |
   | processor becomes root/writable/capable or loses bounds | reject static review and runtime inspection |
   | resolved-version concurrent release/dispatch runs | only one deployment-assets job runs for that version; the running job is not canceled, while an older queued pending run may be replaced by a newer pending run; no two running jobs mutate the same version concurrently |
   | either pre-upload mode render fails or rendered image differs from release tag | deployment-assets job fails before manifest inspection and upload |
   | required asset already exists once with identical bytes | retain it and upload only missing names; repeat is idempotent |
   | required asset exists with different bytes, is duplicated, or upload outcome is ambiguous | fail closed without clobber/delete/retry mutation |
   | public exact name absent/duplicated, legacy normalized name present, download absent, or bytes differ | deployment-assets job fails after upload and before success summary |
   | unrelated agent assets are present on the same Release | preserve and ignore them for deployment cardinality; do not assert total asset count equals four |
   | operator changes only a secret then runs plain `up -d` | not a supported rotation proof; require explicit staging/init/recreation sequence |
   | operator restores only DB, attachments, or authority state | incomplete recovery point; restore the coordinated directory copy |

5. **Good / Base / Bad Cases**
   - Good: bridge-mode NPM operator downloads all four assets, keeps the default `COMPOSE_FILE`, sets the exact existing network discovered read-only from NPM, renders Center on default + external network with no host port, and configures NPM upstream `houfeng:16001` without editing NPM Compose.
   - Good: host-mode NPM operator confirms Engine 28.0.0 or newer, selects the host overlay, leaves `HOUFENG_PROXY_NETWORK` blank, verifies the structured fixed IPv4-loopback TCP port fields and their effective behavior, and configures NPM upstream `127.0.0.1:16001` without moving NPM to bridge mode.
   - Good: real Records smoke authenticates, uploads content, observes quarantine → ClamAV/processor → available, then publishes through the production admission gate and downloads the same content.
   - Base: exact deployment rerun validates the same signed authority state/contract and leaves stable identity/epoch/fence unchanged except bounded membership expiry renewal and approved password rotation; exact release-job retry retains byte-identical assets and uploads only missing ones.
   - Bad: `HOUFENG_PROXY_NETWORK=host`, invented `npm-network`, both overlays, `16001:16001`, `0.0.0.0:16001:16001`, `[::]:16001:16001`, or a configurable host-port variable widens or confuses the approved proxy surface.
   - Bad: duplicating the services into two mode bundles invites initialization/secret/authority/recovery drift; forcing NPM to create/join a Houfeng-owned network or change from host to bridge reverses ownership.
   - Bad: `build: .`, a wrapper script, manual SQL, a named business-data volume, `latest` in the release env asset, or an agent/Caddy service changes the approved operator contract.
   - Bad: copying PostgreSQL without `./data/records-authority`, editing the signed ledger, or exposing the authority database secret to Center bypasses the coordinated trust boundary.

6. **Tests Required**
   - `internal/center/deploy/production_compose_static_test.go` freezes the common graph, namespace gate and scoped mounts, Center on default with no proxy surface, both exact overlay roles, one active default `COMPOSE_FILE`, four ordered assets, and the prohibitions on unsupported proxy configurations.
   - Focused docs/env tests assert both upstreams, Engine 28.0.0+, exact network discovery, default/shared selection, explicit host selection, blank host network value, upgrade/recovery file set, and public HTTPS health validation. Keep all existing secret rotation, authority, and portable recovery assertions.
   - Focused workflow tests assert resolved-version serialization without running-job cancellation, allow pending-run replacement, and also cover ordered staging, both mode renders before upload, one exact image before manifest inspection, the shared required asset array, non-destructive idempotent pre-upload reconciliation, exact-name public cardinality, fresh download/existence/byte comparison ordering, legacy-name rejection, unrelated-asset preservation, and upload → readback → summary ordering.
   - Run `go test ./internal/center/deploy ./cmd/houfeng-record-platform-admin`; it covers topology, env grouping, secret scope/staging, HTTPS preflight, release URLs/assets, safe stage diagnostics, and authority wiring.
   - Render shared-network explicitly with `docker compose --env-file docs/deploy/compose.env.example -f compose.yaml -f compose.proxy-network.yaml config` and a complete task-owned env including a real test network name; assert Center default + external networks and no published port. Render host-proxy with `env -u HOUFENG_PROXY_NETWORK docker compose --env-file docs/deploy/compose.env.example -f compose.yaml -f compose.proxy-host.yaml config`; assert no external proxy network and exactly one structured port entry with `host_ip: 127.0.0.1`, target 16001, published `"16001"`, and TCP protocol.
   - Both rendered modes must give all services the bounded logging options and omit Center's file-log environment/mount. A disposable container using the rendered settings must emit over 30 MiB, demonstrate actual local-driver rotation, and retain readable recent logs.
   - Required-variable negative renders must fail independently; current installed Compose syntax must accept environment-backed secrets, external networks, and long-syntax ports. Workflow YAML parsing, changed multiline shell `bash -n`, `actionlint` when available, `make verify-go`, and `git diff --check` are mandatory local gates.
   - Strict PostgreSQL 16 tests must cover fresh init, exact repeat, role drift, password rotation, current convergence/admission, activation, heartbeat expiry/renewal, and privilege denials with zero skips.
   - Real isolated Compose evidence must inspect the target image, prove fresh and exact-repeat V2 membership and namespace admission, worker effects and attachment/ClamAV flow, and supported same-physical cold transition and failure recovery. A portable copy does not establish new-boot or new-physical namespace admission; no such acceptance is claimed.
   - Inspect the image for all three binaries, baked Web, Poppler/curl, default UID/GID 10001, and entrypoint behavior. Post-release evidence downloads all four exact public assets, byte-compares them with tagged sources, renders both downloaded modes with the matching image, and leaves unrelated Release assets untouched.
   - Static docs tests must scope Docker quick-start prohibitions to the Compose section so advanced direct/systemd provisioning remains correct.

7. **Wrong vs Correct**

```yaml
# Wrong: the common graph requires one proxy topology and publishes broadly.
services:
  houfeng:
    ports: ["16001:16001"]
    networks: [default, houfeng-proxy]
networks:
  houfeng-proxy:
    external: true
    name: "${HOUFENG_PROXY_NETWORK:?required}"
```

```yaml
# Correct compose.yaml: one immutable common graph with no proxy exposure.
services:
  houfeng:
    image: "${HOUFENG_IMAGE:?set HOUFENG_IMAGE in .env}"
    networks:
      default:
```

```yaml
# Correct compose.proxy-network.yaml: Houfeng joins NPM's existing network.
services:
  houfeng:
    networks:
      houfeng-proxy:
        aliases:
          - houfeng
networks:
  houfeng-proxy:
    external: true
    name: "${HOUFENG_PROXY_NETWORK:?set HOUFENG_PROXY_NETWORK for shared-network mode}"
```

```yaml
# Correct compose.proxy-host.yaml: existing host-mode NPM reaches loopback only.
services:
  houfeng:
    ports:
      - name: npm-host-proxy
        target: 16001
        published: "16001"
        host_ip: 127.0.0.1
        protocol: tcp
```

```text
Wrong: set HOUFENG_PROXY_NETWORK=host, load both overlays, or reconfigure NPM solely for Houfeng.
Correct: select one COMPOSE_FILE pair; shared mode joins NPM's existing network, host mode requires Engine 28+ and fixed loopback.

Wrong: restore data/postgres alone or edit the signed authority ledger.
Correct: stop writes and copy compose.yaml + both mode assets + .env + optional-secrets + the complete data tree; restore PostgreSQL and authority state together.
```
