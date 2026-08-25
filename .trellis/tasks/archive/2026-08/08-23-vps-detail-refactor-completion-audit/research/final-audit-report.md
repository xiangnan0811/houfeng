# VPS 详情页重构完成性最终审查报告

- 审查日期：2026-08-23（Asia/Shanghai）
- 冻结提交：`08730e7991f3242ed43fcad561cde1f3ea60b6fb`
- 审查分支：`codex/vps-detail-refactor-completion-audit`
- 基线状态：`HEAD`、`main`、`origin/main` 与 GitHub `main` 均为冻结提交
- 审查方式：findings-only、只读产品审查；未修改产品代码、测试、spec、migration、配置、CI、Git ref、PR、release 或外部数据

## 收尾复核（2026-08-25，取代下方冻结提交结论）

- 当前复核提交：`93f2a2fb1ed56035d1449ab39f183abc6acbe954`（`origin/main`）。
- 原审查的 I-01、I-02、I-03、I-04 与 M-01 已由归档任务
  `08-24-vps-detail-refactor-remediation` 及其四个 child 逐项关闭：动作/关系目的地采用
  closed resolver 与真实 owner，overview success DTO 使用 allowlisted validating decoder，
  source budget/freshness/关系 section 独立化，S3 runner 改为 exact-owned named volume 与
  fail-visible cleanup，React Router production dependency 升级到 7.18.2。
- 产品 PR #444 的七项 required CI、merge 后 main CI `32696132095` 均通过；发布后 CSS
  稳定性 PR #446 的七项 required CI 和最终 main CI `32697595709` 均通过；正式 release
  `v0.75.1` 已发布。任务归档 PR #447 也已合入。
- remediation 最终证据包括 Go 1.26.2 全量/竞态门禁、Node 22 `make verify-web`
  （196 files / 1359 tests）、Chromium 127/127、PostgreSQL 百万行 21 queries / 0 errors /
  0 skips，以及 Records browser/security/capacity/local/S3 integration/recovery 零残留。
- 当前 `main` 的后续 CI `32736193550` 仍为七项全绿；上述三个 merge commit 均为当前
  `origin/main` 祖先。没有重新打开 permanent delete 或三项已接受延期。

因此，本任务在当前批准范围内的未解决计数为 **0 Critical / 0 Important / 0 Minor**；
原冻结提交上的否定结论已被修复、独立复核、protected delivery 和发布证据取代，任务
可以归档。下方内容保留为 `08730e79` 上的原始审查记录和修复动机，不再描述当前产品状态。

## 冻结提交原始结论（历史记录）

**不能确认本轮 VPS 详情页重构已经完整、可靠且无需继续完善。**

在冻结提交上确认了 **0 Critical / 4 Important / 1 Minor**：三个 Important
直接位于新的 `/vps/:id` 概览主路径，一个 Important 位于该交付要求使用的 S3
集成/恢复验证生命周期；Minor 是锁定的生产 React Router 版本仍处在已披露问题范围。
保护分支、PR、required CI、post-merge CI、`v0.75.0` release 与多架构镜像交付链本身
完整；全量 Go/Web/Chromium 和大部分严格 Records 功能断言也通过。但这些通过项不能
覆盖下述可复现缺陷，尤其不能把 S3 功能断言通过误写成整个脚本干净、可重复地通过。

本审查没有实施修复。修复和回归测试需要另行授权。

## Findings（冻结提交时未修复，现已关闭）

### Critical

未发现。

### Important I-01 — 概览异常动作和关系入口不能到达其承诺的操作

**当前证据**

- `/home/murray/code/houfeng/internal/center/vpsoverview/anomalies.go:69-71`
  生成 `/vps/{id}/monitoring`，但 VPS 子路由仅有 IP 质量、activity、records、
  evidence 与详情页；`/home/murray/code/houfeng/web/src/app/router.tsx:121-125`。
- `/home/murray/code/houfeng/internal/center/vpsoverview/anomalies.go:82-84`
  生成 `/incidents`，而实际事件路由是
  `/home/murray/code/houfeng/web/src/app/router.tsx:180` 的 `/events`。未知路由在
  `/home/murray/code/houfeng/web/src/app/router.tsx:183` 被重定向到 `/`。
- IP 质量动作在 `anomalies.go:97-126`、订阅动作在 `:140-156`、管理动作在
  `:170-172`、重试动作在 `:185-187` 都返回当前 `/vps/{id}`；它们既不会打开
  对应管理面板，也不会触发刷新。
- `/home/murray/code/houfeng/internal/center/store/vps_overview.go:264-295` 给全部四类
  relation 返回同一个当前详情路由。
- Web 在
  `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewAnomalies.tsx:35-55`
  和
  `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewRelations.tsx:17-25`
  将非空服务端字符串无条件渲染为 `Link`。同一 `vpsId` 不会重新运行
  `/home/murray/code/houfeng/web/src/pages/VPSDetailPage.tsx:36-82` 的 gate effect。

**影响与复现**

- 在 production build 的受控异常 fixture 中，从 `/vps/vps_001` 点击“查看监控”后，
  实际 URL 变成 `http://127.0.0.1:4178/`，即落入 wildcard 后返回 dashboard。
- 点击同路径的“查看续费”后仍停留在 `/vps/vps_001`；没有产生新的
  `/api/vps/vps_001/overview` 请求，页面标题和工作区均未变化。
- 同类错误会影响事件、IP 质量、订阅、管理、重试以及 relation 卡片。概览作为
  决策入口时，用户看到一个可执行动作，但无法完成动作标签承诺的任务。

**现有测试为何未阻止**

- `/home/murray/code/houfeng/internal/center/vpsoverview/anomalies_test.go:30-134`
  只检查规则存在，不校验目的地能否落到实际路由或命令 owner。
- `/home/murray/code/houfeng/internal/center/store/vps_overview_test.go:100-113`
  只检查 relation 数量。
- `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewPageView.test.tsx:137-157`
  使用当前路由作为 fixture，且只断言 link 存在。

**最小修复方向**

建立一个由前端识别并跨层测试的 action/destination 合同；事件、监控、IP 质量、订阅
和 relation 只返回已注册且能完成任务的目标。管理与刷新应使用有 owner 的 command，
而不是同路由 link。为每条服务端动作增加 router/callback 落地测试。

### Important I-02 — transport、JSON 解码和 2xx DTO 错误会静默进入 legacy 页面

**当前证据**

- `/home/murray/code/houfeng/web/src/pages/VPSDetailPage.tsx:20-25` 声明只有 capability-off
  或 overview endpoint 缺失/不可用才允许 legacy fallback，其他加载失败应显示错误。
- 实际 catch 在 `/home/murray/code/houfeng/web/src/pages/VPSDetailPage.tsx:60-77` 只细分
  `ApiError`，所有其他异常都执行 `setGate('legacy')`。
- `/home/murray/code/houfeng/web/src/lib/apiRequest.ts:110-117` 对 2xx body 直接
  `JSON.parse`；损坏 JSON 抛 `SyntaxError`，fetch/网络层可抛 `TypeError`，二者都不是
  `ApiError`。
- `/home/murray/code/houfeng/web/src/lib/recordsApi.ts:512-589` 将 `unknown` 强转并补齐
  默认值，没有完整 runtime schema。`200 {}` 被规范化为 capability 为空，随后选择
  legacy；`200 null` 在属性读取时抛异常，也进入同一 broad fallback。

**影响与复现**

production build 中把 overview mock 为 HTTP 200 但 body 是 malformed JSON，同时提供
legacy API 后，页面显示 legacy 标题 `Legacy Tokyo Edge`、legacy sections，且不再显示
overview 的“管理”按钮；用户没有看到错误或重试。网络中断、代理响应损坏和前后端
DTO 漂移都会因此伪装成正常的“功能关闭”，还可能在故障期间额外触发 legacy 请求图。

**现有测试为何未阻止**

`/home/murray/code/houfeng/web/src/pages/VPSDetailPage.test.tsx:467-497` 仅覆盖明确的
404、503 和 500 `ApiError`；没有 `TypeError`、`SyntaxError`、`null`、`{}` 或结构非法
的 2xx body。

**最小修复方向**

在 API 边界完整校验 overview success DTO，并把失败映射为有类型的 decode/contract
错误。legacy 只允许由明确、allowlisted 的 feature/capability-off 信号选择；所有未知
transport/decode/contract 错误都进入可见、可重试的 gate error。补齐上述五类回归测试。

### Important I-03 — source budget 相互耦合，且局部失败 freshness 被丢失或伪造

**当前证据**

- current-authority 要求 monitoring、IP、subscription、relations 和 activity 独立降级，
  并保留 `ready|stale|unavailable`、observed time、last success、安全 reason 与 retry：
  `/home/murray/code/houfeng/.trellis/tasks/archive/2026-08/07-14-vps-records-activity-overview/prd.md:27-31`
  和 `:47-48`；对应设计在
  `/home/murray/code/houfeng/.trellis/tasks/archive/2026-08/07-14-vps-records-activity-overview/design.md:135-143`、
  `:159-161`、`:172`。
- `/home/murray/code/houfeng/internal/center/vpsoverview/service.go:139-145` 给整个非 activity
  `LoadSources` 一个 timeout；
  `/home/murray/code/houfeng/internal/center/store/vps_overview.go:95-109` 又按 monitoring →
  IP → renewal → relations 顺序共用该 context。前置慢源可以耗尽后续独立 section 的
  预算，形成级联失败。
- `/home/murray/code/houfeng/internal/center/vpsoverview/types.go:123-130` 的 relation 没有
  `SectionState`；relation 错误在
  `/home/murray/code/houfeng/internal/center/store/vps_overview.go:278-295` 被压缩为 count/status，
  没有统一 freshness/retry owner。
- `/home/murray/code/houfeng/internal/center/store/vps_overview.go:251-254` 把未来的业务
  续费截止时间同时写成 `ObservedAt` 和 `LastSuccessAt`。一个五天后续费的健康订阅会
  声称其源在五天后被观测且成功读取。
- `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewSummaryGrid.tsx:18-27`
  只显示 status/detail，丢弃 `section.state`、时间和 reason；
  `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewPageView.tsx:85-94`
  的 retry 只覆盖 activity/monitoring，不覆盖 IP、renewal 或 relations。

**影响与复现**

一个慢源能让后续无关 section 一起失败；单独的 IP、subscription 或 relation 失败又可能
呈现成普通未知值、零或空列表，没有本地 freshness 和恢复入口。production preview 的
unavailable monitoring/stale IP fixture 仍只显示状态/详情和全局说明，section 本地没有
last-success/reason。未来 renewal timestamp 则是直接错误的操作事实，会误导人工或下游
判断。

**现有测试为何未阻止**

`/home/murray/code/houfeng/internal/center/store/vps_overview_test.go:75-113` 正好创建了
五天后的 renewal fixture，但只断言 identity/count；没有时间不变量、慢源隔离、relation
failure state。Web fixture 填了 section 字段，却没有断言其可见性或五类局部失败的 retry。

**最小修复方向**

在整体预算内给每个 source 独立的有界读取；为 relation 增加 `SectionState`；将真实读取/
观测时间与 `next_renew_at` 分开；所有 degraded section 都呈现安全 freshness 与可工作的
retry。增加五类局部失败、前置 timeout 隔离和“freshness 不得在未来”的表驱动测试。

### Important I-04 — S3 验证脚本把清理失败掩盖为成功并遗留 root-owned MinIO 状态

**当前证据**

- `/home/murray/code/houfeng/scripts/run-records-integration.sh:43` 和
  `/home/murray/code/houfeng/scripts/run-records-recovery.sh:54` 创建调用用户拥有的临时
  workspace。
- 两个脚本分别在 integration `:90-99`、recovery `:101-110` 把
  `$workspace/minio` bind-mount 到 MinIO `/data`，却没有 host UID/GID 映射；容器会在
  host 创建 root-owned `.minio.sys`。
- cleanup 在 integration `:48-58`、recovery `:59-69` 使用 unprivileged
  `rm -rf "$workspace" || true`，并最终返回清理前 suite status。因此 suite 通过时，
  即使 teardown 明确失败，脚本仍为 exit 0。
- `/home/murray/code/houfeng/internal/center/recordbackup/profile_script_test.go:18-34`
  与 `recovery_script_test.go:18-34` 是字符串存在性测试，反而要求上述 `|| true`，没有
  执行容器生命周期、验证 ownership/残留或传播清理失败。

**实际复现**

- `scripts/run-records-integration.sh --profile s3`：功能断言通过；cleanup 多次输出
  `Permission denied`，命令仍成功；遗留
  `/tmp/houfeng-records-integration.kfusdi/minio/.minio.sys`，内部为 `root:root`。
- `scripts/run-records-recovery.sh --profile s3 --all`：完整功能断言通过，但同样以
  `Permission denied` 结束清理且仍返回成功；遗留
  `/tmp/houfeng-records-recovery.isqWM4/minio/.minio.sys`。
- 集成残留在独立检查时为 156 KiB。重复运行会积累调用用户无法正常删除的 `/tmp`
  状态并消耗 quota；本次后续 browser wrapper 的第一次运行也受临时 quota 影响。

这不否定 teardown 前完成的 S3 功能断言，但它使 required gate 成为 false-clean：脚本
无法证明自身是干净、可重复的完成门禁。

**最小修复方向**

让 MinIO 数据由调用用户可回收地拥有（例如受控 Docker volume 或已验证的 host UID/GID
映射）；不要无条件吞掉 workspace 删除失败；既保留原 suite failure，又在 suite 成功但
teardown 失败时返回失败。增加一个真实 S3 lifecycle 测试，断言 container、volume 和
workspace 全部消失。

### Minor M-01 — production React Router lock 仍处于已披露问题范围，生产依赖审计为红

**当前证据与适用性判断**

- `/home/murray/code/houfeng/web/package.json:23-30` 直接依赖
  `react-router-dom ^7.17.0`；
  `/home/murray/code/houfeng/web/package-lock.json:5073-5102` 将 `react-router` 和
  `react-router-dom` 都锁在 `7.17.0`。
- Node 22 下新鲜执行 `npm --prefix web audit --omit=dev --json` 返回 exit 1，production
  aggregate 为 2 个 vulnerable packages（`react-router` high、`react-router-dom`
  moderate，聚合 5 条 advisories）。
- 与当前 Data Mode 客户端最相关的是将 attacker-controlled 外部 URL 传给
  `<Link>`/`useNavigate` 时的 external navigation advisory；受影响范围 `<7.18.0`，
  7.18.0 起修复。当前 UI 在
  `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewRelations.tsx:19` 和
  `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewAnomalies.tsx:37,48`
  直接消费 API route 字符串，没有前端 internal-route 约束。
- 但当前 backend 只生成固定内部前缀加 server ID，handler 又拒绝 ID 中含 `/`；本次
  **没有证明一条 attacker-controlled 外部 URL 的当前 exploit chain**。官方 high DoS
  advisory 明确限定 Framework Mode，本应用使用
  `/home/murray/code/houfeng/web/src/app/router.tsx:190` 的 Data Mode；RSC/SSR 类 advisory
  同样不适用。因此本项不按 high/Important 或已可利用漏洞报告。

参考：
[external navigation advisory](https://github.com/advisories/GHSA-wrjc-x8rr-h8h6)、
[Framework Mode 范围说明](https://github.com/remix-run/react-router/security/advisories/GHSA-chx6-hx7r-mcp5)、
[React Router 7.18.2 release](https://github.com/remix-run/react-router/releases/tag/react-router%407.18.2)。

**影响与最小修复方向**

当前现实风险低，但 production audit 持续为红；且 API route 已进入真实 navigation sink，
客户端没有 defense-in-depth 约束。将 lock 更新到当前已修复 7.18.x，运行全量 Web/E2E，
并把 overview action/relation 目标限制为已知内部 route/command。该项足以阻止“连 Minor
完善都不需要”的绝对结论，但不改变前三项产品缺陷的优先级。

## 审查过且未形成额外 finding 的范围

| 能力/链路 | 当前 owner 与验证 | 结果 |
| --- | --- | --- |
| VPS overview 与五类管理操作 | `vpsoverview`、store、overview Web/API/router、production preview | I-01、I-02、I-03；五个管理 dialog 本身可达且焦点行为通过 |
| Subject activity | `activity`、subject activity API/Web、cursor/freshness/auth filter | focused/full/E2E 通过，无额外 finding |
| Records core 与修订/草稿 | `records`、handlers、migration 0052...0059、workspace Web | 授权/CAS/strict decode/immutable revision 无额外 finding |
| Attachments 与 Blob local/S3 | `attachments`、evidence delivery、Blob ownership/admission | 功能断言通过，无额外产品 finding；S3 runner 生命周期见 I-04 |
| Evidence | `evidence`、attachment lease/stream reauthorization、Web evidence | 无额外 finding |
| Markdown | `recordmarkdown`、阅读/编辑/修订、sanitize/allowlist | 无 XSS/locator/credential 泄漏 finding |
| Search | `recordsearch`、authorized hydration、Records search Web | 无存在性/总数泄漏 finding |
| Comparison | comparison service/Web、具名横向滚动区、semantic row headers | 当前合同通过；390px sticky body-row 仍为接受延期 |
| Collaboration/notifications | collaboration、notifications、trusted actor、outbox/worker | 无额外 finding |
| Portability | import/export、untrusted boundary、download reauthorization | 无额外 finding |
| Archive/restore/recovery | archive/restore application、backup/restore、local/S3 suites | 产品语义无额外 finding；S3 gate lifecycle 见 I-04 |
| Config/bootstrap/readiness | flags、ACL/admission、production handlers、registry pairing | permanent delete 保持 fail-closed |

权限与非泄露路径做了独立交叉检查：record detail/list/revision 在 materialization 前重新
授权；search 通过同一 authorized reader hydration；evidence 和 attachment download 在
签发/stream 时重鉴权；denial 保持 resource-free；Markdown、attachment、portability 未
发现 raw locator、credential、grant、token、proof、secret 或 command output 外泄。

## 新鲜验证结果

### 工具与环境

- repository `go.mod:3`：Go `1.26.2`；host Go：`go1.27.0-X:nodwarf5 linux/amd64`
- exact Go 全量门禁：`golang:1.26.2` container，`--init --user 1000:998`
- Node `v22.23.1`，npm `10.9.8`
- Docker client/server `29.7.2 / 29.7.2`
- Playwright `1.61.1`
- Git hooks：`.githooks`

### 自动化门禁

| 命令/门禁 | 新鲜结果 |
| --- | --- |
| `git ls-files '*.go' -z \| xargs -0 gofmt -l`（Go 1.26.2） | PASS；无输出 |
| `go vet ./agent/... ./cmd/... ./db/... ./internal/...`（Go 1.26.2） | PASS |
| `go test ./agent/... ./cmd/... ./db/... ./internal/... -count=1`（Go 1.26.2） | PASS；所有 package 通过 |
| focused backend owner tests/vet | PASS；overview、records auth/search/activity、config/readiness/handlers 等均通过 |
| focused Vitest | PASS；17 files / 122 tests |
| `make verify-web`（Node 22） | PASS；lint；192 files / 1254 tests；coverage；TypeScript/Vite production build；bundle/CSS budget |
| coverage 摘要 | 81.36% statements / 73.73% branches / 81.41% functions / 85.39% lines |
| focused Playwright | PASS；38 tests |
| `npm --prefix web run test:e2e` | PASS；109 tests（Chromium project） |
| `scripts/run-records-browser.sh` | PASS；64 tests；production bundle fixture/helper leakage check 通过 |
| `scripts/run-records-security.sh` | PASS；7 packages；无 skip |
| `scripts/run-records-capacity.sh --profile local` | PASS；真实 PostgreSQL；这是当前 bounded runner，不冒充延期的正式 mixed-load harness |
| `scripts/run-records-integration.sh --profile local` | PASS |
| `scripts/run-records-integration.sh --profile s3` | **功能断言 PASS；teardown FAIL 但脚本 exit 0**，见 I-04 |
| `scripts/run-records-recovery.sh --profile local --all` | PASS |
| `scripts/run-records-recovery.sh --profile s3 --all` | **功能断言 PASS；teardown FAIL 但脚本 exit 0**，见 I-04 |
| `npm --prefix web audit --omit=dev --json` | FAIL / exit 1；见 M-01 |

第一次 host Go/browser-wrapper 尝试遇到 `/tmp` user quota，早期 root/no-init Go container
也出现 PID 1 zombie/ownership 干扰。审查没有把这些环境失败归为产品问题：在 `/dev/shm`
或 exact Go 1.26.2 non-root `--init` container 中的有界重试均通过。与之不同，I-04 是
脚本自身确定性的 ownership/cleanup 缺陷，且已在两个 S3 profile 复现，不能按环境噪声
忽略。

### Production browser 证据

- 使用当前 `web/dist` 的本地 production preview 和受控 API fixture；不接触 staging。
- 1440×1000 与 390×900 的主要概览布局可用，没有发现新的水平溢出、裁切或触摸目标
  blocker。
- 完整 E2E 已覆盖 overview Axe；没有 serious/critical violation；390px overflow 与
  44px target 合同通过。
- 管理菜单打开后首个 menuitem“编辑事实”获得焦点，Esc 关闭并把焦点还给“管理”。
- “编辑事实”“续费决策”“订阅事实”“取消 / 退役”“归档”五个真实 dialog/alertdialog
  均在 production build 可打开；Esc 关闭后五次都恢复到管理触发器。
- 稳定、异常和局部失败 fixture 没有新增几何 finding；动作落地和 malformed JSON 则分别
  复现 I-01 与 I-02。
- 交互式抽查使用 Playwright Firefox（本机 Chrome channel 不存在）；完整 109-test E2E
  仍由仓库 Chromium project 新鲜通过，因此没有将 Firefox 抽查冒充 Chromium gate。

## Git、GitHub、release 与镜像对账

程序基线 `2cbeb1bb^1` 为 `d38a8cad382667822059188e46afc31f096f8916`；从该基线
到冻结提交共 193 commits、1,059 changed paths。所有相关 protected-main merge 都是
冻结提交祖先，且每个 selected/head 与对应 merge tree 相同。

必须区分 merge 与 squash：前七个 PR（#394、#397、#400、#408、#410、#413、#416）
是 two-parent merge，selected head 是 merge 的 graph ancestor；从 #422 起的十个 pair
是 one-parent squash，selected head **不是** merge 的 graph ancestor，只能表述为
tree-equal 且 merge 到达 `HEAD`。尤其 `35ade851` 不是 `38a5524d` 的 ancestor；本报告
以这一结论更正 `primary-static-audit.md:327` 的措辞。

| Scope | PR | Selected/head | Protected-main merge | Main CI run |
| --- | ---: | --- | --- | ---: |
| Platform foundation | #394 | `7858b30c` | `2cbeb1bb` | 30751460764 |
| Records core | #397 | `ba5f2d8d` | `2279a7fd` | 30874511041 |
| Attachments/storage | #400 | `1887821c` | `78bf44c1` | 31317881804 |
| Evidence platform | #408 | `9ac3e255` | `6a0122a7` | 31981626635 |
| Collaboration | #410 | `c6742c3c` | `a3137864` | 32045454459 |
| Markdown workspace | #413 | `199d2a2b` | `d41c8630` | 32123291411 |
| Search center | #416 | `c01663df` | `bcd8e53a` | 32212463173 |
| Activity/overview | #422 | `b9698ffa` | `b3901e5f` | 32344976169 |
| Comparison workbench | #423 | `9924eae0` | `aacb9c50` | 32442306154 |
| Portability/migration | #425 | `808fda62` | `9e910d7c` | 32466708721 |
| Archive/restore fidelity | #428 | `7ce5dbdd` | `c7081519` | 32475409091 |
| Integration rollout | #433 | `1a440e86` | `79f62aac` | 32497370438 |
| musl/S3 correction | #436 | `35ade851` | `38a5524d` | 32542193350 |
| Overview management | #438 | `7e9080f2` | `af23844a` | 32637395760 |
| Current-authority audit artifact | #441 | `290a5c6d` | `6e9be76e` | 32640659843 |
| Parent archive artifact | #442 | `36d2f808` | `8615679c` | 32641517555 |
| Receipt/current-main artifact | #443 | `e802bb07` | `08730e79` | 32642179923 |

- 上述 17 个 PR 均为 `MERGED`；每个 PR 的七个 required checks 均为 success：Go、Web、
  Web browser、Docker image，以及 PostgreSQL 16.0/16.6/16.12 catalog。
- 每个 merge 后的 protected-main push workflow 均成功；冻结 main 的七个 CI jobs 也成功。
- tag 与公开、非 draft/non-prerelease release `v0.75.0` 都指向
  `ab1ad7cdaab4a7ee57b782a3a9a45e5074b591bd`；发布时间
  `2026-08-23T11:49:42Z`，且包含 overview product merge `af23844a`。
- release assets 有 Linux amd64/arm64 agent、`sha256sums.txt` 和 minisign signature。
- `publish-images` run `32637639621` 全部成功。
- `docker.io/linnea7171/houfeng:v0.75.0` 的 OCI index digest 为
  `sha256:22df0845c806f69f9d4bccecf02227b744b9588e73de86eb03338c068be14415`，
  含 linux/amd64、linux/arm64 manifests 与 attestations。

交付链因此是真实且完整的；I-01...I-04/M-01 是已交付当前产品/门禁中的现存问题，
不是“PR 没合入”或“release 没发布”。

## 明确边界与接受延期

### Permanent delete

单条 Records 永久删除仍按批准边界 fail-closed，不是缺失功能 finding：flags 默认 false，
尝试启用会被拒绝；production 使用 nil handler；用户 capability 不暴露 permanent delete；
readiness 仍缺少以下七项，因此不能启用：

- `deletion.record_markdown_client`
- `deletion.record_comparison`
- `recovery.record_search`
- `recovery.record_collaboration`
- `recovery.record_portability`
- `backup.orchestration`
- `restore.replay`

普通 archive/restore、可丢弃环境整体重建与单条不可逆删除在本审查中保持为三种不同语义。

### 接受延期

以下 future trigger 当前均未成立，代码也没有误报已完成，因此不构成 finding：

1. activity group-granted digest；viewer 仍只允许 project visibility digest；
2. comparison 390px sticky body-row header；当前仅承诺具名 scroll region 与 semantic row header；
3. 4 GiB / 512 MiB fixed-arrival 三轮 mixed-load 正式 harness；当前 capacity runner 仍只声明
   bounded scale `0.001`。

## Acceptance Criteria 对账

| AC | 状态 | 当前证据或 blocker |
| --- | --- | --- |
| AC-01 | Pass | 当前代码、task tree、12 functional children、overview follow-up、PR/CI/release/image 已对账；squash ancestry 措辞已纠正 |
| AC-02 | **Fail** | I-01、I-02、I-03 位于 canonical `/vps/:id` overview/fallback/freshness 路径 |
| AC-03 | Pass（冻结范围内） | Records 共享授权、修订、搜索、附件/证据、比较、协作、可移植性与恢复合同无额外 finding |
| AC-04 | Pass | permanent delete fail-closed；普通 archive/restore 功能断言通过；三项延期仍准确 |
| AC-05 | Pass（代码质量门） | exact Go 1.26.2 format/vet/tests 与 Node 22 Web lint/coverage/build/budgets 通过；M-01 是额外 production dependency audit finding |
| AC-06 | **Fail/Partial** | Chromium、browser/security/capacity/local 功能门及 S3 功能断言通过，但两个 required S3 runner teardown 失败仍 exit 0，见 I-04 |
| AC-07 | Pass | production preview 的 1440/390、五个管理面板、键盘/Esc/focus return 已抽查；Chromium E2E 的 Axe/overflow/44px 合同通过 |
| AC-08 | **Fail** | 0 Critical / 4 Important / 1 Minor，不能输出“无需继续完善” |
| AC-09 | Pass | 仅当前 task artifact 有变化；未改产品/spec/archive/Git/外部状态。仅精确清理本审查自己创建的临时残留和 cache volume，保留所有预存未知残留 |
| AC-10 | Pass | 本报告记录冻结 commit、命令/结果、日期、证据、边界和可复现限制 |

由于 AC-02、AC-06 和 AC-08 不满足，task 保持 `in_progress`，不归档、不提交、不发布。

## 状态隔离与残余限制

- 审查结束前，worktree 只有
  `.trellis/tasks/08-23-vps-detail-refactor-completion-audit/` 为 untracked；产品、spec、
  archived task、migration、配置和 Git refs 均无 diff。
- 为恢复本次验证产生的 quota，只删除了本审查准确识别并创建的两个 workspace
  `/tmp/houfeng-records-integration.kfusdi`、`/tmp/houfeng-records-recovery.isqWM4`，以及
  两个本审查命名的 Go cache volumes
  `houfeng-audit-go126-mod-20260823`、`houfeng-audit-go126-build-20260823`。
- 预先存在、ownership/来源不明的
  `/tmp/houfeng-records-integration.a8zBzi`、
  `/tmp/houfeng-records-recovery.Iyz0MV`、
  `/tmp/houfeng-records-integration.sUO1to` 均保留，未作清理。
- 本报告证明的是冻结提交、已列范围和 2026-08-23 的新鲜门禁结果；静态审查不能证明
  未来绝无缺陷。没有执行新的 20 人理解研究、长期 soak 或已明确延期的正式容量 SLO
  harness，也没有访问 staging/production 业务数据。

因此，下一步不是无边界“继续优化”，而是先解决 I-01...I-04，并在升级/约束 React
Router 后重新运行对应 focused、全量、production browser 与完整 local/S3 lifecycle
门禁。修复完成前，不应把本轮重构标记为“完整且无需进一步工作”。
