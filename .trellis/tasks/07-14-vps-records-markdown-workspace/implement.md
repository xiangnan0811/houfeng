# Markdown 编辑、阅读、差异与材料 Implementation Plan

> **For agentic workers:** Use the native Trellis `trellis-implement` / `trellis-check` workflow, or implement directly on Cursor after `task.py start`. Every dispatch prompt must begin with `Active task: .trellis/tasks/07-14-vps-records-markdown-workspace`. Each bounded slice follows RED -> verified RED -> minimal GREEN -> verified GREEN. Do not start Child 6.

**Goal:** 提供可移植、安全、支持长期修订与故障恢复的全页面Markdown运维记录工作区。

**Architecture:** 服务端和Web共享Markdown v1 corpus；route controller拥有数据/草稿/安全lease；editor/material/diff组件纯受控；正式保存复用records transaction。

**Tech Stack:** Goldmark/Bluemonday、CodeMirror6、react-markdown/remark-gfm/rehype-sanitize、diff、React/Vitest/Playwright。

---

## 2026-08-02 execution override

- 从已接受的 Child 2/3/4/9 protected main 开始，不创建 migration。
- Collaboration migration 为 `0055`；本任务只消费其稳定 API/组件/corpus。
- `comment_markdown/v1` 是完整文档方言的安全子集；复用其共同case与renderer
  contract，不重新实现或迁移评论。
- 不做 old-database/legacy/staging/release work；Node 22 是 Web 验证工具链。

## Preconditions

- [x] 子任务2/3/4/9已合入main；`0055` collaboration API/组件与 `comment_markdown/v1` corpus可用；Node 22.23.1；已读 web component/state/styling/quality 规范。
- [x] Baseline 记录：entry JS gzip `110738`、entry CSS `37125`、max async `32052`。Child 5 不抬 entry/CSS。CodeMirror 因 CSP `style-src 'self'` 放弃；源文用 textarea。lazy `MarkdownPreview` 实测 gzip `48453`（含 render-model 状态提示与共用引用渲染），这是唯一已审计的 max-async 例外。

## Task 1: 跨语言Markdown v1 corpus与服务端render

**Files:** Create `testdata/markdown/houfeng-v1.json`; `internal/center/recordmarkdown/{markdown,render,references}.go` + tests; modify go.mod/sum. `recordmarkdown` owns the document dialect so `records` does not import `recordcollaboration`.

- [x] 先复用Child 9共同case并写文档扩展golden/hostile tests，覆盖GFM/脚注/代码/HTML/XSS/URL/ref/tombstone；共同语义不得漂移。
- [x] 固定 Goldmark 1.8.4 / Bluemonday 1.0.27；`recordmarkdown` 拥有文档方言；原始 HTML 永不透传；GET revision 附带 allowlisted `render_model` 与 `render_model_status`。分工：Goldmark = 源级准入门 `ScanDocumentSource`；块语义由本包扫描器 + 共享区域委派 `ParseSharedMarkdownRegionV1`；Bluemonday 只服务 Child 10 的 `RenderSafeHTML` 导出出口。
- [x] fuzz `FuzzParseDocumentMarkdownV1` 3s GREEN；`go test ./internal/center/recordmarkdown ./internal/center/http/handlers ./internal/center/recordcollaboration` GREEN。

## Task 2: Web依赖与安全preview

**Files:** Modify `web/package*.json`; create `pages/records/editor/{MarkdownSourceEditor,MarkdownPreview}.tsx`、`markdownGolden.test.ts`、`markdownEquivalence.test.tsx`。

- [x] 同一 corpus 的 Vitest golden；安装 `react-markdown@10.1.0` / `remark-gfm@4.0.1` / `rehype-sanitize@6.0.0` / `diff@9.0.0`，lockfile 纳入 diff。
- [x] `markdownEquivalence.test.tsx` 对 corpus 每个可渲染 case 同时跑 render-model 路径与源码回退路径并断言归一化 DOM 一致，防止"服务端 model 与实时渲染说两套话"。
- [x] textarea 受控 source + lazy react-markdown safe preview；houfeng refs 映射 allowlisted `card`；无 `rehype-raw`。CodeMirror 已移除。
- [x] Markdown 依赖只在 lazy `MarkdownPreview` chunk；entry JS `108841 <= 110738`。

## Task 3: Record routes/controller与元数据表单

**Files:** Create four route pages+tests; `pages/records/hooks/useRecordDraft.ts`; editor components; modify router/tests。

- [x] route/controller 覆盖 load/empty/error/revoke、new/edit/revision、static `records/new` 在 `:recordId` 前、Inbox deep link `/records/:id`。
- [x] title/type/impact/subjects/visibility 表单复用 `RecordRevisionCollaborationControls`；`RecordSaveImpact` 列出正式 revision 字段变化。
- [x] 页面不 import `app/layout`，不把状态塞回 `VPSDetailPage`。未新增 `/records` 总入口（Child 6）。

## Task 4: 材料sidebar/drawer与引用往返

**Files:** Create `RecordMaterialDrawer.tsx`,`RecordOutline.tsx`,`RecordSaveImpact.tsx`,`PromoteChecklistActionDialog.tsx` + tests; integrate task3/4 components and task9 `RecordActionPanel`/`RecordCommentThread`/`RecordWatchControl`.

- [x] 材料 drawer / outline / save-impact / promote-checklist 组件测试覆盖 insertion、duplicate/remove、tombstone 与显式提升确认。
- [x] 稳定 `houfeng-ref:v1` token；remove current 不改历史正文。协作区挂载 Child 9 `RecordActionPanel` / `RecordCommentThread` / `RecordWatchControl`；checklist 提升只建 action。
- [x] 组件测试 GREEN；390px 走 focused Playwright `/records/new`，不是 Child 6 列表。

## Task 5: 自动草稿与客户端撤权清理

**Files:** Create `web/src/lib/recordSecurity.ts(.test.ts)`、`pages/records/draftBuffer.ts(.test.ts)`; extend controller tests.

- [x] IndexedDB unsynced buffer、24h TTL、2s idle autosave、logout dispose、pageshow 重读；controller 覆盖 publish/conflict/restore。
- [x] server ETag + unsynced-only buffer；不存 attachment/evidence bytes。
- [x] 真 revoke 才 broadcast；effect cleanup 用 `dispose()`，避免换 epoch 时误踢同记录其它 tab。

## Task 6: Revision diff/conflict/restore

**Files:** Create `RevisionDiff.tsx`,`RecordConflictResolver.tsx` + tests; extend lazy `recordsApi`/controller and canonical DTOs in `web/src/lib/types.ts`.

- [x] `RevisionDiff` / `RecordConflictResolver` 覆盖字段与 Markdown hunks、手动 merge。
- [x] resolver 无明确选择不发送 formal save；409 `server_revision_id` 打开冲突态。
- [x] revision restore 走 `restoreRecordRevision` 创建新 revision；历史页只读。

## Task 7: Artifact视觉与完整门

**Files:** Extend fixture profiles; add focused editor/selector E2E states; styles in existing `legacy-assets.css/page.css/atoms.css` only.

- [x] `/records/new` desktop 1440 + 390 overflow/Axe；未把 `/records` 列入 CORE_ROUTES（Child 6）。触控高度用现有 `Button lg`，不新增 CSS——CSS ratchet 已顶格。
- [x] `recordDetailProfile` 提供含围栏/表格/证据引用的已发布记录，覆盖阅读面 render-model 路径、`编辑/分栏/预览` 切换和含真实材料的抽屉插入，desktop 1440 + 390 + Axe。
- [x] Node 22 lint / coverage / build / CSS analyze GREEN；`make verify-go` GREEN。max-async 仅上调到实测 `MarkdownPreview` `48453`。
- [x] focused Playwright `/records/new` 1440+390+Axe GREEN；`make verify-web` 在本轮收口。不开 Child 6，不提交/不开 PR，除非用户明确要求。

## Rollback

- route flag关闭编辑/阅读UI；raw Markdown/revisions保持可读，不删除数据。
- sanitizer/parser升级只能新增version并保留旧revision renderer，不原地重解释历史。
- 返回更早代码版本时可重建开发数据库，不增加 legacy database adapter。
