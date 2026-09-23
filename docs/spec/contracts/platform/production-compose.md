# 发行部署资产与 Compose 合同

## Scenario: release-asset production Compose and image contract

1. **Scope / Trigger**
   - 触发：修改 `Dockerfile`、`compose.yaml`、`compose.proxy-network.yaml`、`compose.proxy-host.yaml`、`docs/deploy/compose.env.example`、README / deployment docs、Center/processor/ClamAV/PostgreSQL/Records authority 边界，或 `.github/workflows/publish-images.yml`。
   - 目标：同一 GitHub Release 按 `compose.yaml` → `compose.proxy-network.yaml` → `compose.proxy-host.yaml` → `compose.env.example` 的固定顺序发布四个 deployment assets。operator 下载全部四个文件、把 env template 保存为 `.env`，再通过 `COMPOSE_FILE` 选择恰好一个 thin proxy overlay；普通 `docker compose config/pull/up` 命令不变，不 checkout source、不 build、不运行 SQL/helper launcher。
   - 代理责任方向：Houfeng 适配 operator 已有的 NPM，而不是为了 Houfeng 创建 placeholder network、复制整套 Compose，或强迫 operator 改造 NPM。Agent 仍是 monitored host 上的 Linux/systemd workload，不进入 Center Compose。

2. **Signatures**
   - Root `Dockerfile` produces one published image containing `houfeng-center`, `houfeng-content-processor`, `houfeng-record-platform-admin`, baked `web/dist`, curl, and Poppler. Normal runtime stays `USER houfeng:houfeng` (UID/GID 10001); only explicit one-shot/authority services override identity.
   - Common `compose.yaml` is the only full graph and defines exactly eight services: `houfeng-storage-init`, `houfeng-secrets-init`, `db`, `houfeng-db-init`, `houfeng-record-authority`, `clamav`, `houfeng`, and `houfeng-content-processor`. It has no `build:`, Caddy, agent service, `HOUFENG_PROXY_NETWORK`, external proxy network, or Center `ports`; Center explicitly keeps `networks: {default: ...}`.
   - `compose.proxy-network.yaml` is a thin overlay that only adds Center to `houfeng-proxy` with alias `houfeng` and defines `name: "${HOUFENG_PROXY_NETWORK:?set HOUFENG_PROXY_NETWORK for shared-network mode}"` plus `external: true`. It must not duplicate the eight-service graph or publish a port.
   - `compose.proxy-host.yaml` is a thin overlay that only adds one long-syntax TCP mapping with structured fields `target: 16001`, `published: "16001"`, `host_ip: 127.0.0.1`, and `protocol: tcp`. The effective behavior is `127.0.0.1:16001 -> 16001/tcp`; static and rendered-config checks must inspect the structured fields instead of treating a renderer-specific short form as the exact serialized Compose config. The overlay contains no external network, alias, or `HOUFENG_PROXY_NETWORK` reference.
   - The tracked env template has exactly `Must change`, `Recommended`, and `Optional` operator sections and exactly one active default selector: `COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml`. Existing `network_mode: host` NPM operators switch it to `COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml` and leave `HOUFENG_PROXY_NETWORK` blank.
   - The ordered public deployment asset signature is exactly `required_deployment_assets=(compose.yaml compose.proxy-network.yaml compose.proxy-host.yaml compose.env.example)`. The release job writes the matching immutable `docker.io/linnea7171/houfeng:vX.Y.Z` into `HOUFENG_IMAGE`; installation does not ask the user to edit it.
   - The deployment-assets job uses `concurrency.group: deployment-assets-${{ needs.resolve.outputs.version }}` with `cancel-in-progress: false`. This serializes deployment-asset mutation for one resolved version: `cancel-in-progress: false` does not cancel the running job, while queued pending runs may be replaced by a newer run. The group therefore prevents simultaneous mutation by running jobs, but does not promise FIFO ordering or change cancellation outside this job group.
   - GitHub release asset URLs use `https://github.com/xiangnan0811/houfeng/releases/...`; the Docker registry identity remains the separately configured published image path.

3. **Contracts**
   - Mode selection is common base + exactly one selected overlay. Shared-network is the supported default; loading both overlays is unsupported because it combines two distinct proxy reachability surfaces. Compose profiles are not a substitute because interpolation happens per file and profiles cannot make the top-level required external network conditional.
   - In shared-network mode, Houfeng joins the existing NPM user-defined network; NPM remains unchanged and uses upstream `houfeng:16001`. The base and shared-network mode publish no host port, only Center joins the external network, and a blank network value fails interpolation before startup. Do not set `HOUFENG_PROXY_NETWORK=host` or invent a placeholder network.
   - In host-proxy mode, an existing `network_mode: host` NPM remains unchanged and uses upstream `127.0.0.1:16001`. Center stays on the Houfeng private default network for PostgreSQL and ClamAV and gains exactly one fixed IPv4-loopback mapping. Support requires Docker Engine `28.0.0+`; no configurable bind address, all-interface/LAN/IPv6 mapping, old-Engine firewall workaround, or public direct-Center mode is part of the contract.
   - `docker compose up -d` owns ordinary initialization. Storage init creates UID/GID-safe paths; secret init waits for storage and stages only bootstrap/runtime/migrator/platform-admin files; db-init waits for storage, secret staging, and healthy PostgreSQL before it provisions roles, calls `ConvergeAppACLCurrent`, verifies/activates the signed authority bundle through the existing projector, and runs current runtime admission. Records authority waits for db-init and healthy PostgreSQL. Center waits for healthy authority, PostgreSQL, and ClamAV after storage/db-init complete; the processor waits for db-init and ClamAV after storage/secret staging and healthy PostgreSQL.
   - Environment-backed Compose secrets remain scoped: DB gets bootstrap only; Center gets runtime + initial-admin + session; processor gets runtime only; db-init gets staged provisioning secrets; authority reads only its generated private state/database secret. The secret stager bind-mounts only `./data/secrets`, never the whole data tree or authority private bundle. Optional comparison keys are mounted only into Center; only the S3 credential directory is shared with the processor. Host optional-secret directories/files are owned by the image's UID/GID `10001` with directory mode `0700` and file mode `0400`, so non-root services can read their scoped bind mounts without broadening host access. Center/processor never receive bootstrap, migrator, platform-admin, or authority credentials.
   - Production pins `HOUFENG_RECORDS_ENABLED=true` and permanent delete false. Center receives a file-based deployment ID plus fixed `compose-center` / `api` / `records.runtime`; no allow gate or operator-provided authority identity is permitted.
   - Durable local state is a visible portable tree: `./data/postgres`, `./data/attachments`, `./data/logs`, `./data/clamav`, `./data/records-authority`, `./data/center-config`, and `./data/secrets`. PostgreSQL, local attachments, and Records authority state are one coordinated restore unit: backup and restore must stop writes and capture/restore all three from the same recovery point. Restoring any subset is incomplete, and active DB plus absent/corrupt/mismatched authority state fails closed.
   - Processor stays non-root, read-only, `cap_drop: ALL`, `no-new-privileges`, core=0, with bounded `noexec,nosuid,nodev` tmpfs. Center and processor share only the runtime DB role, Blob contract, attachment bind, and scanner settings.
   - `.env` passwords use independently generated, pairwise-distinct hex values (`openssl rand -hex 32`) to avoid dotenv interpolation traps and cross-role credential reuse; db-init rejects duplicate role secrets before mutation. Password-only edits do not guarantee secret restaging or service recreation. Controlled rotation must run these commands in order: `docker compose stop houfeng houfeng-content-processor`; `docker compose run --rm houfeng-secrets-init`; `docker compose run --rm houfeng-db-init`; `docker compose up -d --force-recreate houfeng houfeng-content-processor`.
   - `.github/workflows/publish-images.yml` may publish only on `release.published` or explicit maintainer dispatch. It checks out the resolved release source, builds the image, stages all four files under their public names, pins the release image in `compose.env.example`, then renders both modes before upload with explicit `-f` pairs. The shared render supplies a nonblank network; the host render unsets `HOUFENG_PROXY_NETWORK`; their combined project-image set must contain the one exact release image before manifest inspection.
   - Upload is a non-destructive fail-closed idempotent upload. For every exact required name, an absent asset may be uploaded, one byte-identical existing asset is retained, while duplicate names or differing existing bytes stop the job. Never use clobber, delete, replay after ambiguous mutation, or overwrite as recovery behavior.
   - Post-upload public readback uses the same ordered array, queries all release asset names, requires exact-name cardinality of one for each deployment asset, downloads each exact name to a fresh trap-cleaned directory, and proves byte identity with the staged file before success summary. It rejects legacy `.env.example` / `default.env.example` while preserving unrelated agent assets; total Release asset count is not fixed at four.
   - Direct/systemd documentation may retain explicit pre-R1 provisioning as an advanced path, but the Docker quick-start section must contain no manual SQL, local toolchain, source build, or helper launcher.

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
   | storage/secret/db init fails | dependent Center/processor stay unstarted; safe stage name is visible in init logs |
   | authority state missing/corrupt/mismatched for active DB | db-init/authority fails closed; never regenerate over active state |
   | authority heartbeat stale/unhealthy | Center dependency/readiness or Records writes fail closed |
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
   - Bad: duplicating all eight services into two mode bundles invites initialization/secret/authority/recovery drift; forcing NPM to create/join a Houfeng-owned network or change from host to bridge reverses ownership.
   - Bad: `build: .`, a wrapper script, manual SQL, a named business-data volume, `latest` in the release env asset, or an agent/Caddy service changes the approved operator contract.
   - Bad: copying PostgreSQL without `./data/records-authority`, editing the signed ledger, or exposing the authority database secret to Center bypasses the coordinated trust boundary.

6. **Tests Required**
   - `internal/center/deploy/production_compose_static_test.go` freezes: the base as the only eight-service graph with Center on default and no proxy surface; the shared overlay's exact alias/required external network/no-port role; the host overlay's exact long-syntax loopback/no-network role; one active default `COMPOSE_FILE`; four ordered assets; and the prohibitions on both overlays, placeholder/host network values, broad ports, duplicated full Compose, and NPM responsibility reversal.
   - Focused docs/env tests assert both upstreams, Engine 28.0.0+, exact network discovery, default/shared selection, explicit host selection, blank host network value, upgrade/recovery file set, and public HTTPS health validation. Keep all existing secret rotation, authority, and portable recovery assertions.
   - Focused workflow tests assert resolved-version serialization without running-job cancellation, allow pending-run replacement, and also cover ordered staging, both mode renders before upload, one exact image before manifest inspection, the shared required asset array, non-destructive idempotent pre-upload reconciliation, exact-name public cardinality, fresh download/existence/byte comparison ordering, legacy-name rejection, unrelated-asset preservation, and upload → readback → summary ordering.
   - Run `go test ./internal/center/deploy ./cmd/houfeng-record-platform-admin`; it covers topology, env grouping, secret scope/staging, HTTPS preflight, release URLs/assets, safe stage diagnostics, and authority wiring.
   - Render shared-network explicitly with `docker compose --env-file docs/deploy/compose.env.example -f compose.yaml -f compose.proxy-network.yaml config` and a complete task-owned env including a real test network name; assert Center default + external networks and no published port. Render host-proxy with `env -u HOUFENG_PROXY_NETWORK docker compose --env-file docs/deploy/compose.env.example -f compose.yaml -f compose.proxy-host.yaml config`; assert no external proxy network and exactly one structured port entry with `host_ip: 127.0.0.1`, target 16001, published `"16001"`, and TCP protocol.
   - Required-variable negative renders must fail independently; current installed Compose syntax must accept environment-backed secrets, external networks, and long-syntax ports. Workflow YAML parsing, changed multiline shell `bash -n`, `actionlint` when available, `make verify-go`, and `git diff --check` are mandatory local gates.
   - Strict PostgreSQL 16 tests must cover fresh init, exact repeat, role drift, password rotation, current convergence/admission, activation, heartbeat expiry/renewal, and privilege denials with zero skips.
   - Real isolated Compose evidence must build/inspect the release image, start unique task-owned resources, prove admitted Records + attachment/ClamAV flow, restart/exact repeat, corrupt-state fail-closed, portable-copy behavior, and clean teardown without touching unrelated Docker state.
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
