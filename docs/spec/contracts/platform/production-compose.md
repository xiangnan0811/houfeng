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
   - 未启用 source authority 时，`docker compose up -d` 拥有普通初始化。Storage init 创建 UID/GID-safe 路径；secret init 等待 storage 并仅 stage bootstrap/runtime/migrator/platform-admin/deletion-worker 文件；db-init 等待 storage、secret staging 和健康 PostgreSQL，provision roles，以全部三个 application bindings 调用 `ConvergeAppACLCurrent`，经现有 projector 验证/激活 signed authority bundle 并执行 current runtime admission。Records authority 等待 db-init 和健康 PostgreSQL；Center 等待健康 authority、PostgreSQL 和 ClamAV 及完成的 storage/db-init；processor 等待 db-init、ClamAV、storage/secret staging 和健康 PostgreSQL。Source-enrolled 部署还必须满足 [Compose authority](compose-authority.md) 的条件式 guard、enrollment 和 credential 检查，不得省略配置退回普通路径。
   - Environment-backed Compose secrets remain scoped: DB gets bootstrap only; Center gets runtime + initial-admin + session; processor gets runtime only; db-init gets staged provisioning secrets; authority reads only its generated private state/database secret. The secret stager bind-mounts only `./data/secrets`, never the whole data tree or authority private bundle. Optional comparison keys are mounted only into Center; only the S3 credential directory is shared with the processor. Host optional-secret directories/files are owned by the image's UID/GID `10001` with directory mode `0700` and file mode `0400`, so non-root services can read their scoped bind mounts without broadening host access. Center/processor never receive bootstrap, migrator, platform-admin, deletion-worker, or authority credentials.
   - Production pins `HOUFENG_RECORDS_ENABLED=true` and permanent delete false. Center receives a file-based deployment ID plus fixed `compose-center` / `api` / `records.runtime`; no allow gate or operator-provided authority identity is permitted.
   - C81 requires the independent `HOUFENG_RECORD_DELETION_WORKER_PASSWORD` secret for the manifest-bound `houfeng_record_deletion_worker` login. Its staged file is db-init-only; the eight-service graph does not add or automatically start a deletion worker. Neither provisioned credentials nor successful current admission authorize permanent-deletion HTTP.
   - 当前未交付 C81 local Blob 候选另要求已准入 namespace：单独创建 `data/attachments` 或完成 db-init 不足以启动业务。一次性 participant 以 Blob owner UID10001、独立 direct platform-admin 输入运行，凭据不留在业务 volume/runtime；八服务图尚未自动执行此准入，不得宣称 fresh-install 或 portable-copy 验收通过。UID10002 source outer guard、旧 writer fence 与 APP/W2 最终 handoff 保持独立门禁；source-enabled adoption 当前明确拒绝。见 [部署文档](../../../deploy/local-and-systemd.md#local-blob-namespace-admission)。
   - C81 启动绑定候选还要求 db-init 以 UID10002、补充 GID10001 发布两份 0440 公开文件，Center/processor 通过只读 `/run/houfeng/records-authority` mount 读取各自文件；authority 每次更新 membership 前核实际 image 中两份 executable digest。processor 是固定 Records participant，不借 Center 的 legacy flag 绕过 launch 检查。受用户维护的实际 Compose 文件尚未自动应用以下配置建议；现有八服务资产不能据此标记为已完成新候选的实际部署验收。
   - Durable local state 是可见 portable tree：`./data/postgres`、`./data/attachments`、`./data/clamav`、`./data/records-authority`、`./data/center-config`、`./data/secrets`。历史 `./data/logs` 保留在已有备份中但不再是 active bind mount。PostgreSQL、local attachments 和 Records authority state 是一个协调恢复单元：停止写入并从同一恢复点捕获/恢复全部三项；任何子集均不完整，active DB 加 absent/corrupt/mismatched authority state fail closed。Source-enrolled 时，独立 W2 与 maintenance 配置/guard 位于该单元外且不能一起回滚；重新准入还需 source reconciliation、匹配的当前 generation、completed handoff 和 activated enrollment，W2 active 单独不足以证明恢复完成。当前没有完整可运行的 source recovery coordinator/CLI。
   - All eight services use one shared logging anchor: Docker `local`, `max-size: "10m"`, `max-file: "3"`, `compress: "true"`. Center uses stdout only, with no `HOUFENG_LOG_FILE`, log bind mount, or storage-init log directory. Health evidence uses Compose logs and actual rotation, not `center.log` existence. Do not modify Docker daemon, NPM, or shared journald configuration.
   - Processor stays non-root, read-only, `cap_drop: ALL`, `no-new-privileges`, core=0, with bounded `noexec,nosuid,nodev` tmpfs. Center and processor share only the runtime DB role, Blob contract, attachment bind, and scanner settings.
   - 未启用 source authority 时，`.env` 密码使用独立且互异的 hex 值（`openssl rand -hex 32`），避免 dotenv 插值和跨角色复用；db-init 在 mutation 前拒绝重复 secret。只编辑密码不保证 restaging 或服务重建。普通受控轮换按顺序运行：`docker compose stop houfeng houfeng-content-processor`；`docker compose run --rm houfeng-secrets-init`；`docker compose run --rm houfeng-db-init`；`docker compose up -d --force-recreate houfeng houfeng-content-processor`。Source-enrolled 时 init、stager 和 heartbeat 使用同 kernel 同 flock domain 的共享 maintenance guard，并核验当前 credential commitments；旧 `.env`/staged secret 不可回写修复 mismatch，需受控 credential transition，不能把上述普通轮换序列当作绕过手段。
   - 首次 source enrollment 必须先由操作者实际停止、隔离、等待旧的无 guard writer/初始化/heartbeat callback 及所有旧 endpoint 退出并阻止重启；新发布的 guard 不能追溯排空它们。`enroll-offline` 依赖此前外部 fence，不是安全在线切换；helper/guard 接线不代表已提供完整恢复编排或实际部署已升级。
   - `.github/workflows/publish-images.yml` may publish only on `release.published` or explicit maintainer dispatch. It checks out the resolved release source, builds the image, stages all four files under their public names, pins the release image in `compose.env.example`, then renders both modes before upload with explicit `-f` pairs. The shared render supplies a nonblank network; the host render unsets `HOUFENG_PROXY_NETWORK`; their combined project-image set must contain the one exact release image before manifest inspection.
   - Upload is a non-destructive fail-closed idempotent upload. For every exact required name, an absent asset may be uploaded, one byte-identical existing asset is retained, while duplicate names or differing existing bytes stop the job. Never use clobber, delete, replay after ambiguous mutation, or overwrite as recovery behavior.
   - Post-upload public readback uses the same ordered array, queries all release asset names, requires exact-name cardinality of one for each deployment asset, downloads each exact name to a fresh trap-cleaned directory, and proves byte identity with the staged file before success summary. It rejects legacy `.env.example` / `default.env.example` while preserving unrelated agent assets; total Release asset count is not fixed at four.
   - Direct/systemd documentation may retain explicit pre-R1 provisioning as an advanced path, but the Docker quick-start section must contain no manual SQL, local toolchain, source build, or helper launcher.

以下是需要审核并在隔离 fixture 验证的 Compose 合并片段；保留现有 service 配置和其他 volumes。本候选没有替操作者修改持久部署配置。

```yaml
services:
  houfeng-db-init:
    group_add:
      - "10001"
  houfeng-content-processor:
    volumes:
      - ./data/center-config:/run/houfeng/records-authority:ro
```

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
   - Both rendered modes must give all eight services the bounded logging options and omit Center's file-log environment/mount. A disposable container using the rendered settings must emit over 30 MiB, demonstrate actual local-driver rotation, and retain readable recent logs.
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
