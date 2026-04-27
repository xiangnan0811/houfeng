# Houfeng Object Metadata Editing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add V1 metadata editing for Node/Target labels and notes from list/detail pages without opening generic structural editing.

**Architecture:** Add narrow backend metadata update methods behind `PATCH /api/nodes/:id` and `PATCH /api/targets/:id`, expose typed web helpers, then wire list quick-label editors and detail labels+note editors. Keep runtime/lifecycle/identity fields out of these endpoints.

**Tech Stack:** Go center HTTP/store, PostgreSQL, React/Vite/TypeScript, Vitest, Testing Library

---

## Planned File Structure

### Backend
- Modify: `internal/center/nodes/types.go`
  - Add `UpdateMetadataInput` and repository method.
- Modify: `internal/center/targets/types.go`
  - Add `UpdateMetadataInput` and repository method.
- Modify: `internal/center/store/nodes.go`
  - Add `UpdateNodeMetadata`.
- Modify: `internal/center/store/nodes_test.go`
  - Add SQL shape and not-found tests.
- Modify: `internal/center/store/targets.go`
  - Add `UpdateTargetMetadata`.
- Modify: `internal/center/store/targets_test.go`
  - Add SQL shape and not-found tests.
- Modify: `internal/center/http/handlers/nodes.go`
  - Support `PATCH /api/nodes/:id` for metadata only.
- Modify: `internal/center/http/handlers/nodes_test.go`
  - Add success/invalid/not-found tests.
- Modify: `internal/center/http/handlers/targets.go`
  - Support `PATCH /api/targets/:id` for metadata only.
- Modify: `internal/center/http/handlers/targets_test.go`
  - Add success/invalid/not-found tests.

### Frontend
- Modify: `web/src/lib/types.ts`
  - Add `UpdateNodeMetadataInput`, `UpdateTargetMetadataInput`.
- Modify: `web/src/lib/api.ts`
  - Add `updateNodeMetadata`, `updateTargetMetadata`.
- Modify: `web/src/lib/api.test.ts`
  - Add PATCH helper tests.
- Modify: `web/src/pages/NodesPage.tsx`
  - Add row quick label editor.
- Modify: `web/src/pages/NodesPage.test.tsx`
  - Add quick label edit tests.
- Modify: `web/src/pages/TargetsPage.tsx`
  - Add row quick label editor.
- Modify: `web/src/pages/TargetsPage.test.tsx`
  - Add quick label edit tests.
- Modify: `web/src/pages/NodeDetailPage.tsx`
  - Add labels+note detail editor.
- Modify: `web/src/pages/NodeDetailPage.test.tsx`
  - Add full metadata edit and stale route tests.
- Modify: `web/src/pages/TargetDetailPage.tsx`
  - Add labels+note detail editor.
- Modify: `web/src/pages/TargetDetailPage.test.tsx`
  - Add full metadata edit and stale route tests.

## Shared Semantics

### Input shape

Backend and frontend use:

```ts
type UpdateObjectMetadataInput = {
  labels: string[]
  note: string
}
```

### Normalization

Use this frontend parser in pages that accept comma-separated labels:

```ts
function parseLabels(value: string) {
  const seen = new Set<string>()
  const labels: string[] = []
  for (const raw of value.split(/[,，]/)) {
    const label = raw.trim()
    if (!label || seen.has(label)) continue
    seen.add(label)
    labels.push(label)
  }
  return labels
}
```

Backend metadata normalization must:

- trim labels
- drop empty labels
- de-duplicate while preserving first occurrence order
- trim note
- reject more than 20 labels
- reject any label longer than 64 characters
- reject note longer than 2000 characters

Use the response text `invalid input` for validation failures.

---

### Task 1: Backend Node metadata update

**Files:**
- Modify: `internal/center/nodes/types.go`
- Modify: `internal/center/store/nodes.go`
- Modify: `internal/center/store/nodes_test.go`
- Modify: `internal/center/http/handlers/nodes.go`
- Modify: `internal/center/http/handlers/nodes_test.go`

- [ ] **Step 1: Add failing Node handler tests**

Add tests in `internal/center/http/handlers/nodes_test.go` for:

1. `PATCH /api/nodes/nd_001` accepts `{"labels":[" edge ","core","edge"],"note":" updated "}` and calls repo with normalized labels `["edge","core"]`, note `"updated"`, then returns the updated record.
2. invalid payload with 21 labels returns `400`.
3. repository `nodes.ErrNodeNotFound` returns `404`.

The fake repo must implement:

```go
updateMetadataInput nodes.UpdateMetadataInput
updateMetadataID    string
updateMetadataResult nodes.Record
updateMetadataErr error

func (f *fakeNodeRepository) UpdateNodeMetadata(_ context.Context, nodeID string, input nodes.UpdateMetadataInput) (nodes.Record, error) {
  f.updateMetadataID = nodeID
  f.updateMetadataInput = input
  if f.updateMetadataErr != nil {
    return nodes.Record{}, f.updateMetadataErr
  }
  record := f.updateMetadataResult
  record.NodeID = nodeID
  return record, nil
}
```

Run:

```bash
go test ./internal/center/http/handlers -run 'TestNodeItem.*Metadata|TestNodeItemRejectsInvalidMetadata|TestNodeItemMapsMetadataNotFound' -v
```

Expected: fail because the type/method and PATCH path do not exist.

- [ ] **Step 2: Add failing Node store tests**

Add tests in `internal/center/store/nodes_test.go` that:

- assert `UpdateNodeMetadata` SQL updates `labels`, `note`, and `updated_at`
- returns a scanned full node record
- maps `pgx.ErrNoRows` to `nodes.ErrNodeNotFound`

Run:

```bash
go test ./internal/center/store -run 'TestUpdateNodeMetadata' -v
```

Expected: fail because the store method does not exist.

- [ ] **Step 3: Implement Node domain, handler, and store**

In `internal/center/nodes/types.go`, add:

```go
type UpdateMetadataInput struct {
  Labels []string `json:"labels"`
  Note   string   `json:"note"`
}
```

Extend `Repository` with:

```go
UpdateNodeMetadata(context.Context, string, UpdateMetadataInput) (Record, error)
```

In `handlers/nodes.go`, update `NodeItem` to accept `GET` and `PATCH`. For PATCH:

- decode `nodes.UpdateMetadataInput`
- normalize/validate with helper functions
- call `repo.UpdateNodeMetadata`
- map `nodes.ErrNodeNotFound` to 404
- return updated record as JSON 200

In `store/nodes.go`, implement:

```go
func (r *PostgresNodeRepository) UpdateNodeMetadata(ctx context.Context, nodeID string, input nodes.UpdateMetadataInput) (nodes.Record, error) {
  record, err := scanNode(r.db.QueryRow(ctx, `
    update nodes
    set labels = $2,
        note = $3,
        updated_at = now()
    where node_id = $1
    returning `+nodeSelectColumns, nodeID, input.Labels, input.Note))
  if errors.Is(err, pgx.ErrNoRows) {
    return nodes.Record{}, nodes.ErrNodeNotFound
  }
  if err != nil {
    return nodes.Record{}, fmt.Errorf("update node metadata %q: %w", nodeID, err)
  }
  return record, nil
}
```

- [ ] **Step 4: Run focused Node backend tests and commit**

Run:

```bash
go test ./internal/center/http/handlers -run 'TestNodeItem.*Metadata|TestNodeItemRejectsInvalidMetadata|TestNodeItemMapsMetadataNotFound' -v
go test ./internal/center/store -run 'TestUpdateNodeMetadata' -v
```

Expected: pass.

Commit:

```bash
git add internal/center/nodes/types.go internal/center/http/handlers/nodes.go internal/center/http/handlers/nodes_test.go internal/center/store/nodes.go internal/center/store/nodes_test.go
git commit -m "Allow Node labels and notes to be updated"
```

---

### Task 2: Backend Target metadata update

**Files:**
- Modify: `internal/center/targets/types.go`
- Modify: `internal/center/store/targets.go`
- Modify: `internal/center/store/targets_test.go`
- Modify: `internal/center/http/handlers/targets.go`
- Modify: `internal/center/http/handlers/targets_test.go`

- [ ] **Step 1: Add failing Target handler tests**

Add tests mirroring Task 1:

- `PATCH /api/targets/tg_001` normalizes labels and note, calls `UpdateTargetMetadata`, returns updated record.
- invalid metadata returns 400.
- `targets.ErrTargetNotFound` returns 404.

Run:

```bash
go test ./internal/center/http/handlers -run 'TestTargetItem.*Metadata|TestTargetItemRejectsInvalidMetadata|TestTargetItemMapsMetadataNotFound' -v
```

Expected: fail before implementation.

- [ ] **Step 2: Add failing Target store tests**

Add tests in `internal/center/store/targets_test.go` for SQL shape and `targets.ErrTargetNotFound` mapping.

Run:

```bash
go test ./internal/center/store -run 'TestUpdateTargetMetadata' -v
```

Expected: fail before implementation.

- [ ] **Step 3: Implement Target domain, handler, and store**

In `internal/center/targets/types.go`, add:

```go
type UpdateMetadataInput struct {
  Labels []string `json:"labels"`
  Note   string   `json:"note"`
}
```

Extend `Repository`:

```go
UpdateTargetMetadata(context.Context, string, UpdateMetadataInput) (TargetRecord, error)
```

In `handlers/targets.go`, update `TargetItem` to accept `GET` and `PATCH` with the same normalization/validation semantics as Node.

In `store/targets.go`, implement:

```go
func (r *PostgresTargetRepository) UpdateTargetMetadata(ctx context.Context, targetID string, input targets.UpdateMetadataInput) (targets.TargetRecord, error) {
  record, err := scanTarget(r.db.QueryRow(ctx, `
    update targets
    set labels = $2,
        note = $3,
        updated_at = now()
    where target_id = $1
    returning `+targetSelectColumns, targetID, input.Labels, input.Note))
  if errors.Is(err, pgx.ErrNoRows) {
    return targets.TargetRecord{}, targets.ErrTargetNotFound
  }
  if err != nil {
    return targets.TargetRecord{}, fmt.Errorf("update target metadata %q: %w", targetID, err)
  }
  return record, nil
}
```

- [ ] **Step 4: Run focused Target backend tests and commit**

Run:

```bash
go test ./internal/center/http/handlers -run 'TestTargetItem.*Metadata|TestTargetItemRejectsInvalidMetadata|TestTargetItemMapsMetadataNotFound' -v
go test ./internal/center/store -run 'TestUpdateTargetMetadata' -v
```

Expected: pass.

Commit:

```bash
git add internal/center/targets/types.go internal/center/http/handlers/targets.go internal/center/http/handlers/targets_test.go internal/center/store/targets.go internal/center/store/targets_test.go
git commit -m "Allow Target labels and notes to be updated"
```

---

### Task 3: Frontend API helpers

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`

- [ ] **Step 1: Add failing API helper tests**

Add tests in `web/src/lib/api.test.ts`:

```ts
it('updates node metadata with PATCH /api/nodes/:nodeId', async () => {
  const requestBody = { labels: ['edge', 'core'], note: 'operator note' }
  const responseBody = { /* complete NodeRecord with updated labels/note */ }
  const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
  vi.stubGlobal('fetch', fetchMock)

  await expect(updateNodeMetadata('nd_001', requestBody)).resolves.toEqual(responseBody)
  expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_001', {
    method: 'PATCH',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    body: JSON.stringify(requestBody),
  })
})
```

Add the equivalent target test for `updateTargetMetadata('tg_001', requestBody)`.

Run:

```bash
cd web && npm test -- --run api
```

Expected: fail before helper implementation.

- [ ] **Step 2: Add types and helpers**

In `web/src/lib/types.ts`, add:

```ts
export type UpdateNodeMetadataInput = {
  labels: string[]
  note: string
}

export type UpdateTargetMetadataInput = {
  labels: string[]
  note: string
}
```

In `web/src/lib/api.ts`, import those types and add:

```ts
function patchJSONBody<T>(path: string, body: unknown): Promise<T> {
  return requestJSON<T>(path, {
    method: 'PATCH',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
}

export function updateNodeMetadata(nodeId: string, input: UpdateNodeMetadataInput) {
  return patchJSONBody<NodeRecord>(`/api/nodes/${nodeId}`, input)
}

export function updateTargetMetadata(targetId: string, input: UpdateTargetMetadataInput) {
  return patchJSONBody<TargetRecord>(`/api/targets/${targetId}`, input)
}
```

- [ ] **Step 3: Run API tests and commit**

Run:

```bash
cd web && npm test -- --run api
```

Expected: pass.

Commit:

```bash
git add web/src/lib/types.ts web/src/lib/api.ts web/src/lib/api.test.ts
git commit -m "Expose metadata update helpers to the web UI"
```

---

### Task 4: Node list and detail metadata editing

**Files:**
- Modify: `web/src/pages/NodesPage.tsx`
- Modify: `web/src/pages/NodesPage.test.tsx`
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Modify: `web/src/pages/NodeDetailPage.test.tsx`

- [ ] **Step 1: Add failing Node page tests**

In `NodesPage.test.tsx`, add a test:

- one loaded node with labels `['edge']`, note `'keep me'`
- click `快速编辑标签`
- change `标签` to `edge, core, edge`
- click `保存标签`
- expect PATCH `/api/nodes/nd_001` body `{"labels":["edge","core"],"note":"keep me"}`
- updated row shows `标签：edge · core`
- cancel path does not call PATCH

In `NodeDetailPage.test.tsx`, add tests:

- loaded node shows `标签与备注`, labels, and note
- click `编辑标签与备注`, edit labels and note, save
- expect PATCH body with parsed labels/note
- updated detail shows new labels/note
- failed PATCH shows `标签或备注更新失败`
- stale response after switching node route is ignored

Run:

```bash
cd web && npm test -- --run NodesPage NodeDetailPage
```

Expected: fail before implementation.

- [ ] **Step 2: Implement Node list quick label editor**

In `NodesPage.tsx`:

- import `updateNodeMetadata`
- de-duplicate `parseLabels`
- add row state for open editor, draft label text, busy id, row errors
- render `标签：{formatLabelList(node.labels)}` and `快速编辑标签`
- editor saves `{ labels: parseLabels(draft), note: node.note }`
- success replaces that row
- failure is row-local

- [ ] **Step 3: Implement Node detail labels+note editor**

In `NodeDetailPage.tsx`:

- import `updateNodeMetadata`
- add metadata editor state and route guard using existing refs
- add `标签与备注` detail section
- form uses `标签` and `备注`
- save sends parsed labels and trimmed note
- success updates `state.node`
- failure copy is `标签或备注更新失败`
- stale responses are ignored

- [ ] **Step 4: Run focused Node frontend tests and commit**

Run:

```bash
cd web && npm test -- --run NodesPage NodeDetailPage
cd web && npm run build
```

Expected: pass.

Commit:

```bash
git add web/src/pages/NodesPage.tsx web/src/pages/NodesPage.test.tsx web/src/pages/NodeDetailPage.tsx web/src/pages/NodeDetailPage.test.tsx
git commit -m "Let operators edit Node labels and notes"
```

---

### Task 5: Target list and detail metadata editing

**Files:**
- Modify: `web/src/pages/TargetsPage.tsx`
- Modify: `web/src/pages/TargetsPage.test.tsx`
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.test.tsx`

- [ ] **Step 1: Add failing Target page tests**

In `TargetsPage.test.tsx`, add quick label editor coverage mirroring Node list:

- preserves existing note
- sends PATCH `/api/targets/tg_001`
- updates one row
- failure remains row-local

In `TargetDetailPage.test.tsx`, add full labels+note editor coverage mirroring Node detail:

- section `标签与备注`
- edit/save success
- error stays local
- stale response after route switch ignored

Run:

```bash
cd web && npm test -- --run TargetsPage TargetDetailPage
```

Expected: fail before implementation.

- [ ] **Step 2: Implement Target list quick label editor**

In `TargetsPage.tsx`:

- import `updateTargetMetadata`
- add row quick label editor with same behavior/copy as Node list
- preserve target note on quick-label save

- [ ] **Step 3: Implement Target detail labels+note editor**

In `TargetDetailPage.tsx`:

- import `updateTargetMetadata`
- add `标签与备注` section
- save parsed labels and trimmed note
- update `state.target` on success
- ignore stale route responses

- [ ] **Step 4: Run focused Target frontend tests and commit**

Run:

```bash
cd web && npm test -- --run TargetsPage TargetDetailPage
cd web && npm run build
```

Expected: pass.

Commit:

```bash
git add web/src/pages/TargetsPage.tsx web/src/pages/TargetsPage.test.tsx web/src/pages/TargetDetailPage.tsx web/src/pages/TargetDetailPage.test.tsx
git commit -m "Let operators edit Target labels and notes"
```

---

### Task 6: Verification and review

**Files:**
- No planned edits unless verification exposes issues.

- [ ] **Step 1: Run backend verification**

Run:

```bash
go test ./internal/center/http/handlers -v
go test ./internal/center/store -v
go test ./...
```

Expected: pass.

- [ ] **Step 2: Run frontend verification**

Run:

```bash
cd web && npm test -- --run
cd web && npm run build
cd web && npm run lint
```

Expected: pass.

- [ ] **Step 3: Run repository verification**

Run:

```bash
./scripts/verify.sh
```

Expected: pass.

- [ ] **Step 4: Scope review**

Confirm:

- Metadata endpoints update only labels/note.
- No lifecycle/runtime/identity fields are editable through this slice.
- List quick editors preserve existing notes.
- Detail editors support labels and note.
- Bulk tag editing and tag center were not added.

- [ ] **Step 5: Final code review**

Dispatch a fresh code-review subagent for the whole slice. Fix any issues minimally and rerun focused/full verification.

---

## Self-review

### Spec coverage

- Covers backend metadata update endpoints.
- Covers list quick label editing.
- Covers detail labels+note editing.
- Covers normalization, validation, stale-route, and local error behavior.

### Placeholder scan

- No TBD/TODO placeholders remain.
- Each task names exact files and commands.

### Type consistency

- Backend and frontend use labels/note-only metadata inputs.
- No structural identity fields are included in update payloads.
