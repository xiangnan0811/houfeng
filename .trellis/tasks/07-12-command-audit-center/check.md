# 全局命令审计中心质量证据

## 结论

本地实现与质量门已通过，未发现阻止提交/创建 PR 的问题。证据只能降低风险，不能解释为“零风险证明”。真实 staging 和 PR required CI 仍分别保持未执行/待执行，不能由本地测试替代。

## 1. Unit / fake / static gates

- `make verify-go`：通过；Go 全包测试、vet/静态门均为 green。
- `NODE_ENV=test ... make verify-web`：通过。
  - Vitest coverage：124 files / 861 tests passed。
  - Coverage：statements 79.81%、branches 71.21%、functions 79.19%、lines 83.42%。
  - `observabilityApi.ts`：100% statements、90% branches、100% functions/lines。
  - strict TypeScript + Vite production build：通过。
  - bundle：entry JS gzip 110736/110736；max async JS gzip 31898/32052；未提高预算。
  - CSS：26 files、311047 source bytes、2107 rules、8517 declarations、151 repeated selectors、293254 production raw bytes、38108 gzip bytes；预算通过且 source/raw/gzip 上限向下收紧。
- 聚焦 Web 回归：Command Audit、API façade、router、Sidebar、Events、Monitoring detail、Target detail 共 159 tests 通过；后续完整 coverage 再次覆盖同一行为。
- `git diff --check`：通过。
- 人工 allowlist/ownership 审阅：
  - production Go 中 `monitoring_instance_command_action_audit` INSERT 只有 `insertCommandActionAudit`；其余直接 INSERT 均为 migration/integration/fake test。
  - `commandaudits.Action/Event`、HTTP response DTO、Web `CommandAudit*` types 均不存在 `details`、stdout、stderr。
  - Web table/timeline 只读取显式字段；hostile runtime 附加字段没有遍历或 JSON dump 路径。
  - router 通过既有 session/same-origin protector 注册；未引入未批准的角色判断。

## 2. Real PostgreSQL

本地一次性 PostgreSQL 16 容器提供临时数据库；每个测试创建并清理自己的 database/schema。

命令：

```bash
HOUFENG_POSTGRES_INTEGRATION=1 HOUFENG_DATABASE_URL='<local-postgres>' \
  go test ./internal/center/store/migrate ./internal/center/store ./internal/center/http/handlers -count=1
```

结果：

```text
ok houfeng/internal/center/store/migrate 1.569s
ok houfeng/internal/center/store 3.883s
ok houfeng/internal/center/http/handlers 0.728s
```

真实数据库断言覆盖：

- fresh install + 第二次 Apply；0046 audit schema 到 0050 的升级与 0050 SQL 重复应用。
- 旧记录 snapshot 回填；升级后旧式 queued INSERT 仍成功。
- action/rejected/source/reason/output constraints；删除 user/MonitoringInstance 后 audit 与稳定身份仍保留。
- snapshot helper 对真实/伪造 actor、缺失实例、queued/rejected 写入的 exact-row 完整性。
- 五种 outcome、字面 `%`/`_` actor/instance filter、action/command/sensitivity filter、同时间 keyset、事件顺序、cursor handler、永久清理后全局读取。
- 应用层代表性一页固定两次 SQL 查询；无 per-action SQL round trip。

最新 `EXPLAIN (ANALYZE, BUFFERS)`：

```text
default-window: execution=1.187ms, limit_rows=21, global_index_rows=360
outcome-failed: execution=1.061ms, limit_rows=21, global_index_rows=360
```

两条计划均使用 `idx_monitoring_instance_command_action_audit_global_time`，未出现 audit 全表 sequential scan；阈值为 execution <= 500ms、候选 <= 400、limit rows <= 21。

## 3. Fixture Chromium

命令：

```bash
npm --prefix web run test:e2e
```

结果：64/64 passed（Chromium，production preview）。覆盖：

- 10 条核心路由 × 1440×1000 / 1024×768 / 390×900 = 30/30。
- `/command-audit` exact default fixture、undeclared request fail-closed、hostile stdout/stderr/details 不进入 DOM。
- named focusable local table scroll、事件展开、advanced Modal Tab/Escape/focus restore。
- Command Audit settled axe serious/critical = 0；统一 console/page/network/CSP diagnostics clean。

该层只证明 fixture 数据下的 production frontend 行为，不证明真实后端、真实认证部署或生产数据正确。

## 4. Real deployment / staging

- 未执行：当前任务没有可用的 `HOUFENG_STAGING_BASE_URL` 与受保护 staging credentials。
- staging smoke 源码已加入 `/command-audit` metadata-only/无 output surface 断言，并由 source/type/build gate 覆盖；这不等同于真实部署 smoke。

## 5. PR required CI

- 待执行：提交、push、创建非 draft PR 后监控 required checks。
- required CI 全绿前任务不能宣称交付完成；CI 不替代上述 PostgreSQL/Chromium 证据。

## 6. Residual boundaries

- 审计永久增长；本期无导出、自动保留期、摘要表或合规删除。
- 当前只有 admin 一种真实角色；未来第二种角色、多人写入或合规删除出现时，由 GitHub #381 重新评估 command authorization、audit read access、容量和隐私。
- base64url cursor 是 opaque transport，不是授权或防篡改边界；所有 decode 值仍校验，访问控制仍由 session/same-origin 与未来 RBAC 承担。
