# Child 5 current-main rebaseline (2026-08-18)

## Baseline

- Protected `origin/main` at `036cc7f5` (`v0.67.0`).
- Children 1–4 and 9 are archived on protected main.
- Root migrations end at `0055_create_record_collaboration.sql`. This child
  adds no root migration. `0056` remains reserved for Child 6.
- User approved Child 5 start after the 2026-08-18 phase review. Stop before
  Child 6 search/center work.

## Reused contracts on this baseline

- Records core already stores raw `BodyMarkdown` plus
  `MarkdownDialectVersionV1`, attachment IDs, and evidence snapshot IDs.
  This child adds the closed document render model, preview/read rendering,
  and workspace UI. It does not invent a second revision transaction.
- `comment_markdown/v1` lives in
  `internal/center/recordcollaboration/comment_markdown.go` and
  `internal/center/recordcollaboration/testdata/comment_markdown_v1.json`.
  Shared-legal sources must produce equivalent trees. Headings, tables,
  task lists, footnotes, blockquotes, thematic breaks, and houfeng refs are
  document-only extensions.
- Child 9 shipped these Web surfaces, not the planned aliases:
  `RecordRevisionCollaborationControls`, `RecordActionPanel`,
  `RecordCommentThread`, `RecordWatchControl`, `RecordCommentMarkdown`.
- `PromoteChecklistActionDialog` does not exist. Child 5 creates it and
  must call Child 9 action commands only after explicit preview/confirm.
- Attachment upload UI currently hosted under `asset-decisions` stays as
  a temporary host. The Records workspace is the only new editor/reader.
  Do not create a second attachment stack.
- `recordsApi.ts` is the lazy transport façade. Page controllers may use
  it; eager shell and `api.ts` must not import it.
- `HOUFENG_RECORDS_ENABLED` remains the process gate. Permanent delete
  stays unsupported. Child 6 still owns `/records` list/sidebar.

## Dispatch

Cursor implements this child directly with Trellis native auto artifacts.
`implement.jsonl` / `check.jsonl` are curated before `task.py start`.
Do not follow the retired Codex-inline header in older drafts.
