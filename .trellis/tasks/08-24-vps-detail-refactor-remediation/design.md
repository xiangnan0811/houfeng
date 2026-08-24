# VPS 详情页重构审查修复技术设计

## 1. 边界与交付单位

本任务从审查冻结提交 `08730e7991f3242ed43fcad561cde1f3ea60b6fb` 修复 4 个
Important 与 1 个 Minor。四个 child 各自拥有产品实现与 focused evidence；parent 只拥有
依赖排序、跨 child 合同对齐、完整回归、独立复审和 protected delivery。

不补永久删除 readiness/adapters，不提前实现三项已接受延期，也不顺手重构 unrelated
Records/VPS surfaces。所有修改位于 `codex/vps-detail-refactor-remediation` 非 main worktree。

## 2. 总体数据与故障边界

```text
PostgreSQL / activity service
  -> independent overview source reads
  -> Go Overview DTO (truthful SectionState + owned action/relation targets)
  -> HTTP 200 JSON
  -> recordsApi local validating projection
  -> explicit capability/error gate
  -> VPSOverviewPageView local section states + owned commands/navigation

Records S3 required gates
  -> exact labeled containers + named volume + workspaces
  -> suite status
  -> shared teardown status arbitration
  -> zero-residue assertion
```

服务端以 identity 为唯一 fatal source；其余 source 在同一总预算内得到独立、有界的执行机会，
并把真实 freshness 放进各自 section。客户端先验证完整 DTO，再判断 capability；只有有效
feature-off 或显式 endpoint unavailable 才加载 legacy。UI action 要么指向已注册安全站内 route，
要么由当前 page callback 执行；所有已知 relation 有 exact route/command，未知 relation 不可点击。required harness 同时证明功能与
teardown，不能以 cleanup failure 后仍返回 0 取得绿色。

## 3. 关键设计选择

### 3.1 Action 与 relation ownership

采用“稳定 rule/action/relation token + 精确 route/command owner”模型：导航型 action 的 API
route 必须等于该 token 的唯一已注册目标；page-owned action 只给 ID/label 并由 Web callback
执行，relation route 可缺省。monitoring/service/domain relation 复用现有 VPS-scoped 内容，在
canonical page 增加只读 panels；subscription 使用过滤 route。替代方案一是把所有行为塞 query
参数，容易让 route 冒充 command；替代方案二是引入 free-form command 字段或通用 registry，
会增加第二个未类型化控制面。

route-private resolver 只接受 exact token + exact computed destination；scheme、protocol-
relative、反斜杠、未知 token、command 上的 route 或任何不匹配值都返回无 destination。具体
route 再以 router registration tests 固定。React Router 升级到 7.18.2，并以 production audit、
build 和 E2E 证明兼容。

### 3.2 Overview success validation 与 gate

采用 `recordsApi.getVPSOverview` 局部手写 typed decoder。共享 `apiRequest` 保持泛型 transport
语义；不新增 schema library，也不把 permissive normalization 当 validation。decoder 构造
allowlisted fresh object、允许 additive unknown fields、拒绝已知结构失真且不保存 raw payload。

门控是显式 allowlist：valid capability-on → overview；valid capability-off 或
`overview_unavailable` → legacy；404 → not found；其他 transport/decode/contract/HTTP failure →
可见可重试 error。generic 503 不再代表 feature-off。

### 3.3 Source isolation 与 freshness

采用 identity-first 后并发聚合其余 source，并保留一个 request-scoped overall budget；每个
source 使用自己的 bounded result/state，不允许共享顺序读取使后源没有执行机会。相较“继续
顺序读取但给每源 timeout”，并发更直接满足独立执行；相较重写整个 `SourceReader` 接口，本次
只在 store/service 聚合边界增加最小结果结构，避免扩散。

`SectionState` 是唯一 freshness wire vocabulary：`ready|stale|unavailable`、nullable
observed/last-success 与安全 reason。relation 也携带 section；读取失败时 count 只作占位，不得
被 UI 解释成可信零。renewal deadline 保留业务含义，但不再写作 observation；success time 不得
晚于 `generated_at`。UI 在 summary/activity/relation 的本地 owner 显示状态、时间、reason 与
refresh，不添加全局“看似全坏”的横幅。

### 3.4 S3 harness lifecycle

采用 Docker-managed、带本次 run label 的 named volume，而不是 root container 写 host bind
mount。替代的 host UID mapping 会耦合 daemon/image 用户；tmpfs 会引入 Linux/memory 假设；
privileged cleanup helper 扩大权限面，均不采用。

三个 runner 共用明确的 teardown 状态仲裁：先保留 suite status，再尽力按 container → volume
→ workspace 清理；suite 成功而任一 cleanup 失败必须非零，suite 原本失败则保留原 code 并
同时报告 teardown。默认与 required gate 零残留；显式 keep 模式只保留并打印约定资源。

## 4. 合同兼容与依赖顺序

1. Freshness child 先冻结 Go/TS `RelationSummary.section` 与 source semantics。
2. Action child 在该 relation shape 上冻结 route 可选性与 command callbacks。
3. Gate child 最后按完整 wire shape 实现 decoder；其非 shape RED tests 可提前准备。
4. S3 child 无产品 DTO 依赖，可独立推进，但与 parent 全量 Go/Records gates 汇合。

三个 Web children 会接触 `types.ts`、`VPSOverviewPageView` 或 fixtures，必须按上述顺序串行
落地重叠文件，禁止并行覆盖。S3 文件集独立。每个 child 完成 focused check 后由 parent 在同一
branch 做跨层 diff review；不创建彼此漂移的临时长期分支。

## 5. 安全、隐私和错误语义

- 权限/不存在保持既有统一边界；overview 修复不新增写 API 或提升权限。
- reason code、错误 copy、labels 和 test reports 禁止 raw SQL/backend error、URL、DSN、token、
  response body、record/evidence content。
- unknown action、unsafe route、invalid DTO、unknown freshness 与 cleanup ambiguity 全部 fail closed。
- archive/restore 和 permanent-delete disabled 合同必须在完整 Records gates 中保持。

## 6. 回滚

无 migration。每个 child 可独立 revert，但不得用恢复 silent legacy fallback、伪 freshness、空
relation link 或 masked cleanup 作为长期回滚。若 UI 修复出现 release regression，可关闭现有
`records_v2_read` capability 回到 legacy，同时保留服务端可信 DTO；harness 修复只能通过 revert
回滚，不影响产品数据格式。protected delivery 的任何失败在同一 feature branch 修复。

## 7. 完成证据

完成需要：每项 finding 的真实 RED→GREEN、focused/whole Go 1.26.2 和 Node 22 gates、完整
Chromium/Axe/390px/keyboard/focus、local+S3 integration/recovery 无 skip 且零残留、独立
`trellis-check` 无 findings，以及 PR CI、main CI、Release Please、release、多架构镜像的最终
receipt。缺任何层级只能报告部分完成。
