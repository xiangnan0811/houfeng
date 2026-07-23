# 搜索、记录中心与全局搜索 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`; Codex inline; RED/GREEN mandatory.

**Goal:** 交付权限安全、可深链、可扩展到长期Markdown与多revision的项目级记录检索体验。

**Architecture:** PostgreSQL projection+pg_trgm/tsvector；revision transaction同步current document，worker重建history；server cursor固定查询/auth/watermark；RecordsPage组合route-private query UI。

**Tech Stack:** PostgreSQL pg_trgm/GIN、Go/pgx与stdlib AES-GCM、React/Vitest/Playwright。

---

## Preconditions

- [ ] 子任务1/2/5/9已合入main；记录0051–0054与0056文件hash为不可变并确认0055可用。若任务外主线占用0055，只顺延search与尚未发布0057–0060并同步父/child tests，禁止修改0051–0054/0056。Node22；记录当前GlobalSearch全量拉取baseline与查询计划。

## Task 1: Search schema/plaintext/cursor domain

**Files:** Create migration; `internal/center/recordsearch/{types,normalize}.go` + tests；create `internal/center/recordcursor/{codec,keyring}.go` + tests；modify typed config、`.env.example` 与部署文档。

- [ ] RED tests覆盖Unicode/中文/code/literal wildcard、scope/filter、cursor canonical digest/auth/watermark/tie-breaker，以及AES-GCM nonce/padding、purpose混用、token plaintext扫描、篡改、1h expiry、2m skew、两replica decrypt-set预分发/current切换/旧key退休、未知或撤销key和0400 owner/mode；仅签名可解码JSON必须失败。
- [ ] 实现normalized plaintext与共享`recordcursor` confidential codec；token只作为不可比较字符串，损坏/mixed query/purpose/key拒绝，key/token/plaintext不进日志。
- [ ] migration source/PG tests覆盖0051–0054后应用0055、以及0051–0054/0056已记录后补应用0055，两条路径repeat apply均GREEN；0055不得修改0056对象。

## Task 2: Transaction projector与rebuild worker

**Files:** Create `recordsearch/projector.go`,`worker.go`,`recovery_adapter.go`; store `record_search.go`; tests including `recovery_adapter_test.go`; wire records participant/bootstrap.

- [ ] RED tests固定revision commit同tx更新current document、rollback、archive/restore/delete、lag/rebuild hash。
- [ ] 实现projector/rebuild lease；history/archive只在显式scope维护。
- [ ] `recordsearch.NewRecoveryAdapter`只清空/重建search projection、watermark和cache，不读取恢复出的旧snippet冒充权威；unknown projection contract拒绝。
- [ ] 真实PG并发/rebuild tests GREEN。

## Task 3: Server query/EXPLAIN/handler

**Files:** Create `recordsearch/query.go`; handler `record_search.go`或records collection query; tests; router/bootstrap.

- [ ] SQL fake+real RED tests覆盖all filters OR/AND、EXISTS、auth-first、rank/order、limit+1、query count。
- [ ] 实现`GET /api/records`与全局摘要 endpoint；response allowlist/snippet escape。
- [ ] seed 10k/200k，EXPLAIN与p95 gate GREEN；不靠N+1。

## Task 4: recordsApi/query codec

**Files:** Extend lazy `web/src/lib/recordsApi.ts` and canonical `web/src/lib/types.ts`; create `pages/records/query/recordQueryState.ts(.test.ts)`.

- [ ] RED tests覆盖default omission、canonical URL、invalid recovery、multi-value OR、follow-up/action due分离、cursor append。
- [ ] 实现pure codec和Abort/latest request guard。

## Task 5: Records/Drafts pages

**Files:** Create `RecordsPage.tsx(.test.tsx)`,`RecordDraftsPage.tsx(.test.tsx)`、FilterPanel/Drawer/ResultsTable components; modify router/sidebar/breadcrumb/topbar tests.

- [ ] Page RED matrix覆盖首次空/无结果/load/error/append/revoke、dynamic follow-up chips、no-status row、named scroll/390drawer。
- [ ] 实现高密度list与稳定pagination；Sidebar入口feature gated。
- [ ] Artifact v1 record-center desktop/390 tests GREEN。

## Task 6: GlobalSearch server record results

**Files:** Modify `GlobalSearch.tsx(.test.tsx)`、API/types。

- [ ] RED tests证明不拉record全文/全量列表，权限/abort/latest/query clear正确。
- [ ] 实现≤5摘要+全部入口；现有资产结果行为回归GREEN。

## Task 7: 浏览器/性能/完整门

- [ ] 扩fixtures与record center states、Axe/keyboard/390/overflow；focused Playwright GREEN。
- [ ] PG seed/EXPLAIN、race、`make verify-go`、Node22 `make verify-web`、E2E、`trellis-check`。
- [ ] spec更新、PR/CI；feature仍不默认开放。

## Rollback

- search projection可丢弃重建；关闭records search route不影响revision权威。
- 禁止用旧GlobalSearch浏览器全文过滤作为回退记录实现。
