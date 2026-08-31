# 实施合同摘录

本文件把本任务命中的超大 Trellis spec 收敛为可注入的执行边界；实施者和审查者仍须按 `trellis-before-dev` / `trellis-check` 打开相应完整 spec。

## Database / incident

- PostgreSQL 使用 pgx 手写 SQL；SQL 只能位于 store/snapshot reader，handler/service 不直接拼数据库查询。
- 已发布 migration 不可修改。追加下一个未占用编号，并保持 DDL/DML 幂等。
- 每个 post-`0051` embedded migration 必须在同一改动中注册 exact `AppACLCurrentMigrationFragment`；没有新增 APP-managed object/privilege 也必须注册 explicit empty fragment，且 `Privileges` compiler 不能为 nil。
- 新 migration 必须更新 exact embedded source inventory、fragment count/order/current admission tests；真实数据转换、catalog/index 和 runtime SQL 能力必须由隔离 PostgreSQL test 证明，SKIP 不是 acceptance evidence。
- 心跳 raw facts 保持 append-only；回填数据仍落库，但不得触发实时告警或作为实时恢复证据。
- current/latest 事实排序继续遵守 `observed_at DESC, is_backfilled ASC, received_at DESC, stable key DESC`。本任务的 recovery receipt 查询是独立读模型，不能改写该排序。
- incident mutation 使用 object row-version CAS；通知只能在 mutation commit 成功后 append/dispatch。首次 conflict 进行一次完整重读/重评，第二次安全 yield。
- 暂停、维护、退役、归档的 MonitoringInstance 行政恢复必须关闭 active incidents、写 recovered event，但保持零通知。
- 请求路径只持久化原始观测；incident 判定和通知仍属于既有异步 service/worker，不移入 HTTP transaction。

## Settings / Web

- `settings.Validate` 是持久化/API 输入的权威校验边界；前端字段保持与 Go JSON 完全一致的 snake_case。
- Settings 页面继续通过现有 `web/src/lib/` API façade；本任务不新增 raw `fetch`、全局 state、dependency 或 API 字段。
- 用户可见阈值文案必须与后端 evaluator 同义；修改默认值时同步 Go default、DB default/data migration、Web fixtures 和页面测试。
- 页面测试优先断言可见中文行为和准确 PUT payload；user-visible UI 改动进入 repository Chromium gate。

## Quality / delivery

- 先写能够在旧实现上失败的 focused RED，再做最小 GREEN；至少覆盖 evaluator、service 两入口、store query、migration 和 Web copy。
- Go 完整门为 `make verify-go`；跨前后端最终运行 `make verify-web` / `./scripts/verify.sh`，Node runtime 使用仓库 `.node-version` 固定的 Node 22。
- 真 PostgreSQL fixture 缺失时 broad test 可以 skip，但本任务明确要求的 migration/recovery acceptance 必须走 strict runner并实际 RUN/PASS。
- feature work 只在非 main 分支进行；commit/push/PR/merge/release/deploy/生产验收均遵守独立授权与受保护交付边界。
