# 严格 PostgreSQL 验证合同

## Scenario: APP ACL 严格 PostgreSQL 16 catalog lane

### 1. Scope / Trigger

- Trigger：修改
  `internal/center/store/migrate/app_acl_r2_postgres_integration_test.go`、
  `internal/center/store/migrate/app_acl_current_postgres_integration_test.go`、
  `scripts/test-record-platform-integration.sh` 的 `pg16-catalog` mode，或
  `.github/workflows/ci.yml` 的 `record-platform-pg16-catalog` job 时。
- 本场景的两个 integration 文件沿用 `*_postgres_integration_test.go` 命名。该
  required-CI catalog lane 不替代其他 integration tests，也不替代通用集成的
  record-platform fixture modes。
- 它同时覆盖冻结 APP ACL R2 authority/catalog evidence 与 current-build
  migration/admission evidence，不改变冻结的 R1/R2 source、tuple、ACL、data、
  permission、state 或 clone/restore 合同。
- 这是 cross-layer runner、Actions job、GitHub ruleset required-check 和
  `go test -json` evidence contract；实现时四个边界必须一起验证，不能把
  package compilation 或 zero-test result 标成 PG16 evidence。

### 2. Signatures

```bash
HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=<exact-image> \
  scripts/test-record-platform-integration.sh pg16-catalog -- <command> [args...]
```

- `pg16-catalog` 是本场景的 严格 catalog strict mode。`<exact-image>` 只能是
  `postgres:16.0`、`postgres:16.6` 或 `postgres:16.12`。
- `postgres` 保持通用通用集成合同：
  `scripts/test-record-platform-integration.sh postgres -- <command> [args...]`。
  严格 catalog image variable 不对该 mode 施加 validation 或 defaulting。
- required workflow job id 是 `record-platform-pg16-catalog`；显式 job name
  是 `record-platform-pg16-catalog (${{ matrix.postgres_image }})`。literal
  matrix 必须产生以下精确 check contexts：
  `record-platform-pg16-catalog (postgres:16.0)`,
  `record-platform-pg16-catalog (postgres:16.6)`，和
  `record-platform-pg16-catalog (postgres:16.12)`.
- job 的 fresh-runner signature 是 `runs-on: ubuntu-latest`、
  `actions/checkout@v6`、`actions/setup-go@v6` + `go-version-file: go.mod`，
  随后每一个 matrix lane 执行同一个 strict command：

  ```bash
  scripts/test-record-platform-integration.sh pg16-catalog -- \
    go test -json ./internal/center/store/migrate \
    -run '^(TestPostgresIntegrationAppACLR2|TestPostgresIntegrationAppACLCurrent)$' -count=1
  ```

  `TestPostgresIntegrationAppACLR2` 与
  `TestPostgresIntegrationAppACLCurrent` 是两个必需的 top-level PG16
  anchor；subtest 名可附在各自 anchor 后。`platformmigrate` 与
  record-platform admin CLI 不属于这个 command。
### 3. Contracts

- runner 必须在 `mktemp`、random material、port probing、Docker、container、
  fixture URL 或 child execution 之前验证 mode、`--`、child argv 和严格 image
  值。每个被接受的 strict lane 都把所选 image 用于所有
  APP/ledger/witness/recovery fixture databases。
- `postgres` 的名称和未来通用集成调用是 compatibility boundary。加入或修改
  `pg16-catalog` 时不得 rename、delete、route through strict allowlist 或以其他
  方式改变该通用 mode。
- current APP successor 变更必须保留一个完全离线、测试运行时不读取 git/network
  的 released-predecessor oracle。当前 oracle 固定 v0.79.4 的63-entry migration
  canonical body、privilege body和revision-1 manifest digest；产品transition compiler
  必须逐byte匹配后才可打开transaction，不能和测试共同从fixed current set动态截
  prefix自证。symbolic tag只用于生成前的人工trust-root核验，不属于测试运行时输入。
- successor单元证据必须按TDD覆盖registry定义错误（missing/duplicate/unknown/
  out-of-order/overlap/privilege drift）、writer insert/head CAS/RowsAffected/readback、
  manifest shape与runtime admission，以及每个transaction cutpoint。transaction前
  definition/golden错误断言opener零调用；transaction内任一步错误断言rollback、
  commit零调用和later seam零调用；serialization failure必须重跑完整closure并重读
  ledger/head/settings，而不是从中间步骤续跑。
- strict PostgreSQL successor证据必须用两个隔离singleton settings fixture分别覆盖
  global 3与global 20；两者都含override 3和heartbeat rows。断言0063的3→12、20保留、
  override保留、exact covering index、ledger/revision chain/runtime admission与repeat
  durable snapshot深相等。错误同名index、错误released default与partial-0063必须在
  revision-2发布前fail closed。任何 `SKIP` 都不是证据。
- Compose caller证据必须从独立released predecessor materialize exact 0062/revision-1
  后调用生产init装配，而不是直接调用任意依赖注入helper或success fake；允许仅以
  PostgreSQL transport opener为测试seam，但其余依赖必须由与公开入口相同的production
  factory装配。released fixture必须由build-tagged test-only source直接前向收敛并逐字节
  验证冻结golden，不能先运行fixed current再反向删除0063。验证role password、authority
  state、heartbeat rows/index、Records/attachment readback、0063语义和repeat。focused PG/Compose
  GREEN、affected package与cutpoint GREEN、`go vet`和`git diff --check`相互独立记录，
  不得用其中一项替代另一项或替代full `make verify-go`。
- Integration test 直接执行且未设置 `HOUFENG_POSTGRES_INTEGRATION=1` 时可按
  普通 broad-verify 规则 `t.Skip`。strict runner export 该变量；child output
  中任意 `--- SKIP:` 都必须使 runner exit 1，enabled-test prerequisite failure
  必须使用 `t.Fatal`/error 而非 skip。其他 database/package spec 指定的
  real-PostgreSQL acceptance test 也必须遵守相同的 strict RUN/PASS 合同。
- workflow matrix 只能包含三个 quoted literal，不得有 `include`、default
  expression 或不同 entry point；每个 lane 运行同一 `pg16-catalog` command。
  job 必须先 checkout 和按仓库既有 `setup-go@v6`/`go.mod` 模式安装 Go；显式
  job name 是三个 check contexts 的 evidence contract；workflow 文件本身不能
  把 context 设为 required。
- strict child 只运行 migrate package 的 anchored JSON selector。它必须将
  stdout 保存为 JSONL，并以 `jq -se` 分别证明匹配
  `^TestPostgresIntegrationAppACLR2($|/)` 与
  `^TestPostgresIntegrationAppACLCurrent($|/)` 的 `run` 和 `pass` event
  各至少一条，且该 package 的 `skip` 和 `fail` event 均为零。runner 的 child exit 和
  `--- SKIP:` failure 保持有效；event proof 额外拒绝 zero-test false green。
  runner 必须保持 child stdout/stderr 为两个独立的外部 stream：两者可分别
  tee 到内部诊断文件并共同参与 `--- SKIP:` 检查，但必须等两个 tee 完成后再
  扫描，且不得在 workflow 收集 JSONL 前把 stderr 合并进 stdout。fresh runner
  的 `go: downloading ...` 等 module/tool diagnostics 继续在 stderr 可见，但
  绝不能进入 event 文件或让相同提交因 cache 冷热而得到不同的 JSON 解析结果。
  `internal/center/platformmigrate` 与
  `cmd/houfeng-record-platform-admin` 的回归只能走独立非 PG16
  `go`/`make verify-go` full-test gate。
### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| `pg16-catalog` 缺少 image，或 image 为 `postgres`、`postgres:16`、`postgres:16-alpine`、其他非 allowlist 值 | 在任何 Docker 或 fixture side effect 前 exit 2。 |
| `pg16-catalog` 使用一个 allowlist image | 用该 exact image 启动四个 fixture database 并执行 child command。 |
| 调用 `postgres` | 保持通用 mode contract；不能仅因为 strict image variable 缺少或不同而拒绝。 |
| strict child 输出 `--- SKIP:` | cleanup 后 runner nonzero exit；该 lane 不是 evidence。 |
| strict child 在 stderr 输出 module/tool diagnostics，同时在 stdout 输出 canonical Go JSON events | diagnostics 保持在 stderr 且 event JSONL 可完整解析；任一 stream 的 `--- SKIP:` 仍使 runner nonzero exit。 |
| fresh job 缺少 `runs-on`、checkout 或按 `go.mod` 的 setup-go，或 lane 运行不同 child command | workflow review 拒绝；不能声称 fresh Actions runner 可执行。 |
| matrix 添加 `include`、第四个值、shell/default fallback 或不同 entry point | workflow/runner contract review 必须拒绝；CI matrix 不再 deterministic。 |
| anchored JSON stream 没有 matching `run`/`pass`，或 package 有 `skip`/`fail` event | lane nonzero exit；zero-test、skip 或 fail 不是 PG16 evidence。 |
| strict PG16 command 包含 `platformmigrate` 或 admin CLI | review 拒绝；该 result 会混入非 严格 catalog file ownership 的 package gate。 |

### 5. Good / Base / Bad Cases

- Good：用 `pg16-catalog` 提供 `postgres:16.6`；四个 fixture database 都使用
  该 literal image；fresh job checkout/setup Go 后，migrate anchor 产生
  nonzero `run`/`pass` 和 zero `skip`/`fail` JSON events，check context 为
  `record-platform-pg16-catalog (postgres:16.6)`。
- Base：开发者直接运行且没有 fixture environment 时，批准 integration test
  按普通规则 skip；它不是 required-CI result。
- Bad：改变 `postgres` 以拒绝 `postgres:16-alpine`，从而破坏通用集成
  既有集成 commands。
### 6. Tests Required

- 两个批准 integration file 覆盖 real PG16 authority/current matrix；由 strict runner 执行
  时不得以 skip 作为 evidence。runner coverage 必须证明三个 allowlist literal、
  missing/invalid input 在 side effect 前拒绝、selected image 传到四个 fixtures、
  cleanup 与 skip-to-failure behavior。它还必须用 canonical JSON stdout 加
  `go: downloading` stderr fixture 证明两个外部 stream 不混合，并分别证明
  stdout/stderr 中的 skip marker 都 fail closed。两个文件必须分别声明
  `TestPostgresIntegrationAppACLR2` 与 `TestPostgresIntegrationAppACLCurrent`
  top-level anchor；CI/local selector 必须同时锚定这两个名字。
- 三个 image 都必须在本地运行同一 command；可用 Docker Server 是 local
  evidence，不是把 lane 推给 CI 的理由。随后每个 CI matrix lane 也运行完全相同的
  strict entry point。每次运行把 `go test -json` output 保存为 JSONL，并执行：

  ```bash
  # events is the file populated by tee from the strict child command above.
  jq -se '
    def package_event:
      .Package == "houfeng/internal/center/store/migrate";
    [.[] | select(package_event)] as $package_events
    | ["TestPostgresIntegrationAppACLR2", "TestPostgresIntegrationAppACLCurrent"] as $anchors
    | (all($anchors[];
        . as $anchor
        | [$package_events[] | select(((.Test // "") | test("^" + $anchor + "($|/)")))] as $anchor_events
        | (($anchor_events | map(select(.Action == "run")) | length) > 0)
          and (($anchor_events | map(select(.Action == "pass")) | length) > 0)
      ))
      and (($package_events | map(select(.Action == "skip")) | length) == 0)
      and (($package_events | map(select(.Action == "fail")) | length) == 0)
  ' "$events" >/dev/null
  ```

  Pipeline/runner nonzero exit plus this query proves nonzero execution, zero
  skip and zero fail. `platformmigrate` 与 admin CLI 的必要回归在独立
  `go`/`make verify-go` full-test gate 执行；其 green result 不得成为 PG16
  catalog assertion。
### 7. Wrong vs Correct

```bash
# Wrong：劫持 parent mode 并接受 fallback image。
scripts/test-record-platform-integration.sh postgres -- go test ./...
# HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE defaults to postgres:16-alpine
```

```bash
# Correct：隔离 strict lane 并提供一个 literal image。
HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=postgres:16.12 \
  scripts/test-record-platform-integration.sh pg16-catalog -- \
  go test -json ./internal/center/store/migrate \
  -run '^(TestPostgresIntegrationAppACLR2|TestPostgresIntegrationAppACLCurrent)$' -count=1
```

```yaml
# Wrong：未固定的 include/default 能创建第四个 context 或 fallback。
matrix:
  include:
    - postgres_image: postgres:16

# Correct：精确三个 literal value 驱动 named checks。
matrix:
  postgres_image: ["postgres:16.0", "postgres:16.6", "postgres:16.12"]
```


分支保护属于外部治理状态，不能由 workflow 文件声明其已经 required。修改前必须重新读取实际 checks、rulesets 与 branch protection；本地验证不授权创建或改写远端规则，也不得覆盖既有保护。
