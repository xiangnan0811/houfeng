# Houfeng Object Metadata Editing Design

## Context

Frozen V1 includes a lightweight tag-editing flow:

- list pages support quick tag corrections
- detail pages support fuller tag and note cleanup
- tags remain an auxiliary management attribute, not an independent product domain

The current implementation only accepts `labels` and `note` during Node/Target creation. After creation, operators can see labels in detail pages, but cannot correct labels or notes. This leaves V1 object organization incomplete and makes settings override rules based on labels harder to operate safely.

## Scope

Implement metadata editing for Nodes and Targets:

1. Backend update endpoints for labels and note.
2. Typed frontend API helpers.
3. Node list quick label editing.
4. Target list quick label editing.
5. Node detail full labels + note editing.
6. Target detail full labels + note editing.

Out of scope:

- editing Node identity fields (`display_name`, region, city, provider)
- editing Target identity/execution fields (`name`, type, host, port, execution node labels)
- lifecycle/runtime changes through metadata endpoints
- bulk tag editing
- a standalone tag center

## Chosen approach

Use narrow metadata update endpoints on the existing item resources:

- `PATCH /api/nodes/:nodeId`
- `PATCH /api/targets/:targetId`

Both accept only:

```json
{
  "labels": ["edge", "core"],
  "note": "operator note"
}
```

Why:

- The endpoint is intentionally scoped to metadata and does not become generic structural editing.
- It reuses existing item routing and returns the full updated record.
- It keeps the frontend update path simple for both list quick edits and detail full edits.

## Normalization semantics

Backend and frontend should use the same user-facing behavior:

- trim labels
- split frontend label entry on `,` or `，`
- drop empty labels
- de-duplicate labels while preserving first occurrence order
- trim `note`
- allow empty labels
- allow empty note

Backend validation should enforce a small safety boundary:

- reject more than 20 labels
- reject labels longer than 64 characters
- reject note longer than 2000 characters

## Frontend behavior

### List quick label editing

On `NodesPage` and `TargetsPage`:

- show current object labels in each row with `标签：...`
- show `快速编辑标签`
- clicking opens an inline compact panel in that row
- panel contains one text input labeled `标签`
- save button label: `保存标签`
- cancel button label: `取消`
- save sends existing note unchanged with updated labels
- cancel closes without API call
- success replaces that row with the updated record
- failure stays local to the row
- stale row responses after switching/closing should be ignored where current page patterns already support it

### Detail full metadata editing

On `NodeDetailPage` and `TargetDetailPage`:

- show a `标签与备注` detail section
- display current labels and note
- button label: `编辑标签与备注`
- form fields:
  - `标签`
  - `备注`
- save button label: `保存标签与备注`
- cancel button label: `取消`
- save sends labels and note
- success updates the loaded object and closes the editor
- failure stays local to the section
- stale responses after route switch are ignored using existing route guards

## Copy

Use these labels exactly:

- `标签`
- `标签：`
- `快速编辑标签`
- `保存标签`
- `标签与备注`
- `编辑标签与备注`
- `保存标签与备注`
- `备注`
- `暂无备注`
- `标签或备注更新失败`

## Testing strategy

### Backend

- Node handler accepts `PATCH /api/nodes/:id` and returns updated record.
- Target handler accepts `PATCH /api/targets/:id` and returns updated record.
- invalid label/note payloads return 400.
- missing Node/Target maps to 404.
- stores update only `labels`, `note`, and `updated_at`, returning the full record.

### Frontend

- API helper tests lock `PATCH` paths and JSON bodies.
- Node list quick label edit updates one row and preserves note.
- Target list quick label edit updates one row and preserves note.
- Node detail label/note edit updates the detail hero/section and ignores stale route responses.
- Target detail label/note edit updates the detail hero/section and ignores stale route responses.
- validation errors stay local.

## Self-review

- This implements the frozen V1 metadata organization flow without expanding into identity editing.
- The endpoint shape is narrow and reversible.
- Bulk edits and standalone tag center remain intentionally out of scope.
