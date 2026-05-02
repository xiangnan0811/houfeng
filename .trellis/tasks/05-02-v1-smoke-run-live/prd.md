# Stage 1 P0: V1 fresh-install smoke run on live PostgreSQL

> Stage 1 P0 last item per `docs/release/next-phase-plan.md`. End-to-end V1 smoke against real PG.

## Goal

按 `docs/operations/v1-smoke-run.md` 9 步流程跑端到端 V1 smoke：Node 接入 → Target → ProbeItem → 异常 → 通知 → 恢复，证据归档到 v1-smoke-run.md（新增段或更新 evidence table）。

## What I already know

### 环境（用户已 enable）

- PostgreSQL: `postgres://houfeng:houfeng@192.168.100.192:5432/houfeng?sslmode=disable`
- Center: `http://localhost:8080`（已起，`/api/healthz` 返回 200）
- Web: `http://localhost:5173`（vite dev，200）
- Auth: admin / `Houfeng@123*`

### v1-smoke-run.md 9 步流程

1. Create Node
2. Issue enrollment token
3. Enroll & run agent
4. Create Target
5. Add ProbeItem
6. Trigger incident
7. Recover incident
8. Verify notification record
9. UI verification checkpoints

### 关键约束

- V1.x auth：`/api/*` 除 `/api/healthz` 和 `/api/agent/*` 都需 session cookie；smoke 大部分 curl 需要带 cookie
- agent 在本机跑（127.0.0.1）；smoke target 是 self-probe（target host = 127.0.0.1:8080，probe path = /api/healthz）
- incident sweep interval：`HOUFENG_INCIDENT_SWEEP_INTERVAL` env，默认未知；2026-04-29 上次 smoke 用 5s
- Telegram：除非 user 配 env，否则 Step 8 不发真消息（按上次 smoke 的 evidence，notification 记录仍会生成）

## Decision (ADR-lite) — execution split

**Context**: smoke 是验证 + 文档记录两类工作；按 trellis 应分 agent 类型。

**Decision**: 拆 2 sub-agent：

1. **trellis-research sub-agent** —— 跑 9 步 smoke（curl + bash），收集 evidence，写 `research/v1-smoke-evidence-2026-05-02.md`
   - 含每步 HTTP 响应、IDs、时间戳、错误信息
   - incident sweep 等待用 `sleep` + 多次 poll（不超过 5 min/incident phase）
   - 不改任何 docs

2. **trellis-implement sub-agent** —— 基于 research/* 文件更新 `docs/operations/v1-smoke-run.md`
   - 在 evidence table 加新行（2026-05-02 run）
   - 在文档末尾加 `## 2026-05-02 Live PostgreSQL Smoke Run` 段（含完整 evidence + IDs）
   - 不改 step 1-9 操作描述

3. main agent commit + finish-work

## Requirements

1. trellis-research 跑完整 9 步：
   - Step 1-2: Node + token (HTTP API)
   - Step 3: agent 启动 + 等 enroll → binding 状态变化
   - Step 4-5: Target + ProbeItem (self-probe)
   - Step 6: 等观测 → 故意改 ProbeItem path 触发 incident
   - Step 7: 恢复 ProbeItem path → 等 recovery
   - Step 8: notification-backed event 查询
   - Step 9: UI checkpoint 由 sub-agent 通过 curl + 读 SPA 静态产物验证（不能跑 headless browser）；如不可行则 mark inconclusive

2. evidence 持久化到 `research/v1-smoke-evidence-2026-05-02.md`（每步含：command / response 摘要 / IDs / 时间戳）

3. trellis-implement 更新 `docs/operations/v1-smoke-run.md` evidence table + 末尾加新段

4. 不改 center / agent 二进制 / 业务代码 / 数据库 schema；smoke 是 black-box 验证

## Acceptance Criteria

- [ ] research/v1-smoke-evidence-2026-05-02.md 落地，9 步全部记录
- [ ] docs/operations/v1-smoke-run.md evidence table 加 2026-05-02 行 + 末尾新段
- [ ] Step 6 incident_started 真触发并记录 event_id
- [ ] Step 7 incident_recovered 真触发并记录 event_id
- [ ] Step 8 notification-backed events 查询返回非空（如配 Telegram）或 0 行（如无配，并明示）
- [ ] Step 9 UI checkpoint 至少 mark inconclusive 并说明原因
- [ ] agent 进程在 smoke 完成后 cleanup
- [ ] git diff 范围只在 research/ + docs/operations/v1-smoke-run.md

## Definition of Done

- 端到端真实环境验证通过
- v1-smoke-run.md 含 2026-05-02 evidence
- 不破坏现有数据库（如 PG `houfeng` 库已有数据，smoke 创建的资源用 timestamp 标记便于清理）
- 失败步骤明确记录失败原因，不假装通过

## Out of Scope

- 修复 smoke 中发现的代码 bug（独立 follow-up）
- 跑 headless browser 截图（v2 视觉证据是 post-V1 工作）
- Telegram 真实交付（除非 user 配 env）
- agent 部署到远端机器（仅本机）

## Final Confirmation

**Goal**: 跑端到端 V1 smoke + 落 evidence。
**Approach**: trellis-research 收集 evidence → trellis-implement 写文档 → main commit。
**Plan**: 派 research sub-agent → 读 research 报告 → 派 implement sub-agent → commit + finish-work + parent N/A.
