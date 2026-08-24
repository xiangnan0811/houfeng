# Remediate VPS detail refactor audit findings

## Goal

修复 VPS 详情页完成性审查确认的 4 个 Important 与 1 个 Minor，使新的
`/vps/:id` 概览在动作落地、错误边界、局部 freshness 和 required S3 验证生命周期上
重新满足 current-authority，并在 protected delivery 完成后重新取得“无需继续完善”的
证据。修复必须保持现有权限、非泄露、archive/restore、permanent-delete fail-closed 与
三项接受延期边界。

## Background

- 来源审查：
  `.trellis/tasks/08-23-vps-detail-refactor-completion-audit/research/final-audit-report.md`
  （主 checkout）；冻结产品提交 `08730e7991f3242ed43fcad561cde1f3ea60b6fb`。
- 修复从同一 `origin/main` 提交创建于 branch
  `codex/vps-detail-refactor-remediation`，工作位置
  `.worktree/vps-detail-refactor-remediation`；实现不得进入 local/remote `main`。
- 审查确认：I-01 无效/惰性 overview action 与 relation；I-02 transport/decode/invalid
  DTO 静默选择 legacy；I-03 source budget 级联与 freshness 丢失/伪造；I-04 S3 runner
  teardown false-green；M-01 React Router 7.17.0 production audit 红。
- 相关 baseline focused tests 新鲜通过：Go 3 packages；Web 7 files / 74 tests。该结果只
  证明修复前基线可归因，不是缺陷已经修复。

## Requirements

### R1. Overview actions and navigation must be owned and safe

- 每个 anomaly action 必须落到当前已注册的站内 route，或由当前 page owner 执行的
  command；不得用当前 `/vps/:id` link 冒充 management/retry command。
- relation 只有存在真实、能完成任务的 destination 时才可交互；subscription 使用已注册过滤
  route，monitoring/service/domain 复用现有 VPS-scoped 内容建立 canonical page-owned panel。
- Web 必须按稳定 rule/action/relation token 精确匹配 route 或 command；external、protocol-
  relative、backslash、未知 token 或与 allowlist 不一致的 route 均 fail closed。
- 将 direct production React Router 依赖升级到已修复的 7.18.2，并保持 Data Mode、现有 route
  ordering、keyboard/focus 与 production bundle 合同。

### R2. Capability gate must distinguish feature-off from failures

- 只有经过 runtime validation 的 overview DTO 且明确不含 `records_v2_read`，或当前批准
  的 endpoint-unavailable 信号，才可选择 legacy。
- fetch failure、malformed JSON、`null`、`{}`、缺失必需字段、非法 section/action/relation
  结构及其他 2xx contract drift 必须显示 overview error/retry，不得静默打开 legacy。
- JSON decode 与 DTO validation 的错误必须稳定、安全、可测试，不泄露 response body、
  internal URL、credential 或服务端细节。

### R3. Every overview source must degrade independently and truthfully

- identity 仍是 fatal 404/unauthorized 边界；monitoring、IP、subscription、services、
  domains 与 activity 必须在整体 request budget 内独立、有界，前置慢源不得消耗后续源
  的执行机会。
- monitoring、IP、renewal、recent activity 与每个 relation 都必须携带自己的
  `ready|stale|unavailable`、observed time、last-success time 和安全 reason。
- `next_renew_at` 只表示业务截止时间，不得再作为 source observation/last-success；所有
  success/freshness 时间不得晚于 overview `generated_at`。
- Web 在对应 section/card 本地显示 degraded state、last success/reason 与可工作的 retry；
  用户必须能区分“零/空”与“来源不可用”。

### R4. Required S3 gates must own and prove their lifecycle

- integration 与 recovery S3 runner 不得在 host workspace 留下 root-owned MinIO state。
- runner 必须保留原 suite failure；若 suite 成功但 container/volume/workspace teardown
  失败，则命令必须非零退出。
- 正常成功、suite 失败、cleanup 失败与显式 keep/debug 行为都必须有确定、可测试的资源
  所有权和退出语义；两个 runner 不得漂移。
- required S3 integration/recovery 完整运行后必须证明本次随机 container、volume 与
  workspace 均无残留且没有 skip。

### R5. Verification and delivery

- 每个 child 使用 TDD：先得到能证明原 finding 的 RED，再做最小实现并取得 focused GREEN。
- 执行 exact Go 1.26.2 format/vet/tests、Node 22 Web lint/coverage/build/budgets、完整
  Chromium E2E，以及 Records browser/security/capacity/local+S3 integration/recovery。
- 重跑 production preview 的动作、malformed DTO、局部失败、1440/390、五类写管理面板与三类
  只读 relation panel、keyboard/focus/Axe 检查。
- 通过 feature branch PR 交付，监控 required PR CI、post-merge main CI、Release Please、
  release 与多架构镜像；没有最终 artifact 证据不得宣称完成。

## Child Task Map

1. `08-24-vps-overview-freshness-reliability`：I-03，先建立 read-model/DTO freshness 基础。
2. `08-24-vps-overview-action-contract`：I-01 + M-01，依赖 child 1 的 relation DTO。
3. `08-24-vps-overview-gate-contract`：I-02，可与 child 1 并行，最终按完整 DTO 校验。
4. `08-24-records-s3-harness-lifecycle`：I-04，可独立并行。

Parent 只拥有跨 child integration、全量复核和 protected delivery，不直接承载产品实现。

## Acceptance Criteria

- [x] `AC-01` 所有 anomaly rule 与 relation kind 都有 backend → DTO → Web owner 的表驱动
  测试；真实 route 可达，management/retry 及三类 VPS relation panel 是有 owner 的 command。
- [x] `AC-02` external/protocol-relative/未知 navigation target fail closed；React Router 升级
  后 `npm audit --omit=dev` 不再因 `react-router`/`react-router-dom` 返回 production finding。
- [x] `AC-03` TypeError、SyntaxError、malformed JSON、`null`、`{}` 和结构非法 2xx 都呈现
  overview error/retry；只有 valid capability-off/unavailable contract 选择 legacy。
- [x] `AC-04` 慢 monitoring 不会阻止 IP/renewal/relation/activity 完成；六类局部 source failure 都
  保留独立 section state，且 relation failure 不再伪装为普通零。
- [x] `AC-05` renewal deadline 与 observation/last-success 分离；API 测试证明所有 freshness
  时间 `<= generated_at`，Web 本地显示 state/last-success/reason/retry。
- [x] `AC-06` local/S3 integration 与 local/S3 recovery 功能及 teardown 全部真实通过；成功
  后本次 container、volume、workspace 为零残留，cleanup failure 测试返回非零。
- [x] `AC-07` exact Go 1.26.2、Node 22 Web、Chromium、Records strict gates 无 skip 全通过，
  production preview 与 Axe/390px/keyboard/focus 回归通过。
- [x] `AC-08` 权限、非泄露、permanent-delete fail-closed、ordinary archive/restore 和三项接受
  延期保持不变；没有顺手扩展或不相关重构。
- [x] `AC-09` 独立 `trellis-check` 无未解决 Critical/Important/Minor；原审查 5 项逐条有
  RED→GREEN 与绝对 `file:line` closure 证据。
- [x] `AC-10` feature branch 经 PR/required CI/main CI/release/image protected delivery，最终
  workspace/refs/task artifacts 对账并清理；不得直接修改 local/remote `main`。

## Out of Scope

- 实现或启用单条 Records permanent delete，补齐其 readiness/adapters。
- 提前实现 activity group-granted digest、comparison 390px sticky body rows 或正式
  4 GiB/512 MiB mixed-load harness。
- 改写 unrelated Records 权限、schema、migration、backup format 或 VPS legacy 功能。
- 新的 20 人理解研究、长期 soak、staging/production 业务数据写入。
