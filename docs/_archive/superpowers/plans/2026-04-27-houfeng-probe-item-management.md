# Houfeng ProbeItem Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the V1 Target-detail ProbeItem management loop: edit, enable/disable, and mistaken-creation delete.

**Architecture:** Extend the existing target-scoped ProbeItem collection handler to also handle item paths. Reuse strict ProbeItem config validation for full updates, expose typed frontend helpers, and keep all controls inside `TargetDetailPage` without adding a top-level Probe domain.

**Tech Stack:** Go center HTTP/store, React/Vite/TypeScript, PostgreSQL, Vitest, Testing Library, Go tests

---

## Planned File Structure

- Modify: `internal/center/targets/types.go`
  - Add `ErrProbeItemNotFound`, `UpdateProbeItemInput`, and repository methods.
- Modify: `internal/center/targets/probe_config.go`
  - Add `ValidateUpdateProbeItemInput` reusing the create validation path.
- Modify: `internal/center/store/targets.go`
  - Add `UpdateProbeItem` and `DeleteProbeItem` scoped by target ID + probe item ID.
- Modify: `internal/center/store/targets_test.go`
  - Add SQL-shape and not-found behavior tests for update/delete.
- Modify: `internal/center/http/handlers/targets.go`
  - Support `PUT` and `DELETE` on `/api/targets/:targetId/probe-items/:probeItemId`.
- Modify: `internal/center/http/handlers/targets_test.go`
  - Add handler tests for update/delete success and invalid/not-found cases.
- Modify: `web/src/lib/types.ts`
  - Add `UpdateProbeItemInput`.
- Modify: `web/src/lib/api.ts`
  - Add `updateProbeItem` and `deleteProbeItem`.
- Modify: `web/src/lib/api.test.ts`
  - Lock PUT/DELETE paths and payloads.
- Modify: `web/src/pages/TargetDetailPage.tsx`
  - Add edit/toggle/delete controls inside the ProbeItem section.
- Modify: `web/src/pages/TargetDetailPage.test.tsx`
  - Cover edit prefill/update, enable/disable, delete confirmation/removal, local errors, and stale route mutation safety.

## Shared Shapes

Use full-update semantics for ProbeItem edit and enable/disable. The frontend helper type is intentionally the same shape as create:

```ts
export type UpdateProbeItemInput = CreateProbeItemInput
```

Backend update validation must use the same strict kind-specific config rules as create.

Use this delete confirmation copy exactly:

```ts
const PROBE_DELETE_CONFIRM_MESSAGE = '删除 ProbeItem 会移除这条观测方式，仅应用于误建场景，确定继续吗？'
```

---

### Task 1: Add Backend ProbeItem Item Operations

**Files:**
- Modify: `internal/center/targets/types.go`
- Modify: `internal/center/targets/probe_config.go`
- Modify: `internal/center/store/targets.go`
- Modify: `internal/center/store/targets_test.go`
- Modify: `internal/center/http/handlers/targets.go`
- Modify: `internal/center/http/handlers/targets_test.go`

- [ ] **Step 1: Add failing handler tests for item PUT and DELETE**

In `internal/center/http/handlers/targets_test.go`, extend `fakeTargetRepository` with update/delete fields and methods:

```go
updateProbeItemResult targets.ProbeItemRecord
updateProbeItemErr    error
updateProbeItemInput  targets.UpdateProbeItemInput
updateProbeItemID     string
deleteProbeItemErr    error
deleteProbeItemID     string
```

Add methods:

```go
func (f *fakeTargetRepository) UpdateProbeItem(_ context.Context, targetID string, probeItemID string, input targets.UpdateProbeItemInput) (targets.ProbeItemRecord, error) {
	f.updateProbeItemID = probeItemID
	f.updateProbeItemInput = input
	if f.updateProbeItemErr != nil {
		return targets.ProbeItemRecord{}, f.updateProbeItemErr
	}
	record := f.updateProbeItemResult
	record.TargetID = targetID
	record.ProbeItemID = probeItemID
	return record, nil
}

func (f *fakeTargetRepository) DeleteProbeItem(_ context.Context, _ string, probeItemID string) error {
	f.deleteProbeItemID = probeItemID
	return f.deleteProbeItemErr
}
```

Add success tests:

```go
func TestUpdateProbeItemHandlerReturnsUpdatedRecord(t *testing.T) {
	now := time.Date(2026, time.April, 27, 10, 0, 0, 0, time.UTC)
	repo := &fakeTargetRepository{updateProbeItemResult: targets.ProbeItemRecord{
		ProbeKind:      targets.ProbeKindHTTP,
		Enabled:        false,
		FrequencyTier:  targets.FrequencyTier5m,
		TimeoutSeconds: 8,
		Config:         json.RawMessage(`{"scheme":"https","path":"/ready","method":"HEAD","expected_status_range":[200,204]}`),
		CreatedAt:      now,
		UpdatedAt:      now,
	}}
	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/targets/tg_001/probe-items/pb_001", strings.NewReader(`{"probe_kind":"http","enabled":false,"frequency_tier":"5m","timeout_seconds":8,"config":{"scheme":"https","path":"/ready","method":"HEAD","expected_status_range":[200,204]}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if repo.updateProbeItemID != "pb_001" {
		t.Fatalf("probe item id = %q, want pb_001", repo.updateProbeItemID)
	}
	if repo.updateProbeItemInput.Enabled {
		t.Fatal("expected update input enabled=false")
	}
	var body targets.ProbeItemRecord
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.ProbeItemID != "pb_001" || body.TargetID != "tg_001" || body.Enabled {
		t.Fatalf("body = %#v, want updated scoped probe item", body)
	}
}

func TestDeleteProbeItemHandlerReturnsNoContent(t *testing.T) {
	repo := &fakeTargetRepository{}
	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodDelete, "/api/targets/tg_001/probe-items/pb_001", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
	}
	if repo.deleteProbeItemID != "pb_001" {
		t.Fatalf("probe item id = %q, want pb_001", repo.deleteProbeItemID)
	}
}
```

Add negative tests:

```go
func TestUpdateProbeItemHandlerRejectsInvalidConfig(t *testing.T) {
	repo := &fakeTargetRepository{}
	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/targets/tg_001/probe-items/pb_001", strings.NewReader(`{"probe_kind":"tls","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"port":443}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestProbeItemItemHandlerMapsNotFound(t *testing.T) {
	tests := []struct {
		name string
		method string
		body string
		err error
	}{
		{name: "missing target on update", method: http.MethodPut, body: `{"probe_kind":"tcp","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"port":443}}`, err: targets.ErrTargetNotFound},
		{name: "missing probe on update", method: http.MethodPut, body: `{"probe_kind":"tcp","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"port":443}}`, err: targets.ErrProbeItemNotFound},
		{name: "missing target on delete", method: http.MethodDelete, err: targets.ErrTargetNotFound},
		{name: "missing probe on delete", method: http.MethodDelete, err: targets.ErrProbeItemNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeTargetRepository{updateProbeItemErr: tt.err, deleteProbeItemErr: tt.err}
			handler := handlers.TargetProbeItems(repo)
			req := httptest.NewRequest(tt.method, "/api/targets/tg_missing/probe-items/pb_missing", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
			}
		})
	}
}
```

- [ ] **Step 2: Run handler tests and confirm failure**

Run:

```bash
go test ./internal/center/http/handlers -run 'Test(UpdateProbeItemHandler|DeleteProbeItemHandler|ProbeItemItemHandler)'
```

Expected: fail because repository methods/types and item path handling do not exist.

- [ ] **Step 3: Add domain types and validation**

In `internal/center/targets/types.go`, add:

```go
var ErrProbeItemNotFound = errors.New("probe item not found")

type UpdateProbeItemInput = CreateProbeItemInput
```

Extend `Repository`:

```go
UpdateProbeItem(context.Context, string, string, UpdateProbeItemInput) (ProbeItemRecord, error)
DeleteProbeItem(context.Context, string, string) error
```

In `internal/center/targets/probe_config.go`, add:

```go
func ValidateUpdateProbeItemInput(input UpdateProbeItemInput) (UpdateProbeItemInput, error) {
	validated, err := ValidateCreateProbeItemInput(CreateProbeItemInput(input))
	if err != nil {
		return UpdateProbeItemInput{}, err
	}
	return UpdateProbeItemInput(validated), nil
}
```

- [ ] **Step 4: Implement handler item path behavior**

Replace `targetProbePath` with a shape that returns a probe item ID:

```go
func targetProbePath(path string) (targetID string, probeItemID string, isCollection bool) {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/targets/"), "/"), "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] != "probe-items" {
		return "", "", false
	}
	if len(segments) == 2 {
		return segments[0], "", true
	}
	if len(segments) == 3 && segments[2] != "" {
		return segments[0], segments[2], false
	}
	return segments[0], "", false
}
```

Update `TargetProbeItems`:

```go
targetID, probeItemID, isCollection := targetProbePath(r.URL.Path)
if targetID == "" {
	writeError(w, http.StatusNotFound, "target not found")
	return
}
if !isCollection && probeItemID == "" {
	writeError(w, http.StatusNotFound, "probe item not found")
	return
}
if !isCollection {
	switch r.Method {
	case http.MethodPut:
		var input targets.UpdateProbeItemInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		var err error
		input, err = targets.ValidateUpdateProbeItemInput(input)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		record, err := repo.UpdateProbeItem(r.Context(), targetID, probeItemID, input)
		if errors.Is(err, targets.ErrTargetNotFound) {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}
		if errors.Is(err, targets.ErrProbeItemNotFound) {
			writeError(w, http.StatusNotFound, "probe item not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, record)
	case http.MethodDelete:
		err := repo.DeleteProbeItem(r.Context(), targetID, probeItemID)
		if errors.Is(err, targets.ErrTargetNotFound) {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}
		if errors.Is(err, targets.ErrProbeItemNotFound) {
			writeError(w, http.StatusNotFound, "probe item not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
	return
}
```

Leave existing collection `GET`/`POST` switch unchanged after that item branch.

- [ ] **Step 5: Add store tests and implementation**

Add tests to `internal/center/store/targets_test.go` that assert SQL scoping and error mapping:

```go
func TestUpdateProbeItemScopesByTargetAndProbeItem(t *testing.T) {
	t.Parallel()
	var gotSQL string
	var gotArgs []any
	repo := &PostgresTargetRepository{db: fakeTargetDB{queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
		gotSQL = sql
		gotArgs = append([]any(nil), args...)
		return fakeTargetRow{scan: func(dest ...any) error {
			*(dest[0].(*string)) = "pb_001"
			*(dest[1].(*string)) = "tg_001"
			*(dest[2].(*string)) = targets.ProbeKindTCP
			*(dest[3].(*bool)) = false
			*(dest[4].(*string)) = targets.FrequencyTier5m
			*(dest[5].(*int)) = 7
			*(dest[6].(*json.RawMessage)) = json.RawMessage(`{"port":443}`)
			*(dest[7].(*time.Time)) = time.Date(2026, time.April, 27, 9, 0, 0, 0, time.UTC)
			*(dest[8].(*time.Time)) = time.Date(2026, time.April, 27, 10, 0, 0, 0, time.UTC)
			return nil
		}}
	}}}

	record, err := repo.UpdateProbeItem(context.Background(), "tg_001", "pb_001", targets.UpdateProbeItemInput{ProbeKind: targets.ProbeKindTCP, Enabled: false, FrequencyTier: targets.FrequencyTier5m, TimeoutSeconds: 7, Config: json.RawMessage(`{"port":443}`)})
	if err != nil {
		t.Fatalf("UpdateProbeItem() error = %v", err)
	}
	if record.ProbeItemID != "pb_001" || record.TargetID != "tg_001" || record.Enabled {
		t.Fatalf("record = %#v, want updated scoped probe item", record)
	}
	for _, snippet := range []string{"update probe_items", "where target_id = $1", "probe_item_id = $2", "returning"} {
		if !strings.Contains(gotSQL, snippet) {
			t.Fatalf("SQL missing %q in %q", snippet, gotSQL)
		}
	}
	if len(gotArgs) != 7 || gotArgs[0] != "tg_001" || gotArgs[1] != "pb_001" {
		t.Fatalf("args = %#v, want target/probe ids first", gotArgs)
	}
}

func TestDeleteProbeItemScopesByTargetAndProbeItem(t *testing.T) {
	t.Parallel()
	var gotSQL string
	var gotArgs []any
	repo := &PostgresTargetRepository{db: fakeTargetDB{exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		gotSQL = sql
		gotArgs = append([]any(nil), args...)
		return pgconn.NewCommandTag("DELETE 1"), nil
	}}}

	if err := repo.DeleteProbeItem(context.Background(), "tg_001", "pb_001"); err != nil {
		t.Fatalf("DeleteProbeItem() error = %v", err)
	}
	if !strings.Contains(gotSQL, "delete from probe_items") || !strings.Contains(gotSQL, "target_id = $1") || !strings.Contains(gotSQL, "probe_item_id = $2") {
		t.Fatalf("SQL = %q, want scoped delete", gotSQL)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "tg_001" || gotArgs[1] != "pb_001" {
		t.Fatalf("args = %#v, want target/probe ids", gotArgs)
	}
}
```

Implementation in `internal/center/store/targets.go`:

```go
func (r *PostgresTargetRepository) UpdateProbeItem(ctx context.Context, targetID string, probeItemID string, input targets.UpdateProbeItemInput) (targets.ProbeItemRecord, error) {
	config := input.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	record, err := scanProbeItem(r.db.QueryRow(ctx, `
		update probe_items
		set probe_kind = $3,
			enabled = $4,
			frequency_tier = $5,
			timeout_seconds = $6,
			config = $7::jsonb,
			updated_at = now()
		where target_id = $1
			and probe_item_id = $2
		returning `+probeItemSelectColumns,
		targetID,
		probeItemID,
		input.ProbeKind,
		input.Enabled,
		input.FrequencyTier,
		input.TimeoutSeconds,
		[]byte(config),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, targetErr := r.GetTarget(ctx, targetID); targetErr != nil {
			return targets.ProbeItemRecord{}, targetErr
		}
		return targets.ProbeItemRecord{}, fmt.Errorf("%w: probe item %q under target %q", targets.ErrProbeItemNotFound, probeItemID, targetID)
	}
	if err != nil {
		return targets.ProbeItemRecord{}, fmt.Errorf("update probe item %q for target %q: %w", probeItemID, targetID, err)
	}
	return record, nil
}

func (r *PostgresTargetRepository) DeleteProbeItem(ctx context.Context, targetID string, probeItemID string) error {
	tag, err := r.db.Exec(ctx, `
		delete from probe_items
		where target_id = $1
			and probe_item_id = $2`, targetID, probeItemID)
	if err != nil {
		return fmt.Errorf("delete probe item %q for target %q: %w", probeItemID, targetID, err)
	}
	if tag.RowsAffected() == 0 {
		if _, targetErr := r.GetTarget(ctx, targetID); targetErr != nil {
			return targetErr
		}
		return fmt.Errorf("%w: probe item %q under target %q", targets.ErrProbeItemNotFound, probeItemID, targetID)
	}
	return nil
}
```

- [ ] **Step 6: Run backend tests and commit**

Run:

```bash
go test ./internal/center/targets ./internal/center/store ./internal/center/http/handlers ./internal/center/http
```

Expected: pass.

Commit:

```bash
git add internal/center/targets/types.go internal/center/targets/probe_config.go internal/center/store/targets.go internal/center/store/targets_test.go internal/center/http/handlers/targets.go internal/center/http/handlers/targets_test.go
git commit -m "Add target-scoped ProbeItem item operations"
```

---

### Task 2: Add Typed Web API Helpers

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`

- [ ] **Step 1: Add failing web API tests**

In `web/src/lib/api.test.ts`, import `updateProbeItem`, `deleteProbeItem`, and `UpdateProbeItemInput`.

Add tests after the create ProbeItem helper test:

```ts
it('updates probe items with PUT /api/targets/:targetId/probe-items/:probeItemId', async () => {
  const requestBody = {
    probe_kind: 'http',
    enabled: false,
    frequency_tier: '5m',
    timeout_seconds: 8,
    config: {
      scheme: 'https',
      path: '/ready',
      method: 'HEAD',
      expected_status_range: [200, 204],
    },
  } satisfies UpdateProbeItemInput
  const responseBody = {
    probe_item_id: 'pb_001',
    target_id: 'tg_001',
    ...requestBody,
    created_at: '2026-04-27T09:00:00Z',
    updated_at: '2026-04-27T10:00:00Z',
  } satisfies ProbeItemRecord
  const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
  vi.stubGlobal('fetch', fetchMock)

  await expect(updateProbeItem('tg_001', 'pb_001', requestBody)).resolves.toEqual(responseBody)
  expect(fetchMock).toHaveBeenCalledWith('/api/targets/tg_001/probe-items/pb_001', {
    method: 'PUT',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    body: JSON.stringify(requestBody),
  })
})

it('deletes probe items with DELETE /api/targets/:targetId/probe-items/:probeItemId', async () => {
  const fetchMock = vi.fn().mockResolvedValue(mockResponse(204, ''))
  vi.stubGlobal('fetch', fetchMock)

  await expect(deleteProbeItem('tg_001', 'pb_001')).resolves.toBeUndefined()
  expect(fetchMock).toHaveBeenCalledWith('/api/targets/tg_001/probe-items/pb_001', {
    method: 'DELETE',
    headers: { Accept: 'application/json' },
    cache: 'no-store',
  })
})
```

- [ ] **Step 2: Run focused web API tests and confirm failure**

Run:

```bash
cd web && npm test -- --run api
```

Expected: fail because helpers/types do not exist.

- [ ] **Step 3: Add type and helpers**

In `web/src/lib/types.ts`, add:

```ts
export type UpdateProbeItemInput = CreateProbeItemInput
```

In `web/src/lib/api.ts`, import `UpdateProbeItemInput` and add:

```ts
export function updateProbeItem(
  targetId: string,
  probeItemId: string,
  input: UpdateProbeItemInput,
): Promise<ProbeItemRecord> {
  return requestJSON<ProbeItemRecord>(`/api/targets/${targetId}/probe-items/${probeItemId}`, {
    method: 'PUT',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  })
}

export function deleteProbeItem(targetId: string, probeItemId: string): Promise<void> {
  return requestVoid(`/api/targets/${targetId}/probe-items/${probeItemId}`, {
    method: 'DELETE',
    headers: { Accept: 'application/json' },
  })
}
```

If `requestVoid` does not exist, add it next to `requestJSON`:

```ts
async function requestVoid(path: string, init?: RequestInit): Promise<void> {
  const response = await fetch(path, {
    ...init,
    cache: 'no-store',
  })
  if (!response.ok) {
    throw await buildApiError(response)
  }
}
```

If error parsing is not already factored as `buildApiError`, extract the current `requestJSON` non-ok parsing into a shared helper first.

- [ ] **Step 4: Run focused web API tests and commit**

Run:

```bash
cd web && npm test -- --run api
```

Expected: pass.

Commit:

```bash
git add web/src/lib/types.ts web/src/lib/api.ts web/src/lib/api.test.ts
git commit -m "Expose typed ProbeItem management helpers"
```

---

### Task 3: Add Target Detail ProbeItem Controls

**Files:**
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.test.tsx`

- [ ] **Step 1: Add failing TargetDetailPage tests**

Add tests covering edit, toggle, delete, and errors.

Use existing Target detail test setup. Add this edit test near the ProbeItem creation tests:

```ts
it('edits an existing ProbeItem and replaces the row after save', async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(mockJSONResponse({
      target_id: 'tg_001',
      name: 'Blog',
      target_type: 'service',
      host: 'blog.example.com',
      base_port: 443,
      execution_node_labels: ['edge'],
      run_status: '启用',
      labels: [],
      note: '',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-04-20T00:00:00Z',
      updated_at: '2026-04-24T09:05:00Z',
    }))
    .mockResolvedValueOnce(mockJSONResponse([
      {
        probe_item_id: 'pb_001',
        target_id: 'tg_001',
        probe_kind: 'http',
        enabled: true,
        frequency_tier: '1m',
        timeout_seconds: 5,
        config: { scheme: 'https', path: '/healthz', method: 'GET', expected_status_range: [200, 299] },
        created_at: '2026-04-21T00:00:00Z',
        updated_at: '2026-04-21T00:00:00Z',
      },
    ]))
    .mockResolvedValueOnce(mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse([]))
    .mockResolvedValueOnce(mockJSONResponse({
      probe_item_id: 'pb_001',
      target_id: 'tg_001',
      probe_kind: 'http',
      enabled: true,
      frequency_tier: '5m',
      timeout_seconds: 8,
      config: { scheme: 'https', path: '/ready', method: 'HEAD', expected_status_range: [200, 204] },
      created_at: '2026-04-21T00:00:00Z',
      updated_at: '2026-04-27T10:00:00Z',
    }))
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter initialEntries={['/targets/tg_001']}>
      <Routes>
        <Route path="/targets/:targetId" element={<TargetDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )

  await waitFor(() => expect(screen.getByText('HTTP')).toBeInTheDocument())

  fireEvent.click(screen.getByRole('button', { name: '编辑' }))
  expect(screen.getByRole('heading', { name: '编辑 ProbeItem' })).toBeInTheDocument()
  expect(screen.getByLabelText('HTTP Path')).toHaveValue('/healthz')

  fireEvent.change(screen.getByLabelText('HTTP Path'), { target: { value: '/ready' } })
  fireEvent.change(screen.getByLabelText('HTTP Method'), { target: { value: 'HEAD' } })
  fireEvent.change(screen.getByLabelText('期望状态码终点'), { target: { value: '204' } })
  fireEvent.change(screen.getByLabelText('超时秒数'), { target: { value: '8' } })
  fireEvent.change(screen.getByLabelText('频率档位'), { target: { value: '5m' } })
  fireEvent.click(screen.getByRole('button', { name: '保存 ProbeItem' }))

  await waitFor(() => expect(screen.queryByRole('heading', { name: '编辑 ProbeItem' })).not.toBeInTheDocument())
  expect(screen.getByText('/ready')).toBeInTheDocument()
  expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_001/probe-items/pb_001', {
    method: 'PUT',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    cache: 'no-store',
    body: JSON.stringify({
      probe_kind: 'http',
      enabled: true,
      frequency_tier: '5m',
      timeout_seconds: 8,
      config: { scheme: 'https', path: '/ready', method: 'HEAD', expected_status_range: [200, 204] },
    }),
  })
})
```

Add smaller tests for:

- `停用` sends PUT with the existing config and `enabled: false`, then row badge changes to `停用`.
- `删除` calls `window.confirm` with `PROBE_DELETE_CONFIRM_MESSAGE`, sends DELETE, and removes the row.
- failed PUT/DELETE displays the API error while keeping Target header visible.
- late PUT after switching to another target route does not update the new route.

- [ ] **Step 2: Run focused TargetDetailPage tests and confirm failure**

Run:

```bash
cd web && npm test -- --run TargetDetailPage
```

Expected: fail because management controls do not exist.

- [ ] **Step 3: Add ProbeItem form mode and payload builders**

In `TargetDetailPage.tsx`:

- Import `updateProbeItem`, `deleteProbeItem`, and `UpdateProbeItemInput`.
- Add `PROBE_DELETE_CONFIRM_MESSAGE`.
- Add state:

```ts
type ProbeFormMode =
  | { kind: 'create' }
  | { kind: 'edit'; probeItemId: string }

const [probeFormMode, setProbeFormMode] = useState<ProbeFormMode>({ kind: 'create' })
const [probeMutationError, setProbeMutationError] = useState<string | null>(null)
const [probeMutationBusyId, setProbeMutationBusyId] = useState<string | null>(null)
const probeMutationRequestRef = useRef(0)
```

- Keep `buildProbeCreateInput` and use its return type for update as well:

```ts
function buildProbeUpdateInput(form: ProbeCreateFormState): UpdateProbeItemInput {
  return buildProbeCreateInput(form)
}
```

- Add helpers to convert a `ProbeItemRecord` to form state:

```ts
function formStateForProbeItem(probeItem: ProbeItemRecord): ProbeCreateFormState {
  const config = probeItem.config
  if (probeItem.probe_kind === 'http') {
    const range = Array.isArray(config.expected_status_range) ? config.expected_status_range : []
    return {
      ...initialProbeCreateForm,
      probeKind: 'http',
      enabled: probeItem.enabled,
      frequencyTier: probeItem.frequency_tier,
      timeoutSeconds: String(probeItem.timeout_seconds),
      httpScheme: typeof config.scheme === 'string' ? config.scheme : 'https',
      httpPath: typeof config.path === 'string' ? config.path : '/',
      httpMethod: config.method === 'HEAD' ? 'HEAD' : 'GET',
      expectedStatusStart: String(typeof range[0] === 'number' ? range[0] : 200),
      expectedStatusEnd: String(typeof range[1] === 'number' ? range[1] : 299),
    }
  }
  if (probeItem.probe_kind === 'tls') {
    return {
      ...initialProbeCreateForm,
      probeKind: 'tls',
      enabled: probeItem.enabled,
      frequencyTier: probeItem.frequency_tier,
      timeoutSeconds: String(probeItem.timeout_seconds),
      port: String(typeof config.port === 'number' ? config.port : ''),
      tlsExpiryWarningDays: String(typeof config.expiry_warning_days === 'number' ? config.expiry_warning_days : 14),
    }
  }
  return {
    ...initialProbeCreateForm,
    probeKind: 'tcp',
    enabled: probeItem.enabled,
    frequencyTier: probeItem.frequency_tier,
    timeoutSeconds: String(probeItem.timeout_seconds),
    port: String(typeof config.port === 'number' ? config.port : ''),
  }
}
```

- [ ] **Step 4: Implement edit/toggle/delete handlers**

Add:

```ts
function replaceProbeItem(updated: ProbeItemRecord) {
  setState((current) => ({
    ...current,
    probeItems: current.probeItems.map((item) =>
      item.probe_item_id === updated.probe_item_id ? updated : item,
    ),
  }))
}

function removeProbeItem(probeItemId: string) {
  setState((current) => ({
    ...current,
    probeItems: current.probeItems.filter((item) => item.probe_item_id !== probeItemId),
  }))
}
```

Update submit logic:

```ts
const requestId = probeMutationRequestRef.current + 1
probeMutationRequestRef.current = requestId
const payload = probeFormMode.kind === 'edit' ? buildProbeUpdateInput(probeCreateForm) : buildProbeCreateInput(probeCreateForm)
const createdOrUpdated = probeFormMode.kind === 'edit'
  ? await updateProbeItem(actionTargetId, probeFormMode.probeItemId, payload)
  : await createProbeItem(actionTargetId, payload)
```

After await, guard current route/request before mutation:

```ts
if (!isMountedRef.current || currentRouteTargetIdRef.current !== actionTargetId || currentRequestedTargetIdRef.current !== actionTargetId || probeMutationRequestRef.current !== requestId) return
```

For create append; for edit replace. Close/reset panel.

Add:

```ts
function handleEditProbeItem(probeItem: ProbeItemRecord) {
  probeMutationRequestRef.current += 1
  setProbeMutationError(null)
  setProbeFormMode({ kind: 'edit', probeItemId: probeItem.probe_item_id })
  setProbeCreateForm(formStateForProbeItem(probeItem))
  setProbeCreateOpen(true)
}
```

Add toggle:

```ts
async function handleToggleProbeItem(probeItem: ProbeItemRecord) {
  if (!target) return
  const actionTargetId = target.target_id
  const requestId = probeMutationRequestRef.current + 1
  probeMutationRequestRef.current = requestId
  setProbeMutationBusyId(probeItem.probe_item_id)
  setProbeMutationError(null)
  try {
    const updated = await updateProbeItem(actionTargetId, probeItem.probe_item_id, {
      probe_kind: probeItem.probe_kind,
      enabled: !probeItem.enabled,
      frequency_tier: probeItem.frequency_tier,
      timeout_seconds: probeItem.timeout_seconds,
      config: probeItem.config,
    })
    if (!isMountedRef.current || currentRouteTargetIdRef.current !== actionTargetId || currentRequestedTargetIdRef.current !== actionTargetId || probeMutationRequestRef.current !== requestId) return
    replaceProbeItem(updated)
  } catch (error) {
    if (!isMountedRef.current || currentRouteTargetIdRef.current !== actionTargetId || currentRequestedTargetIdRef.current !== actionTargetId || probeMutationRequestRef.current !== requestId) return
    setProbeMutationError(describeError(error, 'ProbeItem 操作失败'))
  } finally {
    if (isMountedRef.current && currentRouteTargetIdRef.current === actionTargetId && currentRequestedTargetIdRef.current === actionTargetId && probeMutationRequestRef.current === requestId) {
      setProbeMutationBusyId(null)
    }
  }
}
```

Add delete similarly with confirm, `deleteProbeItem`, stale guards, and `removeProbeItem`.

- [ ] **Step 5: Render controls**

In the ProbeItem panel:

- Button that opens create mode must reset `probeFormMode` to `{ kind: 'create' }`.
- Panel title is `创建 ProbeItem` for create and `编辑 ProbeItem` for edit.
- Submit text is `创建 ProbeItem` / `正在创建…` for create and `保存 ProbeItem` / `正在保存…` for edit.
- Render `probeMutationError` near the ProbeItem list.
- Each ProbeItem row renders buttons:

```tsx
<button type="button" onClick={() => handleEditProbeItem(probeItem)}>编辑</button>
<button type="button" disabled={probeMutationBusyId === probeItem.probe_item_id} onClick={() => void handleToggleProbeItem(probeItem)}>
  {probeItem.enabled ? '停用' : '启用'}
</button>
<button type="button" disabled={probeMutationBusyId === probeItem.probe_item_id} onClick={() => void handleDeleteProbeItem(probeItem)}>
  删除
</button>
```

Do not add controls outside Target detail.

- [ ] **Step 6: Run focused TargetDetailPage tests and commit**

Run:

```bash
cd web && npm test -- --run TargetDetailPage
```

Expected: pass.

Commit:

```bash
git add web/src/pages/TargetDetailPage.tsx web/src/pages/TargetDetailPage.test.tsx
git commit -m "Manage ProbeItems from target detail"
```

---

### Task 4: Full Verification and Review

**Files:**
- No planned source edits unless verification exposes a small issue.

- [ ] **Step 1: Run focused checks**

Run:

```bash
go test ./internal/center/targets ./internal/center/store ./internal/center/http/handlers ./internal/center/http
cd web && npm test -- --run api
cd web && npm test -- --run TargetDetailPage
```

Expected: all pass.

- [ ] **Step 2: Run all repository verification**

Run:

```bash
go test ./...
cd web && npm test -- --run
cd web && npm run build
./scripts/verify.sh
```

Expected: all pass.

- [ ] **Step 3: Final scope review**

Confirm:

- ProbeItem management exists only in Target detail.
- No top-level Probe page or navigation was added.
- Delete requires strong confirmation and removes only the selected ProbeItem.
- Enable/disable preserves existing config and only flips `enabled`.
- Edit uses strict kind-specific payloads.
- Existing Target runtime controls and ProbeItem create still work.

Commit only if this review finds a small final fix.

---

## Self-Review

Spec coverage:

- Implements edit, enable, disable, and mistaken-creation delete from the ProbeItem management spec.
- Keeps ProbeItem management target-scoped and structural.
- Preserves strict V1 TCP/HTTP/TLS config schema.

Placeholder scan:

- No TBD/TODO placeholders remain.
- Each task has concrete file paths, tests, commands, and expected outcomes.

Type consistency:

- `UpdateProbeItemInput` aliases `CreateProbeItemInput` in both Go and TypeScript.
- API paths consistently use `/api/targets/:targetId/probe-items/:probeItemId`.
- Frontend helper names match planned usage: `updateProbeItem`, `deleteProbeItem`.
