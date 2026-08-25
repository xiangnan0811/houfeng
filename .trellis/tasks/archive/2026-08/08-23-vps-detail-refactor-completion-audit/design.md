# VPS Detail Refactor Completion Audit Design

## 1. Audit posture

本任务是 read-only、findings-only 的独立审查，不是新的功能实现或历史任务补洞。
产品代码与历史 task artifact 均视为被审对象；只有当前任务目录可记录计划、研究、
命令结果和最终报告。

审查采用“主张必须被当前证据反证尝试”的方式：先把历史完成结论拆成可验证主张，
再从当前代码、测试、运行时门禁和交付状态逐项寻找反例。只有不存在未解释反例且
所需门禁全部新鲜通过，才允许给出完成结论。

## 2. Authority order and baseline

事实优先级固定为：

1. 当前 `origin/main`/selected review commit 上的实际代码、配置、迁移和生成物；
2. 当前运行的自动化、真实浏览器和 strict integration/recovery 结果；
3. Git ancestry、GitHub PR/check/main CI/release/tag/image 当前状态；
4. current-authority Trellis PRD/design/implement/handoff；
5. archived child 自报与历史研究记录。

低优先级与高优先级冲突时形成 finding，不通过回写历史文档消除冲突。审查基线为
任务创建时 `origin/main=08730e79`；开始执行时重新 fetch/解析远端状态，如远端已
推进则记录新基线并重新判断相关性，不静默混用两个 commit 的证据。

## 3. Scope map

### 3.1 User-facing composition

- `web/src/pages/VPSDetailPage.tsx` 与 `web/src/pages/vps-detail/`：overview gate、
  legacy fallback、页面组合、管理操作与局部状态。
- `web/src/pages/SubjectActivityPage.tsx`、`SubjectRecordsPage.tsx`、
  `RecordComparisonPage.tsx`、`web/src/pages/records/`：详情页的深度工作区。
- `web/src/lib/recordsApi.ts`、共享 types、router/layout/search：DTO、错误、URL、
  入口和 capability 传播。

### 3.2 Backend and persistence

- `internal/center/vpsoverview`、`activity`、`records`、`recordsearch`、`evidence`、
  `attachments`、`recordmarkdown`、`recordauth`、collaboration/notification、
  `portability`、`recordbackup`、`recordrestore`、`recordreadiness`。
- `internal/center/http/handlers`、router/bootstrap/config：transport、strict decode、
  wiring、feature flags 和 fail-closed 边界。
- `db/migrations/0052...0059`、store/ACL/admission：schema、权限、projection、
  local/S3/backup/restore 一致性。

### 3.3 Delivery set

审查 12 个功能 child 的 selected/main merge、overview follow-up #438、musl/S3
follow-up #436、release `v0.75.0` 和最终 archive/current-authority 提交。文档-only
closeout 不计为第 13 个产品能力。

## 4. Review streams

### Stream A: Requirements and change-set reconciliation

从首个功能 child merge 的第一父提交计算 pre-program baseline，逐 PR/merge 检查
实际相关 diff 和后续修复。将每个 current acceptance claim 映射到当前 owner 文件、
测试与运行门禁，标记 missing、stale、contradictory 或 verified。

### Stream B: Static and cross-layer review

以风险为中心审查当前代码，而不是平均阅读所有行：

- 路由/capability/feature-flag 与 production wiring；
- 写操作的 authoritative preview、duplicate submit、stale response、refresh failure；
- DTO/错误/授权跨 UI、handler、service、store 的保持；
- transaction/CAS/idempotency/outbox、projection/cursor/watermark；
- Markdown/附件/证据/导入导出安全边界；
- archive/restore 与 permanent-delete fail-closed 分离；
- local/S3 和 backup/restore 的同语义实现。

Trellis `trellis-check` 子代理执行独立代码/spec 审查并只写本任务审查报告；主会话
复核每个 finding 的当前文件与行号，不能直接采信子代理结论。

### Stream C: Fresh automated verification

先跑 focused tests 以定位 owner，再跑全量 Go/Web/Chromium；最后运行需要真实
PostgreSQL/MinIO/Docker 的 strict Records 门禁。所有脚本的 skip 检查保持原样，
不通过环境变量或修改脚本降级。

`make verify-go` 含 `go fmt` 写操作，因此 read-only 审查使用 `gofmt -l`、`go vet`
和 `go test` 的等价非修改组合。Web 构建和测试只允许生成 ignored cache/dist/test
artifact，结束时用 Git 状态确认没有 tracked 产品 diff。

### Stream D: Real browser review

使用 current production build/preview 和仓库 fixture/API mock，自动化并人工观察
desktop 1440 与 mobile 390。核对页面主要状态、五类管理 dialog、局部失败、
route-switch、键盘/焦点、touch size、overflow、Axe 与 production bundle 不泄漏
fixture。浏览器仅访问本地受控环境，不改线上数据。

### Stream E: Delivery and release evidence

通过 `git merge-base --is-ancestor`、`gh pr view/checks`、`gh run view/list`、release/
tag 和 registry manifest 重新验证历史交付主张。网络状态与本地结果分开记录；旧的
成功 run 不能替代当前 commit 的本地质量门，也不能反过来忽略 protected-main
交付事实。

## 5. Finding model

- **Critical**：安全/权限泄露、数据破坏或伪造、不可恢复丢失、默认可触发的危险
  能力、核心路径无法使用。
- **Important**：已批准行为缺失或明显错误、常见边界回归、可靠性/可访问性问题、
  关键测试或交付证据不足，足以阻止“重构完成”。
- **Minor**：真实但低影响的当前范围缺陷；仍会阻止“无需进一步完善”的无条件
  结论，但与纯偏好、未来假设和已接受延期区分。

每项 finding 先由主会话复现，附绝对文件行号和证据。测试失败若仅由外部环境造成，
记录为 verification blocker 而非代码 finding；在 blocker 消除前同样不能给出
无条件完成结论。

## 6. Evidence and state isolation

- 研究与最终结果写入本任务 `research/`；不改 archived artifacts。
- 测试输出留在系统临时目录或现有 ignored output；不得包含凭据、token、正文、
  文件名或其他 records 内容。
- 不清理用户 branch/worktree/ref/container/data。任务脚本自行创建的临时容器和
  workspace 只按其 trap 正常清理。
- 审查前后记录 `HEAD`、`origin/main`、branch、hooks、Git status 和工具版本。

## 7. Stop and rollback conditions

- 发现当前 base 与远端 main 漂移且影响目标代码：停止混合证据，重新确定审查
  commit。
- 必需 Docker/PostgreSQL/MinIO/Chromium 不可用或 strict script 出现 skip：记录
  blocker，不把未运行写成通过。
- 任何命令意外修改 tracked 产品文件：立即停止，不 reset/checkout 覆盖；报告
  精确路径并等待用户决定。
- 发现产品缺陷：完成证据收集后报告，不在本任务修复。

## 8. Final decision rule

只有 `prd.md` 的 AC-01...AC-10 全部有当前证据、无 verification blocker、无未解决
finding，且边界/限制被准确披露时，才输出“当前已批准范围内重构完成，无需继续
完善或优化”。任何一项不满足，结论必须收窄或否定，不能用历史 CI、总体测试数量
或主观观感覆盖。
