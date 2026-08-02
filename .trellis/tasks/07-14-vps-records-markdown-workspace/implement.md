# Markdown 编辑、阅读、差异与材料 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`; Codex inline only; RED/GREEN required.

**Goal:** 提供可移植、安全、支持长期修订与故障恢复的全页面Markdown运维记录工作区。

**Architecture:** 服务端和Web共享Markdown v1 corpus；route controller拥有数据/草稿/安全lease；editor/material/diff组件纯受控；正式保存复用records transaction。

**Tech Stack:** Goldmark/Bluemonday、CodeMirror6、react-markdown/remark-gfm/rehype-sanitize、diff、React/Vitest/Playwright。

---

## 2026-08-02 execution override

- 从已接受的 Child 2/3/4/9 protected main 开始，不创建 migration。
- Collaboration migration 为 `0055`；本任务只消费其稳定 API/组件/corpus。
- 不做 old-database/legacy/staging/release work；Node 22 是 Web 验证工具链。

## Preconditions

- [ ] 子任务2/3/4/9已合入main；确认 `0055` collaboration API/组件与comment Markdown corpus可用；Node22 active；读取web component/state/styling/quality规范。
- [ ] 运行Go/Web baseline并记录entry/max-async bundle预算；不得用抬预算解决依赖体积。

## Task 1: 跨语言Markdown v1 corpus与服务端render

**Files:** Create `testdata/markdown/houfeng-v1.json`; `internal/center/records/{markdown,render,references}.go` + tests; modify go.mod/sum.

- [ ] 先写golden/hostile RED tests，覆盖GFM/脚注/代码/HTML/XSS/URL/ref/tombstone。
- [ ] 固定Goldmark/Bluemonday版本并实现parser/sanitizer/render model；原始HTML永不透传。
- [ ] fuzz URL/ref parser并跑Go tests GREEN。

## Task 2: Web依赖与安全preview

**Files:** Modify `web/package*.json`; create `pages/records/editor/{MarkdownSourceEditor,MarkdownPreview}.tsx`、`markdownGolden.test.ts`。

- [ ] 从同一corpus写Vitest RED；安装固定依赖，lockfile纳入diff。
- [ ] 实现CodeMirror受控source和react-markdown safe components；houfeng refs映射allowlisted cards。
- [ ] focused tests/build/bundle GREEN；确认依赖只在lazy chunks。

## Task 3: Record routes/controller与元数据表单

**Files:** Create four route pages+tests; `pages/records/hooks/useRecordDraft.ts`; editor components; modify router/tests。

- [ ] route/controller RED覆盖load/empty/error/revoke、new/template、edit/revision、static ordering和deep link。
- [ ] 实现title/type/status/impact/subjects/visibility表单并复用task9 `RecordCollaborationFields` 承载owner/participants/follow-up；三模式与保存影响摘要必须列出正式revision字段变化。
- [ ] 页面不import app/layout，不把状态塞回VPSDetailPage。

## Task 4: 材料sidebar/drawer与引用往返

**Files:** Create `RecordMaterialDrawer.tsx`,`RecordOutline.tsx`,`RecordSaveImpact.tsx` + tests; integrate task3/4 components and task9 `RecordActionList`/`PromoteChecklistActionDialog`/`RecordComments`/`RecordFollowButton`.

- [ ] RED tests覆盖evidence/attachment insertion、duplicate/remove、unknown/tombstone、action list、显式checklist提升预览/确认、comments/follow、各区局部失败与390px Modal drawer focus。
- [ ] 实现稳定引用token/清单同步；remove current不破坏历史。协作组件继续调用task9 API，checklist提升不自动改Markdown或记录业务状态。
- [ ] desktop/390 component tests GREEN。

## Task 5: 自动草稿与客户端撤权清理

**Files:** Create `web/src/lib/recordSecurity.ts(.test.ts)`、`pages/records/draftBuffer.ts(.test.ts)`; extend controller tests.

- [ ] fake timers/IndexedDB RED覆盖2s idle、offline、24h、logout/user switch/sync/discard、multi-tab revoke、background pageshow。
- [ ] 实现server ETag与unsynced-only buffer；不存attachment/evidence bytes。
- [ ] revoke abort fetch/autosave，先渲染empty shell再清state/objectURL/BroadcastChannel ack。

## Task 6: Revision diff/conflict/restore

**Files:** Create `RevisionDiff.tsx`,`RecordConflictResolver.tsx` + tests; extend lazy `recordsApi`/controller and canonical DTOs in `web/src/lib/types.ts`.

- [ ] RED tests覆盖base/local/server字段和Markdown hunks、manual merge、server再次推进。
- [ ] 实现resolver；无明确选择不发送formal save。
- [ ] revision restore创建新revision，历史只读。

## Task 7: Artifact视觉与完整门

**Files:** Extend fixture profiles; add focused editor/selector E2E states; styles in existing `legacy-assets.css/page.css/atoms.css` only.

- [ ] 六状态×desktop/390的editor/selector fixtures逐步RED→GREEN；Axe/keyboard/focus/44px/overflow。
- [ ] 运行Node22 coverage/lint/build/bundle/CSS、focused Playwright、Go render tests、`make verify-web`/`make verify-go`。
- [ ] `trellis-check`、spec update、PR/CI；records总入口仍由feature gate控制。

## Rollback

- route flag关闭编辑/阅读UI；raw Markdown/revisions保持可读，不删除数据。
- sanitizer/parser升级只能新增version并保留旧revision renderer，不原地重解释历史。
- 返回更早代码版本时可重建开发数据库，不增加 legacy database adapter。
