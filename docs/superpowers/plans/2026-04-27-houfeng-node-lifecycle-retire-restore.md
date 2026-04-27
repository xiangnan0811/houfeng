# Houfeng Node Lifecycle Retire / Restore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add V1 Node lifecycle retirement and restore-to-observing controls without changing the frozen product scope.

**Architecture:** Add explicit lifecycle transition routes separate from runtime monitoring controls, persist lifecycle state changes in `nodes`, append additive `state_change_events`, then expose detail-page-only Node lifecycle actions in React. The UI uses an inline confirmation panel for retirement and keeps errors local to the lifecycle card.

**Tech Stack:** Go center API/store/router, React/Vite/TypeScript frontend, Testing Library, Vitest, PostgreSQL schema already containing `nodes.lifecycle_status`.

---

## Planned File Structure

- Modify: `internal/center/incidents/types.go`
  - Add node lifecycle event type constants.
- Modify: `internal/center/store/nodes.go`
  - Add lifecycle transition methods and lifecycle event insertion.
- Modify: `internal/center/store/nodes_test.go`
  - Add SQL/event tests for retire and restore transition constraints.
- Modify: `internal/center/http/handlers/runtime_controls.go`
  - Add a dedicated Node lifecycle handler next to runtime controls.
- Modify: `internal/center/http/handlers/runtime_controls_test.go`
  - Add handler dispatch/error tests for lifecycle actions.
- Modify: `internal/center/http/router.go`
  - Route `/api/nodes/{node_id}/lifecycle/{action}` to the lifecycle handler.
- Modify: `internal/center/http/router_api_test.go`
  - Ensure lifecycle API routes do not fall back to the SPA.
- Modify: `cmd/houfeng-center/bootstrap.go`
  - Wire the lifecycle handler.
- Modify: `cmd/houfeng-center/bootstrap_test.go`
  - Assert lifecycle handler wiring exists.
- Modify: `web/src/lib/api.ts`
  - Add `retireNode` and `restoreRetiredNodeToObserving`.
- Modify: `web/src/lib/api.test.ts`
  - Assert lifecycle helpers call explicit endpoints.
- Modify: `web/src/lib/types.ts`
  - Add lifecycle event type labels.
- Modify: `web/src/components/EventList.test.tsx`
  - Cover readable lifecycle event labels.
- Modify: `web/src/pages/EventsPage.test.tsx`
  - Cover lifecycle event filter option.
- Modify: `web/src/pages/NodeDetailPage.tsx`
  - Add lifecycle card, inline retirement confirmation, restore action, local errors, stale-response guard.
- Modify: `web/src/pages/NodeDetailPage.test.tsx`
  - Cover retire/restore/error/stale behavior.

No schema migration is needed because `nodes.lifecycle_status` already exists.

## Shared Constants

Use these route paths:

```text
POST /api/nodes/{node_id}/lifecycle/retire
POST /api/nodes/{node_id}/lifecycle/restore-to-observing
```

Use these event types and labels:

```text
node_retired -> 节点已退役
node_restored_to_observing -> 节点恢复到观察中
```

Use these user-facing labels/copy:

```text
退役节点
确认退役
取消
恢复到观察中
节点生命周期操作失败
已退役节点在 V1 中只能先恢复到观察中，不能直接恢复为在用。
```

---

### Task 1: Backend lifecycle transition API

**Files:**
- Modify: `internal/center/incidents/types.go`
- Modify: `internal/center/store/nodes.go`
- Modify: `internal/center/store/nodes_test.go`
- Modify: `internal/center/http/handlers/runtime_controls.go`
- Modify: `internal/center/http/handlers/runtime_controls_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_api_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [x] **Step 1: Add failing backend store tests**

In `internal/center/store/nodes_test.go`, add tests near the existing node runtime control tests:

```go
func TestNodeLifecycleTransitionsWriteEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		method           func(*PostgresNodeRepository, context.Context, string) (nodes.Record, error)
		initialLifecycle string
		returnLifecycle  string
		wantEventType    incidents.EventType
		wantSummary      string
		wantSQLSnippet   string
	}{
		{
			name:             "retire",
			method:           (*PostgresNodeRepository).RetireNode,
			initialLifecycle: nodes.LifecycleInUse,
			returnLifecycle:  nodes.LifecycleRetired,
			wantEventType:    incidents.EventNodeRetired,
			wantSummary:      "节点已退役并退出活跃舰队，历史记录保留",
			wantSQLSnippet:   "lifecycle_status <> '已退役'",
		},
		{
			name:             "restore retired to observing",
			method:           (*PostgresNodeRepository).RestoreRetiredNodeToObserving,
			initialLifecycle: nodes.LifecycleRetired,
			returnLifecycle:  nodes.LifecycleObserving,
			wantEventType:    incidents.EventNodeRestoredToObserving,
			wantSummary:      "节点已从退役恢复到观察中",
			wantSQLSnippet:   "lifecycle_status = '已退役'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotSQL string
			var execSQL string
			var execArgs []any
			committed := false
			repo := &PostgresNodeRepository{db: fakeNodeDB{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
					return fakeNodeTx{
						queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
							gotSQL = sql
							if len(args) != 1 || args[0] != "nd_001" {
								t.Fatalf("QueryRow args = %#v, want node id", args)
							}
							return fakeNodeRow{scan: func(dest ...any) error {
								scanNodeRecordDestinations(dest, nodes.Record{
									NodeID:          "nd_001",
									DisplayName:     "Tokyo Edge",
									LifecycleStatus: tt.returnLifecycle,
									MonitoringStatus: nodes.MonitoringEnabled,
									BindingStatus:    nodes.BindingBound,
								})
								return nil
							}}
						},
						exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
							execSQL = sql
							execArgs = append([]any(nil), args...)
							return pgconn.NewCommandTag("INSERT 0 1"), nil
						},
						commit: func(context.Context) error {
							committed = true
							return nil
						},
					}, nil
				},
			}}

			got, err := tt.method(repo, context.Background(), "nd_001")
			if err != nil {
				t.Fatalf("%s error = %v", tt.name, err)
			}
			if got.LifecycleStatus != tt.returnLifecycle {
				t.Fatalf("LifecycleStatus = %q, want %q", got.LifecycleStatus, tt.returnLifecycle)
			}
			if !strings.Contains(gotSQL, tt.wantSQLSnippet) {
				t.Fatalf("transition SQL = %q, want snippet %q", gotSQL, tt.wantSQLSnippet)
			}
			if !strings.Contains(execSQL, "insert into state_change_events") {
				t.Fatalf("event SQL = %q, want state_change_events insert", execSQL)
			}
			if len(execArgs) < 8 {
				t.Fatalf("event args = %#v, want full insert args", execArgs)
			}
			if execArgs[1] != string(incidents.ObjectTypeNode) {
				t.Fatalf("event object_type arg = %#v, want node", execArgs[1])
			}
			if execArgs[2] != "nd_001" {
				t.Fatalf("event object_id arg = %#v, want nd_001", execArgs[2])
			}
			if execArgs[3] != string(tt.wantEventType) {
				t.Fatalf("event type arg = %#v, want %q", execArgs[3], tt.wantEventType)
			}
			if execArgs[5] != tt.wantSummary {
				t.Fatalf("summary arg = %#v, want %q", execArgs[5], tt.wantSummary)
			}
			payload, ok := execArgs[6].([]byte)
			if !ok || !strings.Contains(string(payload), `"lifecycle_status":"`+tt.returnLifecycle+`"`) {
				t.Fatalf("payload arg = %#v, want lifecycle status", execArgs[6])
			}
			if !committed {
				t.Fatal("transaction was not committed")
			}
		})
	}
}

func TestNodeLifecycleRejectsInvalidTransition(t *testing.T) {
	t.Parallel()

	repo := &PostgresNodeRepository{db: fakeNodeDB{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return fakeNodeTx{
				queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeNodeRow{scanErr: pgx.ErrNoRows}
				},
			}, nil
		},
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeNodeRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				return nil
			}}
		},
	}}

	_, err := repo.RetireNode(context.Background(), "nd_001")
	if !errors.Is(err, ErrInvalidNodeRuntimeTransition) {
		t.Fatalf("RetireNode() error = %v, want ErrInvalidNodeRuntimeTransition", err)
	}
}
```

Run:

```bash
go test ./internal/center/store
```

Expected: fail because lifecycle methods and event constants do not exist.

- [x] **Step 2: Implement backend store lifecycle transitions**

In `internal/center/incidents/types.go`, add:

```go
EventNodeRetired             EventType = "node_retired"
EventNodeRestoredToObserving EventType = "node_restored_to_observing"
```

In `internal/center/store/nodes.go`, add `insertNodeLifecycleEvent`, `RetireNode`, and `RestoreRetiredNodeToObserving` near runtime controls. Use transactions, `nodeExists`, `nodes.ErrNodeNotFound`, and `ErrInvalidNodeRuntimeTransition` exactly as runtime controls do.

Transition SQL requirements:

```sql
update nodes
set lifecycle_status = '已退役',
    updated_at = now()
where node_id = $1
  and lifecycle_status <> '已退役'
returning ...
```

```sql
update nodes
set lifecycle_status = '观察中',
    updated_at = now()
where node_id = $1
  and lifecycle_status = '已退役'
returning ...
```

Event payload:

```go
json.Marshal(map[string]string{"lifecycle_status": record.LifecycleStatus})
```

- [x] **Step 3: Add failing lifecycle handler/router/bootstrap tests**

Extend `internal/center/http/handlers/runtime_controls_test.go`:

- Add fake repo fields and methods:
  - `retireNodeID`, `retireResult`, `retireErr`
  - `restoreToObservingNodeID`, `restoreToObservingResult`, `restoreToObservingErr`
  - `RetireNode`
  - `RestoreRetiredNodeToObserving`
- Add `TestNodeLifecycleControlHandlerReturnsUpdatedNode`.
- Add `TestNodeLifecycleControlHandlerMapsErrors`.
- Add method rejection coverage for lifecycle handler.

Extend `internal/center/http/router_api_test.go` with:

```go
func TestRouterKeepsNodeLifecycleControlRoutesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		NodeLifecycleControlHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"node_id":"nd_001","lifecycle_status":"已退役"}`))
		}),
	})

	for _, path := range []string{
		"/api/nodes/nd_001/lifecycle/retire",
		"/api/nodes/nd_001/lifecycle/restore-to-observing",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusOK)
		}
		if strings.TrimSpace(recorder.Body.String()) == spaShell {
			t.Fatalf("%s returned SPA fallback body %q", path, recorder.Body.String())
		}
	}
}
```

Extend `cmd/houfeng-center/bootstrap_test.go`:

```go
if gotOpts.NodeLifecycleControlHandler == nil {
	t.Fatal("router node lifecycle control handler = nil, want non-nil")
}
```

Run:

```bash
go test ./internal/center/http/handlers ./internal/center/http ./cmd/houfeng-center
```

Expected: fail because the lifecycle handler/router option is not wired.

- [x] **Step 4: Implement lifecycle handler, router subtree, and bootstrap wiring**

In `internal/center/http/handlers/runtime_controls.go`, add:

```go
type nodeLifecycleControlRepository interface {
	RetireNode(context.Context, string) (nodes.Record, error)
	RestoreRetiredNodeToObserving(context.Context, string) (nodes.Record, error)
}
```

Add `NodeLifecycleControls(repo nodeLifecycleControlRepository) http.Handler` with actions:

- `retire` → `repo.RetireNode`
- `restore-to-observing` → `repo.RestoreRetiredNodeToObserving`

Map errors:

- `nodes.ErrNodeNotFound` → 404 `node not found`
- `store.ErrInvalidNodeRuntimeTransition` → 409 `invalid lifecycle transition`
- unknown → 500

In `internal/center/http/router.go`, add `NodeLifecycleControlHandler` to `RouterOptions`, a `nodeSubtreeLifecycleControl` subtree, parser support for `/api/nodes/{id}/lifecycle/{action}`, and dispatch in `New`.

In `cmd/houfeng-center/bootstrap.go`, wire:

```go
NodeLifecycleControlHandler: handlers.NodeLifecycleControls(nodeRepo),
```

- [x] **Step 5: Run focused backend tests and commit**

Run:

```bash
go test ./internal/center/store ./internal/center/http/handlers ./internal/center/http ./cmd/houfeng-center
```

Expected: pass.

Commit:

```bash
git add internal/center/incidents/types.go internal/center/store/nodes.go internal/center/store/nodes_test.go internal/center/http/handlers/runtime_controls.go internal/center/http/handlers/runtime_controls_test.go internal/center/http/router.go internal/center/http/router_api_test.go cmd/houfeng-center/bootstrap.go cmd/houfeng-center/bootstrap_test.go
git commit -m "Add explicit Node lifecycle transitions"
```

---

### Task 2: Frontend API helpers and event labels

**Files:**
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/components/EventList.test.tsx`
- Modify: `web/src/pages/EventsPage.test.tsx`

- [x] **Step 1: Add failing frontend API/event label tests**

In `web/src/lib/api.test.ts`, extend imports with `retireNode` and `restoreRetiredNodeToObserving`, then extend the node runtime helper test or add a new test:

```ts
await expect(retireNode('nd_001')).resolves.toEqual(responseBody)
await expect(restoreRetiredNodeToObserving('nd_001')).resolves.toEqual(responseBody)

expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes/nd_001/lifecycle/retire', {
  method: 'POST',
  headers: { Accept: 'application/json' },
  cache: 'no-store',
})
expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/lifecycle/restore-to-observing', {
  method: 'POST',
  headers: { Accept: 'application/json' },
  cache: 'no-store',
})
```

In `web/src/components/EventList.test.tsx`, add a lifecycle event test:

```tsx
it('renders node lifecycle event labels without incident-only meta rows', () => {
  render(
    <EventList
      events={[
        {
          event_id: 'evt_node_retired',
          incident_id: '',
          incident_class: '',
          object_type: 'node',
          object_id: 'nd_001',
          event_type: 'node_retired',
          severity: '',
          summary: '节点已退役并退出活跃舰队，历史记录保留',
          created_at: '2026-04-27T08:10:00Z',
        },
        {
          event_id: 'evt_node_restored',
          incident_id: '',
          incident_class: '',
          object_type: 'node',
          object_id: 'nd_001',
          event_type: 'node_restored_to_observing',
          severity: '',
          summary: '节点已从退役恢复到观察中',
          created_at: '2026-04-27T08:20:00Z',
        },
      ]}
    />,
  )

  expect(screen.getByRole('heading', { level: 3, name: '节点已退役' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { level: 3, name: '节点恢复到观察中' })).toBeInTheDocument()
  expect(screen.queryByText('异常类型')).not.toBeInTheDocument()
})
```

In `web/src/pages/EventsPage.test.tsx`, extend the runtime-control event filter test to assert options for `节点已退役` and `节点恢复到观察中`, then select `node_retired` and assert:

```ts
expect(fetchMock).toHaveBeenLastCalledWith('/api/events?event_type=node_retired&limit=50', {
  headers: { Accept: 'application/json' },
  cache: 'no-store',
})
```

Run:

```bash
cd web && npm test -- --run api EventList EventsPage
```

Expected: fail because helpers/types are missing.

- [x] **Step 2: Implement frontend helpers and labels**

In `web/src/lib/api.ts`, add:

```ts
export function retireNode(nodeId: string) {
  return postJSON<NodeRecord>(`/api/nodes/${nodeId}/lifecycle/retire`)
}

export function restoreRetiredNodeToObserving(nodeId: string) {
  return postJSON<NodeRecord>(`/api/nodes/${nodeId}/lifecycle/restore-to-observing`)
}
```

In `web/src/lib/types.ts`, extend `StateChangeEventType` and `STATE_CHANGE_EVENT_TYPE_LABELS`:

```ts
| 'node_retired'
| 'node_restored_to_observing'
```

```ts
node_retired: '节点已退役',
node_restored_to_observing: '节点恢复到观察中',
```

- [x] **Step 3: Run focused frontend helper/label tests and commit**

Run:

```bash
cd web && npm test -- --run api EventList EventsPage
```

Expected: pass.

Commit:

```bash
git add web/src/lib/api.ts web/src/lib/api.test.ts web/src/lib/types.ts web/src/components/EventList.test.tsx web/src/pages/EventsPage.test.tsx
git commit -m "Expose Node lifecycle actions to the frontend"
```

---

### Task 3: Node Detail lifecycle card

**Files:**
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Modify: `web/src/pages/NodeDetailPage.test.tsx`

- [x] **Step 1: Add failing Node Detail lifecycle tests**

In `web/src/pages/NodeDetailPage.test.tsx`, add tests after the runtime-control tests:

1. Non-retired retirement confirmation:
   - Fetch Node, runtime facts, incidents, events.
   - Expect `退役节点`.
   - Click it.
   - Expect inline copy containing `历史记录保留`.
   - Click `取消`.
   - Assert no lifecycle POST happened.
   - Click `退役节点` again, then `确认退役`.
   - Assert POST `/api/nodes/nd_001/lifecycle/retire`.
   - Mock returned node with `lifecycle_status: '已退役'`.
   - Expect `恢复到观察中` appears.

2. Retired restore:
   - Initial node has `lifecycle_status: '已退役'`.
   - Expect explanation `已退役节点在 V1 中只能先恢复到观察中，不能直接恢复为在用。`
   - Click `恢复到观察中`.
   - Assert POST `/api/nodes/nd_001/lifecycle/restore-to-observing`.
   - Mock returned node with `lifecycle_status: '观察中'`.
   - Expect `退役节点` appears.

3. Error remains local:
   - Lifecycle POST returns `{ error: 'invalid lifecycle transition' }`, 409.
   - Expect `role="alert"` with that text.
   - Page hero remains visible.

4. Stale route guard:
   - Use existing `deferredResponse`.
   - Trigger retire on `nd_001`.
   - Route to `nd_002`.
   - Resolve old retire response.
   - Assert `nd_002` remains visible and `已退役` from old response does not replace it.

Run:

```bash
cd web && npm test -- --run NodeDetailPage
```

Expected: fail because lifecycle UI is missing.

- [x] **Step 2: Implement lifecycle card and handlers**

In `web/src/pages/NodeDetailPage.tsx`:

- Import `retireNode` and `restoreRetiredNodeToObserving`.
- Add local state:

```ts
type LifecycleAction = 'retire' | 'restore-to-observing'
const NODE_LIFECYCLE_ACTION_ERROR = '节点生命周期操作失败'
const NODE_LIFECYCLE_RETIRED = '已退役'

const [lifecycleAction, setLifecycleAction] = useState<LifecycleAction | null>(null)
const [lifecycleError, setLifecycleError] = useState<string | null>(null)
const [retireConfirmOpen, setRetireConfirmOpen] = useState(false)
```

- Add `handleLifecycleAction(action, request)` mirroring `handleRuntimeAction` stale guards and updating `state.node` from returned `NodeRecord`.
- Clear local lifecycle error before action.
- Close confirmation after successful retire.

Render a `DetailSection eyebrow="Lifecycle Control" title="生命周期"` near runtime control:

```tsx
<DetailSection eyebrow="Lifecycle Control" title="生命周期">
  <div className="page-stack">
    <p>退役会让该 Node 退出活跃舰队语义，但不会删除节点、清空历史或抹除事件。</p>
    {node.lifecycle_status === NODE_LIFECYCLE_RETIRED ? (
      <>
        <p>已退役节点在 V1 中只能先恢复到观察中，不能直接恢复为在用。</p>
        <button type="button" disabled={lifecycleAction !== null} onClick={() => void handleLifecycleAction('restore-to-observing', restoreRetiredNodeToObserving)}>
          {lifecycleAction === 'restore-to-observing' ? '正在恢复…' : '恢复到观察中'}
        </button>
      </>
    ) : (
      <>
        <button type="button" disabled={lifecycleAction !== null} onClick={() => setRetireConfirmOpen(true)}>
          退役节点
        </button>
        {retireConfirmOpen ? (
          <article className="empty-state" aria-label="退役确认">
            <h3>确认退役这个节点？</h3>
            <p>操作后该 Node 将退出活跃舰队，历史记录保留。这不是删除，也不会清空事件或 observation。</p>
            <div className="badge-row badge-row--wrap">
              <button type="button" disabled={lifecycleAction !== null} onClick={() => void handleLifecycleAction('retire', retireNode)}>
                {lifecycleAction === 'retire' ? '正在退役…' : '确认退役'}
              </button>
              <button type="button" disabled={lifecycleAction !== null} onClick={() => setRetireConfirmOpen(false)}>
                取消
              </button>
            </div>
          </article>
        ) : null}
      </>
    )}
    {lifecycleError ? <p role="alert">{lifecycleError}</p> : null}
  </div>
</DetailSection>
```

- [x] **Step 3: Run focused Node Detail tests and commit**

Run:

```bash
cd web && npm test -- --run NodeDetailPage
```

Expected: pass.

Commit:

```bash
git add web/src/pages/NodeDetailPage.tsx web/src/pages/NodeDetailPage.test.tsx
git commit -m "Manage Node lifecycle from detail view"
```

---

### Task 4: Full verification and final review

**Files:**
- No planned edits unless verification exposes an issue.

- [x] **Step 1: Run focused checks**

Run:

```bash
go test ./internal/center/store ./internal/center/http/handlers ./internal/center/http ./cmd/houfeng-center
cd web && npm test -- --run api EventList EventsPage NodeDetailPage
```

Expected: pass.

- [x] **Step 2: Run repository verification**

Run:

```bash
go test ./...
cd web && npm test -- --run
cd web && npm run build
cd web && npm run lint
./scripts/verify.sh
```

Expected: pass.

- [x] **Step 3: Final scope review**

Confirm:

- Lifecycle routes use `/lifecycle/*`, not `/runtime/*`.
- No generic Node edit API was added.
- No deletion or history rewrite behavior was added.
- Retirement remains detail-page-only.
- Retired restore goes to `观察中`, not `在用`.
- Error messages remain local to the lifecycle card.

- [x] **Step 4: Dispatch final code review**

Use a fresh code-review subagent for the whole slice. If blocked, apply `superpowers:receiving-code-review`, fix minimally, rerun focused and full verification, and re-review.
