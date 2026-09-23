# APP ACL R2 冻结 finalizer 合同

## Scenario: APP ACL R2 相关协议 direct finalizer, M2 evidence, and ACL provenance

### 1. Scope / Trigger

- Trigger：修改隔离的 `db/appaclr2/migrations/0052_app_acl_r2_privileged_transition.sql` 的 finalize section、`internal/center/store/migrate/app_acl_r2_finalize.go`、M2 catalog verifier、或 `houfeng-record-platform-admin finalize --scope app-acl-r2` 时。
- 此场景只覆盖 相关协议 从 exact `PREPARED` 到 exact `FINALIZED` 的 direct-migrator writer。它新增的持久化面仅为 `public.app_acl_r2_manifest_revisions`、`public.app_acl_r2_manifest_head` 及其 immutable trigger helper；冻结 M1 relation、R1 writer 和 runtime route 都不是本场景的写入目标。
- 不把此场景的源码/单元验证当作 PostgreSQL 16 integration evidence，也不把它延伸为 相关协议、R2 总验收、physical clone/restore detection 或 相关集成/PF-AC 的证据。

### 2. Signatures

- Admin route：`houfeng-record-platform-admin finalize --scope app-acl-r2`；唯一允许的 credential input 是 `HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL`。
- Go entry：`FinalizeAppACLR2(ctx context.Context, db *pgxpool.Pool) error`。它在保留的 direct-migrator connection 上开启 `SERIALIZABLE READ WRITE` transaction，并复用 `ClassifyAppACLR2State(ctx, tx)` 和 `ReadAppACLR2CatalogPredicatesInTx(ctx, tx)`。
- M2 SQL identity：`public.app_acl_r2_manifest_revisions`、`public.app_acl_r2_manifest_head`、`record_platform_internal.app_acl_r2_reject_manifest_mutation()`；唯一 revision/head 是 `(protocol_version, manifest_revision) = (2, 2)` 与 singleton head。

### 3. Contracts

- Finalizer 先固定 `search_path`、验证 direct constrained migrator 的 `session_user == current_user` 与冻结 M1 migrator binding、按固定顺序加锁，再只经共享 constrained catalog path 接受 exact `PREPARED`。它不创建 private classifier/predicate/snapshot，也不调用 `pg_control_system()`。
- **Persisted evidence is not fresh physical identity**：bootstrap 在初始 PREPARED commit 前把 live `postgres_system_identifier` 绑定进 immutable receipt/domain。finalizer 只比较该 persisted receipt/domain（以及 M2-domain）并新鲜读取 database OID/name 与 catalog continuity；它既不重读也不声称证明 fresh physical system identifier。
- Finalizer 执行 marker-bounded M2 DDL，写入 immutable M1 links、53-source/206-tuple/domain/receipt/control-ACL digest bodies，以 M1 revision/digest 和 empty-head CAS 建立一 revision/one true head，revoke-first 收敛 ACL，随后以同一共享 path 回读 exact `FINALIZED` 才 commit。`40001`/`40P01` 只能重试整个 closure；不确定 commit acknowledgement 只能在同一总 attempt budget 内重新分类 exact `FINALIZED` 或 `PREPARED`。
- **Owner identity is distinct from explicit ACL and effective privilege**：direct migrator 仍拥有两张 M2 table 和 helper，但 owner OID 不能替代 ACL/effective evidence。revoke-first DCL 必须写入 owner 的 non-grantable self `SELECT`/`EXECUTE` row；effective table vector 对 owner/runtime 都只能是 `SELECT=true`，其余六项为 false，admin 七项全 false；helper 仅 owner `EXECUTE=true`。
- **Raw ACL provenance and effective reachability must both be exact**：`aclexplode` 精确要求每张 M2 table 有 direct-migrator 与 center-runtime 的 no-grant-option `SELECT`，helper 只有 direct-migrator no-grant-option `EXECUTE`；`has_table_privilege`/`has_function_privilege` 独立探测上述完整向量。删除 owner self row 后 raw 与对应 effective 结果都为 false，Exact M2 必须拒绝。

### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| route/scope/DSN 不精确，或存在 ambient `PG*`、service/pass/TLS-file source | 在 pool 创建前拒绝；不回退通用 opener。 |
| 非 direct constrained migrator、R1/CORRUPT/nonexact PREPARED、receipt/M1/source/domain continuity drift | mutation 前 fail closed；不得创建 M2。 |
| M2 section body/marker/cardinality/digest 不精确，或 M1/head CAS 不是一行 | 拒绝；整个 transaction rollback 到 PREPARED。 |
| DDL/DCL/readback 的 `40001` 或 `40P01` | 仅在总 attempt budget 内重跑整个 serializable closure；其他错误或耗尽立即停止。 |
| 缺少/撤销 owner self `SELECT` 或 helper self `EXECUTE` row | raw `aclexplode` 与对应 effective probe 都为 false；不得作为 FINALIZED 或 ACK-recovery success。 |
| owner/runtime 获得非 `SELECT` table privilege、admin 获得任何 queried M2 privilege，或 owner 缺少 `SELECT`/helper `EXECUTE` | effective privilege drift，拒绝。 |
| 需要新的 physical-system 读或 PG16 live integration conclusion | 不属于 相关协议 source contract；不得以 persisted evidence 或 unit test 代替。 |

### 5. Good / Base / Bad Cases

- Good：精确 PREPARED、direct migrator direct login、完整 shared continuity 和 canonical source section 在一个 transaction 中产生两个 direct-owned M2 relations、one revision/head、精确 ACL，readback 为 FINALIZED。
- Base：已 exact FINALIZED 的 direct-migrator repeat 仅做受约束的分类/验证，不重放 M2 DDL/DML；uncertain ACK 只接受 exact FINALIZED，或在剩余 budget 内从 exact PREPARED 整体重试。
- Bad：把 receipt 中保存的 system identifier 当成 finalizer 可重新读取的 fresh physical identity；这越过 bootstrap-only boundary。
- Bad：只检查 owner OID、raw ACL 或 effective privilege 中任一项，因此把缺行、扩权或 grant-option 漂移当作健康；三类证据必须独立核验。

### 6. Tests Required

- 相关协议 focused unit/source gate：

  ```bash
  go test ./db/appaclr2/migrations ./cmd/houfeng-record-platform-admin ./internal/center/store/migrate \
    -run 'AppACLR2(Source|Finalize|Manifest|M2)|VerifyAppACLR2M2' -count=1
  ```

  断言 strict finalizer DSN/opener、reserved connection + lock/transaction lifecycle、whole-closure retry/ACK bounds、M2 DDL shape/CAS/rollback、owner self-ACL inversion，以及 raw ACL/effective privilege split。
- 同一三个 package 的 full `go test -count=1`、`go vet`、受影响 Go 文件的 `gofmt -d` 与 `git diff --check HEAD` 是本地 source gate。
- 真 PostgreSQL 16 authority matrix、OID-10/bootstrap/live-system trace 和 image lanes 仍由 相关协议 integration tests 负责；未运行该 lane 时不得写成 PG16 evidence。

### 7. Wrong vs Correct

```go
// 错误：post-bootstrap writer 重新读取 physical system identity。
systemID := readPGControlSystem(ctx, tx)

// 正确：只比较 persisted receipt/domain 与 fresh database OID/name/catalog facts。
state, err := ClassifyAppACLR2State(ctx, tx)
```

```sql
-- 错误：用 owner 或 has_table_privilege 取代 raw grant proof。
select has_table_privilege(:owner_oid, relation_oid, 'SELECT');

-- 正确：先精确检查 aclexplode 的 owner/runtime rows，再独立检查完整 effective vector。
select grantor, grantee, privilege_type, is_grantable
from pg_catalog.aclexplode(relacl);
```

---
