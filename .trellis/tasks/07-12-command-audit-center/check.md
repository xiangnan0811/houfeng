# 全局命令审计中心质量证据

## 结论

对最初 3 个本地实现提交完成了全分支静态审查、修复、真实 PostgreSQL、完整 Go/Web/Chromium、race detector 和本地视觉复核；截至本次审查提交前，未发现仍需继续修改或优化的本地实现问题。该结论不是“绝对完美”或“零风险证明”：真实 staging 与 PR required CI 未执行，且用户明确禁止 push，因此任务保持 `in_progress`，不归档、不创建 PR。

## 1. Unit / fake / static gates

- `make verify-go`：通过；Go format、vet 与全包 tests 均为 green（tests 同时编译所有 package）。
- `go test -race ./internal/center/store/migrate ./internal/center/store ./internal/center/http/handlers -count=1`：三包通过，无 race 报告。
- 聚焦 Web：7 files / 78 tests passed，覆盖 Command Audit controller/filter/table、API façade、router 与 Sidebar。
- `make verify-web`：通过。
  - `npm ci`：304 packages，0 vulnerabilities。
  - ESLint：通过。
  - Vitest coverage：124 files / 865 tests passed。
  - Coverage：statements 79.85%、branches 71.27%、functions 79.23%、lines 83.45%。
  - `observabilityApi.ts`：100% statements、90% branches、100% functions/lines。
  - strict TypeScript + Vite production build：通过。
  - bundle：entry JS gzip 110738/110738；entry CSS gzip 37125/37125；max async JS gzip 31897/32052；未上调主线预算。
  - CSS：26 files、310967 source bytes、2106 rules、8514 declarations、151 repeated selectors、293189 production raw bytes、38109 gzip bytes；预算通过并向下收紧。
- `git diff --check`：通过；无新增依赖或 lockfile/go module 变化。
- 静态 allowlist/ownership 审阅：
  - production Go 中审计 INSERT 只有 `insertCommandActionAudit`；其余直接 INSERT 仅为 migration/integration/fake fixture。
  - `commandaudits.Action/Event`、handler 自有 response DTO、Web `CommandAudit*` 类型均无 `details`、stdout、stderr 字段。
  - handler 对实例、actor、action、event 逐字段映射；Web table/timeline 不遍历未知对象，不存在 JSON dump/output surface。
  - `/api/command-audits` 通过既有 session + same-origin protector 注册；未引入未批准的角色判断。
  - 新功能生产路径无 TODO/FIXME、debug log、warning suppression、TS bypass 或 JSX inline style。

## 2. Real PostgreSQL 16

本地 `postgres:16-alpine` 容器提供临时 admin database；测试按 package 创建并强制清理独立 database/schema，清理失败会使测试失败。

命令：

```bash
HOUFENG_POSTGRES_INTEGRATION=1 HOUFENG_DATABASE_URL='<local-postgres>' \
  go test ./internal/center/store/migrate ./internal/center/store ./internal/center/http/handlers -count=1
```

结果：

```text
ok houfeng/internal/center/store/migrate 1.467s
ok houfeng/internal/center/store 4.443s
ok houfeng/internal/center/http/handlers 0.613s
```

真实数据库断言覆盖：

- fresh install + 第二次 Apply；0046 audit schema → 0050 升级和 0050 SQL 重复应用。
- 旧记录 snapshot 回填；升级后旧式 queued/dispatched/completed INSERT 均成功。
- 只移除实例/actor 目标 FK，无关外键仍存在；三个审计索引均存在。
- action/rejected/source/reason/output constraints；删除 user/MonitoringInstance 后 audit、稳定 ID 与快照仍保留。
- snapshot helper 对真实/伪造 actor、缺失实例、queued/rejected 的 exact-row 完整性；rejected 写入时实例必须仍未归档、已绑定且未暂停。
- 五种 outcome、全部精确筛选、字面 `%`/`_` actor/instance filter、同时间复合 keyset、事件顺序、cursor handler 和永久清理后全局读取。
- cursor 真实测试在第一页返回后新增固定上界之外的 action，第二页仍无重复、漏项或窗口漂移。
- 应用层代表性一页固定两次 SQL；无 per-action SQL round trip。

最新 `EXPLAIN (ANALYZE, BUFFERS)`：

```text
default-window: execution=1.197ms, limit_rows=21, global_index_rows=360
outcome-failed: execution=1.113ms, limit_rows=21, global_index_rows=360
```

两条计划均使用 `idx_monitoring_instance_command_action_audit_global_time`，未出现 audit 全表 sequential scan；门槛为 execution <= 500ms、候选 <= 400、limit rows <= 21。

## 3. Fixture Chromium and local visual review

`env -u NODE_ENV npm --prefix web run test:e2e`：64/64 passed（Chromium，fresh production build + preview）。覆盖：

- 10 条核心路由 × 1440×1000 / 1024×768 / 390×900 = 30/30。
- `/command-audit` exact default fixture、undeclared request fail-closed、hostile action/event stdout/stderr/details 不进入 DOM。
- named/focusable local table scroll、事件展开、advanced Modal Tab/Escape/focus restore。
- Command Audit settled axe serious/critical = 0；统一 console/page/network/CSP diagnostics clean。

额外本地视觉复核使用 production preview + 显式 fixture：

- 1440×1000：筛选、标题、元数据声明和高密度表格层级清楚，表格无需横向滚动。
- 390×900：document 无横向溢出；表格 region 为 300px client / 1025px scroll，展开时间线可读；console/page error=0，hostile output marker=0。
- 两张截图只写 `/tmp`，复核后已删除，预览服务已停止。

该层只证明 fixture 数据下的 production frontend 行为和本地视觉判断，不证明真实后端认证部署或生产数据正确。

## 4. Real deployment / staging

- 未执行：`HOUFENG_STAGING_BASE_URL`、`HOUFENG_STAGING_USERNAME`、`HOUFENG_STAGING_PASSWORD` 均未配置。
- staging smoke 源码已加入第十条路由的 metadata-only/no-output 断言，并由 lint/type/build gate 覆盖；这不等同于真实部署 smoke。

## 5. Push / PR / required CI

- 未执行：用户明确要求不得 push。
- 当前仅在本地 `codex/command-audit-center`，基线 `origin/main@a375c0b0330b` 无漂移。
- 初始实现提交为 `487ee17a`、`d70d1e6d`、`8aeac032`；全方位审查修复提交为 `9ebb2f9f`。
- 未 push、未创建 PR、未运行 required CI、未 merge/release，也未删除旧 planning 分支。
- required CI 全绿前不能宣称远端交付完成；后续只有得到用户新授权才能恢复该流程。

## 6. Review findings resolved

- 0050 从“删除表上全部 FK”收紧为只删除包含实例/actor 列的目标 FK，并用真实无关 FK 回归证明。
- rejected 写入增加原子状态门，关闭 handler 预读到 INSERT 之间可检测的状态漂移。
- API 嵌套实例/actor 改为 handler-owned allowlist DTO，避免领域类型未来扩展自动扩大 JSON。
- custom 时间先做真实日历校验；加载更多失败保留旧结果和原 cursor 可重试。
- 宽表具备可见标题、滚动提示、ARIA 关联与键盘焦点；route lazy module 保持 named-only。
- hostile fixture 覆盖顶层与 event 的 stdout/stderr/details；临时数据库清理泄漏会直接使测试失败。

## 7. Residual boundaries

- 审计永久增长；本期无导出、自动保留期、摘要表或合规删除。
- 当前只有 admin 一种真实角色；未来第二种角色、多人写入或合规删除出现时，由 GitHub #381 重新评估 command authorization、audit read access、容量和隐私。
- base64url cursor 是 opaque transport，不是授权或防篡改边界；所有 decode 值仍校验，访问控制仍由 session/same-origin 与未来 RBAC 承担。
- 本地单元、真实 PostgreSQL、fixture Chromium 和人工视觉证据能显著降低风险，但不能替代 staging、required CI 或真实生产观测。
