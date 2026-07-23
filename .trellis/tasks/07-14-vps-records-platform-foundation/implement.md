# 统一授权与平台基础 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`. Codex inline execution only; do not dispatch implement/check sub-agents. Follow RED → verify RED → minimal GREEN → verify GREEN.

**Goal:** 建立全部记录能力共享的服务端授权、幂等/outbox、删除账本、恢复信任、lease 和 fail-closed 运行时基础。

**Architecture:** 应用数据库保存可恢复投影与 reservation，独立 PostgreSQL 保存 append-only primary ledger，独立 Postgres/S3 WORM 保存 full witness，recovery-control 保存 trust/inventory/workspace。现有 center 显式装配这些边界，但 feature/capability 默认关闭。

**Tech Stack:** Go 1.26.2、pgx/v5、PostgreSQL、stdlib crypto/rand/ed25519/SHA-256/AES-GCM、MinIO SDK v7.2.1、Docker Compose/systemd。

---

## Preflight

- [ ] 确认父任务设计仍为批准版本，直接依赖为空，当前最大应用 migration 仍可使用 0051；若已被主线占用，先整体顺延本任务 migration 引用。
- [ ] 从最新受保护主线创建非 main 分支并运行 `sh scripts/setup-git-hooks.sh`。
- [ ] 读取 `.trellis/spec/backend/{directory-structure,database-guidelines,error-handling,logging-guidelines,quality-guidelines}.md` 和跨层/分支指南。
- [ ] 记录当前 baseline：`make verify-go`，以及 `cmd/houfeng-center/bootstrap_test.go` 的现有 worker 数 5。
- [ ] 确认 Docker 支持 `--network=host`，本机可运行 `postgres:16-alpine`、`minio/minio:RELEASE.2025-04-22T22-12-26Z`、`minio/mc:RELEASE.2025-04-16T18-13-26Z` 与 `linnea7171/houfeng:v0.59.0`；测试脚本必须自行创建合成凭据、临时目录和空闲端口，并通过 `trap` 清理。计划和测试命令不依赖调用者预先设置的 `TEST_POSTGRES_URL`。

## Task 1: 应用与独立数据库迁移

**Files:**

- Create: `db/migrations/0051_create_record_platform_foundation.sql`
- Create: `db/deletionledger/migrations/embed.go`
- Create: `db/deletionledger/migrations/0001_create_deletion_ledger.sql`
- Create: `db/deletionwitness/migrations/embed.go`
- Create: `db/deletionwitness/migrations/0001_create_full_witness.sql`
- Create: `db/recoverycontrol/migrations/embed.go`
- Create: `db/recoverycontrol/migrations/0001_create_recovery_control.sql`
- Create: `internal/center/platformmigrate/runner.go`
- Create: `internal/center/platformmigrate/runner_test.go`
- Create: `internal/center/deletionledger/migrate/migrate.go`
- Create: `internal/center/deletionledger/migrate/migrate_test.go`
- Create: `internal/center/recovery/migrate/migrate.go`
- Create: `internal/center/recovery/migrate/migrate_test.go`
- Modify: `internal/center/store/migrate/migrate_test.go`
- Modify: `internal/center/store/migrate/postgres_integration_test.go`
- Create: `internal/center/platformmigrate/roles.go`
- Create: `internal/center/platformmigrate/roles_test.go`
- Create: `internal/center/platformmigrate/domain_identity.go`
- Create: `internal/center/platformmigrate/domain_identity_test.go`

- [ ] 写 migration source tests，逐表断言 parent design 中的唯一键、CHECK、TTL 字段、无内容列、禁止 cascade；APP 0051、deletion-ledger 0001、deletion-witness 0001 与 recovery-control 0001 必须各自在本地创建 immutable `public.record_platform_domain_identity` 和 append-only `public.record_platform_domain_attestations`，S3 profile 还要求 recovery-control 的 immutable S3 witness identity/attestation。运行 `go test ./internal/center/store/migrate -run RecordPlatform -count=1`，确认 RED。
- [ ] 写 0051 幂等应用迁移；任何未来 record/object identity 均无 FK，不提前创建 records 表。复跑 focused test GREEN。
- [ ] 为三个独立 migration FS 写 shared runner contract tests：fresh、repeat apply、未知/篡改已应用 migration 失败。先因 runner 不存在 RED。
- [ ] 新建 `internal/center/deletionledger/migrate/`、`internal/center/recovery/migrate/` 的窄 runner；独立 DSN/ledger，不调用应用 `migrate.Apply`；复跑 GREEN。
- [ ] 在四个独立 PostgreSQL 16 container/data-dir integration 中验证 system identifier pairwise distinct、alias/same-cluster/expired/wrong-kind/rollback attestation 拒绝、ledger/witness trigger/role 拒绝 UPDATE/DELETE/TRUNCATE，并验证 app DB 备份不含独立 schema；S3 profile 另以 read-control-only principal 验证 stable bucket identity、应用备份位置不重叠和无 witness DB 读取。

## Task 2: Actor scope 与统一 recordauth.Policy

**Files:**

- Create: `internal/center/recordauth/types.go`
- Create: `internal/center/recordauth/policy.go`
- Create: `internal/center/recordauth/policy_test.go`
- Create: `internal/center/store/record_auth.go`
- Create: `internal/center/store/record_auth_test.go`
- Modify: `internal/center/http/sessionctx/sessionctx.go`
- Modify: `internal/center/http/middleware.go`
- Modify: `internal/center/http/middleware_test.go`

- [ ] 先写表驱动 RED：admin、viewer/group allow、来源交集 deny、未知 capability/source kind/scope version deny、任意 visibility string 拒绝、tombstoned source 缺失/篡改 floor deny、跨 project deny 与不存在/无权 404 外部语义。
- [ ] 定义上文 typed `ActorScope/Capability/VisibilityScope/SourceAuthorization/ResourceScope/Policy`，current middleware 以 `ProjectIDDefault` + admin role 填 actor；不把客户端 header、visibility、role/group 或 source floor 当 scope。
- [ ] store 查询 access groups/members 时只返回 stable IDs，不读取显示正文；adapter 按 registry 校验 source kind，排序去重 role/group，重算 canonical digest；policy 计算 `actor ∩ visibility ∩ capture scope ∩ live source scope/final witnessed floor`。
- [ ] 复跑 `go test ./internal/center/recordauth ./internal/center/http -run 'RecordAuth|SessionScope' -count=1` GREEN。

## Task 3: 幂等、outbox、identity guard 与 lease primitive

**Files:**

- Create: `internal/center/recordplatform/types.go`
- Create: `internal/center/recordplatform/idempotency.go`
- Create: `internal/center/recordplatform/outbox.go`
- Create: `internal/center/recordplatform/guards.go`
- Create: `internal/center/recordplatform/leases.go`
- Create: `internal/center/recordplatform/worker.go`
- Create: `internal/center/recordplatform/*_test.go`
- Create: `internal/center/store/record_platform.go`
- Create: `internal/center/store/record_platform_test.go`

- [ ] 写 RED tests：同 key/同 fingerprint replay、同 key/不同 fingerprint conflict、outbox/idempotency/guard 的 owner ID + 单调 generation + live-expiry 接管、全局 lock order、旧/过期 owner final commit 拒绝、idempotency expiry 晚于 owner lease、cleanup 不删除 live owner、outbox fresh authorization。
- [ ] 实现服务端 CSPRNG `DeletionRequestTokenV1`、canonical transport、domain-separated token commitment 与请求 fingerprint 的 length-prefix 编码；raw token、正文与 stable object ID 不进入持久化或普通日志，低熵/错格式/跨 scope 输入在写前拒绝。
- [ ] 实现 tx seam，使后续 records service 能在同一 pgx transaction 写业务事实/outbox/idempotency；网络调用不暴露在接口内。
- [ ] 实现 object/client/content/serving lease API 与 fake clock；续租失败在 expiry 前取消。
- [ ] `go test -race ./internal/center/recordplatform ./internal/center/store -run 'RecordPlatform|Idempotency|Lease' -count=10` GREEN。

## Task 4: primary ledger、full witness 与 checkpoint

**Files:**

- Create: `internal/center/deletionledger/types.go`
- Create: `internal/center/deletionledger/canonical.go`
- Create: `internal/center/deletionledger/service.go`
- Create: `internal/center/deletionledger/postgres.go`
- Create: `internal/center/deletionledger/witness_postgres.go`
- Create: `internal/center/deletionledger/witness_s3.go`
- Create: `internal/center/deletionledger/reconciler.go`
- Create: `internal/center/deletionledger/checkpoint.go`
- Create: `internal/center/deletionledger/*_test.go`

- [ ] 用 golden bytes 写 canonical encoding/hash-chain 与 deletion-token RED tests；覆盖 `drt1_` 32-byte CSPRNG grammar、domain-separated commitment preimage、request fingerprint、低熵/非 canonical/跨 scope replay 与 raw-token 零持久化，map 顺序变化不能改变 bytes，未知 field/version 必须拒绝。
- [ ] 实现 append-only primary + `postgres_sync` full witness；每个 primary commit/witness ack cutpoint 的期望状态固定为 committed、pending 或 not-committed，绝不猜测。
- [ ] 加入 `s3_worm` adapter，使用 MinIO v7.2.1 PutObject/retention/legal-hold API；对象 key 只含 namespace/sequence，不含记录名。
- [ ] 实现 reconciler owner generation、`attempt_not_committed` outcome 与连续 checkpoint；同 idempotency key 不重新删除。PostgreSQL 与 S3 的新写和已存在/ack-loss 快路径都必须逐字读回目标、枚举 far tail 并从 genesis 验证完整连续链，不能以 head tuple 相同提前返回。
- [ ] 运行 `go test -race ./internal/center/deletionledger -count=10`，再对真实三数据库执行崩溃点 integration。

## Task 5: RecoveryTrustStore、manifest 与 recovery-control

**Files:**

- Create: `internal/center/recovery/types.go`
- Create: `internal/center/recovery/manifest.go`
- Create: `internal/center/recovery/trust_store.go`
- Create: `internal/center/recovery/inventory.go`
- Create: `internal/center/recovery/workspace.go`
- Create: `internal/center/recovery/replay.go`
- Create: `internal/center/recovery/janitor.go`
- Create: `internal/center/recovery/source_object_adapter.go`
- Create: `internal/center/recovery/source_object_adapter_test.go`
- Create: `internal/center/recordplatform/source_deletion_adapter.go`
- Create: `internal/center/recordplatform/source_deletion_adapter_test.go`
- Create: `internal/center/recovery/*_test.go`
- Create: `internal/center/store/recovery_projection.go`
- Create: `internal/center/store/recovery_projection_test.go`

- [ ] 写 Ed25519 manifest/trust chain RED tests：full witness-only rebuild、retired key 可验、compromised/missing key fail closed、signed expiry/watermark 防篡改。
- [ ] 实现 source-specific `RecoveryPointManifest` envelope 和 `RecoveryTrustStore` revision/hash chain；私钥只从 0400 file 读取，不入 DB/log。
- [ ] 实现 attempt/workspace registry 与 `expires_at = min(last_progress+24h, created+7d, source.recoverable_until)`；续租/forensic 转换不得越界。
- [ ] 实现 replay registry 接口，仅定义 adapter contract；真实 record/blob/import adapter 由后续任务注册。
- [ ] 为现有VPS/MonitoringInstance/Target实现versioned source deletion/recovery adapter：delete commit恢复final authorization floor、保持source absent/tombstone、断开live route且禁止名称重连；它不读取未来records表。后续record/evidence引用由各自adapter处理。
- [ ] 真实 recovery-control + witness integration 验证 rollback/head mismatch/expired workspace janitor。

## Task 6: Config、bootstrap、readiness 与 worker lifecycle

**Files:**

- Modify: `internal/center/config/config.go`
- Modify: `internal/center/config/config_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`
- Modify: `internal/center/app/app.go`
- Modify: `internal/center/app/app_test.go`
- Modify: `internal/center/http/handlers/health.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`

- [ ] 先写 config RED matrix：features off、profile-specific 三/四 DB、inactive-profile 变量不得读取、same/alias domain、system identifier/attestation、TLS、不完整 witness、unbounded backup policy、strict raw recovery key、server-issued deletion-token commitment 不读取任何 keyring、bad key file/permissions、unknown profile。
- [ ] 实现 typed config，feature off 时不要求外部 DSN；permanent-delete on 时全部 hard prerequisite 必填且相互独立。
- [ ] bootstrap 显式构造 pool/migrator/service/worker；更新 worker count assertions，不用 service locator。
- [ ] app 把 startup gate failure 与 retry worker error 分开；records protected domain unavailable 不终止无关 monitoring workers。
- [ ] health/readiness 只返回 capability/version/reason code/last-success bucket，不泄露 DSN/head/object identity。

## Task 7: Compose/systemd 部署与恢复域证明

**Files:**

- Modify: `compose.yaml`
- Modify: `Dockerfile`
- Modify: `.env.example`
- Modify: `docs/deploy/compose.env.example`
- Modify: `docs/deploy/local-and-systemd.md`
- Modify: `docs/deploy/systemd/houfeng-center.service`
- Modify: `scripts/docker-entrypoint.sh`
- Modify: `internal/center/deploy/docker_static_test.go`

- [ ] 写 static RED tests，要求 ledger/witness/recovery-control 独立 service/volume/credential，禁止与 app postgres volume/URL 复用。
- [ ] Compose 增加健康检查、internal network、独立 volumes；默认 `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED=false`。
- [ ] systemd 文档列出外部 DSN、key files、目录 owner/mode 与 preflight；不提供“同一数据库快速模式”。
- [ ] 验证 `docker compose config`、四数据库 health、records off 启动、permanent-delete on/配置缺失 fail closed。

## Task 8: TTL、遥测 allowlist 与完整质量门

**Files:**

- Create: `internal/center/recordplatform/janitor.go`
- Create: `internal/center/recordplatform/janitor_test.go`
- Modify: `internal/center/retention/types.go`
- Modify: `internal/center/retention/worker.go`
- Modify: `internal/center/retention/worker_test.go`
- Create: `internal/center/retention/registry.go`
- Create: `internal/center/retention/registry_test.go`
- Create: `internal/center/retention/attestation.go`
- Create: `internal/center/retention/attestation_test.go`
- Create: `internal/center/retention/retention_acceptance_policy_v1.json`
- Modify: `.github/workflows/ci.yml`
- Create: `internal/center/retention/schema_registry_v1.go`
- Create: `internal/center/retention/policy_registry_v1.go`
- Create: `internal/center/retention/manifest_v1.go`
- Create: `internal/center/retention/manifest_v1_test.go`
- Create: `internal/center/retention/filesystem_inventory.go`
- Create: `internal/center/retention/filesystem_inventory_test.go`
- Create: `internal/center/retention/s3_inventory.go`
- Create: `internal/center/retention/s3_inventory_test.go`
- Create: `internal/center/retention/testdata/retention_allowlist_v1.json`
- Create: `internal/center/retention/testdata/forbidden_corpus_v1.json`
- Create: `internal/center/retention/cmd/generate/main.go`
- Modify: applicable `.trellis/spec/backend/*.md`

- [ ] 用 secret + stable-ID corpus 写 RED test，覆盖 errors、worker stdout/stderr、HTTP、DB、object adapter；普通 sink 命中必须为 0。
- [ ] 实现 24h lease/guard/member cleanup、30d operation/job去关联和长期 allowlist scanner；ledger/witness 不被普通 retention 删除。
- [ ] 运行 external migration suites、`go test -race`、`make verify-go` 与 `git diff --check`。
- [ ] 执行 `trellis-check`，更新 spec 中真实 worker count、独立迁移/授权/日志合同；fresh 重跑全部验证。

## Platform foundation 19 条验收证据 registry

Task 18 创建 `scripts/verify-record-platform-foundation-acceptance.sh`、版本化 registry 与 typed receipt writer。每个 selector 必须可单独执行：

```bash
scripts/verify-record-platform-foundation-acceptance.sh \
  --criterion PF-AC-NNN \
  --output-root artifacts/acceptance
```

PRD parser 固定使用 `github.com/yuin/goldmark v1.8.4`、`extension.GFM` 与 AST source ranges。输入先把 CRLF 规范为 LF、拒绝 bare CR，并要求完整 UTF-8 文档恰有一个 inline plain-text 为 `Acceptance Criteria` 的 top-level H2；验收区间从该 H2 标题行末字节后开始，到下一 top-level H2 起始字节前结束。该区间除空白外必须恰有一个 top-level unordered list，criterion 只能来自 direct `ListItem`。每项必须在 column 0 以 exact `- [ ] ` 开头，checkbox 后首个非空 inline 必须是唯一匹配 `^PF-AC-[0-9]{3}$` 的 code span；nested list、fenced block、第二个 top-level block、continuation 中的伪 ID 与 parser-specific 拼接 span 均拒绝。

每条 requirement 使用一个连续源码范围：从 direct item 的 `-` 起始字节到下一 direct item 的 `-` 前一字节，末项到 top-level list 结束字节。规范化只允许：删除首行 exact `- [ ] `；后续每个非空源码行必须至少有两个 ASCII space 且只移除这两个；空白行保留；逐行移除尾随 ASCII space/tab；保留其余 Markdown bytes 与内部空白行；末尾折叠为恰一个 LF。registry 与 parser 必须拒绝 unknown、duplicate、non-contiguous ID、范围参数、AST/source-range 不一致、stale hash 和 selector 顺序漂移。`PF-AC-014` 的完整 normalized bytes 固定进入 testdata，长度为 3236 bytes，literal SHA-256 为 `f41f346cdbe9ca93e159a01a74b4f6d95330f13554c24f969685b854a1027212`；删除、拆分、折叠空白或遗漏任一字节都失败。

registry 必须恰有以下 19 行且顺序固定；每行 receipt 包含 `criterion_id`、`selector`、`normalized_requirement_sha256`、实际命令与 exit code、non-mock production/integration evidence、artifact digest 和验证时间。`--verify-registry` 对 PRD direct items 与 registry 做 exact 19、连续 ID、唯一 selector、source hash 与 receipt path 全等检查；任何 skipped、mock-only、test-only symbol 或没有生产 caller 的 behavioral row 都失败。

| 验收标准 | 可执行 evidence selector | 最终 receipt |
|---|---|---|
| `PF-AC-001` | `auth.policy-parity`：API、store query builder、worker 与 typed floor 的授权结果全等 | `artifacts/acceptance/PF-AC-001.json` |
| `PF-AC-002` | `auth.admin-and-opaque-404`：admin/group allow-deny 交集与外部 opaque 404 | `artifacts/acceptance/PF-AC-002.json` |
| `PF-AC-003` | `delivery.owner-generation-fencing`：outbox/idempotency/guard owner generation、lease、TTL 与旧 owner fencing | `artifacts/acceptance/PF-AC-003.json` |
| `PF-AC-004` | `domain.profile-isolation`：postgres_sync/s3_worm 物理身份、未选 profile 零读取与 alias 拒绝 | `artifacts/acceptance/PF-AC-004.json` |
| `PF-AC-005` | `acl.manifest-and-role-boundary`：migration manifest、canonical function identity、角色与 catalog exact ACL | `artifacts/acceptance/PF-AC-005.json` |
| `PF-AC-006` | `ledger.continuous-full-confirm`：所有 confirm fast path 逐对象并从 genesis 验证到 immutable tail | `artifacts/acceptance/PF-AC-006.json` |
| `PF-AC-007` | `ledger.not-committed-release`：attempt_not_committed witness durable 前后 reservation 语义 | `artifacts/acceptance/PF-AC-007.json` |
| `PF-AC-008` | `recovery.trust-witness-rebuild`：仅凭 full witness 重建完整非秘密 trust chain | `artifacts/acceptance/PF-AC-008.json` |
| `PF-AC-009` | `activation.explicit-genesis`：revision-0 fail closed、显式 plan/apply 与 cutpoint exact resume | `artifacts/acceptance/PF-AC-009.json` |
| `PF-AC-010` | `activation.commitment-dag`：无环 canonical plan/authorization/bundle/trust/ledger DAG | `artifacts/acceptance/PF-AC-010.json` |
| `PF-AC-011` | `admission.minimum-fence`：API/LB/queue membership 与单调 minimum fence gate | `artifacts/acceptance/PF-AC-011.json` |
| `PF-AC-012` | `recovery.key-policy-lifecycle`：key/policy add、rotate、retire、compromise、remove 与依赖库存 | `artifacts/acceptance/PF-AC-012.json` |
| `PF-AC-013` | `rotation.domain-recovery`：四逻辑域 planned/disaster replacement、copy、cutover、retirement | `artifacts/acceptance/PF-AC-013.json` |
| `PF-AC-014` | `rotation.candidate-control`：policy/challenge/request、nonce、credential revoke、abandon 与 teardown | `artifacts/acceptance/PF-AC-014.json` |
| `PF-AC-015` | `security.key-and-delete-token`：strict signing-key loader、active-key gate 与 CSPRNG delete token | `artifacts/acceptance/PF-AC-015.json` |
| `PF-AC-016` | `retention.registry-and-residue`：八 registry roots、21 lifecycle、24 participant 与字段级零残留 | `artifacts/acceptance/PF-AC-016.json` |
| `PF-AC-017` | `retention.protected-merge-attestation`：claim、双亲 merge、protected rescan/sign 与 metadata chain | `artifacts/acceptance/PF-AC-017.json` |
| `PF-AC-018` | `deploy.bootstrap-wiring`：config、migration runner、worker、router、Compose 与 systemd 装配 | `artifacts/acceptance/PF-AC-018.json` |
| `PF-AC-019` | `quality.full-gates`：focused/race/integration/repository gates 全部 fresh 通过 | `artifacts/acceptance/PF-AC-019.json` |

## Approved revision checkpoint

Tasks 1–8 describe the original implementation pass and remain the contract baseline. The frozen branch already contains a provisional implementation, but prior green tests do not complete the task. The following revision tasks are mandatory and supersede any conflicting implementation detail above. Keep `.tmp/`, `node_modules/`, staging credentials and all sibling task directories untouched. The control review completed on 2026-07-15 and the user explicitly approved the revised parent/platform planning artifacts; resume only the existing frozen platform-foundation worktree under this contract. Do not stage, commit or push until Task 18 completes and the control session has reviewed the exact file set.

## Task 9: Close actor-scope and deletion-query authorization gaps

**Files:**

- Modify: `internal/center/http/middleware.go`
- Modify: `internal/center/http/middleware_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`
- Modify: `internal/center/store/record_auth.go`
- Modify: `internal/center/store/record_auth_test.go`

- [ ] **Step 1: Add failing session-scope tests.** Add `TestRequireSessionHydratesPersistedRecordGroups`, `TestRequireSessionRejectsUnavailableScopeStore`, and a forged-header case. The successful handler must observe sorted/deduplicated persisted group IDs; `X-Project-ID`, `X-Role`, and `X-Group-ID` must have no effect. Scope-store failure must stop the request with a safe `503` envelope, not create an empty scope or report a false `401`.

```go
func TestRequireSessionHydratesPersistedRecordGroups(t *testing.T) {
    scopes := fakeScopeRepository{groups: []string{"group-b", "group-a", "group-a"}}
    // RequireSession(authn, scopes) must expose group-a/group-b from the store.
}
```

- [ ] **Step 2: Add failing SQL-filter tests.** For `CapabilityDeletionStatusRead`, require a non-admin predicate equivalent to `project_id = actor.ProjectID AND operation_initiator_user_id = actor.UserID`; project admin may use project-only filtering. `CapabilityDeletionAuditRead` must fail construction for non-admin and use project-only filtering for admin. Preserve the final per-row `Policy.Authorize` check.

```go
switch capability {
case recordauth.CapabilityDeletionStatusRead:
    if recordauth.NormalizeRole(actor.Role) == recordauth.RoleProjectAdmin {
        predicate = "record_scope.project_id = $1"
    } else {
        predicate = "record_scope.project_id = $1 and record_scope.operation_initiator_user_id = $2"
    }
case recordauth.CapabilityDeletionAuditRead:
    predicate = "record_scope.project_id = $1" // policy preflight already requires admin
}
```

- [ ] **Step 3: Verify RED.** Run `go test ./internal/center/http ./internal/center/store ./internal/center/recordauth -run 'SessionScope|RecordAuth|Deletion(Status|Audit)' -count=1`. Expected: failures show that `RequireSession` has no scope repository and deletion status is filtered by project only.

- [ ] **Step 4: Wire the production scope repository.** Change the middleware to require `recordauth.ScopeRepository`, load groups after authentication, normalize them through one exported `recordauth.NormalizeActorScope` path, and put the resulting actor in context. Add the repository to `RouterOptions`; bootstrap must pass `store.NewPostgresRecordAuthorizationRepository(db.Pool())`. There is no optional/nil production fallback.

```go
func RequireSession(authn handlers.AuthService, scopes recordauth.ScopeRepository) func(http.Handler) http.Handler
```

- [ ] **Step 5: Implement capability-specific query filters.** Add the following single builder and assert its exact clause/args pairs. `CapabilityDeletionStatusRead` returns `record_scope.project_id = $1 AND record_scope.operation_initiator_user_id = $2` with `[actor.ProjectID, actor.UserID]` for non-admins and `record_scope.project_id = $1` with `[actor.ProjectID]` for project admins. `CapabilityDeletionAuditRead` returns the project-only pair only for project admins. Unknown actor/role/capability/scope returns `recordauth.ErrDenied` and no clause/args. Callers append keyset and limit arguments only after this result and retain final per-row `Policy.Authorize`.

```go
func BuildDeletionReadFilter(
    actor recordauth.ActorScope,
    capability recordauth.Capability,
) (clause string, args []any, err error)
```

- [ ] **Step 6: Verify GREEN.** Re-run the Step 3 command and `go test ./cmd/houfeng-center -run 'Bootstrap|Router' -count=1`. Expected: PASS with no client-controlled scope path.

## Task 10: Make membership, replay and minimum-version gates authoritative

**Files:**

- Modify: `internal/center/recordplatform/health.go`
- Modify: `internal/center/recordplatform/health_test.go`
- Create: `internal/center/recordplatform/membership.go`
- Create: `internal/center/recordplatform/membership_test.go`
- Modify: `internal/center/store/record_platform_probe.go`
- Modify: `internal/center/store/record_platform_probe_test.go`
- Modify: `internal/center/store/record_platform.go`
- Modify: `internal/center/store/record_platform_test.go`
- Modify: `internal/center/store/deletion_reconcile.go`
- Modify: `internal/center/store/deletion_reconcile_test.go`
- Modify: `internal/center/store/record_leases.go`
- Modify: `internal/center/store/record_leases_test.go`
- Modify: `internal/center/recovery/fence_projector.go`
- Modify: `internal/center/recovery/fence_projector_test.go`
- Modify: `internal/center/http/handlers/health.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`
- Create: `scripts/test-record-platform-integration.sh`

- [ ] **Step 1: Add RED tests for monotonic process safety.** After observing minimum version `CurrentFenceContractVersion+1`, a later zero result, lower result, dependency error, or stale app projection must keep `ProcessReady=false` and preserve the higher observed minimum. A fresh process that has never observed an activation may remain process-ready while records are locally unavailable.

```go
health.ObserveMinimumFenceContract(CurrentFenceContractVersion + 1)
health.SetProcessGateFromProbe(RuntimeProbeResult{})
if got := health.RuntimeHealthSnapshot(); got.ProcessReady || got.MinimumFenceContractVersion != CurrentFenceContractVersion+1 {
    t.Fatalf("gate downgraded: %#v", got)
}
```

- [ ] **Step 2: Add RED admission tests.** The platform probe must not set API LB admission until `deletion_replay_state(sequence,hash)` exactly equals the fresh witness head and the signed inventory is current. API rows set `load_balancer_admitted=true, queue_admitted=false`; the outbox worker row sets `false,true`; fence projector, inventory repair and not-committed reconciler rows use `instance_kind=recovery` and set both bits false. Each process component has a distinct stable `instance_id`; one API row cannot authorize a worker. Add `GET /api/system/record-platform/admission`, returning bodyless 204 only when this process has the exact live API membership and monotonic process gate; otherwise return bodyless 503. Production load balancers must probe this endpoint and require exact 204. A v0.59 process has no route and therefore cannot be admitted even if its legacy `/ready` returns 200.

- [ ] **Step 3: Add RED claim/lease tests.** `PostgresRecordOutboxRepository.Claim`, `AcquireObjectContentLease`, `AcquireClientContentLease`, `FinalizeObjectContentDelivery` and deletion-reservation finalization must reject missing, expired, wrong-epoch, wrong-kind, low-contract or capability-mismatched membership. Outbox/idempotency/guard claim and finalize additionally require exact owner ID, monotonic generation and `owner_expires_at > transaction_now()`; expired processing is reclaimable, but every old-owner finalize affects 0 rows, idempotency expiry is later than the owner lease, and janitor never deletes a live owner. Not-committed reconciliation and fence/inventory repair require a live `recovery` membership with the current contract, but not an ordinary queue-admission bit. The shared authorizer is the conformance boundary later queue repositories must call before claim/finalize; this task does not invent an unnamed future queue.

```go
type AdmissionKind string

const (
    AdmissionLoadBalancer AdmissionKind = "load_balancer"
    AdmissionQueue        AdmissionKind = "queue"
    AdmissionRecovery     AdmissionKind = "recovery"
)

type MembershipIdentity struct {
    DeploymentID, InstanceID, InstanceKind string
    DeploymentEpoch, FenceContractVersion uint64
    Capability recordauth.Capability
}

type MembershipAuthorizer interface {
    AssertAdmission(context.Context, MembershipIdentity, AdmissionKind, time.Time) error
}
```

- [ ] **Step 4: Verify RED.** Run `go test ./internal/center/recordplatform ./internal/center/store ./internal/center/recovery ./cmd/houfeng-center -run 'Membership|Admission|ProcessGate|Replay|Lease|Outbox|NotCommitted|Bootstrap' -count=1`. Expected: the current single API row, project-only probe and unguarded claim/lease SQL fail the new tests.

- [ ] **Step 5: Implement one membership repository and SQL guard.** Use a single transaction-safe `AssertAdmission` query over `(deployment_id, instance_id, deployment_epoch)` with `expires_at > now`, exact kind, capability membership, contract version, and the required admission bit. Constructors for claim/lease repositories receive an immutable `MembershipIdentity`; do not trust event payload identity.

- [ ] **Step 6: Make replay equality part of API admission.** Read `deletion_replay_state` in the same probe cycle as the fresh witness head; any missing/mismatched state leaves both admission bits false. Persist the highest observed minimum contract with `GREATEST(existing, observed)` and never write a lower value.

- [ ] **Step 7: Start repair workers independently of capability availability.** When records/permanent-delete configuration is present, bootstrap the runtime probe, fence projector, inventory worker and reconciler even while the protected capability is unavailable. Wrap each in an explicit recovery membership heartbeat. Only user-content/outbox workers use queue admission. A transient startup outage must be able to converge without process restart, while unknown/below-contract workers still claim zero work.

- [ ] **Step 8: Add the PostgreSQL fixture wrapper and verify GREEN.** `scripts/test-record-platform-integration.sh` accepts mode `postgres`, the separator `--`, and then the child argv. It must choose four unused loopback ports, create a `mktemp -d` workspace and four synthetic passwords, start app/ledger/witness/recovery `postgres:16-alpine` containers with separate data dirs and `--network=host`, wait with `pg_isready`, prove four distinct `pg_control_system().system_identifier` values, and export `HOUFENG_POSTGRES_INTEGRATION=1` plus APP/LEDGER/WITNESS/RECOVERY fixture URLs only to the child command. It captures verbose output, fails if any `--- SKIP:` appears, and removes every container/workspace in an EXIT trap. A separate same-server alias fixture must be rejected by the domain gate. Run Step 4, then:

```bash
scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store/migrate \
  -run 'PostgresIntegrationRecordPlatform.*(Membership|Lease|Takeover)' -count=1
```

Expected: PASS, no `--- SKIP:`, and no surviving fixture container.

## Task 11: Extend canonical contracts and migrations for activation governance

**Files:**

- Modify: `internal/center/recovery/types.go`
- Modify: `internal/center/recovery/trust_store.go`
- Modify: `internal/center/recovery/trust_postgres.go`
- Modify: `internal/center/recovery/trust_service.go`
- Modify: `internal/center/recovery/*_test.go`
- Modify: `internal/center/recovery/source_object_adapter.go`
- Modify: `internal/center/recovery/source_object_adapter_test.go`
- Modify: `internal/center/recordauth/types.go`
- Modify: `internal/center/recordauth/policy.go`
- Modify: `internal/center/recordauth/policy_test.go`
- Modify: `internal/center/recordplatform/source_deletion_adapter.go`
- Modify: `internal/center/recordplatform/source_deletion_adapter_test.go`
- Modify: `internal/center/store/record_auth.go`
- Modify: `internal/center/store/record_auth_test.go`
- Modify: `internal/center/deletionledger/types.go`
- Modify: `internal/center/deletionledger/canonical.go`
- Modify: `internal/center/deletionledger/postgres.go`
- Modify: `internal/center/deletionledger/witness_postgres.go`
- Modify: `internal/center/deletionledger/witness_s3.go`
- Modify: `internal/center/deletionledger/*_test.go`
- Modify: `db/recoverycontrol/migrations/0001_create_recovery_control.sql`
- Modify: `db/deletionledger/migrations/0001_create_deletion_ledger.sql`
- Modify: `db/deletionwitness/migrations/0001_create_full_witness.sql`
- Modify: `db/migrations/0051_create_record_platform_foundation.sql`
- Modify: `internal/center/platformmigrate/roles.go`
- Modify: `internal/center/platformmigrate/roles_test.go`
- Modify: `internal/center/platformmigrate/runner.go`
- Modify: `internal/center/platformmigrate/runner_test.go`
- Modify: `internal/center/platformmigrate/domain_identity.go`
- Modify: `internal/center/platformmigrate/domain_identity_test.go`
- Modify: `internal/center/store/migrate/migrate.go`
- Modify: `internal/center/store/migrate/migrate_test.go`
- Modify: `internal/center/store/migrate/postgres_integration_test.go`
- Create: `internal/center/store/migrate/acl_manifest.go`
- Create: `internal/center/store/migrate/acl_manifest_test.go`

- [ ] **Step 1: Write canonical and authorization-floor RED golden tests.** Freeze exact body bytes plus derived wrapper hash for bootstrap, rotate, retire, compromise, removed and approval-policy rotation trust entries; changing a derived hash field must never change its own preimage. Freeze a sequence-1 contract activation body binding `plan_digest`, `authorization_artifact_digest`, `activation_bundle_digest`, `trust_revision`, `trust_head_hash`, `inventory_digest`, `approval_policy_digest`, `drain_receipt_digest`, `domain_identity_set_digest`, `domain_identity_epoch` and minimum fence version. Freeze all four ledger variants: delete/outcome carry deletion request identity; activation/identity-rotation carry none of actor/route/object/idempotency/fingerprint/origin/floor/release. Across recordauth/store/source-deletion/recovery adapters, cover `capture project-wide → live source restrict group A → source delete → restore older backup`; floor canonical bytes include explicit `VisibilityKind`, so project and restricted-empty decode differently, and full-witness floor keeps non-group-A users at 404 across API/query/worker. Missing/paired-floor mismatch, arbitrary visibility, unknown kind/version and widening all deny. Freeze the commitment DAG: `MutationBundle` contains pre-entry bodies with no bundle/plan/authorization/final-head/current-hash field; full raw plan is hashed next, authorization artifact next, then final trust and ledger bodies. Unknown/missing fields, wrong union shape, nil/empty ambiguity, duplicate identity-set serialization, any digest self-reference, and applying old canonical bytes under v1 must fail.

```go
type TrustMutationKind string
const (
    TrustMutationBootstrap TrustMutationKind = "bootstrap"
    TrustMutationAdd TrustMutationKind = "add"
    TrustMutationRotate TrustMutationKind = "rotate"
    TrustMutationRetire TrustMutationKind = "retire"
    TrustMutationCompromise TrustMutationKind = "compromise"
    TrustMutationRemove TrustMutationKind = "remove"
    TrustMutationApprovalPolicyRotate TrustMutationKind = "approval_policy_rotate"
    TrustMutationDomainIdentityRotate TrustMutationKind = "domain_identity_rotate"
)

type TrustReasonCodeV1 string
const (
    TrustReasonBootstrapActivated TrustReasonCodeV1 = "bootstrap_activated"
    TrustReasonKeyAdded          TrustReasonCodeV1 = "key_added"
    TrustReasonKeyRotated        TrustReasonCodeV1 = "key_rotated"
    TrustReasonKeyRetired        TrustReasonCodeV1 = "key_retired"
    TrustReasonKeyCompromised    TrustReasonCodeV1 = "key_compromised"
    TrustReasonKeyRemoved        TrustReasonCodeV1 = "key_removed"
)

type MutationBundle struct {
    Version uint16
    MutationID string
    MutationKind TrustMutationKind
    Artifacts []MutationArtifact
}

type MutationArtifact struct {
    Kind MutationArtifactKind
    CanonicalBytes []byte
    Digest [32]byte
}

type MutationArtifactKind string

const (
    ArtifactTrustMutationPayload      MutationArtifactKind = "trust_mutation_payload"
    ArtifactCurrentApprovalPolicy     MutationArtifactKind = "current_approval_policy"
    ArtifactCandidateApprovalPolicy   MutationArtifactKind = "candidate_approval_policy"
    ArtifactDomainAttestationPolicy   MutationArtifactKind = "domain_attestation_policy"
    ArtifactAdmissionDrainReceipt     MutationArtifactKind = "admission_drain_receipt"
    ArtifactActivationInventory       MutationArtifactKind = "activation_inventory"
    ArtifactSignedInventory           MutationArtifactKind = "signed_inventory"
    ArtifactActivationManifest        MutationArtifactKind = "activation_manifest"
    ArtifactContractActivationPayload MutationArtifactKind = "contract_activation_payload"
    ArtifactDependencyInventory       MutationArtifactKind = "dependency_inventory"
    ArtifactRotationIntent            MutationArtifactKind = "rotation_intent"
    ArtifactRotationCutover           MutationArtifactKind = "rotation_cutover"
)

type CanonicalEntry struct {
    Body                LedgerEntryBody
    EntryHash           [32]byte // derived wrapper; excluded from Body bytes
}

type LedgerEntryBody struct {
    Common              LedgerEntryCommon
    DeleteCommit        *DeleteCommitPayload
    AttemptNotCommitted *AttemptNotCommittedPayload
    ContractActivation  *ContractActivationPayload
    DomainIdentityRotation *DomainIdentityRotationPayload
}

type LedgerEntryCommon struct {
    Version uint16
    Sequence uint64
    Type LedgerEntryType
    DeploymentID, ProjectID, OperationID string
    ConfirmedAt time.Time
    PreviousHash [32]byte
}

type DeletionRequestIdentity struct {
    ActorID string
    Route DeletionRouteCodeV1
    ObjectKind, ObjectID string
    DeletionRequestTokenCommitment [32]byte
    RequestFingerprint [32]byte
}

type DeletionRouteCodeV1 string
const (
    DeletionRouteRecordPermanentDelete DeletionRouteCodeV1 = "record_permanent_delete"
    DeletionRouteSourcePermanentDelete DeletionRouteCodeV1 = "source_permanent_delete"
)

type DeletionReasonCodeV1 string
const (
    DeletionReasonUserConfirmed DeletionReasonCodeV1 = "user_confirmed"
    DeletionReasonSourceRemoved DeletionReasonCodeV1 = "source_removed"
    DeletionReasonRetentionReplay DeletionReasonCodeV1 = "retention_replay"
)

type OriginIdentityV1 struct {
    Version uint16
    Kind string // closed source-kind registry
    CanonicalID string
}

type DeleteCommitPayload struct {
    Request DeletionRequestIdentity
    OriginIdentity *OriginIdentityV1
    AuthorizationFloor *AuthorizationFloorV1
    DeletionContractVersion uint64
    ReasonCode DeletionReasonCodeV1
}

type AuthorizationFloorV1 struct {
    Version uint16
    Kind recordauth.VisibilityKind
    ProjectID string
    AllowedRoles, AllowedGroupIDs []string
    PolicyVersion, PolicyRevision uint64
}

type AttemptNotCommittedPayload struct {
    Request DeletionRequestIdentity
    ReleaseEpoch uint64
}

type ContractActivationPayload struct {
    MinimumFenceContractVersion uint64
    PlanDigest, AuthorizationArtifactDigest [32]byte
    ActivationBundleDigest, TrustHeadHash [32]byte
    TrustRevision uint64
    InventoryDigest, ApprovalPolicyDigest, AdapterPolicyDigest [32]byte
    AdapterPolicyGeneration uint64
    DrainReceiptDigest, DomainIdentitySetDigest [32]byte
    DomainIdentityEpoch uint64
}

type DomainIdentityRotationPayload struct {
    MinimumFenceContractVersion uint64
    PlanDigest, AuthorizationArtifactDigest [32]byte
    IntentBundleDigest, CutoverBundleDigest [32]byte
    CurrentIdentitySetDigest, CandidateIdentitySetDigest [32]byte
    CurrentIdentitySetEpoch, CandidateIdentitySetEpoch uint64
    CurrentApprovalSetDigest, CandidatePossessionDigest [32]byte
    CurrentAdapterPolicyDigest, CandidateAdapterPolicyDigest [32]byte
    CurrentAdapterPolicyGeneration, CandidateAdapterPolicyGeneration uint64
    CopyReceiptDigest, DrainReceiptDigest [32]byte
    TrustRevision uint64
    TrustHeadHash [32]byte
}

// Built after plan/authorization and actual copy/drain receipts, before the
// cutover bundle/final trust/final ledger digests exist.
type DomainIdentityRotationPreEntryPayload struct {
    MinimumFenceContractVersion uint64
    PlanDigest, AuthorizationArtifactDigest [32]byte
    IntentBundleDigest [32]byte
    CurrentIdentitySetDigest, CandidateIdentitySetDigest [32]byte
    CurrentIdentitySetEpoch, CandidateIdentitySetEpoch uint64
    CurrentApprovalSetDigest, CandidatePossessionDigest [32]byte
    CurrentAdapterPolicyDigest, CandidateAdapterPolicyDigest [32]byte
    CurrentAdapterPolicyGeneration, CandidateAdapterPolicyGeneration uint64
    CopyReceiptDigest, DrainReceiptDigest [32]byte
}

// Serialized exactly once as ArtifactContractActivationPayload before the
// bundle digest exists; the final ledger payload above binds the bundle digest.
type ContractActivationPreEntryPayload struct {
    MinimumFenceContractVersion uint64
    TrustRevision uint64
    InventoryDigest, ApprovalPolicyDigest, AdapterPolicyDigest [32]byte
    AdapterPolicyGeneration uint64
    DomainAttestationPolicyDigest [32]byte
    DrainReceiptDigest, DomainIdentitySetDigest [32]byte
    DomainIdentityEpoch uint64
    CanonicalDomainIdentitySet []byte
}
```

- [ ] **Step 2: Add migration RED tests.** Require the exact DDL contract below, merging additions into the fresh `0001`/`0051` `CREATE TABLE` statements where that is clearer. The final 0051 source owns exactly 22 public application/governance tables: the existing 18 foundation tables, its local two domain identity/attestation tables and two APP ACL manifest tables；runner-owned `schema_migrations` and the internal extension schema are excluded from this count. The APP 0051、deletion-ledger 0001、deletion-witness 0001 and recovery-control 0001 migrations each create their own local `record_platform_internal` schema, install `pgcrypto` into that schema, verify `pg_extension.extnamespace` exact-match, revoke PUBLIC schema/function access, and create a physically local `public.record_platform_domain_identity` plus `public.record_platform_domain_attestations` pair with local immutable triggers. No domain grants access to an absent/shared identity table, and no PostgreSQL domain relies on another database's extension namespace or relation. Every immutable table is protected against UPDATE/DELETE/TRUNCATE；the byte bounds shown below are also parser limits. No table contains command lines, local paths, free text, private-key bytes or record/object content.

Freeze one `CanonicalInfrastructureIdentityRegistryV1` used by config, migrator, live probe, Go encoders, every SQL CHECK/matcher, retention rows and S3/IAM key-policy goldens. `DeploymentIDV1` is exact ASCII `dp-[0-9a-f]{64}` and v1 `ProjectIDV1` is exact bytes `default`; no trimming, case folding or Unicode normalization exists. `HTTPSAuthorityV1` parses config-only raw input with exact `https`, no userinfo, path other than empty or a single input `/` normalized to empty, query, fragment, percent escape or control; host is lowercase ASCII DNS or `netip` canonical IP (bracketed for IPv6), default 443 is omitted and a nondefault port is decimal 1…65535 without leading zero. Its digest preimage is exact bytes `HOUFENG-HTTPS-AUTHORITY-V1 || u16be(1) || u32be(len(host)) || host || u16be(port-or-zero-for-443)`; raw URL is never stored. Infrastructure normalization adapter kind is closed to `aws_rds_postgres_v1|gcp_cloudsql_postgres_v1|azure_postgres_v1|postgres_external_v1|aws_s3_v1|minio_v1|s3_compatible_v1`, normalization version is exactly 1, and each provider/account/cluster/physical-storage/snapshot-policy/restore-authority input is bounded by its adapter-specific 1…512-byte ASCII grammar. The persisted field digest preimage is `HOUFENG-INFRASTRUCTURE-IDENTITY-FIELD-V1 || u32be(len(adapter-kind)) || adapter-kind || u16be(version) || u32be(len(field-kind)) || field-kind || u32be(len(canonical-value)) || canonical-value`; raw values never enter governance. Renewal keeps adapter kind/version and all six digests identical; normalization changes require domain rotation. `S3BucketNameV1` and the 4…540-byte `S3NamespaceV1` `u32be(count)+up to eight (u32be(length),segment)` tuple use the design grammar. Checked-in byte/hex goldens cover every preimage plus case/default-port/trailing-slash/IPv6/Unicode/IDNA/userinfo/path/query/percent/leading-zero-port aliases, namespace exact-max/+1 and every adapter/field confusion.

```sql
create schema if not exists record_platform_internal;
revoke all on schema record_platform_internal from public;
create extension if not exists pgcrypto with schema record_platform_internal;
-- The migrator rejects an existing pgcrypto whose extnamespace is not
-- record_platform_internal; it never moves an installed extension implicitly.
revoke execute on all functions in schema record_platform_internal from public;

-- Repeat this exact local pair in APP 0051, deletion-ledger 0001,
-- deletion-witness 0001 and recovery-control 0001; it is not shared storage.
create table public.record_platform_domain_identity (
  domain_id text primary key check (domain_id ~ '^rd-[0-9a-f]{64}$'),
  domain_kind text not null check (domain_kind in
    ('application','deletion_ledger','deletion_witness','recovery_control')),
  identity_epoch bigint not null default 1 check (identity_epoch > 0),
  identity_mode text not null check (identity_mode in
    ('postgres_system','external_attestation')),
  postgres_system_identifier text
    check (postgres_system_identifier ~ '^[1-9][0-9]{0,19}$'),
  external_stable_identity_digest bytea
    check (octet_length(external_stable_identity_digest) = 32),
  provisioning_attestation_digest bytea
    check (octet_length(provisioning_attestation_digest) = 32),
  database_oid oid not null,
  database_name text not null check (database_name ~ '^[a-z][a-z0-9_]{0,62}$'),
  provisioned_at timestamptz not null default now(),
  check (
    (identity_mode = 'postgres_system'
      and postgres_system_identifier is not null
      and external_stable_identity_digest is null
      and provisioning_attestation_digest is null)
    or
    (identity_mode = 'external_attestation'
      and postgres_system_identifier is null
      and external_stable_identity_digest is not null
      and provisioning_attestation_digest is not null)
  )
);

create table public.record_platform_domain_attestations (
  domain_id text not null,
  attestation_purpose text not null check (attestation_purpose in
    ('provision','renew','rotation_candidate','retirement')),
  attestation_generation bigint not null check (attestation_generation > 0),
  stable_identity_digest bytea not null
    check (octet_length(stable_identity_digest) = 32),
  canonical_attestation_body bytea not null
    check (octet_length(canonical_attestation_body) between 1 and 65536),
  attestation_body_digest bytea not null
    check (octet_length(attestation_body_digest) = 32),
  canonical_attestation bytea not null
    check (octet_length(canonical_attestation) between 1 and 131072),
  attestation_digest bytea not null unique
    check (octet_length(attestation_digest) = 32),
  signature_set_digest bytea not null
    check (octet_length(signature_set_digest) = 32),
  signature_count smallint not null check (signature_count between 1 and 64),
  attestation_policy_digest bytea not null
    check (octet_length(attestation_policy_digest) = 32),
  valid_from timestamptz not null,
  expires_at timestamptz not null,
  witnessed_at timestamptz not null default now(),
  primary key (domain_id, attestation_generation),
  foreign key (domain_id) references public.record_platform_domain_identity(domain_id)
    on delete restrict,
  check (attestation_body_digest =
    record_platform_internal.digest(canonical_attestation_body, 'sha256')),
  check (attestation_digest =
    record_platform_internal.digest(canonical_attestation, 'sha256')),
  check (expires_at > valid_from)
);

-- Each of APP 0051, deletion-ledger 0001, deletion-witness 0001 and
-- recovery-control 0001 installs this helper locally before its identity pair.
create or replace function record_platform_internal.reject_immutable_mutation()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog
as $$
begin
  raise exception using
    errcode = '55000',
    message = 'record-platform immutable artifact cannot be mutated';
  return null;
end
$$;

revoke all on function record_platform_internal.reject_immutable_mutation() from public;

create trigger rp_domain_identity_immutable
before update or delete or truncate on public.record_platform_domain_identity
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_domain_attestations_immutable
before update or delete or truncate on public.record_platform_domain_attestations
for each statement execute function record_platform_internal.reject_immutable_mutation();

create table if not exists public.app_acl_manifest_revisions (
  manifest_revision bigint primary key
    check (manifest_revision between 1 and 999999),
  predecessor_revision bigint generated always as
    (case when manifest_revision = 1 then null else manifest_revision - 1 end) stored,
  previous_manifest_digest bytea not null
    check (octet_length(previous_manifest_digest) = 32),
  canonical_migration_set bytea not null
    check (octet_length(canonical_migration_set) between 1 and 4194304),
  sorted_migration_set_digest bytea not null
    check (octet_length(sorted_migration_set_digest) = 32),
  canonical_privilege_set bytea not null
    check (octet_length(canonical_privilege_set) between 1 and 4194304),
  privilege_set_digest bytea not null
    check (octet_length(privilege_set_digest) = 32),
  manifest_digest bytea not null unique
    check (octet_length(manifest_digest) = 32),
  recorded_at timestamptz not null default transaction_timestamp(),
  unique (manifest_revision, manifest_digest),
  constraint app_acl_manifest_revision_genesis check (
    manifest_revision <> 1
    or previous_manifest_digest = decode(repeat('00', 32), 'hex')
  ),
  constraint app_acl_manifest_migration_digest_matches check (
    sorted_migration_set_digest =
      record_platform_internal.digest(canonical_migration_set, 'sha256')
  ),
  constraint app_acl_manifest_privilege_digest_matches check (
    privilege_set_digest =
      record_platform_internal.digest(canonical_privilege_set, 'sha256')
  ),
  constraint app_acl_manifest_digest_matches check (
    manifest_digest = record_platform_internal.digest(
      convert_to('HOUFENG-APP-ACL-MANIFEST-V1', 'UTF8')
      || int8send(manifest_revision)
      || previous_manifest_digest
      || int4send(octet_length(canonical_migration_set))
      || canonical_migration_set
      || sorted_migration_set_digest
      || int4send(octet_length(canonical_privilege_set))
      || canonical_privilege_set
      || privilege_set_digest,
      'sha256')
  ),
  constraint app_acl_manifest_previous_revision_fk
    foreign key (predecessor_revision, previous_manifest_digest)
    references public.app_acl_manifest_revisions(manifest_revision, manifest_digest)
    on delete restrict
);

create table if not exists public.app_acl_manifest_head (
  singleton boolean primary key default true check (singleton),
  manifest_revision bigint check (manifest_revision between 1 and 999999),
  manifest_digest bytea check (octet_length(manifest_digest) = 32),
  updated_at timestamptz not null default transaction_timestamp(),
  constraint app_acl_manifest_head_pair check (
    (manifest_revision is null and manifest_digest is null)
    or (manifest_revision is not null and manifest_digest is not null)
  ),
  constraint app_acl_manifest_head_revision_fk
    foreign key (manifest_revision, manifest_digest)
    references public.app_acl_manifest_revisions(manifest_revision, manifest_digest)
    on delete restrict
);

insert into public.app_acl_manifest_head(singleton) values (true)
on conflict (singleton) do nothing;

create or replace function record_platform_internal.reject_acl_manifest_revision_mutation()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog
as $$
begin
  raise exception using
    errcode = '55000',
    message = 'app ACL manifest revisions are immutable';
  return null;
end
$$;

revoke all on function
  record_platform_internal.reject_acl_manifest_revision_mutation() from public;

create trigger app_acl_manifest_revisions_immutable
before update or delete or truncate on public.app_acl_manifest_revisions
for each statement execute function
  record_platform_internal.reject_acl_manifest_revision_mutation();

-- Recovery-control only; must remain empty when the active profile is postgres_sync.
create table public.record_platform_s3_witness_identities (
  domain_id text primary key check (domain_id ~ '^rd-[0-9a-f]{64}$'),
  identity_epoch bigint not null default 1 check (identity_epoch > 0),
  https_authority_digest bytea not null
    check (octet_length(https_authority_digest) = 32),
  tls_spki_digest bytea not null check (octet_length(tls_spki_digest) = 32),
  identity_adapter_kind text not null check (identity_adapter_kind in
    ('aws_s3_v1','minio_v1','s3_compatible_v1')),
  normalization_version smallint not null check (normalization_version = 1),
  provider_digest bytea not null check (octet_length(provider_digest) = 32),
  account_digest bytea not null check (octet_length(account_digest) = 32),
  cluster_digest bytea not null check (octet_length(cluster_digest) = 32),
  physical_storage_digest bytea not null
    check (octet_length(physical_storage_digest) = 32),
  snapshot_policy_digest bytea not null
    check (octet_length(snapshot_policy_digest) = 32),
  restore_authority_digest bytea not null
    check (octet_length(restore_authority_digest) = 32),
  bucket_name text not null check (
    bucket_name ~ '^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$'
    and bucket_name !~ '\.\.|\.-|-\.'
    and bucket_name !~ '^[0-9]{1,3}(\.[0-9]{1,3}){3}$'),
  canonical_namespace bytea not null
    check (octet_length(canonical_namespace) between 4 and 540),
  namespace_digest bytea not null check (
    octet_length(namespace_digest) = 32
    and namespace_digest = record_platform_internal.digest(canonical_namespace, 'sha256')),
  versioning_enabled boolean not null check (versioning_enabled),
  object_lock_mode text not null check (object_lock_mode = 'COMPLIANCE'),
  default_retention_seconds bigint not null check (default_retention_seconds >= 315360000),
  legal_hold_required boolean not null check (legal_hold_required),
  stable_identity_digest bytea not null unique
    check (octet_length(stable_identity_digest) = 32),
  provisioning_attestation_digest bytea not null
    check (octet_length(provisioning_attestation_digest) = 32),
  provisioned_at timestamptz not null default now()
);

create table public.record_platform_s3_witness_attestations (
  domain_id text not null,
  attestation_purpose text not null check (attestation_purpose in
    ('provision','renew','rotation_candidate','retirement')),
  attestation_generation bigint not null check (attestation_generation > 0),
  stable_identity_digest bytea not null
    check (octet_length(stable_identity_digest) = 32),
  canonical_attestation_body bytea not null
    check (octet_length(canonical_attestation_body) between 1 and 65536),
  attestation_body_digest bytea not null
    check (octet_length(attestation_body_digest) = 32),
  canonical_attestation bytea not null
    check (octet_length(canonical_attestation) between 1 and 131072),
  attestation_digest bytea not null unique
    check (octet_length(attestation_digest) = 32),
  signature_set_digest bytea not null
    check (octet_length(signature_set_digest) = 32),
  signature_count smallint not null check (signature_count between 1 and 64),
  attestation_policy_digest bytea not null
    check (octet_length(attestation_policy_digest) = 32),
  valid_from timestamptz not null,
  expires_at timestamptz not null,
  witnessed_at timestamptz not null default now(),
  primary key (domain_id, attestation_generation),
  foreign key (domain_id) references public.record_platform_s3_witness_identities(domain_id)
    on delete restrict,
  check (attestation_body_digest =
    record_platform_internal.digest(canonical_attestation_body, 'sha256')),
  check (attestation_digest =
    record_platform_internal.digest(canonical_attestation, 'sha256')),
  check (expires_at > valid_from)
);

-- Recovery-control 0001 only: install the same helper locally before these
-- S3 identity triggers; no function or trigger is shared across databases.
create or replace function record_platform_internal.reject_immutable_mutation()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog
as $$
begin
  raise exception using
    errcode = '55000',
    message = 'record-platform immutable artifact cannot be mutated';
  return null;
end
$$;

revoke all on function record_platform_internal.reject_immutable_mutation() from public;

create trigger rp_s3_witness_identity_immutable
before update or delete or truncate on public.record_platform_s3_witness_identities
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_s3_witness_attestations_immutable
before update or delete or truncate on public.record_platform_s3_witness_attestations
for each statement execute function record_platform_internal.reject_immutable_mutation();

create table public.recovery_trust_mutations (
  mutation_id text primary key check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  plan_digest bytea not null check (octet_length(plan_digest) = 32),
  mutation_kind text not null check (mutation_kind in
    ('bootstrap','add','rotate','retire','compromise','remove','approval_policy_rotate',
     'domain_identity_rotate')),
  deployment_id text not null check (deployment_id ~ '^dp-[0-9a-f]{64}$'),
  project_id text not null check (project_id = 'default'),
  active_profile text not null check (active_profile in ('postgres_sync','s3_worm')),
  domain_identity_epoch bigint not null check (domain_identity_epoch > 0),
  domain_identity_set_digest bytea not null
    check (octet_length(domain_identity_set_digest) = 32),
  state text not null check (state in
    ('intent','copy_pending','dual_write_pending','current_unreachable_pending',
     'drain_pending','import_revoke_pending','trust_primary_unknown',
     'trust_witness_pending','inventory_pending','ledger_primary_unknown',
     'ledger_witness_pending','candidate_cutover_pending','projection_pending',
     'cutover_projection_pending','retirement_pending','final_proof_pending',
     'candidate_teardown_pending','completion_pending','complete')),
  expected_trust_revision bigint not null check (expected_trust_revision >= 0),
  expected_trust_hash bytea not null check (octet_length(expected_trust_hash) = 32),
  expected_ledger_sequence bigint not null check (expected_ledger_sequence >= 0),
  expected_ledger_hash bytea not null check (octet_length(expected_ledger_hash) = 32),
  current_approval_policy_digest bytea not null
    check (octet_length(current_approval_policy_digest) = 32),
  candidate_approval_policy_digest bytea not null
    check (octet_length(candidate_approval_policy_digest) = 32),
  drain_receipt_digest bytea check (octet_length(drain_receipt_digest) = 32),
  drain_scope_digest bytea check (octet_length(drain_scope_digest) = 32),
  authorization_mode text not null check (authorization_mode in ('local_tty','detached')),
  authorization_artifact_digest bytea not null unique
    check (octet_length(authorization_artifact_digest) = 32),
  approval_set_digest bytea check (octet_length(approval_set_digest) = 32),
  operator_principal_digest bytea
    check (octet_length(operator_principal_digest) = 32),
  mutation_bundle_digest bytea not null check (octet_length(mutation_bundle_digest) = 32),
  plan_expires_at timestamptz not null,
  intent_recorded_at timestamptz not null,
  completed_at timestamptz,
  details_delete_after timestamptz,
  last_error_code text not null default ''
    check (last_error_code = '' or last_error_code ~ '^[a-z0-9_]{1,64}$'),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (mutation_id, plan_digest),
  check (((state <> 'complete' and completed_at is null and details_delete_after is null)
    or (state = 'complete' and completed_at is not null
      and details_delete_after = completed_at + interval '30 days')) is true),
  check (((authorization_mode = 'local_tty' and approval_set_digest is null
      and operator_principal_digest is not null)
    or (authorization_mode = 'detached' and approval_set_digest is not null
      and operator_principal_digest is null)) is true),
  check (intent_recorded_at <= plan_expires_at),
  check (((mutation_kind = 'bootstrap'
      and drain_receipt_digest is not null and drain_scope_digest is not null)
    or (mutation_kind = 'domain_identity_rotate' and drain_scope_digest is not null)
    or (mutation_kind not in ('bootstrap','domain_identity_rotate')
      and drain_receipt_digest is null and drain_scope_digest is null)) is true),
  check (((mutation_kind = 'domain_identity_rotate' and state in
      ('intent','copy_pending','dual_write_pending','current_unreachable_pending',
       'drain_pending','import_revoke_pending','trust_primary_unknown',
       'trust_witness_pending','ledger_primary_unknown','ledger_witness_pending',
       'candidate_cutover_pending','cutover_projection_pending','retirement_pending',
       'final_proof_pending','candidate_teardown_pending','completion_pending','complete'))
    or (mutation_kind <> 'domain_identity_rotate' and state in
      ('intent','trust_primary_unknown','trust_witness_pending','inventory_pending',
       'ledger_primary_unknown','ledger_witness_pending','projection_pending','complete')))
    is true),
  check (mutation_kind in ('bootstrap','domain_identity_rotate') or state not in
    ('ledger_primary_unknown','ledger_witness_pending')),
  check (state <> 'inventory_pending' or mutation_kind in
    ('bootstrap','retire','compromise','remove'))
);

create table public.recovery_mutation_plans (
  mutation_id text primary key,
  mutation_kind text not null check (mutation_kind in
    ('bootstrap','add','rotate','retire','compromise','remove','approval_policy_rotate',
     'domain_identity_rotate')),
  plan_digest bytea not null unique check (octet_length(plan_digest) = 32),
  bundle_digest bytea not null check (octet_length(bundle_digest) = 32),
  canonical_plan bytea not null
    check (octet_length(canonical_plan) between 1 and 25165824),
  generated_at timestamptz not null,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  foreign key (mutation_id) references public.recovery_trust_mutations(mutation_id)
    on delete restrict,
  check (plan_digest =
    record_platform_internal.digest(canonical_plan, 'sha256')),
  check (record_platform_internal.platform_mutation_plan_v1_matches(
    canonical_plan, mutation_id, mutation_kind, bundle_digest,
    generated_at, expires_at) is true),
  check (expires_at > generated_at)
);

create table public.recovery_mutation_authorizations (
  mutation_id text primary key,
  plan_digest bytea not null check (octet_length(plan_digest) = 32),
  authorization_mode text not null check (authorization_mode in ('local_tty','detached')),
  authorization_artifact_digest bytea not null unique
    check (octet_length(authorization_artifact_digest) = 32),
  approval_set_digest bytea check (octet_length(approval_set_digest) = 32),
  operator_principal_id text
    check (operator_principal_id ~ '^op-sha256-[0-9a-f]{64}$'),
  intent_recorded_at timestamptz not null,
  canonical_authorization bytea not null
    check (octet_length(canonical_authorization) between 1 and 2097152),
  created_at timestamptz not null default now(),
  foreign key (mutation_id) references public.recovery_trust_mutations(mutation_id)
    on delete restrict,
  check (authorization_artifact_digest =
    record_platform_internal.digest(canonical_authorization, 'sha256')),
  check (record_platform_internal.mutation_authorization_artifact_v1_matches(
    canonical_authorization, mutation_id, plan_digest, authorization_mode,
    approval_set_digest, operator_principal_id, intent_recorded_at) is true),
  check (((authorization_mode = 'local_tty' and approval_set_digest is null
      and operator_principal_id is not null)
    or (authorization_mode = 'detached' and approval_set_digest is not null
      and operator_principal_id is null)) is true)
);

create table public.recovery_mutation_attempts (
  mutation_id text not null,
  attempt_number bigint not null check (attempt_number > 0),
  observed_state text not null check (observed_state in
    ('intent','copy_pending','dual_write_pending','current_unreachable_pending',
     'drain_pending','import_revoke_pending','trust_primary_unknown',
     'trust_witness_pending','inventory_pending','ledger_primary_unknown',
     'ledger_witness_pending','candidate_cutover_pending','projection_pending',
     'cutover_projection_pending','retirement_pending','final_proof_pending',
     'candidate_teardown_pending','completion_pending','complete')),
  cutpoint_code text not null check (cutpoint_code ~ '^[a-z0-9_]{1,64}$'),
  result_code text not null check (result_code ~ '^[a-z0-9_]{1,64}$'),
  started_at timestamptz not null,
  finished_at timestamptz,
  primary key (mutation_id, attempt_number),
  foreign key (mutation_id) references public.recovery_trust_mutations(mutation_id)
    on delete restrict,
  check (finished_at is null or finished_at >= started_at)
);

create table public.recovery_mutation_approval_details (
  mutation_id text not null,
  approval_scope text not null check (approval_scope in ('current','candidate')),
  approval_policy_digest bytea not null
    check (octet_length(approval_policy_digest) = 32),
  approver_principal_id text not null
    check (approver_principal_id ~ '^ak-sha256-[0-9a-f]{64}$'),
  envelope_digest bytea not null check (octet_length(envelope_digest) = 32),
  signed_at timestamptz not null,
  expires_at timestamptz not null,
  primary key (mutation_id, approval_scope, approver_principal_id),
  foreign key (mutation_id) references public.recovery_trust_mutations(mutation_id)
    on delete restrict,
  check (expires_at > signed_at)
);

create table public.recovery_activation_inventories (
  mutation_id text primary key,
  plan_digest bytea not null unique check (octet_length(plan_digest) = 32),
  policy_kind text not null check (policy_kind in ('none','managed')),
  source_count integer not null check (source_count >= 0),
  bounded_until timestamptz not null,
  inventory_digest bytea not null unique check (octet_length(inventory_digest) = 32),
  drain_receipt_digest bytea not null check (octet_length(drain_receipt_digest) = 32),
  drain_scope_digest bytea not null check (octet_length(drain_scope_digest) = 32),
  domain_identity_set_digest bytea not null
    check (octet_length(domain_identity_set_digest) = 32),
  canonical_inventory bytea not null
    check (octet_length(canonical_inventory) between 1 and 8388608),
  created_at timestamptz not null default now(),
  foreign key (mutation_id) references public.recovery_trust_mutations(mutation_id)
    on delete restrict,
  check (inventory_digest =
    record_platform_internal.digest(canonical_inventory, 'sha256')),
  check (record_platform_internal.activation_inventory_v1_matches(
    canonical_inventory, mutation_id, plan_digest, policy_kind, source_count,
    bounded_until, drain_receipt_digest, drain_scope_digest,
    domain_identity_set_digest) is true)
);

create table public.recovery_mutation_bundles (
  mutation_id text not null,
  bundle_kind text not null check (bundle_kind in
    ('activation','mutation','rotation_intent','rotation_cutover')),
  plan_digest bytea not null check (octet_length(plan_digest) = 32),
  mutation_kind text not null check (mutation_kind in
    ('bootstrap','add','rotate','retire','compromise','remove','approval_policy_rotate',
     'domain_identity_rotate')),
  bundle_digest bytea not null unique check (octet_length(bundle_digest) = 32),
  canonical_bundle bytea not null
    check (octet_length(canonical_bundle) between 1 and 20971520),
  created_at timestamptz not null default now(),
  primary key (mutation_id, bundle_kind),
  foreign key (mutation_id) references public.recovery_trust_mutations(mutation_id)
    on delete restrict,
  check (bundle_digest =
    record_platform_internal.digest(canonical_bundle, 'sha256')),
  check (record_platform_internal.mutation_bundle_v1_matches(
    canonical_bundle, mutation_id, bundle_kind, mutation_kind) is true),
  check (((mutation_kind = 'bootstrap' and bundle_kind = 'activation')
    or (mutation_kind = 'domain_identity_rotate'
      and bundle_kind in ('rotation_intent','rotation_cutover'))
    or (mutation_kind not in ('bootstrap','domain_identity_rotate')
      and bundle_kind = 'mutation')) is true)
);

create table public.recovery_mutation_completion_receipts (
  mutation_id text primary key,
  mutation_kind text not null check (mutation_kind in
    ('bootstrap','add','rotate','retire','compromise','remove','approval_policy_rotate',
     'domain_identity_rotate')),
  plan_digest bytea not null check (octet_length(plan_digest) = 32),
  authorization_artifact_digest bytea not null
    check (octet_length(authorization_artifact_digest) = 32),
  bundle_digest bytea not null check (octet_length(bundle_digest) = 32),
  rotation_cutover_bundle_digest bytea
    check (octet_length(rotation_cutover_bundle_digest) = 32),
  trust_primary_receipt_digest bytea not null
    check (octet_length(trust_primary_receipt_digest) = 32),
  trust_witness_receipt_digest bytea not null
    check (octet_length(trust_witness_receipt_digest) = 32),
  inventory_receipt_digest bytea
    check (octet_length(inventory_receipt_digest) = 32),
  ledger_primary_receipt_digest bytea
    check (octet_length(ledger_primary_receipt_digest) = 32),
  ledger_witness_receipt_digest bytea
    check (octet_length(ledger_witness_receipt_digest) = 32),
  projection_receipt_digest bytea not null
    check (octet_length(projection_receipt_digest) = 32),
  replay_receipt_digest bytea
    check (octet_length(replay_receipt_digest) = 32),
  identity_set_primary_receipt_digest bytea
    check (octet_length(identity_set_primary_receipt_digest) = 32),
  identity_set_witness_receipt_digest bytea
    check (octet_length(identity_set_witness_receipt_digest) = 32),
  candidate_control_policy_head_generation bigint
    check (candidate_control_policy_head_generation > 0),
  candidate_control_policy_head_digest bytea
    check (octet_length(candidate_control_policy_head_digest) = 32),
  candidate_control_policy_primary_receipt_digest bytea
    check (octet_length(candidate_control_policy_primary_receipt_digest) = 32),
  candidate_control_policy_witness_receipt_digest bytea
    check (octet_length(candidate_control_policy_witness_receipt_digest) = 32),
  candidate_import_applied_receipt_digest bytea
    check (octet_length(candidate_import_applied_receipt_digest) = 32),
  candidate_import_revocation_receipt_digest bytea
    check (octet_length(candidate_import_revocation_receipt_digest) = 32),
  candidate_cutover_execution_receipt_digest bytea
    check (octet_length(candidate_cutover_execution_receipt_digest) = 32),
  candidate_cutover_revocation_receipt_digest bytea
    check (octet_length(candidate_cutover_revocation_receipt_digest) = 32),
  candidate_artifacts_purge_receipt_digest bytea
    check (octet_length(candidate_artifacts_purge_receipt_digest) = 32),
  workspace_zero_receipt_digest bytea not null
    check (octet_length(workspace_zero_receipt_digest) = 32),
  rotation_receipt_chain_head_hash bytea
    check (octet_length(rotation_receipt_chain_head_hash) = 32),
  rotation_ledger_entry_hash bytea
    check (octet_length(rotation_ledger_entry_hash) = 32),
  old_domain_retirement_receipt_digest bytea
    check (octet_length(old_domain_retirement_receipt_digest) = 32),
  final_domain_proof_receipt_digest bytea
    check (octet_length(final_domain_proof_receipt_digest) = 32),
  candidate_liveness_digest bytea
    check (octet_length(candidate_liveness_digest) = 32),
  canonical_receipt bytea not null
    check (octet_length(canonical_receipt) between 1 and 1048576),
  receipt_digest bytea not null unique check (octet_length(receipt_digest) = 32),
  completed_at timestamptz not null,
  foreign key (mutation_id) references public.recovery_trust_mutations(mutation_id)
    on delete restrict,
  check (canonical_receipt = record_platform_internal.record_platform_encode_mutation_completion_receipt_v1(
    mutation_id, mutation_kind, plan_digest, authorization_artifact_digest,
    bundle_digest, rotation_cutover_bundle_digest, trust_primary_receipt_digest,
    trust_witness_receipt_digest, inventory_receipt_digest,
    ledger_primary_receipt_digest, ledger_witness_receipt_digest,
    projection_receipt_digest, replay_receipt_digest,
    identity_set_primary_receipt_digest, identity_set_witness_receipt_digest,
    candidate_control_policy_head_generation,
    candidate_control_policy_head_digest,
    candidate_control_policy_primary_receipt_digest,
    candidate_control_policy_witness_receipt_digest,
    candidate_import_applied_receipt_digest,
    candidate_import_revocation_receipt_digest,
    candidate_cutover_execution_receipt_digest,
    candidate_cutover_revocation_receipt_digest,
    candidate_artifacts_purge_receipt_digest,
    workspace_zero_receipt_digest, rotation_receipt_chain_head_hash,
    rotation_ledger_entry_hash,
    old_domain_retirement_receipt_digest, final_domain_proof_receipt_digest,
    candidate_liveness_digest, completed_at)),
  check (receipt_digest =
    record_platform_internal.digest(canonical_receipt, 'sha256')),
  check (((mutation_kind = 'bootstrap' and inventory_receipt_digest is not null
      and ledger_primary_receipt_digest is not null
      and ledger_witness_receipt_digest is not null
      and replay_receipt_digest is not null
      and identity_set_primary_receipt_digest is not null
      and identity_set_witness_receipt_digest is not null
      and candidate_control_policy_head_generation is null
      and candidate_control_policy_head_digest is null
      and candidate_control_policy_primary_receipt_digest is null
      and candidate_control_policy_witness_receipt_digest is null
      and candidate_import_applied_receipt_digest is null
      and candidate_import_revocation_receipt_digest is null
      and candidate_cutover_execution_receipt_digest is null
      and candidate_cutover_revocation_receipt_digest is null
      and candidate_artifacts_purge_receipt_digest is null
      and rotation_cutover_bundle_digest is null
      and rotation_receipt_chain_head_hash is null
      and rotation_ledger_entry_hash is null
      and old_domain_retirement_receipt_digest is null
      and final_domain_proof_receipt_digest is null
      and candidate_liveness_digest is null)
    or (mutation_kind = 'domain_identity_rotate'
      and rotation_cutover_bundle_digest is not null
      and ledger_primary_receipt_digest is not null
      and ledger_witness_receipt_digest is not null
      and replay_receipt_digest is not null
      and identity_set_primary_receipt_digest is not null
      and identity_set_witness_receipt_digest is not null
      and candidate_control_policy_head_generation is not null
      and candidate_control_policy_head_digest is not null
      and candidate_control_policy_primary_receipt_digest is not null
      and candidate_control_policy_witness_receipt_digest is not null
      and candidate_import_applied_receipt_digest is not null
      and candidate_import_revocation_receipt_digest is not null
      and candidate_cutover_execution_receipt_digest is not null
      and candidate_cutover_revocation_receipt_digest is not null
      and candidate_artifacts_purge_receipt_digest is not null
      and rotation_receipt_chain_head_hash is not null
      and rotation_ledger_entry_hash is not null
      and old_domain_retirement_receipt_digest is not null
      and final_domain_proof_receipt_digest is not null
      and candidate_liveness_digest is not null
      and inventory_receipt_digest is null)
    or (mutation_kind not in ('bootstrap','domain_identity_rotate')
      and rotation_cutover_bundle_digest is null
      and rotation_receipt_chain_head_hash is null
      and rotation_ledger_entry_hash is null
      and old_domain_retirement_receipt_digest is null
      and final_domain_proof_receipt_digest is null
      and candidate_liveness_digest is null
      and ledger_primary_receipt_digest is null
      and ledger_witness_receipt_digest is null and replay_receipt_digest is null
      and identity_set_primary_receipt_digest is null
      and identity_set_witness_receipt_digest is null
      and candidate_control_policy_head_generation is null
      and candidate_control_policy_head_digest is null
      and candidate_control_policy_primary_receipt_digest is null
      and candidate_control_policy_witness_receipt_digest is null
      and candidate_import_applied_receipt_digest is null
      and candidate_import_revocation_receipt_digest is null
      and candidate_cutover_execution_receipt_digest is null
      and candidate_cutover_revocation_receipt_digest is null
      and candidate_artifacts_purge_receipt_digest is null
      and ((mutation_kind in ('retire','compromise','remove')) =
        (inventory_receipt_digest is not null)))) is true)
);

-- PostgreSQL full-witness schema; S3 uses the same kind/digest/body contract.
create table public.recovery_mutation_witness_artifacts (
  mutation_id text not null check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  artifact_kind text not null check (artifact_kind in
    ('plan','authorization','activation_bundle','mutation_bundle',
     'rotation_intent_bundle','rotation_cutover_bundle')),
  artifact_digest bytea not null check (octet_length(artifact_digest) = 32),
  canonical_bytes bytea not null
    check (octet_length(canonical_bytes) between 1 and 25165824),
  witnessed_at timestamptz not null default now(),
  primary key (mutation_id, artifact_kind),
  unique (artifact_kind, artifact_digest),
  check (artifact_digest =
    record_platform_internal.digest(canonical_bytes, 'sha256')),
  check (record_platform_internal.mutation_witness_artifact_v1_matches(
    artifact_kind, mutation_id, canonical_bytes) is true)
);

-- PostgreSQL full-witness completion is typed, not a generic artifact. This
-- table intentionally mirrors every primary completion field and validator.
create table public.recovery_mutation_completion_receipt_witnesses (
  mutation_id text primary key check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  mutation_kind text not null check (mutation_kind in
    ('bootstrap','add','rotate','retire','compromise','remove','approval_policy_rotate',
     'domain_identity_rotate')),
  plan_digest bytea not null check (octet_length(plan_digest) = 32),
  authorization_artifact_digest bytea not null
    check (octet_length(authorization_artifact_digest) = 32),
  bundle_digest bytea not null check (octet_length(bundle_digest) = 32),
  rotation_cutover_bundle_digest bytea
    check (octet_length(rotation_cutover_bundle_digest) = 32),
  trust_primary_receipt_digest bytea not null
    check (octet_length(trust_primary_receipt_digest) = 32),
  trust_witness_receipt_digest bytea not null
    check (octet_length(trust_witness_receipt_digest) = 32),
  inventory_receipt_digest bytea check (octet_length(inventory_receipt_digest) = 32),
  ledger_primary_receipt_digest bytea
    check (octet_length(ledger_primary_receipt_digest) = 32),
  ledger_witness_receipt_digest bytea
    check (octet_length(ledger_witness_receipt_digest) = 32),
  projection_receipt_digest bytea not null
    check (octet_length(projection_receipt_digest) = 32),
  replay_receipt_digest bytea check (octet_length(replay_receipt_digest) = 32),
  identity_set_primary_receipt_digest bytea
    check (octet_length(identity_set_primary_receipt_digest) = 32),
  identity_set_witness_receipt_digest bytea
    check (octet_length(identity_set_witness_receipt_digest) = 32),
  candidate_control_policy_head_generation bigint
    check (candidate_control_policy_head_generation > 0),
  candidate_control_policy_head_digest bytea
    check (octet_length(candidate_control_policy_head_digest) = 32),
  candidate_control_policy_primary_receipt_digest bytea
    check (octet_length(candidate_control_policy_primary_receipt_digest) = 32),
  candidate_control_policy_witness_receipt_digest bytea
    check (octet_length(candidate_control_policy_witness_receipt_digest) = 32),
  candidate_import_applied_receipt_digest bytea
    check (octet_length(candidate_import_applied_receipt_digest) = 32),
  candidate_import_revocation_receipt_digest bytea
    check (octet_length(candidate_import_revocation_receipt_digest) = 32),
  candidate_cutover_execution_receipt_digest bytea
    check (octet_length(candidate_cutover_execution_receipt_digest) = 32),
  candidate_cutover_revocation_receipt_digest bytea
    check (octet_length(candidate_cutover_revocation_receipt_digest) = 32),
  candidate_artifacts_purge_receipt_digest bytea
    check (octet_length(candidate_artifacts_purge_receipt_digest) = 32),
  workspace_zero_receipt_digest bytea not null
    check (octet_length(workspace_zero_receipt_digest) = 32),
  rotation_receipt_chain_head_hash bytea
    check (octet_length(rotation_receipt_chain_head_hash) = 32),
  rotation_ledger_entry_hash bytea check (octet_length(rotation_ledger_entry_hash) = 32),
  old_domain_retirement_receipt_digest bytea
    check (octet_length(old_domain_retirement_receipt_digest) = 32),
  final_domain_proof_receipt_digest bytea
    check (octet_length(final_domain_proof_receipt_digest) = 32),
  candidate_liveness_digest bytea check (octet_length(candidate_liveness_digest) = 32),
  canonical_receipt bytea not null
    check (octet_length(canonical_receipt) between 1 and 1048576),
  receipt_digest bytea not null unique check (octet_length(receipt_digest) = 32),
  completed_at timestamptz not null,
  witnessed_at timestamptz not null default now(),
  check (canonical_receipt = record_platform_internal.record_platform_encode_mutation_completion_receipt_v1(
    mutation_id, mutation_kind, plan_digest, authorization_artifact_digest,
    bundle_digest, rotation_cutover_bundle_digest, trust_primary_receipt_digest,
    trust_witness_receipt_digest, inventory_receipt_digest,
    ledger_primary_receipt_digest, ledger_witness_receipt_digest,
    projection_receipt_digest, replay_receipt_digest,
    identity_set_primary_receipt_digest, identity_set_witness_receipt_digest,
    candidate_control_policy_head_generation,
    candidate_control_policy_head_digest,
    candidate_control_policy_primary_receipt_digest,
    candidate_control_policy_witness_receipt_digest,
    candidate_import_applied_receipt_digest,
    candidate_import_revocation_receipt_digest,
    candidate_cutover_execution_receipt_digest,
    candidate_cutover_revocation_receipt_digest,
    candidate_artifacts_purge_receipt_digest,
    workspace_zero_receipt_digest, rotation_receipt_chain_head_hash,
    rotation_ledger_entry_hash,
    old_domain_retirement_receipt_digest, final_domain_proof_receipt_digest,
    candidate_liveness_digest, completed_at)),
  check (receipt_digest =
    record_platform_internal.digest(canonical_receipt, 'sha256')),
  check (((mutation_kind = 'bootstrap' and inventory_receipt_digest is not null
      and ledger_primary_receipt_digest is not null
      and ledger_witness_receipt_digest is not null
      and replay_receipt_digest is not null
      and identity_set_primary_receipt_digest is not null
      and identity_set_witness_receipt_digest is not null
      and candidate_control_policy_head_generation is null
      and candidate_control_policy_head_digest is null
      and candidate_control_policy_primary_receipt_digest is null
      and candidate_control_policy_witness_receipt_digest is null
      and candidate_import_applied_receipt_digest is null
      and candidate_import_revocation_receipt_digest is null
      and candidate_cutover_execution_receipt_digest is null
      and candidate_cutover_revocation_receipt_digest is null
      and candidate_artifacts_purge_receipt_digest is null
      and rotation_cutover_bundle_digest is null
      and rotation_receipt_chain_head_hash is null
      and rotation_ledger_entry_hash is null
      and old_domain_retirement_receipt_digest is null
      and final_domain_proof_receipt_digest is null
      and candidate_liveness_digest is null)
    or (mutation_kind = 'domain_identity_rotate'
      and rotation_cutover_bundle_digest is not null
      and ledger_primary_receipt_digest is not null
      and ledger_witness_receipt_digest is not null
      and replay_receipt_digest is not null
      and identity_set_primary_receipt_digest is not null
      and identity_set_witness_receipt_digest is not null
      and candidate_control_policy_head_generation is not null
      and candidate_control_policy_head_digest is not null
      and candidate_control_policy_primary_receipt_digest is not null
      and candidate_control_policy_witness_receipt_digest is not null
      and candidate_import_applied_receipt_digest is not null
      and candidate_import_revocation_receipt_digest is not null
      and candidate_cutover_execution_receipt_digest is not null
      and candidate_cutover_revocation_receipt_digest is not null
      and candidate_artifacts_purge_receipt_digest is not null
      and rotation_receipt_chain_head_hash is not null
      and rotation_ledger_entry_hash is not null
      and old_domain_retirement_receipt_digest is not null
      and final_domain_proof_receipt_digest is not null
      and candidate_liveness_digest is not null
      and inventory_receipt_digest is null)
    or (mutation_kind not in ('bootstrap','domain_identity_rotate')
      and rotation_cutover_bundle_digest is null
      and rotation_receipt_chain_head_hash is null
      and rotation_ledger_entry_hash is null
      and old_domain_retirement_receipt_digest is null
      and final_domain_proof_receipt_digest is null
      and candidate_liveness_digest is null
      and ledger_primary_receipt_digest is null
      and ledger_witness_receipt_digest is null
      and replay_receipt_digest is null
      and identity_set_primary_receipt_digest is null
      and identity_set_witness_receipt_digest is null
      and candidate_control_policy_head_generation is null
      and candidate_control_policy_head_digest is null
      and candidate_control_policy_primary_receipt_digest is null
      and candidate_control_policy_witness_receipt_digest is null
      and candidate_import_applied_receipt_digest is null
      and candidate_import_revocation_receipt_digest is null
      and candidate_cutover_execution_receipt_digest is null
      and candidate_cutover_revocation_receipt_digest is null
      and candidate_artifacts_purge_receipt_digest is null
      and ((mutation_kind in ('retire','compromise','remove')) =
        (inventory_receipt_digest is not null)))) is true)
);

alter table public.recovery_trust_entries
  add column mutation_id text not null unique check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  add column mutation_kind text not null check (mutation_kind in
    ('bootstrap','add','rotate','retire','compromise','remove','approval_policy_rotate',
     'domain_identity_rotate')),
  add column plan_digest bytea not null check (octet_length(plan_digest) = 32),
  add column authorization_mode text not null
    check (authorization_mode in ('local_tty','detached')),
  add column authorization_artifact_digest bytea not null
    check (octet_length(authorization_artifact_digest) = 32),
  add column approval_set_digest bytea check (octet_length(approval_set_digest) = 32),
  add column current_approval_policy_digest bytea not null
    check (octet_length(current_approval_policy_digest) = 32),
  add column candidate_approval_policy_digest bytea not null
    check (octet_length(candidate_approval_policy_digest) = 32),
  add column drain_receipt_digest bytea
    check (octet_length(drain_receipt_digest) = 32),
  add column drain_scope_digest bytea
    check (octet_length(drain_scope_digest) = 32),
  add column domain_identity_set_digest bytea not null
    check (octet_length(domain_identity_set_digest) = 32),
  add column domain_identity_epoch bigint not null
    check (domain_identity_epoch > 0),
  add column mutation_bundle_digest bytea not null
    check (octet_length(mutation_bundle_digest) = 32),
  add column dependency_inventory_digest bytea
    check (octet_length(dependency_inventory_digest) = 32),
  add column rotation_current_identity_set_digest bytea
    check (octet_length(rotation_current_identity_set_digest) = 32),
  add column rotation_candidate_identity_set_digest bytea
    check (octet_length(rotation_candidate_identity_set_digest) = 32),
  add column rotation_current_identity_epoch bigint
    check (rotation_current_identity_epoch > 0),
  add column rotation_candidate_identity_epoch bigint
    check (rotation_candidate_identity_epoch > 0),
  add column current_domain_attestation_policy_digest bytea
    check (octet_length(current_domain_attestation_policy_digest) = 32),
  add column candidate_domain_attestation_policy_digest bytea
    check (octet_length(candidate_domain_attestation_policy_digest) = 32),
  add column rotation_candidate_possession_digest bytea
    check (octet_length(rotation_candidate_possession_digest) = 32),
  add column rotation_intent_bundle_digest bytea
    check (octet_length(rotation_intent_bundle_digest) = 32),
  add column rotation_cutover_bundle_digest bytea
    check (octet_length(rotation_cutover_bundle_digest) = 32),
  alter column key_id drop not null,
  alter column public_key drop not null,
  alter column active_from drop not null,
  alter column reason_code drop default,
  alter column reason_code drop not null,
  alter column inventory_digest drop not null,
  alter column status drop not null,
  drop constraint recovery_trust_entries_status_allowed,
  add constraint recovery_trust_entries_status_allowed
    check (status is null or status in ('active','retired','compromised','removed')),
  add constraint recovery_trust_entries_authorization_shape check ((
    (authorization_mode = 'local_tty' and approval_set_digest is null
      and mutation_kind in ('bootstrap','add','rotate','retire','remove'))
    or (authorization_mode = 'detached' and approval_set_digest is not null)
  ) is true),
  add constraint recovery_trust_entries_kind_shape check ((
    (mutation_kind = 'approval_policy_rotate' and key_id is null and public_key is null
      and active_from is null and retired_at is null and status is null
      and reason_code is null and inventory_digest is null and drain_receipt_digest is null
      and drain_scope_digest is null
      and dependency_inventory_digest is null
      and rotation_current_identity_set_digest is null
      and rotation_candidate_identity_set_digest is null
      and rotation_current_identity_epoch is null
      and rotation_candidate_identity_epoch is null
      and current_domain_attestation_policy_digest is null
      and candidate_domain_attestation_policy_digest is null
      and rotation_candidate_possession_digest is null
      and rotation_intent_bundle_digest is null
      and rotation_cutover_bundle_digest is null)
    or
    (mutation_kind = 'domain_identity_rotate' and authorization_mode = 'detached'
      and key_id is null and public_key is null and active_from is null
      and retired_at is null and status is null and reason_code is null
      and inventory_digest is null
      and drain_receipt_digest is not null and drain_scope_digest is not null
      and dependency_inventory_digest is null
      and rotation_current_identity_set_digest is not null
      and rotation_candidate_identity_set_digest is not null
      and rotation_current_identity_set_digest <> rotation_candidate_identity_set_digest
      and rotation_current_identity_epoch is not null
      and rotation_candidate_identity_epoch = rotation_current_identity_epoch + 1
      and domain_identity_set_digest = rotation_candidate_identity_set_digest
      and domain_identity_epoch = rotation_candidate_identity_epoch
      and candidate_approval_policy_digest = current_approval_policy_digest
      and current_domain_attestation_policy_digest is not null
      and candidate_domain_attestation_policy_digest is not null
      and rotation_candidate_possession_digest is not null
      and rotation_intent_bundle_digest is not null
      and rotation_cutover_bundle_digest is not null
      and mutation_bundle_digest = rotation_intent_bundle_digest)
    or
    (mutation_kind not in ('approval_policy_rotate','domain_identity_rotate')
      and key_id is not null and public_key is not null
      and active_from is not null and status is not null
      and reason_code = case mutation_kind
        when 'bootstrap' then 'bootstrap_activated'
        when 'add' then 'key_added'
        when 'rotate' then 'key_rotated'
        when 'retire' then 'key_retired'
        when 'compromise' then 'key_compromised'
        when 'remove' then 'key_removed'
      end
      and inventory_digest is not null
      and ((mutation_kind = 'bootstrap' and drain_receipt_digest is not null
          and drain_scope_digest is not null)
        or (mutation_kind <> 'bootstrap' and drain_receipt_digest is null
          and drain_scope_digest is null))
      and ((mutation_kind in ('retire','compromise','remove')
          and dependency_inventory_digest is not null)
        or (mutation_kind not in ('retire','compromise','remove')
          and dependency_inventory_digest is null))
      and rotation_current_identity_set_digest is null
      and rotation_candidate_identity_set_digest is null
      and rotation_current_identity_epoch is null
      and rotation_candidate_identity_epoch is null
      and current_domain_attestation_policy_digest is null
      and candidate_domain_attestation_policy_digest is null
      and rotation_candidate_possession_digest is null
      and rotation_intent_bundle_digest is null
      and rotation_cutover_bundle_digest is null)
  ) is true);

alter table public.recovery_trust_witness_entries
  add column mutation_id text not null unique check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  add column mutation_kind text not null check (mutation_kind in
    ('bootstrap','add','rotate','retire','compromise','remove','approval_policy_rotate',
     'domain_identity_rotate')),
  add column plan_digest bytea not null check (octet_length(plan_digest) = 32),
  add column authorization_mode text not null
    check (authorization_mode in ('local_tty','detached')),
  add column authorization_artifact_digest bytea not null
    check (octet_length(authorization_artifact_digest) = 32),
  add column approval_set_digest bytea check (octet_length(approval_set_digest) = 32),
  add column current_approval_policy_digest bytea not null
    check (octet_length(current_approval_policy_digest) = 32),
  add column candidate_approval_policy_digest bytea not null
    check (octet_length(candidate_approval_policy_digest) = 32),
  add column drain_receipt_digest bytea
    check (octet_length(drain_receipt_digest) = 32),
  add column drain_scope_digest bytea
    check (octet_length(drain_scope_digest) = 32),
  add column domain_identity_set_digest bytea not null
    check (octet_length(domain_identity_set_digest) = 32),
  add column domain_identity_epoch bigint not null
    check (domain_identity_epoch > 0),
  add column mutation_bundle_digest bytea not null
    check (octet_length(mutation_bundle_digest) = 32),
  add column dependency_inventory_digest bytea
    check (octet_length(dependency_inventory_digest) = 32),
  add column rotation_current_identity_set_digest bytea
    check (octet_length(rotation_current_identity_set_digest) = 32),
  add column rotation_candidate_identity_set_digest bytea
    check (octet_length(rotation_candidate_identity_set_digest) = 32),
  add column rotation_current_identity_epoch bigint
    check (rotation_current_identity_epoch > 0),
  add column rotation_candidate_identity_epoch bigint
    check (rotation_candidate_identity_epoch > 0),
  add column current_domain_attestation_policy_digest bytea
    check (octet_length(current_domain_attestation_policy_digest) = 32),
  add column candidate_domain_attestation_policy_digest bytea
    check (octet_length(candidate_domain_attestation_policy_digest) = 32),
  add column rotation_candidate_possession_digest bytea
    check (octet_length(rotation_candidate_possession_digest) = 32),
  add column rotation_intent_bundle_digest bytea
    check (octet_length(rotation_intent_bundle_digest) = 32),
  add column rotation_cutover_bundle_digest bytea
    check (octet_length(rotation_cutover_bundle_digest) = 32),
  alter column key_id drop not null,
  alter column public_key drop not null,
  alter column active_from drop not null,
  alter column reason_code drop default,
  alter column reason_code drop not null,
  alter column inventory_digest drop not null,
  alter column status drop not null,
  drop constraint recovery_trust_witness_entries_status_allowed,
  add constraint recovery_trust_witness_entries_status_allowed
    check (status is null or status in ('active','retired','compromised','removed')),
  add constraint recovery_trust_witness_entries_authorization_shape check ((
    (authorization_mode = 'local_tty' and approval_set_digest is null
      and mutation_kind in ('bootstrap','add','rotate','retire','remove'))
    or (authorization_mode = 'detached' and approval_set_digest is not null)
  ) is true),
  add constraint recovery_trust_witness_entries_kind_shape check ((
    (mutation_kind = 'approval_policy_rotate' and key_id is null and public_key is null
      and active_from is null and retired_at is null and status is null
      and reason_code is null and inventory_digest is null and drain_receipt_digest is null
      and drain_scope_digest is null
      and dependency_inventory_digest is null
      and rotation_current_identity_set_digest is null
      and rotation_candidate_identity_set_digest is null
      and rotation_current_identity_epoch is null
      and rotation_candidate_identity_epoch is null
      and current_domain_attestation_policy_digest is null
      and candidate_domain_attestation_policy_digest is null
      and rotation_candidate_possession_digest is null
      and rotation_intent_bundle_digest is null
      and rotation_cutover_bundle_digest is null)
    or
    (mutation_kind = 'domain_identity_rotate' and authorization_mode = 'detached'
      and key_id is null and public_key is null and active_from is null
      and retired_at is null and status is null and reason_code is null
      and inventory_digest is null
      and drain_receipt_digest is not null and drain_scope_digest is not null
      and dependency_inventory_digest is null
      and rotation_current_identity_set_digest is not null
      and rotation_candidate_identity_set_digest is not null
      and rotation_current_identity_set_digest <> rotation_candidate_identity_set_digest
      and rotation_current_identity_epoch is not null
      and rotation_candidate_identity_epoch = rotation_current_identity_epoch + 1
      and domain_identity_set_digest = rotation_candidate_identity_set_digest
      and domain_identity_epoch = rotation_candidate_identity_epoch
      and candidate_approval_policy_digest = current_approval_policy_digest
      and current_domain_attestation_policy_digest is not null
      and candidate_domain_attestation_policy_digest is not null
      and rotation_candidate_possession_digest is not null
      and rotation_intent_bundle_digest is not null
      and rotation_cutover_bundle_digest is not null
      and mutation_bundle_digest = rotation_intent_bundle_digest)
    or
    (mutation_kind not in ('approval_policy_rotate','domain_identity_rotate')
      and key_id is not null and public_key is not null
      and active_from is not null and status is not null
      and reason_code = case mutation_kind
        when 'bootstrap' then 'bootstrap_activated'
        when 'add' then 'key_added'
        when 'rotate' then 'key_rotated'
        when 'retire' then 'key_retired'
        when 'compromise' then 'key_compromised'
        when 'remove' then 'key_removed'
      end
      and inventory_digest is not null
      and ((mutation_kind = 'bootstrap' and drain_receipt_digest is not null
          and drain_scope_digest is not null)
        or (mutation_kind <> 'bootstrap' and drain_receipt_digest is null
          and drain_scope_digest is null))
      and ((mutation_kind in ('retire','compromise','remove')
          and dependency_inventory_digest is not null)
        or (mutation_kind not in ('retire','compromise','remove')
          and dependency_inventory_digest is null))
      and rotation_current_identity_set_digest is null
      and rotation_candidate_identity_set_digest is null
      and rotation_current_identity_epoch is null
      and rotation_candidate_identity_epoch is null
      and current_domain_attestation_policy_digest is null
      and candidate_domain_attestation_policy_digest is null
      and rotation_candidate_possession_digest is null
      and rotation_intent_bundle_digest is null
      and rotation_cutover_bundle_digest is null)
  ) is true);

alter table public.deletion_ledger_entries
  alter column actor_id drop not null,
  alter column route drop not null,
  alter column object_kind drop not null,
  alter column object_id drop not null,
  alter column deletion_request_token_commitment drop not null,
  alter column request_fingerprint drop not null,
  alter column reason_code drop not null,
  add column plan_digest bytea check (octet_length(plan_digest) = 32),
  add column authorization_artifact_digest bytea
    check (octet_length(authorization_artifact_digest) = 32),
  add column activation_bundle_digest bytea check (octet_length(activation_bundle_digest) = 32),
  add column trust_revision bigint check (trust_revision > 0),
  add column trust_head_hash bytea check (octet_length(trust_head_hash) = 32),
  add column inventory_digest bytea check (octet_length(inventory_digest) = 32),
  add column approval_policy_digest bytea check (octet_length(approval_policy_digest) = 32),
  add column adapter_policy_digest bytea check (octet_length(adapter_policy_digest) = 32),
  add column adapter_policy_generation bigint check (adapter_policy_generation > 0),
  add column current_adapter_policy_digest bytea
    check (octet_length(current_adapter_policy_digest) = 32),
  add column candidate_adapter_policy_digest bytea
    check (octet_length(candidate_adapter_policy_digest) = 32),
  add column current_adapter_policy_generation bigint
    check (current_adapter_policy_generation > 0),
  add column candidate_adapter_policy_generation bigint
    check (candidate_adapter_policy_generation > 0),
  add column drain_receipt_digest bytea check (octet_length(drain_receipt_digest) = 32),
  add column domain_identity_set_digest bytea
    check (octet_length(domain_identity_set_digest) = 32),
  add column domain_identity_epoch bigint check (domain_identity_epoch > 0),
  add column rotation_intent_bundle_digest bytea
    check (octet_length(rotation_intent_bundle_digest) = 32),
  add column rotation_cutover_bundle_digest bytea
    check (octet_length(rotation_cutover_bundle_digest) = 32),
  add column current_identity_set_digest bytea
    check (octet_length(current_identity_set_digest) = 32),
  add column candidate_identity_set_digest bytea
    check (octet_length(candidate_identity_set_digest) = 32),
  add column current_identity_set_epoch bigint check (current_identity_set_epoch > 0),
  add column candidate_identity_set_epoch bigint check (candidate_identity_set_epoch > 0),
  add column current_approval_set_digest bytea
    check (octet_length(current_approval_set_digest) = 32),
  add column candidate_possession_digest bytea
    check (octet_length(candidate_possession_digest) = 32),
  add column copy_receipt_digest bytea check (octet_length(copy_receipt_digest) = 32),
  drop constraint deletion_ledger_entries_type_allowed,
  add constraint deletion_ledger_entries_type_allowed check (entry_type in
    ('delete_commit','attempt_not_committed','contract_activation','domain_identity_rotation')),
  drop constraint deletion_ledger_entries_contract_fields_valid,
  add constraint deletion_ledger_entries_common_shape check ((
    deployment_id ~ '^dp-[0-9a-f]{64}$'
    and project_id = 'default'
    and operation_id is not null and char_length(btrim(operation_id)) > 0
    and octet_length(previous_hash) = 32 and octet_length(entry_hash) = 32
    and octet_length(canonical_entry) between 1 and 1048576
    and canonical_entry = record_platform_internal.record_platform_encode_ledger_body_v1(
      entry_version, sequence, entry_type, deployment_id, project_id, operation_id,
      actor_id, route, object_kind, object_id, origin_identity, authorization_floor,
      deletion_request_token_commitment, request_fingerprint,
      deletion_contract_version, minimum_fence_contract_version, reason_code,
      release_epoch, confirmed_at, previous_hash, plan_digest,
      authorization_artifact_digest, activation_bundle_digest, trust_revision,
      trust_head_hash, inventory_digest, approval_policy_digest,
      adapter_policy_digest, adapter_policy_generation,
      current_adapter_policy_digest, candidate_adapter_policy_digest,
      current_adapter_policy_generation, candidate_adapter_policy_generation,
      drain_receipt_digest,
      domain_identity_set_digest, domain_identity_epoch, rotation_intent_bundle_digest,
      rotation_cutover_bundle_digest, current_identity_set_digest,
      candidate_identity_set_digest, current_identity_set_epoch,
      candidate_identity_set_epoch, current_approval_set_digest,
      candidate_possession_digest, copy_receipt_digest)
    and entry_hash = record_platform_internal.digest(canonical_entry, 'sha256')
    and (authorization_floor_hash is null or
      authorization_floor_hash = record_platform_internal.digest(
        authorization_floor, 'sha256'))
  ) is true),
  add constraint deletion_ledger_entries_union_shape check ((
    (entry_type = 'contract_activation' and sequence = 1
      and plan_digest is not null and authorization_artifact_digest is not null
      and activation_bundle_digest is not null and trust_revision is not null
      and trust_head_hash is not null and inventory_digest is not null
      and approval_policy_digest is not null and drain_receipt_digest is not null
      and adapter_policy_digest is not null and adapter_policy_generation = 1
      and domain_identity_set_digest is not null and domain_identity_epoch = 1
      and minimum_fence_contract_version is not null
      and minimum_fence_contract_version > 0
      and previous_hash = decode(repeat('00', 32), 'hex')
      and actor_id is null and route is null and object_kind is null and object_id is null
      and deletion_request_token_commitment is null
      and request_fingerprint is null and origin_identity is null
      and authorization_floor is null and authorization_floor_hash is null
      and deletion_contract_version is null and reason_code is null and release_epoch is null
      and rotation_intent_bundle_digest is null and rotation_cutover_bundle_digest is null
      and current_adapter_policy_digest is null and candidate_adapter_policy_digest is null
      and current_adapter_policy_generation is null
      and candidate_adapter_policy_generation is null
      and current_identity_set_digest is null and candidate_identity_set_digest is null
      and current_identity_set_epoch is null and candidate_identity_set_epoch is null
      and current_approval_set_digest is null and candidate_possession_digest is null
      and copy_receipt_digest is null)
    or
    (entry_type in ('delete_commit','attempt_not_committed') and sequence > 1
      and previous_hash <> decode(repeat('00', 32), 'hex')
      and actor_id is not null and char_length(btrim(actor_id)) > 0
      and route in ('record_permanent_delete','source_permanent_delete')
      and object_kind is not null and object_kind in ('record','vps','monitoring_instance','target')
      and object_id is not null and char_length(btrim(object_id)) > 0
      and deletion_request_token_commitment is not null
      and request_fingerprint is not null
      and adapter_policy_digest is null and adapter_policy_generation is null
      and current_adapter_policy_digest is null and candidate_adapter_policy_digest is null
      and current_adapter_policy_generation is null
      and candidate_adapter_policy_generation is null
      and plan_digest is null and authorization_artifact_digest is null
      and activation_bundle_digest is null
      and trust_revision is null and trust_head_hash is null
      and inventory_digest is null and approval_policy_digest is null
      and drain_receipt_digest is null and domain_identity_set_digest is null
      and domain_identity_epoch is null and minimum_fence_contract_version is null
      and rotation_intent_bundle_digest is null and rotation_cutover_bundle_digest is null
      and current_identity_set_digest is null and candidate_identity_set_digest is null
      and current_identity_set_epoch is null and candidate_identity_set_epoch is null
      and current_approval_set_digest is null and candidate_possession_digest is null
      and copy_receipt_digest is null
      and ((entry_type = 'delete_commit' and deletion_contract_version is not null
          and deletion_contract_version > 0
          and reason_code in ('user_confirmed','source_removed','retention_replay')
          and release_epoch is null
          and ((object_kind in ('vps','monitoring_instance','target')
              and origin_identity is not null
              and octet_length(origin_identity) between 1 and 4096
              and authorization_floor is not null
              and authorization_floor_hash is not null)
            or (object_kind = 'record'
              and origin_identity is null
              and authorization_floor is null
              and authorization_floor_hash is null)))
        or (entry_type = 'attempt_not_committed' and deletion_contract_version is null
          and reason_code is null and release_epoch is not null and release_epoch > 0
          and origin_identity is null and authorization_floor is null
          and authorization_floor_hash is null)))
    or
    (entry_type = 'domain_identity_rotation' and sequence > 1
      and previous_hash <> decode(repeat('00', 32), 'hex')
      and plan_digest is not null and authorization_artifact_digest is not null
      and activation_bundle_digest is null and inventory_digest is null
      and approval_policy_digest is null and domain_identity_set_digest is null
      and adapter_policy_digest is null and adapter_policy_generation is null
      and domain_identity_epoch is null and trust_revision is not null
      and trust_head_hash is not null and drain_receipt_digest is not null
      and minimum_fence_contract_version is not null
      and minimum_fence_contract_version > 0
      and rotation_intent_bundle_digest is not null
      and rotation_cutover_bundle_digest is not null
      and current_identity_set_digest is not null
      and candidate_identity_set_digest is not null
      and current_identity_set_digest <> candidate_identity_set_digest
      and current_identity_set_epoch is not null
      and candidate_identity_set_epoch = current_identity_set_epoch + 1
      and current_approval_set_digest is not null
      and candidate_possession_digest is not null and copy_receipt_digest is not null
      and current_adapter_policy_digest is not null
      and candidate_adapter_policy_digest is not null
      and current_adapter_policy_generation is not null
      and candidate_adapter_policy_generation in
        (current_adapter_policy_generation, current_adapter_policy_generation + 1)
      and actor_id is null and route is null and object_kind is null and object_id is null
      and deletion_request_token_commitment is null
      and request_fingerprint is null and origin_identity is null
      and authorization_floor is null and authorization_floor_hash is null
      and deletion_contract_version is null and reason_code is null and release_epoch is null)
  ) is true);

alter table public.deletion_witness_entries
  alter column actor_id drop not null,
  alter column route drop not null,
  alter column object_kind drop not null,
  alter column object_id drop not null,
  alter column deletion_request_token_commitment drop not null,
  alter column request_fingerprint drop not null,
  alter column reason_code drop not null,
  add column plan_digest bytea check (octet_length(plan_digest) = 32),
  add column authorization_artifact_digest bytea
    check (octet_length(authorization_artifact_digest) = 32),
  add column activation_bundle_digest bytea check (octet_length(activation_bundle_digest) = 32),
  add column trust_revision bigint check (trust_revision > 0),
  add column trust_head_hash bytea check (octet_length(trust_head_hash) = 32),
  add column inventory_digest bytea check (octet_length(inventory_digest) = 32),
  add column approval_policy_digest bytea check (octet_length(approval_policy_digest) = 32),
  add column adapter_policy_digest bytea check (octet_length(adapter_policy_digest) = 32),
  add column adapter_policy_generation bigint check (adapter_policy_generation > 0),
  add column current_adapter_policy_digest bytea
    check (octet_length(current_adapter_policy_digest) = 32),
  add column candidate_adapter_policy_digest bytea
    check (octet_length(candidate_adapter_policy_digest) = 32),
  add column current_adapter_policy_generation bigint
    check (current_adapter_policy_generation > 0),
  add column candidate_adapter_policy_generation bigint
    check (candidate_adapter_policy_generation > 0),
  add column drain_receipt_digest bytea check (octet_length(drain_receipt_digest) = 32),
  add column domain_identity_set_digest bytea
    check (octet_length(domain_identity_set_digest) = 32),
  add column domain_identity_epoch bigint check (domain_identity_epoch > 0),
  add column rotation_intent_bundle_digest bytea
    check (octet_length(rotation_intent_bundle_digest) = 32),
  add column rotation_cutover_bundle_digest bytea
    check (octet_length(rotation_cutover_bundle_digest) = 32),
  add column current_identity_set_digest bytea
    check (octet_length(current_identity_set_digest) = 32),
  add column candidate_identity_set_digest bytea
    check (octet_length(candidate_identity_set_digest) = 32),
  add column current_identity_set_epoch bigint check (current_identity_set_epoch > 0),
  add column candidate_identity_set_epoch bigint check (candidate_identity_set_epoch > 0),
  add column current_approval_set_digest bytea
    check (octet_length(current_approval_set_digest) = 32),
  add column candidate_possession_digest bytea
    check (octet_length(candidate_possession_digest) = 32),
  add column copy_receipt_digest bytea check (octet_length(copy_receipt_digest) = 32),
  drop constraint deletion_witness_entries_type_allowed,
  add constraint deletion_witness_entries_type_allowed check (entry_type in
    ('delete_commit','attempt_not_committed','contract_activation','domain_identity_rotation')),
  drop constraint if exists deletion_witness_entries_contract_fields_valid,
  add constraint deletion_witness_entries_common_shape check ((
    deployment_id ~ '^dp-[0-9a-f]{64}$'
    and project_id = 'default'
    and operation_id is not null and char_length(btrim(operation_id)) > 0
    and octet_length(previous_hash) = 32 and octet_length(entry_hash) = 32
    and octet_length(canonical_entry) between 1 and 1048576
    and canonical_entry = record_platform_internal.record_platform_encode_ledger_body_v1(
      entry_version, sequence, entry_type, deployment_id, project_id, operation_id,
      actor_id, route, object_kind, object_id, origin_identity, authorization_floor,
      deletion_request_token_commitment, request_fingerprint,
      deletion_contract_version, minimum_fence_contract_version, reason_code,
      release_epoch, confirmed_at, previous_hash, plan_digest,
      authorization_artifact_digest, activation_bundle_digest, trust_revision,
      trust_head_hash, inventory_digest, approval_policy_digest,
      adapter_policy_digest, adapter_policy_generation,
      current_adapter_policy_digest, candidate_adapter_policy_digest,
      current_adapter_policy_generation, candidate_adapter_policy_generation,
      drain_receipt_digest,
      domain_identity_set_digest, domain_identity_epoch, rotation_intent_bundle_digest,
      rotation_cutover_bundle_digest, current_identity_set_digest,
      candidate_identity_set_digest, current_identity_set_epoch,
      candidate_identity_set_epoch, current_approval_set_digest,
      candidate_possession_digest, copy_receipt_digest)
    and entry_hash = record_platform_internal.digest(canonical_entry, 'sha256')
    and (authorization_floor_hash is null or
      authorization_floor_hash = record_platform_internal.digest(
        authorization_floor, 'sha256'))
  ) is true),
  add constraint deletion_witness_entries_union_shape check ((
    (entry_type = 'contract_activation' and sequence = 1
      and plan_digest is not null and authorization_artifact_digest is not null
      and activation_bundle_digest is not null and trust_revision is not null
      and trust_head_hash is not null and inventory_digest is not null
      and approval_policy_digest is not null and drain_receipt_digest is not null
      and adapter_policy_digest is not null and adapter_policy_generation = 1
      and domain_identity_set_digest is not null and domain_identity_epoch = 1
      and minimum_fence_contract_version is not null
      and minimum_fence_contract_version > 0
      and previous_hash = decode(repeat('00', 32), 'hex')
      and actor_id is null and route is null and object_kind is null and object_id is null
      and deletion_request_token_commitment is null
      and request_fingerprint is null and origin_identity is null
      and authorization_floor is null and authorization_floor_hash is null
      and deletion_contract_version is null and reason_code is null and release_epoch is null
      and rotation_intent_bundle_digest is null and rotation_cutover_bundle_digest is null
      and current_adapter_policy_digest is null and candidate_adapter_policy_digest is null
      and current_adapter_policy_generation is null
      and candidate_adapter_policy_generation is null
      and current_identity_set_digest is null and candidate_identity_set_digest is null
      and current_identity_set_epoch is null and candidate_identity_set_epoch is null
      and current_approval_set_digest is null and candidate_possession_digest is null
      and copy_receipt_digest is null)
    or
    (entry_type in ('delete_commit','attempt_not_committed') and sequence > 1
      and previous_hash <> decode(repeat('00', 32), 'hex')
      and actor_id is not null and char_length(btrim(actor_id)) > 0
      and route in ('record_permanent_delete','source_permanent_delete')
      and object_kind is not null and object_kind in ('record','vps','monitoring_instance','target')
      and object_id is not null and char_length(btrim(object_id)) > 0
      and deletion_request_token_commitment is not null
      and request_fingerprint is not null
      and adapter_policy_digest is null and adapter_policy_generation is null
      and current_adapter_policy_digest is null and candidate_adapter_policy_digest is null
      and current_adapter_policy_generation is null
      and candidate_adapter_policy_generation is null
      and plan_digest is null and authorization_artifact_digest is null
      and activation_bundle_digest is null
      and trust_revision is null and trust_head_hash is null
      and inventory_digest is null and approval_policy_digest is null
      and drain_receipt_digest is null and domain_identity_set_digest is null
      and domain_identity_epoch is null and minimum_fence_contract_version is null
      and rotation_intent_bundle_digest is null and rotation_cutover_bundle_digest is null
      and current_identity_set_digest is null and candidate_identity_set_digest is null
      and current_identity_set_epoch is null and candidate_identity_set_epoch is null
      and current_approval_set_digest is null and candidate_possession_digest is null
      and copy_receipt_digest is null
      and ((entry_type = 'delete_commit' and deletion_contract_version is not null
          and deletion_contract_version > 0
          and reason_code in ('user_confirmed','source_removed','retention_replay')
          and release_epoch is null
          and ((object_kind in ('vps','monitoring_instance','target')
              and origin_identity is not null
              and octet_length(origin_identity) between 1 and 4096
              and authorization_floor is not null
              and authorization_floor_hash is not null)
            or (object_kind = 'record'
              and origin_identity is null
              and authorization_floor is null
              and authorization_floor_hash is null)))
        or (entry_type = 'attempt_not_committed' and deletion_contract_version is null
          and reason_code is null and release_epoch is not null and release_epoch > 0
          and origin_identity is null and authorization_floor is null
          and authorization_floor_hash is null)))
    or
    (entry_type = 'domain_identity_rotation' and sequence > 1
      and previous_hash <> decode(repeat('00', 32), 'hex')
      and plan_digest is not null and authorization_artifact_digest is not null
      and activation_bundle_digest is null and inventory_digest is null
      and approval_policy_digest is null and domain_identity_set_digest is null
      and adapter_policy_digest is null and adapter_policy_generation is null
      and domain_identity_epoch is null and trust_revision is not null
      and trust_head_hash is not null and drain_receipt_digest is not null
      and minimum_fence_contract_version is not null
      and minimum_fence_contract_version > 0
      and rotation_intent_bundle_digest is not null
      and rotation_cutover_bundle_digest is not null
      and current_identity_set_digest is not null
      and candidate_identity_set_digest is not null
      and current_identity_set_digest <> candidate_identity_set_digest
      and current_identity_set_epoch is not null
      and candidate_identity_set_epoch = current_identity_set_epoch + 1
      and current_approval_set_digest is not null
      and candidate_possession_digest is not null and copy_receipt_digest is not null
      and current_adapter_policy_digest is not null
      and candidate_adapter_policy_digest is not null
      and current_adapter_policy_generation is not null
      and candidate_adapter_policy_generation in
        (current_adapter_policy_generation, current_adapter_policy_generation + 1)
      and actor_id is null and route is null and object_kind is null and object_id is null
      and deletion_request_token_commitment is null
      and request_fingerprint is null and origin_identity is null
      and authorization_floor is null and authorization_floor_hash is null
      and deletion_contract_version is null and reason_code is null and release_epoch is null)
  ) is true);

alter table public.deployment_contract_state
  add column activation_mutation_id text not null,
  add column activation_plan_digest bytea not null
    check (octet_length(activation_plan_digest) = 32),
  add column activation_authorization_artifact_digest bytea not null
    check (octet_length(activation_authorization_artifact_digest) = 32),
  add column activation_bundle_digest bytea not null
    check (octet_length(activation_bundle_digest) = 32),
  add column trust_revision bigint not null check (trust_revision > 0),
  add column trust_head_hash bytea not null check (octet_length(trust_head_hash) = 32),
  add column inventory_digest bytea not null check (octet_length(inventory_digest) = 32),
  add column approval_policy_digest bytea not null
    check (octet_length(approval_policy_digest) = 32),
  add column activation_adapter_policy_digest bytea not null
    check (octet_length(activation_adapter_policy_digest) = 32),
  add column activation_adapter_policy_generation bigint not null
    check (activation_adapter_policy_generation = 1),
  add column active_adapter_policy_digest bytea not null
    check (octet_length(active_adapter_policy_digest) = 32),
  add column active_adapter_policy_generation bigint not null
    check (active_adapter_policy_generation >= activation_adapter_policy_generation),
  add column drain_receipt_digest bytea not null
    check (octet_length(drain_receipt_digest) = 32),
  add column activation_domain_identity_set_digest bytea not null
    check (octet_length(activation_domain_identity_set_digest) = 32),
  add column activation_domain_identity_epoch bigint not null
    check (activation_domain_identity_epoch = 1),
  add column active_domain_identity_set_digest bytea not null
    check (octet_length(active_domain_identity_set_digest) = 32),
  add column active_domain_identity_epoch bigint not null
    check (active_domain_identity_epoch >= activation_domain_identity_epoch),
  add column last_domain_identity_sequence bigint not null
    check (last_domain_identity_sequence >= 1),
  add column last_domain_identity_entry_hash bytea not null
    check (octet_length(last_domain_identity_entry_hash) = 32),
  add constraint deployment_contract_state_activation_sequence_one
    check (activation_sequence = 1),
  add constraint deployment_contract_state_initial_identity_match check ((
    (active_domain_identity_epoch = 1
      and active_domain_identity_set_digest = activation_domain_identity_set_digest
      and last_domain_identity_sequence = activation_sequence)
    or (active_domain_identity_epoch > 1
      and last_domain_identity_sequence > activation_sequence)
  ) is true),
  add constraint deployment_contract_state_adapter_policy_progress check ((
    (active_domain_identity_epoch = 1
      and active_adapter_policy_generation = activation_adapter_policy_generation
      and active_adapter_policy_digest = activation_adapter_policy_digest)
    or (active_domain_identity_epoch > 1
      and active_adapter_policy_generation >= activation_adapter_policy_generation)
  ) is true);
```

- [ ] **Step 3: Verify RED.** Run `go test ./internal/center/recovery ./internal/center/deletionledger ./internal/center/store/migrate -run 'Canonical|Trust|Activation|RecordPlatformMigration' -count=1`. Expected: missing fields/tables and non-idempotent trust retry failures.

- [ ] **Step 4: Implement strict v1 canonical shapes and database byte binding.** Branch validation by mutation kind: key mutations require key/public/status/reason/inventory fields; policy rotation and domain identity rotation are keyless and require `key_id/public_key/active_from/retired_at/status/reason_code/inventory_digest` all null. `TrustReasonCodeV1` is a closed one-to-one action map (`bootstrap_activated|key_added|key_rotated|key_retired|key_compromised|key_removed`); deletion request routes are only `record_permanent_delete|source_permanent_delete`, and deletion reasons are only `user_confirmed|source_removed|retention_replay`. Go, primary PostgreSQL, PostgreSQL witness and S3 canonical decoders reject every other value instead of accepting an arbitrary non-empty string. Policy rotation requires current/candidate approval-policy digests. Domain rotation is detached-only, repeats witnessed current approval policy in the candidate-policy column, and binds the generic `mutation_bundle_digest` exactly to `rotation_intent_bundle_digest`; it cannot carry an uncommitted third policy or bundle value. Bootstrap uses the all-zero current-policy digest and the plan policy as candidate; ordinary key mutations repeat the witnessed policy digest in both fields; policy rotation uses distinct witnessed-current and proposed-candidate digests. `removed` is terminal. A bootstrap `MutationBundle` contains exactly one each of trust pre-entry body, candidate approval policy, domain-attestation policy, admission drain receipt body, activation inventory body, signed inventory, activation manifest and contract-activation pre-entry body in enum order; it contains neither plan/authorization digest nor final trust/ledger head. The pre-entry contract body is the only serialized copy of the stable identity-set body and excludes `TrustHeadHash`. Hash full raw plan, then canonical authorization artifact, then derive final trust and ledger bodies binding plan/bundle/authorization digests and compute wrapper hashes.

  Implement `record_platform_internal.record_platform_encode_ledger_body_v1` in both ledger migrations with byte-identical source and fixed `search_path=pg_catalog`: unsigned integers use big-endian network order, times use signed UTC microseconds, enum/string/optional bytes use one-byte presence plus unsigned 32-bit length, lists are sorted/unique and length-prefixed, and no body includes `entry_hash`. Implement `record_platform_internal.record_platform_encode_mutation_completion_receipt_v1` with the same primitives so every typed receipt column, including identity-set primary/witness digests and the final rotation receipt-chain head, reproduces `canonical_receipt`; no opaque extra field is accepted. Recovery-control primary insertion and PostgreSQL full-witness confirmation both call that same typed completion encoder and mutation-kind union validator before accepting bytes; a generic witness-artifact insert cannot bypass either check. `platform_mutation_plan_v1_matches`, `mutation_authorization_artifact_v1_matches`, `activation_inventory_v1_matches` and `mutation_bundle_v1_matches` dispatch every primary canonical object through the same bounded production decoders used by full witness, compare every typed column and require the stored digest to equal SHA-256 of the exact canonical bytes. `mutation_witness_artifact_v1_matches` performs the same dispatch for the full witness. Define one production `ValidateDomainRotationIntentV1(canonicalBytes, expectedContext)` and require plan-before-output, apply-before-primary, transfer import before object commit, every resume reconstruction, primary SQL match adapter, PostgreSQL witness confirm/readback adapter and S3 put/readback adapter to call it. It bounded-decodes and byte-identical re-encodes the whole intent; validates all nested body/wrapper digests, closed purposes, complete policy bytes/digests, Ed25519 signatures, thresholds, sorted/unique signer sets, candidate-only key possession and challenge/preparation expiry-at-intent; then exact-matches deployment/project/profile/target, mutation/plan context, current/candidate set bytes+digests, adjacent global/member epochs and unchanged members. A digest-only wrapper or syntax-only decoder is invalid. The SQL narrow functions additionally re-encode typed columns and exact-match the validator-approved canonical digest; no generic function exists. The S3 writer/readback path calls the identical validator before accepting or confirming an object. Runtime/admin `SECURITY DEFINER` functions each accept exactly one bounded canonical `bytea` command envelope, exhaustively decode expected head/sequence, canonical body and complete typed payload, call the same encoder, lock head, reject mismatch, insert and CAS in one transaction; no overload exists and no role receives direct table/head write. For `domain_identity_rotation`, the service first proves the plan's witnessed `(sequence, hash, effective minimum)` by a full genesis-to-tail read. Primary SQL then atomically validates only the primary's own expected head and stored effective minimum; PostgreSQL witness confirmation independently locks and validates the actual witness head and witnessed effective minimum, while S3 derives that value from the continuous immutable entry/receipt chain. Effective minimum is the monotonic maximum of every `contract_activation|domain_identity_rotation` minimum in that chain; `delete_commit|attempt_not_committed` carry no minimum and never reset it. Concurrent higher fences, stale tuples and lower proposed minima fail. Each required nullable field has explicit `IS NOT NULL`, the entire union CHECK ends `IS TRUE`, and primary/witness source is checksum-compared. The witness compatibility drop uses `IF EXISTS` because its original fresh 0001 schema never defined the primary-only `deletion_*_contract_fields_valid` name; catalog tests still require exactly one new `*_common_shape` and one `*_union_shape` on each table and reject any surviving legacy shape constraint. Contract activation is the only sequence 1/zero-previous entry and epoch 1. Delete/outcome/identity-rotation are sequence >1; v1 object kinds are exactly `record|vps|monitoring_instance|target`, source kinds require paired floor bytes/hash including `VisibilityKind`, and unknown kinds fail. S3 decoder additionally binds role prefix, object-key sequence and immutable receipt sequence. `DomainIdentityRotationPreEntryPayload` is golden-tested as the only rotation pre-entry shape: it contains known plan/auth/intent/set/approval/possession/copy/drain/minimum fields and forbids cutover bundle digest, resulting trust revision/head, ledger sequence/current hash and future receipts. Run one shared golden corpus through every `ValidateDomainRotationIntentV1` call site plus primary function+CHECK: accept exact bytes, reject single-byte nested mutation, wrong purpose/policy/threshold/signature/order/possession/epoch/domain/mutation, trailing bytes and backend parity drift. Any open-enum acceptance or minimum-fence regression fails.

- [ ] **Step 5: Make trust primary ack-loss idempotent.** Before transition validation, lock the trust head and query by `mutation_id`. If an immutable canonical entry exists, verify the complete primary chain and return the original committed result only when `plan_digest` and every canonical byte match; otherwise return a stable conflict. Do not treat retrying the same active key as a new state transition.

- [ ] **Step 6: Implement additive pre-release migrations, stable domain identity, canonical attestation and versioned role grants.** Because these `0001`/`0051` files have not shipped, update their canonical checksums and fresh schemas directly. Install `pgcrypto` only in `record_platform_internal`, require `pg_extension.extnamespace` to exact-match that schema, and revoke schema/function EXECUTE from PUBLIC; never silently move an existing extension. `ProvisionDomainIdentity` reads database OID/name, expected kind and preferably `pg_control_system().system_identifier`, generates a random domain ID/member epoch 1, and exact-matches that physical domain thereafter; a new domain may be provisioned only as inactive candidate for Task 17. External mode parses the exact `HOUFENG-RECORD-PLATFORM-DOMAIN-ATTESTATION-POLICY-V1` threshold/key format with the strict 0400 regular/no-follow ≤64 KiB loader and the signature-set `SignedDomainAttestationV1` defined in design §4. Verify magic/version/field count, NFC/bounds, UTC microseconds, purpose, deployment/project/profile/kind/domain/member+set epoch, physical identity fields, stable digest, generation, challenge, validity, policy digest and the sorted unique threshold signatures. Renewal appends only when witnessed policy/stable digest/kind match and generation increases; it never changes identity-set bytes. Candidate policy rotation requires current+candidate thresholds and candidate-only possession. S3 uses the read-control-only principal for TLS/SPKI/bucket/Object-Lock probes and candidate locked-nonce proof; it cannot read witness bodies or PUT normal witness prefixes. Pairwise checks reject alias/system/stable digest and backup/log/workspace/replica overlap.

  `schema_migrations` converges to `name text primary key, checksum text not null check (checksum ~ '^[0-9a-f]{64}$'), applied_at timestamptz not null default now()`. The 0.59 baseline has 51 name-only rows through 0050; checksum adoption backfills only exact embedded names, preserves `applied_at`, rejects unknown names, and sets CHECK/NOT NULL inside the locked genesis transaction. `ProvisionRoles` creates no roles: validate quoted pre-created `NOINHERIT` names, reject owner/migrator membership or reuse, revoke PUBLIC/schema CREATE, and apply initial `app_acl_manifest_r000001` plus domain-specific manifests.

  Canonical V1 is frozen. All integers are unsigned big-endian within the positive PostgreSQL bigint range；strings are NFC UTF-8 with u32 byte lengths；lists have u32 counts；decoders reject trailing bytes. `canonical_migration_set` starts with `HOUFENG-APP-MIGRATION-SET-V1`, followed by unique entries sorted by raw full filename bytes. Each entry is `u32 filename_len | filename | 32 raw SHA-256 bytes`；filename is a 1…255-byte `.sql` basename with no NUL or slash. It is the complete applied ledger after that transaction, every entry must exact-match the embedded map, and after the last pending migration it equals the complete embedded map. `canonical_privilege_set` starts with `HOUFENG-APP-PRIVILEGE-SET-V1`, binds semantic subjects `center_runtime|platform_admin` to distinct 1…63-byte catalog role names, then stores sorted unique tuples `(subject, object_class, schema_name, object_identity, column_name, privilege_kind, grant_option)`. Object classes are current database、schema、table、view、column、sequence and function；privileges are `CONNECT|USAGE|SELECT|INSERT|UPDATE|DELETE|EXECUTE`；grant option is always false. Class encoding is disjoint: database uses empty `schema_name|column_name` plus the canonical current database token；schema uses empty `schema_name|column_name` plus `object_identity=public`；table/view/sequence use `schema_name=public`, the exact bare catalog object name in `object_identity` and empty `column_name`；column uses `schema_name=public`, its relation name in `object_identity` and the exact attribute name in `column_name`；function uses empty `schema_name|column_name` and one complete `object_identity=public.name(pg_get_function_identity_arguments)` such as `public.record_platform_append_runtime_entry(bytea)`. Function schema/name/arguments are never serialized in separate fields, and a bare name、OID、separate `bytea` argument field or alternate spelling is invalid. V1 has zero positive column grants, but verifier must still prove `attacl` contributes none. For r1, `migrator_catalog_role` is a required length-prefixed persisted field after revision and before previous digest; it is not a third privilege subject. The manifest digest preimage is exactly that SQL CHECK field order；`recorded_at/updated_at` are excluded.

  There are exactly two transaction paths. For a fresh genesis, the scoped migrator takes the named advisory lock and applies the exact 52-file 0001…0051 baseline in one SERIALIZABLE transaction, or adopts an already-applied exact 52-file flags-off schema; it then locks the newly created null head, validates/backfills the ledger, converges catalog ACL, appends r1 with zero previous digest and CASes head. Adoption is allowed only while head is null and schema/ledger/checksum/catalog all exact-match; it is the sole migration-without-revision exception. After r1, the legacy off/off owner runner refuses every new APP filename. For every later pending migration the scoped migrator precomputes the embedded map and privilege allowlist, begins SERIALIZABLE, takes `pg_advisory_xact_lock(hashtextextended('houfeng-app-schema-acl-v1',0))`, locks `schema_migrations` in SHARE ROW EXCLUSIVE mode and the singleton head `FOR UPDATE`, rejects unknown/malformed/changed ledger rows, executes exactly one migration, inserts its filename/checksum, rebuilds the complete sorted applied set, converges grants/revokes, re-reads catalog for exact equality, inserts `head+1` with the old digest, CAS-updates both head columns using `IS NOT DISTINCT FROM`, revalidates chain/head/catalog and commits. Failures after migration SQL, ledger insert, ACL convergence, revision insert or CAS roll back all five surfaces. A repeat with no missing migration and identical chain/catalog is read-only and does not change revision or timestamps. ACL drift alone may be converged back to the already-recorded privilege bytes without a new revision; migration/manifest/head drift is never repaired in place. A release containing 0056 before 0055 creates r2; a later embedded map adding 0055 creates r3 whose sorted set places 0055 before 0056 and whose previous digest is r2.

  Grant database `CONNECT`, schema `USAGE`, and only these surfaces. In this human-readable allowlist, every bare relation/view/sequence token is display shorthand for the single canonical tuple with `schema_name=public` and that token as `object_identity`；it is not an alternate unqualified identity. Every function is written only as its complete canonical `public.name(bytea)` `object_identity`:

```text
app center-runtime:
  database CONNECT; schema public USAGE;
  SELECT schema_migrations, app_acl_manifest_revisions, app_acl_manifest_head;
  0001–0050 SELECT/INSERT/UPDATE/DELETE:
    active_incidents, asset_decision_manual_group_members,
    monitoring_instance_host_sample_daily_aggregates,
    target_probe_daily_aggregates, monitoring_instances, ip_quality_reports,
    probe_items, sessions;
  0001–0050 SELECT/INSERT/UPDATE:
    asset_decision_manual_groups, asset_decision_record_members,
    asset_decision_records, asset_decision_scenario_templates, center_settings,
    providers, subscription_budgets, subscription_exchange_rates,
    subscription_monthly_budgets, subscription_reminder_deliveries,
    subscriptions, targets, users, vps_assets, vps_monitoring_instance_links;
  0001–0050 SELECT/INSERT/DELETE:
    asset_lifecycle_action_steps, host_samples, monitoring_instance_heartbeats,
    notification_records, probe_observations, state_change_events;
  0001–0050 SELECT/INSERT:
    asset_decision_scenario_template_members, asset_domains, asset_services,
    experience_logs, ip_histories, ip_quality_provider_results,
    ip_quality_service_unlocks, monitoring_instance_command_action_audit,
    price_histories, renewal_decisions, vps_spec_snapshots;
  0001–0050 INSERT only: agent_sync_batches, asset_lifecycle_actions;
  views SELECT only: asset_decision_records_with_counts,
    ip_quality_assigned_vps_reports, ip_quality_latest_vps_summaries;
  sequences USAGE only: node_heartbeats_id_seq, host_samples_id_seq,
    probe_observations_id_seq;
  0051 SELECT/INSERT/UPDATE/DELETE:
    record_outbox, record_idempotency_keys, identity_mutation_guards,
    deletion_reservations, deletion_fence_leases, object_content_leases,
    client_content_leases, content_delivery_epochs, backup_epochs,
    recovery_inventory_projection, deployment_membership;
  0051 SELECT/INSERT/UPDATE: record_purge_operations, deletion_replay_state;
  0051 SELECT/INSERT: record_deletion_audits, source_deletion_tombstones;
  0051 SELECT only: record_access_groups, record_access_group_members,
    record_platform_domain_identity, record_platform_domain_attestations,
    deployment_contract_state;
  sequence USAGE only: record_outbox_outbox_row_id_seq;
  persistent application-function EXECUTE set: empty;
  the two projector definitions are migrator-owned verifier objects reserved for a future admitted caller
app platform-admin:
  database CONNECT; schema public USAGE;
  SELECT schema_migrations, app_acl_manifest_revisions, app_acl_manifest_head,
    record_platform_domain_identity,
    record_platform_domain_attestations, backup_epochs,
    recovery_inventory_projection, deletion_replay_state,
    deployment_membership, deployment_contract_state;
  INSERT/UPDATE deletion_replay_state;
  persistent application-function EXECUTE set: empty;
  no direct deployment_contract_state write, 0001–0050 business-table access
    or sequence USAGE
deletion-ledger center-runtime:
  SELECT schema_migrations, record_platform_domain_identity,
    record_platform_domain_attestations, key metadata,
    head, entries, append-owner; INSERT/UPDATE/DELETE append-owner;
  EXECUTE `public.record_platform_append_runtime_entry(bytea)` only
deletion-ledger platform-admin:
  SELECT schema_migrations, record_platform_domain_identity,
    record_platform_domain_attestations, key metadata,
  head, entries; EXECUTE `public.record_platform_append_contract_activation(bytea)`,
    `public.record_platform_append_domain_identity_rotation(bytea)` only
full-witness center-runtime:
  SELECT schema_migrations, record_platform_domain_identity,
    record_platform_domain_attestations, deletion/trust
    entries and heads, mutation witness artifacts;
  EXECUTE `public.record_platform_confirm_runtime_entry(bytea)` only
full-witness platform-admin:
  same SELECT plus domain identity sets, rotations and receipt chains;
  EXECUTE `public.record_platform_confirm_contract_activation(bytea)`,
    `public.record_platform_confirm_domain_identity_rotation(bytea)`,
    `public.record_platform_append_trust_witness(bytea)`,
    `public.record_platform_store_mutation_witness_artifact(bytea)`,
    `public.record_platform_store_mutation_completion_receipt_witness(bytea)`,
    `public.record_platform_store_domain_identity_set_witness(bytea)`,
    `public.record_platform_store_domain_rotation_receipt_witness(bytea)`,
    `public.record_platform_confirm_candidate_control_policy_v1(bytea)`,
    `public.record_platform_confirm_candidate_control_challenge_v1(bytea)`,
    `public.record_platform_confirm_candidate_recovery_request_v1(bytea)`,
    `public.record_platform_confirm_candidate_control_abandon_authorization_v1(bytea)`,
    `public.record_platform_confirm_candidate_control_abandon_completion_v1(bytea)` only
recovery-control center-runtime:
  SELECT schema_migrations, record_platform_domain_identity,
    record_platform_domain_attestations, S3 witness identity/attestations,
    trust entries/head, a bounded mutation-completion runtime view,
    manifests, inventory,
    attempts, workspaces and receipts;
  INSERT recovery_point_manifests, recovery_inventory, backup_attempts,
    backup_workspaces, restore_attempts, restore_workspaces,
    recovery_purge_receipts; UPDATE attempts/workspaces;
  DELETE backup_workspaces, restore_workspaces only after purge receipt;
  EXECUTE `public.record_platform_compact_completed_mutation_details(bytea)` only
recovery-control platform-admin:
  SELECT schema_migrations, domain identities/attestations, trust entries/head,
    mutation root/plan/authorization/bundle/completion, activation inventory,
    manifests/inventory, attempts/workspaces/receipts and identity rotations;
  INSERT recovery_point_manifests, recovery_inventory;
  EXECUTE `public.record_platform_begin_trust_mutation(bytea)`,
    `public.record_platform_advance_trust_mutation(bytea)`,
    `public.record_platform_store_domain_identity_set_primary(bytea)`,
    `public.record_platform_store_domain_rotation_receipt_primary(bytea)`,
    `public.record_platform_store_mutation_completion_receipt_primary(bytea)`,
    `public.record_platform_append_candidate_control_policy_v1(bytea)`,
    `public.record_platform_start_candidate_control_challenge_v1(bytea)`,
    `public.record_platform_ack_candidate_control_challenge_v1(bytea)`,
    `public.record_platform_append_candidate_recovery_request_v1(bytea)`,
    `public.record_platform_bind_candidate_control_intent_v1(bytea)`,
    `public.record_platform_reserve_candidate_control_abandon_v1(bytea)`,
    `public.record_platform_complete_candidate_control_abandon_v1(bytea)` only for
    mutation/bundle/trust-head/typed-receipt writes
```

Every authorizable persistent mutator has exactly one SQL argument: a bounded canonical `bytea` command envelope. The definer function exhaustively decodes that envelope, rejects trailing bytes, then derives all expected head/sequence, canonical body, typed payload, timestamps and CAS values internally；no caller-visible overload or multi-argument convenience form exists. For `object_class=function`, the canonical privilege tuple stores the one fully qualified `object_identity` string such as `public.record_platform_append_runtime_entry(bytea)` and requires `schema_name|column_name` empty；bare names, OIDs, separately encoded identity arguments, omitted identity arguments and extra overloads are invalid. For table/view/sequence/column classes the canonical `schema_name=public` field is authoritative, so the display shorthand above cannot create a second identity encoding.

Runtime verification is entirely read-only. It requires `current_user` to equal the latest manifest's `center_runtime` binding; requires `schema_migrations` to equal the embedded filename/checksum map; reads all revisions plus head and proves a non-null head, exactly r1…head with no gap/far tail, r1 previous zero, each later previous digest equal to rN-1, and every canonical set/digest/manifest recomputable. The latest migration set must equal both ledger and embedded map; the latest privilege set must equal the compiled semantic allowlist. Catalog verification builds effective privileges from database/schema/relation/sequence/function ACLs, `PUBLIC`, recursive memberships, ownership, `pg_attribute.attacl` and `pg_default_acl`, and requires exact equality plus `LOGIN,NOINHERIT,NOSUPERUSER,NOCREATEDB,NOCREATEROLE,NOREPLICATION,NOBYPASSRLS`, no owner/migrator reuse or membership, no database TEMP, schema CREATE, grant option or default privilege. For every manifest function it joins `pg_namespace`/`pg_proc` and requires schema exactly `public`, `pg_get_function_identity_arguments(oid)='bytea'`, exact migrator-owner OID, `prosecdef=true`, `prokind='f'`, `proconfig=ARRAY['search_path=pg_catalog']`, exact explicit `proacl` with no `PUBLIC` or grant option, zero matching `pg_default_acl` privilege and exactly one row for that schema/name; any extra overload is a catalog delta. Pending/unknown/tampered migrations, null/rollback/far-tail head, parser errors and any ACL delta keep records admission unavailable and never invoke migration code. `node_heartbeats_id_seq` intentionally retains its 0001 name after the 0029 table rename.

The migrator-owner functions use exact `SET search_path=pg_catalog`, schema-qualify every referenced object, decode one canonical `bytea` command, validate typed fields plus canonical bytes/digests and expected head/sequence in one transaction, and are `REVOKE ALL ... FROM PUBLIC`; callers receive only the listed fully qualified `EXECUTE`. Identity-set publication is an ordered protocol, not two unrelated inserts: the recovery-control function locks epoch N-1, inserts/exact-matches set N and its typed primary receipt atomically; only that receipt authorizes the full-witness function to insert/exact-match the byte-identical set, a canonical copy of the complete primary receipt plus its digest, and the typed witness receipt that binds that digest. The caller must then read back five physical artifacts—primary set, primary receipt, witness set, witness primary-receipt copy and witness receipt—before either receipt digest can enter a completion receipt. Candidate-policy and abandon authorization/completion follow the same primary body+typed-primary-receipt → witness body+canonical-primary-copy+typed-witness-receipt 2+3 rule through the exact functions listed above. Ack loss retries exact-match mutation ID, epoch/generation/fence, canonical bodies and receipt bytes; any mismatch or far tail fails. APP activation/rotation projector functions are the only persistent APP functions center-runtime may execute: they lock `deployment_contract_state` and require the witnessed ledger sequence/hash, current active identity-set digest/epoch and exact next values before CAS. Platform-admin never calls them; its saga waits for the runtime projector's witnessed receipt. Neither role has direct table write. Neither runtime nor platform-admin receives `TEMP`, direct immutable-entry/artifact/detail `INSERT`, head `UPDATE`, table ownership, CREATE, ALTER, DROP, TRUNCATE, REFERENCES, TRIGGER, trigger control, role membership, default privileges or blanket schema/sequence privileges. All four abandon artifact tables and all stable identity/attestation tables have explicit statement-level UPDATE/DELETE/TRUNCATE rejection triggers in addition to ACL denial. Mutation plan/authorization/bundle/completion and approval-detail rows are insert-or-exact-match immutable; attempts are append-only except a narrow one-time `finished_at` transition. `public.record_platform_compact_completed_mutation_details(bytea)` decodes `mutation_id`, `expected_plan_digest` and `observed_at`, locks the root, requires state complete, exact immutable completion receipt, `observed_at >= completed_at+30d`, kind-specific ledger/replay/inventory receipts and zero active workspace, then only deletes attempt/parsed-approval detail and clears `last_error_code`; it never deletes canonical plan/authorization/bundle/trust/ledger/witness/completion bytes. Real-role tests prove direct `deployment_contract_state` or immutable writes/read-of-approval-details by runtime/admin fail, runtime immutable activation/rotation/trust mutation calls are 0 while exact projection CAS succeeds, admin delete/outcome/projection calls are 0, wrong canonical/head/epoch projection calls fail, and UPDATE/DELETE/TRUNCATE/ALTER/DISABLE TRIGGER remain denied on each abandon table and every other immutable table.

For `s3_worm`, shared GET/LIST may overlap but write authority may not: center writes only `ledger/runtime/` and `heads/runtime/`; admin writes only `ledger/activation/`, `ledger/rotation/`, `heads/activation/`, `heads/rotation/`, `trust/`, `trust-heads/`, `plans/`, `authorizations/`, `bundles/`, `completion/`, `identity-sets/` and `rotations/`. Under each current/candidate target domain's canonical `record-platform/v1/<scope_hash>/` WORM root, identity sets use immutable `identity-sets/<20-digit-epoch>/<set|primary-receipt|witness-receipt>` keys; activation epoch 1 and rotation N+1 share this namespace and no mutable head key. A third migrator identity may only read current bucket/versioning/Object-Lock/retention identity and cannot PUT any normal witness prefix or read witness object bodies. Task 17 uses separate candidate prepare/import/cutover/cleanup identities with paired least-privilege arms: one arm can write/read back only the mutation's immutable imported evidence in a physically distinct candidate target WORM root, while the other can write/read/purge only the matching `candidate-control/v1/<scope_hash>/` phase prefix. Target/control/current/backup identities, prefixes and credentials are pairwise non-overlapping; control has no default retention/hold and target cannot be deleted or weakened. Each head/set receipt is immutable, sequence/epoch-addressed, retained and held like its entry. Policies prove every runtime/admin/candidate target/control cross-write, overwrite, delete, legal-hold removal and retention reduction fails, and migrator witness-object write/read-body counts are 0.

- [ ] **Step 7: Verify GREEN for both exact profiles.** Add golden/unit selectors `TestAppACLCanonicalMigrationSetBodyV1`, `TestAppACLCanonicalPrivilegeSetBodyV1` and `TestAppACLManifestPersistedV1`, rejecting duplicate/noncanonical entries, bad filename/checksum, oversize, trailing bytes, unknown enum/version and sibling digest mismatch against the PostgreSQL CHECK. Real PostgreSQL selectors are `TestPostgresIntegrationAppACLManifestFresh` (22 0051 tables, 52 migration entries, r1/head, zero previous digest, SELECT-only manifest access and login/VPS/monitoring/IP/subscription/records smoke), `...UpgradeFromV059` (51 name-only rows, checksum/timestamp-safe adoption, both flags-off-before-0051 and flags-off-after-0051 paths), `...RepeatIsReadOnly`, `...TransactionRollback` at all five failpoints, `...RejectsTamper` for ledger/chain/head/catalog/role/PUBLIC/default/column/sequence/function drift, and `...Applies0056Before0055` across two embedded release maps. CLI/bootstrap selectors prove `migrate --scope app` opens only APP migrator and records runtime verifies but never migrates; pending 0052 and an object applied without its manifest revision both fail closed. The `postgres_sync` fixture uses four independent PostgreSQL containers/data dirs and additionally proves ledger/witness/recovery fresh+repeat/checksum/unknown migration behavior, pairwise physical identities, role/function isolation and application backup exclusion. The `s3_worm` fixture uses only app/ledger/recovery PostgreSQL plus locked TLS S3; it proves no witness DSN/role is parsed or opened, provisions S3 stable identity through the read-control principal, rejects expired/rollback/wrong-kind attestations and application-backup overlap, and exact-matches the witnessed identity set. Role tests reject direct immutable/head writes, every cross-entry-kind function call, UPDATE/DELETE/TRUNCATE/ALTER/DISABLE TRIGGER with SQLSTATE `42501` or immutable-trigger SQLSTATE `55000`; allowed function calls commit exact bytes. `pg_dump --schema-only` of the application DB contains none of the independent ledger/witness/recovery tables.

```bash
scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store/migrate \
  -run '^Test(PostgresIntegration(AppACLManifest|RecordPlatform(Application|DeletionLedger|DeletionWitness|RecoveryControl))|AppACL)' \
  -count=1
```

## Task 12: Implement strict plans, approval policy and detached signatures

**Files:**

- Create: `internal/center/platformadmin/types.go`
- Create: `internal/center/platformadmin/canonical.go`
- Create: `internal/center/platformadmin/canonical_test.go`
- Create: `internal/center/platformadmin/approval.go`
- Create: `internal/center/platformadmin/approval_test.go`
- Create: `internal/center/platformadmin/secure_output.go`
- Create: `internal/center/platformadmin/secure_output_test.go`

- [ ] **Step 1: Add RED parser/golden tests.** Cover sorted policy keys, duplicate/unknown lines, threshold 0/overflow, default-denied TTY, key-ID mismatch, symlink/owner/write-bit failures, truncated/oversized files, duplicate approver signatures, wrong plan digest, expiry boundaries and signature tampering.

```go
type ApprovalPolicy struct {
    Version uint16
    LocalTTY bool
    Threshold uint16
    Keys []ApproverKey
}

type ApproverKey struct {
    KeyID string
    PublicKey [32]byte
}

type DetachedApproval struct {
    Version uint16
    PlanDigest [32]byte
    Scope ApprovalScope
    ApprovalPolicyDigest [32]byte
    ApproverKeyID string
    SignedAt, ExpiresAt time.Time
    Signature [64]byte
}

type ApprovalScope string
const (
    ApprovalScopeCurrent ApprovalScope = "current"
    ApprovalScopeCandidate ApprovalScope = "candidate"
)

type AuthorizationMode string
const (
    AuthorizationModeLocalTTY AuthorizationMode = "local_tty"
    AuthorizationModeDetached AuthorizationMode = "detached"
)

type MutationAuthorizationArtifact struct {
    Version uint16
    MutationID string
    PlanDigest [32]byte
    Mode AuthorizationMode
    IntentRecordedAt time.Time
    LocalTTY *LocalTTYAuthorization
    Detached *DetachedAuthorization
}

type LocalTTYAuthorization struct {
    ApprovalPolicyDigest [32]byte
    OperatorPrincipalID string // op-sha256-*
    ConfirmedPlanDigest [32]byte
}

type DetachedAuthorization struct {
    ApprovalSetDigest [32]byte
    Envelopes []DetachedApproval // canonical scope/policy/key-ID order
}

const (
    MaxApprovalPolicyKeys         int64 = 64
    MaxDetachedApprovals          int64 = 64
    MaxApprovalPolicyBytes       int64 = 1 << 20
    MaxDomainAttestationPolicyBytes int64 = 64 << 10
    MaxDetachedApprovalBytes     int64 = 16 << 10
    MaxAuthorizationArtifactBytes int64 = 2 << 20
    MaxOperatorCredentialMapBytes int64 = 1 << 20
    MaxAdmissionDrainReceiptBytes int64 = 1 << 20
    MaxActivationPlanBytes       int64 = 24 << 20
    MaxMutationBundleBytes       int64 = 20 << 20
    MaxActivationInventoryBytes  int64 = 8 << 20
    MaxSignedInventoryBytes      int64 = 8 << 20
    MaxDependencyInventoryBytes  int64 = 8 << 20
    MaxCanonicalManifestBytes    int64 = 1 << 20
    MaxCanonicalPayloadBytes     int64 = 64 << 10
    MaxMutationBundleOverhead    int64 = 512 << 10
)
```

The bootstrap maximum is `8 MiB activation inventory + 8 MiB signed inventory + 1 MiB approval policy + 64 KiB domain-attestation policy + 1 MiB manifest + 1 MiB drain receipt + 2×64 KiB payload + at most 512 KiB framing = 19.6875 MiB`, below the 20 MiB bundle cap. Lifecycle bundles contain at most one 8 MiB dependency inventory, two 1 MiB policies, one 64 KiB payload and framing. The authoritative plan serializes the bundle once, not a second copy of its artifacts; candidate key proof and plan framing keep it below 24 MiB, while final trust/ledger entries are derived later and are not plan bytes. Add boundary tests that construct each exact maximum, prove the arithmetic with the real encoder, and reject one-byte-over inputs before allocation.

- [ ] **Step 2: Add RED secure-output tests.** Plan and approval outputs require parent directory ownership validation, `O_CREATE|O_EXCL|O_NOFOLLOW`, mode `0600`, bounded write, `fsync(file)`, atomic close and `fsync(directory)`. Existing targets, symlinks and partial writes must leave no authoritative artifact.

- [ ] **Step 3: Verify RED.** Run `go test ./internal/center/platformadmin -run 'Canonical|Approval|SecureOutput' -count=1`. Expected: package/types do not exist.

- [ ] **Step 4: Implement exact body/record encodings.** Use fixed magic/version/field count and length-prefix bytes; derive `ak-sha256-*`, `rk-sha256-*`, `tm-*` and `op-sha256-*` exactly as approved. The authoritative plan digest is SHA-256 of complete raw plan bytes; the plan/body contains neither that digest nor final entries. Drain, inventory and identity-set digests are derived wrappers excluded from their body bytes. JSON/text summaries never feed verification. Parse a root-owned 0400 operator map with header `HOUFENG-RECORD-PLATFORM-OPERATOR-MAP-V1` and sorted unique `uid=<positive-decimal>:subject=<1..256-byte-NFC-UTF8>` lines; derive `op-sha256-*` from version+deployment+UID+subject, reject comments/CRLF/duplicates/unknown lines and never persist UID/subject.

- [ ] **Step 5: Implement offline signing and strict authorization union.** `SignApproval` receives only plan bytes, an explicit scope and a securely loaded raw 32-byte seed or matching 64-byte Ed25519 approver private key; text/hex/base64/PEM and mismatched 64-byte keys are rejected, and it has no DB/network interface. `VerifyApprovals` parses current/candidate approval-policy bytes from the plan, deduplicates within each scope, forbids cross-scope counting, verifies scope/policy/time/signatures, requires every approval-policy candidate-only new key possession signature, sorts complete envelope bytes and returns the detached authorization body plus approval-set digest. `VerifyLocalTTY` requires actual TTY, full digest, policy allow and mapped effective UID. Enforce the action matrix: bootstrap candidate TTY or detached; add/rotate/retire/remove current TTY or detached; compromise detached current only; approval-policy rotation detached current+candidate only; domain identity rotation detached current approval only. Domain-candidate possession and optional domain-attestation-policy current+candidate governance signatures are separately typed proofs and never count as approval envelopes. TTY+detached and wrong scope are errors. In the first transaction use database `intent_recorded_at`, prove all artifacts valid then build the strict one-arm `MutationAuthorizationArtifact`; later verification uses that witnessed time.

- [ ] **Step 6: Add artifact-loss and recovery RED/GREEN tests.** Persist canonical plan and authorization separately, with artifact digest bound by final trust/ledger entries. Delete local plan/approval files after every cutpoint and delete/rebuild recovery-control from PostgreSQL/S3 full witness; `status|resume --mutation-id` must reconstruct plan, policies, full detached signatures or witnessed TTY fact and continue without a TTY or fresh authorization. A supplied local file can only exact-match witnessed bytes. Missing plan/auth/bundle, different authorization mode/principal/envelope, changed intent time, head receipt preceding its artifacts or current-time re-evaluation in place of intent-time evaluation all fail closed.

- [ ] **Step 7: Verify GREEN.** Re-run Step 3 and `go test -race ./internal/center/platformadmin -count=10`. Expected: PASS with no secret material in error strings or test logs.

## Task 13: Build a provably read-only activation planner

**Files:**

- Create: `internal/center/platformadmin/inventory.go`
- Create: `internal/center/platformadmin/inventory_test.go`
- Create: `internal/center/platformadmin/planner.go`
- Create: `internal/center/platformadmin/planner_test.go`
- Create: `internal/center/platformadmin/drain.go`
- Create: `internal/center/platformadmin/drain_test.go`
- Create: `internal/center/platformadmin/domain_identity.go`
- Create: `internal/center/platformadmin/domain_identity_test.go`
- Modify: `internal/center/recovery/inventory.go`
- Modify: `internal/center/recovery/manifest.go`

- [ ] **Step 1: Write RED inventory tests.** `managed` requires every registered DB/PITR/WAL/volume/object/partial/workspace adapter to return a bounded, verified canonical source; unknown/unbounded/duplicate sources fail. `none` requires zero still-recoverable sources and still produces a signed zero-point inventory. The activation manifest is not counted as a recoverable data point.

- [ ] **Step 2: Write RED read-only tests.** Give the planner spy repositories whose write methods panic. Planning revision 0/sequence 0 must succeed without a write call; nonzero trust/ledger heads, far tails, head mismatch, existing activation, invalid signing key, changed inventory, non-exact-204 LB config, any legacy target/connection/record-queue consumer/lease, expired drain receipt or receipt scope/config mismatch must fail before output. Add profile/domain spies: `postgres_sync` requires four pairwise-distinct live PostgreSQL identities; `s3_worm` requires exactly three plus the provisioned locked-S3 identity and must never touch a witness DB. Expired/rollback/wrong-kind attestation, live identity drift, backup-location overlap or domain identity-set digest mismatch fail before output.

```go
type ActivationPlan struct {
    Version uint16
    MutationID string
    DeploymentID, ProjectID string
    GeneratedAt, ExpiresAt time.Time
    WitnessMode string
    BackupRecoverability string
    CandidateRecoveryKeyID string
    CandidateRecoveryPublicKey [32]byte
    CandidatePossessionSignature [64]byte
    ExpectedTrustPrimary, ExpectedTrustWitness recovery.TrustHead
    ExpectedLedgerPrimary, ExpectedLedgerWitness deletionledger.Head
    MinimumFenceContractVersion uint64
    CurrentApprovalPolicyDigest [32]byte
    CandidateApprovalPolicyDigest [32]byte
    DrainScopeDigest [32]byte
    DrainReceiptDigest [32]byte
    Bundle recovery.MutationBundle
}

// Bundle is the only serialized copy of policy, receipt, inventory, manifest,
// stable domain identity set and pre-entry payload bytes. JSON/text summaries
// decode a derived DomainIdentitySetV1 view; it is not another plan field.
// Bundle and plan contain neither plan digest nor final chain entries. After
// hashing the complete raw plan, apply derives final entries that bind plan,
// bundle and identity-set digests without a self-reference.

type AdmissionAdapterPurposeV1 string
const (
    AdmissionAdapterLB AdmissionAdapterPurposeV1 = "lb"
    AdmissionAdapterQueue AdmissionAdapterPurposeV1 = "queue"
    AdmissionAdapterCopyReplay AdmissionAdapterPurposeV1 = "copy_replay"
)

type AdmissionAdapterPolicyArmV1 struct {
    Purpose AdmissionAdapterPurposeV1
    AdapterKind string // closed registry token, not a URL/path
    AdapterIdentityDigest [32]byte
    Threshold uint16
    Keys []DomainGovernanceKeyV1 // sorted unique, 1..64 for this purpose
}

type AdmissionAdapterPolicyBodyV1 struct {
    Version uint16
    DeploymentID, ProjectID string
    Generation uint64
    PreviousPolicyDigest [32]byte
    Arms []AdmissionAdapterPolicyArmV1 // exactly lb, queue, copy_replay in enum order
    ValidFrom, ExpiresAt time.Time
}

type AdmissionAdapterPolicySignatureV1 struct {
    Version uint16
    Purpose string // admission_adapter_policy_bootstrap|admission_adapter_policy_rotation
    PolicyBodyDigest, DomainGovernancePolicyDigest [32]byte
    SignerKeyID string
    Signature [64]byte
}

type AdmissionAdapterPolicyBootstrapAuthorizationV1 struct {
    CandidateDomainGovernancePolicyDigest [32]byte
    CandidateSignatures []AdmissionAdapterPolicySignatureV1 // sorted unique, threshold
}

type AdmissionAdapterPolicyRotationAuthorizationV1 struct {
    CurrentDomainGovernancePolicyDigest [32]byte
    CandidateDomainGovernancePolicyDigest [32]byte
    CurrentSignatures []AdmissionAdapterPolicySignatureV1 // sorted unique, current threshold
    CandidateSignatures []AdmissionAdapterPolicySignatureV1 // sorted unique, candidate threshold
}

type AdmissionAdapterPolicyAuthorizationV1 struct {
    Bootstrap *AdmissionAdapterPolicyBootstrapAuthorizationV1
    Rotation *AdmissionAdapterPolicyRotationAuthorizationV1
}

type SignedAdmissionAdapterPolicyV1 struct {
    Body AdmissionAdapterPolicyBodyV1
    BodyDigest [32]byte
    Authorization AdmissionAdapterPolicyAuthorizationV1 // exactly one arm
    PolicyDigest [32]byte
}

type AdmissionLBSnapshotPayloadV1 struct {
    Exact204RouteConfigDigest, TargetInventoryDigest, ConnectionInventoryDigest [32]byte
    LegacyTargetCount, ActiveConnectionCount uint64
}

type AdmissionQueueSnapshotPayloadV1 struct {
    QueueConfigDigest, ConsumerInventoryDigest, LeaseInventoryDigest [32]byte
    LegacyConsumerCount, ActiveQueueLeaseCount uint64
}

type AdmissionSnapshotBodyV1 struct {
    Version uint16
    Purpose AdmissionAdapterPurposeV1 // exactly lb|queue; copy_replay has its own schema
    DraftID, MutationID string // strict activation-draft XOR mutation scope
    DeploymentID, ProjectID string
    DeploymentEpoch, MinimumFenceContractVersion uint64
    AdapterKind string
    AdapterIdentityDigest, AdapterPolicyDigest [32]byte
    LB *AdmissionLBSnapshotPayloadV1
    Queue *AdmissionQueueSnapshotPayloadV1
    ObservedAt, ValidFrom, ExpiresAt time.Time
}

type AdmissionSnapshotSignatureV1 struct {
    Version uint16
    Purpose AdmissionAdapterPurposeV1
    SnapshotBodyDigest, AdapterPolicyDigest [32]byte
    SignerKeyID string
    Signature [64]byte
}

type SignedAdmissionSnapshotV1 struct {
    Body AdmissionSnapshotBodyV1
    BodyDigest [32]byte
    Signatures []AdmissionSnapshotSignatureV1 // sorted unique; selected arm threshold
    WrapperDigest [32]byte
}

type CopyReplaySnapshotBodyV1 struct {
    Version uint16
    MutationID, DeploymentID, ProjectID string
    AdapterKind string
    AdapterIdentityDigest, AdapterPolicyDigest [32]byte
    CopyHeadDigest, ReplayHeadDigest, InventoryHeadDigest [32]byte
    PendingFrameCount, PendingObjectCount, UnappliedReplayCount uint64
    ObservedAt, ValidFrom, ExpiresAt time.Time
}

type SignedCopyReplaySnapshotV1 struct {
    Body CopyReplaySnapshotBodyV1
    BodyDigest [32]byte
    Signatures []AdmissionSnapshotSignatureV1 // purpose=copy_replay; selected arm threshold
    WrapperDigest [32]byte
}

type AdmissionDrainReceiptBodyV1 struct {
    Version uint16
    DeploymentID, ProjectID string
    DeploymentEpoch, MinimumFenceContractVersion uint64
    AdapterPolicy SignedAdmissionAdapterPolicyV1
    LBSnapshot, QueueSnapshot SignedAdmissionSnapshotV1
    DrainScopeDigest [32]byte
    ObservedAt, ExpiresAt time.Time
}

type AdmissionDrainReceiptV1 struct {
    Body AdmissionDrainReceiptBodyV1
    Digest [32]byte
}

type DrainContinuationBodyV1 struct {
    Version uint16
    MutationID string
    PlanDigest, DrainScopeDigest, PreviousDrainProofDigest [32]byte
    Generation uint64
    DeploymentID, ProjectID string
    DeploymentEpoch uint64
    AdapterPolicy SignedAdmissionAdapterPolicyV1
    LBSnapshot, QueueSnapshot SignedAdmissionSnapshotV1
    ObservedAt, ExpiresAt time.Time
}

type DrainContinuationV1 struct {
    Body DrainContinuationBodyV1
    Digest [32]byte
}

// Bootstrap reads a root-owned, regular/no-follow 0400 policy file. Generation
// 1 has a zero previous digest and must satisfy the complete candidate domain-
// governance policy embedded in the activation plan. The signed policy, not
// the local path, is included in the activation drain, bundle and full witness.
// After activation snapshots exact-match the witnessed policy. A policy change
// is legal only inside an authorized domain_identity_rotate mutation: intent
// embeds current and candidate wrappers, candidate generation increments by one,
// binds the previous digest and satisfies both governance thresholds. Every
// pre-projection drain/cutover snapshot still exact-matches current. Candidate
// becomes active only in the identity projection CAS after rotation ledger,
// five identity artifacts and their full-witness readback; the projection embeds
// a typed activation receipt. Equal current/candidate wrappers mean unchanged.
// Purpose-separated signatures prevent lb/queue/copy_replay cross-use.

// AdmissionAdapterPolicyV1 is <=64 KiB. Each snapshot body is <=64 KiB and each
// signed wrapper is <=192 KiB with at most 64 signatures. Inventory digests and
// exact counts are attested leaves; the wrapper does not claim to embed entries. Activation drain
// and continuation receipts are <=512 KiB, and a three-wrapper rotation drain
// is <=768 KiB. Encoders prove the arithmetic with exact-max and +1-byte/count
// tests before allocation; the existing 1 MiB artifact ceiling remains a hard
// outer bound. Receipt primary/full-witness bytes are the recovery authority,
// so deleting every /run snapshot cannot weaken verification.
// Continuation generation 1 binds the original AdmissionDrainReceiptV1 digest;
// generation N>1 binds generation N-1. Zero, gaps and sibling substitution fail.

type DomainIdentitySetBodyV1 struct {
    Version uint16
    DeploymentID, ProjectID, ActiveProfile string
    SetEpoch uint64
    PreviousSetDigest [32]byte
    DomainAttestationPolicyDigest [32]byte
    Members []DomainIdentityMemberV1 // domain-kind sorted, exact profile cardinality
}

type DomainIdentitySetV1 struct {
    Body DomainIdentitySetBodyV1
    SetDigest [32]byte // derived wrapper; excluded from Body bytes
}

type DomainIdentityMemberV1 struct {
    DomainKind string // application|deletion_ledger|deletion_witness|recovery_control
    PostgreSQL *PostgreSQLDomainIdentityV1
    S3Witness *S3WitnessDomainIdentityV1
}

type PostgreSQLDomainIdentityV1 struct {
    DomainKind, DomainID, DatabaseName string
    DatabaseOID uint32
    IdentityEpoch uint64
    IdentityMode string
    SystemIdentifier string
    ExternalInfrastructure *ExternalInfrastructureIdentityDigestsV1
    ExternalStableIdentityDigest [32]byte
}

type ExternalInfrastructureIdentityDigestsV1 struct {
    AdapterKind string // closed CanonicalInfrastructureIdentityRegistryV1 token
    NormalizationVersion uint16 // v1 exact: 1
    ProviderDigest, AccountDigest, ClusterDigest [32]byte
    PhysicalStorageDigest, SnapshotPolicyDigest, RestoreAuthorityDigest [32]byte
}

type S3BucketNameV1 string

type S3WitnessDomainIdentityV1 struct {
    DomainID string
    Infrastructure ExternalInfrastructureIdentityDigestsV1
    Bucket S3BucketNameV1
    CanonicalNamespace []byte // S3NamespaceV1 count+length-prefixed tuple, 4..540 bytes
    IdentityEpoch, DefaultRetentionSeconds uint64
    HTTPSAuthorityDigest, TLSSPKIDigest, BucketIdentityDigest [32]byte
    NamespaceDigest [32]byte // sha256(CanonicalNamespace)
    StableIdentityDigest [32]byte
    VersioningEnabled, LegalHoldRequired bool
    ObjectLockMode string // closed: compliance
}

type IdentitySetPrimaryReceiptBodyV1 struct {
    Version uint16
    MutationID, DeploymentID, ProjectID, ActiveProfile string
    SetEpoch uint64
    PreviousSetDigest, SetDigest [32]byte
    LedgerSequence uint64
    LedgerEntryHash [32]byte
    ActivatedAt time.Time
}

type IdentitySetPrimaryReceiptV1 struct {
    Body IdentitySetPrimaryReceiptBodyV1
    ReceiptDigest [32]byte
}

type IdentitySetWitnessReceiptBodyV1 struct {
    Version uint16
    MutationID, DeploymentID, ProjectID, ActiveProfile string
    SetEpoch uint64
    PreviousSetDigest, SetDigest [32]byte
    PrimaryReceiptDigest [32]byte
    LedgerSequence uint64
    LedgerEntryHash [32]byte
    ActivatedAt, WitnessedAt time.Time
}

type IdentitySetWitnessReceiptV1 struct {
    Body IdentitySetWitnessReceiptBodyV1
    ReceiptDigest [32]byte
}

type DomainGovernanceKeyV1 struct {
    KeyID string
    PublicKey [32]byte
}

type DomainAttestationPolicyV1 struct {
    Version, Threshold uint16
    Keys []DomainGovernanceKeyV1 // key-ID sorted/unique, 1..64
}

// Never serialized into the stable identity set or activation bundle. It is a
// renewable liveness proof whose stable digest must match the witnessed set.
type DomainLivenessSnapshot struct {
    DomainID string
    StableIdentityDigest [32]byte
    AttestationGeneration uint64
    AttestationDigest [32]byte
    AttestationExpiresAt time.Time
    LiveProbeDigest [32]byte
}

type ActivationInventoryBody struct {
    Version uint16
    PolicyKind string
    GeneratedAt, BoundedUntil time.Time
    Sources []ActivationInventorySourceV1
}

type ActivationInventory struct {
    Body ActivationInventoryBody
    Digest [32]byte
}

type ActivationInventorySourceV1 struct {
    SourceKind, SourceID, MediaID, PolicyID string
    ObjectListDigest [32]byte
    SignedWatermarkSequence uint64
    SignedWatermarkHash [32]byte
    CreatedAt, RecoverableUntil time.Time
    DestroyedAt *time.Time
    DestructionStatus string // live|destroyed
    DestructionReceiptDigest *[32]byte
}
```

- [ ] **Step 3: Verify RED.** Run `go test ./internal/center/platformadmin -run 'Inventory|Planner|ReadOnly|NonePolicy|Drain|DomainIdentity|Profile' -count=1`. Expected: failures identify missing planner/drain/domain implementations and the current none-policy shortcut.

- [ ] **Step 4: Implement drain production and the planner.** `activation drain` securely loads the root-owned 0400 bootstrap `SignedAdmissionAdapterPolicyV1`, exact-matches deployment/project and candidate domain-governance policy, then reads two strict root-owned/no-follow/bounded `SignedAdmissionSnapshotV1` wrappers from configured LB and version-aware record-queue adapters; it changes neither system and writes a no-follow 0600 exclusive receipt. Purpose-separated signatures exact-match the policy's LB/queue arms. Complete wrappers bind the same deployment/project/epoch/minimum fence, adapter identities, exact-204 route/config, fresh target/connection/consumer/lease inventories, have observation skew at most five seconds and all four live counts equal zero; the receipt embeds the full signed policy and both wrappers, not signature digests. Receipt expiry is `min(policy expiry, lb expiry, queue expiry, observed+15m)`, canonical size is ≤512 KiB and exact-max/+1 tests run before allocation. Planner input comes from strict non-secret config for `DeploymentIDV1`, fixed project `default`, witness mode, minimum fence and explicit `none|managed` inventory policy, plus required `--drain-receipt`; revision 0/sequence 0 and immutable tails are fully verified. It reads the active profile's provisioned stable identities and latest live attestations, performs pairwise/backup-overlap checks and derives the canonical stable identity set; renewable liveness fields remain outside it. It then parses the bootstrap policy, collects source inventory, loads the strict raw 0400 recovery key, derives deterministic IDs and signs the initial inventory/activation manifest. Construct the eight-artifact pre-entry bundle in enum order: trust pre-entry body, candidate approval policy, domain-attestation policy, admission drain body (including adapter policy+wrappers), activation inventory body, signed inventory, activation manifest and contract-activation pre-entry body. The contract payload is the only serialization of the identity-set body; no leaf body serializes its own digest, and neither bundle nor plan contains a plan digest or final chain entry. Write the complete raw plan, hash those exact bytes, and only then derive non-authoritative display summaries of trust revision 1 and contract activation sequence 1. Set `ExpiresAt = min(GeneratedAt+15*time.Minute, drain.ExpiresAt)` with an injected clock.

- [ ] **Step 5: Verify deterministic serialization and no side effects.** Two serializations of the same `ActivationPlan` must be byte-identical; reparsing and rehashing the written plan must match the displayed full digest. Assert the plan encodes `Bundle` once, embeds the stable identity-set body only in the pre-entry contract payload, excludes all liveness generation/expiry and contains no plan digest/final entry. Assert the body encoders for drain, continuation, inventory and identity set have no digest field; only their outer records carry derived digests. Given raw plan digest, reconstruct final typed trust/ledger entries deterministically, then run the exact-max/one-byte-over size proof from Task 12. Re-run Step 3 GREEN.

## Task 14: Implement the activation saga and management CLI

**Files:**

- Create: `internal/center/platformadmin/mutation.go`
- Create: `internal/center/platformadmin/mutation_test.go`
- Create: `internal/center/platformadmin/repository_postgres.go`
- Create: `internal/center/platformadmin/repository_postgres_test.go`
- Create: `internal/center/platformadmin/witness_postgres.go`
- Create: `internal/center/platformadmin/witness_s3.go`
- Create: `internal/center/platformadmin/witness_test.go`
- Create: `internal/center/platformadmin/saga.go`
- Create: `internal/center/platformadmin/saga_test.go`
- Create: `cmd/houfeng-record-platform-admin/main.go`
- Create: `cmd/houfeng-record-platform-admin/main_test.go`
- Create: `internal/center/platformadmin/config.go`
- Create: `internal/center/platformadmin/config_test.go`
- Modify: `internal/center/platformadmin/drain.go`
- Modify: `internal/center/platformadmin/drain_test.go`
- Modify: `internal/center/config/config.go`
- Modify: `internal/center/config/config_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`
- Modify: `Makefile`
- Modify: `scripts/test-record-platform-integration.sh`

- [ ] **Step 1: Add RED CLI/config contract tests.** `activation drain` is the only authoritative receipt producer and requires strict LB/queue snapshot paths; `activation plan` is read-only and requires `--drain-receipt`; interactive apply requires a real TTY and the full digest; automated apply requires repeatable scoped approvals; post-intent retry requires a fresh same-scope continuation when the original receipt is no longer live. `approval sign` requires `--scope current|candidate` and rejects any DB/DSN flag. The sole mode name is `HOUFENG_DELETION_WITNESS_MODE`; `HOUFENG_RECORD_PLATFORM_WITNESS_MODE` is an unknown-variable error. `migrate --scope app` must not parse a mode and loads only the APP migrator URL plus APP runtime/admin role names. `migrate --scope permanent-delete` requires the mode: `postgres_sync` loads four MIGRATOR DB URLs, profile-matched attestation inputs and eight runtime/admin role names; `s3_worm` loads APP/LEDGER/RECOVERY URLs, six role names and a third read-control-only S3 identity-probe credential/config set. Activation/trust loads four ADMIN DB URLs for `postgres_sync`, or three ADMIN URLs plus admin S3 files for `s3_worm`; inactive WITNESS DB URL/role variables must be absent and are never parsed/opened. The center loads neither ADMIN nor MIGRATOR prefix. Unknown flags, positional secrets, `--yes`, digest prefixes and free-text actor authorization fail with exit code 2 and safe stderr.

```text
HOUFENG_DELETION_WITNESS_MODE
HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL
HOUFENG_RECORD_PLATFORM_MIGRATOR_LEDGER_DATABASE_URL
HOUFENG_RECORD_PLATFORM_MIGRATOR_WITNESS_DATABASE_URL
HOUFENG_RECORD_PLATFORM_MIGRATOR_RECOVERY_DATABASE_URL
HOUFENG_RECORD_PLATFORM_MIGRATOR_{APP,LEDGER,WITNESS,RECOVERY}_DOMAIN_ATTESTATION_FILE
HOUFENG_RECORD_PLATFORM_MIGRATOR_DOMAIN_ATTESTATION_TRUST_FILE
HOUFENG_RECORD_PLATFORM_APP_RUNTIME_ROLE
HOUFENG_RECORD_PLATFORM_APP_ADMIN_ROLE
HOUFENG_RECORD_PLATFORM_LEDGER_RUNTIME_ROLE
HOUFENG_RECORD_PLATFORM_LEDGER_ADMIN_ROLE
HOUFENG_RECORD_PLATFORM_WITNESS_RUNTIME_ROLE
HOUFENG_RECORD_PLATFORM_WITNESS_ADMIN_ROLE
HOUFENG_RECORD_PLATFORM_RECOVERY_RUNTIME_ROLE
HOUFENG_RECORD_PLATFORM_RECOVERY_ADMIN_ROLE

HOUFENG_RECORD_PLATFORM_MIGRATOR_WITNESS_S3_ENDPOINT
HOUFENG_RECORD_PLATFORM_MIGRATOR_WITNESS_S3_BUCKET
HOUFENG_RECORD_PLATFORM_MIGRATOR_WITNESS_S3_REGION
HOUFENG_RECORD_PLATFORM_MIGRATOR_WITNESS_S3_RETENTION
HOUFENG_RECORD_PLATFORM_MIGRATOR_WITNESS_S3_IDENTITY_ACCESS_KEY_FILE
HOUFENG_RECORD_PLATFORM_MIGRATOR_WITNESS_S3_IDENTITY_SECRET_KEY_FILE
HOUFENG_RECORD_PLATFORM_MIGRATOR_WITNESS_S3_CA_FILE
HOUFENG_RECORD_PLATFORM_MIGRATOR_WITNESS_S3_DOMAIN_ATTESTATION_FILE

HOUFENG_RECORD_PLATFORM_ADMIN_APP_DATABASE_URL
HOUFENG_RECORD_PLATFORM_ADMIN_LEDGER_DATABASE_URL
HOUFENG_RECORD_PLATFORM_ADMIN_WITNESS_DATABASE_URL
HOUFENG_RECORD_PLATFORM_ADMIN_RECOVERY_DATABASE_URL
HOUFENG_RECORD_PLATFORM_ADMIN_WITNESS_S3_ENDPOINT
HOUFENG_RECORD_PLATFORM_ADMIN_WITNESS_S3_BUCKET
HOUFENG_RECORD_PLATFORM_ADMIN_WITNESS_S3_REGION
HOUFENG_RECORD_PLATFORM_ADMIN_WITNESS_S3_RETENTION
HOUFENG_RECORD_PLATFORM_ADMIN_WITNESS_S3_ACCESS_KEY_FILE
HOUFENG_RECORD_PLATFORM_ADMIN_WITNESS_S3_SECRET_KEY_FILE
HOUFENG_RECORD_PLATFORM_ADMIN_WITNESS_S3_CA_FILE

HOUFENG_RECORD_PLATFORM_CANDIDATE_PROVISION_DATABASE_URL
HOUFENG_RECORD_PLATFORM_CANDIDATE_IMPORT_DATABASE_URL
HOUFENG_RECORD_PLATFORM_CANDIDATE_CUTOVER_DATABASE_URL
HOUFENG_RECORD_PLATFORM_CANDIDATE_CLEANUP_DATABASE_URL
HOUFENG_RECORD_PLATFORM_CANDIDATE_RUNTIME_ROLE
HOUFENG_RECORD_PLATFORM_CANDIDATE_ADMIN_ROLE
HOUFENG_RECORD_PLATFORM_CANDIDATE_IMPORT_ROLE
HOUFENG_RECORD_PLATFORM_CANDIDATE_CUTOVER_ROLE
HOUFENG_RECORD_PLATFORM_CANDIDATE_CLEANUP_ROLE
HOUFENG_RECORD_PLATFORM_CANDIDATE_PINNED_CURRENT_DOMAIN_ATTESTATION_POLICY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_DOMAIN_ATTESTATION_POLICY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_RECEIPT_SIGNING_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_NONCE_RESERVATION_SIGNING_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_ENDPOINT
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_BUCKET
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_REGION
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_RETENTION
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_PREPARE_ACCESS_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_PREPARE_SECRET_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_IMPORT_ACCESS_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_IMPORT_SECRET_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_CUTOVER_ACCESS_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_CUTOVER_SECRET_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_CLEANUP_READ_ACCESS_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_CLEANUP_READ_SECRET_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_TARGET_S3_CA_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_ENDPOINT
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_BUCKET
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_REGION
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_MAX_BYTES
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_TTL
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_PREPARE_ACCESS_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_PREPARE_SECRET_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_IMPORT_ACCESS_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_IMPORT_SECRET_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_CUTOVER_ACCESS_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_CUTOVER_SECRET_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_CLEANUP_ACCESS_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_CLEANUP_SECRET_KEY_FILE
HOUFENG_RECORD_PLATFORM_CANDIDATE_CONTROL_S3_CA_FILE
```

The command scope and profile form one exact parser/open matrix, not a set of optional aliases:

| command/scope | required inputs opened | forbidden or unread inputs |
|---|---|---|
| center flags off/off | legacy `HOUFENG_DATABASE_URL` owner path only | witness mode and every platform external/runtime/admin/migrator variable or file |
| center records-on/delete-off | APP runtime `HOUFENG_DATABASE_URL` only | witness mode, ledger/witness/recovery/S3, legacy `HOUFENG_DELETION_LEDGER_HMAC_KEY_RING_FILE` and every ADMIN/MIGRATOR input |
| center delete-on `postgres_sync` | APP runtime plus runtime DELETION_LEDGER, DELETION_WITNESS and RECOVERY_CONTROL URLs; `HOUFENG_RECOVERY_TRUST_SIGNING_KEY_FILE`; deletion request tokens are CSPRNG/public-commitment values and add no config | every runtime/ADMIN/MIGRATOR/CANDIDATE S3 input, every ADMIN/MIGRATOR/CANDIDATE DB input and every deletion-token keyring/secret input |
| center delete-on `s3_worm` | APP runtime, runtime LEDGER/RECOVERY URLs, runtime WITNESS S3 endpoint/bucket/region/retention/credential/CA files; `HOUFENG_RECOVERY_TRUST_SIGNING_KEY_FILE`; deletion request tokens are CSPRNG/public-commitment values and add no config | every runtime/ADMIN/MIGRATOR WITNESS DB URL/role/attestation, every ADMIN/MIGRATOR/CANDIDATE input and every deletion-token keyring/secret input |
| `migrate --scope app` | MIGRATOR APP URL and APP runtime/admin role names | witness mode; LEDGER/WITNESS/RECOVERY DB, all domain-attestation, S3, signing inputs, legacy deletion-HMAC variable and non-APP role inputs |
| `migrate --scope permanent-delete`, `postgres_sync` | witness mode; MIGRATOR APP/LEDGER/WITNESS/RECOVERY URLs, four optional external-attestation files plus trust root when fallback is used, eight runtime/admin role names | every runtime/ADMIN/MIGRATOR S3 input, all signing inputs and legacy deletion-HMAC variable |
| `migrate --scope permanent-delete`, `s3_worm` | witness mode; MIGRATOR APP/LEDGER/RECOVERY URLs, three optional PostgreSQL attestation files, six runtime/admin role names, S3 endpoint/bucket/region/retention/CA, read-control-only identity credential files and S3 governance attestation | every MIGRATOR WITNESS DB URL/role/PostgreSQL attestation, every runtime/ADMIN input, all signing inputs and legacy deletion-HMAC variable |
| activation/trust `postgres_sync` | witness mode; ADMIN APP/LEDGER/WITNESS/RECOVERY URLs plus explicit CLI artifact paths | every runtime/MIGRATOR/ADMIN S3 input and all owner credentials |
| activation/trust `s3_worm` | witness mode; ADMIN APP/LEDGER/RECOVERY URLs, ADMIN WITNESS S3 endpoint/bucket/region/retention/credential/CA files plus explicit CLI artifact paths | every ADMIN WITNESS DB URL/role/attestation, every runtime/MIGRATOR input and all owner credentials |
| `domain candidate control-policy draft` | selected current ADMIN profile opened read-only plus candidate read-control probe. Generation 1 requires mode/target/copy/drain, immutable ≤30-day mutation deadline, both explicit public descriptor paths and the purpose-locked recovery-request signer descriptor read from current witnessed trust; renewal/advance requires mutation ID plus complete previous policy and inherits the deadline and all three descriptors | every current mutation/write method, candidate write credential and all private signing keys; generation 1 rejects previous-policy or caller-supplied request-signer inputs, while renewal/advance rejects deadline/descriptor reinjection before stat |
| offline `domain governance sign --purpose candidate-control-policy --scope current|candidate` | one bounded policy body, the independently pinned matching governance policy, one explicit offline governance private-key path and one exact scope per invocation; current/candidate signatures are distinct typed sets and count only toward their own threshold | every DB URL, S3 endpoint/credential, runtime/admin/migrator/candidate environment and network connection; missing/duplicate/unknown scope, cross-scope signature reuse, another purpose or output type |
| `domain candidate control-policy seal` | one bounded signature-free draft plus sorted unique current-scope and candidate-scope signature sets; independently re-verifies both pinned policies and thresholds | every private key, DB/S3/network input, missing scope set, cross-scope double count, caller-selected policy digest or trailing bytes |
| `domain candidate control-policy publish` | selected current ADMIN recovery-control/full-witness narrow append/confirm identities plus one complete threshold-sealed policy | every candidate input, descriptor/private-key input and broad current mutation method; incomplete 2+3 readback or predecessor/prerequisite drift |
| `domain candidate challenge draft` / `domain candidate challenge seal` | draft opens selected current ADMIN adapters read-only for policy/head/copy/drain facts, then uses only the narrow challenge-start primary publisher、full-witness confirmer and primary acknowledgment function to perform `policy_prepared→challenge_started` plus five-artifact readback before emitting bytes; seal reads only the bounded published draft、sorted governance signatures and witnessed current policy/head | every CANDIDATE input；draft的narrow challenge publication以外的current mutation/write method；seal的全部write method；任何governance private key |
| `domain candidate recovery-request export` | selected current ADMIN primary/full-witness read identities, mutation ID, explicit strict-0400 `--recovery-signing-key PATH`, and only the narrow request primary-publish/full-witness-confirm identities; `abandon` first completes its separate no-intent fence/authorization 2+3 publication. Every purpose signs the canonical core, persists primary 2 + witness 3, reads all five artifacts back and only then writes the transport wrapper to the inherited FD | every candidate credential/private key, center signing-key environment fallback, caller-selected output path and broad current mutation method; signer mismatch, purpose/prerequisite mismatch, incomplete request 2+3 proof or abandon after intent produces zero output bytes |
| `domain identity unreachable draft` / `domain identity unreachable seal` | draft reconstructs the witnessed disaster-mode mutation and opens only surviving current/full-witness sources plus explicit quarantine, recovery-source inventory and replay-checkpoint snapshots; seal reads only the bounded draft and sorted current-governance signatures, re-verifies threshold/policy/head and emits one typed proof | the lost-domain adapter, every CANDIDATE input, every mutation/write method and any governance private key |
| offline non-policy `domain governance sign` / `domain attestation sign` / challenge, preparation and unreachable seal commands | governance sign accepts only `candidate-challenge|candidate-preparation|current-unreachable`, forbids `--scope` and emits `DomainGovernanceSignatureV1`; attestation sign accepts only `candidate-attestation`, forbids `--scope` and emits `DomainAttestationSignatureV1`; each sign invocation opens one explicit offline governance private-key path, while every seal opens only bounded body/policy/signature artifacts and no private key | every DB URL, S3 endpoint/credential, runtime/admin/migrator/candidate environment and network connection; any scope argument, cross-type signature output or purpose alias |
| `domain candidate prepare`, PostgreSQL target | only CANDIDATE PROVISION DB URL, target runtime/admin/import/cutover/cleanup role names, pinned-current and candidate domain-attestation policy files, candidate receipt-signing key, and explicit sealed challenge/output paths; after schema+ACL verification it revokes/closes provision and emits import-only, mutation-scoped cutover and cleanup credentials | witness mode and every current runtime/ADMIN/MIGRATOR URL or credential; CANDIDATE S3 plus IMPORT/CUTOVER/CLEANUP DB inputs during provision; every governance private key |
| `domain candidate prepare`, `s3_worm` deletion-witness target | only CANDIDATE TARGET S3 endpoint/bucket/region/retention/PREPARE credential/CA plus distinct CONTROL S3 endpoint/bucket/region/max-bytes/TTL/PREPARE credential/CA, pinned-current and candidate domain-attestation policy files, candidate receipt-signing key, and explicit sealed challenge/output roots; it proves target WORM identity/lock, control Object-Lock/default-retention disabled, pairwise bucket/prefix/backup non-overlap, then emits separate target+control IMPORT, CUTOVER and cleanup/read credentials | witness mode and every current runtime/ADMIN/MIGRATOR input; every CANDIDATE DB input and target/control IMPORT/CUTOVER/CLEANUP credential during prepare; every governance private key; legacy unsplit `CANDIDATE_S3_*` names |
| `domain transfer export` | selected current ADMIN profile inputs opened read-only, witnessed mutation ID and explicit `--recovery-signing-key PATH`; before output, strict 0400 loader derives key ID/public key and exact-matches the witnessed intent transfer signer; output is a signed bounded chunk-frame stream | every CANDIDATE input, center signing-key env fallback and every current mutation/write method |
| `domain transfer import` | only CANDIDATE IMPORT DB URL, or target S3 IMPORT credential plus separate control S3 IMPORT credential, candidate receipt-signing key, authenticated import recovery-request FD and optional exact-match policy; target writes only immutable imported evidence while control holds purgeable transfer state | every current runtime/ADMIN/MIGRATOR input and every CANDIDATE PROVISION/PREPARE/CUTOVER/CLEANUP credential; required local policy after intent |
| `domain transfer cutover export` / `domain identity resume --receipt-fd FD` | selected current ADMIN profile opened read-only plus witnessed mutation ID; export additionally requires explicit `--recovery-signing-key PATH`, exact-matches the witnessed signer before output and signs one derived command frame; resume verifies exactly the next candidate receipt and appends/full-witnesses it | every CANDIDATE input, center signing-key env fallback and every current direct mutation/write method outside the narrow receipt append |
| `domain transfer cutover apply` | only CANDIDATE CUTOVER DB URL, or target S3 CUTOVER credential plus separate control S3 CUTOVER credential, candidate receipt-signing key, authenticated cutover-apply request FD, optional exact-match policy and signed command frame | every current input and every CANDIDATE PROVISION/PREPARE/IMPORT/CLEANUP credential; required local policy after intent; direct table/object writes outside the one mutation |
| `domain candidate credential revoke` | only the matching CANDIDATE CLEANUP/revoke credential, authenticated `revoke_import|revoke_cutover` request FD, candidate receipt-signing key and inherited receipt FD; it revokes exactly one credential and retains signer/bundle/key material pending current witness acknowledgment | every current input, wrong-phase candidate credential, target WORM mutation, purge/destruction action and any caller-selected receipt path |
| `domain candidate abandon` | exact-one still-live CANDIDATE PREPARE-or-CLEANUP arm named by the authenticated no-intent abandon request, optional exact-match policy/preparation, AEAD arm and verifier request FD; it can revoke/delete only request-inventoried principals and registered control/staging bytes | any rotation intent/receipt-chain input, both credential arms, current credential, target WORM mutation, candidate receipt signing, or operation without witnessed abandon fence |
| `domain candidate cleanup` | only CANDIDATE CLEANUP-READ/control credentials, authenticated cleanup request proving witnessed cutover revocation, optional exact-match policy/preparation, AEAD arm and verifier request FD; it may identity-check/destroy receipt/nonce keys and delete only registered control bytes | every current input and CANDIDATE PROVISION/PREPARE/IMPORT/CUTOVER credential; required local artifact after intent; receipt signing; target WORM delete/hold/retention mutation; any normal data/witness write |

The six command scopes that can encrypt, decrypt or destroy candidate bytes have a second exact parser matrix: `domain candidate prepare`, `domain transfer import`, `domain transfer cutover apply`, `domain candidate credential revoke`, `domain candidate abandon` and `domain candidate cleanup`. Each requires exactly one key-source arm: local is exactly `--aead-local-key-file PATH`; KMS is exactly `--aead-kms-config PATH --aead-kms-credential-file PATH`. Local loads one regular/no-follow/bounded strict-0400 file containing exactly 32 raw bytes. KMS config is a regular/no-follow/bounded strict-0400 closed document containing adapter kind/version, provider key reference and canonical context inputs; the credential file is separately regular/no-follow/bounded strict-0400. Raw local/KMS secrets never appear in argv, logs or canonical bytes. Missing/both/mixed arms, a KMS half-arm, key-source change after the first reservation, or a descriptor that does not exact-match the witnessed request/policy/inventory fails before any candidate/control file stat, KMS call, plaintext output or write. Every other command, including recovery-request export, control-policy/challenge/sign/seal/publish, transfer export, current resume and cleanup verifier, rejects all three key-source flags before stat. The separate `HOUFENG_RECORD_PLATFORM_CANDIDATE_NONCE_RESERVATION_SIGNING_KEY_FILE` exposes a purpose-locked signer only in prepare/import/cutover candidate phase processes, exact-matches the policy/preparation/intent descriptor and signs only nonce-reservation wrappers. Credential revoke neither opens nor destroys it. Abandon/cleanup may resolve it solely to derive/exact-match identity, record unsigned destruction evidence and destroy key material; neither constructs a signer and both have nonce-signing call count 0. Every other scope rejects the variable before stat, and the nonce key is never the candidate receipt key or cleanup-verifier key.

Before choosing either profile, center applies the flag parser/open matrix from design §5: off/off does not parse the mode or any platform external input; records-on/delete-off uses only the APP runtime DSN and rejects every external platform variable even when empty; records-on/delete-on requires exactly one profile row above; records-off/delete-on is invalid before any file stat, DNS resolution or connection. Tests assert `session_user=current_user` for every opened pool and the exact expected role rather than inferring isolation from different DSN strings.

Required active-profile names are present exactly once; inactive-profile names are configuration errors even when empty. Tests use stat/parse/resolve/pool/open spies and prove a forbidden URL or credential file is never touched. `platformadmin` owns separate admin, migrator, control-policy-draft/publish, challenge, offline-sign/seal, candidate-prepare, recovery-request-export, transfer-export, transfer-import, cutover-export, candidate-cutover, credential-revoke, receipt-ingest, candidate-abandon, candidate-cleanup and cleanup-verifier parsers so `config.LoadCenterConfig` cannot accidentally retain those credentials; center has a separate exact runtime parser. Each parser has an allowlist of environment names and fails before file stat or DNS resolution when any phase-forbidden name is present, including an empty value. The candidate bundle accepts exactly one PostgreSQL-or-S3 arm and is atomically replaced after preparation: provision/prepare credentials are revoked and absent before any import, while separate import, cutover and cleanup credentials survive only for their own phases. The S3 arm always contains two independently identified and non-overlapping surfaces: immutable target WORM and purgeable candidate-control; alias bucket/prefix, control Object Lock/default retention, backup/snapshot overlap or credential reuse fail before byte 1. The pinned-current governance policy must byte-match the full-witness current policy. Generation-1 control-policy draft binds the candidate governance policy plus the explicitly supplied nonce-reservation signer and cleanup-verifier descriptors into the published policy; sealed preparation and typed intent additionally bind the candidate receipt signer and repeat all three signer identities. Challenge embeds only the complete already-published prepare policy, never an untrusted policy hint. Import/cutover/revocation phase receipts verify under the preparation-bound candidate receipt key; final purge/workspace-zero verify only under the cleanup-verifier key. The cutover role has no table/head ownership or broad write: its definer function/object policy requires the exact witnessed mutation, plan/authorization, expected head/sequence and canonical bytes, and allows only the target-specific append/confirm/projection operation. The cleanup/revoke role can only revoke the request-selected candidate principal and, after witnessed cutover revocation, purge the registered control bundle/workspace; it does not hold the cleanup-verifier signing key. The cleanup-verifier parser can only re-probe target evidence/control zero residue and sign the two typed final teardown purposes, and holds no write/admin credential. Request-export, export, import, cutover, revoke, abandon, cleanup and verifier run as separate OS processes over protected inherited FDs or a protected pipe/socket with mutually filtered environments; no process receives both current write-capable credentials and candidate write credentials, and no non-verifier process receives the verifier key. Every mode rejects same-role reuse and same recovery-domain identity. Braced names above mean one exact variable per active PostgreSQL domain. S3 has no configurable namespace variable: long-lived current/target WORM principals derive `record-platform/v1/<scope_hash>/`, while ephemeral control principals derive the separate `candidate-control/v1/<scope_hash>/`; both use the same canonical scope hash but require different bucket identity/prefix and phase policies. A fixed golden scope hash is shared by key construction and all principal policies; omitting the domain separator, using another preimage, repeating `<scope_hash>` or mapping target/control to overlapping storage yields zero successful writes and fails before readback.

`activation drain` verifies two strict, root-owned/no-follow/bounded signed wrappers emitted by configured LB and record-queue deployment adapters and changes neither system. The LB wrapper binds the private exact-204 admission route, deployment epoch, target+connection inventories/counts and config digest; the queue wrapper binds the same scope/epoch/minimum contract, version-aware admission config, consumer+lease inventories/counts and config digest. Both purpose-specific signatures exact-match the complete signed adapter policy embedded in the receipt. Observation skew is at most five seconds and all legacy/live counts are zero. Receipt lifetime is `min(policy.expires_at, lb_snapshot.expires_at, queue_snapshot.expires_at, observed_at+15m)`; plan expiry is the earlier of that and generated-at+15m. Before the first durable mutation, apply rereads current adapter state and requires the original policy/wrappers/config/inventory/authorization to be live and exact. Once the exact durable intent exists, a retry whose original receipt is no longer live supplies a `DrainContinuationV1` generated by `activation drain --continue-mutation <mutation_id>`; the command first reconstructs the plan and witnessed adapter policy from primary/full witness, then embeds two fresh signed wrappers, fresh four inventories, generation and previous digest. Reappearing old targets pause the saga; after they are drained, a new continuation resumes the same mutation rather than creating a replacement plan. Deleted `/run` snapshots are never required because complete receipt bytes are primary/full-witness artifacts.

- [ ] **Step 2: Add saga cutpoint RED tests.** Cover every transition:

```go
type MutationState string

const (
    MutationIntent MutationState = "intent"
    MutationCopyPending MutationState = "copy_pending"
    MutationDualWritePending MutationState = "dual_write_pending"
    MutationCurrentUnreachablePending MutationState = "current_unreachable_pending"
    MutationDrainPending MutationState = "drain_pending"
    MutationImportRevokePending MutationState = "import_revoke_pending"
    MutationTrustPrimaryUnknown MutationState = "trust_primary_unknown"
    MutationTrustWitnessPending MutationState = "trust_witness_pending"
    MutationInventoryPending MutationState = "inventory_pending"
    MutationLedgerPrimaryUnknown MutationState = "ledger_primary_unknown"
    MutationLedgerWitnessPending MutationState = "ledger_witness_pending"
    MutationCandidateCutoverPending MutationState = "candidate_cutover_pending"
    MutationProjectionPending MutationState = "projection_pending"
    MutationCutoverProjectionPending MutationState = "cutover_projection_pending"
    MutationRetirementPending MutationState = "retirement_pending"
    MutationFinalProofPending MutationState = "final_proof_pending"
    MutationCandidateTeardownPending MutationState = "candidate_teardown_pending"
    MutationCompletionPending MutationState = "completion_pending"
    MutationComplete MutationState = "complete"
)
```

At each primary commit, witness confirm, artifact publish, ledger append, projection and final-readback cutpoint, simulate ack loss and process death. Bootstrap uses only intent, trust primary/witness, inventory, ledger primary/witness, projection and complete; ordinary trust/key/policy actions use only trust primary/witness, their action-required inventory, projection and complete. The eleven rotation-only pending states `copy_pending|dual_write_pending|current_unreachable_pending|drain_pending|import_revoke_pending|candidate_cutover_pending|cutover_projection_pending|retirement_pending|final_proof_pending|candidate_teardown_pending|completion_pending` are accepted only by `domain_identity_rotate`; non-rotation SQL/Go transition tables reject them and never synthesize ledger activation stages. Retry with the exact plan and canonical approval set must continue; a changed digest/head/policy/approval/domain identity must conflict; capability remains closed until complete.

- [ ] **Step 3: Verify RED.** Run `go test ./internal/center/platformadmin ./cmd/houfeng-record-platform-admin -run 'CLI|Activation|Saga|Cutpoint|AckLoss|Drain|Profile|DomainIdentity|ScopedApproval|Retry' -count=1`. Expected: missing CLI/repositories and trust-primary retry failures.

- [ ] **Step 4: Implement persisted intent and immutable governance writes.** In one recovery-control transaction insert or exact-match the mutation root, complete bounded canonical plan, complete canonical authorization artifact, active profile/domain identity-set digest+epoch, activation inventory, mutation bundle, final trust entry and trust head. The mutable root stores only bounded status/error codes plus digest/principal equality guards; the immutable plan/authorization/bundle tables retain the canonical non-secret bytes. Confirm the identical plan, authorization, bundle and trust entry to PostgreSQL full witness in one transaction or to S3 WORM as ordered immutable objects followed by a sequence receipt, then read every object back and verify the complete chain. Never store private keys, argv, local paths or original approval-file metadata.

- [ ] **Step 5: Publish signed artifacts and activation ledger entry.** Rebuild trust only from the fresh witness, verify the plan signatures, idempotently publish the activation manifest and initial inventory, append the exact contract activation, confirm the full witness, and invoke projection. Each adapter returns canonical bytes/hash, not a boolean assertion.

- [ ] **Step 6: Implement final proof.** Re-read complete trust/activation/ledger primary and witness namespaces, compare every canonical byte, re-probe the active physical domain identity set, and verify deployment contract state plus replay watermark equal sequence 1. Publish epoch 1 in this exact order: call the recovery-control primary function to insert-or-exact-match the canonical set plus typed primary receipt; read back those two primary artifacts; pass that receipt to the PostgreSQL/S3 full-witness function to insert-or-exact-match the byte-identical set, a canonical copy of the complete primary receipt plus its digest, and the typed witness receipt that binds that digest; then read back all three witness artifacts. Only after the five-artifact/2+3 full readback may their two receipt digests enter the canonical completion receipt. Build completion binding plan/authorization/bundle/trust/ledger/identity-set/inventory/projection/replay/workspace-zero receipts; insert-or-exact-match it through the typed recovery-control completion function, confirm it through the typed PostgreSQL/S3 full-witness completion path, read it back and revalidate the commitment DAG. Missing, duplicated, gapped or unwitnessed genesis identity history blocks completion and all later rotation planning. Only the narrow completion function may then move the root to `complete` and set `completed_at/details_delete_after`; missing or unwitnessed completion bytes keep `projection_pending`. Expired plans may continue only when the exact durable mutation already exists and a fresh same-scope/config drain continuation proves zero old targets/consumers.

Retry has one branch point. For a new mutation, apply parses and cryptographically verifies the supplied plan plus exactly one authorization mode, requires plan/drain/authorization live at database-selected intent time, exact-matches current drain/domain state and heads, and atomically stores the complete plan/authorization/bundle and intent. For a durable mutation, `activation status|resume --mutation-id` reconstructs those bytes from primary/full witness, verifies the commitment DAG and resumes without local plan/approval files or a TTY; optional local artifacts can only byte-match the witnessed canonical records. A live original receipt or fresh `--drain-continuation` still proves the same drain scope/config is empty. Same detached envelopes in a different CLI order are accepted only when canonical sorting produces identical bytes; any added/removed/replaced envelope, authorization mode/principal, intent time, plan, receipt scope, domain identity, policy, head or canonical byte is a stable conflict. Before intent an expired artifact requires a new plan/mutation; after intent a replacement plan or fresh authorization is forbidden. Local artifact retention is an operator convenience, never a recovery prerequisite.

- [ ] **Step 7: Expose scoped one-shot migration without credential bleed.** `houfeng-record-platform-admin migrate --scope app` opens only the APP migrator pool, applies/verifies the APP schema plus the latest independently monotonic `app_acl_manifest_rNNNNNN`, and rejects witness mode plus every ledger/witness/recovery/S3 variable even when empty; this is the only migrator path used for records-on/delete-off. `migrate --scope permanent-delete` requires the strict witness mode, first verifies APP migration/ACL, then for `postgres_sync` opens LEDGER/WITNESS/RECOVERY migrator pools, while `s3_worm` opens LEDGER/RECOVERY plus the third read-control-only S3 identity client and never reads a WITNESS DB variable. It provisions only active-profile pre-created runtime/admin roles, exact ACL/function grants and stable domain identities, then closes every pool/client. Neither scope loads runtime/admin S3 write credentials, signing/approval keys or creates trust state; the standalone PostgreSQL witness runner remains covered by integration when the selected profile is S3. When either record flag is enabled, center bootstrap uses runtime identities and verifies the exact sorted migration filename/checksum set plus applicable ACL revision/set digest; pending/unknown/tampered state keeps the capability unavailable and never falls back to applying with runtime credentials. Preserve legacy APP owner/auto-migration only while both record flags are false.

- [ ] **Step 8: Verify GREEN with real PostgreSQL and three-principal S3.** `postgres-s3` retains the PostgreSQL fixture, generates a temporary CA/server certificate for `127.0.0.1`, starts the pinned MinIO/mc versions on unused host-network ports, and creates one versioned `--with-lock` bucket with 3650-day default COMPLIANCE retention. It creates three independent random users: `record-center`, `record-platform-admin` and `record-platform-migrator`. Center and admin may GET/LIST immutable witness data but center may PUT/retention/hold only under `ledger/runtime/` and `heads/runtime/`, while admin may do so only under `ledger/activation/`, `ledger/rotation/`, `heads/activation/`, `heads/rotation/`, `trust/`, `trust-heads/`, `plans/`, `authorizations/`, `bundles/`, `completion/`, `identity-sets/` and `rotations/`; explicit denial covers every cross-prefix PUT, overwrite/version replacement, DeleteObject/DeleteObjectVersion, legal-hold removal and retention reduction. Epoch-1 and N+1 set objects/receipts use the same zero-padded immutable identity-set keys. Migrator may read only bucket/versioning/Object-Lock/retention identity, cannot read witness object bodies and cannot PUT any normal witness prefix. A separate non-WORM candidate-control bucket/namespace uses phase-scoped credentials, bounded TTL, no backup/snapshot overlap and signed purge receipts; candidate cannot write the current WORM root, and a forced Object-Lock/default-retention candidate-control fixture fails before byte 1. The wrapper writes distinct 0400 credential files plus CA, exports runtime variables only to deletionledger tests, ADMIN variables only to platformadmin tests, MIGRATOR identity variables only to migration tests and candidate variables only to phase-specific tests, and proves all access keys/policies differ with no fallback. Every immutable witness object receives legal hold. Run Step 3 and these commands; both fail on a skipped test and clean every fixture:

```bash
scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/platformadmin \
  -run 'PostgresIntegration.*(Activation|Saga|Cutpoint|AckLoss)' -count=1

scripts/test-record-platform-integration.sh postgres-s3 -- \
  go test -v ./internal/center/platformadmin ./internal/center/deletionledger \
  -run 'S3WORMIntegration.*(Identity|Activation|Runtime|CrossWrite|Overwrite|Delete|LegalHoldRemoval|RetentionReduction|Tail|AckLoss)' -count=1
```

## Task 15: Complete key and approval-policy lifecycle commands

**Files:**

- Create: `internal/center/platformadmin/trust_mutation.go`
- Create: `internal/center/platformadmin/trust_mutation_test.go`
- Modify: `internal/center/platformadmin/planner.go`
- Modify: `internal/center/platformadmin/saga.go`
- Modify: `internal/center/platformadmin/approval.go`
- Modify: `internal/center/platformadmin/approval_test.go`
- Modify: `internal/center/platformadmin/canonical.go`
- Modify: `internal/center/platformadmin/canonical_test.go`
- Modify: `cmd/houfeng-record-platform-admin/main.go`
- Modify: `cmd/houfeng-record-platform-admin/main_test.go`
- Modify: `internal/center/recovery/trust_store.go`
- Modify: `internal/center/recovery/trust_store_test.go`
- Modify: `scripts/test-record-platform-integration.sh`

- [ ] **Step 1: Add lifecycle RED matrix.** Cover add/rotate with old-key authorization + new-key possession, retire with a signed dependency inventory, compromise using offline approval threshold without trusting the compromised key, remove only at dependency count 0, removed-key verification denial, same-key-ID re-add denial, and policy rotation requiring both current and candidate thresholds/possession proofs.

Approval requirements and modes are action-specific and fail closed outside this table:

| action | TTY | detached approvals | independent possession/inventory proof |
|---|---|---|---|
| bootstrap | candidate policy must set `local_tty=allow` | candidate threshold | candidate recovery key possession + activation inventory |
| add / rotate | witnessed current policy may allow | current threshold | candidate recovery key possession |
| retire / remove | witnessed current policy may allow | current threshold | signed dependency inventory; remove requires zero dependencies |
| compromise | forbidden | current threshold only | signed affected-dependency inventory; compromised key never authorizes |
| approval-policy rotate | forbidden | current threshold and candidate threshold | every candidate-only approver key signs candidate scope |

TTY and detached are mutually exclusive. A recovery candidate-key possession signature is separate proof and never counts as an approver. A key present in both approval policies produces two scoped envelopes to count in both; every candidate-only policy key signs in candidate scope even when the candidate threshold is already met. Verification canonicalizes each scope independently, caps a policy and supplied approval set at 64 keys/envelopes, and hashes the sorted scoped union into one `approval_set_digest`.

- [ ] **Step 2: Add loss-of-quorum RED tests.** If the current approval quorum is unavailable or suspected compromised, every trust/policy mutation must fail closed. There is no CLI recovery flag, environment override, startup seed, or single-admin path.

- [ ] **Step 3: Verify RED.** Run `go test ./internal/center/platformadmin ./internal/center/recovery ./cmd/houfeng-record-platform-admin -run 'Rotate|Retire|Compromise|Remove|Policy|Quorum' -count=1`.

- [ ] **Step 4: Implement lifecycle planning through the same envelope/saga.** Do not add one-off SQL paths. Expose `trust plan --action add|rotate|retire|compromise|remove|approval-policy-rotate`, new-mutation `trust apply --plan PATH [--confirm FULL_DIGEST | --approval PATH ...]`, and durable `trust status|resume --mutation-id ID`; every action writes the same authoritative plan format and follows the same immutable plan/authorization/mutation saga as activation. Ordinary trust/key/policy actions have no drain stage, and their parser explicitly rejects `--drain-continuation`; that flag exists only for activation/domain rotation. Local plan/approval files are never required after intent. New recovery keys become active only after full witness durability; retired keys verify only their original valid interval; compromised keys invalidate affected manifests; removed keys remain reconstructable audit facts but can never verify or reactivate.

- [ ] **Step 5: Implement policy rotation.** Include full candidate policy bytes in the immutable mutation bundle and both policy digests in the trust entry. Apply the new digest only after full witness durability; subsequent commands reject a local policy file whose digest differs from the witnessed current digest.

- [ ] **Step 6: Verify GREEN.** Re-run Step 3, then use `scripts/test-record-platform-integration.sh postgres-s3 -- go test -v ./internal/center/platformadmin ./internal/center/recovery -run 'Integration.*(Add|Rotate|Retire|Compromise|Remove|Policy|PrimaryLoss)' -count=1`. Expected: every lifecycle cutpoint and restart from primary loss rebuilds from the full witness bundle; no test is skipped.

## Task 16: Finish runtime, retention and deployment integration

**Files:**

- Modify: `internal/center/recovery/runtime_inventory.go`
- Modify: `internal/center/recovery/runtime_inventory_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`
- Modify: `internal/center/recordplatform/janitor.go`
- Modify: `internal/center/recordplatform/janitor_test.go`
- Modify: `internal/center/store/retention.go`
- Modify: `internal/center/store/retention_test.go`
- Modify: `internal/center/retention/worker.go`
- Modify: `internal/center/retention/worker_test.go`
- Create: `internal/center/retention/lifecycle_registry.go`
- Create: `internal/center/retention/lifecycle_registry_test.go`
- Create: `internal/center/retention/participant_registry.go`
- Create: `internal/center/retention/participant_registry_test.go`
- Create: `internal/center/retention/acceptance.go`
- Create: `internal/center/retention/acceptance_test.go`
- Create: `cmd/houfeng-retention-scanner/main.go`
- Create: `cmd/houfeng-retention-scanner/main_test.go`
- Create: `cmd/houfeng-retention-acceptance-signer/main.go`
- Create: `cmd/houfeng-retention-acceptance-signer/main_test.go`
- Create: `security/record-retention/acceptance-policy.v1`
- Create: `security/record-retention/genesis-approver-policy.v1`
- Create: `security/record-retention/child-surface-owner-matrix.v1`
- Create: `security/record-retention/initial-lifecycle-program.v1`
- Create: `security/record-retention/initial-purge-participant-program.v1`
- Create: `.github/workflows/record-retention-source-claim.yml`
- Create: `.github/workflows/record-retention-merge-acceptance.yml`
- Create: `.github/workflows/record-retention-acceptance-metadata.yml`
- Modify: `.github/CODEOWNERS`
- Create: `scripts/verify-record-retention-acceptance.sh`
- Create: `scripts/verify-record-retention-acceptance.test.sh`
- Create: `docs/deploy/record-retention-acceptance.md`
- Modify: `compose.yaml`
- Modify: `.env.example`
- Modify: `docs/deploy/compose.env.example`
- Modify: `docs/deploy/local-and-systemd.md`
- Modify: `docs/deploy/systemd/houfeng-center.service`
- Modify: `internal/center/deploy/docker_static_test.go`
- Modify: `scripts/test-record-platform-integration.sh`
- Create: `scripts/test-record-platform-upgrade.sh`
- Modify: `scripts/docker-entrypoint.sh`
- Modify: `Makefile`
- Modify: `Dockerfile`

- [ ] **Step 1: Add runtime RED tests.** Revision 0, incomplete mutation, missing activation bundle, stale/mismatched policy digest, missing signed inventory and replay watermark below the witnessed activation all keep permanent delete unavailable. `none` must verify the signed zero-point inventory rather than bypass it.

- [ ] **Step 2: Add retention RED tests.** Build the eight independent roots in `RetentionRegistryStateV1`; only `CanonicalSchemaRegistryV1` (production bounded encoder/decoder/storage-codec leaves) and manually reviewed `RetentionPolicyRegistryV1` (one class+semantic and concrete stage bindings per exact surface) are semantic classification authorities. `RetentionLifecycleRegistryV1` supplies reusable clock templates, `PurgeParticipantRegistryV1` supplies typed capability rows and executable binding IDs, and the S3/filesystem/client roots supply exact inventory grammars rather than classifications. `go generate` exact-joins all eight roots into checked-in `retention_allowlist_v1.json`. The two immutable initial-program manifests contain the 21/24 canonical rows below, while the live lifecycle/participant roots acquire only the current child's owned rows through the 11 add-only deltas. The generator never invents semantics from field names or database types. Wildcards, relation/schema-only rows, default classification, unknown enum values, a decoder with no policy row, a policy row with no decoder/column/property, missing/extra lifecycle or participant consumer, trailing bytes and any new unclassified column/key/file fail the build and permanent-delete readiness.

  ```go
  type RetentionClassV1 string
  const (
      RetentionImmutableLedger RetentionClassV1 = "immutable_ledger"
      RetentionImmutableGovernance RetentionClassV1 = "immutable_governance"
      RetentionMinimalAudit RetentionClassV1 = "minimal_audit"
      RetentionLiveProductAuthority RetentionClassV1 = "live_product_authority"
      RetentionLiveProductDerived RetentionClassV1 = "live_product_derived"
      RetentionDraftProduct RetentionClassV1 = "draft_product"
      RetentionManagedClientBuffer RetentionClassV1 = "managed_client_buffer"
      RetentionLiveSafety RetentionClassV1 = "live_safety"
      RetentionOperational24H RetentionClassV1 = "operational_24h"
      RetentionOperational30D RetentionClassV1 = "operational_30d"
      RetentionRecoverabilityWindow RetentionClassV1 = "recoverability_window"
      RetentionStorageControl RetentionClassV1 = "storage_control"
      RetentionEphemeralRegistered RetentionClassV1 = "ephemeral_registered"
  )

  type AllowedSemanticV1 string
  const (
      SemanticEncodedContainer AllowedSemanticV1 = "encoded_container"
      SemanticSchemaDiscriminator AllowedSemanticV1 = "schema_discriminator"
      SemanticNamespace AllowedSemanticV1 = "namespace"
      SemanticRecordObjectIdentity AllowedSemanticV1 = "record_object_identity"
      SemanticOriginIdentity AllowedSemanticV1 = "origin_identity"
      SemanticActorPrincipal AllowedSemanticV1 = "actor_principal"
      SemanticGovernancePrincipal AllowedSemanticV1 = "governance_principal"
      SemanticGovernanceMutationIdentity AllowedSemanticV1 = "governance_mutation_identity"
      SemanticGovernancePolicyMaterial AllowedSemanticV1 = "governance_policy_material"
      SemanticAuthorizationFloor AllowedSemanticV1 = "authorization_floor"
      SemanticOwnerLeaseIdentity AllowedSemanticV1 = "owner_lease_identity"
      SemanticOperationReference AllowedSemanticV1 = "operation_reference"
      SemanticRecoverySourceIdentity AllowedSemanticV1 = "recovery_source_identity"
      SemanticRouteCode AllowedSemanticV1 = "route_code"
      SemanticReasonCode AllowedSemanticV1 = "reason_code"
      SemanticClosedEnum AllowedSemanticV1 = "closed_enum"
      SemanticDigestHash AllowedSemanticV1 = "digest_hash"
      SemanticCryptographicChallenge AllowedSemanticV1 = "cryptographic_challenge"
      SemanticPublicKey AllowedSemanticV1 = "public_key"
      SemanticSignature AllowedSemanticV1 = "signature"
      SemanticCounter AllowedSemanticV1 = "counter"
      SemanticSequenceGeneration AllowedSemanticV1 = "sequence_generation"
      SemanticTimestamp AllowedSemanticV1 = "timestamp"
      SemanticBoundedErrorCode AllowedSemanticV1 = "bounded_error_code"
      SemanticIdentityFreeAggregate AllowedSemanticV1 = "identity_free_aggregate"
      SemanticSchemaMigrationFilename AllowedSemanticV1 = "schema_migration_filename"
      SemanticProductTextContent AllowedSemanticV1 = "product_text_content"
      SemanticProductBinaryContent AllowedSemanticV1 = "product_binary_content"
      SemanticProductCanonicalPayload AllowedSemanticV1 = "product_canonical_payload"
      SemanticProductDisplayMetadata AllowedSemanticV1 = "product_display_metadata"
      SemanticProductFilename AllowedSemanticV1 = "product_filename"
      SemanticProductSafeURL AllowedSemanticV1 = "product_safe_url"
      SemanticDerivedProjectionContent AllowedSemanticV1 = "derived_projection_content"
      SemanticLiveRouteReference AllowedSemanticV1 = "live_route_reference"
      SemanticExternalDeliveryIdentity AllowedSemanticV1 = "external_delivery_identity"
      SemanticManagedContentPayload AllowedSemanticV1 = "managed_content_payload"
      SemanticEncryptedKeyMaterial AllowedSemanticV1 = "encrypted_key_material"
      SemanticMediaDestructionIdentity AllowedSemanticV1 = "media_destruction_identity"
      SemanticStorageIntegrity AllowedSemanticV1 = "storage_integrity"
      SemanticStorageRetention AllowedSemanticV1 = "storage_retention"
      SemanticStorageLocationComponent AllowedSemanticV1 = "storage_location_component"
  )

  type PostgreSQLColumnAddressV1 struct {
      DatabaseDomain, SchemaToken, RelationToken, ColumnToken string
  }
  type CanonicalLeafAddressV1 struct {
      SchemaID string
      Version uint16
      Discriminator, LeafPath string
  }
  type S3KeySegmentAddressV1 struct { LocationClass, Family, ExactSegment string }
  type S3MetadataAddressV1 struct { LocationClass, Family, ExactMetadataKey string }
  type S3ControlPropertyAddressV1 struct { LocationClass, Family, ExactProperty string }
  type ManagedFileAddressV1 struct {
      RootToken, TerminalToken, SchemaID, LeafOrProperty string
  }
  type ManagedClientLeafAddressV1 struct {
      AppOriginToken, Engine, DatabaseToken string
      DatabaseVersion uint64
      StoreToken, CodecSchemaID, LeafPath string
  }
  type RetentionSurfaceAddressV1 struct {
      PostgreSQLColumn *PostgreSQLColumnAddressV1
      CanonicalLeaf *CanonicalLeafAddressV1
      S3KeySegment *S3KeySegmentAddressV1
      S3Metadata *S3MetadataAddressV1
      S3ControlProperty *S3ControlPropertyAddressV1
      ManagedFile *ManagedFileAddressV1
      ManagedClientLeaf *ManagedClientLeafAddressV1
  }

  type RetentionLifecycleKindV1 string
  const (
      LifecyclePermanent RetentionLifecycleKindV1 = "permanent"
      LifecycleOwnerBound RetentionLifecycleKindV1 = "owner_bound"
      LifecycleDerivedOwnerBound RetentionLifecycleKindV1 = "derived_owner_bound"
      LifecycleAbsoluteExpiry RetentionLifecycleKindV1 = "absolute_expiry"
      LifecycleReceiptCompaction RetentionLifecycleKindV1 = "receipt_compaction"
      LifecycleRecoverabilityBound RetentionLifecycleKindV1 = "recoverability_bound"
      LifecycleManagedClient RetentionLifecycleKindV1 = "managed_client"
  )

  type RetentionTriggerV1 string
  const (
      TriggerOwnerDeleteCommit RetentionTriggerV1 = "owner_delete_commit"
      TriggerOwnerReferenceZero RetentionTriggerV1 = "owner_reference_zero"
      TriggerProductExpired RetentionTriggerV1 = "product_expired"
      TriggerSaveCommitted RetentionTriggerV1 = "save_committed"
      TriggerExplicitDiscard RetentionTriggerV1 = "explicit_discard"
      TriggerLogout RetentionTriggerV1 = "logout"
      TriggerUserSwitch RetentionTriggerV1 = "user_switch"
      TriggerAuthorizationRevoked RetentionTriggerV1 = "authorization_revoked"
      TriggerSourceWindowClosed RetentionTriggerV1 = "source_window_closed"
      TriggerAbsoluteExpired RetentionTriggerV1 = "absolute_expired"
      TriggerVerifiedPurge RetentionTriggerV1 = "verified_purge"
      TriggerCommentRedacted RetentionTriggerV1 = "comment_redacted"
      TriggerMutationComplete RetentionTriggerV1 = "mutation_complete"
      TriggerTerminalReceiptVerified RetentionTriggerV1 = "terminal_receipt_verified"
  )

  type RetentionBindingRoleV1 string
  const (
      BindingCapturedAt RetentionBindingRoleV1 = "captured_at"
      BindingCreatedAt RetentionBindingRoleV1 = "created_at"
      BindingExpiresAt RetentionBindingRoleV1 = "expires_at"
      BindingLastActivityAt RetentionBindingRoleV1 = "last_activity_at"
      BindingMutationCompletedAt RetentionBindingRoleV1 = "mutation_completed_at"
      BindingOwnerReferenceZeroAt RetentionBindingRoleV1 = "owner_reference_zero_at"
      BindingProductExpiresAt RetentionBindingRoleV1 = "product_expires_at"
      BindingRawEventDeleteAt RetentionBindingRoleV1 = "raw_event_delete_at"
      BindingRetentionEligibleAt RetentionBindingRoleV1 = "retention_eligible_at"
      BindingSourceRecoverableUntil RetentionBindingRoleV1 = "source_recoverable_until"
      BindingUpdatedAt RetentionBindingRoleV1 = "updated_at"
  )

  type RetentionDeadlineKindV1 string
  const (
      DeadlineNone RetentionDeadlineKindV1 = "none"
      DeadlineAbsoluteBinding RetentionDeadlineKindV1 = "absolute_binding"
      DeadlineConfiguredFromBinding RetentionDeadlineKindV1 = "configured_from_binding"
      DeadlineFixedFromBinding RetentionDeadlineKindV1 = "fixed_from_binding"
      DeadlineFixedHardCapAbsoluteBinding RetentionDeadlineKindV1 = "fixed_hard_cap_absolute_binding"
      DeadlineMinConfiguredHardCapAbsoluteBinding RetentionDeadlineKindV1 = "min_configured_hard_cap_absolute_binding"
  )

  type RetentionUpperBoundKindV1 string
  const (
      UpperBoundNone RetentionUpperBoundKindV1 = "none"
      UpperBoundFixed RetentionUpperBoundKindV1 = "fixed"
      UpperBoundNoHardCap RetentionUpperBoundKindV1 = "no_hard_cap"
  )

  type RetentionDeadlineV1 struct {
      Kind RetentionDeadlineKindV1
      AnchorRole RetentionBindingRoleV1
      ExpiryRole RetentionBindingRoleV1
      SettingID string
      DefaultSeconds, MaximumSeconds uint64
      UpperBoundKind RetentionUpperBoundKindV1
  }

  type RetentionLifecycleStageTemplateV1 struct {
      Ordinal uint16
      ImmediateTriggers []RetentionTriggerV1 // enum-sorted unique
      Deadline RetentionDeadlineV1
  }

  type RetentionLifecycleRegistryRowV1 struct {
      Version uint16
      ID string
      IntroducedByChildID uint16
      Kind RetentionLifecycleKindV1
      ApplicableClasses []RetentionClassV1 // enum-sorted unique
      StageTemplates []RetentionLifecycleStageTemplateV1 // ordinal 1..N, no gaps
  }

  type RetentionSurfaceKindV1 string
  const (
      SurfaceCanonicalLeaf RetentionSurfaceKindV1 = "canonical_leaf"
      SurfaceManagedClientLeaf RetentionSurfaceKindV1 = "managed_client_leaf"
      SurfaceManagedFile RetentionSurfaceKindV1 = "managed_file"
      SurfacePostgreSQLColumn RetentionSurfaceKindV1 = "postgresql_column"
      SurfaceS3ControlProperty RetentionSurfaceKindV1 = "s3_control_property"
      SurfaceS3KeySegment RetentionSurfaceKindV1 = "s3_key_segment"
      SurfaceS3Metadata RetentionSurfaceKindV1 = "s3_metadata"
  )

  type RetentionPurgeActionV1 string
  const (
      PurgeAbortMultipartUploads RetentionPurgeActionV1 = "abort_multipart_uploads"
      PurgeDeleteObjectAllVersions RetentionPurgeActionV1 = "delete_object_all_versions"
      PurgeDeleteRow RetentionPurgeActionV1 = "delete_row"
      PurgeNullLeaf RetentionPurgeActionV1 = "null_leaf"
      PurgeClientEntry RetentionPurgeActionV1 = "purge_client_entry"
      PurgeManagedFile RetentionPurgeActionV1 = "purge_managed_file"
      PurgeReduceToSurvivor RetentionPurgeActionV1 = "reduce_to_survivor"
      PurgeRevokeCredentialAuthority RetentionPurgeActionV1 = "revoke_credential_authority"
      PurgeRetainSame RetentionPurgeActionV1 = "retain_same"
      PurgeUnlinkReference RetentionPurgeActionV1 = "unlink_reference"
      PurgeUnlinkThenExactRefcountGC RetentionPurgeActionV1 = "unlink_then_exact_refcount_gc"
  )

  type RetentionProofKindV1 string
  const (
      ProofClientAckOrExpiry RetentionProofKindV1 = "client_ack_or_expiry"
      ProofCredentialAuthorityZero RetentionProofKindV1 = "credential_authority_zero"
      ProofMultipartUploadZero RetentionProofKindV1 = "multipart_upload_zero"
      ProofNone RetentionProofKindV1 = "none"
      ProofObjectVersionZero RetentionProofKindV1 = "object_version_zero"
      ProofOwnerAdapterZero RetentionProofKindV1 = "owner_adapter_zero"
      ProofParticipantTypedZero RetentionProofKindV1 = "participant_typed_zero"
      ProofReferenceInventoryZero RetentionProofKindV1 = "reference_inventory_zero"
      ProofSignedInventoryCheckpoint RetentionProofKindV1 = "signed_inventory_checkpoint"
      ProofWorkspaceZero RetentionProofKindV1 = "workspace_zero"
  )

  type RetentionSurvivorKindV1 string
  const (
      SurvivorIdentityFreeAggregate RetentionSurvivorKindV1 = "identity_free_aggregate"
      SurvivorMediaDestruction RetentionSurvivorKindV1 = "media_destruction"
      SurvivorMinimalAudit RetentionSurvivorKindV1 = "minimal_audit"
      SurvivorNone RetentionSurvivorKindV1 = "none"
      SurvivorOriginTombstone RetentionSurvivorKindV1 = "origin_tombstone"
      SurvivorSameLeaf RetentionSurvivorKindV1 = "same_leaf"
  )

  type ForbiddenResidueKindV1 string
  const (
      ResidueActorPrincipal ForbiddenResidueKindV1 = "actor_principal"
      ResidueCredential ForbiddenResidueKindV1 = "credential"
      ResidueExternalDeliveryIdentity ForbiddenResidueKindV1 = "external_delivery_identity"
      ResidueFreeText ForbiddenResidueKindV1 = "free_text"
      ResidueGovernancePrincipal ForbiddenResidueKindV1 = "governance_principal"
      ResidueOperationReference ForbiddenResidueKindV1 = "operation_reference"
      ResidueOriginIdentity ForbiddenResidueKindV1 = "origin_identity"
      ResidueProductContent ForbiddenResidueKindV1 = "product_content"
      ResidueProductFilename ForbiddenResidueKindV1 = "product_filename"
      ResidueRawPath ForbiddenResidueKindV1 = "raw_path"
      ResidueRecoverySourceIdentity ForbiddenResidueKindV1 = "recovery_source_identity"
      ResidueSafeURL ForbiddenResidueKindV1 = "safe_url"
      ResidueStableRecordObjectIdentity ForbiddenResidueKindV1 = "stable_record_object_identity"
  )

  type PurgeParticipantRuntimeV1 string
  const (
      ParticipantRuntimeGo PurgeParticipantRuntimeV1 = "go"
      ParticipantRuntimeWeb PurgeParticipantRuntimeV1 = "web"
  )

  type PurgeProofRequirementV1 struct {
      Kind RetentionProofKindV1
      SchemaID string // empty only for ProofNone
  }

  type PurgeCapabilityV1 struct {
      ID string
      SurfaceKind RetentionSurfaceKindV1
      Action RetentionPurgeActionV1
      RequiredProofs []PurgeProofRequirementV1 // canonical tuple order, never independent projections
  }

  type PurgeParticipantV1 struct {
      Version uint16
      ID string
      OwnerChildID uint16
      Runtime PurgeParticipantRuntimeV1
      DispatchBindingID string
      Capabilities []PurgeCapabilityV1 // exact, raw-byte-sorted unique tuples
      IdempotencyScope string // owner_version_and_fence_epoch only in v1
      RequiresReservationFence bool
  }

  type RetentionCanonicalIdentityV1 struct {
      SchemaID string
      CanonicalBytes []byte
      Digest [32]byte
  }

  type RetentionPurgeTargetSelectorV1 struct {
      SurfaceKind RetentionSurfaceKindV1
      SchemaID string
      CanonicalBytes []byte
      Digest [32]byte
  }

  type RetentionPurgeDispatchRequestV1 struct {
      Version uint16
      OperationIdentity RetentionCanonicalIdentityV1
      OwnerIdentity RetentionCanonicalIdentityV1
      ExpectedOwnerVersion uint64
      ReservationFenceEpoch uint64
      LifecyclePolicyID string
      StageOrdinal uint16
      SurfaceAddress RetentionSurfaceAddressV1
      Target RetentionPurgeTargetSelectorV1
      PurgeParticipantID, CapabilityID string
      Deadline time.Time
  }

  type TypedPurgeProofV1 struct {
      Kind RetentionProofKindV1
      SchemaID string
      CanonicalBytes []byte
      Digest [32]byte
  }
  type TypedPurgeProofSetV1 struct {
      PurgeParticipantID, CapabilityID string
      Proofs []TypedPurgeProofV1
  }
  type PurgeParticipantDispatchV1 func(context.Context, RetentionPurgeDispatchRequestV1) (TypedPurgeProofSetV1, error)
  type PurgeParticipantBindingV1 struct {
      Row PurgeParticipantV1
      GoDispatch PurgeParticipantDispatchV1
      WebModuleExport string
  }

  type RetentionStageDispositionV1 string
  const (
      StageRetainUnchanged RetentionStageDispositionV1 = "retain_unchanged"
      StageTransition RetentionStageDispositionV1 = "transition"
  )

  type RetentionPolicyStageBindingV1 struct {
      StageOrdinal uint16
      AnchorSurface *RetentionSurfaceAddressV1
      ExpirySurface *RetentionSurfaceAddressV1
      Disposition RetentionStageDispositionV1
      PurgeParticipantID, CapabilityID string
      RequiredProofs []PurgeProofRequirementV1 // exact-equal resolved capability tuple
      SurvivorKind RetentionSurvivorKindV1
      ForbiddenAfterTransition []ForbiddenResidueKindV1 // enum-sorted unique for this stage
  }

  type RetentionPolicyRowV1 struct {
      Version uint16
      OwnerChildID uint16
      SurfaceAddress RetentionSurfaceAddressV1
      RetentionClass RetentionClassV1
      AllowedSemantic AllowedSemanticV1
      LifecyclePolicyID string
      StageBindings []RetentionPolicyStageBindingV1 // applicable template ordinals sorted unique
  }

  type GitObjectIDV1 struct {
      Algorithm string // sha1 only in v1
      Bytes [20]byte
  }

  type RetentionRegistryStateV1 struct {
      CanonicalSchemaRegistryDigest [32]byte
      RetentionPolicyRegistryDigest [32]byte
      RetentionLifecycleRegistryDigest [32]byte
      PurgeParticipantRegistryDigest [32]byte
      S3SurfaceGrammarDigest [32]byte
      ManagedFilesystemGrammarDigest [32]byte
      ManagedFileTerminalRegistryDigest [32]byte
      ManagedClientStorageRegistryDigest [32]byte
  }

  type ChildSurfaceOwnerEntryV1 struct {
      Version uint16
      ChildID, MergeOrdinal uint16
      ChildSlug string
      DirectDependencies []uint16
      MigrationOwners []string
      PostgreSQLFamilies []string
      CanonicalSchemaFamilies []string
      S3Families []string
      ManagedFilesystemFamilies []string
      ManagedClientFamilies []string
      LifecyclePolicyIDs []string
      PurgeParticipantIDs []string
      ExpectedSurfaceKinds []string
      InitialDeltaRequirement string // required_nonempty only for this program
  }

  type RetentionAcceptanceKeyStatusV1 string
  const (
      RetentionAcceptanceKeyActive RetentionAcceptanceKeyStatusV1 = "active"
      RetentionAcceptanceKeyRetired RetentionAcceptanceKeyStatusV1 = "retired"
      RetentionAcceptanceKeyCompromised RetentionAcceptanceKeyStatusV1 = "compromised"
  )

  type RetentionAcceptanceKeyV1 struct {
      KeyID string
      PublicKey [32]byte
      ValidFrom, ValidUntil time.Time
      Status RetentionAcceptanceKeyStatusV1
  }

  type RetentionRequiredCheckPolicyEntryV1 struct {
      Context, WorkflowPath string
      WorkflowBlob GitObjectIDV1
      IssuerAppID uint64
      ImmutableActionSetDigest [32]byte
  }

  type RetentionAcceptancePolicyBodyV1 struct {
      Version uint16
      RepositoryID uint64
      ProgramID, TargetRef string
      MergeMethod string // merge_commit only
      OwnerMatrixDigest [32]byte
      MergeOrder []uint16 // exact 1,2,3,4,9,5,6,7,8,10,11 child IDs
      RequiredChecks []RetentionRequiredCheckPolicyEntryV1
      TrustedAcceptanceWorkflowPath string
      TrustedAcceptanceWorkflowBlob GitObjectIDV1
      AcceptanceWorkflowActionSetDigest [32]byte
      MetadataVerifierWorkflowPath string
      MetadataVerifierWorkflowBlob GitObjectIDV1
      MetadataVerifierActionSetDigest [32]byte
      ScannerID, ScannerVersion string
      ScannerArtifactDigest, ScannerRulesDigest [32]byte
      SignerID, SignerVersion string
      SignerArtifactDigest [32]byte
      SourceClaimArtifactName string
      MetadataRoot, MetadataWriterIdentity string
      MetadataWriterAppID uint64
      MaximumMergeToAcceptanceSeconds uint64
      GenesisApproverPolicyDigest [32]byte
      Keys []RetentionAcceptanceKeyV1
      ActiveKeyID string
  }

  type RetentionAcceptancePolicyV1 struct {
      Body RetentionAcceptancePolicyBodyV1
      PolicyDigest [32]byte
  }

  type RetentionAcceptanceGenesisBodyV1 struct {
      Version uint16
      RepositoryID uint64
      ProgramID string
      BaseCommit, BaseTree GitObjectIDV1
      SourceCommit, SourceTree GitObjectIDV1
      AcceptancePolicyDigest, OwnerMatrixDigest [32]byte
      ScannerArtifactDigest, ScannerRulesDigest, SignerArtifactDigest [32]byte
      AcceptanceWorkflowBlob, MetadataVerifierWorkflowBlob GitObjectIDV1
      IssuedAt, ExpiresAt time.Time
  }

  type RetentionAcceptanceGenesisSignatureV1 struct {
      Version uint16
      Purpose string // retention_acceptance_genesis_v1 only
      GenesisBodyDigest, ApproverPolicyDigest [32]byte
      SignerKeyID string
      Signature [64]byte
  }

  type SignedRetentionAcceptanceGenesisV1 struct {
      Body RetentionAcceptanceGenesisBodyV1
      BodyDigest [32]byte
      Signatures []RetentionAcceptanceGenesisSignatureV1
      GenesisDigest [32]byte
  }

  type RetentionRegistryDeltaOperationV1 struct {
      RegistryKind string // one of the eight RetentionRegistryStateV1 roots
      CanonicalKey []byte // 1..4096 bytes
      Operation string // add|replace|remove
      BeforeRowDigest [32]byte
      AfterCanonicalRow []byte // 0..1048576 bytes
      AfterRowDigest [32]byte
  }

  type RetentionRegistryDeltaV1 struct {
      Version uint16
      Operations []RetentionRegistryDeltaOperationV1 // registry-kind/key sorted unique
      AddedCount, ReplacedCount, RemovedCount uint64
      DeltaDigest [32]byte
  }

  type ChildRetentionSourceClaimBodyV1 struct {
      Version uint16
      RepositoryID uint64
      ChildID, MergeOrdinal uint16
      ChildSlug string
      ClaimRevision, PRNumber uint64
      BaseRef string // refs/heads/main only
      BaseCommit, BaseTree GitObjectIDV1
      SourceCommit, SourceTree GitObjectIDV1
      GitTreeDeltaDigest [32]byte
      BaseProductionInputMerkleDigest [32]byte
      SourceProductionInputMerkleDigest [32]byte
      OwnerMatrixDigest, OwnerMatrixEntryDigest [32]byte
      PreviousAcceptanceDigest [32]byte
      GenesisAuthorization *SignedRetentionAcceptanceGenesisV1 // required only for ordinal 1
      DeltaMode string // registry_delta; no_new_surface only for later maintenance
      RegistryBefore RetentionRegistryStateV1
      RegistryDelta RetentionRegistryDeltaV1
      RegistryDeltaDigest [32]byte
      RegistryAfter RetentionRegistryStateV1
      BaseObservedInventoryDigest [32]byte
      SourceObservedInventoryDigest [32]byte
      DeclaredSourceInventoryDigest [32]byte
      ScannerID, ScannerVersion string
      ScannerArtifactDigest, ScannerRulesDigest, ScannerReportDigest [32]byte
      UnclassifiedCount, MissingCount, ExtraCount uint64
  }

  type ChildRetentionSourceClaimV1 struct {
      Body ChildRetentionSourceClaimBodyV1
      ClaimDigest [32]byte
  }

  type RetentionRequiredCheckObservationV1 struct {
      RepositoryID, IssuerAppID, CheckSuiteID uint64
      Context, WorkflowPath string
      WorkflowBlob, TestedBaseCommit, TestedBaseTree GitObjectIDV1
      TestedMergeCommit, TestedMergeTree GitObjectIDV1
      HeadCommit, HeadTree GitObjectIDV1
      RunID, RunAttempt uint64
      Conclusion string // success only
      CompletedAt time.Time
      ArtifactDigest [32]byte
  }

  type ProtectedRetentionAcceptanceRunV1 struct {
      Version uint16
      RepositoryID, IssuerAppID, RunID, RunAttempt uint64
      EventName, TargetRef, WorkflowPath string
      WorkflowBlob, MergeCommit GitObjectIDV1
      ImmutableActionSetDigest [32]byte
      StartedAt, CompletedAt time.Time
  }

  type ChildRetentionMergeAcceptanceBodyV1 struct {
      Version uint16
      RepositoryID uint64
      ChildID, MergeOrdinal uint16
      ChildSlug string
      ClaimRevision, PRNumber uint64
      SourceCommit, SourceTree GitObjectIDV1
      SourceClaim ChildRetentionSourceClaimV1
      SourceClaimDigest, SourceClaimArtifactDigest [32]byte
      MetadataPath string
      RequiredChecks []RetentionRequiredCheckObservationV1
      RequiredChecksDigest [32]byte
      MergeMethod string // merge_commit only
      PreMergeMainCommit, PreMergeMainTree GitObjectIDV1
      MergeCommit, MergeTree GitObjectIDV1
      AcceptedTreeDeltaDigest [32]byte
      BaseProductionInputMerkleDigest [32]byte
      SourceProductionInputMerkleDigest [32]byte
      MergeProductionInputMerkleDigest [32]byte
      OwnerMatrixDigest, OwnerMatrixEntryDigest [32]byte
      PreviousAcceptanceDigest [32]byte
      RegistryBefore RetentionRegistryStateV1
      RegistryDeltaDigest [32]byte
      RegistryAfter RetentionRegistryStateV1
      ObservedInventoryDigest [32]byte
      ScannerID, ScannerVersion string
      ScannerArtifactDigest, ScannerRulesDigest, ScannerReportDigest [32]byte
      AcceptancePolicyDigest [32]byte
      AcceptanceRun ProtectedRetentionAcceptanceRunV1
      MergedAt, AcceptedAt time.Time
  }

  type SignedChildRetentionMergeAcceptanceV1 struct {
      Body ChildRetentionMergeAcceptanceBodyV1
      BodyDigest [32]byte
      AcceptanceKeyID string
      Signature [64]byte
      ReceiptDigest [32]byte
  }


  type ChildRetentionMergeAcceptanceFileV1 struct {
      Version uint16
      Acceptance SignedChildRetentionMergeAcceptanceV1
      FileDigest [32]byte
  }
  ```

  The following 21 rows are the complete immutable initial-program manifest for the eventual `RetentionLifecycleRegistryV1`. The live registry contains only rows whose introducing child has merged, and reaches this exact set only after ordinal 11. Each `StageTemplates` value has the exact field order `ordinal;immediate_triggers;deadline_kind;anchor_role;expiry_role;setting_id;default_seconds;maximum_seconds;upper_bound`; `-` is the canonical empty string/list, trigger lists are comma-separated raw-byte-sorted enum tokens, and `<br>` separates stages. No prose default or implementation-local row may supplement this table.

  | ID | IntroducedByChildID | Kind | ApplicableClasses | StageTemplates |
  |---|---:|---|---|---|
  | `lc_absolute_10m_v1` | `10` | `absolute_expiry` | `ephemeral_registered` | `1;-;fixed_hard_cap_absolute_binding;created_at;expires_at;-;600;600;fixed` |
  | `lc_absolute_15m_v1` | `4` | `absolute_expiry` | `ephemeral_registered` | `1;-;fixed_hard_cap_absolute_binding;created_at;expires_at;-;900;900;fixed` |
  | `lc_absolute_1h_v1` | `10` | `absolute_expiry` | `ephemeral_registered` | `1;-;fixed_hard_cap_absolute_binding;created_at;expires_at;-;3600;3600;fixed` |
  | `lc_absolute_24h_v1` | `10` | `absolute_expiry` | `ephemeral_registered` | `1;-;fixed_hard_cap_absolute_binding;created_at;expires_at;-;86400;86400;fixed` |
  | `lc_comment_redaction_v1` | `9` | `owner_bound` | `live_product_authority` | `1;comment_redacted;none;-;-;-;0;0;none` |
  | `lc_derived_owner_bound_v1` | `1` | `derived_owner_bound` | `live_product_derived` | `1;owner_delete_commit;none;-;-;-;0;0;none` |
  | `lc_draft_last_activity_90d_v1` | `2` | `owner_bound` | `draft_product` | `1;explicit_discard,owner_delete_commit,save_committed;configured_from_binding;last_activity_at;-;record_draft_inactive_retention_seconds;7776000;0;no_hard_cap` |
  | `lc_ephemeral_absolute_expiry_v1` | `1` | `absolute_expiry` | `ephemeral_registered` | `1;-;absolute_binding;-;expires_at;-;0;0;none` |
  | `lc_managed_client_unsynced_24h_v1` | `5` | `managed_client` | `managed_client_buffer` | `1;authorization_revoked,explicit_discard,logout,owner_delete_commit,save_committed,user_switch;fixed_hard_cap_absolute_binding;updated_at;expires_at;-;86400;86400;fixed` |
  | `lc_notification_product_180d_v1` | `9` | `absolute_expiry` | `live_product_derived` | `1;owner_delete_commit;min_configured_hard_cap_absolute_binding;created_at;product_expires_at;record_notification_retention_seconds;15552000;15552000;fixed` |
  | `lc_owner_bound_authority_v1` | `2` | `owner_bound` | `live_product_authority` | `1;owner_delete_commit;none;-;-;-;0;0;none` |
  | `lc_owner_reference_zero_24h_v1` | `3` | `owner_bound` | `live_product_authority,live_product_derived` | `1;owner_delete_commit;fixed_hard_cap_absolute_binding;owner_reference_zero_at;expires_at;-;86400;86400;fixed` |
  | `lc_permanent_immutable_governance_v1` | `1` | `permanent` | `immutable_governance` | `1;-;none;-;-;-;0;0;none` |
  | `lc_permanent_immutable_ledger_v1` | `1` | `permanent` | `immutable_ledger` | `1;-;none;-;-;-;0;0;none` |
  | `lc_permanent_minimal_audit_v1` | `1` | `permanent` | `minimal_audit` | `1;-;none;-;-;-;0;0;none` |
  | `lc_permanent_origin_tombstone_v1` | `10` | `permanent` | `minimal_audit` | `1;-;none;-;-;-;0;0;none` |
  | `lc_platform_mutation_complete_30d_v1` | `1` | `receipt_compaction` | `operational_30d` | `1;-;fixed_from_binding;mutation_completed_at;-;-;2592000;2592000;fixed` |
  | `lc_recoverability_bound_v1` | `1` | `recoverability_bound` | `recoverability_window` | `1;-;absolute_binding;-;source_recoverable_until;-;0;0;none` |
  | `lc_storage_control_permanent_v1` | `1` | `permanent` | `storage_control` | `1;-;none;-;-;-;0;0;none` |
  | `lc_telemetry_max_30d_v1` | `11` | `absolute_expiry` | `operational_30d` | `1;-;min_configured_hard_cap_absolute_binding;captured_at;raw_event_delete_at;telemetry_sink_retention_seconds;2592000;2592000;fixed` |
  | `lc_verified_purge_24h_30d_v1` | `1` | `receipt_compaction` | `live_safety,operational_24h,operational_30d` | `1;-;fixed_from_binding;retention_eligible_at;-;-;86400;86400;fixed`<br>`2;-;fixed_from_binding;retention_eligible_at;-;-;2592000;2592000;fixed` |

  The following 24 rows are the complete immutable initial-program manifest for the eventual `PurgeParticipantRegistryV1`; the live registry grows only when each owner child merges and reaches this set after ordinal 11. Every row fixes `Version=1` and `IdempotencyScope=owner_version_and_fence_epoch`. The four projection cells are non-authoritative raw-byte-sorted unions calculated from each row's exact `Capabilities` tuples; `-` is the canonical zero-item array. No projection cell authorizes a Cartesian combination; only a declared capability ID with one surface kind, one action and its complete proof-kind+schema requirement set is executable. `DispatchBindingID` is stable registry data, while executable equality is proven by the runtime binding maps below.

  | ID | OwnerChildID | Runtime | DispatchBindingID | SurfaceProjection | ActionProjection | ProofKindProjection | ProofSchemaProjection | ReservationFence |
  |---|---:|---|---|---|---|---|---|---|
  | `pp_activity_v1` | `7` | `go` | `activity.purge.v1` | `canonical_leaf,postgresql_column` | `delete_row` | `owner_adapter_zero` | `activity-purge-receipt/v1` | `true` |
  | `pp_attachments_v1` | `3` | `go` | `attachments.purge.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `abort_multipart_uploads,delete_object_all_versions,delete_row,null_leaf,purge_managed_file,unlink_reference` | `multipart_upload_zero,object_version_zero,participant_typed_zero,reference_inventory_zero` | `attachment-purge-receipt/v1` | `true` |
  | `pp_backup_v1` | `11` | `go` | `integration.backup_purge.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `abort_multipart_uploads,delete_object_all_versions,delete_row,purge_managed_file,reduce_to_survivor` | `multipart_upload_zero,object_version_zero,participant_typed_zero,signed_inventory_checkpoint,workspace_zero` | `integration-purge-receipt/v1` | `true` |
  | `pp_blob_gc_v1` | `3` | `go` | `attachments.blob_gc.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `abort_multipart_uploads,delete_object_all_versions,purge_managed_file,unlink_then_exact_refcount_gc` | `multipart_upload_zero,object_version_zero,reference_inventory_zero` | `attachment-purge-receipt/v1` | `true` |
  | `pp_collaboration_v1` | `9` | `go` | `collaboration.purge.v1` | `canonical_leaf,postgresql_column` | `delete_row,null_leaf,reduce_to_survivor,unlink_reference` | `participant_typed_zero` | `record-collaboration-purge-receipt/v1` | `true` |
  | `pp_content_processor_v1` | `3` | `go` | `attachments.content_processor_purge.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `abort_multipart_uploads,delete_object_all_versions,delete_row,purge_managed_file` | `multipart_upload_zero,object_version_zero,participant_typed_zero,workspace_zero` | `attachment-purge-receipt/v1` | `true` |
  | `pp_deletion_ledger_v1` | `1` | `go` | `deletion_ledger.retention.v1` | `canonical_leaf,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `retain_same` | `none` | `-` | `false` |
  | `pp_evidence_payload_gc_v1` | `4` | `go` | `evidence.payload_gc.v1` | `canonical_leaf,postgresql_column` | `delete_row,unlink_then_exact_refcount_gc` | `reference_inventory_zero` | `evidence-purge-receipt/v1` | `true` |
  | `pp_evidence_v1` | `4` | `go` | `evidence.purge.v1` | `canonical_leaf,postgresql_column` | `delete_row,null_leaf,reduce_to_survivor,unlink_reference` | `participant_typed_zero,reference_inventory_zero` | `evidence-purge-receipt/v1` | `true` |
  | `pp_integration_janitor_v1` | `11` | `go` | `integration.janitor.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `abort_multipart_uploads,delete_object_all_versions,delete_row,null_leaf,purge_managed_file,reduce_to_survivor` | `multipart_upload_zero,object_version_zero,participant_typed_zero,signed_inventory_checkpoint,workspace_zero` | `integration-purge-receipt/v1` | `true` |
  | `pp_legacy_v1` | `10` | `go` | `portability.legacy_purge.v1` | `canonical_leaf,postgresql_column` | `delete_row,null_leaf,reduce_to_survivor` | `participant_typed_zero` | `record-portability-purge-receipt/v1` | `true` |
  | `pp_markdown_client_buffer_v1` | `5` | `web` | `markdown.client_buffer_purge.v1` | `canonical_leaf,managed_client_leaf` | `purge_client_entry` | `client_ack_or_expiry` | `retention-control/v1` | `true` |
  | `pp_portability_v1` | `10` | `go` | `portability.purge.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `abort_multipart_uploads,delete_object_all_versions,delete_row,null_leaf,purge_managed_file,reduce_to_survivor,unlink_reference` | `multipart_upload_zero,object_version_zero,participant_typed_zero,reference_inventory_zero,workspace_zero` | `record-portability-purge-receipt/v1` | `true` |
  | `pp_record_notify_v1` | `9` | `go` | `collaboration.notification_purge.v1` | `canonical_leaf,postgresql_column` | `delete_row,null_leaf,reduce_to_survivor` | `participant_typed_zero` | `record-collaboration-purge-receipt/v1` | `true` |
  | `pp_record_platform_v1` | `1` | `go` | `record_platform.retention.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `delete_row,null_leaf,reduce_to_survivor` | `participant_typed_zero` | `record-platform/v1` | `true` |
  | `pp_record_rollout_v1` | `11` | `go` | `integration.rollout_purge.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `abort_multipart_uploads,delete_object_all_versions,delete_row,null_leaf,purge_managed_file,reduce_to_survivor` | `multipart_upload_zero,object_version_zero,participant_typed_zero,workspace_zero` | `integration-purge-receipt/v1` | `true` |
  | `pp_record_search_v1` | `6` | `go` | `search.purge.v1` | `canonical_leaf,postgresql_column` | `delete_row` | `owner_adapter_zero` | `record-search-rebuild-receipt/v1` | `true` |
  | `pp_record_security_v1` | `11` | `go` | `integration.security_purge.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `abort_multipart_uploads,delete_object_all_versions,delete_row,null_leaf,purge_managed_file,reduce_to_survivor` | `multipart_upload_zero,object_version_zero,participant_typed_zero,workspace_zero` | `integration-purge-receipt/v1` | `true` |
  | `pp_records_core_v1` | `2` | `go` | `records_core.purge.v1` | `canonical_leaf,postgresql_column` | `delete_row,null_leaf,reduce_to_survivor,unlink_reference` | `participant_typed_zero` | `record-core-purge-receipt/v1` | `true` |
  | `pp_recovery_control_v1` | `1` | `go` | `recovery_control.retention.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `abort_multipart_uploads,delete_object_all_versions,delete_row,null_leaf,purge_managed_file,reduce_to_survivor,revoke_credential_authority` | `credential_authority_zero,multipart_upload_zero,object_version_zero,participant_typed_zero,signed_inventory_checkpoint,workspace_zero` | `recovery-governance/v1` | `true` |
  | `pp_restore_v1` | `11` | `go` | `integration.restore_purge.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `abort_multipart_uploads,delete_object_all_versions,delete_row,purge_managed_file` | `multipart_upload_zero,object_version_zero,participant_typed_zero,signed_inventory_checkpoint,workspace_zero` | `integration-purge-receipt/v1` | `true` |
  | `pp_retain_same_v1` | `1` | `go` | `retention.retain_same.v1` | `canonical_leaf,managed_client_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `retain_same` | `none` | `-` | `false` |
  | `pp_retention_v1` | `1` | `go` | `retention.janitor.v1` | `canonical_leaf,managed_file,postgresql_column,s3_control_property,s3_key_segment,s3_metadata` | `delete_row,null_leaf,reduce_to_survivor` | `participant_typed_zero` | `retention-control/v1` | `true` |
  | `pp_vps_overview_v1` | `7` | `go` | `activity.vps_overview_purge.v1` | `canonical_leaf,postgresql_column` | `delete_row` | `owner_adapter_zero` | `activity-purge-receipt/v1` | `true` |

  `RetentionSurfaceAddressV1` requires exactly one non-nil arm. Every address token, `OwnerChildID`, lifecycle ID, stage ordinal, disposition, participant ID, capability ID, exact action+proof requirement tuple, survivor and typed residue value exact-resolves once in the generated eight-root state; raw path/URL and free-form owner/participant names are forbidden. `ManagedClientLeaf.Engine` is exactly `indexed_db`; local/session storage, CacheStorage and service-worker caches have no content address arm.

  Lifecycle rows are reusable clock templates only. Every concrete `RetentionPolicyRowV1.StageBindings` is a raw-byte-stable, strictly increasing subset of valid template ordinals. A binding supplies exact anchor/expiry surfaces, disposition, purge participant, capability ID, complete proof requirements, survivor and stage-specific forbidden residue. Omitting a template ordinal means only “this surface has no transition at that stage”; it never inherits an action or survivor from another row. `retain_unchanged` is explicit and requires the participant's exact `retain_same + none` capability with `SurvivorSameLeaf`; `transition` requires a non-retain action unless the closed policy fixture explicitly proves a same-value governance row. Across the complete policy registry every lifecycle template ordinal must have at least one reverse consumer. A template never chooses physical actions globally. `none` requires nil anchor/expiry, no setting, zero seconds and `upper_bound=none`; `fixed_from_binding` requires one anchor, nil expiry, equal nonzero default/maximum and `fixed`; `fixed_hard_cap_absolute_binding` additionally requires one expiry and verifies `expiry=anchor+seconds`; `configured_from_binding` requires an anchor and setting plus either `fixed` with nonzero maximum or `no_hard_cap` with zero maximum; `absolute_binding` requires only an expiry; `min_configured_hard_cap_absolute_binding` requires anchor+expiry+setting, fixed maximum, and verifies the persisted expiry equals the earlier configured/cap deadline. Missing/extra role bindings, duplicate/unknown stage ordinals, overflow, wrong formula, a globally unconsumed template stage, projection-only capability, or a stage proof set not exact-equal to its declared participant capability fail generation/readiness.

  `RetentionLifecycleRegistryV1` and `PurgeParticipantRegistryV1` are raw-byte-keyed canonical maps. A lifecycle row is added exactly once by `IntroducedByChildID`, which is the first referencing child in merge order; later children may reference it but cannot re-add/replace it. A participant row is added exactly once by `OwnerChildID`. The union of owner-matrix lifecycle/participant IDs must equal the tables above with counts exactly 21/24, and the reverse consumer index for every lifecycle row, every lifecycle stage ordinal, every participant and every exact capability tuple must be nonempty.

  Executable binding is not inferred from `DispatchBindingID`. At every merge ordinal, the generated Go binding map must equal exactly the `Runtime=go` rows present in that live registry, each with byte-identical metadata, non-nil `GoDispatch`, empty `WebModuleExport` and the table's binding ID; after ordinal 11 this is exactly 23 IDs. The generated Web binding manifest is empty before child 5, then contains exactly `pp_markdown_client_buffer_v1`, resolves `markdown.client_buffer_purge.v1` to the actual exported function `web/src/features/records/purgeIndexedDBDraftBufferV1`, and has nil Go dispatch. Unknown, nil, extra, premature or cross-runtime bindings fail build and runtime readiness. Before any side effect, `RetentionPurgeDispatchRequestV1` fully decodes operation/owner/target identities, exact-matches target surface kind and capability tuple, rejects an expired deadline, and reads the expected owner version plus reservation fence. The participant repeats the owner-version/fence check immediately before proof finalization, so a stale owner cannot complete after takeover. `TypedPurgeProofSetV1` must contain exactly the capability's ordered proof requirements: `ProofNone` requires one empty-schema/empty-bytes/zero-digest proof and every other proof must use the tuple-bound schema, consume bounded canonical bytes fully and exact-match its digest. Missing, duplicate, extra or projection-derived proof fails the stage.

  Generation tests are fixed as `TestOwnerMatrixUsesNumericDependencyOrderV1`, `TestOwnerMatrixExpandsAndRawByteSortsKindsV1`, `TestOwnerMatrixRawByteSortsStringListsV1`, `TestLifecycleRegistryHasExactly21UniqueInitialIDsV1`, `TestPurgeParticipantRegistryHasExactly24UniqueInitialIDsV1`, `TestRegistryIntroductionOwnerMatchesMergeOrderV1`, `TestEveryPolicyRowResolvesOneLifecycleAndParticipantV1`, `TestLifecycleReverseConsumerIndexHasNoOrphansV1`, `TestParticipantRegistryEqualsExecutableDispatchMapV1`, `TestParticipantExactCapabilityTuplesMatchConsumersV1` and `TestGeneratedRegistryArtifactsAreByteReproducibleV1`. Negative fixtures remove/add/duplicate one row or binding, change owner, nil a dispatch, add an extra dispatch, use a projection-only or unsupported exact kind/action/proof-schema tuple, leave a Kind alias unexpanded, lexicographically sort numeric dependencies, or add a consumer surface/stage without a tuple; each must fail before permanent delete readiness.

  All policy/claim/acceptance bodies use fixed magic/version/field order, big-endian integers, length-prefixed strings and enum-sorted unique arrays except semantically order-bearing fields, which retain their declared order; specifically `MergeOrder` is exact `1,2,3,4,9,5,6,7,8,10,11` and must never be enum- or numeric-sorted. v1 `GitObjectIDV1` is exactly algorithm `sha1` plus 20 raw bytes. `PolicyDigest=SHA-256("HOUFENG-RETENTION-ACCEPTANCE-POLICY-V1" || canonical-policy-body)` and the policy requires `RepositoryID` plus exact `refs/heads/main`, merge method `merge_commit`, owner-matrix digest, merge order, required-check contexts/workflow blobs/issuer App IDs/action-set digests, trusted acceptance and trusted-base metadata-verifier workflow blobs/action sets, scanner/rules/signer artifact digests, metadata writer/root/App ID, maximum merge-to-acceptance lag and sorted unique key history. Exactly one non-compromised key is active. Key windows are half-open `[ValidFrom,ValidUntil)` UTC-microsecond intervals; `capk-sha256-*` exact-matches public bytes, a signer may create only with the policy's active key at the actual protected-run completion time, retired keys verify only already-created receipts within their original interval, and compromised/unknown/reused key IDs never verify.

  Ordinal 1 is the only genesis exception because it introduces the policy, owner matrix and workflows it must subsequently protect. Before its required check, an offline threshold defined by `genesis-approver-policy.v1` signs `SignedRetentionAcceptanceGenesisV1`, binding the exact base/source trees, policy/matrix/tool/workflow digests and a maximum 24-hour expiry; claim 1 and acceptance 1 embed and reverify that complete wrapper. It authorizes only those genesis bytes, not feature behavior or a later policy. After acceptance 1's metadata PR merges, no child 2..11 and no metadata PR may modify the policy, owner matrix, trusted workflows or tool digests. Any later key/tool/workflow/policy change requires a separately approved, non-child `record-retention-policy-rotation` metadata PR whose old-policy threshold authorizes the complete successor and whose receipt is linked before another child starts; that rotation is not an escape for the frozen initial merge order.

  `ClaimDigest=SHA-256("HOUFENG-CHILD-RETENTION-SOURCE-CLAIM-V1" || canonical-claim-body)`. `ClaimRevision` is exactly `1`; it is not a run counter. The source claim is deterministic, contains no generated time/run ID/signature, never reads an acceptance private key and is emitted only as a required-check artifact, so it does not self-reference its source tree. `GitTreeDeltaDigest` hashes the base→source sorted `(path,mode,blob-OID)` change list without rename detection or text-patch ambiguity. Separate base/source production-input Merkle digests cover the complete sorted `(input-kind,path,mode,blob-OID)` set consumed by the scanner, including unchanged inputs. The claim's base must already contain the preceding acceptance file for ordinals 2..11; ordinal 1 alone uses a zero previous digest and the live genesis wrapper.

  Each `RetentionRegistryDeltaOperationV1` preserves its canonical key and after-row bytes, so verification applies actual operations rather than trusting a digest. `add` requires zero before digest and nonempty after bytes/digest; `replace` requires both digests and different bytes; `remove` requires nonzero before digest and empty after bytes/zero after digest. Counts are recomputed from the sorted unique operation list and `DeltaDigest` covers the complete list. For initial ordinals 1..11, every operation is `add`, `AddedCount>0`, `ReplacedCount=RemovedCount=0`, each row falls inside the exact owner entry, and applying the operations to every before root must byte-for-byte produce every after root. Later `no_new_surface` requires zero operations/counts and all eight roots unchanged.

  Every required-check observation binds repository and issuer App, exact policy context/workflow/action set, check suite/run, head commit/tree, tested base commit/tree and tested merge-group or PR merge-ref commit/tree. For each required context, the highest attempt overall—not the latest successful subset—must be completed `success`; a newer queued/running/cancelled/failure attempt invalidates the claim. The complete sorted observation list and digest are embedded in the acceptance rather than left only in Git-host metadata.

  Acceptance signing input is exact bytes `"HOUFENG-CHILD-RETENTION-MERGE-ACCEPTANCE-SIGNATURE-V1" || body_digest || acceptance_policy_digest || u32be(len(key_id)) || key_id`; `ReceiptDigest` hashes the complete wrapper excluding itself and `FileDigest=SHA-256("HOUFENG-CHILD-RETENTION-MERGE-ACCEPTANCE-FILE-V1" || canonical-file-without-file-digest)`. The body embeds the complete source claim and separately repeats its claim/artifact digests; every repeated child/source/base/owner/registry/scanner/input-Merkle field must exact-match. `MetadataPath` is exactly `attestations/record-retention/v1/<two-digit-merge-ordinal>-<child-slug>/<40-lowercase-hex-feature-merge-sha>.acceptance.v1` and is signed. `MergedAt` is the merge commit's canonical UTC-microsecond committer timestamp; `AcceptedAt` is the actual trusted protected-run completion time and must equal `AcceptanceRun.CompletedAt`, fall within the active key's half-open interval and be no earlier than merge or later than the policy maximum. A rerun first fetches any signed artifact for the same policy-bound run/feature SHA and byte-compares it; only proven absence may create a new acceptance with the then-active key. A retired key can never create a late receipt merely because the merge timestamp was old.

  Credentials are three-way disjoint. The scanner sandbox has a read-only merge tree/report output and no signing key or Git write token. The no-network signer has only the strict regular/no-follow/bounded 0400 Ed25519 key plus inherited report/output FDs, no checkout and no Git/API token. The metadata writer has only the policy-bound PR App token and signed file bytes, no key and no scanner checkout. The metadata verifier runs the pinned trusted-base workflow/action set, fetches the proposed file as bounded data without executing branch code, and has neither signing key nor write token. Cross-possession, unexpected filesystem/network access, wrong App/workflow/action digest or policy-file change fails before signing/writing.

  This program's 11 initial owner entries all require `DeltaMode=registry_delta`, `AddedCount>0`, zero replace/remove counts and at least one concrete surface row; `no_new_surface` is legal only in a later maintenance claim when all eight registry roots are unchanged and the scanner reports zero added/changed/removed surface. `PreviousAcceptanceDigest` is zero only for merge ordinal 1 and otherwise exact-matches the preceding accepted receipt. Registry evolution is verified in merge order `1,2,3,4,9,5,6,7,8,10,11` as `before + canonical delta = after`; sorted final union is computed only after the chain succeeds, so replacement/removal/base drift cannot hide behind set ordering.

  `ForbiddenResidueKindV1` is closed to `product_content|product_filename|safe_url|raw_path|free_text|credential|stable_record_object_identity|origin_identity|actor_principal|governance_principal|operation_reference|recovery_source_identity|external_delivery_identity`. `RequiredForbiddenAfterTransitionV1` is a generated exhaustive table keyed by exact `(RetentionClassV1, AllowedSemanticV1, LifecyclePolicyID, StageOrdinal, RetentionSurvivorKindV1)` and returns the only legal sorted set for that applicable stage. The stage binding's persisted `ForbiddenAfterTransition` is a canonical echo, not an author-selected subset: generator, scanner and runtime readiness independently recompute it and require byte-for-byte equality; omitting or adding one kind fails build/readiness. Thus a minimal deletion audit may retain actor only because its generated survivor row excludes `actor_principal`, while notification, collaboration, portability and provider rows cannot omit any association they promise to remove. The scanner unions the generated set per transitioned row; a digest equal to forbidden raw bytes never exempts the raw-value scan. The legal class/semantic combinations are likewise a generated exact code table, not prose. Define `BaseAll={encoded_container,schema_discriminator,namespace,closed_enum,digest_hash,counter,sequence_generation,timestamp}` and freeze `AllowedPairsV1` as:

  | `RetentionClassV1` | allowed additions to `BaseAll` |
  |---|---|
  | `immutable_ledger` | `record_object_identity,origin_identity,actor_principal,operation_reference,authorization_floor,route_code,reason_code` |
  | `immutable_governance` | `governance_principal,governance_mutation_identity,recovery_source_identity,governance_policy_material,schema_migration_filename,cryptographic_challenge,public_key,signature,storage_integrity,storage_retention,storage_location_component` |
  | `minimal_audit` | `record_object_identity,origin_identity,actor_principal,operation_reference,route_code,reason_code,identity_free_aggregate,media_destruction_identity` |
  | `live_product_authority` | `record_object_identity,origin_identity,actor_principal,operation_reference,authorization_floor,product_text_content,product_binary_content,product_canonical_payload,product_display_metadata,product_filename,product_safe_url,live_route_reference` |
  | `live_product_derived` | `record_object_identity,origin_identity,actor_principal,operation_reference,authorization_floor,external_delivery_identity,derived_projection_content,product_display_metadata,product_safe_url,live_route_reference` |
  | `draft_product` | `record_object_identity,actor_principal,owner_lease_identity,operation_reference,product_text_content,product_binary_content,product_canonical_payload,product_display_metadata,product_filename,product_safe_url,live_route_reference` |
  | `managed_client_buffer` | `record_object_identity,actor_principal,product_text_content,product_canonical_payload,product_display_metadata,product_safe_url` |
  | `live_safety` | `record_object_identity,origin_identity,actor_principal,governance_principal,owner_lease_identity,operation_reference,governance_mutation_identity,identity_free_aggregate` |
  | `operational_24h` | `record_object_identity,origin_identity,actor_principal,governance_principal,owner_lease_identity,operation_reference,governance_mutation_identity,recovery_source_identity,external_delivery_identity,storage_location_component` |
  | `operational_30d` | `record_object_identity,origin_identity,actor_principal,governance_principal,operation_reference,governance_mutation_identity,recovery_source_identity,external_delivery_identity,route_code,reason_code,bounded_error_code,identity_free_aggregate,storage_location_component` |
  | `recoverability_window` | `record_object_identity,origin_identity,actor_principal,governance_principal,operation_reference,governance_mutation_identity,recovery_source_identity,reason_code,governance_policy_material,cryptographic_challenge,public_key,signature,managed_content_payload,media_destruction_identity,storage_integrity,storage_retention,storage_location_component` |
  | `storage_control` | `governance_principal,storage_integrity,storage_retention,storage_location_component` |
  | `ephemeral_registered` | `record_object_identity,origin_identity,actor_principal,governance_principal,owner_lease_identity,operation_reference,governance_mutation_identity,recovery_source_identity,reason_code,governance_policy_material,cryptographic_challenge,public_key,signature,managed_content_payload,encrypted_key_material,bounded_error_code,storage_integrity,storage_retention,storage_location_component` |

  `BaseAll` describes only a physical container/discriminator or the listed content-free primitive; it never grants its nested bytes a policy. Every decoded leaf still needs its own exact policy row, every enum must occur in at least one legal pair, and every concrete row must match exactly one listed pair. `cryptographic_challenge` is exactly 32 random bytes in a purpose-bound signed schema; it is never rendered, searched, logged or mislabeled as a digest/counter. `managed_content_payload` exists only on registered `recoverability_window|ephemeral_registered` surfaces with absolute expiry, inventory and purge/ownership receipt; normal product content uses the explicit live/draft/client semantics instead. `encrypted_key_material` is narrower: it is legal only for the bounded wrapped-DEK leaf on `ephemeral_registered`, is never rendered or searched, requires exact key-descriptor inventory and typed destruction proof, and has survivor `none`. `schema_migration_filename` accepts only the embedded 1…255-byte `.sql` basename grammar and never a user filename. `storage_location_component` is only a platform-generated root-relative token or a closed canonical infrastructure component such as `S3BucketNameV1`, never an absolute/raw path. `product_safe_url` requires the product URL sanitizer and is allowed only in a distinct product leaf; URLs inside Markdown are part of `product_text_content`. Raw/absolute paths, argv and credentials have no semantic anywhere. User filename/URL/free text are legal only in the explicit live product classes and must transition to survivor `none`; they remain forbidden in immutable governance, minimal survivor and operational telemetry.

  Every `live_product_authority` row is owner-bound: archive retains it, but owner delete reservation prevents new writes and the exact purge participant must prove zero before `online_purged`. Shared Blob/evidence bytes use `unlink_then_exact_refcount_gc`; bytes may remain only under another currently authorized owner and its separate rows. Rebuildable `live_product_derived` projections such as search/activity/overview use an exact `delete_row + owner_adapter_zero` tuple, and an old projector epoch cannot rebuild after the reservation. Notification/delivery/collaboration and other non-rebuildable derived product facts instead use their declared typed purge/unlink/reduction tuples plus association-zero proof; they are never coerced into `owner_adapter_zero`. `draft_product` handles save/discard/owner-delete immediately and default last-activity 90 days with the configured setting; its contract explicitly uses `upper_bound=no_hard_cap` because the approved product requirement permits administrator configuration without a hard maximum. All four product classes require survivor `none` for content semantics.

  `ManagedClientStorageRegistryV1` is a production storage/codec producer for `CanonicalSchemaRegistryV1`, not a third policy source. Foundation implements only its closed interface, generator and conformance harness; it does not pre-register a client content row on behalf of a future Web producer. Child 5 owns the first production codec and nonempty registry delta, `IndexedDBDraftBufferV1`: exact key `(deployment,project,user,draft)`, exact value leaves `version|base_revision|unsynced structured fields|markdown|updated_at|expires_at`, canonical value ≤256 KiB, `expires_at<=updated_at+24h`, and no attachment/evidence bytes. The only address is the registered same-origin IndexedDB database/version/store. Sync, discard, logout, user switch, authorization revoke, owner delete and TTL trigger `purge_client_entry`; online tabs coordinate object-content lease plus BroadcastChannel ack, while an offline device is only `client_ack_or_expiry` and remains explicitly disclosed rather than claimed remotely zero. Playwright enumerates every same-origin store and proves content hits in localStorage/sessionStorage/CacheStorage/service-worker cache are zero.

  Every retained encoded value has its own named schema and production decoder. The registry must include `OriginIdentityV1`, `AuthorizationFloorV1`, `CanonicalMigrationSetBodyV1`, `CanonicalPrivilegeSetBodyV1`, `AppACLManifestPersistedV1`, `ImmutableHeadReceiptV1`, `DeletionLedgerEntryV1`, `DeletionWitnessEntryV1`, `RecoveryTrustEntryV1`, `PlatformMutationPlanV1`, `MutationAuthorizationArtifactV1`, `DetachedApprovalV1`, `ApprovalPolicyV1`, `DomainAttestationPolicyV1`, `MutationBundleV1`, every bundle leaf kind, `SignedAdmissionAdapterPolicyV1`, `SignedAdmissionSnapshotV1`, `SignedCopyReplaySnapshotV1`, `AdmissionDrainReceiptV1`, `DrainContinuationV1`, `FilesystemExclusionProofBodyV1`, `SignedFilesystemExclusionProofV1`, `ActivationInventoryV1`, `SignedRecoveryInventoryV1`, `RecoveryPointManifestV1`, `DependencyInventoryV1`, `MutationCompletionReceiptV1`, `DomainIdentitySetBodyV1`, `DomainIdentitySetV1`, `IdentitySetPrimaryReceiptV1`, `IdentitySetWitnessReceiptV1`, `SignedDomainAttestationV1`, `SignedDomainCandidateChallengeV1`, `SignedDomainCandidatePreparationV1`, `SignedDomainCandidatePossessionV1`, `DomainRotationIntentV1`, `DomainRotationCutoverV1`, `SignedDomainTransferFrameV1`, `DomainCutoverCommandV1`, `SignedDomainCutoverExecutionReceiptV1`, `CandidateNonceReservationSignerKeyDescriptorV1`, `CandidateCleanupVerifierKeyDescriptorV1`, `CandidateControlPolicyChainHeadV1`, `SignedCandidateEphemeralStoragePolicyV1`, `CandidateControlPolicyPrimaryReceiptV1`, `CandidateControlPolicyWitnessReceiptV1`, `CandidateControlAbandonAuthorizationV1` plus its typed primary/witness receipts, `CandidatePurgeContextV1`, all six named `Candidate*RequestPrerequisiteV1` arms, `CandidateRecoveryRequestV1` plus both typed request receipts, `CandidateControlAbandonCompletionV1` plus its typed primary/witness receipts, `SignedCandidateAbandonWorkspaceZeroV1`, `SignedCandidateAEADNonceReservationV1`, `CandidateKeyDestructionEvidenceV1`, `CandidateEncryptedContentV1`, `CandidateEphemeralObjectEnvelopeV1`, every closed candidate payload/inventory/purge schema and every `DomainRotationReceiptV1` payload/wrapper. A `CanonicalObjectEnvelopeV1` or `MutationArtifact` recursively dispatches through a closed kind→one named schema map; its bytes are accepted only after the nested production decoder consumes the entire bounded body and reports the exact leaf set.

  `CanonicalMigrationSetBodyV1` contains only magic `HOUFENG-APP-MIGRATION-SET-V1` followed by ordered unique `entries[].filename` (embedded 1…255-byte `.sql` basename) and `entries[].checksum` (exactly 32 raw bytes). `CanonicalPrivilegeSetBodyV1` contains only magic `HOUFENG-APP-PRIVILEGE-SET-V1`, ordered unique `role_bindings[].subject|catalog_role` and `privileges[].subject|object_class|schema_name|object_identity|column_name|privilege_kind|grant_option` with the class-disjoint encoding frozen in Task 6；subjects are exactly `center_runtime|platform_admin`, catalog roles are distinct validated names and v1 grant option is always false. Neither body contains its own digest.

  The persisted PostgreSQL wrapper is `AppACLManifestPersistedV1`: `manifest_revision`, generated/storage-only `predecessor_revision`, `migrator_catalog_role` encoded in the manifest digest as a `u32/length-prefixed UTF-8 migrator catalog role`, `previous_manifest_digest`, `canonical_migration_set` encoded as `CanonicalMigrationSetBodyV1`, sibling `sorted_migration_set_digest=sha256(exact body bytes)`, `canonical_privilege_set` encoded as `CanonicalPrivilegeSetBodyV1`, sibling `privilege_set_digest=sha256(exact body bytes)`, `manifest_digest` using the existing SQL preimage/order, and storage-only `recorded_at`. Decoder leaf registry rows cover only leaves inside the two bodies; sibling/generated/storage-only columns come only from the PostgreSQL surface inventory and cannot masquerade as body leaves. The manifest preimage remains `revision → length-prefixed migrator catalog role → previous digest → length+canonical migration body → migration digest → length+canonical privilege body → privilege digest`; `predecessor_revision|recorded_at` are excluded. Go golden, SQL CHECK and retention registry are generated from one field-order declaration.

  `ImmutableHeadReceiptV1` is exactly `version`, `artifact_family`, `role_partition`, `scope_hash`, `ordinal_kind`, `ordinal`, `object_kind`, `object_digest`, `previous_receipt_digest`, `recorded_at`, and outer `receipt_digest`. Identity-set primary/witness receipt fields are exactly the V1 bodies defined in Task 13; the witness body includes `primary_receipt_digest`. All ordered signature collections expose `body.version|purpose|scope|key_id|policy_digest|proof_body_digest|signed_at|expires_at` and `signature` as individual indexed leaves. Closed enums replace every former open `object_kind|outcome|credential_kind|authority`; named count fields replace positional arrays. No manifest row may say “typed”, “own schema”, “expected tuple”, “remaining counts” or “wrapper fields” in place of concrete decoder leaves.

  PostgreSQL classification is exact by `(database, schema, relation, column)`. A manually reviewed row is required for ordinary, generated, key, CAS/head and storage-only columns alike; there is no global rule for a column named `created_at`. Tests explicitly require rows for `provisioned_at`, `singleton`, `details_delete_after`, every `created_at|updated_at|recorded_at|witnessed_at`, identity-set `activated_at`, sequence/default/CAS columns and all later migrations. `information_schema` and catalog columns must be bidirectionally equal to the manifest. A storage timestamp/key/head obtains only its own storage/control semantic and cannot confer semantic retention on another leaf.

  Non-canonical survivors are closed: minimal deletion audit keeps only deployment/project, object kind/ID, operation/initiator, closed deletion reason, ledger sequence/hash, requested/online-purged timestamps, external-copy-disclosed flag and final receipt digest; origin tombstone keeps only deployment/project, origin kind/canonical ID, object kind, ledger sequence/hash and deleted time; destroyed recovery inventory reduces to media ID, policy ID, destroyed time/status and destruction receipt digest; notification/telemetry survivors are identity-free channel/route/status/time-bucket counts. `retention_eligible_at` is a persisted, monotonic, one-time timestamp written only by the narrow family function after every required proof is simultaneously verified; it is never inferred from an arbitrary terminal/state column. Deadlines use UTC microseconds and are tested at deadline−1µs, exact deadline and deadline+1µs. Everything else follows this transition matrix:

  | object family | required closed state/proof before clock starts | at +24h | at +30d / final survivor |
  |---|---|---|---|
  | membership, lease and guard | released/expired/superseded, live owner absent, generation fenced | delete owner/principal/object refs, claim/heartbeat/lease detail | current membership/fence aggregate only |
  | content delivery epoch | `online_purge` transaction locks and validates the epoch against reservation, deletes it before publishing `online_purged`, and proves zero rows in the final receipt | already absent | no survivor and no 30-day copy |
  | deletion purge operation | online purge receipt + exact primary/full-witness outcome + continuous applied replay watermark + newer signed inventory checkpoint containing that watermark + no live lease/handler/workspace; narrow function sets `retention_eligible_at` | clear owner/lease/participant transient detail at its own 24h deadline | at `retention_eligible_at+30d`, remove remaining operation/object refs; retain only the explicit minimal audit receipt/time |
  | import/export job and material | completed/failed/cancelled + purge/ownership receipt + no live workspace | remove workspace/path/owner/claim detail | remove record/object/material/provider refs and errors; identity-free result aggregate + receipt digest only |
  | processor job/workspace | completed/rejected/expired/cancelled + verified workspace purge receipt | remove workspace/profile/cache/owner/lease detail | remove record/material refs, output/error detail; processor kind/status count + purge digest only |
  | backup partial and ordinary restore attempt/workspace | publish/ownership-transfer or purge receipt, source window not extended, no live lease | remove staging path/prefix, owner and lease detail | remove object/ref/operator/error detail; allowed media/policy/status/time/receipt survivor only |
  | approved forensic restore workspace | conversion receipt fixes source manifest/window, approval/access policy and `expires_at<=source.recoverable_until`; isolated `recoverability_window` has no HTTP/worker/export and expiry cannot be reset | while live, retain only exact approval/access/inventory leaves required by the forensic contract | at fixed expiry purge and witness receipt; only then start 24h owner/lease and 30d operation-detail compaction |
  | platform mutation | exact `complete`, kind-specific completion primary/full-witness readback, checkpoint continuity and zero workspace | no mutation-specific early deletion | remove attempt/parsed-approval/path/cutpoint/error rows; immutable non-content governance DAG remains |
  | telemetry sink/archive/backup | persist `raw_event_delete_at=min(captured_at+configured_sink_ttl,captured_at+30d)` at capture; same absolute deadline covers online sink, spool, archive, backup and replication destination | no special early conversion | at `raw_event_delete_at`, delete request/correlation/operation/object refs and raw event; identity-free route/status/time-bucket only; unknown/unverifiable sink closes capability |
  | notification/delivery/outbox/inbox/provider detail | while record exists, persist immutable `product_expires_at=min(created_at+configured_ttl,created_at+180d)`; delivery terminal does not set retention eligibility. At product expiry or permanent delete, verify a purge that clears summary/content and every record/revision/object/recipient/integration/provider-message/channel association and cancels unsent outbox; permanent delete overrides immediately | after verified purge, set one-time `retention_eligible_at`; remove live claim/lease detail by +24h | remove remaining identity-free operational attempt/error detail by +30d; survivor is only identity-free channel/status aggregate plus minimal-audit `external_copy_disclosed`/category count |

  Boundary tests run at `deadline-1µs`, exact deadline and `+1µs`; unresolved saga, live owner/lease, forensic workspace or missing receipt is never cleaned. For every generated policy row and each exact deadline, inject every required category independently: product text/binary/canonical/display content, filename, safe URL, raw path, free text, credential, record/object ID, origin ID, actor principal, governance principal, operation reference, recovery-source identity and external-delivery identity. Scan PostgreSQL application/primary/full-witness fields, S3 body/key/metadata/control properties, managed filesystem terminals and managed-client leaves; any hit in a category present in that row's generated set fails. One omission-negative fixture per `ForbiddenResidueKindV1` removes exactly that required kind and must fail generation/readiness before scanning. Survivor-allowed immutable/minimal/tombstone/recoverability leaves are exempt only through the same generated row, never by a broad location allowlist. A digest matching injected raw bytes does not waive the raw-value scan.

  The WORM witness root contains only immutable `ledger|heads|trust|trust-heads|plans|authorizations|bundles|completion|identity-sets|rotations` artifacts. `candidate/` is not a WORM family. Candidate prepare/import/cutover/cleanup staging, credential bundles, encrypted transfer scratch and nonce reservations use a separate candidate-control bucket/namespace registered as `ephemeral_registered` under a signed `CandidateEphemeralStoragePolicyV1`. The policy binds the exact phase credential identity commitments and IAM-policy digests, credential expiry, TLS/SPKI, control bucket/namespace identity, versioning, Object-Lock/default-retention/legal-hold disabled state, lifecycle/replication disabled state, backup/snapshot exclusion and encryption-at-rest policy. Startup, every write/readback and final cleanup re-probe those facts and exact-match the witnessed policy before byte 1; an unreachable or drifted control plane pauses the mutation and leaves protected admission fail closed. The surface is never a fallback witness.

  Its exact object grammar has two non-overlapping arms: ordinary objects use `candidate-control/v1/<scope_hash>/<tm-id>/<prepare|import|cutover|cleanup>/<artifact-id>/<20-digit-sequence>/<nonce|object|receipt>`; authoritative AEAD nonce reservations use `candidate-control/v1/<scope_hash>/<tm-id>/nonce-reservations/<64-lowercase-hex-reservation-id>/reservation`. Both use exact metadata `content-type`, provider SHA-256, `houfeng-schema-version`, `houfeng-artifact-kind`, `houfeng-canonical-digest`, `houfeng-expires-at` and no extra tags/metadata. Ordinary bodies exhaustively decode as a registered `CandidateEphemeralObjectEnvelopeV1`; reservation bodies decode only as `SignedCandidateAEADNonceReservationV1`; version/delete-marker/multipart inventory is exact. Ordinary phase receipts may exist under `receipt` until final purge, but `candidate_artifacts_purged` and `workspace_zero` are never written to the surface whose zero state they prove. A preparation-bound `CandidateCleanupVerifierV1` uses a read-control-only identity and a strict-local Ed25519 signer that are outside every candidate phase environment, candidate-control prefix and transfer workspace; after cleanup it re-probes the live control plane, signs the final body and returns it over a protected inherited FD. Current-side resume immediately appends/full-witnesses/readbacks it. On transport or ack loss, current first resolves the singleton receipt from primary/full witness; only a proven absence permits a fresh verifier observation. Thus neither the receipt nor its signing material creates residue or a self-reference.

  Before durable intent all possession/nonce bytes remain here and only the sealed proof enters intent. After durable intent, imported immutable ledger/trust evidence enters the candidate target WORM domain under the same exact WORM grammar/metadata/retention registry as current, but with a distinct witnessed bucket/domain identity; current-side resume mirrors signed phase results only as canonical `DomainRotationReceiptV1` under current `rotations/`. Candidate never writes current recovery-control/WORM.

  ```go
  type CandidateControlPhaseV1 string
  const (
      CandidateControlPrepare CandidateControlPhaseV1 = "prepare"
      CandidateControlImport CandidateControlPhaseV1 = "import"
      CandidateControlCutover CandidateControlPhaseV1 = "cutover"
      CandidateControlCleanup CandidateControlPhaseV1 = "cleanup"
  )

  type CandidateCredentialBindingV1 struct {
      CredentialIdentityDigest [32]byte
      PhaseIAMPolicyDigest [32]byte
      ValidFrom, ExpiresAt time.Time
  }

  type CandidateControlPlaneStateV1 struct {
      AdapterKind string // closed adapter registry token
      AdapterNormalizationVersion uint16 // v1 exact: 1
      HTTPSAuthorityDigest, TLSSPKIDigest [32]byte
      ControlBucketIdentityDigest, ControlNamespaceDigest [32]byte
      VersioningMode string // enabled only
      ObjectLockMode string // disabled only
      DefaultRetentionMode string // none only
      DefaultLegalHoldMode string // off only
      LifecycleMode string // disabled only
      ReplicationMode string // disabled only
      BackupSnapshotExclusionDigest, EncryptionAtRestPolicyDigest [32]byte
  }

  type CandidateNonceReservationSignerKeyDescriptorV1 struct {
      Version uint16
      Purpose string // candidate_aead_nonce_reservation_v1 only
      KeyID string
      PublicKey [32]byte
      KeyIdentityDigest [32]byte
      ValidFrom, ExpiresAt time.Time
  }

  type CandidateCleanupVerifierKeyDescriptorV1 struct {
      Version uint16
      AdapterKind string // strict_local_ed25519_v1 only in v1
      Purpose string // candidate_cleanup_receipt_v1 only
      KeyID string
      PublicKey [32]byte
      KeyIdentityDigest, FilesystemExclusionProofDigest [32]byte
      ValidFrom, ExpiresAt time.Time
  }

  type CandidateRecoveryRequestSignaturePurposeV1 string
  const (
      CandidateRecoveryRequestSignaturePurpose CandidateRecoveryRequestSignaturePurposeV1 = "candidate_recovery_request_v1"
  )

  type CandidateRecoveryRequestSignerKeyDescriptorV1 struct {
      Version uint16
      Purpose CandidateRecoveryRequestSignaturePurposeV1
      KeyID string
      PublicKey [32]byte
      KeyIdentityDigest [32]byte
      ValidFrom, ExpiresAt time.Time
  }

  type CandidateEphemeralStoragePolicyBodyV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID, CandidateDomainID string
      ScopeHash, ControlLocationIdentityDigest [32]byte
      Phase CandidateControlPhaseV1
      PrefixDigest [32]byte
      CredentialBindings []CandidateCredentialBindingV1 // sorted unique identity digests
      ControlPlane CandidateControlPlaneStateV1
      NonceReservationSigner CandidateNonceReservationSignerKeyDescriptorV1
      CleanupVerifier CandidateCleanupVerifierKeyDescriptorV1
      RecoveryRequestSigner CandidateRecoveryRequestSignerKeyDescriptorV1
      MaximumObjectBytes, MaximumPhaseBytes, MaximumObjects uint64
      MaximumVersions, MaximumDeleteMarkers, MaximumMultipartUploads uint64
      Generation uint64
      PhaseRevision uint64
      PreviousPolicyDigest [32]byte
      IssuedAt, ExpiresAt, MutationDeadline time.Time
  }

  type CandidateControlPolicyAuthorizationV1 struct {
      Purpose string // candidate_control_policy
      CurrentGovernancePolicyDigest, CandidateGovernancePolicyDigest [32]byte
      CurrentSignatures []DomainGovernanceSignatureV1 // sorted unique, current threshold
      CandidateSignatures []DomainGovernanceSignatureV1 // sorted unique, candidate threshold
  }

  type SignedCandidateEphemeralStoragePolicyV1 struct {
      Body CandidateEphemeralStoragePolicyBodyV1
      BodyDigest [32]byte
      Authorization CandidateControlPolicyAuthorizationV1
      PolicyDigest [32]byte // outer wrapper digest
  }

  type CandidateControlPolicyPrimaryReceiptBodyV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID string
      Generation uint64
      Phase CandidateControlPhaseV1
      PhaseRevision uint64
      PreviousPolicyDigest, PolicyDigest [32]byte
      ActivationPrerequisiteKind string // none|durable_intent|import_revoked|final_proof
      ActivationPrerequisiteDigest [32]byte
      PublishedAt time.Time
  }

  type CandidateControlPolicyPrimaryReceiptV1 struct {
      Body CandidateControlPolicyPrimaryReceiptBodyV1
      BodyDigest, ReceiptDigest [32]byte
  }

  type CandidateControlPolicyWitnessReceiptBodyV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID string
      Generation uint64
      Phase CandidateControlPhaseV1
      PolicyDigest, PrimaryReceiptDigest [32]byte
      WitnessedAt time.Time
  }

  type CandidateControlPolicyWitnessReceiptV1 struct {
      Body CandidateControlPolicyWitnessReceiptBodyV1
      BodyDigest, ReceiptDigest [32]byte
  }

  type CandidateControlPolicyChainHeadV1 struct {
      Version uint16
      MutationID string
      Generation, PhaseRevision uint64
      Phase CandidateControlPhaseV1
      PolicyDigest, PrimaryReceiptDigest, WitnessReceiptDigest [32]byte
  }

  type CandidateControlChallengeFenceBodyV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID string
      PolicyHead CandidateControlPolicyChainHeadV1
      ChallengeBody DomainCandidateChallengeBodyV1
      ChallengeBodyDigest [32]byte
      StartedAt time.Time
  }

  type CandidateControlChallengeFencePrimaryReceiptV1 struct {
      FenceBodyDigest, PolicyPrimaryReceiptDigest [32]byte
      ReceiptDigest [32]byte
  }

  type CandidateControlChallengeFenceWitnessReceiptV1 struct {
      FenceBodyDigest, CanonicalPrimaryReceiptDigest [32]byte
      PolicyWitnessReceiptDigest, ReceiptDigest [32]byte
  }

  type CandidateControlChallengeFenceArtifactV1 struct {
      Body CandidateControlChallengeFenceBodyV1
      BodyDigest [32]byte
  }

  type CandidateControlChallengeFenceV1 struct {
      Artifact CandidateControlChallengeFenceArtifactV1
      PrimaryReceipt CandidateControlChallengeFencePrimaryReceiptV1
      WitnessReceipt CandidateControlChallengeFenceWitnessReceiptV1
  }

  type CandidateEphemeralPayloadKindV1 string
  const (
      CandidateNonceChallenge CandidateEphemeralPayloadKindV1 = "nonce_challenge"
      CandidatePossessionObservation CandidateEphemeralPayloadKindV1 = "possession_observation"
      CandidateEncryptedCredentialBundle CandidateEphemeralPayloadKindV1 = "encrypted_phase_credential_bundle"
      CandidateTransferScratch CandidateEphemeralPayloadKindV1 = "transfer_scratch"
      CandidateCutoverCommand CandidateEphemeralPayloadKindV1 = "cutover_command"
      CandidatePhaseReceipt CandidateEphemeralPayloadKindV1 = "phase_receipt"
      CandidateInventory CandidateEphemeralPayloadKindV1 = "inventory"
  )

  type CandidateEncryptedPayloadKindV1 string
  const (
      CandidateEncryptedCredentialBundleKind CandidateEncryptedPayloadKindV1 = "encrypted_phase_credential_bundle"
      CandidateTransferScratchKind CandidateEncryptedPayloadKindV1 = "transfer_scratch"
  )

  type CandidateNonceChallengeV1 struct {
      Version uint16
      MutationID, CandidateDomainID string
      Purpose string // prepare_possession only
      Challenge [32]byte // cryptographic_challenge
      IssuedAt, ExpiresAt time.Time
  }

  type CandidateKMSEncryptionContextV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID, CandidateDomainID string
      Phase CandidateControlPhaseV1
      KeyID string
      StoragePolicyDigest [32]byte
  }

  type CandidateKMSWrappedDEKV1 struct {
      Version uint16
      KMSAdapterKind string // closed adapter token
      KMSAdapterVersion uint16
      KMSKeyIdentityDigest [32]byte // never a raw ARN/key path
      EncryptionContext CandidateKMSEncryptionContextV1
      EncryptionContextDigest [32]byte
      WrappedDEK []byte // 1..16384 bytes
      WrappedDEKDigest [32]byte
  }

  type CandidateLocalRawKeyV1 struct {
      Version uint16
  }

  type CandidateAEADKeyDescriptorV1 struct {
      Version uint16
      Kind string // local_raw_32_v1|kms_wrapped_dek_v1
      KeyID string // exact cek-sha256-<64 lowercase hex>, never provider access-key ID
      KeyIdentityDigest [32]byte
      LocalRaw32 *CandidateLocalRawKeyV1 // key/path bytes are never serialized
      KMSWrappedDEK *CandidateKMSWrappedDEKV1
  }

  type CandidateAEADNonceReservationBodyV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID, CandidateDomainID string
      Phase CandidateControlPhaseV1
      ArtifactID string
      Sequence uint64
      PayloadKind CandidateEncryptedPayloadKindV1
      KeyDescriptorDigest, PlaintextManifestDigest [32]byte
      Nonce [12]byte
      ReservedAt, ExpiresAt time.Time
  }

  type SignedCandidateAEADNonceReservationV1 struct {
      Body CandidateAEADNonceReservationBodyV1
      BodyDigest [32]byte
      Purpose string // candidate_aead_nonce_reservation_v1 only
      NonceReservationSignerKeyID string
      Signature [64]byte
      ReservationDigest [32]byte
  }

  type CandidateAEADAADV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID, CandidateDomainID string
      Phase CandidateControlPhaseV1
      ArtifactID string
      Sequence uint64
      PayloadKind CandidateEncryptedPayloadKindV1
      StoragePolicyDigest, KeyDescriptorDigest [32]byte
      NonceReservationDigest, PlaintextManifestDigest [32]byte
      PlaintextBytes uint64
      ExpiresAt time.Time
  }

  type CandidateEncryptedContentV1 struct {
      Version uint16
      KeyDescriptor CandidateAEADKeyDescriptorV1
      KeyDescriptorDigest [32]byte
      NonceReservationDigest [32]byte
      AAD CandidateAEADAADV1
      AADDigest, PlaintextManifestDigest [32]byte
      PlaintextBytes uint64
      Ciphertext []byte // ciphertext+16-byte tag; sole managed_content_payload leaf
  }

  type EncryptedCandidateCredentialBundleV1 struct {
      Version uint16
      MutationID string
      Phase CandidateControlPhaseV1
      CredentialManifestDigest [32]byte
      EncryptedContent CandidateEncryptedContentV1
  }

  type CandidateTransferScratchV1 struct {
      Version uint16
      MutationID, StreamID string
      ChunkOrdinal uint64
      PlaintextChunkDigest [32]byte
      EncryptedContent CandidateEncryptedContentV1
  }

  type CandidateEphemeralInventoryV1 struct {
      Version uint16
      MutationID, Phase string
      PolicyDigest, ObjectSetDigest, VersionSetDigest [32]byte
      MultipartSetDigest, CredentialAuthoritySetDigest [32]byte
      ObjectCount, VersionCount, DeleteMarkerCount, MultipartUploadCount uint64
      CredentialAuthorityCount uint64
      TotalBytes uint64
      ObservedAt time.Time
  }

  type CandidatePhaseReceiptV1 struct {
      Possession *SignedDomainCandidatePossessionV1
      ImportApplied *SignedRotationCandidateImportAppliedProofV1
      ImportRevoked *SignedRotationCredentialRevocationProofV1
      CutoverApplied *SignedDomainCutoverExecutionReceiptV1
      CutoverRevoked *SignedRotationCredentialRevocationProofV1
  }

  type CandidateKeyDestructionEvidenceV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID, CandidateDomainID string
      KeyKind string // aead_local_key|kms_wrapped_dek|nonce_reservation_signer_key
      KeyDescriptorDigest, LocalKeyIdentityDigest, WrappedDEKDigest [32]byte
      NonceSignerIdentityDigest, DestructionEvidenceDigest [32]byte
      DestructionKind string // local_file_unlinked_zeroized|wrapped_dek_purged_zeroized
      DestroyedAt time.Time
  }

  type CandidateEphemeralPurgeReceiptBodyV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID, CandidateDomainID string
      Phase CandidateControlPhaseV1
      StoragePolicy SignedCandidateEphemeralStoragePolicyV1
      BeforeInventory, AfterInventory CandidateEphemeralInventoryV1
      KeyDestructionEvidence []CandidateKeyDestructionEvidenceV1 // sorted, unsigned typed facts
      RemainingObjectCount, RemainingVersionCount uint64
      RemainingDeleteMarkerCount, RemainingMultipartUploadCount uint64
      MultipartAbortProofDigest, CredentialAuthorityZeroProofDigest [32]byte
      RemainingCredentialAuthorityCount uint64
      RemainingAEADKeyCount, RemainingNonceSigningKeyCount uint64
      CleanupVerifierIdentityDigest [32]byte
      ObservedAt time.Time
  }

  type SignedCandidateEphemeralPurgeReceiptV1 struct {
      Body CandidateEphemeralPurgeReceiptBodyV1
      BodyDigest [32]byte
      Context CandidatePurgeContextV1 // exact-one typed arm
      CleanupVerifierKeyID string
      Signature [64]byte
      ReceiptDigest [32]byte
  }

  type CandidateRecoveryRequestPurposeV1 string
  const (
      CandidateRequestAbandon CandidateRecoveryRequestPurposeV1 = "abandon"
      CandidateRequestImport CandidateRecoveryRequestPurposeV1 = "import"
      CandidateRequestCutoverApply CandidateRecoveryRequestPurposeV1 = "cutover_apply"
      CandidateRequestRevokeImport CandidateRecoveryRequestPurposeV1 = "revoke_import"
      CandidateRequestRevokeCutover CandidateRecoveryRequestPurposeV1 = "revoke_cutover"
      CandidateRequestCleanup CandidateRecoveryRequestPurposeV1 = "cleanup"
  )

  type CandidateControlAbandonAuthorizationBodyV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID, CandidateDomainID string
      PolicyHead CandidateControlPolicyChainHeadV1 // prepare phase only
      ChallengeFence *CandidateControlChallengeFenceV1 // nil only before challenge-start CAS
      AbandonFenceEpoch uint64
      IntentAbsenceProofDigest, RegisteredSurfaceInventoryDigest [32]byte
      ReservedAt, ExpiresAt time.Time
  }

  type CandidateControlAbandonAuthorizationPrimaryReceiptV1 struct {
      AuthorizationBodyDigest, PolicyPrimaryReceiptDigest [32]byte
      IntentAbsenceProofDigest, ReceiptDigest [32]byte
  }

  type CandidateControlAbandonAuthorizationWitnessReceiptV1 struct {
      AuthorizationBodyDigest, CanonicalPrimaryReceiptDigest [32]byte
      PolicyWitnessReceiptDigest, ReceiptDigest [32]byte
  }

  type CandidateControlAbandonAuthorizationArtifactV1 struct {
      Body CandidateControlAbandonAuthorizationBodyV1
      BodyDigest [32]byte
  }

  type CandidateControlAbandonAuthorizationV1 struct {
      Artifact CandidateControlAbandonAuthorizationArtifactV1
      PrimaryReceipt CandidateControlAbandonAuthorizationPrimaryReceiptV1
      WitnessReceipt CandidateControlAbandonAuthorizationWitnessReceiptV1
  }

  type CandidatePurgeContextV1 struct {
      SealedPreparation *SignedDomainCandidatePreparationV1
      AbandonAuthorization *CandidateControlAbandonAuthorizationV1
  }

  type CandidatePostIntentRecoveryContextV1 struct {
      Intent DomainRotationIntentV1
      RequiredRotationReceiptChainHead [32]byte
  }

  type CandidateAbandonRequestPrerequisiteV1 struct {
      Authorization CandidateControlAbandonAuthorizationV1
      CandidatePreparation *SignedDomainCandidatePreparationV1 // optional exact-match
  }

  type CandidateImportRequestPrerequisiteV1 struct {
      Context CandidatePostIntentRecoveryContextV1
      TransferStart SignedDomainTransferFrameV1
  }

  type CandidateRevokeImportRequestPrerequisiteV1 struct {
      Context CandidatePostIntentRecoveryContextV1
      ImportAppliedReceipt DomainRotationReceiptV1
  }

  type CandidateCutoverApplyRequestPrerequisiteV1 struct {
      Context CandidatePostIntentRecoveryContextV1
      ImportRevokedReceipt DomainRotationReceiptV1
      CutoverReceipt DomainRotationReceiptV1
      CutoverCommand DomainCutoverCommandV1
  }

  type CandidateRevokeCutoverRequestPrerequisiteV1 struct {
      Context CandidatePostIntentRecoveryContextV1
      FinalProofReceipt DomainRotationReceiptV1
  }

  type CandidateCleanupRequestPrerequisiteV1 struct {
      Context CandidatePostIntentRecoveryContextV1
      FinalProofReceipt, CutoverRevokedReceipt DomainRotationReceiptV1
  }

  type CandidateRecoveryRequestPrerequisiteV1 struct {
      Abandon *CandidateAbandonRequestPrerequisiteV1
      Import *CandidateImportRequestPrerequisiteV1
      RevokeImport *CandidateRevokeImportRequestPrerequisiteV1
      CutoverApply *CandidateCutoverApplyRequestPrerequisiteV1
      RevokeCutover *CandidateRevokeCutoverRequestPrerequisiteV1
      Cleanup *CandidateCleanupRequestPrerequisiteV1
  }

  type CandidateRecoveryRequestBodyV1 struct {
      Version uint16
      Purpose CandidateRecoveryRequestPurposeV1
      MutationID, DeploymentID, ProjectID, CandidateDomainID string
      PolicyHead CandidateControlPolicyChainHeadV1
      AuthorizedPolicy SignedCandidateEphemeralStoragePolicyV1
      Prerequisite CandidateRecoveryRequestPrerequisiteV1 // exact-one purpose-mapped arm
      IssuedAt, ExpiresAt time.Time
  }

  type CandidateRecoveryRequestPrimaryReceiptV1 struct {
      RequestDigest, PolicyPrimaryReceiptDigest [32]byte
      PrerequisiteDigest [32]byte
      ReceiptDigest [32]byte
  }

  type CandidateRecoveryRequestWitnessReceiptV1 struct {
      RequestDigest, CanonicalPrimaryReceiptDigest [32]byte
      PolicyWitnessReceiptDigest, ReceiptDigest [32]byte
  }

  type SignedCandidateRecoveryRequestV1 struct {
      Body CandidateRecoveryRequestBodyV1
      BodyDigest [32]byte
      SignaturePurpose CandidateRecoveryRequestSignaturePurposeV1
      SignerKeyID string
      Signature [64]byte
  }

  type CandidateRecoveryRequestV1 struct {
      SignedRequest SignedCandidateRecoveryRequestV1
      RequestDigest [32]byte // signed-core digest; excludes itself and publication receipts
      PrimaryReceipt CandidateRecoveryRequestPrimaryReceiptV1
      WitnessReceipt CandidateRecoveryRequestWitnessReceiptV1
  }

  type CandidateAbandonWorkspaceZeroBodyV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID, CandidateDomainID string
      AbandonAuthorizationDigest, CandidatePurgeReceiptDigest [32]byte
      CleanupDatabaseObjectCount, CleanupS3ObjectCount uint64
      CleanupFilesystemObjectCount, RemainingCredentialCount uint64
      RemainingWorkspaceCount, RemainingAEADKeyCount uint64
      RemainingNonceSigningKeyCount uint64
      CleanupVerifierIdentityDigest [32]byte
      ObservedAt time.Time
  }

  type SignedCandidateAbandonWorkspaceZeroV1 struct {
      Body CandidateAbandonWorkspaceZeroBodyV1
      BodyDigest [32]byte
      CleanupVerifierKeyID string
      Signature [64]byte // workspace_zero purpose only
      ReceiptDigest [32]byte
  }

  type CandidateControlAbandonCompletionBodyV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID string
      PolicyHead CandidateControlPolicyChainHeadV1
      AbandonAuthorizationDigest [32]byte
      CandidatePurgeReceipt SignedCandidateEphemeralPurgeReceiptV1
      CandidatePurgeReceiptDigest [32]byte
      WorkspaceZeroReceipt SignedCandidateAbandonWorkspaceZeroV1
      WorkspaceZeroReceiptDigest [32]byte
      CompletedAt time.Time
  }

  type CandidateControlAbandonCompletionPrimaryReceiptV1 struct {
      CompletionBodyDigest, AbandonAuthorizationDigest [32]byte
      PolicyPrimaryReceiptDigest, ReceiptDigest [32]byte
  }

  type CandidateControlAbandonCompletionWitnessReceiptV1 struct {
      CompletionBodyDigest, CanonicalPrimaryReceiptDigest [32]byte
      PolicyWitnessReceiptDigest, ReceiptDigest [32]byte
  }

  type CandidateControlAbandonCompletionArtifactV1 struct {
      Body CandidateControlAbandonCompletionBodyV1
      BodyDigest [32]byte
  }

  type CandidateControlAbandonCompletionV1 struct {
      Artifact CandidateControlAbandonCompletionArtifactV1
      PrimaryReceipt CandidateControlAbandonCompletionPrimaryReceiptV1
      WitnessReceipt CandidateControlAbandonCompletionWitnessReceiptV1
  }

  type CandidateEphemeralPayloadV1 struct {
      NonceChallenge *CandidateNonceChallengeV1
      PossessionObservation *SignedDomainCandidatePossessionV1
      EncryptedCredentialBundle *EncryptedCandidateCredentialBundleV1
      TransferScratch *CandidateTransferScratchV1
      CutoverCommand *DomainCutoverCommandV1
      PhaseReceipt *CandidatePhaseReceiptV1
      Inventory *CandidateEphemeralInventoryV1
  }

  type CandidateEphemeralObjectBodyV1 struct {
      Version uint16
      MutationID, DeploymentID, ProjectID, CandidateDomainID string
      Phase CandidateControlPhaseV1
      ArtifactID string
      Sequence uint64
      Kind CandidateEphemeralPayloadKindV1
      StoragePolicyDigest [32]byte
      Payload CandidateEphemeralPayloadV1 // exactly one identically mapped arm
      PayloadDigest [32]byte
      CreatedAt, ExpiresAt time.Time
  }

  type CandidateEphemeralObjectEnvelopeV1 struct {
      Body CandidateEphemeralObjectBodyV1
      BodyDigest [32]byte
      EnvelopeDigest [32]byte // outer; excluded from its preimage
  }
  ```

  `CredentialIdentityDigest=SHA-256("houfeng-candidate-credential-identity-v1" NUL adapter-kind NUL canonical-provider-public-identity)`; only the digest is serialized. `CandidateAEADAADV1` is exact length-prefixed bytes of magic/version, mutation/deployment/project/candidate-domain, phase/artifact/sequence/the closed two-value encrypted-payload kind, storage-policy digest, key-descriptor digest, nonce-reservation digest, plaintext-manifest digest, plaintext length and absolute expiry. AES-256-GCM uses a 12-byte CSPRNG nonce and 16-byte tag. Before encryption, the authoritative `SignedCandidateAEADNonceReservationV1` is signed only for purpose `candidate_aead_nonce_reservation_v1` by the policy-bound nonce-reservation key and durably inserted under `nonce_reservation_id=SHA-256("houfeng-candidate-aead-nonce-reservation-v1" NUL mutation-id NUL key-identity-digest NUL nonce)`; S3 uses `If-None-Match:*`, managed filesystem uses no-follow `O_EXCL`, and PostgreSQL uses an exact unique key over the same tuple. Ack-loss retries read and exact-match the complete signed bytes; a collision with another artifact/plaintext fails, so two concurrent artifact paths cannot reserve the same `(key_identity_digest,nonce)`. The encrypted artifact stores only `NonceReservationDigest` and must fetch/verify that authoritative reservation before decryption. Same artifact retry must exact-match its reservation and plaintext manifest; changed plaintext, wrong key/AAD, repeated nonce, missing reservation or altered tag yields zero plaintext and zero committed object. The local arm loads the command's explicit regular/no-follow/bounded strict-0400 32-byte raw-key file; it serializes neither path nor key. The KMS arm opens the exact config+credential file pair, generates a 32-byte DEK, stores only bounded wrapped-DEK bytes classified as `encrypted_key_material`, exact canonical encryption context and digested KMS identity, and unwraps only through the pinned adapter/version/context. Key descriptor arms and parser inputs are exact XOR in exactly six byte-handling commands: candidate prepare, transfer import, cutover apply, credential revoke, candidate abandon and candidate cleanup. Local, KMS-wrapped-DEK and nonce-signer destruction each emit unsigned typed `CandidateKeyDestructionEvidenceV1`; the unused digest arms are zero and the active identity/evidence exact-match inventory. These facts are covered only by the outer cleanup-verifier signature on `SignedCandidateEphemeralPurgeReceiptV1`, so the verifier still has exactly two signing purposes: purge and workspace-zero. A shared KMS KEK may remain, but the per-mutation DEK, every wrapped-DEK copy, local file, nonce-reservation private key and memory cache must be absent, with both remaining key counts equal to zero, before purge proof. `SignedCandidateEphemeralPurgeReceiptV1.ReceiptDigest` and `SignedCandidateAbandonWorkspaceZeroV1.ReceiptDigest` respectively use `SHA-256("HOUFENG-CANDIDATE-EPHEMERAL-PURGE-RECEIPT-V1" || canonical-wrapper-preimage)` and `SHA-256("HOUFENG-CANDIDATE-ABANDON-WORKSPACE-ZERO-V1" || canonical-wrapper-preimage)`, where the preimage excludes only the final `ReceiptDigest`. The SQL helpers bounded-decode the complete persisted wrapper, exhaustively re-encode that preimage and reject an embedded digest mismatch; a raw SHA-256 alias is invalid.

  `nonce` keys accept only `nonce_challenge`; the separate `nonce-reservations/<nonce-reservation-id>/reservation` key accepts only `SignedCandidateAEADNonceReservationV1`; `object` accepts only `possession_observation|encrypted_phase_credential_bundle|transfer_scratch|cutover_command|inventory`; `receipt` accepts only the non-final `phase_receipt`. `CandidatePhaseReceiptV1` is an exact-one typed union and cannot carry unknown canonical bytes. Final purge/workspace-zero wrappers travel only over the protected verifier FD and are immediately current-side witnessed. Every policy/envelope/payload/nonce-reservation decoder consumes all bytes, recomputes every sibling digest, exact-matches mutation/phase/key/metadata/expiry and rejects nil/empty ambiguity.

  `CandidatePurgeContextV1` is an exact-one structural union, not a caller discriminator plus digest. Normal post-intent teardown requires the complete sealed preparation arm and revalidates its nested policy/intent binding; pre-intent abandon requires the complete `CandidateControlAbandonAuthorizationV1` arm including its typed primary and witness receipts and a fresh 2+3 readback. The selected context's complete canonical bytes and discriminant are inside the cleanup-verifier signature preimage. Nil/both arms, digest-only context, wrong mutation/purpose, an authorization without complete receipts, or using abandon authorization for normal cleanup fails identically in Go, primary SQL, PostgreSQL witness, S3 and managed-filesystem decoders.

  `CandidateRecoveryRequestV1` uses the following closed purpose/phase/prerequisite matrix. `AuthorizedPolicy` and `PolicyHead` must be the same 2+3-read-back generation, every post-intent arm embeds the complete authoritative intent, and no field outside the selected arm may be nonzero:

  | Purpose | exact `PolicyHead.Phase` | exact non-nil prerequisite arm | mandatory typed prerequisite |
  |---|---|---|---|
  | `abandon` | `prepare` | `Abandon` | complete witnessed abandon authorization; no intent; optional preparation only if byte-identical |
  | `import` | `import` | `Import` | complete intent plus signed transfer `Start` for `copy|dual_write`; full-witness-derived current chain head |
  | `revoke_import` | `import` | `RevokeImport` | complete intent plus `candidate_import_applied` receipt whose hash is the required chain head |
  | `cutover_apply` | `cutover` | `CutoverApply` | complete intent; `ImportRevokedReceipt.Kind=candidate_import_revoked`; `CutoverReceipt.Kind=cutover` and `CutoverReceipt.ReceiptHash=Context.RequiredRotationReceiptChainHead`; `CutoverReceipt.Body.PreviousReceiptHash=CutoverReceipt.Payload.Cutover.PreCutoverReceiptChainHead=ImportRevokedReceipt.ReceiptHash`; typed cutover command exact-derived from that Cutover arm |
  | `revoke_cutover` | `cleanup` | `RevokeCutover` | complete intent plus `final_proof` receipt whose hash is the required chain head |
  | `cleanup` | `cleanup` | `Cleanup` | complete intent, `final_proof` and later `candidate_cutover_revoked` receipt whose hash is the required chain head |

  The exporter derives the arm and purpose from primary/full-witness state; the CLI cannot choose phase or substitute a receipt. Nil/two arms, purpose/arm mismatch, wrong phase, wrong receipt kind/order/head, either Cutover predecessor equality failing, cross-mutation intent, stale policy generation, a cutover command not derived from the arm, or abandon with any intent all fail before candidate stat/KMS/network/write. The same positive and one-field-negative canonical corpus runs through request export, the six command parsers, primary/witness receipt encoders and S3 readback.

  `SignedCandidateRecoveryRequestV1` is the publication core and contains no publication receipt or self digest. Its body digest is `SHA-256("HOUFENG-CANDIDATE-RECOVERY-REQUEST-BODY-V1" || canonical-body)`; the Ed25519 signature preimage is `"HOUFENG-CANDIDATE-RECOVERY-REQUEST-SIGNATURE-V1" || body-digest || policy-head-digest || u32be(len(signer-key-id)) || signer-key-id`, with the closed purpose value `candidate_recovery_request_v1`; `RequestDigest=SHA-256("HOUFENG-CANDIDATE-RECOVERY-REQUEST-V1" || canonical-signed-core)`. `CandidateRecoveryRequestPrimaryReceiptV1.ReceiptDigest=SHA-256("HOUFENG-CANDIDATE-RECOVERY-REQUEST-PRIMARY-RECEIPT-V1" || canonical-primary-receipt-preimage)` and `CandidateRecoveryRequestWitnessReceiptV1.ReceiptDigest=SHA-256("HOUFENG-CANDIDATE-RECOVERY-REQUEST-WITNESS-RECEIPT-V1" || canonical-witness-receipt-preimage)`; each receipt preimage excludes only its final `ReceiptDigest`, while persisted receipt bytes contain that digest. The SQL helpers `candidate_recovery_request_digest_v1`、`candidate_recovery_request_primary_receipt_digest_v1` and `candidate_recovery_request_witness_receipt_digest_v1` bounded-decode and exhaustively re-encode these exact preimages rather than applying bare SHA-256 to stored bytes. One checked-in byte/hex golden must match Go、recovery-control SQL、PostgreSQL witness and S3; wrong separator、bare SHA-256、primary/witness type swap and any single-byte mutation fail. The canonical signed core is independently bounded to 32 MiB, each typed publication receipt to 64 KiB and the final FD wrapper to 33 MiB; Go, primary SQL, PostgreSQL witness, S3 and inherited-FD decoders reject oversize before allocation and exhaustively consume all bytes. For immutable identity `(mutation_id, policy_generation, purpose)`, primary stores signed core + typed primary receipt and full witness stores byte-identical core + canonical primary-receipt copy + typed witness receipt. Only exact five-artifact readback may assemble `CandidateRecoveryRequestV1` for the inherited FD. Ack loss exact-matches the original core and both receipts; expiry never permits re-signing changed `IssuedAt|ExpiresAt` under the same tuple, and a new request requires a valid same-phase policy renewal with a new generation. Candidate commands validate request, policy and mutation deadline before any stat/KMS/network/write.

  The storage-policy chain is mutation-wide, not four independent chains: generation 1 has phase `prepare`, phase revision 1 and zero previous digest; every renewal increments both generation and same-phase revision, while a phase advance increments generation, resets phase revision to 1, binds the prior digest and may move only `prepare→import→cutover→cleanup`. The first read-only `control-policy draft` creates the mutation ID, probes current/candidate identities plus the candidate-control surface with zero write credentials, and emits only a signature-free body. Separate offline `domain governance sign --purpose candidate-control-policy --scope current|candidate` invocations produce the two threshold sets; `control-policy seal` verifies both independently. Current-side `control-policy publish` appends the complete signed wrapper plus typed primary receipt to recovery-control, confirms the byte-identical policy and canonical primary-receipt copy with a typed witness receipt, then performs a 2+3 readback before any policy-authorized byte is written. PostgreSQL full witness uses generation-keyed typed rows; S3 uses immutable `bundles/<tm-id>/candidate_control_policy/<20-digit-generation>/<policy|primary-receipt|witness-receipt>` objects and exhaustive decoders. Same-phase renewal and each phase advance repeat draft/sign/seal/publish, bind the witnessed predecessor and are rejected unless their activation prerequisite is already witnessed. Candidate actions accept the full policy as input, exact-match its published bytes/digest/generation and re-probe live control state before byte 1. Challenge embeds the published prepare policy; preparation and durable intent preserve the same complete wrapper.

  Every generation carries current- and candidate-governance threshold signatures over its body digest and a validity window covering all credential bindings, the nonce-reservation signer descriptor and the cleanup-verifier descriptor. Import activation requires durable intent/full-witness and prepare credential revocation; cutover activation requires witnessed `candidate_import_applied` then binding `candidate_import_revoked`; cleanup activation requires witnessed `final_proof` only. The witnessed `final_proof` binds the then-current complete candidate-control policy-chain head and cleanup-verifier descriptor. Every later renewal/phase advance, especially cleanup generation publication, must keep that descriptor's adapter, purpose, key ID/public key, key-identity digest and filesystem-exclusion-proof digest byte-identical; primary append, PostgreSQL witness confirm and S3 put/readback independently reject any post-Final replacement, refreshed proof identity or validity-window substitution. If that exact signer/proof is lost or expires after Final, teardown remains fail closed; it cannot be repaired by a new signer. Publishing/readback of the cleanup generation first makes every cutover credential zero-authority, after which cleanup performs explicit provider/DB revocation and witnesses `candidate_cutover_revoked`; requiring that receipt before cleanup-policy activation would be circular and is forbidden. Old policy/credential writes become zero-authority immediately; they remain readable only for exact inventory and revocation proof. Cleanup revalidates the witnessed signed policy and live control-plane state, destroys all AEAD and nonce-signing key material, inventories zero live objects/versions/delete markers/multipart uploads, then the external verifier signs `CandidateEphemeralPurgeReceiptBodyV1`. Full policy-chain and body→body digest→closed signer purpose/preparation-bound signature→outer receipt digest goldens are shared by Go, SQL, S3 and filesystem decoders.

  `s3_worm` uses this exact key grammar. Tokens are closed: `scope_hash=[0-9a-f]{64}`, `tm-id=tm-[0-9a-f]{64}`, and every sequence/revision/epoch/receipt ordinal is `[0-9]{20}` with zero padding; no signed, short or alternate spelling exists:

  ```text
  record-platform/v1/<scope_hash>/ledger/<runtime|activation|rotation>/<20-digit-sequence>/entry
  record-platform/v1/<scope_hash>/heads/<runtime|activation|rotation>/<20-digit-sequence>/receipt
  record-platform/v1/<scope_hash>/trust/<20-digit-revision>/entry
  record-platform/v1/<scope_hash>/trust-heads/<20-digit-revision>/receipt
  record-platform/v1/<scope_hash>/plans/<tm-id>/plan
  record-platform/v1/<scope_hash>/authorizations/<tm-id>/authorization
  record-platform/v1/<scope_hash>/bundles/<tm-id>/<activation|mutation|rotation_intent|rotation_cutover>/bundle
  record-platform/v1/<scope_hash>/bundles/<tm-id>/candidate_control_policy/<20-digit-generation>/<policy|primary-receipt|witness-receipt>
  record-platform/v1/<scope_hash>/bundles/<tm-id>/candidate_control_challenge/<fence|primary-receipt|witness-receipt>
  record-platform/v1/<scope_hash>/bundles/<tm-id>/candidate_recovery_request/<20-digit-generation>/<abandon|import|cutover_apply|revoke_import|revoke_cutover|cleanup>/<request|primary-receipt|witness-receipt>
  record-platform/v1/<scope_hash>/bundles/<tm-id>/candidate_control_abandon/<authorization|authorization-primary-receipt|authorization-witness-receipt|completion|completion-primary-receipt|completion-witness-receipt>
  record-platform/v1/<scope_hash>/completion/<tm-id>/completion
  record-platform/v1/<scope_hash>/identity-sets/<20-digit-epoch>/<set|primary-receipt|witness-receipt>
  record-platform/v1/<scope_hash>/rotations/<tm-id>/receipts/<20-digit-receipt-sequence>/receipt
  ```

  `scope_hash` is lowercase hex SHA-256 of `"houfeng-record-platform-scope-v1" NUL deployment_id NUL project_id`; record/object IDs are forbidden from its preimage. Every key maps to exactly one family/role/terminal/schema tuple. Object metadata is exactly `content-type=application/octet-stream`, `x-amz-checksum-sha256=<base64 SHA-256 body>`, `houfeng-schema-version=<lowercase canonical schema token>`, `houfeng-artifact-kind=<exact key/body kind>` and `houfeng-canonical-digest=<lowercase 64-hex body digest>`; missing/extra keys, case/encoding/length mismatch, tags or metadata/body/key disagreement fail. Family `recorded_at` is taken only from its canonical leaf: ledger `confirmed_at`, trust `recorded_at`, plan `generated_at`, authorization `intent_recorded_at`, completion and candidate-abandon completion `completed_at`, identity-set `activated_at`, rotation receipt `observed_at`, challenge fence `started_at`, recovery request `issued_at`; each primary/witness receipt object inherits its confirmed parent artifact time rather than a mutable root time. `witnessed_profile_floor_seconds=max(compiled_minimum,active_witnessed_s3_identity.default_retention_seconds)`. Every WORM-family PUT, and only those PUTs, sets Object Lock `COMPLIANCE`, legal hold `ON`, and `retain_until=ceil_utc_second(max(server_now,family_recorded_at)+witnessed_profile_floor_seconds)`; copy/rotation additionally requires `retain_until >= source_retain_until`, and neither retry nor policy change may reduce it. Candidate-control PUTs use `CandidateEphemeralStoragePolicyV1`, never this formula. Readback verifies body, provider checksum/version/ETag, mode, hold and retain-until before publishing the next receipt.

  The S3 `candidate_control_abandon/completion` object is the complete canonical `CandidateControlAbandonCompletionBodyV1` plus its body digest and therefore includes the full signed candidate-purge and workspace-zero wrappers, not only their digests. Its two receipt objects are separately typed and the witness receipt binds the canonical primary-receipt copy. A reader with only the WORM objects must reconstruct and recursively verify the authorization, nested signatures/digests, policy head and complete `CandidateControlAbandonCompletionV1` without PostgreSQL root state, candidate-control storage or list-order assumptions; missing/extra/far-tail objects fail. The generic `completion/<tm-id>/completion` object likewise contains the complete typed `MutationCompletionReceiptV1`, never a digest-only blob. Primary-loss integration deletes the PostgreSQL artifacts and proves both completion families rebuild from S3 alone.

  Bucket control-plane rows cover versioning enabled, Object-Lock default enabled/COMPLIANCE, legal-hold permission, current/noncurrent/delete-marker lifecycle, multipart expiry and replication destination/role/status. No lifecycle rule may expire current versions, noncurrent versions or delete markers under the WORM root; multipart cleanup may target only uploads that never completed and have no immutable receipt. Replication is either explicitly disabled or exact-matches the witnessed non-overlapping destination contract. Missing control data, unknown rule, prefix overlap, extra object/version/delete marker, mutable overwrite/delete, hold removal or retention reduction fails readiness.

  Add a checked-in `ManagedFilesystemGrammarV1`, not a runtime wildcard. Tokens are exact: `tm-id=tm-[0-9a-f]{64}`, `draft-id=dr-[0-9a-f]{64}`, `job-id=job-[0-9a-f]{64}`, `attempt-id=at-[0-9a-f]{64}`, `sink-id=sink-[0-9a-f]{64}`, `stream-id=st-[0-9a-f]{64}`, `artifact-id=af-[0-9a-f]{64}`, `nonce-reservation-id=[0-9a-f]{64}`, `ordinal=[0-9]{20}`; scope, phase and terminal are closed enums. The cross-program grammar reserves these exact families, but a path is not an active registry row until the child that first writes it supplies its concrete schema/policy/producer delta. Relative to persistent `/var/lib/houfeng/record-platform/<root>/`, the closed union is:

  ```text
  plans:     drafts/<draft-id>/activation-drain.v1
             mutations/<tm-id>/plan.v1
             mutations/<tm-id>/drain-continuation/<ordinal>.v1
             mutations/<tm-id>/rotation-drain/<ordinal>.v1
             mutations/<tm-id>/unreachable/<ordinal>.<draft|proof>.v1
  approvals: <tm-id>/<current|candidate>/<ak-sha256-[0-9a-f]{64}>.approval.v1
  candidate: <tm-id>/<prepare|import|cutover|cleanup>/<artifact-id>/<ordinal>.<control-policy-draft|control-policy-sealed|challenge-draft|challenge-sealed|attestation-body|preparation-draft|preparation-sealed|governance-signature|attestation-signature|cutover-command|inventory|phase-receipt>.v1
             <tm-id>/nonce-reservations/<nonce-reservation-id>.reservation.v1
  transfer:  <tm-id>/<copy|dual-write|cutover>/<stream-id>/inventory.v1
             <tm-id>/<copy|dual-write|cutover>/<stream-id>/frames/<ordinal>.frame.v1
             <tm-id>/<copy|dual-write|cutover>/<stream-id>/purge-receipt.v1
  backup:    <job-id>/<attempt-id>/inventory.v1
             <job-id>/<attempt-id>/parts/<ordinal>.<artifact-id>.bin
             <job-id>/<attempt-id>/<manifest|purge-receipt>.v1
  restore:   <job-id>/<attempt-id>/inventory.v1
             <job-id>/<attempt-id>/<db|blob|wal|tmp|export>/<artifact-id>.bin
             <job-id>/<attempt-id>/<ownership-transfer|purge-receipt>.v1
  processor: <job-id>/<attempt-id>/inventory.v1
             <job-id>/<attempt-id>/<profile|cache>/<artifact-id>.bin
             <job-id>/<attempt-id>/<stdout|stderr>/<ordinal>.log
             <job-id>/<attempt-id>/purge-receipt.v1
  telemetry: <sink-id>/<attempt-id>/inventory.v1
             <sink-id>/<attempt-id>/<spool|archive>/<ordinal>.event
             <sink-id>/<attempt-id>/purge-receipt.v1
  archive:   <job-id>/<attempt-id>/inventory.v1
             <job-id>/<attempt-id>/staging/<artifact-id>.bin
             <job-id>/<attempt-id>/<manifest|purge-receipt>.v1
  ```

  Foundation's own nonempty delta claims only Task 1 producers: activation/mutation plans and approvals, admission snapshots, domain candidate/control-policy/transfer artifacts, platform backup/restore control-plane attempts and telemetry emitted by those producers. A dormant grammar token is not inventory, cannot satisfy an owner claim and cannot pre-classify future bytes. Child 3 must add attachment scanner/blob/backup terminals, child 5 the browser draft codec, child 8 the comparison result schema, child 9 delivery surfaces, child 10 import/export/archive terminals and child 11 concrete cross-storage restore/processor integration before any such producer writes byte 1. Child 4's approved initial evidence implementation is PostgreSQL/canonical gzip-bytea only and therefore has no managed-filesystem producer or terminal; a future renderer/capture file producer would require its own reviewed owner/delta rather than inheriting a reserved grammar token.

  `/run/houfeng/record-platform/{candidate,transfer,backup,restore,processor,telemetry,archive}` uses the corresponding family rows, is always `ephemeral_registered`, requires an absolute `expires_at`, and never accepts plans or approvals. The run-only `/run/houfeng/record-platform/admission` root has its own two exact rows and is forbidden under `/var/lib`:

  ```text
  drafts/<draft-id>/<lb|queue>.snapshot.v1
  mutations/<tm-id>/continuations/<ordinal>/<lb|queue>.snapshot.v1
  mutations/<tm-id>/rotation-drains/<ordinal>/<lb|queue|copy-replay>.snapshot.v1
  ```

  Admission LB/queue snapshots are bounded, signed `SignedAdmissionSnapshotV1` files and copy-replay uses `SignedCopyReplaySnapshotV1`, each with the exact draft/mutation/ordinal scope. Every row fixes one expected schema, owner UID/GID, regular-file type, `0600` artifact/credential or `0700` private-directory mode, maximum bytes, created/expiry times and inventory/cleanup receipt. Raw payload names are only artifact-ID/ordinal tokens; original filenames and arbitrary nesting are rejected. Registration commits before byte 1; traversal uses directory FDs with no-follow and rejects absolute paths, `..`, symlinks, external hard links, sockets/devices, unknown files or mode/owner drift. A new physical layout requires a reviewed checked-in grammar/schema row before byte 1.

  `ManagedFileTerminalSchemaV1` is another closed generated table. It maps `activation-drain→AdmissionDrainReceiptV1`, `plan→PlatformMutationPlanV1`, `drain-continuation→DrainContinuationV1`, `rotation-drain→RotationDrainProofV1`, `unreachable.draft→RotationUnreachableProofBodyV1`, `unreachable.proof→SignedRotationUnreachableProofV1`, `approval→DetachedApprovalV1`, `control-policy-draft→CandidateEphemeralStoragePolicyBodyV1`, `control-policy-sealed→SignedCandidateEphemeralStoragePolicyV1`, `challenge-draft→DomainCandidateChallengeBodyV1`, `challenge-sealed→SignedDomainCandidateChallengeV1`, `attestation-body→DomainAttestationBodyV1`, `preparation-draft→DomainCandidatePreparationBodyV1`, `preparation-sealed→SignedDomainCandidatePreparationV1`, `governance-signature→DomainGovernanceSignatureV1`, `attestation-signature→DomainAttestationSignatureV1`, `nonce-reservation→SignedCandidateAEADNonceReservationV1`, `cutover-command→DomainCutoverCommandV1`, `phase-receipt→CandidatePhaseReceiptEnvelopeV1`, `frame→SignedDomainTransferFrameV1`, `lb|queue snapshot→SignedAdmissionSnapshotV1`, `copy-replay snapshot→SignedCopyReplaySnapshotV1`, and every `inventory|manifest|ownership-transfer|purge-receipt` terminal to one explicit family-specific production decoder. Raw `parts/db/blob/wal/tmp/export/profile/cache/staging .bin` bodies are `ManagedContentPayloadV1` envelopes whose bounded payload leaf alone is `managed_content_payload`; processor `stdout|stderr .log` bodies are bounded `ProcessorDiagnosticChunkV1`, and telemetry `spool|archive .event` bodies are `TelemetryEnvelopeV1`. Those content-bearing schemas are legal only as `ephemeral_registered|recoverability_window`, carry absolute expiry plus inventory identity, and are forbidden from immutable/minimal survivors. No terminal, extension or family uses a generic bytes decoder or inherits another row's leaf policy.

  A surface outside those roots uses these exact schemas; a destination can never self-certify:

  ```go
  type FilesystemExclusionProofBodyV1 struct {
      Version uint16
      ProofID, DeploymentID, ProjectID string
      Mode string // bootstrap|renewal
      Generation uint64
      PreviousProofDigest [32]byte
      SurfaceKind, ExactMountType, ExactMountPathToken string // closed ASCII registries
      ExactMountNamespaceID [32]byte
      ExactMountSourceDigest, ExactBucketOrPrefixDigest [32]byte
      ComponentBinaryDigest, ConfigurationDigest [32]byte
      ManagedFilesystemGrammarDigest, SurfaceRegistryDigest [32]byte
      BackupSnapshotReplicationExclusionDigest [32]byte
      SwapMode string // disabled|tmpfs|ephemeral_encrypted
      SwapKeyEpochOrZero uint64
      CorePatternDigest, HelperIdentityDigest [32]byte
      WritableTargetInventoryDigest [32]byte
      ObservedAt, ValidFrom, ExpiresAt time.Time
      PolicyDigest [32]byte
  }

  type FilesystemExclusionSignatureV1 struct {
      Version uint16
      Purpose string // filesystem_exclusion_bootstrap|filesystem_exclusion_renewal
      ProofBodyDigest, PolicyDigest [32]byte
      DeploymentID, ProjectID string
      Generation uint64
      PreviousProofDigest [32]byte
      SignerKeyID string
      Signature [64]byte
  }

  type SignedFilesystemExclusionProofV1 struct {
      Body FilesystemExclusionProofBodyV1
      BodyDigest [32]byte
      Signatures []FilesystemExclusionSignatureV1 // sorted unique, 1..64, threshold
      ProofDigest [32]byte
  }

  type FilesystemExclusionRenewalReceiptV1 struct {
      Version uint16
      ProofID string
      Generation uint64
      PreviousProofDigest, ProofDigest [32]byte
      InventoryCheckpointDigest, FullWitnessHeadDigest [32]byte
      WitnessedAt time.Time
      ReceiptDigest [32]byte
  }
  ```

  Generation 1 requires zero previous digest, purpose `filesystem_exclusion_bootstrap`, and signatures satisfying the complete candidate domain-governance policy embedded in the activation plan; the complete policy digest, proof bytes and signature set are leaves of `ActivationInventoryV1`, primary recovery-control and full witness before sequence 1 can complete. Generation N>1 binds N-1, uses purpose `filesystem_exclusion_renewal`, exact-matches the witnessed current policy and is included with its renewal receipt in the next `SignedRecoveryInventoryV1`/checkpoint, primary and full witness. The authoritative append-only chain is keyed by proof ID/generation in recovery-control and reconstructed from full-witnessed inventory artifacts; local files are optional exact-match inputs. Cross-purpose signatures, a single-signature threshold bypass, gap/fork, expiry, config/mount/helper/registry drift or backup overlap closes permanent delete. If a surface becomes managed, a signed transition receipt binds the last exclusion proof to the first managed inventory and its eventual purge receipt; exclusion history remains governance evidence. If swap/hibernation/pagefile, remote processor, host journald/core collector, container overlay or another writable target cannot be proven excluded, it becomes a managed `recoverability_window|ephemeral_registered` surface with inventory, forbidden-corpus scan and purge receipt.

  Core dumps are a separate required exclusion surface, not an assumed absence. The generated manifest records evidence for process `RLIMIT_CORE=0`, systemd `LimitCORE=0`, container `ulimits.core=0`, the effective kernel `core_pattern`/helper and every writable cwd/mount; integration crashes center, admin, migrator, candidate exporter/importer and processor fixtures and proves no core object/file appeared in a managed root, host dump collector, telemetry archive or backup inventory. If any layer cannot be inspected or can emit a dump, that destination must be registered as a managed telemetry/archive/backup surface with the same field-level policy and purge receipt. Janitor deletion requires the family-specific receipt and the transition matrix above; an unknown or unscannable surface closes permanent delete.

- [ ] **Step 3: Add retention acceptance supply-chain RED tests.** Freeze canonical goldens for `RetentionAcceptancePolicyV1`, the 11-entry owner matrix, source claim, required-check snapshot, signed merge acceptance and metadata file. A claim generated twice for the same exact base/source tree is byte-identical and opens no private-key path. Initial child fixtures all reject `no_new_surface`, empty delta and a surface outside their owner entry. Merge acceptance fixtures require a two-parent merge commit with first parent=claim base and second parent=claim source; reject squash/rebase/octopus commits, wrong repository/ref/PR, source/base/tree drift, merge-resolution changes to owned production inputs, stale/non-latest/wrong-workflow check runs, scanner/rules/signer/workflow digest drift, registry replacement/removal or `before+delta!=after`, wrong/expired/compromised key, cross-repository/child/ordinal replay, duplicate merge SHA, previous-receipt gap and nondeterministic timestamps. The signer parser accepts only one bounded canonical report over an inherited FD, runs with network disabled and never opens the merge tree. Metadata validation permits exactly one regular file at the signed path, rejects symlink/case/ordinal/slug/SHA mismatch and every policy/workflow/registry/production change, and proves child N+1 cannot claim a base missing acceptance N. Task 11 fixtures accept only checked-in 1–10 chain plus its own claim; its pre-merge acceptance count remains zero.

- [ ] **Step 4: Add deployment RED tests.** Document and statically verify the admin CLI, root/effective-user policy ownership, 0400 private keys, protected 0600 local artifacts, detached-approval GitOps flow, three disjoint long-lived base environment files (`center.env`, `record-platform-admin.env`, `record-platform-migrator.env`) plus the separately mounted, phase-replaced and destroyed short-lived `record-platform-candidate.env`, the exact flags/profile/scope matrices, runtime/admin/migrator/candidate DB roles, distinct center/admin S3 write credentials plus migrator read-control and candidate prepare/import/cutover/cleanup policy-phase identities, independently pinned current/candidate governance policies, the distinct preparation-bound nonce-reservation signer and candidate receipt signer, and the isolated one-shot cleanup-verifier process/read-control identity/strict-0400 key/inherited-FD/no-self-storage boundary. The key-source matrix proves exact-one local raw 32-byte file XOR KMS config+credential pair for exactly six byte-handling commands—candidate prepare, transfer import, cutover apply, credential revoke, candidate abandon and candidate cleanup—plus zero secret bytes in argv/canonical output and pre-stat rejection by every other scope. Also verify the load balancer's exact-204 `/api/system/record-platform/admission` probe and the fact that a same-host Compose profile is integration-only. Candidate/verifier secrets are forbidden from all three base files; offline governance signing receives no environment or network credential; the center process environment must contain no `HOUFENG_RECORD_PLATFORM_ADMIN_*` or `HOUFENG_RECORD_PLATFORM_MIGRATOR_*` name; records-only must contain/open no external-domain input. Static and crash probes also verify `RLIMIT_CORE=0`, systemd `LimitCORE=0`, container core ulimit, effective kernel/helper routing and zero core files/objects across every managed/host/archive/backup destination for center, admin, migrator, candidate, verifier and processor processes. Preserve v0.59 `POSTGRES_PASSWORD`, `houfeng` DB/user and `./data/postgres` upgrade path.

- [ ] **Step 5: Verify RED.** Run `go test ./internal/center/recovery ./internal/center/recordplatform ./internal/center/store ./internal/center/retention ./internal/center/deploy ./cmd/houfeng-center ./cmd/houfeng-retention-scanner ./cmd/houfeng-retention-acceptance-signer -run 'Runtime|Inventory|Retention|Allowlist|SchemaRegistry|PolicyRegistry|Acceptance|SourceClaim|MergeTree|Metadata|PostgresColumns|S3Grammar|ObjectLock|ManagedFilesystem|CoreDump|Deadline|Residue|Deployment|Legacy|Profile|CredentialBoundary|Upgrade' -count=1` plus `sh scripts/verify-record-retention-acceptance.test.sh`; every new test must fail for the intended missing production symbol/workflow, not because a fixture is absent.

- [ ] **Step 6: Implement runtime, workers, retention supply chain and janitor contracts.** Center never calls activation/trust/rotation mutation writers and never opens admin/migrator pools. Runtime verifier requires witnessed trust + activation bundle + latest active identity epoch + signed inventory + exact replay watermark. Bootstrap explicitly constructs and registers real `OutboxWorker`, `LedgerReconciler`, `FenceProjector` and `RecoveryInventoryWorker` instances with production stores/adapters; tests drive an actual persisted outbox claim, not-committed release, fence projection and inventory publication through those workers and reject nil/no-op seams or assertions based only on worker count. Implement the independent canonical-schema and manually reviewed retention-policy registries, generate the one-leaf/property manifest, and compare it bidirectionally with every PostgreSQL catalog, S3 object/control-plane property and managed-filesystem inventory at build time and protected-capability startup. The same gate verifies versioned core-dump exclusion evidence at process, systemd, container, kernel/helper and destination layers; an unprovable destination must first become a registered managed surface. Implement the per-family 24h/30d conversion functions with exact state/proof/deadline predicates, live-owner fencing, receipt-bound filesystem/S3 janitors and forbidden-corpus residue scan; no generic “terminal” helper may erase unresolved data.

  Implement `houfeng-retention-scanner` and `houfeng-retention-acceptance-signer` as separate static tools. Before the foundation PR can merge, a protected tool-build workflow builds them with fixed Go/toolchain/flags, publishes immutable OCI artifacts and records their manifest digests in the reviewed acceptance policy; PR and protected-main workflows download by digest and verify bytes before exec. `record-retention-source-claim.yml` runs for ordinary child PR/merge-queue commits and uploads one bounded claim artifact; it never receives the acceptance environment. `record-retention-merge-acceptance.yml` runs only for a just-pushed two-parent feature merge on protected main, verifies the trusted workflow blob/policy before requesting the protected environment, runs the scanner read-only, and gives the 0400 key only to a no-network signer sandbox via inherited report/output FDs. It then creates or exact-matches the feature-SHA-derived metadata branch/PR using the policy-bound writer identity. `record-retention-acceptance-metadata.yml` runs only for that exact path-only PR, validates the full chain without any private key and never emits a child claim. CODEOWNERS and branch rules require the source-claim context for feature PRs and metadata context for metadata PRs; merge method is merge-commit only. Build `/app/houfeng-record-platform-admin` into the image as an alternate entrypoint while the normal image command remains the non-root center; the two CI-only retention tools are release artifacts, not production image entrypoints. Add `make build-record-platform-admin`, `make build-retention-scanner` and `make build-retention-acceptance-signer`.

  Credential/process tests prove the scanner has only a read-only tree and report FD, the signer has no checkout/network/Git or PR token and only the 0400 key plus inherited report/output FDs, and the metadata writer has only its PR App token plus signed bytes and no scanner checkout/key. The metadata verifier executes policy-pinned trusted-base code, treats the proposed file as bounded data and has neither key nor write token. Ordinal 1 additionally verifies the complete threshold-signed `SignedRetentionAcceptanceGenesisV1`; after acceptance 1 metadata merges, owner matrix/policy/tool/workflow digests are frozen. Any later acceptance-key/tool/workflow/policy change is a separate old-policy-threshold-authorized `record-retention-policy-rotation` metadata PR that must merge before another child starts and cannot alter the initial merge order or repair a child delta.

- [ ] **Step 7: Complete docs, retention delivery protocol and upgrade smoke.** Document the exact command family below. Database endpoints come only from the strict admin environment; private-key bytes never appear in argv. `PLAN_DIGEST`, `MUTATION_ID` and `TARGET_KEY_ID` are read from authoritative plan output and validated against `^sha256:[0-9a-f]{64}$`, `^tm-[0-9a-f]{64}$` and `^rk-sha256-[0-9a-f]{64}$` before use. Managed artifact writers accept only the configured exact `--output-root`, derive the `ManagedFilesystemGrammarV1` relative path themselves, use no-follow exclusive create, and return the authoritative path; arbitrary `--output` leaf paths are rejected. Initial apply consumes local plan plus exactly one authorization mode; after durable intent every status/retry uses `--mutation-id` and reconstructs canonical plan/authorization from primary/full witness. The retention delivery document freezes tool artifact provisioning, protected-environment/key ownership, required-check names, merge-commit enforcement, exact metadata branch/path/PR idempotency, acceptance-key rotation and the rule that child N+1 waits for metadata acceptance N.

```bash
houfeng-record-platform-admin migrate --scope app
houfeng-record-platform-admin migrate --scope permanent-delete

# The configured LB/queue adapters atomically register a paired draft snapshot
# and return this cryptographically random ID before activation drain.
IFS= read -r -p 'Admission snapshot draft ID: ' ACTIVATION_DRAFT_ID
[[ "$ACTIVATION_DRAFT_ID" =~ ^dr-[0-9a-f]{64}$ ]] || exit 2
ACTIVATION_LB_SNAPSHOT="/run/houfeng/record-platform/admission/drafts/$ACTIVATION_DRAFT_ID/lb.snapshot.v1"
ACTIVATION_QUEUE_SNAPSHOT="/run/houfeng/record-platform/admission/drafts/$ACTIVATION_DRAFT_ID/queue.snapshot.v1"

houfeng-record-platform-admin activation drain \
  --draft-id "$ACTIVATION_DRAFT_ID" \
  --lb-snapshot "$ACTIVATION_LB_SNAPSHOT" \
  --queue-snapshot "$ACTIVATION_QUEUE_SNAPSHOT" \
  --output-root /var/lib/houfeng/record-platform/plans/drafts

IFS= read -r -p 'Authoritative drain receipt path from command output: ' ACTIVATION_DRAIN_PATH
[[ "$ACTIVATION_DRAIN_PATH" =~ ^/var/lib/houfeng/record-platform/plans/drafts/dr-[0-9a-f]{64}/activation-drain\.v1$ ]] || exit 2

houfeng-record-platform-admin activation plan \
  --drain-receipt "$ACTIVATION_DRAIN_PATH" \
  --approval-policy /etc/houfeng/record-platform/approval-policy.v1 \
  --recovery-signing-key /etc/houfeng/record-platform/recovery-signing.key \
  --output-root /var/lib/houfeng/record-platform/plans/mutations

IFS= read -r -p 'Full activation plan digest: ' PLAN_DIGEST
[[ "$PLAN_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || exit 2
IFS= read -r -p 'Activation mutation ID from plan output: ' ACTIVATION_MUTATION_ID
[[ "$ACTIVATION_MUTATION_ID" =~ ^tm-[0-9a-f]{64}$ ]] || exit 2
IFS= read -r -p 'Authoritative activation plan path from plan output: ' ACTIVATION_PLAN_PATH
[[ "$ACTIVATION_PLAN_PATH" == "/var/lib/houfeng/record-platform/plans/mutations/$ACTIVATION_MUTATION_ID/plan.v1" ]] || exit 2

houfeng-record-platform-admin approval sign \
  --plan "$ACTIVATION_PLAN_PATH" \
  --scope candidate \
  --approver-key /media/houfeng-offline-approver/approver.key \
  --output-root /var/lib/houfeng/record-platform/approvals

IFS= read -r -p 'Authoritative activation approval path: ' ACTIVATION_APPROVAL_PATH
[[ "$ACTIVATION_APPROVAL_PATH" =~ ^/var/lib/houfeng/record-platform/approvals/$ACTIVATION_MUTATION_ID/candidate/ak-sha256-[0-9a-f]{64}\.approval\.v1$ ]] || exit 2

# Choose exactly one authorization mode for a new mutation. Do not run both.
# Interactive mode:
houfeng-record-platform-admin activation apply \
  --plan "$ACTIVATION_PLAN_PATH" \
  --confirm "$PLAN_DIGEST"

# Detached bootstrap mode:
houfeng-record-platform-admin activation apply \
  --plan "$ACTIVATION_PLAN_PATH" \
  --approval "$ACTIVATION_APPROVAL_PATH"

houfeng-record-platform-admin activation status \
  --mutation-id "$ACTIVATION_MUTATION_ID"

# If an unknown result is reported after durable intent and the original drain
# receipt has expired, restore the same exact-204/queue config and zero counts,
# then produce a liveness-only continuation for the original plan.
IFS= read -r -p 'Admission continuation ordinal from paired adapters: ' ACTIVATION_CONTINUATION_ORDINAL
[[ "$ACTIVATION_CONTINUATION_ORDINAL" =~ ^[0-9]{20}$ ]] || exit 2
ACTIVATION_LB_SNAPSHOT="/run/houfeng/record-platform/admission/mutations/$ACTIVATION_MUTATION_ID/continuations/$ACTIVATION_CONTINUATION_ORDINAL/lb.snapshot.v1"
ACTIVATION_QUEUE_SNAPSHOT="/run/houfeng/record-platform/admission/mutations/$ACTIVATION_MUTATION_ID/continuations/$ACTIVATION_CONTINUATION_ORDINAL/queue.snapshot.v1"

houfeng-record-platform-admin activation drain \
  --continue-mutation "$ACTIVATION_MUTATION_ID" \
  --lb-snapshot "$ACTIVATION_LB_SNAPSHOT" \
  --queue-snapshot "$ACTIVATION_QUEUE_SNAPSHOT" \
  --output-root /var/lib/houfeng/record-platform/plans/mutations

IFS= read -r -p 'Authoritative drain continuation path: ' ACTIVATION_CONTINUATION_PATH
[[ "$ACTIVATION_CONTINUATION_PATH" =~ ^/var/lib/houfeng/record-platform/plans/mutations/$ACTIVATION_MUTATION_ID/drain-continuation/[0-9]{20}\.v1$ ]] || exit 2

houfeng-record-platform-admin activation resume \
  --mutation-id "$ACTIVATION_MUTATION_ID" \
  --drain-continuation "$ACTIVATION_CONTINUATION_PATH"

houfeng-record-platform-admin trust plan --action add \
  --approval-policy /etc/houfeng/record-platform/approval-policy.v1 \
  --recovery-signing-key /etc/houfeng/record-platform/recovery-signing.key \
  --candidate-recovery-key /etc/houfeng/record-platform/recovery-next.key \
  --output-root /var/lib/houfeng/record-platform/plans/mutations

houfeng-record-platform-admin trust plan --action rotate \
  --approval-policy /etc/houfeng/record-platform/approval-policy.v1 \
  --recovery-signing-key /etc/houfeng/record-platform/recovery-signing.key \
  --candidate-recovery-key /etc/houfeng/record-platform/recovery-next.key \
  --output-root /var/lib/houfeng/record-platform/plans/mutations

IFS= read -r -p 'Trust-rotate mutation ID from plan output: ' TRUST_ROTATE_MUTATION_ID
[[ "$TRUST_ROTATE_MUTATION_ID" =~ ^tm-[0-9a-f]{64}$ ]] || exit 2
IFS= read -r -p 'Authoritative trust-rotate plan path: ' TRUST_ROTATE_PLAN_PATH
[[ "$TRUST_ROTATE_PLAN_PATH" == "/var/lib/houfeng/record-platform/plans/mutations/$TRUST_ROTATE_MUTATION_ID/plan.v1" ]] || exit 2

IFS= read -r -p 'Target recovery key ID: ' TARGET_KEY_ID
[[ "$TARGET_KEY_ID" =~ ^rk-sha256-[0-9a-f]{64}$ ]] || exit 2

houfeng-record-platform-admin trust plan --action retire \
  --approval-policy /etc/houfeng/record-platform/approval-policy.v1 \
  --recovery-signing-key /etc/houfeng/record-platform/recovery-signing.key \
  --key-id "$TARGET_KEY_ID" \
  --output-root /var/lib/houfeng/record-platform/plans/mutations

houfeng-record-platform-admin trust plan --action compromise \
  --approval-policy /etc/houfeng/record-platform/approval-policy.v1 \
  --key-id "$TARGET_KEY_ID" \
  --output-root /var/lib/houfeng/record-platform/plans/mutations

houfeng-record-platform-admin trust plan --action remove \
  --approval-policy /etc/houfeng/record-platform/approval-policy.v1 \
  --recovery-signing-key /etc/houfeng/record-platform/recovery-signing.key \
  --key-id "$TARGET_KEY_ID" \
  --output-root /var/lib/houfeng/record-platform/plans/mutations

houfeng-record-platform-admin trust plan --action approval-policy-rotate \
  --approval-policy /etc/houfeng/record-platform/approval-policy.v1 \
  --candidate-approval-policy /etc/houfeng/record-platform/approval-policy-next.v1 \
  --output-root /var/lib/houfeng/record-platform/plans/mutations

IFS= read -r -p 'Policy-rotate mutation ID from plan output: ' POLICY_ROTATE_MUTATION_ID
[[ "$POLICY_ROTATE_MUTATION_ID" =~ ^tm-[0-9a-f]{64}$ ]] || exit 2
IFS= read -r -p 'Authoritative policy-rotate plan path: ' POLICY_ROTATE_PLAN_PATH
[[ "$POLICY_ROTATE_PLAN_PATH" == "/var/lib/houfeng/record-platform/plans/mutations/$POLICY_ROTATE_MUTATION_ID/plan.v1" ]] || exit 2

# The compact policy-rotation example assumes current threshold=1 and candidate
# threshold=1 with exactly one candidate-only key. Repeat each scoped signing
# command for the real thresholds and every candidate-only key.
houfeng-record-platform-admin approval sign \
  --plan "$TRUST_ROTATE_PLAN_PATH" \
  --scope current \
  --approver-key /media/houfeng-offline-approver/approver.key \
  --output-root /var/lib/houfeng/record-platform/approvals

IFS= read -r -p 'Authoritative trust-rotate approval path: ' TRUST_ROTATE_APPROVAL_PATH
[[ "$TRUST_ROTATE_APPROVAL_PATH" =~ ^/var/lib/houfeng/record-platform/approvals/$TRUST_ROTATE_MUTATION_ID/current/ak-sha256-[0-9a-f]{64}\.approval\.v1$ ]] || exit 2

houfeng-record-platform-admin approval sign \
  --plan "$POLICY_ROTATE_PLAN_PATH" \
  --scope current \
  --approver-key /media/houfeng-current-approvers/current-01.key \
  --output-root /var/lib/houfeng/record-platform/approvals

IFS= read -r -p 'Authoritative current-policy approval path: ' POLICY_CURRENT_APPROVAL_PATH
[[ "$POLICY_CURRENT_APPROVAL_PATH" =~ ^/var/lib/houfeng/record-platform/approvals/$POLICY_ROTATE_MUTATION_ID/current/ak-sha256-[0-9a-f]{64}\.approval\.v1$ ]] || exit 2

houfeng-record-platform-admin approval sign \
  --plan "$POLICY_ROTATE_PLAN_PATH" \
  --scope candidate \
  --approver-key /media/houfeng-candidate-approvers/candidate-01.key \
  --output-root /var/lib/houfeng/record-platform/approvals

IFS= read -r -p 'Authoritative candidate-policy approval path: ' POLICY_CANDIDATE_APPROVAL_PATH
[[ "$POLICY_CANDIDATE_APPROVAL_PATH" =~ ^/var/lib/houfeng/record-platform/approvals/$POLICY_ROTATE_MUTATION_ID/candidate/ak-sha256-[0-9a-f]{64}\.approval\.v1$ ]] || exit 2

houfeng-record-platform-admin trust apply \
  --plan "$TRUST_ROTATE_PLAN_PATH" \
  --approval "$TRUST_ROTATE_APPROVAL_PATH"

houfeng-record-platform-admin trust apply \
  --plan "$POLICY_ROTATE_PLAN_PATH" \
  --approval "$POLICY_CURRENT_APPROVAL_PATH" \
  --approval "$POLICY_CANDIDATE_APPROVAL_PATH"

houfeng-record-platform-admin trust status \
  --mutation-id "$TRUST_ROTATE_MUTATION_ID"

houfeng-record-platform-admin trust status \
  --mutation-id "$POLICY_ROTATE_MUTATION_ID"

houfeng-record-platform-admin trust resume \
  --mutation-id "$TRUST_ROTATE_MUTATION_ID"
```

The docs state that each new plan gets approvals signed from that exact plan; the activation approval shown above cannot authorize `trust-rotate.plan`. Scope never cross-counts. Candidate-policy rotation supplies enough files to satisfy current and candidate thresholds plus every candidate-only key's possession proof; repeatable `--approval` accepts at most 64 bounded envelopes. Before durable intent, retrying initial apply uses the same exact local plan and authorization while they remain live. After durable intent, `status|resume --mutation-id` recovers the witnessed canonical plan/authorization and never asks for a replacement approval or TTY confirmation; an optional supplied local artifact can only exact-match. Never use a digest prefix, free-text actor, secret CLI argument or `--yes`.

`scripts/test-record-platform-upgrade.sh` uses one `mktemp` workspace and EXIT trap and emits a receipt for every stage:

1. **legacy-0050:** start `linnea7171/houfeng:v0.59.0` on the bind-mounted legacy PostgreSQL data directory; prove login, monitoring health and migration 0050.
2. **current-flags-off:** stop only old center; start current image with both record flags false and no record-platform mode/runtime/admin/migrator variable; prove the legacy owner path auto-migrates the additive 0051 schema without attempting role grants and emit `current-flags-off`. Login/monitoring remain healthy; then restart v0.59.0 on the same data, prove it ignores additive 0051 without a down migration, and emit the separate `flags-off-old-rollback` receipt.
3. **current-records-only:** stop the owner process, run `migrate --scope app` with only the APP migrator identity and exact APP runtime/admin role names, then start current center with the APP runtime DSN, `HOUFENG_RECORDS_ENABLED=true` and permanent-delete false. Prove no external-domain/mode or legacy `HOUFENG_DELETION_LEDGER_HMAC_KEY_RING_FILE` variable is stat'ed, parsed, resolved or opened; center owns no ADMIN/MIGRATOR environment name or connection; the exact sorted migration filename/checksum set and `app_acl_manifest_r000001` digest are verified rather than applied; login, VPS, monitoring/IP, subscription and records-only access succeed; schema migration/DDL/unauthorized relation access fails; permanent delete reports unavailable without probing an external domain. A synthetic pending 0052 keeps runtime fail closed without applying it; a fixture that adds the 0052 object without appending the next ACL revision is rejected; applying migration+`r000002` together restores runtime. A second fixture applies 0056 before 0055 and requires revisions `r000002→r000003` while each body binds the newly sorted complete migration set.
4. **pre-activation rollback:** stop current center and restart v0.59.0 once more before sequence 1; prove compatible flags-off rollback still works. Stop it, return to current center, install exact-204 LB and version-aware queue gates, and produce a live zero-count drain receipt.
5. **activation:** run `migrate --scope permanent-delete` for both `postgres_sync` and `s3_worm` fixtures, prove exact profile identities/ACLs, then plan/sign/apply sequence 1. Inject one ack-loss, delete all local plan/approval files and recovery-control state, reconstruct `status` from full witness by mutation ID, produce a same-scope continuation if the original receipt expires, resume by mutation ID, wait for projection/replay/inventory and prove current admission is bodyless exact 204.
6. **post-activation fence:** start v0.59.0 without making it a target. Its legacy `/ready` may return 200, but it has no admission route, produces no versioned membership/queue receipt and receives zero record traffic/tasks under exact-204/version-aware gates. No down migration or old-binary rollback is attempted after activation.

Every stage asserts container environment and active PostgreSQL connection identities, records image/config/schema/domain-identity/digest, fails on a skipped assertion and leaves no container/workspace behind.

- [ ] **Step 8: Verify GREEN.** Re-run Step 5, `sh scripts/verify-record-retention-acceptance.test.sh`, deterministic claim/acceptance generation twice with byte comparison, wrong-tree/stale-check/wrong-key/replay fixture corpus, static workflow/CODEOWNERS/permission inspection, `docker compose config`, `docker compose --profile record-platform config`, `systemd-analyze verify`, host-network image build and non-root health/ready smoke. The test key is fixture-only; no protected acceptance private key is available to ordinary CI.

## Task 17: Implement domain identity rotation and supported disaster recovery

**Files:**

- Create: `internal/center/platformadmin/domain_rotation.go`
- Create: `internal/center/platformadmin/domain_rotation_test.go`
- Create: `internal/center/platformadmin/domain_candidate.go`
- Create: `internal/center/platformadmin/domain_candidate_test.go`
- Create: `internal/center/platformadmin/domain_copy.go`
- Create: `internal/center/platformadmin/domain_copy_test.go`
- Create: `internal/center/recovery/domain_rotation.go`
- Create: `internal/center/recovery/domain_rotation_test.go`
- Modify: `internal/center/platformadmin/{types,canonical,approval,planner,saga,config}.go`
- Modify: `internal/center/platformadmin/{canonical,approval,planner,saga,config}_test.go`
- Modify: `internal/center/platformadmin/repository_postgres.go`
- Modify: `internal/center/platformadmin/witness_postgres.go`
- Modify: `internal/center/platformadmin/witness_s3.go`
- Modify: `cmd/houfeng-record-platform-admin/main.go`
- Modify: `cmd/houfeng-record-platform-admin/main_test.go`
- Modify: `internal/center/recovery/{types,trust_store,trust_service}.go`
- Modify: `internal/center/deletionledger/{types,canonical,postgres,witness_postgres,witness_s3}.go`
- Modify: `internal/center/store/deletion_replay.go`
- Modify: `internal/center/store/deletion_replay_test.go`
- Modify: `internal/center/platformmigrate/domain_identity.go`
- Modify: `internal/center/platformmigrate/domain_identity_test.go`
- Modify: `db/recoverycontrol/migrations/0001_create_recovery_control.sql`
- Modify: `db/deletionledger/migrations/0001_create_deletion_ledger.sql`
- Modify: `db/deletionwitness/migrations/0001_create_full_witness.sql`
- Modify: `db/migrations/0051_create_record_platform_foundation.sql`
- Modify: `scripts/test-record-platform-integration.sh`
- Modify: `scripts/test-record-platform-upgrade.sh`
- Modify: `docs/deploy/local-and-systemd.md`
- Modify: `docs/deploy/compose.env.example`

- [ ] **Step 1: Freeze the rotation canonical contract with RED goldens.** Use the exact plan/body split below. Body encoders contain no digest of themselves; records hold outer digests. Current/candidate `DomainIdentitySetBodyV1` occur only inside `DomainRotationIntentV1`; the plan top level stores only their digest/epoch plus the one canonical intent, and duplicate body serialization is rejected. A candidate set is valid only when profile is byte-identical, global epoch is `current+1`, exactly one member changes, that member's epoch is `current+1`, all untouched member bytes are identical, and candidate domain ID plus stable physical digest are pairwise distinct from current domains, backups, logs, workspaces and replica targets. `DomainAttestationBodyV1` has a strict PostgreSQL XOR S3 identity arm; its signature-free body is ≤64 KiB, its wrapper is ≤128 KiB, and the sorted unique 1…64 signature set must satisfy the exact pinned/witnessed domain-governance policy threshold. The separately published generation-1 prepare `SignedCandidateEphemeralStoragePolicyV1` creates the mutation ID and is embedded byte-for-byte plus digest in the challenge; `SignedDomainCandidateChallengeV1` is current-threshold signed and candidate verifies it against an independently pinned policy. `SignedDomainCandidatePreparationV1` is candidate-threshold signed and exact-matches the same prepare policy while binding every schema/ACL/principal/credential-manifest/cleanup fact, not only possession. Both wrapper digests, the witnessed recovery transfer signer, complete nonce-reservation signer descriptor, candidate receipt signer and complete cleanup-verifier descriptor enter the plan/intent. `SignedDomainCandidatePossessionV1` binds mutation/target/candidate domain+stable digest, complete challenge artifact digest, nonce digest, policy/key ID, observed/expiry and Ed25519 signature with body/wrapper separation; it is ≤64 KiB, expires within 15 minutes and contains no endpoint, credential, path or free text. Current-scope detached approval remains the sole mutation authorization; challenge, governance, control-policy publication and candidate-domain possession never count toward it. Reject TTY, candidate approval scope, single-signature threshold bypass, self-contained unpinned policy, unpublished/drifted control policy, multiple members, same PostgreSQL system ID, same S3 bucket with another prefix, profile change, unknown kind/version, nil/empty ambiguity and any self-reference.

```go
type DomainRotationMode string

const (
    DomainRotationPlanned  DomainRotationMode = "planned_migration"
    DomainRotationDisaster DomainRotationMode = "disaster_recovery"
)

type DomainAttestationBodyV1 struct {
    Version uint16
    Purpose string // provision|renew|rotation_candidate|retirement
    DeploymentID, ProjectID, ActiveProfile string
    DomainKind, DomainID string
    MemberEpoch uint64
    IdentityMode string // postgres_system|external_attestation
    PostgreSQL *PostgreSQLDomainIdentityV1
    S3Witness *S3WitnessDomainIdentityV1
    StableIdentityDigest [32]byte
    SetEpoch uint64
    SetDigest [32]byte
    Generation uint64
    ChallengeNonce [32]byte // retention semantic: cryptographic_challenge
    IssuedAt, ValidFrom, ExpiresAt time.Time
    PolicyDigest [32]byte
}

type DomainAttestationSignatureBodyV1 struct {
    Version uint16
    AttestationBodyDigest, PolicyDigest [32]byte
    SignerKeyID string
}

type DomainAttestationSignatureV1 struct {
    Body DomainAttestationSignatureBodyV1
    Signature [64]byte
}

type SignedDomainAttestationV1 struct {
    Body DomainAttestationBodyV1
    BodyDigest [32]byte
    Signatures []DomainAttestationSignatureV1 // key-ID sorted/unique, 1..64
}

type DomainCandidateChallengeBodyV1 struct {
    Version uint16
    MutationID, DeploymentID, ProjectID string
    Mode DomainRotationMode
    ActiveProfile, TargetDomainKind string
    CurrentSetDigest [32]byte
    CurrentSetEpoch uint64
    CurrentApprovalPolicyDigest, CurrentDomainAttestationPolicyDigest [32]byte
    ExpectedTrustPrimary, ExpectedTrustWitness recovery.TrustHead
    ExpectedLedgerPrimary, ExpectedLedgerWitness deletionledger.Head
    CopyPolicyDigest, DrainScopeDigest [32]byte
    PrepareControlPolicy SignedCandidateEphemeralStoragePolicyV1
    PrepareControlPolicyDigest [32]byte
    ChallengeNonce [32]byte
    TransferSignerKeyID string
    TransferSignerPublicKey [32]byte
    MinimumFenceContractVersion uint64
    GeneratedAt, ExpiresAt time.Time
}

type SignedDomainCandidateChallengeV1 struct {
    Body DomainCandidateChallengeBodyV1
    BodyDigest [32]byte
    CurrentGovernanceSignatures []DomainGovernanceSignatureV1 // sorted/unique threshold
}

type DomainCandidateIdentityV1 struct {
    PostgreSQL *PostgreSQLDomainIdentityV1
    S3Witness *S3WitnessDomainIdentityV1
}

type DomainCandidatePreparationBodyV1 struct {
    Version uint16
    MutationID string
    ChallengeArtifactDigest [32]byte
    CandidateIdentity DomainCandidateIdentityV1
    CandidateAttestationBody DomainAttestationBodyV1
    CandidateAttestationBodyDigest [32]byte
    Possession SignedDomainCandidatePossessionV1
    NonceReservationSigner CandidateNonceReservationSignerKeyDescriptorV1
    CandidateReceiptSignerKeyID string
    CandidateReceiptSignerPublicKey [32]byte
    PrepareControlPolicy SignedCandidateEphemeralStoragePolicyV1
    CleanupVerifier CandidateCleanupVerifierKeyDescriptorV1
    SchemaMigrationSetDigest, ACLManifestDigest [32]byte
    CopyExclusionManifestDigest [32]byte
    ImportPrincipalDigest, CutoverPrincipalDigest, CleanupPrincipalDigest [32]byte
    CredentialBundleManifestDigest [32]byte
    ProvisionRevocationReceiptDigest [32]byte
    CleanupHandleDigest [32]byte
    PreparedAt, ExpiresAt time.Time
}

type SignedDomainCandidatePreparationV1 struct {
    Body DomainCandidatePreparationBodyV1
    BodyDigest [32]byte
    CandidateAttestationSignatures []DomainAttestationSignatureV1 // sorted/unique threshold
    CandidatePreparationSignatures []DomainGovernanceSignatureV1 // sorted/unique threshold
}

type DomainCopySourceDescriptorV1 struct {
    ObjectKind DomainTransferObjectKindV1
    ObjectIdentityDigest, CanonicalObjectDigest [32]byte
    CanonicalLength uint64
}

type DomainCopySourceInventoryV1 struct {
    Version uint16
    Sources []DomainCopySourceDescriptorV1 // kind/identity sorted and unique
}

type DomainCopyPolicyV1 struct {
    Version uint16
    AllowedObjectKinds []DomainTransferObjectKindV1 // enum sorted/unique
    MaximumFrames, MaximumObjects, MaximumObjectBytes uint64
    MaximumCanonicalBytes uint64
}

type DomainDrainScopeV1 struct {
    Version uint16
    DeploymentID, ProjectID, TargetDomainKind string
    DeploymentEpoch, MinimumFenceContractVersion uint64
    LBConfigDigest, QueueConfigDigest [32]byte
}

type DomainRotationIntentBodyV1 struct {
    Version uint16
    MutationID, DeploymentID, ProjectID string
    Mode DomainRotationMode
    ActiveProfile, TargetDomainKind string
    Challenge SignedDomainCandidateChallengeV1
    CandidatePreparation SignedDomainCandidatePreparationV1
    CurrentIdentitySetBody DomainIdentitySetBodyV1
    CandidateIdentitySetBody DomainIdentitySetBodyV1
    CurrentApprovalPolicy ApprovalPolicy
    CurrentDomainAttestationPolicy DomainAttestationPolicyV1
    CandidateDomainAttestationPolicy DomainAttestationPolicyV1
    CurrentAdapterPolicy SignedAdmissionAdapterPolicyV1
    CandidateAdapterPolicy SignedAdmissionAdapterPolicyV1
    CopySourceInventory DomainCopySourceInventoryV1
    CopyPolicy DomainCopyPolicyV1
    DrainScope DomainDrainScopeV1
    NonceReservationSigner CandidateNonceReservationSignerKeyDescriptorV1
    TransferSignerKeyID, CandidateReceiptSignerKeyID string
    TransferSignerPublicKey, CandidateReceiptSignerPublicKey [32]byte
    CleanupVerifier CandidateCleanupVerifierKeyDescriptorV1
    MinimumFenceContractVersion uint64
}

type DomainRotationIntentV1 struct {
    Body DomainRotationIntentBodyV1
    BodyDigest [32]byte
}

type DomainRotationCutoverBodyV1 struct {
    Version uint16
    MutationID string
    IntentBundleDigest, PreCutoverReceiptChainHead [32]byte
    ReceiptCount uint64
    CopyReceiptDigest, ModeReceiptDigest, DrainReceiptDigest [32]byte
    CandidateImportAppliedReceiptDigest [32]byte
    CandidateImportRevocationReceiptDigest [32]byte
    RotationPreEntry DomainIdentityRotationPreEntryPayload
    BuiltAt time.Time
}

type DomainRotationCutoverV1 struct {
    Body DomainRotationCutoverBodyV1
    BodyDigest [32]byte
}

type DomainIdentityRotationPlan struct {
    Version uint16
    MutationID, DeploymentID, ProjectID string
    Mode DomainRotationMode
    ActiveProfile, TargetDomainKind string
    GeneratedAt, ExpiresAt time.Time
    ChallengeArtifactDigest, CandidatePreparationArtifactDigest [32]byte
    CurrentSetDigest, CandidateSetDigest [32]byte
    CurrentSetEpoch, CandidateSetEpoch uint64
    ExpectedTrustPrimary, ExpectedTrustWitness recovery.TrustHead
    ExpectedLedgerPrimary, ExpectedLedgerWitness deletionledger.Head
    CurrentApprovalPolicyDigest [32]byte
    CurrentDomainAttestationPolicyDigest [32]byte
    CandidateDomainAttestationPolicyDigest [32]byte
    CurrentAdapterPolicyDigest, CandidateAdapterPolicyDigest [32]byte
    CurrentAdapterPolicyGeneration, CandidateAdapterPolicyGeneration uint64
    CandidatePossessionDigest [32]byte
    NonceReservationSigner CandidateNonceReservationSignerKeyDescriptorV1
    TransferSignerKeyID, CandidateReceiptSignerKeyID string
    TransferSignerPublicKey, CandidateReceiptSignerPublicKey [32]byte
    CleanupVerifier CandidateCleanupVerifierKeyDescriptorV1
    CopySourceInventoryDigest, CopyPolicyDigest [32]byte
    DrainScopeDigest, IntentBundleDigest [32]byte
    MinimumFenceContractVersion uint64
    IntentBundle DomainRotationIntentV1
}

type DomainRotationReceiptKindV1 string

const (
    RotationReceiptCopyManifest        DomainRotationReceiptKindV1 = "copy_manifest"
    RotationReceiptDualWriteCheckpoint DomainRotationReceiptKindV1 = "dual_write_checkpoint"
    RotationReceiptCurrentUnreachable  DomainRotationReceiptKindV1 = "current_unreachable"
    RotationReceiptDrain               DomainRotationReceiptKindV1 = "drain"
    RotationReceiptDrainContinuation   DomainRotationReceiptKindV1 = "drain_continuation"
    RotationReceiptCandidateImportApplied DomainRotationReceiptKindV1 = "candidate_import_applied"
    RotationReceiptCandidateImportRevoked DomainRotationReceiptKindV1 = "candidate_import_revoked"
    RotationReceiptCutover             DomainRotationReceiptKindV1 = "cutover"
    RotationReceiptCandidateCutoverApplied DomainRotationReceiptKindV1 = "candidate_cutover_applied"
    RotationReceiptProjection          DomainRotationReceiptKindV1 = "projection"
    RotationReceiptOldDomainRetired    DomainRotationReceiptKindV1 = "old_domain_retired"
    RotationReceiptFinalProof          DomainRotationReceiptKindV1 = "final_proof"
    RotationReceiptCandidateCutoverRevoked DomainRotationReceiptKindV1 = "candidate_cutover_revoked"
    RotationReceiptCandidateArtifactsPurged DomainRotationReceiptKindV1 = "candidate_artifacts_purged"
    RotationReceiptWorkspaceZero       DomainRotationReceiptKindV1 = "workspace_zero"
)

type DomainCandidatePossessionBodyV1 struct {
    Version uint16
    MutationID, TargetDomainKind, CandidateDomainID string
    CandidateStableIdentityDigest, ChallengeArtifactDigest, NonceDigest [32]byte
    PolicyDigest [32]byte
    KeyID string
    ObservedAt, ExpiresAt time.Time
}

type SignedDomainCandidatePossessionV1 struct {
    Body DomainCandidatePossessionBodyV1
    BodyDigest [32]byte
    Signature [64]byte
}

type DomainTransferPhase string
const (
    DomainTransferCopy DomainTransferPhase = "copy"
    DomainTransferDualWrite DomainTransferPhase = "dual_write"
    DomainTransferCutover DomainTransferPhase = "cutover"
)

type DomainTransferFrameKind string
const (
    DomainTransferStart DomainTransferFrameKind = "start"
    DomainTransferObjectStart DomainTransferFrameKind = "object_start"
    DomainTransferObjectChunk DomainTransferFrameKind = "object_chunk"
    DomainTransferObjectEnd DomainTransferFrameKind = "object_end"
    DomainTransferCheckpoint DomainTransferFrameKind = "checkpoint"
    DomainTransferEnd DomainTransferFrameKind = "end"
)

type DomainTransferObjectKindV1 string
const (
    TransferObjectDeletionLedgerEntry DomainTransferObjectKindV1 = "deletion_ledger_entry_v1"
    TransferObjectRecoveryTrustEntry DomainTransferObjectKindV1 = "recovery_trust_entry_v1"
    TransferObjectPlatformMutationPlan DomainTransferObjectKindV1 = "platform_mutation_plan_v1"
    TransferObjectMutationAuthorization DomainTransferObjectKindV1 = "mutation_authorization_v1"
    TransferObjectMutationBundle DomainTransferObjectKindV1 = "mutation_bundle_v1"
    TransferObjectMutationCompletion DomainTransferObjectKindV1 = "mutation_completion_receipt_v1"
    TransferObjectSignedRecoveryInventory DomainTransferObjectKindV1 = "signed_recovery_inventory_v1"
    TransferObjectRecoveryPointManifest DomainTransferObjectKindV1 = "recovery_point_manifest_v1"
    TransferObjectIdentitySet DomainTransferObjectKindV1 = "identity_set_v1"
    TransferObjectIdentitySetPrimaryReceipt DomainTransferObjectKindV1 = "identity_set_primary_receipt_v1"
    TransferObjectIdentitySetWitnessReceipt DomainTransferObjectKindV1 = "identity_set_witness_receipt_v1"
    TransferObjectDomainRotationReceipt DomainTransferObjectKindV1 = "domain_rotation_receipt_v1"
    TransferObjectImmutableHeadReceipt DomainTransferObjectKindV1 = "immutable_head_receipt_v1"
    TransferObjectAppACLManifest DomainTransferObjectKindV1 = "app_acl_manifest_v1"
)

type CanonicalObjectEnvelopeV1 struct {
    Kind DomainTransferObjectKindV1
    ObjectIdentityDigest, CanonicalDigest [32]byte
    CanonicalBytes []byte // recursively decoded by the closed Kind -> schema map
}

// Managed application/recovery rows and object payloads never use an opaque
// transfer object kind. PostgreSQL logical snapshot/WAL adapters and S3
// key/body inventory adapters copy them through their registered production
// schemas and return typed inventory/checkpoint receipts. This envelope is
// reserved for the closed governance schemas above.

type DomainTransferScopeV1 struct {
    MutationID string
    PlanDigest, AuthorizationArtifactDigest [32]byte
    IntentBundleDigest, CandidatePreparationArtifactDigest [32]byte
    StreamID [32]byte
    Phase DomainTransferPhase
}

type DomainTransferStartPayloadV1 struct {
    SourceInventoryDigest, CopyPolicyDigest [32]byte
    MaximumFrames, MaximumObjects, MaximumObjectBytes uint64
    MaximumChunkBytes, MaximumCanonicalBytes uint64
}

type DomainTransferObjectStartPayloadV1 struct {
    ObjectKind DomainTransferObjectKindV1
    ObjectIdentityDigest, CanonicalObjectDigest [32]byte
    CanonicalLength uint64
    ChunkCount uint32
}

type DomainTransferObjectChunkPayloadV1 struct {
    ObjectIdentityDigest [32]byte
    ChunkIndex, ChunkCount uint32
    Offset uint64
    ChunkDigest [32]byte
    CanonicalChunk []byte
}

type DomainTransferObjectEndPayloadV1 struct {
    ObjectIdentityDigest, CanonicalObjectDigest [32]byte
    CanonicalLength uint64
    ChunkCount uint32
}

type DomainTransferCheckpointPayloadV1 struct {
    FirstObjectStartSequence, LastObjectEndSequence uint64
    ObjectCount, CanonicalByteCount uint64
    ObjectSetDigest, SourceHeadDigest [32]byte
}

type DomainTransferEndPayloadV1 struct {
    TotalFrames, TotalObjects, TotalCanonicalBytes uint64
    PreEndFrameChainHead, FinalInventoryDigest [32]byte
}

type DomainTransferFramePayloadV1 struct {
    Start *DomainTransferStartPayloadV1
    ObjectStart *DomainTransferObjectStartPayloadV1
    ObjectChunk *DomainTransferObjectChunkPayloadV1
    ObjectEnd *DomainTransferObjectEndPayloadV1
    Checkpoint *DomainTransferCheckpointPayloadV1
    End *DomainTransferEndPayloadV1
}

type DomainTransferFrameBodyV1 struct {
    Version uint16
    Scope DomainTransferScopeV1
    Generation, Sequence uint64
    Kind DomainTransferFrameKind
    PreviousFrameDigest [32]byte
    Payload DomainTransferFramePayloadV1
}

type SignedDomainTransferFrameV1 struct {
    Body DomainTransferFrameBodyV1
    BodyDigest [32]byte
    SignerKeyID string
    Signature [64]byte
}

type DomainCutoverOperation string
const (
    DomainCutoverAppProjectionCAS DomainCutoverOperation = "app_projection_cas"
    DomainCutoverLedgerAppend DomainCutoverOperation = "ledger_append"
    DomainCutoverWitnessConfirm DomainCutoverOperation = "witness_confirm"
    DomainCutoverRecoveryTrustAppend DomainCutoverOperation = "recovery_trust_append"
)

type DomainCutoverOutcomeV1 string
const (
    DomainCutoverApplied DomainCutoverOutcomeV1 = "applied"
    DomainCutoverExactMatch DomainCutoverOutcomeV1 = "exact_match"
)

type DomainCutoverCommandBodyV1 struct {
    Version uint16
    Scope DomainTransferScopeV1
    CutoverBundleDigest, CandidateSetDigest [32]byte
    CandidateSetEpoch uint64
    CandidateDomainID string
    Operation DomainCutoverOperation
    ExpectedBeforeOrdinal uint64
    ExpectedBeforeHead [32]byte
    CanonicalPayload CanonicalObjectEnvelopeV1
    MinimumFenceContractVersion uint64
    ExpiresAt time.Time
}

type DomainCutoverCommandV1 struct {
    Body DomainCutoverCommandBodyV1
    BodyDigest [32]byte
    SignerKeyID string
    Signature [64]byte
}

type DomainCutoverExecutionReceiptBodyV1 struct {
    Version uint16
    MutationID string
    CommandDigest, CandidateSetDigest [32]byte
    CandidateSetEpoch uint64
    CandidateDomainID string
    Operation DomainCutoverOperation
    Outcome DomainCutoverOutcomeV1
    BeforeOrdinal, AfterOrdinal uint64
    BeforeHead, AfterHead, ResultDigest [32]byte
    ObservedAt time.Time
}

type SignedDomainCutoverExecutionReceiptV1 struct {
    Body DomainCutoverExecutionReceiptBodyV1
    BodyDigest [32]byte
    CandidateReceiptSignerKeyID string
    Signature [64]byte
}

type DomainRotationScopeV1 struct {
    MutationID, DeploymentID, ProjectID string
    ActiveProfile, TargetDomainKind string
    PlanDigest, IntentBundleDigest [32]byte
    CurrentSetDigest, CandidateSetDigest [32]byte
    CurrentSetEpoch, CandidateSetEpoch uint64
}

type RotationCopyProofV1 struct {
    Scope DomainRotationScopeV1
    SourceInventoryDigest, PolicyDigest [32]byte
    LedgerChainDigest, TrustChainDigest, GovernanceArtifactDigest [32]byte
    SignedInventoryDigest, ManagedDataInventoryDigest [32]byte
    SourceHeadDigest, CandidateHeadDigest [32]byte
    EntryCount, ArtifactCount, ManagedRowCount uint64
    ObservedAt time.Time
}

type RotationDualWriteProofV1 struct {
    Scope DomainRotationScopeV1
    FirstSequence, LastSequence uint64
    CurrentHeadDigest, CandidateHeadDigest, MirroredAppendSetDigest [32]byte
    ObservedAt time.Time
}

type DomainGovernanceSignatureBodyV1 struct {
    Version uint16
    Purpose, Scope, KeyID string
    PolicyDigest, ProofBodyDigest [32]byte
    SignedAt, ExpiresAt time.Time
}

type DomainGovernanceSignatureV1 struct {
    Body DomainGovernanceSignatureBodyV1
    Signature [64]byte
}

type RotationUnreachableProofBodyV1 struct {
    Scope DomainRotationScopeV1
    TargetDomainID string
    TargetStableIdentityDigest, QuarantineConfigDigest [32]byte
    RecoverySourceInventoryDigest, ReplayCheckpointDigest [32]byte
    ObservationStartedAt, ObservedAt, ExpiresAt time.Time
}

type SignedRotationUnreachableProofV1 struct {
    Body RotationUnreachableProofBodyV1
    BodyDigest [32]byte
    GovernanceSignatures []DomainGovernanceSignatureV1 // sorted unique, 1..64
}

type RotationDrainProofV1 struct {
    Scope DomainRotationScopeV1
    DeploymentEpoch, MinimumFenceContractVersion uint64
    ContinuationGeneration uint64
    PreviousDrainReceiptHash [32]byte
    DrainScopeDigest [32]byte
    CurrentAdapterPolicy SignedAdmissionAdapterPolicyV1
    LBSnapshot, QueueSnapshot SignedAdmissionSnapshotV1
    CopyReplaySnapshot SignedCopyReplaySnapshotV1
    OldTargetCount, CandidateUnreadyTargetCount uint64
    OldActiveConnectionCount, CandidateActiveConnectionCount uint64
    OldConsumerCount, CandidateConsumerCount uint64
    OldActiveLeaseCount, CandidateActiveLeaseCount uint64
    ObservedAt, ExpiresAt time.Time
}

type RotationCutoverProofV1 struct {
    Scope DomainRotationScopeV1
    IntentBundleDigest, PreCutoverReceiptChainHead [32]byte
    CutoverBundleDigest, RotationPreEntryDigest [32]byte
    MinimumFenceContractVersion uint64
    ObservedAt time.Time
}

type RotationCandidateImportAppliedProofV1 struct {
    Scope DomainRotationScopeV1
    StreamID, EndFrameDigest [32]byte
    Generation uint64
    ObjectSetDigest, FinalInventoryDigest, CandidateHeadDigest [32]byte
    TotalFrames, TotalObjects, TotalCanonicalBytes uint64
    ObservedAt time.Time
}

type SignedRotationCandidateImportAppliedProofV1 struct {
    Body RotationCandidateImportAppliedProofV1
    BodyDigest [32]byte
    CandidateReceiptSignerKeyID string
    Signature [64]byte
}

type RotationCredentialKindV1 string
const (
    RotationCredentialImport RotationCredentialKindV1 = "import"
    RotationCredentialCutover RotationCredentialKindV1 = "cutover"
)

type RotationCredentialRevocationProofV1 struct {
    Scope DomainRotationScopeV1
    CredentialKind RotationCredentialKindV1
    PrincipalDigest, LastAuthorizedReceiptHash [32]byte
    RevokedDatabaseACLDigest, RevokedIAMDigest [32]byte
    RemainingCredentialCount, RemainingSessionCount uint64
    ObservedAt time.Time
}

type SignedRotationCredentialRevocationProofV1 struct {
    Body RotationCredentialRevocationProofV1
    BodyDigest [32]byte
    CandidateReceiptSignerKeyID string
    Signature [64]byte
}

type RotationProjectionAuthorityV1 string
const (
    RotationProjectionCurrentRuntime RotationProjectionAuthorityV1 = "current_runtime"
    RotationProjectionCandidateCutover RotationProjectionAuthorityV1 = "candidate_cutover"
)

type RotationProjectionArmV1 struct {
    Authority RotationProjectionAuthorityV1
    BeforeLedgerSequence, AfterLedgerSequence uint64
    BeforeStateDigest, AfterStateDigest, CASReceiptDigest [32]byte
}

type RotationProjectionProofV1 struct {
    Scope DomainRotationScopeV1
    Current *RotationProjectionArmV1
    Candidate *RotationProjectionArmV1
    AdapterPolicyActivation AdapterPolicyActivationReceiptV1
    ObservedAt time.Time
}

type AdapterPolicyActivationReceiptBodyV1 struct {
    Version uint16
    MutationID, DeploymentID, ProjectID string
    RotationLedgerSequence uint64
    RotationLedgerEntryHash, IdentityProjectionDigest [32]byte
    CurrentPolicyDigest, CandidatePolicyDigest [32]byte
    CurrentGeneration, CandidateGeneration uint64
    Result string // unchanged|activated
    ActivatedAt time.Time
}

type AdapterPolicyActivationReceiptV1 struct {
    Body AdapterPolicyActivationReceiptBodyV1
    BodyDigest [32]byte
    ReceiptDigest [32]byte
}

type RotationRetirementProofV1 struct {
    Scope DomainRotationScopeV1
    RetiredDomainID string
    RevokedDatabaseACLDigest, RevokedIAMDigest, RoutingConfigDigest [32]byte
    RemainingWriterCount, RemainingConnectionCount uint64
    ObservedAt time.Time
}

type RotationFinalProofV1 struct {
    Scope DomainRotationScopeV1
    TrustHeadDigest, LedgerHeadDigest, PreFinalProofWitnessHeadDigest [32]byte
    ProjectionDigest, ReplayDigest, InventoryDigest [32]byte
    CandidateLivenessDigest, RetirementReceiptHash [32]byte
    CandidateControlPolicyHead CandidateControlPolicyChainHeadV1
    CleanupVerifier CandidateCleanupVerifierKeyDescriptorV1
    ObservedAt time.Time
}

type RotationCandidateArtifactsPurgeProofV1 struct {
    Scope DomainRotationScopeV1
    CredentialBundleManifestDigest, TransferWorkspaceInventoryDigest [32]byte
    ImportRevocationReceiptHash, CutoverRevocationReceiptHash [32]byte
    CandidateControlPurgeReceipt SignedCandidateEphemeralPurgeReceiptV1
    CandidateControlPurgeReceiptDigest [32]byte
    KeyDestructionSetDigest, WorkspacePurgeReceiptDigest [32]byte
    RemainingCredentialCount, RemainingWorkspaceCount uint64
    RemainingAEADKeyCount, RemainingNonceSigningKeyCount uint64
    CleanupVerifierIdentityDigest [32]byte
    ObservedAt time.Time
}

type SignedRotationCandidateArtifactsPurgeProofV1 struct {
    Body RotationCandidateArtifactsPurgeProofV1
    BodyDigest [32]byte
    PreparationDigest [32]byte
    CleanupVerifierKeyID string
    Signature [64]byte
    ReceiptDigest [32]byte
}

type RotationWorkspaceZeroProofV1 struct {
    Scope DomainRotationScopeV1
    CandidateArtifactsPurgeReceiptHash [32]byte
    CredentialBundleManifestDigest, TransferWorkspaceInventoryDigest [32]byte
    CleanupDatabaseObjectCount, CleanupS3ObjectCount, CleanupFilesystemObjectCount uint64
    RemainingCredentialCount, RemainingWorkspaceCount uint64
    RemainingAEADKeyCount, RemainingNonceSigningKeyCount uint64
    CleanupVerifierIdentityDigest [32]byte
    ObservedAt time.Time
}

type SignedRotationWorkspaceZeroProofV1 struct {
    Body RotationWorkspaceZeroProofV1
    BodyDigest [32]byte
    PreparationDigest [32]byte
    CleanupVerifierKeyID string
    Signature [64]byte
    ReceiptDigest [32]byte
}

type DomainRotationReceiptPayloadV1 struct {
    CopyManifest *RotationCopyProofV1
    DualWriteCheckpoint *RotationDualWriteProofV1
    CurrentUnreachable *SignedRotationUnreachableProofV1
    Drain *RotationDrainProofV1
    DrainContinuation *RotationDrainProofV1
    CandidateImportApplied *SignedRotationCandidateImportAppliedProofV1
    CandidateImportRevoked *SignedRotationCredentialRevocationProofV1
    Cutover *RotationCutoverProofV1
    CandidateCutoverApplied *SignedDomainCutoverExecutionReceiptV1
    Projection *RotationProjectionProofV1
    OldDomainRetired *RotationRetirementProofV1
    FinalProof *RotationFinalProofV1
    CandidateCutoverRevoked *SignedRotationCredentialRevocationProofV1
    CandidateArtifactsPurged *SignedRotationCandidateArtifactsPurgeProofV1
    WorkspaceZero *SignedRotationWorkspaceZeroProofV1
}

type DomainRotationReceiptBodyV1 struct {
    Version uint16
    MutationID string
    Sequence uint64
    Kind DomainRotationReceiptKindV1
    PreviousReceiptHash [32]byte
    CurrentSetDigest, CandidateSetDigest [32]byte
    Payload DomainRotationReceiptPayloadV1
    ObservedAt time.Time
}

type DomainRotationReceiptV1 struct {
    Body DomainRotationReceiptBodyV1
    ReceiptHash [32]byte
}
```

The receipt validator requires exactly one non-nil payload matching `Kind`; the 15 Go arm names above map one-for-one to `copy_manifest|dual_write_checkpoint|current_unreachable|drain|drain_continuation|candidate_import_applied|candidate_import_revoked|cutover|candidate_cutover_applied|projection|old_domain_retired|final_proof|candidate_cutover_revoked|candidate_artifacts_purged|workspace_zero`, and the generated Go/primary SQL/PostgreSQL-witness/S3 table contains no alias. Planned mode is `CopyManifest→DualWriteCheckpoint→Drain→DrainContinuation*→CandidateImportApplied→CandidateImportRevoked→Cutover→CandidateCutoverApplied→Projection→OldDomainRetired→FinalProof→CandidateCutoverRevoked→CandidateArtifactsPurged→WorkspaceZero`; disaster mode substitutes `CurrentUnreachable` for `DualWriteCheckpoint`; every other order is rejected. All kinds except `DrainContinuation` are singleton. Initial Drain has generation 0 and zero previous-drain hash; every continuation increments generation by one and binds the immediately preceding drain/continuation receipt hash. Cutover uses the newest live drain proof; all expired proofs remain immutable history. `CandidateImportApplied` must exact-match the witnessed transfer scope, End frame, reconstructed object set/inventory/head and totals, then be signed by the preparation-bound candidate receipt key, ingested on the current side, full-witnessed and read back. `CandidateImportRevoked` must bind that applied receipt hash as its last authorized receipt before Cutover; `CandidateCutoverRevoked` must bind the witnessed FinalProof; purge requires both revocations and zero bundle/workspace counts; `WorkspaceZero` binds the witnessed purge and independently reads both cleanup surfaces at zero. Import applied/revoked and cutover applied/revoked are signed only by the preparation-bound candidate receipt key. `CandidateArtifactsPurged` and `WorkspaceZero` are signed only by the preparation-bound cleanup-verifier key described in the witnessed control policy and intent; the shared negative corpus rejects either signer class on the other's purpose. Every wrapper is ingested on the current side and full-witnessed before completion. Projection truth table is exact: planned/application has current-runtime + candidate-cutover arms, disaster/application has candidate only, non-application has current only. Every payload's `Scope` exact-matches the witnessed plan/intent, including mutation, deployment/project/profile/target, plan/intent digests and adjacent set digests/epochs; cross-mutation or old-set proof reuse fails.

Challenge/preparation/attestation/control-policy canonical bodies use fixed magic/version/field-count and the written field order. Challenge body/wrapper limits are 256 KiB/512 KiB; preparation body/wrapper limits are 512 KiB/1 MiB. Governance envelopes sign `DomainGovernanceSignatureBodyV1` with purpose=`candidate_control_policy|candidate_challenge|candidate_preparation|current_unreachable`; `candidate_control_policy` additionally requires scope exactly `current|candidate`, while the other purposes have their fixed existing scope. `ProofBodyDigest` always names the corresponding signature-free body. Candidate possession binds the complete challenge artifact digest, not an ambiguous nonce/body alias. Signature sets are key-ID sorted/unique, exact-match the independently pinned/witnessed policy and satisfy threshold. A signature or wrapper digest never appears in its own preimage.

Transfer frames have fixed `Start|ObjectStart|ObjectChunk|ObjectEnd|Checkpoint|End` one-arm encoding, maximum canonical frame size 4 MiB, no compression and no allocation before header/length validation. `Start` carries signed frame/object/chunk/stream policy limits; `MaximumChunkBytes` is at most `4 MiB - canonical frame overhead`, each object is additionally bounded by its registered schema (plans 24 MiB, bundles 20 MiB, inventories 8 MiB), and the implementation caps a stream at 10,000,000 frames and 1 TiB, with every effective bound being the lower signed-policy/hard/schema value. Generation starts at 1; sequence starts at 1 with zero previous digest and increments without gaps. Within an object, chunk index starts at 0, count is constant, offset equals the preceding end, each chunk digest is verified before buffering, and ObjectEnd accepts only full `[0, CanonicalLength)` coverage, exact object digest and exhaustive production decode under the closed object-kind→schema map. Candidate persists insert-or-exact-match replay rows before applying a frame and never exposes a partial object. Same sequence/same bytes is retry; same sequence/different bytes, missing/duplicate Start/ObjectStart/ObjectEnd/End, gap/overlap, wrong index/count/offset/digest, post-End bytes, cross-phase/mutation replay, signer mismatch, truncated stream, inventory/count/chain mismatch, unknown object kind/schema or trailing decoded bytes fails. The strings `managed_postgresql_row_v1` and `managed_s3_object_v1` are explicitly unknown: managed data is copied only by the logical snapshot/WAL and exact key/body adapters with typed inventory/checkpoint receipts, never as opaque `CanonicalBytes`. Cutover operation is derived only from mode×target and carries a `CanonicalObjectEnvelopeV1` whose kind selects the same production decoder. Its candidate receipt must be signed by the preparation-bound receipt key and exact-match command, candidate identity, before/after head and result; it is ingested only through current-side resume and then full-witnessed.

The cutover bundle is built from the receipt-chain head after the latest Drain/Continuation, `CandidateImportApplied` and its binding `CandidateImportRevoked`; only then is Cutover appended, binding that post-revocation `PreCutoverReceiptChainHead` plus `CutoverBundleDigest`, so it cannot include itself. `RotationFinalProofV1.PreFinalProofWitnessHeadDigest` is the head before Final (through rotation, cutover execution, projection/replay/inventory and Retirement); the resulting witness head and later teardown receipts are bound only by completion/readback and never feed back into Final. Canonical rotation receipt bytes are 1..1 MiB; each nested proof body is ≤64 KiB except bounded copy inventory (≤1 MiB), strings use the identity-attestation NFC/length registry, and the common `ObservedAt` equals its payload (`Unreachable` uses end-of-window). Expiry is database-compared. Receipt payloads contain only typed IDs, counters, times, digests and signatures—never raw evidence bytes, endpoints, paths, filenames, argv, credentials, errors or free text. Golden tests mutate the post-Final witness head and prove it cannot alter the Final preimage.

- [ ] **Step 2: Add migration and immutable-ACL RED tests.** The following DDL is a normalized two-domain contract, not one concatenated migration: primary/root/policy/challenge/request/abandon/identity-set/rotation-receipt tables live only in recovery-control, while every Task-17 PostgreSQL mirror relation named `recovery_*_witnesses` lives only in the independent PostgreSQL full-witness database. This naming rule does not move the separately specified `record_platform_s3_witness_identities` / `record_platform_s3_witness_attestations` control-plane identity tables out of recovery-control. Each physical migration independently installs its own `record_platform_internal` helpers and immutable trigger function；neither file may reference the other database's tables. Because generation-1 candidate-control policy is published before a domain-rotation intent exists, `recovery_candidate_control_roots` is a narrow pre-intent root rather than a fake FK to a future mutation；later intent binding can only attach the same mutation ID and witnessed prepare-policy digest. PostgreSQL full witness mirrors complete candidate policy、challenge artifact、request signed core、abandon artifact、canonical primary-receipt copy、typed witness receipt、identity-set and rotation-receipt bytes. The persisted `canonical_fence|canonical_authorization|canonical_completion` columns and corresponding S3 body objects contain only their named `*ArtifactV1` (`Body+BodyDigest`)；receipt-bearing `*V1` proofs exist only after primary 2 + witness 3 readback. S3 stores challenge and request objects at the exact grammars already frozen above, the three policy objects under `record-platform/v1/<scope_hash>/bundles/<mutation_id>/candidate_control_policy/<20-digit-generation>/<policy|primary-receipt|witness-receipt>`, the three identity objects under `identity-sets/<20-digit-epoch>/<set|primary-receipt|witness-receipt>` and typed rotation receipts under `rotations/<mutation_id>/receipts/<20-digit-sequence>/receipt`. All immutable artifact/receipt rows reject UPDATE/DELETE/TRUNCATE；every same-database primary FK uses `ON DELETE RESTRICT`, while cross-domain witness linkage is proven by mutation/digest/chain bytes rather than an impossible cross-database FK. Only fixed-search-path definer functions can insert/exact-match, bind intent or advance the monotonic saga. `recovery_trust_mutations`, plan/bundle/completion tables and trust primary/witness union accept `domain_identity_rotate`；the trust arm forbids all key lifecycle fields, advances only revision/head/domain-attestation policy, and requires current/candidate sets, policy digests, candidate possession, both bundles and drain. Extend the activation production path to persist/full-witness-confirm epoch-1 identity-set history before bootstrap completion；rotation planning rejects a missing genesis row. The APP projection can only CAS from the current ledger sequence/hash and epoch to the next.

  The implementation output is exactly two independently executable embedded migrations: `db/recoverycontrol/migrations/0001_create_recovery_control.sql` owns every primary relation and primary command function, while `db/deletionwitness/migrations/0001_create_full_witness.sql` owns every PostgreSQL full-witness relation and confirm function. The fenced SQL below is a paired object-shape/check catalog ordered for contract review, not executable file order and never a third or concatenated migration. Each physical file must create its own local `record_platform_internal` schema、digest/decoder helpers and immutable-rejection function before first use；revoke PUBLIC locally；schema-qualify every application relation、index、trigger target and authorizable function as `public.*`；and qualify every helper as `record_platform_internal.*`. The recovery-control file must contain zero Task-17 PostgreSQL mirror relations named `recovery_*_witnesses` and zero PostgreSQL confirm functions；the full-witness file must contain zero primary/root/mutable-saga relations or cross-database FKs. The recovery-control S3 identity/attestation tables remain the explicit non-mirror exception above. Embed/parser/integration tests execute each file alone against its own fresh database while the other domain is absent/unreachable, execute that same file a second time against the same database, and assert catalog/ACL/checksum equality after both runs. A test that concatenates the files, installs both before inspecting either, relies on the other domain's helper, or leaves an unqualified `public` object does not satisfy this contract.

```sql
-- Normative paired object-shape catalog only; physical ownership and executable
-- order are the two-file contract immediately above.
create table recovery_candidate_control_roots (
  mutation_id text primary key check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  deployment_id text not null check (deployment_id ~ '^dp-[0-9a-f]{64}$'),
  project_id text not null check (project_id = 'default'),
  mode text not null check (mode in ('planned_migration','disaster_recovery')),
  target_domain_kind text not null check (target_domain_kind in
    ('application','deletion_ledger','deletion_witness','recovery_control')),
  policy_head_generation bigint not null check (policy_head_generation > 0),
  policy_head_digest bytea not null check (octet_length(policy_head_digest) = 32),
  state text not null check (state in
    ('policy_prepared','challenge_started','abandoning','intent_bound','complete','abandoned')),
  challenge_fence_digest bytea unique
    check (octet_length(challenge_fence_digest) = 32),
  challenge_primary_receipt_digest bytea unique
    check (octet_length(challenge_primary_receipt_digest) = 32),
  challenge_witness_receipt_digest bytea unique
    check (octet_length(challenge_witness_receipt_digest) = 32),
  challenge_started_at timestamptz,
  abandon_fence_epoch bigint not null default 0 check (abandon_fence_epoch >= 0),
  abandon_authorization_digest bytea
    check (octet_length(abandon_authorization_digest) = 32),
  abandon_completion_digest bytea
    check (octet_length(abandon_completion_digest) = 32),
  created_at timestamptz not null,
  intent_bound_at timestamptz,
  check (((state = 'policy_prepared' and intent_bound_at is null
      and abandon_fence_epoch = 0
      and challenge_fence_digest is null
      and challenge_primary_receipt_digest is null
      and challenge_witness_receipt_digest is null
      and challenge_started_at is null
      and abandon_authorization_digest is null and abandon_completion_digest is null)
    or (state = 'challenge_started' and intent_bound_at is null
      and abandon_fence_epoch = 0
      and challenge_fence_digest is not null
      and challenge_primary_receipt_digest is not null
      and challenge_started_at is not null
      and abandon_authorization_digest is null and abandon_completion_digest is null)
    or (state = 'abandoning' and intent_bound_at is null
      and abandon_fence_epoch > 0 and abandon_authorization_digest is not null
      and abandon_completion_digest is null
      and ((challenge_fence_digest is null
          and challenge_primary_receipt_digest is null
          and challenge_witness_receipt_digest is null
          and challenge_started_at is null)
        or (challenge_fence_digest is not null
          and challenge_primary_receipt_digest is not null
          and challenge_witness_receipt_digest is not null
          and challenge_started_at is not null)))
    or (state in ('intent_bound','complete') and intent_bound_at is not null
      and abandon_fence_epoch = 0
      and challenge_fence_digest is not null
      and challenge_primary_receipt_digest is not null
      and challenge_witness_receipt_digest is not null
      and challenge_started_at is not null
      and abandon_authorization_digest is null and abandon_completion_digest is null)
    or (state = 'abandoned' and intent_bound_at is null
      and abandon_fence_epoch > 0 and abandon_authorization_digest is not null
      and abandon_completion_digest is not null
      and ((challenge_fence_digest is null
          and challenge_primary_receipt_digest is null
          and challenge_witness_receipt_digest is null
          and challenge_started_at is null)
        or (challenge_fence_digest is not null
          and challenge_primary_receipt_digest is not null
          and challenge_witness_receipt_digest is not null
          and challenge_started_at is not null)))) is true)
);

create table recovery_candidate_control_challenges (
  mutation_id text primary key references recovery_candidate_control_roots(mutation_id)
    on delete restrict,
  policy_head_generation bigint not null check (policy_head_generation > 0),
  policy_head_digest bytea not null check (octet_length(policy_head_digest) = 32),
  canonical_fence bytea not null
    check (octet_length(canonical_fence) between 1 and 1048576),
  fence_digest bytea not null unique check (octet_length(fence_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 65536),
  primary_receipt_digest bytea not null unique
    check (octet_length(primary_receipt_digest) = 32),
  started_at timestamptz not null,
  check (fence_digest =
    record_platform_internal.digest(canonical_fence, 'sha256')),
  check (primary_receipt_digest =
    record_platform_internal.digest(canonical_primary_receipt, 'sha256')),
  check (record_platform_internal.candidate_control_challenge_fence_v1_matches(
    canonical_fence, canonical_primary_receipt, mutation_id,
    policy_head_generation, policy_head_digest, fence_digest,
    primary_receipt_digest, started_at) is true)
);

create table recovery_candidate_control_challenge_witnesses (
  mutation_id text primary key check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  policy_head_generation bigint not null check (policy_head_generation > 0),
  policy_head_digest bytea not null check (octet_length(policy_head_digest) = 32),
  canonical_fence bytea not null
    check (octet_length(canonical_fence) between 1 and 1048576),
  fence_digest bytea not null unique check (octet_length(fence_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 65536),
  primary_receipt_digest bytea not null unique
    check (octet_length(primary_receipt_digest) = 32),
  canonical_witness_receipt bytea not null
    check (octet_length(canonical_witness_receipt) between 1 and 65536),
  witness_receipt_digest bytea not null unique
    check (octet_length(witness_receipt_digest) = 32),
  started_at timestamptz not null,
  witnessed_at timestamptz not null,
  check (fence_digest =
    record_platform_internal.digest(canonical_fence, 'sha256')),
  check (primary_receipt_digest =
    record_platform_internal.digest(canonical_primary_receipt, 'sha256')),
  check (witness_receipt_digest =
    record_platform_internal.digest(canonical_witness_receipt, 'sha256')),
  check (record_platform_internal.candidate_control_challenge_witness_v1_matches(
    canonical_fence, canonical_primary_receipt, canonical_witness_receipt,
    mutation_id, policy_head_generation, policy_head_digest, fence_digest,
    primary_receipt_digest, witness_receipt_digest, started_at, witnessed_at) is true)
);

create table recovery_candidate_control_abandon_authorizations (
  mutation_id text primary key references recovery_candidate_control_roots(mutation_id)
    on delete restrict,
  abandon_fence_epoch bigint not null check (abandon_fence_epoch > 0),
  policy_head_generation bigint not null check (policy_head_generation > 0),
  policy_head_digest bytea not null check (octet_length(policy_head_digest) = 32),
  canonical_authorization bytea not null
    check (octet_length(canonical_authorization) between 1 and 1048576),
  authorization_digest bytea not null unique
    check (octet_length(authorization_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 1048576),
  primary_receipt_digest bytea not null unique
    check (octet_length(primary_receipt_digest) = 32),
  reserved_at timestamptz not null,
  check (authorization_digest =
    record_platform_internal.digest(canonical_authorization, 'sha256')),
  check (primary_receipt_digest =
    record_platform_internal.digest(canonical_primary_receipt, 'sha256')),
  check (record_platform_internal.candidate_control_abandon_authorization_v1_matches(
    canonical_authorization, canonical_primary_receipt, mutation_id,
    abandon_fence_epoch, policy_head_generation, policy_head_digest,
    primary_receipt_digest, reserved_at) is true)
);

create table recovery_candidate_control_abandon_authorization_witnesses (
  mutation_id text primary key check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  abandon_fence_epoch bigint not null check (abandon_fence_epoch > 0),
  policy_head_generation bigint not null check (policy_head_generation > 0),
  policy_head_digest bytea not null check (octet_length(policy_head_digest) = 32),
  canonical_authorization bytea not null
    check (octet_length(canonical_authorization) between 1 and 1048576),
  authorization_digest bytea not null unique check (octet_length(authorization_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 1048576),
  primary_receipt_digest bytea not null unique
    check (octet_length(primary_receipt_digest) = 32),
  canonical_witness_receipt bytea not null
    check (octet_length(canonical_witness_receipt) between 1 and 1048576),
  witness_receipt_digest bytea not null unique
    check (octet_length(witness_receipt_digest) = 32),
  witnessed_at timestamptz not null,
  check (authorization_digest =
    record_platform_internal.digest(canonical_authorization, 'sha256')),
  check (primary_receipt_digest =
    record_platform_internal.digest(canonical_primary_receipt, 'sha256')),
  check (witness_receipt_digest =
    record_platform_internal.digest(canonical_witness_receipt, 'sha256')),
  check (record_platform_internal.candidate_control_abandon_witness_v1_matches(
    canonical_authorization, canonical_primary_receipt, canonical_witness_receipt,
    mutation_id, abandon_fence_epoch, policy_head_generation,
    policy_head_digest, authorization_digest, primary_receipt_digest,
    witness_receipt_digest) is true)
);

create table recovery_candidate_control_abandon_completions (
  mutation_id text primary key references recovery_candidate_control_roots(mutation_id)
    on delete restrict,
  policy_head_generation bigint not null check (policy_head_generation > 0),
  policy_head_digest bytea not null check (octet_length(policy_head_digest) = 32),
  authorization_digest bytea not null unique check (octet_length(authorization_digest) = 32),
  candidate_purge_receipt bytea not null
    check (octet_length(candidate_purge_receipt) between 1 and 1048576),
  candidate_purge_receipt_digest bytea not null unique
    check (octet_length(candidate_purge_receipt_digest) = 32),
  workspace_zero_receipt bytea not null
    check (octet_length(workspace_zero_receipt) between 1 and 1048576),
  workspace_zero_receipt_digest bytea not null unique
    check (octet_length(workspace_zero_receipt_digest) = 32),
  canonical_completion bytea not null
    check (octet_length(canonical_completion) between 1 and 1048576),
  completion_digest bytea not null unique check (octet_length(completion_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 1048576),
  primary_receipt_digest bytea not null unique
    check (octet_length(primary_receipt_digest) = 32),
  completed_at timestamptz not null,
  check (candidate_purge_receipt_digest =
    record_platform_internal.candidate_ephemeral_purge_receipt_digest_v1(
      candidate_purge_receipt)),
  check (workspace_zero_receipt_digest =
    record_platform_internal.candidate_abandon_workspace_zero_digest_v1(
      workspace_zero_receipt)),
  check (completion_digest =
    record_platform_internal.digest(canonical_completion, 'sha256')),
  check (primary_receipt_digest =
    record_platform_internal.digest(canonical_primary_receipt, 'sha256')),
  check (record_platform_internal.candidate_control_abandon_completion_v1_matches(
    canonical_completion, canonical_primary_receipt, mutation_id,
    policy_head_generation, policy_head_digest, authorization_digest,
    candidate_purge_receipt, candidate_purge_receipt_digest,
    workspace_zero_receipt, workspace_zero_receipt_digest, completed_at) is true)
);

create table recovery_candidate_control_abandon_completion_witnesses (
  mutation_id text primary key check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  policy_head_generation bigint not null check (policy_head_generation > 0),
  policy_head_digest bytea not null check (octet_length(policy_head_digest) = 32),
  authorization_digest bytea not null unique check (octet_length(authorization_digest) = 32),
  candidate_purge_receipt bytea not null
    check (octet_length(candidate_purge_receipt) between 1 and 1048576),
  candidate_purge_receipt_digest bytea not null unique
    check (octet_length(candidate_purge_receipt_digest) = 32),
  workspace_zero_receipt bytea not null
    check (octet_length(workspace_zero_receipt) between 1 and 1048576),
  workspace_zero_receipt_digest bytea not null unique
    check (octet_length(workspace_zero_receipt_digest) = 32),
  canonical_completion bytea not null
    check (octet_length(canonical_completion) between 1 and 1048576),
  completion_digest bytea not null unique check (octet_length(completion_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 1048576),
  primary_receipt_digest bytea not null unique
    check (octet_length(primary_receipt_digest) = 32),
  canonical_witness_receipt bytea not null
    check (octet_length(canonical_witness_receipt) between 1 and 1048576),
  witness_receipt_digest bytea not null unique
    check (octet_length(witness_receipt_digest) = 32),
  witnessed_at timestamptz not null,
  check (candidate_purge_receipt_digest =
    record_platform_internal.candidate_ephemeral_purge_receipt_digest_v1(
      candidate_purge_receipt)),
  check (workspace_zero_receipt_digest =
    record_platform_internal.candidate_abandon_workspace_zero_digest_v1(
      workspace_zero_receipt)),
  check (completion_digest =
    record_platform_internal.digest(canonical_completion, 'sha256')),
  check (primary_receipt_digest =
    record_platform_internal.digest(canonical_primary_receipt, 'sha256')),
  check (witness_receipt_digest =
    record_platform_internal.digest(canonical_witness_receipt, 'sha256')),
  check (record_platform_internal.candidate_control_abandon_completion_witness_v1_matches(
    canonical_completion, canonical_primary_receipt, canonical_witness_receipt,
    mutation_id, policy_head_generation, policy_head_digest,
    authorization_digest, candidate_purge_receipt, candidate_purge_receipt_digest,
    workspace_zero_receipt, workspace_zero_receipt_digest, completion_digest,
    primary_receipt_digest) is true)
);

create table recovery_candidate_control_policies (
  mutation_id text not null,
  generation bigint not null check (generation > 0),
  phase text not null check (phase in ('prepare','import','cutover','cleanup')),
  phase_revision bigint not null check (phase_revision > 0),
  previous_policy_digest bytea not null check (octet_length(previous_policy_digest) = 32),
  canonical_policy bytea not null check (octet_length(canonical_policy) between 1 and 1048576),
  policy_digest bytea not null unique check (octet_length(policy_digest) = 32),
  activation_prerequisite_kind text not null check (activation_prerequisite_kind in
    ('none','durable_intent','import_revoked','final_proof')),
  activation_prerequisite_digest bytea not null
    check (octet_length(activation_prerequisite_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 1048576),
  primary_receipt_digest bytea not null unique
    check (octet_length(primary_receipt_digest) = 32),
  published_at timestamptz not null,
  primary key (mutation_id, generation),
  unique (mutation_id, phase, phase_revision),
  foreign key (mutation_id) references recovery_candidate_control_roots(mutation_id)
    on delete restrict,
  check (policy_digest = record_platform_internal.digest(canonical_policy, 'sha256')),
  check (primary_receipt_digest =
    record_platform_internal.digest(canonical_primary_receipt, 'sha256')),
  check (record_platform_internal.candidate_control_policy_v1_matches(
    canonical_policy, mutation_id, generation, phase, phase_revision,
    previous_policy_digest, activation_prerequisite_kind,
    activation_prerequisite_digest) is true),
  check (record_platform_internal.candidate_control_policy_primary_receipt_v1_matches(
    canonical_primary_receipt, mutation_id, generation, phase, phase_revision,
    previous_policy_digest, policy_digest, activation_prerequisite_kind,
    activation_prerequisite_digest, published_at) is true),
  check (((generation = 1 and phase = 'prepare' and phase_revision = 1
      and previous_policy_digest = decode(repeat('00', 32), 'hex'))
    or (generation > 1 and previous_policy_digest <> decode(repeat('00', 32), 'hex'))) is true),
  check (((phase = 'prepare' and activation_prerequisite_kind = 'none'
      and activation_prerequisite_digest = decode(repeat('00', 32), 'hex'))
    or (phase = 'import' and activation_prerequisite_kind = 'durable_intent'
      and activation_prerequisite_digest <> decode(repeat('00', 32), 'hex'))
    or (phase = 'cutover' and activation_prerequisite_kind = 'import_revoked'
      and activation_prerequisite_digest <> decode(repeat('00', 32), 'hex'))
    or (phase = 'cleanup' and activation_prerequisite_kind = 'final_proof'
      and activation_prerequisite_digest <> decode(repeat('00', 32), 'hex'))) is true)
);

create table recovery_candidate_control_policy_witnesses (
  mutation_id text not null check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  generation bigint not null check (generation > 0),
  phase text not null check (phase in ('prepare','import','cutover','cleanup')),
  phase_revision bigint not null check (phase_revision > 0),
  previous_policy_digest bytea not null check (octet_length(previous_policy_digest) = 32),
  canonical_policy bytea not null check (octet_length(canonical_policy) between 1 and 1048576),
  policy_digest bytea not null unique check (octet_length(policy_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 1048576),
  primary_receipt_digest bytea not null
    check (octet_length(primary_receipt_digest) = 32),
  canonical_witness_receipt bytea not null
    check (octet_length(canonical_witness_receipt) between 1 and 1048576),
  witness_receipt_digest bytea not null unique
    check (octet_length(witness_receipt_digest) = 32),
  activation_prerequisite_kind text not null check (activation_prerequisite_kind in
    ('none','durable_intent','import_revoked','final_proof')),
  activation_prerequisite_digest bytea not null
    check (octet_length(activation_prerequisite_digest) = 32),
  published_at timestamptz not null,
  witnessed_at timestamptz not null,
  primary key (mutation_id, generation),
  check (policy_digest = record_platform_internal.digest(canonical_policy, 'sha256')),
  check (primary_receipt_digest =
    record_platform_internal.digest(canonical_primary_receipt, 'sha256')),
  check (witness_receipt_digest =
    record_platform_internal.digest(canonical_witness_receipt, 'sha256')),
  check (record_platform_internal.candidate_control_policy_v1_matches(
    canonical_policy, mutation_id, generation, phase, phase_revision,
    previous_policy_digest, activation_prerequisite_kind,
    activation_prerequisite_digest) is true),
  check (record_platform_internal.candidate_control_policy_primary_receipt_v1_matches(
    canonical_primary_receipt, mutation_id, generation, phase, phase_revision,
    previous_policy_digest, policy_digest, activation_prerequisite_kind,
    activation_prerequisite_digest, published_at) is true),
  check (record_platform_internal.candidate_control_policy_witness_receipt_v1_matches(
    canonical_witness_receipt, mutation_id, generation, phase, policy_digest,
    primary_receipt_digest, witnessed_at) is true)
);

create table recovery_candidate_recovery_requests (
  mutation_id text not null,
  policy_generation bigint not null check (policy_generation > 0),
  purpose text not null check (purpose in
    ('abandon','import','cutover_apply','revoke_import','revoke_cutover','cleanup')),
  canonical_signed_request bytea not null
    check (octet_length(canonical_signed_request) between 1 and 33554432),
  request_digest bytea not null unique check (octet_length(request_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 65536),
  primary_receipt_digest bytea not null unique
    check (octet_length(primary_receipt_digest) = 32),
  issued_at timestamptz not null,
  expires_at timestamptz not null,
  primary key (mutation_id, policy_generation, purpose),
  foreign key (mutation_id) references recovery_candidate_control_roots(mutation_id)
    on delete restrict,
  foreign key (mutation_id, policy_generation)
    references recovery_candidate_control_policies(mutation_id, generation)
    on delete restrict,
  check (expires_at > issued_at),
  check (request_digest =
    record_platform_internal.candidate_recovery_request_digest_v1(
      canonical_signed_request)),
  check (primary_receipt_digest =
    record_platform_internal.candidate_recovery_request_primary_receipt_digest_v1(
      canonical_primary_receipt)),
  check (record_platform_internal.candidate_recovery_request_v1_matches(
    canonical_signed_request, mutation_id, policy_generation, purpose,
    request_digest, issued_at, expires_at) is true),
  check (record_platform_internal.candidate_recovery_request_primary_receipt_v1_matches(
    canonical_signed_request, canonical_primary_receipt, mutation_id,
    policy_generation, purpose, request_digest, primary_receipt_digest,
    issued_at, expires_at) is true)
);

create table recovery_candidate_recovery_request_witnesses (
  mutation_id text not null check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  policy_generation bigint not null check (policy_generation > 0),
  purpose text not null check (purpose in
    ('abandon','import','cutover_apply','revoke_import','revoke_cutover','cleanup')),
  canonical_signed_request bytea not null
    check (octet_length(canonical_signed_request) between 1 and 33554432),
  request_digest bytea not null unique check (octet_length(request_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 65536),
  primary_receipt_digest bytea not null
    check (octet_length(primary_receipt_digest) = 32),
  canonical_witness_receipt bytea not null
    check (octet_length(canonical_witness_receipt) between 1 and 65536),
  witness_receipt_digest bytea not null unique
    check (octet_length(witness_receipt_digest) = 32),
  issued_at timestamptz not null,
  expires_at timestamptz not null,
  witnessed_at timestamptz not null,
  primary key (mutation_id, policy_generation, purpose),
  check (expires_at > issued_at and witnessed_at >= issued_at),
  check (request_digest =
    record_platform_internal.candidate_recovery_request_digest_v1(
      canonical_signed_request)),
  check (primary_receipt_digest =
    record_platform_internal.candidate_recovery_request_primary_receipt_digest_v1(
      canonical_primary_receipt)),
  check (witness_receipt_digest =
    record_platform_internal.candidate_recovery_request_witness_receipt_digest_v1(
      canonical_witness_receipt)),
  check (record_platform_internal.candidate_recovery_request_witness_v1_matches(
    canonical_signed_request, canonical_primary_receipt, canonical_witness_receipt,
    mutation_id, policy_generation, purpose, request_digest,
    primary_receipt_digest, witness_receipt_digest,
    issued_at, expires_at, witnessed_at) is true)
);

create table recovery_domain_identity_sets (
  deployment_id text not null check (deployment_id ~ '^dp-[0-9a-f]{64}$'),
  project_id text not null check (project_id = 'default'),
  active_profile text not null check (active_profile in ('postgres_sync','s3_worm')),
  set_epoch bigint not null check (set_epoch > 0),
  previous_set_digest bytea not null check (octet_length(previous_set_digest) = 32),
  canonical_set bytea not null check (octet_length(canonical_set) between 1 and 1048576),
  set_digest bytea not null unique check (octet_length(set_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 1048576),
  primary_receipt_digest bytea not null unique
    check (octet_length(primary_receipt_digest) = 32),
  domain_attestation_policy_digest bytea not null
    check (octet_length(domain_attestation_policy_digest) = 32),
  mutation_id text not null check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  ledger_sequence bigint not null check (ledger_sequence > 0),
  ledger_entry_hash bytea not null check (octet_length(ledger_entry_hash) = 32),
  activated_at timestamptz not null,
  primary key (deployment_id, project_id, set_epoch),
  foreign key (mutation_id) references recovery_trust_mutations(mutation_id)
    on delete restrict,
  check (set_digest =
    record_platform_internal.digest(canonical_set, 'sha256')),
  check (primary_receipt_digest =
    record_platform_internal.digest(canonical_primary_receipt, 'sha256')),
  check (record_platform_internal.domain_identity_set_body_v1_matches(
    canonical_set, deployment_id, project_id, active_profile, set_epoch,
    previous_set_digest, domain_attestation_policy_digest) is true),
  check (record_platform_internal.identity_set_primary_receipt_v1_matches(
    canonical_primary_receipt, mutation_id, deployment_id, project_id,
    active_profile, set_epoch, previous_set_digest, set_digest,
    ledger_sequence, ledger_entry_hash, activated_at) is true),
  check (((set_epoch = 1 and ledger_sequence = 1
      and previous_set_digest = decode(repeat('00', 32), 'hex'))
    or (set_epoch > 1 and ledger_sequence > 1
      and previous_set_digest <> decode(repeat('00', 32), 'hex'))) is true)
);

create table recovery_domain_identity_rotations (
  mutation_id text primary key,
  deployment_id text not null check (deployment_id ~ '^dp-[0-9a-f]{64}$'),
  project_id text not null check (project_id = 'default'),
  plan_digest bytea not null unique check (octet_length(plan_digest) = 32),
  authorization_artifact_digest bytea not null
    check (octet_length(authorization_artifact_digest) = 32),
  challenge_artifact_digest bytea not null
    check (octet_length(challenge_artifact_digest) = 32),
  candidate_preparation_artifact_digest bytea not null
    check (octet_length(candidate_preparation_artifact_digest) = 32),
  mode text not null check (mode in ('planned_migration','disaster_recovery')),
  active_profile text not null check (active_profile in ('postgres_sync','s3_worm')),
  target_domain_kind text not null check (target_domain_kind in
    ('application','deletion_ledger','deletion_witness','recovery_control')),
  current_set_epoch bigint not null check (current_set_epoch > 0),
  candidate_set_epoch bigint not null check (candidate_set_epoch > 1),
  current_set_digest bytea not null check (octet_length(current_set_digest) = 32),
  candidate_set_digest bytea not null check (octet_length(candidate_set_digest) = 32),
  current_domain_id text not null check (current_domain_id ~ '^rd-[0-9a-f]{64}$'),
  candidate_domain_id text not null check (candidate_domain_id ~ '^rd-[0-9a-f]{64}$'),
  current_stable_identity_digest bytea not null
    check (octet_length(current_stable_identity_digest) = 32),
  candidate_stable_identity_digest bytea not null
    check (octet_length(candidate_stable_identity_digest) = 32),
  current_domain_attestation_policy_digest bytea not null
    check (octet_length(current_domain_attestation_policy_digest) = 32),
  candidate_domain_attestation_policy_digest bytea not null
    check (octet_length(candidate_domain_attestation_policy_digest) = 32),
  candidate_possession_digest bytea not null
    check (octet_length(candidate_possession_digest) = 32),
  candidate_receipt_signer_key_id text not null
    check (candidate_receipt_signer_key_id ~ '^dk-sha256-[0-9a-f]{64}$'),
  copy_source_inventory_digest bytea not null
    check (octet_length(copy_source_inventory_digest) = 32),
  copy_policy_digest bytea not null check (octet_length(copy_policy_digest) = 32),
  drain_scope_digest bytea not null check (octet_length(drain_scope_digest) = 32),
  intent_bundle_digest bytea not null check (octet_length(intent_bundle_digest) = 32),
  cutover_bundle_digest bytea check (octet_length(cutover_bundle_digest) = 32),
  dual_write_checkpoint_digest bytea
    check (octet_length(dual_write_checkpoint_digest) = 32),
  current_unreachable_digest bytea
    check (octet_length(current_unreachable_digest) = 32),
  drain_receipt_digest bytea check (octet_length(drain_receipt_digest) = 32),
  candidate_import_applied_receipt_digest bytea
    check (octet_length(candidate_import_applied_receipt_digest) = 32),
  candidate_import_revocation_receipt_digest bytea
    check (octet_length(candidate_import_revocation_receipt_digest) = 32),
  rotation_ledger_entry_hash bytea check (octet_length(rotation_ledger_entry_hash) = 32),
  candidate_cutover_execution_receipt_digest bytea
    check (octet_length(candidate_cutover_execution_receipt_digest) = 32),
  projection_receipt_digest bytea check (octet_length(projection_receipt_digest) = 32),
  old_domain_retirement_receipt_digest bytea
    check (octet_length(old_domain_retirement_receipt_digest) = 32),
  final_proof_receipt_digest bytea check (octet_length(final_proof_receipt_digest) = 32),
  candidate_cutover_revocation_receipt_digest bytea
    check (octet_length(candidate_cutover_revocation_receipt_digest) = 32),
  candidate_artifacts_purge_receipt_digest bytea
    check (octet_length(candidate_artifacts_purge_receipt_digest) = 32),
  workspace_zero_receipt_digest bytea
    check (octet_length(workspace_zero_receipt_digest) = 32),
  candidate_liveness_digest bytea check (octet_length(candidate_liveness_digest) = 32),
  state text not null check (state in
    ('intent','copy_pending','dual_write_pending','current_unreachable_pending',
     'drain_pending','import_revoke_pending','trust_primary_unknown','trust_witness_pending',
     'ledger_primary_unknown','ledger_witness_pending','candidate_cutover_pending',
     'cutover_projection_pending','retirement_pending','final_proof_pending',
     'candidate_teardown_pending','completion_pending','complete')),
  created_at timestamptz not null default now(),
  completed_at timestamptz,
  foreign key (mutation_id) references recovery_trust_mutations(mutation_id)
    on delete restrict,
  check (candidate_set_epoch = current_set_epoch + 1),
  check (candidate_set_digest <> current_set_digest),
  check (candidate_domain_id <> current_domain_id),
  check (candidate_stable_identity_digest <> current_stable_identity_digest),
  check (((mode = 'planned_migration' and current_unreachable_digest is null)
    or (mode = 'disaster_recovery' and dual_write_checkpoint_digest is null)) is true),
  check (((state <> 'complete' and completed_at is null)
    or (state = 'complete' and completed_at is not null
      and cutover_bundle_digest is not null
      and ((mode = 'planned_migration' and dual_write_checkpoint_digest is not null)
        or (mode = 'disaster_recovery' and current_unreachable_digest is not null))
      and drain_receipt_digest is not null and rotation_ledger_entry_hash is not null
      and candidate_import_applied_receipt_digest is not null
      and candidate_import_revocation_receipt_digest is not null
      and candidate_cutover_execution_receipt_digest is not null
      and projection_receipt_digest is not null
      and old_domain_retirement_receipt_digest is not null
      and final_proof_receipt_digest is not null
      and candidate_cutover_revocation_receipt_digest is not null
      and candidate_artifacts_purge_receipt_digest is not null
      and workspace_zero_receipt_digest is not null
      and candidate_liveness_digest is not null)) is true)
);

create unique index recovery_one_active_domain_rotation
  on recovery_domain_identity_rotations (deployment_id, project_id, active_profile)
  where state <> 'complete';

create table recovery_domain_identity_rotation_receipts (
  mutation_id text not null,
  receipt_sequence bigint not null check (receipt_sequence > 0),
  receipt_kind text not null check (receipt_kind in
    ('copy_manifest','dual_write_checkpoint','current_unreachable','drain',
     'drain_continuation','candidate_import_applied','candidate_import_revoked','cutover',
     'candidate_cutover_applied','projection','old_domain_retired','final_proof',
     'candidate_cutover_revoked','candidate_artifacts_purged','workspace_zero')),
  previous_receipt_hash bytea not null check (octet_length(previous_receipt_hash) = 32),
  canonical_receipt bytea not null check (octet_length(canonical_receipt) between 1 and 1048576),
  receipt_hash bytea not null unique check (octet_length(receipt_hash) = 32),
  witnessed_at timestamptz not null default now(),
  primary key (mutation_id, receipt_sequence),
  foreign key (mutation_id) references recovery_domain_identity_rotations(mutation_id)
    on delete restrict,
  check (receipt_hash =
    record_platform_internal.digest(canonical_receipt, 'sha256')),
  check (record_platform_internal.domain_rotation_receipt_v1_matches(
    canonical_receipt, mutation_id, receipt_sequence, receipt_kind,
    previous_receipt_hash) is true),
  check (((receipt_sequence = 1
      and previous_receipt_hash = decode(repeat('00', 32), 'hex'))
    or (receipt_sequence > 1
      and previous_receipt_hash <> decode(repeat('00', 32), 'hex'))) is true)
);

create unique index recovery_domain_rotation_singleton_receipt_kind
  on recovery_domain_identity_rotation_receipts (mutation_id, receipt_kind)
  where receipt_kind <> 'drain_continuation';

-- PostgreSQL full-witness mirror. Cross-database FKs are impossible; mutation and
-- chain linkage are instead byte-validated by the narrow confirm functions.
create table recovery_domain_identity_set_witnesses (
  deployment_id text not null check (deployment_id ~ '^dp-[0-9a-f]{64}$'),
  project_id text not null check (project_id = 'default'),
  active_profile text not null check (active_profile in ('postgres_sync','s3_worm')),
  set_epoch bigint not null check (set_epoch > 0),
  previous_set_digest bytea not null check (octet_length(previous_set_digest) = 32),
  canonical_set bytea not null check (octet_length(canonical_set) between 1 and 1048576),
  set_digest bytea not null unique check (octet_length(set_digest) = 32),
  canonical_primary_receipt bytea not null
    check (octet_length(canonical_primary_receipt) between 1 and 1048576),
  primary_receipt_digest bytea not null
    check (octet_length(primary_receipt_digest) = 32),
  canonical_witness_receipt bytea not null
    check (octet_length(canonical_witness_receipt) between 1 and 1048576),
  witness_receipt_digest bytea not null unique
    check (octet_length(witness_receipt_digest) = 32),
  domain_attestation_policy_digest bytea not null
    check (octet_length(domain_attestation_policy_digest) = 32),
  mutation_id text not null check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  ledger_sequence bigint not null check (ledger_sequence > 0),
  ledger_entry_hash bytea not null check (octet_length(ledger_entry_hash) = 32),
  activated_at timestamptz not null,
  witnessed_at timestamptz not null default now(),
  primary key (deployment_id, project_id, set_epoch),
  check (set_digest =
    record_platform_internal.digest(canonical_set, 'sha256')),
  check (primary_receipt_digest =
    record_platform_internal.digest(canonical_primary_receipt, 'sha256')),
  check (witness_receipt_digest =
    record_platform_internal.digest(canonical_witness_receipt, 'sha256')),
  check (record_platform_internal.domain_identity_set_body_v1_matches(
    canonical_set, deployment_id, project_id, active_profile, set_epoch,
    previous_set_digest, domain_attestation_policy_digest) is true),
  check (record_platform_internal.identity_set_primary_receipt_v1_matches(
    canonical_primary_receipt, mutation_id, deployment_id, project_id,
    active_profile, set_epoch, previous_set_digest, set_digest,
    ledger_sequence, ledger_entry_hash, activated_at) is true),
  check (record_platform_internal.identity_set_witness_receipt_v1_matches(
    canonical_witness_receipt, mutation_id, deployment_id, project_id,
    active_profile, set_epoch, previous_set_digest, set_digest,
    primary_receipt_digest, ledger_sequence, ledger_entry_hash,
    activated_at, witnessed_at) is true),
  check (((set_epoch = 1 and ledger_sequence = 1
      and previous_set_digest = decode(repeat('00', 32), 'hex'))
    or (set_epoch > 1 and ledger_sequence > 1
      and previous_set_digest <> decode(repeat('00', 32), 'hex'))) is true)
);

create table recovery_domain_identity_rotation_receipt_witnesses (
  mutation_id text not null check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  receipt_sequence bigint not null check (receipt_sequence > 0),
  receipt_kind text not null check (receipt_kind in
    ('copy_manifest','dual_write_checkpoint','current_unreachable','drain',
     'drain_continuation','candidate_import_applied','candidate_import_revoked','cutover',
     'candidate_cutover_applied','projection','old_domain_retired','final_proof',
     'candidate_cutover_revoked','candidate_artifacts_purged','workspace_zero')),
  previous_receipt_hash bytea not null check (octet_length(previous_receipt_hash) = 32),
  canonical_receipt bytea not null check (octet_length(canonical_receipt) between 1 and 1048576),
  receipt_hash bytea not null unique check (octet_length(receipt_hash) = 32),
  witnessed_at timestamptz not null default now(),
  primary key (mutation_id, receipt_sequence),
  check (receipt_hash =
    record_platform_internal.digest(canonical_receipt, 'sha256')),
  check (record_platform_internal.domain_rotation_receipt_v1_matches(
    canonical_receipt, mutation_id, receipt_sequence, receipt_kind,
    previous_receipt_hash) is true),
  check (((receipt_sequence = 1
      and previous_receipt_hash = decode(repeat('00', 32), 'hex'))
    or (receipt_sequence > 1
      and previous_receipt_hash <> decode(repeat('00', 32), 'hex'))) is true)
);

create unique index recovery_domain_rotation_witness_singleton_receipt_kind
  on recovery_domain_identity_rotation_receipt_witnesses (mutation_id, receipt_kind)
  where receipt_kind <> 'drain_continuation';

-- recovery-control migration only: install locally before primary-table triggers.
create or replace function record_platform_internal.reject_immutable_mutation()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog
as $$
begin
  raise exception using
    errcode = '55000',
    message = 'record-platform immutable artifact cannot be mutated';
  return null;
end
$$;

revoke all on function record_platform_internal.reject_immutable_mutation() from public;

create trigger rp_candidate_challenge_immutable
before update or delete or truncate on recovery_candidate_control_challenges
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_candidate_abandon_auth_immutable
before update or delete or truncate on recovery_candidate_control_abandon_authorizations
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_candidate_abandon_completion_immutable
before update or delete or truncate on recovery_candidate_control_abandon_completions
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_candidate_policy_immutable
before update or delete or truncate on recovery_candidate_control_policies
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_candidate_request_immutable
before update or delete or truncate on recovery_candidate_recovery_requests
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_domain_identity_set_immutable
before update or delete or truncate on recovery_domain_identity_sets
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_domain_rotation_receipt_immutable
before update or delete or truncate on recovery_domain_identity_rotation_receipts
for each statement execute function record_platform_internal.reject_immutable_mutation();

-- full-witness migration only: repeat the same local function definition and
-- PUBLIC revoke above before creating these witness-table triggers.
create or replace function record_platform_internal.reject_immutable_mutation()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog
as $$
begin
  raise exception using
    errcode = '55000',
    message = 'record-platform immutable artifact cannot be mutated';
  return null;
end
$$;

revoke all on function record_platform_internal.reject_immutable_mutation() from public;

create trigger rp_candidate_challenge_witness_immutable
before update or delete or truncate on recovery_candidate_control_challenge_witnesses
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_candidate_abandon_auth_witness_immutable
before update or delete or truncate on recovery_candidate_control_abandon_authorization_witnesses
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_candidate_abandon_completion_witness_immutable
before update or delete or truncate on recovery_candidate_control_abandon_completion_witnesses
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_candidate_policy_witness_immutable
before update or delete or truncate on recovery_candidate_control_policy_witnesses
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_candidate_request_witness_immutable
before update or delete or truncate on recovery_candidate_recovery_request_witnesses
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_domain_identity_set_witness_immutable
before update or delete or truncate on recovery_domain_identity_set_witnesses
for each statement execute function record_platform_internal.reject_immutable_mutation();
create trigger rp_domain_rotation_receipt_witness_immutable
before update or delete or truncate on recovery_domain_identity_rotation_receipt_witnesses
for each statement execute function record_platform_internal.reject_immutable_mutation();
```

`public.record_platform_append_candidate_control_policy_v1(bytea)` is the only primary policy publisher. It locks the control root/head, recursively validates the complete signed policy and typed primary receipt, verifies exact generation/phase revision/predecessor and the closed transition `prepare→import→cutover→cleanup`, and reads the actual phase prerequisite from the typed intent/receipt chain rather than trusting a caller digest. Once `final_proof` exists, it also byte-compares the entire cleanup-verifier descriptor with the descriptor bound by that proof and rejects any adapter/purpose/key/public-key/key-identity/exclusion-proof/validity substitution. It insert-or-exact-matches immutable bytes and CASes the mutable root head. `public.record_platform_confirm_candidate_control_policy_v1(bytea)` is the only PostgreSQL-witness policy publisher; it independently repeats the post-Final descriptor equality check, locks its independent tail, requires the complete canonical primary receipt copy, derives the witness receipt and insert-or-exact-matches the three artifacts. S3 confirmation uses the same Go decoder and post-Final invariant, then writes `policy→primary-receipt→witness-receipt` with COMPLIANCE/legal-hold/readback before returning. Every existing-row and ack-loss path reads and exact-matches all primary 2 + witness 3 artifacts, then validates the continuous policy chain from genesis through the independently enumerated immutable local tail and any farther physical tail before returning success；a policy row without complete receipts, generation gap/fork, stale prerequisite, verifier replacement or far tail is unusable.

`public.record_platform_start_candidate_control_challenge_v1(bytea)` locks the same root, accepts only `policy_prepared`, recursively validates `CandidateControlChallengeFenceArtifactV1` plus its typed primary receipt, insert-or-exact-matches both immutable bytes, records the fence/primary digests and performs the sole `policy_prepared→challenge_started` CAS with witness digest still null. `public.record_platform_confirm_candidate_control_challenge_v1(bytea)` independently stores the byte-identical artifact、canonical primary-receipt copy and typed witness receipt. Its fresh、existing-row and ack-loss success paths all read the complete target primary 2 + witness 3 object set and validate the continuous policy/challenge chain from genesis through the independently enumerated immutable local tail and any farther physical tail；an exact target row alone is never success. Only after that proof may the caller invoke `public.record_platform_ack_candidate_control_challenge_v1(bytea)` to set the root witness-receipt digest. A `challenge_started` row with null witness digest is a recoverable publication cutpoint but cannot emit challenge bytes、bind intent or begin abandon；a non-null digest must exact-match the full witness row. `public.record_platform_bind_candidate_control_intent_v1(bytea)` can move only fully acknowledged `challenge_started→intent_bound` after the domain intent reuses the exact mutation/mode/target and witnessed generation-1 prepare policy plus challenge artifact. Start、ack、bind and abandon all serialize on the same root lock/CAS, so intent and abandon cannot both win. PUBLIC and direct table writes are revoked；platform-admin receives only the exact primary functions and the disjoint full-witness identity only the matching confirms. Candidate/import/cutover/cleanup principals receive none.

The full-witness mirror is complete without duplicating mutable saga state: `public.recovery_mutation_witness_artifacts` stores plan, authorization and rotation intent/cutover bundles, the typed completion-witness table stores completion, existing trust/ledger witness tables store final entries, candidate-control tables store every policy、challenge、recovery-request and abandon artifact with its 2+3 publication proof, and the remaining exact tables store identity sets and the append-only typed receipt chain. `public.record_platform_append_candidate_recovery_request_v1(bytea)` validates the purpose-bound signed core、policy generation、signature、closed prerequisite arm、issued/expiry times and typed primary receipt before insert-or-exact-match under `(mutation_id,policy_generation,purpose)`. `public.record_platform_confirm_candidate_recovery_request_v1(bytea)` stores the byte-identical core、canonical primary-receipt copy and typed witness receipt；its fresh、same-tuple existing-row and every ack-loss path must read the complete target primary 2 + witness 3 object set and validate the continuous policy/request publication chain from genesis through the independently enumerated immutable local tail and any farther physical tail before returning the original receipts. Same tuple/same bytes returns the originals only after that proof；changed time/core/receipt is a stable conflict, and expiry never authorizes re-signing under that tuple. Fixed-search-path primary functions parse canonical bytes with the Go/SQL truth table, lock the prior set/receipt and atomically insert-or-exact-match set/receipt N plus the primary receipt. Only after full primary readback may the corresponding witness function accept that primary receipt, lock its own prior set/receipt and insert-or-exact-match the byte-identical artifact, the complete canonical primary receipt and its digest, plus the witness receipt that binds that digest. Thus PostgreSQL and S3 full witnesses each preserve the same independently decodable V1 artifacts and can reconstruct them without the primary. The caller reads back all primary 2 + witness 3 artifacts and repeats the same genesis-to-immutable-far-tail proof before any proof is assembled for FD output or either receipt digest enters completion. Rotation receipt primary/witness functions both invoke the same closed-kind production decoder and follow the same mutation/sequence/previous-hash/typed-payload ordering. Immutable triggers reject UPDATE/DELETE/TRUNCATE. PostgreSQL integration compares recovery-control and witness columns/bytes as exact sets after every cutpoint；S3 stores the exact challenge、request、abandon、policy、identity-set and rotation grammars frozen above. A missing mirror object, extra far-tail object, kind/payload mismatch or mutable-saga row presented as witness evidence fails the full-chain proof.

`public.record_platform_reserve_candidate_control_abandon_v1(bytea)` is a separate terminal operation, not a `cleanup` phase or a sixteenth rotation receipt. It locks the candidate-control root, accepts `policy_prepared` or fully acknowledged `challenge_started`, proves no typed rotation intent exists in primary or full witness, increments `abandon_fence_epoch`, changes state to `abandoning` and writes `CandidateControlAbandonAuthorizationArtifactV1` plus its typed primary receipt. The `policy_prepared` arm requires all challenge fields null；the `challenge_started` arm requires exact primary 2 + witness 3 challenge readback and embeds the complete `CandidateControlChallengeFenceV1`. A partially published challenge cannot be bypassed: it must first reconcile witness/ack, then may abandon while the immutable deadline and descriptors remain valid. `public.record_platform_confirm_candidate_control_abandon_authorization_v1(bytea)` stores the byte-identical artifact、canonical primary-receipt copy/digest and typed witness receipt before candidate deletion begins. Because bind and reserve share the root lock, only `challenge_started→intent_bound` or `policy_prepared|challenge_started→abandoning` can win. Candidate abandon uses the witnessed prepare-policy head and registered surface inventory even if preparation sealing never completed；an optional local preparation can only exact-match. After cleanup-verifier purge/workspace-zero receipts arrive, `public.record_platform_complete_candidate_control_abandon_v1(bytea)` atomically stores `CandidateControlAbandonCompletionArtifactV1`、typed primary receipt and policy-head generation/digest；`public.record_platform_confirm_candidate_control_abandon_completion_v1(bytea)` copies the full artifact and canonical primary receipt into the witness and appends the typed witness receipt. Both SQL matchers recursively verify the complete nested purge/workspace-zero wrapper bytes and their domain-separated digests. Fresh、existing-row and ack-loss confirmation paths each prove the complete target primary 2 + witness 3 set plus the continuous policy/challenge/abandon chain from genesis through immutable far-tail；only that proof may move `abandoning→abandoned`. No `final_proof`, cleanup phase policy or rotation receipt chain is required.

For `s3_worm`, abandon authorization and completion each use policy-held immutable body→typed primary receipt→typed witness receipt objects under the closed `candidate_control_abandon` grammar; PostgreSQL uses the two primary/two witness tables above with the same canonical body/receipt field order and digests. One shared golden/negative corpus mutates body, policy head, primary copy/digest and witness receipt independently across PostgreSQL and S3. Confirm/readback always proves primary 2 + witness 3 for each publication, and root state never treats a mutable row or S3 list result as abandonment evidence.

- [ ] **Step 3: Implement authenticated challenge and threshold-sealed candidate preparation.** Current-side `domain candidate challenge draft` performs only read operations against external current adapters, but it is state-mutating inside recovery-control: it requires the complete published/read-back generation-1 prepare control policy, reuses its cryptographic `tm-*` mutation ID exactly, builds the signature-free body, then completes the narrow start primary 2 + witness 3 publication/ack before emitting a 0600 artifact. The body binds that policy, current set/epoch, mode/profile/target, witnessed policies/heads, copy/drain scope, transfer signer, a new random nonce and ≤15-minute expiry. It never generates or substitutes a mutation ID, and crash after primary insertion resumes the same canonical artifact rather than observing a new nonce/time. Offline `domain governance sign` opens no database/network and emits one purpose-bound current-policy signature. `domain candidate challenge seal` re-reads the fully witnessed current policy/head and challenge fence, sorts/deduplicates signatures, verifies the threshold and is the sole producer of `SignedDomainCandidateChallengeV1`; the candidate never trusts policy bytes carried only by the challenge.

  `domain candidate prepare` uses only the fourth, ephemeral candidate environment, exact-matches its independent pinned-current policy, verifies the sealed challenge and writes a signature-free preparation draft containing `DomainAttestationBodyV1`, possession proof, candidate identity, schema/ACL/copy-exclusion facts, import/cutover/cleanup principals, credential-bundle manifest, provision-revocation receipt, cleanup handle, signed candidate-control policy, the distinct nonce-reservation signer descriptor, candidate receipt signer descriptor and cleanup-verifier public identity. It reads no governance private key. For PostgreSQL it applies only the target's current migration ledger/schema, creates exact runtime/admin/import/cutover/cleanup roles and narrow functions, runs fresh/repeat/checksum/catalog-role tests, writes the nonce to registered ephemeral staging, then closes and revokes provision owner. For S3 it verifies two pre-created, physically distinct surfaces without creating or weakening either: target WORM is checked only for immutable imported evidence and never receives nonce/staging/credential/scratch bytes; every prepare nonce and mutable artifact goes only to the purgeable candidate-control root. Preparation atomically replaces prepare credentials with separate import-only, mutation-scoped cutover and cleanup credentials; prepare credentials are absent before copy. The only copy exclusion is byte-enumerated local identity/liveness/possession metadata. A physical snapshot with the same PostgreSQL system identifier remains the same domain and cannot be a candidate.

  Offline `domain attestation sign --purpose candidate-attestation` is run once per candidate-policy key over the attestation body and emits only `DomainAttestationSignatureV1`; offline `domain governance sign --purpose candidate-preparation` signs the preparation body and emits only `DomainGovernanceSignatureV1`. Neither opens a candidate/current endpoint. `domain candidate preparation seal` sorts/deduplicates both signature sets, verifies both candidate-policy thresholds and every candidate-only key proof, and is the sole producer of `SignedDomainCandidatePreparationV1`. The final planner accepts only the sealed challenge+preparation, adopts their exact mutation ID and rejects regenerated IDs, changed scope, expiry, policy, signer or byte mismatch. It builds `DomainRotationIntentV1` with both complete wrappers and all typed policies/sets/copy/drain material; the first durable transaction and full witness persist those bytes, so later status/resume/cleanup reconstruct them by mutation ID. Before durable intent, current-side `domain candidate recovery-request export --purpose abandon` first reserves and full-witnesses the no-intent abandon fence; candidate-side `domain candidate abandon` then uses that request, the prepare-policy-bound cleanup authority and registered inventory to revoke any emitted principals, destroy staging/bundle/workspace and ask the isolated verifier for purge/workspace-zero receipts. It does not require a sealed preparation; an optional one can only exact-match. After durable intent, abandon is impossible and optional local policy/preparation inputs can only exact-match request bytes reconstructed from full witness. No command retains credential bytes or paths. Single-signature threshold bypass, self-contained unpinned policy and `prepare --domain-attestation-key` are parser/test failures.

  The sealed preparation, `DomainRotationIntentBodyV1` and `PlatformMutationPlanV1` must each expose the complete `CandidateNonceReservationSignerKeyDescriptorV1` and exact-match it to the descriptor inside the published prepare policy. They also retain the independent candidate receipt signer and cleanup-verifier identity; any cross-wrapper omission, partial cleanup descriptor, key substitution, validity drift or byte mismatch is a canonical validation failure before intent persistence.

- [ ] **Step 4: Implement split-process genesis copy and catch-up adapters.** `domain transfer export` runs with the current admin environment but opens every source pool/object namespace read-only; it explicitly opens one strict 0400 recovery signing key, derives key ID/public key, compares both to the witnessed intent transfer signer before writing byte 1, then emits signed, sequence-numbered, bounded chunk frames. It has no candidate input or mutation method. `domain transfer import` runs with the import-only candidate bundle, verifies every frame/mutation/plan/source inventory digest, reassembles each object with exact index/count/offset coverage, recursively decodes it through the closed kind→production-schema map and commits only after whole-object hash verification; it writes only candidate staging/import surfaces and has no current credential. Connect them only through a protected pipe/socket, or a registered transfer workspace whose every persisted chunk uses `CandidateEncryptedContentV1` under the mutation policy; plaintext scratch files are forbidden. The nonce reservation is durable before encryption and the workspace bytes/expiry/key descriptor/destruction/purge receipts enter inventory. Stream ledger entries, trust entries, canonical plans/authorizations/bundles/completion artifacts, signed inventories/manifests and receipt chains from genesis; verify every body, wrapper digest, previous hash, immutable tail and absence of candidate extra entries. Exact-max tests cover 24 MiB plan, 20 MiB bundle and 8 MiB inventory across multiple sub-4-MiB frames plus one-byte-over rejection. PostgreSQL data adapters use a logical snapshot plus WAL/LSN catch-up and compare bounded canonical row inventories, never physical pages. Planned dual-write uses the same exporter/importer frame protocol and only current-originated append bytes. S3 compares exact key/body/digest; provider version IDs and timestamps may differ, but COMPLIANCE retention cannot be shorter and legal hold must remain set. Replay deletion fences before exposing candidate data. After a valid stream End, importer emits `SignedRotationCandidateImportAppliedProofV1`; current resume must append/full-witness/readback it before cleanup can revoke import and emit the binding revocation receipt. Reject a gap, overlap, far tail, duplicate/out-of-order/conflicting chunk, single-byte mismatch, missing genesis artifact, unresolved workspace/mutation, copy-source inventory drift, candidate retention downgrade, wrong AEAD key/AAD/tag or nonce reuse; parser/open spies prove exporter candidate calls and importer current calls are both 0.

  Import revocation is an explicit crash-safe handshake, not an implicit side effect of cleanup. After current primary/full-witness readback of `candidate_import_applied`, current exports a full-witness-derived `CandidateRecoveryRequestV1(purpose=revoke_import)`; `domain candidate credential revoke --kind import` revokes/exact-matches only that credential and writes its preparation-bound signed receipt to an inherited FD without destroying the candidate receipt key or bundle. Current `resume --receipt-fd` append/full-witness/readback must succeed before the import policy is considered revoked, before cutover policy activation and before a cutover bundle can be built. A crash or lost transport retries the same canonical receipt by mutation ID.

- [ ] **Step 5: Implement the forward-only planned/disaster saga and strict cutover handoff.** The first transaction persists canonical plan, current-scope detached authorization, typed rotation intent containing the complete sealed challenge/preparation and root/rotation rows; primary and full witness use the same production decoder and byte corpus. Planned mode keeps current authoritative and mirrors only identical new append bytes until a witnessed `dual_write_checkpoint`; candidate-originated writes or candidate-ahead state close admission. Disaster mode forbids dual-write and accepts only `domain identity unreachable draft → governance sign --purpose current-unreachable → unreachable seal → resume --unreachable-proof`; the sealed proof binds quarantine and full replay checkpoint from surviving registered sources, and every lost-domain adapter spy remains at zero calls. Both modes append typed copy/mode/final-drain receipts, then enforce `candidate_import_applied → candidate_import_revoked`: both are preparation-key-signed singleton receipts, current-side primary/full-witness/readback precedes each transition, and revocation binds applied receipt hash. Only then build the typed cutover bundle and Cutover receipt. They append/full-witness the keyless rotation trust entry, then append/full-witness `domain_identity_rotation`.

  Entering `candidate_cutover_pending`, current-side `domain transfer cutover export` reconstructs the full mutation DAG and derives exactly one mode×target operation and signed command frame; the CLI cannot select another operation. Candidate-side `domain transfer cutover apply` holds only the mutation-scoped arm, verifies that frame, applies or exact-matches the one canonical payload/head and returns a preparation-bound receipt-key signature. Current-side `domain identity resume --receipt-fd FD` validates the receipt, appends it to the typed primary chain, confirms it to the full witness and reads both back before entering `cutover_projection_pending`; candidate cannot write current recovery-control. The trust replay preserves active recovery key, key roster and recovery approval policy byte-for-byte; only domain-attestation policy may advance with the candidate set. Before append, the service proves the plan's full witnessed `(sequence, hash, effective minimum)`. Primary SQL compares only its own expected head and effective minimum; PostgreSQL/S3 witness confirmation independently validates the actual witnessed head/effective minimum derived from the continuous chain. Every ack-loss branch reads by mutation ID and byte-compares the entire chain.

  The production adapter selected for each write is exact: planned application uses current recovery/ledger/witness; planned ledger additionally mirrors the entry to candidate ledger; planned witness confirms both current+candidate witnesses; planned recovery mirrors trust to candidate recovery. Disaster application uses current recovery/ledger/witness; disaster ledger writes the rebuilt candidate ledger and confirms with surviving current witness, making zero calls to lost ledger; disaster witness writes current recovery+ledger and confirms only rebuilt candidate witness, making zero calls to old witness; disaster recovery writes rebuilt candidate recovery plus current ledger/witness, making zero calls to old recovery. Any missing genesis source or an unexpected call to the lost domain fails the saga. If the target is ledger/witness/recovery, candidate contains the required new chain with no far tail before projection.

- [ ] **Step 6: Project, retire, tear down and only then complete.** After rotation ledger primary + required witness durability, publish epoch N+1 in the exact `primary set → typed primary receipt → witness set + canonical primary-receipt copy + typed witness receipt(binding primary digest) → five-artifact/2+3 full readback` order; both receipt digests are ineligible for completion until readback succeeds. Missing/extra/gapped set history, or a missing/tampered witness copy of the primary receipt, blocks projection. For planned application replacement, current APP runtime `FenceProjector` CASes authoritative state and candidate cutover exact-mirrors it; both receipts match before route switch. Disaster application makes zero old-APP calls and candidate cutover alone executes CAS. Non-APP targets use current APP runtime projection. Candidate HTTP/worker admission remains disabled until the projection receipt is witnessed and config management installs independently generated long-lived runtime/admin credentials; import/cutover/cleanup secrets are never copied into base env.

  Revoke old-domain write/EXECUTE/IAM, remove routing/config, prove zero writer/connection and witness `old_domain_retired`. Build, append, full-witness and read back `final_proof` only after candidate genesis-through-rotation chain, projection/replay/inventory watermarks and fresh liveness match; the canonical proof contains the complete `CandidateControlPolicyChainHeadV1` and full cleanup-verifier descriptor before entering `candidate_teardown_pending`. Publish/read back the cleanup control-policy generation, making the cutover credential zero-authority while keeping both signer descriptors byte-identical. Current then exports `CandidateRecoveryRequestV1(purpose=revoke_cutover)`; `domain candidate credential revoke --kind cutover` performs only the provider/DB revocation and returns the preparation-key-signed `candidate_cutover_revoked` over its inherited receipt FD. It must retain the candidate receipt key, credential bundle, AEAD/nonce key material and workspace until current `domain identity resume --receipt-fd` append/full-witness/readback succeeds. Only a later full-witness-derived `CandidateRecoveryRequestV1(purpose=cleanup)` carrying that acknowledgment lets `domain candidate cleanup` destroy the receipt key and bundle, purge registered encrypted transfer workspace, destroy local/wrapped per-mutation DEKs and nonce-reservation private key, and remove every candidate-control live object/version/delete marker/multipart upload. The preparation-bound cleanup verifier—whose read-control identity and strict regular/no-follow/bounded 0400 Ed25519 key are outside the cleaned surface—reprobes exact policy/control state and returns body/wrapper-separated `candidate_artifacts_purged` plus `workspace_zero` receipts over the inherited FD; unsigned typed AEAD/wrapped-DEK/nonce destruction evidence sits inside the purge body, and neither final receipt is staged on candidate-control. Current-side `domain identity resume --receipt-fd` ingests each receipt; each must be append-only, full-witnessed and read back, and workspace-zero independently verifies DB/S3/filesystem cleanup plus remaining AEAD and nonce-signing key counts are zero. Transport/append ack-loss first resolves existing canonical singleton bytes from primary/full witness; only proven absence permits a fresh observation. Only then enter `completion_pending` and construct a typed completion receipt binding the final rotation receipt-chain head; candidate-import-applied/import-revoked/cutover/revocation/purge/workspace-zero digests; typed identity-set primary/witness receipt digests; explicit candidate-control policy-head generation/digest; and its final typed primary/witness publication receipt digests. Primary and witness completion schemas carry all four policy-head/publication fields, and S3 decodes the same body. Store through the typed recovery-control function, confirm through the typed full-witness function and read back the whole DAG; only that readback may atomically set rotation/root state `complete`. Projection, retirement, final and teardown receipts never feed back into committed ledger/trust preimages. A reappearing or writable old domain immediately closes admission.

- [ ] **Step 7: Expose exact CLI and deployment contracts.** Implement these command shapes. File-producing commands accept only a configured/allowlisted `--output-root ROOT`, derive the complete `ManagedFilesystemGrammarV1` relative path from command kind plus canonical IDs and use no-follow 0600 exclusive create. Streaming commands accept only an already-open inherited FD and never open a caller-selected leaf path:

  ```text
  domain candidate control-policy draft --phase prepare
    --mode planned-migration|disaster-recovery
    --target application|deletion-ledger|deletion-witness|recovery-control
    --copy-policy PATH --drain-scope PATH --mutation-deadline RFC3339-UTC
    --nonce-reservation-signer-descriptor PATH
    --cleanup-verifier-descriptor PATH --output-root ROOT
  domain candidate control-policy draft --phase prepare|import|cutover|cleanup
    --mutation-id ID --previous-policy PATH --output-root ROOT
  domain governance sign --purpose candidate-control-policy --scope current|candidate
    --body PATH --policy PATH --governance-key PATH --output-root ROOT
  domain candidate control-policy seal --draft PATH
    --current-signature PATH [...] --candidate-signature PATH [...] --output-root ROOT
  domain candidate control-policy publish --policy PATH
  domain candidate challenge draft --mode planned-migration|disaster-recovery
    --target application|deletion-ledger|deletion-witness|recovery-control
    --control-policy PATH --copy-policy PATH --drain-scope PATH --output-root ROOT
  domain governance sign --purpose candidate-challenge|candidate-preparation|current-unreachable
    --body PATH
    --policy PATH --governance-key PATH --output-root ROOT
  domain candidate challenge seal --draft PATH --signature PATH [...] --output-root ROOT
  domain candidate prepare --challenge PATH --control-policy PATH --output-root ROOT
    (--aead-local-key-file PATH | --aead-kms-config PATH --aead-kms-credential-file PATH)
  domain attestation sign --purpose candidate-attestation
    --body PATH --policy PATH --governance-key PATH --output-root ROOT
  domain candidate preparation seal --body PATH
    --attestation-signature PATH [...] --preparation-signature PATH [...] --output-root ROOT
  domain identity plan --challenge PATH --candidate-preparation PATH
    --copy-policy PATH --drain-scope PATH --output-root ROOT
  domain identity apply --plan PATH --approval PATH [...]
  domain identity unreachable draft --mutation-id ID --quarantine-snapshot PATH
    --recovery-source-inventory PATH --replay-checkpoint PATH --output-root ROOT
  domain identity unreachable seal --draft PATH --signature PATH [...] --output-root ROOT
  domain candidate recovery-request export --mutation-id ID
    --purpose abandon|import|cutover-apply|revoke-import|revoke-cutover|cleanup
    --recovery-signing-key PATH --output-fd FD
  domain transfer export --mutation-id ID --recovery-signing-key PATH --output-fd FD
  domain transfer import --mutation-id ID [--control-policy PATH]
    --recovery-request-fd FD --input-fd FD --receipt-fd FD
    (--aead-local-key-file PATH | --aead-kms-config PATH --aead-kms-credential-file PATH)
  domain transfer cutover export --mutation-id ID --recovery-signing-key PATH --output-fd FD
  domain transfer cutover apply [--control-policy PATH]
    --recovery-request-fd FD --command-fd FD --receipt-fd FD
    (--aead-local-key-file PATH | --aead-kms-config PATH --aead-kms-credential-file PATH)
  domain candidate credential revoke --kind import|cutover --mutation-id ID
    --recovery-request-fd FD --receipt-fd FD
    (--aead-local-key-file PATH | --aead-kms-config PATH --aead-kms-credential-file PATH)
  domain identity status --mutation-id ID
  domain identity drain --mutation-id ID --lb-snapshot PATH --queue-snapshot PATH
    --copy-replay-snapshot PATH --output-root ROOT
  domain identity drain --continue-mutation ID --lb-snapshot PATH --queue-snapshot PATH
    --copy-replay-snapshot PATH --output-root ROOT
  domain identity resume --mutation-id ID [--unreachable-proof PATH]
    [--drain-proof PATH] [--receipt-fd FD]
  domain candidate abandon --mutation-id ID --recovery-request-fd FD
    [--control-policy PATH] [--candidate-preparation PATH] --verifier-request-fd FD
    (--aead-local-key-file PATH | --aead-kms-config PATH --aead-kms-credential-file PATH)
  domain candidate cleanup --mutation-id ID --recovery-request-fd FD
    [--control-policy PATH] [--candidate-preparation PATH] --verifier-request-fd FD
    (--aead-local-key-file PATH | --aead-kms-config PATH --aead-kms-credential-file PATH)
  domain candidate cleanup verify --request-fd FD --receipt-fd FD
    --cleanup-verifier-signing-key PATH
  ```

  `domain candidate recovery-request export`、`domain transfer export` and `domain transfer cutover export` share one production recovery-key loader and no alternate environment/default-key path. It opens the explicit path with no-follow semantics, uses post-open metadata to require one regular file with exact mode 0400, performs a bounded read that detects trailing byte 65, and accepts only a raw 32-byte Ed25519 seed or a raw 64-byte Ed25519 private key whose stored public half is self-consistent with the seed-derived public key. The derived signer ID/public key must exact-match the purpose-appropriate witnessed descriptor, and both that descriptor and the immutable mutation deadline must still be live. A missing flag rejects before stat/network/FD write；a missing or unreadable file、symlink、non-regular file、wrong mode、short/long read、wrong length/format、inconsistent 64-byte key、wrong signer、expired descriptor or expired deadline fails closed and writes exactly zero bytes to the inherited output FD. Read-only witness lookup may be required to prove signer/validity mismatch, but no publication or output write may precede the complete key、descriptor and deadline checks；no diagnostic may contain key bytes.

  The generation-1 prepare form requires both explicit canonical public descriptor paths plus an absolute UTC mutation deadline, and rejects `--mutation-id|--previous-policy`. Deadline must be after draft time, no later than draft+30 days, and no later than either descriptor expiry；every credential/policy expiry must also be no later than that immutable deadline. Every renewal or phase-advance form requires both `--mutation-id` and the complete witnessed `--previous-policy`, inherits mutation deadline plus nonce-reservation signer and cleanup-verifier descriptors byte-for-byte, and rejects deadline/descriptor reinjection before stat. `--phase prepare` in the second form is legal only for same-phase renewal；phase advance remains exactly `prepare→import→cutover→cleanup`. Descriptor files are regular/no-follow/bounded strict-0400 canonical public artifacts containing no private key or local path. State handling is exact: `policy_prepared` may enter witnessed no-intent abandon；a fully acknowledged `challenge_started` may do so only after exact primary 2 + witness 3 readback and ack；and a partially published `challenge_started` must first reconcile that same 2+3 publication and ack before it can bind intent or reserve abandon. Descriptor or mutation-deadline expiry is terminal fail-closed for the mutation and never permits replacement identities. Only the terminal `abandoned` state permits a new mutation or different descriptors.

  `domain candidate recovery-request export` is the only current-side producer of `CandidateRecoveryRequestV1`. For post-intent purposes it reconstructs complete policy/preparation/intent and prerequisite receipts from primary/full witness, adds typed primary and witness proofs and writes only the inherited FD; for `abandon` it first performs the no-intent fence CAS and 2+3 authorization readback. Candidate commands require this FD and verify its whole chain against independently pinned governance policy before any local path or endpoint access. Optional `--control-policy` and `--candidate-preparation` are never authoritative and may only byte-exact-match request contents, so durable intent plus local artifact/control-store loss remains resumable. Purpose, command and phase are exact: import/cutover/revoke/cleanup requests cannot be replayed across operations.

  `domain candidate credential revoke` is the only parser that revokes import or cutover credentials and emits the corresponding preparation-key-signed receipt. It retains the receipt private key, encrypted bundle and destruction material until the subsequent request proves current primary/full-witness readback of that receipt. `domain candidate cleanup` accepts only a cleanup request that proves cutover revocation readback; `domain candidate abandon` accepts only an abandon authorization and has no rotation-intent/receipt-chain arm. Both send a bounded zero-state request to the isolated verifier FD; neither can sign purge/workspace-zero itself.

  The generation-1 prepare `control-policy draft` creates the cryptographic mutation ID；every later policy, challenge, signature, sealed wrapper, preparation and final plan must reuse it. `control-policy draft` is online but read-only against current witness and candidate read-control probes and holds zero candidate write credential. `challenge draft` is read-only against every external current adapter but is deliberately state-mutating inside recovery-control only through the narrow challenge start/confirm/ack path；it emits no challenge bytes until exact primary 2 + witness 3 readback and ack. `control-policy publish` is current-side and cannot use a policy until its complete wrapper has primary/full-witness readback. `domain governance sign` and `domain attestation sign` are offline-only, have disjoint closed purpose/output types and cannot parse any DSN/S3 variable；seal commands cannot read a private key or network. Disaster `unreachable draft` reconstructs the witnessed mutation, reads only surviving adapters and emits the signature-free body；`unreachable seal` verifies current-governance threshold and is the sole proof producer, while `resume --unreachable-proof` appends/full-witnesses/readbacks it. All three exporters named above require the explicit recovery signing key and emit zero bytes until the derived signer exact-matches the witnessed purpose descriptor. The six candidate encryption/decryption/destruction command scopes parse exactly one local-or-KMS arm shown above；all other scopes reject those flags before stat, and no command infers a key source from whichever file happens to exist. CLI values map exactly once to canonical enums: `planned-migration→planned_migration`, `disaster-recovery→disaster_recovery`, `deletion-ledger→deletion_ledger`, `deletion-witness→deletion_witness`, `recovery-control→recovery_control`, `candidate-control-policy→candidate_control_policy`, `candidate-challenge→candidate_challenge`, `candidate-preparation→candidate_preparation`, `current-unreachable→current_unreachable`, `candidate-attestation→candidate_attestation`; recovery-request purpose values additionally map exactly `cutover-apply→cutover_apply`, `revoke-import→revoke_import` and `revoke-cutover→revoke_cutover`. Every underscore-form CLI input、case variant or alias exits 2 before stat、network access or inherited-FD write；the old single commands `domain candidate challenge`, `domain transfer cutover`, any `prepare --domain-attestation-key`, caller-selected `--receipt`, and every legacy `--output|--output-body|--output-attestation-body|--output-receipt` flag do the same. `--output-root` outside the command's exact allowlisted root, symlink leaf, pre-existing target, caller-chosen relative suffix and FD that is not inherited/writable also exit 2. `domain identity drain` is the only final-drain/continuation producer；`resume --drain-proof` accepts the typed latest live drain proof, while repeatable `resume --receipt-fd` accepts only the next exact typed receipt, including import/cutover revocations, and full-witnesses/readbacks each before advancing. Final purge/workspace-zero receipts can arrive only from the cleanup-verifier process over the inherited FD and are never staged on candidate-control. After durable intent, candidate commands reconstruct policy/preparation/intent and phase prerequisites only from the authenticated recovery-request FD；optional local policy/preparation can only exact-match.

  `domain candidate cleanup verify` is the only parser allowed to accept `--cleanup-verifier-signing-key`. It runs as a separately sandboxed one-shot process with only candidate-control read-control, no current/candidate write/admin credential, no base center/admin/migrator environment and inherited request/receipt FDs. The key loader accepts only a regular/no-follow/bounded strict-0400 raw 32-byte Ed25519 seed or 64-byte private key whose public half matches；the derived descriptor must exact-match the request's published policy and, for post-intent purposes, sealed preparation/intent before any probe/sign operation. Its key file and process root are covered by a live `SignedFilesystemExclusionProofV1`, excluded from candidate-control/transfer/backup/restore surfaces, and never serialized. It signs only the closed purge or workspace-zero wrapper；key-destruction evidence is unsigned nested data. Lost/mismatched/expired key, exclusion-proof drift, deadline expiry or verifier unavailability leaves teardown and admission fail closed. A `policy_prepared` root, or a fully acknowledged `challenge_started` root, may enter the witnessed no-intent abandon protocol. A partially published `challenge_started` root must first reconcile exact primary 2 + witness 3 readback and ack；until then it cannot output challenge bytes、bind intent or reserve abandon. Only `abandoned` permits a new mutation or different descriptors；all nonterminal states retain the same externally provisioned identities, and no renewal/phase advance may substitute them. All other command scopes reject the flag/environment before stat. Reject `--confirm`, candidate-scope mutation approval, mixed authorization, more than one target, profile transition, arbitrary path-based drain/receipt inputs and candidate secrets in center/admin/migrator environments. After intent only exact request-driven resume is legal, and after retirement no old-config rollback path exists. Document the three base environment files plus the separately mounted, phase-replaced candidate file, isolated verifier invocation and final destruction/zero receipts.

- [ ] **Step 8: Verify GREEN across profiles and cutpoints.** Run canonical/DDL/unit tests, then real `postgres_sync` and `s3_worm` fixtures for every logical domain. Cover control-policy draft/current+candidate sign/seal/publish and same-phase renewal/phase-advance prerequisites, challenge/preparation/unreachable draft-sign-seal threshold, governance/attestation/signature-scope type confusion, single-signature bypass, unpinned or unpublished policy, policy→preparation→intent→plan descriptor/deadline/mutation-ID/scope/expiry mismatch, complete intent reconstruction after local artifact plus recovery-control loss, concurrent intents at the same current epoch, concurrent key/policy mutation, stale trust/ledger heads, minimum-fence regression, same-system-ID, same-bucket/different-prefix, backup overlap, missing current full witness, candidate gap/far tail/extra entry, planned dual-write drift, disaster proof substitution/cross-mutation replay, drain drift, drain expiry after first append followed by one or more continuations, 24/20/8-MiB multi-frame exact-max and chunk gap/overlap/digest/offset/count failures, opaque `managed_postgresql_row_v1|managed_s3_object_v1` rejection, exporter wrong-key zero-output, cutover command substitution, candidate import-applied/revoked forgery or out-of-order ingestion, missing/tampered witness primary-receipt copy at every activation/rotation publication cutpoint, S3 hold/retention downgrade, candidate-control IAM/TLS-SPKI/versioning/Object-Lock/lifecycle/replication/backup/encryption drift, raw access-key-ID persistence, local-key wrong mode/length, KMS context/wrapped-DEK substitution and local/KMS parser+descriptor XOR, AEAD wrong-key/AAD/tag, concurrent cross-artifact nonce collision on S3/filesystem/PostgreSQL, nonce-reservation ack loss, final receipt self-storage, nonce-reservation/candidate-receipt/cleanup-verifier three-way signer cross-use, verifier key mode/identity/exclusion-proof/availability failure, post-Final verifier descriptor replacement in primary/PostgreSQL-witness/S3, control-plane zero-inventory ack loss, current-policy snapshot accepted after activation or candidate-policy snapshot accepted before projection, canonical scope-hash golden shared by key builder and all principal policies, wrong-preimage/repeated-scope zero-write, old credential still writable, old domain reappearance, and process death after every policy/receipt/trust/ledger/projection/retirement/final/teardown/completion cutpoint. Assert exact primary set→typed primary receipt→witness set+canonical primary receipt+typed witness receipt(binding primary digest)→five-artifact/2+3 full readback order, prepare→import→cutover→cleanup policy publication/readback before use, current adapter policy through cutover→projection-embedded typed policy activation→candidate policy for later proofs, `DrainContinuation*→CandidateImportApplied→CandidateImportRevoked→Cutover`, and Final→cutover revoke→AEAD+nonce key destruction/control purge→workspace zero→completion(final receipt-chain head+candidate-control policy head/receipts)→complete; each old command/alias/output-leaf/key-arm/phase-forbidden input must fail before stat/network. Spy every lost-domain adapter and require zero calls in its disaster row. Exactly one intent may win; every durable retry converges by mutation ID; unsupported total-loss/profile transition has no override.

The CLI/parser corpus must separately prove generation-1 descriptor paths/deadline are required, renewal/advance rejects deadline or descriptor reinjection before stat, prepare same-phase renewal is expressible, descriptors remain equal across policy→preparation→intent→plan, and descriptor validity covers the immutable ≤30-day mutation deadline. It must prove recovery-request purpose/phase isolation, post-intent success with all local policy/preparation files and recovery-control deleted, abandon-vs-intent CAS exclusivity, abandon without a sealed preparation, and 2+3 abandon completion. Crash tests stop before/after provider revocation, candidate receipt emission, current append, witness confirm and readback; import/cutover receipt keys and bundles remain until the corresponding current acknowledgment and are destroyed only by a later cleanup request. Prepare/import/cutover alone can sign nonce reservations; credential revoke cannot open the nonce key; abandon/cleanup can only identity-check and destroy it with signer calls fixed at 0. Cleanup-verifier sign calls are exactly purge plus workspace-zero, unsigned AEAD/wrapped-DEK/nonce destruction evidence is covered by purge, both remaining-key counts reach zero, and every other scope rejects secret inputs before stat. Final/completion goldens mutate policy-head generation/digest, full verifier descriptor and primary/witness publication receipts independently across Go/primary/PostgreSQL-witness/S3.

The recovery-request matrix is independent of the 24/20/8-MiB transfer-frame matrix. For every one of the six purposes and both PostgreSQL/S3 full-witness profiles, independently remove and single-byte-mutate each of the five artifacts—primary signed core、primary receipt、witness signed-core copy、witness primary-receipt copy and witness receipt—and require zero output-FD bytes. Inject acknowledgment loss immediately before and after each of primary insert、primary readback、witness confirm、witness readback、primary acknowledgment and transport write；retry must reuse byte-identical signed core and both receipt bytes, prove the complete genesis-through-immutable-far-tail chain, and never re-sign time fields under the same tuple. Exact 32 MiB signed core、each exact 64 KiB receipt and exact 33 MiB FD wrapper must succeed；each +1 case must fail before allocation across the Go exporter/decoder、recovery-control SQL、PostgreSQL witness、S3 and inherited-FD decoder. Parser tests enumerate the three canonical hyphenated purpose inputs plus underscore forms、case variants and aliases；every invalid spelling must record stat count 0、network count 0 and FD-write count 0 with exit 2.

```bash
go test ./internal/center/platformadmin ./internal/center/recovery \
  ./internal/center/deletionledger ./internal/center/platformmigrate \
  ./internal/center/store/migrate ./internal/center/store \
  ./cmd/houfeng-record-platform-admin \
  -run 'DomainIdentity|Rotation|Candidate|Copy|Cutover|Retirement|Disaster' -count=1

scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/platformadmin ./internal/center/recovery \
  ./internal/center/deletionledger ./internal/center/platformmigrate \
  ./internal/center/store/migrate -run 'PostgresIntegration.*DomainRotation' -count=1

scripts/test-record-platform-integration.sh postgres-s3 -- \
  go test -v ./internal/center/platformadmin ./internal/center/recovery \
  ./internal/center/deletionledger ./internal/center/platformmigrate \
  ./internal/center/store/migrate -run 'S3WORMIntegration.*DomainRotation' -count=1
```

## Task 18: Full contract verification and handoff

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/center/recordplatform/foundation_acceptance.go`
- Create: `internal/center/recordplatform/foundation_acceptance_test.go`
- Create: `internal/center/recordplatform/testdata/foundation_acceptance_registry_v1.json`
- Create: `cmd/houfeng-record-platform-acceptance/main.go`
- Create: `cmd/houfeng-record-platform-acceptance/main_test.go`
- Create: `scripts/verify-record-platform-foundation-acceptance.sh`
- Modify: `.trellis/spec/backend/database-guidelines.md`
- Modify: `.trellis/spec/backend/directory-structure.md`
- Modify: `.trellis/spec/backend/error-handling.md`
- Modify: `.trellis/spec/backend/logging-guidelines.md`
- Modify: `.trellis/spec/backend/quality-guidelines.md`
- Modify: `.trellis/tasks/07-14-vps-records-platform-foundation/{prd.md,design.md,implement.md}` only if verification exposes a contract correction

- [ ] **Step 1: Run fresh focused tests.** Run the exact package set below with `-count=1`:

```bash
  go test ./internal/center/recordauth ./internal/center/recordplatform \
  ./internal/center/retention \
  ./internal/center/deletionledger ./internal/center/recovery \
  ./internal/center/deletionledger/migrate ./internal/center/recovery/migrate \
  ./internal/center/platformadmin ./internal/center/platformmigrate \
  ./internal/center/store/migrate ./internal/center/store \
  ./internal/center/http ./internal/center/config ./internal/center/deploy \
  ./cmd/houfeng-center ./cmd/houfeng-record-platform-admin \
  ./cmd/houfeng-record-platform-acceptance \
  ./cmd/houfeng-retention-scanner ./cmd/houfeng-retention-acceptance-signer -count=1
```

- [ ] **Step 2: Run race gates.** Run all five commands and retain their exit codes:

```bash
go test -race ./internal/center/recordplatform ./internal/center/store \
  -run 'RecordPlatform|Membership|Admission|Lease|Outbox|Retention' -count=10
go test -race ./internal/center/deletionledger \
  -run 'Canonical|Primary|Witness|Reconcile|Checkpoint|Activation|Rotation' -count=10
go test -race ./internal/center/recovery ./internal/center/platformadmin \
  -run 'Trust|Inventory|Manifest|Runtime|Activation|Saga|Approval|Lifecycle|Drain|DomainIdentity|Rotation|Candidate|Copy|Cutover|Retirement|Disaster|Profile|Retry' -count=10
go test -race ./internal/center/config ./internal/center/deploy ./cmd/houfeng-center \
  ./cmd/houfeng-record-platform-admin -run 'RecordPlatform|Bootstrap|Deployment|CLI|CredentialBoundary|ScopedApproval|DomainIdentity|Rotation|Candidate' -count=3
go test -race ./internal/center/retention ./cmd/houfeng-retention-scanner \
  ./cmd/houfeng-retention-acceptance-signer \
  -run 'Retention|Acceptance|SourceClaim|MergeTree|Metadata|Scanner|Signer|Replay|OwnerMatrix' -count=10
```

- [ ] **Step 3: Run real integrations.** Run all three commands; fixture wrappers must report zero skipped tests and zero surviving containers:

```bash
scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store/migrate ./internal/center/deletionledger \
  ./internal/center/recovery ./internal/center/platformadmin ./internal/center/platformmigrate \
  -run 'PostgresIntegration' -count=1
scripts/test-record-platform-integration.sh postgres-s3 -- \
  go test -v ./internal/center/store/migrate ./internal/center/deletionledger \
  ./internal/center/recovery ./internal/center/platformadmin ./internal/center/platformmigrate \
  -run 'S3WORMIntegration' -count=1
scripts/test-record-platform-upgrade.sh
sh scripts/verify-record-retention-acceptance.test.sh
```

The retention script builds a hermetic temporary Git repository with real blobs/trees/commits, pinned trusted-base workflow blobs and fake Git-host check-run responses for all 11 ordinals. It executes claim → two-parent feature merge → protected rescan → isolated sign → exact-one-file metadata merge → next-child base for merge order `1,2,3,4,9,5,6,7,8,10,11`, then proves Task 11 sees checked-in acceptances 1–10 plus only its own claim and that acceptance 11 is created only after its feature merge. Independent fixtures reject wrong base/tree/parent order, wrong repository/ref/child/ordinal, non-highest or wrong-issuer check attempt, wrong workflow/action-set/scanner/rules/signer/key digest, signature replay, previous gap/fork, add removal/replacement, base-result drift, nondeterministic acceptance time, metadata path/body mismatch and an extra metadata-PR file.

The PostgreSQL suite includes fresh/repeat/checksum/unknown migration cases, out-of-filename-order 0056→0055 ACL revisions, ack-loss cutpoints, runtime-role denial, `postgres_sync` four-database isolation, `s3_worm` three-database inactive-witness non-read evidence, activation incomplete/complete, lifecycle, recovery-from-witness and planned/disaster rotation for every logical domain. Domain tests include system-ID alias, stable attestation wrong-kind/expiry/generation rollback, S3 backup-location overlap, witnessed identity-set rollback, candidate gap/far-tail, dual-write drift, current-unreachable proof substitution, old-domain reappearance and candidate-preparer teardown. The S3 suite uses distinct runtime/admin/migrator/candidate principals and includes every bidirectional cross-write, overwrite, delete/version-delete, hold-removal, retention-reduction denial, migrator no-body-read/no-write, candidate no-current-domain access, far tail and primary-loss recovery. Upgrade receipts are exactly `legacy-0050`, `current-flags-off`, `flags-off-old-rollback`, `current-records-only`, `pre-activation-old-rollback`, `activation-retry` and `post-activation-old-fenced`; rotation tests additionally retain per-mutation copy/cutover/retirement/final-proof receipts.

- [ ] **Step 4: Run repository gates.** Run every command below. The first lint command requires zero findings in new packages; the second requires zero new findings relative to the branch base even if the documented repository baseline remains nonzero.

```bash
go test ./... -count=1
make verify-go
make build-record-platform-admin
go vet ./...
go mod tidy -diff
go mod verify
git diff --check
golangci-lint run ./internal/center/recordauth/... \
  ./internal/center/recordplatform/... ./internal/center/deletionledger/... \
  ./internal/center/retention/... \
  ./internal/center/recovery/... ./internal/center/platformadmin/... \
  ./internal/center/platformmigrate/... \
  ./cmd/houfeng-record-platform-admin/... ./cmd/houfeng-retention-scanner/... \
  ./cmd/houfeng-retention-acceptance-signer/...
golangci-lint run --new-from-rev "$(git merge-base HEAD origin/main)" ./...
docker compose config
docker compose --profile record-platform config
docker build --network=host -t houfeng-record-platform:test .
systemd-analyze verify docs/deploy/systemd/houfeng-center.service
```

- [ ] **Step 5: Perform direct spec and data-flow review.** Trace API/session → typed visibility/source policy/filter, queue/lease → membership, scoped migrator → sorted migration/checksum set → monotonic ACL manifest → exact catalog privileges, migrator → stable identity/liveness attestation → planner identity set, CLI drain → plan → scoped approval → retry/continuation → saga → trust/witness → ledger/witness → projection → readiness, rotation candidate prepare → genesis copy → planned dual-write/disaster proof → drain/cutover → domain-rotation trust+ledger entries → projection/retirement/final-proof witness, and janitor → field-level allowlist. Directly trace the production retention path `ChildRetentionSourceClaimV1 → two-parent feature merge → trusted-base scan/check observation → no-network sign → exact-one-file metadata PR → merged acceptance as next-child base` through actual scanner/signer/workflow/verifier symbols; count-only worker/tool assertions or fixture seams do not qualify. Create one evidence row for each exact `PF-AC-001..019` registry line with the stable selector, normalized requirement-text SHA-256, production symbol, automatic non-mock evidence and every cross-layer participant; no row may widen into a criterion range. Run `scripts/verify-record-platform-foundation-acceptance.sh --verify-registry`, then run all 19 selectors into `artifacts/acceptance`; reject missing, duplicate, reordered selector, stale-text hash, test-only symbol, mock-only receipt or absent production caller. Tests alone do not waive a missing production caller.
- [ ] **Step 6: Inspect the exact git set.** Exclude `.tmp/`, `node_modules/`, staging secrets and all sibling task directories. Only after the control review approves the diff may the feature branch be staged and committed; never modify or push local/remote main/master directly.

## Rollback

- 0051 和独立 schema 只增加对象；feature 默认 off，回滚 binary 不执行 down migration。
- activation plan 写入 durable mutation 前可直接过期重建；写入后只能按相同 mutation/digest 继续，不能删除或重做 revision 1。
- trust/ledger 任一 full witness 已确认后禁止 down migration、覆盖、回滚或重新 bootstrap；失败状态保持 capability unavailable，先修复并续跑 saga。
- contract activation 前可清空未使用的独立测试 DB；activation 后 ledger/witness 与 minimum version 不可回退。
- domain rotation 在 durable intent 前，`policy_prepared` 或完成 exact primary 2 + witness 3 readback/ack 的 `challenge_started` 只能通过 no-intent fence → witnessed abandon authorization → candidate zero/purge/workspace-zero → witnessed abandon completion 前进到 `abandoned`；部分发布的 `challenge_started` 必须先协调完成，任何未见证的清理材料都不能授权回滚。只有 `abandoned` 才允许新 mutation/descriptors。intent 后只能以相同 `mutation_id`、plan、authorization 和 candidate exact-resume，禁止换 candidate、改 profile 或新建替代 mutation。
- rotation ledger/full witness 确认或旧域 retirement 任一发生后没有旧配置回滚；projection 前失败保持旧域 current-authoritative 且 capability unavailable，projection 后失败保持 candidate-authoritative 并向前完成 retirement/final proof。旧域重现或仍可写立即关闭 admission。
- 任一真实 witness/trust/recovery test 失败都回到本任务修复，不能留给子任务 11 才发现。
