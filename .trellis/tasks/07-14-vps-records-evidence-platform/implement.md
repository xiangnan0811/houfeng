# 证据注册表与首批适配器 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`; Codex inline, RED/GREEN only.

**Goal:** 将现有IP质量、监控、事件、订阅预算和命令审计事实固化为可验证、可长期比较的不可变证据快照。

**Architecture:** registry统一kind合同；adapter从权威store读取；capture intent防漂移；logical snapshot/payload分离；Web只渲染allowlisted DTO。

**Tech Stack:** Go/pgx/PostgreSQL、stdlib gzip/SHA、React TypeScript/SVG MetricChart。

---

## Preconditions

- [ ] 子任务1/2已合入main；确认0054可用；读取IP/成本专项spec与backend/web规范。
- [ ] baseline覆盖当前sparkline/IP/cost/event/command API，避免把现有缺陷误当新合同。

## Task 1: Registry、envelope、canonicalization

**Files:** Create `internal/center/evidence/{types,registry,canonical,redaction,conformance}.go` + tests.

- [ ] RED tests定义Kind接口、四类时间、quality/sensitivity、unknown version与禁止字段。
- [ ] 实现确定性encoding/hash、schema field allowlist和registry startup validation。
- [ ] 加fuzz/golden tests；`go test ./internal/center/evidence -run 'Registry|Canonical|Redaction' -count=1` GREEN。

## Task 2: 0054 schema/store与capture intent

**Files:** Create migration; `store/evidence.go`; `evidence/{service,capture}.go`; tests.

- [ ] migration RED断言logical/payload/ref/intent/lineage/TTL/unique，不允许source cascade。
- [ ] 实现preview intent 15m、payload put、logical snapshot与revision participant。
- [ ] real PG tests覆盖drift/rollback/orphan/copy auth/delete；GREEN。

## Task 3: IP与监控时序adapter

**Files:** Create `evidence/adapters/ip_quality.go`,`monitoring.go`; modify/add专用store query tests.

- [ ] RED fixture证明现有sparkline 0-fill/截断不可接受，adapter必须返回actual coverage/buckets/sample counts/gaps。
- [ ] 实现绝对窗口raw/aggregate query、精度/点数上限、IP stale policy与sensitive topology。
- [ ] 30d/partial/backfill/maintenance/retention边界真实PG GREEN。

## Task 4: event、cost、asset-history、command adapter

**Files:** Create `events.go`,`subscription_costs.go`,`asset_history.go`,`command_audits.go` + tests.

- [ ] RED tests固定event/backfill/correction、rate/date/base currency、history event time、command metadata-only。
- [ ] 实现adapter和summary/export DTO；hostile stdout/stderr/details/raw URL corpus命中0。
- [ ] focused source package回归与adapter conformance GREEN。

## Task 5: API/router/bootstrap与deletion/export adapters

**Files:** Create handlers `evidence.go`; create `internal/center/evidence/{deletion_adapter,recovery_adapter,export_adapter}.go` and colocated tests; modify router/bootstrap.

- [ ] handler RED matrix覆盖preview/read、unknown kind、source unstable、preview stale、permission intersection、response allowlist。
- [ ] 实现 `/api/evidence/capture-previews`、`GET /api/evidence/:id`和records save hook。
- [ ] deletion清logical refs/owned snapshots并保留其他copy；export只调用kind.Export。
- [ ] `evidence.NewRecoveryAdapter`重放logical snapshot/payload/intent/source floor与`comparison.result/*` kind，基于恢复后全局引用GC；unknown kind/version失败关闭而非通用JSON。

## Task 6: Web selector与renderer registry

**Files:** Create `pages/records/evidence/EvidenceRendererRegistry.tsx`, kind renderers, `EvidenceCapturePicker.tsx` + tests; extend lazy `web/src/lib/recordsApi.ts` and canonical DTOs in `web/src/lib/types.ts`.

- [ ] RED tests覆盖selector顺序、preview fields/stale、sensitive explicit choice、权威unknown schema fail-closed且payload/metadata不进入普通UI、趋势缺口不连线；external quarantine fallback不在本任务实现。
- [ ] 实现allowlisted renderers并复用MetricChart；禁止`JSON.stringify(payload)` fallback。
- [ ] Vitest/lint/build/bundle/CSS GREEN。

## Task 7: 容量、janitor与完整门

- [ ] 实现evidence独立capacity/alerts、intent/payload orphan janitor与metrics；不受附件quota阻断。
- [ ] 运行determinism/fuzz、race、真实PG、`make verify-go`、Node22 `make verify-web`、`trellis-check`。
- [ ] 更新IP质量/数据库/Web state spec，提交PR/CI；feature仍off。

## Rollback

- 0054 additive；关闭evidence capability，不删除已捕获快照。
- adapter不稳定时只禁用对应kind；权威unknown contract使evidence protected capability失败关闭，外部unsupported metadata只由task10 quarantine拥有，任何普通路径都无通用渲染。
